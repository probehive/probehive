# 0026: Execution Interval Ownership, Slot Derivation, and the Embedded Scheduler

- Status: Accepted
- Date: 2026-07-27
- Amends: [ADR 0021](0021-run-observation-retention-and-scheduling.md), whose claiming mechanism is replaced below
- Clarifies: [ADR 0014](0014-monitor-lifecycle-and-revision-immutability.md), [ADR 0020](0020-check-execution-and-outbound-enforcement.md)
- Related: [ADR 0025](0025-run-storage-schema-and-lease-placement.md)

## Context

ADR 0021 decided what a Run is, how a slot is identified, and how a lease behaves. ADR 0025
built the storage. Nothing can start a Run, because nothing can answer the prior question:
*when is a Monitor due?*

No execution interval exists anywhere in the product. A Monitor has a name, a check type, a
lifecycle state, and revisions. The `http` configuration schema version 1 has a URL, a
method, expected status codes, a timeout, redirect settings, and headers. None of them says
how often the check runs.

Where that field lives is not an implementation detail. It decides whether the scheduler can
select due work without understanding check types, whether changing a check's frequency
rewrites its configuration history, and whether every future check type has to redefine the
same field in its own schema.

Two further questions have to be answered before a scheduler can exist at all, and ADR 0021
answered neither: what determines the exact instant a slot is due, and — with no Probe
Location registry yet — which location a self-hosted installation executes as.

## Decision

### The interval belongs to the Monitor, not to check configuration

The execution interval is a field on the Monitor: a whole number of seconds, from 30 through
86400, defaulting to 60. It is not part of any check configuration schema.

Three reasons, in order of weight:

1. **The scheduler must not know check types.** Selecting due work means reading the interval
   of every active Monitor. If the interval lived inside `check_configuration`, the scheduler
   would have to decode a per-check-type JSON document to learn when to run, which couples the
   scheduler to every check type that will ever exist and makes the interval unindexable. A
   column is a column for every check type.
2. **Every check type needs one.** A field that every schema would have to repeat identically
   belongs above the schemas, not copied into each of them.
3. **It is not check semantics.** ADR 0014 versions a check schema whenever its accepted shape
   or meaning changes. Adding an interval to `http` schema version 1 would force a version 2
   for a field that says nothing about HTTP, and would do it again for every future type.

The interval is validated by the Monitor use case, so an unsupported value is a Monitor
validation failure with a stable ADR 0019 code, not a check-configuration failure.

### Changing the interval does not create a Revision

A Monitor Revision is an immutable snapshot of *check configuration* (ADR 0014). The interval
is scheduling policy, so it is a mutable Monitor field under the same optimistic concurrency
as the name and lifecycle state, and changing it appends nothing to revision history.

The alternative — treating a frequency change as a new revision — would make the revision list
claim the check definition changed when it did not, and would restart the revision-scoped
slot series every time someone doubled a frequency.

The cost is recorded plainly: a Run stores the revision it executed but not the interval that
put it on the clock, so "why did this Monitor run every 30 seconds last Tuesday" is not
answerable from Run history. When that question needs answering it wants an audit record of
Monitor changes, which is a separate decision and would serve renames and state changes too.

### An operator floor, not an operator ceiling

Operator limits elsewhere in the product are ceilings: a user may configure a stricter timeout
but never a looser one (ADR 0024). Frequency inverts that. A *shorter* interval is more load,
so the operator configures a **minimum** interval and user configuration may only be longer.

The platform minimum of 30 seconds is the floor beneath which no operator setting may go. An
installation that raises the minimum does not invalidate existing Monitors configured below
it; the effective interval is the larger of the two, applied at scheduling time. Rewriting
stored Monitors to match a changed operator setting would be a policy change that silently
edits tenant configuration.

### Slots are derived, never stored

The instant a slot is due is computed, not kept in a column:

```text
offset(monitor)      = a stable value in [0, interval) derived from the Monitor identifier
slot(monitor, now)   = the largest instant <= now for which (instant - offset) is a whole
                       multiple of interval, counted from the Unix epoch
```

Every worker computes the same series from the same inputs, so there is no cursor to lose,
duplicate, drift, or reconcile after a restart. The series is reproducible from the Monitor
alone, which is what makes ADR 0021's slot identity meaningful: `scheduled_for` is a value
two workers independently agree on rather than one that whoever wrote it first decided.

