// competitor_identity.jsx: THE ONE predicate for "are these two competitor
// records the same competitor". Leaf module, no imports, so every file that
// compares two resolved sides (winner vs sideA/sideB, a watched/picked
// player vs a match side, a withdrawn player vs a remaining match's side)
// can import it without adding to the dependency graph.
//
// THE RULE (bc-pnum operator ruling, restated once here and nowhere else):
//   - both records carry a non-empty id   -> compare ids
//   - neither record carries an id at all -> compare non-empty names
//   - one carries an id and the other doesn't -> false, ALWAYS. Never guess:
//     a name hit must not stand in for a missing id just because the other
//     side happens to have one.
//
// This governs ATTRIBUTION: deciding who won, which row is watched, which
// player withdrew. It is deliberately narrower than "id decides when
// present, else name" (which independently checks each side and lets a
// mixed pair fall through to a name compare) -- that shape is exactly what
// let a same-name/different-dojo competitor get credited with a stranger's
// win once one side's id went missing.
//
// It does NOT govern ROSTER/LINEUP LOOKUP: resolving an id-less side against
// a roster keyed by id-or-name is a different operation (a KEY composite,
// not a peer comparison) and may still fall back to a name match -- that is
// the intentional recovery path for resolveSide's own "no id at all"
// fallback (api_serializers.jsx), and is why callers like
// match_scoreboard.jsx's useTeamLineups, admin_scoring_team.jsx's
// sideAKey/rosterForSide/teamIdForSide, and admin_schedule_lineup.jsx's
// sideKey/matchesKey call sideLookupKey (below) rather than sameCompetitor.
// admin_lineup.jsx's teamIdOf is the one exception: its legacy ID/Name
// fallback has a precedence sideLookupKey's simple shape can't reproduce,
// so it composes idOf/nameOf directly instead (see that function's own
// comment). Keys and lookups may compose; comparisons of two
// already-resolved records must go through sameCompetitor.
//
// Accepts a {id,name} object OR a bare name string (some callers -- team
// sub-bout winners/sides -- carry no id concept on the wire at all, by
// design; a bare string is therefore "no id" for this rule, exactly like an
// object whose id is "").

export function idOf(x) {
  return (x && typeof x === "object" ? x.id : "") || "";
}

export function nameOf(x) {
  return (x && typeof x === "object" ? x.name : x) || "";
}

// numberOf: the competitor's assigned number ("K12"), or "" when they have
// none. A field accessor beside idOf/nameOf, and nothing more.
//
// It is NOT part of identity and must stay out of competitorKey and
// sameCompetitor. A number belongs to a DRAW POSITION rather than to a person
// (bc-pnum): it does not exist before the draw runs, and regenerating a draw
// re-points it at someone else. Identity here stays id-then-name.
//
// It lives in this leaf rather than in competitor_search.jsx because the
// watchlist permalink wants only this accessor: it compares a machine-
// generated number EXACTLY, which is the opposite of the typed-query rule that
// module owns, and importing that module meant disclaiming its rule in a
// comment.
export function numberOf(x) {
  return (x && typeof x === "object" && x.number) ? String(x.number) : "";
}

// prefixOf: the number PREFIX of the competition this record was numbered in
// ("K", or "K02" when the plain letter was already taken), or "" when the
// record carries none. Trimmed here, mirroring Go's EffectiveNumberPrefix,
// because the number was minted from the trimmed value while the wire carries
// the field as typed. Stamped onto every roster record by buildRoster
// (viewer_watchlist_core.jsx) and onto every resolved match side by
// buildPlayerMap (api_serializers.jsx), from the competition each came from.
//
// It exists because the number string alone cannot say where its prefix ends:
// "K021" is K02's first competitor or K's twenty-first, and only the
// competition knows which. competitor_search.jsx reads it for the "prefix
// alone selects the draw" arm of the number rule; nothing else should need it.
export function prefixOf(x) {
  return (x && typeof x === "object" && x.numberPrefix) ? String(x.numberPrefix).trim() : "";
}

// competitorKey: id-decides-else-name as a single string, so THE RULE above
// falls out of comparing two keys rather than being restated at every call
// site. "id:"+id when x carries one; else "nm:"+normalizeName(name) when x
// carries a name; else "" (unkeyable). The "id:"/"nm:" prefixes are why a
// mixed pair (one keyed, one not) can never collide: an id key and a name
// key are never equal regardless of their values. `normalizeName` lets a
// caller fold case/whitespace for a name-based Set (e.g. the watchlist);
// sameCompetitor below passes none, since ATTRIBUTION compares exact names.
export function competitorKey(x, normalizeName = (s) => s) {
  const id = idOf(x);
  if (id) return "id:" + id;
  const name = normalizeName(nameOf(x));
  return name ? "nm:" + name : "";
}

export function sameCompetitor(a, b) {
  const ka = competitorKey(a);
  return !!ka && ka === competitorKey(b);
}

// sideLookupKey: the LOOKUP counterpart to sameCompetitor -- id else name, a
// roster/lineup key composite (not a peer comparison; see this file's header).
export function sideLookupKey(side) {
  return idOf(side) || nameOf(side);
}
