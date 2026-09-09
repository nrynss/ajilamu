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
owns:       web/src/lib/edit/Boundary.svelte, web/src/routes/d/[id]/+page.svelte
status:     done
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
owns:       web/src/lib/edit/Speaker.svelte, web/src/routes/d/[id]/+page.svelte
status:     done
```
Provide quick speaker toggles per dialogue line. Changing speaker attribution alters voice assignment and requires re-rendering.

Always display estimated re-rendering costs before triggering synthesis calls. Never run billable operations in the background silently.

**Done when:** Changing a speaker displays the associated re-render fee and leaves original takes untouched if canceled.

---

### T6.3: Text correction ★
```yaml
requires:   T5.5, T6.5a
fixture-ok: yes
size:       M · mid
owns:       web/src/lib/edit/Text.svelte, web/src/routes/d/[id]/+page.svelte
status:     done
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
owns:       internal/command/parse.go, internal/command/parse_test.go,
            internal/api/command.go, internal/api/command_test.go, internal/api/server.go,
            web/src/lib/edit/CommandBar.svelte, web/src/routes/d/[id]/+page.svelte
status:     done
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

### T6.5a: Line re-render route ★
```yaml
requires:   T6.2, T2.7
fixture-ok: no
size:       M · frontier
owns:       internal/api/rerender.go, internal/api/rerender_test.go,
            internal/api/server.go, cmd/ajilamu/main.go
status:     done
```
Re-run one dialogue line through the fit loop and save the output as a new take. Never
overwrite a previous take. Preserve `seg_3_try1.wav` when creating `seg_3_try2.wav`.

`RepairLine` with `RewriteConfig.InitialText` makes a corrected target text authoritative,
so a re-render after a text correction calls no translation model. The route accepts an
optional target text and an optional speaker, so a speaker change re-renders with the
voice `tts.Assign` maps to the new speaker.

`internal/api` cannot import `internal/fit`, so the route reaches the renderer through an
api-owned interface that `cmd/ajilamu/main.go` adapts. The route records the new take, its
charges and one commit in ClickHouse.

**Done when:** A POST re-render of one line writes `seg_3_try2.wav` beside
`seg_3_try1.wav`, both decode, a stand-in ClickHouse records one take, one charge row and
one commit, and the response names the new take.

---

### T6.5: Ghost take history ★
```yaml
requires:   T6.3, T6.5a
fixture-ok: yes
size:       S · mid
owns:       web/src/routes/d/[id]/+page.svelte, web/src/lib/Track.svelte,
            web/src/lib/edit/Speaker.svelte
status:     done
```
Display the cost estimate before a re-render runs, and render older takes as ghost
outlines on the timeline. The page calls T6.5a's route and keeps the active take as the
only audible one.

**Done when:** The timeline shows ghost take history, both takes play independently, and
the estimate shows before the call runs.


Round one filed one H and two M. The H needs a server route, so it moved to T6.5c. The two
M stay here: the ghost history must sit in the timeline lanes, and the Speaker note must
stop claiming the take is unchanged once a re-render lands.

---

### T6.5c: Serve take audio ★
```yaml
requires:   T6.5, T6.5a
fixture-ok: no
size:       M · frontier
owns:       internal/api/takes.go, internal/api/takes_test.go,
            internal/api/server.go, cmd/ajilamu/main.go,
            web/src/routes/d/[id]/+page.svelte
status:     done
```
A ledger payload sets `Take.File` to an absolute server path, and no route serves a take
file. Every take of a real project is unplayable, so the fixture hides the defect.

Serve a take file over HTTP and resolve `take.file` to that route on the workspace page.
Range requests keep scrubbing instant, and a path outside the storage root must fail.

**Done when:** A real project's takes play from the served route, the fixture path is
unchanged, and a traversal attempt is refused.

---

### T6.5b: Source-text re-translation path ★
```yaml
requires:   T6.5a, T6.3
fixture-ok: no
size:       M · frontier
owns:       internal/api/rerender.go, internal/api/rerender_test.go,
            internal/api/server.go, cmd/ajilamu/main.go,
            internal/fit/rewrite.go, internal/fit/rewrite_test.go,
            web/src/lib/edit/Text.svelte,
            web/src/routes/d/[id]/+page.svelte
status:     done
```
T6.3's contract change records the gap. The re-render route takes a target text only, so a
corrected source line cannot be re-translated. Add an optional `source_text` to the route
body. When it is present, set the segment text before the fit loop runs and run the
translation model, because no authoritative target text exists yet. Save a new take beside
the old ones. Record the take, its charges, one commit and one timeline snapshot under the
`text_corrected` action with the `manual_ui` author.

Wire the source confirm on the workspace page to that path, replacing the prompt that
reports the gap today.

**Done when:** A corrected source line re-translates and re-renders as a new take, the
translation model runs on that path and zero times on the target-text path, and the page
applies the response.

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
            internal/api/wire.go, internal/api/wire_test.go,
            web/src/lib/types.ts,
            deploy/mcp-clickhouse/, tools/audit_docs.py,
            go.mod, go.sum, .env.example,
            dev-diary/infrastructure.md, dev-diary/PHASE-8-ship.md
