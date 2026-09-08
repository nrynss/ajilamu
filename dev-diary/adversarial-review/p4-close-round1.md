# P4 close: independent verification, round 1

**Scope:** Phase 4 exit criteria for T4.1 through T4.7 as they stand in the tree.
**Method:** Independent measurement of every claim. No reliance on per-task review files.
**Verdict:** REMEDIATE. Findings: 0 C, 1 H, 0 M, 0 L.

I own only this review file. The ledger shapes exist. I can validate the phase alone.
I did not treat task review files as evidence. I re-measured every artifact.

| C | H | M | L |
|---|---|---|---|
| 0 | 1 | 0 | 0 |

## Findings

| Where | What | Pin | Mutation |
|---|---|---|---|
| `internal/ledger/takes.go` `RecordTake` | A 503 on the take insert returns `ErrPending` after journaling only the take. The charge rows never enter the durable queue. Recovered HTTP traffic delivers the take and no charges for that commit. | Drop journal had one file. Decoded body was `takes_raw` `take-8-1` with `delta_ms` -2910. Pending was 1. After reconnect, captures 5 to 7 were `takes_raw`, `actions_raw`, and `timeline_state_raw`. Zero `charges_raw` bodies contained `commit-drop`. | Journal the take and its charges before the first Flush. Reverting that leaves one take file after 503 and omits charge rows from the recovered capture. |

## Task status and owns

Every T4 task reads `status: done`. Every owns path exists on disk.

| Task | Owns | Present |
|---|---|---|
| T4.1 | `internal/ledger/client.go`, `queue.go` | yes |
| T4.2 | `internal/ledger/takes.go`, `takes_test.go` | yes |
| T4.2a | `internal/ledger/takes.go`, `takes_test.go` | yes |
| T4.3 | `internal/ledger/commits.go`, `commits_test.go` | yes |
| T4.4 | `internal/ledger/actions.go`, `actions_test.go` | yes |
| T4.5 | `internal/ledger/history.go`, `history_test.go` | yes |
| T4.6 | `internal/ledger/peaks.go`, `peaks_test.go` | yes |
| T4.7 | `internal/ledger/priors.go`, `priors_test.go` | yes |

I did not tick the phase exit boxes.

## Prior-round residue

This is the first P4 close round. I re-measured T4.1 through T4.7, including T4.4 and T4.5.
I did not quote those task reviews as proof.

**T4.1: residue in the take recorder, not the queue.** The queue journals before send. A new client on the same directory delivered a crash-queued action. Capture 8 was 503. Capture 9 was 200. The bodies were byte identical, including `nudge line 2 left "please"`.
`RecordTake` still loses charges under the same drop. That is finding 1.

**T4.2: zero residue on happy-path rows.** Ten fixture takes reached `takes_raw`. Thirty charge rows reached `charges_raw`. No insert body carried `cost_usd`. Segment 8 stored `slot_ms` 7110, `measured_ms` 4200, `delta_ms` -2910.

**T4.2a: zero residue.** Segment 1 peaks were a 64-value JSON number array. Later fixture takes stored `[]`. An invalid signed delta made zero extra HTTP requests.

**T4.3: zero residue.** Captured commits were `c1` root on main at version 1, `c2` child of `c1` on main at version 2, and `c3` fork of `c1` on alt at version 2.
ClickHouse `commits` returned the same parent edges.

**T4.4: zero residue.** Eight insert bodies stored `segment_created`, `boundary_nudged`, `speaker_reassigned`, `text_corrected`, `take_rendered`, `atempo_stretched`, `line_rewritten`, and `user_command`.
The command-bar prompt bytes were `nudge line 2 left "please"`.

**T4.5: zero residue against the real view.** `ReplayAt` and `timeline_at_commit` matched field for field on root, child, and fork. Child segment 1 started at 1100 ms. Fork segment 2 text was `rewritten` with take `t2-alt`. Fork segment 1 stayed at 1000 ms.

**T4.6: zero residue.** `ApplyTakePeaks` wrote 64 numeric peaks onto `api.Take` JSON. An empty vector omitted the `peaks` key. The take file path did not exist, so no audio read occurred.

**T4.7: zero residue on the query shape.** The captured statement read `take_rates` with bound `owner_id`, `language`, and `speaker`. A 50/10 sample reply produced weight 0.5 and rate 3.

## Independent measurements

A throwaway driver imported the production ledger package from a module copy under `/tmp`.
A local `httptest` server captured every request. Python decoded those bodies. It did not trust driver logs.

ClickHouse local 26.8.2.7 loaded `sql/schema.sql` into an empty database at `/tmp/p4-close-r1/chdata`.
It ingested the captured JSONEachRow files. Queries used the views, not program output.
Go reports 1.27.1.

### Resilient queue under drops

| Step | Artifact |
|---|---|
| First `RecordTake` during 503 | Error wraps `ErrPending`. Journal pending count is 1. |
| Journal file `00000000000000000001.json` | Query is `INSERT INTO takes_raw`. Body is one take row. No charge files. |
| Reconnect with a new client | Capture 5 delivers that take. `delta_ms` is -2910. `commit_id` is `commit-drop`. |
| Charge search | Zero captured `charges_raw` bodies contain `commit-drop`. |
| Crash-queued command-bar action | 503 body equals later 200 body. |

