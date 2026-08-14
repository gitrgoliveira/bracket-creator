// match_scoreboard.jsx: the ONE FIK dantai-shiai scoreboard, shared by every
// surface that shows a match's detail: the viewer card (MatchDetailCard), the
// self-run modal (MatchViewerModal → MatchDetailCard), and the TV display
// (TvDisplay). Built straight from running_a_kendo_tournament.md
// §263 (individual: ippon-letter slots) and §277 (team: IV/PW summary row +
// per-bout rows + Daihyosen).
//
// The build (Makefile `esbuild-jsx`) runs esbuild with --outdir and NO --bundle,
// so each web-mobile/js/*.jsx is TRANSFORMED in place and its imports stay as
// runtime ESM the browser resolves and caches once. Importing this from both
// viewer.jsx and display.jsx is therefore the established DRY mechanism (same as
// lineup_resolver.jsx). This file imports only small leaves and reaches
// bracket.jsx's display primitives through window globals — for the dependency
// reasoning (and the pre-existing bracket double-load it avoids), see the ONE
// statement in result_slot.jsx's header; do not restate it here.
//
// `variant` ("card" | "tv") only changes sizing via a CSS modifier: the markup
// and data-testids are identical across surfaces.

import { resolveMatchLineup, resolveLineupTeamId, pickFromLineup, resolveBoutSideName, kachinukiHidesLineupPosition } from './lineup_resolver.jsx';
import { DAIHYOSEN_POSITION } from './pool_ids.jsx';
import { resultSlot, realIppons, hanteiTied, nameOf } from './result_slot.jsx';

const { useState: useSB, useEffect: useEB } = React;

// boutHansokuMark: red ▲ for ONE outstanding hansoku (fouls % 2 === 1). On the
// second hansoku the ▲ is deleted and 1 ippon (H) goes to the opponent, so a
// side never shows two triangles (FIK Shinpan Management p.15, Table 1).
export function boutHansokuMark(foulCount) {
  return (foulCount || 0) % 2 === 1 ? "▲" : "";
}

// useTeamLineups: fetch per-match lineups for both sides of a team match.
// Unifies the former viewer useTeamLineups + display useTvTeamLineups: pass the
// competition explicitly when available (TV/SSE), else it falls back to
// match.compId (viewer). Returns { lineupA, lineupB }; degrades to null/null
// when window.API is unavailable (public surfaces) → callers fall back to bout
// numbers.
//
// `roundIndex` (optional, 0-based) is the authoritative round for the
// round-scoped lineup fallback. Callers that know the bracket round (the TV
// display / overlay carry it as promoted.roundIndex) MUST pass it: do not
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
      // round-trip: it delays lineup rendering on every promoted-match change.
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
      // fall back to match.round: a raw numeric index, or the legacy engine
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
      // Both sides are independent GETs: fetch them in parallel to halve the
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

// Real ippon letters for a side (realIppons, the shared leaf filter), capped
// at the 2 sanbon slots; pads to exactly 2 so the slot columns always align.
function ipponLetters(arr) {
  const real = realIppons(arr);
  return [real[0] || "", real[1] || ""];
}

// subWinnerSides: does sub.winner name the shiro or aka side? The ONE
// cross-level chain (sub side → daihyosen team alias → match-level side),
// shared by centreMarks' marks and teamIVPW's IV attribution so the bout rows
// and the summary row can never disagree about who a winner names. A winner
// matching BOTH sides is INVALID data (team names are unique by rule; only
// drifted or hand-edited files can produce it) and resolves defensively to
// AKA — the side-A-first order Go uses everywhere for the identical case
// (isWinForSide in engine/scoring.go, TeamResultFrom in state/team_result.go,
// SideMarksLR in export/suffix.go) — so the on-screen rows, the IV summary,
// the server standings and the Excel export all agree on the SAME arbitrary
// side. Never both: asserting two winners was the bug; disagreeing with the
// server's numbers was the fix's bug. The truth is not in the data
// (arbitrary-but-consistent, see api_serializers.jsx).
function subWinnerSides(sub, matchSideA, matchSideB) {
  const w = sub.winner;
  const aka = !!(w && (w === sub.sideA || w === sub.teamA || (matchSideA && w === matchSideA)));
  const shiro = !aka && !!(w && (w === sub.sideB || w === sub.teamB || (matchSideB && w === matchSideB)));
  return { shiro, aka };
}

