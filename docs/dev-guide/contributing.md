# Contributing

By participating in this project, you agree to abide by our
[code of conduct](https://github.com/gitrgoliveira/bracket-creator/blob/main/.github/CODE_OF_CONDUCT.md).

## Set up your machine

`bracket-creator` is written in [Go](https://golang.org/).

Prerequisites:

- [Go 1.27.1+](https://golang.org/doc/install)

Other things you might need to run the tests:

- [Docker](https://www.docker.com/)

Clone `bracket-creator` anywhere:

```sh
git clone git@github.com:gitrgoliveira/bracket-creator.git
```

`cd` into the directory and install the dependencies:

```sh
make local/deps
```

## Test your change

You can create a branch for your changes and try to build from the source as you go:

```sh
make go/build
```

When you are satisfied with the changes, run:

```sh
make go/test
make go/test-race
```

Before you commit the changes, run:

```sh
make pre-commit
```

### Test the bracket generator web UI

```sh
make run          # starts on localhost:8080
PORT=8081 make run
```

Open the browser and walk through bracket generation manually. Type checking and unit tests do not exercise the UI rendering path.

### Test the mobile / tournament app

```sh
make run-mobile                                     # localhost:8080, data dir ./tournament-data
PORT=8082 make run-mobile                           # custom port
TOURNAMENT_DATA_DIR=/path/to/data make run-mobile  # custom data dir
```

The binary reads `PORT`, `BIND_ADDRESS`, and `TOURNAMENT_DATA_DIR` directly, so they also work without `make`:

```sh
TOURNAMENT_DATA_DIR=/path PORT=8082 ./bin/bracket-creator mobile-app
```

An explicit `--folder`, `--port`, or `--bind` flag still overrides the env var.

**Important:** `web-mobile/` is a Preact/JSX frontend compiled by esbuild into `web-mobile/dist/` and then embedded into the Go binary at build time. Any change to the `web-mobile/js/` sources (`.js` or `.jsx`) or `web-mobile/css/*.css` requires the following:

1. Rebuild the JS bundle: `cd web-mobile && npm run build` (or `npx esbuild ...`; refer to the project `Makefile`)
2. Rebuild the binary: `make go/build`
3. Restart the server: `make run-mobile`

Editing these frontend sources and refreshing the browser does **not** pick up changes. The browser still gets the embedded bundle from the last binary build.

## Regenerate the documentation screenshots

Every screenshot and video under `docs/screenshots/` and `docs/videos/` is captured from the running application by a script, so a picture cannot quietly drift from the product it shows.

```sh
make docs/screenshots   # the application screenshots
make docs/videos        # the application videos
make docs/media         # both
```

They are separate targets because you rarely want both at once. Neither is especially slow; the harness README (`scripts/screenshots/README.md`) has the timings, and the detail of how a run is kept reproducible.

Both targets take `NAME=` for a single capture or `FAMILY=` for one group:

```sh
make docs/screenshots NAME=mobile-dashboard
make docs/screenshots FAMILY=editors
make docs/videos NAME=kachinuki-demo
```

To capture only the groups your change can reach, pass a git ref:

```sh
make docs/screenshots SINCE=main
```

That reads git's list of changed files (your branch against `main`, plus anything uncommitted) and matches it against the source files each group declares it depends on. A changed file that no group claims runs everything, so a missed dependency costs time rather than a wrong screenshot. The run prints which groups it selected and why before it starts. A change that reaches no capture, such as this page, reports that and exits cleanly.

You need `node` and `python3`. The first run installs the harness dependencies and downloads a browser for it. That browser is deliberately kept out of the frontend test dependencies, so `make js/deps` stays fast for everyone who never captures anything.

A run builds the binary, starts a server on a free port against a throwaway data directory, seeds a tournament, drives the interface in a real browser, and writes the result to `scripts/screenshots/out/`. Nothing is written into `docs/` directly, so publishing a new capture stays your decision.

You do not have to review all 30. Every screenshot is compared pixel by pixel against the one it would replace, and a run ends by listing only the surfaces that actually changed:

```
28 unchanged, 2 changed, 0 video (not compared)

changed - eyeball these, then copy them over docs/screenshots/:
  viewer-competition: CHANGED 815x1163 (height differs from committed 815x2088 by 44% - check the content by eye)
  team-lineup: CHANGED 1585x1212 (156024 px differ, max 230 levels)
```

Look at those, and copy across the ones whose change you meant to make. A capture reported as unchanged is indistinguishable from the committed file to a reader, so there is nothing to review and nothing to copy.

The comparison allows a small tolerance, because two runs of the same code do not produce identical bytes. It is set well below any difference a reader could notice and well above the measured noise, so a listed change is almost always a real one. Almost: a capture you did not touch does occasionally appear in that list. Re-run before you go looking, since two runs disagreeing about the same surface is itself worth knowing.

Videos are never compared: their encoding depends on how fast the machine drove the interface, so a clip is restaged on every run and only worth copying if you drove a change.

Each capture is a real browser session, so a run also reports anything the page complained about while it was being photographed. An uncaught error fails that capture, because the picture would show a broken surface as though it were working. A console message is only reported, since some are normal. A run with any failed capture exits non-zero, so the target can gate a script.

A capture whose dimensions no longer match its committed twin is reported as a size mismatch. That usually means the viewport or the crop selector needs adjusting rather than that the surface changed.

One rule decides how a capture is seeded. Anything showing competitor numbers, bout rows or points has to be produced by driving the interface. A lineup or a score written straight over the API carries no member identifiers and no bout points, so those numbers render blank and the scores read zero. Creating the tournament, its competitions, its participants and its draw over the API is fine.

## Create a commit

We use Conventional Commits, so write commit messages in that format.

Refer to [Conventional Commits](https://www.conventionalcommits.org) for the specification.

## Submit a pull request

Push your branch to your `bracket-creator` fork and open a pull request against the main branch.

## Credit

This CONTRIBUTING guide is adapted from [goreleaser's](https://github.com/goreleaser/goreleaser/blob/main/CONTRIBUTING.md). Thanks, goreleaser.
