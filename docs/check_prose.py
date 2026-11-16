#!/usr/bin/env python3
"""Check the public docs sources for house-style prose violations.

This runs against the SOURCES (not the built site): the Markdown pages under
``docs/`` and the theme's HTML templates under ``overrides/``, so it catches
wording problems before ``zensical build`` ever renders them. An HTML file is
read for the text between its tags and the attributes a person reads (``alt``,
``title``, ``aria-label``, and ``content`` on a ``<meta>``); its ``<style>``,
``<script>``, ``<pre>`` and code elements, HTML comments, and Jinja
(``{# #}``, ``{{ }}``, ``{% %}``) are skipped. Block tags end a paragraph
there the way a blank line does in Markdown (refer to ``check_html``).
Seven rules are enforced, each of which the public docs must never contain:

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
* ``default-winner``: the same ruling, the noun form -- "default winner(s)"
  -- which ``default-win``'s own word-boundary requirement cannot reach.
* ``wins-by-default``: the same ruling again, the other way to say it --
  naming a side as winning "by default" without the words "default" and
  "win" next to each other.

``docs/dev-guide/code_of_conduct.md`` is skipped because it is third-party
text (the Contributor Covenant) that this repo does not control the wording
of. Lines inside fenced code blocks, HTML comments, and inline code spans are
skipped, since those are not rendered prose; a skipped block always ENDS the
paragraph it interrupts rather than letting the prose before it join the
prose after it (bc-cse) -- a line-based rule never saw the difference, but a
paragraph rule that joined across a skipped block could read two unrelated
sentences as one. HTML tags are stripped before matching, for both the line
rules and the three paragraph rules (bc-cse: before this fix only the line
rules got this treatment, so e.g. ``<img src="shots/default-win.png">`` read
as prose to the paragraph rules alone). Markdown emphasis markers (``*`` and
``_``) are also stripped before the three paragraph rules run, so
``**default** win`` and ``_default_ win`` read exactly as ``default win``
rather than hiding the two words behind punctuation neither gate's character
class names.

The first four rules are checked LINE BY LINE, which is correct for them (an
em dash, a "See [...]" link, an internal id, and "mat" are each self-contained
within one line in every real violation seen). The three default-win rules
are different: Markdown source commonly soft-wraps a sentence across
physical lines (a paragraph renders as one block regardless of where its
source lines break), so "the default" ending one line and "win" starting the
next is one violation a line-by-line check cannot see -- the same class of
miss the sibling JS gate (web-mobile/check-write-result.mjs) fixed for the
same ruling. Those three rules are therefore checked against each PARAGRAPH
(a run of consecutive non-blank prose lines, rejoined with newlines
preserved so a hit's line number can still be computed), never against a
single line alone.

Usage:
    python3 docs/check_prose.py [path ...]   # default: docs

Each path is a ``.md`` or ``.html`` file, or a directory walked for both.
Tests: ``python3 docs/test_check_prose.py``.

Exit code: 0 when no file has a violation, 1 when any do, 2 on a usage
error (a missing path, or a file that is neither .md nor .html).
"""

from __future__ import annotations

import html
import os
import re
import sys
from html.parser import HTMLParser

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

