# P6 close: remediation of round 6

## Role and scope

I am the REMEDIATION role for the P6 close round 6 finding. I fixed the one H. I did
not review my own work, change the verdict, tick an exit box, change a task status or
commit. My code writes stay inside the allowed paths. My only new document is this
record.

## Finding row

| Finding | Fix | Pin | Mutation |
|---|---|---|---|
| H1. `web/src/routes/d/[id]/+page.svelte` derived the boundary editor, the command bar candidate timeline, the overlap check, the timeline and the length bars from `dub.segments`, and `workspaceSegments` in `internal/api/workspace.go` read that list from `languages[0]` only. Every write used `activeLanguage`, and `applyEditedSegment` updated only `dub.segments`. On a two-language dub the editor showed one track and wrote another, and the saved edit vanished on reload. | `api.LanguageTrack` now carries a `segments` list. `workspaceSegments` reads each language's timeline at the head commit and attaches it to that track. `Dub.Segments` still holds the first track, which is the source track. The page derives `activeSegments` and `activeDub` from `activeLanguage`, drives every timing consumer from them, and `applyEditedSegment` writes into the active track. | Headless Chrome against the real ledger writer, with a deterministic line renderer behind `api.NewServer`. Pre-fix dub `p6r6fixpre3`: Tamil active showed `End 0:04.000` while the ta-IN bound was 2000, the drag saved `0..3333`, a reload showed `End 0:04.000` again, the preview posted the ml timeline `0..4000` and returned `0..3000`, and the confirmed command saved `0..2333`. Post-fix dub `p6r6fixpost2`: Tamil active showed `End 0:02.000`, the drag saved `0..3333`, the preview posted the ta timeline `0..3333` and returned `0..2333`, the confirmed command saved `0..2333`, and a reload showed `End 0:02.333`. SQL rows below. | Reverting `workspaceSegments` to read `tracks[0]` only fails `TestWorkspaceRouteCarriesEachLanguageTimeline` with `ta segments = [... EndMs:4000 ...], want line 1 at 0..2000` and `timeline languages asked = [ml], want ml then ta`. Reverting the page to `dub.segments` reproduces the pre-fix transcript above. |

## What changed

1. `internal/api/wire.go` adds `Segments []Segment` to `LanguageTrack`. The
   `Dub.Segments` comment now says it mirrors the first target language's timeline.
2. `web/src/lib/types.ts` mirrors the field, with the same comments.
3. `testdata/wire/language_track.json` and `testdata/wire/dub.json` carry the new
   key, so the wire parity test sees it.
4. `internal/api/workspace.go` reads every track's timeline at the head commit. It
   attaches each list to its own track and returns the first track's list as the
   source track.
5. `web/src/routes/d/[id]/+page.svelte` derives `activeTrack`, `activeSegments` and
   `activeDub` from `activeLanguage`. The selection, the playhead, the overlap
   check, the command preview, the boundary panel, the speaker panel, the length
   bars, the rail and the timeline all read the active track. `patchActiveSegment`
   writes a saved edit into the active track and keeps `dub.segments` equal to the
   first track.
6. `internal/api/workspace_test.go` gains
   `TestWorkspaceRouteCarriesEachLanguageTimeline`, which fails when every track
   shares the first language's timeline.

## Pre-fix transcript

Copy `/tmp/p6r6fix-prefix` at HEAD `b31a502`. Headless Chrome on port 28183.

Workspace payload for `p6r6fixpre3`:

```text
{"title":"Pre-fix ordered.mp4","source_seg1":4000,
 "tracks":[{"language":"ml-IN","has_segments":false,"line_count":2},
           {"language":"ta-IN","has_segments":false,"line_count":2}]}
```

Tamil active, before the drag:

```text
{"active":"Now hearing Tamil (India).","valueText":"End 0:04.000","valueNow":"4000"}
```

The ta-IN bound in SQL was 2000. The handle showed the ml bound.

Real pointer drag of the end handle to 3333 ms:

```text
POST /api/dubs/p6r6fixpre3/edits
{"kind":"boundary","language":"ta-IN","segment_id":1,"start_ms":0,"end_ms":3333}
201 {"commit_id":"63652356dccd5359c1b8daf2763adc30","action":"boundary_nudged",
     "author":"manual_ui","segment":{"id":1,"start_ms":0,"end_ms":3333,...}}
```

