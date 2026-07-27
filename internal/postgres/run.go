package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/probehive/probehive/internal/run"
)

// partitionAdvisoryLockKey serializes partition maintenance the way the migration runner
// serializes migrations. Two workers running maintenance concurrently would otherwise race
// between "does this partition exist" and "create it" (ADR 0025).
var _ run.Store = (*RunStore)(nil)

const partitionAdvisoryLockKey int64 = 7355608015

// runColumns is the projection every Run read shares.
const runColumns = `id, organization_id, monitor_id, revision_number, location, scheduled_for,
       kind, outcome, started_at, finished_at, lease_holder, lease_expires_at`

// RunStore persists Runs, Observations, and the outbox that follows a committed Run.
type RunStore struct {
	pool *pgxpool.Pool
}

// Runs returns the Run persistence adapter.
func (database *DB) Runs() *RunStore { return &RunStore{pool: database.pool} }

// ClaimSlot takes the lease on one Run slot, inserting the Run that holds it.
//
// The insert is the claim: the slot uniqueness index of ADR 0021 is what makes it exclusive,
// so there is no window between reserving a slot and writing the row that owns it. A slot
// whose previous lease has expired is reclaimed in the same statement, which is the storage
// half of ADR 0021's rule that an expired lease is reclaimable by any worker.
//
// It returns run.ErrSlotHeld when another worker holds an unexpired lease or the slot has
// already recorded an outcome.
func (store *RunStore) ClaimSlot(ctx context.Context, value run.Run, now time.Time) (run.Run, error) {
	if !value.InFlight() {
		return run.Run{}, errors.New("claiming a slot requires an in-flight Run")
	}
	if value.Kind == run.KindManual {
		if err := store.insertManualRun(ctx, value, now); err != nil {
			return run.Run{}, err
		}
		return value, nil
	}

	// ON CONFLICT names the partial slot index, so only a non-manual slot collides here.
	// The DO UPDATE guard is the reclaim condition: an unexpired lease and a Run that has
	// already recorded an outcome both leave the existing row untouched and return nothing.
	//
	// A reclaim transfers the lease and keeps the existing Run identifier, so a slot has one
	// identity for its whole life however many workers attempt it. The caller executes
	// against the returned Run rather than the one it proposed.
	row := store.pool.QueryRow(ctx, `
INSERT INTO runs (
    id, organization_id, monitor_id, revision_number, location, scheduled_for,
    kind, outcome, started_at, finished_at, lease_holder, lease_expires_at, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, NULL, NULL, NULL, $8, $9, $10)
ON CONFLICT (monitor_id, revision_number, location, scheduled_for) WHERE kind <> 'manual'
DO UPDATE SET lease_holder = EXCLUDED.lease_holder,
              lease_expires_at = EXCLUDED.lease_expires_at
WHERE runs.outcome IS NULL AND runs.lease_expires_at <= $11
RETURNING `+runColumns,
		string(value.ID), value.Slot.OrganizationID, value.Slot.MonitorID, value.Slot.RevisionNumber,
		value.Slot.Location, value.Slot.ScheduledFor.UTC(), string(value.Kind),
		value.LeaseHolder, value.LeaseExpiresAt.UTC(), now.UTC(), now.UTC())

	claimed, found, err := scanRun(row)
	if err != nil {
		return run.Run{}, err
	}
	if !found {
		return run.Run{}, run.ErrSlotHeld
	}
	return claimed, nil
}

