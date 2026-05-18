# Architecture

This document describes the high-level architecture of the bracket-creator application — a Go CLI and web application for generating kendo tournament brackets.

## System Modes

The application runs in four distinct modes from a single binary:

```
bracket-creator
├── create-pools       CLI: CSV → Excel (pool + playoff format)
├── create-playoffs    CLI: CSV → Excel (direct elimination)
├── serve              Web: stateless form-based bracket generator
└── mobile-app         Web: live tournament management with real-time updates
```

**CLI mode** reads a CSV participant list, generates bracket structures in memory, and writes an Excel workbook with formula-linked cells for bracket visualization.

**Serve mode** is a full-featured web UI for Excel bracket generation. The SPA (`web/index.html`) provides tournament type selection (pools+playoffs or direct elimination), court configuration, CSV participant input with drag-and-drop and validation, a seeding modal, time estimation, and dark/light theming. Form values auto-save to localStorage. On submit, the backend generates the Excel workbook and returns it as a download. No server-side persistence.

**Mobile-app mode** is a full tournament management platform: CRUD for tournaments/competitions, live match scoring, real-time push via SSE, and file-backed persistent state. The frontend is a Preact SPA embedded in the binary.

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
- **Business logic:** kendo tournament rules — pool generation, seeding, bracket/tree construction, scoring, dojo-conflict resolution.
- **Persistence:** file-backed state store for live tournaments; Excel workbook construction.
- **Domain models:** core entities (Player, Pool, Match, Tournament, Seed) decoupled from presentation and I/O.

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
│   └── man.go                   Man-page generation
│
├── internal/
│   ├── domain/                  Pure domain models (no dependencies)
│   │   └── Player, MatchWinner, SeedAssignment, Pool, Tournament, Match
│   │
│   ├── helper/                  Core algorithms and Excel rendering
│   │   ├── tree.go              Binary bracket tree (Node, recursive subdivision)
│   │   ├── seed.go              Seeding algorithms (StandardSeeding, PoolSeeding, ApplySeeds)
│   │   ├── tournament.go        Pool creation (greedy, dojo-conflict avoidance)
│   │   ├── csv.go               CSV parsing and validation
│   │   ├── excel.go             Excel workbook construction
│   │   ├── excel_data.go        Data sheet population
│   │   ├── excel_styles.go      Cell styling and formatting
│   │   ├── excel_tree.go        Tree sheet rendering
│   │   ├── excel_tags.go        Tag sheet rendering
│   │   ├── constants.go         Layout constants, sheet names, defaults
│   │   ├── numbers.go           Number prefix assignment
│   │   ├── uuid.go              UUID utilities
│   │   └── helper.go            Misc utilities
│   │
│   ├── excel/                   Excel file lifecycle
│   │   ├── client.go            File open/save/close (Client)
│   │   ├── error.go             Error handling utilities
│   │   └── template.go          NewFileFromScratch — builds entire workbook
│   │
│   ├── engine/                  Business logic for live tournaments
│   │   ├── engine.go            Engine struct (wraps state.Store)
│   │   ├── scoring.go           Match result recording and tie-breaking
│   │   ├── pools.go             Pool round-robin generation and completion
│   │   ├── bracket.go           Bracket generation and advancement
│   │   ├── ranking.go           Player standings and ranking
│   │   ├── competition.go       Competition lifecycle (start, invalidate, complete)
│   │   ├── schedule.go          Match scheduling across courts
│   │   ├── export.go            Excel export from live state
│   │   └── errors.go            Typed error definitions
│   │
│   ├── state/                   File-backed persistence with caching
│   │   ├── store.go             Store struct, per-competition locking, mtime cache
│   │   ├── models.go            Tournament, Competition, MatchResult, Bracket, etc.
│   │   ├── tournament.go        Tournament YAML read/write
│   │   ├── competition.go       Competition YAML read/write
│   │   ├── participants.go      CSV participant parsing (Go side)
│   │   ├── pools.go             Pool assignment persistence
│   │   ├── bracket.go           Bracket JSON read/write
│   │   ├── schedule.go          Schedule JSON read/write
│   │   ├── seeds.go             Seed assignment persistence
│   │   ├── reservedslots.go     Cross-competition reserved slot resolution
│   │   ├── overrides.go         Manual ranking overrides
│   │   └── ids.go               ID generation utilities
│   │
│   ├── mobileapp/               HTTP handlers and real-time events
│   │   ├── server.go            Gin router setup, CORS, SPA fallback
│   │   ├── middleware.go        Auth middleware (X-Tournament-Password)
│   │   ├── hub.go               SSE pub/sub hub (channel-based, non-blocking)
│   │   ├── handlers_tournament.go
│   │   ├── handlers_competition.go
│   │   ├── handlers_match.go
│   │   ├── handlers_participants.go
│   │   ├── handlers_import.go
│   │   └── handlers_viewer.go   Public (unauthenticated) endpoints
│   │
│   ├── resources/               Embedded filesystem abstraction
│   ├── service/                 Service layer (thin wrapper over helper)
│   └── test/                    Shared test helpers and factories
│
├── web/                         Web UI for Excel bracket generation (embedded, served by `serve` command)
│   └── index.html               Full SPA: tournament config, CSV input, seeding, time estimator, Excel download
│
├── web-mobile/                  Preact SPA (embedded, served by `mobile-app` command)
│   ├── index.html               SPA shell
│   ├── js/
│   │   ├── app.jsx              Root component, routing, SSE listener, auth
│   │   ├── api.jsx              Backend API client + JSON serialization
│   │   ├── data.jsx             State management hooks, data normalization
│   │   ├── ui.jsx               Shared UI components
│   │   ├── bracket.jsx          Bracket/tree rendering
│   │   ├── viewer.jsx           Public viewer (no auth required)
│   │   ├── admin.jsx            Admin panel container
│   │   ├── admin_shell.jsx      Admin layout wrapper
│   │   ├── admin_setup.jsx     Tournament configuration
│   │   ├── admin_competition.jsx  Competition management
│   │   ├── admin_participants.jsx Participant CRUD
│   │   ├── admin_pools.jsx      Pool administration
│   │   ├── admin_schedule.jsx   Schedule management + score editor
│   │   ├── admin_scoring_modal.jsx  Match score entry modal
│   │   ├── admin_helpers.jsx    Admin utility functions
│   │   └── __tests__/           15 test files (Vitest)
│   ├── css/styles.css
│   └── dist/                    esbuild output (pre-compiled JSX)
│
└── specs/                       OpenAPI spec, feature specs
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
       ──→ state  (persistence)

