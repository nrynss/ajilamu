#!/usr/bin/env bash
# Shared settings and helpers for the Ajilamu deploy scripts.
# Every script sources this file, so resource names live in one place.

set -euo pipefail

PROJECT="${AJILAMU_PROJECT:-nryn-personal}"
REGION="${AJILAMU_REGION:-us-central1}"
ZONE="${AJILAMU_ZONE:-us-central1-a}"
VM="${AJILAMU_VM:-ajilamu}"
IP_NAME="${AJILAMU_IP_NAME:-ajilamu-ip}"
DISK_NAME="${AJILAMU_DISK_NAME:-ajilamu-data}"
DISK_SIZE="${AJILAMU_DISK_SIZE:-50GB}"
BUCKET="${AJILAMU_BUCKET:-ajilamu-media}"
SA_NAME="${AJILAMU_SA_NAME:-ajilamu-host}"
SA_EMAIL="${SA_NAME}@${PROJECT}.iam.gserviceaccount.com"
NETWORK_TAG="${AJILAMU_NETWORK_TAG:-ajilamu-web}"
IMAGE_FAMILY="${AJILAMU_IMAGE_FAMILY:-ubuntu-2404-lts-amd64}"
IMAGE_PROJECT="${AJILAMU_IMAGE_PROJECT:-ubuntu-os-cloud}"
MACHINE_TYPE="${AJILAMU_MACHINE_TYPE:-e2-standard-2}"
BOOT_DISK_SIZE="${AJILAMU_BOOT_DISK_SIZE:-20GB}"
SERVICE_USER="ajilamu"
DEPLOY_ROOT="/opt/ajilamu"
DATA_ROOT="/data/storage"

# The five secrets named in dev-diary/infrastructure.md.
SECRET_NAMES=(
  ajilamu-clickhouse-password
  ajilamu-clickhouse-readonly-password
  ajilamu-clickhouse-mcp-token
  ajilamu-clickhouse-key-id
  ajilamu-clickhouse-key-secret
)

# Predefined roles the host holds on the project. The synchronous
# text:synthesize API checks no texttospeech permission, and Google documents
# no role named cloudtexttospeech.user or cloudtts.user. See the deploy record
# dev-diary/adversarial-review/t8.2-deploy.md for the measurement.
PROJECT_ROLES=(
  roles/aiplatform.user
)

log() { printf '%s\n' "$*" >&2; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

# repo_root prints the checkout that holds this script.
repo_root() { cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd; }

# static_ip prints the reserved external address.
static_ip() {
  gcloud compute addresses describe "$IP_NAME" \
    --region="$REGION" --project="$PROJECT" --format='value(address)'
}

# tls_hostname prints the sslip.io name that resolves to the static address.
# No domain exists, and sslip.io encodes an address in the leftmost label.
tls_hostname() {
  local ip
  ip="$(static_ip)"
  [ -n "$ip" ] || die "no reserved address named $IP_NAME"
  printf '%s.sslip.io\n' "$(printf '%s' "$ip" | tr '.' '-')"
}

# source_env loads the local .env when present. It never prints a value.
source_env() {
  local env_file
  env_file="$(repo_root)/.env"
  [ -f "$env_file" ] || return 0
  set -a
  # shellcheck disable=SC1090
  . "$env_file"
  set +a
}

# require_tools fails early when a command the script needs is absent.
require_tools() {
  local tool
  for tool in "$@"; do
    command -v "$tool" >/dev/null 2>&1 || die "missing tool: $tool"
  done
}

# gen_clickhouse_password prints a password that satisfies the ClickHouse Cloud
# policy. The policy demands at least one special character.
gen_clickhouse_password() {
  printf '%sAa9!\n' "$(openssl rand -hex 16)"
}

# gen_token prints a 64 character hex bearer token.
gen_token() { openssl rand -hex 32; }

# sql_quote prints a single quoted SQL literal.
sql_quote() {
  local value="$1"
  printf "'%s'" "${value//\'/\'\'}"
}

# curl_config writes a curl config file that carries one user and password.
# The caller owns the file and removes it. The value never reaches argv.
curl_config() {
  local path="$1" user="$2" password="$3" escaped
  escaped="${password//\\/\\\\}"
  escaped="${escaped//\"/\\\"}"
  umask 077
  printf 'user = "%s:%s"\n' "$user" "$escaped" > "$path"
}