status:     done
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

### T6.6a: Editor agent route
```yaml
requires:   T6.6, T7.0, T7.2c
fixture-ok: yes
size:       M · frontier
owns:       internal/agent/route_test.go,
            internal/api/agent.go, internal/api/agent_test.go,
            internal/api/server.go, internal/api/server_test.go,
            internal/api/wire.go, internal/api/wire_test.go, testdata/wire/,
            web/src/lib/types.ts,
            cmd/ajilamu/main.go, cmd/ajilamu/main_test.go
status:     done
```
T6.6 built `internal/agent` and nothing calls it. No file in `cmd/ajilamu` or `internal/api`
imports the package, so a running server cannot reach the agent. The P6 close review recorded
the agent turn as having no route and no price.

Add `POST /api/dubs/{id}/agent`. The body carries one question. The response carries the
answer, the `[]cost.Charge` the turn produced, and their total in nanodollars. Define the wire
type in `internal/api/wire.go`, mirror it in `web/src/lib/types.ts`, and add its example under
`testdata/wire/`.

The entrypoint builds the agent through `agent.NewFromConfig` only when `MCPConfigured` holds.
A server without MCP settings answers the route 503 with one sentence and serves everything
else. The route never writes to the ledger. The T6.6 handoff notes that `internal/ledger/takes.go`
drops an unknown charge kind, so recording agent charges in the ledger stays out of this task.

**Done when:** The route answers through the offline fake transport with the answer and its
charges. A server without MCP settings answers 503 and still serves the workspace. The returned
total equals the reported token counts priced by the rate card.

---

### T6.6b: Ask the agent from the command bar
```yaml
requires:   T6.6a, T6.4, T5.5a
fixture-ok: yes
size:       S · mid
owns:       web/src/lib/edit/CommandBar.svelte, web/src/routes/d/[id]/+page.svelte
status:     not-started
```
The command bar parses instructions through `POST /api/editor/commands/preview`. Text the
parser rejects stops at the 422 sentence. Give that text a second path. When the parser
answers 422, offer to ask the editor agent, and post the text to `POST /api/dubs/{id}/agent`.

Show the agent's answer in the bar with the charge the turn cost. A token count is unknown
before the turn runs, so show the rate card before the ask and the measured charge after it.
Say so in one sentence rather than showing a price the turn has not earned.

An answer that proposes a command lands in the preview path unchanged. The agent never applies
anything, and the page never treats its answer as a mutation.

**Done when:** A rejected instruction offers the ask, and the answer renders in the bar with
its measured charge. A proposed command runs through the preview route, and no agent answer
mutates the timeline directly.

---

### T6.7: Record boundary and command edits as commits ★
```yaml
requires:   T6.1, T6.4, T6.5c, T4.5
fixture-ok: no
size:       M · frontier
owns:       internal/api/mutations.go, internal/api/mutations_test.go,
            internal/api/server.go, cmd/ajilamu/main.go,
            cmd/ajilamu/main_test.go,
            web/src/routes/d/[id]/+page.svelte
status:     done
```
The P6 close review filed one H. `handleBoundaryChange` and `confirmCommand` in the
workspace page write browser state only and send no request, so a boundary drag or a
command-bar confirmation never reaches the ledger. The History tab never shows it, and a
reload loses it. Exit criterion five is unmet.

`internal/ledger/actions.go` already accepts `boundary_nudged` with the `manual_ui` author
and `user_command` with the `command_bar` author. `HistoryTab.svelte` already renders both
sentences. Add the write path: one route that records the edit as a commit, an action and a
timeline snapshot, and wire the two page confirmations to it.

**Done when:** A boundary drag and a command confirmation each write one commit, one action
and one timeline snapshot, the History tab shows both, and the edit survives a reload.

---

### T6.7a: Read-only fixture workspace
```yaml
requires:   T6.7, T5.7
fixture-ok: yes
size:       S · mid
owns:       web/src/routes/d/[id]/+page.svelte, web/src/lib/fixture.ts
status:     blocked
```
The P6 close review recorded this in its notes. The fixture dub at `/d/fixture` has no ledger,
so every boundary drag, speaker change, text edit and command confirmation posts to
`POST /api/dubs/fixture/edits` and answers 404. The page shows the failure sentence and snaps
the control back. The user sees an error for a state the product designed.

Treat a fixture dub as read-only. `isFixtureID` already names it. Disable each edit confirm and
show one sentence that says the offline fixture cannot save an edit. No request leaves the
page. A real project keeps the T6.7 write path unchanged.

**Done when:** On `/d/fixture` no edit request leaves the page and each edit control shows the
read-only sentence. On a project with a ledger a boundary drag still writes one commit.

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

### T6.4 command parser

`Parse` accepts four grammars and returns a `Mutation`. It does not read a timeline.

