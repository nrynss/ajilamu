# P7: Ingestion and Orchestration (Track E)

```yaml
id:       P7
size:     M
branch:   phase/p7-ingestion
requires: [T1.4]
blocks:   P8
parallel: medium
runs-parallel-with: P2, P3, P4, P5, P6
```

**Goal:** Handle video uploads, orchestrate background dubbing loops asynchronously, and stream real-time progress updates.

**Early start:** Tasks `T7.1` and `T7.4` require only wire contracts. The orchestration engine runs against fixture data until Phase 2 completes.

**T7.0 first:** Every task in this phase writes a handler with nothing to mount it on. T7.0 builds the process that mounts them. Handlers stay plain functions, so the other tasks do not wait for it.

---

### T7.0: Server entrypoint ★
```yaml
requires:   T1.4, T0.2
fixture-ok: yes
size:       M · frontier
owns:       .gitignore, cmd/ajilamu/main.go, internal/api/server.go, internal/api/server_test.go,
            internal/api/static.go, internal/config/config.go, internal/config/config_test.go
status:     done
```
Build the process that runs Ajilamu. It loads configuration, mounts every `internal/api`
handler, serves the built frontend, and shuts down without losing ledger events.

**Why this task exists.** No task owned a server entrypoint. The repository has no `cmd/`
directory, no `main.go`, and nothing that calls `config.Load()`. Eight phases wrote handlers,
domain code, and a frontend against a process that does not exist. Found on 2026-09-08, in the
same sweep that found the ADK gap. Both share a cause. Work nobody names never gets built.

#### A clone with no credentials still runs

This is the load-bearing requirement. A contributor who clones the repository, sets no
environment, and runs the binary gets a working server. The workspace renders from fixtures,
the sample project opens, and the offline suite passes. That holds today under `env -i`, and
this task keeps it true once a process exists to break it.

`config.Load()` requires every secret and exits when one is missing. T0.2 asked for that, and
T0.2 was right when the only consumer always needed ClickHouse. It is wrong for a server whose
fixture and workspace paths need nothing.

Amend it. Configuration splits in two. Process settings load at startup and keep their
defaults. Credentials resolve when a feature first needs one, and a missing credential fails
that request with a message naming the variable. The process never exits because a credential
it has not used yet is absent.

Amend T1.2's neighbour openly, the way T2.2dev amended the cost model. Update the T0.2 block in
`PHASE-0-ground.md` to describe the loader that now exists, rather than leaving the spec and
the code disagreeing.

`ENV=production` keeps the strict behaviour. A deployed host that starts without its ClickHouse
password is a misconfiguration, not a degraded mode, and it fails loudly at boot.

#### Mounting and serving

Handlers stay plain functions in their own files, owned by their own tasks. This task owns the
mux that mounts them and nothing inside them. Adding a route is a one-line change here.

Serve the built Svelte output from `web/`. Serve video with HTTP range requests, because
[T8.2](PHASE-8-ship.md) and the timeline both assume 206 responses and instant scrubbing.

#### Shutdown flushes the ledger

`internal/ledger` holds a durable queue whose contract from T4.1 is that a disconnect drops
zero events. A process that exits on `SIGTERM` without flushing breaks that contract at every
deploy, which is the moment it matters most.

Trap the signal, stop accepting connections, drain in-flight requests, then flush the queue
before exit. Bound the drain, and log what remains queued if the bound expires.

**Done when:** `go run ./cmd/ajilamu` starts with no environment set and serves the workspace
from fixtures. A request needing an absent credential fails with a message naming the variable,
and the process stays up. `ENV=production` with a missing secret exits at boot. Video responds
206 to a range request. `SIGTERM` flushes the ledger queue, proven by a test that enqueues,
signals, and finds zero events lost. The T0.2 block describes the loader that exists.

---

### T7.0a: Static workspace build contract
```yaml
requires:   T5.1, T7.0
fixture-ok: yes
size:       S · mid
owns:       web/package.json, web/svelte.config.js, .gitignore
status:     done
```
Make the Svelte workspace an explicit static build artifact that the Go server can serve.
`adapter-auto` emits no browser-ready `index.html` in this repository. Use the supported static
adapter and a fallback document for client-side routes, including `/d/{id}`. Put the emitted
files in the directory T7.0 serves. Do not add a second Node production server.

**Done when:** `npm run build` emits the frontend artifact expected by T7.0, and starting the
Go binary serves `/`, `/new`, `/config`, and a deep workspace route without Vite or a Node SSR
process.

