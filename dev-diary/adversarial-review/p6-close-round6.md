# P6 close: independent verification, round 6

## Scope and role

**Scope:** All 14 Phase 6 tasks at HEAD `b31a502`, measured in a full copy at `/tmp/p6close-r6`.
**Role:** REVIEW. I fixed nothing. I edited no code, test, phase document, status line or exit box.
I committed nothing. My only write is this record.

## Verdict
**Verdict:** REMEDIATE. Findings: 0 C, 1 H, 0 M, 0 L.

| C | H | M | L |
|---|---|---|---|
| 0 | 1 | 0 | 0 |

I differ from every prior P6 close reviewer and from every T6.6c reviewer.

## Method

I read `AGENTS.md`, `dev-diary/README.md`, `dev-diary/PHASE-6-editing.md`, `dev-diary/project.md`
and `dev-diary/ui-ux.md` in full. I read every prior close record and every T6.6c round and
remediation. I treated each remediation table as a claim under test, never as evidence.

I copied the checkout to `/tmp/p6close-r6` and measured inside the copy. I loaded a fresh
ClickHouse 26.8.2.7 database from `sql/schema.sql` on ports 28001 and 28002. I built the real
`ajilamu` binary and the production frontend. I ran the real ledger client, the real history
reader, the real workspace reader, the real edit recorder, the real run recorder and the real
agent charge recorder behind `api.NewServer`. A fake editor agent and a fake line renderer stood
in for the live Gemini and MCP services, because no MCP server runs here. Every charge, take,
action and commit below landed through the real writer path and was read back by SQL.

I drove headless Chrome over the DevTools protocol. I read the DOM, the network requests and
`Runtime.exceptionThrown`. Charge rows came from SQL through the `charges` view, which applies
FINAL. I treat no program log as evidence.

## Findings

| Where | What | Pin | Mutation |
|---|---|---|---|
| `web/src/routes/d/[id]/+page.svelte` lines 303 to 322, 558 to 571 and 643 to 648, with `internal/api/workspace.go` `workspaceSegments` | The boundary editor and the command bar derive the candidate timeline from `dub.segments`. `workspaceSegments` reads `dub.segments` from `languages[0]`, the first target language. Both write under `activeLanguage`. On a dub with two target languages the editor displays and validates one track and writes another. `applyEditedSegment` updates only `dub.segments`, so the saved edit vanishes on the next reload. | Dub `p6r6live` held `ml-IN` line 1 at `0..4000` and `ta-IN` line 1 at `0..2000`. With Tamil active the end handle read `End 0:04.000`, which is the `ml-IN` bound. A real pointer drag posted `{"kind":"boundary","language":"ta-IN","segment_id":1,"start_ms":0,"end_ms":3333}`. SQL read a new `ta-IN` snapshot at `0..3333` while the `ta-IN` take still carried `slot_ms` 2000. A preview of `shorten line 1 by 1s.` posted the `ml-IN` timeline `0..4000` and showed `Shorten line 1 by 1.000s.`. The confirmation posted `language:"ta-IN"`. SQL read `ta-IN` line 1 at `0..2333`, not the previewed `0..3000`. | A fix must make the displayed track and the written track agree. It must keep the previewed bounds equal to the saved bounds on every language. Reverting it must restore the mismatch. |

## Exit criteria

| Criterion | Independent result |
|---|---|
| Boundaries, speakers, and text support manual user editing | Not fully met. A real pointer drag on the first language moved line 1 from `0..5050` to `0..4000`, posted one edit, and survived a reload. The Speaker panel showed `$0.000648` before any request. The Text panel showed `$0.000648` before any request. A corrected source answered 201 and wrote a `text_corrected` action. A corrected target answered 201 and wrote a new take. The fixture page rendered four disabled fieldsets and sent zero non-GET requests. Finding 1 breaks boundary and command editing on a non-first language. |
| Billable operations display itemized prices before execution | Met. The Speaker confirm read `$0.000648` from `seg_1_try1.wav` with zero POSTs. An outside sum of that take's charges is 648000 nanodollars. The Text confirm read `$0.000648` with zero POSTs. The command bar showed the agent rate card `$0.00000015` and `$0.00000060` before the ask, then the measured `$0.000384` after the turn. |
| Command bar parses instructions into validated deterministic mutations | Met for parsing and validation. `shift line 3 right by 200ms.` returned 9200 to 12200. `shorten line 3 by 0.5s.` returned 9000 to 11500. `change speaker for line 2 to Mark.` resolved `Mark Vande Hei`. `move line 3 to 0:0013 to right.` returned 10000 to 13000. An unknown line, an unknown speaker, unknown grammar, a zero duration and a colliding move each answered 422. Finding 1 shows the confirmed preview can differ from the saved mutation on a non-first language. |
| Timeline edits preserve prior takes without destructive overwrites | Met. After four edits and three re-renders the `takes` view held six rows with six distinct take ids. The seeded files kept their sha256 `446bac62...` and `8928864c...`. Each re-render wrote a new take beside the old ones. The History tab listed nine saved commits. |
| All mutations write author-attributed commits to ClickHouse | Met. The actions view held four `boundary_nudged` rows and one `text_corrected` row by `manual_ui`, one `user_command` row by `command_bar` with the instruction verbatim, and two `take_rendered` rows by `agent`. The History tab rendered the same three authors as `you`, `editor command` and `Ajilamu`. |
| The editor agent reads the ledger through mcp-clickhouse and writes nothing | Met. Only `internal/agent` and its tests import `google.golang.org/adk/v2`. `ReadTools` names `list_databases`, `list_tables` and `run_query`. `go list -deps ./internal/agent` names no ledger package. The `mcp_readonly` SELECT-only grant answered a SELECT and refused an INSERT with ClickHouse code 497. `unshare -rn` with loopback up passed `go test ./internal/agent`. The real binary with no MCP settings answered the agent route 503 and served the workspace 200. |

