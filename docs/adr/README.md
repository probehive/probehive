# Architecture Decision Records

This directory records durable decisions that affect multiple modules, public compatibility, security posture, storage, deployment, or repository boundaries.

## When an ADR Is Required

Write an ADR only for selected implementation work when the decision is intended to be durable, is expensive to reverse, and changes at least one of:

- a published compatibility surface or externally observable semantic guarantee;
- an authentication, authorization, tenancy, secret-storage, encryption, sandbox, or outbound-access trust boundary;
- persisted data ownership, destructive migration, retention, backup, or recovery semantics;
- a cross-module dependency rule, executable or service boundary, repository boundary, or public/private ownership boundary; or
- a durable runtime or infrastructure dependency such as another database, queue, cache, object store, or required platform.

Do not use an ADR for a reversible local implementation choice, an endpoint or page that follows an accepted capability, an internal name, a test arrangement, or an exact operational default such as a batch size, timeout, retry count, concurrency limit, or page size. Keep those choices in code, tests, configuration, contract documentation, or the pull request. An exact value belongs in an ADR only when the value itself is a published compatibility or security guarantee.

Do not draft speculative ADRs for roadmap ideas that have not been selected. Prefer one decision per ADR, identify the current consumer, state non-goals, and name the evidence that would justify revisiting the decision. Prefer fewer than 100 lines; move implementation tables and adjustable defaults to the owning code or documentation.

## Format

Each ADR uses a zero-padded sequence number and a short lowercase filename:

```text
0001-short-decision-title.md
```

An ADR contains its status, decision date, scope, context, decision, consequences, and review triggers. Status and relationship metadata may be updated; a later decision clarifies, amends, or supersedes an earlier ADR through a new record that links to both decisions. New ADRs start from [template.md](template.md).

Existing accepted records remain historical evidence and do not need to be rewritten to match the current template. Change an accepted decision through an explicit amendment or a linked replacement rather than silently editing implementation to disagree with it.

## Decisions

Every record below is `Accepted` unless its entry says otherwise.

- [0001: License and open-core boundary](0001-license-and-open-core-boundary.md)
- [0002: Modular monolith and canonical project topology](0002-modular-monolith-and-project-topology.md)
- [0003: Organization, Project, and Monitor ownership](0003-organization-project-monitor-ownership.md)
- [0004: Browser authentication and public compatibility](0004-browser-authentication-and-public-compatibility.md), which is superseded by ADR 0010
- [0005: PostgreSQL, leases, and outbox baseline](0005-postgresql-leases-and-outbox.md)
- [0006: Official Cloud runtime integration boundary](0006-cloud-runtime-integration-boundary.md)
- [0007: Outbound access and SSRF security](0007-outbound-access-and-ssrf-security.md)
- [0008: Toolchain and dependency reproducibility](0008-toolchain-and-dependency-reproducibility.md)
- [0009: Tenant scope, default Project provisioning, and telemetry](0009-tenant-scope-default-project-and-telemetry.md)
- [0010: Browser authentication trust and compatibility](0010-browser-authentication-trust-and-compatibility.md), which supersedes ADR 0004
- [0011: Third-party licenses and notices](0011-third-party-licenses-and-notices.md)
- [0012: Organization provisioning idempotency semantics](0012-organization-provisioning-idempotency.md), which clarifies ADR 0009
- [0013: First administrator bootstrap and local authentication](0013-first-administrator-and-local-authentication.md), which implements ADR 0010 and documents an ADR 0009 exception
- [0014: Monitor lifecycle, revision immutability, and check configuration versioning](0014-monitor-lifecycle-and-revision-immutability.md), which clarifies ADR 0003
- [0015: Frontend dependency and tooling baseline](0015-frontend-dependency-and-tooling-baseline.md), which clarifies ADR 0002
- [0016: Monitor Project movement and default Project lifecycle](0016-monitor-project-movement-and-default-project-lifecycle.md), which clarifies ADR 0003 and ADR 0014
- [0017: Organization membership and authorization](0017-organization-membership-and-authorization.md), which amends ADR 0013
- [0018: First-run Organization provisioning](0018-first-run-organization-provisioning.md), which amends ADR 0013 and clarifies ADR 0012
- [0019: Internationalization, localization, and stable error codes](0019-internationalization-and-error-codes.md), which amends ADR 0012, ADR 0013, and ADR 0014 by replacing their character-exact English messages with stable codes
- [0020: Check execution placement and outbound access enforcement](0020-check-execution-and-outbound-enforcement.md), which clarifies ADR 0002 and ADR 0007
- [0021: Run and Observation model, retention, and scheduling leases](0021-run-observation-retention-and-scheduling.md), which clarifies ADR 0005 and ADR 0014 and is amended by ADR 0025
- [0022: Organization rename](0022-organization-rename.md), which clarifies ADR 0012 and ADR 0017
- [0023: Outbound address classification, overrides, and denial reasons](0023-outbound-policy-classification-and-denial-reasons.md), which clarifies ADR 0007 and ADR 0020
- [0024: HTTP check execution and Observation content](0024-http-check-execution-and-observation-content.md), which clarifies ADR 0020 and ADR 0021
- [0025: Run storage schema, partition key, and lease placement](0025-run-storage-schema-and-lease-placement.md), which amends ADR 0021
- [0026: Execution interval ownership, slot derivation, and the embedded scheduler](0026-execution-interval-and-slot-derivation.md), which amends ADR 0021 and clarifies ADR 0014 and ADR 0020
- [0027: Health evaluation, confirmation Runs, and location quorum](0027-health-evaluation-confirmation-and-quorum.md)
- [0028: Incident lifecycle and outbox event semantics](0028-incident-lifecycle-and-outbox-events.md)
- [0029: Alert intent and Delivery Attempt semantics](0029-alert-intent-and-delivery-attempt-semantics.md), which clarifies ADR 0028
- [0030: Signed Webhook Integration and delivery](0030-signed-webhook-integration-and-delivery.md), which clarifies ADR 0029
