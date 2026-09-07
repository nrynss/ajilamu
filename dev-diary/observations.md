# Field Observations: Pipeline Validation and Editorial Controls

**Date:** 2026-09-07
**Context:** First live run with the NASA 75-second clip, translated into Malayalam using Gemini, Chirp 3 HD, and ClickHouse Cloud. Concept validated. App not built yet.

**Status of this document:** Sections 1 and 2 record actual run measurements. Every number derives from ffprobe and volumedetect. Sections 3 through 6 define requirements from the run. Section 7 maps both onto candidate tracks.

Full working: [adversarial-review/2026-09-07-pipeline-and-claims.md](adversarial-review/2026-09-07-pipeline-and-claims.md).

---

## 1. What the First Run Actually Produced

The loop closed end to end. Gemini returned 8 segments. The system translated and synthesized each segment. It measured each against its slot and assembled two playable videos.

Measured per-segment result:

| seg | slot ms | take ms | delta | repair | outcome |
|---:|---:|---:|---:|---|---|
| 1 | 1820 | 1680 | −7.7% | none | short |
| 2 | 5480 | 5320 | −2.9% | none | fits |
| 3 | 5320 | 5720 | **+7.5%** | `atempo` 1.075 → 5338 | repaired |
| 4 | 5660 | 5920 | **+4.6%** | `atempo` 1.046 → 5662 | repaired |
| 5 | 4840 | 4720 | −2.5% | none | fits |
| 6 | 4630 | 4440 | −4.1% | none | short |
| 7 | 7040 | 6800 | −3.4% | none | fits |
| 8 | 7110 | 4200 | **−40.9%** | none | **broken, reported as fit** |

**The two `atempo` repairs delivered the real result.** Both overruns were caught and stretched. They landed inside their slots with no extra model call. The under-8% branch works as claimed.

**Segment 8 revealed a critical finding.** The pipeline left 2.9 seconds of dead air over a moving mouth. It recorded this failure as a clean fit. An earlier draft described this as fitting with room to spare. That was wrong, as explained in Section 2.1.

ClickHouse recorded one row per take with overrun metrics and fix types in real time. Assembly produced `dubbed_malayalam_clean.mp4` and `dubbed_malayalam_ducked.mp4`.

---

## 2. Defects Found in the First Run

These issues occurred in `scratch/validate_pipeline.py`. They are straightforward to fix, but they fundamentally redefine product requirements.

### 2.1 The fit loop has no lower bound

The code exited when `overrun_pct <= 0` with `fix_type: "none"`. It never measured, repaired, or highlighted an underrun. A take at 59% of its slot looked identical to a perfect fit.

The design previously described only overrun repairs. **Fit is a two-sided constraint.** A line ending early breaks lip sync just like an overrun. Furthermore, `atempo` slows audio as cheaply as it speeds audio.

Design consequence: The length bar in `ui-ux.md` Section 6 must draw short takes clearly. It needs a dedicated short-take state and plain wording.

### 2.2 The clean export deletes every non-dialogue sound

The assembly script allocated a silent zero buffer matching source length. It wrote takes only into dialogue slots. The clean mux then replaced the original track entirely.

Measured across 54.5s to 75.0s (the tail after the last line):

| Track | mean | max |
|---|---|---|
| original | −15.4 dB | −0.1 dB |
| clean export | **−91.0 dB** | −91.0 dB |
| ducked export | −35.4 dB | −20.1 dB |

The −91 dB measurement represents digital silence. The script discarded 20.5 seconds of NASA music and room tone, representing 27% of the clip. The same loss occurred between segments 6, 7, and 8.

The timeline also forced the take format onto the film audio. Every second kept was downgraded:

| Audio Stream | codec | rate | channels | bitrate |
|---|---|---|---|---|
| source `clip.mp4` | aac | 44100 | 2 | 132 kbps |
| a take | pcm_s16le | 16000 | 1 | none |
| **clean export** | aac | **16000** | **1** | **38 kbps** |
| ducked export | aac | 44100 | 2 | 195 kbps |

The ducked export avoided this only because `amix` takes its format from its first input.

**The output must carry whatever quality the film had.** Probe the source first. Adopt its format. Resample takes up into the background bed. Never downsample the film audio into the takes.

The 16 kHz mono demux serves Gemini analysis only. It must never reach the final mix.

The assembler needs a background bed layer that survives outside dialogue slots at native film quality.

### 2.3 Mixing attenuates the dub by 6 dB

The script ran this filter:

```
[0:a]volume=0.20[bg],[bg][1:a]amix=inputs=2:duration=first[outa]
```

`amix` normalizes by input count unless passed `normalize=0`. This halved both inputs.

The bed sat 20.0 dB below the original: 13.98 dB from volume plus 6.02 dB from division. The Malayalam voice took the same 6 dB cut. Consequently, the ducked export sounded quieter than the clean export.

The filter also applied a constant volume cut, not dynamic ducking. It pulled the bed down across the entire timeline, including empty gaps.

True ducking dips music under speech and recovers when speech stops.

### 2.4 The cost ledger double-counts and omits Gemini

The script accumulated costs with `+=` across attempts. Meanwhile, `record_take_clickhouse` wrote both rejected attempts and winning takes. Therefore, summing costs over-reported rewritten segments.

The calculation also counted TTS characters only. It completely ignored segmentation and translation fees. The run executed 9 Gemini calls without recording their costs.

Our interface must connect every displayed figure to real money. The ledger requires an explicit cost model before the user interface displays totals.

### 2.5 Overrun tails are clipped silently

