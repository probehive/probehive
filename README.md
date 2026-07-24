# ProbeHive

ProbeHive is an open-source distributed synthetic monitoring and availability platform. It runs reliable checks from the networks that matter, turns observations into trustworthy incidents, and communicates service health clearly.

This repository contains the complete self-hosted product. It is designed to remain secure, useful, and operable without a ProbeHive Cloud account.

## Status

ProbeHive is in its foundation phase. The current Go backend implements the M1 through M5 vertical slice: idempotent Organization provisioning with a transactional default Project; first-administrator setup; PostgreSQL-backed browser sessions with antiforgery, origin validation, fixed expiry, and deny-by-default authorization; and Monitors with immutable revisions and strict `http` check configuration validation. The React application and its Playwright journey cover setup, sign-in, sign-out, and Organization creation.

No public release exists yet. Unreleased source may change while the first vertical slice is built. Once an API, schema, event, package, generated client, Agent protocol, or OCI artifact is published, its explicit compatibility contract applies; breaking changes do not silently replace a released `/api/v1` or schema version.

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

Architecture decisions are recorded in [docs/adr](docs/adr/README.md). The exact rewrite contract is recorded in [docs/backend-contract.md](docs/backend-contract.md), and the local development loop is documented in [docs/development.md](docs/development.md).

## ProbeHive Cloud

ProbeHive Cloud is the separately maintained official hosted service. It runs released public ProbeHive artifacts in shared multi-tenant service pools and adds proprietary account lifecycle, billing, metering, managed-location operations, abuse controls, support, and compliance services. The self-hosted product does not require the hosted service.

## Contributing

Read [CONTRIBUTING.md](CONTRIBUTING.md) before proposing a change. Contributions use Developer Certificate of Origin sign-off. Participation follows the community standards in [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md). Security reports must follow [SECURITY.md](SECURITY.md) and must not be filed as public issues.

## License and Trademarks

Source code and documentation in this repository are licensed under the [Apache License 2.0](LICENSE), unless an included third-party artifact states otherwise. See [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) for dependency notices.

The license does not grant unrestricted rights to the ProbeHive name, logo, or visual identity. See [TRADEMARKS.md](TRADEMARKS.md).
