# 0027: Health Evaluation, Confirmation Runs, and Location Quorum

- Status: Proposed
- Date: 2026-07-28
- Clarifies: [ADR 0021](0021-run-observation-retention-and-scheduling.md), [ADR 0024](0024-http-check-execution-and-observation-content.md), [ADR 0026](0026-execution-interval-and-slot-derivation.md)
- Related: [ADR 0009](0009-tenant-scope-default-project-and-telemetry.md)

This is a proposal, not implementation authority. Its status must change to Accepted
before code, migrations, OpenAPI, or event contracts depend on it. The acceptance
blockers below are deliberately unresolved.

## Context

ProbeHive stores and queries Runs and Observations and executes scheduled HTTP checks,
but it does not evaluate operational health. Existing ADRs and code decide only that:

- outcomes are passed, failed, errored, timedout, cancelled, and skipped;
- kinds are scheduled, confirmation, and manual, and kind is public query data;
- Observations carry stable failure codes and bounded detail;
- non-manual slot identity is revision, location, and scheduled_for;
- the scheduler currently creates only scheduled Runs at one configured location;
- Run completion can write outbox entries transactionally, but has no topic or producer;
- abandoned claims can remain outcome-null rather than inventing a measurement.

The blueprint names Unknown, Healthy, Degraded, and Down and calls for thresholds,
X-of-N, location quorum, confirmation, recovery confirmation, location-failure
separation, honest denominators, and flapping handling. It does not decide their meaning.

Still undecided are:

1. The evidence meaning of every outcome and failure code, including skipped, cancelled,
   abandoned, late, manual, and superseded-revision Runs.
2. Evaluation windows, failure and recovery thresholds, staleness, and flapping.
3. Confirmation triggers, causal linkage, revision, location, idempotency, and recursion.
4. Quorum membership, denominator snapshots, missing and unhealthy locations, X-of-N,
   and regional diversity.
5. State transitions through activation, pause, archive, revision, and policy changes.
6. Deterministic handling of out-of-order completion and at-least-once delivery.

## Proposed Decision

Everything below is a candidate answer for owner review.

### Separate stored health from Run outcome

Create a Monitor-scoped, Organization-scoped evaluated-health state machine. Store its
policy version, last complete evaluation cohort, transition time, transition version,
and immutable evidence naming the causal Runs and location votes.

| State | Candidate meaning |
| --- | --- |
| Unknown | No current determinate cohort exists, or evidence is stale. |
| Healthy | Recovery quorum has current passing evidence and no transition is pending. |
| Degraded | Evidence is mixed, quorum is insufficient, or confirmation is pending. |
| Down | Failure quorum and required confirmation have current failing evidence. |

Run outcome remains one execution's result. Health remains a policy conclusion over Runs.
Neither rewrites the other. Degraded is not an alias for one failed Run.

### Classify evidence before quorum

Classify each terminal Run as passing, failing, location-fault, or indeterminate using
outcome plus stable failure code, never prose. Passed is proposed as passing. Cancelled,
skipped, outcome-null, and manual Runs are proposed as indeterminate for automatic health.
Errored is not classified as a group: every failure code must receive an explicit class
before acceptance.

Manual Runs remain diagnostics. A later audited override may change health, but merely
requesting a manual Run does not.

### Evaluate scheduled cohorts

A cohort is one Monitor revision and one ordinary scheduled_for instant across its selected
locations. Snapshot expected locations and policy version when first observed. Each expected
location contributes at most one vote. Passing and failing are determinate; location-fault,
indeterminate, and missing are reported separately and never silently become pass or fail.

The candidate quorum is a strict majority of eligible locations plus any required-region
constraint. Record configured, eligible, responding, passing, failing, indeterminate, and
missing counts. Never report a percentage without numerator and denominator. Excluding a
location requires a durable location-health decision; absence alone cannot shrink the
denominator.

Until a location registry and location-health model exist, permit only the configured
embedded location. Reject multi-location policy and quorum greater than one.

### Make confirmation explicit and causal

A candidate failure or recovery schedules exactly one confirmation Run. It:

- uses the same revision and policy version as the evidence it confirms;
- records the triggering evaluation and Run identifiers;
- uses request time as scheduled_for so it cannot collide with the ordinary slot;
- is idempotent on candidate transition identity;
- remains visible as kind confirmation;
- cannot recursively request confirmation; and
- never replaces or mutates its triggering Run.

Single-location confirmation uses that location. A second-location confirmation waits for
the location registry and quorum model.

The candidate Phase 1 rule is one qualifying scheduled cohort followed by one matching
confirmation. Contradiction returns to the prior stable state. Indeterminate confirmation
leaves Degraded until staleness makes it Unknown; it never manufactures Down.

### Keep replay deterministic

| Current | Evidence | Candidate result |
| --- | --- | --- |
| Unknown | passing cohort | Healthy |
| Unknown or Healthy | failing cohort | Degraded; request failure confirmation |
| Degraded | matching failing confirmation | Down |
| Degraded | contradictory passing confirmation | prior stable state |
| Down | passing cohort | Degraded; request recovery confirmation |
| Degraded | matching passing recovery confirmation | Healthy |
| Degraded | contradictory failing recovery confirmation | Down |
| any | no current determinate evidence | Unknown after accepted staleness |

Serialize evaluation per Organization and Monitor, compare transition versions, and record
processed outbox identifiers. Re-delivery is a no-op. Recompute an affected cohort and later
cohorts in the bounded window by scheduled order when a Run arrives late.

A new revision starts a new evaluation series. Old Runs keep historical evidence but cannot
change current health. Pause and archive stop new evaluation and confirmation; visible state
is an acceptance blocker.

Phase 1 either defines exact flapping behavior or has none. It must not hide cooldowns,
debounce, or notification suppression inside the evaluator.

## Acceptance Blockers

Before acceptance, record owner choices for:

1. Every current HTTP failure-code classification, especially DNS, connect, TLS, policy
   denial, invalid configuration, and internal execution.
2. Strict majority versus separate X-of-N failure and recovery thresholds.
3. Exact staleness duration and whether it starts at slot, completion, or evaluation.
4. Confirmation and recovery thresholds and whether they are identical.
5. Reset versus preservation on a new revision.
6. Visible health of Paused and Archived Monitors.
7. Exact flapping behavior or its explicit Phase 1 omission.
8. Health evidence retention after raw Run partitions expire.

## Consequences

- Health becomes auditable instead of inferred from the latest Run.
- Confirmation remains explicit, causal, and visible through the existing kind.
- Distributed evaluation stays disabled until an honest denominator exists.
- Durable state, idempotent consumption, and bounded replay are required.
- Product choices remain blocked instead of being guessed in code.
