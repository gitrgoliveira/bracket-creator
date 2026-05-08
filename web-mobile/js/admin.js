// Admin side — single tournament. Tournament has multiple Competitions.
// Top-level: Tournament dashboard (all competitions), per-competition pages.

const { useState: useStateA, useMemo: useMemoA, useEffect: useEffectA, useRef: useRefA, useLayoutEffect: useLayoutEffectA } = React;

function Breadcrumbs({ items }) {
  return (
    <div className="crumbs">
      {items.map((item, i) => (
        <React.Fragment key={i}>
          {i > 0 && <span className="sep">/</span>}
          {item.onClick ? (
            <button onClick={item.onClick}>{item.label}</button>
          ) : (
            <span>{item.label}</span>
          )}
        </React.Fragment>
      ))}
    </div>
  );
}

function AdminApp({ tournament, onUpdate, onLogout, onViewerMode, tweaks, password }) {
  const [view, setView] = useStateA({ kind: "dashboard" });
  // Hooks must be declared unconditionally before any conditional returns.
  const [adminCompData, setAdminCompData] = useStateA(null);
  const [adminLoading, setAdminLoading] = useStateA(false);

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

  const editMatchScore = async (compId, matchId, result, match) => {
    try {
      await window.API.recordScore(compId, matchId, result, password, match);
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
      throw e;
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
        if (event.type === "competition_started" || event.type === "match_updated" || event.type === "tournament_updated") {
          if (!event.data || !event.data.competitionId || view.id === event.data.competitionId) {
            window.API.fetchCompetitionDetails(view.id).then(setAdminCompData);
          }
        }
      });
      return unsub;
    }
  }, [view.id, view.kind]);

  if (view.kind === "dashboard") {
    return <AdminDashboard
      tournament={t}
      onOpenCompetition={(id, section) => setView({ kind: "competition", id, section: section || "overview" })}
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
      onCreate={async (c) => {
        try {
          await addCompetition(c);
          setView({ kind: "competition", id: c.id, section: "participants" });
        } catch (_) {
          // error already alerted inside addCompetition
        }
      }}
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
      password={password}
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
      <button className="btn btn--ghost btn--sm" onClick={onLogout}>Sign out</button>
    </div>
  );
}

