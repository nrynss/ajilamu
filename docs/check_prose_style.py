#!/usr/bin/env python3
"""
Check markdown files in docs/src/content/docs for AGENTS.md style rules:
1. No semicolons.
2. No em dashes (— or –).
3. Sentences run 30 words at most.
"""

import os
import re
import sys
from pathlib import Path

DOCS_DIR = Path(__file__).resolve().parent / "src" / "content" / "docs"

def strip_code_blocks(text):
    # Remove fenced code blocks
    text = re.sub(r"```.*?```", "", text, flags=re.DOTALL)
    # Remove JSX Mermaid components or code props
    text = re.sub(r"<Mermaid\s+code=\{`.*?`\}\s*/>", "", text, flags=re.DOTALL)
    # Remove other JSX tags
    text = re.sub(r"<[^>]+>", "", text)
    # Remove inline code
    text = re.sub(r"`.*?`", "", text)
    # Remove import statements
    text = re.sub(r"^import\s+.*?;?\s*$", "", text, flags=re.MULTILINE)
    # Remove html comments
    text = re.sub(r"<!--.*?-->", "", text, flags=re.DOTALL)
    return text

def check_file(path):
    raw = path.read_text(encoding="utf-8")
    issues = []

    # Strip frontmatter
    body = raw
    if body.startswith("---"):
        parts = body.split("---", 2)
        if len(parts) >= 3:
            body = parts[2]

    clean = strip_code_blocks(body)

    # 1. Check semicolons
    semicolon_lines = []
    for i, line in enumerate(clean.splitlines(), 1):
        if ";" in line:
            semicolon_lines.append((i, line.strip()))
    if semicolon_lines:
        for lno, l in semicolon_lines:
            issues.append(f"Semicolon at line {lno}: {l}")

    # 2. Check em dashes
    dash_lines = []
    for i, line in enumerate(clean.splitlines(), 1):
        if "—" in line or "–" in line:
            dash_lines.append((i, line.strip()))
    if dash_lines:
        for lno, l in dash_lines:
            issues.append(f"Em/en dash at line {lno}: {l}")

    # 3. Check sentence length (> 30 words)
    # Normalize whitespace and headings/bullets
    paragraphs = clean.split("\n\n")
    for para in paragraphs:
        p = para.strip()
        if not p or p.startswith("#") or p.startswith("|"):
            continue
        # Replace list markers
        p = re.sub(r"^[\s*+-]+", "", p, flags=re.MULTILINE)
        p = re.sub(r"^\d+\.\s+", "", p, flags=re.MULTILINE)
        # Split into sentences
        sentences = re.split(r"(?<=[.!?])\s+", p.replace("\n", " "))
        for s in sentences:
            s_clean = s.strip()
            # Remove trailing punctuation
            words = s_clean.split()
            if len(words) > 30:
                issues.append(f"Sentence over 30 words ({len(words)} words): {s_clean[:80]}...")

    return issues

def main():
    has_errors = False
    for path in sorted(DOCS_DIR.rglob("*")):
        if path.suffix in (".md", ".mdx"):
            issues = check_file(path)
            if issues:
                has_errors = True
                print(f"FAIL: {path.relative_to(DOCS_DIR)}")
                for iss in issues:
                    print(f"  - {iss}")
            else:
                print(f"PASS: {path.relative_to(DOCS_DIR)}")

    if has_errors:
        sys.exit(1)
    print("\nAll markdown files obey AGENTS.md rules!")

if __name__ == "__main__":
    main()
