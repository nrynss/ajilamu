# P7 close: independent verification, round 1

**Scope:** Phase 7 exit criteria for T7.0 through T7.7 as they stand at HEAD `4a4747b`.
**Method:** Independent measurement of every claim in a clean clone at `/tmp/p7close/ajilamu`. No reliance on per-task review files.
**Verdict:** REMEDIATE. Findings: 0 C, 1 H, 0 M, 0 L.

| C | H | M | L |
|---|---|---|---|
| 0 | 1 | 0 | 0 |

I own only this review file. I did not treat task review files as evidence. I re-measured every artifact.

The live worktree carries uncommitted P6 edits in `cmd/ajilamu/main.go` and `internal/api/server.go`. I did not judge them. I cloned the committed HEAD and measured there.

## Findings

| Where | What | Pin | Mutation |
|---|---|---|---|
| `internal/fit/loop.go:262-271` | A take left without a resume record blocks every later run of that project. `readSegmentRecord` returns false on a missing or mismatched record and never removes the take the fresh render will claim. The synthesizer refuses to overwrite, so the run fails and stays failed. | A throwaway test seeded a work dir with `seg_1_try1.wav` and no record. `readSegmentRecord` returned false and left the file. `RunPipeline` with `tts.NewFixtureSynthesizer` failed with `repair line 1: synthesize line 1 attempt 1: open .../seg_1_try1.wav: file exists`. | Reverting a fix that discards the stale take restores the permanent resume failure. |

### H1: a take without a resume record blocks resumption forever

`repairSegment` writes the take inside `RepairLine` and writes the record after it returns (`internal/fit/loop.go:959-962`). An interruption in that window leaves `seg_N_try1.wav` with no record. The window is reachable in normal use. A cancel or a shutdown between the take write and the record write leaves the take. A record write that fails leaves the take too. The loop never cleans the work directory on failure (`internal/api/run.go:402-414`).

The next run reads no record and renders afresh. `internal/fit/loop.go:262-265` returns false for a missing record. `internal/fit/loop.go:270-271` returns false for a record that names another language or segment. Neither path discards the take. `RepairLine` then synthesizes to the same path (`internal/fit/rewrite.go:318`), and `internal/tts/chirp.go:119` refuses to overwrite it. The run fails with `Output audio file already exists` and repeats on every retry until an operator deletes the file.

`discardTakeFile` exists for exactly this hazard. Its comment at `internal/fit/loop.go:239-240` says the fresh render claims the same path and a leftover file would block it. The code calls it only when a record names an unusable take (`internal/fit/loop.go:273-277`).

The committed resume tests do not cover this case. `TestPipelineResumesAfterCompletedTake` models the interruption as a synthesizer error that writes no file (`internal/fit/loop_test.go:1236`). The corruption tests start from a record that exists.

```go
	if recorded, ok := readSegmentRecord(ctx, p.cfg.WorkDir, p.cfg.Language, seg); ok {
		take := reusableTake(recorded)
		if take == nil {
			return recorded, nil
		}
		cfg.InitialTake = take
		cfg.InitialText = recordedText(recorded)
	} else if cfg.PathBuilder != nil {
		discardTakeFile(cfg.PathBuilder(seg.ID, 1, false))
	}
```

## Task status and owns

Every P7 task reads `status: done`. Every owns path exists on disk.

