// Engi-Kyogi (kata competition) score editor.
// Engi matches are scored by flag count: each judge raises a flag for the side
// they prefer. FIK rule: total flags must be ODD (1, 3, or 5) so no draw is
// possible. flagsA = sideA = Aka (right/red), flagsB = sideB = Shiro
// (left/white): the same SideA=Aka/SideB=Shiro convention as every other
// match display in the app (bracket.jsx PlayerLine, admin_pools.jsx,
// admin_scoring_individual.jsx's "{sideB} vs {sideA}" dialog label).
// The winner is the side with more flags.
// Exported: EngiScoreEditorModal.
// Loaded via admin_scoring_individual.jsx (NOT a separate script tag).
//
// Mirrors ScoreEditorModal's chrome/correction/dismiss conventions (variant
// modal/inline wrapper, editor-modal__head/body/foot, ReasonPrompt-gated
// corrections, Escape-to-close with a dirty-state confirm) so an operator
// moving between a kendo and an Engi match on the same court sees one
// consistent editor, not two different products. While the match is running
// the flags are saved as they are entered (operator ruling 2026-09-26): each
// flag + or − is a tap (bc-evts stage 4), an outbox entry in the tick it is
// made, as the individual editor's strikes are, so a match sent back to the
// queue or switched away from keeps them. It skips the kendo editor's
// PRE-MATCH pill and keyboard scoring shortcuts.

const { useState: useStateE, useEffect: useEffectE, useRef: useRefE } = React;

import {
  ReasonPrompt, CORRECTION_PRESETS, useAdoptFromServer, HeldWriteDiscard, HeldWriteNotice, useClearPendingWhenNothingHeld,
  tapBoardOf, useTapBoard, useTapOutcomeNote, useOwnStatus, DraftKeptNotice, DraftDroppedNotice, usePublishUnsaved,
  confirmDiscardUnsaved, LEAVE_MATCH_ANYWAY,
} from './admin_scoring_shared.jsx';
import { engiDraftTaps } from './draft_taps.jsx';
import { SyncStatusPill, useChangedGroups, useKeptInHistoryNote, KeptInHistoryNote } from './admin_scoring_autosave.jsx';
// bc-evts stage 4: a running match is scored by flag events (the outbox's
// recordTap); MAX_FLAGS is the fold's cap, domain.MaxEngiFlags in Go.
import { makeEvent, FLAG_ADD, FLAG_REMOVE, MAX_FLAGS } from './score_events.jsx';
import { publishBoardOverlay } from './court_bridge.jsx';
import { MatchHistoryDisclosure } from './match_history_view.jsx';
import { GROUP_FLAGS } from './match_groups.jsx';
import { useEscapeToClose, confirmDialog } from './ui.jsx';
// NumberedName: single owner of the number-chip-on-the-outer-side rule.
import { NumberedName } from './numbered_name.jsx';
import { SideCell } from './side_cell.jsx';
import { useArmedConfirm, useOpenedTapGuard } from './tap_guard.jsx';
import { terminalFailureBanner, notSavedText, notLandedBanner, writeWasRefused, writeRetryable, writePending } from './write_result.jsx';

// The fold options of an engi match's board (applyPending refuses a flag tap
// without them).
const ENGI_FOLD = { engi: true };
// Valid totals: 1, 3, 5 (odd, guarantees a winner).
const VALID_TOTALS = new Set([1, 3, 5]);

// PipRow renders up to MAX_FLAGS filled/empty pip circles for one side.
function PipRow({ count, side }) {
  return (
    <div
      className={`engi-pips engi-pips--${side}`}
      aria-label={`${count} flag${count === 1 ? "" : "s"}`}
      data-testid={`engi-pips-${side}`}
    >
      {Array.from({ length: MAX_FLAGS }, (_, i) => (
        <span
          key={i}
          className={`engi-pip${i < count ? " engi-pip--filled" : ""}`}
          aria-hidden="true"
        />
      ))}
    </div>
  );
}

// EngiShortcutHint: quiet keyboard-shortcut reminder, matching the kendo
// editor's ScoringShortcutHint style. aria-hidden (the same actions are
// reachable via the on-screen counters and Save button). It shares the
// `.scoring-shortcut-hint` class for its layout, so it hides under a coarse
// pointer too (bc-kbhn), and lists ←/→ and Esc only where they act.
function EngiShortcutHint({ hasNav = false, canClose = false }) {
  const kbd = {
    fontFamily: "var(--font-mono)", fontSize: 11, padding: "1px 5px",
    border: "1px solid var(--line)", borderRadius: 4, background: "var(--surface)",
    color: "var(--ink-3)", margin: "0 1px",
  };
  return (
    <div
      className="scoring-shortcut-hint"
      data-testid="engi-shortcut-hint"
      aria-hidden="true"
    >
      <kbd style={kbd}>A</kbd><kbd style={kbd}>S</kbd><span>add Aka/Shiro flag</span>
      <span aria-hidden="true">·</span>
      <kbd style={kbd}>⌫</kbd><span>undo</span>
      <span aria-hidden="true">·</span>
      <kbd style={kbd}>Enter</kbd><span>save</span>
      {hasNav && <><span aria-hidden="true">·</span><kbd style={kbd}>←</kbd><kbd style={kbd}>→</kbd><span>prev/next</span></>}
      {canClose && <><span aria-hidden="true">·</span><kbd style={kbd}>Esc</kbd><span>close</span></>}
    </div>
  );
}