// insertManualRun writes a Run that the slot index deliberately does not cover. A manual Run
// is exempt from slot uniqueness under ADR 0021, so a conflict here is a duplicate
// identifier rather than a contested slot.
func (store *RunStore) insertManualRun(ctx context.Context, value run.Run, now time.Time) error {
	_, err := store.pool.Exec(ctx, `
INSERT INTO runs (
    id, organization_id, monitor_id, revision_number, location, scheduled_for,
    kind, outcome, started_at, finished_at, lease_holder, lease_expires_at, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, NULL, NULL, NULL, $8, $9, $10)`,
		string(value.ID), value.Slot.OrganizationID, value.Slot.MonitorID, value.Slot.RevisionNumber,
		value.Slot.Location, value.Slot.ScheduledFor.UTC(), string(value.Kind),
		value.LeaseHolder, value.LeaseExpiresAt.UTC(), now.UTC())
	if err != nil {
		return fmt.Errorf("insert manual Run: %w", err)
	}
	return nil
}

// RenewLease extends a claim the caller still holds. It returns run.ErrLeaseLost when the
// lease was reclaimed or the Run already recorded an outcome.
func (store *RunStore) RenewLease(ctx context.Context, value run.Run) error {
	result, err := store.pool.Exec(ctx, `
UPDATE runs
SET lease_expires_at = $1
WHERE id = $2 AND scheduled_for = $3 AND organization_id = $4
  AND lease_holder = $5 AND outcome IS NULL`,
		value.LeaseExpiresAt.UTC(), string(value.ID), value.Slot.ScheduledFor.UTC(),
		value.Slot.OrganizationID, value.LeaseHolder)
	if err != nil {
		return fmt.Errorf("renew Run lease: %w", err)
	}
	if result.RowsAffected() == 0 {
		return run.ErrLeaseLost
	}
	return nil
}

// ReleaseSlot deletes an in-flight Run the caller still holds, so a graceful shutdown frees
// the slot immediately instead of leaving it unavailable until the lease expires (ADR 0021).
//
// It deletes rather than expiring the lease because the alternative is a Run that never
// started and never will, which would be indistinguishable from an abandoned claim.
func (store *RunStore) ReleaseSlot(ctx context.Context, value run.Run) error {
	result, err := store.pool.Exec(ctx, `
DELETE FROM runs
WHERE id = $1 AND scheduled_for = $2 AND organization_id = $3
  AND lease_holder = $4 AND outcome IS NULL`,
		string(value.ID), value.Slot.ScheduledFor.UTC(), value.Slot.OrganizationID, value.LeaseHolder)
	if err != nil {
		return fmt.Errorf("release Run slot: %w", err)
	}
	if result.RowsAffected() == 0 {
		return run.ErrLeaseLost
	}
	return nil
}

// Complete records an execution outcome, its Observation, and any effects that must follow
// the committed Run, in one transaction (ADR 0021).
//
// The lease is the authority: the update matches on the holder token the caller still
// believes it owns, and affects no rows when the lease was reclaimed. That is how a worker
// whose lease expired discovers it must discard its result rather than write it, so the
// whole transaction rolls back and the Observation and outbox entries are never committed.
//
// The supplied Run must already carry its recorded outcome; Complete does not apply the
// transition, because deciding an outcome is run.Run's job and writing it down is this one's.
func (store *RunStore) Complete(
	ctx context.Context,
	value run.Run,
	holder string,
	observation run.Observation,
	entries []run.OutboxEntry,
) error {
	if value.InFlight() {
		return errors.New("completing a Run requires a recorded outcome")
	}
	if value.Outcome == run.OutcomeSkipped {
		return errors.New("a skipped Run is recorded with RecordSkipped, not Complete")
	}
	if holder == "" {
		return errors.New("completing a Run requires the lease holder that claimed it")
	}
	if err := observation.Validate(); err != nil {
		return fmt.Errorf("validate Observation: %w", err)
	}
	if observation.RunID != value.ID || !observation.ScheduledFor.Equal(value.Slot.ScheduledFor) {
		return errors.New("the Observation does not belong to the Run being completed")
	}
	if len(entries) > run.MaxOutboxBatchSize {
		return fmt.Errorf("at most %d outbox entries accompany one Run", run.MaxOutboxBatchSize)
	}
	for _, entry := range entries {
		if err := entry.Validate(); err != nil {
			return fmt.Errorf("validate outbox entry: %w", err)
		}
	}

	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin Run completion: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	result, err := transaction.Exec(ctx, `
UPDATE runs
SET outcome = $1, started_at = $2, finished_at = $3, lease_holder = NULL, lease_expires_at = NULL
WHERE id = $4 AND scheduled_for = $5 AND organization_id = $6
  AND lease_holder = $7 AND outcome IS NULL`,
		string(value.Outcome), value.StartedAt.UTC(), value.FinishedAt.UTC(),
		string(value.ID), value.Slot.ScheduledFor.UTC(), value.Slot.OrganizationID, holder)
	if err != nil {
		return fmt.Errorf("record Run outcome: %w", err)
	}
	if result.RowsAffected() == 0 {
		return run.ErrLeaseLost
	}

	if err := insertObservation(ctx, transaction, observation, value.FinishedAt); err != nil {
		return err
	}
	for _, entry := range entries {
		if err := insertOutboxEntry(ctx, transaction, entry); err != nil {
			return err
		}
	}

	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit Run completion: %w", err)
	}
	return nil
}

