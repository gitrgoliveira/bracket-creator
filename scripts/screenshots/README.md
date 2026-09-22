# Docs capture harness

Regenerates every screenshot and video the documentation embeds, by driving the
real application in a browser.

    make docs/screenshots                        # the 30 application screenshots
    make docs/videos                             # the 3 application videos
    make docs/media                              # both

    make docs/screenshots NAME=mobile-dashboard  # one capture
    make docs/screenshots FAMILY=editors         # one group
    make docs/videos NAME=kachinuki-demo         # one video

Screenshots and videos are split because you rarely want both at once. Neither
is especially slow: measured on one machine, the three videos take about 60
seconds and the 30 screenshots about 175. A video is recorded in real time and
paced for a human to follow, so it is the more expensive of the two per clip,
but there are only three of them.

Output lands in `out/`, never straight into `docs/`. Each still is compared
pixel by pixel against the file it would replace, and a run ends by naming only
the surfaces that actually changed. Those are the ones to look at and copy
across. A capture reported `unchanged` is indistinguishable from the committed
file to a reader, so there is nothing to review and nothing to copy.

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
    lib/net.mjs      picks a free port for the server
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
Most captures are at a device scale factor of 1, so the committed dimensions are
the viewport. Six are at 2, where a committed 2560x1800 means a 1280x900
viewport: `mobile-dashboard`, `mobile-participants`, `mobile-draw-preview`,
`mobile-pool-standings`, `selfrun-register` and `selfrun-viewer-home`. Read the
recipe's `dpr` rather than assuming either. The runner compares width always,
and height for every mode except `fullPage`, whose height moves with content.

A new capture has nothing to compare against and reports `NEW`.

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

A run with any failed capture exits non-zero, so the target can gate a script.

## Why the run is reproducible

A capture has to reproduce closely enough that an unchanged surface never
reaches the changed list, or that list is noise. Two things make that true and
both are load-bearing: Chromium's text rasterisation is pinned at launch
(`DETERMINISTIC_RENDERING` in `run.mjs`), and every screenshot is taken with
animations finished and the text caret hidden. They do different jobs, and it is
worth knowing which:

- The rasteriser flags fix **which** rendering you get, not whether it repeats.
  Drop them and all 30 captures change, because the committed images are this
  configuration's output. Run-to-run drift without them is one capture at one
  grey level, so they are not what makes a run reproduce.
- The caret and animation settings are what makes a run reproduce. Turn them
  off and run twice: three of the thirty come back different between the two
  runs, by up to 31 grey levels over hundreds of pixels. A CSS transition
  caught mid-flight is the usual culprit.

What is left is small but not zero: re-run the suite and one or two of the
thirty come back a grey level or two from the committed file, on a handful of
pixels. That is why the comparison keeps a tolerance instead of comparing bytes.

One caveat, because it is the operator who would hit it. Over seven runs here,
six reported all thirty unchanged and one reported a single capture changed,
and that one has not reproduced since. So the changed list is not guaranteed
empty on an untouched tree. If a capture you did not expect appears there,
re-run before going looking: two runs disagreeing is itself the finding.
