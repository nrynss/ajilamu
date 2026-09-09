# P5 close review round 1

Verdict: REMEDIATE

Findings: C 1, H 0, M 0, L 0.

## Scope and role

I reviewed Phase 5 at `c4f1f94` against its exit criteria and the T5.5a contract.
I own only this review file.
I did not implement or remediate production code.
I did not change phase status, exit boxes, or task records.

## Findings

| Where | What | Pin | Mutation |
|---|---|---|---|
| C1. `web/src/routes/d/[id]/+page.svelte:1117-1122` and `web/src/routes/d/[id]/+page.svelte:1308-1317` | The editor collapses the picture and rail grid row at a standard desktop viewport. The complete workspace does not render visibly. | Chrome at 1440 by 1000 measured the rail at 352 by 0 pixels. The picture area measured 1073 by 34 pixels. `/tmp/ajilamu-p5-close-r1-light.png` and `/tmp/ajilamu-p5-close-r1-dark.png` show neither panel. | Reverting a future layout fix must restore the zero-height rail and clipped picture at this viewport. |

## Exit criteria

| Criterion | Result | Independent measurement |
|---|---|---|
| Complete workspace renders from offline fixture data. | FAIL | `/d/fixture` loaded the fixture and created all workspace nodes. Chrome measured a zero-height rail and a 34-pixel picture area. |
| Timeline and length bar clearly identify segment 8's underrun. | PASS | The timeline measured a 170.625-pixel slot, a 100.797-pixel take, and a 69.8281-pixel short tail. The length bar states 2.91 seconds short. |
| Light and dark themes function with complete token parity. | PASS | Chrome resolved all 17 colour tokens in both themes. The toggle changed every value and repainted the line 8 canvas bitmap. |
| All displayed metrics trace directly to underlying takes or charges. | PASS | `ffprobe` confirmed rendered take lengths. Independent charge addition matched the rendered project and line totals exactly. |
| All UI strings use natural sentence structure. | PASS | Rendered status, fit, cost, playback, edit, and help messages use complete creator-facing sentences. Headings and controls use the specified plain vocabulary. |

`ffprobe` measured lines 1 and 8 at 1.680375 and 4.200375 seconds.
Rendered values round them to 1.68 and 4.20 seconds.
Independent charge addition produced 23,414,000 nanodollars.
The rendered total was `$0.023414`.
Line 1 independently summed to 842,100 nanodollars and rendered as `$0.0008421`.

## T5.5a measurements

| Contract | Result | Independent measurement |
|---|---|---|
| An `agent` charge reads as `Editor agent turn`. | PASS | An isolated fixture charge rendered `Editor agent turn` in the project charge list. See `/tmp/ajilamu-p5-close-r1-agent-label.png`. |
| Other charge kinds keep their labels. | PASS | The working fixture rendered `Finding lines`, `Translation`, and `Voice render`. |
| A kind with no label fails the type check. | PASS | Adding `review_unlabeled` in an isolated worktree produced the expected `never` assignment error at `DetailsTab.svelte:62`. |
| Removing the agent label fails the rendered-label pin. | PASS | Replacing the label with `Voice render` made the isolated pin fail with the rendered agent row. |

The isolated type mutation also exposed a temporary-directory timeout type diagnostic.
The clean working tree check did not contain that diagnostic.
The added `DetailsTab.svelte` diagnostic therefore remains attributable to the mutation.

## Additional checks

`python3 tools/audit_docs.py` reported no drift before review measurements.
Node 26 `npm --prefix web run check` completed with zero errors and zero warnings.
A Svelte source search found no `export let` declarations or legacy reactive declarations.

The working fixture rendered eight length bars, an Original track, and a Dub ML track.
The timeline duration was 75,008 milliseconds.
The line 8 fixture values were a 7,110 millisecond slot and a 4,200 millisecond take.
Independent subtraction gives the displayed 2,910 millisecond underrun.

## Evidence paths

- `/tmp/ajilamu-p5-close-r1-light.png`
- `/tmp/ajilamu-p5-close-r1-dark.png`
- `/tmp/ajilamu-p5-close-r1-agent-label.png`
- `/tmp/ajilamu-p5-close-r1-mutations.patch`

## Residue

No prior P5 close review or remediation round exists.
There is no prior close residue to assess.
Finding C1 remains open.
