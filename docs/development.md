# Local Development

This guide covers the backend and frontend development loop for the current unreleased
pre-release candidate. It is not an installation or published compatibility guide.

## Prerequisites

- Go 1.26.5, pinned by `go.mod` and `.mise.toml`.
- Node.js 24, the supported LTS major recorded in `web/package.json`.
- Docker or rootless Podman for the development PostgreSQL database.

CI installs and reports the same exact Go toolchain. Do not use an older toolchain or permit an implicit module graph change during validation.

## Development Database

Start the disposable PostgreSQL service:

```bash
docker compose -f deploy/compose/compose.dev.yaml up -d
```

The compose file publishes PostgreSQL on `127.0.0.1:5432` with the sanitized development credentials `probehive` / `probehive` and database `probehive`. These values exist only for local development.

Set the API connection URL:

```bash
export PROBEHIVE_DATABASE_URL='postgresql://probehive:probehive@127.0.0.1:5432/probehive?sslmode=disable'
```

The API applies embedded migrations transactionally at startup. Applied versions are recorded in `schema_migrations`.

## Run the API

```bash
PROBEHIVE_ENVIRONMENT=Development \
PROBEHIVE_HTTP_ADDRESS=127.0.0.1:5080 \
go run -mod=readonly ./cmd/probehive
```

Development exposes the OpenAPI document at `/openapi/v1.json`. Liveness is `/healthz`; readiness, including PostgreSQL connectivity, is `/readyz`.

The supported environment variables are:

| Variable | Meaning | Default |
| --- | --- | --- |
| `PROBEHIVE_DATABASE_URL` | pgx PostgreSQL connection URL for the API | required |
| <code>PROBEHIVE_DATABASE_URL_FILE</code> | UTF-8 file containing the database URL; mutually exclusive with <code>PROBEHIVE_DATABASE_URL</code> | unset |
| `PROBEHIVE_HTTP_ADDRESS` | `net/http` listen address | `:8080` |
| `PROBEHIVE_ENVIRONMENT` | `Development` enables plain-HTTP development cookies and OpenAPI | production behavior |
| `PROBEHIVE_CREDENTIAL_ATTEMPTS_PER_MINUTE` | shared setup/login permits per client address in each fixed minute | `10` |
| `PROBEHIVE_PUBLIC_ORIGIN` | exact external `http://host` or `https://host` origin used behind a gateway | request scheme and Host |
| `PROBEHIVE_WEBHOOK_KEYRING` | ordered `keyId:base64url32` AES-256-GCM keys; first key is active and retained secrets are rewrapped at startup | Webhook creation unavailable |
| <code>PROBEHIVE_WEBHOOK_KEYRING_FILE</code> | UTF-8 file containing the keyring; mutually exclusive with <code>PROBEHIVE_WEBHOOK_KEYRING</code> | unset |

### Webhook wrapping-key operations

`PROBEHIVE_WEBHOOK_KEYRING` is a comma-separated list with no surrounding whitespace.
Each entry is `keyId:base64url`. The 1-32 character id starts with a lowercase ASCII letter
or digit; its remaining characters may also contain periods, underscores, and hyphens. The
unpadded base64url value decodes to exactly 32 cryptographically random bytes. The first entry is active for new encryption;
later entries are retained only to decrypt and rewrap existing rows. For local development,
one key can be generated without writing it to the repository:

```bash
KEY=$(openssl rand -base64 32 | tr '+/' '-_' | tr -d '=\n')
export PROBEHIVE_WEBHOOK_KEYRING="key-2026-07:$KEY"
```

Rotate a wrapping key without losing access to signing secrets:

1. Generate a new independent 32-byte key with a new id, prepend it to the deployed
   keyring, and retain every prior key.
2. Restart each API process with that complete keyring. Startup authenticates retained
   ciphertext and rewraps old-key rows under the new first key before serving requests.
3. Confirm that every non-retired row names the new id:

   ```sql
   SELECT DISTINCT wrapping_key_id
   FROM webhook_signing_secrets
   WHERE state <> 'retired';
   ```

