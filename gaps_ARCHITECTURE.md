# Architecture Recommendations

Observations from the current codebase with concrete suggestions for improvement. Ordered by impact.

## 1. Split the `helper/` package

**Problem**: `helper/` is ~4,160 LOC — the largest package by far — mixing tree algorithms, CSV parsing, Excel rendering, seeding logic, and general utilities. This hurts discoverability and makes it hard to reason about what depends on what.

**Current contents**:

| File | LOC | Responsibility |
|------|-----|----------------|
| excel.go, excel_data.go, excel_styles.go, excel_tree.go, excel_tags.go | ~2,495 | Excel sheet rendering |
| tournament.go | ~435 | Pool creation |
| seed.go | ~414 | Seeding algorithms |
| tree.go | ~324 | Binary bracket tree algorithms |
| helper.go, numbers.go, uuid.go, constants.go | ~397 | Utilities |
| csv.go | ~94 | CSV parsing and validation |

**Suggestion**: Extract into focused sub-packages:

```
internal/
├── helper/           (shrinks to utilities, constants, numbers, uuid)
├── bracket/          (tree.go — Node, subdivision, tree building)
├── seeding/          (seed.go — StandardSeeding, PoolSeeding, ApplySeeds)
├── pool/             (tournament.go — pool creation, dojo-conflict avoidance)
├── csv/              (csv.go — parsing, validation, duplicate detection)
└── excel/            (absorbs helper/excel*.go alongside existing client/sheet/template)
```

**Note**: `internal/engine/` already exists as a thin adapter that drives `helper` pool/bracket generation from a `state.Competition`. The split plan should clarify `engine/`'s role — it should compose the new sub-packages rather than continuing to call `helper` directly, otherwise it becomes a passthrough that adds indirection without value.

**Risk**: This is a large refactor touching many imports across `cmd/`, `engine/`, and `mobileapp/`. Best done in one PR with no functional changes, validated by `make go/test`.

---

## 2. Introduce interfaces for `state.Store` and `engine.Engine`

**Problem**: All consumers depend on concrete `*state.Store` and `*engine.Engine` structs. This makes unit testing handlers and engine logic harder — every test needs a real filesystem store, even when only one method is being exercised.

**Current wiring**:
```go
// mobileapp/server.go
func NewRouter(store *state.Store, eng *engine.Engine, res *resources.Resources) *gin.Engine

// engine/engine.go
type Engine struct {
    store *state.Store
}
```

**Suggestion**: Define interfaces at the consumer boundary (not in `state/` or `engine/`):

```go
// mobileapp/deps.go
type CompetitionStore interface {
    LoadCompetition(id string) (*state.Competition, error)
    SaveCompetition(comp *state.Competition) error
    LoadPoolMatches(compID string) (map[string][]state.MatchResult, error)
    // ... only the methods this package actually calls
}

type ScoringEngine interface {
    RecordMatchResult(compID string, results []state.MatchResult) error
    GetStandings(compID string) (map[string][]state.PlayerStanding, error)
}
```

Then handler tests can use lightweight stubs instead of spinning up real file-backed stores. Apply the same pattern in `engine/` for its dependency on `state.Store`.

The same approach applies to the SSE hub (`hub.go`) — handlers currently depend on a concrete `*Hub` struct. Extracting a `Broadcaster` interface would allow testing event-driven behavior without a real SSE connection.

**Risk**: Low. Interfaces can be introduced incrementally per-handler without changing any runtime behavior.

---

## 3. Increase `excel/` test coverage

**Problem**: The `excel/` package has a 0.2x test-to-code ratio (84 test LOC for 391 source LOC). This package manages workbook lifecycle and sheet operations — bugs here produce corrupt or incorrect Excel files, which are the primary CLI output.

More importantly, the bulk of Excel rendering lives in `helper/excel*.go` (~2,000 LOC) with test coverage inherited from integration-level tests in `cmd/`, not targeted unit tests.