mobileapp ──→ engine     (business logic)
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
3. Input is handed off to the helper layer. Player objects are instantiated.
4. `PoolSeeding` distributes top players to prevent early clashes. Remaining players are assigned to pools, strictly respecting dojo-conflict avoidance rules.
5. Winners from the generated pools are mapped to a binary tree representing the knockout stage.
6. The `excel` layer creates a workbook in memory: sheets for Pool Draws, Pool Matches, and Elimination brackets, with formulas linking pool winners to the playoff tree.
7. The complete Excel file is streamed back to the client as a binary download.

### Mobile-App Mode (live tournament)

```
Admin Client ──→ PUT /api/tournament
                    │
                    ▼
               state.Store.SaveTournament() ──→ tournament.yaml

Admin Client ──→ POST /api/competitions/:id/start
                    │
                    ▼
               engine.StartCompetition()
                    ├──→ state: load participants, seeds
                    ├──→ helper: generate pools/bracket
                    └──→ state: save pools.yaml, bracket.json, schedule.json
                    │
                    ▼
               hub.Broadcast(EventCompetitionStarted)
                    │
                    ▼
               SSE push ──→ All connected clients

Admin Client ──→ POST /api/competitions/:id/matches/bulk-score
                    │
                    ▼
               engine.RecordMatchResult()
                    ├──→ validate + apply scores
                    ├──→ state: save pool-matches.json or bracket.json
                    └──→ engine.MaybeAutoCompletePools() (check phase transition)
                    │
                    ▼
               hub.Broadcast(EventMatchUpdated)
                    │
                    ▼
               SSE push ──→ Viewer clients update in real-time
```

### State on Disk

```
tournament-data/
├── tournament.yaml                  Tournament name, date, venue, courts, password
└── competitions/
    └── {compID}/
        ├── config.yaml              Competition settings (format, pool size, courts, etc.)
        ├── participants.csv         One player per line (optional UUID prefix)
        ├── seeds.csv                Seed rank → player name mapping
        ├── pools.yaml               Pool assignments after start
        ├── pool-matches.json        Pool phase match results
        ├── bracket.json             Elimination bracket structure and results
        └── schedule.json            Court/time assignments for all matches
```

## Concurrency Model

The `state.Store` uses a two-level locking scheme:

1. **Per-competition `sync.RWMutex`** (via `sync.Map`) — isolates competitions from each other. A score update to competition A never blocks reads of competition B.
2. **Per-file `fileCache`** with its own `sync.RWMutex` — within a competition, different files (pools vs bracket vs schedule) can be read concurrently.

