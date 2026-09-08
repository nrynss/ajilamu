# P7 close remediation, round 2

I did not change the round 2 verdict. I fixed finding H1 and nothing else.

I edited `internal/fit/loop.go` and `internal/fit/loop_test.go`. I did not commit.

| Finding | Fix | Pin | Residue |
|---|---|---|---|
| H1, an orphaned stretched take blocks every later run | `repairSegment` resolves both take paths the first attempt claims, the raw take and the stretched take. `readSegmentRecord` receives both and hands them to `discardOrphanTakes`, which removes each path that no readable record at the line names. The ownership guard keeps whichever path the record names, so a take a valid record owns survives. The live-context guard stays. | `TestPipelineDiscardsOrphanStretchedTakeWithoutRecord` seeds `seg_1_try1.wav` and `seg_1_stretched.wav` with no record, then runs behind a synthesizer that refuses to overwrite. The run completes, the synth renders once, and the stretch lands inside the dead band. `TestReadSegmentRecordKeepsRecordOwnedTake` now covers a stretched take owned by a record for another language. | None. |

## Finding 1 measurements

The line claims two paths before its record lands, so the cleanup clears both. A 100 ms overrun sits inside the long budget, so the line plans atempo.

```text
=== RUN   TestPipelineDiscardsOrphanStretchedTakeWithoutRecord
    loop_test.go:1422: synth requests = 1, chosen take = seg_1_stretched.wav, ratio = 1.0500, stretched duration = 1.97s
--- PASS: TestPipelineDiscardsOrphanStretchedTakeWithoutRecord (0.24s)
```

The run outcome is success. The synth call count is 1. The stretch outcome is atempo at ratio 1.0500, and the chosen take is the stretched path. `media.Duration` shells out to `ffprobe` for the 1.97s measurement. The dead band is 40 ms, so the landing sits inside it.

The raw-take pin from round one still passes.

```text
=== RUN   TestPipelineDiscardsOrphanTakeWithoutRecord
    loop_test.go:1352: synth requests = 1, take duration = 2s
--- PASS: TestPipelineDiscardsOrphanTakeWithoutRecord (0.10s)
```

A take a valid record owns survives, including a record for another language that names the stretched take.

```text
=== RUN   TestReadSegmentRecordKeepsRecordOwnedTake
=== RUN   TestReadSegmentRecordKeepsRecordOwnedTake/another_language_raw_take
=== RUN   TestReadSegmentRecordKeepsRecordOwnedTake/another_segment_raw_take
=== RUN   TestReadSegmentRecordKeepsRecordOwnedTake/another_language_stretched_take
--- PASS: TestReadSegmentRecordKeepsRecordOwnedTake (0.00s)
    --- PASS: TestReadSegmentRecordKeepsRecordOwnedTake/another_language_raw_take (0.00s)
    --- PASS: TestReadSegmentRecordKeepsRecordOwnedTake/another_segment_raw_take (0.00s)
    --- PASS: TestReadSegmentRecordKeepsRecordOwnedTake/another_language_stretched_take (0.00s)
```

The valid-record resume path still passes.

```text
=== RUN   TestPipelineReplaysRecordedStretchedTake
--- PASS: TestPipelineReplaysRecordedStretchedTake (0.05s)
```

## Mutation

I removed the stretched path from the `readSegmentRecord` call in `repairSegment`, so the cleanup saw the raw take alone. This restores the round one behaviour.

```go
	if recorded, ok := readSegmentRecord(ctx, p.cfg.WorkDir, p.cfg.Language, seg, freshTake); ok {
```

The new pin fails with the error the finding measured. The raw-take pin still passes, so the new pin measures the stretched path alone.

```text
=== RUN   TestPipelineDiscardsOrphanTakeWithoutRecord
    loop_test.go:1352: synth requests = 1, take duration = 2s
--- PASS: TestPipelineDiscardsOrphanTakeWithoutRecord (0.09s)
=== RUN   TestPipelineDiscardsOrphanStretchedTakeWithoutRecord
    loop_test.go:1393: run after the orphaned takes failed: repair line 1: stretch take attempt 1: output take already exists: /tmp/TestPipelineDiscardsOrphanStretchedTakeWithoutRecord1198140069/001/seg_1_stretched.wav
--- FAIL: TestPipelineDiscardsOrphanStretchedTakeWithoutRecord (0.05s)
FAIL
```

## Suite

```text
$ go test -count=1 ./internal/fit
ok  	github.com/nrynss/ajilamu/internal/fit	16.729s
```

`gofmt -l` reports no file for either edited file.

I did not tick any PHASE-7 exit box. I did not edit the round 2 review file.
