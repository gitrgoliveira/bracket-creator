# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Governance

Before implementing features or making architectural decisions, read the project constitution:
**`.specify/memory/constitution.md`** — defines the core principles (YAGNI, DRY, TDD, DDD, evidence-based decisions, bracket integrity, and live-tournament constraints) that all changes must comply with.

## Project Overview

A Go CLI and web application for generating kendo tournament brackets as Excel spreadsheets. Supports multiple competition formats: **Playoffs** (direct elimination), **Mixed** (round-robin pools then knockout), **League** (full round-robin), and **Swiss** (Swiss-system across N rounds). Input is CSV, output is Excel with formula-linked cells for bracket visualization. The web API is documented via an OpenAPI specification in `specs/openapi.yaml`.

## Build & Test Commands

```bash
make go/build          # Build binary to bin/bracket-creator
make go/test           # Lint + security scan + tests with coverage
make go/test-race      # Lint + tests with race detection (slow)
make go/lint           # golangci-lint only
make run               # Build and start web server (localhost:8080)
PORT=8081 make run      # Use alternate port (also works direct: PORT=8081 ./bin/bracket-creator serve)
make run-mobile        # Build and start the mobile app (localhost:8080, ./tournament-data)
PORT=8082 make run-mobile   # Use alternate port (also works direct: PORT=8082 ./bin/bracket-creator mobile-app)
TOURNAMENT_DATA_DIR=/path make run-mobile  # Custom data folder (also works without make: TOURNAMENT_DATA_DIR=/path ./bin/bracket-creator mobile-app)
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
- **`internal/mobileapp/`** — Gin HTTP handlers for the tournament app (`mobile-app` command). Routes: `handlers_competition.go` (including `POST /api/competitions/:id/generate-draw` [status `draw-ready`] and `DELETE /api/competitions/:id/draw` to discard the draw), `handlers_match.go`, `handlers_participants.go` (including single participant PUT updates, individual/bulk check-ins `POST /api/competitions/:id/participants/checkin-bulk`), `handlers_tournament.go`, `handlers_swiss.go` (`generate-round`, `standings`), `handlers_decision.go` (kiken/fusenpai/daihyosen — `POST /api/matches/:mid/decision`), `handlers_eligibility.go` (`/api/competitions/:id/competitor-status`), `handlers_lineup.go` (team lineups), `handlers_schedule.go` (`GET /api/schedule/estimate`, public), `handlers_reset.go` (`POST /api/tournament/reset`, public — for forgotten admin passwords; 404s in locked mode), `handlers_auth_config.go` (`GET /api/auth-config`, public — reports auth mode to the SPA). Real-time push via SSE (`hub.go`) with events: `match_updated`, `competitor_status_updated`, `competition_completed`, `swiss_round_generated`, etc. Auth via `X-Tournament-Password` header (`middleware.go`), with two modes selected at startup by a `PasswordVerifier` (`auth_source.go`): **file mode** (default — plaintext compare against `tournament.md`) or **locked mode** (`--lock-password` flag + `TOURNAMENT_PASSWORD_HASH` env var — bcrypt compare, `POST /api/tournament/reset` returns 404; the SPA `/reset` page still renders an operator-disabled message). Consumer-boundary interfaces live in `deps.go` (NFR-002).
- **`internal/state/`** — File-backed state store for the mobile app. Tournament and competition config lives in `tournament-data/tournament.md` and `tournament-data/competitions/<id>/config.md` (YAML front-matter). Participants are in `participants.csv` alongside each config.
- **`internal/engine/`** — Thin adapter that drives `internal/helper` pool/bracket generation from a `state.Competition`. Called by the `POST /api/competitions/:id/start` handler.
- **`web-mobile/`** — Preact/JSX frontend for the mobile app, served embedded in the binary. Entry point: `web-mobile/index.html`. JS modules in `web-mobile/js/` are grouped by prefix: `admin_*.jsx` (the operator console — setup, participants, pools, scoring, scheduling, lineups, etc.), `viewer_*.jsx` and `display_*.jsx` (public attendee/spectator surfaces), and shared infrastructure (`app.jsx`, `api_client.jsx`, `api_serializers.jsx`, `bracket.jsx`, `data.jsx`, `patch.jsx`, `router.jsx`, `ui.jsx`, `glossary.jsx`). Run `ls web-mobile/js/` for the current set rather than relying on an enumerated list here. Note `admin_participants.jsx` holds the `LinedTextarea` gutter participant paste box and the check-in filter list. CSS in `web-mobile/css/styles.css`. Pre-compiled to `web-mobile/dist/` by esbuild (run automatically as part of `make go/build`).

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
- **Match Decision Types** (`internal/domain/decision.go`): 10 canonical wire values — `""` (none), `"fought"`, `"hikiwake"` (draw), `"kiken"` (legacy withdrawal, maps to kiken-voluntary on YAML load), `"kiken-voluntary"` (FIK Art. 31, permanent), `"kiken-injury"` (FIK Art. 30, reinstateable), `"fusenpai"` (no-show), `"fusensho"` (per-bout default win), `"daihyosen"` (rep bout), `"kachinuki-exhaustion"`. Use `domain.IsKikenDecision(d)` / `domain.IsKikenDecisionStr(s)` to check any kiken variant. Legacy YAML `decision: true` migrates to `"hikiwake"`, `false` to `"fought"`, `"kiken"` to `"kiken-voluntary"` (Decision.UnmarshalYAML). Visual suffixes in the UI: `Kiken`, `Fus.`, `DH`, `(E)` for encho.
- **Competitor Eligibility** (`internal/state/competitor_status.go`, `internal/engine/eligibility.go`): a kiken/fusenpai decision auto-writes a `CompetitorStatus{Eligible: false}` for the loser; `engine.StartMatch(compID, matchID)` is the pre-flight gate that returns `*IneligibleCompetitorError` (matches `errors.Is(err, ErrIneligibleCompetitor)`). Maps to HTTP 409. Kiken-injury (FIK Art. 30) sets `CompetitorStatus.Reinstateable: true`; the admin can call `POST /api/competitions/:cid/competitors/:pid/reinstate` to restore eligibility. Kiken-voluntary (Art. 31) and fusenpai are not reinstateable.
- **Team Lineups & Kachinuki** (`internal/domain/team_lineup.go`, `internal/engine/kachinuki.go`): TeamLineup pins position→player for a round. FIK 5-person rule: Senpo + Taisho mandatory; 1 vacancy must be Jiho, 2 must be Jiho+Fukusho, 3+ disqualifies. Kachinuki ("winner-stays-on") dynamically appends bouts via `engine.AdvanceKachinuki` until one team is exhausted (`DecisionKachinukiExhaustion`).
- **Schedule Estimator** (`internal/engine/schedule.go`): `EstimateSchedule(EstimateInput) ScheduleEstimate` produces total/per-court minutes from match duration × multiplier × slowest-court buffer. Exposed via stateless `GET /api/schedule/estimate` on both the CLI web server and the mobile app.
- **Store Transactions** (`internal/state/transactions.go`): `Store.WithTransaction(compID, fn)` holds the per-comp lock once across multiple load/save operations. Use the `StoreTx` handle inside `fn` — do NOT call public Store methods (they would deadlock the non-reentrant mutex).

### Excel workbook construction

The workbook is built entirely from code in `internal/excel/template.go` (`NewFileFromScratch`). Each sheet (data, Time Estimator, Pool Draw, Pool Matches, Elimination Matches, Names to Print, Tree) is created and styled programmatically. Layout constants (rows-per-page, spacing, max bracket size) and sheet name constants (`SheetData`, `SheetPoolDraw`, etc.) live in `internal/helper/constants.go` — use these constants everywhere rather than string literals.

### Resource Embedding

`main.go` embeds `web/*` via `//go:embed`. The global var `helper.WebFs` exists for backward compatibility with code paths that still reference it directly. Must rebuild after changing embedded files.

### Mobile-app runtime defaults

Production-hardening defaults applied in the `mobile-app` command. Constants live in [cmd/mobile_app.go](cmd/mobile_app.go) and [internal/mobileapp/middleware.go](internal/mobileapp/middleware.go) / [hub.go](internal/mobileapp/hub.go):

| Concern | Default | Override | Rationale |
|---|---|---|---|
| `ReadHeaderTimeout` | 10s | — | Slowloris-header defense |
| `ReadTimeout` | 30s | — | Slow-body defense (still permits multi-MB CSV import) |
| `IdleTimeout` | 120s | — | Bounds fd commitment per idle keep-alive client |
| `WriteTimeout` | **0** (unbounded) | — | SSE streams are infinite; per-request cancellation runs via `Request.Context().Done()` |
| `MaxHeaderBytes` | 1 MB | — | Header-bomb defense |
| Body cap (admin JSON) | 1 MB | `DefaultMaxBodyBytes` const | `c.BindJSON` payloads are tiny in practice; cap is enforced by `MaxBodyBytes` middleware (returns 413) |
| Body cap (`/tournament/import`) | 64 MB | `MaxImportBodyBytes` const | Matches `ParseMultipartForm` already in the handler |
| SSE subscribers | 5000 | `SSE_MAX_CLIENTS` env var | Bounds fan-out cost + per-client goroutine/channel allocation (~4–10 KB resident per client); raised from 1000 → 5000 by mp-9afd for large-scale events (1000+ viewers); real hardware load test still required |
| Graceful shutdown | 30s | `httpShutdownTimeout` const | `Hub.Close` is wired via `srv.RegisterOnShutdown` so SSE goroutines exit before the deadline |

**`safeGo` convention.** Any goroutine spawned inside a request handler MUST use the `safeGo` helper in [internal/mobileapp/safego.go](internal/mobileapp/safego.go). Gin's Recovery middleware only catches panics on the request goroutine — a panic in a spawned goroutine crashes the entire process. The helper guarantees `wg.Done()` on panic and captures the recovered value into a shared `atomic.Pointer[recoveredPanic]` so the handler can return a single HTTP 500 without leaking internals. Pattern:

```go
var wg sync.WaitGroup
var panicRef atomic.Pointer[recoveredPanic]
safeGo(&wg, &panicRef, func() { /* spawned work */ })
wg.Wait()
if p := panicRef.Load(); p != nil {
    c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
    return
}
```

See `handlers_viewer.go` for the canonical use sites (mp-663 Phase 1).

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
<uuid>, Name[, Zekken/DisplayName], Dojo[, DanGrade][, source]
```

