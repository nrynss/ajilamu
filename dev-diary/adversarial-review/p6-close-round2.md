# P6 close: independent verification, round 2

**Scope:** Phase 6 exit criteria for ten tasks, measured at committed tree `c32cd3b`.
**Method:** Independent measurement with outside tools. I did not treat per-task review files as evidence.
**Verdict:** REMEDIATE. Findings: 0 C, 0 H, 1 M, 0 L.

| C | H | M | L |
|---|---|---|---|
| 0 | 0 | 1 | 0 |

I own only this review file. I did not tick the phase exit boxes. I fixed nothing.

## Audit

`python3 tools/audit_docs.py` exits 0. It prints "No drift. Docs and repository agree." The audit ran first, before I wrote this review.

## Findings

| Where | What | Pin | Mutation |
|---|---|---|---|
| `internal/api/mutations.go` lines 357 to 364, with `web/src/lib/edit/Boundary.svelte` lines 221 to 225 and `web/src/routes/d/[id]/+page.svelte` lines 294 to 313 | `boundaryEntry` rejects every overlap. The Boundary panel offers "Keep overlap" after a collision. Confirming it posts the overlapping bounds, and the route answers 400. T6.1 requires an explicit confirmation to allow an overlap. That confirmation can never succeed, so the control is dead. | I drove `/d/p6r2ui` in headless Chromium. I dragged line 1's end handle from 2000ms to 5000ms. The drag sent no request and the panel read `This timing overlaps line 2. Keep the overlap?`. A real click on `Keep overlap` sent `{"kind":"boundary","language":"ml-IN","segment_id":1,"start_ms":0,"end_ms":5000}`. The route answered 400 with `That boundary would overlap another line.`. The panel showed that sentence and the handle returned to `0:00.000 to 0:02.000`. | A fix carries the creator's explicit overlap confirmation to the route. Removing it returns `Keep overlap` to the 400 measured here. |

The route's blanket rejection contradicts the T6.1 rule, "Disallow silent overlapping without explicit user confirmation." A confirmed overlap is not silent. The panel already gates the collision behind a confirm, so the page can mark the confirmed case.

Concrete server change:

```go
// EditBody gains one field.
// AllowOverlap keeps a boundary the creator explicitly confirmed.
AllowOverlap bool `json:"allow_overlap,omitempty"`
```

```go
func boundaryEntry(entries []TimelineEntry, segmentID int, startMs, endMs int64, allowOverlap bool) (TimelineEntry, TimelineEntry, string) {
	target, ok := findTimelineEntry(entries, segmentID)
	if !ok {
		return TimelineEntry{}, TimelineEntry{}, editsNoLine
	}
	if startMs < 0 || endMs <= startMs {
		return TimelineEntry{}, TimelineEntry{}, editsBadTiming
	}
	if !allowOverlap {
		for _, other := range entries {
			if other.SegmentIndex == segmentID {
				continue
			}
			if startMs < other.EndMs && endMs > other.StartMs {
				return TimelineEntry{}, TimelineEntry{}, editsOverlap
			}
		}
	}
	after := target
	after.StartMs = startMs
	after.EndMs = endMs
	return target, after, ""
}
```

`editOutcome` passes `body.AllowOverlap`. `Boundary.svelte` sets `overlapConfirmed` in `confirmOverlap`. The page sends `allow_overlap: true` for that change only.

## Exit criteria

I measured each criterion. I did not tick the boxes.

| Criterion | Independent result |
|---|---|
| Boundaries, speakers, and text support manual user editing | Met. A real pointer drag on a served project moved line 1's end from 3000ms to 2000ms. `+page.svelte` mounts Boundary, Speaker and Text. `npm --prefix web run check` reports 183 files, 0 errors and 0 warnings. The explicit overlap confirm is broken, which is finding 1. |
| Billable operations display itemized prices before execution | Met for the wired paths. `Speaker.svelte` and `Text.svelte` show a fee from the newest billed take before the call. The command-bar edits are not billable. The agent turn has no route and no price display. |
| Command bar parses instructions into validated deterministic mutations | Met. Live previews parsed `shift`, `change speaker`, `shorten` and `move` into Go values. Unknown grammar, an unknown line, an unknown speaker and a collision each answered 422 with a clear sentence. |
| Timeline edits preserve prior takes without destructive overwrites | Met. The edits wrote timeline snapshots only and left the `takes` table at three rows. An overlay probe measured `nextTakeNumber` at 1, 2, 3 and 5 over seeded files, so a new take never reuses a name. |
| All mutations write author-attributed commits to ClickHouse | Met. A boundary drag and a command confirmation each wrote one commit, one action and one timeline snapshot. Both survived a reload. See the ledger pin below. |
| The editor agent reads the ledger through mcp-clickhouse and writes nothing | Met at the library level. Only `internal/agent` and its test binary import `google.golang.org/adk/v2`. `ReadTools` holds `list_databases`, `list_tables` and `run_query`. `requireSelectOnlyGrants` accepts SELECT alone. `unshare -rn go test ./internal/agent` passes with no socket. |

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

## Criterion five, measured live

I ran `cmd/ajilamu` on port 8791 against ClickHouse 26.8.2.7 through `sql/schema.sql`. I seeded the dub `p6r2ui` with one commit, three timeline rows and three takes.