---

### T7.1: Upload ★
```yaml
requires:   T1.4
fixture-ok: yes
size:       M · mid
owns:       internal/api/upload.go, internal/api/upload_test.go, web/src/routes/new/+page.svelte
status:     done
```
Stream multipart uploads to `/api/dubs/new` directly to persistent disk storage. Avoid buffering full files in system memory.

Accept an optional background music track. When creators provide separate music, dialogue mixes cleanly without requiring ducking.

Provide drag-and-drop zones for video and audio. Display projected cost estimates before starting the job.

**Done when:** Large video uploads stream to disk without memory growth, and the interface displays estimated fees upfront.

---

### T7.2: Sample mode ★
```yaml
requires:   T7.1
fixture-ok: yes
size:       XS · light
owns:       internal/api/sample.go, internal/api/sample_test.go, internal/api/server.go,
            internal/api/server_test.go, cmd/ajilamu/main.go, web/src/routes/new/+page.svelte,
            web/src/routes/d/[id]/+page.svelte, web/src/lib/fixture.ts,
            web/src/routes/+layout.svelte
status:     done
```
Provide a one-click button to launch sample projects using the committed NASA 75-second video. Evaluators can test the system without uploading files.

**Done when:** Clicking the sample button creates a working project instantly from committed test data.

---

### T7.2a: Ledger read settings
```yaml
requires:   T4.3, T4.5, T4.7
fixture-ok: yes
size:       S · light
owns:       internal/ledger/client.go, internal/ledger/commits.go, internal/ledger/priors.go,
            internal/ledger/history.go, internal/ledger/commits_test.go,
            internal/ledger/priors_test.go, internal/ledger/history_test.go
status:     done
```
Give every ClickHouse read the settings it needs, from one place.

**Why this task exists.** `internal/ledger` holds three readers and only one pins its settings.
T4.5 pinned `output_format_json_quote_64bit_integers` and
`max_recursive_cte_evaluation_depth` in `history.go`, because T4.5 owned that file alone.
`commits.go` and `priors.go` pin neither, and both decode `UInt64` out of JSON. Measured on
ClickHouse 26.8.2.7 with the quoting setting at 1, `selectCommit` returns `"version_seq":"7"`
and `selectDurationPrior` returns `"population_samples":"0"`. `Commit.VersionSeq` and
`durationPriorStats.PopulationSamples` then fail to unmarshal.

The runtime default is 0 on a stock server and on the live Cloud instance, so nothing is broken
today. A settings profile that turns quoting on breaks two readers and not the third, and the
failure reads as a client bug.

Build one request helper and route all three readers through it. Copying the pin into each new
reader is what created this, and T7.3 and T7.2c both add readers.

**Done when:** One helper builds every ClickHouse read request. A test drives each of the three
readers against a stand-in that serves the quoted shape and asserts each still decodes. No
reader sets a setting on its own.

---

### T7.2b: Readiness probe budget
```yaml
requires:   T7.0
fixture-ok: yes
size:       XS · light
owns:       internal/api/server.go, internal/api/server_test.go,
            internal/api/ready_internal_test.go
status:     done
```
Give `GET /api/ledger/ready` a budget that matches the deployment target.

`probeClickHouse` bounds `clickHouseProbeClient` at 3 seconds against unauthenticated `/ping`.
Measured against the live ClickHouse Cloud instance on 2026-09-08: a warm ping answers in under
a second, but the first ping on a cold route took 13 seconds and failed both a 3 second and a 10
second budget. An authenticated query answered in 3. With the service fully idle, an
authenticated call needed more than 25 seconds.

The budget is fine steady-state and too tight cold, which is when a readiness route gets asked:
at startup, after a deploy, after idle. The route then reports that ClickHouse did not answer a
ping while the ledger is healthy and merely waking.

Decide the contract rather than only raising the number. A waking service is neither
misconfigured nor broken, and the route says nothing about that today. Keep the route fast when
the answer is already known.

**Owns contention.** Three not-started tasks name `internal/api/server.go`: this one, T7.2c for
its mux line, and T7.4a for its settings store. The graph rule forbids two concurrent tasks
owning one path, so run these three serially in any order. Whoever claims one first edits the
line before starting, as usual. T5.5 owns `web/src/lib/tabs/` and is done, so T7.2c takes
`HistoryTab.svelte` without contention.

