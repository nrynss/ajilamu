# P5 close review round 2

Verdict: REMEDIATE

Findings: C 0, H 0, M 0, L 2.

## Scope and role

I reviewed Phase 5 at `c4f1f94` with the uncommitted round 1 remediation.
I own only this review file.
I did not implement or remediate production code.
I did not change phase status, exit boxes, or task records.

## Findings

| Where | What | Pin | Mutation |
|---|---|---|---|
| L1. `web/src/lib/Track.svelte:80-90` and `web/src/lib/Track.svelte:94-104` | Timeline tooltips use robotic fragments. Ghost takes also display the forbidden word `attempt` instead of `try`. | Chrome rendered `Line 3, attempt 1, 5720 ms` and `Line 4, attempt 1, 5920 ms`. Active takes rendered fragments such as `Line 8, 4200 ms`. This fails the creator vocabulary and natural sentence rules. | Reverting a future tooltip copy fix must restore the forbidden word or a fragment without a natural sentence. |
| L2. `web/src/lib/LengthBar.svelte:259-274` | Screen reader descriptions escape their length rows and create 688 pixels of phantom document scroll. The creator can scroll the entire workspace above a blank region. | Chrome at 1440 by 1000 measured `documentElement.scrollHeight` as 1688 pixels. The body and viewport measured 1000 pixels. Scrolling to the bottom moved the workspace top to -642 and its bottom to 312. The eight list descriptions occupied document positions through 1687.94. | Reverting a future containment fix must restore a document height above 1000 pixels at this viewport. |

## Exit criteria

| Criterion | Result | Independent measurement |
|---|---|---|
| Complete workspace renders from offline fixture data. | PASS | Chrome loaded eight segments, eight visible length rows, two tracks, and three rail tabs. The picture, rail, editor, and timeline all had positive height. |
| Timeline and length bar clearly identify segment 8's underrun. | PASS | The timeline measured a 170.625-pixel slot, a 100.797-pixel take, and a 69.828-pixel short tail. The length bar states 2.91 seconds short. |
| Light and dark themes function with complete token parity. | PASS | Chrome resolved all 17 colour tokens in both themes. Every value changed, and the line 8 canvas bitmap changed after the toggle. |
| All displayed metrics trace directly to underlying takes or charges. | PASS | `ffprobe` confirmed rendered take lengths. Independent charge addition matched the rendered project and line totals exactly. |
| All UI strings use natural sentence structure. | FAIL | Timeline hover titles expose fragments and the forbidden word `attempt`. Finding L1 records the rendered strings. |

`ffprobe` measured lines 1 and 8 at 1.680375 and 4.200375 seconds.
Rendered values round them to 1.68 and 4.20 seconds.
Independent charge addition produced 23,414,000 nanodollars.
The rendered total was `$0.023414`.
Line 1 independently summed to 842,100 nanodollars and rendered as `$0.0008421`.

The line 8 fixture contains a 7,110 millisecond slot and a 4,200 millisecond take.
Independent subtraction gives the displayed 2,910 millisecond underrun.
The timeline short tail completed the slot width exactly.

## T5.5a measurements

| Contract | Result | Independent measurement |
|---|---|---|
| An `agent` charge reads as `Editor agent turn`. | PASS | An isolated fixture mutation rendered `Editor agent turn` with its `$0.000321` charge. |
| Other charge kinds keep their labels. | PASS | The working fixture rendered `Finding lines`, `Translation`, and `Voice render`. |
| A kind with no label fails the type check. | PASS | Adding `review_unlabeled` in `/tmp/ajilamu-p5-r2b.hZxzdv` produced the expected `never` error at `DetailsTab.svelte:62`. |
| Removing the agent label fails the rendered-label pin. | PASS | Replacing the isolated label with `Voice render` made the browser pin report no `Editor agent turn` row. |

## C1 residue

Chrome 153 at 1440 by 1000 measured the picture at 1073 by 494 pixels.
It measured the rail at 352 by 494, the editor at 1425 by 460, and the timeline at 1425 by 156.
The timeline bottom sat at y 1000, inside the viewport.

An injected `grid-template-rows` revert reproduced the prior 34-pixel picture and zero-height rail.
The measurement therefore detects the original regression.
C1 has zero residue.

## Additional checks

`python3 tools/audit_docs.py` exited 0 and reported no drift.
Node 26.8.1 ran `npm --prefix web run check` with zero errors and zero warnings.
A Svelte source search found no `export let` declarations or legacy reactive declarations.
An HTTP range request for the fixture video returned 206 with the requested two bytes.

I used unused ports 57483, 57484, and 57485.
I stopped both Vite servers and headless Chrome.
All three ports were closed after measurement.
No review process remains.

## Residue against prior rounds

Round 1 finding C1 is closed with zero residue.
Findings L1 and L2 are new.
Phase 5 remains open.
