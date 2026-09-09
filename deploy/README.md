# Ajilamu deployment

This directory rebuilds the whole deployment from an empty project. It creates
the Google Cloud resources, creates the ClickHouse ledger database and loads
its schema, builds the release bundle, ships it, installs the services and
measures the result. Every step is a script in this directory, so nobody
reconstructs the deployment from shell history.

## What runs on the host

| Process | Listener | Reaches |
|---|---|---|
| `ajilamu` | `127.0.0.1:8080` | Vertex AI, Cloud Text-to-Speech, ClickHouse Cloud |
| `mcp-clickhouse` container | `127.0.0.1:8000` | ClickHouse Cloud as `mcp_readonly` |
| `caddy` | `0.0.0.0:80` and `0.0.0.0:443` | the public TLS name only |

Caddy is the only public listener. It terminates TLS and reverse proxies to
the Go server on loopback. The `mcp-clickhouse` server never reaches Caddy,
because it binds loopback and Docker publishes no port.

## Resources

`deploy/provision.sh` creates each row when it is absent and leaves it alone
when it exists.

| Resource | Name | Setting |
|---|---|---|
| Project | `nryn-personal` | owner rights, billing enabled |
| Static external address | `ajilamu-ip` | `us-central1` |
| Instance | `ajilamu` | `e2-standard-2`, `us-central1-a` |
| Data disk | `ajilamu-data` | 50 GB `pd-ssd`, mounted at `/data/storage` |
| Boot disk | the instance boot disk | 20 GB `pd-balanced` |
| Bucket | `gs://ajilamu-media` | `us-central1`, uniform access |
| Firewall | `ajilamu-allow-http` | ingress tcp 80 to tag `ajilamu-web` |
| Firewall | `ajilamu-allow-https` | ingress tcp 443 to tag `ajilamu-web` |
| Service account | `ajilamu-host@nryn-personal.iam.gserviceaccount.com` | attached identity |
| Secret | `ajilamu-clickhouse-password` | writer password |
| Secret | `ajilamu-clickhouse-readonly-password` | `mcp_readonly` password |
| Secret | `ajilamu-clickhouse-mcp-token` | bearer token for mcp-clickhouse |
| Secret | `ajilamu-clickhouse-key-id` | ClickHouse Cloud OpenAPI key id |
| Secret | `ajilamu-clickhouse-key-secret` | ClickHouse Cloud OpenAPI secret |

The service account holds `roles/aiplatform.user` on the project,
`roles/storage.objectAdmin` on the bucket, and
`roles/secretmanager.secretAccessor` on each secret alone. It holds no
project-wide secret role. The synchronous Cloud Text-to-Speech API needs no
texttospeech role, because no such predefined role exists.

## The TLS hostnames

Caddy serves two names from one site block and obtains one public certificate
for each over the HTTP-01 challenge.

`ajilamu.nryn.dev` is the custom name. Its A record points at the reserved
address and stays DNS-only (grey cloud), so the challenge reaches the origin
directly rather than through a proxy.

The reserved address also encodes into a name under `sslip.io`, which resolves
the embedded address. An address of `203.0.113.10` becomes
`203-0-113-10.sslip.io`. That name stays the fallback when the custom
certificate cannot be issued.

`deploy/lib.sh` prints the sslip.io name from the reserved address. Every
script calls that function, so no script hardcodes that name.

## Before the first run

1. Authenticate as an owner: `gcloud auth login`.
2. Confirm the project: `gcloud config set project nryn-personal`.
3. Copy `.env.example` to `.env` and fill `CLICKHOUSE_PASSWORD`,
   `CLICKHOUSE_KEY_ID`, `CLICKHOUSE_KEY_SECRET` and the ClickHouse Cloud
   host, service id and organisation id. The `.env` file stays on the
   operator machine and never reaches the host.
4. `deploy/clickhouse-schema.sh` creates the `ajilamu` database, loads
   `sql/schema.sql` and provisions `mcp_readonly` on the first run. No manual
   SQL step exists.

## The operator path

Run each step from the repository root. `deploy/deploy.sh` runs all six in
order.

