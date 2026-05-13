// Admin side — single tournament. Tournament has multiple Competitions.
// Top-level: Tournament dashboard (all competitions), per-competition pages.

const { useState: useStateA, useMemo: useMemoA, useEffect: useEffectA, useRef: useRefA } = React;

// Returns { total, done, live } match counts for a single competition object.
// Accepts either:
//   - flat `poolMatches` array from GET /api/viewer/competitions (list endpoint)
//   - structured `pools[].matches` from GET /api/viewer/competitions/:id (detail endpoint)
// The admin-side GET /api/competitions/:id returns only config; use the viewer
// endpoints when match counts are needed.
function compMatchStats(c) {
  let total = 0, done = 0, live = 0;
  const count = (m) => {
    if (!m || !m.sideA || !m.sideB) return;
    total++;
    if (m.status === "completed") done++;
    if (m.status === "running") live++;
  };
  if (Array.isArray(c.poolMatches)) {
    c.poolMatches.forEach(count);
  } else if (c.pools) {
    c.pools.forEach((p) => (p.matches || []).forEach(count));
  }
  if (c.bracket && c.bracket.rounds) {
    c.bracket.rounds.forEach((r) => (r || []).forEach(count));
  }
  return { total, done, live };
}

// Returns true when line (at index idx in the source array) looks like a CSV
// header that should be skipped.  Checks the first line only.
function looksLikeHeader(line, idx) {
  if (idx !== 0) return false;
  // More robust header detection: contains multiple keywords commonly found in headers
  const keywords = ['name', 'zekken', 'dojo', 'club', 'team', 'grade', 'dan', 'rank'];
  const lower = line.toLowerCase();
  const matched = keywords.filter(k => lower.includes(k));
  // If it contains at least 2 keywords, it's likely a header.
  // Or if it matches a very specific pattern like "Name, Dojo".
  if (matched.length >= 2) return true;
  return false;
}

function parsePastedRows(text, transform) {
  const out = [];
  text.split(/\r?\n/).forEach((line, i) => {
    const trimmed = line.trim();
    if (!trimmed || looksLikeHeader(trimmed, i)) return;
    out.push(transform ? transform(trimmed) : trimmed);
  });
  return out;
}

function normalizeDate(d) {
  if (!d) return d;
  if (/^\d{4}-\d{2}-\d{2}$/.test(d)) return d;
  const match = d.match(/^(\d{1,2})[-/](\d{1,2})[-/](\d{4})$/);
  if (match) {
    return `${match[3]}-${match[2].padStart(2, '0')}-${match[1].padStart(2, '0')}`;
  }
  return d;
}

const pluralize = window.pluralize;
const mergeMatchPatch = window.mergeMatchPatch;

function patchCompetitionData(prev, event) {
  if (!prev || !event.data) return prev;
  const { result, results } = event.data;
  const resultsToApply = results || (result ? [result] : []);
  if (resultsToApply.length === 0) return prev;

  const resultMap = new Map(resultsToApply.map(r => [r.id, r]));
  const next = { ...prev };
  let changed = false;

  if (next.poolMatches) {
    next.poolMatches = next.poolMatches.map(m => {
      const update = resultMap.get(m.id);
      if (update) { changed = true; return mergeMatchPatch(m, update); }
      return m;
    });
  }

  if (next.bracket && next.bracket.rounds) {
    let bChanged = false;
    const rounds = next.bracket.rounds.map(round =>
      round.map(m => {
        const update = resultMap.get(m.id);
        if (update) {
          bChanged = true; changed = true;
          // Map MatchResult to BracketMatch fields if needed
          const patch = { ...update };
          if (patch.ipponsA) patch.scoreA = patch.ipponsA.join("");
          if (patch.ipponsB) patch.scoreB = patch.ipponsB.join("");
          return mergeMatchPatch(m, patch);
        }
        return m;
      })
    );
    if (bChanged) next.bracket = { ...next.bracket, rounds };
  }

  return changed ? next : prev;
}

function Breadcrumbs({ items }) {
  return (
    <div className="crumbs">
      {items.map((item, i) => (
        <React.Fragment key={i}>
          {i > 0 && <span className="sep">/</span>}
          {item.onClick ? (
            <button onClick={() => { document.activeElement?.blur(); item.onClick(); }}>
              {i === 0 && <span style={{ marginRight: 4 }}>←</span>}
              {item.label}
            </button>
          ) : (
            <span>{item.label}</span>
          )}
        </React.Fragment>
      ))}
    </div>
  );
}

