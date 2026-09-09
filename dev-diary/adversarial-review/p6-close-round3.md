# P6 close: independent verification, round 3

**Scope:** Phase 6 exit criteria for ten tasks, measured at the working tree on top of committed tree `c32cd3b`. The tree carries the uncommitted round 2 remediation in three files.
**Method:** Independent measurement with outside tools. I did not write round 1 or round 2, and I did not remediate. I treated no review file as evidence.
**Verdict:** APPROVE. Findings: 0 C, 0 H, 0 M, 0 L.

| C | H | M | L |
|---|---|---|---|
| 0 | 0 | 0 | 0 |

I own only this review file. I did not tick the phase exit boxes. I fixed nothing.

## Audit

`python3 tools/audit_docs.py` exits 0. It prints "No drift. Docs and repository agree." I ran it first, before any other measurement.

## Findings

No finding met the bar of provable impact, an actionable fix, an unintended defect, and a patch introduction.

| Where | What | Pin | Mutation |
|---|---|---|---|
| None | None | None | None |

## Confirmed overlap and silent overlap, re-measured

I built `/tmp/p6r3/ajilamu` from the working tree. I ran ClickHouse 26.8.2.7 from the committed tarball on port 8127 with a fresh data directory, loaded `sql/schema.sql`, and seeded `p6r3ui` with one root commit and three timeline rows. Line 1 ran 0 to 3000, line 2 ran 3750 to 8250, and line 3 ran 9000 to 12000.

**Silent overlap.** A boundary post for line 1 with `end_ms` 5050 and no flag answered 400. The sentence read `That boundary would overlap another line.` Outside SQL read the ledger unchanged at one commit, zero actions and three snapshots.

**Confirmed overlap.** The same bounds with `allow_overlap` true answered 201. The response named commit `8903c4c5535824e41c6a56823aab84ec`. Outside SQL read one commit child of `r3-c1`, one action `boundary_nudged` by `manual_ui` with before `0..3000` and after `0..5050`, and one snapshot at `0..5050`. A reload served line 1 at `0..5050` against line 2 at `3750..8250`, so the stored state carries the overlap.

**The page sends the flag only on the confirm.** Headless Chrome on port 9335 drove `/tmp/p6r3/web/build`, built from the working tree. Real keyboard events on line 1's end handle sent `end_ms` 3050 with no flag, and the route answered 201. A first shift nudge sent `end_ms` 3550 with no flag, and the route answered 201. A second shift nudge produced `end_ms` 4050, the panel read `This timing overlaps line 2. Keep the overlap?`, and no request left the page. A real click on `Keep overlap` sent `{"kind":"boundary","language":"ml-IN","segment_id":1,"start_ms":0,"end_ms":4050,"allow_overlap":true}`, and the route answered 201. After a reload the panel read `0:00.000 to 0:04.050`, and outside SQL read line 1 at `0..4050` against line 2 at `3750..8250`.

## The round 2 fix, pinned by mutation

I copied the working tree to `/tmp/p6r3/repo`. The unmutated `TestEditBoundaryOverlapNeedsConfirmation` passes both subtests.

Mutation A restored the blanket rejection by changing `if !allowOverlap {` to `if true {`. The confirmed subtest failed.

```text
--- FAIL: TestEditBoundaryOverlapNeedsConfirmation/confirmed_overlap_saves
        mutations_test.go:261: confirmed overlap status = 400, want 201
```

Mutation B ignored the flag by changing the same line to `if false {`. The silent subtest failed.

```text
--- FAIL: TestEditBoundaryOverlapNeedsConfirmation/silent_overlap_fails
        mutations_test.go:239: overlap status = 201, want 400
```

I restored the file after each mutation and confirmed it matched the working tree.

## Required validation

`go test -count=1 ./internal/api ./cmd/...` exits 0.

```text
ok  	github.com/nrynss/ajilamu/internal/api	3.964s
ok  	github.com/nrynss/ajilamu/cmd/ajilamu	4.428s
```

`npm --prefix web run check` exits 0.

```text
COMPLETED 183 FILES 0 ERRORS 0 WARNINGS 0 FILES_WITH_PROBLEMS
```

`unshare -rn go test -count=1 ./internal/agent` exits 0 with no socket.

## Exit criteria

I measured each criterion. I did not tick the boxes.