# The default-win trio (operator ruling 2026-10-04), checked against each
# PARAGRAPH instead (see iter_paragraphs below), so a phrase a Markdown
# source wraps across two lines is still caught -- mirrors
# web-mobile/check-write-result.mjs's own default-win rules, widened the
# same way for the same ruling (the Go gate gets the same three too).
#
# The separator in DEFAULT_WIN_RE is REQUIRED (`+`, not `*`), matching the JS
# rule's own reasoning even though prose has no camelCase identifiers to
# protect: the two words running together with NO separator at all
# ("defaultwin") is not a shape an author produces by accident in prose, so
# there is nothing real to catch there, and keeping the gates'
# character classes in the same shape is one less thing to keep in sync by
# hand. One or more of whitespace (a line wrap included -- `\s` matches a
# newline), a quote, a brace, `+`, or a hyphen (the historical "default-win"
# spelling) bridges the two words. The trailing `\b` (bc-cse) requires a
# word boundary right after "win": without it, an HTML attribute run such as
# ``variant="default" winner={w}`` would read the "win" inside "winner" as
# the forbidden word, matching the same fix in the JS and Go gates.
DEFAULT_WIN_RE = re.compile(r"default[\s{}\"'+-]+(?:wins?|loss(?:es)?)\b", re.IGNORECASE)
# "default winner(s)" (bc-cse FIX C): the noun form DEFAULT_WIN_RE's
# trailing \b after "win" cannot reach, because "winner" never satisfies a
# boundary right after "win" (the "n" ending "win" and the "n" opening
# "ner" are both word characters). The separator here is whitespace only,
# deliberately narrower than DEFAULT_WIN_RE's: an HTML attribute run such as
# ``variant="default" winner={w}`` separates the two words with a quote,
# which this class excludes, so that shape still passes here too.
DEFAULT_WINNER_RE = re.compile(r"default\s+winners?\b", re.IGNORECASE)
# The other way to say it without "default" and "win" adjacent: naming a
# side as winning "by default", including the past and participle forms
# ("won by default", "winning by default") a bare `wins?` alternation
# missed. Bounded to 40 characters with no sentence break (a literal ".")
# or Markdown table-row break (a literal "|", bc-cse FIX B) so it cannot
# reach across an unrelated "win" and an unrelated "by default" in two
# different sentences or table cells. The gap is `[^.|]`, not `[^.\n]` or
# `[^.]` (bc-cse): it crosses a real line break, since a Markdown paragraph
# wraps its source lines exactly the way DEFAULT_WIN_RE already accounts
# for, and the 40-character bound (plus "." and "|") was already the thing
# keeping the match inside one sentence/cell -- excluding "\n" as well
# bought nothing but the wrapped shape this fix exists to catch.
#
# Rebalanced to the decision wordings only -- wins, won, winning, loses --
# after "winners?", "lost" and "losing" produced real false positives on
# ordinary settings/connection prose ("Winners per pool is 2 by default.",
# "If the connection is lost, by default the app retries.", "Points lost
# (PL) are hidden by default."): none of those name a kendo decision.
# "winner ... by default" is still caught by DEFAULT_WINNER_RE above when
# the two words sit next to each other, which is the shape that occurs.
#
# "won" excludes its own contraction "won't" without a lookahead (Python's
# re module has none): `\bwon\b` alone also matches inside "won't", because
# the word-to-apostrophe transition is itself a `\b`. The alternative
# instead requires the character right after "won" to be present and NOT
# an apostrophe, a literal "." or a literal "|" (or end of string) --
# excluding "." and "|" too keeps this forced character from swallowing
# the sentence/cell boundary the gap after it exists to stop at.
# "wonder"/"wondering" never reach this branch, since `\bwon\b` does not
# match a "won" that continues into more word characters. `by\s+default`
# (not a literal "by default") lets a wrap land between "by" and
# "default" themselves too (bc-cse FIX A).
WINS_BY_DEFAULT_RE = re.compile(
    r"(?:\b(?:wins?|winning|loses)\b|\bwon\b(?:[^'.|]|$))[^.|]{0,40}\bby\s+default\b",
    re.IGNORECASE,
)

PARAGRAPH_RULES: list[tuple[str, re.Pattern[str]]] = [
    ("default-win", DEFAULT_WIN_RE),
    ("default-winner", DEFAULT_WINNER_RE),
    ("wins-by-default", WINS_BY_DEFAULT_RE),
]

# Inline code spans (single-backtick delimited) and complete HTML comments,
# stripped before prose rules run so code samples and commented-out text
# never trip a rule that only applies to rendered prose.
CODE_SPAN_RE = re.compile(r"`[^`]*`")
COMPLETE_COMMENT_RE = re.compile(r"<!--.*?-->")


def check_line(line: str) -> list[str]:
    """Return the deduplicated list of rule names violated by one prose line."""
    return line_rules(HTML_TAG_RE.sub(" ", line))


def line_rules(text: str) -> list[str]:
    """Return the deduplicated list of line rules violated by text that is
    already plain prose (no markup left to strip)."""
    return [name for name, pattern in RULES if pattern.search(text)]


