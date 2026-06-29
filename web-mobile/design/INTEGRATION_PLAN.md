# Aka/Shiro Distinction; Production Integration Plan

Status: **awaiting final go-ahead** on the live-strip interpretation (see §0).
Prototype: [aka-shiro-system.html](aka-shiro-system.html) (standalone, validated on desktop + 412px).
Doc: `DESIGN.md` Principle 2 + §4 "Aka/Shiro side treatment" already updated.

## 0. Decisions locked (from review)

| Decision | Choice | Source |
|---|---|---|
| White (Shiro) treatment | Framed white + 45° hatch | user review |
| Kanji 赤/白 | **Dropped**; romaji AKA/SHIRO + colour only | user review |
| Validated 6 surfaces | scoreboard ✓, legend ✓, detail ✓; editor/bracket/schedule fixed via **more colour area** | user review |
| Fonts | **Self-host** Anton + Archivo (OFL 1.1, Apache-compatible) | user + license verify |
| Live mark | **Remove the red liveness *styling* everywhere** (viewer + admin) | user (×2) |
| Red meaning | **Aka-side identity + danger** (two non-overlapping contexts) | user |
| Live-strip (admin) | **Keep** as navigation control, restyle neutral (`--accent`, no red); keep its `onClick`. | user confirmed |

## 1. Constraints verified against code (not assumed)

- No external font loads today; everything `//go:embed`-ed → **offline venue use**. CDN fonts would fail. → self-host.
- `--red`/`--red-soft` drives **15+ live-state rules** across every surface. Removing liveness styling touches all of them.
- `admin_shell.jsx:87` live-strip chips are **`<button onClick={onOpenScore}>`**; a navigation affordance, not just decoration.
- Project LICENSE → **MPL-2.0** (user is changing it; swap handled separately, out of this design scope). OFL 1.1 bundling is allowed under *any* host license incl. MPL-2.0; `.woff2`/`OFL.txt` are non-MPL third-party files in the Larger Work (MPL §3.3); no copyleft interaction. Ship each `OFL.txt`, don't reuse Reserved Font Name on subsets.

## 2. Token additions (`styles.css :root`)

Reuse existing where possible; add only what's missing. Names follow the app's convention.

```
--aka:        #c1121f;   /* = existing --red; alias for semantic clarity OR reuse --red directly */
--aka-soft:   #fde7e8;   /* = existing --red-soft */
--shiro-edge: #2a3346;   /* NEW; dark cool frame for white side */
--shiro-hatch:rgba(42,51,70,.07);  /* NEW; diagonal hatch tint */
```
Decision: prefer reusing `--red`/`--red-soft` over aliasing, to avoid token sprawl (DESIGN.md §9). Only `--shiro-edge` + hatch are genuinely new.

## 3. Self-hosted fonts

- Vendor `web-mobile/vendor/fonts/{anton,archivo}/*.woff2` + `OFL.txt` each.
- `@font-face` in styles.css; add to `--font-display` (Anton, hero/scoreboard numerals); body stays system `--font`.
- Confirm `//go:embed web-mobile/vendor` already covers it (it does; main.go:34).
- Subset if size matters; rename subset files to avoid Reserved Font Name.

## 4. Staged rollout (one screen per step, `make go/build` + browser check between)

1. **Tokens + fonts**; ✅ DONE. Vendored Anton (19KB) + Archivo (32KB, wght 100–900) woff2 + OFL.txt under `web-mobile/vendor/fonts/`; `@font-face` + `--font-impact`/`--font-strong`/`--shiro-edge`/`--shiro-hatch` tokens added. Existing `--font`/`--font-display` untouched (no visual change). Verified served from embedded binary (HTTP 200 `font/woff2`, OFL.txt served). Build green.
2. **Score editor** (`admin_competition.jsx` scoreboard mode / `.sc-board`); ✅ DONE.
   - Proposal C: 4-col side-by-side (>600px); colour edge-to-edge, names+tally centred facing a floating "vs", scoring buttons on unlabelled colour-coded rails. Side name stated once (centre chip, DRY).
   - ≤600px: single-column stack; each side = chip+name+tally, buttons full-width below. No overflow (verified via getBoundingClientRect: board 294 ⊂ container 328 @360px). `matchMedia` confirms breakpoint logic.
   - Misleading red dot removed. `.ipt-btn` gets 44px min under coarse pointers (DESIGN.md §6).
   - Shiro frame uses `--accent` navy (not the dropped `--shiro-edge`).
   - LESSON: headless `--screenshot` crops narrower than true render width → false "clipping". Trust getBoundingClientRect / matchMedia over screenshots for layout.
   - TODO: tap mode + card mode in the same component still use the old `.score-card`; left as-is (different interactions). Scoring MODAL (`admin_scoring_modal.jsx`) still pending; user asked to apply C there too.