**Without UUIDs (legacy format)** — detected automatically when first field is not a UUID:
```
Name[, Zekken/DisplayName], Dojo[, DanGrade][, source]
```

- The zekken/display-name column is only present when `withZekkenName=true` for the competition.
- `DanGrade` is optional; omit or leave empty.
- `source` is the last column when present and must be one of: `manual`, `registered`, `transfer`. This is the registration provenance (admin-only). It is distinct from the competitor's "tag" (their assigned competitor number, which is the `Number`/`number` field, optionally prefixed via `numberPrefix` — e.g. "A1").
- Seeds are stored separately in `seeds.csv` and merged at load time — do **not** include seed ranks in `participants.csv`.
- The Go parser lives in `internal/state/participants.go`; the JS parser in `web-mobile/js/data.jsx:parseParticipantLines`. Keep both in sync with this schema when changing column layout.

## Common Pitfalls

- Excel coordinates matter: changing match generation requires updating cell references and formula links across sheets
- `team-matches=0` means individual tournaments, not team tournaments
- The `errcheck` linter is enabled (test files excepted). Don't introduce `_ =` or bare ignored returns in production code — wrap and propagate, or log via `handleExcelError`/`handleExcelDataError`
- Web UI changes (`web/index.html`) should be validated in a running browser, not just by reading diffs — use `make run`
- Mobile app frontend changes (`web-mobile/`) require rebuilding the binary to take effect — the files are embedded at `go build` time via `//go:embed web-mobile/*` in `main.go`. Run `make run-mobile` which rebuilds automatically, or run `make go/build` then restart.
- Duplicate participant names in the CSV are rejected up front by `helper.CheckDuplicateEntries`; the web handler surfaces these to the user
- Chained match navigation in the admin score editor (Prev/Next buttons, Finish + Start Next, ←/→ keys) must stay on the current match's shiaijo — operators run matches per-court, so hopping courts mid-flow breaks the workflow. See `AdminScoreEditor` in `web-mobile/js/admin_schedule.jsx`: filter to `(m.court || "") === (openMatch.court || "")` so empty/undefined courts share one "unassigned" bucket.

