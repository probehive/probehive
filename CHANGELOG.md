# Changelog

This project has not published a release. This file describes the current unreleased
pre-release candidate and must not be read as a version, availability, or compatibility
announcement. The owner controls version selection, signing, tagging, and publication.

## Unreleased

### Candidate surface

- Source-built, production-like Compose installation with a non-root TLS gateway, one Go
  application, one PostgreSQL database, external file secrets, health checks, persistent
  data, and graceful shutdown.
- First-administrator setup, Organization membership, browser sessions, antiforgery and
  origin validation, and permission-based tenant authorization.
- Versioned HTTP Monitor configuration, scheduled and manual Runs, bounded HTTP
  Observations, health evaluation, Incidents, immutable Alerts, and evidence views.
- Monitor-scoped one-time maintenance windows. Maintenance preserves monitoring facts and
  terminally suppresses matching event-time Webhook routes without a network attempt.
- Signed HTTPS Webhook Integrations with encrypted one-time-disclosed secrets, bounded
  retries, two-phase secret rotation, and redacted delivery evidence.
- Private status-page configuration and revocable anonymous publication of only selected
  labels, current evaluated state, update instants, and active-maintenance presentation.
- English and Simplified Chinese static React administration UI.

### Operator boundaries

- The only Check Type is `http` schema version 1. The embedded worker reports one local
  Probe Location; remote Agents and distributed quorum are not included.
- The supported package path builds images from a reviewed source revision. No registry
  images, binaries, installers, hosted service, Kubernetes package, or high-availability
  topology is published.
- The Compose package is single-node. Only its TLS gateway is published by default, on
  loopback; remote exposure requires operator-supplied TLS, exact public origin, firewall,
  ingress, resolver, and outbound-policy configuration.
- Raw Runs and Observations default to a 30-day floor and expire by whole monthly
  partitions. Durable health transitions, Incidents, Alerts, delivery evidence, and
  configuration are not removed by raw-evidence retention.
- Backups are PostgreSQL custom-format logical dumps plus the complete Webhook wrapping
  keyring. Operators choose backup retention and must test a clean restore; no recovery
  point or recovery time guarantee is offered.
- Schema migrations are forward-only. Before upgrade, stop writers and take a verified
  backup. After any newer API startup attempt, rollback means restoring that backup into a
  clean volume and running the prior reviewed source revision.
- The packaged upgrade exercise proves only the penultimate-to-current migration set in
  the checked-out tree. It does not promise arbitrary historical upgrade paths, downgrade,
  cross-version database use, or compatibility between unreleased revisions.
- Public status URLs are intentionally shareable read capabilities. They make no uptime,
  history, or availability guarantee and can be revoked immediately.

### Security and compatibility

- Browser sessions remain server-side and browser credentials are not stored in browser
  storage. Tenant-influenced network destinations pass through the shared outbound policy;
  TLS verification cannot be disabled.
- Secret-bearing payloads, Webhook signing secrets, raw status capabilities, and database
  credentials must not be logged. Anonymous status lookup failures are indistinguishable.
- `/api/v1` identifies the current API namespace, not a published stable release line.
  While this candidate is unreleased, HTTP, event, schema, package, and configuration
  behavior may change. Published compatibility will begin only with an owner-published
  release and its migration guidance.
- See [SECURITY.md](SECURITY.md) for private vulnerability reporting. No security response
  or remediation-time service level is offered before a supported release policy exists.

### Legal and publication

Dependency and container-image versions are recorded in `go.mod`, `go.sum`,
`web/package-lock.json`, the pinned Containerfiles, and
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md). License review, legal conclusions,
cryptographic signing, release identity, publication, and announcement remain
owner-controlled decisions.
