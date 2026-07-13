<!-- BEGIN __DO_NOT_INCLUDE__ -->
<p align="center"><img src="https://github.com/gitrgoliveira/bracket-creator/blob/main/logo/bracket-creator.v2.jpeg?raw=true" alt="bracket-creator logo" height="120" /></p>
<!-- END __DO_NOT_INCLUDE__ -->
<h1 align="center">bracket-creator</h1>

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

  <a href="https://github.com/gitrgoliveira/bracket-creator/blob/main/LICENSE" rel="nofollow">
    <img src="https://img.shields.io/badge/license-MPL 2.0-blue.svg" alt="License MPL 2.0" style="max-width:100%;">
  </a>

  <br/>

  <a href="https://codecov.io/gh/gitrgoliveira/bracket-creator" >
    <img src="https://codecov.io/gh/gitrgoliveira/bracket-creator/branch/main/graph/badge.svg?token=CLP6KW4QLK"/>
  </a>

  <a href="https://github.com/gitrgoliveira/bracket-creator/actions/workflows/codeql.yaml" rel="nofollow">
    <img src="https://github.com/gitrgoliveira/bracket-creator/actions/workflows/codeql.yaml/badge.svg" alt="codeql" style="max-width:100%;">
  </a>
</p>
<br/>

This project lets any club or organisation run kendo and naginata tournaments at whatever level of digitization fits the venue, from fully printed to fully online. Pick the mode that matches your equipment:

* **Offline.** No internet required. Runs entirely on printed brackets and score sheets generated as Excel files.
* **Partially connected.** Internet is available but there are no display screens. Some printed material is still needed for scoreboards and competitors. Keep every shiai-jo in sync either by sharing the Excel file through Google Sheets, or by running the tournament app, with one device per shiai-jo.
* **Fully digital.** A complete online setup with on-screen scoreboards and mobile result pages. Needs one device per shiai-jo table, a monitor for each court, and network access for competitors. Organisers still print player tags and numbers.

I've been using this application to organise the London Cup since ~2023. It reflects everything I've learned from running real tournaments and the feedback I've received.

