# Architecture

This document describes the high-level architecture of the bracket-creator application — a Go CLI and web application for generating kendo tournament brackets.

## System Modes

The application runs in four distinct modes from a single binary (`cmd/`):

```
bracket-creator
├── create-pools       CLI: CSV → Excel (pool + playoff format)
├── create-playoffs    CLI: CSV → Excel (direct elimination)
├── serve              Web: stateless form-based Excel generator
└── mobile-app         Web: live tournament management with real-time updates
```

Additional commands: `version`, `hash-password` (bcrypt hash for locked-mode auth), and `print` (render bracket XLSX workbooks to print-ready PDFs via LibreOffice). `man` (man-page generation) is the only hidden command. Folder-diagnostic helpers live in `cmd/diag_*.go` but are not a subcommand.

**CLI mode** reads a CSV participant list, generates bracket structures in memory, and writes an Excel workbook with formula-linked cells for bracket visualization.

**Serve mode** is a full-featured web UI for Excel bracket generation. The SPA (`web/index.html`) provides tournament type selection (pools+playoffs or direct elimination), court configuration, CSV participant input with drag-and-drop and validation, a seeding modal, time estimation, and dark/light theming. Form values auto-save to localStorage. On submit, the backend generates the Excel workbook and returns it as a download. No server-side persistence.

**Mobile-app mode** is a full tournament management platform: CRUD for tournaments/competitions, live match scoring, decision recording (kiken/fusenpai/daihyosen), team lineups, Swiss/Kachinuki formats, league tie-breakers, mid-tournament participant replacement, self-service registration, public display surfaces (TV/lobby/streaming overlay), operator content (announcements, branding, sponsors), PDF/Excel print, real-time push via SSE, and file-backed persistent state. The frontend is a Preact SPA embedded in the binary.

## Layered View

At the macro level, the codebase follows a layered architecture with no upward dependencies:

```
Presentation (cmd/, mobileapp/, web/, web-mobile/)
        │
   Business logic (engine/, helper/, service/)
        │
   Persistence (state/, excel/)
        │
   Domain models (domain/)
```

- **Presentation:** parses inputs (flags, HTTP requests), orchestrates flow, returns outputs/errors.
- **Business logic:** kendo tournament rules — pool generation, seeding, bracket/tree construction, scoring, dojo-conflict resolution, eligibility, lineup, decision semantics.
- **Persistence:** file-backed state store for live tournaments (with WAL for multi-file transactions); Excel workbook construction.
- **Domain models:** core entities (Player, Pool, Match, Tournament, Seed, Decision, CompetitorStatus, TeamLineup) decoupled from presentation and I/O.

## Package Map

This is a responsibility map at the package level, with a few representative anchor files per package — it is deliberately **not** a file-by-file inventory (those drift on every commit; let `go doc ./...` and a directory listing be the source of truth). Each entry describes what the package owns and where to start reading.

```
main.go        Entry point; embeds web/ and web-mobile/ via //go:embed
specs/         OpenAPI spec, feature specs, this document
web/           index.html — full SPA for the stateless Excel generator (serve mode)
web-mobile/    Preact SPA for the live tournament app (embedded, served by mobile-app)
```

