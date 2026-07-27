# 0025: Run Storage Schema, Partition Key, and Lease Placement

- Status: Accepted
- Date: 2026-07-27
- Amends: [ADR 0021](0021-run-observation-retention-and-scheduling.md)
- Related: [ADR 0005](0005-postgresql-leases-and-outbox.md), [ADR 0009](0009-tenant-scope-default-project-and-telemetry.md), [ADR 0024](0024-http-check-execution-and-observation-content.md)

## Context

ADR 0021 fixed the Run model, the idempotent run identity, monthly range partitioning,
partition-drop retention, lease semantics, and the outbox before the first row was written.
Writing the first row found that three of those decisions cannot all hold at once, and left
several questions a reader should not have to reconstruct from the DDL.

ADR 0021 chose `started_at` as the partition key. Two of its own decisions contradict that
choice, and both were reproduced against PostgreSQL 17 rather than argued from memory:

1. The idempotent run identity is a unique index over `(monitor_id, revision_number,
   location, scheduled_for)`. PostgreSQL requires a unique constraint on a partitioned table
   to include every partition-key column, and rejects the index outright: `unique constraint
   on partitioned table must include all partitioning columns`. Adding `started_at` to the
   index would satisfy PostgreSQL and destroy the guarantee, because two workers finishing
   the same slot at different instants would both be accepted.
2. `skipped` is an outcome for a slot the scheduler deliberately did not execute. A skipped
   Run never started, so its `started_at` is null, and a range-partitioned table has no
   partition for a null key: `no partition of relation "runs" found for row`. Skip-and-record
   would fail at exactly the moment it exists to serve — after downtime.

The remaining questions were open rather than contradictory: where the lease lives when there
is no separate queue table, how a Run that has been claimed but not yet finished is
represented, what an Observation row holds now that ADR 0024 has fixed its content, whether
the outbox is partitioned, and how partitions are created and dropped.

## Decision

### `runs` and `observations` are partitioned by `scheduled_for`

Both tables are range-partitioned by month on `scheduled_for`, not `started_at`. It is
`NOT NULL` for every Run: a scheduled or confirmation Run has the instant its slot was due,
and a manual Run has the instant it was requested.

This restores both of ADR 0021's decisions. The idempotency index becomes legal because the
partition key is one of its columns, and a skipped Run has a non-null partition key because
the slot existed even though the execution did not.

Retention is unaffected in intent: a partition still covers one month and is still dropped
whole. It shifts by the delay between a slot being due and its execution starting, which is
seconds under normal operation and bounded by the misfire policy after downtime.

`started_at` and `finished_at` become nullable, which they had to be regardless — a skipped
Run has neither.

### A claimed Run has no outcome yet

The lease lives on the Run row. There is no separate queue table, because the slot identity
that the lease protects is already a unique index on `runs`, and a second table holding the
same key would be a second thing to keep consistent with it.

A Run therefore has three representable states, distinguished by two nullable groups:

| State | `outcome` | `lease_expires_at` |
| --- | --- | --- |
| Claimed, executing | null | set |
| Finished | set | null |
| Skipped | `skipped` | null |

A check constraint pins that a Run is leased if and only if it has no outcome, so "in flight"
cannot be spelled two ways. Claiming a slot is the insert; it is what makes the slot
unavailable, so no separate reservation step exists to leave behind.

Claiming reclaims an expired lease through `ON CONFLICT ... DO UPDATE` guarded by the
expiry, which is the storage-layer half of ADR 0021's rule that an expired lease is
reclaimable by any worker. Recording a result requires the recorder to still hold the lease;
the update matches on the holder token and affects no rows when the lease was taken, which is
how the original holder discovers it must discard its result rather than write it.

A Run whose worker died and whose lease expired with nobody reclaiming it stays outcome-null
until its partition is dropped. That is deliberate: it is the honest record that the slot was
claimed and never finished, and inventing an outcome for it would be inventing a measurement.

### Observation columns

An Observation is one row per Run carrying Organization identity, sharing the Run's partition
key, and referencing it by the composite `(run_id, scheduled_for)` foreign key that
partitioning requires.

The columns are exactly ADR 0024's content: the failure code and outbound denial class, the
monotonic elapsed duration, the three phase timings, and a nullable HTTP detail group with a
nullable TLS detail group inside it. Durations are stored as microsecond integers rather than
`interval`, because they are measured monotonically as integers and comparing them is
arithmetic, not calendar arithmetic.

