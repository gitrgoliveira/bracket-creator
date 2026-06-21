// Individual match score editor (ScoreEditorModal).
// For team matches, ScoreEditorModal routes internally to TeamScoreEditorModal
// (imported from admin_scoring_team.jsx).
// Extracted from admin_scoring_modal.jsx (mp-zac3).

const { useState: useStateA, useEffect: useEffectA, useRef: useRefA } = React;

import {
  MAX_IPPONS_PER_SIDE,
  isBoutDecided,
  getIpponButtons,
  getValidPointKeys,
  IpponLegend,
  ScoringShortcutHint,
  applyFoulIncrement,
  reconcileFoulsAtOpen,
  TermAS,
  GlossaryHintAS,
  resolveDecisionPassword,
  makeSubmitDecision,
  decideDrawToggle,
  shouldBlockScoringKeys,
  EnchoControl,
  DecisionPrompt,
  RemainingMatchesPanel,
  FoulCounter,
  ReasonPrompt,
  CORRECTION_PRESETS,
} from './admin_scoring_shared.jsx';

import { SyncStatusPill, useDebouncedRunningWrite } from './admin_scoring_autosave.jsx';

import { TeamScoreEditorModal } from './admin_scoring_team.jsx';

export function ScoreEditorModal({ match, onClose, onSubmit, onSubmitAndNext, onAfterDecision, prevMatch, nextMatch, onPrev, onNext, password, selfReport, variant = "modal", canClose = true }) {
  const m = match;
  const isComplete = m.status === "completed";
  // Canonical team check (matches admin_pools.jsx and the lineup panel):
  // compKind OR a positive teamSize. A team competition created with only
  // teamSize set (compKind empty) must still route to TeamScoreEditorModal,
  // and pool-daihyosen rows (compMatches forces compKind="" AND teamSize=0)
  // correctly stay on the individual editor.
  const isTeam = m.compKind === "team" || (m.teamSize || 0) > 0;
  const teamSize = m.teamSize || 5;
  if (isTeam) return <TeamScoreEditorModal match={m} teamSize={teamSize} onClose={onClose} onSubmit={onSubmit} onSubmitAndNext={onSubmitAndNext} onAfterDecision={onAfterDecision} prevMatch={prevMatch} nextMatch={nextMatch} onPrev={onPrev} onNext={onNext} password={password} selfReport={selfReport} variant={variant} canClose={canClose} />;

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
  // Item 7: a non-kiken decision (fusenpai) routes through onAfterDecision when
  // the host page provides it (and this isn't a correction) so the court
  // advances to the next match — mirroring the Finish + Start Next flow. Hantei
  // advance is handled separately in submitHantei below. Kiken keeps the modal
  // open for RemainingMatchesPanel regardless.
  const submitDecision = makeSubmitDecision({
    match: m, enchoPeriodCount, password, mountedRef,
    setDecisionSubmitting, setDecisionErr, setWithdrawnPlayer, setDecisionPromptKind,
    onClose, onAfterDecision, isComplete, entityLabel: "competitors",
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
  //
  // Item 7: mirror the Finish button's isComplete ? onSubmit : onSubmitAndNext
  // choice so a hantei finish also advances to the next match on the same
  // court (when onSubmitAndNext is available and this isn't a correction).
  const submitHantei = (winnerSide) => {
    const winner = winnerSide === "a" ? m.sideA : m.sideB;
    const aFinal = aPts.filter(x => x !== "•").slice(0, MAX_IPPONS_PER_SIDE);
    const bFinal = bPts.filter(x => x !== "•").slice(0, MAX_IPPONS_PER_SIDE);
    const patch = {
      winner,
      ipponsA: aFinal,
      ipponsB: bFinal,
      hansokuA: aFouls,
      hansokuB: bFouls,
      status: "completed",
      ...enchoBlock(),
      decidedByHantei: true,
    };
    const submitFn = (!isComplete && onSubmitAndNext) ? onSubmitAndNext : onSubmit;
    return doSubmit(() => submitFn(patch));
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
              {m.compName} · {m.phase === "pool" ? window.poolLabel(m) : m.round}
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
            {canClose && <button className="btn btn--ghost btn--sm" onClick={handleDismiss} disabled={submitting} style={{ padding: "2px 8px" }}>✕ Close</button>}
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
                          <button key={i} className={`sb-slot ${s.pts[i] ? "sb-slot--filled" : ""}`} onClick={() => removePt(s.key, i)} disabled={decidedByHantei} title={decidedByHantei ? (initialDecidedByHantei ? "Locked — hantei already recorded" : "Hantei armed — choose a winner above, or cancel") : "Click to remove"}>
                            {s.pts[i] || "·"}
                          </button>
                        ))}
                      </div>
                      <div className="sb-points-grid">
                        {getIpponButtons(isNaginata).map((cc) => (
                          <button key={cc} className={`ipt-btn ${cc === "H" ? "ipt-btn--h" : ""}`} onClick={() => addPt(s.key, cc)} disabled={boutDecided || decidedByHantei}>{cc}</button>
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
                        <button
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
                doSubmit(() => onSubmit(patch));
              }}
              onCancel={() => setShowCorrectionPrompt(false)}
            />
          )}
          <div className="score-nav">
            {prevMatch ? (
              <button className="btn btn--sm score-nav__prev" onClick={onPrev} disabled={submitting} title={prevMatch.sideA?.name + " vs " + prevMatch.sideB?.name}>← Prev</button>
            ) : <span />}

            <div className="score-nav__actions">
              {m.status === "scheduled" && (
                <button className="btn btn--sm" onClick={() => doSubmit(() => onSubmit(buildPatch("running")))} disabled={submitting}>
                  ▶ Start match
                </button>
              )}
              {canClose && <button className="btn" onClick={handleDismiss} disabled={submitting}>Cancel</button>}
              {onSubmitAndNext ? (
                <button className={`btn btn--primary ${finishArmed && !isComplete ? "btn--confirm" : ""}`} onClick={() => {
                  if (isComplete && !correctionReason) { setShowCorrectionPrompt(true); return; }
                  if (!isComplete && !finishArmed) { setFinishArmed(true); return; }
                  doSubmit(() => (isComplete ? onSubmit : onSubmitAndNext)(buildPatch("completed")));
                }} disabled={submitting || !canFinish}>
                  {submitting ? "Saving…" : isComplete ? "Save correction" : finishArmed ? `Confirm · ${finishSummary} →` : "Finish + Start Next →"}
                </button>
              ) : (
                <button className={`btn btn--primary ${finishArmed && !isComplete ? "btn--confirm" : ""}`} onClick={() => {
                  if (isComplete && !correctionReason) { setShowCorrectionPrompt(true); return; }
                  if (!isComplete && !finishArmed) { setFinishArmed(true); return; }
                  doSubmit(() => onSubmit(buildPatch("completed")));
                }} disabled={submitting || !canFinish}>
                  {submitting ? "Saving…" : isComplete ? "Save correction" : finishArmed ? `Confirm · ${finishSummary}` : "Finish"}
                </button>
              )}
            </div>

            {nextMatch ? (
              <button className="btn btn--sm score-nav__next" onClick={onNext} disabled={submitting} title={nextMatch.sideA?.name + " vs " + nextMatch.sideB?.name}>Next →</button>
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
