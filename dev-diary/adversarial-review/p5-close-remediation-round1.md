# P5 close remediation round 1

Round 1 review: `dev-diary/adversarial-review/p5-close-round1.md`. Verdict REMEDIATE, C 1, H 0, M 0, L 0.

I fixed the one finding. I did not change the verdict and I did not review my own work.

## Scope

I edited `web/src/routes/d/[id]/+page.svelte` only, and only its layout CSS.
No markup, no script, and no other file changed.
    10|The four T6.7a read-only fieldsets and their four sentences render unchanged at every viewport measured.

## Remediation

| Finding | Fix | Pin | Residue |
|---|---|---|---|
| C1. The editor row collapses the picture and rail row at a standard desktop viewport. | `.workspace` now sizes its rows `minmax(220px, 1fr) minmax(156px, 46vh)` instead of `minmax(0, 1fr) auto`, so the editor band can no longer grow past its cap. `.editor` becomes a column flex box with `overflow: auto`, and `.editor-panels` takes `min-height: 0` with `overflow: auto`. The line panels absorb the shortfall, so the command bar, take history, and timeline all keep their own minimums. | Chrome 151 at 1440 by 1000 measured the picture area at 1073 by 494, the rail at 352 by 494, the editor at 1425 by 460, and the timeline at 1425 by 156. The timeline bottom sits at y 1000, inside the viewport. 1512 by 982 measured 484, 484, 452, and 156. 1280 by 800 measured 386, 386, 368, and 156. The same page before the fix measured the rail at 352 by 0 and the picture area at 1073 by 34. | None. Both halves of the fix carry load, and a runtime revert of either one fails a measurement. |

## Measurement method

    20|I drove headless Chrome 151.0.7922.71 over raw CDP against `vite dev` on port 5591.
Every number below comes from `getBoundingClientRect` in the browser, not from application output.
I started the dev server and the browser, and I stopped both when the measurements finished.

### Heights at three desktop viewports, after the fix

| Viewport | Picture area | Rail | Editor | Timeline | Read-only fieldsets |
|---|---|---|---|---|---|
| 1440 by 1000 | 1073 by 494 | 352 by 494 | 1425 by 460 | 1425 by 156 | 4 |
| 1512 by 982 | 1145 by 484.28 | 352 by 484.28 | 1497 by 451.72 | 1497 by 156 | 4 |
    30|| 1280 by 800 | 913 by 386 | 352 by 386 | 1265 by 368 | 1265 by 156 | 4 |
| 1920 by 1200 | 1553 by 602 | 352 by 602 | 1905 by 552 | 1905 by 156 | 4 |

The workspace `scrollHeight` now equals its `clientHeight` at 1440 by 1000, at 954 pixels each.
Before the fix it read 1104 against 954, which is the overflow that drove the collapse.

The 780 pixel breakpoint still stacks. At 760 by 900 the picture area measured 745 by 1167.63,
the rail measured 745 by 369.5, the editor measured 745 by 378, and the timeline measured 745 by 156.

### Mutation, run in the browser rather than in the repository

    40|I injected a stylesheet that reverts each rule at 1440 by 1000 and re-measured. No file changed.

| Reverted rule | Picture | Rail | Timeline | Timeline bottom | Pin result |
|---|---|---|---|---|---|
| Nothing, the fixed page | 494 | 494 | 156 | 1000 | pass |
| `grid-template-rows` back to `minmax(0, 1fr) auto` | 34 | 0 | 214.53 | 1000 | fail, the rail has no height |
| `.editor` back to `display: block` with visible overflow | 494 | 494 | 256 | 1644.39 | fail, the timeline falls below the fold |
| Both rules | 34 | 0 | 256 | 1150.39 | fail on both counts |

The second row reproduces the reviewer's exact numbers, a 352 by 0 rail and a 34 pixel picture area.

    50|### Other checks

Node 26.8.1 ran `npm --prefix web run check` with zero errors and zero warnings.
The diff touches CSS declarations only, so no Svelte 4 idiom can enter through it.

## Out of scope, recorded rather than fixed

The document still scrolls 688 pixels past the viewport at 1440 by 1000, and it did so before this fix too.
The `.workspace` element measures 1425 wide against a 1440 viewport, which is the resulting scrollbar.
`document.body.scrollHeight` reads 1000 while `document.documentElement.scrollHeight` reads 1688.
    60|The overflow traces to absolutely positioned screen reader spans inside the length bars, which escape
the `overflow: auto` clip on `.picture-area` because no ancestor between them is positioned.
That lives in `web/src/lib/LengthBar.svelte`, which this remediation does not own.
No finding names it, so I left it alone.

## Evidence paths

- `/tmp/ajilamu-p5-rem-r1/before-1440x1000.png`
- `/tmp/ajilamu-p5-rem-r1/after-1440x1000.png`
- `/tmp/ajilamu-p5-rem-r1/after-1512x982.png`
- `/tmp/ajilamu-p5-rem-r1/after-1280x800.png`
    70|- `/tmp/ajilamu-p5-rem-r1/after-1920x1200.png`
- `/tmp/ajilamu-p5-rem-r1/after-760x900.png`
- `/tmp/ajilamu-p5-rem-r1/measure.mjs`
- `/tmp/ajilamu-p5-rem-r1/mutate.mjs`

## Residue against prior rounds

No prior P5 close remediation round exists. C1 is the only open finding, and it is now closed.
The phase stays open for re-review. I did not commit and I did not mark the phase complete.
