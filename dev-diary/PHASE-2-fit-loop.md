# P2: Fit Loop (Track A)

```yaml
id:       P2
size:     M
branch:   phase/p2-fit-loop
requires: [P1, T0.3]
blocks:   P7 (live run), P8
parallel: medium
runs-parallel-with: P3, P4, P5, P6, P7
```

**Goal:** Port the validated pipeline loop from Python into Go. Add lower-bound duration checks and support for three retry attempts.

**Port proven logic:** The core loop works reliably. It detected 8 segments and repaired 2 overruns using time-stretching without extra model fees.

**Network dependency:** This track interacts with Gemini and Chirp APIs. We isolate external services behind interfaces with fixture doubles.

---

### T2.1: Segmentation client ★
```yaml
requires:   T1.1, T1.2, T0.2
fixture-ok: yes
size:       M · mid
owns:       internal/gemini/segment.go
status:     done
```
Execute one multimodal pass returning timestamped segments, speakers, and emotional tone. Port the proven prompt and JSON response schema from Python.

Call Gemini through `google.golang.org/genai` on Vertex AI with Application Default Credentials.
Read `GEMINI_MODEL` and `GOOGLE_CLOUD_LOCATION` from configuration rather than hardcoding values.

Accept media buffers rather than audio-only paths so future updates can pass video frames directly.

Emit a `Charge` per API invocation. Bill it from `UsageMetadata` prompt and candidate token counts.

**Done when:** A live probe asserts structure (ordered ids, clip bounds, no overlaps, speaker coverage) and prints a drift table. The offline suite passes without a network call and without ADC.

---

### T2.2: Duration-budgeted translation ★
```yaml
requires:   T1.1, T1.2, T0.2
fixture-ok: yes
size:       S · mid
owns:       internal/gemini/translate.go
status:     done
```
Translate dialogue under a time budget. Implement three modes: normal, shorter (for overruns), and fuller (for underruns).

Call Gemini through `google.golang.org/genai` on Vertex AI with Application Default Credentials.

The shorter-mode prompt proved effective in testing. For underruns, prompt for complete phrasing that fills the slot naturally without meaningless padding.

Pass emotional tone into this prompt.

**Done when:** All three modes return natural translations for segment 8, and each request emits an itemized `Charge` billed from `UsageMetadata` token counts.

---

### T2.2dev: Adopt the Gen AI SDK and ADC ★
```yaml
requires:   T2.1, T2.2
fixture-ok: yes
size:       L · frontier
owns:       internal/gemini/segment.go, internal/gemini/translate.go,
            internal/gemini/segment_test.go, internal/gemini/translate_test.go,
            internal/gemini/segment_live_test.go, internal/gemini/translate_live_test.go,
            internal/cost/cost.go, internal/cost/cost_test.go,
            internal/config/config.go, internal/config/config_test.go,
            .env.example, go.mod, go.sum,
            AGENTS.md, dev-diary/project.md, dev-diary/infrastructure.md,
            dev-diary/PHASE-2-fit-loop.md
status:     done
```
Migrate both Gemini clients onto `google.golang.org/genai` and onto Application Default
Credentials. Retire the hand-rolled REST path.

**Why this task exists.** T2.1 and T2.2 shipped a wider variation than the owner approved. The
owner approved a credential library. The clients also dropped the Gemini SDK and hand-rolled
`net/http` calls against `aiplatform.googleapis.com`. Neither `project.md` nor `AGENTS.md`
records that second change, so the repository describes a stack the code does not use.

The contest rules require an accepted Google package imported and actually called in code. They
name `google-adk`, `google-genai`, `google-generativeai` and `google-cloud-aiplatform`.
`golang.org/x/oauth2/google` appears on none of those lists, because it authenticates rather
than calling a model. A judge who greps the imports finds no accepted package today.

Do this before T2.3. A third hand-rolled client triples the migration.

#### The SDK

Add `google.golang.org/genai`. Pin below v2.0.0, which the SDK's own README asks for. Version
v1.71.0 published on 2026-08-31.

Construct the client with `Backend: genai.BackendVertexAI`, plus `Project` and `Location` from
configuration. `BackendEnterprise` names the same backend and the client normalises it, so
prefer the explicit `BackendVertexAI`.

The SDK covers what the hand-rolled code built by hand. `GenerateContentConfig` carries
`ResponseMIMEType` and `ResponseSchema` for the strict segmentation JSON. `Part.InlineData`
carries media bytes, which keeps T2.1's buffer interface. `UsageMetadata` reports token counts.

#### Credentials

Use ADC and nothing else. Pass no `Credentials` and no `APIKey`, so the SDK calls
`credentials.DetectDefault` on its own. That path reads the GCE metadata server, so a deployed
binary needs no key material.

