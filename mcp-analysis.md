# MCP and Agent Framework Analysis

Decision record for the ClickHouse MCP read path and the ADK agent surface.
Written 2026-09-08 after the Option B decision. The docs updated in the same
commit carry the authoritative architecture. This file records the reasoning
and the rejected alternative.

## Why adk-go and mcp-clickhouse were not in the code

The repository never used `google/adk-go` or `mcp-clickhouse`, yet `project.md`
listed both from the initial commit `654db86` (T0.1). No repo document records
a decision against them, because no decision was made. The spec rows simply
never became tasks:

- No phase task owns an ADK integration or an MCP bridge.
- `project.md` rows 138 and 140 and the `infrastructure.md` service layout
  survived untouched while every phase shipped code that contradicted them.
- `AGENTS.md` froze the stack to the standard library plus one exception,
  `google.golang.org/genai`, which excluded both.
- Task T2.2dev owned `project.md` with the brief to reconcile the stack table
  to the SDK. It updated the Gemini rows and left the Agent Framework and
  Provenance Ledger rows stale. Its round 1 review approved that state.
- `go.mod` confirms the gap. Direct requires are only `texttospeech` and
  `genai`. The ledger client in `internal/ledger` writes ClickHouse over
  stdlib `net/http`, and the fit loop in `internal/fit/loop.go` is plain Go.

The skip was distributed across roles, by design. Fresh agents per round and
status claims erased on completion leave no named agent in the record. The
structural cause matters more than any one agent. Spec rows with no owning
task are invisible to a review loop that reads diffs against task specs.

## What the two candidates are

**mcp-clickhouse** is a Python MCP server, not a library. A Go backend needs
an MCP client to reach it. The only MCP toolset in the chosen stack is
`google.golang.org/adk/v2/tool/mcptoolset`, so adopting ClickHouse MCP brings
in ADK. Version `v2.3.0`, published 2026-08-31, requires Go 1.26.6. The repo
runs Go 1.27.1.

**ClickHouse Cloud managed remote MCP** is the hosted server at
`https://mcp.clickhouse.cloud/mcp`. The docs describe it as read-only by
design, disabled by default, enabled per service under Connect, and governed
by the signed-in Cloud user's permissions. Every documented client flow
authenticates with interactive browser OAuth. No client-credentials grant,
service account, or workload identity is documented.

## Why Option A was rejected

A 24/7 backend cannot complete interactive user OAuth. Caching a human-user
token on the GCE host contradicts the credential architecture in
`infrastructure.md`, which promises an attached service account, a metadata
server token, and no key material on the machine. Cloud OAuth has no
documented path to consume the GCE identity. The managed server also exposes
a tool set different from the named OSS package, which weakens the literal
rule match. Read-only by design fits the agent, but the auth model decides
against it.

## Decision: Option B

Run the official OSS `mcp-clickhouse` server in Docker beside the Go backend
on the GCE host. It connects to the same ClickHouse Cloud service with a
dedicated read-only database user. The backend reaches it over localhost with
a static bearer token. No OAuth runs on the host.

The split is strict:

- Ledger writes stay on the durable HTTP client in `internal/ledger`, the
  single writer. Queue, batching, and reconcile semantics from T4.1 do not
  change.
- Agent reads go through ADK `tool/mcptoolset` to mcp-clickhouse. The agent
  never composes SQL that inserts.
- Read-only is enforced twice. The `mcp_readonly` user holds SELECT grants
  only, and the server runs with write access disabled.
- No fallback path exists. If mcp-clickhouse fails, the agent loses read
  tools. Dubbing, edits, and the ledger keep running because nothing
  load-bearing routes through MCP.

## Difficulty assessment

The code-side work is additive. The fit loop, ledger client, and `wire.go`
event plumbing already expose the boundaries an agent needs. ADK builds on
`google.golang.org/genai`, which the repo already pins. The costs are a wide
transitive dependency tree, agent turn charges that the `Charge` model does
not yet name, and sequencing with tasks that do not exist yet. P6 owns the
command mutations the agent would wrap. T8.2 owns the deployment and now
names the mcp-clickhouse container.

The ADK integration is unbuilt. A future task owns `internal/agent`.

## Docs changed in this commit

- `dev-diary/project.md`: stack rows name `google.golang.org/adk/v2` and the
  self-hosted mcp-clickhouse read path. A paragraph records the read or write
  split and marks the agent unbuilt.
- `dev-diary/infrastructure.md`: service layout names the editor agent and
  the mcp-clickhouse container. Credentials record the writer user, the
  `mcp_readonly` user, and the bearer token. The Option A rejection reason
  sits beside them.
- `dev-diary/PHASE-8-ship.md`: the T8.2 deploy text includes the
  mcp-clickhouse container.
- `.env.example`: adds `CLICKHOUSE_MCP_URL` and `CLICKHOUSE_MCP_AUTH_TOKEN`.

`AGENTS.md` is unchanged. The stack there stays frozen for agents. The owner
amends it at land time when a task lands the ADK pin.
