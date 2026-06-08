// match_scoreboard.jsx — the ONE FIK dantai-shiai scoreboard, shared by every
// surface that shows a match's detail: the viewer card (MatchDetailCard), the
// self-run modal (MatchViewerModal → MatchDetailCard), and the TV display
// (TvDisplay). Built straight from running_a_kendo_tournament.md
// §263 (individual: ippon-letter slots) and §277 (team: IV/PW summary row +
// per-bout rows + Daihyosen).
//
// esbuild compiles each web-mobile/js/*.jsx as a separate entry and inlines
// imported modules, so importing this from both viewer.jsx and display.jsx is
// the established DRY mechanism (same as lineup_resolver.jsx). No window-global
// coupling needed.
//
// `variant` ("card" | "tv") only changes sizing via a CSS modifier — the markup
// and data-testids are identical across surfaces.

import { resolveMatchLineup, resolveLineupTeamId, pickFromLineup } from './lineup_resolver.jsx';

const { useState: useSB, useEffect: useEB } = React;

// boutHansokuMark — red ▲ for ONE outstanding hansoku (fouls % 2 === 1). On the
// second hansoku the ▲ is deleted and 1 ippon (H) goes to the opponent, so a
// side never shows two triangles (FIK Shinpan Management p.15, Table 1).
export function boutHansokuMark(foulCount) {
  return (foulCount || 0) % 2 === 1 ? "▲" : "";
}

// useTeamLineups — fetch per-match lineups for both sides of a team match.
// Unifies the former viewer useTeamLineups + display useTvTeamLineups: pass the
// competition explicitly when available (TV/SSE), else it falls back to
// match.compId (viewer). Returns { lineupA, lineupB }; degrades to null/null
// when window.API is unavailable (public surfaces) → callers fall back to bout
// numbers.
//
// `roundIndex` (optional, 0-based) is the authoritative round for the
// round-scoped lineup fallback. Callers that know the bracket round (the TV
// display / overlay carry it as promoted.roundIndex) MUST pass it — do not
// rely on parsing match.round, which now holds a bracket-size display label
// ("Round 16"/"Round 32") in some surfaces and would misderive the round.
export function useTeamLineups(match, competition, roundIndex) {
  const [lineupA, setLineupA] = useSB(null);
  const [lineupB, setLineupB] = useSB(null);

  const compId = (competition && competition.id) || match?.compId;
  const matchId = match?.id;
  const sideAId = match?.sideA?.id || match?.sideA?.name || (typeof match?.sideA === "string" ? match?.sideA : "");
  const sideBId = match?.sideB?.id || match?.sideB?.name || (typeof match?.sideB === "string" ? match?.sideB : "");

  useEB(() => {
    // Clear stale lineups immediately so the previous match's names never leak
    // into the next render (Copilot review: stale lineup state).
    setLineupA(null);
    setLineupB(null);
    if (!compId || !matchId || !window.API) return undefined;
    let cancelled = false;
    (async () => {
      let players = [];
      try {
        const detail = await window.API.fetchCompetitionDetails(compId);
        if (cancelled) return;
        players =
          (detail && detail.players && detail.players.length ? detail.players : null)
          || (detail && detail.config && detail.config.players)
          || [];
      } catch (_e) {
        console.warn("useTeamLineups: competition fetch failed", _e);
      }
      // Prefer the explicit 0-based round index. Only when it is absent do we
      // fall back to match.round — a raw numeric index, or the legacy engine
      // label "Round <number>" (1-based round NUMBER → 0-based). We deliberately
      // do NOT trust a bracket-size label here; callers with a real round pass
      // roundIndex so this parse is never reached on those surfaces.
      let round = 0;
      if (typeof roundIndex === "number" && roundIndex >= 0) {
        round = roundIndex;
      } else if (typeof match.round === "number") {
        round = match.round;
      } else if (typeof match.round === "string") {
        const mr = /^Round\s+(\d+)$/.exec(match.round);
        if (mr) round = parseInt(mr[1], 10) - 1;
      }
      const teamAId = resolveLineupTeamId(sideAId, players);
      const teamBId = resolveLineupTeamId(sideBId, players);
      // Both sides are independent GETs — fetch them in parallel to halve the
      // time-to-render (the promoted match changes often on TV/overlay).
      const [la, lb] = await Promise.all([
        teamAId ? resolveMatchLineup(compId, teamAId, matchId, round, window.API) : null,
        teamBId ? resolveMatchLineup(compId, teamBId, matchId, round, window.API) : null,
      ]);
      if (cancelled) return;
      if (teamAId) setLineupA(la);
      if (teamBId) setLineupB(lb);
    })();
    return () => { cancelled = true; };
    // match?.round participates in the fallback-round lineup fetch, so a round
    // change on a reused match id must re-run the effect.
  }, [compId, matchId, sideAId, sideBId, roundIndex, match?.round]);

  return { lineupA, lineupB };
}

