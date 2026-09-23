import { numberOf, prefixOf } from './competitor_identity.jsx';

// competitor_search.jsx: the owner of the competitor-NUMBER match rule, and
// with it the answer to "does this row match what the reader typed" for the
// pickers that FILTER A LIST by it (bc-nsrc).
//
// It is NOT the app's only people-search, and calling it that would be wrong
// twice over (operator correction 2026-09-22). The registration desk runs a
// fuzzy RANKED search, and that is a different question as well as a different
// rule: it asks "who did this person most likely mean", answered with a score
// that orders the arrival queue, where this module answers "does this row
// match", a boolean that keeps or drops a row. Participants & seeds keeps its
// own name/zekken/dojo/dan-grade haystack. Both are ruled (see SCOPE below).
//
// The consumers are the public watchlist picker, the schedule filter (which
// PlayerMultiFilter mounts on the PUBLIC schedule and on admin_schedule_page
// alike) and the admin Scores page's match filter. So this rule is not "public
// only" -- which matters, because the two admin REFUSALS recorded further down
// are specific surfaces rather than a blanket exemption.
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
// THE NUMBER RULE (operator rulings 2026-09-21 and 2026-09-23, bc-nsrc). The
// prefix alone selects the competition's draw, and a number typed past its
// prefix must be the WHOLE number:
//
//     roster: K1, K12, K120 (prefix K); K021 (prefix K02); M12 (M); SK1 (SK)
//     "12"    -> nobody          a bare number is not a competitor number
//     "k"     -> K1, K12, K120, K021   both K draws: "k" begins both prefixes
//     "k0"    -> K021            only the K02 draw's prefix begins with "k0"
//     "k02"   -> K021            the whole prefix selects that draw
//     "sk"    -> SK1             "k" is not how "sk" begins
//     "k1"    -> K1              NOT K12, NOT K120
//     "k12"   -> K12             NOT K120
//     "k021"  -> K021            past the prefix, the whole number
//
// Two arms, and both read the record's own competition prefix:
//
//     q EQUALS the number                       ->  match
//     q BEGINS the competition's numberPrefix   ->  match (the draw)
//
// The prefix has to come from the record (prefixOf, stamped by the two record
// builders named in competitor_identity.jsx) and not be inferred from the
// number string, because the string cannot say where its prefix ends:
// DefaultNumberPrefix mints "K02" once "K" is taken, and "K021" is then K02's
// first competitor, not K's twenty-first. An earlier form of this rule read
// "q holds a digit -> exact" and so found nobody for "k02", while the docs
// promised the whole draw. A competitor only ever carries a number when their
// competition carries a prefix (handlers_viewer.go numberingApplies), so a
// record with a number and no prefix is a hand-edited legacy file, where the
// first arm still lets "1" find the competitor numbered "1" -- that number's
// WHOLE value -- and nothing else does.
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
// Consumers ES-import it directly; its only import is the identity leaf.

// The competitor-number ACCESSORS are competitor_identity.jsx's numberOf and
// prefixOf: they sit beside idOf/nameOf as plain field reads, and live there
// because the watchlist permalink wants the number WITHOUT this module's rule
// (it compares a machine-generated number exactly).
//
// This module owns the RULE and nothing else. Importing the leaf costs it no
// leaf-ness of its own: competitor_identity.jsx has no imports, and the chain
// stays acyclic and script-tag free.

// matchesCompetitorNumber: THE number rule, stated once. `q` is already
// trimmed and lowercased by the caller (every call site computes exactly that
// before it filters, so re-normalising here would hide a caller that stopped).
export function matchesCompetitorNumber(p, q) {
  if (!q) return false;
  const n = numberOf(p).toLowerCase();
  if (!n) return false;
  if (n === q) return true;
  const prefix = prefixOf(p).toLowerCase();
  return !!prefix && prefix.startsWith(q);
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