4. Only after all processes use the new keyring and the query returns only the new id,
   remove old keys in a later deployment.

Back up the complete keyring in the operator's secret backup system, separately from the
database but with the same recovery coverage. A database backup is not restorable while
any retained row refers to a key id whose material is missing. Never commit, log, or place
real keyring values in database backups or general configuration archives. An empty
keyring permits startup only when no retained Webhook secret exists; creation then returns
`503` until a keyring is configured.

The embedded worker executes checks inside the same process and is configured
separately. Every value below is an operator ceiling or floor; user configuration may be
stricter but never looser:

| Variable | Meaning | Default |
| --- | --- | --- |
| `PROBEHIVE_WORKER_ENABLED` | run the embedded scheduler and executor; `false` gives an API-only replica | `true` |
| `PROBEHIVE_PROBE_LOCATION` | Probe Location identifier every Run this process claims carries | `local` |
| `PROBEHIVE_MINIMUM_INTERVAL_SECONDS` | operator floor on execution interval; may be raised but never lowered below 30 | `30` |
| `PROBEHIVE_EXECUTION_CEILING_SECONDS` | operator ceiling on one whole execution, and the basis of the lease duration | `60` |
| `PROBEHIVE_SCHEDULER_TICK_SECONDS` | how often due slots are looked for | `5` |
| `PROBEHIVE_WORKER_CONCURRENCY` | in-flight executions shared by scheduled, confirmation, and manual Runs | `8` |
| `PROBEHIVE_RETENTION_DAYS` | raw Run and Observation retention; whole partitions are dropped, so effective retention exceeds this by up to a month | `30` |
| `PROBEHIVE_OUTBOUND_PROFILE` | `managed` or `private`; `operator` is rejected here because it is never tenant-reachable | `private` |
| `PROBEHIVE_OUTBOUND_ALLOWED_CIDRS` | comma-separated prefixes the `private` profile opts back in; metadata endpoints stay denied | empty |
| `PROBEHIVE_OUTBOUND_ALLOWED_PORTS` | comma-separated destination port ceiling | `80,443` |
| `PROBEHIVE_OUTBOUND_RESOLVERS` | comma-separated `address:port` resolvers every query is confined to | the host resolver |
| `PROBEHIVE_RESOLVER_TIMEOUT_SECONDS` | bound on one resolver query connection | `5` |
| `PROBEHIVE_CONNECT_TIMEOUT_SECONDS` | bound on one connection attempt | `10` |
| `PROBEHIVE_PROBE_ROOT_CA_FILE` | PEM roots trusted for probe TLS instead of the host's, for an internal certificate authority | host roots |

There is no setting that disables TLS verification. Leaving
`PROBEHIVE_OUTBOUND_RESOLVERS` unset uses the host's resolver, which is operator-controlled
only in the sense that the operator controls the machine; an installation that wants an
auditable answer path names its resolvers explicitly.

A Monitor becomes due on a series derived from its identifier and interval, so slot instants
are reproducible without coordination and Monitors sharing an interval do not all fire in the
same second. Partition maintenance runs at startup and every six hours; an
installation whose worker never runs eventually cannot insert a Run, because there is no
default partition to fall back on.

`POST /api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/runs`
executes the latest revision synchronously and returns the completed manual Run. An explicit
request may exercise a draft or paused Monitor. It does not queue when the shared worker
capacity is occupied, and an API-only process with `PROBEHIVE_WORKER_ENABLED=false` returns
`503` instead of executing outbound traffic.

## First Administrator and Sessions

Every `/api/v1` endpoint except setup status, first-administrator creation, login,
and antiforgery issuance requires an authenticated browser session. A fresh
installation reports `{"setupComplete":false}` at `GET /api/v1/setup/status`.
`POST /api/v1/setup/admin` creates the first administrator exactly once,
provisions the installation Organization with slug `default` and its default
Project, and signs the administrator in. It returns both the user and that
Organization, so no separate Organization step is needed before creating a
Monitor. Setup also makes that administrator the Organization's first member,
which grants access: every endpoint under `/api/v1/organizations/{id}/` resolves
membership and checks a permission, and a non-member receives `404` rather than
`403`. The instance `Administrator` role alone grants no access to an
Organization's monitoring data.

