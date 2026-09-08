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
owns:       internal/ledger/takes.go, internal/ledger/takes_test.go
status:     done
```
Store one record per take attempt: project id, language, segment index, attempt number, voice, target slot, measured duration, signed delta, repair type, and audio path.

Store signed deltas rather than one-sided percentages.

Record charges on their respective attempts. Do not carry accumulated running totals into individual take records.

**Done when:** Replaying test data writes 10 take rows, charges match actual call costs, and segment 8 records a negative delta.

---

### T4.2a: Waveform peak take mapping
```yaml
requires:   T4.2
fixture-ok: no
size:       S · mid
owns:       internal/ledger/takes.go, internal/ledger/takes_test.go
status:     done
```
Extend the take recorder so each take persists its `[]uint8` waveform peaks to `takes_raw`.

The recorder and its capture regression test own this boundary. T4.6 owns peak validation and
delivery mapping, so it waits for T4.2a instead of editing the take writer across its seam.

**Done when:** A captured take insert carries the exact unsigned peak vector, and an invalid peak
value fails before transport.

---

### T4.3: Commit DAG ★
```yaml
requires:   T4.1, T1.1
fixture-ok: no
size:       M · mid
owns:       internal/ledger/commits.go, internal/ledger/commits_test.go
status:     done
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
owns:       internal/ledger/actions.go, internal/ledger/actions_test.go
status:     done
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
owns:       internal/ledger/history.go, internal/ledger/history_test.go
status:     done
```
Reconstruct timeline state at any historical commit using analytical SQL queries.

Support branching a language track to evaluate alternative translation phrasing. Compare duration metrics and costs between branches.

**Done when:** Reconstructing state at commit N yields identical timelines whether replayed or queried directly.

---

### T4.6: Waveform peak storage
```yaml
requires:   T4.2a, T3.5
fixture-ok: no
size:       XS · light
owns:       internal/ledger/peaks.go, internal/ledger/peaks_test.go
status:     done
```
Store `Array(UInt8)` waveform peaks on take rows. Deliver peaks in API payloads so the timeline renders waveforms without reading audio files from disk.

**Done when:** The API delivers peak arrays with take records, enabling the web frontend to render waveforms instantly.

---

### T4.7: Learned priors
```yaml
requires:   T4.2
fixture-ok: no
size:       M · mid
owns:       internal/ledger/priors.go, internal/ledger/priors_test.go
status:     done
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

### T4.2 implementation handoff

`internal/ledger/takes.go` exposes `Client.RecordTake`. It accepts one `TakeAttempt`, derives
the take row from the frozen `types.Segment` and `types.Take` contracts, and writes only to
`takes_raw` and `charges_raw` through the durable queue.

The recorder rejects mismatched segments, unsigned deltas, non-millisecond durations, unknown
repairs, and invalid charge shapes before it queues a row. Gemini prompt and candidate usage
becomes distinct token rows. Synthesis remains one character row. Every row repeats the attempt
identity, and the recorder never accepts or writes a running cost total.

`internal/ledger/takes_test.go` preserves the HTTP capture regression pin for ten take rows,
per-attempt charge rows, and segment 8's negative delta. It also proves invalid deltas do not
reach the transport.

### T4.2a implementation handoff

`TakeAttempt.Peaks` accepts optional unsigned waveform samples. Empty vectors and vectors with
64 through 128 values pass validation. Other lengths fail before the ledger queue receives a row.

`takes_raw` JSONEachRow inserts now name `peaks` and serialize it as numeric values. Go's default
`[]uint8` encoding uses base64, so `takePeaks` deliberately emits a JSON number array instead.

The capture test pins one exact 64-value vector, an empty vector, the 128-value upper bound, and
the rejected 63-value vector. It confirms invalid vectors make no transport request.

### T4.6 implementation handoff

`internal/ledger/peaks.go` exposes `ApplyTakePeaks`. It validates the stored empty or 64 through
128 `UInt8` samples, copies populated vectors into `api.Take.Peaks`, and leaves an empty vector
absent from the JSON payload. It never reads an audio file.

