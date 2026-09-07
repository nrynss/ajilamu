# Ajilamu: Interface Direction

**Read this before writing a component.** Where a screen and this document disagree, this document wins.

---

## 1. The Brief in One Line

**A dubbing editor tool, not a progress page.** We build for one person who publishes their own work, not for a studio floor.

The interface must not look generated. It must not lie about precision.

**Use words people already use.** Do not borrow trade jargon. If a word needs a glossary, replace it with a plain word.

Keep numbers exact and density high. Solo creators cut their own video and read timelines easily.

**Plain words, real information.** Say "1.2 seconds too long", not "overrun +18%".

**Not generated** means no color-coded cards in a grid and no emojis as icons. Avoid flat rectangles standing in for time. Use real typography, monospace numbers, and true duration geometry.

**Not lying** means every displayed number connects to real audio, real duration, or real money.

---

## 2. Colour

The video frame dictates the color palette. Surrounding colors with high chroma distort how creators perceive picture colors.

We use near-neutral, warm grounds and a single low-saturation accent.

| Token | Light | Dark | Use |
|---|---|---|---|
| `--ground` | `#F2F1EF` | `#12110F` | App background |
| `--surface` | `#FFFFFF` | `#1B1A18` | Panels, chrome, rail |
| `--raised` | `#FAF9F7` | `#242220` | Controls, inset cards |
| `--sunken` | `#E7E5E1` | `#0C0B0A` | Wells, video letterbox, length troughs |
| `--line` | `#DBD8D2` | `#302D2A` | Borders |
| `--line-soft` | `#E8E5E0` | `#26241F` | Internal dividers |
| `--text` | `#171614` | `#EDEAE5` | Primary text |
| `--dim` | `#6B6862` | `#9A958D` | Secondary text |
| `--faint` | `#96918A` | `#726D66` | Metadata, ruler ticks |
| `--accent` | `#8A5A2B` | `#C79355` | Selection, active item, primary action |
| `--accent-q` | `#F0E4D6` | `#33261A` | Accent fills |
| `--ok` | `#4A7A4E` | `#71AE76` | Semantic success only |
| `--warn` | `#946A17` | `#D7A445` | Semantic warning only |
| `--stop` | `#A94A3F` | `#DC6A5E` | Semantic stop only |
| `--fit` | `#8FA58C` | `#5C7259` | Take audio inside the slot |
| `--over` | `#B4736A` | `#8C4F47` | Take audio past the slot |
| `--short` | `#C2A878` | `#7A6742` | Slot left unfilled by a short take |

### Rules

* Never use semantic colors as decorative accents. Accent indicates selection or active state, never positive status.
* Pair color with a glyph and a word. Color never carries meaning alone.
* Both light and dark themes receive equal care. Define tokens on `:root` and manage themes with explicit `data-theme` attributes.

---

## 3. Typography

* **UI:** Use `-apple-system, BlinkMacSystemFont, "SF Pro Text", "Segoe UI", system-ui, sans-serif`. It renders as the native system face.
* **Numerics:** Use `ui-monospace, "SF Mono", Menlo, Consolas, monospace` with `font-variant-numeric: tabular-nums`. Apply this to all timecodes, lengths, attempts, and costs.
* **Scale:** Base 13px. Uppercase labels at 10px, metadata at 11.5px, UI text at 12.5px to 13px, titles at 14px.
* **Dialogue text:** Content lines use 14px, normal weight, generous line height, and no truncation. Tag target lines with `lang` attributes.

---

## 4. Form

Apply radii consistently: 5px for controls, 7px for panels, 10px for containers, and 999px for pills.

Use one subtle shadow, reserved exclusively for the video frame. Separate elements with hairlines, never nested boxes. Avoid gradients.

---

## 5. Layout

Use professional video editing composition: picture top-left, details panel right, and timeline along the bottom.

```
┌────────────────────────────────────────────────────────────┐
│ Chrome 46px: title, readiness, budget, theme, actions      │
├────────────────────────────────┬───────────────────────────┤
│ 16:9 preview (flex)            │ Rail 352px                │
│ Player controls                │  Lines, Details, History  │
│ Language strip                 │  Line details             │
├────────────────────────────────┴───────────────────────────┤
│ Timeline: resizable, min 156px                             │
│ Header, ruler, original track, one track per language      │
└────────────────────────────────────────────────────────────┘
```

