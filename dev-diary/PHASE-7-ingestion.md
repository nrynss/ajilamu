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
owns:       cmd/ajilamu/main.go, internal/api/server.go, internal/api/server_test.go,
            internal/api/static.go, internal/config/config.go, internal/config/config_test.go
status:     not-started
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

### T7.1: Upload ★
```yaml
requires:   T1.4
fixture-ok: yes
size:       M · mid
owns:       internal/api/upload.go, web/src/routes/new/+page.svelte
status:     not-started
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
owns:       internal/api/sample.go
status:     not-started
```
Provide a one-click button to launch sample projects using the committed NASA 75-second video. Evaluators can test the system without uploading files.

**Done when:** Clicking the sample button creates a working project instantly from committed test data.

---

### T7.3: Run orchestration and progress ★
```yaml
requires:   T1.4, T2.7, T3.4
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
owns:       internal/api/index.go, web/src/routes/config/+page.svelte
status:     not-started
```
Implement `/` to list projects ordered by creation time. Implement `/config` to manage API keys and application settings.

Ensure credential inputs are write-only. Never log, echo, or expose API secrets back to client browsers.

**Done when:** The index displays active projects, and saved API keys remain hidden from client-side inspection.

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
