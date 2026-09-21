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
//   watches, capped at WATCHLIST_MAX. resolveDeepLink says why: "Adding to
//   the watchlist is non-destructive (unlike the old single-follow
//   overwrite)". Arriving at a friend's link must not delete your own list.
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
//   a competitor with a live number  ->  the number        "K12"
//   a competitor without one         ->  their id          "3f2a9c7e-..."
//   a dojo entry                     ->  "d~" + the name   "d~Hagane Dojo"
//
// Numbers are SHORT, which is the whole point: a QR tops out at
// QR_MAX_BYTES (213, qr.jsx) including the origin, so a number-encoded list
// of ~45 scans while a UUID-encoded one of 6 already does not. Numbers have
// two costs the caller must surface rather than hide: a competitor has none
// until the draw runs, and a number is a draw POSITION (bc-pnum), so
// discarding and regenerating a draw re-mints it and an old link then names
// a DIFFERENT competitor. A number also stops being live when its
// competition completes while others run (the hide-finished-ones rule in
// viewer_watchlist_core.jsx). An id has none of those failure modes and is
// simply longer. That trade is the operator's ruling, not a default.
//
// SEPARATOR. "," and not ".", and the query is read RAW rather than through
// URLSearchParams.get. encodeURIComponent leaves "." untouched, so a dojo
// named "St. Mary's Kendo Club" would have split into three tokens; it DOES
// escape "," to %2C. But URLSearchParams.get decodes before we ever see the
// string, handing back a literal comma that then splits wrongly -- so the
// decode has to happen per token, after the split, never before it.
//
// Not a pure leaf: it imports competitor_search.jsx (itself a leaf) for the
// one thing it must not restate, "which numbers does this competitor hold".
// The chain viewer_watchlist -> watchlist_link -> competitor_search is
// acyclic and none of the three is script-tagged, which is the real safety
// condition behind viewer_watchlist.jsx's import note.
import { competitorNumbers } from './competitor_search.jsx';

export const WATCHLIST_PARAM = "w";
const DOJO_PREFIX = "d~";
const SEP = ",";

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
// `roster` is buildRoster output, so `numbers` already holds only the LIVE
// numbers.
export function watchlistTokens(watchlist, roster) {
  const byId = new Map();
  (roster || []).forEach((p) => { if (p && p.id) byId.set(p.id, p); });
  const out = [];
  (watchlist || []).forEach((e) => {
    if (!e) return;
    if (e.type === "dojo") {
      if (e.dojo) out.push(DOJO_PREFIX + encodeURIComponent(e.dojo));
      return;
    }
    if (!e.id) return;
    const numbers = competitorNumbers(byId.get(e.id));
    out.push(encodeURIComponent(numbers.length ? numbers[0] : e.id));
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

// parseWatchlistTokens: the tokens carried by a query string, decoded.
export function parseWatchlistTokens(search) {
  const raw = rawParam(search, WATCHLIST_PARAM);
  if (!raw) return [];
  return raw.split(SEP).map(safeDecodeToken).filter(Boolean);
}

// resolveWatchlistTokens: tokens back into watchlist entries, against the
// roster. A token that resolves to nobody is DROPPED, not rendered as a
// broken entry: it means the competitor left the roster, or their number was
// re-minted by a re-draw, or their competition completed while others run.
//
// Id first, then number, matching resolveDeepLink's precedence. The number
// comparison is EXACT and case-sensitive on purpose: these values are
// machine-generated by watchlistTokens above, not typed by a person, so the
// typed-query rule in competitor_search.jsx deliberately does not apply.
export function resolveWatchlistTokens(tokens, roster) {
  const list = roster || [];
  const out = [];
  (tokens || []).forEach((tok) => {
    if (tok.startsWith(DOJO_PREFIX)) {
      const dojo = tok.slice(DOJO_PREFIX.length);
      if (dojo) out.push({ type: "dojo", dojo });
      return;
    }
    const hit = list.find((p) => p && p.id === tok)
      || list.find((p) => competitorNumbers(p).includes(tok));
    if (hit) out.push({ type: "player", id: hit.id, name: hit.name || "", dojo: hit.dojo || "" });
  });
  return out;
}

// watchlistLinkFitsQR: can this URL be a QR code at all? Asked BEFORE offering
// the control, rather than calling renderQR and catching its throw. The byte
// length is what matters, not the character count: a non-ASCII dojo name costs
// several bytes per character once percent-encoded.
export function watchlistLinkFitsQR(url, maxBytes) {
  if (!url) return false;
  // No guard on maxBytes: a missing or unparseable limit coerces to NaN and
  // `length <= NaN` is already false, which is the answer we would have
  // written by hand. An explicit check would be a second way to say it.
  return new TextEncoder().encode(url).length <= Number(maxBytes);
}
