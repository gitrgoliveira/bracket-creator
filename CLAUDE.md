# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Git / Worktree Rules

- **Edit only inside the correct worktree branch, never the main repo directory.** This repo uses a git worktree per PR. Before any file edit, verify the current working directory (`pwd`) and branch (`git branch --show-current`). Edits landing in the wrong worktree (or the `main` checkout) force costly patch-and-revert recovery.

## Governance

Before implementing features or making architectural decisions, read the project constitution:
**`.specify/memory/constitution.md`**: defines the core principles (YAGNI, DRY, TDD, DDD, evidence-based decisions, bracket integrity, and live-tournament constraints) that all changes must comply with.

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

make docs/build        # Build + strict-validate the MkDocs site (the PR-template gate)
make docs/serve        # Serve docs locally with live reload
make docs/deps         # Create .venv-docs from docs/requirements.txt
make docs/clean        # Remove .venv-docs and the built site/
# All docs/* targets run through .venv-docs (pinned mkdocs + Material from
# docs/requirements.txt). The system-PATH mkdocs drifts (older, no Material),
# so never call mkdocs directly for verification; use make docs/build.

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

- **`cmd/`**: Cobra CLI commands. Each uses an options struct with a `run()` method. `create-pools` and `create-playoffs` share significant logic. Shared helpers (`processEntries`, `openOutputFile`, `assignPlayerNumbers`) live in `cmd/shared.go`.
- **`internal/helper/`**: Core business logic: CSV parsing, pool/match generation, tree building, seeding algorithms, and all Excel rendering. This is the largest package.
- **`internal/excel/`**: Excel file lifecycle (`Client`), sheet operations (`SheetManager`), style definitions.
- **`internal/service/`**: Service layer abstraction over helper logic.
- **`internal/resources/`**: Embedded file management. Resources flow: `main.go` embeds → `resources.NewResources()` → `cmd.ExecuteWithResources()`.
- **`internal/mobileapp/`**: Gin HTTP handlers for the tournament app (`mobile-app` command). Routes: `handlers_competition.go` (including `POST /api/competitions/:id/generate-draw` [status `draw-ready`] and `DELETE /api/competitions/:id/draw` to discard the draw), `handlers_match.go`, `handlers_participants.go` (including single participant PUT updates, individual/bulk check-ins `POST /api/competitions/:id/participants/checkin-bulk`), `handlers_tournament.go`, `handlers_swiss.go` (`generate-round`, `standings`), `handlers_decision.go` (kiken/fusenpai/daihyosen: `POST /api/matches/:mid/decision`), `handlers_eligibility.go` (`/api/competitions/:id/competitor-status`), `handlers_lineup.go` (team lineups), `handlers_schedule.go` (`GET /api/schedule/estimate`, public), `handlers_reset.go` (`POST /api/tournament/reset`, public, for forgotten admin passwords; 404s in locked mode), `handlers_auth_config.go` (`GET /api/auth-config`, public, reports auth mode to the SPA). Real-time push via SSE (`hub.go`) with events: `match_updated`, `competitor_status_updated`, `competition_completed`, `swiss_round_generated`, etc. Auth via `X-Tournament-Password` header (`middleware.go`), with two modes selected at startup by a `PasswordVerifier` (`auth_source.go`): **file mode** (default, plaintext compare against `tournament.md`) or **locked mode** (`--lock-password` flag + `TOURNAMENT_PASSWORD_HASH` env var, bcrypt compare, `POST /api/tournament/reset` returns 404; the SPA `/reset` page still renders an operator-disabled message). Consumer-boundary interfaces live in `deps.go` (NFR-002).
- **`internal/state/`**: File-backed state store for the mobile app. Tournament and competition config lives in `tournament-data/tournament.md` and `tournament-data/competitions/<id>/config.md` (YAML front-matter). Participants are in `participants.csv` alongside each config.
- **`internal/engine/`**: Thin adapter that drives `internal/helper` pool/bracket generation from a `state.Competition`. Called by the `POST /api/competitions/:id/start` handler.
- **`web-mobile/`**: Preact/JSX frontend for the mobile app, served embedded in the binary. Entry point: `web-mobile/index.html`. JS modules in `web-mobile/js/` are grouped by prefix: `admin_*.jsx` (the operator console: setup, participants, pools, scoring, scheduling, lineups, etc.), `viewer_*.jsx` and `display_*.jsx` (public attendee/spectator surfaces), and shared infrastructure (`app.jsx`, `api_client.jsx`, `api_serializers.jsx`, `bracket.jsx`, `data.jsx`, `patch.jsx`, `router.jsx`, `ui.jsx`, `glossary.jsx`). Run `ls web-mobile/js/` for the current set rather than relying on an enumerated list here. Note `admin_participants.jsx` holds the `LinedTextarea` gutter participant paste box and the check-in filter list. CSS in `web-mobile/css/styles.css`. Pre-compiled to `web-mobile/dist/` by esbuild (run automatically as part of `make go/build`).

### Layering and on-disk state

