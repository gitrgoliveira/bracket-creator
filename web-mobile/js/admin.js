// Admin side — single tournament. Tournament has multiple Competitions.
// Top-level: Tournament dashboard (all competitions), per-competition pages.

const { useState: useStateA, useMemo: useMemoA, useEffect: useEffectA, useRef: useRefA, useLayoutEffect: useLayoutEffectA } = React;

function AdminApp({ tournament, onUpdate, onLogout, onViewerMode, tweaks, password }) {
  const [view, setView] = useStateA({ kind: "dashboard" });

  const t = tournament;

  const updateCompetition = async (cid, next) => {
    try {
        await window.API.updateCompetition(cid, next, password);
        const comps = await window.API.fetchCompetitions();
        onUpdate({ ...t, competitions: comps });
    } catch (e) {
        alert("Failed to update competition: " + e.message);
    }
  };

  const moveMatchCourt = async (compId, matchId, newCourt) => {
    try {
        // Placeholder API call
        // await window.API.moveMatchCourt(compId, matchId, newCourt, password);
        alert("Move court not yet implemented in API");
    } catch (e) {
        alert("Failed to move court: " + e.message);
    }
  };

  const editMatchScore = async (compId, matchId, result) => {
    try {
        await window.API.recordScore(compId, matchId, result, password);
        // Refresh competition data?
        // For now just refresh the whole list or assume the caller handles UI update
        const comps = await window.API.fetchCompetitions();
        onUpdate({ ...t, competitions: comps });
    } catch (e) {
        alert("Failed to record score: " + e.message);
    }
  };

  const addCompetition = async (c) => {
    try {
        await window.API.createCompetition(c, password);
        const comps = await window.API.fetchCompetitions();
        onUpdate({ ...t, competitions: comps });
    } catch (e) {
        alert("Failed to add competition: " + e.message);
    }
  };

  const updateTournament = async (patch) => {
      try {
          const next = { ...t, ...patch };
          // If password was not in the patch, preserve the current session password
          // (since tournament.password is cleared in the viewer API)
          if (!patch.password) {
              next.password = password;
          }
          await window.API.updateTournament(next, password);
          onUpdate(next);
      } catch (e) {
          alert("Failed to update tournament: " + e.message);
      }
  };

  if (view.kind === "dashboard") {
    return <AdminDashboard
      tournament={t}
      onOpenCompetition={(id) => setView({ kind: "competition", id, section: "overview" })}
      onCreateCompetition={() => setView({ kind: "createComp" })}
      onEditTournament={() => setView({ kind: "editTournament" })}
      onOpenSchedule={() => setView({ kind: "schedule" })}
      onOpenScoreEditor={() => setView({ kind: "scoreEditor" })}
      onLogout={onLogout}
      onViewerMode={onViewerMode}
    />;
  }

  if (view.kind === "createComp") {
    return <AdminCreateCompetition
      tournament={t}
      onCancel={() => setView({ kind: "dashboard" })}
      onCreate={(c) => { addCompetition(c); setView({ kind: "competition", id: c.id, section: "participants" }); }}
      onLogout={onLogout}
      onViewerMode={onViewerMode}
    />;
  }

  if (view.kind === "editTournament") {
    return <AdminEditTournament
      tournament={t}
      onCancel={() => setView({ kind: "dashboard" })}
      onSave={(patch) => { updateTournament(patch); setView({ kind: "dashboard" }); }}
      onLogout={onLogout}
      onViewerMode={onViewerMode}
    />;
  }

  if (view.kind === "schedule") {
    return <AdminSchedulePage
      tournament={t}
      onBack={() => setView({ kind: "dashboard" })}
      onMoveCourt={moveMatchCourt}
      onEditScore={editMatchScore}
      onLogout={onLogout}
      onViewerMode={onViewerMode}
      tweaks={tweaks}
    />;
  }

  if (view.kind === "scoreEditor") {
    return <AdminScoreEditorPage
      tournament={t}
      onBack={() => setView({ kind: "dashboard" })}
      onEditScore={editMatchScore}
      onMoveCourt={moveMatchCourt}
      onLogout={onLogout}
      onViewerMode={onViewerMode}
      tweaks={tweaks}
    />;
  }

  const [adminCompData, setAdminCompData] = useStateA(null);
  const [adminLoading, setAdminLoading] = useStateA(false);

  useEffectA(() => {
    if (view.kind === "competition") {
      setAdminLoading(true);
      window.API.fetchCompetitionDetails(view.id)
        .then(data => {
          setAdminCompData(data);
          setAdminLoading(false);
        })
        .catch(err => {
          console.error(err);
          setAdminLoading(false);
        });
    } else {
      setAdminCompData(null);
    }
  }, [view.id, view.kind]);

  useEffectA(() => {
    if (view.kind === "competition") {
        const unsub = window.API.subscribeToEvents((event) => {
            if (event.type === "competition_started" || event.type === "match_updated") {
                if (view.id === event.data.competitionId) {
                    window.API.fetchCompetitionDetails(view.id).then(setAdminCompData);
                }
            }
        });
        return unsub;
    }
  }, [view.id, view.kind]);

  if (view.kind === "competition") {
    const c = t.competitions.find((cc) => cc.id === view.id);
    if (!c) return <div className="page"><div className="empty"><h3>Competition not found</h3></div></div>;
    if (adminLoading && !adminCompData) return <div className="page"><div className="loading">Loading details...</div></div>;
    
    return <AdminCompetition
      tournament={t}
      competition={adminCompData?.config || c}
      pools={adminCompData?.pools}
      poolMatches={adminCompData?.poolMatches}
      standings={adminCompData?.standings}
      bracket={adminCompData?.bracket}
      section={view.section}
      onSection={(section) => setView({ ...view, section })}
      onBack={() => setView({ kind: "dashboard" })}
      onUpdate={(next) => updateCompetition(c.id, next)}
      onMoveCourt={moveMatchCourt}
      onEditScore={editMatchScore}
      onLogout={onLogout}
      onViewerMode={onViewerMode}
      tweaks={tweaks}
      password={password}
    />;
  }
}

function AdminTopbar({ onLogout, onViewerMode, tournament }) {
  return (
    <div className="topbar">
      <div className="topbar__brand">
        <div className="topbar__logo">BC</div>
        <div>
          <div className="topbar__title">{tournament?.name || "Bracket Creator"}</div>
          <div className="topbar__sub">Admin console</div>
        </div>
      </div>
      <div className="topbar__spacer"></div>
      <button className="viewer-toggle" onClick={onViewerMode}>👁 Public viewer</button>
      <div className="topbar__user">
        <span className="dot"></span> sensei@dojo
      </div>
      <button className="btn btn--ghost btn--sm" onClick={onLogout}>Sign out</button>
    </div>
  );
}

function StatusBadge({ status }) {
  const map = {
    setup: ["badge--setup", "Setup"],
    pools: ["badge--pools", "Pools"],
    playoffs: ["badge--playoffs", "Playoffs"],
    completed: ["badge--completed", "Completed"],
  };
  const [cls, label] = map[status] || ["badge--setup", status];
  return <span className={`badge ${cls}`}>{label}</span>;
}

function formatDate(d) {
  const date = new Date(d + "T00:00");
  return date.toLocaleDateString("en-GB", { day: "numeric", month: "short", year: "numeric" });
}

function AdminDashboard({ tournament, onOpenCompetition, onCreateCompetition, onEditTournament, onOpenSchedule, onOpenScoreEditor, onLogout, onViewerMode }) {
  const t = tournament;
  const comps = t.competitions || [];

  // global stats across all competitions
  let totalMatches = 0, doneMatches = 0, liveMatches = 0;
  let totalParticipants = 0;
  comps.forEach((c) => {
    const players = c.players || [];
    totalParticipants += players.length;
    const ms = window.compMatches ? window.compMatches(c).filter((m) => m && m.sideA && m.sideB) : [];
    ms.forEach((m) => { totalMatches++; if (m.status === "complete") doneMatches++; if (m.status === "in_progress") liveMatches++; });
  });

  const running = comps.filter((c) => c.status === "pools" || c.status === "playoffs");

  return (
    <div className="app">
      <AdminTopbar onLogout={onLogout} onViewerMode={onViewerMode} tournament={t} />
      <div className="page page--wide" style={{ maxWidth: 1280 }}>
        <div className="page-head">
          <div>
            <div style={{ display: "flex", alignItems: "center", gap: 10, flexWrap: "wrap" }}>
              <h1 className="page-head__title">{t.name}</h1>
              <StatusBadge status={t.status} />
            </div>
            <div className="page-head__sub">
              {formatDate(t.date)} · {t.venue} · {t.courts.length} shiaijo · {comps.length} competition{comps.length === 1 ? "" : "s"} · {totalParticipants} participants
            </div>
          </div>
          <div className="page-head__actions">
            <button className="btn" onClick={onEditTournament}>Edit details</button>
            <button className="btn btn--primary" onClick={onCreateCompetition}>+ Add competition</button>
          </div>
        </div>

        <div className="stats-strip">
          <div className="stat-box"><div className="v">{comps.length}</div><div className="l">Competitions</div></div>
          <div className="stat-box"><div className="v">{totalParticipants}</div><div className="l">Participants</div></div>
          <div className="stat-box"><div className="v">{doneMatches}/{totalMatches}</div><div className="l">Matches done</div></div>
          <div className="stat-box"><div className="v" style={{ color: liveMatches > 0 ? "var(--red)" : "inherit" }}>{liveMatches}</div><div className="l">Live now</div></div>
        </div>

        <div className="row" style={{ marginBottom: 24 }}>
          <button className="card" style={{ textAlign: "left", cursor: "pointer", border: "1px solid var(--line)" }} onClick={onOpenSchedule}>
            <div className="card__title" style={{ marginBottom: 6 }}>🗓 Tournament schedule →</div>
            <div className="card__sub">All matches across courts. Move matches between shiaijo, filter by player.</div>
          </button>
          <button className="card" style={{ textAlign: "left", cursor: "pointer", border: "1px solid var(--line)" }} onClick={onOpenScoreEditor}>
            <div className="card__title" style={{ marginBottom: 6 }}>✎ Score editor →</div>
            <div className="card__sub">Update live results or correct past matches across the tournament.</div>
          </button>
        </div>

        {running.length > 0 && (<>
          <div className="section-title" style={{ display: "flex", alignItems: "center", gap: 6 }}>
            <span className="dot dot--live"></span> Currently running
          </div>
          <div className="tlist" style={{ marginBottom: 24 }}>
            {running.map((c) => <CompCard key={c.id} c={c} onOpen={() => onOpenCompetition(c.id)} />)}
          </div>
        </>)}

        <div className="section-title">All competitions</div>
        <div className="tlist">
          {comps.map((c) => <CompCard key={c.id} c={c} onOpen={() => onOpenCompetition(c.id)} />)}
          <button className="tcard tcard--add" onClick={onCreateCompetition}>
            <div style={{ fontSize: 28, color: "var(--ink-3)" }}>+</div>
            <div style={{ fontWeight: 600, marginTop: 4 }}>Add competition</div>
            <div style={{ fontSize: 12, color: "var(--ink-3)", marginTop: 2 }}>Individual or Team</div>
          </button>
        </div>
      </div>
    </div>
  );
}

