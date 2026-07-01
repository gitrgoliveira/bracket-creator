// Schedule page components extracted from admin_schedule.jsx (mp-d7tl).
// EstInput (local), AdminTWMatch (local), AdminSchedulePage.

import { filterMatchesByCourt, CourtPacePanel, PerCourtBreakdown } from './admin_schedule_pacing.jsx';
import { formatMinutes, timeToMinutes, timeEdited, clampMatchDuration, COURT_STORAGE_KEY } from './admin_schedule_utils.jsx';

const { useState: useStateA, useMemo: useMemoA, useEffect: useEffectA, useRef: useRefA } = React;

const AdminTopbar = window.AdminTopbar;
const Breadcrumbs = window.Breadcrumbs;
const CourtPicker = window.CourtPicker;
const hasBothSides = window.hasBothSides;

function EstInput({ label, value, setter, min, max, step = "1" }) {
  return (
    <div className="form-group">
      <label className="label">{label}</label>
      <input
        type="number"
        className="input"
        value={Number.isFinite(value) ? value : ""}
        min={min}
        max={max}
        step={step}
        onChange={e => { const val = e.target.value; setter(val === "" ? NaN : +val); }}
      />
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
    if (onTimeChange && timeEdited(m.scheduledAt, timeVal)) onTimeChange(timeVal);
  };
  return (
    <div
      className={`tw-match ${m.status === "running" ? "tw-match--running" : ""} ${m.status === "completed" ? "tw-match--done" : ""} ${highlight ? "tw-match--highlight" : ""}`}
      draggable
      onDragStart={(e) => {
        e.dataTransfer.setData("application/json", JSON.stringify({ compId: m.compId, matchId: m.id }));
        e.dataTransfer.effectAllowed = "move";
      }}
      style={{ cursor: "grab", position: "relative" }}
    >
      <div className="tw-match__meta">
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
          <button type="button"
            className="tw-match__time tw-match__time--editable"
            onClick={(e) => { e.stopPropagation(); if (onTimeChange) { setTimeVal(m.scheduledAt || ""); setEditingTime(true); } }}
            title="Click to set time"
          >
            {m.scheduledAt || <span style={{ color: "var(--ink-4)", fontWeight: 400 }}>-</span>}
            {onTimeChange && <span style={{ fontSize: 9, color: "var(--ink-4)", marginLeft: 2 }}>✎</span>}
          </button>
        )}
        <div className="tw-match__phase">
          {m.phase === "pool" ? window.poolLabel(m) : m.round}
          {m.round != null && typeof m.round === "number" && m.round >= 0 && (
            <span className="tw-match__round">R{m.round + 1}</span>
          )}
        </div>
      </div>
      <div className="tw-match__players">
        <div className={`tw-match__name ${bWin ? "tw-match__name--w" : ""}`}>
          <span className="tw-match__badge tw-match__badge--shiro">S</span>
          {m.sideB?.number ? <span className="num-prefix">{m.sideB.number}</span> : null}
          {m.sideB?.name || "TBD"}
        </div>
        <div className={`tw-match__name ${aWin ? "tw-match__name--w" : ""}`}>
          <span className="tw-match__badge tw-match__badge--aka">A</span>
          {m.sideA?.number ? <span className="num-prefix">{m.sideA.number}</span> : null}
          {m.sideA?.name || "TBD"}
        </div>
        <div className="tw-match__comp">{m.compName}</div>
      </div>
      {/* T097: formatIpponsScore appends "Kiken / Fus. / DH / (E)" suffixes
          for non-fought decisions and overtime. The match-level decision
          covers kiken / fusenpai / daihyosen here; per-bout fusensho is a
          SubMatchResult and is rendered inside the score modal: the row
          here doesn't expose individual bout cells.
          TODO(T096): once per-bout fusensho is wired through the team-score
          serializer and the schedule row exposes bout details, append an
          "FS" badge to each affected bout cell. */}
      <div style={{ display: "flex", flexDirection: "column", alignItems: "flex-end", gap: 4 }}>
        {m.status === "completed" && (() => {
          // Bracket matches carry scoreA/scoreB rather than ipponsA/B.
          // Derive per-side arrays so the score reads SHIRO–AKA correctly
          // even when AKA wins (winnerPts–loserPts fallback inverts left/right).
          // Use ipponsFromScore so the trailing "(HN)" hansoku suffix from
          // Go's formatScore doesn't get split into bogus ippon letters.
          const tIpponsA = m.ipponsA || window.ipponsFromScore(m.scoreA);
          const tIpponsB = m.ipponsB || window.ipponsFromScore(m.scoreB);
          return <div style={{ fontFamily: "var(--font-mono)", fontWeight: 700, fontSize: 13 }}>{window.matchScoreStr(m, tIpponsB, tIpponsA)}</div>;
        })()}
        {/* No centre "●" dot: a running match is signalled by the row's
            .tw-match--running highlight (accent border + ring). The labelled
            "● NOW" / "● {count} now" badges elsewhere are a separate affordance. */}
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