- `move wav|line <id> to <minutes:seconds> to left|right`
- `shift line <id> left|right by <duration>`
- `change speaker for line <id> to <name>`
- `shorten line <id> by <duration>`

A trailing period is optional. `0:0005` is five padded seconds, so 5000ms.
Durations end in `ms` or `s`. `0.5s` is 500ms.

`Validate` then `Apply` do the arithmetic. They reject unknown lines, unknown or
ambiguous speakers, non-positive durations, out-of-timeline bounds, and overlaps.
`Describe` is the confirmation sentence. The browser must show it before it writes
the Go-derived segment.

`POST /api/editor/commands/preview` is the only command route. T6.6 may propose
richer language later. It still has to land on this mutation and this validator.

T6.1 documented boundary snap and the 100ms minimum in
[t6.1-round3.md](adversarial-review/t6.1-round3.md). This task does not change that.

### Seam expansion and the T6.5 split, 2026-09-09

T6.2, T6.3 and T6.5 each compose into the workspace route. T6.5 also mounted an HTTP
route. Their `owns` lines named neither the route nor the server files, so no
implementer could finish inside its seam. The orchestrator expanded them before
dispatch.

- T6.2 and T6.3 gain `web/src/routes/d/[id]/+page.svelte`.
- T6.6 gains `internal/api/wire.go`, `internal/api/wire_test.go`,
  `web/src/lib/types.ts` and `tools/audit_docs.py`. An agent charge crosses the
  wire, so the UI needs its kind. The audit script holds the ADK pending entry
  that T6.6 retires.

T6.5 split. T6.3's done condition needs a per-line synthesis trigger, and T6.5 owned
that trigger. Landing T6.3 first would ship a call to a route that did not exist.
T6.5a now owns the route, its tests and the two server files. T6.3 requires T6.5a and
owns the text fields plus the page. T6.5 keeps the ghost take history and the estimate
display, and it requires T6.3 and T6.5a.

Shared paths carry a serialization rule. One task owns each at a time. The page
passes from T6.2 to T6.3 to T6.5. The server files pass from T7.6 to T7.4a to T7.5
to T6.5a.

T6.2 shows the re-render fee from the measured charges on the selected line's newest
billed take. A line whose takes carry no charges reads zero. That number is already
on the payload. No new endpoint carries it.

### T6.2 speaker reassignment

`Speaker.svelte` shows one toggle per speaker for the selected line. The toggle set
comes from the distinct `Segment.speaker` values plus the current speaker, sorted.
For `/d/fixture` that set is exactly `Mark Vande Hei` and `Suni Williams`. The panel
never invents a speaker, so it cannot bind a name that `tts.Assign` rejects.

The estimate sums `Take.Charges` on the selected line's newest take whose
`charges` array is non-empty. The panel labels it as estimated and names that
take. Line 1 shows `$0.0008421` from `seg_1_try1.wav`. Line 7 shows `$0.004751`
from `seg_7_try1.wav`. Neither line moved. Line 3 shows `$0.0023182` from
`seg_3_try1.wav`. Line 4 shows `$0.0036392` from `seg_4_try1.wav`. A line whose
takes carry no charges reads zero and names that take. The hint reads: "A new
speaker needs a fresh voice render. The estimate uses the newest billed take, or
reads zero when none is billed."

Changing a speaker opens a confirmation panel. Cancel closes it and leaves every take
untouched. Confirm applies the speaker to route-local state and states that the take
still needs a re-render. Nothing here fetches. A headless Chromium run against the Go
server triggered zero synthesis calls and zero API calls. T6.5a owns the route that
will make the confirm billable, so T6.3 must keep this confirmation before that call.

Surprise. The first draft ordered toggles by first appearance in `segments`. Confirming
a change to line 1 reordered the buttons, because line 1 then led with the new speaker.
Sorting the names keeps the order stable.

`npm --prefix web run check` passes with zero errors and zero warnings. The changed
files carry no `export let`, no `$:` and no store.

### T6.6 editor agent on ADK

`internal/agent` builds the editor agent on `google.golang.org/adk/v2` v2.3.0. It is the
only package that imports ADK. `NewToolset` sets `mcptoolset.Config.Endpoint` and passes
`auth.StaticToken` as `Auth`, so the toolset uses the streamable HTTP transport. It then
narrows the server to `list_databases`, `list_tables` and `run_query` through
`tool.FilterToolset`. A transport seam lets the offline suite build the same toolset
against an in-memory fake server. No socket opens, so no network call happens.

`Agent.Ask` runs one turn and returns the answer plus `[]cost.Charge`. An ADK
`AfterModelCallback` records `cost.ChargeAgent` from the response `UsageMetadata`, so a
turn emits the prompt and candidate token counts it reported. The agent reads and never
writes. No route calls it yet, because no task owns an agent endpoint.

`internal/config` reads the five MCP variables as optional settings. `MCPConfigured`
requires the URL and the bearer token. A missing value makes `NewFromConfig` return a nil
agent and never stops the server. `TestMissingMCPDisablesAgentNotServer` and
`TestNewFromConfigDisablesWithoutMCP` pin that.