function CompCard({ c, onOpen }) {
  let liveCount = 0;
  if (c.pools) c.pools.forEach((p) => (p.matches || []).forEach((m) => m.status === "in_progress" && liveCount++));
  if (c.bracket && c.bracket.rounds) c.bracket.rounds.forEach((r) => (r || []).forEach((m) => m.status === "in_progress" && liveCount++));
  return (
    <button className="tcard" onClick={onOpen}>
      <div className="tcard__head">
        <div>
          <div className="tcard__eyebrow">{window.competitionKindLabel(c)}{c.teamSize ? ` · ${c.teamSize}-person` : ""}</div>
          <div className="tcard__name">{c.name}</div>
          <div className="tcard__meta">Starts {c.startTime} · {c.courts.join(", ")}</div>
        </div>
        <StatusBadge status={c.status} />
      </div>
      <div className="tcard__stats">
        <div className="tcard__stat"><div className="v">{(c.players || []).length}</div><div className="l">{c.kind === "team" ? "Teams" : "Players"}</div></div>
        <div className="tcard__stat"><div className="v">{c.courts.length}</div><div className="l">Shiaijo</div></div>
        <div className="tcard__stat"><div className="v">{c.format === "pools" ? "Pools" : "KO"}</div><div className="l">Format</div></div>
        {liveCount > 0 && <div className="tcard__stat"><div className="v" style={{ color: "var(--red)" }}>{liveCount}</div><div className="l">Live</div></div>}
      </div>
    </button>
  );
}

function AdminEditTournament({ tournament, onCancel, onSave, onLogout, onViewerMode }) {
  const [name, setName] = useStateA(tournament.name);
  const [venue, setVenue] = useStateA(tournament.venue);
  const [date, setDate] = useStateA(tournament.date);
  const [courts, setCourts] = useStateA(tournament.courts.length);
  const [pass, setPass] = useStateA(""); // Leave empty to keep existing, unless changed
  return (
    <div className="app">
      <AdminTopbar onLogout={onLogout} onViewerMode={onViewerMode} tournament={tournament} />
      <div className="page" style={{ maxWidth: 720 }}>
        <div className="crumbs"><button onClick={onCancel}>{tournament.name}</button><span className="sep">/</span><span>Edit details</span></div>
        <div className="page-head"><h1 className="page-head__title">Edit tournament details</h1></div>
        <div className="card card--pad-lg">
          <div className="row">
            <div className="field"><label className="field__label">Name</label><input className="input" value={name} onChange={(e) => setName(e.target.value)} /></div>
            <div className="field"><label className="field__label">Date</label><input className="input" type="date" value={date} onChange={(e) => setDate(e.target.value)} /></div>
          </div>
          <div className="field"><label className="field__label">Venue</label><input className="input" value={venue} onChange={(e) => setVenue(e.target.value)} /></div>
          <div className="field">
            <label className="field__label">Number of Shiaijo (courts)</label>
            <input className="input" type="number" min="1" max="26" value={courts} onChange={(e) => setCourts(+e.target.value)} />
            <div className="field__hint">Shiaijo are shared by all competitions. Each competition can be assigned a subset.</div>
          </div>
          <div className="field">
            <label className="field__label">Admin Password</label>
            <input className="input" type="password" value={pass} onChange={(e) => setPass(e.target.value)} placeholder="••••••••" />
            <div className="field__hint">Enter a new password to change it. Leave blank to keep the current one.</div>
          </div>
          <div style={{ display: "flex", justifyContent: "flex-end", gap: 8, marginTop: 16 }}>
            <button className="btn" onClick={onCancel}>Cancel</button>
            <button className="btn btn--primary" onClick={() => onSave({ name, venue, date, password: pass || undefined, courts: Array.from({ length: courts }, (_, i) => String.fromCharCode(65 + i)) })}>Save</button>
          </div>
        </div>
      </div>
    </div>
  );
}

function AdminCreateCompetition({ tournament, onCancel, onCreate, onLogout, onViewerMode }) {
  const [name, setName] = useStateA("");
  const [kind, setKind] = useStateA("individual");
  const [gender, setGender] = useStateA("M"); // for individual: M/F/X
  const [format, setFormat] = useStateA("playoffs");
  const [useSample, setUseSample] = useStateA(false);
  const [sampleSize, setSampleSize] = useStateA("medium");
  const [poolMode, setPoolMode] = useStateA("max");
  const [poolSize, setPoolSize] = useStateA(4);
  const [winners, setWinners] = useStateA(2);
  const [startTime, setStartTime] = useStateA("09:00");
  const [teamSize, setTeamSize] = useStateA(5);
  const [selectedCourts, setSelectedCourts] = useStateA(tournament.courts.slice(0, Math.min(2, tournament.courts.length)));

  const toggleCourt = (cc) => setSelectedCourts((sc) => sc.includes(cc) ? sc.filter((c) => c !== cc) : [...sc, cc].sort());

  const create = () => {
    const id = "c-" + Date.now().toString(36);
    const finalName = name || (kind === "team"
      ? (gender === "F" ? "Women's Teams" : "Men's Teams")
      : (gender === "F" ? "Women's Individual" : gender === "M" ? "Men's Individual" : "Individual"));
    const c = window.buildCompetition({
      id,
      name: finalName,
      kind, gender,
      format,
      sampleRoster: useSample ? sampleSize : null,
      seedCount: 0, status: "setup",
      startTime,
      teamSize: kind === "team" ? teamSize : 0,
      courts: selectedCourts.length ? selectedCourts : [tournament.courts[0]],
      poolMode, poolSize, winnersPerPool: winners,
    });
    onCreate(c);
  };

  return (
    <div className="app">
      <AdminTopbar onLogout={onLogout} onViewerMode={onViewerMode} tournament={tournament} />
      <div className="page" style={{ maxWidth: 760 }}>
        <div className="crumbs"><button onClick={onCancel}>{tournament.name}</button><span className="sep">/</span><span>Add competition</span></div>
        <div className="page-head">
          <div>
            <h1 className="page-head__title">Add competition</h1>
            <div className="page-head__sub">A competition is one event within the tournament — e.g. Men's Individual, Women's Teams.</div>
          </div>
        </div>

        <div className="card card--pad-lg">
          <div className="field">
            <label className="field__label">Competition type</label>
            <div className="radio-group">
              <button className={`radio-pill ${kind === "individual" ? "is-active" : ""}`} onClick={() => setKind("individual")}>Individual</button>
              <button className={`radio-pill ${kind === "team" ? "is-active" : ""}`} onClick={() => setKind("team")}>Team</button>
            </div>
          </div>

          <div className="field">
            <label className="field__label">Category (optional)</label>
            <div className="radio-group">
              <button className={`radio-pill ${gender === "M" ? "is-active" : ""}`} onClick={() => setGender("M")}>Men</button>
              <button className={`radio-pill ${gender === "F" ? "is-active" : ""}`} onClick={() => setGender("F")}>Women</button>
              <button className={`radio-pill ${gender === "X" ? "is-active" : ""}`} onClick={() => setGender("X")}>Mixed / Other</button>
            </div>
            <div className="field__hint">Used for the display label and in name suggestions. You can change later.</div>
          </div>

          <div className="row">
            <div className="field">
              <label className="field__label">Display name</label>
              <input className="input" placeholder="e.g. Men's Individual" value={name} onChange={(e) => setName(e.target.value)} />
            </div>
            <div className="field">
              <label className="field__label">Start time</label>
              <input className="input" type="time" value={startTime} onChange={(e) => setStartTime(e.target.value)} />
            </div>
          </div>

          <div className="field">
            <label className="field__label">Format</label>
            <div className="radio-group">
              <button className={`radio-pill ${format === "playoffs" ? "is-active" : ""}`} onClick={() => setFormat("playoffs")}>Knockout only</button>
              <button className={`radio-pill ${format === "pools" ? "is-active" : ""}`} onClick={() => setFormat("pools")}>Pools + Knockout</button>
            </div>
            <div className="field__hint">"Pools + Knockout" runs round-robin pools first, then top finishers advance to a knockout bracket.</div>
          </div>

          <div className="field">
            <label className="field__label">
              <label className="checkbox" style={{ display: "inline-flex" }}>
                <input type="checkbox" checked={useSample} onChange={(e) => setUseSample(e.target.checked)} />
                Pre-fill with sample roster
              </label>
            </label>
            {useSample && (
              <div className="radio-group" style={{ marginTop: 8 }}>
                <button className={`radio-pill ${sampleSize === "small" ? "is-active" : ""}`} onClick={() => setSampleSize("small")}>Small (8)</button>
                <button className={`radio-pill ${sampleSize === "medium" ? "is-active" : ""}`} onClick={() => setSampleSize("medium")}>Medium (16)</button>
                <button className={`radio-pill ${sampleSize === "large" ? "is-active" : ""}`} onClick={() => setSampleSize("large")}>Large (32)</button>
              </div>
            )}
            <div className="field__hint">Leave off to add real participants in the next step.</div>
          </div>

          <div className="field">
            <label className="field__label">Assigned shiaijo</label>
            <div className="radio-group">
              {tournament.courts.map((cc) => (
                <button key={cc} className={`radio-pill ${selectedCourts.includes(cc) ? "is-active" : ""}`} onClick={() => toggleCourt(cc)}>Shiaijo {cc}</button>
              ))}
            </div>
            <div className="field__hint">Concurrency for this competition equals the number of courts assigned. Different competitions can share courts; the schedule prevents conflicts.</div>
          </div>

          {format === "pools" && (
            <>
              <div className="field">
                <label className="field__label">Pool sizing mode</label>
                <div className="radio-group">
                  <button className={`radio-pill ${poolMode === "max" ? "is-active" : ""}`} onClick={() => setPoolMode("max")}>Max players per pool</button>
                  <button className={`radio-pill ${poolMode === "min" ? "is-active" : ""}`} onClick={() => setPoolMode("min")}>Min players per pool</button>
                </div>
                <div className="field__hint">
                  {poolMode === "max"
                    ? "Pools will be created so no pool has more than the size below. More pools, smaller pools."
                    : "Pools will be created so each has at least the size below. Fewer pools, larger pools."}
                </div>
              </div>
              <div className="row">
                <div className="field"><label className="field__label">{poolMode === "max" ? "Maximum" : "Minimum"} players per pool</label><input className="input" type="number" min="3" value={poolSize} onChange={(e) => setPoolSize(+e.target.value)} /></div>
                <div className="field"><label className="field__label">Winners per pool</label><input className="input" type="number" min="1" value={winners} onChange={(e) => setWinners(+e.target.value)} /></div>
              </div>
            </>
          )}

          {kind === "team" && (
            <div className="field">
              <label className="field__label">Team size</label>
              <input className="input" type="number" min="1" max="9" value={teamSize} onChange={(e) => setTeamSize(+e.target.value)} />
              <div className="field__hint">Standard kendo team is 5 (Senpou, Jihou, Chuken, Fukushou, Taishou).</div>
            </div>
          )}

          <div style={{ display: "flex", justifyContent: "flex-end", gap: 8, marginTop: 16 }}>
            <button className="btn" onClick={onCancel}>Cancel</button>
            <button className="btn btn--primary" onClick={create}>Create & continue →</button>
          </div>
        </div>
      </div>
    </div>
  );
}

