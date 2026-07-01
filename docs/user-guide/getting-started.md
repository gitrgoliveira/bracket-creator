# Install

You can install with Homebrew, download a pre-compiled binary, use Go, build from source, or use Docker.

The following sections describe each method.

## Homebrew

The Homebrew formula lives in the project repository and builds from source, so it needs the Xcode Command Line Tools and network access for Go module downloads.

=== "Direct install"

    ```bash
    brew install https://raw.githubusercontent.com/gitrgoliveira/bracket-creator/main/Formula/bracket-creator.rb
    ```

    To update later, run the same command again.

=== "Named tap"

    Add the repository as a tap so `brew upgrade` tracks it:

    ```bash
    brew tap gitrgoliveira/kendo https://github.com/gitrgoliveira/bracket-creator
    brew install gitrgoliveira/kendo/bracket-creator
    ```

    The tap is named `kendo` rather than `bracket-creator` because the latter would resolve to a separate, unmaintained `homebrew-bracket-creator` repository.

The single binary bundles every subcommand, including `bracket-creator serve` (web UI) and `bracket-creator mobile-app` (live-tournament app).

## Pre-compiled binaries

=== "go install"

    ```bash
    go install github.com/gitrgoliveira/bracket-creator@latest
    ```

=== "Released tar file"

    Download the pre-compiled binaries from the [release page](https://github.com/gitrgoliveira/bracket-creator/releases) and extract them to your desired location.

    ```bash
    $ VERSION=v1.0.0
    $ OS=Linux
    $ ARCH=x86_64
    $ TAR_FILE=bracket-creator_${OS}_${ARCH}.tar.gz
    $ wget https://github.com/gitrgoliveira/bracket-creator/releases/download/${VERSION}/${TAR_FILE}
    $ sudo tar xvf ${TAR_FILE} bracket-creator -C /usr/local/bin
    $ rm -f ${TAR_FILE}
    ```

## Build from source

If you prefer to compile from source, `bracket-creator` is written in [Go](https://golang.org/).

Prerequisites:
- [Go 1.26.4+](https://golang.org/doc/install)

```bash
git clone https://github.com/gitrgoliveira/bracket-creator.git
cd bracket-creator
make go/build
```

The binary is at `./bin/bracket-creator`.

## Docker

You can also run the application using Docker.

=== "Docker Compose"

    The easiest way to get the web UI running is using Docker Compose:

    ```bash
    git clone https://github.com/gitrgoliveira/bracket-creator.git
    cd bracket-creator
    docker compose up -d
    ```

    The application is available at `http://localhost:8080`.

=== "Make"

    Alternatively, you can build and run it using the provided Makefile targets:

    ```bash
    git clone https://github.com/gitrgoliveira/bracket-creator.git
    cd bracket-creator
    make docker/build
    make docker/run
    ```
