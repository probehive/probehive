# Local Development

This guide covers the backend and frontend development loop for the current foundation phase.

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
| `PROBEHIVE_HTTP_ADDRESS` | `net/http` listen address | `:8080` |
| `PROBEHIVE_ENVIRONMENT` | `Development` enables plain-HTTP development cookies and OpenAPI | production behavior |
| `PROBEHIVE_CREDENTIAL_ATTEMPTS_PER_MINUTE` | shared setup/login permits per client address in each fixed minute | `10` |
| `PROBEHIVE_PUBLIC_ORIGIN` | exact external `http://host` or `https://host` origin used behind a gateway | request scheme and Host |

## First Administrator and Sessions

Every `/api/v1` endpoint except setup status, first-administrator creation, login, and antiforgery issuance requires an authenticated browser session. A fresh installation reports `{"setupComplete":false}` at `GET /api/v1/setup/status`. `POST /api/v1/setup/admin` creates the first administrator exactly once and signs it in.

Unsafe requests need the token from `GET /api/v1/auth/antiforgery` echoed in the response-named `X-ProbeHive-Antiforgery` header. The token is bound to the current anonymous or authenticated identity, so fetch a fresh token after setup, login, or logout.

An example development flow:

```bash
JAR=$(mktemp)
TOKEN=$(curl -s -c "$JAR" http://localhost:5080/api/v1/auth/antiforgery | jq -r .requestToken)
curl -s -b "$JAR" -c "$JAR" -X POST http://localhost:5080/api/v1/setup/admin \
  -H 'Content-Type: application/json' -H "X-ProbeHive-Antiforgery: $TOKEN" \
  -d '{"email":"admin@example.test","displayName":"Admin","password":"a-long-admin-password"}'
TOKEN=$(curl -s -b "$JAR" -c "$JAR" http://localhost:5080/api/v1/auth/antiforgery | jq -r .requestToken)
curl -s -b "$JAR" -X POST http://localhost:5080/api/v1/organizations \
  -H 'Content-Type: application/json' -H "X-ProbeHive-Antiforgery: $TOKEN" \
  -d '{"slug":"acme","displayName":"Acme Monitoring"}'
```

Only SHA-256 token digests are stored in PostgreSQL. Session cookies are host-only, `HttpOnly`, `SameSite=Lax`, fixed at 12 hours, and never renewed by reads. Authenticated antiforgery selector and request-token digests are also server-side; anonymous tokens are validated with one PostgreSQL-backed HMAC key. Cookies are unconditionally `Secure` outside Development.

The server does not implicitly trust forwarded headers. A TLS-terminating production gateway must preserve the public `Host` and set `PROBEHIVE_PUBLIC_ORIGIN` to the exact browser origin, or use HTTPS on the upstream connection so the request scheme remains visible. Proxy-supplied client addresses are not used for credential rate-limit partitions.

## Monitors and Revisions

Monitors nest under their Organization and Project. With the session and token from above, and the `id` / `defaultProject.id` values returned by Organization creation:

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

Playwright runs the unchanged browser journey against the real Go API and a dedicated `probehive_e2e` database. It requires the development PostgreSQL service, Go, and Playwright Chromium:

```bash
npx --prefix web playwright install chromium
npm --prefix web run e2e
```

The Playwright launcher preserves the existing `psql` reset with `ON_ERROR_STOP=1` to recreate only `probehive_e2e`, builds a temporary `probehive` binary, and starts the API on `127.0.0.1:5080`. API startup applies the embedded migrations. Vite runs on `127.0.0.1:5173`. The first-run journey requires fresh state, so neither server is reused.

The default disposable PostgreSQL connection can be overridden with `PROBEHIVE_E2E_PGHOST`, `PROBEHIVE_E2E_PGPORT`, `PROBEHIVE_E2E_PGUSER`, `PROBEHIVE_E2E_PGPASSWORD`, and `PROBEHIVE_E2E_PGDATABASE`. The launcher maps the host, port, user, and password to libpq-compatible `PG*` variables for pgx while always setting the API database to `probehive_e2e`. Set `PROBEHIVE_GO` when the pinned Go executable is not named `go`.

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
