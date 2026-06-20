// Score-entry modals used by the schedule and competition pages. See
// web-mobile/admin_split_plan.md.

const { useState: useStateA, useEffect: useEffectA, useRef: useRefA } = React;

// Foundation (pure helpers + small presentational components) lives in
// admin_scoring_shared.jsx; imported here and re-exported below so existing
// `import { … } from './admin_scoring_modal.jsx'` call sites keep working.
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
  buildDecisionBody,
  submitDecisionRequest,
  makeSubmitDecision,
  shouldShowEnchoMaxBanner,
  canIncrementEncho,
  nextEnchoPeriod,
  prevEnchoPeriod,
  initialEnchoPeriodsForMatch,
  daihyosenEnchoFields,
  decideDrawToggle,
  shouldBlockScoringKeys,
  EnchoControl,
  DecisionPrompt,
  RemainingMatchesPanel,
  FoulCounter,
  LineupNameInput,
  ReasonPrompt,
  CORRECTION_PRESETS,
  LINEUP_PRESETS,
} from './admin_scoring_shared.jsx';

// useDebouncedRunningWrite — shared autosave hook used by BOTH ScoreEditorModal
// and TeamScoreEditorModal. On every user-driven scoring mutation the caller
// invokes markDirty(); the hook schedules a single trailing-edge write
// (debounced ~300ms) with a "running" patch so viewers receive per-point
// progress without waiting for the explicit Finish button.
//
// Gates (ALL must pass for any network call to fire):
//   1. SCHEDULE-time: match.status === "running" — never auto-start a
//      scheduled match. markDirty() is only ever called from user-driven
//      scoring handlers (addPt, fouls, draw, encho…), never from prop/SSE-
//      driven re-renders, so there is no SSE→write→SSE feedback loop.
//   2. FIRE-time: the component is still mounted.
//   3. FIRE-time: match.status is STILL "running". If the match completed
//      during the ~300ms debounce window (an SSE update or another operator),
//      the queued running write is suppressed so it can't revert the result.
//
// cancelDebounce() must be called before any explicit Start / Finish submit
// so a queued running-write can't land after (or over) the final patch.
//
// The hook does NOT call doSubmit (which sets submitting=true and would
// disable the UI). Background writes are fire-and-forget with their own
// error swallowing — operators must not see save-indicator churn on every
// single ippon tap.
//
// Design note on buildPatchRef: buildPatch reads aPts/bPts/fouls etc. from
// the enclosing component's closure. Because the timeout fires asynchronously
// we must capture the *latest* version of buildPatch rather than the one that
// existed when markDirty was called. The caller keeps buildPatchRef.current
// updated on every render (see the comment near useDebouncedRunningWrite
// call-sites below).
const AUTOSAVE_DEBOUNCE_MS = 300;

// ---------------------------------------------------------------------------
// C2: SyncStatusPill
// ---------------------------------------------------------------------------
// Small indicator rendered in the scoring-panel header while a match is
// running. Subscribes to the write-queue sync status from api_client.jsx
// (via window.subscribeSyncStatus) and reflects:
//   synced     — last write landed; no queue pending
//   syncing    — write in flight / in queue
//   offline    — network down; queue retrying with backoff
//
// COPY RULE: NEVER use the word "live" in user-facing strings.
// Colors use design tokens only (var(--...)) — no hardcoded hex.
// ---------------------------------------------------------------------------

// Module-level const — hoisted so the object is not rebuilt on every render.
const SYNC_PILL_CONFIG = {
  synced:  { label: 'Synced',   cls: 'sync-pill--synced',  dot: '●' },
  syncing: { label: 'Syncing…', cls: 'sync-pill--syncing', dot: '◌' },
  offline: { label: 'Offline',  cls: 'sync-pill--offline', dot: '●' },
};

function SyncStatusPill({ isRunning }) {
  // The component always mounts and subscribes to sync status (the subscription
  // is a single Set entry and replays the current value on subscribe). It only
  // renders a VISIBLE pill while the match is running — autosave fires only on
  // running matches, so the pill carries no meaning otherwise. The render guard
  // is the `if (!isRunning) return null` below.
  const [status, setStatus] = useStateA('synced');
  useEffectA(() => {
    // window.subscribeSyncStatus is set by api_client.jsx when loaded.
    const subscribe = typeof window !== 'undefined' && window.subscribeSyncStatus;
    if (!subscribe) return;
    const unsub = subscribe((s) => setStatus(s));
    return () => unsub();
  }, []);

  if (!isRunning) return null; // render guard: no visible pill unless running

  const c = SYNC_PILL_CONFIG[status] || SYNC_PILL_CONFIG.synced;
  return (
    <span className={`sync-status-pill ${c.cls}`} data-testid="sync-status-pill" aria-label={`Score sync: ${c.label}`}>
      <span className="sync-pill__dot" aria-hidden="true">{c.dot}</span>
      <span className="sync-pill__label">{c.label}</span>
    </span>
  );
}

function useDebouncedRunningWrite({ isRunningRef, buildPatchRef, onSubmitRef, mountedRef }) {
  const timerRef = useRefA(null);

  // cancelDebounce — call this before any explicit submit (Start / Finish /
  // Hantei / Decision) so the queued timer can't fire afterward.
  const cancelDebounce = () => {
    if (timerRef.current !== null) {
      clearTimeout(timerRef.current);
      timerRef.current = null;
    }
  };

  // Clear on unmount so the closure can't fire after the component is gone.
  useEffectA(() => () => { cancelDebounce(); }, []);

  // markDirty — call from every user-driven mutation handler (addPt,
  // removePt, foul increment/decrement, draw toggle, encho change, team
  // sub-bout edits). Do NOT call from prop/SSE-driven state writes.
  const markDirty = () => {
    if (!isRunningRef.current) return; // gate 1: never auto-start a scheduled match
    cancelDebounce();
    timerRef.current = setTimeout(() => {
      timerRef.current = null;
      if (!mountedRef.current) return;
      // gate 3: re-check running at FIRE time. If the match was completed
      // during the debounce window (this operator's Finish cancels the timer,
      // but an SSE update or another operator can complete it out from under
      // us), isRunningRef has flipped false on re-render — sending a
      // status:"running" autosave now would regress the completed result.
      if (!isRunningRef.current) return;
      // Fire-and-forget: errors swallowed; operator's explicit Finish is
      // the authoritative write.
      try {
        const p = onSubmitRef.current(buildPatchRef.current("running"));
        if (p && typeof p.catch === "function") p.catch(() => {});
      } catch (_) { /* swallow */ }
    }, AUTOSAVE_DEBOUNCE_MS);
  };

  return { markDirty, cancelDebounce };
}