The offset exists because aligned slots without one make every Monitor sharing an interval
fire in the same second. Deriving it from the Monitor identifier rather than from randomness
keeps it stable across restarts and identical across workers, which a random spread would not
be.

### Claiming is the insert, and it replaces `FOR UPDATE SKIP LOCKED`

ADR 0021 said claiming uses `FOR UPDATE SKIP LOCKED` so workers do not queue behind each
other. That presumed a table of pending slots to lock rows in. With derived slots there is no
such table: the claim *is* the insert of the Run, and the partial unique index of ADR 0021 is
what makes it exclusive.

The property ADR 0021 wanted is preserved. Two workers attempting the same slot contend only
for the duration of a single-statement insert, and the loser is told the slot is held rather
than waiting for work it will not get. What is lost is nothing, because there was never a
queue row whose lock a worker could skip.

### The tick

The embedded scheduler wakes on a fixed period, lists the active Monitors of the installation,
computes each one's current slot, and attempts a claim. A claim that reports the slot is held
means another worker has it or it is already finished, and the scheduler moves on.

It remembers in memory the last slot it attempted per Monitor, so a ten-second tick does not
re-attempt a five-minute slot thirty times. That memory is an optimization with no correctness
role: losing it costs one redundant insert attempt that the slot index rejects harmlessly.

The tick's cost is therefore proportional to the number of *active* Monitors rather than to
the number *due*. That is the right trade at this size and the wrong one eventually. The
trigger for revisiting it is measurement, not intuition: when a tick's cost against all active
Monitors becomes visible in scheduling latency, a due-cursor table indexed on the next due
instant is the additive change, and the slot index remains the authority so the cursor may be
wrong without ever causing duplicate execution.

### Missed slots are recorded, but not all of them

ADR 0021's misfire policy is skip and record. On a tick, every slot older than the current one
that has no Run is a slot the installation missed, and the scheduler writes it as a `skipped`
Run walking backwards from the current slot.

It writes at most a bounded number of them per Monitor per tick, defaulting to ten. A week of
downtime at a 30-second interval is twenty thousand missed slots per Monitor, and writing them
all would spend the storage that retention exists to bound in order to record an outage of the
installation rather than of any target. Beyond the bound the absence of Runs is the record.

This is a deliberate weakening of "skip and record" for long outages, and it is the reason it
is written down rather than discovered later from a table that grew unexpectedly.

### One location until there is a registry

There is no Probe Location entity (ADR 0025). The embedded worker executes as a single
operator-configured location identifier, defaulting to `local`, and every slot it claims
carries that identifier.

Fan-out across locations is exactly the thing that needs the registry: deciding which
locations a Monitor runs from, what happens when one is unreachable, and how quorum reads
their results are all questions ADR 0021 left to a future decision. A single configured
identifier is the honest placeholder, and because slot identity already includes the location,
adding more later widens the slot series rather than reshaping it.

### The scheduler lives in `internal/run`

The tick loop, slot derivation, and the claim-execute-record sequence live in `internal/run`,
which stays standard-library-only. Every collaborator is a narrow port the package defines:
the source of active Monitors, the Run store, and an executor that turns a stored
configuration into a measurement.

`cmd/probehive` composes them, as ADR 0020 requires of the embedded worker. It is also the
only place where `probe.Observation` becomes `run.Observation`; the adapter that implements
the executor port is where the two shapes ADR 0025 kept separate finally meet, and it is a
composition detail rather than a dependency between the packages.

Putting the loop in the composition root instead would have made the only interesting logic in
the scheduler — slot arithmetic, misfire bounds, lease handling — reachable only by starting a
process with a database behind it.

## Consequences

- A Monitor can finally be due, which is what has blocked every downstream decision since
  ADR 0021.
- The scheduler never decodes check configuration, so adding a check type does not touch it.
- Frequency changes stay out of revision history, and Run history cannot answer what interval
  produced a given cadence.
- Slot instants are reproducible from the Monitor identifier, so two workers need no
  coordination and a restart resumes exactly where the arithmetic says it should.
- Every tick costs work proportional to all active Monitors. This is acceptable now and has a
  named, additive escape.
- A long outage leaves an unmarked gap after the first ten skipped Runs per Monitor.
- A self-hosted installation reports one Probe Location until the registry exists, so
  location-aware features have a value to carry but nothing yet to choose between.
- A run that outlives its interval overlaps the next slot. The slot series is independent of
  execution, so this is bounded by worker concurrency and lease expiry rather than prevented;
  serializing a Monitor's own executions is a separate decision if it turns out to be wanted.