`deploy/mcp-clickhouse/` builds an image from `mcp-clickhouse==0.6.0`, because no published
image exists. The container runs as the `mcp_readonly` OS user, connects as the
`mcp_readonly` database user, sets `CLICKHOUSE_ALLOW_WRITE_ACCESS=false`, and binds host
loopback only. `infrastructure.md` now names the SELECT grant as the boundary and the flag
as an accident guard. T8.2 now reads as building the image.

**T1.2 amendment, second after T2.2dev.** `cost.ChargeAgent` joins `ChargeKind`, and
`RateCard` gains `AgentPerPromptToken` and `AgentPerCandidateToken` at 150 and 600
nanodollars. `internal/api/wire.go`, `web/src/lib/types.ts` and the `isChargeKind` guard
landed together. `testdata/wire/charge.json` stays a translate example. T4.2 now reads one
charge contract.

Surprises. The ledger stores no title column, so the live probe names a dub by id and
reports the fixture title beside it. `internal/ledger/takes.go` maps only the three
existing kinds into charge rows, and it drops an unknown kind silently. That stays correct
while the agent never writes a take. A future task that records agent turns in the ledger
must extend that switch. `web/src/lib/tabs/DetailsTab.svelte` labels any kind
other than segment or translate as a voice render, so an agent charge needs a label there.
T5.5 owns `web/src/lib/tabs/` and is done, so a defect there is a new task or a finding,
not an unowned blind spot. ADK moved `go list -m all` from 79 entries to 126, and `go.mod`
grew from 34 require lines to 45.

The live probe sits behind `//go:build live`. It reads `SHOW GRANTS FOR mcp_readonly`,
requires a SELECT-only grant set, lists tables, reads commits for one dub, and refuses an
INSERT. No container ran here, so it has no transcript. `go build ./...`
and the scoped tests pass. `python3 tools/audit_docs.py` prints no pending line.

### T6.5a line re-render route

`POST /api/dubs/{id}/lines/{segment}/rerender` re-runs one dialogue line through the
fit loop. It writes the result as a new take beside the previous ones. It never
overwrites an existing take.

The body is optional JSON. Every field is optional.

```json
{"language": "ml-IN", "text": "corrected target line", "speaker": "Mark Vande Hei"}
```

`language` overrides the project's stored target language. Without it the route
resolves the language from `project.json`. The route maps the code to its catalog
tag, so `ml` and `ml-IN` name one track. `text` is the authoritative target line.
`speaker` reassigns the line to another voice owner. An empty body re-renders the
stored line with its stored speaker.
A project with no upload record needs the `language` field, because the bundled
fixture stores no code to fall back to.

The response is 201.

```json
{
  "segment_id": 3,
  "language": "ml-IN",
  "commit_id": "99114c542ff4ab74415e13c014396a25",
  "total_nanodollars": 630000,
  "sentence": "Line 3 re-rendered as take seg_3_try2.wav.",
  "take": {
    "take_id": "a5e9b83bc5f9455950543d405939a8e8",
    "file": "/data/uploads/probedub/work/ml-IN/seg_3_try2.wav",
    "name": "seg_3_try2.wav",
    "attempt": 2,
    "voice": "ml-IN-Chirp3-HD-Achird",
    "repair": "none",
    "repair_detail": "fits slot within dead band",
    "flagged": false,
    "fit": {"slot_ms": 5320, "measured_ms": 5320, "delta_ms": 0, "state": "fits"},
    "charges": [{"kind": "synthesize", "segment_id": 3, "take_file": "seg_3_try2.wav",
      "units": 21, "unit_price_nanodollars": 30000, "total_nanodollars": 630000}]
  }
}
```

T6.3 calls it after a target-text edit. Send `{"text": "<the edited line>"}`. The
route passes that text as `RewriteConfig.InitialText` and sets
`RewriteConfig.AuthoritativeText`, so every attempt speaks the creator's line and
the loop calls no translator. The loop flags a line that still misses the slot
rather than rewriting it. A measured run made zero translation calls and three TTS
calls. T6.3 must keep its confirmation panel before the call, because the call bills.

T6.5 calls it after a speaker change and for a re-roll. Send
`{"speaker": "<name>"}` or an empty body. The route replaces the segment speaker
before the fit loop, so `tts.Assign` maps the new speaker to its own voice. A
measured run sent Cloud TTS `voice.language_code=ml-IN` and
`voice.name=ml-IN-Chirp3-HD-Achird` for `Mark Vande Hei`. T6.5 shows the estimate
first, then refreshes the workspace payload after the 201.

The route records the take, its charges, one commit, one action and one timeline
snapshot through `api.RunRecorder`. A stand-in ClickHouse measured one `takes_raw`
row, one `charges_raw` row and one `commits_raw` row. The take row carries `attempt`
2, so the ledger attempt matches the file name. The route answers 409 while a run
holds the project. It reads the head timeline under the caller's language code
first, then falls back to the resolved tag.