| Task | Owns | Present |
|---|---|---|
| T7.0 | `.gitignore`, `cmd/ajilamu/main.go`, `internal/api/server.go`, `internal/api/server_test.go`, `internal/api/static.go`, `internal/config/config.go`, `internal/config/config_test.go` | yes |
| T7.0a | `web/package.json`, `web/svelte.config.js`, `.gitignore` | yes |
| T7.1 | `internal/api/upload.go`, `internal/api/upload_test.go`, `web/src/routes/new/+page.svelte` | yes |
| T7.2 | `internal/api/sample.go`, `internal/api/sample_test.go`, `internal/api/server.go`, `internal/api/server_test.go`, `cmd/ajilamu/main.go`, `web/src/routes/new/+page.svelte`, `web/src/routes/d/[id]/+page.svelte`, `web/src/lib/fixture.ts`, `web/src/routes/+layout.svelte` | yes |
| T7.2a | `internal/ledger/client.go`, `internal/ledger/commits.go`, `internal/ledger/priors.go`, `internal/ledger/history.go`, `internal/ledger/commits_test.go`, `internal/ledger/priors_test.go`, `internal/ledger/history_test.go` | yes |
| T7.2b | `internal/api/server.go`, `internal/api/server_test.go`, `internal/api/ready_internal_test.go` | yes |
| T7.2c1 | `internal/ledger/commits.go`, `internal/ledger/commits_test.go`, `internal/ledger/history.go`, `internal/ledger/history_test.go` | yes |
| T7.2c | `internal/api/history.go`, `internal/api/history_test.go`, `internal/api/server.go`, `internal/api/server_test.go`, `internal/api/wire.go`, `cmd/ajilamu/main.go`, `cmd/ajilamu/main_test.go`, `web/src/lib/types.ts`, `web/src/lib/Rail.svelte`, `web/src/lib/tabs/HistoryTab.svelte` | yes |
| T7.2d | `web/src/routes/d/[id]/+page.svelte` | yes |
| T7.3 | `internal/api/run.go`, `internal/api/run_test.go`, `internal/api/events.go`, `internal/api/events_test.go`, `internal/api/server.go`, `internal/api/server_test.go`, `cmd/ajilamu/main.go`, `cmd/ajilamu/main_test.go` | yes |
| T7.3a | `internal/fit/loop.go`, `internal/fit/loop_test.go` | yes |
| T7.3b | `web/src/lib/progress.ts`, `web/src/routes/d/[id]/+page.svelte` | yes |
| T7.3c | `internal/media/ffmpeg.go`, `internal/media/probe.go`, `internal/media/media_test.go`, `internal/assemble/bed.go`, `internal/assemble/place.go`, `internal/assemble/duck.go`, `internal/assemble/export.go`, `internal/assemble/peaks.go`, `internal/assemble/bed_test.go`, `internal/assemble/place_test.go`, `internal/assemble/duck_test.go`, `internal/assemble/export_test.go`, `internal/assemble/peaks_test.go`, `internal/fit/measure.go`, `internal/fit/measure_test.go`, `internal/fit/stretch.go`, `internal/fit/stretch_test.go`, `internal/fit/rewrite.go`, `internal/fit/rewrite_test.go`, `internal/fit/loop.go`, `internal/fit/loop_test.go`, `internal/gemini/segment_live_test.go`, `testdata/fixtures_test.go` | yes |
| T7.4 | `internal/api/index.go`, `internal/api/index_test.go`, `web/src/routes/+page.svelte`, `web/src/routes/config/+page.svelte` | yes |
| T7.4a | `internal/api/server.go`, `internal/config/settings.go`, `internal/config/settings_test.go`, `cmd/ajilamu/main.go` | yes |
| T7.5a | `internal/ledger/workspace.go`, `internal/ledger/workspace_test.go` | yes |
| T7.5 | `internal/api/workspace.go`, `internal/api/workspace_test.go`, `internal/api/server.go`, `cmd/ajilamu/main.go`, `internal/ledger/workspace.go`, `internal/ledger/workspace_test.go`, `web/src/routes/d/[id]/+page.svelte` | yes |
| T7.5b | `internal/api/upload.go`, `internal/api/upload_test.go`, `internal/api/sample.go`, `internal/api/sample_test.go`, `internal/api/run.go`, `internal/api/run_test.go`, `internal/api/workspace.go`, `internal/api/workspace_test.go`, `cmd/ajilamu/main.go`, `web/src/routes/new/+page.svelte` | yes |
| T7.7 | `internal/tts/catalog.go`, `internal/tts/catalog_test.go`, `internal/api/languages.go`, `internal/api/languages_test.go`, `internal/api/wire.go`, `internal/api/wire_test.go`, `web/src/lib/types.ts`, `web/src/lib/LanguagePicker.svelte`, `internal/api/server.go`, `cmd/ajilamu/main.go` | yes |
| T7.6 | `web/package.json`, `web/package-lock.json`, `web/src/app.html`, `.gitignore`, `cmd/ajilamu/main.go` | yes |

I did not tick the phase exit boxes.

## Prior-round residue

I re-measured the pins that closed each P7 task at the artifact level. I did not quote those reviews as proof.

**T7.0: zero residue.** A credential-free binary starts and serves. `ENV=production` exits 1 naming `CLICKHOUSE_HOST`. A range request returns 206 with 1024 bytes. My own flush probe left one journal file and pending 1 before shutdown, and zero of both after. The stand-in received the row.

**T7.0a: zero residue.** `npm ci` and `npm run build` wrote `web/build` with `index.html`. The server answered 200 for `/`, `/new`, `/config`, and `/d/fixture`.

