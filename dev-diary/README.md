# Development Diary: Phase Handoffs

This document outlines the work breakdown for **Ajilamu**.

Reference specifications:
- [`project.md`](project.md): Product architecture and goals.
- [`ui-ux.md`](ui-ux.md): Interface design, layout, and styling.
- [`observations.md`](observations.md): First run validation findings.

Where a phase document and specification conflict, the specification wins. Record discrepancies in the handoff log.

Each document provides complete context for an engineer starting cold.

**Current status:** We validated the core pipeline end to end. We have not built the application yet.

---

## Swarm Coordination Rule

> **Phase-level dependencies are advisory. Task-level dependencies are binding.**

Do not delay work for an entire phase to finish. If task `T5.4` requires only `T1.1` and `T1.5`, start immediately when those tasks complete.

**The initial validation run created real test fixtures.** Task `T0.4` promotes 10 real WAV takes, `segments.json`, and exports into `testdata/`. Tasks marked `fixture-ok` run immediately against local fixtures without waiting for live cloud services.

This allows offline development. Engineers can build and test the audio assembler, timeline canvas, and ledger views without invoking Gemini or Chirp APIs. Only Phase 2 requires active network connections.

---

## Phase Graph

| Phase | Document | Requires | Runs parallel with | Blocks |
|---|---|---|---|---|
| **P0** Ground | [PHASE-0-ground.md](PHASE-0-ground.md) | none | none | everything |
| **P1** Contracts and fixtures | [PHASE-1-contracts.md](PHASE-1-contracts.md) | T0.1, T0.3 | none | downstream tracks |
| **P2** Fit loop | [PHASE-2-fit-loop.md](PHASE-2-fit-loop.md) | P1, T0.3 | P3, P4, P5, P6, P7 | P7 (live run), P8 |
| **P3** Assembler | [PHASE-3-assembler.md](PHASE-3-assembler.md) | P1, T0.3, T0.4 | P2, P4, P5, P6, P7 | P7 (export), P8 |
| **P4** Ledger | [PHASE-4-ledger.md](PHASE-4-ledger.md) | P1 | P2, P3, P5, P6, P7 | P6 (history), P8 |
| **P5** Workspace | [PHASE-5-workspace.md](PHASE-5-workspace.md) | T1.1, T1.4, T1.5 | P2, P3, P4, P6, P7 | P6, P8 |
| **P6** Editing | [PHASE-6-editing.md](PHASE-6-editing.md) | T5.3, T5.5, soft P4 | P2, P3, P7 | P8 |
| **P7** Ingestion | [PHASE-7-ingestion.md](PHASE-7-ingestion.md) | T1.4, soft P2, P3 | P2, P3, P4, P5, P6 | P8 |
| **P8** Ship | [PHASE-8-ship.md](PHASE-8-ship.md) | P2, P3, P5, P7 | none | final submission |

```text
  P0 ──▶ P1 ──┬──▶ P2 fit loop ─────┐
              ├──▶ P3 assembler ────┤
              ├──▶ P4 ledger ───────┼──▶ P8 ship
              ├──▶ P5 workspace ──┬─┤
              ├──▶ P6 editing ◀───┘ │
              └──▶ P7 ingestion ────┘
```

Phases P0 and P1 form the initial serial bottleneck. Once contracts freeze, development scales across six parallel tracks.

---

## Parallel Tracks

When Phase P1 completes, six tracks launch concurrently without path conflicts:

| Track | Phase | Network Required? | First Task | Readiness |
|---|---|---|---|---|
| **A: Fit loop** | P2 | Yes (Gemini and Chirp) | T2.1 | Ports proven Python validation logic to Go. |
| **B: Assembler** | P3 | No | T3.1 | Builds offline on `testdata/` WAV fixtures. |
| **C: Ledger** | P4 | ClickHouse only | T4.1 | Implements SQL schema, DAG, and analytics. |
| **D: Workspace** | P5 | No | T5.1 | Builds Svelte UI against mock JSON data. |
| **E: Ingestion** | P7 | No | T7.1 | Handles multipart uploads and job orchestration. |
| **F: Editing** | P6 | No | T6.4 | Parses natural language timeline edit commands. |

Prioritize tracks B and D first if resources are constrained. Track B corrects audio mixing. Track D builds the entire user interface.

---

## Task Pattern

Every task in the phase documents follows this template:

```markdown
### T3.2: Take placement
requires:   T1.1, T3.1
fixture-ok: yes
size:       M · frontier
owns:       internal/assemble/place.go
status:     not-started
```

- **requires:** Binding task identifiers.
- **fixture-ok:** Indicates whether development can begin using local fixture files.
- **size:** Scope estimation, from XS to XL.
- **class:** Which agent to send. See the agent class table in [`../AGENTS.md`](../AGENTS.md).
- **owns:** File paths exclusively managed by this task.
- **status:** Current task state.

A star (★) marks tasks on the critical demo path.

---

## Developer Guidelines

1. **Claim before starting:** Update status to `claimed:<id>` in the corresponding phase document.
2. **Respect file ownership:** Do not edit files outside your assigned `owns` path.
3. **Freeze contracts after P1:** Update `testdata/` alongside any type modifications.
4. **Enforce signed fit:** Measure duration delta as a signed value. Flag overruns and underruns equally.
5. **Preserve background audio:** Never delete non-dialogue audio. Place speech over the original soundtrack.
6. **Ground metrics in reality:** Connect every displayed number to real measurements and costs.
7. **Use creator language:** Follow terminology rules in `ui-ux.md` Section 8.
8. **Document handoffs:** Update the handoff log when finishing a phase or task.
9. **Write tests that pass on any machine:** A test must not assume where `t.TempDir()` lands.