function ScoreEditorModal({ match, onClose, onSubmit, onSubmitAndNext, prevMatch, nextMatch, onPrev, onNext, password, selfReport, variant = "modal", canClose = true }) {
  const m = match;
  const isComplete = m.status === "completed";
  // Canonical team check (matches admin_pools.jsx and the lineup panel):
  // compKind OR a positive teamSize. A team competition created with only
  // teamSize set (compKind empty) must still route to TeamScoreEditorModal,
  // and pool-daihyosen rows (compMatches forces compKind="" AND teamSize=0)
  // correctly stay on the individual editor.
  const isTeam = m.compKind === "team" || (m.teamSize || 0) > 0;
  const teamSize = m.teamSize || 5;
  if (isTeam) return <TeamScoreEditorModal match={m} teamSize={teamSize} onClose={onClose} onSubmit={onSubmit} onSubmitAndNext={onSubmitAndNext} prevMatch={prevMatch} nextMatch={nextMatch} onPrev={onPrev} onNext={onNext} password={password} selfReport={selfReport} variant={variant} canClose={canClose} />;

  const seedAPts = m.ipponsA?.filter(x => x && x !== "•") || (m.score?.type === "ippon" && m.winner?.id === m.sideA?.id ? m.score.ippons || [] : []);
  const seedBPts = m.ipponsB?.filter(x => x && x !== "•") || (m.score?.type === "ippon" && m.winner?.id === m.sideB?.id ? m.score.ippons || [] : []);

  // Use ?? not || so an explicit 0 isn't treated as "unset".
  // reconcileFoulsAtOpen turns the pre-fix cumulative raw count into the
  // post-fix "outstanding fouls" semantics AND tops up the opponent's pts
  // with any missing discharged H ippons (legacy/imported data that has
  // hansokuA >= 2 without matching H's in ipponsB would otherwise silently
  // lose points on resubmit). A's fouls discharge into B's pts; B's into A's.
  const rawAFouls = m.hansokuA ?? m.score?.fouls?.a ?? 0;
  const rawBFouls = m.hansokuB ?? m.score?.fouls?.b ?? 0;
  const reconA = reconcileFoulsAtOpen(rawAFouls, seedBPts);
  const reconB = reconcileFoulsAtOpen(rawBFouls, seedAPts);
  const initialAPts = reconB.opponentPts;
  const initialBPts = reconA.opponentPts;
  const initialAFouls = reconA.outstandingFouls;
  const initialBFouls = reconB.outstandingFouls;
  // FR-033: encho (overtime) counter rides alongside the score. Initialized
  // from the existing match.encho?.periodCount so re-opens of completed
  // matches retain the toggle. Slice 1 ships the operator-visible toggle and
  // round-trips the count via toBackendMatchResult; Slice 3 (T093+) layers
  // the decision/kiken UI on top.
  const initialEnchoPeriods = m.encho?.periodCount || 0;
  const initialDecidedByHantei = !!m.decidedByHantei;
  const [aPts, setAPts] = useStateA(initialAPts);
  const [bPts, setBPts] = useStateA(initialBPts);
  const [aFouls, setAFouls] = useStateA(initialAFouls);
  const [bFouls, setBFouls] = useStateA(initialBFouls);
  const [enchoPeriodCount, setEnchoPeriodCount] = useStateA(initialEnchoPeriods);
  // FIK Art. 7-5 / 29-6: an encho match that remains tied is decided by
  // referee hantei. Persisting this on MatchResult so the UI / Excel can
  // mark it distinctly (vs an ippon-derived win).
  const [decidedByHantei, setDecidedByHantei] = useStateA(initialDecidedByHantei);
  const [submitting, setSubmitting] = useStateA(false);
  // T104/CHK029: MaxEnchoPeriods cap from the competition config.
  // Fetched once on open so the warning banner can fire before the
  // operator submits (the server validates the same cap on PUT /score).
  const [maxEnchoPeriods, setMaxEnchoPeriods] = useStateA(0);
  // Naginata competitions add an extra "S" (Sune) ippon button.
  // Fetched from the competition config alongside maxEnchoPeriods.
  const [isNaginata, setIsNaginata] = useStateA(false);
  // T093–T098: decision (kiken/fusenpai) prompt state. promptKind is
  // "" | "kiken-voluntary" | "kiken-injury" | "fusenpai"; when non-empty the inline prompt replaces the
  // bottom controls. After the POST /decision succeeds, withdrawnPlayer holds
  // the side that lost so the "Remaining matches" panel can render below.
  const [decisionPromptKind, setDecisionPromptKind] = useStateA("");
  const [decisionSubmitting, setDecisionSubmitting] = useStateA(false);
  const [decisionErr, setDecisionErr] = useStateA("");
  const [withdrawnPlayer, setWithdrawnPlayer] = useStateA(null);
  // Audit reason collected when correcting a completed match. showCorrectionPrompt
  // gates the ReasonPrompt overlay; correctionReason carries the confirmed string.
  const [correctionReason, setCorrectionReason] = useStateA("");
  const [showCorrectionPrompt, setShowCorrectionPrompt] = useStateA(false);
  // doSubmit's setSubmitting(false) in finally fires post-await; if the
  // parent unmounts the modal during the in-flight save (e.g.
  // AdminScoreEditor unmounts), gate the setState. handleDismiss
  // already no-ops UI dismissal while submitting=true, so this covers
  // only external/parent-driven unmount.
  const mountedRef = useRefA(true);
  useEffectA(() => () => { mountedRef.current = false; }, []);

  // C1: refs updated each render so the debounce callback always sees the
  // latest buildPatch / onSubmit / running-status even though the hook is
  // called early (before buildPatch is defined in this component body).
  const _autosaveIsRunningRef = useRefA(false);
  const _autosaveBuildPatchRef = useRefA(null);
  const _autosaveOnSubmitRef = useRefA(null);
  const { markDirty: markScoringDirty, cancelDebounce: cancelScoringDebounce } = useDebouncedRunningWrite({
    isRunningRef: _autosaveIsRunningRef,
    buildPatchRef: _autosaveBuildPatchRef,
    onSubmitRef: _autosaveOnSubmitRef,
    mountedRef,
  });

  useEffectA(() => {
    if (!m.compId) return;
    let cancelled = false;
    window.API.fetchCompetitionDetails(m.compId).then(d => {
      if (!cancelled) {
        setMaxEnchoPeriods(d?.config?.maxEnchoPeriods || 0);
        setIsNaginata(!!d?.config?.naginata);
      }
    }).catch(() => {});
    return () => { cancelled = true; };
  }, [m.compId]);

  // T093/T094: shared decision-submit path for kiken & fusenpai.
  // - decisionBy is "shiro" or "aka" per the server contract.
  // - encho rides along when the operator has marked overtime so the server
  //   can attach the periodCount metadata to the resulting MatchResult.
  // - On success we close the modal (matching the Save button contract) UNLESS
  //   the decision is kiken — in that case we keep the modal open and surface
  //   the RemainingMatchesPanel so the operator can chain default-win awards.
  // - T103/CHK024: when the server replies 409 decision_locked (the
  //   prior kiken on this match can't be safely overwritten because a
  //   subsequent match for either side has started), prompt the
  //   operator to confirm and re-send with force=true.
  // Shared factory (admin_scoring_shared.jsx) — the individual + team modals had
  // byte-identical copies; "competitors" is the only per-modal wording.
  const submitDecision = makeSubmitDecision({
    match: m, enchoPeriodCount, password, mountedRef,
    setDecisionSubmitting, setDecisionErr, setWithdrawnPlayer, setDecisionPromptKind,
    onClose, entityLabel: "competitors",
  });

  // Hansoku Hs are now physically present in the opponent's pts array
  // (folded in at the 2-foul boundary by applyFoulIncrement). The counter
  // is "outstanding fouls" — no derived addends needed.
  const aTotal = aPts.filter((x) => x !== "•").length;
  const bTotal = bPts.filter((x) => x !== "•").length;

  const addPt = (side, letter) => {
    // No-op when the side is already at the 2-ippon max: don't mark dirty or
    // schedule an autosave PUT / SSE fan-out for a tap that changes nothing.
    const cur = side === "a" ? aPts : bPts;
    if (cur.length >= 2) return; // fast no-op path: don't mark dirty / autosave
    // The functional updater re-checks the cap against the AUTHORITATIVE current
    // state (p), not the render-closure cur — so the 2-ippon invariant holds even
    // if addPt is called twice before a re-render (React batching / rapid taps).
    if (side === "a") setAPts((p) => p.length < 2 ? [...p, letter] : p);
    else setBPts((p) => p.length < 2 ? [...p, letter] : p);
    markScoringDirty(); // C1: trigger debounced autosave
  };
  const removePt = (side, idx) => {
    if (side === "a") setAPts((p) => p.filter((_, i) => i !== idx));
    else setBPts((p) => p.filter((_, i) => i !== idx));
    markScoringDirty(); // C1
  };

  // FR-033: when the operator has marked overtime, attach the encho block
  // to non-"reset" patches. periodCount=0 means "no overtime"; emitting the
  // field as undefined keeps the wire payload clean (omitempty server-side).
  const enchoBlock = () => enchoPeriodCount > 0 ? { encho: { periodCount: enchoPeriodCount } } : {};
  // decidedByHantei is only set via the dedicated submitHantei path
  // (SHIRO/AKA hantei buttons). The regular Finish/Enter buildPatch
  // explicitly clears the flag (sends false) when the match was previously
  // hantei-decided, so a re-edit via the normal flow removes the stale Ht
  // marker rather than preserving it on the server.
  const hanteiClear = initialDecidedByHantei ? { decidedByHantei: false } : {};

  const buildPatch = (targetStatus) => {
    const fouls = { a: aFouls, b: bFouls };
    if (targetStatus === "scheduled") return { winner: null, status: "scheduled", score: null, ipponsA: [], ipponsB: [], hansokuA: 0, hansokuB: 0, ...hanteiClear };
    if (targetStatus === "running") return {
      status: "running", winner: null,
      ipponsA: aPts.filter(x => x !== "•"), ipponsB: bPts.filter(x => x !== "•"),
      hansokuA: aFouls, hansokuB: bFouls,
      score: { type: "ippon", winnerPts: aTotal, loserPts: bTotal, ippons: aPts, fouls, live: true, corrected: isComplete },
      ...enchoBlock(), ...hanteiClear,
    };
    const correctionBlock = isComplete && correctionReason ? { correctionReason } : {};
    if (isDrawToggled) return { winner: null, ipponsA: [], ipponsB: [], hansokuA: aFouls, hansokuB: bFouls, status: "completed", score: { type: "hikiwake", winnerPts: 0, loserPts: 0, fouls, corrected: isComplete }, ...enchoBlock(), ...hanteiClear, ...correctionBlock };
    // ippon. Hansoku Hs are already physically present in the pts arrays
    // (folded in by applyFoulIncrement at the 2-foul boundary), so no
    // additional H fold is needed here.
    const aLetters = aPts.filter(x => x !== "•");
    const bLetters = bPts.filter(x => x !== "•");
    const aFinal = aLetters.slice(0, MAX_IPPONS_PER_SIDE);
    const bFinal = bLetters.slice(0, MAX_IPPONS_PER_SIDE);
    const winnerSide = aFinal.length > bFinal.length ? "a" : bFinal.length > aFinal.length ? "b" : null;
    if (!winnerSide) return { winner: null, ipponsA: aFinal, ipponsB: bFinal, hansokuA: aFouls, hansokuB: bFouls, status: "completed", score: { type: "hikiwake", winnerPts: 0, loserPts: 0, fouls, corrected: isComplete }, ...enchoBlock(), ...hanteiClear, ...correctionBlock };
    const winner = winnerSide === "a" ? m.sideA : m.sideB;
    const ippons = winnerSide === "a" ? aFinal : bFinal;
    return { winner, ipponsA: aFinal, ipponsB: bFinal, hansokuA: aFouls, hansokuB: bFouls, status: "completed", score: { type: "ippon", winnerPts: ippons.length, loserPts: (winnerSide === "a" ? bFinal : aFinal).length, ippons, fouls, corrected: isComplete }, ...enchoBlock(), ...hanteiClear, ...correctionBlock };
  };
  // C1: keep autosave refs fresh with the latest buildPatch / onSubmit /
  // running-status so the debounce callback never reads a stale closure.
  _autosaveIsRunningRef.current = m.status === "running";
  _autosaveBuildPatchRef.current = buildPatch;
  _autosaveOnSubmitRef.current = onSubmit;

  // Hantei submit: tied scoreline (with or without encho). Operator picks a
  // side; we send a completed patch with the chosen side as winner, the
  // *entered* ippon arrays preserved (so a 1–1 score stays visible alongside the Ht
  // marker — clearing them would lose the tied score history that the
  // viewer/Excel renderers display under the hantei suffix), and the
  // decidedByHantei flag set. This is a dedicated affordance because the
  // regular flow assumes an ippon-derived win.
  const submitHantei = (winnerSide) => {
    const winner = winnerSide === "a" ? m.sideA : m.sideB;
    const aFinal = aPts.filter(x => x !== "•").slice(0, MAX_IPPONS_PER_SIDE);
    const bFinal = bPts.filter(x => x !== "•").slice(0, MAX_IPPONS_PER_SIDE);
    return doSubmit(() => onSubmit({
      winner,
      ipponsA: aFinal,
      ipponsB: bFinal,
      hansokuA: aFouls,
      hansokuB: bFouls,
      status: "completed",
      ...enchoBlock(),
      decidedByHantei: true,
    }));
  };

  const doSubmit = async (fn) => {
    cancelScoringDebounce(); // C1: cancel any pending autosave before explicit submit
    setSubmitting(true);
    try { await fn(); } finally { if (mountedRef.current) setSubmitting(false); }
  };

  // Draw detection: check both the score.type (when present) and the
  // top-level decision string. Either being "hikiwake" means draw.
  const initialIsDrawToggled = window.isHikiwake(m.score?.type) || window.isHikiwake(m.decision);
  const [isDrawToggled, setIsDrawToggled] = useStateA(initialIsDrawToggled);

  // Arranged as [left, right] — left is always SHIRO (White), right is always AKA (Red).
  // onIncrement applies the FIK 2-foul auto-award rule via applyFoulIncrement:
  // every 2nd foul on this side discharges into a hansoku ippon ("H") for
  // the OPPONENT and resets this side's counter to 0.
  const sides = [
    {
      key: "b", name: m.sideB?.name, dojo: m.sideB?.dojo, pts: bPts, fouls: bFouls,
      setFouls: (v) => { setBFouls(v); markScoringDirty(); }, // C1
      onIncrement: () => {
        const r = applyFoulIncrement(bFouls, aPts, bPts);
        setBFouls(r.fouls);
        setAPts(r.opponentPts);
        markScoringDirty(); // C1
      },
      color: "shiro", label: "SHIRO (White)",
    },
    {
      key: "a", name: m.sideA?.name, dojo: m.sideA?.dojo, pts: aPts, fouls: aFouls,
      setFouls: (v) => { setAFouls(v); markScoringDirty(); }, // C1
      onIncrement: () => {
        const r = applyFoulIncrement(aFouls, bPts, aPts);
        setAFouls(r.fouls);
        setBPts(r.opponentPts);
        markScoringDirty(); // C1
      },
      color: "aka", label: "AKA (Red)",
    },
  ];

  // Bout is decided once either side reaches 2 ippons — disable add-ippon
  // buttons on BOTH sides (mirrors validateIpponCounts on the server).
  const boutDecided = isBoutDecided(aPts, bPts);

  // While hantei is armed the operator must commit via the dedicated SHIRO /
  // AKA buttons (which route through submitHantei). Disable the regular
  // Finish/Enter so the patch can't accidentally mark an ippon-decided match
  // as hantei-decided. Keyboard Enter is also gated on canFinish.
  // A knockout match can't end in a draw (it's decided by encho then hantei),
  // so the hikiwake toggle is suppressed in the bracket phase — m.phase ===
  // "bracket" is the in-modal KO signal (see TeamScoreEditorModal).
  const isKnockoutPhase = m.phase === "bracket";
  const canFinish = !decidedByHantei && (isDrawToggled || aTotal > 0 || bTotal > 0);

  // Finish guard (see TeamScoreEditorModal): one tap arms + shows the result on
  // the button, a second commits. Disarms on any score change so a stale result
  // can't be confirmed. Keyboard Enter stays direct (deliberate, not an
  // accidental tablet brush). a-vs-b is AKA-vs-SHIRO; show SHIRO–AKA order.
  const [finishArmed, setFinishArmed] = useStateA(false);
  const finishVerdict = isDrawToggled ? "DRAW" : (aTotal > bTotal ? "AKA WIN" : bTotal > aTotal ? "SHIRO WIN" : "");
  const finishSummary = isDrawToggled ? "DRAW" : `${finishVerdict} ${bTotal}–${aTotal}`.trim();
  useEffectA(() => { setFinishArmed(false); }, [aTotal, bTotal, isDrawToggled]);

  const isDirty =
    !window.arraysEqual(aPts, initialAPts) ||
    !window.arraysEqual(bPts, initialBPts) ||
    aFouls !== initialAFouls ||
    bFouls !== initialBFouls ||
    isDrawToggled !== initialIsDrawToggled ||
    enchoPeriodCount !== initialEnchoPeriods ||
    decidedByHantei !== initialDecidedByHantei;
  const handleDismiss = async () => {
    // Don't close while any save/decision request is in flight — letting
    // the modal unmount would orphan the pending fetch and lose the
    // setState landing.
    if (submitting || decisionSubmitting) return;
    if (isDirty && !(await window.confirmDialog({ message: "Discard unsaved scoring changes?", confirmLabel: "Discard changes", danger: true }))) return;
    onClose();
  };

  // Keyboard shortcuts:
  //   Shift+M/K/D/T/H  → award point to AKA (red, sideA)
  //   m/k/d/t/h        → award point to SHIRO (white, sideB)
  //   Shift+S / s      → award Sune to AKA / SHIRO (naginata competitions only)
  //   x / X            → toggle hikiwake (draw)
  //   ←/→              → previous / next match (skipped inside text-entry elements)
  //   Enter            → finish (or finish + start next when available)
  //   Esc              → close the modal (respects dirty-state confirm)
  // Scoring shortcuts (Enter/M/K/D/T/H/X, plus S in Naginata) are skipped when any interactive
  // element (input, button, link, …) has focus so native activation still works.
  const kbRef = React.useRef(null);
  kbRef.current = { submitting, canFinish, isDrawToggled, isKnockoutPhase, aTotal, bTotal, handleDismiss, onPrev, onNext, onSubmit, onSubmitAndNext, buildPatch, addPt, doSubmit, isNaginata, decidedByHantei, isComplete, correctionReason, setShowCorrectionPrompt, markScoringDirty, cancelScoringDebounce };

  useEffectA(() => {
    const onKeyDown = (ev) => {
      const s = kbRef.current;
      if (s.submitting) return;
      if (ev.ctrlKey || ev.metaKey || ev.altKey) return;

      // Esc routes through handleDismiss so the dirty-state confirm still fires
      if (ev.key === "Escape") { ev.preventDefault(); s.handleDismiss(); return; }

      // Navigation blocked only inside text-entry elements (preserves cursor movement)
      if (!window.isTextEntry(ev.target)) {
        if (ev.key === "ArrowLeft" && s.onPrev) { ev.preventDefault(); s.onPrev(); return; }
        if (ev.key === "ArrowRight" && s.onNext) { ev.preventDefault(); s.onNext(); return; }
      }

      // Scoring shortcuts blocked when any interactive element has focus
      if (window.isInteractiveTarget(ev.target)) return;

      if (ev.key === "Enter" && s.canFinish) {
        ev.preventDefault();
        if (s.isComplete && !s.correctionReason) {
          s.setShowCorrectionPrompt(true);
          return;
        }
        const patch = s.buildPatch("completed");
        // A correction (completed match) saves the current match only — never
        // auto-advance / start-next, even when onSubmitAndNext is wired.
        if (s.onSubmitAndNext && !s.isComplete) s.doSubmit(() => s.onSubmitAndNext(patch));
        else s.doSubmit(() => s.onSubmit(patch));
        return;
      }

      // Scoring shortcuts (point keys + draw toggle) blocked while hantei is armed
      // (backend requires a tied scoreline; any score mutation would produce a 400).
      // Enter and arrow keys are handled above/before this guard and are unaffected.
      if (shouldBlockScoringKeys(s)) return;

      const k = ev.key;
      const upper = k.toUpperCase();
      const validKeys = getValidPointKeys(s.isNaginata);
      if (validKeys.includes(upper) && k.length === 1) {
        ev.preventDefault();
        // Pressing a point key exits draw mode first
        if (s.isDrawToggled) setIsDrawToggled(false);
        // Shift held → AKA (red); no Shift → SHIRO (white). ev.shiftKey is used
        // instead of uppercase detection to avoid Caps Lock misrouting.
        s.addPt(ev.shiftKey ? "a" : "b", upper);
        return;
      }
      if (k === "x" || k === "X") {
        ev.preventDefault();
        // No hikiwake in a knockout match — leave the key inert there.
        if (s.isKnockoutPhase && !s.isDrawToggled) return;
        const r = decideDrawToggle({ isDrawToggled: s.isDrawToggled, aTotal: s.aTotal, bTotal: s.bTotal });
        if (r.action === "cancel") { setIsDrawToggled(false); s.markScoringDirty(); } // C1
        else if (r.action === "enter") { setIsDrawToggled(true); setAPts([]); setBPts([]); s.markScoringDirty(); } // C1
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, []); // listener registered once; reads fresh state via kbRef

  // a11y: label the dialog with the match/court context so screen readers
  // announce who is fighting and on which shiaijo when the modal opens.
  const dialogLabel = `Score editor — ${m.sideB?.name || "Shiro"} vs ${m.sideA?.name || "Aka"}${m.court ? ` · Shiaijo ${m.court}` : ""}`;

  const inner = (
    <>
        <div className="editor-modal__head">
          <div style={{ flex: 1 }}>
            <div className="editor-modal__eyebrow">
              {m.compName} · {m.phase === "pool" ? m.poolName : m.round}
              {enchoPeriodCount > 0 && <span className="editor-modal__eyebrow-encho">· (E) Overtime ×{enchoPeriodCount}</span>}
            </div>
            <div className="editor-modal__title">
              <TermAS name="shiaijo">Shiaijo</TermAS> {m.court} · {m.scheduledAt || "Now"}
            </div>
          </div>
          <div style={{ display: "flex", flexDirection: "column", alignItems: "flex-end", gap: 4 }}>
            <div className={`editor-head-pill ${m.status === "running" ? "sched-row--running" : ""}`} style={{ fontSize: 10, fontWeight: 700 }}>
              {isComplete ? "CORRECTION" : m.status === "running" ? "● NOW" : "PRE-MATCH"}
            </div>
            {/* C2: sync status indicator — only visible while the match is running */}
            <SyncStatusPill isRunning={m.status === "running"} />
            {canClose && <button type="button" className="btn btn--ghost btn--sm" onClick={handleDismiss} disabled={submitting} style={{ padding: "2px 8px" }}>✕ Close</button>}
          </div>
        </div>

        <div className="editor-modal__body">
          {/* FR-033 encho toggle: collapses to a small "⏱ Overtime" pill
              when no overtime is active. Click the pill (or set the
              counter through the existing flow) to mount the full
              counter UI. Saves ~32px of vertical space pre-overtime. */}
          <EnchoControl
            enchoPeriodCount={enchoPeriodCount}
            setEnchoPeriodCount={setEnchoPeriodCount}
            maxEnchoPeriods={maxEnchoPeriods}
          />
          {/* A tied match may be decided by referee hantei. Surface the
              affordance whenever the scoreline is tied — encho is optional,
              not required (operators may go straight to a judges' decision).
              The winner is recorded with the hantei flag, distinguishable
              from an ippon-derived win for stats, audit, and Excel. */}
          {aTotal === bTotal && (
            <div className="hantei-row" data-testid="scoring-modal-hantei-row" style={{ display: "flex", gap: 8, alignItems: "center", padding: "6px 8px", marginBottom: 6, background: "var(--card-2, #fafafa)", borderRadius: 6, fontSize: 12 }}>
              <span style={{ fontWeight: 600, color: "var(--ink-2)" }}>Hantei</span>
              <span style={{ color: "var(--ink-3)" }}>(judges' decision)</span>
              {!decidedByHantei && (
                <button
                  type="button"
                  className="btn btn--sm"
                  data-testid="scoring-modal-hantei-arm"
                  onClick={() => setDecidedByHantei(true)}
                  // Hantei declares a winner from a genuinely tied bout. Disable
                  // the arm button when totals are unequal (an ippon-derived win
                  // is already decided), when the bout is already decided by
                  // ippons (boutDecided), or when a draw is already toggled.
                  // (0-0 is still a valid tied state. Encho is NOT required.)
                  disabled={submitting || decisionSubmitting || aTotal !== bTotal || boutDecided || isDrawToggled}
                  title={
                    submitting || decisionSubmitting
                      ? "Saving…"
                      : isDrawToggled
                        ? "Cancel the draw toggle before using hantei"
                        : aTotal !== bTotal || boutDecided
                          ? "Hantei applies only to a tied scoreline"
                          : "Record a judges' decision"
                  }
                  style={{ marginLeft: "auto" }}
                >
                  Decide by hantei…
                </button>
              )}
              {decidedByHantei && (
                <div style={{ marginLeft: "auto", display: "flex", gap: 6 }}>
                  <button
                    type="button"
                    className="btn btn--sm"
                    data-testid="scoring-modal-hantei-shiro"
                    onClick={() => submitHantei("b")}
                    disabled={submitting || decisionSubmitting}
                  >
                    SHIRO wins
                  </button>
                  <button
                    type="button"
                    className="btn btn--sm"
                    data-testid="scoring-modal-hantei-aka"
                    onClick={() => submitHantei("a")}
                    disabled={submitting || decisionSubmitting}
                  >
                    AKA wins
                  </button>
                  <button
                    type="button"
                    className="btn btn--ghost btn--sm"
                    data-testid="scoring-modal-hantei-cancel"
                    onClick={() => setDecidedByHantei(false)}
                    disabled={submitting || decisionSubmitting}
                  >
                    Cancel
                  </button>
                </div>
              )}
            </div>
          )}
          <div className="scoring-board">
              {/* Score slots + point buttons */}
              <div className="sb-match">
                {sides.map((s, idx) => (
                  <React.Fragment key={s.key}>
                    <div className={`sb-side sb-side--${s.color}`}>
                      <div className="sb-name">{s.name}</div>
                      <div className="sb-slots">
                        {[0, 1].map((i) => (
                          <button type="button" key={i} className={`sb-slot ${s.pts[i] ? "sb-slot--filled" : ""}`} onClick={() => removePt(s.key, i)} disabled={decidedByHantei} title={decidedByHantei ? (initialDecidedByHantei ? "Locked — hantei already recorded" : "Hantei armed — choose a winner above, or cancel") : "Click to remove"}>
                            {s.pts[i] || "·"}
                          </button>
                        ))}
                      </div>
                      <div className="sb-points-grid">
                        {getIpponButtons(isNaginata).map((cc) => (
                          <button type="button" key={cc} className={`ipt-btn ${cc === "H" ? "ipt-btn--h" : ""}`} onClick={() => addPt(s.key, cc)} disabled={boutDecided || decidedByHantei}>{cc}</button>
                        ))}
                      </div>
                    </div>
                    {idx === 0 && (
                      <div className="sb-center">
                        {!isDrawToggled && (
                          <div className="sb-vs">
                            {aTotal === 0 && bTotal === 0 ? "VS" : `${bTotal}–${aTotal}`}
                          </div>
                        )}
                        <button type="button"
                          className={`sb-draw-toggle btn${isDrawToggled ? " sb-draw-toggle--active" : ""}`}
                          data-testid="scoring-modal-mark-draw"
                          onClick={() => {
                            const r = decideDrawToggle({ isDrawToggled, aTotal, bTotal });
                            if (r.action === "cancel") { setIsDrawToggled(false); markScoringDirty(); } // C1
                            else if (r.action === "enter") { setIsDrawToggled(true); setAPts([]); setBPts([]); markScoringDirty(); } // C1
                          }}
                          disabled={decidedByHantei || (!isDrawToggled && (aTotal > 0 || bTotal > 0)) || (!isDrawToggled && isKnockoutPhase)}
                          title={decidedByHantei ? (initialDecidedByHantei ? "Locked — hantei already recorded" : "Hantei armed — choose a winner above, or cancel") : (!isDrawToggled && isKnockoutPhase ? "Knockout matches can't draw — decide by hantei after encho" : (!isDrawToggled && (aTotal > 0 || bTotal > 0) ? "Clear scores before marking a draw" : (isDrawToggled ? "Cancel draw" : "Mark as draw (hikiwake)")))}
                          aria-label={isDrawToggled ? "Cancel draw (hikiwake)" : "Mark as draw (hikiwake)"}
                        >{isDrawToggled ? "Cancel draw" : "Mark draw"}</button>
                      </div>
                    )}
                  </React.Fragment>
                ))}
              </div>

              {/* Independent foul counters */}
              <div className="sb-fouls">
                {sides.map((s) => (
                  <FoulCounter
                    key={s.key}
                    label={s.label}
                    fouls={s.fouls}
                    setFouls={s.setFouls}
                    onIncrement={s.onIncrement}
                    color={s.color}
                    disabled={boutDecided || decidedByHantei}
                  />
                ))}
              </div>
          </div>

          {/* Ippon-type letter legend — discoverable "?" affordance mapping
              M/K/D/T/H (+S in naginata) to their kendo meaning, so operators
              don't depend on the viewer-only glossary. */}
          <IpponLegend isNaginata={isNaginata} />

          {/* T093–T098: decision (kiken/fusenpai) controls + remaining-matches
              follow-up. Sits between the scoring board and the footer so the
              flow is: enter score OR record a decision → either way the modal
              closes (or surfaces the remaining-matches list for kiken). */}
          {!withdrawnPlayer && !decisionPromptKind && !selfReport && (
            <div className="decision-controls" style={{ display: "flex", gap: 8, marginTop: 12, fontSize: 12, alignItems: "center" }}>
              <span style={{ color: "var(--ink-3)", fontWeight: 600 }}>Decision:</span>
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
              {/* Per-bout fusensho is a sub-match concept — implemented inside
                  TeamScoreEditorModal. This placeholder explains the affordance
                  to operators who open the individual-match editor. */}
              <div className="decision-btn-group">
                <button type="button" className="btn btn--sm" disabled title="Fusensho is recorded per-bout inside the team-match editor">
                  Fusensho (team only)
                </button>
                <GlossaryHintAS name="fusensho" />
              </div>
            </div>
          )}
          {decisionErr && (
            <div style={{ color: "var(--danger, #c00)", fontSize: 12, marginTop: 6 }}>{decisionErr}</div>
          )}
          {decisionPromptKind && (
            <DecisionPrompt
              kind={decisionPromptKind}
              sideA={m.sideA}
              sideB={m.sideB}
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

        {/* Sticky navigation + action footer */}
        <div className="editor-modal__foot editor-modal__foot--nav">
          {/* Audit reason prompt — shown when correcting a completed match.
              Operator must confirm a reason before the patch is submitted. */}
          {isComplete && showCorrectionPrompt && (
            <ReasonPrompt
              label="Reason for correction"
              presets={CORRECTION_PRESETS}
              submitting={submitting}
              onConfirm={(r) => {
                setCorrectionReason(r);
                setShowCorrectionPrompt(false);
                // Re-trigger submit with the now-populated reason.
                // buildPatch reads correctionReason from state, but state
                // updates are async — pass r inline via a local override
                // so the patch is correct on the very first submit.
                const patch = { ...buildPatch("completed"), correctionReason: r };
                // Correction (isComplete) saves the current match only.
                if (onSubmitAndNext && !isComplete) doSubmit(() => onSubmitAndNext(patch));
                else doSubmit(() => onSubmit(patch));
              }}
              onCancel={() => setShowCorrectionPrompt(false)}
            />
          )}
          <div className="score-nav">
            {prevMatch ? (
              <button type="button" className="btn btn--sm score-nav__prev" onClick={onPrev} disabled={submitting} title={prevMatch.sideA?.name + " vs " + prevMatch.sideB?.name}>← Prev</button>
            ) : <span />}

            <div className="score-nav__actions">
              {m.status === "scheduled" && (
                <button type="button" className="btn btn--sm" onClick={() => doSubmit(() => onSubmit(buildPatch("running")))} disabled={submitting}>
                  ▶ Start match
                </button>
              )}
              {canClose && <button type="button" className="btn" onClick={handleDismiss} disabled={submitting}>Cancel</button>}
              {onSubmitAndNext ? (
                <button type="button" className={`btn btn--primary ${finishArmed && !isComplete ? "btn--confirm" : ""}`} onClick={() => {
                  if (isComplete && !correctionReason) { setShowCorrectionPrompt(true); return; }
                  if (!isComplete && !finishArmed) { setFinishArmed(true); return; }
                  doSubmit(() => (isComplete ? onSubmit : onSubmitAndNext)(buildPatch("completed")));
                }} disabled={submitting || !canFinish}>
                  {submitting ? "Saving…" : isComplete ? "Save correction" : finishArmed ? `Confirm · ${finishSummary} →` : "Finish + Start Next →"}
                </button>
              ) : (
                <button type="button" className={`btn btn--primary ${finishArmed && !isComplete ? "btn--confirm" : ""}`} onClick={() => {
                  if (isComplete && !correctionReason) { setShowCorrectionPrompt(true); return; }
                  if (!isComplete && !finishArmed) { setFinishArmed(true); return; }
                  doSubmit(() => onSubmit(buildPatch("completed")));
                }} disabled={submitting || !canFinish}>
                  {submitting ? "Saving…" : isComplete ? "Save correction" : finishArmed ? `Confirm · ${finishSummary}` : "Finish"}
                </button>
              )}
            </div>

            {nextMatch ? (
              <button type="button" className="btn btn--sm score-nav__next" onClick={onNext} disabled={submitting} title={nextMatch.sideA?.name + " vs " + nextMatch.sideB?.name}>Next →</button>
            ) : <span />}
          </div>
          {/* Quiet, always-present keyboard-shortcut reminder. */}
          <ScoringShortcutHint />
        </div>
    </>
  );

  // Inline variant (shiaijo operator view): no backdrop / overlay / aria-modal
  // — the panel lives in the page. The shiaijo page passes no prevMatch/
  // nextMatch (queue drives navigation) so the foot's prev/next render as
  // empty spans; Cancel/Close still call onClose to deselect.
  if (variant === "inline") {
    return <div className="scoring-panel" aria-label={dialogLabel}>{inner}</div>;
  }

  return (
    <div className="modal-backdrop" data-testid="scoring-modal-root" onClick={handleDismiss}>
      <div className="editor-modal editor-modal--lg editor-modal--compact" role="dialog" aria-modal="true" aria-label={dialogLabel} onClick={(e) => e.stopPropagation()}>
        {inner}
      </div>
    </div>
  );
}

// Built from MAX_TEAM_SIZE (admin_helpers.jsx) so the scoring UI's
// position count stays in lockstep with the team-size input caps in
// admin_competition.jsx and admin_setup.jsx. Bumping MAX_TEAM_SIZE
// flows automatically to all three sites.
const TEAM_POSITIONS = Array.from({ length: window.MAX_TEAM_SIZE }, (_, i) => String(i + 1));

// T131 helper: human-friendly position label for the team-match scoring
// modal. 5-person teams use the canonical FIK names; non-5 sizes use the
// position number. Kept inline here so the team modal doesn't have a
// hard import dependency on admin_lineup.jsx (the two files are loaded
// independently and admin_lineup.jsx may not be present in older
// builds). The mapping mirrors POS_LABELS_5 in admin_lineup.jsx.
const POS_LABELS_BY_INDEX_5 = ["Senpo", "Jiho", "Chuken", "Fukusho", "Taisho"];
const POS_ABBREV_BY_INDEX_5 = ["Sen", "Ji", "Chu", "Fuk", "Tai"];
function positionLabelFor(teamSize, index, sub) {
  if (sub && sub.position && typeof sub.position === "string" && sub.position.length > 0 && /[a-z]/i.test(sub.position)) {
    // Backend may emit a name string in Position for non-5 sizes once
    // domain.Position is wire-stable. Use it verbatim when present.
    return sub.position;
  }
  if (teamSize === 5 && index >= 0 && index < 5) return POS_LABELS_BY_INDEX_5[index];
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

// teamResultLabel — the RESULT-band / Finish-button verdict text for a team
// encounter. A knockout match cannot be a draw (a tie is broken by a daihyosen,
// FIK rules), so a null winner in the bracket phase never reads "DRAW": it's
// "DAIHYOSEN" once a scored tie exists to break, or "—" before any bout lands.
// Only a pool encounter reads a null winner as a true draw. (a = Aka, b = Shiro.)
export function teamResultLabel({ teamWinner, isKnockoutPhase, hasAnyScore }) {
  if (teamWinner === "a") return "AKA WIN";
  if (teamWinner === "b") return "SHIRO WIN";
  if (isKnockoutPhase) return hasAnyScore ? "DAIHYOSEN" : "—";
  return "DRAW";
}

// isKoTieBlocked — Finish must be blocked while a knockout encounter has no
// winner: the operator has to add and score a daihyosen first. Pool draws stay
// finishable, and an already-completed match (correction flow) is never blocked.
export function isKoTieBlocked({ isKnockoutPhase, teamWinner, isComplete }) {
  return !!isKnockoutPhase && teamWinner === null && !isComplete;
}

// mp-bkg / mp-13y: resolveMatchLineup and resolveLineupTeamId are now shared
// across all consumer surfaces (admin scoring modal, viewer, TvDisplay,
// StreamingOverlay). The implementations live in lineup_resolver.js;
// re-exported here so existing imports from admin_scoring_modal.jsx continue
// to work (scoring_modal_match_lineup.test.jsx imports directly from here).
import { resolveMatchLineup, resolveLineupTeamId } from './lineup_resolver.jsx';

function TeamScoreEditorModal({ match, teamSize, onClose, onSubmit, onSubmitAndNext, prevMatch, nextMatch, onPrev, onNext, password, selfReport, variant = "modal", canClose = true }) {
  const m = match;
  const isComplete = m.status === "completed";
  const numberedPositions = TEAM_POSITIONS.slice(0, teamSize);
  // mp-4pc: a persisted daihyosen (representative bout) lives in
  // SubResults at wire position -1. It is scored "like any other
  // sub-match" (handlers_daihyosen.go) but is NOT an individual victory —
  // it breaks an IV/PW tie. Render it as a trailing scoreable row,
  // exclude it from the IV/PW tally, and let its winner decide the
  // encounter. The "daihyosen" slot sentinel maps to position -1 in
  // buildPatch. It is the ONLY team sub-bout that may carry encho/hantei
  // (validation.go validateSubBout).
  const existingDaihyosen = (m.subResults || []).find(s => s.position === -1);
  const hasDaihyosen = !!existingDaihyosen;
  const positions = hasDaihyosen ? [...numberedPositions, "daihyosen"] : numberedPositions;
  const daihyosenIdx = hasDaihyosen ? numberedPositions.length : -1;
  // FR-033: encho counter for team matches (overtime period count rides
  // alongside the score on the wire — same shape as ScoreEditorModal).
  // mp-4pc: derive from the daihyosen sub when present — see
  // initialEnchoPeriodsForMatch for why. Captured in a const so isDirty
  // can compare against the initial value (the function is not idempotent
  // across re-renders because m may mutate).
  const initialEnchoPeriods = initialEnchoPeriodsForMatch(m);
  const [enchoPeriodCount, setEnchoPeriodCount] = useStateA(initialEnchoPeriods);
  const [submitting, setSubmitting] = useStateA(false);
  // T093–T098: decision state — same shape as the individual editor. See the
  // ScoreEditorModal copy for the contract.
  const [decisionPromptKind, setDecisionPromptKind] = useStateA("");
  const [decisionSubmitting, setDecisionSubmitting] = useStateA(false);
  const [decisionErr, setDecisionErr] = useStateA("");
  const [withdrawnPlayer, setWithdrawnPlayer] = useStateA(null);
  // Audit reason collected when correcting a completed team match — mirrors
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
  // 400 not_tied / 400 pool_match / 409 insufficient_eligibility — see
  // handlers_daihyosen.go for the canonical strings.
  const [daihyosenErr, setDaihyosenErr] = useStateA("");
  const [daihyosenBusy, setDaihyosenBusy] = useStateA(false);
  // mp-4pc: the daihyosen is the only team sub-bout that may be decided
  // by hantei (judges' decision on a tied bout, FIK 7-5 / 29-6 — encho
  // optional). Mirrors the individual ScoreEditorModal hantei flow but scoped to the
  // position -1 row. "" = score-decided; "a"/"b" = hantei winner side.
  const initialDaihyosenHantei = existingDaihyosen?.decidedByHantei
    ? (existingDaihyosen.winner === (typeof m.sideA === "object" ? m.sideA?.name : m.sideA) ? "a" : "b")
    : "";
  const [daihyosenHantei, setDaihyosenHantei] = useStateA(initialDaihyosenHantei);
  const [daihyosenHanteiArmed, setDaihyosenHanteiArmed] = useStateA(!!initialDaihyosenHantei);
  // Same teardown-race guard as ScoreEditorModal — covers external/
  // parent-driven unmount during in-flight save.
  const mountedRef = useRefA(true);

  // C1: debounced autosave refs (same pattern as ScoreEditorModal).
  // Updated after buildPatch is defined below.
  const _autosaveIsRunningRefT = useRefA(false);
  const _autosaveBuildPatchRefT = useRefA(null);
  const _autosaveOnSubmitRefT = useRefA(null);
  const { markDirty: markScoringDirtyT, cancelDebounce: cancelScoringDebounceT } = useDebouncedRunningWrite({
    isRunningRef: _autosaveIsRunningRefT,
    buildPatchRef: _autosaveBuildPatchRefT,
    onSubmitRef: _autosaveOnSubmitRefT,
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
      // lineup is stored under first — otherwise every GET 404s.
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
  // for ≤5-person teams. Kachinuki renders only the current bout
  // (see visiblePositions: positions.slice(kachinukiIdx, kachinukiIdx+1)),
  // so it always fits even with a 9-person roster. Larger fixed-format
  // teams keep the roomier layout and use .team-bouts-scroll for
  // independent bout-list scrolling.
  const useCompact = teamSize <= 5 || isKachinuki;
  // T141: daihyosen is knockout-only — pool matches resolve ties via
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
  // bracket/unknown-phase matches in KO-bearing formats — it must exclude
  // explicit pool matches, or a drawn pool match becomes unfinishable and the
  // in-match daihyosen affordance wrongly appears (the comment above this line
  // already states daihyosen is knockout-only).
  const isKnockoutPhase = m.phase === "bracket"
    || ((compFormat === "playoffs" || compFormat === "mixed") && m.phase !== "pool");

  // Inline lineup select state: tracks whether a lineup-reason prompt is shown
  // for inline position changes mid-match, and which (teamId, positionKey, value)
  // is pending confirmation.
  const [inlineLineupPrompt, setInlineLineupPrompt] = useStateA(null); // { teamId, posKey, value, lineup }
  const [inlineLineupSaving, setInlineLineupSaving] = useStateA(false);

  // Derive each team's roster from compMeta.players. rosterFor expects the
  // team object (with metadata array); resolveLineupTeamId matches by name.
  const allPlayers =
    (compMeta?.players?.length ? compMeta.players : null)
    || (compMeta?.config?.players)
    || [];
  const rosterForSide = (side) => {
    if (!window.AdminLineupHelpers?.rosterFor) return [];
    const sideKey = typeof side === "object" ? (side?.id || side?.name) : side;
    const teamObj = allPlayers.find(p => {
      const pid = p?.id || p?.ID || p?.name || p?.Name || "";
      const pname = p?.name || p?.Name || "";
      return pid === sideKey || pname === sideKey;
    });
    return window.AdminLineupHelpers.rosterFor(teamObj || null);
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
  // existing lineup + the changed key→value, then PUTs.
  // force=true + reason only when the match is live AND we are changing an
  // already-recorded player (a substitution). Adding to an empty slot saves
  // directly with force=false — the backend now allows this at any time.
  const submitInlineLineup = async (teamId, lineup, posKey, value, reason, force) => {
    setInlineLineupSaving(true);
    try {
      const existing = lineup?.positions || {};
      const updated = { ...existing };
      if (value) updated[posKey] = value;
      else delete updated[posKey];
      await window.API.putMatchLineup(m.compId, teamId, m.id, updated, password, force, reason);
      // Refresh lineup state from the response is deferred — on next open the
      // modal re-fetches. For immediate feedback we do a partial reload of
      // lineup state for the affected side.
      if (!mountedRef.current) return;
      if (teamId === teamIdForSide(m.sideA)) {
        setLineupA(prev => ({ ...(prev || {}), positions: updated }));
      } else {
        setLineupB(prev => ({ ...(prev || {}), positions: updated }));
      }
    } catch (e) {
      // Surface error briefly — can't use a toast from inside the modal so
      // we reuse the daihyosenErr channel for a one-off message.
      if (mountedRef.current) setDaihyosenErr(e?.message || "Failed to update lineup");
    } finally {
      if (mountedRef.current) setInlineLineupSaving(false);
    }
  };

  // Shared factory (admin_scoring_shared.jsx) — same handler as ScoreEditorModal;
  // "teams" is the only per-modal wording (in the decision_locked confirm).
  const submitDecision = makeSubmitDecision({
    match: m, enchoPeriodCount, password, mountedRef,
    setDecisionSubmitting, setDecisionErr, setWithdrawnPlayer, setDecisionPromptKind,
    onClose, entityLabel: "teams",
  });

  const existingSub = m.subResults || [];
  // T096/FR-031: round-trip per-bout fusensho. SubMatchResult.decision is
  // the canonical signal — when "fusensho", figure out which side it
  // belongs to via the recorded winner so the UI re-opens with the
  // affordance shown as active.
  const sideAName = typeof m.sideA === "object" ? m.sideA?.name : m.sideA;
  const sideBName = typeof m.sideB === "object" ? m.sideB?.name : m.sideB;
  const initSubsRef = React.useRef(null);
  if (initSubsRef.current === null) {
    initSubsRef.current = positions.map((_, idx) => {
      const pos = idx === daihyosenIdx ? -1 : idx + 1;
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
        // Operator-marked hikiwake for this bout (live display only — a 0–0
        // bout already serialises to hikiwake on finish, so this just lets the
        // centre show X/△ during scoring instead of the pending dash).
        draw: false,
      };
    });
  }
  const [subs, setSubs] = useStateA(initSubsRef.current);
  // C1: updateSub is the single choke-point for all sub-bout state
  // mutations. Calling markScoringDirtyT() here captures every edit
  // (pts add/remove, fouls, fusensho, draw) without repetition.
  const updateSub = (idx, fn) => { setSubs(prev => prev.map((s, i) => i === idx ? fn(s) : s)); markScoringDirtyT(); };

  // T096/FR-031: per-bout Fusensho — award a 2-0 default win to the
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

  // mp-4pc: the daihyosen row (when present) is excluded from IV/PW — it
  // is a tiebreaker, not an individual victory. Its own winner (hantei
  // side first, then score) decides the encounter.
  const ivA = subTotals.filter((s, i) => i !== daihyosenIdx && s.winner === "a").length;
  const ivB = subTotals.filter((s, i) => i !== daihyosenIdx && s.winner === "b").length;
  const pwA = subTotals.reduce((sum, s, i) => i === daihyosenIdx ? sum : sum + s.aTotal, 0);
  const pwB = subTotals.reduce((sum, s, i) => i === daihyosenIdx ? sum : sum + s.bTotal, 0);
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
  // confirm a stale verdict. Keyboard Enter is left direct — it's deliberate,
  // unlike an accidental brush on a tablet. (a-vs-b is AKA-vs-SHIRO; the band
  // and this label read SHIRO–AKA to match the sheet's left-right order.)
  const [finishArmed, setFinishArmed] = useStateA(false);
  // A knockout encounter cannot end in a draw — a tie is resolved by a
  // representative bout (daihyosen), not recorded as hikiwake. So in a KO phase
  // a null teamWinner is never "DRAW": it's "DAIHYOSEN" once there's a scored
  // tie to break, or simply pending ("—") before any bout lands. Only pool
  // matches read a null winner as a true draw.
  const teamHasAnyScore = (ivA + ivB + pwA + pwB) > 0;
  const teamVerdictText = teamResultLabel({ teamWinner, isKnockoutPhase, hasAnyScore: teamHasAnyScore });
  // Block Finish while a KO encounter has no winner: the operator must add and
  // score a daihyosen first (the affordance below). Pool draws stay finishable.
  const koTieBlocked = isKoTieBlocked({ isKnockoutPhase, teamWinner, isComplete });
  const finishSummary = `${teamVerdictText} · IV ${ivB}–${ivA} · PW ${pwB}–${pwA}`;
  useEffectA(() => { setFinishArmed(false); }, [ivA, ivB, pwA, pwB, teamWinner]);

  // mp-4pc: when a daihyosen exists the encho counter belongs to that
  // sub-bout (attached per-sub in buildPatch), so suppress the top-level
  // encho to avoid duplicate/ambiguous semantics on the team match.
  const enchoBlock = () => (enchoPeriodCount > 0 && !hasDaihyosen) ? { encho: { periodCount: enchoPeriodCount } } : {};

  const buildPatch = (targetStatus) => {
    if (targetStatus === "scheduled") return { winner: null, status: "scheduled", score: null, ipponsA: [], ipponsB: [], subResults: [] };
    const subResults = subs.map((s, idx) => {
      const t = subTotals[idx];
      const isDaihyo = idx === daihyosenIdx;
      // Hansoku Hs already in pts arrays via applyFoulIncrement — no fold.
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
      const entry = {
        position: isDaihyo ? -1 : idx + 1,
        sideA: typeof m.sideA === "object" ? m.sideA?.name : m.sideA,
        sideB: typeof m.sideB === "object" ? m.sideB?.name : m.sideB,
        ipponsA: aAll,
        ipponsB: bAll,
        hansokuA: s.aFouls,
        hansokuB: s.bFouls,
        winner: w ? (typeof w === "object" ? w.name : w) : "",
        decision,
      };
      // mp-4pc: encho + hantei are valid ONLY on the daihyosen
      // (validation.go validateSubBout). daihyosenEnchoFields emits the two
      // independently — encho is optional for a hantei decision.
      if (isDaihyo) {
        Object.assign(entry, daihyosenEnchoFields({ enchoPeriodCount, daihyosenTied, daihyosenHantei }));
      }
      return entry;
    });
    const winner = teamWinner === "a" ? m.sideA : teamWinner === "b" ? m.sideB : null;
    const correctionBlock = isComplete && correctionReason ? { correctionReason } : {};
    // When transitioning to "running" (▶ Start), teamWinner is typically
    // null (0–0). Don't emit score.type: "hikiwake" — toBackendMatchResult
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
  _autosaveIsRunningRefT.current = m.status === "running";
  _autosaveBuildPatchRefT.current = buildPatch;
  _autosaveOnSubmitRefT.current = onSubmit;

  const doSubmit = async (fn) => {
    cancelScoringDebounceT(); // C1: cancel pending autosave before explicit submit
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
  // surface (M/K/D/T/H, ←/→, Enter) isn't wired here — team scoring is
  // many sub-matches and would need different bindings — but Esc is
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
  const dialogLabel = `Team score editor — ${m.sideB?.name || m.sideB || "Shiro"} vs ${m.sideA?.name || m.sideA || "Aka"}${m.court ? ` · Shiaijo ${m.court}` : ""}`;

  const inner = (
    <>
        <div className="editor-modal__head">
          <div style={{ flex: 1 }}>
            <div className="editor-modal__eyebrow">
              {m.compName} · {m.phase === "pool" ? m.poolName : m.round}
              {enchoPeriodCount > 0 && <span className="editor-modal__eyebrow-encho">· (E) Overtime ×{enchoPeriodCount}</span>}
            </div>
            <div className="editor-modal__title">
              <TermAS name="shiaijo">Shiaijo</TermAS> {m.court} · {m.scheduledAt || "Now"}
            </div>
          </div>
          <div style={{ display: "flex", flexDirection: "column", alignItems: "flex-end", gap: 4 }}>
            <div className={`editor-head-pill ${m.status === "running" ? "sched-row--running" : ""}`} style={{ fontSize: 10, fontWeight: 700 }}>
              {isComplete ? "CORRECTION" : m.status === "running" ? "● NOW" : "PRE-MATCH"}
            </div>
            {/* C2: sync status indicator — only visible while the match is running */}
            <SyncStatusPill isRunning={m.status === "running"} />
            {canClose && <button type="button" className="btn btn--ghost btn--sm" onClick={handleDismiss} disabled={submitting} style={{ padding: "2px 8px" }}>✕ Close</button>}
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
          {/* Non-blocking lineup-incomplete hints — one per team, shown only
              for 5-person teams when Senpo or Taisho is unset or any position
              is empty. Muted and informational: does NOT block scoring. */}
          {teamSize === 5 && (lineupIncompleteB || lineupIncompleteA) && (
            <div style={{ display: "flex", gap: 8, marginBottom: 12 }}>
              {[
                { incomplete: lineupIncompleteB, label: "SHIRO" },
                { incomplete: lineupIncompleteA, label: "AKA" },
              ].map(({ incomplete, label }) => incomplete ? (
                <div key={label} className="tsm-lineup__incomplete">
                  {label}: Lineup incomplete — add the remaining players
                </div>
              ) : null)}
            </div>
          )}

          {/* Individual match rows. T136: in kachinuki mode only the
              CURRENT bout is rendered (positions.slice(kachinukiIdx,
              kachinukiIdx+1)) — the last row that has any data, or row 0
              if nothing has been scored yet. The server appends new bouts
              via engine.MaybeAdvanceKachinuki after each score record, so
              the operator re-opens the modal to score the next bout.
              The .team-bouts-scroll wrapper gives the roomy (non-compact)
              layout an independent scroll region for the bout list so the
              team header / summary / decision / footer stay anchored. */}
          <div className="team-bouts-scroll">
          {(() => {
            // T136: kachinuki "current bout" index — last row that has
            // any data, or 0 if nothing scored yet.
            let kachinukiIdx = 0;
            for (let i = subs.length - 1; i >= 0; i--) {
              if (subs[i].aPts.length > 0 || subs[i].bPts.length > 0 || subs[i].aFouls > 0 || subs[i].bFouls > 0) {
                kachinukiIdx = i;
                break;
              }
            }
            // T136: "kachinuki-exhaustion" sentinel — surface the end
            // banner instead of more bout rows when the backend has
            // already decided the match.
            const exhausted = isKachinuki && (m.decision === "kachinuki-exhaustion" || (m.subResults || []).some(s => s.decision === "kachinuki-exhaustion"));
            const visiblePositions = isKachinuki ? positions.slice(kachinukiIdx, kachinukiIdx + 1) : positions;
            return [
              isKachinuki && (
                <div key="kachinuki-banner" style={{ background: "var(--bg-2, #fafafa)", border: "1px solid var(--accent, #ddd)", borderRadius: 4, padding: "8px 12px", marginBottom: 12, fontSize: 12, display: "flex", flexDirection: "column", gap: 4 }}>
                  <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
                    <span style={{ fontWeight: 700 }}><TermAS name="kachinuki">Kachinuki</TermAS> (winner-stays)</span>
                    <span style={{ color: "var(--ink-3)" }}>
                      {exhausted
                        ? "One team exhausted — match ended."
                        : "Score only the current bout. The server appends the next bout automatically; reopen the match to score it."}
                    </span>
                  </div>
                  {/* TODO(T136): inline auto-refresh after each score so
                      operators don't have to close+reopen the modal —
                      requires hooking the onSubmit response (current
                      flow forwards through parent + closes the modal). */}
                </div>
              ),
              ...visiblePositions
            ];
          })().filter(Boolean).map((pos, _displayIdx) => {
            // Kachinuki returns a banner element as the first item; pass
            // it through unchanged. Other items are position strings —
            // map them back to their canonical index in `positions`.
            if (React.isValidElement(pos)) return pos;
            const idx = positions.indexOf(pos);
            const s = subs[idx];
            const t = subTotals[idx];
            // T131: pull the per-side player + position label. existingSub
            // (from the match) and lineup data are both consulted so the
            // bout cell shows e.g. "Match 1 (Senpo) — A. Tanaka vs B. Sato".
            const isDaihyoRow = idx === daihyosenIdx;
            const existingSubAtIdx = (m.subResults || []).find(sr => sr.position === (isDaihyoRow ? -1 : idx + 1));
            const posLabel = isDaihyoRow ? "Daihyosen" : positionLabelFor(teamSize, idx, existingSubAtIdx);
            const posAbbrev = isDaihyoRow ? "" : positionAbbrevFor(teamSize, idx, existingSubAtIdx);
            // Resolve the player name occupying this position on each
            // side: lineup data first (canonical when present), then the
            // SubMatchResult.SideA/SideB strings from a prior score.
            //
            // 5-person teams use named position keys (senpo, jiho, ...);
            // other sizes use the numeric string "1".."N". Try both
            // shapes so this stays size-agnostic.
            const posKey5 = (teamSize === 5 && idx < 5) ? POS_LABELS_BY_INDEX_5[idx].toLowerCase() : null;
            const posKeyN = String(positions[idx]);
            const pickFromLineup = (lineup) => {
              if (!lineup?.positions) return "";
              if (posKey5 && lineup.positions[posKey5]) return lineup.positions[posKey5];
              if (lineup.positions[posKeyN]) return lineup.positions[posKeyN];
              return "";
            };
            const playerAName = pickFromLineup(lineupA) || existingSubAtIdx?.sideA || "";
            const playerBName = pickFromLineup(lineupB) || existingSubAtIdx?.sideB || "";

            // Feature 2 / layout: each player's name select lives WITH that
            // side's score controls (grouped, and aligned down the sheet),
            // not in the position column. Compute the per-side name props here
            // so they can ride on the rowSides entries below.
            const matchStartedForLineup = m.status === "running" || m.status === "completed";
            const lineupPosKey = posKey5 || posKeyN;
            const teamIdB = teamIdForSide(m.sideB); // SHIRO = left
            const teamIdA = teamIdForSide(m.sideA); // AKA = right
            const rosterB = rosterForSide(m.sideB);
            const rosterA = rosterForSide(m.sideA);
            const pickPlayer = (teamId, lineup) => (value) => {
              const prev = (lineup?.positions || {})[lineupPosKey] || "";
              // A mid-match substitution (changing or clearing an already-recorded
              // player) needs an audit reason + the force override. Adding a name to an
              // empty slot — the normal live-entry case — and any pre-match edit save
              // directly.
              const isChange = prev !== "" && value !== prev;
              if (matchStartedForLineup && isChange) {
                setInlineLineupPrompt({ teamId, posKey: lineupPosKey, value, lineup });
              } else {
                submitInlineLineup(teamId, lineup, lineupPosKey, value, "", false);
              }
            };

            // Each row: [left side, center score, right side] — left=SHIRO, right=AKA
            // T096/FR-031: manual pts/fouls edits clear the per-bout fusensho
            // flag AND discard the _preFusensho snapshot so the bout becomes
            // a regular fought score once the operator intervenes. Re-applying
            // via the Fusensho button captures a fresh snapshot from the
            // current (manually-edited) state.
            // onIncrement applies the FIK 2-foul rule via applyFoulIncrement:
            // the 2nd foul auto-awards an H to the OPPONENT and resets this
            // side's foul counter. The auto-award also invalidates the
            // _preFusensho snapshot — once an H lands in the slot the prior
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
                playerName: playerBName, roster: rosterB, onSelectName: pickPlayer(teamIdB, lineupB),
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
                playerName: playerAName, roster: rosterA, onSelectName: pickPlayer(teamIdA, lineupA),
              },
            ];

            // Sub-bout is decided once either side reaches 2 ippons.
            const subBoutDecided = isBoutDecided(s.aPts, s.bPts);

            const scoreDisplay = (() => {
              // mp-4pc: a hantei-decided daihyosen has a tied scoreline but
              // a declared winner — show the winner + (Ht) rather than X.
              if (isDaihyoRow && daihyosenTied && daihyosenHantei) {
                return <span>{`${t.bTotal}–${t.aTotal}`} <span style={{ fontSize: 11, opacity: 0.7 }}>(Ht)</span></span>;
              }
              // Draw: either an operator-marked tie, or equal non-zero scores
              // (the tie-marking rule). Canonical display: a hikiwake is an X on
              // the centre line (running_a_kendo_tournament.md), scored or not.
              const scored = t.aTotal > 0 || t.bTotal > 0;
              const isDraw = s.draw || (t.winner === null && scored);
              if (isDraw) return <span className="tsm-draw">X</span>;
              // Pending bout (0–0, not yet marked) — a quiet placeholder.
              if (t.winner === null) return <span style={{ color: "var(--ink-3)" }}>–</span>;
              // Decided bout: the centred ippon letters already show who won —
              // the numeric tally was redundant, so the centre stays clear.
              return null;
            })();

            return (
              <div key={idx} className="team-sub-match">
                <div className="team-sub-match__pos" title={posLabel}>
                  {/* Bout number AND the FIK position handle (Sen/Ji/Chu/Fuk/Tai
                      for 5-person teams) — operators think in positions, so the
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
                            no roster metadata. Mid-match changes gate through the
                            audit ReasonPrompt; pre-match are saved direct. */}
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
                              : <span className="tsm-name__static tsm-name__static--empty">—</span>
                          )}
                        </div>
                        {/* Row 1: point slots + M/K/D/T/H buttons. In compact
                            mode these align on one horizontal channel-strip;
                            in roomy mode the wrapper is display:contents so the
                            legacy column stack is preserved. */}
                        <div className="tsm-row-1">
                          {/* Buttons only — the scored ippon letters show in the
                              centre column (between the two competitors), like an
                              individual bout. H (hansoku point) renders as △ there. */}
                          <div className="team-sub-match__btns">
                            {getIpponButtons(isNaginataTeam).map(cc => (
                              <button type="button" key={cc} className={`ipt-btn ipt-btn--sm ${cc === "H" ? "ipt-btn--h" : ""}`}
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
                            the opponent's pts array — no derived display.
                            T096/FR-031: per-bout Fusensho — awards the bout
                            2-0 to this side. Re-clicking the active side
                            undoes the fusensho; manual pts/fouls edits while
                            active clear the flag and discard the snapshot. */}
                        <div className="tsm-row-2">
                          <div className="tsm-fouls" data-testid={`scoring-modal-hansoku-${rs.color}`}>
                            <span className="tsm-fouls__label">{rs.label} Fouls</span>
                            <div className="tsm-fouls__controls">
                              <button type="button" className="tsm-fouls__btn" onClick={() => rs.setFouls(nextFoulOnDecrement(rs.fouls))} disabled={rs.fouls === 0}>−</button>
                              <span className={`tsm-fouls__count ${rs.fouls >= 1 ? "tsm-fouls__count--warn" : ""}`}>{rs.fouls}</span>
                              <button type="button" className="tsm-fouls__btn" onClick={rs.onIncrement} disabled={subBoutDecided}>+</button>
                            </div>
                          </div>
                          <div className="tsm-fusensho">
                            <button
                              data-testid="scoring-modal-fusensho-button"
                              type="button"
                              className={`btn btn--sm ${s.fusensho === rs.key ? "btn--primary" : ""}`}
                              onClick={() => setFusenshoFor(idx, rs.key)}
                              title={s.fusensho === rs.key
                                ? `Click to undo fusensho — restores the previous score`
                                : `Mark bout as fusensho — default win 2-0 to ${rs.label}`}
                            >
                              {s.fusensho === rs.key
                                ? <>✓ <TermAS name="fusensho">Fusensho</TermAS></>
                                : <TermAS name="fusensho">Fusensho</TermAS>}
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
                            {rowSides[0].fouls >= 1 && <span className="tsm-foul-tri" title="Hansoku — 1 foul">▲</span>}
                            {[0, 1].map(i => (
                              <button type="button" key={i} className={`editor-side__pt ${rowSides[0].pts[i] ? "editor-side__pt--filled" : ""}`}
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
                              <button type="button" key={i} className={`editor-side__pt ${rowSides[1].pts[i] ? "editor-side__pt--filled" : ""}`}
                                onClick={() => rowSides[1].setPts(rowSides[1].pts.filter((_, j) => j !== i))} title="Click to remove">
                                {rowSides[1].pts[i] || "·"}
                              </button>
                            ))}
                            {/* Outstanding hansoku → red ▲ next to the Aka name
                                (the outer/right edge), after the reversed slots. */}
                            {rowSides[1].fouls >= 1 && <span className="tsm-foul-tri" title="Hansoku — 1 foul">▲</span>}
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
                                {s.draw ? <>✓ Tie (<TermAS name="hikiwake">hikiwake</TermAS>)</> : <>Tie (<TermAS name="hikiwake">hikiwake</TermAS>)</>}
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

          {/* Team summary — T138: sticky to the top of the modal body so
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

          {/* mp-4pc: hantei affordance for the daihyosen — the rep bout is
              the only team sub-bout that may be decided by judges (FIK 7-5 /
              29-6). Encho is optional: a tied daihyosen may be taken straight
              to a judges' decision. Mounts whenever a daihyosen exists;
              arming requires a tied scoreline. The chosen winner rides onto
              the position -1 sub (decidedByHantei) when the operator saves. */}
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

          {/* mp-c2yr: daihyosen (representative bout) affordance — an
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
            // Local tie detection drives the highlight + helper copy only —
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
                // flow — otherwise recordDaihyosen runs against the stale
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

          {/* Ippon-type letter legend — same affordance as the individual
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
                (<TermAS name="fusensho">Fusensho</TermAS> is per-bout — use the "Fusensho" button on each row above.)
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
          {/* Inline lineup position change — requires an audit reason when
              the match has already started (force=true on the API call). */}
          {inlineLineupPrompt && (
            <ReasonPrompt
              label="Reason for lineup change"
              presets={LINEUP_PRESETS}
              submitting={inlineLineupSaving}
              onConfirm={(r) => {
                const { teamId, posKey, value, lineup } = inlineLineupPrompt;
                setInlineLineupPrompt(null);
                submitInlineLineup(teamId, lineup, posKey, value, r, true);
              }}
              onCancel={() => setInlineLineupPrompt(null)}
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
          {/* Audit reason prompt for team-match corrections — same contract
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
                // Correction (isComplete) saves the current match only.
                if (onSubmitAndNext && !isComplete) doSubmit(() => onSubmitAndNext(patch));
                else doSubmit(() => onSubmit(patch));
              }}
              onCancel={() => setShowCorrectionPrompt(false)}
            />
          )}
          <div className="score-nav">
            {prevMatch ? (
              <button type="button" className="btn btn--sm score-nav__prev" onClick={onPrev} disabled={submitting}>← Prev</button>
            ) : <span />}
            <div className="score-nav__actions">
              {m.status === "scheduled" && (
                <button type="button" className="btn btn--sm" onClick={() => doSubmit(() => onSubmit(buildPatch("running")))} disabled={submitting}>▶ Start match</button>
              )}
              {canClose && <button type="button" className="btn" onClick={handleDismiss} disabled={submitting}>Cancel</button>}
              {onSubmitAndNext ? (
                <button type="button" className={`btn btn--primary ${finishArmed && !isComplete ? "btn--confirm" : ""}`} onClick={() => {
                  if (isComplete && !correctionReason) { setShowCorrectionPrompt(true); return; }
                  if (!isComplete && !finishArmed) { setFinishArmed(true); return; }
                  doSubmit(() => (isComplete ? onSubmit : onSubmitAndNext)(buildPatch("completed")));
                }} disabled={submitting || koTieBlocked}
                  title={koTieBlocked ? "A knockout match can't be a draw — add and score a daihyosen to decide a winner" : undefined}>
                  {submitting ? "Saving…" : isComplete ? "Save correction" : koTieBlocked ? "Needs a winner" : finishArmed ? `Confirm · ${finishSummary} →` : "Finish + Start Next →"}
                </button>
              ) : (
                <button type="button" className={`btn btn--primary ${finishArmed && !isComplete ? "btn--confirm" : ""}`} onClick={() => {
                  if (isComplete && !correctionReason) { setShowCorrectionPrompt(true); return; }
                  if (!isComplete && !finishArmed) { setFinishArmed(true); return; }
                  doSubmit(() => onSubmit(buildPatch("completed")));
                }} disabled={submitting || koTieBlocked}
                  title={koTieBlocked ? "A knockout match can't be a draw — add and score a daihyosen to decide a winner" : undefined}>
                  {submitting ? "Saving…" : isComplete ? "Save correction" : koTieBlocked ? "Needs a winner" : finishArmed ? `Confirm · ${finishSummary}` : "Finish"}
                </button>
              )}
            </div>
            {nextMatch ? (
              <button type="button" className="btn btn--sm score-nav__next" onClick={onNext} disabled={submitting}>Next →</button>
            ) : <span />}
          </div>
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

window.ScoreEditorModal = ScoreEditorModal;

// ES exports for the vitest suite. Most are pure helpers, but the list also
// includes a few test-only component/hook exports (SyncStatusPill,
// useDebouncedRunningWrite) that the render tests mount directly. The primary
// component (ScoreEditorModal) still ships via the window.* pattern to match
// the rest of admin_*.jsx — these named exports are a test seam, not an
// architectural guarantee that components are exported.
export {
  useDebouncedRunningWrite,
  SyncStatusPill,
  AUTOSAVE_DEBOUNCE_MS,
  resolveDecisionPassword,
  assertRunningWritePersisted,
  buildDecisionBody,
  submitDecisionRequest,
  makeSubmitDecision,
  shouldShowEnchoMaxBanner,
  canIncrementEncho,
  nextEnchoPeriod,
  prevEnchoPeriod,
  initialEnchoPeriodsForMatch,
  daihyosenEnchoFields,
  decideDrawToggle,
  shouldBlockScoringKeys,
  DecisionPrompt,
  MAX_IPPONS_PER_SIDE,
  isBoutDecided,
  applyFoulIncrement,
  reconcileFoulsAtOpen,
  nextFoulOnDecrement,
  applyFusenshoToggle,
  getIpponButtons,
  getValidPointKeys,
  resolveMatchLineup,
  resolveLineupTeamId,
};
