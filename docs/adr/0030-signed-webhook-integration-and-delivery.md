# 0030: Signed Webhook Integration and Delivery

- Status: Accepted
- Date: 2026-07-31
- Clarifies: [ADR 0029](0029-alert-intent-and-delivery-attempt-semantics.md)
- Related: [ADR 0007](0007-outbound-access-and-ssrf-security.md), [ADR 0009](0009-tenant-scope-default-project-and-telemetry.md), [ADR 0017](0017-organization-membership-and-authorization.md), [ADR 0019](0019-internationalization-and-error-codes.md)

## Context

ADR 0029 defines immutable Alert intents and append-only Delivery Attempts but forbids
notification network I/O until the first channel records routing, secret storage, signing,
replay, retry, and outbound-security decisions. The product blueprint puts a signed generic
webhook first because it covers providers ProbeHive does not integrate directly.

A tenant-influenced webhook can become an SSRF, metadata-access, credential-exfiltration, or
resource-exhaustion path. Its signing secret must remain recoverable for future calls, so a
one-way hash is insufficient and plaintext database storage is unacceptable.

The first implementation slice only creates and lists disabled Integrations. It establishes
the configuration and encrypted-secret boundary without claiming that delivery is active.

## Decision

### Organization-scoped Integrations and routing

A signed Webhook Integration belongs to one Organization. It has a UUIDv7 id, Organization
id, display name, HTTPS destination URL, enabled state, optimistic version, active secret
version, and UTC timestamps. Names are unique by exact, case-sensitive stored value within
an Organization. At most five may be enabled per Organization.

Phase 1 routes every new `incident.opened` and `incident.resolved` Alert to every
Integration enabled when that Alert is projected. It does not backfill older Alerts.
Monitor, Project, label, schedule, maintenance, or escalation routing is deferred.

The API begins at:

```text
/api/v1/organizations/{organizationId}/webhook-integrations
```

Configuration reads and writes require the internal `integration.manage` permission.
Administrators receive it; Viewers do not. Viewer-visible delivery evidence omits
destination URLs, secrets, and provider content.

An Integration is created disabled. Creation generates its first active signing secret and
returns the encoded secret exactly once. Ordinary reads never return secret material,
ciphertext, nonces, or wrapping-key identifiers. The one-time response uses
`Cache-Control: no-store`.

### Encrypted signing secrets

Signing secrets are 32 random bytes exposed as `phwh_` plus unpadded base64url. The complete
encoded value is the future HMAC key.

PostgreSQL stores only AES-256-GCM ciphertext, a fresh 96-bit nonce, and a non-secret key id.
Associated data binds Organization, Integration, and secret version. The operator supplies
an ordered keyring: the first key encrypts new writes and later keys decrypt retained rows.

Startup validates retained secrets and re-encrypts rows using older keys under the active
key before Webhook configuration becomes available. Missing keys, authentication failures,
malformed ciphertext, and failed rewraps fail closed. A process may run without a keyring
only while no retained Webhook secret exists; creation is then unavailable.

Signing-secret rotation is two-phase. Preparation creates one pending secret and returns it
once. Activation selects it for future calls and leaves the former secret retiring until an
Administrator explicitly retires it. Retiring removes ciphertext but retains bounded audit
metadata. Rotation endpoints are a later slice.

### Strict destination profile

Destinations are absolute HTTPS URLs at most 2,048 bytes. User information, query strings,
fragments, and empty authorities are invalid. HTTP has no tenant or operator escape hatch.

Delivery uses ADR 0007's shared Policy and validating Dialer, checks the URL again when used,
validates and binds every connection, and follows no redirects. Host roots are the default;
a dedicated operator-controlled Webhook CA file may be added with delivery. TLS verification
cannot be disabled. Destination URLs never enter metrics, delivery logs, Alerts, attempts,
or payloads.

### Versioned signing contract

The deterministic JSON payload contains a schema version, stable routed-delivery id, Alert
id and kind, Organization, Project, Monitor, and Incident ids, source Incident version,
occurrence time, and attempt sequence. It contains no prose, Monitor name, target URL,
Observation, credential, or rendered provider content.

Version 1 signs these exact bytes:

```text
v1\n<delivery-id>\n<unix-timestamp>\n<attempt>\n<secret-version>\n<raw-json-body>
```

The signature is unpadded base64url HMAC-SHA256 using the complete `phwh_...` secret bytes.
Headers carry the version, stable delivery id, Unix timestamp, attempt sequence, secret
version, and signature. Receivers compare in constant time, reject timestamps outside five
minutes, and deduplicate the stable delivery id. Retries keep that id but use a fresh
timestamp, sequence, and signature.

### Bounded Delivery Attempts

Each real call appends one Delivery Attempt. An in-progress row is durable before the call.
A crash or lost lease can leave the result uncertain, so a retry uses the same delivery id
and a new attempt sequence. Receivers must tolerate duplicates.

Phase 1 bounds are:

- HTTPS only, zero redirects, and a ten-second total timeout.
- Request body at most 16 KiB and at most four concurrent calls per process.
- No response body retention.
- Any `2xx` succeeds.
- Network errors, timeout, `408`, `425`, `429`, and `5xx` retry.
- Other `3xx` and `4xx` responses fail terminally.
- At most five real calls, using the existing bounded exponential delay and jitter.

Attempts retain only integration and secret versions, sequence, instants, outcome, HTTP
status when present, and a stable allowlisted failure code. URL, response bodies, arbitrary
headers, signing material, and provider text are never retained.

### No new dependency

Secret generation, AES-GCM, HMAC-SHA256, JSON, TLS, and HTTP use the Go standard library.
External secret managers and provider SDKs remain future dependency and infrastructure
decisions.

## Consequences

- ProbeHive gains a secure configuration boundary before making delivery claims.
- A database disclosure alone does not reveal usable signing secrets; operators must back up
  the wrapping keyring separately.
- Strict HTTPS and no redirects intentionally reject URL-token and redirect-based endpoints.
- This first slice performs no Webhook network I/O and creates no Delivery Attempt. Enabling,
  rotation, routing, delivery, and audit reads remain separate slices governed by this ADR.
