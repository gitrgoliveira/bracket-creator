#!/usr/bin/env python3
"""Tests for docs/check_prose.py (stdlib unittest, no venv needed).

Run with ``python3 docs/test_check_prose.py`` (what ``make docs/prose`` does)
or ``python3 -m unittest discover -s docs -p 'test_check_prose.py'``.
"""

from __future__ import annotations

import contextlib
import io
import os
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import check_prose  # noqa: E402

EM = "—"

# Every forbidden word sits once in the text and once in an alt, which must be
# caught, and again in a style block, a script, a comment, a <pre> and each
# Jinja form, which must be ignored.
HTML_FIXTURE = f"""{{% extends "main.html" %}}
{{# a Jinja comment: default win, mat {EM} #}}
<!doctype html>
<html>
<head>
<title>Live brackets</title>
<style>
  .hero::after {{ content: "{EM} default win on the mat"; }}
</style>
<script>
  const note = "default win {EM} mat";
</script>
</head>
<body>
<!-- an HTML comment: default win, mat {EM} -->
<p>Prose with an em dash {EM} here.</p>
<p>The court is a mat.</p>
<p>A default win.</p>
<img src="a.png" alt="hero {EM} shot">
<img src="b.png" alt="the mat">
<img src="c.png" alt="a default win">
<pre>default win on the mat {EM}</pre>
{{{{ "default win, mat {EM}" }}}}
{{% set note = "default win > mat {EM}" %}}
</body>
</html>
"""


def rules_by_line(hits):
    return sorted((lineno, rule) for lineno, rule, _excerpt in hits)


class HtmlFixtureTest(unittest.TestCase):
    def setUp(self):
        self.hits = check_prose.check_html(HTML_FIXTURE)
        self.lines = HTML_FIXTURE.splitlines()

    def line_of(self, needle):
        for i, line in enumerate(self.lines, start=1):
            if needle in line:
                return i
        raise AssertionError(f"fixture has no line with {needle!r}")

    def test_text_violations_are_caught(self):
        got = rules_by_line(self.hits)
        self.assertIn((self.line_of("Prose with an em dash"), "em-dash"), got)
        self.assertIn((self.line_of("The court is a mat."), "mat"), got)
        self.assertIn((self.line_of("<p>A default win.</p>"), "default-win"), got)

    def test_alt_violations_are_caught(self):
        got = rules_by_line(self.hits)
        self.assertIn((self.line_of('alt="hero'), "em-dash"), got)
        self.assertIn((self.line_of('alt="the mat"'), "mat"), got)
        self.assertIn((self.line_of('alt="a default win"'), "default-win"), got)

    def test_style_script_comment_pre_and_jinja_are_ignored(self):
        caught = {
            self.line_of("Prose with an em dash"),
            self.line_of("The court is a mat."),
            self.line_of("<p>A default win.</p>"),
            self.line_of('alt="hero'),
            self.line_of('alt="the mat"'),
            self.line_of('alt="a default win"'),
        }
        stray = [h for h in self.hits if h[0] not in caught]
        self.assertEqual(stray, [])

    def test_exactly_six_hits(self):
        self.assertEqual(len(self.hits), 6, self.hits)