// RecordSkipped writes a slot the scheduler deliberately did not execute (ADR 0021). It
// takes no lease: there is nothing to protect, because the Run is finished the moment it is
// written. A slot another worker already claimed or executed is left alone.
func (store *RunStore) RecordSkipped(ctx context.Context, value run.Run, now time.Time) error {
	if value.Outcome != run.OutcomeSkipped {
		return errors.New("recording a skipped Run requires the skipped outcome")
	}
	if value.Kind == run.KindManual {
		return errors.New("a manual Run is requested rather than scheduled, so it cannot be skipped")
	}
	result, err := store.pool.Exec(ctx, `
INSERT INTO runs (
    id, organization_id, monitor_id, revision_number, location, scheduled_for,
    kind, outcome, started_at, finished_at, lease_holder, lease_expires_at, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULL, NULL, NULL, NULL, $9)
ON CONFLICT (monitor_id, revision_number, location, scheduled_for) WHERE kind <> 'manual'
DO NOTHING`,
		string(value.ID), value.Slot.OrganizationID, value.Slot.MonitorID, value.Slot.RevisionNumber,
		value.Slot.Location, value.Slot.ScheduledFor.UTC(), string(value.Kind),
		string(value.Outcome), now.UTC())
	if err != nil {
		return fmt.Errorf("record skipped Run: %w", err)
	}
	if result.RowsAffected() == 0 {
		return run.ErrSlotHeld
	}
	return nil
}

// FindRun loads one Run under explicit Organization scope.
func (store *RunStore) FindRun(ctx context.Context, organizationID string, id run.ID, scheduledFor time.Time) (run.Run, bool, error) {
	return scanRun(store.pool.QueryRow(ctx, `
SELECT `+runColumns+`
FROM runs
WHERE id = $1 AND scheduled_for = $2 AND organization_id = $3`,
		string(id), scheduledFor.UTC(), organizationID))
}