export function AdminSchedulePage({ tournament, onBack, onMoveCourt, onLogout, onViewerMode, password }) {
  const [picked, setPicked] = useStateA([]);
  const [dojoText, setDojoText] = useStateA("");
  const [compFilter, setCompFilter] = useStateA("all");
  const [matchDuration, setMatchDuration] = useStateA(3); // minutes per match estimate
  // Per-competition auto-schedule: startTime + duration
  const [autoComp, setAutoComp] = useStateA(tournament.competitions[0]?.id || "");
  const [autoStart, setAutoStart] = useStateA(tournament.competitions[0]?.startTime || "09:00");
  const [autoSaving, setAutoSaving] = useStateA(false);

  const [estOpen, setEstOpen] = useStateA(false);
  const [estMatchDuration, setEstMatchDuration] = useStateA(3);
  const [estMultiplier, setEstMultiplier] = useStateA(1.5);
  const [estCourts, setEstCourts] = useStateA(tournament.courts?.length || 1);
  const [estNumMatches, setEstNumMatches] = useStateA(0);
  const [estTeamSize, setEstTeamSize] = useStateA(tournament.competitions[0]?.teamSize || 0);
  const [estBoutsPerTeamMatch, setEstBoutsPerTeamMatch] = useStateA(0);
  const [estBuffer, setEstBuffer] = useStateA(0);
  const [estCeremony, setEstCeremony] = useStateA(0);
  const [estResult, setEstResult] = useStateA(null);
  const [estLoading, setEstLoading] = useStateA(false);

  useEffectA(() => {
    const newBouts = estTeamSize > 0 ? 2 * estTeamSize - 1 : 0;
    setEstBoutsPerTeamMatch(prev => prev === newBouts ? prev : newBouts);
  }, [estTeamSize]);

  useEffectA(() => {
    if (!estOpen) {
      setEstLoading(false);
      return;
    }
    // Guard: required params must be valid numbers > 0 to avoid 400s
    if (!Number.isFinite(estMatchDuration) || estMatchDuration <= 0 ||
        !Number.isFinite(estMultiplier) || estMultiplier <= 0 ||
        !Number.isFinite(estCourts) || estCourts <= 0 ||
        !Number.isFinite(estNumMatches) || estNumMatches <= 0) {
      setEstResult(null);
      setEstLoading(false);
      return;
    }

    const controller = new AbortController();
    const timer = setTimeout(async () => {
      setEstLoading(true);
      try {
        const res = await window.API.estimateSchedule({
          matchDuration: estMatchDuration,
          multiplier: estMultiplier,
          courts: estCourts,
          numMatches: estNumMatches,
          teamSize: estTeamSize,
          boutsPerTeamMatch: estBoutsPerTeamMatch,
          buffer: estBuffer,
          ceremonyMinutes: estCeremony
        }, password, controller.signal);
        setEstResult(res);
      } catch (e) {
        if (!controller.signal.aborted) {
          console.error("Estimation failed", e);
        }
      } finally {
        setEstLoading(false);
      }
    }, 300);
    return () => { clearTimeout(timer); controller.abort(); };
  }, [estOpen, estMatchDuration, estMultiplier, estCourts, estNumMatches, estTeamSize, estBoutsPerTeamMatch, estBuffer, estCeremony, password]);

  // T040/T041: read ?court= from the URL; useQuery re-renders on history
  // changes so navigating between /admin/schedule and /admin/schedule?court=A
  // toggles the filter without a full page reload. The window.AppRouter
  // fallback is a defence against the router not being registered yet
  // (e.g. during JSDOM tests that don't load preact-router).
  const useQueryFn = (window.AppRouter && window.AppRouter.useQuery) || (() => ({}));
  const query = useQueryFn();
  const courtFromURL = query.court;

  // Resolve the active court filter: URL ?court= wins; localStorage is the
  // fallback when the URL is bare. Whenever the URL carries a court we
  // persist it back to localStorage so a later bare-URL visit on the same
  // device restores the same shiaijo. Wrapped in try/catch: Safari private
  // mode disables localStorage entirely.
  const effectiveCourt = (() => {
    if (courtFromURL && courtFromURL.trim() !== "") {
      try { window.localStorage.setItem(COURT_STORAGE_KEY, courtFromURL); } catch (_) { /* ignore */ }
      return courtFromURL;
    }
    try { return window.localStorage.getItem(COURT_STORAGE_KEY) || ""; } catch (_) { return ""; }
  })();

  const allMatches = useMemoA(
    () => window.tournamentMatches(tournament).filter(hasBothSides),
    [tournament]
  );

  const estNumMatchesRef = useRefA(false);
  useEffectA(() => {
    if (!estNumMatchesRef.current && allMatches.length > 0) {
      setEstNumMatches(allMatches.length);
      estNumMatchesRef.current = true;
    }
  }, [allMatches.length]);

  const filtered = window.applyFilters(allMatches, picked, dojoText, compFilter);
  // T040 (US1, FR-001): apply the court scope AFTER the user-driven
  // player/dojo/competition filters but BEFORE the byCourt bucket split.
  // Doing it here means SSE patches for off-court matches naturally drop
  // out of the visible grid without any patch.jsx-side awareness.
  const courtFiltered = filterMatchesByCourt(filtered, effectiveCourt);

  // Guard against a nil courts slice: the JSON tag has no omitempty, so the
  // API can serialize `courts: null`. The rest of the codebase guards
  // `t.courts || []` (e.g. the score editor): match that to avoid a render
  // crash on courts.forEach/map/includes below.
  const courts = tournament.courts || [];
  const byCourt = {};
  courts.forEach((cc) => byCourt[cc] = []);
  // Matches whose court isn't in the configured list (missing, removed from
  // config, or stale) go into a separate "Unassigned" bucket so they stay
  // visible and movable instead of vanishing into an unrendered key.
  const unassigned = [];
  courtFiltered.forEach((m) => {
    if (m.court && byCourt[m.court]) byCourt[m.court].push(m);
    else unassigned.push(m);
  });
  const courtOrder = (a, b) => {
    const order = { running: 0, scheduled: 1, completed: 2 };
    // Unknown / new statuses sink to "completed" (=2): matches the
    // ScheduleViewer in viewer.jsx and the patch.jsx _orderByCourtKey
    // helper, so admin and viewer surfaces stay in sync if a new
    // terminal status (kiken / fusenpai / forfeit) ever appears.
    const ao = order[a.status] ?? 2;
    const bo = order[b.status] ?? 2;
    if (ao !== bo) return ao - bo;
    return (a.scheduledAt || "99:99").localeCompare(b.scheduledAt || "99:99");
  };
  Object.values(byCourt).forEach((list) => list.sort(courtOrder));
  unassigned.sort(courtOrder);

  // Pace stats must reflect the full per-court workload: the admin uses
  // the panel to rebalance scheduling, not to inspect filter results.
  // Build a separate by-court bucket from allMatches (still narrowed by
  // the explicit ?court= scope so the panel can be reviewed one court at
  // a time, but NOT by the ad-hoc player/dojo/competition filters).
  const paceByCourt = {};
  courts.forEach((cc) => paceByCourt[cc] = []);
  // Also bucket matches on courts not in the current config (stale/moved)
  // so the pace panel stays accurate even after court list changes.
  // Trim before bucketing: whitespace-only courts are treated as unassigned
  // by the rest of the UI and must not create phantom pace tiles.
  filterMatchesByCourt(allMatches, effectiveCourt).forEach((m) => {
    const court = (m.court || "").trim();
    if (court) {
      if (!paceByCourt[court]) paceByCourt[court] = [];
      paceByCourt[court].push(m);
    }
  });

  const matchHasFilter = (m) => window.matchHighlightedBy(m, picked, dojoText);
  const hasAnyFilter = picked.length > 0 || dojoText || compFilter !== "all";

  // Duration estimation: earliest start to latest finish across all matches.
  // clampMatchDuration coerces NaN / fractional / sub-1 values to the
  // 3-minute default before any arithmetic: see helper for the full
  // rationale (Copilot found that addMinutes("00:00", 2.5) → "00:2.5",
  // which then persists as an invalid scheduledAt string).
  const safeMatchDuration = clampMatchDuration(matchDuration);
  const scheduledWithTimes = allMatches.filter(m => m.scheduledAt);
  const firstTime = scheduledWithTimes.length > 0 ? scheduledWithTimes.reduce((mn, m) => !mn || m.scheduledAt < mn ? m.scheduledAt : mn, null) : null;
  const lastTime = scheduledWithTimes.length > 0 ? scheduledWithTimes.reduce((mx, m) => !mx || m.scheduledAt > mx ? m.scheduledAt : mx, null) : null;
  const durationEstimate = firstTime && lastTime ? (() => {
    const a = timeToMinutes(firstTime);
    const b = timeToMinutes(lastTime);
    if (a === null || b === null) return null;
    const diff = b - a + safeMatchDuration;
    return formatMinutes(diff);
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
  // shrink the set being scheduled: otherwise it's easy to time only a subset.
  const autoSchedule = async () => {
    const comp = tournament.competitions.find(c => c.id === autoComp);
    if (!comp) return;
    // Skip matches with no/unknown court: the per-court scheduler assumes
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
        // ?? not || so "00:00" (0 minutes: midnight) doesn't fall through to 09:00.
        let cursor = timeToMinutes(autoStart) ?? 540;
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
              <input
                className="input"
                type="number"
                min="1"
                max="60"
                step="1"
                /* Clearing a number input gives e.target.value === "", and
                   passing value={0} or value={NaN} to React produces either
                   a visible "0" (jarring) or a "Received NaN for the value"
                   warning. Round-trip NaN ↔ empty-string at the value
                   attribute boundary so the cleared display stays empty
                   while safeMatchDuration's Number.isFinite check (above)
                   keeps providing the 3-minute fallback for scheduling.
                   step="1" makes the browser's up/down arrows step in
                   whole minutes: typing fractions like "2.5" is still
                   physically possible, so safeMatchDuration also guards
                   Number.isInteger. Belt-and-braces. */
                value={Number.isFinite(matchDuration) ? matchDuration : ""}
                onChange={e => {
                  const raw = e.target.value;
                  setMatchDuration(raw === "" ? NaN : +raw);
                }}
                style={{ width: 80 }}
              />
            </div>
            <button type="button" className="btn btn--primary" onClick={autoSchedule} disabled={autoSaving} style={{ alignSelf: "flex-end" }}>
              {autoSaving ? "Scheduling…" : "Auto-schedule competition"}
            </button>
            {durationEstimate && (
              <div style={{ alignSelf: "flex-end", fontSize: 13, color: "var(--ink-3)", paddingBottom: 2 }}>
                Est. duration: <strong>{durationEstimate}</strong> · {scheduledWithTimes.length} of {allMatches.length} timed
              </div>
            )}
          </div>
        </div>

        <div className="card" style={{ marginBottom: 20 }}>
          <div
            className="card__title"
            style={{ display: "flex", justifyContent: "space-between", cursor: "pointer", marginBottom: estOpen ? 12 : 0 }}
            onClick={() => setEstOpen(!estOpen)}
          >
            <span>Schedule estimator {estLoading && <span style={{ fontSize: 12, fontWeight: 400, color: "var(--ink-4)", marginLeft: 8 }}>Recalculating...</span>}</span>
            <span style={{ fontSize: 18, fontWeight: 400 }}>{estOpen ? "−" : "+"}</span>
          </div>
          {estOpen && (
            <div className="est-form">
              <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(180px, 1fr))", gap: 16 }}>
                <EstInput label="Match duration (min)" value={estMatchDuration} setter={setEstMatchDuration} min="1" max="60" />
                <EstInput label="Multiplier" value={estMultiplier} setter={setEstMultiplier} min="1" max="3" step="0.1" />
                <EstInput label="Courts" value={estCourts} setter={setEstCourts} min="1" max="26" />
                <EstInput label="Matches" value={estNumMatches} setter={setEstNumMatches} min="1" />
                <EstInput label="Team size (0=indiv)" value={estTeamSize} setter={setEstTeamSize} min="0" />
                <EstInput label="Bouts per team match" value={estBoutsPerTeamMatch} setter={setEstBoutsPerTeamMatch} min="0" />
                <EstInput label="Buffer %" value={estBuffer} setter={setEstBuffer} min="0" max="100" />
                <EstInput label="Ceremony (min)" value={estCeremony} setter={setEstCeremony} min="0" />
              </div>

              {estResult && (
                <div style={{ marginTop: 20, paddingTop: 20, borderTop: "1px solid var(--bg-3)" }}>
                  <div style={{ display: "flex", flexWrap: "wrap", alignItems: "baseline", gap: 12 }}>
                    <div style={{ fontSize: 24, fontWeight: 700 }}>Total: {formatMinutes(estResult.totalDurationMinutes)}</div>
                    {autoStart && (
                      <div style={{ fontSize: 16, color: "var(--ink-2)" }}>
                        Projected finish: <strong>{window.addMinutes(autoStart, estResult.totalDurationMinutes)}</strong>
                      </div>
                    )}
                  </div>
                  {estResult.ceremonyMinutes > 0 && (
                    <div style={{ fontSize: 13, color: "var(--ink-3)", marginTop: 4 }}>Includes {estResult.ceremonyMinutes}m ceremony</div>
                  )}
                  <PerCourtBreakdown perCourtMinutes={estResult.perCourtMinutes} />
                </div>
              )}
            </div>
          )}
        </div>

        <CourtPacePanel byCourt={paceByCourt} safeMatchDuration={safeMatchDuration} />

        <div className="tw-sched">
          <div className="tw-sched__filters" data-testid="admin-schedule-court-filter">
            <window.PlayerMultiFilter tournament={tournament} picked={picked} setPicked={setPicked} dojoText={dojoText} setDojoText={setDojoText} />
            <select className="input" style={{ width: "auto", minWidth: 200 }} value={compFilter} onChange={(e) => setCompFilter(e.target.value)}>
              <option value="all">All competitions</option>
              {tournament.competitions.map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
            </select>
            {hasAnyFilter && (
              <button type="button" className="btn btn--ghost btn--sm" onClick={() => { setPicked([]); setDojoText(""); setCompFilter("all"); }}>Clear</button>
            )}
            {/* T042 (US1, FR-001/FR-002): the court-scope badge mirrors the
                operator's active filter. The "Show all courts" link clears
                both the URL ?court= param AND the localStorage fallback so
                a bookmark-driven tablet doesn't immediately re-scope on
                next render. We navigate via AppRouter.route so preact-router
                fires its history listeners (useQuery re-renders). */}
            {effectiveCourt && (
              <span className="bc-court-badge" style={{ display: "inline-flex", alignItems: "center", gap: 8, padding: "4px 10px", borderRadius: 14, background: "var(--bg-2, #eef2f7)", fontSize: 12, fontWeight: 600 }}>
                Showing {window.Term ? React.createElement(window.Term, { name: "shiaijo" }, "Shiaijo") : "Shiaijo"} {effectiveCourt}
                <button type="button"
                  className="btn btn--ghost btn--sm"
                  style={{ padding: "0 6px", fontSize: 11, fontWeight: 500 }}
                  onClick={() => {
                    try { window.localStorage.removeItem(COURT_STORAGE_KEY); } catch (_) { /* ignore */ }
                    const routeFn = (window.AppRouter && window.AppRouter.route) || ((url) => { window.history.pushState(null, "", url); window.dispatchEvent(new PopStateEvent("popstate")); });
                    routeFn("/admin/schedule");
                  }}
                >
                  Show all courts
                </button>
              </span>
            )}
            <span style={{ marginLeft: "auto", fontSize: 12, color: "var(--ink-3)" }}>{courtFiltered.length} of {allMatches.length} matches</span>
          </div>

          <div className="tw-courts" data-testid="admin-schedule-list">
            {courts.map((cc) => {
              const list = byCourt[cc] || [];
              const runningOn = list.find((m) => m.status === "running");
              return (
                <div key={cc} className="tw-court">
                  <div className="tw-court__head">
                    <div>
                      <div className="tw-court__title">SHIAIJO {cc}</div>
                      <div className="tw-court__sub">{list.length} match{list.length === 1 ? "" : "es"}</div>
                    </div>
                    {runningOn && <span className="bc-running">● NOW</span>}
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
