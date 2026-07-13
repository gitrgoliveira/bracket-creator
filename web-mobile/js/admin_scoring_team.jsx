// Team-match helpers and TeamScoreEditorModal.
// Private to the scoring module; ScoreEditorModal routes here for team matches.
// Extracted from admin_scoring_modal.jsx (mp-zac3).

const { useState: useStateA, useEffect: useEffectA, useRef: useRefA } = React;

import {
  MAX_IPPONS_PER_SIDE,
  isBoutDecided,
  getIpponButtons,
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
} from './admin_scoring_shared.jsx';

import { useDebouncedRunningWrite, SyncStatusPill } from './admin_scoring_autosave.jsx';

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
export function teamResultLabel({ teamWinner, isKnockoutPhase, hasAnyScore }) {
  if (teamWinner === "a") return "AKA WIN";
  if (teamWinner === "b") return "SHIRO WIN";
  if (isKnockoutPhase) return hasAnyScore ? "DAIHYOSEN" : "-";
  return "DRAW";
}

// isKoTieBlocked: Finish must be blocked while a knockout encounter has no
// winner: the operator has to add and score a daihyosen first. Pool draws stay
// finishable, and an already-completed match (correction flow) is never blocked.
export function isKoTieBlocked({ isKnockoutPhase, teamWinner, isComplete }) {
  return !!isKnockoutPhase && teamWinner === null && !isComplete;
}

