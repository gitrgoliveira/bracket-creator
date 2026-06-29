// admin_competition_swiss.jsx: Swiss-round management section (AdminSwissRounds)
// and its pure round predicates. Split out of admin_competition.jsx (mp-hpe3).
// The predicates are ES-exported and re-exported by the entry for the vitest suite.

const { useState: useStateA, useEffect: useEffectA, useRef: useRefA } = React;

// T191 (US13, FR-050d): pure helpers for the Swiss-round admin
// section. Extracted so the conditional logic ("which round, are
// matches complete, can we generate next?") is unit-testable without
// mounting AdminSwissRounds. Mirrors the admin_scoring_modal.jsx
// pattern (buildDecisionBody / shouldShowEnchoMaxBanner pure helpers
// exported for tests).

// Returns the canonical match-ID prefix for a Swiss round. Matches
// engine/swiss.go's `swissPoolName`/`swissMatchID`. Keep in sync.
function swissRoundIDPrefix(round) {
  return `Swiss-R${round}-`;
}

// Filter the competition's pool-matches list down to a single
// Swiss round's matches. Returns [] for non-Swiss formats and for
// rounds that have not been generated yet.
function filterSwissRoundMatches(poolMatches, round) {
  if (!poolMatches || !Array.isArray(poolMatches) || !round || round < 1) return [];
  const prefix = swissRoundIDPrefix(round);
  return poolMatches.filter(m => (m.id || "").startsWith(prefix));
}

// Returns true when every match in `matches` has status "completed".
// Returns false for an empty list (an unbegun round is not "complete")
// from the admin's perspective; the Generate Next Round button stays
// disabled until the round exists AND every match in it is done.
function isSwissRoundComplete(matches) {
  if (!matches || matches.length === 0) return false;
  return matches.every(m => m.status === "completed");
}

// Returns true when the operator should see the "Generate next round"
// button enabled. The conditions: format=swiss, current round generated
// (and complete), and we haven't reached the final round yet.
function canGenerateNextSwissRound(comp, currentRoundMatches) {
  if (!comp || comp.format !== "swiss") return false;
  const total = comp.swissRounds || 0;
  const current = comp.swissCurrentRound || 0;
  if (total < 1) return false;
  if (current >= total) return false;
  // First round is generated on competition start; if currentRound is
  // 0 (status=setup) we never enable Generate Next. Operator should
  // hit "Start competition" first. After start, current >= 1 and we
  // require the current round to be complete to advance.
  if (current < 1) return false;
  return isSwissRoundComplete(currentRoundMatches);
}

// T193 (FR-050e): once every configured round has been generated and
// completed, the admin page hides the Generate button and surfaces a
// "Competition complete. View final standings" link. `currentRound
// >= swissRounds` AND every match in the final round done.
function isSwissCompetitionComplete(comp, currentRoundMatches) {
  if (!comp || comp.format !== "swiss") return false;
  const total = comp.swissRounds || 0;
  const current = comp.swissCurrentRound || 0;
  if (total < 1 || current < total) return false;
  return isSwissRoundComplete(currentRoundMatches);
}

