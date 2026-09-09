# H1 line cost remediation

**Date:** 2026-09-09
**Agent:** LineCostFix, implementation role
**Findings:** H1 from `p8-close-round1.md`, and L2 on the Details tab charge labels.

## Mechanism

The fit loop records every rendered attempt in `LineResult.Attempts`. It marks one
attempt as the chosen take. The pipeline adapter turned only `line.ChosenTake`
into one `api.RunTake`. The ledger therefore held one take row per line. The
workspace attached a charge only when a take's attempt matched. Charges from a
discarded attempt matched no take row. The Details tab summed the takes it
received, so it read the chosen attempt's cost as the whole line and Tries as 1.

The live dub `38fa4e9c69715c3c3b2d011a14d9eccf` line 7 held 11,398,200
nanodollars across attempts 1, 2 and 3, while the tab read `$0.00366765` and
Tries 1.

L2 is separate. One Gemini translate call bills a prompt row and a candidate row.
The wire `Charge` carried kind, ids, units and prices but no billing unit. The two
rows therefore rendered with the same label.

## The fix

- `internal/api/run.go` adds `RunTake.Active`. `mintRunIdentity` links each
  segment's timeline snapshot to the active take rather than the last one.
  `RunResult.Takes` now lists one entry per rendered attempt.
- `cmd/ajilamu/main.go` maps every `LineAttempt` to its own `RunTake`. Each take
  carries that attempt's text, file, fit, repair, peaks and charges.
  `chargesForAttempt` attaches only the calls whose attempt matches. Attempt 0
  work stays whole-pass. The commit message counts distinct lines.
- `internal/ledger/workspace.go` already returned every take row. The
  `selectWorkspaceTakes` statement now computes `active`, the take the newest
  timeline snapshot names. It orders the active take last, so the wire rule that
  the last take is active holds. `cmpWorkspaceTakeRow` applies the same order and
  the line text follows the active row. Both charge mappings carry `Unit`.
- `internal/api/wire.go` adds `Charge.Unit`. `web/src/lib/types.ts` mirrors it,
  and every wire example that carries a charge now names the unit.
- `web/src/lib/tabs/DetailsTab.svelte` labels each charge with its unit. The
  prompt row and the candidate row now read differently. Row order and the cost
  arithmetic are unchanged.

## Boundary

`internal/api/rerender.go` builds the re-render response charges without a unit,
because that file sits outside this task. Its `wireCharges` already collapses a
Gemini call into one row with zero units and zero unit price, so that response
never held a prompt row and a candidate row to tell apart. The workspace payload
comes from the ledger, where every row names its unit. A page reload reads the
ledger rows again.

## SQL pin

The pin `TestLineCostStoresEveryAttempt` in `cmd/ajilamu/main_test.go` renders one
line in three attempts and one line in a single attempt. It persists the run and
reads the views and the workspace payload. It skips unless
`AJILAMU_CHARGE_PIN_CLICKHOUSE=1`.

```bash
docker run -d --name line-cost-ch -p 18127:8123 -p 19004:9000 clickhouse/clickhouse-server:26.8.2.7
docker exec line-cost-ch clickhouse-client --query "CREATE USER IF NOT EXISTS line IDENTIFIED WITH plaintext_password BY 'linescratch' HOST ANY"
docker exec line-cost-ch clickhouse-client --query "CREATE DATABASE IF NOT EXISTS ajilamu_line_cost"
docker exec -i line-cost-ch clickhouse-client --database ajilamu_line_cost --multiquery < sql/schema.sql
docker exec line-cost-ch clickhouse-client --query "GRANT SELECT, INSERT ON ajilamu_line_cost.* TO line"

AJILAMU_CHARGE_PIN_CLICKHOUSE=1 AJILAMU_CHARGE_PIN_ADDR=127.0.0.1:18127 \
AJILAMU_CHARGE_PIN_DB=ajilamu_line_cost AJILAMU_CHARGE_PIN_USER=line \
AJILAMU_CHARGE_PIN_PASSWORD=linescratch \
go test -count=1 -run '^TestLineCostStoresEveryAttempt$' -v ./cmd/ajilamu/
```

