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

Hidden helper commands: `version`, `man` (man-page generation).

**CLI mode** reads a CSV participant list, generates bracket structures in memory, and writes an Excel workbook with formula-linked cells for bracket visualization.

**Serve mode** is a full-featured web UI for Excel bracket generation. The SPA (`web/index.html`) provides tournament type selection (pools+playoffs or direct elimination), court configuration, CSV participant input with drag-and-drop and validation, a seeding modal, time estimation, and dark/light theming. Form values auto-save to localStorage. On submit, the backend generates the Excel workbook and returns it as a download. No server-side persistence.

**Mobile-app mode** is a full tournament management platform: CRUD for tournaments/competitions, live match scoring, decision recording (kiken/fusenpai/daihyosen), team lineups, Swiss/Kachinuki formats, real-time push via SSE, and file-backed persistent state. The frontend is a Preact SPA embedded in the binary.

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

```
main.go                          Entry point; embeds web/ and web-mobile/ via //go:embed
│
├── cmd/                         Cobra CLI commands
│   ├── root.go                  Root command, flag registration
│   ├── create-pools.go          Pool + playoff bracket generation
│   ├── create-playoffs.go       Direct elimination bracket generation
│   ├── shared.go                Shared CLI helpers (processEntries, openOutputFile, etc.)
│   ├── serve.go                 Stateless web server (form → Excel)
│   ├── mobile_app.go            Live tournament server startup
│   ├── version.go               Version display
│   └── man.go                   Man-page generation (hidden)
│
├── internal/
│   ├── domain/                  Pure domain models (no internal dependencies)
│   │   ├── player.go            Player, MatchWinner
│   │   ├── seed.go              SeedAssignment
│   │   ├── pool.go, match.go, tournament.go
│   │   ├── decision.go          Decision wire values + UnmarshalYAML migration
│   │   ├── competitor_status.go Eligibility record
│   │   ├── team_lineup.go       Team-match lineup (Senpo..Taisho)
│   │   └── glossary.go          Kendo-term glossary used by the UI
│   │
│   ├── helper/                  Core algorithms and Excel rendering (largest package)
│   │   ├── tree.go              Binary bracket tree (Node, recursive subdivision)
│   │   ├── seed.go              Seeding algorithms (StandardSeeding, PoolSeeding, ApplySeeds)
│   │   ├── tournament.go        Pool creation (greedy, dojo-conflict avoidance)
│   │   ├── csv.go               CSV parsing and validation
│   │   ├── excel.go, excel_*.go Excel rendering (data, styles, tree, tags, kachinuki)
│   │   ├── constants.go         Layout constants, sheet names, defaults
│   │   ├── numbers.go, uuid.go, helper.go
│   │   └── bracket/, csv/, pool/, seeding/   Small focused subpackages
│   │
│   ├── excel/                   Excel file lifecycle
│   │   ├── client.go            File open/save/close (Client)
│   │   ├── error.go             Error handling utilities
│   │   └── template.go          NewFileFromScratch — builds entire workbook
│   │
│   ├── engine/                  Business logic for live tournaments (15 files)
│   │   ├── engine.go            Engine struct (wraps state.Store)
│   │   ├── scoring.go, scoring_tx.go   Match result recording (tx-aware variants)
│   │   ├── pools.go, bracket.go        Pool round-robin + bracket advancement
│   │   ├── ranking.go                  Standings, tie-breaking
│   │   ├── competition.go              Lifecycle (start, invalidate, complete)
│   │   ├── schedule.go, scheduler_slots.go   Court scheduling + estimator
│   │   ├── eligibility.go              StartMatch gate (kiken/fusenpai)
│   │   ├── kachinuki.go, kachinuki_export.go  Winner-stays-on bouts
│   │   ├── daihyosen.go                Rep-bout flow
│   │   ├── swiss.go                    Swiss-round format
│   │   ├── export.go                   Excel export from live state
│   │   └── errors.go                   Typed error definitions
│   │
│   ├── state/                   File-backed persistence with caching + WAL
│   │   ├── store.go             Store struct, per-competition locking, mtime cache
│   │   ├── transactions.go      WithTransaction / StoreTx (multi-file atomic commits)
│   │   ├── atomic_write.go      Single-file durable write (tmp → fsync → rename)
│   │   ├── wal/wal.go           Write-ahead log for multi-file transactions
│   │   ├── models.go            Tournament, Competition, MatchResult, Bracket, etc.
│   │   ├── tournament.go, competition.go   YAML-frontmatter Markdown read/write
│   │   ├── participants.go, seeds.go, pools.go, bracket.go, schedule.go
│   │   ├── competitor_status.go Eligibility persistence
│   │   ├── team_lineup.go       Team lineup persistence
│   │   ├── reservedslots.go     Cross-competition reserved slot resolution
│   │   ├── overrides.go         Manual ranking overrides
│   │   ├── match.go             DeriveQueuePositions (pure helper)
│   │   └── ids.go               ID generation utilities
│   │
│   ├── mobileapp/               HTTP handlers and real-time events
│   │   ├── server.go            Gin router setup, CORS, SPA fallback
│   │   ├── middleware.go        Auth middleware (X-Tournament-Password)
│   │   ├── hub.go               SSE pub/sub hub (channel-based, non-blocking)
│   │   ├── deps.go              Consumer-boundary interfaces (NFR-002)
│   │   ├── validation.go        Shared request validation helpers
│   │   ├── handlers_tournament.go, handlers_competition.go, handlers_participants.go
│   │   ├── handlers_match.go, handlers_decision.go, handlers_daihyosen.go
│   │   ├── handlers_eligibility.go, handlers_lineup.go, handlers_swiss.go
│   │   ├── handlers_schedule.go Stateless schedule estimator (also wired into `serve`)
│   │   ├── handlers_import.go   Tournament import
│   │   ├── handlers_display.go  Public TV/lobby/overlay surfaces
│   │   └── handlers_viewer.go   Public (unauthenticated) endpoints
│   │
│   ├── resources/               Embedded filesystem abstraction
│   ├── service/                 Thin service layer (tournament_service.go)
│   ├── cmd/version/             Build/version metadata helpers
│   └── test/                    Shared test helpers and factories
│
├── web/                         Web UI for Excel bracket generation
│   └── index.html               Full SPA: tournament config, CSV input, seeding, estimator
│
├── web-mobile/                  Preact SPA (embedded, served by `mobile-app`)
│   ├── index.html               SPA shell
│   ├── js/
│   │   ├── app.jsx              Root component, SSE listener, auth, view state
│   │   ├── router.jsx           preact-router wrapper, useQuery()
│   │   ├── api_client.jsx       HTTP client (auth, retry, error mapping)
│   │   ├── api_serializers.jsx  JSON ↔ camelCase normalization
│   │   ├── data.jsx             State hooks, data normalization
│   │   ├── patch.jsx            Patch-merge for SSE updates
│   │   ├── ui.jsx, bracket.jsx, glossary.jsx, glossary_data.js
│   │   ├── viewer.jsx, display.jsx          Public surfaces
│   │   ├── admin.jsx, admin_shell.jsx       Admin container + chrome
│   │   ├── admin_setup.jsx, admin_competition.jsx, admin_participants.jsx
│   │   ├── admin_pools.jsx, admin_schedule.jsx, admin_scoring_modal.jsx
│   │   ├── admin_lineup.jsx, admin_helpers.jsx
│   │   └── __tests__/           22 Vitest spec files
│   ├── css/styles.css
│   └── dist/                    esbuild output (pre-compiled JSX)
│
└── specs/                       OpenAPI spec, feature specs, this document
```

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
        ├── reserved-slots.json         Cross-competition reserved slot bindings
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
| `match_updated` | score / decision / status change |
| `competition_started` | `POST /competitions/:id/start` |
| `competition_completed` | final match resolved |
| `tournament_updated` | tournament-level CRUD |
| `schedule_updated` | court/time edits, schedule regenerated |
| `competitor_status_updated` | eligibility change (new kiken/fusenpai) |

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
│   ├── CompetitionViewer (pools, bracket, results)
│   └── ScheduleView
│
├── Display mode (/display — TV/lobby/overlay, public, query-param driven)
│
└── Admin mode (password-protected)
    └── AdminShell
        ├── AdminDashboard
        ├── AdminTournament
        ├── AdminCompetition (overview, participants, schedule, scoring, seeding)
        ├── AdminPools
        ├── AdminSchedule (score editor with chained court-scoped navigation)
        ├── AdminLineup (team lineup composer)
        └── ImportTournament
