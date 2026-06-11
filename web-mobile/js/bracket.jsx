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
  // mp-13y #8: "Round N" (drop the "of"), where N is the round's bracket size
  // = 2^(fromEnd+1). Computed generically so 128/256-player brackets read
  // "Round 128" / "Round 256" instead of falling back to "Round 1".
  if (fromEnd >= 3) return `Round ${2 ** (fromEnd + 1)}`;
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

// teamIVScore: derive a team match's individual-victories aggregate ("shiroIV–akaIV")
// from persisted subResults. Mirrors Go engine.ComputeTeamSummary: skip the daihyosen
// sentinel (position < 0); award IV to whichever match-level side won each bout (winner
// matches the match-level OR sub-level side name); empty winner = hikiwake (no IV).
// Orientation: sideB = Shiro (left), sideA = Aka (right) — matches the (ipponsB, ipponsA)
// call order used everywhere. Returns null when there are no subResults (individual
// matches) so callers fall back to formatIpponsScore.
function teamIVScore(m) {
  const subs = m && m.subResults;
  if (!Array.isArray(subs) || subs.length === 0) return null;
  const aName = typeof m.sideA === "object" ? m.sideA?.name : m.sideA;
  const bName = typeof m.sideB === "object" ? m.sideB?.name : m.sideB;
  let ivA = 0, ivB = 0;
  for (const sub of subs) {
    if (!sub || sub.position < 0) continue; // skip daihyosen sentinel
    const w = sub.winner;
    if (!w) continue;                        // hikiwake / undecided → no IV
    if (w === aName || w === sub.sideA) ivA++;
    else if (w === bName || w === sub.sideB) ivB++;
  }
  return `${ivB}–${ivA}`; // Shiro (B) – Aka (A)
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
          {player.number ? <span className="num-prefix">{player.number}</span> : null}
          {player.name}
        </span>
        {/* Reserve the dojo line on every side when dojos are shown — a real
            player without a dojo, or a "Winner of…" placeholder, gets an
            invisible spacer line so all sides (and thus all bracket cards) keep
            a uniform height (mp-7f2w). When showDojo is off, no line is rendered
            anywhere, so cards stay uniformly short. */}
        {showDojo ? <span className="bc-dojo">{player.dojo || <span aria-hidden="true">{"\u00A0"}</span>}</span> : null}
      </div>
      {score != null ? <span className="bc-score">{score}</span> : null}
    </div>
  );
});
PlayerLine.displayName = "PlayerLine";

