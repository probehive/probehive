# 0002: Modular Monolith and Canonical Project Topology

- Status: Accepted
- Date: 2026-07-22
- Clarified by: [ADR 0015](0015-frontend-dependency-and-tooling-baseline.md)

## Context

The initial product needs clear boundaries without speculative services, repositories, abstractions, or deployment complexity.

## Decision

Begin with a feature-oriented Go modular monolith and create packages or commands only when they receive real behavior. Use this canonical layout:

```text
cmd/probehive/
internal/organization/
internal/user/
internal/monitor/
internal/check/
internal/postgres/
internal/httpapi/
internal/httpapi/v1/
web/
```

The package and command boundaries are:

- `cmd/probehive` is the API server and composition root. Worker, Agent, CLI, and other executable hosts become sibling commands only when they gain real behavior. Executable hosts never import one another.
- `internal/organization`, `internal/user`, and `internal/monitor` own their domain types, invariants, use cases, and narrow store or service interfaces.
- Feature packages and `internal/check` remain standard-library-only. They import no SQL package, HTTP package, database driver, composition package, or sibling feature implementation.
- `internal/postgres` implements feature-owned persistence interfaces with explicit SQL and pgx. It owns embedded migrations and may import feature packages; feature packages never import it.
- `internal/httpapi` owns HTTP composition, sessions, antiforgery, authentication, authorization, health endpoints, and transport adapters. Versioned wire types live in `internal/httpapi/v1` beside their handlers.
- A separate exported contracts package or module is created only for a real source consumer. Cross-repository consumption uses published APIs, events, generated clients, wire packages, and OCI artifacts.

Use short, lowercase, singular package names without stutter. Keep pure state transitions separate from transport and persistence, propagate `context.Context` across I/O boundaries, and keep executable composition out of feature packages.

Use React with strict TypeScript, Vite, and React Router. Use Vitest with React Testing Library for unit and component tests and Playwright for critical browser journeys. Do not introduce Next.js, a production Node.js runtime, generic repositories, generic units of work, mapping frameworks, message buses, or microservices without a concrete requirement and a new decision.

## Consequences

- Product features can ship atomically across explicit package boundaries.
- Architecture tests can enforce dependency direction.
- Hosts remain independently deployable without becoming separate source repositories.
- New abstractions and services require evidence rather than template-driven scaffolding.
