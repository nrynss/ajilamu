# Fresh project cannot start from the interface

## Mechanism

`web/src/routes/d/[id]/+page.svelte` set `workspaceState` to `kind: empty` when the payload
held no segments or languages. `PanelState` renders its children only for `kind: populated`.
The run panel therefore never rendered for a project with no take.

The workspace payload also reported no languages for that project. `WorkspaceTakes` returns
one track per target language that holds a take, and the ledger held none. The upload record
named `de-DE`. Even a rendered button had no language to run into.

## Fix

`internal/api/workspace.go` widens `WorkspaceReader` with `StoredTargetLanguage`. The read
names the target language of the stored upload record. `assembleWorkspace` carries that
language as a track with no lines when the ledger holds no track for it. A track the ledger
already holds wins, because a recorded take is the truth. `UploadTargetLanguage` and
`loadUploadRecord` read the record, and `UploadProjectLookup` now shares the record read.

`cmd/ajilamu/main.go` gives `workspaceReader` the upload storage dir and implements
`StoredTargetLanguage` on it. The upload directory moves above the ledger wiring, so the
adapter receives it.

`web/src/routes/d/[id]/+page.svelte` renders the workspace whenever the payload names a
language. The waiting sentence became a note inside the workspace. The note shows only when
the payload holds no segment and no line. A project with takes renders exactly as before.

## Pins

Every pin is an independent measurement. The local server ran on port 38180 against
ClickHouse `clickhouse/clickhouse-server:26.8.2.7` on HTTP port 38123 and native port 39000.
The schema loaded from `sql/schema.sql` into database `ajilamu_pin`. Ports were high and free.

### Payload

The seed went through the real upload route.

```bash
curl -sS -X POST http://127.0.0.1:38180/api/dubs/new \
  -F "video=@/tmp/ajilamu-pin/media/source.mp4;filename=source.mp4" \
  -F "language=de-DE" -F "source_language=en-US"
```

The ledger held no row for that project.

```text
SELECT count() FROM takes_raw WHERE dub_id='5fbf...'    -> 0
SELECT count() FROM commits_raw WHERE dub_id='5fbf...'  -> 0
```

`GET /api/dubs/5fbf0d21ef7ecc0d37c07a1b4b2a51e5` answered this body.

```json
{"id":"5fbf0d21ef7ecc0d37c07a1b4b2a51e5","title":"source.mp4","source_language":"en-US","readiness":"pending","segments":[],"languages":[{"language":"de-DE","lines":[],"segments":[]}],"charges":[],"total":{"total_nanodollars":0,"covers":"0 segment calls, 0 translation calls, 0 render calls, and 0 agent calls."},"commits":[],"created_at":"2026-09-09T17:51:31Z","updated_at":"2026-09-09T17:51:31Z"}
```

The payload names `de-DE`. The track carries `lines: []` and `segments: []`.

### DOM

The built frontend was served by the same server. Chrome measured the live DOM.

```json
{"title":"source.mp4 · Ajilamu","panelState":"populated",
 "runButton":"Start the dubbing run into DE-DE",
 "waitingNote":"This project is waiting for dubbing. Its video is saved, but no lines, takes, or costs exist yet.",
 "workspace":true,"languageButtons":["German (Germany)"]}
```

The run control renders and names the target language. The waiting sentence appears as a note
inside the workspace.

### Started run

A real click on the run control produced this request and response.

```text
POST http://127.0.0.1:38180/api/dubs/5fbf.../run?language=de-DE  -> 202
```

The server log shows the run reached the pipeline.

```text
ERROR run failed run_id=c7aeab0423ad1f66b17876fb78a2d28f dub_id=5fbf... stage=segmenting
error="segment media: ... auth: cannot fetch token: 400 ... invalid_grant"
```

The run started with `language=de-DE`. The synthetic credentials stop the Gemini call, so the
run fails after it starts. That failure is the expected end of an offline pin.

### Project with a take

A take row inserted into `takes_raw` for the same project changed the payload.

```text
readiness: review
track count: 1
 track de-DE lines 1 segments 0 takes 1
```

The route added no second empty track. The stored language matched the ledger track.

### Missing project

`http://127.0.0.1:38180/d/does-not-exist-000` measured this DOM.

```json
{"panelState":"error",
 "message":"We could not find this project. Check the URL or return to the project index.",
 "runButton":null}
```

### Bar

```text
go build ./...                                  ok
go vet ./...                                    ok
go test -count=1 ./internal/... ./cmd/...       all ok
npm --prefix web run check                      183 files, 0 errors, 0 warnings
python3 tools/audit_docs.py                     No drift. Docs and repository agree. exit 0
```

## Mutation

Backend. `StoredTargetLanguage` returned `""` for every project. A fresh upload answered
`"languages":[]`, which is the original defect. Reverting the mutation restores `de-DE`.

Frontend. The state condition returned to `segments.length > 0 && languages.length > 0`. The
page measured `panelState: "empty"`, no run button, and the waiting sentence as the whole
panel. Reverting the mutation restores `populated` and the run control.

Both mutations break the fix. Each half of the fix is load-bearing on its own.

## Files changed

- `internal/api/workspace.go`
- `internal/api/workspace_test.go`
- `cmd/ajilamu/main.go`
- `web/src/routes/d/[id]/+page.svelte`

## State

No commit. The ClickHouse container is stopped and removed. Both local servers stopped. The
managed browser tab closed. `git status` lists only the four changed paths above and this
record.
