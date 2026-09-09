# P5 close review round 3

Verdict: APPROVE

Findings: C 0, H 0, M 0, L 0.

## Scope and role

I reviewed Phase 5 at `fd32354` against its five exit criteria, the T5.5a contract, and the three findings from rounds 1 and 2.
The remediations under review are committed at `a2d5ebc` in `web/src/lib/Track.svelte`, `web/src/lib/LengthBar.svelte`, and `web/src/routes/d/[id]/+page.svelte`.
I own only this review file.
I did not implement or remediate production code.
I did not change phase status, exit boxes, or task records.
I confirmed `git diff a2d5ebc HEAD` is empty for the three remediated files, so this review measures the landed remediation.
Every measurement ran inside an isolated copy at `/tmp/p5r3`.
I drove headless Chrome 152.0.7977.82 over the raw DevTools protocol against `vite dev` on port 58101 and the browser on port 58102.
Every number below comes from the browser DOM, `Runtime.exceptionThrown`, CDP network events, or `ffprobe`, never from application output.

## Verdict

APPROVE with zero findings at every severity.
C1, L1, and L2 each close with zero residue.
I claim zero residue against every prior round by direct re-measurement, not by reading the remediation records.

## Findings

| Where | What | Pin | Mutation |
|---|---|---|---|
| none | No finding at any severity | No failing check exists | No mutation is possible for a clean verdict |

## Exit criteria

| Criterion | Result | Independent measurement |
|---|---|---|
| Complete workspace renders from offline fixture data. | PASS | Chrome loaded `/d/fixture` at 1440 by 1000 and five other desktop viewports. Every viewport had a positive picture, rail, editor, and timeline. The route rendered 8 length rows, 8 canvases, 2 tracks, and 3 rail tabs. No exception and no console error occurred on load at any viewport. |
| Timeline and length bar clearly identify segment 8's underrun. | PASS | The timeline drew a 170.63 pixel slot, a 100.8 pixel take, and a 69.83 pixel short tail. The tail completes the slot width exactly. The length bar description reads `Line 8 length bar. This take is 2.91 seconds short of the slot.` The canvas held 7962 pixels of the `--short` colour. Independent subtraction gives 7110 minus 4200 equals 2910 milliseconds. |
| Light and dark themes function with complete token parity. | PASS | All 17 colour tokens changed value across the toggle. The line 8 canvas bitmap hash moved from 1952709001 to 939932820 and returned on the second toggle. The theme-color meta moved from `#F2F1EF` to `#12110F`. |
| All displayed metrics trace directly to underlying takes or charges. | PASS | `ffprobe` measured all 10 take files. Every rendered `data-duration-ms` matched the real file within one millisecond. Independent charge addition produced 23414000 nanodollars and the route rendered `$0.023414`. |
| All UI strings use natural sentence structure. | PASS | A scan of visible text and accessible attributes found no forbidden vocabulary. All 10 timeline tooltips are complete sentences. The flagged note and repair sentences match the specification examples. |

## T5.5a measurements

| Contract | Result | Independent measurement |
|---|---|---|
| An `agent` charge reads as `Editor agent turn`. | PASS | I added one agent charge to the fixture inside the isolated copy. The Details tab rendered `Editor agent turn` beside `$0.000321`. |
| Other charge kinds keep their labels. | PASS | The working fixture rendered `Finding lines`, `Translation`, and `Voice render`. |
| A kind with no label fails the type check. | PASS | Adding `review_unlabeled` to `ChargeKind` made `npm run check` exit 1 with exactly one error, `Type '"review_unlabeled"' is not assignable to type 'never'` at `DetailsTab.svelte:62`. The clean tree then reported 0 errors and 0 warnings. |
| Removing the label fails the rendered-label pin. | PASS | Replacing the agent case with `Voice render` made the agent row render `Voice render` and removed every `Editor agent turn` row. I restored the file and the baseline rows returned. |

## Residue against prior rounds

