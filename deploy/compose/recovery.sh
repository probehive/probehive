#!/usr/bin/env bash
set -euo pipefail

script_dir="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
base_compose="$script_dir/compose.yaml"
smoke_compose="$script_dir/compose.smoke.yaml"

for command_name in curl jq openssl mktemp timeout cmp; do
  if ! command -v "$command_name" >/dev/null 2>&1; then
    printf 'Required command is unavailable: %s\n' "$command_name" >&2
    exit 1
  fi
done

project_source="probehive-recovery-$$"
project_restore="probehive-restore-$$"
source_args=(-f "$base_compose" -f "$smoke_compose" -p "$project_source")
restore_args=(-f "$base_compose" -f "$smoke_compose" -p "$project_restore")

if podman compose version >/dev/null 2>&1 &&
  podman compose "${source_args[@]}" ps >/dev/null 2>&1; then
  compose=(podman compose)
elif docker compose version >/dev/null 2>&1 &&
  docker compose "${source_args[@]}" ps >/dev/null 2>&1; then
  compose=(docker compose)
else
  printf 'A reachable Podman Compose or Docker Compose engine is required.\n' >&2
  exit 1
fi

umask 077
temporary_dir="$(mktemp -d)"
source_secrets="$temporary_dir/source-secrets"
restore_secrets="$temporary_dir/restore-secrets"
backup_file="$temporary_dir/probehive.dump"
source_port="${PROBEHIVE_RECOVERY_SOURCE_PORT:-18443}"
restore_port="${PROBEHIVE_RECOVERY_RESTORE_PORT:-18444}"
failed=1

cleanup() {
  status=$?
  set +e
  trap - EXIT INT TERM
  if (( failed != 0 )); then
    "${compose[@]}" "${source_args[@]}" ps >&2
    "${compose[@]}" "${source_args[@]}" logs --no-color >&2
    "${compose[@]}" "${restore_args[@]}" ps >&2
    "${compose[@]}" "${restore_args[@]}" logs --no-color >&2
  fi
  "${compose[@]}" "${source_args[@]}" down --volumes --remove-orphans >/dev/null 2>&1
  "${compose[@]}" "${restore_args[@]}" down --volumes --remove-orphans >/dev/null 2>&1
  rm -rf -- "$temporary_dir"
  exit "$status"
}
trap cleanup EXIT INT TERM

"$script_dir/generate-secrets.sh" "$source_secrets" >/dev/null
export PROBEHIVE_POSTGRES_PASSWORD_FILE="$source_secrets/postgres-password"
export PROBEHIVE_DATABASE_URL_FILE="$source_secrets/database-url"
export PROBEHIVE_WEBHOOK_KEYRING_FILE="$source_secrets/webhook-keyring"
export PROBEHIVE_TLS_CERT_FILE="$source_secrets/tls.crt"
export PROBEHIVE_TLS_KEY_FILE="$source_secrets/tls.key"
export PROBEHIVE_HTTPS_PORT="$source_port"
export PROBEHIVE_PUBLIC_ORIGIN="https://localhost:$source_port"

timeout 300 "${compose[@]}" "${source_args[@]}" up --detach --build

wait_ready() {
  local base_url=$1
  local certificate=$2
  for _ in {1..120}; do
    if curl --fail --silent --show-error --noproxy '*' --cacert "$certificate" \
      "$base_url/readyz" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  printf 'ProbeHive did not become ready at %s within 120 seconds.\n' "$base_url" >&2
  return 1
}

wait_ready "$PROBEHIVE_PUBLIC_ORIGIN" "$source_secrets/tls.crt"

cookie_jar="$temporary_dir/source-cookies"
curl_common=(
  --fail-with-body
  --silent
  --show-error
  --noproxy '*'
  --cacert "$source_secrets/tls.crt"
  --cookie "$cookie_jar"
  --cookie-jar "$cookie_jar"
)
request_header_name=
request_token=
request_antiforgery() {
  local antiforgery
  antiforgery="$(curl "${curl_common[@]}" "$PROBEHIVE_PUBLIC_ORIGIN/api/v1/auth/antiforgery")"
  request_header_name="$(jq -er '.headerName' <<<"$antiforgery")"
  request_token="$(jq -er '.requestToken' <<<"$antiforgery")"
}

request_antiforgery
setup="$(curl "${curl_common[@]}" \
  --request POST \
  --header 'Content-Type: application/json' \
  --header "$request_header_name: $request_token" \
  --data '{"email":"recovery-admin@example.test","displayName":"Recovery Administrator","password":"a-long-recovery-password"}' \
  "$PROBEHIVE_PUBLIC_ORIGIN/api/v1/setup/admin")"
organization_id="$(jq -er '.organization.id' <<<"$setup")"
project_id="$(jq -er '.organization.defaultProject.id' <<<"$setup")"
scope_url="$PROBEHIVE_PUBLIC_ORIGIN/api/v1/organizations/$organization_id/projects/$project_id"
monitors_url="$scope_url/monitors"