function AdminApp({ tournament, onUpdate, onLogout, onViewerMode, onPasswordChange, tweaks, password, view: propView, setView: propSetView, showToast }) {
  const [internalView, setInternalView] = useStateA({ kind: "dashboard" });
  const view = propView || internalView;
  const setView = propSetView || setInternalView;
  // Hooks must be declared unconditionally before any conditional returns.
  const [adminCompData, setAdminCompData] = useStateA(null);
  const [adminLoading, setAdminLoading] = useStateA(false);

  const t = tournament;

  const updateCompetition = async (cid, next) => {
    try {
      const updated = await window.API.updateCompetition(cid, next, password);
      // Merge PUT response locally; SSE will reconcile cross-client.
      const comps = (t.competitions || []).map(c => c.id === cid ? { ...c, ...updated } : c);
      onUpdate({ ...t, competitions: comps });
      if (view.kind === "competition" && view.id === cid) {
        const details = await window.API.fetchCompetitionDetails(cid);
        setAdminCompData(details);
      }
    } catch (e) {
      showToast(e.message, "error");
    }
  };

  const moveMatchCourt = async (compId, matchId, newCourt) => {
    try {
      await window.API.moveMatchCourt(compId, matchId, newCourt, password);
      const comps = await window.API.fetchCompetitions();
      onUpdate({ ...t, competitions: comps });
    } catch (e) {
      showToast(e.message, "error");
    }
  };

  const editMatchScore = async (compId, matchId, result, match) => {
    try {
      await window.API.recordScore(compId, matchId, result, password, match);
      const comps = await window.API.fetchCompetitions();
      onUpdate({ ...t, competitions: comps });
    } catch (e) {
      showToast(e.message, "error");
      throw e;
    }
  };

  const addCompetition = async (c) => {
    try {
      const created = await window.API.createCompetition(c, password);
      const comps = await window.API.fetchCompetitions();
      onUpdate({ ...t, competitions: comps });
      return created;
    } catch (e) {
      showToast(e.message, "error");
      throw e;
    }
  };

  const startAllCompetitions = async () => {
    const setupComps = (t.competitions || []).filter(c => c.status === "setup" && (c.players || []).length >= 2);
    if (setupComps.length === 0) return;

    showToast(`Starting ${setupComps.length} competitions…`);

    setAdminLoading(true);
    const results = await Promise.allSettled(setupComps.map(c => window.API.startCompetition(c.id, password)));

    let success = 0, fail = 0;
    results.forEach((r, i) => {
      if (r.status === "fulfilled") success++;
      else {
        console.error(`Failed to start ${setupComps[i].name}:`, r.reason);
        fail++;
      }
    });

    const comps = await window.API.fetchCompetitions();
    onUpdate({ ...t, competitions: comps });
    setAdminLoading(false);

    if (fail > 0) {
      showToast(`Started ${success} competitions, but ${fail} failed.`, "error");
    } else if (success > 0) {
      showToast(`Successfully started all ${success} competitions.`);
    }
  };

  const createPlayoff = async (sourceId) => {
    try {
      const created = await window.API.createPlayoff(sourceId, password);
      const comps = await window.API.fetchCompetitions();
      onUpdate({ ...t, competitions: comps });
      setView({ kind: "competition", id: created.id, section: "participants" });
      showToast(`Playoff "${created.name}" created`);
    } catch (e) {
      showToast(e.message, "error");
    }
  };

  const startCompetition = async (cid) => {
    const c = t.competitions.find(cc => cc.id === cid);
    if (!c) return;
    showToast(`Starting ${c.name}…`);
    try {
      await window.API.startCompetition(cid, password);
      const comps = await window.API.fetchCompetitions();
      onUpdate({ ...t, competitions: comps });
      showToast(`${c.name} started`);
    } catch (e) {
      showToast(e.message, "error");
    }
  };

  const updateTournament = async (patch) => {
    try {
      const next = { ...t, ...patch };
      // If password was not in the patch, preserve the current session password
      // (since tournament.password is cleared in the viewer API)
      if (!patch.password) {
        next.password = password;
      } else {
        // New password provided in the patch - update parent state and storage
        if (onPasswordChange) onPasswordChange(patch.password);
      }
      await window.API.updateTournament(next, password);
      onUpdate(next);
      showToast("Tournament updated");
    } catch (e) {
      showToast(e.message, "error");
    }
  };

  useEffectA(() => {
    if (view.kind !== "competition") {
      setAdminCompData(null);
      return;
    }
    let cancelled = false;
    // Clear stale data from the previously-viewed competition so the
    // header/modal don't briefly render with another comp's identity.
    setAdminCompData(prev => (prev && prev.config && prev.config.id === view.id) ? prev : null);
    setAdminLoading(true);
    window.API.fetchCompetitionDetails(view.id)
      .then(data => {
        if (!cancelled) { setAdminCompData(data); setAdminLoading(false); }
      })
      .catch(err => {
        if (!cancelled) { console.error(err); setAdminLoading(false); }
      });
    return () => { cancelled = true; };
  }, [view.id, view.kind]);

  useEffectA(() => {
    if (view.kind === "competition") {
      const unsub = window.API.subscribeToEvents((event) => {
        const jitter = Math.random() * 500;
        if (event.type === "competition_started" || event.type === "match_updated" || event.type === "tournament_updated") {
          // tournament_updated carries null data (tournament-wide change) — always
          // relevant.  match_updated / competition_started carry competitionId —
          // skip if they belong to a different competition.
          const relevant = event.type === "tournament_updated"
            || !event.data?.competitionId
            || event.data.competitionId === view.id;
          if (relevant) {
            // Apply partial update immediately for responsive UI
            if (event.type === "match_updated") {
              setAdminCompData(prev => patchCompetitionData(prev, event));
            }
            // Still trigger full refresh (jittered) to reconcile standings/propagation
            setTimeout(() => window.API.fetchCompetitionDetails(view.id).then(setAdminCompData), jitter);
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
      onOpenImport={() => setView({ kind: "import" })}
      onStartAll={startAllCompetitions}
      onStartCompetition={startCompetition}
      onLogout={onLogout}
      onViewerMode={onViewerMode}
      onUpdate={onUpdate}
    />;
  }

  if (view.kind === "createComp") {
    return <AdminCreateCompetition
      tournament={t}
      onCancel={() => setView({ kind: "dashboard" })}
      onCreate={async (c) => {
        try {
          const created = await addCompetition(c);
          setView({ kind: "competition", id: created.id, section: "participants" });
        } catch {
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

  if (view.kind === "import") {
    return <AdminImportPage
      tournament={t}
      onBack={() => setView({ kind: "dashboard" })}
      onImported={async () => {
        const comps = await window.API.fetchCompetitions();
        onUpdate({ ...t, competitions: comps });
        setView({ kind: "dashboard" });
      }}
      onLogout={onLogout}
      onViewerMode={onViewerMode}
      password={password}
    />;
  }

  if (view.kind === "competition") {
    const c = t.competitions.find((cc) => cc.id === view.id);
    if (!c) return <div className="page"><div className="empty"><h3>Competition not found</h3></div></div>;
    // Only use adminCompData when it matches the current view; otherwise it's stale.
    const detail = adminCompData && adminCompData.config && adminCompData.config.id === view.id ? adminCompData : null;
    if (adminLoading && !detail) return <div className="page"><div className="loading">Loading details...</div></div>;

    return <AdminCompetition
      tournament={t}
      competition={detail?.config || c}
      pools={detail?.pools}
      poolMatches={detail?.poolMatches}
      standings={detail?.standings}
      bracket={detail?.bracket}
      reservedSlots={detail?.reservedSlots || []}
      section={view.section}
      onSection={(section) => setView({ ...view, section })}
      onBack={() => setView({ kind: "dashboard" })}
      onOpenCompetition={(id, section) => setView({ kind: "competition", id, section: section || "overview" })}
      onUpdate={(next) => updateCompetition(c.id, next)}
      onCreatePlayoff={createPlayoff}
      onMoveCourt={moveMatchCourt}
      onEditScore={editMatchScore}
      onLogout={onLogout}
      onViewerMode={onViewerMode}
      tweaks={tweaks}
      password={password}
      showToast={showToast}
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

const StatusBadge = window.StatusBadge;
const formatDate = window.formatDate;

function initialSectionFor(status) {
  if (status === "setup") return "participants";
  if (status === "pools" || status === "playoffs") return "scores";
  return "overview";
}

function AdminDashboard({ tournament, onOpenCompetition, onCreateCompetition, onEditTournament, onOpenSchedule, onOpenScoreEditor, onOpenImport, onStartAll, onStartCompetition, onLogout, onViewerMode, onUpdate }) {
  const t = tournament;
  const comps = t.competitions || [];

  useEffectA(() => {
    const unsub = window.API.subscribeToEvents((event) => {
      const jitter = Math.random() * 500;
      if (event.type === "tournament_updated" || event.type === "competition_started" || event.type === "competition_deleted") {
        // Refresh everything on the dashboard
        setTimeout(() => {
          Promise.all([
            window.API.fetchTournament(),
            window.API.fetchCompetitions()
          ]).then(([tourney, competitions]) => {
            onUpdate({ ...tourney, competitions });
          }).catch(err => console.error("Dashboard refresh failed", err));
        }, jitter);
      }
    });
    return unsub;
  }, []);

  const { totalMatches, doneMatches, liveMatches, totalParticipants } = useMemoA(() => {
    let totalMatches = 0, doneMatches = 0, liveMatches = 0, totalParticipants = 0;
    comps.forEach((c) => {
      totalParticipants += (c.players || []).length;
      const s = compMatchStats(c);
      totalMatches += s.total; doneMatches += s.done; liveMatches += s.live;
    });
    return { totalMatches, doneMatches, liveMatches, totalParticipants };
  }, [comps]);

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
              {formatDate(t.date)} · {t.venue} · {pluralize(t.courts.length, "shiaijo (court)", "shiaijo (courts)")} · {pluralize(comps.length, "competition")} · {pluralize(totalParticipants, "participant")}
            </div>
          </div>
          <div className="page-head__actions">
            <button className="btn" onClick={onEditTournament}>Edit details</button>
            {comps.some(c => c.status === "setup" && (c.players || []).length >= 2) && (
              <button className="btn btn--danger" onClick={onStartAll}>Start all</button>
            )}
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
            {running.map((c) => <CompCard key={c.id} c={c} onOpen={() => onOpenCompetition(c.id, "scores")} onStart={() => onStartCompetition(c.id)} />)}
          </div>
        </>)}

        <div className="section-title">All competitions</div>
        <div className="tlist">
          {comps.map((c) => <CompCard key={c.id} c={c} onOpen={() => onOpenCompetition(c.id, initialSectionFor(c.status))} onStart={() => onStartCompetition(c.id)} />)}
          <button className="tcard tcard--add" onClick={onCreateCompetition}>
            <div style={{ fontSize: 28, color: "var(--ink-3)" }}>+</div>
            <div style={{ fontWeight: 600, marginTop: 4 }}>Add competition</div>
            <div style={{ fontSize: 12, color: "var(--ink-3)", marginTop: 2 }}>Individual or Team</div>
          </button>
          <button className="tcard tcard--add" onClick={onOpenImport}>
            <div style={{ fontSize: 28, color: "var(--ink-3)" }}>📂</div>
            <div style={{ fontWeight: 600, marginTop: 4 }}>Import competitions</div>
            <div style={{ fontSize: 12, color: "var(--ink-3)", marginTop: 2 }}>From folder with manifest.yaml</div>
          </button>
        </div>
      </div>
    </div>
  );
}

function CompCard({ c, onOpen, onStart }) {
  const { live: liveCount } = compMatchStats(c);
  const playerCount = (c.players || []).length;

  return (
    <div className="tcard" onClick={onOpen}>
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
        <div className="tcard__stat"><div className="v">{playerCount}</div><div className="l">{pluralize(playerCount, c.kind === "team" ? "Team" : "Player")}</div></div>
        <div className="tcard__stat"><div className="v">{c.courts.length}</div><div className="l">{pluralize(c.courts.length, "Shiaijo", "Shiaijo")}</div></div>
        <div className="tcard__stat"><div className="v">{c.format === "pools" ? "Pools" : "KO"}</div><div className="l">Format</div></div>
        {liveCount > 0 && <div className="tcard__stat"><div className="v" style={{ color: "var(--red)" }}>{liveCount}</div><div className="l">Live</div></div>}
      </div>
      <div className="tcard__actions">
        {c.status === "setup" && playerCount >= 2 && (
          <button className="btn btn--primary btn--sm btn--full" onClick={(e) => { e.stopPropagation(); onStart(); }}>Start Competition →</button>
        )}
        {(c.status === "pools" || c.status === "playoffs") && (
          <button className="btn btn--primary btn--sm btn--full" onClick={(e) => { e.stopPropagation(); onOpen(); }}>Go to Live Scoring →</button>
        )}
        {c.status === "completed" && (
          <button className="btn btn--sm btn--full" onClick={(e) => { e.stopPropagation(); onOpen(); }}>View Results</button>
        )}
      </div>
    </div>
  );
}

function AdminEditTournament({ tournament, onCancel, onSave, onLogout, onViewerMode }) {
  const [name, setName] = useStateA(tournament.name);
  const [venue, setVenue] = useStateA(tournament.venue);
  const [date, setDate] = useStateA(tournament.date);
  const [courts, setCourts] = useStateA(tournament.courts.length);
  const [pass, setPass] = useStateA(""); // Leave empty to keep existing, unless changed
  const [error, setError] = useStateA("");

  const handleSave = () => {
    if (!name.trim()) { setError("Tournament name is required."); return; }
    const norm = normalizeDate(date);
    if (!/^\d{4}-\d{2}-\d{2}$/.test(norm)) { setError("Invalid date format. Use DD-MM-YYYY."); return; }
    const year = parseInt(norm.substring(0, 4));
    if (year < 1900 || year > 2100) { setError("Year must be between 1900 and 2100."); return; }
    if (courts < 1 || courts > 26) { setError("Number of courts must be between 1 and 26."); return; }

    onSave({
      name,
      venue,
      date: norm,
      password: pass || undefined,
      courts: Array.from({ length: courts }, (_, i) => String.fromCharCode(65 + i))
    });
  };

  return (
    <div className="app">
      <AdminTopbar onLogout={onLogout} onViewerMode={onViewerMode} tournament={tournament} />
      <div className="page" style={{ maxWidth: 720 }}>
        <Breadcrumbs items={[
          { label: tournament.name, onClick: onCancel },
          { label: "Edit details" }
        ]} />
        <div className="page-head"><h1 className="page-head__title">Edit tournament details</h1></div>
        {error && <div className="auth__error" style={{ marginBottom: 16 }}>{error}</div>}
        <div className="card card--pad-lg">
          <div className="row">
            <div className="field"><label className="field__label">Name</label><input className="input" value={name} onChange={(e) => { setName(e.target.value); setError(""); }} /></div>
            <div className="field">
              <label className="field__label">Date</label>
              <input className="input" type="date" value={date} onChange={(e) => { setDate(e.target.value); setError(""); }} />
              <div className="field__hint">Format: DD-MM-YYYY</div>
            </div>
          </div>
          <div className="field"><label className="field__label">Venue</label><input className="input" value={venue} onChange={(e) => { setVenue(e.target.value); setError(""); }} /></div>
          <div className="field">
            <label className="field__label">Number of Shiaijo (courts)</label>
            <input className="input" type="number" min="1" max="26" value={courts} onChange={(e) => { setCourts(+e.target.value); setError(""); }} />
            <div className="field__hint">Enter a number (1-26). Courts will be automatically labeled A, B, C, etc.</div>
          </div>
          <div className="field">
            <label className="field__label">Admin Password</label>
            <input className="input" type="password" value={pass} onChange={(e) => { setPass(e.target.value); setError(""); }} placeholder="••••••••" autoComplete="new-password" />
            <div className="field__hint">Enter a new password to change it. Leave blank to keep the current one.</div>
          </div>
          <div style={{ display: "flex", justifyContent: "flex-end", gap: 8, marginTop: 16 }}>
            <button className="btn" onClick={onCancel}>Cancel</button>
            <button className="btn btn--primary" onClick={handleSave}>Save changes</button>
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
  const [error, setError] = useStateA("");

  const toggleCourt = (cc) => setSelectedCourts((sc) => sc.includes(cc) ? sc.filter((c) => c !== cc) : [...sc, cc].sort());

  const create = () => {
    const finalName = name || (kind === "team"
      ? (gender === "F" ? "Women's Teams" : "Men's Teams")
      : (gender === "F" ? "Women's Individual" : gender === "M" ? "Men's Individual" : "Individual"));

    const exists = (tournament.competitions || []).some(cc => cc.name.toLowerCase() === finalName.toLowerCase());
    if (exists) {
      setError(`A competition named "${finalName}" already exists. Please use a unique name.`);
      return;
    }

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

        {error && <div className="alert alert--error" style={{ marginBottom: 16 }}>{error}</div>}

        <div className="card card--pad-lg">
          <div className="field">
            <label className="field__label">Competition type</label>
            <div className="radio-group">
              <button className={`radio-pill ${kind === "individual" ? "is-active" : ""}`} type="button" onClick={() => setKind("individual")}>Individual</button>
              <button className={`radio-pill ${kind === "team" ? "is-active" : ""}`} type="button" onClick={() => setKind("team")}>Team</button>
            </div>
          </div>

          <div className="field">
            <label className="field__label">Category (optional)</label>
            <div className="radio-group">
              <button className={`radio-pill ${gender === "M" ? "is-active" : ""}`} type="button" onClick={() => setGender("M")}>Men</button>
              <button className={`radio-pill ${gender === "F" ? "is-active" : ""}`} type="button" onClick={() => setGender("F")}>Women</button>
              <button className={`radio-pill ${gender === "X" ? "is-active" : ""}`} type="button" onClick={() => setGender("X")}>Mixed / Other</button>
            </div>
            <div className="field__hint">Used for the display label and in name suggestions. You can change later.</div>
          </div>

          <div className="row">
            <div className="field">
              <label className="field__label">Display name</label>
              <input className="input" placeholder="e.g. Men's Individual" value={name} onChange={(e) => { setName(e.target.value); setError(""); }} />
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
              <div className="field__hint">Format: DD-MM-YYYY. For multi-day tournaments, specify which day this competition takes place.</div>
            </div>
            <div className="field">
              <label className="field__label">Player number prefix <span style={{ fontWeight: 400, color: "var(--ink-3)" }}>(optional)</span></label>
              <input className="input" placeholder="e.g. A" maxLength="3" value={numberPrefix} onChange={(e) => setNumberPrefix(e.target.value)} style={{ maxWidth: 80 }} />
              <div className="field__hint">Single letter prefix for participant numbers (A1, B1…). Keeps numbers unique across competitions.</div>
            </div>
          </div>

          <div className="field">
            <label className="field__label">Format</label>
            <div className="radio-group">
              <button className={`radio-pill ${format === "playoffs" ? "is-active" : ""}`} type="button" onClick={() => setFormat("playoffs")}>Knockout only</button>
              <button className={`radio-pill ${format === "pools" ? "is-active" : ""}`} type="button" onClick={() => setFormat("pools")}>Pools + Knockout</button>
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
                <button className={`radio-pill ${sampleSize === "small" ? "is-active" : ""}`} type="button" onClick={() => setSampleSize("small")}>Small (8)</button>
                <button className={`radio-pill ${sampleSize === "medium" ? "is-active" : ""}`} type="button" onClick={() => setSampleSize("medium")}>Medium (16)</button>
                <button className={`radio-pill ${sampleSize === "large" ? "is-active" : ""}`} type="button" onClick={() => setSampleSize("large")}>Large (32)</button>
              </div>
            )}
            <div className="field__hint">Leave off to add real participants in the next step.</div>
          </div>

          <div className="field">
            <label className="field__label">Assigned shiaijo (courts)</label>
            <div className="radio-group">
              {tournament.courts.map((cc) => (
                <button key={cc} className={`radio-pill ${selectedCourts.includes(cc) ? "is-active" : ""}`} type="button" onClick={() => toggleCourt(cc)}>Shiaijo (court) {cc}</button>
              ))}
            </div>
            <div className="field__hint">Concurrency for this competition equals the number of shiaijo (courts) assigned. Different competitions can share shiaijo (courts); the schedule prevents conflicts.</div>
          </div>

          {format === "pools" && (
            <>
              <div className="field">
                <label className="field__label">Pool size is a</label>
                <div className="radio-group">
                  <button className={`radio-pill ${poolMode === "max" ? "is-active" : ""}`} type="button" onClick={() => setPoolMode("max")}>maximum</button>
                  <button className={`radio-pill ${poolMode === "min" ? "is-active" : ""}`} type="button" onClick={() => setPoolMode("min")}>minimum</button>
                </div>
                <div className="field__hint">
                  {poolMode === "max"
                    ? "No pool will have more than the size below (more pools, smaller pools)."
                    : "Each pool will have at least the size below (fewer pools, larger pools)."}
                </div>
              </div>
              <div className="row">
                <div className="field"><label className="field__label">Players per pool</label><input className="input" type="number" min="3" value={poolSize} onChange={(e) => setPoolSize(+e.target.value)} /></div>
                <div className="field"><label className="field__label">Winners per pool</label><input className="input" type="number" min="1" value={winners} onChange={(e) => setWinners(+e.target.value)} /></div>
              </div>
            </>
          )}

          {kind === "team" && (
            <div className="field">
              <label className="field__label">Team size</label>
              <window.StableInput className="input" type="number" min="1" max="9" value={teamSize} onChange={(val) => setTeamSize(val)} />
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

function AdminCompetition({ tournament, competition, pools, poolMatches, standings, bracket, reservedSlots, section, onSection, onBack, onOpenCompetition, onUpdate, onCreatePlayoff, onMoveCourt, onEditScore, onLogout, onViewerMode, tweaks, password, showToast }) {
  const c = competition;
  const t = tournament;
  const [starting, setStarting] = useStateA(false);

  const isDateValid = (date) => {
    if (!date) return false;
    if (!/^\d{4}-\d{2}-\d{2}$/.test(date)) return false;
    const year = parseInt(date.substring(0, 4));
    return year >= 1900 && year <= 2100;
  };

  const start = async () => {
    showToast(`Starting ${c.name}…`);

    setStarting(true);
    try {
      const updated = await window.API.startCompetition(c.id, password);
      // Directly update the local state without calling updateCompetition (PUT)
      // updated is a CompetitionDetail (config, pools, bracket…); extract the flat config for the list
      const flatComp = updated.config || updated;
      const comps = (t.competitions || []).map(cc => cc.id === c.id ? { ...cc, ...flatComp } : cc);
      onUpdate({ ...t, competitions: comps });
      showToast(`${c.name} started`);
      onSection("scores");
    } catch (e) {
      console.error("Start competition failed:", e);
      showToast(e.message, "error");
    } finally {
      setStarting(false);
    }
  };

  const sections = [
    {
      sec: "Preparation", items: [
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
          { label: "Dashboard", onClick: onBack },
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
          <div className="page-head__actions" style={{ display: "flex", flexDirection: "column", alignItems: "flex-end", gap: 8 }}>
            {c.status === "setup" && c.players.length >= 2 && (
              <>
                <button className="btn btn--primary" onClick={start} disabled={!isDateValid(c.date) || starting}>
                  {starting && <span className="spinner" />}
                  {starting ? "Starting…" : "Start competition →"}
                </button>
                {!isDateValid(c.date) && (
                  <div style={{ color: "var(--red)", fontSize: 11, fontWeight: 600 }}>
                    ⚠ Cannot start: invalid date in Settings tab (e.g. "{c.date}")
                  </div>
                )}
              </>
            )}
            {c.format === "pools" && c.status !== "setup" && onCreatePlayoff && (() => {
              const playoffName = c.name + " - Playoffs";
              const hasPlayoff = (t.competitions || []).some(cc => cc.name === playoffName);
              return hasPlayoff
                ? <div style={{ fontSize: 11, color: "var(--ink-3)", fontWeight: 600 }}>Playoff bracket already created</div>
                : <button className="btn btn--primary" onClick={() => onCreatePlayoff(c.id)}>Create playoff bracket →</button>;
            })()}
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
                <button key={cc.id} onClick={() => onOpenCompetition(cc.id)}>{cc.name}</button>
              ))}
            </div>
          </div>
          <div>
            {section === "overview" && <AdminCompOverview c={c} pools={pools} poolMatches={poolMatches} bracket={bracket} onSection={onSection} />}
            {section === "participants" && <AdminParticipants c={c} tournament={t} reservedSlots={reservedSlots || []} onUpdate={onUpdate} password={password} showToast={showToast} onSection={onSection} />}
            {section === "settings" && <AdminSettings c={c} tournament={t} onUpdate={onUpdate} onBack={onBack} password={password} showToast={showToast} />}
            {section === "pools" && <AdminPools c={c} pools={pools} standings={standings} tweaks={tweaks} onEditScore={onEditScore} password={password} />}
            {section === "bracket" && <AdminBracket c={c} t={t} bracket={bracket} onUpdate={onUpdate} onMoveCourt={onMoveCourt} tweaks={tweaks} password={password} showToast={showToast} />}
            {section === "scores" && <AdminScoreEditor c={c} t={t} onEditScore={onEditScore} onMoveCourt={onMoveCourt} restrictToCompId={c.id} embedded />}
            {section === "export" && <AdminExport c={c} t={t} />}
          </div>
        </div>
      </div>
    </div>
  );
}

function AdminCompOverview({ c, pools, poolMatches, bracket, onSection }) {
  const { total, done, live } = compMatchStats({ ...c, pools, poolMatches, bracket });
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
        <button className="card" style={{ textAlign: "left", cursor: "pointer", border: "1px solid var(--line)" }} onClick={() => onSection(bracket ? "bracket" : "pools")}>
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

function levenshtein(a, b) {
  const m = a.length, n = b.length;
  if (m === 0) return n;
  if (n === 0) return m;
  let prev = Array.from({ length: n + 1 }, (_, j) => j);
  let curr = new Array(n + 1);
  for (let i = 1; i <= m; i++) {
    curr[0] = i;
    for (let j = 1; j <= n; j++)
      curr[j] = a[i - 1] === b[j - 1] ? prev[j - 1] : 1 + Math.min(prev[j], curr[j - 1], prev[j - 1]);
    [prev, curr] = [curr, prev];
  }
  return prev[n];
}

function AdminParticipants({ c, tournament, reservedSlots, onUpdate, password, showToast, onSection }) {
  const [showAllPreview, setShowAllPreview] = useStateA(false);
  const [seedImportResult, setSeedImportResult] = useStateA(null);
  const [importSummary, setImportSummary] = useStateA(null);
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
  const textFocusRef = useRefA(false);

  const generateText = (playersList) => (playersList || []).map((p) => {
    if (c.withZekkenName && p.displayName) {
      const base = `${p.name ?? ""}, ${p.displayName ?? ""}, ${p.dojo ?? ""}`;
      return p.danGrade ? `${base}, ${p.danGrade}` : base;
    }
    const base = `${p.name ?? ""}, ${p.dojo ?? ""}`;
    return p.danGrade ? `${base}, ${p.danGrade}` : base;
  }).join("\n");

  useEffectA(() => {
    if (!textFocusRef.current) {
      setText(generateText(c.players || []));
    }
  }, [c.players, c.withZekkenName]);

  const handleSeedFile = (file) => {
    if (!file) return;
    const reader = new FileReader();
    reader.onload = (e) => {
      const raw = e.target.result;
      const np = [...(c.players || [])];
      let updatedCount = 0;
      const unmatched = [];
      const allNames = np.map(p => ({ name: p.name, display: p.displayName }));

      raw.split(/\r?\n/).forEach((line, i) => {
        const trimmed = line.trim();
        if (!trimmed) return;
        // Skip header if it looks like one
        if (i === 0 && (/name/i.test(trimmed) || /zekken/i.test(trimmed)) && /seed|rank/i.test(trimmed)) return;

        const parts = trimmed.split(",").map(s => s.trim());
        if (parts.length >= 2) {
          const name = parts[0];
          const seedStr = parts[1];
          const seed = parseInt(seedStr);

          if (name && !isNaN(seed) && seed > 0) {
            const nameLower = name.toLowerCase();
            const pIdx = np.findIndex(p =>
              p.name.toLowerCase() === nameLower ||
              (p.displayName && p.displayName.toLowerCase() === nameLower) ||
              p.name.toLowerCase().includes(nameLower) ||
              nameLower.includes(p.name.toLowerCase())
            );

            if (pIdx >= 0) {
              np[pIdx] = { ...np[pIdx], seed };
              updatedCount++;
            } else {
              // Find closest name suggestion
              let best = null, bestDist = Infinity;
              allNames.forEach(({ name: n, display: d }) => {
                const d1 = levenshtein(nameLower, n.toLowerCase());
                const d2 = d ? levenshtein(nameLower, d.toLowerCase()) : Infinity;
                const dist = Math.min(d1, d2);
                if (dist < bestDist) { bestDist = dist; best = n; }
              });
              unmatched.push({ name, seed, suggestion: bestDist <= 5 ? best : null });
            }
          }
        }
      });

      if (updatedCount > 0) {
        onUpdate({ ...c, players: np });
        showToast(`Matched ${updatedCount} seeds`);
      }
      setSeedImportResult({ updatedCount, unmatched, totalRows: updatedCount + unmatched.length });
    };
    reader.readAsText(file);
  };

  const handleFile = (file) => {
    if (!file) return;
    const reader = new FileReader();
    reader.onload = (e) => {
      const parsedLines = parsePastedRows(e.target.result);
      const newText = parsedLines.join("\n");
      setText(newText);
      
      const newCount = parsedLines.length;
      const existingCount = (c.players || []).length;
      setImportSummary({ newCount, existingCount });
      
      showToast(`CSV loaded: ${newCount} entries.`);
    };
    reader.readAsText(file);
  };

  const [tagFilter, setTagFilter] = useStateA(null);
  const lines = useMemoA(() => text.split("\n").filter((l) => l.trim()), [text]);
  const players = useMemoA(() => c.players || [], [c.players]);
  const allTags = useMemoA(() => [...new Set(players.map(p => p.tag).filter(Boolean))], [players]);
  const visiblePlayers = useMemoA(() => tagFilter ? players.filter(p => p.tag === tagFilter) : players, [players, tagFilter]);
  const { gaps, hasGaps } = useMemoA(() => {
    const sortedSeeds = players.filter(p => p.seed).map(p => p.seed).sort((a, b) => a - b);
    const gaps = [];
    if (sortedSeeds.length > 0) {
      const maxSeed = sortedSeeds[sortedSeeds.length - 1];
      for (let s = 1; s <= maxSeed; s++) {
        if (!sortedSeeds.includes(s)) gaps.push(s);
      }
    }
    return { gaps, hasGaps: gaps.length > 0 };
  }, [players]);

  const updateSeed = (idx, val) => {
    const np = [...(c.players || [])];
    const seed = parseInt(val);
    np[idx] = { ...np[idx], seed: isNaN(seed) || seed <= 0 ? null : seed };
    onUpdate({ ...c, players: np });
  };

  const dragIdxRef = useRefA(null);
  const [dragOverIdx, setDragOverIdx] = useStateA(null);
  const moveSeedRow = (fromIdx, toIdx) => {
    if (fromIdx === toIdx) return;
    const np = [...(c.players || [])];
    const [moved] = np.splice(fromIdx, 1);
    np.splice(toIdx, 0, moved);
    // Re-assign seeds by new position order (only for currently-seeded players)
    const seededCount = np.filter(p => p.seed).length;
    if (seededCount > 0) {
      let rank = 1;
      const renumbered = np.map(p => p.seed ? { ...p, seed: rank++ } : p);
      onUpdate({ ...c, players: renumbered });
    } else {
      onUpdate({ ...c, players: np });
    }
  };

  const shuffleUnseeded = () => {
    const np = [...(c.players || [])];
    const unseeded = np.filter(p => !p.seed);
    if (unseeded.length < 2) return;
    for (let i = unseeded.length - 1; i > 0; i--) {
      const j = Math.floor(Math.random() * (i + 1));
      [unseeded[i], unseeded[j]] = [unseeded[j], unseeded[i]];
    }
    let uIdx = 0;
    const shuffled = np.map(p => p.seed ? p : unseeded[uIdx++]);
    onUpdate({ ...c, players: shuffled });
    showToast("Unseeded list shuffled");
  };

  const [showSlotForm, setShowSlotForm] = useStateA(false);
  const [slotSrcComp, setSlotSrcComp] = useStateA("");
  const [slotRank, setSlotRank] = useStateA(1);
  const [slotLoading, setSlotLoading] = useStateA(false);

  const otherComps = (tournament?.competitions || []).filter(cc => cc.id !== c.id);

  const addSlot = async () => {
    if (!slotSrcComp || slotRank < 1) return;
    setSlotLoading(true);
    try {
      await window.API.addReservedSlot(c.id, slotSrcComp, slotRank, password);
      setShowSlotForm(false);
      setSlotSrcComp("");
      setSlotRank(1);
      showToast("Reserved slot added");
    } catch (e) {
      alert("Failed to add reserved slot: " + e.message);
    } finally {
      setSlotLoading(false);
    }
  };

  const removeSlot = async (slotID) => {
    try {
      await window.API.deleteReservedSlot(c.id, slotID, password);
      showToast("Reserved slot removed");
    } catch (e) {
      alert("Failed to remove reserved slot: " + e.message);
    }
  };

  const apply = () => {
    try {
      console.log("AdminParticipants: Applying", lines.length, "lines");
      const withZekken = c.withZekkenName;
      const existingMap = new Map((c.players || []).map(p => [p.name, p]));
      const parsed = window.parseParticipantLines(lines, withZekken);

      // Duplicate detection (case-insensitive)
      const nameSeen = new Map();
      const dupes = [];
      parsed.forEach(({ name }) => {
        const key = name.toLowerCase();
        if (nameSeen.has(key)) { if (!dupes.includes(name)) dupes.push(name); }
        else nameSeen.set(key, true);
      });
      if (dupes.length > 0) {
        showToast(`Duplicate names detected: ${dupes.join(", ")}`, "error");
        return;
      }

      let added = 0, updatedCount = 0;
      const np = parsed.map(({ name, displayName, dojo, danGrade, tag }, i) => {
        const existing = existingMap.get(name);
        if (existing) updatedCount++; else added++;
        return { id: existing?.id || `${c.id}-p${i + 1}`, name, displayName, dojo, danGrade, tag, seed: existing?.seed || null };
      });
      console.log("AdminParticipants: Final list", np);
      onUpdate({ ...c, players: np });
      
      const label = c.kind === "team" ? "team" : "participant";
      let msg = `Saved ${pluralize(np.length, label)}`;
      if (added > 0 || updatedCount > 0) {
        msg += ` (${added} new, ${updatedCount} updated)`;
      }
      showToast(msg);
      setImportSummary(null);
    } catch (err) {
      console.error("AdminParticipants: Apply failed", err);
      showToast("Failed to apply participants: " + err.message, "error");
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
      // Normalise tabs to commas, strip leading row numbers (e.g. "1, Name, Dojo")
      setText(parsePastedRows(clipboardText, (s) => s.replace(/\t/g, ", ").replace(/^\d+,\s*/, "")).join("\n"));
    } catch (err) {
      console.error("Paste failed", err);
      alert("Failed to read clipboard: " + err.message + "\n\nMake sure you have granted clipboard permissions.");
    }
  };

  const downloadTemplate = () => {
    const content = c.kind === "team"
      ? "Team Name, Dojo\nTora A, Tora Dojo London\n"
      : c.withZekkenName
        ? "Name, Zekken, Dojo, Dan\nAkira Tanaka, TANAKA, Mumeishi, 3\n"
        : "Name, Dojo, Dan\nAkira Tanaka, Mumeishi, 3\n";
    const blob = new Blob([content], { type: "text/csv" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = "participants_template.csv";
    a.click();
  };

  const isStarted = c.status !== "setup";
  return (
    <>
      {isStarted && (
        <div style={{ marginBottom: 16, display: "flex", justifyContent: "flex-end" }}>
          <button className="btn btn--primary" onClick={() => onSection("scores")}>Go to Scoring →</button>
        </div>
      )}
      <div className="row row--equal" style={{ alignItems: "start" }}>
        <div className="card">
          <div className="card__head">
            <div>
              <div className="card__title">{c.kind === "team" ? "Team list" : "Participant list"}</div>
              <div className="card__sub">
                {lines.length} entries · One per line · <span style={{ color: "var(--ink-2)", fontWeight: 600 }}>Example: Alice Smith, Mumeishi, 3</span>
              </div>
              <div className="field__hint" style={{ marginTop: 2, fontSize: 11 }}>
                Format: "{c.kind === "team" ? "Team name, Dojo" : c.withZekkenName ? "Name, Zekken, Dojo[, Dan]" : "Name, Dojo[, Dan grade]"}"
                <br />* Dan = kendo grade (optional)
                <br /><button className="btn--link" style={{ padding: 0, fontSize: 11, fontWeight: 600 }} onClick={downloadTemplate}>Download CSV template</button>
              </div>
            </div>
            <div style={{ display: "flex", gap: 6 }}>
              <button className="btn btn--sm" type="button" onClick={pasteFromExcel} title="Reads clipboard and converts tab-separated values (e.g. from Excel) to CSV">Paste clipboard</button>
              <button className="btn btn--sm btn--primary" type="button" onClick={apply} disabled={hasGaps}>Apply changes</button>
            </div>
          </div>

          <div style={{ display: "flex", gap: 10, marginBottom: 12 }}>
            <div
              className={`dropzone ${dragOver ? "dropzone--active" : ""}`}
              onClick={() => fileRef.current?.click()}
              onDragOver={(e) => { e.preventDefault(); setDragOver(true); }}
              onDragLeave={() => setDragOver(false)}
              onDrop={onDrop}
              style={{ flex: 1, height: 80, minHeight: 80 }}
            >
              <div className="dropzone__icon">📥</div>
              <div>
                <div className="dropzone__title">{dragOver ? "Drop CSV to import" : "Click or drop CSV to import participants"}</div>
                <div className="dropzone__sub">
                  {c.withZekkenName ? "Name, Zekken, Dojo[, Dan]" : "Name, Dojo[, Dan grade] (e.g. Alice Smith, Mumeishi, 3)"}
                </div>
              </div>
              <input ref={fileRef} type="file" accept=".csv,.txt,text/csv,text/plain" style={{ display: "none" }} onChange={(e) => handleFile(e.target.files[0])} />
            </div>
          </div>

          {importSummary && (
            <div className="alert alert--success" style={{ marginBottom: 12, display: "flex", justifyContent: "space-between", alignItems: "center" }}>
              <span>✔ Loaded <strong>{importSummary.newCount}</strong> entries. {importSummary.existingCount > 0 ? `This will replace ${importSummary.existingCount} existing ${c.kind === "team" ? "teams" : "players"} on Apply.` : ""}</span>
              <button className="btn btn--sm btn--ghost" onClick={() => setImportSummary(null)}>Dismiss</button>
            </div>
          )}

          <LinedTextarea 
            value={text} 
            onChange={(e) => setText(e.target.value)} 
            onFocus={() => { textFocusRef.current = true; }}
            onBlur={() => { textFocusRef.current = false; }}
            rows={14} 
            placeholder={c.kind === "team" ? "Tora A, Tora Dojo London" : c.withZekkenName ? "Akira Tanaka, TANAKA, Mumeishi" : "Akira Tanaka, Mumeishi"} 
          />
          <div className="field__hint" style={{ marginTop: 6 }}>Click "Apply" to save the participant list. Existing seeds are preserved by row order.</div>
          {otherComps.length > 0 && (
            <div style={{ marginTop: 10 }}>
              {!showSlotForm ? (
                <button className="btn btn--sm" onClick={() => setShowSlotForm(true)}>+ Reserved slot</button>
              ) : (
                <div style={{ padding: "10px 0", display: "flex", flexDirection: "column", gap: 8 }}>
                  <div style={{ fontWeight: 600, fontSize: 13 }}>Add reserved slot</div>
                  <div style={{ display: "flex", gap: 8, flexWrap: "wrap", alignItems: "flex-end" }}>
                    <div style={{ flex: 2 }}>
                      <div className="field__label">Source competition</div>
                      <select className="field__select" value={slotSrcComp} onChange={e => setSlotSrcComp(e.target.value)}>
                        <option value="">Select…</option>
                        {otherComps.map(cc => <option key={cc.id} value={cc.id}>{cc.name}</option>)}
                      </select>
                    </div>
                    <div style={{ flex: 1 }}>
                      <div className="field__label">Rank</div>
                      <input className="field__input" type="number" min={1} value={slotRank} onChange={e => setSlotRank(+e.target.value)} />
                    </div>
                    <div style={{ display: "flex", gap: 6 }}>
                      <button className="btn btn--sm btn--primary" onClick={addSlot} disabled={!slotSrcComp || slotRank < 1 || slotLoading}>Add</button>
                      <button className="btn btn--sm" onClick={() => setShowSlotForm(false)}>Cancel</button>
                    </div>
                  </div>
                  <div className="field__hint">The placeholder participant will be replaced with the real player when the source competition reaches playoffs.</div>
                </div>
              )}
            </div>
          )}
          {lines.length > 0 && (() => {
            const previewLimit = showAllPreview ? lines.length : 10;
            const preview = window.parseParticipantLines(lines.slice(0, previewLimit), c.withZekkenName);
            const cols = c.withZekkenName ? ["Name", "Zekken", "Dojo", "Dan"] : ["Name", "Dojo", "Dan"];
            return (
              <div style={{ marginTop: 8, overflowX: "auto" }}>
                <table className="parse-preview">
                  <thead><tr>{cols.map(h => <th key={h}>{h}</th>)}</tr></thead>
                  <tbody>{preview.map((p, i) => (
                    <tr key={i}>
                      <td className={!p.name ? "cell--missing" : ""}>{p.name || "—"}</td>
                      {c.withZekkenName && <td className={!p.displayName ? "cell--missing" : ""}>{p.displayName || "—"}</td>}
                      <td className={!p.dojo ? "cell--missing" : ""}>{p.dojo || "—"}</td>
                      <td>{p.danGrade || "—"}</td>
                    </tr>
                  ))}</tbody>
                </table>
                <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginTop: 4 }}>
                  <div className="field__hint">Preview of {Math.min(lines.length, previewLimit)} of {lines.length} rows</div>
                  {lines.length > 10 && (
                    <button className="btn btn--ghost btn--sm" style={{ color: "var(--accent)", padding: "2px 6px" }} onClick={() => setShowAllPreview(!showAllPreview)}>
                      {showAllPreview ? "Show less" : "Show all"}
                    </button>
                  )}
                </div>
              </div>
            );
          })()}
        </div>
        <div className="card">
          <div className="card__head">
            <div>
              <div className="card__title">Seeding</div>
              <div className="card__sub">{players.filter((p) => p.seed).length} of {players.length} seeded</div>
            </div>
            <div style={{ display: "flex", gap: 6 }}>
              <button className="btn btn--sm" type="button" onClick={shuffleUnseeded} disabled={players.length === 0} title="Shuffle unseeded players">Shuffle unseeded</button>
              <button className="btn btn--sm" type="button" onClick={() => seedFileRef.current?.click()} disabled={players.length === 0} title={players.length === 0 ? "Add participants first" : undefined}>Import Seeds CSV</button>
              <input ref={seedFileRef} type="file" accept=".csv,.txt,text/csv,text/plain" style={{ display: "none" }} onChange={(e) => handleSeedFile(e.target.files[0])} />
              <button className="btn btn--sm" type="button" onClick={() => onUpdate({ ...c, players: c.players.map((p) => ({ ...p, seed: null })) })}>Clear seeds</button>
            </div>
          </div>
          <div className="card__body" style={{ paddingTop: 0, paddingBottom: 8 }}>
            <div className="field__hint" style={{ marginBottom: 12 }}>
              Assign seed ranks (1, 2, 3…) to separate top players. Seeds 1 and 2 will be placed on opposite sides of the bracket.
              Drag rows to change order.
            </div>
          </div>
          {hasGaps && (
            <div className="alert alert--error" style={{ margin: "0 16px 16px" }}>
              ❌ Seed gap detected: rank {gaps.join(", ")} {gaps.length > 1 ? "are" : "is"} missing. Seeds must be sequential (1, 2, 3…).
            </div>
          )}
          {seedImportResult && (
            <div style={{ margin: "0 16px 12px" }}>
              <div className="alert alert--success" style={{ marginBottom: 6 }}>
                ✔ Matched {seedImportResult.updatedCount} of {seedImportResult.totalRows} seeded players.
              </div>
              {seedImportResult.unmatched.length > 0 && (
                <div className="alert alert--warn">
                  ⚠ {seedImportResult.unmatched.length} row{seedImportResult.unmatched.length !== 1 ? "s" : ""} not matched:
                  <ul style={{ margin: "4px 0 0 16px", padding: 0 }}>
                    {seedImportResult.unmatched.map(({ name, suggestion }) => (
                      <li key={name}>{name}{suggestion ? <span style={{ color: "var(--ink-3)" }}> — did you mean <em>{suggestion}</em>?</span> : ""}</li>
                    ))}
                  </ul>
                </div>
              )}
              <button className="btn btn--sm" style={{ marginTop: 4 }} onClick={() => setSeedImportResult(null)}>Dismiss</button>
            </div>
          )}
          {allTags.length > 0 && (
            <div style={{ padding: "0 16px 10px", display: "flex", gap: 6, flexWrap: "wrap" }}>
              <button className={`radio-pill ${!tagFilter ? "is-active" : ""}`} onClick={() => setTagFilter(null)}>All</button>
              {allTags.map(t => (
                <button key={t} className={`radio-pill ${tagFilter === t ? "is-active" : ""}`} onClick={() => setTagFilter(tagFilter === t ? null : t)}>{t}</button>
              ))}
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
              {visiblePlayers.map((p) => {
                const i = players.indexOf(p);
                return (
                  <div
                    key={p.id}
                    className={`seed-row ${p.seed ? "has-seed" : ""} ${dragOverIdx === i ? "seed-row--drop-target" : ""}`}
                    draggable
                    onDragStart={() => { dragIdxRef.current = i; }}
                    onDragOver={(e) => { e.preventDefault(); setDragOverIdx(i); }}
                    onDragLeave={() => { if (dragOverIdx === i) setDragOverIdx(null); }}
                    onDrop={() => { 
                      moveSeedRow(dragIdxRef.current, i); 
                      dragIdxRef.current = null; 
                      setDragOverIdx(null); 
                    }}
                    style={{ cursor: "grab" }}
                  >
                    <span className="seed-row__handle" title="Drag to reorder">⠿</span>
                    <span className="seed-row__rank">{p.seed ? `#${p.seed}` : ""}</span>
                    <div style={{ flex: 1 }}>
                      <div className="seed-row__name" title={p.name}>{p.name}{p.tag && <span className="tag-badge">{p.tag}</span>}</div>
                      <div className="seed-row__dojo">{p.dojo}</div>
                    </div>
                    <div style={{ display: "flex", flexDirection: "column", gap: 2 }}>
                      <button className="btn btn--sm" style={{ padding: "1px 6px", fontSize: 11 }} onClick={() => moveSeedRow(i, i - 1)} disabled={i === 0}>↑</button>
                      <button className="btn btn--sm" style={{ padding: "1px 6px", fontSize: 11 }} onClick={() => moveSeedRow(i, i + 1)} disabled={i === players.length - 1}>↓</button>
                    </div>
                     <window.StableInput
                        className="seed-row__input"
                        type="number"
                        placeholder="—"
                        value={p.seed || ""}
                        onChange={(val) => updateSeed(i, val)}
                        autoSelect={false}
                      />
                  </div>
                );
              }
              )}
            </div>
          )}
        </div>
      </div>
      {reservedSlots && reservedSlots.length > 0 && (
        <div className="card" style={{ marginTop: 12 }}>
          <div className="card__head">
            <div className="card__title">Reserved slots ({reservedSlots.length})</div>
          </div>
          <div className="card__body" style={{ padding: "0 0 8px" }}>
            {reservedSlots.map(slot => {
              const srcComp = (tournament?.competitions || []).find(cc => cc.id === slot.sourceCompID);
              const ready = srcComp && (srcComp.status === "playoffs" || srcComp.status === "completed");
              return (
                <div key={slot.id} style={{ display: "flex", alignItems: "center", padding: "8px 16px", gap: 8, borderBottom: "1px solid var(--border)" }}>
                  <div style={{ flex: 1 }}>
                    <div style={{ fontWeight: 500, fontSize: 13 }}>
                      {srcComp?.name || slot.sourceCompID} — rank {slot.sourceRank}
                    </div>
                    <span className={`tag-badge ${ready ? "" : "tag-badge--warn"}`}>
                      {ready ? "✓ Source ready" : "⚠ Source not yet in playoffs"}
                    </span>
                  </div>
                  <button className="btn btn--sm btn--danger" onClick={() => removeSlot(slot.id)} title="Remove reserved slot">✕</button>
                </div>
              );
            })}
            {reservedSlots.some(s => { const src = (tournament?.competitions || []).find(cc => cc.id === s.sourceCompID); return !src || (src.status !== "playoffs" && src.status !== "completed"); }) && (
              <div className="alert alert--warn" style={{ margin: "12px 16px 4px" }}>
                ⚠ Some reserved slots cannot be resolved yet. The competition cannot be started until all source competitions have reached playoffs.
              </div>
            )}
          </div>
        </div>
      )}
    </>
  );
}

function AdminImportPage({ tournament, onBack, onImported, onLogout, onViewerMode, password }) {
  const [files, setFiles] = useStateA([]);
  const [preview, setPreview] = useStateA(null);
  const [loading, setLoading] = useStateA(false);
  const [results, setResults] = useStateA(null);
  const [error, setError] = useStateA(null);
  const folderRef = useRefA(null);
  const filesRef = useRefA(null);

  const collectFiles = (fileList) => {
    const arr = Array.from(fileList);
    setFiles(arr);
    setPreview(null);
    setResults(null);
    setError(null);

    // Try to parse manifest client-side for preview (JSON only — YAML needs server).
    const manifestFile = arr.find(f => f.name === "manifest.yaml" || f.name === "manifest.yml" || f.name === "manifest.json");
    if (manifestFile && manifestFile.name.endsWith(".json")) {
      const reader = new FileReader();
      reader.onload = (e) => {
        try {
          const m = JSON.parse(e.target.result);
          setPreview(m.competitions || []);
        } catch { setPreview(null); }
      };
      reader.readAsText(manifestFile);
    } else {
      setPreview(null);
    }
  };

  const doImport = async () => {
    if (!files.length) return;
    if (!confirm("Are you sure you want to import these competitions? This will add new competitions to the tournament.")) return;
    setLoading(true);
    setError(null);
    try {
      const fd = new FormData();
      files.forEach(f => fd.append("files", f, f.webkitRelativePath || f.name));
      const res = await fetch("/api/tournament/import", {
        method: "POST",
        headers: { "X-Tournament-Password": password },
        body: fd,
      });
      const body = await res.json();
      if (!res.ok) {
        setError(body.error || "Import failed");
      } else {
        setResults(body.results || []);
        const hasErrors = (body.results || []).some(r => r.error);
        if (!hasErrors) {
          setTimeout(onImported, 1500);
        }
      }
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  };

  const manifestFile = files.find(f => f.name === "manifest.yaml" || f.name === "manifest.yml" || f.name === "manifest.json");
  const csvFiles = files.filter(f => f.name.endsWith(".csv"));

  return (
    <div className="app">
      <AdminTopbar onLogout={onLogout} onViewerMode={onViewerMode} tournament={tournament} />
      <div className="page">
        <Breadcrumbs items={[{ label: tournament?.name || "Tournament", onClick: onBack }, { label: "Import competitions" }]} />
        <h2 style={{ margin: "0 0 16px" }}>Import competitions</h2>

        <div className="card" style={{ marginBottom: 16 }}>
          <div className="card__title">Select files</div>
          <div className="card__body">
            <p style={{ fontSize: 13, color: "var(--ink-2)", marginTop: 0 }}>
              Select a folder containing <strong>manifest.yaml</strong> and participant CSV files, or select the files individually.
              The manifest must list competitions with their CSV file names.
            </p>
            <div style={{ display: "flex", gap: 10, flexWrap: "wrap", marginBottom: 12 }}>
              <button className="btn btn--primary" onClick={() => folderRef.current?.click()}>Select folder</button>
              <button className="btn" onClick={() => filesRef.current?.click()}>Select files individually</button>
            </div>
            <input ref={folderRef} type="file" style={{ display: "none" }} webkitdirectory="true" multiple onChange={e => collectFiles(e.target.files)} />
            <input ref={filesRef} type="file" style={{ display: "none" }} multiple accept=".yaml,.yml,.json,.csv,.txt" onChange={e => collectFiles(e.target.files)} />

            {files.length > 0 && (
              <div>
                <div style={{ fontSize: 13, color: "var(--ink-2)", marginBottom: 6 }}>
                  {files.length} file{files.length !== 1 ? "s" : ""} selected
                  {manifestFile ? <span className="tag-badge">✓ manifest found: {manifestFile.name}</span> : <span className="tag-badge tag-badge--warn">⚠ no manifest.yaml found</span>}
                  {csvFiles.length > 0 && <span style={{ marginLeft: 6, fontSize: 12 }}>· {csvFiles.length} CSV file{csvFiles.length !== 1 ? "s" : ""}</span>}
                </div>
              </div>
            )}
          </div>
        </div>

        {preview && (
          <div className="card" style={{ marginBottom: 16 }}>
            <div className="card__title">Preview ({preview.length} competitions)</div>
            <div className="card__body">
              <table className="parse-preview" style={{ width: "100%" }}>
                <thead><tr><th>ID</th><th>Name</th><th>Format</th><th>Participants file</th><th>Seeds file</th></tr></thead>
                <tbody>
                  {preview.map(comp => (
                    <tr key={comp.id || comp.name}>
                      <td>{comp.id || "—"}</td>
                      <td>{comp.name || "—"}</td>
                      <td>{comp.format || "pools"}</td>
                      <td className={!comp.participants ? "cell--missing" : ""}>{comp.participants || "—"}</td>
                      <td>{comp.seeds || "—"}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        )}

        {error && <div className="alert alert--error" style={{ marginBottom: 16 }}>{error}</div>}

        {results && (
          <div className="card" style={{ marginBottom: 16 }}>
            <div className="card__title">Import results</div>
            <div className="card__body">
              {results.map(r => (
                <div key={r.id} style={{ padding: "6px 0", borderBottom: "1px solid var(--border)", display: "flex", gap: 8, alignItems: "center" }}>
                  <div style={{ flex: 1 }}>
                    <strong>{r.name || r.id}</strong>
                    {!r.error && <span style={{ fontSize: 12, color: "var(--ink-3)", marginLeft: 8 }}>{pluralize(r.participantCount, "participant")} {r.seedCount > 0 ? `, ${pluralize(r.seedCount, "seed")}` : ""}</span>}
                  </div>
                  {r.error
                    ? <span className="tag-badge tag-badge--warn">✕ {r.error}</span>
                    : <span className="tag-badge">✓ imported</span>}
                </div>
              ))}
              {!results.some(r => r.error) && (
                <div className="alert alert--success" style={{ marginTop: 12 }}>All competitions imported successfully. Returning to dashboard…</div>
              )}
            </div>
          </div>
        )}

        <div style={{ display: "flex", gap: 10 }}>
          <button className="btn btn--primary" onClick={doImport} disabled={!manifestFile || loading}>
            {loading ? "Importing…" : "Import"}
          </button>
          <button className="btn" onClick={onBack}>Cancel</button>
        </div>
      </div>
    </div>
  );
}

function AdminSettings({ c, tournament, onUpdate, onBack, password, showToast }) {
  const [lastSaved, setLastSaved] = useStateA(null);
  const [saveErr, setSaveErr] = useStateA(null);
  const [deleting, setDeleting] = useStateA(false);
  const [local, setLocal] = useStateA({ ...c });
  const debounceRef = useRefA(null);

  useEffectA(() => {
    setLocal(prev => {
      if (debounceRef.current) return prev;
      return { ...prev, ...c };
    });
  }, [c.id, c.name, c.date, c.startTime, c.poolSize, c.poolWinners, c.poolSizeMode, c.courts, c.roundRobin, c.withZekkenName]);

  const saveNow = (next) => {
    const norm = normalizeDate(next.date);
    if (norm && !/^\d{4}-\d{2}-\d{2}$/.test(norm)) {
      setSaveErr("Invalid date format. Use DD-MM-YYYY.");
      return;
    }
    const year = parseInt(norm.substring(0, 4));
    if (year < 1900 || year > 2100) {
      setSaveErr("Year must be between 1900 and 2100.");
      return;
    }

    if (next.name.toLowerCase() !== c.name.toLowerCase()) {
      const exists = (tournament.competitions || []).some(cc => cc.id !== c.id && cc.name.toLowerCase() === next.name.toLowerCase());
      if (exists) {
        setSaveErr(`Competition name "${next.name}" is already in use.`);
        return;
      }
    }

    const finalNext = { ...c, ...next, date: norm };
    Promise.resolve(onUpdate(finalNext)).then(() => {
      const now = new Date();
      setLastSaved(`${String(now.getHours()).padStart(2, "0")}:${String(now.getMinutes()).padStart(2, "0")}:${String(now.getSeconds()).padStart(2, "0")}`);
      setSaveErr(null);
    }).catch((e) => {
      setSaveErr(e?.message || "Save failed");
      showToast(e?.message || "Save failed", "error");
    });
  };

  const saveLater = (next) => {
    clearTimeout(debounceRef.current);
    debounceRef.current = setTimeout(() => {
      debounceRef.current = null;
      saveNow(next);
    }, 400);
  };

  const update = (k, v) => {
    const next = { ...local, [k]: v };
    setLocal(next);
    saveLater(next);
  };

  const updateNow = (k, v) => {
    const next = { ...local, [k]: v };
    setLocal(next);
    saveNow(next);
  };

  const toggleCourt = (cc) => {
    const nextCourts = local.courts.includes(cc) ? local.courts.filter((x) => x !== cc) : [...local.courts, cc].sort();
    if (nextCourts.length) updateNow("courts", nextCourts);
  };

  return (
    <div className="card">
      <div className="card__head">
        <div className="card__title">Competition settings</div>
        <div style={{
          fontSize: 12.5,
          padding: "4px 8px",
          borderRadius: 4,
          background: saveErr ? "var(--red-soft)" : lastSaved ? "var(--accent-soft)" : "transparent",
          color: saveErr ? "var(--red)" : "var(--accent)",
          fontWeight: 600,
          transition: "all 300ms"
        }}>
          {saveErr ? `⚠ ${saveErr}` : lastSaved ? `✓ Saved at ${lastSaved}` : ""}
        </div>
      </div>
      <div className="row">
        <div className="field"><label className="field__label">Display name</label><input className="input" value={local.name} onChange={(e) => update("name", e.target.value)} /></div>
        <div className="field">
          <label className="field__label">Date</label>
          <input className="input" type="date" min="2020-01-01" max="2100-12-31" value={local.date} onChange={(e) => update("date", e.target.value)} />
          <div className="field__hint">Format: DD-MM-YYYY</div>
        </div>
        <div className="field"><label className="field__label">Start time</label><input className="input" type="time" value={local.startTime} onChange={(e) => update("startTime", e.target.value)} /></div>
      </div>
      {local.kind === "team" && (
        <div className="field"><label className="field__label">Team size</label><input className="input" type="number" min="1" max="100" value={local.teamSize} onChange={(e) => update("teamSize", +e.target.value)} /></div>
      )}
      <div className="field">
        <label className="field__label">Assigned shiaijo (courts)</label>
        <div className="radio-group">
          {tournament.courts.map((cc) => (
            <button key={cc} className={`radio-pill ${local.courts.includes(cc) ? "is-active" : ""}`} type="button" onClick={() => toggleCourt(cc)}>Shiaijo (court) {cc}</button>
          ))}
        </div>
        <div className="field__hint">Concurrency = number of shiaijo (courts) assigned. Schedule prevents double-booking with other competitions.</div>
      </div>
      {local.format === "pools" && (
        <>
          <div className="field">
            <label className="field__label">Pool size is a</label>
            <div className="radio-group">
              <button className={`radio-pill ${local.poolSizeMode === "max" ? "is-active" : ""}`} type="button" onClick={() => updateNow("poolSizeMode", "max")}>maximum</button>
              <button className={`radio-pill ${local.poolSizeMode === "min" ? "is-active" : ""}`} type="button" onClick={() => updateNow("poolSizeMode", "min")}>minimum</button>
            </div>
          </div>
          <div className="row">
            <div className="field"><label className="field__label">Players per pool</label><input className="input" type="number" min="3" value={local.poolSize} onChange={(e) => update("poolSize", +e.target.value)} /></div>
            <div className="field"><label className="field__label">Winners per pool</label><input className="input" type="number" min="1" value={local.poolWinners} onChange={(e) => update("poolWinners", +e.target.value)} /></div>
          </div>
        </>
      )}
      <div className="field">
        <label className="field__label">Player number prefix <span style={{ fontWeight: 400, color: "var(--ink-3)" }}>(optional)</span></label>
        <input className="input" placeholder="e.g. A" maxLength="3" value={local.numberPrefix || ""} onChange={(e) => update("numberPrefix", e.target.value.substring(0, 3))} style={{ maxWidth: 80 }} />
        <div className="field__hint">Single letter prefix for participant numbers (A1, B1…). Keeps numbers unique across competitions.</div>
      </div>
      <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
        <label className="checkbox"><input type="checkbox" checked={local.roundRobin} onChange={(e) => updateNow("roundRobin", e.target.checked)} /> Round-robin in pools</label>
<div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
          <label className="checkbox"><input type="checkbox" checked={local.withZekkenName} onChange={(e) => updateNow("withZekkenName", e.target.checked)} disabled={local.kind === "team"} /> Use Zekken display name</label>
          <div className="field__hint" style={{ fontSize: 11, paddingLeft: 22 }}>{local.kind === "team" ? "(Only applicable for individual competitions)" : "When enabled, participant CSV uses three columns: Name, Zekken, Dojo."}</div>
        </div>
      </div>
      <div style={{ marginTop: 24, padding: 16, borderTop: "1px solid var(--line)" }}>
        <button className="btn btn--danger btn--ghost" disabled={deleting} onClick={async () => {
          const started = local.status && local.status !== "setup" && local.status !== "pending";
          const msg = started
            ? `"${local.name}" has already started. Deleting it will remove ALL matches and results. This cannot be undone. Continue?`
            : `Are you sure you want to delete "${local.name}"? This action cannot be undone.`;
          if (confirm(msg)) {
            setDeleting(true);
            try {
              const ok = await window.API.deleteCompetition(local.id, password);
              if (ok) onBack();
              else showToast("Failed to delete competition.", "error");
            } catch (e) {
              console.error("Delete competition failed:", e);
              showToast(e.message, "error");
            } finally {
              setDeleting(false);
            }
          }
        }}>
          {deleting && <span className="spinner" />}
          {deleting ? "Deleting…" : "Delete competition"}
        </button>
        <div className="field__hint" style={{ marginTop: 4 }}>Deleting a started competition will remove all matches and results.</div>
      </div>
    </div>
  );
}

function AdminBracket({ c, t, bracket, onUpdate, onMoveCourt, tweaks, password, showToast }) {
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
  const recordWinner = (winnerSide, _mode = "ippon", ipponLetter = null) => {
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
      .catch(err => showToast(err.message, "error"));
  };

  const overrideWinner = (winnerName) => {
    if (!selected) return;
    window.API.overrideBracketWinner(c.id, selected.matchId, winnerName, password)
      .then(() => onUpdate(c))
      .catch(err => showToast(err.message, "error"));
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

function CourtPicker({ value, courts, onChange, btnClassName = "", label = "", align = "left" }) {
  const [open, setOpen] = useStateA(false);
  const ref = useRefA(null);
  useEffectA(() => {
    if (!open) return;
    const close = (e) => { if (ref.current && !ref.current.contains(e.target)) setOpen(false); };
    document.addEventListener("mousedown", close);
    return () => document.removeEventListener("mousedown", close);
  }, [open]);
  return (
    <div ref={ref} style={{ position: "relative", display: "inline-block" }}>
      <button
        className={btnClassName}
        onClick={(e) => { e.stopPropagation(); setOpen((o) => !o); }}
        title="Change shiaijo"
      >{label}{value} ▾</button>
      {open && (
        <div className="court-popover" style={{ [align === "right" ? "right" : "left"]: 0, top: "100%", marginTop: 4 }}>
          {courts.map((cc) => (
            <button
              key={cc}
              className={cc === value ? "is-current" : ""}
              onClick={(e) => { e.stopPropagation(); setOpen(false); if (cc !== value) onChange(cc); }}
            >{cc}</button>
          ))}
        </div>
      )}
    </div>
  );
}

const LiveMatchPanel = React.memo(({ match, compId, courts, onMoveCourt, onRecord, onOverride }) => {
  const [mode, setMode] = useStateA("tap");
  const [aPoints, setAPoints] = useStateA([]);
  const [bPoints, setBPoints] = useStateA([]);
  useEffectA(() => {
    setAPoints(match.score?.type === "ippon" && match.winner?.id === match.sideA?.id ? match.score.ippons || [] : []);
    setBPoints(match.score?.type === "ippon" && match.winner?.id === match.sideB?.id ? match.score.ippons || [] : []);
  }, [match.id]);
  const a = match.sideA, b = match.sideB;
  const isComplete = match.status === "completed";
  return (
    <div className="live-panel">
      <div className="live-panel__head">
        <div className="live-panel__title">Match · {match.id.slice(-6)}</div>
        <div className="live-panel__court">
          {onMoveCourt && courts && courts.length ? (
            <>
              <CourtPicker
                value={match.court}
                courts={courts}
                onChange={(cc) => onMoveCourt(compId, match.id, cc)}
                btnClassName="live-panel__court-btn"
                label="SHIAIJO "
              />
              <span> · {match.scheduledAt || "TBA"}</span>
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
});
LiveMatchPanel.displayName = "LiveMatchPanel";

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

// ---------- Tournament-wide schedule (admin) ----------
// Estimate minutes from HH:MM string; returns null if invalid
function timeToMinutes(t) {
  if (!t) return null;
  const [h, m] = t.split(":").map(Number);
  if (isNaN(h) || isNaN(m)) return null;
  return h * 60 + m;
}

function AdminSchedulePage({ tournament, onBack, onMoveCourt, _onEditScore, onLogout, onViewerMode, _tweaks, password }) {
  const [picked, setPicked] = useStateA([]);
  const [dojoText, setDojoText] = useStateA("");
  const [compFilter, setCompFilter] = useStateA("all");
  const [matchDuration, setMatchDuration] = useStateA(3); // minutes per match estimate
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
      for (const [_ct, list] of Object.entries(byCt)) {
        let cursor = timeToMinutes(autoStart) || 540;
        for (const m of list.sort((a, b) => (a.scheduledAt || "99:99").localeCompare(b.scheduledAt || "99:99"))) {
          await window.API.updateMatchTime(m.compId, m.id, window.addMinutes("00:00", cursor), password);
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
              <window.StableInput className="input" type="time" value={autoStart} onChange={val => setAutoStart(val)} style={{ width: 120 }} />
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
                      } catch (err) {
                        console.error("Failed to parse drop data:", err);
                      }
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

const AdminTWMatch = React.memo(({ m, highlight, courts, onMove, onTimeChange }) => {
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
            <window.StableInput
              autoFocus
              type="time"
              value={timeVal}
              onChange={val => setTimeVal(val)}
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
      <div style={{ display: "flex", flexDirection: "column", alignItems: "flex-end", gap: 4 }}>
        {m.status === "completed" && (
          <div style={{ fontFamily: "var(--font-mono)", fontWeight: 700, fontSize: 13 }}>{window.formatIpponsScore(m.ipponsB, m.ipponsA, m.score, m.decision)}</div>
        )}
        {m.status === "running" && <span className="bc-live">●</span>}
        <CourtPicker
          value={m.court}
          courts={courts}
          onChange={onMove}
          btnClassName="tw-match__court-btn"
          label="Shiaijo "
          align="right"
        />
      </div>
    </div>
  );
});
AdminTWMatch.displayName = "AdminTWMatch";

// ---------- Score editor ----------
function AdminScoreEditorPage({ tournament, onBack, onEditScore, onMoveCourt, onLogout, onViewerMode, _tweaks }) {
  return (
    <div className="app">
      <AdminTopbar onLogout={onLogout} onViewerMode={onViewerMode} tournament={tournament} />
      <div className="page page--wide" style={{ maxWidth: 1200 }}>
        <Breadcrumbs items={[
          { label: "Dashboard", onClick: onBack },
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
  if (!onMoveCourt || !courts.length) {
    return <div className="score-edit-row__court">{m.court}</div>;
  }
  return (
    <CourtPicker
      value={m.court}
      courts={courts}
      onChange={(cc) => onMoveCourt(m.compId, m.id, cc)}
      btnClassName="score-edit-row__court score-edit-row__court--btn"
    />
  );
}

function AdminScoreEditor({ t, c, onEditScore, onMoveCourt, restrictToCompId, _embedded }) {
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
        <span style={{ marginLeft: "auto", fontSize: 12, color: "var(--ink-3)" }}>{pluralize(filtered.length, "match", "matches")}</span>
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
                  <div className={`score-edit-row__side ${bWin ? "score-edit-row__side--win" : ""}`} style={{ textAlign: "right" }}>
                    <div className="name">{m.sideB?.name}</div>
                    <div className="dojo">{m.sideB?.dojo}</div>
                    <span className="se-color-badge se-color-badge--shiro">SHIRO</span>
                  </div>
                  <div className="score-edit-row__score">
                    {m.status === "completed" && window.formatIpponsScore(m.ipponsB, m.ipponsA, m.score, m.decision)}
                    {m.status === "running" && <span className="bc-live">●</span>}
                    {m.status === "scheduled" && <span style={{ fontSize: 11, color: "var(--ink-3)" }}>vs</span>}
                  </div>
                  <div className={`score-edit-row__side ${aWin ? "score-edit-row__side--win" : ""}`}>
                    <span className="se-color-badge se-color-badge--aka">AKA</span>
                    <div className="name">{m.sideA?.name}</div>
                    <div className="dojo">{m.sideA?.dojo}</div>
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

      {openMatch && (() => {
        const openIdx = filtered.findIndex(m => m.compId + m.id === openMatch.compId + openMatch.id);
        const prevMatch = openIdx > 0 ? filtered[openIdx - 1] : null;
        const nextMatch = openIdx < filtered.length - 1 ? filtered[openIdx + 1] : null;
        return (
          <ScoreEditorModal
            key={openMatch.compId + '-' + openMatch.id}
            match={openMatch}
            tournament={tournament}
            prevMatch={prevMatch}
            nextMatch={nextMatch}
            onPrev={() => setOpenMatch(prevMatch)}
            onNext={() => setOpenMatch(nextMatch)}
            onClose={() => setOpenMatch(null)}
            onSubmit={async (patch) => {
              try {
                await onEditScore(openMatch.compId, openMatch.id, patch, openMatch);
                setOpenMatch(null);
              } catch (_err) {
                // Error handled by onEditScore/toast, but we catch here to keep modal open
              }
            }}
            onSubmitAndNext={nextMatch ? async (patch) => {
              try {
                await onEditScore(openMatch.compId, openMatch.id, patch, openMatch);
                setOpenMatch(nextMatch);
              } catch (_err) { /* keep modal open on error */ }
            } : null}
          />
        );
      })()}
    </div>
  );
}

// Reusable foul counter: independent +/- buttons per side with clear labeling
function FoulCounter({ label, fouls, setFouls, color, hansokuPts }) {
  return (
    <div className={`foul-counter foul-counter--${color}`}>
      <div className="foul-counter__label">{label} Fouls</div>
      <div className="foul-counter__controls">
        <button className="foul-counter__btn foul-counter__btn--dec" onClick={() => setFouls(f => Math.max(0, f - 1))} disabled={fouls === 0}>−</button>
        <div className="foul-counter__count">
          <span className={`foul-counter__num ${fouls >= 2 ? "foul-counter__num--warn" : ""}`}>{fouls}</span>
          {hansokuPts > 0 && <span className="foul-counter__h">→ +{hansokuPts}H to opp.</span>}
        </div>
        <button className="foul-counter__btn foul-counter__btn--inc" onClick={() => setFouls(f => Math.min(4, f + 1))} disabled={fouls >= 4}>+</button>
      </div>
    </div>
  );
}

function ScoreEditorModal({ match, tournament, onClose, onSubmit, onSubmitAndNext, prevMatch, nextMatch, onPrev, onNext }) {
  const m = match;
  const isComplete = m.status === "completed";
  const isTeam = m.compKind === "team";
  const teamSize = m.teamSize || 5;
  if (isTeam) return <TeamScoreEditorModal match={m} teamSize={teamSize} onClose={onClose} onSubmit={onSubmit} onSubmitAndNext={onSubmitAndNext} prevMatch={prevMatch} nextMatch={nextMatch} onPrev={onPrev} onNext={onNext} />;

  const initialAPts = m.ipponsA?.filter(x => x && x !== "•") || (m.score?.type === "ippon" && m.winner?.id === m.sideA?.id ? m.score.ippons || [] : []);
  const initialBPts = m.ipponsB?.filter(x => x && x !== "•") || (m.score?.type === "ippon" && m.winner?.id === m.sideB?.id ? m.score.ippons || [] : []);

  const [aPts, setAPts] = useStateA(initialAPts);
  const [bPts, setBPts] = useStateA(initialBPts);
  const [aFouls, setAFouls] = useStateA(m.hansokuA || m.score?.fouls?.a || 0);
  const [bFouls, setBFouls] = useStateA(m.hansokuB || m.score?.fouls?.b || 0);
  const [note, setNote] = useStateA("");
  const [submitting, setSubmitting] = useStateA(false);

  // Hansoku → ippon awarded to opponent on every 2nd foul
  const aHansokuPts = Math.floor(bFouls / 2);
  const bHansokuPts = Math.floor(aFouls / 2);
  const aTotal = aPts.filter((x) => x !== "•").length + aHansokuPts;
  const bTotal = bPts.filter((x) => x !== "•").length + bHansokuPts;

  const addPt = (side, letter) => {
    if (side === "a") setAPts((p) => p.length < 2 ? [...p, letter] : p);
    else setBPts((p) => p.length < 2 ? [...p, letter] : p);
  };
  const removePt = (side, idx) => {
    if (side === "a") setAPts((p) => p.filter((_, i) => i !== idx));
    else setBPts((p) => p.filter((_, i) => i !== idx));
  };

  const buildPatch = (targetStatus) => {
    const fouls = { a: aFouls, b: bFouls };
    if (targetStatus === "scheduled") return { winner: null, status: "scheduled", score: null, ipponsA: [], ipponsB: [], hansokuA: 0, hansokuB: 0 };
    if (targetStatus === "running") return {
      status: "running", winner: null,
      ipponsA: aPts.filter(x => x !== "•"), ipponsB: bPts.filter(x => x !== "•"),
      hansokuA: aFouls, hansokuB: bFouls,
      score: { type: "ippon", winnerPts: aTotal, loserPts: bTotal, ippons: aPts, fouls, live: true, corrected: isComplete, note },
    };
    if (isDrawToggled) return { winner: null, ipponsA: [], ipponsB: [], hansokuA: aFouls, hansokuB: bFouls, status: "completed", score: { type: "hikiwake", winnerPts: 0, loserPts: 0, fouls, corrected: isComplete, note } };
    // ippon
    const aLetters = aPts.filter(x => x !== "•");
    const bLetters = bPts.filter(x => x !== "•");
    const aFinal = [...aLetters, ...Array(aHansokuPts).fill("H")].slice(0, 2);
    const bFinal = [...bLetters, ...Array(bHansokuPts).fill("H")].slice(0, 2);
    const winnerSide = aFinal.length > bFinal.length ? "a" : bFinal.length > aFinal.length ? "b" : null;
    if (!winnerSide) return { winner: null, ipponsA: aFinal, ipponsB: bFinal, hansokuA: aFouls, hansokuB: bFouls, status: "completed", score: { type: "hikiwake", winnerPts: 0, loserPts: 0, fouls, corrected: isComplete, note } };
    const winner = winnerSide === "a" ? m.sideA : m.sideB;
    const ippons = winnerSide === "a" ? aFinal : bFinal;
    return { winner, ipponsA: aFinal, ipponsB: bFinal, hansokuA: aFouls, hansokuB: bFouls, status: "completed", score: { type: "ippon", winnerPts: ippons.length, loserPts: (winnerSide === "a" ? bFinal : aFinal).length, ippons, fouls, corrected: isComplete, note } };
  };

  const doSubmit = async (fn) => {
    setSubmitting(true);
    try { await fn(); } finally { setSubmitting(false); }
  };

  const [isDrawToggled, setIsDrawToggled] = useStateA(m.score?.type === "hikiwake" || m.decision === "hikewake");

  // Arranged as [left, right] — left is always SHIRO (White), right is always AKA (Red)
  const sides = [
    { key: "b", name: m.sideB?.name, dojo: m.sideB?.dojo, pts: bPts, fouls: bFouls, setFouls: setBFouls, hansokuPts: bHansokuPts, color: "shiro", label: "SHIRO (White)" },
    { key: "a", name: m.sideA?.name, dojo: m.sideA?.dojo, pts: aPts, fouls: aFouls, setFouls: setAFouls, hansokuPts: aHansokuPts, color: "aka", label: "AKA (Red)" },
  ];

  const canFinish = isDrawToggled || aTotal > 0 || bTotal > 0;

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="editor-modal editor-modal--lg" onClick={(e) => e.stopPropagation()}>
        <div className="editor-modal__head">
          <div style={{ flex: 1 }}>
            <div style={{ fontSize: 11, color: "var(--ink-3)", textTransform: "uppercase", letterSpacing: "0.1em", fontWeight: 700 }}>
              {m.compName} · {m.phase === "pool" ? m.poolName : m.round}
            </div>
            <div style={{ fontSize: 20, fontWeight: 700, marginTop: 2, letterSpacing: "-0.01em" }}>
              Shiaijo {m.court} · {m.scheduledAt || "Live"}
            </div>
          </div>
          <div style={{ display: "flex", flexDirection: "column", alignItems: "flex-end", gap: 4 }}>
            <div className={`viewer__admin-pill ${m.status === "running" ? "sched-row--live" : ""}`} style={{ fontSize: 10, fontWeight: 700 }}>
              {isComplete ? "CORRECTION" : m.status === "running" ? "● LIVE" : "PRE-MATCH"}
            </div>
            <button className="btn btn--ghost btn--sm" onClick={onClose} style={{ padding: "2px 8px" }}>✕ Close</button>
          </div>
        </div>

        <div className="editor-modal__body">
          <div className="scoring-board">
              {/* Score slots + point buttons */}
              <div className="sb-match">
                {sides.map((s, idx) => (
                  <React.Fragment key={s.key}>
                    <div className={`sb-side sb-side--${s.color}`}>
                      <div className="sb-name">{s.name}</div>
                      <div className="sb-dojo">{s.label}</div>
                      <div className="sb-slots">
                        {[0, 1].map((i) => (
                          <button key={i} className={`sb-slot ${s.pts[i] ? "sb-slot--filled" : ""}`} onClick={() => removePt(s.key, i)} title="Click to remove">
                            {s.pts[i] || "·"}
                          </button>
                        ))}
                      </div>
                      <div className="sb-points-grid">
                        {["M", "K", "D", "T", "H"].map((cc) => (
                          <button key={cc} className={`ipt-btn ${cc === "H" ? "ipt-btn--h" : ""}`} onClick={() => addPt(s.key, cc)} disabled={s.pts.length >= 2}>{cc}</button>
                        ))}
                      </div>
                    </div>
                    {idx === 0 && (
                      <div className="sb-center">
                        {isDrawToggled ? (
                          <button className="sb-draw-toggle sb-draw-toggle--active" onClick={() => { setIsDrawToggled(false); }} title="Cancel draw">X</button>
                        ) : aTotal === 0 && bTotal === 0 ? (
                          <button className="sb-draw-toggle" onClick={() => { setIsDrawToggled(true); setAPts([]); setBPts([]); }} title="Mark as draw (hikiwake)">vs</button>
                        ) : (
                          <div className="sb-vs">{`${bTotal}–${aTotal}`}</div>
                        )}
                      </div>
                    )}
                  </React.Fragment>
                ))}
              </div>

              {/* Independent foul counters */}
              <div className="sb-fouls">
                {sides.map((s) => (
                  <FoulCounter
                    key={s.key}
                    label={s.label}
                    fouls={s.fouls}
                    setFouls={s.setFouls}
                    color={s.color}
                    hansokuPts={s.hansokuPts}
                  />
                ))}
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

        {/* Sticky navigation + action footer */}
        <div className="editor-modal__foot editor-modal__foot--nav">
          <div className="score-nav">
            {prevMatch ? (
              <button className="btn btn--sm score-nav__prev" onClick={onPrev} disabled={submitting} title={prevMatch.sideA?.name + " vs " + prevMatch.sideB?.name}>← Prev</button>
            ) : <span />}

            <div className="score-nav__actions">
              {m.status === "scheduled" && (
                <button className="btn btn--sm" onClick={() => doSubmit(() => onSubmit(buildPatch("running")))} disabled={submitting}>
                  ▶ Start Match
                </button>
              )}
              <button className="btn" onClick={onClose} disabled={submitting}>Cancel</button>
              {onSubmitAndNext ? (
                <button className="btn btn--primary" onClick={() => doSubmit(() => onSubmitAndNext(buildPatch("completed")))} disabled={submitting || !canFinish}>
                  {submitting ? "Saving…" : "Finish + Start Next →"}
                </button>
              ) : (
                <button className="btn btn--primary" onClick={() => doSubmit(() => onSubmit(buildPatch("completed")))} disabled={submitting || !canFinish}>
                  {submitting ? "Saving…" : isComplete ? "Save correction" : "Finish"}
                </button>
              )}
            </div>

            {nextMatch ? (
              <button className="btn btn--sm score-nav__next" onClick={onNext} disabled={submitting} title={nextMatch.sideA?.name + " vs " + nextMatch.sideB?.name}>Next →</button>
            ) : <span />}
          </div>
        </div>
      </div>
    </div>
  );
}

const TEAM_POSITIONS = ["1", "2", "3", "4", "5", "6", "7", "8", "9"];

function TeamScoreEditorModal({ match, teamSize, onClose, onSubmit, onSubmitAndNext, prevMatch, nextMatch, onPrev, onNext }) {
  const m = match;
  const isComplete = m.status === "completed";
  const positions = TEAM_POSITIONS.slice(0, teamSize);
  const [note, setNote] = useStateA("");
  const [submitting, setSubmitting] = useStateA(false);

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

  const buildPatch = (targetStatus) => {
    if (targetStatus === "scheduled") return { winner: null, status: "scheduled", score: null, ipponsA: [], ipponsB: [], subResults: [] };
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
    return {
      winner,
      status: targetStatus === "running" ? "running" : "completed",
      ipponsA: [],
      ipponsB: [],
      score: { type: teamWinner ? "ippon" : "hikiwake", winnerPts: teamWinner === "a" ? ivA : ivB, loserPts: teamWinner === "a" ? ivB : ivA, fouls: { a: 0, b: 0 }, corrected: isComplete, note },
      subResults,
    };
  };

  const doSubmit = async (fn) => {
    setSubmitting(true);
    try { await fn(); } finally { setSubmitting(false); }
  };

  // left = SHIRO (White), right = AKA (Red)
  const teamSides = [
    { key: "b", name: m.sideB?.name || m.sideB, label: "SHIRO (White)", color: "shiro", iv: ivB, pw: pwB },
    { key: "a", name: m.sideA?.name || m.sideA, label: "AKA (Red)", color: "aka", iv: ivA, pw: pwA },
  ];

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="editor-modal editor-modal--team" onClick={(e) => e.stopPropagation()}>
        <div className="editor-modal__head">
          <div style={{ flex: 1 }}>
            <div style={{ fontSize: 11, color: "var(--ink-3)", textTransform: "uppercase", letterSpacing: "0.1em", fontWeight: 700 }}>
              {m.compName} · {m.phase === "pool" ? m.poolName : m.round}
            </div>
            <div style={{ fontSize: 20, fontWeight: 700, marginTop: 2, letterSpacing: "-0.01em" }}>
              Shiaijo {m.court} · {m.scheduledAt || "Live"}
            </div>
          </div>
          <div style={{ display: "flex", flexDirection: "column", alignItems: "flex-end", gap: 4 }}>
            <div className={`viewer__admin-pill ${m.status === "running" ? "sched-row--live" : ""}`} style={{ fontSize: 10, fontWeight: 700 }}>
              {isComplete ? "CORRECTION" : m.status === "running" ? "● LIVE" : "PRE-MATCH"}
            </div>
            <button className="btn btn--ghost btn--sm" onClick={onClose} style={{ padding: "2px 8px" }}>✕ Close</button>
          </div>
        </div>

        <div className="editor-modal__body">
          {/* Team header */}
          <div className="sb-match" style={{ marginBottom: 16 }}>
            {teamSides.map((s, idx) => (
              <React.Fragment key={s.key}>
                <div className={`sb-side sb-side--${s.color}`}>
                  <div className="sb-name">{s.name}</div>
                  <div className="sb-dojo">{s.label}</div>
                </div>
                {idx === 0 && (
                  <div className="sb-center">
                    <div className="sb-vs">VS</div>
                  </div>
                )}
              </React.Fragment>
            ))}
          </div>

          {/* Individual match rows */}
          {positions.map((pos, idx) => {
            const s = subs[idx];
            const t = subTotals[idx];

            // Each row: [left side, center score, right side] — left=SHIRO, right=AKA
            const rowSides = [
              { key: "b", pts: s.bPts, fouls: s.bFouls, hansokuPts: t.bHansoku,
                setPts: (pts) => updateSub(idx, prev => ({ ...prev, bPts: pts })),
                setFouls: (f) => updateSub(idx, prev => ({ ...prev, bFouls: f })),
                color: "shiro", label: "SHIRO" },
              { key: "a", pts: s.aPts, fouls: s.aFouls, hansokuPts: t.aHansoku,
                setPts: (pts) => updateSub(idx, prev => ({ ...prev, aPts: pts })),
                setFouls: (f) => updateSub(idx, prev => ({ ...prev, aFouls: f })),
                color: "aka", label: "AKA" },
            ];

            const scoreDisplay = (() => {
              if (t.winner === null && t.aTotal === 0 && t.bTotal === 0) return <span style={{ color: "var(--ink-3)" }}>–</span>;
              if (t.winner === null) return <span className="tsm-draw">X</span>;
              return <span>{`${t.bTotal}–${t.aTotal}`}</span>;
            })();

            return (
              <div key={idx} className="team-sub-match">
                <div className="team-sub-match__pos">Match {pos}</div>
                <div className="team-sub-match__row">
                  {rowSides.map((rs, rsIdx) => (
                    <React.Fragment key={rs.key}>
                      <div className={`team-sub-match__side ${rsIdx === 1 ? "team-sub-match__side--right" : ""}`}>
                        <div className="tsm-side-label">{rs.label}</div>
                        {/* Point slots */}
                        <div className="team-sub-match__pts">
                          {[0, 1].map(i => (
                            <button key={i} className={`editor-side__pt ${rs.pts[i] ? "editor-side__pt--filled" : ""}`}
                              onClick={() => rs.setPts(rs.pts.filter((_, j) => j !== i))} title="Click to remove">
                              {rs.pts[i] || "·"}
                            </button>
                          ))}
                        </div>
                        {/* Point buttons incl. H */}
                        <div className="team-sub-match__btns">
                          {["M", "K", "D", "T", "H"].map(cc => (
                            <button key={cc} className={`ipt-btn ipt-btn--sm ${cc === "H" ? "ipt-btn--h" : ""}`}
                              onClick={() => rs.setPts(rs.pts.length < 2 ? [...rs.pts, cc] : rs.pts)}
                              disabled={rs.pts.length >= 2}>{cc}</button>
                          ))}
                        </div>
                        {/* Independent foul counter */}
                        <div className="tsm-fouls">
                          <span className="tsm-fouls__label">{rs.label} Fouls</span>
                          <div className="tsm-fouls__controls">
                            <button className="tsm-fouls__btn" onClick={() => rs.setFouls(f => Math.max(0, f - 1))} disabled={rs.fouls === 0}>−</button>
                            <span className={`tsm-fouls__count ${rs.fouls >= 2 ? "tsm-fouls__count--warn" : ""}`}>{rs.fouls}</span>
                            <button className="tsm-fouls__btn" onClick={() => rs.setFouls(f => Math.min(4, f + 1))} disabled={rs.fouls >= 4}>+</button>
                          </div>
                          {rs.hansokuPts > 0 && <span className="tsm-fouls__h">→ +{rs.hansokuPts}H</span>}
                        </div>
                      </div>
                      {rsIdx === 0 && (
                        <div className={`team-sub-match__score ${t.winner === "b" ? "team-sub-match__score--a-win" : t.winner === "a" ? "team-sub-match__score--b-win" : ""}`}>
                          {scoreDisplay}
                        </div>
                      )}
                    </React.Fragment>
                  ))}
                </div>
              </div>
            );
          })}

          {/* Team summary */}
          <div className="team-summary">
            {teamSides.map((ts, idx) => (
              <React.Fragment key={ts.key}>
                <div className="team-summary__side">
                  <div className="team-summary__label">{ts.label}</div>
                  <div className="team-summary__stats">IV: {ts.iv} · PW: {ts.pw}</div>
                </div>
                {idx === 0 && (
                  <div className="team-summary__result">
                    {teamWinner === "a" ? "AKA WIN" : teamWinner === "b" ? "SHIRO WIN" : "DRAW"}
                    <div style={{ fontSize: 14, opacity: 0.6, marginTop: 4 }}>RESULT</div>
                  </div>
                )}
              </React.Fragment>
            ))}
          </div>

          {isComplete && (
            <div className="field">
              <label className="field__label">Correction note (optional)</label>
              <input className="input" placeholder="e.g. Reviewed video" value={note} onChange={(e) => setNote(e.target.value)} />
            </div>
          )}
        </div>

        <div className="editor-modal__foot editor-modal__foot--nav">
          <div className="score-nav">
            {prevMatch ? (
              <button className="btn btn--sm score-nav__prev" onClick={onPrev} disabled={submitting}>← Prev</button>
            ) : <span />}
            <div className="score-nav__actions">
              {m.status === "scheduled" && (
                <button className="btn btn--sm" onClick={() => doSubmit(() => onSubmit(buildPatch("running")))} disabled={submitting}>▶ Start</button>
              )}
              <button className="btn" onClick={onClose} disabled={submitting}>Cancel</button>
              {onSubmitAndNext ? (
                <button className="btn btn--primary" onClick={() => doSubmit(() => onSubmitAndNext(buildPatch("completed")))} disabled={submitting}>
                  {submitting ? "Saving…" : "Finish + Start Next →"}
                </button>
              ) : (
                <button className="btn btn--primary" onClick={() => doSubmit(() => onSubmit(buildPatch("completed")))} disabled={submitting}>
                  {submitting ? "Saving…" : isComplete ? "Save correction" : "Finish"}
                </button>
              )}
            </div>
            {nextMatch ? (
              <button className="btn btn--sm score-nav__next" onClick={onNext} disabled={submitting}>Next →</button>
            ) : <span />}
          </div>
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