`GEMINI_API_KEY` stops being a credential path. Narrow the loader in `internal/config` to
require `GOOGLE_CLOUD_PROJECT`, and let `GOOGLE_APPLICATION_CREDENTIALS` stay optional for local
development. Update the config tests, `.env.example` and the error messages together.

Adopting the SDK retires the stack exception. `golang.org/x/oauth2` becomes an indirect
dependency of the SDK rather than a direct one. Rewrite the exception paragraph in `AGENTS.md`
to name the SDK, and record that the SDK supplies ADC itself.

#### Deployment model

Record this in `dev-diary/infrastructure.md` beside the existing service layout.

The GCE host runs under an attached service account holding the Vertex AI User role. The
metadata server supplies the token. No key file reaches the virtual machine, and no credential
reaches an image layer.

A developer machine authenticates with `gcloud auth application-default login`, or points
`GOOGLE_APPLICATION_CREDENTIALS` at a key file it already holds. Both paths satisfy
`DetectDefault`, so the code reads the same in both places.

#### The model id defect

`.env` carries `GEMINI_MODEL=google/gemini-3.8-flash`. The hand-rolled builder already inserts
`publishers/google/models/`, so the configured value doubles the prefix and returns 404. A
measured probe on 2026-09-07 returned 404 for the prefixed id and 200 for `gemini-3.8-flash`.

The prefix belongs to the OpenAPI compatible surface that `VERTEX_OPENAPI_BASE_URL` names. The
SDK uses the native surface and takes a bare model id. Fix `.env.example` to carry the bare id,
and decide in the handoff log whether `VERTEX_OPENAPI_BASE_URL` still earns its place.

Both prior handoff notes documented this defect and left it, because no task owned the file.
This task owns it.

The project model is `gemini-3.8-flash`, which the owner settled on 2026-09-07. It already
matches `config.DefaultGeminiModel`. Every live pin for T2.1 and T2.2 ran `gemini-2.5-flash`
instead, through an override the probes export. Drop those overrides and re-pin the live
evidence on the project model, so the model this repository claims is the model it measured.

#### Charges bill on tokens

Bill each `Charge` from `UsageMetadata`, using the prompt and candidate token counts the API
returns. The current clients bill on prompt character count, which is a proxy that no invoice
will match.

This amends T1.2, which P1 froze. Amend it openly in `internal/cost` and record the change, so
T4.2 reads one contract rather than two. Keep the rule that a take costs its own charges and
never an accumulated total.

#### Amend the specifications you invalidate

Update the stack table in `project.md` to name the SDK. Update the stack exception in
`AGENTS.md`. Update the T2.1 and T2.2 blocks in this file to describe the client that now
exists.

T2.1's **Done when** still reads that the client matches `testdata/segments.json`. Live runs
returned 11, 10 and 8 segments across models, and the probe now asserts structure and prints a
drift table instead. That relaxation is sound, because no generative model reproduces an exact
segment count on demand. Write the relaxation into the **Done when** rather than leaving the
spec and the probe disagreeing.

**Done when:** `go.mod` requires `google.golang.org/genai`, and no file under `internal/`
imports `golang.org/x/oauth2` directly. A live probe segments the sample clip through the SDK and
translates segment 8 in all three modes. It runs on ADC with no API key set. Its charges carry
the token counts the response reported. The offline suite passes with no network call.
`.env.example` carries a model id that returns 200 against Vertex. `project.md`, `AGENTS.md` and
`infrastructure.md` describe the shipped stack, the ADC model and the attached service account.

---

### T2.3: Synthesis client ★
```yaml
requires:   T1.1, T1.2, T0.2, T2.2dev
fixture-ok: yes
size:       M · mid
owns:       internal/tts/chirp.go, internal/tts/voices.go,
            internal/tts/chirp_test.go, internal/tts/voices_test.go,
            go.mod, go.sum
status:     done
```
Synthesize speech with Google Cloud Chirp 3 HD voices in LINEAR16 format. Save each take as an independent WAV file.

Request native sample rates rather than forcing 16 kHz. Downstream mixing preserves higher fidelity when takes match high-quality source video.

Implement voice assignment. Map speakers to distinct voices based on language and gender instead of sharing one hardcoded voice.

**Done when:** Distinct speakers in `testdata/segments.json` receive distinct voices, and the test double returns fixture audio files.

---

### T2.4: Measure ★
```yaml
requires:   T1.1, T0.3
fixture-ok: yes
size:       XS · light
owns:       internal/fit/measure.go, internal/fit/measure_test.go
status:     done
```
Accept a rendered WAV take and target duration, then return a populated `Fit` struct.

Calculate duration using `internal/media` and compute signed deltas using domain methods.

**Done when:** Measuring takes in `testdata/takes/` reproduces golden values, including segment 8's −2910 ms underrun.

