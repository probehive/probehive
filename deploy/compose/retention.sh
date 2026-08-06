#!/usr/bin/env bash
set -euo pipefail

export LC_ALL=C

script_dir="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
base_compose="$script_dir/compose.yaml"
fixture_sql="$script_dir/retention-fixture.sql"

for command_name in openssl mktemp timeout; do
  if ! command -v "$command_name" >/dev/null 2>&1; then
    printf 'Required command is unavailable: %s\n' "$command_name" >&2
    exit 1
  fi
done

project_name="probehive-retention-$$"
compose_args=(-f "$base_compose" -p "$project_name")
if podman compose version >/dev/null 2>&1 &&
  podman compose "${compose_args[@]}" ps >/dev/null 2>&1; then
  compose=(podman compose)
elif command -v podman-compose >/dev/null 2>&1 &&
  podman-compose "${compose_args[@]}" ps >/dev/null 2>&1; then
  compose=(podman-compose)
elif docker compose version >/dev/null 2>&1 &&
  docker compose "${compose_args[@]}" ps >/dev/null 2>&1; then
  compose=(docker compose)
else
  printf 'A reachable Podman Compose or Docker Compose engine is required.\n' >&2
  exit 1
fi

umask 077
temporary_dir="$(mktemp -d)"
failed=1
cleanup() {
  status=$?
  set +e
  trap - EXIT INT TERM
  if (( failed != 0 )); then
    "${compose[@]}" "${compose_args[@]}" ps >&2
    "${compose[@]}" "${compose_args[@]}" logs --no-color >&2
  fi
  "${compose[@]}" "${compose_args[@]}" down --volumes --remove-orphans >/dev/null 2>&1
  rm -rf -- "$temporary_dir"
  exit "$status"
}
trap cleanup EXIT INT TERM

"$script_dir/generate-secrets.sh" "$temporary_dir/secrets" >/dev/null
export PROBEHIVE_POSTGRES_PASSWORD_FILE="$temporary_dir/secrets/postgres-password"
export PROBEHIVE_DATABASE_URL_FILE="$temporary_dir/secrets/database-url"
export PROBEHIVE_WEBHOOK_KEYRING_FILE="$temporary_dir/secrets/webhook-keyring"
export PROBEHIVE_TLS_CERT_FILE="$temporary_dir/secrets/tls.crt"
export PROBEHIVE_TLS_KEY_FILE="$temporary_dir/secrets/tls.key"
export PROBEHIVE_PUBLIC_ORIGIN="https://localhost:18446"
export PROBEHIVE_WORKER_ENABLED=false
export PROBEHIVE_RETENTION_DAYS=30

timeout 300 "${compose[@]}" "${compose_args[@]}" up --detach --build api

postgres_psql() {
  "${compose[@]}" "${compose_args[@]}" exec -T postgres sh -c \
    'PGPASSWORD="$(cat /run/secrets/postgres_password)" exec psql --quiet --username=probehive --dbname=probehive --set=ON_ERROR_STOP=1 "$@"' \
    sh "$@"
}

postgres_query() {
  postgres_psql --tuples-only --no-align --command "$1"
}

wait_postgres_ready() {
  for _ in {1..60}; do
    if postgres_query 'SELECT 1' >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  printf 'PostgreSQL did not become ready within 60 seconds.\n' >&2
  return 1
}

wait_api_ready() {
  for _ in {1..120}; do
    if "${compose[@]}" "${compose_args[@]}" exec -T api \
      wget -q -O /dev/null http://127.0.0.1:8080/readyz >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  printf 'The API did not become ready within 120 seconds.\n' >&2
  return 1
}

wait_postgres_ready
wait_api_ready
"${compose[@]}" "${compose_args[@]}" stop api >/dev/null

postgres_psql --single-transaction <"$fixture_sql"

fixture_fingerprint="$(postgres_query "
SELECT concat_ws('|',
  (SELECT count(*) FROM runs),
  (SELECT count(*) FROM observations),
  (SELECT count(*) FROM health_transitions),
  (SELECT count(*) FROM incidents),
  (SELECT count(*) FROM incident_timeline_entries),
  (SELECT count(*) FROM alerts),
  (SELECT count(*) FROM monitors));")"
