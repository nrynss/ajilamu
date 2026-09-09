# P6 close remediation, round 4

This record answers the two M findings in `p6-close-round4.md`. I did not change the verdict.
I did not tick an exit box, mark a task done, mark the phase complete, or commit.

I differ from the round 4 close reviewer and from every earlier P6 reviewer.

| Finding | Fix | Pin | Residue |
|---|---|---|---|
| 1, M. `command.Validate` rejected a speaker-only command on either line of a creator-confirmed overlap, because it tested the candidate against a flat no-overlap rule. Chrome received `line 2 would overlap line 1` for `change speaker for line 2 to Mark`. | `overlapping` in `internal/command/parse.go` now compares per line. It measures the milliseconds the candidate shares with each other line against the milliseconds the stored segment already shares with that line, and fails only where the candidate number is larger. New helper `overlapMs` does the measurement. `Validate` and the package comments state the rule. | `TestConfirmedOverlapAcceptsSpeakerChangeAndRejectsNewOverlap` in `internal/command/parse_test.go` and `TestCommandPreviewHandlerAllowsCommandsOnAConfirmedOverlap` in `internal/api/command_test.go`. Both run the timeline the reviewer saved, with line 1 at `0..4500` and line 2 at `4000..8000`. | No residue. The reviewer's mutation holds. Restoring the flat rule as `overlapMs(candidate, other) > 0` returns the exact 422 sentence Chrome received. |
| 2, M. Agent turns bill tokens and no durable writer records the charges, so the running total omits every turn. `charges_raw.kind` accepts only `segment`, `translate` and `synthesize`. | Filed **T6.6c: Record agent charges in the ledger** in `dev-diary/PHASE-6-editing.md`. Wrote no writer code, per the review's own conclusion that this needs a new task. Updated the README status board to `13 / 14` and named T6.6c in the current status line and the follow-up wave note. Updated the T6.6b handoff observation to name the task the close filed. | `python3 tools/audit_docs.py` exits 0 and prints `No drift`. Its README check parses the phase file and would report drift if the count and the task blocks disagreed. | No residue against the finding, which asked for a task rather than a feature. The defect itself stays open by design. `sql/schema.sql` still rejects kind `agent` and the total still omits every turn until T6.6c lands. |

## Finding 1, the confirmed overlap

### Why the old rule was wrong

T6.7 gives `POST /api/dubs/{id}/edits` an `allow_overlap` flag. Only the Boundary panel's
`Keep overlap` control sends it. A creator who confirms an overlap therefore saves one, and
`internal/api/mutations_test.go` pins that path with `confirmed overlap saves`.

An overlapping timeline is valid stored state. The command validator did not know that. It ran
`overlapping` on the candidate and failed on any shared millisecond, so every command on either
overlapping line failed. A speaker change moves no boundary and cannot introduce an overlap, so
the creator lost the command path on a state the product allows.

### The new rule

```go
func overlapping(segments []Segment, current int, candidate Segment) (Segment, bool) {
	stored := segments[current]
	for index, other := range segments {
		if index == current {
			continue
		}
		if overlapMs(candidate, other) > overlapMs(stored, other) {
			return other, true
		}
	}
	return Segment{}, false
}
```

The comparison runs per line rather than against a flat rule. Three consequences follow.

1. A speaker change leaves both boundaries alone, so every comparison holds equal and the
   command validates.
2. A move or a shift that reaches into another line grows one comparison and still fails.
3. A command that shrinks a confirmed overlap validates, which gives the creator a way to
   repair one from the bar.

Case 3 is new behaviour that no finding asked for. It falls out of the rule rather than sitting
beside it. A rule that rejected a shrinking overlap would have to treat the confirmed state as
both legal to hold and illegal to improve.

### Pins

`internal/command/parse_test.go` runs three accepted mutations and two rejected ones against
`overlappingTimeline`, which reproduces the reviewer's saved state.

| Mutation | Expected | Reason |
|---|---|---|
| `ChangeSpeaker` line 1 to `Suni` | accepted, bounds stay `0..4500` | boundaries do not move |
| `ChangeSpeaker` line 2 to `Mark` | accepted, bounds stay `4000..8000` | boundaries do not move |
| `Shift` line 2 right by 250ms | accepted, bounds become `4250..8250` | the shared span falls from 500ms to 250ms |
| `Move` line 1 left to 4000ms | rejected | the shared span with line 2 grows from 500ms to 4000ms |
| `Shift` line 3 left by 1500ms | rejected | line 3 was clear and now shares 500ms with line 2 |

