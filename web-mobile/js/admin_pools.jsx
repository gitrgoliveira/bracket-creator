// Pools section of a competition: standings tables with rank-override inputs
// and per-pool drill-down. See web-mobile/admin_split_plan.md.

const { useState: useStateA } = React;
const pluralize = window.pluralize;

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
                      <input
                        type="number"
                        className="input"
                        value={s.rank || i + 1}
                        onChange={(e) => overrideRank(selectedPool.poolName, s.player.name, e.target.value)}
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
                    <div className="sched-row__side" style={{ textAlign: "right" }}>
                      <div className="name" style={{ fontSize: 13 }}>{m.sideA?.name || m.sideA}</div>
                    </div>
                    <div className="sched-row__vs">vs</div>
                    <div className="sched-row__side">
                      <div className="name" style={{ fontSize: 13 }}>{m.sideB?.name || m.sideB}</div>
                    </div>
                  </div>
                  <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
                    <div className="sched-row__score" style={{ minWidth: 60, textAlign: "center" }}>
                      {m.status === "completed" ? window.formatIpponsScore(m.ipponsA, m.ipponsB, m.score, m.decision) : m.status === "running" ? "● LIVE" : "—"}
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
            <div key={pool.poolName} className={`pool ${isDone ? "pool--done" : ""}`} onClick={() => setSelectedPoolName(pool.poolName)} style={{ cursor: "pointer" }}>
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
                          <input
                            type="number"
                            className="rank-input"
                            value={s.rank || i + 1}
                            onChange={(e) => overrideRank(pool.poolName, s.player.name, e.target.value)}
                            style={{
                              width: 32,
                              border: s.isOverridden ? "1px solid var(--accent)" : "1px solid transparent",
                              background: s.isOverridden ? "var(--accent-soft)" : "transparent",
                              borderRadius: 4,
                              textAlign: "center",
                              fontSize: 12,
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
                        <div style={{ flex: 1, display: "flex", gap: 6, alignItems: "center" }}>
                          <span style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", maxWidth: 80 }}>{m.sideA?.name || m.sideA}</span>
                          <span style={{ color: "var(--ink-4)", fontSize: 10 }}>vs</span>
                          <span style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", maxWidth: 80 }}>{m.sideB?.name || m.sideB}</span>
                        </div>
                        <div style={{ fontSize: 11, fontWeight: 600, display: "flex", alignItems: "center", gap: 8 }}>
                          {m.status === "completed" ? window.formatIpponsScore(m.ipponsA, m.ipponsB, m.score, m.decision) : m.status === "running" ? "● LIVE" : "—"}
                          <button className="btn btn--sm" style={{ padding: "2px 6px", fontSize: 10 }} onClick={() => onEditScore(c.id, m.id, null, m)}>Score</button>
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
