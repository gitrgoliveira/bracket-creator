<!-- BEGIN __DO_NOT_INCLUDE__ -->
<p align="center"><img src="https://gist.githubusercontent.com/gitrgoliveira/5be06c101f53079b9914d6efd867e690/raw/1db944ea6c82dde17ad24f2288eaeafe4013dafc/bracket-creator.v2.png" alt="Logo" height="120" /></p>
<!-- END __DO_NOT_INCLUDE__ -->
<h1 align="center"> bracket-creator</h1>

<p align="center">
  <a href="https://github.com/gitrgoliveira/bracket-creator/releases" rel="nofollow">
    <img alt="GitHub release (latest SemVer including pre-releases)" src="https://img.shields.io/github/v/release/gitrgoliveira/bracket-creator?include_prereleases">
  </a>

  <a href="https://github.com/gitrgoliveira/bracket-creator/actions/workflows/release.yaml" rel="nofollow">
    <img src="https://github.com/gitrgoliveira/bracket-creator/actions/workflows/release.yaml/badge.svg" alt="goreleaser" style="max-width:100%;">
  </a>

  <a href="https://pkg.go.dev/github.com/gitrgoliveira/bracket-creator" rel="nofollow">
    <img src="https://pkg.go.dev/badge/github.com/gitrgoliveira/bracket-creator.svg" alt="Go reference" style="max-width:100%;">
  </a>

  <a href="https://github.com/gojp/goreportcard/blob/master/LICENSE" rel="nofollow">
    <img src="https://img.shields.io/badge/license-Apache 2.0-blue.svg" alt="License Apache 2.0" style="max-width:100%;">
  </a>

  <br/>

  <a href="https://codecov.io/gh/gitrgoliveira/bracket-creator" >
    <img src="https://codecov.io/gh/gitrgoliveira/bracket-creator/branch/main/graph/badge.svg?token=CLP6KW4QLK"/>
  </a>

  <a href="https://github.com/gitrgoliveira/bracket-creator/actions/workflows/codeql.yaml" rel="nofollow">
    <img src="https://github.com/gitrgoliveira/bracket-creator/actions/workflows/codeql.yaml/badge.svg" alt="codeql" style="max-width:100%;">
  </a>

  <a href="https://goreportcard.com/report/github.com/gitrgoliveira/bracket-creator" rel="nofollow">
    <img src="https://goreportcard.com/badge/github.com/gitrgoliveira/bracket-creator" alt="Go report card" style="max-width:100%;">
  </a>
</p>
<br/>

A CLI to create kendo tournament brackets

<!-- BEGIN __DO_NOT_INCLUDE__ -->

## Usage

You will need to compile the binary from source or download a release from github, if one is available.

To learn how to use the CLI run:
```bash
bc --help
```

Example usage:
```bash
make go/build && ./bin/bc create -p ./teams.csv
```

CSV format for individual matches should be:
```csv
Name, Dojo
```

For teams, it shoud be one team per line.


## Install - WIP

*You can install the pre-compiled binary (in several ways), use Docker or compile from source (when on OSS).*

*Below you can find the steps for each of them.*
<details>
  <summary><h3>homebrew tap</h3></summary>

```bash
brew install gitrgoliveira/tap/bracket-creator
```

</details>

<details>
  <summary><h3>apt</h3></summary>

```bash
echo 'deb [trusted=yes] https://apt.fury.io/gitrgoliveira/ /' | sudo tee /etc/apt/sources.list.d/gitrgoliveira.list
sudo apt update
sudo apt install bracket-creator
```

</details>

<details>
  <summary><h3>yum</h3></summary>

```bash
echo '[gitrgoliveira]
name=Gemfury gitrgoliveira repository
baseurl=https://yum.fury.io/gitrgoliveira/
enabled=1
gpgcheck=0' | sudo tee /etc/yum.repos.d/gitrgoliveira.repo
sudo yum install goreleaser
```

</details>

<details>
  <summary><h3>deb, rpm and apk packages</h3></summary>
Download the .deb, .rpm or .apk packages from the [release page](https://github.com/gitrgoliveira/bracket-creator/releases) and install them with the appropriate tools.
</details>

<details>
  <summary><h3>go install</h3></summary>

```bash
go install github.com/gitrgoliveira/bracket-creator@latest
```

</details>

<details>
  <summary><h3>from the GitHub releases</h3></summary>

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

</details>

<details>
  <summary><h3>manually</h3></summary>

```bash
$ git clone github.com/gitrgoliveira/bracket-creator
$ cd bracket-creator
$ go generate ./...
$ go install
```

</details>

## Contribute to this repository

This project adheres to the Contributor Covenant [code of conduct](https://github.com/gitrgoliveira/bracket-creator/blob/main/.github/CODE_OF_CONDUCT.md). By participating, you are expected to uphold this code. We appreciate your contribution. Please refer to our [contributing](https://github.com/gitrgoliveira/bracket-creator/blob/main/.github/CONTRIBUTING.md) guidelines for further information.
