# Install

Docker is the recommended way to run bracket-creator on every platform. You can also install with Homebrew, Linux packages (`.deb`, `.rpm`, `.apk`), a pre-compiled binary, Go, or a source build.

The following sections describe each method, with [Upgrading](#upgrading) notes at the end.

## Docker (recommended)

Three multi-architecture (amd64 and arm64) images are published to the GitHub Container Registry:

* `ghcr.io/gitrgoliveira/bracket-creator-mobile`: the tournament app (`mobile-app` command)
* `ghcr.io/gitrgoliveira/bracket-creator-mobile-pdf`: the tournament app plus LibreOffice, needed for PDF export
* `ghcr.io/gitrgoliveira/bracket-creator`: the legacy Excel-generator web UI (`serve` command)

Run the tournament app; tournament state is stored in the mounted folder:

```bash
mkdir -p tournament-data
docker run -p 8080:8080 -v "$PWD/tournament-data:/tournament-data" \
  ghcr.io/gitrgoliveira/bracket-creator-mobile:latest
```

The app is available at `http://localhost:8080`.

The container runs as a non-root user (UID 65534). On Linux hosts, create the folder and make it writable by that UID before the first run: `mkdir -p tournament-data && sudo chown 65534 tournament-data`. Without the `chown`, the container cannot write to the folder: if the folder does not exist, Docker creates it owned by root, and if you created it with `mkdir`, it is owned by your login user, which is a different UID. Docker Desktop on macOS and Windows handles the permissions automatically.

See the [hosting guide](hosting.md) for production deployments, and [operating modes](../organisers/operating-modes.md) for access control.

### Docker from source

If you prefer to build the image yourself:

=== "Docker Compose"

    ```bash
    git clone https://github.com/gitrgoliveira/bracket-creator.git
    cd bracket-creator
    docker compose up -d
    ```

    Compose starts both services: the tournament app at `http://localhost:8081` and the legacy Excel-generator web UI at `http://localhost:8080`.

=== "Make"

    ```bash
    git clone https://github.com/gitrgoliveira/bracket-creator.git
    cd bracket-creator
    make docker/build
    make docker/run
    ```

## Homebrew

```bash
brew tap gitrgoliveira/tap
brew trust gitrgoliveira/tap
brew install bracket-creator
```

`brew trust` marks the tap as trusted, which Homebrew requires before installing from a third-party tap. Update later with `brew upgrade bracket-creator`. The formula (in the [gitrgoliveira/homebrew-tap](https://github.com/gitrgoliveira/homebrew-tap) repository) builds from source, so it needs a C toolchain (the Xcode Command Line Tools on macOS or `build-essential` on Linux) and network access for Go module downloads.

The single binary bundles every subcommand, including `bracket-creator serve` (web UI) and `bracket-creator mobile-app` (tournament app).

## Linux packages (deb, rpm, apk)

From the next release onwards, `.deb`, `.rpm`, and `.apk` packages for amd64/x86_64 and arm64/aarch64 are attached to the [release page](https://github.com/gitrgoliveira/bracket-creator/releases). Download the package for your distribution and architecture, then install it with the native tool (which resolves dependencies for local files):

=== "Debian/Ubuntu"

    ```bash
    sudo apt install ./bracket-creator_*_$(dpkg --print-architecture).deb
    ```

=== "Fedora/RHEL"

    ```bash
    sudo dnf install ./bracket-creator-*.$(uname -m).rpm
    ```

=== "Alpine"

    ```bash
    apk add --allow-untrusted ./bracket-creator_*_$(apk --print-arch).apk
    ```

    The package is not signed with an Alpine key, hence `--allow-untrusted`.

The packages install the binary to `/usr/bin`, plus the man page and bash/zsh/fish shell completions.

There is no hosted `apt`/`dnf`/`apk` repository, so these installs do not receive automatic upgrades; see [Upgrading](#upgrading).

## Pre-compiled binaries

Download the pre-compiled binaries from the [release page](https://github.com/gitrgoliveira/bracket-creator/releases) and extract them to your desired location.

```bash
OS=linux
ARCH=x86_64
TAR_FILE=bracket-creator_${OS}_${ARCH}.tar.gz
wget https://github.com/gitrgoliveira/bracket-creator/releases/latest/download/${TAR_FILE}
sudo tar xzvf ${TAR_FILE} -C /usr/local/bin bracket-creator
rm -f ${TAR_FILE}
```

## Go install

```bash
go install github.com/gitrgoliveira/bracket-creator@latest
```

`go install` compiles from source rather than downloading a prebuilt binary. It builds the full binary, including the `serve` and `mobile-app` subcommands, but the embedded web assets are not part of the Go module, so those web UIs render blank. The Excel-generating CLI commands work normally. Use Docker, Homebrew, or a release binary if you need the web UI.

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

## Upgrading

* **Docker**: `docker pull ghcr.io/gitrgoliveira/bracket-creator-mobile:latest` to fetch the newer image, then stop the running container and start it again with the same `docker run` command.
* **Homebrew**: `brew upgrade bracket-creator`.
* **Linux packages**: there is no hosted package repository, so upgrades are not automatic. Download the new release's package and install it the same way.
* **Pre-compiled binaries**: download and extract the new release's archive over the old binary.
* **Go / source builds**: re-run `go install github.com/gitrgoliveira/bracket-creator@latest`, or `git pull` and `make go/build`.