`internal/api` cannot import `internal/fit`. `api.LineRenderer` is the seam, and
`*pipelineRunner` in `cmd/ajilamu/main.go` adapts it. `RenderLine` holds the runner
mutex, so a re-render never overlaps a run. It builds a per-call synthesizer from the
resolved tag and a per-call charge ledger. The handler serializes re-renders, so two
requests cannot choose one take file.

Surprises. `fit.RepairLine` reads `InitialText` on attempt one alone unless the
caller sets `RewriteConfig.AuthoritativeText`. That mode speaks the text on every
attempt, may still apply an atempo stretch, and never translates. The route always
passes the stored or corrected text, so a speaker-only re-render also skips
translation. `RecordTake` rejects a charge whose `TakeID` differs from the segment,
so the route attributes every charge to the line. The ledger's `takes_raw.text`
column carries the source line, because `TakeAttempt.rows` writes `Segment.Text`.

Measured on 2026-09-09 with a throwaway harness in package main. A POST with a
corrected text and a new speaker wrote `seg_3_try2.wav` beside `seg_3_try1.wav`.
The probe `sha256sum testdata/takes/seg_3_try1.wav` prints
`8928864cd133b7c718c50eec7088f6e2d7eecdfcd39201676504f5369c43ec6c` before and after
the call. The probe `ffprobe -v error -show_entries format=duration
testdata/takes/seg_3_try1.wav` prints 5.720375 seconds. The new take measures
5.320000 seconds, and `TestRerenderReadsTheLedgerTrackUnderTheCallerCode` pins it
with `ffprobe`. `internal/api/rerender_test.go` keeps the route tests with a
stand-in renderer.

### T6.3 text correction

`Text.svelte` edits the selected line's transcribed source sentence and translated
target line in place. It composes into the editor section beside Boundary and
Speaker. It occupies its own full-width row, so the two earlier panels keep their
layout.

The source element carries the project's source language when the project knows it.
The target element carries the active target language. On the probe dub the source
reads `lang="en"` and the target reads `lang="ml"`.

Correcting the target text is authoritative. The field action opens a confirmation
panel. The panel names the estimate from the newest billed take and states that no
translation model runs. Confirm sends one request.

```text
POST /api/dubs/t63-real-dub/lines/3/rerender
{"language":"ml","text":"ഹായ്, ഞാൻ സുനി വില്യംസ് ആണ്, തിരുത്തിയത്."}
```

The 201 answer names `seg_3_try2.wav`. The page appends that take to the active
track line, sets the line text to the corrected text, and closes the panel. The
Text panel's take list and the Speaker panel's take list both then read
`seg_3_try1.wav,seg_3_stretched.wav,/tmp/t63/storage/t63-real-dub/work/ml-IN/seg_3_try2.wav`.
The route returns that absolute path for the new take, which matches the Surprises
paragraph. The estimate basis moves to the new take. Cancel sends nothing and keeps the
draft.

Zero translation calls hold for that path. A throwaway overlay test measured
`fit.RepairLine` with `InitialText` set. The translator recorded zero calls and the
synthesizer recorded one call. The same run with an empty `InitialText` recorded one
translator call. `internal/fit/rewrite.go` takes the authoritative text on attempt
one and skips `cfg.Translator.Translate`.

Correcting the source text cannot re-translate yet. The landed re-render route
accepts `language`, `text` and `speaker`. A supplied `text` is a target line. No
handler writes a corrected source line. The source field action opens a prompt.
Confirm reports the gap, sends nothing and calls no model. The record in [t6.3-contract-change.md](adversarial-review/t6.3-contract-change.md) names the missing path and its owner.

Surprises. The page sends `language`, because the bundled fixture stores no
language code to fall back to. The 201 take `file` is an absolute server path, so
`fixtureTakeSource` cannot resolve it and `Play this take` does nothing for the new
take. T6.5 owns the ghost take history and needs an asset URL for it. The draft
reset effect ignores prop changes while a prompt or a call is open, so a slow
re-render cannot clobber an open panel.

Measured on 2026-09-09 against a stand-in API server that served the built bundle
and the fixture payload under a non-fixture id. The stand-in logged the POST and
answered the documented 201 shape. It makes no outbound call. The browser raised
zero console messages and zero page errors. `npm --prefix web run check` reports
183 files, 0 errors and 0 warnings. Neither changed file carries `export let`, `$:`
or a store.

### T6.5b source-text re-translation path

`POST /api/dubs/{id}/lines/{segment}/rerender` now takes an optional `source_text`.
A supplied `source_text` replaces the stored transcript before the fit loop runs.
The route then sends an empty target text, so the fit loop translates the corrected
source on attempt one. The 201 body gained `text` and `source_text`. A supplied
`text` stays authoritative on every attempt and never translates.

The route records this path under the `text_corrected` action with the `manual_ui`
author. `api.CorrectedRecorder` carries the provenance. `cmd/ajilamu/main.go` maps
it onto the ledger. A stand-in ClickHouse captured the row.

