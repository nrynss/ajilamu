# P6 close: independent verification, round 5

**Scope:** Round 4 remediation on the uncommitted working tree above `c4f1f94`.
**Method:** Independent HTTP, Chrome, SQL, source, and audit measurements.
**Verdict:** APPROVE. Findings: 0 C, 0 H, 0 M, 0 L.

| C | H | M | L |
|---|---|---|---|
| 0 | 0 | 0 | 0 |

I own only this review file. I did not fix production code, tick exit boxes, change task
status, or commit. I differ from the round 4 reviewer and its remediator.

## Audit and checks

`python3 tools/audit_docs.py` ran before the close measurements and exited 0.
It reported no drift.

`go test -count=1 ./internal/command ./internal/api ./cmd/...` passed.
The agent, cost, and config package tests passed separately.
The frontend check reported zero errors and zero warnings.
A fresh frontend production build completed.

## Findings

No finding met the bar of provable impact, an actionable fix, and an unintended defect.

| Where | What | Pin | Mutation |
|---|---|---|---|
| None | None | None | None |

## Round 4 M1 residue

**Zero residue.**

I started a fresh local ClickHouse database from `sql/schema.sql`.
I seeded three clear lines and one root commit for `p6r5live`.

A silent boundary overlap returned HTTP 400.
The same bounds with `allow_overlap: true` returned 201.
SQL then read line 1 at `0..4500` and line 2 at `4000..8000`.

Against that stored overlap, a speaker-only preview returned HTTP 200.
The response kept line 2 at `4000..8000` and changed only its speaker.

A shift of line 2 left by 250ms would grow the overlap from 500ms to 750ms.
That preview returned HTTP 422 with `line 2 would overlap line 1`.
A clear line shifted into line 2 also returned 422.

Chrome drove the current production bundle against the same stored timeline.
It previewed `change speaker for line 2 to Maya.` and rendered the parsed intent.
It previewed the overlap-growing shift and rendered `line 2 would overlap line 1`.

The accepted speaker command also passed through the durable edit route.
It returned 201 and SQL read `user_command` by `command_bar`.
The command preserved the overlapping bounds.

Reverting to a flat no-overlap rule would return the speaker-only preview to 422.
Removing the growth comparison would let the measured left shift return 200.

## Round 4 M2 residue

**Zero residue against the required filing.**

T6.6c exists as the fourteenth P6 task and remains `not-started`.
Thirteen task blocks remain done.
Its owns list names the schema, durable ledger writer, running total, route, and entrypoint.
It owns no path under `internal/agent`.

The README reports P6 as `13 / 14`.
It names T6.6c in the current status, status board, and follow-up note.
The documentation audit independently accepted those counts.

The writer correctly remains absent until T6.6c lands.
An independent insert of charge kind `agent` failed with ClickHouse code 691.
That expected failure confirms the filed task still describes real unfinished work.
It is not a new close finding.

## Prior close residue

**Round 1 H1 has zero residue.**

The confirmed boundary edit and speaker command each wrote one child commit.
SQL read one `boundary_nudged` action by `manual_ui`.
It also read one `user_command` action by `command_bar`.

The three-commit ancestry was linear through versions one, two, and three.
The timeline grew from three seed snapshots to five snapshots.
The take count stayed at three.

**Round 2 M1 has zero residue.**

The silent overlap returned 400 before any child commit existed.
The confirmed overlap returned 201 and created one child commit.
SQL read the overlapping head bounds from the new snapshot.

## Evidence

- Review server port: `19671`
- Chrome debugging port: `19672`
- ClickHouse HTTP port: `18171`
- Fresh database: `p6close_r5`
- Fresh dub: `p6r5live`
- Loaded schema: `sql/schema.sql`

## Process ownership

I stopped every server, browser, and container I started for this review.
No listener remained on ports `19671`, `19672`, or `18171`.
The review ClickHouse container no longer existed.
