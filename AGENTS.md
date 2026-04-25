# Agent Guide: Bracket Creator

High-signal instructions for AI agents working in this repository.

## Core Technical Context
- **Domain Logic:** Primarily in `internal/helper/`. A new `internal/domain/` package is being introduced to decouple logic from Excel formatting; use it for new pure-domain models but check `helper` for existing Excel-linked logic.
- **Excel Generation:** Uses `excelize/v2`. The workbook is constructed from scratch in `internal/excel/template.go`. Coordinates and formulas are hardcoded in `internal/helper/` (e.g., `tree.go`, `excel.go`). Layout/page-break constants live in `internal/helper/constants.go`.
- **Binary Trees:** Brackets are recursive binary trees (`Node` struct in `helper/tree.go`).
- **Paging:** `helper.MaxPlayersPerTree = 16`. Brackets larger than 16 are subdivided into multiple sheets (pages) unless `--single-tree` is used.
- **Embedding:** Only `web/*` is embedded via `//go:embed` in `main.go`. Rebuild with `make go/build` after modifying web assets.
- **Court limit:** A–Z labels mean `--courts` is rejected if greater than 26.

## Developer Workflow
- **Standard Verification:** `make go/test` (runs lint + security + tests).
- **Visual Verification:** `make examples` is **critical**. It generates `.xlsx` files from mock data. Inspect these to verify bracket layout, page breaks, and formula links.
- **Race Detection:** Use `make go/test-race` for concurrency-heavy changes (though most logic is currently sequential).
- **Run Web UI:** `make run` (starts on `localhost:8080`). Use `PORT=8081 make run` to override.

## Common Pitfalls
- **Case Sensitivity:** Seeding names must match the participant list *exactly*.
- **Team Matches:** `team-matches=0` is the default for individual tournaments.
- **Shiaijo (Courts):** `--courts` defaults to 2. It controls both pool distribution and tree labeling.
- **Dojo Conflicts:** The `Dojo` field in CSVs is used *only* for pool randomization to avoid early teammate matches; it's not used in playoffs-only mode.
- **Workbook construction:** All sheets/styles are emitted by `internal/excel/template.go` and `internal/helper/excel_styles.go`. To change global appearance, edit these — there is no template binary.
- **Duplicates:** Participant CSVs are checked for duplicate names; both CLI and Web UI return an error before generating output.

## Useful Commands
- **Lint only:** `make go/lint`
- **Security only:** `make go/security`
- **Single test:** `go test -v -run <TestName> ./internal/helper/...`
- **Generate examples:** `make examples`
