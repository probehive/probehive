# 0009: Tenant Scope, Default Project Provisioning, and Telemetry

- Status: Accepted
- Date: 2026-07-23
- Amends: [ADR 0003](0003-organization-project-monitor-ownership.md)

## Context

Organization ownership must remain explicit across application behavior and operations, but copying tenant-specific identifiers into every telemetry dimension would create unbounded metrics and unnecessary data exposure. Default Project creation also needs to be an invariant rather than an onboarding convention.

## Decision

All Organization creation paths use one idempotent Application use case that creates the Organization and its default Project transactionally in the public core. Hosted onboarding calls that public behavior rather than constructing monitoring records directly. The default Project is identified by stable internal state, not by matching a localized display name.

Every tenant-scoped authorization decision, query, uniqueness rule, lease, event, cache key, and object key carries Organization identity explicitly. Global reference data and genuinely platform-wide records are documented exceptions rather than receiving a meaningless tenant key.

Structured security and audit logs and traces may include an internal Organization identifier when it is required for diagnosis or authorization evidence. Exporters, retention, and access controls treat it as sensitive operational metadata. Targets, secret-bearing URLs, credentials, and personal data are not telemetry attributes.

Metrics never use identifiers specific to an Organization, Project, Monitor, Run, target, URL, or user, including hashed or encoded forms, as labels. Metrics use bounded dimensions such as check type, normalized outcome, execution mode, and managed location class. Per-tenant reporting is produced from tenant-scoped stored data or bounded dedicated views rather than unbounded metric labels.

## Consequences

- Organization isolation remains explicit in every material data and authorization boundary.
- Default Project creation cannot be skipped by a new onboarding path.
- Metrics remain operable at high tenant counts.
- Logs and traces retain enough controlled tenant context for security investigation without turning identifiers into public or unbounded telemetry dimensions.
