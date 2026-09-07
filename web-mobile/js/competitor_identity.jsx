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