## T6.6c at the close bar

| Condition | Independent result |
|---|---|
| An agent turn writes measured charges read back by SQL | PASS. Two identical POSTs to `/api/dubs/p6r6live/agent` wrote four rows. Each row carried kind `agent`, an empty `take_id`, an empty `commit_id`, `segment_index` -1, and units 1200 at price 150 or 340 at price 600. |
| The running total grows by the turn nanodollars | PASS. SQL read 1263000 before the turns and 2031000 after, a rise of 768000 for two turns. Each response reported 384000. |
| The covers sentence names the agent calls | PASS. The payload read `1 segment call, 1 translation call, 3 render calls, and 2 agent calls.` after the two turns. The final payload read `and 4 agent calls.` |
| Two identical turns record two charges | PASS. Two identical POSTs produced four rows, two distinct `turn_id` values and four distinct `event_key` values. |
| The Details tab renders with the agent charges present | PASS. Headless Chrome opened the Details tab on line 1 and read `Editor agent turn $0.001152` in the project list, plus the covers sentence and two identical `Finding lines` rows. `Runtime.exceptionThrown` stayed empty. |
| The agent package imports no writer | PASS. `go list -deps ./internal/agent` names only `internal/agent`, `internal/config` and `internal/cost` among repository packages. |
| The mcp_readonly grant refuses an INSERT | PASS. `SHOW GRANTS FOR mcp_readonly` read `GRANT SELECT ON p6close_r6.*`. A SELECT answered 10. An INSERT of kind `agent` failed with code 497. |

## Residue against every prior round

