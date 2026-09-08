# P7 close: independent verification, round 4

**Scope:** The round 3 remediation of finding H1 in `internal/fit/loop.go` and `internal/fit/loop_test.go`, measured at committed tree `c7ab102` plus the uncommitted patch.
**Method:** Independent measurement in one scratch copy at `/tmp/p7r4/ajilamu`. The copy carries the two patched files byte for byte. My probes are throwaway and live only in the copy. I did not write rounds 1 to 3 and I fixed nothing.
**Verdict:** APPROVE. Findings: 0 C, 0 H, 0 M, 0 L.

| C | H | M | L |
|---|---|---|---|
| 0 | 0 | 0 | 0 |

I own only this review file. I changed no production file. I removed the scratch copy after measurement.

## Findings

| Where | What | Pin | Mutation |
|---|---|---|---|
| None | No finding met the bar. | The four orphan cases and the attempt-four probe pass. | The attempt-one-only mutation fails every orphan probe. |

The cleanup now resolves every path the fresh render can claim. It walks attempts one to `MaxAttempts`, and it takes the raw and stretched form of each (`internal/fit/loop.go:977-982`). `RepairLine` resolves the same paths through the same builder (`internal/fit/rewrite.go:250-259`, `internal/fit/loop.go:862-875`). The two sets match. The ownership guard and the live-context guard stay.

## The four orphan cases, re-measured

My probe seeded each orphan, ran the pipeline behind a synthesizer that refuses to overwrite, then ran it again behind a second refusing synthesizer. `media.Duration` shells out to ffprobe n9.0.1.

| Case | First run | Second run |
|---|---|---|
| try2 raw | err nil, 2 synth writes, chosen `seg_1_try2.wav`, 2s | err nil, 0 synth writes, chosen `seg_1_try2.wav` |
| try2 stretched | err nil, 2 synth writes, chosen `seg_1_try2_stretched.wav`, 1.97s | err nil, 0 synth writes, chosen `seg_1_try2_stretched.wav` |
| try3 raw | err nil, 3 synth writes, chosen `seg_1_try3.wav`, 2s | err nil, 0 synth writes, chosen `seg_1_try3.wav` |
| try3 stretched | err nil, 3 synth writes, chosen `seg_1_try3_stretched.wav`, 1.97s | err nil, 0 synth writes, chosen `seg_1_try3_stretched.wav` |

```text
run1 err=<nil> writes=2 chosen=seg_1_try2.wav dur=2s | run2 err=<nil> writes=0 chosen=seg_1_try2.wav
run1 err=<nil> writes=2 chosen=seg_1_try2_stretched.wav dur=1.97s | run2 err=<nil> writes=0 chosen=seg_1_try2_stretched.wav
run1 err=<nil> writes=3 chosen=seg_1_try3.wav dur=2s | run2 err=<nil> writes=0 chosen=seg_1_try3.wav
run1 err=<nil> writes=3 chosen=seg_1_try3_stretched.wav dur=1.97s | run2 err=<nil> writes=0 chosen=seg_1_try3_stretched.wav
```

The first run clears the orphan, renders fresh, and claims the seeded path. The second run resumes from the record and writes nothing.

## Review angle: a path neither prior round named

I probed attempt four. Rounds 1 to 3 named attempts one to three only. A pipeline configured with `PipelineConfig.MaxAttempts = 4` can claim `seg_1_try4.wav` and `seg_1_try4_stretched.wav`. `repairSegment` reads `cfg.MaxAttempts`, so the cleanup resolves attempts one to four.

```text
maxAttempts=4 run1 err=<nil> writes=4 chosen=seg_1_try4.wav dur=2s | run2 err=<nil> writes=0 chosen=seg_1_try4.wav
maxAttempts=4 stretched run1 err=<nil> writes=4 chosen=seg_1_try4_stretched.wav dur=1.97s | run2 err=<nil> writes=0 chosen=seg_1_try4_stretched.wav
```

Both cases complete on the first run and resume with zero writes on the second. The loop honours the configured bound, not the default three. No unnamed path remains.

I also probed the re-render route, because its takes carry no segment record. `nextTakeNumber` starts above the highest existing try number, so an orphaned re-render take at attempt two cannot block the next re-render.

```text
nextTakeNumber with try1 and try2 orphans = 3
```

## Mutation: the attempt-one-only cleanup

I restored the round 2 cleanup in the copy. The loop saw the attempt-one raw and stretched paths alone. All six orphan cases fail with the permanent error.

```text
try2 raw:                first run failed: repair line 1: synthesize line 1 attempt 2: tts refuses to overwrite .../seg_1_try2.wav
try2 stretched:          first run failed: repair line 1: stretch take attempt 2: output take already exists: .../seg_1_try2_stretched.wav
try3 raw:                first run failed: repair line 1: synthesize line 1 attempt 3: tts refuses to overwrite .../seg_1_try3.wav
try3 stretched:          first run failed: repair line 1: stretch take attempt 3: output take already exists: .../seg_1_try3_stretched.wav
attempt four:            first run failed: repair line 1: synthesize line 1 attempt 4: tts refuses to overwrite .../seg_1_try4.wav
attempt four stretched:  first run failed: repair line 1: stretch take attempt 4: output take already exists: .../seg_1_try4_stretched.wav
```

The second run of the try2 raw case also fails under the mutation. The orphan survives both runs.

```text
run2 err=repair line 1: synthesize line 1 attempt 2: tts refuses to overwrite .../seg_1_try2.wav writes=1
```

I restored the attempt walk and reran the probe. It passes.

## A take a valid record owns survives at every attempt

