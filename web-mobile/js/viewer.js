// Viewer side — mobile-first. Single tournament. Shows competitions as the home;
// each competition opens to its own Overview/Bracket/Pools/Schedule/Results.

const { useState, useMemo, useRef: useRefV } = React;

function StatusBadge({ status }) {
  const map = {
    setup: ["badge--setup", "Setup"],
    pools: ["badge--pools", "Pools"],
    playoffs: ["badge--playoffs", "Playoffs"],
    completed: ["badge--completed", "Completed"],
  };
  const [cls, label] = map[status] || ["badge--setup", status];
  const showLive = status === "pools" || status === "playoffs";
  return (
    <span className={`badge ${cls}`}>
      {showLive && <span className="dot dot--live" style={{ marginRight: 4 }}></span>}
      {label}
    </span>
  );
}

function formatDate(d) {
  const date = new Date(d + "T00:00");
  return date.toLocaleDateString("en-GB", { day: "numeric", month: "short", year: "numeric" });
}

function competitionKindLabel(c) {
  // c may be a kind string for backward-compat or a competition obj
  if (typeof c === "string") return c === "team" ? "Teams" : "Individual";
  const base = c.kind === "team" ? "Teams" : "Individual";
  if (c.gender === "M") return `${base} · Men`;
  if (c.gender === "F") return `${base} · Women`;
  return base;
}

function compMatches(c) {
  const out = [];
  const poolMatches = c.poolMatches || (c.pools ? c.pools.flatMap(p => p.matches.map(m => ({ ...m, phase: "pool", poolName: p.name, phaseName: p.name }))) : []);
  poolMatches.forEach(m => out.push({ ...m, compId: c.id, compName: c.name, compKind: c.kind, teamSize: c.teamSize }));

  const rounds = (c.bracket && c.bracket.rounds) ? c.bracket.rounds : (c.bracket || []);
  rounds.forEach((round, ri) => round.forEach((m) => out.push({
    ...m,
    phase: "bracket",
    round: window.roundLabel(ri, rounds.length),
    phaseName: window.roundLabel(ri, rounds.length),
    compId: c.id,
    compName: c.name,
    compKind: c.kind,
    teamSize: c.teamSize
  })));
  return out;
}

function tournamentMatches(t) {
  return (t.competitions || []).flatMap(compMatches);
}

// Next match to be played in this competition (live first, else first scheduled in time order)
function currentMatchOf(c) {
  const ms = compMatches(c);
  const live = ms.find((m) => m.status === "in_progress" && m.sideA && m.sideB);
  if (live) return live;
  const sched = ms.filter((m) => m.status === "scheduled" && m.sideA && m.sideB);
  sched.sort((a, b) => (a.scheduledAt || "99:99").localeCompare(b.scheduledAt || "99:99"));
  return sched[0] || null;
}