// Real ippon letters for a side (drops placeholders), capped at the 2 sanbon
// slots; pads to exactly 2 so the slot columns always align.
function ipponLetters(arr) {
  const real = (arr || []).filter(x => x && x !== "•");
  return [real[0] || "", real[1] || ""];
}

// letters[0] is the OUTER ippon (the first point scored), letters[1] the inner.
// Ippons fill from the OUTSIDE toward the centre: shiro fills left→right (its
// outer edge is the left), aka fills right→left (its outer edge is the right),
// so for aka we reverse the visual cell order. The testid stays on the logical
// outer cell (letters[0]) regardless of which side renders it.
function slotCells(letters, side, testid) {
  const cells = [0, 1].map(i => (
    <span key={i} className={"msb-slot" + (side === "aka" ? " msb-slot--aka" : "")}
      data-testid={i === 0 ? testid : undefined}>{letters[i] || ""}</span>
  ));
  return side === "aka" ? cells.reverse() : cells;
}

// centreMarks — the §263 inner cells: [shiro slot][shiro slot] | vs/X | [aka slot][aka slot].
// Hansoku ▲ shows on the offending side, on the OUTER edge of the slots (away
// from centre); X marks a hikiwake; "Ht" flags hantei. For an ippon-less
// decision (hantei, fusensho, kiken) the winning side is otherwise invisible,
// so we mark it with ○ (FIK hantei-win symbol) in the winner's first slot.
// A plain helper (not a component) so it renders inline into the parent's tree.
function centreMarks(sub) {
  const lettersB = ipponLetters(sub.ipponsB); // shiro / left
  const lettersA = ipponLetters(sub.ipponsA); // aka / right
  const foulB = boutHansokuMark(sub.hansokuB);
  const foulA = boutHansokuMark(sub.hansokuA);
  const isDraw = !sub.decidedByHantei &&
    typeof window.isHikiwake === "function" &&
    (window.isHikiwake(sub.score?.type) || window.isHikiwake(sub.decision));
  // Win mark only when there are no ippon letters (otherwise the letters
  // already show who won). sideB = shiro/left, sideA = aka/right.
  const noIppons = !lettersB.some(Boolean) && !lettersA.some(Boolean);
  const winShiro = !!(noIppons && sub.winner && sub.winner === sub.sideB);
  const winAka = !!(noIppons && sub.winner && sub.winner === sub.sideA);
  // The mark sits on the WINNING side, not in the centre: "Ht" for a hantei
  // decision, else ○ (fusensho/kiken/other ippon-less win). Only when the
  // hantei winner is unknown does "Ht" fall back to the centre cell.
  const winMark = sub.decidedByHantei ? "Ht" : "○";
  const hasWinSide = winShiro || winAka;
  return (
    <span className="msb-marks" data-testid="sub-marks">
      <span className={"msb-slots" + (winShiro ? " msb-slots--win" : "")}>
        {foulB && <span className="msb-hansoku" data-testid="foul-mark-b">{foulB}</span>}
        {winShiro ? slotCells([winMark, ""], "shiro", "sub-win-b") : slotCells(lettersB, "shiro")}
      </span>
      <span className="msb-vs">
        {isDraw ? <span data-testid="sub-row-draw">X</span> : null}
        {sub.decidedByHantei && !hasWinSide ? <span className="msb-ht" data-testid="sub-row-hantei">Ht</span> : null}
      </span>
      <span className={"msb-slots msb-slots--aka" + (winAka ? " msb-slots--win" : "")}>
        {winAka ? slotCells([winMark, ""], "aka", "sub-win-a") : slotCells(lettersA, "aka")}
        {foulA && <span className="msb-hansoku" data-testid="foul-mark-a">{foulA}</span>}
      </span>
    </span>
  );
}

