// admin_competition_bracket.jsx: AdminBracket section plus its RunningMatchPanel
// winner-picker detail panel and the pure scoreboard helpers they consume.
// Split out of admin_competition.jsx (mp-hpe3). buildRunningIpponResult and
// loadScoreboardPoints are ES-exported and re-exported by the entry for tests.

const { useState: useStateA, useEffect: useEffectA, useRef: useRefA } = React;

const hasBothSides = window.hasBothSides;
const hasPoolOriginPlaceholder = window.hasPoolOriginPlaceholder;
const CourtPicker = window.CourtPicker;
const EmptyState = window.EmptyState;

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
// truncation surfaced: a 2-1 win came back as 2-0 on re-render and
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

const RunningMatchPanel = React.memo(({ match, compId, courts, matchNum, onMoveCourt, onEditScore, password }) => {
  // Reuse the shared components wholesale: both off the window bridge at render
  // time so module load order never matters:
  //   ScoreEditorModal (variant="inline"): the ONE scoring editor the shiaijo /
  //     pools / scores screens use. We keep ITS header (it's the same in every
  //     host), so the panel doesn't add a competing title. `match` is enriched
  //     upstream with the comp metadata the editor needs (compId, compKind,
  //     teamSize, phase, round): see AdminBracket.scoringMatch.
  //   MatchDetailCard: the SAME read-only result card the public viewer shows
  //     for a completed match (names + winner highlight + FINAL + score marks),
  //     instead of a stripped-down lookalike.
  const ScoreEditorModal = window.ScoreEditorModal;
  const MatchDetailCard = window.MatchDetailCard;
  const isComplete = match.status === "completed";
  // A completed match shows its read-only result by default; re-opening the
  // editor to change a recorded result is gated behind a confirmation so it's
  // not changed by accident. An un-played match goes straight to the editor
  // (nothing to overwrite). Reset when switching matches.
  const [editing, setEditing] = useStateA(false);
  useEffectA(() => { setEditing(false); }, [match.id]);
  const showEditor = !isComplete || editing;

  return (
    <div className="running-panel">
      {/* The card fills the row to align with the bracket card above; this inner
          column caps the content to a comfortable width and centres it so the
          reused editor / result card aren't stretched edge-to-edge. */}
      <div className="running-panel__inner">
      {/* Slim header: only the bits the reused components' own headers DON'T
          carry: the bracket match number and the move-court control. Comp /
          shiaijo / round / time / status come from the embedded component. */}
      <div className="running-panel__head">
        <div className="running-panel__title">{matchNum != null ? `Match M${matchNum}` : `Match · ${match.id.slice(-6)}`}</div>
        {onMoveCourt && courts && courts.length ? (
          <div className="running-panel__court">
            <CourtPicker
              value={match.court}
              courts={courts}
              onChange={(cc) => onMoveCourt(compId, match.id, cc)}
              btnClassName="running-panel__court-btn"
              label="SHIAIJO "
            />
          </div>
        ) : null}
      </div>

      {showEditor ? (
        ScoreEditorModal && (
          <ScoreEditorModal
            // Include subResults.length: the editor reads team sub-bouts into
            // refs on mount, so an SSE update that adds/removes a daihyosen
            // sub-bout from another surface must remount to avoid a stale board
            // (mirrors the shiaijo inline embed's key: admin_shiaijo.jsx).
            key={`${match.id}:${match.status}:${(match.subResults || []).length}`}
            variant="inline"
            match={match}
            // Offer "close" only when there's a result card to fall back to
            // (editing a completed match); an un-played match has nowhere to go.
            canClose={isComplete}
            onClose={() => setEditing(false)}
            onSubmit={async (patch) => {
              try {
                await onEditScore(compId, match.id, patch, match);
                setEditing(false); // recorded → fall back to the result card
              } catch (_e) { /* surfaced via toast in the parent */ }
            }}
            onSubmitAndNext={null}
            password={password}
          />
        )
      ) : (
        <div className="running-panel__result">
          {MatchDetailCard && <MatchDetailCard match={match} escapeToClose={false} />}
          <button type="button" className="btn btn--sm btn--full" onClick={async () => {
            const ok = await window.confirmDialog({
              title: "Edit recorded result?",
              message: `${matchNum != null ? `Match M${matchNum}` : "This match"} already has a result. Re-open scoring to change it?`,
              confirmLabel: "Edit result",
            });
            if (ok) setEditing(true);
          }}>Edit result</button>
        </div>
      )}
      </div>
    </div>
  );
});
RunningMatchPanel.displayName = "RunningMatchPanel";
function AdminBracket({ c, t, bracket, onMoveCourt, onEditScore, tweaks, password }) {
  const [selected, setSelected] = useStateA(null);
  const scrollRef = useRefA(null);
  const [autoScrollId, setAutoScrollId] = useStateA(null);

  // Recenter on the running match whenever it changes (initial bracket
  // load, or one match finishing and the next starting). Empty deps would
  // miss the case where `bracket` is still null on first mount and only
  // populates via the detail fetch / SSE. Also check thirdPlaceMatch.
  const runningMatchId = (() => {
    const inRounds = (bracket?.rounds || []).flatMap(r => r || []).find(m => m && m.status === "running");
    if (inRounds) return inRounds.id;
    const bronze = bracket?.thirdPlaceMatch;
    if (bronze && bronze.status === "running") return bronze.id;
    return null;
  })();
  useEffectA(() => {
    if (runningMatchId) setAutoScrollId(runningMatchId + "::" + Date.now());
  }, [runningMatchId]);

  // Label a selected match with the SAME number ("M1") and round the bracket
  // tree shows: derived from the shared buildDisplayModel so the panel and the
  // cards/columns can never disagree. Recomputed from bracket.rounds (not stored
  // at click time) so it survives SSE topology refreshes. Hook stays above the
  // early return below so the hook order is stable.
  const displayModel = React.useMemo(
    () => (bracket?.rounds && window.buildDisplayModel ? window.buildDisplayModel(bracket.rounds) : { hasMeta: false, matchNumById: null }),
    [bracket]
  );
  const matchMeta = (matchId) => {
    if (!matchId || !bracket?.rounds) return { matchNum: null, roundName: null };
    const cols = displayModel.hasMeta ? displayModel.columns : bracket.rounds;
    let ci = -1;
    for (let i = 0; i < cols.length; i++) {
      if ((cols[i] || []).some((m) => m && m.id === matchId)) { ci = i; break; }
    }
    return {
      matchNum: displayModel.matchNumById ? displayModel.matchNumById[matchId] : null,
      roundName: (ci >= 0 && window.roundLabel) ? window.roundLabel(ci, cols.length) : null,
    };
  };

  if (!bracket || !bracket.rounds) {
    const previewMode = c && c.status === "draw-ready";
    return <EmptyState icon="⚙" title="Bracket not generated yet" message={previewMode ? "Bracket not available for this format preview." : "Start the competition to build the bracket."} />;
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
    // Also check the bronze/3rd-place match (naginata competitions).
    const bronze = bracket?.thirdPlaceMatch;
    if (bronze && bronze.id === selected.matchId) return bronze;
    return null;
  };
  // All bracket scoring now flows through the shared inline ScoreEditorModal →
  // onEditScore (the same path pools / scores / shiaijo use), so the bespoke
  // recordScore / overrideBracketWinner entry points are gone from this panel.
  const selectedMatch = findSelectedMatch();
  // Compute the match's number/round ONCE (one scan of the display model).
  const selectedMeta = selectedMatch ? matchMeta(selectedMatch.id) : { matchNum: null, roundName: null };
  // Enrich the bracket match with the competition metadata the shared scorer
  // (ScoreEditorModal / MatchDetailCard) reads off the match object: the raw
  // bracket.rounds entries carry none of it. Mirrors enrichPoolMatchWithComp:
  //   compId      → decision endpoints + maxEncho/naginata fetch
  //   compKind /  → individual vs team editor routing
  //   teamSize
  //   phase       → "bracket" makes isKnockoutPhase true: blocks hikiwake (no
  //                 draws in elimination: decide by hantei after encho)
  //   compFormat, round, matchNumber → header/label fields
  const scoringMatch = selectedMatch && {
    ...selectedMatch,
    compId: selectedMatch.compId || c.id || "",
    compName: selectedMatch.compName || c.name || "",
    compFormat: selectedMatch.compFormat || c.format || "",
    compKind: selectedMatch.compKind || c.kind || "",
    teamSize: selectedMatch.teamSize ?? c.teamSize ?? 0,
    compEngi: !!(selectedMatch.compEngi ?? c.engi),
    phase: selectedMatch.phase || "bracket",
    round: selectedMatch.round || selectedMeta.roundName,
    matchNumber: selectedMatch.matchNumber || selectedMeta.matchNum || 0,
  };
  // mp-turx: per-match playability: a bracket match is running iff hasBothSides()
  // returns true (both sides are resolved real participants, not “Winner of rX-mY”
  // feeders or pool-origin placeholders like “Pool A-1st”). The old bracket-wide
  // `isPreview` gate is replaced: the tree always renders, and individual matches
  // are non-interactive until their sides are decided. This supports the incremental
  // fill-in model where pool finishers seed into the knockout as each pool completes.
  // Show the "filling in" banner ONLY when pool-origin placeholders remain (a
  // mixed comp whose feeder pools haven't all finished): NOT for ordinary
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
            <strong>Knockout filling in</strong>: this bracket fills in automatically as each pool finishes. Matches start once both sides are decided.
          </div>
        )}
        <div className="bracket-canvas" ref={scrollRef}>
          <div className="bracket-canvas__inner">
            <window.BracketTree
              rounds={bracket.rounds}
              isEngi={!!c.engi}
              variant={tweaks.cardVariant}
              showDojo={tweaks.showDojo}
              onMatchClick={(m, ri, mi) => { if (hasBothSides(m)) select(m, ri, mi); }}
              highlightedMatchId={selected?.matchId}
              autoScrollMatchId={autoScrollId}
              scrollContainerRef={scrollRef}
            />
            {bracket.thirdPlaceMatch && (() => {
              const bm = bracket.thirdPlaceMatch;
              const isReady = hasBothSides(bm);
              const isHighlighted = selected?.matchId === bm.id;
              // The "3rd Place Match" section header identifies the lone bronze
              // card, so the card omits a per-card meta badge (no redundant
              // "3RD" repeating the header). It renders smaller and offset UNDER
              // the final match card (bronzeUnderFinalStyle) — the two "end"
              // matches read together, and it can't be misread as a semifinal.
              return (
                <div className="bracket-bronze-section" style={{ marginTop: 28, ...window.bronzeUnderFinalStyle(bracket.rounds) }} data-testid="bracket-bronze-match">
                  <div className="bracket-bronze-label">
                    3rd Place Match
                  </div>
                  <window.MatchCard
                    match={bm}
                    isEngi={!!c.engi}
                    variant={tweaks.cardVariant}
                    showDojo={tweaks.showDojo}
                    highlighted={isHighlighted}
                    onClick={isReady ? () => select(bm, -1, 0) : undefined}
                  />
                </div>
              );
            })()}
          </div>
        </div>
      </div>
      <div className="bracket-layout__panel">
        {hasBothSides(selectedMatch) ? (
          <RunningMatchPanel
            match={scoringMatch}
            compId={c.id}
            courts={t?.courts || []}
            matchNum={selectedMeta.matchNum}
            onMoveCourt={onMoveCourt}
            onEditScore={onEditScore}
            password={password}
          />
        ) : selectedMatch ? (
          <EmptyState title="Match not ready" message="Waiting for upstream winners." />
        ) : (
          <EmptyState icon="👆" title="Pick a match" message="Click any match in the bracket to record results." />
        )}
      </div>
    </div>
  );
}

window.AdminBracket = AdminBracket;

export { buildRunningIpponResult, loadScoreboardPoints };
