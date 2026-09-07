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

- [ ] Large video files stream directly to storage without memory bloat.
- [ ] Users can upload optional background music tracks.
- [ ] Sample mode launches instantly with one click.
- [ ] Progress events stream complete sentences with running costs.
- [ ] Interrupted runs remain resumable without data corruption.

---

## Handoff Log

_(Fill on completion: record upload throughput metrics and SSE client reconnection behaviors.)_