function StatusBadge({ status }) {
  const map = {
    setup: ["badge--setup", "⚙ Setup"],
    pools: ["badge--pools", "▶ Pools"],
    playoffs: ["badge--playoffs", "▶ Playoffs"],
    completed: ["badge--completed", "✔ Completed"],
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
    ms.forEach((m) => { totalMatches++; if (m.status === "completed") doneMatches++; if (m.status === "running") liveMatches++; });
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
            {running.map((c) => <CompCard key={c.id} c={c} onOpen={() => onOpenCompetition(c.id, "scores")} />)}
          </div>
        </>)}

        <div className="section-title">All competitions</div>
        <div className="tlist">
          {comps.map((c) => <CompCard key={c.id} c={c} onOpen={() => onOpenCompetition(c.id, c.status === "setup" ? "participants" : c.status === "pools" || c.status === "playoffs" ? "scores" : "overview")} />)}
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
  if (c.pools) c.pools.forEach((p) => (p.matches || []).forEach((m) => m.status === "running" && liveCount++));
  if (c.bracket && c.bracket.rounds) c.bracket.rounds.forEach((r) => (r || []).forEach((m) => m.status === "running" && liveCount++));
  return (
    <button className="tcard" onClick={onOpen}>
      <div className="tcard__head">
        <div>
          <div className="tcard__eyebrow">{window.competitionKindLabel(c)}{c.teamSize ? ` · ${c.teamSize}-person` : ""}</div>
          <div className="tcard__name">{c.name}</div>
          <div className="tcard__meta">
            {c.date && <span style={{ fontWeight: 600 }}>{formatDate(c.date)}</span>}
            {c.date && c.startTime && " · "}
            {c.startTime && `Starts ${c.startTime}`}
            {" · "}
            {c.courts.join(", ")}
          </div>
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
        <Breadcrumbs items={[
          { label: tournament.name, onClick: onCancel },
          { label: "Edit details" }
        ]} />
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
  const [poolSize, setPoolSize] = useStateA(3);
  const [winners, setWinners] = useStateA(2);
  const [startTime, setStartTime] = useStateA("09:00");
  const [date, setDate] = useStateA(tournament.date);
  const [teamSize, setTeamSize] = useStateA(5);
  const [numberPrefix, setNumberPrefix] = useStateA("");
  const [withZekken, setWithZekken] = useStateA(false);
  const [selectedCourts, setSelectedCourts] = useStateA(tournament.courts.slice(0, Math.min(2, tournament.courts.length)));

  const toggleCourt = (cc) => setSelectedCourts((sc) => sc.includes(cc) ? sc.filter((c) => c !== cc) : [...sc, cc].sort());

  const create = () => {
    const finalName = name || (kind === "team"
      ? (gender === "F" ? "Women's Teams" : "Men's Teams")
      : (gender === "F" ? "Women's Individual" : gender === "M" ? "Men's Individual" : "Individual"));
    const slug = finalName.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/(^-|-$)/g, '').substring(0, 50);
    const id = slug || "c-" + Date.now().toString(36);
    const c = window.buildCompetition({
      id,
      name: finalName,
      kind, gender,
      format,
      sampleRoster: useSample ? sampleSize : null,
      seedCount: 0, status: "setup",
      startTime,
      date,
      teamSize: kind === "team" ? teamSize : 0,
      courts: selectedCourts.length ? selectedCourts : [tournament.courts[0]],
      poolMode, poolSize, winnersPerPool: winners,
      numberPrefix: numberPrefix.trim().substring(0, 3),
      withZekkenName: kind === "individual" ? withZekken : false,
    });
    onCreate(c);
  };

  return (
    <div className="app">
      <AdminTopbar onLogout={onLogout} onViewerMode={onViewerMode} tournament={tournament} />
      <div className="page" style={{ maxWidth: 760 }}>
        <Breadcrumbs items={[
          { label: tournament.name, onClick: onCancel },
          { label: "Add competition" }
        ]} />
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

          <div className="row">
            <div className="field">
              <label className="field__label">Date</label>
              <input className="input" type="date" value={date} onChange={(e) => setDate(e.target.value)} />
              <div className="field__hint">Format: YYYY-MM-DD. For multi-day tournaments, specify which day this competition takes place.</div>
            </div>
            <div className="field">
              <label className="field__label">Match number prefix <span style={{ fontWeight: 400, color: "var(--ink-3)" }}>(optional)</span></label>
              <input className="input" placeholder="e.g. A" maxLength="3" value={numberPrefix} onChange={(e) => setNumberPrefix(e.target.value)} style={{ maxWidth: 80 }} />
              <div className="field__hint">Single letter prefix for match numbers (A1, B1…). Keeps numbers unique across competitions.</div>
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

          {kind === "individual" && (
            <div className="field">
              <label className="checkbox"><input type="checkbox" checked={withZekken} onChange={(e) => setWithZekken(e.target.checked)} /> Use Zekken display name</label>
              <div className="field__hint" style={{ marginTop: 4 }}>When enabled, participant CSV uses three columns: Name, Zekken, Dojo.</div>
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
    {
      sec: "Setup", items: [
        { id: "overview", label: "Overview" },
        { id: "participants", label: "Participants & seeds" },
        { id: "settings", label: "Settings" },
      ]
    },
    {
      sec: "Run", items: [
        pools ? { id: "pools", label: "Pools — live" } : null,
        bracket ? { id: "bracket", label: "Bracket — live" } : null,
        { id: "scores", label: "Scores — edit" },
      ].filter(Boolean)
    },
    {
      sec: "Output", items: [
        { id: "export", label: "Export & print" },
      ]
    },
  ];

  const currentItem = sections.flatMap(m => m.items).find(i => i.id === section);

  return (
    <div className="app">
      <AdminTopbar onLogout={onLogout} onViewerMode={onViewerMode} tournament={t} />
      <div className="page page--wide" style={{ maxWidth: 1400 }}>
        <Breadcrumbs items={[
          { label: t.name, onClick: onBack },
          { label: c.name, onClick: section === "overview" ? null : () => onSection("overview") },
          currentItem && section !== "overview" ? { label: currentItem.label } : null
        ].filter(Boolean)} />
        <div className="page-head">
          <div>
            <div style={{ display: "flex", alignItems: "center", gap: 10, flexWrap: "wrap" }}>
              <h1 className="page-head__title">{c.name}</h1>
              <StatusBadge status={c.status} />
            </div>
            <div className="page-head__sub">
              {window.competitionKindLabel(c)} · {c.players.length} {c.kind === "team" ? "teams" : "players"} ·
              {c.date && ` ${formatDate(c.date)} at `} {c.startTime} · {c.courts.join(", ")}
            </div>
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
            {section === "pools" && <AdminPools c={c} pools={pools} standings={standings} tweaks={tweaks} onEditScore={onEditScore} password={password} />}
            {section === "bracket" && <AdminBracket c={c} t={t} bracket={bracket} onUpdate={onUpdate} onMoveCourt={onMoveCourt} tweaks={tweaks} password={password} />}
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
  if (c.pools) c.pools.forEach((p) => (p.matches || []).forEach((m) => { total++; if (m.status === "completed") done++; if (m.status === "running") live++; }));
  if (c.bracket && c.bracket.rounds) c.bracket.rounds.forEach((r) => (r || []).forEach((m) => { if (!m.sideA || !m.sideB) return; total++; if (m.status === "completed") done++; if (m.status === "running") live++; }));
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

function LinedTextarea({ value, onChange, rows, placeholder }) {
  const textareaRef = useRefA(null);
  const numsRef = useRefA(null);
  const lineCount = Math.max(1, (value || '').split('\n').length);
  const nums = Array.from({ length: lineCount }, (_, i) => i + 1).join('\n');

  const syncScroll = () => {
    if (numsRef.current && textareaRef.current) {
      numsRef.current.scrollTop = textareaRef.current.scrollTop;
    }
  };

  return (
    <div className="lined-textarea">
      <div ref={numsRef} className="lined-textarea__nums" aria-hidden="true">{nums}</div>
      <textarea
        ref={textareaRef}
        className="lined-textarea__area"
        value={value}
        onChange={onChange}
        onScroll={syncScroll}
        rows={rows}
        placeholder={placeholder}
        spellCheck={false}
        autoCorrect="off"
        autoCapitalize="off"
      />
    </div>
  );
}

function AdminParticipants({ c, onUpdate }) {
  const [text, setText] = useStateA(() => (c.players || []).map((p) => {
    if (c.withZekkenName && p.displayName) {
      const base = `${p.name ?? ""}, ${p.displayName ?? ""}, ${p.dojo ?? ""}`;
      return p.danGrade ? `${base}, ${p.danGrade}` : base;
    }
    const base = `${p.name ?? ""}, ${p.dojo ?? ""}`;
    return p.danGrade ? `${base}, ${p.danGrade}` : base;
  }).join("\n"));
  const [dragOver, setDragOver] = useStateA(false);
  const fileRef = useRefA(null);
  const seedFileRef = useRefA(null);

  const handleSeedFile = (file) => {
    if (!file) return;
    const reader = new FileReader();
    reader.onload = (e) => {
      const raw = e.target.result;
      const np = [...(c.players || [])];
      let updatedCount = 0;
      
      raw.split(/\r?\n/).forEach((line, i) => {
        const trimmed = line.trim();
        if (!trimmed) return;
        if (i === 0 && /name/i.test(trimmed) && /seed|rank/i.test(trimmed)) return;
        
        const parts = trimmed.split(",").map(s => s.trim());
        if (parts.length >= 2) {
          const name = parts[0];
          const seed = parseInt(parts[1]);
          if (name && !isNaN(seed) && seed > 0) {
            const pIdx = np.findIndex(p => p.name.toLowerCase() === name.toLowerCase() || (p.displayName && p.displayName.toLowerCase() === name.toLowerCase()));
            if (pIdx >= 0) {
              np[pIdx] = { ...np[pIdx], seed };
              updatedCount++;
            }
          }
        }
      });
      
      if (updatedCount > 0) {
        onUpdate({ ...c, players: np });
        alert(`Successfully imported ${updatedCount} seeds.`);
      } else {
        alert("No matching participants found for the provided seeds.");
      }
    };
    reader.readAsText(file);
  };

  const handleFile = (file) => {
    if (!file) return;
    const reader = new FileReader();
    reader.onload = (e) => {
      const raw = e.target.result;
      const out = [];
      raw.split(/\r?\n/).forEach((line, i) => {
        const trimmed = line.trim();
        if (!trimmed) return;
        if (i === 0 && /name/i.test(trimmed) && /dojo|club|team/i.test(trimmed)) return;
        out.push(trimmed);
      });
      setText(out.join("\n"));
    };
    reader.readAsText(file);
  };

  const lines = text.split("\n").filter((l) => l.trim());
  const players = c.players || [];

  const sortedSeeds = players.filter(p => p.seed).map(p => p.seed).sort((a, b) => a - b);
  const gaps = [];
  if (sortedSeeds.length > 0) {
    const maxSeed = sortedSeeds[sortedSeeds.length - 1];
    for (let s = 1; s <= maxSeed; s++) {
      if (!sortedSeeds.includes(s)) gaps.push(s);
    }
  }
  const hasGaps = gaps.length > 0;

  const updateSeed = (idx, val) => {
    const np = [...(c.players || [])];
    const seed = parseInt(val);
    np[idx] = { ...np[idx], seed: isNaN(seed) || seed <= 0 ? null : seed };
    onUpdate({ ...c, players: np });
  };

  const apply = () => {
    try {
      console.log("AdminParticipants: Applying", lines.length, "lines");
      const withZekken = c.withZekkenName;
      const existingMap = new Map((c.players || []).map(p => [p.name, p]));
      const np = lines.map((line, i) => {
        const parts = line.split(",").map((s) => s.trim());
        const name = parts[0] || "";
        const existing = existingMap.get(name);
        let displayName = "";
        let dojo = "";

        let danGrade = "";
        if (withZekken) {
          displayName = parts[1] || "";
          dojo = parts[2] || "";
          danGrade = parts[3] || "";
        } else {
          dojo = parts[1] || "";
          danGrade = parts[2] || "";
        }

        return {
          id: existing?.id || `${c.id}-p${i + 1}`,
          name,
          displayName,
          dojo,
          danGrade,
          seed: existing?.seed || null
        };
      });
      console.log("AdminParticipants: Final list", np);
      onUpdate({ ...c, players: np });
      alert(`Successfully applied ${np.length} players.`);
    } catch (err) {
      console.error("AdminParticipants: Apply failed", err);
      alert("Failed to apply participants: " + err.message);
    }
  };

  const onDrop = (e) => {
    e.preventDefault();
    setDragOver(false);
    const file = e.dataTransfer.files[0];
    handleFile(file);
  };

  const pasteFromExcel = async () => {
    try {
      const clipboardText = await navigator.clipboard.readText();
      const lines = clipboardText.split(/\r?\n/);
      const out = [];
      lines.forEach((line, i) => {
        const trimmed = line.trim();
        if (!trimmed) return;
        // Detect header row: if first row matches common patterns, skip it
        if (i === 0 && /name|team|dojo|club/i.test(trimmed)) return;

        // Normalise tabs to commas
        let processed = trimmed.replace(/\t/g, ", ");
        // Strip leading row numbers if any (e.g. "1, Name, Dojo")
        processed = processed.replace(/^\d+,\s*/, "");
        out.push(processed);
      });
      setText(out.join("\n"));
    } catch (err) {
      console.error("Paste failed", err);
      alert("Failed to read clipboard: " + err.message + "\n\nMake sure you have granted clipboard permissions.");
    }
  };

  return (
    <div className="row" style={{ gridTemplateColumns: "2fr 1fr", alignItems: "start" }}>
      <div className="card">
        <div className="card__head">
          <div>
            <div className="card__title">{c.kind === "team" ? "Team list" : "Participant list"}</div>
            <div className="card__sub">
              {lines.length} entries · One per line,
              "{c.kind === "team" ? "Team name, Dojo" : c.withZekkenName ? "Name, Zekken, Dojo[, Dan]" : "Name, Dojo[, Dan]"}"
            </div>
          </div>
          <div style={{ display: "flex", gap: 6 }}>
            <button className="btn btn--sm" onClick={pasteFromExcel}>Paste from Excel</button>
            <button className="btn btn--sm" onClick={() => fileRef.current?.click()}>Upload CSV</button>
            <input ref={fileRef} type="file" accept=".csv,.txt,text/csv,text/plain" style={{ display: "none" }} onChange={(e) => handleFile(e.target.files[0])} />
            <button className="btn btn--sm btn--primary" onClick={apply} disabled={hasGaps}>Apply</button>
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
          <div className="dropzone__sub">
            {c.withZekkenName ? "Columns: name, zekken, dojo[, dan grade]" : "Columns: name, dojo[, dan grade]"} · header row optional
          </div>
        </div>

        <LinedTextarea value={text} onChange={(e) => setText(e.target.value)} rows={14} placeholder={c.kind === "team" ? "Tora A, Tora Dojo London" : c.withZekkenName ? "Akira Tanaka, TANAKA, Mumeishi" : "Akira Tanaka, Mumeishi"} />
        <div className="field__hint" style={{ marginTop: 6 }}>Click "Apply" to save the participant list. Existing seeds are preserved by row order.</div>
      </div>
      <div className="card">
        <div className="card__head">
          <div>
            <div className="card__title">Seeding</div>
            <div className="card__sub">{players.filter((p) => p.seed).length} of {players.length} seeded</div>
          </div>
          <div style={{ display: "flex", gap: 6 }}>
            <button className="btn btn--sm" onClick={() => seedFileRef.current?.click()}>Import Seeds CSV</button>
            <input ref={seedFileRef} type="file" accept=".csv,.txt,text/csv,text/plain" style={{ display: "none" }} onChange={(e) => handleSeedFile(e.target.files[0])} />
            <button className="btn btn--sm" onClick={() => onUpdate({ ...c, players: c.players.map((p) => ({ ...p, seed: null })) })}>Clear all</button>
          </div>
        </div>
        {hasGaps && (
          <div className="alert alert--error" style={{ margin: "0 16px 16px" }}>
            ❌ Seed gap detected: rank {gaps.join(", ")} {gaps.length > 1 ? "are" : "is"} missing. Seeds must be sequential (1, 2, 3…).
          </div>
        )}
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
  const set = (k, v) => onUpdate({ ...c, [k]: v });
  const toggleCourt = (cc) => {
    const next = c.courts.includes(cc) ? c.courts.filter((x) => x !== cc) : [...c.courts, cc].sort();
    if (next.length) onUpdate({ ...c, courts: next });
  };
  return (
    <div className="card">
      <div className="card__head"><div className="card__title">Competition settings</div></div>
      <div className="row">
        <div className="field"><label className="field__label">Display name</label><input className="input" value={c.name} onChange={(e) => onUpdate({ ...c, name: e.target.value })} /></div>
        <div className="field"><label className="field__label">Date</label><input className="input" type="date" value={c.date} onChange={(e) => onUpdate({ ...c, date: e.target.value })} /><div className="field__hint">Format: YYYY-MM-DD</div></div>
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
              <button className={`radio-pill ${c.poolSizeMode === "max" ? "is-active" : ""}`} onClick={() => set("poolSizeMode", "max")}>Max per pool</button>
              <button className={`radio-pill ${c.poolSizeMode === "min" ? "is-active" : ""}`} onClick={() => set("poolSizeMode", "min")}>Min per pool</button>
            </div>
          </div>
          <div className="row">
            <div className="field"><label className="field__label">{c.poolSizeMode === "max" ? "Maximum" : "Minimum"} per pool</label><input className="input" type="number" min="3" value={c.poolSize} onChange={(e) => set("poolSize", +e.target.value)} /></div>
            <div className="field"><label className="field__label">Winners per pool</label><input className="input" type="number" min="1" value={c.poolWinners} onChange={(e) => set("poolWinners", +e.target.value)} /></div>
          </div>
        </>
      )}
      <div className="field">
        <label className="field__label">Match number prefix <span style={{ fontWeight: 400, color: "var(--ink-3)" }}>(optional)</span></label>
        <input className="input" placeholder="e.g. A" maxLength="3" value={c.numberPrefix || ""} onChange={(e) => set("numberPrefix", e.target.value.substring(0, 3))} style={{ maxWidth: 80 }} />
        <div className="field__hint">Single letter prefix for match numbers (A1, B1…). Keeps numbers unique across competitions.</div>
      </div>
      <label className="checkbox" style={{ marginBottom: 8 }}><input type="checkbox" checked={c.roundRobin} onChange={(e) => set("roundRobin", e.target.checked)} /> Round-robin in pools</label>
      <label className="checkbox" style={{ marginBottom: 8 }}><input type="checkbox" checked={c.mirror} onChange={(e) => set("mirror", e.target.checked)} /> Mirror sides (White on left)</label>
      {c.kind === "individual" && (
        <label className="checkbox"><input type="checkbox" checked={c.withZekkenName} onChange={(e) => set("withZekkenName", e.target.checked)} /> Use Zekken display name</label>
      )}
    </div>
  );
}

function AdminBracket({ c, t, bracket, onUpdate, onMoveCourt, tweaks, password }) {
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
      score: { type: "ippon", winnerPts: 1, loserPts: 0, ippons: [ipponLetter || "M"], fouls: { a: 0, b: 0 } },
    };

    window.API.recordScore(c.id, m.id, result, password, m)
      .then(() => onUpdate(c))
      .catch(err => alert(err.message));
  };

  const overrideWinner = (winnerName) => {
    if (!selected) return;
    window.API.overrideBracketWinner(c.id, selected.matchId, winnerName, password)
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
          <LiveMatchPanel 
            match={selectedMatch} 
            compId={c.id} 
            courts={t?.courts || []} 
            onMoveCourt={onMoveCourt} 
            onRecord={recordWinner}
            onOverride={overrideWinner}
          />
        ) : selectedMatch ? (
          <div className="empty"><h3>Match not ready</h3><div style={{ fontSize: 13 }}>Waiting for upstream winners.</div></div>
        ) : (
          <div className="empty"><div className="icon">👆</div><h3>Pick a match</h3><div style={{ fontSize: 13 }}>Click any match in the bracket to record results.</div></div>
        )}
      </div>
    </div>
  );
}