There is no free-text column, no header column, and no body column. ADR 0024 made an
Observation safe by construction by giving it nothing to redact, and a schema with nowhere to
put target-supplied text is what keeps that true when a later change is tempted.

The HTTP detail columns belong to the first check type. A second check type decides for
itself whether it adds a column group or a bounded document; it does not get to widen this
one by reinterpreting its columns.

### The outbox is a queue, not a record

The outbox is not partitioned and has no retention window. It is drained and deleted, so
partitioning it would be machinery for a table that is meant to stay near-empty. Entries
carry Organization identity, a topic, a bounded JSON payload, a creation instant, and an
attempt counter; consumers are at-least-once and idempotent on the entry identifier, as
ADR 0021 requires.

Completing a Run writes the Run, its Observation, and any outbox entries the caller supplies
in one transaction. No event topic is defined here and nothing currently supplies an entry:
incident evaluation and alert delivery are undecided, and inventing a topic before its
consumer exists would publish a contract nothing has agreed to. What this decision fixes is
that the entries are written transactionally with the Run and never emitted from inside it.

### Partition maintenance

Partitions are named `<table>_<year>_<month>`, are created ahead of time for the current
month and a bounded number of following months, and are dropped whole once the entire range
they cover is older than the retention window.

There is no default partition. A default partition would turn ADR 0021's operational alert
into silent misfiling, and it cannot be split back out later without moving rows.

Maintenance derives a partition's range from its name and ignores any partition whose name it
does not recognise. An operator who attached a partition by hand keeps it; the maintenance job
drops only what it created. Creation and dropping hold the same style of advisory lock the
migration runner uses, so two workers running maintenance concurrently do not race.

An expiring partition is detached from its parent before it is dropped, and the two tables are
expired in reverse dependency order — observations first, then runs. This is not tidiness: the
foreign key on the `observations` parent depends on *every* `runs` partition, so PostgreSQL
refuses to drop an attached `runs` partition while any observations partition remains, whatever
order the months are processed in. `DROP ... CASCADE` would also succeed and would remove
whatever else happened to depend on the partition, which is not a power a scheduled job should
hold. Detaching takes a brief exclusive lock on the parent, so expiry is a short write-blocking
operation rather than a free one.

Because a partition is only dropped once its whole range has aged out, effective retention is
the configured window plus up to one month. Retention is a floor on what is kept, not a
ceiling, and an operator sizing a disk should size it for the floor plus a month.

Retention defaults to 30 days, expressed in whole days as ADR 0021 requires.

### `location` has no table yet

`location` is a bounded identifier column naming the Probe Location that executed the Run. The
Probe Location entity, its registry, and its Organization scoping are a later decision; there
is no foreign key, because the table it would reference does not exist and creating it empty
would be the speculative scaffolding ADR 0002 bars.

### `internal/run` restates what it stores

`internal/run` is a feature package and stays standard-library-only, so it cannot import
`internal/probe`. Its Observation type therefore restates the shape `probe.Observation`
produces rather than reusing it, and mapping between them belongs to the composition that
owns both — the embedded worker of ADR 0020, which does not exist yet.

The duplication is the price of the boundary, and it is the same price `internal/monitor`
already pays for not importing `internal/check`'s configuration types. It is bounded: both
shapes are fixed by ADR 0024, and a change to one that is not mirrored in the other fails to
compile at the mapping.

## Consequences

- ADR 0021's idempotent run identity and its `skipped` outcome both work, which neither did
  under the partition key it named.
- Partition pruning now keys off the column dashboards and retention questions actually filter
  by, so "runs in the last hour" prunes without a second predicate.
- A lease is a column group on the row it protects, so there is no queue table to keep
  consistent with the runs table, and no path by which a slot is claimed twice.
- A crashed worker leaves an outcome-null Run behind. Anything counting Runs must decide
  whether it means "in flight" or "abandoned" by looking at the lease expiry, and reporting
  that will need to say so.
- Effective retention exceeds configured retention by up to a month, which is the cost of
  dropping partitions instead of deleting rows.
- Expiry briefly blocks writes while it detaches partitions, so it is a job to schedule at a
  quiet hour rather than one to run in a tight loop.
- The outbox exists with no producer supplying entries. It is a mechanism waiting for the
  decision that gives it a topic, not a queue quietly filling up.
