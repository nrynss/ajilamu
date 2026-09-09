# P6 close: independent verification, round 8

## Scope and role

**Scope:** All 14 Phase 6 tasks at HEAD `b31a502` plus the uncommitted round 6 and round 7 remediations in the working tree. I measured a full copy at `/tmp/p6r8`.

**Role:** REVIEW. I fixed nothing. I edited no code, test, phase document, status line or exit box. I committed nothing. My only write is this record.

## Method

I read `AGENTS.md`, `dev-diary/README.md`, `dev-diary/PHASE-6-editing.md`, `dev-diary/project.md` and `dev-diary/ui-ux.md` in full. I read every P6 close round and remediation, and every T6.6c round and remediation. I treated each remediation table as a claim under test, never as evidence.

I copied the checkout to `/tmp/p6r8` and measured inside the copy. I loaded a fresh ClickHouse 26.8.2.7 database from `sql/schema.sql` on ports 29601 and 29602, database `p6close_r8`. I built the production frontend. I mounted the real `api.NewServer`. It held the real ledger client, history reader, workspace reader, run recorder, edit recorder and agent charge recorder. A fake line renderer and a fake editor agent stood in for the live Gemini and MCP services, which do not run here. Every charge, take, action and commit below landed through the real writer path and was read back by SQL.

I drove headless Chrome 152 over the DevTools protocol. I read the DOM, the network request bodies, `Runtime.exceptionThrown` and page errors. I seeded three dubs through the real writer. `p6r8two` holds `ml-IN` line 1 at `0..4000` and `ta-IN` line 1 at `0..2000`. `p6r8notake` holds a `ta-IN` line 2 with no take. Charge rows came from SQL through the `charges` view, which applies FINAL. I treat no program log as evidence.

## Verdict

**Verdict:** REMEDIATE. Findings: 0 C, 0 H, 1 M, 0 L.

| C | H | M | L |
|---|---|---|---|
| 0 | 0 | 1 | 0 |

I differ from every prior P6 close reviewer and from every T6.6c reviewer.

## Findings

| Where | What | Pin | Mutation |
|---|---|---|---|
| `web/src/routes/d/[id]/+page.svelte` `handleSpeakerChange` line 437 and `web/src/lib/edit/Speaker.svelte` `confirm` lines 106 to 110 | A failed speaker re-render stays applied on screen. The page patches the active segment before the call, and the panel sets `appliedSpeaker` before the call. Neither reverts on error. The panel then shows the new speaker beside an error sentence, while the ledger keeps the old one. A reload is the only correction. The Boundary control reverts on the same failure, which the T6.7 done condition requires. | With ClickHouse 26.8.2.7 stopped, the Speaker panel on `p6r8two` `ta-IN` line 1 showed `$0.00072` with zero POSTs, then confirmed `{"language":"ta-IN","speaker":"Mark Vande Hei"}`. The route answered 500. The panel read `data-speaker="Mark Vande Hei"` and `Could not read the project ledger.`. SQL `timeline_at_commit` read `Suni Williams`, and the take list stayed at one take. A boundary nudge on the same outage posted and reverted to `End 0:03.000`. After restart and reload the panel read `Suni Williams`. | Patch the segment and `appliedSpeaker` only after a successful reply, and restore the stored speaker on error. Reverting that fix restores the stale panel. |

## Exit criteria

