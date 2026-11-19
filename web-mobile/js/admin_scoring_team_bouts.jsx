// Team-match bout and result rules: the pure helpers the team score editor
// (admin_scoring_team.jsx) and its tests share. No state, no hooks.
// Split out of admin_scoring_team.jsx; the kachinuki rules are in
// admin_scoring_team_kachinuki.jsx, which imports this module (never the reverse).

import { boutPositionName } from './lineup_resolver.jsx';
import { joinList } from './admin_helpers.jsx';
import { DEFAULT_WIN_IPPON, attributeWinnerSide, subBoutAttribution } from './result_slot.jsx';
// boutMiddle is THE single source for a bout's centre value (vs/X/(E)/(DH));
// the editor derives its per-bout middle from it rather than restating the
// chain (CLAUDE.md § Match Decision Types: the middle rule lives in ONE place).
import { boutMiddle } from './bracket.jsx';

// renderTeamBoutMiddle: the ONE place the editor turns a sub-bout into its
// centre value, for BOTH the read-only done row and the live entry row. Derives
// vs/X/(E)/(DH) from the single-source boutMiddle (decided-by-points reads "vs"
// like the bracket/scoreboard; the cell ippon letters carry the score). The
// editor's local sub state has no decision string, so synthesise boutMiddle's
// three inputs: a marked/derived tie → hikiwake, the daihyosen row →
// "daihyosen", s.encho (a period count) → {periodCount}. X keeps its dedicated
// styling; vs/(E)/(DH) render as the quiet centre span. (Defined below
// teamBoutIsDraw, which it calls.)
// teamBoutIsDraw: does this bout row read as a hikiwake? A declared winner
// un-draws the bout — that is the whole rule. The hantei case needs no
// parameter here because boutWinnerSide folds the hantei verdict into
// t.winner (its hanteiSide input), so a 1-1 daihyosen taken to hantei
// arrives with a winner and never derives the X that would claim a draw on
// a bout whose winner is simultaneously wearing Ht. Exported so the
// winner-un-draws rule is pinned without mounting the editor.
export function teamBoutIsDraw(s, t) {
  return t.winner === null && !!(s.draw || t.aTotal > 0 || t.bTotal > 0);
}

export function renderTeamBoutMiddle(s, t, isDaihyoRow) {
  const isDraw = teamBoutIsDraw(s, t);
  const mid = boutMiddle(
    isDaihyoRow ? "daihyosen" : (isDraw ? "hikiwake" : ""),
    s.encho > 0 ? { periodCount: s.encho } : null,
    isDraw ? { type: "hikiwake" } : null,
  );
  return mid === "X" ? <span className="tsm-draw">X</span> : <span style={{ color: "var(--ink-3)" }}>{mid}</span>;
}

// preserveStoredDaihyosenVerdict: while a stored hantei verdict is
// UNATTRIBUTABLE (rename drift: the stored winner name no longer matches
// exactly one current side) and the operator has neither re-picked a side nor
// cancelled, a correction save must NOT erase it - without this, fixing an
// unrelated bout score on a reopened encounter silently flipped a hantei win
// into a hikiwake (pool) or stripped a completed match's winner (knockout).
// It preserves ONLY the untouched state: withdrawal is explicit (clearHantei),
// re-picking replaces the verdict, and editing the daihyosen row itself
// yields too - `tied` must reflect the CURRENT local scoreline, because
// re-asserting decidedByHantei onto a row the operator just untied would
// have the server reject the whole save ("requires a tied scoreline").
// encho is deliberately NOT carried: the editor seeds its encho counter from
// the stored row, so daihyosenEnchoFields already round-trips an untouched
// value, and carrying it here would overwrite an operator's encho edit.
//
// The protection is ONE SAVE DEEP, not permanent, for an unattributable
// verdict specifically: since the winner names neither side, the wire's
// placeHtForWinner can never re-place the mark (there is no side to put it
// on - see the "row goes out markless" note at the call site below), so the
// save that runs THIS function still erases the stored Ht mark, even though
// it preserves the winner NAME. The next mount then derives
// existingDaihyosen.decidedByHantei as false (it is read from the mark, and
// the mark is gone), so `armed` starts false, `preserveStoredDaihyosenVerdict`
// returns null on its very first line, and an untouched correction save after
// THAT one clears the winner too - the exact hikiwake flip this function
// exists to prevent, deferred by exactly one save rather than avoided. This
// is why rename-drift is deliberately not "fixed" further here: renames are
// gated to setup/draw-ready, when no scored sub rows exist, so an
// unattributable daihyosen winner is not reachable through real flows in the
// first place - the decay above is a property of the (dead) scenario, not a
// live bug.
export function preserveStoredDaihyosenVerdict({ armed, pickedSide, tied, existingDaihyosen }) {
  if (!armed || pickedSide || !tied) return null;
  if (!existingDaihyosen?.decidedByHantei || !existingDaihyosen.winner) return null;
  return { winner: existingDaihyosen.winner, decidedByHantei: true };
}