---

### Portable tests, and one fix already applied

A test that passes only on the machine that wrote it hides real failures everywhere else.

`t.TempDir()` returns a path under the system temporary directory. On Linux that is often
`tmpfs`, and on macOS it sits under `/var/folders`. Either way it usually belongs to a different
filesystem from the repository checkout.

Two consequences follow. A hard link from a repository fixture into `t.TempDir()` fails with
`EXDEV`, because a hard link cannot cross a filesystem. Any check that compares device numbers
also behaves differently across the boundary.

Copy the fixture into the temporary directory first, then work on the copy. `os.Rename` across
the same boundary carries the same trap, so prefer a copy there too.

**Fixed 2026-09-07 in `internal/assemble/bed_test.go`.**
`TestBedRejectsInvalidInputsAndPreservesOutputs` linked `testdata/clip.mp4` straight into
`t.TempDir()`. It passed where the temporary directory shared a filesystem with the repository
and failed elsewhere with `invalid cross-device link`. P3 closed while the test failed on
another machine. The test now copies the clip into the temporary directory and links the
copy beside it. That keeps both paths on one filesystem. It still gives `os.SameFile` two names
for one inode, which is all the assertion needs. Verified passing with the default temporary
directory and with `TMPDIR` set inside the repository.

A red suite costs more than the one test it names. It teaches the next agent to read a failure
as noise.

### macOS, which P5 will build on

P5 runs on a Mac. Two macOS behaviours differ from the Linux boxes the other tracks use, and
both stay invisible until Linux rejects work that passed locally.

**The filesystem ignores case by default.** APFS treats `Timeline.svelte` and `timeline.svelte`
as one file. An import whose casing does not match its file resolves on a Mac and fails on
Linux. Vite reports nothing on the machine that wrote it. This bites component imports hardest,
because a track full of them only needs one wrong letter. Match the casing exactly, and let CI
on Linux stay the authority.

**System paths hide a symlink.** `/var` points at `/private/var`, and `t.TempDir()` returns a
path underneath it. A path that arrives resolved and a path that arrives unresolved name one
file while comparing unequal as strings. Compare with `os.SameFile` rather than with `==`, or
run both sides through `filepath.EvalSymlinks` first.

Nothing here blocks P5. Both traps cost minutes when a reader expects them and hours when
nobody does.

---

## Scope and Execution

All tasks defined across these documents will ship.

We size tasks for full completion rather than approximate implementation. If a task proves unwieldy, split it into subtasks and document the change in the handoff log.

If external blockers arise, record the root cause clearly in the handoff log and notify dependent tasks.

---

## Critical Path

The demonstration depends on this unbroken execution sequence:

```
Upload or sample (T7.1, T7.2)
  -> Segment (T2.1) -> Translate (T2.2) -> Synthesize (T2.3)
  -> Measure signed delta (T2.4) -> Repair both directions (T2.5, T2.6)
  -> Place takes over audio bed (T3.1, T3.2) -> Duck music (T3.3) -> Export (T3.4)
  -> Record ledger rows (T4.2) -> Render timeline with length bars (T5.3, T5.4)
```

Protect this core flow above auxiliary features.

---

## Status Board

| Phase | Tasks Complete | Status |
|---|---|---|
| P0 | 4 / 4 | Complete. Close review approved with zero residue. See [p0-close-round2.md](adversarial-review/p0-close-round2.md). |
| P1 | 6 / 6 | Complete. Contracts frozen with one example payload per shared struct. See [t1.4a-round2.md](adversarial-review/t1.4a-round2.md). |
| P2 | 10 / 10 | Complete. T2.1 to T2.7 approved with zero residue. T2.8a and T2.8b approved, with round four's L applied under the exemption. See [t2.8b-round4.md](adversarial-review/t2.8b-round4.md). |
| P3 | 5 / 5 | Complete. Close review approved with zero residue. See [p3-close-round1.md](adversarial-review/p3-close-round1.md). |
| P4 | 8 / 8 | Complete. Close review approved with zero residue. See [p4-close-round2.md](adversarial-review/p4-close-round2.md). |
| P5 | 7 / 7 | Complete. Workspace renders from offline fixtures. See [PHASE-5-workspace.md](PHASE-5-workspace.md). |
| P6 | 4 / 7 | In progress. T6.1, T6.2, T6.4 and T6.6 approved with zero residue. See [t6.6-round3.md](adversarial-review/t6.6-round3.md). |
| P7 | 19 / 20 | In progress. T7.0, T7.0a, T7.1, T7.2, T7.2a, T7.2b, T7.2c, T7.2c1, T7.2d, T7.3, T7.3a, T7.3b, T7.3c, T7.4, T7.4a, T7.5, T7.5a, T7.6 and T7.7 approved. T7.5b remains. See [t7.7-round2.md](adversarial-review/t7.7-round2.md). |
| P8 | 0 / 4 | Not started. |

**Concept validation:** Complete. Recorded in [observations.md](observations.md) and reviewed in [adversarial-review](adversarial-review/2026-09-07-pipeline-and-claims.md).