```bash
deploy/clickhouse-schema.sh  # the ledger database, schema and mcp_readonly user
deploy/build.sh              # the release bundle
deploy/ship.sh               # copy the bundle to /opt/ajilamu
gcloud compute ssh ajilamu --zone us-central1-a --command 'sudo /opt/ajilamu/deploy/bootstrap.sh'
deploy/verify.sh             # independent measurements over TLS
deploy/run-sample.sh         # the sample dub, end to end
```

`clickhouse-schema.sh` creates the database when it is absent, loads
`sql/schema.sql` with `clickhouse client --queries-file`, and calls
`deploy/clickhouse-readonly.sh`. The schema file owns `IF NOT EXISTS` and
`OR REPLACE`, so a second run keeps every row. The operator machine needs the
`clickhouse` client or docker. The load uses the native protocol on port 9440,
because the HTTP endpoint refuses a multi statement body.

To rebuild the virtual machine on a clean boot disk, keep the address and the
data disk, and rerun the path:

```bash
deploy/provision.sh --recreate-vm
deploy/build.sh && deploy/ship.sh
gcloud compute ssh ajilamu --zone us-central1-a --command 'sudo /opt/ajilamu/deploy/bootstrap.sh'
```

The ledger lives in ClickHouse Cloud, so it survives the rebuild. Run
`deploy/clickhouse-schema.sh` when the database is absent.

`bootstrap.sh` is idempotent. It installs the packages, formats the data disk
only when it carries no filesystem, builds the mcp-clickhouse image, installs
the systemd units, writes the Caddy hostname drop-in from the instance
metadata, and starts every service.

## How configuration reaches the process

Non-secret settings live in `systemd/ajilamu.service` as plain
`Environment=` lines. The five secrets live in Secret Manager.

`systemd/ajilamu-secrets.service` runs `deploy/fetch-secrets.sh` at boot. The
script reads the service account token from the metadata server, calls the
Secret Manager REST API once per secret, and writes `/run/ajilamu/env` at
mode `0600`, owned by `ajilamu`. `/run` is tmpfs, so the file never reaches
the persistent disk. Both `ajilamu.service` and `ajilamu-mcp.service` read
that file with `EnvironmentFile=`.

`ajilamu.service` also runs the same script from `ExecStartPre`, so a restart
refreshes a rotated secret. On a cold `/run` the secrets unit must run first,
because some systemd releases read `EnvironmentFile=` before `ExecStartPre`.
That ordering is why the separate unit exists.

`GOOGLE_APPLICATION_CREDENTIALS` stays unset on the host. The Gen AI SDK
calls `credentials.DetectDefault` and finds the attached service account.

## Rotating a secret

```bash
printf '%s' "$NEW_VALUE" | gcloud secrets versions add ajilamu-clickhouse-password --data-file=-
gcloud compute ssh ajilamu --zone us-central1-a \
  --command 'sudo systemctl restart ajilamu-secrets.service ajilamu.service ajilamu-mcp.service'
gcloud secrets versions disable <old-version> --secret ajilamu-clickhouse-password
```

The restart refreshes `/run/ajilamu/env`. Disable the old version only after
the restart succeeds.

## Cost

| Item | Rate | Monthly |
|---|---|---|
| `e2-standard-2` | $0.066988 per hour | about $48.90 |
| 50 GB `pd-ssd` | $0.170 per GB month | $8.50 |
| 20 GB `pd-balanced` boot disk | $0.100 per GB month | $2.00 |
| Static external address in use | no charge | $0.00 |
| Gemini and Chirp calls | per call | measured per dub |
| ClickHouse Cloud | separate contract | already running |

The running total sits near $59 per month, before model and speech calls. The
machine fits inside the earliest expiring promotional voucher with a safety
buffer, as `dev-diary/infrastructure.md` records.

## What these scripts never do

They never print, log, commit or ship a credential. They never copy `.env`
to the host. They never set `GOOGLE_APPLICATION_CREDENTIALS` on the host.
They never publish the mcp-clickhouse port.
