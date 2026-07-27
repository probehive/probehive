# 0021: Run and Observation Model, Retention, and Scheduling Leases

- Status: Accepted
- Date: 2026-07-27
- Clarifies: [ADR 0005](0005-postgresql-leases-and-outbox.md), [ADR 0014](0014-monitor-lifecycle-and-revision-immutability.md)

## Context

ADR 0014 completed the ownership path down to Monitor Revision and stopped there. Runs and Observations are the first high-volume tables the product will hold, and the blueprint puts "PostgreSQL retention and partitioning design" in Phase 0, where it remains undone. Its own risk list names unbounded raw observation retention.

ADR 0005 chose PostgreSQL leases and an outbox but left the semantics open, and the workspace rules require exact retry and transition semantics to be recorded before the implementation they affect. Writing the Runs table without answering retention first is the expensive mistake here: a partitioning scheme cannot be retrofitted onto a large table without downtime, and by the time it hurts, the table is large by definition.

## Decision

### Run

A Run is one Monitor Revision executed once from one Probe Location. It carries Organization identity explicitly (ADR 0009), the Monitor and the exact revision number it executed, the location, the instant it was scheduled for, the instants it started and finished, and one outcome.

Outcomes are exactly `passed`, `failed`, `errored`, `timedout`, `cancelled`, and `skipped`. The blueprint requires an assertion failure, a timeout, invalid configuration, a scheduler delay, an agent outage, a cancellation, and an internal error to stay distinguishable, so a Run never collapses them: `failed` means the target answered and an assertion rejected it, `errored` means execution itself failed, and `skipped` means the scheduler deliberately did not execute this slot.

A Run also records whether it was an ordinary scheduled execution, a confirmation execution, or a manual execution. ADR's Phase 1 requirement that confirmation runs be explicitly identified is a property of the Run, not something inferred from timing.

### Idempotent run identity

The identity of a scheduled Run is `(monitor_id, revision_number, location, scheduled_for)`. A unique index over those columns makes duplicate execution of one slot unrepresentable, so a retry, a duplicate lease delivery, or a restarted worker cannot produce two rows for the same slot. Manual runs carry a distinct discriminator and are exempt.

The primary key stays a UUIDv7 for ordering and for external reference; the natural key is the uniqueness rule, not the identifier.

### Observation

An Observation is the bounded detail of one Run: phase timings, assertion results, protocol details, and diagnostics. It is one row per Run, carrying Organization identity, and is partitioned and expired on the same schedule as its Run.

Every Observation is bounded before it is stored: response bodies are truncated to an operator ceiling, headers are capped, and deterministic redaction runs before persistence, never after. An Observation is not a place where an unbounded provider response comes to rest.

### Partitioning and retention

`runs` and `observations` are range-partitioned monthly on `started_at`. Retention is expressed in whole days, is operator-configurable, and defaults to a value small enough that an unattended installation cannot fill its disk with raw rows.

Expiry drops whole partitions rather than deleting rows. A partition is dropped once its entire range is older than the retention window, which makes reclaiming space an `O(1)` catalogue operation instead of a bulk delete that leaves bloat behind. A maintenance job creates partitions ahead of time; a missing future partition is an operational alert, not an insert failure discovered at midnight.

Long-term rollups are separate aggregate tables with their own longer retention. They are not partitions of the raw tables, and raw expiry never waits on them.

Deleting an Organization is still not an operation (ADR 0016). When it becomes one, it deletes through these tables rather than relying on partition expiry.

### Scheduling leases

Scheduling uses PostgreSQL leases as ADR 0005 chose, with these semantics fixed:

- A due slot is claimed by a worker taking a lease with an explicit expiry. Claiming uses `FOR UPDATE SKIP LOCKED` so workers do not queue behind each other.
- A lease has a bounded duration derived from the effective execution ceiling plus a margin, and the holder renews it while executing. A lease that expires is reclaimable by any worker; the original holder discovers it lost the lease when it tries to record its result, and discards that result rather than writing it.
- The run-identity uniqueness rule above is the backstop: if a lease is lost and both workers finish, only one row exists.
- Misfire policy after downtime is *skip and record*. A slot whose scheduled instant is older than one interval is recorded as a `skipped` Run rather than executed late. Running a backlog of stale checks produces alerts about the past, and silently dropping them makes gaps invisible.
- Cancellation and graceful shutdown release the lease explicitly, so a restart does not wait for expiry.

### Outbox

Side effects that must follow a committed Run — incident evaluation, notification delivery, external events — are written to an outbox in the same transaction as the Run, never emitted from within it. Consumers are at-least-once and must be idempotent on the outbox entry identifier. This is the delivery boundary; the Alert and Delivery Attempt model itself is a separate future decision.

### Not decided here

Health evaluation, confirmation and quorum rules, flapping suppression, incident lifecycle, alert routing, and Delivery Attempt semantics each need their own record before implementation, as the workspace rules already require. This decision fixes only what has to exist before the first row is written.

## Consequences

- The first high-volume tables arrive partitioned, so retention is a configuration value rather than a future migration against a large table.
- The distinctions the product promises between failure kinds are enforced by the schema instead of reconstructed later from timings.
- Duplicate execution is impossible at the storage layer, which means lease correctness is a performance concern rather than a correctness one.
- Skip-and-record makes downtime visible in the data instead of producing a burst of late executions.
- A maintenance job becomes a required operational component, and an installation that never runs it will eventually fail to insert.
- Rollups are extra tables with their own lifecycle, which is more schema than a single retention window would need.
