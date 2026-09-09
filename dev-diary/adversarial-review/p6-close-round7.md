# P6 close: independent verification, round 7

## Scope and role

**Scope:** All 14 Phase 6 tasks at HEAD `b31a502` plus the uncommitted round 6
remediation in the working tree. I measured a full copy at `/tmp/p6r7`.

**Role:** REVIEW. I fixed nothing. I edited no code, test, phase document, status line
or exit box. I committed nothing. My only write is this record.

## Method

I read `AGENTS.md`, `dev-diary/README.md`, `dev-diary/PHASE-6-editing.md`,
`dev-diary/project.md` and `dev-diary/ui-ux.md` in full. I read every prior close
record, both close remediations, the round 6 remediation, and every T6.6c round and
remediation. I treated each remediation table as a claim under test, never as evidence.

I copied the checkout to `/tmp/p6r7` and measured inside the copy. I loaded a fresh
ClickHouse 26.8.2.7 database from `sql/schema.sql` on ports 29001 and 29002. I built the
production frontend. I mounted the real `api.NewServer` with the real ledger reader, the
real workspace reader, the real run recorder, the real edit recorder and the real agent
charge recorder. A fake line renderer and a fake editor agent stood in for the live Gemini
and MCP services, which do not run here. Every charge, take, action and commit below landed
through the real writer path and was read back by SQL.

I drove headless Chrome over the DevTools protocol. I read the DOM, the network request
bodies and `pageerror`. I seeded three dubs. `p6r7two` holds `ml-IN` line 1 at `0..4000`
and `ta-IN` line 1 at `0..2000`. `p6r7one` holds one language. `p6r7notake` holds a `ta-IN`
line 2 with no take. Charge rows came from SQL through the `charges` view, which applies
FINAL. I treat no program log as evidence.

## Verdict

**Verdict:** REMEDIATE. Findings: 0 C, 0 H, 1 M, 0 L.

| C | H | M | L |
|---|---|---|---|
| 0 | 0 | 1 | 0 |

I differ from every prior P6 close reviewer and from every T6.6c reviewer.

## Findings

| Where | What | Pin | Mutation |
|---|---|---|---|
| `internal/ledger/takes.go` `TakeAttempt.rows` (`Text: a.Segment.Text`), `internal/ledger/workspace.go` `buildLanguageTracks` (`line.Text = row.Text`), `web/src/lib/edit/Text.svelte` (`targetDraft = $state(line?.text ?? "")`) | `api.Line.Text` is documented as the target-language line, and `testdata/wire/line.json` and `dub.json` carry the target. The take writer stores the source transcript in `takes_raw.text`, so `Line.Text` is the source line for every real project. The Text panel's target field therefore shows the source line after a reload. A corrected target lands in the timeline snapshot and the new take, but the field reverts. The fixture carries the target text, so it hides the defect. | On `p6r7two` `ta-IN` line 1 a target correction answered 201. SQL read `timeline_state_raw.text = 'r7 rendered line'` at version 6. After a reload `GET /api/dubs/p6r7two` returned `languages[ta-IN].lines[segment_id=1].text = 'Hi, I am Suni Williams'`, which is the source line. The target textarea read the same string as the source textarea. On `/d/fixture` the same field reads the Malayalam target from `testdata/wire/dub.json`. | A fix that copies `api.TimelineEntry.Text` into `Line.Text` shows the corrected target after a reload. Reverting it restores the source line in the target field. |

## Exit criteria

| Criterion | Independent result |
|---|---|
| Boundaries, speakers, and text support manual user editing | Met. A real pointer drag on `ta-IN` line 1 posted `{"kind":"boundary","language":"ta-IN","segment_id":1,"start_ms":0,"end_ms":1500}` and the handle survived a reload. The Speaker confirm showed `$0.0003` before any POST, then posted `{"language":"ta-IN","speaker":"Mark Vande Hei"}`. The Text confirm showed `$0.00063` before any POST, then posted `{"language":"ta-IN","text":"ta corrected target line."}`. `/d/fixture` rendered four disabled fieldsets and four read-only sentences. Finding 1 affects the target field display after a reload. |
| Billable operations display itemized prices before execution | Met. The Speaker confirm read `$0.0003` from the seeded `ta-IN` take with zero POSTs. An outside sum of that take's charge is 300000 nanodollars. The Text confirm read `$0.00063` with zero POSTs. A parser 422 showed the agent rate card `$0.00000015` and `$0.00000060` before the ask, then the measured `$0.000384` after the turn. |
| Command bar parses instructions into validated deterministic mutations | Met. `move wav 1 to 0:0005 to right.` returned 1000 to 5000. `shift line 3 right by 200ms.` returned 10200 to 13200. `change speaker for line 2 to Mark.` resolved `Mark Vande Hei`. `shorten line 3 by 0.5s.` returned 10000 to 12500. An unknown line, an unknown speaker, unknown grammar and a zero duration each answered 422 with a plain sentence. |
| Timeline edits preserve prior takes without destructive overwrites | Met. After two re-renders the `takes` view held three `ta-IN` line 1 rows and the seeded row kept its id and speaker. Each re-render appended a row. The harness renderer writes no audio file, so the file-level no-overwrite rests on the committed `nextTakeNumber` tests and the prior rounds. |
| All mutations write author-attributed commits to ClickHouse | Met. SQL read `boundary_nudged` by `manual_ui` under `ml-IN` and `ta-IN`, `user_command` by `command_bar` with the instruction verbatim, and two `take_rendered` rows by `agent` under `ta-IN`. Every row names the language of the track it changed. |
| The editor agent reads the ledger through mcp-clickhouse and writes nothing | Met. `go list -deps ./internal/agent` names only `internal/agent`, `internal/config` and `internal/cost` among repository packages. The filtered tool list names `list_databases`, `list_tables` and `run_query`. The `mcp_readonly` SELECT-only grant answered a SELECT and refused an INSERT with ClickHouse code 497. |

