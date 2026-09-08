# P7 close: independent verification, round 2

**Scope:** The round 1 remediation of finding H1 in `internal/fit/loop.go` and `internal/fit/loop_test.go`, measured at the committed tree `c7ab102` plus that patch.
**Method:** Independent measurement in two clean clones, `/tmp/p7r2/ajilamu` at `4a4747b` and `/tmp/p7r2b/ajilamu` at `c7ab102`, each carrying the two patched files. The patch is byte-identical in both. I did not judge the sibling's `internal/api/rerender.go`, which landed as `c7ab102` during this review.
**Verdict:** REMEDIATE. Findings: 0 C, 1 H, 0 M, 0 L.

| C | H | M | L |
|---|---|---|---|
| 0 | 1 | 0 | 0 |

I own only this review file. I did not write round 1 and I fixed nothing. I changed no production file.

## Findings

| Where | What | Pin | Mutation |
|---|---|---|---|
| `internal/fit/loop.go:963-968` | The orphan cleanup resolves one path, the raw take. A line that needs an atempo stretch keeps its orphaned stretched take. The next run clears the raw take, renders it fresh, then collides on the stretched path. The run stays failed until an operator deletes that file. | A throwaway test seeded `seg_1_try1.wav` at 1000 ms and `seg_1_stretched.wav` at 2000 ms with no record. `RunPipeline` failed with `repair line 1: stretch take attempt 1: output take already exists: .../seg_1_stretched.wav`. The synth wrote once and the stretched file survived. | Clearing the raw path alone leaves the stretch block, which the pin measures. Removing the discard call restores the raw-take block. |

### H1: an orphaned stretched take still blocks every later run

`repairSegment` clears the take that the fresh render claims. It resolves `freshTake` from `PathBuilder(seg.ID, 1, false)` at `internal/fit/loop.go:963-968`. It never resolves the stretched path. The discard call at `internal/fit/loop.go:277` receives that one path.

`RepairLine` writes the raw take and then the stretched take. It returns after both. `repairSegment` writes the record after that return (`internal/fit/loop.go:983-987`). An interruption after the stretch render leaves both files with no record. The patch clears the raw file only.

The next run clears the raw orphan and renders it fresh. `PlanStretchWithLimits` then plans an atempo repair. `StretchWithLimits` calls `claimOutput`, which opens the stretched path with `O_EXCL` and returns `ErrOutputExists` (`internal/fit/stretch.go:510-514`). The run fails and stays failed. Before the patch the raw take blocked the synthesizer, so a stretch line cannot resume either way. The remediation pin writes a take that matches the slot, so it plans no stretch and never reaches `claimOutput`.

Suggestion. Resolve both paths and let the ownership guard keep whichever path the record names:

```go
	freshTake := DefaultTakeName(seg.ID, 1, false)
	freshStretched := DefaultTakeName(seg.ID, 1, true)
	if cfg.PathBuilder != nil {
		freshTake = cfg.PathBuilder(seg.ID, 1, false)
		freshStretched = cfg.PathBuilder(seg.ID, 1, true)
	} else if cfg.WorkDir != "" {
		freshTake = filepath.Join(cfg.WorkDir, freshTake)
		freshStretched = filepath.Join(cfg.WorkDir, freshStretched)
	}
```

`readSegmentRecord` then needs both paths, and `discardOrphanTake` must compare each one against the take the record names.

## Round one residue

**Finding 1: open residue.** The pinned raw-take state is fixed. A take with no record still blocks every later run of a stretch line, because the cleanup clears one path only. My pin measured the failure and the surviving stretched file.

Round one also described the mismatched-record path as never discarding. That path still blocks when a readable record names the fresh take but describes another line or language. I measured both variants. The ownership guard keeps such a take on purpose and the remediation pins that behavior. So I record it as a pre-existing note rather than a finding. Task round 4 measured the same collision.

## Task status and owns

Every P7 task reads `status: done`. Every owns path exists on disk. I resolved all 124 owns entries from the twenty task blocks and each one exists.

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
| T7.6 | 5 | yes |
| T7.7 | 10 | yes |

The phase document carries a second set of `### T7.x` headings in its Handoff Log. Those are prose notes and hold no yaml block.

I did not tick the phase exit boxes.

## Exit criteria

The phase document lists seven checkboxes. I re-measured each one.

| Criterion | Independent result |
|---|---|
| The server runs from a clone with no credentials and serves the workspace from fixtures | I ran the binary with `env -i`. It answered 200 for `/`, `/new`, `/config`, `/d/fixture`, `/api/healthz`, `/api/languages`, and `/api/dubs`. It answered 503 `misconfigured` naming `CLICKHOUSE_HOST` for `/api/ledger/ready`. |
| Shutdown flushes the ledger queue without losing events | `TestShutdownFlushesDurableLedgerQueue`, `TestShutdownFlushesLedgerWhenDrainExpires`, `TestShutdownCancelsRunningPipeline`, and `TestEntrypointExitsCleanlyWithoutLedger` pass. |
| Large video files stream directly to storage without memory bloat | A 512 MiB upload returned 201 in 0.86 s. Server RSS stayed between 27728 and 28880 KiB across 7437 samples. |
| Users can upload optional background music tracks | An upload with `video` and `music` returned 201. Both files landed at mode 0600 in a 0700 directory. The record carried `en-US` to `ml-IN`. |
| Sample mode launches instantly with one click | `POST /api/dubs/sample` returned 201 in 0.010 s and copied the committed clip. |
| Progress events stream complete sentences with running costs | `TestProgressEventsCarrySentenceAndCost`, `TestEventsStreamFramesWithIDsAndCost`, and `TestEventsReconnectKeepsCumulativeCost` pass. |
| Interrupted runs remain resumable without data corruption | The fit suite passes and my orphan probe resumes a raw-take orphan. Finding H1 shows a stretch line still cannot resume. |

