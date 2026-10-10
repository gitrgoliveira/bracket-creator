// side_marks.jsx: the RESULT-mark placement helpers (Kiken, Fus., Ht beside the
// competitor they name) and the winner-side lookup they hang off. Leaf module:
// its one import is competitor_identity.jsx, itself a leaf, so a surface that
// must not reach bracket.jsx can still place a mark.
//
// WHY THIS IS NOT IN bracket.jsx. The court console (admin_shiaijo.jsx) places a
// completed row's winner cue and a withdrawn team's mark, and it cannot import
// bracket.jsx: its render suite stubs window.BracketTree before importing the
// console, the console reads that global at module eval, and bracket.jsx
// reassigns window.BracketTree in its own module body. An import's dependencies
// evaluate FIRST, so a path from the console to bracket.jsx would overwrite the
// stub (the console's header states the rule). The helpers used to be read off
// `window` there, behind a guard that painted nothing when the global was
// missing; they are imported from here instead. side_marks_leaf.test.jsx pins
// the one-import rule.
//
// bracket.jsx imports these six, keeps every `window.X = X` line (the display
// and viewer bundles read them) and re-exports the names, so every importer of
// bracket.jsx keeps working.

import { sameCompetitor } from './competitor_identity.jsx';

// Local kiken check, so this leaf relies on no window global (the twin of
// bracket.jsx's isHikiwakeBC). The kiken family is the wire's `kiken`,
// `kiken-voluntary` and `kiken-injury`: see specs/openapi.yaml.
export function isKikenDecisionBC(v) { return v === "kiken" || v === "kiken-voluntary" || v === "kiken-injury"; }

// joinSp: join a score fragment and a result mark with a space, skipping
// empties ("M" + "Ht" → "M Ht", "" + "Kiken" → "Kiken"). The JS twin of
// domain.JoinNonEmpty in internal/domain/result_marks.go.
export const joinSp = (a, b) => [a, b].filter(Boolean).join(" ");

// placeMarks: resolve sideMarks onto the two display slots — the winner's
// mark rides the winning side, the loser's the other. When neither slot is
// known to have won, no marks are placed; each caller owns that fallback
// (score strings trail the marks, match cards drop them). The JS analogue
// of domain.SideMarksAB (internal/domain/result_marks.go), whose marks
// export.SideMarksLR places White-left on the sheet.
// Companion rule: on the two-slot GRID surfaces (the shared scoreboard and the
// team score editor) which of a side's two cells the mark takes is answered by
// resultSlot in result_slot.jsx — a separate leaf; the dependency reasoning is
// stated ONCE, in that file's header. Flat score strings have no slots, so
// they concatenate instead and never call it.
export function placeMarks(marks, firstWins, secondWins) {
  return firstWins ? [marks.winner, marks.loser] : secondWins ? [marks.loser, marks.winner] : ["", ""];
}

// sideMarks: the per-side RESULT marks. winner goes in the winning side's
// score cell, loser in the losing side's — the mark names its competitor:
//   hantei   → winner "Ht"    (FIK 7-5 / 29-6: judges picked the winner)
//   kiken    → loser  "Kiken" (the competitor who withdrew)
//   fusenpai → loser  "Fus."  (the no-show)
//   fusensho → winner "Fus."  (the default WIN names the present side)
// Mirrors domain.SideMarks (internal/domain/result_marks.go) exactly
// (CLAUDE.md documents the pair as one mirrored rule). bc-tmfn removed the
// earlier fusensho gap here: this surface used to omit the winner-side "Fus."
// mark on the theory that "the viewer surfaces it via a separate bout badge",
// but no such badge exists for a MATCH-LEVEL fusensho decision (only a
// per-bout team row's ○○ fill, which is a different thing), so a match-level
// default win used to render with no mark on this surface at all. There is no
// divergence left to document: a fusensho match now reads identically here and
// in the export.
export function sideMarks(decision, decidedByHantei) {
  let winner = "", loser = "";
  if (isKikenDecisionBC(decision)) loser = "Kiken";
  else if (decision === "fusenpai") loser = "Fus.";
  else if (decision === "fusensho") winner = "Fus.";
  if (decidedByHantei) winner = joinSp(winner, "Ht");
  return { winner, loser };
}

// teamMatchMarks: the match-level Kiken/Fus. mark for EACH side of a TEAM
// match a default-win decision closed -- the same sideMarks + placeMarks
// pattern MatchCard already applies to an individual match's score cell
// (aWin/bWin via sameCompetitor, then placeMarks), generalized for a caller
// that renders a side's NAME separately from its score cell (a list row, a
// TV headline) rather than inline in a flat score string. THE one place this
// composition lives (bc-tmfn): every consumer below calls this rather than
// re-deriving its own copy.
//
// `isTeamRow` is the caller's OWN team-match signal (a subResults array, a
// compKind check, whatever it already has) -- this function has no way to
// tell an individual match's kiken from a team one, so it never guesses.
// Without it, an ordinary INDIVIDUAL kiken/fusenpai match would get this
// mark TWICE: once here, once already inline in its own matchScoreStr
// (formatIpponsScore's sideMarks call), since teamIVPWScore is deliberately
// free of marks and an individual score string is not.
//
// bc-cse: OPTIONAL. Four callers (admin_schedule_score_editor.jsx,
// viewer_match.jsx, viewer_schedule.jsx, viewer_standings.jsx) computed the
// exact same `Array.isArray(m.subResults) && m.subResults.length > 0` before
// calling in, so that default now lives here instead and those four callers
// pass nothing. A caller with a BETTER signal (viewer_match.jsx's own
// compKind/teamSize check, display_scoreboard.jsx's competition-format
// isTeamMatch prop) still passes it explicitly to override the default --
// e.g. a genuine team match with an empty subResults array (nothing fought
// yet) would otherwise read as non-team here.
//
// Returns {} for a non-team row, a not-yet-completed match, or a decision
// sideMarks has nothing to say about (returns "" for both sides, same as
// the individual case).
export function teamMatchMarks(match, isTeamRow) {
  const teamRow = isTeamRow === undefined
    ? Array.isArray(match?.subResults) && match.subResults.length > 0
    : isTeamRow;
  if (!teamRow || !match || match.status !== "completed") return { shiro: "", aka: "" };
  const marks = sideMarks(match.decision, !!match.decidedByHantei);
  const aWin = sameCompetitor(match.winner, match.sideA);
  const bWin = sameCompetitor(match.winner, match.sideB);
  const [aMark, bMark] = placeMarks(marks, aWin, bWin);
  return { shiro: bMark, aka: aMark };
}

// winnerSideLR: which DISPLAY side won, under the SHIRO-left convention every
// score string uses (sideB = Shiro = left, sideA = Aka = right). Returns
// "left" | "right" | null (no winner recorded, drifted data, or a mixed
// id/no-id pair that sameCompetitor refuses to guess on). Accepts both
// object sides ({id, name}) and bare name strings (routed through
// sameCompetitor, competitor_identity.jsx -- the one owner of the id/name
// attribution rule).
export function winnerSideLR(m) {
  if (!m || !m.winner) return null;
  if (sameCompetitor(m.winner, m.sideB)) return "left";
  if (sameCompetitor(m.winner, m.sideA)) return "right";
  return null;
}
