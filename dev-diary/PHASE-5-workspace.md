# P5: Workspace (Track D)

```yaml
id:       P5
size:     XL
branch:   phase/p5-workspace
requires: [T1.1, T1.4, T1.5]
blocks:   P6, P8
parallel: high
runs-parallel-with: P2, P3, P4, P6, P7
```

**Goal:** Build the complete user interface using Svelte 5 and runes. Render the workspace directly from mock JSON fixtures without requiring a running backend.

**Design compliance:** Follow interface specifications in `ui-ux.md` strictly. Where code and specification conflict, the specification wins.

**Offline development:** Task `T1.5` serves full project fixtures including segments, takes, gaps, and underruns.

---

### T5.1: Shell, routes, tokens ★
```yaml
requires:   T1.4, T1.5
fixture-ok: yes
size:       M · mid
owns:       web/package.json, web/vite.config.ts, web/svelte.config.js, web/tsconfig.json, web/src/app.d.ts, web/src/app.html, web/src/routes/+layout.svelte, web/src/routes/+page.svelte, web/src/routes/new/+page.svelte, web/src/routes/d/[id]/+page.svelte, web/src/routes/config/+page.svelte, web/src/lib/tokens.css
status:     done
```
Initialize Svelte 5 with runes and Vite. Implement routes: `/`, `/new`, `/d/{id}`, and `/config`. Build the 46px header chrome.

Define all color tokens in CSS variables. Implement light and dark themes with explicit `data-theme` switching. Both themes must receive equal polish.

Apply system typography for UI elements and monospace tabular numbers for timecodes, durations, attempt counts, and pricing. Apply specified corner radii and border styles.

**Done when:** All four routes render, theme toggling functions smoothly, and semantic colors convey status without decorative overuse.

---

### T5.2: Preview and language strip
```yaml
requires:   T5.1
fixture-ok: yes
size:       M · mid
owns:       web/src/lib/Preview.svelte, web/src/lib/LanguageStrip.svelte, web/static/clip.mp4
status:     done
```
Render the 16:9 video preview top-left with playback controls. Place the language strip directly beneath to switch active playback tracks. Pressing Space toggles play and pause.

Serve video content via HTTP 206 range requests to ensure instant scrubbing.

**Done when:** Test video plays, scrubs smoothly, and switches language audio streams without reloading the page.

---

### T5.3: Timeline ★
```yaml
requires:   T5.1
fixture-ok: yes
size:       L · frontier
owns:       web/src/lib/Timeline.svelte, web/src/lib/Ruler.svelte, web/src/lib/Track.svelte
status:     done
```
Build a resizable timeline with a 156px minimum height. Keep the header and time ruler pinned while audio tracks scroll.

Stack tracks vertically: original audio on top, followed by one track per dubbed language. Align corresponding lines to identical horizontal column positions.

Scale block widths to true time durations. A 1.8-second clip must measure one-third the width of a 5.4-second clip.

**Done when:** The 8 test segments render at true offsets, audio gaps display accurately, and track columns align vertically.

---

### T5.4: Length bar instrument ★
```yaml
requires:   T5.1, T1.1
fixture-ok: yes
size:       L · frontier
owns:       web/src/lib/LengthBar.svelte
status:     claimed:gpt-5-codex-t5.4
```
Render the length bar instrument on HTML canvas. Repaint automatically upon window resize or theme toggle.

Draw sunken troughs scaled to slot lengths. Render takes inside using `--fit` for matching lengths, `--over` for overruns, and `--short` for unfilled time.

Render ghost shapes behind active takes to represent earlier attempts.

Display repair styles visually: hatched patterns indicate time-stretches with speed percentages, while solid colors indicate rewrites.

**Done when:** Canvas renders all three states accurately, highlights segment 8's underrun clearly, and repaints cleanly on theme changes.

---

### T5.5: Rail ★
```yaml
requires:   T5.1
fixture-ok: yes
size:       M · mid
owns:       web/src/lib/Rail.svelte, web/src/lib/tabs/
status:     not-started
```
Implement the 352px right rail with three tabs: **Lines**, **Details**, and **History**.

The Lines tab displays line numbers, slot durations, actual lengths, and try counts. The Details tab shows voice configurations, repairs, and itemized costs. The History tab displays the commit DAG.

Clicking duration labels triggers audio playback for that take.

**Done when:** Selecting a line updates all three tabs instantly, and every displayed cost matches real ledger data.

---

### T5.6: States and keyboard ★
```yaml
requires:   T5.1
fixture-ok: yes
size:       M · mid
owns:       web/src/lib/states/, web/src/lib/shortcuts.ts
status:     not-started
```
Build four UI states for each view: loading, empty, error, and populated. Show an active step banner during processing instead of an indefinite spinner.

Implement keyboard shortcuts: `?` for help, `/` for command bar, Space for playback, `[` and `]` for line stepping, and Escape to close modals.

Use natural sentences for all messaging. For example, write "We could not reach the voice service, so nothing was charged."

**Done when:** Panels display all four states from fixture data, and keyboard navigation operates without pointer input.

---

### T5.7: Workspace composition
```yaml
requires:   T5.2, T5.3, T5.4, T5.5, T5.6
fixture-ok: yes
size:       M · frontier
owns:       web/src/routes/d/[id]/+page.svelte, web/src/lib/fixture.ts
status:     not-started
```
Compose the reviewed workspace components into the `/d/{id}` route. Load the T1.5 offline
fixture through one typed client helper. Connect shared selection, playback, theme repaint, and
keyboard state without changing component-owned files.

**Done when:** The full workspace renders from the fixture. Preview, timeline, length bars,
rail, states, and shortcuts interact as one route.

---

## Exit Criteria

- [ ] Complete workspace renders from offline fixture data.
- [ ] Timeline and length bar clearly identify segment 8's underrun.
- [ ] Light and dark themes function with complete token parity.
- [ ] All displayed metrics trace directly to underlying takes or charges.
- [ ] All UI strings use natural sentence structure.

---

## Handoff Log

_(Fill on completion: record component layout decisions and canvas rendering performance.)_
