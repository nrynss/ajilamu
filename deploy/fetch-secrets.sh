#!/usr/bin/env bash
# Fetch the five Ajilamu secrets into /run/ajilamu/env for EnvironmentFile=.
#
# The metadata server supplies the service account token, so no key file
# reaches this machine. /run is tmpfs, so the file disappears on reboot and
# never touches the persistent disk. This script runs as root and never
# prints a secret value.

set -euo pipefail

PROJECT="${GOOGLE_CLOUD_PROJECT:-nryn-personal}"
ENV_DIR="/run/ajilamu"
ENV_FILE="${ENV_DIR}/env"
SERVICE_USER="ajilamu"
METADATA="http://metadata.google.internal/computeMetadata/v1"
SECRET_API="https://secretmanager.googleapis.com/v1"

log() { printf 'fetch-secrets: %s\n' "$*" >&2; }
fail() { printf 'fetch-secrets: error: %s\n' "$*" >&2; exit 1; }

# read_token prints one service account access token.
read_token() {
  local token
  token="$(curl -sS -m 10 -H 'Metadata-Flavor: Google' \
    "${METADATA}/instance/service-accounts/default/token" \
    | sed -n 's/.*"access_token"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
  [ -n "$token" ] || fail "the metadata server returned no access token"
  printf '%s' "$token"
}

# read_secret prints the latest version of one secret.
read_secret() {
  local token="$1" name="$2" payload value
  payload="$(curl -sS -m 15 -H "Authorization: Bearer ${token}" \
    "${SECRET_API}/projects/${PROJECT}/secrets/${name}/versions/latest:access" \
    | sed -n 's/.*"data"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
  [ -n "$payload" ] || fail "no payload for secret ${name}"
  value="$(printf '%s' "$payload" | base64 -d)"
  [ -n "$value" ] || fail "secret ${name} is empty"
  case "$value" in
    *$'\n'* | *$'\r'*)
      fail "secret ${name} carries a line break and cannot reach an EnvironmentFile"
      ;;
  esac
  printf '%s' "$value"
}

main() {
  local token tmp
  token="$(read_token)"

  install -d -m 0700 -o "$SERVICE_USER" -g "$SERVICE_USER" "$ENV_DIR"
  tmp="$(mktemp "${ENV_DIR}/env.XXXXXX")"
  chmod 0600 "$tmp"
  trap 'rm -f "${tmp:-}"' EXIT

  # Each row pairs the process variable with its Secret Manager name.
  local row variable name
  for row in \
    "CLICKHOUSE_PASSWORD ajilamu-clickhouse-password" \
    "CLICKHOUSE_READONLY_PASSWORD ajilamu-clickhouse-readonly-password" \
    "CLICKHOUSE_MCP_AUTH_TOKEN ajilamu-clickhouse-mcp-token" \
    "CLICKHOUSE_KEY_ID ajilamu-clickhouse-key-id" \
    "CLICKHOUSE_KEY_SECRET ajilamu-clickhouse-key-secret"; do
    variable="${row%% *}"
    name="${row##* }"
    printf '%s=%s\n' "$variable" "$(read_secret "$token" "$name")" >> "$tmp"
  done

  chown "$SERVICE_USER:$SERVICE_USER" "$tmp"
  chmod 0600 "$tmp"
  mv "$tmp" "$ENV_FILE"
  trap - EXIT
  log "wrote 5 secrets to ${ENV_FILE} at mode 0600"
}

main "$@"
