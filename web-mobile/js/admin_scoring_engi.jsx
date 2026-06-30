// Engi-Kyogi (kata demonstration) score editor.
// Engi matches are scored by flag count: each judge raises a flag for the side
// they prefer. FIK rule: total flags must be ODD (1, 3, or 5) so no draw is
// possible. flagsA = Shiro (left/white), flagsB = Aka (right/red).
// The winner is the side with more flags.
// Exported: EngiScoreEditorModal.
// Loaded via admin_scoring_individual.jsx (NOT a separate script tag).

const { useState: useStateE } = React;

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
export function EngiScoreEditorModal({ match, onClose, onSubmit, canClose = true }) {
  const m = match;
  const [flagsA, setFlagsA] = useStateE(m.flagsA || 0);
  const [flagsB, setFlagsB] = useStateE(m.flagsB || 0);
  const [submitting, setSubmitting] = useStateE(false);
  const [err, setErr] = useStateE("");

  const total = flagsA + flagsB;
  const isValidTotal = VALID_TOTALS.has(total);
  const winnerSide = deriveWinner(flagsA, flagsB);
  const canSubmit = isValidTotal && winnerSide !== null && !submitting;

  // Pair names: member1 = sideA.name (or sideA string), member2 = sideA.displayName.
  // Both sides of an engi match are pairs (one entry each).
  const nameOf = (side) => side && typeof side === "object" ? side.name || "" : side || "";
  const displayOf = (side) => side && typeof side === "object" ? side.displayName || "" : "";
  const dojoOf = (side) => side && typeof side === "object" ? side.dojo || "" : "";

  const shiroName = nameOf(m.sideA);
  const shiroDN   = displayOf(m.sideA);
  const shiroDojo = dojoOf(m.sideA);
  const akaName   = nameOf(m.sideB);
  const akaDN     = displayOf(m.sideB);
  const akaDojo   = dojoOf(m.sideB);

  const clamp = (n) => Math.max(0, Math.min(MAX_FLAGS, n));

  const handleSubmit = async () => {
    if (!canSubmit) return;
    setSubmitting(true);
    setErr("");
    const payload = { flagsA, flagsB, status: "completed" };
    try {
      await onSubmit(payload);
    } catch (e) {
      setErr(e?.message || "Save failed");
      setSubmitting(false);
    }
  };

  // Outline styling: danger when total is non-zero but invalid.
  const invalidOutline = total > 0 && !isValidTotal;

  return (
    <div className="score-editor score-editor--engi" data-testid="engi-score-editor">
      {/* Header */}
      <div className="score-editor__header">
        <div className="score-editor__title">Engi Score</div>
        {canClose && (
          <button type="button" className="btn btn--icon" onClick={onClose} aria-label="Close" data-testid="engi-close-btn">
            ✕
          </button>
        )}
      </div>

      {/* Sides: Shiro (left) | Aka (right) */}
      <div className="engi-sides">
        {/* Shiro / White / sideA */}
        <div className={`engi-side engi-side--shiro${winnerSide === "a" ? " engi-side--winner" : ""}`} data-testid="engi-side-shiro">
          <div className="engi-side__badge engi-side__badge--shiro">Shiro</div>
          <div className="engi-side__names">
            <div className="engi-side__name">{shiroName}</div>
            {shiroDN && <div className="engi-side__name">{shiroDN}</div>}
            {shiroDojo && <div className="engi-side__dojo">{shiroDojo}</div>}
          </div>
          <PipRow count={flagsA} side="shiro" />
          <div className="engi-counter">
            <button
              type="button"
              className="btn engi-counter__btn"
              onClick={() => setFlagsA(clamp(flagsA - 1))}
              disabled={flagsA <= 0}
              aria-label="Shiro minus one flag"
              data-testid="engi-shiro-dec"
            >−</button>
            <span className="engi-counter__value" data-testid="engi-shiro-count">{flagsA}</span>
            <button
              type="button"
              className="btn engi-counter__btn"
              onClick={() => setFlagsA(clamp(flagsA + 1))}
              disabled={flagsA >= MAX_FLAGS}
              aria-label="Shiro plus one flag"
              data-testid="engi-shiro-inc"
            >+</button>
          </div>
        </div>

        <div className="engi-divider" aria-hidden="true">vs</div>

        {/* Aka / Red / sideB */}
        <div className={`engi-side engi-side--aka${winnerSide === "b" ? " engi-side--winner" : ""}`} data-testid="engi-side-aka">
          <div className="engi-side__badge engi-side__badge--aka">Aka</div>
          <div className="engi-side__names">
            <div className="engi-side__name">{akaName}</div>
            {akaDN && <div className="engi-side__name">{akaDN}</div>}
            {akaDojo && <div className="engi-side__dojo">{akaDojo}</div>}
          </div>
          <PipRow count={flagsB} side="aka" />
          <div className="engi-counter">
            <button
              type="button"
              className="btn engi-counter__btn"
              onClick={() => setFlagsB(clamp(flagsB - 1))}
              disabled={flagsB <= 0}
              aria-label="Aka minus one flag"
              data-testid="engi-aka-dec"
            >−</button>
            <span className="engi-counter__value" data-testid="engi-aka-count">{flagsB}</span>
            <button
              type="button"
              className="btn engi-counter__btn"
              onClick={() => setFlagsB(clamp(flagsB + 1))}
              disabled={flagsB >= MAX_FLAGS}
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
            ? `Total: ${total} — ${winnerSide === "a" ? "Shiro wins" : "Aka wins"}`
            : `Total: ${total} — must be 1, 3, or 5 (odd)`}
      </div>

      {/* Error */}
      {err && <div className="score-editor__err" role="alert">{err}</div>}

      {/* Submit */}
      <div className="score-editor__actions">
        <button
          type="button"
          className="btn btn--primary"
          onClick={handleSubmit}
          disabled={!canSubmit}
          data-testid="engi-submit"
          style={invalidOutline ? { outline: "2px solid var(--danger)" } : null}
        >
          {submitting ? "Saving…" : "Save result"}
        </button>
        {canClose && (
          <button type="button" className="btn" onClick={onClose} disabled={submitting}>
            Cancel
          </button>
        )}
      </div>
    </div>
  );
}

export { MAX_FLAGS, VALID_TOTALS, deriveWinner };
