# Contributing

By participating in this project, you agree to abide by our
[code of conduct](https://github.com/gitrgoliveira/bracket-creator/blob/main/.github/CODE_OF_CONDUCT.md).

## Set up your machine

`bracket-creator` is written in [Go](https://golang.org/).

Prerequisites:

- [Go 1.27.2+](https://golang.org/doc/install)

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

### Run the end-to-end journeys

The end-to-end journeys drive the tournament app in a real browser, the way a court operator and a spectator use it: an iPad-sized touch screen for the operator and a phone for the public pages. Run them before you open a pull request that changes `web-mobile/`; the pull request template asks for it.

```sh
make e2e                                              # every journey
make e2e E2E_ARGS='journeys/smoke.spec.mjs'           # one journey file
make e2e E2E_ARGS='--grep "score on a shiaijo"'       # one test by title
make e2e E2E_ARGS='journeys/smoke.spec.mjs --headed'  # watch it run
```

`make e2e` builds the binary, then runs each journey file against its own server on a free port with an empty data folder. You need Go, `node` with `npm`, and a network connection: the first run installs the harness's packages and its Chromium, and every build downloads the frontend libraries. The journeys are not part of `make go/test` and do not run in CI.

The harness's own guide, [`scripts/e2e/README.md`](https://github.com/gitrgoliveira/bracket-creator/blob/main/scripts/e2e/README.md), covers the rest: the devices, the console policy that fails a journey on a browser error, where screenshots and traces are written, how to add a journey, and what to do when a journey finds a bug.

## Regenerate the documentation screenshots

Every screenshot and video under `docs/screenshots/` and `docs/videos/` is captured from the running application by a script, so a picture cannot quietly drift from the product it shows. If your change alters anything a user sees, recapture what it reaches with `make docs/media SINCE=main` before you open the pull request.

```sh
make docs/screenshots                        # the application screenshots
make docs/videos                             # the application videos
make docs/media                              # both

make docs/screenshots NAME=mobile-dashboard  # one capture
make docs/screenshots FAMILY=editors         # one group
make docs/screenshots SINCE=main             # only the groups your changes reach
```

You need `node` and `python3`; the first run installs the rest. A run writes to `scripts/screenshots/out/`, never into `docs/` directly, and ends by listing only the screenshots that differ from the committed ones. Look at those and copy across the ones whose change you meant to make. Anything reported as unchanged needs no review. Videos are never compared, so copy a video only when your change alters what it shows.

The harness's own guide, [`scripts/screenshots/README.md`](https://github.com/gitrgoliveira/bracket-creator/blob/main/scripts/screenshots/README.md), covers the rest: how `SINCE=` decides what to capture, what a changed line tells you, what to do when a capture you did not touch appears in the list, how a capture must be seeded, and how to add one.

## Create a commit

We use Conventional Commits, so write commit messages in that format.

Refer to [Conventional Commits](https://www.conventionalcommits.org) for the specification.

## Submit a pull request

Push your branch to your `bracket-creator` fork and open a pull request against the main branch.

## Credit

This CONTRIBUTING guide is adapted from [goreleaser's](https://github.com/goreleaser/goreleaser/blob/main/CONTRIBUTING.md). Thanks, goreleaser.
