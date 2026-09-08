# P4 close: independent verification, round 2

**Scope:** Phase 4 exit criteria for T4.1 through T4.7 as they stand in the tree.
**Method:** Independent measurement of every claim. No reliance on per-task review files.
**Verdict:** APPROVE. Findings: 0 C, 0 H, 0 M, 0 L.

I own only this review file. The ledger shapes exist. I can validate the phase alone.
I did not treat task review files as evidence. I re-measured every artifact.

| C | H | M | L |
|---|---|---|---|
| 0 | 0 | 0 | 0 |

## Findings

| Where | What | Pin | Mutation |
|---|---|---|---|
| None | No findings at any severity. | Independent artifact measurements below. | Reverting the pre-Flush charge journal would leave one take file after 503 and omit `charges_raw` from recovered HTTP. |

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

**Finding 1: zero residue.** A 503 on the take insert now leaves take plus charges pending.
Recovered HTTP delivers `charges_raw` for that commit.

I copied the production module to `/tmp/p4-close-r2/ajilamu`. A throwaway driver called `RecordTake`.
A local `httptest` returned 503 until I flipped it. Python decoded the files. It did not trust driver logs.

After the first failed flush, `/tmp/p4-close-r2/artifacts/journal-after-503` held four files.

| File | Table | Identity | Extra |
|---|---|---|---|
| `00000000000000000001.json` | `takes_raw` | `take-8-1` `commit-drop` | `delta_ms` -2910, `slot_ms` 7110, `measured_ms` 4200 |
| `00000000000000000002.json` | `charges_raw` | `take-8-1` `commit-drop` | `unit` `prompt_tokens` |
| `00000000000000000003.json` | `charges_raw` | `take-8-1` `commit-drop` | `unit` `candidate_tokens` |
| `00000000000000000004.json` | `charges_raw` | `take-8-1` `commit-drop` | `unit` `characters` |

Pending was 4. That is one take plus three charges. It is not 1.

The first HTTP capture returned 503. Its query was `INSERT INTO takes_raw`. Its body was the take row only.
Charge rows were already on disk. Flush stops at the first failed batch.

After reconnect the endpoint returned 200.

| Capture | Status | Table | Lines | `commit_id` |
|---|---:|---|---:|---|
| `0001` | 503 | `takes_raw` | 1 | `commit-drop` |
| `0002` | 200 | `takes_raw` | 1 | `commit-drop` |
| `0003` | 200 | `charges_raw` | 3 | `commit-drop` on every line |

Capture `0003` units were `prompt_tokens`, `candidate_tokens`, and `characters`.
Every recovered charge row stored `take_id` `take-8-1`.

ClickHouse local 26.8.2.7 ingested those recovered bodies.

`SELECT take_id, unit, units, unit_price_usd, cost_usd FROM charges WHERE commit_id = 'commit-drop'`
returned three rows for `take-8-1`. Decimal costs were 0.0000162, 0.0000168, and 0.03024.

Finding 1 does not reopen. I found no additional phase defect from the remediation.

## Independent measurements

The driver lived outside the repository. Python decoded journal files and HTTP bodies.
ClickHouse loaded `sql/schema.sql` into an empty database at `/tmp/p4-close-r2/chdata`.
Queries used the views. Go reports 1.27.1.

`go test ./internal/ledger -count=1` passed. `go test -race ./internal/ledger -count=1` passed.
`go vet ./internal/ledger` passed. `gofmt -d` was empty. `git diff --check` passed.
Those checks are hygiene. They are not spec proof.

### Resilient queue under drops

| Step | Artifact |
|---|---|
| First `RecordTake` during 503 | Error wraps `ErrPending`. Journal pending count is 4. |
| Journal file 1 | Query is `INSERT INTO takes_raw`. Body is one take row with `delta_ms` -2910. |
| Journal files 2 to 4 | Query is `INSERT INTO charges_raw`. Bodies are prompt, candidate, and character rows. |
| Reconnect with a new client | Capture `0002` delivers that take. Capture `0003` delivers three `charges_raw` lines. |
| Charge search | Every recovered `charges_raw` line contains `commit-drop`. |
| Crash-queued command-bar action | 503 body equals later 200 body. Prompt bytes were `nudge line 2 left "please"`. |

### Charges without double counting

Python summed `units * unit_price_usd` from decoded happy-path charge JSON with `Decimal`.

| Source | Cost USD | Rows |
|---|---|---|
| Decoded insert bodies | 0.301592250 | 30 |
| Fixture formula from the same inputs | 0.301592250 | 30 |
| `SELECT sum(cost_usd) FROM charges` after a duplicate insert | 0.30159225 | 30 |
| `SELECT sum(cost_usd) FROM charges_raw` after the same duplicate | 0.6031845 | 60 |

