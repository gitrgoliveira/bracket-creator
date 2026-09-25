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
// Cycle note: viewer.jsx imports from this file and re-exports MOST symbols
// here (plus window.* assignments) so its own public surface stays backward
// compatible. Four are NOT re-exported through viewer.jsx -- buildWatchedSets,
// sideIsWatched, matchParticipantNames, matchInvolvesWatchedSet -- their
// consumers (viewer_home.jsx, viewer_schedule.jsx, viewer_competition.jsx,
// viewer_standings.jsx) import this file directly instead. viewer_watchlist.jsx
// (panel UI) continues to read the re-exported watchlist helpers via window.*
// lazy reads: those assignments still live in viewer.jsx.

import { competitorKey } from './competitor_identity.jsx';
import { resultRecencyDesc } from './result_recency.jsx';
import { parseWatchlistTokens, resolveFreshTokens, WATCHLIST_PARAM } from './watchlist_link.jsx';
import { isBarredMatch } from './ineligible_match.jsx';

const { useState } = React;

// Case-insensitive, whitespace-trimmed name normaliser for every watch-list
// key in this file: names here come from operator-typed rosters, so "Sato"
// and " sato " must key the same. competitorKey's id branch never calls this
// (an id compares exact), so it only ever affects the name fallback.
const watchNameKey = (s) => s.trim().toLowerCase();

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

// Check whether a participant object `p` refers to the followed player, via
// competitorKey with the case-insensitive name normaliser above: id decides
// whenever BOTH carry one, name only when NEITHER does (case/whitespace
// folded -- team-match sub-players or legacy fixtures key by display name
// only), and a mixed pair (one has an id, the other doesn't) is never
// guessed at by name, because an id key and a name key never collide.
export function isFollowedPlayer(p, followed) {
  if (!p || !followed) return false;
  const pk = competitorKey(p, watchNameKey);
  return !!pk && pk === competitorKey(followed, watchNameKey);
}