class HtmlRulesTest(unittest.TestCase):
    def test_person_read_attributes(self):
        html = (
            '<a href="#" title="the mat">x</a>\n'
            '<button aria-label="a default win">y</button>\n'
            '<meta name="description" content="wins by default">\n'
            '<div data-note="the mat" class="mat">z</div>\n'
        )
        self.assertEqual(
            rules_by_line(check_prose.check_html(html)),
            [(1, "mat"), (2, "default-win"), (3, "wins-by-default")],
        )

    def test_content_counts_only_on_meta(self):
        self.assertEqual(check_prose.check_html('<div content="the mat"></div>'), [])

    def test_entity_em_dash_is_caught(self):
        self.assertEqual(rules_by_line(check_prose.check_html("<p>a &mdash; b</p>")), [(1, "em-dash")])

    def test_wrapped_phrase_inside_one_block_is_caught(self):
        html = "<p>\n  A side gets a default\n  win here.\n</p>\n"
        self.assertEqual(rules_by_line(check_prose.check_html(html)), [(2, "default-win")])

    def test_inline_tags_do_not_split_a_phrase(self):
        html = "<p>a <strong>default</strong> <em>win</em></p>"
        self.assertEqual(rules_by_line(check_prose.check_html(html)), [(1, "default-win")])

    def test_block_tags_split_paragraphs(self):
        html = "<p>the default</p>\n<p>win is not a phrase here</p>\n"
        self.assertEqual(check_prose.check_html(html), [])

    def test_code_element_is_ignored(self):
        self.assertEqual(check_prose.check_html("<p>Run <code>--mat</code> now</p>"), [])

    def test_jinja_with_angle_bracket_does_not_open_a_tag(self):
        html = "{% if a > b %}<p>the mat</p>{% endif %}"
        self.assertEqual(rules_by_line(check_prose.check_html(html)), [(1, "mat")])

    def test_multiline_jinja_and_comment_keep_line_numbers(self):
        html = "{#\nnote\n#}\n<!--\nx\n-->\n<p>the mat</p>\n"
        self.assertEqual(rules_by_line(check_prose.check_html(html)), [(7, "mat")])

    def test_clean_html_has_no_hits(self):
        self.assertEqual(check_prose.check_html("<p>Every bout on every court.</p>"), [])


class MarkdownUnchangedTest(unittest.TestCase):
    def test_wrapped_default_win_in_markdown(self):
        self.assertEqual(rules_by_line(check_prose.check_markdown("A default\nwin.\n")), [(1, "default-win")])

    def test_fenced_code_is_skipped(self):
        self.assertEqual(check_prose.check_markdown("```\nthe mat\n```\n"), [])


class MainTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = self.tmp.name

    def tearDown(self):
        self.tmp.cleanup()

    def write(self, rel, text):
        path = os.path.join(self.root, rel)
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "w", encoding="utf-8") as fh:
            fh.write(text)
        return path

    def run_main(self, *args):
        out, err = io.StringIO(), io.StringIO()
        with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
            code = check_prose.main(list(args))
        return code, out.getvalue(), err.getvalue()

    def test_directory_walk_reads_html_and_markdown(self):
        md = self.write("docs/page.md", "The mat.\n")
        html = self.write("overrides/landing.html", "<p>the mat</p>\n")
        code, out, _ = self.run_main(os.path.join(self.root, "docs"), os.path.join(self.root, "overrides"))
        self.assertEqual(code, 1)
        self.assertIn(f"{md}:1: mat:", out)
        self.assertIn(f"{html}:1: mat:", out)

    def test_file_argument(self):
        good = self.write("ok.html", "<p>Every court.</p>\n")
        code, out, _ = self.run_main(good)
        self.assertEqual(code, 0)
        self.assertIn("OK: 1 files", out)
        bad = self.write("bad.md", f"a {EM} b\n")
        code, out, _ = self.run_main(good, bad)
        self.assertEqual(code, 1)
        self.assertIn(f"{bad}:1: em-dash:", out)

    def test_code_of_conduct_skipped_in_a_walk(self):
        self.write("docs/dev-guide/code_of_conduct.md", "the mat\n")
        code, out, _ = self.run_main(os.path.join(self.root, "docs"))
        self.assertEqual(code, 0, out)

    def test_other_files_in_a_walk_are_ignored(self):
        self.write("docs/script.py", "# the mat\n")
        code, _, _ = self.run_main(os.path.join(self.root, "docs"))
        self.assertEqual(code, 0)

    def test_missing_path_is_a_usage_error(self):
        code, _, err = self.run_main(os.path.join(self.root, "nope"))
        self.assertEqual(code, 2)
        self.assertIn("not found", err)

    def test_unsupported_file_is_a_usage_error(self):
        path = self.write("notes.txt", "x\n")
        code, _, err = self.run_main(path)
        self.assertEqual(code, 2)
        self.assertIn("not a .md or .html file", err)


if __name__ == "__main__":
    unittest.main()
