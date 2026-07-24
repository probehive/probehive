#!/usr/bin/env bash
# Playwright API web server: resets the dedicated end-to-end database, builds
# the Go API, and serves it on the port the Vite development proxy targets. API
# startup applies the embedded migrations before accepting requests.
set -euo pipefail

PGHOST="${PROBEHIVE_E2E_PGHOST:-127.0.0.1}"
PGPORT="${PROBEHIVE_E2E_PGPORT:-5432}"
PGUSER="${PROBEHIVE_E2E_PGUSER:-probehive}"
PGPASSWORD="${PROBEHIVE_E2E_PGPASSWORD:-probehive}"
PGMAINTENANCE_DB="${PROBEHIVE_E2E_PGDATABASE:-probehive}"
E2E_DB="probehive_e2e"
export PGHOST PGPORT PGUSER PGPASSWORD

psql -h "$PGHOST" -p "$PGPORT" -U "$PGUSER" -d "$PGMAINTENANCE_DB" -v ON_ERROR_STOP=1 \
  -c "DROP DATABASE IF EXISTS ${E2E_DB} WITH (FORCE)" \
  -c "CREATE DATABASE ${E2E_DB}"

# pgx merges sslmode=disable below with these libpq-compatible PG* variables,
# keeping the API on the same server and database that psql just reset.
export PGDATABASE="$E2E_DB"

REPOSITORY_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
GO_COMMAND="${PROBEHIVE_GO:-go}"
API_BINARY="$(mktemp "${TMPDIR:-/tmp}/probehive-e2e.XXXXXXXX")"
API_PID=""
cleanup() {
  if [[ -n "$API_PID" ]]; then
    kill "$API_PID" 2>/dev/null || true
    wait "$API_PID" 2>/dev/null || true
  fi
  rm -f "$API_BINARY"
}
trap cleanup EXIT INT TERM

(
  cd "$REPOSITORY_ROOT"
  "$GO_COMMAND" build -mod=readonly -o "$API_BINARY" ./cmd/probehive
)

PROBEHIVE_ENVIRONMENT=Development \
  PROBEHIVE_HTTP_ADDRESS=127.0.0.1:5080 \
  PROBEHIVE_DATABASE_URL="sslmode=disable" \
  "$API_BINARY" &
API_PID=$!
wait "$API_PID"
