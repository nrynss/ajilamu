# Ajilamu

**Give a film another tongue, and keep its rhythm.**

Aji Lhamu is the masked dance-drama of the Monpa people in western Arunachal Pradesh. It is six centuries old. Actors perform it over five days in dance, song, and pantomime.

It tells the Tibetan story of the Ramayana. The tale crosses language and borders. Masked players match their voices directly to physical movement.

Ajilamu does the same thing for modern creator video.

---

## Who This Is For

**Small creators.** One person or a small team publishes regularly. They want to reach new audiences in multiple languages.

These creators lack localization budgets, dialect coaches, vendors, and audio engineers.

Studios hire dedicated staff to fit dubbed dialogue into video slots. Solo creators understand the concept, but traditional studio tools cost too much.

Ajilamu closes this pricing gap.

Two principles shape this architecture:

1. **The same person speaks in every video.** A studio spreads history across forty voice actors. A solo creator uses one voice. The system learns their speech patterns quickly.
2. **Cost is never a rounding error.** A studio ignores forty cents. A creator dubbing their back catalogue notices every cent. The interface shows every price clearly.

---

## The Friction

Dubbing video is not solely a translation problem. Translation is the easier half.

**The dubbed line must fit the time slot.**

A dubbed line lands in a slot the original actor already defined. The mouth stops moving at a specific millisecond.

German runs 20 to 30 percent longer than English. Spanish runs 20 percent longer. Naively translated lines overrun the slot.

Editors usually rewrite lines shorter, record new takes, and measure repeatedly.

Ajilamu automates this loop: generate, measure, adjust, and retry.

---

## What It Does

> **This section is the target, not a description of working software.**
> We validated the loop below on 2026-09-07 against a 75-second clip.
> See [observations.md](observations.md) for measured results and known limits.

Provide a video and target languages. The system returns a dubbed video for each language and displays its audit trail.

1. **Watch the video.** A single multimodal pass extracts timestamped speech segments, speaker names, emotional tone, and on-screen text.
2. **Translate under a duration budget.** The system translates each line with slot length as a hard constraint.
3. **Render speech.** Google Cloud Chirp 3 HD voices synthesize audio matching speaker gender and emotional register.
4. **Measure.** Code calculates actual duration against the allocated slot and records a signed delta. A line that ends early fails fit just like a line that runs long.
5. **Repair the misfit.** The threshold checks absolute delta. The direction decides how `atempo` adjusts speed:
   * **Within 8 percent, long or short:** The system applies `atempo`. It speeds up an overrun or slows down an underrun. It sounds natural on speech, runs instantly, and incurs zero model fees.
   * **Beyond 8 percent long:** The system requests a shorter rewrite from Gemini.
   * **Beyond 8 percent short:** The system requests a fuller rewrite. Alternatively, it leaves the slot partly silent and informs the creator. It never reports a 40 percent underrun as a fit.
   * After three failed attempts, the system flags the line for manual creator review.
6. **Mux.** ffmpeg multiplexes audio and video streams into the final file. It preserves everything outside dialogue slots.

---

## Audio Handling

The system demuxes audio for analysis and preserves original video frames:

```bash
ffmpeg -i source.mp4 -vn -ac 1 -ar 16000 -c:a pcm_s16le speech.wav
ffmpeg -i source.mp4 -i dubbed.wav -c:v copy -map 0:v -map 1:a out.mp4
```

ffmpeg handles assembly, audio ducking, time-stretching, and multiplexing without third-party AI dependencies.

**The original background audio survives outside the dialogue slots.** A dubbed timeline places dialogue takes over the source audio. It never replaces the original audio entirely.

Music, room tone, and effects in the gaps between lines belong to the film. We keep them intact.

**The dub carries the film's quality, not the synthesizer's quality.** The output adopts the source sample rate, channel count, and layout.

We resample takes up into that background audio bed. We never downsample the film's own audio.

The small mono file extracted during analysis serves only analysis. It never enters the final mix.

**Ducking is dynamic, not a constant gain.** The background audio dips under the voice and recovers when the voice stops.

A fixed volume cut across the whole track is not ducking. It only makes the whole film quieter.

