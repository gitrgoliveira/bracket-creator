// Shared bracket rendering with SVG connector overlay.
// Connectors are drawn after layout via an effect that measures actual match
// card positions, so they always line up correctly regardless of card height.

const { useRef, useLayoutEffect: useLayoutEffectBC, useState: useStateBC, useEffect: useEffectBC } = React;

// TermBC — kendo-glossary tooltip wrapper. Lazy lookup so the script
// load order between glossary.jsx and this module doesn't matter.
function TermBC(props) {
  if (typeof window !== 'undefined' && window.Term) {
    return React.createElement(window.Term, props, props.children);
  }
  return React.createElement('span', null, props.children);
}

// Local hikiwake check — bracket.jsx is tested in isolation, so we don't rely
// on window.isHikiwake here. See specs/openapi.yaml.
function isHikiwakeBC(v) { return v === "hikiwake"; }
function isKikenDecisionBC(v) { return v === "kiken" || v === "kiken-voluntary" || v === "kiken-injury"; }

function roundLabel(roundIdx, total) {
  const fromEnd = total - 1 - roundIdx;
  if (fromEnd === 0) return "Final";
  if (fromEnd === 1) return "Semifinals";
  if (fromEnd === 2) return "Quarterfinals";
  if (fromEnd === 3) return "Round of 16";
  if (fromEnd === 4) return "Round of 32";
  return `Round ${roundIdx + 1}`;
}

const DECISION_CHIPS = {
  fusenpai:  { term: "fusenpai",   label: "Fus."  },
  daihyosen: { term: "daihyosen",  label: "DH"    },
};

// sideA = top = Aka (Red), sideB = bottom = Shiro (White)
function sideLabel(side) {
  return side === "a" ? "AKA" : "SHIRO";
}

// Decision-driven suffix appended to score strings on schedule rows, bracket
// nodes, viewer cards, and TV displays. Mirrors the Visual Rendering Contract
// in specs/003-tournament-gap-closure/contracts/match-decisions.md §Visual:
//   decision == "kiken"       → "Kiken"
//   decision == "fusenpai"    → "Fus."
//   decision == "daihyosen"   → "DH"
// Encho (overtime) appends " (E)" on top of any other suffix so a kiken-in-
// overtime renders "0–2 Kiken (E)". `fusensho` is per-bout only — handled by
// a separate bout badge, not by this helper. Pure and DOM-free so it can be
// reused by display.jsx (which builds its own scoreline) without dragging in
// the rest of formatIpponsScore's bye/hantei special cases.
function decisionSuffix(match) {
  if (!match) return "";
  const d = match.decision || "";
  const enchoOn = !!(match.encho && match.encho.periodCount > 0);
  const hanteiOn = !!match.decidedByHantei;
  let suffix = "";
  if (isKikenDecisionBC(d)) suffix = "Kiken";
  else if (DECISION_CHIPS[d]) suffix = DECISION_CHIPS[d].label;
  if (enchoOn) suffix = (suffix ? suffix + " " : "") + "(E)";
  // FIK 7-5 / 29-6: judges' decision after a tied encho. Mark explicitly so
  // a hantei-decided final is distinguishable from an ippon-derived one
  // (audit + Excel + viewer parity).
  if (hanteiOn) suffix = (suffix ? suffix + " " : "") + "HT";
  return suffix;
}

// Derive an ippon array from a Go-formatted scoreA/scoreB string.
// The backend formatScore() appends "(HN)" for outstanding hansoku, e.g.
// "MK(H1)" — and inserts a SPACE separator between ippons and the suffix
// when both are present, e.g. "MK (H1)" (see engine/scoring.go:715-724).
// Splitting the raw string would inject "(", "H", "1", ")" — plus a
// stray " " for the spaced shape — as bogus ippon letters. This helper
// strips the suffix AND the separator space first.
function ipponsFromScore(scoreStr) {
  if (!scoreStr) return [];
  return scoreStr.replace(/\s*\(H\d+\)$/, "").split("");
}