| Finding | Zero residue evidence | Mutation pin |
|---|---|---|
| C1, the collapsed editor row at 1440 by 1000. | The picture measured 1088 by 494, the rail 352 by 494, the editor 1440 by 460, and the timeline 1440 by 156 with its bottom at y 1000. Five other viewports held: 1920 by 1200 gave 1568 by 602, 1280 by 800 gave 928 by 386, 1512 by 982 gave 1160 by 484.28, 2560 by 1440 gave 2208 by 731.61, and 1366 by 768 gave 1014 by 368.73. No row left its viewport. | An injected stylesheet that reverted `grid-template-rows` to `minmax(0, 1fr) auto` and `.editor` to `display: block` restored the original failure. The rail measured 352 by 0 and the picture 1073 by 34, and the timeline bottom fell to y 1150.39. |
| L1, tooltip fragments and the forbidden word `attempt`. | All 10 take tooltips are complete sentences ending in a period. The two ghost tooltips read `Try 1 for line 3 lasts 5720 milliseconds.` and `Try 1 for line 4 lasts 5920 milliseconds.` No title contains `attempt`. | Reverting the two title strings in an isolated copy restored `Line 3, attempt 1, 5720 ms`, `Line 4, attempt 1, 5920 ms`, and fragments such as `Line 1, 1680 ms`. The pin flags both the forbidden word and the missing period. |
| L2, phantom document scroll from screen reader spans. | Document scroll height equalled client height at all six viewports, 1000 at 1440 by 1000 and 1440 at 2560 by 1440. All nine spans reported `position: fixed` with a maximum bottom edge of 1 pixel. | An injected stylesheet that restored `position: absolute` with automatic edges raised the document height to 1687 pixels and the span bottom to 1686.94 pixels, which reproduces the round 2 defect. |

## Additional checks

`python3 tools/audit_docs.py` exited 0 and reported no drift.
A search over `web/src` found no `export let`, no `$:`, no `svelte/store` import, and no `writable`, `readable`, or store `get`.
The clean tree reported 0 errors and 0 warnings under `npm run check` with Node 26.8.1.
An HTTP range request for `/clip.mp4` returned 206 with `Content-Range: bytes 0-1/18385109`.
Independent charge sums were 23414000 nanodollars for the project, 842100 for line 1, 2948600 for line 8, and 67000 for the segmentation charge.
The History tab's learned rate computed to 38160 measured milliseconds over 41900 slot milliseconds across 8 lines, which is 8.9 percent shorter and matches the rendered sentence.
The picture duration of 75008 milliseconds matches the `ffprobe` clip duration of 75.008267 seconds.
The stacked layout at 760 by 900 held a 745 by 1167 picture, a 745 by 370 rail, a 745 by 378 editor, and a 745 by 156 timeline. No phantom scroll appeared.

## Observations, not findings

The History tab issues `GET /api/dubs/{id}/history` and the offline fixture answers 404, so the console records one error.
The component documents the ledger as the source of truth and the fixture commits as the fallback.
The fallback commits rendered and no page exception occurred, so this is the designed fallback path rather than a defect.
The picture area scrolls internally.
At 1440 by 1000 the 16 to 9 preview is 1048 by 581 inside a 494 pixel region. The player controls, language strip, flagged note, and length checks sit below the fold.
The region has used `overflow: auto` since T5.1 and prior rounds accepted it, so I do not count it against criterion two.

## Evidence paths

- `/tmp/p5-close-r3-evidence/viewports.json`, six viewports, layout boxes, tooltips, and per viewport diagnostics
- `/tmp/p5-close-r3-evidence/interact.json`, rail tabs, charge rows, theme parity, and the C1 and L2 style mutations
- `/tmp/p5-close-r3-evidence/interact2.json`, canvas repaint, vocabulary scan, selection scroll, keyboard, and the range request
- `/tmp/p5-close-r3-evidence/probe.json`, canvas pixel counts, tooltip sentences, and the stacked layout
- `/tmp/p5-close-r3-evidence/fold.json`, picture area fold positions and the History request
- `/tmp/p5-close-r3-evidence/details-*.png`, the agent label pin before, during, and after mutation
- `/tmp/p5-close-r3-evidence/shot-*.png`, one screenshot per measured viewport
- `/tmp/p5-close-r3-evidence/theme-light-1440x1000.png` and `theme-dark-1440x1000.png`
- `/tmp/p5-close-r3-evidence/timeline-scrolled.png`, the dubbed track and underrun tail after internal scroll
- `/tmp/p5-close-r3-evidence/harness/`, the DevTools protocol scripts that produced every number

## Cleanup statement

I stopped the Vite server on port 58101 and headless Chrome on port 58102.
Both ports closed and no server, browser, container, or stray process survived.
I deleted the isolated copy at `/tmp/p5r3`.
The main checkout carries only this new untracked review file.
