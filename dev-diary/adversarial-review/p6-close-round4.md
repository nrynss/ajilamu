# P6 close: independent verification, round 4

**Scope:** All 13 Phase 6 tasks at committed tree `c4f1f94`.
**Method:** Independent HTTP, Chrome, SQL, and filesystem measurements.
**Verdict:** REMEDIATE. Findings: 0 C, 0 H, 2 M, 0 L.

| C | H | M | L |
|---|---|---|---|
| 0 | 0 | 2 | 0 |

I own only this review file. I did not tick exit boxes, fix code, change task status, or commit.
I differ from every prior P6 close reviewer.

## Audit and required checks

`python3 tools/audit_docs.py` ran first and exited 0.
It reported no drift.

`go test -count=1 ./internal/api ./cmd/...` passed.
The Node 26 frontend check reported zero errors and zero warnings.
macOS lacks `unshare`, so I used `sandbox-exec` with external network access denied.
`go test -count=1 ./internal/agent` passed while localhost remained available for its in-memory HTTP test.

## Findings

| Where | What | Pin | Mutation |
|---|---|---|---|
| `internal/command/parse.go`, `Validate`, and `internal/api/command.go` | A creator-confirmed overlap is valid stored state. The command validator rejects a speaker-only command on either overlapping line, although that command changes no timing. Chrome received `line 2 would overlap line 1` for `change speaker for line 2 to Mark`. The manual speaker control remains a workaround. | Chrome first saved line 1 at `0..4500` against line 2 at `4000..8000`. SQL confirmed the overlapping snapshot. A later preview POST returned 422. The same browser previewed and saved `change speaker for line 3 to Suni`, proving the command path remained healthy for a non-overlapping line. | A fix must reject newly introduced overlaps without rejecting unchanged, creator-confirmed overlaps. Reverting that fix must make the speaker-only preview return 422 again. |
| `internal/api/agent.go`, `internal/ledger/takes.go`, and `sql/schema.sql` | Agent turns cost money, but no durable writer records their charges. The project running total therefore omits every agent call. Its `covers` sentence limits the displayed claim, but UI rule 7 still requires every billed call once and continuous totals. This needs a new task. The task must preserve the agent's read-only boundary while a durable application writer records charges. | The offline real-agent route returned two measured charges totaling 768000 nanodollars. The route type has no ledger writer. An independent insert of kind `agent` into the loaded schema failed with ClickHouse code 691. The enum accepts only `segment`, `translate`, and `synthesize`. | A new task must extend storage, the durable writer, and the running total. Reverting that work must either make the agent charge insert fail or make the total omit a measured turn. |

## Exit criteria

| Criterion | Independent result |
|---|---|
| Boundaries, speakers, and text support manual user editing | Met. Chrome rendered all four enabled editor fieldsets on a real project. A trusted boundary drag saved and survived reload. Speaker and text controls opened their confirmations. |
| Billable operations display itemized prices before execution | Met. Speaker and target-text confirmations showed `$0.0004` before any POST. A parser 422 showed both agent rates before the ask. The answer showed the measured `$0.00032865` afterward. Finding 2 concerns durable total completeness. |
| Command bar parses instructions into validated deterministic mutations | Not fully met. The agent proposal re-entered preview unchanged and a valid line 3 command saved. Finding 1 blocks commands on lines in permitted overlap state. |
| Timeline edits preserve prior takes without destructive overwrites | Met. The live edit sequence left the take count at three. Targeted rerender checks kept `try1`, `try2`, and `try3` across both correction orders. |
| All mutations write author-attributed commits to ClickHouse | Met for the mutation routes. SQL read two `boundary_nudged` rows by `manual_ui`. It also read one `user_command` row by `command_bar`, with the instruction verbatim. |
| The editor agent reads the ledger through mcp-clickhouse and writes nothing | Met. The offline route made one MCP `run_query` read and two model calls. Only `internal/agent` imports ADK. The filtered tool list contains three read tools. The database grant checks reject write privileges. |

## New work since round 3

### T6.6a

`POST /api/dubs/{id}/agent` exists.
With no MCP settings it returned 503 and `The editor agent is unavailable.`
The same process served the workspace and health route with status 200.
The route receives no edit recorder, run recorder, or ledger writer.

### T6.6b

Chrome sent a parser-rejected instruction and received the 422 offer.
The offer showed `$0.00000015` and `$0.00000060` before the turn.

The browser observed one agent POST carrying the rejected text.
A reviewer-controlled response reported 328650 nanodollars.
The page rendered `This turn cost $0.00032865.`

The answer `shift line 2 right by 250ms` re-entered the preview route byte for byte.
The preview rendered `Shift line 2 right by 0.250s.`
The whole answer flow sent zero edit POSTs.

### T6.7a

Chrome measured all three fixture aliases.
Each alias rendered four disabled editor fieldsets and four read-only sentences.
Each alias held ten disabled native controls.
A forced command submit produced zero POST requests.

A real project drag sent one edit POST.
SQL moved from one seed commit to two commits and preserved three take rows.
The new action read `boundary_nudged` by `manual_ui`.

## Prior close residue

**Round 1 H1 has zero residue.**
That finding said boundary and command edits wrote no commits.
A trusted drag and a confirmed command each wrote one commit, one action, and one timeline snapshot.
SQL read both authors, and reload preserved both changes.

**Round 2 M1 has zero residue.**
That finding said a confirmed overlap could not save.
A silent overlap returned 400 and left the commit count at four.
Chrome then sent `allow_overlap: true` only after `Keep overlap`.
The request returned 201 and SQL stored line 1 at `0..4500`.

Finding 1 is new behavior after the confirmed overlap saves.
It does not reopen round 2.

## Agent spend decision

The missing agent spend is a defect that needs a new task.
It is not only a note.

The `covers` sentence prevents the current number from claiming agent spend.
It does not satisfy the stronger rule that every billed call counts once.
The measured charge also disappears from the workspace after the command bar clears it.

The new task must use the durable application writer.
It must not grant write access to the agent or mcp-clickhouse.

## Evidence

- Review server binary: `/tmp/p6close-r4-ajilamu`
- Review data directory: `/tmp/p6close-r4-data`
- Chrome profile: `/tmp/p6close-r4-chrome`
- Built frontend: `web/build`
- Isolated ClickHouse database: `p6close_r4`
- Fresh non-fixture dub: `p6r4live`
- Loaded schema: `sql/schema.sql`

The final SQL reading showed four commits, three actions, six snapshots, and three takes.
The actions held two manual boundary changes and one command-bar speaker change.

## Process ownership

The tree started clean at `c4f1f94`.
Other reviews created `p5-close-round1.md` and `p7-close-round5.md` during this run.
I did not read or modify either file.

I stopped every process and container I started before finishing this review.