* **Language strip:** Sits directly under the picture beside player controls. It displays target languages and active playback.
* **Resizable timeline:** Drag the top edge to resize. Tracks scroll internally while the header and ruler remain pinned.
* **Stacked tracks:** Original track sits on top, followed by one track per language. Columns align identically across tracks.
* **The Editor AI Command Bar:** A single-line input bar sits above the timeline. Press `/` to focus. Type commands like `move wav 1 to 0:0005 to right`.
* **Manual sliders:** Automated speaker detection makes mistakes. Drag boundary handles on any segment to adjust timing, or click to reassign speakers.
* **Discrete WAV files:** Every take exists as an individual file. The timeline edits discrete files and renders the mix on demand.

---

## 6. The Length Bar Instrument

The length bar visually reports whether audio fits its allocated video slot.

```
   Original line duration
   ├──────────────────────────────────┤
   ▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓░░░░░░       fits        0.4s to spare
   ▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▒▒   too long    by 1.2 seconds
                                      └──┘  overrun drawn past the slot
   ▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒     too short   by 2.9 seconds
                      └──────────────┘      unfilled slot drawn to the end
```

* Draw time to true scale in a sunken trough. Short lines get short troughs.
* Draw takes inside the trough using `--fit` for fits, `--over` for the part past the slot, and `--short` for slot left unfilled.
* **A short take is a miss, not a fit.** The first run produced a take at 59% of its slot and the pipeline called it clean. The bar makes 2.9 seconds of dead air as visible as 1.2 seconds of overrun, with the same visual weight in the opposite direction.
* Show earlier takes as faint ghost shapes behind the active take.
* Indicate the exact repair method. Stretched takes display a hatched pattern with speed percentage. Rewritten takes display a solid fill.
* Render on canvas, repainting on resize and theme changes.

---

## 7. Structural Honesty

* Never show a length measurement without its source take. Link the number directly to playable audio.
* Always state sample sizes for learned speech rates. Say "about a fifth longer, from your last 400 lines".
* Report length as an objective measurement, not a subjective verdict. Say "1.2 seconds too long", never "bad fit".
* Show exact costs in dollars and cents before running actions. Display running totals continuously.
* **A cost is every call, counted once.** Say what the total covers. A running total that omits translation calls fails rule 1. A total that double-counts rejected takes also fails rule 1. Creators dub back catalogues and watch the meter closely.
* Report fit as a signed measurement in both directions. Never let "not too long" render as "fits".
* Give every line a visible number.
* If all retries fail, say "still 0.6 seconds too long after 3 tries, this one needs you".

---

## 8. Vocabulary

Use words creators already understand from video editing.

| Never Use | Always Use |
|---|---|
| prior | what we've learned, your usual |
| M&E stem | your music track |
| duck or sidechain | the music dips under the voice |
| diarization | who's speaking |
| transcode or mux | internal terms only, do not display |
| latency | how long it takes |
| token or model call | step that costs money |
| commit | saved |
| draft | not saved yet |
| inspector | details |
| lane | track |
| attempt | try |

Write complete sentences instead of robotic labels.

* Do not write: `Overrun: +1.2s (18%)`.
* Instead write: `This take overruns the slot by 1.2 seconds.`

* Do not write: `Prior: de-DE, spk_1, +22%, n=400`.
* Instead write: `Your German usually runs about a fifth longer. That is from your last 400 lines.`

* Do not write: `Underrun: -2.9s (-41%)`.
* Instead write: `This take is 2.9 seconds short of the slot. The picture keeps going after the voice stops.`

* Do not write: `fix_type: none`.
* Instead write: `This one fits.` (and only when it does). A short take gets the sentence above, never this one.

---

## 9. States and Keyboard Access

Design four states for every panel: loading, empty, error, and populated.

During processing, display a clear lock banner with active step details. Never show an endless spinner.

### Keyboard Shortcuts

* `?` opens keyboard shortcuts dialog.
* `/` focuses the AI command bar.
* Space toggles play and pause.
* `[` and `]` step between lines.
* Arrow keys move selection.
* Escape dismisses modals.

---

## 10. Screens and Routes

| Route | Purpose |
|---|---|
| `/` | Index: lists all dubs, newest first |
| `/new` | Create: upload video, select languages, start |
| `/d/{id}` | Workspace: picture, rail, and interactive timeline |
| `/config` | Settings and API credentials |

### Create Screen (`/new`)

* Drag-and-drop zone for video files.
* Optional background music drop zone.
* One-click "Try sample video (NASA 75s clip)" button for instant testing.
* Projected cost displayed in currency before launching the run.

### The Workspace (`/d/{id}`)

* **Lines tab:** Lists all lines, target slots, actual durations, and try counts.
* **Details tab:** Displays voice model, measured length, repair type, and itemized cost.
* **History tab:** Displays the Git for Video commit DAG alongside what the system learned about your voice.
