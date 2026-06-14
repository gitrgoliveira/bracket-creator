// Pools section of a competition: standings tables with rank-override inputs
// and per-pool drill-down. See web-mobile/admin_split_plan.md.

const { useState: useStateA, useEffect: useEffectA, useRef: useRefA, useMemo: useMemoA } = React;
const pluralize = window.pluralize;
// Canonical rank cap (admin_helpers.jsx) — mirrors helper.MaxRankOverride
// on the Go side. The override-rank handler ALSO validates against the
// actual pool size; this cap is the absolute overflow guard.
const MAX_RANK = window.MAX_RANK;
const getScoreBtnClass = window.getScoreBtnClass;
const ScoreEditorModal = window.ScoreEditorModal;

// Pure decision logic for what RankInput.handleBlur should do, given the
// state of its refs and props at blur time. Returned as a tagged action so
// callers (handleBlur) just dispatch — no React state lives in here.
//
// Cases (in priority order):
//   1. `cancelled` (Esc was pressed) → noop. The Esc handler already
//      queued setV(String(initial)) for the visual revert.
//   2. User focused but never typed (v === focusValue): if `initial`
//      changed under them while focused (SSE update), sync to it;
//      otherwise noop. This is the focus-without-edit TOCTOU guard:
//      committing the stale focus-time value here would clobber a
//      concurrent server update.
//   3. Invalid input (not finite, ≤ 0, > MAX_RANK) → revert visually to
//      String(initial) so the user doesn't see garbage persist as if
//      accepted. MAX_RANK mirrors the server-side overflow cap
//      (helper.MaxRankOverride); the backend ALSO rejects when the
//      rank exceeds the actual pool size, which is the real semantic
//      constraint — this cap is the overflow guard.
//   4. Valid edit different from initial → commit String(parsed).
//      Same value as initial (e.g. typed "02" when initial is 2,
//      or no-op retype) → noop.
//
// Exported for vitest at __tests__/admin_pools.test.jsx.
function decideRankCommit({ v, initial, focusValue, cancelled }) {
  if (cancelled) return { action: "noop" };
  if (v === focusValue) {
    if (v !== String(initial)) return { action: "sync", value: String(initial) };
    return { action: "noop" };
  }
  const next = parseInt(v);
  if (!Number.isFinite(next) || next <= 0 || next > MAX_RANK) {
    return { action: "revert", value: String(initial) };
  }
  if (String(next) !== String(initial)) return { action: "commit", value: String(next) };
  return { action: "noop" };
}

// Regex that strips the trailing match-index segment from a pool-match id to
// recover the pool name. Backend formats: "PoolName-N", "PoolName-DH-N",
// "PoolName-TB-N". A plain split('-')[0] breaks on hyphenated pool names
// (e.g. "Pool A-East-0" → "Pool A", not "Pool A-East"). This regex captures
// everything before the trailing numeric/DH/TB suffix; ids without a
// recognisable suffix degrade to "" (no pool name inferable).
const POOL_MATCH_ID_RE = /^(.*?)-(?:DH-|TB-)?\d+$/;

// Extract the pool name from a pool-match id using POOL_MATCH_ID_RE.
// Returns "" when the id is falsy or has no recognisable suffix.
// Single source of truth so all callers stay in sync if the backend
// id format evolves.
function poolNameFromMatchId(id) {
  return (id || "").match(POOL_MATCH_ID_RE)?.[1] ?? "";
}

// Filter a flat poolMatches array down to entries belonging to a single pool.
// Uses POOL_MATCH_ID_RE so DH/TB suffix variants are handled correctly.
//
// pool.matches (helper.Match) carries only sideA/sideB — no id, status, or
// score data. poolMatches (state.MatchResult) has the full data including the
// id required by the score API endpoint. Score buttons in the pool-card view
// must use poolMatchesForPool to get the correct MatchResult objects.
//
// Exported for vitest at __tests__/admin_pools.test.jsx.
function poolMatchesForPool(poolMatches, poolName) {
  return (poolMatches || []).filter(m => poolNameFromMatchId(m.id) === poolName);
}

