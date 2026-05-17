# Agent Guide: Bracket Creator

High-signal instructions for AI agents working in this repository.

## Governance

Before implementing features or making architectural decisions, read the project constitution:
**`.specify/memory/constitution.md`** — defines the core principles (YAGNI, DRY, TDD, DDD, evidence-based decisions, bracket integrity, and live-tournament constraints) that all changes must comply with.

## Core Technical Context
- **Domain Logic:** Primarily in `internal/helper/`. A new `internal/domain/` package is being introduced to decouple logic from Excel formatting; use it for new pure-domain models but check `helper` for existing Excel-linked logic.
- **Excel Generation:** Uses `excelize/v2`. The workbook is constructed from scratch in `internal/excel/template.go`. Coordinates and formulas are hardcoded in `internal/helper/` (e.g., `tree.go`, `excel.go`). Layout/page-break constants live in `internal/helper/constants.go`.
- **Binary Trees:** Brackets are recursive binary trees (`Node` struct in `helper/tree.go`).
- **Paging:** `helper.MaxPlayersPerTree = 16`. Brackets larger than 16 are subdivided into multiple sheets (pages) unless `--single-tree` is used.
- **Embedding:** Both `web/*` and `web-mobile/*` are embedded via `//go:embed` in `main.go`. Rebuild with `make go/build` after modifying any web assets.
- **Court limit:** A–Z labels mean `--courts` is rejected if greater than 26.
- **Excel Layout:** Standardized on an **8-column per court** structure. Column A (Red Name) and Column G (White Name/Rank) are set to 30 units wide. Columns B–F and H are 5 units wide.
- **Pool Spacing:** There is exactly one blank row of space between the end of one pool's ranking summary and the start of the next pool's header.
- **API Documentation:** The OpenAPI specification for the web API is located in `specs/openapi.yaml` and is fully synchronized with the backend implementation.
- **Mobile App (`mobile-app` command):** A live tournament management server serving a Preact/JSX UI from `web-mobile/`. State is file-backed: `tournament-data/tournament.md` (YAML) and `tournament-data/competitions/<id>/config.md` + `participants.csv`. Backend in `internal/mobileapp/` (Gin handlers) and `internal/state/` (store). Real-time updates via SSE. Admin actions require `X-Tournament-Password` header. Run with `make run-mobile`.
- **Live Updates:** Real-time updates via SSE. Admin actions require `X-Tournament-Password` header. Run with `make run-mobile`.
- **Pool Scoring Rules:**
    - **Individual:** 1. Fights Won, 2. Fights Lost, 3. Hikiwake, 4. Points Scored, 5. Points Lost.
    - **Team:** 1. Team W, 2. Team L, 3. Team T, 4. Individual Winners (IV), 5. Individual Losses (IL), 6. Individual Ties (IT), 7. Points Won (PW), 8. Points Lost (PL).
- **Team Match Winning Criteria:**
    1. Highest number of individual winners.
    2. Highest number of points scored.
    3. If tied: draw in pools, play-off in playoffs.
- **Team Elimination Labels:** In team elimination match summaries, "V" is labeled as **"IV"** (Individual Victories) and "P" as **"PW"** (Points Won).
- **Match Colors:** On tree/playoff brackets, the player on the top is Red (Aka) and the bottom is White (Shiro).
- **Tie-marking Rule:** A match is only considered a tie (hikiwake) if an **'X'** is entered in the "vs" column. This column is unlocked on all sheets.
- **Automated Pool Ranking:** Pool standings are calculated using weighted composite formulas in Excel/Google Sheets. The "Rank" column in the Results table is the source of truth for the "Ranking" section, which uses reactive `INDEX/MATCH` lookups. Operators can manually override rankings by typing over the formula in the "Rank" column.

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
- **Mobile app (local):** `make run-mobile` (default data dir `./tournament-data`, port 8080)
- **Mobile app (custom port/dir):** `PORT=8082 TOURNAMENT_DATA_DIR=/path make run-mobile`

<!-- BEGIN BEADS INTEGRATION v:1 profile:minimal hash:7510c1e2 -->
## Beads Issue Tracker

This project uses **bd (beads)** for issue tracking. Run `bd prime` to see full workflow context and commands.

### Quick Reference

```bash
bd ready              # Find available work
bd show <id>          # View issue details
bd update <id> --claim  # Claim work
bd close <id>         # Complete work
```

### Rules

- Use `bd` for ALL task tracking — do NOT use TodoWrite, TaskCreate, or markdown TODO lists
- Run `bd prime` for detailed command reference and session close protocol
- Use `bd remember` for persistent knowledge — do NOT use MEMORY.md files

**Architecture in one line:** issues live in a local Dolt DB; sync uses `refs/dolt/data` on your git remote; `.beads/issues.jsonl` is a passive export. See https://github.com/gastownhall/beads/blob/main/docs/SYNC_CONCEPTS.md for details and anti-patterns.

## Session Completion

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **PUSH TO REMOTE** - This is MANDATORY:
   ```bash
   git pull --rebase
   git push
   git status  # MUST show "up to date with origin"
   ```
5. **Clean up** - Clear stashes, prune remote branches
6. **Verify** - All changes committed AND pushed
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds
<!-- END BEADS INTEGRATION -->
