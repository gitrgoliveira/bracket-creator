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

    def test_skip_tag_own_attributes_are_read_but_not_its_contents(self):
        html = (
            '<code title="the mat">--mat</code>\n'
            '<pre aria-label="a default win">the mat</pre>\n'
            '<pre><code title="the mat">x</code></pre>\n'
        )
        self.assertEqual(
            rules_by_line(check_prose.check_html(html)),
            [(1, "mat"), (2, "default-win")],
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

    def test_code_kbd_samp_inside_a_phrase_do_not_split_it(self):
        for tag in ("code", "kbd", "samp"):
            html = f"<p>A side wins <{tag}>2-0</{tag}> by default.</p>"
            self.assertEqual(rules_by_line(check_prose.check_html(html)), [(1, "wins-by-default")], tag)

    def test_escaped_angle_brackets_are_visible_text(self):
        html = "<p>Do not write &lt;mat&gt; or &lt;default win&gt;.</p>"
        self.assertEqual(rules_by_line(check_prose.check_html(html)), [(1, "default-win"), (1, "mat")])

    def test_multiline_printed_literal_keeps_line_numbers(self):
        html = '{% set lede = "fine" %}<p>{{\n lede\n}}</p>\n<p>the mat</p>\n'
        self.assertEqual(rules_by_line(check_prose.check_html(html)), [(4, "mat")])

    def test_jinja_with_angle_bracket_does_not_open_a_tag(self):
        html = "{% if a > b %}<p>the mat</p>{% endif %}"
        self.assertEqual(rules_by_line(check_prose.check_html(html)), [(1, "mat")])

    def test_multiline_jinja_and_comment_keep_line_numbers(self):
        html = "{#\nnote\n#}\n<!--\nx\n-->\n<p>the mat</p>\n"
        self.assertEqual(rules_by_line(check_prose.check_html(html)), [(7, "mat")])

    def test_printed_set_literal_is_read_where_it_prints(self):
        html = (
            '{%- set lede = "Score the mat live." -%}\n'
            "{%- set title = 'A default win' -%}\n"
            '<meta name="description" content="{{ lede }}">\n'
            "<h1>{{ title }}</h1>\n"
            "<p>{{ other }}</p>\n"
        )
        self.assertEqual(
            rules_by_line(check_prose.check_html(html)),
            [(3, "mat"), (4, "default-win")],
        )

    def test_set_literal_that_is_never_printed_is_ignored(self):
        self.assertEqual(check_prose.check_html('{% set note = "the mat" %}\n<p>ok</p>'), [])

    def test_filtered_print_of_a_set_literal_is_read(self):
        html = (
            '{%- set lede = "Score the mat live." -%}\n'
            "<p>{{ lede | e }}</p>\n"
            "<p>{{ lede|safe }}</p>\n"
            "<p>{{ lede if x else y }}</p>\n"
        )
        self.assertEqual(
            rules_by_line(check_prose.check_html(html)),
            [(2, "mat"), (3, "mat")],
        )

    def test_see_link_in_html_is_caught(self):
        for html in (
            '<p>See <a href="x">the guide</a> first.</p>',
            '<p>see the <a href="x">guide</a>.</p>',
            '<p>See also\n<a href="x">the guide</a>.</p>',
        ):
            self.assertEqual([r for _l, r, _e in check_prose.check_html(html)], ["see-link"], html)

    def test_see_link_hit_is_on_the_line_of_the_link(self):
        html = '<p>\n  See\n  <a href="x">the guide</a>.\n</p>\n'
        self.assertEqual(rules_by_line(check_prose.check_html(html)), [(3, "see-link")])

    def test_see_without_a_link_is_clean(self):
        for html in (
            '<p>See the guide <a href="x">here</a>.</p>',
            '<p>See</p><p><a href="x">the guide</a></p>',
        ):
            self.assertEqual(check_prose.check_html(html), [], html)

    def test_phrasing_elements_do_not_split_a_phrase(self):
        for tag in ('<img src="a.png" alt="">', "<button></button>", "<label></label>",
                    "<input>", "<select></select>", "<textarea></textarea>"):
            html = f"<p>a default {tag} win</p>"
            self.assertEqual(rules_by_line(check_prose.check_html(html)), [(1, "default-win")], tag)

    def test_attribute_hit_is_on_the_line_of_the_attribute(self):
        html = '<p>ok</p>\n<img\n  src="a.png"\n  alt="the mat"\n  loading="lazy">\n'
        self.assertEqual(rules_by_line(check_prose.check_html(html)), [(4, "mat")])

    def test_attribute_name_inside_an_earlier_value_is_not_the_attribute(self):
        html = '<a href="https://x.com?title=1"\n  title="the mat">x</a>'
        self.assertEqual(rules_by_line(check_prose.check_html(html)), [(2, "mat")])

    def test_attribute_value_on_the_next_line_is_found(self):
        html = '<img src="a.png" alt=\n"the mat">'
        self.assertEqual(rules_by_line(check_prose.check_html(html)), [(2, "mat")])

    def test_clean_html_has_no_hits(self):
        self.assertEqual(check_prose.check_html("<p>Every bout on every court.</p>"), [])


class MarkdownUnchangedTest(unittest.TestCase):
    def test_wrapped_default_win_in_markdown(self):
        self.assertEqual(rules_by_line(check_prose.check_markdown("A default\nwin.\n")), [(1, "default-win")])

    def test_fenced_code_is_skipped(self):
        self.assertEqual(check_prose.check_markdown("```\nthe mat\n```\n"), [])


def words(n: int) -> str:
    """A sentence of exactly n words."""
    return " ".join(["Word"] + ["word"] * (n - 1)) + "."


class SentenceLengthTest(unittest.TestCase):
    def rules(self, text):
        return rules_by_line(check_prose.check_markdown(text))

    def test_a_sentence_over_the_limit_is_refused_at_its_first_line(self):
        self.assertEqual(self.rules(f"{words(40)}\n\n{words(41)}\n"), [(3, "long-sentence")])

    def test_a_sentence_wrapped_over_lines_is_one_sentence(self):
        wrapped = words(41).replace(" word", "\nword", 1)
        self.assertEqual(self.rules(wrapped), [(1, "long-sentence")])

    def test_list_items_headings_and_tables_end_a_sentence(self):
        half = words(30)[:-1]
        text = f"- {half}\n- {half}.\n\n## {half}\n{half}.\n\n{half}\n| {half} |\n{half}.\n"
        self.assertEqual(self.rules(text), [])

    def test_code_quotations_and_link_targets_are_one_word_or_none(self):
        quoted = '"' + " ".join(["q"] * 20) + '"'
        text = words(35)[:-1] + f" `a b c d e f` {quoted} [a link](https://example.com/a/b/c).\n"
        self.assertEqual(self.rules(text), [])

    def test_a_quotation_that_ends_a_sentence_ends_it(self):
        text = words(30)[:-1] + ' reads "Reset the password." ' + words(30)
        self.assertEqual(self.rules(text), [])

    def test_an_abbreviation_does_not_end_a_sentence(self):
        text = words(20)[:-1] + " e.g. " + words(21).lower()
        self.assertEqual(self.rules(text), [(1, "long-sentence")])

    def test_fenced_code_is_not_a_sentence(self):
        self.assertEqual(self.rules(f"```\n{words(60)}\n```\n"), [])

    def test_an_exception_with_a_reason_lets_the_sentence_stand(self):
        long = words(41).replace(" word", "\nword", 1)
        self.assertEqual(self.rules(f"{long} <!-- long-sentence: quoted from the FIK rules -->\n"), [])

    def test_an_exception_needs_a_reason(self):
        self.assertEqual(self.rules(f"{words(41)} <!-- long-sentence: -->\n"), [(1, "long-sentence-exception")])

    def test_an_exception_with_no_long_sentence_is_refused(self):
        text = f"{words(10)} <!-- long-sentence: stale -->\n"
        self.assertEqual(self.rules(text), [(1, "long-sentence-exception")])

    def test_html_text_is_checked_too(self):
        html = f"<p>{words(41)}</p>\n<p>{words(40)}</p>\n"
        self.assertEqual(rules_by_line(check_prose.check_html(html)), [(1, "long-sentence")])


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
