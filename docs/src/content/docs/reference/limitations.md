---
title: System Boundaries & Unbuilt Scope
description: Transparent documentation of unbuilt features, current limitations, and engineering trade-offs.
template: doc
---

We prefer that you read current system boundaries here rather than discover them unexpectedly in production.

---

## Unextracted On-Screen Text

Ajilamu focuses exclusively on spoken dialogue.

The Gemini segmentation prompt instructs the model to transcribe speech phonetics. It ignores visual signage, burned-in captions, credits, and lower-third graphics.

Although Gemini accepts full video frames, the ClickHouse schema contains no fields for optical bounding boxes or graphic overlays. Burned-in video text remains untranslated in the exported video.

---

## Audio-Only Speaker Identification

The segmentation stage infers speaker identities solely from audio characteristics. The model analyzes vocal timbre rather than facial movements in the visual frame.

This acoustic heuristic occasionally introduces errors. During early testing, the model produced slightly different name spellings for the same speaker across distinct segments. The text-to-speech engine synthesized the spelling discrepancy literally.

The manual speaker reassignment selector on the workspace timeline exists specifically to correct these identification slips.

---

## Live Edge Cases Now Verified

Two operational claims from the 2026-09-09 validation run were verified on the deployed host:

1. **Live ledger charge insertion.** A live run wrote 56 charge rows with 56 distinct event keys, summing to the run-reported 65,330,550 nanodollars.
2. **Live acoustic time stretching.** A live run repaired two overruns with `atempo` at 0.9813 and 0.9844, and both takes landed inside their slots.

---

## One Target Language Per Project

A project dubs into the single target language chosen at creation. Dubbing the same film into
a second language needs a second project, uploaded from the same video.

---

## Audio Ducking Strategy

Ducking dialogue over a separately provided music stem yields the cleanest acoustic results.

When users provide only a single composite video file, the system attempts dynamic sidechain compression against the mixed background bed. Separating overlapping dialogue from loud background music remains an imperfect acoustic filter.
