// watchlist_link.jsx: the watchlist as a shareable permalink (bc-wlpl).
//
// The watchlist lives in localStorage, so it does not survive a second phone,
// a different browser profile or a cleared cache. This module encodes it into
// a URL and reads it back, so a coach can hand their list to a parent and a
// competitor can move it to the tablet they actually watch on.
//
// It GENERALISES what viewer_home.jsx's resolveDeepLink already does for ONE
// person (`?player=`, `?playerNumber=`). Two of that function's decisions are
// inherited rather than re-made, because they are settled:
//
//   MERGE, NOT REPLACE. Opening a link ADDS to whatever the device already
//   watches, capped at WATCHLIST_MAX. The deep-link effect in ViewerHome says
//   why: "Adding to the watchlist is non-destructive (unlike the old
//   single-follow overwrite)". Arriving at a friend's link must not delete
//   your own list. This is load-bearing and was briefly lost to a bad commit
//   (see the fix commit for bc-wlpl), so mergeSharedWatchlist owns that rule
//   and watchlist_merge.test.jsx pins it.
//
//   APPLIED ONCE, AFTER THE ROSTER LOADS. The tokens cannot resolve against a
//   roster that has not arrived, and re-applying on every render would fight
//   an operator removing an entry.
//
// TOKEN FORM (operator ruling 2026-09-21, "Depends what the watchlist has"):
// each entry is encoded with whatever THAT entry actually carries, per entry
// rather than per list, so one pre-draw competitor does not cost the QR for
// thirty numbered ones.
//
//   a competitor with a number  ->  the number       "K12"
//   a competitor without one    ->  their id         "3f2a9c7e-..."
//   a dojo entry                ->  ":" + the name   ":Hagane Dojo"
//
// Numbers are SHORT, which is the whole point: a QR tops out at QR_MAX_BYTES
// (213, qr.jsx) including the origin, so a number-encoded list of ~45 scans
// while a UUID-encoded one of 6 already does not. The number costs two things
// the caller must surface rather than hide: a competitor has none until the
// draw runs, and a number is a draw POSITION (bc-pnum), so discarding and
// regenerating a draw re-mints it and an old link then names a DIFFERENT
// competitor. An id has neither failure mode and is simply longer. That trade
// is the operator's ruling, not a default.
//
// SEPARATOR. "," and not ".", and the query is read RAW rather than through
// URLSearchParams.get. encodeURIComponent leaves "." untouched, so a dojo
// named "St. Mary's Dojo" would have split into two tokens; it DOES
// escape "," to %2C. But URLSearchParams.get decodes before we ever see the
// string, handing back a literal comma that then splits wrongly -- so the
// decode has to happen per token, after the split, never before it.
//
// Not a pure leaf: it imports competitor_identity.jsx for the one thing it
// must not restate, "what number does this competitor hold". That module has
// no imports at all, so the chain viewer_watchlist -> watchlist_link ->
// competitor_identity is acyclic and none of the three is script-tagged,
// which is the real safety condition behind viewer_watchlist.jsx's note.
//
// This said competitor_search.jsx until 2026-09-23, naming an edge that does
// not exist: the accessor was moved to the identity leaf precisely so that a
// caller could read the FIELD without this module's typed-query rule, and
// this line did not move with it. Do not "restore" it.
import { numberOf } from './competitor_identity.jsx';

export const WATCHLIST_PARAM = "w";
const SEP = ",";

// The marker that says "this token is a DOJO, not a competitor". It has to be
// a character encodeURIComponent ALWAYS escapes, and ":" is: the set it leaves
// alone is A-Z a-z 0-9 - _ . ! ~ * ' ( ). An encoded competitor number or id
// therefore can never begin with a literal ":", so the marker cannot be
// confused with one -- which is why the test below runs on the RAW token,
// BEFORE decoding.
//
// The first version used "d~" and was ambiguous. Nothing stops an operator
// typing "d~" as a competition's number prefix: helper.ValidateNumberPrefix
// checks LENGTH only (3 runes, no charset) and the client's cutNumberPrefix is
// trim-and-truncate. That competition's competitors are then numbered "d~1",
// "d~2", and encodeURIComponent leaves every one of those characters alone, so
// sharing a watchlist containing "d~1" handed the recipient a DOJO entry named
// "1" and silently dropped the competitor.
const DOJO_SENTINEL = ":";