// ListRunsForMonitor returns the most recent Runs of one Monitor, newest first. The bound is
// the caller's, because an unbounded read of a partitioned high-volume table is a mistake
// that only shows up in production.
func (store *RunStore) ListRunsForMonitor(
	ctx context.Context,
	organizationID, monitorID string,
	notBefore time.Time,
	limit int,
) ([]run.Run, error) {
	if limit < 1 || limit > MaxRunPageSize {
		return nil, fmt.Errorf("a Run page is 1 to %d rows", MaxRunPageSize)
	}
	rows, err := store.pool.Query(ctx, `
SELECT `+runColumns+`
FROM runs
WHERE monitor_id = $1 AND organization_id = $2 AND scheduled_for >= $3
ORDER BY scheduled_for DESC, id
LIMIT $4`, monitorID, organizationID, notBefore.UTC(), limit)
	if err != nil {
		return nil, fmt.Errorf("list Runs: %w", err)
	}
	defer rows.Close()

	values := make([]run.Run, 0)
	for rows.Next() {
		value, _, scanErr := scanRun(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list Runs: %w", err)
	}
	return values, nil
}

// MaxRunPageSize bounds one read of the Run table.
const MaxRunPageSize = 500

// FindObservation loads the stored detail of one Run under explicit Organization scope.
func (store *RunStore) FindObservation(ctx context.Context, organizationID string, id run.ID, scheduledFor time.Time) (run.Observation, bool, error) {
	var (
		failureCode        *string
		failureClass       *string
		duration           int64
		connect            int64
		tlsPhase           int64
		firstByte          int64
		statusCode         *int32
		protocol           *string
		redirectCount      *int32
		bodyBytes          *int64
		bodyTruncated      *bool
		tlsVersion         *string
		tlsCipherSuite     *string
		tlsCertificateTime *time.Time
	)
	err := store.pool.QueryRow(ctx, `
SELECT failure_code, failure_class, duration_microseconds, connect_microseconds,
       tls_microseconds, first_byte_microseconds, http_status_code, http_protocol,
       http_redirect_count, http_body_bytes, http_body_truncated,
       tls_version, tls_cipher_suite, tls_certificate_expires_at
FROM observations
WHERE run_id = $1 AND scheduled_for = $2 AND organization_id = $3`,
		string(id), scheduledFor.UTC(), organizationID).Scan(
		&failureCode, &failureClass, &duration, &connect, &tlsPhase, &firstByte,
		&statusCode, &protocol, &redirectCount, &bodyBytes, &bodyTruncated,
		&tlsVersion, &tlsCipherSuite, &tlsCertificateTime)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return run.Observation{}, false, nil
		}
		return run.Observation{}, false, fmt.Errorf("scan Observation: %w", err)
	}

	value := run.Observation{
		RunID:          id,
		ScheduledFor:   scheduledFor.UTC(),
		OrganizationID: organizationID,
		FailureCode:    textValue(failureCode),
		FailureClass:   textValue(failureClass),
		Duration:       time.Duration(duration) * time.Microsecond,
		Phases: run.Phases{
			Connect:   time.Duration(connect) * time.Microsecond,
			TLS:       time.Duration(tlsPhase) * time.Microsecond,
			FirstByte: time.Duration(firstByte) * time.Microsecond,
		},
	}
	if statusCode != nil {
		detail := run.HTTPDetail{
			StatusCode:    int(*statusCode),
			Protocol:      textValue(protocol),
			RedirectCount: int(int32Value(redirectCount)),
			BodyBytes:     int64Value(bodyBytes),
			BodyTruncated: boolValue(bodyTruncated),
		}
		if tlsVersion != nil || tlsCipherSuite != nil || tlsCertificateTime != nil {
			expiry := time.Time{}
			if tlsCertificateTime != nil {
				expiry = tlsCertificateTime.UTC()
			}
			detail.TLS = &run.TLSDetail{
				Version:              textValue(tlsVersion),
				CipherSuite:          textValue(tlsCipherSuite),
				CertificateExpiresAt: expiry,
			}
		}
		value.HTTP = &detail
	}
	return value, true, nil
}

