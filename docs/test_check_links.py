#!/usr/bin/env python3
"""Tests for docs/check_links.py (stdlib unittest, no venv needed).

Run with ``python3 docs/test_check_links.py`` (``make docs/prose`` does).
"""

from __future__ import annotations

import contextlib
import io
import os
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import check_links  # noqa: E402


def run_on_site(files: dict[str, str]):
    """Build a site tree from {relative path: text}, run main() on it and
    return (exit code, printed output)."""
    with tempfile.TemporaryDirectory() as site:
        for rel, text in files.items():
            path = os.path.join(site, rel)
            os.makedirs(os.path.dirname(path), exist_ok=True)
            with open(path, "w", encoding="utf-8") as fh:
                fh.write(text)
        out = io.StringIO()
        with contextlib.redirect_stdout(out):
            code = check_links.main([site])
        return code, out.getvalue()


class PosterTest(unittest.TestCase):
    def test_video_poster_is_collected(self):
        page = check_links.Page()
        page.feed('<video controls poster="shots/hero.png"><source src="a.mp4"></video>')
        self.assertEqual(page.links, ["shots/hero.png", "a.mp4"])

    def test_poster_on_another_tag_is_not_a_link(self):
        page = check_links.Page()
        page.feed('<div poster="nowhere.png"></div>')
        self.assertEqual(page.links, [])

    def test_existing_poster_passes(self):
        code, _ = run_on_site({
            "index.html": '<video poster="shots/hero.png"></video>',
            "shots/hero.png": "x",
        })
        self.assertEqual(code, 0)

    def test_missing_poster_fails(self):
        code, out = run_on_site({"index.html": '<video poster="shots/hero.png"></video>'})
        self.assertEqual(code, 1)
        self.assertIn("shots/hero.png", out)


if __name__ == "__main__":
    unittest.main()
