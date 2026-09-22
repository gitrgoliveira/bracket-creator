# Docs capture harness

Regenerates every screenshot and video the documentation embeds, by driving the
real application in a browser.

    make docs/screenshots                        # the 30 application screenshots
    make docs/videos                             # the 3 application videos
    make docs/media                              # both

    make docs/screenshots NAME=mobile-dashboard  # one capture
    make docs/screenshots FAMILY=editors         # one group
    make docs/videos NAME=kachinuki-demo         # one video

Screenshots and videos are split because they cost very different amounts of
time: the stills finish in a few minutes, while each video is recorded in real
time and has to be paced for a human to follow.

Output lands in `out/`, never straight into `docs/`. Each still is compared
pixel by pixel against the file it would replace, and a run ends by naming only
the surfaces that actually changed - those are the ones to look at and copy
across. A capture reported `unchanged` reproduced the committed file exactly.

That comparison is only meaningful because `docs/screenshots/` holds this
harness's own output. If you replace a committed image by any other means, the
next run reports it as changed until the harness has produced it once.

Videos are not compared: their encoding depends on how fast the machine drove
the interface, so a clip never reproduces its committed bytes.

Re-recording `kachinuki-demo` carries one manual step. The clip is chaptered by
a numbered list under the `<video>` in
`docs/user-guide/organisers/team-tournaments.md`, and its timings move by
several seconds between recordings. The runner prints the new marks as
`CHAPTERS`; copy them into that list, or the numbers point at the wrong
moments.

## When a page misbehaves

A capture is a real browser session, so the page can say it is broken while
being photographed. Two severities, treated differently on purpose:

- An **uncaught exception** fails the capture. The surface is broken and the
  screenshot would record that as though it were the product working.
- A **console error** is reported after the run but does not fail it. The SPA
  asks for a team's lineup before one exists and the server answers 404, which
  the client handles; that is normal on seven captures here. Failing on it
  would fire on every team surface, and a gate that always fires is one the
  operator learns to skip.

## Prerequisites

`node` and `python3`. `make docs/screenshots` (or `docs/videos`) installs this directory's npm
dependencies and downloads its own chromium on first use; both are stamped inside
`node_modules/`, so the download happens once per worktree.

Playwright lives here rather than in `web-mobile/package.json` on purpose. Putting
it there would download a browser for every worktree that runs `make js/deps`, and
would pull it into `audit-ci`'s scope.

## Layout

    run.mjs          the runner: boots a server, seeds, drives, captures
    lib/server.mjs   starts `mobile-app` or `serve` on a free port + temp data dir
    lib/api.mjs      the scaffolding calls (tournament, competition, roster, draw)
    lib/ui.mjs       signing in, and nothing else: each editor is driven by
                     the recipe group that needs it (see that file's header)
    lib/seed.mjs     runs scripts/setup_tournament.py for the demo tournament
    lib/fixture.mjs  read-back checks that refuse to capture a bad fixture
    lib/png.mjs      PNG dimensions and the pixel compare behind `unchanged`
    recipes/         one file per group, registered in recipes/index.mjs

## The rule that shapes the seeding

Competitor numbers resolve by member id and points come from ippon arrays. A
lineup or a score written over the HTTP API carries neither, so the number chips
render blank and PW/PL read zero. That has shipped a misleading screenshot before.

So the API creates the tournament, the competitions, the participants and the
draw, and everything that shows numbers, bout rows or points is entered by
driving the interface. `lib/fixture.mjs` re-reads the stored files afterwards and
refuses to capture when the ids or the points are missing, because a
half-completed flow leaves a page that still looks right.

## Adding a capture

Add a recipe to the file for its group, or create a new group file and register it
in `recipes/index.mjs`. Recipes are declarative where they can be (route,
viewport, DPR, what to wait for, what to crop) and carry a `drive()` where the
subject of the capture is itself a procedure, such as a score editor mid-bout.

Derive the viewport from the committed file: `file docs/screenshots/<name>.png`.
Most captures are at a device scale factor of 2, so a committed 2560x1800 means a
1280x900 viewport. The runner compares width always, and height for every mode
except `fullPage`, whose height moves with content.

A new capture has nothing to compare against and simply reports `NEW`.

## Why the run is reproducible

A capture has to reproduce byte for byte when nothing changed, or the "what
changed" list is noise. Two things make that true and both are load-bearing:
Chromium's text rasterisation is pinned at launch (`DETERMINISTIC_RENDERING` in
`run.mjs`), and every screenshot is taken with animations finished and the text
caret hidden. Without the first, six captures landed a few grey levels apart on
every run; without the second, a CSS transition caught mid-flight moved one
input border by 40 levels. The comparison still allows a small tolerance, set
from those measurements, because the residue does not quite reach zero.