| Prior round | Finding | Residue |
|---|---|---|
| `p6-close-round1.md` | H1, boundary and command edits wrote no commit | Zero. A real pointer drag and a confirmed command each wrote one child commit, one action and one snapshot. SQL read `boundary_nudged` by `manual_ui` and `user_command` by `command_bar`. Both survived a reload. |
| `p6-close-round2.md` | M1, a confirmed overlap could never save | Zero. A silent overlap answered 400 and wrote nothing. The same bounds with `allow_overlap` answered 201 and stored the overlap. Mutation B restored the blanket rejection and `TestEditBoundaryOverlapNeedsConfirmation/confirmed_overlap_saves` failed with `status = 400, want 201`. |
| `p6-close-round3.md` | APPROVE, no findings | Zero residue stands. I re-measured its two prior residues above. |
| `p6-close-round4.md` | M1, a speaker-only command on a confirmed overlap answered 422 | Zero. Against a stored overlap `0..5050` and `4000..8500`, a speaker-only preview answered 200, an overlap-growing left shift answered 422, and an overlap-shrinking right shift answered 200. Mutation A restored the flat rule and `TestConfirmedOverlapAcceptsSpeakerChangeAndRejectsNewOverlap` failed on all three subtests. |
| `p6-close-round4.md` | M2, no durable writer recorded agent charges | Zero. T6.6c landed. Two agent turns wrote four rows, the running total grew by 768000, and the covers sentence named the calls. |
| `p6-close-round5.md` | APPROVE, no findings | Zero residue stands. I re-measured both round 4 findings above. |
| `p6-close-remediation-round2.md` | The `allow_overlap` claim | Zero. A real keyboard and pointer path saved a confirmed overlap, and a silent overlap failed. Mutation B reproduces the defect. |
| `p6-close-remediation-round4.md` | The per-line overlap claim and the T6.6c filing | Zero. Mutation A reproduces the round 4 defect. T6.6c exists and its done conditions hold. |
| `t6.6c-round1.md` | C1, project charge key and folded row | Zero. The Details tab rendered two identical `Finding lines` rows and the `Editor agent turn` row with no exception. |
| `t6.6c-round1.md` | H1, journal before flush | Zero. With ClickHouse stopped, one POST answered 500 and left two queue files and 1266 bytes. Restarting drained the queue and SQL gained the two rows. |
| `t6.6c-round1.md` | H2, empty commit sentinel | Zero. Every agent row carried an empty `commit_id`. |
| `t6.6c-round1.md` | M1, the whole-pass comment | Zero. The comment states the writer attributes segmentation to the first rendered take and that agent calls own no take. |
| `t6.6c-round1.md` | M2, the running total call identity | Zero. SQL counted 1 segment, 1 translation, 7 render and 4 agent calls while the rows numbered 2, 2, 7 and 8. |
| `t6.6c-round1.md` | L1, the attempt rule | Zero. `sql/schema.sql` lines 41 and 764 both state that a genuine repeated take call must raise `attempt`. |
| `t6.6c-round2.md` | H, the alias shadow in `selectWholePassCharges` | Zero. The fold returned units 4 and total 1536000, exactly the agent rows alone. The segment rows stayed separate. |
| `t6.6c-round2.md` | C, the Details tab take charge keys | Zero. One take with a prompt row and a candidate row rendered both `Translation` rows with no exception. |
| `t6.6c-round2.md` | L, the tests | Zero. `internal/ledger/workspace_test.go` requires the substring `charges.kind = 'agent'`. |
| `t6.6c-round3.md` | H, four duplicate take keys | Zero. Line 3 held three takes on two files, one file twice. The page rendered the rail, the take chips and the Details tab with no exception. |
| `t6.6c-round3.md` | L, the stale comment | Zero. The comment above `selectWholePassCharges` names kind, units, unit price, total and position. |
| `t6.6c-round4.md` | L, the whole-pass order comment | Zero. The comment reads ordered by commit, kind, then unit. The shipped statement reads `ORDER BY commit_id ASC, kind ASC, unit ASC`. |
| `t6.6c-remediation-round1.md`, `-round2.md`, `-round3.md` | The three remediation tables | Zero. Every row is re-measured above. |

## Additional checks

`python3 tools/audit_docs.py` ran before the measurements and exited 0 with `No drift. Docs and repository agree.`

The phase file holds 14 task blocks, all `status: done`. All 91 `owns` entries exist on disk. The six exit boxes are unchecked. The README status board reads `P6 | 14 / 14`. The README current status and follow-up note name T6.6c and the pending P6 close. No drift appeared.

Svelte 5 only. `grep` over `web/src` found no `export let`, no `$:` and no store call. `web/package.json` pins `svelte ^5.57.0` and `node_modules/svelte/package.json` reads 5.57.0. The four keyed take lists and both charge lists append the array position.

`go test -count=1 ./internal/command ./internal/api ./internal/ledger ./internal/agent ./internal/cost ./internal/config ./cmd/...` exited 0 across seven packages. `unshare -rn sh -c 'ip link set lo up && go test ./internal/agent'` exited 0. `go vet -tags live ./internal/ledger ./internal/agent` exited 0. `go build ./...` exited 0. `npm --prefix web run check` read 183 files with 0 errors and 0 warnings.

The take audio route served 53816 bytes identical to the fixture, answered a range request with 206 and `Content-Range: bytes 1000-2999/53816`, and refused five traversal shapes, a missing file and a language escape with 404.

Notes, not findings. The Details tab labels the folded agent row `Editor agent turn` without a turn count, and the covers sentence above it states the count. The take route serves any regular file in the work directory, which prior rounds recorded. The real binary needs the fixture manifest in its working directory, which is a startup path rather than a P6 defect.

## Evidence paths

- Copy `/tmp/p6close-r6`, deleted after the review.
- ClickHouse container `p6r6-ch` on `127.0.0.1:28001` and `127.0.0.1:28002`, database `p6close_r6`, removed.
- Harness server `p6r6-harness` on port 28011, stopped. Real binary on port 28012, stopped.
- Data directory `/tmp/p6r6-data`, seed `/tmp/p6r6-seed.sql`.
- Harness `cmd/ajilamu/zz_p6r6_harness_test.go` in package `main` behind the `r6harness` tag, deleted with the copy.
- Headless Chrome over the managed browser daemon, tabs released.
- Mutation backups `/tmp/p6r6-parse.go.bak` and `/tmp/p6r6-mutations.go.bak`, deleted.

## Cleanup statement

I stopped the harness server and the real binary. I removed the ClickHouse container. I released
the browser tab. I deleted the copy, both mutation backups, the seed, the data directories and the
temporary binaries. No container, server or browser survives. The main checkout shows only this
new untracked record.