const MatchCard = React.memo(({ match, variant, showDojo, onClick, highlighted, matchRef, isPlaceholder, highlightPlayers }) => {
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

  // mp-xhaa: highlight any watched player (Set of ids+names). Lazy window
  // lookup mirrors the prior pattern so bracket.jsx stays decoupled from
  // viewer.jsx module load order.
  const _isWatched = (typeof window !== "undefined" && window.isPlayerWatched) || (() => false);
  const playerHighlight = !!(highlightPlayers && (_isWatched(match.sideA, highlightPlayers) || _isWatched(match.sideB, highlightPlayers)));

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
        {live ? <span className="bc-live">● NOW</span> : null}
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

// buildDisplayModel decides how to render a bracket. When the engine has tagged
// matches with effective-round metadata (mp-7f2w: displayRound / hidden /
// feeders), it groups the REAL matches into effective-round columns identical to
// the Excel Tree sheet (structural byes skip a column, phantom bye matches are
// dropped) and exposes a feeder graph for connector drawing. Otherwise it falls
// back to the legacy balanced-rounds shape with positional (2i, 2i+1) feeders so
// brackets generated before this field existed render exactly as before.
// useAutoScrollToMatch smooth-scrolls the bracket so the given match is centred
// in the scroll container. Shared by both the effective-round (BracketTreeMeta)
// and legacy (BracketTreeLegacy) renderers so the centring math lives in one place.
function useAutoScrollToMatch(autoScrollMatchId, refMap, scrollContainerRef, version) {
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
}

function buildDisplayModel(rounds) {
  if (!rounds || rounds.length === 0) return { hasMeta: false, columns: rounds || [], feedersById: {} };
  const hasMeta = rounds.some((r) => r.some((m) => (m.displayRound || 0) > 0 || m.hidden));
  if (hasMeta) {
    let maxDR = 0;
    const real = [];
    rounds.forEach((r) => r.forEach((m) => {
      if (!m.hidden && (m.displayRound || 0) > 0) { real.push(m); if (m.displayRound > maxDR) maxDR = m.displayRound; }
    }));
    const columns = [];
    for (let dr = maxDR; dr >= 1; dr--) columns.push(real.filter((m) => m.displayRound === dr));
    const feedersById = {};
    real.forEach((m) => { feedersById[m.id] = (m.feeders || []).filter(Boolean); });
    return { hasMeta: true, columns, feedersById };
  }
  // Legacy: columns = rounds. Connectors are positional (BracketConnectors
  // derives the 2i/2i+1 feeders from `rounds` itself), so no feeder graph is
  // produced here — feedersById stays empty to match the empty-input shape.
  return { hasMeta: false, columns: rounds, feedersById: {} };
}

// computeMetaTops lays out an (uneven) effective-round bracket. It walks the
// feeder graph from the final: matches with no feeders ("seeded" entrants — real
// players or bye recipients) are stacked top-to-bottom in depth-first encounter
// order, and every parent is centred on the mean of its feeders. Returns a map
// of matchId → absolute top (px). heights is matchId → measured card height.
function computeMetaTops(columns, feedersById, heights) {
  const GAP = 16;
  const DEFAULT_H = 110;
  const centerOf = {};
  const inProgress = new Set();
  let cursor = 0;
  const visit = (id) => {
    if (centerOf[id] != null) return centerOf[id];
    // Cycle guard (mirrors the DisplayRound!=0 guard in the Go BFS): centerOf is
    // only set post-order for parents, so a cyclic feeders graph would recurse
    // forever. The engine only emits acyclic trees, but a corrupt/hand-edited
    // bracket.json must not crash the renderer — break the cycle and return 0.
    if (inProgress.has(id)) return 0;
    inProgress.add(id);
    const fs = (feedersById[id] || []).filter(Boolean);
    const h = heights[id] || DEFAULT_H;
    if (fs.length === 0) {
      const c = cursor + h / 2;
      cursor += h + GAP;
      centerOf[id] = c;
      inProgress.delete(id);
      return c;
    }
    const cs = fs.map(visit);
    const c = cs.reduce((a, b) => a + b, 0) / cs.length;
    centerOf[id] = c;
    inProgress.delete(id);
    return c;
  };
  const rootId = columns[columns.length - 1]?.[0]?.id;
  if (rootId) visit(rootId);
  // Defensive: the engine only sets displayRound>0 on matches reachable from the
  // final, so visit(rootId) already placed every match in `columns`. This loop
  // is a no-op for engine output and only fires on corrupt/hand-written metadata.
  columns.forEach((col) => col.forEach((m) => {
    if (centerOf[m.id] == null) {
      const h = heights[m.id] || DEFAULT_H;
      centerOf[m.id] = cursor + h / 2;
      cursor += h + GAP;
    }
  }));
  const tops = {};
  columns.forEach((col) => col.forEach((m) => {
    tops[m.id] = centerOf[m.id] - (heights[m.id] || DEFAULT_H) / 2;
  }));
  return tops;
}

// BracketConnectorsMeta draws feeder→parent elbows for the effective-round
// layout (mp-7f2w). Unlike the legacy BracketConnectors it pairs by the explicit
// feeder graph, not binary (2i, 2i+1) positions, so uneven columns connect
// correctly. A match with a single feeder (the other side is a bye/seeded
// entrant) gets one elbow; the bye side draws no line.
function BracketConnectorsMeta({ columns, feedersById, treeRef, refMap, version, showDojo, variant }) {
  const [paths, setPaths] = useStateBC([]);
  const [size, setSize] = useStateBC({ w: 0, h: 0 });

  useLayoutEffectBC(() => {
    const compute = () => {
      const tree = treeRef.current;
      if (!tree) return;
      const treeRect = tree.getBoundingClientRect();
      const out = [];
      columns.forEach((col) => col.forEach((m) => {
        const fs = (feedersById[m.id] || []).filter(Boolean);
        if (fs.length === 0) return;
        const mEl = refMap.current[m.id];
        if (!mEl) return;
        const mR = mEl.getBoundingClientRect();
        const mLeft = mR.left - treeRect.left;
        const mMidY = anchorY(mEl, mR, treeRect.top);
        fs.forEach((fid) => {
          const fEl = refMap.current[fid];
          if (!fEl) return;
          const fR = fEl.getBoundingClientRect();
          const fRight = fR.right - treeRect.left;
          const fMidY = anchorY(fEl, fR, treeRect.top);
          const midX = (fRight + mLeft) / 2;
          out.push({ d: `M ${fRight} ${fMidY} L ${midX} ${fMidY} L ${midX} ${mMidY} L ${mLeft} ${mMidY}` });
        });
      }));
      setPaths(out);
      setSize({ w: tree.scrollWidth, h: tree.scrollHeight });
    };
    compute();
    const ro = new ResizeObserver(compute);
    if (treeRef.current) ro.observe(treeRef.current);
    window.addEventListener("resize", compute);
    return () => { ro.disconnect(); window.removeEventListener("resize", compute); };
    // showDojo/variant change card heights → feeder anchors move, so recompute.
  }, [columns, feedersById, version, showDojo, variant]);

  return (
    <svg className="bc-connectors" width={size.w} height={size.h} style={{ position: "absolute", left: 0, top: 0, pointerEvents: "none" }}>
      {paths.map((p, i) => (
        <path key={i} d={p.d} fill="none" stroke="var(--line-strong, #c7cdd9)" strokeWidth="1.5" />
      ))}
    </svg>
  );
}

// BracketTreeMeta renders the effective-round columns (mp-7f2w). Every card is a
// real match; structural byes are absent. Cards in column 0 plus every card is
// absolutely positioned at the feeder-graph-derived top so parents sit centred
// on their feeders (see computeMetaTops).
function BracketTreeMeta({ columns, feedersById, variant = 1, showDojo = true, onMatchClick, highlightedMatchId, autoScrollMatchId, scrollContainerRef, highlightPlayers }) {
  const treeRef = useRef(null);
  const refMap = useRef({});
  const [version, setVersion] = useStateBC(0);
  const [cardTops, setCardTops] = useStateBC(null);

  // Reset the measured layout only when the TOPOLOGY changes, not on every new
  // `columns` reference. Match ids and the feeder graph are frozen at generation
  // time, so a score update (which only mutates sideA/sideB/status/score) yields
  // the same signature and the measured tops are preserved — avoiding a reset-to-
  // null reflow flash on every live-court update. Genuine height changes on
  // resolution are still caught by the measure effect's ResizeObserver below.
  const topoSig = React.useMemo(
    () => columns.map((c) => c.map((m) => m.id).join(",")).join("|"),
    [columns]
  );
  useEffectBC(() => { setCardTops(null); setVersion((v) => v + 1); }, [topoSig]);

  useLayoutEffectBC(() => {
    const measure = () => {
      const tree = treeRef.current;
      if (!tree || !columns || columns.length === 0) return;
      const heights = {};
      for (const col of columns) {
        for (const m of col) {
          const el = refMap.current[m.id];
          if (!el) return;
          heights[m.id] = el.getBoundingClientRect().height;
        }
      }
      const tops = computeMetaTops(columns, feedersById, heights);
      // Every column is absolutely positioned, so the flow height of each
      // round-matches container is 0 and the tree would collapse. Derive the
      // overall content height from the lowest card bottom and pin it on the
      // containers so the tree (and its scroll area) size correctly.
      let height = 0;
      for (const col of columns) {
        for (const m of col) {
          const bottom = (tops[m.id] || 0) + (heights[m.id] || 0);
          if (bottom > height) height = bottom;
        }
      }
      setCardTops((prev) => {
        if (prev) {
          const same = Math.abs((prev.height ?? 0) - height) < 0.5 &&
            Object.keys(tops).length === Object.keys(prev.tops).length &&
            Object.keys(tops).every((k) => Math.abs((prev.tops[k] ?? 0) - tops[k]) < 0.5);
          if (same) return prev;
        }
        return { tops, height };
      });
    };
    measure();
    const ro = new ResizeObserver(measure);
    if (treeRef.current) ro.observe(treeRef.current);
    window.addEventListener("resize", measure);
    return () => { ro.disconnect(); window.removeEventListener("resize", measure); };
    // showDojo/variant affect card heights, so re-measure (and reposition) when
    // they change — not just on topology/version. The convergence guard keeps a
    // no-op recompute from causing a render.
  }, [columns, feedersById, version, showDojo, variant]);

  useAutoScrollToMatch(autoScrollMatchId, refMap, scrollContainerRef, version);

  if (!columns || columns.length === 0) return null;
  const positioned = !!cardTops;
  // Every column is absolutely positioned, so override the legacy flex:1 (which
  // would zero the basis) and pin an explicit height; otherwise the flex row has
  // no non-abs column to establish its height and the tree collapses.
  const matchesStyle = positioned ? { flex: "none", height: `${cardTops.height}px`, minHeight: `${cardTops.height}px` } : undefined;
  return (
    <div className={`bc-tree bc-tree--v${variant}`} ref={treeRef}>
      <BracketConnectorsMeta columns={columns} feedersById={feedersById} treeRef={treeRef} refMap={refMap} version={version} showDojo={showDojo} variant={variant} />
      {columns.map((col, ci) => (
        <div key={ci} className="bc-round" style={{ "--round": ci }}>
          <div className="bc-round-label">{roundLabel(ci, columns.length)}</div>
          <div className={`bc-round-matches${positioned ? " bc-round-matches--abs" : ""}`} style={matchesStyle}>
            {col.map((m, mi) => {
              const top = positioned ? cardTops.tops[m.id] : undefined;
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
                    onClick={() => onMatchClick && onMatchClick(m, ci, mi)}
                    highlightPlayers={highlightPlayers}
                  />
                </div>
              );
            })}
          </div>
        </div>
      ))}
    </div>
  );
}

// BracketTree switches between the effective-round renderer (when the engine
// supplied display metadata) and the legacy balanced-rounds renderer.
function BracketTree(props) {
  // Memoise on rounds identity: buildDisplayModel returns fresh arrays, and the
  // meta renderer's effects key on those — recomputing every render would reset
  // the measured layout in a loop.
  const model = React.useMemo(() => buildDisplayModel(props.rounds), [props.rounds]);
  if (model.hasMeta) {
    return <BracketTreeMeta {...props} columns={model.columns} feedersById={model.feedersById} />;
  }
  return <BracketTreeLegacy {...props} />;
}

function BracketTreeLegacy({ rounds, variant = 1, showDojo = true, onMatchClick, highlightedMatchId, autoScrollMatchId, scrollContainerRef, highlightPlayers }) {
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

  // On a bracket change, clear measured positions so the new rounds render in
  // natural flow for one frame (their match ids differ, so stale tops wouldn't
  // apply anyway) until the layout effect re-measures — avoids any stale-position
  // flash. The version bump re-runs the measure effect.
  useEffectBC(() => { setCardTops(null); setVersion((v) => v + 1); }, [rounds]);

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

  useAutoScrollToMatch(autoScrollMatchId, refMap, scrollContainerRef, version);

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
                    highlightPlayers={highlightPlayers}
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

// matchScoreStr: unified score string for any completed match.
// Tries teamIVScore first (team matches with subResults → "IV–IV"),
// then falls back to formatIpponsScore. Callers pass pre-resolved ippons
// arrays (which may be derived from scoreA/scoreB for bracket matches).
// Returns "" when neither path produces a string (caller handles "—" fallback).
function matchScoreStr(m, ipponsB, ipponsA) {
  return teamIVScore(m)
    || formatIpponsScore(ipponsB, ipponsA, m.score, m.decision, m.encho, m.decidedByHantei);
}

window.BracketTree = BracketTree;
window.MatchCard = MatchCard;
window.roundLabel = roundLabel;
window.formatIpponsScore = formatIpponsScore;
window.teamIVScore = teamIVScore;
window.matchScoreStr = matchScoreStr;
window.decisionSuffix = decisionSuffix;
window.sideLabel = sideLabel;
window.ipponsFromScore = ipponsFromScore;

export { formatIpponsScore, decisionSuffix, sideLabel, roundLabel, ipponsFromScore, teamIVScore, matchScoreStr, buildDisplayModel, computeMetaTops };
