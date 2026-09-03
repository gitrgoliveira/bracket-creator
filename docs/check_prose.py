#!/usr/bin/env python3
"""Check the public docs sources for house-style prose violations.

This runs against the Markdown SOURCES under ``docs/`` (not the built site),
so it catches wording problems before ``mkdocs build`` ever renders them.
Four rules are enforced, each of which the public docs must never contain:

* ``em-dash``: the character U+2014 (an em dash). House style writes short
  sentences instead.
* ``see-link``: a "See [...]" / "See the [...]" / "See also [...]" link. The
  site says "refer to" instead.
* ``internal-id``: an internal bead/tooling reference like ``mp-xxxx`` or
  ``bc-xxxx``. These are internal-tooling identifiers with no meaning to a
  public reader.
* ``mat``: the word "mat"/"mats". Kendo has no mats; the fighting area is a
  shiai-jo (court).

``docs/dev-guide/code_of_conduct.md`` is skipped because it is third-party
text (the Contributor Covenant) that this repo does not control the wording
of. Lines inside fenced code blocks and HTML comments are skipped, since
those are not rendered prose.

Usage:
    python3 docs/check_prose.py [docs_dir]   # default: docs

Exit code: 0 when no file has a violation, 1 when any do.
"""

from __future__ import annotations

import os
import re
import sys

SKIP_FILES = {
    os.path.join("dev-guide", "code_of_conduct.md"),
}

EM_DASH = "—"

SEE_LINK_RE = re.compile(r"\b[Ss]ee (the |also )?\[")
INTERNAL_ID_RE = re.compile(r"\b(mp|bc)-[a-z0-9]{3,4}\b")
MAT_RE = re.compile(r"\bmats?\b", re.IGNORECASE)
CLASS_ATTR_RE = re.compile(r'class="[^"]*"')

# Internal-id matches that are actually CSS class names, not bead references.
INTERNAL_ID_CLASS_ALLOWLIST = {"bc-fig"}


def in_class_attr(line: str, start: int, end: int) -> bool:
    """True when line[start:end] falls inside a class="..." attribute value."""
    for m in CLASS_ATTR_RE.finditer(line):
        if m.start() <= start and end <= m.end():
            return True
    return False


def check_line(line: str) -> list[tuple[str, str]]:
    """Return a list of (rule, excerpt) violations for one line of prose."""
    violations: list[tuple[str, str]] = []

    if EM_DASH in line:
        violations.append(("em-dash", line.strip()))

    if SEE_LINK_RE.search(line):
        violations.append(("see-link", line.strip()))

    for m in INTERNAL_ID_RE.finditer(line):
        token = m.group(0)
        if token in INTERNAL_ID_CLASS_ALLOWLIST:
            continue
        if in_class_attr(line, m.start(), m.end()):
            continue
        violations.append(("internal-id", line.strip()))

    if MAT_RE.search(line):
        violations.append(("mat", line.strip()))

    return violations


def iter_prose_lines(text: str):
    """Yield (lineno, line) for lines that are not fenced code or HTML comments."""
    in_code_block = False
    in_html_comment = False
    for lineno, line in enumerate(text.splitlines(), start=1):
        stripped = line.strip()

        if in_html_comment:
            if "-->" in line:
                in_html_comment = False
                # Anything after the closing marker on the same line is prose.
                after = line.split("-->", 1)[1]
                if after.strip():
                    yield lineno, after
            continue

        if stripped.startswith("```"):
            in_code_block = not in_code_block
            continue

        if in_code_block:
            continue

        if "<!--" in line:
            before, _, rest = line.partition("<!--")
            if "-->" in rest:
                after = rest.split("-->", 1)[1]
                combined = before + after
                if combined.strip():
                    yield lineno, combined
            else:
                in_html_comment = True
                if before.strip():
                    yield lineno, before
            continue

        yield lineno, line


def main() -> int:
    root = sys.argv[1] if len(sys.argv) > 1 else "docs"

    md_files = []
    for dirpath, _dirs, filenames in os.walk(root):
        for name in filenames:
            if not name.endswith(".md"):
                continue
            path = os.path.join(dirpath, name)
            rel = os.path.relpath(path, root)
            if rel in SKIP_FILES:
                continue
            md_files.append(path)

    violations: list[tuple[str, int, str, str]] = []
    for path in sorted(md_files):
        with open(path, encoding="utf-8") as fh:
            text = fh.read()
        for lineno, line in iter_prose_lines(text):
            for rule, excerpt in check_line(line):
                violations.append((path, lineno, rule, excerpt))

    if violations:
        for path, lineno, rule, excerpt in violations:
            print(f"{path}:{lineno}: {rule}: {excerpt}")
        return 1

    print(f"OK: {len(md_files)} files, no prose-rule violations")
    return 0


if __name__ == "__main__":
    sys.exit(main())