if [[ "$fixture_fingerprint" != "2|2|1|1|1|1|1" ]]; then
  printf 'The retention fixture did not match the expected evidence counts.\n' >&2
  exit 1
fi

expired_suffix="$(postgres_query \
  "SELECT to_char(date_trunc('month', now()) - interval '3 months', 'YYYY_MM');")"
retained_suffix="$(postgres_query \
  "SELECT to_char(date_trunc('month', now()), 'YYYY_MM');")"
expired_run_partition="runs_$expired_suffix"
expired_observation_partition="observations_$expired_suffix"
retained_run_partition="runs_$retained_suffix"
retained_observation_partition="observations_$retained_suffix"

export PROBEHIVE_WORKER_ENABLED=true
timeout 120 "${compose[@]}" "${compose_args[@]}" up --detach --force-recreate api
wait_api_ready

retention_applied=0
expected_fingerprint="0|0|1|1|1|1|1|1|1|t|t|t|t"
for _ in {1..60}; do
  actual_fingerprint="$(postgres_query "
SELECT concat_ws('|',
  (SELECT count(*) FROM runs
    WHERE id = '00000000-0000-7000-8000-000000000006'),
  (SELECT count(*) FROM observations
    WHERE run_id = '00000000-0000-7000-8000-000000000006'),
  (SELECT count(*) FROM runs
    WHERE id = '00000000-0000-7000-8000-000000000007'),
  (SELECT count(*) FROM observations
    WHERE run_id = '00000000-0000-7000-8000-000000000007'),
  (SELECT count(*) FROM health_transitions),
  (SELECT count(*) FROM incidents),
  (SELECT count(*) FROM incident_timeline_entries),
  (SELECT count(*) FROM alerts),
  (SELECT count(*) FROM monitors),
  to_regclass('$expired_run_partition') IS NULL,
  to_regclass('$expired_observation_partition') IS NULL,
  to_regclass('$retained_run_partition') IS NOT NULL,
  to_regclass('$retained_observation_partition') IS NOT NULL);")"
  if [[ "$actual_fingerprint" == "$expected_fingerprint" ]]; then
    retention_applied=1
    break
  fi
  sleep 1
done
if (( retention_applied == 0 )); then
  printf 'Retention did not remove only the expired raw evidence.\n' >&2
  exit 1
fi

api_logs="$("${compose[@]}" "${compose_args[@]}" logs --no-color api)"
if [[ "$api_logs" != *'expired Run partitions'* ||
  "$api_logs" != *"$expired_run_partition"* ||
  "$api_logs" != *"$expired_observation_partition"* ||
  "$api_logs" != *'"retentionDays":30'* ]]; then
  printf 'The API did not emit the expected partition-expiry signal.\n' >&2
  exit 1
fi

"${compose[@]}" "${compose_args[@]}" stop postgres >/dev/null
readiness_failed=0
for _ in {1..30}; do
  readiness_output="$("${compose[@]}" "${compose_args[@]}" exec -T api \
    wget -S -O - http://127.0.0.1:8080/readyz 2>&1 || true)"
  if [[ "$readiness_output" == *'503 Service Unavailable'* ||
    "$readiness_output" == *'Unhealthy'* ]]; then
    readiness_failed=1
    break
  fi
  sleep 1
done
if (( readiness_failed == 0 )); then
  printf 'Readiness did not report the stopped PostgreSQL dependency.\n' >&2
  exit 1
fi

"${compose[@]}" "${compose_args[@]}" start postgres >/dev/null
wait_postgres_ready
wait_api_ready

export PROBEHIVE_RETENTION_DAYS=731
set +e
invalid_retention_output="$("${compose[@]}" "${compose_args[@]}" run \
  --rm --no-deps api 2>&1)"
invalid_retention_status=$?
set -e
if (( invalid_retention_status == 0 )) ||
  [[ "$invalid_retention_output" != *'PROBEHIVE_RETENTION_DAYS: raw retention is 1 to 730 whole days'* ]]; then
  printf 'The packaged API did not reject an invalid retention window clearly.\n' >&2
  exit 1
fi

failed=0
printf 'Retention check passed: raw expiry, durable evidence, readiness, and startup diagnostics.\n'
