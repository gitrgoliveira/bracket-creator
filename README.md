<!-- BEGIN __DO_NOT_INCLUDE__ -->
<p align="center"><img src="https://github.com/gitrgoliveira/bracket-creator/blob/main/logo/bracket-creator.v2.jpeg?raw=true" alt="Logo" height="120" /></p>
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

Download the pre-compiled binaries from the [release page](https://github.com/gitrgoliveira/bracket-creator/releases) page and copy them to the desired location.

To use the web front end run this command and open your browser on http://localhost:8080
```bash
bracket-creator serve
```

You can also use docker with:
```bash
docker run -p 8080:8080 ghcr.io/gitrgoliveira/bracket-creator/bracket-creator:latest
```

or docker-compose to run the web server:
```bash
docker-compose up -d
```


There's also a CLI. To learn how to use the CLI run:
```bash
bracket-creator --help
bracket-creator create-pools --help
bracket-creator create-playoffs --help
```

Example to build the tool from source:
```bash
make go/build
```

### Input file format

The input file can be a simple list of names or a CSV formatted file.
For example:
```csv
First_Name Last_Name, Dojo
```
For teams, it should be just one team per line.

When using the CSV formatted style, `Dojo` is only used to try to ensure players/teams don't meet someone of the same dojo **when doing pools.**

### Customizing the web server
To set the listen address and port run:
```bash
bracket-creator serve --listen-address 0.0.0.0 --listen-port 8080
```

You can also use the environment variables:
```bash
export BIND_ADDRESS=0.0.0.0
export PORT=8080
```


### CLI Parameters to create Pools
Example command line to create pools with 5 players and 3 winners per pool:
```bash
bracket-creator create-pools -s -p 5 -w 3 -f ./mock_data_medium.csv -o ./pools-example.xlsx
```

* `-d` / `-determined` - Do not shuffle the names read from the input file
* `-f` / `-file` - Path to the CSV file containing the players/teams in `Name, Dojo` format. `Dojo` is a field to ensure players/teams don't endup fighting someone of the same dojo
* `-h` / `-help` - Show help
* `-o` / `-output` - Path to write the output excel file
* `-p` / `-players` - Minimum number of players/teams per pool. Extra players are added to the end of the pool if there are more than expected. The default is 3
* `-w` / `-pool-winners` - Number of players/teams that can qualify from each pool. The default is 2
* `-r` / `-round-robin` - Round robin, to ensure that in a pool of 4 or more, everyone would fight everyone. Otherwise, everyone fights only twice in their pool. The default is False
* `-s` / `-sanitize` - sanitize print names into first name initial and capitalize the last name. This is useful for individual player tournaments.
* `-t` / `-team-matches` - Create team matches with x players per team. Default is 0, which means these are not team matches

### CLI Parameters to create Playoffs
Example command line to create team playoffs with 5 players per team:
```bash
bracket-creator create-playoffs -t 5 -f ./mock_data_small.csv -o ./playoffs-example.xlsx
```

* `-d` / `-determined` - Do not shuffle the names read from the input file
* `-f` / `-file` - Path to the CSV file containing the players/teams in `Name, Dojo` format. `Dojo` is a field to ensure players/teams don't endup fighting someone of the same dojo
* `-h` / `-help` - Show help
* `-o` / `-output` - Path to write the output excel file
* `-s` / `-sanitize` - sanitize print names into first name initial and capitalize the last name. This is useful for individual player tournaments.
* `-t` / `-team-matches` - Create team matches with x players per team. Default is 0, which means these are not team matches

### Examples
See also the example files created by the Makefile:
- [playoffs-example-large.xlsx](playoffs-example-large.xlsx)
- [playoffs-example-medium.xlsx](playoffs-example-medium.xlsx)
- [playoffs-example-small.xlsx](playoffs-example-small.xlsx)
- [pools-example-large.xlsx](pools-example-large.xlsx)
- [pools-example-medium.xlsx](pools-example-medium.xlsx)
- [pools-example-small.xlsx](pools-example-small.xlsx)

**Individual pool player tournament**

With 4 players and 2 winners per pool with sanitized names:
```bash
./bin/bracket-creator create-pools -s -p 4 -f mock_data.csv -o output.xlsx
```

**Team pool tournament**

With 5 players per team:
```bash
./bin/bracket-creator create-pools -t 5 -f mock_data.csv -o output.xlsx 
```
**Individual playoffs player tournament**

Straight knockout with sanitized names:
```bash
./bin/bracket-creator create-playoffs -s -f mock_data.csv -o output.xlsx
```

**Team pool tournament**

Straight knockout team competition with teams of 3:
```bash
./bin/bracket-creator create-playoffs -t 3 -f mock_data.csv -o output.xlsx
```

## How to Use the output files
All generated output files are based on the `template.xlsx` file and to customise it you will need to edit the final file.

To be able to print the tree, you will need to reset the width and height in the Page Layout tab.

### On the day of the tournament
These files are generated to be uploaded to Google Drive (or similar), so all shiai-jo tables are in sync during the tournament, working from the same file.


## Install - WIP

Please use the pre-compiled binaries from the [release page](https://github.com/gitrgoliveira/bracket-creator/releases) or build from sratch with `make go/build`
The instructions below do not work yet.

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