function AdminCompetition({ tournament, competition, pools, poolMatches, standings, bracket, section, onSection, onBack, onUpdate, onMoveCourt, onEditScore, onLogout, onViewerMode, tweaks, password }) {
  const c = competition;
  const t = tournament;

  const start = async () => {
      try {
          await window.API.startCompetition(c.id, password);
          // Trigger a refresh of competition data in the parent
          onUpdate(c); 
          onSection(c.format === "pools" ? "pools" : "bracket");
      } catch (e) {
          alert("Failed to start competition: " + e.message);
      }
  };

  const sections = [
    { sec: "Setup", items: [
      { id: "overview", label: "Overview" },
      { id: "participants", label: "Participants & seeds" },
      { id: "settings", label: "Settings" },
    ]},
    { sec: "Run", items: [
      pools ? { id: "pools", label: "Pools — live" } : null,
      bracket ? { id: "bracket", label: "Bracket — live" } : null,
      { id: "scores", label: "Scores — edit" },
    ].filter(Boolean) },
    { sec: "Output", items: [
      { id: "export", label: "Export & print" },
    ]},
  ];

  return (
    <div className="app">
      <AdminTopbar onLogout={onLogout} onViewerMode={onViewerMode} tournament={t} />
      <div className="page page--wide" style={{ maxWidth: 1400 }}>
        <div className="crumbs">
          <button onClick={onBack}>{t.name}</button>
          <span className="sep">/</span>
          <span>{c.name}</span>
        </div>
        <div className="page-head">
          <div>
            <div style={{ display: "flex", alignItems: "center", gap: 10, flexWrap: "wrap" }}>
              <h1 className="page-head__title">{c.name}</h1>
              <StatusBadge status={c.status} />
            </div>
            <div className="page-head__sub">{window.competitionKindLabel(c)} · {c.players.length} {c.kind === "team" ? "teams" : "players"} · Starts {c.startTime} · {c.courts.join(", ")}</div>
          </div>
          <div className="page-head__actions">
            {c.status === "setup" && c.players.length >= 2 && (
              <button className="btn btn--primary" onClick={start}>Start competition →</button>
            )}
          </div>
        </div>

        <div className="workspace">
          <div className="side-nav">
            {sections.map((sec) => (
              <div key={sec.sec}>
                <div className="side-nav__sec">{sec.sec}</div>
                {sec.items.map((it) => (
                  <button key={it.id} className={section === it.id ? "is-active" : ""} onClick={() => onSection(it.id)}>{it.label}</button>
                ))}
              </div>
            ))}
            <div>
              <div className="side-nav__sec">Other competitions</div>
              {t.competitions.filter((cc) => cc.id !== c.id).map((cc) => (
                <button key={cc.id} onClick={onBack}>{cc.name}</button>
              ))}
            </div>
          </div>
          <div>
            {section === "overview" && <AdminCompOverview c={c} onSection={onSection} />}
            {section === "participants" && <AdminParticipants c={c} onUpdate={onUpdate} />}
            {section === "settings" && <AdminSettings c={c} tournament={t} onUpdate={onUpdate} />}
            {section === "pools" && <AdminPools c={c} pools={pools} standings={standings} onUpdate={onUpdate} tweaks={tweaks} onEditScore={onEditScore} />}
            {section === "bracket" && <AdminBracket c={c} t={t} bracket={bracket} onUpdate={onUpdate} onMoveCourt={onMoveCourt} tweaks={tweaks} />}
            {section === "scores" && <AdminScoreEditor c={c} t={t} onEditScore={onEditScore} onMoveCourt={onMoveCourt} restrictToCompId={c.id} embedded />}
            {section === "export" && <AdminExport c={c} t={t} />}
          </div>
        </div>
      </div>
    </div>
  );
}

function AdminCompOverview({ c, onSection }) {
  let total = 0, done = 0, live = 0;
  if (c.pools) c.pools.forEach((p) => (p.matches || []).forEach((m) => { total++; if (m.status === "complete") done++; if (m.status === "in_progress") live++; }));
  if (c.bracket && c.bracket.rounds) c.bracket.rounds.forEach((r) => (r || []).forEach((m) => { if (!m.sideA || !m.sideB) return; total++; if (m.status === "complete") done++; if (m.status === "in_progress") live++; }));
  const pct = total ? Math.round((done / total) * 100) : 0;
  return (
    <div>
      <div className="stats-strip">
        <div className="stat-box"><div className="v">{c.players.length}</div><div className="l">{c.kind === "team" ? "Teams" : "Participants"}</div></div>
        <div className="stat-box"><div className="v">{c.players.filter((p) => p.seed).length}</div><div className="l">Seeded</div></div>
        <div className="stat-box"><div className="v">{done}/{total}</div><div className="l">Matches done</div></div>
        <div className="stat-box"><div className="v" style={{ color: live > 0 ? "var(--red)" : "inherit" }}>{live}</div><div className="l">Live now</div></div>
      </div>
      <div className="card" style={{ marginBottom: 16 }}>
        <div className="card__head"><div className="card__title">Progress</div><div className="card__sub">{pct}%</div></div>
        <div style={{ height: 8, background: "var(--line-2)", borderRadius: 999, overflow: "hidden" }}>
          <div style={{ height: "100%", width: pct + "%", background: "var(--accent)", transition: "width 300ms" }}></div>
        </div>
      </div>
      <div className="row">
        <button className="card" style={{ textAlign: "left", cursor: "pointer", border: "1px solid var(--line)" }} onClick={() => onSection("scores")}>
          <div className="card__title" style={{ marginBottom: 6 }}>Scores →</div>
          <div className="card__sub">Update or correct match results</div>
        </button>
        <button className="card" style={{ textAlign: "left", cursor: "pointer", border: "1px solid var(--line)" }} onClick={() => onSection(c.bracket ? "bracket" : "pools")}>
          <div className="card__title" style={{ marginBottom: 6 }}>Live results →</div>
          <div className="card__sub">Visual bracket / pool standings</div>
        </button>
      </div>
    </div>
  );
}

