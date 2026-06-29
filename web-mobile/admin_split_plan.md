# admin.jsx split plan

`web-mobile/js/admin.jsx` is 3538 lines / 31 top-level components. Build wiring uses a `window.*` global pattern; `make go/build` runs `esbuild web-mobile/js/*.jsx` and `index.html` loads each compiled `dist/*.js` separately. Splitting has **no module-graph cost**: each new file adds `<script type="module">` tags in `index.html` and `window.X = X` exports.

The biggest gotcha is that several "shell" components (`AdminTopbar`, `Breadcrumbs`, `CourtPicker`) are referenced by code in 4+ proposed files. They must live in a file that **loads before** every page-level file. That forces a revision to the starting split sketched in the task brief; see [§1](#1-proposed-file-structure) and [§3](#3-load-order).

## 1. Proposed file structure

LOC estimates from line-range arithmetic on the current file.

| File | Components / helpers | ~LOC |
|---|---|---:|
| `admin_helpers.jsx` | `sideName`, `compMatchStats`, `normalizeDate` (cross-file pure helpers) | 80 |
| `admin_shell.jsx` | `Breadcrumbs`, `AdminTopbar`, `AdminDashboard`, `CompCard`, `CourtPicker`, `initialSectionFor` (+ `LIVE_STRIP_MAX_CHIPS`) | 310 |
| `admin_scoring_modal.jsx` | `FoulCounter`, `ScoreEditorModal`, `TeamScoreEditorModal`, `TEAM_POSITIONS` | 530 |
| `admin_participants.jsx` | `AdminParticipants`, `LinedTextarea`, `levenshtein`, `parsePastedRows`, `looksLikeHeader` | 580 |
| `admin_pools.jsx` | `AdminPools` | 250 |
| `admin_schedule.jsx` | `AdminSchedulePage`, `AdminTWMatch`, `AdminScoreEditorPage`, `ScoreEditCourtBtn`, `AdminScoreEditor`, `AdminExport`, `timeToMinutes` | 500 |
| `admin_competition.jsx` | `AdminCompetition`, `AdminCompOverview`, `AdminSettings`, `AdminBracket`, `LiveMatchPanel` | 540 |
| `admin_setup.jsx` | `AdminEditTournament`, `AdminCreateCompetition`, `AdminImportPage` | 430 |
| `admin.jsx` (shell) | `AdminApp`, `REFRESHABLE_EVENTS`, `patchCompetitionData`, `window.AdminApp` export | 480 |

**Deviations from the brief's starter split:**
- `AdminTopbar`/`CompCard`/`AdminDashboard` move **out** of `admin.jsx` into `admin_shell.jsx`. Reason: `AdminTopbar` is referenced in 7 different page components across 4 proposed files; keeping it in `admin.jsx` (which must load last because it aliases all section components) creates a load-order deadlock.
- `CourtPicker` moves into `admin_shell.jsx` instead of `admin_competition.jsx`. Reason: it's used by `LiveMatchPanel` (competition), `AdminTWMatch` (schedule), and `ScoreEditCourtBtn` (schedule); cross-file consumer set.
- `LinedTextarea` moves into `admin_participants.jsx` (its sole consumer) instead of `admin_competition.jsx`.
- Helpers `looksLikeHeader` / `parsePastedRows` / `levenshtein` / `timeToMinutes` travel with their sole consumers rather than collecting in `admin_helpers.jsx`. Only the truly cross-file helpers (`sideName`, `compMatchStats`, `normalizeDate`) live there.

## 2. Dependency map

`<-` = aliases at module top (load-order critical). `~>` = references at render time (just needs to be set before mount).

| File | Produces (`window.*`) | Consumes from siblings (`window.*`) |
|---|---|---|
| `admin_helpers.jsx` | `sideName`, `compMatchStats`, `normalizeDate` |; |
| `admin_shell.jsx` | `Breadcrumbs`, `AdminTopbar`, `AdminDashboard`, `CourtPicker` (`CompCard`, `initialSectionFor` stay module-internal) | `<-` `sideName`, `compMatchStats`, `StatusBadge`, `formatDate`, `pluralize`, `competitionKindLabel` |
| `admin_scoring_modal.jsx` | `ScoreEditorModal` (`TeamScoreEditorModal`, `FoulCounter` stay module-internal) | `<-` `useEscapeToClose`, `isTextEntry`, `isInteractiveTarget`, `isHikiwake`, `arraysEqual`, `formatIpponsScore` |
| `admin_participants.jsx` | `AdminParticipants` | `<-` `AdminTopbar`, `Breadcrumbs`, `StableInput`, `parseParticipantLines`, `pluralize`, `API` |
| `admin_pools.jsx` | `AdminPools` | `<-` `formatIpponsScore`, `API`, `pluralize` |
| `admin_schedule.jsx` | `AdminSchedulePage`, `AdminScoreEditorPage`, `AdminScoreEditor`, `AdminExport` | `<-` `AdminTopbar`, `Breadcrumbs`, `CourtPicker`, `ScoreEditorModal`, `PlayerMultiFilter`, `applyFilters`, `matchHighlightedBy`, `tournamentMatches`, `compMatches`, `formatIpponsScore`, `addMinutes`, `StableInput`, `API` |
| `admin_competition.jsx` | `AdminCompetition` | `<-` `AdminTopbar`, `Breadcrumbs`, `CourtPicker`, `AdminParticipants`, `AdminPools`, `AdminScoreEditor`, `AdminExport`, `BracketTree`, `StableInput`, `normalizeDate`, `compMatchStats`, `pluralize`, `API` |
| `admin_setup.jsx` | `AdminEditTournament`, `AdminCreateCompetition`, `AdminImportPage` | `<-` `AdminTopbar`, `Breadcrumbs`, `StableInput`, `buildCompetition`, `parseParticipantLines`, `normalizeDate`, `pluralize`, `API` |
| `admin.jsx` | `AdminApp` | `<-` `AdminDashboard`, `AdminCreateCompetition`, `AdminEditTournament`, `AdminSchedulePage`, `AdminScoreEditorPage`, `AdminImportPage`, `AdminCompetition`, `pluralize`, `mergeMatchPatch`, `sideName`, `compMatches`, `tournamentMatches`, `API` |
| `app.jsx` (unchanged) | `App` | `~>` `AdminApp` |

**No circular references.** The producer/consumer graph is a DAG provided `CourtPicker` lives in `admin_shell.jsx` and `AdminTopbar` is **not** in `admin.jsx`. If `AdminTopbar` were kept in `admin.jsx`, every page file would need `admin.jsx` loaded first, but `admin.jsx` aliases page components → deadlock.

## 3. Load order

Module scripts are deferred and execute in document order, so the existing `<script type="module">` mechanism preserves order naturally. Required order in `web-mobile/index.html` (deps before users):

```html
<script type="module" src="/dist/api.js?v=5"></script>
<script type="module" src="/dist/data.js?v=5"></script>
<script type="module" src="/dist/bracket.js?v=5"></script>
<script type="module" src="/dist/ui.js?v=5"></script>
<script type="module" src="/dist/viewer.js?v=5"></script>
<!-- admin layer: leaf-first -->
<script type="module" src="/dist/admin_helpers.js?v=5"></script>
<script type="module" src="/dist/admin_shell.js?v=5"></script>
<script type="module" src="/dist/admin_scoring_modal.js?v=5"></script>
<script type="module" src="/dist/admin_participants.js?v=5"></script>
<script type="module" src="/dist/admin_pools.js?v=5"></script>
<script type="module" src="/dist/admin_schedule.js?v=5"></script>
<script type="module" src="/dist/admin_competition.js?v=5"></script>
<script type="module" src="/dist/admin_setup.js?v=5"></script>
<script type="module" src="/dist/admin.js?v=5"></script>
<script type="module" src="/dist/app.js?v=5"></script>
```

Bump the `?v=` query param to bust browser cache of the old `admin.js`. Confirmed no TDZ risk: every `const X = window.X` alias at a module's top resolves because all dependency files appear earlier in the document.

## 4. Build wiring

The esbuild glob in `Makefile:104` is `web-mobile/js/*.jsx`, so any new `.jsx` files are picked up automatically with **no Makefile change**. The `EMBEDDED_ASSETS` variable in `Makefile:8` watches `web-mobile/` so `go/build` re-runs when sources change. No other changes needed.

One caveat: `npm run lint` (config in `web-mobile/`) may flag new files if eslint config is path-scoped; verify after first split PR lands.

## 5. Migration sequence

One file per PR, each independently mergeable. After each step `make go/build && make run-mobile` and smoke-test the admin UI (dashboard → competition → participants → settings → bracket → schedule → score editor → export).

| # | Move | Adds globals | Risk |
|---|---|---|---|
| 1 | Extract `admin_helpers.jsx` (`sideName`, `compMatchStats`, `normalizeDate`) | `window.sideName`, `window.compMatchStats`, `window.normalizeDate` | Lowest. Pure helpers, no JSX, easy to verify. |
| 2 | Extract `admin_shell.jsx` (Breadcrumbs, AdminTopbar, AdminDashboard, CompCard, CourtPicker, `initialSectionFor`) | `window.Breadcrumbs`, `window.AdminTopbar`, `window.AdminDashboard`, `window.CourtPicker` (CompCard/initialSectionFor are module-internal; only `AdminDashboard` references them) | Medium. Touches the dashboard + topbar live-strip path. Test live-match chips, navigation, "Start All" button. |
| 3 | Extract `admin_scoring_modal.jsx` (`ScoreEditorModal`, `TeamScoreEditorModal`, `FoulCounter`) | `window.ScoreEditorModal` (TeamScoreEditorModal/FoulCounter are module-internal; only `ScoreEditorModal` renders them) | Medium. Largest single behavioral surface; verify both individual and team scoring keyboard shortcuts (prev/next chained nav, draw toggle, foul counter). |
| 4 | Extract `admin_participants.jsx` (+ LinedTextarea, levenshtein, parsePastedRows, looksLikeHeader) | `window.AdminParticipants` | Medium-high. Biggest single component (~530 LOC) with CSV paste, drag-import, seed UX, reserved-slot dialogs. Verify all four paste/import paths. |
| 5 | Extract `admin_pools.jsx` (`AdminPools`) | `window.AdminPools` | Low. Self-contained section. |
| 6 | Extract `admin_schedule.jsx` (`AdminSchedulePage`, `AdminScoreEditorPage`, `AdminScoreEditor`, `ScoreEditCourtBtn`, `AdminExport`, `AdminTWMatch`, `timeToMinutes`) | `window.AdminSchedulePage`, `window.AdminScoreEditorPage`, `window.AdminScoreEditor`, `window.AdminExport` | Medium. Verify chained-next/prev navigation stays court-scoped (recent fix), status-sort order in score editor, time auto-assignment. |
| 7 | Extract `admin_competition.jsx` (`AdminCompetition`, `AdminCompOverview`, `AdminSettings`, `AdminBracket`, `LiveMatchPanel`) | `window.AdminCompetition` | Medium. Verify section routing (overview/settings/bracket/scores), live-match tap/card/scoreboard modes, bracket override prompt. |
| 8 | Extract `admin_setup.jsx` (`AdminEditTournament`, `AdminCreateCompetition`, `AdminImportPage`) | `window.AdminEditTournament`, `window.AdminCreateCompetition`, `window.AdminImportPage` | Low-medium. Verify date-normalization, the team-size field, and the CSV bulk-import dropzone. |
| 9 | Shell-only `admin.jsx`: alias all sibling globals, keep `AdminApp` + `patchCompetitionData` + `REFRESHABLE_EVENTS` + `window.AdminApp` export | (none new) | Lowest. Just removes already-moved code. After this, original file should be ~480 lines. |

Steps 1–3 can ship serially; 4–8 are independent and parallelizable once 1–3 are in.

After every step: bump `?v=` in `index.html` script tags and run `make js/validate && make go/test`.

## 6. Test impact

Files in `web-mobile/js/__tests__/`:

| Test file | Imports from `admin.jsx`? | Action |
|---|---|---|
| `admin_ui_fixes.test.jsx` | No; imports `arraysEqual` from `data.jsx` and re-implements `pluralize` locally. Conceptual tests only. | **None.** |
| `api.test.jsx` | No | None |
| `data.test.jsx` | No | None |
| `keyboard_shortcuts.test.jsx` | No; imports `isTextEntry`/`isInteractiveTarget` from `ui.jsx`. | **None.** |
| `mergeMatchPatch.test.jsx` | No | None |
| `score_display.test.jsx` | No | None |
| `ui.test.jsx`, `viewer.test.jsx` | No | None |

No existing tests import from `admin.jsx`. The split is test-neutral. **Opportunity:** the helpers extracted to `admin_helpers.jsx` (`sideName`, `compMatchStats`, `normalizeDate`) become testable in isolation; consider a follow-up adding `admin_helpers.test.jsx`, especially for `compMatchStats` which has the subtle "treat `{id:"",name:""}` as missing" logic that the existing comments call out.

## 7. Resolved questions & notes

### Decisions (maintainer-confirmed)

- **`window.*` reference style**; per-file aliases at the top of each new file (`const AdminTopbar = window.AdminTopbar;`). Matches the existing pattern at [admin.jsx:90](web-mobile/js/admin.jsx:90) and [admin.jsx:604](web-mobile/js/admin.jsx:604). The codebase mixes inline `<window.X />` (e.g. [viewer.jsx:300](web-mobile/js/viewer.jsx:300)) with aliasing; standardizing on aliases inside the new admin layer keeps JSX readable and surfaces load-order requirements explicitly at the top of each file.
- **`?v=` cache-busting**; single bump from `?v=4` to `?v=5` after the 9-PR migration lands. Production embeds (`//go:embed web-mobile/*` via [main.go:14](main.go:14), served from [server.go:62](internal/mobileapp/server.go:62)) refresh on every Go build, so this is a dev-side courtesy only.
- **`admin_shell.jsx` scope**; combined file (~310 LOC) holding chrome (`Breadcrumbs`, `AdminTopbar`, `CourtPicker`) and dashboard (`AdminDashboard`, `CompCard`, `initialSectionFor`). Re-split only if the dashboard grows past the 700-line cap.

### Resolved by inspection

- **`React.memo` identity**; `LiveMatchPanel` and `AdminTWMatch` are module-level `const`s wrapped once at definition. Moving them to a different file preserves identity; memoization behavior is unchanged.
- **`patchCompetitionData` location**; only consumed by `AdminApp`'s SSE handler at [admin.jsx:384](web-mobile/js/admin.jsx:384). Stays in `admin.jsx`.
- **`AdminScoreEditor` reuse**; embedded inside `AdminCompetition` (section="scores") and standalone via `AdminScoreEditorPage`. Both live in different files but the producer (`admin_schedule.jsx`) loads before the consumer (`admin_competition.jsx`) per §3, so the dependency is acyclic.

### Deferred follow-up

- **`pluralize` shadowing in tests**; was [admin_ui_fixes.test.jsx:5](web-mobile/js/__tests__/admin_ui_fixes.test.jsx:5) re-implementing `pluralize` locally rather than importing it from `ui.jsx`. **Resolved** in a follow-up commit (`05b37b0`); the test now imports the canonical `pluralize` from `ui.jsx`.