| Package | Owns | Start here |
|---|---|---|
| `cmd/` | Cobra CLI commands — the larger ones (e.g. `create-pools`) use an options struct + `run()`; small ones (`version`, `man`) are plain `cobra.Command`. `serve`/`mobile_app` boot the web servers; shared CLI logic in `shared.go`. | `root.go`, `shared.go`, `serve.go`, `mobile_app.go` |
| `internal/domain/` | Pure domain models with **zero internal dependencies** — Player, Pool, Match, Tournament, Seed, Decision, CompetitorStatus, TeamLineup, plus the UI glossary. | `decision.go`, `team_lineup.go` |
| `internal/helper/` | Core algorithms + all Excel rendering (the historical catch-all). Bracket trees, seeding, pool creation, CSV parsing, `excel_*.go` renderers. Subpackages `bracket/`, `csv/`, `seeding/` are an in-progress extraction. | `tree.go`, `seed.go`, `tournament.go`, `constants.go` |
| `internal/excel/` | Excel file lifecycle (`Client`) and full-workbook construction. | `template.go` (`NewFileFromScratch`) |
| `internal/engine/` | Business logic for live tournaments — scoring, pools/bracket advancement, ranking & tie-breaking, scheduling, eligibility, kachinuki, daihyosen, Swiss, participant replacement, Excel/PDF export. | `engine.go`, `scoring_tx.go`, `eligibility.go` |
| `internal/state/` | File-backed persistence with mtime caching + WAL. Markdown/YAML and CSV/JSON readers per artifact; multi-file transactions. | `store.go`, `transactions.go`, `models.go`, `wal/wal.go` |
| `internal/mobileapp/` | Gin HTTP handlers (`handlers_*.go`, grouped by feature), SSE hub, auth, and supporting infra (rate limiting, broadcast coalescing, viewer single-flight, `safeGo`). Handlers depend on `deps.go` interfaces. | `server.go`, `hub.go`, `deps.go`, `middleware.go` |
| `internal/resources/` | Embedded-filesystem abstraction (`fs.FS`) over the bundled web assets. | — |
| `internal/service/` | Thin service layer over helper logic. | `tournament_service.go` |
| `internal/test/` | Shared test helpers and factories. | `helpers.go` |

The `web-mobile/js/` SPA is organized by feature prefix rather than enumerated here: `app.jsx`/`router.jsx` (shell + routing), `api_*.jsx`/`data.jsx`/`patch.jsx` (transport + state), `viewer_*.jsx` (public viewer), `display_*.jsx`/`streaming_overlay.jsx` (TV/lobby/overlay), `admin_*.jsx` (operator surfaces — setup, competition, pools, schedule, scoring, lineup, shiaijo, content), and `registration.jsx`/`reset.jsx` (public self-service). Tests live in `js/__tests__/` (Vitest, including `render/` DOM tests); compiled output in `dist/`.

## Dependency Graph

Arrows point from dependent to dependency. No circular dependencies exist.

```
cmd ──────────────┬──→ mobileapp ──→ engine ──→ state ──→ domain
                  │         │                     │
                  │         ├──→ state             ├──→ helper (Player, UUID)
                  │         └──→ resources         │
                  │                                └──→ filesystem
                  ├──→ engine
                  ├──→ helper ──→ domain
                  ├──→ excel  ──→ excelize (lib)
                  ├──→ domain
                  └──→ resources

engine ──→ helper (seeding, tree, scores)
       ──→ excel  (export)
       ──→ state  (persistence, transactions)

mobileapp ──→ engine     (business logic, via deps.go interfaces)
          ──→ state      (direct reads for viewer endpoints)
          ──→ resources  (embedded frontend files)
```

`domain` has zero internal dependencies — it is the leaf of the graph.

## Data Flow

### CLI Mode (create-pools / create-playoffs)

```
CSV file ──→ helper.ReadEntriesFromFile()
                │
                ▼
         processEntries() ──→ []helper.Player
                │
                ▼
         helper.ParseSeedsFile() (optional)
                │
                ▼
         domain.AssignSeeds() (mutates Player.Seed)
                │
                ▼
         excel.NewFileFromScratch() + helper rendering functions
                │
                ▼
           Excel workbook (.xlsx)
```

### Serve Mode (web → Excel)

1. Gin router receives a POST request with tournament configuration and a CSV list of participants.
2. The `cmd` layer validates the request, checks for duplicate players, and parses any provided seed assignments.
3. `PoolSeeding` distributes top players to prevent early clashes. Remaining players are assigned to pools, strictly respecting dojo-conflict avoidance rules.
4. Winners from the generated pools are mapped to a binary tree representing the knockout stage.
5. The `excel` layer creates a workbook in memory: sheets for Pool Draws, Pool Matches, and Elimination brackets, with formulas linking pool winners to the playoff tree.
6. The complete Excel file is streamed back to the client as a binary download.

