// Tournament-wide schedule, score editor, and export pages. See
// web-mobile/admin_split_plan.md.

const { useState: useStateA, useMemo: useMemoA } = React;

const pluralize = window.pluralize;
const AdminTopbar = window.AdminTopbar;
const Breadcrumbs = window.Breadcrumbs;
const CourtPicker = window.CourtPicker;
const ScoreEditorModal = window.ScoreEditorModal;

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
  // Matches whose court isn't in the configured list (missing, removed from
  // config, or stale) go into a separate "Unassigned" bucket so they stay
  // visible and movable instead of vanishing into an unrendered key.
  const unassigned = [];
  filtered.forEach((m) => {
    if (m.court && byCourt[m.court]) byCourt[m.court].push(m);
    else unassigned.push(m);
  });
  const courtOrder = (a, b) => {
    const order = { running: 0, scheduled: 1, completed: 2 };
    const ao = order[a.status] ?? 99;
    const bo = order[b.status] ?? 99;
    if (ao !== bo) return ao - bo;
    return (a.scheduledAt || "99:99").localeCompare(b.scheduledAt || "99:99");
  };
  Object.values(byCourt).forEach((list) => list.sort(courtOrder));
  unassigned.sort(courtOrder);

  const matchHasFilter = (m) => window.matchHighlightedBy(m, picked, dojoText);
  const hasAnyFilter = picked.length > 0 || dojoText || compFilter !== "all";

  // Duration estimation: earliest start to latest finish across all matches
  // matchDuration is bound to a <input type="number"> whose value can become
  // NaN if cleared. Coerce to a sane fallback before any arithmetic so the
  // duration estimate and auto-schedule loop don't propagate NaN.
  const safeMatchDuration = Number.isFinite(matchDuration) && matchDuration >= 1 ? matchDuration : 3;
  const scheduledWithTimes = allMatches.filter(m => m.scheduledAt);
  const firstTime = scheduledWithTimes.length > 0 ? scheduledWithTimes.reduce((mn, m) => !mn || m.scheduledAt < mn ? m.scheduledAt : mn, null) : null;
  const lastTime = scheduledWithTimes.length > 0 ? scheduledWithTimes.reduce((mx, m) => !mx || m.scheduledAt > mx ? m.scheduledAt : mx, null) : null;
  const durationEstimate = firstTime && lastTime ? (() => {
    const a = timeToMinutes(firstTime);
    const b = timeToMinutes(lastTime);
    if (a === null || b === null) return null;
    const diff = b - a + safeMatchDuration;
    return `${Math.floor(diff / 60)}h ${diff % 60}m`;
  })() : null;

  const saveMatchTime = async (m, newTime) => {
    try {
      await window.API.updateMatchTime(m.compId, m.id, newTime, password);
    } catch (e) {
      alert("Failed to update time: " + e.message);
    }
  };

  // Auto-schedule: assign sequential times to matches within a competition per court.
  // Operate on allMatches so UI filters (player/dojo/competition pill) don't
  // shrink the set being scheduled — otherwise it's easy to time only a subset.
  const autoSchedule = async () => {
    const comp = tournament.competitions.find(c => c.id === autoComp);
    if (!comp) return;
    // Skip matches with no/unknown court — the per-court scheduler assumes
    // each match lives on one of the configured courts; otherwise we'd
    // create a phantom bucket and time matches that the user hasn't placed.
    const compMatches = allMatches.filter(m => m.compId === autoComp && courts.includes(m.court));
    const byCt = {};
    courts.forEach(cc => byCt[cc] = []);
    compMatches.forEach(m => byCt[m.court].push(m));

    setAutoSaving(true);
    try {
      // Assign times: each court runs in parallel from autoStart, matches spaced by matchDuration
      for (const [_ct, list] of Object.entries(byCt)) {
        let cursor = timeToMinutes(autoStart) || 540;
        for (const m of list.sort((a, b) => (a.scheduledAt || "99:99").localeCompare(b.scheduledAt || "99:99"))) {
          await window.API.updateMatchTime(m.compId, m.id, window.addMinutes("00:00", cursor), password);
          cursor += safeMatchDuration;
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
                        key={`${m.compId}:${m.id}`}
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
            {unassigned.length > 0 && (
              <div className="tw-court">
                <div className="tw-court__head">
                  <div>
                    <div className="tw-court__title">UNASSIGNED</div>
                    <div className="tw-court__sub">{unassigned.length} match{unassigned.length === 1 ? "" : "es"}</div>
                  </div>
                </div>
                <div className="tw-court__list">
                  {unassigned.map((m) => (
                    <AdminTWMatch
                      key={`${m.compId}:${m.id}`}
                      m={m}
                      highlight={matchHasFilter(m)}
                      courts={courts}
                      onMove={(toCourt) => onMoveCourt(m.compId, m.id, toCourt)}
                      onTimeChange={(newTime) => saveMatchTime(m, newTime)}
                    />
                  ))}
                </div>
              </div>
            )}
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

  // Status keys must match backend values; sort live first, then upcoming,
  // then completed. Anything unrecognized goes to the end.
  const order = { running: 0, scheduled: 1, completed: 2 };
  filtered.sort((a, b) => {
    const ao = order[a.status] ?? 99;
    const bo = order[b.status] ?? 99;
    if (ao !== bo) return ao - bo;
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
            <div key={`${m.compId}:${m.id}`} className={`score-edit-row ${m.status === "running" ? "score-edit-row--live" : ""} ${m.status === "completed" ? "score-edit-row--complete" : ""}`}>
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
        const openIdx = filtered.findIndex(m => `${m.compId}:${m.id}` === `${openMatch.compId}:${openMatch.id}`);
        const prevMatch = openIdx > 0 ? filtered[openIdx - 1] : null;
        const nextMatch = openIdx < filtered.length - 1 ? filtered[openIdx + 1] : null;
        return (
          <ScoreEditorModal
            key={openMatch.compId + '-' + openMatch.id}
            match={openMatch}
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

  const copyUrl = async () => {
    try {
      await navigator.clipboard.writeText(url);
      alert("URL copied to clipboard!");
    } catch (err) {
      alert("Failed to copy URL: " + (err?.message || err));
    }
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

window.AdminSchedulePage = AdminSchedulePage;
window.AdminScoreEditorPage = AdminScoreEditorPage;
window.AdminScoreEditor = AdminScoreEditor;
window.AdminExport = AdminExport;