---

### T2.5: Two-sided stretch repair ★
```yaml
requires:   T2.4, T0.3
fixture-ok: yes
size:       M · frontier
owns:       internal/fit/stretch.go
status:     done
```
Apply `atempo` time-stretching when audio duration falls within 8% of target slot duration in either direction.

Calculate ratio as `Measured / Slot`. Ratios above 1.0 speed up long takes. Ratios below 1.0 slow down short takes.

Always re-measure audio duration after applying `atempo`. Verify the output landed inside the target slot.

**Done when:** Stretched audio matches expected durations within tolerance, and invalid ratios outside 0.5 to 2.0 return explicit errors.

---

### T2.6: Rewrite repair and third attempt ★
```yaml
requires:   T2.2, T2.3, T2.5
fixture-ok: yes
size:       M · mid
owns:       internal/fit/rewrite.go
status:     done
```
Request shorter translations when takes overrun by more than 8%. Request fuller translations when takes underrun by more than 8%.

Support three consecutive attempts per dialogue line. Re-measure each attempt and apply time-stretching if duration enters the 8% window.

If three attempts fail, flag the line for creator review and keep the closest take.

**Done when:** Failing three attempts produces a flagged take with signed delta records and clear notification copy.

---

### T2.7: Loop orchestration ★
```yaml
requires:   T2.1, T2.4, T2.5, T2.6, T1.2
fixture-ok: yes
size:       L · frontier
owns:       internal/fit/loop.go
status:     not-started
```
Orchestrate the dubbing pipeline: segment video, translate dialogue, synthesize speech, measure fit, and apply repairs.

Save every attempt as an immutable WAV file (`seg_1_try1.wav`, `seg_1_stretched.wav`). Never overwrite prior takes.

Emit progress events and charges at every stage. Call ledger logging through interfaces so tests run with mock recorders.

**Done when:** The Go loop reproduces test run outputs against fixtures, flags segment 8, and accounts for all API expenses.

---

## Exit Criteria

- [ ] Pipeline executes end to end in Go.
- [ ] Correctly identifies and repairs or flags underruns in segment 8.
- [ ] Implements three full attempts and handles flagged lines cleanly.
- [ ] Assigns distinct voice profiles to distinct speakers.
- [ ] Records itemized charges for every API request, billed on reported token counts.
- [ ] Calls Gemini through `google.golang.org/genai` and authenticates with ADC alone.
- [ ] All unit tests pass offline using fixtures.

---

## Handoff Log

_(Fill on completion: record chosen stretch thresholds, listening evaluations, and implementation details.)_

### T2.1: Segmentation client (done 2026-09-07)

Client lives in `internal/gemini/segment.go`. It sends the proven prompt byte verbatim (658 bytes) and parses the JSON schema into `types.Segment` with strict validation. One `Charge` (kind segment, 658 units) records only after full parse. Auth uses `golang.org/x/oauth2` v0.36.0 through `FindDefaultCredentials` (owner-approved stack exception). It covers `authorized_user` ADC and the GCE metadata server. Live evidence ran three probe passes: gemini-2.5-flash returned 11 segments, gemini-3.8-flash returned 10, and the remediated probe passed twice with 8. Every pass billed $0.0000658. The probe asserts structure (ordered ids, clip bounds, no overlaps, speaker coverage) and prints a drift table instead of exact counts. The `.env` model id carries a `google/` prefix that 404s on Vertex, so probes export working values. Next agent: T2.2 consumes `endpointURL`, `postGenerate`, and `envelopeText` from this file.

### T2.2: Duration-budgeted translation (done 2026-09-07)

Client lives in `internal/gemini/translate.go`. Normal and shorter prompts render byte identical to the Python f-string. The new fuller prompt diagnoses the empty slot, names the budget, demands complete natural phrasing, and bans padding. Each successful request records one `ChargeTranslate` with `TakeID` from `SegmentID`. Live evidence used segment 8 under the proven config (gemini-2.5-flash, global). All three modes returned natural Malayalam with itemized charges ($0.0000509, $0.0000479, $0.0000529). The as-configured `.env` model id 404s on Vertex. The probe records that instead of masking it. Next agent: T2.6 consumes `Translator` with `ModeShorter` and `ModeFuller` for repairs.

### T2.2dev: Adopt the Gen AI SDK and ADC (done 2026-09-07)

Both Gemini clients now call `google.golang.org/genai` v1.71.0. Constructors are
`NewSegmenter(cfg, rec, card, client *genai.Client)` and the same shape for
`NewTranslator`. A nil client builds BackendVertexAI with Project and Location.
It passes no Credentials, no APIKey, and no HTTPClient. The SDK then calls
`credentials.DetectDefault`.