function LiveMatchPanel({ match, compId, courts, onMoveCourt, onRecord, onOverride }) {
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
  const isComplete = match.status === "completed";
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
          <button className="card" style={{ padding: 16, textAlign: "center", cursor: "pointer", borderColor: match.winner?.id === a.id ? "var(--red)" : "var(--line)", background: match.winner?.id === a.id ? "var(--red)" : "var(--surface)", color: match.winner?.id === a.id ? "white" : "inherit" }} onClick={() => onRecord("a", "ippon")}>
            <div style={{ fontSize: 10, fontWeight: 700, opacity: 0.7, letterSpacing: "0.1em", color: "var(--red)" }}>AKA (RED)</div>
            <div style={{ fontWeight: 600, fontSize: 15, marginTop: 6 }}>{a.name}</div>
            <div style={{ fontSize: 12, opacity: 0.8, marginTop: 2 }}>{a.dojo}</div>
          </button>
          <button className="card" style={{ padding: 16, textAlign: "center", cursor: "pointer", borderColor: match.winner?.id === b.id ? "var(--accent)" : "var(--line)", background: match.winner?.id === b.id ? "var(--accent)" : "var(--surface)", color: match.winner?.id === b.id ? "white" : "inherit" }} onClick={() => onRecord("b", "ippon")}>
            <div style={{ fontSize: 10, fontWeight: 700, opacity: 0.7, letterSpacing: "0.1em" }}>SHIRO (WHITE)</div>
            <div style={{ fontWeight: 600, fontSize: 15, marginTop: 6 }}>{b.name}</div>
            <div style={{ fontSize: 12, opacity: 0.8, marginTop: 2 }}>{b.dojo}</div>
          </button>
        </div>
        <div className="field__hint" style={{ textAlign: "center" }}>Tap the winner. Use Match card or Scoreboard for detail.</div>
      </>)}
      {mode === "card" && (
        <div className="score-card">
          <div className="score-side score-side--red">
            <div><div className="score-side__lbl">Aka (Red)</div><div className="score-side__name">{a.name}</div><div className="score-side__dojo">{a.dojo}</div></div>
            <div className="score-side__buttons"><button className="btn btn--sm btn--danger" onClick={() => onRecord("a", "ippon", "M")}>Win (Ippon)</button><button className="btn btn--sm" onClick={() => onRecord("a", "hantei")}>Hantei</button></div>
          </div>
          <div className="score-vs">VS</div>
          <div className="score-side score-side--white">
            <div><div className="score-side__lbl">Shiro (White)</div><div className="score-side__name">{b.name}</div><div className="score-side__dojo">{b.dojo}</div></div>
            <div className="score-side__buttons"><button className="btn btn--sm btn--primary" onClick={() => onRecord("b", "ippon", "M")}>Win (Ippon)</button><button className="btn btn--sm" onClick={() => onRecord("b", "hantei")}>Hantei</button></div>
          </div>
        </div>
      )}
      {mode === "scoreboard" && (
        <div className="score-card">
          <div className="score-side score-side--red">
            <div><div className="score-side__lbl">Aka (Red)</div><div className="score-side__name">{a.name}</div></div>
            <div className="score-side__points">{[0, 1].map((i) => (<span key={i} className={`score-pt score-pt--aka ${aPoints[i] ? "score-pt--filled" : "score-pt--empty"}`}>{aPoints[i] || "·"}</span>))}</div>
            <div className="score-side__buttons">{["M", "K", "D", "T"].map((cc) => (<button key={cc} className="ipt-btn ipt-btn--aka" onClick={() => setAPoints((p) => p.length < 2 ? [...p, cc] : p)}>{cc}</button>))}<button className="ipt-btn" onClick={() => setAPoints([])}>↺</button></div>
          </div>
          <div className="score-vs">VS</div>
          <div className="score-side score-side--white">
            <div><div className="score-side__lbl">Shiro (White)</div><div className="score-side__name">{b.name}</div></div>
            <div className="score-side__points">{[0, 1].map((i) => (<span key={i} className={`score-pt score-pt--shiro ${bPoints[i] ? "score-pt--filled" : "score-pt--empty"}`}>{bPoints[i] || "·"}</span>))}</div>
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
      <div style={{ marginTop: 14, paddingTop: 14, borderTop: "1px dashed var(--line)" }}>
        <button className="btn btn--sm btn--full" onClick={() => {
          const name = prompt("Enter the name of the winner to override:", match.winner?.name || match.sideA?.name);
          if (name) onOverride(name);
        }}>Force winner (manual override)</button>
      </div>
    </div>
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

  if (!pools || pools.length === 0) {
    return <div className="empty"><div className="icon">⏳</div><h3>Pools not drawn yet</h3><div style={{ fontSize: 13 }}>Add participants and start the competition to draw pools.</div></div>;
  }
  return (
    <div>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 14 }}>
        <div>
          <div style={{ fontSize: 14, fontWeight: 600 }}>{pools.length} pools</div>
        </div>
        <button className="btn btn--sm btn--danger" onClick={resetOverrides}>Reset all overrides</button>
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
            </div>
          );
        })}
      </div>
    </div>
  );
}

