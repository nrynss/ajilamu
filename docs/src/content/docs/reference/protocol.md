---
title: Agent Protocol & Principles
description: The adversarial review loop, role boundaries, and the core measurement discipline.
template: doc
---

The protocol in `AGENTS.md` governs every human engineer and AI agent contributing to this repository.

---

## Four Separated Roles

The protocol separates responsibilities across four distinct roles:

| Role | Core Duty | Forbidden Action |
| :--- | :--- | :--- |
| **Orchestrator** | Selects tasks, dispatches agents, gates reviews, and lands commits. | Never implements tasks or pardons unreviewed defects. |
| **Implementation** | Writes code strictly inside the files named in the task block. | Never reviews its own diffs or marks tasks done. |
| **Review** | Evaluates diffs against specifications and records measured findings. | Never applies fixes or softens severe defects. |
| **Remediation** | Resolves identified defects row by row. | Never changes verdicts or alters unflagged code. |

Each role runs as a fresh agent instance. An agent never reviews code that it wrote.

---

## The Adversarial Cycle

Every task progresses through a structured verification loop:

1. **Implement**: The implementer modifies only the designated owned paths.
2. **Review**: An independent reviewer inspects the diff and generates a report with findings categorized by severity.
3. **Remediate**: A remediator addresses every finding with pinpoint fixes.
4. **Re-Review**: A fresh reviewer evaluates the updated commit until achieving zero unresolved findings.
5. **Land**: The orchestrator commits the approved changes to the branch.

Findings fall into four severities:
- **Critical (C)**: Breaks the demo.
- **High (H)**: A real defect that the demo survives.
- **Medium (M)**: A real defect with a viable workaround.
- **Low (L)**: Visual or structural polish.

Every finding requires a resolution. Developers never dismiss an issue simply because it carries low severity.

---

## A Pin Is a Measurement, Never a Log Line

This principle forms the bedrock of our verification discipline.

Software naturally reports its own success. That report represents the subject under evaluation rather than valid evidence of correctness.

During early validation, a logging call reported "perfect fit" for a take that ran 40.9 percent short of its designated slot. Relying on logged strings masked severe lip sync degradation.

A valid pin requires independent observation. Reviewers inspect artifacts using external measurement tools:
- Run `ffprobe` or audio analysis scripts directly against generated WAV files.
- Query ClickHouse tables using independent SQL statements.
- Issue live HTTP requests against running services.
- Inspect rendered visual pages directly.

Quoting internal log statements does not constitute review evidence.

---

## Preventing Documentation Drift

Task reviews only inspect paths that a task explicitly owns. Disconnected claims in documentation can slip past unnoticed.

To prevent architectural drift, every phase close runs an automated audit:

```bash
python3 tools/audit_docs.py
```

The script verifies that documented dependencies, environment variables, and module structures match the live codebase.
