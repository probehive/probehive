package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/probehive/probehive/internal/outbox"
)

var _ outbox.Store = (*OutboxStore)(nil)

type OutboxStore struct{ pool *pgxpool.Pool }

func (database *DB) Outbox() *OutboxStore { return &OutboxStore{pool: database.pool} }

func (store *OutboxStore) Claim(ctx context.Context, holder string, now, expiresAt time.Time, limit int) ([]outbox.Entry, error) {
	if holder == "" || !isUTCInstant(now) || !isUTCInstant(expiresAt) || !expiresAt.After(now) {
		return nil, errors.New("claiming outbox entries requires a holder and UTC lease")
	}
	if limit < 1 || limit > outbox.DefaultBatchSize {
		return nil, fmt.Errorf("outbox claim limit is 1 to %d", outbox.DefaultBatchSize)
	}
	rows, err := store.pool.Query(ctx, `
WITH candidates AS (
    SELECT id FROM outbox_entries
    WHERE available_at <= $1 AND (lease_expires_at IS NULL OR lease_expires_at <= $1)
    ORDER BY available_at, id
    FOR UPDATE SKIP LOCKED
    LIMIT $2
)
UPDATE outbox_entries AS entry
SET lease_holder = $3, lease_expires_at = $4, attempts = entry.attempts + 1
FROM candidates
WHERE entry.id = candidates.id
RETURNING entry.id, entry.organization_id, entry.topic, entry.payload,
          entry.attempts, entry.created_at, entry.available_at,
          entry.lease_holder, entry.lease_expires_at, entry.gap_first_seen_at`,
		now.UTC(), limit, holder, expiresAt.UTC())
	if err != nil {
		return nil, fmt.Errorf("claim outbox entries: %w", err)
	}
	defer rows.Close()
	entries := make([]outbox.Entry, 0, limit)
	for rows.Next() {
		var entry outbox.Entry
		var gapFirstSeenAt *time.Time
		if err := rows.Scan(&entry.ID, &entry.OrganizationID, &entry.Topic, &entry.Payload,
			&entry.Attempts, &entry.CreatedAt, &entry.AvailableAt,
			&entry.LeaseHolder, &entry.LeaseExpiresAt,
			&gapFirstSeenAt); err != nil {
			return nil, fmt.Errorf("scan outbox entry: %w", err)
		}
		entry.CreatedAt, entry.AvailableAt = entry.CreatedAt.UTC(), entry.AvailableAt.UTC()
		entry.LeaseExpiresAt = entry.LeaseExpiresAt.UTC()
		if gapFirstSeenAt != nil {
			entry.GapFirstSeenAt = gapFirstSeenAt.UTC()
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan outbox entries: %w", err)
	}
	return entries, nil
}

func (store *OutboxStore) Succeed(ctx context.Context, entry outbox.Entry, holder string, now time.Time) error {
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin handled outbox completion: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	result, err := transaction.Exec(ctx, `
INSERT INTO processed_outbox_events (id, organization_id, topic, processed_at)
SELECT id, organization_id, topic, $1
FROM outbox_entries
WHERE id = $2 AND organization_id = $3 AND lease_holder = $4
ON CONFLICT (organization_id, id) DO NOTHING`,
		now.UTC(), entry.ID, entry.OrganizationID, holder)
	if err != nil {
		return fmt.Errorf("retain handled outbox marker: %w", err)
	}
	if result.RowsAffected() == 0 {
		var held bool
		if err := transaction.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1 FROM outbox_entries
    WHERE id = $1 AND organization_id = $2 AND lease_holder = $3
)`, entry.ID, entry.OrganizationID, holder).Scan(&held); err != nil {
			return fmt.Errorf("check handled outbox lease: %w", err)
		}
		if !held {
			return outbox.ErrLeaseLost
		}
	}
	result, err = transaction.Exec(ctx, `
DELETE FROM outbox_entries
WHERE id = $1 AND organization_id = $2 AND lease_holder = $3`,
		entry.ID, entry.OrganizationID, holder)
	if err != nil {
		return fmt.Errorf("delete handled outbox entry: %w", err)
	}
	if result.RowsAffected() == 0 {
		return outbox.ErrLeaseLost
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit handled outbox completion: %w", err)
	}
	return nil

}
func (store *OutboxStore) Fail(ctx context.Context, entry outbox.Entry, holder, failureCode string, next, now time.Time, dead bool) error {
	if dead {
		return store.deadLetter(ctx, entry, holder, failureCode, now)
	}
	result, err := store.pool.Exec(ctx, `
UPDATE outbox_entries
SET available_at = $1, lease_holder = NULL, lease_expires_at = NULL,
    last_failure_code = $2,
    gap_first_seen_at = CASE WHEN $2 = $3
        THEN COALESCE(gap_first_seen_at, $4) ELSE NULL END
WHERE id = $5 AND organization_id = $6 AND lease_holder = $7`,
		next.UTC(), failureCode, outbox.CodeAggregateVersionGap, now.UTC(),
		entry.ID, entry.OrganizationID, holder)
	if err != nil {
		return fmt.Errorf("release failed outbox entry: %w", err)
	}
	if result.RowsAffected() == 0 {
		return outbox.ErrLeaseLost
	}
	return nil
}

func (store *OutboxStore) deadLetter(ctx context.Context, entry outbox.Entry, holder, failureCode string, now time.Time) error {
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin outbox dead letter: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	result, err := transaction.Exec(ctx, `
INSERT INTO dead_letter_outbox_entries (
    id, organization_id, topic, payload, attempts, created_at,
    final_failure_code, dead_lettered_at
)
SELECT id, organization_id, topic, payload, attempts, created_at, $1, $2
FROM outbox_entries
WHERE id = $3 AND organization_id = $4 AND lease_holder = $5
ON CONFLICT (organization_id, id) DO NOTHING`,
		failureCode, now.UTC(), entry.ID, entry.OrganizationID, holder)
	if err != nil {
		return fmt.Errorf("insert dead-letter outbox entry: %w", err)
	}
	if result.RowsAffected() == 0 {
		return outbox.ErrLeaseLost
	}
	result, err = transaction.Exec(ctx, `
DELETE FROM outbox_entries WHERE id = $1 AND organization_id = $2 AND lease_holder = $3`,
		entry.ID, entry.OrganizationID, holder)
	if err != nil {
		return fmt.Errorf("delete dead-lettered outbox entry: %w", err)
	}
	if result.RowsAffected() == 0 {
		return outbox.ErrLeaseLost
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit outbox dead letter: %w", err)
	}
	return nil
}

func (store *OutboxStore) Cleanup(ctx context.Context, before time.Time) error {
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin outbox cleanup: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	if _, err := transaction.Exec(ctx, `DELETE FROM processed_outbox_events WHERE processed_at < $1`, before.UTC()); err != nil {
		return fmt.Errorf("delete expired processed outbox events: %w", err)
	}
	if _, err := transaction.Exec(ctx, `DELETE FROM dead_letter_outbox_entries WHERE dead_lettered_at < $1`, before.UTC()); err != nil {
		return fmt.Errorf("delete expired dead-letter outbox entries: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit outbox cleanup: %w", err)
	}
	return nil
}

func outboxEventProcessed(ctx context.Context, transaction pgx.Tx, id, organizationID string) (bool, error) {
	var exists bool
	if err := transaction.QueryRow(ctx, `
SELECT EXISTS (SELECT 1 FROM processed_outbox_events WHERE id = $1 AND organization_id = $2)`,
		id, organizationID).Scan(&exists); err != nil {
		return false, fmt.Errorf("check processed outbox event: %w", err)
	}
	return exists, nil
}

func markOutboxEventProcessed(ctx context.Context, transaction pgx.Tx, id, organizationID, topic string, processedAt time.Time) error {
	if _, err := transaction.Exec(ctx, `
INSERT INTO processed_outbox_events (id, organization_id, topic, processed_at)
VALUES ($1, $2, $3, $4) ON CONFLICT (organization_id, id) DO NOTHING`,
		id, organizationID, topic, processedAt.UTC()); err != nil {
		return fmt.Errorf("mark outbox event processed: %w", err)
	}
	return nil
}

func isUTCInstant(value time.Time) bool {
	if value.IsZero() {
		return false
	}
	_, offset := value.Zone()
	return offset == 0
}