| Criterion | Independent result |
|---|---|
| Boundaries, speakers, and text support manual user editing | Met. A keyboard nudge wrote `{"kind":"boundary","language":"ml-IN","segment_id":1,"start_ms":0,"end_ms":3950}` and survived a reload. A speaker confirm wrote `seg_1_try3.wav` and survived a reload. A target correction wrote `seg_1_try2.wav` and a source correction wrote `seg_2_try2.wav`, and both survived a reload. `/d/fixture` rendered four disabled fieldsets, four read-only sentences and zero non-GET requests. Finding 1 affects only the display after a failed speaker change. |
| Billable operations display itemized prices before execution | Met. The Speaker confirm showed `$0.00114` with zero POSTs before it. An outside sum is 38 target runes at `$0.00003`, which equals 1140000 nanodollars. The Text confirm showed `$0.00066`, which equals 22 runes at the same price. A parser 422 showed the agent rates `$0.00000015` and `$0.00000060` before the ask, then the measured `$0.000384` after it. `internal/cost/cost.go` reads 150 and 600 nanodollars. |
| Command bar parses instructions into validated deterministic mutations | Met. `move wav 1 to 0:0005 to right.` returned 2000 to 5000. `shift line 3 right by 200ms.` returned 10200 to 13200. `change speaker for line 7 to Mark.` resolved `Mark Vande Hei`. `shorten line 4 by 0.5s.` returned 14000 to 16000. An unknown line, an unknown speaker, unknown grammar, a zero duration and a colliding move each answered 422 with a plain sentence. |
| Timeline edits preserve prior takes without destructive overwrites | Met. After one boundary nudge and three re-renders the `takes` view held three `ml-IN` line 1 rows with three distinct take ids. The seeded take kept its id and speaker. The served take decoded as wav at 1.680375 seconds, and a range request answered 206 with `Content-Range: bytes 1000-2999/53816`. |
| All mutations write author-attributed commits to ClickHouse | Met. SQL read `boundary_nudged` by `manual_ui`, `user_command` by `command_bar` with the instruction verbatim, `text_corrected` by `manual_ui`, and `take_rendered` by `agent`. Every row named the language of the track it changed. |
| The editor agent reads the ledger through mcp-clickhouse and writes nothing | Met. `go list -deps ./internal/agent` names only `internal/agent`, `internal/config` and `internal/cost` among repository packages. `ReadTools` names `list_databases`, `list_tables` and `run_query`. The `mcp_readonly` SELECT-only grant answered a SELECT and refused an INSERT with ClickHouse code 497. A server with no agent answered the agent route 503 and served the workspace 200. |

## T6.6c at the close bar

| Condition | Independent result |
|---|---|
| A fresh database accepts kind `agent` | PASS. `sql/schema.sql` loaded on an empty database and the preflight returned 0. `system.columns` read `Enum8('segment' = 1, 'translate' = 2, 'synthesize' = 3, 'agent' = 4)`. |
| An agent turn writes measured charges read back by SQL | PASS. Two identical POSTs to `/api/dubs/p6r8two/agent` wrote four rows. Each row carried kind `agent`, an empty `take_id`, an empty `commit_id`, `segment_index` -1, and units 1200 at price 0.00000015 or units 340 at price 0.0000006. |
| The running total grows by the turn nanodollars | PASS. SQL read 5460000 before the turns and 6228000 after, a rise of 768000 for two turns. Each response reported 384000. |
| The covers sentence names the agent calls | PASS. The payload read `0 segment calls, 0 translation calls, 6 render calls, and 2 agent calls.` |
| Two identical turns record two charges | PASS. Two identical POSTs produced four rows, two distinct `turn_id` values and four distinct `event_key` values. |
| The Details tab renders with the agent charges present | PASS. Headless Chrome opened the Details tab on `p6r8two` and read `Editor agent turn$0.000768` plus the covers sentence. The page raised no error. |
| The agent package imports no writer | PASS. `go list -deps ./internal/agent` names no ledger package and no api package. |
| The mcp_readonly grant refuses an INSERT | PASS. `SHOW GRANTS FOR mcp_readonly` read `GRANT SELECT ON p6close_r8.*`. A SELECT answered 10. An INSERT of kind `agent` failed with code 497. |
| Journal before flush | PASS. With ClickHouse stopped one POST answered 500 and left two queue files of 1262 bytes. Restarting the container drained the queue and SQL gained the rows. |
| The live fold statement runs against a real ClickHouse | PASS. `TestWholePassChargesLiveFoldsAgentRows` printed `units=2 total=768000 segment_rows=1` and exited 0. |

## Round 7 M re-derivation

The take writer stores the spoken target line. `takes_raw.text` for `p6r8two` `ml-IN` line 1 read the Malayalam target, not the English source. `take_rates` divided the target length by measured milliseconds. The seeded take read 22 characters over 4000 milliseconds, which is 5.5 characters per second. An outside Python count of the same string read 22.

`api.Line.Text` equals that value. The workspace payload served the corrected target after a correction, and the Text panel's target field showed it. The source field kept `I am an astronaut.` after the same correction. The correction survived a reload.