# Markdown emphasis delimiters. Stripped (not replaced with a space) before
# the three paragraph rules run, so "**default** win" and "_default_ win"
# read as the plain "default win" a reader actually sees, rather than
# hiding the two words behind punctuation none of DEFAULT_WIN_RE's,
# DEFAULT_WINNER_RE's, nor WINS_BY_DEFAULT_RE's character class names.
# Inline code spans are already gone by this point (iter_prose_lines strips
# them before yielding a line), so this cannot eat an underscore inside a
# real identifier such as `snake_case`.
EMPHASIS_RE = re.compile(r"[*_]+")


def _paragraph_prose(line: str) -> str:
    """Normalize one line the way the three PARAGRAPH_RULES need it: HTML tags
    stripped to a space (bc-cse), exactly as check_line does for the line
    rules -- before this fix only check_line got that treatment, so e.g.
    ``<img src="shots/default-win.png">`` read as prose to the paragraph
    rules alone -- plus Markdown emphasis markers removed. Applied PER LINE,
    before iter_paragraphs joins lines into a paragraph: HTML_TAG_RE's
    `[^>]+` would otherwise match across a real line break once lines are
    joined with "\\n" and silently eat it, throwing off every lineno that
    check_paragraphs recovers by counting newlines.
    """
    return EMPHASIS_RE.sub("", HTML_TAG_RE.sub(" ", line))


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
        yield from _paragraph_hits(start, paragraph)


def _paragraph_hits(start: int, paragraph: str):
    """Yield (lineno, rule, excerpt) for every PARAGRAPH_RULES hit in one
    paragraph whose first line is ``start`` and whose lines are joined with
    "\\n", one per source line."""
    for name, pattern in PARAGRAPH_RULES:
        for m in pattern.finditer(paragraph):
            lineno = start + paragraph.count("\n", 0, m.start())
            excerpt = re.sub(r"\s+", " ", m.group(0)).strip()
            yield lineno, name, excerpt


def check_markdown(text: str) -> list[tuple[int, str, str]]:
    """Return every (lineno, rule, excerpt) violation in a Markdown source."""
    hits: list[tuple[int, str, str]] = []
    prose_lines = list(iter_prose_lines(text))
    for lineno, line in prose_lines:
        for rule in check_line(line):
            hits.append((lineno, rule, line.strip()))
    hits.extend(check_paragraphs(prose_lines))
    return hits


# --- HTML -------------------------------------------------------------------
#
# A theme override (overrides/*.html) is a Jinja template whose text a reader
# sees as directly as a Markdown page's, so the same rules apply to it. What a
# person reads there is the text between tags plus a few attributes (an
# image's alt, a tooltip, a screen reader's label, a <meta> description that
# shows up in search results and link previews). Everything else is not
# prose: styles, scripts, preformatted and code text, comments, and Jinja.

# Jinja comments, expressions and statements, masked out BEFORE the HTML is
# parsed, so a "{% if a > b %}" can never close a tag and an expression's
# string literal is never read as text. Each is replaced by spaces with its
# newlines kept, so every later line number still matches the source. The one
# exception is a plain `{{ name }}` that prints a string `set` earlier (below),
# which is replaced by that text, then the newlines the expression spanned.
JINJA_RE = re.compile(r"\{#.*?#\}|\{\{.*?\}\}|\{%.*?%\}", re.DOTALL)

# Elements whose whole content is skipped. `code` (and `kbd`, `samp`) mirror
# the Markdown side, which skips inline code spans, so they are phrasing
# elements too (below): a skipped span leaves the sentence around it whole.
HTML_SKIP_TAGS = {"style", "script", "pre", "code", "kbd", "samp", "template"}

# Phrasing elements that sit inside a run of prose without ending it, so
# "a <strong>default</strong> <em>win</em>" still reads as one phrase, and so
# does one with an <img> or a <button> in the middle of it. Any other tag ends
# the current block, the way a blank line ends a Markdown paragraph, so text
# in two separate <p>s is never read as one sentence.
HTML_INLINE_TAGS = {
    "a", "abbr", "b", "bdi", "bdo", "br", "button", "cite", "data", "del",
    "dfn", "em", "i", "img", "input", "ins", "label", "mark", "output", "q",
    "s", "select", "small", "span", "strong", "sub", "sup", "textarea",
    "time", "u", "var", "wbr", "code", "kbd", "samp",
}