// letters[0] is the OUTER ippon (the first point scored), letters[1] the inner.
// "Outer"/"inner" are relative to that side's OWN TWO SLOTS, never to the board:
// the names hold the board's outer edges and both slot groups flank the centre
// vs (FIK Table 2, p.16). Ippons fill from each name toward the centre: shiro
// fills left→right (its outer slot is the left), aka fills right→left (its outer
// slot is the right), so for aka we reverse the visual cell order. The testid
// stays on the LOGICAL OUTER cell (letters[0]) regardless of which side renders
// it — and on a win group that is NOT necessarily the Ht cell: at 1-1 the outer
// cell holds the winner's real letter and the Ht fills the inner one, so
// `sub-win-*` means "the winner's outer cell", never "the mark cell". At 0-0 the
// Ht DOES land there (it takes the first free slot), so a selector on it proves
// the mark's position only when the fixture's scoreline is stated too; assert on
// the whole `.msb-slots` group when you mean "somewhere in the win group".
const WAZA_NAMES = { M: "Men (head)", K: "Kote (wrist)", D: "Do (body)", T: "Tsuki (throat)", H: "Hansoku (penalty)", S: "Sune (shin)", "○": "Default win", Ht: "Hantei (judges' decision)" };

function slotCells(letters, side, testid) {
  const cells = [0, 1].map(i => {
    const ch = letters[i] || "";
    return (
      <span key={i} className={"msb-slot" + (side === "aka" ? " msb-slot--aka" : "")}
        title={WAZA_NAMES[ch] || undefined}
        data-testid={i === 0 ? testid : undefined}>{ch}</span>
    );
  });
  return side === "aka" ? cells.toReversed() : cells;
}