```json
{"action_id":"61cfad4111c703322797cfea920de94a","commit_id":"a17af21ff25818a667843600f94148d1","project_id":"live-source-dub","dub_id":"live-source-dub","owner_id":"local","language":"ml-IN","segment_index":3,"take_id":"","action_type":"text_corrected","author":"manual_ui","prompt":"","before_value":"","after_value":""}
```

`Text.svelte` now sends `source_text` from its source confirm. The page applies the
answer to the source segment and the active track line. The confirm closed and the
note read `Line 3 re-rendered as take seg_3_try2.wav.`

Measured on 2026-09-09. A live run against Gemini and Cloud TTS answered 201 with a
real Malayalam line and `seg_3_try3.wav`. The commit message read `Corrected the
source of line 3 and re-rendered it.` The takes `seg_3_try1.wav` through
`seg_3_try4.wav` kept their md5 hashes across a later target correction.

Translation counts came from the real fit loop with a counting translator. A
slot-fitting take gave one translation call on the source path and zero on the
target-text path. A corrected target that missed its slot also gave zero
translation calls, and the take spoke the creator's line.
`TestRerenderCorrectedSourceRetranslates`, `TestRerenderCorrectedTargetSkipsTranslation`,
and `TestRerenderCorrectedTargetMissNeverTranslates` pin those counts.

Surprises. `fit.RepairLine` only read `InitialText` on attempt one, so attempts two
and three translated when the take missed its slot. A live target correction paid
for two translation calls while the route itself called the translator zero times.
Round 1 of the T6.5b review found the gap. `RewriteConfig.AuthoritativeText` now
speaks the creator's text on every attempt, and the loop flags a line that still
misses. `RenderLine` names the target and source language on the translator
request, because it calls the translator directly and skips the fit loop wrapper.
The live model needs `GOOGLE_CLOUD_LOCATION=global`. The region `us-central1`
answers 404 for `gemini-3.8-flash`.

Seam expansion. T6.5b's owns line gained `web/src/lib/edit/Text.svelte`. The source
confirm that reported the gap lives there, not on the page. The orchestrator granted
the expansion before the edit.

`go test -count=1 ./internal/api ./internal/fit ./cmd/...` reports ok for all three
packages.
`npm --prefix web run check` reports 183 files, 0 errors and 0 warnings.

### T6.5 ghost take history

The page renders a take history strip for the selected line. The strip sits in the
timeline editor section above the Timeline component. Each take is a button. Older takes
render as ghost outlines. The active take renders as a solid chip.

Measured on 2026-09-09 against a stand-in API server. The server served the built bundle
and the fixture payload under the non-fixture id `t65-real-dub`. Line 3 has two takes.

The ghost chip carries `data-take-file="seg_3_try1.wav"` and
`data-take-state="ghost"`. Its computed style is a dashed border, a transparent
background and opacity 0.72. The active chip carries
`data-take-file="seg_3_stretched.wav"` and `data-take-state="active"`. Its computed
style is a solid border and the `--fit` fill `rgb(143, 165, 140)`.

Both takes play independently. A ghost click fetched
`/_app/immutable/assets/seg_3_try1.xBIiyNfY.wav` with status 200 and type
`audio/wav`. An active click fetched
`/_app/immutable/assets/seg_3_stretched.ClDJvJby.wav` with status 200 and type
`audio/wav`. Each click fetched exactly one take. `playTake` pauses the previous take
first, so only one take is audible at a time.

The speaker confirm now calls the re-render route. The Speaker panel shows the estimate
before the call. The measured sequence is:

1. Choosing `Mark Vande Hei` showed the fee `$0.0023182` from `seg_3_try1.wav` at
   `t=1788910220727`. Zero POST requests existed then.
2. Confirming sent `POST /api/dubs/t65-real-dub/lines/3/rerender` with the body
   `{"language":"ml","speaker":"Mark Vande Hei"}` at `t=1788910221137`.
3. The 201 answer appended `seg_3_try3.wav`. The note read
   `Line 3 re-rendered as take seg_3_try3.wav.` The chips then read ghost, ghost,
   active.

The route records each take, so the older takes stay on disk and now render as ghosts.
T6.2 measured zero API calls on a speaker confirm. That confirm calls the route now,
because the T6.5a handoff assigns the call to T6.5. The re-roll path with an empty body
stays unwired. No page control asks for one.

A new take returns an absolute server path in `file`, so `fixtureTakeSource` cannot
resolve it. `playTake` now names that gap instead of doing nothing. A click on the new
take showed `The take /tmp/t65-storage/t65-real-dub/work/ml-IN/seg_3_try3.wav has no
browser audio URL.` No HTTP route serves a take file yet, so the page cannot build a
URL. A later task must add that route.

The fixture id still loads the bundled fixture. `/d/fixture` made zero
`/api/dubs/fixture` requests and rendered the same ghost and active chips for line 3.

