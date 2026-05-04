# Agent Guide: Bracket Creator

High-signal instructions for AI agents working in this repository.

## Core Technical Context
- **Domain Logic:** Primarily in `internal/helper/`. A new `internal/domain/` package is being introduced to decouple logic from Excel formatting; use it for new pure-domain models but check `helper` for existing Excel-linked logic.
- **Excel Generation:** Uses `excelize/v2`. The workbook is constructed from scratch in `internal/excel/template.go`. Coordinates and formulas are hardcoded in `internal/helper/` (e.g., `tree.go`, `excel.go`). Layout/page-break constants live in `internal/helper/constants.go`.
- **Binary Trees:** Brackets are recursive binary trees (`Node` struct in `helper/tree.go`).
- **Paging:** `helper.MaxPlayersPerTree = 16`. Brackets larger than 16 are subdivided into multiple sheets (pages) unless `--single-tree` is used.
- **Embedding:** Only `web/*` is embedded via `//go:embed` in `main.go`. Rebuild with `make go/build` after modifying web assets.
- **Court limit:** A–Z labels mean `--courts` is rejected if greater than 26.
- **API Documentation:** The OpenAPI specification for the web API is located in `specs/openapi.yaml`.
- **Pool Scoring Rules:**
    - **Individual:** 1. Fights Won, 2. Fights Lost, 3. Hikiwake, 4. Points Scored, 5. Points Lost.
    - **Team:** 1. Team W, 2. Team L, 3. Team T, 4. Individual Winners (IV), 5. Individual Losses (IL), 6. Individual Ties (IT), 7. Points Won (PW), 8. Points Lost (PL).
- **Team Match Winning Criteria:**
    1. Highest number of individual winners.
    2. Highest number of points scored.
    3. If tied: draw in pools, play-off in playoffs.
- **Match Colors:** On tree/playoff brackets, the player on the top is Red (Aka) and the bottom is White (Shiro).
- **Tie-marking Rule:** A match is only considered a tie (hikiwake) if an **'X'** is entered in the "vs" column. This column is unlocked on all sheets.

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
