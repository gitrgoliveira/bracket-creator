// viewer_watchlist_core.jsx: watchlist hooks + normalization logic extracted
// from viewer.jsx (mp-pxxc step 2). Pure file split: no behaviour change.
//
// Sharing model: esbuild compiles each .jsx to dist/.js individually (no bundle);
// the server rewrites `/dist/X.jsx` → the compiled `X.js`, so ES `import "./X.jsx"`
// specifiers resolve in the browser. viewer.js is the SOLE viewer entry script in
// index.html and imports this module: do NOT give it its own <script type="module">
// tag, or the browser fetches it under a second URL (.js?v=N vs .jsx) and evaluates
// it twice (double-load; same class as mp-zd1v).
//
// Cycle note: viewer.jsx imports from this file and re-exports every symbol
// here (plus window.* assignments) so the public surface of viewer.jsx is
// unchanged. viewer_watchlist.jsx (panel UI) continues to read the watchlist
// helpers via window.* lazy reads: those assignments still live in viewer.jsx.

const { useState } = React;

// --- Slice 4 helpers: "Find my matches" + Watchlist (FR-020 / FR-022 / FR-024) ---

// Pull a participant id off a match in either the canonical shape
// (`m.sideA.id` / `m.sideB.id` as produced by api_serializers.jsx) or
// the flat shape (`m.sideAId` / `m.sideBId`) some tests/fixtures use.
// Returns the two ids as a [aId, bId] tuple, either of which may be "".
export function matchParticipantIds(m) {
  if (!m) return ["", ""];
  const aId = (m.sideA && typeof m.sideA === "object" ? m.sideA.id : null) || m.sideAId || "";
  const bId = (m.sideB && typeof m.sideB === "object" ? m.sideB.id : null) || m.sideBId || "";
  return [aId, bId];
}

// Pull the two display names off a match, again tolerant of both shapes.
export function matchParticipantNames(m) {
  if (!m) return ["", ""];
  const aName = (m.sideA && typeof m.sideA === "object" ? m.sideA.name : m.sideA) || "";
  const bName = (m.sideB && typeof m.sideB === "object" ? m.sideB.name : m.sideB) || "";
  return [aName, bName];
}

// Check whether a participant object `p` refers to the followed player. An
// id decides whenever BOTH carry one (sameCompetitor's rule, applied by
// hand here rather than delegated so the pre-existing case-INSENSITIVE name
// compare -- team-match sub-players or legacy fixtures key by display name
// only, in any case -- is preserved); a mixed pair (one has an id, the
// other doesn't) is never guessed at by name.
export function isFollowedPlayer(p, followed) {
  if (!p || !followed) return false;
  const pId = (typeof p === "object" ? p.id : null) || "";
  const pName = (typeof p === "object" ? p.name : p) || "";
  const fId = followed.id || "";
  const fName = followed.name || "";
  if (pId && fId) return pId === fId;
  if (!pId && !fId) return !!pName && !!fName && pName.trim().toLowerCase() === fName.trim().toLowerCase();
  return false;
}

// sideIsWatched: does one id/name pair belong to the watched set? `watched`
// is `{ids, names}` (buildWatchedSets below) -- ids from every watched entry
// that carries one, lowercased names from every watched entry that DOESN'T.
// THE single case-insensitive side predicate: decides by the CHECKED pair's
// OWN id presence -- an id-carrying pair matches only a watched id, never
// falling through to a name hit. A pooled "check id OR name independently"
// shape let watching Sato of Tokyo also highlight/list/on-deck Sato of
// Osaka's rows whenever the id check missed; this is the fix and the one
// place it lives. Every case-insensitive watch surface (highlighting,
// upcoming-list, on-deck banner, running/recent filtering) must consult
// this on top of `buildWatchedSets`'s sets, not a hand-rolled equivalent,
// or the surfaces can disagree on the same id-less side. `watched` may also
// be a legacy empty array ([]) from callers with no watchlist concept
// (admin console): the shape guard below reads that as "nothing watched"
// rather than throwing.
export function sideIsWatched(id, name, watched) {
  if (!watched) return false;
  if (id) return !!(watched.ids && typeof watched.ids.has === "function" && watched.ids.has(String(id)));
  return !!name && !!(watched.names && typeof watched.names.has === "function" && watched.names.has(name.trim().toLowerCase()));
}

// mp-xhaa: is participant `p` in the watched set? Thin wrapper over
// sideIsWatched for callers that already have a resolved {id,name} record
// (or a bare name string) rather than the id/name pair split out. Drives
// highlighting across bracket, pool, and schedule surfaces for EVERY watched
// player (not just one followed player).
export function isPlayerWatched(p, watched) {
  if (!p) return false;
  const id = (typeof p === "object" ? p.id : null) || "";
  const name = (typeof p === "object" ? p.name : p) || "";
  return sideIsWatched(id, name, watched);
}