The serve mode also exposes the stateless `GET /api/schedule/estimate` endpoint shared with mobile-app.

### Mobile-App Mode (live tournament)

```
Admin Client ──→ PUT /api/tournament
                    │
                    ▼
               state.Store.SaveTournament() ──→ tournament.md

Admin Client ──→ POST /api/competitions/:id/start
                    │
                    ▼
               engine.StartCompetition()
                    ├──→ state: load participants, seeds
                    ├──→ helper: generate pools/bracket/schedule
                    └──→ state: save pools.csv, bracket.json, schedule.csv
                    │
                    ▼
               hub.Broadcast(EventCompetitionStarted)
                    │
                    ▼
               SSE push ──→ All connected clients

Admin Client ──→ POST /api/competitions/:id/matches/:mid/score
                    │
                    ▼
               Store.WithTransaction(compID, tx → {
                    engine.StartMatchTx()                  ← eligibility gate
                    engine.RecordMatchResultWithIneligibilityTx()
                       ├──→ apply scores
                       ├──→ write pool-matches.csv | bracket.json
                       ├──→ write competitor-status.yaml (on kiken/fusenpai)
                       └──→ write lineups.yaml (on team lineup freeze)
                  })
                  → WAL commits all writes atomically
                    │
                    ▼
               hub.Broadcast(EventMatchUpdated [+ EventCompetitorStatusUpdated])
                    │
                    ▼
               SSE push ──→ Viewer clients update in real-time
```

### State on Disk

```
tournament-data/
├── tournament.md                       YAML front-matter: name, date, venue, courts, password
├── wal/                                Pending multi-file transactions (replayed on startup)
├── branding/                           Uploaded logo (logo.png|jpg) for display surfaces
├── sponsors/                           Uploaded sponsor images
└── competitions/
    └── {compID}/
        ├── config.md                   YAML front-matter: format, pool size, courts, etc.
        ├── participants.csv            One participant per line (UUID-prefixed)
        ├── seeds.csv                   Seed rank → player mapping
        ├── pools.csv                   Pool assignments after start
        ├── pool-matches.csv            Pool phase match results
        ├── bracket.json                Elimination bracket structure and results
        ├── schedule.csv                Court/time assignments
        ├── competitor-status.yaml      Eligibility records (kiken/fusenpai losers)
        ├── lineups.yaml                Team lineups, keyed by round
        └── overrides.json              Manual ranking overrides
```

## Concurrency Model

`state.Store` uses a two-level locking scheme:

1. **Per-competition `sync.RWMutex`** (via `sync.Map`) — isolates competitions from each other. A score update to competition A never blocks reads of competition B. The mutex is **non-reentrant**, so transaction bodies must use the `StoreTx` handle rather than calling public `Store` methods.
2. **Per-file `fileCache`** with its own `sync.RWMutex` — within a competition, different files (pools vs bracket vs schedule) can be read concurrently.

Cache invalidation uses **file mtime**: on each read, the cached mtime is compared against `os.Stat`. A mismatch triggers a re-parse under the write lock. This handles external file edits gracefully.

**Single-file durability** (`state/atomic_write.go`): tmp file → fsync → rename → fsync(dir).

**Multi-file transactions** (`state/transactions.go` + `state/wal/`): `Store.WithTransaction(compID, fn)` acquires the per-competition lock once and runs `fn` against a `StoreTx` handle. All writes within `fn` stage into an in-memory intent log; on success the WAL is committed atomically before any target file is touched. A crash after WAL commit but before all writes finish is recovered by replaying the WAL on `NewStore`. This is what lets the score handler write `pool-matches.csv` + `competitor-status.yaml` + `lineups.yaml` atomically.

The `engine.Engine` maintains a separate `standingsCache` (sync.Map) with a `sync.Once`-based flight deduplication to prevent thundering-herd re-computation of standings.

The SSE `Hub` uses a non-blocking send pattern: if a client's 100-message buffer is full, it is unsubscribed immediately rather than blocking the broadcaster.

## Mobile-App Endpoints

Routes are registered in `internal/mobileapp/server.go` in three tiers:

- **Public viewer (`/api/viewer/*`)** — unauthenticated read paths for the public viewer SPA: pools, brackets, schedules, results, plus the `/display` surfaces (TV/lobby/overlay).
- **Public read-only (`/api/*`)** — `GET /schedule/estimate` (stateless), `GET /competitions/:id/competitor-status`, `GET /competitions/:id/teams/:tid/lineups/:round`, plus Swiss read endpoints.
- **Admin (`/api/*`, auth-required)** — tournament/competition/participant CRUD, match scoring (`POST /matches/:mid/score`), decisions (`POST /matches/:mid/decision`), eligibility, lineup freeze, daihyosen, Swiss round advancement, import.

Auth is `X-Tournament-Password` header validated by `middleware.go` against the tournament record. `deps.go` defines the consumer-boundary interfaces (`CompetitionStore`, `ScoringEngine`, etc.) so handlers can be unit-tested without spinning up the real engine + store stack.

### SSE Events

The `Hub` broadcasts the following event types over `GET /api/events`:

| Event | Triggered by |
|---|---|
| `match_updated` | score / decision / status change (coalesced ≤4/s per match) |
| `competition_started` | `POST /competitions/:id/start` |
| `competition_completed` | final match resolved |
| `tournament_updated` | tournament-level CRUD |
| `schedule_updated` | court/time edits, schedule regenerated |
| `competitor_status_updated` | eligibility change (new kiken/fusenpai) |
| `participants_updated` | participant add/edit/remove/import |
| `lineup_updated` | team lineup freeze/change |
| `draw_generated` / `draw_discarded` | pool/bracket draw created or rolled back |
| `announcement` | operator posts/clears an announcement |
| `password_reset` | tournament password changed via reset flow |

## Frontend Architecture

**Stack**: Preact (lightweight React alternative), JSX compiled by esbuild, Vitest for tests.

**Routing**: preact-router via `router.jsx` (thin wrapper exposing `<Router>`, `<Route>`, `route()`, `useQuery()`). URL is the source of truth for the active route; the App component still owns richer view state (admin sub-tab, viewer screen) so route → state hydration is explicit.

**State**: Preact `useState`/`useEffect`/`useRef` hooks in `app.jsx`. No external state library.

**Real-time**: `EventSource` on `/api/events` receives SSE messages. Match updates are merged into local state via `applyPatch()` in `patch.jsx`.

**Auth**: Admin mode requires `X-Tournament-Password` header, stored in `localStorage`. Two server-side modes selected at startup by `internal/mobileapp/auth_source.go`:

- **File mode** (default): header is compared plaintext against `tournament.md`'s `password` field. A public `POST /api/tournament/reset` lets an operator who's forgotten the password set a new one without authenticating.
- **Locked mode** (`--lock-password` flag + `TOURNAMENT_PASSWORD_HASH` env var): header is compared via bcrypt against the env-var hash; on-disk password is ignored; `POST /api/tournament/reset` returns 404 (the SPA `/reset` page is still served and renders an operator-disabled message). Recommended for internet-exposed deployments. Generate the hash with `bracket-creator hash-password`.

The SPA discovers the active mode via the public `GET /api/auth-config` endpoint so it can hide the "Forgot password?" link in locked mode.

**Component tree**:

```
App (app.jsx)
├── Viewer mode (public)
│   ├── TournamentHome
│   ├── CompetitionViewer (pools, bracket, standings, results, awards)
│   ├── ScheduleView, MatchView
│   └── Notifications / Alerts / Watchlist
│
├── Display mode (/display — TV/lobby/scoreboard/streaming overlay, public, query-param driven)
│
├── Registration / Reset (public self-service surfaces)
│
└── Admin mode (password-protected)
    └── AdminShell
        ├── AdminDashboard
        ├── AdminTournament (+ branding, sponsors, announcements)
        ├── AdminCompetition (overview, settings, bracket, swiss)
        ├── AdminParticipants
        ├── AdminPools
        ├── AdminSchedule (score editor with chained court-scoped navigation)
        ├── AdminShiaijo (per-court now-playing / up-next board)
        ├── AdminScoring (individual / team modal with autosave)
        ├── AdminLineup (team lineup composer)
        └── ImportTournament
```