// ---------- Tournament-wide schedule (admin) ----------
// Estimate minutes from HH:MM string; returns null if invalid
function timeToMinutes(t) {
  if (!t) return null;
  const [h, m] = t.split(":").map(Number);
  if (isNaN(h) || isNaN(m)) return null;
  return h * 60 + m;
}
function minutesToTime(mins) {
  const h = Math.floor(mins / 60) % 24;
  const m = mins % 60;
  return `${String(h).padStart(2, "0")}:${String(m).padStart(2, "0")}`;
}

function AdminSchedulePage({ tournament, onBack, onMoveCourt, onEditScore, onLogout, onViewerMode, tweaks, password }) {
  const [picked, setPicked] = useStateA([]);
  const [dojoText, setDojoText] = useStateA("");
  const [compFilter, setCompFilter] = useStateA("all");
  const [tab, setTab] = useStateA("courts"); // courts | timeline
  const [matchDuration, setMatchDuration] = useStateA(7); // minutes per match estimate
  const [breakLabel, setBreakLabel] = useStateA("Lunch break");
  const [breakTime, setBreakTime] = useStateA("12:00");
  const [breakDur, setBreakDur] = useStateA(60);
  // Per-competition auto-schedule: startTime + duration
  const [autoComp, setAutoComp] = useStateA(tournament.competitions[0]?.id || "");
  const [autoStart, setAutoStart] = useStateA(tournament.competitions[0]?.startTime || "09:00");
  const [autoSaving, setAutoSaving] = useStateA(false);

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
    const order = { running: 0, scheduled: 1, completed: 2 };
    const ao = order[a.status] ?? 1;
    const bo = order[b.status] ?? 1;
    if (ao !== bo) return ao - bo;
    return (a.scheduledAt || "99:99").localeCompare(b.scheduledAt || "99:99");
  }));

  const matchHasFilter = (m) => window.matchHighlightedBy(m, picked, dojoText);
  const hasAnyFilter = picked.length > 0 || dojoText || compFilter !== "all";

  // Duration estimation: earliest start to latest finish across all matches
  const scheduledWithTimes = allMatches.filter(m => m.scheduledAt);
  const firstTime = scheduledWithTimes.length > 0 ? scheduledWithTimes.reduce((mn, m) => !mn || m.scheduledAt < mn ? m.scheduledAt : mn, null) : null;
  const lastTime = scheduledWithTimes.length > 0 ? scheduledWithTimes.reduce((mx, m) => !mx || m.scheduledAt > mx ? m.scheduledAt : mx, null) : null;
  const durationEstimate = firstTime && lastTime ? (() => {
    const a = timeToMinutes(firstTime);
    const b = timeToMinutes(lastTime);
    if (a === null || b === null) return null;
    const diff = b - a + matchDuration;
    return `${Math.floor(diff / 60)}h ${diff % 60}m`;
  })() : null;

  const saveMatchTime = async (m, newTime) => {
    try {
      await window.API.updateMatchTime(m.compId, m.id, newTime, password);
    } catch (e) {
      alert("Failed to update time: " + e.message);
    }
  };

  // Auto-schedule: assign sequential times to matches within a competition per court
  const autoSchedule = async () => {
    const comp = tournament.competitions.find(c => c.id === autoComp);
    if (!comp) return;
    const compMatches = filtered.filter(m => m.compId === autoComp);
    const byCt = {};
    courts.forEach(cc => byCt[cc] = []);
    compMatches.forEach(m => (byCt[m.court] = byCt[m.court] || []).push(m));

    setAutoSaving(true);
    try {
      // Assign times: each court runs in parallel from autoStart, matches spaced by matchDuration
      for (const [ct, list] of Object.entries(byCt)) {
        let cursor = timeToMinutes(autoStart) || 540;
        for (const m of list.sort((a, b) => (a.scheduledAt || "99:99").localeCompare(b.scheduledAt || "99:99"))) {
          await window.API.updateMatchTime(m.compId, m.id, minutesToTime(cursor), password);
          cursor += matchDuration;
        }
      }
    } catch (e) {
      alert("Auto-schedule failed: " + e.message);
    } finally {
      setAutoSaving(false);
    }
  };

  return (
    <div className="app">
      <AdminTopbar onLogout={onLogout} onViewerMode={onViewerMode} tournament={tournament} />
      <div className="page page--wide" style={{ maxWidth: 1400 }}>
        <Breadcrumbs items={[
          { label: tournament.name, onClick: onBack },
          { label: "Schedule" }
        ]} />
        <div className="page-head">
          <div>
            <h1 className="page-head__title">Tournament schedule</h1>
            <div className="page-head__sub">Set match times, assign courts, and estimate duration.</div>
          </div>
        </div>

        {/* Duration summary + auto-schedule controls */}
        <div className="card card--pad-lg" style={{ marginBottom: 16 }}>
          <div className="row" style={{ alignItems: "flex-end", flexWrap: "wrap" }}>
            <div className="field" style={{ minWidth: 180 }}>
              <label className="field__label">Competition</label>
              <select className="input" value={autoComp} onChange={e => { setAutoComp(e.target.value); const c = tournament.competitions.find(x => x.id === e.target.value); if (c?.startTime) setAutoStart(c.startTime); }}>
                {tournament.competitions.map(c => <option key={c.id} value={c.id}>{c.name}</option>)}
              </select>
            </div>
            <div className="field">
              <label className="field__label">Start time</label>
              <input className="input" type="time" value={autoStart} onChange={e => setAutoStart(e.target.value)} style={{ width: 120 }} />
            </div>
            <div className="field">
              <label className="field__label">Minutes per match</label>
              <input className="input" type="number" min="1" max="60" value={matchDuration} onChange={e => setMatchDuration(+e.target.value)} style={{ width: 80 }} />
            </div>
            <button className="btn btn--primary" onClick={autoSchedule} disabled={autoSaving} style={{ alignSelf: "flex-end" }}>
              {autoSaving ? "Scheduling…" : "Auto-schedule competition"}
            </button>
            {durationEstimate && (
              <div style={{ alignSelf: "flex-end", fontSize: 13, color: "var(--ink-3)", paddingBottom: 2 }}>
                Est. duration: <strong>{durationEstimate}</strong> · {scheduledWithTimes.length} of {allMatches.length} timed
              </div>
            )}
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
              const liveOn = list.find((m) => m.status === "running");
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
                      } catch (err) { }
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
                        onTimeChange={(newTime) => saveMatchTime(m, newTime)}
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

