#!/usr/bin/env bash
set -euo pipefail

export LC_ALL=C

script_dir="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repository_root="$(CDPATH= cd -- "$script_dir/../.." && pwd)"
base_compose="$script_dir/compose.yaml"
fixture_sql="$script_dir/upgrade-fixture.sql"

for command_name in openssl mktemp timeout; do
  if ! command -v "$command_name" >/dev/null 2>&1; then
    printf 'Required command is unavailable: %s\n' "$command_name" >&2
    exit 1
  fi
done

project_name="probehive-upgrade-$$"
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
export PROBEHIVE_PUBLIC_ORIGIN="https://localhost:18445"
export PROBEHIVE_WORKER_ENABLED=false

timeout 120 "${compose[@]}" "${compose_args[@]}" up --detach postgres

postgres_psql() {
  "${compose[@]}" "${compose_args[@]}" exec -T postgres sh -c \
    'PGPASSWORD="$(cat /run/secrets/postgres_password)" exec psql --quiet --username=probehive --dbname=probehive --set=ON_ERROR_STOP=1 "$@"' \
    sh "$@"
}

postgres_query() {
  postgres_psql --tuples-only --no-align --command "$1"
}

postgres_ready=0
for _ in {1..60}; do
  if postgres_query 'SELECT 1' >/dev/null 2>&1; then
    postgres_ready=1
    break
  fi
  sleep 1
done
if (( postgres_ready == 0 )); then
  printf 'PostgreSQL did not become ready within 60 seconds.\n' >&2
  exit 1
fi

postgres_psql <<'SQL'
CREATE TABLE schema_migrations (
    version bigint NOT NULL,
    name text NOT NULL,
    applied_at timestamp with time zone NOT NULL DEFAULT now(),
    CONSTRAINT pk_schema_migrations PRIMARY KEY (version)
);
SQL

migration_files=("$repository_root"/internal/postgres/migrations/*.sql)
if (( ${#migration_files[@]} < 2 )); then
  printf 'At least two migrations are required to exercise an upgrade.\n' >&2
  exit 1
fi

baseline_count=$((${#migration_files[@]} - 1))
expected_version=1
expected_manifest=
for migration_file in "${migration_files[@]}"; do
  migration_name="${migration_file##*/}"
  if [[ ! "$migration_name" =~ ^([0-9]{4})_[a-z0-9_]+\.sql$ ]]; then
    printf 'Migration filename is not supported by the upgrade check: %s\n' "$migration_name" >&2
    exit 1
  fi
  version=$((10#${BASH_REMATCH[1]}))
  if (( version != expected_version )); then
    printf 'Migration versions must be sequential: expected %d, found %d.\n' \
      "$expected_version" "$version" >&2
    exit 1
  fi
  expected_manifest+="${expected_manifest:+,}$version:$migration_name"

  if (( version <= baseline_count )); then
    {
      printf 'BEGIN;\n'
      sed -n 'p' "$migration_file"
      printf "\nINSERT INTO schema_migrations (version, name) VALUES (%d, '%s');\n" \
        "$version" "$migration_name"
      printf 'COMMIT;\n'
    } | postgres_psql
  fi
  expected_version=$((expected_version + 1))
done

postgres_psql --single-transaction <"$fixture_sql"

evidence_query="SELECT concat_ws('|',
  organizations.id::text,
  projects.id::text,
  users.email,
  organization_members.role,
  monitors.id::text,
  monitor_revisions.id::text,
  runs.id::text,
  runs.outcome,
  observations.http_status_code::text)
FROM organizations
JOIN projects ON projects.organization_id = organizations.id
JOIN organization_members ON organization_members.organization_id = organizations.id
JOIN users ON users.id = organization_members.user_id
JOIN monitors ON monitors.project_id = projects.id AND monitors.organization_id = organizations.id
JOIN monitor_revisions ON monitor_revisions.monitor_id = monitors.id
JOIN runs ON runs.monitor_id = monitors.id AND runs.organization_id = organizations.id
JOIN observations ON observations.run_id = runs.id
  AND observations.scheduled_for = runs.scheduled_for;"
expected_evidence="00000000-0000-7000-8000-000000000001|00000000-0000-7000-8000-000000000002|upgrade-admin@example.test|Administrator|00000000-0000-7000-8000-000000000004|00000000-0000-7000-8000-000000000005|00000000-0000-7000-8000-000000000006|passed|200"
before_evidence="$(postgres_query "$evidence_query")"
if [[ "$before_evidence" != "$expected_evidence" ]]; then
  printf 'The pre-upgrade evidence fixture did not match the expected fingerprint.\n' >&2
  exit 1
fi

timeout 300 "${compose[@]}" "${compose_args[@]}" up --detach --build api

wait_api_ready() {
  for _ in {1..120}; do
    if "${compose[@]}" "${compose_args[@]}" exec -T api \
      wget -q -O /dev/null http://127.0.0.1:8080/readyz >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  printf 'The upgraded API did not become ready within 120 seconds.\n' >&2
  return 1
}

wait_api_ready

actual_manifest="$(postgres_query \
  "SELECT string_agg(version::text || ':' || name, ',' ORDER BY version) FROM schema_migrations;")"
if [[ "$actual_manifest" != "$expected_manifest" ]]; then
  printf 'The upgraded migration manifest did not match the source migration set.\n' >&2
  exit 1
fi
after_evidence="$(postgres_query "$evidence_query")"
if [[ "$after_evidence" != "$before_evidence" ]]; then
  printf 'Representative persisted evidence changed during the schema upgrade.\n' >&2
  exit 1
fi

timeout 120 "${compose[@]}" "${compose_args[@]}" restart api >/dev/null
wait_api_ready

restart_manifest="$(postgres_query \
  "SELECT string_agg(version::text || ':' || name, ',' ORDER BY version) FROM schema_migrations;")"
restart_evidence="$(postgres_query "$evidence_query")"
if [[ "$restart_manifest" != "$actual_manifest" || "$restart_evidence" != "$after_evidence" ]]; then
  printf 'The migration runner was not idempotent across an API restart.\n' >&2
  exit 1
fi

failed=0
printf 'Upgrade check passed: forward migration, evidence preservation, and restart idempotency.\n'
