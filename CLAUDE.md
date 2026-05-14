# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Governance

Before implementing features or making architectural decisions, read the project constitution:
**`.specify/memory/constitution.md`** — defines the core principles (YAGNI, DRY, TDD, DDD, evidence-based decisions, bracket integrity, and live-tournament constraints) that all changes must comply with.

## Project Overview

A Go CLI and web application for generating kendo tournament brackets as Excel spreadsheets. Supports two formats: **Pools & Playoffs** (round-robin pools then knockout) and **Playoffs Only** (direct elimination). Input is CSV, output is Excel with formula-linked cells for bracket visualization. The web API is documented via an OpenAPI specification in `specs/openapi.yaml`.

## Build & Test Commands

```bash
make go/build          # Build binary to bin/bracket-creator
make go/test           # Lint + security scan + tests with coverage
make go/test-race      # Lint + tests with race detection (slow)
make go/lint           # golangci-lint only
make run               # Build and start web server (localhost:8080)
PORT=8081 make run      # Use alternate port
make run-mobile        # Build and start the mobile/live app (localhost:8080, ./tournament-data)
PORT=8082 make run-mobile   # Use alternate port
TOURNAMENT_DATA_DIR=/path make run-mobile  # Custom data folder
make examples          # Generate example Excel files from mock data

# Run a single test
go test -run TestName ./internal/helper/...
go test -run TestName ./cmd/...

# Run a single package's tests
go test -cover ./internal/helper/...
```

## Architecture

### Dual Domain Model (In Transition)

- **`internal/helper`** — Where the actual logic lives. Types here include Excel coordinates (`sheetName`, `cell` fields) tightly coupled to output generation. This is the primary implementation.
- **`internal/domain`** — Clean domain models (Player, Pool, Tournament, Match, Seed) being phased in gradually. Don't confuse these with the helper types.

### Package Responsibilities

- **`cmd/`** — Cobra CLI commands. Each uses an options struct with a `run()` method. `create-pools` and `create-playoffs` share significant logic. Shared helpers (`processEntries`, `openOutputFile`, `assignPlayerNumbers`) live in `cmd/shared.go`.
- **`internal/helper/`** — Core business logic: CSV parsing, pool/match generation, tree building, seeding algorithms, and all Excel rendering. This is the largest package.
- **`internal/excel/`** — Excel file lifecycle (`Client`), sheet operations (`SheetManager`), style definitions.
- **`internal/service/`** — Service layer abstraction over helper logic.
- **`internal/resources/`** — Embedded file management. Resources flow: `main.go` embeds → `resources.NewResources()` → `cmd.ExecuteWithResources()`.
- **`internal/mobileapp/`** — Gin HTTP handlers for the live tournament app (`mobile-app` command). Routes: `handlers_competition.go`, `handlers_match.go`, `handlers_participants.go`, `handlers_tournament.go`. Real-time push via SSE (`hub.go`). Auth via `X-Tournament-Password` header (`middleware.go`).
- **`internal/state/`** — File-backed state store for the mobile app. Tournament and competition config lives in `tournament-data/tournament.md` and `tournament-data/competitions/<id>/config.md` (YAML front-matter). Participants are in `participants.csv` alongside each config.
- **`internal/engine/`** — Thin adapter that drives `internal/helper` pool/bracket generation from a `state.Competition`. Called by the `POST /api/competitions/:id/start` handler.
- **`web-mobile/`** — Preact/JSX frontend for the mobile app, served embedded in the binary. Entry point: `web-mobile/index.html`. JS modules in `web-mobile/js/` (`admin.js`, `viewer.js`, `app.js`, `api.js`, `data.js`, `bracket.js`). CSS in `web-mobile/css/styles.css`. Pre-compiled to `web-mobile/dist/` by esbuild (run automatically as part of `make go/build`). Key component: `LinedTextarea` in `admin.js` — shows numbered line gutter alongside the participant paste box.

### Key Algorithms

- **Binary tree brackets** (`helper/tree.go`): `Node` struct with `Left`/`Right` children, recursive subdivision for multi-page output. `maxPlayersPerTree = 16`.
- **Seeding** (`helper/seed.go`): `StandardSeeding()` uses `generateBracketOrder()` for placement. `ApplySeeds()` handles collisions by swapping seed values. Names must match exactly (case-sensitive).
- **Pool creation** (`helper/tournament.go`): Greedy algorithm with dojo-conflict avoidance. Pools distributed contiguously across courts (Shiaijo).
- **Court-aware seeding** (`helper/seed.go`): `PoolSeeding(players, numPools, numCourts)` interleaves seeded players so that after `ReorderPoolsForCourts` the top seeds land in different courts and on opposite ends of each court's bracket.
- **Pool Scoring & Tie-breaking**:
    - **Individual Tournaments**:
        1. Higher number of fights won.
        2. Lower number of fights lost.
        3. Higher number of hikiwake (Matches Tied).
        4. Higher number of points scored.
        5. Lower number of points lost.
    - **Team Tournaments**:
        1. Higher number of team matches won (W).
        2. Lower number of team matches lost (L).
        3. Higher number of draws in team matches (T).
        4. Higher number of individual winners (IV).
        5. Lower number of individual losses (IL).
        6. Higher number of individual draws (IT).
        7. Higher number of points scored (PW).
        8. Lower number of points lost (PL).