## Independent measurements

I built and ran both clones. Go reports 1.27.1. `ffprobe` reports n9.0.1. `go build ./...` passed in both.

### The orphan-take fix

My own probe seeded `seg_1_try1.wav` at 1000 ms with no record and ran the pipeline twice. The first run wrote the take once and measured 2000 ms. The second run wrote nothing and resumed from the record.

```text
seed take duration = 1s, record present = false
first run: synth writes = 1, chosen take = .../seg_1_try1.wav, duration = 2s
second run: synth writes = 0, chosen take = .../seg_1_try1.wav, duration = 2s
```

The committed pin reports the same counts.

```text
=== RUN   TestPipelineDiscardsOrphanTakeWithoutRecord
    loop_test.go:1352: synth requests = 1, take duration = 2s
--- PASS: TestPipelineDiscardsOrphanTakeWithoutRecord (0.09s)
```

### Mutation of the discard call

I removed the `discardOrphanTake` call in the missing-record branch at `internal/fit/loop.go:277`. The committed pin and my probe both failed with the round one error.

```text
--- FAIL: TestPipelineDiscardsOrphanTakeWithoutRecord (0.00s)
    loop_test.go:1332: run after the orphaned take failed: repair line 1: synthesize line 1 attempt 1: tts refuses to overwrite .../seg_1_try1.wav
--- FAIL: TestR2ProbeOrphanTakeThenResume (0.05s)
    zz_r2_probe_test.go:76: first run after the orphan take failed: repair line 1: synthesize line 1 attempt 1: probe refuses to overwrite .../seg_1_try1.wav
```

### Mutation of the ownership guard

I changed the guard to `if ctx.Err() != nil || freshTake == ""`. The protection pin failed on both cases. The orphan pin still passed.

```text
--- FAIL: TestReadSegmentRecordKeepsRecordOwnedTake (0.00s)
    --- FAIL: TestReadSegmentRecordKeepsRecordOwnedTake/another_language (0.00s)
    loop_test.go:1410: orphan cleanup removed a take the record owns: stat .../seg_1_try1.wav: no such file or directory
    --- FAIL: TestReadSegmentRecordKeepsRecordOwnedTake/another_segment (0.00s)
    loop_test.go:1410: orphan cleanup removed a take the record owns: stat .../seg_1_try1.wav: no such file or directory
```

### A record-owned take survives

`TestReadSegmentRecordKeepsRecordOwnedTake` passes for a record for another language and for a record for another segment. The take stays on disk in both cases.

### The stretch block

My probe seeded both an orphaned raw take and an orphaned stretched take. The line needs an atempo repair after the fresh render.

```text
pipeline stretched name = seg_1_stretched.wav
run error = repair line 1: stretch take attempt 1: output take already exists: .../seg_1_stretched.wav, synth writes = 1, stretched still present = true
```

My stale-record probe seeded a readable record for the same language with a different segment, and a readable record for another language with the same segment. Each run failed at the synthesizer.

```text
same language, different segment: run error = repair line 1: synthesize line 1 attempt 1: probe refuses to overwrite .../seg_1_try1.wav, synth writes = 0
another language, same segment: run error = repair line 1: synthesize line 1 attempt 1: probe refuses to overwrite .../seg_1_try1.wav, synth writes = 0
```

## Test command

Command, run in both clones with the patch applied and no review file in the tree:

```text
$ go test -count=1 ./internal/fit ./internal/api ./cmd/...   # c7ab102 plus the patch
ok  	github.com/nrynss/ajilamu/internal/fit	16.660s
ok  	github.com/nrynss/ajilamu/internal/api	1.509s
ok  	github.com/nrynss/ajilamu/cmd/ajilamu	3.892s

$ go test -count=1 ./internal/fit ./internal/api ./cmd/...   # 4a4747b plus the patch
ok  	github.com/nrynss/ajilamu/internal/fit	16.824s
ok  	github.com/nrynss/ajilamu/internal/api	1.422s
ok  	github.com/nrynss/ajilamu/cmd/ajilamu	4.048s
```

Exit code 0. `go vet ./internal/fit` passed and `gofmt -l` named neither patched file.

## audit_docs.py

Command: `python3 tools/audit_docs.py`

Exit code: 0

```text
Ajilamu documentation drift audit


No drift. Docs and repository agree.
```

I ran it in the worktree, in the `4a4747b` clone, and in the `c7ab102` clone. All three returned 0.

## Notes, not findings

A readable record that names the fresh take but describes another line or language blocks the run. The guard keeps that take on purpose. The state predates this patch and task round 4 measured the same collision.

The `internal/media` tests need gitignored `scratch/takes/` and `assets/source/`. A clean clone cannot run them. Round 1 recorded the same note.

I removed every throwaway probe and both clones after measurement. I changed no production code.
