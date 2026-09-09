# The projects index reports the ledger truth

**Date:** 2026-09-09
**Agent:** IndexReadinessFix, implementation role
**Scope:** the projects index read only. No workspace route, no ledger write path, no web file.

## Root cause

`GET /api/dubs` built every row from `api.ListUploadSummaries`. That function reads
`project.json` and hardcodes `ReadinessPending`, and it leaves `TotalNanodollars` at zero.
It never reads the ledger. So a finished dub showed as waiting and free while its workspace
payload showed the real state. The four live dubs named in the ticket each hold one commit,
seven takes and their charges. The index reported `pending` and `0` for every one of them.

## The fix

The index now overlays each upload row with the ledger.

- `internal/api/index.go` adds `IndexFact`, the `IndexLedger` interface, and
  `IndexHandlerWithLedger`. The handler asks the ledger once for the whole row set, then
  replaces the readiness and the running total of every row the ledger answered for. A row
  the ledger holds no data for keeps the upload values, so an upload with no ledger rows stays
  pending and free. A ledger read failure answers 500 rather than reporting a started project
  as unstarted.
- `internal/ledger/index.go` adds `Client.IndexFacts`. It runs three grouped statements. The
  first reads one row per take for every requested project. The second counts head timeline
  segments per project and language. The third sums charges per project. Each statement binds
  the whole id list as one `{dub_ids:Array(String)}` parameter.
- `internal/api/index.go` adds `ReadinessFacts` and `DeriveReadiness`. `DeriveReadiness`
  applies the precedence `workspaceReadiness` already implements. The index counts the grouped
  rows and calls it, so the index word matches the workspace word. The workspace route keeps
  its own function, because `internal/api/workspace.go` sits outside this task's paths.
- `cmd/ajilamu/main.go` wires `newIndexLedger(eventLedger)` into `IndexHandlerWithLedger`. A
  nil client stays a nil interface, so a clone with no ClickHouse credentials still serves the
  fixture and the upload rows.

The fixture row is unchanged. Its id is the UUID `d3bca364-9c8a-4107-8df9-c4faf909b008`, and
no ledger project carries that id, so the overlay never touches it.

## SQL pin

Local ClickHouse 26.8.2.7, database loaded from `sql/schema.sql`, seeded with the live shape of
`d04a7275133c68a59e16965dc8b23d58`. The seed came from the live ledger itself, so the rows are
the real ones.

```sql
SELECT count() AS charge_rows,
       sum(toInt64(round(cost_usd * 1000000000))) AS total_nanodollars
FROM charges
WHERE dub_id = 'd04a7275133c68a59e16965dc8b23d58'
```

```
62      72309000
```

```sql
SELECT (SELECT count() FROM takes WHERE dub_id = 'd04a7275133c68a59e16965dc8b23d58') AS takes,
       (SELECT uniqExact(segment_index) FROM timeline_state
        WHERE dub_id = 'd04a7275133c68a59e16965dc8b23d58') AS segments
```

```
7       7
```

## HTTP pin

The shipped server ran against that database. The same dub row and the workspace payload agree.

`GET /api/dubs` row for the seeded dub, beside the two uploads that hold no ledger rows:

```json
{
  "id": "d04a7275133c68a59e16965dc8b23d58",
  "title": "dubbed-run-3.mp4",
  "languages": ["ml"],
  "readiness": "review",
  "total_nanodollars": 72309000,
  "created_at": "2026-09-09T15:30:00Z",
  "updated_at": "2026-09-09T15:30:00Z"
}
{
  "id": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "readiness": "pending",
  "total_nanodollars": 0
}
{
  "id": "bb2c96d89be1cb62dc1ebb87979b95da",
  "readiness": "pending",
  "total_nanodollars": 0
}
```

The HTTP total equals the SQL sum of 72,309,000. The index word is `review`, and the SQL takes
and segments confirm the ledger holds data for the row.