**Done when:** A cold ClickHouse answers ready within the route's budget, or the route
distinguishes waking from unreachable in its response. The route still fails fast when
credentials are absent.

---

### T7.2c1: Ledger commit DAG read
```yaml
requires:   T4.3, T4.4, T7.2a
fixture-ok: yes
size:       S · frontier
owns:       internal/ledger/commits.go, internal/ledger/commits_test.go,
            internal/ledger/history.go, internal/ledger/history_test.go
status:     done
```
Serve the commit DAG the History tab needs, and make the branch cost field honest.

**Why this task exists.** T7.2c serves timeline state, the commit DAG, and branch comparison.
`TimelineAt` and `CompareBranches` already exist. Nothing reads commits: `internal/ledger` has no
method that lists a dub's commits or joins their action provenance, so no caller can build the DAG
the wire `Commit` describes. T7.2c does not own `internal/ledger`, so the read lands here first.

**The read.** Add one exported method that returns every commit of one dub with its action
provenance, ordered oldest first by `version_seq` then `commit_id`. Route it through
`queryClickHouse` so both pinned settings apply. `commits` and `actions` are views over their
`_raw` tables. A commit may carry zero, one, or several action rows, and the wire `Commit`
carries one action, one author, and one instruction. Choose a deterministic collapse rule,
document it, and pin it with a test that gives one commit two actions.

**The branch cost decision.** The P4 Handoff Log records that `BranchView.CostUSD` sums
`charges.cost_usd` over an ancestry of commit ids, so it drops every charge row carrying the
default empty `commit_id`. The first caller inherits that gap. Settle it here: either make the
ancestry sum complete or rename the field to say it counts attributed charges only. Measure
whether any shipped writer can produce an unattributed charge, because `charges_raw.commit_id`
defaults to empty while `internal/ledger/takes.go` requires a non-empty commit id on a take.
Record the measurement and the decision in the handoff.

**Done when:** One method returns a dub's commits with action provenance, ordered and
deterministic, and a stand-in test proves the decode. The branch cost field's name matches what
it counts, and the review file records the measurement that decided it.
`go test -count=1 ./internal/ledger` passes.

---

### T7.2c: Ledger read routes
```yaml
requires:   T7.0, T7.2a, T7.2c1, T4.5, T5.5
fixture-ok: yes
size:       L · frontier
owns:       internal/api/history.go, internal/api/history_test.go,
            internal/api/server.go, internal/api/server_test.go,
            internal/api/wire.go, cmd/ajilamu/main.go,
            cmd/ajilamu/main_test.go,
            web/src/lib/types.ts, web/src/lib/Rail.svelte,
            web/src/lib/tabs/HistoryTab.svelte
status:     done
```
Serve timeline state, the commit DAG, and branch comparison over HTTP.

**Why this task exists.** No task read the ledger for the UI. T5.5 built the History tab against
mock JSON and closed. P7 shipped and mounts `healthz`, `ledger/ready`, `GET /api/dubs`,
`POST /api/config`, `POST /api/dubs/new` and `POST /api/dubs/sample`. None reads timeline state,
commits, or costs. `cmd/ajilamu/main.go` imports `internal/ledger` only to build the client for
the shutdown flush. So `TimelineAt` and `CompareBranches` shipped, were reviewed, were probed
against a live server, and have no caller. Found on 2026-09-08. It is the same cause T7.0 names.
Work nobody owns never gets built.

T7.0 mounts handlers and owns nothing inside them, so this task writes the handler and T7.0's
mux gains a line.

**T7.2c1 settles the branch cost question** before this task serves it. Serve the field name that
task chooses.

**Routes.** The History tab fetches its own data by project id, so the workspace page needs no
change. Add three read routes and mount them in T7.0's mux. `GET /api/dubs/{id}/history` returns
the commit DAG. `GET /api/dubs/{id}/timeline?language=..&commit=..` returns that commit's
timeline. `GET /api/dubs/{id}/branches?language=..&a=..&b=..` returns both heads with their slot
duration, take count, and attributed cost. Every response is JSON. A missing credential answers
503 and the tab falls back to the fixture commits it already receives, so a clone with no
credentials still renders the tab.

**Wiring.** `ServerOptions` gains a read interface, and `cmd/ajilamu/main.go` supplies the ledger
client it already builds for the flush. Without credentials that field stays nil and every route
answers 503, which is what the fixture fallback needs. Add the mount assertions to
`internal/api/server_test.go`.

