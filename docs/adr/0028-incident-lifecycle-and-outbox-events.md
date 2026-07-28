# 0028: Incident Lifecycle and Outbox Event Semantics

- Status: Proposed
- Date: 2026-07-28
- Clarifies: [ADR 0005](0005-postgresql-leases-and-outbox.md), [ADR 0021](0021-run-observation-retention-and-scheduling.md), [ADR 0025](0025-run-storage-schema-and-lease-placement.md)
- Related: [ADR 0009](0009-tenant-scope-default-project-and-telemetry.md), [ADR 0027](0027-health-evaluation-confirmation-and-quorum.md)

This is a proposal, not implementation authority. Its status must change to Accepted
before code, migrations, OpenAPI, or event contracts depend on it. Alert routing and
Delivery Attempt semantics remain a separate future ADR.

## Context

ADR 0005 chose a PostgreSQL outbox. ADR 0021 requires effects following a committed Run
to be written transactionally, delivered at least once, and consumed idempotently on the
outbox identifier. ADR 0025 adds an Organization-scoped queue row with bounded topic and
JSON payload, attempt count, creation time, and availability time.

Those records deliberately define no topic, producer, claimant, retry schedule, dead-letter
behavior, ordering, or consumer. The scheduler always passes an empty outbox batch and
RecordSkipped cannot create one.

There is no Incident type, table, use case, endpoint, OpenAPI schema, or event contract.
The blueprint suggests Open, Acknowledged, and Resolved but leaves undecided:

1. Which health transition opens and resolves an Incident.
2. Whether the first Incident owns one Monitor or correlates several.
3. The meaning and effect of acknowledgement and manual resolution.
4. Reopening versus a new Incident after repeated failure.
5. Effects of pause, archive, maintenance, policy, and revision changes.
6. Timeline facts and retention after raw Runs expire.
7. Topic vocabulary, schema version, payload, visibility, ordering, and fan-out.
8. Claim leases, retries, poison messages, dead letters, shutdown, and observability.

## Proposed Decision

Everything below is a candidate answer for owner review.

### Start with one automatic Incident per Monitor

An Incident carries Organization, Project, and Monitor identity. Phase 1 supports one
automatic Incident for one Monitor and one evaluated-health episode. Multi-Monitor
correlation, merging, manual Incidents, assignment, notes, public descriptions, and
postmortem links remain additive future behavior rather than empty schema.

At most one unresolved automatic Incident exists per Monitor, enforced by persistence.

| State | Candidate meaning |
| --- | --- |
| open | A qualifying health transition created the Incident and nobody acknowledged it. |
| acknowledged | An authorized actor acknowledged it; health is unchanged. |
| resolved | Recovery ended the episode; this Incident is terminal. |

Candidate transitions are:

- no active Incident plus health becoming Down creates open;
- open may become acknowledged;
- open or acknowledged plus health becoming Healthy becomes resolved.

Degraded and Unknown neither open nor resolve. A later Down after resolution creates a new
Incident; resolved is never reopened. Re-delivery of one health transition creates neither
a duplicate Incident nor duplicate timeline entry.

Acknowledgement stores actor and UTC time and appends a timeline entry. It does not change
health, resolve the Incident, suppress evaluation, or imply delivery.

The proposed first slice has no manual resolution while health remains Down. A later audited
health override may permit it, but an Incident endpoint must not claim recovery while the
evaluator disagrees.

### Monitor and maintenance state do not rewrite history

Pause, archive, revision change, and policy change never delete an Incident or timeline.

- Maintenance may suppress future Alert creation or delivery, but never health evaluation
  or Incident transitions.
- Pause stops new Runs and confirmation but does not invent recovery.
- Archive prevents new Incidents but does not silently resolve an active one.
- Revision and policy changes are timeline facts; ADR 0027 decides their health effect.

Archive treatment remains an acceptance blocker because retaining an active Incident while
excluding manual resolution needs an explicit operator path.

### Keep an append-only bounded timeline

Creation, acknowledgement, and resolution append immutable timeline entries in the same
transaction as state change. Health entries name the transition, old and new health, policy
version, bounded quorum counts, and causal Run identifiers with scheduled_for partition keys.

Do not copy raw Observation content into the Incident. Retain the bounded transition summary
with the Incident so history remains intelligible after Run partitions expire. Incident
retention and authorization remain Organization-scoped and separate from raw retention.