The pin mints a fresh dub id per run, so a rerun never collides. The run below is
`dub-line-cost-pin-1788974949790114144`.

```text
--- RUN   TestLineCostStoresEveryAttempt
    dub id = dub-line-cost-pin-1788974949790114144
    line 1 take attempts:
        1
        2
        3
    line 1 SQL charge sum = 981000, run reported 981000
--- PASS: TestLineCostStoresEveryAttempt (2.38s)
```

The takes view holds one row per rendered attempt.

```sql
SELECT segment_index, attempt, repair, measured_ms, delta_ms FROM takes
WHERE dub_id='dub-line-cost-pin-1788974949790114144'
ORDER BY segment_index, attempt;
-- 1  1  rewrite  3000  1000
-- 1  2  rewrite  4000  2000
-- 1  3  rewrite  4000  2000
-- 2  1  none     2000     0

SELECT segment_index, count() AS take_rows, groupArray(attempt) AS attempts
FROM takes WHERE dub_id='dub-line-cost-pin-1788974949790114144'
GROUP BY segment_index ORDER BY segment_index;
-- 1  3  [1,2,3]
-- 2  1  [1]
```

The charges view keeps every call and still sums to the same number. Each attempt
bills one translate prompt row, one translate candidate row and one render row.

```sql
SELECT attempt, toString(kind) AS kind, toString(unit) AS unit,
       toInt64(round(units)) AS units,
       sum(toInt64(round(cost_usd * 1000000000))) AS nanos
FROM charges WHERE dub_id='dub-line-cost-pin-1788974949790114144'
  AND segment_index=1 AND kind IN ('translate', 'synthesize')
GROUP BY attempt, kind, unit, units ORDER BY attempt, kind, unit;
-- 1  synthesize  characters        10  300000
-- 1  translate   candidate_tokens  20   12000
-- 1  translate   prompt_tokens    100   15000
-- 2  synthesize  characters        10  300000
-- 2  translate   candidate_tokens  20   12000
-- 2  translate   prompt_tokens    100   15000
-- 3  synthesize  characters        10  300000
-- 3  translate   candidate_tokens  20   12000
-- 3  translate   prompt_tokens    100   15000

SELECT count() AS rows, sum(toInt64(round(cost_usd * 1000000000))) AS line1_nanos
FROM charges WHERE dub_id='dub-line-cost-pin-1788974949790114144'
  AND segment_index=1 AND kind IN ('translate', 'synthesize');
-- 9  981000

SELECT count() AS rows, uniqExact(event_key) AS keys,
       sum(toInt64(round(cost_usd * 1000000000))) AS nanos
FROM charges WHERE dub_id='dub-line-cost-pin-1788974949790114144';
-- 14  14  1362000
```

The charge rows themselves are the same rows the pre-fix writer stored, because
the charge `event_key` never named the take. The line sum is unchanged at
981,000 nanodollars. The fix changes which take row owns each row.

## Workspace payload pin

The same run served over `GET /api/dubs/{id}`. The payload carries three takes for
line 1, the chosen attempt last, and every take carries its own charges.

```text
segment 1 takes 3 attempts [2, 3, 1] line_sum 981000
  attempt 2 measured_ms 4000 units ['characters', 'candidate_tokens', 'prompt_tokens']
  attempt 3 measured_ms 4000 units ['characters', 'candidate_tokens', 'prompt_tokens']
  attempt 1 measured_ms 3000 units ['characters', 'candidate_tokens', 'prompt_tokens']
segment 2 takes 1 attempts [1] line_sum 327000
  attempt 1 measured_ms 2000 units ['characters', 'candidate_tokens', 'prompt_tokens']
total 1362000 | 1 segment call, 4 translation calls, 4 render calls, and 0 agent calls.
```

## DOM pin

The server ran the built frontend against the same database. The browser loaded
`/d/dub-line-cost-pin-1788974949790114144` and opened the Details tab. The
Details tab reads the line cost equal to the SQL sum, Tries 3, and the chosen
attempt as the active take.

