import { numberOf } from './competitor_identity.jsx';

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
// Consumers ES-import it directly; its only import is the identity leaf.

// The competitor-number ACCESSOR is competitor_identity.jsx's numberOf: it
// sits beside idOf/nameOf as a plain field read, and lives there because two
// callers want the field WITHOUT this module's rule (resolveDeepLink and the
// watchlist permalink both compare a machine-generated number exactly).
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
