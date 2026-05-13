// Viewer side — mobile-first. Single tournament. Shows competitions as the home;
// each competition opens to its own Overview/Bracket/Pools/Schedule/Results.

const { useState, useMemo, useRef: useRefV } = React;
const StatusBadge = window.StatusBadge;
const formatDate = window.formatDate;

function competitionKindLabel(c) {
  // c may be a kind string for backward-compat or a competition obj
  if (typeof c === "string") return c === "team" ? "Teams" : "Individual";
  const base = c.kind === "team" ? "Teams" : "Individual";
  if (c.gender === "M") return `${base} · Men`;
  if (c.gender === "F") return `${base} · Women`;
  return base;
}

const pluralize = window.pluralize;

function compMatches(c) {
  const out = [];

  // Setup-mode: no backend data yet, return empty (no client-side preview)
  if (c.status === "setup") return out;

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
  return (t.competitions || [])
    .filter(c => c.status !== "setup")
    .flatMap(compMatches);
}

// Next match to be played in this competition (live first, else first scheduled in time order)
function currentMatchOf(c) {
  const ms = compMatches(c);
  const live = ms.find((m) => m.status === "running" && m.sideA && m.sideB);
  if (live) return live;
  const sched = ms.filter((m) => m.status === "scheduled" && m.sideA && m.sideB);
  sched.sort((a, b) => (a.scheduledAt || "99:99").localeCompare(b.scheduledAt || "99:99"));
  return sched[0] || null;
}

function ViewerHome({ tournament, onSelectCompetition, onAdminClick, onOpenSchedule }) {
  const t = tournament;
  const comps = t.competitions || [];
  const compsByDate = useMemo(() => {
    const map = {};
    comps.forEach((c) => {
      const d = c.date || t.date || "";
      if (!map[d]) map[d] = [];
      map[d].push(c);
    });
    return map;
  }, [comps, t.date]);
  const dates = Object.keys(compsByDate).sort();

  const [courtFilter, setCourtFilter] = useState("all");
  const [selectedMatch, setSelectedMatch] = useState(null);

  // global "across-all-competitions" lists for the home page
  const allMatches = useMemo(() => tournamentMatches(t), [t]);
  const liveCompIds = useMemo(() => new Set((t.competitions || []).filter(c => c.status !== "setup").map(c => c.id)), [t.competitions]);
  const live = allMatches.filter((m) => m.status === "running" && liveCompIds.has(m.compId) && (courtFilter === "all" || m.court === courtFilter));
  let upNext = allMatches.filter((m) => m.status === "scheduled" && m.sideA && m.sideB && liveCompIds.has(m.compId) && (courtFilter === "all" || m.court === courtFilter));
  if (courtFilter === "all") upNext = upNext.slice(0, 3);

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
          <div style={{ marginBottom: 16, display: "flex", justifyContent: "flex-end" }}>
             <select className="input" style={{ width: "auto" }} value={courtFilter} onChange={(e) => setCourtFilter(e.target.value)}>
               <option value="all">All Shiaijo</option>
               {(t.courts || ["A"]).map(c => <option key={c} value={c}>Shiaijo {c}</option>)}
             </select>
          </div>

          {live.length > 0 && (
            <div className="hero-live">
              <div className="hero-live__lbl"><span className="dot dot--live"></span> LIVE NOW · {pluralize(live.length, "match", "matches")}</div>
              <div className="vsched" style={{ marginTop: 8 }}>
                {live.slice(0, 3).map((m) => <VSchedItem key={m.compId + m.id} m={m} tweaks={{ showDojo: true }} showCompetition onClick={() => setSelectedMatch(m)} />)}
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
              <div className="vlist-item__rowsub">{pluralize(allMatches.filter((m) => m.sideA && m.sideB).length, "match", "matches")} across {pluralize(tournament.courts.length, "shiaijo (court)", "shiaijo (courts)")} · search by player or team</div>
            </div>
            <span className="vlist-item__rowchev">→</span>
          </button>

          {dates.length === 0 ? (
            <>
              <div className="section-title">Competitions</div>
              <div className="vlist">
                <div className="empty">
                  <div className="icon">⏳</div>
                  <h3>No competitions yet</h3>
                  <div style={{ fontSize: 13 }}>Check back soon for the tournament schedule and live updates.</div>
                </div>
              </div>
            </>
          ) : dates.map((d) => (
            <div key={d}>
              <div className="section-title">{formatDate(d)}</div>
              <div className="vlist">
                {compsByDate[d].map((c) => {
                  const matches = compMatches(c).filter((m) => m.sideA && m.sideB);
                  const total = matches.length;
                  const done = matches.filter((m) => m.status === "completed").length;
                  const liveCount = matches.filter((m) => m.status === "running").length;
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
                        <StatusBadge status={c.status} showLiveDot />
                      </div>
                      {c.status !== "setup" && total > 0 && (
                        <div className="vlist-item__progress">
                          <div className="vlist-item__bar"><div style={{ width: pct + "%" }}></div></div>
                          <div className="vlist-item__pct">
                            {liveCount > 0 ? <span style={{ color: "var(--red)", fontWeight: 600 }}>● {liveCount} live</span> : pluralize(done, "match", "matches") + " / " + total}
                          </div>
                        </div>
                      )}
                    </button>
                  );
                })}
              </div>
            </div>
          ))}

          {upNext.length > 0 && (
            <>
              <div className="section-title" style={{ marginTop: 20 }}>Up next · {upNext.length}</div>
              <div className="vsched">
                {upNext.map((m) => <VSchedItem key={m.compId + m.id} m={m} tweaks={{ showDojo: true }} showCompetition onClick={() => setSelectedMatch(m)} />)}
              </div>
            </>
          )}
        </div>
      </div>
      {selectedMatch && <MatchViewerModal match={selectedMatch} onClose={() => setSelectedMatch(null)} />}
    </div>
  );
}