- **Team Match Winning Criteria**: An encounter between two teams is decided by:
    1. Highest number of individual winners (Victories).
    2. Highest number of points scored.
    3. If both are equal, it's a draw (Tie) in pools, or requires a play-off (Encho) in elimination matches.
- **Tie-marking Rule**: A match (individual or sub-match) is a tie when the operator enters **'X'** (or 'x') in the "vs" column, **or when both sides finish with the same total score** (equal character-count after stripping spaces/zeros/dashes). For auto-detection, at least one score cell in that row must be non-empty. A team match is automatically a draw (T=1) when both IV and PW totals are equal.
- **Match Colors**: White (Shiro) is always the left column and Red (Aka) is always the right column — this is fixed and not configurable. In pool matches the first-listed player (SideA) is Red; in elimination matches the upper-bracket player (`node.Left`) is Red.
- **Excel Layout**: Uses an **8-column per court** layout. Columns A and G (and their court-shifted counterparts) are 30 units wide; others are 5 units wide. A blank row separates pools vertically.
- **Team Match Labels**: Summaries use **"IV"** (Individual Victories) and **"PW"** (Points Won).
- **Court limit**: courts are labelled A–Z, so `--courts` is hard-capped at 26 and any value over that returns an error rather than silently truncating.

### Excel workbook construction

The workbook is built entirely from code in `internal/excel/template.go` (`NewFileFromScratch`). Each sheet (data, Time Estimator, Pool Draw, Pool Matches, Elimination Matches, Names to Print, Tree) is created and styled programmatically. Layout constants (rows-per-page, spacing, max bracket size) and sheet name constants (`SheetData`, `SheetPoolDraw`, etc.) live in `internal/helper/constants.go` — use these constants everywhere rather than string literals.

### Resource Embedding

`main.go` embeds `web/*` via `//go:embed`. The global var `helper.WebFs` exists for backward compatibility with code paths that still reference it directly. Must rebuild after changing embedded files.

## Testing Conventions

- **Table-driven tests** with `t.Run()` subtests throughout (see `seed_test.go`, `tree_test.go`)
- **Package naming**: `_test` suffix for external tests of `domain`; same package (`package helper`, `package cmd`) for `helper` and `cmd` tests
- **Test helpers**: `internal/test/helpers.go` has factories (`CreateTestPlayers`, `CreateTestPools`, `CreateTestTournament`)
- **Assertions**: `testify/assert` for non-fatal, `require` for fatal
- **Cleanup**: Always use `defer` for temp files, servers, and env vars

## Participant CSV Schema (canonical)

`participants.csv` stores one participant per line. Two formats are supported:

**With UUIDs (new format)** — first field is a UUID v4 (lowercase hex):
```
<uuid>, Name[, Zekken/DisplayName], Dojo[, DanGrade][, tag]
```

**Without UUIDs (legacy format)** — detected automatically when first field is not a UUID:
```
Name[, Zekken/DisplayName], Dojo[, DanGrade][, tag]
```

- The zekken/display-name column is only present when `withZekkenName=true` for the competition.
- `DanGrade` is optional; omit or leave empty.
- `tag` is the last column when present and must be one of: `manual`, `registered`, `transfer`.
- Seeds are stored separately in `seeds.csv` and merged at load time — do **not** include seed ranks in `participants.csv`.
- The Go parser lives in `internal/state/participants.go`; the JS parser in `web-mobile/js/data.js:parseParticipantLines`. Keep both in sync with this schema when changing column layout.

## Common Pitfalls

- Excel coordinates matter: changing match generation requires updating cell references and formula links across sheets
- `team-matches=0` means individual tournaments, not team tournaments
- The `errcheck` linter is enabled (test files excepted). Don't introduce `_ =` or bare ignored returns in production code — wrap and propagate, or log via `handleExcelError`/`handleExcelDataError`
- Web UI changes (`web/index.html`) should be validated in a running browser, not just by reading diffs — use `make run`
- Mobile app frontend changes (`web-mobile/`) require rebuilding the binary to take effect — the files are embedded at `go build` time via `//go:embed web-mobile/*` in `main.go`. Run `make run-mobile` which rebuilds automatically, or run `make go/build` then restart.
- Duplicate participant names in the CSV are rejected up front by `helper.CheckDuplicateEntries`; the web handler surfaces these to the user
- Chained match navigation in the admin score editor (Prev/Next buttons, Finish + Start Next, ←/→ keys) must stay on the current match's shiaijo — operators run matches per-court, so hopping courts mid-flow breaks the workflow. See `AdminScoreEditor` in `web-mobile/js/admin_schedule.jsx`: filter to `(m.court || "") === (openMatch.court || "")` so empty/undefined courts share one "unassigned" bucket.


# Validation

All changes must be validated with `make go/test` and inspection of the generated example files from `make examples`. Pay attention to page breaks and seeding. You can change the code of `scratch/inspect.go` or generate your own.

<!-- SPECKIT START -->
For additional context about technologies to be used, project structure,
shell commands, and other important information, read the current plan
<!-- SPECKIT END -->