**T7.1: zero residue.** A 512 MiB upload returned 201 while server RSS stayed at 27128 KiB across 31 samples. Stored files measured mode 0600. The projected fee formula divides the P2 fixture total by its measured duration.

**T7.2: zero residue.** `POST /api/dubs/sample` returned 201 with `en` to `ml`, and the record on disk matched. `GET /api/dubs` listed it on the next request.

**T7.2a: zero residue.** The central helper tests passed. `TestOnlyClientPinsServerSettings` still finds no setting name outside `client.go`.

**T7.2b: zero residue.** `GET /api/ledger/ready` answered 503 `misconfigured` with `missing required environment variable: CLICKHOUSE_HOST`. The route named the variable before it opened a connection. I could not re-measure the cold and waking cases without ClickHouse.

**T7.2c1: zero residue.** The ledger suite passed, including the quoted-integer and single-pin tests.

**T7.2c: zero residue.** All three read routes answered 503 with no reader. The History tab rendered the fixture DAG.

**T7.2d: zero residue.** Headless Chromium loaded `/d/fixture`, showed the three tabs, switched to History, and logged no console error or exception. The panel rendered the saved history.

**T7.3: zero residue.** The run routes answered 503 with no runner. The event tests passed.

**T7.3a: one residue.** The committed resume test passed. Finding H1 records a hole in the same resume class. Prior rounds 1 to 3 found other members of that class, so H1 does not reopen a closed finding. It is new.

**T7.3b: zero residue.** The progress client and the workspace run panel exist. No browser test runner ships, so I measured the rendered History tab only.

**T7.3c: zero residue.** The media and assemble suites passed on the committed fixtures.

**T7.4: zero residue.** The index answered 200. The config route answered 200 for the page and 204 for a save.

**T7.4a: zero residue.** A marker save returned 204. The file landed at `$AJILAMU_DATA_DIR/settings/credentials.json` at mode 0600 inside a 0700 directory. `GET /api/config` reported presence only. Nine responses held no marker.

**T7.5a: zero residue.** The ledger workspace reads passed, including the quoted-integer stand-in.

**T7.5: zero residue.** `GET /api/dubs/{id}` answered 503 with no reader. The route fills `source_language` from the record through `ProjectLookup`.

**T7.5b: zero residue.** A browser-shaped upload of `en-US` to `ml-IN` returned 201 with both values, and the record carried the same pair. The sample recorded `en` to `ml`.

**T7.7: zero residue.** A credential-free server served 53 committed languages with no `fetched_at`. Refresh answered 502 and named Application Default Credentials. The cache still held 53. The wire parity tests passed.

**T7.6: zero residue.** A headless load of `/d/fixture` and `/new` made no favicon request and returned no response at or above 400. `package-lock.json` is committed.

## Exit criteria

I re-measured each criterion. I did not tick the boxes.

| Criterion | Independent result |
|---|---|
| The server runs from a clone with no credentials and serves the workspace from fixtures | A credential-free binary started, served `/d/fixture` 200, and returned the fixture summary from `GET /api/dubs`. |
| Shutdown flushes the ledger queue without losing events | My probe left one journal file and pending 1, then zero of both after `Shutdown` and `FlushLedger`. The stand-in received the row. |
| Large video files stream directly to storage without memory bloat | A 512 MiB upload returned 201. Server RSS stayed at 27128 KiB across 31 samples. |
| Users can upload optional background music tracks | An upload with `video` and `music` returned 201. Both files landed at mode 0600. A missing music part still returned 201. |
| Sample mode launches instantly with one click | The create screen's button posts once to `/api/dubs/sample`. The route returned 201 in about 0.15 s and copied the committed clip. |
| Progress events stream complete sentences with running costs | `eventEmitter` stamps the cumulative total on every pipeline event. Assembly and export events stamp the run total. The wire and event tests passed. |
| Interrupted runs remain resumable without data corruption | The committed resume test passed. Finding H1 shows a take without a record blocks every later run. |

### Language work

| Claim | Independent result |
|---|---|
| The catalog serves a clone with no credentials | `GET /api/languages` returned 200 with 53 sorted committed codes, `source` `committed`, and no `fetched_at`. |
| The refresh fetches from the provider | `POST /api/languages/refresh` answered 502 and named Application Default Credentials on the credential-free server. The cache still held 53. The handler path calls `voices.list` and swaps the cache only after success. |
| The create screen stores both languages | The form appends `source_language` and `language`. A POST with `en-US` and `ml-IN` returned 201, and `project.json` carried the same pair. The sample stored `en` to `ml`. |