// Read one query parameter WITHOUT decoding it. See the SEPARATOR note above:
// decoding before the split is what breaks a dojo name containing a comma.
function rawParam(search, key) {
  const s = String(search || "").replace(/^\?/, "");
  for (const part of s.split("&")) {
    const eq = part.indexOf("=");
    if (eq < 0) continue;
    if (part.slice(0, eq) === key) return part.slice(eq + 1);
  }
  return "";
}

// decodeURIComponent throws on a malformed escape ("%zz", a truncated "%2"),
// which a hand-edited or truncated URL genuinely produces. Drop that one
// token and keep the rest: losing one name out of a shared list is far better
// than the whole link resolving to nothing.
function safeDecodeToken(tok) {
  try {
    return decodeURIComponent(tok);
  } catch (_e) {
    return "";
  }
}

// watchlistTokens: the watchlist as tokens, applying the per-entry rule above.
// `roster` is buildRoster output.
//
// An entry the roster cannot resolve still yields a token, carrying its id.
// That looks droppable and is not: the roster can legitimately be absent when
// the link is built (a competition's participants can fail to load, which is
// why rosterFullyLoaded exists), and nothing here distinguishes "this person
// left the roster" from "the roster is not here yet". Dropping would hand over
// a shorter list than the sender is looking at.
export function watchlistTokens(watchlist, roster) {
  const byId = new Map();
  (roster || []).forEach((p) => { if (p && p.id) byId.set(p.id, p); });
  const out = [];
  (watchlist || []).forEach((e) => {
    if (!e) return;
    if (e.type === "dojo") {
      if (e.dojo) out.push(DOJO_SENTINEL + encodeURIComponent(e.dojo));
      return;
    }
    if (!e.id) return;
    const number = numberOf(byId.get(e.id));
    out.push(encodeURIComponent(number || e.id));
  });
  return out;
}

// buildWatchlistLink: the shareable URL, or "" when there is nothing to share.
// `base` is the page URL WITHOUT a query (the caller builds it rather than
// reading location.href: app.jsx syncs state to the path only, so the address
// bar drops this query on the first navigation and is never the permalink).
export function buildWatchlistLink(base, watchlist, roster) {
  const tokens = watchlistTokens(watchlist, roster);
  if (!tokens.length) return "";
  return `${base}?${WATCHLIST_PARAM}=${tokens.join(SEP)}`;
}

// parseWatchlistTokens: the tokens carried by a query string, each tagged with
// what it IS. The tag is decided on the RAW token and the decode happens after
// (see DOJO_SENTINEL), so a competitor whose number merely looks like the
// marker once decoded cannot be mistaken for a dojo.
//
// Returns [{ kind: "dojo" | "competitor", value }]. Tagging here rather than
// re-sniffing in the resolver keeps the encode and decode halves of the format
// facing each other, with nothing in between able to disagree about them.
export function parseWatchlistTokens(search) {
  const raw = rawParam(search, WATCHLIST_PARAM);
  if (!raw) return [];
  const out = [];
  raw.split(SEP).forEach((tok) => {
    const isDojo = tok.startsWith(DOJO_SENTINEL);
    const value = safeDecodeToken(isDojo ? tok.slice(DOJO_SENTINEL.length) : tok);
    if (value) out.push({ kind: isDojo ? "dojo" : "competitor", value });
  });
  return out;
}

// stripWatchlistParam: the same query WITHOUT `w`, every other parameter left
// alone. Returns the string unchanged when there was no `w` to remove, so a
// caller can compare and leave the address bar untouched.
//
// Dropping the WHOLE query here would be wrong, and specifically wrong for the
// competitor tag. `helper.playerTagURL` prints each tag's QR as
// `<publicURL>/?playerNumber=K12`, which resolveDeepLink (viewer_home.jsx)
// reads. That link is applied as soon as the roster is non-empty, but a
// competitor whose competition has not loaded yet resolves to nobody and the
// deep link is spent; the query surviving in the address bar is what lets a
// reload retry it. Clearing the query wholesale took that away, so scanning a
// tag while one competition was still loading silently did nothing, with a
// reload no longer able to fix it.
//
// Hand-rolled rather than `new URLSearchParams(s); sp.delete(WATCHLIST_PARAM)`
// because that cannot return the string UNCHANGED: it re-serialises what it
// kept, and `toString()` writes a space as "+", so `?name=Ken%20Saito` (a real
// deep-link shape, resolveDeepLink reads `name`) comes back rewritten even
// when there was no `w` in it at all. The caller compares before it touches
// the address bar, so a rewrite there would rewrite links this function has no
// business touching. Measured, not assumed: only the space differs, which is
// enough.
export function stripWatchlistParam(search) {
  const s = String(search || "");
  const q = s.replace(/^\?/, "");
  if (!q) return s;
  const kept = q.split("&").filter((part) => {
    if (!part) return false;
    const eq = part.indexOf("=");
    return (eq < 0 ? part : part.slice(0, eq)) !== WATCHLIST_PARAM;
  });
  return kept.length ? `?${kept.join("&")}` : "";
}

