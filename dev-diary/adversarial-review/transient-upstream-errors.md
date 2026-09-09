# Transient upstream errors

Task: make a transient upstream error survivable.
Branch: master, HEAD 691d0f6, no commit.
Date: 2026-09-09.

## The mechanism

A run for dub `19f8d87716f274fe629a3c4bfd69441d` failed on 2026-09-09 after the creator paid
37,351,500 nanodollars. The server log named the cause:

```
repair line 5: translate line 5 attempt 3: Error 429, Message: Resource exhausted. Please try again later. ... Status: RESOURCE_EXHAUSTED
```

Vertex returned a rate limit. `vertexTranslator.Translate` returned it to `RepairLine`.
`RepairLine` wrapped it as `translate line %d attempt %d` and returned it to the loop.
`Pipeline.Run` returned it to `runRegistry.execute`. `terminalEvent` sent the `error` event
with the sentence `Unexpected error occurred during pipeline execution.` One transient
upstream reply killed the run, and the export never ran.

## The fix

`internal/fit/retry.go` carries one retry policy and three decorators.

- `IsTransientUpstream` classifies a cause. A 429, a 503, a reset connection, a timed out
  connection, and an unexpected EOF are transient. A 400, a 401, a 403, an internal error,
  and an ended context are not. It reads `genai.APIError` (the Vertex shape), gRPC status
  codes (the Cloud TTS shape), and `net` errors.
- `retryTransient` calls the wrapped client at most `MaxAttempts` times. It returns at once
  on a non-transient error and at once when the context ends. The loop can never spin.
- `RetryPolicy.backoff` doubles the delay from `BaseDelay`, caps it at `MaxDelay`, and
  spreads the delay over the second half of that window (equal jitter). No wait exceeds the
  cap.
- `NewRetryingTranslator`, `NewRetryingSynthesizer`, and `NewRetryingSegmenter` wrap the
  three upstream clients. `Pipeline.Run` applies them to the segmenter, the translator, and
  the synthesizer. `pipelineRunner.RenderLine` applies them to the re-render clients.

The charge rule holds by construction. Every production client records its charge only after
a successful round trip. The decorator sits above the client and returns on the first
success. A retried call therefore bills once, and a call that already billed is never
retried.

A line whose retries are exhausted is flagged, exactly as a segment outside the film is
flagged. `Pipeline.Run` catches the transient error, appends a flagged `LineResult` with no
take and no attempts, emits a progress event, and renders the rest. It writes no resume
record, so the next run renders that line fresh. The run finishes `done` with its export and
its persisted takes. The line keeps no take, so the creator loses the earlier attempts of
that one line and re-runs it.

The terminal sentence now follows the cause. `internal/api/run.go` adds `api.ErrUpstreamBusy`.
`Pipeline.Run` tags an exhausted transient failure that stops the run with that marker.
`terminalEvent` maps it to `The model service was busy, so the run stopped. Start the run
again shortly.` A cancelled run still reads `The run was cancelled.` An internal failure keeps
a plain sentence. `run.fail` still logs the exact cause, the run id, the dub id, and the
stage. The browser sentence never carries a SQL error, a host name, a query, or an HTTP
status.

## Pins

Each pin is an independent measurement. The command was
`go test -count=1 -run <name> ./internal/fit/ ./internal/api/ ./cmd/ajilamu/`.

