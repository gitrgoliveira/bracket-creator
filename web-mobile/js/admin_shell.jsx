// Admin shell: chrome (Breadcrumbs, AdminTopbar, CourtPicker) plus the
// dashboard page (AdminDashboard, CompCard). All cross-page shared admin UI
// lives here so it loads before any section-specific file. See
// web-mobile/admin_split_plan.md.

const { useState: useStateA, useMemo: useMemoA, useEffect: useEffectA, useRef: useRefA } = React;

// Producers (loaded earlier).
const sideName = window.sideName;
const compMatchStats = window.compMatchStats;
const pluralize = window.pluralize;
const StatusBadge = window.StatusBadge;
const formatDate = window.formatDate;
const formatLabelShort = window.formatLabelShort;

// Maximum live-match chips rendered in the topbar status strip before the
// "+N more" overflow indicator kicks in.
const LIVE_STRIP_MAX_CHIPS = 6;

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

function AdminTopbar({ onLogout, onViewerMode, tournament }) {
  // Render running matches as chips below the topbar so admins always
  // know what's live, regardless of which screen they're on. Clicking
  // a chip jumps to that competition's score editor via the global
  // navigator helper set up by AdminApp. sideName (hoisted to module
  // scope) handles the three possible side shapes; chips with no real
  // name on either side are filtered out.
  const liveMatches = useMemoA(() => {
    if (!tournament || !window.compMatches) return [];
    return (tournament.competitions || [])
      .flatMap(cc => window.compMatches(cc))
      .filter(m => m.status === "running" && sideName(m.sideA) && sideName(m.sideB));
  }, [tournament]);
  const onOpenScore = (m) => {
    if (typeof window.__adminNavigateToScore === "function") {
      window.__adminNavigateToScore(m.compId);
    }
  };

  return (
    // Wrap topbar + live-strip in a single sticky container so they scroll
    // together. This lets the topbar size naturally (min-height instead of a
    // fixed height) — robust to font scaling / browser zoom — while still
    // keeping the live-strip visually anchored beneath it.
    <div className="topbar-stack">
      <div className="topbar">
        <div className="topbar__brand">
          <img src="/logo.jpeg" alt="Kendo Tournament Logo" className="topbar__logo" decoding="async" />
          <div>
            <div className="topbar__title">{tournament?.name || "Bracket Creator"}</div>
            <div className="topbar__sub">Admin console</div>
          </div>
        </div>
        <div className="topbar__spacer"></div>
        <button className="viewer-toggle" onClick={onViewerMode}>👁 Public viewer</button>
        <button className="btn btn--ghost btn--sm" onClick={onLogout}>Sign out</button>
      </div>
      {liveMatches.length > 0 && (
        <div className="live-strip" role="region" aria-label={`${pluralize(liveMatches.length, "match", "matches")} live`}>
          <span className="live-strip__lbl"><span className="dot dot--live"></span> {pluralize(liveMatches.length, "match", "matches")} live</span>
          <div className="live-strip__chips">
            {liveMatches.slice(0, LIVE_STRIP_MAX_CHIPS).map(m => {
              const a = sideName(m.sideA);
              const b = sideName(m.sideB);
              const courtLabel = m.court ? `Shiaijo ${m.court}` : "Unassigned";
              const courtPhrase = m.court ? `on Shiaijo ${m.court}` : "with no court assigned";
              return (
                <button
                  key={`${m.compId}:${m.id}`}
                  className="live-strip__chip"
                  onClick={() => onOpenScore && onOpenScore(m)}
                  aria-label={`Open score editor for ${b} versus ${a} ${courtPhrase}`}
                >
                  <span className="live-strip__court">{courtLabel}</span>
                  <span className="live-strip__names">{b} – {a}</span>
                </button>
              );
            })}
            {liveMatches.length > LIVE_STRIP_MAX_CHIPS && <span className="live-strip__more">+{liveMatches.length - LIVE_STRIP_MAX_CHIPS} more</span>}
          </div>
        </div>
      )}
    </div>
  );
}

function initialSectionFor(status) {
  if (status === "setup") return "participants";
  if (status === "pools" || status === "playoffs") return "scores";
  return "overview";
}

function AdminDashboard({ tournament, onOpenCompetition, onCreateCompetition, onEditTournament, onAnnounce, onOpenSchedule, onOpenScoreEditor, onOpenImport, onStartAll, onStartCompetition, onLogout, onViewerMode, onUpdate }) {
  const t = tournament;
  const comps = t.competitions || [];

  useEffectA(() => {
    // Coalesce bursts of events into a single dashboard refresh. On a busy
    // event the SSE stream can fire dozens of match_updated per minute, and
    // each one previously triggered a full tournament+competitions fetch.
    let pending = null;
    let fetching = false;
    // Track events that arrive during a pending timer or in-flight fetch
    // so they don't silently get dropped — kick off one more refresh in
    // the finally block when set.
    let needsRefresh = false;
    const scheduleRefresh = () => {
      if (pending || fetching) { needsRefresh = true; return; }
      const jitter = 500 + Math.random() * 1000; // 500-1500ms window
      pending = setTimeout(() => {
        pending = null;
        // Keep the gate closed for the duration of the fetch so a second
        // burst can't fire a concurrent request that might resolve
        // out-of-order and clobber fresher data.
        fetching = true;
        Promise.all([
          window.API.fetchTournament(),
          window.API.fetchCompetitions()
        ]).then(([tourney, competitions]) => {
          onUpdate({ ...tourney, competitions });
        }).catch(err => console.error("Dashboard refresh failed", err))
          .finally(() => {
            fetching = false;
            if (needsRefresh) {
              needsRefresh = false;
              scheduleRefresh();
            }
          });
      }, jitter);
    };
    const unsub = window.API.subscribeToEvents((event) => {
      if (event.type === "tournament_updated" || event.type === "competition_started" || event.type === "competition_completed" || event.type === "competition_deleted" || event.type === "match_updated" || event.type === "schedule_updated") {
        scheduleRefresh();
      }
    });
    return () => {
      if (pending) clearTimeout(pending);
      unsub();
    };
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
            <button className="btn" onClick={onAnnounce}>📣 Announce</button>
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
    <div
      className="tcard"
      role="button"
      tabIndex={0}
      onClick={onOpen}
      // Only fire when focus is on the card itself, not on a nested
      // <button> inside .tcard__actions — otherwise Enter on
      // "Start Competition" would also trigger onOpen.
      onKeyDown={(e) => { if (e.target === e.currentTarget && (e.key === "Enter" || e.key === " ")) { e.preventDefault(); onOpen(); } }}
    >
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
        <div className="tcard__stat"><div className="v">{formatLabelShort(c.format)}</div><div className="l">Format</div></div>
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

window.Breadcrumbs = Breadcrumbs;
window.AdminTopbar = AdminTopbar;
window.AdminDashboard = AdminDashboard;
window.CourtPicker = CourtPicker;