// BoutSubRow — one FIK bout row: Shiro name | ippon slots · vs · ippon slots | Aka name.
// variant: "card" (default) | "tv" (larger). state: "now" | "queued" | "done"
// (TV highlight only). Names come from the pinned lineup, else the bout number —
// never the team name (it would repeat on every row).
export function BoutSubRow({ sub, index, lineupA, lineupB, teamSize, isDH, variant, state }) {
  const boutNum = isDH ? "DH" : String(sub && sub.position > 0 ? sub.position : index + 1);
  const shiroName = (lineupB ? pickFromLineup(lineupB, index, teamSize) : "") || boutNum;
  const akaName = (lineupA ? pickFromLineup(lineupA, index, teamSize) : "") || boutNum;
  const cls = "msb-row"
    + (variant === "tv" ? " msb-row--tv" : "")
    + (state === "now" ? " msb-row--now" : "")
    + (state === "queued" ? " msb-row--queued" : "")
    + (isDH ? " msb-row--dh" : "");
  return (
    <div className={cls} data-testid={isDH ? "sub-row-dh" : `sub-row-${index}`}>
      <span className="msb-name" data-testid="sub-shiro-name">{shiroName}</span>
      {centreMarks(sub)}
      <span className="msb-name msb-name--aka" data-testid="sub-aka-name">{akaName}</span>
    </div>
  );
}

// Aggregate IV (individual victories) + PW (points won) per side from the
// regular (non-DH) bouts. sideB = shiro/left, sideA = aka/right.
export function teamIVPW(subResults) {
  let ivShiro = 0, ivAka = 0, pwShiro = 0, pwAka = 0;
  for (const s of (subResults || []).filter(x => x.position !== -1)) {
    const a = ipponLetters(s.ipponsA).filter(Boolean).length;
    const b = ipponLetters(s.ipponsB).filter(Boolean).length;
    pwShiro += b; pwAka += a;
    // IV (individual victory): prefer the explicit winner — quick-score and
    // decision-based outcomes (fusensho, kiken, hantei) set `winner` without
    // ippon letters, so an ippon-count comparison alone would miss the
    // victory. Fall back to ippon counts only when no winner is recorded or
    // it matches neither side. sideA = aka (right), sideB = shiro (left).
    if (s.winner && s.winner === s.sideA) ivAka++;
    else if (s.winner && s.winner === s.sideB) ivShiro++;
    else if (b > a) ivShiro++;
    else if (a > b) ivAka++;
  }
  return { ivShiro, ivAka, pwShiro, pwAka };
}

// IndividualScore — §263 row for an individual match: ippon slots per side
// (the match IS one bout). Renders the same CentreMarks as a bout row.
export function IndividualScore({ match, variant }) {
  // Normalise side/winner to names so centreMarks can mark the ippon-less
  // (hantei/decision) winner. Sides may be {id,name} objects or bare strings.
  const sideName = (v) => v?.name || (typeof v === "string" ? v : "");
  const sub = {
    ipponsA: match.ipponsA || (window.ipponsFromScore ? window.ipponsFromScore(match.scoreA) : []),
    ipponsB: match.ipponsB || (window.ipponsFromScore ? window.ipponsFromScore(match.scoreB) : []),
    hansokuA: match.hansokuA, hansokuB: match.hansokuB,
    decidedByHantei: match.decidedByHantei, score: match.score, decision: match.decision,
    winner: sideName(match.winner), sideA: sideName(match.sideA), sideB: sideName(match.sideB),
  };
  return (
    <div className={"msb msb-individual" + (variant === "tv" ? " msb--tv" : "")} data-testid="individual-score">
      <div className="msb-row">
        <span className="msb-name" />
        {centreMarks(sub)}
        <span className="msb-name msb-name--aka" />
      </div>
    </div>
  );
}

