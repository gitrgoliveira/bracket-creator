# Phase 2 — Court-local display hub (BroadcastChannel)

> Part of the **mp-gpra** flaky-wifi/internet hardening initiative. Phase 1 (PR #321)
> hardened the existing client/server flow (durable offline write queue, SSE resync,
> silence watchdog, terminal-failure surfacing, tamper hardening). Phase 2 keeps a
> **shiaijo's TV display alive and updating during a link outage**, on the same laptop,
> by making the operator tab the court's local data hub.
>
> **Tracking bead:** `mp-9ukk` (related to `mp-gpra`).

## Goal

On one laptop driving an HDMI display, the **operator tab** (admin scoring) and the
**display tab** (`display_scoreboard.jsx` / `TvDisplay`) keep working **together with no
server connectivity** — the operator's scores show on the TV board in real time, the board
cold-starts from the operator tab, and everything reconciles to the server when the link
returns.

Root of trust: each shiaijo owns and controls its own display (decided model). The operator
tab is authoritative for *its own court*; the display is a projection of it.

## In scope (v1)

1. **Operator tab as the court's local data hub.** It already holds the court's
   `competitions` in memory (`app.jsx` → `fetchCompetitions`/`fetchCompetitionDetails`).
2. **BroadcastChannel transport** (same-origin, same-browser → same laptop):
   - **Patches:** when a match write is confirmed *or* durably queued, publish the match
     patch.
   - **Bootstrap handshake:** a display tab on load posts a snapshot request; the operator
     tab(s) reply with their court slice of `competitions` so the display cold-starts
     **from the operator tab, not the server**.
3. **Display consumes + merges** through the SAME path `app.jsx` already uses for SSE
   `match_updated` (reuse `mergeMatchPatch`/`patch.jsx`) — one merge implementation, no fork.
4. **3-state discreet link indicator** on the board (replaces the "RECONNECTING" pill — see
   below).
5. **Reconcile-on-reconnect:** server SSE remains authoritative; server patches override the
   local optimistic ones, ordered by seq/rev.

## Non-goals / accepted limits (write them down so they aren't mistaken for bugs)

- **Operator-tab cold-start stays broken.** If the operator tab itself reloads/crashes
  offline it can't bootstrap (no app-shell/data cache without a service worker, which is
  vetoed). Then the display goes **red** and the court is down until the link returns. The
  guarantee is precisely: *the court keeps running through an outage **as long as the operator
  tab stays open***.
- **Knockout advancement = option A (placeholder).** The next-round slot is computed
  server-side (`engine.propagateBracketWinner`); offline it stays a `"Winner of …"`
  placeholder until the server advances it. The **current/running** match still shows live.
  Option B (bounded client-side promotion) is deferred — see below.