// Format ippons as a readable score string: ["M","K"] → "MK", [] → ""
// Returns something like "MM–K", "M–·", "△", "X", "BYE".
// Hantei (judges' decision after tied encho) is NOT a separate return value;
// it surfaces as an "HT" suffix appended by decisionSuffix when
// decidedByHantei=true — e.g. "M–K (E) HT".
//
// FR-033: when `encho` carries a positive periodCount, append " (E)" to the
// rendered string so operators and viewers see at a glance that the match
// went to overtime. Argument is optional and defaults to no-encho when absent.
//
// T097: kiken / fusenpai / daihyosen append labelled suffixes alongside the
// encho marker — wired through decisionSuffix() so the same string is used
// by display.jsx's hand-rolled score block. The decision-derived suffix
// supersedes the bare " (E)" so we don't double-print "(E)" alongside
// "Kiken (E)".
function formatIpponsScore(ipponsA, ipponsB, score, decision, encho, decidedByHantei) {
  // decidedByHantei (positional) is the canonical flag. The `typeof` guard
  // lets callers that omit the arg safely get false without sending undefined.
  const hantei = typeof decidedByHantei === "boolean" ? decidedByHantei : false;
  const decSfx = decisionSuffix({ decision, encho, decidedByHantei: hantei });
  const enchoOnly = (encho && encho.periodCount > 0) ? " (E)" : "";
  const suffix = decSfx ? " " + decSfx : enchoOnly;
  if (score?.type === "bye") return "BYE";
  const aStr = (ipponsA || []).filter(x => x && x !== "•").join("");
  const bStr = (ipponsB || []).filter(x => x && x !== "•").join("");
  const isDraw = isHikiwakeBC(decision) || isHikiwakeBC(score?.type);
  if (isDraw) {
    // No-score draw → X; with scores → △
    return ((!aStr && !bStr) ? "X" : "△") + suffix;
  }
  if (!aStr && !bStr) {
    // Fall back when the per-side ippon arrays are absent but a score object
    // exists (e.g. server-provided bracket scores). Prefer the winner's waza
    // LETTERS (score.ippons) over a bare count so the schedule always shows
    // technique letters when the data carries them — only the loser, which is
    // stored as a count not letters, falls back to a number. Winner-first
    // order matches the historical numeric fallback (no orientation change).
    if (score?.type === "ippon" && (score.winnerPts > 0 || score.loserPts > 0)) {
      const winnerLetters = (score.ippons || []).filter(x => x && x !== "•").join("");
      const winnerStr = winnerLetters || `${score.winnerPts}`;
      return `${winnerStr}–${score.loserPts}` + suffix;
    }
    // No scores but a decision was recorded (e.g. kiken before any ippon
    // was struck) — still print the suffix so the operator sees "Kiken".
    return suffix ? suffix.trimStart() : "";
  }
  return `${aStr || "·"}–${bStr || "·"}` + suffix;
}

const PlayerLine = React.memo(({ player, isWinner, side, showDojo, score, isTBD }) => {
  const isAka = side === "a";
  if (!player || isTBD) {
    return (
      <div className={`bc-side bc-side--empty bc-side--${side}`}>
        <span className={`bc-color-badge bc-color-badge--${isAka ? "aka" : "shiro"}`}>{isAka ? "AKA" : "SHIRO"}</span>
        <span className="bc-name bc-name--tbd">{isTBD ? "TBD" : "—"}</span>
      </div>
    );
  }
  return (
    <div className={`bc-side bc-side--${side} ${isWinner ? "bc-side--winner" : ""}`}>
      <span className={`bc-color-badge bc-color-badge--${isAka ? "aka" : "shiro"}`}>{isAka ? "AKA" : "SHIRO"}</span>
      {player.seed ? <span className="bc-seed">{player.seed}</span> : <span className="bc-seed bc-seed--empty"></span>}
      <div className="bc-name-wrap">
        <span className="bc-name">
          {isWinner ? <span className="bc-winner-tick" aria-label="Winner" title="Winner">✓</span> : null}
          {player.name}
        </span>
        {showDojo && player.dojo ? <span className="bc-dojo">{player.dojo}</span> : null}
      </div>
      {score != null ? <span className="bc-score">{score}</span> : null}
    </div>
  );
});
PlayerLine.displayName = "PlayerLine";