1. A real pointer drag on line 1's end handle moved it from 3000ms to 2000ms. The page sent `POST /api/dubs/p6r2ui/edits` with `{"kind":"boundary","language":"ml-IN","segment_id":1,"start_ms":0,"end_ms":2000}` and the route answered 201.
2. A real command-bar confirmation of `shift line 2 right by 250ms` sent `{"kind":"command","language":"ml-IN","command":"shift line 2 right by 250ms","duration_ms":75008}` and the route answered 201.
3. Outside SQL read three commits in one chain. Version 1 is the seed, version 2 is the boundary child, version 3 is the command child.
4. The boundary action reads `boundary_nudged`, `manual_ui` and an empty prompt. The command action reads `user_command`, `command_bar` and the instruction verbatim. Each writes one snapshot, and each snapshot copies the source text, target text and take id forward.
5. After a reload the boundary panel read `0:00.000 to 0:02.000`. Line 2 read `0:03.750 to 0:08.250`. The workspace payload served the same bounds.
6. The History tab read `A line boundary was adjusted.` under `you` and `shift line 2 right by 250ms` under `editor command`.
7. A failed write never reported success. An overlap answered 400. With ClickHouse stopped, a drag answered 500 with `Could not read the project ledger.`. The dub still held three commits, two actions and five snapshots.
8. Twelve concurrent boundary edits all answered 201. Outside SQL read thirteen commits with thirteen distinct `version_seq` values. The head ancestry held all thirteen, and twelve edit commits sat on that chain. No fork appeared.

## Prior-round residue, re-measured at the artifact level

**T6.1: zero residue.** `Boundary.svelte` defaults snap 120ms, minimum 100ms and keyboard step 50ms. A keyboard move passes `shouldSnap=false`. A collision opens a confirm and does not commit. The explicit confirm is finding 1.

**T6.2: zero residue.** `jq` on `testdata/wire/dub.json` summed the charges on the newest billed take of each line. The eight sums are 842100, 2438300, 2318200, 3639200, 3459900, 2949700, 4751000 and 2948600 nanodollars. `Speaker.svelte` reads that same take as the estimate basis.

**T6.3: zero residue.** `Text.svelte` edits both fields in place. The source textarea carries `lang={sourceLanguage || undefined}` and the target carries `lang={language}`. The source confirm sends `source_text`. `internal/fit/rewrite.go` sets `AuthoritativeText` and skips the translator on every attempt.

**T6.4: zero residue.** Live previews parsed the four documented grammars. `shift line 3 right by 200ms.` returned 9200 to 12200. `shorten line 3 by 0.5s.` returned 9000 to 11500. `change speaker for line 2 to Mark.` resolved the speaker. `move wav 1 to 0:0005 to right.` parsed and then reported the collision. An unknown line, an unknown speaker and unknown grammar each answered 422.

**T6.5a: zero residue.** The route mounts at `POST /api/dubs/{id}/lines/{segment}/rerender`. A bad segment returned 400, a bad body returned 400 and a missing line returned 404.

**T6.5: zero residue.** `Track.svelte` renders `takes.slice(0, -1)` as ghosts and `takes.at(-1)` as active. Both take chips call `playTake`.

**T6.5b: zero residue.** `rerenderBody` carries `source_text`. `CorrectedRecorder` records `text_corrected` with `manual_ui`. `AuthoritativeText` speaks the creator's text on every attempt.

**T6.5c: zero residue.** The live take route returned 200 with `audio/wav` and byte-identical bytes, 206 with `Content-Range: bytes 0-9/3244`, and 404 for six traversal shapes and a missing file.

**T6.6: zero residue.** Only `internal/agent` imports ADK. `unshare -rn go test ./internal/agent` passes with no socket. `ReadTools` names three read tools. `requireSelectOnlyGrants` rejects INSERT, ALTER, ALL and every non-SELECT privilege.

**T6.7: one finding.** The write path lands every edit, and the shared per-dub lock holds under twelve concurrent edits. The overlap confirm is broken, which is finding 1.

## Residue against round one finding 1

**Zero residue.** Round one filed one H. Boundary and command-bar edits wrote browser state only, so exit criterion five was unmet. T6.7 landed `POST /api/dubs/{id}/edits`. A real boundary drag and a real command confirmation each wrote one commit, one action and one timeline snapshot. Both reached ClickHouse and both survived a reload. The History tab rendered both. The round one pin, a live edit that wrote no commit, no longer reproduces.

Finding 1 in this review is a new defect. It is not residue of the round one finding.

## Notes, not findings

The fixture workspace cannot save an edit. A drag on `/d/fixture` posts to `/api/dubs/fixture/edits` and answers 404 `This project has no timeline to edit.`. The handle reverts. The fixture has no ledger, so the route cannot record it. The fixture was never durable, and I did not assume it should be.

A boundary for a missing segment answers 400. A command for a missing segment answers 404. The page shows the same sentence for both, so no consumer observes the difference.

`internal/api/mutations.go` line 346 still says a drag may not overlap a neighbour. That sentence describes the current code. It conflicts with the T6.1 rule behind finding 1.

## What I measured

- `python3 tools/audit_docs.py` exit 0.
- `go build -o /tmp/p6r2/ajilamu ./cmd/ajilamu` exit 0.
- ClickHouse 26.8.2.7 from the committed tarball, loaded with `sql/schema.sql`.
- The server ran on port 8791 with a real ledger and a real project.
- `go test -count=1 ./internal/api ./cmd/...` passed for both packages.
- `unshare -rn go test -count=1 ./internal/agent` passed.
- `npm --prefix web run check` reported 183 files, 0 errors and 0 warnings.
- A `go test -overlay` probe measured `nextTakeNumber`. The repository stayed clean.
- Headless Chromium 1234 drove the built bundle through CDP. I wrote the probes. The drag, the command confirmation, the reload and the History tab are real browser events.
- I did not run a live Gemini or Cloud TTS call. The rerender pins rest on the route's reject paths and the committed components.
