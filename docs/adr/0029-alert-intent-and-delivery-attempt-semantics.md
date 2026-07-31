# 0029: Alert Intent and Delivery Attempt Semantics

- Status: Accepted
- Date: 2026-07-31
- Clarifies: [ADR 0028](0028-incident-lifecycle-and-outbox-events.md)
- Related: [ADR 0007](0007-outbound-access-and-ssrf-security.md), [ADR 0009](0009-tenant-scope-default-project-and-telemetry.md), [ADR 0017](0017-organization-membership-and-authorization.md), [ADR 0019](0019-internationalization-and-error-codes.md)

## Context

ADR 0028 records automatic Incident lifecycle and leaves Alert routing and Delivery Attempt
semantics blocked. The current outbox can durably project a health transition into an
Incident, but the Incident transaction emits no fact for a notification consumer. There is
therefore no stable deduplication identity, recovery notification, delivery audit, or honest
way to distinguish an Alert from one attempt to send it.

The product blueprint distinguishes an Alert, which is one logical routed notification
intent, from a Delivery Attempt, which is one bounded call through one channel. It also puts
a signed generic webhook before SMTP and vendor integrations. Implementing that order still
needs answers that must not be hidden in a provider adapter:

1. Which Incident facts create an Alert and which do not.
2. Whether redelivery creates another Alert.
3. Whether retry mutates one attempt or appends another.
4. What can be retained without leaking target, provider, or secret material.
5. What is observable before any integration or routing policy exists.

## Decision

### Record an immutable Alert intent before delivery

An Alert is an immutable, render-neutral intent derived from one durable source fact. It is
not a queue row, HTTP request, translated message, or aggregate delivery status.

Phase 1 creates exactly these Alert kinds:

| Alert kind | Source fact | Meaning |
| --- | --- | --- |
| `incident.opened` | automatic Incident becomes `open` | the confirmed failure episode began |
| `incident.resolved` | automatic Incident becomes `resolved` | confirmed recovery ended the episode |

Acknowledgement does not create an Alert. It changes operator ownership, not evaluated
health, and must not be presented as failure or recovery. Degraded, Unknown, duplicate
health events, and Incident reads also create no Alert.

The stable deduplication identity is Organization, Incident, and Incident version. The
stored Alert carries its own UUIDv7, Organization, Project, Monitor, Incident, source
Incident version, kind, source occurrence time, and projection creation time. It carries no
localized prose, Monitor name snapshot, target URL, Observation bytes, recipient, route,
secret, or rendered provider payload. Consumers join the still-retained Incident when they
need evidence; future delivery prepares a bounded channel payload from an explicit routing
and integration snapshot.

An Alert exists even when no route or integration exists. Zero Delivery Attempts means no
delivery was attempted; it does not mean success, failure, or suppression. This makes the
current slice useful as an honest audit and prevents later configuration from fabricating a
claim about notifications that were never configured.

### Emit one internal Incident fact in the state-change transaction

Opening or resolving an Incident writes `incident.transitioned.v1` to the PostgreSQL outbox
in the same transaction as the Incident and timeline change. Its owning consumer is the
Alert projector. The camelCase payload contains the ADR 0028 common envelope plus
`incidentId`, `projectId`, `monitorId`, and `transition` (`opened` or `resolved`). Its
aggregate is the Incident and its aggregate version is the source Incident version. Its
causation is the consumed `health.transitioned.v1` event.

The topic is an internal durability contract, not a public webhook. The consumer verifies
the row and payload Organization, verifies the referenced Incident timeline version and
kind, inserts the Alert, and marks the event processed in one transaction. Re-delivery is a
no-op. Incident versions need not be contiguous in this topic because acknowledgement has
no notification fact; each event is independently valid against its immutable timeline row.

### Keep Alert reads tenant- and Monitor-scoped

The versioned API exposes Monitor-scoped Alert history under the existing Organization and
Project path. `alert.read` follows ADR 0017: built-in Administrators have it, built-in
Viewers have it because it is read-only, and non-members cannot distinguish the scope from
one that does not exist.

Lists are newest-first by `(occurredAt, id)`, use an opaque exclusive keyset cursor, default
to 50 rows, and permit 1 through 100. Alert and source Incident records have no Phase 1
time-based deletion. They follow the existing tenant-scoped Monitor or Organization
cascade. A future retention decision must preserve the Alert/Attempt audit relationship.

### Make every Delivery Attempt one bounded external call

A Delivery Attempt is append-only evidence of one actual call through one snapshotted
integration. It is not the outbox entry that requested work, and a probe retry is unrelated.
A retry appends a new attempt with the next positive sequence; it never overwrites the
outcome of an earlier attempt.

Every implemented attempt must carry Organization, Alert, integration identity and version,
channel, sequence, start and finish instants, and one terminal outcome:

| Outcome | Meaning |
| --- | --- |
| `succeeded` | the channel-specific success condition was observed within all bounds |
| `failed` | the provider returned a terminal rejection or a bounded call failed |
| `cancelled` | shutdown or lease loss cancelled the call before an outcome was observed |

A claimed but unfinished call may be represented as in progress while its lease is live,
but it is not a terminal Delivery Attempt and never counts as success. The channel record
defines its exact success statuses, timeout, payload ceiling, retry classification, and
maximum attempts before implementation.

Attempts retain stable failure codes and only bounded, allowlisted provider metadata needed
for diagnosis. They never retain response bodies, authorization material, signing secrets,
cookies, full secret-bearing URLs, or arbitrary provider text. Metrics carry channel and
outcome only; they never label Organization, Alert, Incident, Monitor, integration, target,
recipient, or URL identity.

### Require a channel decision before external delivery

This ADR does not select secret storage, integration configuration, routing, maintenance
suppression, recipients, message localization, webhook signing, DNS behavior, or retry
bounds. The first signed-webhook implementation needs a follow-up accepted ADR that applies
ADR 0007 to redirects and every connection, defines its signature and replay contract,
sets operator ceilings, and records how secret references are stored and rotated. SMTP and
every new outbound protocol category need their own reviewed threat model.

Until one channel ADR is accepted, ProbeHive records and exposes Alert intents but creates
no Delivery Attempt and performs no notification network I/O. Logs and UI must not describe
an Alert as sent or delivered merely because its intent exists.

Maintenance and quiet hours may later suppress Alert creation, attempt creation, or both,
but that policy must record an explicit auditable disposition. It may not delete or rewrite
an Incident, Alert, or prior Delivery Attempt.

## Consequences

- Incident opening and confirmed recovery gain one idempotent, queryable Alert intent.
- Alert history is truthful before routing exists and remains independent of outbox retries.
- Acknowledgement cannot accidentally page recipients as if health changed.
- The API adds an unreleased, Monitor-scoped Alert collection with no delivery claim.
- The first real channel remains blocked on its threat model, routing, secret storage, and
  bounded Delivery Attempt contract; this ADR deliberately does not choose them by accident.