// Enrich a pool-match object with the comp-* metadata that
// ScoreEditorModal reads off the match prop. Pool matches arrive as the
// raw MatchResult shape (id, status, sides, ippons, decision) with none
// of the comp-level fields the modal needs:
//   * compKind / teamSize — picks TeamScoreEditorModal vs individual editor
//   * compId — fetches competition details (maxEnchoPeriods, naginata)
//              and is the path for decision/score endpoints
//   * compName — header eyebrow
//   * phase / poolName — header subtitle ("CompName · PoolName")
//
// Pool name falls back to the prefix of the match id ("PoolName-MatchIdx"
// per parsePoolMatchesRecords in internal/state/pools.go) when the caller
// can't supply a known pool name. Existing falsy fields on `m` are
// overwritten with derived values — truthy fields are preserved so a
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
  const derivedPoolName = poolNameOverride || poolNameFromMatchId(m.id);
  const playerMap = window.buildPlayerMap ? window.buildPlayerMap(comp) : {};
  const toPlayer = (side) => {
    if (side && typeof side === "object") return side;
    if (!side) return { id: "", name: "" };
    const p = playerMap[side];
    return p || { id: side, name: side };
  };
  // Pool daihyosen ("Pool X-DH-N") and tiebreaker ("Pool X-TB-N") bouts are
  // single representative/ippon-shobu matches, scored as INDIVIDUAL even in a
  // team competition — force compKind=""/teamSize=0 so ScoreEditorModal routes
  // to the individual editor (one bout), not the 5-person team sheet. This
  // mirrors the same override in viewer.jsx compMatches; without it, scoring a
  // team pool-DH from the Pools tab opens the wrong (team) scorer.
  const isSupplementary = /-(?:DH|TB)-\d+$/.test(m.id || "");
  return {
    ...m,
    sideA: toPlayer(m.sideA),
    sideB: toPlayer(m.sideB),
    compId: m.compId || (comp && comp.id) || "",
    compName: m.compName || (comp && comp.name) || "",
    compKind: isSupplementary ? "" : (m.compKind || (comp && comp.kind) || ""),
    teamSize: isSupplementary ? 0 : (m.teamSize ?? (comp && comp.teamSize) ?? 0),
    phase: m.phase || "pool",
    poolName: m.poolName || derivedPoolName,
  };
}

// Rank inputs commit on blur / Enter rather than every keystroke. Typing
// "10" used to fire two API calls (rank=1 then rank=10); the intermediate
// rank=1 collided with whoever already held rank 1 and produced visible
// swap-flicker until the second call landed.
//
// `focusedRef` suppresses the upstream-sync useEffect while the user is
// mid-edit — otherwise an SSE-driven standings refresh (another admin
// completing a match) would clobber the half-typed value.
//
// `focusValueRef` snapshots `v` at focus time so we can detect
// focus-without-edit and avoid committing a stale value if `initial`
// changed under the user while they were focused.
//
// `cancelRef` lets the Esc handler signal handleBlur to skip the commit.
// Without it, the React-async-state hazard would let Esc actually commit
// the typed value: setV(String(initial)) is queued, but blur() fires
// handleBlur synchronously with the stale typed `v` still in the closure.
function RankInput({ initial, className, onCommit, style }) {
  const [v, setV] = useStateA(String(initial));
  const focusedRef = useRefA(false);
  const focusValueRef = useRefA(String(initial));
  const cancelRef = useRefA(false);
  useEffectA(() => {
    if (!focusedRef.current) setV(String(initial));
  }, [initial]);
  const handleFocus = () => {
    focusedRef.current = true;
    focusValueRef.current = v;
  };
  const handleBlur = () => {
    const result = decideRankCommit({
      v,
      initial,
      focusValue: focusValueRef.current,
      cancelled: cancelRef.current,
    });
    focusedRef.current = false;
    if (cancelRef.current) cancelRef.current = false;
    if (result.action === "commit") {
      // Mirror the normalized value into local state so the input
      // immediately reflects what's being saved. Without this, typing
      // "5abc" then blurring would commit rank=5 to the server but
      // leave the input displaying "5abc" until the SSE-driven prop
      // refresh re-runs the upstream-sync useEffect — a confusing
      // few-hundred-ms window where the visible value doesn't match
      // what was sent.
      setV(result.value);
      onCommit(result.value);
    } else if (result.action === "sync" || result.action === "revert") {
      setV(result.value);
    }
    // "noop" → nothing.
  };
  const handleKeyDown = (e) => {
    if (e.key === "Enter") {
      e.preventDefault();
      e.currentTarget.blur();
    } else if (e.key === "Escape") {
      e.preventDefault();
      cancelRef.current = true;
      setV(String(initial));
      e.currentTarget.blur();
    }
  };
  return (
    <input
      type="number"
      min="1"
      max={MAX_RANK}
      className={className}
      value={v}
      onChange={(e) => setV(e.target.value)}
      onFocus={handleFocus}
      onBlur={handleBlur}
      onKeyDown={handleKeyDown}
      onClick={(e) => e.stopPropagation()}
      autoComplete="off"
      style={style}
    />
  );
}