Seam. `Track.svelte` owns the timeline lanes and renders the active take plus every older
take as a dashed ghost shape inside the lane. The owns line already lists `Track.svelte`,
so the lane work needs no owns change. The page-owned strip stays as the keyboard and
playback control. `LengthBar.svelte` already paints older takes as ghost shapes on its
canvas at alpha 0.28.

Surprise. The measurement browser ran `page.evaluate` in an isolated world. Window
patches missed the app, so the first audio probes saw nothing. Network capture proved
the real sources. The page was healthy the whole time.

`npm --prefix web run check` reports 183 files, 0 errors and 0 warnings. The browser run
raised zero console messages and zero page errors. The changed file carries no
`export let`, no `$:` and no store.

### T6.5c serve take audio

The route is `GET /api/dubs/{id}/takes/{language}/{name}`. `internal/api/server.go`
mounts it with `TakeAudioHandler(options.StorageDir, logger)`. The handler lives in
`internal/api/takes.go`.

`language` resolves through `runWorkDirFor`, the same resolver the run route and the
re-render route use. `name` must be one path segment. The handler rejects a separator, a
dot, or a double dot. It then requires the joined path to stay inside the storage root.
A missing file answers 404. An unwired storage root answers 503.

`http.ServeContent` answers a range request with 206 and `Content-Range`. It sets
`Content-Type` from the extension and `Accept-Ranges: bytes`.

The page resolves a take in `takeAudioSource`. A fixture id still calls
`fixtureTakeSource`. A real project calls `servedTakeURL`. That helper takes the last
path segment of `take.file` and builds
`/api/dubs/{project}/takes/{activeLanguage}/{name}`. The chips and the `Play this take`
control both call `playTake`, so both use the route. The page never sends the server path
to the browser. The old gap sentence is gone.

Measured on 2026-09-09 against `cmd/ajilamu` on port 8799 with `AJILAMU_DATA_DIR`
holding a real project.

1. The request `GET /api/dubs/t65clive/takes/ml-IN/seg_3_try1.wav` answered 200 with
   `Content-Type: audio/wav` and 183096 bytes. `ffprobe` read the served bytes as wav,
   5.720375 seconds.
2. `Range: bytes=1000-2999` answered 206 with `Content-Range: bytes 1000-2999/183096`,
   `Content-Length: 2000` and `Content-Type: audio/wav`.
3. The encoded relative escape
   `..%2F..%2F..%2F..%2F..%2Ftmp%2Ft65c-outside%2Fsecret.wav` answered 404. The encoded
   absolute path `%2Fetc%2Fpasswd` answered 404. A language escape answered 404. A raw
   `..` path answered 307 and then 404.
4. `/d/fixture` still plays bundled assets. A chip click requested
   `/_app/immutable/assets/seg_1_try1.CwhAvsFF.wav` and no `/takes/` route.
5. Headless Chromium on the built bundle played a real project from both controls. The
   `Try 1` chip requested `GET /api/dubs/t65clive/takes/ml/seg_3_try1.wav` with
   `Range: bytes=0-`, status 206 and `Content-Range: bytes 0-183095/183096`. The `Play
   this take` control requested `GET /api/dubs/t65clive/takes/ml/seg_3_try2.wav`,
   status 206 and `Content-Range: bytes 0-53815/53816`. The language read `ml` because
   the stand-in payload carried the sample code.
6. `go test -count=1 ./internal/api ./cmd/...` reports `ok` for both packages.
   `npm --prefix web run check` reports 183 files, 0 errors and 0 warnings.

Seam. `cmd/ajilamu/main.go` needed no change. It already passes `uploadDir` as
`StorageDir`, the same root the run and re-render routes write takes into.

Surprise. The route carries the language because a take basename is not unique across
language tracks. The page names the active language, so the resolver finds the right work
directory.

`internal/api/takes_test.go` covers the served bytes, the range answer, three escapes,
and an unwired storage root.

### T6.7 boundary and command edits reach the ledger

The route is `POST /api/dubs/{id}/edits`. `internal/api/server.go` mounts it with
`EditsHandler(options.Edits, options.History, logger)`. The handler lives in
`internal/api/mutations.go`.

The body names one kind.

```json
{"kind":"boundary","language":"ml-IN","segment_id":1,"start_ms":0,"end_ms":2950}
{"kind":"command","language":"ml-IN","command":"shift line 2 right by 250ms","duration_ms":75008}
```

A boundary carries the dragged `start_ms` and `end_ms`. A command carries the instruction
and the timeline length the validator bounds against.

The handler reads the head timeline, derives the result, and records it through
`api.EditRecorder`. The adapter in `cmd/ajilamu/main.go` writes one commit, one action and
one timeline snapshot. A boundary writes `boundary_nudged` with the `manual_ui` author
and an empty prompt. A command writes `user_command` with the `command_bar` author and
the instruction verbatim as the prompt.

The route never trusts a browser-supplied result. It validates a boundary against the
head timeline and derives a command through `internal/command`. A rejected edit answers
400 or 422 with the validator sentence and writes nothing.

