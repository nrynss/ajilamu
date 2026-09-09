#!/usr/bin/env bash
# Prepare the host and start every Ajilamu service.
#
# Run this on the virtual machine as root, after deploy/ship.sh has unpacked
# the release at /opt/ajilamu:
#
#   sudo /opt/ajilamu/deploy/bootstrap.sh
#
# The script is idempotent. It installs the packages, mounts the data disk,
# builds the mcp-clickhouse image, installs the systemd units, writes the
# Caddy hostname drop-in from the instance metadata, and starts everything.
# It reads no credential.

set -euo pipefail

export DEBIAN_FRONTEND=noninteractive

DEPLOY_ROOT=/opt/ajilamu
DATA_ROOT=/data/storage
DATA_DIR=/data/storage/ajilamu
DATA_DISK=/dev/disk/by-id/google-ajilamu-data
SERVICE_USER=ajilamu
METADATA="http://metadata.google.internal/computeMetadata/v1"

log() { printf 'bootstrap: %s\n' "$*" >&2; }
die() { printf 'bootstrap: error: %s\n' "$*" >&2; exit 1; }

[ "$(id -u)" -eq 0 ] || die "run this script with sudo"
[ -d "$DEPLOY_ROOT/deploy" ] || die "release missing at $DEPLOY_ROOT, run deploy/ship.sh first"

# external_hostname prints the sslip.io name for the instance address.
external_hostname() {
  local ip
  ip="$(curl -sS -m 10 -H 'Metadata-Flavor: Google' \
    "${METADATA}/instance/network-interfaces/0/access-configs/0/external-ip")"
  [ -n "$ip" ] || die "the metadata server returned no external address"
  printf '%s.sslip.io\n' "$(printf '%s' "$ip" | tr '.' '-')"
}

install_packages() {
  log "installing packages"
  apt-get update
  apt-get install -y --no-install-recommends \
    ca-certificates curl gnupg ffmpeg docker.io docker-compose-v2
  if ! command -v caddy >/dev/null 2>&1; then
    log "adding the Caddy package repository"
    curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' \
      | gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
    curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' \
      > /etc/apt/sources.list.d/caddy-stable.list
    apt-get update
    apt-get install -y caddy
  fi
  systemctl enable --now docker
  log "ffmpeg $(ffmpeg -version | head -1 | cut -d' ' -f3)"
  log "caddy $(caddy version)"
}

ensure_user() {
  if ! id -u "$SERVICE_USER" >/dev/null 2>&1; then
    useradd --system --home "$DEPLOY_ROOT" --shell /usr/sbin/nologin "$SERVICE_USER"
    log "created user $SERVICE_USER"
  fi
}

ensure_data_disk() {
  [ -e "$DATA_DISK" ] || die "data disk $DATA_DISK is not attached"
  if ! blkid "$DATA_DISK" >/dev/null 2>&1; then
    log "formatting $DATA_DISK"
    mkfs.ext4 -F -m0 -L ajilamu-data "$DATA_DISK"
  fi
  mkdir -p "$DATA_ROOT"
  if ! grep -q " $DATA_ROOT " /etc/fstab; then
    printf '%s %s ext4 defaults,nofail,discard 0 2\n' "$DATA_DISK" "$DATA_ROOT" >> /etc/fstab
  fi
  mountpoint -q "$DATA_ROOT" || mount "$DATA_ROOT"
  mkdir -p "$DATA_DIR"
  chown -R "${SERVICE_USER}:${SERVICE_USER}" "$DATA_DIR"
  log "data disk mounted at $DATA_ROOT, data directory $DATA_DIR"
}

build_mcp_image() {
  log "building the mcp-clickhouse image"
  docker compose -f "$DEPLOY_ROOT/deploy/mcp-clickhouse/compose.yaml" build
}

install_units() {
  log "installing systemd units"
  install -m 0644 "$DEPLOY_ROOT"/systemd/*.service /etc/systemd/system/
  systemctl daemon-reload
}

install_caddy() {
  local host
  host="$(external_hostname)"
  install -m 0644 "$DEPLOY_ROOT/Caddyfile" /etc/caddy/Caddyfile
  mkdir -p /etc/systemd/system/caddy.service.d
  printf '[Service]\nEnvironment=AJILAMU_HOSTNAME=%s\n' "$host" \
    > /etc/systemd/system/caddy.service.d/ajilamu.conf
  systemctl daemon-reload
  log "Caddy terminates TLS for ajilamu.nryn.dev and ${host}"
}

start_services() {
  log "starting services"
  systemctl enable ajilamu-secrets.service ajilamu.service ajilamu-mcp.service
  systemctl restart ajilamu-secrets.service
  systemctl restart ajilamu-mcp.service
  systemctl restart ajilamu.service
  systemctl restart caddy.service
}

wait_for_server() {
  local attempt=1
  while [ "$attempt" -le 30 ]; do
    if curl -sS -m 5 http://127.0.0.1:8080/api/healthz >/dev/null 2>&1; then
      log "the server answers on 127.0.0.1:8080"
      return 0
    fi
    attempt=$((attempt + 1))
    sleep 2
  done
  die "the server did not answer on 127.0.0.1:8080, inspect journalctl -u ajilamu"
}

main() {
  install_packages
  ensure_user
  ensure_data_disk
  build_mcp_image
  install_units
  install_caddy
  start_services
  wait_for_server
  log "done, TLS hostnames ajilamu.nryn.dev and $(external_hostname)"
}

main "$@"
