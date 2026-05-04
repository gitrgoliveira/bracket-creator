// Shared bracket rendering with SVG connector overlay.
// Connectors are drawn after layout via an effect that measures actual match
// card positions, so they always line up correctly regardless of card height.

const { useRef, useLayoutEffect: useLayoutEffectBC, useState: useStateBC, useEffect: useEffectBC } = React;

function roundLabel(roundIdx, total) {
  const fromEnd = total - 1 - roundIdx;
  if (fromEnd === 0) return "Final";
  if (fromEnd === 1) return "Semifinals";
  if (fromEnd === 2) return "Quarterfinals";
  if (fromEnd === 3) return "Round of 16";
  if (fromEnd === 4) return "Round of 32";
  return `Round ${roundIdx + 1}`;
}

function PlayerLine({ player, isWinner, side, showDojo, score, isTBD }) {
  if (!player || isTBD) {
    return (
      <div className={`bc-side bc-side--empty bc-side--${side}`}>
        <span className="bc-name bc-name--tbd">{isTBD ? "TBD" : "—"}</span>
      </div>
    );
  }
  return (
    <div className={`bc-side bc-side--${side} ${isWinner ? "bc-side--winner" : ""}`}>
      {player.seed ? <span className="bc-seed">{player.seed}</span> : <span className="bc-seed bc-seed--empty"></span>}
      <div className="bc-name-wrap">
        <span className="bc-name">{player.name}</span>
        {showDojo && player.dojo ? <span className="bc-dojo">{player.dojo}</span> : null}
      </div>
      {score != null ? <span className="bc-score">{score}</span> : null}
    </div>
  );
}

function MatchCard({ match, variant, showDojo, onClick, highlighted, matchRef, isPlaceholder }) {
  const aWin = match.winner && match.sideA && match.winner.id === match.sideA.id;
  const bWin = match.winner && match.sideB && match.winner.id === match.sideB.id;
  const live = match.status === "in_progress";
  const isBye = match.score?.type === "bye";

  const aScore = match.score && match.score.type === "ippon" ? (aWin ? match.score.winnerPts : match.score.loserPts) : null;
  const bScore = match.score && match.score.type === "ippon" ? (bWin ? match.score.winnerPts : match.score.loserPts) : null;

  // detect placeholder TBD (id starts with "tbd-")
  const aTBD = isPlaceholder || (match.sideA && typeof match.sideA.id === "string" && match.sideA.id.startsWith("tbd-"));
  const bTBD = isPlaceholder || (match.sideB && typeof match.sideB.id === "string" && match.sideB.id.startsWith("tbd-"));

  return (
    <button
      ref={matchRef}
      type="button"
      data-match-id={match.id}
      className={`bc-match bc-match--v${variant} ${live ? "bc-match--live" : ""} ${match.status === "complete" ? "bc-match--done" : ""} ${highlighted ? "bc-match--highlight" : ""}`}
      onClick={onClick}
      aria-label={`Match ${match.id}`}
    >
      <div className="bc-match-meta">
        <span className="bc-court">Shiaijo {match.court}</span>
        {match.scheduledAt ? <span className="bc-time">{match.scheduledAt}</span> : null}
        {live ? <span className="bc-live">● LIVE</span> : null}
        {isBye ? <span className="bc-bye-tag">BYE</span> : null}
      </div>
      <PlayerLine player={match.sideA} isWinner={aWin} side="a" showDojo={showDojo} score={aScore} isTBD={aTBD} />
      <div className="bc-divider"></div>
      <PlayerLine player={match.sideB} isWinner={bWin} side="b" showDojo={showDojo} score={bScore} isTBD={bTBD} />
    </button>
  );
}

