#!/usr/bin/env bash
# Prove the mcp-clickhouse read server answers and reaches ClickHouse Cloud.
#
# Run this on the host as root. It reads the bearer token from
# /run/ajilamu/env and never prints it. The tool call runs as the
# mcp_readonly database user, so the answer also proves the SELECT grant.
#
# Usage: sudo /opt/ajilamu/deploy/mcp-probe.sh

set -euo pipefail

ENV_FILE=/run/ajilamu/env
[ -r "$ENV_FILE" ] || { echo "mcp-probe: run as root, ${ENV_FILE} is not readable" >&2; exit 1; }
tok="$(sed -n 's/^CLICKHOUSE_MCP_AUTH_TOKEN=//p' "$ENV_FILE")"
[ -n "$tok" ] || { echo "mcp-probe: no bearer token in ${ENV_FILE}" >&2; exit 1; }

BASE=http://127.0.0.1:8000/mcp
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

call() { # call <body> <session-id>
  local body="$1" session="${2:-}"
  local args=(
    -sS -m 30
    -H 'Content-Type: application/json'
    -H 'Accept: application/json, text/event-stream'
    -H "Authorization: Bearer ${tok}"
    -H 'Host: 127.0.0.1:8000'
  )
  [ -n "$session" ] && args+=(-H "mcp-session-id: ${session}")
  curl "${args[@]}" --data-binary "$body" "$BASE"
}

initialize='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"ajilamu-mcp-probe","version":"1"}}}'
code="$(curl -sS -m 30 -D "$WORK/headers" -o "$WORK/init" -w '%{http_code}' \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -H "Authorization: Bearer ${tok}" \
  -H 'Host: 127.0.0.1:8000' \
  --data-binary "$initialize" "$BASE")"
server="$(sed -n 's/.*"serverInfo":{\([^}]*\)}.*/\1/p' "$WORK/init")"
echo "initialize    HTTP ${code} ${server}"
session="$(sed -n 's/^[Mm]cp-[Ss]ession-[Ii]d: *//p' "$WORK/headers" | tr -d '\r')"
[ -n "$session" ] || { echo "mcp-probe: no session id" >&2; exit 1; }

call '{"jsonrpc":"2.0","method":"notifications/initialized"}' "$session" >/dev/null
# The transport answers as server-sent events, so each frame carries one
# data: line with a JSON document. python3 parses it without a jq dependency.
tools="$(call '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' "$session" | python3 -c '
import json, sys
for line in sys.stdin:
    if line.startswith("data: "):
        body = json.loads(line[6:])
        print(" ".join(tool["name"] for tool in body.get("result", {}).get("tools", [])))
')"
echo "tools         ${tools}"

answer="$(call '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_databases","arguments":{}}}' "$session" | python3 -c '
import json, sys
for line in sys.stdin:
    if line.startswith("data: "):
        body = json.loads(line[6:])
        for item in body.get("result", {}).get("content", []):
            print(item.get("text", ""))
')"
echo "list_databases ${answer}"