`AppendCommit` during the same outage failed on the identity SELECT. It never journaled. The caller received a lookup error, not `ErrPending`. That is not silent drop. It is not this finding.

### Charges without double counting

Python summed `units * unit_price_usd` from decoded charge JSON with `Decimal`.

| Source | Cost USD | Rows |
|---|---|---|
| Decoded insert bodies | 0.30159225 | 30 |
| Fixture formula from the same inputs | 0.30159225 | 30 |
| `SELECT sum(cost_usd) FROM charges` after a duplicate insert | 0.30159225 | 30 |
| `SELECT sum(cost_usd) FROM charges_raw` after the same duplicate | 0.6031845 | 60 |

The view applies FINAL. The raw table counted the retry twice. Insert bodies omitted `cost_usd`. Take bodies omitted `cost_usd`.
Writers named `_raw` tables only: `takes_raw`, `charges_raw`, `commits_raw`, `actions_raw`, `timeline_state_raw`.

An insert into the `takes` view failed with `Method write is not supported by storage View`.

### Signed duration deltas

Independent expected values came from `testdata/segments.json` and `testdata/expected/metrics.json`.
Every captured take matched `measured_ms - slot_ms`. Six deltas were negative.

| Segment | Slot ms | Measured ms | Delta ms |
|---|---:|---:|---:|
| 1 | 1820 | 1680 | -140 |
| 8 | 7110 | 4200 | -2910 |

`SELECT delta_ms FROM takes WHERE segment_index = 8 AND attempt = 1` returned -2910.
`countIf(delta_ms = measured_ms - slot_ms)` returned 10 of 10.

A row with `delta_ms` 2910 against slot 7110 and measured 4200 failed constraint `delta_is_signed`.

### Commit DAG, branching, and time travel

Captured insert parents were `c1` empty, `c2` to `c1`, `c3` to `c1`.
ClickHouse `commits` returned the same fork.

`SELECT * FROM timeline_at_commit(dub_id = 'd1', language = 'ml', commit_id = ...)` on the real view matched `ReplayAt` on every field below.

| Head | Segment | Start ms | Text | Take | `state_version_seq` |
|---|---:|---:|---|---|---:|
| c1 | 1 | 1000 | hello | t1 | 1 |
| c2 | 1 | 1100 | hello | t1 | 2 |
| c3 | 1 | 1000 | hello | t1 | 1 |
| c3 | 2 | 3000 | rewritten | t2-alt | 2 |

The child's nudge did not leak onto the fork. The fork rewrite did not leak onto the child.
`CompareBranches` bound `param_commit_id` to `c2` and `c3`. The cost statement summed `charges` over recursive ancestry. It did not filter `branch`.

### Schema on disk and in the database

`sql/schema.sql` declares five `_raw` tables and seven views. ClickHouse created all twelve.

| Name | Engine |
|---|---|
| `takes_raw` | ReplacingMergeTree |
| `commits_raw` | ReplacingMergeTree |
| `timeline_state_raw` | ReplacingMergeTree |
| `actions_raw` | ReplacingMergeTree |
| `charges_raw` | ReplacingMergeTree |
| `takes`, `commits`, `timeline_state`, `actions`, `charges`, `take_rates`, `timeline_at_commit` | View |

No other package issues `INSERT` into these tables. `internal/ledger` is the single writer.

### Waveform peaks on the API take

Decoded `api.Take` JSON after `ApplyTakePeaks` stored 64 numbers. Empty input omitted `peaks`.

### Learned priors

Captured query:

`FROM take_rates WHERE language = {language:String} AND speaker = {speaker:String}`

Bound parameters were `ten`, `ml-IN`, and `Suni Williams`. Sample counts were 50 and 10. Weight was 0.5. Rate was 3.

`take_rates` over the ingested fixture returned 10 rows.

## Exit criteria

| Criterion | Independent result |
|---|---|
| Resilient write queues verified under network drops | Queue replays journaled rows after reconnect. `RecordTake` does not journal charges when the take insert returns 503. Finding 1. |
| Sum of logged charges matches total API expenses without double counting | Happy-path Decimal sum matches the charges view at 0.30159225. FINAL collapses a duplicate insert from 60 raw rows to 30. A dropped take still loses its charges. |
| Database stores signed duration deltas | Captured JSON and ClickHouse both store segment 8 as -2910. The unsigned 2910 insert failed `delta_is_signed`. |
| Commit DAG supports branching and time travel queries | Fork insert and `timeline_at_commit` match `ReplayAt` on root, child, and fork. |
| All tables created via `sql/schema.sql` | Local 26.8.2.7 created the five `_raw` tables and seven views from that file. |

## audit_docs.py

Command: `python3 tools/audit_docs.py`

Exit code: 0

Printed findings:

```
pending: google.golang.org/adk/v2 (T6.6 lands the pin)

No drift. Docs and repository agree.
```

Pending ADK for T6.6 is expected. The audit reported no other drift.

## Note, not a finding

PHASE-4 still lists action type `stretched`. The writer stores `atempo_stretched` from the frozen P1 wire. A `stretched` insert never reached HTTP.
That task line is coordination copy. It does not change captured behaviour.

No unowned ClickHouse writer appeared. T7.1 still owns `cmd/ajilamu`. That missing process is documented and pending.

Temporary review artifacts under `/tmp/p4-close-r1` were removed after measurement. No production code changed during review.
