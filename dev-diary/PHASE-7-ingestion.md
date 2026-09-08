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
owns:       internal/api/server.go, internal/api/server_test.go
status:     not-started
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

### T7.2c: Ledger read routes
```yaml
requires:   T7.0, T7.2a, T4.5, T5.5
fixture-ok: yes
size:       M · frontier
owns:       internal/api/history.go, internal/api/history_test.go,
            internal/api/server.go, web/src/lib/tabs/HistoryTab.svelte
status:     not-started
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

**This task settles the branch cost question.** The P4 Handoff Log records that
`BranchView.CostUSD` sums charges over an ancestry of commit ids, so it drops every whole pass
charge carrying the default empty `commit_id`. A measured 0.75 whole pass charge left a 0.7508
dub reporting 0.0003 and 0.0006 per branch. The number matches the stated contract and the field
name does not. As the first caller, this task either attributes a whole pass charge to a commit
at write time or renames the field to say it counts attributed charges only. Read that finding
before choosing. Check whether an unattributed charge can reach `charges_raw` at all, because
`internal/gemini/segment.go` and `internal/tts/chirp.go` build a `cost.Charge` with no
`CommitID` while `internal/ledger/takes.go` rejects an empty one.

**Done when:** The History tab renders a real commit DAG from ClickHouse rather than fixture
JSON. Time travel to a commit returns that commit's timeline. Branch comparison returns a cost
that a reviewer can tie to `charges` by hand, and the field's name matches what it counts. A
clone with no credentials still serves the tab from fixtures.

---

### T7.3: Run orchestration and progress ★
```yaml
requires:   T1.4, T2.7, T3.4, T7.2a
fixture-ok: yes
size:       L · frontier
owns:       internal/api/run.go, internal/api/events.go
status:     not-started
```
Demux audio, run the fit loop, assemble output audio, and export video asynchronously. Stream server-sent events to the client.

Send natural sentences rather than technical status codes. For example: "Rendering line 3 in Malayalam" or "This take overruns by 1.2 seconds, trying a shorter line."

Include cumulative project cost on every event to keep the UI budget meter synchronized.

Preserve discrete WAV takes on disk during execution. If an external failure interrupts processing, the project resumes without re-rendering completed takes.

**Done when:** Progress streaming functions smoothly across both fixture simulations and live end-to-end runs.

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
owns:       internal/api/server.go, internal/config/settings.go, internal/config/settings_test.go
status:     not-started
```
Connect T7.4's injected write-only configuration callback to durable server-side storage. Store
secret values outside the repository and image, with permissions that prevent other local users
from reading them. The read API may report whether a value exists, but must never serialize,
log, or return the secret itself. Reuse T7.0's application data-directory contract instead of
inventing a second location.

**Done when:** A submitted credential survives a process restart, cannot be read through any
HTTP response, is absent from logs, and has restrictive on-disk permissions.

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