**Done when:** The History tab renders a real commit DAG from ClickHouse rather than fixture
JSON. Time travel to a commit returns that commit's timeline. Branch comparison returns a cost
that a reviewer can tie to `charges` by hand, and the field's name matches what it counts. A
clone with no credentials still serves the tab from fixtures.

---

### T7.2d: Workspace page effect loop
```yaml
requires:   []
fixture-ok: yes
size:       XS · mid
owns:       web/src/routes/d/[id]/+page.svelte
status:     done
```
The workspace page throws `effect_update_depth_exceeded` on load. The effect reads `dub` after it
writes `dub`, so Svelte aborts it and rail tab switching stops. The History tab therefore never
mounts in the running app.

Found on 2026-09-08 by the T7.2c review. Measured at HEAD `26ce432`, whose web sources do not
carry T7.2c, and the Details tab fails the same way. T7.2c's done-when needs this fixed to be
demonstrable in the browser.

**Done when:** Loading `/d/fixture` renders the workspace, switching to the History tab works, and
the console holds no `effect_update_depth_exceeded`.

---

### T7.3: Run engine, server-sent events, and persistence ★
```yaml
requires:   T7.3c, T1.4, T2.7, T3.4, T7.2a
fixture-ok: yes
size:       L · frontier
owns:       internal/api/run.go, internal/api/run_test.go,
            internal/api/events.go, internal/api/events_test.go,
            internal/api/server.go, internal/api/server_test.go,
            cmd/ajilamu/main.go, cmd/ajilamu/main_test.go
status:     done
```
Start a run over HTTP, drive the fit loop and the assembler, persist what they produce, and stream server-sent events.

Send natural sentences rather than technical status codes. For example: "Rendering line 3 in Malayalam" or "This take overruns by 1.2 seconds, trying a shorter line." Include cumulative project cost on every event.

**The import cycle decides the shape.** `internal/fit/loop.go` imports `internal/api` for the wire types, so `internal/api/run.go` cannot import the loop. Break it the way T7.2c broke the ledger seam. Declare the run interface in api types, adapt the pipeline in `cmd/ajilamu/main.go`, and assign the adapter only when its dependencies exist. Do not move the wire types.

**Persistence.** The run mints project_id, owner_id, commit_id, version_seq, and take_id. It flushes the durable queue before it reads the dub's head, so a parent still queued is delivered before `AppendCommit` checks it. It writes takes, charges, commits, actions, and timeline snapshots through `internal/ledger`. A whole-pass charge has no commit until one exists, so decide its attribution here and record the decision.

**Resume is T7.3a. The client is T7.3b.** This task stops at the last event.

**Done when:** A run starts over HTTP, streams complete sentences with cumulative cost through the fixture simulation and a live end-to-end run, and the ledger holds the takes, charges, commits, actions, and snapshots it produced.

### T7.3a: Resumable runs
```yaml
requires:   T7.3
fixture-ok: yes
size:       M · mid
owns:       internal/fit/loop.go, internal/fit/loop_test.go
status:     claimed:orchestrator
```
An interrupted run must resume without re-rendering completed takes.

The loop writes each take with `O_EXCL`, so a second pass over the same work directory fails rather than resumes. `RewriteConfig.InitialTake` already exists and `rewrite.go` honours it, but the loop never sets it. Detect a completed take for a segment, pass it as the initial take, and skip the synthesis that produced it.

**Done when:** A run interrupted after several segments resumes and re-renders only the remaining segments, proven by a test that stops after a completed take and restarts.

---

### T7.3b: Live progress in the workspace
```yaml
requires:   T7.3
fixture-ok: yes
size:       M · mid
owns:       web/src/lib/progress.ts, web/src/routes/d/[id]/+page.svelte
status:     done
```
No web code consumes `ProgressEvent`. `ProcessingBanner.svelte` exists and is exported, and nothing imports it.

Subscribe to the run's event stream, feed the banner the active step and sentence, keep the budget meter on the cumulative cost, and reconnect after the stream drops. `ui-ux.md` lines 130 and 177 name the running totals and the active step banner.

Nothing starts a run today. `ui-ux.md` puts start on the create screen, and that screen only
uploads. This task adds the smallest control that makes the workspace able to start the run it
watches: a start button that posts `/run` for the active language when no run exists.

**Done when:** A run shows its active step and running cost in the workspace, and a dropped stream reconnects without losing the last known cost.

---