# The see-link rule's HTML shape: text ending "See" (or "See the", "See
# also") with a link opening right after it. The Markdown rule reads the `[`
# of a link; here the link's own tag is what follows the words.
HTML_SEE_LINK_RE = re.compile(r"\b[Ss]ee\s+(?:(?:the|also)\s+)?$")

# Attributes a person reads, on any element; plus `content`, read only on a
# <meta>.
HTML_READ_ATTRS = {"alt", "title", "aria-label"}


# A start tag's name, and one attribute in it (name, then `=` and a quoted or
# bare value when it has one), so an attribute is found by walking the tag and
# a name inside an earlier attribute's quoted value is never mistaken for it.
HTML_TAG_NAME_RE = re.compile(r"<[^\s/>]*")
HTML_ATTR_RE = re.compile(r"""([^\s"'=<>/]+)(?:(\s*=\s*)(?:"[^"]*"|'[^']*'|[^\s"'>]+))?""")

# A `{% set name = "text" %}` whose value is one plain string literal on one
# line, and a `{{ name }}` that prints it, bare or through filters
# (`{{ name | e }}`). The template's own copy (the headline, the lede) is
# written this way, so the printed text is read as if it stood where the
# expression does.
JINJA_SET_RE = re.compile(r"""\{%-?\s*set\s+(\w+)\s*=\s*(?:"([^"\n]*)"|'([^'\n]*)')\s*-?%\}""")
JINJA_NAME_RE = re.compile(r"\{\{-?\s*(\w+)\s*(?:\|[^{}]*?)?-?\}\}")


def _mask(m: re.Match[str]) -> str:
    return re.sub(r"[^\n]", " ", m.group(0))


def _substitute_literals(text: str) -> str:
    """Replace each `{{ name }}` (filters included) that prints a
    `{% set name = "literal" %}` with that literal, escaped so it stays inside
    an attribute, and mask every other Jinja form. A literal holds no newline and
    the expression's own newlines follow it, so line numbers still match the
    source."""
    literals = {
        m.group(1): html.escape(m.group(2) if m.group(2) is not None else m.group(3))
        for m in JINJA_SET_RE.finditer(text)
    }

    def replace(m: re.Match[str]) -> str:
        name = JINJA_NAME_RE.fullmatch(m.group(0))
        if name and name.group(1) in literals:
            return literals[name.group(1)] + "\n" * m.group(0).count("\n")
        return _mask(m)

    return JINJA_RE.sub(replace, text)


