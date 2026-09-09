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

## Unverified Live Edge Cases

Two specific operational claims from the 2026-09-09 validation run require verification on a fresh live dataset:

1. **Live Ledger Charge Insertion**: An earlier live run encountered an error when inserting API billing records into a stale schema definition. The migration in `sql/schema.sql` resolved the column mismatch, but requires fresh live validation.
2. **Live Acoustic Time Stretching**: During that same initial run, every generated take exceeded the stretch budget by more than 8 percent. The automated rewrite path handled repairs, leaving live `atempo` processing untested in that specific session.

---

## Audio Ducking Strategy

Ducking dialogue over a separately provided music stem yields the cleanest acoustic results.

When users provide only a single composite video file, the system attempts dynamic sidechain compression against the mixed background bed. Separating overlapping dialogue from loud background music remains an imperfect acoustic filter.
