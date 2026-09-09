#!/usr/bin/env bash
# Create the Ajilamu ledger database and load sql/schema.sql into it.
#
# The script creates the database when it is absent, loads the schema with
# `clickhouse client --queries-file`, and then provisions the read-only user by
# calling deploy/clickhouse-readonly.sh. Every step is idempotent. The schema
# file owns CREATE TABLE IF NOT EXISTS and CREATE OR REPLACE VIEW, so a second
# run keeps every row and refreshes the view definitions. Nothing here drops,
# truncates or renames an object.
#
# ClickHouse Cloud serves HTTPS on CLICKHOUSE_PORT and the native protocol on
# port 9440. `--queries-file` needs the native protocol, because the HTTP
# endpoint refuses a multi statement body. Set CLICKHOUSE_NATIVE_PORT to
# override that port.
#
# The operator machine needs the `clickhouse` client, or docker with the image
# named by CLICKHOUSE_CLIENT_IMAGE. No credential reaches a file or the console.
# The client reads CLICKHOUSE_USER and CLICKHOUSE_PASSWORD from the environment.
#
# Run it from the repository root after deploy/provision.sh, because the
# read-only password lives in Secret Manager.
#
# Usage: deploy/clickhouse-schema.sh

set -euo pipefail
# shellcheck source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

ROOT="$(repo_root)"
SCHEMA="sql/schema.sql"
[ -f "$ROOT/$SCHEMA" ] || die "missing $SCHEMA in $ROOT"

source_env

: "${CLICKHOUSE_HOST:?CLICKHOUSE_HOST is required in .env}"
: "${CLICKHOUSE_PORT:?CLICKHOUSE_PORT is required in .env}"
: "${CLICKHOUSE_USER:?CLICKHOUSE_USER is required in .env}"
: "${CLICKHOUSE_PASSWORD:?CLICKHOUSE_PASSWORD is required in .env}"

DATABASE="${CLICKHOUSE_DATABASE:-ajilamu}"
SECURE="${CLICKHOUSE_SECURE:-true}"
CLIENT_IMAGE="${CLICKHOUSE_CLIENT_IMAGE:-clickhouse/clickhouse-server:26.8.2.7}"

# The native port carries --queries-file. The .env port serves HTTPS.
if [ "$SECURE" = true ]; then
  NATIVE_PORT="${CLICKHOUSE_NATIVE_PORT:-9440}"
else
  NATIVE_PORT="${CLICKHOUSE_NATIVE_PORT:-9000}"
fi

CLIENT_ARGS=(--host "$CLICKHOUSE_HOST" --port "$NATIVE_PORT")
if [ "$SECURE" = true ]; then
  CLIENT_ARGS+=(--secure)
fi

# clickhouse_client runs the client against the ledger service. A local client
# wins over docker. The container mounts the repository read only, so the
# client resolves sql/schema.sql from the same checkout.
clickhouse_client() {
  if command -v clickhouse >/dev/null 2>&1; then
    ( cd "$ROOT" && exec clickhouse client "${CLIENT_ARGS[@]}" "$@" ) </dev/null
  fi
  command -v docker >/dev/null 2>&1 \
    || die "install the clickhouse client or docker, then run this step again"
  docker run --rm -i --network host \
    -e CLICKHOUSE_USER -e CLICKHOUSE_PASSWORD \
    -v "${ROOT}:/repo:ro" -w /repo \
    "$CLIENT_IMAGE" clickhouse client "${CLIENT_ARGS[@]}" "$@" </dev/null
}

log "creating database ${DATABASE} when absent"
clickhouse_client --query "CREATE DATABASE IF NOT EXISTS \`${DATABASE}\`"

# The file runs its own preflight guard as the first statement. The guard
# throws on any mismatch and the client then exits nonzero. Its printed value
# is the mismatch count, so a zero confirms the shape this file owns.
log "loading ${SCHEMA} into ${DATABASE}"
guard="$(clickhouse_client --database "$DATABASE" --queries-file "$SCHEMA")"
guard_value="$(printf '%s' "$guard" | tr -d '[:space:]')"
if [ "$guard_value" != 0 ]; then
  die "the preflight guard reported ${guard:-an empty result} against ${DATABASE}"
fi
log "preflight guard reported 0 mismatches"

# Measure the landed objects through the catalog rather than through the
# loader's exit code. Five append tables and seven read views are the shape.
log "measuring the ledger objects in ${DATABASE}"
objects="$(clickhouse_client --database "$DATABASE" --query \
  "SELECT countIf(engine = 'View'), countIf(engine LIKE '%MergeTree') FROM system.tables WHERE database = currentDatabase()" \
  | tr -d '\r')"
views="${objects%%$'\t'*}"
raws="${objects##*$'\t'}"
if [ "$views" != 7 ] || [ "$raws" != 5 ]; then
  die "expected 7 views and 5 append tables in ${DATABASE}, measured views=${views} tables=${raws}"
fi
log "measured views=${views} append tables=${raws}"

# The read-only user reads this database and nothing else.
export CLICKHOUSE_DATABASE="$DATABASE"
log "provisioning the read-only user for ${DATABASE}"
"$ROOT/deploy/clickhouse-readonly.sh"

log "ledger ${DATABASE} is present, loaded and readable by mcp_readonly"
