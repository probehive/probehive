package postgres

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/probehive/probehive/internal/monitor"
	"github.com/probehive/probehive/internal/run"
)

const checkViolation = "23514"

// TestSlotUniquenessMakesDuplicateExecutionUnrepresentable is the decision ADR 0021 calls a
// backstop: whatever the lease does, one slot is one row. Two workers claiming the same slot
// while the first lease is alive must not produce two Runs.
func TestSlotUniquenessMakesDuplicateExecutionUnrepresentable(t *testing.T) {
	database := newIntegrationDatabase(t, true)
	store, slot, now := seedRunSlot(t, database, 100)

	first := mustClaim(t, run.ID(testUUID(301)), slot, run.KindScheduled, "worker-a", now.Add(time.Minute))
	claimed, err := store.ClaimSlot(t.Context(), first, now)
	if err != nil {
		t.Fatalf("first ClaimSlot() error = %v", err)
	}
	if claimed.LeaseHolder != "worker-a" {
		t.Fatalf("first ClaimSlot() holder = %q, want worker-a", claimed.LeaseHolder)
	}

	second := mustClaim(t, run.ID(testUUID(302)), slot, run.KindScheduled, "worker-b", now.Add(time.Minute))
	if _, err := store.ClaimSlot(t.Context(), second, now); !errors.Is(err, run.ErrSlotHeld) {
		t.Fatalf("contended ClaimSlot() error = %v, want run.ErrSlotHeld", err)
	}

	if got := countRuns(t, database); got != 1 {
		t.Fatalf("Run count = %d, want exactly 1 row for one slot", got)
	}
}

// An expired lease is reclaimable by any worker (ADR 0021), and the reclaim keeps the slot's
// existing Run identity rather than minting a second one (ADR 0025).
func TestExpiredLeaseIsReclaimedAndKeepsTheRunIdentity(t *testing.T) {
	database := newIntegrationDatabase(t, true)
	store, slot, now := seedRunSlot(t, database, 110)

	first := mustClaim(t, run.ID(testUUID(301)), slot, run.KindScheduled, "worker-a", now.Add(30*time.Second))
	if _, err := store.ClaimSlot(t.Context(), first, now); err != nil {
		t.Fatalf("first ClaimSlot() error = %v", err)
	}

	afterExpiry := now.Add(2 * time.Minute)
	second := mustClaim(t, run.ID(testUUID(302)), slot, run.KindScheduled, "worker-b", afterExpiry.Add(time.Minute))
	reclaimed, err := store.ClaimSlot(t.Context(), second, afterExpiry)
	if err != nil {
		t.Fatalf("reclaiming ClaimSlot() error = %v", err)
	}
	if reclaimed.ID != first.ID {
		t.Fatalf("reclaimed Run ID = %q, want the slot's existing %q", reclaimed.ID, first.ID)
	}
	if reclaimed.LeaseHolder != "worker-b" {
		t.Fatalf("reclaimed lease holder = %q, want worker-b", reclaimed.LeaseHolder)
	}
	if got := countRuns(t, database); got != 1 {
		t.Fatalf("Run count after reclaim = %d, want 1", got)
	}
}

// The worker that lost its lease discovers it when recording, and its whole result — Run
// outcome, Observation, and outbox entries — is discarded rather than written (ADR 0021).
func TestCompleteDiscardsTheResultOfALostLease(t *testing.T) {
	database := newIntegrationDatabase(t, true)
	store, slot, now := seedRunSlot(t, database, 120)

	first := mustClaim(t, run.ID(testUUID(301)), slot, run.KindScheduled, "worker-a", now.Add(30*time.Second))
	if _, err := store.ClaimSlot(t.Context(), first, now); err != nil {
		t.Fatalf("ClaimSlot() error = %v", err)
	}

	afterExpiry := now.Add(2 * time.Minute)
	second := mustClaim(t, run.ID(testUUID(302)), slot, run.KindScheduled, "worker-b", afterExpiry.Add(time.Minute))
	reclaimed, err := store.ClaimSlot(t.Context(), second, afterExpiry)
	if err != nil {
		t.Fatalf("reclaiming ClaimSlot() error = %v", err)
	}

	// The original holder finishes anyway and tries to write what it measured.
	stale := first
	if err := stale.Complete(run.OutcomePassed, now, now.Add(time.Second)); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	staleObservation := observationFor(reclaimed, slot)
	entry := outboxEntry(t, slot.OrganizationID, "stale.effect")
	err = store.Complete(t.Context(), stale, "worker-a", staleObservation, []run.OutboxEntry{entry})
	if !errors.Is(err, run.ErrLeaseLost) {
		t.Fatalf("stale Complete() error = %v, want run.ErrLeaseLost", err)
	}
	if _, found, err := store.FindObservation(t.Context(), slot.OrganizationID, reclaimed.ID, slot.ScheduledFor); err != nil || found {
		t.Fatalf("FindObservation() after a lost lease = found %v (err %v), want no row", found, err)
	}
	if got := countOutboxEntries(t, database); got != 0 {
		t.Fatalf("outbox entries after a lost lease = %d, want 0", got)
	}

	// The holder that owns the lease writes its result normally.
	winner := reclaimed
	if err := winner.Complete(run.OutcomeFailed, afterExpiry, afterExpiry.Add(300*time.Millisecond)); err != nil {
		t.Fatalf("winner Complete() error = %v", err)
	}
	if err := store.Complete(t.Context(), winner, "worker-b", observationFor(winner, slot), nil); err != nil {
		t.Fatalf("winner store.Complete() error = %v", err)
	}
	stored, found, err := store.FindRun(t.Context(), slot.OrganizationID, winner.ID, slot.ScheduledFor)
	if err != nil || !found {
		t.Fatalf("FindRun() = found %v (err %v), want the completed Run", found, err)
	}
	if stored.Outcome != run.OutcomeFailed || stored.InFlight() {
		t.Fatalf("stored Run = %q/in-flight %v, want failed and finished", stored.Outcome, stored.InFlight())
	}
}

