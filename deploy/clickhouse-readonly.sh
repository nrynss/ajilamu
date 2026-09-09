#!/usr/bin/env bash
# Create the ClickHouse Cloud read-only user that mcp-clickhouse uses.
#
# The password lives in Secret Manager under
# ajilamu-clickhouse-readonly-password. A value in CLICKHOUSE_READONLY_PASSWORD
# wins over Secret Manager. This script creates or repairs the user and grants
# SELECT on the ledger database only. A probe then proves the grant allows a
# read and refuses a write.
#
# Run it from the operator machine after deploy/provision.sh. It sources the
# local .env for the writer credential and never prints any password.
#
# Usage: deploy/clickhouse-readonly.sh

set -euo pipefail
# shellcheck source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

require_tools curl
source_env

: "${CLICKHOUSE_HOST:?CLICKHOUSE_HOST is required in .env}"
: "${CLICKHOUSE_PORT:?CLICKHOUSE_PORT is required in .env}"
: "${CLICKHOUSE_USER:?CLICKHOUSE_USER is required in .env}"
: "${CLICKHOUSE_PASSWORD:?CLICKHOUSE_PASSWORD is required in .env}"

DATABASE="${CLICKHOUSE_DATABASE:-ajilamu}"
READONLY_USER=mcp_readonly
SECRET=ajilamu-clickhouse-readonly-password

# The scheme follows CLICKHOUSE_SECURE, so a scratch service on plain HTTP
# runs the same probes as the Cloud service over TLS.
SCHEME=https
if [ "${CLICKHOUSE_SECURE:-true}" = false ]; then
  SCHEME=http
fi

# The password comes from Secret Manager. A value already in the environment
# wins, so a scratch run reuses this script without reaching gcloud.
READONLY_PASSWORD="${CLICKHOUSE_READONLY_PASSWORD:-}"
if [ -z "$READONLY_PASSWORD" ]; then
  require_tools gcloud
  READONLY_PASSWORD="$(gcloud secrets versions access latest \
    --secret="$SECRET" --project="$PROJECT")"
fi
[ -n "$READONLY_PASSWORD" ] || die "secret $SECRET holds no value"

WRITER_CONFIG="$(mktemp)"
READONLY_CONFIG="$(mktemp)"
chmod 0600 "$WRITER_CONFIG" "$READONLY_CONFIG"
trap 'rm -f "$WRITER_CONFIG" "$READONLY_CONFIG"' EXIT
curl_config "$WRITER_CONFIG" "$CLICKHOUSE_USER" "$CLICKHOUSE_PASSWORD"
curl_config "$READONLY_CONFIG" "$READONLY_USER" "$READONLY_PASSWORD"

ENDPOINT="${SCHEME}://${CLICKHOUSE_HOST}:${CLICKHOUSE_PORT}/"

# write_query runs one statement as the writer and prints the answer.
write_query() {
  printf '%s' "$1" | curl -sS -m 120 --fail-with-body \
    --config "$WRITER_CONFIG" --data-binary @- "$ENDPOINT"
}

# readonly_query runs one statement as the read-only user.
readonly_query() {
  printf '%s' "$1" | curl -sS -m 120 --fail-with-body \
    --config "$READONLY_CONFIG" --data-binary @- "$ENDPOINT"
}

# readonly_status prints the HTTP status of one statement as the read-only user.
readonly_status() {
  printf '%s' "$1" | curl -sS -m 120 -o /dev/null -w '%{http_code}' \
    --config "$READONLY_CONFIG" --data-binary @- "$ENDPOINT"
}

log "creating or repairing user ${READONLY_USER} on ${CLICKHOUSE_HOST}"
quoted="$(sql_quote "$READONLY_PASSWORD")"
write_query "CREATE USER IF NOT EXISTS ${READONLY_USER} IDENTIFIED WITH sha256_password BY ${quoted} SETTINGS readonly = 2" >/dev/null
write_query "ALTER USER ${READONLY_USER} IDENTIFIED WITH sha256_password BY ${quoted} SETTINGS readonly = 2" >/dev/null
write_query "GRANT SELECT ON ${DATABASE}.* TO ${READONLY_USER}" >/dev/null
log "grant in place"
write_query "SHOW GRANTS FOR ${READONLY_USER}"

# The grant propagates asynchronously, so the login probe retries.
attempt=1
while :; do
  if readonly_query "SELECT count() FROM ${DATABASE}.charges" >/dev/null 2>&1; then
    break
  fi
  [ "$attempt" -lt 20 ] || die "the read-only user could not read ${DATABASE}.charges"
  attempt=$((attempt + 1))
  sleep 3
done

log "read probe   SELECT count() FROM ${DATABASE}.charges -> $(readonly_query "SELECT count() FROM ${DATABASE}.charges")"
log "system probe SELECT name FROM system.databases -> $(readonly_query "SELECT name FROM system.databases" | tr '\n' ' ')"
log "catalog probe system.tables in ${DATABASE} -> $(readonly_query "SELECT count() FROM system.tables WHERE database = '${DATABASE}'")"

status="$(readonly_status "INSERT INTO ${DATABASE}.charges_raw (charge_id) VALUES ('readonly-probe')")"
if [ "$status" = 200 ]; then
  die "the read-only user accepted an INSERT, so the SELECT grant is not the boundary"
fi
log "write probe  INSERT INTO ${DATABASE}.charges_raw -> HTTP ${status} (refused)"

log "user ${READONLY_USER} holds SELECT on ${DATABASE} only"
