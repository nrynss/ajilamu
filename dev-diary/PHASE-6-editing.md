# P6: Editing (Track F)

```yaml
id:       P6
size:     M
branch:   phase/p6-editing
requires: [T5.3, T5.5]
blocks:   P8
parallel: medium
runs-parallel-with: P2, P3, P7
```

**Goal:** Provide manual and natural language editing controls so creators can correct AI transcription, timing, and attribution errors.

**Field evidence:** Initial tests transcribed "Mark Van der High" instead of "Mark Vande Hei". Gemini spelled the name wrong inside the sentence, producing misspelled Malayalam audio.

Manual correction of boundaries, speakers, and transcript text is essential.

**Start T6.4 first:** The command parser is a pure function from text to mutation. It needs no visual timeline, so an agent can start it immediately.

---

### T6.1: Boundary handles ★
```yaml
requires:   T5.3, T1.1
fixture-ok: yes
size:       L · frontier
owns:       web/src/lib/edit/Boundary.svelte
status:     not-started
```
Implement draggable start and end handles on timeline segments. Adjusting handles alters slot duration and updates the length bar dynamically.

Snap dragging to adjacent segment boundaries and silence gaps. Disallow silent overlapping without explicit user confirmation.

**Done when:** Dragging boundary handles recalculates fit metrics in real time and enforces clear collision rules.

---

### T6.2: Speaker reassignment ★
```yaml
requires:   T5.3, T5.5
fixture-ok: yes
size:       S · mid
owns:       web/src/lib/edit/Speaker.svelte
status:     not-started
```
Provide quick speaker toggles per dialogue line. Changing speaker attribution alters voice assignment and requires re-rendering.

Always display estimated re-rendering costs before triggering synthesis calls. Never run billable operations in the background silently.

**Done when:** Changing a speaker displays the associated re-render fee and leaves original takes untouched if canceled.

---

### T6.3: Text correction ★
```yaml
requires:   T5.5
fixture-ok: yes
size:       M · mid
owns:       web/src/lib/edit/Text.svelte
status:     not-started
```
Support in-place text editing for transcribed source sentences and translated target lines.

Correcting source text prompts for re-translation. Correcting target text directly is authoritative and proceeds straight to speech synthesis without second-guessing the creator.

Tag translated elements with proper HTML `lang` attributes to ensure correct Malayalam font rendering.

**Done when:** Both text fields edit cleanly, and editing target text triggers synthesis without calling translation models.

---

### T6.4: Command bar ★
```yaml
requires:   T1.1
fixture-ok: yes
size:       M · mid
owns:       internal/command/parse.go, web/src/lib/edit/CommandBar.svelte
status:     not-started
```
Build a command input above the timeline. Press `/` to focus. Parse natural language instructions into structured mutations:

* "move wav 1 to 0:0005 to right."
* "shift line 3 right by 200ms."
* "change speaker for line 7 to Mark."
* "shorten line 4 by 0.5s."

**The model parses, but never computes:** The model outputs structured actions with targets and values. Deterministic Go code validates values and performs timeline arithmetic.

Display parsed intent before applying changes so creators can confirm modifications.

**Done when:** Sample commands parse into valid mutations, invalid references fail with clear explanations, and values validate before execution.

---

### T6.5: Re-render a line ★
```yaml
requires:   T6.2, T6.3, T2.7
fixture-ok: no
size:       M · mid
owns:       internal/api/rerender.go
status:     not-started
```
Re-run an individual dialogue line through the fit loop and save the output as a new take. Never overwrite previous takes.

Preserve `seg_3_try1.wav` when creating `seg_3_try2.wav`. Render older takes as ghost outlines on the timeline.

Display cost estimates before running and record new commits in ClickHouse.

**Done when:** Re-rendering creates a secondary take file, both takes play independently, and the timeline shows ghost take history.

---

## Exit Criteria

- [ ] Boundaries, speakers, and text support manual user editing.
- [ ] Billable operations display itemized prices before execution.
- [ ] Command bar parses instructions into validated deterministic mutations.
- [ ] Timeline edits preserve prior takes without destructive overwrites.
- [ ] All mutations write author-attributed commits to ClickHouse.

---

## Handoff Log

_(Fill on completion: document command parser grammar and boundary dragging sensitivity.)_
