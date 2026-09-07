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

Read `GEMINI_MODEL` and `GOOGLE_CLOUD_LOCATION` from configuration rather than hardcoding values.

Accept media buffers rather than audio-only paths so future updates can pass video frames directly.

Emit a `Charge` per API invocation.

**Done when:** The client matches `testdata/segments.json` against the sample clip, and the mock double passes without network calls.

---

### T2.2: Duration-budgeted translation ★
```yaml
requires:   T1.1, T1.2, T0.2
fixture-ok: yes
size:       S · mid
owns:       internal/gemini/translate.go
status:     claimed:orchestrator-track-a
```
Translate dialogue under a time budget. Implement three modes: normal, shorter (for overruns), and fuller (for underruns).

The shorter-mode prompt proved effective in testing. For underruns, prompt for complete phrasing that fills the slot naturally without meaningless padding.

Pass emotional tone into this prompt.

**Done when:** All three modes return natural translations for segment 8, and each request emits an itemized `Charge`.

---

### T2.3: Synthesis client ★
```yaml
requires:   T1.1, T1.2, T0.2
fixture-ok: yes
size:       M · mid
owns:       internal/tts/chirp.go, internal/tts/voices.go
status:     not-started
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
owns:       internal/fit/measure.go
status:     not-started
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
status:     not-started
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
status:     not-started
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
- [ ] Records itemized charges for every API request.
- [ ] All unit tests pass offline using fixtures.

---

## Handoff Log

_(Fill on completion: record chosen stretch thresholds, listening evaluations, and implementation details.)_

### T2.1: Segmentation client (done 2026-09-07)

Client lives in `internal/gemini/segment.go`. It sends the proven prompt byte verbatim (658 bytes) and parses the JSON schema into `types.Segment` with strict validation. One `Charge` (kind segment, 658 units) records only after full parse. Auth uses `golang.org/x/oauth2` v0.36.0 through `FindDefaultCredentials` (owner-approved stack exception). It covers `authorized_user` ADC and the GCE metadata server. Live evidence ran three probe passes: gemini-2.5-flash returned 11 segments, gemini-3.8-flash returned 10, and the remediated probe passed twice with 8. Every pass billed $0.0000658. The probe asserts structure (ordered ids, clip bounds, no overlaps, speaker coverage) and prints a drift table instead of exact counts. The `.env` model id carries a `google/` prefix that 404s on Vertex, so probes export working values. Next agent: T2.2 consumes `endpointURL`, `postGenerate`, and `envelopeText` from this file.
