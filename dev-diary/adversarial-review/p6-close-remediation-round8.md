# P6 close remediation round 8

Round 8 review: `dev-diary/adversarial-review/p6-close-round8.md`. Verdict REMEDIATE,
C 0, H 0, M 1, L 0.

The user directed the orchestrator to fix the one finding directly and close the cycle, so no
separate remediator ran. The orchestrator changed only the two files the finding names.

## Remediation

| Finding | Fix | Pin | Mutation |
|---|---|---|---|
| M1. A failed speaker re-render stayed applied on screen. `web/src/routes/d/[id]/+page.svelte` `handleSpeakerChange` patched the speaker before the request and never reverted, and `web/src/lib/edit/Speaker.svelte` `confirm` set `appliedSpeaker` and `sourceSignature` before the call and returned on error. | The page now captures the stored segment from the active track before it patches, and patches it back when the reply carries an error. The panel captures `previousSpeaker` and `previousSignature` before it shows the choice, and restores both in the error branch and the catch branch. The Boundary control already reverted on the same failure. | With the ledger stopped, the panel read `data-speaker="Suni Williams"` plus `Could not read the project ledger.` With the ledger up, the same control re-rendered the line as `seg_1_try3.wav` and the panel read `data-speaker="Mark Vande Hei"`. | Reverting either restore leaves the new speaker beside the error while the ledger keeps the old one, which is the round 8 defect. |

## Measurement

The environment was a throwaway ClickHouse 26.8.2.7 on port 18999, loaded from `sql/schema.sql`.
The dub `d-verify` held one take and two timeline segments with two speakers. The real binary
served the production frontend build on port 18299. Headless Chrome drove the Speaker panel.

Pre-fix the round 8 review measured `data-speaker=Mark Vande Hei` beside
`Could not read the project ledger.` on the same outage.

Post-fix, with the container stopped:

```
applied: Suni Williams
errors:  ["Could not read the project ledger."]
```

Post-fix, with the container restarted:

```
applied: Mark Vande Hei
sentence: Line 1 re-rendered as take seg_1_try3.wav and needs review.
```

## Checks

`npm --prefix web run check` read 183 files with 0 errors and 0 warnings.
`npm --prefix web run build` wrote the site.
The container, the server and the browser tab were removed. No process survives.

## Scope

Changed `web/src/routes/d/[id]/+page.svelte` and `web/src/lib/edit/Speaker.svelte`.
No other file changed. No commit was made by this remediation.