The 201 body carries the commit id, the action, the author, the resulting segment and a
sentence.

```json
{"commit_id":"d84ee6ac...","action":"user_command","author":"command_bar",
 "segment":{"id":2,"start_ms":3750,"end_ms":8250,"duration_ms":4500,"speaker":"Suni"},
 "sentence":"Saved the command. Line 2 now runs from 3.750s to 8.250s."}
```

The page calls the route from `recordEdit` in `web/src/routes/d/[id]/+page.svelte`.
`handleBoundaryChange` sends a boundary. `confirmCommand` sends the previewed command.
Both apply the returned segment to `dub.segments` only after the write succeeds. A failed
write sets `lineNote` to the server sentence and remounts `Boundary`, so the editor
cannot show a boundary that never saved.

Measured on 2026-09-09 against `cmd/ajilamu` on port 8080 and ClickHouse 26.8.2.7 through
`sql/schema.sql`. The dub `t67-real-dub` seeded one commit, three timeline rows and three
takes.

1. A drag on line 1's end handle from 3000ms to 2950ms wrote commit version 2. The action
   row reads `boundary_nudged`, `manual_ui`, empty prompt,
   `before_value {"start_ms":0,"end_ms":3000,"speaker":"Mark"}` and
   `after_value {"start_ms":0,"end_ms":2950,"speaker":"Mark"}`. One timeline snapshot
   records line 1 at 0..2950 and copies the source text, target text and take id forward.
2. A confirmed command `shift line 2 right by 250ms` wrote commit version 3. The action
   row reads `user_command`, `command_bar`, prompt `shift line 2 right by 250ms`, and
   `after_value {"start_ms":3750,"end_ms":8250,"speaker":"Suni"}`. One timeline snapshot
   records line 2 at 3750..8250.
3. The History tab listed three saved changes. It read `A line boundary was adjusted.`
   under `you` and `shift line 2 right by 250ms` under `editor command`.
4. After a reload the boundary panel read `0:00.000 to 0:02.950` for line 1 and
   `0:03.750 to 0:08.250` for line 2. The workspace payload served those bounds.
5. A failed write never reports success. A drag that overlapped line 3 answered 400 with
   `That boundary would overlap another line.` The page showed that sentence and the
   handle returned to `0:03.750 to 0:08.250`. With ClickHouse stopped, a drag answered
   500 with `Could not read the project ledger.` The page showed that sentence and the
   handle returned to `0:00.000 to 0:02.950`. The dub still held three commits, two
   actions and five snapshots.
6. `go test -count=1 ./internal/api ./cmd/...` reports `ok` for both packages.
   `npm --prefix web run check` reports 183 files, 0 errors and 0 warnings.

`internal/api/mutations_test.go` covers the boundary row, the command row, an invalid
command, an overlapping boundary, a failed recorder, and a project with no timeline.

### T6.6a, T6.6b and T6.7a filed 2026-09-09

A grounding review filed three tasks after the P6 close. T6.6a gives the T6.6 agent a route,
because nothing in the server constructs it. T6.6b reaches that route from the command bar.
T6.7a makes the fixture workspace read-only, which the P6 close round 3 notes had recorded.
No code changed with this filing.

### Wave handoff 2026-09-09

T6.6a implementation exists in the working tree. The implementer reported passing offline route integration and startup gating checks.
The workspace spend cap stopped the worker before its final handoff. No fresh review has run.
The accepted contract adds `internal/agent/route_test.go` for offline integration without importing ADK outside that package.
See `adversarial-review/t6.6a-contract.md`.

T6.7a implementation exists in the workspace page. Browser measurements covered all fixture aliases and observed zero POST requests after editing attempts.
A mocked real-project response preserved the boundary request path. A fresh reviewer must still verify a real ledger commit.

The spend cap blocks fresh reviews for both tasks. Neither task has landed.
T6.6b remains unstarted until T6.6a, T5.5a and T6.7a have approved commits.
Resume the required review loops with fresh agents.

### T6.7a validation constraint 2026-09-09

The user declined pulling or running a local ClickHouse container. Keep the real-ledger acceptance check blocked.
Automatic approval review rejected the reviewer image pull under the Endor Labs package and container approval policy.
No existing local ClickHouse runtime was found. Browser and isolated checks may continue.
T6.7a cannot land until the required ledger check completes. T6.6b remains blocked by the workspace ownership gate.

### T6.6a landed 2026-09-09

The server exposes `POST /api/dubs/{id}/agent` with an answer, native charges and their nanodollar total.
Startup constructs the agent only with MCP settings. Missing settings leave the workspace available and answer agent requests with 503.
The route never writes to the ledger. Wire examples and TypeScript mirror the native charge fields.
Fresh review `adversarial-review/t6.6a-round1.md` approves with zero findings at every severity.
An independent real-agent HTTP probe measured two model calls, one MCP read and zero writer calls.
Its token arithmetic reconciled to 16650 nanodollars. Startup probes and scoped Go and Node 26 checks passed.
