# Architecture Decision Records

This directory records durable decisions about public compatibility, security,
storage, deployment, and module or repository boundaries.

## Context-Efficient Reading

Start with this index and read only the group related to the behavior being
changed. Then follow explicit `amends`, `clarifies`, or `supersedes` links from
those records. Do not load every ADR for routine work.

Code, tests, migrations, OpenAPI, and contract documentation provide current
implementation evidence. An accepted ADR remains decision authority until a
linked record changes it; a planning document does not override either source.

## When an ADR Is Required

Write an ADR only for selected implementation work when the decision is durable,
expensive to reverse, and changes at least one of:

- a published compatibility surface or externally observable guarantee;
- an authentication, authorization, tenancy, secret-storage, encryption, sandbox, or outbound-access trust boundary;
- persisted data ownership, destructive migration, retention, backup, or recovery semantics;
- a cross-module dependency, executable, service, repository, or public/private boundary; or
- a required durable runtime or infrastructure dependency.

Do not create an ADR for a reversible local implementation choice, an endpoint or
page inside an accepted capability, internal naming, test arrangement, or an
adjustable default. Keep those choices in code, tests, configuration, contract
documentation, or the introducing change. An exact value belongs in an ADR only
when it is itself a published compatibility or security guarantee.

Do not draft speculative ADRs for roadmap ideas. Prefer one decision per record,
identify the current consumer and non-goals, and name the evidence that would
justify revisiting it. Prefer fewer than 100 lines; keep detailed mappings and
adjustable defaults in their owning source.

## Lifecycle

Use a zero-padded number and lowercase filename. New records start from
[template.md](template.md). Accepted records are historical evidence and are not
rewritten to match newer templates. Change a decision with a linked amendment or
replacement. Move superseded records to `superseded/` so active navigation stays
clear while history remains available.

A proposed record may be deleted only when it was never accepted, implemented,
or referenced. Do not delete an accepted record merely to reduce the count.

## Active Decisions

Every record below is `Accepted`.

### Repository and Platform Foundations

- [0001: License and open-core boundary](0001-license-and-open-core-boundary.md)
- [0002: Modular monolith and canonical project topology](0002-modular-monolith-and-project-topology.md)
- [0005: PostgreSQL, leases, and outbox baseline](0005-postgresql-leases-and-outbox.md)
- [0006: Official Cloud runtime integration boundary](0006-cloud-runtime-integration-boundary.md)
- [0008: Toolchain and dependency reproducibility](0008-toolchain-and-dependency-reproducibility.md)
- [0011: Third-party licenses and notices](0011-third-party-licenses-and-notices.md)
- [0015: Frontend dependency and tooling baseline](0015-frontend-dependency-and-tooling-baseline.md), which clarifies ADR 0002

### Identity and Tenancy

- [0003: Organization, Project, and Monitor ownership](0003-organization-project-monitor-ownership.md)
- [0009: Tenant scope, default Project provisioning, and telemetry](0009-tenant-scope-default-project-and-telemetry.md)
- [0010: Browser authentication trust and compatibility](0010-browser-authentication-trust-and-compatibility.md), which supersedes ADR 0004
- [0012: Organization provisioning idempotency semantics](0012-organization-provisioning-idempotency.md), which clarifies ADR 0009
- [0013: First administrator bootstrap and local authentication](0013-first-administrator-and-local-authentication.md), which implements ADR 0010 and documents an ADR 0009 exception
- [0017: Organization membership and authorization](0017-organization-membership-and-authorization.md), which amends ADR 0013
- [0018: First-run Organization provisioning](0018-first-run-organization-provisioning.md), which amends ADR 0013 and clarifies ADR 0012
- [0019: Internationalization, localization, and stable error codes](0019-internationalization-and-error-codes.md), which amends ADRs 0012-0014 by replacing character-exact English messages with stable codes
- [0022: Organization rename](0022-organization-rename.md), which clarifies ADRs 0012 and 0017

### Monitoring and Execution

- [0007: Outbound access and SSRF security](0007-outbound-access-and-ssrf-security.md)
- [0014: Monitor lifecycle, revision immutability, and check configuration versioning](0014-monitor-lifecycle-and-revision-immutability.md), which clarifies ADR 0003
- [0016: Monitor Project movement and default Project lifecycle](0016-monitor-project-movement-and-default-project-lifecycle.md), which clarifies ADRs 0003 and 0014
- [0020: Check execution placement and outbound access enforcement](0020-check-execution-and-outbound-enforcement.md), which clarifies ADRs 0002 and 0007
- [0021: Run and Observation model, retention, and scheduling leases](0021-run-observation-retention-and-scheduling.md), which clarifies ADRs 0005 and 0014 and is amended by ADR 0025
- [0023: Outbound address classification, overrides, and denial reasons](0023-outbound-policy-classification-and-denial-reasons.md), which clarifies ADRs 0007 and 0020
- [0024: HTTP check execution and Observation content](0024-http-check-execution-and-observation-content.md), which clarifies ADRs 0020 and 0021
- [0025: Run storage schema, partition key, and lease placement](0025-run-storage-schema-and-lease-placement.md), which amends ADR 0021
- [0026: Execution interval ownership, slot derivation, and the embedded scheduler](0026-execution-interval-and-slot-derivation.md), which amends ADR 0021 and clarifies ADRs 0014 and 0020
- [0027: Health evaluation, confirmation Runs, and location quorum](0027-health-evaluation-confirmation-and-quorum.md)

### Incidents and Delivery

- [0028: Incident lifecycle and outbox event semantics](0028-incident-lifecycle-and-outbox-events.md)
- [0029: Alert intent and Delivery Attempt semantics](0029-alert-intent-and-delivery-attempt-semantics.md), which clarifies ADR 0028
- [0030: Signed Webhook Integration and delivery](0030-signed-webhook-integration-and-delivery.md), which clarifies ADR 0029

## Superseded Decisions

- [0004: Browser authentication and public compatibility](superseded/0004-browser-authentication-and-public-compatibility.md), superseded by ADR 0010