3. **Bracket card** (`bracket.jsx` / `.bc-side`, `.bc-color-badge`); ✅ DONE.
   - Universal leading colour bar via `.bc-side--a/--b::before` (red / navy); winner = thicker bar (5px) + bold name (no accent-soft flood that fought the red side).
   - v1 side tints scoped `.bc-match--v1 .bc-side--a/--b` (red-soft / hatched white) so v2 (full-flood "now playing") and v3 (compact, transparent) keep their own backgrounds. Verified via computed-style probe.
   - `.bc-color-badge--shiro` changed flat grey → framed white (#fff + --accent border) to match the system.
   - `.bc-match { overflow:hidden }` so tints/bars clip to rounded corners.
   - NOTE: vitest is broken PRE-EXISTING in this worktree; `web-mobile/node_modules` has no `vitest` install (`ERR_MODULE_NOT_FOUND` loading vitest.config.js, fails before any test). Needs `npm ci` in web-mobile. Not caused by these changes.
4. **Pool + schedule rows** (`viewer.jsx` / `.pool-match-row`, `.vsched-item`); ✅ DONE.
   - Pool rows: each `__side` gets a tinted cell (red-soft Aka / hatched-white Shiro) + padding/radius.
   - vsched rows: side tint keyed off the badge via `:has(.vsched-item__color-badge--aka/--shiro)` so order can't desync colour; name-over-dojo block flow preserved.
   - ALL flat-grey Shiro badges (`#e8eaf0` ×5: bc/tw/vsched/pool/se, plus match-detail `#f0f0f0`) → framed white (#fff + --accent border). Zero grey shiro badges remain.
   - vitest now installed (`npm ci --legacy-peer-deps`; plain `npm ci` failed on eslint-plugin-react peer conflict). Full suite GREEN: 29 files / 609 tests.
5. **Display surfaces** (`display.jsx`); ✅ DONE. 3 components, all already per-court via `/display?court=X`. Tailored per surface (a red flood is wrong for OBS):
   a. **TvDisplay** (fullscreen per-court TV, `?court=A`); restructure to bold colored half-panels (approved C/D mock): Shiro left (white bar + hatch), Aka right (red flood + glow), centered real score `MK – M` (white/red), decision-suffix + fouls preserved. Drop LIVE badge; UP NEXT → tiny muted "↑ up next" note in header. Extract `.tvd-*` CSS classes.
   b. **StreamingOverlay** (OBS/vMix lower-third, `?court=A&overlay=1`); KEEP compact transparent lower-third (flood would obscure broadcast video); just align its existing white/red chips to framed-white + `--red` tokens.
   c. **LobbyDisplay/LobbyCard** (`?court=all` grid); side tints/chips at card scale.
   d. **DisplayModes entry points** (`viewer.jsx`); add a per-court "🎥 Shiaijo X streaming overlay" card linking `/display?court=X&overlay=1` (currently the overlay has NO UI entry; URL-only). One per shiaijo.
6. **Live-styling removal sweep**; ✅ DONE. Swept red→`--accent` on ALL liveness rules: `.dot--live` (pulse dot), `.bc-match--live`, `.sched-row--live`, `.vsched-item--live`, `.tw-match--live`, `.score-edit-row--live`, `.pool-matrix__cell--live`, `.bc-live` (● LIVE label), + the whole `.live-strip` block (kept as nav control, recoloured navy). One inline red in `admin_competition.jsx` ("Live" status text) → `--accent`. Verified: `.btn--danger` still `--red` (danger intact). Liveness now conveyed by motion (pulse) + navy, never red. Red = Aka + danger only. 895 tests green.
7. **Tests + a11y**; update any vitest snapshots; verify side distinction has non-colour redundancy (label/position/badge) for colour-blind + glare.

### Schedule waza letters (user request, mid Step 4); ✅ DONE
Requirement: "waza letters must always be present if there's a score" + "loser waza too".
- Root cause: `normalizeMatch` built `score` (winner-only `ippons`) from bracket `scoreA/scoreB` but never populated per-side `ipponsA/ipponsB`. When both per-side arrays were empty, `formatIpponsScore` fell back to numbers (`2–1`).
- Upstream fix (`api_serializers.jsx`): recover BOTH sides' letters into `ipponsA/ipponsB` from `scoreA/scoreB` (each is `formatScore(Ippons)` server-side = loss-free, unlike winner-only `score.ippons`). Only fills when absent so server arrays win.
- Defensive display fix (`bracket.jsx formatIpponsScore`): if the numeric fallback ever fires, prefer winner `score.ippons` letters over the count.
- INVARIANT respected (user note "some paths need counts, others letters"): counts still come from `score.winnerPts/loserPts` + array `.length`; `formatIpponsScore` output is display-only (grep-verified no caller parses it as a number).
- Tests: +3 api.test (both-sides recovery, server-array precedence, hansoku-strip both sides), +2 score_display (letters-preferred fallback, dot-placeholder→count). Suite 895 green.

## 5. Out of scope / follow-ups

- Excel output side-colours (separate renderer in `internal/`).
- `web/` legacy bracket generator (Bootstrap; DESIGN.md says don't import mobile patterns).
