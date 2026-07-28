# 0027: Health Evaluation, Confirmation Runs, and Location Quorum

- Status: Accepted
- Date: 2026-07-28
- Clarifies: [ADR 0021](0021-run-observation-retention-and-scheduling.md), [ADR 0024](0024-http-check-execution-and-observation-content.md), [ADR 0026](0026-execution-interval-and-slot-derivation.md)
- Related: [ADR 0009](0009-tenant-scope-default-project-and-telemetry.md)

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

This ADR fixes those meanings for the embedded single-location implementation. Distributed
location membership, location-health exclusion, regional diversity, and configurable X-of-N
policy remain Phase 2 work and require another accepted decision.

## Decision

### Separate stored health from Run outcome

Create a Monitor-scoped, Organization-scoped evaluated-health state machine. Store its
policy version, last complete evaluation cohort, transition time, transition version,
and immutable evidence naming the causal Runs and location votes.

| State | Meaning |
| --- | --- |
| Unknown | No current determinate cohort exists, or evidence is stale. |
| Healthy | Recovery quorum has current passing evidence and no transition is pending. |
| Degraded | Evidence is mixed, quorum is insufficient, or confirmation is pending. |
| Down | Failure quorum and required confirmation have current failing evidence. |

Run outcome remains one execution's result. Health remains a policy conclusion over Runs.
Neither rewrites the other. Degraded is not an alias for one failed Run.

### Classify evidence before quorum

Classify each terminal Run as passing, failing, location-fault, or indeterminate using
outcome plus stable failure code, never prose.

| Run evidence | Classification |
| --- | --- |
| `passed` | passing |
| `failed` with `probe.http.status.unexpected` | failing |
| `timedout` with `probe.execution.timeout` | failing |
| `errored` with `probe.http.redirect.tooMany`, `probe.tls.certificateInvalid`, `probe.transport.failed`, `outbound.resolution.failed`, `outbound.resolution.empty`, or `outbound.connect.failed` | failing |
| `errored` with `outbound.address.mismatch` | location-fault |
| `errored` with `probe.checkType.unsupported`, `probe.http.request.invalid`, `outbound.policy.unconfigured`, `outbound.url.tooLong`, `outbound.url.invalid`, `outbound.url.notAbsolute`, `outbound.url.scheme`, `outbound.url.userInfo`, `outbound.host.missing`, `outbound.host.invalid`, `outbound.port.invalid`, `outbound.port.denied`, `outbound.network.unsupported`, `outbound.address.denied`, or any unrecognized failure code | indeterminate |
| `cancelled`, `skipped`, or outcome-null | indeterminate |
| any manual Run | indeterminate |

The outcome/code combinations above are validated defensively. A combination that is not
listed is indeterminate. This makes a new executor code fail closed with respect to health:
it cannot declare a target Down until this ADR or a superseding decision classifies it.

Manual Runs remain diagnostics. A later audited override may change health, but merely
requesting a manual Run does not.

### Evaluate scheduled cohorts

A cohort is one Monitor revision and one ordinary `scheduled_for` instant. Phase 1 permits
only the configured embedded location, snapshots that location and policy version
`phase1.v1`, and fixes both failure and recovery quorum at one of one. Configuration that
names multiple locations, quorum other than one, or a regional constraint is rejected.

Passing and failing are determinate votes. Location-fault, indeterminate, and missing are
reported separately and do not cast a vote. A location-fault cannot shrink the denominator
without a durable location-health model, so it leaves the current stable state unchanged
and may leave a pending transition Degraded. Every stored evaluation records configured,
eligible, responding, passing, failing, location-fault, indeterminate, and missing counts.
An API never reports a percentage without its numerator and denominator.

### Make confirmation explicit and causal

A failure or recovery candidate schedules exactly one confirmation Run. It:

- uses the same revision and policy version as the evidence it confirms;
- records the triggering evaluation and Run identifiers;
- uses request time as `scheduled_for` so it cannot collide with the ordinary slot;
- is idempotent on candidate transition identity;
- remains visible as kind confirmation;
- cannot recursively request confirmation; and
- never replaces or mutates its triggering Run.

Single-location confirmation uses that location. A second-location confirmation waits for
the location registry and quorum model.

The Phase 1 threshold is exactly one qualifying scheduled cohort followed by one matching
confirmation. Failure and recovery use the same threshold. A candidate identity is
Organization, Monitor, source revision, transition direction, and triggering Run. A unique
constraint makes the request idempotent. A confirmation stores the candidate, triggering
Run, and request event as causal fields.

Contradiction returns to the prior stable state. An indeterminate confirmation leaves
Degraded until a later determinate scheduled cohort supersedes the candidate or staleness
makes it Unknown; it never manufactures Down.

### Keep processing deterministic

| Current | Evidence | Result |
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
processed outbox identifiers. Re-delivery is a no-op.

Current health follows evidence arrival order, because outbox commit order is the only order
that was actually observed. A Run whose `scheduled_for` is earlier than the last applied
scheduled cohort is retained and queryable but marked late and does not rewrite current
health, a pending candidate, an Incident, or an already published transition. Phase 1 does
not perform retroactive replay. This avoids silently rewriting Incident history after an
operator has seen or acknowledged it.

### Staleness, Monitor lifecycle, and retention

For an Active Monitor, determinate evidence becomes stale at:

`latest determinate finished_at + max(3 * effective interval, 2 * execution ceiling)`.

The staleness sweep transitions the health to Unknown and clears a pending candidate. The
Run's completion instant, not its slot or evaluation time, starts the window.

A new revision does not reset health eagerly. The prior state and its source revision remain
visible until the latest revision supplies new determinate evidence; Runs from a superseded
revision cannot change current health. Pause and archive stop scheduled Runs, confirmations,
and staleness transitions. They preserve the last evaluated state and expose its source
revision rather than inventing Unknown or Healthy. Reactivation resumes evaluation and
staleness from new evidence.

There is no Phase 1 flapping suppression, cooldown, debounce, or hidden notification policy.
Every actual state transition is stored. A future flapping policy must be separately visible
and must not rewrite evidence.

Current health and immutable transition summaries live for the Monitor's lifetime and are
deleted only through the existing tenant-scoped Monitor or Organization cascade. Each
summary retains bounded counts, causal Run identifiers, and their `scheduled_for` partition
keys, so it remains intelligible after raw Run partitions expire.

## Consequences

- Health becomes auditable instead of inferred from the latest Run.
- Confirmation remains explicit, causal, and visible through the existing kind.
- Distributed evaluation stays disabled until an honest denominator exists.
- Durable state and idempotent consumption are required.
- Late evidence is retained without retroactively changing current health or Incident
  history.
