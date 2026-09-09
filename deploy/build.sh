#!/usr/bin/env bash
# Build the Ajilamu release bundle on the operator machine.
#
# The bundle holds the static Go binary, the built Svelte workspace, the
# committed testdata the server loads at start, the deploy scripts, the
# systemd units and the Caddyfile. deploy/ship.sh copies it to the host.
#
# Nothing from the local .env is staged, so no credential leaves this machine.
#
# Usage: deploy/build.sh

set -euo pipefail
# shellcheck source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

require_tools go npm tar git
ROOT="$(repo_root)"
cd "$ROOT"

STAGE="$ROOT/dist/release"
BUNDLE="$ROOT/dist/ajilamu-release.tgz"
REVISION="$(git rev-parse HEAD)"

log "building revision ${REVISION}"

rm -rf "$STAGE"
mkdir -p "$STAGE/bin" "$STAGE/web" "$STAGE/testdata" "$STAGE/deploy" "$STAGE/systemd"

CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -trimpath -ldflags='-s -w' -o "$STAGE/bin/ajilamu" ./cmd/ajilamu
log "built bin/ajilamu"

if [ ! -d "$ROOT/web/node_modules" ]; then
  log "installing web dependencies"
  npm --prefix web ci
fi
npm --prefix web run build
cp -a "$ROOT/web/build/." "$STAGE/web/"
log "built web/"

# The server reads testdata/manifest.json at start and copies testdata/clip.mp4
# for the sample route, so the committed fixtures ship with the binary.
cp -a "$ROOT/testdata/." "$STAGE/testdata/"
cp -a "$ROOT/deploy/." "$STAGE/deploy/"
cp -a "$ROOT/systemd/." "$STAGE/systemd/"
cp "$ROOT/Caddyfile" "$STAGE/Caddyfile"
printf '%s\n' "$REVISION" > "$STAGE/REVISION"

tar -C "$STAGE" -czf "$BUNDLE" .
log "bundle $(du -h "$BUNDLE" | cut -f1) at dist/ajilamu-release.tgz"
