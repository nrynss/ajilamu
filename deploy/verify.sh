#!/usr/bin/env bash
# Measure the deployed host independently.
#
# Every line here is an outside observation: an HTTP status over TLS, the
# certificate the host presents, a systemd state, a listener, a mount, or a
# storage API answer. No line quotes the application's own log.
#
# Usage: deploy/verify.sh [dub-id]

set -euo pipefail
# shellcheck source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

require_tools gcloud curl openssl
# The custom name is primary. The sslip.io name stays the fallback.
# The origin address is pinned with --resolve and -connect, so the
# measurement does not depend on the operator machine's resolver cache.
# A resolver that has not seen the new record cannot fail this check.
HOSTS=("ajilamu.nryn.dev" "$(tls_hostname)")
PRIMARY="${HOSTS[0]}"
IP="$(static_ip)"
DUB_ID="${1:-}"
BASE="https://${PRIMARY}"

log "== TLS"
# Caddy issues the certificates after it starts, so wait for the first 200
# before measuring. A handshake before issuance raises an internal error.
attempt=1
while :; do
  status="$(curl -sS -m 20 -o /dev/null -w '%{http_code}' \
    --resolve "${PRIMARY}:443:${IP}" "https://${PRIMARY}/api/healthz" 2>/dev/null || true)"
  [ "$status" = 200 ] && break
  [ "$attempt" -lt 40 ] || die "TLS answered ${status} after ${attempt} tries"
  attempt=$((attempt + 1))
  sleep 5
done

for host in "${HOSTS[@]}"; do
  log "hostname     ${host}"
  log "  public dns $(dig +short @8.8.8.8 "$host" 2>/dev/null | tr '\n' ' ')"
  log "  healthz    HTTP $(curl -sS -m 20 -o /dev/null -w '%{http_code}' --resolve "${host}:443:${IP}" "https://${host}/api/healthz")"
  log "  index      HTTP $(curl -sS -m 20 -o /dev/null -w '%{http_code}' --resolve "${host}:443:${IP}" "https://${host}/")"
  log "  plain http HTTP $(curl -sS -m 20 -o /dev/null -w '%{http_code}' --resolve "${host}:80:${IP}" "http://${host}/api/healthz")"
  openssl s_client -connect "${IP}:443" -servername "$host" </dev/null 2>/dev/null \
    | openssl x509 -noout -issuer -subject -dates 2>/dev/null \
    | sed 's/^/  /'
done

if [ -n "$DUB_ID" ]; then
  log "== range request over TLS"
  headers="$(curl -sS -m 30 -D - -o /dev/null -H 'Range: bytes=0-1023' \
    --resolve "${PRIMARY}:443:${IP}" \
    "${BASE}/api/dubs/${DUB_ID}/takes/ml/dubbed_ducked.mp4")"
  printf '%s\n' "$headers" | grep -Ei '^(HTTP/|content-range|accept-ranges|content-length)' | sed 's/^/  /'
fi

log "== host"
REMOTE='
set -u
units="$(systemctl is-active ajilamu.service ajilamu-secrets.service ajilamu-mcp.service caddy.service | tr "\n" " ")"
echo "units        ${units}"
echo "mcp listener $(ss -ltnH "sport = :8000" | awk "{print \$4}" | tr "\n" " ")"
echo "web listener $(ss -ltnH "sport = :8080" | awk "{print \$4}" | tr "\n" " ")"
cid="$(sudo docker ps -q --filter name=mcp-clickhouse)"
echo "mcp health   $(sudo docker inspect --format "{{.State.Health.Status}}" ${cid})"
echo "mount        $(findmnt -no SOURCE,FSTYPE,SIZE,USED /data/storage)"
echo "ffmpeg       $(ffmpeg -version | head -1)"
pid="$(systemctl show -p MainPID --value ajilamu.service)"
# Only presence is printed. The values stay in the process environment.
for name in CLICKHOUSE_PASSWORD CLICKHOUSE_READONLY_PASSWORD CLICKHOUSE_MCP_AUTH_TOKEN CLICKHOUSE_KEY_ID CLICKHOUSE_KEY_SECRET; do
  count="$(sudo cat /proc/${pid}/environ | tr "\0" "\n" | grep -c "^${name}=" || true)"
  echo "secret var   ${name} present=${count}"
done
echo "adc override $(sudo cat /proc/${pid}/environ | tr "\0" "\n" | grep -c "^GOOGLE_APPLICATION_CREDENTIALS=" || true) (must be 0)"
tok="$(curl -sS -m 10 -H "Metadata-Flavor: Google" http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token | sed -n "s/.*\"access_token\"[[:space:]]*:[[:space:]]*\"\([^\"]*\)\".*/\1/p")"
# roles/storage.objectAdmin carries storage.objects.list, not
# storage.buckets.get, so the probe lists objects rather than the bucket.
echo "bucket api   HTTP $(curl -sS -m 20 -o /dev/null -w "%{http_code}" -H "Authorization: Bearer ${tok}" "https://storage.googleapis.com/storage/v1/b/ajilamu-media/o?maxResults=1")"
echo "mcp noauth   HTTP $(curl -sS -m 20 -o /dev/null -w "%{http_code}" -H "Host: 127.0.0.1:8000" http://127.0.0.1:8000/mcp)"
echo "mcp auth     HTTP $(sudo bash -c "t=\$(sed -n \"s/^CLICKHOUSE_MCP_AUTH_TOKEN=//p\" /run/ajilamu/env); curl -sS -m 20 -o /dev/null -w \"%{http_code}\" -H \"Host: 127.0.0.1:8000\" -H \"Authorization: Bearer \${t}\" http://127.0.0.1:8000/mcp")"
sudo /opt/ajilamu/deploy/mcp-probe.sh
'
gcloud compute ssh "$VM" --zone="$ZONE" --project="$PROJECT" --quiet \
  --command="$REMOTE" | sed 's/^/  /'
