# GEMINI.md

## Governance

Before implementing features or making architectural decisions, read the project constitution:
**`.specify/memory/constitution.md`** — defines the core principles (YAGNI, DRY, TDD, DDD, evidence-based decisions, bracket integrity, and live-tournament constraints) that all changes must comply with.

## Project Overview

`bracket-creator` is a specialized CLI and Web application designed to generate tournament brackets for Kendo competitions. It supports both straight knockout (Playoffs) and round-robin (Pools) formats, accommodating both individual and team matches.

### Key Technologies
- **Language:** Go (Golang)
- **CLI Framework:** [Cobra](https://github.com/spf13/cobra)
- **Web Framework:** [Gin](https://github.com/gin-gonic/gin)
- **Excel Manipulation:** [Excelize](https://github.com/xuri/excelize/v2)
- **Frontend (bracket generator):** Vanilla HTML/JS embedded in the Go binary (`web/`)
- **Frontend (mobile/live app):** Preact + JSX compiled by esbuild, embedded in the Go binary (`web-mobile/`)
- **Containerization:** Docker & Docker Compose

### Architecture
- `cmd/`: CLI command definitions (serve, create-playoffs, create-pools, mobile-app, version).
- `internal/`: Core logic and domain models.
    - `domain/`: Domain entities (Tournament, Pool, Match, Player, Seed).
    - `helper/`: Implementation of bracket generation, pool creation, Excel file creation, and business logic.
    - `excel/`: Lower-level Excel client and styling logic.
    - `resources/`: Management of embedded assets (both `web/` and `web-mobile/`).
    - `mobileapp/`: Gin HTTP handlers for the live tournament app. Handlers split by entity: `handlers_competition.go`, `handlers_match.go`, `handlers_participants.go`, `handlers_tournament.go`. Real-time updates via SSE (`hub.go`). Password auth middleware (`middleware.go`).
    - `state/`: File-backed store for the mobile app. Reads/writes `tournament.md` and per-competition `config.md` + `participants.csv` in the data folder.
    - `engine/`: Thin adapter bridging `state.Competition` to `internal/helper` pool/bracket generation.
- `web/`: Frontend assets for the bracket-generator web UI, embedded via `go:embed`.
- `web-mobile/`: Preact/JSX frontend for the live tournament mobile app, embedded via `go:embed`. Pre-compiled to `web-mobile/dist/` by esbuild. Key components: `LinedTextarea` (numbered participant input), admin dashboard, live score editor, public viewer.
- `tests/`: Integration tests for the Web API and CLI.
- `specs/`: OpenAPI specification (`openapi.yaml`) for the web API, fully synchronized with the backend implementation.

### Seeding Logic
- **Playoffs (`StandardSeeding`)**: Uses a power-of-2 bracket distribution (e.g., seeds 1 and 2 on opposite halves). Includes displaced seed placement using a furthest-distance heuristic for out-of-range seeds.
- **Pools (`PoolSeeding`)**: Distributes seeds across pools using an "extremes and middle" balanced priority distribution (e.g., for 12 pools: Pool 1, Pool 12, Pool 6, Pool 7), with cyclic priority for additional seeds.

### Pool Scoring Rules
Rankings within pools are determined by the following criteria:

**Individual Tournaments:**
1. Higher number of fights won (Matches Won).
2. Lower number of fights lost (Matches Lost).
3. Higher number of hikiwake (Matches Tied).
4. Higher number of points scored (Points Won).
5. Lower number of points lost (Points Against).

**Team Tournaments:**
1. Higher number of team matches won (W).
2. Lower number of team matches lost (L).
3. Higher number of draws in team matches (T).
4. Higher number of individual winners (IV).
5. Lower number of individual losses (IL).
6. Higher number of individual draws (IT).
7. Higher number of points scored (PW).
8. Lower number of points lost (PL).
- **Automated Ranking Formulas:** Pool standings are automatically calculated using weighted composite scores.
    - **Individual Formula:** `(W*1,000,000)-(L*10,000)+(T*100)+(PW*1)-(PL*0.01)`
    - **Team Formula:** Uses an 8-tier hierarchical composite score representing the official tie-breaking rules.
    - **Deterministic Tie-Breaking:** `RANK.EQ` combined with `COUNTIF` ensures unique rankings even in case of identical composite scores.
    - **Reactive Propagation:** The "Ranking" summary section uses `INDEX/MATCH` formulas to automatically pull names from the Results table based on their calculated rank.
    - **Operator Override:** Operators can manually intervene by typing over the formula in the "Rank" column; the Ranking section will automatically update to reflect the manual entry.

### Team Match Winning Criteria
Individual encounters between teams are decided by:
1. Highest number of individual winners (Victories).
2. Highest number of points scored.
3. If still tied, the match is a draw in pool play, or proceeds to a play-off in elimination rounds.

### Tie-marking Rule
A match (individual or sub-match) is ONLY considered a tie if the operator enters an **'X'** (or 'x') in the "vs" column between the players. Equal scores without an 'X' are NOT treated as ties. The "vs" column is unlocked on all sheets to facilitate this.

### Match Colors
On tree and playoff brackets, the player/team on the top of the bracket is always assigned the color **Red (Aka)** and the player/team on the bottom is assigned **White (Shiro)**.

### Excel Layout Standards
- **Court Structure:** Each court uses exactly **8 columns**. (Court A: A–H, Court B: I–P, etc.)
- **Column Widths:** The first and seventh columns of each court (e.g., A and G) are 30 units wide for names/ranks. Intermediate columns (B–F and H) are 5 units wide.
- **Pool Spacing:** A single blank row is maintained between the end of one pool and the start of the next to improve readability.
- **Team Labels:** Team elimination match results use **"IV"** (Individual Victories) and **"PW"** (Points Won) as headers in the summary box.

## Building and Running

### Prerequisites
- Go 1.26.3+
- Make

### Key Commands
- **Build the application:** `make go/build` (outputs to `./bin/bracket-creator`)
- **Run the Web UI (bracket generator):** `make run` or `./bin/bracket-creator serve`
- **Run the mobile/live app:** `make run-mobile` (default: `./tournament-data`, port 8080)
  - Override port: `PORT=8082 make run-mobile`
  - Override data dir: `TOURNAMENT_DATA_DIR=/path make run-mobile`
  - The binary reads `PORT`, `BIND_ADDRESS`, and `TOURNAMENT_DATA_DIR` directly, so the env vars also apply when running without `make` (e.g. `TOURNAMENT_DATA_DIR=/path bracket-creator mobile-app`). Explicit `--port`/`--bind`/`--folder` flags win.
- **Run Tests (fast):** `make go/test`
- **Run Tests (with race detection):** `make go/test-race`
- **Run Linters:** `make go/lint`
- **Generate Examples:** `make examples`
- **Build Docker Image:** `make docker/build`

### CLI Usage Examples
- **Create Playoffs:**
  ```bash
  ./bin/bracket-creator create-playoffs -f players.csv -o bracket.xlsx
  ```
- **Create Pools:**
  ```bash
  ./bin/bracket-creator create-pools -p 3 -w 2 -f players.csv -o pools.xlsx
  ```

## Development Conventions

- **Code Style:** Follow standard Go idioms. Use `go fmt` and `golangci-lint` for consistency.
- **Dependency Management:** Use `go mod tidy` to manage `go.mod` and `go.sum`.
- **Testing:**
    - New features should include unit tests in the same directory as the implementation.
    - Integration tests should be added to the `tests/` directory.
    - Ensure tests pass with: `make go/test`.
- **Embedded Assets:** Static files in `web/` are embedded via `//go:embed`. The Excel workbook is built entirely from code in `internal/excel/template.go`. After modifying embedded web assets, rebuild with `go build` (or `make go/build`).
- **CI/CD:** GitHub Actions are used for validation (`.github/workflows/validate.yaml`), including security scans (`gosec`), linting, and coverage reporting via Codecov.
- **Git:** Never commit changes directly to `main` without a PR. Ensure the build and tests pass before requesting a review.

## PR Workflow

- **Test plan is a gate, not a formality.** Before requesting review on a PR, check off EVERY item in the PR description's test plan. Do not mark a PR ready while any checkbox is unverified. Manual/browser steps are not optional — execute them, then check them.
- **Keep the issue (bead) `in_progress` until the PR actually merges.** A green review is not a merge. Only close the issue after the merge lands, with a reason referencing the merge commit/PR.
- **After a merge, run full cleanup**: close the issue → fast-forward `main` → remove the worktree → delete the local and remote branch → prune.

## Code Review

- **Never report an automated-review round "clean" until a fresh fetch shows zero unresolved threads.** State the total unresolved count first, give every thread an explicit disposition (fix, or dismissal with a reason), then re-verify the count is zero.
- When re-requesting a GitHub Copilot review, use the REST endpoint (the `gh pr edit --add-reviewer` form lowercases the login and fails):
  `gh api repos/<owner>/<repo>/pulls/<pr>/requested_reviewers -X POST -f "reviewers[]=Copilot"`
- Run `make go/test` after fixes and before pushing — a red gate means fix-or-revert, never push.

## Testing & Verification

- **Verify in the browser, never substitute API/curl calls.** Manual test-plan items and UAT must be executed through the actual UI.
- **Test self-run / public features from the PUBLIC page, not the admin UI** — the public flow is what users hit; admin-side scoring proves nothing about it.
- **File gap/UX issues incrementally as you find them**, not batched at the end of a UAT pass.
- Frontend changes under `web-mobile/` require a rebuild to take effect (`//go:embed`); use `make run-mobile` or rebuild + restart.

## Merge & Rebase

When rebasing or resolving conflicts, watch for these recurring breakages:
- Duplicate declarations introduced by the rebase (same symbol defined twice after a merge).
- UUID-vs-name-string mismatches in player/entity maps — match on id OR name, and use participant UUIDs (not display names) for bracket-highlight IDs.
- Re-run `make go/test` after every rebase; a clean rebase that compiles can still be semantically broken.


<!-- BEGIN BEADS INTEGRATION v:1 profile:minimal hash:7510c1e2 -->
## Beads Issue Tracker

This project uses **bd (beads)** for issue tracking. Run `bd prime` to see full workflow context and commands.

### Quick Reference

```bash
bd ready              # Find available work
bd show <id>          # View issue details
bd update <id> --claim  # Claim work
bd close <id>         # Complete work if the PR is merged
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
