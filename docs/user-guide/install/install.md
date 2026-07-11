# Install

You can install with Homebrew, download a pre-compiled binary, use Go, build from source, or use Docker.

The following sections describe each method.

## Homebrew

```bash
brew tap gitrgoliveira/tap
brew trust gitrgoliveira/tap
brew install bracket-creator
```

`brew trust` marks the tap as trusted, which Homebrew requires before installing from a third-party tap. Update later with `brew upgrade bracket-creator`. The formula (in the [gitrgoliveira/homebrew-tap](https://github.com/gitrgoliveira/homebrew-tap) repository) builds from source, so it needs a C toolchain (the Xcode Command Line Tools on macOS or `build-essential` on Linux) and network access for Go module downloads.

The single binary bundles every subcommand, including `bracket-creator serve` (web UI) and `bracket-creator mobile-app` (tournament app).

## Pre-compiled binaries

Download the pre-compiled binaries from the [release page](https://github.com/gitrgoliveira/bracket-creator/releases) and extract them to your desired location.

```bash
OS=linux
ARCH=x86_64
TAR_FILE=bracket-creator_${OS}_${ARCH}.tar.gz
wget https://github.com/gitrgoliveira/bracket-creator/releases/latest/download/${TAR_FILE}
sudo tar xvf ${TAR_FILE} -C /usr/local/bin bracket-creator
rm -f ${TAR_FILE}
```

## Go install

```bash
go install github.com/gitrgoliveira/bracket-creator@latest
```

`go install` compiles from source rather than downloading a prebuilt binary. It builds the full binary, including the `serve` and `mobile-app` subcommands, but the embedded web assets are not part of the Go module, so those web UIs render blank. The Excel-generating CLI commands work normally. Use Homebrew or a release binary if you need the web UI.

## Build from source

If you prefer to compile from source, `bracket-creator` is written in [Go](https://golang.org/).

Prerequisites:
- [Go 1.26.5+](https://golang.org/doc/install)
- [Node.js](https://nodejs.org/) (`make go/build` runs `npx esbuild` to compile the web assets)
- `curl` (used to fetch the vendored frontend runtime)

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