// buildWatchedSets: the {ids, names} shape isPlayerWatched consumes, from a
// resolved watched-player list (resolveWatchedPlayers output, or any
// {id,name} list). An entry WITH an id contributes to `ids` only; an entry
// WITHOUT one contributes its lowercased name to `names` only -- the two
// sets are mutually exclusive per entry, matching sameCompetitor's "id
// decides when the record carries one" rule instead of pooling everything
// into one lookup either check could satisfy.
export function buildWatchedSets(resolvedWatched) {
  const ids = new Set();
  const names = new Set();
  (Array.isArray(resolvedWatched) ? resolvedWatched : []).forEach((p) => {
    if (!p) return;
    if (p.id) ids.add(String(p.id));
    else {
      const n = (p.name || "").trim().toLowerCase();
      if (n) names.add(n);
    }
  });
  return { ids, names };
}

// LocalStorage keys for FR-020 / FR-024. Centralised so the deep-link
// handler (T114) writes the same keys the panels read.
const LS_MY_PLAYER_ID = "bc_my_player_id";
const LS_MY_PLAYER_NAME = "bc_my_player_name";
const LS_WATCHLIST = "bc_watchlist";

export const WATCHLIST_MAX = 50;

// ---------------------------------------------------------------------------
// mp-xhaa: Unified watchlist: polymorphic entries + primary selection
// ---------------------------------------------------------------------------
//
// The watchlist absorbs the old single "followed player". Entries are now
// polymorphic:
//   - player: { type: "player", id, name, dojo }
//   - dojo:   { type: "dojo", dojo }   (expands to all current roster members)
//
// A single entry is *implicitly* primary (gets the hero card + chime). With
// ≥2 entries the user may pin exactly one as primary; if none is pinned there
// is no hero and no chime (critique decision: avoid the alert storm). The pin
// is stored as an `entryKey` string, decoupled from list order.

// entryKey: stable identity for an entry, used for the pin pointer, React
// keys, and dedup. Player keys are id-based, dojo keys are name-based. Returns
// "" for anything unrecognisable so callers can filter it out.
export function entryKey(entry) {
  if (!entry || typeof entry !== "object") return "";
  if (entry.type === "dojo") return entry.dojo ? "dojo:" + entry.dojo : "";
  const id = entry.id != null ? String(entry.id) : "";
  return id ? "player:" + id : "";
}

// normalizeWatchlistEntry: coerce a raw stored/added value into a canonical
// entry, or null if it carries no usable identity. Legacy entries (pre-merge
// `{id,name,dojo}` with no `type`) are upgraded to player entries. This is the
// single choke point that lets dojo entries survive a round-trip through
// localStorage (the old useWatchlist dropped anything without an `id`).
export function normalizeWatchlistEntry(x) {
  if (!x || typeof x !== "object") return null;
  if (x.type === "dojo") {
    const dojo = (x.dojo != null ? String(x.dojo) : "").trim();
    return dojo ? { type: "dojo", dojo } : null;
  }
  // Explicit player OR legacy (no type): both keyed by id.
  const id = x.id != null ? String(x.id) : "";
  if (!id) return null;
  return { type: "player", id, name: x.name != null ? String(x.name) : "", dojo: x.dojo != null ? String(x.dojo) : "" };
}

// normalizeWatchlist: normalize every entry, drop the unusable ones, dedup by
// entryKey (first occurrence wins), and cap at WATCHLIST_MAX. Tolerant of a
// non-array argument (returns []), so it doubles as the storage guard.
export function normalizeWatchlist(arr) {
  const out = [];
  const seen = new Set();
  (Array.isArray(arr) ? arr : []).forEach((x) => {
    const e = normalizeWatchlistEntry(x);
    if (!e) return;
    const k = entryKey(e);
    if (!k || seen.has(k)) return;
    seen.add(k);
    out.push(e);
  });
  return out.slice(0, WATCHLIST_MAX);
}

// migrateWatchlistOnLoad: fold the legacy single "followed player"
// (bc_my_player_id / bc_my_player_name) into the watchlist exactly once.
// Returns { list, migrated }:
//   - list: the normalized watchlist with the legacy player injected at the
//           FRONT iff it isn't already present (dedup by id).
//   - migrated: true when a legacy player was actually injected, so the caller
//           knows to persist and then delete the legacy keys (the deletion is
//           what makes this one-time: a second load sees no legacy keys).
// Pure and idempotent: calling it again with the legacy id already in the list
// is a no-op (the dedup in normalizeWatchlist absorbs it).
export function migrateWatchlistOnLoad(rawWatchlist, legacyId, legacyName) {
  const base = normalizeWatchlist(rawWatchlist);
  const id = legacyId != null ? String(legacyId).trim() : "";
  if (!id) return { list: base, migrated: false };
  const already = base.some((e) => e.type === "player" && e.id === id);
  if (already) return { list: base, migrated: false };
  const injected = [{ type: "player", id, name: legacyName != null ? String(legacyName) : "", dojo: "" }, ...base];
  return { list: normalizeWatchlist(injected), migrated: true };
}