### T7.3c: Cancellable media and assembler
```yaml
requires:   []
fixture-ok: yes
size:       M · mid
owns:       internal/media/ffmpeg.go, internal/media/probe.go, internal/media/media_test.go,
            internal/assemble/bed.go, internal/assemble/place.go, internal/assemble/duck.go,
            internal/assemble/export.go, internal/assemble/peaks.go,
            internal/assemble/bed_test.go, internal/assemble/place_test.go,
            internal/assemble/duck_test.go, internal/assemble/export_test.go,
            internal/assemble/peaks_test.go,
            internal/fit/measure.go, internal/fit/measure_test.go,
            internal/fit/stretch.go, internal/fit/stretch_test.go,
            internal/fit/rewrite.go, internal/fit/rewrite_test.go,
            internal/fit/loop.go, internal/fit/loop_test.go,
            internal/gemini/segment_live_test.go, testdata/fixtures_test.go
status:     done
```
Every ffmpeg and ffprobe call uses `exec.Command` with no context, so a run cannot be stopped and shutdown leaves children behind.

Thread `context.Context` through every exported call in `internal/media` and `internal/assemble`, use `exec.CommandContext`, and return an error that names cancellation. Change nothing on the success path.

The change reaches `internal/fit`, because `Measure` and `renderStretch` call media. Thread the
context through them and through `Pipeline.Run`. A `context.Background()` substitute leaves the
stretch and measure steps uncancellable, which is the defect this task exists to remove.

**Done when:** Cancelling the context kills the child process, a test proves it, and every caller still compiles.

---

### T7.4: Index and config routes
```yaml
requires:   T1.4
fixture-ok: yes
size:       S · light
owns:       internal/api/index.go, internal/api/index_test.go, web/src/routes/+page.svelte,
            web/src/routes/config/+page.svelte
status:     done
```
Implement `/` to list projects ordered by creation time. Implement `/config` to manage API keys and application settings.

Ensure credential inputs are write-only. Never log, echo, or expose API secrets back to client browsers.

**Done when:** The index displays active projects, and saved API keys remain hidden from client-side inspection.

---

### T7.4a: Write-only credential persistence
```yaml
requires:   T7.0, T7.4
fixture-ok: yes
size:       S · frontier
owns:       internal/api/server.go, internal/config/settings.go, internal/config/settings_test.go,
            cmd/ajilamu/main.go
status:     not-started
```
Connect T7.4's injected write-only configuration callback to durable server-side storage. Store
secret values outside the repository and image, with permissions that prevent other local users
from reading them. The read API may report whether a value exists, but must never serialize,
log, or return the secret itself. Reuse T7.0's application data-directory contract instead of
inventing a second location.

The adapter that supplies the callback must live in `cmd/ajilamu/main.go`, because `internal/api`
imports `internal/config` and the store cannot accept an `api.ConfigUpdate`. Nothing consumes a
stored key today: `internal/config` has no voice or translation field, and Gemini and TTS
authenticate through ADC. Record that in the handoff rather than inventing a consumer.

**Done when:** A submitted credential survives a process restart, cannot be read through any
HTTP response, is absent from logs, and has restrictive on-disk permissions.

### T7.5a: Ledger workspace reads
```yaml
requires:   T4.2a, T4.6, T7.2a
fixture-ok: yes
size:       M · frontier
owns:       internal/ledger/workspace.go, internal/ledger/workspace_test.go
status:     not-started
```
The wire `Dub` needs takes with their fit and itemized charges, the whole-pass charges, the
running total, the language list, and project metadata. The ledger exposes four reads and none
returns any of that.

Add the reads in one file, routed through `queryClickHouse` so both pinned settings apply. Bind
every id as a parameter. Order each result deterministically.

**Done when:** A stand-in drives each read, the quoted 64-bit shape still decodes, and no read
names a setting on its own.

---

### T7.5: Workspace payload route
```yaml
requires:   T7.5a, T7.3b
fixture-ok: yes
size:       L · frontier
owns:       internal/api/workspace.go, internal/api/workspace_test.go,
            internal/api/server.go, cmd/ajilamu/main.go,
            web/src/routes/d/[id]/+page.svelte
status:     not-started
```
Serve the `Dub` payload the workspace renders, so a real project stops showing the pending
sentence.