// Position keys are generated inline in TeamScoreEditorModal (numbered "1".."N")
// from teamSize and any persisted kachinuki bouts; the upper bound everywhere is
// MAX_TEAM_SIZE (admin_helpers.jsx), kept in lockstep with the team-size input
// caps in admin_competition.jsx and admin_setup.jsx.

// The note under a representative bout the judges decided, on the public
// self-run page (bc-dhas). The server refuses a participant's change to that
// bout with the same sentence (repBoutHanteiRefusal,
// mobileapp), and both are pinned to the
// "recorded" value of internal/mobileapp/testdata/rep_bout_hantei_messages.json,
// so the participant reads one sentence whichever side says it.
export const REP_BOUT_DECIDED_NOTE = "The judges decided this representative bout (hantei). Ask the tournament organizer to change it.";

// teamResultLabel: the RESULT-band / Finish-button verdict text for a team
// encounter. A knockout match cannot be a draw (a tie is broken by a daihyosen,
// FIK rules), so a null winner in the bracket phase never reads "Draw": it's
// "Daihyosen" once a scored tie exists to break, or "-" before any bout lands.
// Only a pool encounter reads a null winner as a true draw. (a = Aka, b = Shiro.)
// Sentence case, like every other verdict the editors show.
//
// KACHINUKI (mp-gmcg): daihyosen does not exist, so a tied knockout kachinuki
// encounter must never read "Daihyosen": the tie is resolved by fighting on
// (next bout or encho on the same pair), so the band stays pending ("-")
// until the operator ends the match. Pools keep "Draw": that is exactly what
// End match would record there.
export function teamResultLabel({ teamWinner, isKnockoutPhase, hasAnyScore, isKachinuki }) {
  if (teamWinner === "a") return "Aka wins";
  if (teamWinner === "b") return "Shiro wins";
  if (isKnockoutPhase) return hasAnyScore && !isKachinuki ? "Daihyosen" : "-";
  return "Draw";
}

// isKoTieBlocked: Finish must be blocked while a knockout encounter has no
// winner: the operator has to add and score a daihyosen first. Pool draws stay
// finishable, and an already-completed match (correction flow) is never blocked.
export function isKoTieBlocked({ isKnockoutPhase, teamWinner, isComplete }) {
  return !!isKnockoutPhase && teamWinner === null && !isComplete;
}

// unfinishedTeamBouts: the numbered bouts (1..teamSize, in order) of a team
// match that have no result, which Finish refuses while any remain: every
// bout of a team match is fought (operator ruling 2026-09-24). "Has a result"
// is subBoutHasBeenPlayed, the one played-bout predicate, and state.
// SubMatchResult.HasResult is its Go twin on the server gate. Rows are found
// by their `_pos`, never by index. The daihyosen row is never numbered, so it
// is never returned.
//
// Every position counts, whoever the lineups field there: a vacancy on one
// side is a fusensho for the fighter who is present, and a position neither
// side fields is recorded as a Tie (operator ruling 2026-09-24). Callers skip
// kachinuki, which ends on End match instead.
export function unfinishedTeamBouts({ subs, teamSize }) {
  const out = [];
  for (let bout = 1; bout <= teamSize; bout++) {
    const row = (subs || []).find(s => s && s._pos === bout);
    if (!subBoutHasBeenPlayed(row)) out.push(bout);
  }
  return out;
}

// unfinishedTeamBoutsMessage: the refusal copy, word for word the server's
// (unfinishedTeamBoutsMessage in internal/mobileapp/team_finish_gate.go; both
// are pinned to internal/mobileapp/testdata/unfinished_team_bouts.json). It
// names the bouts and what to record, and never mentions the lineup: a vacancy
// is legitimate play, not something to complete.
export function unfinishedTeamBoutsMessage(teamSize, bouts) {
  const labels = (bouts || []).map(b => {
    const name = boutPositionName(teamSize, b - 1);
    return `Bout ${b}${name ? ` (${name})` : ""}`;
  });
  if (labels.length === 0) return "";
  const list = joinList(labels, "and", "");
  return `${list} ${labels.length === 1 ? "has" : "have"} no result. Record a score, a Tie, or a Fusensho before finishing.`;
}

