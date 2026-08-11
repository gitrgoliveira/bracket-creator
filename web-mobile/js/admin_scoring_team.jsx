// Team-match helpers and TeamScoreEditorModal.
// Private to the scoring module; ScoreEditorModal routes here for team matches.
// Extracted from admin_scoring_modal.jsx (mp-zac3).

const { useState: useStateA, useEffect: useEffectA, useRef: useRefA } = React;

import {
  MAX_IPPONS_PER_SIDE,
  isBoutDecided,
  getIpponButtons,
  getValidPointKeys,
  IpponLegend,
  ScoringShortcutHint,
  applyFusenshoToggle,
  applyFoulIncrement,
  reconcileFoulsAtOpen,
  nextFoulOnDecrement,
  TermAS,
  GlossaryHintAS,
  resolveDecisionPassword,
  assertRunningWritePersisted,
  makeSubmitDecision,
  initialEnchoPeriodsForMatch,
  daihyosenEnchoFields,
  EnchoControl,
  DecisionPrompt,
  RemainingMatchesPanel,
  LineupNameInput,
  ReasonPrompt,
  CORRECTION_PRESETS,
  REOPEN_PRESETS,
} from './admin_scoring_shared.jsx';

import { useDebouncedRunningWrite, SyncStatusPill } from './admin_scoring_autosave.jsx';

// boutMiddle is THE single source for a bout's centre value (vs/X/(E)/(DH));
// the editor derives its per-bout middle from it rather than restating the
// chain (CLAUDE.md § Match Decision Types: the middle rule lives in ONE place).
import { boutMiddle, winnerSideLR } from './bracket.jsx';
import { resultSlot } from './result_slot.jsx';

// renderTeamBoutMiddle: the ONE place the editor turns a sub-bout into its
// centre value, for BOTH the read-only done row and the live entry row. Derives
// vs/X/(E)/(DH) from the single-source boutMiddle (decided-by-points reads "vs"
// like the bracket/scoreboard; the cell ippon letters carry the score). The
// editor's local sub state has no decision string, so synthesise boutMiddle's
// three inputs: a marked/derived tie → hikiwake, the daihyosen row →
// "daihyosen", s.encho (a period count) → {periodCount}. X keeps its dedicated
// styling; vs/(E)/(DH) render as the quiet centre span.
// teamBoutIsDraw: does this bout row read as a hikiwake? Tied-and-scored is the
// derivation, EXCEPT that a hantei declares a winner and so un-draws the bout
// however level the scoreline. Without that exception a 1-1 daihyosen taken to
// hantei derives hikiwake, and boutMiddle puts X in the centre — claiming a draw
// on a knockout bout whose winner is simultaneously wearing Ht. Exported so the
// exception is pinned without mounting the editor.
export function teamBoutIsDraw(s, t, hanteiWinner) {
  if (hanteiWinner) return false;
  return !!(s.draw || (t.winner === null && (t.aTotal > 0 || t.bTotal > 0)));
}

function renderTeamBoutMiddle(s, t, isDaihyoRow, hanteiWinner) {
  const isDraw = teamBoutIsDraw(s, t, hanteiWinner);
  const mid = boutMiddle(
    isDaihyoRow ? "daihyosen" : (isDraw ? "hikiwake" : ""),
    s.encho > 0 ? { periodCount: s.encho } : null,
    isDraw ? { type: "hikiwake" } : null,
  );
  return mid === "X" ? <span className="tsm-draw">X</span> : <span style={{ color: "var(--ink-3)" }}>{mid}</span>;
}

// hanteiSlot: which of this side's two ippon slots carries the "Ht" mark, or -1
// for none. The placement rule itself lives in result_slot.jsx and is shared
// with the viewer/display scoreboard, so the two surfaces cannot drift; this
// adds the editor's "is this the side that won the hantei" test.
//
// It also DELIBERATELY discards resultSlot's `loose` (both slots full). That is
// a decision, not an oversight: unlike the read-only scoreboard, which would
// otherwise lose the result and so renders a loose mark, this editor always
// mounts a second channel for the verdict — daihyosenHanteiArmed is seeded from
// the stored decision, so the hantei row shows the winning side's button as
// primary regardless. And 2-2 is unreachable through the ippon buttons anyway
// (MAX_IPPONS_PER_SIDE plus isBoutDecided disable both sides at 2), so only
// drifted stored data reaches it. Rendering a third slot-shaped chip here would
// claim an ippon that does not exist.
export function hanteiSlot(isWinner, pts) {
  if (!isWinner) return -1;
  return resultSlot(pts).slot;
}

// mp-bkg / mp-13y: resolveMatchLineup and resolveLineupTeamId are now shared
// across all consumer surfaces (admin scoring modal, viewer, TvDisplay,
// StreamingOverlay). The implementations live in lineup_resolver.jsx;
// re-exported here so existing imports from admin_scoring_modal.jsx (which
// re-exports them onward) continue to work.
import { resolveMatchLineup, resolveLineupTeamId, resolveBoutSideName, POS_KEYS_5, POS_LABELS_5 } from './lineup_resolver.jsx';
import { DAIHYOSEN_POSITION } from './pool_ids.jsx';

// Position keys are generated inline in TeamScoreEditorModal (numbered "1".."N")
// from teamSize and any persisted kachinuki bouts; the upper bound everywhere is
// MAX_TEAM_SIZE (admin_helpers.jsx), kept in lockstep with the team-size input
// caps in admin_competition.jsx and admin_setup.jsx.

// T131 helper: human-friendly position label for the team-match scoring
// modal. 5-person teams use the canonical FIK names (POS_LABELS_5 from
// lineup_resolver.jsx, the single source of truth); non-5 sizes use the
// position number.
const POS_ABBREV_BY_INDEX_5 = ["Sen", "Ji", "Chu", "Fuk", "Tai"];
function positionLabelFor(teamSize, index, sub) {
  if (sub && sub.position && typeof sub.position === "string" && sub.position.length > 0 && /[a-z]/i.test(sub.position)) {
    // Backend may emit a name string in Position for non-5 sizes once
    // domain.Position is wire-stable. Use it verbatim when present.
    return sub.position;
  }
  if (teamSize === 5 && index >= 0 && index < 5) return POS_LABELS_5[index];
  return `Match ${index + 1}`;
}
// Short position handle shown beside the bout number. Operators think in
// positions ("Taisho's up"), so for 5-person teams we surface the abbreviation
// in the row itself rather than hiding the full name in a title tooltip
// (unreachable on a touch tablet). Returns "" for sizes/rows with no canonical
// position, where the number alone is the right label.
function positionAbbrevFor(teamSize, index, sub) {
  if (sub && sub.position && typeof sub.position === "string" && /[a-z]/i.test(sub.position)) {
    return sub.position.slice(0, 3);
  }
  if (teamSize === 5 && index >= 0 && index < 5) return POS_ABBREV_BY_INDEX_5[index];
  return "";
}

// teamResultLabel: the RESULT-band / Finish-button verdict text for a team
// encounter. A knockout match cannot be a draw (a tie is broken by a daihyosen,
// FIK rules), so a null winner in the bracket phase never reads "DRAW": it's
// "DAIHYOSEN" once a scored tie exists to break, or "-" before any bout lands.
// Only a pool encounter reads a null winner as a true draw. (a = Aka, b = Shiro.)
//
// KACHINUKI (mp-gmcg): daihyosen does not exist, so a tied knockout kachinuki
// encounter must never read "DAIHYOSEN": the tie is resolved by fighting on
// (next bout or encho on the same pair), so the band stays pending ("-")
// until the operator ends the match. Pools keep "DRAW": that is exactly what
// End match would record there.
export function teamResultLabel({ teamWinner, isKnockoutPhase, hasAnyScore, isKachinuki }) {
  if (teamWinner === "a") return "AKA WIN";
  if (teamWinner === "b") return "SHIRO WIN";
  if (isKnockoutPhase) return hasAnyScore && !isKachinuki ? "DAIHYOSEN" : "-";
  return "DRAW";
}

// isKoTieBlocked: Finish must be blocked while a knockout encounter has no
// winner: the operator has to add and score a daihyosen first. Pool draws stay
// finishable, and an already-completed match (correction flow) is never blocked.
export function isKoTieBlocked({ isKnockoutPhase, teamWinner, isComplete }) {
  return !!isKnockoutPhase && teamWinner === null && !isComplete;
}

// isKachinukiBoutMode: while a kachinuki encounter is being fought (not a
// correction, no legacy daihyosen row) the modal's primary actions are
// [Record bout] (a running write flagged kachinukiBoutFinal that the server
// uses to append the next pairing) and [End match] (an explicit completed
// write whose outcome is derived from the last scored bout, see
// deriveKachinukiEndOutcome). Completion is OPERATOR-LED (mp-gmcg): the
// engine never auto-finalizes, so `isComplete` is the only terminator. A
// sub-result carrying decision "kachinuki-exhaustion" on a RUNNING match
// (e.g. a reopened encounter) must NOT kill bout mode: roster snapshots are
// advisory, only the operator knows whether a team is truly out of players.
// Legacy daihyosen rows keep the Finish/correction semantics (daihyosen does
// not exist in kachinuki; existing rows are rendered defensively only). The
// knockout no-draw rule (isKoTieBlocked) never applies to a bout submit: a
// bout-level hikiwake is a legitimate result; End match carries its own
// knockout-tie gate instead.
export function isKachinukiBoutMode({ isKachinuki, isComplete, hasDaihyosen }) {
  return !!isKachinuki && !isComplete && !hasDaihyosen;
}

// canReopenKachinukiMatch: [Reopen match] renders ONLY on a completed
// KACHINUKI team match (mp-gmcg mistake recovery: status back to running,
// winner/decision cleared, bout log kept, more bouts addable). The backend
// endpoint 400s non-kachinuki competitions, whose only sanctioned edit of a
// finished result remains the correction path, so the button must not
// render for them at all.
export function canReopenKachinukiMatch({ isKachinuki, isComplete }) {
  return !!isKachinuki && !!isComplete;
}

// isKachinukiBoutRemovable: whether the [× Remove this bout] undo renders for
// the current bout. True only when the encounter is in bout mode, the current
// bout is UNSCORED, and a prior bout WAS scored (lastScoredIdx >= 0) — so the
// current bout is an appended EXTRA, never the bootstrap bout 1. That is exactly
// the row the End-match strip (stripTrailingUnscoredKachinukiBouts) would drop,
// so the button just makes the implicit strip explicit and reversible on the
// spot. mp-gmcg.
export function isKachinukiBoutRemovable({ boutMode, currentBoutPlayed, lastScoredIdx }) {
  return !!boutMode && !currentBoutPlayed && lastScoredIdx >= 0;
}

// boutWinnerSide: THE single home for "which side won this sub-bout".
// Returns "a" (Aka), "b" (Shiro), or null when the bout has no winner.
// One rule, three surfaces — the IV/PW tally and persisted bout winner
// (subTotals), the kachinuki band's last-bout fact (kachinukiBandModel),
// and the End-match derivation (deriveKachinukiEndOutcome) all call this,
// so they can never disagree about who won a bout.
//
// Order matters. An operator-marked hikiwake has no winner at all. A
// FUSENSHO is a DEFAULT WIN (FIK Art. 32): the decision names the winner,
// the maru cells (○○, or a single ○ in encho) are merely the score board's
// marking of it, and the loser's already-struck ippons are PRESERVED
// (preserveLoserScore, engine/scoring.go). So a walkover whose loser kept
// two ippons re-opens as 2–2 and one declared in encho as 1–1: counting
// alone would read those as tied, which left the band rendering
// "Last:  beat  (fusensho)" with both names blank, and could even hand the
// bout to the side that withdrew. The fusensho side wins regardless of the
// cells. Widen or narrow this rule in ONE place only.
export function boutWinnerSide({ aCount = 0, bCount = 0, draw = false, fusenshoSide = "" } = {}) {
  if (draw) return null;
  if (fusenshoSide === "a" || fusenshoSide === "b") return fusenshoSide;
  if (aCount > bCount) return "a";
  if (bCount > aCount) return "b";
  return null;
}

// buildKachinukiEndEntries: maps the modal's LOCAL sub state to the
// wire-shaped entries deriveKachinukiEndOutcome consumes, keeping ONLY
// bouts that carry operator input — decided by subBoutHasBeenPlayed, the
// single played-bout primitive (it also gates the wire filter, the Record
// button, visible positions, and the encho target, so every kachinuki
// surface agrees on which bout is "last"). The daihyosen row is dropped
// here (daihyosen does not exist in kachinuki); untouched auto-appended /
// manual placeholder rows are dropped exactly as they are dropped from
// the wire.
//
// FUSENSHO: the entries carry decision "fusensho" but no winner NAME (the
// local sub state has no names for a bout the server has not paired yet),
// so they also carry fusenshoSide — the side the operator marked — and
// boutWinnerSide resolves the winner from that decision rather than from the
// ippon counts. Counting alone is not enough: applyFusenshoToggle
// (admin_scoring_shared.jsx) writes the ○○ maru into the winning side's pts,
// but a walkover re-opened from the server arrives with the loser's
// preserved ippons too and can read 2–2 / 1–1. fusenshoSide is local to this
// derivation and never reaches the wire (buildPatch builds the wire rows).
export function buildKachinukiEndEntries(subs, daihyosenIdx) {
  return (subs || [])
    .map((s, idx) => ({ s, idx }))
    .filter(({ s, idx }) => idx !== daihyosenIdx && subBoutHasBeenPlayed(s))
    .map(({ s, idx }) => ({
      position: idx + 1,
      ipponsA: s.aPts,
      ipponsB: s.bPts,
      decision: s.draw ? "hikiwake" : s.fusensho ? "fusensho" : "",
      fusenshoSide: s.draw ? "" : (s.fusensho || ""),
      encho: s.encho > 0 ? { periodCount: s.encho } : undefined,
    }));
}

// deriveKachinukiEndOutcome: context-derived outcome for the [End match]
// action (mp-gmcg, spec 006 "Resolved decisions" #2). NO picker, ever:
// OPERATOR INPUT DETERMINES THE BOUT OUTCOME — the last bout carrying any
// operator input decides the encounter. If that bout has a winner (more
// ippons, or a fusensho), that team wins; if it does not (explicit
// hikiwake, equal ippons, a pending 0-0 encho, or ONLY fouls: a bout
// fought to time with no ippon is a hikiwake), the encounter is tied on
// that bout. There is no separate "has an outcome" notion: outcomes exist
// only as operator input, so the app never skips back past an
// input-bearing bout when ending a match.
//
// PRECONDITION: entries are wire-shaped sub-results ({position, sideA,
// sideB, ipponsA, ipponsB, winner, decision, encho}) that already carry
// operator input — production callers build them with
// buildKachinukiEndEntries (above), whose subBoutHasBeenPlayed filter is
// the single source of that judgement. This function only drops
// daihyosen (-1) / legacy non-positive sentinel rows. Returns:
//   {kind:"win", winnerSide:"a"|"b"}          last bout has a winner
//                                             (score, fusensho, or a winner
//                                             name matching a side)
//   {kind:"draw"}                             last bout tied, pools/
//                                             league: drawn encounter
//   {kind:"blocked", reason:"knockout-tie"}   tied in a knockout: continue
//                                             (next bout or encho)
//   {kind:"blocked", reason:"no-bouts"}       nothing recorded yet
export function deriveKachinukiEndOutcome({ subResults, isKnockoutPhase }) {
  const scored = (subResults || [])
    .filter(e => e && e.position > 0)
    .sort((x, y) => x.position - y.position);
  if (scored.length === 0) return { kind: "blocked", reason: "no-bouts" };
  const last = scored[scored.length - 1];
  // The side rule is NOT re-spelled here: boutWinnerSide is its one home.
  let side = boutWinnerSide({
    aCount: (last.ipponsA || []).length,
    bCount: (last.ipponsB || []).length,
    draw: last.decision === "hikiwake",
    fusenshoSide: last.fusenshoSide,
  });
  if (side === null && last.decision !== "hikiwake" && last.winner) {
    // Layered ON TOP of the side rule, not a second copy of it: a
    // server-recorded bout may name its winner by STRING with nothing a
    // side rule can key off (a fusensho persisted without maru cells and
    // without a marked side). Map that name back to a side when possible.
    if (last.sideA && last.winner === last.sideA) side = "a";
    else if (last.sideB && last.winner === last.sideB) side = "b";
  }
  if (side) return { kind: "win", winnerSide: side };
  if (isKnockoutPhase) return { kind: "blocked", reason: "knockout-tie" };
  return { kind: "draw" };
}

// kachinukiEndOutcomeLabel: the ONE home for the [End match] verdict wording.
// deriveKachinukiEndOutcome owns the OUTCOME; this owns its DISPLAY STRING so
// the two End-match surfaces (the reopen verdict-preview and the End/Confirm
// button) can never drift. The win wording defers to teamResultLabel, the
// canonical "AKA WIN"/"SHIRO WIN" source; a tied end reads "Draw (hikiwake)".
export function kachinukiEndOutcomeLabel(outcome) {
  if (outcome?.kind === "win") return teamResultLabel({ teamWinner: outcome.winnerSide });
  return "Draw (hikiwake)";
}

// kachinukiEnchoAvailable: whether the Encho (same pair fights on)
// affordance renders for the current End-match outcome. Available for
// EVERY tied last bout — knockout (End is blocked, overtime is one of the
// two ways forward) AND pools/league (End would record a drawn encounter,
// but whether the pairing must produce a result, e.g. the taisho must be
// defeated, is OPERATOR DISCRETION, never derived from the phase). Not
// available when nothing is recorded or when the last bout already has a
// winner.
export function kachinukiEnchoAvailable(outcome) {
  if (!outcome) return false;
  return outcome.kind === "draw" || (outcome.kind === "blocked" && outcome.reason === "knockout-tie");
}