When the creator supplies a separate music track, we replace dialogue cleanly without ducking. Dynamic ducking serves as the fallback when separate tracks are missing.

**Each segment is a discrete WAV file.** The system stores and serves takes individually, such as `seg_1_try1.wav` or `seg_1_stretched.wav`.

It never bakes audio into a single stream until final export. This design enables scrubbing, line soloing, and non-destructive retries.

### Editor Controls: Manual Sliders and the AI Command Bar

1. **Manual boundary and speaker sliders:** Automated speaker identification makes mistakes across quick cuts. The timeline provides draggable boundary handles and speaker selectors for immediate manual correction.
2. **The Editor AI Command Bar:** Creators adjust the timeline using natural language commands. For example, they type `move wav 1 to 0:0005 to right` or `shift line 3 right by 200ms`. The model translates these commands into deterministic timeline mutations.

---

## The Provenance Ledger: Git for Video Production

Every edit, take, and timeline adjustment generates an immutable event in ClickHouse. The system never overwrites state. It appends to an audit DAG.

### What Lives in ClickHouse:

* **Edit lineage:** `commit_id`, `dub_id`, `parent_commit_id`, and `version_seq`.
* **Action provenance:** Action types (`detect_segments`, `nudge_boundary`, `reassign_speaker`, `render_take`, `stretch_audio`, `rewrite_line`, `user_command`).
* **Author and intent:** Author source (`agent`, `command_bar`, or `manual_ui`) and original command text.
* **Take metrics:** Slot duration, measured duration, signed fit delta, repair strategy, and cost in USD. Every Gemini call and synthesis call counts once.
* **Waveform peak vectors:** `Array(UInt8)` storing 64 to 128 normalized peak numbers per take. The frontend renders waveforms instantly without reading files from disk.
* **Asset reference:** Content-addressed file path to the discrete WAV take.

### What Lives on Disk (Git LFS Model):

The filesystem stores the raw discrete WAV takes and video files. ClickHouse stores the index, DAG, and analytical metadata.

### Advantages of This Design:

1. **Time-travel timeline:** Reconstruct project state at any previous version using standard SQL group queries. Creators revert changes without an ephemeral RAM undo stack.
2. **Branching and A/B takes:** Branch an entire language track to test colloquial phrasing against literal translation. Compare overrun metrics side by side.
3. **Audit and cost tracking:** Inspect who modified each line, why the system adjusted timing, and the exact cost of each take.
4. **Learned prior feedback loop:** The system queries historical takes in ClickHouse. It learns speaker speech rates across titles to compress future first attempts automatically.

---

## Technology Stack

| Component | Technology |
|---|---|
| Video and Audio Understanding | Gemini 3.8 Flash (Vertex AI global) |
| Translation | Gemini 3.8 Flash, duration-constrained |
| Speech Synthesis | Google Cloud Chirp 3 HD Voices |
| Agent Framework | google.golang.org/adk/v2 with tool/mcptoolset |
| Gemini SDK | google.golang.org/genai |
| Provenance Ledger | ClickHouse Cloud, read by self-hosted mcp-clickhouse |
| Audio Assembly | ffmpeg |
| Frontend | Svelte 5 with runes, Vite |
| Cloud Host | Google Compute Engine (e2-standard-2 in us-central1) |

All AI services use Google Cloud. Gemini calls go through `google.golang.org/genai` on Vertex AI.
The SDK authenticates with Application Default Credentials. Production passes no API key.

The editor agent lives in `internal/agent` on `google.golang.org/adk/v2`. Its
`tool/mcptoolset` package reads the ledger through a self-hosted mcp-clickhouse server
running beside the backend on the GCE host. The agent reads only. Ledger writes stay on
the durable client in `internal/ledger`, the single writer. The agent is not built yet.

---

## Project Name

*Ajilamu* originates from **Aji Lhamu**, the Monpa masked dance-drama.

The theatre tradition descends from Thangtong Gyalpo, the fifteenth-century builder who staged performances to fund iron suspension bridges across the Himalayas. The Mukto Monpa tradition credits this dance with financing 108 bridges.

A system that carries stories across language barriers shares this exact spirit.
