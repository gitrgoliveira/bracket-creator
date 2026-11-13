// Pools section of a competition: standings rendered via the shared public
// PoolsViewer (draw order + rank/DH badges); matches open the score editor.

// Canonical pool-id parser shared with the display surfaces (single source of
// truth: ./pool_ids.jsx is a leaf module with no import chain).
import { poolNameOf, isSupplementaryBout, isPoolDaihyosenBout, teamMatchTypeFor } from './pool_ids.jsx';
import { nameOf } from './result_slot.jsx';
// checkinPid (id when non-empty, else "name|dojo") is the ONE owner of the
// id-else-name|dojo identity rule; the chusen banner below used to key
// its rank inputs with a self-contained chusenMemberKey because checkinPid's
// old `p.id ?? fallback` only fell back on null/undefined, not the ""
// chusen-candidates always sends for a legacy/UUID-less member. checkinPid's
// own truthiness check now closes that gap, so the banner delegates here
// like every other identity-keyed surface.
import { checkinPid } from './data.jsx';
import { NO_ID_POOL_HINT, NoIdHint } from './data_integrity.jsx';
// A recorded rank can move who holds a qualifying place after the knockout
// has started, and the server refuses it like a pool result correction
// (downstream_knockout_played): the chusen writes go through the same
// confirm-and-retry every score write uses.
import { attemptScoreWrite, DOWNSTREAM_KNOCKOUT_RANKING_CANCELLED, writeKeepsEditorOpen } from './write_result.jsx';
import { rankOrdinal } from './viewer_standings.jsx';
import { useUnsavedInput } from './app_version.jsx';
// Fighting a drawn tie-break bout again is the editors' reopen, so the notice
// below runs the same hook and shows the same court-busy panel.
import { useMatchReopen, ReopenFeedback } from './admin_scoring_shared.jsx';

const { useState, useEffect, useRef, useMemo } = React;
const EmptyState = window.EmptyState;
const ScoreEditorModal = window.ScoreEditorModal;

// poolNameOf is imported above from ./pool_ids.jsx (the shared
// pool-id parser; "PoolName-N" / "PoolName-DH-N" / "PoolName-TB-N" → pool name,
// hyphenated names preserved, unrecognised ids → "").

// Filter a flat poolMatches array down to entries belonging to a single pool.
// Uses poolNameOf (./pool_ids.jsx) so DH/TB suffix variants are handled correctly.
//
// pool.matches (helper.Match) carries only sideA/sideB: no id, status, or
// score data. poolMatches (state.MatchResult) has the full data including the
// id required by the score API endpoint. Score buttons in the pool-card view
// must use poolMatchesForPool to get the correct MatchResult objects.
//
// Exported for vitest at __tests__/admin_pools.test.jsx.
function poolMatchesForPool(poolMatches, poolName) {
  return (poolMatches || []).filter(m => poolNameOf(m.id) === poolName);
}

// Enrich a pool-match object with the comp-* metadata that
// ScoreEditorModal reads off the match prop. Pool matches arrive as the
// raw MatchResult shape (id, status, sides, ippons, decision) with none
// of the comp-level fields the modal needs:
//   * compKind / teamSize: picks TeamScoreEditorModal vs individual editor
//   * compId: fetches competition details (naginata)
//              and is the path for decision/score endpoints
//   * compName: header eyebrow
//   * phase / poolName: header subtitle ("CompName · PoolName")
//
// Pool name falls back to the prefix of the match id ("PoolName-MatchIdx"
// per parsePoolMatchesRecords in internal/state/pools.go) when the caller
// can't supply a known pool name. Existing falsy fields on `m` are
// overwritten with derived values: truthy fields are preserved so a
// server-injected compId or compKind from a later refresh is not clobbered.
//
// sideA/sideB are normalized from plain strings to {id,name} objects via
// buildPlayerMap so ScoreEditorModal can render competitor names without
// m.sideA?.name returning undefined.
//
// Note: teamSize uses `??` (not `||`) so an explicit teamSize=0 on the
// match (individual comp) is preserved instead of falling through to the
// comp's teamSize. The same `??` is applied to the comp.teamSize fallback
// so a null comp degrades to teamSize=0 (individual) rather than throwing.
//
// Exported for vitest at __tests__/admin_pools.test.jsx.
function enrichPoolMatchWithComp(m, comp, poolNameOverride) {
  if (!m) return m;
  const derivedPoolName = poolNameOverride || poolNameOf(m.id);
  const playerMap = window.buildPlayerMap ? window.buildPlayerMap(comp) : {};
  // bc-pnum: pool matches carry sideAId/sideBId (state.MatchResult flat
  // fields) alongside the plain-string side. Resolve by that id first: the
  // plain-name playerMap key collapses two same-name/different-dojo
  // participants onto whichever one buildPlayerMap added last, so a name-
  // only lookup can attach the WRONG dojo/number to this side. Name lookup
  // is the fallback only when the match carries no id for this side at all.
  const toPlayer = (side, sideId) => {
    if (side && typeof side === "object") return side;
    if (!side) return { id: "", name: "" };
    const p = sideId ? playerMap[sideId] : playerMap[side];
    // bc-pnum: an unresolved side gets id "", never the name -- resolveSide
    // (api_serializers.jsx) stopped inventing an id from the name, so this
    // matches that contract instead of reintroducing it here. Consumers key
    // on `side.id || side.name` (sideLookupKey), which already falls
    // through to the name when id is "".
    return p || { id: sideId || "", name: side };
  };
  // Pool daihyosen ("Pool X-DH-N") and tiebreaker ("Pool X-TB-N") bouts are
  // single representative/ippon-shobu matches, scored as INDIVIDUAL even in a
  // team competition: force compKind=""/teamSize=0 so ScoreEditorModal routes
  // to the individual editor (one bout), not the 5-person team sheet. This
  // mirrors the same override in viewer.jsx compMatches; without it, scoring a
  // team pool-DH from the Pools tab opens the wrong (team) scorer.
  const isSupplementary = isSupplementaryBout(m.id);
  // mp-62vr: for a team pool/league daihyosen/tiebreaker rep bout the SideA/SideB
  // are TEAM names; the operator must pick which player each team fields. Attach
  // each team's roster so ScoreEditorModal can render the two rep-player
  // dropdowns. comp.players entries ARE the teams (member names live in
  // team.metadata via AdminLineupHelpers.rosterFor); config may nest players.
  const isTeamComp = !!(comp && (comp.kind === "team" || comp.teamSize > 0));
  const repIsTeam = isSupplementary && isTeamComp;
  let repRosterA = [];
  let repRosterB = [];
  if (repIsTeam) {
    const teams = (comp && comp.config && comp.config.players) || (comp && comp.players) || [];
    const teamByName = (nm) => teams.find(t => ((t.name || t.Name) === nm));
    const rosterFor = (window.AdminLineupHelpers && window.AdminLineupHelpers.rosterFor) || (() => []);
    repRosterA = rosterFor(teamByName(nameOf(m.sideA))) || [];
    repRosterB = rosterFor(teamByName(nameOf(m.sideB))) || [];
  }
  return {
    ...m,
    sideA: toPlayer(m.sideA, m.sideAId),
    sideB: toPlayer(m.sideB, m.sideBId),
    compId: m.compId || (comp && comp.id) || "",
    compName: m.compName || (comp && comp.name) || "",
    compFormat: m.compFormat || (comp && comp.format) || "",
    compKind: isSupplementary ? "" : (m.compKind || (comp && comp.kind) || ""),
    teamSize: isSupplementary ? 0 : (m.teamSize ?? (comp && comp.teamSize) ?? 0),
    teamMatchType: isSupplementary ? "" : (m.teamMatchType || teamMatchTypeFor(comp)),
    compEngi: isSupplementary ? false : !!(m.compEngi ?? (comp && comp.engi)),
    phase: m.phase || "pool",
    poolName: m.poolName || derivedPoolName,
    // Rep-bout dropdown inputs (empty/false for non-supplementary matches).
    repIsTeam,
    repRosterA,
    repRosterB,
  };
}

