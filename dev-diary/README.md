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
| P1 | 0 / 5 | Ready to start. Unblocked by P0. |
| P2 | 0 / 7 | Not started. |
| P3 | 5 / 5 | Complete. Close review approved with zero residue. See [p3-close-round1.md](adversarial-review/p3-close-round1.md). |
| P4 | 0 / 7 | Not started. |
| P5 | 0 / 6 | Not started. |
| P6 | 0 / 5 | Not started. |
| P7 | 0 / 4 | Not started. |
| P8 | 0 / 4 | Not started. |

**Concept validation:** Complete. Recorded in [observations.md](observations.md) and reviewed in [adversarial-review](adversarial-review/2026-09-07-pipeline-and-claims.md).