function AdminPools({ c, pools, poolMatches, standings, tweaks, onEditScore, password }) {
  const resetOverrides = async () => {
    if (!(await window.confirmDialog({ message: "Are you sure you want to reset ALL manual overrides (ranks and winners) for this competition?", confirmLabel: "Reset overrides", danger: true }))) return;
    const admin = await window.promptAdminPassword();
    if (admin === null) return;
    try {
      await window.API.resetOverrides(c.id, password, admin);
    } catch (e) {
      alert("Failed to reset overrides: " + e.message);
    }
  };

  const overrideRank = async (poolName, playerName, rank) => {
    try {
      const nextRank = parseInt(rank);
      if (isNaN(nextRank) || nextRank <= 0) return;
      await window.API.overridePoolRank(c.id, poolName, playerName, nextRank, password);
    } catch (e) {
      alert("Failed to override rank: " + e.message);
    }
  };

  const [selectedPoolName, setSelectedPoolName] = useStateA(null);
  const [scoreOpenMatch, setScoreOpenMatch] = useStateA(null);
  const mountedRef = useRefA(true);
  useEffectA(() => () => { mountedRef.current = false; }, []);

  // ScoreEditorModal reads comp-* metadata off the match (compId, compKind,
  // teamSize, compName, phase, poolName). Pool-match objects from
  // pools[i].matches carry only the MatchResult shape — no comp-level
  // enrichment — so we wrap them at the modal boundary via the pure
  // enrichPoolMatchWithComp helper. See its docstring for the rationale.
  const enrichPoolMatch = (m, poolNameOverride) => enrichPoolMatchWithComp(m, c, poolNameOverride);

  // pool.matches (helper.Match) only carries sideA/sideB — no id, status, or
  // score data. poolMatches (state.MatchResult) has the full data including
  // the id used by the score API endpoint. Use poolMatchesFor filtered by
  // pool name for rendering match rows so Score/Edit buttons have the real id.
  // See poolMatchesForPool's docstring for the full rationale.
  //
  // Precompute a Map<poolName, MatchResult[]> so poolMatchesFor is O(1) per
  // lookup instead of O(pools × matches) — each card in the grid called the
  // old inline filter independently, scanning the full array per pool.
  const poolMatchesMap = useMemoA(() => {
    const map = new Map();
    for (const m of (poolMatches || [])) {
      const name = poolNameFromMatchId(m.id);
      if (!name) continue;
      const bucket = map.get(name);
      if (bucket) { bucket.push(m); } else { map.set(name, [m]); }
    }
    return map;
  }, [poolMatches]);
  const poolMatchesFor = (poolName) => poolMatchesMap.get(poolName) ?? [];

  // Modal rendered in both return paths (detail view and card list).
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
    return <div className="empty"><div className="icon">⏳</div><h3>Pools not drawn yet</h3><div style={{ fontSize: 13 }}>Add participants and start the competition to draw pools.</div></div>;
  }

  const selectedPool = selectedPoolName ? pools.find(p => p.poolName === selectedPoolName) : null;

  if (selectedPool) {
    const poolStandings = standings ? standings[selectedPool.poolName] : null;
    const court = c.courts[pools.indexOf(selectedPool) % c.courts.length];
    return (
      <>
      <div className="pool-detail">
        <div style={{ marginBottom: 16 }}>
          <button className="btn btn--sm" onClick={() => setSelectedPoolName(null)}>← All pools</button>
        </div>
        <div className="card">
          <div className="card__head">
            <div>
              <h2 className="page-head__title">{selectedPool.poolName}</h2>
              <div className="card__sub">Shiaijo {court} · {pluralize(selectedPool.players.length, "participant")}</div>
            </div>
            <button className="btn btn--sm btn--danger" onClick={resetOverrides}>Reset rankings</button>
          </div>

          <div className="field__hint" style={{ marginBottom: 12 }}>
            Rankings are calculated automatically based on wins, points, and sub-scores.
            Enter a number in the "#" column to manually override the rank.
          </div>

          <table className="pool__table" style={{ fontSize: 14 }}>
            <thead>
              {c.kind === "team" || c.teamSize > 0 ? (
                <tr><th>#</th><th>Team</th><th className="num">W</th><th className="num">L</th><th className="num">T</th><th className="num">IV</th><th className="num">IL</th><th className="num">IT</th><th className="num">PW</th><th className="num">PL</th></tr>
              ) : (
                <tr><th>#</th><th>Player</th><th className="num">W</th><th className="num">L</th><th className="num">D</th><th className="num">PW</th><th className="num">PL</th></tr>
              )}
            </thead>
            <tbody>
              {(poolStandings || selectedPool.players.map((p) => ({ player: p, wins: 0, losses: 0, draws: 0, ipponsGiven: 0, ipponsTaken: 0 }))).map((s, i) => {
                const isTeamComp = c.kind === "team" || c.teamSize > 0;
                return (
                  <tr key={s.player.name}>
                    <td style={{ width: 60 }}>
                      <RankInput
                        initial={s.rank || i + 1}
                        className="input"
                        onCommit={(v) => overrideRank(selectedPool.poolName, s.player.name, v)}
                        style={{
                          width: 44,
                          padding: "4px 8px",
                          border: s.isOverridden ? "1px solid var(--accent)" : "1px solid var(--line)",
                          background: s.isOverridden ? "var(--accent-soft)" : "transparent",
                          textAlign: "center",
                          fontWeight: s.isOverridden ? "700" : "400"
                        }}
                      />
                    </td>
                    <td>
                      <div style={{ fontWeight: 600 }}>
                        {s.player.number ? <span className="num-prefix">{s.player.number}</span> : null}
                        {s.player.name}
                      </div>
                      {tweaks.showDojo && <div style={{ fontSize: 12, color: "var(--ink-3)" }}>{s.player.dojo}</div>}
                    </td>
                    <td className="num">{s.wins}</td>
                    <td className="num">{s.losses}</td>
                    <td className="num">{s.draws || 0}</td>
                    {isTeamComp && <td className="num">{s.individualWins || 0}</td>}
                    {isTeamComp && <td className="num">{s.individualLosses || 0}</td>}
                    {isTeamComp && <td className="num">{s.individualDraws || 0}</td>}
                    <td className="num">{isTeamComp ? (s.pointsWon || 0) : s.ipponsGiven}</td>
                    <td className="num">{isTeamComp ? (s.pointsLost || 0) : s.ipponsTaken}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>

          {/* Pool-daihyosen banner: shown when the backend has injected DH matches
              for this pool (all regular matches complete but teams still tied). */}
          {(() => {
            const dhPrefix = selectedPool.poolName + '-DH-';
            const dhMatches = (poolMatches || []).filter(m =>
              (m.id || "").startsWith(dhPrefix)
            );
            if (dhMatches.length === 0) return null;
            const pending = dhMatches.filter(m => m.status !== "completed" || !m.winner);
            const label = pending.length > 0
              ? `${pending.length} daihyosen bout${pending.length > 1 ? "s" : ""} pending — teams tied on all 8 criteria`
              : "Daihyosen complete — standings updated";
            const color = pending.length > 0 ? "var(--warn-bg, #fffbe6)" : "var(--ok-bg, #e8f5e9)";
            const border = pending.length > 0 ? "1px solid var(--warn, #e6a817)" : "1px solid var(--ok, #4caf50)";
            return (
              <div style={{ marginTop: 16, padding: "10px 14px", background: color, border, borderRadius: 6, fontSize: 13 }}>
                <strong>Representative bout (daihyosen):</strong> {label}
                {pending.length > 0 && (
                  <ul style={{ margin: "6px 0 0", paddingLeft: 20 }}>
                    {pending.map(m => {
                      const a = m.sideA?.name || m.sideA || "";
                      const b = m.sideB?.name || m.sideB || "";
                      return <li key={m.id}>{a && b ? `${b} vs ${a}` : m.id}</li>;
                    })}
                  </ul>
                )}
              </div>
            );
          })()}

          <div style={{ marginTop: 24 }}>
            <h3 className="section-title">Match Results</h3>
            <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
              {poolMatchesFor(selectedPool.poolName).map(m => (
                <div key={m.id} className="sched-row" style={{ gridTemplateColumns: "60px 1fr auto" }}>
                  <div className="sched-row__court" style={{ height: 24, fontSize: 10 }}>#{m.id ? m.id.split('-').pop() : ""}</div>
                  <div className="sched-row__players">
                    {/* Global UI contract: SHIRO (sideB) on left, AKA (sideA) on right (mp-41o). */}
                    <div className="sched-row__side" style={{ textAlign: "right" }}>
                      <div className="name" style={{ fontSize: 13, display: "flex", alignItems: "center", justifyContent: "flex-end" }}>
                        <span className="bc-color-badge bc-color-badge--shiro">SHIRO</span>
                        {m.sideB?.name || m.sideB}
                      </div>
                    </div>
                    <div className="sched-row__vs">vs</div>
                    <div className="sched-row__side">
                      <div className="name" style={{ fontSize: 13, display: "flex", alignItems: "center" }}>
                        <span className="bc-color-badge bc-color-badge--aka">AKA</span>
                        {m.sideA?.name || m.sideA}
                      </div>
                    </div>
                  </div>
                  <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
                    <div className="sched-row__score" style={{ minWidth: 60, textAlign: "center" }}>
                      {m.status === "completed" ? window.formatIpponsScore(m.ipponsB, m.ipponsA, m.score, m.decision, m.encho, m.decidedByHantei) : m.status === "running" ? "● NOW" : "—"}
                    </div>
                    <button className={getScoreBtnClass(m.status)} onClick={() => setScoreOpenMatch(enrichPoolMatch(m, selectedPool.poolName))}>
                      {m.status === "completed" ? "Correct" : "Score"}
                    </button>
                  </div>
                </div>
              ))}
            </div>
          </div>
        </div>
      </div>
      {scoreModal}
      </>
    );
  }
  return (
    <>
    <div>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 14 }}>
        <div>
          <div style={{ fontSize: 14, fontWeight: 600 }}>{pluralize(pools.length, "pool")}</div>
        </div>
        <button className="btn btn--sm btn--danger" onClick={resetOverrides}>Reset all overrides</button>
      </div>
      <div className="pools-grid">
        {pools.map((pool) => {
          const poolStandings = standings ? standings[pool.poolName] : null;
          const court = c.courts[pools.indexOf(pool) % c.courts.length];
          const pm = poolMatchesFor(pool.poolName);
          // pool.matches (helper.Match) has no status field; use poolMatches-
          // sourced pm entries which carry the full MatchResult including status.
          const isDone = pm.length > 0 && pm.every(m => m.status === "completed");
          return (
            <div
              key={pool.poolName}
              className={`pool ${isDone ? "pool--done" : ""}`}
              role="button"
              tabIndex={0}
              onClick={() => setSelectedPoolName(pool.poolName)}
              // Only fire when the card itself has focus, not a nested
              // rank input or per-match Score button — those handle their
              // own activation.
              onKeyDown={(e) => { if (e.target === e.currentTarget && (e.key === "Enter" || e.key === " ")) { e.preventDefault(); setSelectedPoolName(pool.poolName); } }}
              style={{ cursor: "pointer" }}
            >
              <div className="pool__head">
                <div style={{ display: "flex", justifyContent: "space-between", width: "100%", alignItems: "center" }}>
                  <div className="pool__name">{pool.poolName}</div>
                  <div className="tag-badge" style={{ margin: 0 }}>SHIAIJO {court}</div>
                </div>
              </div>
              <table className="pool__table">
                <thead>
                  {c.kind === "team" || c.teamSize > 0 ? (
                    <tr><th>#</th><th>Team</th><th className="num">W</th><th className="num">L</th><th className="num">T</th><th className="num">IV</th><th className="num">IL</th><th className="num">IT</th><th className="num">PW</th><th className="num">PL</th></tr>
                  ) : (
                    <tr><th>#</th><th>Player</th><th className="num">W</th><th className="num">L</th><th className="num">D</th><th className="num">PW</th><th className="num">PL</th></tr>
                  )}
                </thead>
                <tbody>
                  {(poolStandings || pool.players.map((p) => ({ player: p, wins: 0, losses: 0, draws: 0, ipponsGiven: 0, ipponsTaken: 0 }))).map((s, i) => {
                    const isTeamComp = c.kind === "team" || c.teamSize > 0;
                    return (
                      <tr key={s.player.name}>
                        <td style={{ color: "var(--ink-3)", fontFamily: "var(--font-mono)" }}>
                          <RankInput
                            initial={s.rank || i + 1}
                            className="rank-input"
                            onCommit={(v) => overrideRank(pool.poolName, s.player.name, v)}
                            style={{
                              width: 40,
                              height: 36,
                              padding: "0 4px",
                              border: s.isOverridden ? "1px solid var(--accent)" : "1px solid transparent",
                              background: s.isOverridden ? "var(--accent-soft)" : "transparent",
                              borderRadius: 4,
                              textAlign: "center",
                              fontSize: 13,
                              fontWeight: s.isOverridden ? "700" : "400"
                            }}
                          />
                        </td>
                        <td>
                          <div style={{ fontWeight: 500 }}>
                            {s.player.number ? <span className="num-prefix">{s.player.number}</span> : null}
                            {s.player.name}
                          </div>
                          {tweaks.showDojo && <div style={{ fontSize: 11, color: "var(--ink-3)" }}>{s.player.dojo}</div>}
                        </td>
                        <td className="num">{s.wins}</td>
                        <td className="num">{s.losses}</td>
                        <td className="num">{s.draws || 0}</td>
                        {isTeamComp && <td className="num">{s.individualWins || 0}</td>}
                        {isTeamComp && <td className="num">{s.individualLosses || 0}</td>}
                        {isTeamComp && <td className="num">{s.individualDraws || 0}</td>}
                        <td className="num">{isTeamComp ? (s.pointsWon || 0) : s.ipponsGiven}</td>
                        <td className="num">{isTeamComp ? (s.pointsLost || 0) : s.ipponsTaken}</td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
              {pm.length > 0 && (
                <div style={{ marginTop: 12, borderTop: "1px dashed var(--line)", paddingTop: 8 }}>
                  <div className="overline" style={{ marginBottom: 6 }}>Matches</div>
                  <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
                    {pm.map(m => (
                      // Fixed grid columns keep the names from reflowing when a
                      // score replaces "—" or the button label flips
                      // Score→Correct (mp-p8n). Column order encodes the global
                      // UI contract: SHIRO (sideB) on the left, AKA (sideA) on
                      // the right (mp-41o).
                      <div key={m.id} style={{ display: "grid", gridTemplateColumns: "26px minmax(0,1fr) 18px minmax(0,1fr) 56px 62px", gap: 6, fontSize: 12, alignItems: "center", padding: "2px 0" }}>
                        <div style={{ fontWeight: 600, color: "var(--accent)" }}>{m.id ? m.id.split('-').pop() : ""}</div>
                        <div style={{ display: "flex", alignItems: "center", minWidth: 0 }}>
                          <span className="bc-color-badge bc-color-badge--shiro">SHIRO</span>
                          <span style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{m.sideB?.name || m.sideB}</span>
                        </div>
                        <span style={{ color: "var(--ink-4)", fontSize: 10, textAlign: "center" }}>vs</span>
                        <div style={{ display: "flex", alignItems: "center", minWidth: 0 }}>
                          <span className="bc-color-badge bc-color-badge--aka">AKA</span>
                          <span style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{m.sideA?.name || m.sideA}</span>
                        </div>
                        <div style={{ fontSize: 11, fontWeight: 600, textAlign: "right", whiteSpace: "nowrap" }}>
                          {m.status === "completed" ? window.formatIpponsScore(m.ipponsB, m.ipponsA, m.score, m.decision, m.encho, m.decidedByHantei) : m.status === "running" ? "● NOW" : "—"}
                        </div>
                        <button className={getScoreBtnClass(m.status)} style={{ minWidth: 0 }} onClick={(e) => { e.stopPropagation(); setScoreOpenMatch(enrichPoolMatch(m, pool.poolName)); }}>{m.status === "completed" ? "Correct" : "Score"}</button>
                      </div>
                    ))}
                  </div>
                </div>
              )}
            </div>
          );
        })}
      </div>
    </div>
    {scoreModal}
    </>
  );
}

window.AdminPools = AdminPools;

// ES export for the vitest suite — pure helpers only. The component
// stays behind window.* to match the rest of admin_*.jsx.

// Build a map of match id → MatchResult for O(1) running-state lookups.
function buildRunningById(poolMatches) {
  return Object.fromEntries((poolMatches || []).map(m => [m.id, m]));
}

// Returns true when rank inputs should be locked (competition is past the
// pools phase — playoffs, completed, setup, invalid, or unknown status).
function isRanksLocked(compStatus) {
  return compStatus !== "pools";
}

export { decideRankCommit, enrichPoolMatchWithComp, poolMatchesForPool, buildRunningById, isRanksLocked };
