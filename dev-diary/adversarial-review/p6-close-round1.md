# P6 close: independent verification, round 1

**Scope:** Phase 6 exit criteria for T6.1 to T6.6 plus T6.5a, T6.5b and T6.5c, measured at committed tree `440fdc7`.
**Method:** Independent measurement with outside tools. I did not treat per-task review files as evidence.
**Verdict:** REMEDIATE. Findings: 0 C, 1 H, 0 M, 0 L.

| C | H | M | L |
|---|---|---|---|
| 0 | 1 | 0 | 0 |

I own only this review file. I did not tick the phase exit boxes. I fixed nothing.

## Audit

`python3 tools/audit_docs.py` exits 0. It prints "No drift. Docs and repository agree." The audit ran first, before I wrote this review.

## Findings

| Where | What | Pin | Mutation |
|---|---|---|---|
| `web/src/routes/d/[id]/+page.svelte` lines 267 to 276 and 454 to 468 | `handleBoundaryChange` and `confirmCommand` write the browser `dub` rune alone. Neither sends a request. `NewCommandPreviewHandler()` takes no recorder and records nothing. The history tab never shows a `boundary_nudged` or `user_command` commit, and a reload discards the edit. The ledger already accepts `boundary_nudged` with `manual_ui` and `user_command` with `command_bar`. `HistoryTab.svelte` renders both sentences. The vocabulary exists and no writer uses it. | The route table in `internal/api/server.go` holds no boundary or command commit route. A live `POST /api/editor/commands/preview` of `move wav 1 to 0:0005 to right.` answered 200 with the Go-derived segment and wrote no commit. `grep` over `internal/` finds `ActionUserCommand` and `ActionBoundaryNudged` only in the constants, the ledger allow list and tests. `confirmCommand` carries no `fetch`. | A later fix adds a recorder and a commit route for these edits. Reverting that fix returns each edit to browser memory alone, and the history tab loses the row. |

## Exit criteria

I measured each criterion. I did not tick the boxes.

| Criterion | Independent result |
|---|---|
| Boundaries, speakers, and text support manual user editing | Met. `+page.svelte` mounts `Boundary`, `Speaker` and `Text` at lines 806 to 825. `Boundary.svelte` defaults snap 120 ms, minimum 100 ms and keyboard step 50 ms, and gates an overlap behind a confirm. `Text.svelte` carries `lang={sourceLanguage || undefined}` and `lang={language}`. `npm --prefix web run check` reports 183 files, 0 errors, 0 warnings. |
| Billable operations display itemized prices before execution | Met for the wired paths. The Speaker and Text confirm panels show a fee from the newest billed take before the call. The command-bar edits are not billable. The agent turn has no route and no price display. |
| Command bar parses instructions into validated deterministic mutations | Met. Live POSTs returned 200 with Go-derived values for the four documented commands. An unknown line, an unknown speaker, an out-of-bounds move and unknown grammar returned 422 with clear sentences. |
| Timeline edits preserve prior takes without destructive overwrites | Met. The served take measured 183096 bytes and decoded at 5.720375 s. A throwaway probe measured `nextTakeNumber` at 2, 3 and 4 over seeded files, so a new take never reuses a name. |
| All mutations write author-attributed commits to ClickHouse | NOT met. Boundary and command-bar edits write no commit. See H1. |
| The editor agent reads the ledger through mcp-clickhouse and writes nothing | Met at the library level. Only `internal/agent` and its test binary import `google.golang.org/adk/v2`. `NewToolset` filters to `list_databases`, `list_tables` and `run_query`. The grant parser accepts SELECT alone. `unshare -rn go test ./internal/agent` passes with no socket. No route constructs the agent, because no task owns an agent endpoint. |

## Task status and owns

All nine P6 task blocks read `status: done`. All 51 owns entries exist on disk. The 36 unique paths exist.

| Task | Status | Owns | Present |
|---|---|---:|---|
| T6.1 | done | 2 | yes |
| T6.2 | done | 2 | yes |
| T6.3 | done | 2 | yes |
| T6.4 | done | 7 | yes |
| T6.5a | done | 4 | yes |
| T6.5 | done | 3 | yes |
| T6.5c | done | 5 | yes |
| T6.5b | done | 8 | yes |
| T6.6 | done | 18 | yes |

## Prior-round residue

All nine final task reviews read APPROVE with zero findings. I re-measured their pins at the artifact level. I did not quote those files as proof.

**T6.1: zero residue.** `Boundary.svelte` defaults snap 120 ms, minimum 100 ms and keyboard step 50 ms. A keyboard move passes `shouldSnap=false`. A collision opens a confirm and does not commit. `+page.svelte` mounts the component.

