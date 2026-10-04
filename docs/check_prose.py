#!/usr/bin/env python3
"""Check the public docs sources for house-style prose violations.

This runs against the Markdown SOURCES under ``docs/`` (not the built site),
so it catches wording problems before ``mkdocs build`` ever renders them.
Six rules are enforced, each of which the public docs must never contain:

* ``em-dash``: the character U+2014 (an em dash). House style writes short
  sentences instead.
* ``see-link``: a "See [...]" / "See the [...]" / "See also [...]" link. The
  site says "refer to" instead.
* ``internal-id``: an internal issue-tracker ID like ``mp-xxxx`` or
  ``bc-xxxx``. These are internal-tooling identifiers with no meaning to a
  public reader.
* ``mat``: the word "mat"/"mats"/"matside". Kendo has no mats; the fighting area
  is a shiai-jo (court).
* ``default-win``: the phrase "default win" (or "default-win"). Kendo has no
  such term (operator ruling 2026-10-04): every finished match has a result,
  a scoreline or a registered decision that names the winner -- name the
  recorded decision instead (kiken, fusenpai, fusensho).
* ``wins-by-default``: the same ruling, the other way to say it -- naming a
  side as winning "by default" without the words "default" and "win" next
  to each other.

``docs/dev-guide/code_of_conduct.md`` is skipped because it is third-party
text (the Contributor Covenant) that this repo does not control the wording
of. Lines inside fenced code blocks, HTML comments, and inline code spans are
skipped, since those are not rendered prose; a skipped block always ENDS the
paragraph it interrupts rather than letting the prose before it join the
prose after it (bc-cse) -- a line-based rule never saw the difference, but a
paragraph rule that joined across a skipped block could read two unrelated
sentences as one. HTML tags are stripped before matching, for both the line
rules and the two paragraph rules (bc-cse: before this fix only the line
rules got this treatment, so e.g. ``<img src="shots/default-win.png">`` read
as prose to the paragraph rules alone). Markdown emphasis markers (``*`` and
``_``) are also stripped before the two paragraph rules run, so
``**default** win`` and ``_default_ win`` read exactly as ``default win``
rather than hiding the two words behind punctuation neither gate's character
class names.

The first four rules are checked LINE BY LINE, which is correct for them (an
em dash, a "See [...]" link, an internal id, and "mat" are each self-contained
within one line in every real violation seen). The two default-win rules are
different: Markdown source commonly soft-wraps a sentence across physical
lines (a paragraph renders as one block regardless of where its source lines
break), so "the default" ending one line and "win" starting the next is one
violation a line-by-line check cannot see -- the same class of miss the
sibling JS gate (web-mobile/check-write-result.mjs) fixed for the same
ruling. Those two rules are therefore checked against each PARAGRAPH (a run
of consecutive non-blank prose lines, rejoined with newlines preserved so a
hit's line number can still be computed), never against a single line alone.

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

SEE_LINK_RE = re.compile(r"\b[Ss]ee (the |also )?\[")
INTERNAL_ID_RE = re.compile(r"\b(mp|bc)-[a-z0-9]{3,4}\b")
MAT_RE = re.compile(r"\bmat(s|side)?\b", re.IGNORECASE)
HTML_TAG_RE = re.compile(r"<[^>]+>")

# (rule name, pattern) pairs checked against each prose LINE, in report order.
RULES: list[tuple[str, re.Pattern[str]]] = [
    ("em-dash", re.compile("—")),  # U+2014; house style writes short sentences instead
    ("see-link", SEE_LINK_RE),
    ("internal-id", INTERNAL_ID_RE),
    ("mat", MAT_RE),
]

# The default-win pair (operator ruling 2026-10-04), checked against each
# PARAGRAPH instead (see iter_paragraphs below), so a phrase a Markdown
# source wraps across two lines is still caught -- mirrors
# web-mobile/check-write-result.mjs's own default-win rules, widened the
# same way for the same ruling (the Go gate gets the same pair too).
#
# The separator in DEFAULT_WIN_RE is REQUIRED (`+`, not `*`), matching the JS
# rule's own reasoning even though prose has no camelCase identifiers to
# protect: the two words running together with NO separator at all
# ("defaultwin") is not a shape an author produces by accident in prose, so
# there is nothing real to catch there, and keeping the two gates'
# character classes in the same shape is one less thing to keep in sync by
# hand. One or more of whitespace (a line wrap included -- `\s` matches a
# newline), a quote, a brace, `+`, or a hyphen (the historical "default-win"
# spelling) bridges the two words. The trailing `\b` (bc-cse) requires a
# word boundary right after "win": without it, an HTML attribute run such as
# ``variant="default" winner={w}`` would read the "win" inside "winner" as
# the forbidden word, matching the same fix in the JS and Go gates.
DEFAULT_WIN_RE = re.compile(r"default[\s{}\"'+-]+(?:wins?|loss(?:es)?)\b", re.IGNORECASE)
# The other way to say it without "default" and "win" adjacent: naming a
# side as winning "by default", including the past and participle forms
# ("won by default", "winning by default") a bare `wins?` alternation
# missed. Bounded to 40 characters with no sentence break (a literal ".")
# so it cannot reach across an unrelated "win" and an unrelated "by
# default" in two different sentences. The gap is `[^.]`, not `[^.\n]`
# (bc-cse): it now crosses a real line break, since a Markdown paragraph
# wraps its source lines exactly the way the default-win rule above already
# accounts for, and the 40-character bound (plus the literal ".") was
# already the thing keeping the match inside one sentence -- excluding "\n"
# as well bought nothing but the wrapped shape this fix exists to catch.
WINS_BY_DEFAULT_RE = re.compile(r"\b(?:wins?|won|winning|winners?|loses|lost|losing)\b[^.]{0,40}\bby default\b", re.IGNORECASE)

PARAGRAPH_RULES: list[tuple[str, re.Pattern[str]]] = [
    ("default-win", DEFAULT_WIN_RE),
    ("wins-by-default", WINS_BY_DEFAULT_RE),
]

# Inline code spans (single-backtick delimited) and complete HTML comments,
# stripped before prose rules run so code samples and commented-out text
# never trip a rule that only applies to rendered prose.
CODE_SPAN_RE = re.compile(r"`[^`]*`")
COMPLETE_COMMENT_RE = re.compile(r"<!--.*?-->")


def check_line(line: str) -> list[str]:
    """Return the deduplicated list of rule names violated by one prose line."""
    text = HTML_TAG_RE.sub(" ", line)
    rules: list[str] = []

    for name, pattern in RULES:
        if pattern.search(text):
            rules.append(name)

    return rules


# Markdown emphasis delimiters. Stripped (not replaced with a space) before
# the two paragraph rules run, so "**default** win" and "_default_ win" read
# as the plain "default win" a reader actually sees, rather than hiding the
# two words behind punctuation neither DEFAULT_WIN_RE's nor
# WINS_BY_DEFAULT_RE's character class names. Inline code spans are already
# gone by this point (iter_prose_lines strips them before yielding a line),
# so this cannot eat an underscore inside a real identifier such as
# `snake_case`.
EMPHASIS_RE = re.compile(r"[*_]+")


def _paragraph_prose(line: str) -> str:
    """Normalize one line the way the two PARAGRAPH_RULES need it: HTML tags
    stripped to a space (bc-cse), exactly as check_line does for the line
    rules -- before this fix only check_line got that treatment, so e.g.
    ``<img src="shots/default-win.png">`` read as prose to the paragraph
    rules alone -- plus Markdown emphasis markers removed. Applied PER LINE,
    before iter_paragraphs joins lines into a paragraph: HTML_TAG_RE's
    `[^>]+` would otherwise match across a real line break once lines are
    joined with "\\n" and silently eat it, throwing off every lineno that
    check_paragraphs recovers by counting newlines.
    """
    text = HTML_TAG_RE.sub(" ", line)
    return EMPHASIS_RE.sub("", text)


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

    A line this function skips (fence open/body/close, an HTML comment's
    interior) is yielded as an EMPTY line rather than omitted outright
    (bc-cse): check_line("") trips nothing, so the four line rules are
    unaffected, but iter_paragraphs treats a blank line as the end of a
    paragraph, so a skipped block always breaks the paragraph around it
    instead of letting prose before it run on into prose after it.
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
            yield lineno, ""
            continue

        ch, run = _fence_marker(stripped)
        if ch is not None:
            fence_char, fence_len = ch, run
            yield lineno, ""
            continue

        if in_html_comment:
            idx = line.find("-->")
            if idx == -1:
                yield lineno, ""
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


def iter_paragraphs(prose_lines: list[tuple[int, str]]):
    """Group consecutive non-blank prose lines into paragraphs, each yielded
    as (start_lineno, joined_text), the lines rejoined with "\\n" (not
    collapsed to a space) so a hit's line can still be recovered by counting
    the newlines before its match start.

    A blank line (empty after stripping) ends the current paragraph. This
    runs over the lines iter_prose_lines already yielded -- fenced code,
    HTML comments and inline code spans are already gone -- so a paragraph
    here is exactly a CommonMark paragraph: one block that renders as
    continuous prose regardless of where its source lines break, which is
    the reason DEFAULT_WIN_RE and WINS_BY_DEFAULT_RE need this instead of
    the plain per-line check every other rule uses.
    """
    buf: list[str] = []
    start: int | None = None
    for lineno, line in prose_lines:
        if line.strip() == "":
            if buf:
                yield start, "\n".join(buf)
                buf = []
                start = None
            continue
        if not buf:
            start = lineno
        buf.append(line)
    if buf:
        yield start, "\n".join(buf)


def check_paragraphs(prose_lines: list[tuple[int, str]]):
    """Yield (lineno, rule, excerpt) for every PARAGRAPH_RULES hit, lineno
    being where the match itself starts (not merely the paragraph's first
    line), excerpt the matched text with internal whitespace collapsed to a
    single space so a wrapped hit still prints as one readable line.

    Each line is run through _paragraph_prose (HTML tags and Markdown
    emphasis markers stripped) BEFORE iter_paragraphs joins it with its
    neighbours (bc-cse), so an HTML tag's own "<"/">" can never be mistaken
    for a line break once lines are joined with "\\n", and lineno stays a
    correct count of real newlines.
    """
    normalized = [(lineno, _paragraph_prose(line)) for lineno, line in prose_lines]
    for start, paragraph in iter_paragraphs(normalized):
        for name, pattern in PARAGRAPH_RULES:
            for m in pattern.finditer(paragraph):
                lineno = start + paragraph.count("\n", 0, m.start())
                excerpt = re.sub(r"\s+", " ", m.group(0)).strip()
                yield lineno, name, excerpt


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
        prose_lines = list(iter_prose_lines(text))
        for lineno, line in prose_lines:
            for rule in check_line(line):
                bad = True
                print(f"{path}:{lineno}: {rule}: {line.strip()}")
        for lineno, rule, excerpt in check_paragraphs(prose_lines):
            bad = True
            print(f"{path}:{lineno}: {rule}: {excerpt}")

    if bad:
        return 1

    print(f"OK: {len(md_files)} files, no prose-rule violations")
    return 0


if __name__ == "__main__":
    sys.exit(main())