// boutWinnerSide: THE single home for "which side won this sub-bout".
// Returns "a" (Aka), "b" (Shiro), or null when the bout has no winner.
// One rule, three surfaces — the IV/PW tally and persisted bout winner
// (subTotals), the kachinuki band's last-bout fact (kachinukiBandModel),
// and the End-match derivation (deriveKachinukiEndOutcome) all call this,
// so they can never disagree about who won a bout.
//
// Order matters. An operator-marked hikiwake has no winner at all. A
// FUSENSHO (FIK Art. 32) is decided by the decision alone: it names the winner,
// the maru cells (○○, or a single ○ in encho) are merely the score board's
// marking of it, and the loser's already-struck ippons are PRESERVED
// (preserveLoserScore, engine). So a walkover whose loser kept
// two ippons re-opens as 2–2 and one declared in encho as 1–1: counting
// alone would read those as tied, which left the band rendering
// "Last:  beat  (fusensho)" with both names blank, and could even hand the
// bout to the side that withdrew. The fusensho side wins regardless of the
// cells. Widen or narrow this rule in ONE place only.
// hanteiSide is the same class of input as fusenshoSide: a decision that names
// the winner over what the cells read. It applies ONLY to a tied scoreline
// (FIK 7-5 / 29-6, mirrored by validation.go's equal-ippon-counts gate) and it
// beats an operator draw flag: a hantei declares a winner, so it un-draws the
// bout however level the scoreline. It also outranks fusenshoSide on tied
// cells (a stale reopened hantei plus a new fusensho keeps the hantei side) —
// not a new choice, the same precedence the old daihyosenWinner overlay had;
// stated here so the ordering reads as intentional. Folding it in HERE (rather than overlaying
// it at consumers) is what keeps every reader of subTotals[i].winner — the
// centre chip colouring, the draw derivation, the save path — agreeing that a
// hantei-decided bout is decided.
export function boutWinnerSide({ aCount = 0, bCount = 0, draw = false, fusenshoSide = "", hanteiSide = "" } = {}) {
  if ((hanteiSide === "a" || hanteiSide === "b") && aCount === bCount) return hanteiSide;
  if (draw) return null;
  if (fusenshoSide === "a" || fusenshoSide === "b") return fusenshoSide;
  if (aCount > bCount) return "a";
  if (bCount > aCount) return "b";
  return null;
}

// teamEncounterHasResult: has any counting bout produced a landed result?
// IV/PW totals capture decisive bouts and scored draws, but a drawn (hikiwake)
// bout scored 0–0 contributes neither while still being a real result. Without
// counting those, a KO encounter tied solely on 0–0 draws would read pending
// ("-") instead of "DAIHYOSEN". The daihyosen row is excluded to mirror the
// IV/PW totals (it is the tiebreaker, not a counting bout).
export function teamEncounterHasResult({ ivA, ivB, pwA, pwB, subTotals, daihyosenIdx }) {
  if ((ivA + ivB + pwA + pwB) > 0) return true;
  return (subTotals || []).some((s, i) => i !== daihyosenIdx && s.draw);
}

// teamEncounterIsLevel: some bout has landed (a point, a win or a draw) and the
// two sides finish level on IV and PW. The one spelling of "tied" the daihyosen
// control and the league line both ask, so they cannot disagree. A bout counts
// as decided once it carries any ippon or is a draw: a 5-person tie reaches
// even IV only through a drawn bout (the bug the old `allComplete && tied` gate
// had).
export function teamEncounterIsLevel({ ivA, ivB, pwA, pwB, subTotals }) {
  const anyBoutDecided = (subTotals || []).some(t => t.aTotal > 0 || t.bTotal > 0 || t.draw || t.winner !== null);
  return anyBoutDecided && ivA === ivB && pwA === pwB;
}

// fusenshoSideFromSub: which side ("a" / "b" / "") a persisted fusensho sub-bout
// was awarded to, for re-seeding the local editor state on a reopen or remount.
// The winner is stored as the bout competitor's OWN name — for a KACHINUKI bout
// that is the PLAYER name (resolveKachinukiBoutSides), which never equals the
// match-level team names, so match the sub's own sideA/sideB (correct for a
// fixed-position bout too: there winner === sideA === the team name). Fall back
// to the maru (○) pattern applyFusenshoToggle writes into the winner's ippons,
// for legacy rows that carry no per-bout sides. mp-gmcg review: the earlier
// match against the team-level sideAName/sideBName silently dropped a reopened
// kachinuki fusensho (its player-name winner matched neither team name), so the
// "(fusensho)" affordance vanished on every Reopen and Record-bout remount.
export function fusenshoSideFromSub(sub) {
  if (!sub || sub.decision !== "fusensho") return "";
  const allMaru = (arr) => Array.isArray(arr) && arr.length > 0 && arr.every(x => x === DEFAULT_WIN_IPPON);
  // bc-pnum: through the shared owner, so the row's member ids decide and two
  // fighters sharing a display name fall through to the maru test below
  // rather than being resolved by name order. That fall-through is the point:
  // the maru cells are real evidence of who was awarded the bout, and the old
  // name arms short-circuited them with a guess.
  const side = attributeWinnerSide(subBoutAttribution(sub));
  if (side) return side;
  if (allMaru(sub.ipponsA) && !allMaru(sub.ipponsB)) return "a";
  if (allMaru(sub.ipponsB) && !allMaru(sub.ipponsA)) return "b";
  return "";
}

