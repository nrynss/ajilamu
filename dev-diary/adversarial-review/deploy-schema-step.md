# M3: deploy the ledger database and its schema

**Date:** 2026-09-09
**Agent:** SchemaDeployFix, implementation role
**Finding:** M3 in `p8-close-round1.md`
**Verdict:** the deploy path now creates the ClickHouse database, loads `sql/schema.sql`
and provisions the read-only user. A scratch service measured every claim. No commit.

## The mechanism

`deploy/` rebuilt the machine and nothing else. `provision.sh` created the address,
instance, disk, bucket, firewall, service account and five secrets. `build.sh`,
`ship.sh` and `bootstrap.sh` installed the binary, web build, mcp image, units and
Caddy. No script created the ClickHouse database or loaded `sql/schema.sql`.
`deploy/README.md` named that load as a manual prerequisite at item 4. So a rebuilt
machine pointed at a ledger backend that the repository could not rebuild.

`grep -rn schema.sql deploy/ systemd/ Caddyfile` returned only that README line
before this change. It now returns the new step and its README entry.

## The fix

New `deploy/clickhouse-schema.sh` runs four idempotent actions.

1. Validate the `.env` connection settings.
2. Run `CREATE DATABASE IF NOT EXISTS <db>`.
3. Load `sql/schema.sql` with `clickhouse client --queries-file --database <db>`.
4. Call `deploy/clickhouse-readonly.sh` for the `mcp_readonly` user and its grant.

The script reads `CLICKHOUSE_DATABASE` (default `ajilamu`), `CLICKHOUSE_SECURE`
(default `true`) and `CLICKHOUSE_NATIVE_PORT`. The native protocol carries
`--queries-file`, because the HTTP endpoint refuses a multi statement body. The
native port defaults to 9440 over TLS and 9000 without it. A local `clickhouse`
client wins over docker. The docker branch forwards `CLICKHOUSE_USER` and
`CLICKHOUSE_PASSWORD` by variable name and mounts the checkout read only.

The script refuses to trust the loader alone. It checks that the guard printed `0`,
then measures five append tables and seven views through `system.tables`. It never
runs `DROP`, `TRUNCATE` or `RENAME`.

`deploy/deploy.sh` now runs the new step as step 2 in place of the standalone
read-only call. The read-only script is the child of the new step.

`deploy/clickhouse-readonly.sh` gained two small changes. It honors a
`CLICKHOUSE_READONLY_PASSWORD` already in the environment, and it derives the HTTP
scheme from `CLICKHOUSE_SECURE`. Both changes let the same shipped script measure a
scratch service. The production path still reads the password from Secret Manager,
because `.env` holds no `CLICKHOUSE_READONLY_PASSWORD`.

`deploy/README.md` now names the step in the operator path and drops the manual
prerequisite.

## Measured pins

Container: `clickhouse/clickhouse-server:26.8.2.7`, HTTP port 18123, native port
19000, writer user `writer`, database `ajilamu`. The harness copied `deploy/` and
`sql/` to `/tmp/m3-schema`, `/tmp/m3-wire` and `/tmp/m3-bad`. `sha256sum` matched
every copy against the checkout, so the pins measure the shipped bytes. The `.env`
held two generated throwaway passwords and mode 600.

### The step against an empty service

```bash
/tmp/m3-schema/deploy/clickhouse-schema.sh
```

Exit 0. Independent SQL through the writer over HTTP:

| Query | Result |
|---|---|
| `SELECT count() FROM system.databases WHERE name = 'ajilamu'` | `1` |
| `SELECT count() FROM system.tables WHERE database = 'ajilamu' AND engine = 'View'` | `7` |
| `SELECT count() FROM system.tables WHERE database = 'ajilamu' AND engine LIKE '%MergeTree'` | `5` |
| `SELECT count() FROM system.users WHERE name = 'mcp_readonly'` | `1` |
| `SHOW GRANTS FOR mcp_readonly` | `GRANT SELECT ON ajilamu.* TO mcp_readonly` |

