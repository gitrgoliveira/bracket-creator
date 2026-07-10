#!/usr/bin/env python3
"""Check the built MkDocs site for broken internal links, anchors, and assets.

This runs against the generated ``site/`` directory (after ``mkdocs build``), so
it validates exactly what ships. That catches two whole classes of breakage that
``mkdocs build --strict`` does not:

* links produced by page templates (``overrides/home.html``,
  ``overrides/404.html``) that strict-mode never validates, and
* same-page ``#anchor`` links, which strict mode only logs as INFO.

Every internal ``<a href>``, ``<img src>``, ``<script src>`` and ``<link href>``
is resolved against the built tree and, when it carries a ``#fragment``, checked
against the ``id`` attributes on the target page. External links
(http, https, mailto, tel, ...) are deliberately not checked: network probes are
flaky and do not belong in a build gate.

Usage:
    python3 docs/check_links.py [site_dir]   # default: site

Exit code: 0 when every internal link resolves, 1 when any are broken,
2 on a usage error (missing site dir).
"""

from __future__ import annotations

import os
import re
import sys
from html.parser import HTMLParser

# Schemes / prefixes we treat as external and never resolve on disk.
EXTERNAL_PREFIXES = (
    "http:",
    "https:",
    "mailto:",
    "tel:",
    "data:",
    "javascript:",
    "ftp:",
    "//",  # protocol-relative
)


def site_base_path(repo_root: str) -> str:
    """Return the URL path prefix of ``site_url`` (e.g. ``/bracket-creator``).

    The 404 page emits root-absolute links under this prefix, so we strip it to
    resolve them against the built tree. Read with a plain regex rather than a
    YAML parser: mkdocs.yaml carries ``!!python/name:`` tags that trip
    ``yaml.safe_load``.
    """
    cfg = os.path.join(repo_root, "mkdocs.yaml")
    if not os.path.exists(cfg):
        cfg = os.path.join(repo_root, "mkdocs.yml")
    try:
        with open(cfg, encoding="utf-8") as fh:
            for line in fh:
                m = re.match(r"\s*site_url:\s*(\S+)", line)
                if m:
                    # https://host/bracket-creator/ -> /bracket-creator
                    path = re.sub(r"^https?://[^/]+", "", m.group(1).strip())
                    return "/" + path.strip("/")
    except OSError:
        pass
    return ""


class Page(HTMLParser):
    """Collect anchor ids and outgoing links from one HTML page."""

    def __init__(self) -> None:
        super().__init__(convert_charrefs=True)
        self.ids: set[str] = set()
        self.links: list[str] = []

    def handle_starttag(self, tag, attrs):
        a = dict(attrs)
        if a.get("id"):
            self.ids.add(a["id"])
        if tag == "a" and a.get("name"):  # legacy named anchors
            self.ids.add(a["name"])
        if tag in ("a", "link") and a.get("href") is not None:
            self.links.append(a["href"])
        elif tag in ("img", "script") and a.get("src") is not None:
            self.links.append(a["src"])

    handle_startendtag = handle_starttag


def is_external(href: str) -> bool:
    h = href.strip().lower()
    return any(h.startswith(p) for p in EXTERNAL_PREFIXES)


def resolve(href: str, page_path: str, site: str, base_path: str):
    """Resolve an internal href to (target_html_path, fragment).

    Returns (None, fragment) for a same-page anchor. target_html_path is the
    absolute path we expect on disk (a directory URL maps to its index.html).
    """
    base, _, frag = href.partition("#")
    base = base.split("?", 1)[0]
    base = re.sub(r"/{2,}", "/", base)  # collapse // (the 404 template emits it)

    if base == "":
        return page_path, frag  # same-page anchor

    if base.startswith("/"):
        # Root-absolute (the 404 page). Strip the site path prefix, then treat
        # the remainder as relative to the site root.
        if base_path and base.startswith(base_path):
            base = base[len(base_path):]
        target = os.path.normpath(os.path.join(site, base.lstrip("/")))
    else:
        target = os.path.normpath(os.path.join(os.path.dirname(page_path), base))

    if os.path.isdir(target) or href.split("#", 1)[0].endswith("/"):
        target = os.path.join(target, "index.html")
    return target, frag


def main() -> int:
    site = os.path.abspath(sys.argv[1] if len(sys.argv) > 1 else "site")
    if not os.path.isdir(site):
        print(
            f"error: site dir not found: {site}\n"
            "run `make docs/build` first (the checker validates the built site).",
            file=sys.stderr,
        )
        return 2

    repo_root = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
    base_path = site_base_path(repo_root)

    pages: dict[str, Page] = {}
    for root, _dirs, files in os.walk(site):
        for f in files:
            if f.endswith(".html"):
                fp = os.path.join(root, f)
                p = Page()
                with open(fp, encoding="utf-8", errors="replace") as fh:
                    p.feed(fh.read())
                pages[fp] = p

    broken: list[tuple[str, str, str]] = []
    checked = 0
    for fp, page in sorted(pages.items()):
        src = os.path.relpath(fp, site)
        for href in page.links:
            h = href.strip()
            if not h or h == "#" or is_external(h):
                continue
            checked += 1
            target, frag = resolve(h, fp, site, base_path)

            if target is not None and not os.path.exists(target):
                broken.append((src, href, f"target not found: {os.path.relpath(target, site)}"))
                continue

            if frag:
                tgt = target if target is not None else fp
                if tgt.endswith(".html"):
                    tp = pages.get(tgt)
                    if tp is None:
                        broken.append((src, href, f"unparsed target for #{frag}: {os.path.relpath(tgt, site)}"))
                    elif frag not in tp.ids:
                        where = "this page" if tgt == fp else os.path.relpath(tgt, site)
                        broken.append((src, href, f"missing anchor #{frag} in {where}"))

    if broken:
        print(f"FAIL: {len(broken)} broken internal link(s) in the built site:\n")
        for src, href, why in broken:
            print(f"  page: {src}")
            print(f"  link: {href}")
            print(f"    -> {why}\n")
        return 1

    print(f"OK: {len(pages)} pages, {checked} internal links and anchors all resolve.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
