#!/usr/bin/env python3
"""Tests for docs/publish_docs.py (stdlib unittest, no venv needed).

Run with ``python3 docs/test_publish_docs.py`` (``make docs/prose`` does).
"""

from __future__ import annotations

import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import publish_docs  # noqa: E402

TAGS = ["v2.4.2", "v2.5.0", "v2.5.1", "v2.6.0-rc1", "v1.9.0"]


class PlanTest(unittest.TestCase):
    def test_main_publishes_dev(self):
        self.assertEqual(publish_docs.plan("main", TAGS), ("dev", []))

    def test_newest_release_publishes_its_line_and_latest(self):
        self.assertEqual(publish_docs.plan("v2.5.1", TAGS), ("2.5", ["latest"]))

    def test_a_tag_the_remote_does_not_list_yet_counts(self):
        self.assertEqual(publish_docs.plan("v2.6.0", TAGS), ("2.6", ["latest"]))

    def test_an_older_patch_of_a_line_publishes_nothing(self):
        self.assertIsNone(publish_docs.plan("v2.5.0", TAGS))

    def test_a_patch_for_an_older_line_keeps_latest_where_it_is(self):
        self.assertEqual(publish_docs.plan("v2.5.2", TAGS + ["v2.6.0"]), ("2.5", []))

    def test_a_release_before_zensical_publishes_nothing(self):
        self.assertIsNone(publish_docs.plan("v2.4.3", TAGS))

    def test_versions_order_by_number_not_text(self):
        self.assertEqual(publish_docs.plan("v2.10.0", TAGS + ["v2.9.0"]), ("2.10", ["latest"]))

    def test_a_prerelease_publishes_nothing(self):
        self.assertIsNone(publish_docs.plan("v2.6.0-rc1", TAGS))

    def test_a_branch_name_publishes_nothing(self):
        self.assertIsNone(publish_docs.plan("feature/x", TAGS))


VERSIONS = [
    {"version": "dev", "title": "dev", "aliases": []},
    {"version": "2.5", "title": "2.5", "aliases": ["latest"]},
]


class RootTest(unittest.TestCase):
    def test_stray_entries_are_the_unversioned_site(self):
        root = [".nojekyll", "2.5", "404.html", "assets", "dev", "index.html",
                "latest", "search.json", "user-guide", "versions.json"]
        self.assertEqual(publish_docs.stray_root_entries(root, VERSIONS),
                         ["assets", "search.json", "user-guide"])

    def test_base_path_of_a_project_site(self):
        self.assertEqual(publish_docs.base_path("https://example.github.io/proj/"), "/proj/")
        self.assertEqual(publish_docs.base_path("https://example.github.io/proj"), "/proj/")
        self.assertEqual(publish_docs.base_path("https://docs.example.com"), "/")

    def test_404_knows_every_version_and_alias(self):
        page = publish_docs.root_404_html("/proj/", VERSIONS)
        self.assertIn('var names = ["2.5", "dev", "latest"];', page)
        self.assertIn('var base = "/proj/";', page)
        self.assertIn('base + "latest/" + rest', page)
        self.assertIn('href="/proj/latest/"', page)

    def test_404_redirects_nowhere_until_latest_exists(self):
        page = publish_docs.root_404_html("/proj/", [VERSIONS[0]])
        self.assertIn('names.indexOf("latest") === -1', page)


if __name__ == "__main__":
    unittest.main()