// isKachinukiBoutMode: while a kachinuki encounter is being fought (not a
// correction, not exhausted, no daihyosen row) the modal's primary action
// records the CURRENT BOUT: a running write flagged kachinukiBoutFinal that
// the server uses to append the next pairing. The match itself completes
// server-side when one team is exhausted. Finish/complete semantics only
// apply for corrections (isComplete) and for the tied-after-exhaustion
// daihyosen resolution (exhausted / hasDaihyosen), where the knockout
// no-draw rule (isKoTieBlocked) still holds. A bout-level hikiwake is a
// legitimate bout result and must never be blocked by that rule.
export function isKachinukiBoutMode({ isKachinuki, isComplete, exhausted, hasDaihyosen }) {
  return !!isKachinuki && !isComplete && !exhausted && !hasDaihyosen;
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

// subBoutHasBeenPlayed: true once a sub-bout carries any operator input
// (ippons, fouls, a per-bout fusensho, or an explicit hikiwake). Used to drop
// untouched positions from a KACHINUKI patch: the modal maps over all team
// positions, but kachinuki appends bouts dynamically, so emitting unplayed
// positions as 0–0 hikiwake would corrupt advancement (AdvanceKachinuki keys
// off the LAST SubResult having an outcome) and inflate individual-draw
// standings. Fixed-position matches keep all positions: a 0–0 there is a
// legitimate hikiwake.
export function subBoutHasBeenPlayed(s) {
  if (!s) return false;
  return (s.aPts?.length > 0) || (s.bPts?.length > 0) || (s.aFouls > 0) || (s.bFouls > 0) || !!s.fusensho || !!s.draw;
}

export function TeamScoreEditorModal({ match, teamSize, onClose, onSubmit, onSubmitAndNext, onAfterDecision, prevMatch, nextMatch, onPrev, onNext, password, selfReport, variant = "modal", canClose = true }) {
  const m = match;
  const isComplete = m.status === "completed";
  // Kachinuki appends bouts beyond teamSize (engine assigns Position =
  // len(SubResults)+1, up to 2*roster-1 bouts), so size the grid to cover every
  // persisted bout position: otherwise reopening a kachinuki match hides (and
  // can't score) the later bouts. A fixed-position match never persists a
  // position past teamSize, so it is unaffected. Capped at the theoretical
  // kachinuki maximum (2*MAX_TEAM_SIZE-1) as a guard against malformed data.
  const maxSubPos = (m.subResults || []).reduce((mx, s) => (s.position > 0 && s.position > mx ? s.position : mx), 0);
  const positionCount = Math.min(Math.max(teamSize, maxSubPos), 2 * window.MAX_TEAM_SIZE - 1);
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
  // Audit reason collected when correcting a completed team match: mirrors
  // the ScoreEditorModal correction flow (same ReasonPrompt + CORRECTION_PRESETS).
  const [correctionReason, setCorrectionReason] = useStateA("");
  const [showCorrectionPrompt, setShowCorrectionPrompt] = useStateA(false);
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
  const [daihyosenErr, setDaihyosenErr] = useStateA("");
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
    setDaihyosenErr("");
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
      setDaihyosenErr(userMsg);
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
  const maxEnchoPeriods = compMeta?.config?.maxEnchoPeriods || 0;
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
        setLineupA(prev => ({ ...(prev || {}), positions: updated }));
      } else {
        setLineupB(prev => ({ ...(prev || {}), positions: updated }));
      }
    } catch (e) {
      // Surface error briefly: can't use a toast from inside the modal so
      // we reuse the daihyosenErr channel for a one-off message.
      if (mountedRef.current) setDaihyosenErr(e?.message || "Failed to update lineup");
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
  const initSubsRef = React.useRef(null);
  if (initSubsRef.current === null) {
    initSubsRef.current = positions.map((_, idx) => {
      const pos = idx === daihyosenIdx ? DAIHYOSEN_POSITION : idx + 1;
      const existing = existingSub.find(s => s.position === pos);
      let fusensho = "";
      if (existing?.decision === "fusensho") {
        if (existing.winner === sideAName) fusensho = "a";
        else if (existing.winner === sideBName) fusensho = "b";
      }
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
      };
    });
  }
  const [subs, setSubs] = useStateA(initSubsRef.current);
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
  // hikiwake for IV/PW and serialises with decision="hikiwake".
  const subTotals = subs.map(s => {
    const aT = s.aPts.length;
    const bT = s.bPts.length;
    const winner = s.draw ? null : aT > bT ? "a" : bT > aT ? "b" : null;
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
  const teamVerdictText = teamResultLabel({ teamWinner, isKnockoutPhase, hasAnyScore: teamHasAnyScore });
  // Block Finish while a KO encounter has no winner: the operator must add and
  // score a daihyosen first (the affordance below). Pool draws stay finishable.
  const koTieBlocked = isKoTieBlocked({ isKnockoutPhase, teamWinner, isComplete });
  // T136: "kachinuki-exhaustion" sentinel from the backend: one team has
  // no players left, the encounter is decided. Shared by the banner and
  // the footer action choice.
  const kachinukiExhausted = isKachinuki && (m.decision === "kachinuki-exhaustion" || (m.subResults || []).some(s => s.decision === "kachinuki-exhaustion"));
  // While a kachinuki match is being fought, the primary action records
  // the current BOUT (running write + kachinukiBoutFinal flag), never a
  // match completion; the server appends the next bout or ends the match
  // by exhaustion. koTieBlocked does NOT apply to a bout submit (a bout
  // hikiwake is legitimate; the match is not being completed).
  const kachinukiBoutMode = isKachinukiBoutMode({ isKachinuki, isComplete, exhausted: kachinukiExhausted, hasDaihyosen });
  // Rows to render: kachinuki shows only bouts that exist in the server log
  // (kachinukiVisiblePositions handles the bout-1 bootstrap, the running
  // current-bout selection, the correction show-all branch, and the
  // always-visible daihyosen slot); fixed-format matches keep all positions.
  // Computed once here and shared by the Record-bout guard below and the
  // render body.
  const visiblePositions = isKachinuki
    ? kachinukiVisiblePositions({
        positions, daihyosenIdx, subResults: m.subResults, isComplete,
        isPlayedAt: (idx) => subBoutHasBeenPlayed(subs[idx]),
      })
    : positions;
  // UX guard: Record bout with nothing entered for the current bout would
  // submit a silent no-op (known quirk Q4: the glossary term inside the
  // Tie button swallows taps, and that chain used to end in a silent 200).
  // Disable the button until the CURRENT visible bout carries operator
  // input (points, fouls, fusensho, or an explicit draw).
  const kachinukiCurrentBoutPlayed = kachinukiBoutMode ? (() => {
    const cur = visiblePositions.find(p => p !== "daihyosen");
    return cur != null && subBoutHasBeenPlayed(subs[positions.indexOf(cur)]);
  })() : true;
  const finishSummary = `${teamVerdictText} · IV ${ivB}–${ivA} · PW ${pwB}–${pwA}`;
  useEffectA(() => { setFinishArmed(false); }, [ivA, ivB, pwA, pwB, teamWinner]);

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
    return {
      aName: resolveBoutSideName({ isKachinuki, isDaihyosen: isDaihyoRow, existingName: existing?.sideA, lineupName: pick(lineupA) }),
      bName: resolveBoutSideName({ isKachinuki, isDaihyosen: isDaihyoRow, existingName: existing?.sideB, lineupName: pick(lineupB) }),
    };
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
    const correctionBlock = isComplete && correctionReason ? { correctionReason } : {};
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

  // Esc-to-close, matching ScoreEditorModal. The full keyboard-shortcut
  // surface (M/K/D/T/H, ←/→, Enter) isn't wired here: team scoring is
  // many sub-matches and would need different bindings: but Esc is
  // table-stakes UX.
  const kbRef = React.useRef(null);
  kbRef.current = { submitting, handleDismiss, onPrev, onNext };
  useEffectA(() => {
    const onKeyDown = (ev) => {
      const s = kbRef.current;
      if (s.submitting) return;
      if (ev.ctrlKey || ev.metaKey || ev.altKey) return;
      if (ev.key === "Escape") { ev.preventDefault(); s.handleDismiss(); return; }
      if (window.isTextEntry(ev.target)) return;
      if (ev.key === "ArrowLeft" && s.onPrev) { ev.preventDefault(); s.onPrev(); return; }
      if (ev.key === "ArrowRight" && s.onNext) { ev.preventDefault(); s.onNext(); return; }
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
  const isFivePersonLineupIncomplete = (lineup) => {
    if (teamSize !== 5) return false;
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
          {/* FR-033 encho toggle: see ScoreEditorModal for the contract.
              EnchoControl collapses to a pill when no overtime is active. */}
          <EnchoControl
            enchoPeriodCount={enchoPeriodCount}
            setEnchoPeriodCount={setEnchoPeriodCount}
            maxEnchoPeriods={maxEnchoPeriods}
          />
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
            // T136: kachinukiExhausted (hoisted above) surfaces the end
            // banner instead of more bout rows when the backend has
            // already decided the match.
            isKachinuki && (
              <div key="kachinuki-banner" style={{ background: "var(--bg-2, #fafafa)", border: "1px solid var(--accent, #ddd)", borderRadius: 4, padding: "8px 12px", marginBottom: 12, fontSize: 12, display: "flex", flexDirection: "column", gap: 4 }}>
                <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
                  <span style={{ fontWeight: 700 }}><TermAS name="kachinuki">Kachinuki</TermAS> (winner-stays)</span>
                  <span style={{ color: "var(--ink-3)" }}>
                    {kachinukiExhausted
                      ? "One team exhausted: match ended."
                      : "Score the current bout, then tap Record bout. The next bout is added automatically; the match ends when one team runs out of players."}
                  </span>
                </div>
                {/* TODO(T136): inline auto-refresh after each score so
                    operators don't have to close+reopen the modal:
                    requires hooking the onSubmit response (current
                    flow forwards through parent + closes the modal). */}
              </div>
            ),
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
            const posAbbrev = isDaihyoRow ? "" : positionAbbrevFor(teamSize, idx, existingSubAtIdx);
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
                playerName: playerBName, roster: isDaihyoRow ? [] : rosterB, onSelectName: pickPlayer(teamIdB, lineupB),
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
                playerName: playerAName, roster: isDaihyoRow ? [] : rosterA, onSelectName: pickPlayer(teamIdA, lineupA),
              },
            ];

            // Sub-bout is decided once either side reaches 2 ippons.
            const subBoutDecided = isBoutDecided(s.aPts, s.bPts);

            const scoreDisplay = (() => {
              // mp-4pc: a hantei-decided daihyosen has a tied scoreline but
              // a declared winner: show the winner + (Ht) rather than X.
              if (isDaihyoRow && daihyosenTied && daihyosenHantei) {
                return <span>{`${t.bTotal}–${t.aTotal}`} <span style={{ fontSize: 11, opacity: 0.7 }}>(Ht)</span></span>;
              }
              // Draw: either an operator-marked tie, or equal non-zero scores
              // (the tie-marking rule). Canonical display: a hikiwake is an X on
              // the centre line (running_a_kendo_tournament.md), scored or not.
              const scored = t.aTotal > 0 || t.bTotal > 0;
              const isDraw = s.draw || (t.winner === null && scored);
              if (isDraw) return <span className="tsm-draw">X</span>;
              // Pending bout (0–0, not yet marked): a quiet placeholder.
              if (t.winner === null) return <span style={{ color: "var(--ink-3)" }}>–</span>;
              // Decided bout: the centred ippon letters already show who won: 
              // the numeric tally was redundant, so the centre stays clear.
              return null;
            })();

            return (
              <div key={idx} className="team-sub-match">
                <div className="team-sub-match__pos" title={posLabel}>
                  {/* Bout number AND the FIK position handle (Sen/Ji/Chu/Fuk/Tai
                      for 5-person teams): operators think in positions, so the
                      abbreviation rides in the row instead of hiding in the
                      title tooltip (unreachable on touch). The number stays as
                      the size-agnostic anchor; >5-person teams show it alone.
                      Daihyosen (the rep bout) shows "DH". */}
                  <span className="team-sub-match__pos-num">{isDaihyoRow ? "DH" : idx + 1}</span>
                  {!isDaihyoRow && posAbbrev && (
                    <span className="team-sub-match__pos-name">{posAbbrev}</span>
                  )}
                </div>
                <div className="team-sub-match__row">
                  {rowSides.map((rs, rsIdx) => (
                    <React.Fragment key={rs.key}>
                      <div className={`team-sub-match__side ${rsIdx === 1 ? "team-sub-match__side--right" : ""}`}>
                        {/* Name picker grouped with this side's score controls.
                            SHIRO chip + a typeable picker (filter the roster as
                            you type, or write a name) so operators can set the
                            order live; falls back to a static name when there's
                            no roster metadata. Lineups are always editable. */}
                        <div className="tsm-name">
                          <span className={`se-color-badge se-color-badge--${rs.color}`}>{rs.label}</span>
                          {rs.roster && rs.roster.length > 0 ? (
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
                            {/* Outstanding hansoku → red ▲ next to the name (the
                                outer edge), rendered before the slots. A 2nd foul
                                discharges to an H ippon for the opponent and clears
                                this. (running_a_kendo_tournament.md: ▲ next to name.) */}
                            {rowSides[0].fouls >= 1 && <span className="tsm-foul-tri" title="Hansoku: 1 foul">▲</span>}
                            {[0, 1].map(i => (
                              <button key={i} className={`editor-side__pt ${rowSides[0].pts[i] ? "editor-side__pt--filled" : ""}`}
                                onClick={() => rowSides[0].setPts(rowSides[0].pts.filter((_, j) => j !== i))} title="Click to remove">
                                {rowSides[0].pts[i] || "·"}
                              </button>
                            ))}
                          </div>
                          <div className={`team-sub-match__score ${scoreDisplay && t.winner === "b" ? "team-sub-match__score--a-win" : scoreDisplay && t.winner === "a" ? "team-sub-match__score--b-win" : ""}`}>
                            {scoreDisplay}
                          </div>
                          <div className="tsm-center-pts tsm-center-pts--aka">
                            {/* Aka fills outside-in: its first ippon sits on the
                                outer (right) edge nearest the Aka name, so render
                                the slots in reverse (pts[1] then pts[0]). */}
                            {[1, 0].map(i => (
                              <button key={i} className={`editor-side__pt ${rowSides[1].pts[i] ? "editor-side__pt--filled" : ""}`}
                                onClick={() => rowSides[1].setPts(rowSides[1].pts.filter((_, j) => j !== i))} title="Click to remove">
                                {rowSides[1].pts[i] || "·"}
                              </button>
                            ))}
                            {/* Outstanding hansoku → red ▲ next to the Aka name
                                (the outer/right edge), after the reversed slots. */}
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

          {/* Team summary: T138: sticky to the top of the modal body so
              the IV/PW totals stay visible as the operator scrolls through
              many bout rows (especially relevant on small screens / when
              every sub-match has been scored). zIndex: 5 keeps it under
              the modal head (10) but above the bout cells. */}
          <div className="team-summary" style={{ position: "sticky", top: 0, zIndex: 5 }}>
            {teamSides.map((ts, idx) => (
              <React.Fragment key={ts.key}>
                <div className="team-summary__side">
                  <div className="team-summary__label">{ts.label}</div>
                  <div className="team-summary__stats">IV: {ts.iv} · PW: {ts.pw}</div>
                </div>
                {idx === 0 && (
                  <div className="team-summary__side team-summary__side--center">
                    <div className="team-summary__label">RESULT</div>
                    <div className="team-summary__verdict">{teamVerdictText}</div>
                  </div>
                )}
              </React.Fragment>
            ))}
          </div>

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
              <div className="hantei-row" data-testid="team-daihyosen-hantei-row" style={{ display: "flex", gap: 8, alignItems: "center", padding: "6px 8px", marginTop: 12, background: "var(--card-2, #fafafa)", borderRadius: 6, fontSize: 12 }}>
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
            if (hasDaihyosen || !isKnockoutPhase) return null;
            // Local tie detection drives the highlight + helper copy only: 
            // the backend is the source of truth and re-validates on submit.
            // A bout is "decided" once it carries any ippon or is a draw; a
            // 5-person tie reaches even IV only via at least one drawn bout,
            // so draws MUST count here (the bug the old gate had).
            const anyBoutDecided = subTotals.some(t => t.aTotal > 0 || t.bTotal > 0 || t.draw || t.winner !== null);
            const teamTied = anyBoutDecided && ivA === ivB && pwA === pwB;
            const onDaihyosen = async () => {
              setDaihyosenErr("");
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
                setDaihyosenErr(userMsg);
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
                {daihyosenErr && (
                  <div className="daihyosen-controls__err">{daihyosenErr}</div>
                )}
              </div>
            );
          })()}

          {/* Ippon-type letter legend: same affordance as the individual
              editor; the per-bout buttons use the same M/K/D/T/H letters. */}
          <IpponLegend isNaginata={isNaginataTeam} />

          {/* T093–T098: decision (kiken/fusenpai) controls for the overall
              team match. Per-bout Fusensho lives on each sub-match row
              (see the row-level "Fusensho" button per side, T096). */}
          {!withdrawnPlayer && !decisionPromptKind && !selfReport && (
            <div className="decision-controls" style={{ display: "flex", gap: 8, marginTop: 12, fontSize: 12, alignItems: "center" }}>
              <span style={{ color: "var(--ink-3)", fontWeight: 600 }}>Team decision:</span>
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
              <span style={{ color: "var(--ink-3)", fontSize: 11, marginLeft: 4 }}>
                (<TermAS name="fusensho">Fusensho</TermAS> is per-bout: use the "Fusensho" button on each row above.)
              </span>
            </div>
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

        </div>

        <div className="editor-modal__foot editor-modal__foot--nav">
          {/* Audit reason prompt for team-match corrections: same contract
              as ScoreEditorModal: operator must confirm before the patch fires. */}
          {isComplete && showCorrectionPrompt && (
            <ReasonPrompt
              label="Reason for correction"
              presets={CORRECTION_PRESETS}
              submitting={submitting}
              onConfirm={(r) => {
                setCorrectionReason(r);
                setShowCorrectionPrompt(false);
                const patch = { ...buildPatch("completed"), correctionReason: r };
                doSubmit(() => onSubmit(patch));
              }}
              onCancel={() => setShowCorrectionPrompt(false)}
            />
          )}
          {/* While the correction prompt is open it owns the only Cancel/commit
              row: hide the footer's own nav+actions so the operator never sees
              two Cancels and two commit buttons at the highest-stakes moment
              (amending a recorded result). Mirrored in Score/EngiScoreEditorModal. */}
          {!(isComplete && showCorrectionPrompt) && (
          <div className="score-nav">
            {prevMatch ? (
              <button className="btn btn--sm score-nav__prev" onClick={onPrev} disabled={submitting}>← Prev</button>
            ) : <span />}
            <div className="score-nav__actions">
              {m.status === "scheduled" && (
                <button className="btn btn--sm" onClick={() => doSubmit(() => onSubmit(buildPatch("running")))} disabled={submitting}>▶ Start match</button>
              )}
              {canClose && <button className="btn" onClick={handleDismiss} disabled={submitting}>Cancel</button>}
              {kachinukiBoutMode ? (
                // Kachinuki bout submit: a RUNNING write flagged
                // kachinukiBoutFinal, not a match completion. The server
                // appends the next bout, or ends the match by exhaustion.
                // koTieBlocked deliberately does not apply here: a bout
                // hikiwake is a legitimate result (both players retire).
                // Same two-step arm/confirm pattern as Finish; any score
                // edit disarms via the finishArmed effect above.
                <button type="button" className={`btn btn--primary ${finishArmed ? "btn--confirm" : ""}`} onClick={() => {
                  if (!finishArmed) { setFinishArmed(true); return; }
                  doSubmit(() => onSubmit(buildPatch("running", { kachinukiBoutFinal: true })));
                }} disabled={submitting || !kachinukiCurrentBoutPlayed}
                  title={!kachinukiCurrentBoutPlayed ? "Nothing recorded for this bout yet" : undefined}>
                  {submitting ? "Saving…" : !kachinukiCurrentBoutPlayed ? "Nothing recorded yet" : finishArmed ? "Confirm · Record bout" : "Record bout"}
                </button>
              ) : onSubmitAndNext ? (
                <button className={`btn btn--primary ${finishArmed && !isComplete ? "btn--confirm" : ""}`} onClick={() => {
                  if (isComplete && !correctionReason) { setShowCorrectionPrompt(true); return; }
                  if (!isComplete && !finishArmed) { setFinishArmed(true); return; }
                  doSubmit(() => (isComplete ? onSubmit : onSubmitAndNext)(buildPatch("completed")));
                }} disabled={submitting || koTieBlocked}
                  title={koTieBlocked ? "A knockout match can't be a draw: add and score a daihyosen to decide a winner" : undefined}>
                  {submitting ? "Saving…" : isComplete ? "Save correction" : koTieBlocked ? "Needs a winner" : finishArmed ? `Confirm · ${finishSummary} →` : "Finish + Start Next →"}
                </button>
              ) : (
                <button className={`btn btn--primary ${finishArmed && !isComplete ? "btn--confirm" : ""}`} onClick={() => {
                  if (isComplete && !correctionReason) { setShowCorrectionPrompt(true); return; }
                  if (!isComplete && !finishArmed) { setFinishArmed(true); return; }
                  doSubmit(() => onSubmit(buildPatch("completed")));
                }} disabled={submitting || koTieBlocked}
                  title={koTieBlocked ? "A knockout match can't be a draw: add and score a daihyosen to decide a winner" : undefined}>
                  {submitting ? "Saving…" : isComplete ? "Save correction" : koTieBlocked ? "Needs a winner" : finishArmed ? `Confirm · ${finishSummary}` : "Finish"}
                </button>
              )}
            </div>
            {nextMatch ? (
              <button className="btn btn--sm score-nav__next" onClick={onNext} disabled={submitting}>Next →</button>
            ) : <span />}
          </div>
          )}
          {/* Quiet, always-present keyboard-shortcut reminder. */}
          <ScoringShortcutHint />
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