// drawnSupplementaryBouts: the pool tie-break ("-TB-") and daihyosen ("-DH-")
// bouts that were finished as a draw. A draw settles nothing, so the pool keeps
// its tie (the engine's poolCompletion counts a winnerless supplementary bout
// as unfinished) and, in a Pools + Knockout competition, the knockout places it
// decides stay empty. Nothing else on the page says so, hence the notice.
// The rule is the engine's drawnSupplementaryBout (competition.go), read here
// from the wire fields; both are pinned to the one table
// internal/engine/testdata/drawn_supplementary_cases.json.
//
// A drawn supplementary bout is not stuck, and no notice names it, in two
// cases the engine's poolMatchSettled counts settled. The lots ruled a drawn
// daihyosen (daihyosenRuledByChusen): every team the pool's standings hold level
// on points with its two sides carries a rank in force, i.e. its standings row is
// isOverridden (the server marks only the ranks that still govern a tie no bout
// settled, poolRanksInForce). Or it is moot (drawnSupplementaryMoot, a daihyosen
// or a tie-break bout): a correction no longer holds its two sides level, or has
// moved their tie outside the band a tie is broken in (tieOutOfBand on the
// standings rows, tieNeedsBreak in Go), so it decides no place. The pool is
// judged by name and the teams by id only; a pool with no standings, or a side
// it does not hold, settles nothing. The second half of the same table
// (chusenCases) pins both copies.
//
// Exported for vitest at __tests__/admin_pools.test.jsx.
function drawnSupplementaryBouts(poolMatches, standings) {
  const rows = poolMatches || [];
  // lotsGroup in chusen.go: the rows level with both sides, or just the two
  // sides when they are no longer level, or none when a side is not held.
  const lotsGroup = (m) => {
    const poolRows = (standings || {})[poolNameOf(m.id)] || [];
    const rowOf = (id) => (id ? poolRows.find(s => s.player && s.player.id === id) : undefined);
    const a = rowOf(m.sideAId);
    const b = rowOf(m.sideBId);
    if (!a || !b) return { group: null, level: false };
    if (a.points !== b.points) return { group: [a, b], level: false };
    return { group: poolRows.filter(s => s.points === a.points), level: true };
  };
  const settledWithoutRematch = (m) => {
    const { group, level } = lotsGroup(m);
    if (!group) return false;
    // Moot: any drawn supplementary bout whose sides are no longer level, or
    // whose level group lies outside the band a tie is broken in (the server
    // marks those rows tieOutOfBand). Only a representative bout can be ruled
    // by the lots.
    return !level || group[0].tieOutOfBand || (isPoolDaihyosenBout(m.id) && group.every(s => s.isOverridden));
  };
  return rows.filter(m => isSupplementaryBout(m.id) && m.status === "completed" && !nameOf(m.winner) && !settledWithoutRematch(m));
}

// DrawnTieBreakNotice: names one such bout and the two ways on, fight it again
// (the reopen: the draw is cleared and the bout runs on its court) or open it
// to decide it by encho or hantei. Neither is forced: the pool stays as it is
// until the operator chooses.
function DrawnTieBreakNotice({ match, comp, password, onOpen }) {
  const ctl = useMatchReopen({ match, password, isComplete: true, allowCourtForce: true });
  const what = isPoolDaihyosenBout(match.id) ? "daihyosen" : "tie-break bout";
  // A league has one table and no pools: "Pool A" is only its storage name.
  const pool = comp && comp.format === "league" ? "" : `${poolNameOf(match.id)}: `;
  const sides = `${nameOf(match.sideA)} and ${nameOf(match.sideB)}`;
  const effect = comp && comp.format === "mixed"
    ? "so the knockout places this pool decides stay empty"
    : "so the table keeps them tied";
  return (
    <div className="alert alert--warn league-tiebreak" role="status" aria-live="polite" data-testid="drawn-tiebreak-notice">
      <div className="league-tiebreak__title">A {what} was saved as a draw</div>
      <div className="league-tiebreak__desc">
        {pool}{sides} are still tied, {effect}. Fight it again, or open it to decide it by encho or hantei.
      </div>
      <div className="league-tiebreak__actions">
        <button type="button" className="btn btn--sm btn--primary" disabled={ctl.busy || ctl.landed} onClick={() => ctl.reopen()}>
          {ctl.busy && <span className="spinner" />}
          {ctl.landed ? "Reopened" : "Fight it again"}
        </button>
        <button type="button" className="btn btn--sm btn--ghost" disabled={ctl.busy || ctl.landed} onClick={onOpen}>
          Open bout
        </button>
      </div>
      <ReopenFeedback ctl={ctl} testIdPrefix="drawn-tiebreak-reopen" />
    </div>
  );
}

// groupTeamIds derives the teamIds array to send alongside teamNames on a
// league-tiebreak generate/remove request: the candidates payload's `teams`
// array carries {id,name,dojo} per team, positionally parallel to
// `teamNames` (server: handlers_competition.go's GET
// /league-tiebreak/candidates builds both from the same loop over g.Teams,
// mirroring chusen's own teams array).
//
// Returns undefined unless `teams` is present, the same length as `names`,
// and EVERY team carries a non-empty id. teamIds is now REQUIRED by the
// server (operator ruling bc-pnum: the tied group is selected by id only,
// >= 2 entries, no blanks, no duplicates) -- undefined here does NOT mean
// "safe to omit and let the server fall back to names", it means the
// action cannot be performed at all for this group. The caller (the
// "Run tie-breaker" / "Remove unscored tie-breaker" buttons below) MUST
// disable itself and show a hint rather than send a request shaped to
// 400: a legacy id-less group is exactly the error state the competition
// Overview's missing-ids notice already reports, so the operator has
// already been told what to fix.
//
// Exported for vitest at __tests__/admin_pools.test.jsx.
function groupTeamIds(teams, names) {
  if (!teams || teams.length !== names.length) return undefined;
  const ids = teams.map(t => t && t.id);
  if (ids.some(id => !id)) return undefined;
  return ids;
}

// The place a tied group shares, in an operator's words: its best place.
function sharedPlaceLabel(group) {
  return `${rankOrdinal(group.minPosition)} place`;
}

// The line naming an individual tie a tie-breaker round left level: its pool
// and the places it shares, e.g. "Pool A, 1st to 3rd place". Exported for
// vitest at __tests__/admin_pools.test.jsx.
function circleGroupLine(group) {
  const n = (group.teams || group.teamNames || []).length;
  const last = group.minPosition + Math.max(n, 1) - 1;
  const places = last > group.minPosition
    ? `${rankOrdinal(group.minPosition)} to ${rankOrdinal(last)} place`
    : sharedPlaceLabel(group);
  return `${group.poolName}, ${places}`;
}