function AdminTWMatch({ m, highlight, courts, onMove, onTimeChange }) {
  const [popoverOpen, setPopoverOpen] = useStateA(false);
  const [editingTime, setEditingTime] = useStateA(false);
  const [timeVal, setTimeVal] = useStateA(m.scheduledAt || "");
  const aWin = m.winner && m.sideA && m.winner.id === m.sideA.id;
  const bWin = m.winner && m.sideB && m.winner.id === m.sideB.id;
  const submitTime = (e) => {
    e.preventDefault();
    setEditingTime(false);
    if (onTimeChange && timeVal !== m.scheduledAt) onTimeChange(timeVal);
  };
  return (
    <div
      className={`tw-match ${m.status === "running" ? "tw-match--live" : ""} ${m.status === "completed" ? "tw-match--done" : ""} ${highlight ? "tw-match--highlight" : ""}`}
      draggable
      onDragStart={(e) => {
        e.dataTransfer.setData("application/json", JSON.stringify({ compId: m.compId, matchId: m.id }));
        e.dataTransfer.effectAllowed = "move";
      }}
      style={{ cursor: "grab", position: "relative" }}
    >
      <div>
        {editingTime ? (
          <form onSubmit={submitTime} style={{ display: "flex", gap: 2 }}>
            <input
              autoFocus
              type="time"
              value={timeVal}
              onChange={e => setTimeVal(e.target.value)}
              onBlur={submitTime}
              className="input"
              style={{ width: 80, padding: "2px 4px", fontSize: 12, height: 26 }}
              onClick={e => e.stopPropagation()}
            />
          </form>
        ) : (
          <button
            className="tw-match__time tw-match__time--editable"
            onClick={(e) => { e.stopPropagation(); if (onTimeChange) { setTimeVal(m.scheduledAt || ""); setEditingTime(true); } }}
            title="Click to set time"
          >
            {m.scheduledAt || <span style={{ color: "var(--ink-4)", fontWeight: 400 }}>—</span>}
            {onTimeChange && <span style={{ fontSize: 9, color: "var(--ink-4)", marginLeft: 2 }}>✎</span>}
          </button>
        )}
        <div className="tw-match__phase">{m.phase === "pool" ? m.poolName : m.round}</div>
      </div>
      <div className="tw-match__players">
        <div className={`tw-match__name ${aWin ? "tw-match__name--w" : ""}`}>
          <span className="tw-match__badge tw-match__badge--aka">A</span>
          {m.sideA?.name || "TBD"}
        </div>
        <div className={`tw-match__name ${bWin ? "tw-match__name--w" : ""}`}>
          <span className="tw-match__badge tw-match__badge--shiro">S</span>
          {m.sideB?.name || "TBD"}
        </div>
        <div className="tw-match__comp">{m.compName}</div>
      </div>
      <div style={{ display: "flex", flexDirection: "column", alignItems: "flex-end", gap: 4 }}>
        {m.status === "completed" && (
          <div style={{ fontFamily: "var(--font-mono)", fontWeight: 700, fontSize: 13 }}>{window.formatIpponsScore(m.ipponsA, m.ipponsB, m.score, m.decision)}</div>
        )}
        {m.status === "running" && <span className="bc-live">●</span>}
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
        <Breadcrumbs items={[
          { label: tournament.name, onClick: onBack },
          { label: "Scores" }
        ]} />
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
    if (statusFilter === "live" && m.status !== "running") return false;
    if (statusFilter === "scheduled" && m.status !== "scheduled") return false;
    if (statusFilter === "complete" && m.status !== "completed") return false;
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
          const isCorrection = m.status === "completed" && m.score?.corrected;
          return (
            <div key={m.compId + m.id} className={`score-edit-row ${m.status === "running" ? "score-edit-row--live" : ""} ${m.status === "completed" ? "score-edit-row--complete" : ""}`}>
              <div>
                <div className="score-edit-row__time">{m.scheduledAt || "—"}</div>
                <div style={{ fontSize: 10, color: "var(--ink-3)", marginTop: 2 }}>{m.compName}</div>
              </div>
              <ScoreEditCourtBtn m={m} courts={tournament.courts || []} onMoveCourt={onMoveCourt} />
              <div className="score-edit-row__sides">
                <div className={`score-edit-row__side ${aWin ? "score-edit-row__side--win" : ""}`}>
                  <span className="se-color-badge se-color-badge--aka">AKA</span>
                  <div className="name">{m.sideA?.name}</div>
                  <div className="dojo">{m.sideA?.dojo}</div>
                </div>
                <div className="score-edit-row__score">
                  {m.status === "completed" && window.formatIpponsScore(m.ipponsA, m.ipponsB, m.score, m.decision)}
                  {m.status === "running" && <span className="bc-live">●</span>}
                  {m.status === "scheduled" && <span style={{ fontSize: 11, color: "var(--ink-3)" }}>vs</span>}
                </div>
                <div className={`score-edit-row__side ${bWin ? "score-edit-row__side--win" : ""}`} style={{ textAlign: "right" }}>
                  <div className="name">{m.sideB?.name}</div>
                  <div className="dojo">{m.sideB?.dojo}</div>
                  <span className="se-color-badge se-color-badge--shiro">SHIRO</span>
                </div>
              </div>
              <div>
                {m.status === "running" && <span className="bc-live">● LIVE</span>}
                {m.status === "completed" && <span style={{ fontSize: 10, color: "var(--ink-3)" }}>{isCorrection ? "Corrected" : "Final"}</span>}
              </div>
              <button className="btn btn--sm score-edit-row__edit" onClick={() => setOpenMatch(m)}>
                {m.status === "completed" ? "Correct" : "Score"}
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
            onEditScore(openMatch.compId, openMatch.id, patch, openMatch);
            setOpenMatch(null);
          }}
        />
      )}
    </div>
  );
}