### The preflight guard

A second direct load of the file, outside the new script:

```bash
docker run --rm -i --network host \
  -e CLICKHOUSE_USER=writer -e CLICKHOUSE_PASSWORD=<scratch> \
  -v /tmp/m3-schema:/repo:ro -w /repo clickhouse/clickhouse-server:26.8.2.7 \
  clickhouse client --host 127.0.0.1 --port 19000 \
  --database ajilamu --queries-file sql/schema.sql
```

Exit 0, stdout `0`, stderr 0 bytes. The guard's value is its mismatch count.

### The second run keeps the rows

A seeded row `m3-probe` in `charges_raw`, then the step again. Both runs exit 0.

| Measure | Value |
|---|---:|
| rows before run 2 | `1` |
| rows after run 2 | `1` |
| `m3-probe` after run 2 | `1` |

### The read-only user

| Statement as `mcp_readonly` over HTTP | HTTP status | Body |
|---|---:|---|
| `SELECT count() FROM ajilamu.charges` | `200` | `1` |
| `INSERT INTO ajilamu.charges_raw (charge_id) VALUES ('readonly-m3')` | `403` | refusal |
| `SELECT count() FROM otherdb.t` | `403` | `Code: 497 ... grant SELECT for at least one column on otherdb` |

The grant names `ajilamu` alone. The refusal on `otherdb` is the boundary.

### The entrypoint wiring

```bash
PATH=/tmp/m3-wire/bin:$PATH /tmp/m3-wire/deploy/deploy.sh --recreate-vm --skip-verify
```

Exit 0. The log shows `== provision`, `== clickhouse ledger`, `== build`, `== ship`,
`== bootstrap`. The seeded row survives, so the `--recreate-vm` path ends with the
same ledger. The harness stubbed the four unrelated steps and `gcloud`, and the real
`deploy.sh` bytes ran unchanged.

### The wrong shape is refused

Database `bad` held the pre-T1.3 `takes_raw (take_id UUID, overrun_pct Float32,
cost_usd Float64)`. The step against it exited 139, and `bad` still held one object.
The guard created nothing. The 139 is the client crash while formatting the server
exception, which `t8.1-ledger-provisioning.md` already records. The server exception
is `Code: 395 ... Ajilamu schema.sql preflight failed`.

## Mutation

Revert `deploy/deploy.sh` step 2 to `clickhouse-readonly.sh`. A fresh project or a
`--recreate-vm` rebuild then reaches the read-only grant with no database and no
schema, so the writer and the agent both fail. Revert the guard check in
`clickhouse-schema.sh` and an empty client response passes as a successful load.

## Files changed

| Path | Change |
|---|---|
| `deploy/clickhouse-schema.sh` | New. Create the database, load the schema, provision the read-only user. |
| `deploy/clickhouse-readonly.sh` | Honor an environment password and `CLICKHOUSE_SECURE`. |
| `deploy/deploy.sh` | Run the new step as step 2. |
| `deploy/README.md` | Name the step in the operator path, drop the manual prerequisite. |

No application code changed. No credential appears in any file.

## Bar

| Command | Result |
|---|---|
| `bash -n` on the three touched scripts | exit 0 |
| `go build ./...` | exit 0 |
| `go vet ./...` | exit 0 |
| `go test -count=1 ./internal/... ./cmd/...` | exit 0, every package ok |
| `npm --prefix web run check` | exit 0, 183 files, 0 errors |
| `python3 tools/audit_docs.py` | exit 0, no drift |
| `grep -rIl` for both scratch passwords | no file in the repository |

## Cleanup

The harness removed its container on exit. `docker ps` reports none. The throwaway
trees under `/tmp/m3-schema`, `/tmp/m3-wire` and `/tmp/m3-bad` and the generated
`.env` files were deleted after the record was written.
