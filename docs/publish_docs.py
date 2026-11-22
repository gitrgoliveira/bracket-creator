#!/usr/bin/env python3
"""Publish one version of the docs to the gh-pages branch with mike.

Usage: publish_docs.py REF [--source DIR] [--remote NAME] [--branch NAME] [--push]

Everything happens in the SOURCE checkout: it is what gets built, and its git
repository is where the branch is committed and which remote is pushed to.
To rehearse, point --source at a clone whose remote is a scratch repository:
a checkout whose origin is GitHub publishes the live site.

REF is `main` or a release tag. Which version it publishes:

  main                      -> `dev`
  vX.Y.Z, newest of X.Y     -> `X.Y`, plus the `latest` alias when it is the
                               newest stable release of all
  an older patch, a
  prerelease, a release
  before 2.5, anything else -> nothing (exit 0)

So a patch for an older line never overwrites the newer patch's docs, and a
prerelease (v2.6.0-rc1) publishes nothing: the same rule the Docker release
uses for its floating tags. The releases are read from the remote's tags.

Steps, each a commit on the local BRANCH (mike's own commit helper, so nothing
touches a working tree):
1. mike builds SOURCE with Zensical and commits it into the version's folder,
   with redirect pages for its aliases.
2. The root index.html sends readers to `latest`, once a release is published.
3. Once `latest` exists, the root is tidied: anything that is not a version, an alias or mike's own
   files is removed (the unversioned site that predates versioning), and a
   404.html is written that sends an old unversioned URL to the same page
   under `latest`. GitHub Pages serves that file for every missing path.

With --push the branch is pushed with a plain `git push`: if the remote moved
meanwhile the push is refused, never overwritten.

Run it with the docs venv's python (.venv-docs/bin/python), which has mike
and Zensical; .github/workflows/doc.yaml does, on each push to main and each
release tag. There is no make target on purpose: a run commits to the local
gh-pages branch, which every worktree of this repository shares. The functions above main() are stdlib-only, so
test_publish_docs.py runs without the venv.
"""

from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import sys
import tempfile
from urllib.parse import urlparse

STABLE_TAG = re.compile(r"^v(\d+)\.(\d+)\.(\d+)$")
# The first release built with Zensical. Earlier ones were built with MkDocs
# and are not published as versions (operator decision 2026-11-22).
OLDEST = (2, 5)
DEV = "dev"
LATEST = "latest"
# mike's files at the root of the branch, kept by the tidy.
ROOT_FILES = {".nojekyll", "404.html", "index.html", "versions.json"}

# Built on top of mkdocs.yaml for a published build only: the version picker
# reads the versions.json mike writes, which a local `make docs/serve` has
# none of. INHERIT keeps the tag's own mkdocs.yaml as it was released, so a
# tag made before versioning (v2.5.0) builds the same way as a later one.
PUBLISH_CONFIG = """\
INHERIT: mkdocs.yaml
extra:
  version:
    provider: mike
    default: latest
    alias: true
"""


def _release(tag: str) -> tuple[int, int, int]:
    m = STABLE_TAG.match(tag)
    assert m, tag
    return int(m[1]), int(m[2]), int(m[3])


def plan(ref: str, tags: list[str]) -> tuple[str, list[str]] | None:
    """The (version, aliases) REF publishes, or None when it publishes nothing.

    TAGS are the release tags on the remote; REF counts as one of them, since
    the remote may not list a tag pushed a moment ago yet.
    """
    if ref == "main":
        return DEV, []
    if not STABLE_TAG.match(ref):
        return None
    stable = sorted({t for t in tags if STABLE_TAG.match(t)} | {ref}, key=_release)
    major, minor, _ = _release(ref)
    if (major, minor) < OLDEST:
        return None
    line = [t for t in stable if _release(t)[:2] == (major, minor)]
    if line[-1] != ref:
        return None
    return f"{major}.{minor}", [LATEST] if stable[-1] == ref else []


def published_names(versions: list[dict]) -> set[str]:
    """Every folder name versions.json accounts for: versions and aliases."""
    names = set()
    for v in versions:
        names.add(v["version"])
        names.update(v.get("aliases", []))
    return names


def stray_root_entries(root_entries: list[str], versions: list[dict]) -> list[str]:
    """Root entries that belong to no version: the unversioned site's files."""
    keep = ROOT_FILES | published_names(versions)
    return sorted(e for e in root_entries if e not in keep)


def base_path(site_url: str) -> str:
    """The path the site is served under, with a trailing slash."""
    path = urlparse(site_url).path or "/"
    return path if path.endswith("/") else path + "/"


