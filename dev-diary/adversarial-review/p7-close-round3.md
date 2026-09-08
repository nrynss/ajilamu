# P7 close: independent verification, round 3

**Scope:** The round 2 remediation of finding H1 in `internal/fit/loop.go` and `internal/fit/loop_test.go`, measured at committed tree `c7ab102` plus the uncommitted patch.
**Method:** Independent measurement in one scratch copy at `/tmp/p7r3/ajilamu`. The copy carries the worktree patch byte for byte. I did not write rounds 1 or 2 and I fixed nothing.
**Verdict:** REMEDIATE. Findings: 0 C, 1 H, 0 M, 0 L.

| C | H | M | L |
|---|---|---|---|
| 0 | 1 | 0 | 0 |

I own only this review file. I changed no production file. I removed the scratch copy after measurement.

## Findings

| Where | What | Pin | Mutation |
|---|---|---|---|
| `internal/fit/loop.go:974-975` | The cleanup resolves the two attempt-one take paths. `RepairLine` also writes the attempt-two and attempt-three takes before the record lands. An interruption there leaves one of those files with no record. The cleanup never resolves those paths, so the next run still fails on the orphan. | A throwaway probe seeded `seg_1_try2.wav` with no record. Two runs failed with `repair line 1: synthesize line 1 attempt 2: tts refuses to overwrite .../seg_1_try2.wav`. A `seg_1_try3.wav` seed failed at attempt 3 on the first run and at attempt 2 on the second. | Extending the cleanup to every attempt path makes both probes complete with `err=<nil>`. Removing those paths restores the permanent failure. Removing the whole discard call restores the round 1 raw-take block. |

### H1: an orphaned later-attempt take still blocks every later run

`repairSegment` resolves two paths, the attempt-one raw take and the attempt-one stretched take (`internal/fit/loop.go:974-975`). It hands both to `readSegmentRecord`. `discardOrphanTakes` removes a fresh path when no readable record at the line names it (`internal/fit/loop.go:261-271`).

`RepairLine` writes more than those two paths. Attempt two synthesizes to `seg_N_try2.wav`, and its atempo repair writes `seg_N_try2_stretched.wav` (`internal/fit/rewrite.go:318`, `internal/fit/rewrite.go:376`). Attempt three writes the `try3` names. `DefaultMaxAttempts` is 3 (`internal/fit/rewrite.go:19`). `repairSegment` writes the record only after `RepairLine` returns (`internal/fit/loop.go:990-994`).

An interruption after an attempt-two or attempt-three render leaves that take with no record. A cancel, a shutdown, or a crash in that window is enough. Round 1 measured the same window for attempt one. The work directory is never cleaned on failure (`internal/api/run.go:401-414` holds no removal).

The next run reads no record. It clears the attempt-one paths and renders attempt one fresh. If the line again needs a later attempt, the synthesizer refuses to overwrite the orphan. `refusingSynthesizer` mirrors the production client. The run fails and stays failed until an operator deletes the file.

The normal run engine sets no `PathBuilder`, so `NewPipeline` defaults to `PipelineTakeName` (`cmd/ajilamu/main.go:594`, `internal/fit/loop.go:709-711`). Production names the later takes `seg_N_try2.wav` and `seg_N_try3.wav`.

My probe measured the failure. The scenario needs a line whose first attempt misses the slot, so the loop rewrites and reaches attempt two.

```text
=== RUN   TestR3OrphanAttempt2TakeBlocksRun
    zz_r3_probe_test.go:52: run1: err=repair line 1: synthesize line 1 attempt 2: tts refuses to overwrite /tmp/.../seg_1_try2.wav synthWrites=1 orphanPresent=true
    zz_r3_probe_test.go:57: run2: err=repair line 1: synthesize line 1 attempt 2: tts refuses to overwrite /tmp/.../seg_1_try2.wav synthWrites=1 orphanPresent=true
--- PASS: TestR3OrphanAttempt2TakeBlocksRun (0.09s)
=== RUN   TestR3OrphanAttempt3TakeBlocksRun
    zz_r3_probe_test.go:70: run1: err=repair line 1: synthesize line 1 attempt 3: tts refuses to overwrite /tmp/.../seg_1_try3.wav synthWrites=2 orphanPresent=true
    zz_r3_probe_test.go:74: run2: err=repair line 1: synthesize line 1 attempt 2: tts refuses to overwrite /tmp/.../seg_1_try2.wav synthWrites=1 orphanPresent=true
--- PASS: TestR3OrphanAttempt3TakeBlocksRun (0.14s)
```

A candidate fix resolves every attempt path from the builder and passes the list. Both probes then complete.

```text
=== RUN   TestR3OrphanAttempt2TakeBlocksRun
    zz_r3_probe_test.go:52: run1: err=<nil> synthWrites=2 orphanPresent=true
    zz_r3_probe_test.go:57: run2: err=<nil> synthWrites=0 orphanPresent=true
=== RUN   TestR3OrphanAttempt3TakeBlocksRun
    zz_r3_probe_test.go:70: run1: err=<nil> synthWrites=3 orphanPresent=true
    zz_r3_probe_test.go:74: run2: err=<nil> synthWrites=0 orphanPresent=true
```