// Computes connector lines from the DOM positions of each match card,
// then draws them in an absolutely positioned SVG.
function BracketConnectors({ rounds, treeRef, refMap, version }) {
  const [paths, setPaths] = useStateBC([]);
  const [size, setSize] = useStateBC({ w: 0, h: 0 });

  useLayoutEffectBC(() => {
    const compute = () => {
      const tree = treeRef.current;
      if (!tree || !rounds) return;
      const treeRect = tree.getBoundingClientRect();
      const out = [];
      for (let r = 0; r < rounds.length - 1; r++) {
        for (let i = 0; i < rounds[r].length; i += 2) {
          const a = refMap.current[rounds[r][i]?.id];
          const b = refMap.current[rounds[r][i + 1]?.id];
          const next = refMap.current[rounds[r + 1][i / 2]?.id];
          if (!a || !b || !next) continue;
          const aR = a.getBoundingClientRect();
          const bR = b.getBoundingClientRect();
          const nR = next.getBoundingClientRect();
          const aMidY = aR.top + aR.height / 2 - treeRect.top;
          const bMidY = bR.top + bR.height / 2 - treeRect.top;
          const nMidY = nR.top + nR.height / 2 - treeRect.top;
          const aRight = aR.right - treeRect.left;
          const nLeft = nR.left - treeRect.left;
          const midX = (aRight + nLeft) / 2;
          // Two horizontals from each source, vertical joiner, one horizontal into next.
          out.push({ d: `M ${aRight} ${aMidY} L ${midX} ${aMidY} L ${midX} ${bMidY} L ${aRight} ${bMidY}` });
          out.push({ d: `M ${midX} ${(aMidY + bMidY) / 2} L ${nLeft} ${nMidY}` });
        }
      }
      setPaths(out);
      setSize({ w: tree.scrollWidth, h: tree.scrollHeight });
    };
    compute();
    const ro = new ResizeObserver(compute);
    if (treeRef.current) ro.observe(treeRef.current);
    window.addEventListener("resize", compute);
    return () => { ro.disconnect(); window.removeEventListener("resize", compute); };
  }, [rounds, version]);

  return (
    <svg className="bc-connectors" width={size.w} height={size.h} style={{ position: "absolute", left: 0, top: 0, pointerEvents: "none" }}>
      {paths.map((p, i) => (
        <path key={i} d={p.d} fill="none" stroke="var(--line-strong, #c7cdd9)" strokeWidth="1.5" />
      ))}
    </svg>
  );
}

function BracketTree({ rounds, variant = 1, showDojo = true, onMatchClick, highlightedMatchId, autoScrollMatchId, scrollContainerRef }) {
  const treeRef = useRef(null);
  const refMap = useRef({});
  const [version, setVersion] = useStateBC(0);

  // Bump version when rounds change so connectors recompute
  useEffectBC(() => { setVersion((v) => v + 1); }, [rounds]);

  // Auto-scroll to a target match when its id changes.
  // Strip the cache-busting suffix ("::<timestamp>") that callers append to
  // force re-fire. Use rAF so the layout pass and ref callbacks complete first.
  useLayoutEffectBC(() => {
    if (!autoScrollMatchId) return;
    const realId = String(autoScrollMatchId).split("::")[0];
    let frame1 = 0, frame2 = 0;
    const run = () => {
      const el = refMap.current[realId];
      const scrollEl = scrollContainerRef?.current;
      if (!el || !scrollEl) return;
      const elRect = el.getBoundingClientRect();
      const scRect = scrollEl.getBoundingClientRect();
      const targetLeft = scrollEl.scrollLeft + (elRect.left - scRect.left) - (scRect.width / 2 - elRect.width / 2);
      const targetTop = scrollEl.scrollTop + (elRect.top - scRect.top) - (scRect.height / 2 - elRect.height / 2);
      scrollEl.scrollTo({ left: Math.max(0, targetLeft), top: Math.max(0, targetTop), behavior: "smooth" });
    };
    // double rAF: first paints, second guarantees refs are populated post-mount
    frame1 = requestAnimationFrame(() => { frame2 = requestAnimationFrame(run); });
    return () => { cancelAnimationFrame(frame1); cancelAnimationFrame(frame2); };
  }, [autoScrollMatchId, version]);

  if (!rounds) return null;
  return (
    <div className={`bc-tree bc-tree--v${variant}`} ref={treeRef}>
      <BracketConnectors rounds={rounds} treeRef={treeRef} refMap={refMap} version={version} />
      {rounds.map((round, ri) => (
        <div key={ri} className="bc-round" style={{ "--round": ri }}>
          <div className="bc-round-label">{roundLabel(ri, rounds.length)}</div>
          <div className="bc-round-matches">
            {round.map((m, mi) => (
              <div className="bc-match-wrap" key={m.id} style={{ "--mi": mi }}>
                <MatchCard
                  match={m}
                  variant={variant}
                  showDojo={showDojo}
                  highlighted={m.id === highlightedMatchId}
                  matchRef={(el) => { if (el) refMap.current[m.id] = el; }}
                  onClick={() => onMatchClick && onMatchClick(m, ri, mi)}
                />
              </div>
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}

window.BracketTree = BracketTree;
window.MatchCard = MatchCard;
window.roundLabel = roundLabel;