The view applies FINAL. The raw table counted the retry twice. Insert bodies omitted `cost_usd`.
Take bodies omitted `cost_usd`.
Writers named `_raw` tables only: `takes_raw`, `charges_raw`, `commits_raw`, `actions_raw`, `timeline_state_raw`.

An insert into the `takes` view failed with `Method write is not supported by storage View`.

The dropped take no longer loses its charges. The recovered `commit-drop` view sum is 0.030273.

### Signed duration deltas

Independent expected values came from `testdata/segments.json` and `testdata/expected/metrics.json`.
Every captured take matched `measured_ms - slot_ms`. Six attempt-one deltas were negative.

| Segment | Slot ms | Measured ms | Delta ms |
|---|---:|---:|---:|
| 1 | 1820 | 1680 | -140 |
| 8 | 7110 | 4200 | -2910 |

`SELECT delta_ms FROM takes WHERE segment_index = 8 AND attempt = 1 AND commit_id = 'commit-fixture'`
returned -2910.
`countIf(delta_ms = measured_ms - slot_ms)` returned 10 of 10.

A row with `delta_ms` 2910 against slot 7110 and measured 4200 failed constraint `delta_is_signed`.

### Commit DAG, branching, and time travel

Captured insert parents were `c1` empty, `c2` to `c1`, `c3` to `c1`.
ClickHouse `commits` returned the same fork (`c2` main version 2, `c3` alt version 2).

Python replayed ancestry plus newest `version_seq` per segment from the snapshot log.
`SELECT * FROM timeline_at_commit(dub_id = 'd1', language = 'ml', commit_id = ...)` on the real view
matched that replay on every field below.

| Head | Segment | Start ms | Text | Take | `state_version_seq` |
|---|---:|---:|---|---|---:|
| c1 | 1 | 1000 | hello | t1 | 1 |
| c2 | 1 | 1100 | hello | t1 | 2 |
| c3 | 1 | 1000 | hello | t1 | 1 |
| c3 | 2 | 3000 | rewritten | t2-alt | 2 |

The child's nudge did not leak onto the fork. The fork rewrite did not leak onto the child.
Ancestry for `c2` was `c2, c1` on branch `main`. Ancestry for `c3` was `c3, c1` on branch `alt`.
The cost statement sums `charges` over recursive ancestry. It does not filter `branch`.

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

Decoded `api.Take` JSON after `ApplyTakePeaks` stored 64 numbers (`0, 4, 8, 12, 16`, then by fours).
Empty input omitted the `peaks` key. The take file path did not exist, so no audio read occurred.
Happy-path segment 1 stored a 64-value JSON number array. Segment 2 stored `[]`.

### Learned priors

Captured query:

`FROM take_rates WHERE language = {language:String} AND speaker = {speaker:String}`

Bound parameters were `ten`, `ml-IN`, and `Suni Williams`. Sample counts were 50 and 10.
Independent weight is `10/(10+10)` = 0.5. Independent rate is `0.5*4 + 0.5*2` = 3.

`take_rates` over the ingested takes returned 11 rows (10 fixture takes plus the recovered drop take).

### Action vocabulary

Eight insert bodies stored `segment_created`, `boundary_nudged`, `speaker_reassigned`, `text_corrected`,
`take_rendered`, `atempo_stretched`, `line_rewritten`, and `user_command`.
A `stretched` insert never reached HTTP. The writer returned that `stretched` is unknown.

## Exit criteria

| Criterion | Independent result |
|---|---|
| Resilient write queues verified under network drops | Queue replays journaled rows after reconnect. `RecordTake` journals take plus charges before the first Flush. Finding 1 is closed. |
| Sum of logged charges matches total API expenses without double counting | Decimal sum matches the charges view at 0.30159225. FINAL collapses a duplicate insert from 60 raw rows to 30. The dropped take recovered three charge rows. |
| Database stores signed duration deltas | Captured JSON and ClickHouse both store segment 8 as -2910. The unsigned 2910 insert failed `delta_is_signed`. |
| Commit DAG supports branching and time travel queries | Fork insert and `timeline_at_commit` match independent replay on root, child, and fork. |
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

PHASE-4 still lists action type `stretched`. The writer stores `atempo_stretched` from the frozen P1 wire.
A `stretched` insert never reached HTTP.
That task line is coordination copy. It does not change captured behaviour.

No unowned ClickHouse writer appeared. T7.1 still owns `cmd/ajilamu`. That missing process is documented and pending.

I removed temporary review artifacts under `/tmp/p4-close-r2` after measurement. No production code changed during review.