function AdminParticipants({ c, onUpdate }) {
  const [text, setText] = useStateA(() => c.players.map((p) => `${p.name}, ${p.dojo}`).join("\n"));
  const [dragOver, setDragOver] = useStateA(false);
  const fileRef = useRefA(null);

  const lines = text.split("\n").filter((l) => l.trim());
  const players = c.players;

  const updateSeed = (idx, val) => {
    const np = [...c.players];
    const seed = parseInt(val);
    np[idx] = { ...np[idx], seed: isNaN(seed) || seed <= 0 ? null : seed };
    onUpdate({ ...c, players: np });
  };
  const apply = () => {
    const np = lines.map((line, i) => {
      const parts = line.split(",").map((s) => s.trim());
      const existing = c.players[i];
      return { id: existing?.id || `${c.id}-p${i + 1}`, name: parts[0], dojo: parts[1] || "", seed: existing?.seed || null };
    });
    onUpdate({ ...c, players: np });
  };

  const handleFile = (file) => {
    if (!file) return;
    const reader = new FileReader();
    reader.onload = (e) => {
      const raw = e.target.result;
      // Lightweight CSV parse — handles "name, dojo" with optional header & quoted fields
      const out = [];
      raw.split(/\r?\n/).forEach((line, i) => {
        const trimmed = line.trim();
        if (!trimmed) return;
        // skip a header row that says name/dojo
        if (i === 0 && /name/i.test(trimmed) && /dojo|club|team/i.test(trimmed)) return;
        // naive split on commas / tabs / semicolons
        const cells = trimmed.split(/[,;\t]/).map((s) => s.trim().replace(/^"|"$/g, ""));
        if (cells[0]) out.push(`${cells[0]}${cells[1] ? `, ${cells[1]}` : ""}`);
      });
      setText(out.join("\n"));
    };
    reader.readAsText(file);
  };

  const onDrop = (e) => {
    e.preventDefault();
    setDragOver(false);
    const file = e.dataTransfer.files[0];
    handleFile(file);
  };

  return (
    <div className="row" style={{ gridTemplateColumns: "1.4fr 1fr", alignItems: "start" }}>
      <div className="card">
        <div className="card__head">
          <div>
            <div className="card__title">{c.kind === "team" ? "Team list" : "Participant list"}</div>
            <div className="card__sub">{lines.length} entries · One per line, "{c.kind === "team" ? "Team name, Dojo" : "Name, Dojo"}"</div>
          </div>
          <div style={{ display: "flex", gap: 6 }}>
            <button className="btn btn--sm" onClick={() => fileRef.current?.click()}>Upload CSV</button>
            <input ref={fileRef} type="file" accept=".csv,.txt,text/csv,text/plain" style={{ display: "none" }} onChange={(e) => handleFile(e.target.files[0])} />
            <button className="btn btn--sm btn--primary" onClick={apply}>Apply</button>
          </div>
        </div>

        <div
          className={`dropzone ${dragOver ? "dropzone--active" : ""}`}
          onClick={() => fileRef.current?.click()}
          onDragOver={(e) => { e.preventDefault(); setDragOver(true); }}
          onDragLeave={() => setDragOver(false)}
          onDrop={onDrop}
          style={{ marginBottom: 10 }}
        >
          <div className="dropzone__icon">📥</div>
          <div className="dropzone__title">{dragOver ? "Drop CSV to import" : "Drop a CSV here, or click to browse"}</div>
          <div className="dropzone__sub">Two columns: name, dojo · header row optional</div>
        </div>

        <textarea className="textarea" value={text} onChange={(e) => setText(e.target.value)} rows="14" placeholder="Akira Tanaka, Mumeishi&#10;Hiroshi Sato, Sanshukai" />
        <div className="field__hint" style={{ marginTop: 6 }}>Click "Apply" to save the participant list. Existing seeds are preserved by row order.</div>
      </div>
      <div className="card">
        <div className="card__head">
          <div>
            <div className="card__title">Seeding</div>
            <div className="card__sub">{players.filter((p) => p.seed).length} of {players.length} seeded</div>
          </div>
          <button className="btn btn--sm" onClick={() => onUpdate({ ...c, players: c.players.map((p) => ({ ...p, seed: null })) })}>Clear all</button>
        </div>
        {players.length === 0 ? (
          <div className="empty" style={{ padding: 24 }}>
            <div className="icon">🌱</div>
            <h3>No participants yet</h3>
            <div style={{ fontSize: 12 }}>Add names on the left, then "Apply".</div>
          </div>
        ) : (
          <div className="seed-list">
            {players.map((p, i) => (
              <div key={p.id} className={`seed-row ${p.seed ? "has-seed" : ""}`}>
                <span className="seed-row__rank">{p.seed ? `#${p.seed}` : ""}</span>
                <div>
                  <div className="seed-row__name">{p.name}</div>
                  <div className="seed-row__dojo">{p.dojo}</div>
                </div>
                <input className="seed-row__input" type="number" placeholder="—" value={p.seed || ""} onChange={(e) => updateSeed(i, e.target.value)} />
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

function AdminSettings({ c, tournament, onUpdate }) {
  const s = c.settings;
  const set = (k, v) => onUpdate({ ...c, settings: { ...s, [k]: v } });
  const toggleCourt = (cc) => {
    const next = c.courts.includes(cc) ? c.courts.filter((x) => x !== cc) : [...c.courts, cc].sort();
    if (next.length) onUpdate({ ...c, courts: next });
  };
  return (
    <div className="card">
      <div className="card__head"><div className="card__title">Competition settings</div></div>
      <div className="row">
        <div className="field"><label className="field__label">Display name</label><input className="input" value={c.name} onChange={(e) => onUpdate({ ...c, name: e.target.value })} /></div>
        <div className="field"><label className="field__label">Start time</label><input className="input" type="time" value={c.startTime} onChange={(e) => onUpdate({ ...c, startTime: e.target.value })} /></div>
      </div>
      {c.kind === "team" && (
        <div className="field"><label className="field__label">Team size</label><input className="input" type="number" min="1" value={c.teamSize} onChange={(e) => onUpdate({ ...c, teamSize: +e.target.value })} /></div>
      )}
      <div className="field">
        <label className="field__label">Assigned shiaijo</label>
        <div className="radio-group">
          {tournament.courts.map((cc) => (
            <button key={cc} className={`radio-pill ${c.courts.includes(cc) ? "is-active" : ""}`} onClick={() => toggleCourt(cc)}>Shiaijo {cc}</button>
          ))}
        </div>
        <div className="field__hint">Concurrency = number of courts assigned. Schedule prevents double-booking with other competitions.</div>
      </div>
      {c.format === "pools" && (
        <>
          <div className="field">
            <label className="field__label">Pool sizing mode</label>
            <div className="radio-group">
              <button className={`radio-pill ${s.poolMode === "max" ? "is-active" : ""}`} onClick={() => set("poolMode", "max")}>Max per pool</button>
              <button className={`radio-pill ${s.poolMode === "min" ? "is-active" : ""}`} onClick={() => set("poolMode", "min")}>Min per pool</button>
            </div>
          </div>
          <div className="row">
            <div className="field"><label className="field__label">{s.poolMode === "max" ? "Maximum" : "Minimum"} per pool</label><input className="input" type="number" min="3" value={s.poolSize} onChange={(e) => set("poolSize", +e.target.value)} /></div>
            <div className="field"><label className="field__label">Winners per pool</label><input className="input" type="number" min="1" value={s.winnersPerPool} onChange={(e) => set("winnersPerPool", +e.target.value)} /></div>
          </div>
        </>
      )}
      <label className="checkbox" style={{ marginBottom: 8 }}><input type="checkbox" checked={s.roundRobin} onChange={(e) => set("roundRobin", e.target.checked)} /> Round-robin in pools</label>
      <label className="checkbox" style={{ marginBottom: 8 }}><input type="checkbox" checked={s.mirror} onChange={(e) => set("mirror", e.target.checked)} /> Mirror sides (White on left)</label>
      <label className="checkbox"><input type="checkbox" checked={s.withZekken} onChange={(e) => set("withZekken", e.target.checked)} /> Use Zekken display name</label>
    </div>
  );
}

function AdminBracket({ c, t, bracket, onUpdate, onMoveCourt, tweaks }) {
  const [selected, setSelected] = useStateA(null);
  const scrollRef = useRefA(null);
  const [autoScrollId, setAutoScrollId] = useStateA(null);

  useEffectA(() => {
    // Center on current match
    const rounds = bracket?.rounds || [];
    const flat = rounds.flatMap(r => r);
    const cur = flat.find(m => m.status === "running");
    if (cur) setAutoScrollId(cur.id + "::" + Date.now());
  }, []); 

  if (!bracket || !bracket.rounds) {
    return <div className="empty"><div className="icon">⚙</div><h3>Bracket not generated yet</h3><div>Start the competition to build the bracket.</div></div>;
  }
  const select = (m, ri, mi) => setSelected({ matchId: m.id, ri, mi });
  const recordWinner = (winnerSide, mode = "ippon", ipponLetter = null) => {
    if (!selected) return;
    const m = bracket.rounds[selected.ri][selected.mi];
    const winner = winnerSide === "a" ? m.sideA : m.sideB;
    if (!winner) return;
    
    const result = {
        winner: winner,
        status: "completed",
        ipponsA: winnerSide === "a" ? [ipponLetter || "M"] : [],
        ipponsB: winnerSide === "b" ? [ipponLetter || "M"] : [],
    };
    
    window.API.recordScore(c.id, m.id, result)
        .then(() => onUpdate(c))
        .catch(err => alert(err.message));
  };
  const selectedMatch = selected ? bracket.rounds[selected.ri][selected.mi] : null;
  return (
    <div className="row" style={{ gridTemplateColumns: "1fr 360px", alignItems: "start" }}>
      <div>
        <div className="bracket-canvas" ref={scrollRef}>
          <div className="bracket-canvas__inner">
            <window.BracketTree
              rounds={bracket.rounds}
              variant={tweaks.cardVariant}
              showDojo={tweaks.showDojo}
              onMatchClick={select}
              highlightedMatchId={selected?.matchId}
              autoScrollMatchId={autoScrollId}
              scrollContainerRef={scrollRef}
            />
          </div>
        </div>
      </div>
      <div>
        {selectedMatch && selectedMatch.sideA && selectedMatch.sideB ? (
          <LiveMatchPanel match={selectedMatch} compId={c.id} courts={t?.courts || []} onMoveCourt={onMoveCourt} onRecord={recordWinner} />
        ) : selectedMatch ? (
          <div className="empty"><h3>Match not ready</h3><div style={{ fontSize: 13 }}>Waiting for upstream winners.</div></div>
        ) : (
          <div className="empty"><div className="icon">👆</div><h3>Pick a match</h3><div style={{ fontSize: 13 }}>Click any match in the bracket to record results.</div></div>
        )}
      </div>
    </div>
  );
}

function LiveMatchPanel({ match, compId, courts, onMoveCourt, onRecord }) {
  const [mode, setMode] = useStateA("tap");
  const [aPoints, setAPoints] = useStateA([]);
  const [bPoints, setBPoints] = useStateA([]);
  const [courtOpen, setCourtOpen] = useStateA(false);
  useEffectA(() => {
    setAPoints(match.score?.type === "ippon" && match.winner?.id === match.sideA?.id ? match.score.ippons || [] : []);
    setBPoints(match.score?.type === "ippon" && match.winner?.id === match.sideB?.id ? match.score.ippons || [] : []);
    setCourtOpen(false);
  }, [match.id]);
  const a = match.sideA, b = match.sideB;
  const isComplete = match.status === "complete";
  return (
    <div className="live-panel">
      <div className="live-panel__head">
        <div className="live-panel__title">Match · {match.id.slice(-6)}</div>
        <div className="live-panel__court" style={{ position: "relative" }}>
          {onMoveCourt && courts && courts.length ? (
            <>
              <button
                className="live-panel__court-btn"
                onClick={(e) => { e.stopPropagation(); setCourtOpen((o) => !o); }}
                title="Change shiaijo"
              >SHIAIJO {match.court} ▾</button>
              <span> · {match.scheduledAt || "TBA"}</span>
              {courtOpen && (
                <div className="court-popover" style={{ left: 0, top: "100%", marginTop: 4 }}>
                  {courts.map((cc) => (
                    <button
                      key={cc}
                      className={cc === match.court ? "is-current" : ""}
                      onClick={(e) => { e.stopPropagation(); setCourtOpen(false); if (cc !== match.court) onMoveCourt(compId, match.id, cc); }}
                    >{cc}</button>
                  ))}
                </div>
              )}
            </>
          ) : (
            <span>SHIAIJO {match.court} · {match.scheduledAt || "TBA"}</span>
          )}
        </div>
      </div>
      <div className="mode-tabs">
        <button className={mode === "tap" ? "is-active" : ""} onClick={() => setMode("tap")}>Tap winner</button>
        <button className={mode === "card" ? "is-active" : ""} onClick={() => setMode("card")}>Match card</button>
        <button className={mode === "scoreboard" ? "is-active" : ""} onClick={() => setMode("scoreboard")}>Scoreboard</button>
      </div>
      {mode === "tap" && (<>
        <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 8, marginBottom: 10 }}>
          <button className="card" style={{ padding: 16, textAlign: "center", cursor: "pointer", borderColor: match.winner?.id === a.id ? "var(--accent)" : "var(--line)", background: match.winner?.id === a.id ? "var(--accent)" : "var(--surface)", color: match.winner?.id === a.id ? "white" : "inherit" }} onClick={() => onRecord("a", "ippon")}>
            <div style={{ fontSize: 10, fontWeight: 700, opacity: 0.7, letterSpacing: "0.1em" }}>WHITE</div>
            <div style={{ fontWeight: 600, fontSize: 15, marginTop: 6 }}>{a.name}</div>
            <div style={{ fontSize: 12, opacity: 0.8, marginTop: 2 }}>{a.dojo}</div>
          </button>
          <button className="card" style={{ padding: 16, textAlign: "center", cursor: "pointer", borderColor: match.winner?.id === b.id ? "var(--red)" : "var(--line)", background: match.winner?.id === b.id ? "var(--red)" : "var(--surface)", color: match.winner?.id === b.id ? "white" : "inherit" }} onClick={() => onRecord("b", "ippon")}>
            <div style={{ fontSize: 10, fontWeight: 700, opacity: 0.7, letterSpacing: "0.1em" }}>RED</div>
            <div style={{ fontWeight: 600, fontSize: 15, marginTop: 6 }}>{b.name}</div>
            <div style={{ fontSize: 12, opacity: 0.8, marginTop: 2 }}>{b.dojo}</div>
          </button>
        </div>
        <div className="field__hint" style={{ textAlign: "center" }}>Tap the winner. Use Match card or Scoreboard for detail.</div>
      </>)}
      {mode === "card" && (
        <div className="score-card">
          <div className="score-side score-side--white">
            <div><div className="score-side__lbl">White</div><div className="score-side__name">{a.name}</div><div className="score-side__dojo">{a.dojo}</div></div>
            <div className="score-side__buttons"><button className="btn btn--sm btn--primary" onClick={() => onRecord("a", "ippon", "M")}>Win (Ippon)</button><button className="btn btn--sm" onClick={() => onRecord("a", "hantei")}>Hantei</button></div>
          </div>
          <div className="score-vs">VS</div>
          <div className="score-side score-side--red">
            <div><div className="score-side__lbl">Red</div><div className="score-side__name">{b.name}</div><div className="score-side__dojo">{b.dojo}</div></div>
            <div className="score-side__buttons"><button className="btn btn--sm btn--danger" onClick={() => onRecord("b", "ippon", "M")}>Win (Ippon)</button><button className="btn btn--sm" onClick={() => onRecord("b", "hantei")}>Hantei</button></div>
          </div>
        </div>
      )}
      {mode === "scoreboard" && (
        <div className="score-card">
          <div className="score-side score-side--white">
            <div><div className="score-side__lbl">White</div><div className="score-side__name">{a.name}</div></div>
            <div className="score-side__points">{[0, 1].map((i) => (<span key={i} className={`score-pt ${aPoints[i] ? "score-pt--filled" : "score-pt--empty"}`}>{aPoints[i] || "·"}</span>))}</div>
            <div className="score-side__buttons">{["M", "K", "D", "T"].map((cc) => (<button key={cc} className="ipt-btn" onClick={() => setAPoints((p) => p.length < 2 ? [...p, cc] : p)}>{cc}</button>))}<button className="ipt-btn" onClick={() => setAPoints([])}>↺</button></div>
          </div>
          <div className="score-vs">VS</div>
          <div className="score-side score-side--red">
            <div><div className="score-side__lbl">Red</div><div className="score-side__name">{b.name}</div></div>
            <div className="score-side__points">{[0, 1].map((i) => (<span key={i} className={`score-pt ${bPoints[i] ? "score-pt--filled" : "score-pt--empty"}`}>{bPoints[i] || "·"}</span>))}</div>
            <div className="score-side__buttons">{["M", "K", "D", "T"].map((cc) => (<button key={cc} className="ipt-btn" onClick={() => setBPoints((p) => p.length < 2 ? [...p, cc] : p)}>{cc}</button>))}<button className="ipt-btn" onClick={() => setBPoints([])}>↺</button></div>
          </div>
        </div>
      )}
      {mode === "scoreboard" && (
        <div className="live-panel__actions">
          <button className="btn btn--primary btn--full" disabled={aPoints.length === 0 && bPoints.length === 0} onClick={() => {
            const aWins = aPoints.length > bPoints.length;
            onRecord(aWins ? "a" : "b", "ippon", aWins ? aPoints[0] : bPoints[0]);
          }}>Submit result</button>
        </div>
      )}
      {isComplete && (
        <div style={{ marginTop: 12, padding: 10, background: "#ecfdf5", border: "1px solid #a7f3d0", borderRadius: 8, fontSize: 12.5, color: "#065f46" }}>
          ✓ Recorded — {match.winner?.name} advances
        </div>
      )}
    </div>
  );
}

function AdminPools({ c, pools, standings, tweaks, onEditScore }) {
  if (!pools || pools.length === 0) {
    return <div className="empty"><div className="icon">⏳</div><h3>Pools not drawn yet</h3><div style={{ fontSize: 13 }}>Add participants and start the competition to draw pools.</div></div>;
  }
  return (
    <div>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 14 }}>
        <div>
          <div style={{ fontSize: 14, fontWeight: 600 }}>{pools.length} pools</div>
        </div>
      </div>
      <div className="pools-grid">
        {pools.map((pool) => {
          const poolStandings = standings ? standings[pool.poolName] : null;
          return (
            <div key={pool.poolName} className="pool">
              <div className="pool__head">
                <div>
                  <div className="pool__name">{pool.poolName}</div>
                </div>
              </div>
              <table className="pool__table">
                <thead><tr><th>#</th><th>Player</th><th className="num">W</th><th className="num">L</th><th className="num">G</th><th className="num">T</th></tr></thead>
                <tbody>
                  {(poolStandings || pool.players.map((p) => ({ player: p, wins: 0, losses: 0, ipponsGiven: 0, ipponsTaken: 0 }))).map((s, i) => (
                    <tr key={s.player.name}>
                      <td style={{ color: "var(--ink-3)", fontFamily: "var(--font-mono)" }}>{i + 1}</td>
                      <td>
                        <div style={{ fontWeight: 500 }}>{s.player.name}</div>
                        {tweaks.showDojo && <div style={{ fontSize: 11, color: "var(--ink-3)" }}>{s.player.dojo}</div>}
                      </td>
                      <td className="num">{s.wins}</td>
                      <td className="num">{s.losses}</td>
                      <td className="num">{s.ipponsGiven}</td>
                      <td className="num">{s.ipponsTaken}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          );
        })}
      </div>
    </div>
  );
}

// ---------- Tournament-wide schedule (admin) ----------
function AdminSchedulePage({ tournament, onBack, onMoveCourt, onEditScore, onLogout, onViewerMode, tweaks }) {
  const [picked, setPicked] = useStateA([]);
  const [dojoText, setDojoText] = useStateA("");
  const [compFilter, setCompFilter] = useStateA("all");

  const allMatches = useMemoA(
    () => window.tournamentMatches(tournament).filter((m) => m.sideA && m.sideB),
    [tournament]
  );

  const filtered = window.applyFilters(allMatches, picked, dojoText, compFilter);

  const courts = tournament.courts;
  const byCourt = {};
  courts.forEach((cc) => byCourt[cc] = []);
  filtered.forEach((m) => { (byCourt[m.court] = byCourt[m.court] || []).push(m); });
  Object.values(byCourt).forEach((list) => list.sort((a, b) => {
    const order = { in_progress: 0, scheduled: 1, pending: 2, complete: 3 };
    if (order[a.status] !== order[b.status]) return order[a.status] - order[b.status];
    return (a.scheduledAt || "99:99").localeCompare(b.scheduledAt || "99:99");
  }));

  const matchHasFilter = (m) => window.matchHighlightedBy(m, picked, dojoText);
  const hasAnyFilter = picked.length > 0 || dojoText || compFilter !== "all";

  return (
    <div className="app">
      <AdminTopbar onLogout={onLogout} onViewerMode={onViewerMode} tournament={tournament} />
      <div className="page page--wide" style={{ maxWidth: 1400 }}>
        <div className="crumbs"><button onClick={onBack}>{tournament.name}</button><span className="sep">/</span><span>Schedule</span></div>
        <div className="page-head">
          <div>
            <h1 className="page-head__title">Tournament schedule</h1>
            <div className="page-head__sub">All matches across all competitions and shiaijo. Drag — or click court — to move a match.</div>
          </div>
        </div>

        <div className="tw-sched">
          <div className="tw-sched__filters">
            <window.PlayerMultiFilter tournament={tournament} picked={picked} setPicked={setPicked} dojoText={dojoText} setDojoText={setDojoText} />
            <select className="input" style={{ width: "auto", minWidth: 200 }} value={compFilter} onChange={(e) => setCompFilter(e.target.value)}>
              <option value="all">All competitions</option>
              {tournament.competitions.map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
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
                      <div className="tw-court__sub">{list.length} match{list.length === 1 ? "" : "es"}</div>
                    </div>
                    {liveOn && <span className="bc-live">● LIVE</span>}
                  </div>
                  <div
                    className="tw-court__list"
                    onDragOver={(e) => e.preventDefault()}
                    onDrop={(e) => {
                      e.preventDefault();
                      const data = e.dataTransfer.getData("application/json");
                      if (!data) return;
                      try {
                        const { compId, matchId } = JSON.parse(data);
                        onMoveCourt(compId, matchId, cc);
                      } catch (err) {}
                    }}
                  >
                    {list.length === 0 ? (
                      <div style={{ fontSize: 12, color: "var(--ink-3)", padding: "20px 8px", textAlign: "center" }}>No matches assigned to this shiaijo</div>
                    ) : list.map((m) => (
                      <AdminTWMatch
                        key={m.compId + m.id}
                        m={m}
                        highlight={matchHasFilter(m)}
                        courts={courts}
                        onMove={(toCourt) => onMoveCourt(m.compId, m.id, toCourt)}
                      />
                    ))}
                  </div>
                </div>
              );
            })}
          </div>
        </div>
      </div>
    </div>
  );
}

function AdminTWMatch({ m, highlight, courts, onMove }) {
  const [popoverOpen, setPopoverOpen] = useStateA(false);
  const aWin = m.winner && m.sideA && m.winner.id === m.sideA.id;
  const bWin = m.winner && m.sideB && m.winner.id === m.sideB.id;
  return (
    <div
      className={`tw-match ${m.status === "in_progress" ? "tw-match--live" : ""} ${m.status === "complete" ? "tw-match--done" : ""} ${highlight ? "tw-match--highlight" : ""}`}
      draggable
      onDragStart={(e) => {
        e.dataTransfer.setData("application/json", JSON.stringify({ compId: m.compId, matchId: m.id }));
        e.dataTransfer.effectAllowed = "move";
      }}
      style={{ cursor: "grab", position: "relative" }}
    >
      <div>
        <div className="tw-match__time">{m.scheduledAt || "—"}</div>
        <div className="tw-match__phase">{m.phase === "pool" ? m.poolName : m.round}</div>
      </div>
      <div className="tw-match__players">
        <div className={`tw-match__name ${aWin ? "tw-match__name--w" : ""}`}>{m.sideA?.name || "TBD"}</div>
        <div className={`tw-match__name ${bWin ? "tw-match__name--w" : ""}`}>{m.sideB?.name || "TBD"}</div>
        <div className="tw-match__comp">{m.compName}</div>
      </div>
      <div style={{ display: "flex", flexDirection: "column", alignItems: "flex-end", gap: 4 }}>
        {m.status === "complete" && m.score?.type === "ippon" && (
          <div style={{ fontFamily: "var(--font-mono)", fontWeight: 700, fontSize: 13 }}>{m.score.winnerPts}–{m.score.loserPts}</div>
        )}
        {m.status === "in_progress" && <span className="bc-live">●</span>}
        <button
          className="tw-match__court-btn"
          onClick={(e) => { e.stopPropagation(); setPopoverOpen((o) => !o); }}
        >Shiaijo {m.court} ▾</button>
        {popoverOpen && (
          <div className="court-popover" style={{ right: 0, top: "100%", marginTop: 4 }}>
            {courts.map((cc) => (
              <button
                key={cc}
                className={cc === m.court ? "is-current" : ""}
                onClick={(e) => { e.stopPropagation(); setPopoverOpen(false); if (cc !== m.court) onMove(cc); }}
              >{cc}</button>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

// ---------- Score editor ----------
function AdminScoreEditorPage({ tournament, onBack, onEditScore, onMoveCourt, onLogout, onViewerMode, tweaks }) {
  return (
    <div className="app">
      <AdminTopbar onLogout={onLogout} onViewerMode={onViewerMode} tournament={tournament} />
      <div className="page page--wide" style={{ maxWidth: 1200 }}>
        <div className="crumbs"><button onClick={onBack}>{tournament.name}</button><span className="sep">/</span><span>Scores</span></div>
        <div className="page-head">
          <div>
            <h1 className="page-head__title">Score editor</h1>
            <div className="page-head__sub">Update live scores or correct past matches across the tournament. Changes propagate through the bracket.</div>
          </div>
        </div>
        <AdminScoreEditor t={tournament} onEditScore={onEditScore} onMoveCourt={onMoveCourt} />
      </div>
    </div>
  );
}

function ScoreEditCourtBtn({ m, courts, onMoveCourt }) {
  const [open, setOpen] = useStateA(false);
  const ref = window.React.useRef(null);
  window.React.useEffect(() => {
    if (!open) return;
    const close = (e) => { if (ref.current && !ref.current.contains(e.target)) setOpen(false); };
    document.addEventListener("mousedown", close);
    return () => document.removeEventListener("mousedown", close);
  }, [open]);
  if (!onMoveCourt || !courts.length) {
    return <div className="score-edit-row__court">{m.court}</div>;
  }
  return (
    <div ref={ref} style={{ position: "relative" }}>
      <button
        className="score-edit-row__court score-edit-row__court--btn"
        onClick={(e) => { e.stopPropagation(); setOpen((o) => !o); }}
        title="Change shiaijo"
      >{m.court} ▾</button>
      {open && (
        <div className="court-popover" style={{ left: 0, top: "100%", marginTop: 4 }}>
          {courts.map((cc) => (
            <button
              key={cc}
              className={cc === m.court ? "is-current" : ""}
              onClick={(e) => { e.stopPropagation(); setOpen(false); if (cc !== m.court) onMoveCourt(m.compId, m.id, cc); }}
            >{cc}</button>
          ))}
        </div>
      )}
    </div>
  );
}

function AdminScoreEditor({ t, c, onEditScore, onMoveCourt, restrictToCompId, embedded }) {
  const [filter, setFilter] = useStateA("");
  const [compFilter, setCompFilter] = useStateA(restrictToCompId || "all");
  const [statusFilter, setStatusFilter] = useStateA("all");
  const [openMatch, setOpenMatch] = useStateA(null);

  const tournament = t || (c ? { competitions: [c] } : { competitions: [] });
  const allMatches = useMemoA(
    () => tournament.competitions.flatMap((cc) => window.compMatches(cc)).filter((m) => m.sideA && m.sideB),
    [tournament]
  );

  const f = filter.trim().toLowerCase();
  const filtered = allMatches.filter((m) => {
    if (restrictToCompId && m.compId !== restrictToCompId) return false;
    if (compFilter !== "all" && m.compId !== compFilter) return false;
    if (statusFilter === "live" && m.status !== "in_progress") return false;
    if (statusFilter === "scheduled" && m.status !== "scheduled") return false;
    if (statusFilter === "complete" && m.status !== "complete") return false;
    if (!f) return true;
    return [m.sideA?.name, m.sideB?.name, m.sideA?.dojo, m.sideB?.dojo].some((s) => (s || "").toLowerCase().includes(f));
  });

  const order = { in_progress: 0, scheduled: 1, complete: 2, pending: 3 };
  filtered.sort((a, b) => {
    if (order[a.status] !== order[b.status]) return order[a.status] - order[b.status];
    return (a.scheduledAt || "99:99").localeCompare(b.scheduledAt || "99:99");
  });

  return (
    <div className="score-editor">
      <div className="score-editor__bar">
        <span className="tw-sched__filter-label">Filter</span>
        <input
          className="input"
          style={{ flex: 1, minWidth: 180 }}
          placeholder="Search player, team, dojo…"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
        />
        {!restrictToCompId && (
          <select className="input" style={{ width: "auto", minWidth: 160 }} value={compFilter} onChange={(e) => setCompFilter(e.target.value)}>
            <option value="all">All competitions</option>
            {tournament.competitions.map((cc) => <option key={cc.id} value={cc.id}>{cc.name}</option>)}
          </select>
        )}
        <div className="seg">
          <button className={statusFilter === "all" ? "is-active" : ""} onClick={() => setStatusFilter("all")}>All</button>
          <button className={statusFilter === "live" ? "is-active" : ""} onClick={() => setStatusFilter("live")}>Live</button>
          <button className={statusFilter === "scheduled" ? "is-active" : ""} onClick={() => setStatusFilter("scheduled")}>Scheduled</button>
          <button className={statusFilter === "complete" ? "is-active" : ""} onClick={() => setStatusFilter("complete")}>Completed</button>
        </div>
        <span style={{ marginLeft: "auto", fontSize: 12, color: "var(--ink-3)" }}>{filtered.length} matches</span>
      </div>

      <div className="score-editor__list">
        {filtered.length === 0 && (
          <div className="empty"><div className="icon">🔍</div><h3>No matches</h3><div style={{ fontSize: 12 }}>Adjust your filters or check that competitions have started.</div></div>
        )}
        {filtered.map((m) => {
          const aWin = m.winner && m.sideA && m.winner.id === m.sideA.id;
          const bWin = m.winner && m.sideB && m.winner.id === m.sideB.id;
          const isCorrection = m.status === "complete" && m.score?.corrected;
          return (
            <div key={m.compId + m.id} className={`score-edit-row ${m.status === "in_progress" ? "score-edit-row--live" : ""} ${m.status === "complete" ? "score-edit-row--complete" : ""}`}>
              <div>
                <div className="score-edit-row__time">{m.scheduledAt || "—"}</div>
                <div style={{ fontSize: 10, color: "var(--ink-3)", marginTop: 2 }}>{m.compName}</div>
              </div>
              <ScoreEditCourtBtn m={m} courts={tournament.courts || []} onMoveCourt={onMoveCourt} />
              <div className="score-edit-row__sides">
                <div className={`score-edit-row__side ${aWin ? "score-edit-row__side--win" : ""}`}>
                  <div className="name">{m.sideA?.name}</div>
                  <div className="dojo">{m.sideA?.dojo}</div>
                </div>
                <div className="score-edit-row__score">
                  {m.status === "complete" && m.score?.type === "ippon" && `${aWin ? m.score.winnerPts : m.score.loserPts}–${bWin ? m.score.winnerPts : m.score.loserPts}`}
                  {m.status === "complete" && m.score?.type === "hantei" && "H"}
                  {m.status === "complete" && m.score?.type === "hikiwake" && "△"}
                  {m.status === "in_progress" && <span className="bc-live">●</span>}
                  {m.status === "scheduled" && <span style={{ fontSize: 11, color: "var(--ink-3)" }}>vs</span>}
                </div>
                <div className={`score-edit-row__side ${bWin ? "score-edit-row__side--win" : ""}`} style={{ textAlign: "right" }}>
                  <div className="name">{m.sideB?.name}</div>
                  <div className="dojo">{m.sideB?.dojo}</div>
                </div>
              </div>
              <div>
                {m.status === "in_progress" && <span className="bc-live">● LIVE</span>}
                {m.status === "complete" && <span style={{ fontSize: 10, color: "var(--ink-3)" }}>{isCorrection ? "Corrected" : "Final"}</span>}
              </div>
              <button className="btn btn--sm score-edit-row__edit" onClick={() => setOpenMatch(m)}>
                {m.status === "complete" ? "Correct" : "Score"}
              </button>
            </div>
          );
        })}
      </div>

      {openMatch && (
        <ScoreEditorModal
          match={openMatch}
          onClose={() => setOpenMatch(null)}
          onSubmit={(patch) => {
            onEditScore(openMatch.compId, openMatch.id, patch);
            setOpenMatch(null);
          }}
        />
      )}
    </div>
  );
}

function ScoreEditorModal({ match, onClose, onSubmit }) {
  const m = match;
  const isComplete = m.status === "complete";
  const isTeam = m.compKind === "team";
  const teamSize = m.teamSize || 5;
  if (isTeam) return <TeamScoreEditorModal match={m} teamSize={teamSize} onClose={onClose} onSubmit={onSubmit} />;
  const initialAPts = m.score?.type === "ippon" && m.winner?.id === m.sideA?.id ? m.score.ippons || [] :
                      m.score?.type === "ippon" && m.winner?.id === m.sideB?.id ? Array(m.score.loserPts || 0).fill("•") : [];
  const initialBPts = m.score?.type === "ippon" && m.winner?.id === m.sideB?.id ? m.score.ippons || [] :
                      m.score?.type === "ippon" && m.winner?.id === m.sideA?.id ? Array(m.score.loserPts || 0).fill("•") : [];

  const [aPts, setAPts] = useStateA(initialAPts);
  const [bPts, setBPts] = useStateA(initialBPts);
  // Hansoku (fouls). 2 fouls on one side awards an "H" ippon to the OTHER side.
  const [aFouls, setAFouls] = useStateA(m.score?.fouls?.a || 0);
  const [bFouls, setBFouls] = useStateA(m.score?.fouls?.b || 0);
  const [resultType, setResultType] = useStateA(m.score?.type || "ippon"); // ippon | hantei
  const [hanteiSide, setHanteiSide] = useStateA(m.score?.type === "hantei" && m.winner?.id === m.sideB?.id ? "b" : "a");
  const [statusVal, setStatusVal] = useStateA(m.status);
  const [note, setNote] = useStateA("");

  // Hansoku → ippon awarded to opponent on every 2nd foul (i.e. fouls/2 floor)
  const aHansokuPts = Math.floor(bFouls / 2); // points awarded to A from B's fouls
  const bHansokuPts = Math.floor(aFouls / 2);
  const aTotal = aPts.filter((x) => x !== "•").length + aHansokuPts;
  const bTotal = bPts.filter((x) => x !== "•").length + bHansokuPts;

  const addPt = (side, letter) => {
    if (side === "a") setAPts((p) => p.length < 2 ? [...p, letter] : p);
    else setBPts((p) => p.length < 2 ? [...p, letter] : p);
  };
  const addFoul = (side) => {
    if (side === "a") setAFouls((f) => Math.min(f + 1, 4));
    else setBFouls((f) => Math.min(f + 1, 4));
  };

  const removePt = (side, idx) => {
    if (side === "a") setAPts((p) => p.filter((_, i) => i !== idx));
    else setBPts((p) => p.filter((_, i) => i !== idx));
  };

  const submit = () => {
    let patch = {};
    if (resultType === "ippon") {
      const aLetters = aPts.filter((x) => x !== "•");
      const bLetters = bPts.filter((x) => x !== "•");
      // Add Hansoku-awarded ippons (each pair of opponent fouls = one "H" point)
      const aAll = [...aLetters, ...Array(aHansokuPts).fill("H")];
      const bAll = [...bLetters, ...Array(bHansokuPts).fill("H")];
      // Cap at 2 (kendo max)
      const aFinal = aAll.slice(0, 2);
      const bFinal = bAll.slice(0, 2);
      const winnerSide = aFinal.length > bFinal.length ? "a" : bFinal.length > aFinal.length ? "b" : null;
      const fouls = { a: aFouls, b: bFouls };
      if (!winnerSide) {
        patch = { winner: null, status: "complete", score: { type: "hikiwake", winnerPts: 0, loserPts: 0, fouls, corrected: isComplete, note } };
      } else {
        const winner = winnerSide === "a" ? m.sideA : m.sideB;
        const winPts = winnerSide === "a" ? aFinal.length : bFinal.length;
        const losePts = winnerSide === "a" ? bFinal.length : aFinal.length;
        const ippons = winnerSide === "a" ? aFinal : bFinal;
        patch = { winner, status: "complete", score: { type: "ippon", winnerPts: winPts, loserPts: losePts, ippons, fouls, corrected: isComplete, note } };
      }
    } else if (resultType === "hantei") {
      const winner = hanteiSide === "a" ? m.sideA : m.sideB;
      patch = { winner, status: "complete", score: { type: "hantei", winnerPts: 0, loserPts: 0, fouls: { a: aFouls, b: bFouls }, corrected: isComplete, note } };
    } else if (resultType === "hikiwake") {
      patch = { winner: null, status: "complete", score: { type: "hikiwake", winnerPts: 0, loserPts: 0, fouls: { a: aFouls, b: bFouls }, corrected: isComplete, note } };
    }
    if (statusVal === "in_progress") {
      patch = { ...patch, status: "in_progress", winner: null, score: { type: "ippon", winnerPts: aTotal, loserPts: bTotal, ippons: aPts, fouls: { a: aFouls, b: bFouls }, live: true, corrected: isComplete, note } };
    }
    if (statusVal === "scheduled") {
      patch = { winner: null, status: "scheduled", score: null };
    }
    onSubmit(patch);
  };

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="editor-modal" onClick={(e) => e.stopPropagation()}>
        <div className="editor-modal__head">
          <div style={{ fontSize: 11, color: "var(--ink-3)", textTransform: "uppercase", letterSpacing: "0.04em", fontWeight: 600 }}>
            {m.compName} · {m.phase === "pool" ? m.poolName : m.round} · Shiaijo {m.court} · {m.scheduledAt || "TBA"}
          </div>
          <div style={{ fontSize: 18, fontWeight: 600, marginTop: 4 }}>
            {isComplete ? "Correct match result" : m.status === "in_progress" ? "Edit live score" : "Record match result"}
          </div>
        </div>
        <div className="editor-modal__body">
          <div style={{ display: "flex", gap: 6, flexWrap: "wrap" }}>
            <button className={`chip-toggle ${resultType === "ippon" ? "is-active" : ""}`} onClick={() => setResultType("ippon")}>Ippon (timed)</button>
            <button className={`chip-toggle ${resultType === "hantei" ? "is-active" : ""}`} onClick={() => setResultType("hantei")}>Hantei</button>
            <button className={`chip-toggle ${resultType === "hikiwake" ? "is-active" : ""}`} onClick={() => setResultType("hikiwake")}>Hikiwake (draw)</button>
          </div>

          {resultType === "ippon" && (
            <>
              <div className="editor-side editor-side--white">
                <div>
                  <div className="editor-side__name">{m.sideA?.name}</div>
                  <div className="editor-side__dojo">White · {m.sideA?.dojo}</div>
                </div>
                <div className="editor-side__score">
                  {[0, 1].map((i) => (
                    <button key={i} className={`editor-side__pt ${aPts[i] ? "editor-side__pt--filled" : ""}`} onClick={() => removePt("a", i)} title={aPts[i] ? "Click to remove" : "Empty"}>
                      {aPts[i] || "·"}
                    </button>
                  ))}
                </div>
              </div>
              <div style={{ display: "flex", gap: 4, flexWrap: "wrap", marginTop: -8 }}>
                {["M", "K", "D", "T"].map((cc) => (
                  <button key={cc} className="ipt-btn" onClick={() => addPt("a", cc)}>+ {cc}</button>
                ))}
                <button className="ipt-btn" onClick={() => setAPts([])}>Clear</button>
              </div>

              <div className="editor-side editor-side--red">
                <div>
                  <div className="editor-side__name">{m.sideB?.name}</div>
                  <div className="editor-side__dojo">Red · {m.sideB?.dojo}</div>
                </div>
                <div className="editor-side__score">
                  {[0, 1].map((i) => (
                    <button key={i} className={`editor-side__pt ${bPts[i] ? "editor-side__pt--filled" : ""}`} onClick={() => removePt("b", i)}>
                      {bPts[i] || "·"}
                    </button>
                  ))}
                </div>
              </div>
              <div style={{ display: "flex", gap: 4, flexWrap: "wrap", marginTop: -8 }}>
                {["M", "K", "D", "T"].map((cc) => (
                  <button key={cc} className="ipt-btn" onClick={() => addPt("b", cc)}>+ {cc}</button>
                ))}
                <button className="ipt-btn" onClick={() => setBPts([])}>Clear</button>
              </div>
              <div className="field__hint">
                M = Men · K = Kote · D = Do · T = Tsuki. Tap a recorded ippon to remove it.
              </div>

              <div className="field">
                <label className="field__label">Hansoku (fouls)</label>
                <div className="hansoku-grid">
                  <div className="hansoku-row">
                    <span className="hansoku-row__label">White · {m.sideA?.name}</span>
                    <div className="hansoku-row__dots">
                      {[0, 1, 2, 3].map((i) => (
                        <button key={i} className={`hansoku-dot ${i < aFouls ? "is-on" : ""}`} onClick={() => setAFouls(i < aFouls ? i : i + 1)} title={`Foul ${i + 1}`}>
                          {i < aFouls ? "✕" : "·"}
                        </button>
                      ))}
                    </div>
                    <span className="hansoku-row__hint">{aHansokuPts > 0 ? `→ +${aHansokuPts} ippon to Red` : "—"}</span>
                  </div>
                  <div className="hansoku-row">
                    <span className="hansoku-row__label">Red · {m.sideB?.name}</span>
                    <div className="hansoku-row__dots">
                      {[0, 1, 2, 3].map((i) => (
                        <button key={i} className={`hansoku-dot ${i < bFouls ? "is-on" : ""}`} onClick={() => setBFouls(i < bFouls ? i : i + 1)} title={`Foul ${i + 1}`}>
                          {i < bFouls ? "✕" : "·"}
                        </button>
                      ))}
                    </div>
                    <span className="hansoku-row__hint">{bHansokuPts > 0 ? `→ +${bHansokuPts} ippon to White` : "—"}</span>
                  </div>
                </div>
                <div className="field__hint">Two fouls award an ippon to the opponent. Tap a dot to set the count.</div>
              </div>
            </>
          )}

          {resultType === "hantei" && (
            <div className="field">
              <label className="field__label">Hantei winner</label>
              <div className="radio-group">
                <button className={`radio-pill ${hanteiSide === "a" ? "is-active" : ""}`} onClick={() => setHanteiSide("a")}>White · {m.sideA?.name}</button>
                <button className={`radio-pill ${hanteiSide === "b" ? "is-active" : ""}`} onClick={() => setHanteiSide("b")}>Red · {m.sideB?.name}</button>
              </div>
            </div>
          )}

          <div className="field">
            <label className="field__label">Match status</label>
            <div className="radio-group">
              <button className={`radio-pill ${statusVal === "scheduled" ? "is-active" : ""}`} onClick={() => setStatusVal("scheduled")}>Reset to scheduled</button>
              <button className={`radio-pill ${statusVal === "in_progress" ? "is-active" : ""}`} onClick={() => setStatusVal("in_progress")}>Live (in progress)</button>
              <button className={`radio-pill ${statusVal === "complete" ? "is-active" : ""}`} onClick={() => setStatusVal("complete")}>Final</button>
            </div>
          </div>

          {isComplete && (
            <div className="field">
              <label className="field__label">Correction note (optional)</label>
              <input className="input" placeholder="e.g. Reviewed video, ippon awarded to White" value={note} onChange={(e) => setNote(e.target.value)} />
              <div className="field__hint">Recorded as audit trail. Subsequent rounds will be re-propagated automatically.</div>
            </div>
          )}
        </div>
        <div className="editor-modal__foot">
          <button className="btn" onClick={onClose}>Cancel</button>
          <button className="btn btn--primary" onClick={submit} disabled={statusVal === "complete" && resultType === "ippon" && aPts.length === 0 && bPts.length === 0}>
            {isComplete ? "Save correction" : "Save result"}
          </button>
        </div>
      </div>
    </div>
  );
}

function AdminExport({ c, t }) {
  const url = `https://${t.id}.bracket.kendo/${c.id}`;
  return (
    <div className="row">
      <div className="card">
        <div className="card__title" style={{ marginBottom: 8 }}>Export {c.name}</div>
        <div className="card__sub" style={{ marginBottom: 14 }}>Generate the official Excel workbook used during the day.</div>
        <button className="btn btn--primary btn--full">Download .xlsx</button>
        <div className="field__hint" style={{ marginTop: 10 }}>Includes pool draws, pool matches, and elimination brackets with linked formulas.</div>
      </div>
      <div className="card">
        <div className="card__title" style={{ marginBottom: 8 }}>Public viewer link</div>
        <div className="card__sub" style={{ marginBottom: 14 }}>Players & spectators see this competition's bracket, schedule and results live.</div>
        <div style={{ display: "flex", gap: 8 }}>
          <input className="input" value={url} readOnly style={{ flex: 1 }} />
          <button className="btn">Copy</button>
        </div>
      </div>
    </div>
  );
}

window.AdminApp = AdminApp;
