// Individual match score editor (ScoreEditorModal).
// For team matches, ScoreEditorModal routes internally to TeamScoreEditorModal
// (imported from admin_scoring_team.jsx).
// Extracted from admin_scoring_modal.jsx (mp-zac3).

const { useState: useStateA, useEffect: useEffectA, useRef: useRefA } = React;

// Leaf module (no side effects): safe to ES-import. The DH label is
// daihyosen-specific; the rep pickers below stay gated on m.repIsTeam (a "-TB-"
// tiebreaker is also a rep bout, just not a daihyosen).
import { isPoolDaihyosenBout } from './pool_ids.jsx';
import { SideLabel } from './side_cell.jsx';
import { realIppons, hanteiTied, hanteiSlot, hanteiWinnerKey, sideSlotOrder } from './result_slot.jsx';
import { sameCompetitor } from './competitor_identity.jsx';
// Imported from the leaf, not read off `window`: this editor is ES-imported by
// its host and by unit tests that never load api_client, and write_result.jsx
// is import-only so it can be reached directly (see its header).
import { notLandedBanner } from './write_result.jsx';

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
  useAdoptFromServer,
  sideColorName,
  useMatchReopen,
  ReopenFeedback,
  RecordedWithdrawal,
  withdrawalInForce,
  withdrawnKeyOf,
  WithdrawalMarkedName,
  BarredMatchNotice,
} from './admin_scoring_shared.jsx';
// bc-cse: a SCHEDULED match a competitor is barred from must never offer a
// Start the server would refuse; isBarredMatch (ineligible_match.jsx) is the
// one owner of that question.
import { isBarredMatch } from './ineligible_match.jsx';

import { SyncStatusPill, useDebouncedRunningWrite } from './admin_scoring_autosave.jsx';

// isKoTieBlocked: import-only, from the team editor's shared module. bc-rawm
// reuses it here for the SAME tie rule (a knockout match cannot finish with
// no winner), never a re-derivation of it; see canFinish below.
import { TeamScoreEditorModal, isKoTieBlocked } from './admin_scoring_team.jsx';
import { EngiScoreEditorModal } from './admin_scoring_engi.jsx';

