#!/usr/bin/env bash
# Drive the sample dub end to end over TLS and measure the result.
#
# The server on the host does the work. This script only sends requests and
# then measures the artifacts with outside tools: ffprobe on the export,
# volumedetect on its audio, and SQL through the ledger views.
#
# Usage: deploy/run-sample.sh

set -euo pipefail
# shellcheck source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

require_tools curl ffprobe ffmpeg python3 gcloud
source_env

: "${CLICKHOUSE_HOST:?CLICKHOUSE_HOST is required in .env}"
: "${CLICKHOUSE_PORT:?CLICKHOUSE_PORT is required in .env}"
: "${CLICKHOUSE_USER:?CLICKHOUSE_USER is required in .env}"
: "${CLICKHOUSE_PASSWORD:?CLICKHOUSE_PASSWORD is required in .env}"

HOST="$(tls_hostname)"
BASE="https://${HOST}"
OUT="${AJILAMU_EVIDENCE:-${HOME}/ajilamu-t82-evidence}"
mkdir -p "$OUT"

CH_CONFIG="$(mktemp)"
chmod 0600 "$CH_CONFIG"
trap 'rm -f "$CH_CONFIG"' EXIT
curl_config "$CH_CONFIG" "$CLICKHOUSE_USER" "$CLICKHOUSE_PASSWORD"

# ledger runs one statement against ClickHouse Cloud as the writer.
ledger() {
  printf '%s' "$1" | curl -sS -m 120 --fail-with-body \
    --config "$CH_CONFIG" --data-binary @- \
    "https://${CLICKHOUSE_HOST}:${CLICKHOUSE_PORT}/"
}

log "== sample"
sample="$(curl -sS -m 60 -X POST "${BASE}/api/dubs/sample")"
printf '%s\n' "$sample" > "$OUT/sample.json"
dub="$(printf '%s' "$sample" | python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])')"
log "dub          ${dub}"

log "== run"
started="$(date +%s)"
run="$(curl -sS -m 60 -X POST "${BASE}/api/dubs/${dub}/run?language=ml")"
printf '%s\n' "$run" > "$OUT/run.json"
log "start        ${run}"

# The first frame must arrive seconds after the run starts. A proxy that
# buffered the body would deliver it only at the end.
curl -sS -N --no-buffer -m 1800 "${BASE}/api/dubs/${dub}/events" \
  | tee "$OUT/events.sse" \
  | awk -v s="$started" '/^data: / { if (!first) { printf "first frame  %ds\n", systime()-s; first=1 } } END { printf "stream close %ds\n", systime()-s }'
finished="$(date +%s)"
log "wall time    $((finished - started)) seconds"
terminal="$(grep -o '"type":"[a-z]*"' "$OUT/events.sse" | tail -1)"
log "terminal     ${terminal}"
grep -o '"total_nanodollars":[0-9]*' "$OUT/events.sse" | tail -1 | sed 's/^/reported     /'

log "== export over TLS"
export_url="${BASE}/api/dubs/${dub}/takes/ml/dubbed_ducked.mp4"
curl -sS -m 60 -D "$OUT/range-headers.txt" -o /dev/null \
  -H 'Range: bytes=0-1023' "$export_url"
grep -Ei '^(HTTP/|content-range|accept-ranges|content-length)' "$OUT/range-headers.txt" | sed 's/^/  /'
curl -sS -m 300 -o "$OUT/dubbed_ducked.mp4" "$export_url"
ffprobe -v error -show_entries format=duration,size -show_entries stream=codec_type,codec_name \
  -of default=noprint_wrappers=1 "$OUT/dubbed_ducked.mp4" > "$OUT/ffprobe.txt"
sed 's/^/  /' "$OUT/ffprobe.txt"

duration="$(ffprobe -v error -show_entries format=duration -of csv=p=0 "$OUT/dubbed_ducked.mp4")"
tail_start="$(python3 -c "print(max(0.0, ${duration} - 5.0))")"
# volumedetect reports at info level, so the run must not pass -v error.
ffmpeg -hide_banner -nostats -ss "$tail_start" -i "$OUT/dubbed_ducked.mp4" -vn \
  -af volumedetect -f null - 2> "$OUT/volumedetect-tail.txt" || true
grep -E 'mean_volume|max_volume' "$OUT/volumedetect-tail.txt" | sed 's/^/  /' || true

log "== ledger"
{
  printf 'charges_rows      %s\n' "$(ledger "SELECT count() FROM ajilamu.charges WHERE dub_id = '${dub}'")"
  printf 'charges_distinct  %s\n' "$(ledger "SELECT uniqExact(event_key) FROM ajilamu.charges WHERE dub_id = '${dub}'")"
  printf 'charges_by_kind\n'
  ledger "SELECT toString(kind), count(), toString(round(sum(cost_usd), 12)) FROM ajilamu.charges WHERE dub_id = '${dub}' GROUP BY kind ORDER BY kind"
  printf 'takes_rows        %s\n' "$(ledger "SELECT count() FROM ajilamu.takes WHERE dub_id = '${dub}'")"
  printf 'cost_usd          %s\n' "$(ledger "SELECT toString(round(sum(cost_usd), 12)) FROM ajilamu.charges WHERE dub_id = '${dub}'")"
} | tee "$OUT/ledger.txt" | sed 's/^/  /'

log "evidence at ${OUT}"