T4.6 owns no test path. `go test ./internal/ledger` and `go vet ./internal/ledger` compile the
mapping. A future owned regression path should pin copied values, the 64 and 128 boundaries, the
rejected 63 and 129 lengths, and clearing a stale payload for an empty vector.

### T4.3 implementation handoff

`internal/ledger/commits.go` exposes `Client.AppendCommit`. It writes one immutable row to
`commits_raw` through the durable queue. It never issues an update or delete query.

The writer lets ClickHouse stamp `created_at` and `ingested_at`. It validates root and child
shape, rejects self-parenting nodes, and supports a branch whose parent is an earlier commit.

`internal/ledger/commits_test.go` commits HTTP artifact coverage for roots, linear chains, forks,
server-owned timestamps, replay, parent validity, and immutable commit identities. The identity
tests cover visible duplicates, pending journal entries, and conflicting concurrent appends.

### T4.7 implementation handoff

`internal/ledger/priors.go` exposes `Client.DurationPrior`. It reads the deduplicated
`take_rates` view and returns the population and creator sample counts beside the learned
characters-per-second estimate.

New creators receive the population rate. Creator evidence gains the weight `n/(n+10)` after
`n` personal rows, so a small library cannot overturn the shared history.

`internal/ledger/priors_test.go` pins bound query parameters, sample counts, cold-start fallback,
creator weighting, malformed responses, and upstream errors through an HTTP capture.

### T4.4 implementation handoff

`internal/ledger/actions.go` exposes `Client.RecordAction`. It writes one append-only row to
`actions_raw` through the durable queue. It never inserts into the `actions` view.

The writer uses the frozen P1 wire vocabulary from `internal/api`. Stored stretch type is
`atempo_stretched`. The PHASE-4 name `stretched` fails validation and never reaches the queue.

`created_at`, `ingested_at`, and `event_key` stay off the JSON body. ClickHouse stamps those
columns. `action_id` is a delivery id. The writer generates one when the caller leaves it empty.

Command-bar prompts store byte for byte, including spaces, quotes, and punctuation. An empty
command-bar prompt fails before enqueue. Language is required even for dub-wide rows.

`before_value` and `after_value` are string fields that hold JSON text. They explain an edit.
They are not nested objects, so encoding/json cannot flatten them into the row.
A non-empty value that is not valid JSON fails before enqueue. Empty values stay allowed.

`internal/ledger/actions_test.go` pins every recognized action type against the HTTP artifact.
It also pins omitted server columns, verbatim command-bar prompts, rejected aliases, JSON text
in before_value and after_value, and 503 replay of a byte-identical body.

### T4.5 implementation handoff

`internal/ledger/history.go` writes full snapshots to `timeline_state_raw` and reads state
through `timeline_at_commit`. `RecordSegmentState` validates a complete row and enqueues it.
It never fills a field from an earlier snapshot.

`TimelineAt` binds `dub_id`, `language`, and `commit_id` and queries the view. It does not
walk `parent_commit_id` in Go. A `version_seq` range would pull sibling commits once two
branches reuse a number. A `branch` filter would drop the history a fork inherited.

`ReplayAt` is a pure function over an in-memory DAG and snapshot log. It uses the same
ancestry plus newest `version_seq` per segment rule as the view. Tests hard-code the view's
JSONEachRow payload for root, child, and fork. They never call `ReplayAt` inside the mock.
`TimelineAt` on that stand-in must equal `ReplayAt` field for field.

`CompareBranches` reconstructs both heads, sums slot lengths, counts takes, and sums
`charges.cost_usd` over each ancestry. It does not filter history by branch label. The
label is display only.

A live ClickHouse probe of the real view is not in this task's owns list. HTTP capture pins
the query shape. A later b-track can hit a real server.

The package now has a snapshot writer, a view query, a replay pin, branch compare, and 503
replay. The view names the ranked version `state_version_seq`, not `version_seq`. Decode
that name or replay and query disagree on version. The next agent must not treat a snapshot
as a delta. A nudge that omits `take_id` erases the take at that commit.