`VERTEX_OPENAPI_BASE_URL` is retired. The SDK uses the native Vertex surface.
`GEMINI_API_KEY` is no longer a credential path. Config requires
`GOOGLE_CLOUD_PROJECT`. `GOOGLE_APPLICATION_CREDENTIALS` stays optional.

This amends frozen T1.2. Gemini `Charge` values bill from
`UsageMetadata.PromptTokenCount` and `CandidatesTokenCount`. Synthesize stays
per character. `Charge.Total` sums both token kinds plus character units.
`SegmentPerInputChar` remains on the rate card for T1.5 fixture math.

Default location is now `global`. `us-central1` 404s `gemini-3.8-flash`.
The loader strips a `google/` model prefix. That prefix 404s on the native
surface. Live probes force `config.DefaultGeminiModel` and
`config.DefaultGoogleLocation`. They unset `GEMINI_API_KEY`. They send
`testdata/clip.mp4` as `video/mp4`. Derived MP3 timestamps overran the clip.

What surprised us. HTTPClient set skips DetectDefault, which is the offline
httptest seam. A fake `GenerateContent` interface pins arguments without ADC.
The first `global` audio pass returned `duration_ms` 2500 against a 63700 ms
span. Video input plus a three-attempt structure retry made the probe stable.

Next agent. Do not import `golang.org/x/oauth2`. Do not rebuild REST JSON
types. T2.3 is Chirp, not Gemini. T2.6 consumes `Translator`. T4.2 must persist
prompt and candidate token fields, not character length. Do not inherit
`.env`'s `google/` model id.

Live evidence (ADC, no API key, model `gemini-3.8-flash`, location `global`,
project `nryn-personal`):

- Segmentation: 8 segments, promptTokens 7227, candidateTokens 800, total
  $0.00156405. Speakers Suni Williams and Mark Vande Hei. Clip bounds held.
- Translation of golden segment 8:
  - normal: "അതുപോലെ തന്നെ, പുറത്തുനിന്നൊരു ബലം പ്രവർത്തിക്കാത്തിടത്തോളം, ചലിച്ചുകൊണ്ടിരിക്കുന്ന ഒരു വസ്തു ആ ചലനത്തിൽ തന്നെ തുടർന്നുകൊണ്ടേയിരിക്കും." prompt 107, candidate 40, $0.00004005
  - shorter: "പിന്നെ, ബാഹ്യബലമില്ലെങ്കിൽ ചലിക്കുന്ന വസ്തു ചലിച്ചുകൊണ്ടേയിരിക്കും." prompt 99, candidate 22, $0.00002805
  - fuller: "അതുപോലെ തന്നെ, പുറത്തുനിന്നുള്ള മറ്റൊരു ബലം അതിൽ സ്വാധീനം ചെലുത്താത്തിടത്തോളം കാലം, ചലിച്ചുകൊണ്ടിരിക്കുന്ന ഏതൊരു വസ്തുവും അതേ ചലനാവസ്ഥയിൽ തന്നെ തുടരാനാണ് പ്രവണത കാണിക്കുന്നത്." prompt 110, candidate 54, $0.0000489

### T2.3: Synthesis client (done 2026-09-07)

Client lives in `internal/tts`. Production calls `cloud.google.com/go/texttospeech`
v1.22.0 through `TTSClient`. A nil client is `texttospeech.NewClient(ctx)` on ADC.
No API key, no key file, no hand-rolled REST.

`Assign` gives Suni `ml-IN-Chirp3-HD-Achernar` and Mark `ml-IN-Chirp3-HD-Achird`.
Unknown speakers error. Requests send `languageCode=ml-IN`, LINEAR16, and leave
`SampleRateHertz` unset. That is the 16 kHz Python defect. LINEAR16 already
carries a WAV header. Write those bytes once to the caller-chosen `OutPath`.
Do not wrap them. Do not overwrite.

One `ChargeSynthesize` per successful write, billed on rune count. Token fields
stay zero. `NewFixtureSynthesizer` copies `testdata/takes/seg_{id}_try1.wav`
and still bills. Offline tests never need ADC.

What surprised us. The official proto already documents the WAV header on
LINEAR16. The SDK client method takes `gax.CallOption`, so a thin adapter is
the test seam. Malayalam rune count is not byte length. `go mod tidy` pulled
`oauth2` as an indirect of the TTS module. Keep it off the direct require list.

Next agent. T2.6 and T2.7 should not re-derive the voice table, the native-rate
omission, or the WAV-header rule. Language is hardcoded `ml-IN` on the
synthesizer. `Assign` already takes a language code. German or Spanish needs a
request-field contract change. T2.7 chooses `OutPath` names (`seg_1_try1.wav`).
This package writes that path once.

### T2.4: Measure (done 2026-09-07)

