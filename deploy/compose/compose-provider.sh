#!/usr/bin/env bash

# Select a reachable Compose implementation for the supplied Compose arguments.
# The result is stored in the caller's `compose` array.
select_compose() {
  local requested_provider="${PROBEHIVE_COMPOSE_PROVIDER:-auto}"
  local provider
  local -a providers
  local -a candidate

  case "$requested_provider" in
    auto)
      providers=(podman podman-compose docker)
      ;;
    podman|podman-compose|docker)
      providers=("$requested_provider")
      ;;
    *)
      printf 'Unsupported PROBEHIVE_COMPOSE_PROVIDER: %s (expected auto, podman, podman-compose, or docker).\n' \
        "$requested_provider" >&2
      return 1
      ;;
  esac

  for provider in "${providers[@]}"; do
    case "$provider" in
      podman)
        candidate=(podman compose)
        ;;
      podman-compose)
        candidate=(podman-compose)
        ;;
      docker)
        candidate=(docker compose)
        ;;
    esac
    if ! command -v "${candidate[0]}" >/dev/null 2>&1; then
      continue
    fi
    if [[ "$provider" == podman || "$provider" == docker ]] &&
      ! "${candidate[@]}" version >/dev/null 2>&1; then
      continue
    fi
    if "${candidate[@]}" "$@" ps >/dev/null 2>&1; then
      compose=("${candidate[@]}")
      return 0
    fi
  done

  if [[ "$requested_provider" == auto ]]; then
    printf 'A reachable Podman Compose or Docker Compose engine is required.\n' >&2
  else
    printf 'The requested Compose provider is unavailable: %s\n' "$requested_provider" >&2
  fi
  return 1
}