## T6.6c at the close bar

| Condition | Independent result |
|---|---|
| An agent turn writes measured charges read back by SQL | PASS. Two identical POSTs to `/api/dubs/p6r7two/agent` wrote four rows. Each row carried kind `agent`, an empty `take_id`, an empty `commit_id`, `segment_index` -1, and units 1200 at price 0.00000015 or units 340 at price 0.0000006. |
| The running total grows by the turn nanodollars | PASS. SQL read 3390000 before the turns and 4158000 after, a rise of 768000 for two turns. Each response reported 384000. |
| The covers sentence names the agent calls | PASS. The payload read `0 segment calls, 0 translation calls, 6 render calls, and 4 agent calls.` |
| Two identical turns record two charges | PASS. Two identical POSTs produced four rows, two distinct `turn_id` values and four distinct `event_key` values. |
| The Details tab renders with the agent charges present | PASS. Headless Chrome opened the Details tab on `p6r7two` and read `Editor agent turn $0.000768` plus the covers sentence. `pageerror` stayed empty. |
| The agent package imports no writer | PASS. `go list -deps ./internal/agent` names no ledger package and no api package. |
| The mcp_readonly grant refuses an INSERT | PASS. `SHOW GRANTS FOR mcp_readonly` read `GRANT SELECT ON p6close_r7.*`. A SELECT answered 15. An INSERT of kind `agent` failed with code 497 `ACCESS_DENIED`. |
| Journal before flush | PASS. With ClickHouse stopped one POST answered 500 and left two queue files of 1262 bytes. Restarting the container drained the queue and SQL gained the two rows. |
| A fresh database accepts kind `agent` | PASS. `system.columns` read `Enum8('segment' = 1, 'translate' = 2, 'synthesize' = 3, 'agent' = 4)`. |
| The live fold statement runs against a real ClickHouse | PASS. `TestWholePassChargesLiveFoldsAgentRows` printed `units=2 total=768000 segment_rows=1` and exited 0. |

## Residue against every prior round

| Prior round | Finding | Residue |
|---|---|---|
| `p6-close-round1.md` | H1, boundary and command edits wrote no commit | Zero. A real pointer drag and a confirmed command each wrote one child commit, one action and one snapshot. SQL read `boundary_nudged` by `manual_ui` and `user_command` by `command_bar`. Both survived a reload. |
| `p6-close-round2.md` | M1, a confirmed overlap could never save | Zero. A silent overlap on `p6r7one` answered 400 and wrote nothing. The same bounds with `allow_overlap` answered 201 and stored the overlap. |
| `p6-close-round3.md` | APPROVE, no findings | Zero residue stands. I re-measured the round 1 and round 2 residues above. |
| `p6-close-round4.md` | M1, a speaker-only command on a confirmed overlap answered 422 | Zero. Against a stored overlap `0..6000` and `5000..9000`, a speaker-only preview answered 200, an overlap-growing shift answered 422 `line 1 would overlap line 2`, and an overlap-shrinking shorten answered 200. |
| `p6-close-round4.md` | M2, no durable writer recorded agent charges | Zero. T6.6c landed. Two agent turns wrote four rows, the running total grew by 768000, and the covers sentence named the calls. |
| `p6-close-round5.md` | APPROVE, no findings | Zero residue stands. I re-measured both round 4 findings above. |
| `p6-close-remediation-round2.md` | The `allow_overlap` claim | Zero. A silent overlap failed and a confirmed overlap saved. |
| `p6-close-remediation-round4.md` | The per-line overlap claim and the T6.6c filing | Zero. The speaker-only, growing and shrinking previews behave as claimed. T6.6c exists and its done conditions hold. |
| `p6-close-round6.md` | H1, the editor read one language track and wrote another | Zero. The payload carries per-track `segments`, and the handle, the command preview, the overlap check, the length bars and the timeline all follow the active language. A drag, a speaker change, a text correction and a confirmed command each wrote the active language and survived a reload. Switching twice and editing first then second kept both tracks independent. The mutation that makes every track share `tracks[0]` fails `TestWorkspaceRouteCarriesEachLanguageTimeline`. |
| `p6-close-remediation-round6.md` | The per-track timeline claim | Zero. The claim holds at the wire, the page and the ledger. |
| `t6.6c-round1.md` | C1, project charge key and folded row | Zero. The Details tab rendered the folded `Editor agent turn` row of units 2 and total 768000 with no exception. |
| `t6.6c-round1.md` | H1, journal before flush | Zero. The stopped-container run left two queue files and the restart drained them. |
| `t6.6c-round1.md` | H2, empty commit sentinel | Zero. Every agent row carried an empty `commit_id`. |
| `t6.6c-round1.md` | M1, the whole-pass comment | Zero. The comment states the writer attributes segmentation to the first rendered take and that agent calls own no take. |
| `t6.6c-round1.md` | M2, the running total call identity | Zero. The statement counts all four kinds by call identity, and two turns read as four agent calls. |
| `t6.6c-round1.md` | L1, the attempt rule | Zero. `sql/schema.sql` lines 41 and 764 both state that a genuine repeated take call must raise `attempt`. |
| `t6.6c-round2.md` | H, the alias shadow in `selectWholePassCharges` | Zero. The live fold returned units 2 and total 768000, which equals the agent rows alone. The folded row excluded 3390000 nanodollars of synthesize rows. |
| `t6.6c-round2.md` | C, the Details tab take charge keys | Zero. The Details tab rendered with no exception. |
| `t6.6c-round2.md` | L, the tests | Zero. `internal/ledger/workspace_test.go` requires the substring `charges.kind = 'agent'`. |
| `t6.6c-round3.md` | H, four duplicate take keys | Zero. `p6r7two` `ta-IN` line 1 held three takes on two files, one file twice. The page rendered the rail, the take chips and the Details tab with no exception. |
| `t6.6c-round3.md` | L, the stale comment | Zero. The comment above `selectWholePassCharges` names kind, units, unit price, total and position. |
| `t6.6c-round4.md` | L, the whole-pass order comment | Zero. The comment reads ordered by commit, kind, then unit. The shipped statement reads `ORDER BY commit_id ASC, kind ASC, unit ASC`. |
| `t6.6c-remediation-round1.md`, `-round2.md`, `-round3.md` | The three remediation tables | Zero. Every row is re-measured above. |

