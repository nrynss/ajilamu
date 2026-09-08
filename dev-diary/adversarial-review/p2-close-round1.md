# P2 close: independent verification, round 1

**Scope:** Phase 2 exit criteria for T2.1 through T2.8b as they stand in the tree.
**Method:** Independent measurement of every claim with outside tools. No reliance on per-task review files.
**Verdict:** APPROVE. Findings: 0 C, 0 H, 0 M, 0 L.

I own only this review file. I did not tick the phase exit boxes. I did not treat task review files as evidence.

I measured commit `4a4747b` in a temporary copy at `/var/tmp/p2close/repo`. Another agent edits
`cmd/ajilamu/main.go` and `internal/api/server.go` for P6. I never measured that uncommitted state.
The copy came from `git archive HEAD`, so it holds exactly the committed tree.

| C | H | M | L |
|---|---|---|---|
| 0 | 0 | 0 | 0 |

## Audit

`python3 tools/audit_docs.py` exits 0. It prints "No drift. Docs and repository agree."
The audit ran first, before I wrote this review.

## Findings

| Where | What | Pin | Mutation |
|---|---|---|---|
| None | No findings at any severity. | Independent artifact measurements below. | Reverting the ADC cutover, the token billing, the language fields, or the stretch sentinels would fail those pins. |

## Task status and owns

All ten P2 tasks read `status: done`. Every owns path exists on disk.

| Task | Owns | Present |
|---|---|---|
| T2.1 | `internal/gemini/segment.go` | yes |
| T2.2 | `internal/gemini/translate.go` | yes |
| T2.2dev | 17 paths across `internal/gemini`, `internal/cost`, `internal/config`, `go.mod`, `go.sum`, `.env.example`, `AGENTS.md`, `dev-diary/` | yes |
| T2.3 | `internal/tts/chirp.go`, `voices.go`, `chirp_test.go`, `voices_test.go`, `go.mod`, `go.sum` | yes |
| T2.4 | `internal/fit/measure.go`, `measure_test.go` | yes |
| T2.5 | `internal/fit/stretch.go` | yes |
| T2.6 | `internal/fit/rewrite.go` | yes |
| T2.7 | `internal/fit/loop.go` | yes |
| T2.8a | `internal/tts/chirp.go`, `chirp_test.go`, `voices.go`, `voices_test.go`, `internal/gemini/translate.go`, `translate_test.go` | yes |
| T2.8b | `internal/fit/loop.go`, `loop_test.go`, `cmd/ajilamu/main.go`, `main_test.go`, `dev-diary/adversarial-review/t2.8-contract-change.md` | yes |

## Exit criteria

I measured each criterion. I did not tick the boxes.

| Criterion | Independent result |
|---|---|
| Pipeline executes end to end in Go | A driver ran `RunPipeline` over the eight fixture segments. It produced eight lines, one flagged line, and 22 itemized charges. |
| Identifies and repairs or flags segment 8 underrun | Segment 8 measured 4200 ms against its 7110 ms slot. The loop flagged it after three attempts. Segments 2 through 7 repaired by atempo. |
| Three full attempts and clean flagged lines | Segment 8 holds three attempts at 4200, 4600, and 4400 ms. The loop kept attempt 2 at signed delta -2510 ms and wrote bounded copy. |
| Distinct voice profiles per speaker | `Assign` returned `ml-IN-Chirp3-HD-Achernar` for Suni Williams and `ml-IN-Chirp3-HD-Achird` for Mark Vande Hei. An unknown speaker errors. |
| Itemized charges on reported token counts | Each Gemini charge carried the `UsageMetadata` counts. The fake reply 7227 prompt and 800 candidate tokens billed 1564050 nanodollars. Synthesize bills runes. |
| Gemini through `genai` and ADC alone | `go.mod` requires `google.golang.org/genai` v1.71.0. No `internal/` or `cmd/` file imports `golang.org/x/oauth2`. The ADC client skips cleanly under a bogus credential path. |
| Unit tests pass offline on fixtures | `go test -count=1` passes for `internal/fit`, `internal/tts`, `internal/gemini`, `internal/cost`, `internal/config`, and `cmd/ajilamu`. A poisoned environment still passes. |

## Prior-round residue

I re-measured the pins that closed T2.1 through T2.8b. I did not quote those reviews as proof.

**T2.1: zero residue.** `segmentPrompt` measures 658 bytes. Its SHA-256 is
`0e8717cd81426cddaaf5c46403e9ca20271001f0c9ad9714c374c2748c5dfaef`. It is byte identical to the
prompt literal in `tools/validate_pipeline.py`. One segment `Charge` lands after a full parse. It
billed 7227 prompt and 800 candidate tokens, with `Units` at zero. The production client passes
`BackendVertexAI`, `Project`, and `Location` only. The live probe checks ordered ids, clip bounds,
overlaps, and speaker coverage, and it prints a drift table.

