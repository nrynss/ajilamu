# Retry after a failure

Task: make a failed run retryable.
Branch: master, HEAD 5a3bc50, no commit.
Date: 2026-09-10.

## The mechanism

Dub `19f8d87716f274fe629a3c4bfd69441d` ran on the deployed host. The deployed revision was
`5a3bc5069afae5c81e2ce3c809b46b3a0673d8d0`, which is this HEAD. The journal carried the same
failure three times:

```
18:02:16 ERROR run failed ... stage=repairing error="repair line 1: synthesize line 1 attempt 1: tts refuses to overwrite /data/storage/ajilamu/uploads/19f8d87716f274fe629a3c4bfd69441d/work/de-DE/seg_1_try1.wav"
18:02:55 ERROR run failed ... stage=repairing error="repair line 1: synthesize line 1 attempt 1: tts refuses to overwrite .../seg_1_try1.wav"
18:20:58 ERROR run failed ... stage=repairing error="repair line 1: synthesize line 1 attempt 1: tts refuses to overwrite .../seg_1_try1.wav"
```

The run before the first failure stopped on line 5 with a Vertex 429 at `18:01:16`. The loop
renders lines in order, so that run finished lines 1 to 4. Each finished line wrote a resume
record and its take. The retry asked the segmenter again. Vertex returned the same line id
under a changed segment for line 1.

`readSegmentRecord` reads `seg_1_result.json`. It reports false when the record names another
language or another segment. That branch keeps the take the record names, because the record
owns it. The loop then renders the line fresh. `RepairLine` resolves attempt one to
`seg_1_try1.wav`. `chirpSynthesizer.Synthesize` calls `refuseOverwrite` first and returns
`tts refuses to overwrite`. The run fails at once and stays failed. A record lands only after
its line finishes, so no retry can clear the block.

The orphan cleanup already clears a take with no record. It cannot clear this take, because a
readable record names it. The loop had no path that both keeps the file and renders the line.
The task brief said the record did not exist. The code path proves the sharper cause. A missing
or unreadable record clears the take, so the live failure needed a readable record that no
longer described the line.

I measured the defect on this HEAD with a throwaway test before the fix. The second and third
passes died on the same path.

```
pass 2 err = repair line 1: synthesize line 1 attempt 1: tts refuses to overwrite .../seg_1_try1.wav (synth writes = 0)
pass 3 err = repair line 1: synthesize line 1 attempt 1: tts refuses to overwrite .../seg_1_try1.wav (synth writes = 0)
```

The live run only completed after an operator cleared the work directory by hand. The directory
`work/de-DE` carries the mtime `18:21:32`, so the retry recreated it. The dub then reached
`readiness: review` with 7 lines and 1 commit. The fix removes that manual step.

## The fix

A fresh render claims the first free attempt window. It never writes over a take file that
already exists, so a file the ledger references keeps its bytes.

- `internal/fit/rewrite.go` adds `RewriteConfig.FirstAttempt`. `RepairLine` numbers its
  attempts from that value and resolves the take paths from the same number. The mode choice,
  the charge attempt, the record attempt, and the file try number stay aligned.
- `internal/fit/loop.go` adds `firstFreeAttempt`, `attemptWindowFree`, and `takePathExists`.
  The scan returns the first attempt whose whole window holds no raw and no stretched take.
  `repairSegment` sets `FirstAttempt` when no usable record resumes the line.
- A line with a usable record resumes exactly as before. `reusableTake` still hands an
  attempt-one take to `RepairLine` as the initial take. A record the loop cannot consume still
  replays.
- The cleanup and the writer guard are unchanged. The cleanup keeps the take a readable record
  names. `chirpSynthesizer.refuseOverwrite` still refuses a direct write onto a take.

## The failing run and the successful retry

`TestRunRetriesAfterStaleRecordTake` runs the real `pipelineRunner`, the real
`chirpSynthesizer` over a stub Cloud TTS client, the real API server, and real ffmpeg. The
recorder writes its rows into a stand-in ClickHouse, which captures every insert. The first
run renders line 1 under the text `source one`. The test then changes the segmenter to return
`source one, reworded` under the same line id.

The first run stream ends clean.

```
data: {"type":"progress","stage":"synthesizing","sentence":"Rendering line 1 in Malayalam.","segment_id":1,"language":"ml-IN","take_file":"seg_1_try1.wav"}
data: {"type":"done","stage":"measuring","sentence":"Dubbing pipeline completed successfully.","total_nanodollars":981000,"language":"ml-IN"}
```

The retry stream ends clean. The fresh render claims the next free window.

```
data: {"type":"progress","stage":"synthesizing","sentence":"Rendering line 1 in Malayalam.","segment_id":1,"language":"ml-IN","take_file":"seg_1_try2.wav"}
data: {"type":"done","stage":"measuring","sentence":"Dubbing pipeline completed successfully.","total_nanodollars":981000,"language":"ml-IN"}
main_test.go: ffprobe export duration = 2.000s
main_test.go: persisted 2 take rows, the stale take stays referenced, Cloud TTS calls = 2
```

The test reads `seg_1_try1.wav` before and after the retry and compares the bytes. They match.
It also scans the captured `takes_raw` inserts. One row references `seg_1_try1.wav`, and the
retry adds the row for `seg_1_try2.wav`.

## Pins