```

## Key Algorithms

**Binary bracket tree** (`helper/tree.go`): Recursive subdivision into `Node` structs with `Left`/`Right` children. Max 16 players per tree page. Multi-page output splits the tree and links pages via cell references.

**Seeding** (`helper/seed.go`): `StandardSeeding()` uses `generateBracketOrder()` for placement. `PoolSeeding()` interleaves seeds across courts so `ReorderPoolsForCourts` lands top seeds on different courts and opposite ends. `ApplySeeds()` handles collisions by swapping.

**Pool creation** (`helper/tournament.go`): Greedy algorithm with dojo-conflict avoidance. Pools distributed contiguously across courts.

**Tie-breaking**: Multi-criteria cascade — wins → losses → draws → points scored → points lost (individual). Team tournaments add team-level criteria before individual criteria. See CLAUDE.md for the full precedence.

**Decision types** (`internal/domain/decision.go`): 8 canonical wire values — `""`, `fought`, `hikiwake`, `kiken`, `fusenpai`, `fusensho`, `daihyosen`, `kachinuki-exhaustion`. Legacy YAML `decision: true` migrates to `hikiwake`, `false` to `fought` (Decision.UnmarshalYAML).

**Competitor eligibility** (`engine/eligibility.go`, `state/competitor_status.go`): a kiken/fusenpai decision auto-writes `CompetitorStatus{Eligible: false}` for the loser. `engine.StartMatchTx` is the FR-035 pre-flight gate — returns `*IneligibleCompetitorError` (matches `errors.Is(err, ErrIneligibleCompetitor)`), mapped to HTTP 409 by the score handler. Re-scoring a match that itself created the ineligibility is permitted (undo path).

**Team lineups & kachinuki** (`domain/team_lineup.go`, `engine/kachinuki.go`): TeamLineup pins position → player for a round. FIK 5-person rule: Senpo + Taisho mandatory; 1 vacancy must be Jiho, 2 must be Jiho+Fukusho, 3+ disqualifies. Kachinuki ("winner-stays-on") dynamically appends bouts via `engine.AdvanceKachinuki` until one team is exhausted (`DecisionKachinukiExhaustion`).

**Schedule estimator** (`engine/schedule.go`): `EstimateSchedule(EstimateInput) ScheduleEstimate` returns total/per-court minutes from match duration × multiplier × slowest-court buffer. Exposed via stateless `GET /api/schedule/estimate` on both the CLI web server (`serve`) and the mobile app.

**Swiss format** (`engine/swiss.go`): pairings + round advancement for Swiss-style competitions.

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

| Package | Source LOC | Test LOC | Ratio |
|---|---|---|---|
| engine | 4,942 | 7,360 | 1.5× |
| helper | 4,582 | 9,245 | 2.0× |
| state | 4,192 | 6,336 | 1.5× |
| mobileapp | 5,333 | 8,205 | 1.5× |
| cmd | 1,216 | 2,055 | 1.7× |
| domain | 860 | 913 | 1.1× |
| excel | 391 | 135 | 0.3× |
| service / resources / test | 120 | 157 | — |
| **Go total** | **~21,600** | **~34,400** | **1.6×** |
| **Frontend (JSX/JS)** | **12,970** | **6,047** | **0.5×** |

## Known Architectural Observations

### Strengths

- **Clean layering**: presentation → engine → state → filesystem with no circular dependencies.
- **Strong algorithmic test coverage**: helper (2.0×), engine (1.5×), state (1.5×), and mobileapp (1.5×) all carry substantial test bodies.
- **Single-binary deployment**: all assets embedded at compile time.
- **Fine-grained concurrency**: per-competition + per-file locking avoids global contention; non-reentrant mutex caught misuse via `StoreTx`.
- **Multi-file atomicity**: WAL-backed `Store.WithTransaction` lets the score handler commit several files (match result + eligibility + lineup) atomically across a crash.
- **Real-time push**: SSE hub with non-blocking broadcast handles stalled clients gracefully.
- **Consumer-boundary interfaces** in `mobileapp/deps.go` keep handler tests light.

### Areas to Watch

- **`helper/` is the largest package** (4.6K LOC) mixing tree algorithms, CSV parsing, Excel rendering, seeding, and utility functions. The newer `helper/{bracket,csv,pool,seeding}/` subpackages signal an ongoing extraction; `helper/` proper has not shrunk yet.
- **`engine/` has grown rapidly** — 15 files, ~5K LOC — and now spans scoring, eligibility, kachinuki, daihyosen, Swiss, and scheduling. Further sub-splitting may be warranted.
- **`mobileapp/` is approaching `helper/` in size** (5.3K LOC) due to the new handler families (decision, eligibility, lineup, daihyosen, Swiss, display).
- **`excel/` has minimal test coverage** (0.3× ratio) despite Excel output being the primary CLI deliverable. Most Excel test coverage lives in `helper/*_test.go` instead.
- **`schedule_updated` SSE event is broadcast but the frontend does not consume it** — admin schedule view doesn't auto-refresh after court/time moves (see memory `project_schedule_updated_gap`).
- **`domain/` is now substantially larger** (860 LOC, 8 files) but most business logic in `helper/` still uses `helper.Player` directly rather than domain types — the migration is partial.
- **No Go interfaces** for `state.Store` or `engine.Engine` at the top level — interface adoption is happening incrementally via `mobileapp/deps.go`, but engine-to-state and helper-to-engine calls still go through concrete types.