The defeat attempts failed. A blank target text made `RecordTake` reject the row, and the route answered 500 with no take and no commit. A speaker re-render after a correction recorded `seg_1_try3.wav` with the corrected target. A boundary nudge after a correction wrote `0..3950` and copied the corrected target into the new timeline snapshot.

## Residue against every prior round

| Prior round | Finding | Residue |
|---|---|---|
| `p6-close-round1.md` | H1, boundary and command edits wrote no commit | Zero. A keyboard nudge and a confirmed command each wrote one commit, one action and one snapshot. SQL read `boundary_nudged` by `manual_ui` and `user_command` by `command_bar`. Both survived a reload. |
| `p6-close-round2.md` | M1, a confirmed overlap could never save | Zero. A silent overlap on `p6r8notake` answered 400 and wrote nothing. The same bounds with `allow_overlap` answered 201 and stored line 1 at `0..6000` against line 2 at `5000..9000`. |
| `p6-close-round3.md` | APPROVE, no findings | Zero residue stands. I re-measured the round 1 and round 2 residues above. |
| `p6-close-round4.md` | M1, a speaker-only command on a confirmed overlap answered 422 | Zero. Against the stored overlap a speaker-only preview answered 200, an overlap-growing shift answered 422 `line 1 would overlap line 2`, and an overlap-shrinking shorten answered 200. A growing shift on line 2 answered 422. |
| `p6-close-round4.md` | M2, no durable writer recorded agent charges | Zero. T6.6c landed. Two agent turns wrote four rows, the running total grew by 768000, and the covers sentence named the calls. |
| `p6-close-round5.md` | APPROVE, no findings | Zero residue stands. I re-measured both round 4 findings above. |
| `p6-close-remediation-round2.md` | The `allow_overlap` claim | Zero. A silent overlap failed and a confirmed overlap saved. |
| `p6-close-remediation-round4.md` | The per-line overlap claim and the T6.6c filing | Zero. The speaker-only, growing and shrinking previews behave as claimed. T6.6c exists and its done conditions hold. |
| `p6-close-round6.md` | H1, the editor read one language track and wrote another | Zero. The payload carries per-track `segments`. The handle, the command preview, the overlap check, the length bars and the timeline all follow the active language. A command confirmed on `ta-IN` posted the `ta-IN` timeline and saved `1500..3000`, which equals the preview. A reload kept `ta-IN` at `1500..3000` and `ml-IN` at `0..3950`. The mutation that reads `tracks[0].Language` for every track fails `TestWorkspaceRouteCarriesEachLanguageTimeline`. |
| `p6-close-remediation-round6.md` | The per-track timeline claim | Zero. The claim holds at the wire, the page and the ledger. |
| `p6-close-round7.md` | M1, `takes_raw.text` held the source line | Zero. The writer stores the target line, `api.Line.Text` equals it, and the panel shows it after a reload. The mutation that restores `Text: a.Segment.Text` fails `TestRecordTakeFixtureCapture` with `segment 1 text = Hi, I'm Suni Williams, want the spoken target line`. |
| `p6-close-remediation-round7.md` | The target-text claim | Zero. Every row is re-measured above. |
| `t6.6c-round1.md` | C1, project charge key and folded row | Zero. The Details tab rendered the folded `Editor agent turn` row of units 2 and total 768000 with no exception. |
| `t6.6c-round1.md` | H1, journal before flush | Zero. The stopped-container run left two queue files and the restart drained them. |
| `t6.6c-round1.md` | H2, empty commit sentinel | Zero. Every agent row carried an empty `commit_id`. |
| `t6.6c-round1.md` | M1, the whole-pass comment | Zero. The comment states the writer attributes segmentation to the first rendered take and that agent calls own no take. |
| `t6.6c-round1.md` | M2, the running total call identity | Zero. `p6r8notake` held one translate call with two unit rows, and the covers sentence read `1 translation call`. Two agent turns read as two agent calls. |
| `t6.6c-round1.md` | L1, the attempt rule | Zero. `sql/schema.sql` lines 41 and 764 both state that a genuine repeated take call must raise `attempt`. |
| `t6.6c-round2.md` | H, the alias shadow in `selectWholePassCharges` | Zero. The live fold returned units 2 and total 768000, which equals the agent rows alone. Independent SQL on `p6r8two` read two agent rows of 768000 and no segment row in the fold. |
| `t6.6c-round2.md` | C, the Details tab take charge keys | Zero. One take with two translate rows rendered both rows with no exception. |
| `t6.6c-round2.md` | L, the tests | Zero. `internal/ledger/workspace_test.go` line 427 requires the substring `charges.kind = 'agent'`. |
| `t6.6c-round3.md` | H, four duplicate take keys | Zero. `p6r8notake` `ta-IN` line 1 held two takes on one `audio_path`. The page rendered the rail, two take chips, two ghosts, two speaker takes and the Details tab with no exception. |
| `t6.6c-round3.md` | L, the stale comment | Zero. The comment above `selectWholePassCharges` names kind, units, unit price, total and position. |
| `t6.6c-round4.md` | L, the whole-pass order comment | Zero. The comment reads ordered by commit, kind, then unit. The shipped statement reads `ORDER BY commit_id ASC, kind ASC, unit ASC`. |
| `t6.6c-remediation-round1.md`, `-round2.md`, `-round3.md` | The three remediation tables | Zero. Every row is re-measured above. |