The second run writes nothing and resumes from the record. The orphan path then holds the fresh render, not the stale file. The existing pins still passed under the candidate.

Suggestion. Resolve every attempt path in `repairSegment`:

```go
	freshTakes := make([]string, 0, 2*DefaultMaxAttempts)
	for attempt := 1; attempt <= DefaultMaxAttempts; attempt++ {
		freshTakes = append(freshTakes,
			resolveTakePath(p.cfg.PathBuilder, p.cfg.WorkDir, seg.ID, attempt, false),
			resolveTakePath(p.cfg.PathBuilder, p.cfg.WorkDir, seg.ID, attempt, true))
	}

	if recorded, ok := readSegmentRecord(ctx, p.cfg.WorkDir, p.cfg.Language, seg, freshTakes...); ok {
```

## Review angle: an orphaned attempt-2 or attempt-3 take

I seeded `seg_1_try2.wav` and, in a second case, `seg_1_try3.wav` with no record. I ran the pipeline twice against each work directory.

The same permanent failure remains. Both seeds failed with `tts refuses to overwrite` on the orphan path. The second run repeated the failure. The cleanup never resolves those paths, so no later run clears them.

The finding rests on measurement, not on the remediation note. The round 2 record claims residue `None`. My measurement disagrees.

## Closed findings re-measured

| Finding | Path | Outcome |
|---|---|---|
| Round 1 H1, orphaned raw take | `seg_1_try1.wav` | Closed. The pin passes. |
| Round 2 H1, orphaned stretched take | `seg_1_stretched.wav` | Closed at attempt one. The pin passes. |

```text
=== RUN   TestPipelineDiscardsOrphanTakeWithoutRecord
    loop_test.go:1352: synth requests = 1, take duration = 2s
--- PASS: TestPipelineDiscardsOrphanTakeWithoutRecord (0.10s)
=== RUN   TestPipelineDiscardsOrphanStretchedTakeWithoutRecord
    loop_test.go:1422: synth requests = 1, chosen take = seg_1_stretched.wav, ratio = 1.0500, stretched duration = 1.97s
--- PASS: TestPipelineDiscardsOrphanStretchedTakeWithoutRecord (0.24s)
```

The duration is `media.Duration`, which shells out to `ffprobe` n9.0.1.

## Mutation of the round 2 fix

I removed the stretched path from the `readSegmentRecord` call in the scratch copy. The new pin failed with the round 2 error. The raw-take pin still passed, so the pin measures the stretched path alone.

```text
=== RUN   TestPipelineDiscardsOrphanTakeWithoutRecord
    loop_test.go:1352: synth requests = 1, take duration = 2s
--- PASS: TestPipelineDiscardsOrphanTakeWithoutRecord (0.09s)
=== RUN   TestPipelineDiscardsOrphanStretchedTakeWithoutRecord
    loop_test.go:1393: run after the orphaned takes failed: repair line 1: stretch take attempt 1: output take already exists: /tmp/.../seg_1_stretched.wav
--- FAIL: TestPipelineDiscardsOrphanStretchedTakeWithoutRecord (0.05s)
FAIL
```

## Residue against prior rounds

**Round 1 H1, a take without a resume record blocks resumption forever: open residue.** The attempt-one raw path is closed. The attempt-two and attempt-three raw paths stay outside the fix. My probe measured the permanent failure on `seg_1_try2.wav` and `seg_1_try3.wav`. Finding H1 records it.

**Round 2 H1, an orphaned stretched take blocks every later run: closed at attempt one, open at later attempts.** `TestPipelineDiscardsOrphanStretchedTakeWithoutRecord` passes and the mutation fails it. The names `seg_1_try2_stretched.wav` and `seg_1_try3_stretched.wav` stay outside the fix. They carry the same failure the round 2 pin measured.

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

The patch touches `internal/fit` alone. This round re-measured the resumability criterion, which the patch changes. Rounds 1 and 2 measured the other six.

| Criterion | Independent result |
|---|---|
| Interrupted runs remain resumable without data corruption | The attempt-one orphans resume. Finding H1 shows a later-attempt orphan still blocks every run. |

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
ok  	github.com/nrynss/ajilamu/internal/fit	17.046s
ok  	github.com/nrynss/ajilamu/internal/api	1.701s
ok  	github.com/nrynss/ajilamu/cmd/ajilamu	4.072s
```

Go reports 1.27.1. The copy matched the worktree files by MD5.

## Notes, not findings

The round 2 remediation record states residue `None`. My measurement finds the later-attempt residue. I record the disagreement.

The T7.3a handoff says rounds 1 to 3 each found one member of the resume class and round 5 returned APPROVE with zero residue (`dev-diary/PHASE-7-ingestion.md:806-808`). The later-attempt member stays outside every prior round.

A readable record that names the fresh take but describes another line or language still blocks the run. Round 2 recorded that state as pre-existing. The ownership guard keeps it on purpose.

The `internal/media` tests need gitignored `scratch/takes/` and `assets/source/`. A clean clone cannot run them. Rounds 1 and 2 recorded the same note.

I removed the throwaway probe and the scratch copy after measurement. I changed no production file.
