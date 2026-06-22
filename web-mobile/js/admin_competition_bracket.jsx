// admin_competition_bracket.jsx — AdminBracket section plus its RunningMatchPanel
// winner-picker detail panel and the pure scoreboard helpers they consume.
// Split out of admin_competition.jsx (mp-hpe3). buildRunningIpponResult and
// loadScoreboardPoints are ES-exported and re-exported by the entry for tests.

const { useState: useStateA, useEffect: useEffectA, useRef: useRefA } = React;

const hasBothSides = window.hasBothSides;
const hasPoolOriginPlaceholder = window.hasPoolOriginPlaceholder;
const CourtPicker = window.CourtPicker;

// Pure result-builder for AdminBracket.recordWinner. Captures the schema
// for a completed-ippon result so it's unit-testable and can't drift
// from the canonical shape that admin_scoring_modal.jsx's buildPatch
// produces (which the backend persists via /recordScore).
//
// Inputs:
//   winnerSide:    "a" | "b"
//   sideA, sideB:  match.sideA / match.sideB (each {id, name, ...})
//   winnerIppons:  array of letter codes ("M","K","D","T","H") the
//                  winning side scored. Empty/missing → ["M"] (the
//                  tap-mode default: a single unspecified ippon).
//   loserIppons:   array of letter codes the losing side scored.
//                  Empty by default; tap/card modes don't expose
//                  loser points.
//
// Output: the result object POST'd to /recordScore.
//
// Copilot finding (PR #103): the scoreboard mode supports 2-ippon wins
// but the previous recordWinner only ever recorded a 1-ippon result
// (winnerPts=1, single-letter ippons array). A 2-ippon match was
// silently truncated to 1 ippon. Fix lifts winnerPts/ipponsA/ipponsB
// from the full array; loserIppons is now first-class too so a 2–1
// win records the loser's ippon instead of dropping it.
//
// Kendo win conditions this helper covers (one side strictly leads):
//   - 2 ippons (sansoo) → automatic win
//   - 1 ippon at time-up → valid win when opponent has 0
//   - 2-1 at time-up → valid win when opponent has 1
// Tied counts (0-0, 1-1, 2-2) are not wins; the scoreboard's Submit
// button is disabled in those states and the operator routes the
// match through the full editor's hikiwake toggle instead.
//
// Exported for vitest at __tests__/admin_competition.test.jsx.
function buildRunningIpponResult(winnerSide, sideA, sideB, winnerIppons, loserIppons) {
  const winner = winnerSide === "a" ? sideA : sideB;
  const winnerLetters = (winnerIppons && winnerIppons.length > 0) ? winnerIppons : ["M"];
  const loserLetters = loserIppons || [];
  return {
    winner,
    status: "completed",
    ipponsA: winnerSide === "a" ? winnerLetters : loserLetters,
    ipponsB: winnerSide === "b" ? winnerLetters : loserLetters,
    score: {
      type: "ippon",
      winnerPts: winnerLetters.length,
      loserPts: loserLetters.length,
      ippons: winnerLetters,
      fouls: { a: 0, b: 0 },
    },
  };
}

// Pure loader for RunningMatchPanel's scoreboard-mode aPoints/bPoints from
// a (possibly completed) match. Reads each side's letters DIRECTLY from
// match.ipponsA / match.ipponsB rather than from score.ippons (which is
// only the winner's letters, by buildPatch / normalizeMatch convention).
//
// Bug fix companion to buildRunningIpponResult: the previous loader gated
// on `winner.id === sideX.id` and pulled from `score.ippons`, which
// returned only the winner's letters. That was fine when the writer
// always recorded loser=[]; once buildRunningIpponResult started writing
// 2-1 wins correctly (loser's single ippon preserved), the loader's
// truncation surfaced — a 2-1 win came back as 2-0 on re-render and
// re-submission silently dropped the loser's letter.
//
// admin_scoring_modal.jsx's initialAPts at line 30-31 already used the
// `m.ipponsA?.filter(...)` pattern; this helper aligns the running panel
// with the same shape.
//
// "•" placeholders are filtered (the full editor uses "•" as an empty
// slot marker; running panel doesn't write those but the data round-trips
// through the same backend fields so filtering is defensive).
//
// Exported for vitest at __tests__/admin_competition.test.jsx.
function loadScoreboardPoints(match) {
  if (!match) return { aPoints: [], bPoints: [] };
  return {
    aPoints: (match.ipponsA || []).filter(x => x && x !== "•"),
    bPoints: (match.ipponsB || []).filter(x => x && x !== "•"),
  };
}