// ADR 0021 requires the Run, its Observation, and the effects that follow it to commit
// together. A rejected outbox entry must leave no Run outcome behind.
func TestCompleteWritesRunObservationAndOutboxInOneTransaction(t *testing.T) {
	database := newIntegrationDatabase(t, true)
	store, slot, now := seedRunSlot(t, database, 130)

	claimed := mustClaim(t, run.ID(testUUID(301)), slot, run.KindScheduled, "worker-a", now.Add(time.Minute))
	if _, err := store.ClaimSlot(t.Context(), claimed, now); err != nil {
		t.Fatalf("ClaimSlot() error = %v", err)
	}
	completed := claimed
	if err := completed.Complete(run.OutcomePassed, now, now.Add(420*time.Millisecond)); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	// An entry naming an Organization that does not exist violates the outbox foreign key,
	// so the whole completion must roll back.
	rogue := outboxEntry(t, testUUID(999), "unknown.tenant")
	if err := store.Complete(t.Context(), completed, "worker-a", observationFor(completed, slot), []run.OutboxEntry{rogue}); err == nil {
		t.Fatalf("Complete() with a rogue outbox entry = nil error, want a rejection")
	}
	stored, found, err := store.FindRun(t.Context(), slot.OrganizationID, completed.ID, slot.ScheduledFor)
	if err != nil || !found {
		t.Fatalf("FindRun() = found %v (err %v), want the still-claimed Run", found, err)
	}
	if !stored.InFlight() {
		t.Fatalf("rolled-back Run outcome = %q, want it still in flight", stored.Outcome)
	}

	// The same completion with a valid entry commits all three writes.
	entry := outboxEntry(t, slot.OrganizationID, "run.example")
	if err := store.Complete(t.Context(), completed, "worker-a", observationFor(completed, slot), []run.OutboxEntry{entry}); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	observation, found, err := store.FindObservation(t.Context(), slot.OrganizationID, completed.ID, slot.ScheduledFor)
	if err != nil || !found {
		t.Fatalf("FindObservation() = found %v (err %v), want the stored detail", found, err)
	}
	if observation.Duration != 420*time.Millisecond {
		t.Fatalf("stored Duration = %v, want 420ms", observation.Duration)
	}
	if observation.HTTP == nil || observation.HTTP.StatusCode != 200 || observation.HTTP.TLS == nil {
		t.Fatalf("stored HTTP detail = %#v, want status 200 with TLS detail", observation.HTTP)
	}
	if observation.HTTP.TLS.Version != "TLS 1.3" || observation.HTTP.TLS.CipherSuite != "TLS_AES_128_GCM_SHA256" {
		t.Fatalf("stored TLS detail = %#v, want the negotiated values", observation.HTTP.TLS)
	}
	if got := countOutboxEntries(t, database); got != 1 {
		t.Fatalf("outbox entries = %d, want 1", got)
	}
}

// Skip and record is ADR 0021's misfire policy: a gap after downtime is written down as a
// Run with no execution, which the schema must accept without a started_at.
func TestSkippedRunIsStoredWithoutExecutionInstants(t *testing.T) {
	database := newIntegrationDatabase(t, true)
	store, slot, now := seedRunSlot(t, database, 140)

	skipped, err := run.Skip(run.ID(testUUID(303)), slot, run.KindScheduled)
	if err != nil {
		t.Fatalf("run.Skip() error = %v", err)
	}
	if err := store.RecordSkipped(t.Context(), skipped, nil, now); err != nil {
		t.Fatalf("RecordSkipped() error = %v", err)
	}

	stored, found, err := store.FindRun(t.Context(), slot.OrganizationID, skipped.ID, slot.ScheduledFor)
	if err != nil || !found {
		t.Fatalf("FindRun() = found %v (err %v), want the skipped Run", found, err)
	}
	if stored.Outcome != run.OutcomeSkipped || !stored.StartedAt.IsZero() || !stored.FinishedAt.IsZero() {
		t.Fatalf("stored skipped Run = %#v, want no execution instants", stored)
	}

	// The slot is taken, so a late worker cannot execute it after all.
	late := mustClaim(t, run.ID(testUUID(308)), slot, run.KindScheduled, "worker-a", now.Add(time.Minute))
	if _, err := store.ClaimSlot(t.Context(), late, now); !errors.Is(err, run.ErrSlotHeld) {
		t.Fatalf("ClaimSlot() on a skipped slot = %v, want run.ErrSlotHeld", err)
	}
}