Measure lives in `internal/fit/measure.go` as
`Measure(path string, slot time.Duration) (types.Fit, error)`. It probes with
`media.Duration` and builds the result with `types.NewFit`. It does not
recompute Measured minus Slot by hand. An empty path, a missing file, or an
ffprobe failure returns a zero Fit and an error.

`go test ./internal/fit/` measured every `testdata/takes/seg_N_try1.wav`
against `metrics.json` slots. All eight golden rows matched, including segment 8
at Slot 7110 ms, Measured 4200 ms, Delta -2910 ms, TooShort true, Fits false.
Stretched pins also matched: `seg_3_stretched.wav` 5338 ms and
`seg_4_stretched.wav` 5662 ms.

Next agent. T2.5 consumes `Measure` to re-probe after `atempo`. Do not stub
Duration. Do not treat a take inside 8 percent as a one-sided overrun check.

### T2.5: Two-sided stretch repair (done 2026-09-07, updated after remediation round 4)

The repair lives in `internal/fit/stretch.go`. Its exported surface consists of
functions `PlanStretch`, `PlanStretchWithLimits`, `Stretch`, `StretchWithLimits`,
and `DefaultStretchLimits`. It includes types `StretchLimits`, `StretchPlan`,
and `StretchResult`. It exports constants `MinAtempoRatio`, `MaxAtempoRatio`,
`DefaultMaxStretchShort`, `DefaultMaxStretchLong`, `MaxStretchLimit`, and
`StretchDeadBand`. It defines thirteen sentinel errors. No exported function
takes an atempo ratio. That door stayed shut on purpose, and the reason sits
under "The bypass" below.

**Two budgets, not one. Short 0.05, long 0.08.** The owner ruled on this in
remediation round 1. Slowing speech sounds more artificial than speeding it, so
the short side carries the tighter budget. Where the number is a guess, the
error falls toward a Gemini rewrite, which sounds right. It does not fall
toward a stretched take that sounds wrong and logs as repaired.

**The 5 percent is UNVERIFIED BY EAR.** It comes from general speech time
tailoring practice. No measurement in this repository decides it and no
listener has ruled on it. The listening ladder below is the instrument that
will settle it. Read the number as reasoned, never as measured.

**The budget is an input, not a baked constant.** `DefaultMaxStretchShort` and
`DefaultMaxStretchLong` name the project defaults. A caller passes its own
through `StretchLimits`. Tolerance varies by language and by content slice,
because Malayalam, German and Spanish do not share a syllable rate. A fast
passage also tolerates less than a slow one. The caller resolves project
default, then language, then content slice, narrowest wins. That resolution
belongs to T2.6 or T2.7. `stretch.go` receives an already resolved value and
stays ignorant of where it came from.

`MaxStretchLimit` caps any resolved budget at 0.10. Without it a caller could
resolve 0.45 and hand segment 8 back to atempo, which reopens the defect this
project exists to close. Remediation round 2 moved the ceiling down from 0.15.

**The 0.10 ceiling is REASONED, NOT HEARD.** It carries the same honesty label
as the 5 percent short default. Nobody has listened to anything yet. The
listening ladder below reaches ratio 0.9000 at its lowest rung, a 10 percent
correction, and the ceiling stops there because the project chose to respect
the ladder's reach. That is a choice, not a judgement about the audio.

**Only a listener ruling widens the ceiling.** Rendering more rungs does not.
A render produces a file to judge and settles nothing on its own. Three new
files at 0.86, 0.85 and 0.84 would license nothing. Anyone who widens the
ceiling records the listener's verdict in this entry, beside the ladder,
before the constant moves.

**The ceiling binds one call, never a take.** `internal/fit` holds no
provenance and cannot tell a fresh synthesis from its own earlier output. Six
legal calls under a 0.10 budget on both sides walked `seg_8_try1.wav` from
4200 ms to 7101 ms against its real 7110 ms slot. Every step returned a nil
error and landed inside its own supplied slot. The finished artifact reports
`Fits()` true and replans `none`, and the total correction is the 40.9 percent
underrun this project exists to refuse. The caller owns the running total, and
the "Next agent" note below says what T2.6 and T2.7 must do about it.

**This package does not read `types.FitThreshold`.** The two constants answer
different questions. `Fits()` asks whether a take ships as it stands. The
stretch budget asks whether a take repairs locally instead of paying for a
rewrite. A take now reports `Fits` true at 6 percent short and still plans a
rewrite. P1 and T2.6 reconcile the two deliberately. Nobody should quietly
align them.

