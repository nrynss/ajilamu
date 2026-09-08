# Cloud Infrastructure and Machine Sizing

We host Ajilamu on a dedicated Google Compute Engine virtual machine in `us-central1`. This guarantees zero cold starts for evaluators and judges.

## Machine Evaluation: e2-standard-2 vs e2-standard-4

We compared two machine sizes against our workloads, credit limits, and judging timeline.

### 1. Cost and Credit Allocation

You hold 24,584.01 INR in total promotional credits. Your earliest expiring voucher gives you 9,564.63 INR. It expires on October 12, 2026.

Competition judging runs until October 9, 2026. We need 32 continuous days of uptime.

* **e2-standard-2 (2 vCPUs, 8 GB RAM, 50 GB SSD):**
  Total cost for 32 days equals 5,578 INR. This leaves 3,986 INR unspent in your first voucher. It gives us a 42% safety buffer.

* **e2-standard-4 (4 vCPUs, 16 GB RAM, 50 GB SSD):**
  Total cost for 32 days equals 9,203 INR. This leaves 361 INR unspent in your first voucher. Heavy manual testing spills slightly into your second voucher.

### 2. Operational Performance

Our Go backend replaces audio without re-encoding video frames. It copies video streams directly.

* **Audio stretching:**
  Both machines run `atempo` on a 5-second take in under 40 milliseconds.

* **Final video muxing:**
  Both machines assemble a 75-second video in 0.05 seconds.

* **Video transcoding:**
  If someone uploads uncompressed raw camera video, 4 cores transcode it in 7 seconds. 2 cores finish in 16 seconds.

* **Scrubbing cache:**
  The 8 GB memory on `e2-standard-2` provides 6.5 GB for Linux page cache. It holds over 90 video projects in memory simultaneously.

### 3. Sizing Verdict

We start on **e2-standard-2**. It provides instant responses, avoids cold starts, and preserves a 4,000 INR safety buffer. We can resize to `e2-standard-4` in 30 seconds if required.

---

## Storage Architecture

We split storage between local SSD and object storage using the Git LFS pattern.

### Local SSD Storage (/data/storage)

An attached 50 GB persistent disk stores active projects and individual WAV takes. It serves video via HTTP 206 Partial Content. Timeline scrubbing responds instantly.

### Google Cloud Storage (gs://ajilamu-media)

A bucket in `us-central1` archives raw uploads and final renders. Traffic between our GCE VM and Cloud Storage costs nothing because both share the same region.

---

## Service Layout

1. **GCE Host (`e2-standard-2`):** Runs the Go web server with the in-process editor agent, the mcp-clickhouse container, and Caddy as reverse proxy.
2. **ClickHouse Cloud:** Stores the commit DAG, take ledger, and pre-computed waveform peak arrays.
3. **Vertex AI (`global`):** Runs Gemini 3.8 Flash for segmentation and translation.
4. **Cloud Text-to-Speech:** Generates Chirp 3 HD voices in Malayalam, German, and Spanish.

mcp-clickhouse runs only for editor-agent reads. It listens on localhost and never reaches
Caddy. It connects to the ClickHouse Cloud service with a dedicated read-only user.

## Credentials

The GCE host runs under an attached service account. The metadata server supplies its token.
No key file reaches the virtual machine, and no credential reaches an image layer.

The service account holds three roles. `roles/aiplatform.user` reaches Vertex AI.
`roles/secretmanager.secretAccessor` reads the secrets below, granted on each secret rather
than on the project. `roles/storage.objectAdmin` reaches `gs://ajilamu-media`.

ClickHouse holds two database users. The writer user in `CLICKHOUSE_USER` serves the
durable ledger client in `internal/ledger`. The read-only user `mcp_readonly` serves
mcp-clickhouse with SELECT grants only. The backend passes a static bearer token to
mcp-clickhouse over localhost. No OAuth flow runs on the host, because Cloud-managed MCP
requires interactive user login and a backend cannot complete it.

A developer machine authenticates with `gcloud auth application-default login`.
It may instead point `GOOGLE_APPLICATION_CREDENTIALS` at a key file it already holds.
Both paths satisfy `credentials.DetectDefault`. The code reads the same in both places.

---

## Configuration and Secrets

Google Cloud holds every value the deployed host needs. Nothing is typed onto the machine by
hand, and no `.env` file ships to production.

The split is by sensitivity. A secret lives in Secret Manager. A non-secret setting lives in
the systemd unit as a plain `Environment=` line. Both arrive at the process as ordinary
environment variables, so `internal/config` reads them the same way in both places.

### Secrets in Secret Manager

Each row is one secret. Grant `roles/secretmanager.secretAccessor` on the individual secret,
never on the project.