function ViewerHome({ tournament, onSelectCompetition, onAdminClick, onOpenSchedule }) {
  const t = tournament;
  // global "across-all-competitions" lists for the home page
  const allMatches = useMemo(() => tournamentMatches(t), [t]);
  const live = allMatches.filter((m) => m.status === "in_progress");
  const upNext = allMatches.filter((m) => m.status === "scheduled" && m.sideA && m.sideB).slice(0, 3);

  return (
    <div className="viewer">
      <div className="viewer__shell">
        <div className="viewer__head viewer__head--hero">
          <div className="topbar__logo viewer__logo">BC</div>
          <div className="viewer__title-block">
            <div className="viewer__eyebrow">{formatDate(t.date)} · {t.venue}</div>
            <div className="viewer__title viewer__title--lg">{t.name}</div>
          </div>
          <button className="viewer__admin-pill" onClick={onAdminClick}>
            <span>🔒</span> Admin
          </button>
        </div>

        <div className="viewer__body">
          {live.length > 0 && (
            <div className="hero-live">
              <div className="hero-live__lbl"><span className="dot dot--live"></span> LIVE NOW · {live.length} match{live.length === 1 ? "" : "es"}</div>
              <div className="vsched" style={{ marginTop: 8 }}>
                {live.slice(0, 3).map((m) => <VSchedItem key={m.compId + m.id} m={m} tweaks={{ showDojo: true }} showCompetition />)}
              </div>
            </div>
          )}

          <button
            className="vlist-item vlist-item--row"
            onClick={onOpenSchedule}
          >
            <span className="vlist-item__icon">🗓</span>
            <div className="vlist-item__rowbody">
              <div className="vlist-item__rowtitle">Full schedule</div>
              <div className="vlist-item__rowsub">{allMatches.filter((m) => m.sideA && m.sideB).length} matches across {tournament.courts.length} shiaijo · search by player or team</div>
            </div>
            <span className="vlist-item__rowchev">→</span>
          </button>

          <div className="section-title">Competitions</div>
          <div className="vlist">
            {(t.competitions || []).map((c) => {
              const matches = compMatches(c).filter((m) => m.sideA && m.sideB); // exclude TBD placeholders
              const total = matches.length;
              const done = matches.filter((m) => m.status === "complete").length;
              const liveCount = matches.filter((m) => m.status === "in_progress").length;
              const pct = total ? Math.round((done / total) * 100) : 0;
              return (
                <button key={c.id} className="vlist-item vlist-item--comp" onClick={() => onSelectCompetition(c.id)}>
                  <div style={{ display: "flex", justifyContent: "space-between", alignItems: "flex-start", gap: 8 }}>
                    <div style={{ minWidth: 0 }}>
                      <div className="vlist-item__eyebrow">{competitionKindLabel(c)}{c.teamSize ? ` · ${c.teamSize}-person` : ""}</div>
                      <div className="vlist-item__name">{c.name}</div>
                      <div className="vlist-item__meta">
                        {c.players.length} {c.kind === "team" ? "teams" : "players"} · {c.format === "pools" ? "Pools + Knockout" : "Knockout"} · Starts {c.startTime}
                      </div>
                    </div>
                    <StatusBadge status={c.status} />
                  </div>
                  {c.status !== "setup" && total > 0 && (
                    <div className="vlist-item__progress">
                      <div className="vlist-item__bar"><div style={{ width: pct + "%" }}></div></div>
                      <div className="vlist-item__pct">
                        {liveCount > 0 ? <span style={{ color: "var(--red)", fontWeight: 600 }}>● {liveCount} live</span> : `${done}/${total} matches`}
                      </div>
                    </div>
                  )}
                </button>
              );
            })}
          </div>

          {upNext.length > 0 && (
            <>
              <div className="section-title" style={{ marginTop: 20 }}>Up next · {upNext.length}</div>
              <div className="vsched">
                {upNext.map((m) => <VSchedItem key={m.compId + m.id} m={m} tweaks={{ showDojo: true }} showCompetition />)}
              </div>
            </>
          )}
        </div>
      </div>
    </div>
  );
}

