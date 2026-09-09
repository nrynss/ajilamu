#!/usr/bin/env bash
# Provision every Ajilamu Google Cloud resource in one idempotent pass.
#
# Run it from a machine that holds owner rights on the project. It creates
# the static address, the data disk, the bucket, the firewall rules, the
# service account, the five secrets and the virtual machine. A second run
# reports what already exists and changes nothing.
#
# The writer password and the ClickHouse Cloud OpenAPI pair come from the
# local .env. Every other secret is generated here. No value is printed.
#
# Usage: deploy/provision.sh [--recreate-vm]

set -euo pipefail
# shellcheck source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

RECREATE_VM=no
case "${1:-}" in
  --recreate-vm) RECREATE_VM=yes ;;
  "") ;;
  *) die "unknown argument: $1" ;;
esac

require_tools gcloud openssl
source_env

# enable_apis turns on every API the host and the operator need.
enable_apis() {
  gcloud services enable --project="$PROJECT" \
    compute.googleapis.com \
    iam.googleapis.com \
    secretmanager.googleapis.com \
    aiplatform.googleapis.com \
    texttospeech.googleapis.com \
    storage.googleapis.com
}

# ensure_service_account creates the attached identity when it is absent.
ensure_service_account() {
  if gcloud iam service-accounts describe "$SA_EMAIL" --project="$PROJECT" >/dev/null 2>&1; then
    log "service account $SA_EMAIL exists"
    return
  fi
  gcloud iam service-accounts create "$SA_NAME" \
    --project="$PROJECT" --display-name="Ajilamu host"
  log "created service account $SA_EMAIL"
}

# ensure_address reserves one regional static external address.
ensure_address() {
  if gcloud compute addresses describe "$IP_NAME" \
    --region="$REGION" --project="$PROJECT" >/dev/null 2>&1; then
    log "address $IP_NAME exists"
    return
  fi
  gcloud compute addresses create "$IP_NAME" --region="$REGION" --project="$PROJECT"
  log "reserved address $IP_NAME"
}

# ensure_bucket creates the media bucket in the same region as the host.
ensure_bucket() {
  if gcloud storage buckets describe "gs://$BUCKET" --project="$PROJECT" >/dev/null 2>&1; then
    log "bucket gs://$BUCKET exists"
    return
  fi
  gcloud storage buckets create "gs://$BUCKET" \
    --project="$PROJECT" --location="$REGION" \
    --uniform-bucket-level-access --public-access-prevention
  log "created bucket gs://$BUCKET"
}

# ensure_disk creates the 50 GB SSD that mounts at /data/storage.
ensure_disk() {
  if gcloud compute disks describe "$DISK_NAME" \
    --zone="$ZONE" --project="$PROJECT" >/dev/null 2>&1; then
    log "disk $DISK_NAME exists"
    return
  fi
  gcloud compute disks create "$DISK_NAME" \
    --project="$PROJECT" --zone="$ZONE" \
    --size="$DISK_SIZE" --type=pd-ssd
  log "created disk $DISK_NAME"
}

# ensure_firewall opens one ingress port to the tagged host.
ensure_firewall() {
  local name="$1" port="$2"
  if gcloud compute firewall-rules describe "$name" --project="$PROJECT" >/dev/null 2>&1; then
    log "firewall rule $name exists"
    return
  fi
  gcloud compute firewall-rules create "$name" \
    --project="$PROJECT" --network=default --direction=INGRESS --action=ALLOW \
    --rules="tcp:${port}" --source-ranges=0.0.0.0/0 --target-tags="$NETWORK_TAG"
  log "created firewall rule $name for tcp:${port}"
}

# ensure_secret creates one secret and adds its first version.
# An existing version is left alone, so a rerun never rotates a live value.
ensure_secret() {
  local name="$1" value="${2-}"
  if ! gcloud secrets describe "$name" --project="$PROJECT" >/dev/null 2>&1; then
    gcloud secrets create "$name" --project="$PROJECT" --replication-policy=automatic >/dev/null
    log "created secret $name"
  fi
  if [ -n "$(gcloud secrets versions list "$name" --project="$PROJECT" \
    --format='value(name)' --limit=1 2>/dev/null)" ]; then
    log "secret $name already holds a version"
    return
  fi
  [ -n "$value" ] || die "secret $name has no version and no value was supplied"
  printf '%s' "$value" | gcloud secrets versions add "$name" \
    --project="$PROJECT" --data-file=- >/dev/null
  log "added the first version of $name"
}

