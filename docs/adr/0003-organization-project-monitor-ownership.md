# 0003: Organization, Project, and Monitor Ownership

- Status: Accepted
- Date: 2026-07-22
- Amended by: [ADR 0009](0009-tenant-scope-default-project-and-telemetry.md)

## Context

Tenant isolation requires an unambiguous ownership path, while initial onboarding must let a user create a Monitor without first building a deep administrative hierarchy.

## Decision

Use this required ownership path:

```text
Organization
  -> Project
    -> Monitor
      -> Monitor Revision
        -> Run
          -> Observation
```

Create a default Project during secure Organization bootstrap. Every Monitor belongs to exactly one Project. Environment and Service are optional Project-scoped classification resources rather than required ownership ancestors. Initially, a Monitor may reference zero or one Environment and zero or one Service. Agents and Probe Locations are Organization-scoped so they can serve multiple Projects under one tenant policy.

Include Organization identity in every relevant authorization decision, query, uniqueness rule, lease, event, cache key, object key, and telemetry context (for metrics-label restrictions see [ADR 0009](0009-tenant-scope-default-project-and-telemetry.md)).

## Consequences

- Users can create a Monitor immediately through the default Project.
- Project remains a stable administrative and authorization boundary.
- Environment and Service do not force an inflexible hierarchy.
- Moving a Monitor between Organizations is not an ordinary update and requires an explicit future design.