function ViewerCompetition({ _tournament, competition, pools, poolMatches, standings, bracket, onBack, _onAdminClick, tweaks }) {
  const [tab, setTab] = useState("overview");
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

  const liveMatches = allMatches.filter((m) => m.status === "running" && m.sideA && m.sideB);
  const upcomingMatches = allMatches.filter((m) => m.status === "scheduled" && m.sideA && m.sideB).slice(0, 3);
  const recentMatches = allMatches.filter((m) => m.status === "completed" && m.winner).slice(-5).reverse();

  // pick a "my match" — placeholder for now
  const myPlayer = null;
  const myUpcoming = null;

  const derivedBracket = useMemo(() => {
    if (bracket && bracket.rounds && bracket.rounds.length > 0) return bracket;
    if (c.format === "pools" && pools && pools.length > 0) {
      const placeholders = [];
      const winners = c.poolWinners || 2;
      pools.forEach(p => {
        for(let i=0; i<winners; i++) {
           let rank = i===0?'1st':i===1?'2nd':i===2?'3rd':(i+1)+'th';
           placeholders.push({ id: `tbd-${p.poolName}-${i}`, name: `${p.poolName} ${rank}`, dojo: "", seed: null });
        }
      });
      return { rounds: window.buildBracket(placeholders, c.courts) };
    }
    return null;
  }, [bracket, c, pools]);

  const hasPools = !!pools && pools.length > 0;
  const hasBracket = !!derivedBracket && derivedBracket.rounds && derivedBracket.rounds.length > 0;
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
  const [selectedMatch, setSelectedMatch] = useState(null);

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
            <div className="viewer__eyebrow">
              {c.date && <span style={{ fontWeight: 600 }}>{formatDate(c.date)}</span>}
              {c.date && c.startTime && " at "}
              {c.startTime} · {c.courts.join(", ")}
            </div>
            <div className="viewer__title">{c.name}</div>
            <div className="viewer__sub">{competitionKindLabel(c)}</div>
          </div>
          <StatusBadge status={c.status} showLiveDot />
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
              currentMatch={currentMatch}
              liveMatches={liveMatches}
              upcomingMatches={upcomingMatches}
              recentMatches={recentMatches}
              tweaks={tweaks}
              onMatchClick={setSelectedMatch}
            />
          )}
          {tab === "bracket" && derivedBracket && (
            <div style={{ marginLeft: -16, marginRight: -16 }}>
              <div ref={bracketScrollRef} className="bracket-canvas" style={{ borderRadius: 0, borderLeft: 0, borderRight: 0 }}>
                <div className="bracket-canvas__inner" style={{ padding: 18 }}>
                  <window.BracketTree
                    rounds={derivedBracket.rounds}
                    variant={tweaks.cardVariant}
                    showDojo={tweaks.showDojo}
                    highlightedMatchId={currentMatch?.id}
                    autoScrollMatchId={bracketScrollTarget}
                    scrollContainerRef={bracketScrollRef}
                    onMatchClick={(m) => setSelectedMatch(m)}
                  />
                </div>
              </div>
            </div>
          )}
          {tab === "pools" && hasPools && (
            <PoolsViewer pools={pools} standings={standings} poolMatches={poolMatches} tweaks={tweaks} competition={c} onMatchClick={setSelectedMatch} />
          )}
          {tab === "results" && c.status === "completed" && derivedBracket && (
            <ResultsViewer c={c} bracket={derivedBracket} />
          )}
        </div>
      </div>
      {selectedMatch && <MatchViewerModal match={selectedMatch} onClose={() => setSelectedMatch(null)} />}
    </div>
  );
}