**T2.2: zero residue.** The normal, shorter, and fuller prompts measure 509, 479, and 529 bytes.
All three are byte identical to the Python f-string for segment 8. The fuller sentence names
`7.1 seconds (7110 ms)`, says `too empty`, and bans meaningless padding. One translation charge
billed 107 prompt and 40 candidate tokens for 40050 nanodollars.

**T2.2dev: zero residue.** `go.mod` requires `google.golang.org/genai` v1.71.0 directly.
`golang.org/x/oauth2` v0.36.0 sits in the indirect block. No `internal/` or `cmd/` file imports
`oauth2` or names `aiplatform.googleapis.com`. The loader requires `GOOGLE_CLOUD_PROJECT`, strips a
`google/` model prefix, and never reads `GEMINI_API_KEY`. `.env.example` carries the bare
`gemini-3.8-flash` id and no API key. `project.md`, `AGENTS.md`, and `infrastructure.md` name the
SDK, ADC, and the attached service account.

**T2.3: zero residue.** The fixture double copied `seg_8_try1.wav` byte for byte. The copy measured
134456 bytes with SHA-256 `484c67e514ddfda52f7ab88b40b5283365d8496c81640c6d335e9fe091ae87bd`. A
four-rune line billed 4 units and 120000 nanodollars, never 12 bytes. The real synthesizer sent
`LINEAR16`, left `SampleRateHertz` unset, and sent the stored language code.

**T2.4: zero residue.** `ffprobe` reports the eight takes at 1.680375, 5.320375, 5.720375,
5.920375, 4.720375, 4.440375, 6.800375, and 4.200375 seconds. Every duration and signed delta
matches `testdata/expected/metrics.json`. Segment 8 is -2910 ms. The repair label differs for
segments 2, 5, 6, and 7, where that file records `none` and the package now plans atempo. The T2.5
entry documents that difference. The stretched goldens measure 5.337563 and 5.662250 seconds.

**T2.5: zero residue.** The plan table under default limits matches the handoff. Segment 1 and
segment 8 plan a rewrite. Segments 2 through 7 plan atempo at 0.970803, 1.075188, 1.045936,
0.975207, 0.958963, and 0.965909. The short budget is 0.05, the long budget 0.08, and the band
40 ms. The package's own `StretchWithLimits` landed the six repairs at 5469, 5333, 5663, 4832,
4621, and 7031 ms. Every miss stays inside 13 ms. A stale `Delta` literal still plans a rewrite.
Six legal calls under a 0.10 budget walked segment 8 from 4200 to 7101 ms. Every call returned nil.
The finished artifact replans `none`. The raw take replans `rewrite`.

**T2.6: zero residue.** An overrun requested `ModeShorter` on attempt 2. An underrun requested
`ModeFuller`. `SignedDeltaMs` derived `[500, -300]` from literals whose `Delta` field was zero.
Neither `loop.go` nor `rewrite.go` calls `applyStretch`, `renderStretch`, `stretchRatio`,
`renderCandidate`, or `prepareRatio`.

**T2.7: zero residue.** The run produced 11 translation charges and 11 synthesis charges. The total
was 10010550 nanodollars, which equals the sum of the parts. The work directory held
`seg_3_try1.wav`, `seg_3_stretched.wav`, `seg_4_try1.wav`, `seg_4_stretched.wav`, `seg_8_try1.wav`,
`seg_8_try2.wav`, and `seg_8_try3.wav` together. No take overwrote an earlier attempt. Events
covered segmenting, translating, synthesizing, measuring, and repairing. Running cost never fell.

**T2.8a: zero residue.** A Spanish target reached Cloud TTS as `es-ES`. The prompt named Spanish in
words. It carried no Malayalam hint. An unknown source named no language and never claimed English.
An empty or malformed code failed before any billable call. The Malayalam prompt stayed byte
identical to the Python f-string.

**T2.8b: zero residue.** The loop copied the target and source names onto all 11 translation
requests. Its sentences named the target language. The runner built one synthesizer per run from
the resolved tag. A hardcoded tag inside the factory failed
`TestRunSynthesizerFactoryForwardsLanguage`. A hardcoded tag at the call site failed
`TestPipelineRunnerLanguageWiring`. The Malayalam fixture run is unchanged. It flagged segment 8
alone, with attempts at -2910, -2510, and -2710 ms and attempt 2 chosen.

## Independent measurements

I copied the committed tree to `/var/tmp/p2close/repo` and added throwaway drivers under `cmd/`.
The drivers called the exported production API. They printed artifacts, and I measured those
artifacts again outside Go. Every duration below came from `ffprobe` or from `internal/media`,
which wraps `ffprobe`. Go reports 1.27.1. ffmpeg and ffprobe report n9.0.1.

### Pipeline run over the fixture segments

A WAV synthesizer wrote takes of scripted durations. A recording translator billed 107 prompt and
40 candidate tokens per call. Segment 1 retried into its slot. Segment 8 failed three attempts.