const MatchCard = React.memo(({ match, variant, showDojo, onClick, highlighted, matchRef, isPlaceholder, highlightPlayer }) => {
  const aWin = match.winner && match.sideA && match.winner.id === match.sideA.id;
  const bWin = match.winner && match.sideB && match.winner.id === match.sideB.id;
  const live = match.status === "running";
  const isBye = match.score?.type === "bye";

  const ipponsA = match.ipponsA || ipponsFromScore(match.scoreA);
  const ipponsB = match.ipponsB || ipponsFromScore(match.scoreB);
  const isDone = match.status === "completed";
  const aScore = isDone ? (ipponsA.join("") || null) : null;
  const bScore = isDone ? (ipponsB.join("") || null) : null;

  const aTBD = isPlaceholder || (match.sideA && typeof match.sideA.id === "string" && match.sideA.id.startsWith("tbd-"));
  const bTBD = isPlaceholder || (match.sideB && typeof match.sideB.id === "string" && match.sideB.id.startsWith("tbd-"));

  const _isFollowed = (typeof window !== "undefined" && window.isFollowedPlayer) || (() => false);
  const playerHighlight = highlightPlayer && (_isFollowed(match.sideA, highlightPlayer) || _isFollowed(match.sideB, highlightPlayer));

  return (
    <button
      ref={matchRef}
      type="button"
      data-match-id={match.id}
      className={`bc-match bc-match--v${variant} ${live ? "bc-match--live" : ""} ${match.status === "completed" ? "bc-match--done" : ""} ${highlighted ? "bc-match--highlight" : ""} ${playerHighlight ? "bc-match--my-match" : ""}`}
      onClick={onClick}
      aria-label={`Match ${match.id}`}
    >
      <div className="bc-match-meta">
        <span className="bc-court"><TermBC name="shiaijo">Shiaijo</TermBC> {match.court}</span>
        {match.scheduledAt ? <span className="bc-time">{match.scheduledAt}</span> : null}
        {live ? <span className="bc-live">● LIVE</span> : null}
        {isBye ? <span className="bc-bye-tag">BYE</span> : null}
        {match.score?.type === "hikiwake" ? <span className="bc-draw">△</span> : null}
        {match.encho?.periodCount > 0 ? <span className="bc-encho"><TermBC name="encho">(E)</TermBC></span> : null}
        {match.decidedByHantei ? <span className="bc-decision-chip">HT</span> : null}
        {isKikenDecisionBC(match.decision) ? (
          <span className="bc-decision-chip"><TermBC name="kiken">Kiken</TermBC></span>
        ) : null}
        {DECISION_CHIPS[match.decision] ? (
          <span className="bc-decision-chip">
            <TermBC name={DECISION_CHIPS[match.decision].term}>{DECISION_CHIPS[match.decision].label}</TermBC>
          </span>
        ) : null}
      </div>
      <PlayerLine player={match.sideA} isWinner={aWin} side="a" showDojo={showDojo} score={aScore} isTBD={aTBD} />
      <div className="bc-divider"></div>
      <PlayerLine player={match.sideB} isWinner={bWin} side="b" showDojo={showDojo} score={bScore} isTBD={bTBD} />
    </button>
  );
});
MatchCard.displayName = "MatchCard";

