# Project naming

Task: let a creator name a project at creation and rename it afterwards.
Branch: master, HEAD 6ee35f8, no commit.
Date: 2026-09-09.

## The mechanism

The create form at `web/src/routes/new/+page.svelte` collected a video, an optional music
track, a source language and a target language. It collected no name. The upload handler in
`internal/api/upload.go` parsed `language`, `languages`, `source_language`,
`source_languages`, `video` and `music` only. It ignored every other text part.
`writeUploadRecord` set `Title: upload.Video.Name`, so every project was named after its
file. Nothing could rename a project later. The index and the workspace both read that one
title, so both showed the raw filename forever.

## The fix

Server, `internal/api/upload.go`:

- `Upload` gains a `Title` field with the JSON key `title`, so the create answer names the
  project.
- `ServeHTTP` reads a `title` part through `readUploadField`, which trims it. A name longer
  than 200 runes is refused with the sentence
  `The project name is too long. Keep it to 200 characters or fewer.` A blank name falls
  back to the video filename through `projectTitle`, so the old behaviour holds.
- `writeUploadRecord` stores `projectTitle(upload.Title, upload.Video.Name)` and writes
  through the new `saveUploadRecord`.
- `NewRenameHandler` serves `POST /api/dubs/{id}/title`. The body is `{"title": "..."}` and
  the answer is `ProjectTitle` with the stored name. It trims the name and refuses a blank
  name with `A project name cannot be blank.` It refuses a name past the limit and answers
  404 for an id no upload record names. It rewrites the title alone and keeps the language,
  the source language and `created_at`.

Route and wiring:

- `internal/api/server.go` adds `ServerOptions.Rename` and mounts
  `POST /api/dubs/{id}/title`.
- `cmd/ajilamu/main.go` passes `api.NewRenameHandler(uploadDir)`.

Web:

- `web/src/routes/new/+page.svelte` adds a Project name field and appends it as the `title`
  part.
- `web/src/routes/d/[id]/+page.svelte` adds a Rename control. The form posts the new name
  and then shows the name the server answers. Svelte renders that value as text, so markup
  stays literal. The offline fixture stays read only.

## Pins

Each pin is an independent measurement. The unit command was
`go test -count=1 -run <name> ./internal/api/`.

| Where | What | Pin | Mutation |
|---|---|---|---|
| `upload_test.go` `TestUploadHandlerStoresCreatorTitle` | a create with `"  Launch Film  "` | the response, the record and the index all carry `Launch Film` | `projectTitle` ignoring the creator name gave `response title = "clip.mp4", want Launch Film` |
| `upload_test.go` `TestUploadHandlerNamesProjectFromVideoWithoutTitle` | a create with no title part | the response and the record carry `clip.mp4` | removing the fallback stores an empty title |
| `upload_test.go` `TestUploadHandlerKeepsTitleMarkupAsText` | a name holding tags and quotes | the record holds the exact text | a store that strips markup fails |
| `upload_test.go` `TestUploadHandlerRejectsOverlongTitle` | a name of 201 runes | 400 with the plain sentence and no project written | deleting the rune check writes a 201 project |
| `upload_test.go` `TestUploadHandlerAnswersTitlePastFieldGuard` | a name past the 8 KiB field guard | 400 with the limit sentence | the generic `store upload` answer returns |
| `upload_test.go` `TestRenameHandlerStoresNewTitle` | a rename to `"  Festival Cut  "` | the answer, the record and the index carry `Festival Cut`, and every other field is unchanged | a handler that skips `saveUploadRecord` leaves `clip.mp4` |
| `upload_test.go` `TestRenameHandlerKeepsMarkupAsText` | a script tag as the name | the record holds the exact text | the same store mutation |
| `upload_test.go` `TestRenameHandlerRejectsBlankTitle` | empty, spaces and whitespace | 400 with the blank sentence and the old title | deleting the blank check stores an empty title |
| `upload_test.go` `TestRenameHandlerRejectsOverlongTitle` | a name of 201 runes | 400 with the limit sentence and the old title | deleting the rune check stores the long title |
| `upload_test.go` `TestRenameHandlerAnswersTitlePastBodyGuard` | a rename body past the byte guard | 400 with the limit sentence and the old title | the read-failure sentence returns |
| `upload_test.go` `TestRenameHandlerRejectsUnknownProject` | an id no upload wrote | 404 | a handler that invents a record answers 200 |
| `upload_test.go` `TestRenameHandlerRejectsInvalidRequests` | GET and a non-JSON body | 405 with `Allow: POST`, and 400 | a route without the method guard answers 200 |
| `server_test.go` `TestServerRoutesProjectNaming` | the real mux | create, index, workspace, rename, blank rename | dropping the `Rename` mount answers 404 |

### Live pins

The server ran from the real entrypoint on port 18937 with `AJILAMU_DATA_DIR`
`/tmp/ajilamu-naming-data` and the ClickHouse settings from `.env`.

A create with a title:

