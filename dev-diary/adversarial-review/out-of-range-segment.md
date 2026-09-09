# Out-of-range segment fix

**Date:** 2026-09-09
**Agent:** OutOfRangeSegmentFix, implementation role
**Verdict:** A segment outside the film now becomes a flagged line. The run finishes.

## Mechanism

The live run for dub `13333271b29564a79b467732030f9b81` died after the creator paid
80,777,700 nanodollars. The source clip runs 75.008 seconds. Segmentation returned
segment 7 at 106,420 to 114,880 milliseconds, which lies past the film end.

Nothing checked the segment against the film length. The fit loop translated and
rendered the line, because a segment only needs a positive slot. It spent money on
three attempts for a line the film cannot hold. The assembler then refused the clip.
`internal/assemble/place.go` compared the clip start against the bed frames and
returned `segment 7 starts after the bed`. That error travelled back through
`assembleRun` and `pipelineRunner.Run` into `runRegistry.execute`. The registry calls
`run.fail`, so the terminal event was `error` with the sentence `The run could not
finish.` Persist never ran, so the takes and their charges stayed on disk.

The assembler was right. A clip that starts past the bed has no position on it. The
defect is that the run let that correct refusal decide its fate.

## The fix

The loop now validates the segmentation against the source duration when the segments
arrive. `fit.PipelineConfig` carries a new `SourceDuration` field. The production
adapter in `cmd/ajilamu/main.go` probes the source once with `media.Duration` and
passes it.

The loop then splits the segments against the film in milliseconds.

- A segment that starts at or after the film end is flagged. The loop renders nothing
  for it, so it never reaches placement.
- A segment that starts inside the film but ends past the film end is clamped to the
  film end and flagged. It still renders at its shortened slot.

A flagged line keeps a `LineResult` with no attempts. `pipelineRunResult` therefore
writes a timeline snapshot for it, which names the line in the workspace. The snapshot
carries no take, so the workspace readiness turns to review. The excluded line never
bills a translate or a render call.

The assembler keeps its refusal, because the guard is correct. It is now a typed
`*assemble.OutsideBedError` that satisfies `errors.Is(err, ErrClipOutsideBed)`. The
error names every refused clip. `placeTakes` in `cmd/ajilamu/main.go` catches that
error, drops the refused clips, flags their lines, and places the rest. A genuine
unplaceable clip therefore costs the creator a flagged line rather than the run.

## Pins

Every pin measures an artifact. None quotes a run log.

### The terminal event

`TestRunFlagsSegmentOutsideFilm` in `cmd/ajilamu/main_test.go` mounts the real API
server over the real runner and a real `runRecorder`. A stubbed segmenter returns one
line inside the film and one line at 106,420 to 114,880 milliseconds. The test reads
the server-sent event stream over HTTP. Post-fix:

```text
id: 17
data: {"type":"done","stage":"repairing","sentence":"Dubbing pipeline completed with 1 flagged line.","total_nanodollars":708000,"segment_id":0,"language":"ml-IN","take_file":""}
```

The stream also carries `Line 2 lies outside the film, so the run left it out and
flagged it.` The stream carries no `"type":"error"`.

### The same run before the fix

The same test ran against a clean `git archive HEAD` tree with only the new test file
copied in. The runner, the stubs, and the server are unchanged from HEAD. Pre-fix:

```text
id: 24
data: {"type":"error","stage":"measuring","sentence":"The run could not finish.","total_nanodollars":1689000,"segment_id":0,"language":"ml","take_file":""}
level=ERROR msg="run failed" run_id=... dub_id=dub-out-of-range stage=measuring error="place the takes: segment 2 starts after the bed"
--- FAIL: TestRunFlagsSegmentOutsideFilm
```

The pre-fix run spent 1,689,000 nanodollars on stubs, because it rendered three
attempts for a line the film cannot hold. It persisted nothing. That is the live
failure in miniature.

### The export

`ffprobe` measured the exported film the run wrote, not the run's own report:

```text
ffprobe .../work/ml-IN/dubbed_ducked.mp4 duration = 2.000s
```

The source film is 2.000 seconds, so the export kept the film length after dropping
the refused line.

### Persistence against a real ClickHouse

Docker ran `clickhouse/clickhouse-server:26.8.2.7` on port 18123 for HTTP and port
19000 for native. `sql/schema.sql` loaded into database `ajilamu_pin`. The container
is stopped and removed.

`TestRunFlagsSegmentOutsideFilmPin` in `cmd/ajilamu/main_test.go` runs the same
out-of-range scenario and persists through the real `runRecorder`:

```text
terminal event = done "Dubbing pipeline completed with 1 flagged line."
ffprobe export duration = 2.000s
takes = 2	1	[1]
charges = 8	0	708000
timeline:
    1	source one
    2	past the film
```

The pin reads the stored rows, so two takes exist on line 1 alone, no charge belongs
to line 2, and the charge sum is 708,000 nanodollars. A direct client query over the
same database agrees:

```sql
SELECT segment_index, source_text, take_id FROM timeline_state ORDER BY segment_index;
-- 1  source one     dub-out-of-range-pin-...-take-0
-- 2  past the film  (empty)

SELECT segment_index, attempt, repair FROM takes ORDER BY segment_index, attempt;
-- 1  1  rewrite
-- 1  2  none

SELECT kind, segment_index, count() FROM charges GROUP BY kind, segment_index ORDER BY kind, segment_index;
-- segment     1  2
-- translate   1  4
-- synthesize  1  2
```

### A segment fully inside the film

`TestPipelineFlagsSegmentOutsideFilm` in `internal/fit/loop_test.go` also carries an
in-range line. It renders one attempt, stays unflagged, and keeps its slot. The
existing `TestChargeWiringRoutesAttemptsToTheWriter` covers the in-range end-to-end
path unchanged.

## Mutation

Revert the clamp and flag block in `internal/fit/loop.go` and the live failure
returns. The out-of-range line renders, placement refuses it, `assembleRun` fails the
run, and Persist never runs. `TestRunFlagsSegmentOutsideFilm` fails at the missing
`done` event and prints the pre-fix stream above.

Revert the typed refusal in `internal/assemble/place.go` to a plain error and
`TestPlaceRefusesClipOutsideBed` in `internal/assemble/place_test.go` fails, because
the error stops satisfying `errors.Is(err, ErrClipOutsideBed)`.

Delete the `placeTakes` recovery and a clip that slips past the loop validation fails
the whole run again. The loop validation currently stops such a clip before placement,
so only a future regression that reintroduces one would reach it.

Drop the `SourceDuration` wiring in `cmd/ajilamu/main.go` and the loop cannot validate
anything. The end-to-end test fails with the pre-fix stream.

## Files changed

- `internal/fit/loop.go`
- `internal/fit/loop_test.go`
- `internal/assemble/place.go`
- `internal/assemble/place_test.go`
- `cmd/ajilamu/main.go`
- `cmd/ajilamu/main_test.go`
- `dev-diary/adversarial-review/out-of-range-segment.md` (this record)

`internal/gemini/segment.go`, `internal/api/run.go`, and `internal/types/types.go` were
read and left unchanged. No commit was made. No container survives.

## Bar

`go build ./...`, `go vet ./...`, `go test -count=1 ./internal/... ./cmd/...`, and
`python3 tools/audit_docs.py` all pass. One run hit
`TestRecordActionWritesRecognizedTypes` in `internal/ledger` with a broken idle
connection, which is a flake in a package this fix does not touch. The package passed
on the next run, and the full bar passed clean afterwards.