// Derive the winner side from flag counts. Returns "a" | "b" | null.
function deriveWinner(flagsA, flagsB) {
  if (flagsA > flagsB) return "a";
  if (flagsB > flagsA) return "b";
  return null;
}

// EngiScoreEditorModal: full engi flag-counter editor.
// Props mirror the individual ScoreEditorModal surface so the dispatch in
// admin_scoring_individual.jsx can forward the same prop bag.
export function EngiScoreEditorModal({ match, onClose, onSubmit: onSubmitProp, onSubmitAndNext: onSubmitAndNextProp, prevMatch, nextMatch, onPrev, onNext, variant = "modal", canClose = true, password, selfReport, hostUnsavedRef = null }) {
  const m = match;
  // bc-mrgc: names the groups each write changes, against the match this
  // editor renders from (see useChangedGroups).
  const claimChanged = useChangedGroups(m);
  // Every write built through claim is watched: one that does not land
  // (refused, held or discarded) is forgotten, so the next build of the same
  // value names its groups again (stage 5 review 5).
  const onSubmit = onSubmitProp && ((patch) => claimChanged.watch(onSubmitProp(patch), patch));
  const onSubmitAndNext = onSubmitAndNextProp && ((patch) => claimChanged.watch(onSubmitAndNextProp(patch), patch));
  const isComplete = m.status === "completed";
  const initialFlagsA = m.flagsA || 0;
  const initialFlagsB = m.flagsB || 0;
  // The counts of a match that is NOT running (before its start, or a
  // correction of a finished one), kept here until Save sends them.
  const [draftA, setDraftA] = useStateE(initialFlagsA);
  const [draftB, setDraftB] = useStateE(initialFlagsB);
  const [submitting, setSubmitting] = useStateE(false);
  const [err, setErr] = useStateE("");
  // Audit reason collected when correcting a completed match, mirroring
  // ScoreEditorModal's correctionPrompt/correctionReason pair.
  const [correctionReason, setCorrectionReason] = useStateE("");
  const [showCorrectionPrompt, setShowCorrectionPrompt] = useStateE(false);

  // F5 offline safety net (mirrors ScoreEditorModal): when a terminal write is
  // only queued (offline / transient failure) or permanently rejected, show a
  // sticky banner instead of silently closing as if saved. A dropped venue
  // Wi-Fi must never lose an engi result.
  const mountedRef = useRefE(true);
  useEffectE(() => () => { mountedRef.current = false; }, []);
  // Holds the queued answer, false when nothing is pending (queuedNotice).
  const [pendingWrite, setPendingWrite] = useStateE(false);
  // Holds the last submit closure so the banner's "Retry now" can re-invoke it
  // (a closure, not a bare payload, so a queued Finish+Next retries the same
  // advance path). Null after a re-open hydration (the queue still auto-retries,
  // but the closure can't be recovered from the serialized queue, so Retry hides).
  const pendingFnRef = useRefE(null);
  const [writeFailed, setWriteFailed] = useStateE(null); // { reason, advice? } | null

  // bc-mrgc: what a write applied only in part kept in the match's history.
  const keptInHistory = useKeptInHistoryNote();

  // bc-evts stage 4: a RUNNING match is scored by taps. Each flag + or − is one
  // flag.add / flag.remove event for its side, handed to the outbox
  // (API.recordTap) in the tick it is made, and the board is the server's
  // match with this device's pending taps folded on (tapBoardOf, shared with
  // the individual editor), so there is no autosave to debounce, to write on
  // the way out or to adopt back, and no echo can take a tap off the board.
  // Operator decision D1 (a): a draft this editor holds when ANOTHER device
  // moves the match to running is kept on screen with Save and Discard
  // (own.kept, the ONE owner of this editor's own status writes and its kept
  // draft, own_status.jsx through useOwnStatus; this editor makes no status
  // write of its own, so every change of the feed is another device's);
  // the counts stay a draft meanwhile.
  const own = useOwnStatus(m);
  const tapMode = m.status === "running" && !own.kept;
  const tapBoard = useTapBoard(m, tapMode, ENGI_FOLD);
  const flagsA = tapMode ? (tapBoard.match.flagsA || 0) : draftA;
  const flagsB = tapMode ? (tapBoard.match.flagsB || 0) : draftB;
  // A tap of this match the server did not apply (the quiet note), and a tap
  // refused because the server holds the match as not running (the server's
  // sentence, next to the board).
  const tapNote = useTapOutcomeNote(m.compId, m.id);
  // stepFlags: one operator change, `delta` +1 or -1, to a side's count,
  // clamped 0..MAX_FLAGS. On a running match it is read against the board as
  // it is NOW (tapBoardOf, not this render's copy) and sent as one event, made
  // against the board's base (the match whose stamp floors the tap's), so a
  // quick double tap is two flags (never guarded: CLAUDE.md "A double tap is
  // two complete clicks") and a key pressed at a bound (the buttons are
  // disabled there) sends nothing (bc-rvfx). Nothing waits on the outbox: the
  // board shows the tap at once. The public self-run sheet passes the empty
  // password.
  const stepFlags = (side, delta) => {
    if (tapMode) {
      const board = tapBoardOf(m, ENGI_FOLD);
      const cur = (side === "a" ? board.match.flagsA : board.match.flagsB) || 0;
      if (clamp(cur + delta) === cur) return;
      tapNote.clear();
      window.API.recordTap(m.compId, m.id, makeEvent({ pos: 0, kind: delta > 0 ? FLAG_ADD : FLAG_REMOVE, side }), password, board.base);
      return;
    }
    (side === "a" ? setDraftA : setDraftB)((n) => clamp(n + delta));
  };

  const total = flagsA + flagsB;
  const isValidTotal = VALID_TOTALS.has(total);
  // winnerSide drives the per-side winner highlight below. A valid total is
  // always odd ({1,3,5}), so it already implies a strict winner; canSubmit
  // needs only isValidTotal, not a separate non-null check.
  const winnerSide = deriveWinner(flagsA, flagsB);
  // A kept draft (D1) is saved or discarded first: no result over it.
  const canSubmit = isValidTotal && !submitting && !own.kept;
  // Only a draft can be unsaved: a running match's counts are taps.
  const isDirty = !tapMode && (draftA !== initialFlagsA || draftB !== initialFlagsB);
  // The host asks it before it leaves or swaps this editor (R1).
  usePublishUnsaved(hostUnsavedRef, isDirty, () => discardEntries());
  own.note({ a: initialFlagsA, b: initialFlagsB }, { a: draftA, b: draftB });
  // Follow a flag count recorded on another device — the same rule the kendo
  // editors follow, because this is the same rule: an engi match has ONE result
  // and every surface asking for it shows the same one, and while this editor
  // is open the viewer card, the board and the export are all already showing
  // whatever the server holds. On a running match the board IS the server's
  // copy (plus this device's taps); the draft follows it too, so it starts from
  // the server's count once the match stops running.
  //
  // A draft keeps the operator's counts (keepLocalEdits off running), like the
  // individual editor's: a pair of flag counts is ONE reading of ONE kata
  // performance, so there is no per-row seam to merge along. Until the match
  // is running nothing this editor holds reaches the server before the
  // operator submits, so a stale reading can sit in front of them for as long
  // as the match is open.
  const takeServerFlags = () => {
    setDraftA(initialFlagsA);
    setDraftB(initialFlagsB);
    claimChanged.agree(GROUP_FLAGS);
  };
  useAdoptFromServer({
    signature: `${initialFlagsA}:${initialFlagsB}`,
    apply: takeServerFlags,
    keepLocalEdits: !tapMode,
    isDirty,
  });
  // The match leaves running (finished, or sent back to the queue, here or
  // elsewhere): the draft starts from the server's count, never from one a
  // draft held before the match started (the adopt above keeps a draft it last
  // saw unsaved, and it does not watch the draft while the match runs).
  // D1: Save sends the kept counts as the flag taps that would have made them
  // (draft_taps.jsx), against the counts they were typed over; Discard drops
  // them. Either way the board shows from now on.
  const leaveKeptDraft = () => { takeServerFlags(); own.release(); };
  // Discard entries from the host (R1, stage 5 review 9): the counts take the
  // server's at once and the owner forgets the draft, as the individual and
  // team editors' own Discard entries does.
  const discardEntries = () => { takeServerFlags(); own.release(); own.discardDraft(); };
  const saveKeptDraft = () => {
    const board = tapBoardOf(m, ENGI_FOLD);
    for (const ev of engiDraftTaps(own.kept.base, { a: draftA, b: draftB })) {
      window.API.recordTap(m.compId, m.id, makeEvent({ pos: 0, ...ev }), password, board.base);
    }
    tapNote.clear();
    leaveKeptDraft();
  };
  // The feed took the match out of running (finished or sent back, here or
  // elsewhere), or a kept draft was dropped: the draft starts from the
  // server's counts (own.resetSeq, the owner's one signal).
  useEffectE(() => {
    if (own.resetSeq > 0) takeServerFlags();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [own.resetSeq]);

  // A display of this court on the same device (another tab, the HDMI output)
  // shows exactly this board, pending taps included (operator S1 a): it is
  // published on every tap and every outcome, each of which moves the board.
  // The counts are said even at 0 (the server leaves a 0 out), so a flag taken
  // back clears on the display.
  const overlaySig = tapMode ? JSON.stringify([flagsA, flagsB, tapBoard.match.scoreVersion]) : "";
  useEffectE(() => {
    if (!tapMode) return;
    // Once a finished result of the match is queued the board is no longer
    // the match: its running copy would show a finish as running again on the
    // display (stage 3 review 11, Y1).
    const api = window.API || {};
    if (typeof api.hasPendingTerminalWrite === "function" && api.hasPendingTerminalWrite(m.compId, m.id)) return;
    publishBoardOverlay({ ...tapBoard.match, flagsA, flagsB });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [overlaySig]);

  // Every way out that is not a write, Close and Prev/Next alike (operator
  // ruling 2026-09-27: Prev/Next ask as Close does).
  const leaveEditor = async (go) => {
    if (submitting) return;
    // A running match's flags are taps, already in the outbox, so there is
    // nothing to discard.
    if (tapMode) {
      go();
      return;
    }
    // Not running, so the counts are a draft nothing has sent: the one warning
    // (R1, unsaved_entries.jsx), whose Discard entries drops them at once.
    if (!(await confirmDiscardUnsaved(isDirty, LEAVE_MATCH_ANYWAY, discardEntries))) return;
    go();
  };
  const handleDismiss = () => leaveEditor(onClose);
  useEscapeToClose(canClose ? handleDismiss : undefined);
  // bc-cfbd: the bounce of the tap that opened the overlay must not dismiss it.
  const { openedRef, onClickCapture } = useOpenedTapGuard({ backdropOnly: true });
  const goPrev = () => leaveEditor(onPrev);
  const goNext = () => leaveEditor(onNext);

  // Pair names: the side's name holds both members combined ("Name 1 - Name 2");
  // split so member 2 renders under member 1. Both sides of an engi match are
  // pairs (one entry each).
  const nameOf = (side) => side && typeof side === "object" ? side.name || "" : side || "";
  const dojoOf = (side) => side && typeof side === "object" ? side.dojo || "" : "";

  // sideB = Shiro, sideA = Aka (see file header). Guard on the helper's presence
  // for consistency with bracket.jsx: if ui.jsx has not defined
  // window.engiPairParts yet, fall back to the unsplit name instead of throwing.
  // These lines run only at render time, where window is always present.
  const [shiroName, shiroDN] = window.engiPairParts ? window.engiPairParts(nameOf(m.sideB)) : [nameOf(m.sideB), ""];
  const shiroDojo = dojoOf(m.sideB);
  const [akaName, akaDN] = window.engiPairParts ? window.engiPairParts(nameOf(m.sideA)) : [nameOf(m.sideA), ""];
  const akaDojo   = dojoOf(m.sideA);

  const clamp = (n) => Math.max(0, Math.min(MAX_FLAGS, n));

  // doSubmit takes a submit CLOSURE (like ScoreEditorModal) so the caller picks
  // onSubmit vs onSubmitAndNext; F5 retry re-invokes the same closure.
  const doSubmit = async (fn) => {
    setSubmitting(true);
    setErr("");
    // Clear any prior pending/failed state when the operator explicitly retries.
    if (mountedRef.current) { setPendingWrite(false); setWriteFailed(null); }
    let res;
    try {
      res = await fn();
    } catch (e) {
      if (mountedRef.current) { setErr(e?.message || "Save failed"); setSubmitting(false); }
      return;
    }
    // bc-evts: a save the outbox holds answers `pending` at once; the server's
    // answer is read when it comes, through a callback, never waited for. A
    // refused one then raises the not-saved banner (notLandedBanner) and
    // gives the controls back, as a direct refusal does below. The controls
    // come back at once (bc-evts stage 3 review 11, X2), as the individual
    // and team editors' do: the save is the outbox's, and offline the server
    // may not answer for a long time.
    if (writePending(res)) {
      if (mountedRef.current) setSubmitting(false);
      res.settled.then((body) => {
        keptInHistory.noteFromWrite(body);
        if (!writeWasRefused(body) || !mountedRef.current) return;
        setSaveArmed(false);
        const banner = notLandedBanner(body);
        if (banner) setWriteFailed(banner);
      }, () => {
        // A refusal on the flush: the terminal-fail subscription below raises
        // its banner; Save is disarmed here.
        if (mountedRef.current) setSaveArmed(false);
      });
      return res;
    }
    keptInHistory.noteFromWrite(res);
    // A refused save (writeWasRefused: the host reported it and handed back
    // nothing, or superseded / clock_skew) re-enables the controls and
    // disarms Save: left armed, one tap re-sent the write just refused. It
    // used to leave `submitting` set, so Save read "Saving…" and stayed
    // disabled for good.
    if (writeWasRefused(res)) {
      if (mountedRef.current) { setSubmitting(false); setSaveArmed(false); }
      return res;
    }
    // F5: a terminal write that was only queued (offline / transient) resolves
    // { queued: true } instead of throwing. Do NOT close as if saved: re-enable
    // the controls, enter pending-write mode with the sticky banner, and
    // remember the closure so "Retry now" can re-invoke it (writeRetryable:
    // the one write a re-send can land). Save stays armed. On a clean success
    // the parent closes the modal, so we intentionally leave `submitting` set
    // (matches the prior behaviour and avoids a post-unmount state update).
    if (writeRetryable(res) && mountedRef.current) {
      setSubmitting(false);
      setPendingWrite(res);
      pendingFnRef.current = fn;
    }
    return res;
  };

  // F5 (re-open hydration): if a terminal write for THIS match is still queued
  // (operator finished offline, closed the editor, reopened before the queue
  // drained), surface the pending banner on mount. The payload can't be
  // recovered from the serialized queue, so "Retry now" stays hidden; the
  // queue keeps auto-retrying meanwhile.
  useEffectE(() => {
    if (!m.compId || !m.id) return;
    if (window.API && typeof window.API.hasPendingTerminalWrite === "function"
        && window.API.hasPendingTerminalWrite(m.compId, m.id)) {
      setPendingWrite(true);
    }
  }, [m.compId, m.id]);

  // F5: the pending banner goes once this device holds no write for the
  // match: landed, or discarded here or from the topbar's list.
  // bc-cse (operator ruling 2026-10-05): a discarded held write disarms the
  // two-tap commit too, so a discarded result is not one tap from being sent
  // again. The same function serves the editor's own Discard and the hook's
  // edge for a discard made elsewhere.
  const dropHeldWrite = () => {
    setPendingWrite(false);
    pendingFnRef.current = null;
    setSaveArmed(false);
  };
  useClearPendingWhenNothingHeld(m.compId, m.id, pendingWrite, dropHeldWrite);

  // Save guard, the same as the individual and team editors' Finish (bc-dtfn,
  // operator ruling 2026-09-27 that the editors behave alike): a tap ARMS the
  // button, whose label then says to tap again, and a second tap after
  // TAP_BOUNCE_MS saves, so the bounce of the arming tap can neither save nor
  // start the next match. Any flag change disarms it, so a stale count cannot
  // be confirmed. A correction ("Save correction") and keyboard Enter save
  // directly, as there.
  const { armed: saveArmed, setArmed: setSaveArmed, confirm: confirmSave } = useArmedConfirm();
  useEffectE(() => { setSaveArmed(false); }, [flagsA, flagsB]);

  // F5: surface a PERMANENT terminal-write failure (non-retryable 4xx on a
  // queued retry) as an explicit "not saved" state, else the write is silently
  // dropped and the pending banner clears to look saved.
  useEffectE(() => {
    if (!m.compId || !m.id) return;
    if (typeof window.subscribeTerminalWriteFailed !== "function") return;
    const unsub = window.subscribeTerminalWriteFailed((info) => {
      if (!mountedRef.current) return;
      if (!info || info.compID !== m.compId || info.matchID !== m.id) return;
      setWriteFailed(terminalFailureBanner(info));
      setPendingWrite(false);
      // Re-sending a failed save has to be deliberate: disarm, as the
      // individual editor does.
      setSaveArmed(false);
    });
    return unsub;
  }, [m.compId, m.id]);

  // buildPayload assembles the wire patch. correctionReason persists in state
  // once confirmed via ReasonPrompt, so a retry after a failed first attempt
  // (operator clicks "Save correction" again without reopening the prompt) must
  // still carry it: otherwise the retry silently drops the audit reason.
  // On a running match the save is a rule-bound write over the board: it
  // names the server copy the board showed (`seenScoreVersion`, request-only,
  // never sent: the outbox turns it into the write's basis, beside this
  // device's pending taps of the match and the writeId), so a finish never
  // claims a result its screen did not show.
  const boardSeen = tapMode ? { seenScoreVersion: Number(tapBoard.base.scoreVersion) || 0 } : {};
  const buildPayload = () => claimChanged({ flagsA, flagsB, status: "completed", ...boardSeen, ...(correctionReason ? { correctionReason } : {}) });
  const handleSubmit = () => {
    if (!canSubmit) return;
    if (isComplete && !correctionReason) {
      setShowCorrectionPrompt(true);
      return;
    }
    const payload = buildPayload();
    // A correction (completed match) saves the current match only: never
    // auto-advance / start-next, even when onSubmitAndNext is wired.
    if (onSubmitAndNext && !isComplete) doSubmit(() => onSubmitAndNext(payload));
    else doSubmit(() => onSubmit(payload));
  };

  // Keyboard flag entry (impeccable critique P2/P3). Engi scores are COUNTS,
  // not waza TYPES, so the kendo editor's "bare letter = waza to Shiro, Shift =
  // award to Aka" model doesn't map; instead the side is named by its initial
  // (A = Aka, S = Shiro), which is more intuitive for a two-counter entry:
  //   a / A → Aka + 1        (case-insensitive: Shift is INERT, not a surprise)
  //   s / S → Shiro + 1
  //   Backspace / Delete → undo the last flag added (removes from that side)
  //   Enter → save (same completed-match/correction gating as the button)
  //   ← / → → prev/next match
  // Case-insensitive adds are deliberate: a kendo-trained operator whose muscle
  // memory is "hold Shift for Aka" gets Aka +1 (harmless), NOT a decrement — the
  // one cross-editor collision the re-critique flagged. Removal lives on
  // Backspace (a universal "undo") rather than Shift. Registered once; reads
  // fresh state via kbRef. Escape stays owned by useEscapeToClose above.
  const kbRef = useRefE(null);
  const lastSideRef = useRefE(null); // "a" | "s" | null: which side Backspace undoes
  kbRef.current = { submitting, canSubmit, showCorrectionPrompt, stepFlags, handleSubmit, onPrev, onNext, goPrev, goNext, prevMatch, nextMatch };
  useEffectE(() => {
    const onKeyDown = (ev) => {
      const s = kbRef.current;
      if (s.submitting) return;
      if (ev.ctrlKey || ev.metaKey || ev.altKey) return;
      // The correction reason prompt owns interaction while it's open.
      if (s.showCorrectionPrompt) return;
      // Never hijack typing in a text field (e.g. the reason note).
      if (window.isTextEntry && window.isTextEntry(ev.target)) return;

      // ←/→ move between queued matches (parity with the kendo editor).
      // Keyed on the neighbour match as well as the callback: the Scores tab
      // wires onPrev/onNext unconditionally, and with no neighbour they call
      // scoreKeyOf(null), which throws. Same condition as the hint's hasNav.
      if (ev.key === "ArrowLeft" && s.onPrev && s.prevMatch) { ev.preventDefault(); s.goPrev(); return; }
      if (ev.key === "ArrowRight" && s.onNext && s.nextMatch) { ev.preventDefault(); s.goNext(); return; }

      if (ev.key === "Enter") {
        // Let a focused button/link/input handle its own Enter (e.g. Cancel).
        if (window.isInteractiveTarget && window.isInteractiveTarget(ev.target)) return;
        if (s.canSubmit) { ev.preventDefault(); s.handleSubmit(); }
        return;
      }
      switch (ev.key) {
        case "a": case "A":
          ev.preventDefault(); s.stepFlags("a", 1); lastSideRef.current = "a"; break;
        case "s": case "S":
          ev.preventDefault(); s.stepFlags("b", 1); lastSideRef.current = "s"; break;
        case "Backspace": case "Delete":
          ev.preventDefault();
          if (lastSideRef.current === "a") s.stepFlags("a", -1);
          else if (lastSideRef.current === "s") s.stepFlags("b", -1);
          break;
        default: break;
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, []); // registered once; reads fresh state via kbRef

  // Outline styling: danger when total is non-zero but invalid.
  const invalidOutline = total > 0 && !isValidTotal;

  const dialogLabel = `Engi score editor: ${shiroName || "Shiro"} vs ${akaName || "Aka"}${m.court ? ` · Shiaijo ${m.court}` : ""}`;

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
          </div>
          <div className="editor-modal__title">
            <span>Shiaijo {m.court} · {m.scheduledAt || "Now"}</span>
          </div>
        </div>
        <div style={{ display: "flex", flexDirection: "column", alignItems: "flex-end", gap: 4 }}>
          {isComplete && (
            <div className="editor-head-pill" style={{ fontSize: 10, fontWeight: 700 }}>CORRECTION</div>
          )}
          <SyncStatusPill isRunning={m.status === "running"} />
          {canClose && <button className="btn btn--ghost btn--sm" onClick={handleDismiss} disabled={submitting} style={{ padding: "2px 8px" }} aria-label="Close" data-testid="engi-close-btn">✕ Close</button>}
        </div>
      </div>

      <div className="editor-modal__body">
        {/* Sides: Shiro (left, sideB) | Aka (right, sideA) */}
        <div className="engi-sides">
          {/* Shiro / White / sideB */}
          <SideCell side="shiro" fill={false} className={`engi-side engi-side--shiro${winnerSide === "b" ? " engi-side--winner" : ""}`} data-testid="engi-side-shiro">
            {/* No Aka/Shiro badge (operator decision 2026-09-20, bc-sccl): this
                card already carries the full DESIGN.md §4 treatment -- a tinted
                fill plus a coloured top bar, the same tokens and 45° hatch pitch
                as the kendo score card -- so the badge restated the side a third
                time. The sr-only label keeps it in text for a screen reader,
                since the hatch only serves the sighted case. */}
            {/* The competitor number rides the FIRST member's line, as on every
                other engi surface (viewer_standings.jsx, viewer_competition.jsx):
                an engi PAIR is one participant with one number, so the chip
                belongs to the pair, not to either member. This editor had no
                number at all, which made it the one scoring surface where an
                operator could not match the card in front of them to the bout
                sheet. */}
            <div className="engi-side__names">
              <div className="engi-side__name">
                <NumberedName side="shiro" name={shiroName} number={m.sideB?.number} />
              </div>
              {shiroDN && <div className="engi-side__name">{shiroDN}</div>}
              {shiroDojo && <div className="engi-side__dojo">{shiroDojo}</div>}
            </div>
            <PipRow count={flagsB} side="shiro" />
            <div className="engi-counter">
              <button
                type="button"
                className="btn engi-counter__btn"
                onClick={() => stepFlags("b", -1)}
                disabled={flagsB <= 0}
                aria-label="Shiro minus one flag"
                data-testid="engi-shiro-dec"
              >−</button>
              <span className="engi-counter__value" data-testid="engi-shiro-count">{flagsB}</span>
              <button
                type="button"
                className="btn engi-counter__btn"
                onClick={() => stepFlags("b", 1)}
                disabled={flagsB >= MAX_FLAGS}
                aria-label="Shiro plus one flag"
                data-testid="engi-shiro-inc"
              >+</button>
            </div>
          </SideCell>

          {/* Decorative entry-zone divider (aria-hidden), exempt from the
              boutMiddle contract per the SCOPE note in bracket.jsx (the
              master statement; change it there first). Here the exemption is
              structural, not just stylistic: flag totals are constrained to
              odd values by VALID_TOTALS, so a draw cannot exist and "X" is
              unreachable by construction; engi has no ippon, no encho and no
              daihyosen, so "(E)" and "(DH)" have nothing to report either. */}
          <div className="engi-divider" aria-hidden="true">vs</div>

          {/* Aka / Red / sideA */}
          <SideCell side="aka" fill={false} className={`engi-side engi-side--aka${winnerSide === "a" ? " engi-side--winner" : ""}`} data-testid="engi-side-aka">
            <div className="engi-side__names">
              <div className="engi-side__name">
                <NumberedName side="aka" name={akaName} number={m.sideA?.number} />
              </div>
              {akaDN && <div className="engi-side__name">{akaDN}</div>}
              {akaDojo && <div className="engi-side__dojo">{akaDojo}</div>}
            </div>
            <PipRow count={flagsA} side="aka" />
            <div className="engi-counter">
              <button
                type="button"
                className="btn engi-counter__btn"
                onClick={() => stepFlags("a", -1)}
                disabled={flagsA <= 0}
                aria-label="Aka minus one flag"
                data-testid="engi-aka-dec"
              >−</button>
              <span className="engi-counter__value" data-testid="engi-aka-count">{flagsA}</span>
              <button
                type="button"
                className="btn engi-counter__btn"
                onClick={() => stepFlags("a", 1)}
                disabled={flagsA >= MAX_FLAGS}
                aria-label="Aka plus one flag"
                data-testid="engi-aka-inc"
              >+</button>
            </div>
          </SideCell>
        </div>

        {/* Total validity indicator */}
        <div
          className={`engi-total${invalidOutline ? " engi-total--invalid" : ""}${isValidTotal && total > 0 ? " engi-total--valid" : ""}`}
          data-testid="engi-total"
          style={invalidOutline ? { outline: "2px solid var(--danger)", borderRadius: 4 } : null}
        >
          {total === 0
            ? "Enter flags (total must be 1, 3, or 5)"
            : isValidTotal
              ? `Total: ${total}, ${winnerSide === "a" ? "Aka wins" : "Shiro wins"}`
              : `Total: ${total}, must be 1, 3, or 5 (odd)`}
        </div>

        {/* Operator S7 a: a tap refused because the server holds the match
            as not running, in the server's own sentence, next to the board. */}
        {tapNote.refusal && (
          <div className="sb-hint" role="status" data-testid="tap-refusal">{tapNote.refusal}</div>
        )}

        {/* Error */}
        {err && <div className="score-editor__err" role="alert">{err}</div>}
      </div>

      {/* Column layout so the banners, the Cancel/Save row, and the keyboard
          hint stack vertically (the shared .editor-modal__foot is a flex ROW
          with space-between, which would otherwise put the hint beside the
          actions). */}
      <div className="editor-modal__foot editor-modal__foot--nav" style={{ flexDirection: "column", alignItems: "stretch", gap: 8 }}>
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
              // A correction saves the current match only (never advance).
              const patch = claimChanged({ flagsA, flagsB, status: "completed", correctionReason: r });
              doSubmit(() => onSubmit(patch));
            }}
            onCancel={() => setShowCorrectionPrompt(false)}
          />
        )}
        {/* F5: PERMANENT-failure banner: the write was refused, so it never
            saved. No Retry: a refusal is never fixed by sending the same write
            again (writeRetryable). Takes precedence over the pending banner.
            Mirrors ScoreEditorModal. */}
        {writeFailed && (
          <div className="pending-write-banner pending-write-banner--failed" role="alert" aria-live="assertive">
            <span>{notSavedText(writeFailed)}</span>
          </div>
        )}
        <KeptInHistoryNote note={keptInHistory.note} />
        {/* bc-evts: a tap the server did not apply, said quietly
            (tapNotAppliedText); the server kept it in the match's history. */}
        {tapNote.note && (
          <div className="sb-hint" role="status" data-testid="tap-not-applied-note">{tapNote.note}</div>
        )}
        {/* bc-mrgc: every write that reached this match, kept or applied.
            The organiser's view, so not on a self-run participant's sheet. */}
        <MatchHistoryDisclosure match={m} password={password} hidden={!!selfReport} />
        {/* F5: pending-write banner: a terminal submit was only queued (offline
            / transient). The write is durable in localStorage and auto-retries;
            the operator may still retry manually while we hold the payload. */}
        {pendingWrite && !writeFailed && (
          <div className="pending-write-banner" role="status" aria-live="polite">
            <HeldWriteNotice compId={m.compId} matchId={m.id} match={m} res={pendingWrite} />
            {pendingFnRef.current && (
              <button type="button" className="btn btn--sm btn--ghost" disabled={submitting} onClick={() => doSubmit(pendingFnRef.current)}>Retry now</button>
            )}
            <HeldWriteDiscard
              compId={m.compId}
              matchId={m.id}
              disabled={submitting}
              onDiscarded={dropHeldWrite}
            />
          </div>
        )}
        {/* While the correction prompt is open it owns the only Cancel/commit
            row: hide the footer's own actions so the operator never sees two
            Cancels and two commit buttons at the highest-stakes moment
            (amending a recorded result). Mirrors ScoreEditorModal. */}
        {own.kept && <DraftKeptNotice from={own.kept.from} onSave={saveKeptDraft} onDiscard={leaveKeptDraft} disabled={submitting} />}
        {own.droppedShown && <DraftDroppedNotice />}
        {!(isComplete && showCorrectionPrompt) && (
          <div className="score-nav">
            {prevMatch ? (
              <button type="button" className="btn btn--sm score-nav__prev" onClick={goPrev} disabled={submitting} title={(prevMatch.sideA?.name || "") + " vs " + (prevMatch.sideB?.name || "")}>← Prev</button>
            ) : <span />}
            <div className="score-nav__actions">
              {canClose && <button type="button" className="btn" onClick={handleDismiss} disabled={submitting}>Cancel</button>}
              <button
                type="button"
                className={`btn btn--primary ${saveArmed && !isComplete ? "btn--confirm" : ""}`}
                onClick={(ev) => { if (!isComplete && !confirmSave(ev)) return; handleSubmit(); }}
                disabled={!canSubmit}
                data-testid="engi-submit"
                style={invalidOutline ? { outline: "2px solid var(--danger)" } : null}
              >
                {submitting ? "Saving…" : isComplete ? "Save correction"
                  : saveArmed ? (onSubmitAndNext ? "Tap again to finish →" : "Tap again to save")
                  : (onSubmitAndNext ? "Finish + Start Next →" : "Save result")}
              </button>
            </div>
            {nextMatch ? (
              <button type="button" className="btn btn--sm score-nav__next" onClick={goNext} disabled={submitting} title={(nextMatch.sideA?.name || "") + " vs " + (nextMatch.sideB?.name || "")}>Next →</button>
            ) : <span />}
          </div>
        )}
        {/* Quiet keyboard-shortcut reminder (parity with the kendo editor's
            ScoringShortcutHint). Hidden during the reason prompt, when keys
            are disabled. */}
        {!(isComplete && showCorrectionPrompt) && <EngiShortcutHint hasNav={!!((prevMatch && onPrev) || (nextMatch && onNext))} canClose={canClose} />}
      </div>
    </>
  );

  // BOTH hosts are compact (operator ruling 2026-09-18). The inline panel had
  // the class and the overlay did not, so the same editor rendered at two
  // densities depending on which screen opened it. Rendered side by side on a
  // live match the two were near-indistinguishable -- the compact rules only
  // change padding here, never the pair cards, the flag circles or the
  // `.engi-counter__btn` controls -- so consistency won at no visual cost.
  if (variant === "inline") {
    return <div className="scoring-panel editor-modal--compact" aria-label={dialogLabel} data-testid="engi-score-editor">{inner}</div>;
  }

  return (
    <div className="modal-backdrop" data-testid="scoring-modal-root" ref={openedRef} onClickCapture={onClickCapture} onClick={handleDismiss}>
      <div className="editor-modal editor-modal--compact" role="dialog" aria-modal="true" aria-label={dialogLabel} onClick={(e) => e.stopPropagation()} data-testid="engi-score-editor">
        {inner}
      </div>
    </div>
  );
}

export { MAX_FLAGS, VALID_TOTALS, deriveWinner };