# ensure_secrets creates the five secrets named in infrastructure.md.
ensure_secrets() {
  ensure_secret ajilamu-clickhouse-password "${CLICKHOUSE_PASSWORD:-}"
  ensure_secret ajilamu-clickhouse-readonly-password "$(gen_clickhouse_password)"
  ensure_secret ajilamu-clickhouse-mcp-token "$(gen_token)"
  ensure_secret ajilamu-clickhouse-key-id "${CLICKHOUSE_KEY_ID:-}"
  ensure_secret ajilamu-clickhouse-key-secret "${CLICKHOUSE_KEY_SECRET:-}"
}

# grant_roles gives the attached identity exactly what the host uses.
# A role the project does not support stops the run rather than passing
# silently, because a missing binding shows up later as a denied API call.
grant_roles() {
  local role
  for role in "${PROJECT_ROLES[@]}"; do
    gcloud projects add-iam-policy-binding "$PROJECT" \
      --member="serviceAccount:${SA_EMAIL}" --role="$role" --condition=None >/dev/null \
      || die "the project rejected role $role for $SA_EMAIL"
    log "granted $role on the project"
  done
  local name
  for name in "${SECRET_NAMES[@]}"; do
    gcloud secrets add-iam-policy-binding "$name" \
      --project="$PROJECT" --member="serviceAccount:${SA_EMAIL}" \
      --role=roles/secretmanager.secretAccessor --condition=None >/dev/null
    log "granted secretAccessor on $name"
  done
  gcloud storage buckets add-iam-policy-binding "gs://$BUCKET" \
    --project="$PROJECT" --member="serviceAccount:${SA_EMAIL}" \
    --role=roles/storage.objectAdmin >/dev/null
  log "granted objectAdmin on gs://$BUCKET"
}

# ensure_vm creates the host when it is absent.
# --recreate-vm deletes it first and leaves the data disk and address in place.
ensure_vm() {
  local ip
  ip="$(static_ip)"
  [ -n "$ip" ] || die "address $IP_NAME holds no value"
  if gcloud compute instances describe "$VM" --zone="$ZONE" --project="$PROJECT" >/dev/null 2>&1; then
    if [ "$RECREATE_VM" = no ]; then
      log "instance $VM exists"
      return
    fi
    log "deleting instance $VM for a clean rebuild"
    gcloud compute instances delete "$VM" --zone="$ZONE" --project="$PROJECT" --quiet
  fi
  gcloud compute instances create "$VM" \
    --project="$PROJECT" --zone="$ZONE" \
    --machine-type="$MACHINE_TYPE" \
    --image-family="$IMAGE_FAMILY" --image-project="$IMAGE_PROJECT" \
    --boot-disk-size="$BOOT_DISK_SIZE" --boot-disk-type=pd-balanced \
    --disk="name=${DISK_NAME},device-name=${DISK_NAME},mode=rw,boot=no,auto-delete=no" \
    --address="$ip" --network=default --tags="$NETWORK_TAG" \
    --service-account="$SA_EMAIL" \
    --scopes=https://www.googleapis.com/auth/cloud-platform
  log "created instance $VM"
}

main() {
  enable_apis
  ensure_service_account
  ensure_address
  ensure_bucket
  ensure_disk
  ensure_firewall ajilamu-allow-http 80
  ensure_firewall ajilamu-allow-https 443
  ensure_secrets
  grant_roles
  ensure_vm

  log ""
  log "project      ${PROJECT}"
  log "zone         ${ZONE}"
  log "instance     ${VM} (${MACHINE_TYPE})"
  log "static ip    $(static_ip)"
  log "tls hostname $(tls_hostname)"
  log "data disk    ${DISK_NAME} (${DISK_SIZE} pd-ssd) at ${DATA_ROOT}"
  log "bucket       gs://${BUCKET}"
  log "next         deploy/deploy.sh"
}

main "$@"
