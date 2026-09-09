# Charge wiring, live reconciliation

**Date:** 2026-09-09
**Host:** `https://ajilamu.nryn.dev`, deployed from commit `9fae43f`.
**Verdict:** PASS. The ledger reconciles with the run to the nanodollar.

This closes the failure recorded in `t8.1-charge-verify.md`, where a live run wrote zero
charge rows after the charge-identity change.

## Run

| Field | Value |
|---|---|
| Dub id | `38fa4e9c69715c3c3b2d011a14d9eccf` |
| Run id | `b7d48fc00881f61f31e578291207ad6d` |
| Wall time | 239.44 seconds |
| Terminal event | `done`, "Dubbing pipeline completed with 5 flagged lines." |
| Run-reported total | 65,330,550 nanodollars |

## Reconciliation

The charges view applies FINAL, so a re-sent row collapses once.

```sql
SELECT count() rows, uniqExact(event_key) keys,
       toInt64(sum(toInt64(round(cost_usd * 1000000000)))) sum_nanodollars
FROM charges WHERE dub_id = '38fa4e9c69715c3c3b2d011a14d9eccf'
```

| rows | distinct event_key | sum nanodollars |
|---:|---:|---:|
| 56 | 56 | 65,330,550 |

The ledger sum equals the run-reported total. The gap is zero.

```sql
SELECT kind, count() rows, uniqExact(event_key) keys,
       toInt64(sum(toInt64(round(cost_usd * 1000000000)))) nd
FROM charges WHERE dub_id = '38fa4e9c...' GROUP BY kind ORDER BY kind
```

| kind | rows | distinct event_key | nanodollars |
|---|---:|---:|---:|
| segment | 2 | 2 | 1,513,650 |
| translate | 36 | 36 | 696,900 |
| synthesize | 18 | 18 | 63,120,000 |

The row counts match the call mix. One segmentation call bills a prompt and a candidate row.
Eighteen translation calls bill thirty-six rows. Eighteen render calls bill eighteen rows.

## Product surfaces

| Surface | Result |
|---|---|
| `GET /api/dubs` | `review`, 65,330,550 |
| `GET /api/dubs/{id}` | `review`, 65,330,550, "1 segment call, 18 translation calls, 18 render calls, and 0 agent calls." |

The index, the workspace and the ledger agree.

## Mutation

Reverting the proxy retarget so `SetRecorder` only forwards to the inner client reddens the
unit pins `TestChargeWiringRoutesAttemptsToTheWriter` and `TestProxyRetargetsFixedRecorder`
with zero charge rows and zero nanodollars. That is the state the failed live run measured.
