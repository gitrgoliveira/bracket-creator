# Mobile App — Implementation Plan

Based on the gap analysis between [running_a_kendo_tournament.md](running_a_kendo_tournament.md) and the current mobile app.

## Stage 1: Per-Phase Match Durations

**Backend + frontend changes required.**

The admin UI already has auto-schedule with a single `matchDuration` input (default 3 min, not persisted). This stage adds per-phase durations to the competition config so auto-schedule uses the correct timing for pools vs playoffs.

### Backend

- `internal/state/models.go` — Add `PoolMatchDuration` and `PlayoffMatchDuration` (int, minutes) to `Competition` struct.
- `internal/state/competition.go` — Persist/load new fields in YAML frontmatter.
- `internal/mobileapp/handlers_competition.go` — Include new fields in create/update endpoints.

### Frontend

- `admin.jsx` — In competition settings: two duration inputs (pools, playoffs). In auto-schedule (`admin.jsx:2595–2619`): use the appropriate duration per match phase.
- `api.jsx` — Send new fields on competition create/update.

### Tests

- Go unit tests for `Competition` YAML round-trip with new duration fields.
- Go handler test: create/update competition with durations, verify they persist and return correctly.
- JS test: auto-schedule applies pool duration to pool matches and playoff duration to bracket matches.

### What it enables

Accurate estimated match times. The auto-schedule already works — it just gains phase-aware durations.

---

## Stage 2: TV Display Mode (Single Court)

**Frontend only.** URL: `/display?court=A`

Fullscreen, dark-background, auto-updating scoreboard for a TV at one shiaijo. Also serves **scorecard helpers** — they can use this view to know which names to put on the board.

### What it shows

- Current match: large player/team names, SHIRO (left) / AKA (right) clearly marked, live ippons.
- Next 2 upcoming matches on that court.
- Competition name and phase (pool name or round label).
- No header, footer, or navigation chrome — fills the viewport.

### Design

- Dark background (`#0a0e17`), white text, responsive font sizing via `clamp()` and `vw`/`vh` units.
- Readable from 5+ meters on a 1080p TV.
- Pulsing live indicator (reuse existing `.dot--live` keyframe).
- Auto-updates via existing SSE stream (no polling).

### Files to change

- `app.jsx` — Add `/display` route in `getRouteFromUrl()`, new `displayCourt` state, render `DisplayMode` component, suppress auth modal. Update URL sync to handle query strings.
- `viewer.jsx` — New `DisplayCourt` component using existing `tournamentMatches()` + court filter. New `DisplayMode` wrapper that dispatches `court==="all"` (Stage 3) vs single court.
- `styles.css` — New `.display` section at end of file: fixed positioning, dark theme, `clamp()`-based font sizing for names/ippons/labels.

### Tests

- JS test: `DisplayCourt` renders current live match with correct SHIRO/AKA sides and ippons.
- JS test: `DisplayCourt` shows next 2 upcoming matches when a live match is present.
- JS test: `DisplayCourt` promotes first scheduled match to main area when no match is live.
- JS test: `DisplayCourt` shows "No matches scheduled" when court has no matches.
- JS test: `DisplayCourt` shows "All matches completed" when all matches are done.
- JS test: routing — `/display?court=A` sets mode to `"display"` and `displayCourt` to `"A"`.
- Manual: open `/display?court=A` on a second screen, score a match via admin, verify display updates via SSE.

### Edge cases

- No matches scheduled — "No matches scheduled" message.
- All matches completed — "All matches completed" message.
- Multiple live matches on one court — show first as current, rest as upcoming.
- SSE reconnection — existing 5-second retry in `api.jsx:288` is adequate; consider a subtle "Reconnecting..." indicator.

---

## Stage 3: Tournament Overview Display (All Courts)

**Frontend only.** URL: `/display?court=all`

Fullscreen grid for a projector/outside screen showing all shiaijo at once.

### What it shows

- CSS grid with one cell per active court (adapts from 2 to 6+ courts).
- Each cell: court label, current match (names + SHIRO/AKA badges), next 2 upcoming.
- Competition name and phase per match.
- Same dark theme and SSE auto-update as Stage 2.

### Design

- `grid-template-columns: repeat(auto-fit, minmax(min(350px, 100%), 1fr))` — adapts to court count and screen size.
- Each court cell is a card with subtle border.
- Font sizes smaller than single-court mode but still readable from a distance.

### Tests

- JS test: `DisplayOverview` renders one cell per court from `tournament.courts`.
- JS test: each cell shows current live match or first scheduled match.
- JS test: each cell shows next 2 upcoming matches.
- JS test: grid adapts — 2 courts renders 2 columns, 6 courts renders 3x2.
- JS test: routing — `/display?court=all` dispatches to `DisplayOverview`.
- Manual: open `/display?court=all` on a projector, verify all courts update live.