`TestReadSegmentRecordKeepsRecordOwnedTake` seeds the six attempt paths and a foreign record. The cleanup keeps the named path and clears the other five. My own probe repeats this for all six names, including `seg_1_try2_stretched.wav` and `seg_1_try3.wav`, which the shipped pin does not name.

```text
--- PASS: TestR4RecordOwnedTakeSurvivesEveryAttempt (0.00s)
    --- PASS: TestR4RecordOwnedTakeSurvivesEveryAttempt/seg_1_try1.wav (0.00s)
    --- PASS: TestR4RecordOwnedTakeSurvivesEveryAttempt/seg_1_stretched.wav (0.00s)
    --- PASS: TestR4RecordOwnedTakeSurvivesEveryAttempt/seg_1_try2.wav (0.00s)
    --- PASS: TestR4RecordOwnedTakeSurvivesEveryAttempt/seg_1_try2_stretched.wav (0.00s)
    --- PASS: TestR4RecordOwnedTakeSurvivesEveryAttempt/seg_1_try3.wav (0.00s)
    --- PASS: TestR4RecordOwnedTakeSurvivesEveryAttempt/seg_1_try3_stretched.wav (0.00s)
```

The shipped pins for the earlier rounds still pass.

```text
=== RUN   TestPipelineResumesAfterCompletedTake
--- PASS: TestPipelineResumesAfterCompletedTake (0.24s)
=== RUN   TestPipelineDiscardsOrphanTakeWithoutRecord
    loop_test.go:1352: synth requests = 1, take duration = 2s
--- PASS: TestPipelineDiscardsOrphanTakeWithoutRecord (0.09s)
=== RUN   TestPipelineDiscardsOrphanStretchedTakeWithoutRecord
    loop_test.go:1422: synth requests = 1, chosen take = seg_1_stretched.wav, ratio = 1.0500, stretched duration = 1.97s
--- PASS: TestPipelineDiscardsOrphanStretchedTakeWithoutRecord (0.24s)
```

## Residue against prior rounds

**Round 1 H1, a take without a resume record blocks resumption forever: closed.** The attempt-one raw path is inside the walk. `TestPipelineDiscardsOrphanTakeWithoutRecord` passes.

**Round 2 H1, an orphaned stretched take blocks every later run: closed.** The attempt-one stretched path is inside the walk. `TestPipelineDiscardsOrphanStretchedTakeWithoutRecord` passes.

**Round 3 H1, an orphaned later-attempt take still blocks every later run: closed.** The walk covers attempts two and three, raw and stretched. My probe measured all four cases. Each first run completes and each second run writes nothing.

Residue against rounds 1, 2, and 3: zero.

## Task status and owns

All 20 P7 task blocks read `status: done`. All 124 `owns` entries exist on disk. I resolved each entry against the worktree root.

| Task | Owns paths | Present |
|---|---|---|
| T7.0 | 7 | yes |
| T7.0a | 3 | yes |
| T7.1 | 3 | yes |
| T7.2 | 9 | yes |
| T7.2a | 7 | yes |
| T7.2b | 3 | yes |
| T7.2c1 | 4 | yes |
| T7.2c | 10 | yes |
| T7.2d | 1 | yes |
| T7.3 | 8 | yes |
| T7.3a | 2 | yes |
| T7.3b | 2 | yes |
| T7.3c | 23 | yes |
| T7.4 | 4 | yes |
| T7.4a | 4 | yes |
| T7.5a | 2 | yes |
| T7.5 | 7 | yes |
| T7.5b | 10 | yes |
| T7.7 | 10 | yes |
| T7.6 | 5 | yes |

I did not tick the phase exit boxes.

## Exit criteria

The patch touches `internal/fit` alone. This round re-measured the resumability criterion, which the patch changes.

| Criterion | Independent result |
|---|---|
| Interrupted runs remain resumable without data corruption | An orphaned take at any attempt one to `MaxAttempts`, raw or stretched, no longer blocks a later run. The cleanup clears it and the fresh render claims the path. |

## audit_docs.py

Command: `python3 tools/audit_docs.py`

Exit code: 0

```text
Ajilamu documentation drift audit


No drift. Docs and repository agree.
```

## Test command

Command: `go test -count=1 ./internal/fit ./internal/api ./cmd/...`

Exit code: 0

```text
ok  	github.com/nrynss/ajilamu/internal/fit	18.087s
ok  	github.com/nrynss/ajilamu/internal/api	1.634s
ok  	github.com/nrynss/ajilamu/cmd/ajilamu	4.032s
```

The worktree now carries sibling edits to `internal/api/rerender.go` and `cmd/ajilamu/main.go`. The run above included them and passed. I also ran the same command on the clean base copy, which carries the P7 patch and none of the sibling edits.

```text
ok  	github.com/nrynss/ajilamu/internal/fit	17.968s
ok  	github.com/nrynss/ajilamu/internal/api	1.626s
ok  	github.com/nrynss/ajilamu/cmd/ajilamu	3.907s
```

Go reports 1.27.1. ffprobe reports n9.0.1. The copy matched the worktree for both patched files by MD5.

## Notes, not findings

The patch clears every take at attempts one to `MaxAttempts` that no readable record at the line names. A re-render take lives in the ledger and not in a segment record. A later full run clears such a take when the segment record is missing, mismatched, or unusable. The re-render route numbers its next take above every existing take, so the clear cannot block it. Round 2 did not judge `internal/api/rerender.go`.

A readable record that names the fresh take but describes another line or language still blocks the run. The ownership guard keeps that take on purpose. The state predates this patch, and rounds 2 and 3 recorded it.

The `internal/media` tests need gitignored `scratch/takes/` and `assets/source/`. A clean clone cannot run them. Rounds 1 to 3 recorded the same note.

I removed the throwaway probes and the scratch copy after measurement. I changed no production file.