// resolveWatchlistTokens: parsed tokens back into watchlist entries, against
// the roster. A competitor token that resolves to nobody is DROPPED rather
// than rendered as a broken entry: it means they left the roster, or their
// number was re-minted by a re-draw.
//
// Id first, then number, matching resolveDeepLink's precedence. The number
// comparison is EXACT and case-sensitive on purpose: these values are
// machine-generated by watchlistTokens above, not typed by a person, so the
// typed-query rule in competitor_search.jsx deliberately does not apply.
// resolveToken: ONE token against the roster, or null. This is the real
// contract -- the live path resolves token by token (see resolveFreshTokens)
// and only the batch form below is ever handed more than one. Keeping the
// singular case primary means a rule added to the batch form cannot quietly
// have no effect on production.
function resolveToken(tok, roster) {
  if (!tok) return null;
  if (tok.kind === "dojo") return { type: "dojo", dojo: tok.value };
  const list = roster || [];
  const hit = list.find((p) => p && p.id === tok.value)
    || list.find((p) => numberOf(p) === tok.value);
  return hit ? { type: "player", id: hit.id, name: hit.name || "", dojo: hit.dojo || "" } : null;
}

export function resolveWatchlistTokens(tokens, roster) {
  return (tokens || []).map((tok) => resolveToken(tok, roster)).filter(Boolean);
}

// tokenKey: the identity of a parsed token, for remembering that it has
// already been folded into the watchlist.
function tokenKey(tok) {
  return tok ? `${tok.kind}:${tok.value}` : "";
}

// resolveFreshTokens: the tokens that have NOT been applied yet and that
// resolve against the roster as it stands right now, with the keys to record
// for the ones that did.
//
// Token by token, and that is the whole point. A tournament's payload can come
// back with one competition's participants missing (rosterFullyLoaded exists
// because that is real), and resolving the link as a single all-or-nothing
// batch meant a coach's 20-entry link could arrive as 15, permanently, with no
// retry and no sign to either end. Resolving individually lets the rest land on
// a later pass when that roster arrives.
//
// Recording only what RESOLVED is what makes the retry safe in the other
// direction: an entry the reader has since removed is never re-added, because
// its token was recorded the first time it resolved. Resolving is necessary
// and not sufficient, though: the caller records a token once it has actually
// LANDED in the list (landedSharedKeys, viewer_watchlist_core.jsx), because
// the merge can still drop a resolved entry at WATCHLIST_MAX.
//
// `entries` and `keys` are INDEX-ALIGNED: both are pushed in the same step
// below, so keys[i] is the token that produced entries[i]. landedSharedKeys
// reads them that way.
//
// `outstanding` counts the tokens this pass could neither skip as already
// applied nor resolve: the ones a later roster might still answer for. It is
// how the caller knows the link is FINISHED with, without waiting for every
// competition in the tournament to load -- a wait that never ends when one
// of them permanently fails, and the caller's retry then never stops.
//
// Lives here, not inline in the effect that calls it. The last rule this
// feature kept inside a useEffect was silently replaced and no test could
// reach it.
export function resolveFreshTokens(tokens, roster, appliedKeys) {
  const seen = appliedKeys || new Set();
  const entries = [];
  const keys = [];
  let outstanding = 0;
  (tokens || []).forEach((tok) => {
    const key = tokenKey(tok);
    if (!key) return;
    if (seen.has(key)) return;
    const entry = resolveToken(tok, roster);
    if (!entry) { outstanding += 1; return; } // not loaded yet, or genuinely not in this tournament
    entries.push(entry);
    keys.push(key);
  });
  return { entries, keys, outstanding };
}
