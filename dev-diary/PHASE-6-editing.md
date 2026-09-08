# P6: Editing (Track F)

```yaml
id:       P6
size:     L
branch:   phase/p6-editing
requires: [T5.3, T5.5]
blocks:   P8
parallel: medium
runs-parallel-with: P2, P3, P7
```

**Goal:** Provide manual and natural language editing controls so creators can correct AI transcription, timing, and attribution errors.

**Field evidence:** Initial tests transcribed "Mark Van der High" instead of "Mark Vande Hei". Gemini spelled the name wrong inside the sentence, producing misspelled Malayalam audio.

Manual correction of boundaries, speakers, and transcript text is essential.

**Start T6.4 first:** The command parser is a pure function from text to mutation. It needs no visual timeline, so an agent can start it immediately.

---

### T6.1: Boundary handles ★
```yaml
requires:   T5.3, T1.1
fixture-ok: yes
size:       L · frontier
owns:       web/src/lib/edit/Boundary.svelte
status:     not-started
```
Implement draggable start and end handles on timeline segments. Adjusting handles alters slot duration and updates the length bar dynamically.

Snap dragging to adjacent segment boundaries and silence gaps. Disallow silent overlapping without explicit user confirmation.

**Done when:** Dragging boundary handles recalculates fit metrics in real time and enforces clear collision rules.

---

### T6.2: Speaker reassignment ★
```yaml
requires:   T5.3, T5.5
fixture-ok: yes
size:       S · mid
owns:       web/src/lib/edit/Speaker.svelte
status:     not-started
```
Provide quick speaker toggles per dialogue line. Changing speaker attribution alters voice assignment and requires re-rendering.

Always display estimated re-rendering costs before triggering synthesis calls. Never run billable operations in the background silently.

**Done when:** Changing a speaker displays the associated re-render fee and leaves original takes untouched if canceled.

---

### T6.3: Text correction ★
```yaml
requires:   T5.5
fixture-ok: yes
size:       M · mid
owns:       web/src/lib/edit/Text.svelte
status:     not-started
```
Support in-place text editing for transcribed source sentences and translated target lines.

Correcting source text prompts for re-translation. Correcting target text directly is authoritative and proceeds straight to speech synthesis without second-guessing the creator.

Tag translated elements with proper HTML `lang` attributes to ensure correct Malayalam font rendering.

**Done when:** Both text fields edit cleanly, and editing target text triggers synthesis without calling translation models.

---

### T6.4: Command bar ★
```yaml
requires:   T1.1
fixture-ok: yes
size:       M · mid
owns:       internal/command/parse.go, web/src/lib/edit/CommandBar.svelte
status:     not-started
```
Build a command input above the timeline. Press `/` to focus. Parse natural language instructions into structured mutations:

* "move wav 1 to 0:0005 to right."
* "shift line 3 right by 200ms."
* "change speaker for line 7 to Mark."
* "shorten line 4 by 0.5s."

**The model parses, but never computes:** The model outputs structured actions with targets and values. Deterministic Go code validates values and performs timeline arithmetic.

Display parsed intent before applying changes so creators can confirm modifications.

**Done when:** Sample commands parse into valid mutations, invalid references fail with clear explanations, and values validate before execution.

---

### T6.5: Re-render a line ★
```yaml
requires:   T6.2, T6.3, T2.7
fixture-ok: no
size:       M · mid
owns:       internal/api/rerender.go
status:     not-started
```
Re-run an individual dialogue line through the fit loop and save the output as a new take. Never overwrite previous takes.

Preserve `seg_3_try1.wav` when creating `seg_3_try2.wav`. Render older takes as ghost outlines on the timeline.

Display cost estimates before running and record new commits in ClickHouse.

**Done when:** Re-rendering creates a secondary take file, both takes play independently, and the timeline shows ghost take history.

---

### T6.6: Editor agent on ADK
```yaml
requires:   T6.4, T4.3, T1.2, T1.3
fixture-ok: yes
size:       L · frontier
owns:       internal/agent/agent.go, internal/agent/tools.go,
            internal/agent/agent_test.go, internal/agent/agent_live_test.go,
            internal/cost/cost.go, internal/cost/cost_test.go,
            internal/config/config.go, internal/config/config_test.go,
            deploy/mcp-clickhouse/,
            go.mod, go.sum, .env.example,
            dev-diary/infrastructure.md, dev-diary/PHASE-8-ship.md
status:     not-started
```
Build the editor agent on `google.golang.org/adk/v2`. The agent answers questions about the
ledger and proposes mutations. It reads the ledger through a self-hosted mcp-clickhouse server.