Unsafe requests need the token from `GET /api/v1/auth/antiforgery` echoed in the response-named `X-ProbeHive-Antiforgery` header. The token is bound to the current anonymous or authenticated identity, so fetch a fresh token after setup, login, or logout.

An example development flow:

```bash
JAR=$(mktemp)
TOKEN=$(curl -s -c "$JAR" http://localhost:5080/api/v1/auth/antiforgery | jq -r .requestToken)
curl -s -b "$JAR" -c "$JAR" -X POST http://localhost:5080/api/v1/setup/admin \
  -H 'Content-Type: application/json' -H "X-ProbeHive-Antiforgery: $TOKEN" \
  -d '{"email":"admin@example.test","displayName":"Admin","password":"a-long-admin-password"}'
TOKEN=$(curl -s -b "$JAR" -c "$JAR" http://localhost:5080/api/v1/auth/antiforgery | jq -r .requestToken)
curl -s -b "$JAR" http://localhost:5080/api/v1/organizations
```

The setup response already contains the installation Organization at `.organization`, so
the listing above is only for inspection. Provision an additional Organization when you
want one:

```bash
curl -s -b "$JAR" -X POST http://localhost:5080/api/v1/organizations \
  -H 'Content-Type: application/json' -H "X-ProbeHive-Antiforgery: $TOKEN" \
  -d '{"slug":"acme","displayName":"Acme Monitoring"}'
```

Only SHA-256 token digests are stored in PostgreSQL. Session cookies are host-only, `HttpOnly`, `SameSite=Lax`, fixed at 12 hours, and never renewed by reads. Authenticated antiforgery selector and request-token digests are also server-side; anonymous tokens are validated with one PostgreSQL-backed HMAC key. Cookies are unconditionally `Secure` outside Development.

The server does not implicitly trust forwarded headers. A TLS-terminating production gateway must preserve the public `Host` and set `PROBEHIVE_PUBLIC_ORIGIN` to the exact browser origin, or use HTTPS on the upstream connection so the request scheme remains visible. Proxy-supplied client addresses are not used for credential rate-limit partitions.

## Monitors and Revisions

Monitors nest under their Organization and Project. With the session and token from above, and the `organization.id` / `organization.defaultProject.id` values that setup returned:

```bash
ORG=<organization id>; PROJ=<default project id>
BASE="http://localhost:5080/api/v1/organizations/$ORG/projects/$PROJ/monitors"
MON=$(curl -s -b "$JAR" -X POST "$BASE" \
  -H 'Content-Type: application/json' -H "X-ProbeHive-Antiforgery: $TOKEN" \
  -d '{"name":"Checkout heartbeat","checkType":"http"}' | jq -r .id)
curl -s -b "$JAR" -X POST "$BASE/$MON/revisions" \
  -H 'Content-Type: application/json' -H "X-ProbeHive-Antiforgery: $TOKEN" \
  -d '{"checkSchemaVersion":1,"checkConfiguration":{"url":"https://example.test/health"}}'
curl -s -b "$JAR" -X PUT "$BASE/$MON/state" \
  -H 'Content-Type: application/json' -H "X-ProbeHive-Antiforgery: $TOKEN" \
  -d '{"state":"active"}'
```

A Monitor starts in `draft` and cannot activate until it has a revision. Revisions are immutable, append-only, and strictly numbered from 1. Configuration is validated against the Monitor's check type and integer schema version; `http` currently supports version 1. Lifecycle targets are `active`, `paused`, and `archived`; `archived` is terminal and read-only.

## Run the Web Application

```bash
npm --prefix web ci
npm --prefix web run dev
```

The Vite development server proxies `/api` to `http://localhost:5080`, so run the API alongside it. Production deployments serve the static build behind a same-origin gateway. npm lifecycle scripts stay disabled through `web/.npmrc`; no current dependency needs an install script.
The production-like container build and operator startup path are documented in
[installation.md](installation.md).

## Validation

Backend checks:

```bash
go version
go mod verify
test -z "$(gofmt -l .)"
go vet -mod=readonly ./...
go test -mod=readonly -race ./...
go build -mod=readonly ./cmd/probehive
```

PostgreSQL integration tests require a disposable database URL. Each test creates and drops an isolated schema:

```bash
export PROBEHIVE_TEST_DATABASE_URL='postgresql://probehive:probehive@127.0.0.1:5432/probehive?sslmode=disable'
go test -mod=readonly -race ./internal/postgres
```

Without the variable, integration tests explicitly skip. No passing claim should include those tests unless the variable was set and PostgreSQL was reachable.

Frontend checks:

```bash
npm --prefix web ci
npm --prefix web run lint
npm --prefix web run typecheck
npm --prefix web test
npm --prefix web run build
```

## Browser Journeys

Playwright runs the browser journeys against the real Go API and a dedicated
`probehive_e2e` database. The first-run journey creates, exercises, fails, and
recovers HTTP Monitors through a later target revision. It also creates and enables
the bounded Webhook route through the Integrations UI, verifies that the one-time
signing secret disappears on reload, follows delivery evidence, and exercises the
Organization Incident inbox across active evidence, causal navigation, resolved history,
and a narrow viewport. A second journey switches
the interface to `zh-CN` and confirms the preference survives a reload. It requires
the development PostgreSQL service, Go, and Playwright Chromium:

```bash
npx --prefix web playwright install chromium
npm --prefix web run e2e
```

The Playwright launcher preserves the existing `psql` reset with `ON_ERROR_STOP=1` to recreate only `probehive_e2e`, builds a temporary `probehive` binary, and starts the API on `127.0.0.1:5080`. API startup applies the embedded migrations. Vite runs on `127.0.0.1:5173`. The first-run journey requires fresh state, so neither server is reused.

The default disposable PostgreSQL connection can be overridden with `PROBEHIVE_E2E_PGHOST`, `PROBEHIVE_E2E_PGPORT`, `PROBEHIVE_E2E_PGUSER`, `PROBEHIVE_E2E_PGPASSWORD`, and `PROBEHIVE_E2E_PGDATABASE`. The launcher maps the host, port, user, and password to libpq-compatible `PG*` variables for pgx while always setting the API database to `probehive_e2e`. Set `PROBEHIVE_GO` when the pinned Go executable is not named `go`.

To exercise the same journeys against an externally managed, freshly initialized
stack, set its browser origin and reachable fixture targets. Supplying
`PROBEHIVE_E2E_BASE_URL` disables the development API and Vite launchers:

```bash
PROBEHIVE_E2E_BASE_URL=https://localhost:18443 \
PROBEHIVE_E2E_PASSING_TARGET=https://web:8443/readyz \
PROBEHIVE_E2E_FAILING_TARGET=https://web:8443/assets/probehive-e2e-intentional-404 \
PROBEHIVE_E2E_RECOVERY_TARGET=https://web:8443/readyz \
PROBEHIVE_E2E_WEBHOOK_TARGET=https://web:8443/healthz \
npm --prefix web run e2e
```

The external stack must be disposable and permit only the fixture destinations
required by the journey. A self-signed HTTPS base URL is accepted only by the
test browser context; application probes still require their configured trust
roots and never disable TLS verification.
The journey runs the failing target once manually before activation and stops
immediately unless it produces failed evidence; manual Runs do not advance
evaluated health or create Incidents.

## Changing Dependencies

Discover versions with current Go tooling and approved sources. After reviewing ownership, support, advisories, transitive dependencies, and exact-version licenses:

```bash
go get module/path@reviewed-version
go mod tidy
go mod verify
go test -mod=readonly ./...
```

Review `go.mod`, `go.sum`, and `go list -m all` together. Do not configure a checksum-database bypass or commit a local proxy setting.

## Adding a Migration

Add the next sequential `internal/postgres/migrations/NNNN_description.sql` file. Migrations are embedded in the binary, run in version order under a session-level advisory lock, and record their version only in the same transaction as the schema change. Never edit an applied migration; add a new one.