// centreMarks: the §263 inner cells: [shiro slot][shiro slot] | vs/X/(E)/(DH) | [aka slot][aka slot].
// Hansoku ▲ shows between the offending competitor's name and that side's ippon
// slots (FIK Table 2, p.16 Taisho row: White's ▲ far left, Red's far right, each
// on its own name side); X marks a hikiwake. For an ippon-less DEFAULT WIN the
// winning side is otherwise invisible, so we mark its slots with the maru pair
// ○ ○, one per awarded point (see resultCells below). Modern fusensho/kiken
// carry ["○","○"] ippons and render through the normal slot path, so they never
// reach that fallback. The hantei "Ht" is NOT part of it: it has its own gate
// (`markable` below), which unlike the maru fallback does not require an empty
// scoreline — a 1-1 hantei is the normal case — but DOES require the letters to
// be tied, so a drifted untied row renders no Ht at all.
// A plain helper (not a component) so it renders inline into the parent's tree.
function centreMarks(sub, matchSideA, matchSideB) {
  // Engi (flag-count scoring) is the ONLY competition type where the centre
  // marks are numeric (a referee flag count, single digit 0-5). There are no
  // ippon letters, hansoku fouls, or hikiwake draws in an engi bout, so this
  // branches before all of that logic; sub.flagsA/flagsB=sideA(Aka)/sideB
  // (Shiro), same convention as engiFlagScore in bracket.jsx.
  if (sub.flagsA != null || sub.flagsB != null) {
    const flagsB = sub.flagsB || 0, flagsA = sub.flagsA || 0;
    const winShiro = flagsB > flagsA;
    const winAka = flagsA > flagsB;
    return (
      <span className="msb-marks" data-testid="sub-marks">
        <span className={"msb-slots" + (winShiro ? " msb-slots--win" : "")}>
          {slotCells([String(flagsB), ""], "shiro", "sub-flags-b")}
        </span>
        <span className="msb-vs">
          <span className="msb-sep" aria-hidden="true">vs</span>
        </span>
        <span className={"msb-slots msb-slots--aka" + (winAka ? " msb-slots--win" : "")}>
          {slotCells([String(flagsA), ""], "aka", "sub-flags-a")}
        </span>
      </span>
    );
  }
  const lettersB = ipponLetters(sub.ipponsB); // shiro / left
  const lettersA = ipponLetters(sub.ipponsA); // aka / right
  const foulB = boutHansokuMark(sub.hansokuB);
  const foulA = boutHansokuMark(sub.hansokuA);
  // The centre chip comes from the single middle-value source's chip
  // projection (matchMiddleMark: X / (E) / (DH), "" when the middle is the
  // plain "vs" — never a dash). Nothing else may write the middle: the centre
  // carries SHARED marks only, never a result belonging to one competitor.
  const mid = window.matchMiddleMark ? window.matchMiddleMark(sub) : "";
  const noIppons = !lettersB.some(Boolean) && !lettersA.some(Boolean);
  // A hantei ALWAYS names a winner and is decided from a TIED scoreline
  // (validation.go: "requires winner to be set" + "requires a tied scoreline"),
  // so its mark is NOT gated on noIppons: a 1-1 hantei after encho is the
  // normal case, and its slots already hold letters. It IS gated on the
  // letters actually being tied, mirroring validation and boutWinnerSide:
  // a drifted decidedByHantei on an untied row (2-1) renders its letters
  // plainly rather than fabricating a judges'-decision mark. The maru pair
  // for an ippon-less default win (fusensho/kiken/bye) still gates on
  // noIppons, because there the empty letters are what make room for it.
  // Tied is judged on the RAW recorded ippons via the shared hanteiTied leaf
  // rule, not the display pair: ipponLetters caps each side at the 2 sanbon
  // slots, so a drifted 3-2 row would compare as 2-2 and pass the gate it is
  // meant to fail.
  const markable = (sub.decidedByHantei && hanteiTied(sub.ipponsA, sub.ipponsB)) || noIppons;
  // Which side the result mark belongs to (sideB = shiro/left, sideA = aka/right),
  // via subWinnerSides: the one cross-level chain, shared with teamIVPW, that
  // falls back sub-level side → daihyosen team alias → match-level side for the
  // quick-score bouts with empty sub.sideA/sideB, and resolves a both-sides
  // match aka-first to align every JS surface with the Go standings/export.
  const { shiro: winShiro, aka: winAka } = markable
    ? subWinnerSides(sub, matchSideA, matchSideB)
    : { shiro: false, aka: false };
  // Ht behaves like a point and rides beside the competitor it names; the slot
  // it takes is the shared rule in result_slot.jsx (which the team editor uses
  // too), so it is not restated here. `loose` means both slots were full, and
  // the mark then renders inboard of them rather than being dropped.
  const resultCells = (letters) => {
    if (!sub.decidedByHantei) {
      return { cells: window.defaultWinMaru ? window.defaultWinMaru(sub.encho) : ["○", "○"], loose: false };
    }
    const cells = letters.slice(0, 2);
    const { slot, loose } = resultSlot(cells);
    if (slot >= 0) cells[slot] = "Ht";
    return { cells, loose };
  };
  const shiroRes = winShiro ? resultCells(lettersB) : null;
  const akaRes = winAka ? resultCells(lettersA) : null;
  return (
    <span className="msb-marks" data-testid="sub-marks">
      <span className={"msb-slots" + (winShiro ? " msb-slots--win" : "")}>
        {foulB && <span className="msb-hansoku" data-testid="foul-mark-b">{foulB}</span>}
        {shiroRes ? slotCells(shiroRes.cells, "shiro", "sub-win-b") : slotCells(lettersB, "shiro")}
        {shiroRes?.loose && <span className="msb-ht" data-testid="sub-ht-b">Ht</span>}
      </span>
      <span className="msb-vs">
        {mid ? <span data-testid="sub-row-mid">{mid}</span>
          : <span className="msb-sep" aria-hidden="true">vs</span>}
      </span>
      <span className={"msb-slots msb-slots--aka" + (winAka ? " msb-slots--win" : "")}>
        {akaRes?.loose && <span className="msb-ht" data-testid="sub-ht-a">Ht</span>}
        {akaRes ? slotCells(akaRes.cells, "aka", "sub-win-a") : slotCells(lettersA, "aka")}
        {foulA && <span className="msb-hansoku" data-testid="foul-mark-a">{foulA}</span>}
      </span>
    </span>
  );
}