function ViewerCompetition({ tournament, competition, pools, poolMatches, standings, bracket, onBack, onAdminClick, tweaks }) {
  const [tab, setTab] = useState("overview");
  const t = tournament;
  const c = competition;

  const allMatches = useMemo(() => {
    const out = [];
    if (pools) {
        pools.forEach((p) => {
            const matches = poolMatches ? poolMatches.filter(m => m.id.startsWith(p.poolName + "-")) : [];
            matches.forEach((m) => out.push({ ...m, phase: "pool", phaseName: p.poolName, poolName: p.poolName }));
        });
    }
    if (bracket && bracket.rounds) {
        bracket.rounds.forEach((round, ri) => {
            round.forEach((m) => out.push({ ...m, phase: "bracket", round: window.roundLabel(ri, bracket.rounds.length), phaseName: window.roundLabel(ri, bracket.rounds.length) }));
        });
    }
    return out;
  }, [pools, poolMatches, bracket]);

  const liveMatches = allMatches.filter((m) => m.status === "running");
  const upcomingMatches = allMatches.filter((m) => m.status === "scheduled" && m.sideA && m.sideB).slice(0, 3);
  const recentMatches = allMatches.filter((m) => m.status === "completed" && m.winner).slice(-5).reverse();

  // pick a "my match" — placeholder for now
  const myPlayer = null;
  const myUpcoming = null;

  const hasPools = !!pools && pools.length > 0;
  const hasBracket = !!bracket && bracket.rounds && bracket.rounds.length > 0;
  const tabs = [
    { id: "overview", label: "Overview" },
    hasPools ? { id: "pools", label: "Pools" } : null,
    hasBracket ? { id: "bracket", label: "Bracket" } : null,
    c.status === "completed" ? { id: "results", label: "Results" } : null,
  ].filter(Boolean);

  const currentMatch = useMemo(() => {
      const live = allMatches.find((m) => m.status === "running" && m.sideA && m.sideB);
      if (live) return live;
      const sched = allMatches.filter((m) => m.status === "scheduled" && m.sideA && m.sideB);
      sched.sort((a, b) => (a.scheduledAt || "99:99").localeCompare(b.scheduledAt || "99:99"));
      return sched[0] || null;
  }, [allMatches]);

  const [bracketScrollTarget, setBracketScrollTarget] = useState(null);
  const bracketScrollRef = useRefV(null);

  React.useEffect(() => {
    if (tab === "bracket" && currentMatch) {
      setBracketScrollTarget(currentMatch.id + "::" + Date.now());
    }
  }, [tab, currentMatch?.id]);

  return (
    <div className="viewer">
      <div className="viewer__shell">
        <div className="viewer__head">
          <button className="viewer__back" onClick={onBack} aria-label="Back">←</button>
          <div className="viewer__title-block">
            <div className="viewer__eyebrow">{t.name}</div>
            <div className="viewer__title">{c.name}</div>
            <div className="viewer__sub">{competitionKindLabel(c)}</div>
          </div>
          <StatusBadge status={c.status} />
        </div>
        <div className="viewer__tabs">
          {tabs.map((tb) => (
            <button key={tb.id} className={`viewer__tab ${tab === tb.id ? "is-active" : ""}`} onClick={() => setTab(tb.id)}>
              {tb.label}
            </button>
          ))}
        </div>
        <div className="viewer__body">
          {tab === "overview" && (
            <ViewerOverview
              c={c}
              myPlayer={myPlayer}
              myUpcoming={myUpcoming}
              liveMatches={liveMatches}
              upcomingMatches={upcomingMatches}
              recentMatches={recentMatches}
              tweaks={tweaks}
            />
          )}
          {tab === "bracket" && bracket && (
            <div style={{ marginLeft: -16, marginRight: -16 }}>
              <div ref={bracketScrollRef} className="bracket-canvas" style={{ borderRadius: 0, borderLeft: 0, borderRight: 0 }}>
                <div className="bracket-canvas__inner" style={{ padding: 18 }}>
                  <window.BracketTree
                    rounds={bracket.rounds}
                    variant={tweaks.cardVariant}
                    showDojo={tweaks.showDojo}
                    highlightedMatchId={currentMatch?.id}
                    autoScrollMatchId={bracketScrollTarget}
                    scrollContainerRef={bracketScrollRef}
                  />
                </div>
              </div>
            </div>
          )}
          {tab === "pools" && hasPools && (
            <PoolsViewer pools={pools} standings={standings} tweaks={tweaks} />
          )}
          {tab === "results" && c.status === "completed" && bracket && (
            <ResultsViewer c={c} bracket={bracket} />
          )}
        </div>
      </div>
    </div>
  );
}

function ViewerOverview({ c, myPlayer, myUpcoming, liveMatches, upcomingMatches, recentMatches, tweaks }) {
  if (c.status === "setup") {
    return (
      <div className="empty" style={{ padding: 32 }}>
        <div className="icon">⏳</div>
        <h3>Not started yet</h3>
        <div style={{ fontSize: 13 }}>Starts at {c.startTime}. Check back when the competition begins.</div>
      </div>
    );
  }
  return (
    <div>
      {myUpcoming && myPlayer ? (
        <div className="my-match">
          <div className="my-match__lbl">Your next match</div>
          <div className="my-match__name">{myPlayer.name}</div>
          <div className="my-match__round">
            {myUpcoming.phase === "pool" ? myUpcoming.poolName : myUpcoming.round}
            {myUpcoming.status === "in_progress" ? " · LIVE NOW" : ""}
          </div>
          <div className="my-match__row">
            <div className="my-match__chip">
              <span className="l">Court</span>
              <span className="v">Shiaijo {myUpcoming.court}</span>
            </div>
            <div className="my-match__chip">
              <span className="l">Time</span>
              <span className="v">{myUpcoming.scheduledAt || "TBA"}</span>
            </div>
          </div>
          {(() => {
            const opp = myUpcoming.sideA?.id === myPlayer.id ? myUpcoming.sideB : myUpcoming.sideA;
            return opp ? (
              <div className="my-match__opp">
                <div className="l">vs Opponent</div>
                <div className="n">{opp.name}</div>
                {tweaks.showDojo ? <div className="d">{opp.dojo}</div> : null}
              </div>
            ) : null;
          })()}
        </div>
      ) : null}

      {liveMatches.length > 0 && (
        <>
          <div className="section-title" style={{ display: "flex", alignItems: "center", gap: 6 }}>
            <span className="dot dot--live"></span> LIVE NOW
          </div>
          <div className="vsched">
            {liveMatches.map((m) => <VSchedItem key={m.id} m={m} tweaks={tweaks} />)}
          </div>
        </>
      )}

      <div className="section-title">Up next · {upcomingMatches.length}</div>
      <div className="vsched">
        {upcomingMatches.length === 0 ? <div className="empty"><h3>Nothing scheduled</h3></div> :
          upcomingMatches.map((m) => <VSchedItem key={m.id} m={m} tweaks={tweaks} />)}
      </div>

      {recentMatches.length > 0 && (
        <>
          <div className="section-title">Recent results</div>
          <div className="vsched">
            {recentMatches.map((m) => <VSchedItem key={m.id} m={m} tweaks={tweaks} />)}
          </div>
        </>
      )}
    </div>
  );
}