request_antiforgery
passing_monitor="$(curl "${curl_common[@]}" \
  --request POST \
  --header 'Content-Type: application/json' \
  --header "$request_header_name: $request_token" \
  --data '{"name":"Recovery passing monitor","checkType":"http"}' \
  "$monitors_url")"
passing_monitor_id="$(jq -er '.id' <<<"$passing_monitor")"

request_antiforgery
curl "${curl_common[@]}" \
  --request POST \
  --header 'Content-Type: application/json' \
  --header "$request_header_name: $request_token" \
  --data '{"checkSchemaVersion":1,"checkConfiguration":{"url":"https://web:8443/healthz"}}' \
  "$monitors_url/$passing_monitor_id/revisions" >/dev/null

passing_run="$(curl "${curl_common[@]}" \
  --request POST \
  --header "$request_header_name: $request_token" \
  "$monitors_url/$passing_monitor_id/runs")"
passing_run_id="$(jq -er 'select(.outcome == "passed") | .id' <<<"$passing_run")"
passing_observation="$(curl "${curl_common[@]}" \
  "$monitors_url/$passing_monitor_id/runs/$passing_run_id/observation")"
jq -e --arg run_id "$passing_run_id" \
  '.runId == $run_id and .http.statusCode == 200' <<<"$passing_observation" >/dev/null

request_antiforgery
webhook_integration="$(curl "${curl_common[@]}" \
  --request POST \
  --header 'Content-Type: application/json' \
  --header "$request_header_name: $request_token" \
  --data '{"name":"Recovery receiver","destinationUrl":"https://web:8443/healthz"}' \
  "$PROBEHIVE_PUBLIC_ORIGIN/api/v1/organizations/$organization_id/webhook-integrations")"
integration_id="$(jq -er '.integration.id' <<<"$webhook_integration")"

request_antiforgery
curl "${curl_common[@]}" \
  --request PUT \
  --header 'Content-Type: application/json' \
  --header "$request_header_name: $request_token" \
  --data '{"enabled":true,"version":1}' \
  "$PROBEHIVE_PUBLIC_ORIGIN/api/v1/organizations/$organization_id/webhook-integrations/$integration_id/state" >/dev/null

request_antiforgery
failing_monitor="$(curl "${curl_common[@]}" \
  --request POST \
  --header 'Content-Type: application/json' \
  --header "$request_header_name: $request_token" \
  --data '{"name":"Recovery failing monitor","checkType":"http","intervalSeconds":30}' \
  "$monitors_url")"
failing_monitor_id="$(jq -er '.id' <<<"$failing_monitor")"

request_antiforgery
curl "${curl_common[@]}" \
  --request POST \
  --header 'Content-Type: application/json' \
  --header "$request_header_name: $request_token" \
  --data '{"checkSchemaVersion":1,"checkConfiguration":{"url":"https://web:8443/assets/probehive-recovery-intentional-404","expectedStatusCodes":[200]}}' \
  "$monitors_url/$failing_monitor_id/revisions" >/dev/null

request_antiforgery
curl "${curl_common[@]}" \
  --request PUT \
  --header 'Content-Type: application/json' \
  --header "$request_header_name: $request_token" \
  --data '{"state":"active"}' \
  "$monitors_url/$failing_monitor_id/state" >/dev/null

incident_id=
alert_id=
for _ in {1..120}; do
  incidents="$(curl "${curl_common[@]}" \
    "$monitors_url/$failing_monitor_id/incidents?pageSize=1")"
  alerts="$(curl "${curl_common[@]}" \
    "$monitors_url/$failing_monitor_id/alerts?pageSize=1")"
  incident_id="$(jq -r '.items[0].id // empty' <<<"$incidents")"
  alert_id="$(jq -r '.items[0].id // empty' <<<"$alerts")"
  if [[ -n "$incident_id" && -n "$alert_id" ]]; then
    break
  fi
  sleep 1
done
if [[ -z "$incident_id" || -z "$alert_id" ]]; then
  printf 'The recovery fixture did not produce an Incident and Alert within 120 seconds.\n' >&2
  exit 1
fi

source_query() {
  local query=$1
  "${compose[@]}" "${source_args[@]}" exec -T postgres sh -c \
    'PGPASSWORD="$(cat /run/secrets/postgres_password)" psql --username=probehive --dbname=probehive --tuples-only --no-align --command="$1"' \
    sh "$query"
}

webhook_attempt_count=0
for _ in {1..30}; do
  webhook_attempt_count="$(source_query \
    "SELECT count(*) FROM webhook_delivery_attempts WHERE alert_id='$alert_id';")"
  if (( webhook_attempt_count >= 1 )); then
    break
  fi
  sleep 1
done
if (( webhook_attempt_count < 1 )); then
  printf 'The recovery fixture did not produce a Webhook delivery attempt.\n' >&2
  exit 1
fi

"${compose[@]}" "${source_args[@]}" stop api web >/dev/null
"${compose[@]}" "${source_args[@]}" exec -T postgres sh -c \
  'PGPASSWORD="$(cat /run/secrets/postgres_password)" pg_dump --username=probehive --dbname=probehive --format=custom --no-owner --no-privileges' \
  >"$backup_file"
