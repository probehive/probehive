# 0002: Modular Monolith and Canonical Project Topology

- Status: Accepted
- Date: 2026-07-22
- Superseded by: [ADR 0015](0015-adopt-go-for-the-backend-implementation.md)
- Amended: 2026-07-23 — Recorded the full host dependency matrix in place of the earlier composition-root summary.

## Context

The initial product needs clear boundaries without speculative services, repositories, abstractions, or deployment complexity.

## Decision

Begin with a modular monolith and create projects only when they receive real behavior. Use these canonical names:

- `ProbeHive.Domain`
- `ProbeHive.Contracts`
- `ProbeHive.Application`
- `ProbeHive.Checks`
- `ProbeHive.Infrastructure`
- `ProbeHive.Api`
- `ProbeHive.Worker`
- `ProbeHive.Agent`
- `ProbeHive.Cli`
- root `web/` for the static React application

Domain is dependency-free. Contracts contains deliberately versioned wire types. Application depends on Domain and Contracts. Checks depends on Contracts. Infrastructure depends on Application and Domain. API and Worker are thin composition roots over Application and Infrastructure, and the Worker performs its optional embedded check execution through Checks. Agent is an outbound execution host and depends only on Contracts and Checks. CLI is an HTTP client of the versioned public API and may depend on Contracts. Executable hosts never reference one another.

Use React with strict TypeScript, Vite, and React Router. Use Vitest with React Testing Library for unit and component tests and Playwright for critical browser journeys. Do not introduce Next.js, Blazor, a production Node.js runtime, MediatR, generic repositories, generic units of work, mapping frameworks, message buses, or microservices without a concrete requirement and a new decision.

## Consequences

- Product features can ship atomically across internal layers.
- Architecture tests can enforce dependency direction.
- Hosts remain independently deployable without becoming separate source repositories.
- New abstractions and services require evidence rather than template-driven scaffolding.