`GET /api/dubs/d04a7275133c68a59e16965dc8b23d58` on the same server:

```
readiness: review
segments: 7
languages: [('ml', 7 lines, 7 takes)]
total: {'total_nanodollars': 72309000, 'covers': '1 segment call, 7 translation calls, 7 render calls, and 0 agent calls.'}
commits: 1
flagged lines: [3, 4, 6]
```

Both routes report `review` at 72,309,000 nanodollars.

## Grouped read pin

The index makes three ClickHouse queries for N projects. The count does not grow with N.

Live measurement from `system.query_log`, filtered to the three index statements:

| Upload rows in one `GET /api/dubs` | Ledger rows among them | Index SELECT statements |
|---|---:|---:|
| 1 | 1 | 3 |
| 4 | 2 | 3 |

`TestIndexFactsGroupsAcrossProjects` pins the same shape without a server. It calls `IndexFacts`
with one id and with four ids and asserts three requests each time. The four-id call carries
`param_dub_ids=['d1','d2','d3','d4']`, so the whole list rides one bound parameter.

## Tests

- `TestIndexHandlerWithLedgerReportsTheLedgerTruth` (new). A ledger fact of `review` at
  72,309,000 must reach the row, and an upload with no fact must stay pending at zero. A
  mutation that drops the overlay fails this test.
- `TestIndexHandlerWithLedgerRefusesToGuess` (new). A ledger error answers 500 and leaks no
  internal text.
- `TestIndexFactsGroupsAcrossProjects` (new). Pins the three-query count, the bound id list, the
  `review` word on a flagged line, and the charge sum.
- `TestIndexFactsLeavesAnEmptyProjectListUnqueried` (new). An empty id list sends no statement.
- `TestIndexAndWorkspaceReadinessAgree` (new). Runs the workspace route and the shared index
  rule over the same facts for pending, review, ready, a coverage gap and a run in flight. It
  asserts one word. A mutation to the flagged case in `DeriveReadiness` fails this test.
- `TestNewIndexLedgerReturnsNilForNilClient` (new). A nil client stays a nil interface.
- The existing index, upload and workspace tests still pass.

## Files changed

| Path | Change |
|---|---|
| `internal/api/index.go` | Added `IndexFact`, `IndexLedger`, `IndexHandlerWithLedger`, `ReadinessFacts`, `DeriveReadiness`, the overlay and its log line |
| `internal/api/index_test.go` | Added the overlay, failure and readiness agreement tests |
| `internal/ledger/index.go` | New file. Three grouped statements and `Client.IndexFacts` |
| `internal/ledger/workspace_test.go` | Added the grouped read tests |
| `cmd/ajilamu/main.go` | Wired the ledger adapter into the index handler |
| `cmd/ajilamu/main_test.go` | Added the nil client adapter test |
| `dev-diary/adversarial-review/index-readiness.md` | This record |

## Notes

The index cannot see the run registry. `ServerOptions.Index` is an `http.Handler` built before
`NewServer` creates the registry, and `internal/api/server.go` is outside this task's paths.
So a project with a run in flight reports its ledger word, while the workspace route reports
`running`. `DeriveReadiness` keeps the running input, so the index can pass it once a seam
exists. This is unchanged from the previous index, which never reported `running`.

`DeriveReadiness` and `workspaceReadiness` are two functions that apply one rule, because
`internal/api/workspace.go` is outside this task's paths. `TestIndexAndWorkspaceReadinessAgree`
drives both over the same facts and fails if they ever disagree.

The grouped statements read `takes`, `timeline_state` and `charges` views. The segment count
reads `commits_raw FINAL` because the recursive ancestry walk cannot use the `commits` view
without materializing every view column at every step. `selectBranchCost` makes the same choice
for the same reason.

## Cleanup

The ClickHouse container `ajilamu-index-ch` was stopped. The server process was stopped. No
container survives this task.