// Inline match detail card — shown directly on the page (no modal needed).
function MatchDetailCard({ match, onClose }) {
  if (!match) return null;
  const isTeam = match.compKind === "team" || match.teamSize > 0;
  const aName = match.sideA?.name || "TBD";
  const bName = match.sideB?.name || "TBD";
  const aWin = match.winner?.id === match.sideA?.id && match.winner?.id;
  const bWin = match.winner?.id === match.sideB?.id && match.winner?.id;
  const isLive = match.status === "running";
  const isDone = match.status === "completed";

  return (
    <div className="match-detail-card">
      <div className="match-detail-card__head">
        <div className="match-detail-card__meta">
          <span>Shiaijo {match.court}</span>
          <span>·</span>
          <span>{match.phase === "pool" ? match.poolName : (match.round || "")}</span>
          {match.scheduledAt && <><span>·</span><span>{match.scheduledAt}</span></>}
        </div>
        <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
          {isLive && <span className="bc-live">● LIVE</span>}
          {isDone && <span style={{ fontSize: 11, color: "var(--ink-3)", fontWeight: 600 }}>FINAL</span>}
          {onClose && <button className="match-detail-card__close" onClick={onClose} aria-label="Close">×</button>}
        </div>
      </div>

      <div className="match-detail-card__players">
        <div className={`match-detail-card__side ${bWin ? "match-detail-card__side--win" : ""}`}>
          <span className="match-detail-card__color-badge match-detail-card__color-badge--shiro">SHIRO</span>
          <span className="match-detail-card__name">{bName}</span>
        </div>
        <div className="match-detail-card__score">
          {isDone ? (() => {
            const scoreStr = window.formatIpponsScore(match.ipponsB, match.ipponsA, match.score, match.decision);
            return <span>{scoreStr || "—"}</span>;
          })() : <span className="match-detail-card__vs">vs</span>}
        </div>
        <div className={`match-detail-card__side match-detail-card__side--right ${aWin ? "match-detail-card__side--win" : ""}`}>
          <span className="match-detail-card__name">{aName}</span>
          <span className="match-detail-card__color-badge match-detail-card__color-badge--aka">AKA</span>
        </div>
      </div>

      {isDone && !isTeam && (
        <div className="match-detail-card__ippons">
          <div className="match-detail-card__ippons-side">
            <span className="match-detail-card__ippons-val">{(match.ipponsB || []).filter(x => x && x !== "•").join("") || "—"}</span>
            {match.hansokuB > 0 && <span className="match-detail-card__fouls">Fouls: {match.hansokuB}</span>}
          </div>
          <div className="match-detail-card__ippons-center">
            {match.score?.type === "hantei" && <span className="match-detail-card__decision">Hantei</span>}
            {(match.score?.type === "hikiwake" || match.decision === "hikewake") && <span className="match-detail-card__decision">Draw</span>}
          </div>
          <div className="match-detail-card__ippons-side match-detail-card__ippons-side--right">
            <span className="match-detail-card__ippons-val">{(match.ipponsA || []).filter(x => x && x !== "•").join("") || "—"}</span>
            {match.hansokuA > 0 && <span className="match-detail-card__fouls">Fouls: {match.hansokuA}</span>}
          </div>
        </div>
      )}

      {isDone && isTeam && match.subResults && match.subResults.length > 0 && (
        <div className="match-detail-card__team-subs">
          {match.subResults.map((sub, i) => {
            const sA = (sub.ipponsA || []).filter(x => x && x !== "•").join("") || "—";
            const sB = (sub.ipponsB || []).filter(x => x && x !== "•").join("") || "—";
            return (
              <div key={i} className="match-detail-card__sub-row">
                <span className="match-detail-card__sub-score">{sB}</span>
                <span className="match-detail-card__sub-pos">Match {sub.position || i + 1}</span>
                <span className="match-detail-card__sub-score match-detail-card__sub-score--right">{sA}</span>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}

function ViewerOverview({ c, myPlayer, myUpcoming, currentMatch, liveMatches, upcomingMatches, recentMatches, tweaks, onMatchClick }) {
  const [expandedMatchId, setExpandedMatchId] = useState(null);

  if (c.status === "setup") {
    return (
      <div className="empty" style={{ padding: 32 }}>
        <div className="icon">⏳</div>
        <h3>Not started yet</h3>
        <div style={{ fontSize: 13 }}>Starts at {c.startTime}. Check back when the competition begins.</div>
      </div>
    );
  }

  const handleMatchClick = (m) => {
    setExpandedMatchId(prev => prev === m.id ? null : m.id);
    if (onMatchClick) onMatchClick(m);
  };

  return (
    <div>
      {myUpcoming && myPlayer ? (
        <div className="my-match">
          <div className="my-match__lbl">Your next match</div>
          <div className="my-match__name">{myPlayer.name}</div>
          <div className="my-match__round">
            {myUpcoming.phase === "pool" ? myUpcoming.poolName : myUpcoming.round}
            {myUpcoming.status === "running" ? " · LIVE NOW" : ""}
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

      {/* Current match — shown inline, before Up Next */}
      {currentMatch && currentMatch.status === "running" && (
        <div style={{ marginBottom: 12 }}>
          <div className="section-title" style={{ display: "flex", alignItems: "center", gap: 6 }}>
            <span className="dot dot--live"></span> ON NOW
          </div>
          <MatchDetailCard match={currentMatch} />
        </div>
      )}

      {/* Live matches beyond the single current match */}
      {liveMatches.length > 1 && (
        <>
          <div className="section-title" style={{ display: "flex", alignItems: "center", gap: 6 }}>
            <span className="dot dot--live"></span> LIVE NOW · {liveMatches.length}
          </div>
          <div className="vsched">
            {liveMatches.filter(m => !currentMatch || m.id !== currentMatch.id).map((m) => (
              <React.Fragment key={m.id}>
                <VSchedItem m={m} tweaks={tweaks} onClick={() => handleMatchClick(m)} />
                {expandedMatchId === m.id && <MatchDetailCard match={m} onClose={() => setExpandedMatchId(null)} />}
              </React.Fragment>
            ))}
          </div>
        </>
      )}

      {/* Up next */}
      {upcomingMatches.length > 0 && (
        <>
          <div className="section-title">Up next · {upcomingMatches.length}</div>
          <div className="vsched">
            {upcomingMatches.map((m) => (
              <React.Fragment key={m.id}>
                <VSchedItem m={m} tweaks={tweaks} onClick={() => handleMatchClick(m)} />
                {expandedMatchId === m.id && <MatchDetailCard match={m} onClose={() => setExpandedMatchId(null)} />}
              </React.Fragment>
            ))}
          </div>
        </>
      )}

      {/* No live or upcoming */}
      {!currentMatch && upcomingMatches.length === 0 && liveMatches.length === 0 && (
        <div className="empty" style={{ padding: 20 }}><h3>Nothing scheduled</h3></div>
      )}

      {recentMatches.length > 0 && (
        <>
          <div className="section-title">Recent results</div>
          <div className="vsched">
            {recentMatches.map((m) => (
              <React.Fragment key={m.id}>
                <VSchedItem m={m} tweaks={tweaks} onClick={() => setExpandedMatchId(prev => prev === m.id ? null : m.id)} />
                {expandedMatchId === m.id && <MatchDetailCard match={m} onClose={() => setExpandedMatchId(null)} />}
              </React.Fragment>
            ))}
          </div>
        </>
      )}
    </div>
  );
}

const VSchedItem = React.memo(({ m, tweaks, showCompetition, onClick }) => {
  const aWin = m.winner && m.sideA && m.winner.id === m.sideA.id;
  const bWin = m.winner && m.sideB && m.winner.id === m.sideB.id;
  const scoreStr = m.status === "completed" ? window.formatIpponsScore(m.ipponsB, m.ipponsA, m.score, m.decision) : null;
  return (
    <button className={`vsched-item ${m.status === "running" ? "vsched-item--live" : ""}`} onClick={onClick} style={{ textAlign: "left", width: "100%", border: "none", background: "none", cursor: onClick ? "pointer" : "default" }}>
      <div className="vsched-item__head">
        <span className="vsched-item__time">{m.scheduledAt || "—"}</span>
        <span className="vsched-item__court">SHIAIJO {m.court}</span>
        {showCompetition && m.compName ? <span>· {m.compName}</span> : null}
        {m.phase === "pool" ? <span>· {m.poolName}</span> : <span>· {m.round || ""}</span>}
        {m.status === "running" && <span className="bc-live" style={{ marginLeft: "auto" }}>● LIVE</span>}
        {m.status === "completed" && <span style={{ marginLeft: "auto", color: "var(--ink-3)" }}>Final</span>}
      </div>
      <div className="vsched-item__players">
        <div className={`vsched-item__side ${bWin ? "vsched-item__side--w" : ""}`} style={{ textAlign: "right" }}>
          <span className="n">{m.sideB?.name || "TBD"}</span>
          {tweaks.showDojo && m.sideB?.dojo ? <span className="d">{m.sideB.dojo}</span> : null}
          <span className="vsched-item__color-badge vsched-item__color-badge--shiro">SHIRO</span>
        </div>
        {m.status === "completed" && scoreStr ? (
          <span className="vsched-item__score">{scoreStr}</span>
        ) : m.status === "completed" ? (
          <span className="vsched-item__vs">—</span>
        ) : (
          <span className="vsched-item__vs">vs</span>
        )}
        <div className={`vsched-item__side ${aWin ? "vsched-item__side--w" : ""}`}>
          <span className="vsched-item__color-badge vsched-item__color-badge--aka">AKA</span>
          <span className="n">{m.sideA?.name || "TBD"}</span>
          {tweaks.showDojo && m.sideA?.dojo ? <span className="d">{m.sideA.dojo}</span> : null}
        </div>
      </div>
    </button>
  );
});
VSchedItem.displayName = "VSchedItem";

const PoolMatchRow = React.memo(({ m, onClick }) => {
  const aName = typeof m.sideA === "object" ? m.sideA?.name : m.sideA;
  const bName = typeof m.sideB === "object" ? m.sideB?.name : m.sideB;
  const winnerName = typeof m.winner === "object" ? m.winner?.name : m.winner;
  const aWin = winnerName && winnerName === aName;
  const bWin = winnerName && winnerName === bName;

  const scoreStr = m.status === "completed"
    ? window.formatIpponsScore(m.ipponsB, m.ipponsA, m.score, m.decision)
    : null;

  return (
    <button className="pool-match-row" onClick={onClick} style={{ textAlign: "left", width: "100%", border: "none", background: "none", cursor: onClick ? "pointer" : "default" }}>
      <div className={`pool-match-row__side pool-match-row__side--right ${bWin ? "pool-match-row__side--win" : ""}`}>
        <span className="pool-match-row__name">{bName}</span>
        <span className="pool-match-row__badge pool-match-row__badge--shiro">SHIRO</span>
      </div>
      <span className="pool-match-row__score">
        {m.status === "completed" ? (
          scoreStr || "—"
        ) : m.status === "running" ? (
          <span className="bc-live" style={{ fontSize: 10 }}>●</span>
        ) : "–"}
      </span>
      <div className={`pool-match-row__side ${aWin ? "pool-match-row__side--win" : ""}`}>
        <span className="pool-match-row__badge pool-match-row__badge--aka">AKA</span>
        <span className="pool-match-row__name">{aName}</span>
      </div>
    </button>
  );
});
PoolMatchRow.displayName = "PoolMatchRow";

// Round-robin matrix for a single pool. Rows = players (AKA), cols = players (SHIRO).
// Diagonal and upper triangle are empty; lower triangle shows result from match AKA vs SHIRO.
function PoolMatrix({ pool, matches, tweaks }) {
  const players = pool.players || [];
  if (players.length < 2) return null;

  // Build a lookup: (aName, bName) → match
  const matchMap = {};
  matches.forEach(m => {
    const aName = typeof m.sideA === "object" ? m.sideA?.name : m.sideA;
    const bName = typeof m.sideB === "object" ? m.sideB?.name : m.sideB;
    if (aName && bName) {
      matchMap[`${aName}||${bName}`] = m;
      matchMap[`${bName}||${aName}`] = m;
    }
  });

  const shortName = (p) => {
    const n = p.name || "";
    const parts = n.split(" ");
    return parts.length > 1 ? parts[0][0] + ". " + parts.slice(1).join(" ") : n;
  };

  return (
    <div className="pool-matrix-wrap">
      <table className="pool-matrix">
        <thead>
          <tr>
            <th className="pool-matrix__corner"></th>
            {players.map((p, i) => (
              <th key={p.name} className="pool-matrix__col-head" title={p.name}>{i + 1}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {players.map((rowPlayer, ri) => (
            <tr key={rowPlayer.name}>
              <td className="pool-matrix__row-head" title={rowPlayer.name}>
                <span className="pool-matrix__num">{ri + 1}</span>
                <span className="pool-matrix__pname">{tweaks.showDojo ? rowPlayer.name : shortName(rowPlayer)}</span>
              </td>
              {players.map((colPlayer, ci) => {
                if (ri === ci) return <td key={colPlayer.name} className="pool-matrix__cell pool-matrix__cell--self">—</td>;
                const m = matchMap[`${rowPlayer.name}||${colPlayer.name}`];
                if (!m) return <td key={colPlayer.name} className="pool-matrix__cell pool-matrix__cell--empty"></td>;

                const aName = typeof m.sideA === "object" ? m.sideA?.name : m.sideA;
                const winnerName = typeof m.winner === "object" ? m.winner?.name : m.winner;
                const rowIsAka = aName === rowPlayer.name;

                if (m.status !== "completed") {
                  return <td key={colPlayer.name} className={`pool-matrix__cell pool-matrix__cell--pending ${m.status === "running" ? "pool-matrix__cell--live" : ""}`}>
                    {m.status === "running" ? <span className="bc-live" style={{ fontSize: 9 }}>●</span> : "–"}
                  </td>;
                }

                const ipponsA = (m.ipponsA || []).filter(x => x && x !== "•");
                const ipponsB = (m.ipponsB || []).filter(x => x && x !== "•");
                const rowIppons = rowIsAka ? ipponsA : ipponsB;
                const rowWon = winnerName && winnerName === rowPlayer.name;
                const isDraw = m.decision === "hikewake" || m.score?.type === "hikiwake";

                let cellContent;
                if (isDraw) {
                  cellContent = <span className="pool-matrix__draw">X</span>;
                } else if (rowWon) {
                  cellContent = <span className="pool-matrix__win">{rowIppons.join("") || "W"}</span>;
                } else {
                  cellContent = <span className="pool-matrix__loss">{rowIppons.join("") || "L"}</span>;
                }

                return (
                  <td key={colPlayer.name} className={`pool-matrix__cell ${rowWon ? "pool-matrix__cell--win" : isDraw ? "pool-matrix__cell--draw" : "pool-matrix__cell--loss"}`}>
                    {cellContent}
                  </td>
                );
              })}
            </tr>
          ))}
        </tbody>
      </table>
      <div className="pool-matrix__legend">
        <span className="pool-matrix__legend-item pool-matrix__legend-item--win">W = win</span>
        <span className="pool-matrix__legend-item pool-matrix__legend-item--loss">L = loss</span>
        <span className="pool-matrix__legend-item pool-matrix__legend-item--draw">X = draw</span>
        <span style={{ color: "var(--ink-3)", fontSize: 11 }}>Row plays AKA vs col SHIRO</span>
      </div>
    </div>
  );
}

function PoolsViewer({ pools, standings, poolMatches, tweaks, competition, onMatchClick }) {
  if (!pools || pools.length === 0) {
    return <div className="empty"><div className="icon">⏳</div><h3>Pools not drawn yet</h3></div>;
  }
  const isTeam = competition && (competition.kind === "team" || competition.teamSize > 0);

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
      {pools.map((pool) => {
        const poolStandings = standings ? standings[pool.poolName] : null;
        const matches = poolMatches ? poolMatches.filter(m => {
          const id = m.id || "";
          return id.startsWith(pool.poolName + "-");
        }) : [];
        return (
          <div key={pool.poolName} className="pool" style={{ padding: 14 }}>
            <div className="pool__head">
              <div className="pool__name">{pool.poolName}</div>
              <div style={{ fontSize: 12, color: "var(--ink-3)" }}>
                {matches.filter(m => m.status === "completed").length}/{matches.length} matches
              </div>
            </div>

            {/* Standings table */}
            <table className="pool__table">
              <thead>
                {isTeam ? (
                  <tr><th>#</th><th>Team</th><th className="num">W</th><th className="num">L</th><th className="num">T</th><th className="num">IV</th><th className="num">IL</th><th className="num">PW</th><th className="num">PL</th></tr>
                ) : (
                  <tr><th>#</th><th>Player</th><th className="num">W</th><th className="num">L</th><th className="num">D</th><th className="num">PW</th><th className="num">PL</th></tr>
                )}
              </thead>
              <tbody>
                {poolStandings && poolStandings.length > 0 ? poolStandings.map((s, i) => (
                  <tr key={s.player.name}>
                    <td style={{ color: s.isOverridden ? "var(--accent)" : "var(--ink-3)", fontFamily: "var(--font-mono)", fontWeight: s.isOverridden ? 700 : 400 }}>{i + 1}{s.isOverridden ? "*" : ""}</td>
                    <td>
                      <div style={{ fontWeight: 500 }}>{s.player.name}</div>
                      {tweaks.showDojo ? <div style={{ fontSize: 11, color: "var(--ink-3)" }}>{s.player.dojo}</div> : null}
                    </td>
                    <td className="num">{s.wins}</td>
                    <td className="num">{s.losses}</td>
                    <td className="num">{s.draws}</td>
                    {isTeam && <td className="num">{s.individualWins || 0}</td>}
                    {isTeam && <td className="num">{s.individualLosses || 0}</td>}
                    <td className="num">{isTeam ? (s.pointsWon || 0) : s.ipponsGiven}</td>
                    <td className="num">{isTeam ? (s.pointsLost || 0) : s.ipponsTaken}</td>
                  </tr>
                )) : pool.players.map((p, i) => {
                  const cols = isTeam ? 7 : 5;
                  return (
                    <tr key={p.name}>
                      <td style={{ color: "var(--ink-3)", fontFamily: "var(--font-mono)" }}>{i + 1}</td>
                      <td>
                        <div style={{ fontWeight: 500 }}>{p.name}</div>
                        {tweaks.showDojo ? <div style={{ fontSize: 11, color: "var(--ink-3)" }}>{p.dojo}</div> : null}
                      </td>
                      {Array.from({ length: cols }, (_, j) => <td key={j} className="num">—</td>)}
                    </tr>
                  );
                })}
              </tbody>
            </table>

            {/* Round-robin matrix — always visible */}
            {matches.length > 0 && !isTeam && (
              <div style={{ marginTop: 12 }}>
                <PoolMatrix pool={pool} matches={matches} tweaks={tweaks} />
              </div>
            )}

            {/* Team matches: show as list (no matrix for team) */}
            {matches.length > 0 && isTeam && (
              <div style={{ marginTop: 12 }}>
                <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
                  {matches.map(m => <PoolMatchRow key={m.id} m={m} onClick={() => onMatchClick && onMatchClick(m)} />)}
                </div>
              </div>
            )}
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
            {q ? pluralize(matches.length, "match", "matches") : `${pluralize(roster.length, "participant")} — type to search`}
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

export { PlayerMultiFilter, applyFilters, matchHighlightedBy, competitionKindLabel, compMatches, tournamentMatches, currentMatchOf };

if (typeof window !== 'undefined') {
    window.PlayerMultiFilter = PlayerMultiFilter;
    window.applyFilters = applyFilters;
    window.matchHighlightedBy = matchHighlightedBy;
}

// Tournament-wide schedule (across competitions) — grouped by day, then court swimlanes + filter
function ScheduleViewer({ tournament, tweaks }) {
  const allMatches = useMemo(() => tournamentMatches(tournament).filter((m) => m.sideA && m.sideB), [tournament]);
  const courts = tournament.courts;
  const [picked, setPicked] = useState([]);
  const [dojoText, setDojoText] = useState("");
  const [courtFilter, setCourtFilter] = useState("all");

  // Derive unique days from competitions and matches
  const allDates = useMemo(() => {
    const days = new Set();
    (tournament.competitions || []).forEach((c) => { if (c.date) days.add(c.date); });
    allMatches.forEach((m) => { if (m.date) days.add(m.date); });
    const sorted = Array.from(days).sort();
    return sorted.length > 0 ? sorted : [""];
  }, [tournament, allMatches]);

  const [activeDay, setActiveDay] = useState(() => allDates[0] || "");
  const [compFilter, setCompFilter] = useState("all");

  // When dates change (data loads), reset to first
  React.useEffect(() => {
    setActiveDay(prev => allDates.includes(prev) ? prev : (allDates[0] || ""));
  }, [allDates]);

  const filtered = applyFilters(allMatches, picked, dojoText, compFilter);

  // For day-based filtering: if no dates at all, show everything; otherwise filter by active day
  const dayFiltered = allDates.length <= 1 && allDates[0] === ""
    ? filtered
    : filtered.filter((m) => {
        const mDate = m.date || (tournament.competitions || []).find(c => c.id === m.compId)?.date || "";
        return mDate === activeDay || (!mDate && activeDay === "");
      });

  const byCourt = {};
  courts.forEach((cc) => byCourt[cc] = []);
  dayFiltered.forEach((m) => { (byCourt[m.court] = byCourt[m.court] || []).push(m); });
  Object.values(byCourt).forEach((list) => list.sort((a, b) => {
    const order = { running: 0, scheduled: 1, completed: 2 };
    const ao = order[a.status] ?? 1;
    const bo = order[b.status] ?? 1;
    if (ao !== bo) return ao - bo;
    return (a.scheduledAt || "99:99").localeCompare(b.scheduledAt || "99:99");
  }));

  const matchHasFilter = (m) => matchHighlightedBy(m, picked, dojoText);
  const hasAnyFilter = picked.length > 0 || dojoText || compFilter !== "all" || courtFilter !== "all";
  const multiDay = allDates.length > 1 || (allDates.length === 1 && allDates[0] !== "");

  return (
    <div className="tw-sched">
      <div className="tw-sched__filters">
        <PlayerMultiFilter tournament={tournament} picked={picked} setPicked={setPicked} dojoText={dojoText} setDojoText={setDojoText} />
        <select className="input" style={{ width: "auto", minWidth: 160 }} value={compFilter} onChange={(e) => setCompFilter(e.target.value)}>
          <option value="all">All competitions</option>
          {(tournament.competitions || []).map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
        </select>
        <select className="input" style={{ width: "auto" }} value={courtFilter} onChange={(e) => setCourtFilter(e.target.value)}>
          <option value="all">All Shiaijo</option>
          {courts.map(c => <option key={c} value={c}>Shiaijo {c}</option>)}
        </select>
        {hasAnyFilter && (
          <button className="btn btn--ghost btn--sm" onClick={() => { setPicked([]); setDojoText(""); setCompFilter("all"); setCourtFilter("all"); }}>Clear</button>
        )}
        <span style={{ marginLeft: "auto", fontSize: 12, color: "var(--ink-3)" }}>{pluralize(dayFiltered.length, "match", "matches")} of {allMatches.length}</span>
      </div>

      {multiDay && (
        <div className="day-tabs">
          {allDates.map((d) => (
            <button key={d} className={`day-tab ${activeDay === d ? "is-active" : ""}`} onClick={() => setActiveDay(d)}>
              {d ? formatDate(d) : "All days"}
            </button>
          ))}
        </div>
      )}

      <div className="tw-courts">
        {allMatches.length === 0 ? (
          <div className="empty" style={{ gridColumn: "1 / -1" }}>
            <div className="icon">🗓</div>
            <h3>No matches scheduled yet</h3>
            <div style={{ fontSize: 13 }}>The schedule will appear here once the tournament begins.</div>
          </div>
        ) : courts.map((cc) => {
          const list = byCourt[cc] || [];
          const liveOn = list.find((m) => m.status === "running");
          if (courtFilter !== "all" && cc !== courtFilter) return null;
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
                  <TWMatch key={m.compId + m.id} m={m} highlight={matchHasFilter(m)} tweaks={tweaks} onClick={() => tweaks.onMatchClick && tweaks.onMatchClick(m)} />
                ))}
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}

function TWMatch({ m, highlight, _tweaks, onClick }) {
  const aWin = m.winner && m.sideA && m.winner.id === m.sideA.id;
  const bWin = m.winner && m.sideB && m.winner.id === m.sideB.id;
  const scoreStr = m.status === "completed" ? window.formatIpponsScore(m.ipponsB, m.ipponsA, m.score, m.decision) : null;
  return (
    <button className={`tw-match ${m.status === "running" ? "tw-match--live" : ""} ${m.status === "completed" ? "tw-match--done" : ""} ${highlight ? "tw-match--highlight" : ""}`} onClick={onClick} style={{ textAlign: "left", border: "none", background: "none", cursor: onClick ? "pointer" : "default" }}>
      <div>
        <div className="tw-match__time">{m.scheduledAt || "—"}</div>
        <div className="tw-match__phase">{m.phase === "pool" ? m.poolName : m.round}</div>
      </div>
      <div className="tw-match__players">
        <div className={`tw-match__name ${bWin ? "tw-match__name--w" : ""}`}>
          <span className="tw-match__badge tw-match__badge--shiro">S</span>
          {m.sideB?.name || "TBD"}
        </div>
        <div className={`tw-match__name ${aWin ? "tw-match__name--w" : ""}`}>
          <span className="tw-match__badge tw-match__badge--aka">A</span>
          {m.sideA?.name || "TBD"}
        </div>
        <div className="tw-match__comp">{m.compName}</div>
      </div>
      <div style={{ textAlign: "right", fontFamily: "var(--font-mono)", fontWeight: 700, fontSize: 13 }}>
        {m.status === "completed" && scoreStr}
        {m.status === "completed" && m.score?.type === "bye" && <span style={{ fontSize: 10, color: "var(--ink-3)" }}>BYE</span>}
        {m.status === "running" && <span className="bc-live">●</span>}
      </div>
    </button>
  );
}

function ResultsViewer({ _c, bracket }) {
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
  const [selectedMatch, setSelectedMatch] = useState(null);
  const extendedTweaks = { ...tweaks, onMatchClick: setSelectedMatch };
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
          <ScheduleViewer tournament={tournament} tweaks={extendedTweaks} />
        </div>
      </div>
      {selectedMatch && <MatchViewerModal match={selectedMatch} onClose={() => setSelectedMatch(null)} />}
    </div>
  );
}

function MatchViewerModal({ match, onClose }) {
  const onCloseRef = React.useRef(onClose);
  React.useEffect(() => { onCloseRef.current = onClose; });
  React.useEffect(() => {
    const onKey = (e) => { if (e.key === "Escape") onCloseRef.current(); };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []); // listener registered once; reads latest callback via ref
  if (!match) return null;
  const isTeam = match.compKind === "team" || match.teamSize > 0;
  const aName = match.sideA?.name || "TBD";
  const bName = match.sideB?.name || "TBD";
  const aWin = match.winner?.id === match.sideA?.id;
  const bWin = match.winner?.id === match.sideB?.id;

  return (
    <div className="modal-backdrop" onClick={onClose} style={{ zIndex: 1000, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
      <div className="card" onClick={e => e.stopPropagation()} style={{ width: "100%", maxWidth: 500, margin: 16 }}>
        <div style={{ display: "flex", justifyContent: "space-between", marginBottom: 16 }}>
          <h2 style={{ margin: 0, fontSize: 18 }}>Match Details</h2>
          <button className="btn btn--ghost btn--sm" onClick={onClose}>Close</button>
        </div>
        <div style={{ marginBottom: 16, fontSize: 13, color: "var(--ink-3)", display: "flex", justifyContent: "space-between", alignItems: "center" }}>
          <span>Shiaijo {match.court} · {match.phase === "pool" ? match.poolName : match.round}</span>
          <window.StatusBadge status={match.status} showLiveDot={match.status === "running"} />
        </div>
        
        <div className="se-head" style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 24 }}>
          <div className={`se-head__side ${bWin ? "se-head__side--w" : ""}`} style={{ flex: 1 }}>
             <div className="se-head__name" style={{ fontWeight: 600 }}>{bName}</div>
             <div className="se-head__badge se-head__badge--shiro" style={{ display: "inline-block", background: "#fff", color: "#000", border: "1px solid #ddd", padding: "2px 6px", fontSize: 11, borderRadius: 4, marginTop: 4 }}>SHIRO</div>
          </div>
          <div className="se-head__vs" style={{ padding: "0 16px", color: "var(--ink-3)" }}>vs</div>
          <div className={`se-head__side ${aWin ? "se-head__side--w" : ""}`} style={{ flex: 1, textAlign: "right" }}>
             <div className="se-head__name" style={{ fontWeight: 600 }}>{aName}</div>
             <div className="se-head__badge se-head__badge--aka" style={{ display: "inline-block", background: "var(--red)", color: "#fff", padding: "2px 6px", fontSize: 11, borderRadius: 4, marginTop: 4 }}>AKA</div>
          </div>
        </div>

        {isTeam ? (
          <div>
            <div style={{ display: "flex", justifyContent: "space-between", borderBottom: "1px solid var(--line)", paddingBottom: 8, marginBottom: 8, fontSize: 12, fontWeight: 600, color: "var(--ink-3)" }}>
              <div style={{ width: 80 }}>SHIRO</div>
              <div style={{ flex: 1, textAlign: "center" }}>Position</div>
              <div style={{ width: 80, textAlign: "right" }}>AKA</div>
            </div>
            {match.subResults && match.subResults.length > 0 ? match.subResults.map((sub, i) => {
               const sIpponsB = (sub.ipponsB || []).filter(x => x && x !== "•").join("");
               const sIpponsA = (sub.ipponsA || []).filter(x => x && x !== "•").join("");
               const hansokuB = sub.hansokuB || 0;
               const hansokuA = sub.hansokuA || 0;
               return (
                 <div key={i} style={{ display: "flex", justifyContent: "space-between", alignItems: "center", padding: "8px 0", borderBottom: "1px solid var(--line-light)" }}>
                   <div style={{ width: 80, display: "flex", flexDirection: "column" }}>
                     <span style={{ fontWeight: 600, fontSize: 14 }}>{sIpponsB || "—"}</span>
                     {hansokuB > 0 && <span style={{ fontSize: 11, color: "var(--ink-3)" }}>Fouls: {hansokuB}</span>}
                   </div>
                   <div style={{ flex: 1, textAlign: "center", fontSize: 13, color: "var(--ink-3)" }}>
                     Match {sub.position || i+1}
                   </div>
                   <div style={{ width: 80, textAlign: "right", display: "flex", flexDirection: "column" }}>
                     <span style={{ fontWeight: 600, fontSize: 14 }}>{sIpponsA || "—"}</span>
                     {hansokuA > 0 && <span style={{ fontSize: 11, color: "var(--ink-3)" }}>Fouls: {hansokuA}</span>}
                   </div>
                 </div>
               );
            }) : (
              <div style={{ textAlign: "center", padding: 20, color: "var(--ink-3)" }}>No individual match details available.</div>
            )}
          </div>
        ) : (
          <div style={{ textAlign: "center", background: "var(--bg-2)", borderRadius: 8, padding: 16 }}>
            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
               <div style={{ display: "flex", flexDirection: "column", alignItems: "flex-start", flex: 1 }}>
                  <div style={{ fontSize: 12, color: "var(--ink-3)", marginBottom: 4 }}>Ippons</div>
                  <div style={{ fontSize: 24, fontWeight: 700 }}>{(match.ipponsB || []).filter(x => x && x !== "•").join("") || "—"}</div>
                  {match.hansokuB > 0 && <div style={{ fontSize: 12, color: "var(--ink-3)", marginTop: 4 }}>Fouls: {match.hansokuB}</div>}
               </div>
               <div style={{ fontSize: 24, color: "var(--ink-3)", padding: "0 16px" }}>-</div>
               <div style={{ display: "flex", flexDirection: "column", alignItems: "flex-end", flex: 1 }}>
                  <div style={{ fontSize: 12, color: "var(--ink-3)", marginBottom: 4 }}>Ippons</div>
                  <div style={{ fontSize: 24, fontWeight: 700 }}>{(match.ipponsA || []).filter(x => x && x !== "•").join("") || "—"}</div>
                  {match.hansokuA > 0 && <div style={{ fontSize: 12, color: "var(--ink-3)", marginTop: 4 }}>Fouls: {match.hansokuA}</div>}
               </div>
            </div>
            {match.score?.type === "hantei" && <div style={{ marginTop: 16, fontSize: 14, fontWeight: 600 }}>Decision (Hantei)</div>}
            {match.score?.type === "hikiwake" && <div style={{ marginTop: 16, fontSize: 14, fontWeight: 600 }}>Draw (Hikiwake)</div>}
            {match.score?.type === "bye" && <div style={{ marginTop: 16, fontSize: 14, fontWeight: 600 }}>BYE</div>}
          </div>
        )}
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
