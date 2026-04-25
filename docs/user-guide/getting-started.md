# Install

You can install the pre-compiled binary, use Go, build from source, or use Docker.

Below you can find the steps for each of them.

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
- [Go 1.26.2+](https://golang.org/doc/install)

```bash
git clone https://github.com/gitrgoliveira/bracket-creator.git
cd bracket-creator
make go/build
```

The binary will be available at `./bin/bracket-creator`.

## Docker

You can also run the application using Docker.

=== "Docker Compose"

    The easiest way to get the web UI running is using Docker Compose:

    ```bash
    git clone https://github.com/gitrgoliveira/bracket-creator.git
    cd bracket-creator
    docker compose up -d
    ```

    The application will be available at `http://localhost:8080`.

=== "Make"

    Alternatively, you can build and run it using the provided Makefile targets:

    ```bash
    git clone https://github.com/gitrgoliveira/bracket-creator.git
    cd bracket-creator
    make docker/build
    make docker/run
    ```
