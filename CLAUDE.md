# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

A Go CLI and web application for generating kendo tournament brackets as Excel spreadsheets. Supports two formats: **Pools & Playoffs** (round-robin pools then knockout) and **Playoffs Only** (direct elimination). Input is CSV, output is Excel with formula-linked cells for bracket visualization.

## Build & Test Commands

```bash
make go/build          # Build binary to bin/bracket-creator
make go/test           # Lint + security scan + tests with coverage
make go/test-race      # Lint + tests with race detection (slow)
make go/lint           # golangci-lint only
make run               # Build and start web server (localhost:8080)
PORT=8081 make run      # Use alternate port
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

- **`cmd/`** — Cobra CLI commands. Each uses an options struct with a `run()` method. `create-pools` and `create-playoffs` share significant logic.
- **`internal/helper/`** — Core business logic: CSV parsing, pool/match generation, tree building, seeding algorithms, and all Excel rendering. This is the largest package.
- **`internal/excel/`** — Excel file lifecycle (`Client`), sheet operations (`SheetManager`), style definitions.
- **`internal/service/`** — Service layer abstraction over helper logic.
- **`internal/resources/`** — Embedded file management. Resources flow: `main.go` embeds → `resources.NewResources()` → `cmd.ExecuteWithResources()`.

### Key Algorithms

- **Binary tree brackets** (`helper/tree.go`): `Node` struct with `Left`/`Right` children, recursive subdivision for multi-page output. `maxPlayersPerTree = 16`.
- **Seeding** (`helper/seed.go`): `StandardSeeding()` uses `generateBracketOrder()` for placement. `ApplySeeds()` handles collisions by swapping seed values. Names must match exactly (case-sensitive).
- **Pool creation** (`helper/tournament.go`): Greedy algorithm with dojo-conflict avoidance. Pools distributed contiguously across courts (Shiaijo).
- **Court-aware seeding** (`helper/seed.go`): `PoolSeeding(players, numPools, numCourts)` interleaves seeded players so that after `ReorderPoolsForCourts` the top seeds land in different courts and on opposite ends of each court's bracket.
- **Court limit**: courts are labelled A–Z, so `--courts` is hard-capped at 26 and any value over that returns an error rather than silently truncating.

### Excel workbook construction

The workbook is built entirely from code in `internal/excel/template.go` (`NewFileFromScratch`). Each sheet (data, Time Estimator, Pool Draw, Pool Matches, Elimination Matches, Names to Print, Tree) is created and styled programmatically. Layout constants (rows-per-page, spacing, max bracket size) live in `internal/helper/constants.go`.

### Resource Embedding

`main.go` embeds `web/*` via `//go:embed`. The global var `helper.WebFs` exists for backward compatibility with code paths that still reference it directly. Must rebuild after changing embedded files.

## Testing Conventions

- **Table-driven tests** with `t.Run()` subtests throughout (see `seed_test.go`, `tree_test.go`)
- **Package naming**: `_test` suffix for external tests of domain/internal; same package for cmd tests
- **Test helpers**: `internal/test/helpers.go` has factories (`CreateTestPlayers`, `CreateTestPools`, `CreateTestTournament`)
- **Assertions**: `testify/assert` for non-fatal, `require` for fatal
- **Cleanup**: Always use `defer` for temp files, servers, and env vars

## Common Pitfalls

- Excel coordinates matter: changing match generation requires updating cell references and formula links across sheets
- `team-matches=0` means individual tournaments, not team tournaments
- The `errcheck` linter is enabled (test files excepted). Don't introduce `_ =` or bare ignored returns in production code — wrap and propagate, or log via `handleExcelError`/`handleExcelDataError`
- Web UI changes (`web/index.html`) should be validated in a running browser, not just by reading diffs — use `make run`
- Duplicate participant names in the CSV are rejected up front by `helper.CheckDuplicateEntries`; the web handler surfaces these to the user


# Validation

All changes must be validated with `make go/test` and inspection of the generated example files from `make examples`. Pay attention to page breaks and seeding. You can change the code of `scratch/inspect.go` or generate your own.