// sideIsWatched: does one id/name pair belong to the watched set? `watched`
// is the Set buildWatchedSets below produces, keyed by competitorKey --
// mutually exclusive per entry by construction, so the CHECKED pair's own
// id presence alone decides what it can match: an id-carrying pair keys to
// "id:…" and can only hit a watched id, never falling through to a name hit.
// A pooled "check id OR name independently" shape let watching Sato of
// Tokyo also highlight/list/on-deck Sato of Osaka's rows whenever the id
// check missed; the key shape is what forecloses that, not a runtime branch.
// Every case-insensitive watch surface (highlighting, upcoming-list,
// on-deck banner, running/recent filtering, matchInvolvesWatchedSet below)
// must consult this on top of `buildWatchedSets`'s set, not a hand-rolled
// equivalent, or the surfaces can disagree on the same id-less side.
// `watched` may also be a legacy empty array ([]) from callers with no
// watchlist concept (admin console): the `.has` guard below reads that as
// "nothing watched" rather than throwing.
export function sideIsWatched(id, name, watched) {
  if (!watched || typeof watched.has !== "function") return false;
  const key = competitorKey({ id, name }, watchNameKey);
  return !!key && watched.has(key);
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

// buildWatchedSets: the Set sideIsWatched consumes, keyed by competitorKey
// (case-insensitive name fallback) from a resolved watched-player list
// (resolveWatchedPlayers output, or any {id,name} list). Mutual exclusion
// per entry -- an id-carrying entry can never ALSO contribute a name key --
// falls out of competitorKey's "id:"/"nm:" prefixes rather than needing two
// separate sets and a branch to keep them apart.
export function buildWatchedSets(resolvedWatched) {
  const list = Array.isArray(resolvedWatched) ? resolvedWatched : [];
  return new Set(list.map((p) => competitorKey(p, watchNameKey)).filter(Boolean));
}

// matchInvolvesWatchedSet: does either side of match `m` belong to the
// watched set? `watched` is the Set buildWatchedSets produces. Routes
// through sideIsWatched, THE single case-insensitive side predicate, rather
// than a hand-rolled equivalent: a separate inline copy once consulted its
// OWN watchedIds/watchedNames pair, built inclusively (an id-carrying
// entry's name leaked into watchedNames too), so an id-less side sharing
// that name was listed in the running/upcoming/recent/on-deck filters even
// though the highlight predicate (same producer) correctly refused it.
// Shared by ViewerCompetition's running/upcoming/recent filtering,
// viewer_home.jsx's filterSecondaryOnDeck, and viewer_schedule.jsx's
// buildWatchlistUpcoming -- every match-level "is a watched side in this
// match" surface, so they cannot drift back apart into separate copies.
export function matchInvolvesWatchedSet(m, watched) {
  const [aId, bId] = matchParticipantIds(m);
  const [aName, bName] = matchParticipantNames(m);
  return sideIsWatched(aId, aName, watched) || sideIsWatched(bId, bName, watched);
}

// LocalStorage keys for FR-020 / FR-024. Centralised so every writer and
// reader shares the same keys.
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

// mergeSharedWatchlist: what opening a watchlist permalink (bc-wlpl) does to
// the list already on the device. It ADDS. It never replaces.
//
// A one-line rule with an expensive failure mode, and it has already failed
// once: a commit briefly shipped the shared list alone, so opening a friend's
// link would have deleted every competitor the recipient was already watching.
// The whole suite stayed green, because the rule was spelled inline inside an
// effect where no test could reach it. It has a name now so it can be pinned,
// and watchlist_merge.test.jsx pins it.
//
// normalizeWatchlist does the work that makes ADD safe: it dedupes by entry
// key with the FIRST occurrence winning, so a competitor already on the device
// keeps their existing entry rather than being replaced by the incoming copy,
// and it applies WATCHLIST_MAX to the result so a large shared list cannot
// push the device over the cap.
//
// A merge that adds nothing returns `existing` ITSELF, not an equal copy.
// That is the common case now: the home address bar mirrors the list
// (mirrorWatchlistParam), so every reload of home reads the device's own list
// back as a link. useWatchlist's setter drops a same-reference result, so the
// no-op costs no re-render and no localStorage write, and cannot feed the
// effect's own dependency (the write loop sharedLinkPass describes).
export function mergeSharedWatchlist(existing, shared) {
  const merged = normalizeWatchlist([...(existing || []), ...(shared || [])]);
  const unchanged = Array.isArray(existing) && merged.length === existing.length
    && merged.every((e, i) => entryKey(e) === entryKey(existing[i]));
  return unchanged ? existing : merged;
}

// landedSharedKeys: which of a shared link's tokens actually ENDED UP in the
// list, given what the device already watches.
//
// It exists because the merge above can drop entries in silence:
// normalizeWatchlist caps at WATCHLIST_MAX and the existing entries are
// concatenated first, so a reader already at the cap receives nothing from a
// link. The list is right to refuse -- the cap is the cap -- but the CALLER's
// ledger is not, and that is what this answers.
//
// viewer_home records a token as applied so that a healing roster can never
// re-add an entry the reader has since pruned. Recording one that never
// landed inverted that protection into data loss: nothing was added, the
// ledger said it had been, and settling then took ?w= out of the address
// bar, the only copy of the link, so pruning to make room and reloading
// brought back nothing. A token that did not land is therefore NOT applied,
// and the caller keeps the query until it is.
//
// `entries` and `keys` are the index-aligned pair resolveFreshTokens returns
// (it pushes to both in the same step); that alignment is stated there.
export function landedSharedKeys(existing, entries, keys) {
  const present = new Set(mergeSharedWatchlist(existing, entries).map(entryKey));
  const landed = [];
  (entries || []).forEach((e, i) => {
    const key = (keys || [])[i];
    if (key && present.has(entryKey(e))) landed.push(key);
  });
  return landed;
}

// sharedLinkPass: ONE pass of applying a ?w= permalink to the device's list,
// as a decision rather than an action. The effect in viewer_home.jsx calls it
// and then does exactly what it says: record `landed`, merge `entries` if
// `write`, and once `settle`, hand the address bar over to the list. Every
// rule about the pass lives here, where a unit test can run it, and none in
// the effect, where nothing can.
//
// It exists because of a loop. The effect used to write whenever a token
// RESOLVED and settle only once every token had LANDED -- two different
// conditions, and the gap between them is WATCHLIST_MAX. A reader already at
// the cap opened a link: the token resolved, the merge dropped it, nothing
// was recorded, the write still ran, and mergeSharedWatchlist returns a fresh
// array every time, so the state changed by reference, the effect re-fired on
// its own dependency, and round again -- measured at ~60 localStorage writes
// a second, indefinitely, for exactly the reader the at-cap retry was written
// for. `write` is now the same condition the ledger records on: something
// landed. A pass that writes always records, so each pass leaves strictly
// fewer unrecorded tokens than the last, and the sequence reaches a pass that
// writes nothing. That is pinned as a fixpoint test, not asserted in prose.
//
// `settle` is the two-clause rule the effect used to spell inline: nothing a
// later pass could still answer, meaning no token resolved-but-unlanded (the
// list is full; the reader prunes and the query must survive to retry) and no
// token unresolved while a roster may still arrive.
export function sharedLinkPass({ search, roster, watchlist, applied, rosterLoaded }) {
  const tokens = parseWatchlistTokens(search);
  const { entries, keys, outstanding } = resolveFreshTokens(tokens, roster, applied);
  const landed = landedSharedKeys(watchlist, entries, keys);
  const unlanded = keys.length - landed.length;
  return {
    entries,
    landed,
    write: landed.length > 0,
    settle: unlanded === 0 && (rosterLoaded || outstanding === 0),
  };
}

// The ledger of ?w= tokens that LANDED (sharedApplied in viewer_home.jsx)
// used to live for one mount, while the list it protects lives in
// localStorage and outlives it. sharedLinkPass keeps the query while a token
// is still outstanding -- the list is full, or a competition's roster failed
// to read -- and across a RELOAD in that state a fresh mount, with an empty
// ledger, re-applied every token the previous mount had landed: a reader who
// had pruned one of them got it back. That is the resurrection mirroring
// the list into the address bar prevents, reopened for exactly the held case.
//
// sessionStorage carries the ledger across the reload: same tab, same link.
// It is keyed on the raw `w` value, so a DIFFERENT link starts a fresh
// ledger; a new tab is a fresh open (sessionStorage is per tab); and
// settling clears it, so re-opening the same link later still adds. The three
// helpers take the storage as a parameter so a unit test can hand them a
// fake, and sessionStore() is the one place the real one is reached -- the
// property access itself can throw where storage is blocked, and a link that
// cannot remember what it landed still lands it (the in-memory ledger
// protects the mount; only the reload protection is lost).
export const SS_SHARED_LEDGER = "bc_watch_shared_ledger";

const sharedLinkValue = (search) => new URLSearchParams(search || "").get(WATCHLIST_PARAM) || "";

export function sessionStore() {
  try {
    return typeof window === "undefined" ? null : window.sessionStorage;
  } catch (_e) {
    return null; // storage blocked (private mode, cookies off): no reload protection
  }
}

export function readSharedLedger(storage, search) {
  const w = sharedLinkValue(search);
  if (!w || !storage) return new Set();
  try {
    const parsed = JSON.parse(storage.getItem(SS_SHARED_LEDGER) || "null");
    if (!parsed || parsed.w !== w || !Array.isArray(parsed.keys)) return new Set();
    return new Set(parsed.keys.filter((k) => typeof k === "string"));
  } catch (_e) {
    return new Set(); // malformed or unreadable: start a fresh ledger
  }
}

export function writeSharedLedger(storage, search, keys) {
  const w = sharedLinkValue(search);
  if (!w || !storage) return;
  try {
    storage.setItem(SS_SHARED_LEDGER, JSON.stringify({ w, keys: Array.from(keys) }));
  } catch (_e) { /* quota or blocked: the in-memory ledger still protects this mount */ }
}

export function clearSharedLedger(storage) {
  if (!storage) return;
  try {
    storage.removeItem(SS_SHARED_LEDGER);
  } catch (_e) { /* nothing to clear */ }
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

// heroEntry: which entry the CARD shows. This is deliberately NOT
// effectivePrimaryKey.
//
// The primary drives two different things: the hero card (display) and
// useFollowedMatchAlert (a chime, a title flash, a notification). Returning
// null for 2+ unpinned entries is the right answer for the ALERT -- the loud
// tier must never attach itself to someone the reader did not choose; that is
// what the "no hero, no chime" test pins, and the quiet tier
// (useSecondaryWatchAlert) covers the others.
//
// But it was also the answer for the CARD, so adding a training partner deleted
// the most valuable element on the page and left a ☆ hint in its place
// (bc-wlhc). Display has no such hazard: showing the first-added entry's match
// costs the reader nothing and is almost always themselves.
//
// So: the card falls back to the first entry, the alert does not. Callers that
// mean "who gets the chime" keep using findPrimaryEntry.
// matchFor is optional and, when given, decides the UNPINNED fallback: the
// first entry that can actually fill the card, rather than the first ADDED.
// Without it the fallback reproduced the very bug this function exists to fix,
// by list order instead of by pin -- a coach who added a training partner
// first and themselves second got "No upcoming matches for <partner>" the
// moment the partner finished, while their own bout was minutes away and had
// no card. A PIN still wins even when it yields nothing: it is an explicit
// choice, and silently showing someone else would be the worse surprise.
//
// It returns the MATCH, not a boolean, and the two tiers below are why. Once
// the card began falling back to a finished bout (buildPrimaryLastResult), a
// boolean "can this entry fill the card" went true for anyone with a RESULT,
// which is almost everyone by mid-afternoon -- so the fallback collapsed back
// to the first-added entry and re-opened the bug above in a quieter form: the
// partner's finished bout on the card while the reader's own is minutes away.
// A match still to fight therefore outranks a result, and a result outranks an
// entry with nothing at all. One pass: matchFor is asked once per entry, the
// first entry still to fight returns on the spot, and the first with any
// match at all is remembered in case nobody is.
export function heroEntry(watchlist, pinnedKey, matchFor) {
  const pinned = findPrimaryEntry(watchlist, pinnedKey);
  if (pinned) return pinned;
  const list = normalizeWatchlist(watchlist);
  if (typeof matchFor === "function") {
    let finished = null;
    for (const e of list) {
      const m = matchFor(e);
      if (!m) continue;
      if (m.status !== "completed") return e;
      if (!finished) finished = e;
    }
    if (finished) return finished;
  }
  return list[0] || null;
}

// findPrimaryEntry: the primary entry object (or null), per effectivePrimaryKey.
export function findPrimaryEntry(watchlist, pinnedKey) {
  const key = effectivePrimaryKey(watchlist, pinnedKey);
  if (!key) return null;
  return normalizeWatchlist(watchlist).find((e) => entryKey(e) === key) || null;
}

// matchesInvolving: the matches `keep` accepts that the primary entry is a
// side of (a player → just them; a dojo → any current member). The two
// builders below differ only in which matches they keep and how they order
// what is left, so the id resolution -- and the bc-pnum rule it carries --
// is answered here once.
//
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
function matchesInvolving(primaryEntry, roster, allMatches, keep) {
  if (!primaryEntry) return [];
  const ids = new Set(resolveEntryPlayerIds(primaryEntry, roster));
  if (ids.size === 0) return [];
  const index = indexFor(allMatches);
  // A dojo entry whose two members meet each other lists that match under
  // both ids; the Set folds it back to one, in the list's own order.
  const seen = new Set();
  ids.forEach((id) => (index.get(id) || []).forEach((m) => seen.add(m)));
  return (Array.isArray(allMatches) ? allMatches : []).filter((m) => seen.has(m) && keep(m));
}

// matchesByParticipantId: every match a participant id appears on, keyed by
// that id. A side with no id is not indexed: an id-less side is a MIXED pair
// under sameCompetitor's rule and must never be reached by name.
export function matchesByParticipantId(allMatches) {
  const index = new Map();
  (Array.isArray(allMatches) ? allMatches : []).forEach((m) => {
    if (!m) return;
    matchParticipantIds(m).forEach((id) => {
      if (!id) return;
      if (!index.has(id)) index.set(id, []);
      index.get(id).push(m);
    });
  });
  return index;
}

// One index per match ARRAY, not per call. The home page asks the two
// builders above for up to WATCHLIST_MAX entries on every SSE refresh, each
// against the same `bothSidesMatches` array, so a filter per call walked the
// whole schedule fifty times per tick. A WeakMap keyed on the array itself
// builds the index once per array identity and lets it go with the array;
// callers keep passing plain arrays (viewer_competition.jsx, the suite) and
// never see it.
const INDEX_BY_LIST = new WeakMap();
function indexFor(allMatches) {
  if (!Array.isArray(allMatches)) return new Map();
  let index = INDEX_BY_LIST.get(allMatches);
  if (!index) {
    index = matchesByParticipantId(allMatches);
    INDEX_BY_LIST.set(allMatches, index);
  }
  return index;
}

// buildPrimaryNextMatch: the hero match for the primary entry: the nearest
// match STILL TO FIGHT, ordered running-first then by scheduledAt so the hero
// surfaces a running match before a merely-scheduled one. Callers pass match
// lists already filtered through hasBothSides (as the home page does): this
// helper stays free of the window.hasBothSides proxy so it is unit-testable
// in isolation.
//
// It answers exactly that question and never falls back to a finished match.
// The last-result card the watchlist hero shows once a competitor is done
// (operator ruling 2026-09-22) is buildPrimaryLastResult below, and the
// surface that wants both composes them. Answering both HERE was tried and
// leaked immediately: ViewerOverview's banner (viewer_competition.jsx) asks
// this same function and prints the answer under a hard-coded "Your next
// match" with a court and a time, so a bout already fought arrived there as a
// fixture still to come.
export function buildPrimaryNextMatch(primaryEntry, roster, allMatches) {
  // A barred match (ineligible_match.jsx) cannot be fought as scheduled: a
  // running match is never barred (barredSides is empty for anything but
  // `scheduled`), so this filter only ever drops a real scheduled match the
  // player cannot yet fight.
  const mine = matchesInvolving(primaryEntry, roster, allMatches, (m) => m.status !== "completed")
    .filter((m) => !isBarredMatch(m));
  mine.sort((a, b) => {
    const ao = a.status === "running" ? 0 : 1;
    const bo = b.status === "running" ? 0 : 1;
    if (ao !== bo) return ao - bo;
    return (a.scheduledAt || "99:99").localeCompare(b.scheduledAt || "99:99");
  });
  return mine[0] || null;
}

// buildPrimaryLastResult: the most recent RESULT involving the primary entry,
// or null. The watchlist hero offers it when there is nothing left to fight
// (operator ruling 2026-09-22): a competitor who is out, or who has finished
// their day, is exactly who the reader still cares about, and the card used to
// go to "No upcoming matches", which answers the wrong question. The watchlist
// is how a reader follows a PERSON, not only a fixture.
//
// Recency is resultRecencyDesc's rule, not a re-sort by scheduled time: the
// most recent RESULT is the last write, which is not the latest slot when a
// court has run out of schedule order (result_recency.jsx owns this; the court
// console and the public Recent results already ask it).
export function buildPrimaryLastResult(primaryEntry, roster, allMatches) {
  const done = matchesInvolving(primaryEntry, roster, allMatches, (m) => m.status === "completed");
  done.sort(resultRecencyDesc);
  return done[0] || null;
}

// Did every competition's roster LOAD? buildRoster cannot say: a competition
// whose participants.csv failed to read contributes no players, which is
// indistinguishable there from one that simply has none.
//
// That difference decides whether an id's ABSENCE from the roster means
// anything. The viewer payload swallowed a per-competition participants
// failure (logged, then Players = nil, payload still returned), so one
// unreadable file left every OTHER competition populating the roster -- the
// watchlist's roster.length > 0 guard passed, and every watched competitor
// from the failed competition turned amber and was told to delete and re-add
// someone the picker could not offer back, because the same missing roster is
// why they were not listed.
//
// Absent is read as LOADED: an older payload carries no such key, and a
// client that read that as "unavailable" would go silent about genuinely
// stale entries. A MISSING participants.csv is not a failure either -- the
// store returns ([], nil) for one -- so a competition with no roster yet
// reports true.
export function rosterFullyLoaded(competitions) {
  return (competitions || []).every((c) => !c || c.rosterAvailable !== false);
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
        // `comps` is the competition names this record covers, for the
        // schedule picker's row. It lives here rather than in that picker
        // because it used to run its OWN near-identical dedup to collect it
        // -- same shape, but with no `!p || !p.id` guard, so every id-less
        // player collapsed into one entry keyed on `undefined`.
        // `numberPrefix` rides with the number it was minted under: the
        // number rule's "prefix alone selects the draw" arm reads it through
        // prefixOf (competitor_identity.jsx), because "K021" alone cannot say
        // whether its prefix is K or K02.
        map.set(p.id, { ...p, checkedIn, comps: [c.name || ""], numberPrefix: c.numberPrefix || "" });
      } else {
        // Reached only if the SAME participant id appears under two
        // competitions. Participant ids are minted per competition (a fresh
        // uuid in state.AddParticipant, `${compID}-pN` in the admin client),
        // so one person entered in two competitions holds two DIFFERENT ids
        // and arrives here as two separate records, each with its own number
        // -- which is why both of their numbers are independently searchable
        // without anything merging them.
        //
        // So this branch is effectively unreachable. It is kept because the
        // `checkedIn` merge predates bc-nsrc and removing a guard needs better
        // evidence than "I could not reach it"; `comps` accumulates with it so
        // the two cannot diverge if it ever does run. A fresh record, not a
        // mutation: the `...p` above already gave the map its own object, and
        // an in-place push would reach into it after it was stored.
        map.set(p.id, {
          ...existing,
          comps: [...existing.comps, c.name || ""],
          checkedIn: existing.checkedIn || checkedIn,
        });
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
