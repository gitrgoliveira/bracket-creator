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

[Install](#install) bracket-creator first (the Docker image starts the server for you), then with a binary install start the server:

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

**Docker is the recommended way to run bracket-creator on every platform.** Homebrew, `apt`, and `dnf` are the other main options, shown below. More methods are covered in the [install guide](https://gitrgoliveira.github.io/bracket-creator/user-guide/install/install/).

<details>
  <summary><b>Docker</b> (recommended)</summary>

Pull and run the tournament app image; tournament state lives in the mounted folder:

```bash
docker run -p 8080:8080 -v "$PWD/tournament-data:/tournament-data" \
  ghcr.io/gitrgoliveira/bracket-creator-mobile:latest
```

Then open [http://localhost:8080](http://localhost:8080).

Three image variants are published: `bracket-creator-mobile` (tournament app), `bracket-creator-mobile-pdf` (tournament app plus LibreOffice for PDF export), and `bracket-creator` (the legacy Excel-generator web UI). See the [hosting guide](https://gitrgoliveira.github.io/bracket-creator/user-guide/install/hosting/) for deployment details.

</details>

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
  <summary><b><code>apt</code> (Debian/Ubuntu)</b></summary>

From the next release onwards, `.deb` packages for amd64 and arm64 are attached to the [release page](https://github.com/gitrgoliveira/bracket-creator/releases). Download the one for your architecture and install it (`apt` resolves dependencies for local files):

```bash
sudo apt install ./bracket-creator_*_amd64.deb
```

The package installs the binary, the man page, and shell completions.

</details>

<details>
  <summary><b><code>dnf</code> (Fedora/RHEL)</b></summary>

From the next release onwards, `.rpm` packages for x86_64 and aarch64 are attached to the [release page](https://github.com/gitrgoliveira/bracket-creator/releases). Download the one for your architecture and install it:

```bash
sudo dnf install ./bracket-creator-*.x86_64.rpm
```

The package installs the binary, the man page, and shell completions.

</details>

### Upgrading

There is no hosted `apt`/`yum` package repository, so Linux package upgrades are not automatic. To upgrade:

* **Docker**: `docker pull` the image again and recreate the container.
* **Homebrew**: `brew upgrade bracket-creator`.
* **`apt`/`dnf`**: download the new release's package and install it the same way.

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