// kachinukiBandModel: content model for the summary band in KACHINUKI mode
// (light instrument panel, user-confirmed brief 2026-08-02). The band shows
// BOUT-LOG FACTS ONLY — never roster inferences (the roster is advisory by
// operator ruling) and NEVER a verdict while the match is running (the old
// IV/PW-derived "AKA WIN" contradicted the End gate mid-match, critique P1).
// The verdict returns only on completion, derived from the MATCH winner —
// the last bout decides kachinuki, never the IV lead.
//
// Inputs: subs = the modal's local sub state (subBoutHasBeenPlayed is the
// single played-bout primitive); serverSubs = m.subResults (bout-log names);
// currentBout = the visible bout position (1-based). Pure and unit-tested.
// Returns { headline, fact } while running, { headline, verdict,
// verdictSide: "aka"|"shiro"|"draw" } when complete.
export function kachinukiBandModel({ subs, daihyosenIdx, isComplete, matchWinner, matchDecision, sideA, sideB, currentBout, namesAt }) {
  // The display string for the "unattributable winner" fallback below, kept
  // separate from `matchWinner` itself: winnerSideLR needs the RAW (possibly
  // {id,name}) form to prefer id equality over name (two teams CAN share a
  // display name), so matchWinner is passed through unflattened.
  const matchWinnerName = matchWinner && typeof matchWinner === "object" ? matchWinner.name : matchWinner;
  const played = [];
  (subs || []).forEach((s, idx) => {
    if (idx === daihyosenIdx || !subBoutHasBeenPlayed(s)) return;
    // Names come from the caller's resolver — the modal passes the SAME
    // primitive the bout rows render with (playerNamesForBout: local
    // override → server log → lineup), so the band can never disagree
    // with the row above it. Anonymous bouts degrade to side labels.
    const names = namesAt ? namesAt(idx) : {};
    const aName = names.aName || "";
    const bName = names.bName || "";
    const aN = (s.aPts || []).length;
    const bN = (s.bPts || []).length;
    // Winner/loser derive from the SIDE, never from name comparison — an
    // unresolved name must not collapse both sides onto one label. The side
    // itself comes from boutWinnerSide (the one winner rule), so a fusensho
    // whose maru happen to tie the loser's preserved score still names its
    // winner instead of rendering "Last:  beat  (fusensho)".
    const winnerSide = boutWinnerSide({ aCount: aN, bCount: bN, draw: s.draw, fusenshoSide: s.fusensho });
    played.push({
      winner: winnerSide === "a" ? (aName || "Aka") : winnerSide === "b" ? (bName || "Shiro") : "",
      loser: winnerSide === "a" ? (bName || "Shiro") : winnerSide === "b" ? (aName || "Aka") : "",
      // "No winner" IS the tie, so it reads off the same rule: an explicit
      // hikiwake or equal counts with no fusensho to attribute them to.
      tie: winnerSide === null,
      fusensho: !!s.fusensho,
    });
  });

  if (isComplete) {
    const n = played.length;
    const headline = `FINAL · ${n} BOUT${n === 1 ? "" : "S"}`;
    // Winner wording defers to teamResultLabel (the one "AKA WIN"/"SHIRO WIN"
    // home). The raw-name fallback keeps an unattributable winner from being
    // silently dropped. winSide reuses winnerSideLR (bracket.jsx) rather than
    // re-deriving name-equality here: it prefers id equality over name, which
    // bare-name comparison cannot (review: two teams CAN share a display name).
    const lr = winnerSideLR({ winner: matchWinner, sideA, sideB });
    const winSide = lr === "right" ? "a" : lr === "left" ? "b" : null;
    if (winSide) return { headline, verdict: teamResultLabel({ teamWinner: winSide }), verdictSide: winSide === "a" ? "aka" : "shiro" };
    if (matchDecision === "hikiwake" || !matchWinner) return { headline, verdict: "DRAW", verdictSide: "draw" };
    return { headline, verdict: String(matchWinnerName).toUpperCase(), verdictSide: "draw" };
  }

  const headline = `BOUT ${currentBout || played.length + 1}`;
  const last = played[played.length - 1];
  if (!last) return { headline, fact: "" };
  if (last.tie) return { headline, fact: "Last: hikiwake · both retired" };
  // Streak: consecutive trailing wins by the same fighter — a pure bout-log
  // fact ("stays on" is winner-stays semantics, not a roster claim).
  let streak = 0;
  for (let i = played.length - 1; i >= 0; i--) {
    if (played[i].winner && played[i].winner === last.winner) streak++;
    else break;
  }
  const how = last.fusensho ? " (fusensho)" : "";
  const stay = streak >= 2 ? ` · stays on, ${streak} wins` : " · stays on";
  // "Last:" prefix mirrors the tie-fact ("Last: hikiwake …") so the headline
  // "BOUT N" is never misread as bout N's own (unfought) result (critique P2).
  return { headline, fact: `Last: ${last.winner} beat ${last.loser}${how}${stay}` };
}

