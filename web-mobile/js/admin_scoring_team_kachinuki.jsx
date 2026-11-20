// Team-match kachinuki rules: the pure helpers the team score editor
// (admin_scoring_team.jsx) and its tests share. No state, no hooks.
// Split out of admin_scoring_team.jsx; imports the bout rules from
// admin_scoring_team_bouts.jsx (that module never imports this one).

import { attributeWinnerSide, subBoutAttribution, nameOf, realIppons } from './result_slot.jsx';
import { winnerSideLR } from './bracket.jsx';
import { lastNumberedBout, kachinukiRecordedBoutCloses } from './kachinuki_advisory.jsx';
import { boutWinnerSide, subBoutHasBeenPlayed, teamResultLabel } from './admin_scoring_team_bouts.jsx';

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
// render for them at all. Nor does it render on an encounter a withdrawal
// decided (recordedWithdrawal, admin_scoring_shared.jsx withdrawalInForce):
// that reopen also makes the withdrawn team eligible again, so it goes
// through RecordedWithdrawal, which states that consequence and asks why
// first, rather than one silent tap. The argument is required: a caller that
// forgets it must not quietly bring the silent path back.
export function canReopenKachinukiMatch({ isKachinuki, isComplete, recordedWithdrawal }) {
  if (typeof recordedWithdrawal !== "boolean") {
    throw new TypeError("canReopenKachinukiMatch: recordedWithdrawal is required");
  }
  return !!isKachinuki && !!isComplete && !recordedWithdrawal;
}

// isKachinukiBoutRemovable: whether the [× Remove this bout] undo renders for
// the current bout. True only when the encounter is in bout mode, the current
// bout is UNSCORED, and a prior bout WAS scored (lastScoredIdx >= 0) — so the
// current bout is an appended EXTRA, never the bootstrap bout 1. That is exactly
// the row the End-match strip (stripTrailingUnscoredKachinukiBouts) would drop,
// so the button just makes the implicit strip explicit and reversible on the
// spot. mp-gmcg.
export function isKachinukiBoutRemovable({ boutMode, currentBoutPlayed, lastScoredIdx, hasLiveBout = true }) {
  // bc-kbrc: with no live bout (the last one recorded, no pairing appended)
  // there is no empty bout to remove.
  return !!boutMode && hasLiveBout && !currentBoutPlayed && lastScoredIdx >= 0;
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
    // without a marked side). Map that winner back to a side through the
    // shared owner (bc-pnum), so the row's member ids answer where they can
    // and two fighters sharing a display name name NO side -- the verdict
    // then stays blocked or drawn, which tells the operator to settle it,
    // instead of silently crediting whichever fighter is written first.
    side = attributeWinnerSide(subBoutAttribution(last));
  }
  if (side) return { kind: "win", winnerSide: side };
  if (isKnockoutPhase) return { kind: "blocked", reason: "knockout-tie" };
  return { kind: "draw" };
}

// kachinukiEndOutcomeLabel: the ONE home for the [End match] verdict wording.
// deriveKachinukiEndOutcome owns the OUTCOME; this owns its DISPLAY STRING so
// the two End-match surfaces (the reopen verdict-preview and the End/Confirm
// button) can never drift. The win wording defers to teamResultLabel, the
// canonical "Aka wins"/"Shiro wins" source; a tied end reads "Draw (hikiwake)".
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
// winner. Nor by pairing: any tied pair may fight on, the operator decides
// and the app records it (operator ruling 2026-09-26).
export function kachinukiEnchoAvailable(outcome) {
  if (!outcome) return false;
  return outcome.kind === "draw" || (outcome.kind === "blocked" && outcome.reason === "knockout-tie");
}