**Why this task exists.** The wire contract defines `Dub` for the whole workspace, and
`web/src/lib/fixture.ts` is its only producer. The workspace page loads the fixture for a fixture
id and shows an empty pending state for every other id. T7.2c gives the History tab its own
routes, so a real dub still has no segments, lines, takes, charges, or total. Found on 2026-09-08
while scoping T7.2c. It is the same cause T7.0 names. Work nobody owns never gets built.

**Done when:** `GET /api/dubs/{id}` returns a `Dub` assembled from the ledger for a real project,
the workspace page renders it for a non-fixture id, and a clone with no credentials still renders
the fixture project.

### T7.6: Frontend and entrypoint hygiene
```yaml
requires:   []
fixture-ok: yes
size:       S · mid
owns:       web/package.json, web/package-lock.json, web/src/app.html, .gitignore,
            cmd/ajilamu/main.go
status:     not-started
```
Three live defects. Every page load logs a 404 because `web/src/app.html` declares no favicon.
No `package-lock.json` exists and `web/package.json` pins no Node engine, so the version
`AGENTS.md` names is unenforced. `findFrontendRoot` probes `web/dist`, which `.gitignore` does not
cover, so a stale untracked build can be served.

**Done when:** A page load logs no 404, `npm ci` reproduces the dependency tree, and the frontend
probe cannot serve an untracked `web/dist`.

---

## Exit Criteria

- [ ] The server runs from a clone with no credentials and serves the workspace from fixtures.
- [ ] Shutdown flushes the ledger queue without losing events.
- [ ] Large video files stream directly to storage without memory bloat.
- [ ] Users can upload optional background music tracks.
- [ ] Sample mode launches instantly with one click.
- [ ] Progress events stream complete sentences with running costs.
- [ ] Interrupted runs remain resumable without data corruption.

---

## Handoff Log

_(Fill on completion: record upload throughput metrics and SSE client reconnection behaviors.)_

### T7.0a: Static workspace build contract

- `@sveltejs/adapter-static` emits `web/build/index.html` as the client route fallback.
- The existing Go binary serves `/`, `/new`, `/config`, and `/d/fixture` as matching HTML with status 200.
- The task records a contract change on .gitignore to add the web/build/ ignore rule.

### T7.0: Server entrypoint

`go run ./cmd/ajilamu` starts with no credentials and serves fixtures. Process settings
load at boot. Credentials resolve when a feature needs them. `ENV=production` fails at
boot if ClickHouse, Google Cloud, or the frontend build is missing.

The mux mounts health, ledger readiness, `GET /api/dubs`, `POST /api/dubs/new`, and
`POST /api/config`. Unmatched `/api/` paths return 404. Video ranges return 206.

Shutdown drains HTTP for 10 seconds, then flushes the ledger for 5 seconds. A drain
timeout still reaches flush and logs the pending count. Development leaves the ledger
interface genuinely nil.

`AJILAMU_DATA_DIR` defaults to `data`, which `.gitignore` covers. `AJILAMU_FRONTEND_DIR`
names the static root. `/api/ledger/ready` pings ClickHouse. Config stays unwired
(`ConfigHandler(nil)` returns 503) until T7.4a.

The index provider returns the fixture summary plus `ListUploadSummaries` of the
upload directory. A new upload appears on `GET /api/dubs` on the next request.

Round 4 review returned APPROVE with zero residue.

### T7.1: Upload

`NewUploadHandler` streams multipart `video` and optional `music` to a persistent
storage directory that T7.0 supplies. It never buffers a file in memory. Paths in
the `201` JSON stay project-relative. Stored files measure mode 0600.

The create screen projects a fee by scaling the completed P2 fixture ledger.
The NASA clip reference is 23,414,000 nanodollars for 75.008267 seconds.
Round 2 review returned APPROVE with zero residue.

T7.0 mounts the handler at `POST /api/dubs/new`.

### T7.2: Sample mode

- `POST /api/dubs/sample` copies the NASA clip into persistent upload storage, records Malayalam, and returns a project-relative response.
- Clip discovery checks `AJILAMU_SAMPLE_CLIP` first, then searches relative testdata locations. Missing clip logs name searched paths and override variables.
- All fixture ids (`fixture`, manifest id, and wire id) show completed fixture work. Real pending sample ids render honest empty workspaces, and unknown ids render error states.
- The chrome frame dynamically updates status and spend per route.
- Fixture chrome uses the fixture readiness `review`, so it shows In review with the amber class.
- A failed index fetch says the list could not load. It does not claim the project is missing.
- Pending copy names a project, not a sample, so an upload is not called the NASA clip.
- The create screen shows the sample fee before launch and names the copy step while busy.
- Operator skipped a fourth review after those remediations.

