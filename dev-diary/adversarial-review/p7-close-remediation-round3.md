# P7 close remediation, round 3

I did not change the round 3 verdict. I fixed finding H1 and nothing else.

I edited `internal/fit/loop.go` and `internal/fit/loop_test.go`. I did not commit.

| Finding | Fix | Pin | Residue |
|---|---|---|---|
| H1, an orphaned later-attempt take still blocks every later run | `repairSegment` now resolves every take path the fresh render can claim. It walks attempts 1 to `MaxAttempts` (`DefaultMaxAttempts` when unset) and takes the raw and stretched form of each. `readSegmentRecord` receives all of them, so `discardOrphanTakes` clears a `seg_N_try2.wav`, a `seg_N_try2_stretched.wav`, a `seg_N_try3.wav`, or a `seg_N_try3_stretched.wav` left without a record. The ownership guard and the live-context guard stay. | `TestPipelineDiscardsOrphanLaterAttemptTakesWithoutRecord` seeds one orphan at attempt two and one at attempt three, raw and stretched. Each case runs twice behind a synthesizer that refuses to overwrite. The first run completes, and the second run resumes with zero synth writes. `TestReadSegmentRecordKeepsRecordOwnedTake` now passes all six attempt paths and keeps the take a record names. | None. |

## Acceptance 1: an orphaned `seg_1_try2.wav`

The first run completes. The second run resumes from the record and writes nothing.

```text
=== RUN   TestPipelineDiscardsOrphanLaterAttemptTakesWithoutRecord/attempt_two_raw
    loop_test.go:2302: first run synth writes = 2, second run synth writes = 0, chosen take = seg_1_try2.wav
--- PASS: TestPipelineDiscardsOrphanLaterAttemptTakesWithoutRecord/attempt_two_raw (0.14s)
```

The first run needed two attempts, so the line claimed `seg_1_try2.wav`. The cleanup cleared the seed, and the fresh render claimed the path. The record then names that take.

## Acceptance 2: an orphaned `seg_1_try3.wav`

```text
=== RUN   TestPipelineDiscardsOrphanLaterAttemptTakesWithoutRecord/attempt_three_raw
    loop_test.go:2302: first run synth writes = 3, second run synth writes = 0, chosen take = seg_1_try3.wav
--- PASS: TestPipelineDiscardsOrphanLaterAttemptTakesWithoutRecord/attempt_three_raw (0.19s)
```

The first run needed three attempts, so the line claimed `seg_1_try3.wav`. The second run writes nothing.

The stretched forms pass on the same terms.

```text
=== RUN   TestPipelineDiscardsOrphanLaterAttemptTakesWithoutRecord/attempt_two_stretched
    loop_test.go:2302: first run synth writes = 2, second run synth writes = 0, chosen take = seg_1_try2_stretched.wav
--- PASS: TestPipelineDiscardsOrphanLaterAttemptTakesWithoutRecord/attempt_two_stretched (0.28s)
=== RUN   TestPipelineDiscardsOrphanLaterAttemptTakesWithoutRecord/attempt_three_stretched
    loop_test.go:2302: first run synth writes = 3, second run synth writes = 0, chosen take = seg_1_try3_stretched.wav
--- PASS: TestPipelineDiscardsOrphanLaterAttemptTakesWithoutRecord/attempt_three_stretched (0.33s)
```

## Acceptance 3: the mutation that fails the pin

I replaced the attempt walk with the round 2 attempt-one list, so the cleanup saw two paths again. All four cases fail with the permanent error the finding measured.

```text
    loop_test.go:2266: first run after the orphaned seg_1_try2.wav failed: repair line 1: synthesize line 1 attempt 2: tts refuses to overwrite /tmp/.../seg_1_try2.wav
    loop_test.go:2266: first run after the orphaned seg_1_try2_stretched.wav failed: repair line 1: stretch take attempt 2: output take already exists: /tmp/.../seg_1_try2_stretched.wav
    loop_test.go:2266: first run after the orphaned seg_1_try3.wav failed: repair line 1: synthesize line 1 attempt 3: tts refuses to overwrite /tmp/.../seg_1_try3.wav
    loop_test.go:2266: first run after the orphaned seg_1_try3_stretched.wav failed: repair line 1: stretch take attempt 3: output take already exists: /tmp/.../seg_1_try3_stretched.wav
--- FAIL: TestPipelineDiscardsOrphanLaterAttemptTakesWithoutRecord (0.38s)
    --- FAIL: TestPipelineDiscardsOrphanLaterAttemptTakesWithoutRecord/attempt_two_raw (0.05s)
    --- FAIL: TestPipelineDiscardsOrphanLaterAttemptTakesWithoutRecord/attempt_two_stretched (0.09s)
    --- FAIL: TestPipelineDiscardsOrphanLaterAttemptTakesWithoutRecord/attempt_three_raw (0.09s)
    --- FAIL: TestPipelineDiscardsOrphanLaterAttemptTakesWithoutRecord/attempt_three_stretched (0.14s)
FAIL
```

I restored the walk and reran the pin. It passes.

## Acceptance 4: a take a valid record owns survives

`TestReadSegmentRecordKeepsRecordOwnedTake` now seeds all six attempt paths and writes a foreign record. The cleanup keeps the path the record names and clears the other five. It covers a later raw take and a later stretched take.

```text
=== RUN   TestReadSegmentRecordKeepsRecordOwnedTake
=== RUN   TestReadSegmentRecordKeepsRecordOwnedTake/another_language_raw_take
=== RUN   TestReadSegmentRecordKeepsRecordOwnedTake/another_segment_raw_take
=== RUN   TestReadSegmentRecordKeepsRecordOwnedTake/another_language_stretched_take
=== RUN   TestReadSegmentRecordKeepsRecordOwnedTake/another_language_later_raw_take
=== RUN   TestReadSegmentRecordKeepsRecordOwnedTake/another_language_later_stretched_take
--- PASS: TestReadSegmentRecordKeepsRecordOwnedTake (0.00s)
    --- PASS: TestReadSegmentRecordKeepsRecordOwnedTake/another_language_raw_take (0.00s)
    --- PASS: TestReadSegmentRecordKeepsRecordOwnedTake/another_segment_raw_take (0.00s)
    --- PASS: TestReadSegmentRecordKeepsRecordOwnedTake/another_language_stretched_take (0.00s)
    --- PASS: TestReadSegmentRecordKeepsRecordOwnedTake/another_language_later_raw_take (0.00s)
    --- PASS: TestReadSegmentRecordKeepsRecordOwnedTake/another_language_later_stretched_take (0.00s)
```

The second run of the new pin also confirms a valid record keeps its take. That run resumes from the record and leaves the named take in place.

## Acceptance 5: the package suite

```text
$ go test -count=1 ./internal/fit
ok  	github.com/nrynss/ajilamu/internal/fit	18.195s
```

The two prior orphan pins still pass.

```text
=== RUN   TestPipelineDiscardsOrphanTakeWithoutRecord
    loop_test.go:1352: synth requests = 1, take duration = 2s
--- PASS: TestPipelineDiscardsOrphanTakeWithoutRecord (0.10s)
=== RUN   TestPipelineDiscardsOrphanStretchedTakeWithoutRecord
    loop_test.go:1422: synth requests = 1, chosen take = seg_1_stretched.wav, ratio = 1.0500, stretched duration = 1.97s
--- PASS: TestPipelineDiscardsOrphanStretchedTakeWithoutRecord (0.23s)
```

`gofmt -l` reports no file for either edited file.

I did not tick any PHASE-7 exit box. I did not edit the round 3 review file.