// kachinukiBandModel: content model for the summary band in KACHINUKI mode
// (light instrument panel, user-confirmed brief 2026-08-02). The band shows
// BOUT-LOG FACTS ONLY — never roster inferences (the roster is advisory by
// operator ruling) and NEVER a verdict while the match is running (the old
// IV/PW-derived "Aka wins" contradicted the End gate mid-match, critique P1).
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
  const matchWinnerName = nameOf(matchWinner);
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
    // realIppons, not raw .length: an unfilled "•" placeholder or an empty cell
    // is not a point. Raw length here made the band announce "Aka beat Shiro"
    // under a bout row the scoreboard was rendering as a tie.
    const aN = realIppons(s.aPts).length;
    const bN = realIppons(s.bPts).length;
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
    // "Final" names the last match of a knockout and nothing else (operator
    // ruling 2026-09-20, bc-sccl), so the finished band says Finished.
    const headline = `Finished, ${n} bout${n === 1 ? "" : "s"}`;
    // Winner wording defers to teamResultLabel (the one "Aka wins"/"Shiro wins"
    // home). The raw-name fallback keeps an unattributable winner from being
    // silently dropped. winSide reuses winnerSideLR (bracket.jsx) rather than
    // re-deriving name-equality here: it prefers id equality over name, which
    // bare-name comparison cannot (review: two teams CAN share a display name).
    const lr = winnerSideLR({ winner: matchWinner, sideA, sideB });
    const winSide = lr === "right" ? "a" : lr === "left" ? "b" : null;
    if (winSide) return { headline, verdict: teamResultLabel({ teamWinner: winSide }), verdictSide: winSide === "a" ? "aka" : "shiro" };
    if (matchDecision === "hikiwake" || !matchWinner) return { headline, verdict: "Draw", verdictSide: "draw" };
    return { headline, verdict: String(matchWinnerName).toUpperCase(), verdictSide: "draw" };
  }

  const headline = `Bout ${currentBout || played.length + 1}`;
  const last = played[played.length - 1];
  if (!last) return { headline, fact: "" };
  if (last.tie) return { headline, fact: "Last: hikiwake, both retired" };
  // Streak: consecutive trailing wins by the same fighter — a pure bout-log
  // fact ("stays on" is winner-stays semantics, not a roster claim).
  let streak = 0;
  for (let i = played.length - 1; i >= 0; i--) {
    if (played[i].winner && played[i].winner === last.winner) streak++;
    else break;
  }
  const how = last.fusensho ? " (fusensho)" : "";
  const stay = streak >= 2 ? `, stays on (${streak} wins)` : ", stays on";
  // "Last:" prefix mirrors the tie-fact ("Last: hikiwake …") so the headline
  // "Bout N" is never misread as bout N's own (unfought) result (critique P2).
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
export function kachinukiVisiblePositions({ positions, daihyosenIdx, subResults, isComplete, isPlayedAt, closeRecordedLast = false }) {
  const subs = subResults || [];
  let slots = positions.filter((_, i) => i !== daihyosenIdx && subs.some(sr => sr.position === i + 1));
  if (slots.length === 0 && positions.length > 0 && daihyosenIdx !== 0) {
    slots = [positions[0]]; // bootstrap: bout 1 on a fresh match
  }
  const daihyosenSlot = daihyosenIdx >= 0 ? [positions[daihyosenIdx]] : [];
  if (isComplete) return [...slots, ...daihyosenSlot];
  let cur = -1;
  for (let i = 0; i < slots.length; i++) {
    if (!isPlayedAt(positions.indexOf(slots[i]))) { cur = i; break; }
  }
  if (cur < 0) {
    // Every bout is played. bc-kbrc: when Record bout has recorded the last
    // one as a WIN (the server's stored mark) and appended no pairing, there
    // is no live bout: the recorded one renders read-only with the others. A
    // recorded DRAW never closes it (kachinukiRecordedBoutCloses: Encho on that
    // bout is the operator's call in any phase). A bout added by hand is a
    // placeholder with no mark, so it is the live bout again.
    const last = lastNumberedBout(subs);
    if (closeRecordedLast && last && slots.includes(positions[last.position - 1])
      && kachinukiRecordedBoutCloses(subs)) {
      return daihyosenSlot;
    }
    cur = slots.length - 1;
  }
  return [...slots.slice(cur, cur + 1), ...daihyosenSlot];
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