**T6.2: zero residue.** `jq` on `testdata/wire/dub.json` sums the charges on the newest billed take of each line. Line 1 reads 842100 nanodollars from `seg_1_try1.wav`, line 3 reads 2318200 from `seg_3_try1.wav`, line 4 reads 3639200 from `seg_4_try1.wav`, and line 7 reads 4751000 from `seg_7_try1.wav`. Those values match the handoff fees. The toggle set is exactly the two fixture speakers. Cancel sends nothing.

**T6.3: zero residue.** `Text.svelte` edits both fields in place. The source textarea carries `lang={sourceLanguage || undefined}` and the target carries `lang={language}`. The target confirm sends `text`, and the source confirm sends `source_text`. `internal/fit/rewrite.go` sets `AuthoritativeText` and skips the translator on every attempt.

**T6.4: zero residue.** Live POSTs measured the four grammars and the rejects. `0:0005` parsed to 5000 ms, `0.5s` to 500 ms, and `Mark` resolved to `Mark Vande Hei`. An overlap, an unknown line, an unknown speaker and unknown grammar all returned 422.

**T6.5a: zero residue.** The route mounts at `POST /api/dubs/{id}/lines/{segment}/rerender`. A bad segment returned 400, a bad body returned 400, and a missing line returned 404. The throwaway probe measured `nextTakeNumber` at 2, 3 and 4, so a new take never overwrites one.

**T6.5: zero residue.** The page renders a take-history strip. `Track.svelte` renders `takes.slice(0, -1)` as ghosts and `takes.at(-1)` as active. `playTake` pauses the preview and the previous take. Both chips call `playTake`, which resolves a real project through `servedTakeURL`.

**T6.5b: zero residue.** `rerenderBody` carries `source_text`. A supplied source sets the segment text and clears the target text, so the loop translates. `AuthoritativeText` speaks the creator's text on every attempt. The source path records `text_corrected` with `manual_ui`.

**T6.5c: zero residue.** The live take route returned 200 with `audio/wav` and byte-identical bytes, 206 with the exact range slice, and 404 for six traversal shapes and a missing file. The `ml` short code resolved to the `ml-IN` work directory. `/d/fixture` still uses the bundled asset map.

**T6.6: zero residue.** Only `internal/agent` imports ADK. `unshare -rn go test ./internal/agent` passes with no socket. `requireSelectOnlyGrants` accepts SELECT alone and rejects INSERT, ALTER, DROP, CREATE, TRUNCATE, DELETE and SYSTEM. `deploy/mcp-clickhouse/compose.yaml` runs as `mcp_readonly` and sets `CLICKHOUSE_ALLOW_WRITE_ACCESS=false`. `MCPConfigured` requires the URL and the token.

## Notes, not findings

The take route serves any regular file in the work directory, not only takes. `notes.json` returned 200 `application/json`. The path stays inside the storage root, so this is not a traversal. The round 2 review recorded the same note.

The agent is not constructed by the server. `cmd/ajilamu/main.go` never calls `NewFromConfig`, and no route owns an agent endpoint. The task records this as deliberate.

`internal/ledger/takes.go` drops an `agent` charge silently, because its switch has no `ChargeAgent` case. The T6.6 handoff records this, and the agent never writes a take.

The target-text and speaker re-renders record `take_rendered` with the `agent` author. The `speaker_reassigned` action and the `manual_ui` author stay unwritten on those paths.

## What I measured

- `python3 tools/audit_docs.py` exit 0.
- `go build -o /tmp/p6close-ajilamu ./cmd/ajilamu` exit 0.
- The server ran on port 8799 with `AJILAMU_DATA_DIR=/tmp/p6close/data` and a real project `p6live` holding `uploads/p6live/work/ml-IN/seg_3_try1.wav`.
- `go test -count=1 ./internal/command ./internal/api ./internal/fit ./internal/agent ./internal/cost ./internal/config ./cmd/...` passed for all seven packages.
- `unshare -rn go test -count=1 ./internal/agent` passed. `go vet -tags live ./internal/agent` exit 0.
- `npm --prefix web run check` reported 183 files, 0 errors, 0 warnings.
- A throwaway probe lived at `/tmp/p6close/repo/internal/api/zz_p6probe_test.go`. It called the production `nextTakeNumber` over seeded take files. It measured 2, then 3, then 4.

I did not run a live Gemini or Cloud TTS call. The re-translation counts and the live take bytes in the T6.5b handoff rest on the committed tests. I did not run a browser, so the click and drag behavior rests on the committed components and the web check.
