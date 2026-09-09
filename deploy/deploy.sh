#!/usr/bin/env bash
# Rebuild the whole Ajilamu deployment from this directory.
#
# The steps run in order and each one is idempotent:
#
#   1. provision.sh          every Google Cloud resource
#   2. clickhouse-schema.sh  the ledger database, the schema, the read-only user
#   3. build.sh              the release bundle
#   4. ship.sh               copy the bundle to the host
#   5. bootstrap.sh          install and start on the host
#   6. verify.sh             independent measurements over TLS
#
# Usage: deploy/deploy.sh [--recreate-vm] [--skip-verify]

set -euo pipefail
# shellcheck source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

RECREATE=()
SKIP_VERIFY=no
for argument in "$@"; do
  case "$argument" in
    --recreate-vm) RECREATE=(--recreate-vm) ;;
    --skip-verify) SKIP_VERIFY=yes ;;
    *) die "unknown argument: $argument" ;;
  esac
done

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

log "== provision"
"$HERE/provision.sh" "${RECREATE[@]}"

log "== clickhouse ledger"
"$HERE/clickhouse-schema.sh"

log "== build"
"$HERE/build.sh"

log "== ship"
"$HERE/ship.sh"

log "== bootstrap"
gcloud compute ssh "$VM" --zone="$ZONE" --project="$PROJECT" --quiet \
  --ssh-flag='-o ServerAliveInterval=30' \
  --command="sudo ${DEPLOY_ROOT}/deploy/bootstrap.sh"

if [ "$SKIP_VERIFY" = no ]; then
  log "== verify"
  "$HERE/verify.sh"
fi

log "deployment complete for ajilamu.nryn.dev and $(tls_hostname)"