func insertObservation(ctx context.Context, transaction pgx.Tx, value run.Observation, createdAt time.Time) error {
	var (
		statusCode     *int32
		protocol       *string
		redirectCount  *int32
		bodyBytes      *int64
		bodyTruncated  *bool
		tlsVersion     *string
		tlsCipherSuite *string
		tlsExpiry      *time.Time
	)
	if value.HTTP != nil {
		statusCode = int32Pointer(int32(value.HTTP.StatusCode))
		protocol = textPointer(value.HTTP.Protocol)
		redirectCount = int32Pointer(int32(value.HTTP.RedirectCount))
		bodyBytes = &value.HTTP.BodyBytes
		bodyTruncated = &value.HTTP.BodyTruncated
		if value.HTTP.TLS != nil {
			tlsVersion = textPointer(value.HTTP.TLS.Version)
			tlsCipherSuite = textPointer(value.HTTP.TLS.CipherSuite)
			if !value.HTTP.TLS.CertificateExpiresAt.IsZero() {
				expiry := value.HTTP.TLS.CertificateExpiresAt.UTC()
				tlsExpiry = &expiry
			}
		}
	}

	if _, err := transaction.Exec(ctx, `
INSERT INTO observations (
    run_id, scheduled_for, organization_id, failure_code, failure_class,
    duration_microseconds, connect_microseconds, tls_microseconds, first_byte_microseconds,
    http_status_code, http_protocol, http_redirect_count, http_body_bytes, http_body_truncated,
    tls_version, tls_cipher_suite, tls_certificate_expires_at, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)`,
		string(value.RunID), value.ScheduledFor.UTC(), value.OrganizationID,
		textPointer(value.FailureCode), textPointer(value.FailureClass),
		value.Duration.Microseconds(), value.Phases.Connect.Microseconds(),
		value.Phases.TLS.Microseconds(), value.Phases.FirstByte.Microseconds(),
		statusCode, protocol, redirectCount, bodyBytes, bodyTruncated,
		tlsVersion, tlsCipherSuite, tlsExpiry, createdAt.UTC()); err != nil {
		return fmt.Errorf("insert Observation: %w", err)
	}
	return nil
}

func insertOutboxEntry(ctx context.Context, transaction pgx.Tx, entry run.OutboxEntry) error {
	if _, err := transaction.Exec(ctx, `
INSERT INTO outbox_entries (id, organization_id, topic, payload, attempts, created_at, available_at)
VALUES ($1, $2, $3, $4, 0, $5, $5)`,
		string(entry.ID), entry.OrganizationID, entry.Topic,
		string(entry.Payload), entry.CreatedAt.UTC()); err != nil {
		return fmt.Errorf("insert outbox entry: %w", err)
	}
	return nil
}

func scanRun(row rowScanner) (run.Run, bool, error) {
	var (
		id             string
		organizationID string
		monitorID      string
		revisionNumber int
		location       string
		scheduledFor   time.Time
		kind           string
		outcome        *string
		startedAt      *time.Time
		finishedAt     *time.Time
		leaseHolder    *string
		leaseExpiresAt *time.Time
	)
	if err := row.Scan(
		&id, &organizationID, &monitorID, &revisionNumber, &location, &scheduledFor,
		&kind, &outcome, &startedAt, &finishedAt, &leaseHolder, &leaseExpiresAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return run.Run{}, false, nil
		}
		return run.Run{}, false, fmt.Errorf("scan Run: %w", err)
	}

	value, err := run.Restore(
		run.ID(id),
		run.Slot{
			OrganizationID: organizationID,
			MonitorID:      monitorID,
			RevisionNumber: revisionNumber,
			Location:       location,
			ScheduledFor:   scheduledFor.UTC(),
		},
		run.Kind(kind),
		run.Outcome(textValue(outcome)),
		instantValue(startedAt), instantValue(finishedAt),
		textValue(leaseHolder), instantValue(leaseExpiresAt),
	)
	if err != nil {
		return run.Run{}, false, fmt.Errorf("restore Run: %w", err)
	}
	return value, true, nil
}

func textValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func textPointer(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func int32Pointer(value int32) *int32 { return &value }

func int32Value(value *int32) int32 {
	if value == nil {
		return 0
	}
	return *value
}

func int64Value(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func boolValue(value *bool) bool {
	if value == nil {
		return false
	}
	return *value
}

func instantValue(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return value.UTC()
}
