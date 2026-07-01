// Engi-Kyogi (kata demonstration) score editor.
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

const { useState: useStateE } = React;

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

// Derive the winner side from flag counts. Returns "a" | "b" | null.
function deriveWinner(flagsA, flagsB) {
  if (flagsA > flagsB) return "a";
  if (flagsB > flagsA) return "b";
  return null;
}

// EngiScoreEditorModal: full engi flag-counter editor.
// Props mirror the individual ScoreEditorModal surface so the dispatch in
// admin_scoring_individual.jsx can forward the same prop bag.
export function EngiScoreEditorModal({ match, onClose, onSubmit, variant = "modal", canClose = true }) {
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

  const doSubmit = async (payload) => {
    setSubmitting(true);
    setErr("");
    try {
      await onSubmit(payload);
    } catch (e) {
      setErr(e?.message || "Save failed");
      setSubmitting(false);
    }
  };

  const handleSubmit = () => {
    if (!canSubmit) return;
    if (isComplete && !correctionReason) {
      setShowCorrectionPrompt(true);
      return;
    }
    // correctionReason persists in state once confirmed via ReasonPrompt, so
    // a retry after a failed first attempt (operator clicks "Save correction"
    // again without reopening the prompt) must still carry it: otherwise the
    // retry silently drops the audit reason the operator already gave.
    doSubmit({ flagsA, flagsB, status: "completed", ...(correctionReason ? { correctionReason } : {}) });
  };

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
              doSubmit({ flagsA, flagsB, status: "completed", correctionReason: r });
            }}
            onCancel={() => setShowCorrectionPrompt(false)}
          />
        )}
        <div className="score-nav__actions" style={{ marginLeft: "auto" }}>
          {canClose && <button type="button" className="btn" onClick={handleDismiss} disabled={submitting}>Cancel</button>}
          <button
            type="button"
            className="btn btn--primary"
            onClick={handleSubmit}
            disabled={!canSubmit}
            data-testid="engi-submit"
            style={invalidOutline ? { outline: "2px solid var(--danger)" } : null}
          >
            {submitting ? "Saving…" : isComplete ? "Save correction" : "Save result"}
          </button>
        </div>
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
