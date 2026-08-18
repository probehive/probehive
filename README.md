<p align="center">
  <img src="docs/brand/probehive-logo.svg" alt="ProbeHive logo" width="120">
</p>

<h1 align="center">ProbeHive</h1>

<p align="center">
  <strong>Self-hosted HTTP monitoring with auditable operational evidence.</strong>
</p>

<p align="center">
  Run bounded HTTP checks, follow health and incident evidence, deliver signed Webhooks, and publish a disclosure-safe current status page.
</p>

<p align="center">
  <a href="https://github.com/probehive/probehive/actions/workflows/ci.yml"><img src="https://github.com/probehive/probehive/actions/workflows/ci.yml/badge.svg" alt="CI status"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache--2.0-4c7bd9.svg" alt="Apache 2.0 license"></a>
  <img src="https://img.shields.io/badge/status-unreleased%20candidate-d89b2b.svg" alt="Project status: unreleased candidate">
</p>

> [!IMPORTANT]
> ProbeHive has no published release or supported version yet. The current source is an
> unreleased pre-release candidate for development and evaluation; interfaces and package
> behavior may change until the owner selects, signs, and publishes a version.

ProbeHive is an open-source distributed synthetic monitoring and availability platform. The public repository is the home of the complete self-hosted product and does not require a ProbeHive Cloud account.

## Why ProbeHive

- **Monitor HTTP services from an operator-controlled location.** Run scheduled and manual checks from the embedded local worker without a hosted dependency.
- **Keep self-hosting independent.** Operate the control plane, data, retention, authentication, and probe locations without a hosted-service dependency.
- **Turn observations into operational truth.** Keep checks, observations, evaluated health, incidents, alerts, and status communication precise and auditable.
- **Communicate current state deliberately.** Publish only operator-selected labels and evaluated state through a revocable opaque URL.

## What Works Today

The current foundation implements:

- First-administrator setup that also provisions the installation's Organization and default Project, so a fresh install can hold a Monitor without any organizational setup.
- PostgreSQL-backed browser sessions with antiforgery, origin validation, fixed expiry, and deny-by-default authorization.
- Organization membership with permission-based authorization: a non-member cannot distinguish an Organization from one that does not exist.
- A permission-aware Organization operational overview with bounded Monitor lifecycle,
  Active-Monitor health, active-Incident, Integration, and status-publication summaries.
- Monitors with immutable revisions and strict HTTP check configuration validation.
- Monitor-scoped one-time maintenance windows with explicit UTC bounds, overlap prevention, durable cancellation, and Viewer-readable current and upcoming state.
- Event-time maintenance semantics that preserve Runs, Observations, health, Incidents, and Alerts while terminally suppressing matching Webhook routes with explicit window attribution and no network attempt.
- Stable machine-readable error codes on every API failure, so clients localize without the API ever returning a translated string.
- An embedded scheduler and bounded worker that execute validated HTTP checks through the shared outbound-access policy and persist partitioned Runs and Observations.
- An antiforgery-protected manual Run endpoint that executes a Monitor's latest revision immediately through the same bounded worker.
- Fully scoped Run history and Observation query APIs with bounded keyset pagination and stable filters.
- Auditable Monitor health evaluation with explicit failure and recovery confirmation Runs, honest single-location quorum counts, and staleness handling.
- Administrator-only private status-page drafts with explicitly selected Monitor-backed components, operator-chosen public labels, and deterministic ordering.
- Revocable anonymous status publication with one-time opaque URLs and a disclosure-safe current-state projection.
- Automatic per-Monitor Incidents with open, acknowledge, and resolve lifecycle, immutable timelines, scoped keyset query APIs, and PostgreSQL-backed outbox dispatch.
- Immutable Alert intents for Incident opening and confirmed recovery, with Monitor-scoped API and React audit history whose separate delivery evidence distinguishes no route, pending work, maintenance suppression, and attempt outcomes.
- Administrator-only browser management for signed Webhook Integrations, with one-time-disclosed secrets, operator-keyring encryption, explicit two-phase rotation, point-in-time Alert routing, strict HTTPS delivery, bounded retries, and Viewer-safe delivery-attempt evidence.
- A React administration application in English and Simplified Chinese, rendering instants in the viewer's time zone, with Playwright browser journeys.
- A Default-Project Monitor inventory workbench with literal search, explicit lifecycle, evaluated-health, latest-Run, and maintenance filters, stable sorting and pagination, quick actions, and a recoverable first-HTTP-Monitor flow.
- A Monitor Health evidence view with current and stable state, complete quorum counts, confirmation candidates, and causal Run links.
- A Monitor-scoped Incident view with cursor history, lifecycle state, an immutable timeline, complete quorum counts, causal Run links, and authorized acknowledgement.
- A Monitor-scoped Run evidence screen with 30-day keyset history, deep-linked Run detail, and bounded HTTP Observation detail.
- A production-like rootless Compose package with pinned images, a non-root TLS gateway, external secret files, persistent PostgreSQL data, health checks, graceful shutdown, and a disposable first-result smoke check.
- A documented logical PostgreSQL backup and clean-restore procedure with deterministic Organization, maintenance, monitoring, Incident, Alert, and encrypted Webhook recovery verification.
- A deterministic packaged schema-upgrade exercise with persisted-evidence verification and a documented restore-based rollback boundary.
- A documented raw-evidence retention and troubleshooting workflow with a disposable package exercise that preserves current and durable evidence while expiring old raw partitions.

The current source contains the complete supported surface proposed for the first
pre-release candidate. Release-candidate validation is complete; owner-controlled
publication remains pending. [CHANGELOG.md](CHANGELOG.md) records the candidate boundaries.

## Get Started

ProbeHive does not yet publish images or releases. Build and run the supported
production-like package with the [self-hosted installation guide](docs/installation.md).
For source development, use the [local development guide](docs/development.md).

Useful project references:

- [Self-hosted installation](docs/installation.md)
- [Backend contract](docs/backend-contract.md)
- [Unreleased changelog](CHANGELOG.md)
- [Architecture baseline](docs/architecture.md)
- [Contributing guide](CONTRIBUTING.md)
- [Security policy](SECURITY.md)

## Current Focus

The candidate feature surface, operator documentation, and complete release-candidate
validation matrix are complete. The owner still controls version selection, signing,
tagging, and publication. No release or availability claim is made.

New Check Types, remote Agents and Probe Locations, identity expansion, richer status
pages, CLI, monitoring as code, Kubernetes packaging, high availability, and hosted-service
implementation are not part of this candidate.

## Architecture

ProbeHive is a feature-oriented Go modular monolith. Commands are composition roots; feature packages own their domain behavior and persistence ports; PostgreSQL and HTTP packages adapt those ports.

```text
cmd/probehive/
internal/
  organization/
  user/
  monitor/
  maintenance/
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

ProbeHive Cloud is the name reserved for a separately maintained future official hosted
service. No hosted service is included or made available by this repository. Any future
service must consume released public contracts or artifacts; the self-hosted product does
not require it.

## Contributing

Read [CONTRIBUTING.md](CONTRIBUTING.md) before proposing a change. Contributions use Developer Certificate of Origin sign-off. Participation follows the community standards in [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md). Security reports must follow [SECURITY.md](SECURITY.md) and must not be filed as public issues.

## License and Trademarks

Source code and documentation in this repository are licensed under the [Apache License 2.0](LICENSE), unless an included third-party artifact states otherwise. See [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) for dependency notices.

The license does not grant unrestricted rights to the ProbeHive name, logo, or visual identity. See [TRADEMARKS.md](TRADEMARKS.md).
