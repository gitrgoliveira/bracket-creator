# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Git / Worktree Rules

- **Edit only inside the correct worktree, never the main checkout.** This repo uses a git worktree per PR, under `.claude/worktrees/`. Before the first edit or git operation, confirm the checkout with `git rev-parse --show-toplevel` and the branch with `git branch --show-current`. Edits landing in the wrong worktree (or the `main` checkout) force costly patch-and-revert recovery.
- The number of tokens used to edit files is best minimized, all else being equal. Therefore, when it will not affect the end result, try to surgically edit a file rather than rewrite the entire thing.
- **Never `cd` inside a Bash command to reach a repo.** Address the worktree explicitly (`git -C <abs-path>`, `make -C <abs-path>`) or run from its root. A `cd` in a chain has sent git to the wrong checkout and left the shell there for later calls.

## Tool selection (read before every tool call on a code file)

This project uses Serena, an MCP server exposing semantic, symbol-aware tools. **Serena's tools are the PRIMARY tools for code work here.** The built-in Read, Glob, Grep and Edit are SECONDARY and must not be used on code files when a Serena equivalent exists.

**Serena only serves the languages `.serena/project.yml` lists**, so check that file rather than assuming coverage. It IS tracked in git as of PR #439 (`.gitignore` ignores `.serena/*` and re-includes that one file) and carries `language_servers: [go, typescript, scss, terraform, yaml]`. Tracking was the only available fix: Serena generates the file per project DIRECTORY (`project_serena_folder_location: "$projectDir/.serena"`) and this repo uses a worktree per PR, so before #439 every new worktree auto-detected `[go]` alone, and editing the main checkout's copy propagated to nothing. **But the active set is bound when a session activates the project, not when the file appears**, so a worktree predating #439 stays Go-only until a session starts with the file — measured here, where a mid-session fast-forward brought the file in and `get_symbols_overview` on a `.jsx` still failed with `Active language servers: ['go']`. When it matters, ask Serena rather than the file: the error names the active set. For any source that set does not cover there is no language server, the symbol tools below cannot work, and you should use the built-in Read/Edit/Glob/Grep on it directly instead of spending a Serena call that cannot succeed. On `.jsx` the JS server reads and edits symbols correctly, and `web-mobile/jsconfig.json` gives it a project, so `find_referencing_symbols` follows ES `import`s across modules, tests included. Without that file it saw only the symbol's own file (`resultSlot` missed its real importer, `match_scoreboard.jsx`), so a reference search from a checkout that lacks it proves nothing. It still cannot follow wiring that has no import to follow: about a third of the top-level `js/*.jsx` modules carry no ES `import` at all, cross-module wiring leans on `window.*` globals (`window.API` alone has over 200 references outside the tests), and Preact is a vendored UMD global rather than an npm dependency. Treat a reference search there as a lower bound and grep as well for anything reachable through `window.*`. Keep that jsconfig to the options it has: esbuild reads it too, and the Makefile passes only `--jsx-factory`/`--jsx-fragment`, so an option those flags do not override changes the shipped bundle (`"jsx": "react-jsx"` there makes the compiled JSX import `react/jsx-runtime`, which the page does not load).

The built-in tool descriptions in your context say things like "use Read for a known path" and "prefer dedicated tools (Read, Edit, Write, Glob, Grep)". Those are written for projects without Serena and are SUPERSEDED here; when they conflict with this section, this section wins. Do not rationalize the built-in tools with "the file is small", "I already know what I need", "this is one call versus three", or "the path is known" — those rationalizations have produced incorrect behaviour before and are explicitly disallowed.

| Task | Tool |
|---|---|
| See a code file's structure | `get_symbols_overview` |
| Read a specific symbol's body | `find_symbol` (`include_body=true`) |
| Find a symbol by name across the repo | `find_symbol` |
| Find references / callers | `find_referencing_symbols` |
| Find declarations / implementations | `find_declaration` / `find_implementations` |
| Edit a symbol's body | `replace_symbol_body` |
| Insert near a symbol | `insert_before_symbol` / `insert_after_symbol` |
| Pattern replace inside a file | `replace_content` |
| Pattern replace across files | `replace_in_files` |
| Rename a symbol | `rename_symbol` |
| Delete a symbol | `safe_delete_symbol` |
| Compiler/linter errors for a file | `get_diagnostics_for_file` |
| Compiler/linter errors for one symbol | `get_diagnostics_for_symbol` |
| Language server gone stale (after a rebase, a branch switch, a large generated change) | `restart_language_server`, only after asking the user |