// Manual Runs are exempt from slot uniqueness (ADR 0021): asking twice is a request rather
// than a duplicate, so the partial index must not collapse them into one row.
func TestManualRunsAreExemptFromSlotUniqueness(t *testing.T) {
	database := newIntegrationDatabase(t, true)
	store, slot, now := seedRunSlot(t, database, 150)

	for index, id := range []run.ID{run.ID(testUUID(304)), run.ID(testUUID(305))} {
		value := mustClaim(t, id, slot, run.KindManual, fmt.Sprintf("worker-%d", index), now.Add(time.Minute))
		if _, err := store.ClaimSlot(t.Context(), value, now); err != nil {
			t.Fatalf("manual ClaimSlot() %d error = %v", index, err)
		}
	}
	if got := countRuns(t, database); got != 2 {
		t.Fatalf("manual Run count = %d, want 2", got)
	}

	// A scheduled Run of the same slot is still exclusive despite the manual rows present.
	scheduled := mustClaim(t, run.ID(testUUID(306)), slot, run.KindScheduled, "worker-a", now.Add(time.Minute))
	if _, err := store.ClaimSlot(t.Context(), scheduled, now); err != nil {
		t.Fatalf("scheduled ClaimSlot() error = %v", err)
	}
	contender := mustClaim(t, run.ID(testUUID(307)), slot, run.KindScheduled, "worker-b", now.Add(time.Minute))
	if _, err := store.ClaimSlot(t.Context(), contender, now); !errors.Is(err, run.ErrSlotHeld) {
		t.Fatalf("contended scheduled ClaimSlot() = %v, want run.ErrSlotHeld", err)
	}
}

// A graceful shutdown releases the slot rather than leaving it unavailable until the lease
// expires (ADR 0021).
func TestReleaseSlotFreesTheSlotImmediately(t *testing.T) {
	database := newIntegrationDatabase(t, true)
	store, slot, now := seedRunSlot(t, database, 160)

	claimed := mustClaim(t, run.ID(testUUID(301)), slot, run.KindScheduled, "worker-a", now.Add(10*time.Minute))
	if _, err := store.ClaimSlot(t.Context(), claimed, now); err != nil {
		t.Fatalf("ClaimSlot() error = %v", err)
	}
	if err := store.ReleaseSlot(t.Context(), claimed); err != nil {
		t.Fatalf("ReleaseSlot() error = %v", err)
	}
	if got := countRuns(t, database); got != 0 {
		t.Fatalf("Run count after release = %d, want 0", got)
	}

	next := mustClaim(t, run.ID(testUUID(302)), slot, run.KindScheduled, "worker-b", now.Add(10*time.Minute))
	if _, err := store.ClaimSlot(t.Context(), next, now); err != nil {
		t.Fatalf("ClaimSlot() after release error = %v", err)
	}
	if err := store.ReleaseSlot(t.Context(), claimed); !errors.Is(err, run.ErrLeaseLost) {
		t.Fatalf("ReleaseSlot() by a stale holder = %v, want run.ErrLeaseLost", err)
	}
}

func TestRenewLeaseRequiresTheCurrentHolder(t *testing.T) {
	database := newIntegrationDatabase(t, true)
	store, slot, now := seedRunSlot(t, database, 170)

	claimed := mustClaim(t, run.ID(testUUID(301)), slot, run.KindScheduled, "worker-a", now.Add(30*time.Second))
	if _, err := store.ClaimSlot(t.Context(), claimed, now); err != nil {
		t.Fatalf("ClaimSlot() error = %v", err)
	}
	renewed := claimed
	if err := renewed.RenewLease(now.Add(5 * time.Minute)); err != nil {
		t.Fatalf("run.RenewLease() error = %v", err)
	}
	if err := store.RenewLease(t.Context(), renewed); err != nil {
		t.Fatalf("store.RenewLease() error = %v", err)
	}

	impostor := renewed
	impostor.LeaseHolder = "worker-b"
	if err := store.RenewLease(t.Context(), impostor); !errors.Is(err, run.ErrLeaseLost) {
		t.Fatalf("RenewLease() by another holder = %v, want run.ErrLeaseLost", err)
	}
}