function VSchedItem({ m, tweaks, showCompetition }) {
  const aWin = m.winner && m.sideA && m.winner.id === m.sideA.id;
  const bWin = m.winner && m.sideB && m.winner.id === m.sideB.id;
  return (
    <div className={`vsched-item ${m.status === "in_progress" ? "vsched-item--live" : ""}`}>
      <div className="vsched-item__head">
        <span className="vsched-item__time">{m.scheduledAt || "—"}</span>
        <span className="vsched-item__court">SHIAIJO {m.court}</span>
        {showCompetition && m.compName ? <span>· {m.compName}</span> : null}
        {m.phase === "pool" ? <span>· {m.poolName}</span> : <span>· {m.round || ""}</span>}
        {m.status === "in_progress" && <span className="bc-live" style={{ marginLeft: "auto" }}>● LIVE</span>}
        {m.status === "complete" && <span style={{ marginLeft: "auto", color: "var(--ink-3)" }}>Final</span>}
      </div>
      <div className="vsched-item__players">
        <div className={`vsched-item__side ${aWin ? "vsched-item__side--w" : ""}`}>
          <span className="n">{m.sideA?.name || "TBD"}</span>
          {tweaks.showDojo && m.sideA?.dojo ? <span className="d">{m.sideA.dojo}</span> : null}
        </div>
        {m.score?.type === "ippon" ? (
          <span className="vsched-item__score">
            {aWin ? m.score.winnerPts : m.score.loserPts}–{bWin ? m.score.winnerPts : m.score.loserPts}
          </span>
        ) : (
          <span className="vsched-item__vs">vs</span>
        )}
        <div className={`vsched-item__side ${bWin ? "vsched-item__side--w" : ""}`} style={{ textAlign: "right" }}>
          <span className="n">{m.sideB?.name || "TBD"}</span>
          {tweaks.showDojo && m.sideB?.dojo ? <span className="d">{m.sideB.dojo}</span> : null}
        </div>
      </div>
    </div>
  );
}