### Files to change

- `viewer.jsx` — New `DisplayOverview` component. The `DisplayMode` wrapper (from Stage 2) dispatches to it when `court === "all"`.
- `styles.css` — Grid layout additions under `.display--overview` and `.display__grid`.

---

## Stages 2 + 3: Home Page Access

**Both display modes need an easy way to launch from the viewer home page.**

### What it shows

After the "Full schedule" link on `ViewerHome`, a "Display modes" section with compact link cards:

- **Tournament overview** — opens `/display?court=all` in a new tab.
- **Shiaijo A**, **Shiaijo B**, etc. — one link per court, opens `/display?court=X` in a new tab.

Links open in new tabs (`target="_blank"`) since display modes run on different screens (TVs, projectors).

### Design

- Flex-wrap layout so links tile naturally (2 per row on mobile, more on wider screens).
- Same card styling as `vlist-item` but compact.
- Each card shows court name + short description.

### Tests

- JS test: `ViewerHome` renders display links for each court in `tournament.courts`.
- JS test: `ViewerHome` renders an "all courts" overview link.
- JS test: links use `target="_blank"` and have correct `/display?court=X` hrefs.

### Files to change

- `viewer.jsx` — Add display link section in `ViewerHome` after the "Full schedule" button.
- `styles.css` — `.display-links` and `.display-link` styles.

---

## Stage 4: "My Match" — Participant Identification

**Frontend only.** URL param: `/viewer?player=<uuid>` (for QR codes on badges).

### What it does

- "Find my matches" button in viewer header opens a search modal (reuse `PlayerMultiFilter` pattern from `viewer.jsx:772–869`).
- Selection persisted in `localStorage` (`bc_my_player_id`, `bc_my_player_name`).
- Activates the existing stubbed "Your next match" card (`viewer.jsx:426–454`, currently wired to `myPlayer = null`).
- Queue position: "X matches before yours on Shiaijo B" — count scheduled matches on the same court before the player's match.
- Highlights the player's matches in schedule views (reuse existing `.tw-match--highlight` CSS).
- Persistent indicator in header: "Following: Name [X]" with clear button.

### URL parameter support

`/viewer?player=<uuid>` auto-selects the player (for QR codes on registration badges). Only applied if no localStorage selection exists.

### Caveat

`buildPlayerMap` in `api.jsx` uses player name as the ID key. Name-based fallback matching is needed alongside UUID: `sideA?.id === myPlayerId || sideA?.name === myPlayerName`.

### Tests

- JS test: selecting a player saves to `localStorage` and renders "Your next match" card.
- JS test: "Your next match" card shows correct opponent, court, time, and phase.
- JS test: queue position counts scheduled matches on same court before player's match.
- JS test: `?player=<uuid>` URL param auto-selects player when no localStorage selection exists.
- JS test: clearing selection removes card and schedule highlights.
- JS test: name-based fallback matching works when player ID is a name string.
- Manual: select a player, navigate away and back, verify selection persists.

### Files to change

- `app.jsx` — `myPlayerId` state + localStorage sync + URL param reading; pass down to viewer components.
- `viewer.jsx` — Replace `myPlayer = null` stub at line 213; add search modal; add queue position calculation; add "Your next match" card to `ViewerHome`.
- `styles.css` — Player indicator bar, queue chip styling.

---

## Stage 5: Coach Watchlist

**Frontend only.** Follow multiple participants across the tournament.

### What it does

- "Watchlist" button in viewer header opens a management panel.
- Add/remove players via search (reuse `PlayerMultiFilter`).
- Entries stored in `localStorage` (`bc_watchlist`) as `[{ id, name, dojo }]`.
- "Watched matches" section on `ViewerHome` showing upcoming matches for all watched players (up to 6).
- Auto-populates schedule filter `picked` array with watchlist entries — leverages existing `applyFilters` and `matchHighlightedBy` in `viewer.jsx:872–894`.

### Tests

- JS test: adding a player to watchlist saves to `localStorage` and shows their upcoming matches.
- JS test: removing a player from watchlist removes their matches from the summary.
- JS test: watchlist entries auto-populate schedule filter `picked` array.
- JS test: watchlist persists across page reloads (localStorage round-trip).
- JS test: watchlist badge shows correct count.
- Manual: add 3 players, verify "Watched matches" section and schedule highlights update correctly.

### Files to change

- `app.jsx` — Watchlist state + localStorage sync.
- `viewer.jsx` — Watchlist management panel, upcoming summary section, schedule pre-population.
- `styles.css` — Watchlist button/badge/panel.

---

## Stage 6: Streaming Overlay (OBS / vMix)

**Frontend + small backend addition.**

Provide a live overlay page that streaming software can use as a browser source, plus a simple JSON endpoint for custom integrations.

### Browser source overlay

URL: `/display?court=A&overlay=true`