## Additional checks

`python3 tools/audit_docs.py` ran before the measurements and exited 0 with
`No drift. Docs and repository agree.`

The phase file holds 14 task blocks, all `status: done`. All 91 `owns` entries exist on
disk. The six exit boxes are unchecked. The README status board reads `P6 | 14 / 14`. The
README current status and follow-up note name T6.6c and the pending P6 close. No drift
appeared.

The wire contract change holds. `api.LanguageTrack` carries `Segments []Segment` with tag
`segments`, and `Dub.Segments` keeps its tag. `testdata/wire/dub.json` and
`language_track.json` both carry the key, because it is not `omitempty`.
`TestExamplesMatchJSONTags`, `TestEverySharedStructHasExample`,
`TestEveryWireStructIsRegistered` and `TestGoTSParity` each exited 0. `web/src/lib/types.ts`
mirrors both fields at lines 94 and 136. No other shared struct changed.

Svelte 5 only. `grep` over `web/src` found no `export let`, no `$:`, no store call and no
`writable(` or `readable(`. `web/package.json` pins `svelte ^5.57.0` and
`node_modules/svelte/package.json` reads 5.57.0. The four keyed take lists and both charge
lists append the array position.

`go build ./...` and `go vet ./...` exited 0. `go test -count=1 ./internal/... ./cmd/...`
exited 0 across 14 packages. `go vet -tags live ./internal/ledger ./internal/agent` exited
0. `npm --prefix web run check` read 183 files with 0 errors and 0 warnings.

The take audio route answered a missing file and five traversal shapes with 404, and a
language escape with 404.

Notes, not findings. The timeline passes the active language's segments to every lane, so a
non-active lane draws its takes against the active language's slots. The active language
does drive the timeline as the round 6 task requires. The two re-renders record
`take_rendered` with the `agent` author rather than a `manual_ui` action, which prior
rounds recorded. The harness renderer writes no audio, so it reuses a take file name and
exercises the duplicate-file key path rather than the file-number path.

## Evidence paths

- Copy `/tmp/p6r7`, deleted after the review.
- ClickHouse container `p6r7-ch` on `127.0.0.1:29001` and `127.0.0.1:29002`, database
  `p6close_r7`, removed. The live fold test used database `p6r7live`.
- Harness `cmd/ajilamu/zz_p6r7_harness_test.go` in package `main` behind the `r7harness`
  tag, deleted with the copy. Binary `/tmp/p6r7-harness.test`, deleted.
- Harness server `p6r7-harness` on port 29111, stopped. Data directory `/tmp/p6r7-data`,
  seed `/tmp/p6r7-seed.sql`.
- Headless Chrome over the managed browser daemon, tab `p6r7` released.
- Mutation backup `/tmp/p6r7-workspace.go.bak`, deleted.

## Cleanup statement

I stopped the harness server. I removed the ClickHouse container. I released the browser
tab. I deleted the copy, the harness binary, the mutation backup, the seed and the data
directory. No container, server or browser survives. The main checkout shows only this new
untracked record plus the two untracked round 6 records.