// BoutSubRow: one FIK bout row: Shiro name | ippon slots · vs · ippon slots | Aka name.
// TV sizing is driven by the parent `.msb--tv` CSS selector, not a prop.
// state: "now" | "queued" | "done" (TV highlight only). Names come from the
// pinned lineup, else the per-bout competitor stored on the sub (kachinuki
// matches carry sub.sideA/sub.sideB), else the bout number: never the team
// name (it would repeat on every row).
//
// kachinuki (boolean): when true, flip resolution to server-bout-first.
// Lineup fallback is used ONLY for the index-0 bootstrap (the initial
// senpo-vs-senpo pairing); later rows must never show position-N lineup
// names because kachinuki bouts are winner-stays, not position-keyed.
export function BoutSubRow({ sub, index, lineupA, lineupB, teamSize, isDH, state, matchSideA, matchSideB, kachinuki }) {
  const subSideName = (v) => {
    const n = nameOf(v);
    if (!n) return "";
    // Filter out match-level team names: when the backend stores the team
    // name in every sub-bout (quick-score path), we must fall through to the
    // bout number rather than repeating the team name on every row.
    if (n === matchSideA || n === matchSideB) return "";
    return n;
  };
  const boutNum = isDH ? "DH" : "#" + (sub && sub.position > 0 ? sub.position : index + 1);
  // Name priority is resolveBoutSideName (lineup_resolver.jsx): kachinuki is
  // server-bout-first with the lineup only seeding the index-0 bootstrap;
  // fixed format is lineup-first.
  // Daihyosen (isDH) is lineup-first even in kachinuki: the rep bout is not a
  // server-driven kachinuki bout, so never blank its lineup pick (the DH row is
  // rendered without the kachinuki prop today, but !isDH keeps this correct if
  // a caller ever passes it).
  const lineupNameFor = (lu) =>
    kachinukiHidesLineupPosition(kachinuki, isDH, index) ? "" : (lu ? pickFromLineup(lu, index, teamSize) : "");
  const resolveSide = (subSide, lu) =>
    resolveBoutSideName({ isKachinuki: kachinuki, isDaihyosen: isDH, existingName: subSideName(sub && subSide), lineupName: lineupNameFor(lu) }) || boutNum;
  const shiroName = resolveSide(sub && sub.sideB, lineupB);
  const akaName = resolveSide(sub && sub.sideA, lineupA);
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
  // Count only real numbered bouts: skip the daihyosen sentinel AND any
  // malformed negative position, mirroring the Go-side defensive skip
  // (state.TeamResultFrom: Position <= DaihyosenSubPosition).
  for (const s of (subResults || []).filter(x => x.position > DAIHYOSEN_POSITION)) {
    const a = ipponLetters(s.ipponsA).filter(Boolean).length;
    const b = ipponLetters(s.ipponsB).filter(Boolean).length;
    pwShiro += b; pwAka += a;
    // Mirror Go backend pattern (scoring.go): check match-level side name
    // first, then sub-level side name (guarded against "" == "" false
    // positive). Quick-scored bouts have empty sub-level sides.
    // IV attribution runs through subWinnerSides — the SAME resolver the bout
    // rows use, aka-first on a both-sides match like Go's isWinForSide — so
    // rows, summary, server standings and the Excel export all agree. The
    // ippon comparison below still decides where the winner names nobody.
    const wsides = subWinnerSides(s, matchSideA, matchSideB);
    const isAkaWin = wsides.aka;
    const isShiroWin = wsides.shiro;
    if (isAkaWin) ivAka++;
    else if (isShiroWin) ivShiro++;
    else if (b > a) ivShiro++;
    else if (a > b) ivAka++;
  }
  return { ivShiro, ivAka, pwShiro, pwAka };
}

// IndividualScore: §263 row for an individual match: ippon slots per side
// (the match IS one bout). Renders the same CentreMarks as a bout row.
// withNumber: prepend the assigned competitor number (e.g. "K1") to the
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