// The tenant scope is enforced by the query, not by the caller remembering to filter.
func TestRunQueriesDenyWrongOrganization(t *testing.T) {
	database := newIntegrationDatabase(t, true)
	store, slot, now := seedRunSlot(t, database, 180)
	otherOrganization, _ := seedTenant(t, database, 195, "other-run-tenant")

	claimed := mustClaim(t, run.ID(testUUID(301)), slot, run.KindScheduled, "worker-a", now.Add(time.Minute))
	if _, err := store.ClaimSlot(t.Context(), claimed, now); err != nil {
		t.Fatalf("ClaimSlot() error = %v", err)
	}
	completed := claimed
	if err := completed.Complete(run.OutcomePassed, now, now.Add(time.Second)); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if err := store.Complete(t.Context(), completed, "worker-a", observationFor(completed, slot), nil); err != nil {
		t.Fatalf("store.Complete() error = %v", err)
	}

	wrong := string(otherOrganization.ID)
	if _, found, err := store.FindRun(t.Context(), wrong, completed.ID, slot.ScheduledFor); err != nil || found {
		t.Fatalf("wrong-Organization FindRun() = found %v (err %v), want no row", found, err)
	}
	if _, found, err := store.FindObservation(t.Context(), wrong, completed.ID, slot.ScheduledFor); err != nil || found {
		t.Fatalf("wrong-Organization FindObservation() = found %v (err %v), want no row", found, err)
	}
	runs, err := store.ListRunsForMonitor(t.Context(), wrong, slot.MonitorID, slot.ScheduledFor.Add(-time.Hour), 10)
	if err != nil {
		t.Fatalf("wrong-Organization ListRunsForMonitor() error = %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("wrong-Organization ListRunsForMonitor() = %d rows, want none", len(runs))
	}
}

// ADR 0025 makes "in flight" have exactly one spelling. The check constraints are what keep
// a hand-written row from inventing a second one.
func TestRunCheckConstraintsRejectContradictoryState(t *testing.T) {
	database := newIntegrationDatabase(t, true)
	store, slot, now := seedRunSlot(t, database, 200)
	if _, err := store.EnsurePartitions(t.Context(), now, run.DefaultPartitionsAhead); err != nil {
		t.Fatalf("EnsurePartitions() error = %v", err)
	}

	cases := map[string]struct {
		outcome    any
		startedAt  any
		finishedAt any
		holder     any
		leaseEnd   any
		constraint string
	}{
		"finished but still leased": {
			"passed", now, now, "worker-a", now.Add(time.Minute), "ck_runs_lease_matches_outcome",
		},
		"in flight with no lease": {
			nil, nil, nil, nil, nil, "ck_runs_lease_matches_outcome",
		},
		"lease expiry without a holder": {
			nil, nil, nil, nil, now.Add(time.Minute), "ck_runs_lease_matches_outcome",
		},
		"skipped with execution instants": {
			"skipped", now, now, nil, nil, "ck_runs_execution_instants",
		},
		"executed without instants": {
			"passed", nil, nil, nil, nil, "ck_runs_execution_instants",
		},
		"finished before it started": {
			"passed", now, now.Add(-time.Second), nil, nil, "ck_runs_execution_instants",
		},
		"unknown outcome": {
			"degraded", now, now, nil, nil, "ck_runs_outcome",
		},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := database.pool.Exec(t.Context(), `
INSERT INTO runs (
    id, organization_id, monitor_id, revision_number, location, scheduled_for,
    kind, outcome, started_at, finished_at, lease_holder, lease_expires_at, created_at
) VALUES ($1, $2, $3, $4, $5, $6, 'scheduled', $7, $8, $9, $10, $11, $12)`,
				testUUID(700+len(name)), slot.OrganizationID, slot.MonitorID, slot.RevisionNumber,
				slot.Location, slot.ScheduledFor, testCase.outcome, testCase.startedAt,
				testCase.finishedAt, testCase.holder, testCase.leaseEnd, now)
			requireConstraint(t, err, checkViolation, testCase.constraint)
		})
	}
}

// ADR 0024 gives an Observation nothing to redact by giving it nowhere to put target text.
// The stored columns are the enforcement of that, so their absence is asserted directly.
func TestObservationsTableHoldsNoTargetSuppliedText(t *testing.T) {
	database := newIntegrationDatabase(t, true)

	rows, err := database.pool.Query(t.Context(), `
SELECT column_name
FROM information_schema.columns
WHERE table_schema = current_schema() AND table_name = 'observations'`)
	if err != nil {
		t.Fatalf("read observation columns: %v", err)
	}
	defer rows.Close()

	forbidden := map[string]struct{}{
		"message": {}, "body": {}, "response_body": {}, "headers": {},
		"response_headers": {}, "detail": {}, "error_message": {}, "url": {},
	}
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			t.Fatalf("scan observation column: %v", err)
		}
		if _, banned := forbidden[column]; banned {
			t.Fatalf("observations has column %q, which ADR 0024 forbids", column)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read observation columns: %v", err)
	}
}

// Partition maintenance is what keeps inserts possible: ADR 0025 creates no default
// partition, so a month with no partition is an insert failure by design.
func TestEnsurePartitionsIsIdempotentAndRequiredForInserts(t *testing.T) {
	database := newIntegrationDatabase(t, true)
	store := database.Runs()
	now := time.Date(2026, time.July, 27, 10, 0, 0, 0, time.UTC)

	created, err := store.EnsurePartitions(t.Context(), now, run.DefaultPartitionsAhead)
	if err != nil {
		t.Fatalf("EnsurePartitions() error = %v", err)
	}
	want := []string{
		"runs_2026_07", "runs_2026_08", "runs_2026_09",
		"observations_2026_07", "observations_2026_08", "observations_2026_09",
	}
	if len(created) != len(want) {
		t.Fatalf("EnsurePartitions() created %v, want %v", created, want)
	}
	for _, name := range want {
		if !relationExists(t, database, name) {
			t.Fatalf("partition %q does not exist", name)
		}
	}

	repeated, err := store.EnsurePartitions(t.Context(), now, run.DefaultPartitionsAhead)
	if err != nil {
		t.Fatalf("repeated EnsurePartitions() error = %v", err)
	}
	if len(repeated) != 0 {
		t.Fatalf("repeated EnsurePartitions() created %v, want nothing", repeated)
	}
}

func TestInsertWithoutAPartitionFails(t *testing.T) {
	database := newIntegrationDatabase(t, true)
	organizationValue, project := seedTenant(t, database, 210, "unpartitioned-tenant")
	monitorValue := seedMonitor(t, database, 215, organizationValue, project, testTime())

	// No partition covers this instant, because EnsurePartitions was never called.
	_, err := database.pool.Exec(t.Context(), `
INSERT INTO runs (
    id, organization_id, monitor_id, revision_number, location, scheduled_for,
    kind, outcome, started_at, finished_at, lease_holder, lease_expires_at, created_at
) VALUES ($1, $2, $3, 1, 'managed-eu-west', $4, 'scheduled', 'skipped', NULL, NULL, NULL, NULL, $4)`,
		testUUID(216), string(organizationValue.ID), string(monitorValue.ID), testTime())
	if err == nil {
		t.Fatalf("insert without a partition = nil error, want a rejection")
	}
}