function AdminSwissRounds({ c, poolMatches, password, onViewStandings, showToast }) {
  const [generating, setGenerating] = useStateA(false);
  const [genError, setGenError] = useStateA(null);
  const mountedRef = useRefA(true);
  useEffectA(() => () => { mountedRef.current = false; }, []);

  // Reset the inline error whenever the round changes or new matches
  // land via SSE (the prior "current round incomplete" message is
  // not meaningful once the operator has moved on.
  useEffectA(() => { setGenError(null); }, [c.swissCurrentRound, (poolMatches || []).length]);

  const currentRound = c.swissCurrentRound || 0;
  const totalRounds = c.swissRounds || 0;
  const currentMatches = filterSwissRoundMatches(poolMatches, currentRound);
  const complete = isSwissRoundComplete(currentMatches);
  const canGenerate = canGenerateNextSwissRound(c, currentMatches);
  const allDone = isSwissCompetitionComplete(c, currentMatches);

  const generate = async () => {
    setGenerating(true);
    setGenError(null);
    try {
      await window.API.swissGenerateRound(c.id, password);
      // SSE swiss_round_generated will trigger AdminApp's refetch.
      // no local-state mutation needed. Surface a toast so the
      // operator sees confirmation in case the SSE is slow.
      if (!mountedRef.current) return;
      if (showToast) showToast(`Round ${currentRound + 1} generated`);
    } catch (e) {
      if (!mountedRef.current) return;
      // 409 / round_incomplete is a known operator-error condition;
      // surface it inline rather than as a generic toast.
      if (e.code === "round_incomplete") {
        setGenError("Cannot generate. Current round still has incomplete matches.");
      } else {
        setGenError(e.message || "Failed to generate next round");
        if (showToast) showToast(e.message || "Failed to generate next round", "error");
      }
    } finally {
      if (mountedRef.current) setGenerating(false);
    }
  };

  // Setup state (status === "setup"). Nudge the operator to hit
  // Start so round 1 is generated.
  if (c.status === "setup") {
    return (
      <div className="card" style={{ padding: 14 }}>
        <div className="card__title" style={{ marginBottom: 6 }}>Swiss rounds</div>
        <div className="card__sub">{totalRounds} rounds configured. Round 1 will be generated when you start the competition.</div>
      </div>
    );
  }

  return (
    <div className="card" style={{ padding: 14 }}>
      <div className="card__head" style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 10 }}>
        <div className="card__title">Round {currentRound} of {totalRounds}</div>
        <div style={{ fontSize: 12, color: "var(--ink-3)" }}>
          {currentMatches.filter(m => m.status === "completed").length}/{currentMatches.length} matches complete
        </div>
      </div>

      {/* Current-round match list (id + sides + status). Kept compact. */}
      {/* the full edit experience lives in the Scores section. */}
      {currentMatches.length > 0 && (
        <table className="pool__table" style={{ marginBottom: 10 }}>
          <thead><tr><th style={{ width: 28 }}>#</th><th>White (Shiro)</th><th>Red (Aka)</th><th style={{ width: 80 }}>Court</th><th style={{ width: 100 }}>Status</th></tr></thead>
          <tbody>
            {currentMatches.map((m, i) => (
              <tr key={m.id}>
                <td style={{ color: "var(--ink-3)", fontFamily: "var(--font-mono)" }}>{i + 1}</td>
                <td>{m.sideB?.name || "n/a"}</td>
                <td>{m.sideA?.name || "n/a"}</td>
                <td style={{ fontFamily: "var(--font-mono)" }}>{m.court || "n/a"}</td>
                <td style={{ fontSize: 12, color: m.status === "completed" ? "var(--accent)" : m.status === "running" ? "var(--accent)" : "var(--ink-3)" }}>
                  {m.status === "completed" ? "Done" : m.status === "running" ? "Now" : "Scheduled"}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      {/* T193: when all rounds complete, hide the Generate button and */}
      {/* surface the final-standings link. */}
      {allDone ? (
        <div style={{ display: "flex", flexDirection: "column", gap: 8, padding: 12, background: "var(--accent-soft, #ecfdf5)", border: "1px solid var(--accent, #a7f3d0)", borderRadius: 8 }}>
          <div style={{ fontWeight: 600, color: "var(--accent, #065f46)" }}>Competition complete</div>
          {onViewStandings && (
            <button type="button" className="btn btn--primary btn--sm" onClick={onViewStandings}>View final standings →</button>
          )}
        </div>
      ) : (
        <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
          <button type="button"
            className="btn btn--primary"
            disabled={!canGenerate || generating}
            onClick={generate}
          >
            {generating && <span className="spinner" />}
            {generating ? "Generating…" : `Generate round ${currentRound + 1}`}
          </button>
          {!complete && currentMatches.length > 0 && (
            <div className="field__hint">Finish the remaining matches in round {currentRound} before generating the next round.</div>
          )}
          {genError && <div className="alert alert--error" style={{ fontSize: 13 }}>{genError}</div>}
        </div>
      )}
    </div>
  );
}


window.AdminSwissRounds = AdminSwissRounds;

export {
  swissRoundIDPrefix,
  filterSwissRoundMatches,
  isSwissRoundComplete,
  canGenerateNextSwissRound,
  isSwissCompetitionComplete,
};
