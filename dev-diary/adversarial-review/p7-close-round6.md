# P7 close: independent verification, round 6

**Scope:** All 21 Phase 7 tasks at HEAD `c4f1f94`, plus the uncommitted
`web/src/lib/progress.ts` fix.
**Method:** Independent measurements in `/tmp/p7-close-r6`.
**Verdict:** APPROVE. Findings: 0 C, 0 H, 0 M, 0 L.

| C | H | M | L |
|---|---|---|---|
| 0 | 0 | 0 | 0 |

I own only this review file. I changed no production file, task status, exit box, or shared
document. I did not treat earlier review records as evidence.

The source worktree contains unrelated P5 and P6 work. The review clone contains committed
HEAD plus the one-line progress timer fix.

## Findings

| Where | What | Pin | Mutation |
|---|---|---|---|
| None | No finding met the bar. | The clean-clone check and all close pins pass. | Both targeted mutations fail their pins. |

## Round 5 M1

I ran `npm ci` in the clean clone with Node 26.8.1. The clone installed from the committed
lockfile. `npm run check` exited 0 with zero errors and zero warnings.

I then restored `let timer: number | undefined` in the clone. The same check exited 1 at
`progress.ts:106`. It reported `Type 'Timeout' is not assignable to type 'number'`.

I restored `ReturnType<typeof setTimeout>` after the mutation. Tracked clone content then
differed from HEAD only by the intended one-line fix.

Round 5 M1 residue: zero.

## Exit criteria

I re-measured all seven criteria. I did not tick the phase boxes.

| Criterion | Independent result |
|---|---|
| The server runs from a clone with no credentials and serves the workspace from fixtures | An `env -i` server answered 200 for `/`, `/d/fixture`, and `/api/dubs`. Readiness answered 503 and named `CLICKHOUSE_HOST`. A range request returned 206 with 1024 bytes. |
| Shutdown flushes the ledger queue without losing events | Both shutdown queue pins passed. They cover a normal drain and a drain timeout. |
| Large video files stream directly to storage without memory bloat | The upload path still uses `http.MaxBytesReader` and `io.CopyBuffer` with a 128 KiB buffer. A fresh multipart upload returned 201. The authorized 512 MiB pin was not repeated. |
| Users can upload optional background music tracks | A fresh video and music request returned 201 in 0.008 seconds. Stored sizes matched 134456 and 183096 bytes. Both files measured mode 0600. |
| Sample mode launches instantly with one click | The create screen has one sample button bound to one POST. The POST returned 201 in 0.083 seconds and copied the 18385109-byte fixture. |
| Progress events stream complete sentences with running costs | The mounted stream and wire pins passed. They verify frame ids, sentence punctuation, and cumulative nanodollar totals. |
| Interrupted runs remain resumable without data corruption | The completed-take and six orphan paths passed. Every second run makes zero synthesis writes. |

## Orphan cleanup mutation

The clean implementation walks from attempt one through `MaxAttempts`. It resolves raw and
stretched paths for every attempt.

I changed the walk to attempt one only in the review clone. All four later-attempt cases failed
with overwrite errors. They covered attempt two and three in raw and stretched forms.

I restored the complete walk after the mutation.

## T7.3d media fixtures

All four fixture paths are tracked. The clean clone contains no `scratch/` or `assets/`
directory. Direct ffprobe 9.0.1 measurements produced these results.

| Fixture | Duration | Audio |
|---|---:|---|
| `testdata/takes/seg_3_try1.wav` | 5.720375 s | PCM s16le, 16000 Hz, mono, 256000 bit/s |
| `testdata/takes/seg_3_stretched.wav` | 5.337563 s | PCM s16le, 16000 Hz, mono, 256000 bit/s |
| `testdata/takes/seg_8_try1.wav` | 4.200375 s | PCM s16le, 16000 Hz, mono, 256000 bit/s |
| `testdata/clip.mp4` | 75.008267 s | AAC, 44100 Hz, stereo, 132532 bit/s |

The nearest-millisecond pins remain 5720 ms and 5338 ms.
`go test -count=1 ./...` passed all 14 packages in the clean clone.

T7.3d residue: zero.

## Prior close residue

| Prior claim | Independent result |
|---|---|
| Round 1 raw attempt-one orphan | Closed. The raw orphan pin passes. |
| Round 2 stretched attempt-one orphan | Closed. The stretched orphan pin passes. |
| Round 3 later-attempt orphans | Closed. All four raw and stretched cases pass. |
| Round 4 complete cleanup walk | Closed. The attempt-one-only mutation fails every later-attempt case. |
| Earlier clean-clone media gap | Closed. The full Go suite passes without either legacy directory. |
| Earlier frontend build probe gap | Closed. The clean static build serves the fixture workspace and range request. |

Residue against P7 close rounds 1 through 5: zero.

## Supporting checks

`python3 tools/audit_docs.py` ran first and exited 0.
It reported that the docs and repository agree.

The clean clone frontend build passed. `go build ./cmd/ajilamu` passed.
All 21 task blocks read `status: done`.

Go reports 1.27.1. Node reports 26.8.1.
The media measurements used ffmpeg and ffprobe 9.0.1.

## Evidence paths

- Clean clone and mutations: `/tmp/p7-close-r6`
- HTTP bodies: `/tmp/p7-close-r6/*.json` and `/tmp/p7-close-r6/*.html`
- Range body: `/tmp/p7-close-r6/range.bin`
- Stored upload artifacts: `/tmp/p7-close-r6/data/uploads`

## Leftover processes

None. I stopped the server and confirmed that port 19781 has no listener.
