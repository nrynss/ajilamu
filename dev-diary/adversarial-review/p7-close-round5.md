# P7 close: independent verification, round 5

**Scope:** All 21 Phase 7 tasks and every phase exit criterion at HEAD `c4f1f94`.
**Method:** Independent measurements in a clean clone at `/tmp/p7-close-r5-20260909`.
**Verdict:** REMEDIATE. Findings: 0 C, 0 H, 1 M, 0 L.

| C | H | M | L |
|---|---|---|---|
| 0 | 0 | 1 | 0 |

I own only this review file. I changed no production file, task status, exit box, or shared document.
I did not treat earlier review records as evidence.

The source worktree gained an untracked P5 review during measurement. It sits outside my scope.
Tracked production content remained clean at `c4f1f94d3ae0d15f25e3a415d73226ac58a6271a`.

## Findings

| Where | What | Pin | Mutation |
|---|---|---|---|
| `web/src/lib/progress.ts:76,106` | The reconnect timer has type `number`, but the clean clone resolves `setTimeout` as `NodeJS.Timeout`. The required Svelte check fails before reporting any other diagnostics. The production build and browser still work, so this is M. | After `npm ci`, `PATH=/opt/homebrew/opt/node/bin:$PATH npm --prefix web run check` exits 1. It reports `Type 'Timeout' is not assignable to type 'number'` at line 106. | In the temporary clone, `ReturnType<typeof setTimeout>` makes the same check report zero errors. Restoring `number` restores the failure. |

## Exit criteria

I re-measured all seven criteria. I did not tick the phase boxes.

| Criterion | Independent result |
|---|---|
| The server runs from a clone with no credentials and serves the workspace from fixtures | An `env -i` server answered 200 for `/`, `/d/fixture`, and `/api/dubs`. Readiness answered 503 and named missing `CLICKHOUSE_HOST`. A range request returned 206 with exactly 1024 bytes. Headless Chrome rendered the create screen. |
| Shutdown flushes the ledger queue without losing events | A blocked stand-in left one durable queue file. I opened the transport and sent `SIGTERM`. The process exited, the queue reached zero, and the stand-in captured `shutdown-r5` once. |
| Large video files stream directly to storage without memory bloat | A 512 MiB upload returned 201 in 1.179 seconds. Server RSS ranged from 8,864 KiB to 13,152 KiB across 123 samples. Growth was 4,288 KiB. |
| Users can upload optional background music tracks | A multipart request with video and music returned 201 in 0.034 seconds. Both stored files matched their source sizes and measured mode 0600. A prior 512 MiB request without music also returned 201. |
| Sample mode launches instantly with one click | Chrome clicked the sample button once. The browser issued one sample POST and navigated to the new workspace in 49 ms. A direct POST returned 201 in 0.019 seconds. |
| Progress events stream complete sentences with running costs | A mounted fixture runner produced four live SSE frames through HTTP. Their costs were 0, 1,250,000, 2,500,000, and 2,500,000 nanodollars. Every sentence ended with punctuation. |
| Interrupted runs remain resumable without data corruption | The completed-take and six orphan paths passed. Every second run made zero synthesis writes. An attempt-one-only mutation failed all four later-attempt cases with overwrite errors. |

The runtime criteria pass. Finding M1 keeps the phase open because every severity must reach zero.

## T7.3d committed media fixtures

I confirmed all four fixture paths are tracked. I invoked ffprobe 9.0.1 directly on each artifact.

| Fixture | Duration | Audio stream |
|---|---:|---|
| `testdata/takes/seg_3_try1.wav` | 5.720375 s | PCM s16le, 16000 Hz, mono, 256000 bit/s |
| `testdata/takes/seg_3_stretched.wav` | 5.337563 s | PCM s16le, 16000 Hz, mono, 256000 bit/s |
| `testdata/takes/seg_8_try1.wav` | 4.200375 s | PCM s16le, 16000 Hz, mono, 256000 bit/s |
| `testdata/clip.mp4` | 75.008267 s | AAC, 44100 Hz, stereo, 132532 bit/s |

The pinned nearest-millisecond values remain 5720 ms and 5338 ms.
`go test -count=1 ./internal/media` passed with ffmpeg and ffprobe 9.0.1 first on PATH.

I restored all seven legacy `scratch/` and `assets/` references in the temporary clone.
Neither legacy directory existed. Six tests failed on missing fixtures, including both `TestAudioFormat` subtests.
I restored the committed file and confirmed no tracked mutation remained.

T7.3d residue: zero.

## Prior close residue

| Prior claim | Independent result |
|---|---|
| Round 1 raw attempt-one orphan | Closed. The raw orphan pin passes and the next run makes zero synthesis writes. |
| Round 2 stretched attempt-one orphan | Closed. The stretched orphan pin passes and the next run makes zero synthesis writes. |
| Round 3 later-attempt orphans | Closed. Attempt two and three pass in raw and stretched forms. |
| Round 4 complete cleanup walk | Closed. An attempt-one-only mutation fails every later-attempt case. Restoring the walk makes all cases pass. |
| Earlier clean-clone media note | Closed by T7.3d. The full Go suite passes without `scratch/` or `assets/`. |
| Earlier `web/dist` probe note | Closed by T7.6. `findFrontendRoot` probes only `web/build`. `.gitignore` covers both generated directories. No new task is needed. |

Residue against P7 close rounds 1 through 4: zero.

## Supporting checks

`python3 tools/audit_docs.py` exited 0 before review work.
It reported that the docs and repository agree.

`go test -count=1 ./...` passed all 14 Go packages in the clean clone.
The clone had no `scratch/` or `assets/` directory.

`npm ci` installed from the committed lockfile.
`npm run build` emitted `web/build`, and `go build ./cmd/ajilamu` passed.
The clean-clone Svelte check failed only with finding M1.

Go reports 1.27.1. Node reports 26.8.1.
The media acceptance run used ffmpeg 9.0.1 and ffprobe 9.0.1 from `/opt/homebrew/bin`.

All 21 task blocks read `status: done`.
The documentation audit found no missing owned path.

## Evidence paths

- Credential-free HTTP responses: `/tmp/p7-close-r5-20260909/*.headers`
- Browser screenshot: `/tmp/p7-close-r5-20260909/new-page.png`
- Upload response and RSS samples: `/tmp/p7-close-r5-20260909/large-upload.txt` and `upload-rss-kib.txt`
- Music and sample responses: `/tmp/p7-close-r5-20260909/music-upload.txt` and `sample-response.txt`
- SSE frames: `/tmp/p7-close-r5-20260909/sse-events.txt`
- Shutdown transport capture: `/tmp/p7-close-r5-20260909/clickhouse-capture.txt`
- Orphan mutation output: `/tmp/p7-close-r5-20260909/orphan-mutation.txt`
- Legacy media mutation output: `/tmp/p7-close-r5-20260909/media-legacy-mutation.txt`

## Leftover processes

None. I stopped every Ajilamu, Chrome, SSE probe, and ClickHouse stand-in process that this review started.
