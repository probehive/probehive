# Architecture Decision Records

This directory records durable decisions that affect multiple modules, public compatibility, security posture, storage, deployment, or repository boundaries.

## Format

Each ADR uses a zero-padded sequence number and a short lowercase filename:

```text
0001-short-decision-title.md
```

An ADR contains its status, decision date, context, decision, and consequences. Status and relationship metadata may be updated; a later decision clarifies, amends, or supersedes an earlier ADR through a new record that links to both decisions. New ADRs start from [template.md](template.md).

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
- [0021: Run and Observation model, retention, and scheduling leases](0021-run-observation-retention-and-scheduling.md), which clarifies ADR 0005 and ADR 0014