```text
LINE 1

Suni Williams

source one

Voice
ml-IN-Chirp3-HD-Achernar
Source slot
2.00 seconds
Active take
3.00 seconds
Tries
3

This take was rewritten before it was recorded.

What this line cost
Voice render (characters) for try 2
$0.0003
Translation (candidate tokens) for try 2
$0.000012
Translation (prompt tokens) for try 2
$0.000015
Voice render (characters) for try 3
$0.0003
Translation (candidate tokens) for try 3
$0.000012
Translation (prompt tokens) for try 3
$0.000015
Voice render (characters) for try 1
$0.0003
Translation (candidate tokens) for try 1
$0.000012
Translation (prompt tokens) for try 1
$0.000015

This line has cost $0.000981 across every try.

PROJECT COST

$0.001362

1 segment call, 4 translation calls, 4 render calls, and 0 agent calls.

Finding lines (candidate tokens)
$0.000024
Finding lines (prompt tokens)
$0.00003
```

The Lines tab reports the single-attempt line as Tries 1.

```text
Line 1 | Play take | Line 1 length bar. This take overruns the slot by 1.00 seconds.
Line 2 | Play take | Line 2 length bar. This take fits the 2.00 seconds slot.
1 | 2.00s | 3.00s | 3
2 | 2.00s | 2.00s | 1
```

The active take reads 3.00 seconds, which is attempt 1. The chosen attempt is the
closest, and it is not the last attempt the loop rendered.

## Mutation

Three reverts redden the pins.

1. Skipping a non-chosen attempt in `pipelineRunResult` reddens the SQL pin.
   `run reported 1 attempts on line 1 and 1 on line 2, want 3 and 1`. The ledger
   would hold one take row per line again.
2. Dropping the `Active` key from `cmpWorkspaceTakeRow` reddens
   `TestWorkspaceTakesPutsTheActiveAttemptLast`. The takes sort by attempt, so the
   last take is attempt 3 and the Details tab would show the wrong active take.
3. Dropping `Unit: row.Unit` from `buildLanguageTracks` reddens the SQL pin with
   `take attempt 2 charge carries no unit`. The Details tab would show
   `Translation for try 2` twice.

## Files changed

| Path | Change |
|---|---|
| `internal/api/run.go` | `RunTake.Active` and a timeline link to the chosen take. |
| `internal/api/run_test.go` | The identity pin uses three takes and asserts the active link. |
| `internal/api/wire.go` | `Charge.Unit` names the billing unit. |
| `internal/api/wire_test.go` | Pins the unit on the charge example. |
| `internal/ledger/workspace.go` | `selectWorkspaceTakes` marks the active take, the comparator sorts it last, and both charge mappings carry `Unit`. |
| `internal/ledger/workspace_test.go` | Pins the active-last order, the line text and the charge unit. |
| `cmd/ajilamu/main.go` | One run take per attempt with its own charges, and a line count for the commit message. |
| `cmd/ajilamu/main_test.go` | Updates the adapter and wiring pins and adds the ClickHouse line cost pin. |
| `web/src/lib/types.ts` | Mirrors `Charge.Unit`. |
| `web/src/lib/tabs/DetailsTab.svelte` | Labels each charge with its unit. |
| `testdata/wire/charge.json`, `dub.json`, `language_track.json`, `line.json` | Every charge object names its unit. |

## Bar

All five commands exit 0.

```bash
go build ./...
go vet ./...
go test -count=1 ./internal/... ./cmd/...
npm --prefix web run check
python3 tools/audit_docs.py
```

`npm --prefix web run check` reports 183 files, 0 errors and 0 warnings. The audit
reports no drift.

## Cleanup

The container `line-cost-ch` and the server `line-cost-server` were stopped and
removed. The browser tab closed. The ledger data stays in the scratch database
`ajilamu_line_cost` on the removed container, so it is gone with it. No commit was
made. No repository write survives outside the paths above and this record.
