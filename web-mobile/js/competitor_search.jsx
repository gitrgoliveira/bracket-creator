// competitor_search.jsx: the ONE answer to "does this competitor match what
// the reader typed" in the tournament's people-pickers (bc-nsrc).
//
// Those pickers are the public watchlist and the schedule filter -- and the
// schedule filter is mounted on an ADMIN surface too (PlayerMultiFilter, from
// admin_schedule_page.jsx as well as viewer_schedule.jsx), so this rule is not
// "public only". That matters because the two admin REFUSALS recorded further
// down are specific surfaces, not a blanket exemption.
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

// competitorNumber: a competitor holds exactly ONE number, and this is the one
// place that reads it. Both shapes this app carries agree on that:
//   a ROSTER record (buildRoster output)
//   a MATCH SIDE, whose number is already the right one because a match
//   belongs to exactly one competition
//
// Someone entered in two competitions does NOT hold two numbers here: ids are
// minted per competition (a fresh uuid in state.AddParticipant, `${compID}-pN`
// in the admin client), so they arrive as two separate records with one number
// each and nothing merges them. An earlier revision of this module accepted a
// `numbers` ARRAY for that case; nothing ever produced one, so the branch was
// dead and the comments around it described a feature that does not exist.
//
// Exported because resolveDeepLink (viewer_home.jsx) and the watchlist
// permalink (watchlist_link.jsx) both need the same answer while applying
// their OWN comparison: those values are machine-generated, so they stay EXACT
// and CASE-SENSITIVE (pinned by resolve_deep_link.test.jsx, "QR encodes exact
// value"). That is a different contract from a person typing into a search
// box, and routing them through the typed-query rule below would quietly break
// it. Shape knowledge is shared; the comparison is not.
export function competitorNumber(p) {
  return p && p.number ? String(p.number) : "";
}

// matchesCompetitorNumber: THE number rule, stated once. `q` is already
// trimmed and lowercased by the caller (every call site computes exactly that
// before it filters, so re-normalising here would hide a caller that stopped).
export function matchesCompetitorNumber(p, q) {
  if (!q) return false;
  const n = competitorNumber(p).toLowerCase();
  if (!n) return false;
  return /\d/.test(q) ? n === q : n.startsWith(q);
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
