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
status:     claimed:claude-opus-5
```
Write reproducible SQL DDL into `sql/schema.sql`.

Create tables for `takes`, `commits`, `actions`, and `charges`. Store signed deltas instead of one-sided percentages.

Store waveform peaks as `Array(UInt8)` on take rows. Use server-side timestamps (`DEFAULT now64(3)`) for `created_at`.

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

### T1.5: Fixture manifest ★
```yaml
requires:   T0.4, T1.1, T1.4
fixture-ok: no
size:       S · mid
owns:       testdata/manifest.json, internal/fixtures/load.go
status:     not-started
```
Build a fixture loader that converts `testdata/` files into typed objects.

`fixtures.LoadDub()` returns a complete dub project containing 8 segments, 10 takes, and applied stretches.

The frontend consumes the identical JSON payload to render the timeline during standalone development.

**Done when:** The web interface renders the full workspace from static fixture data without running a Go backend server.

---

## Exit Criteria

- [ ] `Fit` enforces signed delta comparisons in `internal/types`.
- [ ] Cost model tracks Gemini and TTS calls without double counting.
- [ ] `sql/schema.sql` reproduces the entire database schema from scratch.
- [ ] Matching wire types exist in Go and TypeScript.
- [ ] Fixture loader serves mock data to Go tests and the frontend.

---

## Handoff Log

_(Fill on completion: what exists now, what surprised you, and notes for the next developer.)_