For the kendo tournament guidelines this application is based on, see [running_a_kendo_tournament.md](running_a_kendo_tournament.md). Naginata and Engi-kyogi specifics are covered in the [Naginata competitions guide](https://gitrgoliveira.github.io/bracket-creator/user-guide/organisers/naginata/).

**Full documentation lives on the [documentation website](https://gitrgoliveira.github.io/bracket-creator/).** This README is only a quick overview.

## The tournament app

The `mobile-app` command starts the tournament app server: create competitions, import participants, draw pools and brackets, schedule matches across courts, score in real time, and show results on any device (phone, tablet, laptop, or a TV by the court).

[Install](#install) the binary first (Homebrew or a release binary), then start the server:

```bash
bracket-creator mobile-app --folder ./tournament-data
```

Then open [http://localhost:8080](http://localhost:8080) in your browser.

<img src="docs/screenshots/mobile-dashboard.png" alt="Admin dashboard" width="720">

Highlights:

* **Admin console** (password-protected): competitions, participants, seeding, draws, scheduling, and scoring.
* **Public viewer and court displays**: real-time schedule, standings, and brackets for spectators, plus big-screen court displays and a transparent overlay for streaming.
* **All formats**: Playoffs, Pools + playoffs, League, and Swiss, for individuals and teams (including kachinuki), plus naginata and Engi-kyogi (kata) competitions.
* **Excel export**: download the results as a workbook at any point during the event.

Start with these guides on the documentation site:

* [Choosing your setup](https://gitrgoliveira.github.io/bracket-creator/user-guide/start-here/choosing-your-setup/)
* [Your first tournament](https://gitrgoliveira.github.io/bracket-creator/user-guide/start-here/first-tournament/)
* [Run a tournament on the day](https://gitrgoliveira.github.io/bracket-creator/user-guide/organisers/run-tournament/)
* [Host the tournament app](https://gitrgoliveira.github.io/bracket-creator/user-guide/install/hosting/) and [operating modes and access control](https://gitrgoliveira.github.io/bracket-creator/user-guide/organisers/operating-modes/)
* [mobile-app command reference](https://gitrgoliveira.github.io/bracket-creator/user-guide/commands/mobile-app/)

## Excel brackets from the command line

The original core of the project is still there: give it a CSV of participants and it produces fully formatted, print-ready Excel workbooks with pool draws, match schedules, and elimination trees.

```bash
# Pools of at least 5 with 3 winners per pool
bracket-creator create-pools -z -p 5 -w 3 -f players.csv -o pools.xlsx

# Straight knockout for teams of 5
bracket-creator create-playoffs -t 5 -f teams.csv -o playoffs.xlsx
```

See the docs for the full command references and the input format:

* [create-pools](https://gitrgoliveira.github.io/bracket-creator/user-guide/commands/create-pools/)
* [create-playoffs](https://gitrgoliveira.github.io/bracket-creator/user-guide/commands/create-playoffs/)
* [Input format](https://gitrgoliveira.github.io/bracket-creator/user-guide/organisers/input-format/)
* [Tournament formats](https://gitrgoliveira.github.io/bracket-creator/user-guide/organisers/formats/)

## Legacy Web UI

`bracket-creator serve` starts a browser front-end for the same Excel generators, useful if you prefer a form over CLI flags. It is kept for compatibility; the tournament app above is the recommended way to run an event. See the [Legacy Web UI guide](https://gitrgoliveira.github.io/bracket-creator/user-guide/organisers/web-ui/).

## Install

*You can install via Homebrew, download a pre-compiled binary, use Docker, or compile from source.* Full instructions, including Docker and hosting options, are in the [install guide](https://gitrgoliveira.github.io/bracket-creator/user-guide/install/install/).

Before you pick a method:

* Homebrew, the pre-compiled binaries on the [release page](https://github.com/gitrgoliveira/bracket-creator/releases), and building from source (`make go/build`) all work today and bundle the web/mobile UI.
* `go install` builds the full binary, but the embedded web assets (the Preact runtime and compiled JS bundle) are not part of the Go module, so the `serve`/`mobile-app` web UIs render blank. Use Homebrew or a release binary if you need them.
* The `apt`/`yum`/`deb`/`rpm`/`apk` sections below are still a work in progress: those package repositories are not published yet, so the commands shown there do not work.

<details>
  <summary><b>Homebrew</b></summary>

```bash
brew tap gitrgoliveira/tap
brew trust gitrgoliveira/tap
brew install bracket-creator
```

`brew trust` marks the tap as trusted, which Homebrew requires before installing from a third-party tap. Update later with `brew upgrade bracket-creator`. The formula (in the [gitrgoliveira/homebrew-tap](https://github.com/gitrgoliveira/homebrew-tap) repository) builds from source, so it needs a C toolchain (the Xcode Command Line Tools on macOS or `build-essential` on Linux) and network access for Go module downloads.

The single binary bundles every subcommand, including `bracket-creator serve` (web UI) and `bracket-creator mobile-app` (tournament app).

</details>

<details>
  <summary><b><code>apt</code></b></summary>

```bash
echo 'deb [trusted=yes] https://apt.fury.io/gitrgoliveira/ /' | sudo tee /etc/apt/sources.list.d/gitrgoliveira.list
sudo apt update
sudo apt install bracket-creator
```

</details>

<details>
  <summary><b><code>yum</code></b></summary>

```bash
echo '[gitrgoliveira]
name=Gemfury gitrgoliveira repository
baseurl=https://yum.fury.io/gitrgoliveira/
enabled=1
gpgcheck=0' | sudo tee /etc/yum.repos.d/gitrgoliveira.repo
sudo yum install bracket-creator
```

</details>

<details>
  <summary><b>deb, rpm and apk packages</b></summary>
Download the .deb, .rpm or .apk packages from the [release page](https://github.com/gitrgoliveira/bracket-creator/releases) and install them with the appropriate tools.
</details>

<details>
  <summary><b><code>go install</code></b></summary>

```bash
go install github.com/gitrgoliveira/bracket-creator@latest
```

This builds the full binary, including the `serve` and `mobile-app` subcommands. However, the embedded web assets are not part of the Go module, so those web UIs render blank from a `go install` build. The Excel-generating CLI commands (`create-pools`, `create-playoffs`, ...) work normally. Use Homebrew or a release binary if you need the web UI.

</details>

<details>
  <summary><b>From the GitHub releases</b></summary>

Download the pre-compiled binaries from the [release page](https://github.com/gitrgoliveira/bracket-creator/releases) and copy them to the desired location.

```bash
OS=linux
ARCH=x86_64
TAR_FILE=bracket-creator_${OS}_${ARCH}.tar.gz
wget https://github.com/gitrgoliveira/bracket-creator/releases/latest/download/${TAR_FILE}
sudo tar xzvf ${TAR_FILE} -C /usr/local/bin bracket-creator
rm -f ${TAR_FILE}
```

</details>

<details>
  <summary><b>Build from source</b></summary>

```bash
git clone https://github.com/gitrgoliveira/bracket-creator.git
cd bracket-creator
make go/build
./bin/bracket-creator --help
```

`make go/build` vendors the frontend runtime and compiles the JSX bundle before building, so the resulting binary bundles the full web/mobile UI (unlike plain `go install`; see the note above). It needs Go, plus Node.js and `curl` for the frontend steps.

</details>

## Contribute to this repository

This project adheres to the Contributor Covenant [code of conduct](https://github.com/gitrgoliveira/bracket-creator/blob/main/.github/CODE_OF_CONDUCT.md). By participating, you are expected to uphold this code. We appreciate your contribution. Please refer to our [contributing](https://github.com/gitrgoliveira/bracket-creator/blob/main/.github/CONTRIBUTING.md) guidelines for further information.

## License

Copyright © 2023–2026 Ricardo Oliveira &lt;oliveira.rg [at] gmail.com&gt;

This is an independent project created and maintained by Ricardo Oliveira in a
personal capacity: in his own time and on his own equipment. It is not
affiliated with, endorsed by, or owned by any employer, and Ricardo Oliveira is
the sole copyright holder.

This Source Code Form is subject to the terms of the Mozilla Public License,
v. 2.0. If a copy of the MPL was not distributed with this file, You can obtain
one at <https://mozilla.org/MPL/2.0/>. The full license text is in
[LICENSE](LICENSE).
