#!/usr/bin/env bash
set -euo pipefail

script_dir="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repository_root="$(CDPATH= cd -- "$script_dir/../.." && pwd)"
secrets_dir="${1:-$repository_root/secrets}"

if [[ -e "$secrets_dir" ]]; then
  printf 'Refusing to replace existing secret path: %s\n' "$secrets_dir" >&2
  exit 1
fi

for command_name in openssl mktemp; do
  if ! command -v "$command_name" >/dev/null 2>&1; then
    printf 'Required command is unavailable: %s\n' "$command_name" >&2
    exit 1
  fi
done

parent_dir="$(dirname -- "$secrets_dir")"
mkdir -p "$parent_dir"
temporary_dir="$(mktemp -d "$parent_dir/.probehive-secrets.XXXXXX")"
cleanup() {
  rm -rf -- "$temporary_dir"
}
trap cleanup EXIT
umask 077

database_password="$(openssl rand -hex 32)"
printf '%s\n' "$database_password" >"$temporary_dir/postgres-password"
printf 'postgresql://probehive:%s@postgres:5432/probehive?sslmode=disable\n' \
  "$database_password" >"$temporary_dir/database-url"

webhook_key="$(openssl rand -base64 32 | tr '+/' '-_' | tr -d '=\n')"
printf 'local-1:%s\n' "$webhook_key" >"$temporary_dir/webhook-keyring"

openssl req \
  -x509 \
  -newkey rsa:3072 \
  -sha256 \
  -nodes \
  -days 30 \
  -subj '/CN=localhost' \
  -addext 'subjectAltName=DNS:localhost,DNS:web,IP:127.0.0.1' \
  -keyout "$temporary_dir/tls.key" \
  -out "$temporary_dir/tls.crt" \
  >/dev/null 2>&1

chmod 0444 "$temporary_dir"/*
mv -- "$temporary_dir" "$secrets_dir"
trap - EXIT
printf 'Created local evaluation secrets in %s\n' "$secrets_dir"