// kachinukiVisiblePositions: which bout slots to render for a kachinuki
// match. The server bout log (m.subResults) is the source of truth for
// which bouts exist, with two carve-outs:
//
//   - Bootstrap: the server never creates bout 1 (MaybeAdvanceKachinuki
//     only APPENDS bouts 2+ after the first recorded bout), so a fresh
//     match with no positive-position entries shows position 1: the
//     senpo pairing resolves from the lineup.
//   - Daihyosen: the position DAIHYOSEN_POSITION rep bout is a server row and the
//     actionable slot for a tied encounter; it is always visible when
//     present.
//
// Running (non-correction): only the current bout, the first server bout
// the operator has not scored yet (isPlayedAt on its canonical index in
// `positions`), else the last one. Completed (correction): every server
// bout so any of them can be edited.
export function kachinukiVisiblePositions({ positions, daihyosenIdx, subResults, isComplete, isPlayedAt }) {
  const subs = subResults || [];
  let slots = positions.filter((_, i) => i !== daihyosenIdx && subs.some(sr => sr.position === i + 1));
  if (slots.length === 0 && positions.length > 0 && daihyosenIdx !== 0) {
    slots = [positions[0]]; // bootstrap: bout 1 on a fresh match
  }
  const daihyosenSlot = daihyosenIdx >= 0 ? [positions[daihyosenIdx]] : [];
  if (isComplete) return [...slots, ...daihyosenSlot];
  let cur = slots.length - 1;
  for (let i = 0; i < slots.length; i++) {
    if (!isPlayedAt(positions.indexOf(slots[i]))) { cur = i; break; }
  }
  return [...slots.slice(cur, cur + 1), ...daihyosenSlot];
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

// resolveKachinukiBoutSides: competitor identity for a KACHINUKI sub-bout.
// Unlike a fixed-position encounter (settled on IV/PW at the match level, where
// computeStandings matches the match-level side first via isWinForSide), a
// kachinuki bout is consumed per-competitor: engine.AdvanceKachinuki compares
// the bout winner against sideA/sideB to decide who stays on, and the bout-log
// export prints those names. So a kachinuki bout must persist the INDIVIDUAL
// player names and a player-name winner: never the team name. When the lineup
// is unknown the sides are left empty and the winner falls back to the team
// name, the same "sides empty when unknown" contract the backend's quick-score
// path documents (handlers_match.go). Fixed-position and daihyosen bouts keep
// their existing team-name behaviour and do not call this.
export function resolveKachinukiBoutSides({ aName, bName, wKey, teamWinnerName }) {
  const sideA = aName || "";
  const sideB = bName || "";
  let winner = "";
  if (wKey === "a") winner = aName || teamWinnerName || "";
  else if (wKey === "b") winner = bName || teamWinnerName || "";
  return { sideA, sideB, winner };
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
  const allMaru = (arr) => Array.isArray(arr) && arr.length > 0 && arr.every(x => x === "○");
  if (sub.winner && sub.winner === sub.sideA) return "a";
  if (sub.winner && sub.winner === sub.sideB) return "b";
  if (allMaru(sub.ipponsA) && !allMaru(sub.ipponsB)) return "a";
  if (allMaru(sub.ipponsB) && !allMaru(sub.ipponsA)) return "b";
  return "";
}

// subBoutHasBeenPlayed: true once a sub-bout carries any operator input
// (ippons, fouls, a per-bout fusensho, an explicit hikiwake, or an encho
// marker: a 0-0 knockout tie sent to encho must stay in the patch or the
// overtime would silently vanish from the wire). Used to drop untouched
// positions from a KACHINUKI patch: the modal maps over all team
// positions, but kachinuki appends bouts dynamically, so emitting unplayed
// positions as 0–0 hikiwake would corrupt advancement (AdvanceKachinuki keys
// off the LAST SubResult having an outcome) and inflate individual-draw
// standings. Fixed-position matches keep all positions: a 0–0 there is a
// legitimate hikiwake.
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

export function TeamScoreEditorModal({ match, teamSize, onClose, onSubmit, onSubmitAndNext, onAfterDecision, prevMatch, nextMatch, onPrev, onNext, password, selfReport, variant = "modal", canClose = true }) {
  // mp-gmcg: a successful [× Remove this bout] shrinks the SERVER bout log, but
  // the parent does NOT refresh the openMatch snapshot on an out-of-band
  // mutation (SSE refreshes the LIST, not this prop — the same reason
  // Record-bout growth rides onSubmit's return, see admin_schedule_score_editor).
  // matchOverride shadows the prop so the removed bout disappears at once. It is
  // cleared whenever the parent passes a genuinely new match object (the next
  // Record / prev / next), after which the prop is authoritative again.
  const [matchOverride, setMatchOverride] = useStateA(null);
  // Clear the override once the prop actually MOVES off the pre-removal state —
  // a genuine match switch (id) OR the bout-log length changing (the parent
  // catching up to the removal, or a later Record-bout re-growing it). Keying
  // on the object identity alone (`[match]`) cleared on every same-content SSE
  // reload, so the removed bout flashed back; keying on id alone would never
  // clear on a same-id Record-bout and the stale shorter override would then
  // shadow the freshly-grown log (mp-gmcg review F4).
  useEffectA(() => { setMatchOverride(null); }, [match?.id, (match?.subResults || []).length]);
  // mp-gmcg: never carry an open past-bout correction across a match SWITCH,
  // but DO survive a same-match reload. Autosave persists each correction as a
  // running write, which round-trips back over SSE as a fresh `match` object
  // (same id); keying on match?.id — not the object ref — keeps the editor open
  // across that reload instead of collapsing it after every ippon change.
  useEffectA(() => { setEditingDoneBoutIdx(-1); editingDoneOriginalRef.current = null; }, [match?.id]);
  const m = matchOverride || match;
  const isComplete = m.status === "completed";
  // Kachinuki appends bouts beyond teamSize (engine assigns Position =
  // len(SubResults)+1, up to 2*roster-1 bouts), so size the grid to cover every
  // persisted bout position: otherwise reopening a kachinuki match hides (and
  // can't score) the later bouts. A fixed-position match never persists a
  // position past teamSize, so it is unaffected. Capped at the theoretical
  // kachinuki maximum (2*MAX_TEAM_SIZE-1) as a guard against malformed data.
  const maxSubPos = (m.subResults || []).reduce((mx, s) => (s.position > 0 && s.position > mx ? s.position : mx), 0);
  // mp-gmcg: bout positions the OPERATOR added client-side ("Add next bout
  // manually", kachinuki only). The server auto-append can only pair fighters
  // it knows from lineups + the bout log, and team sizes are unregulated, so
  // the operator can append the next pairing locally: the row rides the next
  // score write as a regular subResult (the server merge keeps client rows).
  const [manualBouts, setManualBouts] = useStateA([]);
  const kachinukiMaxBouts = 2 * window.MAX_TEAM_SIZE - 1;
  const manualMaxPos = manualBouts.length ? Math.max(...manualBouts) : 0;
  // The ONE clamp defining how many bout slots the grid covers: teamSize is
  // the floor, the highest known position (server log or a manually-added
  // row) extends it, kachinukiMaxBouts is the theoretical ceiling. Reused by
  // removeCurrentBout below to size subsRaw back down after a removal — the
  // two must never drift, since a mismatch is exactly the F1 freeze bug
  // (review: this and the removal-time clamp used to be two independent
  // copies of the same formula).
  const clampPositionCount = (maxSub, manualMax) => Math.min(Math.max(teamSize, maxSub, manualMax), kachinukiMaxBouts);
  const positionCount = clampPositionCount(maxSubPos, manualMaxPos);
  const numberedPositions = Array.from({ length: positionCount }, (_, i) => String(i + 1));
  // mp-4pc: a persisted daihyosen (representative bout) lives in
  // SubResults at wire position DAIHYOSEN_POSITION. It is scored "like any other
  // sub-match" (handlers_daihyosen.go) but is NOT an individual victory: 
  // it breaks an IV/PW tie. Render it as a trailing scoreable row,
  // exclude it from the IV/PW tally, and let its winner decide the
  // encounter. The "daihyosen" slot sentinel maps to DAIHYOSEN_POSITION in
  // buildPatch. It is the ONLY team sub-bout that may carry encho/hantei
  // (validation.go validateSubBout).
  const existingDaihyosen = (m.subResults || []).find(s => s.position === DAIHYOSEN_POSITION);
  const hasDaihyosen = !!existingDaihyosen;
  const positions = hasDaihyosen ? [...numberedPositions, "daihyosen"] : numberedPositions;
  const daihyosenIdx = hasDaihyosen ? numberedPositions.length : -1;
  // FR-033: encho counter for team matches (overtime period count rides
  // alongside the score on the wire: same shape as ScoreEditorModal).
  // mp-4pc: derive from the daihyosen sub when present: see
  // initialEnchoPeriodsForMatch for why. Captured in a const so isDirty
  // can compare against the initial value (the function is not idempotent
  // across re-renders because m may mutate).
  const initialEnchoPeriods = initialEnchoPeriodsForMatch(m);
  const [enchoPeriodCount, setEnchoPeriodCount] = useStateA(initialEnchoPeriods);
  const [submitting, setSubmitting] = useStateA(false);
  // T093–T098: decision state: same shape as the individual editor. See the
  // ScoreEditorModal copy for the contract.
  const [decisionPromptKind, setDecisionPromptKind] = useStateA("");
  const [decisionSubmitting, setDecisionSubmitting] = useStateA(false);
  const [decisionErr, setDecisionErr] = useStateA("");
  const [withdrawnPlayer, setWithdrawnPlayer] = useStateA(null);
  // Audit reason collected when correcting a completed team match, or when
  // closing out a REOPENED one: mirrors the ScoreEditorModal correction flow
  // (same ReasonPrompt), and rides the completing write as correctionReason
  // either way. One reason value, because the wire field is one field.
  const [correctionReason, setCorrectionReason] = useStateA("");
  // Which audit prompt owns the footer: "" | "correction" | "reopen". Both are
  // the same ReasonPrompt and both ARE the confirm step of a high-stakes
  // write, so exactly one may ever be on screen: a single selector makes that
  // true by construction rather than by a pair of flags that must be kept
  // from overlapping.
  const [reasonPromptKind, setReasonPromptKind] = useStateA("");
  // mp-gmcg: [Reopen match] on a completed kachinuki match. Reopening is ONE
  // TAP (operator ruling): the operator who ended a match by mistake is at the
  // shiaijo with the competitors still standing there, so nothing may stand
  // between them and getting back into the encounter. The justification is
  // collected on the way OUT instead (see reopenReasonRequired below).
  // 409s from the server ("not completed" / "downstream match already
  // fought") surface inline; the court-busy 409 gets an actionable remedy
  // panel instead of a dead end (reopenConflict).
  const [reopenBusy, setReopenBusy] = useStateA(false);
  const [reopenErr, setReopenErr] = useStateA("");
  // mp-gmcg: [Remove this bout] busy/error, kept separate from the reopen
  // channel above (that one is the completed-match flow; this is a running-
  // match empty-bout undo).
  const [removingBout, setRemovingBout] = useStateA(false);
  const [removeBoutErr, setRemoveBoutErr] = useStateA("");
  // mp-gmcg: full inline edit of a bout ALREADY fought during a RUNNING
  // kachinuki encounter. Tapping a read-only past bout reopens it in the same
  // scoring controls as the current bout; the debounced autosave persists the
  // change as an unflagged running write the server merges BY POSITION (no
  // advancement, no truncation — the bout log is the source of truth and every
  // bout/participant stays recorded). editingDoneBoutIdx is the canonical index
  // of the past bout being corrected, or -1. editingDoneOriginalRef snapshots
  // that bout's outcome on open so a correction that FLIPS who won can warn the
  // operator that the later bouts (fought on the old result) need re-checking —
  // the app never restacks them itself, because only the courtside operator
  // knows the real later results.
  const [editingDoneBoutIdx, setEditingDoneBoutIdx] = useStateA(-1);
  const editingDoneOriginalRef = useRefA(null);
  // The structured court_busy 409, unpacked: { court, matchId, compId,
  // message } describing the match ALREADY RUNNING on this court. Non-null
  // means the remedy panel is on screen.
  const [reopenConflict, setReopenConflict] = useStateA(null);
  // Best-effort "Shiro vs Aka" for the blocking match. The operator is about
  // to wipe that match's score, so naming the competitors (not just the
  // server's opaque match id) is a safety property, not decoration. Empty
  // until/unless the lookup lands; the panel falls back to the id.
  const [blockerLabel, setBlockerLabel] = useStateA("");
  // T131: lineup data so each bout cell can show the assigned player
  // name + canonical position label. Falls back gracefully when the
  // lineup hasn't been submitted yet (404 → null).
  const [lineupA, setLineupA] = useStateA(null);
  const [lineupB, setLineupB] = useStateA(null);
  // T136 / T141: competition lookup so we can branch on teamMatchType
  // ("kachinuki" vs "fixed") and gate the daihyosen affordance on the
  // knockout-format precondition. Falls back to compKind/teamSize when
  // the fetch fails so the existing fixed-grid flow still works.
  const [compMeta, setCompMeta] = useStateA(null);
  // T141: error banner mapping for the daihyosen POST. Server returns
  // 400 not_tied / 400 pool_match / 409 insufficient_eligibility: see
  // handlers_daihyosen.go for the canonical strings.
  const [editorErr, setEditorErr] = useStateA(""); // inline error surface: daihyosen + lineup saves
  const [daihyosenBusy, setDaihyosenBusy] = useStateA(false);
  // mp-4pc: the daihyosen is the only team sub-bout that may be decided
  // by hantei (judges' decision on a tied bout, FIK 7-5 / 29-6: encho
  // optional). Mirrors the individual ScoreEditorModal hantei flow but scoped to the
  // position DAIHYOSEN_POSITION row. "" = score-decided; "a"/"b" = hantei winner side.
  const initialDaihyosenHantei = existingDaihyosen?.decidedByHantei
    ? (existingDaihyosen.winner === (typeof m.sideA === "object" ? m.sideA?.name : m.sideA) ? "a" : "b")
    : "";
  const [daihyosenHantei, setDaihyosenHantei] = useStateA(initialDaihyosenHantei);
  const [daihyosenHanteiArmed, setDaihyosenHanteiArmed] = useStateA(!!initialDaihyosenHantei);
  // Same teardown-race guard as ScoreEditorModal: covers external/
  // parent-driven unmount during in-flight save.
  const mountedRef = useRefA(true);

  // C1: debounced autosave refs (same pattern as ScoreEditorModal).
  // Updated after buildPatch is defined below.
  const _autosaveIsRunningRef = useRefA(false);
  const _autosaveBuildPatchRef = useRefA(null);
  const _autosaveOnSubmitRef = useRefA(null);
  const { markDirty: markScoringDirty, cancelDebounce: cancelScoringDebounce } = useDebouncedRunningWrite({
    isRunningRef: _autosaveIsRunningRef,
    buildPatchRef: _autosaveBuildPatchRef,
    onSubmitRef: _autosaveOnSubmitRef,
    mountedRef,
  });

  // T141: remove an unscored daihyosen placeholder. Defined at component
  // level so both the hantei row and any other affordance can call it.
  const onRemoveDaihyosen = async () => {
    setEditorErr("");
    setDaihyosenBusy(true);
    try {
      await window.API.removeDaihyosen(m.compId, m.id, resolveDecisionPassword(password));
      if (!mountedRef.current) return;
      onClose();
    } catch (e) {
      if (!mountedRef.current) return;
      const msg = String(e?.message || "");
      let userMsg = msg;
      if (msg === "daihyosen_scored") userMsg = "Clear the daihyosen score before removing it";
      else if (msg === "no_daihyosen") userMsg = "No daihyosen to remove";
      setEditorErr(userMsg);
    } finally {
      if (mountedRef.current) setDaihyosenBusy(false);
    }
  };
  useEffectA(() => () => { mountedRef.current = false; }, []);

  // Fetch lineup + competition data on mount. Both endpoints are
  // read-only and idempotent; failures degrade gracefully (the modal
  // still functions, just without position labels / kachinuki mode).
  useEffectA(() => {
    let cancelled = false;
    if (!m.compId) return;
    // compMatches injects m.roundIndex (0-based) for bracket matches, and
    // m.round as a string label for display ("R16", "Quarterfinals", ...).
    // resolveRoundIndex prefers roundIndex, falls back for legacy shapes.
    // Pool matches return 0 (no per-round lineup).
    const round = window.resolveRoundIndex(m);
    // Side keys are NAME-keyed (api_serializers.buildPlayerMap sets id =
    // name); lineups are stored under the participant's real id (UUID).
    const sideAKey = m.sideA?.id || m.sideA?.name || (typeof m.sideA === "string" ? m.sideA : "");
    const sideBKey = m.sideB?.id || m.sideB?.name || (typeof m.sideB === "string" ? m.sideB : "");
    (async () => {
      // Competition detail for teamMatchType + format AND the participant
      // list used to map the name-keyed sides to their real lineup ids.
      // fetchCompetitionDetails already exists and is cheap.
      let detail = null;
      try {
        detail = await window.API.fetchCompetitionDetails(m.compId);
        if (cancelled) return;
        setCompMeta(detail || null);
      } catch (e) {
        // Soft-fail: kachinuki/daihyosen UI just won't render.
        console.warn("Competition fetch for team modal failed:", e);
      }
      // mp-bkg: prefer per-match lineup (GET match-lineups/:matchId); fall
      // back to round lineup when no per-match entry exists (404 → null →
      // round lookup). Map the name-keyed side to the participant id the
      // lineup is stored under first: otherwise every GET 404s.
      // The detail payload carries participants under config.players; the
      // top-level players array is often an empty (but truthy) [] in this
      // shape, so prefer whichever list is non-empty.
      const players =
        (detail && detail.players && detail.players.length ? detail.players : null)
        || (detail && detail.config && detail.config.players)
        || [];
      const teamAId = resolveLineupTeamId(sideAKey, players);
      const teamBId = resolveLineupTeamId(sideBKey, players);
      if (teamAId) {
        const l = await resolveMatchLineup(m.compId, teamAId, m.id, round, window.API);
        if (!cancelled) setLineupA(l);
      }
      if (teamBId) {
        const l = await resolveMatchLineup(m.compId, teamBId, m.id, round, window.API);
        if (!cancelled) setLineupB(l);
      }
    })();
    return () => { cancelled = true; };
  }, [m.compId, m.id]);

  // T136: kachinuki branch. Match-level teamMatchType (added by
  // viewer.compMatches in a sibling slice) is preferred; competition
  // fetch is the fallback. Default "fixed" preserves the legacy N×1
  // grid behaviour.
  const teamMatchType = m.teamMatchType || compMeta?.config?.teamMatchType || "fixed";
  const isKachinuki = teamMatchType === "kachinuki";
  // Compact "Instrument Panel" mode fits the modal on one viewport page
  // for ≤5-person teams. Kachinuki renders only the current bout while
  // running (see kachinukiVisiblePositions), so it always fits even
  // with a 9-person roster. Larger fixed-format
  // teams keep the roomier layout and use .team-bouts-scroll for
  // independent bout-list scrolling.
  const useCompact = teamSize <= 5 || isKachinuki;
  // T141: daihyosen is knockout-only: pool matches resolve ties via
  // the standings tiebreak, not a representative bout. Format comes
  // from match-level compFormat (when set by compMatches) or the comp
  // fetch fallback. Phase === "bracket" is the in-modal signal.
  const compFormat = m.compFormat || compMeta?.config?.format || "";
  const isNaginataTeam = !!compMeta?.config?.naginata;
  // Knockout phase = a bracket match. A POOL match is never knockout, even in a
  // mixed/playoffs competition: pool team matches may legitimately draw
  // (hikiwake) and resolve ties via the auto-injected pool daihyosen, NOT an
  // in-match representative bout. The compFormat clause is only a fallback for
  // bracket/unknown-phase matches in KO-bearing formats: it must exclude
  // explicit pool matches, or a drawn pool match becomes unfinishable and the
  // in-match daihyosen affordance wrongly appears (the comment above this line
  // already states daihyosen is knockout-only).
  const isKnockoutPhase = m.phase === "bracket"
    || ((compFormat === "playoffs" || compFormat === "mixed") && m.phase !== "pool");

  // Whether an inline position PUT is in flight (prevents double-submit).
  const [inlineLineupSaving, setInlineLineupSaving] = useStateA(false);

  // Derive each team's roster from compMeta.players. rosterFor expects the
  // team object (with metadata array); resolveLineupTeamId matches by name.
  const allPlayers =
    (compMeta?.players?.length ? compMeta.players : null)
    || (compMeta?.config?.players)
    || [];
  // lineup is this side's already-assigned positions; mergeRosterWithAssigned
  // folds any operator-added substitute (a "+ Add …" free name not in
  // team.metadata) back into the autocomplete so it reappears for the team's
  // other positions instead of vanishing after a single entry.
  const rosterForSide = (side, lineup) => {
    if (!window.AdminLineupHelpers?.rosterFor) return [];
    const sideKey = typeof side === "object" ? (side?.id || side?.name) : side;
    const teamObj = allPlayers.find(p => {
      const pid = p?.id || p?.ID || p?.name || p?.Name || "";
      const pname = p?.name || p?.Name || "";
      return pid === sideKey || pname === sideKey;
    });
    const base = window.AdminLineupHelpers.rosterFor(teamObj || null);
    return window.AdminLineupHelpers.mergeRosterWithAssigned
      ? window.AdminLineupHelpers.mergeRosterWithAssigned(base, lineup)
      : base;
  };
  const teamIdForSide = (side) => {
    const sideKey = typeof side === "object" ? (side?.id || side?.name) : side;
    const teamObj = allPlayers.find(p => {
      const pid = p?.id || p?.ID || p?.name || p?.Name || "";
      const pname = p?.name || p?.Name || "";
      return pid === sideKey || pname === sideKey;
    });
    return teamObj ? (teamObj.id || teamObj.ID || teamObj.name || teamObj.Name || sideKey) : sideKey;
  };

  // Submit an inline position change: builds the full positions map from the
  // existing lineup + the changed key→value, then PUTs. Lineups are always
  // editable; no force/reason needed.
  const submitInlineLineup = async (teamId, lineup, posKey, value) => {
    setInlineLineupSaving(true);
    try {
      const existing = lineup?.positions || {};
      const updated = { ...existing };
      if (value) updated[posKey] = value;
      else delete updated[posKey];
      await window.API.putMatchLineup(m.compId, teamId, m.id, updated, password);
      // Refresh lineup state from the response is deferred: on next open the
      // modal re-fetches. For immediate feedback we do a partial reload of
      // lineup state for the affected side.
      if (!mountedRef.current) return;
      if (teamId === teamIdForSide(m.sideA)) {
        setLineupA(prev => ({ ...prev, positions: updated }));
      } else {
        setLineupB(prev => ({ ...prev, positions: updated }));
      }
    } catch (e) {
      // Surface error briefly: can't use a toast from inside the modal so
      // we reuse the editorErr channel for a one-off message.
      if (mountedRef.current) setEditorErr(e?.message || "Failed to update lineup");
    } finally {
      if (mountedRef.current) setInlineLineupSaving(false);
    }
  };

  // Shared factory (admin_scoring_shared.jsx): same handler as ScoreEditorModal;
  // "teams" is the only per-modal wording (in the decision_locked confirm).
  // Item 7: fusenpai routes through onAfterDecision (host-supplied) to advance
  // the court, same as ScoreEditorModal. Kiken keeps the modal open regardless.
  const submitDecision = makeSubmitDecision({
    match: m, enchoPeriodCount, password, mountedRef,
    setDecisionSubmitting, setDecisionErr, setWithdrawnPlayer, setDecisionPromptKind,
    onClose, onAfterDecision, isComplete, entityLabel: "teams",
  });

  const existingSub = m.subResults || [];
  // T096/FR-031: round-trip per-bout fusensho. SubMatchResult.decision is
  // the canonical signal: when "fusensho", figure out which side it
  // belongs to via the recorded winner so the UI re-opens with the
  // affordance shown as active.
  const sideAName = typeof m.sideA === "object" ? m.sideA?.name : m.sideA;
  const sideBName = typeof m.sideB === "object" ? m.sideB?.name : m.sideB;
  // seedSubAt builds the local sub state for one position index from the
  // CURRENT match prop (existingSub is re-derived each render). Used for
  // the one-time initial seed below AND to extend the rows when the server
  // appends a kachinuki bout while the modal is open (growth effect after
  // the subs state declaration).
  const seedSubAt = (idx) => {
      const pos = idx === daihyosenIdx ? DAIHYOSEN_POSITION : idx + 1;
      const existing = existingSub.find(s => s.position === pos);
      const fusensho = fusenshoSideFromSub(existing);
      // reconcileFoulsAtOpen mirrors ScoreEditorModal: pre-fix builds
      // stored the cumulative raw foul count alongside the already-awarded
      // H in the opponent's ippon array. The counter now means "outstanding
      // fouls not yet discharged" and any missing discharged H's in the
      // opponent's pts are topped up (defensive against legacy/imported data).
      const rawAFouls = existing ? existing.hansokuA || 0 : 0;
      const rawBFouls = existing ? existing.hansokuB || 0 : 0;
      const seedAPts = existing ? (existing.ipponsA || []).filter(x => x && x !== "•") : [];
      const seedBPts = existing ? (existing.ipponsB || []).filter(x => x && x !== "•") : [];
      const reconA = reconcileFoulsAtOpen(rawAFouls, seedBPts);
      const reconB = reconcileFoulsAtOpen(rawBFouls, seedAPts);
      return {
        aPts: reconB.opponentPts,
        bPts: reconA.opponentPts,
        aFouls: reconA.outstandingFouls,
        bFouls: reconB.outstandingFouls,
        fusensho,
        // Operator-marked hikiwake. Seed from the persisted decision: a
        // kachinuki bout recorded as a 0-0 hikiwake carries decision
        // "hikiwake" with no points, and seeding false here made it look
        // UNPLAYED to subBoutHasBeenPlayed, so the modal re-selected it
        // as the current bout and the next autosave rewrote the server
        // log (UAT: a recorded draw was lost and the appended placeholder
        // dropped). A fresh/unrecorded bout stays false.
        draw: existing?.decision === "hikiwake",
        // mp-gmcg: per-bout encho count for a KACHINUKI numbered bout (a
        // knockout tie on the final pairing goes to overtime on that same
        // bout; daihyosen does not exist in kachinuki). Seeded from the
        // persisted sub so re-opens keep the overtime marker. The daihyosen
        // row's encho is handled separately (daihyosenEnchoFields).
        encho: idx === daihyosenIdx ? 0 : (existing?.encho?.periodCount || 0),
      };
  };
  const initSubsRef = React.useRef(null);
  if (initSubsRef.current === null) {
    initSubsRef.current = positions.map((_, idx) => seedSubAt(idx));
  }
  const [subsRaw, setSubs] = useStateA(initSubsRef.current);
  // mp-gmcg growth: a kachinuki Record-bout write appends the next pairing
  // server-side and the host adopts the fresh bout log into the match prop,
  // so `positions` can GROW while the modal is mounted. The extension must
  // be RENDER-SYNCHRONOUS: this very render already indexes subs for the
  // new position (an effect would run after the crash). Extend the dirty
  // baseline once (length-guarded, so re-renders never double-append) and
  // derive a row list that always covers positions; the effect below then
  // commits the extension into state so updateSub can edit the new row.
  // Rows the operator already has are never touched. Kachinuki only, and
  // kachinuki has no daihyosen row (daihyosenIdx -1), so appending keeps
  // the idx↔position mapping stable.
  let subs = subsRaw;
  if (isKachinuki && daihyosenIdx < 0 && positions.length > subsRaw.length) {
    if (positions.length > initSubsRef.current.length) {
      const added = [];
      for (let i = initSubsRef.current.length; i < positions.length; i++) added.push(seedSubAt(i));
      initSubsRef.current = [...initSubsRef.current, ...added];
    }
    subs = [...subsRaw, ...initSubsRef.current.slice(subsRaw.length, positions.length)];
  }
  useEffectA(() => {
    if (!isKachinuki || daihyosenIdx >= 0) return;
    const target = positions.length;
    setSubs(prev => prev.length >= target ? prev : [...prev, ...initSubsRef.current.slice(prev.length, target)]);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [positions.length]);
  // C1: updateSub is the single choke-point for all sub-bout state
  // mutations. Calling markScoringDirty() here captures every edit
  // (pts add/remove, fouls, fusensho, draw) without repetition.
  const updateSub = (idx, fn) => { setSubs(prev => prev.map((s, i) => i === idx ? fn(s) : s)); markScoringDirty(); };

  // T096/FR-031: per-bout Fusensho: award a 2-0 default win to the
  // present side. Re-clicking the active side undoes the fusensho and
  // restores the score that existed before fusensho was applied (the
  // operator's intent on the active button is "undo this"). Clicking
  // the OTHER side while fusensho is active is a side-switch; the
  // original pre-fusensho snapshot is preserved so a later untoggle
  // still restores the genuine prior state, not the intermediate 2-0.
  const setFusenshoFor = (idx, side) => updateSub(idx, prev => applyFusenshoToggle(prev, side));

  // Toggle an operator-marked hikiwake (draw) for a sub-bout. Marking a draw
  // clears any fusensho; editing scores/fouls later clears the draw flag (see
  // rowSides setters), mirroring how fusensho behaves.
  const setDrawFor = (idx) => updateSub(idx, prev => ({ ...prev, draw: !prev.draw, fusensho: "", _preFusensho: undefined }));

  // Hansoku Hs are already in the pts arrays (folded in by
  // applyFoulIncrement at the 2-foul boundary), so totals are just the
  // pts length. No separate hansoku tally is needed in the live view.
  // A bout the operator marked as a draw has no winner, so it counts as a
  // hikiwake for IV/PW and serialises with decision="hikiwake". The winner
  // itself comes from boutWinnerSide — the one winner rule, shared with the
  // kachinuki band and End-match derivation.
  const subTotals = subs.map(s => {
    const aT = s.aPts.length;
    const bT = s.bPts.length;
    const winner = boutWinnerSide({ aCount: aT, bCount: bT, draw: s.draw, fusenshoSide: s.fusensho });
    return { aTotal: aT, bTotal: bT, winner, draw: !!s.draw };
  });

  // mp-4pc: the daihyosen row (when present) is excluded from IV/PW: it
  // is a tiebreaker, not an individual victory. Its own winner (hantei
  // side first, then score) decides the encounter.
  let ivA = 0, ivB = 0, pwA = 0, pwB = 0;
  subTotals.forEach((s, i) => {
    if (i === daihyosenIdx) return;
    if (s.winner === "a") ivA++;
    else if (s.winner === "b") ivB++;
    pwA += s.aTotal;
    pwB += s.bTotal;
  });
  // Hantei applies only to a tied daihyosen scoreline (FIK 7-5 / 29-6);
  // otherwise the bout is decided by ippons like any other.
  const daihyosenTied = hasDaihyosen && subTotals[daihyosenIdx].aTotal === subTotals[daihyosenIdx].bTotal;
  const daihyosenWinner = hasDaihyosen
    ? ((daihyosenTied && daihyosenHantei) ? daihyosenHantei : subTotals[daihyosenIdx].winner)
    : null;
  const teamWinner = hasDaihyosen
    ? (daihyosenWinner || null)
    : (ivA > ivB ? "a" : ivB > ivA ? "b" : pwA > pwB ? "a" : pwB > pwA ? "b" : null);

  // Finish guard: recording a team result is the highest-stakes action here, so
  // it gets the same deliberate gate Cancel already has (the dirty-discard
  // confirm). One tap arms and surfaces the computed verdict on the button; a
  // second tap commits. Any score change disarms so the operator can never
  // confirm a stale verdict. Keyboard Enter is left direct: it's deliberate,
  // unlike an accidental brush on a tablet. (a-vs-b is AKA-vs-SHIRO; the band
  // and this label read SHIRO–AKA to match the sheet's left-right order.)
  const [finishArmed, setFinishArmed] = useStateA(false);
  // A knockout encounter cannot end in a draw: a tie is resolved by a
  // representative bout (daihyosen), not recorded as hikiwake. So in a KO phase
  // a null teamWinner is never "DRAW": it's "DAIHYOSEN" once there's a scored
  // tie to break, or simply pending ("-") before any bout lands. Only pool
  // matches read a null winner as a true draw.
  // A drawn (hikiwake) sub-bout produces no IV or PW but is still a landed
  // result, so a KO encounter tied solely on 0–0 draws must read "DAIHYOSEN",
  // not pending ("-"). teamEncounterHasResult folds those draws in.
  const teamHasAnyScore = teamEncounterHasResult({ ivA, ivB, pwA, pwB, subTotals, daihyosenIdx });
  const teamVerdictText = teamResultLabel({ teamWinner, isKnockoutPhase, hasAnyScore: teamHasAnyScore, isKachinuki });
  // Block Finish while a KO encounter has no winner: the operator must add and
  // score a daihyosen first (the affordance below). Pool draws stay finishable.
  const koTieBlocked = isKoTieBlocked({ isKnockoutPhase, teamWinner, isComplete });
  // While a kachinuki match is being fought, the primary actions record
  // the current BOUT (running write + kachinukiBoutFinal flag) or END the
  // match (operator-led, mp-gmcg): completion is never inferred from
  // roster exhaustion, so a running match: including one that carries a
  // "kachinuki-exhaustion" sub decision after a reopen: is always in bout
  // mode. koTieBlocked does NOT apply to a bout submit (a bout hikiwake is
  // legitimate; the match is not being completed); End match has its own
  // knockout-tie gate via deriveKachinukiEndOutcome.
  const kachinukiBoutMode = isKachinukiBoutMode({ isKachinuki, isComplete, hasDaihyosen });
  // mp-gmcg: the server stamps `reopenPending` on a match it reopened for a
  // mistake and REJECTS the write that completes it again unless that write
  // carries a correctionReason (400, field correctionReason). So the reason
  // the reopen skipped is demanded here, on [End match].
  //
  // This MUST be driven off the server field, never off local state: the
  // editor mounts per match, so an operator who reopens, walks away to another
  // court and comes back has no local memory of the reopen at all — which is
  // precisely the case this design exists to survive. m.reopenPending survives
  // it because the match carries it.
  const reopenReasonRequired = !!m.reopenPending;
  // The two audit prompts that can take over the footer, derived from the one
  // reasonPromptKind selector so only one Cancel/Confirm row may ever be on
  // screen. Each also hides the footer's own nav+actions below. The extra
  // condition on each is the state that makes that prompt meaningful at all,
  // so a stale kind (e.g. the match completing underneath an open prompt)
  // closes itself rather than hanging over a footer it no longer belongs to.
  const correctionPromptOpen = reasonPromptKind === "correction" && isComplete;
  // kachinukiBoutMode because this prompt belongs to [End match] specifically:
  // that keeps kachinukiEndOutcome non-null wherever the prompt commits it,
  // and closes the prompt if the encounter completes underneath it.
  const reopenPromptOpen = reasonPromptKind === "reopen" && reopenReasonRequired && kachinukiBoutMode;
  // Rows to render: kachinuki shows only bouts that exist in the server log
  // (kachinukiVisiblePositions handles the bout-1 bootstrap, the running
  // current-bout selection, the correction show-all branch, and the
  // always-visible daihyosen slot); fixed-format matches keep all positions.
  // Computed once here and shared by the Record-bout guard below and the
  // render body.
  const visiblePositions = isKachinuki
    ? kachinukiVisiblePositions({
        positions, daihyosenIdx,
        // Client-added manual bouts (mp-gmcg) are not in the server log yet;
        // fold them in as placeholder entries so the new row renders.
        subResults: manualBouts.length
          ? [...(m.subResults || []), ...manualBouts.map(p => ({ position: p }))]
          : m.subResults,
        isComplete,
        isPlayedAt: (idx) => subBoutHasBeenPlayed(subs[idx]),
      })
    : positions;
  // mp-gmcg: keyboard ippon entry for the CURRENT kachinuki bout (critique P2).
  // Team scoring is many bouts in general (an ambiguous key target — the reason
  // the handler below only bound Esc/arrows), but a RUNNING kachinuki encounter
  // shows exactly ONE current bout, so M/K/D/T/H (Shift = Aka) is unambiguous
  // here — mirroring the individual editor. Wired only in kachinukiBoutMode;
  // fixed-format multi-bout scoring stays tap-only. This is THE single
  // "current visible bout" derivation (the daihyosen-skip lives here only);
  // kachinukiCurrentBoutPlayed and kachinukiCurBoutPos below are pure
  // functions of it.
  const kachinukiCurBoutIdx = kachinukiBoutMode
    ? (() => { const cur = visiblePositions.find(p => p !== "daihyosen"); return cur != null ? positions.indexOf(cur) : -1; })()
    : -1;
  // UX guard: Record bout with nothing entered for the current bout would
  // submit a silent no-op (known quirk Q4: the glossary term inside the
  // Tie button swallows taps, and that chain used to end in a silent 200).
  // Disable the button until the CURRENT visible bout carries operator
  // input (points, fouls, fusensho, or an explicit draw).
  const kachinukiCurrentBoutPlayed = kachinukiBoutMode
    ? kachinukiCurBoutIdx >= 0 && subBoutHasBeenPlayed(subs[kachinukiCurBoutIdx])
    : true;
  // mp-gmcg: the bouts already fought (everything played except the current
  // bout). They render as READ-ONLY team-sub-match rows above the current
  // editable bout, so the operator sees the whole encounter like a regular team
  // sheet — reusing the bout-scoring component in a display mode (this replaces
  // the earlier plain-text "bout log").
  const kachinukiDoneBoutIdxs = kachinukiBoutMode
    ? positions.map((_, i) => i).filter(i => i !== daihyosenIdx && i !== kachinukiCurBoutIdx && subBoutHasBeenPlayed(subs[i]))
    : [];
  const scoreCurrentBoutWaza = (side, waza) => {
    if (kachinukiCurBoutIdx < 0) return;
    const cur = subs[kachinukiCurBoutIdx];
    // Match the ippon buttons' guards so the keyboard can't record a score the
    // taps forbid: the buttons are disabled once the bout is decided
    // (subBoutDecided — a side reached 2 ippons, so scoring on could push an
    // impossible 2-2), and never append past MAX_IPPONS_PER_SIDE per side.
    if (isBoutDecided(cur.aPts, cur.bPts)) return;
    const key = side === "a" ? "aPts" : "bPts";
    updateSub(kachinukiCurBoutIdx, prev => (
      prev[key].length >= MAX_IPPONS_PER_SIDE
        ? prev
        // Mirror setPts's clear-tail: a fresh strike clears a pending fusensho/draw.
        : { ...prev, [key]: [...prev[key], waza], fusensho: "", _preFusensho: undefined, draw: false }
    ));
  };

  // mp-gmcg: [End match] outcome, derived from LOCAL bout state so an
  // unsaved just-scored bout counts (the operator scores the final bout and
  // taps End without a Record in between). buildKachinukiEndEntries keeps
  // exactly the bouts subBoutHasBeenPlayed admits, so End derivation, the
  // wire filter, and the encho target below all agree on which bout is the
  // last one.
  const [endArmed, setEndArmed] = useStateA(false);
  const kachinukiEndOutcome = kachinukiBoutMode
    ? deriveKachinukiEndOutcome({
        subResults: buildKachinukiEndEntries(subs, daihyosenIdx),
        isKnockoutPhase,
      })
    : null;
  // Last LOCALLY-played numbered bout: the encho target (the tied final
  // pairing keeps fighting on that same bout). Same subBoutHasBeenPlayed
  // primitive as the End derivation above, so Encho always lands on the
  // bout End would judge.
  const kachinukiLastScoredIdx = (() => {
    let li = -1;
    subs.forEach((s, i) => { if (i !== daihyosenIdx && subBoutHasBeenPlayed(s)) li = i; });
    return li;
  })();
  // Encho is offered ONLY while the bout End would judge (the last SCORED bout)
  // is the bout being edited (the current visible slot). They diverge the
  // moment Record bout is tapped on a tie: the server appends the next pairing,
  // kachinukiCurBoutIdx moves to that fresh unplayed slot, but the End outcome
  // still reflects the tied bout N — so without this gate the Encho button
  // stays on screen and applyKachinukiEncho would write overtime onto bout N,
  // now rendered READ-ONLY, while the appended bout N+1 hangs unscored
  // (mp-gmcg review). Overtime means "the SAME pair keeps fighting THIS bout",
  // meaningful only before the encounter has moved on.
  const kachinukiEnchoOffered = kachinukiBoutMode
    && kachinukiEnchoAvailable(kachinukiEndOutcome)
    && kachinukiLastScoredIdx === kachinukiCurBoutIdx;
  // Encho on the tied current kachinuki bout: bump the bout's overtime count
  // AND the match-level counter (decisionSuffix reads match.encho for the
  // "(E)" suffix; enchoBlock forwards it since kachinuki has no daihyosen),
  // then clear the tied outcome so the SAME pair keeps scoring that bout. The
  // guard mirrors kachinukiEnchoOffered so the keyboard/programmatic path can
  // never target a bout the encounter has already advanced past.
  const applyKachinukiEncho = () => {
    if (kachinukiLastScoredIdx < 0 || kachinukiLastScoredIdx !== kachinukiCurBoutIdx) return;
    setEnchoPeriodCount(cnt => cnt + 1);
    updateSub(kachinukiLastScoredIdx, prev => ({ ...prev, encho: (prev.encho || 0) + 1, draw: false, _preFusensho: undefined }));
    setEndArmed(false);
  };
  // Manual next bout (mp-gmcg): the server auto-append can only pair
  // fighters it knows (lineups + bout log) and team sizes are unregulated,
  // so the operator can append the next pairing locally. The row becomes a
  // regular subResult on the next write (position = last + 1); an untouched
  // manual row is dropped by the subBoutHasBeenPlayed filter and never
  // reaches the wire.
  //
  // One past the furthest bout anyone knows about, reusing the values already
  // in scope rather than re-scanning: maxSubPos (the server log) and
  // manualMaxPos (rows this operator added) — the same two that size the grid
  // via positionCount — plus kachinukiLastScoredIdx (the last locally-played
  // bout, from the one subBoutHasBeenPlayed primitive), which is -1 when
  // nothing is played and so cannot raise the floor. Floor 1 because bout 1
  // always exists (the bootstrap slot). Position is the merge key the server
  // keys off (mergeKachinukiSubResults), so a private re-derivation here
  // would silently mis-number a bout the grid has already numbered.
  const kachinukiNextManualPos = Math.max(1, maxSubPos, manualMaxPos, kachinukiLastScoredIdx + 1) + 1;
  const addManualBout = () => {
    const nextPos = kachinukiNextManualPos;
    if (nextPos > kachinukiMaxBouts) return;
    setManualBouts(prev => [...prev, nextPos]);
    setSubs(prev => {
      const out = [...prev];
      while (out.length < nextPos) out.push({ aPts: [], bPts: [], aFouls: 0, bFouls: 0, fusensho: "", draw: false, encho: 0 });
      return out;
    });
    setFinishArmed(false);
    setEndArmed(false);
  };
  // Remove-this-bout (mp-gmcg): the explicit undo for a pairing appended by
  // mistake. Removable ONLY when the current bout is unscored AND a prior bout
  // was already scored (kachinukiLastScoredIdx >= 0) — i.e. it is an appended
  // EXTRA, never the bootstrap bout 1. This is exactly the row the End-match
  // strip would drop; surfacing it as a button makes that implicit behaviour
  // explicit and reversible on the spot.
  const kachinukiBoutRemovable = isKachinukiBoutRemovable({
    boutMode: kachinukiBoutMode,
    currentBoutPlayed: kachinukiCurrentBoutPlayed,
    lastScoredIdx: kachinukiLastScoredIdx,
  });
  // Position of the current (trailing) visible bout, the removal target
  // (derived from kachinukiCurBoutIdx; only read when kachinukiBoutRemovable,
  // which itself requires bout mode).
  const kachinukiCurBoutPos = kachinukiCurBoutIdx < 0 ? 0 : kachinukiCurBoutIdx + 1;
  // A removal lowers maxSubPos or manualMaxPos, but positionCount is floored at
  // teamSize, so subsRaw must never be trimmed BELOW the new positionCount:
  // otherwise the state array falls short of positions.length (pinned by the
  // floor), the render-only extension (above) patches the gap every render
  // without ever committing, updateSub(idx) then silently misses the regrown
  // rows, and the [subs] disarm effect thrashes on a fresh identity each render
  // (mp-gmcg review F1). Resize to exactly the post-removal positionCount, via
  // the SAME clampPositionCount that sizes positionCount above — trailing rows
  // the floor still shows are re-seeded from initSubsRef.
  const resizeSubsTo = (prev, target) => {
    const kept = prev.slice(0, target);
    return kept.length >= target ? kept : [...kept, ...initSubsRef.current.slice(kept.length, target)];
  };
  const removeCurrentBout = async () => {
    if (!kachinukiBoutRemovable || kachinukiCurBoutPos <= 0) return;
    const pos = kachinukiCurBoutPos;
    // A manual bout the operator added but the server has NOT persisted yet
    // (position beyond maxSubPos, the server log's high-water mark) is purely
    // local: pop it, no round-trip. A [Record bout] append IS persisted
    // (pos <= maxSubPos), so it must be stripped server-side.
    if (pos > maxSubPos) {
      const remaining = manualBouts.filter(p => p !== pos);
      setManualBouts(remaining);
      const target = clampPositionCount(maxSubPos, remaining.length ? Math.max(...remaining) : 0);
      setSubs(prev => resizeSubsTo(prev, target));
      setEndArmed(false);
      setFinishArmed(false);
      setRemoveBoutErr("");
      return;
    }
    setRemoveBoutErr("");
    setRemovingBout(true);
    try {
      const updated = await window.API.removeKachinukiBout(m.compId, m.id, resolveDecisionPassword(password));
      // The parent won't refresh this snapshot for an out-of-band mutation, so
      // adopt the shorter log locally (server truth if returned, else drop the
      // removed position) and trim the matching local row.
      const nextSubs = updated && Array.isArray(updated.subResults)
        ? updated.subResults
        : (match.subResults || []).filter(s => s.position !== pos);
      setMatchOverride({ ...match, subResults: nextSubs });
      // The server strips exactly the trailing bout at `pos`, so the new log
      // ceiling is pos-1; keep the teamSize floor (clampPositionCount).
      setSubs(prev => resizeSubsTo(prev, clampPositionCount(pos - 1, manualMaxPos)));
      setEndArmed(false);
      setFinishArmed(false);
    } catch (e) {
      setRemoveBoutErr(String(e?.message || "Could not remove that bout"));
    } finally {
      setRemovingBout(false);
    }
  };
  // Any edit disarms every two-step confirm so a stale verdict can never be
  // committed (subs identity changes only through updateSub/addManualBout;
  // daihyosenHantei flips the verdict without touching subs). The audit
  // prompt is dismissed here too (mp-gmcg): its Confirm IS a commit, and the
  // End-match prompt commits the verdict shown when it opened — an operator
  // who scores another point while the prompt is up must not have the stale
  // verdict confirmed out from under them.
  useEffectA(() => { setFinishArmed(false); setEndArmed(false); setReasonPromptKind(""); }, [subs, daihyosenHantei]);

  // mp-gmcg: reopen a completed kachinuki match (POST .../reopen): status
  // back to running, winner/decision cleared, bout log kept. This POST is a
  // direct API call, NOT a score write, so it does NOT flow through the host's
  // onEditScore/setOpenMatch channel that keeps the modal live across a
  // running write — the `match` prop here is the host's openMatch snapshot,
  // captured completed, and nothing repaints it in place. So on success we
  // CLOSE the editor: the host's SSE match_updated handler flips the match
  // row to running, and the operator taps Score to resume bout-by-bout on the
  // now-running encounter. The server's 409s are full-sentence messages ("...
  // is not completed ..." / "cannot reopen: a downstream knockout match ...")
  // and surface inline verbatim, never silently; the court-busy one carries a
  // remedy panel with them (below).
  //
  // NO REASON IS ASKED FOR HERE (operator ruling): the tap posts. The
  // justification rides the write that closes the encounter back out
  // (reopenReasonRequired above), which is where the operator knows what
  // actually happened anyway.
  //
  // A reopen failure is either actionable here or a sentence to read. The
  // court-busy 409 is the actionable one: it names the match holding the
  // court, so the operator can clear it from this panel instead of being told
  // to go somewhere else. Note the asymmetry that makes this mandatory: a
  // plain correction bypasses the court gate entirely, so kachinuki operators
  // — for whom reopen is the ONLY way to fix a bout log — would otherwise be
  // the one group with no way out of a busy court.
  const applyReopenFailure = (e) => {
    const msg = String(e?.message || "Failed to reopen match");
    if (e?.code === "court_busy" && e?.matchId) {
      setReopenErr("");
      setReopenConflict({
        court: e.court || m.court || "",
        matchId: e.matchId,
        compId: e.compId || m.compId,
        message: msg,
      });
      return;
    }
    setReopenConflict(null);
    setReopenErr(msg);
  };

  // Once the match reopens (status flips to running / bout mode), drop any
  // stale reopen error or court-busy conflict and re-enable the control: they
  // describe a completed-state action that no longer applies. Without this a
  // failed-then-succeeded reopen could leave an error lingering in bout mode
  // (the inline variant does not remount on the transition).
  useEffectA(() => {
    if (!isComplete) { setReopenErr(""); setReopenConflict(null); setReopenBusy(false); }
  }, [isComplete]);

  const onReopenMatch = async () => {
    if (reopenBusy) return;
    setReopenErr("");
    setReopenConflict(null);
    setReopenBusy(true);
    try {
      await window.API.reopenMatch(m.compId, m.id, resolveDecisionPassword(password));
      if (!mountedRef.current) return;
      // Success: KEEP the button disabled (reopenBusy stays true). onClose is a
      // no-op in the inline (shiaijo) variant, so the completed snapshot lingers
      // through the SSE refetch window; re-enabling now would let a double-tap
      // fire a second reopen that the server rejects ("not completed", 409). The
      // refetch flips the match to running, and the effect above clears
      // reopenBusy. The overlay variant unmounts on onClose, so this is moot.
      onClose();
    } catch (e) {
      if (!mountedRef.current) return;
      applyReopenFailure(e);
      setReopenBusy(false);
    }
  };

  // The remedy: send the blocking match back to the queue, then retry the
  // reopen the conflict refused. DESTRUCTIVE — revert-to-queue clears that
  // match's partial score — which is why the panel spells the consequence out
  // before this can be tapped.
  const requeueBlockerAndReopen = async () => {
    const c = reopenConflict;
    if (!c || reopenBusy) return;
    setReopenErr("");
    setReopenBusy(true);
    try {
      // ONE atomic server call (mp-gmcg review A4): requeue the blocker AND
      // reopen the target under a single court lock, closing the race the old
      // two-call revert-then-reopen had (a peer could take the freed court in
      // between). A court_busy failure means a DIFFERENT match has since taken
      // the court; applyReopenFailure re-offers the remedy for that one.
      await window.API.requeueBlockerAndReopen(m.compId, m.id, c.compId, c.matchId, resolveDecisionPassword(password));
      if (!mountedRef.current) return;
      setReopenConflict(null);
      onClose();
    } catch (e) {
      if (!mountedRef.current) return;
      // One atomic call now, so a single failure path: applyReopenFailure
      // re-offers the remedy if a DIFFERENT match has since taken the court
      // (court_busy), and otherwise shows the server's sentence — a completed
      // or unknown blocker, a downstream-fought target, etc. The requeue and
      // reopen commit together or not at all, so there is no partial state to
      // describe separately.
      applyReopenFailure(e);
    } finally {
      if (mountedRef.current) setReopenBusy(false);
    }
  };

  // Name the blocking match's competitors when we can. Best effort by design:
  // the server's match id is enough to ACT on, so a failed or unavailable
  // lookup degrades to that rather than blocking the remedy.
  useEffectA(() => {
    setBlockerLabel("");
    if (!reopenConflict?.matchId || !reopenConflict?.compId) return;
    if (typeof window.compMatches !== "function" || typeof window.API?.fetchCompetitionDetails !== "function") return;
    let cancelled = false;
    (async () => {
      try {
        const detail = await window.API.fetchCompetitionDetails(reopenConflict.compId);
        if (cancelled || !mountedRef.current) return;
        // compMatches expects a competition in LIST shape, where `status` and
        // `teamMatchType` sit at the TOP level. The details payload keeps those
        // under `config`, and compMatches bails on a missing status via its
        // "setup" guard, so passing `detail` straight in always returned [] and
        // every conflict panel fell back to naming the blocker by its raw id
        // ("Shiaijo A is running m-r1-1"), which tells an operator nothing.
        // Detail's own keys stay authoritative for the match collections.
        const listShaped = { ...detail.config, ...detail, status: detail.config?.status };
        const hit = (window.compMatches(listShaped) || []).find(x => x.id === reopenConflict.matchId);
        if (!hit) return;
        const shiro = hit.sideB?.name || hit.sideB || "";
        const aka = hit.sideA?.name || hit.sideA || "";
        // Match number first: it is how the console labels every other match
        // ("09:16 · KTeam · Match 2"), so it is what the operator is looking for.
        const label = [
          hit.matchNumber ? `Match ${hit.matchNumber}` : "",
          shiro && aka ? `${shiro} vs ${aka}` : "",
        ].filter(Boolean).join(" · ");
        if (label) setBlockerLabel(label);
      } catch { /* best effort: the panel falls back to the match id */ }
    })();
    return () => { cancelled = true; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [reopenConflict?.compId, reopenConflict?.matchId]);

  // mp-4pc: when a daihyosen exists the encho counter belongs to that
  // sub-bout (attached per-sub in buildPatch), so suppress the top-level
  // encho to avoid duplicate/ambiguous semantics on the team match.
  const enchoBlock = () => (enchoPeriodCount > 0 && !hasDaihyosen) ? { encho: { periodCount: enchoPeriodCount } } : {};

  // Per-bout competitor names. Single choke point shared by the row
  // renderer and buildPatch (via resolveKachinukiBoutSides) so display and
  // persisted identity can never diverge. Priority is format-aware
  // (resolveBoutSideName): kachinuki numbered bouts are server-bout-log
  // first (the engine's winner-stays pairing is authoritative; the lineup
  // only seeds the bootstrapped bout 1), fixed-format and daihyosen rows
  // are lineup-first as before.
  const playerNamesForBout = (idx) => {
    const isDaihyoRow = idx === daihyosenIdx;
    const pos = isDaihyoRow ? DAIHYOSEN_POSITION : idx + 1;
    const existing = existingSub.find(s => s.position === pos);
    const posKey5 = (teamSize === 5 && idx < 5) ? POS_KEYS_5[idx] : null;
    const posKeyN = String(positions[idx]);
    const pick = (lineup) => {
      if (!lineup?.positions) return "";
      if (posKey5 && lineup.positions[posKey5]) return lineup.positions[posKey5];
      if (lineup.positions[posKeyN]) return lineup.positions[posKeyN];
      return "";
    };
    // mp-gmcg: a manually-added bout (no server entry, no lineup key) carries
    // the operator's player picks on the local sub state (aName/bName). They
    // take the existing-name slot: for that bout they ARE the authoritative
    // per-bout identity, exactly like a server bout-log entry.
    const override = subs[idx] || {};
    return {
      aName: resolveBoutSideName({ isKachinuki, isDaihyosen: isDaihyoRow, existingName: override.aName || existing?.sideA, lineupName: pick(lineupA) }),
      bName: resolveBoutSideName({ isKachinuki, isDaihyosen: isDaihyoRow, existingName: override.bName || existing?.sideB, lineupName: pick(lineupB) }),
    };
  };

  // mp-gmcg: open a past (already-fought) bout for inline correction. Snapshot
  // its current outcome so renderCorrectionWarning can tell if the operator
  // FLIPS who won (which invalidates the later bouts' fighters).
  const openDoneBoutEdit = (idx) => {
    const t = subTotals[idx];
    editingDoneOriginalRef.current = { winner: t ? t.winner : null };
    setEditingDoneBoutIdx(idx);
  };
  const closeDoneBoutEdit = () => { editingDoneOriginalRef.current = null; setEditingDoneBoutIdx(-1); };

  // mp-gmcg: the non-destructive warning shown beneath a past bout opened for
  // correction, when the correction has changed who won (the later bouts were
  // fought on the old result). Collapse is the black-triangle caret, so there is
  // no Done button. Rendered as its own element in the bout list so it sits
  // directly beneath the bout being edited without surgery inside the shared
  // editable-bout block.
  const renderCorrectionWarning = (idx) => {
    const orig = editingDoneOriginalRef.current;
    const t = subTotals[idx];
    const winnerFlipped = !!orig && !!t && t.winner !== orig.winner;
    if (!winnerFlipped) return null; // nothing to show unless the winner changed
    return (
      <p key={`edit-warn-${idx}`} className="alert alert--warn kachinuki-done-edit-warn" data-testid="kachinuki-done-edit-warn">
        You changed who won bout {idx + 1}. The later bouts were fought based on the old result, so check they still show the right competitors and fix any that are wrong.
      </p>
    );
  };

  // mp-gmcg: read-only display of a fought kachinuki bout — the SAME
  // team-sub-match layout as the editable bout row (position, Shiro/Aka names,
  // centred ippon-mark slots, winner) minus the controls. Reuses the
  // bout-scoring component's markup + CSS so past bouts read like a regular team
  // sheet / the TV board rather than a text log. Tapping the row reopens it for
  // inline correction (openDoneBoutEdit) — the only mid-encounter fix path.
  const renderReadOnlyBout = (idx) => {
    const s = subs[idx];
    const t = subTotals[idx];
    const { aName, bName } = playerNamesForBout(idx);
    const nameCls = (side) => "tsm-name__static" + (t.winner === side ? " tsm-name__static--win" : "");
    return (
      <div key={`ro-${idx}`} className="team-sub-match team-sub-match--readonly team-sub-match--editable" data-testid={`kachinuki-done-bout-${idx}`}
        role="button" tabIndex={0} aria-label={`Correct bout ${idx + 1}`} aria-expanded={false}
        onClick={() => openDoneBoutEdit(idx)}
        onKeyDown={e => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); openDoneBoutEdit(idx); } }}>
        {/* Kachinuki: only the bout number (no FIK position handle — the winner
            stays on, so a bout is not "position vs same position"). */}
        <div className="team-sub-match__pos"><span className="tsm-caret" aria-hidden="true">▶</span><span className="team-sub-match__pos-num">{idx + 1}</span></div>
        <div className="team-sub-match__row">
          <div className="team-sub-match__side team-sub-match__side--shiro">
            <div className="tsm-name"><span className={nameCls("b")}>{bName || "-"}</span></div>
          </div>
          <div className="team-sub-match__center">
            <div className="tsm-center-marks">
              <div className="tsm-center-pts tsm-center-pts--shiro">
                {s.bFouls >= 1 && <span className="tsm-foul-tri" title="Hansoku: 1 foul">▲</span>}
                {[0, 1].map(i => <span key={i} className={`editor-side__pt ${s.bPts[i] ? "editor-side__pt--filled" : ""}`}>{s.bPts[i] || "·"}</span>)}
              </div>
              <div className="team-sub-match__score">{renderTeamBoutMiddle(s, t, false)}</div>
              <div className="tsm-center-pts tsm-center-pts--aka">
                {[1, 0].map(i => <span key={i} className={`editor-side__pt ${s.aPts[i] ? "editor-side__pt--filled" : ""}`}>{s.aPts[i] || "·"}</span>)}
                {s.aFouls >= 1 && <span className="tsm-foul-tri" title="Hansoku: 1 foul">▲</span>}
              </div>
            </div>
          </div>
          <div className="team-sub-match__side team-sub-match__side--aka team-sub-match__side--right">
            <div className="tsm-name"><span className={nameCls("a")}>{aName || "-"}</span></div>
          </div>
        </div>
      </div>
    );
  };

  // opts.kachinukiBoutFinal: attach the transient bout-final flag ONLY for
  // the explicit "Record bout" action (never for autosave, Start, Finish or
  // corrections). The server advances the kachinuki sequence only on
  // flagged writes (handlers_match.go scoreRequestBody).
  const buildPatch = (targetStatus, opts = {}) => {
    if (targetStatus === "scheduled") return { winner: null, status: "scheduled", score: null, ipponsA: [], ipponsB: [], subResults: [] };
    let subResults = subs.map((s, idx) => {
      const t = subTotals[idx];
      const isDaihyo = idx === daihyosenIdx;
      // Hansoku Hs already in pts arrays via applyFoulIncrement: no fold.
      const aAll = s.aPts.slice(0, MAX_IPPONS_PER_SIDE);
      const bAll = s.bPts.slice(0, MAX_IPPONS_PER_SIDE);
      // The daihyosen winner may come from hantei (tied bout); fall back
      // to the score-derived winner otherwise.
      const wKey = isDaihyo ? daihyosenWinner : t.winner;
      const w = wKey === "a" ? m.sideA : wKey === "b" ? m.sideB : null;
      // T096/FR-031: per-bout fusensho overrides the default hikiwake/fought
      // mapping. The daihyosen always carries decision="daihyosen".
      let decision = "";
      if (isDaihyo) decision = "daihyosen";
      else if (s.fusensho) decision = "fusensho";
      else if (t.winner === null) decision = "hikiwake";
      const teamWinnerName = w ? (typeof w === "object" ? w.name : w) : "";
      // Competition-type-aware sub-bout identity: a kachinuki bout is consumed
      // per-competitor (advancement + bout-log export), so persist player-name
      // sides + winner; a fixed-position or daihyosen bout settles at the match
      // level, so it keeps the team-name behaviour (standings match the
      // match-level side first via isWinForSide).
      let sideA, sideB, winner;
      if (isKachinuki && !isDaihyo) {
        const { aName, bName } = playerNamesForBout(idx);
        ({ sideA, sideB, winner } = resolveKachinukiBoutSides({ aName, bName, wKey, teamWinnerName }));
      } else {
        sideA = sideAName;
        sideB = sideBName;
        winner = teamWinnerName;
      }
      const entry = {
        position: isDaihyo ? DAIHYOSEN_POSITION : idx + 1,
        sideA,
        sideB,
        ipponsA: aAll,
        ipponsB: bAll,
        hansokuA: s.aFouls,
        hansokuB: s.bFouls,
        winner,
        decision,
      };
      // mp-4pc: encho + hantei are valid ONLY on the daihyosen
      // (validation.go validateSubBout). daihyosenEnchoFields emits the two
      // independently: encho is optional for a hantei decision.
      if (isDaihyo) {
        Object.assign(entry, daihyosenEnchoFields({ enchoPeriodCount, daihyosenTied, daihyosenHantei }));
      } else if (isKachinuki && s.encho > 0) {
        // mp-gmcg: numbered-bout encho is the KACHINUKI knockout-tie
        // resolution (same pair keeps fighting the same bout; daihyosen
        // does not exist in kachinuki). validateSubBout relaxes the
        // daihyosen-only encho gate for kachinuki competitions. Hantei
        // stays daihyosen-only: never emitted here.
        entry.encho = { periodCount: s.encho };
      }
      return entry;
    });
    // Kachinuki appends bouts dynamically, so the all-positions map above leaves
    // untouched trailing positions. Drop them (keep the daihyosen and any played
    // bout): see subBoutHasBeenPlayed. Fixed-position matches keep every
    // position because a 0–0 there is a legitimate hikiwake.
    if (isKachinuki) {
      subResults = subResults.filter((_entry, idx) => idx === daihyosenIdx || subBoutHasBeenPlayed(subs[idx]));
    }
    const winner = teamWinner === "a" ? m.sideA : teamWinner === "b" ? m.sideB : null;
    // correctionReason rides any write that AMENDS a finalized result: a
    // correction to a completed match, and (mp-gmcg) the write that completes
    // a REOPENED one, which the server refuses without it. Same field, same
    // audit trail, two ways in.
    const correctionBlock = (isComplete || reopenReasonRequired) && correctionReason ? { correctionReason } : {};
    // When transitioning to "running" (▶ Start), teamWinner is typically
    // null (0–0). Don't emit score.type: "hikiwake": toBackendMatchResult
    // maps score.type to decision, which would persist a draw decision on
    // a running match. Send score.live: true with no completed-state semantics
    // so the backend leaves decision empty until the match actually finishes.
    if (targetStatus === "running") {
      return {
        winner: null,
        status: "running",
        ipponsA: [],
        ipponsB: [],
        score: { type: "ippon", winnerPts: 0, loserPts: 0, fouls: { a: 0, b: 0 }, live: true, corrected: isComplete },
        subResults,
        ...enchoBlock(),
        ...(opts.kachinukiBoutFinal ? { kachinukiBoutFinal: true } : {}),
      };
    }
    // mp-gmcg: [End match] carries a context-derived outcome
    // (deriveKachinukiEndOutcome). The winner is the LAST SCORED BOUT's
    // team, NOT the IV/PW leader: kachinuki is decided by exhaustion or
    // the taisho-defeated rule, both of which the last bout expresses and
    // both of which record decision "kachinuki-exhaustion". A tied last
    // bout in pools/league ends the encounter as a drawn match
    // ("hikiwake"); a knockout tie never reaches here (End is blocked).
    if (opts.endOutcome) {
      const eo = opts.endOutcome;
      const endWinnerSide = eo.kind === "win" ? eo.winnerSide : null;
      return {
        winner: endWinnerSide === "a" ? m.sideA : endWinnerSide === "b" ? m.sideB : null,
        status: "completed",
        ipponsA: [],
        ipponsB: [],
        decision: eo.kind === "win" ? "kachinuki-exhaustion" : "hikiwake",
        score: {
          type: endWinnerSide ? "ippon" : "hikiwake",
          winnerPts: endWinnerSide === "a" ? ivA : ivB,
          loserPts: endWinnerSide === "a" ? ivB : ivA,
          fouls: { a: 0, b: 0 },
          corrected: isComplete,
        },
        subResults,
        // The match-level (E) is omitted on a DRAWN end: the middle mark can be
        // X (tie) OR (E) but never both — a match that went to encho cannot end
        // tied (boutMiddle) — so persisting encho alongside decision "hikiwake"
        // is a contradiction the display only swallows because X beats (E)
        // (mp-gmcg review). Each bout that actually went to overtime still
        // records its own `encho` on its SubMatchResult (entry.encho above), so
        // no overtime is lost; only the spurious encounter-level marker is.
        ...(endWinnerSide ? enchoBlock() : {}),
        ...correctionBlock,
      };
    }
    return {
      winner,
      status: "completed",
      ipponsA: [],
      ipponsB: [],
      score: { type: teamWinner ? "ippon" : "hikiwake", winnerPts: teamWinner === "a" ? ivA : ivB, loserPts: teamWinner === "a" ? ivB : ivA, fouls: { a: 0, b: 0 }, corrected: isComplete },
      subResults,
      ...enchoBlock(),
      ...correctionBlock,
    };
  };
  // C1: keep autosave refs fresh with the latest buildPatch / onSubmit /
  // running-status for TeamScoreEditorModal.
  _autosaveIsRunningRef.current = m.status === "running";
  _autosaveBuildPatchRef.current = buildPatch;
  _autosaveOnSubmitRef.current = onSubmit;

  const doSubmit = async (fn) => {
    cancelScoringDebounce(); // C1: cancel pending autosave before explicit submit
    setSubmitting(true);
    try { await fn(); } finally { if (mountedRef.current) setSubmitting(false); }
  };

  // Mirrors ScoreEditorModal.isDirty: structural compare of current subs
  // to the initial snapshot. Used by handleDismiss below to prompt before
  // discarding multi-sub-match edits. Team scoring typically has 3–9 sub
  // entries; the JSON serialize approach is fine for that size and keeps
  // the comparison robust against array identity drift from setSubs.
  // Encho toggle is included so an operator-only encho change still
  // triggers the discard confirm.
  const isDirty = JSON.stringify(subs) !== JSON.stringify(initSubsRef.current) || enchoPeriodCount !== initialEnchoPeriods || daihyosenHantei !== initialDaihyosenHantei;

  // Match ScoreEditorModal's dismiss contract: never close mid-submit
  // (setState-after-unmount), AND confirm-then-discard when the user has
  // unsaved sub-match edits. The earlier version only checked submitting,
  // so an accidental backdrop/Esc silently lost up to 9 sub-match scores.
  const handleDismiss = async () => {
    // Same contract as ScoreEditorModal: never close while a save,
    // decision, or daihyosen request is mid-flight.
    if (submitting || decisionSubmitting || daihyosenBusy) return;
    if (isDirty && !(await window.confirmDialog({ message: "Discard unsaved scoring changes?", confirmLabel: "Discard changes", danger: true }))) return;
    onClose();
  };

  // Esc-to-close + ←/→ match nav, matching ScoreEditorModal. M/K/D/T/H ippon
  // shortcuts are wired ONLY for kachinuki bout mode (one current bout, an
  // unambiguous target — see scoreCurrentBoutWaza); fixed-format team scoring
  // is many sub-matches and stays tap-only, and Enter-to-finish isn't wired.
  const kbRef = React.useRef(null);
  kbRef.current = { submitting, handleDismiss, onPrev, onNext, kachinukiBoutMode, isNaginataTeam, scoreCurrentBoutWaza };
  useEffectA(() => {
    const onKeyDown = (ev) => {
      const s = kbRef.current;
      if (s.submitting) return;
      if (ev.ctrlKey || ev.metaKey || ev.altKey) return;
      if (ev.key === "Escape") { ev.preventDefault(); s.handleDismiss(); return; }
      if (window.isTextEntry(ev.target)) return;
      if (ev.key === "ArrowLeft" && s.onPrev) { ev.preventDefault(); s.onPrev(); return; }
      if (ev.key === "ArrowRight" && s.onNext) { ev.preventDefault(); s.onNext(); return; }
      // mp-gmcg: keyboard ippon entry, KACHINUKI bout mode only (one current
      // bout → unambiguous target; the general team editor has many). Mirrors
      // the individual editor: blocked when any interactive element
      // (input/button/link) has focus so name typing and native button
      // activation still work.
      if (!s.kachinukiBoutMode || window.isInteractiveTarget(ev.target)) return;
      if (ev.key.length !== 1) return;
      const upper = ev.key.toUpperCase();
      if (getValidPointKeys(s.isNaginataTeam).includes(upper)) {
        ev.preventDefault();
        // Shift → AKA (red, sideA); no Shift → SHIRO (white, sideB). ev.shiftKey
        // (not uppercase) avoids Caps Lock misrouting.
        s.scoreCurrentBoutWaza(ev.shiftKey ? "a" : "b", upper);
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, []);

  // left = SHIRO (White), right = AKA (Red)
  const teamSides = [
    { key: "b", name: m.sideB?.name || m.sideB, label: "SHIRO (White)", color: "shiro", iv: ivB, pw: pwB },
    { key: "a", name: m.sideA?.name || m.sideA, label: "AKA (Red)", color: "aka", iv: ivA, pw: pwA },
  ];

  // Compute whether each team's 5-person lineup is incomplete (any position
  // empty). Used for the non-blocking UI warning; does NOT block scoring.
  // mp-gmcg: suppressed entirely for kachinuki: team sizes are unregulated
  // and position vacancies are irrelevant, so "Lineup incomplete" would
  // contradict legitimate play.
  const isFivePersonLineupIncomplete = (lineup) => {
    if (teamSize !== 5 || isKachinuki) return false;
    const pos = lineup?.positions || {};
    return !pos.senpo || !pos.jiho || !pos.chuken || !pos.fukusho || !pos.taisho;
  };
  const lineupIncompleteB = isFivePersonLineupIncomplete(lineupB);
  const lineupIncompleteA = isFivePersonLineupIncomplete(lineupA);

  // a11y: label the dialog with the match/court context (mirrors the
  // individual ScoreEditorModal).
  const dialogLabel = `Team score editor: ${m.sideB?.name || m.sideB || "Shiro"} vs ${m.sideA?.name || m.sideA || "Aka"}${m.court ? ` · Shiaijo ${m.court}` : ""}`;

  const inner = (
    <>
        <div className="editor-modal__head">
          <div style={{ flex: 1 }}>
            <div className="editor-modal__eyebrow">
              {m.compName} · {m.phase === "pool" ? window.poolLabel(m) : m.round}
              {m.phase === "pool" && m.poolPosition > 0 && m.poolCount > 0
                ? <span> · Match {m.poolPosition} of {m.poolCount}</span>
                : m.phase === "bracket" && m.matchNumber > 0
                ? <span> · Match {m.matchNumber}</span>
                : null}
              {enchoPeriodCount > 0 && <span className="editor-modal__eyebrow-encho">· (E) Overtime ×{enchoPeriodCount}</span>}
            </div>
            <div className="editor-modal__title" style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
              <span><TermAS name="shiaijo">Shiaijo</TermAS> {m.court} · {m.scheduledAt || "Now"}</span>
              {/* C2: sync status indicator: inline on the title line (no dedicated
                  row); SyncStatusPill renders nothing unless the match is running. */}
              <SyncStatusPill isRunning={m.status === "running"} />
            </div>
          </div>
          <div style={{ display: "flex", flexDirection: "column", alignItems: "flex-end", gap: 4 }}>
            {(isComplete || m.status !== "running") && (
              <div className="editor-head-pill" style={{ fontSize: 10, fontWeight: 700 }}>
                {isComplete ? "CORRECTION" : "PRE-MATCH"}
              </div>
            )}
            {canClose && <button className="btn btn--ghost btn--sm" onClick={handleDismiss} disabled={submitting} style={{ padding: "2px 8px" }}>✕ Close</button>}
          </div>
        </div>

        <div className="editor-modal__body">
          {/* Team header */}
          <div className="sb-match" style={{ marginBottom: teamSize === 5 && (lineupIncompleteB || lineupIncompleteA) ? 4 : 16 }}>
            {teamSides.map((s, idx) => (
              <React.Fragment key={s.key}>
                <div className={`sb-side sb-side--${s.color}`}>
                  {/* SHIRO/AKA pill, matching the individual + Engi editors. */}
                  <div className={`sb-side__badge sb-side__badge--${s.color}`}>{s.color === "shiro" ? "Shiro" : "Aka"}</div>
                  <div className="sb-name">{s.name}</div>
                </div>
                {idx === 0 && (
                  <div className="sb-center">
                    {/* Encounter-header separator, deliberately a plain "VS" —
                        the same mp-42g entry-zone exemption as the individual
                        editor's centre, per the SCOPE note in bracket.jsx (the
                        master statement; change it there first). The
                        result-projecting surface in this file is the per-bout
                        row, which goes through renderTeamBoutMiddle → boutMiddle.
                        Don't "fix" this one to match that one; they answer
                        different questions. */}
                    <div className="sb-vs">VS</div>
                  </div>
                )}
              </React.Fragment>
            ))}
          </div>
          {/* Non-blocking lineup-incomplete hints: one per team, shown only
              for 5-person teams when Senpo or Taisho is unset or any position
              is empty. Muted and informational: does NOT block scoring. */}
          {teamSize === 5 && (lineupIncompleteB || lineupIncompleteA) && (
            <div style={{ display: "flex", gap: 8, marginBottom: 12 }}>
              {[
                { incomplete: lineupIncompleteB, label: "SHIRO" },
                { incomplete: lineupIncompleteA, label: "AKA" },
              ].map(({ incomplete, label }) => incomplete ? (
                <div key={label} className="tsm-lineup__incomplete">
                  {label}: Lineup incomplete, add the remaining players
                </div>
              ) : null)}
            </div>
          )}

          {/* Individual match rows. T136: in kachinuki mode only the
              current bout is rendered (see visiblePositions /
              kachinukiVisiblePositions above). The server appends new bouts
              via engine.MaybeAdvanceKachinuki after each score record, so
              the operator re-opens the modal to score the next bout.
              The .team-bouts-scroll wrapper gives the roomy (non-compact)
              layout an independent scroll region for the bout list so the
              team header / summary / decision / footer stay anchored. */}
          <div className="team-bouts-scroll">
          {[
            // mp-gmcg: operator-led completion. The banner reads "ended"
            // ONLY for a completed match (correction view): a running
            // match is always live bout-by-bout scoring, even when a
            // sub-result carries a kachinuki-exhaustion decision (roster
            // data is advisory; the operator decides when it's over).
            isKachinuki && (
              <div key="kachinuki-banner" style={{ background: "var(--bg-2, #f1f5f9)", border: "1px solid var(--accent, #ddd)", borderRadius: 4, padding: "8px 12px", marginBottom: 12, fontSize: 12, display: "flex", flexDirection: "column", gap: 4 }}>
                <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
                  <span style={{ fontWeight: 700 }}><TermAS name="kachinuki">Kachinuki</TermAS> (winner stays on)</span>
                  <span style={{ color: "var(--ink-2)" }}>
                    {isComplete
                      ? "Match ended."
                      : "Score the current bout, then Record bout to continue (the next pairing is added), or End match to finish on the last scored bout."}
                  </span>
                </div>
                {/* TODO(T136): inline auto-refresh after each score so
                    operators don't have to close+reopen the modal:
                    requires hooking the onSubmit response (current
                    flow forwards through parent + closes the modal). */}
              </div>
            ),
            // mp-gmcg: the bouts already fought render as read-only rows above
            // the current editable bout (pre-rendered elements pass through the
            // isValidElement gate below, like the banner). kachinukiDoneBoutIdxs
            // is already [] outside kachinuki bout mode, so no guard is needed.
            // The one done bout opened for correction (editingDoneBoutIdx) emits
            // its POSITION instead — falling through to the shared editable-bout
            // block below, in its own chronological slot — followed by a footer
            // (Done + winner-flip warning). positions holds unique 1..N values,
            // so positions.indexOf round-trips the index cleanly.
            ...kachinukiDoneBoutIdxs.flatMap(idx => (
              idx === editingDoneBoutIdx
                ? [positions[idx], renderCorrectionWarning(idx)]
                : [renderReadOnlyBout(idx)]
            )),
            ...visiblePositions,
          ].filter(Boolean).map((pos, _displayIdx) => {
            // Kachinuki returns a banner element as the first item; pass
            // it through unchanged. Other items are position strings: 
            // map them back to their canonical index in `positions`.
            if (React.isValidElement(pos)) return pos;
            const idx = positions.indexOf(pos);
            const s = subs[idx];
            const t = subTotals[idx];
            // T131: pull the per-side player + position label. existingSub
            // (from the match) and lineup data are both consulted so the
            // bout cell shows e.g. "Match 1 (Senpo): A. Tanaka vs B. Sato".
            const isDaihyoRow = idx === daihyosenIdx;
            const existingSubAtIdx = (m.subResults || []).find(sr => sr.position === (isDaihyoRow ? DAIHYOSEN_POSITION : idx + 1));
            const posLabel = isDaihyoRow ? "Daihyosen" : positionLabelFor(teamSize, idx, existingSubAtIdx);
            // No FIK position handle (Senpo/Jiho/...) for the daihyosen rep bout,
            // nor for kachinuki: with winner-stays-on a bout is the previous
            // winner vs the NEXT team's fighter, not "Jiho vs Jiho", so the label
            // misleads. This is the single home for the rule (the read-only rows
            // simply never render it).
            const posAbbrev = (isDaihyoRow || isKachinuki) ? "" : positionAbbrevFor(teamSize, idx, existingSubAtIdx);
            // Resolve the player name occupying this position on each
            // side: lineup data first (canonical when present), then the
            // SubMatchResult.SideA/SideB strings from a prior score.
            //
            // 5-person teams use named position keys (senpo, jiho, ...);
            // other sizes use the numeric string "1".."N". Try both
            // shapes so this stays size-agnostic.
            const posKey5 = (teamSize === 5 && idx < 5) ? POS_KEYS_5[idx] : null;
            const posKeyN = String(positions[idx]);
            // Same lineup→competitor resolution buildPatch uses (DRY).
            const { aName: playerAName, bName: playerBName } = playerNamesForBout(idx);

            // Feature 2 / layout: each player's name select lives WITH that
            // side's score controls (grouped, and aligned down the sheet),
            // not in the position column. Compute the per-side name props here
            // so they can ride on the rowSides entries below.
            const lineupPosKey = posKey5 || posKeyN;
            const teamIdB = teamIdForSide(m.sideB); // SHIRO = left
            const teamIdA = teamIdForSide(m.sideA); // AKA = right
            const rosterB = rosterForSide(m.sideB, lineupB);
            const rosterA = rosterForSide(m.sideA, lineupA);
            const pickPlayer = (teamId, lineup) => (value) => {
              submitInlineLineup(teamId, lineup, lineupPosKey, value);
            };
            // mp-gmcg: a manually-added bout has no lineup key (positions
            // beyond teamSize are not valid lineup keys) and no server
            // entry, so its player picks are stored on the local sub state
            // (aName/bName) and ride the subResult itself. The picker is
            // forced to render even with an empty roster so a free-typed
            // name ("+ Add …") is always possible.
            const isManualRow = isKachinuki && !isDaihyoRow && manualBouts.includes(idx + 1);
            const pickManual = (sideKey) => (value) => updateSub(idx, prev => ({ ...prev, [sideKey]: value }));
            // mp-gmcg: a kachinuki side with NO resolved name AND no lineup
            // route gets a free-typed name input riding the sub (like a
            // manual row). This covers the one-sided WALKOVER SLOT the engine
            // appends when a hikiwake empties one advisory roster (spec 006
            // decision 2): the operator can fill the empty side to keep
            // fighting (taisho rule / a fighter the app has never seen)
            // instead of taking the walkover fusensho. Scoped tightly: for
            // positions 1..teamSize with a roster present, name picks keep
            // routing through the INLINE LINEUP submit (existing T131 flow);
            // the free path applies only where that route cannot work —
            // position beyond teamSize (not a valid lineup key, the PUT
            // would 4xx) or no roster to pick from.
            const freeNameA = isKachinuki && !isDaihyoRow && !playerAName
              && (idx + 1 > teamSize || !(rosterA && rosterA.length));
            const freeNameB = isKachinuki && !isDaihyoRow && !playerBName
              && (idx + 1 > teamSize || !(rosterB && rosterB.length));

            // Each row: [left side, center score, right side]: left=SHIRO, right=AKA
            // T096/FR-031: manual pts/fouls edits clear the per-bout fusensho
            // flag AND discard the _preFusensho snapshot so the bout becomes
            // a regular fought score once the operator intervenes. Re-applying
            // via the Fusensho button captures a fresh snapshot from the
            // current (manually-edited) state.
            // onIncrement applies the FIK 2-foul rule via applyFoulIncrement:
            // the 2nd foul auto-awards an H to the OPPONENT and resets this
            // side's foul counter. The auto-award also invalidates the
            // _preFusensho snapshot: once an H lands in the slot the prior
            // pre-fusensho state is stale.
            const rowSides = [
              {
                key: "b", pts: s.bPts, fouls: s.bFouls,
                setPts: (pts) => updateSub(idx, prev => ({ ...prev, bPts: pts, fusensho: "", _preFusensho: undefined, draw: false })),
                setFouls: (f) => updateSub(idx, prev => ({ ...prev, bFouls: f, fusensho: "", _preFusensho: undefined, draw: false })),
                onIncrement: () => updateSub(idx, prev => {
                  const r = applyFoulIncrement(prev.bFouls, prev.aPts, prev.bPts);
                  return { ...prev, bFouls: r.fouls, aPts: r.opponentPts, fusensho: "", _preFusensho: undefined, draw: false };
                }),
                color: "shiro", label: "SHIRO",
                // The daihyosen is a representative bout, not a lineup position:
                // "daihyosen" is not a valid lineup key (domain/team_lineup.go
                // accepts only senpo/… or "1".."N"), so a name pick there would
                // 4xx. Suppress the picker by passing an empty roster (the input
                // only renders when roster.length > 0).
                playerName: playerBName, roster: isDaihyoRow ? [] : rosterB, forceInput: isManualRow || freeNameB,
                onSelectName: (isManualRow || freeNameB) ? pickManual("bName") : pickPlayer(teamIdB, lineupB),
              },
              {
                key: "a", pts: s.aPts, fouls: s.aFouls,
                setPts: (pts) => updateSub(idx, prev => ({ ...prev, aPts: pts, fusensho: "", _preFusensho: undefined, draw: false })),
                setFouls: (f) => updateSub(idx, prev => ({ ...prev, aFouls: f, fusensho: "", _preFusensho: undefined, draw: false })),
                onIncrement: () => updateSub(idx, prev => {
                  const r = applyFoulIncrement(prev.aFouls, prev.bPts, prev.aPts);
                  return { ...prev, aFouls: r.fouls, bPts: r.opponentPts, fusensho: "", _preFusensho: undefined, draw: false };
                }),
                color: "aka", label: "AKA",
                // See SHIRO note above: no lineup picker on the daihyosen row.
                playerName: playerAName, roster: isDaihyoRow ? [] : rosterA, forceInput: isManualRow || freeNameA,
                onSelectName: (isManualRow || freeNameA) ? pickManual("aName") : pickPlayer(teamIdA, lineupA),
              },
            ];

            // Sub-bout is decided once either side reaches 2 ippons.
            const subBoutDecided = isBoutDecided(s.aPts, s.bPts);

            // The side key ("a"/"b") that won the hantei on this row, else "".
            const dhHantei = isDaihyoRow && daihyosenTied ? daihyosenHantei : "";
            // Clicking the Ht undoes the hantei, the same way clicking a point
            // removes it: a mis-entered referee decision must be correctable
            // without leaving the row.
            const clearHantei = () => { setDaihyosenHanteiArmed(false); setDaihyosenHantei(""); };
            // One shape for both sides. Everything that differs is derived from
            // the side itself rather than passed in, so a caller cannot pair a
            // side with the other side's slot index or testid: aka renders its
            // pair reversed (so each side's first ippon stays on its name side),
            // and only the hantei winner carries an Ht.
            const ptSlots = (rs) => {
              const htSlot = hanteiSlot(dhHantei === rs.key, rs.pts);
              const order = rs.color === "aka" ? [1, 0] : [0, 1];
              return order.map(i => {
                const isHt = htSlot === i;
                return (
                  <button key={i} className={`editor-side__pt ${(isHt || rs.pts[i]) ? "editor-side__pt--filled" : ""}`}
                    data-testid={isHt ? `team-daihyosen-ht-${rs.color}` : undefined}
                    onClick={() => (isHt ? clearHantei() : rs.setPts(rs.pts.filter((_, j) => j !== i)))}
                    title={isHt ? "Hantei winner: click to undo" : "Click to remove"}>
                    {isHt ? "Ht" : (rs.pts[i] || "·")}
                  </button>
                );
              });
            };

            // The centre carries the SINGLE-SOURCE boutMiddle projection only
            // (vs/X/(E)/(DH)) — never restated here, and never a result mark or
            // a numeric bout score (CLAUDE.md). dhHantei is passed so a hantei
            // winner un-draws a level scoreline instead of centring an X.
            const scoreDisplay = renderTeamBoutMiddle(s, t, isDaihyoRow, dhHantei);

            return (
              <div key={idx} className={"team-sub-match" + (idx === editingDoneBoutIdx ? " team-sub-match--correcting" : "")}>
                <div className="team-sub-match__pos" title={posLabel}>
                  {/* mp-gmcg: a bout opened for correction gets the same black
                      triangle as its collapsed row, now pointing down (rotated
                      via --open) and clickable to collapse it back. Only the
                      corrected past bout carries it; the live current bout does
                      not (it is never collapsible). */}
                  {idx === editingDoneBoutIdx && (
                    <button type="button" className="tsm-caret tsm-caret--open tsm-caret-btn" data-testid={`kachinuki-done-collapse-${idx}`}
                      aria-label={`Collapse bout ${idx + 1}`} aria-expanded={true} onClick={closeDoneBoutEdit}>▶</button>
                  )}
                  {/* Bout number AND the FIK position handle (Sen/Ji/Chu/Fuk/Tai
                      for 5-person teams): operators think in positions, so the
                      abbreviation rides in the row instead of hiding in the
                      title tooltip (unreachable on touch). The number stays as
                      the size-agnostic anchor; >5-person teams show it alone.
                      Daihyosen (the rep bout) shows "DH". */}
                  <span className="team-sub-match__pos-num">{isDaihyoRow ? "DH" : idx + 1}</span>
                  {/* posAbbrev is already "" for kachinuki + daihyosen (see its
                      computation above), so only the number shows for those. */}
                  {posAbbrev && (
                    <span className="team-sub-match__pos-name">{posAbbrev}</span>
                  )}
                </div>
                <div className="team-sub-match__row">
                  {rowSides.map((rs, rsIdx) => (
                    <React.Fragment key={rs.key}>
                      <div className={`team-sub-match__side team-sub-match__side--${rs.color} ${rsIdx === 1 ? "team-sub-match__side--right" : ""}`}>
                        {/* Name picker grouped with this side's score controls.
                            SHIRO chip + a typeable picker (filter the roster as
                            you type, or write a name) so operators can set the
                            order live; falls back to a static name when there's
                            no roster metadata. Lineups are always editable. */}
                        <div className="tsm-name">
                          <span className={`se-color-badge se-color-badge--${rs.color}`}>{rs.label}</span>
                          {(rs.roster && rs.roster.length > 0) || rs.forceInput ? (
                            <LineupNameInput
                              value={rs.playerName || ""}
                              roster={rs.roster}
                              color={rs.color}
                              disabled={inlineLineupSaving}
                              ariaLabel={`${posLabel} ${rs.label} player`}
                              onSelect={(name) => rs.onSelectName(name)}
                            />
                          ) : (
                            rs.playerName
                              ? <span className="tsm-name__static">{rs.playerName}</span>
                              : <span className="tsm-name__static tsm-name__static--empty">-</span>
                          )}
                        </div>
                        {/* Row 1: point slots + M/K/D/T/H buttons. In compact
                            mode these align on one horizontal channel-strip;
                            in roomy mode the wrapper is display:contents so the
                            legacy column stack is preserved. */}
                        <div className="tsm-row-1">
                          {/* Buttons only: the scored ippon letters show in the
                              centre column (between the two competitors), like an
                              individual bout. H (hansoku point) renders as △ there. */}
                          <div className="team-sub-match__btns">
                            {getIpponButtons(isNaginataTeam).map(cc => (
                              <button key={cc} className={`ipt-btn ipt-btn--sm ${cc === "H" ? "ipt-btn--h" : ""}`}
                                onClick={() => rs.setPts(rs.pts.length < MAX_IPPONS_PER_SIDE ? [...rs.pts, cc] : rs.pts)}
                                disabled={subBoutDecided}>{cc}</button>
                            ))}
                          </div>
                        </div>
                        {/* Row 2: foul stepper + per-bout Fusensho button.
                            Independent foul counter. The `+` button calls
                            onIncrement which applies the FIK 2-foul rule via
                            applyFoulIncrement (auto-award H to opponent, reset
                            counter to 0). The discharged H is physically in
                            the opponent's pts array: no derived display.
                            T096/FR-031: per-bout Fusensho: awards the bout
                            2-0 to this side. Re-clicking the active side
                            undoes the fusensho; manual pts/fouls edits while
                            active clear the flag and discard the snapshot. */}
                        <div className="tsm-row-2">
                          <div className="tsm-fouls" data-testid={`scoring-modal-hansoku-${rs.color}`}>
                            <span className="tsm-fouls__label">{rs.label} Fouls</span>
                            <div className="tsm-fouls__controls">
                              <button className="tsm-fouls__btn" onClick={() => rs.setFouls(nextFoulOnDecrement(rs.fouls))} disabled={rs.fouls === 0}>−</button>
                              <span className={`tsm-fouls__count ${rs.fouls >= 1 ? "tsm-fouls__count--warn" : ""}`}>{rs.fouls}</span>
                              <button className="tsm-fouls__btn" onClick={rs.onIncrement} disabled={subBoutDecided}>+</button>
                            </div>
                          </div>
                          <div className="tsm-fusensho">
                            <button
                              data-testid="scoring-modal-fusensho-button"
                              type="button"
                              className={`btn btn--sm ${s.fusensho === rs.key ? "btn--primary" : ""}`}
                              onClick={() => setFusenshoFor(idx, rs.key)}
                              title={s.fusensho === rs.key
                                ? `Click to undo fusensho: restores the previous score`
                                : `Mark bout as fusensho: default win 2-0 to ${rs.label}`}
                            >
                              {s.fusensho === rs.key ? "✓ Fusensho" : "Fusensho"}
                            </button>
                          </div>
                        </div>
                      </div>
                      {rsIdx === 0 && (
                        <div className="team-sub-match__center">
                          <div className="tsm-center-marks">
                          <div className="tsm-center-pts tsm-center-pts--shiro">
                            {/* Outstanding hansoku → red ▲ between the competitor's
                                name and that side's ippon slots, so rendered before
                                the slots. A 2nd foul discharges to an H ippon for the
                                opponent and clears this. (FIK Table 2, p.16 Taisho
                                row; running_a_kendo_tournament.md: ▲ next to name.) */}
                            {rowSides[0].fouls >= 1 && <span className="tsm-foul-tri" title="Hansoku: 1 foul">▲</span>}
                            {ptSlots(rowSides[0])}
                          </div>
                          <div className={`team-sub-match__score ${scoreDisplay && t.winner === "b" ? "team-sub-match__score--a-win" : scoreDisplay && t.winner === "a" ? "team-sub-match__score--b-win" : ""}`}>
                            {scoreDisplay}
                          </div>
                          <div className="tsm-center-pts tsm-center-pts--aka">
                            {/* Aka fills outside-in: its first ippon sits in the
                                outer (right) one of its OWN two slots, nearest the
                                Aka name, so render the slots in reverse (pts[1] then
                                pts[0]). The pair itself stays inside, flanking the
                                centre score (FIK Table 2, p.16). */}
                            {ptSlots(rowSides[1])}
                            {/* Outstanding hansoku → red ▲ between the Aka name and
                                that side's ippon slots, so after the reversed slots. */}
                            {rowSides[1].fouls >= 1 && <span className="tsm-foul-tri" title="Hansoku: 1 foul">▲</span>}
                          </div>
                          </div>
                          {/* Per-bout tie toggle, directly beneath the centre
                              marks: pressing it puts an X on the centre line
                              (hikiwake). Hidden once a side has decided the bout,
                              and on the daihyosen (its own hantei flow). */}
                          {!isDaihyoRow && !subBoutDecided && (
                            <div className="team-sub-match__tie">
                              <button
                                type="button"
                                data-testid="scoring-modal-tie-button"
                                className={`btn btn--sm ${s.draw ? "btn--primary" : ""}`}
                                onClick={() => setDrawFor(idx)}
                                title={s.draw ? "Undo tie" : "Mark this bout a draw (hikiwake)"}
                              >
                                {s.draw ? "✓ Tie (hikiwake)" : "Tie (hikiwake)"}
                              </button>
                            </div>
                          )}
                        </div>
                      )}
                    </React.Fragment>
                  ))}
                </div>
              </div>
            );
          })}
          </div>

          {/* mp-gmcg: manual next bout. Secondary/unobtrusive: Record bout
              stays the primary flow (the server auto-appends the pairing it
              can infer). This is for the unknown-roster continue path: team
              sizes are unregulated, so the server may not know the next
              fighter. Disabled until the current bout is scored (the next
              bout comes after the current one) and at the theoretical
              kachinuki bout cap. */}
          {kachinukiBoutMode && (
            <div style={{ display: "flex", alignItems: "center", gap: 8, marginTop: 8, fontSize: 12 }}>
              <button
                type="button"
                className="btn btn--ghost btn--sm"
                data-testid="kachinuki-add-bout-button"
                onClick={addManualBout}
                disabled={submitting || !kachinukiCurrentBoutPlayed || kachinukiNextManualPos > kachinukiMaxBouts}
                title={!kachinukiCurrentBoutPlayed
                  ? "Score the current bout first"
                  : kachinukiNextManualPos > kachinukiMaxBouts
                  ? "Bout limit reached"
                  : "Add the next pairing yourself when it isn't added automatically"}
              >
                + Add next bout manually
              </button>
              <span style={{ color: "var(--ink-3)" }}>
                For fighters the app doesn’t know: pick or type both players on the new row.
              </span>
            </div>
          )}

          {/* mp-gmcg: explicit undo for a bout appended by mistake. Renders only
              when the current bout is an unscored EXTRA (a prior bout was
              scored), i.e. exactly the row the End-match strip would drop — made
              visible and reversible on the spot instead of silent. */}
          {kachinukiBoutRemovable && (
            <div style={{ display: "flex", alignItems: "center", gap: 8, marginTop: 8, fontSize: 12, flexWrap: "wrap" }}>
              <button
                type="button"
                className="btn btn--ghost btn--sm"
                data-testid="kachinuki-remove-bout-button"
                onClick={removeCurrentBout}
                disabled={submitting || removingBout}
                title="Remove this empty bout — added by mistake. Nothing has been recorded for it, so nothing is lost."
              >
                {removingBout ? "Removing…" : "× Remove this bout"}
              </button>
              <span style={{ color: "var(--ink-3)" }}>
                Added a pairing by mistake? This bout has no score yet, so removing it loses nothing.
              </span>
              {removeBoutErr && (
                <span data-testid="kachinuki-remove-bout-error" style={{ color: "var(--danger, #c00)", width: "100%" }}>{removeBoutErr}</span>
              )}
            </div>
          )}

          {/* Team summary: T138: sticky to the top of the modal body so
              the totals stay visible as the operator scrolls through many
              bout rows. zIndex: 5 keeps it under the modal head (10) but
              above the bout cells.
              mp-gmcg band redesign (user-confirmed brief): in kachinuki the
              centre cell carries BOUT-LOG FACTS while running (bout number +
              last bout result; never a verdict — the old IV/PW-derived
              "AKA WIN" contradicted the End gate) and the verdict only on
              completion, derived from the match winner. IV/PW demote to the
              side cells (they still feed standings tie-breaks). Non-kachinuki
              team matches keep RESULT + teamVerdictText: fixed formats ARE
              decided by IV/PW. */}
          {/* mp-gmcg: in the RUNNING kachinuki scorer the fought bouts render as
              read-only rows above (renderReadOnlyBout), so the IV/PW summary band
              is redundant and is dropped. Completed kachinuki (correction verdict)
              and fixed-format team matches keep it. */}
          {!kachinukiBoutMode && (() => {
            const kb = isKachinuki && !hasDaihyosen
              ? kachinukiBandModel({
                  subs, daihyosenIdx, isComplete,
                  namesAt: playerNamesForBout,
                  // RAW winner/sides (not pre-flattened to names): winnerSideLR
                  // inside kachinukiBandModel needs the {id,name} shape to prefer
                  // id equality over name (review: two teams CAN share a name).
                  matchWinner: m.winner,
                  matchDecision: m.decision,
                  sideA: m.sideA, sideB: m.sideB,
                  currentBout: parseInt(visiblePositions[visiblePositions.length - 1], 10) || undefined,
                })
              : null;
            return (
              <div className="team-summary" style={{ position: "sticky", top: 0, zIndex: 5 }}>
                {teamSides.map((ts, idx) => (
                  <React.Fragment key={ts.key}>
                    {/* idx 0 = SHIRO (left, default left-align); idx 1 = AKA, which
                        sits in the right 1fr grid cell and must right-align to mirror
                        SHIRO — the --right class existed but was never wired up, so
                        AKA's IV/PW floated mid-panel. */}
                    <div className={`team-summary__side${idx === 1 ? " team-summary__side--right" : ""}`}>
                      <div className="team-summary__label">{ts.label}</div>
                      <div className="team-summary__stats">IV: {ts.iv} · PW: {ts.pw}</div>
                    </div>
                    {idx === 0 && (
                      <div className="team-summary__side team-summary__side--center">
                        {kb ? (
                          <>
                            <div className="team-summary__label">{kb.headline}</div>
                            {kb.verdict
                              ? <div className={`team-summary__verdict team-summary__verdict--${kb.verdictSide}`} data-testid="team-summary-verdict">{kb.verdict}</div>
                              : (kb.fact ? <div className="team-summary__fact" data-testid="team-summary-fact">{kb.fact}</div> : null)}
                          </>
                        ) : (
                          <>
                            <div className="team-summary__label">RESULT</div>
                            <div className="team-summary__verdict">{teamVerdictText}</div>
                          </>
                        )}
                      </div>
                    )}
                  </React.Fragment>
                ))}
              </div>
            );
          })()}

          {/* mp-4pc: hantei affordance for the daihyosen: the rep bout is
              the only team sub-bout that may be decided by judges (FIK 7-5 /
              29-6). Encho is optional: a tied daihyosen may be taken straight
              to a judges' decision. Mounts whenever a daihyosen exists;
              arming requires a tied scoreline. The chosen winner rides onto
              the position DAIHYOSEN_POSITION sub (decidedByHantei) when the operator saves. */}
          {hasDaihyosen && (() => {
            const dt = subTotals[daihyosenIdx];
            const tiedScore = dt.aTotal === dt.bTotal;
            return (
              <div className="hantei-row" data-testid="team-daihyosen-hantei-row" style={{ display: "flex", gap: 8, alignItems: "center", padding: "6px 8px", marginTop: 12, background: "var(--surface-2)", borderRadius: 6, fontSize: 12 }}>
                <span style={{ fontWeight: 600, color: "var(--ink-2)" }}>Daihyosen hantei</span>
                <span style={{ color: "var(--ink-3)" }}>(judges' decision)</span>
                {dt.aTotal === 0 && dt.bTotal === 0 && !daihyosenHanteiArmed && (
                  <button
                    type="button"
                    className="btn btn--ghost btn--sm"
                    data-testid="team-daihyosen-remove"
                    title="Remove the representative bout"
                    onClick={onRemoveDaihyosen}
                    disabled={daihyosenBusy || submitting || decisionSubmitting}
                  >
                    Remove daihyosen
                  </button>
                )}
                {!daihyosenHanteiArmed && (
                  <button
                    type="button"
                    className="btn btn--sm"
                    data-testid="team-daihyosen-hantei-arm"
                    onClick={() => setDaihyosenHanteiArmed(true)}
                    disabled={submitting || decisionSubmitting || !tiedScore}
                    title={!tiedScore ? "Hantei applies only to a tied daihyosen" : "Record a judges' decision"}
                    style={{ marginLeft: "auto" }}
                  >
                    Decide by hantei…
                  </button>
                )}
                {daihyosenHanteiArmed && (
                  <div style={{ marginLeft: "auto", display: "flex", gap: 6 }}>
                    <button type="button" className={`btn btn--sm ${daihyosenHantei === "b" ? "btn--primary" : ""}`} data-testid="team-daihyosen-hantei-shiro"
                      onClick={() => setDaihyosenHantei("b")} disabled={submitting || decisionSubmitting}>SHIRO wins</button>
                    <button type="button" className={`btn btn--sm ${daihyosenHantei === "a" ? "btn--primary" : ""}`} data-testid="team-daihyosen-hantei-aka"
                      onClick={() => setDaihyosenHantei("a")} disabled={submitting || decisionSubmitting}>AKA wins</button>
                    <button type="button" className="btn btn--ghost btn--sm" data-testid="team-daihyosen-hantei-cancel"
                      onClick={() => { setDaihyosenHanteiArmed(false); setDaihyosenHantei(""); }} disabled={submitting || decisionSubmitting}>Cancel</button>
                  </div>
                )}
              </div>
            );
          })()}

          {/* mp-c2yr: daihyosen (representative bout) affordance: an
              always-available manual control for any unfinished knockout
              team match. The operator decides when a tie needs a
              representative bout, so the button is never gated behind
              auto-detection (the old `allComplete && tied` gate silently
              hid it whenever the tie involved a drawn 0–0 bout, which a
              5-person tie always does). It is *highlighted* when a tie on
              IV+PW is detected locally; otherwise it sits quietly as a
              ghost button. Clicking it flushes the current bout scores
              (the backend recomputes the tie from the PERSISTED SubResults,
              so an unsaved tie would otherwise read as not_tied) and then
              POSTs to /daihyosen; the server appends a SubMatchResult with
              decision="daihyosen" that the operator scores via the regular
              bout flow. Errors map to user-visible strings per the contract
              in handlers_daihyosen.go. Once a daihyosen exists it renders as
              a scoreable row above (mp-4pc), so don't offer a second. */}
          {(() => {
            // mp-gmcg: daihyosen does not exist in kachinuki (a tied final
            // bout goes to encho on that same bout; the server now 400s a
            // kachinuki daihyosen POST), so the ADD affordance is hidden.
            // Existing/legacy daihyosen rows still render defensively via
            // hasDaihyosen above.
            if (hasDaihyosen || !isKnockoutPhase || isKachinuki) return null;
            // Local tie detection drives the highlight + helper copy only: 
            // the backend is the source of truth and re-validates on submit.
            // A bout is "decided" once it carries any ippon or is a draw; a
            // 5-person tie reaches even IV only via at least one drawn bout,
            // so draws MUST count here (the bug the old gate had).
            const anyBoutDecided = subTotals.some(t => t.aTotal > 0 || t.bTotal > 0 || t.draw || t.winner !== null);
            const teamTied = anyBoutDecided && ivA === ivB && pwA === pwB;
            const onDaihyosen = async () => {
              setEditorErr("");
              setDaihyosenBusy(true);
              try {
                // Persist the operator's current bout scores first (status
                // stays "running"); the backend derives the tie from the
                // saved SubResults, so a freshly-scored-but-unsaved tie
                // would otherwise be rejected as not_tied.
                //
                // recordScore returns { queued: true } when the write could
                // only be enqueued (offline / retryable 5xx) instead of being
                // confirmed by the server. Daihyosen is a hard prerequisite on
                // that persistence, so a queued (unconfirmed) save MUST abort the
                // flow: otherwise recordDaihyosen runs against the stale
                // server-side SubResults. The queued write still delivers in the
                // background, so a retry succeeds once the connection is back.
                const saveRes = await window.API.recordScore(m.compId, m.id, buildPatch("running"), resolveDecisionPassword(password), m);
                assertRunningWritePersisted(saveRes); // abort if the save was only queued, not server-confirmed
                await window.API.recordDaihyosen(m.compId, m.id, resolveDecisionPassword(password));
                if (!mountedRef.current) return;
                // Closing + reopening is the cleanest cross-cutting refresh
                // path. The parent listens for SSE match_updated and pushes
                // the new bout when re-opened.
                onClose();
              } catch (e) {
                if (!mountedRef.current) return;
                const msg = String(e?.message || "");
                let userMsg = msg;
                if (msg === "not_tied") userMsg = "Daihyosen needs a tie on IV and PW (this encounter already has a winner)";
                else if (msg === "pool_match") userMsg = "Daihyosen is only for knockout matches";
                else if (msg === "insufficient_eligibility") userMsg = "Not enough eligible competitors for a representative bout";
                else if (msg === "score_not_synced") userMsg = "Couldn't save the current scores (offline or server busy). Try again once the connection is back.";
                else if (!userMsg) userMsg = "Could not add a representative bout";
                setEditorErr(userMsg);
              } finally {
                if (mountedRef.current) setDaihyosenBusy(false);
              }
            };
            return (
              <div className={`daihyosen-controls${teamTied ? " daihyosen-controls--tied" : ""}`}>
                <div className="daihyosen-controls__title">
                  {teamTied ? "Match tied on IV and PW" : <>Tie-breaker (<TermAS name="daihyosen">daihyosen</TermAS>)</>}
                </div>
                <div className="daihyosen-controls__hint">
                  {teamTied
                    ? <>This encounter is tied. Add a representative bout (<TermAS name="daihyosen">daihyosen</TermAS>) to decide it. Each side picks one eligible competitor, scored like any other sub-match.</>
                    : <>A knockout encounter must have a winner. If the bouts end tied, add a representative bout (<TermAS name="daihyosen">daihyosen</TermAS>) to break it.</>}
                </div>
                {/* Plain-text label only: a glossary <TermAS> inside the
                    button would swallow the tap via stopPropagation (the
                    term's own click handler), leaving a dead-zone over the
                    word. The term is taught in the title/hint above instead. */}
                <div>
                  <button data-testid="scoring-modal-daihyosen-button" type="button" className={`btn btn--sm ${teamTied ? "btn--primary" : "btn--ghost"}`} onClick={onDaihyosen} disabled={daihyosenBusy}>
                    {daihyosenBusy ? "Adding…" : "Add representative bout"}
                  </button>
                </div>
              </div>
            );
          })()}

          {/* The modal's ONE inline error surface: the daihyosen add/remove
              POSTs AND the inline lineup-position PUT all report here. It
              used to live INSIDE the add-daihyosen block above, which returns
              null for kachinuki / pool / already-has-a-daihyosen matches — so
              a failed lineup save (reachable exactly in the kachinuki flow)
              set a message the operator never saw. Rendered here, a sibling
              of decisionErr, it shows wherever it is set. */}
          {editorErr && (
            <div data-testid="team-editor-error" className="daihyosen-controls__err" style={{ marginTop: 6 }}>{editorErr}</div>
          )}

          {/* Ippon-type letter legend: same affordance as the individual
              editor; the per-bout buttons use the same M/K/D/T/H letters. */}
          <IpponLegend isNaginata={isNaginataTeam} />

          {/* T093–T098: decision (kiken/fusenpai) controls for the overall
              team match. Per-bout Fusensho lives on each sub-match row
              (see the row-level "Fusensho" button per side, T096). */}
          {/* mp-gmcg (critique P3): kiken/fusenpai are withdrawal/no-show
              decisions, rare relative to scoring, so they collapse into a
              disclosure by default instead of standing always-open beside the
              active bout. Native <details> keeps the buttons in the DOM
              (still queryable/testable); it just reclaims the ambient
              attention the scoring task should own. */}
          {!withdrawnPlayer && !decisionPromptKind && !selfReport && (
            <details className="decision-disclosure">
              <summary className="decision-disclosure__summary">Withdrawal or no-show (kiken · fusenpai)</summary>
              <div className="decision-controls" style={{ display: "flex", gap: 8, marginTop: 10, fontSize: 12, alignItems: "center", flexWrap: "wrap" }}>
                <div className="decision-btn-group">
                  <button data-testid="scoring-modal-kiken-voluntary-button" type="button" className="btn btn--sm" onClick={() => { setDecisionErr(""); setDecisionPromptKind("kiken-voluntary"); }} disabled={submitting || decisionSubmitting}>
                    Kiken – Voluntary
                  </button>
                  <GlossaryHintAS name="kiken-voluntary" />
                </div>
                <div className="decision-btn-group">
                  <button data-testid="scoring-modal-kiken-injury-button" type="button" className="btn btn--sm" onClick={() => { setDecisionErr(""); setDecisionPromptKind("kiken-injury"); }} disabled={submitting || decisionSubmitting}>
                    Kiken – Injury
                  </button>
                  <GlossaryHintAS name="kiken-injury" />
                </div>
                <div className="decision-btn-group">
                  <button data-testid="scoring-modal-fusenpai-button" type="button" className="btn btn--sm" onClick={() => { setDecisionErr(""); setDecisionPromptKind("fusenpai"); }} disabled={submitting || decisionSubmitting}>
                    Fusenpai
                  </button>
                  <GlossaryHintAS name="fusenpai" />
                </div>
                <span style={{ color: "var(--ink-3)", fontSize: 12, marginLeft: 4 }}>
                  (<TermAS name="fusensho">Fusensho</TermAS> is per-bout: use the "Fusensho" button on each row above.)
                </span>
              </div>
            </details>
          )}
          {decisionErr && (
            <div style={{ color: "var(--danger, #c00)", fontSize: 12, marginTop: 6 }}>{decisionErr}</div>
          )}
          {decisionPromptKind && (
            <DecisionPrompt
              kind={decisionPromptKind}
              sideA={{ name: m.sideA?.name || m.sideA }}
              sideB={{ name: m.sideB?.name || m.sideB }}
              defaultSide="shiro"
              askReason={window.isKikenDecision(decisionPromptKind)}
              // Same server-owned obligation the End-match flow honours via
              // reopenReasonRequired: a decision finalizes the match too, so
              // POST /decision rejects it without a reason. Rides m.reopenPending
              // (not local state) for the same reason the End prompt does — the
              // editor remounts per match and a client-only flag would evaporate.
              requireReason={reopenReasonRequired}
              submitting={decisionSubmitting}
              onCancel={() => { setDecisionPromptKind(""); setDecisionErr(""); }}
              onSubmit={({ decisionBy, decisionReason }) => submitDecision(decisionPromptKind, { decisionBy, decisionReason })}
            />
          )}
          {withdrawnPlayer && (
            <RemainingMatchesPanel
              compID={m.compId}
              password={resolveDecisionPassword(password)}
              withdrawnPlayer={withdrawnPlayer}
              onAwarded={() => { /* stay open; operator decides when to close */ }}
              onClose={() => { setWithdrawnPlayer(null); onClose(); }}
            />
          )}

          {/* FR-033 encho toggle. Placed at the BOTTOM, beside the End/Reopen
              controls (operator feedback: controls belong at the bottom, not the
              top). EnchoControl collapses to a pill when no overtime is active.
              mp-gmcg (critique + operator ruling): suppressed in kachinuki bout
              mode — declaring encho there is OPTIONAL, its only effect the middle
              mark (vs → "(E)"), and it is done via the footer Encho button, so a
              period-stepper would be redundant AND confusing. Corrections,
              daihyosen and fixed-format team matches keep it. */}
          {!kachinukiBoutMode && (
            <EnchoControl
              enchoPeriodCount={enchoPeriodCount}
              setEnchoPeriodCount={setEnchoPeriodCount}
            />
          )}

        </div>

        <div className="editor-modal__foot editor-modal__foot--nav">
          {/* Audit reason prompt for team-match corrections: same contract
              as ScoreEditorModal: operator must confirm before the patch fires. */}
          {correctionPromptOpen && (
            <ReasonPrompt
              label="Reason for correction"
              presets={CORRECTION_PRESETS}
              submitting={submitting}
              onConfirm={(r) => {
                setCorrectionReason(r);
                setReasonPromptKind("");
                const patch = { ...buildPatch("completed"), correctionReason: r };
                doSubmit(() => onSubmit(patch));
              }}
              onCancel={() => setReasonPromptKind("")}
            />
          )}
          {/* mp-gmcg: the same audit prompt on the way OUT of a reopen. The
              reopen itself was one tap; this write is the one that re-finalizes
              the result, so this is where the justification is owed — and the
              server enforces it (400 correctionReason on a completing write
              while reopenPending). Its Confirm IS the commit, so the verdict
              being committed is shown above it (the End button's armed label is
              hidden while a prompt owns the footer). setCorrectionReason is
              async, so the confirmed string is spread onto the patch directly
              rather than read back through buildPatch. */}
          {reopenPromptOpen && (
            <>
              <div className="reopen-reason__verdict" data-testid="kachinuki-end-verdict">
                Ending this match: <strong>{kachinukiEndOutcomeLabel(kachinukiEndOutcome)}</strong>
              </div>
              <ReasonPrompt
                label="Reason for reopening"
                presets={REOPEN_PRESETS}
                submitting={submitting}
                onConfirm={(r) => {
                  setCorrectionReason(r);
                  setReasonPromptKind("");
                  const patch = { ...buildPatch("completed", { endOutcome: kachinukiEndOutcome }), correctionReason: r };
                  doSubmit(() => onSubmit(patch));
                }}
                onCancel={() => setReasonPromptKind("")}
              />
            </>
          )}
          {/* While a reason prompt is open it owns the only Cancel/commit
              row: hide the footer's own nav+actions so the operator never sees
              two Cancels and two commit buttons at the highest-stakes moment
              (amending or discarding a recorded result). Mirrored in
              Score/EngiScoreEditorModal. */}
          {!(correctionPromptOpen || reopenPromptOpen) && (
          <>
          {/* mp-gmcg: inline End-match hint (plain text, no modal). Shown
              while End is blocked (nothing scored yet, or a knockout tie:
              no draws in a bracket) AND on a tied last bout in pools/
              league, where ending as a draw and fighting on are BOTH
              legitimate: whether the pairing must produce a result (e.g.
              the taisho must be defeated) is operator discretion, never
              derived from the phase. The Encho affordance therefore
              renders for every tied last bout (kachinukiEnchoAvailable).
              This replaces the koTieBlocked gating for kachinuki: the
              correction-mode Finish buttons below never see a running
              kachinuki match, so there are no competing hints. */}
          {kachinukiBoutMode && (kachinukiEndOutcome?.kind === "blocked" || kachinukiEndOutcome?.kind === "draw") && (
            <div data-testid="kachinuki-end-hint" style={{ fontSize: 12, color: "var(--ink-2)" }}>
              {!kachinukiEnchoAvailable(kachinukiEndOutcome) ? (
                <span>Nothing recorded yet: score a bout before ending the match.</span>
              ) : kachinukiEndOutcome.kind === "draw" ? (
                // The Encho clause is dropped once the encounter has advanced
                // past the tied bout (kachinukiEnchoOffered false) so the hint
                // never advertises a hidden button (mp-gmcg review).
                <span>Tied bout. End match records a drawn encounter. Record bout retires both and brings the next pair up.{kachinukiEnchoOffered ? " Encho keeps the same pair fighting when this pairing must produce a result." : ""}</span>
              ) : (
                <span>No draws in a knockout. Record bout brings the next fighter up.{kachinukiEnchoOffered ? " Encho keeps the same pair on this bout." : ""} Continue until there is a point.</span>
              )}
            </div>
          )}
          {/* mp-gmcg: inline reason for the disabled [Record bout]. The tired
              operator sees the greyed button but the "why" was tooltip-only,
              unreachable on a tablet (critique P2). Shown only in the common
              "winner stays, next bout pending" state the blocked/draw hints
              above do not cover; it clears the moment the bout is scored. */}
          {kachinukiBoutMode && !kachinukiCurrentBoutPlayed && kachinukiEndOutcome?.kind === "win" && (
            <div data-testid="kachinuki-record-hint" style={{ fontSize: 12, color: "var(--ink-2)" }}>
              <span>Score this bout to enable <strong>Record bout</strong>, or <strong>End match</strong> to finish on the last scored bout.</span>
            </div>
          )}
          {reopenErr && (
            <div data-testid="kachinuki-reopen-error" style={{ color: "var(--danger, #c00)", fontSize: 12, marginBottom: 6 }}>{reopenErr}</div>
          )}
          {/* mp-gmcg: court-busy remedy. A busy court used to be a dead end
              for the one group that has no alternative (a correction bypasses
              the court gate; a kachinuki bout log can only be fixed by
              reopening). So name the match holding the court and offer to
              clear it from here.

              The warning is NOT optional chrome: revert-to-queue WIPES that
              match's partial score, and the operator tapping this is looking
              at their own match, not that one. It is stated in full, in the
              danger palette, above the button — never hidden behind a tooltip
              or implied by a red button. */}
          {reopenConflict && (
            <div className="alert alert--error reopen-conflict" data-testid="kachinuki-reopen-conflict">
              <div className="reopen-conflict__head">
                Shiaijo {reopenConflict.court || "?"} is running {blockerLabel || reopenConflict.matchId}.
              </div>
              <div className="reopen-conflict__warn" data-testid="kachinuki-reopen-conflict-warning">
                Sending it back to the queue clears any score already entered for it. Finishing that
                match instead keeps its score.
              </div>
              <div className="reopen-conflict__msg">{reopenConflict.message}</div>
              <div className="reopen-conflict__actions">
                <button
                  type="button"
                  className="btn btn--sm btn--danger"
                  data-testid="kachinuki-reopen-requeue-button"
                  onClick={requeueBlockerAndReopen}
                  disabled={reopenBusy}
                  title="Clears that match's score, returns it to the queue, then reopens this one"
                >
                  {reopenBusy ? "Working…" : "Clear its score, queue it, and reopen"}
                </button>
                <button
                  type="button"
                  className="btn btn--sm btn--ghost"
                  data-testid="kachinuki-reopen-conflict-dismiss"
                  onClick={() => setReopenConflict(null)}
                  disabled={reopenBusy}
                >
                  Leave it running
                </button>
              </div>
            </div>
          )}
          <div className="score-nav">
            {prevMatch ? (
              <button className="btn btn--sm score-nav__prev" onClick={onPrev} disabled={submitting}>← Prev</button>
            ) : <span />}
            <div className="score-nav__actions">
              {m.status === "scheduled" && (
                <button className="btn btn--sm" onClick={() => doSubmit(() => onSubmit(buildPatch("running")))} disabled={submitting}>Start match</button>
              )}
              {/* mp-gmcg: mistake recovery on a completed kachinuki match:
                  status back to running, winner/decision cleared, bout log
                  kept; the modal re-enters bout mode via the refreshed
                  match prop. ONE TAP, no prompt: the operator is standing at
                  the shiaijo with the competitors waiting, and the write is
                  reversible (the encounter is re-ended from the same bout
                  log). The audit reason is demanded on the way out instead. */}
              {canReopenKachinukiMatch({ isKachinuki, isComplete }) && (
                <button
                  type="button"
                  className="btn btn--sm btn--ghost"
                  data-testid="kachinuki-reopen-button"
                  onClick={onReopenMatch}
                  disabled={submitting || reopenBusy || decisionSubmitting}
                  title="Reopen: back to running, result cleared, bouts kept"
                >
                  {reopenBusy ? "Reopening…" : "Reopen match"}
                </button>
              )}
              {canClose && <button className="btn" onClick={handleDismiss} disabled={submitting}>Cancel</button>}
              {kachinukiBoutMode ? (
                // Kachinuki bout mode (mp-gmcg): TWO always-visible actions.
                // [Record bout]: a RUNNING write flagged kachinukiBoutFinal;
                // the server appends the next pairing (winner stays, tie
                // retires both). [End match]: an explicit COMPLETED write
                // whose outcome is derived from the last scored bout: NO
                // picker (deriveKachinukiEndOutcome). koTieBlocked
                // deliberately does not apply to Record bout (a bout
                // hikiwake is a legitimate result); End match carries its
                // own knockout-tie block + inline hint above. Record is
                // ONE TAP (hot path, recoverable); End keeps the two-step
                // arm/confirm and any score edit disarms it.
                <>
                <button type="button" className="btn btn--primary" onClick={() => {
                  // ONE TAP (user ruling via critique): Record is the hot
                  // path, repeated every bout, and a running, mergeable
                  // write — a mis-tap is recovered by editing the appended
                  // bout. End/Reopen keep the two-step arm: they are rare
                  // and terminal.
                  setEndArmed(false);
                  doSubmit(async () => {
                    const res = await onSubmit(buildPatch("running", { kachinukiBoutFinal: true }));
                    // mp-gmcg review C1: if a prior [Remove this bout] left
                    // matchOverride shadowing the `match` prop, adopt THIS
                    // write's own fresh subResults into the override directly
                    // rather than waiting for the prop to change (it can
                    // return to exactly its pre-removal length, which the
                    // id+length effect above can never observe — see that
                    // effect's comment). A failed write resolves with no
                    // subResults (the host's onSubmit swallows the error), so
                    // this is a no-op then and the still-correct override is
                    // left untouched.
                    if (mountedRef.current && res && Array.isArray(res.subResults)) {
                      setMatchOverride(prev => prev ? { ...prev, subResults: res.subResults } : prev);
                    }
                  });
                }} disabled={submitting || !kachinukiCurrentBoutPlayed}
                  title={!kachinukiCurrentBoutPlayed ? "Nothing recorded for this bout yet" : undefined}>
                  {submitting ? "Saving…" : "Record bout"}
                </button>
                {/* Encho sits BESIDE End match (critique P2): it is End's
                    alternative on a tied last bout — same pair fights on —
                    so the two must share the action row, not have Encho
                    buried in the hint paragraph. Rendered only when the tied
                    bout End would judge is still the CURRENT editable bout
                    (kachinukiEnchoOffered): once Record has advanced the
                    encounter, overtime on the past bout is meaningless and the
                    button would silently edit a read-only row (mp-gmcg review). */}
                {kachinukiEnchoOffered && (
                  <button
                    type="button"
                    className="btn"
                    data-testid="kachinuki-encho-button"
                    onClick={applyKachinukiEncho}
                    disabled={submitting}
                    title="Overtime: the same pair keeps fighting this bout"
                  >
                    Encho
                  </button>
                )}
                <button type="button" className={`btn ${endArmed ? "btn--confirm" : ""}`} data-testid="kachinuki-end-match-button" onClick={() => {
                  if (kachinukiEndOutcome?.kind === "blocked") return;
                  // mp-gmcg: on a REOPENED encounter the audit reason is owed
                  // now, and the prompt's Confirm is the commit — so it
                  // REPLACES the arm step rather than following it (one
                  // confirm, not two; same shape as the correction path).
                  // Once a reason has been confirmed it sticks, so a retry
                  // after a failed write doesn't re-ask for it.
                  if (reopenReasonRequired && !correctionReason) { setEndArmed(false); setReasonPromptKind("reopen"); return; }
                  if (!endArmed) { setEndArmed(true); setFinishArmed(false); return; }
                  doSubmit(() => onSubmit(buildPatch("completed", { endOutcome: kachinukiEndOutcome })));
                }} disabled={submitting || kachinukiEndOutcome?.kind === "blocked"}
                  title={kachinukiEndOutcome?.kind === "blocked"
                    ? (kachinukiEndOutcome.reason === "no-bouts"
                        ? "Score a bout before ending the match"
                        : "No draws in a knockout: continue (next bout or encho) until there is a point")
                    : "End the match on the last scored bout"}>
                  {submitting ? "Saving…"
                    : endArmed
                    ? `Tap again — ${kachinukiEndOutcomeLabel(kachinukiEndOutcome)}`
                    : "End match"}
                </button>
                </>
              ) : (isKachinuki && isComplete) ? null : onSubmitAndNext ? (
                // mp-gmcg: a COMPLETED kachinuki match has no generic "Save
                // correction" button. That path calls buildPatch("completed")
                // with no endOutcome, which derives the winner from the IV/PW
                // leader (teamWinner) — the exact rule kachinuki does NOT use
                // (it is decided by the LAST scored bout, via
                // deriveKachinukiEndOutcome). Correcting a finalized kachinuki
                // result therefore goes through Reopen (above): back to bout
                // mode, then End match re-derives from the last bout. Only
                // non-kachinuki completed matches keep the generic correction.
                <button className={`btn btn--primary ${finishArmed && !isComplete ? "btn--confirm" : ""}`} onClick={() => {
                  if (isComplete && !correctionReason) { setReasonPromptKind("correction"); return; }
                  if (!isComplete && !finishArmed) { setFinishArmed(true); return; }
                  doSubmit(() => (isComplete ? onSubmit : onSubmitAndNext)(buildPatch("completed")));
                }} disabled={submitting || koTieBlocked}
                  title={koTieBlocked ? "A knockout match can't be a draw: add and score a daihyosen to decide a winner" : undefined}>
                  {submitting ? "Saving…" : isComplete ? "Save correction" : koTieBlocked ? "Needs a winner" : finishArmed ? "Tap again to finish →" : "Finish + Start Next →"}
                </button>
              ) : (
                <button className={`btn btn--primary ${finishArmed && !isComplete ? "btn--confirm" : ""}`} onClick={() => {
                  if (isComplete && !correctionReason) { setReasonPromptKind("correction"); return; }
                  if (!isComplete && !finishArmed) { setFinishArmed(true); return; }
                  doSubmit(() => onSubmit(buildPatch("completed")));
                }} disabled={submitting || koTieBlocked}
                  title={koTieBlocked ? "A knockout match can't be a draw: add and score a daihyosen to decide a winner" : undefined}>
                  {submitting ? "Saving…" : isComplete ? "Save correction" : koTieBlocked ? "Needs a winner" : finishArmed ? "Tap again to finish" : "Finish"}
                </button>
              )}
            </div>
            {nextMatch ? (
              <button className="btn btn--sm score-nav__next" onClick={onNext} disabled={submitting}>Next →</button>
            ) : <span />}
          </div>
          </>
          )}
          {/* Quiet, always-present keyboard-shortcut reminder. */}
          <ScoringShortcutHint pointKeys={kachinukiBoutMode ? getValidPointKeys(isNaginataTeam) : ""} />
        </div>
    </>
  );

  if (variant === "inline") {
    return <div className="scoring-panel scoring-panel--team" aria-label={dialogLabel}>{inner}</div>;
  }

  return (
    <div className="modal-backdrop" data-testid="scoring-modal-root" onClick={handleDismiss}>
      <div className={`editor-modal editor-modal--team ${useCompact ? "editor-modal--compact" : ""}`} role="dialog" aria-modal="true" aria-label={dialogLabel} onClick={(e) => e.stopPropagation()}>
        {inner}
      </div>
    </div>
  );
}

export { resolveMatchLineup, resolveLineupTeamId };
