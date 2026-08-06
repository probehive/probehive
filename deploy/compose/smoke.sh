#!/usr/bin/env bash
set -euo pipefail

script_dir="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repository_root="$(CDPATH= cd -- "$script_dir/../.." && pwd)"
base_compose="$script_dir/compose.yaml"
smoke_compose="$script_dir/compose.smoke.yaml"

for command_name in curl jq openssl mktemp timeout; do
  if ! command -v "$command_name" >/dev/null 2>&1; then
    printf 'Required command is unavailable: %s\n' "$command_name" >&2
    exit 1
  fi
done

project_name="probehive-smoke-$$"
compose_args=(-f "$base_compose" -f "$smoke_compose" -p "$project_name")
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
export PROBEHIVE_HTTPS_PORT="${PROBEHIVE_SMOKE_PORT:-18443}"
export PROBEHIVE_PUBLIC_ORIGIN="https://localhost:$PROBEHIVE_HTTPS_PORT"

timeout 300 "${compose[@]}" "${compose_args[@]}" up --detach --build

published_https_port="$("${compose[@]}" "${compose_args[@]}" port web 8443 2>/dev/null || true)"
if [[ -z "$published_https_port" ]]; then
  printf 'The Compose engine did not publish web port 8443; check the web port mapping.\n' >&2
  exit 1
fi

base_url="$PROBEHIVE_PUBLIC_ORIGIN"
ready=0
for _ in {1..120}; do
  if curl --fail --silent --show-error \
    --noproxy '*' \
    --cacert "$PROBEHIVE_TLS_CERT_FILE" \
    "$base_url/readyz" >/dev/null 2>&1; then
    ready=1
    break
  fi
  sleep 1
done
if (( ready == 0 )); then
  printf 'ProbeHive did not become ready within 120 seconds.\n' >&2
  exit 1
fi

index_html="$(curl --fail --silent --show-error \
  --noproxy '*' \
  --cacert "$PROBEHIVE_TLS_CERT_FILE" "$base_url/")"
if [[ "$index_html" != *'<div id="root"></div>'* ]]; then
  printf 'The packaged web application did not return its root document.\n' >&2
  exit 1
fi

cookie_jar="$temporary_dir/cookies"
curl_common=(
  --fail-with-body
  --silent
  --show-error
  --noproxy '*'
  --cacert "$PROBEHIVE_TLS_CERT_FILE"
  --cookie "$cookie_jar"
  --cookie-jar "$cookie_jar"
)

antiforgery="$(curl "${curl_common[@]}" "$base_url/api/v1/auth/antiforgery")"
header_name="$(jq -er '.headerName' <<<"$antiforgery")"
request_token="$(jq -er '.requestToken' <<<"$antiforgery")"
setup="$(curl "${curl_common[@]}" \
  --request POST \
  --header 'Content-Type: application/json' \
  --header "$header_name: $request_token" \
  --data '{"email":"admin@example.test","displayName":"Smoke Administrator","password":"a-long-smoke-password"}' \
  "$base_url/api/v1/setup/admin")"
organization_id="$(jq -er '.organization.id' <<<"$setup")"
project_id="$(jq -er '.organization.defaultProject.id' <<<"$setup")"

antiforgery="$(curl "${curl_common[@]}" "$base_url/api/v1/auth/antiforgery")"
header_name="$(jq -er '.headerName' <<<"$antiforgery")"
request_token="$(jq -er '.requestToken' <<<"$antiforgery")"
monitors_url="$base_url/api/v1/organizations/$organization_id/projects/$project_id/monitors"
monitor="$(curl "${curl_common[@]}" \
  --request POST \
  --header 'Content-Type: application/json' \
  --header "$header_name: $request_token" \
  --data '{"name":"Packaged gateway","checkType":"http"}' \
  "$monitors_url")"
monitor_id="$(jq -er '.id' <<<"$monitor")"

curl "${curl_common[@]}" \
  --request POST \
  --header 'Content-Type: application/json' \
  --header "$header_name: $request_token" \
  --data '{"checkSchemaVersion":1,"checkConfiguration":{"url":"https://web:8443/healthz"}}' \
  "$monitors_url/$monitor_id/revisions" >/dev/null

completed_run="$(curl "${curl_common[@]}" \
  --request POST \
  --header "$header_name: $request_token" \
  "$monitors_url/$monitor_id/runs")"
run_id="$(jq -er 'select(.outcome == "passed") | .id' <<<"$completed_run")"
observation="$(curl "${curl_common[@]}" "$monitors_url/$monitor_id/runs/$run_id/observation")"
jq -e --arg run_id "$run_id" \
  '.runId == $run_id and .http.statusCode == 200' \
  <<<"$observation" >/dev/null

"${compose[@]}" "${compose_args[@]}" stop api >/dev/null
api_logs="$("${compose[@]}" "${compose_args[@]}" logs --no-color api)"
if [[ "$api_logs" != *'ProbeHive stopped gracefully'* ]]; then
  printf 'The API container did not record a graceful shutdown.\n' >&2
  exit 1
fi

failed=0
printf 'Smoke check passed: setup, static web, HTTP Monitor result, and graceful shutdown.\n'