The assembly script wrote takes into a shared buffer in ascending order. When an overrun take ran past its slot, the next take overwrote its tail. The speech cut off mid-word without recording an error.

### 2.6 Provenance is best-effort

The logging function caught exceptions and printed warnings. A failed insert did not stop execution, retry, or queue.

A version control system cannot support time travel or branching when commits fail silently. The system must queue and reconcile writes.

Additionally, the `default.takes` schema existed only in ClickHouse Cloud. We must check in `sql/schema.sql` to keep the ledger reproducible.

The initial script also lacked `commit_id`, parent pointers, and waveform peak arrays.

---

## 3. Where Documents Ran Ahead of Code

We record these discrepancies to reconcile the design intentionally.

| Project Claim | Actual First Run |
|---|---|
| Multimodal pass extracts on-screen text | Uploaded audio only (`speech.mp3`). No video frames sent. Voice inferred speaker names. |
| Voices match speaker gender and emotion | One hardcoded voice used for all speakers. Emotion prompt bypassed TTS. |
| Flags lines after three attempts | Code ran two attempts only. |
| Uses Gemini 3.8 Flash | Pinned `gemini-2.5-flash` in code URLs. Ignored model environment variable. |

---

## 4. Hard Requirements from the Run

### I. Each Segment Must Be a Discrete WAV File

The timeline arranges discrete takes, not an opaque audio file.

* The system generates, stores, and serves every take as an independent WAV asset (`seg_1_try1.wav`, `seg_1_stretched.wav`).
* Creators solo lines, preview prior takes, adjust timing, and re-record sentences.
* Merging audio into a single stream destroys line-level editing. The master mix must assemble on demand.

Discrete WAV storage kept takes intact during earlier tests, even when timeline assembly failed.

### II. Manual Controls Are Critical for Speaker Attribution

The test run proved why automated attribution needs manual oversight. Segment 6 transcribed "Mark Van der High". Segments 7 and 8 transcribed "Mark Vande Hei".

Gemini identified the speaker field correctly, but misspelled the name in the sentence. The synthesizer spoke the misspelling aloud in Malayalam.

* The system must never assume artificial intelligence output is correct.
* Creators must edit segment boundaries, speaker assignments, and transcribed text directly on the timeline.
* We provide draggable boundary handles and quick speaker selectors.

### III. The Editor AI Command Bar

Creators need conversational micro-adjustments alongside visual timeline editing.

* Creators type operational commands directly into the editor.
  * "move wav 1 to 0:0005 to right."
  * "shift line 3 right by 200ms."
  * "change speaker for line 7 to Mark."
  * "shorten line 4 by 0.5s."
* The model parses conversational commands into structured mutations.
* Deterministic Go code executes arithmetic and audio positioning.

---

## 5. Git for Video Production in ClickHouse

Video editors usually rely on volatile in-memory undo stacks. When tabs crash, edits disappear. ClickHouse provides an immutable audit trail for video dubbing.

### What Goes into ClickHouse

1. **The Commit DAG:** `commit_id`, `parent_commit_id`, `version_number`, and `created_at`.
2. **Action Provenance:** actions (`segment_created`, `boundary_nudged`, `speaker_reassigned`, `take_rendered`, `atempo_stretched`, `line_rewritten`, `user_command`), author (`agent`, `command_bar`, `manual_ui`), and prompt text.
3. **State Diffs:** millisecond boundaries and text before and after edits.
4. **Cost and Timing Ledger:** duration before and after, signed fit delta, repair method, and itemized model expenses.
5. **Waveform Peak Vectors:** `Array(UInt8)` storing 64 to 128 normalized peaks for instant UI rendering without audio reads.
6. **Asset Reference:** content-addressed path to the discrete WAV file.

We check in `sql/schema.sql` so the ledger remains fully reproducible.

### What Stays on Disk (Git LFS Model)

ClickHouse avoids storing raw binary audio or video. Large blobs degrade columnar query performance. The local filesystem stores immutable media assets.

### Benefits of This Architecture

* **Instant Time Travel:** Reconstruct project state at any previous point. Revert one sentence without discarding other work.
* **A/B Takes:** Branch a language track to compare literal translations against colloquial phrasing.
* **Explainable Edits:** Every automated speed adjustment or rewrite preserves a permanent explanation.

---

## 6. Cloud Ingestion and File Uploads

* **Streaming multipart upload (`/api/dubs/new`):** Streams uploaded video directly to persistent storage. It accepts an optional background music track.
* **Pre-packaged Sample Mode:** A prominent button copies the NASA 75-second clip into a fresh project and starts processing immediately.
* **Asynchronous Execution:** The Go server demuxes audio with ffmpeg, runs the pipeline asynchronously, and streams progress events to the client.

---

## 7. Track Breakdown

The project breaks down into sequential phases:

* **Contracts:** Take, segment, and commit types, signed fit delta, cost model, `sql/schema.sql`, and test fixtures.
* **Fit Loop:** Go port of the validated loop, lower bound check, third attempt, and per-attempt cost tracking.
* **Assembler:** Background audio preservation, proper mixing without attenuation, dynamic ducking, and overrun tail management.
* **Ledger:** ClickHouse event writes, commit DAG, waveform peak vectors, and resilient queueing.
* **Workspace:** Svelte 5 shell, timeline tracks, length bar canvas with underrun support, and rail panel.
* **Editing:** Boundary handles, speaker selectors, text correction, and the AI command bar.
* **Ingestion and Ship:** Multipart uploads, sample mode, progress streaming, and GCE deployment.