// addPlayerToWatchlist: append a player entry (dedup by id), returning the
// new list (or the original unchanged when the player is missing or already
// watched). Single source of truth for the player-entry shape + dedup rule,
// shared by the home panel and its picker.
export function addPlayerToWatchlist(watchlist, p) {
  if (!p || !p.id) return watchlist;
  if (watchlist.some((e) => e.type === "player" && e.id === p.id)) return watchlist;
  return [...watchlist, { type: "player", id: p.id, name: p.name || "", dojo: p.dojo || "" }];
}

// resolveEntryPlayerIds: the set of roster player ids a single entry covers.
// A player entry is itself (even if not in the roster: a stale watch still
// filters matches by id). A dojo entry expands to every CURRENT roster member
// of that dojo, so late registrations are auto-included.
export function resolveEntryPlayerIds(entry, roster) {
  if (!entry) return [];
  if (entry.type === "dojo") {
    return (Array.isArray(roster) ? roster : [])
      .filter((p) => p && p.id && p.dojo === entry.dojo)
      .map((p) => String(p.id));
  }
  return entry.id ? [String(entry.id)] : [];
}

// resolveWatchedPlayers: expand the whole watchlist to a flat, deduped list of
// player records (preferring the live roster record so check-in state and the
// canonical name come through). Dojo entries expand to their current members.
// This single resolved list feeds the schedule filter (watchedIds), the
// highlight Set, and the alert hook: they must all agree on who is watched.
export function resolveWatchedPlayers(watchlist, roster) {
  const rosterById = new Map((Array.isArray(roster) ? roster : []).filter((p) => p && p.id).map((p) => [String(p.id), p]));
  const out = [];
  const seen = new Set();
  const push = (id, fallback) => {
    const key = String(id);
    if (!key || seen.has(key)) return;
    seen.add(key);
    const rec = rosterById.get(key);
    out.push(rec ? { ...rec, id: key } : { id: key, name: (fallback && fallback.name) || "", dojo: (fallback && fallback.dojo) || "" });
  };
  normalizeWatchlist(watchlist).forEach((entry) => {
    if (entry.type === "dojo") {
      resolveEntryPlayerIds(entry, roster).forEach((id) => push(id, null));
    } else {
      push(entry.id, entry);
    }
  });
  return out;
}

// effectivePrimaryKey: which entry (by entryKey) is currently primary.
//   0 entries  → null (nothing to be primary).
//   1 entry    → that entry, implicitly (no pin UI is shown for a lone entry).
//   ≥2 entries → the pinned entry IF it still exists, else null (no hero, no
//                chime). A stale pin (pinned entry was removed) resolves to
//                null rather than silently promoting another entry.
export function effectivePrimaryKey(watchlist, pinnedKey) {
  const list = normalizeWatchlist(watchlist);
  if (list.length === 0) return null;
  if (list.length === 1) return entryKey(list[0]);
  if (!pinnedKey) return null;
  return list.some((e) => entryKey(e) === pinnedKey) ? pinnedKey : null;
}

// findPrimaryEntry: the primary entry object (or null), per effectivePrimaryKey.
export function findPrimaryEntry(watchlist, pinnedKey) {
  const key = effectivePrimaryKey(watchlist, pinnedKey);
  if (!key) return null;
  return normalizeWatchlist(watchlist).find((e) => entryKey(e) === key) || null;
}

