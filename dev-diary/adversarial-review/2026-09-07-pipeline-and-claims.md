# Adversarial Review: First Run vs. Stated Claims

**Date:** 2026-09-07
**Scope:** `dev-diary/*.md` against `scratch/validate_pipeline.py` and its actual outputs.
**Method:** Read the script, re-measured every take, and analyzed both exports with ffprobe and volumedetect.

The pipeline works. Eight segments translated, synthesized, and assembled into playable video. The system repaired two segments with time-stretching.

What follows identifies where documents claim more than the run delivered, and where the run produced errors.

---

## 1. The Application Is Not Built Yet (Blocking)

`project.md` specifies a Go server, ADK agent, Svelte 5 frontend, and an interactive timeline. None of this code exists yet.

The repository currently contains documentation, one Python script, sample media, and two ClickHouse archives. We have zero commits on master.

The competition timeline gives us sufficient room to build the interface.

**Do first:** Commit what exists, and move `validate_pipeline.py` into a tracked directory.

---

## 2. Clean Export Discards Background Sound (Severe)

The last dubbed line ends at 54,491 ms. The clip lasts 75,008 ms.

| Window 54.5s to 75.0s | mean | max |
|---|---|---|
| original | −15.4 dB | −0.1 dB |
| `dubbed_malayalam_clean.mp4` | **−91.0 dB** | −91.0 dB |
| `dubbed_malayalam_ducked.mp4` | −35.4 dB | −20.1 dB |

The −91 dB figure represents digital silence. Assembly wrote takes only into dialogue slots.

The clean mux replaced the original track entirely. Consequently, 20.5 seconds of NASA background audio vanished. The silence also cleared natural gaps between dialogue lines.

The export also downgraded sample rates. The timeline built at 16 kHz mono (38 kbps) rather than the source 44.1 kHz stereo (132 kbps).

Hard rule for Phase 3: The final output must preserve source audio quality and maintain the background track.

---

## 3. Mixing Attenuates the Dub by 6 dB (Severe)

The script ran this mixing filter:

```
[0:a]volume=0.20[bg],[bg][1:a]amix=inputs=2:duration=first[outa]
```

`amix` halves both inputs unless configured with `normalize=0`. Both streams lost 6 dB.

The Malayalam voice took this 6 dB cut. As a result, the ducked export played dialogue quieter than the clean export.

Use `amix=inputs=2:normalize=0` to preserve dialogue volume.

Additionally, `volume=0.20` lowers audio by 14 dB across the entire timeline. It does not perform dynamic ducking.

---

## 4. The Fit Loop Lacks a Lower Bound (Severe)

Segment 8 had a 7,110 ms slot, but generated a 4,200 ms take. It ran 40.9% short, creating 2.9 seconds of silence over moving lips.

| seg | slot | take | delta | repaired |
|---:|---:|---:|---:|---|
| 1 | 1820 | 1680 | −7.7% | none |
| 2 | 5480 | 5320 | −2.9% | none |
| 3 | 5320 | 5720 | **+7.5%** | stretch → 5338 |
| 4 | 5660 | 5920 | **+4.6%** | stretch → 5662 |
| 5 | 4840 | 4720 | −2.5% | none |
| 6 | 4630 | 4440 | −4.1% | none |
| 7 | 7040 | 6800 | −3.4% | none |
| 8 | 7110 | 4200 | **−40.9%** | none |

The initial script treated non-positive deltas as instant success. It never measured or surfaced underruns.

Underruns break lip sync just like overruns. The fit loop must enforce both upper and lower boundaries.

---

## 5. Cost Ledger Double Counts and Skips Gemini (Severe)

The script accumulated costs with `+=` across attempts. It logged both failed attempts and final takes into ClickHouse. Summing costs therefore over-reported rewritten lines.

Furthermore, the calculation counted speech synthesis characters only. It recorded zero dollars for Gemini segmentation and translation calls.

The ledger must track every model call accurately to maintain credibility with creators.

---

## 6. Documented Features Missing from Code (Moderate)

- **Multimodal Video Pass:** Code uploaded audio only (`speech.mp3`). The pass never sent video frames.
- **Voice Matching:** Code hardcoded one female voice for all speakers. Gender and emotional settings never reached the synthesizer.
- **Retry Count:** Code executed two attempts instead of three.
- **Model Versions:** Code hardcoded `gemini-2.5-flash` URLs instead of reading `GEMINI_MODEL`.

We must align documentation with code reality or implement missing branches.

---

## 7. Ledger Resilience Needs Hardening (Moderate)

The script logged takes using best-effort error handling. If an insert failed, it printed a warning and continued.

A version control system cannot drop events silently. The production service must queue writes and guarantee delivery.

Additionally, we must check in `sql/schema.sql` so anyone can recreate the database tables.

The database must also store commit identifiers, parent pointers, and waveform peak arrays.

---

## 8. Credential Management (Moderate)

The test script contained fallback passwords in code.

Store all credentials exclusively in `.env` or secret managers. Never commit fallback secrets into repository source code.

---

## 9. Additional Findings

- **Take Overruns:** The initial assembly script risked clipping overrun takes when writing sequentially to a shared buffer.
- **Transcription Errors:** The speech recognition model transcribed "Mark Van der High" instead of "Mark Vande Hei". This proves creators need visual editing controls to fix dialogue text.

---

## Action Plan

1. Commit repository structure and write `sql/schema.sql`.
2. Ensure secure credential handling without hardcoded fallbacks.
3. Fix audio mixing: Preserve background audio, disable `amix` normalization, and detect underruns.
4. Correct cost ledger math and include Gemini API calls.
5. Align codebase with documented retry and voice selection logic.
6. Build the Svelte workspace timeline and controls.