if [[ ! -s "$backup_file" ]]; then
  printf 'The logical PostgreSQL backup is empty.\n' >&2
  exit 1
fi

"$script_dir/generate-secrets.sh" "$restore_secrets" >/dev/null
chmod 0600 "$restore_secrets/webhook-keyring"
cp -- "$source_secrets/webhook-keyring" "$restore_secrets/webhook-keyring"
chmod 0444 "$restore_secrets/webhook-keyring"
if ! cmp -s "$source_secrets/webhook-keyring" "$restore_secrets/webhook-keyring"; then
  printf 'The recovery fixture did not carry the complete Webhook keyring forward.\n' >&2
  exit 1
fi

export PROBEHIVE_POSTGRES_PASSWORD_FILE="$restore_secrets/postgres-password"
export PROBEHIVE_DATABASE_URL_FILE="$restore_secrets/database-url"
export PROBEHIVE_WEBHOOK_KEYRING_FILE="$restore_secrets/webhook-keyring"
export PROBEHIVE_TLS_CERT_FILE="$restore_secrets/tls.crt"
export PROBEHIVE_TLS_KEY_FILE="$restore_secrets/tls.key"
export PROBEHIVE_HTTPS_PORT="$restore_port"
export PROBEHIVE_PUBLIC_ORIGIN="https://localhost:$restore_port"
export PROBEHIVE_WORKER_ENABLED=false

timeout 120 "${compose[@]}" "${restore_args[@]}" up --detach postgres

restore_query() {
  local query=$1
  "${compose[@]}" "${restore_args[@]}" exec -T postgres sh -c \
    'PGPASSWORD="$(cat /run/secrets/postgres_password)" psql --username=probehive --dbname=probehive --tuples-only --no-align --command="$1"' \
    sh "$query"
}

restore_ready=0
for _ in {1..60}; do
  if restore_query 'SELECT 1;' >/dev/null 2>&1; then
    restore_ready=1
    break
  fi
  sleep 1
done
if (( restore_ready == 0 )); then
  printf 'The disposable PostgreSQL restore did not become queryable within 60 seconds.\n' >&2
  exit 1
fi

"${compose[@]}" "${restore_args[@]}" exec -T postgres sh -c \
  'PGPASSWORD="$(cat /run/secrets/postgres_password)" pg_restore --username=probehive --dbname=probehive --clean --if-exists --no-owner --no-privileges --exit-on-error' \
  <"$backup_file"

timeout 300 "${compose[@]}" "${restore_args[@]}" up --detach --no-build
wait_ready "$PROBEHIVE_PUBLIC_ORIGIN" "$restore_secrets/tls.crt"

assert_min_count() {
  local label=$1
  local query=$2
  local minimum=$3
  local count
  count="$(restore_query "$query")"
  if ! [[ "$count" =~ ^[0-9]+$ ]] || (( count < minimum )); then
    printf 'Restored %s count was %s; expected at least %s.\n' "$label" "$count" "$minimum" >&2
    exit 1
  fi
}

assert_min_count 'Organization' \
  "SELECT count(*) FROM organizations WHERE id='$organization_id';" 1
assert_min_count 'Monitor' \
  "SELECT count(*) FROM monitors WHERE organization_id='$organization_id' AND id IN ('$passing_monitor_id','$failing_monitor_id');" 2
assert_min_count 'Run' \
  "SELECT count(*) FROM runs WHERE id='$passing_run_id' AND organization_id='$organization_id';" 1
assert_min_count 'Observation' \
  "SELECT count(*) FROM observations WHERE run_id='$passing_run_id' AND organization_id='$organization_id';" 1
assert_min_count 'Incident' \
  "SELECT count(*) FROM incidents WHERE id='$incident_id' AND organization_id='$organization_id';" 1
assert_min_count 'Alert' \
  "SELECT count(*) FROM alerts WHERE id='$alert_id' AND organization_id='$organization_id';" 1
assert_min_count 'Webhook Integration' \
  "SELECT count(*) FROM webhook_integrations WHERE id='$integration_id' AND organization_id='$organization_id';" 1
assert_min_count 'Webhook delivery route' \
  "SELECT count(*) FROM webhook_deliveries WHERE alert_id='$alert_id' AND organization_id='$organization_id';" 1
assert_min_count 'Webhook delivery attempt' \
  "SELECT count(*) FROM webhook_delivery_attempts WHERE alert_id='$alert_id' AND organization_id='$organization_id';" 1
assert_min_count 'retained encrypted Webhook secret' \
  "SELECT count(*) FROM webhook_signing_secrets WHERE organization_id='$organization_id' AND integration_id='$integration_id' AND state <> 'retired' AND octet_length(ciphertext) >= 16;" 1

failed=0
printf 'Recovery check passed: logical backup, clean restore, business evidence, Webhook delivery evidence, and keyring recovery.\n'