function PoolsViewer({ pools, standings, tweaks }) {
  if (!pools || pools.length === 0) {
    return <div className="empty"><div className="icon">⏳</div><h3>Pools not drawn yet</h3></div>;
  }
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
      {pools.map((pool) => {
        const poolStandings = standings ? standings[pool.poolName] : null;
        return (
          <div key={pool.poolName} className="pool" style={{ padding: 14 }}>
            <div className="pool__head">
              <div>
                <div className="pool__name">{pool.poolName}</div>
              </div>
            </div>
            <table className="pool__table">
              <thead>
                <tr><th>#</th><th>Player</th><th className="num">W</th><th className="num">L</th><th className="num">G</th><th className="num">T</th></tr>
              </thead>
              <tbody>
                {poolStandings && poolStandings.length > 0 ? poolStandings.map((s, i) => (
                  <tr key={s.player.name}>
                    <td style={{ color: "var(--ink-3)", fontFamily: "var(--font-mono)" }}>{i + 1}</td>
                    <td>
                      <div style={{ fontWeight: 500 }}>{s.player.name}</div>
                      {tweaks.showDojo ? <div style={{ fontSize: 11, color: "var(--ink-3)" }}>{s.player.dojo}</div> : null}
                    </td>
                    <td className="num">{s.wins}</td>
                    <td className="num">{s.losses}</td>
                    <td className="num">{s.ipponsGiven}</td>
                    <td className="num">{s.ipponsTaken}</td>
                  </tr>
                )) : pool.players.map((p, i) => (
                  <tr key={p.name}>
                    <td style={{ color: "var(--ink-3)", fontFamily: "var(--font-mono)" }}>{i + 1}</td>
                    <td>
                      <div style={{ fontWeight: 500 }}>{p.name}</div>
                      {tweaks.showDojo ? <div style={{ fontSize: 11, color: "var(--ink-3)" }}>{p.dojo}</div> : null}
                    </td>
                    <td className="num">—</td><td className="num">—</td><td className="num">—</td><td className="num">—</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        );
      })}
    </div>
  );
}

// Reusable multi-player filter — used by both viewer & admin schedule pages.
// Picks any number of participants/teams across all competitions; matches are
// kept if they involve ANY of the picked sides. Free-text dojo search works in parallel.
function PlayerMultiFilter({ tournament, picked, setPicked, dojoText, setDojoText }) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const ref = useRefV(null);

  // build a deduped roster across all competitions
  const roster = useMemo(() => {
    const map = new Map();
    (tournament.competitions || []).forEach((c) => {
      c.players.forEach((p) => {
        const key = p.id;
        if (!map.has(key)) map.set(key, { ...p, comps: [c.name] });
        else map.get(key).comps.push(c.name);
      });
    });
    return Array.from(map.values());
  }, [tournament]);

  const q = query.trim().toLowerCase();
  const matches = q ? roster.filter((p) =>
    p.name.toLowerCase().includes(q) || (p.dojo || "").toLowerCase().includes(q)
  ).slice(0, 30) : roster.slice(0, 30);

  React.useEffect(() => {
    const onDoc = (e) => { if (ref.current && !ref.current.contains(e.target)) setOpen(false); };
    document.addEventListener("mousedown", onDoc);
    return () => document.removeEventListener("mousedown", onDoc);
  }, []);

  const toggle = (p) => {
    if (picked.find((x) => x.id === p.id)) setPicked(picked.filter((x) => x.id !== p.id));
    else setPicked([...picked, p]);
  };

  return (
    <div className="pmf" ref={ref}>
      <div className="pmf__bar" onClick={() => setOpen(true)}>
        {picked.length === 0 && !dojoText ? (
          <span className="pmf__placeholder">Filter by player, team, or dojo…</span>
        ) : null}
        {picked.map((p) => (
          <span key={p.id} className="pmf__chip">
            {p.name}
            <button onClick={(e) => { e.stopPropagation(); toggle(p); }} aria-label="Remove">×</button>
          </span>
        ))}
        {dojoText ? (
          <span className="pmf__chip pmf__chip--text">
            “{dojoText}”
            <button onClick={(e) => { e.stopPropagation(); setDojoText(""); }} aria-label="Remove">×</button>
          </span>
        ) : null}
        <input
          className="pmf__input"
          placeholder={picked.length || dojoText ? "Add more…" : ""}
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onFocus={() => setOpen(true)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && query.trim()) {
              setDojoText(query.trim());
              setQuery("");
            } else if (e.key === "Backspace" && !query && picked.length) {
              setPicked(picked.slice(0, -1));
            }
          }}
        />
      </div>
      {open && (
        <div className="pmf__dropdown">
          <div className="pmf__dropdown-head">
            {q ? `${matches.length} match${matches.length === 1 ? "" : "es"}` : `${roster.length} participants — type to search`}
            {(picked.length > 0 || dojoText) && (
              <button className="btn btn--ghost btn--sm" onClick={() => { setPicked([]); setDojoText(""); setQuery(""); }}>Clear all</button>
            )}
          </div>
          {q && (
            <button className="pmf__option pmf__option--text" onClick={() => { setDojoText(query.trim()); setQuery(""); }}>
              <span>Match text “<b>{query}</b>” in any name/dojo</span>
            </button>
          )}
          {matches.map((p) => {
            const isPicked = !!picked.find((x) => x.id === p.id);
            return (
              <button key={p.id} className={`pmf__option ${isPicked ? "is-picked" : ""}`} onClick={() => toggle(p)}>
                <span className="pmf__check">{isPicked ? "✓" : ""}</span>
                <span className="pmf__opt-body">
                  <span className="pmf__opt-name">{p.name}</span>
                  <span className="pmf__opt-dojo">{p.dojo}{p.comps?.length ? ` · ${p.comps.join(", ")}` : ""}</span>
                </span>
              </button>
            );
          })}
          {matches.length === 0 && !q && <div className="pmf__empty">No participants yet.</div>}
        </div>
      )}
    </div>
  );
}

