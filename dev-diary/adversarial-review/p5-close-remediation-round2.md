# P5 close remediation round 2

Round 2 review: `dev-diary/adversarial-review/p5-close-round2.md`.
Verdict REMEDIATE, C 0, H 0, M 0, L 2.

I fixed both findings. I did not change the verdict and I did not review my own work.

## Remediation

| Finding | Fix | Pin | Mutation |
|---|---|---|---|
| L1. Timeline tooltips use fragments and ghost tooltips say `attempt`. | Ghost tooltips now say `Try 1 for line 3 lasts 5720 milliseconds.` Active tooltips use the same natural sentence structure. The `data-attempt` attributes remain unchanged. | Headless Chrome rendered both ghost sentences and eight active sentences. Line 8 rendered `The active take for line 8 lasts 4200 milliseconds.` | Reverting the copy restores `attempt` in ghost tooltips and restores sentence fragments in active tooltips. |
| L2. Screen reader descriptions create phantom document scroll. | The hidden descriptions now use fixed positioning with explicit top and left edges. They remain available to assistive technology without extending the document. | Headless Chrome at 1440 by 1000 measured document, body, and viewport heights at 1000 pixels. It found all nine descriptions. | A browser-injected revert to absolute positioning and automatic edges restored the 1688-pixel document height. Body and viewport heights remained 1000 pixels. |

## Checks

Node 26.8.1 ran `npm --prefix web run check` with zero errors and zero warnings.
Chrome measurement used Vite on port 57555 and raw browser protocol on port 57556.
Both processes stopped, and both ports closed after measurement.

## Scope

I edited `web/src/lib/Track.svelte`, `web/src/lib/LengthBar.svelte`, and this record.
I did not touch the workspace page, T6.6c files, `internal/command`, or `web/src/lib/progress.ts`.
I did not commit, mark Phase 5 complete, or mark any task done.
