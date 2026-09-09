# T8.1 charge wiring fix

**Date:** 2026-09-09
**Agent:** ChargeWiringFix, implementation role
**Verdict:** The production wiring now routes every charge through the recorder the fit loop installs.

## Mechanism

The fit loop wraps its recorder in `trackingRecorder`. That wrapper fills its own
`recorded` slice only when a client calls its `Add`. `PipelineResult.AttemptCharges` reads
that slice through `RecordedCharges()`.

Production never routed a charge through the wrapper. `cmd/ajilamu/main.go` built the
Gemini clients with a `chargeRouter` and the synthesizer with a `cost.Ledger`. Neither
client implements `SetRecorder`. The pipeline's `SetRecorder(tracker)` assertions therefore
failed. The clients kept the router and the ledger, and the wrapper's slice stayed empty.

`tracker.Total()` and `tracker.Charges()` still delegated to the inner ledger. The run
therefore reported the full cost while `AttemptCharges` was empty. `pipelineRunResult`
attaches no charge, so the ledger wrote no charge row.

## The fix

Three changes.

The fit recorder proxies now retarget a fixed recorder. `SetRecorder` still forwards to a
client that accepts one. A production client holds its recorder at construction, so the
proxy retargets that recorder instead. The proxy keeps the original recorder, so a later
run retargets it again.

`chargeRouter` now implements `fit.RecorderSetter`. Its target is a `fit.ChargeRecorder`,
so the fit tracker can replace the run ledger.

`newChargeRoutedRunner` builds the runner for production. It wraps the segmenter and the
translator in the fit proxies. `Run` and `RenderLine` build the synthesizer with the router
and wrap it in `NewSynthesizerProxy`. The router now points at the fit tracker for every
client, so every charge carries the attempt the loop set.

## The pin

`TestChargeWiringRoutesAttemptsToTheWriter` in `cmd/ajilamu/main_test.go` builds the runner
through `newChargeRoutedRunner`, the same helper `newPipelineRunner` calls. Stand-in
segmenter, translator and synthesizer clients bill known amounts through the router. The
line renders two attempts. The test asserts the writer receives the calls with the attempt
that produced each.

Pre-fix, with the proxy retarget reverted:

```text
attempt charges = map[], want two calls on each of attempts 1 and 2
charge rows = 0, want 8
```

Post-fix:

```text
--- PASS: TestChargeWiringRoutesAttemptsToTheWriter (0.95s)
```

`TestProxyRetargetsFixedRecorder` in `internal/fit/loop_test.go` pins the mechanism alone.
A client with a fixed recorder and no `SetRecorder` reaches the tracker only through the
proxy retarget.

Pre-fix:

```text
retargeted recorder charges = 0, want 1
```

Post-fix it passes.

## ClickHouse measurement

Docker ran `clickhouse/clickhouse-server:26.8.2.7` on port 18125 for HTTP and port 19002
for native. `sql/schema.sql` loaded into database `ajilamu_charge_wiring`.
`TestChargeWiringWritesChargeRows` ran the production wiring against it. The run reported
708,000 nanodollars over 8 charge rows. The dub is
`dub-charge-pin-20260909165534.086969637`.

Sum and row count:

```sql
SELECT count() AS rows, uniqExact(event_key) AS keys,
       sum(toInt64(round(cost_usd * 1000000000))) AS nanos
FROM charges WHERE dub_id='dub-charge-pin-20260909165534.086969637';
-- 8  8  708000
```

By kind:

```sql
SELECT toString(kind) AS kind, count() AS rows, uniqExact(event_key) AS keys,
       sum(toInt64(round(cost_usd * 1000000000))) AS nanos
FROM charges WHERE dub_id='dub-charge-pin-20260909165534.086969637'
GROUP BY kind ORDER BY kind;
-- segment     2  2   54000
-- synthesize  2  2  600000
-- translate   4  4   54000
```

The two identical translation calls bill the same 20 candidate tokens at 0.0000006 USD.
They differ only by attempt, so they store two rows with two distinct `event_key` values.

```sql
SELECT attempt, toString(unit) AS unit, toString(units) AS units,
       toString(unit_price_usd) AS price, event_key
FROM charges
WHERE dub_id='dub-charge-pin-20260909165534.086969637'
  AND kind='translate' AND unit='candidate_tokens'
ORDER BY attempt;
-- 1  candidate_tokens  20  0.0000006  6cbb7609dabd85a5b71d5413a08bebf9
-- 2  candidate_tokens  20  0.0000006  dbe8c021b5758c4e308fd8c5c2b5780a
```

The raw table agrees, and the take records attempt 2 at 2000ms with a zero delta.

```sql
SELECT count(), uniqExact(event_key) FROM charges_raw
WHERE commit_id='dub-charge-pin-20260909165534.086969637-commit';
-- 8  8

SELECT segment_index, attempt, measured_ms, delta_ms FROM takes
WHERE dub_id='dub-charge-pin-20260909165534.086969637';
-- 1  2  2000  0
```

The running total and the covers sentence read the same rows.

```text
running total = 708000 nanodollars
covers = "1 segment call, 2 translation calls, 2 render calls, and 0 agent calls."
```

The pre-fix wiring wrote nothing to the same real server.

```text
summary = 0  0  0
charge rows = 0, want 8
```

## Live run

The recorded t8.1 run reported 70,635,000 nanodollars and wrote zero charge rows. The
run-reported total is the inner ledger's own total, so those charges existed. The wiring
now sends the same charges through the fit tracker, so the writer receives them and the
ledger sums to 70,635,000 nanodollars.

The row count follows the call mix, one row per billed token kind and one row per synthesis
call. The seven take rows imply at least eleven billed attempts. At three rows per attempt
and two segmentation rows, the ledger would hold at least 35 rows.

## Mutation

Reverting the proxy retarget, so `SetRecorder` only forwards to `p.inner`, reddens both
pins. The unit pin writes zero charge rows and the ClickHouse pin sums to zero. Reverting
`newChargeRoutedRunner` to raw clients does the same, because the pipeline's type
assertions fail again.

## Bar

All four commands exit 0.

```bash
go build ./...
go vet ./...
go test -count=1 ./internal/... ./cmd/...
python3 tools/audit_docs.py
```

`go test -count=1 ./internal/... ./cmd/...` reports ok for every package. The ClickHouse
pin skips unless `AJILAMU_CHARGE_PIN_CLICKHOUSE=1`.

## Files changed

| Path | Change |
|---|---|
| `internal/fit/loop.go` | The three recorder proxies retarget a client's fixed recorder on `SetRecorder`. |
| `internal/fit/loop_test.go` | Adds `TestProxyRetargetsFixedRecorder` and its fixed-recorder stand-in. |
| `cmd/ajilamu/main.go` | Adds `newChargeRoutedRunner`, makes `chargeRouter` a `fit.RecorderSetter`, and routes the synthesizer through the router and the proxy. |
| `cmd/ajilamu/main_test.go` | Adds the production-wiring pin and the env-gated ClickHouse pin. |

## Cleanup

The container `t81-charge-wiring-ch` stopped and was removed. No other container was
touched. No repository write survives outside the four files above and this record. No
commit was made.
