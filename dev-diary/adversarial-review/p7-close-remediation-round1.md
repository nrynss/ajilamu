# P7 close remediation, round 1

I did not change the round 1 verdict. I fixed finding H1 and nothing else.

I edited `internal/fit/loop.go` and `internal/fit/loop_test.go`. I did not commit.

| Finding | Fix | Pin | Residue |
|---|---|---|---|
| H1, a take without a resume record blocks resumption forever | `readSegmentRecord` receives the take the fresh render will claim. It removes that path when no readable record at the line names it. It keeps the path when a readable record names it, even a record for another language or segment. `repairSegment` resolves the fresh path and passes it. The live-context guard from round 3 stays. | `TestPipelineDiscardsOrphanTakeWithoutRecord` seeds `seg_1_try1.wav` with no record and runs behind a synthesizer that refuses to overwrite. The run completes, synth calls are 1, and ffprobe measures 2000ms. `TestReadSegmentRecordKeepsRecordOwnedTake` keeps the take a record owns for another language and for another segment. | None. |

## Finding 1 measurements

A take with no record no longer blocks the run. The next run clears it, renders
fresh, and completes. The synthesizer refuses to overwrite, so a missing clear
fails the run.

```text
=== RUN   TestPipelineDiscardsOrphanTakeWithoutRecord
    loop_test.go:1352: synth requests = 1, take duration = 2s
--- PASS: TestPipelineDiscardsOrphanTakeWithoutRecord (0.09s)
```

The duration is `media.Duration`, which shells out to `ffprobe`.

A take with a valid record for another language is not deleted. A take a record
owns for another segment is not deleted either.

```text
=== RUN   TestReadSegmentRecordKeepsRecordOwnedTake
=== RUN   TestReadSegmentRecordKeepsRecordOwnedTake/another_language
=== RUN   TestReadSegmentRecordKeepsRecordOwnedTake/another_segment
--- PASS: TestReadSegmentRecordKeepsRecordOwnedTake (0.00s)
    --- PASS: TestReadSegmentRecordKeepsRecordOwnedTake/another_language (0.00s)
    --- PASS: TestReadSegmentRecordKeepsRecordOwnedTake/another_segment (0.00s)
```

The resume tests still pass.

```text
$ go test -count=1 ./internal/fit
ok  	github.com/nrynss/ajilamu/internal/fit	16.737s
```

`go test -count=1 -v -run 'Resume|Replay|Rerender|Cancel|Deadline' ./internal/fit`
passed every resume, replay, re-render, cancel, and deadline test.

## Mutation

I replaced `loop.go` through a Go overlay and removed the clear from the
missing-record branch. The overlay left the repository untouched.

```go
	data, err := os.ReadFile(filepath.Join(workDir, segmentRecordName(seg.ID)))
	if err != nil {
		return LineResult{}, false
	}
```

The pin fails with the error from the finding.

```text
=== RUN   TestPipelineDiscardsOrphanTakeWithoutRecord
    loop_test.go:1332: run after the orphaned take failed: repair line 1: synthesize line 1 attempt 1: tts refuses to overwrite /home/nryn/.cache/remp7-tmp/TestPipelineDiscardsOrphanTakeWithoutRecord3420134422/001/seg_1_try1.wav
--- FAIL: TestPipelineDiscardsOrphanTakeWithoutRecord (0.00s)
FAIL
```

The second mutation drops the ownership check, so the clear removes every
fresh path. The protection pin fails on both cases.

```go
	if ctx.Err() != nil || freshTake == "" {
```

```text
=== RUN   TestReadSegmentRecordKeepsRecordOwnedTake
=== RUN   TestReadSegmentRecordKeepsRecordOwnedTake/another_language
    loop_test.go:1410: orphan cleanup removed a take the record owns: stat .../seg_1_try1.wav: no such file or directory
=== RUN   TestReadSegmentRecordKeepsRecordOwnedTake/another_segment
    loop_test.go:1410: orphan cleanup removed a take the record owns: stat .../seg_1_try1.wav: no such file or directory
--- FAIL: TestReadSegmentRecordKeepsRecordOwnedTake (0.00s)
FAIL
```

## Validation

`gofmt -l` reported nothing for `internal/fit/loop.go` and `internal/fit/loop_test.go`.
`go vet ./internal/fit` passed.

I did not tick any PHASE-7 exit box. I did not edit the round 1 review file.