| Environment variable | Secret name | What it opens |
|---|---|---|
| `CLICKHOUSE_PASSWORD` | `ajilamu-clickhouse-password` | Writer user for the ledger client |
| `CLICKHOUSE_READONLY_PASSWORD` | `ajilamu-clickhouse-readonly-password` | `mcp_readonly` user for mcp-clickhouse |
| `CLICKHOUSE_MCP_AUTH_TOKEN` | `ajilamu-clickhouse-mcp-token` | Bearer token the backend sends to mcp-clickhouse |
| `CLICKHOUSE_KEY_ID` | `ajilamu-clickhouse-key-id` | ClickHouse Cloud OpenAPI key, paired with the secret below |
| `CLICKHOUSE_KEY_SECRET` | `ajilamu-clickhouse-key-secret` | ClickHouse Cloud OpenAPI secret |

`CLICKHOUSE_KEY_ID` is an identifier rather than a password. It still lives in Secret Manager,
because it is useless apart from its secret and leaking the pair together is the common
mistake.

### Non-secrets in the systemd unit

These carry no sensitivity and belong in the unit file, where an operator can read them
without an access grant.

| Environment variable | Production value |
|---|---|
| `PORT` | `8080` |
| `ENV` | `production` |
| `GEMINI_MODEL` | `gemini-3.8-flash` |
| `GOOGLE_CLOUD_PROJECT` | the project id |
| `GOOGLE_CLOUD_LOCATION` | `global` |
| `CLICKHOUSE_HOST` | the ClickHouse Cloud hostname |
| `CLICKHOUSE_PORT` | `8443` |
| `CLICKHOUSE_USER` | the writer user name |
| `CLICKHOUSE_DATABASE` | `default` |
| `CLICKHOUSE_SECURE` | `true` |
| `CLICKHOUSE_SERVICE_ID` | the ClickHouse Cloud service id |
| `CLICKHOUSE_ORG_ID` | the ClickHouse Cloud organisation id |
| `CLICKHOUSE_MCP_URL` | `http://127.0.0.1:8000/mcp` |
| `CLICKHOUSE_MCP_SERVER_TRANSPORT` | `http` |
| `CLICKHOUSE_MCP_ALLOWED_HOSTS` | `127.0.0.1:8000,localhost:8000` |

The hostname and the two ClickHouse Cloud identifiers are not public, but they are not
credentials either. Knowing them opens nothing without a password.

### GOOGLE_APPLICATION_CREDENTIALS stays unset in production

The deployed host never sets it. The Gen AI SDK calls `credentials.DetectDefault`, which reads
the metadata server and finds the attached service account. Setting the variable would point
the SDK at a key file, which is the exact thing this design removes.

The variable stays in `.env.example` for developer machines that already hold a key file.

### How secrets reach the process

The systemd unit fetches them at start and never stores them on the persistent disk.

An `ExecStartPre` step reads the metadata server for the service account token, calls the
Secret Manager REST API for each secret above, and writes the results to `/run/ajilamu/env`.
`/run` is tmpfs, so the file lives in memory and disappears on reboot. Create it mode `0600`
and owned by the service user. The unit then reads it with `EnvironmentFile=/run/ajilamu/env`.

The mcp-clickhouse container receives `CLICKHOUSE_READONLY_PASSWORD` and
`CLICKHOUSE_MCP_AUTH_TOKEN` from the same file. It never receives the writer password.

This path adds no Go dependency. The alternative reads Secret Manager from the process with
`cloud.google.com/go/secretmanager`, which keeps secrets off the filesystem entirely but needs
a third exception in the `AGENTS.md` stack freeze. We chose the shell fetch, because the stack
freeze costs more to widen than a tmpfs file costs to protect. Revisit that trade if the secret
count grows or if a secret needs reloading without a restart.

### Local development runs without secrets

A clone with no `.env` and no environment set builds and passes the whole offline suite. That
holds today, measured with `env -i`, and it is a requirement rather than an accident. Nobody
needs a Secret Manager grant, a ClickHouse password, or a Google Cloud project to work on this
repository.

A developer who wants live calls copies `.env.example` to `.env` and fills it. `.gitignore`
already excludes `.env` and `*.env.local`. Live probes sit behind `//go:build live` and stay
out of the default suite, so an absent credential never reddens a build.

`config.Load()` requires every secret and exits when one is missing or empty, which T0.2 asked
for. Nothing calls it yet, because no server entrypoint exists. Whoever writes that entrypoint
keeps the guarantee above: a missing credential fails the feature that needs it, at the moment
it is needed, and never at process start. A contributor with no credentials still gets a running
server, a browsable workspace, and the fixture pipeline.

Local development never reads Secret Manager.

### Rotation

Add a new version to the secret, then restart the unit. `ExecStartPre` fetches
`versions/latest` on every start, so no file changes and no redeploy is needed. Disable the
previous version once the restart succeeds.

### Rules

Never commit a real value to `.env.example`. It carries names and safe defaults only.

Never paste a credential into a document, a handoff log, or a review record. This includes a
rotated one. A repository that turns public for judging carries every literal in its history
forever, and the next writer copies whatever format they find. Describe the check instead, and
redact the value where a command must be quoted.