## PR Workflow

- **Build the PR body from the repo template.** When creating a PR, populate the description from `.github/pull_request_template.md` and fill every section — `gh pr create --body-file <filled-template>` (the bare `gh pr create` / `--fill` does NOT apply the template). Set the `Closes mp-xxxx` bead reference.
- **Embed screenshots via the `pr-assets` side branch, not gists** (`gh gist create` rejects binaries). Push the PNG to the `pr-assets` branch (never merged to main): `gh api --method PUT .../contents/pr-assets/<pr>/shot.png -f branch=pr-assets -f content="$(base64 < shot.png | tr -d '\n')"`, then embed `![](https://raw.githubusercontent.com/gitrgoliveira/bracket-creator/pr-assets/pr-assets/<pr>/shot.png)`. A real browser/MCP screenshot is MANDATORY for any UI change — there is NO textual/DOM/geometry substitute. If you have not captured one, the PR is not review-ready: capture it first, then fill the Screenshots section. Full verified recipe: the `/pr-screenshots` skill. **Capture only via the browser/MCP screenshot tools — NEVER a desktop or full-screen grab (`screencapture`, `scrot`, OS shortcuts), which exposes the user's private screen.**
- **Test plan is a gate, not a formality.** Before requesting review on a PR, check off EVERY item in the PR description's test plan. Do not mark a PR ready while any checkbox is unverified. Manual/browser steps are not optional — execute them, then check them.
- **Keep the bead `in_progress` until the PR actually merges.** A green review is not a merge. Only `bd close <id>` after the merge lands, with a reason referencing the merge commit/PR.
- **After a merge, run the full `/cleanup` sequence** (close bead → fast-forward main → remove worktree → delete local + remote branch → prune). Don't wait to be asked for each step. See the `/cleanup` skill.
- **Verify the worktree/branch before any edit.** This repo uses a git worktree per PR; edits applied to the wrong worktree (or directly to the `main` checkout) force patch-and-revert recovery. When there is any ambiguity, confirm with `pwd` and `git branch --show-current` before the first Edit/Write, and never edit the main checkout directly — always work inside a worktree.