// Anchor connectors to the midline of a card's two competitor sides rather than
// the card's geometric centre. A card stacks a meta header (time/court) above the
// two .bc-side rows, so its geometric centre sits inside the upper (Aka) row — a
// connector landing there reads as pointing at one competitor instead of the seam
// where the two feeders merge. The sides-block midline is the visual join point;
// the offset from geometric centre is uniform across cards, so the arms stay
// horizontal. Falls back to geometric centre if sides are absent.
function anchorY(el, rect, treeTop) {
  const sides = el.querySelectorAll(".bc-side");
  if (sides.length >= 2) {
    const first = sides[0].getBoundingClientRect();
    const last = sides[sides.length - 1].getBoundingClientRect();
    return (first.top + last.bottom) / 2 - treeTop;
  }
  return rect.top + rect.height / 2 - treeTop;
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
          const aMidY = anchorY(a, aR, treeRect.top);
          const bMidY = anchorY(b, bR, treeRect.top);
          const nMidY = anchorY(next, nR, treeRect.top);
          const aRight = aR.right - treeRect.left;
          const nLeft = nR.left - treeRect.left;
          const midX = (aRight + nLeft) / 2;
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

function BracketTree({ rounds, variant = 1, showDojo = true, onMatchClick, highlightedMatchId, autoScrollMatchId, scrollContainerRef, highlightPlayer }) {
  const treeRef = useRef(null);
  const refMap = useRef({});
  const [version, setVersion] = useStateBC(0);
  // Measured absolute top (px) for each round≥1 card, keyed by match id. Round 0
  // flows naturally; every later card is then positioned at the exact midpoint of
  // its two real feeder centres. This is measured rather than derived from a fixed
  // slot pitch because card heights are not uniform within a bracket — a filled
  // name+dojo card (~118px) is taller than a TBD/placeholder card (~104px), so no
  // single pitch can centre every parent on its children.
  const [cardTops, setCardTops] = useStateBC(null);

  useEffectBC(() => { setVersion((v) => v + 1); }, [rounds]);

  useLayoutEffectBC(() => {
    const measure = () => {
      const tree = treeRef.current;
      if (!tree || !rounds || rounds.length === 0) return;
      const rmEls = tree.querySelectorAll(".bc-round-matches");
      if (rmEls.length < rounds.length) return;
      const heights = {};
      const centers = []; // centers[r][i] — card centre relative to its round-matches top
      for (let r = 0; r < rounds.length; r++) {
        const rmTop = rmEls[r].getBoundingClientRect().top;
        if (r === 0) {
          const c0 = [];
          for (const m of rounds[0]) {
            const el = refMap.current[m.id];
            if (!el) return;
            const rect = el.getBoundingClientRect();
            heights[m.id] = rect.height;
            c0.push(rect.top - rmTop + rect.height / 2);
          }
          centers.push(c0);
        } else {
          // Heights still come from the DOM (card content is unaffected by the
          // absolute positioning, which keeps full width via left/right: 0).
          for (const m of rounds[r]) {
            const el = refMap.current[m.id];
            if (!el) return;
            heights[m.id] = el.getBoundingClientRect().height;
          }
          const prev = centers[r - 1];
          centers.push(rounds[r].map((_, i) => {
            const lo = prev[2 * i];
            const hi = prev[2 * i + 1] != null ? prev[2 * i + 1] : lo;
            return (lo + hi) / 2;
          }));
        }
      }
      const tops = {};
      for (let r = 1; r < rounds.length; r++) {
        rounds[r].forEach((m, i) => { tops[m.id] = centers[r][i] - heights[m.id] / 2; });
      }
      setCardTops((prev) => {
        if (prev) {
          const keys = Object.keys(tops);
          const prevKeys = Object.keys(prev);
          if (keys.length === prevKeys.length &&
              keys.every((k) => Math.abs((prev[k] ?? 0) - tops[k]) < 0.5)) {
            return prev; // unchanged — avoid a re-render loop
          }
        }
        return tops;
      });
    };
    measure();
    const ro = new ResizeObserver(measure);
    if (treeRef.current) ro.observe(treeRef.current);
    window.addEventListener("resize", measure);
    return () => { ro.disconnect(); window.removeEventListener("resize", measure); };
  }, [rounds, version]);

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
    frame1 = requestAnimationFrame(() => { frame2 = requestAnimationFrame(run); });
    return () => { cancelAnimationFrame(frame1); cancelAnimationFrame(frame2); };
  }, [autoScrollMatchId, version]);

  if (!rounds) return null;
  // Round 0 flows naturally; rounds ≥ 1 are absolutely positioned at the measured
  // midpoint of their two feeder cards (see the layout effect above). cardTops is
  // null on the first paint, so every round renders in natural flow; the effect
  // then measures real centres and re-renders the later rounds into place, and
  // BracketConnectors' ResizeObserver redraws the SVG once the layout settles.
  return (
    <div className={`bc-tree bc-tree--v${variant}`} ref={treeRef}>
      <BracketConnectors rounds={rounds} treeRef={treeRef} refMap={refMap} version={version} />
      {rounds.map((round, ri) => {
        const positioned = ri > 0 && cardTops;
        return (
          <div key={ri} className="bc-round" style={{ "--round": ri }}>
            <div className="bc-round-label">{roundLabel(ri, rounds.length)}</div>
            <div className={`bc-round-matches${positioned ? " bc-round-matches--abs" : ""}`}>
              {round.map((m, mi) => {
                const top = positioned ? cardTops[m.id] : undefined;
                const wrapStyle = top != null
                  ? { "--mi": mi, position: "absolute", top: `${top}px`, left: 0, right: 0 }
                  : { "--mi": mi };
                return (
                <div className="bc-match-wrap" key={m.id} style={wrapStyle}>
                  <MatchCard
                    match={m}
                    variant={variant}
                    showDojo={showDojo}
                    highlighted={m.id === highlightedMatchId}
                    matchRef={(el) => { if (el) refMap.current[m.id] = el; }}
                    onClick={() => onMatchClick && onMatchClick(m, ri, mi)}
                    highlightPlayer={highlightPlayer}
                  />
                </div>
                );
              })}
            </div>
          </div>
        );
      })}
    </div>
  );
}

window.BracketTree = BracketTree;
window.MatchCard = MatchCard;
window.roundLabel = roundLabel;
window.formatIpponsScore = formatIpponsScore;
window.decisionSuffix = decisionSuffix;
window.sideLabel = sideLabel;
window.ipponsFromScore = ipponsFromScore;

export { formatIpponsScore, decisionSuffix, sideLabel, roundLabel, ipponsFromScore };
