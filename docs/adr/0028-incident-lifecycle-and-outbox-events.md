# 0028: Incident Lifecycle and Outbox Event Semantics

- Status: Accepted
- Date: 2026-07-28
- Clarifies: [ADR 0005](0005-postgresql-leases-and-outbox.md), [ADR 0021](0021-run-observation-retention-and-scheduling.md), [ADR 0025](0025-run-storage-schema-and-lease-placement.md)
- Related: [ADR 0009](0009-tenant-scope-default-project-and-telemetry.md), [ADR 0027](0027-health-evaluation-confirmation-and-quorum.md)

Alert routing and Delivery Attempt semantics remain a separate future ADR.

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

## Decision

### Start with one automatic Incident per Monitor

An Incident carries Organization, Project, and Monitor identity. Phase 1 supports one
automatic Incident for one Monitor and one evaluated-health episode. Multi-Monitor
correlation, merging, manual Incidents, assignment, notes, public descriptions, and
postmortem links remain additive future behavior rather than empty schema.

At most one unresolved automatic Incident exists per Monitor, enforced by persistence.

| State | Meaning |
| --- | --- |
| open | A qualifying health transition created the Incident and nobody acknowledged it. |
| acknowledged | An authorized actor acknowledged it; health is unchanged. |
| resolved | Recovery ended the episode; this Incident is terminal. |

The only lifecycle transitions are:

- no active Incident plus health becoming Down creates open;
- open may become acknowledged;
- open or acknowledged plus health becoming Healthy becomes resolved.

Degraded and Unknown neither open nor resolve. A later Down after resolution creates a new
Incident; resolved is never reopened. Re-delivery of one health transition creates neither
a duplicate Incident nor duplicate timeline entry.

Acknowledgement stores actor and UTC time and appends a timeline entry. It does not change
health, resolve the Incident, suppress evaluation, or imply delivery.

Phase 1 has no manual Incident creation or manual resolution. A later audited health
override may permit it, but an Incident endpoint must not claim recovery while the evaluator
disagrees.

### Monitor and maintenance state do not rewrite history

Pause, archive, revision change, and policy change never delete an Incident or timeline.

- Maintenance may suppress future Alert creation or delivery, but never health evaluation
  or Incident transitions.
- Pause stops new Runs and confirmation but does not invent recovery.
- Archive prevents new Incidents but does not silently resolve an active one.
- Revision and policy changes are timeline facts; ADR 0027 decides their health effect.

An Incident that is unresolved when its Monitor is archived remains open or acknowledged.
It can still be acknowledged and queried. It resolves only if the Monitor is reactivated and
later becomes Healthy. This is deliberately honest: permanent archive may leave an
unresolved historical episode, and the API reports that fact instead of inventing recovery.

Maintenance never suppresses health evaluation or Incident creation, acknowledgement,
resolution, or history. Future Alert policy may suppress Alert creation, delivery, or both,
but that behavior is outside this ADR and must be explicit.

### Keep an append-only bounded timeline

Creation, acknowledgement, and resolution append immutable timeline entries in the same
transaction as state change. Health entries name the transition, old and new health, policy
version, bounded quorum counts, and causal Run identifiers with `scheduled_for` partition
keys.

Do not copy raw Observation content into the Incident. Retain the bounded transition summary
with the Incident so history remains intelligible after Run partitions expire. Incident
retention and authorization remain Organization-scoped and separate from raw retention.

Incidents and timeline entries have no time-based Phase 1 deletion. They are removed only by
the existing tenant-scoped Monitor or Organization cascade. A future retention setting and
Organization deletion workflow must preserve the same tenant boundary.

### Use versioned internal fact topics

Phase 1 defines only topics that have a current owning consumer:

| Topic | Producer transaction | Owning consumer |
| --- | --- | --- |
| `run.recorded.v1` | completed Run plus Observation, or skipped Run | health evaluator |
| `run.confirmation.requested.v1` | pending health candidate and request | confirmation executor |
| `health.transitioned.v1` | health state and evidence transition | Incident projector |

