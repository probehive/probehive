#!/usr/bin/env bash
set -euo pipefail

script_dir="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
source "$script_dir/compose-provider.sh"

temporary_dir="$(mktemp -d)"
fake_bin="$temporary_dir/bin"
trace_file="$temporary_dir/trace"
mkdir -p "$fake_bin"
cleanup() {
  rm -rf -- "$temporary_dir"
}
trap cleanup EXIT INT TERM

make_fake() {
  local command_name=$1
  local exit_status=$2
  {
    printf '%s\n' '#!/usr/bin/env bash'
    printf '%s\n' 'printf "%s\\n" "$0 $*" >> "$PROVIDER_TRACE"'
    printf 'exit %s\n' "$exit_status"
  } >"$fake_bin/$command_name"
  chmod 755 "$fake_bin/$command_name"
}

make_fake podman 1
make_fake podman-compose 0
make_fake docker 0
export PATH="$fake_bin:$PATH"
export PROVIDER_TRACE="$trace_file"

compose=()
select_compose -f fixture.yaml -p provider-test
[[ "${compose[*]}" == podman-compose ]]
grep -q '/podman-compose -f fixture.yaml -p provider-test ps' "$trace_file"
! grep -q '/docker ' "$trace_file"

: >"$trace_file"
PROBEHIVE_COMPOSE_PROVIDER=docker select_compose -f fixture.yaml -p provider-test
[[ "${compose[*]}" == 'docker compose' ]]
grep -q '/docker compose version' "$trace_file"
grep -q '/docker compose -f fixture.yaml -p provider-test ps' "$trace_file"
! grep -q '/podman' "$trace_file"

if PROBEHIVE_COMPOSE_PROVIDER=invalid select_compose -f fixture.yaml \
  2>"$temporary_dir/error"; then
  printf 'Invalid provider unexpectedly succeeded.\n' >&2
  exit 1
fi
grep -q 'Unsupported PROBEHIVE_COMPOSE_PROVIDER: invalid' "$temporary_dir/error"

printf 'Compose provider selection tests passed.\n'
