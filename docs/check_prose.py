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

# Internal-id matches that are actually CSS class names, not bead references.
INTERNAL_ID_CLASS_ALLOWLIST = {"bc-fig"}


def check_line(line: str) -> list[str]:
    """Return the deduplicated list of rule names violated by one prose line."""
    rules: list[str] = []

    if EM_DASH in line:
        rules.append("em-dash")

    if SEE_LINK_RE.search(line):
        rules.append("see-link")

    for m in INTERNAL_ID_RE.finditer(line):
        if m.group(0) in INTERNAL_ID_CLASS_ALLOWLIST:
            continue
        if "internal-id" not in rules:
            rules.append("internal-id")

    if MAT_RE.search(line):
        rules.append("mat")

    return rules


def iter_prose_lines(text: str):
    """Yield (lineno, line) for lines that are not fenced code or HTML comments."""
    in_code_block = False
    in_html_comment = False
    for lineno, line in enumerate(text.splitlines(), start=1):
        stripped = line.strip()

        if in_html_comment:
            if "-->" in line:
                in_html_comment = False
            continue

        if stripped.startswith("```"):
            in_code_block = not in_code_block
            continue

        if in_code_block:
            continue

        if "<!--" in line:
            if "-->" not in line:
                in_html_comment = True
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

    bad = False
    for path in sorted(md_files):
        with open(path, encoding="utf-8") as fh:
            text = fh.read()
        for lineno, line in iter_prose_lines(text):
            for rule in check_line(line):
                bad = True
                print(f"{path}:{lineno}: {rule}: {line.strip()}")

    if bad:
        return 1

    print(f"OK: {len(md_files)} files, no prose-rule violations")
    return 0


if __name__ == "__main__":
    sys.exit(main())