Cache invalidation uses **file mtime**: on each read, the cached mtime is compared against `os.Stat`. A mismatch triggers a re-parse under the write lock. This handles external file edits gracefully.

The `engine.Engine` maintains a separate `standingsCache` (sync.Map) with a `sync.Once`-based flight deduplication to prevent thundering-herd re-computation of standings.

The SSE `Hub` uses a non-blocking send pattern: if a client's 100-message buffer is full, it is unsubscribed immediately rather than blocking the broadcaster.

## Frontend Architecture

**Stack**: Preact (lightweight React alternative), JSX compiled by esbuild, Vitest for tests.

**Routing**: Manual SPA routing via `window.location.pathname` parsing and `history.pushState`. No router library.

**State**: Preact `useState`/`useEffect` hooks in `app.jsx`. No external state library.

**Real-time**: `EventSource` on `/api/events` receives SSE messages. Match updates are merged into local state via `patchCompetitionData()` in `data.jsx`.

**Auth**: Admin mode requires `X-Tournament-Password` header, stored in `localStorage`.

**Component tree**:

```
App (app.jsx)
├── Viewer mode (public)
│   ├── TournamentHome
│   ├── CompetitionViewer (pools, bracket, results)
│   └── ScheduleView
│
└── Admin mode (password-protected)
    └── AdminShell
        ├── AdminDashboard
        ├── AdminTournament
        ├── AdminCompetition (overview, participants, schedule, scoring, seeding)
        ├── AdminPools
        ├── AdminSchedule (score editor with chained court-scoped navigation)
        └── ImportTournament
```

## Key Algorithms

**Binary bracket tree** (`helper/tree.go`): Recursive subdivision into `Node` structs with `Left`/`Right` children. Max 16 players per tree page. Multi-page output splits the tree and links pages via cell references.

**Seeding** (`helper/seed.go`): `StandardSeeding()` uses `generateBracketOrder()` for placement. `PoolSeeding()` interleaves seeds across courts. `ApplySeeds()` handles collisions by swapping.

**Pool creation** (`helper/tournament.go`): Greedy algorithm with dojo-conflict avoidance. Pools distributed contiguously across courts.

**Tie-breaking**: Multi-criteria cascade — wins → losses → draws → points scored → points lost (individual). Team tournaments add team-level criteria before individual criteria. See CLAUDE.md for the full precedence.

## Design Patterns & Principles

- **Command Pattern** — Cobra encapsulates execution logic for each CLI subcommand.
- **Dependency Injection** — Embedded resources are exposed through an `fs.FS` interface, so production runs use `embed.FS` and tests can swap in `fstest.MapFS`. Consumer-boundary interfaces for the mobile app live in `internal/mobileapp/deps.go`.
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
|---------|-----------|----------|-------|
| helper | 4,159 | 8,674 | 2.1x |
| mobileapp | 2,678 | 4,536 | 1.7x |
| state | 2,175 | 1,361 | 0.6x |
| engine | 1,555 | 2,806 | 1.8x |
| cmd | 1,205 | 1,955 | 1.6x |
| excel | 391 | 84 | 0.2x |
| domain | 152 | 330 | 2.2x |
| **Go total** | **~12,500** | **~20,000** | **1.6x** |
| **Frontend (JSX)** | **8,213** | **3,455** | **0.4x** |

## Known Architectural Observations

### Strengths

- **Clean layering**: presentation → engine → state → filesystem with no circular dependencies.
- **Strong algorithmic test coverage**: helper (2.1x), engine (1.8x), and mobileapp (1.7x) have high test ratios. The mobile app handler tests more than doubled during recent review rounds.
- **Single-binary deployment**: all assets embedded at compile time.
- **Fine-grained concurrency**: per-competition + per-file locking avoids global contention.
- **Real-time push**: SSE hub with non-blocking broadcast handles stalled clients gracefully.

### Areas to Watch

- **`helper/` is the largest package** (4.2K LOC) mixing tree algorithms, CSV parsing, Excel rendering, seeding, and utility functions. Cohesion is lower than other packages.
- **No Go interfaces** for `state.Store` or `engine.Engine` — all consumers depend on concrete structs, which makes isolated unit testing harder.
- **`excel/` has minimal test coverage** (0.2x ratio) despite Excel output being the primary CLI deliverable.
- **Frontend has no router library** — SPA routing is manual string parsing in `app.jsx`, which requires care when adding new routes.
- **`api.jsx` mixes concerns** — HTTP client, JSON serialization/normalization, and state patching live in one file.
- **`domain/` is small and partially adopted** — most business logic still uses `helper.Player` directly rather than domain types.