### T7.4: Index and config routes

`IndexHandlerFrom(func() []DubSummary)` queries a provider on each GET request. It copies the
result, sorts by descending RFC 3339 creation time, and serves `DubIndex` JSON without caching.
`IndexHandler([]DubSummary)` snapshots its input at construction and wraps `IndexHandlerFrom`.
T7.0 mounts `IndexHandlerFrom` at `GET /api/dubs`. The provider returns the fixture
project and every persisted upload record.

`ConfigHandler(func(ConfigUpdate) error)` accepts only URL-encoded POST form fields named
`voice_key` and `translation_key`. It accepts at most 16 KiB, treats an omitted field as
an empty field, and returns `204 No Content` after its callback succeeds. Chunked bodies
over 16 KiB return `413`.

`ConfigUpdate` keeps both values unexported and implements `LogValue` and `String` to prevent
credential logging. `VoiceKey` and `TranslationKey` return each non-empty value with a presence
flag. The handler sends no credential in any response and uses fixed error text. It returns
`503` when no callback is installed.

T7.4a owns durable local credential storage and supplies the callback after T7.0 lands.

Round 3 returned one L. The orchestrator added `TestConfigHandlerRejectsBothEmptyFields`
and landed the task.

### T7.2a: Ledger read settings

`queryClickHouse` in `client.go` builds and sends every ClickHouse read request. It pins
`output_format_json_quote_64bit_integers` to 0 and `max_recursive_cte_evaluation_depth` to
`maxRecursiveCTEDepth`. `postClickHouse` is deleted. `TimelineAt`, `branchCost`, `commitByID`,
and `durationPriorStats` route through the helper.

`TestClickHouseReadersSurviveQuotedIntegers` drives all three readers against a stand-in that
quotes 64-bit integers unless the request pins the quoting setting. `TestOnlyClientPinsServerSettings`
fails when any non-test file other than `client.go` names either setting.

Round 1 returned two L findings, one doc claim and one test comment. The orchestrator applied
both under the L exemption and synced the `postClickHouse` claim in `PHASE-4-ledger.md`. Round 2
returned APPROVE with zero residue.

### T7.2b: Readiness probe budget

`GET /api/ledger/ready` answers JSON on every outcome. `{"status":"ready"}` is 200.
`misconfigured`, `waking`, and `unreachable` are 503. The credential check runs first, so a
missing credential names the variable and opens no connection.

`probeClickHouse` classifies with `httptrace.ConnectDone`. Waking means the response budget
expired after the TCP connection came up. Every other post-connect failure reports unreachable,
so a scheme mismatch cannot report waking forever.

`probeDialTimeout` is 2 seconds and `probeResponseBudget` is 15 seconds. The measured cold first
ping took 13 seconds, so a cold ClickHouse answers ready. The measured idle authenticated call
exceeded 25 seconds, so that case reports waking. The probe decides on the status line and leaves
the body unread.

T7.2b owns `internal/api/ready_internal_test.go` for the package variables, because
`server_test.go` is an external test package and the internal split cycles through
`internal/ledger`. The record is in `adversarial-review/t7.2b-contract-change.md`.

Round 1 returned one M and two L. Remediation fixed all three, and the orchestrator added the
post-connect refinement. Round 2 returned APPROVE with zero residue.

### T7.2c1: Ledger commit DAG read

`ListCommits(ctx, dubID)` returns one `CommitHistoryRow` per commit with its action provenance,
ordered by `version_seq` then `commit_id`. It binds `dub_id` and routes through `queryClickHouse`,
so both T7.2a pins apply. `created_at` crosses the wire as RFC 3339, converted from epoch
milliseconds in Go.

A commit may carry several action rows. A row with a prompt outranks one without, then the newest
action wins, then the greatest `event_key`. A commit with no action row keeps empty provenance.

`BranchView.CostUSD` is now `AttributedCostUSD`. No shipped writer can produce an unattributed
charge: `charges_raw` has one writer, `RecordTake`, which stamps the attempt commit and rejects
an empty one. The rename makes the field name match the ancestry sum.

Round 1 returned one L, applied by the orchestrator under the L exemption. Round 2 returned
APPROVE with zero residue.

### T7.2c: Ledger read routes

