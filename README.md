<p align="center">
  <img src="docs/brand/probehive-logo.svg" alt="ProbeHive logo" width="120">
</p>

<h1 align="center">ProbeHive</h1>

<p align="center">
  <strong>Distributed synthetic monitoring from every network that matters.</strong>
</p>

<p align="center">
  Monitor websites, APIs, networks, jobs, and critical services from public regions and private networks with one open platform.
</p>

<p align="center">
  <a href="https://github.com/probehive/probehive/actions/workflows/ci.yml"><img src="https://github.com/probehive/probehive/actions/workflows/ci.yml/badge.svg" alt="CI status"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache--2.0-4c7bd9.svg" alt="Apache 2.0 license"></a>
  <img src="https://img.shields.io/badge/status-pre--alpha-d89b2b.svg" alt="Project status: pre-alpha">
</p>

> [!IMPORTANT]
> ProbeHive is in active pre-alpha development and does not have a stable public release yet. The current source is intended for development and evaluation, and unreleased interfaces may change.

ProbeHive is an open-source distributed synthetic monitoring and availability platform. The public repository is the home of the complete self-hosted product and does not require a ProbeHive Cloud account.

## Why ProbeHive

- **Monitor from the networks that matter.** Combine public probe locations with private Agents deployed close to internal services and infrastructure.
- **Keep self-hosting independent.** Operate the control plane, data, retention, authentication, and probe locations without a hosted-service dependency.
- **Turn observations into operational truth.** Keep checks, observations, evaluated health, incidents, alerts, and status communication precise and auditable.
- **Use one open platform.** Bring websites, APIs, networks, jobs, certificates, and critical services under one versioned monitoring model.

## What Works Today

The current foundation implements:

- First-administrator setup that also provisions the installation's Organization and default Project, so a fresh install can hold a Monitor without any organizational setup.
- PostgreSQL-backed browser sessions with antiforgery, origin validation, fixed expiry, and deny-by-default authorization.
- Organization membership with permission-based authorization: a non-member cannot distinguish an Organization from one that does not exist.
- Monitors with immutable revisions and strict HTTP check configuration validation.
- Stable machine-readable error codes on every API failure, so clients localize without the API ever returning a translated string.
- An embedded scheduler and bounded worker that execute validated HTTP checks through the shared outbound-access policy and persist partitioned Runs and Observations.
- An antiforgery-protected manual Run endpoint that executes a Monitor's latest revision immediately through the same bounded worker.
- Fully scoped Run history and Observation query APIs with bounded keyset pagination and stable filters.
- Auditable Monitor health evaluation with explicit failure and recovery confirmation Runs, honest single-location quorum counts, and staleness handling.
- Automatic per-Monitor Incidents with open, acknowledge, and resolve lifecycle, immutable timelines, scoped keyset query APIs, and PostgreSQL-backed outbox dispatch.
- Immutable Alert intents for Incident opening and confirmed recovery, with Monitor-scoped API and React audit history that makes no delivery claim.
- Administrator-only signed Webhook Integrations with one-time-disclosed secrets, operator-keyring encryption, two-phase rotation, point-in-time Alert routing, strict HTTPS delivery, bounded retries, and Viewer-safe delivery-attempt evidence.
- A React administration application in English and Simplified Chinese, rendering instants in the viewer's time zone, with Playwright browser journeys.
- Default-Project Monitor inventory, a recoverable first-HTTP-Monitor flow, and authorized lifecycle, name, and execution-interval controls.
- A Monitor Health evidence view with current and stable state, complete quorum counts, confirmation candidates, and causal Run links.
- A Monitor-scoped Incident view with cursor history, lifecycle state, an immutable timeline, complete quorum counts, causal Run links, and authorized acknowledgement.
- A Monitor-scoped Run evidence screen with 30-day keyset history, deep-linked Run detail, and bounded HTTP Observation detail.
- A production-like rootless Compose package with pinned images, a non-root TLS gateway, external secret files, persistent PostgreSQL data, health checks, graceful shutdown, and a disposable first-result smoke check.
- A documented logical PostgreSQL backup and clean-restore procedure with deterministic Organization, monitoring, Incident, Alert, and encrypted Webhook recovery verification.

Upgrade, rollback, and broader operator readiness are the current focus. Status pages and Agents remain later work.

## Get Started

ProbeHive does not yet publish images or releases. Build and run the supported
production-like package with the [self-hosted installation guide](docs/installation.md).
For source development, use the [local development guide](docs/development.md).

Useful project references:

- [Self-hosted installation](docs/installation.md)
- [Backend contract](docs/backend-contract.md)
- [Architecture baseline](docs/architecture.md)
- [Contributing guide](CONTRIBUTING.md)
- [Security policy](SECURITY.md)

## Current Focus

The current milestone turns the implemented HTTP path into a dependable self-hosted product:

- Exercise schema upgrades and rollback boundaries.
- Complete operator guidance for retention and common failures.
- Extend clean-install verification through Incident evidence and signed Webhook delivery.

New Check Types, remote Agents, identity expansion, public status pages, CLI, monitoring as code, Kubernetes packaging, and hosted-service implementation are deferred until this milestone is complete unless one is required to close an installation or operability gap.

## Architecture

ProbeHive is a feature-oriented Go modular monolith. Commands are composition roots; feature packages own their domain behavior and persistence ports; PostgreSQL and HTTP packages adapt those ports.

```text
cmd/probehive/
internal/
  organization/
  user/
  monitor/
  check/
  outbound/
  probe/
  postgres/
  httpapi/
  httpapi/v1/
web/
deploy/
```

Feature packages, `internal/check`, and `internal/outbound` use only the Go standard library. `internal/outbound` owns the outbound-access policy and the validating dialer every tenant-influenced destination passes through, including probes, redirects, webhooks, and notification deliveries. `internal/probe` executes checks, turning a validated configuration and that dialer into an Observation and reaching the network through no other path. `internal/postgres` implements feature-owned persistence interfaces with pgx and embedded SQL migrations. `internal/httpapi` owns HTTP routing, browser security, versioned wire types, and Problem Details. The frontend remains a separately deployable API client and owns no authoritative authorization or business rules.

The backend uses Go 1.26.5 and PostgreSQL. First-party web applications use React, strict TypeScript, Vite, and React Router and build to static assets. The public API begins at `/api/v1`.

## ProbeHive Cloud

ProbeHive Cloud is the separately maintained official hosted service. It runs released public ProbeHive artifacts in shared multi-tenant service pools and adds proprietary account lifecycle, billing, metering, managed-location operations, abuse controls, support, and compliance services. The self-hosted product does not require the hosted service.

## Contributing

Read [CONTRIBUTING.md](CONTRIBUTING.md) before proposing a change. Contributions use Developer Certificate of Origin sign-off. Participation follows the community standards in [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md). Security reports must follow [SECURITY.md](SECURITY.md) and must not be filed as public issues.

## License and Trademarks

Source code and documentation in this repository are licensed under the [Apache License 2.0](LICENSE), unless an included third-party artifact states otherwise. See [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) for dependency notices.

The license does not grant unrestricted rights to the ProbeHive name, logo, or visual identity. See [TRADEMARKS.md](TRADEMARKS.md).
