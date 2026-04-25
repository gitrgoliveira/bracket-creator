# bracket-creator

<p align="center">
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
  <a href="https://goreportcard.com/report/github.com/gitrgoliveira/bracket-creator">
    <img src="https://goreportcard.com/badge/github.com/gitrgoliveira/bracket-creator" alt="Go report card">
  </a>
</p>

**bracket-creator** is a CLI and web application for generating kendo tournament brackets as Excel spreadsheets. Give it a CSV of participants and it produces a fully formatted, print-ready `.xlsx` file with pool draws, match schedules, and elimination trees.

## Formats

| Format | Command | Use when |
|--------|---------|----------|
| **Pools & Playoffs** | `create-pools` | Round-robin pools followed by a knockout bracket |
| **Playoffs Only** | `create-playoffs` | Direct single-elimination bracket |

## Quick start

```bash
# Install
brew install gitrgoliveira/tap/bracket-creator

# Create a pools + playoffs bracket
bracket-creator create-pools -f participants.csv -o tournament.xlsx

# Or run the web UI
bracket-creator serve
```

Open `tournament.xlsx` in Excel or LibreOffice and print.

## What you need on tournament day

- **A3 printer** — for team/player name sheets
- **A4 printer** — for the bracket trees
- Scoreboards, whiteboard markers, scissors, tasuki

### Keeping courts in sync

If you have multiple shiai-jo, upload the Excel file to Google Drive (or similar) so all tables share one live document.

## Before the tournament

1. **Collect participants** — one name per line in a CSV file ([input format](user-guide/input-format.md))
2. **Generate the bracket** — run `create-pools` or `create-playoffs` ([commands](user-guide/commands/create-pools.md))
3. **Optionally seed** top competitors so they land in separate pools/sides of the bracket ([seeding](user-guide/commands/create-pools.md#seeding))
4. **Print** — the Excel file is laid out to print cleanly on A4/A3
