// competitor_search.jsx: the ONE answer to "does this competitor match what
// the reader typed" on the public surfaces (bc-nsrc).
//
// It exists because that question was spelled three different ways in three
// files and two of them could not find a competitor by the number printed on
// the draw sheet:
//
//   viewer_schedule.jsx    (p.number || "").toLowerCase().includes(q)
//   viewer_watchlist.jsx   String(p.number || "").toLowerCase().startsWith(q)
//   admin_participants.jsx the number was not a search target at all
//
// For the string "K12", includes("12") is true and startsWith("12") is false,
// so the same keystroke found a person on one surface and nobody on its
// sibling, under a placeholder promising both.
//
// THE NUMBER RULE (operator ruling 2026-09-21, bc-nsrc). The number always
// needs its prefix, and a number typed WITH its prefix must be the WHOLE
// number:
//
//     roster: K1, K12, K120, M12, M112, SK1
//     "12"    -> nobody          a bare number is not a competitor number
//     "k"     -> K1, K12, K120   the prefix alone selects that draw
//     "sk"    -> SK1             "sk1" does not start with "k"
//     "k1"    -> K1              NOT K12, NOT K120
//     "k12"   -> K12             NOT K120
//
// Two arms, and deliberately NO bare-digit special case:
//
//     q holds no digit  ->  the number STARTS WITH q
//     q holds a digit   ->  the number EQUALS q
//
// "a bare number finds nobody" falls out of exactness rather than being its
// own clause, because a competition is never drawn without a prefix (see
// cmd/shared.go resolveNumberPrefix: an unprefixed number "would collide with
// every other competition's and is not a tag anyone can call at the desk").
// A hand-edited legacy file with unprefixed numbers would let "1" match the
// competitor numbered "1" -- which is still that competitor's WHOLE number,
// so it does not violate the ruling.
//
// NAME AND DOJO keep substring matching. Only the number is anchored: a
// surname is something you type a fragment of, a number is an identifier you
// read off a sheet.
//
// SCOPE. The admin roster search (admin_participants.jsx) is NOT a consumer:
// the operator ruled on 2026-09-21 that it should not search numbers at all,
// so it keeps its own name/zekken/dojo/dan-grade haystack and this module
// does not reach it. The registration desk (admin_registration_desk.jsx) is
// not a consumer either, by the same day's ruling: its fuzzy ranked search
// returns a SCORE for the arrival loop rather than a boolean for a list, and
// it keeps finding K12 from a bare "12". Both divergences are ruled, not
// oversights -- do not "unify" them.
//
// A leaf with no imports, like numbered_name.jsx and write_result.jsx, so
// every consumer ES-imports it directly.

// competitorNumbers accepts both competitor shapes this app carries, so one
// rule serves both without either caller restating it:
//   a ROSTER record  -> `numbers`, every live number (buildRoster applies the
//                       hide-finished-ones ruling, viewer_watchlist_core.jsx)
//   a MATCH SIDE     -> `number`, a single number that is already the right
//                       one, because a match belongs to exactly one
//                       competition
// Exported because resolveDeepLink (viewer_home.jsx) needs the same answer to
// "which numbers does this competitor hold" while applying its OWN comparison:
// `?playerNumber=` carries a machine-generated value, so it stays EXACT and
// CASE-SENSITIVE, pinned by resolve_deep_link.test.jsx ("QR encodes exact
// value"). That is a different contract from a person typing into a search
// box, and routing it through the typed-query rule below would have quietly
// broken it. Shape knowledge is shared; the comparison is not.
export function competitorNumbers(p) {
  if (!p) return [];
  if (Array.isArray(p.numbers)) return p.numbers;
  return p.number ? [p.number] : [];
}

// matchesCompetitorNumber: THE number rule, stated once. `q` is already
// trimmed and lowercased by the caller (every call site computes exactly that
// before it filters, so re-normalising here would hide a caller that stopped).
export function matchesCompetitorNumber(p, q) {
  if (!q) return false;
  const anchored = /\d/.test(q);
  return competitorNumbers(p).some((raw) => {
    const n = String(raw || "").toLowerCase();
    if (!n) return false;
    return anchored ? n === q : n.startsWith(q);
  });
}

// competitorMatchesQuery: the whole predicate, for a roster record or a match
// side. An empty query matches everything, which is what a typeahead wants
// before the reader has typed.
export function competitorMatchesQuery(p, q) {
  if (!q) return true;
  if (!p) return false;
  if ((p.name || "").toLowerCase().includes(q)) return true;
  if ((p.dojo || "").toLowerCase().includes(q)) return true;
  return matchesCompetitorNumber(p, q);
}

// matchMentions: the free-text arm of the schedule filter, which asks the same
// question of a MATCH rather than a person -- does either side match? Shared
// with the picker above so the dropdown and the free-text chip on the SAME
// page can never disagree about who "K12" is.
export function matchMentions(m, q) {
  if (!q) return false;
  if (!m) return false;
  return competitorMatchesQuery(m.sideA, q) || competitorMatchesQuery(m.sideB, q);
}
