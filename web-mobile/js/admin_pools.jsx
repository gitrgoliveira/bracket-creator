// Pools section of a competition: standings rendered via the shared public
// PoolsViewer (draw order + rank/DH badges); matches open the score editor.

// Canonical pool-id parser shared with the display surfaces (single source of
// truth: ./pool_ids.jsx is a leaf module with no import chain).
import { poolNameOf, isSupplementaryBout, isPoolDaihyosenBout } from './pool_ids.jsx';

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
//   * compId: fetches competition details (maxEnchoPeriods, naginata)
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
    const nameOf = (s) => (typeof s === "string" ? s : (s && s.name) || "");
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
    compEngi: isSupplementary ? false : !!(m.compEngi ?? (comp && comp.engi)),
    phase: m.phase || "pool",
    poolName: m.poolName || derivedPoolName,
    // Rep-bout dropdown inputs (empty/false for non-supplementary matches).
    repIsTeam,
    repRosterA,
    repRosterB,
  };
}

function AdminPools({ c, pools, poolMatches, standings, tweaks, onEditScore, password }) {
  const isLeague = c && c.format === "league";
  const [scoreOpenMatch, setScoreOpenMatch] = useStateA(null);
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
  // Per-group input values: keys are "${poolName}::${teamName}" -> string.
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

  const handleTiebreakGenerate = async (groupTeamNames) => {
    const actionKey = groupTeamNames.join(",") + ":generate";
    setTiebreakActionBusy(true);
    setTiebreakBusyAction(actionKey);
    setTiebreakErr(null);
    try {
      await window.API.leagueTiebreakGenerate(c.id, groupTeamNames, password);
      // SSE match_updated will reload poolMatches and re-fetch candidates.
    } catch (e) {
      if (mountedRef.current) setTiebreakErr(e.message || "Failed to generate tie-breaker matches");
    } finally {
      if (mountedRef.current) { setTiebreakActionBusy(false); setTiebreakBusyAction(null); }
    }
  };

  const handleTiebreakRemove = async (groupTeamNames) => {
    if (!(await window.confirmDialog({ message: `Remove unscored tie-breaker matches for ${groupTeamNames.join(", ")}?`, confirmLabel: "Remove", danger: true }))) return;
    const actionKey = groupTeamNames.join(",") + ":remove";
    setTiebreakActionBusy(true);
    setTiebreakBusyAction(actionKey);
    setTiebreakErr(null);
    try {
      await window.API.leagueTiebreakRemove(c.id, groupTeamNames, password);
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
        // A pool can hold more than one unresolved tied group (e.g. a cycle at
        // 1st/2nd and a separate cycle at 3rd/4th). Key by pool + best position
        // so the React key and the busy/error maps never collide across groups.
        const groupKey = `${poolName}::${minPosition}`;
        const isBusy = !!chusenBusy[groupKey];
        const groupErrMsg = chusenGroupErr[groupKey] || null;

        // Effective value for a team's input: the operator's edit if present,
        // else the displayed default (minPosition + its listed index). Both the
        // validation and the submit read this so accepting the shown defaults
        // (already a valid permutation) records without forcing a manual edit.
        const effRank = (name) => {
          const raw = chusenInputs[`${poolName}::${name}`];
          return parseInt(raw !== undefined ? raw : String(minPosition + teamNames.indexOf(name)), 10);
        };

        const handleRecord = async () => {
          // Validate: entered positions must be exactly the set
          // {minPosition .. minPosition + teamNames.length - 1}.
          const expected = new Set();
          for (let i = 0; i < teamNames.length; i++) expected.add(minPosition + i);
          const entered = new Set();
          let valid = true;
          for (const name of teamNames) {
            const val = effRank(name);
            if (isNaN(val) || !expected.has(val) || entered.has(val)) { valid = false; break; }
            entered.add(val);
          }
          if (!valid) {
            const lo = minPosition;
            const hi = minPosition + teamNames.length - 1;
            setChusenGroupErr(prev => ({ ...prev, [groupKey]: `Enter each of positions ${lo} to ${hi} exactly once` }));
            return;
          }
          setChusenGroupErr(prev => ({ ...prev, [groupKey]: null }));
          setChusenBusy(prev => ({ ...prev, [groupKey]: true }));
          try {
            for (const name of teamNames) {
              await window.API.overridePoolRank(c.id, poolName, name, effRank(name), password);
            }
            // Optimistically hide THIS group only (a pool can hold several) - the
            // effect re-fetches on the next update to reconcile.
            setChusenCandidates(prev => (prev || []).filter(g => !(g.poolName === poolName && g.minPosition === minPosition)));
            // Clear inputs for this group.
            setChusenInputs(prev => {
              const next = { ...prev };
              for (const name of teamNames) delete next[`${poolName}::${name}`];
              return next;
            });
          } catch (e) {
            setChusenGroupErr(prev => ({ ...prev, [groupKey]: e.message || "Failed to record chusen result" }));
            // The per-team overridePoolRank writes are sequential, so a mid-loop
            // failure may have persisted some ranks but not others. overridePoolRank
            // is idempotent per team (retrying re-sends every rank), and the group
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
              Assign positions {minPosition} to {minPosition + teamNames.length - 1} (one per team):
            </div>
            {teamNames.map((name) => {
              const inputKey = `${poolName}::${name}`;
              const defaultVal = minPosition + teamNames.indexOf(name);
              // Stable DOM id so the label is programmatically tied to its input.
              const inputId = `chusen-${groupKey}-${teamNames.indexOf(name)}`.replace(/[^a-zA-Z0-9_-]+/g, "-");
              return (
                <div key={name} style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 4 }}>
                  <label htmlFor={inputId} style={{ flex: 1 }}>{name}</label>
                  <input
                    id={inputId}
                    type="number"
                    min={minPosition}
                    max={minPosition + teamNames.length - 1}
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
      {tiebreakCandidates.map((group, gi) => {
        const names = group.teamNames || [];
        const hasDH = dhMatchExistsForGroup(names);
        const dhScored = hasDH && dhMatchScoredForGroup(names);
        const posLabel = group.minPosition === group.maxPosition
          ? `Position ${group.minPosition}`
          : `Positions ${group.minPosition}–${group.maxPosition}`;
        const generateKey = names.join(",") + ":generate";
        const removeKey = names.join(",") + ":remove";
        return (
          <div key={gi} className="league-tiebreak__group">
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
                  onClick={() => handleTiebreakGenerate(names)}
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
                    onClick={() => handleTiebreakRemove(names)}
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

  // Modal rendered alongside the PoolsViewer.
  const scoreModal = scoreOpenMatch ? (
    <ScoreEditorModal
      key={c.id + '-' + scoreOpenMatch.id}
      match={scoreOpenMatch}
      prevMatch={null}
      nextMatch={null}
      onPrev={null}
      onNext={null}
      onClose={() => setScoreOpenMatch(null)}
      onSubmit={async (patch) => {
        try {
          await onEditScore(c.id, scoreOpenMatch.id, patch, scoreOpenMatch);
          if (mountedRef.current) setScoreOpenMatch(null);
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
      {isLeague ? (
        LeagueStandingsViewer ? (
          <LeagueStandingsViewer
            competition={c}
            poolMatches={poolMatches}
            tweaks={tweaks}
            onMatchClick={(m) => setScoreOpenMatch(enrichPoolMatchWithComp(m, c))}
            highlightPlayers={[]}
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
            onMatchClick={(m) => setScoreOpenMatch(enrichPoolMatchWithComp(m, c))}
            highlightPlayers={[]}
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
export { enrichPoolMatchWithComp, poolMatchesForPool };