| Line | Attempts | Modes | Deltas (ms) | Chosen | Repair |
|---|---|---|---|---|---|
| 1 | 2 | normal, fuller | -140, 0 | `seg_1_try2.wav` | none |
| 2 | 1 | normal | -3 | `seg_2_stretched.wav` | atempo 0.970803 |
| 3 | 1 | normal | -1 | `seg_3_stretched.wav` | atempo 1.075188 |
| 4 | 1 | normal | -1 | `seg_4_stretched.wav` | atempo 1.045936 |
| 5 | 1 | normal | -3 | `seg_5_stretched.wav` | atempo 0.975207 |
| 6 | 1 | normal | -3 | `seg_6_stretched.wav` | atempo 0.958963 |
| 7 | 1 | normal | -3 | `seg_7_stretched.wav` | atempo 0.965909 |
| 8 | 3 | normal, fuller, fuller | -2910, -2510, -2710 | `seg_8_try2.wav` | flagged |

The run recorded 22 charges, 11 translate and 11 synthesize. The ledger total matched the result
total at 10010550 nanodollars. The event stream named `ml-IN` on every event.

### Language wiring

A Spanish run captured every request at the translator and the synthesizer boundary. All 11
translation requests carried `TargetLanguageName` "Spanish" and `SourceLanguageName` "English".
The progress sentences read "Translating line 1 into Spanish." and "Rendering line 1 in Spanish."
The voice map held `es-ES-Chirp3-HD-Achernar` and `es-ES-Chirp3-HD-Achird`.

A real `tts.NewSynthesizer` with a fake Cloud client sent `LanguageCode` `es-ES` and voice
`es-ES-Chirp3-HD-Achernar`, with `LINEAR16` and no sample rate. The same build for `ml-IN` sent
`ml-IN` and `ml-IN-Chirp3-HD-Achernar`.

`NewPipeline` rejected an empty code, a malformed code, and a missing display name. Every rejection
recorded zero charges.

### Prompt bytes

I extracted the two proven prompt literals from `tools/validate_pipeline.py` in Python. I rendered
the translation prompt with the Python f-string for segment 8. I compared each file with `cmp`.

| Prompt | Bytes | Python SHA-256 | Go SHA-256 | Identical |
|---|---|---|---|---|
| Segment | 658 | `0e8717cd…5dfaef` | `0e8717cd…5dfaef` | yes |
| Malayalam normal | 509 | `2c5b4b18…e4b77a1` | `2c5b4b18…e4b77a1` | yes |
| Malayalam shorter | 479 | `c5d7b140…e5d322ee` | `c5d7b140…e5d322ee` | yes |

The Spanish prompt read "Translate this English dialogue line into natural spoken Spanish:".
It closed with "Respond with strictly the translated Spanish text."
An unknown source produced "Translate this dialogue line into natural spoken Spanish:".
That prompt named no English.

### Stretch sentinels

| Case | Sentinel | Output path |
|---|---|---|
| Segment 8 underrun | `ErrUnderrunTooLarge` | empty |
| Take inside the dead band | `ErrDeadBand` | empty |
| Existing output | `ErrOutputExists` | seed preserved |
| Directory as source | `ErrSourceUnreadable` | empty |
| Missing output directory | `ErrRenderFailed` | empty, 172 bytes on one line |
| Extensionless output | `ErrRenderFailed` | empty, 2563 bytes on 18 lines |
| Limits above the ceiling | `ErrLimits` | empty |

The wrapped messages keep the raw tool text and its banner. The handoff tells callers to branch on
the sentinel instead of printing that text.

## Notes, not findings

**Live calls were not re-run.** Vertex and Cloud TTS need a network and a real key. I measured the
ADC path with `GOOGLE_APPLICATION_CREDENTIALS=/no/such/file.json`. Both live probes skipped with
`application default credentials unusable`. The offline artifact behavior carries the rest.

**Line numbers in the T2.8b records moved after T7.5b.** The handoff names
`cmd/ajilamu/main.go:402-404`, `413-417`, `438`, `447`, and `459`. The contract-change table names
`370`, `378`, `398`, `404`, `429-431`, `440`, `452`, `467-478`, `475`, and `702`. Those numbers
matched commit `4622205`, the T2.8b commit. Commit `4a4747b` (T7.5b) shifted them. The contract
change file warns that its table may move. A reader should match the function name.

**The T2.1 handoff entry predates the SDK cutover.** It still names `golang.org/x/oauth2`, a 658
unit character charge, and a next step that consumes `endpointURL`, `postGenerate`, and
`envelopeText`. T2.2dev deleted those symbols and the T2.2dev entry supersedes that text. The entry
is a dated record of 2026-09-07. I did not file it, because no exit criterion depends on it.

**A clean clone fails `internal/media` tests.** Those tests read `scratch/takes/` and
`assets/source/clip.mp4`, which stay untracked. The worktree holds both, so the suite passes there.
`internal/media` belongs to T0.3. It sits outside P2's owns, and I filed nothing.

Temporary drivers and measurement files live under `/var/tmp/p2close/`. They never entered the
repository. No production code changed during this review.