## Additional checks

`python3 tools/audit_docs.py` ran before I wrote this record and exited 0 with `No drift. Docs and repository agree.`

The phase file holds 14 task blocks, all `status: done`. All 91 `owns` entries exist on disk. The six exit boxes are unchecked. The README status board reads `P6 | 14 / 14`. The README current status names T6.6c and the pending P6 close. No drift appeared.

The wire contract change holds. `api.LanguageTrack` carries `Segments []Segment` with tag `segments`, and `Dub.Segments` keeps its tag. `testdata/wire/dub.json` and `language_track.json` both carry the key. `TestExamplesMatchJSONTags`, `TestEverySharedStructHasExample`, `TestEveryWireStructIsRegistered` and `TestGoTSParity` each exited 0. `web/src/lib/types.ts` mirrors both fields. No other shared struct changed.

Svelte 5 only. `grep` over `web/src` found no `export let`, no `$:` and no store call. `web/package.json` pins `svelte ^5.57.0`. The four keyed take lists and both charge lists append the array position.

`go build ./...` and `go vet ./...` exited 0. `go test -count=1 ./internal/... ./cmd/...` exited 0 across 14 packages. `npm --prefix web run check` read 183 files with 0 errors and 0 warnings.

Notes, not findings. The timeline passes the active language's segments to every lane, so a non-active lane draws its takes against the active language's slots. The active language does drive the timeline as the round 6 task requires, and the UI specification asks columns to align across tracks. A project whose recorded language is a short code such as `ml` records a re-render under the catalog tag `ml-IN`, which splits one language into two tracks. The product UI sends catalog tags, so only an API client reaches that state. A failed re-render writes the commit and action before a later take write fails, so a mid-write failure can leave a commit with no timeline snapshot. The blank-text trigger for that path is not reachable in production.

## Evidence paths

- Copy `/tmp/p6r8`, deleted after the review.
- ClickHouse container `p6r8-ch` on `127.0.0.1:29601` and `127.0.0.1:29602`, database `p6close_r8`, removed. The live fold test used database `p6r8live`.
- Harness `cmd/ajilamu/zz_p6r8_harness_test.go` in package `main` behind the `r8harness` tag, deleted with the copy. Server `p6r8-harness` on port 29611, stopped. Empty-text server on port 29612, stopped. No-agent server on port 29613, stopped.
- Data directories `/tmp/p6r8-data`, `/tmp/p6r8-data2` and `/tmp/p6r8-data3`, deleted.
- Headless Chrome over the managed browser daemon, tab `p6r8` released.
- Mutation backups `/tmp/p6r8-takes.go.bak` and `/tmp/p6r8-workspace.go.bak`, deleted.

## Cleanup statement

I stopped every harness server. I removed the ClickHouse container. I released the browser tab. I deleted the copy and both mutation backups. No container, server or browser survives. The main checkout shows this new untracked record plus the untracked records of other agents.
