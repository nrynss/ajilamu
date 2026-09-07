# P3: Assembler (Track B)

```yaml
id:       P3
size:     L
branch:   phase/p3-assembler
requires: [P1, T0.3, T0.4]
blocks:   P7 (export), P8
parallel: medium
runs-parallel-with: P2, P4, P5, P6, P7
```

**Goal:** Correct audio mixing issues identified during pipeline validation. Preserve background audio beds, implement dynamic ducking, and enforce explicit policies for overrun takes.

**Zero model dependencies:** This track requires zero API calls. All tasks run offline against fixture audio in `testdata/`.

---

### T3.1: Bed layer ★
```yaml
requires:   T1.1, T0.3, T0.4
fixture-ok: yes
size:       L · frontier
owns:       internal/assemble/bed.go, internal/assemble/bed_test.go
status:     done
```
Preserve original background audio outside dialogue slots. The initial script muted 20.5 seconds of soundtrack after the final line by replacing audio streams wholesale.

Create a full-length background bed using the source audio. If creators supply a separate music track, use that track directly without ducking.

**Match source audio format:**
The source video uses 44.1 kHz stereo audio. Synthesized takes produce 16 kHz mono audio.

Probe source format and adopt its sample rate, channel count, and layout. Never degrade the film to match speech takes.

Resample speech takes up to match the background bed. Never downsample the film soundtrack.

**Done when:** Assembling an empty timeline reproduces original audio levels within 0.5 dB, and output matches source 44.1 kHz stereo parameters.

---

### T3.2: Take placement and overrun policy ★
```yaml
requires:   T3.1
fixture-ok: yes
size:       M · frontier
owns:       internal/assemble/place.go, internal/assemble/place_test.go
status:     done
```
Place each final take at its segment start offset over the background audio bed.

Establish an explicit policy when a take overruns its slot and encroaches on the next take.

The system allows audio to extend into trailing silence gaps. If speech still collides, crossfade or truncate cleanly while logging the event. Never cut speech mid-word silently.

**Done when:** Synthetic long takes follow the defined policy and log truncation metrics, while the 8 test takes place accurately over the preserved bed.

---

### T3.3: Ducking ★
```yaml
requires:   T3.2
fixture-ok: yes
size:       L · frontier
owns:       internal/assemble/duck.go, internal/assemble/duck_test.go
status:     claimed:t33-impl-cursor
```
Implement dynamic ducking using ffmpeg filters.

The original script halved audio volume because `amix` normalizes inputs by default. It also applied constant volume attenuation across the entire clip.

Use `sidechaincompress` keyed on dialogue speech so the background music dips during speech and recovers during silence.

Pass `normalize=0` to prevent unintended 6 dB volume drops on dubbed speech. Skip ducking when creators upload separate background music tracks.

**Done when:** Music measures at full volume during silent intervals, dips measurably under speech, and dubbed voices maintain full volume.

---

### T3.4: Export ★
```yaml
requires:   T3.3
fixture-ok: yes
size:       S · mid
owns:       internal/assemble/export.go
status:     not-started
```
Multiplex assembled audio with untouched video streams using `-c:v copy` and `-map 0:v -map 1:a`. Video frames are never re-encoded.

Use descriptive output names like `dubbed_ducked.mp4` and `dubbed_replaced.mp4`. Avoid ambiguous labels like "clean" when describing replaced audio.

**Done when:** Exporting `testdata/` produces a 75.0-second video with intact video streams, active background audio tails, and properly mixed dialogue.

---

### T3.5: Waveform peaks
```yaml
requires:   T1.1, T0.3
fixture-ok: yes
size:       S · mid
owns:       internal/assemble/peaks.go, internal/assemble/peaks_test.go
status:     done
```
Compute 64 to 128 normalized `uint8` peak values per take during rendering. Store vectors in ClickHouse for instant timeline waveform rendering.

Pre-computing waveform peaks eliminates heavy audio file reads during user scrubbing.

**Done when:** Every take in `testdata/takes/` yields a consistent array of peak values reflecting actual waveform dynamics.

---

## Exit Criteria

- [ ] Background audio survives intact outside dialogue intervals.
- [ ] Tail audio measures within 0.5 dB of the original soundtrack.
- [ ] Implements dynamic ducking without volume attenuation on speech.
- [ ] Enforces and records take overrun collision policies.
- [ ] All unit tests pass offline using fixture data.

---

## Handoff Log

- **T3.1:** `BuildBed` preserves source timing, stream gaps, native sample rate, and channel layout.
  Separate music replaces the source bed and pads or trims to the video duration.
  `PrepareTake` resamples speech with unity-gain channel duplication. The filter uses
  `aresample=async=1:first_pts=0:min_hard_comp=0` to preserve short timestamp gaps.
- **T3.2:** `Place` writes each take onto a silent speech layer at its segment start.
  Fit keeps a take inside its slot. Gap uses trailing silence before the next take.
  Truncate drops colliding tail silence at the next take start.
  Crossfade overlaps colliding speech through the fade window and records it.
  `Overlay` mixes the speech layer onto the bed without ducking.
  Fixture segments 3 and 4 use Gap. Event start times round to the nearest 44.1 kHz frame.
- **T3.3:** `Duck` keys `sidechaincompress` on the speech layer. The mix uses `amix` with `normalize=0`.
  Compressor settings are threshold 0.016, ratio 12, attack 5 ms, and release 80 ms.
  The knee is hard. Detection is peak. Makeup and mix stay at 1.
  The published mix lasts as long as the bed.
  A one second pad keeps the compressor from dropping the tail.
  The first `amix` input is the original bed so `duration=first` holds that length.
  `SeparateMusic` skips the compressor and calls `Overlay`.
  Failed renders stay private. The output path never replaces an input.
- **T3.5:** `Peaks` decodes a take to float PCM and returns 128 `uint8` values.
  Each bin holds the max-abs of its frames, scaled to the take peak on 0 to 255.
  A silent take is all zeros. The same path returns an identical slice.
  T4.6 stores the vector on take rows. This task only computes it.