## Independent measurements

I built and ran the committed tree in `/tmp/p7close/ajilamu` at `4a4747b`. I ran `npm ci` and `npm run build` there. Go reports 1.27.1. Node reports 26.8.1.

`go build ./...` passed. `go test -count=1 ./internal/api/... ./internal/tts/... ./internal/config/... ./cmd/...` passed. `go test -count=1 ./internal/fit/... ./internal/ledger/... ./internal/assemble/...` passed.

The first test run failed two packages with `no space left on device`. The `tmpfs` at `/tmp` was full from other scratch trees. I moved `TMPDIR` to `/home` and the same tests passed. That failure was environmental.

The `internal/media` package fails in a clean clone. Its tests need `scratch/takes/` and `assets/source/`, which `.gitignore` excludes. That dependency predates P7 and I record it as a note.

### Server, credential-free

| Probe | Result |
|---|---|
| `GET /api/languages` | 200, 53 codes, `source` `committed` |
| `POST /api/languages/refresh` | 502, named Application Default Credentials |
| `GET /api/languages` after the failure | still 53 committed |
| `GET /` `/new` `/config` `/d/fixture` | 200 HTML |
| `GET /api/dubs` | 200, fixture summary plus uploads |
| `GET /api/ledger/ready` | 503 `misconfigured` naming `CLICKHOUSE_HOST` |
| `GET /api/dubs/{id}` | 503 with the fixed ledger sentence |
| `GET /api/dubs/{id}/history` and peers | 503 |
| `GET /api/healthz` | 200 `{"status":"ok"}` |
| `GET /api/nope` | 404 |
| `GET /api/dubs/sample` | 405 |
| `GET /clip.mp4` with `Range: bytes=0-1023` | 206, 1024 bytes |
| `ENV=production` with no secrets | exit 1, named `CLICKHOUSE_HOST` |

### Upload and sample

A 512 MiB file returned 201 in 1.83 s. RSS sampled every 50 ms stayed at 27128 KiB. A second upload carried `video`, `music`, `source_language=en-US`, and `language=ml-IN`. It returned 201. The record read:

`{"id":"...","title":"small.mp4","source_language":"en-US","language":"ml-IN","created_at":"..."}`

The project directory held `source.mp4` and `music.wav` at mode 0600. The sample route returned 201 with `en` and `ml`, and its record matched.

### Credential persistence

A POST of two markers returned 204. The directory measured 0700 and the file 0600. `GET /api/config` returned `{"voice_key_set":true,"translation_key_set":true}`. Nine responses, including the index and the language routes, held no marker.

### Browser

Headless Chromium 151 loaded `/d/fixture`. The three rail tabs rendered. Clicking History set `aria-selected` on History and the panel showed the saved history. The console held no entry and the runtime threw no exception. A CDP network capture for `/d/fixture` and `/new` recorded no favicon request and no response at or above 400.

### Shutdown flush

A throwaway driver enqueued one ledger row against a stand-in that first answered 503. The queue directory held one journal file and `Pending` returned 1. After `Server.Shutdown` and `Server.FlushLedger`, the directory was empty and `Pending` returned 0. The stand-in received the row.

### Resume probe

A throwaway test seeded a work directory with `seg_1_try1.wav` and no record. `readSegmentRecord` returned false and left the file. `RunPipeline` with the fixture synthesizer failed with `repair line 1: synthesize line 1 attempt 1: open .../seg_1_try1.wav: file exists`.

## audit_docs.py

Command: `python3 tools/audit_docs.py`

Exit code: 0

Printed output:

```
Ajilamu documentation drift audit


No drift. Docs and repository agree.
```

## Notes, not findings

`internal/media` tests need gitignored `scratch/takes/` and `assets/source/`, so a clean clone cannot run the whole offline suite. The dependency comes from T0.3 and predates P7.

`web/src/routes/d/[id]/+page.svelte:542` plays `src="/clip.mp4"` for every project, and no route serves an uploaded video. T5.7 introduced the line and no task owns serving uploads. The phase exit criteria do not cover it.

`POST /api/languages` answers 404 through the `/api/` catch-all, while `GET /api/dubs/sample` answers 405. Earlier reviews accepted the catch-all pattern.

`upload.go` does not validate `source_language`, and `resolveLanguageName` falls back to the raw code. The fallback is deliberate and documented, and no current create-screen path reaches it.

I removed every throwaway probe after measurement. I changed no production code.