function ScoreEditorModal({ match, onClose, onSubmit }) {
  const m = match;
  const isComplete = m.status === "completed";
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
      const aAll = [...aLetters, ...Array(aHansokuPts).fill("H")];
      const bAll = [...bLetters, ...Array(bHansokuPts).fill("H")];
      const aFinal = aAll.slice(0, 2);
      const bFinal = bAll.slice(0, 2);
      const winnerSide = aFinal.length > bFinal.length ? "a" : bFinal.length > aFinal.length ? "b" : null;
      const fouls = { a: aFouls, b: bFouls };
      if (!winnerSide) {
        patch = { winner: null, ipponsA: aFinal, ipponsB: bFinal, status: "completed", score: { type: "hikiwake", winnerPts: 0, loserPts: 0, fouls, corrected: isComplete, note } };
      } else {
        const winner = winnerSide === "a" ? m.sideA : m.sideB;
        const winPts = winnerSide === "a" ? aFinal.length : bFinal.length;
        const losePts = winnerSide === "a" ? bFinal.length : aFinal.length;
        const ippons = winnerSide === "a" ? aFinal : bFinal;
        patch = { winner, ipponsA: aFinal, ipponsB: bFinal, status: "completed", score: { type: "ippon", winnerPts: winPts, loserPts: losePts, ippons, fouls, corrected: isComplete, note } };
      }
    } else if (resultType === "hantei") {
      const winner = hanteiSide === "a" ? m.sideA : m.sideB;
      patch = { winner, ipponsA: [], ipponsB: [], status: "completed", score: { type: "hantei", winnerPts: 0, loserPts: 0, fouls: { a: aFouls, b: bFouls }, corrected: isComplete, note } };
    } else if (resultType === "hikiwake") {
      patch = { winner: null, ipponsA: [], ipponsB: [], status: "completed", score: { type: "hikiwake", winnerPts: 0, loserPts: 0, fouls: { a: aFouls, b: bFouls }, corrected: isComplete, note } };
    }
    if (statusVal === "running") {
      patch = { ...patch, status: "running", winner: null, ipponsA: aPts.filter(x => x !== "•"), ipponsB: bPts.filter(x => x !== "•"), score: { type: "ippon", winnerPts: aTotal, loserPts: bTotal, ippons: aPts, fouls: { a: aFouls, b: bFouls }, live: true, corrected: isComplete, note } };
    }
    if (statusVal === "scheduled") {
      patch = { winner: null, status: "scheduled", score: null, ipponsA: [], ipponsB: [] };
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
            {isComplete ? "Correct match result" : m.status === "running" ? "Edit live score" : "Record match result"}
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
              <div className="editor-side editor-side--red">
                <div>
                  <div className="editor-side__name">{m.sideA?.name}</div>
                  <div className="editor-side__dojo">Aka (Red) · {m.sideA?.dojo}</div>
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

              <div className="editor-side editor-side--white">
                <div>
                  <div className="editor-side__name">{m.sideB?.name}</div>
                  <div className="editor-side__dojo">Shiro (White) · {m.sideB?.dojo}</div>
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
                    <span className="hansoku-row__label">Aka · {m.sideA?.name}</span>
                    <div className="hansoku-row__dots">
                      {[0, 1, 2, 3].map((i) => (
                        <button key={i} className={`hansoku-dot ${i < aFouls ? "is-on" : ""}`} onClick={() => setAFouls(i < aFouls ? i : i + 1)} title={`Foul ${i + 1}`}>
                          {i < aFouls ? "✕" : "·"}
                        </button>
                      ))}
                    </div>
                    <span className="hansoku-row__hint">{aHansokuPts > 0 ? `→ +${aHansokuPts} ippon to Shiro` : "—"}</span>
                  </div>
                  <div className="hansoku-row">
                    <span className="hansoku-row__label">Shiro · {m.sideB?.name}</span>
                    <div className="hansoku-row__dots">
                      {[0, 1, 2, 3].map((i) => (
                        <button key={i} className={`hansoku-dot ${i < bFouls ? "is-on" : ""}`} onClick={() => setBFouls(i < bFouls ? i : i + 1)} title={`Foul ${i + 1}`}>
                          {i < bFouls ? "✕" : "·"}
                        </button>
                      ))}
                    </div>
                    <span className="hansoku-row__hint">{bHansokuPts > 0 ? `→ +${bHansokuPts} ippon to Aka` : "—"}</span>
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
                <button className={`radio-pill ${hanteiSide === "a" ? "is-active" : ""}`} onClick={() => setHanteiSide("a")}>Aka · {m.sideA?.name}</button>
                <button className={`radio-pill ${hanteiSide === "b" ? "is-active" : ""}`} onClick={() => setHanteiSide("b")}>Shiro · {m.sideB?.name}</button>
              </div>
            </div>
          )}

          <div className="field">
            <label className="field__label">Match status</label>
            <div className="radio-group">
              <button className={`radio-pill ${statusVal === "scheduled" ? "is-active" : ""}`} onClick={() => setStatusVal("scheduled")}>Reset to scheduled</button>
              <button className={`radio-pill ${statusVal === "running" ? "is-active" : ""}`} onClick={() => setStatusVal("running")}>Live (in progress)</button>
              <button className={`radio-pill ${statusVal === "completed" ? "is-active" : ""}`} onClick={() => setStatusVal("completed")}>Final</button>
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
          <button className="btn btn--primary" onClick={submit} disabled={statusVal === "completed" && resultType === "ippon" && aPts.length === 0 && bPts.length === 0}>
            {isComplete ? "Save correction" : "Save result"}
          </button>
        </div>
      </div>
    </div>
  );
}