```
curl -s -i -F "video=@/tmp/naming-clip.mp4;type=video/mp4" -F "language=ml" -F "title=Launch Film" \
  http://127.0.0.1:18937/api/dubs/new

HTTP/1.1 201 Created
{"id":"10c7541454c7479ad60929a8bb1488f0","title":"Launch Film","source_language":"","language":"ml","video":{"name":"naming-clip.mp4","path":"10c7541454c7479ad60929a8bb1488f0/source.mp4","bytes":17}}
```

The record on disk, `/tmp/ajilamu-naming-data/uploads/10c7541454c7479ad60929a8bb1488f0/project.json`:

```
{"id":"10c7541454c7479ad60929a8bb1488f0","title":"Launch Film","source_language":"","language":"ml","created_at":"2026-09-09T18:11:01Z"}
```

The index row and the workspace payload:

```
{'id': '10c7541454c7479ad60929a8bb1488f0', 'title': 'Launch Film', 'languages': ['ml'], 'readiness': 'pending', ...}
{'id': '10c7541454c7479ad60929a8bb1488f0', 'title': 'Launch Film', 'readiness': 'pending'}
```

The DOM showed `h2` `Launch Film` on the index and `h1` `Launch Film` in the workspace.

A create with no title:

```
{"id":"02922d2267e812f506a4df773ab220b5","title":"clip.mp4", ...}
{"id":"02922d2267e812f506a4df773ab220b5","title":"clip.mp4","source_language":"","language":"ml","created_at":"2026-09-09T18:11:08Z"}
```

An over-long create answered 400 with `The project name is too long. Keep it to 200 characters or fewer.`

A rename to a markup name:

```
curl -s -i -X POST -H 'Content-Type: application/json' \
  -d '{"title":"<b>Festival</b> Cut"}' \
  http://127.0.0.1:18937/api/dubs/10c7541454c7479ad60929a8bb1488f0/title

HTTP/1.1 200 OK
{"id":"10c7541454c7479ad60929a8bb1488f0","title":"\u003cb\u003eFestival\u003c/b\u003e Cut"}
```

The record on disk carried `"\u003cb\u003eFestival\u003c/b\u003e Cut"`, which decodes to the
literal text. The index row and the workspace payload carried the literal text. The workspace
DOM showed `h1` text `<b>Festival</b> Cut`, `innerHTML` `&lt;b&gt;Festival&lt;/b&gt; Cut`, and
no child elements.

A rename through the control: the creator typed `Browser Rename`. The heading became
`Browser Rename`, the record on disk held `Browser Rename`, and the index row and the
workspace heading both showed `Browser Rename` after a reload.

A blank rename through the control: the note read `A project name cannot be blank.`, the
heading stayed `Browser Rename`, and the form stayed open.

An over-long rename through the control: the note read
`The project name is too long. Keep it to 200 characters or fewer.` and the heading did not move.

A create through the form: a real 1 second mp4, the name `Form Named Project`. The record on
disk `/tmp/ajilamu-naming-data/uploads/a2d5a672ee637b190aeaad62dba37225/project.json` carried
the name. The workspace heading and the index row both showed `Form Named Project`.

### Measured mutation

`projectTitle` returning `videoName` alone. Three pins failed:

```
--- FAIL: TestUploadHandlerStoresCreatorTitle
    upload_test.go:301: response title = "clip.mp4", want Launch Film
--- FAIL: TestUploadHandlerKeepsTitleMarkupAsText
    upload_test.go:360: stored title = "clip.mp4", want "<b>Final</b> & \"best\" cut"
--- FAIL: TestServerRoutesProjectNaming
    server_test.go:1150: create title = "clip.mp4", want "Festival Cut"
```

Restoring the trimmed-title branch made all three pass.

## Verification

- `go build ./...` passed.
- `go vet ./...` passed.
- `go test -count=1 ./internal/... ./cmd/...` passed.
- `npm --prefix web run check` reported 0 errors and 0 warnings across 183 files.
- `python3 tools/audit_docs.py` reported `No drift. Docs and repository agree.`
- The server ran on port 18937 for the pins and stopped afterwards. No server survives.

## Files changed

- `internal/api/upload.go`. The `Upload.Title` field, the `title` part, the rune limit, the
  filename fallback, `saveUploadRecord`, `NewRenameHandler` and `ProjectTitle`.
- `internal/api/upload_test.go`. The create, fallback, markup, limit and rename pins.
- `internal/api/server.go`. `ServerOptions.Rename` and the route.
- `internal/api/server_test.go`. The route pin through the real mux.
- `cmd/ajilamu/main.go`. One `Rename` option line.
- `web/src/routes/new/+page.svelte`. The Project name field and the `title` part.
- `web/src/routes/d/[id]/+page.svelte`. The Rename control and its sentences.

## Contract notes

- `web/src/lib/types.ts` is unchanged. `ProjectTitle` lives in `upload.go` and mirrors no
  struct in `wire.go`, so the Go and TypeScript parity test does not cover it.
- No commit. `git status` lists the allowed paths and this record alone.
