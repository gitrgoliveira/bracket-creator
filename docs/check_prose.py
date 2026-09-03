#!/usr/bin/env python3
"""Check the public docs sources for house-style prose violations.

This runs against the Markdown SOURCES under ``docs/`` (not the built site),
so it catches wording problems before ``mkdocs build`` ever renders them.
Four rules are enforced, each of which the public docs must never contain:

* ``em-dash``: the character U+2014 (an em dash). House style writes short
  sentences instead.
* ``see-link``: a "See [...]" / "See the [...]" / "See also [...]" link. The
  site says "refer to" instead.
* ``internal-id``: an internal issue-tracker ID like ``mp-xxxx`` or
  ``bc-xxxx``. These are internal-tooling identifiers with no meaning to a
  public reader.
* ``mat``: the word "mat"/"mats". Kendo has no mats; the fighting area is a
  shiai-jo (court).

``docs/dev-guide/code_of_conduct.md`` is skipped because it is third-party
text (the Contributor Covenant) that this repo does not control the wording
of. Lines inside fenced code blocks, HTML comments, and inline code spans are
skipped, since those are not rendered prose. HTML tags are stripped before
matching, so an attribute (e.g. a CSS class) can never be misread as prose.

Usage:
    python3 docs/check_prose.py [docs_dir]   # default: docs

Exit code: 0 when no file has a violation, 1 when any do, 2 on a usage
error (missing docs dir).
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
HTML_TAG_RE = re.compile(r"<[^>]+>")

# Inline code spans (single-backtick delimited) and complete HTML comments,
# stripped before prose rules run so code samples and commented-out text
# never trip a rule that only applies to rendered prose.
CODE_SPAN_RE = re.compile(r"`[^`]*`")
COMPLETE_COMMENT_RE = re.compile(r"<!--.*?-->")


def check_line(line: str) -> list[str]:
    """Return the deduplicated list of rule names violated by one prose line."""
    text = HTML_TAG_RE.sub(" ", line)
    rules: list[str] = []

    if EM_DASH in text:
        rules.append("em-dash")

    if SEE_LINK_RE.search(text):
        rules.append("see-link")

    if INTERNAL_ID_RE.search(text):
        rules.append("internal-id")

    if MAT_RE.search(text):
        rules.append("mat")

    return rules


def _fence_marker(stripped: str) -> tuple[str | None, int]:
    """Return (char, run_length) when stripped opens a fence (three or more
    backticks or tildes at the start of the line), else (None, 0)."""
    if not stripped:
        return None, 0
    ch = stripped[0]
    if ch not in ("`", "~"):
        return None, 0
    run = 0
    while run < len(stripped) and stripped[run] == ch:
        run += 1
    if run < 3:
        return None, 0
    return ch, run


def iter_prose_lines(text: str):
    """Yield (lineno, line) for prose text, with fenced code blocks, HTML
    comments, and inline code spans stripped out.

    Fence handling mirrors CommonMark: a line whose stripped form starts with
    three or more backticks or tildes opens a fence, recording the character
    and run length; only a later line starting with that same character
    repeated at least that many times closes it. Everything between is
    skipped verbatim, including anything that looks like a comment or a rule
    violation.

    HTML comment handling runs per line, after inline code spans are removed
    (so a code span containing literal "<!--" text, e.g. `` `<!--` ``, never
    opens comment state): any complete ``<!-- ... -->`` segments are dropped;
    if an unterminated ``<!--`` remains, the prose before it is still checked
    and the rest of the file is skipped until a later line's ``-->`` closes
    it, after which the remainder of THAT line is checked normally.
    """
    fence_char: str | None = None
    fence_len = 0
    in_html_comment = False

    for lineno, line in enumerate(text.splitlines(), start=1):
        stripped = line.strip()

        if fence_char is not None:
            if stripped.startswith(fence_char * fence_len):
                fence_char = None
                fence_len = 0
            continue

        ch, run = _fence_marker(stripped)
        if ch is not None:
            fence_char, fence_len = ch, run
            continue

        if in_html_comment:
            idx = line.find("-->")
            if idx == -1:
                continue
            in_html_comment = False
            line = line[idx + 3 :]

        line = CODE_SPAN_RE.sub("", line)
        line = COMPLETE_COMMENT_RE.sub("", line)

        if "<!--" in line:
            prose_before, _, _ = line.partition("<!--")
            in_html_comment = True
            yield lineno, prose_before
            continue

        yield lineno, line


def main() -> int:
    root = sys.argv[1] if len(sys.argv) > 1 else "docs"
    if not os.path.isdir(root):
        print(
            f"error: docs dir not found: {root}\n"
            "usage: python3 docs/check_prose.py [docs_dir]",
            file=sys.stderr,
        )
        return 2

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