function applyFilters(matches, picked, dojoText, compFilter) {
  const ids = new Set(picked.map((p) => p.id));
  const dt = (dojoText || "").trim().toLowerCase();
  return matches.filter((m) => {
    if (compFilter !== "all" && m.compId !== compFilter) return false;
    if (ids.size > 0) {
      const hit = (m.sideA && ids.has(m.sideA.id)) || (m.sideB && ids.has(m.sideB.id));
      if (!hit) return false;
    }
    if (dt) {
      const hit = [m.sideA?.name, m.sideB?.name, m.sideA?.dojo, m.sideB?.dojo].some((s) => (s || "").toLowerCase().includes(dt));
      if (!hit) return false;
    }
    return true;
  });
}

function matchHighlightedBy(m, picked, dojoText) {
  const ids = new Set(picked.map((p) => p.id));
  if (ids.size > 0 && ((m.sideA && ids.has(m.sideA.id)) || (m.sideB && ids.has(m.sideB.id)))) return true;
  const dt = (dojoText || "").trim().toLowerCase();
  if (dt && [m.sideA?.name, m.sideB?.name, m.sideA?.dojo, m.sideB?.dojo].some((s) => (s || "").toLowerCase().includes(dt))) return true;
  return false;
}

window.PlayerMultiFilter = PlayerMultiFilter;
window.applyFilters = applyFilters;
window.matchHighlightedBy = matchHighlightedBy;

