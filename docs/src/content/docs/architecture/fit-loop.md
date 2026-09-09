---
title: Two-Sided Fit Loop
description: Why fit is two-sided and how the signed delta repair algorithm corrects misfits.
template: doc
---

import Mermaid from '../../../components/Mermaid.astro'

Dubbing software frequently fails when translated lines finish too quickly. Most tools only detect lines that overrun their slot. Ajilamu enforces a two-sided fit evaluation.

---

## Why Fit Is Two-Sided

A line that ends prematurely breaks lip sync just as noticeably as speech that overruns. When audio stops while lips continue moving, the viewer immediately senses artificiality.

On 2026-09-07, an early validation run exposed this failure mode. The baseline script evaluated a take that ran 40.9 percent shorter than its assigned window. The script logged "perfect fit" and reported zero defects. In playback, 2.9 seconds of dead air played over moving lips.

Ajilamu calculates the signed duration delta. The sign determines whether the speech requires compression or expansion.

---

## Repair Strategy Matrix

The repair evaluator chooses one of four deterministic actions based on the signed delta:

| Signed Delta | Action Taken | API Cost |
| :--- | :--- | :--- |
| **Within ±8%** | ffmpeg `atempo` adjusts speed without altering pitch. | $0.00 |
| **Longer than +8%** | Gemini rewrites the translated phrase shorter. | Standard token rate |
| **Shorter than -8%** | Gemini rewrites the translated phrase fuller. | Standard token rate |
| **Fails 3 attempts** | System flags line for manual creator review in UI. | No additional retries |

---

## Decision Logic

The following diagram traces the evaluation flow for each take:

<Mermaid code={`
flowchart TD
    M[ffprobe Reads WAV Duration] --> C[Calculate Signed Delta\nDelta = t_measured - t_slot]
    C --> T{Within ±8%?}
    T -- Yes --> A[ffmpeg atempo\nSpeed Up / Slow Down]
    A --> S[Pass: Stage Take]
    T -- No --> AT{Attempt Count < 3?}
    AT -- Yes: Long (> +8%) --> R1[Gemini Rewrite Shorter]
    AT -- Yes: Short (< -8%) --> R2[Gemini Rewrite Fuller]
    R1 --> SYN[Chirp 3 HD Re-Synthesize]
    R2 --> SYN
    SYN --> M
    AT -- No --> FL[Flag in Workspace UI\nManual Correction Required]

    classDef dark fill:#090d15,stroke:#334155,color:#f1f5f9
    classDef highlight fill:#1e293b,stroke:#f59e0b,color:#f8fafc
    classDef cyan fill:#082f49,stroke:#06b6d4,color:#38bdf8
    class M,C,T,AT dark
    class R1,R2,SYN highlight
    class A,S,FL cyan
`} />

---

## Code Boundaries

The constants governing the fit loop live in dedicated packages:

- `types.FitThreshold`: Sets the acceptable margin before repair interventions activate.
- `fit.DefaultMaxStretchLong`: Defines the upper limit for acoustic time stretching without digital artifacts.
- `fit.DefaultMaxAttempts`: Restricts automated LLM rewrites to three attempts to prevent runaway billing.

When an attempt fails three times, the workspace warns the creator. The interface displays the duration gap so the user can adjust phrasing manually.
