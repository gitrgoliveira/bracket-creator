---
template: home.html
---

<p class="bc-badges" markdown>
  <a href="https://github.com/gitrgoliveira/bracket-creator/releases">
    <img alt="GitHub release" src="https://img.shields.io/github/v/release/gitrgoliveira/bracket-creator?include_prereleases">
  </a>
  <a href="https://github.com/gitrgoliveira/bracket-creator/actions/workflows/validate.yaml">
    <img src="https://github.com/gitrgoliveira/bracket-creator/actions/workflows/validate.yaml/badge.svg" alt="CI">
  </a>
  <a href="https://codecov.io/gh/gitrgoliveira/bracket-creator">
    <img src="https://codecov.io/gh/gitrgoliveira/bracket-creator/branch/main/graph/badge.svg?token=CLP6KW4QLK" alt="Coverage">
  </a>
  <a href="https://pkg.go.dev/github.com/gitrgoliveira/bracket-creator">
    <img src="https://pkg.go.dev/badge/github.com/gitrgoliveira/bracket-creator.svg" alt="Go reference">
  </a>
</p>

**bracket-creator** lets any club or organisation run kendo tournaments at whatever level of digitization fits the venue. Give it a CSV of participants and it produces fully formatted, print-ready Excel brackets (pool draws, match schedules, and elimination trees), and it can run pools and scores on the day. Choose how digital you go.

!!! tip "New here? Start with these"
    Not sure where to begin? [Choosing your setup](user-guide/start-here/choosing-your-setup.md) narrows it down in two questions, and [Your first tournament](user-guide/start-here/first-tournament.md) walks you from an empty folder to results on a screen.

## Three ways to run a tournament

The same toolkit scales from a fully printed event to a fully online one. Pick the mode that matches your venue and equipment.

<div class="grid cards bc-modes" markdown>

-   **Offline**

    ---

    No internet required. Generate the brackets and score sheets as an Excel file, print them, and run the whole day on paper.

    *Needs:* an A4/A3 printer.

    [Generate a bracket](user-guide/organisers/web-ui.md)

-   **Partially connected**

    ---

    Internet is available but there are no display screens. Keep every shiai-jo in sync through a shared Google Sheet or the tournament app, with one device per court. Some printed material is still needed for scoreboards and competitors.

    *Needs:* one device per shiai-jo.

    [Tournament app](user-guide/organisers/run-tournament.md)

-   **Fully digital**

    ---

    On-screen scoreboards and mobile result pages, updated in real time. Organisers still print player tags and numbers.

    *Needs:* a device and monitor per court, plus network access for competitors.

    [Host it online](user-guide/install/hosting.md)

</div>

## Tools

Three programs ship in the single binary. The three ways to run a tournament decide how digital your event is; these tools are what you actually run to do it.

<div class="grid cards" markdown>

-   **CLI**

    ---

    Generate print-ready Excel brackets from the command line.

    `create-pools` (pools + knockout) · `create-playoffs` (straight knockout)

    [Command reference](user-guide/commands/create-pools.md)

-   **Bracket generator web UI**

    ---

    Browser-based bracket generation, no CSV editing needed.

    `serve`

    [Web UI guide](user-guide/organisers/web-ui.md)

-   **Tournament app**

    ---

    Run pools and scores on the day, in real time on any device.

    `mobile-app`

    [Tournament app guide](user-guide/organisers/run-tournament.md)

</div>

## Quick start

The simplest way to run the tournament app, no Go toolchain needed:

1. Download the binary for your platform from the [releases page](https://github.com/gitrgoliveira/bracket-creator/releases).
2. Start the app:

    ```bash
    bracket-creator mobile-app --folder ./tournament-data
    ```

3. Open [http://localhost:8080](http://localhost:8080) and follow the setup in the [tournament app guide](user-guide/organisers/run-tournament.md).

Prefer Go or Docker? See the [install options](user-guide/install/install.md).

## What you need on tournament day

- **A3 printer**: for team/player name sheets
- **A4 printer**: for the bracket trees
- Scoreboards, whiteboard markers, scissors, tasuki

### Keep courts in sync

If you have multiple shiai-jo, upload the Excel file to Google Drive (or similar) so every table works from the same document.

## Before the tournament

1. **Collect participants**: one name per line in a CSV file ([input format](user-guide/organisers/input-format.md))
2. **Generate the bracket**: run `create-pools` or `create-playoffs` ([commands](user-guide/commands/create-pools.md))
3. **Optionally seed** top competitors so they land in separate pools/sides of the bracket ([seeding](user-guide/commands/create-pools.md#seeding))
4. **Print**: the Excel file is laid out to print cleanly on A4/A3

## On the day

Use the **tournament app** to manage pools and scores in real time across all devices on your network (no Excel required on the day). See the [tournament app guide](user-guide/organisers/run-tournament.md).
