# Brief: Closing the Gaps in the Bracket Creator Mobile App

A natural-language brief intended as the prompt for `/speckit.specify` (https://github.com/github/spec-kit). Consolidates four gap-analysis documents into one cohesive scope.

## Goal

The bracket-creator mobile app is the live-event tool for running kendo tournaments. Source-of-truth requirements live in `running_a_kendo_tournament.md`. Four gap analyses identified ~30 capability, UX, and architectural gaps between that spec and the current implementation:

- `gaps_tournament_spec.md`; 16 user-facing feature gaps
- `gaps_implementation_plan.md`; a staged plan covering 6 of those gaps
- `gaps_webui.md`; 14 UI issues (10 already fixed, 4 open)
- `gaps_ARCHITECTURE.md`; 11 codebase quality recommendations

This brief asks SpecKit to plan the work needed to close all those gaps so the app can fully support a multi-court, multi-competition kendo tournament from setup through closing.

## Scope

**In scope**:

- All user-facing tournament-spec gaps (Gap 1–16)
- All open webui issues (Issues 11–14); already-fixed issues (1–10) are omitted
- All 11 architecture recommendations as non-functional constraints

**Out of scope** (physical/logistical, no app support needed):

- Equipment inspection (kensa) at physical stations
- Opening/closing ceremonies (modelled only as time blocks for schedule estimation)
- Referee rotation and breaks (Shinpan-cho concern)
- Medical/first aid (recorded outcomes only)
- Awards presentation (physical ceremony)
- The Excel CLI bracket generator's core flow (already complete; only Time Estimator improvements are in scope, per §7b)

## Glossary

- **Shiaijo / Court**: A single match area, labelled A–Z; up to 26 per tournament.
- **Shiro / Aka**: White (left) / Red (right); the two sides of a kendo match. Fixed convention across all views.
- **Ippon**: A point scored by Men (M), Kote (K), Do (D), Tsuki (T), or by Hansoku (H) penalty award.
- **Hantei**: Judges' decision awarding a point after time expires; recorded as "H" in our scoring model.
- **Hikiwake**: Draw (tied match). Marked with the "X" toggle.
- **Encho**: Overtime period under ippon-shobu (first-point) rules after a knockout draw.
- **Kiken**: Withdrawal mid-tournament (injury/illness). Opponent wins 2–0 (1–0 in encho). The withdrawn competitor is barred from subsequent matches.
- **Fusenpai**: No-show at court call. Opponent wins 2–0. Same prohibition as kiken.
- **Fusensho**: Default win awarded when a team is missing a player for a position; counts as 2–0 for that bout.
- **Daihyosen**: Representative ippon-shobu bout used to resolve a tied team knockout match when individual victories (IV) and points won (PW) are equal.
- **Kachinuki**: Winner-stays-on team format; the winning fighter remains to face the next opponent.
- **Senpo / Jiho / Chuken / Fukusho / Taisho**: Standard 5-person team positions (1st through 5th/captain).
- **IV / PW**: Individual Victories / Points Won; team match totals.
- **Pool**: Round-robin group (full or partial). Winners advance to playoffs in mixed format.
- **Playoff / Bracket**: Single-elimination tree.
- **Zekken**: Cloth name-tag worn during match; the `withZekkenName` competition option uses this as the display name.

## Roles

- **Tournament Manager**: Global control over all competitions, courts, settings; can override anything.
- **Court Manager**: Oversees flow at one shiaijo (paper task today; no distinct app role yet).
- **Table Operator**: Records scores at one assigned shiaijo. Should not see or modify other courts' matches or tournament settings.
- **Scorecard Helper**: Physical helper at a court; reads the court TV to know which names to write on the scorecard.
- **Ribbon Helper**: Physical role only; no app access needed.
- **Participant (Athlete)**: Looks up their own upcoming matches and queue position.
- **Coach**: Tracks multiple players or a whole dojo across courts; reviews results.
- **Spectator / Audience**: Public viewer with read-only access.
- **Streaming Crew**: Pulls live match data into OBS/vMix overlays.

---

## Feature Areas

### 1. Role-Based Access (High priority)

Today a single shared password grants full admin access; every authenticated user can change anything. The scoring editor's Prev/Next is court-scoped, but the rest of the admin UI is not.

**User stories**:

- As a tournament manager, I want to issue scoped credentials to table operators so they can only score matches on their assigned court(s).
- As a table operator, I want the admin UI to show only my court's matches and hide settings/competitions I shouldn't touch.

**Requirements**:

- At least two roles: **Manager** (full access) and **Operator** (scoped to one or more assigned courts).
- Backend enforcement: operator API calls must be rejected when the target match is not on an assigned court.
- Frontend scoping: operator view hides tournament-wide settings, other competitions' admin pages, and other courts' matches.
- Auth options (one or both): per-role passwords, or manager-issued session tokens with court assignment embedded.
- A court-scoped admin schedule view (e.g., `/admin/schedule?court=A`) that operators bookmark and that persists across sessions. This also closes webui Issue 12 (operators wasting time scanning multi-court grids on tablets).

**Out of scope**: a full user/account system. Lightweight tokens or per-role passwords are sufficient.

---

### 2. Display Modes for TVs, Lobby, and Streaming (High–Medium priority)

Tournaments need passive read-only displays for several audiences. These share a `/display` route family with no auth and no chrome.

#### 2a. Per-court TV display (`/display?court=A`)

- Fullscreen, dark theme, large fonts readable from 5+ metres.
- Current match: player/team names, Shiro (left) / Aka (right), live ippons, competition name, phase (pool name or round label).
- Next 2 upcoming matches in a visible queue below.
- Live indicator and auto-update via existing SSE stream; no polling.
- Auto-promote first scheduled match if no live match exists.
- Edge cases: "No matches scheduled", "All matches completed", multiple live matches on one court, SSE reconnect indicator.

#### 2b. Lobby / venue overview (`/display?court=all`)

- One card per active court, CSS grid auto-fit.
- Each card: court label, current match (names + side colours), next 2 upcoming matches, competition name and phase per match.
- Adapts from 2 to 6+ courts; legible from 5+ metres; auto-cycle/page if too many courts for one screen.

#### 2c. Streaming overlay (`/display?court=A&overlay=true`)

- Transparent background for OBS/vMix browser-source capture.
- Lower-third (default) or configurable position (`?position=top`) showing player names, side colours, live ippons.
- Uses the zekken/display name when `withZekkenName` is enabled on the competition (matches what's physically worn).
- Hides automatically when no match is running.
- Animates in/out as matches start/end.

#### 2d. JSON endpoint for streaming integrations

- `GET /api/viewer/court/:court/live` returning a flat JSON object with the current running match (or `null`).
- Fields: court, status, competition, phase, sideA (name, dojo), sideB (name, dojo), ipponsA, ipponsB, hansokuA, hansokuB.
- Designed for vMix data sources, custom OBS plugins, or any non-browser integration.

#### 2e. Launch UX

- Viewer home shows a "Display modes" section with link cards: one per court plus the overview. Links open in new tabs (displays run on separate screens).

---

### 3. Participant & Coach Self-Service (Medium priority)

#### 3a. My-match (Gap 2 + Plan Stage 4)

- "Find my matches" entry point in viewer (reuse `PlayerMultiFilter`).
- Selection persisted in localStorage (`bc_my_player_id`, `bc_my_player_name`).
- Activates the currently-stubbed "Your next match" card on viewer home, showing opponent, court, time, phase.
- Highlight player's matches in schedule views (reuse `.tw-match--highlight`).
- Header indicator: "Following: Name [X]" with clear button.
- URL parameter `?player=<uuid>` auto-selects (for QR codes on registration badges).
- Name-based fallback matching alongside UUID, because `buildPlayerMap` currently uses player names as IDs in some paths.

#### 3b. Coach watchlist (Plan Stage 5)

- "Watchlist" entry point in viewer manages a list of followed participants.
- Entries in localStorage (`bc_watchlist`) as `[{id, name, dojo}]`.
- "Watched matches" home section showing upcoming matches for all watched players (cap ~6).
- Auto-populates the schedule filter `picked` array, reusing existing `applyFilters` / `matchHighlightedBy`.
- Dojo filter remains available (the existing `PlayerMultiFilter` already supports case-insensitive dojo substring matching).

#### 3c. Match queue position (Gap 7)

- Computed `queuePosition` per scheduled match: count of non-completed matches ahead on the same court.
- Displayed in viewer schedule ("Match 3 of 12 on Shiaijo A" or "2 matches before yours") and on the court TV display (§2a) next to upcoming matches.
- Recalculated in real time on SSE match-completion events.

---

### 4. Match Resolution & Competitor Eligibility (Medium priority)

Today `MatchResult.Decision` is a draw flag only. A fought 2–0, a kiken 2–0, and a fusenpai 2–0 are stored identically. Withdrawn competitors can silently reappear in later bracket rounds.

#### 4a. Decision types (Gaps 10, 13, 15)

- Extend `MatchResult.Decision` to carry: `hikiwake`, `kiken`, `fusenpai`, `fusensho`.
- New `DecisionBy` field naming the side that triggered the decision (the losing side for kiken/fusenpai/fusensho).
- Scoring modal additions:
  - "Kiken" (withdrawal) action: auto-fills 2–0 in regulation, 1–0 in encho, records reason.
  - "Default win" / "Fusenpai" action for no-shows: auto-fills 2–0.
  - "Fusensho" action per bout in team scoring for missing-player defaults; contributes to IV/PW.
- Encho tracking: counter or flag indicating how many encho periods were played; optional separate hansoku tracking per period if the tournament rules reset fouls between encho.
- Visual distinction in match list, results, and brackets: "Kiken", "Fus.", "(E)" suffixes alongside the score.

#### 4b. Eligibility tracking (Gap 13)

- New `CompetitorStatus { PlayerID, Eligible, Reason, MatchID }` state.
- Engine checks eligibility before allowing a match to start; rejects starting a match where a competitor is marked ineligible.
- After kiken/fusenpai is recorded, the app surfaces the competitor's remaining scheduled matches and prompts the operator to resolve them (award default wins to opponents or remove from bracket).
- Team kiken under FIK rules: validate that the correct positions are vacated (Jiho for 1 withdrawal, Jiho + Fukusho for 2; Senpo and Taisho cannot be forfeited; 3+ withdrawals disqualify the team).

---

### 5. Team Operations (Low–Medium priority)

#### 5a. Lineup management (Gap 14, webui Issue 14)

- Named positions: Senpo, Jiho, Chuken, Fukusho, Taisho for 5-person teams; numbered positions for other sizes.
- Lineup submission UI before competition start: each team (or the tournament admin) assigns members to positions.
- Between-round revision: after a team match completes, the team can submit a revised order for the next round; lineup locks when the next match starts.
- Validation: Senpo and Taisho cannot be vacant.
- Display: position names shown alongside player names in the team scoring modal and match results.

#### 5b. Kachinuki winner-stays-on (Gap 9)

- New team match type distinct from the standard fixed-bout team match.
- Dynamic bout progression: winner of bout N faces the next opponent; on hikiwake, both retire and the next pair enters.
- Variable bout count; the match ends when one team is eliminated.
- Scoring: team result determined by eliminations, not IV/PW.
- Team scoring modal must support dynamic bout addition (no fixed grid).

#### 5c. Large-team scoring modal (webui Issue 11)

- Sticky/fixed summary row at the top while bouts scroll, OR paginated/collapsible bout view.
- Operator must always see running IV/PW totals while scoring later bouts in a 10+ player team match.

#### 5d. Daihyosen (representative bout)

- For tied team knockout matches (IV and PW equal), allow the operator to add a dynamic representative ippon-shobu bout instead of working around it manually.

---

### 6. Tournament Formats (Low priority, but spec-required)

#### 6a. Partial round-robin pools (Gap 8)

- Competition-level setting: `full` (default) or `partial` (adjacent-neighbour pairings: 1v2, 2v3, …).
- Reduces match count for large pools (8+). Same pool ranking criteria apply; no engine changes for ranking.

#### 6b. League format (Gap 12)

- Explicit "League" label in competition setup (today this emerges as a side effect of not creating playoffs after pools complete).
- Hide or contextualise the "Create playoff bracket" button on the Scores–edit page when in league mode.
- "Final standings" header on the pool standings page; derive competition winner from standings.

#### 6c. Swiss system (Gap 11)

- New format alongside Pools, Playoffs, and Mixed.
- Round-by-round pairing engine matching competitors with similar records.
- Configurable number of Swiss rounds.
- Cumulative standings computed across rounds.
- Admin UI to generate next-round pairings after the current round completes.

---

### 7. Scheduling & Time Estimation (Low priority, but pervasive)

#### 7a. Per-phase match duration (Gap 6, Plan Stage 1)

- New fields on `Competition`: `PoolMatchDuration`, `PlayoffMatchDuration` (minutes).
- Persist in YAML frontmatter; expose via API on competition create/update.
- Auto-schedule uses the right duration per match phase.
- Admin UI: two duration inputs when the competition has both pools and playoffs.

#### 7b. Schedule estimation improvements (Gap 16)

- **Clock-to-elapsed ratio**: replace the fixed 30-second padding with a configurable multiplier (spec recommends 1.5–2x clock time; e.g., 4.5–6 min for a 3-min match, not 3.5 min).
- **Ceremony and break blocks**: configurable opening (15–30 min), closing (20–30 min), and lunch slots.
- **Slowest-court buffer**: add 10–15% to the optimistic parallel-court division (`sequential time ÷ courts`).
- **Team match duration**: scale by bout count plus inter-bout transitions, rather than treating a team match as a single individual match.
- **Mobile parity**: reconcile the Excel Time Estimator and the mobile app's auto-scheduler so both produce consistent estimates.

---

### 8. UX Polish; Open webui issues

The four open issues in `gaps_webui.md` map onto the features above:

- **Issue 11** (sticky summary in large team modal) → covered in §5c.
- **Issue 12** (court-scoped admin schedule) → covered in §1.
- **Issue 13** (kiken/fusenpai in scoring modal) → covered in §4.
- **Issue 14** (team lineup UI) → covered in §5a.

Already-fixed issues (Issues 1–10) are intentionally omitted to avoid re-speccing completed work.

---

### 9. Technical Foundation (Non-functional constraints)

These reshape the codebase so the features above land safely. They do not deliver direct user value, but they constrain how the work above is done. Included per explicit user request.

- **9.1 Split `helper/` package**: Extract `bracket/` (tree algorithms), `seeding/`, `pool/`, `csv/` from `helper/`; absorb `helper/excel*.go` into `internal/excel/`. Clarify that `engine/` composes the new sub-packages rather than passing through to `helper/`.
- **9.2 Interfaces at consumer boundaries**: Define `CompetitionStore`, `ScoringEngine`, `Broadcaster` (SSE hub) where they're consumed (e.g., `mobileapp/deps.go`), not in the implementing packages. Enables unit-testing handlers without a real store/hub.
- **9.3 Excel test coverage**: Unit tests for `excel.NewFileFromScratch()`; snapshot-style tests for `helper/excel*.go` that read cells back with `excelize` (no golden files). Prioritise pool layout and tree sheet rendering.
- **9.4 API boundary validation**: `Request.Validate()` methods on handler payloads; reject invalid input at the handler, not deep in the engine. Use struct methods, not a framework.
- **9.5 Frontend router**: Adopt `preact-router` (3 KB gzipped) to replace manual `getRouteFromUrl`/`getUrlFromRoute` parsing. Especially valuable as display routes (`/display?court=A`, `/display?court=A&overlay=true`, etc. from §2) multiply.
- **9.6 Split `api.jsx`**: Separate HTTP client (`api_client.jsx`), serialisers (`api_serializers.jsx`), and centralised SSE patch logic (`patch.jsx`, deduplicated from `app.jsx`/`admin.jsx`). Keep `data.jsx` as sample data generators only.
- **9.7 Complete `domain/` adoption**: Migrate engine, state, and mobileapp to `domain.Player`/`domain.Pool`/etc.; keep `helper.Player` only inside `helper/` and `excel/` for rendering. Add conversion functions at the boundary. Incremental, one package at a time, starting with `engine/`.
- **9.8 Frontend render error boundary**: Preact `ErrorBoundary` at the `App` level so a render exception shows a recoverable banner instead of a white screen. SSE-disconnect retry already exists in `api.jsx`.
- **9.9 Modularise `web/index.html`**: Extract inline JS into `web/js/{app,validation,seeding,time_estimator,api}.js` + `web/css/styles.css`. Update `//go:embed web/*` to include subdirectories. Add Vitest tests for pure validation and time-estimator logic.
- **9.10 State store transactions**: `Store.WithTransaction(fn func(tx StoreTx) error) error` to centralise locking. Acquires the relevant competition/tournament locks, provides a transactional handle, commits on success, rolls back on error. Wrap handlers incrementally. Builds on the existing PR #103/#104 mutex work.
- **9.11 Extend match model for decision types and eligibility**: Repurpose `Decision` as a resolution type (`hikiwake`, `kiken`, `fusenpai`, `fusensho`), add `DecisionBy`. New `CompetitorStatus` for per-competitor eligibility. **This is the data-model foundation for §4 (Match Resolution) and must land before that user-facing work.**

---

## Non-Functional Requirements

- **Live-event constraint**: All UI changes must remain functional and performant on tablets used at the table side. The score editor's chained navigation (Prev/Next, ←/→, Finish + Start Next) must stay scoped to the current shiaijo; operators run matches per-court, so hopping courts mid-flow breaks the workflow.
- **Real-time updates**: All viewer and display surfaces must update via SSE; no polling.
- **Embedded-binary delivery**: The mobile app frontend is embedded into the Go binary at build time (`//go:embed web-mobile/*`). The build pipeline must continue to rebuild the esbuild bundle on every `go build` (`make run-mobile` already does this).
- **Excel output stability**: Changes to scheduling or formats must not break the existing Excel bracket export. Validate with `make examples`.
- **Project constitution**: Comply with `.specify/memory/constitution.md` (YAGNI, DRY, TDD, DDD, evidence-based decisions, bracket integrity, live-tournament safety).
- **Schema compatibility**: Existing `participants.csv` schemas (both with-UUID and legacy without-UUID) and competition YAML frontmatter must round-trip cleanly when new fields are added.

## Acceptance Approach

- Each feature area should be independently shippable.
- Every change validated by `make go/test` and inspection of generated example files from `make examples`.
- For UI changes, validate in a running browser via `make run-mobile`; not by reading diffs.
- For schedule and format changes, validate that the Excel Time Estimator output remains coherent.

## Sequencing & Dependencies

- **Foundation first**: §9.11 (extend match model + eligibility) must land before §4 (Match Resolution).
- **Independent / quick wins**:
  - §1 (Role-Based Access; high impact)
  - §7a (Per-phase durations; small, isolated)
  - §3c (Queue position; small, isolated)
  - §6a (Partial round-robin)
  - §6b (League labeling)
  - Encho-only metadata from §4a
- **Display family** (§2): Build §2a first; §2b and §2c share components and routing with §2a. §2c benefits from §9.5 (router).
- **Participant family**: §3b (Watchlist) extends §3a (My-match).
- **Team family** (§5): §5a (Lineup) is foundational. §5b (Kachinuki) is the largest single-feature build. §5c (Sticky summary) and §5d (Daihyosen) are small additions.
- **Formats** (§6c Swiss): Defer unless concrete demand; it's a large new engine.
- **Refactors** (§9): Mostly independent. §9.1 (helper/ split) and §9.11 (match model) are the largest. §9.6 (api.jsx split) and §9.8 (error boundary) can ship anytime.

## Notes for SpecKit

- This brief consolidates four source documents (`gaps_tournament_spec.md`, `gaps_ARCHITECTURE.md`, `gaps_implementation_plan.md`, `gaps_webui.md`). Where they overlap (e.g., Plan Stage 4 vs. tournament-spec Gap 2, or webui Issue 13 vs. Gap 13), this brief follows the more comprehensive tournament-spec gap and absorbs implementation details into the plan/tasks phase.
- Already-fixed webui issues (1–10) are omitted to avoid re-speccing completed work.
- Architecture recommendations are included as non-functional constraints rather than user-facing features. They do not map cleanly to user stories or acceptance scenarios; that is expected and acceptable.
- The scope is intentionally broad (the user chose "all gaps including refactors"). SpecKit may want to subdivide into multiple feature branches during `/speckit.plan`; sequencing guidance above identifies natural seams.