## Key Algorithms

**Binary bracket tree** (`helper/tree.go`): Recursive subdivision into `Node` structs with `Left`/`Right` children. Max 16 players per tree page. Multi-page output splits the tree and links pages via cell references.

**Seeding** (`helper/seed.go`): `StandardSeeding()` uses `generateBracketOrder()` for placement. `PoolSeeding()` interleaves seeds across courts so `ReorderPoolsForCourts` lands top seeds on different courts and opposite ends. `ApplySeeds()` handles collisions by swapping.

**Pool creation** (`helper/tournament.go`): Greedy algorithm with dojo-conflict avoidance. Pools distributed contiguously across courts.

**Tie-breaking**: Multi-criteria cascade — wins → losses → draws → points scored → points lost (individual). Team tournaments add team-level criteria before individual criteria. See CLAUDE.md for the full precedence.

**Decision types** (`internal/domain/decision.go` is the source of truth): the canonical wire values include `""`, `fought`, `hikiwake`, `kiken` (legacy), `kiken-voluntary` (FIK Art. 31, permanent), `kiken-injury` (FIK Art. 30, reinstateable), `fusenpai`, `fusensho`, `daihyosen`, `kachinuki-exhaustion`, `ippon-shobu`. Use `domain.IsKikenDecision`/`IsKikenDecisionStr` to match any kiken variant. Legacy YAML `decision: true` migrates to `hikiwake`, `false` to `fought`, and `kiken` to `kiken-voluntary` (Decision.UnmarshalYAML).

**Competitor eligibility** (`engine/eligibility.go`, `state/competitor_status.go`): a kiken/fusenpai decision auto-writes `CompetitorStatus{Eligible: false}` for the loser. `engine.StartMatchTx` is the FR-035 pre-flight gate — returns `*IneligibleCompetitorError` (matches `errors.Is(err, ErrIneligibleCompetitor)`), mapped to HTTP 409 by the score handler. Re-scoring a match that itself created the ineligibility is permitted (undo path). Kiken-injury (FIK Art. 30) sets `Reinstateable: true`; an admin can restore eligibility via `POST /competitions/:cid/competitors/:pid/reinstate`. Kiken-voluntary (Art. 31) and fusenpai are not reinstateable.

**Team lineups & kachinuki** (`domain/team_lineup.go`, `engine/kachinuki.go`): TeamLineup pins position → player for a round. FIK 5-person rule: Senpo + Taisho mandatory; 1 vacancy must be Jiho, 2 must be Jiho+Fukusho, 3+ disqualifies. Kachinuki ("winner-stays-on") dynamically appends bouts via `engine.AdvanceKachinuki` until one team is exhausted (`DecisionKachinukiExhaustion`).

**Schedule estimator** (`engine/schedule.go`): `EstimateSchedule(EstimateInput) ScheduleEstimate` returns total/per-court minutes from match duration × multiplier × slowest-court buffer. Exposed via stateless `GET /api/schedule/estimate` on both the CLI web server (`serve`) and the mobile app.

**Swiss format** (`engine/swiss.go`): pairings + round advancement for Swiss-style competitions.

**League tie-breaker** (`engine/league_tiebreak.go`, `engine/tiebreaker.go`): operator-driven play-off bouts to resolve tied standings in league/pool formats when the automatic multi-criteria cascade cannot separate competitors.

## Design Patterns & Principles

