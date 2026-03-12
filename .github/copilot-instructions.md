# Bracket Creator — Workspace Instructions

A Go CLI tool for generating kendo tournament brackets with pool stages and playoff knockouts. Outputs Excel files with formula-linked cells for bracket visualization. Includes a web UI (Gin) for interactive tournament creation.

## Architecture

### Dual Domain Model (In Transition)
- **Legacy**: `internal/helper` types include Excel coordinates (`sheetName`, `cell` fields) — **still the primary implementation**
- **Modern**: `internal/domain` types are clean domain models — being phased in gradually
- When working with tournament logic, expect Excel coordinate coupling in helper package types

### Excel-Centric Design
- Business logic is tightly coupled to Excel generation
- Players, pools, matches carry Excel sheet references for formula outputs
- Binary tree structures (`internal/helper/tree.go`) build playoff brackets recursively
- `excel.Client` manages file lifecycle, `excel.SheetManager` handles sheet operations

### Key Packages
- `cmd/` — Cobra commands with options structs (each has a `run()` method)
- `internal/domain` — Domain models (Player, Pool, Tournament, Match, Seed)
- `internal/service` — Business logic orchestration
- `internal/helper` — **Core logic**: CSV parsing, pool/match generation, tree building, Excel rendering
- `internal/excel` — Excel file management (Client, SheetManager, StyleManager)
- `internal/resources` — Embedded files (web UI, Excel templates) injected via `ExecuteWithResources()`

### Seeding System
- `StandardSeeding()` positions seeded players using `generateBracketOrder()`
- `ApplySeeds()` handles collisions by **swapping seed values**, not failing
- Seed validation: case-sensitive exact name matching with uniqueness checks

## Build and Test

### Essential Commands
```bash
# Install dependencies
make local/deps

# Run tests (includes linting + race detection)
make go/test

# Build locally
make go/build

# Run web server
make run
# or manually: bin/bracket-creator serve

# If 8080 is occupied
PORT=8081 make run

# Generate example files
make examples

# Docker
make docker/run
```

### Web UI Validation Workflow
- For changes in `web/index.html`, validate behavior in the running app, not only by reading file diffs.
- Use `make run` (or `PORT=<port> make run` when 8080 is busy), then exercise the changed flow with Playwright click interactions.
- Prefer concise, aggregated informational messages over repetitive per-line warnings in client-side validation feedback.

### Test Execution
- `make go/test` runs: `go test -race -cover ./cmd/... ./internal/... ./tests/...`
- Linting is a pre-check (golangci-lint)
- Coverage reported to Codecov in CI

## Testing Conventions

### Test Organization
- **Package naming**: Use `_test` suffix for external tests of domain/internal (e.g., `package domain_test`); same package for cmd tests (e.g., `package cmd`)
- **Co-location**: Test files alongside source files (`*_test.go`)

### Test Patterns
- **Table-driven tests**: Use `t.Run()` with subtests for multiple cases — see [internal/helper/seed_test.go](internal/helper/seed_test.go), [cmd/serve_test.go](cmd/serve_test.go)
- **Test helpers**: Leverage `internal/test/helpers.go` factories (`CreateTestPlayers`, `CreateTestPools`, `CreateTestTournament`, `CreateTestFS`)
- **Assertions**: `github.com/stretchr/testify/assert` for non-fatal checks, `require` for fatal errors
- **Mocking**: Manual mocks with `testify/mock` (no code generation tools)

### Test Structure
- Always test both success and error paths
- Use `defer` for cleanup (files, servers, resources)
- Use `testing/fstest.MapFS` for filesystem mocking
- Use `httptest` for HTTP server testing
- Clean up temporary resources and environment variables in defer blocks

## Code Style

### Go Conventions
- Follow standard Go idioms (use `gofmt`, golangci-lint enforces rules)
- Cobra commands use options structs to hold flags and state
- Domain types include self-validation methods (e.g., `SeedAssignment.Validate()`)
- Resource injection: embed files flow through `resources.Resources` → commands → services

### File Organization
- Embedded resources defined in `main.go` via `//go:embed`
- Commands call `ExecuteWithResources()` to receive embedded files
- Completions and manpages generated via `scripts/`

## Important Notes

### Excel Formulas
- Brackets use Excel formulas to link cells across sheets
- When modifying match generation, verify formula references are correct
- The tree structure (`Node` in tree.go) drives playoff bracket layout

### Seeding Edge Cases
- Seed assignments must exactly match participant names (case-sensitive)
- Duplicate seed ranks are rejected
- Empty seed ranks place participants in unseeded pool

### Web UI
- Seeding modal enforces strict validation before submission
- CSV drag-and-drop supported in player/team list textarea
- Environment variables: `BIND_ADDRESS`, `PORT` for server configuration
- Additional Web UI-specific rules live in `.github/instructions/web-ui.instructions.md` (applies to `web/index.html`)

## Common Pitfalls

- **Don't confuse domain packages**: `internal/domain` is aspirational, `internal/helper` is where logic lives
- **Excel coordinates matter**: Changing match generation requires updating cell references
- **Resource embedding**: Must rebuild after changing web/ or embedded files
- **Test cleanup**: Always use defer to clean up files, otherwise tests leave artifacts
- **Team matches**: `team-matches=0` means individual tournaments, not team tournaments

## Further Reading

- [README.md](../README.md) — CLI usage, quickstart, Docker setup
- [docs/dev-guide/](../docs/dev-guide/) — Contributing guidelines, code of conduct
- [specs/](../specs/) — Feature specifications with data models and plans