def root_404_html(base: str, versions: list[dict]) -> str:
    """The page GitHub Pages serves for a missing path.

    A path whose first folder is not a version or an alias is a link from
    before versioning: it goes to the same page under `latest`. A missing
    page inside a version stays put and says so.
    """
    names = json.dumps(sorted(published_names(versions)))
    base_js = json.dumps(base)
    latest = base + LATEST + "/"
    return f"""<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Page not found</title>
  <script>
    (function () {{
      var base = {base_js};
      var names = {names};
      var path = window.location.pathname;
      if (names.indexOf("{LATEST}") === -1 || path.indexOf(base) !== 0) return;
      var rest = path.slice(base.length);
      if (names.indexOf(rest.split("/")[0]) !== -1) return;
      window.location.replace(
        base + "{LATEST}/" + rest + window.location.search + window.location.hash
      );
    }})();
  </script>
</head>
<body>
  <h1>Page not found</h1>
  <p>This page is not in this version of the docs. Go to the
    <a href="{latest}">latest docs</a>.</p>
</body>
</html>
"""


def site_url(source: str) -> str:
    with open(os.path.join(source, "mkdocs.yaml"), encoding="utf-8") as fh:
        for line in fh:
            if line.startswith("site_url:"):
                return line.split(":", 1)[1].strip().strip("'\"")
    raise SystemExit("publish_docs: mkdocs.yaml has no site_url")


def remote_tags(remote: str) -> list[str]:
    out = subprocess.run(
        ["git", "ls-remote", "--tags", "--refs", remote, "refs/tags/v*"],
        check=True, capture_output=True, text=True,
    ).stdout
    return [line.split("refs/tags/", 1)[1] for line in out.splitlines()]


def run(*cmd: str, cwd: str | None = None) -> None:
    print("+ " + " ".join(cmd), flush=True)
    subprocess.run(cmd, check=True, cwd=cwd)


def fetch_branch(remote: str, branch: str) -> None:
    """Bring the remote's branch in as REMOTE/BRANCH, which mike compares the
    local branch with before committing (it moves a local branch that is
    behind, and refuses one that has diverged). A remote without the branch
    yet (a rehearsal on a new branch) leaves mike to start it."""
    found = subprocess.run(
        ["git", "ls-remote", "--heads", remote, f"refs/heads/{branch}"],
        check=True, capture_output=True, text=True,
    ).stdout.strip()
    if found:
        # One commit deep: mike commits on top of the tip and needs no history.
        run("git", "fetch", "--no-tags", "--depth=1", remote,
            f"+refs/heads/{branch}:refs/remotes/{remote}/{branch}")


def tidy_root(branch: str, base: str) -> None:
    from mike import git_utils  # the docs venv's mike

    versions = json.loads(git_utils.read_file(branch, "versions.json"))
    if LATEST not in published_names(versions):
        # Until a release is published the unversioned site stays: an old
        # URL has nowhere else to go yet.
        print("No release is published yet: the root is left as it is.")
        return
    root = [
        line.split("\t", 1)[1]
        for line in subprocess.run(
            ["git", "ls-tree", branch], check=True, capture_output=True, text=True
        ).stdout.splitlines()
    ]
    stray = stray_root_entries(root, versions)
    if stray:
        print("Removing the unversioned site from the root: " + ", ".join(stray))
    try:
        with git_utils.Commit(branch, "Tidy the root of the versioned docs") as commit:
            commit.delete_files(stray)
            commit.add_file(git_utils.FileInfo("404.html", root_404_html(base, versions)))
    except git_utils.GitEmptyCommit:
        print("The root is already tidy.")


def main(argv: list[str]) -> int:
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("ref", help="main, or a release tag such as v2.6.0")
    ap.add_argument("--source", default=".", help="the checkout to build (default: .)")
    ap.add_argument("--remote", default="origin")
    ap.add_argument("--branch", default="gh-pages")
    ap.add_argument("--push", action="store_true", help="push the branch when done")
    args = ap.parse_args(argv)

    # mike, and the zensical it runs, are beside this interpreter in the venv.
    venv_bin = os.path.dirname(sys.executable)
    os.environ["PATH"] = venv_bin + os.pathsep + os.environ.get("PATH", "")
    os.chdir(os.path.abspath(args.source))
    target = plan(args.ref, remote_tags(args.remote) if args.ref != "main" else [])
    if target is None:
        print(f"{args.ref} publishes no version (see the rules at the top of this script).")
        return 0
    version, aliases = target
    print(f"Publishing {args.ref} as {version}" + (f" ({', '.join(aliases)})" if aliases else ""))

    git = ["--remote", args.remote, "--branch", args.branch]
    fetch_branch(args.remote, args.branch)
    source = os.getcwd()
    with tempfile.NamedTemporaryFile(
        "w", suffix=".yaml", prefix=".mkdocs-publish-", dir=source, delete=False
    ) as fh:
        fh.write(PUBLISH_CONFIG)
        config = fh.name
    try:
        run("mike", "deploy", *git, "--config-file", config, "--alias-type", "redirect",
            "--update-aliases", version, *aliases)
    finally:
        os.unlink(config)
    if LATEST in aliases:
        run("mike", "set-default", *git, LATEST)
    tidy_root(args.branch, base_path(site_url(".")))
    if args.push:
        run("git", "push", "--", args.remote, args.branch)
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
