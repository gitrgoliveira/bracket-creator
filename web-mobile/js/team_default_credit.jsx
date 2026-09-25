// team_default_credit.jsx: THE ONE JS OWNER of the "every team bout needs a
// result" default-win credit rule (bc-tmfn operator ruling).
//
// THE RULE: a team match (fixed-format, never kachinuki) that is COMPLETED
// with a match-level decision in the default-win class -- any kiken variant,
// fusenpai, or fusensho -- credits every NUMBERED bout (position >= 1; the
// daihyosen row is position -1, see DAIHYOSEN_POSITION in pool_ids.jsx) that
// carries no result of its own as a default win for the winning team: IV +1
// and PW +2 for the winner, nothing subtracted from the other side (an IL/PL
// tally, if a caller needs one, is a server-side standings concern --
// engine.accrueTeamSubResults and friends -- not something this DISPLAY-only
// leaf computes). decisionBy names the side that withdrew or was barred
// ("aka" = sideA, "shiro" = sideB, the same convention
// engine.RecordDecisionTx uses to decide Winner/WinnerSide), so the OTHER
// side is credited. decisionBy is preserved on every PUBLIC read path
// (internal/mobileapp/public_projection.go: "decisionBy, an enum, is what
// drives viewer rendering and is deliberately preserved"), unlike the free-
// text DecisionReason/CorrectionReason audit fields, so every consumer below
// may rely on it being present wherever a match reaches the wire at all.
//
// Kachinuki is EXCLUDED on purpose: it has no fixed teamSize of positions to
// pad out to (bouts are appended one at a time, "winner stays on", and the
// encounter ends on an explicit operator action -- End match -- never on a
// match-level default-win decision standing in for unplayed slots). Callers
// signal this via a `kachinuki` boolean in the match-context object every
// exported function below takes; most already carry a `teamMatchType` field
// (viewer_utils.jsx's compMatches / compMatchesForCompetition, pool_ids.jsx's
// teamMatchTypeFor) a caller resolves before calling in. That field is a
// VIEWER-LIST enrichment, not guaranteed on every match shape a generic
// shared function like bracket.jsx's teamIVScore/teamIVPWScore may be handed
// (e.g. a raw normalizeMatch object outside those enriched lists), so a
// caller that cannot resolve it should pass `kachinuki: false` -- crediting a
// kachinuki match's client-side IV/PW FALLBACK undercounts nothing real,
// because the server's own `teamResult` (state.TeamResultFrom, changed
// concurrently in internal/state/team_result.go) is authoritative whenever
// present and this leaf's arithmetic is consulted only in ITS absence.
//
// SCOPE: this leaf answers "which side does a default-win ruling credit" and
// "what does that mean for one bout's outcome / for an IV-PW aggregate". It
// does NOT decide what MARK (Kiken/Fus.) a surface shows: that is bracket.jsx
// `sideMarks`' job (mirrored from internal/export/suffix.go SideMarks), kept
// there because CLAUDE.md documents sideMarks/SideMarks as one mirrored pair
// with one owner, and duplicating its "Kiken"/"Fus." label logic here would
// split that pair into three. A caller placing a mark beside a withdrawn
// team's name computes it the SAME way MatchCard already does (sideMarks +
// placeMarks, keyed off sameCompetitor(match.winner, match.sideA/sideB)),
// not through this leaf.
//
// Imports realIppons/hanteiDecided from result_slot.jsx and withdrawnSideKey
// from ineligible_match.jsx -- both leaves with no non-leaf imports of their
// own -- so this file stays leaf-or-nothing per the dependency-hygiene rule
// stated in result_slot.jsx's own header.

import { realIppons, hanteiDecided } from './result_slot.jsx';
import { withdrawnSideKey } from './ineligible_match.jsx';

// isTeamDefaultWinDecision: the default-win decision class -- every kiken
// variant, fusenpai, or fusensho -- whose awarded points record as maru.
// THE one JS owner of this class (bc-cse): bracket.jsx's isDefaultWinBC and
// admin_scoring_shared.jsx's withdrawalInForce both used to hand-roll their
// own copy and now delegate here instead. Mirrors domain.IsDefaultWinDecisionStr
// (Go), kept as its own small copy against THAT language rather than an
// import: a one-line class predicate is cheaper to duplicate across languages
// than to wire a cross-language dependency for.
export function isTeamDefaultWinDecision(decision) {
  return decision === "kiken" || decision === "kiken-voluntary" || decision === "kiken-injury" ||
    decision === "fusenpai" || decision === "fusensho";
}