**Suggestion**:
- Add unit tests for `excel.NewFileFromScratch()` that verify sheet names, column widths, and style IDs on a generated workbook.
- Add snapshot-style tests for `helper/excel*.go` functions: generate a workbook from known input, then assert specific cell values and formulas. The `excelize` library supports reading cells back, so no golden-file diffing is needed.
- Prioritize pool match layout and tree sheet rendering — these have the most complex coordinate arithmetic.

**Risk**: None. Pure test additions.

---

## 4. Add a validation layer at the API boundary

**Problem**: Input validation is scattered between HTTP handlers and engine methods. Invalid inputs (e.g., bad competition IDs, out-of-range pool sizes, malformed match results) sometimes propagate deep into the engine before being caught, producing internal error messages that leak implementation details.

**Example**: A `POST /api/competitions/:id/matches/bulk-score` with a malformed match ID reaches `engine.RecordMatchResult()` before being rejected, when it could be caught at the handler.

**Suggestion**: Add a thin validation step in each handler before calling into the engine:

```go
func handleBulkScore(c *gin.Context) {
    var req BulkScoreRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(400, gin.H{"error": "invalid request body"})
        return
    }
    if err := req.Validate(); err != nil {  // <-- new
        c.JSON(400, gin.H{"error": err.Error()})
        return
    }
    // ... call engine
}
```

Use struct methods (e.g., `BulkScoreRequest.Validate()`) rather than a validation framework — keeps it simple and grep-able.

**Risk**: Low. Each handler can be updated independently.

---

## 5. Adopt a lightweight frontend router

**Problem**: SPA routing in `app.jsx` is manual `window.location.pathname` parsing with hand-written URL construction. Adding a new route requires updating both `getRouteFromUrl()` and `getUrlFromRoute()` — easy to get out of sync, and there's no type safety on route parameters.

**Suggestion**: Adopt `preact-router` (3 KB gzipped, designed for Preact):

```jsx
import Router from 'preact-router';

function App() {
    return (
        <Router>
            <AdminDashboard path="/admin" />
            <AdminCompetition path="/admin/competition/:id" />
            <CompetitionViewer path="/competition/:id" />
            <TournamentHome default />
        </Router>
    );
}
```

This eliminates `getRouteFromUrl`, `getUrlFromRoute`, and the manual `history.pushState` calls. Route parameters (`:id`) are passed as props automatically.

**Trade-off**: The app currently has zero external dependencies beyond Preact+HTM. Adding `preact-router` would be the first external dep, with implications for embedded binary size and build pipeline. This trade-off becomes more worthwhile if display routes (Gap 3–5 in `gaps_tournament_spec.md`) are implemented — those add several new routes that would benefit most from declarative routing.

**Risk**: Medium. Touches the core of `app.jsx` and every navigation call. Best done as a dedicated PR with manual browser testing of all routes (admin dashboard, competition views, viewer, deep links).

---

## 6. Separate concerns in `api.jsx`

**Problem**: `api.jsx` (479 LOC) handles two distinct responsibilities:
1. **HTTP client** — fetch calls with auth headers, error handling, SSE subscription
2. **Serialization** — `normalizeMatch()`, `normalizePlayer()`, `toBackendMatchResult()`, `normalizeCompetitionDetail()`, legacy spelling fixups

Additionally, `patchCompetitionData()` — which merges SSE event data into local state — lives in `app.jsx` and is duplicated in `admin.jsx`, rather than being centralized.

Note: `data.jsx` (356 LOC) is currently a sample data generator (mock players, pools, brackets for testing), not a state management module.

**Suggestion**: Split `api.jsx` into two files and centralize the patch logic:

```
web-mobile/js/
├── api_client.jsx        HTTP methods, auth headers, error handling, SSE subscription
├── api_serializers.jsx   normalizeMatch, normalizePlayer, toBackendMatchResult, legacy fixups
├── patch.jsx             patchCompetitionData (extracted from app.jsx/admin.jsx, single source of truth)
└── data.jsx              (unchanged — sample data generators for testing)
```

**Risk**: Low. Pure file reorganization with no behavior change. Deduplicating `patchCompetitionData` removes a maintenance hazard.