**This package derives the delta and never reads `types.Fit.Delta`.**
Remediation round 3 closed this. `types.Fit` exports `Slot`, `Measured` and
`Delta`, and `Delta` names a value the other two already decide. A composite
literal may omit it, and Go then fills it with zero. Round 3 reached the
2026-09-07 report in one exported call.
`fit.PlanStretch(types.Fit{Slot: 7110ms, Measured: 4200ms})` returned
`repair=none` for a take running 40.9 percent short, and a stale
`Delta: -100ms` sent that same take to atempo at ratio 0.590717.
`PlanStretchWithLimits` now computes `Measured` minus `Slot` and routes on
that, so a literal cannot lie to it. `internal/ledger/takes.go` and
`internal/fixtures/load.go` already re-derived the same invariant, which
makes derive the house rule rather than a local choice. Whether the check
belongs on `types.Fit` itself is a P1 question, recorded in
`dev-diary/adversarial-review/t2.5-remediation-round3.md`.

**The eight fixture rows under the new budgets.** Every duration below comes
from `ffprobe` run outside Go, not from a value the package printed.

| seg | slot ms | take ms | delta ms | delta % | side | budget | band ms | repair | ratio |
|---|---|---|---|---|---|---|---|---|---|
| 1 | 1820 | 1680 | -140 | -7.692 | short | 0.05 | 40 | rewrite | n/a |
| 2 | 5480 | 5320 | -160 | -2.920 | short | 0.05 | 40 | atempo | 0.970803 |
| 3 | 5320 | 5720 | +400 | +7.519 | long | 0.08 | 40 | atempo | 1.075188 |
| 4 | 5660 | 5920 | +260 | +4.594 | long | 0.08 | 40 | atempo | 1.045936 |
| 5 | 4840 | 4720 | -120 | -2.479 | short | 0.05 | 40 | atempo | 0.975207 |
| 6 | 4630 | 4440 | -190 | -4.104 | short | 0.05 | 40 | atempo | 0.958963 |
| 7 | 7040 | 6800 | -240 | -3.409 | short | 0.05 | 40 | atempo | 0.965909 |
| 8 | 7110 | 4200 | -2910 | -40.928 | short | 0.05 | 40 | rewrite | n/a |

Exactly one segment moved when the short budget fell from 0.08 to 0.05.
Segment 1 runs 7.7 percent short and now routes to a fuller rewrite. Segment 8
routed to a rewrite before the change and still does.

**Where the six repairs land.** Rebuilt with `ffmpeg -filter:a atempo=R` and
measured with `ffprobe`, both outside Go.

| seg | ratio | landed ms | slot ms | miss ms |
|---|---|---|---|---|
| 2 | 0.9708 | 5469 | 5480 | -11 |
| 3 | 1.0752 | 5333 | 5320 | +13 |
| 4 | 1.0459 | 5663 | 5660 | +3 |
| 5 | 0.9752 | 4832 | 4840 | -8 |
| 6 | 0.9590 | 4621 | 4630 | -9 |
| 7 | 0.9659 | 7031 | 7040 | -9 |

The worst miss is 13 ms. `maxLandingMiss` in the test file pins a 20 ms
ceiling that reads no constant the package can move. `landedMs` in
`stretchGoldens` pins each landing to 2 ms. Neither assertion asks the dead
band what the dead band allows.

**The dead band stays 40 ms and absolute.** ffmpeg quantises to its own
analysis windows, so the residual does not scale with take length. Broadcast
practice tolerates roughly 45 ms of audio lead. A relative band breaks at both
ends. One percent of the 7040 ms slot crosses the perceptual line, and one
percent of the 1820 ms slot sits under the residual on that very take.

**Each side of the band reads only its own budget.** `deadBand` takes the
slot, the signed delta and the limits. Round 1 capped it at the minimum of the
two budgets, so editing the short side moved the long side. On a 600 ms slot,
tightening the short budget to 0.04 alone now leaves the long band at 40 ms and
leaves a long take's plan at `none`. The mirror holds too.

**Every failure leaves the output path empty.** A refusal never starts ffmpeg.
A stretch that ran and missed gets its output removed before `Stretch` returns
`ErrLandedOutside`. Round 1 left a wrong length WAV under the take name, which
`ErrOutputExists` then blocked forever. A removal that itself fails joins the
returned message, so the caller learns that a file survived.

**A nil error means a file exists at `Out`, and takes stay immutable under
concurrency.** Remediation round 3 closed this. `checkPaths` only stats, so
round 3 raced two calls on one output path and broke both guarantees. Ten of
ten trials with different slots had one call return a nil error and a
populated `After`. `os.Stat` then found nothing at `res.Out`, because the
other call's cleanup removed it. Ten of ten same-slot trials had both calls
return nil against one file, and `ErrOutputExists` never fired. `applyStretch`
now claims the output path with an exclusive create before ffmpeg starts. Two
racing calls give one winner and one `ErrOutputExists`, and the loser writes
nothing and removes nothing. The cleanup can only delete a file this call
created. The limit is exact. The guarantee holds against every caller that
goes through `internal/fit`. A program that writes or deletes the output path
behind this package's back can still break it.