A transparent-background page showing only the current match on a given court — designed to be composited on top of a video feed.

#### What it shows

- Lower-third style layout: player names (SHIRO left, AKA right), live ippons, competition + phase label.
- Transparent background (CSS `background: transparent`) so OBS composites it over video.
- Animates in/out when a match starts or ends.
- Auto-updates via existing SSE stream.
- No upcoming matches, no chrome — just the active match scoreboard.

#### Design

- Reuses the `/display` route from Stage 2 — the `DisplayMode` wrapper checks the `overlay` query param and renders `DisplayOverlay` instead of `DisplayCourt`.
- Positioned at the bottom of the viewport (lower-third) by default, configurable via query param (`?position=top`).
- Large, high-contrast text with subtle drop shadow for readability over varied video backgrounds.
- Hides automatically when no match is running (avoids stale info on stream).

### JSON endpoint for custom integrations

`GET /api/viewer/court/:court/live` — returns the current running match for a specific court, or `null` if no match is live. Lightweight endpoint for vMix data sources, custom OBS plugins, or any non-browser integration.

#### Response

```json
{
  "court": "A",
  "status": "running",
  "competition": "Men's Individual",
  "phase": "Pool A",
  "sideA": { "name": "Suzuki", "dojo": "Oxford KC" },
  "sideB": { "name": "Yamamoto", "dojo": "London KC" },
  "ipponsA": ["M", ""],
  "ipponsB": ["", ""],
  "hansokuA": 0,
  "hansokuB": 0
}
```

### Tests

- JS test: `DisplayOverlay` renders current live match with names and ippons on transparent background.
- JS test: `DisplayOverlay` hides when no match is running.
- JS test: routing — `/display?court=A&overlay=true` dispatches to `DisplayOverlay`.
- Go test: `GET /api/viewer/court/A/live` returns current running match.
- Go test: `GET /api/viewer/court/A/live` returns `null` when no match is live.
- Manual: add as OBS browser source, score a match via admin, verify overlay updates in real time.

### Files to change

- `viewer.jsx` — New `DisplayOverlay` component. `DisplayMode` wrapper checks `overlay` param.
- `styles.css` — `.display-overlay` styles: transparent background, lower-third positioning, text shadow, enter/exit transitions.
- `internal/mobileapp/handlers_viewer.go` — New `GET /api/viewer/court/:court/live` endpoint.
- `internal/mobileapp/server.go` — Register the new route.

---

## Documentation Updates

Each stage must update relevant documentation before it can be considered complete.

### Per stage

- **Stage 1 (Timings)** — `CLAUDE.md`: add `PoolMatchDuration`/`PlayoffMatchDuration` to architecture notes. `README.md`: document new competition settings. `specs/openapi.yaml`: add new fields to competition create/update schemas.
- **Stage 2 (TV Display)** — `CLAUDE.md`: document `/display` route, `DisplayMode` component, display CSS conventions. `README.md`: add "Display Modes" section with single-court TV usage and URL format.
- **Stage 3 (Overview Display)** — `CLAUDE.md`: document `DisplayOverview` component. `README.md`: extend "Display Modes" section with overview usage.
- **Stage 4 (My Match)** — `CLAUDE.md`: document `myPlayerId` state flow, `?player=` URL param, activated stub. `README.md`: document participant self-identification and QR code URL format.
- **Stage 5 (Watchlist)** — `CLAUDE.md`: document watchlist localStorage schema and schedule filter integration. `README.md`: document coach watchlist feature.
- **Stage 6 (Streaming Overlay)** — `CLAUDE.md`: document `DisplayOverlay` component, `?overlay=true` param, and `/api/viewer/court/:court/live` endpoint. `README.md`: add "Streaming Integration" section with OBS/vMix setup instructions. `specs/openapi.yaml`: add the new viewer endpoint schema.

### General

- `CLAUDE.md` — Keep "Architecture" and "Common Pitfalls" sections current with new components, routes, and state patterns.
- `README.md` — Keep user-facing feature descriptions and usage instructions current.
- `specs/openapi.yaml` — Update whenever API request/response schemas change (Stage 1 only).

---

## Sequencing

```
Stage 1 (Timings) ─── improves data for ───> Stage 4 (My Match) ───> Stage 5 (Watchlist)

Stage 2 (TV Display) + Home Access ───> Stage 3 (Overview Display) ───> Stage 6 (Streaming Overlay)
```

- Stages 1, 2, and 4 can start independently.
- Stage 3 shares components with Stage 2 — natural to build together.
- Stage 5 extends Stage 4's infrastructure.
- Stage 6 builds on Stage 2's routing and display components.
- Each stage is independently shippable.
- Stages 1 and 6 require Go backend changes; all others are frontend-only.
- Every stage requires `make run-mobile` to rebuild (esbuild + Go embed).
