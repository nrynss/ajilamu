#!/usr/bin/env bash
# Copy the release bundle to the host and unpack it at /opt/ajilamu.
#
# Run deploy/build.sh first. This script waits for SSH, copies the bundle,
# and swaps the unpacked tree into place. It changes no credential.
#
# Usage: deploy/ship.sh

set -euo pipefail
# shellcheck source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

require_tools gcloud
ROOT="$(repo_root)"
BUNDLE="$ROOT/dist/ajilamu-release.tgz"
[ -f "$BUNDLE" ] || die "missing dist/ajilamu-release.tgz, run deploy/build.sh first"

# wait_for_ssh polls until the instance answers over SSH.
wait_for_ssh() {
  local attempt=1
  while [ "$attempt" -le 30 ]; do
    if gcloud compute ssh "$VM" --zone="$ZONE" --project="$PROJECT" \
      --command=true --quiet >/dev/null 2>&1; then
      return 0
    fi
    log "waiting for SSH on $VM (attempt $attempt)"
    attempt=$((attempt + 1))
    sleep 10
  done
  die "SSH did not answer on $VM"
}

log "shipping $(basename "$BUNDLE") to $VM"
wait_for_ssh

gcloud compute scp "$BUNDLE" "${VM}:/tmp/ajilamu-release.tgz" \
  --zone="$ZONE" --project="$PROJECT" --quiet

REMOTE='
set -euo pipefail
sudo rm -rf /opt/ajilamu.new
sudo mkdir -p /opt/ajilamu.new
sudo tar -xzf /tmp/ajilamu-release.tgz -C /opt/ajilamu.new
sudo chown -R root:root /opt/ajilamu.new
sudo rm -rf /opt/ajilamu.old
if [ -d /opt/ajilamu ]; then sudo mv /opt/ajilamu /opt/ajilamu.old; fi
sudo mv /opt/ajilamu.new /opt/ajilamu
sudo rm -rf /opt/ajilamu.old
rm -f /tmp/ajilamu-release.tgz
sudo cat /opt/ajilamu/REVISION
'
log "unpacking at /opt/ajilamu, revision $(gcloud compute ssh "$VM" \
  --zone="$ZONE" --project="$PROJECT" --command="$REMOTE" --quiet)"