**Thirteen sentinels now cover every exit round 2 walked.** `ErrPathRequired`,
`ErrSameFile`, `ErrSourceMissing` and `ErrLimits` joined the seven from round
1. Remediation round 2 found four more exits with no sentinel: a directory
given as a source, a source ffprobe cannot decode, a missing output
directory, and an extensionless output path. `ErrSourceUnreadable` now covers
the first two and `ErrRenderFailed` covers the last two, and both wrap the
underlying ffmpeg or ffprobe message. A missing output directory now fails
when the claim tries to create the file, one step before ffmpeg starts, and it
still reports `ErrRenderFailed`. Writing the output failed either way, which
is what that sentinel names. A missing take still reports
`source take does not exist: <path>` rather than a raw `ffprobe` string that
sends the reader to the wrong tool. These sentinels cover every failure path
this package has walked and measured, not every failure path that exists. An
unclassified operating system error still returns a plain error, so T2.6
should keep a default arm rather than assume the switch is exhaustive.

**Never put a wrapped error's text in front of a reader.** The text inside
`ErrRenderFailed` and `ErrSourceUnreadable` is raw tool output and it carries
no bound. Round 4 measured one `ErrRenderFailed` from an extensionless output
path at 2451 bytes across 18 lines on ffmpeg n9.0.1, opening with the full
build configuration banner. ffmpeg puts the actual reason last, so the opening
lines say nothing a reader wants. A missing output directory fails earlier at
`claimOutput`, returning 128 bytes on 1 line with no banner. Two sinks
downstream invite exactly this mistake. The doc on
`api.ProgressEvent.Sentence` calls it a complete natural-language update, and
`ledger.takeRow.RepairDetail` holds a plain string column. Branch on
the sentinel, write your own sentence from it, and log the wrapped text where
an operator reads it. `internal/fit` wraps rather than trims, because the
reason sits after the banner and a blind trim would drop the reason.
Bounding the tool output belongs to `internal/media` and T0.3, which build
the string.

**`ErrRatioRange` is real but unreachable through this package's exported
surface today.** `MaxStretchLimit` confines every policy derived ratio to
roughly 0.90 through 1.10, well inside the atempo filter's own 0.5 to 2.0
range. Round 2 swept the widest budget this package accepts and found the
guard never fires through `Stretch` or `StretchWithLimits`. It stays as
defence in depth, not as a branch T2.6 should expect to take.

**The bypass, and why no exported symbol takes a ratio.** Round 1 exported
`StretchRatio`. A reviewer called it on `seg_8_try1.wav` at that take's own
corrective ratio of 0.5907. It returned a nil error, wrote a 7085 ms file
against a 7110 ms slot, and `PlanStretch` then called that output `none`. That
is the 2026-09-07 defect reproduced through the repair package itself. In round
4, `stretchRatio`, `renderCandidate`, and `prepareRatio` moved into
`stretch_test.go`. Sibling files in `package fit`, like `internal/fit/rewrite.go`
or `internal/fit/loop.go`, cannot compile against them in production builds.

Go package scope cannot prevent sibling files from calling unexported functions
`applyStretch` or `renderStretch`. The only defense against sibling abuse is
the written rule. Authors of T2.6 and T2.7 must not call `applyStretch` or
`renderStretch` directly. Route all repairs through `Stretch` or
`StretchWithLimits`.

**The listening ladder.** The WAV files stay in gitignored
`scratch/t25-listening/`, so this table is the tracked record. Every duration
comes from `ffprobe`. The slowed column measures each candidate against the
untouched take, which is the quantity a listener judges.

Segment 1, slot 1820 ms, take 1680 ms, 7.7 percent short.
Segment 6, slot 4630 ms, take 4440 ms, 4.1 percent short.

| ratio | seg 1 ms | seg 1 slowed | seg 6 ms | seg 6 slowed |
|---|---|---|---|---|
| 0.9900 | 1689 | +0.54% | 4478 | +0.86% |
| 0.9800 | 1706 | +1.55% | 4525 | +1.91% |
| 0.9700 | 1717 | +2.20% | 4563 | +2.77% |
| 0.9600 | 1739 | +3.51% | 4615 | +3.94% |
| 0.9590 | n/a | n/a | 4621 | +4.08% |
| 0.9500 | 1753 | +4.35% | 4657 | +4.89% |
| 0.9400 | 1771 | +5.42% | 4711 | +6.10% |
| 0.9300 | 1788 | +6.43% | 4756 | +7.12% |
| 0.9231 | 1804 | +7.38% | n/a | n/a |
| 0.9200 | 1807 | +7.56% | 4813 | +8.40% |
| 0.9000 | 1845 | +9.82% | 4913 | +10.65% |