`GET /api/dubs/{id}/history`, `/timeline`, and `/branches` answer JSON with `Cache-Control:
no-store`. A blank parameter answers 400 naming it, a nil reader answers 503, and a read failure
answers 500 with a fixed sentence while the detail goes to the log. `internal/api` declares a
`HistoryReader` interface in api types, and `cmd/ajilamu/main.go` adapts `*ledger.Client` to it
inside the credentials branch, so a typed nil never reaches the interface.

The History tab fetches by project id and renders the fetched commits when the response carries
at least one. It keeps the fixture commits when the fetch fails or returns none, so a clone with
no credentials still renders the tab.

The review proved the routes end to end against a stand-in ClickHouse. It also found a
pre-existing defect in `web/src/routes/d/[id]/+page.svelte` at HEAD, recorded as T7.2d. After
that fix the tab renders stand-in commits in the running app, and it keeps 7 fixture commits when
the route answers 503.

Round 1 returned APPROVE with zero findings.

### T7.2d: Workspace page effect loop

The fixture branch of the page `$effect` read `dub` after writing it, so `dub` became an effect
dependency and Svelte aborted with `effect_update_depth_exceeded`. The fix loads into a local and
decides the panel state from that local. No behaviour changed.

The review reproduced the defect and the fix against the built frontend served by the real
binary. The reverted build threw `effect_update_depth_exceeded` and both tab switches stayed on
`rail-lines`. The fixed build switches Details and History, mounts 7 DAG nodes and 6 edges, and
logs no error. Round 1 returned APPROVE with zero findings.

### T7.3c: Cancellable media and assembler

`media.Command(ctx, name, args...)` builds `exec.CommandContext` and sets `WaitDelay` to 5
seconds. `Run`, `Demux`, `Atempo`, `Duration`, and `AudioFormat` take a context and route through
it. A cancelled or expired context yields an error that satisfies `errors.Is` for its sentinel.

`BuildBed`, `PrepareTake`, `Place`, `Overlay`, `Duck`, `Export`, and `Peaks` take a context too.
The change reaches `internal/fit`, so `Measure`, `PlanStretch`, `PlanStretchWithLimits`,
`Stretch`, `StretchWithLimits`, and `Pipeline.Run` carry it down to media. No non-test file
passes `context.Background()`.

Two implementers collided in this working tree. One repaired a corrupted call at
`internal/fit/stretch_test.go:424` to compile. The review confirmed the repair matches the
function contract and that no assertion changed.

Round 1 returned APPROVE with zero findings.

### T7.3: Run engine, server-sent events, and persistence

`POST /api/dubs/{id}/run?language=L` starts a run and answers 202 before it finishes. `GET
/api/dubs/{id}/events` streams `ProgressEvent` frames with a per-run id and the cumulative cost,
and ends with one terminal event. `POST /api/dubs/{id}/run/cancel` stops it. A duplicate run and a
cancel with no run answer 409. A missing language answers 400. A project with no source video
answers 404, and so does any id that is not a plain identifier.

`internal/api` declares `PipelineRunner` and `RunRecorder` in api types, and `cmd/ajilamu/main.go`
adapts the fit loop and the ledger client to them, so `internal/api` still imports no pipeline
package. `Shutdown` cancels active runs and waits a bounded time before the ledger flush.

`Persist` flushes the queue, reads the dub head, appends one commit, records its action, one take
with charges per rendered take, and one snapshot per segment, then flushes again. A whole-pass
charge rides the first rendered take, because `charges_raw` keys every row by take.

Round 1 returned two H and one M. The assembly and export frames carried zero cost, the run route
globbed a path built from the raw dub id, and four history adapter tests were deleted. Remediation
fixed all three. Round 2 returned APPROVE with zero residue.

### T7.3b: Live progress in the workspace

`web/src/lib/progress.ts` opens the run's event stream, parses `id:` and `data:` frames, ignores
frames at or below the last id, and keeps the last cumulative cost across a reconnect. A first
contact that fails never retries. A stream that opened and dropped reconnects at 500 ms doubling
to 8 seconds, at most six times, and a terminal event ends the watch.

The workspace shows a start control that posts `/run` for the active language, renders the active
step and sentence, and shows the running cost. A 409 attaches to the existing run. A 503, 404,
other status, or network error shows a sentence and opens no stream.

The review drove the built frontend through the real server, severed the stream, and measured the
cost rise from $0.05 to $0.06 rather than a reset. Round 1 returned APPROVE with zero findings.