export function ScoreEditorModal({ match, onClose, onSubmit, onSubmitAndNext, onAfterDecision, onWithdrawal, onBoardChange, prevMatch, nextMatch, onPrev, onNext, password, selfReport, variant = "modal", canClose = true }) {
  const m = match;
  const isComplete = m.status === "completed";
  // Canonical team check (matches admin_pools.jsx and the lineup panel):
  // compKind OR a positive teamSize. A team competition created with only
  // teamSize set (compKind empty) must still route to TeamScoreEditorModal,
  // and pool-daihyosen rows (compMatches forces compKind="" AND teamSize=0)
  // correctly stay on the individual editor.
  const isTeam = m.compKind === "team" || m.teamSize > 0;
  const teamSize = m.teamSize || 5;

  // Seed from ipponsA/ipponsB: pool and bracket matches share this one wire
  // shape (scoreA/scoreB strings never appear), so every match arrives with
  // its ippon arrays already populated when it has recorded points.
  // Keep the score.type === "ippon" branch as the LAST resort: it serves the
  // quick-score paths that set only score.ippons. It must stay reachable when
  // neither source yields a point, so test emptiness explicitly rather than
  // relying on ||: an empty array is truthy and would swallow it.
  const cellsA = m.ipponsA || [];
  const cellsB = m.ipponsB || [];
  // realIppons, not a local copy: this file imports the leaf that owns "what
  // counts as a recorded ippon", and its own comments require these totals to
  // read the same rule as the scoreboard's hanteiTied.
  const cleanA = realIppons(cellsA);
  const cleanB = realIppons(cellsB);
  // The score.ippons FALLBACK is filtered like the primary path above: pts must
  // hold only REAL points, because the slot grid indexes it directly for
  // display while resultSlot picks the mark's slot from it. Any placeholder or
  // empty cell would desynchronise those two (a placeholder in cell 0 pushes
  // the mark to cell 1, where it renders OVER a recorded letter, hiding a
  // struck point). Cleaning once here keeps every downstream consumer honest.
  // bc-pnum: sameCompetitor, never a bare `winner?.id === side?.id` -- with
  // both sides id-less (buildPlayerMap/resolveSide keep id "" rather than
  // inventing one from the name), the naked equality made BOTH conditions
  // true and seeded the SAME score.ippons onto both sides at once (the
  // 4d602de2 regression class this seeding already guards elsewhere).
  const seedAPts = cleanA.length ? cleanA : (m.score?.type === "ippon" && sameCompetitor(m.winner, m.sideA) ? realIppons(m.score.ippons) : []);
  const seedBPts = cleanB.length ? cleanB : (m.score?.type === "ippon" && sameCompetitor(m.winner, m.sideB) ? realIppons(m.score.ippons) : []);

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
  const [aPts, setAPts] = useStateA(initialAPts);
  const [bPts, setBPts] = useStateA(initialBPts);
  const [aFouls, setAFouls] = useStateA(initialAFouls);
  const [bFouls, setBFouls] = useStateA(initialBFouls);
  const [enchoPeriodCount, setEnchoPeriodCount] = useStateA(initialEnchoPeriods);
  // The verdict the SERVER holds right now. A match has ONE result and every
  // surface asking for it must show the same one, and this editor is such a
  // surface: while it is open, the viewer card, the bracket, the TV board and
  // the Excel export are all already showing whatever this says.
  const hanteiRecorded = !!m.decidedByHantei;
  // FIK Art. 7-5 / 29-6: an encho match that remains tied is decided by
  // referee hantei. Persisting this on MatchResult so the UI / Excel can
  // mark it distinctly (vs an ippon-derived win).
  const [decidedByHantei, setDecidedByHantei] = useStateA(hanteiRecorded);
  // ADOPT a verdict recorded on another device, so this editor cannot sit
  // showing "Decide by hantei…" while every other surface shows the Ht. The
  // policy (unconditional, because keying on the VALUE already leaves a local
  // arm or cancel standing) lives in useAdoptFromServer; see that comment. It
  // touches the verdict only: aPts/bPts and the fouls are untouched, so an edit
  // in progress survives.
  //
  // The alternative — freeze at mount and stay silent about what you never saw
  // — trades the erase for a divergence, and a result that reads differently
  // depending on which screen you look at is the worse failure. Adopting means
  // an explicit `false` below is always the operator ruling on something in
  // front of them.
  useAdoptFromServer({
    signature: hanteiRecorded,
    apply: () => setDecidedByHantei(hanteiRecorded),
  });
  // Which side ("a"/"b"/"") holds a RECORDED hantei verdict, for the display
  // chip in the slot grid. Gated on the SERVER's verdict — not the local armed
  // state: arming a hantei on a reopened match must not resolve the stale
  // m.winner and pre-mark the previous winner before the operator has picked a
  // side. Empty when the winner is unattributable (same-name pair: mirror the
  // scoreboard, mark neither).
  // The tie gate is applied at the render site against the CURRENT pts.
  const recordedHtKey = hanteiRecorded ? hanteiWinnerKey(m) : "";
  const [submitting, setSubmitting] = useStateA(false);
  // F5: pending-write state: set when a terminal submit resolves { queued:true }
  // (offline / transient failure). While pending the modal stays open and shows a
  // sticky "Not saved yet" banner. Cleared when the queue drains for this match
  // (subscribeSyncStatus + hasPendingTerminalWrite). pendingFn holds the last
  // terminal submit closure so "Retry now" can re-invoke it directly.
  const [pendingWrite, setPendingWrite] = useStateA(false);
  const pendingFnRef = useRefA(null);
  // F5: set when a queued terminal write is PERMANENTLY rejected (non-retryable
  // 4xx on retry): the write never landed, so we must show an explicit "not
  // saved" failure state rather than let the pending banner clear to "saved".
  const [writeFailed, setWriteFailed] = useStateA(null); // { reason, advice? } | null
  // Naginata competitions add an extra "S" (Sune) ippon button.
  // Fetched from the competition config on open.
  const [isNaginata, setIsNaginata] = useStateA(false);
  // Engi competitions use flag-count scoring; dispatched to EngiScoreEditorModal.
  // Derived synchronously from m.compEngi (stamped at enrichment time by
  // compMatches / enrichPoolMatchWithComp / scoringMatch): eliminates the
  // async-fetch flash where the kendo editor briefly shows before switching
  // to the engi editor once the competition-config fetch lands.
  const isEngi = !!m.compEngi;
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
  // bc-htcr: the write the open ReasonPrompt confirms, handed the reason.
  // null means the default correction (Save correction, Enter: the
  // buildPatch("completed") write); submitHantei sets its own, because a
  // hantei verdict is not a buildPatch write. Every opener goes through
  // askCorrectionReason, so a pending action never outlives its prompt.
  const correctionActionRef = useRefA(null);
  const askCorrectionReason = (action = null) => {
    correctionActionRef.current = action;
    setShowCorrectionPrompt(true);
  };
  // mp-62vr: for a team daihyosen/tiebreaker rep bout the sides are TEAM names;
  // the operator picks which player each team fields from its roster. repPlayerA
  // = Aka (sideA), repPlayerB = Shiro (sideB). Only rendered when m.repIsTeam.
  const [repPlayerA, setRepPlayerA] = useStateA(m.repPlayerA || "");
  const [repPlayerB, setRepPlayerB] = useStateA(m.repPlayerB || "");
  // Follow a pick made on another device, same rule as every other channel.
  //
  // The narrow reading first, because it bounds what this is worth: an UNSET
  // dropdown was never the danger. repBlock rides every write, but the server
  // preserves these on empty (backfillMatchIdentity, engine/scoring.go), so an
  // editor that mounted before anyone picked sends "" and wipes nothing. What
  // it does NOT protect is a mount-time value that has since been CHANGED
  // elsewhere: "an explicit value in result always wins", so this editor would
  // put its stale name back. That is the case this closes.
  useAdoptFromServer({
    signature: JSON.stringify([m.repPlayerA || "", m.repPlayerB || ""]),
    apply: () => { setRepPlayerA(m.repPlayerA || ""); setRepPlayerB(m.repPlayerB || ""); },
  });
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
  const { markDirty: markScoringDirty, cancelDebounce: cancelScoringDebounce, flushPending: flushScoringAutosave } = useDebouncedRunningWrite({
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
  //   the decision is kiken: in that case we keep the modal open and surface
  //   the RemainingMatchesPanel so the operator can chain default-win awards.
  // - T103/CHK024: when the server replies 409 decision_locked (the
  //   prior kiken on this match can't be safely overwritten because a
  //   subsequent match for either side has started), prompt the
  //   operator to confirm and re-send with force=true.
  // Shared factory (admin_scoring_shared.jsx): the individual + team modals had
  // byte-identical copies; "competitors" is the only per-modal wording.
  // Item 7: a non-kiken decision (fusenpai) routes through onAfterDecision when
  // the host page provides it (and this isn't a correction) so the court
  // advances to the next match: mirroring the Finish + Start Next flow. Hantei
  // advance is handled separately in submitHantei below. Kiken keeps the modal
  // open for RemainingMatchesPanel regardless.
  const submitDecision = makeSubmitDecision({
    match: m, enchoPeriodCount, password, mountedRef,
    setDecisionSubmitting, setDecisionErr, setWithdrawnPlayer, setDecisionPromptKind,
    onClose, onAfterDecision, onWithdrawal, isComplete, entityLabel: "competitors",
    // F5: thread pending-write handles so the factory can show the sticky banner
    // when the decision write is only queued (offline / transient failure).
    setPendingWrite, pendingFnRef,
  });
  // bc-tmfn: Clear withdrawal and reopen, the same door and component the team
  // editor uses (RecordedWithdrawal / useMatchReopen, admin_scoring_shared.jsx).
  // A correction here keeps a recorded withdrawal (it has no way to state a
  // decision), so removing one recorded by mistake is a reopen: the match goes
  // back to running with the letters the withdrawing side struck. The editor
  // STAYS OPEN and follows the match to running in place, so
  // the operator scores the rest here, as the consequence text tells them,
  // and the ReopenFeedback in the footer can still show what else the reopen
  // reopened. Called unconditionally (rules of hooks: the team and engi
  // dispatch below returns after every hook).
  const recordedWithdrawal = withdrawalInForce(m);
  const reopenCtl = useMatchReopen({ match: m, password, isComplete });
  // While that withdrawal is in force, the WINNER's side holds the default-win
  // maru, which is the ruling itself: Save correction keeps it whatever is
  // sent (engine keptWithdrawalScoreline) and only the withdrawing side's
  // letters are the operator's to correct. So the winner's slots are
  // read-only (lockedKey) and the decided state is the withdrawer's alone,
  // capped one short of a decided bout: a side with two points has won the
  // bout, so it cannot be the side that withdrew (and the server refuses the
  // 2-2 that two letters against two maru would make). "" when no withdrawal
  // is in force, or the ruling does not say who withdrew.
  const withdrawnKey = recordedWithdrawal ? withdrawnKeyOf(m) : "";
  const lockedKey = withdrawnKey === "a" ? "b" : withdrawnKey === "b" ? "a" : "";
  const sideCap = (side) => (side === lockedKey ? 0 : lockedKey ? MAX_IPPONS_PER_SIDE - 1 : MAX_IPPONS_PER_SIDE);

  // Hansoku Hs are now physically present in the opponent's pts array
  // (folded in at the 2-foul boundary by applyFoulIncrement). The counter
  // is "outstanding fouls": no derived addends needed.
  // Counted through the shared realIppons filter (drops empties AND the "•"
  // placeholder): the raw score.ippons seed path can inject either, and no
  // entry path ever adds "" as a point, so every consumer of these totals -
  // the hantei gates, winner derivation, buildPatch - reads the same rule as
  // the scoreboard's hanteiTied.
  const aTotal = realIppons(aPts).length;
  const bTotal = realIppons(bPts).length;

  const addPt = (side, letter) => {
    // No-op when the side is already at the 2-ippon max: don't mark dirty or
    // schedule an autosave PUT / SSE fan-out for a tap that changes nothing.
    // sideCap: 2 normally; under a recorded withdrawal 0 for the winner's
    // read-only maru and 1 for the withdrawer (see lockedKey above). The
    // keyboard path reaches here without the render's disabled buttons.
    const cap = sideCap(side);
    const cur = side === "a" ? aPts : bPts;
    if (cur.length >= cap) return; // fast no-op path: don't mark dirty / autosave
    // The functional updater re-checks the cap against the AUTHORITATIVE current
    // state (p), not the render-closure cur: so the 2-ippon invariant holds even
    // if addPt is called twice before a re-render (React batching / rapid taps).
    if (side === "a") setAPts((p) => p.length < cap ? [...p, letter] : p);
    else setBPts((p) => p.length < cap ? [...p, letter] : p);
    markScoringDirty(); // C1: trigger debounced autosave
  };
  const removePt = (side, idx) => {
    // Mirror of addPt's no-op guard above, for the same reason. An UNFILLED slot
    // is still an ENABLED button -- the grid disables only on decidedByHantei, and the
    // slot's own aria-label announces it as "empty" -- so a stray tap used to
    // filter nothing out, mark dirty anyway, and 300ms later send a full
    // running-match PUT stamped NOW. That write can beat another device's
    // correctly-entered result still sitting in its offline queue (stamped at
    // enqueue, so OLDER) on ApplyByTimestamp, and that operator is then told
    // {"applied": false, "reason": "superseded"} and not to re-enter -- all from
    // a tap that changed nothing.
    const cur = side === "a" ? aPts : bPts;
    if (cur[idx] === undefined) return; // fast no-op path: don't mark dirty / autosave
    if (side === lockedKey) return; // the recorded default-win maru is not the operator's to remove
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
  //
  // "The server holds a verdict and this editor is showing none" — which, given
  // the adopt effect above, means the operator pressed Cancel on a verdict that
  // was on screen. An explicit false is an authoritative "there is no verdict",
  // and that is exactly who should be allowed to say it. There is no
  // mounted-before-the-verdict blind spot to guard any more: the editor adopts
  // what the server holds, so it can never rule on something it did not show.
  const hanteiClear = hanteiRecorded && !decidedByHantei ? { decidedByHantei: false } : {};

  // mp-62vr: carry the rep-player names on every write for a team rep bout so
  // the per-court display can show who fought. backfillMatchIdentity preserves
  // them on empty server-side, so an unset dropdown won't wipe a prior pick.
  const repBlock = m.repIsTeam ? { repPlayerA, repPlayerB } : {};

  const buildPatch = (targetStatus) => {
    const fouls = { a: aFouls, b: bFouls };
    if (targetStatus === "scheduled") return { winner: null, status: "scheduled", score: null, ipponsA: [], ipponsB: [], hansokuA: 0, hansokuB: 0, ...hanteiClear, ...repBlock };
    if (targetStatus === "running") return {
      status: "running", winner: null,
      ipponsA: realIppons(aPts), ipponsB: realIppons(bPts),
      hansokuA: aFouls, hansokuB: bFouls,
      score: { type: "ippon", winnerPts: aTotal, loserPts: bTotal, ippons: aPts, fouls, live: true, corrected: isComplete },
      ...enchoBlock(), ...hanteiClear, ...repBlock,
    };
    const correctionBlock = isComplete && correctionReason ? { correctionReason } : {};
    if (isDrawToggled) return { winner: null, ipponsA: [], ipponsB: [], hansokuA: aFouls, hansokuB: bFouls, status: "completed", score: { type: "hikiwake", winnerPts: 0, loserPts: 0, fouls, corrected: isComplete }, ...enchoBlock(), ...hanteiClear, ...correctionBlock, ...repBlock };
    // ippon. Hansoku Hs are already physically present in the pts arrays
    // (folded in by applyFoulIncrement at the 2-foul boundary), so no
    // additional H fold is needed here.
    const aLetters = realIppons(aPts);
    const bLetters = realIppons(bPts);
    const aFinal = aLetters.slice(0, MAX_IPPONS_PER_SIDE);
    const bFinal = bLetters.slice(0, MAX_IPPONS_PER_SIDE);
    const winnerSide = aFinal.length > bFinal.length ? "a" : bFinal.length > aFinal.length ? "b" : null;
    if (!winnerSide) return { winner: null, ipponsA: aFinal, ipponsB: bFinal, hansokuA: aFouls, hansokuB: bFouls, status: "completed", score: { type: "hikiwake", winnerPts: 0, loserPts: 0, fouls, corrected: isComplete }, ...enchoBlock(), ...hanteiClear, ...correctionBlock, ...repBlock };
    const winner = winnerSide === "a" ? m.sideA : m.sideB;
    const ippons = winnerSide === "a" ? aFinal : bFinal;
    return { winner, ipponsA: aFinal, ipponsB: bFinal, hansokuA: aFouls, hansokuB: bFouls, status: "completed", score: { type: "ippon", winnerPts: ippons.length, loserPts: (winnerSide === "a" ? bFinal : aFinal).length, ippons, fouls, corrected: isComplete }, ...enchoBlock(), ...hanteiClear, ...correctionBlock, ...repBlock };
  };
  // C1: keep autosave refs fresh with the latest buildPatch / onSubmit /
  // running-status so the debounce callback never reads a stale closure.
  _autosaveIsRunningRef.current = m.status === "running";
  _autosaveBuildPatchRef.current = buildPatch;
  _autosaveOnSubmitRef.current = onSubmit;

  // Hantei submit: tied scoreline (with or without encho). Operator picks a
  // side; we send a completed patch with the chosen side as winner, the
  // *entered* ippon arrays preserved (so a 1–1 score stays visible alongside the Ht
  // marker: clearing them would lose the tied score history that the
  // viewer/Excel renderers display under the hantei suffix), and the
  // decidedByHantei flag set. This is a dedicated affordance because the
  // regular flow assumes an ippon-derived win.
  //
  // Item 7: mirror the Finish button's isComplete ? onSubmit: onSubmitAndNext
  // choice so a hantei finish also advances to the next match on the same
  // court (when onSubmitAndNext is available and this isn't a correction).
  const submitHantei = (winnerSide) => {
    const winner = winnerSide === "a" ? m.sideA : m.sideB;
    const aFinal = realIppons(aPts).slice(0, MAX_IPPONS_PER_SIDE);
    const bFinal = realIppons(bPts).slice(0, MAX_IPPONS_PER_SIDE);
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
    // bc-htcr: a hantei verdict on a completed match is a correction like
    // any other, so it carries the audit reason the server requires, and
    // asks for one first when none has been given, exactly as Save
    // correction does. Without this the write was refused and the wrong
    // verdict stood, with no other way to change it (Save correction is off
    // while the hantei is set).
    if (isComplete && !correctionReason) {
      askCorrectionReason((r) => doSubmit(() => onSubmit({ ...patch, correctionReason: r })));
      return undefined;
    }
    if (isComplete) patch.correctionReason = correctionReason;
    const submitFn = (!isComplete && onSubmitAndNext) ? onSubmitAndNext : onSubmit;
    return doSubmit(() => submitFn(patch));
  };

  const doSubmit = async (fn) => {
    cancelScoringDebounce(); // C1: cancel any pending autosave before explicit submit
    setSubmitting(true);
    // Clear any prior pending/failed state when the operator explicitly retries.
    if (mountedRef.current) { setPendingWrite(false); setWriteFailed(null); }
    let res;
    try {
      res = await fn();
    } finally {
      if (mountedRef.current) setSubmitting(false);
    }
    // F5: if the terminal write was only queued (offline / transient), do NOT
    // close or advance. Instead enter pending-write mode: show the sticky banner
    // and remember the submit closure so "Retry now" can re-invoke it.
    if (res && res.queued) {
      if (mountedRef.current) {
        setPendingWrite(true);
        pendingFnRef.current = fn;
      }
    }
    return res;
  };

  // F5 (re-open hydration): if a terminal write for THIS match is still queued
  // (operator finished offline, closed the editor, then reopened before the
  // queue drained), surface the pending banner on mount. The submit closure
  // can't be recovered from the serialized queue, so "Retry now" stays disabled
  // until the operator re-submits; the queue keeps auto-retrying meanwhile.
  useEffectA(() => {
    if (!m.compId || !m.id) return;
    if (window.API && typeof window.API.hasPendingTerminalWrite === 'function'
        && window.API.hasPendingTerminalWrite(m.compId, m.id)) {
      setPendingWrite(true);
    }
  }, [m.compId, m.id]);

  // F5: subscribe to sync-status so the pending-write banner auto-clears once
  // the queue drains for this match. Mounted once; reads match identity via
  // closure. Uses mountedRef guard so setState never fires after unmount.
  useEffectA(() => {
    if (!m.compId || !m.id) return;
    // Guard the window globals: in unit/render tests (and during boot ordering)
    // subscribeSyncStatus / API can be absent: mirror SyncStatusPill's guard so
    // the modal never throws on mount.
    if (typeof window.subscribeSyncStatus !== 'function') return;
    const unsub = window.subscribeSyncStatus((status) => {
      if (!mountedRef.current) return;
      const stillPending = (window.API && typeof window.API.hasPendingTerminalWrite === 'function')
        ? window.API.hasPendingTerminalWrite(m.compId, m.id)
        : false;
      if (status === 'synced' && !stillPending) {
        setPendingWrite(false);
        pendingFnRef.current = null;
      }
    });
    return unsub;
  }, [m.compId, m.id]);

  // F5: surface a PERMANENT terminal-write failure (non-retryable 4xx on a queued
  // retry) as an explicit "not saved" state: otherwise the write is silently
  // dropped and the pending banner clears to look saved. Guarded like above.
  useEffectA(() => {
    if (!m.compId || !m.id) return;
    if (typeof window.subscribeTerminalWriteFailed !== 'function') return;
    const unsub = window.subscribeTerminalWriteFailed((info) => {
      if (!mountedRef.current) return;
      if (!info || info.compID !== m.compId || info.matchID !== m.id) return;
      setWriteFailed({ reason: info.reason || `save rejected (${info.status || 'error'})`, advice: info.advice });
      setPendingWrite(false); // the queued write is gone: it failed, not pending
      // DISARM the finish confirmation. The submit that just failed left the
      // button in its "Tap again to finish" state, so the operator was one tap
      // from re-sending -- which for a superseded write is the one thing the
      // banner tells them not to do, and which would WIN, because a re-submit
      // carries a fresh stamp and beats the newer result it overwrites.
      // Re-arming has to be deliberate.
      setFinishArmed(false);
    });
    return unsub;
  }, [m.compId, m.id]);

  // Draw detection: check both the score.type (when present) and the
  // top-level decision string. Either being "hikiwake" means draw.
  const initialIsDrawToggled = window.isHikiwake(m.score?.type) || window.isHikiwake(m.decision);
  const [isDrawToggled, setIsDrawToggled] = useStateA(initialIsDrawToggled);

  // RE-SEED the whole scoring state when the stored result moves and the
  // operator has nothing unsaved — the score half of the same rule the verdict
  // adopt effect implements. Every ippon slot, foul counter and overtime count
  // above is local state seeded at MOUNT, so without this an editor left open
  // kept showing the scoreline it opened with while the list behind it, the
  // viewer, the board and the export all moved on. A verdict adopted onto a
  // stale scoreline is worse than either alone: the Ht lands in the slot the
  // OLD score left free, so the editor showed `Ht` at 0-0 against a stored 1-1.
  //
  // Keyed on a signature of the SERVER's result, so it fires when that changes
  // and not on every SSE re-render. The keep-local-edits policy and the
  // previous-render dirty read both live in useAdoptFromServer now; the call
  // site is below isDirty, which is what it needs.
  //
  // Be precise about what happens if they then save over a newer result.
  // Timestamp last-write-wins (mp-y3nk) now covers EVERY match, pool and
  // knockout alike, through engine.applyMatchWrite: a write stamped older than
  // the stored result is dropped, so an editor that sat through an outage
  // cannot bury a result recorded meanwhile. That is a floor, not a resolution
  // protocol. Concurrent editors are still deliberately last-write-wins
  // (handlers_match.go says so), the guard only orders writes that carry
  // stamps, and the `stale: true` response covers a narrower case again (a
  // lower Rev from the SAME session, or a running write arriving after
  // completion).
  //
  // So this re-seed is still doing the load-bearing work: it removes the
  // ARTIFICIAL conflicts, where an editor holding a mount-time snapshot wrote
  // back state it was never showing. What remains is a genuine disagreement
  // between two people who both typed something, which is theirs to resolve.
  // The signature is the tuple the effect WRITES, not the `m` fields it reads.
  // Those two lists had already drifted apart: `m.status` fed no setter (so a
  // status-only broadcast triggered a no-op re-seed), while the bracket-match
  // fallbacks `m.ipponsA`/`m.ipponsB`, `m.score.ippons`,
  // `m.score.fouls` and `m.winner?.id` all feed one and were absent — so a
  // bracket match whose score moved elsewhere did not re-seed at all. Keying on
  // the seeds themselves cannot drift: whatever the derivation above starts
  // reading is automatically part of the key.
  const serverScoreSig = JSON.stringify([
    initialAPts, initialBPts, initialAFouls, initialBFouls,
    initialEnchoPeriods, initialIsDrawToggled,
  ]);
  const applyServerScore = () => {
    setAPts(initialAPts);
    setBPts(initialBPts);
    setAFouls(initialAFouls);
    setBFouls(initialBFouls);
    setEnchoPeriodCount(initialEnchoPeriods);
    setIsDrawToggled(initialIsDrawToggled);
  };

  // Arranged as [left, right]: left is always SHIRO (White), right is always AKA (Red).
  // onIncrement applies the FIK 2-foul auto-award rule via applyFoulIncrement:
  // every 2nd foul on this side discharges into a hansoku ippon ("H") for
  // the OPPONENT and resets this side's counter to 0.
  // slotButtons: one side's two ippon slots. Display parity with the shared
  // scoreboard and the team editor: a RECORDED hantei shows Ht in the winner's
  // free slot via the same hanteiSlot the team editor uses (delegating to
  // resultSlot, the one owner of which-slot). Gated on the LIVE decidedByHantei
  // too, so cancelling the recorded verdict clears the chip with it, and on the
  // shared tie rule (aTotal/bTotal count through realIppons, like the
  // scoreboard's hanteiTied): a drifted decidedByHantei on an untied line
  // shows letters plainly here too. Display only: the slots are disabled while
  // the hantei stands.
  const slotButtons = (s) => {
    // resultSlot gets the SAME array the render loop below indexes. Passing a
    // filtered view instead desynchronises them: on pts ["•","M"] the filter
    // yields ["M"] → slot 1, and cell 1 then renders "Ht" OVER the recorded
    // men. pts is kept clean at the seed instead (seedAPts/seedBPts run both
    // the primary cells AND the score.ippons fallback through realIppons), so
    // raw and filtered agree here and the mark lands in a genuinely free cell.
    const htSlot = hanteiSlot(
      decidedByHantei && hanteiTied(aPts, bPts) && recordedHtKey === s.key, s.pts);
    // sideSlotOrder: the same visual mirror the read-only scoreboard and the
    // team editor apply, so DOM order is visual order and no CSS mirror is
    // needed here any more (result_slot.jsx owns the rule).
    // The winner's slots while a recorded withdrawal is in force: the maru
    // shows, read-only (see lockedKey).
    const locked = s.key === lockedKey;
    return sideSlotOrder(s.color).map((i, ordinal) => {
      const isHt = htSlot === i;
      // The spoken ordinal counts in READING order (ordinal), not by the
      // array index (i). Those differ on Aka, whose slots are mirrored:
      // labelling by index made Aka announce "slot 2" then "slot 1" while
      // Shiro announced "slot 1" then "slot 2", so the two sides counted
      // opposite ways through the same control. `i` stays the identity for
      // key/removePt, and "slot 0 = outer" remains the convention in code
      // comments and tests -- this is the user-facing number only.
      return (
        <button
          key={i}
          className={`sb-slot ${(isHt || s.pts[i]) ? "sb-slot--filled" : ""}`}
          onClick={() => removePt(s.key, i)}
          disabled={decidedByHantei || locked}
          title={locked ? "Default win recorded with the withdrawal" : decidedByHantei ? (hanteiRecorded ? "Locked: hantei already recorded" : "Hantei armed: choose a winner above, or cancel") : "Click to remove"}
          aria-label={`${sideColorName(s.color)} slot ${ordinal + 1}: ${isHt ? "Ht" : (s.pts[i] ? (locked ? `${s.pts[i]}, default win` : `remove ${s.pts[i]}`) : "empty")}`}
        >
          {isHt ? "Ht" : (s.pts[i] || "\u00b7")}
        </button>
      );
    });
  };

  const sides = [
    {
      key: "b", name: m.sideB?.name, dojo: m.sideB?.dojo, number: m.sideB?.number, pts: bPts, fouls: bFouls,
      setFouls: (v) => { setBFouls(v); markScoringDirty(); }, // C1
      onIncrement: () => {
        const r = applyFoulIncrement(bFouls, aPts, bPts);
        setBFouls(r.fouls);
        setAPts(r.opponentPts);
        markScoringDirty(); // C1
      },
      color: "shiro",
    },
    {
      key: "a", name: m.sideA?.name, dojo: m.sideA?.dojo, number: m.sideA?.number, pts: aPts, fouls: aFouls,
      setFouls: (v) => { setAFouls(v); markScoringDirty(); }, // C1
      onIncrement: () => {
        const r = applyFoulIncrement(aFouls, bPts, aPts);
        setAFouls(r.fouls);
        setBPts(r.opponentPts);
        markScoringDirty(); // C1
      },
      color: "aka",
    },
  ];

  // Bout is decided once either side reaches 2 ippons: disable add-ippon
  // buttons on BOTH sides (mirrors validateIppons on the server). Under a
  // recorded withdrawal the winner's maru does not count: only the
  // withdrawer's letters are editable, up to their cap (sideCap, lockedKey).
  const withdrawerPts = lockedKey === "a" ? bPts : lockedKey === "b" ? aPts : null;
  const boutDecided = withdrawerPts
    ? withdrawerPts.length >= sideCap(withdrawnKey)
    : isBoutDecided(aPts, bPts);
  // Marks a tap can clear: under a recorded withdrawal, the withdrawer's only.
  const clearableMarks = withdrawerPts ? realIppons(withdrawerPts).length : aTotal + bTotal;

  // While hantei is armed the operator must commit via the dedicated SHIRO /
  // AKA buttons (which route through submitHantei). Disable the regular
  // Finish/Enter so the patch can't accidentally mark an ippon-decided match
  // as hantei-decided. Keyboard Enter is also gated on canFinish.
  // A knockout match can't end in a draw (it's decided by encho then hantei),
  // so the hikiwake toggle is suppressed in the bracket phase: m.phase ===
  // "bracket" is the in-modal KO signal (see TeamScoreEditorModal).
  const isKnockoutPhase = m.phase === "bracket";
  // bc-rawm: a tied individual knockout bout (0 encho periods or several,
  // 1-1 or any other equal score) has no winner, and the server refuses it
  // outright -- validateBracketCompletion runs on a Finish AND on a later
  // Save correction alike -- so this must block BOTH, not just the first
  // Finish. That is why isComplete is always passed false below, unlike the
  // team editor's own isKoTieBlocked call: a team tie is broken by an
  // appended daihyosen bout, so a COMPLETED team encounter never carries a
  // null teamWinner and isKoTieBlocked's isComplete exemption is safe there;
  // an individual bout has no daihyosen, so the identical tie can recur
  // under a correction (the operator removes a point and the score is tied
  // again) and must stay blocked there too. A recorded withdrawal
  // (lockedKey) already has a winner regardless of the scoreline, so it is
  // exempt from the tie check, exactly as hantei is (decidedByHantei already
  // disables Finish below; guarded out of koTieBlocked too so the button's
  // label/title do not claim "needs a winner" while the hantei picker, the
  // actual remedy, is on screen).
  const individualWinner = lockedKey || (aTotal === bTotal ? null : (aTotal > bTotal ? "a" : "b"));
  // bc-cse: hasPointsOrDraw gates koTieBlocked too, not just canFinish -- at
  // a pristine 0-0 (or a still-SCHEDULED match, which reads 0-0 the same
  // way) aTotal===bTotal is true by construction, so without this the
  // button read "Needs a winner" on every knockout bout before anything was
  // struck, including a scheduled barred match nobody has opened yet. The
  // button was already disabled there via canFinish; only the WORDING was
  // wrong, claiming a tie that never happened.
  const hasPointsOrDraw = isDrawToggled || aTotal > 0 || bTotal > 0;
  const koTieBlocked = !decidedByHantei && m.status !== "scheduled" && hasPointsOrDraw &&
    isKoTieBlocked({ isKnockoutPhase, teamWinner: individualWinner, isComplete: false });
  const KO_TIE_REASON = "Needs a winner: fight encho, then record hantei if still tied.";
  const canFinish = !decidedByHantei && !koTieBlocked && hasPointsOrDraw;

  // Finish guard (see TeamScoreEditorModal): one tap ARMS the button — its label
  // becomes an explicit "Tap again to finish" INSTRUCTION (not a verdict), so the
  // two-tap requirement is stated rather than inferred from a colour flip — and a
  // second tap commits. Disarms on any score change so a stale result can't be
  // confirmed. Keyboard Enter stays direct (deliberate, not an accidental tablet
  // brush). The result itself is verified from the score slots above, not the
  // button; a lossy "SHIRO WIN 1–0" caption is not a check.
  const [finishArmed, setFinishArmed] = useStateA(false);
  useEffectA(() => { setFinishArmed(false); }, [aTotal, bTotal, isDrawToggled]);

  // "Has the OPERATOR changed anything", which gates the discard prompt — so
  // the verdict term compares against what the SERVER holds, not against a
  // mount-time snapshot. Adopting a verdict recorded elsewhere moves both sides
  // of that comparison together and is therefore not dirty, which is right: it
  // is not an unsaved change of theirs, and prompting "discard unsaved scoring
  // changes?" on an editor nobody touched trains operators to dismiss the one
  // prompt that protects real work.
  //
  // scoringDirty is what closing a RUNNING match may flush (bc-dscn): isDirty
  // without the hantei ARM, which is a mode rather than a result. Flushing on
  // an arm alone would send a freshly stamped write with an unchanged
  // scoreline, which could win last-write-wins over another device's older
  // queued result. Withdrawing a RECORDED verdict is a result, not the arm,
  // and buildPatch carries it (hanteiClear), so that term stays.
  const scoringDirty =
    !window.arraysEqual(aPts, initialAPts) ||
    !window.arraysEqual(bPts, initialBPts) ||
    aFouls !== initialAFouls ||
    bFouls !== initialBFouls ||
    isDrawToggled !== initialIsDrawToggled ||
    enchoPeriodCount !== initialEnchoPeriods ||
    (hanteiRecorded && !decidedByHantei);
  const isDirty = scoringDirty || decidedByHantei !== hanteiRecorded;
  // The scoreline half of the same rule, declared HERE because the hook needs
  // isDirty: it reads the value from the render BEFORE the server change (see
  // useAdoptFromServer). It self-corrects: a re-seed makes the next render's
  // isDirty false again.
  useAdoptFromServer({
    signature: serverScoreSig,
    apply: applyServerScore,
    keepLocalEdits: true,
    isDirty,
  });
  // ...except when the RULING moves under the board (lockedKey changes): a
  // cleared withdrawal (Clear withdrawal and reopen, which keeps this editor
  // open) or one moved to the other side. Unsaved taps were made against the
  // old ruling, and keeping them would keep the default-win maru on a side
  // that is no longer locked: tappable, and sent as two points by the next
  // autosave onto a match with no withdrawal left to explain them. This is
  // not the peer disagreement keepLocalEdits protects; the operator's own
  // reopen moved the server, so the board re-seeds from it.
  const lockedKeyRef = useRefA(lockedKey);
  useEffectA(() => {
    if (lockedKeyRef.current === lockedKey) return;
    lockedKeyRef.current = lockedKey;
    applyServerScore();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [lockedKey]);
  const handleDismiss = async () => {
    // Don't close while any save/decision request is in flight: letting
    // the modal unmount would orphan the pending fetch and lose the
    // setState landing.
    if (submitting || decisionSubmitting) return;
    // bc-dscn: a host that cannot close (the inline court console) has nothing
    // to discard INTO, so it never prompts either.
    if (!canClose) return;
    // bc-dscn: on a RUNNING match every scoring edit is autosaved, so closing
    // discards nothing: save any edit still inside the debounce window now and
    // close without asking. Two local states are NOT in buildPatch("running")
    // and so are not saved by it: the hantei ARM, which is only a mode (the
    // verdict is committed by the side buttons, submitHantei) and is dropped
    // with no write (scoringDirty excludes it), and the hikiwake toggle, which
    // is a result the operator entered; that one keeps the prompt below,
    // because closing would lose it.
    if (m.status === "running" && isDrawToggled === initialIsDrawToggled) {
      if (scoringDirty) flushScoringAutosave();
      onClose();
      return;
    }
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
  //   Esc              → close the modal (respects dirty-state confirm) where the host
  //                      can close; a running match saves and closes without asking
  // Scoring shortcuts (Enter/M/K/D/T/H/X, plus S in Naginata) are skipped when any interactive
  // element (input, button, link, …) has focus so native activation still works.
  const kbRef = React.useRef(null);
  kbRef.current = { delegated: isTeam || isEngi, submitting, canFinish, isDrawToggled, isKnockoutPhase, aTotal, bTotal, handleDismiss, canClose, onPrev, onNext, prevMatch, nextMatch, onSubmit, onSubmitAndNext, buildPatch, addPt, doSubmit, isNaginata, decidedByHantei, isComplete, correctionReason, askCorrectionReason, markScoringDirty, cancelScoringDebounce };

  useEffectA(() => {
    const onKeyDown = (ev) => {
      const s = kbRef.current;
      // A team or engi match renders its own editor below, which owns the
      // keyboard. This listener is registered anyway (hooks run before the
      // dispatch), and acting here too closed a team or engi editor on Esc past
      // its own discard prompt, and turned a point key on a team match into a
      // phantom individual-shaped running write (match-level ippons, no
      // subResults).
      if (s.delegated) return;
      if (s.submitting) return;
      if (ev.ctrlKey || ev.metaKey || ev.altKey) return;

      // Esc routes through handleDismiss so the dirty-state confirm still fires.
      // bc-dscn: only where the host can close. On the inline court console
      // Esc belongs to whatever has focus (e.g. an open fighter list), so it
      // is left unhandled and not prevented.
      if (ev.key === "Escape") { if (!s.canClose) return; ev.preventDefault(); s.handleDismiss(); return; }

      // Navigation blocked only inside text-entry elements (preserves cursor movement)
      if (!window.isTextEntry(ev.target)) {
        // Keyed on the neighbour match as well as the callback: the Scores tab
        // wires onPrev/onNext unconditionally, and with no neighbour they
        // call scoreKeyOf(null), which throws. Same condition as the nav
        // buttons and the shortcut hint's hasNav.
        if (ev.key === "ArrowLeft" && s.onPrev && s.prevMatch) { ev.preventDefault(); s.onPrev(); return; }
        if (ev.key === "ArrowRight" && s.onNext && s.nextMatch) { ev.preventDefault(); s.onNext(); return; }
      }

      // Scoring shortcuts blocked when any interactive element has focus
      if (window.isInteractiveTarget(ev.target)) return;

      if (ev.key === "Enter" && s.canFinish) {
        ev.preventDefault();
        if (s.isComplete && !s.correctionReason) {
          s.askCorrectionReason();
          return;
        }
        const patch = s.buildPatch("completed");
        // A correction (completed match) saves the current match only: never
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
        // No hikiwake in a knockout match: leave the key inert there.
        if (s.isKnockoutPhase && !s.isDrawToggled) return;
        const r = decideDrawToggle({ isDrawToggled: s.isDrawToggled, aTotal: s.aTotal, bTotal: s.bTotal });
        if (r.action === "cancel") { setIsDrawToggled(false); s.markScoringDirty(); } // C1
        else if (r.action === "enter") { setIsDrawToggled(true); setAPts([]); setBPts([]); s.markScoringDirty(); } // C1
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, []); // listener registered once; reads fresh state via kbRef

  // bc-sbq: report what is on this board to a host that must say what a
  // requeue would discard (the shiaijo console's Send back to queue). The
  // host's court feed lags this editor, since an autosaved mark reaches it
  // only on the next refetch, so a confirm built from the feed alone told
  // the operator "nothing will be lost" with a point on the board. Keyed on
  // the VALUES, so a refetch that re-creates `m` reports nothing new. The
  // team editor reports its own board; engi reports none.
  const boardPoints = realIppons(aPts).length + realIppons(bPts).length;
  const boardFouls = aFouls + bFouls;
  const boardOvertime = enchoPeriodCount > 0;
  useEffectA(() => {
    if (typeof onBoardChange !== "function" || isTeam || isEngi) return;
    onBoardChange({ compId: m.compId, matchId: m.id, points: boardPoints, fouls: boardFouls, overtime: boardOvertime, draw: isDrawToggled, bouts: 0 });
  }, [m.compId, m.id, boardPoints, boardFouls, boardOvertime, isDrawToggled, isTeam, isEngi, onBoardChange]);

  // Engi competition: delegate to the flag-count editor. This check runs after
  // all hooks so React's rules-of-hooks are satisfied. isEngi is derived
  // synchronously from m.compEngi (stamped at enrichment time), so the engi
  // editor is shown from the first render with no kendo-editor flash.
  // The team check is skipped for engi (engi is never a team).
  if (isEngi) {
    return <EngiScoreEditorModal match={m} onClose={onClose} onSubmit={onSubmit} onSubmitAndNext={onSubmitAndNext} prevMatch={prevMatch} nextMatch={nextMatch} onPrev={onPrev} onNext={onNext} variant={variant} canClose={canClose} />;
  }
  // Team routing: forward to TeamScoreEditorModal.
  if (isTeam) {
    return <TeamScoreEditorModal match={m} teamSize={teamSize} onClose={onClose} onSubmit={onSubmit} onSubmitAndNext={onSubmitAndNext} onAfterDecision={onAfterDecision} onWithdrawal={onWithdrawal} onBoardChange={onBoardChange} prevMatch={prevMatch} nextMatch={nextMatch} onPrev={onPrev} onNext={onNext} password={password} selfReport={selfReport} variant={variant} canClose={canClose} />;
  }

  // a11y: label the dialog with the match/court context so screen readers
  // announce who is fighting and on which shiaijo when the modal opens.
  const dialogLabel = `Score editor: ${m.sideB?.name || "Shiro"} vs ${m.sideA?.name || "Aka"}${m.court ? ` · Shiaijo ${m.court}` : ""}`;

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
              {isPoolDaihyosenBout(m.id) && (
                <span className="tag-badge" style={{ marginLeft: 6 }}>
                  {window.Term ? React.createElement(window.Term, { name: "daihyosen" }, "DH") : "DH"}
                </span>
              )}
            </div>
            {isPoolDaihyosenBout(m.id) && (
              <div style={{ fontSize: 11, color: "var(--ink-2)", marginTop: 2 }}>
                {window.Term
                  ? <>{React.createElement(window.Term, { name: "daihyosen" }, "Daihyosen")} · representative tie-break bout</>
                  : "Daihyosen · representative tie-break bout"}
              </div>
            )}
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
          {/* mp-62vr: rep-player pickers for a team daihyosen/tiebreaker rep
              bout. The sides are TEAM names; the operator records which player
              each team fields, picked from that team's roster. Shiro = sideB,
              Aka = sideA, matching the scoreboard's colour assignment. */}
          {m.repIsTeam && (
            <div data-testid="rep-bout-picker" className="rep-bout-picker" style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 12, marginBottom: 14 }}>
              <label style={{ display: "flex", flexDirection: "column", gap: 4, fontSize: 12, fontWeight: 700, color: "var(--ink-2)" }}>
                <span>Shiro rep · {m.sideB?.name || ""}</span>
                <select
                  data-testid="rep-shiro-select"
                  className="input"
                  value={repPlayerB}
                  disabled={submitting}
                  onChange={(e) => setRepPlayerB(e.target.value)}
                  style={{ padding: "6px 8px", fontSize: 14 }}
                >
                  <option value="">Select player</option>
                  {(m.repRosterB || []).map(nm => <option key={nm} value={nm}>{nm}</option>)}
                </select>
              </label>
              <label style={{ display: "flex", flexDirection: "column", gap: 4, fontSize: 12, fontWeight: 700, color: "var(--red)" }}>
                <span>Aka rep · {m.sideA?.name || ""}</span>
                <select
                  data-testid="rep-aka-select"
                  className="input"
                  value={repPlayerA}
                  disabled={submitting}
                  onChange={(e) => setRepPlayerA(e.target.value)}
                  style={{ padding: "6px 8px", fontSize: 14 }}
                >
                  <option value="">Select player</option>
                  {(m.repRosterA || []).map(nm => <option key={nm} value={nm}>{nm}</option>)}
                </select>
              </label>
            </div>
          )}
          <div className="scoring-board">
              {/* Score slots + point buttons */}
              <div className="sb-match">
                {sides.map((s, idx) => (
                  <React.Fragment key={s.key}>
                    <div className={`sb-side sb-side--${s.color}`}>
                      {/* No SHIRO/AKA pill: the half is TINTED, and a tinted
                          surface does not need the badge as well (operator
                          ruling 2026-09-20, bc-sccl). This SUPERSEDES the
                          bc-dnst rule that the board names the side once in
                          text by this header badge; the tint plus the Shiro
                          hatch now carry it, exactly as the bout rows and foul
                          counters below already did. The sr-only label keeps
                          the side in text for a screen reader, which is what
                          DESIGN.md §4 requires when colour would otherwise be
                          the only signal. */}
                      <SideLabel side={s.color} />
                      {/* Competitor number chip: owned by numbered_name.jsx
                          (the outer-side rule lives there). A recorded
                          withdrawal's Kiken/Fus. rides beside the withdrawn
                          competitor (WithdrawalMarkedName, bc-kcsh). */}
                      <div className="sb-name">
                        <WithdrawalMarkedName match={m} sideKey={s.key} side={s.color} name={s.name} number={s.number} />
                      </div>
                      <div className="sb-points-grid">
                        {getIpponButtons(isNaginata).map((cc) => (
                          <button key={cc} className={`ipt-btn ${cc === "H" ? "ipt-btn--h" : ""}`} onClick={() => addPt(s.key, cc)} disabled={boutDecided || decidedByHantei || s.key === lockedKey}>{cc}</button>
                        ))}
                      </div>
                    </div>
                    {idx === 0 && (
                      <div className="sb-center">
                        {/* Middle mark is "VS" only — individual ippons are WAZA
                            LETTERS in the slots, never a numeric count (numbers
                            belong to pool/team summaries or engi). Entry-zone
                            separator, exempt from the boutMiddle contract per the
                            SCOPE note in bracket.jsx (the master statement; change
                            it there first). mp-42g demoted the vs/X that used to
                            live here (it was the draw-toggle button itself,
                            undiscoverable as such) to a plain separator; the
                            special middles have dedicated controls — tie via the
                            draw toggle below (seeded from the persisted decision
                            via initialIsDrawToggled), encho via the "· (E)
                            Overtime ×N" eyebrow + EnchoControl pill, daihyosen via
                            the DH eyebrow badge. Don't "restore" X/(E)/(DH) here.
                            The scored ippon sit HERE, flanking the VS, as on the
                            team bout rows (operator ruling): Shiro's pair on the
                            left, Aka's on the right, each filling outside-in. */}
                        <div className="sb-center__marks">
                          <div className="sb-slots sb-slots--shiro">{slotButtons(sides[0])}</div>
                          <div className={`sb-vs${isDrawToggled ? " sb-vs--quiet" : ""}`} aria-hidden={isDrawToggled}>VS</div>
                          <div className="sb-slots sb-slots--aka">{slotButtons(sides[1])}</div>
                        </div>
                        {/* Clearing a mark is a tap on the mark itself, which
                            nothing else on the board says (a mouse tooltip
                            never reaches a tablet), so one hint line names it
                            while any mark is scored (operator decision
                            2026-09-15, bc-dnst: the cells stay as they are, no
                            corner badge, no undo). Hidden under hantei, where
                            the cells are locked. */}
                        {(clearableMarks > 0 && !decidedByHantei) && (
                          <div className="sb-hint" data-testid="scoring-modal-clear-hint">Tap a scored mark to clear it</div>
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
                          title={decidedByHantei ? (hanteiRecorded ? "Locked: hantei already recorded" : "Hantei armed: choose a winner above, or cancel") : (!isDrawToggled && isKnockoutPhase ? "Knockout matches can't draw: decide by hantei after encho" : (!isDrawToggled && (aTotal > 0 || bTotal > 0) ? "Clear scores before marking a draw" : (isDrawToggled ? "Cancel draw" : "Mark as draw (hikiwake)")))}
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
                    fouls={s.fouls}
                    setFouls={s.setFouls}
                    onIncrement={s.onIncrement}
                    color={s.color}
                    // Stays off under a recorded withdrawal, as it was before
                    // the withdrawer's letters became editable: a foul
                    // discharges its hansoku into the OTHER side's slots, and
                    // for the withdrawer that is the read-only maru.
                    disabled={boutDecided || decidedByHantei || !!lockedKey}
                  />
                ))}
              </div>
          </div>

          {/* FR-033 encho toggle: collapses to a small "⏱ Overtime" pill
              when no overtime is active. Click the pill (or set the
              counter through the existing flow) to mount the full
              counter UI. Saves ~32px of vertical space pre-overtime.
              Centered below the scoring board so the score + foul boxes
              stay the operator's first focus; overtime is a follow-up. */}
          <div className="encho-center">
            <EnchoControl
              enchoPeriodCount={enchoPeriodCount}
              setEnchoPeriodCount={setEnchoPeriodCount}
            />
          </div>

          {/* Ippon-type letter legend: discoverable "?" affordance mapping
              M/K/D/T/H (+S in naginata) to their kendo meaning, so operators
              don't depend on the viewer-only glossary. */}
          <IpponLegend isNaginata={isNaginata} />

          {/* T093–T098: decision place. The judges'-decision (hantei) affordance
              forms the TOP row whenever the scoreline is tied; the withdrawal/
              forfeit controls (kiken/fusenpai/fusensho) form the BOTTOM row.
              Hantei keeps its own tied-scoreline condition so it still surfaces
              in self-report mode, where the admin-only controls below are hidden.
              Sits between the scoring board and the footer so the flow is: enter
              score OR record a decision → either way the modal closes (or
              surfaces the remaining-matches list for kiken). */}
          {(aTotal === bTotal || (!withdrawnPlayer && !decisionPromptKind && !selfReport)) && (
            <div className="decision-controls decision-controls--stacked" style={{ marginTop: 12, fontSize: 12 }}>
              <span className="decision-controls__label" style={{ color: "var(--ink-3)", fontWeight: 600 }}>Decision:</span>
              {/* A tied match may be decided by referee hantei. The winner is
                  recorded with the hantei flag, distinguishable from an
                  ippon-derived win for stats, audit, and Excel. */}
              {aTotal === bTotal && (
                <div className="hantei-row" data-testid="scoring-modal-hantei-row" style={{ display: "flex", gap: 8, alignItems: "center", padding: "6px 8px", background: "var(--card-2, #fafafa)", borderRadius: 6 }}>
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
                      {/* The recorded side renders primary, like the team
                          panel: this row is the verdict's second channel, so
                          it must SHOW the verdict, not just offer buttons —
                          on a drifted 2-2 the dropped loose mark makes this
                          highlight the only place the side is visible. */}
                      <button
                        type="button"
                        className={`btn btn--sm ${recordedHtKey === "b" ? "btn--primary" : ""}`}
                        data-testid="scoring-modal-hantei-shiro"
                        onClick={() => submitHantei("b")}
                        disabled={submitting || decisionSubmitting}
                      >
                        SHIRO wins
                      </button>
                      <button
                        type="button"
                        className={`btn btn--sm ${recordedHtKey === "a" ? "btn--primary" : ""}`}
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
              {!withdrawnPlayer && !decisionPromptKind && !selfReport && (
                <div className="decision-controls__group">
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
                  {/* Per-bout fusensho is a sub-match concept: implemented inside
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
            </div>
          )}
          {/* Correcting a match a withdrawal ended: what is recorded, and the
              way to remove it when it was a wrong entry. */}
          {recordedWithdrawal && !decisionPromptKind && !withdrawnPlayer && !selfReport && (
            <RecordedWithdrawal match={m} ctl={reopenCtl} disabled={submitting || decisionSubmitting} singleBout />
          )}
          {decisionErr && (
            <div style={{ color: "var(--danger)", fontSize: 12, marginTop: 6 }}>{decisionErr}</div>
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
          {/* Audit reason prompt: shown when correcting a completed match.
              Operator must confirm a reason before the patch is submitted. */}
          {isComplete && showCorrectionPrompt && (
            <ReasonPrompt
              label="Reason for correction"
              presets={CORRECTION_PRESETS}
              submitting={submitting}
              onConfirm={(r) => {
                setCorrectionReason(r);
                setShowCorrectionPrompt(false);
                // A write that asked for this reason (the hantei buttons)
                // runs itself; otherwise it is the default correction.
                const action = correctionActionRef.current;
                correctionActionRef.current = null;
                if (action) { action(r); return; }
                // Re-trigger submit with the now-populated reason.
                // buildPatch reads correctionReason from state, but state
                // updates are async: pass r inline via a local override
                // so the patch is correct on the very first submit.
                const patch = { ...buildPatch("completed"), correctionReason: r };
                doSubmit(() => onSubmit(patch));
              }}
              onCancel={() => { correctionActionRef.current = null; setShowCorrectionPrompt(false); }}
            />
          )}
          {/* What Clear withdrawal and reopen came back with: the notice
              naming a later match it also reopened, its error, and the
              court-busy remedy. Here in the footer, not inside
              RecordedWithdrawal, which unmounts the moment the match is
              running again (the team editor places its copy the same way). */}
          <ReopenFeedback ctl={reopenCtl} testIdPrefix="withdrawal-reopen" />
          {/* F5: PERMANENT-failure banner: a queued terminal write was rejected
              (non-retryable 4xx) and dropped, so it never saved. Non-dismissible
              danger state; the operator must re-enter and submit again. Takes
              precedence over the (now-cleared) pending banner. */}
          {writeFailed && (
            <div className="pending-write-banner pending-write-banner--failed" role="alert" aria-live="assertive">
              <span>Not saved: {writeFailed.reason}. {writeFailed.advice || "Re-enter the result and submit again."}</span>
              {/* Only offer Retry when we still hold the submit closure. After a
                  reopen/hydration it can't be recovered from the serialized queue,
                  so a Retry button would be permanently disabled and misleading: 
                  the banner text already tells the operator to re-enter. */}
              {pendingFnRef.current && (
                <button
                  type="button"
                  className="btn btn--sm"
                  disabled={submitting}
                  onClick={() => doSubmit(pendingFnRef.current)}
                >
                  Retry
                </button>
              )}
            </div>
          )}
          {/* F5: pending-write banner: shown when a terminal submit was only queued
              (offline / transient failure). The write is durable in localStorage
              and will be retried automatically. Operator may still dismiss. */}
          {pendingWrite && !writeFailed && (
            <div className="pending-write-banner" role="status" aria-live="polite">
              <span>Not saved yet: will keep retrying until it lands.</span>
              {/* Only show Retry when we hold the submit closure. On a hydrated
                  re-open it can't be restored from the serialized queue: but the
                  queue still auto-retries in the background, so no button is fine. */}
              {pendingFnRef.current && (
                <button
                  type="button"
                  className="btn btn--sm btn--ghost"
                  disabled={submitting}
                  onClick={() => doSubmit(pendingFnRef.current)}
                >
                  Retry now
                </button>
              )}
            </div>
          )}
          {/* bc-cse: a barred match cannot be started as scheduled -- the
              server would just refuse it -- so the ONE component that shows
              why and offers the default-win/reinstate resolution
              (BarredMatchNotice, admin_scoring_shared.jsx) stands where
              Start would be. Lifted ABOVE .score-nav__actions (a centred
              wrapping flex row of small buttons): the notice's own note text
              plus its action buttons were squeezed into one flex item there
              instead of reading as the full-width block it is everywhere
              else this component renders. */}
          {m.status === "scheduled" && isBarredMatch(m) && (
            <BarredMatchNotice match={m} password={password} />
          )}
          {/* While the correction prompt is open it owns the only Cancel/commit
              row: hide the footer's own nav+actions so the operator never sees
              two Cancels and two commit buttons at the highest-stakes moment
              (amending a recorded result). Mirrored in EngiScoreEditorModal. */}
          {!(isComplete && showCorrectionPrompt) && (
          <div className="score-nav">
            {prevMatch ? (
              <button className="btn btn--sm score-nav__prev" onClick={onPrev} disabled={submitting} title={prevMatch.sideA?.name + " vs " + prevMatch.sideB?.name}>← Prev</button>
            ) : <span />}

            <div className="score-nav__actions">
              {m.status === "scheduled" && !isBarredMatch(m) && (
                <button className="btn btn--sm" onClick={async () => {
                  // F5: this submits status:"running", the one shape
                  // _notifyScoreSuperseded (api_client.jsx) deliberately stays
                  // silent for -- a debounced autosave being superseded is
                  // routine noise there, but Start match is an explicit
                  // operator tap: silence here would just re-enable the
                  // button having saved nothing. Check the awaited result
                  // directly instead of relying on the broadcast.
                  const res = await doSubmit(() => onSubmit(buildPatch("running")));
                  // bc-cse: which not-saved banner, if any. The clock-vs-
                  // supersede ordering (and the silence on a queued write)
                  // lives in notLandedBanner; see write_result.jsx.
                  const banner = notLandedBanner(res);
                  if (banner) setWriteFailed(banner);
                }} disabled={submitting}>
                  Start match
                </button>
              )}
              {canClose && <button className="btn" onClick={handleDismiss} disabled={submitting}>Cancel</button>}
              {onSubmitAndNext ? (
                <button className={`btn btn--primary ${finishArmed && !isComplete ? "btn--confirm" : ""}`} onClick={() => {
                  if (isComplete && !correctionReason) { askCorrectionReason(); return; }
                  if (!isComplete && !finishArmed) { setFinishArmed(true); return; }
                  doSubmit(() => (isComplete ? onSubmit : onSubmitAndNext)(buildPatch("completed")));
                }} disabled={submitting || !canFinish}
                  title={koTieBlocked ? KO_TIE_REASON : undefined}>
                  {submitting ? "Saving…" : koTieBlocked ? "Needs a winner" : isComplete ? "Save correction" : finishArmed ? "Tap again to finish →" : "Finish + Start Next →"}
                </button>
              ) : (
                <button className={`btn btn--primary ${finishArmed && !isComplete ? "btn--confirm" : ""}`} onClick={() => {
                  if (isComplete && !correctionReason) { askCorrectionReason(); return; }
                  if (!isComplete && !finishArmed) { setFinishArmed(true); return; }
                  doSubmit(() => onSubmit(buildPatch("completed")));
                }} disabled={submitting || !canFinish}
                  title={koTieBlocked ? KO_TIE_REASON : undefined}>
                  {submitting ? "Saving…" : koTieBlocked ? "Needs a winner" : isComplete ? "Save correction" : finishArmed ? "Tap again to finish" : "Finish"}
                </button>
              )}
            </div>

            {nextMatch ? (
              <button className="btn btn--sm score-nav__next" onClick={onNext} disabled={submitting} title={nextMatch.sideA?.name + " vs " + nextMatch.sideB?.name}>Next →</button>
            ) : <span />}
          </div>
          )}
          {/* Quiet keyboard-shortcut reminder. It lists only keys that act on
              this host (the same conditions the keydown handler checks). */}
          <ScoringShortcutHint pointKeys={getValidPointKeys(isNaginata)} hasNav={!!((prevMatch && onPrev) || (nextMatch && onNext))} canClose={canClose} />
        </div>
    </>
  );

  // Inline variant (shiaijo operator view): no backdrop / overlay / aria-modal:
  // the panel lives in the page. The shiaijo page passes no prevMatch/
  // nextMatch (queue drives navigation) so the foot's prev/next render as
  // empty spans; Cancel/Close still call onClose to deselect.
  // bc-dnst: the inline panel takes the same compact density as the overlay.
  // Two hosts mount variant="inline": the shiaijo console (admin_shiaijo.jsx)
  // and the Competition > Bracket running-match panel
  // (admin_competition_bracket.jsx). Both are surfaces operators actually
  // score on, so neither may miss the density pass.
  if (variant === "inline") {
    return <div className="scoring-panel editor-modal--compact" aria-label={dialogLabel}>{inner}</div>;
  }

  return (
    <div className="modal-backdrop" data-testid="scoring-modal-root" onClick={handleDismiss}>
      <div className="editor-modal editor-modal--lg editor-modal--compact" role="dialog" aria-modal="true" aria-label={dialogLabel} onClick={(e) => e.stopPropagation()}>
        {inner}
      </div>
    </div>
  );
}