Every pin is an independent measurement. The commands were
`go test -count=1 -run <name> ./internal/fit/ ./internal/tts/ ./cmd/ajilamu/`.

| Where | What | Pin | Mutation |
|---|---|---|---|
| `internal/fit/loop_test.go` `TestPipelineRendersFreshBesideStaleRecordTake` | a changed segment under the same line id, then a repeat run | second pass renders `seg_1_try2.wav`, keeps `seg_1_try1.wav` byte for byte, third pass makes 0 synth requests | disabling the free scan fails with `repair line 1: synthesize line 1 attempt 1: tts refuses to overwrite .../seg_1_try1.wav` |
| `internal/fit/loop_test.go` `TestFirstFreeAttemptSkipsClaimedRawAndStretchedPaths` | empty dir, raw take at two, stretched take at one, stretched take at three | first free attempt = 1, 3, 2, 4 | a raw-only scan answers 1 for both stretched cases |
| `cmd/ajilamu/main_test.go` `TestRunRetriesAfterStaleRecordTake` | a real server, real ffmpeg, the production synthesizer, a changed segment | first run `done`, retry `done` with no `error` event, 1 fresh Cloud TTS call, `seg_1_try2.wav` beside an untouched `seg_1_try1.wav`, 2 take rows with the stale path referenced, export 2.000s | the free-scan mutation makes the retry emit `{"type":"error",...}` and logs `run failed` |
| `internal/fit/loop_test.go` `TestPipelineResumesAfterCompletedTake` | a completed line, then a second pass | the second pass renders lines 2 and 3 alone and line 1 keeps the first pass take | ignoring the record renders line 1 again, `resumed synth requests = [1 1 1 2 3]` |
| `internal/tts/chirp_test.go` `TestSynthesizeRefusesOverwriteWithoutAPICall` | a take already at the path | the writer refuses, calls no API, records no charge, leaves the bytes | removing the guard calls Cloud TTS once and fails on the exclusive create |

The resume call count appears in the first row and the fourth row. A completed line makes zero
render calls on the repeat pass, and an ignored record makes five.

## Mutation

I disabled the free scan with `firstFreeAttempt` returning one. The fit pin failed with the
live error.

```
--- FAIL: TestPipelineRendersFreshBesideStaleRecordTake (0.05s)
    loop_test.go:1405: run after the stale record failed: repair line 1: synthesize line 1 attempt 1: tts refuses to overwrite /tmp/.../seg_1_try1.wav
```

The end-to-end pin failed the same way. The retry stream carried the error terminal event, and
the server log reported a failed run.

```
data: {"type":"progress","stage":"synthesizing","sentence":"Rendering line 1 in Malayalam.","segment_id":1,"language":"ml-IN","take_file":"seg_1_try1.wav"}
data: {"type":"error","stage":"measuring","sentence":"Unexpected error occurred during pipeline execution.","language":"ml"}
FAIL
```

I also dropped the stretched half of the window check. The unit pin failed on both stretched
rows.

```
    loop_test.go:1477: firstFreeAttempt = 1, want 2
    loop_test.go:1477: firstFreeAttempt = 1, want 4
```

I also made `readSegmentRecord` report false for every line. The resume pins failed.

```
--- FAIL: TestPipelineResumesAfterCompletedTake (0.30s)
    loop_test.go:1337: resumed synth requests = [1 1 1 2 3], want [2 3]
--- FAIL: TestPipelineRendersFreshBesideStaleRecordTake (0.15s)
    loop_test.go:1438: resumed synth requests = 1, want 0
```

I also removed the writer guard. The refusal pin failed. The exclusive create still stopped
the write, so the mutation cost an API call rather than a lost take.

```
    chirp_test.go:258: error = open /tmp/.../seg_1_try1.wav: file exists, want overwrite refusal
    chirp_test.go:261: overwrite path called SynthesizeSpeech 1 times
```

I restored each mutation and reran the pins. All passed.

## Verification

- `go build ./...` passed.
- `go vet ./...` passed.
- `go test -count=1 ./internal/... ./cmd/...` passed, every package ok.
- `python3 tools/audit_docs.py` reported `No drift. Docs and repository agree.` with exit 0.
- `git status --porcelain` lists the four changed files and this record alone.
- No commit. I started no local server and no container. The end-to-end pin runs in process
  over `httptest`. I read the deployed host over `gcloud compute ssh` and the run route over
  HTTPS, and neither call changed the host.

## Files changed

- `internal/fit/rewrite.go`. The `FirstAttempt` field and the attempt numbering in `RepairLine`.
- `internal/fit/loop.go`. The free window scan and the `FirstAttempt` decision in
  `repairSegment`.
- `internal/fit/loop_test.go`. The stale-record regression pin and the window unit pin.
- `cmd/ajilamu/main_test.go`. The end-to-end retry pin over the real server and the real
  synthesizer.
- `dev-diary/adversarial-review/retry-after-failure.md`. This record.

## Contract notes

- No path outside the allowed set changed. `internal/api/run.go`, `cmd/ajilamu/main.go`, and
  `internal/ledger/workspace.go` needed no edit, because the seam lives in `internal/fit`.
- The scan bound is `freeAttemptScan = 1024`. The cleanup leaves at most one take, so the
  first free window starts within `MaxAttempts + 1` in practice.
- The writer guard, the cleanup ownership rule, the fit budgets, `web/`, and `deploy/` are
  untouched.