## Code Review (Copilot)

- **Never report a review round "clean" until a fresh fetch shows zero unresolved threads.** State the total unresolved count first, give every thread an explicit disposition (fix or dismissal with a reason), then re-verify the count is zero. The `/review-loop` skill encodes the full loop.
- **Re-request Copilot via the GraphQL `requestReviews` mutation with the bot node id** — both `gh pr edit --add-reviewer Copilot` (lowercases the login, fails) and REST `POST .../requested_reviewers -f "reviewers[]=Copilot"` (silently no-ops; that array is users-only and Copilot is a Bot) are broken. Full recipe + verification step in the `/review-loop` skill (rule 4).
- Run `make go/test` after fixes and before pushing — a red gate means fix-or-revert, never push.

## Testing & Verification

- **Verify in the browser, never substitute API/curl calls.** Manual test-plan items and UAT must be executed through the actual UI.
- **Test self-run / public features from the PUBLIC page, not the admin UI** — the public flow is what users hit; admin-side scoring proves nothing about it.
- **File gap/UX issues incrementally as you find them**, not batched at the end of a UAT pass.
- Frontend changes under `web-mobile/` require a rebuild to take effect (`//go:embed`); use `make run-mobile` or rebuild + restart.
- **Diagnose failures from evidence, never fabricate a cause.** When a test, build, or CI step fails (Codecov, GPG, lint, etc.), read the actual logs before explaining it. Do not invent "known bugs", version-specific regressions, or other rationalizations to justify a workaround — if the root cause isn't established, say so and keep investigating.
- **Test coverage gate: every package that has test files must maintain ≥85% statement coverage.** Verify before any PR with:
  ```bash
  go test -race -cover . ./cmd/... ./internal/... ./tests/...
  ```
  Packages below 85% must be brought up before merging. New packages must include test files covering their public API. Tracked in bead mp-3abe.
  **Intentionally untested:** `internal/domain/internal/glossarygen` is a `go generate` code-generator (emits `glossary_data.js`); it has no exported API and is excluded from the gate. `internal/helper/bracket`, `internal/helper/csv`, and `internal/helper/seeding` are empty stub packages (no exported symbols yet) and are likewise excluded.

## Merge & Rebase

When rebasing or resolving conflicts, watch for these recurring breakages:
- Duplicate declarations introduced by the rebase (same symbol defined twice after a merge).
- UUID-vs-name-string mismatches in player/entity maps — match on id OR name, and use participant UUIDs (not display names) for bracket-highlight IDs.
- Missed call sites when removing or renaming a symbol — `grep -r` the name across **all** packages **including `_test.go` files** before committing; a refactor that compiles can still leave stale test references or skip-test code pointing at dead paths.
- Re-run `make go/test` after every rebase; a clean rebase that compiles must not be semantically broken.


# Validation

All changes must be validated with `make go/test` and inspection of the generated example files from `make examples`. Pay attention to page breaks and seeding. You can change the code of `scratch/inspect.go` or generate your own.

<!-- SPECKIT START -->
For additional context about technologies to be used, project structure,
shell commands, and other important information, read the current plan:
`specs/003-tournament-gap-closure/plan.md`
<!-- SPECKIT END -->


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