// Tournament-wide schedule (across competitions) — court swimlanes + filter
function ScheduleViewer({ tournament, tweaks }) {
  const allMatches = useMemo(() => tournamentMatches(tournament).filter((m) => m.sideA && m.sideB), [tournament]);
  const courts = tournament.courts;
  const [picked, setPicked] = useState([]);
  const [dojoText, setDojoText] = useState("");
  const [compFilter, setCompFilter] = useState("all");

  const filtered = applyFilters(allMatches, picked, dojoText, compFilter);

  // Group by court
  const byCourt = {};
  courts.forEach((cc) => byCourt[cc] = []);
  filtered.forEach((m) => { (byCourt[m.court] = byCourt[m.court] || []).push(m); });
  Object.values(byCourt).forEach((list) => list.sort((a, b) => {
    const order = { in_progress: 0, scheduled: 1, pending: 2, complete: 3 };
    if (order[a.status] !== order[b.status]) return order[a.status] - order[b.status];
    return (a.scheduledAt || "99:99").localeCompare(b.scheduledAt || "99:99");
  }));

  const matchHasFilter = (m) => matchHighlightedBy(m, picked, dojoText);
  const hasAnyFilter = picked.length > 0 || dojoText || compFilter !== "all";

  return (
    <div className="tw-sched">
      <div className="tw-sched__filters">
        <PlayerMultiFilter tournament={tournament} picked={picked} setPicked={setPicked} dojoText={dojoText} setDojoText={setDojoText} />
        <select className="input" style={{ width: "auto", minWidth: 160 }} value={compFilter} onChange={(e) => setCompFilter(e.target.value)}>
          <option value="all">All competitions</option>
          {(tournament.competitions || []).map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
        </select>
        {hasAnyFilter && (
          <button className="btn btn--ghost btn--sm" onClick={() => { setPicked([]); setDojoText(""); setCompFilter("all"); }}>Clear</button>
        )}
        <span style={{ marginLeft: "auto", fontSize: 12, color: "var(--ink-3)" }}>{filtered.length} of {allMatches.length} matches</span>
      </div>

      <div className="tw-courts">
        {courts.map((cc) => {
          const list = byCourt[cc] || [];
          const liveOn = list.find((m) => m.status === "in_progress");
          return (
            <div key={cc} className="tw-court">
              <div className="tw-court__head">
                <div>
                  <div className="tw-court__title">SHIAIJO {cc}</div>
                  <div className="tw-court__sub">{list.length} match{list.length === 1 ? "" : "es"}{liveOn ? " · live now" : ""}</div>
                </div>
                {liveOn && <span className="bc-live">● LIVE</span>}
              </div>
              <div className="tw-court__list">
                {list.length === 0 ? (
                  <div style={{ fontSize: 12, color: "var(--ink-3)", padding: "20px 8px", textAlign: "center" }}>No matches</div>
                ) : list.map((m) => (
                  <TWMatch key={m.compId + m.id} m={m} highlight={matchHasFilter(m)} tweaks={tweaks} />
                ))}
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}

function TWMatch({ m, highlight, tweaks }) {
  const aWin = m.winner && m.sideA && m.winner.id === m.sideA.id;
  const bWin = m.winner && m.sideB && m.winner.id === m.sideB.id;
  return (
    <div className={`tw-match ${m.status === "in_progress" ? "tw-match--live" : ""} ${m.status === "complete" ? "tw-match--done" : ""} ${highlight ? "tw-match--highlight" : ""}`}>
      <div>
        <div className="tw-match__time">{m.scheduledAt || "—"}</div>
        <div className="tw-match__phase">{m.phase === "pool" ? m.poolName : m.round}</div>
      </div>
      <div className="tw-match__players">
        <div className={`tw-match__name ${aWin ? "tw-match__name--w" : ""}`}>{m.sideA?.name || "TBD"}</div>
        <div className={`tw-match__name ${bWin ? "tw-match__name--w" : ""}`}>{m.sideB?.name || "TBD"}</div>
        <div className="tw-match__comp">{m.compName}</div>
      </div>
      <div style={{ textAlign: "right", fontFamily: "var(--font-mono)", fontWeight: 700, fontSize: 13 }}>
        {m.status === "complete" && m.score?.type === "ippon" && `${m.score.winnerPts}–${m.score.loserPts}`}
        {m.status === "complete" && m.score?.type === "bye" && <span style={{ fontSize: 10, color: "var(--ink-3)" }}>BYE</span>}
        {m.status === "in_progress" && <span className="bc-live">●</span>}
      </div>
    </div>
  );
}

function ResultsViewer({ c, bracket }) {
  const final = bracket.rounds[bracket.rounds.length - 1][0];
  const sf = bracket.rounds[bracket.rounds.length - 2] || [];
  const champion = final.winner;
  const runnerUp = final.winner === final.sideA ? final.sideB : final.sideA;
  const third = sf.map((m) => m.winner === m.sideA ? m.sideB : m.sideA).filter(Boolean);
  return (
    <div>
      <div className="podium">
        {third[0] && (
          <div className="podium-step podium-step--3">
            <div className="place">3</div>
            <div className="name">{third[0]}</div>
          </div>
        )}
        {champion && (
          <div className="podium-step podium-step--1">
            <div style={{ fontSize: 28 }}>🏆</div>
            <div className="place">1</div>
            <div className="name">{champion}</div>
          </div>
        )}
        {runnerUp && (
          <div className="podium-step podium-step--2">
            <div className="place">2</div>
            <div className="name">{runnerUp}</div>
          </div>
        )}
        {third[1] && (
          <div className="podium-step podium-step--3" style={{ order: 4 }}>
            <div className="place">3</div>
            <div className="name">{third[1]}</div>
          </div>
        )}
      </div>
      <div className="card" style={{ marginTop: 16 }}>
        <div className="card__title" style={{ marginBottom: 10 }}>Final match</div>
        <window.MatchCard match={final} variant={1} showDojo={true} />
      </div>
    </div>
  );
}

// Tournament-wide schedule wrapper for the viewer (its own screen)
function ViewerSchedule({ tournament, onBack, tweaks }) {
  return (
    <div className="viewer">
      <div className="viewer__shell">
        <div className="viewer__head">
          <button className="viewer__back" onClick={onBack} aria-label="Back">←</button>
          <div className="viewer__title-block">
            <div className="viewer__eyebrow">{tournament.name}</div>
            <div className="viewer__title">Schedule</div>
            <div className="viewer__sub">All matches across courts and competitions</div>
          </div>
        </div>
        <div className="viewer__body">
          <ScheduleViewer tournament={tournament} tweaks={tweaks} />
        </div>
      </div>
    </div>
  );
}

window.ViewerHome = ViewerHome;
window.ViewerCompetition = ViewerCompetition;
window.ViewerSchedule = ViewerSchedule;
window.ScheduleViewer = ScheduleViewer;
window.competitionKindLabel = competitionKindLabel;
window.compMatches = compMatches;
window.tournamentMatches = tournamentMatches;
window.currentMatchOf = currentMatchOf;