const TEAM_POSITIONS = ["Senpou", "Jihou", "Chuken", "Fukushou", "Taishou", "6th", "7th", "8th", "9th"];

function TeamScoreEditorModal({ match, teamSize, onClose, onSubmit }) {
  const m = match;
  const isComplete = m.status === "completed";
  const positions = TEAM_POSITIONS.slice(0, teamSize);
  const [statusVal, setStatusVal] = useStateA(m.status);
  const [note, setNote] = useStateA("");

  const existingSub = m.subResults || [];
  const initSubs = positions.map((_, idx) => {
    const existing = existingSub.find(s => s.position === idx + 1);
    return {
      aPts: existing ? (existing.ipponsA || []).filter(x => x !== "•") : [],
      bPts: existing ? (existing.ipponsB || []).filter(x => x !== "•") : [],
      aFouls: existing ? existing.hansokuA || 0 : 0,
      bFouls: existing ? existing.hansokuB || 0 : 0,
    };
  });
  const [subs, setSubs] = useStateA(initSubs);

  const updateSub = (idx, fn) => setSubs(prev => prev.map((s, i) => i === idx ? fn(s) : s));

  const subTotals = subs.map(s => {
    const aH = Math.floor(s.bFouls / 2);
    const bH = Math.floor(s.aFouls / 2);
    const aT = s.aPts.length + aH;
    const bT = s.bPts.length + bH;
    const winner = aT > bT ? "a" : bT > aT ? "b" : null;
    return { aTotal: aT, bTotal: bT, aHansoku: aH, bHansoku: bH, winner };
  });

  const ivA = subTotals.filter(s => s.winner === "a").length;
  const ivB = subTotals.filter(s => s.winner === "b").length;
  const pwA = subTotals.reduce((sum, s) => sum + s.aTotal, 0);
  const pwB = subTotals.reduce((sum, s) => sum + s.bTotal, 0);
  const teamWinner = ivA > ivB ? "a" : ivB > ivA ? "b" : pwA > pwB ? "a" : pwB > pwA ? "b" : null;

  const submit = () => {
    const subResults = subs.map((s, idx) => {
      const t = subTotals[idx];
      const aAll = [...s.aPts, ...Array(t.aHansoku).fill("H")].slice(0, 2);
      const bAll = [...s.bPts, ...Array(t.bHansoku).fill("H")].slice(0, 2);
      const w = t.winner === "a" ? m.sideA : t.winner === "b" ? m.sideB : null;
      return {
        position: idx + 1,
        sideA: typeof m.sideA === "object" ? m.sideA?.name : m.sideA,
        sideB: typeof m.sideB === "object" ? m.sideB?.name : m.sideB,
        ipponsA: aAll,
        ipponsB: bAll,
        hansokuA: s.aFouls,
        hansokuB: s.bFouls,
        winner: w ? (typeof w === "object" ? w.name : w) : "",
        decision: t.winner === null ? "hikewake" : "",
      };
    });

    const winner = teamWinner === "a" ? m.sideA : teamWinner === "b" ? m.sideB : null;

    let patch = {
      winner,
      status: statusVal === "scheduled" ? "scheduled" : statusVal === "running" ? "running" : "completed",
      ipponsA: [],
      ipponsB: [],
      score: { type: teamWinner ? "ippon" : "hikiwake", winnerPts: teamWinner === "a" ? ivA : ivB, loserPts: teamWinner === "a" ? ivB : ivA, fouls: { a: 0, b: 0 }, corrected: isComplete, note },
      subResults,
    };
    if (statusVal === "scheduled") {
      patch = { winner: null, status: "scheduled", score: null, ipponsA: [], ipponsB: [], subResults: [] };
    }
    onSubmit(patch);
  };

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="editor-modal editor-modal--team" onClick={(e) => e.stopPropagation()}>
        <div className="editor-modal__head">
          <div style={{ fontSize: 11, color: "var(--ink-3)", textTransform: "uppercase", letterSpacing: "0.04em", fontWeight: 600 }}>
            {m.compName} · {m.phase === "pool" ? m.poolName : m.round} · Shiaijo {m.court} · {m.scheduledAt || "TBA"}
          </div>
          <div style={{ fontSize: 18, fontWeight: 600, marginTop: 4 }}>
            {isComplete ? "Correct team result" : m.status === "running" ? "Edit live team score" : "Record team result"}
          </div>
        </div>
        <div className="editor-modal__body">
          <div className="team-header">
            <div className="team-header__side team-header__side--red"><span className="team-header__color">AKA</span>{m.sideA?.name || m.sideA}</div>
            <div className="team-header__vs">vs</div>
            <div className="team-header__side team-header__side--white"><span className="team-header__color">SHIRO</span>{m.sideB?.name || m.sideB}</div>
          </div>

          {positions.map((pos, idx) => {
            const s = subs[idx];
            const t = subTotals[idx];
            return (
              <div key={idx} className="team-sub-match">
                <div className="team-sub-match__pos">{pos}</div>
                <div className="team-sub-match__row">
                  <div className="team-sub-match__side">
                    <div className="team-sub-match__pts">
                      {[0, 1].map(i => (
                        <button key={i} className={`editor-side__pt ${s.aPts[i] ? "editor-side__pt--filled" : ""}`}
                          onClick={() => updateSub(idx, prev => ({ ...prev, aPts: prev.aPts.filter((_, j) => j !== i) }))}>{s.aPts[i] || "·"}</button>
                      ))}
                    </div>
                    <div className="team-sub-match__btns">
                      {["M", "K", "D", "T"].map(c => (
                        <button key={c} className="ipt-btn ipt-btn--sm" onClick={() => updateSub(idx, prev => ({ ...prev, aPts: prev.aPts.length < 2 ? [...prev.aPts, c] : prev.aPts }))}>{c}</button>
                      ))}
                    </div>
                    <div className="team-sub-match__fouls">
                      {[0, 1, 2, 3].map(i => (
                        <button key={i} className={`hansoku-dot hansoku-dot--sm ${i < s.aFouls ? "is-on" : ""}`}
                          onClick={() => updateSub(idx, prev => ({ ...prev, aFouls: i < prev.aFouls ? i : i + 1 }))}>{i < s.aFouls ? "✕" : "·"}</button>
                      ))}
                    </div>
                  </div>
                  <div className={`team-sub-match__score ${t.winner === "a" ? "team-sub-match__score--a-win" : t.winner === "b" ? "team-sub-match__score--b-win" : ""}`}>
                    {t.aTotal}–{t.bTotal}
                  </div>
                  <div className="team-sub-match__side team-sub-match__side--right">
                    <div className="team-sub-match__pts">
                      {[0, 1].map(i => (
                        <button key={i} className={`editor-side__pt ${s.bPts[i] ? "editor-side__pt--filled" : ""}`}
                          onClick={() => updateSub(idx, prev => ({ ...prev, bPts: prev.bPts.filter((_, j) => j !== i) }))}>{s.bPts[i] || "·"}</button>
                      ))}
                    </div>
                    <div className="team-sub-match__btns">
                      {["M", "K", "D", "T"].map(c => (
                        <button key={c} className="ipt-btn ipt-btn--sm" onClick={() => updateSub(idx, prev => ({ ...prev, bPts: prev.bPts.length < 2 ? [...prev.bPts, c] : prev.bPts }))}>{c}</button>
                      ))}
                    </div>
                    <div className="team-sub-match__fouls">
                      {[0, 1, 2, 3].map(i => (
                        <button key={i} className={`hansoku-dot hansoku-dot--sm ${i < s.bFouls ? "is-on" : ""}`}
                          onClick={() => updateSub(idx, prev => ({ ...prev, bFouls: i < prev.bFouls ? i : i + 1 }))}>{i < s.bFouls ? "✕" : "·"}</button>
                      ))}
                    </div>
                  </div>
                </div>
              </div>
            );
          })}

          <div className="team-summary">
            <div className={`team-summary__side ${teamWinner === "a" ? "team-summary__side--win" : ""}`}>
              <div className="team-summary__label">IV: {ivA} · PW: {pwA}</div>
            </div>
            <div className="team-summary__result">
              {teamWinner === "a" ? "WIN" : teamWinner === "b" ? "LOSS" : "DRAW"}
              {" – "}
              {teamWinner === "b" ? "WIN" : teamWinner === "a" ? "LOSS" : "DRAW"}
            </div>
            <div className={`team-summary__side ${teamWinner === "b" ? "team-summary__side--win" : ""}`}>
              <div className="team-summary__label">IV: {ivB} · PW: {pwB}</div>
            </div>
          </div>

          <div className="field">
            <label className="field__label">Match status</label>
            <div className="radio-group">
              <button className={`radio-pill ${statusVal === "scheduled" ? "is-active" : ""}`} onClick={() => setStatusVal("scheduled")}>Reset to scheduled</button>
              <button className={`radio-pill ${statusVal === "running" ? "is-active" : ""}`} onClick={() => setStatusVal("running")}>Live (in progress)</button>
              <button className={`radio-pill ${statusVal === "completed" ? "is-active" : ""}`} onClick={() => setStatusVal("completed")}>Final</button>
            </div>
          </div>

          {isComplete && (
            <div className="field">
              <label className="field__label">Correction note (optional)</label>
              <input className="input" placeholder="e.g. Reviewed video" value={note} onChange={(e) => setNote(e.target.value)} />
            </div>
          )}
        </div>
        <div className="editor-modal__foot">
          <button className="btn" onClick={onClose}>Cancel</button>
          <button className="btn btn--primary" onClick={submit}>{isComplete ? "Save correction" : "Save result"}</button>
        </div>
      </div>
    </div>
  );
}