// TeamScoreboard — §277 team table: an IV/PW summary row (labeled, per side) +
// one BoutSubRow per regular bout + the Daihyosen banner + rep-bout row when
// `showDH`. Shiro left/dark, Aka right/red.
export function TeamScoreboard({ subResults, lineupA, lineupB, teamSize, showDH, variant, shiroName, akaName }) {
  const regular = (subResults || []).filter(s => s.position !== -1);
  const { ivShiro, ivAka, pwShiro, pwAka } = teamIVPW(subResults);
  // FIK: a Daihyosen (representative bout) only happens when the team match is
  // TIED after the regular bouts — equal individual victories AND equal points.
  // Guard the render on the tie so a stale/invalid position:-1 sub never shows a
  // Daihyosen on an already-decided match (mp-13y #12).
  const tied = ivShiro === ivAka && pwShiro === pwAka;
  const renderDH = !!showDH && tied;
  const dhSub = renderDH ? (subResults || []).find(s => s.position === -1) : null;
  const tv = variant === "tv";
  // The current bout = first unscored regular bout (amber highlight). Already-
  // scored bouts are "done"; later ones "queued". A completed match → all done.
  const isScored = (s) => {
    const a = ipponLetters(s.ipponsA).filter(Boolean).length;
    const b = ipponLetters(s.ipponsB).filter(Boolean).length;
    // A bout counts as scored once it has any recorded outcome: ippon letters,
    // a hansoku, a hantei, an explicit winner or decision (quick-score and
    // forfeit-style outcomes set winner/decision without ippon letters), or a
    // hikiwake draw.
    return a > 0 || b > 0 || s.hansokuA || s.hansokuB || s.decidedByHantei ||
      !!s.winner || (typeof s.decision === "string" && s.decision !== "") ||
      (typeof window.isHikiwake === "function" && (window.isHikiwake(s.score?.type) || window.isHikiwake(s.decision)));
  };
  const currentIdx = regular.findIndex(s => !isScored(s));
  const rowState = (i) => (i < currentIdx || currentIdx === -1) ? "done" : (i === currentIdx ? "now" : "queued");

  return (
    <div className={"msb msb-team" + (tv ? " msb--tv" : "")} data-testid="team-scoreboard">
      {/* §277 summary row: team name + IV then PW per side */}
      <div className="msb-row msb-row--summary" data-testid="team-summary">
        <span className="msb-name" data-testid="summary-shiro-name">{shiroName || ""}</span>
        <span className="msb-marks">
          <span className="msb-slots">
            <span className="msb-slot msb-sum"><span className="msb-lab">IV</span>{ivShiro}</span>
            <span className="msb-slot msb-sum"><span className="msb-lab">PW</span>{pwShiro}</span>
          </span>
          <span className="msb-vs" />
          <span className="msb-slots msb-slots--aka">
            <span className="msb-slot msb-slot--aka msb-sum"><span className="msb-lab">PW</span>{pwAka}</span>
            <span className="msb-slot msb-slot--aka msb-sum"><span className="msb-lab">IV</span>{ivAka}</span>
          </span>
        </span>
        <span className="msb-name msb-name--aka" data-testid="summary-aka-name">{akaName || ""}</span>
      </div>

      {/* per-bout rows */}
      {regular.map((sub, i) => (
        <BoutSubRow key={i} sub={sub} index={i} lineupA={lineupA} lineupB={lineupB}
          teamSize={teamSize} isDH={false} variant={variant} state={rowState(i)} />
      ))}

      {/* No bouts recorded yet (lineups not submitted / up-next): show the
          teamSize numbered/roster rows so the board reads as a real scoreboard
          rather than a lone IV/PW summary (mp-13y #4/#6). BoutSubRow shows the
          pinned player name when a lineup exists, else the bout number. */}
      {regular.length === 0 && teamSize > 0 && Array.from({ length: teamSize }, (_, i) => (
        <BoutSubRow key={"ph" + i} sub={{}} index={i} lineupA={lineupA} lineupB={lineupB}
          teamSize={teamSize} isDH={false} variant={variant} state="queued" />
      ))}

      {/* Daihyosen banner + rep bout (knockout tie only) */}
      {renderDH && (
        <>
          <div className="msb-row msb-row--dh-banner" data-testid="dh-banner">
            <span className="msb-dh-tag">DAIHYOSEN</span>
          </div>
          {dhSub
            ? <BoutSubRow sub={dhSub} index={regular.length} lineupA={lineupA} lineupB={lineupB}
                teamSize={teamSize} isDH={true} variant={variant} state="now" />
            : <div className="msb-dh-pending" data-testid="tvd-dh-pending">Daihyosen pending</div>}
        </>
      )}
    </div>
  );
}

if (typeof window !== "undefined") {
  // Exposed for any non-importing surface + debugging; importers use the ES exports.
  window.TeamScoreboard = TeamScoreboard;
  window.IndividualScore = IndividualScore;
  window.BoutSubRow = BoutSubRow;
}