- **Cross-court data is stale** (other courts' results, global standings) — as of the
  operator tab's last sync. The board is per-court, so this only affects the secondary
  "other courts in progress" wayfinding strip.
- **Same machine only.** BroadcastChannel is same-origin, same-browser. No cross-laptop sync
  (that would be the local-server / merge-protocol path — out of scope).

## Architecture

```
 ┌─ one laptop, one browser ───────────────────────────────────────────┐
 │  Operator tab (admin)                    Display tab (TvDisplay)      │
 │  app.jsx owns competitions  ──┐          app.jsx (mode="display")     │
 │  api_client.recordScore()     │          owns its own competitions    │
 │      │ on confirm/queue       │  Broadcast │  copy, renders board      │
 │      ▼                        │  Channel   │      ▲                    │
 │  publish match patch ─────────┼────────────┼──────┘ merge via          │
 │  answer snapshot request ◀────┼────────────┼─ post "req snapshot"      │
 │                               │            │  (on display load)        │
 └───────────────────────────────┴────────────┴──────────────────────────┘
        both also hold their own EventSource → server (when reachable)
```

### Seam (NOT "only admin_scoring_* + display_scoreboard")

Verified in code: `display_scoreboard.jsx` is a pure leaf rendering a `competitions` prop;
`app.jsx` owns the data + the SSE `match_updated` merge (~app.jsx:801, display-mode immediate
patch ~816); `admin_scoring_*` is presentational and writes via `api_client.recordScore`.
Therefore:

- **Publish side → `api_client`** (where the write + its result live). Broadcast the match
  patch on a confirmed 2xx *and* on a durable enqueue (`{queued:true}`). Also register a
  snapshot responder.
- **Consume side → `app.jsx`** (display mode): subscribe to the channel; on a patch, merge
  via the existing SSE merge path; on load, post a snapshot request and, if the server fetch
  fails, bootstrap from the operator tab's reply.
- **New small module `court_bridge.jsx`** to encapsulate the BroadcastChannel (open/close,
  message schema, origin tagging, recency tracking) so it's unit-testable and DRY.
- **`display_scoreboard.jsx`:** only change is the link indicator (below) — it keeps
  rendering whatever `competitions` it's given.

### Message schema (versioned)

```
{ v: 1, type: 'patch'|'snapshot-req'|'snapshot', origin: <tabId>,
  court: 'A', compId: '…', payload: <matchPatch | competitionsCourtSlice>, rev: <seq> }
```
- Tag `origin` to ignore self-echo. `rev` reuses the per-match/SSE seq so server truth
  (higher authority) wins on reconcile.
- Snapshot payload = the **court slice only** (that court's competitions), not the whole
  tournament — bounds payload size.

### Reconciliation / ordering

- Broadcast patches are **optimistic, court-scoped**. SSE patches are authoritative.
- Merge keeps the highest `rev` per match; a server patch with `rev ≥ local` overwrites the
  optimistic one. If the server *rejects* a write (the phase-1 terminal-failure path), the
  operator tab corrects + re-broadcasts; the display follows.
- No echo loops: display only consumes; operator only publishes/answers.

## Link-status indicator — discreet 3-state dot (replaces the pill)

**Requirement:** NOT a "connected/RECONNECTING" text pill. A **very discreet small circle**
in the board header status corner — green / amber / red.

| State | Color | Meaning | Condition |
|---|---|---|---|
| **Live** | 🟢 green | SSE to server is up; board is server-fresh | `sseConnected` |
| **Court-local** | 🟡 amber | server link down, but updating from the operator tab (this court is current) | `!sseConnected && broadcastFreshWithin(Ns)` |
| **Stale** | 🔴 red | no server **and** no operator feed — data may be outdated | otherwise |

Design constraints:
- **Discreet:** a small dot (~1.2–1.6vh) in the existing header status corner; **no text
  label** on screen (the current `RECONNECTING` pill is removed). Static, **no pulse**
  (pulse + navy is reserved for the running-match state per DESIGN.md — this is a status dot,
  not an activity indicator).
- **Colors from DESIGN.md tokens** (green = a success/ok token, amber = `--warn`, red =
  `--danger`). Verify contrast against the **white** TV board; the red must be positionally
  unambiguous vs the AKA player red (it lives in the header status corner, tiny, and only
  appears when stale). Final palette/placement to pass `/impeccable` during implementation.
- **Accessibility:** the dot carries an `aria-label`/`title` ("Live" / "Court-local — server
  offline" / "Stale — no updates"), so it's discreet visually but labelled for assistive tech
  and for operators who hover on a non-TV screen.
- **Derivation is a pure exported function** `deriveLinkState({sseConnected, lastBroadcastAt, now, freshnessMs})` → `'live'|'local'|'stale'` so it's unit-testable (vitest doesn't mount the board).

Wiring: `app.jsx` computes `linkState` from its SSE `connected` + the bridge's last-broadcast
recency and passes `linkState` (replacing the boolean `connected`) down through `display.jsx`
→ `TvDisplay`. The four header sites in `display_scoreboard.jsx` that currently render the
`RECONNECTING` pill render the dot instead.

## Knockout advancement — option A now, B deferred

- **A (v1):** show the current/running match live + pool progression; knockout next-round
  slots needing server promotion stay placeholders until the server advances them. Zero
  bracket logic on the client → zero integrity risk.
- **B (deferred):** bounded client-side promotion ("winner of (r,m) → round r+1, slot ⌊m/2⌋,
  side by parity") for clean single-elim, **degrading to placeholder** for byes / multi-page
  subdivision (`maxPlayersPerTree=16`) / court-spanning / daihyōsen-kachinuki. Only if
  operators actually need the next *knockout* pairing during outages. Risk: a second
  (partial) promotion implementation must stay consistent with `engine.propagateBracketWinner`
  or it diverges; that's why it's deferred and fallback-guarded.

## Security / trust

- BroadcastChannel is same-origin, same-browser → no new network surface, no cross-machine
  exposure. The snapshot is the operator tab's own already-authorized data. Operator = root
  of trust for the court (decided). No untrusted input crosses the channel.

## Edge cases

- **Multiple display tabs** on one laptop → each requests its own snapshot; broadcast patches
  fan out to all (BroadcastChannel is multi-listener). Fine.
- **Multiple operator tabs / courts on one laptop** → scope snapshot replies by `court`; a
  responder only answers for courts it has. (Unusual but handle by court-scoping.)
- **Operator tab closes** → display stops receiving → after `freshnessMs` the dot goes red.
- **No BroadcastChannel support** (very old browser) → bridge is a no-op; behavior falls back
  to today's SSE-only (graceful degrade; no regression).
- **Snapshot race** (display loads before operator answers) → board shows `Loading…` until the
  first snapshot or SSE arrives (same as today's empty state).

## Files touched

- `web-mobile/js/court_bridge.jsx` (new) — BroadcastChannel wrapper + `deriveLinkState`.
- `web-mobile/js/api_client.jsx` — publish patch on confirm/enqueue; snapshot responder.
- `web-mobile/js/app.jsx` — display-mode consumer: subscribe, merge via existing SSE path,
  bootstrap-from-broadcast fallback, compute `linkState`.
- `web-mobile/js/display.jsx` — thread `linkState` instead of boolean `connected`.
- `web-mobile/js/display_scoreboard.jsx` — replace the `RECONNECTING` pill (4 header sites)
  with the discreet 3-state dot.
- Tests: `court_bridge` + `deriveLinkState` (vitest); browser/Playwright two-tab test.

## Testing & acceptance

**Unit (vitest):** `deriveLinkState` truth table; bridge publish/subscribe/snapshot with a
mocked `BroadcastChannel`; self-echo ignored; rev ordering (server > local).

**Browser (Playwright, two same-origin tabs):**
1. Operator scores a match with the **server reachable** → display updates; dot **green**.
2. Cut the server (block `/api/events` + writes) → operator scores → **display still updates
   via broadcast**; dot **amber**; operator shows the phase-1 pending banner.
3. **Cold-load the display tab while the server is down** but the operator tab is open →
   display **bootstraps from the operator tab** and renders the court; dot **amber**.
4. Close the operator tab (server still down) → after `freshnessMs` the dot goes **red**.
5. Restore the server → queued writes flush → SSE delivers authoritative state → both tabs
   reconcile; dot back to **green**; no double-apply (rev ordering).
6. Knockout: completing a bout offline shows the bout done on the display; the next-round
   pairing stays a placeholder until the server advances it (option A).

**Gates:** `make go/test` (no Go change expected), full JS suite + eslint + build; `/impeccable`
on the dot; real browser screenshot of all three dot states for the PR.

## Open decisions for the maintainer

1. **One PR or follow-up?** Recommend a **follow-up PR** (still `Refs mp-gpra`) so review-clean
   PR #321 lands now; this gets its own clean review surface.
2. **Knockout: ship A only, or A + deferred B?** Recommend **A only** for v1.
3. **Feature flag?** Default **on + graceful no-op** when BroadcastChannel is unavailable
   (it's additive and degrades safely), unless you want it behind an env flag for the first
   event.