function AdminExport({ c, t }) {
  const url = `${window.location.origin}/viewer.html?id=${t.id}#comp-${c.id}`;
  
  const downloadXlsx = async () => {
    try {
      const resp = await fetch(`/api/competitions/${c.id}/export`);
      if (!resp.ok) throw new Error(await resp.text());
      const blob = await resp.blob();
      const dlUrl = window.URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = dlUrl;
      a.download = `bracket-${c.id}.xlsx`;
      document.body.appendChild(a);
      a.click();
      a.remove();
      window.URL.revokeObjectURL(dlUrl);
    } catch (err) {
      alert("Export failed: " + err.message);
    }
  };

  const copyUrl = () => {
    navigator.clipboard.writeText(url);
    alert("URL copied to clipboard!");
  };

  return (
    <div className="row">
      <div className="card">
        <div className="card__title" style={{ marginBottom: 8 }}>Export {c.name}</div>
        <div className="card__sub" style={{ marginBottom: 14 }}>Generate the official Excel workbook used during the day.</div>
        <button className="btn btn--primary btn--full" onClick={downloadXlsx}>Download .xlsx</button>
        <div className="field__hint" style={{ marginTop: 10 }}>Includes pool draws, pool matches, and elimination brackets with linked formulas.</div>
      </div>
      <div className="card">
        <div className="card__title" style={{ marginBottom: 8 }}>Public viewer link</div>
        <div className="card__sub" style={{ marginBottom: 14 }}>Players & spectators see this competition's bracket, schedule and results live.</div>
        <div style={{ display: "flex", gap: 8 }}>
          <input className="input" value={url} readOnly style={{ flex: 1 }} />
          <button className="btn" onClick={copyUrl}>Copy</button>
        </div>
      </div>
    </div>
  );
}

window.AdminApp = AdminApp;