| Criterion | Independent result |
|---|---|
| Boundaries, speakers, and text support manual user editing | Met. A real keyboard nudge on line 1 saved a commit and survived a reload. The Speaker panel renders one toggle per speaker, and its confirm is enabled. The Text panel edits both fields in place, the source field carries `lang={sourceLanguage || undefined}`, the target field carries `lang={language}`, and the source confirm sends `source_text`. `npm run check` reports 183 files, 0 errors and 0 warnings. |
| Billable operations display itemized prices before execution | Met for the wired paths. On `/d/fixture` I selected line 1's other speaker, and the panel read `$0.0008421` from `seg_1_try1.wav`. An outside sum of that take's charges reads 842100 nanodollars. The eight fixture lines sum to 842100, 2438300, 2318200, 3639200, 3459900, 2949700, 4751000 and 2948600. Speaker and Text use the newest billed take as the basis. The agent turn has no route and shows no price, which T6.6 records. |
| Command bar parses instructions into validated deterministic mutations | Met. Three grammars answered 200 with a parsed mutation and a Go-derived segment. `shift line 3 right by 200ms.` returned 9200 to 12200. `shorten line 3 by 0.5s.` returned 9000 to 11500. `change speaker for line 2 to Mark.` resolved the speaker. The move grammar answered 200 for a clear target and 422 `line 1 would overlap line 2` for a colliding one. An unknown line, an unknown speaker and unknown grammar each answered 422. |
| Timeline edits preserve prior takes without destructive overwrites | Met. Across one boundary edit and one command edit the `takes` view stayed at three rows, and each edit wrote timeline rows only. An overlay probe measured `nextTakeNumber` over seeded files. Empty read 1, `try1` read 2, `try1` plus `try2` read 3, and `try1` plus `try2` plus `try4` read 5. A stretched take, another segment and a non-wav file did not move the answer. |
| All mutations write author-attributed commits to ClickHouse | Met. The boundary edit wrote one commit, one action `boundary_nudged` by `manual_ui` and one snapshot. The command edit wrote one commit, one action `user_command` by `command_bar` with the instruction verbatim and one snapshot. The history route returned both with the author and the instruction. Both survived a reload. |
| The editor agent reads the ledger through mcp-clickhouse and writes nothing | Met at the library level. Only `internal/agent` and its tests import `google.golang.org/adk/v2`. `ReadTools` names `list_databases`, `list_tables` and `run_query`. `requireSelectOnlyGrants` accepts SELECT alone and rejects INSERT, ALTER and DROP. `unshare -rn go test ./internal/agent` passes with no socket. |

## Task status and owns

All ten P6 task blocks read `status: done`. All 57 owns entries exist on disk. The 39 unique paths exist.

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
| T6.7 | done | 6 | yes |

## Residue against round one finding 1

**Zero residue.** Round one filed one H. Boundary and command-bar edits wrote browser state only, so exit criterion five was unmet. T6.7 landed `POST /api/dubs/{id}/edits`. A real boundary nudge and a real command confirmation each wrote one commit, one action and one timeline snapshot to ClickHouse. The history route rendered both with the author and the instruction. Both survived a reload. The round one pin, a live edit that wrote no commit, no longer reproduces.

## Residue against round two finding 1

**Zero residue.** Round two filed one M. The Boundary panel's `Keep overlap` confirmation could never save, because `boundaryEntry` rejected every overlap. The remediation added an explicit `allow_overlap` flag. A silent overlap still answers 400 with zero writes. A confirmed overlap answers 201 and stores the overlapping bounds. The page sends the flag only for the confirmed collision, proven with real keyboard events and a real click. Mutations A and B each fail the right subtest. The round two pin, a confirmed overlap that answers 400, no longer reproduces.

## Notes, not findings

The fixture workspace cannot save an edit. A drag on `/d/fixture` answers 404, because the fixture has no ledger. This is pre-existing and not part of the remediation.

`allow_overlap` is client-asserted. A scripted client can send it with no UI confirm. The round two review proposed this server shape, and the page's only overlapping emission path is the confirm.

The page derives the flag from the same overlap predicate the Boundary panel uses. The two agree today. A future change to one predicate must change the other.

## What I measured

- `python3 tools/audit_docs.py` exit 0, run first.
- `/tmp/p6r3/ajilamu` built from the working tree.
- ClickHouse 26.8.2.7 on port 8127 with `sql/schema.sql` and a fresh `p6r3ui` seed.
- The silent overlap answered 400 and wrote nothing. The confirmed overlap answered 201 and wrote one commit, one action and one snapshot.
- Headless Chrome on port 9335 drove the built bundle from `/tmp/p6r3/web/build`. The keyboard nudges and the confirm click are real events.
- Mutation A and mutation B each failed the expected subtest in `/tmp/p6r3/repo`.
- `go test -count=1 ./internal/api ./cmd/...` exit 0.
- `npm --prefix web run check` exit 0 with 183 files.
- `unshare -rn go test -count=1 ./internal/agent` exit 0.
- A `go test -overlay` probe measured `nextTakeNumber` in the copy. The repository stayed clean.
- I did not run a live Gemini or Cloud TTS call. The re-render pins rest on the route's reject paths and the committed tests.