After the drag the handle read `End 0:03.333`. A reload reset it:

```text
{"active":"Now hearing Tamil (India).","valueText":"End 0:04.000","valueNow":"4000"}
```

The saved ta-IN edit vanished from the editor.

Command preview and confirm:

```text
POST /api/editor/commands/preview
{"command":"shorten line 1 by 1s.","timeline":{"duration_ms":75008,
 "segments":[{"id":1,"start_ms":0,"end_ms":4000,...},{"id":2,"start_ms":5000,"end_ms":9000,...}]}}
200 {"summary":"Shorten line 1 by 1.000s.","segment":{"id":1,"start_ms":0,"end_ms":3000,...}}

POST /api/dubs/p6r6fixpre3/edits
{"kind":"command","language":"ta-IN","command":"shorten line 1 by 1s.","duration_ms":75008}
201 {"action":"user_command","author":"command_bar",
     "segment":{"id":1,"start_ms":0,"end_ms":2333,...}}
```

The preview posted the ml timeline and promised `0..3000`. The save produced
`0..2333`.

SQL rows for `p6r6fixpre3`, `ta-IN`, joined to `commits_raw`:

```text
version_seq  segment_index  start_ms  end_ms
1            1              0         2000
1            2              5000      9000
2            1              0         3333
3            1              0         2333
```

Head state through `timeline_at_commit`: line 1 `0..2333`, line 2 `5000..9000`.
The ta-IN take still carried `slot_ms` 2000:

```text
ta-IN  1  1  2000  2000
```

## Post-fix transcript

Fixed checkout. Headless Chrome on port 28182. Dub `p6r6fixpost2`.

Workspace payload:

```text
ml-IN segments: line 1 0..4000, line 2 5000..9000
ta-IN segments: line 1 0..2000, line 2 5000..9000
source segments: line 1 0..4000, line 2 5000..9000
```

Tamil active, before the drag:

```text
{"active":"Now hearing Tamil (India).","valueText":"End 0:02.000","valueNow":"2000"}
```

Real pointer drag of the end handle to 3333 ms:

```text
POST /api/dubs/p6r6fixpost2/edits
{"kind":"boundary","language":"ta-IN","segment_id":1,"start_ms":0,"end_ms":3333}
201 {"commit_id":"78af0ba1641496f2c9519dfdd948ac7e","action":"boundary_nudged",
     "author":"manual_ui","segment":{"id":1,"start_ms":0,"end_ms":3333,...}}
```

Command preview and confirm:

```text
POST /api/editor/commands/preview
{"command":"shorten line 1 by 1s.","timeline":{"duration_ms":75008,
 "segments":[{"id":1,"start_ms":0,"end_ms":3333,...},{"id":2,"start_ms":5000,"end_ms":9000,...}]}}
200 {"summary":"Shorten line 1 by 1.000s.","segment":{"id":1,"start_ms":0,"end_ms":2333,...}}

POST /api/dubs/p6r6fixpost2/edits
{"kind":"command","language":"ta-IN","command":"shorten line 1 by 1s.","duration_ms":75008}
201 {"action":"user_command","author":"command_bar",
     "segment":{"id":1,"start_ms":0,"end_ms":2333,...}}
```

The preview posted the ta timeline and promised `0..2333`. The save produced
`0..2333`. The preview equals the saved value.

Reload, Tamil active:

```text
{"active":"Now hearing Tamil (India).","valueText":"End 0:02.333","valueNow":"2333",
 "line1":"Line 1\nPlay take\nLine 1 length bar. This take fits the 2.33 seconds slot."}
```

The saved edit survived the reload.

SQL rows for `p6r6fixpost2`, `ta-IN`:

```text
version_seq  segment_index  start_ms  end_ms
1            1              0         2000
1            2              5000      9000
2            1              0         3333
3            1              0         2333
```

Head state: line 1 `0..2333`, line 2 `5000..9000`.

## Single-language re-measure

Dub `p6r6fixone2`, one language `ml-IN`, line 1 by Suni Williams and line 2 by
Mark Vande Hei.