---

## 7. Complete the `domain/` package adoption

**Problem**: The `domain/` package (152 LOC) defines clean models (`Player`, `SeedAssignment`, `Pool`, etc.) but most business logic still operates on `helper.Player` directly. The two `Player` types coexist — `helper.Player` has Excel coordinate fields (`sheetName`, `cell`) tightly coupled to output, while `domain.Player` is a pure data model.

This dual-model situation is documented as "in transition" but creates confusion about which type to use where, and prevents the engine from being output-format-agnostic.

**Suggestion**: Continue the gradual migration with a clear boundary:
- `domain.Player` for all business logic (engine, state, mobileapp).
- `helper.Player` only inside `helper/` and `excel/` for rendering.
- Add conversion functions at the boundary: `helper.PlayerFromDomain(domain.Player)` and `domain.PlayerFromHelper(helper.Player)`.

Don't attempt a big-bang migration — keep it incremental, one package at a time, starting with `engine/` since it already imports `domain`.

**Risk**: Medium. Each migration step changes function signatures. Table-driven tests make verification straightforward.

---

## 8. Add a frontend render error boundary

**Problem**: There is no global error boundary in the Preact app. An unhandled exception during rendering (e.g., a null dereference in bracket rendering when data is mid-load) crashes the entire SPA with a white screen.

Note: SSE disconnect handling already exists — `subscribeToEvents()` in `api.jsx` has `onerror` retry logic that reconnects automatically. What's missing is only the **render-level** error boundary.

**Suggestion**: Add a Preact error boundary at the `App` level:

```jsx
class ErrorBoundary extends Component {
    state = { error: null };
    static getDerivedStateFromError(error) {
        return { error };
    }
    render() {
        if (this.state.error) {
            return <div class="error-banner">Something went wrong. <a href="/">Reload</a></div>;
        }
        return this.props.children;
    }
}
```

**Risk**: Low. Additive change, no impact on happy path.

---

## 9. Modularize the `web/index.html` serve UI

**Problem**: The Excel bracket generation web UI (`web/index.html`) is a 92KB monolithic file with all JavaScript, CSS, and HTML inline (~2,000 lines). It includes tournament configuration, CSV input with drag-and-drop, participant validation, a seeding modal, a time estimator, dark/light theming, and localStorage auto-save — all in a single file with no modularity.

This is a fully featured SPA that handles the core use case of the application (Excel bracket generation), but:
- No JavaScript tests exist for form behavior, validation logic, or UI state.
- Validation logic, API calls, and DOM manipulation are interleaved.
- Adding features (e.g., new tournament options) requires editing a large, undifferentiated file.

**Suggestion**: Extract the inline JavaScript into separate modules, mirroring the `web-mobile/` approach:

```
web/
├── index.html              Shell HTML + Bootstrap imports
├── js/
│   ├── app.js              Form initialization, event binding, localStorage
│   ├── validation.js       CSV parsing, duplicate detection, line-level errors
│   ├── seeding.js          Seed modal logic
│   ├── time_estimator.js   Duration calculation
│   └── api.js              POST /create, parse-participants, download polling
└── css/
    └── styles.css          Custom styles (if any beyond Bootstrap)
```

Add Vitest tests for the validation and time estimator logic — these are pure functions that can be tested without DOM mocking.

**Risk**: Medium. Requires updating the `//go:embed web/*` directive to include subdirectories, and verifying the embedded file server still resolves paths correctly. No functional change.

---

## 10. Harden `state.Store` concurrency safety

**Problem**: The file-backed `state.Store` uses per-file reads and writes with no atomic transactions. Concurrent requests (e.g., two operators scoring different matches simultaneously) can produce TOCTOU races: read config, modify in-memory, write back — if two requests interleave, the second write overwrites the first's changes.

PR #103/#104 addressed the most critical TOCTOU vectors (tournament password, pool/bracket scoring, competition lifecycle) with targeted mutex guards, but the store's overall design still relies on callers getting locking right. There is no store-level transaction primitive.

