# Contributing

By participating in this project, you agree to abide our
[code of conduct](https://github.com/gitrgoliveira/bracket-creator/blob/main/.github/CODE_OF_CONDUCT.md).

## Set up your machine

`bracket-creator` is written in [Go](https://golang.org/).

Prerequisites:

- [Go 1.26.5+](https://golang.org/doc/install)

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

When you are satisfied with the changes, we suggest you run:

```sh
make go/test
make go/test-race
```

Before you commit the changes, we also suggest you run:

```sh
make pre-commit
```

### Testing the bracket generator web UI

```sh
make run          # starts on localhost:8080
PORT=8081 make run
```

Open the browser and walk through bracket generation manually. Type checking and unit tests do not exercise the UI rendering path.

### Testing the mobile / tournament app

```sh
make run-mobile                                     # localhost:8080, data dir ./tournament-data
PORT=8082 make run-mobile                           # custom port
TOURNAMENT_DATA_DIR=/path/to/data make run-mobile  # custom data dir
```

`PORT`, `BIND_ADDRESS`, and `TOURNAMENT_DATA_DIR` are read by the binary directly, so they also work without `make`:

```sh
TOURNAMENT_DATA_DIR=/path PORT=8082 ./bin/bracket-creator mobile-app
```

An explicit `--folder`, `--port`, or `--bind` flag still overrides the env var.

**Important:** `web-mobile/` is a Preact/JSX frontend compiled by esbuild into `web-mobile/dist/` and then embedded into the Go binary at build time. Any change to `web-mobile/js/*.js` or `web-mobile/css/*.css` requires the following:

1. Rebuild the JS bundle: `cd web-mobile && npm run build` (or `npx esbuild ...`; see the project `Makefile`)
2. Rebuild the binary: `make go/build`
3. Restart the server: `make run-mobile`

Editing `.js` files and refreshing the browser does **not** pick up changes; the browser is served the embedded bundle baked into the last binary build.

## Create a commit

Commit messages should be well formatted, and to make that "standardized", we
are using Conventional Commits.

You can follow the documentation on
[their website](https://www.conventionalcommits.org).

## Submit a pull request

Push your branch to your `bracket-creator` fork and open a pull request against the main branch.

## Credit

This CONTRIBUTING guideline is very inspired by the [goreleaser](https://github.com/goreleaser/goreleaser/blob/main/CONTRIBUTING.md). Thanks `goreleaser`.
