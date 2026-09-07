# P4: Ledger (Track C)

```yaml
id:       P4
size:     L
branch:   phase/p4-ledger
requires: [P1]
blocks:   P6 (history), P8
parallel: high
runs-parallel-with: P2, P3, P5, P6, P7
```

**Goal:** Implement the Git for Video Production ledger in ClickHouse. Support take records, an immutable commit DAG, and resilient write handling.

**Zero AI dependencies:** This track requires only ClickHouse connectivity. It operates independently of Gemini, Chirp, and ffmpeg.

---

### T4.1: Client and failure mode ★
```yaml
requires:   T1.3
fixture-ok: no
size:       L · frontier
owns:       internal/ledger/client.go, internal/ledger/queue.go
status:     done
```
Initialize the ClickHouse database client and configure write resilience.

The test script swallowed write exceptions. A version control system cannot drop commit events silently.

Implement a local queue that flushes batches and reconciles upon reconnect. This protects creator edits if network connectivity drops during processing.

Batch all insert queries to maximize ClickHouse ingestion performance.

**Done when:** Disconnecting the network during execution drops zero events, and all queued commits reconcile upon reconnection.

---

### T4.2: Take rows ★
```yaml
requires:   T4.1, T1.1, T1.2
fixture-ok: no
size:       S · mid
owns:       internal/ledger/takes.go
status:     not-started
```
Store one record per take attempt: project id, language, segment index, attempt number, voice, target slot, measured duration, signed delta, repair type, and audio path.

Store signed deltas rather than one-sided percentages.

Record charges on their respective attempts. Do not carry accumulated running totals into individual take records.

**Done when:** Replaying test data writes 10 take rows, charges match actual call costs, and segment 8 records a negative delta.

---

### T4.3: Commit DAG ★
```yaml
requires:   T4.1, T1.1
fixture-ok: no
size:       M · mid
owns:       internal/ledger/commits.go
status:     not-started
```
Store immutable commits containing `commit_id`, `parent_commit_id`, `dub_id`, `version_seq`, and database timestamps.

Every pipeline action and manual timeline edit produces an append-only commit. Never mutate existing rows in place.

**Done when:** A dub run creates a linear commit chain, and branching from an earlier commit creates a valid fork.

---

### T4.4: Action provenance
```yaml
requires:   T4.3
fixture-ok: no
size:       S · light
owns:       internal/ledger/actions.go
status:     not-started
```
Record action metadata for each commit: action type, author (`agent`, `command_bar`, `manual_ui`), prompt text, and before-and-after values.

Support actions: `segment_created`, `boundary_nudged`, `speaker_reassigned`, `text_corrected`, `take_rendered`, `stretched`, `line_rewritten`, and `user_command`.

**Done when:** Every user edit type maps to a recognized action, and command-bar edits preserve input instructions verbatim.

---

### T4.5: Time travel and branching
```yaml
requires:   T4.3, T4.4
fixture-ok: no
size:       L · frontier
owns:       internal/ledger/history.go
status:     not-started
```
Reconstruct timeline state at any historical commit using analytical SQL queries.

Support branching a language track to evaluate alternative translation phrasing. Compare duration metrics and costs between branches.

**Done when:** Reconstructing state at commit N yields identical timelines whether replayed or queried directly.

---

### T4.6: Waveform peak storage
```yaml
requires:   T4.2, T3.5
fixture-ok: no
size:       XS · light
owns:       internal/ledger/peaks.go
status:     not-started
```
Store `Array(UInt8)` waveform peaks on take rows. Deliver peaks in API payloads so the timeline renders waveforms without reading audio files from disk.

**Done when:** The API delivers peak arrays with take records, enabling the web frontend to render waveforms instantly.

---

### T4.7: Learned priors
```yaml
requires:   T4.2
fixture-ok: no
size:       M · mid
owns:       internal/ledger/priors.go
status:     not-started
```
Query historical take metrics to predict speaker duration tendencies across languages.

Every learned prior query must report its sample size.

Provide population aggregates across all users to address cold starts. Shift weighting toward a creator's personal history as their library grows.

**Done when:** Queries return learned speech rates with sample counts, falling back to population baselines for new creators.

---

## Exit Criteria

- [ ] Implemented resilient write queues verified under network drops.
- [ ] Sum of logged charges matches total API expenses without double counting.
- [ ] Database stores signed duration deltas.
- [ ] Commit DAG supports branching and time travel queries.
- [ ] All tables created via `sql/schema.sql`.

---

## Handoff Log

### T4.1 implementation handoff

`internal/ledger` now journals each JSONEachRow insert to a local directory before it sends the
request. The queue batches adjacent rows with one insert statement, removes files only after a
successful response, and replays retained files in insertion order after reconnecting.

T1.3 natural event keys make a retry after an ambiguous network failure safe. A failed flush
returns `ErrPending`, so callers cannot mistake locally retained events for delivered events.

T4.1 owns no test path. The client exposes its transport and endpoint as options so its assigned
reviewer can run network-drop checks with a local HTTP server without changing production code.