class _ProseHTMLParser(HTMLParser):
    """Collect the person-read text of an HTML document as blocks, each a
    (start_lineno, text) pair whose text holds one "\\n" per source line, so
    a hit's line can be recovered by counting newlines as for Markdown."""

    def __init__(self) -> None:
        super().__init__(convert_charrefs=True)
        self.blocks: list[tuple[int, str]] = []
        self._skip_depth = 0
        self._start: int | None = None
        self._lines: list[str] = []
        # Hits the parser finds itself, which the text of a block cannot show.
        self.hits: list[tuple[int, str, str]] = []

    def _attr_line(self, tag_line: int, name: str) -> int:
        """The source line an attribute's value starts on: the tag may wrap, so
        its own start line is not where a hit in an attribute sits."""
        raw = self.get_starttag_text() or ""
        for m in HTML_ATTR_RE.finditer(raw, HTML_TAG_NAME_RE.match(raw).end()):
            if m.group(1).lower() == name:
                return tag_line + raw.count("\n", 0, m.end(2) if m.group(2) else m.start())
        return tag_line

    def _add(self, lineno: int, text: str) -> None:
        for i, piece in enumerate(text.split("\n")):
            line = lineno + i
            if self._start is None:
                if not piece.strip():
                    continue
                self._start = line
                self._lines = [""]
            while self._start + len(self._lines) - 1 < line:
                self._lines.append("")
            self._lines[-1] += piece

    def _flush(self) -> None:
        if self._start is not None:
            self.blocks.append((self._start, "\n".join(self._lines)))
        self._start = None
        self._lines = []

    def handle_starttag(self, tag, attrs):
        lineno = self.getpos()[0]
        if tag not in HTML_INLINE_TAGS:
            self._flush()
        if tag in HTML_SKIP_TAGS:
            self._skip_depth += 1
            return
        if self._skip_depth:
            return
        if tag == "a":
            m = HTML_SEE_LINK_RE.search("\n".join(self._lines)) if self._start is not None else None
            if m:
                self.hits.append((lineno, "see-link", re.sub(r"\s+", " ", m.group(0)) + "<a>"))
        for name, value in attrs:
            if value and (name in HTML_READ_ATTRS or (tag == "meta" and name == "content")):
                # An attribute is its own block: alt text never joins the
                # prose around the image into one sentence.
                saved = (self._start, self._lines)
                self._start, self._lines = None, []
                self._add(self._attr_line(lineno, name), value)
                self._flush()
                self._start, self._lines = saved

    def handle_endtag(self, tag):
        if tag in HTML_SKIP_TAGS:
            self._skip_depth = max(0, self._skip_depth - 1)
        if tag not in HTML_INLINE_TAGS:
            self._flush()

    def handle_data(self, data):
        if not self._skip_depth:
            self._add(self.getpos()[0], data)

    def close(self) -> None:
        super().close()
        self._flush()


def check_html(text: str) -> list[tuple[int, str, str]]:
    """Return every (lineno, rule, excerpt) violation in an HTML (or Jinja
    template) source: the line rules on each line of each block of text,
    the paragraph rules on each block as a whole."""
    parser = _ProseHTMLParser()
    parser.feed(_substitute_literals(text))
    parser.close()

    hits = list(parser.hits)
    for start, block in parser.blocks:
        lines = block.split("\n")
        for offset, line in enumerate(lines):
            for rule in line_rules(line):
                hits.append((start + offset, rule, line.strip()))
        # The parser already decoded entities, so an "&lt;mat&gt;" is visible
        # text here and no tag is left to strip.
        hits.extend(_paragraph_hits(start, "\n".join(EMPHASIS_RE.sub("", line) for line in lines)))
    return hits


CHECKERS = {".md": check_markdown, ".html": check_html}


def collect_files(paths: list[str]) -> list[str]:
    """Expand each path: a file is checked as given, a directory is walked for
    its .md and .html files (SKIP_FILES, relative to it, left out). Raises
    ValueError naming the first path that is neither."""
    files: list[str] = []
    for root in paths:
        if os.path.isfile(root):
            if os.path.splitext(root)[1] not in CHECKERS:
                raise ValueError(f"not a .md or .html file: {root}")
            files.append(root)
            continue
        if not os.path.isdir(root):
            raise ValueError(f"path not found: {root}")
        for dirpath, _dirs, filenames in os.walk(root):
            for name in filenames:
                if os.path.splitext(name)[1] not in CHECKERS:
                    continue
                path = os.path.join(dirpath, name)
                if os.path.relpath(path, root) in SKIP_FILES:
                    continue
                files.append(path)
    return sorted(set(files))


def main(argv: list[str] | None = None) -> int:
    paths = (sys.argv[1:] if argv is None else argv) or ["docs"]
    try:
        files = collect_files(paths)
    except ValueError as err:
        print(
            f"error: {err}\nusage: python3 docs/check_prose.py [path ...]",
            file=sys.stderr,
        )
        return 2

    bad = False
    for path in files:
        with open(path, encoding="utf-8") as fh:
            text = fh.read()
        for lineno, rule, excerpt in CHECKERS[os.path.splitext(path)[1]](text):
            bad = True
            print(f"{path}:{lineno}: {rule}: {excerpt}")

    if bad:
        return 1

    print(f"OK: {len(files)} files, no prose-rule violations")
    return 0


if __name__ == "__main__":
    sys.exit(main())