Only the 15 tools in this table are exposed, plus `initial_instructions` (Serena's manual) and `open_dashboard` (the `claude-code` context; memory tools are disabled because Hindsight owns memory). The last two rows are Serena's optional tools, switched on in the user's `~/.serena/serena_config.yml` (`included_optional_tools`), so a session started before that file changed does not have them. Serena's wider tool set is NOT available: there is no `inline_symbol`, `type_hierarchy`, `move_symbol`, `delete_lines` or `replace_lines` — do not call them. Serena's `editing` mode tells you not to re-run the build or tests after a successful `rename_symbol` or `safe_delete_symbol`; this file's test rules win (see **Gate cadence**).

Built-in Read/Edit/Glob/Grep are permitted on code files ONLY when: the file's language is not in `language_servers` (see above — no Serena call is owed on those); Serena was tried on the target and failed; the file is not parseable as code; you need a regex search across many files that the symbolic tools cannot express (Grep is fine as a discovery step, but follow-up reads/edits on matched served files still go through Serena); you need a few lines and a symbolic read would be overkill; or you genuinely must read the whole file. They are always fine for non-code files: markdown, JSON, YAML, TOML, config, lockfiles, plain text, images.

**Before editing code Serena serves:** `get_symbols_overview` on the target file (skip if already done this session) → `find_symbol` with `include_body=true` for the symbols you will touch, reading only those symbols rather than the whole file → edit with `replace_symbol_body`, `insert_before_symbol`, `insert_after_symbol` or `replace_content`.

**Self-check before every Read, Glob, Grep or Edit call:** does this target a file whose language `language_servers` lists, and does the table above name a Serena tool for the task? If yes, switch. Every time, not once per session.

## Workflow Rules

### Planning vs. Implementation

When asked to *plan*, *verify*, or *record a plan on a bead*: do NOT write code. Produce the plan, write it to the bead, and stop. Never discard uncommitted work (a stash, a checkout over a dirty tree, a reset) without explicit approval; capture a diff first if work must be set aside.

### Keep the orchestrating session small

Every model call re-reads the session's whole context, this file included (about 17k tokens on its own since the domain rules moved to `.claude/rules/`), so a session's cost grows with its length. One session that ran a multi-bead PR and its review loops made 209 calls at an average of about 360k tokens of context, and never compacted (2026-10-11).
- **Delegate review rounds, investigations, history lookups and verification sweeps to subagents, and keep only their short summary** (findings, decisions, `file:line`) in the main session: ask the subagent for a summary in a set number of lines, not its full report (operator feedback 2026-09-13: "Make better usage of sub agents").
- **Subagents run on Sonnet unless you say otherwise** (`CLAUDE_CODE_SUBAGENT_MODEL=sonnet` in the user settings, 2026-10-12). That suits searches, history lookups, reference sweeps and verification. Pass `model: "opus"` for an implementer and for the final review round; the pr-review-toolkit agents are unaffected, since each names its own model (`opus`, or `inherit` for the session's). The ones that drop to Sonnet are `general-purpose`, `Explore`, `claude` and `beads:task-agent`.
- **Start a new session for each PR.** Once a PR merges, begin the next bead's work in a fresh session; the bead and the PR carry the state.

## Governance

Before implementing features or making architectural decisions, read the project constitution:
**`.specify/memory/constitution.md`**: defines the core principles (YAGNI, DRY, TDD, DDD, evidence-based decisions, bracket integrity, and live-tournament constraints) that all changes must comply with. The constitution is tracked in git. Feature plans under `specs/<n>-.../` are gitignored (only `specs/openapi.yaml` and `specs/*/screenshots/` are tracked by the rules), so a worktree or fresh clone has no plan file, except the few that were force-added before the rule (`git ls-files specs` lists them, e.g. `specs/007-ekc-draw/spec.md`). Those are tracked and read, so a change that contradicts one corrects it in the same PR.

## Project Overview

A Go CLI and web application for generating kendo tournament brackets as Excel spreadsheets. Supports multiple competition formats: **Knockout** (direct elimination), **Mixed** (round-robin pools then knockout), **League** (full round-robin), and **Swiss** (Swiss-system across N rounds). Input is CSV, output is Excel with formula-linked cells for bracket visualization. The web API is documented via an OpenAPI specification in `specs/openapi.yaml`.

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
make mobile-app-example  # Build, start the mobile app and seed a full demo tournament through its API (fixture driver for UAT)
make examples          # Generate example Excel files from mock data

make docs/build        # Build + strict-validate the MkDocs site (the PR-template gate)
make docs/serve        # Serve docs locally with live reload
make docs/deps         # Create .venv-docs from docs/requirements.txt
make docs/clean        # Remove .venv-docs and the built site/
# The MkDocs targets above run through .venv-docs (pinned mkdocs + Material
# from docs/requirements.txt). The system-PATH mkdocs drifts (older, no
# Material), so never call mkdocs directly for verification; use make docs/build.

make docs/screenshots  # Recapture the docs screenshots (NAME=, FAMILY=, SINCE=main to scope)
make docs/videos       # Recapture the docs videos
make docs/media        # Both. After a change to what a user sees: make docs/media SINCE=main
# The capture harness (scripts/screenshots/, its README is the guide) writes to
# scripts/screenshots/out/ and lists only the screenshots that changed; review
# those and copy them into docs/. Videos are never compared: copy one only when
# the change alters what it shows.

# Run a single test
go test -run TestName ./internal/helper/...
go test -run TestName ./cmd/...

# Run a single package's tests
go test -cover ./internal/helper/...
```

## Architecture

### Dual Domain Model (In Transition)

- **`internal/helper`**: Where the actual logic lives. Types here include Excel coordinates (`sheetName`, `cell` fields) tightly coupled to output generation. This is the primary implementation.
- **`internal/domain`**: Clean domain models (Player, Pool, Tournament, Match, Seed) being phased in gradually. Don't confuse these with the helper types.

### Package Responsibilities

- **`cmd/`**: Cobra CLI commands. Each uses an options struct with a `run()` method. `create-pools` and `create-knockout` share significant logic. Shared helpers (`processEntries`, `openOutputFile`, `resolveNumberPrefix`) live in `cmd/shared.go`; competitor numbering itself is `helper.AssignPlayerNumbers`.
- **`internal/helper/`**: Core business logic: CSV parsing, pool/match generation, tree building, seeding algorithms, and all Excel rendering. This is the largest package.
- **`internal/excel/`**: Excel file lifecycle (`Client`), the from-scratch workbook template (`NewFileFromScratch`), style definitions.
- **`internal/service/`**: Service layer abstraction over helper logic.
- **`internal/resources/`**: Embedded file management. Resources flow: `main.go` embeds → `resources.NewResources()` → `cmd.ExecuteWithResources()`.
- **`internal/mobileapp/`**: Gin HTTP handlers for the tournament app (`mobile-app` command). Routes: `handlers_competition.go` (including `POST /api/competitions/:id/generate-draw` [status `draw-ready`] and `DELETE /api/competitions/:id/draw` to discard the draw; a draw-ready competition is listed on the shiaijo operator views and its FIRST RECORDED result starts it -- a match left running or completed, NEVER a write that clears a result back to scheduled (the engine's result gate, which also discounts the byes a knockout draw resolves at generation time) -- via `engine.MaybeAutoCompletePools`'s `AutoCompleteStarted` outcome, which `tryAutoCompletePools` broadcasts as `competition_started`, so the desk's `POST .../start` is one of two doors, bc-prow ruling), `handlers_match.go`, `handlers_participants.go` (including single participant PUT updates, individual/bulk check-ins `POST /api/competitions/:id/participants/checkin-bulk`; self-run mode has no check-in, so the three check-in writes answer 409 `check_in_not_in_self_run` for every caller, and `state.Competition.CheckInApplies` is the one owner the draw, the Swiss field and the estimate ask, never `CheckInEnabled` itself), `handlers_tournament.go`, `handlers_swiss.go` (`generate-round`, `standings`), `handlers_decision.go` (kiken/fusenpai/fusensho/daihyosen: `POST /api/competitions/:id/matches/:mid/decision`; `handleBothSidesBarredHikiwake` runs before the endpoint's own `Validate()` -- which otherwise 400s `hikiwake` outright with "use /score for fought/hikiwake" -- as the one carve-out that accepts `decision: "hikiwake"`, no `decisionBy` needed, when both sides of a SCHEDULED pool or league match (`engine.IsPoolMatchID`, league reuses the same `"Pool "`-prefixed ids; Swiss ids never match and are not covered) are already barred; the write records `Status: completed, Decision: "hikiwake"` with no new `CompetitorStatus`. The same both-barred knockout match instead gets `ineligible_competitor` naming both sides and pointing at the earlier withdrawal or the draw), `handlers_eligibility.go` (`/api/competitions/:id/competitor-status`), `handlers_lineup.go` (team lineups), `handlers_schedule.go` (`GET /api/schedule/estimate`, public), `handlers_reset.go` (`POST /api/tournament/reset`, public, for forgotten admin passwords; 404s in locked mode), `handlers_auth_config.go` (`GET /api/auth-config`, public, reports auth mode to the SPA). Real-time push via SSE (`hub.go`) with events: `match_updated`, `competitor_status_updated`, `competition_completed`, `swiss_round_generated`, etc. Auth via `X-Tournament-Password` header (`middleware.go`), with two modes selected at startup by a `PasswordVerifier` (`auth_source.go`): **file mode** (default, plaintext compare against `tournament.md`) or **locked mode** (`--lock-password` flag + `TOURNAMENT_PASSWORD_HASH` env var, bcrypt compare, `POST /api/tournament/reset` returns 404; the SPA `/reset` page still renders an operator-disabled message). Consumer-boundary interfaces live in `deps.go` (NFR-002).
- **`internal/state/`**: File-backed state store for the mobile app. Tournament and competition config lives in `tournament-data/tournament.md` and `tournament-data/competitions/<id>/config.md` (YAML front-matter). Participants are in `participants.csv` alongside each config.
- **`internal/engine/`**: Thin adapter that drives `internal/helper` pool/bracket generation from a `state.Competition`. Called by the `POST /api/competitions/:id/start` handler.
- **`web-mobile/`**: Preact/JSX frontend for the mobile app, served embedded in the binary. Entry point: `web-mobile/index.html`. JS modules in `web-mobile/js/` are grouped by prefix: `admin_*.jsx` (the operator console: setup, participants, pools, scoring, scheduling, lineups, etc.), `viewer_*.jsx` and `display_*.jsx` (public attendee/spectator surfaces), and shared infrastructure (`app.jsx`, `api_client.jsx`, `api_serializers.jsx`, `bracket.jsx`, `data.jsx`, `patch.jsx`, `router.jsx`, `ui.jsx`, `glossary.jsx`). Run `ls web-mobile/js/` for the current set rather than relying on an enumerated list here. Note `admin_participants.jsx` holds the `LinedTextarea` gutter participant paste box; check-in is NOT there, it is owned by `admin_registration_desk.jsx`. CSS in `web-mobile/css/styles.css`. Pre-compiled to `web-mobile/dist/` by esbuild (run automatically as part of `make go/build`).

### Layering and on-disk state

Layered with no upward or circular dependencies: presentation (`cmd/`, `mobileapp/`, `web/`, `web-mobile/`) → business logic (`engine/`, `helper/`, `service/`) → persistence (`state/`, `excel/`) → domain models (`domain/`, the dependency leaf with zero internal deps). `mobileapp` → `engine` (via `deps.go` interfaces) and `state` (direct reads for viewer endpoints); `engine` → `helper` + `excel` + `state`.

Live-tournament state is file-backed under `tournament-data/` (owned by `internal/state/`). `docs/architecture/data-model.md` owns the model (the file layout, field lists, which records carry ids, what each file's columns mean), so add a fact there rather than growing a second copy here. The root holds `tournament.md`, `.wal/` (pending multi-file transactions, replayed on startup), `branding/` and `sponsors/`, and `competitions/<id>/`, whose `history/` keeps one append-only `.jsonl` per match.

### Domain rules (`.claude/rules/`)

The detailed rulings for each area live in `.claude/rules/`, one file per area, so a session pays for them only when it works there. Claude Code loads a file by itself when a built-in Read, Edit or Write touches a file matching its `paths`, **but a Serena call does not trigger that**: Read the file yourself before you change code in its area (a global guard refuses a Serena edit there until you have). Other agents never load them automatically.

| Before changing… | Read |
|---|---|
| How a draw places, seeds or numbers competitors; pool bout ids | `.claude/rules/draw-and-seeding.md` |
| How a result is recorded, validated or shown (decisions, the middle mark, hantei, side colours, operator labels) | `.claude/rules/match-results-and-marks.md` |
| Any match write, its answer, the offline queue, a score editor's save path | `.claude/rules/match-writes.md` |
| Team scoring, team members, lineups, kachinuki | `.claude/rules/teams-and-lineups.md` |
| Withdrawals, bars, reinstatement, starting a match, send back to queue | `.claude/rules/eligibility-and-queue.md` |
| `internal/state` (transactions, caches, directories, startup checks) | `.claude/rules/store.md` |
| Any workbook output (layout, owners, sheets, examples) | `.claude/rules/workbook.md` |

### Key Algorithms

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
    3. If both are equal, it's a draw (Tie) in pools, or goes to a daihyosen (representative bout) in elimination matches (FIK Article 8-1; see "Where a mark lands" in `.claude/rules/match-results-and-marks.md`).
- **Tie-marking Rule**: A match (individual or sub-match) is a tie when the operator enters **'X'** (or 'x') in the "vs" column or when both sides finish with the same total score (equal character-count after stripping spaces/zeros/dashes). For auto-detection, at least one score cell in that row must be non-empty. A team match is automatically a draw (T=1) when both IV and PW totals are equal.
- **Team Match Labels**: Summaries use **"IV"** (Individual Victories) and **"PW"** (Points Won).
- **Court limit**: `--courts` is hard-capped at 16 (`helper.MaxCourts`, derived from the court label alphabet A–P) and any value over that returns an error rather than silently truncating. 16 is the largest allocation any one competition can legally hold, so a venue beyond it could never give all its shiaijo to a single competition.
- **Schedule Estimator** (`internal/engine/schedule.go`): `EstimateSchedule(EstimateInput) ScheduleEstimate` produces total/per-court minutes from match duration × multiplier × slowest-court buffer. Exposed via stateless `GET /api/schedule/estimate` on both the CLI web server and the mobile app.

### The competitor-number search rule (bc-nsrc)

A picker that FILTERS A LIST by what the reader typed asks ONE rule for the competitor number, `matchesCompetitorNumber` in `web-mobile/js/competitor_search.jsx` (composed into `competitorMatchesQuery` for a record and `matchMentions` for a match), and never re-derives it (operator rulings 2026-09-21 and 2026-09-23). Two arms over the record's own competition prefix: the query EQUALS the whole number, or the query BEGINS the competition's `numberPrefix`. So "k" lists every K draw (K and an auto-minted K02 alike), "k02" lists the K02 draw, "k1" is K1 alone (not K12), and bare digits find nobody. The prefix has to be a FIELD on the record, because the number string cannot say where its prefix ends ("K021" is K02's first competitor or K's twenty-first): `buildRoster` (`viewer_watchlist_core.jsx`) stamps `numberPrefix` on every roster record and `buildPlayerMap` (`api_serializers.jsx`) on every resolved match side, `prefixOf` (`competitor_identity.jsx`, beside `numberOf`, trimmed as Go's `EffectiveNumberPrefix` trims) reads it back, and a competitor only carries a number when their competition carries a prefix (`handlers_viewer.go` `numberingApplies`), so there is no fallback arm. Names and dojos stay substring matches. Two admin surfaces are ruled OUT and carried as data in the gate: `admin_participants.jsx` does not search numbers, and `admin_registration_desk.jsx` keeps its fuzzy ranked search (it still finds K12 from a bare "12"); do not "unify" them. The gate is `web-mobile/check-competitor-search.mjs` (a `.number`, `numberOf(` or `prefixOf(` read on the same line as `.includes(`/`.startsWith(` outside the owner fails the build); it shares `check-helpers.mjs` with `check-write-result.mjs` and exports `FORBIDDEN`, and `rule_gates.test.jsx` pins both gates. The one call site the gate cannot see, the admin Scores filter, is pinned by a render test instead (`score_editor_filter_finds_a_number.render.test.jsx`).

### Resource Embedding

`main.go` embeds `web/*` via `//go:embed`. The global var `helper.WebFs` exists for backward compatibility with code paths that still reference it directly. Must rebuild after changing embedded files.

### Mobile-app runtime defaults

Production-hardening defaults (header, read and idle timeouts, body caps, SSE subscriber cap, graceful shutdown) are applied in the `mobile-app` command; the constants live in [cmd/mobile_app.go](cmd/mobile_app.go) and [internal/mobileapp/middleware.go](internal/mobileapp/middleware.go) / [hub.go](internal/mobileapp/hub.go). `WriteTimeout` is deliberately **0** (unbounded): SSE streams are infinite, so per-request cancellation runs via `Request.Context().Done()`. The `/tournament/import` body cap is 64 MB (`MaxImportBodyBytes`) to match the handler's `ParseMultipartForm`, and `SSE_MAX_CLIENTS` overrides the subscriber cap.

**`safeGo` convention.** Any goroutine spawned inside a request handler MUST use the `safeGo` helper in [internal/mobileapp/safego.go](internal/mobileapp/safego.go). Gin's Recovery middleware only catches panics on the request goroutine; a panic in a spawned goroutine crashes the entire process. The helper guarantees `wg.Done()` on panic and captures the recovered value into a shared `atomic.Pointer[recoveredPanic]` so the handler can return a single HTTP 500 without leaking internals. Pattern:

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

### Architectural observations (areas to watch)

In-progress migration to keep in mind (the other tech-debt notes, such as which packages have grown large, are derivable with `gocloc`):

- **No top-level interfaces** for `state.Store` or `engine.Engine`: interface adoption is incremental via `mobileapp/deps.go`. One exception: the engine's match-write and decision primitives take `state.StoreTx` as a store handle (`*state.Store` satisfies it), so `writeToPoolOrBracket`, `recordBracketMatchResult`, `RecordDecisionTx`, `lookupMatchSides` and `checkConcurrentIneligibility` each exist ONCE, with `RecordMatchResultWithIneligibility` and `RecordDecision` as `WithTransaction` shims (the eligibility restore on a rescore is keyed on the competitor-status record's own `MatchID`, never on a re-derived identity). Other engine-to-state and helper-to-engine calls still use concrete types. Extend the handle pattern; never add tx/non-tx twin bodies.

## Participant CSV Schema (canonical)

`participants.csv` stores one participant per line. Two formats are supported:

**With UUIDs (new format)**: first field is a UUID v4 (lowercase hex).
```
<uuid>, Name[, Zekken/DisplayName], Dojo[, DanGrade][, source]
```

**Without UUIDs (legacy format)**: detected automatically when first field is not a UUID.
```
Name[, Zekken/DisplayName], Dojo[, DanGrade][, source]
```

- The zekken/display-name column is only present when `withZekkenName=true` for the competition.
- `DanGrade` is optional; omit or leave empty.
- `source` is the last column when present and must be one of: `manual`, `registered`, `transfer`. This is the registration provenance (admin-only). It is distinct from the competitor's "tag" (their assigned competitor number, which is the `Number`/`number` field, optionally prefixed via `numberPrefix`, e.g. "A1").
- Seeds are stored separately in `seeds.csv` and merged at load time. Do **not** include seed ranks in `participants.csv`.
- **Engi (kata-competition) pairs**: an engi competitor is a PAIR (two member names, one shared dojo) but is a SINGLE participant on the Go side. Both member names are COMBINED in the `Name` field, joined by `" - "`: a paste row is `Name1 - Name2, Dojo`. Engi does NOT alter the column layout (`EffectiveWithZekkenName()` returns `WithZekkenName` only), so an engi roster parses byte-identically to any non-engi roster with the same zekken setting; when zekken is on, the zekken column holds the combined pair zekken (`ZEKKEN1 - ZEKKEN2`). Display surfaces split the combined name on the FIRST `" - "` via `window.engiPairParts` (ui.jsx); Go keeps the name whole. A pair is one row in standings and one side in a match; the two names are presentation only and never split the entity for scoring.
- The Go parser lives in `internal/state/participants.go`; the JS parser in `web-mobile/js/data.jsx:parseParticipantLines`. Keep both in sync with this schema when changing column layout.

## Common Pitfalls

- **A team's people are its "team members", never its "squad"** in UI strings, in `docs/`, and on disk (operator ruling 2026-09-16, bc-dnst). The store file is `team-members.yaml`, renamed from `squads.yaml` (root key `squads` -> `members`). **A storage change carries a migration path on load covering the last TWO releases** (operator rule 2026-09-17): `upgradeTeamMembersFilenameLocked` (legacy_upgrade.go) adopts v2.0.0's file on first load and removes it once the new one is written, so a tournament recorded by that release keeps its members. v1.1.0 and earlier stored none, so that is the whole history. It converges rather than dual-reading, so only one shape stays live. The route is `GET /api/competitions/:id/team-members` and its payload key is `teamMembers`; both were renamed with the file, with no compatibility shim, because client and server ship in one binary. Three team writes are deliberately PUBLIC in self-run (operator decision 2026-09-27, bc-dhas, "Open lineups to participants"): the ones the public score sheet makes when a competitor names a bout's fighter, `PUT .../teams/:tid/match-lineups/:matchId`, `POST .../teams/:tid/members` and `PUT .../teams/:tid/members/:memberId`. They are NOT in `isSelfRunMainGatedConfigRoute`; their handlers hold a caller with an EMPTY password (`selfRunAnonymous`, middleware.go, shared with `/score`'s `enforceSelfRunPolicy` and the representative bout's add and remove) to the score path's rule. A password sent but wrong is an organiser with a stale one: it gets 401 with AuthMiddleware's body (`respondInvalidPassword`), never a participant's sentence. That is why the member POST asks `selfRunAnonymous` too. The rule has these parts: no lineup change once the match has finished (409 `result_finalized`, from the `resultFinalized` helper the score path and the representative bout's add and remove use, with the lineup's own sentence; an unknown match is 404) or for a team the match does not hold by id (404 `team_not_in_match`); no renaming a member who already has a name (409 `member_already_named`, a `selfRunRefusal`); no adding or naming a member once the competition has finished (409 `competition_finished`); and no add past `selfRunMemberLimit` (2 x team size + `SquadReserveSlots`, 409 `team_member_limit`), because a member can never be removed; the count and the add are ONE step under the store lock (`AddTeamMemberUpTo`, reached through the `deps.go` interface), since a count taken before the add let concurrent anonymous adds pass it together. The finished-competition refusal is judged there too, and in `NameUnnamedTeamMember`, under the lock the competition's completion takes (`state.ErrCompetitionFinished`), never from the handler's earlier read, which a finish could land after. The match-lineup PUT also refuses an anonymous caller's member id the named team does not hold among the positions the save writes (the changed ones when it names them, `LineupRequest.memberIDsRead`) (400 `team_member_not_in_team`, `memberIDsOutsideTeam`, through the score judge's `memberJudge`: a save naming no member id reads no file, and a team-members.yaml that cannot be read is 409 `team_members_unreadable`), so a participant cannot seat another team's member, or an invented id, in a lineup; the organiser is not judged by it. Every member write answers with the member as stored, stamped (`TeamMember.ModifiedAt`, `Store.stampMember`: the server's time in ms, or one more than the member's previous stamp, so one member's stamps only grow): POST 201, PUT and DELETE 200 (they were 204), and all three announce the change (`lineup-updated` naming the team), POST included. A member name longer than `MaxLenPlayerName` is 400 for every caller. Each of these handlers resolves the caller before reading the body. The organiser, sending the password, keeps mp-q722's always-editable rule. Every other lineup or member route (`GET team-members`, `DELETE members/:memberId`, the round lineups PUT/DELETE, `DELETE match-lineups`) MUST stay in that list or it becomes publicly reachable in self-run; pinned by `self_run_team_writes_test.go`. When you add a route here, decide which side it is on and pin it there. The public page sends an EMPTY `X-Tournament-Password`, never omits it. A public surface reads a team's members from the viewer payload, never from `GET team-members`: the self-run score editor gets them through `MatchViewerModal`'s `teamMembers` prop, and `TeamScoreEditorModal` calls `fetchSquads` only when no host passed them. "Squad" survives as a code name only, in identifiers such as `squadMemberLabel`, `squadSlotLabel` and `squadRosterEntries`, and in the client-side `squads` property that `normalizeViewerCompItem` maps the wire key onto (note `normalizeCompetitionDetail` SPREADS the payload instead, so that shape carries `teamMembers`: two shapes, two names, one concept). The Lineups page heading reads "Team members"; write the same in any new operator copy or docs page.
- **Never write "default win"** in anything a person reads: a UI string, a docs page, an API message, a commit or PR (operator ruling 2026-10-04: "such thing does not exist"). Every finished match has a result: a scoreline, or a registered decision that names the winner (kiken, fusenpai, fusensho, hantei). Name the decision (`domain.DecisionWord` in Go, `decisionWord` in write_result.jsx); say "decision" only where several kinds are meant together. Internal identifiers keep the old name (`IsDefaultWinDecisionStr`, `DefaultWinIppons`, the wire code `default_win_stands`), since no person reads them. The other forms are the same concept and banned with it: "default loss", "default winner", and a side that wins, won (never the contraction "won't"), loses or is winning "by default". Enforced by three gates sharing three patterns, pinned by one shared case table (`internal/mobileapp/testdata/default_win_cases.json`, read by the Go and JS gate tests): "default" then win(s) or loss(es) with at least one separator (any space, a hyphen, `{}`, quotes or `+`, and a word boundary after, so a camelCase identifier or `variant="default" winner` passes); "default", whitespace, winner(s); and wins, won, winning or loses followed within 40 characters by "by", whitespace, "default" (crossing line breaks, stopping at a full stop or a table-cell `|`). The words "winners", "lost" and "losing" are deliberately left out of the last pattern: they refused ordinary settings and connection prose ("Winners per pool is 2 by default"). The gates: `TestNoDefaultWinInStringLiterals` (every Go string literal under internal/ and cmd/, `+`-joined literals checked as one) with `TestNoDefaultWinInPersonReadText` (every git-tracked person-read text file outside docs/: `specs/openapi.yaml`, tracked `specs/**/*.md`, and the root `*.md` except CLAUDE.md and AGENTS.md), a FORBIDDEN rule in `web-mobile/check-write-result.mjs` (the comment-stripped WHOLE file; also index.html and styles.css) and `docs/check_prose.py` (paragraph-wide, on tag- and emphasis-stripped text; a fenced block or comment ends a paragraph).
- **Kendo uses NO MATS. Never write "mat" or "mats"** in code, comments, tests, UI strings, docs or commit messages. The fighting area is a **shiaijo** (in prose, a **court**); "shiai-jo" hyphenated on the public `docs/` site. "Mat" is judo/wrestling vocabulary and is simply wrong for this sport. Reaching for it to avoid repeating "court" is how it gets in. (The only legitimate `mat` in the tree is a QR bitmap variable in `web-mobile/js/qr.jsx`, plus the rule's own name inside `docs/check_prose.py`.)
- Excel coordinates matter: changing match generation requires updating cell references and formula links across sheets
- `team-matches=0` means individual tournaments, not team tournaments. In the tournament app, `state.Competition.IsTeam()` (`Kind == "team" || TeamSize > 0`) is the one spelling of "a team competition"; do not respell it (several near-variants that differ on hand-edited data, e.g. `TeamSize > 0` alone, remain where they mean something narrower).
- The `errcheck` linter is enabled (test files excepted). Don't introduce `_ =` or bare ignored returns in production code. Wrap and propagate, or log via `handleExcelError`
- Web UI changes (`web/index.html`) should be validated in a running browser, not just by reading diffs. Use `make run`
- Mobile app frontend changes (`web-mobile/`) require rebuilding the binary to take effect. The files are embedded at `go build` time via `//go:embed web-mobile/*` in `main.go`. Run `make run-mobile` which rebuilds automatically, or run `make go/build` then restart.
- Competitor identity is (name, dojo), never the name alone. Two competitors sharing a NAME from different dojos are different people and are allowed everywhere; only same-name AND same-dojo is a duplicate. The CLI/web CSV path rejects duplicate ROWS up front via `helper.CheckDuplicateEntries` and the web handler surfaces them; the tournament app enforces the (name, dojo) rule via `helper.CheckDuplicateEntriesByNameDojo`, with the floor guard in `state.saveParticipantsNoLock` so every persistence path is covered. A blank dojo has TWO floors with TWO distinct sentinels: `state.ErrBlankDojo` refuses every roster save (loading stays tolerant on purpose so the roster can be repaired), and `helper.ErrBlankDojoInDraw` refuses the tree-aware draw over a loaded legacy roster (the draw path never crosses the save floor; the refusal names the offending row and is NOT a bug to relax). They are different errors in different packages; `errors.Is` against the wrong one silently never matches. TEAM names are the exception: they must be unique even across dojos (`helper.DuplicateNamesWithKeys`), because a team name is its identity in results. **A record that carries an id field is resolved by id only** (operator ruling bc-pnum): an empty id resolves to NOTHING, never to a name or (name, dojo) match, and no name/(name, dojo) fallback arm may sit beside an id lookup on the SAME record (`lookupStandingsPlayer`/`resolveWinnerSide` in `internal/engine/engi.go`, `groupMemberIDs` in `tiebreaker.go`, `resolveParticipantIndex` in `internal/state/participants.go`, `matchesParticipant` in `replace_participant.go`, `engine.AttachPoolMatches` (`internal/engine/workbook.go`) and `standingMap` in `internal/export/builder.go`, and the pool-rank-override / league-tiebreak selection paths are all id-only end to end). `helper.CompetitorKey(id, name, dojo)`'s (name, dojo) fallback survives for exactly two legitimate uses: duplicate detection over an input list BEFORE ids exist (`helper.CheckDuplicateEntriesByNameDojo`, and an explicit `helper.CompetitorKey("", name, dojo)` comparison for that purpose), and records that carry NO id field at all (`state.SubMatchResult`'s lineup names). A third, narrower carve-out belongs to `state.BracketMatch` (bc-brid): its sides now carry `SideAID`/`SideBID`/`WinnerID`, so it is NOT a no-id record, and an id is present exactly when the side is a resolved competitor. A side legitimately carries no id in exactly three shapes: an empty bye slot, an unresolved feeder (a "Winner of ..." advancement or a "Pool A-1st"-style placeholder before the pool phase resolves it), and a legacy row a repair has not yet reached. For `BracketMatch` the rule is therefore resolve-by-id-when-present, name-fallback ONLY for those three shapes: `buildSideByIDOrName` (`internal/mobileapp/handlers_display.go`), `matchesBracketSide` (`internal/engine/scoring_tx.go`), and pass 2 of `ReplaceParticipantInDraw` (`internal/engine/replace_participant.go`) are the three sites that do it. A pool row has no equivalent unresolved shape: its side ids are stamped when the draw runs, and a legacy row is repaired on load where the name resolves unambiguously, so an id-less pool side is a fault the operator is told about rather than a normal state. That is why its stricter id-only rule above stands unchanged. A fourth carve-out, the same shape as `BracketMatch`'s, belongs to `domain.SeedAssignment` (bc-sdid): it now carries `ID` too, so it left the no-id list above. `state.SaveSeeds` stamps it from the roster on every write, and the legacy-upgrade pass (`upgradeSeedRowsLocked`, `internal/state/legacy_upgrade.go`) backfills it on load where the row resolves unambiguously; an id is present exactly when a resolver has already attached the row to a participant. The one shape that legitimately still carries no id is a row a repair has not yet reached (an ambiguous bare name, or a file predating the id column). `domain.RosterIndex.LookupSeed` is the id-first-then-(name, dojo) resolver every seed matcher (the seeds.csv-onto-roster merge, `helper.ApplySeeds`, `mobileapp.seedsOffRoster`) now calls, so an id, once present, is never re-checked against a possibly-stale Name/Dojo. Anything that keys a competitor with a name/(name, dojo) fallback OUTSIDE those four carve-outs is a bug: use the participant id.
- **A double tap is two complete clicks** (bc-dtfn): Preact renders between them, so no state check can tell a bouncing thumb from a second tap. `web-mobile/js/tap_guard.jsx` is the ONE owner of "is this pointer tap the bounce of the previous one" (`TAP_BOUNCE_MS`, a click with `detail === 0` from the keyboard is never a bounce). Two-tap commits (Finish, Finish + Start Next, End match, and the engi editor's Save result, since the editors behave alike: operator ruling 2026-09-27) use `useArmedConfirm`; the ippon and foul `+` buttons guard per bout side through `acceptTap`. A layer a tap opens (a confirm, `Modal`, the overlay score editors, the participant Edit dialog, the public match card, the sign-in dialog) takes the bounce of that tap through `useOpenedTapGuard` (tap_guard.jsx); the overlay score editors, and the at-court lineup panel with them, guard their backdrop only (`backdropOnly`), so their controls work at once. That stays as it is (operator ruling 2026-10-06), with its known consequence: the second tap of a double tap that opens an overlay score editor lands on whatever control is under the finger (measured at 1180x820: a double tap on a running match's row on the Pools tab opened the editor and scored Shiro's M, and on a finished match it opened the Kiken – Injury panel; the Scores tab's Score buttons sit outside the editor). Do not report it as a new defect. Deliberately NOT guarded, by the same ruling: the engi flag `+` (a flag count is not an ippon call, so a quick double tap is legitimate entry) and every removal tap (a scored mark, the engi `−`), where a bounce removes a second mark or flag and the operator re-enters it. The overtime counter's `+` (the `EnchoControl` stepper, admin_scoring_shared.jsx) is left unguarded too (operator ruling 2026-09-27): a bounce saves one period too many, which shows only as `×N` in that editor row, because everything else reads only whether overtime happened (`enchoOn`), and one tap on `−` takes it back. Kachinuki bout mode's Encho button is left unguarded as well (operator ruling 2026-10-02, bc-kheb), and **Undo encho** holds its place beside it, held (`.holds-space`, and disabled) while there is nothing to undo, so neither button moves when the other comes or goes (operator ruling 2026-10-06): a bounce lands on Encho again and saves one period too many on that bout, which shows only as (E), and one tap on Undo encho takes it back. Before that ruling the centred row shifted, Undo encho took Encho's place under the finger, and a double tap cancelled itself. Do not add any of these guards; the capture harness waits `TAP_DWELL_MS` (scripts/screenshots/lib/editor.mjs) between such taps. Do not add a per-file timer. The gap is timed with `Date.now()` when the handler runs, not with the event's `timeStamp` (weighed in review, 2026-09-27): `timeStamp` helps only where a browser stamps a tap's click at the touch rather than at dispatch, and the bout-list guard stamps with no event at all (`stampTap(boutListTapRef)`), so mixing the two would compare different clocks and silently disable the guard.
- **The team sheet's pinned bars never hide what a scroll brings into view** (the pinned header and footer dock, bc-tmfd). `published_height.jsx` (`publishHeight`) is the one owner of a published bar height: `--topbar-stack-h` (AdminTopbar, on the root), `--team-pin-h` (the team sheet: on the root for an inline host, on the overlay's `.editor-modal__body` for an overlay) and `--team-dock-h` (inline only; an overlay's footer sits outside its scrolling body). The clearance is `scroll-margin` on the sheet's OWN content controls (inline: topbar plus pin above, dock below; overlay: pin above; the inline head: topbar), pinned by `team_editor_pinned_bars.render.test.jsx`, so a control a keyboard Tab or a `scrollIntoView` reaches stops clear of the bars (WCAG 2.2 2.4.11). Never `scroll-padding` on a scroller by a bar height: a bar's own buttons lie inside the padded strip, so focusing one (a Tab, a dialog returning focus) scrolled the page by about 300 px each time (measured 2026-10-06). One Escape closes one layer: an open fighter list takes it (`LineupNameInput` stops it there; a box with no list drawn lets it through, so the editor closes at once), as does an open rename box (`lineup_rename.jsx`), and the layer behind closes on the next. `dialog_focus.jsx` (`useDialogFocus`) is the one owner of "focus goes into a layer as it opens and back, without scrolling, to the control that opened it": the court console's two confirms (the court move, Send back to queue), which also cancel on Escape while their request is not out, and the at-court lineup panel, a `role="dialog"` that closes on Escape unless a lineup write is out. The panel is an editor like the overlay score editors, so it guards only its backdrop against the bounce of the tap that opened it and its controls work at once (the 2026-10-06 ruling for the editor overlays; a whole-layer guard made a Close tapped at once do nothing, found by PR #442's journeys). The court picker gives focus back to its button only for a close made inside it (Escape, a choice, its own button), never for a tap elsewhere, which keeps the focus it took (taking it back dropped the iPad keyboard of the tapped name box and scrolled the page). Layers stack by one scale: announcement cards 900, `.modal-backdrop` (every plain modal, the overlay score editors included) 1000, dialogs 1100, the toast 1200; the public match card and the lineup panel also sit at 1000, where DOM order decides.
- Chained match navigation in the admin score editor (Prev/Next buttons, Finish + Start Next, ←/→ keys) must stay on the current match's shiaijo, and the AUTOMATIC advance (Finish + Start Next, the start after a withdrawal or no-show) also stays in the current match's competition (operator ruling 2026-09-26: one operator per court, scoring from the court view; an operator view never switches competition or court by itself). The tournament-wide Scores page lists every competition, so its `nextActiveFrom` filters on the open match's `compId`; the court console's `nextActiveAfter` always did. Prev/Next, which the operator taps, still move along the whole court. Operators run matches per-court, so hopping courts mid-flow breaks the workflow. See `AdminScoreEditor` in `web-mobile/js/admin_schedule_score_editor.jsx` (`admin_schedule.jsx` only re-exports it): filter to `(m.court || "") === (openMatch.court || "")` so empty/undefined courts share one "unassigned" bucket.
- Docs under `docs/` are PUBLIC-facing: no beads (`mp-xxxx`)/`bd`, no internal-tooling or `CLAUDE.md` references, and NO em-dashes. The PR template gates `make docs/build`; run it before pushing docs changes.
- `mkdocs build --strict` fails on broken cross-file links (WARNING) but only logs missing same-page anchors as INFO (build still passes), so verify anchors by hand. Anchors use pymdownx slugs: lowercase, punctuation dropped, `--`/parens collapsed to single hyphens (e.g. `### Locked mode (--lock-password)` becomes `#locked-mode-lock-password`). Note TWO files named `mobile-app.md` exist (`docs/user-guide/mobile-app.md` guide vs `docs/user-guide/commands/mobile-app.md` reference); a relative link from `commands/` resolves to the sibling, so confirm the target file before trusting a flagged anchor.
- **A court call is the console's, and withdrawing one that is already gone is not a failure** (bc-cdbl, operator decisions 2026-10-08 Q1 (a) and Q2 3A). The console that called a match keeps the announcement id per match and withdraws it once that match leaves scheduled; the server never tags an announcement with a match. `AnnouncementStore.Add` replaces an active announcement with the identical message (new id, fresh duration), and the id it replaced still withdraws the one that replaced it (`replaced`; an alias whose announcement has gone is dropped at the next add or removal, `dropOrphanAliasesLocked`), so a console holding the older id can take the call down. When two consoles called the same match, the second withdrawal finds it gone: `API.deleteAnnouncement` reads a 404 as the outcome the caller asked for (the banner is gone), on the console and the organiser's Dismiss alike. The browser still logs the 404 response itself; that line is not the app's.
- **A lineup read never answers 404 for "nothing saved"** (bc-k404, operator decision 2026-09-27: "there either is one or there isn't and it's empty"). All three public lineup GETs (`lineups/:round`, `match-lineups/:matchId`, `lineup-in-force/:matchId`) in `internal/mobileapp/handlers_lineup.go` answer 200 with `saved: false` and an empty lineup instead; `api_client.jsx`'s `lineupOrNull`, used by `fetchTeamLineup` and `fetchLineupInForce`, is the ONE place that turns `saved: false` into `null`, and a 404 there means the competition itself does not exist. Do not re-add a not-found answer for an unsaved lineup.

## PR Workflow

- **Build the PR body from the repo template.** When creating a PR, populate the description from `.github/pull_request_template.md` and fill every section: `gh pr create --body-file <filled-template>` (the bare `gh pr create` / `--fill` does NOT apply the template). Set the `Closes bc-xxxx` bead reference (`mp-xxxx` for older beads that predate the prefix change).
- **Embed screenshots via the `pr-assets` side branch, not gists** (`gh gist create` rejects binaries). Push the PNG to the `pr-assets` branch (never merged to main) by PIPING the base64 in; it must never pass through argv, or any shot over ~96 KiB dies with `Argument list too long` (Linux caps a single argument at 128 KiB, and base64 inflates by 4/3):
  ```bash
  base64 < shot.png | tr -d '\n' \
    | jq -Rs --arg m 'pr <pr> screenshot' '{message:$m, branch:"pr-assets", content:.}' \
    | gh api --method PUT /repos/gitrgoliveira/bracket-creator/contents/pr-assets/<pr>/shot.png \
        --input - --jq '.content.path'
  ```
  Then embed `![](https://raw.githubusercontent.com/gitrgoliveira/bracket-creator/pr-assets/pr-assets/<pr>/shot.png)` and `curl` that raw URL back to confirm it resolves, rather than trusting the 201. A real browser/MCP screenshot is MANDATORY for any UI change. There is NO textual/DOM/geometry substitute. If you have not captured one, the PR is not review-ready: capture it first, then fill the Screenshots section. Full verified recipe: the `/pr-screenshots` skill. **Capture only via the browser/MCP screenshot tools, NEVER a desktop or full-screen grab (`screencapture`, `scrot`, OS shortcuts), which exposes the user's private screen.**
- **Chain gate → commit → push with `&&`, from the worktree's absolute path** (`make -C <wt> go/test && git -C <wt> commit ... && git -C <wt> push`). A `;` chain pushed once with the gate never having run (PR #376). The `;` rule in Shell Discipline is for cleanup steps, where one failure must not abort the rest; a gate is the opposite case.
- **Test plan is a gate, not a formality.** Before requesting review on a PR, check off EVERY item in the PR description's test plan. Do not mark a PR ready while any checkbox is unverified. Manual/browser steps are not optional; execute them, then check them.
- **Keep the bead `in_progress` until the PR actually merges.** A green review is not a merge. Only `bd close <id>` after the merge lands, with a reason referencing the merge commit/PR.
- **After a merge, run the full `/cleanup` sequence** (close bead → fast-forward main → remove worktree → delete local + remote branch → prune). Don't wait to be asked for each step. See the `/cleanup` skill.

## Code Review

- **There is no automated Copilot review loop.** It was retired on 2026-07-24. Do not run `/review-loop`, do not re-request Copilot, and never treat "waiting for a bot review" as a merge gate.
- **Review threads still appear; the repo owner posts them by hand.** Read and address them like any other review feedback. What is retired is the bot and the loop around it, not the reviewing.
- **Never report a review round "clean" until a fresh fetch shows zero unresolved threads.** State the total unresolved count first, give every thread an explicit disposition (fix or dismissal with a reason), then re-verify the count is zero.
- **Report `resolved` and `outdated` threads separately.** Never claim zero unresolved threads without checking for outdated-but-visible threads that the user can still see in the GitHub UI. A query that filters out outdated threads produces a false "clean" that contradicts what the user sees.
- **Paginate when counting or resolving threads.** GitHub's `reviewThreads(first:100)` caps at 100; a capped lookup silently finds nothing for threads past #100 and falsely prints "already resolved" while leaving them unresolved, which then blocks merge under a ruleset with `required_review_thread_resolution:true`.
- Deeper passes are run on request: `/tri-review`, `/code-review`, `/security-review`, `/impeccable critique`. A zero-findings result from a reviewer that never ran looks identical to a genuinely clean review; check for agent failures before trusting one.
- Test review fixes with targeted tests while you work, and run the full `make go/test` once before saying the round is done (see **Gate cadence** under Testing & Verification). A red gate means fix-or-revert, never push.

## Testing & Verification

- **Gate cadence: targeted tests while iterating, the full gate before claiming completion** (operator ruling 2026-09-25). `make go/test` takes about 3 minutes and keeps every core busy, which slows every other session on the machine; a full run after each small fix is what made concurrent sessions look stuck. While iterating, run only what the change touches, each in seconds and without `cd`:
  ```bash
  go -C <wt> test -run TestName ./internal/<pkg>/...
  npm --prefix <wt>/web-mobile test -- js/__tests__/<file>.test.jsx
  npm --prefix <wt>/web-mobile run test:render -- js/__tests__/render/<file>.render.test.jsx
  ```
  Run the full `make go/test` before any claim that the work is done, fixed or passing, before pushing, and before marking a PR ready. A change made after that run needs it again before the next such claim. Brief subagents the same way: an implementer or fixer runs targeted tests, and whoever claims completion runs the full gate once, in the foreground.
- **Verify in the browser; never substitute API/curl calls.** Manual test-plan items and UAT must be executed through the actual UI.
- **Test self-run / public features from the PUBLIC page, not the admin UI**: the public flow is what users hit. Admin-side scoring proves nothing about it.
- **File gap/UX issues incrementally as you find them**, not batched at the end of a UAT pass.
- **Diagnose failures from evidence, never fabricate a cause.** When a test, build, or CI step fails (Codecov, GPG, lint, etc.), read the actual logs before explaining it. Do not invent "known bugs", version-specific regressions, or other rationalizations to justify a workaround (e.g. a fabricated "known bug in Codecov v6.0.x" masking a transient GPG keyserver failure). If the root cause isn't established, say so and keep investigating.
- **Test coverage gate: every package that has test files must maintain ≥85% statement coverage.** Verify before any PR with:
  ```bash
  go test -race -cover . ./cmd/... ./internal/... ./tests/...
  ```
  Packages below 85% must be brought up before merging. New packages must include test files covering their public API.
  **Intentionally untested:** `internal/domain/internal/glossarygen` is a `go generate` code-generator (emits `glossary_data.js`); it has no exported API and is excluded from the gate. `internal/helper/bracket`, `internal/helper/csv`, and `internal/helper/seeding` are empty stub packages (no exported symbols yet) and are likewise excluded.

### Verification Claims

Never state that something is fixed, rendered, counted, or passing based on grep or DOM selectors alone. Confirm with the authoritative source: run the test, take a screenshot, or read the file. If evidence is unavailable, say so explicitly rather than asserting.

## Merge & Rebase

When rebasing or resolving conflicts, watch for these recurring breakages:
- Duplicate declarations introduced by the rebase (same symbol defined twice after a merge).
- UUID-vs-name-string mismatches in player/entity maps: match on id OR name, and use participant UUIDs (not display names) for bracket-highlight IDs.
- Missed call sites when removing or renaming a symbol (see **Refactoring Rules**).
- Re-run `make go/test` after every rebase; a clean rebase that compiles must not be semantically broken.

## Refactoring Rules

- **Search ALL call sites, including test files, before removing or renaming code or parameters.** Run `grep -r` (or `grep -rn 'SYMBOL' . --include='*.go' --include='*.jsx'`) to find every reference, not just production code. A removal that compiles can still leave stale test references or skip-test code pointing at dead paths.
- **Verify that guards and defensive code are intentional before removing them.** If a reviewer flags a removal, assume the guard was intentional unless you can prove otherwise from git blame or comments. Aggressive removal of guards (e.g. `sourceCompID` checks, `defer os.RemoveAll`) has had to be reverted.
- **Boy Scout rule: leave code better than you found it, even outside the diff.** When a review or task surfaces a worthwhile adjacent fix (a literal duplicated in sibling files, a comment contradicting the code, a swallowed error in a handler you touched), apply it rather than skipping it as "outside the reviewed diff". Precedent: the shared singleflight response tail was hoisted and the ZIP-magic literal deduplicated across untouched test files precisely because a review flagged them. Two limits keep this from scope creep: don't apply adjacent fixes that change intended behavior (surface those as findings for a decision), and don't let a small PR grow into a refactor: fix what you touched or verified, and raise the rest with the user rather than filing a follow-up bead unasked.

## Shell Discipline

Do not use `pkill`, broad `kill`, or force-push. Long-running processes get their own background handle and are terminated by PID; find it by what it holds (`ss -ltnp | grep ':8080'`), never by command-line pattern, which matches the Bash tool's own shell. Chain commands with `;` not `&&` when a non-zero exit from cleanup would abort the rest.


## Validation

All changes must be validated with `make go/test` and inspection of the generated example files from `make examples`. Pay attention to page breaks and seeding.

<!-- BEGIN BEADS INTEGRATION v:1 profile:minimal hash:6cd5cc61 -->
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

- Use `bd` for ALL task tracking. Do NOT use TodoWrite, TaskCreate, or markdown TODO lists
- Run `bd prime` for detailed command reference and session close protocol
- Use `bd remember` for persistent knowledge. Do NOT use MEMORY.md files

**Architecture in one line:** issues live in a local Dolt DB; sync uses `refs/dolt/data` on your git remote; `.beads/issues.jsonl` is a passive export. See https://github.com/gastownhall/beads/blob/main/docs/SYNC_CONCEPTS.md for details and anti-patterns.

## Agent Context Profiles

The managed Beads block is task-tracking guidance, not permission to override repository, user, or orchestrator instructions.

- **Conservative (default)**: Use `bd` for task tracking. Do not run git commits, git pushes, or Dolt remote sync unless explicitly asked. At handoff, report changed files, validation, and suggested next commands.
- **Minimal**: Keep tool instruction files as pointers to `bd prime`; use the same conservative git policy unless active instructions say otherwise.
- **Team-maintainer**: Only when the repository explicitly opts in, agents may close beads, run quality gates, commit, and push as part of session close. A current "do not commit" or "do not push" instruction still wins.

## Session Completion

This protocol applies when ending a Beads implementation workflow. It is subordinate to explicit user, repository, and orchestrator instructions.

1. **File issues for remaining work** - Create beads for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **Handle git/sync by active profile**:
   ```bash
   # Conservative/minimal/default: report status and proposed commands; wait for approval.
   git status

   # Team-maintainer opt-in only, unless current instructions forbid it:
   git pull --rebase
   git push
   git status
   ```
5. **Hand off** - Summarize changes, validation, issue status, and any blocked sync/commit/push step

**Critical rules:**
- Explicit user or orchestrator instructions override this Beads block.
- Do not commit or push without clear authority from the active profile or the current user request.
- If a required sync or push is blocked, stop and report the exact command and error.
<!-- END BEADS INTEGRATION -->

<!-- Everything below is repo-specific and deliberately OUTSIDE the BEADS INTEGRATION
     markers above, which are tool-managed (hash-stamped) and regenerated by
     `bd setup claude`. Do not move it back inside, or it will be overwritten. -->

## Beads: this repo's setup (overrides the generic notes above)

### Session completion here

The generic Session Completion steps above are narrowed in two places. Step 1: raise remaining work with the user and file a follow-up bead only once they agree. Step 3: a bead stays `in_progress` until its PR merges (see PR Workflow), so "close finished work" means merged work.

### Sync the beads DB

`git push` does NOT sync beads. **There is no auto-push**: `bd create/update/close` only touch the local Dolt DB, so an issue change exists solely on this disk until you run:

```bash
bd dolt push     # sync issue changes to the private remote
bd dolt pull     # pick up changes made on another machine
```

So when you hand off after changing beads, include `bd dolt push` in the sync you run or propose, exactly as you would a `git push`. `sync.auto-push` is accepted by bd but silently ignored, so nothing syncs on its own.

### One memory store: Hindsight

Persistent memory lives in **Hindsight**, and only there (2026-10-09). This OVERRIDES the generic line above ("Use `bd remember` for persistent knowledge"): `bd remember` is retired, the bd store was emptied, and a global Bash guard refuses `bd remember`. Claude Code's built-in auto-memory (`~/.claude/projects/<slug>/memory/`, indexed by its `MEMORY.md`) is switched off too (`autoMemoryEnabled: false` in the user settings).

The plugin is `hindsight-coding-agents` (upstream's replacement for the retired `hindsight-memory` plugin), wired for Claude Code and Antigravity CLI; its companion skill `hindsight-coding-agent` documents it.

- **Recall:** the first prompt of a session gets one memory synthesis injected (`<hindsight_memory>`), and the knowledge pages are re-offered as the session goes on (`pageRefreshEveryTurns`). Search yourself with `hindsight_search_knowledge_pages` first, then `hindsight_read_knowledge_page`; `hindsight_reflect` for the why behind a decision (slower).
- **Save** a lesson, ruling or recipe with `hindsight_ingest_document` (title = a short kebab-case key, content = the full fact with its reason); a new initiative with `hindsight_capture_initiative`. The session transcript is saved by the plugin at each Stop, and git commit messages are seeded from history.
- **Correct** a memory you verify is wrong with `hindsight_ingest_document`, titled `Correction: <topic>`: what memory claimed, what is true now, and the evidence.
- **Where:** the server and a bank per repo come from `~/.hindsight/coding-agent.json`. Its `bankIdTemplate` is `{gitProject}`, so the bank is named for the main checkout and shared by every worktree. The bank was seeded from the old auto-memory notes (documents keyed `memory/<file>.md`) and the `bd remember` entries (`beads/<key>`), so `merge-concurrency-plan` and the other bd entries are read from there.

The old `memory/` directory is kept untouched as a rollback copy: do not delete it, and do not read or review it as current. The post-merge cleanup skill reviews the Hindsight bank. AGENTS.md and GEMINI.md tell the other agents not to use `bd remember` either.

### Where the issues live

- **New issues use the `bc-` prefix; existing issues keep the IDs they already have.** Create one with `bd create "<title>" --id bc-<4-char-slug> --force`. BOTH flags are required: without `--id` bd mints an `mp-` ID from the in-DB `issue_prefix` (still `mp`), and without `--force` it refuses with `prefix mismatch: database uses 'mp-'`. Pick the slug yourself in the existing house style and check it is free first.
- **Do not "fix" this by renaming the prefix.** `bd config set issue_prefix` is refused outright, and `bd rename-prefix` rewrites the whole DB. With this repo's three prefixes present (`mp`, `mp-mol`, `bracket-creator`) it requires `--repair`, which REGENERATES every ID as random 8-hex (`mp-gmcg` → `bc-b90638d7`; verified on a throwaway DB copy; 0 of 581 mnemonics survived). That would strand ~1240 `mp-xxxx` references across the codebase, docs, git history and merged PR bodies. Mixed prefixes are fine: a `bc-` bead is fully first-class and can depend on an `mp-` bead.
- **Historical IDs:** ~550 use `mp-` (including `mp-mol-*` molecules poured from formulas) and a small legacy set uses `bracket-creator-*`. They stay as they are, so every existing reference in code comments and docs remains valid.
- The DB lives in the local Dolt store under `.beads/` (database `mp`, named in `.beads/metadata.json`). All of `.beads/` is gitignored, so the DB is never committed here.
- **Sync remote:** the PRIVATE Forgejo repo `Ricardo/bracket-creator-beads` over `git+https`, stored as `sync.remote`. Beads push to `refs/dolt/data` there, deliberately NOT to `origin`; this GitHub repo is public and the issue DB is not. Do not add a beads remote pointing at a public repo.
- **`.beads/` is shared by every worktree** (bd walks up from the worktree to the repo root), so concurrent sessions write to one DB. Never swap, restore, or replace the DB while another session may be running.

### Gotchas that have already cost time

- **`store is read-only` means a prefix-ownership problem, not a lock or corruption.** bd routes writes by issue-ID prefix; a prefix this repo does not own is routed elsewhere (see `routing.*` in `.beads/config.yaml`) and rejected. Run the write where that prefix lives, or fix the routing. Diagnose by comparing a native-prefix write against the failing one.
- **`bd config set` / `unset` are unreliable on nested keys.** `set` can rewrite flat `a.b:` keys into a nested `a:` block; `unset` can report success while leaving the key in the file. Always confirm with `bd config get <key>` AND by re-reading `.beads/config.yaml`, and hand-edit when they misbehave.
- **`bd where` reports the wrong prefix here.** It reads `issue_prefix` from `.beads/config.yaml` (which says `bc`), but ID generation uses the value stored in the DB (`bd config list` → `issue_prefix = mp`). The file value is inert. Trust `bd config list`, not `bd where`.
- **`bd export` JSONL is lossy: it carries `dependency_count` but no dependency edges, and no comment bodies.** Never migrate or restore a beads DB via export/import; it silently drops blocker relationships. Copy the Dolt database directory instead.