const RunningMatchPanel = React.memo(({ match, compId, courts, onMoveCourt, onRecord, onOverride, onEditScore, password }) => {
  const [mode, setMode] = useStateA("tap");
  // The "Scoreboard" tab embeds the ONE shared scoring editor
  // (ScoreEditorModal, variant="inline") — the same component the shiaijo
  // operator view and the pools/scores editors use — rather than a bespoke
  // scoreboard. Pulled off the window bridge (admin_scoring_modal.jsx) at
  // render time so load order with this module never matters.
  const ScoreEditorModal = window.ScoreEditorModal;
  const a = match.sideA, b = match.sideB;
  const isComplete = match.status === "completed";
  return (
    <div className="running-panel">
      <div className="running-panel__head">
        <div className="running-panel__title">Match · {match.id.slice(-6)}</div>
        <div className="running-panel__court">
          {onMoveCourt && courts && courts.length ? (
            <>
              <CourtPicker
                value={match.court}
                courts={courts}
                onChange={(cc) => onMoveCourt(compId, match.id, cc)}
                btnClassName="running-panel__court-btn"
                label="SHIAIJO "
              />
              <span> · {match.scheduledAt || "TBA"}</span>
            </>
          ) : (
            <span>SHIAIJO {match.court} · {match.scheduledAt || "TBA"}</span>
          )}
        </div>
      </div>
      <div className="mode-tabs">
        <button type="button" className={mode === "tap" ? "is-active" : ""} onClick={() => setMode("tap")}>Tap winner</button>
        <button type="button" className={mode === "card" ? "is-active" : ""} onClick={() => setMode("card")}>Match card</button>
        <button type="button" className={mode === "scoreboard" ? "is-active" : ""} onClick={() => setMode("scoreboard")}>Scoreboard</button>
      </div>
      {mode === "tap" && (<>
        {/* Layout convention: SHIRO (White, sideB) on the LEFT, AKA (Red, sideA) on the RIGHT. */}
        <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 8, marginBottom: 10 }}>
          <button type="button" className="card" style={{ padding: 16, textAlign: "center", cursor: "pointer", borderColor: match.winner?.id === b.id ? "var(--accent)" : "var(--line)", background: match.winner?.id === b.id ? "var(--accent)" : "var(--surface)", color: match.winner?.id === b.id ? "white" : "inherit" }} onClick={() => onRecord("b", "ippon")}>
            <div style={{ fontSize: 10, fontWeight: 700, opacity: 0.7, letterSpacing: "0.1em" }}>SHIRO (WHITE)</div>
            <div style={{ fontWeight: 600, fontSize: 15, marginTop: 6 }}>{b.name}</div>
            <div style={{ fontSize: 12, opacity: 0.8, marginTop: 2 }}>{b.dojo}</div>
          </button>
          <button type="button" className="card" style={{ padding: 16, textAlign: "center", cursor: "pointer", borderColor: match.winner?.id === a.id ? "var(--red)" : "var(--line)", background: match.winner?.id === a.id ? "var(--red)" : "var(--surface)", color: match.winner?.id === a.id ? "white" : "inherit" }} onClick={() => onRecord("a", "ippon")}>
            {/* Label tinted red when unselected (button is on white), inherits white when selected (button background is red) */}
            <div style={{ fontSize: 10, fontWeight: 700, opacity: 0.7, letterSpacing: "0.1em", color: match.winner?.id === a.id ? "inherit" : "var(--red)" }}>AKA (RED)</div>
            <div style={{ fontWeight: 600, fontSize: 15, marginTop: 6 }}>{a.name}</div>
            <div style={{ fontSize: 12, opacity: 0.8, marginTop: 2 }}>{a.dojo}</div>
          </button>
        </div>
        <div className="field__hint" style={{ textAlign: "center" }}>Tap the winner. Use Match card or Scoreboard for detail.</div>
      </>)}
      {mode === "card" && (
        <div className="score-card">
          <div className="score-side score-side--white">
            <div><div className="score-side__lbl">Shiro (White)</div><div className="score-side__name">{b.name}</div><div className="score-side__dojo">{b.dojo}</div></div>
            <div className="score-side__buttons"><button type="button" className="btn btn--sm btn--primary" onClick={() => onRecord("b", "ippon", ["M"])}>Win (Ippon)</button></div>
          </div>
          <div className="score-vs">VS</div>
          <div className="score-side score-side--red">
            <div><div className="score-side__lbl">Aka (Red)</div><div className="score-side__name">{a.name}</div><div className="score-side__dojo">{a.dojo}</div></div>
            <div className="score-side__buttons"><button type="button" className="btn btn--sm btn--danger" onClick={() => onRecord("a", "ippon", ["M"])}>Win (Ippon)</button></div>
          </div>
        </div>
      )}
      {mode === "scoreboard" && ScoreEditorModal && (
        // Reuse the shared inline scoring editor — full FIK scoreboard with
        // ippons, draws (hikiwake), hantei, fouls, encho and kiken/fusenpai
        // decisions — instead of a bespoke board. onSubmit is wired exactly as
        // the pools/shiaijo embeddings: onEditScore(compId, matchId, patch, match).
        <ScoreEditorModal
          key={`${match.id}:${match.status}`}
          variant="inline"
          match={match}
          onClose={() => {}}
          canClose={false}
          onSubmit={async (patch) => {
            try { await onEditScore(compId, match.id, patch, match); }
            catch (_e) { /* surfaced via toast in the parent */ }
          }}
          onSubmitAndNext={null}
          password={password}
        />
      )}
      {isComplete && (
        <div style={{ marginTop: 12, padding: 10, background: "#ecfdf5", border: "1px solid #a7f3d0", borderRadius: 8, fontSize: 12.5, color: "#065f46" }}>
          ✓ Recorded — {match.winner?.name} advances
        </div>
      )}
      <div style={{ marginTop: 14, paddingTop: 14, borderTop: "1px dashed var(--line)" }}>
        <button type="button" className="btn btn--sm btn--full" onClick={async () => {
          // promptDialog returns null on empty/cancel and a string otherwise.
          // A whitespace-only value would be truthy under `if (name)` and would
          // persist a whitespace key as `m.Winner` on the backend (and then
          // mismatch the canonical SideA / SideB names downstream). Trim
          // defensively and only override when there's a real value to record.
          const raw = await window.promptDialog({ title: "Override winner", message: "Enter the name of the winner to override:", defaultValue: match.winner?.name || match.sideA?.name });
          const name = raw?.trim();
          if (name) onOverride(name);
        }}>Force winner (manual override)</button>
      </div>
    </div>
  );
});
RunningMatchPanel.displayName = "RunningMatchPanel";
function AdminBracket({ c, t, bracket, onMoveCourt, onEditScore, tweaks, password, showToast }) {
  const [selected, setSelected] = useStateA(null);
  const scrollRef = useRefA(null);
  const [autoScrollId, setAutoScrollId] = useStateA(null);

  // Recenter on the running match whenever it changes (initial bracket
  // load, or one match finishing and the next starting). Empty deps would
  // miss the case where `bracket` is still null on first mount and only
  // populates via the detail fetch / SSE.
  const runningMatchId = (bracket?.rounds || []).flatMap(r => r || []).find(m => m && m.status === "running")?.id || null;
  useEffectA(() => {
    if (runningMatchId) setAutoScrollId(runningMatchId + "::" + Date.now());
  }, [runningMatchId]);

  if (!bracket || !bracket.rounds) {
    const previewMode = c && c.status === "draw-ready";
    return <div className="empty"><div className="icon">⚙</div><h3>Bracket not generated yet</h3><div>{previewMode ? "Bracket not available for this format preview." : "Start the competition to build the bracket."}</div></div>;
  }
  const select = (m, ri, mi) => setSelected({ matchId: m.id, ri, mi });
  // Look up the selected match by ID rather than [ri][mi] index. The
  // index can go stale if an SSE-driven bracket rebuild (playoff
  // regeneration, source-comp promotion) reorders entries between the
  // user's click and the next render/action; the ID is the only stable
  // handle we set in `selected`. Returns null when the match has been
  // removed entirely from the bracket.
  const findSelectedMatch = () => {
    if (!selected || !bracket?.rounds) return null;
    for (const round of bracket.rounds) {
      for (const m of (round || [])) {
        if (m && m.id === selected.matchId) return m;
      }
    }
    return null;
  };
  // winnerIppons/loserIppons are arrays of letter codes. Tap mode (no
  // detail) and card mode (single explicit letter) pass a single-element
  // array; scoreboard mode passes the full points it accumulated for
  // each side. See buildRunningIpponResult above for the schema rationale.
  const recordWinner = (winnerSide, _mode = "ippon", winnerIppons = ["M"], loserIppons = []) => {
    const m = findSelectedMatch();
    if (!m) return;
    const winner = winnerSide === "a" ? m.sideA : m.sideB;
    if (!winner) return;

    const result = buildRunningIpponResult(winnerSide, m.sideA, m.sideB, winnerIppons, loserIppons);

    // Don't call onUpdate(c) on success — AdminApp's onUpdate is the
    // competition-config PUT, which would overwrite server state with
    // the (now-stale) c prop. SSE + patchCompetitionData in AdminApp
    // already refreshes the bracket after a recordScore.
    window.API.recordScore(c.id, m.id, result, password, m)
      .catch(err => showToast(err.message, "error"));
  };

  const overrideWinner = (winnerName) => {
    if (!selected) return;
    // Same reason as recordWinner: rely on SSE to refresh, don't
    // route the success path through the config-PUT callback.
    window.API.overrideBracketWinner(c.id, selected.matchId, winnerName, password)
      .catch(err => showToast(err.message, "error"));
  };
  const selectedMatch = findSelectedMatch();
  // mp-turx: per-match playability — a bracket match is running iff hasBothSides()
  // returns true (both sides are resolved real participants, not “Winner of rX-mY”
  // feeders or pool-origin placeholders like “Pool A-1st”). The old bracket-wide
  // `isPreview` gate is replaced: the tree always renders, and individual matches
  // are non-interactive until their sides are decided. This supports the incremental
  // fill-in model where pool finishers seed into the knockout as each pool completes.
  // Show the "filling in" banner ONLY when pool-origin placeholders remain (a
  // mixed comp whose feeder pools haven't all finished) — NOT for ordinary
  // "Winner of rX-mY" feeders or structural byes, which standalone playoffs and
  // bye-containing brackets legitimately have.
  const hasUnseededPools = bracket.rounds.some(r => (r || []).some(m => hasPoolOriginPlaceholder(m)));
  return (
    // Flex-wrap rather than a fixed 2-col grid (see .bracket-layout in
    // styles.css): the scoring panel keeps a minimum width so the reused inline
    // editor never squashes/clips, and when the bracket + that min width can't
    // sit side by side, the panel wraps to a full-width row BELOW the bracket.
    <div className="bracket-layout">
      <div className="bracket-layout__bracket">
        {hasUnseededPools && (
          <div className="banner banner--info" style={{ marginBottom: 12, padding: "10px 14px", background: "var(--accent-soft)", border: "1px solid var(--accent)", borderRadius: 6, fontSize: 13 }}>
            <strong>Knockout filling in</strong> — this bracket fills in automatically as each pool finishes. Matches start once both sides are decided.
          </div>
        )}
        <div className="bracket-canvas" ref={scrollRef}>
          <div className="bracket-canvas__inner">
            <window.BracketTree
              rounds={bracket.rounds}
              variant={tweaks.cardVariant}
              showDojo={tweaks.showDojo}
              onMatchClick={(m, ri, mi) => { if (hasBothSides(m)) select(m, ri, mi); }}
              highlightedMatchId={selected?.matchId}
              autoScrollMatchId={autoScrollId}
              scrollContainerRef={scrollRef}
            />
          </div>
        </div>
      </div>
      <div className="bracket-layout__panel">
        {hasBothSides(selectedMatch) ? (
          <RunningMatchPanel
            match={selectedMatch}
            compId={c.id}
            courts={t?.courts || []}
            onMoveCourt={onMoveCourt}
            onRecord={recordWinner}
            onOverride={overrideWinner}
            onEditScore={onEditScore}
            password={password}
          />
        ) : selectedMatch ? (
          <div className="empty"><h3>Match not ready</h3><div style={{ fontSize: 13 }}>Waiting for upstream winners.</div></div>
        ) : (
          <div className="empty"><div className="icon">👆</div><h3>Pick a match</h3><div style={{ fontSize: 13 }}>Click any match in the bracket to record results.</div></div>
        )}
      </div>
    </div>
  );
}

window.AdminBracket = AdminBracket;

export { buildRunningIpponResult, loadScoreboardPoints };