// creditedSideKey: the side ("a" = Aka/sideA, "b" = Shiro/sideB) a match-
// level default-win ruling credits -- the COMPLEMENT of ineligible_match.jsx's
// withdrawnSideKey (bc-cse), the one owner of the aka/shiro <-> a/b mapping:
// decisionBy "shiro" -> sideA/Aka wins, decisionBy "aka" -> sideB/Shiro wins
// (mirroring engine.RecordDecisionTx's decisionBy -> Winner mapping: "decisionBy
// names the WITHDRAWING side"). Kept accepting decisionBy alone, not a full
// match, for its existing callers (teamDefaultWinCreditActive, creditedBoutSide),
// which already know their match is completed and in the default-win class
// before asking, so no winner-attribution fallback is needed here. Returns ""
// for a missing/unrecognised decisionBy -- nothing to credit.
const SIDE_COMPLEMENT = { a: "b", b: "a" };
export function creditedSideKey(decisionBy) {
  return SIDE_COMPLEMENT[withdrawnSideKey({ decisionBy })] || "";
}

// teamDefaultWinCreditActive(matchCtx): is match-level default-win crediting
// IN FORCE right now? matchCtx = { status, decision, decisionBy, kachinuki }.
// A COMPLETED, non-kachinuki match whose decision is in the default-win
// class and whose decisionBy names a side. Callers already know their match
// is a TEAM match before asking this (this leaf has no way to tell an
// individual match's kiken from a team one -- see the file header), so every
// call site below gates on team-shaped data first (a subResults array, or a
// component that only ever mounts for a team match).
export function teamDefaultWinCreditActive(matchCtx) {
  const { status, decision, decisionBy, kachinuki } = matchCtx || {};
  return status === "completed" && !kachinuki &&
    isTeamDefaultWinDecision(decision) && !!creditedSideKey(decisionBy);
}

// subBoutHasResult: the wire-level twin of Go's SubMatchResult.HasResult
// (internal/state/models.go) -- a winner, a decision (a tie is "hikiwake", a
// default win "fusensho"), a struck point or the hantei mark on either side,
// a foul, or an overtime. A row with none of those was never fought. Reuses
// realIppons (drops the "•" placeholder AND the "Ht" mark, i.e. counts only a
// real struck point, matching Go's CountScoringIppons) and hanteiDecided
// (does either side carry the "Ht" mark) from result_slot.jsx rather than
// re-spelling either filter, per that file's "one definition" rule for what
// counts as a recorded ippon.
//
// This is the WIRE shape (ipponsA/ipponsB/winner/decision/hansokuA/hansokuB/
// encho), the same shape TeamScoreboard's isScored (match_scoreboard.jsx)
// reads -- NOT the team editor's local row-EDITING state (aPts/bPts/fusensho/
// draw), which is a different shape entirely and already has its own has-a-
// result check, admin_scoring_team.jsx's subBoutHasBeenPlayed. Do not feed
// one shape to the other's checker.
export function subBoutHasResult(sub) {
  if (!sub) return false;
  return !!sub.winner || !!sub.decision ||
    realIppons(sub.ipponsA).length > 0 || realIppons(sub.ipponsB).length > 0 ||
    hanteiDecided(sub) ||
    (sub.hansokuA || 0) > 0 || (sub.hansokuB || 0) > 0 ||
    !!(sub.encho && (sub.encho.periodCount || 0) > 0);
}

// creditedBoutSide(sub, matchCtx): for ONE numbered bout row (position >= 1;
// pass a stub {position} — or nothing at all — for a row the wire has not
// sent, e.g. a padding position beyond what subResults currently carries),
// which side the active ruling credits it to ("a"/"b"), or "" when this bout
// is NOT credited: the ruling is not in force, or the bout already carries
// its own result. One-stop call for a per-row renderer (TeamScoreboard's
// effective-bout synthesis) or a per-bout aggregate loop (bracket.jsx's
// teamIVScore fallback).
export function creditedBoutSide(sub, matchCtx) {
  if (!teamDefaultWinCreditActive(matchCtx)) return "";
  if (subBoutHasResult(sub)) return "";
  return creditedSideKey(matchCtx.decisionBy);
}

// creditedTotals(missingCount, side): the IV/PW a default-win ruling adds on
// top of the bouts actually fought -- IV +1 and PW +2 per credited bout,
// awarded entirely to `side` ("a"/"b"); the other side gets nothing.
// `missingCount` is the CALLER's own count of numbered bouts with no result:
// every caller's bout shape differs (the wire SubMatchResult array vs the
// team editor's local row-editing state), so this takes a plain count rather
// than an array, and each caller derives it with the has-a-result check
// appropriate to ITS shape (subBoutHasResult above for wire data,
// admin_scoring_team.jsx's own subBoutHasBeenPlayed + unfinishedTeamBouts for
// the live modal). Returns all-zero for a falsy side or a zero count, so a
// caller need not gate the call itself.
export function creditedTotals(missingCount, side) {
  if (!side || !missingCount) return { ivA: 0, ivB: 0, pwA: 0, pwB: 0 };
  const iv = missingCount, pw = missingCount * 2;
  return side === "a" ? { ivA: iv, ivB: 0, pwA: pw, pwB: 0 } : { ivA: 0, ivB: iv, pwA: 0, pwB: pw };
}