The scheduler produces `run.recorded.v1` instead of an empty batch. `RecordSkipped` gains
the same transaction boundary. An outcome-null abandoned claim is not terminal and emits no
recorded event. Incident topics are not produced until an Alert or audit consumer exists;
durable Incident and timeline rows are the current contract.

Each payload uses camelCase JSON. Its common fields are `eventId` equal to the outbox row
id, `organizationId`, `occurredAt`, `aggregateType`, `aggregateId`,
`aggregateVersion`, and optional `causationId`.

- `run.recorded.v1` adds `runId`, `monitorId`, `revisionNumber`, `location`,
  `scheduledFor`, `kind`, and `outcome`. Its aggregate is the Run at version 1.
- `run.confirmation.requested.v1` adds `candidateId`, `monitorId`,
  `revisionNumber`, `location`, `triggeringRunId`, `triggeringScheduledFor`,
  `requestedFor`, `expectedEvidence` (`passing` or `failing`), and
  `policyVersion`. Its aggregate is the candidate at version 1 and its causation is the
  triggering `run.recorded.v1` event.
- `health.transitioned.v1` adds `transitionId`, `monitorId`, `projectId`,
  `oldState`, `newState`, and `policyVersion`. Its aggregate is the Monitor health and
  its aggregate version is the health transition version; its causation is the consumed Run
  event.

Timestamps use RFC 3339 with UTC offsets and integers are JSON numbers. Version 1 rejects
unknown required enum values but ignores additional object members so a producer may add
optional metadata without changing meaning.

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
change and processed-event marker atomically; deletion may follow because re-delivery then
observes the processed marker and is a no-op.

Failure increments attempts, records a bounded stable failure code and next `available_at`,
and releases the claim. Retry uses exponential backoff with bounded jitter and operator
ceiling. Exhausting the accepted attempt limit moves the row to a dead-letter table in one
transaction; never silently delete it.

Dead letters retain Organization, topic, payload, attempts, creation time, final failure
code, and dead-letter time. Expose bounded metrics and structured logs, without tenant or
payload data in metric labels.

Shutdown stops claiming, cancels handlers, and lets leases expire within the 60-second bound.

### Promise no queue-wide ordering

The outbox promises at-least-once delivery, not global or Organization FIFO. UUIDv7 and
timestamps aid inspection but are not concurrency sequence.

Each mutable aggregate carries a version. A consumer serializes that aggregate, treats stale
versions as handled, and defers a future version until its predecessor exists. Health orders
current state by the arrival rule in ADR 0027.

### Fix the Phase 1 delivery bounds

- Claim lease: 60 seconds.
- Claim batch: at most 32 rows.
- Handler concurrency: at most 4.
- Retry: exponential from 1 second, capped at 5 minutes. Attempt N uses
  `min(5m, 1s * 2^(N-1))` plus deterministic non-negative jitter derived from event id and
  attempt, bounded to 20 percent of the base delay and never beyond the 5-minute cap.
- Maximum attempts: 12, counting the first handler call.
- Dead-letter retention: 30 days.
- Successful processed-event retention: 30 days.
- Future aggregate-version gap timeout: 15 minutes from the first observation of the gap,
  after which the event is dead-lettered.

The stable dispatcher failure codes are `outbox.topic.unknown`,
`outbox.payload.invalid`, `outbox.organization.mismatch`,
`outbox.aggregate.versionGap`, `outbox.handler.failed`, and
`outbox.handler.cancelled`. A lost claim is normal at-least-once behavior and is not a
failure code. Invalid payload, unknown topic, and Organization mismatch are permanent and go
directly to dead letter; handler failures and cancellations consume the retry budget.

Processed ids and dead letters are tenant-scoped. The maintenance pass deletes processed ids
and dead letters older than their retention windows; it never deletes a live queue row.

## Consequences

- Incident state follows health without conflating acknowledgement and recovery.
- One unresolved Incident per Monitor gives Phase 1 a clear idempotency boundary.
- Bounded summaries survive raw measurement retention.
- Outbox topics gain versioned schemas and one owner.
- Reliable consumption needs lease and dead-letter schema beyond the current table.
- Delivery timing is an operational contract rather than a hidden implementation default.
- Alert and Delivery Attempt semantics remain blocked on their own ADR.