**Why this task exists.** `project.md` named `google/adk-go` and `mcp-clickhouse` in the stack
table from the first commit `654db86`. No phase task ever owned either one, so the repository
described a stack the code did not have. `mcp-analysis.md` records the read path decision and
the rejected ClickHouse Cloud alternative. This task lands it.

#### The read and write split

Ledger writes stay on the durable client in `internal/ledger`, the single writer. Queue,
batching and reconcile semantics from T4.1 do not change. The agent reads, and it never
composes SQL that inserts.

Read-only holds twice, and the two halves carry unequal weight. The `mcp_readonly` database
user holds SELECT grants and nothing else, and that grant is the security boundary. The server
flag `CLICKHOUSE_ALLOW_WRITE_ACCESS=false` guards against accidents, and the upstream README
declines to call it a boundary. Correct `infrastructure.md`, which presents the two as equal.

#### Wiring the toolset

`mcptoolset.Config` takes an `Endpoint` string and builds a streamable HTTP transport from it.
Set `Auth` to a credential provider carrying the static bearer token. Auth requires that HTTP
transport, so passing a command transport instead is a configuration error.

Narrow the toolset with `tool.FilterToolset` to the read tools this agent needs. Prefer a named
list over exposing `run_query` unfiltered.

The agent proposes and T6.4 validates. Deterministic Go code performs every timeline
calculation, exactly as T6.4 already requires. An agent turn never applies a mutation itself.

#### Configuration

`.env.example` already carries the five MCP variables, including
`CLICKHOUSE_MCP_SERVER_TRANSPORT=http` and `CLICKHOUSE_MCP_ALLOWED_HOSTS`. The server defaults
to stdio without the first and rejects every request without the second. Verify both reach the
container rather than adding them again.

`CLICKHOUSE_READONLY_PASSWORD` and `CLICKHOUSE_MCP_AUTH_TOKEN` are secrets. Read the
"Configuration and Secrets" section of `infrastructure.md` before wiring the container. The
container receives the `mcp_readonly` password and never the writer password.

#### Deployment

No published image exists. Docker Hub carries no `clickhouse/mcp-clickhouse` repository. The
upstream project ships a Dockerfile, so `deploy/` builds the image or pins a PyPI install.
Correct the T8.2 text in `PHASE-8-ship.md`, which reads today as though an image gets pulled.

The server listens on localhost and never reaches Caddy.

#### Agent turns cost money

`internal/cost` names `ChargeSegment`, `ChargeTranslate` and `ChargeSynthesize`. An agent turn
calls Gemini and bills tokens, and no kind covers it. Add `ChargeAgent` and its two token rates
to `RateCard`. This amends T1.2 a second time, after T2.2dev. Amend it openly and record the
change, so T4.2 reads one contract rather than three.

The exit criteria below require itemized prices before execution. An agent turn is a billable
operation and shows its price like every other one.

#### Failure stays off the critical path

If mcp-clickhouse stops, the agent loses its read tools. Dubbing, editing, export and the
ledger keep running, because nothing load-bearing routes through MCP. Build no fallback path,
and never let an agent retry block a user action.

#### What the dependency costs

ADK moves the module graph from 79 modules to 125. It pulls `github.com/openai/openai-go/v3`
in transitively, which belongs to ADK rather than to this repository. `AGENTS.md` already
carries the stack exception and the import fence, so this task leaves that file alone.

**Done when:** `go.mod` requires `google.golang.org/adk/v2`, and only `internal/agent` imports
it. The offline suite builds the toolset against a fake MCP server and passes with no network
call. A live probe behind `//go:build live` reaches a running mcp-clickhouse container, lists
the ledger tables and reads commits for one title. That probe also confirms an INSERT through
the agent path fails on the `mcp_readonly` grant. Agent turns emit `ChargeAgent` carrying the
token counts the response reported. `.env.example` starts a listening server as written.
`infrastructure.md` names the grant as the boundary, and T8.2 builds the image.

---

## Exit Criteria

- [ ] Boundaries, speakers, and text support manual user editing.
- [ ] Billable operations display itemized prices before execution.
- [ ] Command bar parses instructions into validated deterministic mutations.
- [ ] Timeline edits preserve prior takes without destructive overwrites.
- [ ] All mutations write author-attributed commits to ClickHouse.
- [ ] The editor agent reads the ledger through mcp-clickhouse and writes nothing.

---

## Handoff Log

_(Fill on completion: document command parser grammar and boundary dragging sensitivity.)_