// shiroName / akaName: optional resolved display names, mirroring the props
// TeamScoreboard already takes. A caller that has better names than this
// component can derive passes them in — the viewer card resolves an unplayed
// bracket side to its feeder label ("Winner of M1"), where withNumber below can
// only say "TBD". Omit them and the component derives its own, as the TV boards
// and the lobby do. They are STRINGS: the dojo second line is this component's
// job (showDojo), not something a caller splices into the name as a VNode.
//
// showDojo: render each competitor's dojo as a second line UNDER their name.
// The rule lives here so a surface that wants it passes a flag instead of
// reaching into these internals with its own selector — the shape that produced
// the `.msb-sep { display: none }` bug this component just had to fix.
export function IndividualScore({ match, variant, showNames, withZekkenName, shiroName, akaName, showDojo }) {
  // nameOf, not a local unwrap: same object-or-bare-string rule the slot leaf's
  // hanteiWinnerKey applies, so the two attribution paths cannot read a side
  // name differently.
  const sideId = (v) => (v && v.id != null && v.id !== "") ? String(v.id) : "";
  // centreMarks marks the ippon-less (hantei/decision) winner by comparing the
  // winner key to each side's key. Prefer the participant id so a same-name
  // head-to-head (two players sharing a name) isn't flagged a win on BOTH
  // sides; fall back to the name. When the two sides are indistinguishable
  // (same name, no ids), blank the winner so neither side is marked. Nothing
  // then reports the result on this row: the centre carries shared marks only,
  // so there is deliberately no centre fallback to fall back TO. Unreachable
  // through the API, which requires a winner and disambiguates same-name pairs
  // by uuid.
  const aKey = sideId(match.sideA) || nameOf(match.sideA);
  const bKey = sideId(match.sideB) || nameOf(match.sideB);
  const ambiguous = !!aKey && aKey === bKey;
  const sub = {
    ipponsA: match.ipponsA || (window.ipponsFromScore ? window.ipponsFromScore(match.scoreA) : []),
    ipponsB: match.ipponsB || (window.ipponsFromScore ? window.ipponsFromScore(match.scoreB) : []),
    hansokuA: match.hansokuA, hansokuB: match.hansokuB,
    decidedByHantei: match.decidedByHantei, score: match.score, decision: match.decision,
    // encho MUST be threaded: without it matchMiddleMark can never yield (E) on
    // an individual row, and defaultWinMaru would award the regulation ○○ for a
    // default win that actually happened in overtime, where the rulebook marks
    // a single ○. This row centre is the mark's ONE home (operator ruling):
    // the TV/lobby header chips that used to duplicate X/(E)/(DH) above it
    // were removed; only the OBS overlay, which renders no row, keeps a chip.
    encho: match.encho,
    winner: ambiguous ? "" : (sideId(match.winner) || nameOf(match.winner)),
    sideA: aKey, sideB: bKey,
    flagsA: match.flagsA, flagsB: match.flagsB,
  };
  // showNames fills the (otherwise empty) name spans with the two competitors,
  // colour-coded Shiro dark / Aka red: used by the TV pool/round list where each
  // row IS a full match, and by the viewer's match card, which has no name row of
  // its own (a competitor's points must never sit under their name).
  // Always display the human NAME (never the id key used for comparison).
  // withNumber prepends the assigned competitor number (e.g. "K1 Tanaka") when
  // the competition has a numberPrefix configured; falls back to the bare name.
  // tri-review #2: pass withZekkenName so zekken-mode comps render the
  // displayName ("K1 TANAKA") instead of the canonical full name.
  const shiroDisplay = shiroName ?? withNumber(match.sideB, withZekkenName);
  const akaDisplay = akaName ?? withNumber(match.sideA, withZekkenName);
  // Name over dojo, the same block the bracket's PlayerLine and the up-next row
  // render. A SECOND LINE UNDER THE NAME only: the ippon slots stay on the
  // name's row, vertically centred against the block, because a competitor's
  // points must never sit beneath their name (operator ruling). A side with no
  // dojo (an unresolved feeder, "Winner of M1") renders the bare name, so the
  // row does not gain an empty line.
  // bc-dojo carries the shared dojo type (11px, ink-3, ellipsised); msb-dojo
  // adds only what makes it a second LINE, rather than restating those five.
  const shiroDojo = (showDojo && match.sideB && match.sideB.dojo) || "";
  const akaDojo = (showDojo && match.sideA && match.sideA.dojo) || "";
  const nameCell = (display, dojo) =>
    dojo ? <>{display}<span className="bc-dojo msb-dojo">{dojo}</span></> : display;
  // Emphasise the decided winner's NAME. sub.winner is already id-first with a
  // name fallback and is blanked for an indistinguishable same-name pair, so
  // neither side lights up when the data cannot attribute the win. This is the
  // one place the rule lives: the viewer card used to bold its own name row via
  // .match-detail-card__side--win, which meant every other surface that renders
  // this row marked an ordinary ippon win nowhere (.msb-slots--win only fires
  // for an ippon-LESS result, i.e. hantei or a default win).
  const winShiroName = !!sub.winner && sub.winner === sub.sideB;
  const winAkaName = !!sub.winner && sub.winner === sub.sideA;
  return (
    <div className={"msb msb-individual" + (variant === "tv" ? " msb--tv" : "")} data-testid="individual-score">
      <div className="msb-row">
        <span className={"msb-name" + (shiroDojo ? " msb-name--stacked" : "") + (winShiroName ? " msb-name--win" : "")} data-testid={showNames ? "indiv-shiro-name" : undefined}>{showNames ? nameCell(shiroDisplay, shiroDojo) : ""}</span>
        {centreMarks(sub)}
        <span className={"msb-name msb-name--aka" + (akaDojo ? " msb-name--stacked" : "") + (winAkaName ? " msb-name--win" : "")} data-testid={showNames ? "indiv-aka-name" : undefined}>{showNames ? nameCell(akaDisplay, akaDojo) : ""}</span>
      </div>
    </div>
  );
}

