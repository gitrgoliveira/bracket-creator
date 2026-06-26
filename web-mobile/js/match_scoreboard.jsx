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
  const [lineupVersion, setLineupVersion] = useSB(0);

  const compId = (competition && competition.id) || match?.compId;
  const matchId = match?.id;
  const sideAId = match?.sideA?.id || match?.sideA?.name || (typeof match?.sideA === "string" ? match?.sideA : "");
  const sideBId = match?.sideB?.id || match?.sideB?.name || (typeof match?.sideB === "string" ? match?.sideB : "");

  // Subscribe to lineup-updated window CustomEvent (dispatched by app.jsx when
  // the backend emits an SSE lineup_updated for this competition). Incrementing
  // lineupVersion causes the fetch effect below to re-run and pick up the new
  // lineup without a full page reload.
  useEB(() => {
    if (!compId) return;
    const handler = (e) => {
      if (!e.detail || e.detail.competitionId === compId) {
        setLineupVersion(v => v + 1);
      }
    };
    window.addEventListener("lineup-updated", handler);
    return () => window.removeEventListener("lineup-updated", handler);
  }, [compId]);

  useEB(() => {
    // Clear stale lineups immediately so the previous match's names never leak
    // into the next render (Copilot review: stale lineup state).
    setLineupA(null);
    setLineupB(null);
    if (!compId || !matchId || !window.API) return undefined;
    let cancelled = false;
    (async () => {
      // Prefer the players already on the passed competition (TvDisplay /
      // StreamingOverlay carry them) and skip the extra fetchCompetitionDetails
      // round-trip — it delays lineup rendering on every promoted-match change.
      let players = (competition && competition.players && competition.players.length)
        ? competition.players
        : [];
      if (!players.length) {
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
  }, [compId, matchId, sideAId, sideBId, roundIndex, match?.round, lineupVersion]);

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
const WAZA_NAMES = { M: "Men (head)", K: "Kote (wrist)", D: "Do (body)", T: "Tsuki (throat)", H: "Hansoku (penalty)", S: "Sune (shin)", "○": "Default win" };

function slotCells(letters, side, testid) {
  const cells = [0, 1].map(i => {
    const ch = letters[i] || "";
    return (
      <span key={i} className={"msb-slot" + (side === "aka" ? " msb-slot--aka" : "")}
        title={WAZA_NAMES[ch] || undefined}
        data-testid={i === 0 ? testid : undefined}>{ch}</span>
    );
  });
  return side === "aka" ? cells.reverse() : cells;
}

// centreMarks — the §263 inner cells: [shiro slot][shiro slot] | vs/X | [aka slot][aka slot].
// Hansoku ▲ shows on the offending side, on the OUTER edge of the slots (away
// from centre); X marks a hikiwake; "Ht" flags hantei. For an ippon-less win
// the winning side is otherwise invisible, so we mark the winner's first slot:
// "Ht" when decided by hantei, else ○ for a non-hantei ippon-less win (see
// the winMark line below). Modern fusensho/kiken carry ["○","○"] ippons
// and render through the normal slot path, so they never reach this fallback.
// A plain helper (not a component) so it renders inline into the parent's tree.
function centreMarks(sub, matchSideA, matchSideB) {
  const lettersB = ipponLetters(sub.ipponsB); // shiro / left
  const lettersA = ipponLetters(sub.ipponsA); // aka / right
  const foulB = boutHansokuMark(sub.hansokuB);
  const foulA = boutHansokuMark(sub.hansokuA);
  const isDraw = !sub.decidedByHantei &&
    typeof window.isHikiwake === "function" &&
    (window.isHikiwake(sub.score?.type) || window.isHikiwake(sub.decision));
  // Win mark only when there are no ippon letters (otherwise the letters
  // already show who won). sideB = shiro/left, sideA = aka/right.
  // Fallback chain: sub-level side → daihyosen team alias → match-level side
  // (quick-score sub-bouts have empty sub.sideA/sideB).
  const noIppons = !lettersB.some(Boolean) && !lettersA.some(Boolean);
  const winShiro = !!(noIppons && sub.winner &&
    (sub.winner === sub.sideB || sub.winner === sub.teamB || (matchSideB && sub.winner === matchSideB)));
  const winAka = !!(noIppons && sub.winner &&
    (sub.winner === sub.sideA || sub.winner === sub.teamA || (matchSideA && sub.winner === matchSideA)));
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
        {isDraw ? <span data-testid="sub-row-draw">X</span>
          : sub.decidedByHantei && !hasWinSide ? <span className="msb-ht" data-testid="sub-row-hantei">Ht</span>
          : <span className="msb-sep" aria-hidden="true">–</span>}
      </span>
      <span className={"msb-slots msb-slots--aka" + (winAka ? " msb-slots--win" : "")}>
        {winAka ? slotCells([winMark, ""], "aka", "sub-win-a") : slotCells(lettersA, "aka")}
        {foulA && <span className="msb-hansoku" data-testid="foul-mark-a">{foulA}</span>}
      </span>
    </span>
  );
}

// BoutSubRow — one FIK bout row: Shiro name | ippon slots · vs · ippon slots | Aka name.
// TV sizing is driven by the parent `.msb--tv` CSS selector, not a prop.
// state: "now" | "queued" | "done" (TV highlight only). Names come from the
// pinned lineup, else the per-bout competitor stored on the sub (kachinuki
// matches carry sub.sideA/sub.sideB), else the bout number — never the team
// name (it would repeat on every row).
export function BoutSubRow({ sub, index, lineupA, lineupB, teamSize, isDH, state, matchSideA, matchSideB }) {
  const subSideName = (v) => {
    const n = (v && v.name) || (typeof v === "string" ? v : "");
    if (!n) return "";
    // Filter out match-level team names — when the backend stores the team
    // name in every sub-bout (quick-score path), we must fall through to the
    // bout number rather than repeating the team name on every row.
    if (n === matchSideA || n === matchSideB) return "";
    return n;
  };
  const boutNum = isDH ? "DH" : "#" + (sub && sub.position > 0 ? sub.position : index + 1);
  const shiroName = (lineupB ? pickFromLineup(lineupB, index, teamSize) : "") || subSideName(sub && sub.sideB) || boutNum;
  const akaName = (lineupA ? pickFromLineup(lineupA, index, teamSize) : "") || subSideName(sub && sub.sideA) || boutNum;
  // TV sizing comes from the parent `.msb--tv .msb-row` selector, so no
  // per-row --tv modifier is needed here.
  const cls = "msb-row"
    + (state === "now" ? " msb-row--now" : "")
    + (state === "queued" ? " msb-row--queued" : "")
    + (isDH ? " msb-row--dh" : "");
  return (
    <div className={cls} data-testid={isDH ? "sub-row-dh" : `sub-row-${index}`}>
      <span className="msb-name" data-testid="sub-shiro-name">{shiroName}</span>
      {centreMarks(sub, matchSideA, matchSideB)}
      <span className="msb-name msb-name--aka" data-testid="sub-aka-name">{akaName}</span>
    </div>
  );
}

// Aggregate IV (individual victories) + PW (points won) per side from the
// regular (non-DH) bouts. sideB = shiro/left, sideA = aka/right.
export function teamIVPW(subResults, matchSideA, matchSideB) {
  let ivShiro = 0, ivAka = 0, pwShiro = 0, pwAka = 0;
  for (const s of (subResults || []).filter(x => x.position !== -1)) {
    const a = ipponLetters(s.ipponsA).filter(Boolean).length;
    const b = ipponLetters(s.ipponsB).filter(Boolean).length;
    pwShiro += b; pwAka += a;
    // Mirror Go backend pattern (scoring.go): check match-level side name
    // first, then sub-level side name (guarded against "" == "" false
    // positive). Quick-scored bouts have empty sub-level sides.
    const isAkaWin = s.winner && (s.winner === matchSideA || (s.sideA && s.winner === s.sideA));
    const isShiroWin = s.winner && (s.winner === matchSideB || (s.sideB && s.winner === s.sideB));
    if (isAkaWin) ivAka++;
    else if (isShiroWin) ivShiro++;
    else if (b > a) ivShiro++;
    else if (a > b) ivAka++;
  }
  return { ivShiro, ivAka, pwShiro, pwAka };
}

// IndividualScore — §263 row for an individual match: ippon slots per side
// (the match IS one bout). Renders the same CentreMarks as a bout row.
// withNumber — prepend the assigned competitor number (e.g. "K1") to the
// display name when present. Falls back to the bare name when no number is
// set, so competitions without `numberPrefix` render identically to before.
// Honours the zekken `displayName` when `withZekkenName` is true, matching
// `sideLabel` in display.jsx. Used by every individual-match name-rendering
// site (TV display, streaming overlay, viewer card, schedule list) so the
// number prefix appears consistently across all spectator surfaces.
export function withNumber(side, withZekkenName) {
  if (!side) return "TBD";
  if (typeof side === "string") return side;
  const name = (withZekkenName && side.displayName) ? side.displayName : (side.name || "TBD");
  return side.number ? `${side.number} ${name}` : name;
}

export function IndividualScore({ match, variant, showNames, withZekkenName }) {
  const sideName = (v) => v?.name || (typeof v === "string" ? v : "");
  const sideId = (v) => (v && v.id != null && v.id !== "") ? String(v.id) : "";
  // centreMarks marks the ippon-less (hantei/decision) winner by comparing the
  // winner key to each side's key. Prefer the participant id so a same-name
  // head-to-head (two players sharing a name) isn't flagged a win on BOTH
  // sides; fall back to the name. When the two sides are indistinguishable
  // (same name, no ids), blank the winner so neither side is marked — the
  // centre Ht/○ fallback still conveys the result.
  const aKey = sideId(match.sideA) || sideName(match.sideA);
  const bKey = sideId(match.sideB) || sideName(match.sideB);
  const ambiguous = !!aKey && aKey === bKey;
  const sub = {
    ipponsA: match.ipponsA || (window.ipponsFromScore ? window.ipponsFromScore(match.scoreA) : []),
    ipponsB: match.ipponsB || (window.ipponsFromScore ? window.ipponsFromScore(match.scoreB) : []),
    hansokuA: match.hansokuA, hansokuB: match.hansokuB,
    decidedByHantei: match.decidedByHantei, score: match.score, decision: match.decision,
    winner: ambiguous ? "" : (sideId(match.winner) || sideName(match.winner)),
    sideA: aKey, sideB: bKey,
  };
  // showNames fills the (otherwise empty) name spans with the two competitors,
  // colour-coded Shiro dark / Aka red — used by the TV pool/round list where
  // each row IS a full match. The card leaves them empty (names render above).
  // Always display the human NAME (never the id key used for comparison).
  // withNumber prepends the assigned competitor number (e.g. "K1 Tanaka") when
  // the competition has a numberPrefix configured; falls back to the bare name.
  // tri-review #2: pass withZekkenName so zekken-mode comps render the
  // displayName ("K1 TANAKA") instead of the canonical full name.
  const shiroDisplay = withNumber(match.sideB, withZekkenName);
  const akaDisplay = withNumber(match.sideA, withZekkenName);
  return (
    <div className={"msb msb-individual" + (variant === "tv" ? " msb--tv" : "")} data-testid="individual-score">
      <div className="msb-row">
        <span className="msb-name" data-testid={showNames ? "indiv-shiro-name" : undefined}>{showNames ? shiroDisplay : ""}</span>
        {centreMarks(sub)}
        <span className="msb-name msb-name--aka" data-testid={showNames ? "indiv-aka-name" : undefined}>{showNames ? akaDisplay : ""}</span>
      </div>
    </div>
  );
}

// TeamScoreboard — §277 team table: an IV/PW summary row (labeled, per side) +
// one BoutSubRow per regular bout + the Daihyosen banner + rep-bout row when
// `showDH`. Shiro left/dark, Aka right/red.
export function TeamScoreboard({ subResults, lineupA, lineupB, teamSize, showDH, variant, shiroName, akaName, matchSideA, matchSideB, isRunning }) {
  const regular = (subResults || []).filter(s => s.position !== -1);
  const { ivShiro, ivAka, pwShiro, pwAka } = teamIVPW(subResults, matchSideA, matchSideB);
  // FIK: a Daihyosen (representative bout) only happens when the team match is
  // TIED after the regular bouts — equal individual victories AND equal points.
  // Guard the render on the tie so a stale/invalid position:-1 sub never shows a
  // Daihyosen on an already-decided match (mp-13y #12).
  const tied = ivShiro === ivAka && pwShiro === pwAka;
  const renderDH = !!showDH && tied;
  const dhSub = renderDH ? (subResults || []).find(s => s.position === -1) : null;
  const tv = variant === "tv";
  // The current bout = first unscored regular bout (navy "now" highlight via
  // var(--accent-soft) — the running signal), but only while the match is
  // RUNNING (see rowState below). Already-scored bouts are "done"; unscored
  // bouts are "queued". On a non-running board (completed or up-next) nothing
  // is "now": a completed match that left padded/unplayed positions unscored
  // (e.g. a quick-score synthesising fewer subResults than teamSize) keeps
  // those rows "queued", not "done".
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
  // Render one row PER LINEUP POSITION (teamSize), padding past the recorded
  // subResults so a running encounter shows all bouts — completed, the live one,
  // and the still-to-come positions — not just the scored ones. Kachinuki can
  // exceed teamSize, so never shrink below regular.length.
  const rowCount = Math.max(regular.length, teamSize || 0);
  const scoredAt = (i) => i < regular.length && isScored(regular[i]);
  // Per-row state: a scored bout is "done"; the first unscored bout is "now"
  // ONLY when the match is RUNNING (so a 0–0 running board highlights bout 1);
  // every other unscored bout is "queued". Gating "now" on isRunning means a
  // completed match — including a quick-score that synthesised fewer
  // subResults than teamSize — never lights up a padded blank row, and an
  // up-next board stays all-queued.
  let firstUnscored = -1;
  for (let i = 0; i < rowCount; i++) { if (!scoredAt(i)) { firstUnscored = i; break; } }
  const rowState = (i) => {
    if (scoredAt(i)) return "done";
    if (isRunning && i === firstUnscored) return "now";
    return "queued";
  };

  return (
    <div className={"msb msb-team" + (tv ? " msb--tv" : "")} data-testid="team-scoreboard">
      {/* §277 summary row: team name + IV then PW per side */}
      <div className="msb-row msb-row--summary" data-testid="team-summary">
        <span className="msb-name" data-testid="summary-shiro-name">{shiroName || ""}</span>
        <span className="msb-marks">
          <span className="msb-slots">
            <span className="msb-slot msb-sum"><abbr className="msb-lab" title="Individual Victories">IV</abbr>{ivShiro}</span>
            <span className="msb-slot msb-sum"><abbr className="msb-lab" title="Points Won">PW</abbr>{pwShiro}</span>
          </span>
          <span className="msb-vs" />
          <span className="msb-slots msb-slots--aka">
            <span className="msb-slot msb-slot--aka msb-sum"><abbr className="msb-lab" title="Points Won">PW</abbr>{pwAka}</span>
            <span className="msb-slot msb-slot--aka msb-sum"><abbr className="msb-lab" title="Individual Victories">IV</abbr>{ivAka}</span>
          </span>
        </span>
        <span className="msb-name msb-name--aka" data-testid="summary-aka-name">{akaName || ""}</span>
      </div>

      {/* One row per lineup position (teamSize), padding past the recorded
          subResults so a running encounter shows the still-to-come bouts too —
          not just the scored ones (a partially-scored match used to render only
          its scored rows). A padding row has no sub: BoutSubRow shows the pinned
          lineup name when present, else the bout number (mp-13y #4/#6). */}
      {Array.from({ length: rowCount }, (_, i) => (
        <BoutSubRow key={i} sub={regular[i] || {}} index={i} lineupA={lineupA} lineupB={lineupB}
          teamSize={teamSize} isDH={false} state={rowState(i)} matchSideA={matchSideA} matchSideB={matchSideB} />
      ))}

      {/* Daihyosen banner + rep bout (knockout tie only). The DH sub is
          enriched with the parent team names (teamB=shiro, teamA=aka) so
          centreMarks can resolve a winner key stored as the TEAM name to
          the correct side — see centreMarks for the fallback chain. */}
      {renderDH && (
        <>
          <div className="msb-row msb-row--dh-banner" data-testid="dh-banner">
            <span className="msb-dh-tag">DAIHYOSEN</span>
          </div>
          {dhSub
            ? <BoutSubRow sub={{ ...dhSub, teamB: shiroName, teamA: akaName }}
                index={regular.length} lineupA={lineupA} lineupB={lineupB}
                teamSize={teamSize} isDH={true} state={isRunning ? "now" : "done"} matchSideA={matchSideA} matchSideB={matchSideB} />
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
  window.withNumber = withNumber;
}