| Where | What | Pin | Mutation |
|---|---|---|---|
| `internal/fit/retry_test.go` `TestIsTransientUpstreamClassifiesCauses` | 18 causes, transient and not | passes | `transientHTTPStatus` returning 400 fails the 429 and 503 rows |
| `internal/fit/retry_test.go` `TestRetryTransientRecoversAfterTwoRateLimits` | two 429 then success | 3 calls, reply returned | see the classifier mutation, the call count falls to 1 |
| `internal/fit/retry_test.go` `TestRetryTransientFailsFastOnRejectedRequest` | a 400 | 1 call, error returned | the classifier mutation retries the 400 |
| `internal/fit/retry_test.go` `TestRetryTransientStopsAfterBoundedAttempts` | a 429 that never clears | 4 calls, elapsed under 1s | removing the bound hangs or overruns the test |
| `internal/fit/retry_test.go` `TestRetryBackoffIsBoundedExponential` | per-attempt window, 200 samples each | every delay inside its window | removing the cap gave `backoff(4) = 592.631761ms, want [200ms, 400ms)` |
| `internal/fit/retry_test.go` `TestRetryTransientStopsWhenContextEnds` | cancel during the wait | `context.Canceled`, 1 call | a wait that ignores the context runs all 5 calls |
| `internal/fit/retry_test.go` `TestRetryTransientSkipsAnEndedContext` | cancel before the call | 0 calls | the same |
| `internal/fit/retry_test.go` `TestRetryingTranslatorBillsOneChargePerLogicalCall` | two 429 then success | 3 calls, exactly 1 charge | a retry above the charge records 2 |
| `internal/fit/retry_test.go` `TestMarkUpstreamBusyTagsOnlyTransientFailures` | busy and internal errors | the marker on the first alone | the route reads the raw cause |
| `internal/fit/loop_test.go` `TestPipelineRetriesTransientTranslationThenCompletesLine` | line 1 gets two 429 then succeeds | line unflagged, 1 attempt, 3 calls, 1 charge | dropping the translator decorator gave `translation calls = 1, want 3` and a flagged line |
| `internal/fit/loop_test.go` `TestPipelineFlagsLineWhenTransientRetriesExhaust` | line 2 always 429 | line 2 flagged with no take, line 1 rendered, 3 calls, last event `done` | the classifier mutation makes `RunPipeline` fail |
| `internal/fit/loop_test.go` `TestPipelineRetriesTransientSegmentation` | a 503 on segmentation | 2 calls, line 1 rendered | dropping the segmenter decorator gave `RunPipeline failed: ... Error 503 ... UNAVAILABLE` |
| `internal/fit/loop_test.go` `TestPipelineRetriesTransientSynthesis` | a 503 on synthesis | 2 calls, line 1 rendered | dropping the synthesizer decorator gave `synthesizer calls = 1, want 2` and a flagged line |
| `internal/fit/loop_test.go` `TestPipelineMarksBusyWhenSegmenterStaysUnavailable` | a 429 on segmentation | error carries `api.ErrUpstreamBusy` and the cause | the route reports the generic sentence |
| `internal/api/run_test.go` `TestRunTerminalSentenceFollowsCause` | busy, cancelled, internal | the busy sentence, the cancelled sentence, the plain sentence, no leak, the log names cause, run id, dub id, stage | deleting the busy case gave the original `Unexpected error occurred during pipeline execution.` |
| `cmd/ajilamu/main_test.go` `TestRunFlagsLineOnPersistentRateLimit` | a real server, real ffmpeg, a stub 429 | terminal `done`, 1 flagged line, busy sentence, export 2.000s, 2 takes and 8 charges on line 1 alone, 3 rate-limited calls | the classifier mutation failed the run on the 429 with no export |

Measured mutation, `transientHTTPStatus` returning `http.StatusBadRequest` instead of 429 and
503. `TestPipelineRetriesTransientTranslationThenCompletesLine`,
`TestRetryTransientRecoversAfterTwoRateLimits`, `TestRetryTransientFailsFastOnRejectedRequest`,
and `TestRunFlagsLineOnPersistentRateLimit` all failed. The end-to-end log reproduced the
incident:

```
level=ERROR msg="run failed" run_id=... dub_id=dub-rate-limited stage=repairing error="repair line 2: translate line 2 attempt 1: Error 429, Message: Resource exhausted. Please try again later., Status: RESOURCE_EXHAUSTED"
```

Measured mutation, deleting the `ErrUpstreamBusy` case from `terminalEvent`. The busy row of
`TestRunTerminalSentenceFollowsCause` failed with the original sentence and leaked it:

```
sentence":"Unexpected error occurred during pipeline execution."
```

## Verification

- `go build ./...` passed.
- `go vet ./...` passed.
- `python3 tools/audit_docs.py` reported `No drift. Docs and repository agree.`
- `go test -count=1 ./internal/fit/ ./internal/api/ ./cmd/ajilamu/` passed.
- The end-to-end pin printed `terminal event: data: {"type":"done","stage":"repairing","sentence":"Dubbing pipeline completed with 1 flagged line.",...}`,
  `ffprobe export duration = 2.000s`, and `persisted 2 takes and 8 charges`.

## Files changed

- `internal/fit/retry.go` (new). The policy, the classifier, the retry loop, the three
  decorators, and the busy marker.
- `internal/fit/retry_test.go` (new). The unit pins.
- `internal/fit/loop.go`. `PipelineConfig.Retry`, the three decorator wraps, the busy marker
  on the segmenter failure, and the flagged line for an exhausted transient failure.
- `internal/fit/loop_test.go`. The pipeline pins.
- `internal/api/run.go`. `api.ErrUpstreamBusy`, the busy sentence, and the terminal mapping.
- `internal/api/run_test.go`. The terminal sentence pin.
- `cmd/ajilamu/main.go`. The retry decorators on the re-render clients.
- `cmd/ajilamu/main_test.go`. The end-to-end pin.

## Contract notes

- `internal/fit/retry.go` imports `google.golang.org/grpc/codes` and `status` to read the
  Cloud TTS failure shape. That module is already in `go.mod` at v1.83.1 as an indirect
  dependency. `go.mod` is untouched, so `go mod tidy` would reclassify the comment only.
- No commit. I started no server and no container. `git status` lists the allowed paths and
  this record alone.
