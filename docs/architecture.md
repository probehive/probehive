# Architecture Baseline

Status: Current
Last reviewed: 2026-08-03

This is the living summary of ProbeHive's durable technical boundaries. Exact
behavior belongs in code, tests, migrations, OpenAPI, and
[backend-contract.md](backend-contract.md). Operational behavior belongs in
[development.md](development.md) and deployment documentation.

ProbeHive does not maintain Architecture Decision Records. Git history preserves
the rationale and retired alternatives behind this baseline. Update this file
only when implementation changes a durable boundary; do not expand it with local
design choices, adjustable defaults, or future ideas.

## Product and Runtime

ProbeHive is a complete self-hosted synthetic monitoring product. It does not
require private source, private feeds, or a ProbeHive Cloud account. Hosted
services consume versioned public contracts and released artifacts rather than
public implementation source or public-core database tables.

The default runtime remains one Go application, one PostgreSQL database, and
separately built static React assets. Add no queue, cache, second database,
service, production Node.js runtime, or required orchestration platform until a
measured constraint justifies the additional operational cost.

## Code Boundaries

The backend is a feature-oriented modular monolith:

```text
cmd/probehive/       composition root
internal/<feature>/  domain behavior and feature-owned ports
internal/postgres/   SQL adapters and migrations
internal/httpapi/    HTTP composition, security, and versioned wire adapters
internal/outbound/   shared outbound-access policy and validating dialer
internal/probe/      protocol execution
web/                 static React API client
```

Feature packages own invariants and narrow interfaces. Composition and adapters
depend on those interfaces, never the reverse. Feature packages,
`internal/check`, and `internal/outbound` remain standard-library-only. Executable
hosts do not import one another. The architecture test is the executable source
of truth for import restrictions.

Create packages, abstractions, configuration, and extension points for current
behavior. Prefer constants until operators need variation and prefer concrete
implementations until multiple current consumers justify a general interface.

## Ownership and Tenancy

The required ownership path is:

```text
Organization -> Project -> Monitor -> Monitor Revision -> Run -> Observation
```

Status Components are Organization-owned communication identities associated with
explicitly selected Monitors; they are not Monitors and do not copy targets or monitoring
evidence into configuration. Draft configuration is private. Anonymous publication,
revocation, and disclosure-safe current-state projection are separate boundaries.

Organization is the tenant, authorization, and isolation boundary. Every
Organization creation path uses the same idempotent core operation and creates a
default Project transactionally. Monitors never move between Organizations.
Tenant identity is explicit in authorization, persistence, leases, events, and
uniqueness rules. A non-member cannot use object existence as an information
oracle.

Monitor Revisions are immutable. Run outcome, evaluated health, Incident
lifecycle, Alert intent, Delivery Attempt, and maintenance policy are separate
concepts. Stored evidence remains attributable to the revision and location that
produced it.

## Security Boundaries

Production browser access is same-origin by default. Browser sessions use opaque
server-side credentials in secure, HTTP-only cookies. Unsafe cookie-authenticated
requests require antiforgery and origin validation. Browser storage never holds
access or refresh tokens, and selecting an Organization in client state never
grants membership.

Every tenant- or target-influenced destination passes through the shared
outbound-access policy. Validate and bind each connection and redirect, preserve
the intended Host and TLS SNI, and reject cloud metadata and special-purpose
addresses according to the active operator profile. Tenant input cannot disable
TLS verification or loosen operator ceilings.

Checks and deliveries have bounded time, redirects, ports, concurrency, payload,
response, bandwidth, and retained artifacts. Never persist or log credentials,
secret-bearing URLs, unredacted provider responses, or unbounded target content.
See [SECURITY.md](../SECURITY.md) for reporting and supported-version policy.

## Contracts and Persistence

The public HTTP API starts at `/api/v1`. Unreleased behavior may evolve; once an
API, schema, event, package, generated client, Agent protocol, or artifact is
published, breaking meaning or shape requires a declared new version and
migration guidance.

Stable machine-readable error and failure codes are contract. English messages
are documentation and may be reworded. The React client localizes from stable
codes and renders times in the viewer's locale and time zone.

PostgreSQL owns durable application state. Runs and Observations use bounded,
partitioned storage; scheduling uses leases and idempotent slot identity; durable
side effects use an outbox. Retention, backup, restore, schema upgrade, and
rollback behavior must remain explicit and testable. Migrations are append-only
once published and must preserve tenant isolation and recoverability.

## Monitoring Semantics

Checks execute only validated, versioned Monitor Revisions. HTTP execution uses
the shared outbound policy, enforces effective limits again at execution, and
persists bounded observations without target-controlled bodies, headers, or
messages that would require later redaction.

Health evaluation distinguishes unknown, healthy, degraded, and down evidence,
including confirmation Runs and location quorum. Incidents form an auditable
lifecycle from health transitions. Alerts are immutable routing intents;
Delivery Attempts separately record bounded retries and outcomes. Signed Webhook
Integrations keep secrets encrypted, disclose them once, rotate in two phases,
and deliver only through the shared outbound policy.

Maintenance is an event-time overlay evaluated at the immutable Alert occurrence
instant against retained window creation, bounds, and cancellation timestamps. It
never pauses Checks or changes Runs, Observations, evaluated health, Incidents, or
Alerts. A matching window terminally completes each applicable Webhook route with a
stable maintenance reason and window identity, without creating a Delivery Attempt.
Delayed or replayed projection therefore preserves the same result after later
window cancellation.

## Frontend and Deployment

The frontend uses React, strict TypeScript, Vite, React Router, TanStack Query,
Vitest with React Testing Library, and Playwright. It is a static API client and
owns no authoritative authorization or business rule. Do not add another
component, styling, or state framework without a current need.

Released artifacts run as non-root under rootless Podman and Docker with explicit
persistent data, health checks, graceful shutdown, external secret injection,
and no privileged mode, host networking, or mounted container-engine socket.
The Compose package uses a project-scoped application bridge for the gateway and
API, plus an internal data bridge for the API and PostgreSQL. Only the gateway's
TLS port is published; the API and PostgreSQL have no host port mappings. The
application bridge permits required API egress, while tenant-influenced
destinations still pass through the shared outbound policy and nginx has no
tenant-controlled upstream.

## Changing This Baseline

Normal implementation inside these boundaries needs no architecture document
change. Before changing a published contract, trust boundary, persisted-data or
recovery semantic, service or repository boundary, or required infrastructure:

1. state the concrete problem and evidence;
2. update this baseline and the owning contract, migration, test, or operator document;
3. keep alternatives and detailed rationale in the introducing commit or pull request; and
4. remove superseded prose instead of accumulating parallel historical documents.
