// Pools section of a competition: standings tables with rank-override inputs
// and per-pool drill-down. See web-mobile/admin_split_plan.md.

const { useState: useStateA, useEffect: useEffectA, useRef: useRefA } = React;
const pluralize = window.pluralize;

// Rank inputs commit on blur / Enter rather than every keystroke. Typing
// "10" used to fire two API calls (rank=1 then rank=10); the intermediate
// rank=1 collided with whoever already held rank 1 and produced visible
// swap-flicker until the second call landed.
//
// `focusedRef` suppresses the upstream-sync useEffect while the user is
// mid-edit — otherwise an SSE-driven standings refresh (another admin
// completing a match) would clobber the half-typed value. Invalid commits
// (non-numeric, negative, zero) revert to `initial` so the user doesn't
// see their garbage typing persist as if accepted.
//
// `focusValueRef` snapshots `v` at focus time so we can detect
// focus-without-edit and avoid committing a stale value if `initial`
// changed under the user while they were focused (concurrent SSE update).
function RankInput({ initial, className, onCommit, style }) {
  const [v, setV] = useStateA(String(initial));
  const focusedRef = useRefA(false);
  const focusValueRef = useRefA(String(initial));
  useEffectA(() => {
    if (!focusedRef.current) setV(String(initial));
  }, [initial]);
  const handleFocus = () => {
    focusedRef.current = true;
    focusValueRef.current = v;
  };
  const handleBlur = () => {
    focusedRef.current = false;
    // User focused but didn't type anything. If `initial` changed under
    // them while focused (SSE), sync to the latest server value rather
    // than committing the stale focus-time value.
    if (v === focusValueRef.current) {
      if (v !== String(initial)) setV(String(initial));
      return;
    }
    const next = parseInt(v);
    if (!Number.isFinite(next) || next <= 0) {
      setV(String(initial));
      return;
    }
    if (String(next) !== String(initial)) onCommit(String(next));
  };
  const handleKeyDown = (e) => {
    if (e.key === "Enter") {
      e.preventDefault();
      e.currentTarget.blur();
    } else if (e.key === "Escape") {
      e.preventDefault();
      setV(String(initial));
      // Reset the focus snapshot so the subsequent blur is treated as
      // "no edit" rather than triggering revert-to-initial twice.
      focusValueRef.current = String(initial);
      e.currentTarget.blur();
    }
  };
  return (
    <input
      type="number"
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

function AdminPools({ c, pools, standings, tweaks, onEditScore, password }) {
  const resetOverrides = async () => {
    if (!confirm("Are you sure you want to reset ALL manual overrides (ranks and winners) for this competition?")) return;
    try {
      await window.API.resetOverrides(c.id, password);
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

  if (!pools || pools.length === 0) {
    return <div className="empty"><div className="icon">⏳</div><h3>Pools not drawn yet</h3><div style={{ fontSize: 13 }}>Add participants and start the competition to draw pools.</div></div>;
  }

  const selectedPool = selectedPoolName ? pools.find(p => p.poolName === selectedPoolName) : null;

  if (selectedPool) {
    const poolStandings = standings ? standings[selectedPool.poolName] : null;
    const court = c.courts[pools.indexOf(selectedPool) % c.courts.length];
    return (
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
                      <div style={{ fontWeight: 600 }}>{s.player.name}</div>
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

          <div style={{ marginTop: 24 }}>
            <h3 className="section-title">Match Results</h3>
            <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
              {selectedPool.matches.map(m => (
                <div key={m.id} className="sched-row" style={{ gridTemplateColumns: "60px 1fr auto" }}>
                  <div className="sched-row__court" style={{ height: 24, fontSize: 10 }}>#{m.id.split('-').pop()}</div>
                  <div className="sched-row__players">
                    {/* Global UI contract: SHIRO (sideB) on left, AKA (sideA) on right. */}
                    <div className="sched-row__side" style={{ textAlign: "right" }}>
                      <div className="name" style={{ fontSize: 13 }}>{m.sideB?.name || m.sideB}</div>
                    </div>
                    <div className="sched-row__vs">vs</div>
                    <div className="sched-row__side">
                      <div className="name" style={{ fontSize: 13 }}>{m.sideA?.name || m.sideA}</div>
                    </div>
                  </div>
                  <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
                    <div className="sched-row__score" style={{ minWidth: 60, textAlign: "center" }}>
                      {m.status === "completed" ? window.formatIpponsScore(m.ipponsB, m.ipponsA, m.score, m.decision) : m.status === "running" ? "● LIVE" : "—"}
                    </div>
                    <button className="btn btn--sm" onClick={() => onEditScore(c.id, m.id, null, m)}>
                      {m.status === "completed" ? "Edit" : "Score"}
                    </button>
                  </div>
                </div>
              ))}
            </div>
          </div>
        </div>
      </div>
    );
  }
  return (
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
          const isDone = pool.matches && pool.matches.every(m => m.status === "completed");
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
                          <div style={{ fontWeight: 500 }}>{s.player.name}</div>
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
              {pool.matches && pool.matches.length > 0 && (
                <div style={{ marginTop: 12, borderTop: "1px dashed var(--line)", paddingTop: 8 }}>
                  <div style={{ fontSize: 11, fontWeight: 700, color: "var(--ink-3)", textTransform: "uppercase", marginBottom: 6 }}>Matches</div>
                  <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
                    {pool.matches.map(m => (
                      <div key={m.id} style={{ display: "flex", justifyContent: "space-between", fontSize: 12, alignItems: "center", padding: "2px 0" }}>
                        <div style={{ width: 30, fontWeight: 600, color: "var(--accent)" }}>{m.id.split('-').pop()}</div>
                        {/* Global UI contract: SHIRO (sideB) on left, AKA (sideA) on right. */}
                        <div style={{ flex: 1, display: "flex", gap: 6, alignItems: "center" }}>
                          <span style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", maxWidth: 80 }}>{m.sideB?.name || m.sideB}</span>
                          <span style={{ color: "var(--ink-4)", fontSize: 10 }}>vs</span>
                          <span style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", maxWidth: 80 }}>{m.sideA?.name || m.sideA}</span>
                        </div>
                        <div style={{ fontSize: 11, fontWeight: 600, display: "flex", alignItems: "center", gap: 8 }}>
                          {m.status === "completed" ? window.formatIpponsScore(m.ipponsB, m.ipponsA, m.score, m.decision) : m.status === "running" ? "● LIVE" : "—"}
                          <button className="btn btn--sm" style={{ padding: "2px 6px", fontSize: 10 }} onClick={(e) => { e.stopPropagation(); onEditScore(c.id, m.id, null, m); }}>Score</button>
                        </div>
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
  );
}

window.AdminPools = AdminPools;