**Suggestion**: Introduce a `Store.WithTransaction(fn func(tx StoreTx) error) error` method that:
- Acquires the relevant lock(s) for the competition/tournament being modified.
- Provides a `StoreTx` handle that reads and writes within the lock scope.
- Commits (writes to disk) on success, rolls back (discards in-memory changes) on error.

This centralizes the locking discipline so individual handlers don't need to know about mutexes. Start with competition-scoped transactions since that's where most concurrent writes happen.

**Risk**: Medium. Requires refactoring handler code to use transactions. Can be done incrementally — wrap one handler at a time.

---

## 11. Extend the match model for decision types and competitor eligibility

**Problem**: The current `MatchResult` model records ippon scores and a winner, but does not capture *how* the match was decided. A fought 2–0, a withdrawal (kiken) 2–0, and a no-show (fusenpai) 2–0 are stored identically. There is also no competitor-level eligibility state — once a competitor withdraws, the app has no way to flag or block their remaining scheduled matches.

This becomes architecturally relevant when multiple features need it: kiken/fusenpai handling (Gap 13 in `gaps_tournament_spec.md`), fusensho for absent team members (Gap 10), and team position constraints all depend on richer match metadata and per-competitor state.

**Current model** (simplified from `state/models.go`):
```go
type MatchResult struct {
    SideA       string       // Player/Team Name
    SideB       string
    Winner      string       // "sideA", "sideB", or ""
    Decision    string       // currently only "hikiwake" or "" (draw flag)
    Status      MatchStatus  // "scheduled", "running", "completed"
    IpponsA     []string     // M, K, D, T, H
    IpponsB     []string
    HansokuA    int
    HansokuB    int
}
```

**Note**: The frontend maintains a separate `score.type` field ("ippon", "hikiwake", "hantei", "bye") for display purposes, but this is not persisted on the backend. The Go `Decision` field currently serves only as a draw flag — it does not distinguish how a non-draw match was won.

**Suggestion**: Repurpose `Decision` as a general match resolution type and add a new field for attribution:
```go
type MatchResult struct {
    // ... existing fields ...
    Decision   string    // extend: "kiken", "fusenpai", "fusensho" alongside "hikiwake"
    DecisionBy string    // which side triggered the decision (for kiken/fusenpai: the losing side)
}
```

And add competitor-level eligibility tracking:
```go
type CompetitorStatus struct {
    PlayerID    string
    Eligible    bool
    Reason      string    // "kiken", "fusenpai", or ""
    MatchID     string    // the match where eligibility was lost
}
```

The engine should check eligibility before allowing a match to start, and auto-resolve remaining matches for ineligible competitors.

**Risk**: Medium. Changes the match data model, which affects storage, API responses, SSE events, and frontend rendering. Best done before other features that depend on richer match metadata.

---

## Summary

| # | Recommendation | Impact | Effort | Risk |
|---|---------------|--------|--------|------|
| 1 | Split `helper/` package | High | High | Medium |
| 2 | Introduce interfaces for Store/Engine/Hub | High | Medium | Low |
| 3 | Increase `excel/` test coverage | Medium | Medium | None |
| 4 | API boundary validation | Medium | Low | Low |
| 5 | Adopt frontend router | Medium | Medium | Medium |
| 6 | Separate `api.jsx` concerns + deduplicate `patchCompetitionData` | Low | Low | Low |
| 7 | Complete `domain/` adoption | Medium | High | Medium |
| 8 | Add frontend render error boundary | Low | Low | Low |
| 9 | Modularize `web/index.html` serve UI | Medium | Medium | Medium |
| 10 | Harden `state.Store` concurrency safety | High | Medium | Medium |
| 11 | Extend match model for decision types + eligibility | Medium | Medium | Medium |

Recommendations 2, 3, 4, 6, and 8 can each be done independently as small PRs. Recommendation 11 should be done early if kiken/fusenpai or fusensho features are planned, since other features depend on the richer data model. Recommendations 1, 5, 7, 9, and 10 are larger refactors best planned as dedicated efforts.
