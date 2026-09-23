# Contributing

By participating in this project, you agree to abide our
[code of conduct](https://github.com/gitrgoliveira/bracket-creator/blob/main/.github/CODE_OF_CONDUCT.md).

## Set up your machine

`bracket-creator` is written in [Go](https://golang.org/).

Prerequisites:

- [Go 1.21+](https://golang.org/doc/install)

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

## Update the documentation screenshots

The screenshots and videos in the documentation are captured from the running
application by a script. If your change alters anything a user sees, recapture
the ones it reaches:

```sh
make docs/media SINCE=main
```

The run lists the screenshots that changed. Look at each, and copy the ones
your change meant to alter from `scripts/screenshots/out/` into
`docs/screenshots/`. Videos are not compared, so copy a video only when your
change alters what it shows. The
[contributing guide](https://gitrgoliveira.github.io/bracket-creator/dev-guide/contributing/#regenerate-the-documentation-screenshots)
has the details.

## Create a commit

Commit messages should be well formatted, and to make that "standardized", we
are using Conventional Commits.

You can follow the documentation on
[their website](https://www.conventionalcommits.org).

## Submit a pull request

Push your branch to your `bracket-creator` fork and open a pull request against the main branch.

## Credit

This CONTRIBUTING guideline is very inspired by the [goreleaser](https://github.com/goreleaser/goreleaser/blob/main/CONTRIBUTING.md). Thanks `goreleaser`.
