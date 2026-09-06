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

const { useState: useStateA, useEffect: useEffectA, useRef: useRefA, useMemo: useMemoA } = React;
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
  const toPlayer = (side) => {
    if (side && typeof side === "object") return side;
    if (!side) return { id: "", name: "" };
    const p = playerMap[side];
    return p || { id: side, name: side };
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
    sideA: toPlayer(m.sideA),
    sideB: toPlayer(m.sideB),
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

// Identity keying for the chusen (drawing-lots) rank inputs below: each rank
// input is keyed by the member's IDENTITY (checkinPid, imported above; id
// when non-empty, else "name|dojo" -- the same rule helper.CompetitorKey
// applies server-side), never by that member's position in the `members`
// array (bc-appx item 2). The chusen-candidates payload's `teams` array is
// "the still-tied members in current standings order"
// (engine.ChusenCandidates), and standings order depends on which members
// already carry a rank override: a member with ANY recorded override sorts
// ahead of one without, regardless of the override's actual value
// (engine/scoring.go), so completing 2 of 3 sequential writes and then
// re-fetching after a mid-loop failure can return the SAME group with its
// members in a DIFFERENT order (reproduced: [Alpha,Beta,Gamma] ->
// [Beta,Alpha,Gamma] after two of three writes). An index-keyed input map
// then reads the operator's typed value for the WRONG team on retry,
// silently inverting the recorded draw.
//
// checkinPid's own fallback previously had a gap that mattered here
// (`p.id ?? fallback` only fell back on a null/undefined id, but the
// chusen-candidates handler always emits an "id" key -- handlers_competition.go:
// `gin.H{"id": t.Player.ID, ...}` -- which is "" for a competitor with no
// UUID, not null/undefined, so every legacy member in a group collapsed onto
// the SAME empty-string key: duplicate React keys, duplicate DOM ids, and
// typing into one row's input moved all of them). That gap is now closed in
// checkinPid itself (id-else-fallback via a truthiness check, not `??`),
// so this file delegates to it rather than keeping a second, drift-prone
// copy of the same rule.
//
// No positional tie-break is needed for the reachable collisions: two
// members with the same name AND dojo are a duplicate roster row the
// write-floor save already refuses (state.ErrDuplicateName /
// CheckDuplicateEntriesByNameDojo), so that pairing cannot reach this
// screen; the pairing that CAN -- same name, different dojo, via the
// documented team-name-uniqueness enforcement hole -- is already separated
// by the dojo half of the key (checkinPid's fallback).

// groupTeamIds derives the teamIds array to send alongside teamNames on a
// league-tiebreak generate/remove request (second-Opus-pass nit 7): the
// candidates payload's `teams` array carries {id,name,dojo} per team,
// positionally parallel to `teamNames` (server: handlers_competition.go's
// GET /league-tiebreak/candidates builds both from the same loop over
// g.Teams, mirroring chusen's own teams array).
//
// Returns undefined -- teamIds omitted entirely -- unless `teams` is
// present, the same length as `names`, and EVERY team carries a non-empty
// id: a legacy id-less group (or a group predating this field) must not
// send a teamIds array at all, since the server now rejects a blank entry
// outright (second-Opus-pass item 4) rather than treat it as "no id
// available".
//
// Exported for vitest at __tests__/admin_pools.test.jsx.
function groupTeamIds(teams, names) {
  if (!teams || teams.length !== names.length) return undefined;
  const ids = teams.map(t => t && t.id);
  if (ids.some(id => !id)) return undefined;
  return ids;
}

function AdminPools({ c, pools, poolMatches, standings, tweaks, onEditScore, password }) {
  const isLeague = c && c.format === "league";
  // A KEY, re-resolved from the live poolMatches every render, never a captured
  // object. A match has ONE result and every surface asking for it shows the
  // same one (operator ruling); a snapshot shows the answer it was opened with
  // for as long as it stays open, so a result recorded on another device
  // updated the pool grid behind this modal but not the modal itself. Same
  // pattern as AdminScoreEditor, the bracket panel and the shiaijo panel.
  const [scoreOpenId, setScoreOpenId] = useStateA(null);
  const mountedRef = useRefA(true);
  useEffectA(() => () => { mountedRef.current = false; }, []);

  // Phase 3b (mp-8rc9): league tie-breaker candidate state.
  // Only fetched for team leagues in the "pools" phase.
  const isTeamLeague = isLeague && (c.teamSize > 0 || c.kind === "team");
  const [tiebreakCandidates, setTiebreakCandidates] = useStateA(null);
  const [tiebreakFinalized, setTiebreakFinalized] = useStateA(false);
  const [tiebreakActionBusy, setTiebreakActionBusy] = useStateA(false);
  // Per-button busy key: "<teamNamesKey>:<action>" or "finalize". Keeps the
  // spinner scoped to exactly the clicked button while all others stay
  // disabled (tiebreakActionBusy is still set on all). null = no action in
  // flight.
  const [tiebreakBusyAction, setTiebreakBusyAction] = useStateA(null);
  const [tiebreakErr, setTiebreakErr] = useStateA(null);

  // Chusen (drawing lots) candidate state: team-pool ties the daihyosen
  // could not settle (a cycle / all-drawn). Only fetched for team comps in
  // the "pools" phase (non-league too: mixed pool stage can have DH cycles).
  const isTeamComp = c && (c.kind === "team" || c.teamSize > 0);
  const [chusenCandidates, setChusenCandidates] = useStateA(null);
  // Per-member input values: keys are "${groupKey}::${identity}" -> string,
  // where groupKey is "${poolName}::${minPosition}" and identity is
  // checkinPid(member) (id when non-empty, else "name|dojo") -- never
  // the member's index in the group, which reorders after a partial write
  // (bc-appx item 2), and never the bare display name, which two members
  // can share.
  const [chusenInputs, setChusenInputs] = useStateA({});
  // Per-group busy flag: keyed by groupKey "${poolName}::${minPosition}" -> bool
  // (a pool can hold more than one unresolved tied group).
  const [chusenBusy, setChusenBusy] = useStateA({});
  // Per-group error: keyed by the same "${poolName}::${minPosition}" -> string.
  const [chusenGroupErr, setChusenGroupErr] = useStateA({});

  // Lightweight signature so the effect re-runs when match results change.
  // Memoized so typing into a chusen position input (local state) does not
  // re-scan every pool match on each render.
  const poolMatchesSig = useMemoA(
    () => (poolMatches || []).map(m => `${m.id}:${m.status}:${typeof m.winner === "string" ? m.winner : (m.winner && m.winner.name) || ""}`).join("|"),
    [poolMatches]
  );
  // Override-sensitive signature: a chusen result is a rank override, which the
  // backend broadcasts as EventTournamentUpdated WITHOUT changing pool matches.
  // Capturing rank + isOverridden lets the fetch effect refresh candidates after
  // an override recorded elsewhere (another operator tab), not only after a bout.
  const standingsSig = useMemoA(
    () => Object.keys(standings || {}).sort().map(pn =>
      (standings[pn] || []).map(s => `${(s.player && s.player.name) || ""}:${s.rank}:${s.isOverridden ? 1 : 0}`).join(",")
    ).join("|"),
    [standings]
  );

  useEffectA(() => {
    if (!isTeamComp || !c || c.status !== "pools" || !window.API || typeof window.API.chusenCandidates !== "function") {
      setChusenCandidates(null);
      return;
    }
    let cancelled = false;
    window.API.chusenCandidates(c.id, password)
      .then(list => { if (!cancelled) setChusenCandidates(list); })
      .catch(() => { if (!cancelled) setChusenCandidates(null); });
    return () => { cancelled = true; };
  }, [c && c.id, c && c.status, isTeamComp, poolMatchesSig, standingsSig, password]);

  // Fetch candidates whenever poolMatches changes (triggered by match_updated
  // SSE events, which the Go handler now broadcasts for AwaitingLeagueTiebreak).
  useEffectA(() => {
    if (!isTeamLeague || c.status !== "pools") return;
    let cancelled = false;
    window.API.leagueTiebreakCandidates(c.id)
      .then(data => {
        if (cancelled || !mountedRef.current) return;
        setTiebreakCandidates(data.candidates || []);
        setTiebreakFinalized(!!data.finalized);
        setTiebreakErr(null);
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

  const handleTiebreakFinalize = async () => {
    if (!(await window.confirmDialog({ message: "Accept the current standings as final without a tie-breaker? This cannot be undone.", confirmLabel: "Accept shared ranks", danger: false }))) return;
    setTiebreakActionBusy(true);
    setTiebreakBusyAction("finalize");
    setTiebreakErr(null);
    try {
      await window.API.leagueTiebreakFinalize(c.id, password);
      if (mountedRef.current) setTiebreakFinalized(true);
    } catch (e) {
      if (mountedRef.current) setTiebreakErr(e.message || "Failed to finalise standings");
    } finally {
      if (mountedRef.current) { setTiebreakActionBusy(false); setTiebreakBusyAction(null); }
    }
  };

  // Does a DH match already exist for the given group (both sides present in
  // poolMatches)? Used to decide whether to show "Run tie-breaker" vs "Remove".
  const dhMatchExistsForGroup = (groupTeamNames) => {
    const nameSet = new Set(groupTeamNames);
    return (poolMatches || []).some(m => {
      const sideA = m.sideA?.name || m.sideA;
      const sideB = m.sideB?.name || m.sideB;
      return m.id && /(-DH-)/.test(m.id) && nameSet.has(sideA) && nameSet.has(sideB);
    });
  };

  // Returns true if any DH match for the given group is running or already scored.
  // The DELETE endpoint returns 409 in that state, so the Remove button must be disabled.
  const dhMatchScoredForGroup = (groupTeamNames) => {
    const nameSet = new Set(groupTeamNames);
    return (poolMatches || []).some(m => {
      const sideA = m.sideA?.name || m.sideA;
      const sideB = m.sideB?.name || m.sideB;
      if (!(m.id && /(-DH-)/.test(m.id) && nameSet.has(sideA) && nameSet.has(sideB))) return false;
      return m.status === "running" || m.status === "completed" || !!m.winner;
    });
  };

  // Chusen banner: shown when chusenCandidates is non-empty (team comp in
  // pools stage, at least one DH cycle left unresolved).
  const chusenBanner = chusenCandidates && chusenCandidates.length > 0 ? (
    <div
      className="alert alert--warn league-tiebreak"
      role="status"
      aria-live="polite"
    >
      <div className="league-tiebreak__title">Chusen (drawing lots) required</div>
      <div className="league-tiebreak__desc">
        The daihyosen didn&apos;t settle the order (two or more teams tied on daihyosen wins). Draw lots and record each team&apos;s finishing position below.
      </div>
      {chusenCandidates.map((group) => {
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

        // Effective value for a member's input, by the member's IDENTITY
        // (checkinPid), never its position in `members`: the group order
        // comes from the server's live standings sort, which reorders after
        // ANY partial write (see the identity-keying comment above the chusen
        // banner for the
        // mechanism and the reproduced [Alpha,Beta,Gamma] ->
        // [Beta,Alpha,Gamma] case), so an index-keyed lookup can read back a
        // DIFFERENT team's typed value after a mid-loop failure. The operator's
        // edit if present, else the displayed default (minPosition + index --
        // idx is still used here only to pick a distinct default rank per
        // position, not to key the input). Both validation and submit read
        // this so accepting the shown defaults (already a valid permutation)
        // records without forcing a manual edit. Keyed on groupKey (pool +
        // minPosition) as well as identity, not bare poolName: a pool can hold
        // more than one unresolved tied group (see the groupKey comment
        // above), and PoolWinners has no upper bound, so e.g. a cycle at
        // 1st/2nd and another at 3rd/4th in the SAME pool must not share one
        // input/clear per member.
        const effRank = (member, idx) => {
          const raw = chusenInputs[`${groupKey}::${checkinPid(member)}`];
          return parseInt(raw !== undefined ? raw : String(minPosition + idx), 10);
        };

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
          try {
            for (let i = 0; i < members.length; i++) {
              const member = members[i];
              await window.API.overridePoolRank(c.id, poolName, member.name, effRank(member, i), password, member.id, member.dojo);
            }
            // Optimistically hide THIS group only (a pool can hold several) - the
            // effect re-fetches on the next update to reconcile.
            setChusenCandidates(prev => (prev || []).filter(g => !(g.poolName === poolName && g.minPosition === minPosition)));
            // Clear inputs for this group only (groupKey + identity, not bare
            // poolName -- see the effRank comment: a sibling tied group in the
            // same pool must not have its inputs wiped here too).
            setChusenInputs(prev => {
              const next = { ...prev };
              for (let i = 0; i < members.length; i++) delete next[`${groupKey}::${checkinPid(members[i])}`];
              return next;
            });
          } catch (e) {
            setChusenGroupErr(prev => ({ ...prev, [groupKey]: e.message || "Failed to record chusen result" }));
            // The per-member overridePoolRank writes are sequential, so a mid-loop
            // failure may have persisted some ranks but not others. overridePoolRank
            // is idempotent per member (retrying re-sends every rank), and the group
            // stays visible on failure so the operator can retry. Re-fetch the
            // candidates so the banner reflects exactly which teams still need a
            // rank, rather than waiting for the next SSE-driven refresh.
            if (window.API && typeof window.API.chusenCandidates === "function") {
              window.API.chusenCandidates(c.id, password)
                .then(list => setChusenCandidates(list))
                .catch(() => {});
            }
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
            <div className="league-tiebreak__desc" style={{ marginBottom: 8 }}>
              Assign positions {minPosition} to {minPosition + members.length - 1} (one per team):
            </div>
            {members.map((member, idx) => {
              // groupKey + member IDENTITY, not index: see the effRank/
              // checkinPid comments above -- the member array order is
              // not stable across a re-fetch, so an index-keyed input can
              // silently attach to the WRONG team after a mid-loop failure.
              const memberKey = checkinPid(member);
              const inputKey = `${groupKey}::${memberKey}`;
              const defaultVal = minPosition + idx;
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
                disabled={isBusy}
                onClick={handleRecord}
              >
                {isBusy && <span className="spinner" />}
                Record chusen result
              </button>
            </div>
            {groupErrMsg && (
              <div className="league-tiebreak__err">{groupErrMsg}</div>
            )}
          </div>
        );
      })}
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
        All regular matches are complete. The groups below are tied at a qualifying position: run a tie-breaker or accept the shared ranks to finalise standings.
      </div>
      {tiebreakCandidates.map((group) => {
        const names = group.teamNames || [];
        const teamIds = groupTeamIds(group.teams, names);
        const hasDH = dhMatchExistsForGroup(names);
        const dhScored = hasDH && dhMatchScoredForGroup(names);
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
              {!hasDH ? (
                <button
                  type="button"
                  className="btn btn--sm btn--primary"
                  disabled={tiebreakActionBusy}
                  onClick={() => handleTiebreakGenerate(names, teamIds)}
                >
                  {tiebreakBusyAction === generateKey && <span className="spinner" />}
                  Run tie-breaker
                </button>
              ) : (
                <>
                  <button
                    type="button"
                    className="btn btn--sm btn--danger btn--ghost"
                    disabled={tiebreakActionBusy || dhScored}
                    onClick={() => handleTiebreakRemove(names, teamIds)}
                  >
                    {tiebreakBusyAction === removeKey && <span className="spinner" />}
                    Remove unscored tie-breaker
                  </button>
                  {dhScored && (
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
            Accept shared ranks / no tie-breaker
          </button>
          <div className="field__hint" style={{ marginTop: 4 }}>Marks all tied groups as final without additional matches.</div>
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
  const scoreOpenMatch = useMemoA(
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
      onSubmit={async (patch) => {
        try {
          await onEditScore(c.id, scoreOpenMatch.id, patch, scoreOpenMatch);
          if (mountedRef.current) setScoreOpenId(null);
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
      {chusenBanner}
      {leagueTiebreakBanner}
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
export { enrichPoolMatchWithComp, poolMatchesForPool, groupTeamIds };
