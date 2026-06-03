# Viewer redesign — implementation plan (mp-8jbo)

## Decisions (locked by operator)
- **Group 1 — unify:** A · one card with a **phase strip** (Pools ✓ / Knockout ●).
- **Group 2 — standings:** A · **one combined team table**.
- **Group 3 — grid:** A · **2 columns in landscape, 1 in portrait** (cap at 2).
- **Group 4 — quick-wins:** label-person, team-count, winner-tick, results-order. (Dropped: bye-final, queue-yours, ippon-zero, dark-mode.)
- Group 4 Awards redesign + the two-3rd-places fix were NOT among the picks — see "Open" below.

## ⚠️ Grounding correction — some findings were TEST-SEED artifacts, not app bugs
Verified against real code, not the seeded demo (whose `setup_tournament.py` wrongly set `teamSize=1` for individuals and loaded 36 loose individuals into a 5-person team comp with no lineups):

- Real admin forces `teamSize: kind === "team" ? teamSize : 0` (`web-mobile/js/admin_setup.jsx:515`; `internal/state/models.go:265`). So **real individual comps have teamSize=0**.
- `isTeam = c.kind === "team" || c.teamSize > 0` (`viewer.jsx:2293`). With teamSize=0, individual pools already render the **canonical** header `# Player W L D PW PL` (`viewer.jsx:2332`). 
- Therefore **NOT real bugs** (seed-only): "individual pools show IV/IL", "PW/PL all zero", and the **"Individual · 1-person"** eyebrow (`c.teamSize ? …`, falsy at 0).
- **team-count ("36 teams")**: unverified — the seed put 36 individuals (no teams) into a team comp. Needs a real team comp (with lineups) to confirm whether `c.players.length` counts teams or members before deciding it's a bug.

➡️ Action: build a *proper* team competition (real 5-person teams + lineups) to re-validate the team-side items.

## Genuinely real work (matches the picks)

### G1 — Unify pools+knockout into one card (phase strip)
- Linkage exists: playoffs comp carries **`SourceCompID`** → mixed source (`internal/state/models.go:251`; set by `POST /competitions/:id/playoffs`).
- Frontend (`viewer.jsx` `ViewerHome` comp list, ~line 790-835):
  - Group each mixed/pools comp with its linked `sourceCompID` playoff into **one card**; render a phase strip (Pools done/live + Knockout pending/live/done) from the two comps' statuses + progress.
  - **Hide** standalone playoff cards and any `status==="setup"`/0-player playoff shells from spectators.
  - Tapping opens the unified competition; Pools tab → source comp, Bracket tab → playoff comp.
- Suppress the **"UP NEXT" preview-bracket placeholders** on a completed pools comp (`ViewerCompetition` overview; preview bracket already flagged `bracket.preview`).
- Files: `web-mobile/js/viewer.jsx` (+ maybe `api_serializers.jsx` to expose `sourceCompID`/grouping), `styles.css` (phase strip).

### G2 — Combined team standings: add IT + wire qualifier highlight + tooltips
- Team header today: `# Team W L T IV IL PW PL` (`viewer.jsx:2331`) — **missing IT** (individual ties) the handbook requires (W→L→T→IV→IL→**IT**→PW→PL). Add an IT column (`s.individualTies`); confirm the Go standings serializer emits it.
- **Qualifier highlight**: `.pool__table tr.advancing` (green + ▲) **exists in CSS (styles.css:1301) but is never applied** in JSX. Wire `className="advancing"` onto the top `poolWinners` rows; add an advance-line divider after the cut.
- Header **tooltips**: wrap W/L/T/IV/IL/IT/PW/PL headers with the existing glossary `Term` so the jargon is decodable.
- Individual table: already correct — **no change**.
- Files: `viewer.jsx` (PoolsTab render ~2328-2375), possibly Go standings serializer for IT, `styles.css`.

### G3 — Pools grid: 2-up landscape / 1-up portrait
- Pools tab container is a single-column flex (`viewer.jsx:2310`). Switch to the existing `.pools-grid` (`styles.css:1230`).
- CSS: `@media (orientation: portrait){ grid-template-columns: 1fr } @media (orientation: landscape){ grid-template-columns: repeat(2,1fr) }` (cap 2). Keep a sane min so a tiny landscape phone still reads.
- Scope: pools first (the win). Competition list / watched-matches grid optional — confirm.
- Files: `viewer.jsx` (container), `styles.css`.

### G4 — quick-wins (selected)
- **label-person** (defensive): guard `c.teamSize > 1 ? …` so a stray teamSize=1 can't print "1-person" (`viewer.jsx:803`). (Already correct for real teamSize=0 data; cheap hardening.)
- **team-count**: only if validation on a real team comp confirms `c.players.length` ≠ team count; then count teams. (`viewer.jsx:805`) — **gated on re-validation**.
- **winner-tick**: bracket winner already gets `bc-side--winner` styling (`bracket.jsx:140`, styled `styles.css:1050`); add an explicit ✓ glyph in `PlayerLine` for an unambiguous, weight-independent marker.
- **results-order**: sort per-competition "Recent Results" reverse-chronological (verify current sort in `ViewerCompetition` overview).

## Open (need an operator call)
- **Awards**: the champion-hero redesign and the **two-3rd-places** correctness fix were not in the picks. The two-3rd fix is a real correctness issue (current Awards shows 1st/2nd/3rd/4th; kendo bracket = two joint 3rds) and likely needs a backend placings change too. Include or defer?
- Build a real team comp for team-side re-validation? (recommended)

## Sequencing
1. (validation) Stand up a real team competition with lineups; re-check team-count + team standings.
2. G3 (smallest, isolated CSS+container) → G2 (highlight/IT/tooltips) → G4 (winner-tick, results-order, label guard) → G1 (largest; card grouping).
3. `make go/build` + browser verification (landscape & portrait, individual + team) after each.