// The Accept shared ranks confirm (the wording the e2e fixture reads). `groups`
// are the tied groups being accepted, `unfought` the tie-breaker bouts not yet
// fought that accepting removes ("Team A v Team B"), `cyclic` whether a group
// already fought its tie-breaker round without separating the teams.
//
// Exported for vitest at __tests__/render/league_tiebreak_banner.render.test.jsx.
function acceptSharedRanksMessage(groups, unfought, cyclic) {
  const shares = (groups || []).map(g => `${(g.teamNames || []).join(", ")} share ${sharedPlaceLabel(g)}.`).join(" ");
  const parts = [`Accept shared ranks? ${shares} The competition finishes now. Undo shared ranks brings the question back.`];
  for (const bout of unfought || []) parts.push(`This also removes the tie-breaker not yet fought: ${bout}.`);
  if (cyclic) parts.push("Drawing lots is the other way to separate them.");
  return parts.join(" ");
}

// The Undo shared ranks confirm. `removedCourts` are shiaijo the tournament no
// longer has that some of the league's matches still name: the undo clears
// those courts, so the matches show as unassigned.
function undoSharedRanksMessage(removedCourts) {
  const base = "Undo the shared ranks? The league goes back to match play and the tie is asked again: run a tie-breaker or accept shared ranks.";
  if (!removedCourts || removedCourts.length === 0) return base;
  return `${base} Matches on shiaijo ${removedCourts.join(", ")}, which the tournament no longer has, become unassigned.`;
}

// "Shared ranks accepted: 1st place: Team A, Team B." for one accepted group.
function acceptedRankLine(group) {
  return `Shared ranks accepted: ${sharedPlaceLabel(group)}: ${(group.teamNames || []).join(", ")}.`;
}