- **Command Pattern** — Cobra encapsulates execution logic for each CLI subcommand.
- **Dependency Injection** — Embedded resources are exposed through an `fs.FS` interface, so production runs use `embed.FS` and tests can swap in `fstest.MapFS`. Mobile-app handlers depend on consumer-boundary interfaces in `internal/mobileapp/deps.go` (NFR-002) rather than concrete types, so handler-level tests avoid temp dirs and real engine wiring.
- **Fail-Fast Error Handling** — Strict linter enforcement (`errcheck`) and comprehensive input validation catch configuration or formatting errors before engaging the heavy Excel generation logic.
- **Immutable Output** — The application does not edit existing Excel templates on disk. It produces a fresh, deterministic workbook on every run, built dynamically in `internal/excel/template.go` from layout constants in `internal/helper/constants.go`.
- **Single-binary deployment** — All frontend assets, templates, and static files are embedded at compile time via `//go:embed`. No runtime file dependencies.

## Build and Deployment

```
make go/build
  1. esbuild: compile web-mobile/js/*.jsx → web-mobile/dist/
  2. go build: embed web/ and web-mobile/ via //go:embed
  3. ldflags: inject version, commit hash, build time
  4. Output: bin/bracket-creator (single self-contained binary)
```

The binary includes all frontend assets, templates, and static files. No runtime file dependencies. Distributed as a single executable.

Docker images available via `Dockerfile` and `Dockerfile.mobile`.

## Codebase Size

Relative sizing only — exact line counts go stale immediately and aren't worth hand-maintaining. Re-derive with a `wc -l` / `gocloc` sweep when you need numbers.

**Package mass** (largest → smallest): `mobileapp` ≫ `engine` ≳ `helper` ≈ `state` ≫ `cmd` ≫ `domain` ≫ `excel`. The Go backend and the JSX frontend are roughly comparable in size.

**Test investment** (test LOC ÷ source LOC): most packages sit around 1.5–1.9× — substantial, algorithm-heavy test bodies. Two outliers:

- `excel` ≈ 0.3× — thinly tested directly; most Excel coverage lives in `helper/*_test.go` instead.
- Frontend ≈ 0.8× — lighter than the Go side, though still a substantial Vitest suite.

## Known Architectural Observations

### Strengths

- **Clean layering**: presentation → engine → state → filesystem with no circular dependencies.
- **Strong algorithmic test coverage**: helper, engine, state, and mobileapp all carry substantial test bodies (roughly 1.5–1.9× source).
- **Single-binary deployment**: all assets embedded at compile time.
- **Fine-grained concurrency**: per-competition + per-file locking avoids global contention; non-reentrant mutex caught misuse via `StoreTx`.
- **Multi-file atomicity**: WAL-backed `Store.WithTransaction` lets the score handler commit several files (match result + eligibility + lineup) atomically across a crash.
- **Real-time push**: SSE hub with non-blocking broadcast handles stalled clients gracefully, with a broadcast coalescer (≤4 `match_updated`/s per match) and per-IP rate limiting bounding fan-out cost.
- **Consumer-boundary interfaces** in `mobileapp/deps.go` keep handler tests light.

### Areas to Watch

- **`mobileapp/` is now the largest package** after the new handler families landed (decision, eligibility, lineup, daihyosen, Swiss, display, league tie-breaker, registration, announcement, branding, sponsors, print). It now carries supporting infra too (rate limiting, broadcast coalescer, viewer single-flight). Further grouping into subpackages may be warranted.
- **`engine/` has grown rapidly** — spanning scoring, eligibility, kachinuki, daihyosen, Swiss, scheduling, league tie-breaks, participant replacement, and PDF export. Further sub-splitting may be warranted.
- **`helper/` mixes concerns**: tree algorithms, CSV parsing, Excel rendering, seeding, and utilities. The `helper/{bracket,csv,seeding}/` subpackages signal an ongoing extraction (the earlier `pool/` subpackage was folded back to `pool_partial.go`); `helper/` proper has not shrunk yet.
- **`excel/` has minimal direct test coverage** despite Excel output being the primary CLI deliverable. Most Excel test coverage lives in `helper/*_test.go` instead.
- **`domain/` is still partially adopted**: most business logic in `helper/` uses `helper.Player` directly rather than domain types — the migration is incomplete.
- **No Go interfaces** for `state.Store` or `engine.Engine` at the top level — interface adoption is happening incrementally via `mobileapp/deps.go`, but engine-to-state and helper-to-engine calls still go through concrete types.
