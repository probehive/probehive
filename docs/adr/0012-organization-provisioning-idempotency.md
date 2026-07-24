# 0012: Organization Provisioning Idempotency Semantics

- Status: Accepted
- Date: 2026-07-23
- Clarifies: [ADR 0009](0009-tenant-scope-default-project-and-telemetry.md)

## Context

ADR 0009 requires every Organization creation path to use one idempotent Application use case that creates the Organization and its default Project transactionally. Hosted onboarding and self-hosted bootstrap will both retry this operation after timeouts, restarts, and duplicate deliveries, so the exact idempotency contract must be fixed before the first implementation and cannot be left to endpoint-specific behavior.

## Decision

The Organization slug is the natural idempotency key for provisioning.

- A slug is 3 to 63 characters of lowercase ASCII letters, digits, and single interior hyphens; it starts and ends with a letter or digit. Slugs are unique across the platform; this is a documented global uniqueness exception under ADR 0009 because the Organization is itself the tenant boundary.
- The display name is trimmed Unicode text of 1 to 100 characters that is not empty after trimming.
- Creating an Organization always creates its default Project in the same transaction. The default Project is marked by persistent state (`is_default`), never by display name, and the database enforces at most one default Project per Organization.
- Repeating a creation request with a slug that already exists and an identical trimmed display name is a successful idempotent replay: the use case reports the existing Organization and its default Project without modifying state. The HTTP endpoint returns `200 OK` for a replay and `201 Created` for a first creation.
- A creation request whose slug exists with a different display name is a conflict, not a replay. The use case reports the conflict and the HTTP endpoint returns `409 Conflict` with Problem Details.
- Lost races are resolved by the database unique index on the slug: on a uniqueness violation the use case re-reads the winner and then applies the same replay-or-conflict rules.
- Validation of slug and display name lives in the use case so every creation path shares it; the API surfaces failures as `400` validation Problem Details.

The first HTTP surface for this use case is `POST /api/v1/organizations`. It ships before the first-administrator authentication baseline exists and is unreleased source under ADR 0010; it must be placed behind the authentication and authorization baseline before any published artifact exposes it.

## Consequences

- Every provisioning path, hosted or self-hosted, can retry safely without duplicate Organizations or duplicate default Projects.
- Clients need no separate idempotency-key header for provisioning; the slug carries that role.
- Renaming an Organization is a distinct future update operation and never happens implicitly through provisioning retries.
- The platform-wide slug uniqueness rule is recorded as a deliberate exception to Organization-scoped uniqueness.
