#!/usr/bin/env python3
"""
Ajilamu - Documentation Drift Audit

The adversarial review loop reads a diff against a task specification. A claim in
project.md, infrastructure.md or README.md that no task owns appears in no diff, so
the loop cannot see it. That blind spot cost this project two components: the ADK
agent and the server entrypoint, both named in the architecture from the first
commit and never built.

This script reads the docs and the repository and reports where they disagree.
Run it at every phase close. Exit code 1 means drift.
"""

import re
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
DIARY = ROOT / "dev-diary"

# Modules named in the project.md stack table that are deliberately not in go.mod
# yet, each with the task that lands it. Remove the entry when the task lands.
PENDING_MODULES = {
    "google.golang.org/adk/v2": "T6.6 lands the pin",
}

findings = []


def drift(check, message):
    findings.append((check, message))


def read(path):
    return (ROOT / path).read_text(encoding="utf-8")


def phase_files():
    return sorted(DIARY.glob("PHASE-*.md"))


def tasks_in(path):
    """Yield (task_id, status, owns_paths) for each task block in a phase file."""
    text = path.read_text(encoding="utf-8")
    for block in re.finditer(
        r"^### (T[\d.]+\w*):.*?\n```yaml\n(.*?)\n```", text, re.S | re.M
    ):
        task_id, body = block.group(1), block.group(2)
        status = ""
        m = re.search(r"^status:\s*(\S+)", body, re.M)
        if m:
            status = m.group(1)
        owns = ""
        m = re.search(r"^owns:\s*(.*?)(?=^\w+:|\Z)", body, re.S | re.M)
        if m:
            owns = m.group(1)
        paths = [
            p.strip().rstrip(",")
            for p in re.split(r",\s*", owns.replace("\n", " "))
            if p.strip() and "(" not in p
        ]
        yield task_id, status, paths


def check_owned_paths_exist():
    """A done task whose owns path is missing means the doc outlived the code."""
    for path in phase_files():
        for task_id, status, paths in tasks_in(path):
            if status != "done":
                continue
            for p in paths:
                if p.endswith("/"):
                    ok = (ROOT / p).is_dir()
                else:
                    ok = (ROOT / p).exists()
                if not ok:
                    drift(
                        "owns",
                        f"{task_id} is done but owns a missing path: {p}",
                    )


def check_readme_counts():
    """The progress table drifts silently when a task is inserted."""
    readme = read("dev-diary/README.md")
    actual = {}
    for path in phase_files():
        pid = "P" + path.name.split("-")[1]
        rows = list(tasks_in(path))
        actual[pid] = (sum(1 for _, s, _ in rows if s == "done"), len(rows))
    for pid, (done, total) in sorted(actual.items()):
        m = re.search(rf"^\|\s*{pid}\s*\|\s*(\d+)\s*/\s*(\d+)\s*\|", readme, re.M)
        if not m:
            drift("readme", f"{pid} has no row in the README progress table")
            continue
        claimed = (int(m.group(1)), int(m.group(2)))
        if claimed != (done, total):
            drift(
                "readme",
                f"{pid} table says {claimed[0]}/{claimed[1]}, phase file has {done}/{total}",
            )


def check_stack_table():
    """Every Go module the stack table names must be in go.mod or explicitly pending."""
    project = read("dev-diary/project.md")
    gomod = read("go.mod")
    table = re.findall(r"^\|.*\|.*\|$", project, re.M)
    named = set()
    for row in table:
        for mod in re.findall(r"\b((?:[\w-]+\.)+[\w-]+/[\w./-]+)", row):
            mod = mod.rstrip("/.")
            if mod.startswith("gs://"):
                continue
            named.add(mod)
    for mod in sorted(named):
        base = re.sub(r"/v\d+$", "", mod)
        if base in gomod or mod in gomod:
            continue
        if mod in PENDING_MODULES:
            print(f"  pending: {mod} ({PENDING_MODULES[mod]})")
            continue
        drift(
            "stack",
            f"project.md stack table names {mod}, which go.mod does not require "
            f"and PENDING_MODULES does not excuse",
        )


def check_env_coverage():
    """Every .env.example variable needs a home in the secrets doc."""
    env_vars = set(re.findall(r"^([A-Z][A-Z0-9_]+)=", read(".env.example"), re.M))
    infra = read("dev-diary/infrastructure.md")
    for var in sorted(env_vars):
        if var not in infra:
            drift(
                "env",
                f"{var} is in .env.example but not in infrastructure.md, so it has "
                f"no documented production home",
            )


def check_no_untracked_go_packages():
    """Every internal package should be owned by some task."""
    owned = set()
    for path in phase_files():
        for _, _, paths in tasks_in(path):
            for p in paths:
                owned.add(str(Path(p).parent))
    for pkg in sorted((ROOT / "internal").iterdir()):
        if not pkg.is_dir():
            continue
        rel = f"internal/{pkg.name}"
        if rel not in owned:
            drift("ownership", f"{rel} exists but no task's owns list mentions it")


def main():
    print("Ajilamu documentation drift audit\n")
    for check in (
        check_owned_paths_exist,
        check_readme_counts,
        check_stack_table,
        check_env_coverage,
        check_no_untracked_go_packages,
    ):
        check()

    if not findings:
        print("\nNo drift. Docs and repository agree.")
        return 0

    print(f"\n{len(findings)} finding(s):\n")
    for check, message in findings:
        print(f"  [{check}] {message}")
    print("\nFix the doc or write the task. Do not silence a finding by deleting it.")
    return 1


if __name__ == "__main__":
    sys.exit(main())