The 0.9231 row is segment 1's full correction and the 0.9590 row is segment
6's. Rebuild the audio with this command, which needs ffmpeg and ffprobe only
and makes no network call.

```
AJILAMU_LISTENING_DIR=$PWD/scratch/t25-listening go test -count=1 -run ListeningCandidates -v ./internal/fit/
```

Listen down each ladder and say where the slow down starts to sound wrong.
That percentage is the value `DefaultMaxStretchShort` should take. Paste the
verdict into this entry when it arrives, because `scratch/` does not survive a
commit.

The same verdict is the only thing that moves `MaxStretchLimit`. A wider
ceiling needs a listener who heard a wider correction and accepted it. More
rendered rungs are not that.

**Verification.** `go test -count=1 ./internal/fit/` passes, `go vet
./internal/fit/` is clean, and `gofmt -l internal/fit/` prints nothing. The
suite also passes under `unshare -rn`, which proves it makes no network call.
It passes with `TMPDIR` inside the checkout, which crosses a real filesystem
boundary on this machine, because `/tmp` is tmpfs and `/home` is btrfs. It
also passes under `-race`, which round 3's concurrency test needs.

Next agent. T2.6 and T2.7 own threshold resolution. Build the layered lookup
outside this package and pass `StretchLimits` in. Do not call `applyStretch`
or `renderStretch` directly from sibling files. Route all repairs through
`Stretch` or `StretchWithLimits`. Do not add a language argument to `Stretch`.
Do not read `types.FitThreshold` to decide a repair. Do not build a
`types.Fit` by hand and expect this package to read its `Delta`, because it
derives that value itself. Cap the total correction a take receives across
attempts, because `MaxStretchLimit` binds one call and six legal calls reach
40.9 percent. Refuse a source take that is already a stretched output, because
nothing inside `internal/fit` can tell.
Expect `ErrUnderrunTooLarge` on segment 1 now. Remediation round 2 corrected
this entry. `testdata/expected/metrics.json` does not record segment 1 as a
fit. It records `"repair": "none"` and `"outcome": "short"`, so the Python
run also declined to repair that take. The label differs from the Go
decision and the outcome does not. The rows that genuinely need a note are
segments 2, 5, 6 and 7. `metrics.json` records `"repair": "none"` for all
four while this package now plans `atempo`. T2.7 also chooses take names, and
`checkPaths` refuses a second write to any name, so a scheme like
`seg_3_try2_stretched.wav` avoids `ErrOutputExists`.

### T2.6: Rewrite repair and third attempt (implemented 2026-09-08)

The rewrite repair lives in `internal/fit/rewrite.go`. It exports `RepairLine`,
`NewRepairer`, and `DefaultTakeName`. It exports types `RewriteConfig`,
`LineRepairer`, `Repairer`, `LineAttempt`, and `LineResult`. It exports sentinels
`ErrAlreadyStretched`, `ErrTotalStretchExceeded`, `ErrTranslatorRequired`, and
`ErrSynthesizerRequired`.

**Three attempts per line.** The repair loop executes up to three consecutive attempts.
Attempt 1 translates with normal mode unless the caller provides an initial take or text.
If an attempt fits inside the dead band, the loop succeeds with no repair.
If duration enters the repair window, the loop calls `StretchWithLimits`.
A successful stretch returns `RepairAtempo` with the stretched take.

**Direction-aware rewrites.** An overrun beyond the stretch budget requests `ModeShorter`.
An underrun beyond the stretch budget requests `ModeFuller`.
The loop re-evaluates direction after every attempt from measured duration minus slot.
Over-corrections flip direction automatically for the next attempt.

**Flagged line handling and closest take.** If three attempts fail to fit, the repairer flags the line.
It sets `Flagged` to true.
The pipeline selects the closest take by finding the attempt with minimum absolute delta.
The result records signed deltas for all attempts.
It generates concise notification copy explaining the failure and naming the closest take.

**Guards and limits.** Each retry synthesizes fresh audio and stretches only fresh takes.
The loop refuses to re-stretch an already stretched take.
It validates caller stretch limits against `MaxStretchLimit`.
It never calls unexported sibling functions `applyStretch` or `renderStretch`.
All stretching routes strictly through `StretchWithLimits`.
It derives signed miss directly and ignores unverified delta fields.

**Clean notification sentences.** Sentinel errors from `StretchWithLimits` map to bounded sentences.
User notifications never include raw ffmpeg or ffprobe output banners.
Each notification sentence stays under 30 words and uses active voice.

**Next agent.** T2.7 orchestrates the dubbing pipeline across all segments.
Call `RepairLine` with `types.Segment` and `RewriteConfig`.
Use `DefaultTakeName` or supply a custom `PathBuilder` for immutable WAV naming.
Log itemized charges through ledger recorders after each API call.