Real pointer drag of line 1 end to 3666 ms:

```text
POST /api/dubs/p6r6fixone2/edits
{"kind":"boundary","language":"ml-IN","segment_id":1,"start_ms":0,"end_ms":3666}
201 {"action":"boundary_nudged","author":"manual_ui","segment":{"id":1,"start_ms":0,"end_ms":3666,...}}
```

Speaker change on line 1 to Mark Vande Hei. The estimate showed before the call.

```text
POST /api/dubs/p6r6fixone2/lines/1/rerender
{"language":"ml-IN","speaker":"Mark Vande Hei"}
201 {"segment_id":1,"language":"ml-IN","take":{"name":"seg_1_try1.wav","attempt":1,
     "voice":"ml-IN-Chirp3-HD-Achird","fit":{"slot_ms":3666,"measured_ms":3666,...}}}
```

Target text correction on line 1.

```text
POST /api/dubs/p6r6fixone2/lines/1/rerender
{"language":"ml-IN","text":"തിരുത്തിയ വരി ഒന്ന്."}
201 {"segment_id":1,"language":"ml-IN","take":{"name":"seg_1_try2.wav","attempt":2,
     "voice":"ml-IN-Chirp3-HD-Achird",...}}
```

SQL head state through `timeline_at_commit`:

```text
segment_index  start_ms  end_ms  speaker         source_text              text
1              0         3666    Mark Vande Hei  Hi, I am Suni Williams   തിരുത്തിയ വരി ഒന്ന്.
2              5000      9000    Mark Vande Hei  and I am an astronaut    ഞാൻ ഒരു ബഹിരാകാശ സഞ്ചാരി.
```

SQL takes view. The seeded take stayed beside the new ones:

```text
segment_index  attempt  speaker         slot_ms  voice                     audio_path
1              1        Suni Williams   4000     ml-IN-Chirp3-HD-Achernar  seg_1_try1.wav
1              1        Mark Vande Hei  3666     ml-IN-Chirp3-HD-Achird    .../work/ml-IN/seg_1_try1.wav
1              2        Mark Vande Hei  3666     ml-IN-Chirp3-HD-Achird    .../work/ml-IN/seg_1_try2.wav
2              1        Mark Vande Hei  4000     ml-IN-Chirp3-HD-Achird    seg_2_try1.wav
```

## Fixture path

Headless Chrome opened `/d/fixture`. The page rendered four disabled fieldsets,
eight length rows and the boundary handle `End 0:07.728`. A typed command sent no
request. The read-only sentences read `This offline fixture cannot save an edit.`

## Mutation

I restored the pre-fix read in `workspaceSegments` so every track shared the first
language's timeline. `go test -count=1 -run
TestWorkspaceRouteCarriesEachLanguageTimeline ./internal/api/` failed:

```text
workspace_test.go:317: ta segments = [{ID:1 StartMs:0 EndMs:4000 ...} ...],
  want line 1 at 0..2000
workspace_test.go:323: timeline languages asked = [ml], want ml then ta
```

I restored the fix. The same command passed.

## Commands

| Command | Result |
|---|---|
| `go build ./...` | exit 0 |
| `go vet ./...` | exit 0 |
| `go test -count=1 ./internal/... ./cmd/...` | exit 0, every package ok |
| `npm --prefix web run check` | 183 files, 0 errors, 0 warnings |
| `python3 tools/audit_docs.py` | exit 0, `No drift. Docs and repository agree.` |
| grep `export let`, `$:`, `svelte/store`, `writable(`, `readable(` over `web/src` | no matches |
| `git status --short` | only the allowed paths plus this record and the untracked round 6 review |

## Files changed

- `internal/api/wire.go`
- `internal/api/workspace.go`
- `internal/api/workspace_test.go`
- `web/src/lib/types.ts`
- `web/src/routes/d/[id]/+page.svelte`
- `testdata/wire/dub.json`
- `testdata/wire/language_track.json`

## Cleanup

I stopped the harness servers, removed the ClickHouse container, released every
browser tab and deleted the temporary copies, seeds, data directories and binaries.
No container, server or browser survives.
