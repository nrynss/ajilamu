# P1: Contracts and Fixtures

```yaml
id:       P1
size:     M
requires: [T0.1, T0.3]
blocks:   downstream tracks
parallel: low
```

**Goal:** Define the four core contracts used across all tracks: domain types, cost model, database schema, and wire payloads.

**Why this phase is critical:** Six parallel development tracks compile against these definitions. We freeze contracts after completing this phase.

The type system must enforce two foundational rules: duration fit is signed, and every costed call is tracked exactly once.

---

### T1.1: Domain types ★
```yaml
requires:   T0.3
fixture-ok: no
size:       M · frontier
owns:       internal/types/types.go
status:     done
```
Define core Go structures: `Segment`, `Speaker`, `Take`, `Repair`, and `Fit`.

`Fit` carries a signed delta to ensure developers never overlook underruns:

```go
type Fit struct {
    Slot     time.Duration
    Measured time.Duration
    Delta    time.Duration // signed: positive = long, negative = short
}

func (f Fit) Ratio() float64
func (f Fit) Fits() bool
func (f Fit) TooLong() bool
func (f Fit) TooShort() bool
```

Avoid one-sided comparisons like `Fits() = Measured <= Slot`. The initial run missed segment 8's underrun because the comparison only checked overruns.

**Done when:** `Fit` validates every row of `testdata/expected/`. Segment 8 must report `TooShort() == true` with a delta of −2910 ms.

---

### T1.2: Cost model ★
```yaml
requires:   T1.1
fixture-ok: no
size:       M · frontier
owns:       internal/cost/cost.go
status:     done
```
Track expenses per operation. The validation script double-counted retries and omitted Gemini calls entirely.

Define `Charge` per API invocation: kind (`segment`, `translate`, `synthesize`), unit count, unit price, and associated take identifier.

A take cost reflects its own charges, never an accumulated sum from prior attempts. Load pricing rates from configuration rather than hardcoded literals.

**Done when:** Calculating expenses across the 8 test segments matches the exact sum of individual calls, including Gemini operations.

---

### T1.3: ClickHouse schema ★
```yaml
requires:   T1.1
fixture-ok: no
size:       M · frontier
owns:       sql/schema.sql
status:     done
```
Write reproducible SQL DDL into `sql/schema.sql`.

Create tables for `takes`, `commits`, `actions`, `charges`, and `timeline_state`. Store signed deltas instead of one-sided percentages.

Store waveform peaks as `Array(UInt8)` on take rows. Use server-side timestamps (`DEFAULT now64(3)`) for `created_at`.

The five `_raw` tables hold rows and dedup on the natural identity of each event. The five plain names are views that apply `FINAL`, so the obvious read never counts a retry twice. Two read views ship with the schema: `take_rates` for learned priors and `timeline_at_commit` for state at any commit. The preflight guard refuses to run against a database holding an incompatible legacy object.

**Done when:** Executing `sql/schema.sql` on a fresh database successfully creates all tables and accepts sample inserts.

---

### T1.4: Wire contracts ★
```yaml
requires:   T1.1
fixture-ok: no
size:       M · mid
owns:       internal/api/wire.go, web/src/lib/types.ts
status:     done
```
Define JSON payloads and server-sent events in Go and TypeScript.

Cover dub summaries, detailed take listings, fit metrics, charges, commits, and progress notifications.

Progress events must transmit natural sentences rather than raw status codes. The user interface renders these sentences directly.

**Done when:** Go and TypeScript type definitions match, and `testdata/` stores example JSON payloads for each structure.

---

### T1.4a: Wire examples for the ledger read payloads
```yaml
requires:   T7.2c
fixture-ok: yes
size:       XS · mid
owns:       testdata/wire/, internal/api/wire_test.go
status:     not-started
```
T1.4's done condition says `testdata/` stores one example payload per structure. T7.2c added five
wire types with no example, so the claim is false for `DubHistory`, `TimelineView`,
`TimelineEntry`, `BranchComparison`, and `BranchSummary`.

Add one payload each and register them in `TestExamplesUnmarshal`, so a decode failure fails the
suite. Keep every key identical to the Go tag.

**Done when:** Every shared struct has an example payload and the suite decodes each one.

---

### T1.5: Fixture manifest ★
```yaml
requires:   T0.4, T1.1, T1.4
fixture-ok: no
size:       S · mid
owns:       testdata/manifest.json, internal/fixtures/load.go
status:     done
```
Build a fixture loader that converts `testdata/` files into typed objects.

`fixtures.LoadDub()` returns a complete dub project containing 8 segments, 10 takes, and applied stretches.

The frontend consumes the identical JSON payload to render the timeline during standalone development.

**Done when:** The web interface renders the full workspace from static fixture data without running a Go backend server.

---

## Exit Criteria

- [x] `Fit` enforces signed delta comparisons in `internal/types`.
- [x] Cost model tracks Gemini and TTS calls without double counting.
- [x] `sql/schema.sql` reproduces the entire database schema from scratch.
- [x] Matching wire types exist in Go and TypeScript.
- [x] Fixture loader serves mock data to Go tests and the frontend.

---

## Handoff Log

### What exists now

All five P1 tasks are done. `internal/types` enforces signed fit deltas. `internal/cost`
tracks every API call once in nanodollars. `sql/schema.sql` builds five `_raw` tables,
five `FINAL` views and two read views on Cloud and local ClickHouse. `internal/api` and
`web/src/lib/types.ts` define matching wire payloads. `internal/fixtures` loads the full
workspace from `testdata/manifest.json`, and `testdata/wire/` stores one example payload
per structure.

The production `default` database now runs the ledger schema. The pre-T1.3 validation
table survives as `takes_legacy_pre_t13` with its 8 rows intact.

### What surprised us

The schema took five adversarial rounds. Dedup needs natural-event keys, never client
ids. The preflight guard must compare expressions and engine parameters exactly, or an
incompatible table passes and silently collapses distinct events.

The T1.3 block named four tables. The schema ships five tables and seven views, because
`timeline_state`, `take_rates` and `timeline_at_commit` close T4.5 and T4.7. Review
records in `adversarial-review/t1.3-round*.md` carry the reasoning.

`internal/cost` declares `Charge.TakeID` as an int. `charges.take_id` is a String. T4.2
must bridge the two.

### Notes for the next developer

Contracts freeze after this phase. Code against the wire types in `internal/api` and the
schema in `sql/schema.sql`, never against earlier names.

T4.1 needs three write rules recorded in `PHASE-4-ledger.md`. Writes name the `_raw`
table. Reads name the plain view. A genuine repeat call must raise `attempt`. T4.5 needs
the snapshot rule recorded too, that a `timeline_state_raw` writer copies every field
forward.

The schema loads through `clickhouse client --queries-file`. The HTTP endpoint refuses a
multi statement body. T4.1 must split statements if it applies the schema over HTTP.