### Use versioned internal fact topics

Candidate initial topics are:

| Topic | Producer transaction | Owning consumer |
| --- | --- | --- |
| run.recorded.v1 | completed Run plus Observation, or skipped Run | health evaluator |
| health.transitioned.v1 | health state and evidence transition | Incident projector |
| incident.opened.v1 | Incident and opening timeline entry | future Alert evaluator |
| incident.acknowledged.v1 | acknowledgement and timeline entry | future audit and Alert policy |
| incident.resolved.v1 | resolution and timeline entry | future recovery Alert evaluator |

The scheduler would produce run.recorded.v1 instead of an empty batch. RecordSkipped would
gain the same transaction boundary. An outcome-null abandoned claim is not terminal and
emits no recorded event.

Each topic version has one exact payload schema beside its owning producer. Candidate common
fields are eventId equal to outbox id, organizationId, occurredAt, aggregate type and id,
aggregate version, optional causationId, and topic-specific identifiers needed to load the
authoritative row.

Run events include Run id, Monitor id, revision, location, scheduledFor, kind, and outcome,
but no Observation detail or target data. Health events include Monitor id, transition id
and version, old and new states, and policy version. Incident events include Incident id,
Monitor id, and Incident version.

Table and payload Organization ids must match. Consumers load rows with Organization scope
rather than trusting JSON identifiers.

These topics are internal durability contracts, not public webhooks. External publication
requires its own authenticated, versioned, redaction-reviewed public event contract.
Internal topics still need versions because rows may survive an upgrade.

### Give each row one owning consumer

One row has exactly one topic owner. Success deletes it. If committed state must drive
another independent consumer, the producer writes another purpose-specific entry in the
same transaction. Do not make deletion depend on implicit in-memory subscribers.

### Make delivery bounded and observable

Claim available entries in bounded batches with PostgreSQL row locking without waiting
behind another worker. Process outside the claim transaction under a bounded lease so a
crash is reclaimable. When handler state shares PostgreSQL, commit its idempotent state
change and outbox deletion atomically.

Failure increments attempts, records a bounded stable failure code and next available_at,
and releases the claim. Retry uses exponential backoff with bounded jitter and operator
ceiling. Exhausting the accepted attempt limit moves the row to a dead-letter table in one
transaction; never silently delete it.

Dead letters retain Organization, topic, payload, attempts, creation time, final failure
code, and dead-letter time. Expose bounded metrics and structured logs, without tenant or
payload data in metric labels.

Shutdown stops claiming, cancels handlers, and releases claims within a bound. Exact lease,
batch, retry, jitter, attempt, and dead-letter values are acceptance blockers, not defaults
selected in code.

### Promise no queue-wide ordering

The outbox promises at-least-once delivery, not global or Organization FIFO. UUIDv7 and
timestamps aid inspection but are not concurrency sequence.

Each mutable aggregate carries a version. A consumer serializes that aggregate, treats stale
versions as handled, and defers a future version until its predecessor exists. Health also
orders cohorts by scheduled_for because completion order is not evidence order.

## Acceptance Blockers

Before acceptance, record owner choices for:

1. Down-only opening and Healthy-only resolution.
2. Whether Phase 1 excludes manual Incident creation and manual resolution.
3. Active-Incident behavior when its Monitor is archived.
4. Whether maintenance suppresses Alert creation, delivery, or both, while never suppressing
   health or Incident history.
5. Incident and timeline retention, including future Organization deletion.
6. Final topic names and exact version-1 payload schemas.
7. Claim lease, batch, concurrency, retry base and ceiling, jitter, maximum attempts, and
   dead-letter retention.
8. The retained idempotency record after successful deletion and its retention.
9. Handling and timeout for a future aggregate version missing its predecessor.
10. The stable outbox failure-code catalog.

## Consequences

- Incident state follows health without conflating acknowledgement and recovery.
- One unresolved Incident per Monitor gives Phase 1 a clear idempotency boundary.
- Bounded summaries survive raw measurement retention.
- Outbox topics gain versioned schemas and one owner.
- Reliable consumption needs lease and dead-letter schema beyond the current table.
- Alert and Delivery Attempt semantics remain blocked on their own ADR.