Layered with no upward or circular dependencies: presentation (`cmd/`, `mobileapp/`, `web/`, `web-mobile/`) → business logic (`engine/`, `helper/`, `service/`) → persistence (`state/`, `excel/`) → domain models (`domain/`, the dependency leaf with zero internal deps). `mobileapp` → `engine` (via `deps.go` interfaces) and `state` (direct reads for viewer endpoints); `engine` → `helper` + `excel` + `state`.

Live-tournament state is file-backed under `tournament-data/` (owned by `internal/state/`):

```
tournament-data/
├── tournament.md                  YAML front-matter: name, date, venue, courts, password
├── .wal/                          Pending multi-file transactions (replayed on startup)
├── branding/  sponsors/           Uploaded logo + sponsor images for display surfaces
└── competitions/<id>/
    ├── config.md                  YAML front-matter: format, pool size, courts
    ├── participants.csv           One participant per line (UUID-prefixed)
    ├── seeds.csv                  Seed rank to player mapping
    ├── pools.csv                  Pool assignments after start
    ├── pool-matches.csv           Pool phase match results
    ├── bracket.json               Elimination bracket structure + results
    ├── schedule.csv               Court/time assignments
    ├── competitor-status.yaml     Eligibility records (kiken/fusenpai)
    ├── lineups.yaml               Team lineups, keyed by round
    └── overrides.json             Manual ranking overrides
```

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
- **Tie-marking Rule**: A match (individual or sub-match) is a tie when the operator enters **'X'** (or 'x') in the "vs" column or when both sides finish with the same total score (equal character-count after stripping spaces/zeros/dashes). For auto-detection, at least one score cell in that row must be non-empty. A team match is automatically a draw (T=1) when both IV and PW totals are equal.
- **Match Colors**: White (Shiro) is always the left column and Red (Aka) is always the right column (this is fixed and not configurable). In pool matches the first-listed player (SideA) is Red; in elimination matches the upper-bracket player (`node.Left`) is Red.
- **Excel Layout**: Uses an **8-column per court** layout. Columns A and G (and their court-shifted counterparts) are 30 units wide; others are 5 units wide. A blank row separates pools vertically.
- **Team Match Labels**: Summaries use **"IV"** (Individual Victories) and **"PW"** (Points Won).
- **Court limit**: courts are labelled A–Z, so `--courts` is hard-capped at 26 and any value over that returns an error rather than silently truncating.
- **Match Decision Types** (`internal/domain/decision.go`): 10 canonical wire values: `""` (none), `"fought"`, `"hikiwake"` (draw), `"kiken"` (legacy withdrawal, maps to kiken-voluntary on YAML load), `"kiken-voluntary"` (FIK Art. 31, permanent), `"kiken-injury"` (FIK Art. 30, reinstateable), `"fusenpai"` (no-show), `"fusensho"` (per-bout default win), `"daihyosen"` (rep bout), `"kachinuki-exhaustion"`. Use `domain.IsKikenDecision(d)` / `domain.IsKikenDecisionStr(s)` to check any kiken variant. Legacy YAML `decision: true` migrates to `"hikiwake"`, `false` to `"fought"`, `"kiken"` to `"kiken-voluntary"` (Decision.UnmarshalYAML). Visual marks follow the score sheet's layout, split into two kinds. MIDDLE marks (the centre "vs" cell / score-string centre) — exactly one of `X` (tie), `(E)` (overtime, always bare: `periodCount` is recorded but never displayed), `(DH)` (rep bout); mutually exclusive by rule (a match that went to encho cannot end tied, so X beats (E); a daihyosen is one-point sudden death, so DH bouts have no encho). RESULT marks ride beside the competitor they name: `Ht` (hantei winner), `Kiken` (withdrawer), `Fus.` (no-show; the Excel export also marks the fusensho winner — the JS uses a bout badge instead). Web score strings read `[left cell] [middle] [right cell]` with `vs` as the plain middle and `–` (or empty) meaning no points — never digits, ippon renders as waza letters only: `M vs –`, `M (E) K`, `M Ht (E) K`. The dash is a CELL value only: the middle never shows a dash, so unplayed/pending match rows read `vs` too. A default win (kiken/fusenpai/fusensho) awards the WINNER its points as maru, one `○` per point per FIK Regulations Art. 32 + Score Board appendix p.15 (`["○","○"]` in regulation, one `○` in encho — NOT a house style, it is the rulebook's score-board marking), via `domain.DefaultWinIppons`. Per the same Article ("Any point scored by the shiai-funo-sha shall remain valid") the LOSER is NOT zeroed: `preserveLoserScore` (engine/scoring.go, called by both `RecordDecision` twins) keeps the withdrawing side's already-struck ippons and preserves the encounter's prior `SubResults`, so a team withdrawal never wipes the sub-bouts already fought (they keep counting in IV/PW standings). Guarded on a side-matching prior so a drifted prior can't mis-attribute points; `state.EnchoMetadata.On` ↔ `enchoOn` (bracket.jsx) is the single "did this happen in encho" predicate (non-nil AND positive periodCount), shared by the (E) label, the maru count, and decision validation so a degenerate `{periodCount: 0}` block can never split the surfaces; display fallbacks (`DefaultWinMaruAB` in suffix.go, `formatIpponsScore`, the scoreboard win slots) render `○○` for winners whose recorded cells are empty (byes, pre-fill/legacy data). The middle-value rule lives in ONE place — `boutMiddle` (bracket.jsx), which returns exactly `vs`/`X`/`(E)`/`(DH)`; `matchMiddleMark` (the chip variant), `matchStateCell`, and the scoreboard separators all derive from it. Do not restate the middle chain at a call site. A surface that renders the FIK row must show the mark ONLY in that row's centre (operator ruling: the TV header and lobby chips were duplicates and were removed); a surface that renders NO such row (`MatchCard`'s meta strip, the OBS lower-third) keeps its chip, because there the chip IS the one home. **A middle mark belongs ONLY in the middle of an individual fight** (operator ruling), and a team bout's score is displayed the same way in every format, kachinuki or not: cells either side of a centre, on that bout's row. This governs the WHOLE set — `X`, `(E)`, `(DH)` — not just one of them. `TeamScoreboard`'s §277 summary row is an AGGREGATE (IV/PW), not a fight, so its centre is a deliberately empty spacer and NO mark ever goes in it. That holds even when the aggregate itself is tied: a team encounter drawn on equal IV and PW displays no `X` at the encounter level, and if no individual bout was drawn then no `X` appears at all — correct, because the mark describes a fight, not a standings figure (the draw is carried by the T column and by each drawn bout's own centre). Two rejected fixes, both reverted, both verified in the browser: threading the match-level mark into the summary spacer (a review round claimed removing the TV header chip left it homeless) put a second `(E)` directly above the bout's own; and placing the encounter tie there was proposed and refused for the same reason. Do not re-add either. Before acting on any "this mark has nowhere to render" finding, seed the state through the real editor path and check a UI can produce it. (`formatIpponsScore` composes the same `isDrawResult`/`middleMark` primitives directly rather than calling `boutMiddle`: its empty-middle-vs-`""` distinction is load-bearing for callers' fallbacks. The centre NEVER carries `Ht`: a hantei always names a winner and is decided from a TIED scoreline (`validation.go`: equal ippon counts and a winner; encho is NOT a precondition; in a TEAM match `validateSubBout` additionally restricts hantei to the daihyosen bout, position -1 — a tied numbered bout is simply a hikiwake). `Ht` therefore behaves like a point and fills the winner's next FREE slot in the same outside-to-inside order (0-0 → the outer slot, 1-1 → the inner one, giving `[K][ ] vs [Ht][M]`). Through normal play sanbon-shobu ends at 2, so 0-0 and 1-1 are the only reachable scores and a free slot always exists; 2-2 is IMPOSSIBLE under the rules and closed on both sides (the editors' ippon entry stops each side at 2; `validateIpponCounts` caps each side at 2 AND rejects 2-2 outright on every sub-bout and on the bulk path) — the `loose` return exists solely for hand-edited files — no point is ever overwritten, and each consumer then applies ITS OWN policy (the read-only scoreboard renders the mark beside the slots; both editors drop it because each has a second always-mounted channel for the verdict — see result_slot.jsx's header for the enumeration). The slot rule has ONE owner, `resultSlot` in web-mobile/js/result_slot.jsx (a small leaf; the dependency reasoning is stated once in that file's header); its three consumers are `resultCells` in match_scoreboard.jsx, `hanteiSlot`/`ptSlots` in admin_scoring_team.jsx, and the individual editor's slot grid in admin_scoring_individual.jsx (display-only `Ht` chip via `hanteiWinnerKey`) — change the contract and ALL THREE surfaces must be re-checked. Score strings TRAIL loose marks as an unattributable-winner degradation, but the scoreboard has no centre-`Ht` equivalent: an unattributable winner gets no mark rather than one in the shared centre cell. ACCEPTED no-mark classes (all drifted/legacy data; the centre is forbidden by the operator ruling and no side is attributable): (i) same-name-no-id pairs; (ii) rename-drifted winner names — and note the old centre fallback DID fire for these whenever the middle was empty, which a no-encho hantei makes normal, so this IS a regression there, accepted; (iii) winner-less legacy `decidedByHantei` rows (previously centre `Ht`); (iv) untied legacy `decidedByHantei` rows written before the tied-scoreline validation existed (letters still convey the winner; the judges'-decision marker is dropped). Two related deliberate divergences: score strings (`sideMarks`) and the Excel export (`SideMarks`) still mark `Ht` UNCONDITIONALLY — the mirrored pair stays consistent with itself and gains no tie gate — and a winner naming BOTH sides (INVALID data: team names are unique by rule) resolves defensively to AKA everywhere in JS (`subWinnerSides`, aka-first) to match Go's `isWinForSide`/`TeamResultFrom`/`SideMarksLR` order, so rows, summaries, standings and the export agree on the same arbitrary side. Nothing re-normalises stored sub names after a rename.) Mirrored helpers: `middleMark`/`sideMarks` (bracket.jsx) ↔ `MiddleMark`/`SideMarks` (internal/export/suffix.go); `enchoLabel` pinned in both languages via `internal/export/testdata/encho_labels.json`. **Writing `SubResults`?** `SubMatchResult.DecidedByHantei` is a tri-state `*bool` — `true`, an explicit `false` (withdrawn), or nil meaning *this writer said nothing* — read it via `HanteiDecided()`, never by dereferencing. Every forward replacement of a `SubResults` slice must call `engine.preserveSubHantei(stored, incoming)` first (both twins in scoring.go AND both branches of the tx twins in scoring_tx.go; a guard on only one of a twin pair reads as covered while the live `/score` path stays unprotected). It carries a stored verdict, its winner and the scoreline it rests on onto a verdict-silent row, and is deliberately narrow in two directions: it PATCHES an existing position -1 row but never RE-APPENDS a dropped one (`DELETE /daihyosen` removes that row on purpose through this same path), and it refuses any `Decision` outside validateSubBout's allow-list because it runs AFTER validation and its output is never re-checked. The rollback paths must call `normalizePriorForRollback` for the mirror-image reason: there, nil means preserve, so an un-normalized nil restores the write being undone. On the wire, an editor sends an explicit `false` only when it actually HAS an opinion (`hanteiKnown` in `daihyosenEnchoFields`) — an editor mounted before the verdict existed goes silent instead, or it would erase a verdict its operator was never shown.
- **Competitor Eligibility** (`internal/state/competitor_status.go`, `internal/engine/eligibility.go`): a kiken/fusenpai decision auto-writes a `CompetitorStatus{Eligible: false}` for the loser; `engine.StartMatch(compID, matchID)` is the pre-flight gate that returns `*IneligibleCompetitorError` (matches `errors.Is(err, ErrIneligibleCompetitor)`). Maps to HTTP 409. Kiken-injury (FIK Art. 30) sets `CompetitorStatus.Reinstateable: true`; the admin can call `POST /api/competitions/:cid/competitors/:pid/reinstate` to restore eligibility. Kiken-voluntary (Art. 31) and fusenpai are not reinstateable.
- **Team Lineups & Kachinuki** (`internal/domain/team_lineup.go`, `internal/engine/kachinuki.go`): TeamLineup pins position→player for a round or a specific match (mp-825). Team sizes are unregulated and vacancies are never enforced: `TeamLineup.Validate`/`validateFive` (the FIK 5-person back-fill/DQ rule) were REMOVED (mp-gmcg); only the key-only `ValidatePositions` check remains. Kachinuki ("winner-stays-on") is operator-led: `engine.MaybeAdvanceKachinuki` is append-only (adds the next pairing; NEVER auto-finalizes — the roster snapshot is advisory) and the encounter ends only on an explicit `status: completed` score write, which strips trailing unscored auto-appended bouts. Both win rules — exhaustion and taisho-defeated — record `decision: "kachinuki-exhaustion"`; a drawn pool/league encounter is `hikiwake`. A tied pairing may be fought on in encho on that same bout in ANY phase — whether it must produce a result (e.g. the taisho must be defeated) is OPERATOR DISCRETION, never derived from pool-vs-bracket (the phase-independence rule lives in `allowNumberedEnchoFromStore` (`handlers_match.go`) and `subBoutNeedsNumberedEnchoAllowance` (`validation.go`); do NOT re-scope numbered-bout encho by match-ID phase — that was tried and reverted). Only the knockout END stays blocked while tied (the bracket needs a winner, `validateBracketCompletion`). Daihyosen does not exist in kachinuki → `POST .../daihyosen` returns 400. Mistakes: `POST /api/competitions/:id/matches/:mid/reopen` (kachinuki-only) flips a completed match back to running, clearing winner/decision but keeping the bout log; `POST .../matches/:mid/requeue-blocker-and-reopen` atomically frees the target's court (requeuing the blocking match) and reopens under one court-lock hold; `DELETE .../matches/:mid/kachinuki-bout` removes the current trailing unscored auto-appended pairing.
- **Schedule Estimator** (`internal/engine/schedule.go`): `EstimateSchedule(EstimateInput) ScheduleEstimate` produces total/per-court minutes from match duration × multiplier × slowest-court buffer. Exposed via stateless `GET /api/schedule/estimate` on both the CLI web server and the mobile app.
- **Store Transactions** (`internal/state/transactions.go`): `Store.WithTransaction(compID, fn)` holds the per-comp lock once across multiple load/save operations. Use the `StoreTx` handle inside `fn`. Do NOT call public Store methods (they would deadlock the non-reentrant mutex).
- **Cache invalidation: never key a cache on mtime alone** (`internal/state/store.go`): filesystem mtimes come from the kernel coarse clock (~1ms granularity), so two writes inside one tick are indistinguishable and an mtime-keyed cache serves pre-write data (mp-n6ke: stale standings vs fresh matches injected a phantom tiebreaker bout and stalled bracket advancement; mp-p7n was the same class). Each `fileCache` therefore carries a monotonic `version` counter, exposed as `Store.FileVersion(compID, filename)`; `engine.CalculatePoolStandings` requires BOTH version and mtime to match (the counter catches same-millisecond in-process writes, mtime still catches out-of-process edits). **If you add a writer for a file some cache derives from, call `s.bumpFileVersion` AFTER the bytes land**: an extra bump only costs a recompute, a missed bump serves stale data, and bumping before the write lets a reader stamp OLD bytes with the NEW token. Existing bumps sit at the chokepoints every writer funnels through (`savePoolMatchesLocked`, `saveOverridesLocked`) plus draw deletion and transaction abort. Two further rules follow from the counter being the cache key: **every read path must validate it, not just the fast path** (the `CalculatePoolStandings` single-flight loser returned the winner's entry unvalidated, and the winner stamps *pre-compute* tokens, so it could predate the loser's own write), and **the counter must stay monotonic for the life of the process, including across a competition being deleted and recreated** (IDs are name slugs, so `DeleteCompetition` calls `discardCompCacheBodies` to clear the cached bodies while leaving the counters climbing, rather than `compCache.Delete`, which reset them to 0 and let a recreated competition match its predecessor's stale entry). Prefer a deterministic counter over a sleep, which only hides the race.
- **Only `saveCompetitionChangedLocked` may create the competition directory** (`internal/state/overrides.go`, `competitor_status.go`, `team_lineup.go`): `saveOverridesLocked`, `saveCompetitorStatusLocked` and `saveTeamLineupsLocked` each used to `os.MkdirAll` before writing, so a write landing after `DeleteCompetition` rebuilt `competitions/<id>/` around a lone `overrides.json` / `competitor-status.yaml` / `lineups.yaml`. That orphan survives the delete (`ListCompetitions` returns every directory under `competitions/`) and, because IDs are name slugs, a same-named recreation adopts the dead competition's data. **Locking cannot fix this**: the write does not have to interleave with the delete to resurrect the directory, only to run after it (the two status/lineup writers take the very lock `DeleteCompetition` holds, so they cannot interleave at all, and still resurrected it). `atomicWriteFile` opens its temp with `O_CREATE` but never creates the parent, so once the `MkdirAll` is gone a write to a deleted competition correctly fails with ENOENT. Creating the directory is competition *creation*'s job and `saveCompetitionChangedLocked` is the sole legitimate `MkdirAll` site (plus `Store.init` for the root/WAL folders) — do not strip that one, and do not reintroduce the pattern in a new per-competition writer. The interleaving half is closed separately: `overrides.json` is the one competition file whose writers serialize on the store-wide `s.mu` rather than the per-competition lock, so `DeleteCompetition` takes **both**, in the order compLock then `s.mu` (`s.mu` is a sink: nothing holding it acquires a comp lock, so the edge cannot close a cycle). **Do not "tidy" this by moving overrides onto the per-competition lock**: `computeStandingsFrom` runs inside `WithTransaction`, which already holds that lock, and reads overrides via `e.store.LoadOverrides`, so it would self-deadlock the live scoring path on a non-reentrant mutex.

### Excel workbook construction

The workbook is built entirely from code in `internal/excel/template.go` (`NewFileFromScratch`). Each sheet (data, Time Estimator, Pool Draw, Pool Matches, Elimination Matches, Names to Print, Tree) is created and styled programmatically. Layout constants (rows-per-page, spacing, max bracket size) and sheet name constants (`SheetData`, `SheetPoolDraw`, etc.) live in `internal/helper/constants.go`. Use these constants everywhere rather than string literals.

### Resource Embedding

`main.go` embeds `web/*` via `//go:embed`. The global var `helper.WebFs` exists for backward compatibility with code paths that still reference it directly. Must rebuild after changing embedded files.

### Mobile-app runtime defaults

Production-hardening defaults applied in the `mobile-app` command. Constants live in [cmd/mobile_app.go](cmd/mobile_app.go) and [internal/mobileapp/middleware.go](internal/mobileapp/middleware.go) / [hub.go](internal/mobileapp/hub.go):

| Concern | Default | Override | Rationale |
|---|---|---|---|
| `ReadHeaderTimeout` | 10s | (none) | Slowloris-header defense |
| `ReadTimeout` | 30s | (none) | Slow-body defense (still permits multi-MB CSV import) |
| `IdleTimeout` | 120s | (none) | Bounds fd commitment per idle keep-alive client |
| `WriteTimeout` | **0** (unbounded) | (none) | SSE streams are infinite; per-request cancellation runs via `Request.Context().Done()` |
| `MaxHeaderBytes` | 1 MB | (none) | Header-bomb defense |
| Body cap (admin JSON) | 1 MB | `DefaultMaxBodyBytes` const | `c.BindJSON` payloads are tiny in practice; cap is enforced by `MaxBodyBytes` middleware (returns 413) |
| Body cap (`/tournament/import`) | 64 MB | `MaxImportBodyBytes` const | Matches `ParseMultipartForm` already in the handler |
| SSE subscribers | 5000 | `SSE_MAX_CLIENTS` env var | Bounds fan-out cost + per-client goroutine/channel allocation (~4–10 KB resident per client); raised from 1000 → 5000 by mp-9afd for large-scale events (1000+ viewers); real hardware load test still required |
| Graceful shutdown | 30s | `httpShutdownTimeout` const | `Hub.Close` is wired via `srv.RegisterOnShutdown` so SSE goroutines exit before the deadline |

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

In-progress migrations and tech debt to keep in mind (re-derive package sizes with `gocloc` when you need numbers):

- **`mobileapp/` is the largest package** after the decision/eligibility/lineup/daihyosen/Swiss/display/league/registration/announcement/branding/sponsors/print handler families plus supporting infra (rate limiting, broadcast coalescer, viewer single-flight). Grouping into subpackages may be warranted.
- **`engine/` has grown rapidly**: scoring, eligibility, kachinuki, daihyosen, Swiss, scheduling, league tie-breaks, participant replacement, PDF export. Sub-splitting may be warranted.
- **`helper/` mixes concerns**: tree algorithms, CSV parsing, Excel rendering, seeding, utilities. The `helper/{bracket,csv,seeding}/` subpackages are an in-progress extraction; `helper/` proper has not shrunk yet.
- **`excel/` has minimal direct test coverage** (roughly 0.3x source) despite Excel being the primary CLI deliverable; most Excel coverage lives in `helper/*_test.go`.
- **`domain/` adoption is partial**: much business logic still uses `helper.Player` directly rather than domain types; the migration is incomplete.
- **No top-level interfaces** for `state.Store` or `engine.Engine`: interface adoption is incremental via `mobileapp/deps.go`; engine-to-state and helper-to-engine calls still use concrete types.

## Testing Conventions

- **Table-driven tests** with `t.Run()` subtests throughout (see `seed_test.go`, `tree_test.go`)
- **Package naming**: `_test` suffix for external tests of `domain`; same package (`package helper`, `package cmd`) for `helper` and `cmd` tests
- **Test helpers**: `internal/test/helpers.go` has factories (`CreateTestPlayers`, `CreateTestPools`, `CreateTestTournament`)
- **Assertions**: `testify/assert` for non-fatal, `require` for fatal
- **Cleanup**: Always use `defer` for temp files, servers, and env vars

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

- Excel coordinates matter: changing match generation requires updating cell references and formula links across sheets
- `team-matches=0` means individual tournaments, not team tournaments
- The `errcheck` linter is enabled (test files excepted). Don't introduce `_ =` or bare ignored returns in production code. Wrap and propagate, or log via `handleExcelError`/`handleExcelDataError`
- Web UI changes (`web/index.html`) should be validated in a running browser, not just by reading diffs. Use `make run`
- Mobile app frontend changes (`web-mobile/`) require rebuilding the binary to take effect. The files are embedded at `go build` time via `//go:embed web-mobile/*` in `main.go`. Run `make run-mobile` which rebuilds automatically, or run `make go/build` then restart.
- Duplicate participant names in the CSV are rejected up front by `helper.CheckDuplicateEntries`; the web handler surfaces these to the user
- Chained match navigation in the admin score editor (Prev/Next buttons, Finish + Start Next, ←/→ keys) must stay on the current match's shiaijo. Operators run matches per-court, so hopping courts mid-flow breaks the workflow. See `AdminScoreEditor` in `web-mobile/js/admin_schedule.jsx`: filter to `(m.court || "") === (openMatch.court || "")` so empty/undefined courts share one "unassigned" bucket.
- Docs under `docs/` are PUBLIC-facing: no beads (`mp-xxxx`)/`bd`, no internal-tooling or `CLAUDE.md` references, and NO em-dashes. The PR template gates `make docs/build`; run it before pushing docs changes.
- `mkdocs build --strict` fails on broken cross-file links (WARNING) but only logs missing same-page anchors as INFO (build still passes), so verify anchors by hand. Anchors use pymdownx slugs: lowercase, punctuation dropped, `--`/parens collapsed to single hyphens (e.g. `### Locked mode (--lock-password)` becomes `#locked-mode-lock-password`). Note TWO files named `mobile-app.md` exist (`docs/user-guide/mobile-app.md` guide vs `docs/user-guide/commands/mobile-app.md` reference); a relative link from `commands/` resolves to the sibling, so confirm the target file before trusting a flagged anchor.

## PR Workflow

- **Build the PR body from the repo template.** When creating a PR, populate the description from `.github/pull_request_template.md` and fill every section: `gh pr create --body-file <filled-template>` (the bare `gh pr create` / `--fill` does NOT apply the template). Set the `Closes bc-xxxx` bead reference (`mp-xxxx` for older beads that predate the prefix change).
- **Embed screenshots via the `pr-assets` side branch, not gists** (`gh gist create` rejects binaries). Push the PNG to the `pr-assets` branch (never merged to main): `gh api --method PUT .../contents/pr-assets/<pr>/shot.png -f branch=pr-assets -f content="$(base64 < shot.png | tr -d '\n')"`, then embed `![](https://raw.githubusercontent.com/gitrgoliveira/bracket-creator/pr-assets/pr-assets/<pr>/shot.png)`. A real browser/MCP screenshot is MANDATORY for any UI change. There is NO textual/DOM/geometry substitute. If you have not captured one, the PR is not review-ready: capture it first, then fill the Screenshots section. Full verified recipe: the `/pr-screenshots` skill. **Capture only via the browser/MCP screenshot tools, NEVER a desktop or full-screen grab (`screencapture`, `scrot`, OS shortcuts), which exposes the user's private screen.**
- **Test plan is a gate, not a formality.** Before requesting review on a PR, check off EVERY item in the PR description's test plan. Do not mark a PR ready while any checkbox is unverified. Manual/browser steps are not optional; execute them, then check them.
- **Keep the bead `in_progress` until the PR actually merges.** A green review is not a merge. Only `bd close <id>` after the merge lands, with a reason referencing the merge commit/PR.
- **After a merge, run the full `/cleanup` sequence** (close bead → fast-forward main → remove worktree → delete local + remote branch → prune). Don't wait to be asked for each step. See the `/cleanup` skill.
- **Verify the worktree/branch before any edit.** This repo uses a git worktree per PR; edits applied to the wrong worktree (or directly to the `main` checkout) force patch-and-revert recovery. When there is any ambiguity, confirm with `pwd` and `git branch --show-current` before the first Edit/Write. Never edit the main checkout directly; always work inside a worktree.

## Code Review

- **There is no automated Copilot review loop.** It was retired on 2026-07-24. Do not run `/review-loop`, do not re-request Copilot, and never treat "waiting for a bot review" as a merge gate.
- **Review threads still appear — the repo owner posts them by hand.** Read and address them like any other review feedback. What is retired is the bot and the loop around it, not the reviewing.
- **Never report a review round "clean" until a fresh fetch shows zero unresolved threads.** State the total unresolved count first, give every thread an explicit disposition (fix or dismissal with a reason), then re-verify the count is zero.
- **Report `resolved` and `outdated` threads separately.** Never claim zero unresolved threads without checking for outdated-but-visible threads that the user can still see in the GitHub UI. A query that filters out outdated threads produces a false "clean" that contradicts what the user sees.
- **Paginate when counting or resolving threads.** GitHub's `reviewThreads(first:100)` caps at 100; a capped lookup silently finds nothing for threads past #100 and falsely prints "already resolved" while leaving them unresolved, which then blocks merge under a ruleset with `required_review_thread_resolution:true`.
- Deeper passes are run on request: `/tri-review`, `/code-review`, `/security-review`, `/impeccable critique`. A zero-findings result from a reviewer that never ran looks identical to a genuinely clean review — check for agent failures before trusting one.
- Run `make go/test` after fixes and before pushing. A red gate means fix-or-revert, never push.

## Testing & Verification

- **Verify in the browser; never substitute API/curl calls.** Manual test-plan items and UAT must be executed through the actual UI.
- **Test self-run / public features from the PUBLIC page, not the admin UI**: the public flow is what users hit. Admin-side scoring proves nothing about it.
- **File gap/UX issues incrementally as you find them**, not batched at the end of a UAT pass.
- Frontend changes under `web-mobile/` require a rebuild to take effect (`//go:embed`); use `make run-mobile` or rebuild + restart.
- **Diagnose failures from evidence, never fabricate a cause.** When a test, build, or CI step fails (Codecov, GPG, lint, etc.), read the actual logs before explaining it. Do not invent "known bugs", version-specific regressions, or other rationalizations to justify a workaround. If the root cause isn't established, say so and keep investigating.
- **Test coverage gate: every package that has test files must maintain ≥85% statement coverage.** Verify before any PR with:
  ```bash
  go test -race -cover . ./cmd/... ./internal/... ./tests/...
  ```
  Packages below 85% must be brought up before merging. New packages must include test files covering their public API. Tracked in bead mp-3abe.
  **Intentionally untested:** `internal/domain/internal/glossarygen` is a `go generate` code-generator (emits `glossary_data.js`); it has no exported API and is excluded from the gate. `internal/helper/bracket`, `internal/helper/csv`, and `internal/helper/seeding` are empty stub packages (no exported symbols yet) and are likewise excluded.

## Merge & Rebase

When rebasing or resolving conflicts, watch for these recurring breakages:
- Duplicate declarations introduced by the rebase (same symbol defined twice after a merge).
- UUID-vs-name-string mismatches in player/entity maps: match on id OR name, and use participant UUIDs (not display names) for bracket-highlight IDs.
- Missed call sites when removing or renaming a symbol: `grep -r` the name across **all** packages **including `_test.go` files** before committing. A refactor that compiles can still leave stale test references or skip-test code pointing at dead paths.
- Re-run `make go/test` after every rebase; a clean rebase that compiles must not be semantically broken.

## Refactoring Rules

- **Search ALL call sites, including test files, before removing code or parameters.** Run `grep -r` (or `grep -rn 'SYMBOL' . --include='*.go' --include='*.jsx'`) to find every reference, not just production code. A removal that compiles can still leave stale test references or skip-test code pointing at dead paths.
- **Verify that guards and defensive code are intentional before removing them.** If a Copilot reviewer flags a removal, assume the guard was intentional unless you can prove otherwise from git blame or comments. Aggressive removal of guards (e.g. `sourceCompID` checks, `defer os.RemoveAll`) has had to be reverted.
- **Boy Scout rule: leave code better than you found it, even outside the diff.** When a review or task surfaces a worthwhile adjacent fix — a literal duplicated in sibling files, a comment contradicting the code, a swallowed error in a handler you touched — apply it rather than skipping it as "outside the reviewed diff". Precedent: the shared singleflight response tail was hoisted and the ZIP-magic literal deduplicated across untouched test files precisely because a review flagged them. Two limits keep this from scope creep: don't apply adjacent fixes that change intended behavior (surface those as findings for a decision), and don't let a small PR grow into a refactor — fix what you touched or verified, file the rest.

## Debugging Principles

- **Never fabricate explanations for tool/infrastructure failures.** If you don't know the root cause of a CI failure, say so and investigate. Do not invent "known bugs", version-specific regressions, or other rationalizations to justify a workaround (e.g. a fabricated "known bug in Codecov v6.0.x" masking a transient GPG keyserver failure). Read the actual logs first.


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

- Use `bd` for ALL task tracking. Do NOT use TodoWrite, TaskCreate, or markdown TODO lists
- Run `bd prime` for detailed command reference and session close protocol
- Use `bd remember` for persistent knowledge. Do NOT use MEMORY.md files

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

<!-- Everything below is repo-specific and deliberately OUTSIDE the BEADS INTEGRATION
     markers above, which are tool-managed (hash-stamped) and regenerated by
     `bd setup claude`. Do not move it back inside, or it will be overwritten. -->

## Beads: this repo's setup (overrides the generic notes above)

### Push the beads DB at session end

`git push` does NOT sync beads. **There is no auto-push**: `bd create/update/close` only touch the local Dolt DB, so an issue change exists solely on this disk until you run:

```bash
bd dolt push     # sync issue changes to the private remote
bd dolt pull     # pick up changes made on another machine
```

Treat this as an extra mandatory step in the Session Completion workflow above, alongside `git push`. `sync.auto-push` is accepted by bd 1.1.0 but silently ignored, so never rely on it.

### Where the issues live

- **New issues use the `bc-` prefix; existing issues keep the IDs they already have.** Create one with `bd create "<title>" --id bc-<4-char-slug> --force`. BOTH flags are required: without `--id` bd mints an `mp-` ID from the in-DB `issue_prefix` (still `mp`), and without `--force` it refuses with `prefix mismatch: database uses 'mp-'`. Pick the slug yourself in the existing house style and check it is free first.
- **Do not "fix" this by renaming the prefix.** `bd config set issue_prefix` is refused outright, and `bd rename-prefix` rewrites the whole DB. With this repo's three prefixes present (`mp`, `mp-mol`, `bracket-creator`) it requires `--repair`, which REGENERATES every ID as random 8-hex (`mp-gmcg` → `bc-b90638d7`; verified on a throwaway DB copy — 0 of 581 mnemonics survived). That would strand ~1240 `mp-xxxx` references across the codebase, docs, git history and merged PR bodies. Mixed prefixes are fine: a `bc-` bead is fully first-class and can depend on an `mp-` bead.
- **Historical IDs:** ~550 use `mp-` (including `mp-mol-*` molecules poured from formulas) and a small legacy set uses `bracket-creator-*`. They stay as they are, so every existing reference in code comments and docs remains valid.
- The DB lives in the local Dolt store under `.beads/` (database `mp`, named in `.beads/metadata.json`). All of `.beads/` is gitignored, so the DB is never committed here.
- **Sync remote:** the PRIVATE Forgejo repo `Ricardo/bracket-creator-beads` over `git+https`, stored as `sync.remote`. Beads push to `refs/dolt/data` there, deliberately NOT to `origin` — this GitHub repo is public and the issue DB is not. Do not add a beads remote pointing at a public repo.
- **`.beads/` is shared by every worktree** (bd walks up from the worktree to the repo root), so concurrent sessions write to one DB. Never swap, restore, or replace the DB while another session may be running.

### Gotchas that have already cost time

- **`store is read-only` means a prefix-ownership problem, not a lock or corruption.** bd routes writes by issue-ID prefix; a prefix this repo does not own is routed elsewhere (see `routing.*` in `.beads/config.yaml`) and rejected. Run the write where that prefix lives, or fix the routing. Diagnose by comparing a native-prefix write against the failing one.
- **`bd config set` / `unset` are unreliable on nested keys.** `set` can rewrite flat `a.b:` keys into a nested `a:` block; `unset` can report success while leaving the key in the file. Always confirm with `bd config get <key>` AND by re-reading `.beads/config.yaml`, and hand-edit when they misbehave.
- **`bd where` reports the wrong prefix here.** It reads `issue_prefix` from `.beads/config.yaml` (which says `bc`), but ID generation uses the value stored in the DB (`bd config list` → `issue_prefix = mp`). The file value is inert. Trust `bd config list`, not `bd where`.
- **`bd export` JSONL is lossy: it carries `dependency_count` but no dependency edges, and no comment bodies.** Never migrate or restore a beads DB via export/import — it silently drops blocker relationships. Copy the Dolt database directory instead.