function AdminPools({ c, pools, poolMatches, standings, tweaks, onEditScore, password }) {
  const isLeague = c && c.format === "league";
  // A KEY, re-resolved from the live poolMatches every render, never a captured
  // object. A match has ONE result and every surface asking for it shows the
  // same one (operator ruling); a snapshot shows the answer it was opened with
  // for as long as it stays open, so a result recorded on another device
  // updated the pool grid behind this modal but not the modal itself. Same
  // pattern as AdminScoreEditor, the bracket panel and the shiaijo panel.
  const [scoreOpenId, setScoreOpenId] = useState(null);
  const mountedRef = useRef(true);
  useEffect(() => () => { mountedRef.current = false; }, []);

  // Phase 3b (mp-8rc9): league tie-breaker candidate state.
  // Fetched for every team league whatever its status: once the shared ranks
  // are accepted the league is finished, and the Undo surface still has to
  // read the accepted groups from the same answer.
  const isTeamLeague = isLeague && (c.teamSize > 0 || c.kind === "team");
  const [tiebreakCandidates, setTiebreakCandidates] = useState(null);
  const [tiebreakFinalized, setTiebreakFinalized] = useState(false);
  // The ties the operator accepted as shared ranks (the candidates answer's
  // `accepted`, same shape as a candidate group). Empty for a legacy flag that
  // stored none: the Undo control still shows, naming no teams.
  const [tiebreakAccepted, setTiebreakAccepted] = useState([]);
  // The refusal to accept while a league match is unfinished (league_not_settled).
  // Its own state, shown outside the banner: the refetch it triggers answers an
  // unfinished table with no candidates, so the banner (and an error inside it)
  // goes away at once.
  const [tiebreakNotSettled, setTiebreakNotSettled] = useState(null);
  const [tiebreakActionBusy, setTiebreakActionBusy] = useState(false);
  // Per-button busy key: "<teamNamesKey>:<action>" or "finalize". Keeps the
  // spinner scoped to exactly the clicked button while all others stay
  // disabled (tiebreakActionBusy is still set on all). null = no action in
  // flight.
  const [tiebreakBusyAction, setTiebreakBusyAction] = useState(null);
  const [tiebreakErr, setTiebreakErr] = useState(null);

  // Chusen (drawing lots) candidate state: team-pool ties the daihyosen
  // could not settle (a cycle / all-drawn), and individual ties a finished
  // tie-breaker round left level (decision F5, shown by the circle notice
  // below). Fetched in the "pools" phase (non-league too: a mixed pool stage
  // can hold either), and for a mixed comp in its "knockout" phase as well: a pool correction
  // made then can leave a tie only a chusen settles, and a wrong chusen must
  // stay fixable. A finished league is asked too: its recorded lots stay
  // correctable, the table and podium follow, and it stays finished. The
  // server's AcceptsPoolRankOverride is the same rule.
  const isTeamComp = c && (c.kind === "team" || c.teamSize > 0);
  const chusenOpen = !!c && (c.status === "pools"
    || (c.format === "mixed" && c.status === "knockout")
    || (c.format === "league" && c.status === "complete"));
  const [chusenCandidates, setChusenCandidates] = useState(null);
  // Per-member input values: keys are "${groupKey}::${identity}" -> string,
  // where groupKey is "${poolName}::${minPosition}" and identity is
  // checkinPid(member) (id when non-empty, else "name|dojo") -- never
  // the member's index in the group, whose order is the server's standings
  // order and can change between fetches (bc-appx item 2), and never the bare display name, which two members
  // can share.
  const [chusenInputs, setChusenInputs] = useState({});
  // Per-group busy flag: keyed by groupKey "${poolName}::${minPosition}" -> bool
  // (a pool can hold more than one unresolved tied group).
  const [chusenBusy, setChusenBusy] = useState({});
  // Per-group error: keyed by the same "${poolName}::${minPosition}" -> string.
  const [chusenGroupErr, setChusenGroupErr] = useState({});
  // Ties a chusen already settled, each carrying its recorded `ranks`
  // (parallel to `teams`), so one recorded in the wrong order can be changed.
  // Fetched with chusenCandidates, so it shows exactly where they would.
  const [chusenRecorded, setChusenRecorded] = useState(null);
  // groupKey -> true while the operator is changing that recorded chusen.
  const [chusenChanging, setChusenChanging] = useState({});
  // A chusen order being typed holds a new build's automatic reload
  // (app_version.jsx), but only while it holds a change: a rank in a group
  // still shown (an unresolved tie, or a recorded one being changed) that is
  // not empty and not the rank the box shows by itself. A key left behind by
  // an erased rank, or by a group another device resolved, holds nothing.
  const chusenShown = [
    ...(chusenCandidates || []),
    ...(chusenRecorded || []).filter(g => chusenChanging[`${g.poolName}::${g.minPosition}`]),
  ];
  useUnsavedInput(chusenShown.some(g => (g.teams || []).some((m, idx) => {
    const raw = chusenInputs[`${g.poolName}::${g.minPosition}::${checkinPid(m)}`];
    const typed = raw === undefined ? "" : String(raw).trim();
    return typed !== "" && typed !== String(g.ranks ? g.ranks[idx] : g.minPosition + idx);
  })));
  const applyChusen = (res) => {
    setChusenCandidates(res ? res.candidates : null);
    setChusenRecorded(res ? res.recorded : null);
  };

  // Lightweight signature so the effect re-runs when match results change.
  // Memoized so typing into a chusen position input (local state) does not
  // re-scan every pool match on each render.
  const poolMatchesSig = useMemo(
    () => (poolMatches || []).map(m => `${m.id}:${m.status}:${typeof m.winner === "string" ? m.winner : (m.winner && m.winner.name) || ""}`).join("|"),
    [poolMatches]
  );
  // Override-sensitive signature: a chusen result is a rank override, which the
  // backend broadcasts as EventTournamentUpdated WITHOUT changing pool matches.
  // Capturing rank + isOverridden lets the fetch effect refresh candidates after
  // an override recorded elsewhere (another operator tab), not only after a bout.
  const standingsSig = useMemo(
    () => Object.keys(standings || {}).sort().map(pn =>
      (standings[pn] || []).map(s => `${(s.player && s.player.name) || ""}:${s.rank}:${s.isOverridden ? 1 : 0}`).join(",")
    ).join("|"),
    [standings]
  );

  const chusenCompId = c && c.id;
  const chusenCompStatus = c && c.status;
  // Asked for an individual competition too: an ippon-shobu round that went in
  // a circle is offered there (decision F5), with another round as the main
  // way on and the lots as the operator's other choice (circleNotice below).
  useEffect(() => {
    if (!chusenOpen || !window.API || typeof window.API.chusenCandidates !== "function") {
      applyChusen(null);
      return;
    }
    let cancelled = false;
    window.API.chusenCandidates(chusenCompId, password)
      .then(res => { if (!cancelled) applyChusen(res); })
      .catch(() => { if (!cancelled) applyChusen(null); });
    return () => { cancelled = true; };
  }, [chusenCompId, chusenCompStatus, chusenOpen, poolMatchesSig, standingsSig, password]);

  // An individual tie a finished tie-break round left level (a circle: A beat
  // B, B beat C, C beat A). runBusy is the run whose round is being added,
  // circleErr a refusal per tie (keyed like the chusen groups), and lotsOpen
  // the ties whose lots the operator chose to enter instead.
  const [runBusy, setRunBusy] = useState(null);
  const [circleErr, setCircleErr] = useState({});
  const [lotsOpen, setLotsOpen] = useState({});
  const handleRunTiebreakerRound = async (group, run) => {
    const groupKey = `${group.poolName}::${group.minPosition}`;
    const ids = run.map(m => m.id);
    setRunBusy(`${groupKey}::${ids.join(",")}`);
    setCircleErr(prev => ({ ...prev, [groupKey]: null }));
    try {
      await window.API.runTiebreakerRound(c.id, group.poolName, ids, password);
      // match_updated reloads the pool matches, and with them this notice,
      // which goes once the new round is under way.
    } catch (e) {
      if (mountedRef.current) setCircleErr(prev => ({ ...prev, [groupKey]: (e && e.message) || "Failed to add another tie-breaker round" }));
    } finally {
      if (mountedRef.current) setRunBusy(null);
    }
  };

  // Fetch candidates whenever poolMatches changes (triggered by match_updated
  // SSE events, which the Go handler now broadcasts for AwaitingLeagueTiebreak).
  // The answer, applied: what is still asked, whether the operator accepted
  // shared ranks, and which ties that covers.
  const applyTiebreak = (data) => {
    setTiebreakCandidates(data.candidates || []);
    setTiebreakFinalized(!!data.finalized);
    setTiebreakAccepted(data.accepted || []);
    setTiebreakErr(null);
  };
  const reloadTiebreak = () => window.API.leagueTiebreakCandidates(c.id).then(data => {
    if (mountedRef.current) applyTiebreak(data);
  }).catch(e => {
    if (mountedRef.current) setTiebreakErr(e.message);
  });
  // A result recorded changes what the table says, so a "not finished yet"
  // sentence about the table as it was no longer holds.
  useEffect(() => { setTiebreakNotSettled(null); }, [poolMatchesSig]);

  useEffect(() => {
    if (!isTeamLeague) return;
    let cancelled = false;
    window.API.leagueTiebreakCandidates(c.id)
      .then(data => {
        if (cancelled || !mountedRef.current) return;
        applyTiebreak(data);
      })
      .catch(e => {
        if (cancelled || !mountedRef.current) return;
        // Non-fatal: banner stays hidden on fetch error (operator can still
        // use the schedule page to score matches; the banner is advisory).
        setTiebreakErr(e.message);
      });
    return () => { cancelled = true; };
  }, [c.id, c.status, isTeamLeague, poolMatches]);

  const handleTiebreakGenerate = async (groupTeamNames, teamIds) => {
    const actionKey = groupTeamNames.join(",") + ":generate";
    setTiebreakActionBusy(true);
    setTiebreakBusyAction(actionKey);
    setTiebreakErr(null);
    try {
      await window.API.leagueTiebreakGenerate(c.id, groupTeamNames, password, teamIds);
      // SSE match_updated will reload poolMatches and re-fetch candidates.
    } catch (e) {
      if (mountedRef.current) setTiebreakErr(e.message || "Failed to generate tie-breaker matches");
    } finally {
      if (mountedRef.current) { setTiebreakActionBusy(false); setTiebreakBusyAction(null); }
    }
  };

  const handleTiebreakRemove = async (groupTeamNames, teamIds) => {
    if (!(await window.confirmDialog({ message: `Remove unscored tie-breaker matches for ${groupTeamNames.join(", ")}?`, confirmLabel: "Remove", danger: true }))) return;
    const actionKey = groupTeamNames.join(",") + ":remove";
    setTiebreakActionBusy(true);
    setTiebreakBusyAction(actionKey);
    setTiebreakErr(null);
    try {
      await window.API.leagueTiebreakRemove(c.id, groupTeamNames, password, teamIds);
    } catch (e) {
      if (mountedRef.current) setTiebreakErr(e.message || "Failed to remove tie-breaker matches");
    } finally {
      if (mountedRef.current) { setTiebreakActionBusy(false); setTiebreakBusyAction(null); }
    }
  };

  // Accept shared ranks: the confirm names every tie-breaker bout not yet
  // fought that accepting removes (the operator declined it by accepting), says
  // the competition finishes now and that Undo brings the question back, and
  // for a fought round that went in a circle, that lots are the other way.
  const handleTiebreakFinalize = async () => {
    const groups = tiebreakCandidates || [];
    const unfought = [];
    let cyclic = false;
    for (const g of groups) {
      const names = g.teamNames || [];
      const dhRows = dhRowsOfGroup(groupTeamIds(g.teams, names));
      const open = dhRows.filter(m => !dhRowsScored([m]));
      for (const m of open) unfought.push(`${nameOf(m.sideA)} v ${nameOf(m.sideB)}`);
      if (dhRows.length > 0 && open.length === 0 && dhPairsComplete(dhRows, names.length)) cyclic = true;
    }
    if (!(await window.confirmDialog({ message: acceptSharedRanksMessage(groups, unfought, cyclic), confirmLabel: "Accept shared ranks", danger: false }))) return;
    setTiebreakActionBusy(true);
    setTiebreakBusyAction("finalize");
    setTiebreakErr(null);
    setTiebreakNotSettled(null);
    try {
      await window.API.leagueTiebreakFinalize(c.id, password);
      if (mountedRef.current) setTiebreakFinalized(true);
      // The accepted groups (and the Undo control) come from the server's answer.
      await reloadTiebreak();
    } catch (e) {
      if (!mountedRef.current) return;
      if (e.code === "league_not_settled") {
        // A match was put back to play since this banner was drawn: say so, and
        // read the table again so the banner follows what the server holds.
        setTiebreakNotSettled(e.detail || e.message);
        await reloadTiebreak();
      } else {
        setTiebreakErr(e.message || "Failed to accept the shared ranks");
      }
    } finally {
      if (mountedRef.current) { setTiebreakActionBusy(false); setTiebreakBusyAction(null); }
    }
  };

  // Undo shared ranks: the league goes back to match play and the tie is asked
  // again. The confirm names any shiaijo the tournament no longer has, whose
  // matches become unassigned (the server clears those courts in the same
  // write). The venue is read for that sentence only; a read that fails leaves
  // it out and the Undo still goes ahead on the operator's say.
  const handleTiebreakUndo = async () => {
    let gone = [];
    try {
      const t = await window.API.fetchTournament();
      const venue = (t && Array.isArray(t.courts)) ? t.courts : [];
      if (venue.length > 0) {
        gone = [...new Set((poolMatches || []).map(m => m.court).filter(ct => ct && !venue.includes(ct)))];
      }
    } catch (_err) { gone = []; }
    if (!(await window.confirmDialog({ message: undoSharedRanksMessage(gone), confirmLabel: "Undo shared ranks", danger: false }))) return;
    setTiebreakActionBusy(true);
    setTiebreakBusyAction("undo");
    setTiebreakErr(null);
    try {
      await window.API.leagueTiebreakUndo(c.id, password);
      if (mountedRef.current) setTiebreakFinalized(false);
      await reloadTiebreak();
    } catch (e) {
      if (mountedRef.current) setTiebreakErr(e.message || "Failed to undo the shared ranks");
    } finally {
      if (mountedRef.current) { setTiebreakActionBusy(false); setTiebreakBusyAction(null); }
    }
  };

  // The group's daihyosen rows: every "-DH-" row whose two sides are both
  // members of the group, judged by team id only. Undefined ids (a legacy
  // id-less group) hold nothing, and the buttons are disabled for it anyway.
  const dhRowsOfGroup = (teamIds) => {
    const idSet = new Set(teamIds || []);
    return (poolMatches || []).filter(m =>
      m.id && /(-DH-)/.test(m.id) && idSet.has(m.sideAId) && idSet.has(m.sideBId));
  };

  // Does the group hold a daihyosen bout for EVERY pair of its teams? The
  // server keeps a team league open until it does (pendingTieBreaks), and
  // refuses "Run tie-breaker" only when every pair is held, so a correction
  // that grows a tie leaves a partial set: "Run tie-breaker" then adds the
  // missing pairs, and only a complete set shows "Remove".
  const dhPairsComplete = (dhRows, groupSize) => {
    const pairs = new Set(dhRows.map(m => [m.sideAId, m.sideBId].sort().join("|")));
    return pairs.size >= (groupSize * (groupSize - 1)) / 2;
  };

  // Is any of those daihyosen bouts running or already scored? The DELETE
  // endpoint returns 409 in that state, so the Remove button must be disabled.
  const dhRowsScored = (dhRows) =>
    dhRows.some(m => m.status === "running" || m.status === "completed" || !!m.winner);

  // chusenEntry renders one tie's position entry: the team inputs, their
  // validation and the overridePoolRanks write. The pending banner uses it for
  // a first chusen; the recorded panel reuses it (changing=true) to change a
  // chusen recorded in the wrong order, so both go through the same path and
  // the same knockout warning.
  const chusenEntry = (group, changing = false) => {
    const { poolName, teamNames, minPosition } = group;
    // Members keyed by IDENTITY (checkinPid), never by name
    // (bc-cse follow-up) and never by index (bc-appx item 2): `teams`
    // carries the authoritative per-member identity ({id, name, dojo}),
    // positionally parallel to the legacy `teamNames` strings (server:
    // handlers_competition.go builds teams[i] and names[i] from the same
    // loop over g.Teams). Two members CAN share a display name --
    // reachable only via the documented enforcement hole in team-name
    // uniqueness (an unreadable config.md write skips
    // checkNewTeamNameCollisions) -- so keying anything by bare name
    // collapses both onto one identity. `teams` is required on the wire
    // (this SPA ships in the same binary as the server that emits it,
    // so there is no older-server case to be compatible with); a
    // teamNames-only fallback would silently collapse a same-name pair
    // back onto one key, exactly the bug this comment used to guard
    // against, so there is deliberately no fallback here. A payload
    // without it is a server bug, and skipping the group keeps that
    // contained to this banner rather than tripping the page-level
    // error boundary for every other pool on the screen.
    const members = group.teams;
    if (!members) return null;
    // A pool can hold more than one unresolved tied group (e.g. a cycle at
    // 1st/2nd and a separate cycle at 3rd/4th). Key by pool + best position
    // so the React key and the busy/error maps never collide across groups.
    const groupKey = `${poolName}::${minPosition}`;
    const isBusy = !!chusenBusy[groupKey];
    const groupErrMsg = chusenGroupErr[groupKey] || null;
    // overridePoolRanks requires playerId (operator ruling bc-pnum: the
    // server resolves a pool member by id only and 400s outright
    // without one). Disabling the button here, with NoIdHint's remedy,
    // replaces letting the operator click through to that 400. Mirrors
    // the league-tiebreak buttons' own idsMissing gate below.
    const idsMissing = members.some(m => !m.id);

    // Effective value for a member's input, keyed by the member's
    // IDENTITY (checkinPid; data.jsx owns id-vs-name|dojo fallback rule),
    // never its position in `members`: the group order comes from the
    // server's standings sort, which can change between fetches (any
    // write to the pool), so an index-keyed lookup can read back a
    // DIFFERENT team's typed value on retry. The operator's edit if present, else the
    // displayed default (minPosition + index --
    // idx is still used here only to pick a distinct default rank per
    // position, not to key the input). Both validation and submit read
    // this so accepting the shown defaults (already a valid permutation)
    // records without forcing a manual edit. Keyed on groupKey (pool +
    // minPosition) as well as identity, not bare poolName: a pool can hold
    // more than one unresolved tied group (see the groupKey comment
    // above), and PoolWinners has no upper bound, so e.g. a cycle at
    // 1st/2nd and another at 3rd/4th in the SAME pool must not share one
    // input/clear per member.
    // A recorded chusen being changed starts from the ranks it was
    // recorded with (group.ranks, parallel to members), so Change opens
    // on the order the operator is correcting.
    const defaultRank = (idx) => (group.ranks ? group.ranks[idx] : minPosition + idx);
    const effRank = (member, idx) => {
      const raw = chusenInputs[`${groupKey}::${checkinPid(member)}`];
      return parseInt(raw !== undefined ? raw : String(defaultRank(idx)), 10);
    };
    const clearInputs = () => setChusenInputs(prev => {
      const next = { ...prev };
      for (let i = 0; i < members.length; i++) delete next[`${groupKey}::${checkinPid(members[i])}`];
      return next;
    });

    const handleRecord = async () => {
      // Validate: entered positions must be exactly the set
      // {minPosition .. minPosition + members.length - 1}.
      const expected = new Set();
      for (let i = 0; i < members.length; i++) expected.add(minPosition + i);
      const entered = new Set();
      let valid = true;
      for (let i = 0; i < members.length; i++) {
        const val = effRank(members[i], i);
        if (isNaN(val) || !expected.has(val) || entered.has(val)) { valid = false; break; }
        entered.add(val);
      }
      if (!valid) {
        const lo = minPosition;
        const hi = minPosition + members.length - 1;
        setChusenGroupErr(prev => ({ ...prev, [groupKey]: `Enter each of positions ${lo} to ${hi} exactly once` }));
        return;
      }
      setChusenGroupErr(prev => ({ ...prev, [groupKey]: null }));
      setChusenBusy(prev => ({ ...prev, [groupKey]: true }));
      // A refusal is told on this group, and the server's order read again:
      // the order is the outbox's (bc-evts stage 3 review 11, Y7), answered
      // at once and shown as entered, so a refusal arrives later, through
      // onRefusal (claimed: the outbox raises no second alert for it), or
      // thrown by a door that still answers directly.
      const refused = (e) => {
        const msg = e && e.downstreamKnockoutPlayedCancelled
          ? DOWNSTREAM_KNOCKOUT_RANKING_CANCELLED
          : ((e && e.message) || "Failed to record chusen result");
        setChusenGroupErr(prev => ({ ...prev, [groupKey]: msg }));
        // A refusal records none of the group, and a lost response may have
        // recorded all of it. Re-fetch the candidates so the panel shows the
        // order the server holds rather than waiting for the next SSE-driven
        // refresh; the typed ranks are keyed by team, so they survive a
        // reorder.
        if (window.API && typeof window.API.chusenCandidates === "function") {
          window.API.chusenCandidates(c.id, password)
            .then(applyChusen)
            .catch(() => {});
        }
        return true;
      };
      try {
        // The whole order is ONE write, so the server answers for the order
        // entered and nothing else: sent one rank at a time, it passed
        // through orders nobody chose, and a knockout match whose place the
        // final order keeps was named and reopened. An order that moves a
        // qualifier out of a knockout match the old one already fought is
        // refused with the names of who moves; attemptScoreWrite asks the
        // operator and, confirmed, resends it with forceDownstreamReopen,
        // which reopens that match.
        await attemptScoreWrite({
          recordScore: (cId, pool, body, pwd, _match, o) => window.API.overridePoolRanks(cId, pool, body.ranks, pwd, !!body.forceDownstreamReopen, o),
          confirmDialog: window.confirmDialog,
          compId: c.id, matchId: poolName,
          result: { ranks: members.map((m, i) => ({ playerId: m.id, rank: effRank(m, i) })) },
          password,
          onRefusal: refused,
        });
        if (changing) {
          // Show the new order at once and close the entry; the re-fetch
          // the new standings trigger reconciles it.
          const ranks = members.map((m, i) => effRank(m, i));
          setChusenRecorded(prev => (prev || []).map(g => (g.poolName === poolName && g.minPosition === minPosition ? { ...g, ranks } : g)));
          setChusenChanging(prev => ({ ...prev, [groupKey]: false }));
        } else {
          // Optimistically hide THIS group only (a pool can hold several) - the
          // effect re-fetches on the next update to reconcile.
          setChusenCandidates(prev => (prev || []).filter(g => !(g.poolName === poolName && g.minPosition === minPosition)));
        }
        // Clear inputs for this group only (groupKey + identity, not bare
        // poolName -- see the effRank comment: a sibling tied group in the
        // same pool must not have its inputs wiped here too).
        clearInputs();
      } catch (e) {
        refused(e);
      } finally {
        setChusenBusy(prev => ({ ...prev, [groupKey]: false }));
      }
    };

    return (
      <div key={groupKey} className="league-tiebreak__group">
        <div className="league-tiebreak__group-header">
          <span className="league-tiebreak__pos">{poolName}</span>
          <span className="league-tiebreak__teams">{teamNames.join(" · ")}</span>
        </div>
        {changing && (
          <div className="league-tiebreak__desc" style={{ marginBottom: 8 }}>
            {isLeague
              ? (c.status === "complete"
                ? "Enter the order the lots actually gave. The final table and the podium change at once; the league stays finished and no match is reopened."
                : "Enter the order the lots actually gave. The league table changes at once.")
              : `Enter the order the lots actually gave. Changing it can change who qualifies from ${poolName}. If ${isTeamComp ? "a team" : "a competitor"} moves out of a knockout match that has already been fought, you are asked before that match is reopened.`}
          </div>
        )}
        <div className="league-tiebreak__desc" style={{ marginBottom: 8 }}>
          Assign positions {minPosition} to {minPosition + members.length - 1} (one per {isTeamComp ? "team" : "competitor"}):
        </div>
        {members.map((member, idx) => {
          // groupKey + member IDENTITY, not index: see the effRank/
          // checkinPid comments above -- the member array order is
          // not stable across a re-fetch, so an index-keyed input can
          // silently attach to the WRONG team after a failed write's
          // re-fetch.
          const memberKey = checkinPid(member);
          const inputKey = `${groupKey}::${memberKey}`;
          const defaultVal = defaultRank(idx);
          // Stable DOM id so the label is programmatically tied to its
          // input. `idx` here, NOT memberKey: memberKey can be a
          // non-ASCII name|dojo string (Japanese names are the normal
          // case for this roster), and the regex below collapses every
          // non-ASCII run to a single "-", so two id-less members whose
          // keys differ only in non-ASCII characters collided on the
          // SAME DOM id (duplicate ids, and the label's htmlFor focused
          // the other team's input). idx is unique within this group's
          // render (label and input come from the same map iteration),
          // which is all a DOM id needs -- unlike inputKey/memberKey
          // above, it does not need to survive a re-fetch reorder.
          const inputId = `chusen-${groupKey}-${idx}`.replace(/[^a-zA-Z0-9_-]+/g, "-");
          return (
            <div key={inputKey} style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 4 }}>
              <label htmlFor={inputId} style={{ flex: 1 }}>{member.name}</label>
              <input
                id={inputId}
                type="number"
                min={minPosition}
                max={minPosition + members.length - 1}
                style={{ width: 64 }}
                value={chusenInputs[inputKey] !== undefined ? chusenInputs[inputKey] : String(defaultVal)}
                onChange={e => setChusenInputs(prev => ({ ...prev, [inputKey]: e.target.value }))}
                disabled={isBusy}
              />
            </div>
          );
        })}
        <div className="league-tiebreak__actions" style={{ marginTop: 8 }}>
          <button
            type="button"
            className="btn btn--sm btn--primary"
            disabled={isBusy || idsMissing}
            onClick={handleRecord}
          >
            {isBusy && <span className="spinner" />}
            Record chusen result
          </button>
          {changing && (
            <button
              type="button"
              className="btn btn--sm btn--ghost"
              disabled={isBusy}
              onClick={() => {
                clearInputs();
                setChusenGroupErr(prev => ({ ...prev, [groupKey]: null }));
                setChusenChanging(prev => ({ ...prev, [groupKey]: false }));
              }}
            >
              Cancel
            </button>
          )}
          {idsMissing && (
            <NoIdHint text={NO_ID_POOL_HINT} />
          )}
        </div>
        {groupErrMsg && (
          <div className="league-tiebreak__err">{groupErrMsg}</div>
        )}
      </div>
    );
  };

  // Chusen banner: shown when chusenCandidates is non-empty (team comp whose
  // pool order is still open, chusenOpen above; at least one DH cycle left
  // unresolved).
  const chusenBanner = isTeamComp && chusenCandidates && chusenCandidates.length > 0 ? (
    <div
      className="alert alert--warn league-tiebreak"
      role="status"
      aria-live="polite"
    >
      <div className="league-tiebreak__title">Chusen (drawing lots) required</div>
      <div className="league-tiebreak__desc">
        The daihyosen didn&apos;t settle the order (two or more teams tied on daihyosen wins). Draw lots and record each team&apos;s finishing position below.
      </div>
      {chusenCandidates.map(group => chusenEntry(group))}
    </div>
  ) : null;

  // Individual circle notice (decision F5): a tie-breaker round left a tie
  // level (A beat B, B beat C, C beat A), so the pool's order is still open.
  // The main way on is another round among the ones still level
  // (group.stillLevel, each run one round); drawing lots is the operator's
  // other choice, and opens the same entry the team chusen uses.
  const circleNotice = !isTeamComp && chusenCandidates && chusenCandidates.length > 0 ? (
    <div
      className="alert alert--warn league-tiebreak"
      role="status"
      aria-live="polite"
    >
      <div className="league-tiebreak__title">A tie-breaker round left a tie</div>
      <div className="league-tiebreak__desc">
        Each won as many tie-breaker bouts as the others, so the round did not decide the order. Run another tie-breaker round among them, or draw lots instead.
      </div>
      {chusenCandidates.map(group => {
        const groupKey = `${group.poolName}::${group.minPosition}`;
        const runs = group.stillLevel || [];
        const errMsg = circleErr[groupKey] || null;
        return (
          <div key={groupKey} className="league-tiebreak__group">
            <div className="league-tiebreak__group-header">
              <span className="league-tiebreak__pos">{circleGroupLine(group)}</span>
              <span className="league-tiebreak__teams">{(group.teamNames || []).join(" · ")}</span>
            </div>
            <div className="league-tiebreak__actions">
              {runs.map(run => {
                const ids = run.map(m => m.id);
                const busyKey = `${groupKey}::${ids.join(",")}`;
                const label = runs.length > 1
                  ? `Run another tie-breaker round: ${run.map(m => m.name).join(" · ")}`
                  : "Run another tie-breaker round";
                return (
                  <button
                    key={busyKey}
                    type="button"
                    className="btn btn--sm btn--primary"
                    disabled={runBusy !== null || ids.some(id => !id)}
                    onClick={() => handleRunTiebreakerRound(group, run)}
                  >
                    {runBusy === busyKey && <span className="spinner" />}
                    {label}
                  </button>
                );
              })}
              {!lotsOpen[groupKey] && (
                <button
                  type="button"
                  className="btn btn--sm btn--ghost"
                  disabled={runBusy !== null}
                  onClick={() => setLotsOpen(prev => ({ ...prev, [groupKey]: true }))}
                >
                  Draw lots instead
                </button>
              )}
            </div>
            {errMsg && <div className="league-tiebreak__err">{errMsg}</div>}
            {lotsOpen[groupKey] && chusenEntry(group)}
          </div>
        );
      })}
    </div>
  ) : null;

  // Recorded chusen: each tie a chusen already settled, shown in the recorded
  // order with a Change control, because a chusen recorded in the wrong order
  // must stay fixable. Only these ties are editable: the general rank override
  // was removed on purpose. Change reopens chusenEntry pre-filled with the
  // recorded ranks.
  const chusenRecordedPanel = chusenRecorded && chusenRecorded.length > 0 ? (
    <div className="card league-tiebreak">
      <div className="league-tiebreak__title">Chusen (drawing lots) recorded</div>
      {chusenRecorded.map((group) => {
        const groupKey = `${group.poolName}::${group.minPosition}`;
        if (!group.teams || !group.ranks) return null;
        if (chusenChanging[groupKey]) return chusenEntry(group, true);
        const order = group.teams
          .map((t, i) => ({ name: t.name, rank: group.ranks[i] }))
          .sort((a, b) => a.rank - b.rank)
          .map(o => `${rankOrdinal(o.rank)} ${o.name}`)
          .join(", ");
        return (
          <div key={groupKey} className="league-tiebreak__group">
            <div className="league-tiebreak__group-header">
              <span className="league-tiebreak__pos">{group.poolName}</span>
              <span className="league-tiebreak__teams">Drawing lots: {order}</span>
            </div>
            <div className="league-tiebreak__actions">
              <button
                type="button"
                className="btn btn--sm"
                aria-label={`Change the drawing lots for ${group.poolName}`}
                onClick={() => setChusenChanging(prev => ({ ...prev, [groupKey]: true }))}
              >
                Change
              </button>
            </div>
          </div>
        );
      })}
    </div>
  ) : null;

  // The accepted shared ranks, shown whatever the league's status: a finished
  // league has no banner, and this is where the operator takes the acceptance
  // back. One line per accepted tie; a legacy flag that stored none still
  // offers Undo.
  const acceptedRanksPanel = isTeamLeague && tiebreakFinalized ? (
    <div className="alert alert--info league-tiebreak" role="status" aria-live="polite" data-testid="league-shared-ranks">
      {tiebreakAccepted.length === 0 && (
        <div className="league-tiebreak__desc">Shared ranks accepted.</div>
      )}
      {tiebreakAccepted.map((g) => (
        <div key={`${g.minPosition}:${(g.teamNames || []).join(",")}`} className="league-tiebreak__desc">{acceptedRankLine(g)}</div>
      ))}
      <div className="league-tiebreak__actions">
        <button
          type="button"
          className="btn btn--sm btn--ghost"
          disabled={tiebreakActionBusy}
          onClick={handleTiebreakUndo}
        >
          {tiebreakBusyAction === "undo" && <span className="spinner" />}
          Undo shared ranks
        </button>
      </div>
      {tiebreakErr && (
        <div className="league-tiebreak__err">{tiebreakErr}</div>
      )}
    </div>
  ) : null;

  // The sentence for an Accept refused because a league match is unfinished.
  const notSettledNotice = isTeamLeague && tiebreakNotSettled ? (
    <div className="alert alert--warn league-tiebreak" role="alert" data-testid="league-not-settled">
      {tiebreakNotSettled}
    </div>
  ) : null;

  // Banner element: shown when there are consequential tied groups with no
  // tie-breaker matches yet, OR when tie-breaker matches have been generated.
  const leagueTiebreakBanner = isTeamLeague && c.status === "pools" && tiebreakCandidates && tiebreakCandidates.length > 0 ? (
    <div
      className="alert alert--warn league-tiebreak"
      role="status"
      aria-live="polite"
    >
      <div className="league-tiebreak__title">Tie-breaker required</div>
      <div className="league-tiebreak__desc">
        All regular matches are finished, and the teams below are level on every criterion for a place that counts. Run a tie-breaker to separate them, or accept shared ranks, which finishes the competition now.
      </div>
      {tiebreakCandidates.map((group) => {
        const names = group.teamNames || [];
        const teamIds = groupTeamIds(group.teams, names);
        // teamIds is REQUIRED the same way (operator ruling bc-pnum): see
        // the chusen idsMissing comment above for why an id-less team
        // disables the button instead of letting the write 400.
        const idsMissing = !teamIds;
        const dhRows = dhRowsOfGroup(teamIds);
        const dhComplete = dhPairsComplete(dhRows, names.length);
        const dhScored = dhRowsScored(dhRows);
        const posLabel = group.minPosition === group.maxPosition
          ? `Position ${group.minPosition}`
          : `Positions ${group.minPosition}–${group.maxPosition}`;
        const generateKey = names.join(",") + ":generate";
        const removeKey = names.join(",") + ":remove";
        return (
          <div key={names.join(",")} className="league-tiebreak__group">
            <div className="league-tiebreak__group-header">
              <span className="league-tiebreak__pos">{posLabel}</span>
              <span className="league-tiebreak__teams">{names.join(" · ")}</span>
            </div>
            <div className="league-tiebreak__actions">
              {!dhComplete ? (
                <>
                  <button
                    type="button"
                    className="btn btn--sm btn--primary"
                    disabled={tiebreakActionBusy || idsMissing}
                    onClick={() => handleTiebreakGenerate(names, teamIds)}
                  >
                    {tiebreakBusyAction === generateKey && <span className="spinner" />}
                    Run tie-breaker
                  </button>
                  {idsMissing && (
                    <NoIdHint text={NO_ID_POOL_HINT} />
                  )}
                </>
              ) : (
                <>
                  <button
                    type="button"
                    className="btn btn--sm btn--danger btn--ghost"
                    disabled={tiebreakActionBusy || dhScored || idsMissing}
                    onClick={() => handleTiebreakRemove(names, teamIds)}
                  >
                    {tiebreakBusyAction === removeKey && <span className="spinner" />}
                    Remove unscored tie-breaker
                  </button>
                  {idsMissing && (
                    <NoIdHint text={NO_ID_POOL_HINT} />
                  )}
                  {dhScored && !dhRows.some(m => m.status !== "completed") && (
                    <span className="field__hint">Every tie-breaker bout is finished and these teams are still level. Accept shared ranks, or draw lots.</span>
                  )}
                  {dhScored && dhRows.some(m => m.status !== "completed") && (
                    <span className="field__hint">Tie-breaker is running or already scored: score it to continue.</span>
                  )}
                </>
              )}
            </div>
          </div>
        );
      })}
      {!tiebreakFinalized && (
        <div className="league-tiebreak__finalize">
          <button
            type="button"
            className="btn btn--sm btn--ghost"
            disabled={tiebreakActionBusy}
            onClick={handleTiebreakFinalize}
          >
            {tiebreakBusyAction === "finalize" && <span className="spinner" />}
            Accept shared ranks
          </button>
          <div className="field__hint" style={{ marginTop: 4 }}>The tied teams share the place with no tie-breaker, and the competition finishes now. Undo shared ranks brings the question back.</div>
        </div>
      )}
      {tiebreakErr && (
        <div className="league-tiebreak__err">{tiebreakErr}</div>
      )}
    </div>
  ) : null;

  // Resolved live, then enriched: enrichPoolMatchWithComp derives rep-player
  // rosters from `c`, so it must run on the CURRENT row, not on the row as it
  // looked when the operator clicked it.
  // Memoized because enrichPoolMatchWithComp calls buildPlayerMap, which walks
  // the whole roster three times and allocates an entry per participant. Without
  // this it reruns on every render while the modal is open, i.e. on every SSE
  // broadcast for ANY match in the tournament, and throws the map away each
  // time. Nothing downstream depends on the object identity changing per render.
  const scoreOpenRaw = scoreOpenId ? (poolMatches || []).find((m) => m.id === scoreOpenId) || null : null;
  const scoreOpenMatch = useMemo(
    () => (scoreOpenRaw ? enrichPoolMatchWithComp(scoreOpenRaw, c) : null),
    [scoreOpenRaw, c]
  );

  // Modal rendered alongside the PoolsViewer.
  const scoreModal = scoreOpenMatch ? (
    <ScoreEditorModal
      key={c.id + '-' + scoreOpenMatch.id}
      match={scoreOpenMatch}
      prevMatch={null}
      nextMatch={null}
      onPrev={null}
      onNext={null}
      onClose={() => setScoreOpenId(null)}
      allowCourtForce
      onSubmit={async (patch, opts) => {
        try {
          // opts: the editor's own claim on a refusal (onRefusal).
          const res = await onEditScore(c.id, scoreOpenMatch.id, patch, scoreOpenMatch, { ...opts, allowCourtForce: true });
          // What the write came back with: a refused one threw and returns
          // nothing, which is how the editor tells them apart.
          if (mountedRef.current && !writeKeepsEditorOpen(patch, res)) setScoreOpenId(null);
          return res;
        } catch (_err) { /* keep modal open on error */ }
      }}
      onSubmitAndNext={null}
      password={password}
    />
  ) : null;

  if (!pools || pools.length === 0) {
    return <EmptyState icon="⏳" title={isLeague ? "League not drawn yet" : "Pools not drawn yet"} message={`Add participants and start the competition to ${isLeague ? "draw the league table" : "draw pools"}.`} />;
  }

  const PoolsViewer = window.PoolsViewer;
  const LeagueStandingsViewer = window.LeagueStandingsViewer;
  return (
    <>
    <div>
      {drawnSupplementaryBouts(poolMatches, standings).map(m => (
        <DrawnTieBreakNotice
          key={m.id}
          match={enrichPoolMatchWithComp(m, c)}
          comp={c}
          password={password}
          onOpen={() => setScoreOpenId(m.id)}
        />
      ))}
      {chusenBanner}
      {circleNotice}
      {chusenRecordedPanel}
      {notSettledNotice}
      {leagueTiebreakBanner}
      {acceptedRanksPanel}
      {/* Standings ordering is decided by FORMAT (mp-ahu6): pools always
          draw-order via PoolsViewer, leagues always rank-order via
          LeagueStandingsViewer. Mirrored in viewer_competition.jsx and
          admin_shiaijo.jsx's ShiaijoContext. */}
      {isLeague ? (
        LeagueStandingsViewer ? (
          // showDataIssues on both branches: this is an operator surface, and
          // the same components render for spectators from viewer_competition
          // WITHOUT it. That prop is the whole audience gate, because the flags
          // it reveals travel on the public payload the admin console reads.
          <LeagueStandingsViewer
            competition={c}
            poolMatches={poolMatches}
            tweaks={tweaks}
            onMatchClick={(m) => setScoreOpenId(m.id)}
            highlightPlayers={[]}
            showDataIssues
          />
        ) : null
      ) : (
        PoolsViewer ? (
          <PoolsViewer
            pools={pools}
            standings={standings}
            poolMatches={poolMatches}
            competition={c}
            tweaks={tweaks}
            onMatchClick={(m) => setScoreOpenId(m.id)}
            highlightPlayers={[]}
            showDataIssues
          />
        ) : null
      )}
    </div>
    {scoreModal}
    </>
  );
}

window.AdminPools = AdminPools;

// Expose enrichPoolMatchWithComp and isSupplementaryBout as window globals so
// window-only modules (admin_shiaijo.jsx) can call them at render time without
// ESM imports (which would double-eval those script-tagged modules). This is a
// browser-only admin module (its components read window.* at render time); the
// typeof guard exists only so the vitest suite can ES-import the pure helpers
// above without the top-level assignment throwing when window is absent.
if (typeof window !== "undefined") {
  window.enrichPoolMatchWithComp = enrichPoolMatchWithComp;
  window.isSupplementaryBout = isSupplementaryBout;
  window.isPoolDaihyosenBout = isPoolDaihyosenBout;
}

// ES export for the vitest suite: pure helpers only. The component
// stays behind window.* to match the rest of admin_*.jsx.
export { enrichPoolMatchWithComp, poolMatchesForPool, groupTeamIds, drawnSupplementaryBouts, acceptSharedRanksMessage, undoSharedRanksMessage, acceptedRankLine, circleGroupLine };