`internal/api/command_test.go` pins the route, because the reviewer measured a 422 over HTTP
rather than a Go error. `change speaker for line 2 to Mark.` answers 200 and returns
`{ID: 2, StartMs: 4000, EndMs: 8000, DurationMs: 4000, Speaker: "Mark Vande Hei"}`.
`shift line 3 left by 1500ms.` answers 422 with `line 3 would overlap line 2`.

### Mutation evidence

Measured on 2026-09-09. I replaced the per-line comparison with the flat rule
`overlapMs(candidate, other) > 0` and reran the two tests.

```text
--- FAIL: TestConfirmedOverlapAcceptsSpeakerChangeAndRejectsNewOverlap/speaker_change_on_the_line_that_overruns
    parse_test.go:166: Validate(...Speaker:"Suni"): line 1 would overlap line 2
--- FAIL: TestConfirmedOverlapAcceptsSpeakerChangeAndRejectsNewOverlap/speaker_change_on_the_line_that_is_overrun
    parse_test.go:166: Validate(...Speaker:"Mark"): line 2 would overlap line 1
--- FAIL: TestConfirmedOverlapAcceptsSpeakerChangeAndRejectsNewOverlap/shift_that_shrinks_the_confirmed_overlap
    parse_test.go:166: Validate(...DeltaMs:250): line 2 would overlap line 1
--- FAIL: TestCommandPreviewHandlerAllowsCommandsOnAConfirmedOverlap
    command_test.go:83: speaker status = 422, want 200: line 2 would overlap line 1
```

The route failure prints the sentence Chrome received in the close review. I restored the fix
and both packages pass again.

### What I did not touch

`web/src/routes/d/[id]/+page.svelte` belongs to the P5 remediator. The route needed no change,
so `internal/api/command.go` is unedited. Only its test file gained the route pin.

## Finding 2, agent charges

The review asked for a task and said so twice. Its finding row reads "This needs a new task."
Its spend decision reads "The missing agent spend is a defect that needs a new task." I filed
the task and wrote no writer code.

### T6.6c owns

```yaml
requires:   T6.6a, T6.6b, T4.1
fixture-ok: no
size:       M · frontier
owns:       sql/schema.sql,
            internal/ledger/charges.go, internal/ledger/charges_test.go,
            internal/ledger/takes.go, internal/ledger/takes_test.go,
            internal/ledger/workspace.go, internal/ledger/workspace_test.go,
            internal/api/agent.go, internal/api/agent_test.go,
            cmd/ajilamu/main.go, cmd/ajilamu/main_test.go
status:     not-started
```

`internal/agent` appears nowhere on that line. The agent package stays read-only, and ledger
writes stay on `internal/ledger`. The route gains a recorder seam that `cmd/ajilamu/main.go`
adapts, which is the pattern T6.5a, T6.5b and T6.7 already use.

### Three real obstacles the task names

I read the storage before writing the task, so the text names measured obstacles rather than
guesses.

1. **The enum.** `sql/schema.sql` line 780 declares
   `kind Enum8('segment' = 1, 'translate' = 2, 'synthesize' = 3)`. The declared-shape preflight
   at line 114 repeats it, so the task must change both.
2. **The writer hangs every row off a take.** `chargeInsert` at `internal/ledger/takes.go`
   line 19 is the only charge insert in the repository. `chargeRows` rejects a charge whose
   `TakeID` differs from the segment. An agent turn renders nothing, so the task must use the
   whole-pass shape with the empty take id and the `-1` segment sentinel.
3. **Two identical turns would collapse into one row.** `charges_raw` is a
   `ReplacingMergeTree`, and `event_key` hashes the dub, language, commit, segment index,
   attempt, kind, provider, unit, units and unit price. Nothing in that hash separates two
   turns that report the same token counts against one commit. The task requires a distinct
   turn identity and asks for the pin to use two identical turns.

The done conditions ask for SQL read-back rather than a log line. They also ask that
`internal/agent` still import no writer and that `mcp_readonly` still refuse an INSERT.

### The frontend needs nothing

`web/src/lib/tabs/DetailsTab.svelte` line 59 already maps `agent` to `Editor agent turn`. The
T6.6 handoff warned that the label was missing. It landed since, so no frontend task blocks
T6.6c.

## Checks

| Check | Result |
|---|---|
| `go test -count=1 ./internal/command ./internal/api` | ok for both packages |
| `python3 tools/audit_docs.py` | exit 0, `No drift. Docs and repository agree.` |
| Files edited | `internal/command/parse.go`, `internal/command/parse_test.go`, `internal/api/command_test.go`, `dev-diary/PHASE-6-editing.md`, `dev-diary/README.md`, this file |
