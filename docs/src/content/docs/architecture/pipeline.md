---
title: The Dubbing Pipeline
description: Six stages from video upload to audio assembly with strict audio preservation.
template: doc
---

import Mermaid from '../../../components/Mermaid.astro'

Ajilamu transforms a source video into a dubbed final cut through a deterministic six-stage pipeline.

---

## Pipeline Overview

The pipeline balances automated AI generation with exact acoustic measurement.

<Mermaid code={`
graph TD
    S1[1. Watch\nGemini 3.8 Flash] --> S2[2. Translate\nHard Duration Budget]
    S2 --> S3[3. Render\nChirp 3 HD Audio]
    S3 --> S4[4. Measure\nffprobe Signed Delta]
    S4 --> S5[5. Repair\natempo or Rewrite]
    S5 --> S6[6. Assemble\nffmpeg Lossless Mux]

    classDef stage fill:#090d15,stroke:#f59e0b,color:#f8fafc
    class S1,S2,S3,S4,S5,S6 stage
`} />

---

## Stage Details

### 1. Watch
A single Gemini 3.8 Flash call inspects the source video. The model extracts speech boundaries, assigns persistent speaker names, and notes emotional register. The server demuxes a temporary 16 kHz mono track for model analysis. This low-resolution file never touches the final output mix.

### 2. Translate Under Budget
The translator sends each dialogue segment to Gemini. The prompt includes the exact slot duration in milliseconds. The model treats this duration as an inviolable budget, not as a casual target.

### 3. Render
Google Cloud Chirp 3 HD generates spoken audio for the translated text. The system maps detected speaker identities to voice profiles matching the speaker's documented gender. Each generation writes a fresh WAV file to disk.

### 4. Measure
The worker invokes `ffprobe` to read the exact duration of the synthesized audio file. The engine calculates a signed delta against the designated timeline slot:

$$\Delta = t_{\text{measured}} - t_{\text{slot}}$$

A negative delta indicates an underrun. A positive delta marks an overrun. Both states disrupt lip synchronization.

### 5. Repair
The fit evaluator checks the signed delta against tolerance limits. Minor misfits undergo time stretching. Larger discrepancies trigger automated prompt rewrites. See the **[Fit Loop](/ajilamu/architecture/fit-loop/)** guide for the complete repair algorithm.

### 6. Assemble
ffmpeg places approved takes onto the timeline. The assembler clears dialogue regions from the original track while preserving room tone, ambient sound, and background music elsewhere. Finally, ffmpeg muxes the audio over the original video stream.

---

## Audio Preservation Principles

Ajilamu adheres to strict acoustic integrity rules throughout the pipeline:

### Non-Dialogue Bed Preservation
Original audio outside speech slots remains untouched. Musical scores, room resonance, and incidental sound effects pass through to the final cut. The final export sounds grounded rather than sterile.

### Source Sample Rate and Layout
The export adopts the exact sample rate and channel layout of the original video. A 48 kHz stereo master stays 48 kHz stereo. The system never downsamples production audio to fit synthesized clips.

### Discrete Takes on Disk
The worker stores every synthesis attempt as an independent WAV file. The server never mutates or overwrites generated audio. This persistence enables line soloing, historical comparison, and non-destructive branch revisions.
