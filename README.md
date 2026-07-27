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
- A React administration application in English and Simplified Chinese, rendering instants in the viewer's time zone, with Playwright browser journeys.

Check execution, scheduling, incidents, alert delivery, status pages, Agents, and packaged releases remain under development.

## Get Started

ProbeHive does not yet publish installation artifacts. To run the current foundation locally, start with the [local development guide](docs/development.md).

Useful project references:

- [Backend contract](docs/backend-contract.md)
- [Architecture decisions](docs/adr/README.md)
- [Contributing guide](CONTRIBUTING.md)
- [Security policy](SECURITY.md)

## Planned Capabilities

- HTTP and HTTPS, TCP and TLS, ICMP ping, DNS, heartbeat, and certificate checks.
- Embedded and outbound private Agents with bounded execution and renewable identity.
- Organization and Project isolation, incidents, maintenance, alerts, and status pages.
- A versioned HTTP API, CLI, monitoring as code, and a static React administration application.
- PostgreSQL persistence, OpenTelemetry integration, and Compose-based self-hosting for rootless Podman and Docker.

## Architecture

ProbeHive is a feature-oriented Go modular monolith. Commands are composition roots; feature packages own their domain behavior and persistence ports; PostgreSQL and HTTP packages adapt those ports.

```text
cmd/probehive/
internal/
  organization/
  user/
  monitor/
  check/
  postgres/
  httpapi/
  httpapi/v1/
web/
deploy/
```

Feature packages and `internal/check` use only the Go standard library. `internal/postgres` implements feature-owned persistence interfaces with pgx and embedded SQL migrations. `internal/httpapi` owns HTTP routing, browser security, versioned wire types, and Problem Details. The frontend remains a separately deployable API client and owns no authoritative authorization or business rules.

The backend uses Go 1.26.5 and PostgreSQL. First-party web applications use React, strict TypeScript, Vite, and React Router and build to static assets. The public API begins at `/api/v1`.

## ProbeHive Cloud

ProbeHive Cloud is the separately maintained official hosted service. It runs released public ProbeHive artifacts in shared multi-tenant service pools and adds proprietary account lifecycle, billing, metering, managed-location operations, abuse controls, support, and compliance services. The self-hosted product does not require the hosted service.

## Contributing

Read [CONTRIBUTING.md](CONTRIBUTING.md) before proposing a change. Contributions use Developer Certificate of Origin sign-off. Participation follows the community standards in [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md). Security reports must follow [SECURITY.md](SECURITY.md) and must not be filed as public issues.

## License and Trademarks

Source code and documentation in this repository are licensed under the [Apache License 2.0](LICENSE), unless an included third-party artifact states otherwise. See [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) for dependency notices.

The license does not grant unrestricted rights to the ProbeHive name, logo, or visual identity. See [TRADEMARKS.md](TRADEMARKS.md).