// fusenshoButtonTitle: what a sub-bout's Fusensho button for side `rs` will
// do. `rs` and `other` are the row's two rowSides entries (key "a"/"b", label
// "AKA"/"SHIRO"). The other side keeps its struck points
// (applyFusenshoToggle), and a refused fusensho (fusenshoAllowed) names the
// side that already won the bout. That win can sit under the winner's own
// fusensho, whose circles are not tapped away, so its undo is named first.
// The pressed Fusensho button's label, the longer of its two: the button keeps
// room for it in both states (.holds-label), so it holds its place.
export const FUSENSHO_PRESSED_LABEL = "✓ Fusensho";

// subBoutHasBeenPlayed: true once a sub-bout carries any operator input
// (ippons, fouls, a per-bout fusensho, an explicit hikiwake, or an encho
// marker: a 0-0 knockout tie sent to encho must stay in the patch or the
// overtime would silently vanish from the wire). Used to drop untouched
// positions from a KACHINUKI patch: the modal maps over all team
// positions, but kachinuki appends bouts dynamically, so emitting unplayed
// positions as 0–0 hikiwake would corrupt advancement (AdvanceKachinuki keys
// off the LAST SubResult having an outcome) and inflate individual-draw
// standings. Team matches keep all positions on the wire, but every bout
// is fought (operator ruling 2026-09-24): an unplayed row goes out with
// decision "" rather than as a hikiwake, and Finish refuses while one
// remains (unfinishedTeamBouts, which asks this predicate per bout).
//
// This is THE single played-bout primitive for kachinuki (operator input
// determines the bout outcome): the wire filter, the Record-bout gate,
// kachinukiVisiblePositions, the encho target, and End-match derivation
// (via buildKachinukiEndEntries) all route through it. Fouls: "2 fouls
// become a point" is applyFoulIncrement's job (the 2nd foul auto-awards
// an H into the opponent's pts and resets the counter), so a live
// counter only ever holds ONE outstanding foul. That lone foul counts
// here as input — the bout was fought (a hansoku was given in it), so at
// End it reads 0-0 = hikiwake, never an unplayed row — but it never
// influences points; only the discharged H does. Widen or narrow this
// predicate in ONE place only.
export function subBoutHasBeenPlayed(s) {
  if (!s) return false;
  return (s.aPts?.length > 0) || (s.bPts?.length > 0) || (s.aFouls > 0) || (s.bFouls > 0) || !!s.fusensho || !!s.draw || (s.encho > 0);
}

// Align a local bout board to the positions the server board currently has,
// matching rows by POSITION (`_pos`) and never by index.
//
// The editor's grid can change shape while it is open, in both directions: a
// kachinuki Record-bout write appends the next pairing, adding a daihyosen
// appends the rep-bout row, deleting one on another device removes it, and a
// late-arriving teamSize moves the numbered count either way. Index alignment
// survives none of those on its own, because the daihyosen sits at the TAIL —
// so a tail change silently re-points a row at a different position. That is
// not a cosmetic problem: buildPatch derives each row's wire `position` from
// its index, so a mis-aligned board sends wrong rows. Two shipped symptoms: a
// stale-long board emitted `position: 4` for a three-person team, and the rep
// bout's ippons were counted as a numbered bout's IV/PW.
//
// Matching on position makes growth, shrink, and a daihyosen appearing or
// vanishing ONE rule instead of three cases. `serverRows` supplies both the
// target shape and the fallback for a position the local board does not have.
//
// Returns `rows` UNCHANGED when it already aligns: rebuilding unconditionally
// would hand a fresh array identity to every consumer on every render, which is
// the mp-gmcg F1 thrash.
//
// Exported for its own test. In the app the adopt merge also re-shapes the
// committed state, so an append-only version would usually look fine — this is
// deliberately not left to depend on that ordering, and the test is what keeps
// the distinction from being quietly refactored away.
export function reconcileRowsToPositions(rows, serverRows) {
  const aligned = rows.length === serverRows.length
    && serverRows.every((ss, i) => rows[i] && rows[i]._pos === ss._pos);
  if (aligned) return rows;
  const byPos = new Map(rows.map(s => [s._pos, s]));
  return serverRows.map(ss => byPos.get(ss._pos) || ss);
}