// buildPrimaryNextMatch: the hero match for the primary entry: the nearest
// non-completed match involving the primary (a player → just them; a dojo →
// any current member), ordered running-first then by scheduledAt so the hero
// surfaces a live match before a merely-scheduled one. Callers pass match
// lists already filtered through hasBothSides (as the home page does): this
// helper stays free of the window.hasBothSides proxy so it is unit-testable
// in isolation.
export function buildPrimaryNextMatch(primaryEntry, roster, allMatches) {
  if (!primaryEntry) return null;
  const ids = new Set(resolveEntryPlayerIds(primaryEntry, roster));
  if (ids.size === 0) return null;
  const list = Array.isArray(allMatches) ? allMatches : [];
  const pending = list.filter((m) => m && m.status !== "completed");
  // bc-pnum (HIGH regression fix): the primary
  // entry always carries a real id (resolveEntryPlayerIds only ever returns
  // roster-backed ids), so a match side with NO id is a MIXED pair and must
  // never be guessed at by name -- sameCompetitor's rule. A removed name
  // fallback used to activate whenever this id pass found nothing, matching
  // ANY pending match whose side's name happened to equal a current
  // member's roster name (or, for a player entry, the follower's own
  // name), regardless of whether that side carried an id. On a legacy/
  // id-less roster this could name the follower as their own opponent
  // ("Alice ... vs Opponent: Alice", reported live) or surface a dojo-mate's
  // unrelated match. Removed outright: a roster whose matches predate id
  // persistence now shows no hero card rather than a wrong one.
  const mine = pending.filter((m) => {
    const [a, b] = matchParticipantIds(m);
    return (a && ids.has(a)) || (b && ids.has(b));
  });
  mine.sort((a, b) => {
    const ao = a.status === "running" ? 0 : 1;
    const bo = b.status === "running" ? 0 : 1;
    if (ao !== bo) return ao - bo;
    return (a.scheduledAt || "99:99").localeCompare(b.scheduledAt || "99:99");
  });
  return mine[0] || null;
}

// checkedIn=true wins if any check-in-enabled competition has the player checked in.
export function buildRoster(competitions) {
  const map = new Map();
  (competitions || []).forEach((c) => {
    (c.players || []).forEach((p) => {
      if (!p || !p.id) return;
      const checkedIn = !!c.checkInEnabled && !!p.checkedIn;
      const existing = map.get(p.id);
      if (!existing) {
        map.set(p.id, { ...p, checkedIn });
      } else if (checkedIn && !existing.checkedIn) {
        map.set(p.id, { ...existing, checkedIn: true });
      }
    });
  });
  return Array.from(map.values());
}

// Hook: watchlist (array of polymorphic player/dojo entries) backed by
// localStorage. Defends against malformed JSON in storage (rare, but a corrupt
// key shouldn't crash the viewer for everyone using that browser profile), and
// folds the legacy single "followed player" keys into the list once on first
// load (mp-xhaa migration).
export function useWatchlist() {
  const [list, setList] = useState(() => {
    if (typeof window === "undefined") return [];
    let raw = null;
    try {
      const stored = window.localStorage.getItem(LS_WATCHLIST);
      raw = stored ? JSON.parse(stored) : [];
    } catch (_e) {
      raw = [];
    }
    // mp-xhaa: fold the legacy single "followed player" into the list once,
    // then delete the legacy keys so the migration never repeats.
    let legacyId = "", legacyName = "";
    try {
      legacyId = window.localStorage.getItem(LS_MY_PLAYER_ID) || "";
      legacyName = window.localStorage.getItem(LS_MY_PLAYER_NAME) || "";
    } catch (_e) { /* storage unavailable */ }
    const { list: migrated, migrated: didMigrate } = migrateWatchlistOnLoad(raw, legacyId, legacyName);
    // Persist the migrated list; track whether the write actually landed.
    // didMigrate=false means there was nothing to fold in, so the legacy keys
    // (if any) are safe to clear without a fresh write.
    let persisted = !didMigrate;
    if (didMigrate) {
      try {
        window.localStorage.setItem(LS_WATCHLIST, JSON.stringify(migrated));
        persisted = true;
      } catch (_e) { /* keep the legacy keys as a fallback: see below */ }
    }
    // Clear the legacy keys ONLY once the migrated list is durably written.
    // If the write failed (e.g. QuotaExceededError), leave them in place so the
    // followed player isn't silently lost and migration safely retries on the
    // next load. removeItem frees space, so it can succeed even when setItem
    // threw: gating on `persisted` prevents that asymmetric loss.
    if ((legacyId || legacyName) && persisted) {
      try {
        window.localStorage.removeItem(LS_MY_PLAYER_ID);
        window.localStorage.removeItem(LS_MY_PLAYER_NAME);
      } catch (_e) { /* storage unavailable */ }
    }
    return migrated;
  });
  const persist = (next) => {
    // Capture the normalized value from inside the updater so the LS write
    // can happen outside (side effects must not live in state updaters).
    // Preact 10 executes functional updaters synchronously within the useState
    // setter call, so normalized and changed are always set before the LS write below.
    // Same reliance as useChimeMuted.toggle: revisit if upgrading Preact beyond v10.
    let normalized;
    let changed = false;
    setList(prevList => {
      const resolved = typeof next === "function" ? next(prevList) : next;
      if (resolved === prevList) return prevList; // same-reference: no re-render, skip LS write
      changed = true;
      normalized = normalizeWatchlist(resolved);
      return normalized;
    });
    if (changed && typeof window !== "undefined") {
      try { window.localStorage.setItem(LS_WATCHLIST, JSON.stringify(normalized)); } catch (_e) { /* ignore */ }
    }
  };
  return [list, persist];
}