// Expiry is ADR 0021's O(1) catalogue operation: whole partitions are dropped, partitions
// still inside the window are kept, and anything the job did not create is left alone.
func TestDropExpiredPartitionsDropsOnlyWhollyAgedOwnedPartitions(t *testing.T) {
	database := newIntegrationDatabase(t, true)
	store := database.Runs()
	seeded := time.Date(2026, time.January, 15, 0, 0, 0, 0, time.UTC)
	if _, err := store.EnsurePartitions(t.Context(), seeded, 3); err != nil {
		t.Fatalf("EnsurePartitions() error = %v", err)
	}

	// A partition an operator attached by hand, under a name maintenance does not recognise.
	if _, err := database.pool.Exec(t.Context(), `
CREATE TABLE runs_operator_archive PARTITION OF runs
FOR VALUES FROM ('2025-12-01T00:00:00Z') TO ('2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("create operator partition: %v", err)
	}

	now := time.Date(2026, time.April, 15, 0, 0, 0, 0, time.UTC)
	dropped, err := store.DropExpiredPartitions(t.Context(), run.DefaultRetention(), now)
	if err != nil {
		t.Fatalf("DropExpiredPartitions() error = %v", err)
	}

	// With a 30-day window on 2026-04-15 the cutoff is 2026-03-16, so January and February
	// are wholly aged out and March is not.
	want := map[string]struct{}{
		"runs_2026_01": {}, "runs_2026_02": {},
		"observations_2026_01": {}, "observations_2026_02": {},
	}
	if len(dropped) != len(want) {
		t.Fatalf("DropExpiredPartitions() = %v, want %v", dropped, want)
	}
	for _, name := range dropped {
		if _, expected := want[name]; !expected {
			t.Fatalf("DropExpiredPartitions() dropped %q, which is still inside the window", name)
		}
		if relationExists(t, database, name) {
			t.Fatalf("partition %q still exists after being dropped", name)
		}
	}
	for _, kept := range []string{"runs_2026_03", "runs_2026_04", "runs_operator_archive"} {
		if !relationExists(t, database, kept) {
			t.Fatalf("partition %q was dropped, want it kept", kept)
		}
	}
}

// A zero-value Retention would make every partition expired, so expiry refuses one rather
// than treating "unconfigured" as "keep nothing".
func TestDropExpiredPartitionsRefusesAnUnconfiguredWindow(t *testing.T) {
	database := newIntegrationDatabase(t, true)
	store := database.Runs()
	now := time.Date(2026, time.July, 27, 10, 0, 0, 0, time.UTC)
	if _, err := store.EnsurePartitions(t.Context(), now, run.DefaultPartitionsAhead); err != nil {
		t.Fatalf("EnsurePartitions() error = %v", err)
	}

	for _, retention := range []run.Retention{{}, {Days: -1}, {Days: run.MaxRetentionDays + 1}} {
		if _, err := store.DropExpiredPartitions(t.Context(), retention, now.AddDate(10, 0, 0)); err == nil {
			t.Fatalf("DropExpiredPartitions(%+v) = nil error, want a rejection", retention)
		}
	}
	if !relationExists(t, database, "runs_2026_07") {
		t.Fatalf("a rejected retention window dropped a partition")
	}
}

// Dropping a Run partition takes its Observations with it, so raw expiry never leaves
// orphaned detail behind.
func TestDroppingARunPartitionExpiresItsObservations(t *testing.T) {
	database := newIntegrationDatabase(t, true)
	store, slot, now := seedRunSlot(t, database, 220)

	claimed := mustClaim(t, run.ID(testUUID(301)), slot, run.KindScheduled, "worker-a", now.Add(time.Minute))
	if _, err := store.ClaimSlot(t.Context(), claimed, now); err != nil {
		t.Fatalf("ClaimSlot() error = %v", err)
	}
	completed := claimed
	if err := completed.Complete(run.OutcomePassed, now, now.Add(time.Second)); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if err := store.Complete(t.Context(), completed, "worker-a", observationFor(completed, slot), nil); err != nil {
		t.Fatalf("store.Complete() error = %v", err)
	}
	if got := countObservations(t, database); got != 1 {
		t.Fatalf("Observation count = %d, want 1", got)
	}

	farFuture := slot.ScheduledFor.AddDate(0, 4, 0)
	if _, err := store.DropExpiredPartitions(t.Context(), run.DefaultRetention(), farFuture); err != nil {
		t.Fatalf("DropExpiredPartitions() error = %v", err)
	}
	if got := countRuns(t, database); got != 0 {
		t.Fatalf("Run count after expiry = %d, want 0", got)
	}
	if got := countObservations(t, database); got != 0 {
		t.Fatalf("Observation count after expiry = %d, want 0", got)
	}
}

func TestListRunsForMonitorIsNewestFirstAndBounded(t *testing.T) {
	database := newIntegrationDatabase(t, true)
	store, slot, now := seedRunSlot(t, database, 230)

	for index := range 3 {
		current := slot
		current.ScheduledFor = slot.ScheduledFor.Add(time.Duration(index) * time.Minute)
		value := mustClaim(t, run.ID(testUUID(240+index)), current, run.KindScheduled, "worker-a", now.Add(time.Hour))
		if _, err := store.ClaimSlot(t.Context(), value, now); err != nil {
			t.Fatalf("ClaimSlot() %d error = %v", index, err)
		}
	}

	runs, err := store.ListRunsForMonitor(t.Context(), slot.OrganizationID, slot.MonitorID, slot.ScheduledFor, 10)
	if err != nil {
		t.Fatalf("ListRunsForMonitor() error = %v", err)
	}
	if len(runs) != 3 {
		t.Fatalf("ListRunsForMonitor() = %d rows, want 3", len(runs))
	}
	for index := 1; index < len(runs); index++ {
		if runs[index].Slot.ScheduledFor.After(runs[index-1].Slot.ScheduledFor) {
			t.Fatalf("ListRunsForMonitor() is not newest first: %v then %v",
				runs[index-1].Slot.ScheduledFor, runs[index].Slot.ScheduledFor)
		}
	}

	for _, limit := range []int{0, -1, MaxRunPageSize + 1} {
		if _, err := store.ListRunsForMonitor(t.Context(), slot.OrganizationID, slot.MonitorID, slot.ScheduledFor, limit); err == nil {
			t.Fatalf("ListRunsForMonitor(limit %d) = nil error, want a rejection", limit)
		}
	}
}

func TestScopedRunQueryServicePaginatesFiltersAndHidesWrongProject(t *testing.T) {
	database := newIntegrationDatabase(t, true)
	store, slot, now := seedRunSlot(t, database, 240)
	var projectID string
	if err := database.pool.QueryRow(
		t.Context(), "SELECT project_id FROM monitors WHERE id = $1", slot.MonitorID,
	).Scan(&projectID); err != nil {
		t.Fatalf("load seeded Project id: %v", err)
	}
	scope := run.Scope{
		OrganizationID: slot.OrganizationID,
		ProjectID:      projectID,
		MonitorID:      slot.MonitorID,
	}
	service := run.NewQueryService(store)

	values := make([]run.Run, 3)
	for index := range values {
		current := slot
		current.ScheduledFor = slot.ScheduledFor.Add(time.Duration(index) * time.Minute)
		claimed := mustClaim(
			t, run.ID(testUUID(260+index)), current, run.KindScheduled,
			"query-worker", now.Add(time.Hour),
		)
		stored, err := store.ClaimSlot(t.Context(), claimed, now)
		if err != nil {
			t.Fatalf("ClaimSlot() %d error = %v", index, err)
		}
		outcome := run.OutcomePassed
		if index == 1 {
			outcome = run.OutcomeFailed
		}
		if err := stored.Complete(outcome, current.ScheduledFor, current.ScheduledFor.Add(time.Second)); err != nil {
			t.Fatalf("Complete() %d error = %v", index, err)
		}
		if err := store.Complete(
			t.Context(), stored, "query-worker", observationFor(stored, current), nil,
		); err != nil {
			t.Fatalf("store.Complete() %d error = %v", index, err)
		}
		values[index] = stored
	}

	first, found, err := service.List(t.Context(), scope, run.ListQuery{
		NotBefore: slot.ScheduledFor,
		PageSize:  2,
	})
	if err != nil || !found {
		t.Fatalf("first List() = found %v, err %v", found, err)
	}
	if len(first.Runs) != 2 || first.Runs[0].ID != values[2].ID ||
		first.Runs[1].ID != values[1].ID || first.NextCursor == nil {
		t.Fatalf("first List() = %#v", first)
	}

	second, found, err := service.List(t.Context(), scope, run.ListQuery{
		NotBefore: slot.ScheduledFor,
		Cursor:    first.NextCursor,
		PageSize:  2,
	})
	if err != nil || !found || len(second.Runs) != 1 ||
		second.Runs[0].ID != values[0].ID || second.NextCursor != nil {
		t.Fatalf("second List() = %#v, found %v, err %v", second, found, err)
	}

	passed, found, err := service.List(t.Context(), scope, run.ListQuery{
		NotBefore: slot.ScheduledFor,
		PageSize:  run.MaxPageSize,
		Outcome:   run.OutcomePassed,
		Kind:      run.KindScheduled,
		Location:  slot.Location,
	})
	if err != nil || !found || len(passed.Runs) != 2 {
		t.Fatalf("filtered List() = %#v, found %v, err %v", passed, found, err)
	}

	loaded, found, err := service.Get(t.Context(), scope, values[2].ID)
	if err != nil || !found || loaded.ID != values[2].ID {
		t.Fatalf("Get() = %#v, found %v, err %v", loaded, found, err)
	}
	observation, found, err := service.GetObservation(t.Context(), scope, values[2].ID)
	if err != nil || !found || observation.RunID != values[2].ID {
		t.Fatalf("GetObservation() = %#v, found %v, err %v", observation, found, err)
	}

	wrongProject := scope
	wrongProject.ProjectID = testUUID(999)
	if _, found, err := service.List(t.Context(), wrongProject, run.ListQuery{
		NotBefore: slot.ScheduledFor, PageSize: 10,
	}); err != nil || found {
		t.Fatalf("wrong-Project List() = found %v, err %v", found, err)
	}
	if _, found, err := service.Get(t.Context(), wrongProject, values[2].ID); err != nil || found {
		t.Fatalf("wrong-Project Get() = found %v, err %v", found, err)
	}
	if _, found, err := service.GetObservation(
		t.Context(), wrongProject, values[2].ID,
	); err != nil || found {
		t.Fatalf("wrong-Project GetObservation() = found %v, err %v", found, err)
	}
}

// seedRunSlot builds a tenant, a Monitor with one revision, the partitions the Run will land
// in, and the slot identity the tests contend over.
func seedRunSlot(t *testing.T, database *DB, offset int) (*RunStore, run.Slot, time.Time) {
	t.Helper()
	organizationValue, project := seedTenant(t, database, offset, fmt.Sprintf("run-tenant-%d", offset))
	monitorValue := seedMonitor(t, database, offset+5, organizationValue, project, testTime())

	store := database.Runs()
	scheduledFor := time.Date(2026, time.July, 27, 10, 0, 0, 0, time.UTC)
	if _, err := store.EnsurePartitions(t.Context(), scheduledFor, run.DefaultPartitionsAhead); err != nil {
		t.Fatalf("EnsurePartitions() error = %v", err)
	}
	slot := run.Slot{
		OrganizationID: string(organizationValue.ID),
		MonitorID:      string(monitorValue.ID),
		RevisionNumber: 1,
		Location:       "managed-eu-west",
		ScheduledFor:   scheduledFor,
	}
	return store, slot, scheduledFor
}

func mustClaim(t *testing.T, id run.ID, slot run.Slot, kind run.Kind, holder string, leaseEnd time.Time) run.Run {
	t.Helper()
	value, err := run.Claim(id, slot, kind, holder, leaseEnd)
	if err != nil {
		t.Fatalf("run.Claim() error = %v", err)
	}
	return value
}

func observationFor(value run.Run, slot run.Slot) run.Observation {
	return run.Observation{
		RunID:          value.ID,
		ScheduledFor:   slot.ScheduledFor,
		OrganizationID: slot.OrganizationID,
		Duration:       420 * time.Millisecond,
		Phases: run.Phases{
			Connect: 30 * time.Millisecond, TLS: 40 * time.Millisecond, FirstByte: 200 * time.Millisecond,
		},
		HTTP: &run.HTTPDetail{
			StatusCode: 200, Protocol: "HTTP/2.0", RedirectCount: 1, BodyBytes: 4096,
			TLS: &run.TLSDetail{
				Version: "TLS 1.3", CipherSuite: "TLS_AES_128_GCM_SHA256",
				CertificateExpiresAt: time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC),
			},
		},
	}
}

func outboxEntry(t *testing.T, organizationID, topic string) run.OutboxEntry {
	t.Helper()
	return run.OutboxEntry{
		ID:             run.ID(testUUID(880)),
		OrganizationID: organizationID,
		Topic:          topic,
		Payload:        json.RawMessage(`{"example":true}`),
		CreatedAt:      testTime(),
	}
}

func countRuns(t *testing.T, database *DB) int {
	t.Helper()
	return countRows(t, database, "SELECT count(*) FROM runs")
}

func countObservations(t *testing.T, database *DB) int {
	t.Helper()
	return countRows(t, database, "SELECT count(*) FROM observations")
}

func countOutboxEntries(t *testing.T, database *DB) int {
	t.Helper()
	return countRows(t, database, "SELECT count(*) FROM outbox_entries")
}

func countRows(t *testing.T, database *DB, query string) int {
	t.Helper()
	var count int
	if err := database.pool.QueryRow(t.Context(), query).Scan(&count); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return count
}

// The scheduler's whole tick is this one read (ADR 0026), so it must return exactly the
// Monitors that should run, joined to the revision a new Run would execute.
func TestListSchedulableReturnsActiveMonitorsWithTheirLatestRevision(t *testing.T) {
	database := newIntegrationDatabase(t, true)
	store := database.Runs()
	organizationValue, project := seedTenant(t, database, 400, "schedulable-tenant")

	// Active with two revisions: the latest one is what a new Run executes.
	active := seedMonitor(t, database, 410, organizationValue, project, testTime())
	active = appendTestRevision(t, database, active, 1, `{"url":"https://one.example.test"}`)
	active = appendTestRevision(t, database, active, 2, `{"url":"https://two.example.test"}`)
	activateMonitor(t, database, &active)

	// A Draft Monitor has configuration but has never been activated.
	draft := seedMonitor(t, database, 420, organizationValue, project, testTime())
	appendTestRevision(t, database, draft, 1, `{"url":"https://draft.example.test"}`)

	// A Paused Monitor is deliberately not running.
	paused := seedMonitor(t, database, 430, organizationValue, project, testTime())
	paused = appendTestRevision(t, database, paused, 1, `{"url":"https://paused.example.test"}`)
	activateMonitor(t, database, &paused)
	transitionMonitor(t, database, &paused, monitor.StatePaused)

	schedulable, err := store.ListSchedulable(t.Context())
	if err != nil {
		t.Fatalf("ListSchedulable() error = %v", err)
	}
	if len(schedulable) != 1 {
		t.Fatalf("ListSchedulable() = %d Monitors, want only the active one", len(schedulable))
	}

	value := schedulable[0]
	if value.MonitorID != string(active.ID) || value.OrganizationID != string(organizationValue.ID) {
		t.Fatalf("ListSchedulable()[0] identity = %s/%s, want %s/%s",
			value.OrganizationID, value.MonitorID, organizationValue.ID, active.ID)
	}
	if value.RevisionNumber != 2 {
		t.Fatalf("revision = %d, want the latest revision 2", value.RevisionNumber)
	}
	if !bytes.Contains(value.CheckConfiguration, []byte("two.example.test")) {
		t.Fatalf("configuration = %s, want the latest revision's document", value.CheckConfiguration)
	}
	if value.CheckType != "http" || value.CheckSchemaVersion != 1 {
		t.Fatalf("check type/version = %s/%d, want http/1", value.CheckType, value.CheckSchemaVersion)
	}
	if value.Interval != time.Duration(monitor.DefaultIntervalSeconds)*time.Second {
		t.Fatalf("interval = %v, want the Monitor's %d seconds", value.Interval, monitor.DefaultIntervalSeconds)
	}
	if err := value.Validate(); err != nil {
		t.Fatalf("ListSchedulable() returned an unschedulable projection: %v", err)
	}
}

// The interval round-trips through persistence, and changing it is an ordinary update that
// leaves the revision counter alone (ADR 0026).
func TestMonitorIntervalRoundTripsAndChangesWithoutARevision(t *testing.T) {
	database := newIntegrationDatabase(t, true)
	organizationValue, project := seedTenant(t, database, 440, "interval-tenant")
	created, err := monitor.NewMonitor(
		monitor.ID(testUUID(450)), string(organizationValue.ID), string(project.ID),
		"Interval Monitor", "http", 300, testTime(),
	)
	if err != nil {
		t.Fatalf("NewMonitor() error = %v", err)
	}
	if err := database.Monitors().CreateMonitor(t.Context(), created); err != nil {
		t.Fatalf("CreateMonitor() error = %v", err)
	}

	stored, found, err := database.Monitors().FindMonitor(t.Context(), monitor.Scope{
		OrganizationID: string(organizationValue.ID), ProjectID: string(project.ID), MonitorID: created.ID,
	})
	if err != nil || !found {
		t.Fatalf("FindMonitor() = found %v (err %v), want the Monitor", found, err)
	}
	if stored.IntervalSeconds != 300 {
		t.Fatalf("stored interval = %d, want 300", stored.IntervalSeconds)
	}

	changed := stored
	if err := changed.ChangeInterval(600, testTime().Add(time.Minute)); err != nil {
		t.Fatalf("ChangeInterval() error = %v", err)
	}
	if err := database.Monitors().UpdateMonitor(t.Context(), changed, stored.Version); err != nil {
		t.Fatalf("UpdateMonitor() error = %v", err)
	}
	reread, _, err := database.Monitors().FindMonitor(t.Context(), monitor.Scope{
		OrganizationID: string(organizationValue.ID), ProjectID: string(project.ID), MonitorID: created.ID,
	})
	if err != nil {
		t.Fatalf("FindMonitor() error = %v", err)
	}
	if reread.IntervalSeconds != 600 || reread.LatestRevisionNumber != 0 {
		t.Fatalf("after ChangeInterval: interval %d, revisions %d; want 600 and 0",
			reread.IntervalSeconds, reread.LatestRevisionNumber)
	}
}

// The check constraint is what keeps a hand-written row from scheduling a Monitor every
// second, whatever the application validator did.
func TestMonitorIntervalCheckConstraint(t *testing.T) {
	database := newIntegrationDatabase(t, true)
	organizationValue, project := seedTenant(t, database, 460, "interval-constraint-tenant")
	for _, seconds := range []int{29, 0, -1, 86401} {
		_, err := database.pool.Exec(t.Context(), `
INSERT INTO monitors (
    id, organization_id, project_id, name, check_type, state, interval_seconds,
    latest_revision_number, created_at, updated_at
) VALUES ($1, $2, $3, 'Bad Interval', 'http', 'draft', $4, 0, $5, $5)`,
			testUUID(470+seconds), string(organizationValue.ID), string(project.ID), seconds, testTime())
		requireConstraint(t, err, checkViolation, "ck_monitors_interval_seconds")
	}
}

// testRevisionIDs hands out distinct revision identifiers. Deriving them from the Monitor
// and revision number collided across Monitors that differ only in their number.
var testRevisionIDs = func() *atomic.Int64 {
	counter := &atomic.Int64{}
	counter.Store(5000)
	return counter
}()

func appendTestRevision(t *testing.T, database *DB, value monitor.Monitor, number int, configuration string) monitor.Monitor {
	t.Helper()
	revision, err := monitor.NewRevision(
		monitor.RevisionID(testUUID(int(testRevisionIDs.Add(1)))), value.ID, value.OrganizationID,
		number, value.CheckType, 1, json.RawMessage(configuration), testTime(),
	)
	if err != nil {
		t.Fatalf("NewRevision() error = %v", err)
	}
	current := reloadMonitor(t, database, value)
	advanced := current
	if err := advanced.RecordRevision(number, testTime()); err != nil {
		t.Fatalf("RecordRevision() error = %v", err)
	}
	if err := database.Monitors().AppendRevision(t.Context(), advanced, revision, current.Version); err != nil {
		t.Fatalf("AppendRevision() error = %v", err)
	}
	return reloadMonitor(t, database, advanced)
}

func activateMonitor(t *testing.T, database *DB, value *monitor.Monitor) {
	t.Helper()
	transitionMonitor(t, database, value, monitor.StateActive)
}

func transitionMonitor(t *testing.T, database *DB, value *monitor.Monitor, target monitor.State) {
	t.Helper()
	current := reloadMonitor(t, database, *value)
	updated := current
	if err := updated.TransitionTo(target, testTime()); err != nil {
		t.Fatalf("TransitionTo(%s) error = %v", target, err)
	}
	if err := database.Monitors().UpdateMonitor(t.Context(), updated, current.Version); err != nil {
		t.Fatalf("UpdateMonitor() error = %v", err)
	}
	*value = reloadMonitor(t, database, updated)
}

// reloadMonitor rereads a Monitor so its xmin matches the row, which the in-memory value
// from a constructor never does.
func reloadMonitor(t *testing.T, database *DB, value monitor.Monitor) monitor.Monitor {
	t.Helper()
	stored, found, err := database.Monitors().FindMonitor(t.Context(), monitor.Scope{
		OrganizationID: value.OrganizationID, ProjectID: value.ProjectID, MonitorID: value.ID,
	})
	if err != nil || !found {
		t.Fatalf("reload Monitor = found %v (err %v)", found, err)
	}
	return stored
}
