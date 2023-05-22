# Install

You can install the pre-compiled binary (in several ways), use Docker or compile from source (when on OSS).

Below you can find the steps for each of them.

## Install the pre-compiled binary

=== "homebrew tap"

    ```bash
    brew install gitrgoliveira/tap/bracket-creator
    ```

=== "apt"

    ```bash
    echo 'deb [trusted=yes] https://apt.fury.io/gitrgoliveira/ /' | sudo tee /etc/apt/sources.list.d/gitrgoliveira.list
    sudo apt update
    sudo apt install bracket-creator
    ```

=== "yum"

    ```bash
    echo '[gitrgoliveira]
    name=Gemfury gitrgoliveira repository
    baseurl=https://yum.fury.io/gitrgoliveira/
    enabled=1
    gpgcheck=0' | sudo tee /etc/yum.repos.d/gitrgoliveira.repo
    sudo yum install goreleaser
    ```

## deb, rpm and apk packages

Download the .deb, .rpm or .apk packages from the [release page](https://github.com/gitrgoliveira/bracket-creator/releases) and install them with the appropriate tools.

## Manually

=== "go install"

    ```bash
    go install github.com/gitrgoliveira/bracket-creator@latest
    ```

=== "Released tar file"

    Download the pre-compiled binaries from the [release page](https://github.com/gitrgoliveira/bracket-creator/releases) page and copy them to the desired location.
    ```bash
    $ VERSION=v1.0.0
    $ OS=Linux
    $ ARCH=x86_64
    $ TAR_FILE=bracket-creator_${OS}_${ARCH}.tar.gz
    $ wget https://github.com/gitrgoliveira/bracket-creator/releases/download/${VERSION}/${TAR_FILE}
    $ sudo tar xvf ${TAR_FILE} bracket-creator -C /usr/local/bin
    $ rm -f ${TAR_FILE}
    ```

=== "manually"

    ```bash
    $ git clone github.com/gitrgoliveira/bracket-creator
    $ cd bracket-creator
    $ go generate ./...
    $ go install
    ```