// TeamScoreboard: §277 team table: an IV/PW summary row (labeled, per side) +
// one BoutSubRow per regular bout + the Daihyosen banner + rep-bout row when
// `showDH`. Shiro left/dark, Aka right/red.
// kachinuki (boolean, default false): when true the match uses winner-stays
// ordering. Row count is driven by recorded bouts (never padded to teamSize)
// and name resolution is server-bout-first (see BoutSubRow).
export function TeamScoreboard({ subResults, lineupA, lineupB, teamSize, showDH, variant, shiroName, akaName, matchSideA, matchSideB, isRunning, kachinuki }) {
  // Real numbered bouts only: exclude the daihyosen sentinel and any malformed
  // negative position (mirrors the Go-side defensive skip).
  const regular = (subResults || []).filter(s => s.position > DAIHYOSEN_POSITION);
  const { ivShiro, ivAka, pwShiro, pwAka } = teamIVPW(subResults, matchSideA, matchSideB);
  // FIK: a Daihyosen (representative bout) only happens when the team match is
  // TIED after the regular bouts: equal individual victories AND equal points.
  // Guard the render on the tie so a stale/invalid position:-1 sub never shows a
  // Daihyosen on an already-decided match (mp-13y #12).
  const tied = ivShiro === ivAka && pwShiro === pwAka;
  const renderDH = !!showDH && tied;
  const dhSub = renderDH ? (subResults || []).find(s => s.position === DAIHYOSEN_POSITION) : null;
  const tv = variant === "tv";
  // The current bout = first unscored regular bout (navy "now" highlight via
  // var(--accent-soft): the running signal), but only while the match is
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
  // Kachinuki: row count = recorded bouts only (no teamSize padding).
  // Show at least 1 row so the bootstrap senpo-vs-senpo bout is always visible.
  // Fixed-order: render one row per lineup position (teamSize), padding past
  // recorded subResults so a running encounter shows all bouts: completed,
  // the current one, and still-to-come positions.
  const rowCount = kachinuki ? Math.max(regular.length, 1) : Math.max(regular.length, teamSize || 0);
  const scoredAt = (i) => i < regular.length && isScored(regular[i]);
  // Per-row state: a scored bout is "done"; the first unscored bout is "now"
  // ONLY when the match is RUNNING (so a 0–0 running board highlights bout 1);
  // every other unscored bout is "queued". Gating "now" on isRunning means a
  // completed match: including a quick-score that synthesised fewer
  // subResults than teamSize: never lights up a padded blank row, and an
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
          subResults so a running encounter shows the still-to-come bouts too:
          not just the scored ones (a partially-scored match used to render only
          its scored rows). A padding row has no sub: BoutSubRow shows the pinned
          lineup name when present, else the bout number (mp-13y #4/#6). */}
      {Array.from({ length: rowCount }, (_, i) => (
        <BoutSubRow key={i} sub={regular[i] || {}} index={i} lineupA={lineupA} lineupB={lineupB}
          teamSize={teamSize} isDH={false} state={rowState(i)} matchSideA={matchSideA} matchSideB={matchSideB} kachinuki={!!kachinuki} />
      ))}

      {/* Rep bout (knockout tie only). No separate "DAIHYOSEN" text banner:
          the rep-bout row already carries the (DH) centre mark and a top-
          border divider (.msb-row--dh / .msb-dh-pending), so the banner was
          redundant. The DH sub is enriched with the parent team names
          (teamB=shiro, teamA=aka) so centreMarks can resolve a winner key
          stored as the TEAM name to the correct side: see centreMarks for
          the fallback chain. */}
      {renderDH && (dhSub
        ? <BoutSubRow sub={{ ...dhSub, teamB: shiroName, teamA: akaName }}
            index={regular.length} lineupA={lineupA} lineupB={lineupB}
            teamSize={teamSize} isDH={true} state={isRunning ? "now" : "done"} matchSideA={matchSideA} matchSideB={matchSideB} />
        : <div className="msb-dh-pending" data-testid="tvd-dh-pending">Daihyosen pending</div>)}
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
