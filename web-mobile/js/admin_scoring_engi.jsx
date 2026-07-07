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
// consistent editor, not two different products. Engi has no live/"running"
// scoring phase (flags are entered all at once, no autosave), so it skips
// the kendo editor's PRE-MATCH pill, keyboard scoring shortcuts, and
// SyncStatusPill: those concepts don't apply here.

const { useState: useStateE, useEffect: useEffectE, useRef: useRefE } = React;

import { ReasonPrompt, CORRECTION_PRESETS } from './admin_scoring_shared.jsx';
import { useEscapeToClose, confirmDialog } from './ui.jsx';

const MAX_FLAGS = 5;
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
// reachable via the on-screen counters and Save button).
function EngiShortcutHint({ hasNav = false }) {
  const kbd = {
    fontFamily: "var(--font-mono)", fontSize: 11, padding: "1px 5px",
    border: "1px solid var(--line)", borderRadius: 4, background: "var(--surface)",
    color: "var(--ink-3)", margin: "0 1px",
  };
  return (
    <div
      data-testid="engi-shortcut-hint"
      aria-hidden="true"
      style={{ marginTop: 6, fontSize: 12, color: "var(--ink-3)", textAlign: "center", display: "flex", gap: 4, justifyContent: "center", alignItems: "center", flexWrap: "wrap" }}
    >
      <kbd style={kbd}>A</kbd><kbd style={kbd}>S</kbd><span>add Aka/Shiro flag</span>
      <span aria-hidden="true">·</span>
      <kbd style={kbd}>⌫</kbd><span>undo</span>
      <span aria-hidden="true">·</span>
      <kbd style={kbd}>Enter</kbd><span>save</span>
      {hasNav && <><span aria-hidden="true">·</span><kbd style={kbd}>←</kbd><kbd style={kbd}>→</kbd><span>prev/next</span></>}
      <span aria-hidden="true">·</span>
      <kbd style={kbd}>Esc</kbd><span>close</span>
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
export function EngiScoreEditorModal({ match, onClose, onSubmit, onSubmitAndNext, prevMatch, nextMatch, onPrev, onNext, variant = "modal", canClose = true }) {
  const m = match;
  const isComplete = m.status === "completed";
  const initialFlagsA = m.flagsA || 0;
  const initialFlagsB = m.flagsB || 0;
  const [flagsA, setFlagsA] = useStateE(initialFlagsA);
  const [flagsB, setFlagsB] = useStateE(initialFlagsB);
  const [submitting, setSubmitting] = useStateE(false);
  const [err, setErr] = useStateE("");
  // Audit reason collected when correcting a completed match, mirroring
  // ScoreEditorModal's showCorrectionPrompt/correctionReason pair.
  const [correctionReason, setCorrectionReason] = useStateE("");
  const [showCorrectionPrompt, setShowCorrectionPrompt] = useStateE(false);

  // F5 offline safety net (mirrors ScoreEditorModal): when a terminal write is
  // only queued (offline / transient failure) or permanently rejected, show a
  // sticky banner instead of silently closing as if saved. A dropped venue
  // Wi-Fi must never lose an engi result.
  const mountedRef = useRefE(true);
  useEffectE(() => () => { mountedRef.current = false; }, []);
  const [pendingWrite, setPendingWrite] = useStateE(false);
  // Holds the last submit closure so the banner's "Retry now" can re-invoke it
  // (a closure, not a bare payload, so a queued Finish+Next retries the same
  // advance path). Null after a re-open hydration (the queue still auto-retries,
  // but the closure can't be recovered from the serialized queue, so Retry hides).
  const pendingFnRef = useRefE(null);
  const [writeFailed, setWriteFailed] = useStateE(null); // { reason } | null

  const total = flagsA + flagsB;
  const isValidTotal = VALID_TOTALS.has(total);
  // winnerSide drives the per-side winner highlight below. A valid total is
  // always odd ({1,3,5}), so it already implies a strict winner; canSubmit
  // needs only isValidTotal, not a separate non-null check.
  const winnerSide = deriveWinner(flagsA, flagsB);
  const canSubmit = isValidTotal && !submitting;
  const isDirty = flagsA !== initialFlagsA || flagsB !== initialFlagsB;

  const handleDismiss = async () => {
    if (submitting) return;
    if (isDirty && !(await confirmDialog({ message: "Discard unsaved scoring changes?", confirmLabel: "Discard changes", danger: true }))) return;
    onClose();
  };
  useEscapeToClose(canClose ? handleDismiss : undefined);

  // Pair names: member1 = side.name (or side string), member2 = side.displayName.
  // Both sides of an engi match are pairs (one entry each).
  const nameOf = (side) => side && typeof side === "object" ? side.name || "" : side || "";
  const displayOf = (side) => side && typeof side === "object" ? side.displayName || "" : "";
  const dojoOf = (side) => side && typeof side === "object" ? side.dojo || "" : "";

  // sideB = Shiro, sideA = Aka (see file header).
  const shiroName = nameOf(m.sideB);
  const shiroDN   = displayOf(m.sideB);
  const shiroDojo = dojoOf(m.sideB);
  const akaName   = nameOf(m.sideA);
  const akaDN     = displayOf(m.sideA);
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
    // F5: a terminal write that was only queued (offline / transient) resolves
    // { queued: true } instead of throwing. Do NOT close as if saved: re-enable
    // the controls, enter pending-write mode with the sticky banner, and
    // remember the closure so "Retry now" can re-invoke it. On a clean success
    // the parent closes the modal, so we intentionally leave `submitting` set
    // (matches the prior behaviour and avoids a post-unmount state update).
    if (res && res.queued && mountedRef.current) {
      setSubmitting(false);
      setPendingWrite(true);
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

  // F5: auto-clear the pending banner once the queue drains for this match.
  // Guards the window globals so the modal never throws on mount in tests.
  useEffectE(() => {
    if (!m.compId || !m.id) return;
    if (typeof window.subscribeSyncStatus !== "function") return;
    const unsub = window.subscribeSyncStatus((status) => {
      if (!mountedRef.current) return;
      const stillPending = (window.API && typeof window.API.hasPendingTerminalWrite === "function")
        ? window.API.hasPendingTerminalWrite(m.compId, m.id)
        : false;
      if (status === "synced" && !stillPending) {
        setPendingWrite(false);
        pendingFnRef.current = null;
      }
    });
    return unsub;
  }, [m.compId, m.id]);

  // F5: surface a PERMANENT terminal-write failure (non-retryable 4xx on a
  // queued retry) as an explicit "not saved" state, else the write is silently
  // dropped and the pending banner clears to look saved.
  useEffectE(() => {
    if (!m.compId || !m.id) return;
    if (typeof window.subscribeTerminalWriteFailed !== "function") return;
    const unsub = window.subscribeTerminalWriteFailed((info) => {
      if (!mountedRef.current) return;
      if (!info || info.compID !== m.compId || info.matchID !== m.id) return;
      setWriteFailed({ reason: info.reason || `save rejected (${info.status || "error"})` });
      setPendingWrite(false);
    });
    return unsub;
  }, [m.compId, m.id]);

  // buildPayload assembles the wire patch. correctionReason persists in state
  // once confirmed via ReasonPrompt, so a retry after a failed first attempt
  // (operator clicks "Save correction" again without reopening the prompt) must
  // still carry it: otherwise the retry silently drops the audit reason.
  const buildPayload = () => ({ flagsA, flagsB, status: "completed", ...(correctionReason ? { correctionReason } : {}) });
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
  kbRef.current = { submitting, canSubmit, showCorrectionPrompt, flagsA, flagsB, setFlagsA, setFlagsB, clamp, handleSubmit, onPrev, onNext };
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
      if (ev.key === "ArrowLeft" && s.onPrev) { ev.preventDefault(); s.onPrev(); return; }
      if (ev.key === "ArrowRight" && s.onNext) { ev.preventDefault(); s.onNext(); return; }

      if (ev.key === "Enter") {
        // Let a focused button/link/input handle its own Enter (e.g. Cancel).
        if (window.isInteractiveTarget && window.isInteractiveTarget(ev.target)) return;
        if (s.canSubmit) { ev.preventDefault(); s.handleSubmit(); }
        return;
      }
      switch (ev.key) {
        case "a": case "A":
          ev.preventDefault(); s.setFlagsA(s.clamp(s.flagsA + 1)); lastSideRef.current = "a"; break;
        case "s": case "S":
          ev.preventDefault(); s.setFlagsB(s.clamp(s.flagsB + 1)); lastSideRef.current = "s"; break;
        case "Backspace": case "Delete":
          ev.preventDefault();
          if (lastSideRef.current === "a") s.setFlagsA(s.clamp(s.flagsA - 1));
          else if (lastSideRef.current === "s") s.setFlagsB(s.clamp(s.flagsB - 1));
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
          {canClose && <button className="btn btn--ghost btn--sm" onClick={handleDismiss} disabled={submitting} style={{ padding: "2px 8px" }} aria-label="Close" data-testid="engi-close-btn">✕ Close</button>}
        </div>
      </div>

      <div className="editor-modal__body">
        {/* Sides: Shiro (left, sideB) | Aka (right, sideA) */}
        <div className="engi-sides">
          {/* Shiro / White / sideB */}
          <div className={`engi-side engi-side--shiro${winnerSide === "b" ? " engi-side--winner" : ""}`} data-testid="engi-side-shiro">
            <div className="engi-side__badge engi-side__badge--shiro">Shiro</div>
            <div className="engi-side__names">
              <div className="engi-side__name">{shiroName}</div>
              {shiroDN && <div className="engi-side__name">{shiroDN}</div>}
              {shiroDojo && <div className="engi-side__dojo">{shiroDojo}</div>}
            </div>
            <PipRow count={flagsB} side="shiro" />
            <div className="engi-counter">
              <button
                type="button"
                className="btn engi-counter__btn"
                onClick={() => setFlagsB(clamp(flagsB - 1))}
                disabled={flagsB <= 0}
                aria-label="Shiro minus one flag"
                data-testid="engi-shiro-dec"
              >−</button>
              <span className="engi-counter__value" data-testid="engi-shiro-count">{flagsB}</span>
              <button
                type="button"
                className="btn engi-counter__btn"
                onClick={() => setFlagsB(clamp(flagsB + 1))}
                disabled={flagsB >= MAX_FLAGS}
                aria-label="Shiro plus one flag"
                data-testid="engi-shiro-inc"
              >+</button>
            </div>
          </div>

          <div className="engi-divider" aria-hidden="true">vs</div>

          {/* Aka / Red / sideA */}
          <div className={`engi-side engi-side--aka${winnerSide === "a" ? " engi-side--winner" : ""}`} data-testid="engi-side-aka">
            <div className="engi-side__badge engi-side__badge--aka">Aka</div>
            <div className="engi-side__names">
              <div className="engi-side__name">{akaName}</div>
              {akaDN && <div className="engi-side__name">{akaDN}</div>}
              {akaDojo && <div className="engi-side__dojo">{akaDojo}</div>}
            </div>
            <PipRow count={flagsA} side="aka" />
            <div className="engi-counter">
              <button
                type="button"
                className="btn engi-counter__btn"
                onClick={() => setFlagsA(clamp(flagsA - 1))}
                disabled={flagsA <= 0}
                aria-label="Aka minus one flag"
                data-testid="engi-aka-dec"
              >−</button>
              <span className="engi-counter__value" data-testid="engi-aka-count">{flagsA}</span>
              <button
                type="button"
                className="btn engi-counter__btn"
                onClick={() => setFlagsA(clamp(flagsA + 1))}
                disabled={flagsA >= MAX_FLAGS}
                aria-label="Aka plus one flag"
                data-testid="engi-aka-inc"
              >+</button>
            </div>
          </div>
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
              doSubmit(() => onSubmit({ flagsA, flagsB, status: "completed", correctionReason: r }));
            }}
            onCancel={() => setShowCorrectionPrompt(false)}
          />
        )}
        {/* F5: PERMANENT-failure banner: a queued terminal write was rejected
            (non-retryable) and dropped, so it never saved. Takes precedence
            over the pending banner. Mirrors ScoreEditorModal. */}
        {writeFailed && (
          <div className="pending-write-banner pending-write-banner--failed" role="alert" aria-live="assertive">
            <span>Not saved: {writeFailed.reason}. Re-enter the result and submit again.</span>
            {pendingFnRef.current && (
              <button type="button" className="btn btn--sm" disabled={submitting} onClick={() => doSubmit(pendingFnRef.current)}>Retry</button>
            )}
          </div>
        )}
        {/* F5: pending-write banner: a terminal submit was only queued (offline
            / transient). The write is durable in localStorage and auto-retries;
            the operator may still retry manually while we hold the payload. */}
        {pendingWrite && !writeFailed && (
          <div className="pending-write-banner" role="status" aria-live="polite">
            <span>Not saved yet: will keep retrying until it lands.</span>
            {pendingFnRef.current && (
              <button type="button" className="btn btn--sm btn--ghost" disabled={submitting} onClick={() => doSubmit(pendingFnRef.current)}>Retry now</button>
            )}
          </div>
        )}
        {/* While the correction prompt is open it owns the only Cancel/commit
            row: hide the footer's own actions so the operator never sees two
            Cancels and two commit buttons at the highest-stakes moment
            (amending a recorded result). Mirrors ScoreEditorModal. */}
        {!(isComplete && showCorrectionPrompt) && (
          <div className="score-nav">
            {prevMatch ? (
              <button type="button" className="btn btn--sm score-nav__prev" onClick={onPrev} disabled={submitting} title={(prevMatch.sideA?.name || "") + " vs " + (prevMatch.sideB?.name || "")}>← Prev</button>
            ) : <span />}
            <div className="score-nav__actions">
              {canClose && <button type="button" className="btn" onClick={handleDismiss} disabled={submitting}>Cancel</button>}
              <button
                type="button"
                className="btn btn--primary"
                onClick={handleSubmit}
                disabled={!canSubmit}
                data-testid="engi-submit"
                style={invalidOutline ? { outline: "2px solid var(--danger)" } : null}
              >
                {submitting ? "Saving…" : isComplete ? "Save correction" : (onSubmitAndNext ? "Finish + Start Next →" : "Save result")}
              </button>
            </div>
            {nextMatch ? (
              <button type="button" className="btn btn--sm score-nav__next" onClick={onNext} disabled={submitting} title={(nextMatch.sideA?.name || "") + " vs " + (nextMatch.sideB?.name || "")}>Next →</button>
            ) : <span />}
          </div>
        )}
        {/* Quiet keyboard-shortcut reminder (parity with the kendo editor's
            ScoringShortcutHint). Hidden during the reason prompt, when keys
            are disabled. */}
        {!(isComplete && showCorrectionPrompt) && <EngiShortcutHint hasNav={!!(prevMatch || nextMatch)} />}
      </div>
    </>
  );

  if (variant === "inline") {
    return <div className="scoring-panel" aria-label={dialogLabel} data-testid="engi-score-editor">{inner}</div>;
  }

  return (
    <div className="modal-backdrop" data-testid="scoring-modal-root" onClick={handleDismiss}>
      <div className="editor-modal" role="dialog" aria-modal="true" aria-label={dialogLabel} onClick={(e) => e.stopPropagation()} data-testid="engi-score-editor">
        {inner}
      </div>
    </div>
  );
}

export { MAX_FLAGS, VALID_TOTALS, deriveWinner };
