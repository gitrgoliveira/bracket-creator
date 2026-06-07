// Tournament-wide schedule, score editor, and export pages. See
// web-mobile/admin_split_plan.md.

const { useState: useStateA, useMemo: useMemoA, useEffect: useEffectA, useRef: useRefA } = React;

function formatMinutes(m) {
  return `${Math.floor(m / 60)}h ${m % 60}m`;
}

const pluralize = window.pluralize;
const AdminTopbar = window.AdminTopbar;
const Breadcrumbs = window.Breadcrumbs;
const CourtPicker = window.CourtPicker;
const ScoreEditorModal = window.ScoreEditorModal;
// `hasBothSides` rejects matches with bye/TBD placeholder sides — see
// admin_helpers.jsx. Required because normalizeMatch substitutes
// {id:"",name:""} for missing sides, making the naive `m.sideA && m.sideB`
// check always pass.
const hasBothSides = window.hasBothSides;
const getScoreBtnClass = window.getScoreBtnClass;

// ---------- Tournament-wide schedule (admin) ----------
// Estimate minutes from HH:MM string; returns null if invalid
function timeToMinutes(t) {
  if (!t) return null;
  const [h, m] = t.split(":").map(Number);
  if (isNaN(h) || isNaN(m)) return null;
  return h * 60 + m;
}

// True when the user's time edit (newVal, a "HH:MM" string from the
// time input) is a real change relative to the stored scheduledAt
// (which is null for untimed matches, "HH:MM" string otherwise). The
// AdminTWMatch.useState initializer normalizes scheduledAt-or-null to
// "" for the input's value attribute, so a naive `newVal !==
// oldScheduledAt` check would treat the no-op open-and-blur case ("" vs
// null) as a change and fire an unnecessary PUT + SSE broadcast.
// Normalize both sides the same way the initializer does.
function timeEdited(oldScheduledAt, newVal) {
  return (oldScheduledAt || "") !== newVal;
}

// filterMatchesByCourt(matches, courtParam) — pure list filter.
//
// FR-001 / T040 (US1, SC-001): bookmark `/admin/schedule?court=A` to scope
// an operator's view to a single shiaijo. Returns matches unchanged when no
// filter is set ("", null, undefined, or "all"); otherwise returns only
// matches whose `m.court` exactly equals courtParam (case-sensitive — the
// app's canonical court labels are uppercase A–Z per the Excel layout).
// Pure and DOM-free so the helper is unit-testable from jsdom without
// mounting AdminSchedulePage.
export function filterMatchesByCourt(matches, courtParam) {
  const c = (courtParam || "").trim();
  if (c === "" || c === "all") return matches;
  return matches.filter((m) => m.court === c);
}

// Coerces the matchDuration form value to a safe integer minutes count
// for arithmetic in durationEstimate (rendered as "HH h MM m") and the
// auto-schedule loop (`cursor += safeMatchDuration` + addMinutes).
//
// Rejects:
//   - NaN / undefined / null            (cleared input → stored as NaN)
//   - Infinity / -Infinity              (impossible via UI but defensive)
//   - non-integers like 2.5             (Copilot found: addMinutes would
//                                        produce "00:2.5" — invalid HH:MM —
//                                        and durationEstimate "0h 32.5m")
//   - values < 1                        (zero or negative makes no sense)
//
// Falls back to 3 minutes — the same default the matchDuration state
// uses, so the UX is "if your typed value is invalid, we schedule as if
// you'd left the field at 3 (the placeholder default)."
function clampMatchDuration(raw, fallback = 3) {
  return Number.isFinite(raw) && Number.isInteger(raw) && raw >= 1 ? raw : fallback;
}

// True when the list is non-empty and every match is in 'completed' status.
// Drives the "All matches scored" banner in AdminScoreEditor.
function allMatchesCompleted(matches) {
  return matches.length > 0 && matches.every(m => m.status === "completed");
}

// T041 (US1, FR-002, SC-002): per-tablet localStorage key. The URL
// ?court= param remains canonical — localStorage is a fallback that lets
// a bookmarked operator tablet land on the same shiaijo after they
// navigate away and return via a bare URL.
const COURT_STORAGE_KEY = "bc_operator_courts";

function AdminSchedulePage({ tournament, onBack, onMoveCourt, onLogout, onViewerMode, password }) {
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
  // device restores the same shiaijo. Wrapped in try/catch — Safari private
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

  const courts = tournament.courts;
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
    // Unknown / new statuses sink to "completed" (=2) — matches the
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

  // Pace stats must reflect the full per-court workload — the admin uses
  // the panel to rebalance scheduling, not to inspect filter results.
  // Build a separate by-court bucket from allMatches (still narrowed by
  // the explicit ?court= scope so the panel can be reviewed one court at
  // a time, but NOT by the ad-hoc player/dojo/competition filters).
  const paceByCourt = {};
  courts.forEach((cc) => paceByCourt[cc] = []);
  // Also bucket matches on courts not in the current config (stale/moved)
  // so the pace panel stays accurate even after court list changes.
  // Trim before bucketing — whitespace-only courts are treated as unassigned
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
  // 3-minute default before any arithmetic — see helper for the full
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
        // ?? not || so "00:00" (0 minutes — midnight) doesn't fall through to 09:00.
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
                   whole minutes — typing fractions like "2.5" is still
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
              <button className="btn btn--ghost btn--sm" onClick={() => { setPicked([]); setDojoText(""); setCompFilter("all"); }}>Clear</button>
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
                <button
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
    if (onTimeChange && timeEdited(m.scheduledAt, timeVal)) onTimeChange(timeVal);
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
          <button
            className="tw-match__time tw-match__time--editable"
            onClick={(e) => { e.stopPropagation(); if (onTimeChange) { setTimeVal(m.scheduledAt || ""); setEditingTime(true); } }}
            title="Click to set time"
          >
            {m.scheduledAt || <span style={{ color: "var(--ink-4)", fontWeight: 400 }}>—</span>}
            {onTimeChange && <span style={{ fontSize: 9, color: "var(--ink-4)", marginLeft: 2 }}>✎</span>}
          </button>
        )}
        <div className="tw-match__phase">
          {m.phase === "pool" ? m.poolName : m.round}
          {m.round != null && typeof m.round === "number" && m.round >= 0 && (
            <span class="tw-match__round">R{m.round + 1}</span>
          )}
        </div>
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
      {/* T097: formatIpponsScore appends "Kiken / Fus. / DH / (E)" suffixes
          for non-fought decisions and overtime. The match-level decision
          covers kiken / fusenpai / daihyosen here; per-bout fusensho is a
          SubMatchResult and is rendered inside the score modal — the row
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
          return <div style={{ fontFamily: "var(--font-mono)", fontWeight: 700, fontSize: 13 }}>{window.formatIpponsScore(tIpponsB, tIpponsA, m.score, m.decision, m.encho, m.decidedByHantei)}</div>;
        })()}
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

// ---------- Per-match lineup panel (mp-bkg) ----------

// pickCopySource — pure helper that selects the most recent saved lineup
// among this team's *earlier* matches ("Copy from previous match").
// Exported for unit testing.
// Candidate filter: this team's matches, not the current match, with a saved
// lineup, scheduled at or before the current match's time (when it has one).
// Sort order: scheduledAt DESC (nulls last — unscheduled matches treated as
// least-recent), then court ASC, then queue-position (index in allMatches)
// ASC, then matchId DESC.
function pickCopySource(allMatches, currentMatchId, teamId, savedLineups) {
  // savedLineups is a map of matchId → lineup (non-null only when a lineup
  // has been saved). Candidate = match for this team, not the current match,
  // with a saved lineup.
  //
  // teamId may be a single key or an array of keys ([id, name]). A match
  // side may be keyed by the team NAME (api_serializers name-as-id fallback)
  // while the team's real id is a UUID, so we match a side against ANY of
  // the provided keys by either its id OR its name — comparing only one key
  // space would silently find zero candidates (the original copy-from-
  // previous bug).
  const keys = (Array.isArray(teamId) ? teamId : [teamId]).filter(Boolean);
  const sideMatches = (side) => {
    if (side == null) return false;
    const sid = typeof side === "object" ? (side.id ?? side.ID) : side;
    const sname = typeof side === "object" ? (side.name ?? side.Name) : side;
    return keys.includes(sid) || keys.includes(sname);
  };
  // "Previous match": only consider siblings scheduled at or before the
  // current match. When the current match has no time, don't restrict (any
  // saved sibling is a valid source). An unscheduled sibling (no time) is
  // always allowed — it sorts last anyway.
  const current = allMatches.find(m => m.id === currentMatchId);
  const currentTime = current && current.scheduledAt ? current.scheduledAt : "";
  const candidates = allMatches.filter(m => {
    if (m.id === currentMatchId) return false;
    if (!savedLineups[m.id]) return false;
    if (!sideMatches(m.sideA) && !sideMatches(m.sideB)) return false;
    if (currentTime && m.scheduledAt && m.scheduledAt > currentTime) return false;
    return true;
  });
  if (candidates.length === 0) return null;
  candidates.sort((a, b) => {
    // scheduledAt DESC; null/missing treated as "" so they sort after any real
    // time string in a DESC comparison (unscheduled = least recent).
    const aT = a.scheduledAt || "";
    const bT = b.scheduledAt || "";
    if (aT !== bT) return bT.localeCompare(aT);
    // court ASC
    const aC = a.court || "";
    const bC = b.court || "";
    if (aC !== bC) return aC.localeCompare(bC);
    // queue/sequence: original index in allMatches (lower = earlier)
    const aIdx = allMatches.indexOf(a);
    const bIdx = allMatches.indexOf(b);
    if (aIdx !== bIdx) return aIdx - bIdx;
    // matchId DESC: a defensive final tiebreak. In practice distinct match
    // objects always have distinct indices above, so this is effectively
    // unreachable — kept only so the comparator is total.
    return (b.id || "").localeCompare(a.id || "");
  });
  return candidates[0];
}

// MatchLineupSideEditor — inline lineup editor for one team side within
// the per-match lineup panel. Handles load/save/copy-from-previous for a
// single (compId, teamId, matchId) triple.
// Reuses admin_lineup.jsx's exported helpers (positionsForSize, rosterFor,
// teamIdOf) so there is no duplication of position-label / roster logic.
// The helpers are read lazily on each render so module evaluation order
// does not matter (safe in test/bundler contexts too).
function MatchLineupSideEditor({ comp, team, match, allMatches, password, showToast }) {
  const teamSize = comp?.teamSize || 5;
  const { positionsForSize: lineupPositionsForSize, rosterFor: lineupRosterFor, teamIdOf: lineupTeamIdOf } = window.AdminLineupHelpers || {};
  const positions = (typeof lineupPositionsForSize === "function")
    ? lineupPositionsForSize(teamSize)
    : [];
  const roster = (typeof lineupRosterFor === "function")
    ? lineupRosterFor(team)
    : [];
  const teamId = (typeof lineupTeamIdOf === "function")
    ? lineupTeamIdOf(team)
    : (team?.id || team?.name || "");
  const compId = comp?.id || "";
  const matchId = match?.id || "";

  // A match "involves" this team when either side resolves to it by id OR by
  // name. Match sides may be keyed by team NAME (api_serializers name-as-id
  // fallback) while teamId is the participant UUID, so we compare against
  // both keys — the same id-vs-name pitfall the roster resolver hits.
  const teamKeys = [team?.id, team?.ID, team?.name, team?.Name].filter(Boolean);
  const sideMatchesTeam = (side) => {
    if (side == null) return false;
    const sid = typeof side === "object" ? (side.id ?? side.ID) : side;
    const sname = typeof side === "object" ? (side.name ?? side.Name) : side;
    return teamKeys.includes(sid) || teamKeys.includes(sname);
  };
  const matchInvolvesTeam = (mm) => sideMatchesTeam(mm.sideA) || sideMatchesTeam(mm.sideB);

  const [values, setValues] = useStateA(() => {
    const init = {};
    positions.forEach(p => { init[p.key] = ""; });
    return init;
  });
  const [lockedAt, setLockedAt] = useStateA(null);
  const [loading, setLoading] = useStateA(true);
  const [saving, setSaving] = useStateA(false);
  const [copying, setCopying] = useStateA(false);
  const [error, setError] = useStateA("");
  // Track whether the current match's lineup was loaded from a per-match
  // entry (true) or is inheriting the round default (false).
  const [isMatchOverride, setIsMatchOverride] = useStateA(false);

  // Load per-match lineup on mount; record whether it was a real hit.
  useEffectA(() => {
    let cancelled = false;
    if (!compId || !teamId || !matchId) {
      setLoading(false);
      return;
    }
    (async () => {
      try {
        const matchLineup = await window.API.fetchMatchLineup(compId, teamId, matchId);
        if (cancelled) return;
        if (matchLineup) {
          const next = {};
          positions.forEach(p => {
            next[p.key] = (matchLineup.positions || {})[p.key] || "";
          });
          setValues(next);
          setLockedAt(matchLineup.lockedAt || null);
          setIsMatchOverride(true);
        } else {
          // No per-match entry — reflect the round default (fetch-and-show,
          // but do NOT set isMatchOverride so the label says "inheriting").
          let round = 0;
          if (typeof match.round === "string") {
            const mr = /^Round\s+(\d+)$/.exec(match.round);
            if (mr) round = parseInt(mr[1], 10) - 1;
          } else if (typeof match.round === "number") {
            round = match.round;
          }
          try {
            const roundLineup = await window.API.fetchTeamLineup(compId, teamId, round);
            if (cancelled) return;
            if (roundLineup) {
              const next = {};
              positions.forEach(p => {
                next[p.key] = (roundLineup.positions || {})[p.key] || "";
              });
              setValues(next);
            }
          } catch (_e) { /* no round lineup — leave blank */ }
          setIsMatchOverride(false);
        }
      } catch (e) {
        if (!cancelled) setError(e?.message || "Failed to load lineup");
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => { cancelled = true; };
  }, [compId, teamId, matchId]);

  // Locked when this side's lineup carries a lockedAt OR the match itself is
  // already live/finished — the backend locks the whole match once it starts
  // (LockTeamLineupForMatch), so a side with no saved lineup yet must also
  // read as locked rather than show an editable form that 409s on save.
  const matchStarted = match?.status === "running" || match?.status === "completed";
  const isLocked = !!lockedAt || matchStarted;

  const save = async () => {
    setError("");
    setSaving(true);
    try {
      const positionsOut = {};
      Object.entries(values).forEach(([k, v]) => {
        if (v) positionsOut[k] = v;
      });
      const updated = await window.API.putMatchLineup(compId, teamId, matchId, positionsOut, password);
      setLockedAt(updated.lockedAt || null);
      setIsMatchOverride(true);
      if (typeof showToast === "function") showToast("Match lineup saved");
    } catch (e) {
      const msg = e?.message || "Failed to save match lineup";
      if (/ErrLineupLocked|lineup.*locked|locked/i.test(msg)) {
        setError("This match is live — lineup is locked and cannot be changed.");
      } else {
        setError(msg);
      }
    } finally {
      setSaving(false);
    }
  };

  // Copy from previous: probe sibling lineups on demand (deferred until click
  // to avoid N fire-and-forget GETs on every panel mount). Fetches all
  // siblings in parallel, picks the best candidate via pickCopySource, and
  // copies its positions into the current match.
  const hasSiblings = !!(allMatches && allMatches.some(m => m.id !== matchId && matchInvolvesTeam(m)));
  const copyFromPrevious = async () => {
    if (!compId || !teamId || !allMatches) return;
    setError("");
    setCopying(true);
    try {
      // Fetch all sibling lineups in parallel to find the best candidate.
      const siblings = allMatches.filter(m => m.id !== matchId && matchInvolvesTeam(m));
      const results = await Promise.all(
        siblings.map(s =>
          window.API.fetchMatchLineup(compId, teamId, s.id)
            .then(l => ({ matchId: s.id, lineup: l }))
            .catch(() => ({ matchId: s.id, lineup: null }))
        )
      );
      const savedLineupsByMatch = {};
      results.forEach(({ matchId: mid, lineup }) => {
        if (lineup) savedLineupsByMatch[mid] = lineup;
      });
      const copySource = pickCopySource(allMatches, matchId, teamKeys, savedLineupsByMatch);
      if (!copySource) {
        setError("No previous match lineup found to copy.");
        return;
      }
      const sourceLineup = savedLineupsByMatch[copySource.id];
      if (!sourceLineup) return;
      const positionsOut = {};
      positions.forEach(p => {
        const v = (sourceLineup.positions || {})[p.key] || "";
        if (v) positionsOut[p.key] = v;
      });
      const updated = await window.API.putMatchLineup(compId, teamId, matchId, positionsOut, password);
      // Apply cloned values into local state
      const next = {};
      positions.forEach(p => {
        next[p.key] = (updated.positions || {})[p.key] || "";
      });
      setValues(next);
      setLockedAt(updated.lockedAt || null);
      setIsMatchOverride(true);
      if (typeof showToast === "function") showToast("Lineup copied from previous match");
    } catch (e) {
      const msg = e?.message || "Failed to copy lineup";
      if (/ErrLineupLocked|lineup.*locked|locked/i.test(msg)) {
        setError("This match is live — lineup is locked and cannot be changed.");
      } else {
        setError(msg);
      }
    } finally {
      setCopying(false);
    }
  };

  if (loading) {
    return <div style={{ padding: "8px 0", color: "var(--ink-3)", fontSize: 12 }}>Loading lineup…</div>;
  }

  const teamName = team?.name || team?.Name || "Team";

  return (
    <div data-testid={`match-lineup-side-${teamId}`}>
      <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 8 }}>
        <span style={{ fontWeight: 700, fontSize: 13 }}>{teamName}</span>
        {isMatchOverride
          ? <span style={{ fontSize: 11, color: "var(--accent, #1d73d5)", fontWeight: 600 }}>Override for this match</span>
          : <span style={{ fontSize: 11, color: "var(--ink-3)" }}>Inheriting round default</span>
        }
        {isLocked && (
          <span style={{ fontSize: 11, fontWeight: 700, color: "var(--ink-3)", background: "var(--bg-2, #fafafa)", border: "1px solid var(--line, #ddd)", padding: "1px 6px", borderRadius: 3 }}>
            🔒 Locked
          </span>
        )}
      </div>

      {error && (
        <div style={{ color: "var(--danger, #c00)", fontSize: 12, marginBottom: 8, padding: "6px 8px", border: "1px solid var(--danger, #c00)", borderRadius: 4, background: "rgba(204,0,0,0.05)" }}>
          {error}
        </div>
      )}

      <div style={{ display: "flex", flexDirection: "column", gap: 8, marginBottom: 12 }}>
        {positions.map(p => (
          <label key={p.key} style={{ display: "flex", alignItems: "center", gap: 8, fontSize: 13 }}>
            <span style={{ minWidth: 72, fontWeight: 600, color: "var(--ink-2)", fontSize: 12 }}>{p.label}</span>
            <select
              data-testid={`match-lineup-pos-${teamId}-${p.key}`}
              className="input"
              value={values[p.key] || ""}
              disabled={isLocked || saving || copying}
              onChange={(e) => setValues(v => ({ ...v, [p.key]: e.target.value }))}
              style={{ flex: 1, padding: "4px 6px", fontSize: 13 }}
            >
              <option value="">— Select —</option>
              {roster.map(member => (
                <option key={member} value={member}>{member}</option>
              ))}
            </select>
          </label>
        ))}
        {roster.length === 0 && (
          <div style={{ fontSize: 12, color: "var(--ink-3)", fontStyle: "italic" }}>
            No roster found. Add member names as metadata in the participant CSV.
          </div>
        )}
      </div>

      <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
        {!isLocked && (
          <button
            className="btn btn--primary btn--sm"
            onClick={save}
            disabled={saving || copying || roster.length === 0}
          >
            {saving ? "Saving…" : "Save lineup"}
          </button>
        )}
        <button
          className="btn btn--sm"
          onClick={copyFromPrevious}
          disabled={!hasSiblings || copying || saving || isLocked}
          title={hasSiblings
            ? "Find and copy the lineup from the most recent previous match"
            : "No other matches for this team"}
        >
          {copying ? "Copying…" : "Copy from previous match"}
        </button>
      </div>
    </div>
  );
}

// MatchLineupPanel — modal overlay for per-match lineup editing. Renders
// one MatchLineupSideEditor per team side (sideA / sideB). Only shown for
// team competitions (compKind === "team" || teamSize > 0).
function MatchLineupPanel({ match, tournament, password, showToast, onClose }) {
  const m = match;
  // Find the competition this match belongs to so we can access teamSize,
  // players (roster), etc.
  const comp = (tournament?.competitions || []).find(cc => cc.id === m.compId) || null;
  const isTeamComp = comp && (comp.kind === "team" || (comp.teamSize || 0) > 0);

  if (!isTeamComp) return null;

  // Resolve team objects from the competition's player list. comp.players
  // (loaded via /api/viewer/competitions) already carries each team's
  // metadata (member roster) — no extra participants fetch is needed.
  //
  // The match's sideA/sideB are normalized to { id, name }, where `id`
  // falls back to the team NAME when the backend has no UUID for that slot
  // (see api_serializers.normalizeMatch). A real participant's id is a
  // UUID, so the side key may be a name while the participant key is a
  // UUID (or vice-versa). We must therefore match on EITHER id OR name:
  // the previous `(p.id || p.name) === sideId` form compared only the
  // first truthy key (the UUID), which never equals a name-keyed sideId,
  // so the roster silently failed to resolve and every dropdown showed
  // "No roster found".
  const sideKey = (side) =>
    (side && typeof side === "object" ? (side.id || side.name) : side) || "";
  const sideAKey = sideKey(m.sideA);
  const sideBKey = sideKey(m.sideB);
  const players = comp.players || [];
  const matchesKey = (p, key) =>
    !!key && (p.id === key || p.ID === key || p.name === key || p.Name === key);
  const teamA = players.find(p => matchesKey(p, sideAKey)) || (m.sideA && typeof m.sideA === "object" ? m.sideA : null);
  const teamB = players.find(p => matchesKey(p, sideBKey)) || (m.sideB && typeof m.sideB === "object" ? m.sideB : null);

  // All matches for this competition (needed for "Copy from previous" candidate search).
  const allMatches = typeof window.compMatches === "function" ? window.compMatches(comp) : [];

  return (
    <div style={{
      position: "fixed", inset: 0, background: "rgba(0,0,0,0.35)",
      display: "flex", alignItems: "center", justifyContent: "center",
      zIndex: 1000, padding: 16
    }}>
      <div style={{
        background: "var(--bg, #fff)", borderRadius: 8,
        boxShadow: "0 8px 32px rgba(0,0,0,0.18)", padding: 24,
        width: "100%", maxWidth: 680, maxHeight: "90vh", overflowY: "auto"
      }}>
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "flex-start", marginBottom: 16 }}>
          <div>
            <div style={{ fontSize: 11, color: "var(--ink-3)", textTransform: "uppercase", letterSpacing: "0.1em", fontWeight: 700 }}>
              {comp?.name} · {m.scheduledAt || m.round || ""}
            </div>
            <h2 style={{ margin: "4px 0 0", fontSize: 20, fontWeight: 700 }}>Lineup for this match</h2>
            <div style={{ fontSize: 12, color: "var(--ink-3)", marginTop: 2 }}>
              Set per-match lineups below. Changes take effect when you save; the round-default lineup is used as a fallback until then.
            </div>
          </div>
          <button className="btn btn--ghost btn--sm" onClick={onClose}>✕ Close</button>
        </div>

        <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 24 }}>
          <div style={{ borderRight: "1px solid var(--line, #e5e7eb)", paddingRight: 20 }}>
            <div style={{ fontSize: 11, fontWeight: 700, textTransform: "uppercase", color: "var(--ink-3)", letterSpacing: "0.08em", marginBottom: 8 }}>
              SHIRO (white)
            </div>
            {teamB ? (
              <MatchLineupSideEditor
                key={`${m.id}-side-b`}
                comp={comp}
                team={teamB}
                match={m}
                allMatches={allMatches}
                password={password}
                showToast={showToast}
              />
            ) : (
              <div style={{ color: "var(--ink-3)", fontSize: 12, fontStyle: "italic" }}>Team not found in roster.</div>
            )}
          </div>
          <div>
            <div style={{ fontSize: 11, fontWeight: 700, textTransform: "uppercase", color: "var(--ink-3)", letterSpacing: "0.08em", marginBottom: 8 }}>
              AKA (red)
            </div>
            {teamA ? (
              <MatchLineupSideEditor
                key={`${m.id}-side-a`}
                comp={comp}
                team={teamA}
                match={m}
                allMatches={allMatches}
                password={password}
                showToast={showToast}
              />
            ) : (
              <div style={{ color: "var(--ink-3)", fontSize: 12, fontStyle: "italic" }}>Team not found in roster.</div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

// ---------- Score editor ----------
function AdminScoreEditorPage({ tournament, onBack, onEditScore, onMoveCourt, onLogout, onViewerMode, password }) {
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
        <AdminScoreEditor t={tournament} onEditScore={onEditScore} onMoveCourt={onMoveCourt} password={password} />
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

function AdminScoreEditor({ t, c, onEditScore, onMoveCourt, restrictToCompId, password, showToast }) {
  const [filter, setFilter] = useStateA("");
  const [compFilter, setCompFilter] = useStateA(restrictToCompId || "all");
  const [statusFilter, setStatusFilter] = useStateA("all");
  const [openMatch, setOpenMatch] = useStateA(null);
  // mp-bkg: per-match lineup panel state. lineupMatch holds the match
  // currently open in the lineup panel (null = panel closed).
  const [lineupMatch, setLineupMatch] = useStateA(null);
  // ScoreEditorModal's onSubmit / onSubmitAndNext callbacks await
  // onEditScore (which routes through AdminApp.editMatchScore — a
  // server PUT). If AdminScoreEditor unmounts during the in-flight
  // save (parent navigates away), the post-await setOpenMatch fires
  // on a torn-down component. Gate via mountedRef.
  const mountedRef = useRefA(true);
  useEffectA(() => () => { mountedRef.current = false; }, []);

  const tournament = t || (c ? { competitions: [c] } : { competitions: [] });
  const allMatches = useMemoA(
    () => tournament.competitions.flatMap((cc) => window.compMatches(cc)).filter(hasBothSides),
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
          aria-label="Filter matches by player, team, or dojo"
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
        {/* "All matches scored" banner. Guarded against statusFilter === "complete"
            because the filter trivially makes filtered all-completed, which would
            misleadingly fire the banner. The wording is intentionally generic — this
            view spans all match phases (pool + bracket) and all competition formats
            (pools/mixed/playoffs/league/swiss), so we don't claim "Pool play is
            complete" or point at a specific next tab. */}
        {statusFilter !== "complete" && allMatchesCompleted(filtered) && (
          <div className="alert alert--success" style={{ marginBottom: 12 }}>
            <div style={{ fontWeight: 600, marginBottom: 4 }}>All matches scored</div>
            <div style={{ fontSize: 13, color: "var(--ink-2)" }}>Every visible match is complete. Open the competition to review standings, generate playoffs, or start the next phase.</div>
          </div>
        )}
        {filtered.map((m) => {
          const aWin = m.winner && m.sideA && m.winner.id === m.sideA.id;
          const bWin = m.winner && m.sideB && m.winner.id === m.sideB.id;
          const isCorrection = m.status === "completed" && m.score?.corrected;
          // Bracket matches carry scoreA/scoreB strings rather than ipponsA/B
          // arrays (see normalizeMatch). Apply the same fallback used in VSchedItem.
          // Use ipponsFromScore so the trailing "(HN)" hansoku suffix from
          // Go's formatScore doesn't get split into bogus ippon letters.
          const seIpponsA = m.ipponsA || window.ipponsFromScore(m.scoreA);
          const seIpponsB = m.ipponsB || window.ipponsFromScore(m.scoreB);
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
                    {m.status === "completed" && window.formatIpponsScore(seIpponsB, seIpponsA, m.score, m.decision, m.encho, m.decidedByHantei)}
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
              <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
                <button className={getScoreBtnClass(m.status)} onClick={() => setOpenMatch(m)}>
                  {m.status === "completed" ? "Correct" : "Score"}
                </button>
                {/* mp-bkg: show Lineup button only for team competitions */}
                {(m.compKind === "team" || (m.teamSize || 0) > 0) && (
                  <button
                    className="btn btn--ghost btn--sm"
                    style={{ fontSize: 11 }}
                    onClick={() => setLineupMatch(m)}
                  >
                    Lineup
                  </button>
                )}
              </div>
            </div>
          );
        })}
      </div>

      {/* mp-bkg: per-match lineup panel */}
      {lineupMatch && (
        <MatchLineupPanel
          key={lineupMatch.compId + '-' + lineupMatch.id}
          match={lineupMatch}
          tournament={tournament}
          password={password}
          showToast={showToast}
          onClose={() => setLineupMatch(null)}
        />
      )}

      {openMatch && (() => {
        // Chained nav (Prev/Next/Finish+Start Next/←/→) must stay on the same
        // shiaijo. Operators run matches per-court; jumping courts mid-flow
        // skips the wrong matches. Unassigned matches scope to other
        // unassigned matches so the behaviour is consistent.
        const openCourt = openMatch.court || "";
        const sameCourt = filtered.filter(m => (m.court || "") === openCourt);
        const openIdx = sameCourt.findIndex(m => `${m.compId}:${m.id}` === `${openMatch.compId}:${openMatch.id}`);
        const prevMatch = openIdx > 0 ? sameCourt[openIdx - 1] : null;
        const nextMatch = openIdx >= 0 && openIdx < sameCourt.length - 1 ? sameCourt[openIdx + 1] : null;
        // Finish+Start Next must only advance to a non-completed match. Without
        // this guard, the last scheduled match has nextMatch = the first completed
        // match (completed matches sort after scheduled in the list), causing the
        // modal to loop back to match 1 after the final save.
        // Guard on openIdx >= 0: when openMatch is not found in sameCourt (openIdx
        // === -1), slice(0) would scan the whole array and return a spurious match.
        const nextActiveMatch = openIdx >= 0
          ? sameCourt.slice(openIdx + 1).find(m => m.status !== 'completed') || null
          : null;
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
                if (mountedRef.current) setOpenMatch(null);
              } catch (_err) {
                // Error handled by onEditScore/toast, but we catch here to keep modal open
              }
            }}
            onSubmitAndNext={nextActiveMatch ? async (patch) => {
              try {
                await onEditScore(openMatch.compId, openMatch.id, patch, openMatch);
                if (mountedRef.current) setOpenMatch(nextActiveMatch);
              } catch (_err) { /* keep modal open on error */ }
            } : null}
            password={password}
          />
        );
      })()}
    </div>
  );
}

function AdminExport({ c, t, password }) {
  const url = `${(window.linkBase || (() => window.location.origin))(t)}/viewer.html?id=${t.id}#comp-${c.id}`;

  const downloadXlsx = async () => {
    try {
      // /api/* is behind AuthMiddleware; without the password header
      // this returns 401 and the download silently fails.
      const resp = await fetch(`/api/competitions/${c.id}/export`, {
        headers: password ? { "X-Tournament-Password": password } : {},
      });
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

function PerCourtBreakdown({ perCourtMinutes }) {
  if (!perCourtMinutes || perCourtMinutes.length === 0) return null;
  return (
    <div className="est-breakdown" style={{ marginTop: 12 }}>
      <div style={{ fontSize: 12, fontWeight: 600, marginBottom: 4, color: "var(--ink-2)" }}>Per-court breakdown:</div>
      <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(120px, 1fr))", gap: 8 }}>
        {perCourtMinutes.map((m, i) => (
          <div key={i} style={{ fontSize: 12, padding: "4px 8px", background: "var(--bg-2)", borderRadius: 4, border: "1px solid var(--bg-3)" }}>
            <span style={{ color: "var(--ink-3)" }}>Court {i < 26 ? String.fromCharCode(65 + i) : i + 1}:</span>
            <strong style={{ marginLeft: 4 }}>{formatMinutes(m)}</strong>
          </div>
        ))}
      </div>
    </div>
  );
}

// computeCourtPaceStats(byCourt, perMatchMinutes, nowMinutes) — deterministic
// when nowMinutes is supplied; non-deterministic (reads wall-clock via
// `new Date()`) when omitted.
//
// For each court, derive:
//   court               — the court label (e.g. "A")
//   completedCount      — matches that are neither scheduled nor running (i.e. the slot is consumed)
//   remainingCount      — matches NOT yet completed
//   estimatedRemainingMin — remainingCount × perMatchMinutes
//   plannedRemainingMin   — time from now to the *end* of the last scheduled
//                           match on the court (latestMin + perMatchMinutes
//                           − nowMin, floored at 0). Falls back to
//                           estimatedRemainingMin when no scheduled times exist.
//   delta               — estimatedRemainingMin − plannedRemainingMin
//                         positive = behind schedule, negative = ahead
//
// nowMinutes is optional; defaults to the current wall-clock (read via
// `new Date()`). CourtPacePanel omits it — the 60 s tick forces a re-render
// so this helper re-reads fresh wall-clock time on each tick.
// Tests pass nowMinutes explicitly for determinism.
//
// Exported for the vitest suite.
export function computeCourtPaceStats(byCourt, perMatchMinutes, nowMinutes) {
  // Tournament config can leak strings through (e.g. localStorage,
  // URL params, form input). `5 > 0` is truthy in JS so the original
  // ternary returned the bare value, and `latestMin + "5" - nowMin`
  // would have done string-concatenation arithmetic. Coerce up front
  // and guard against NaN.
  const ppmNum = Number(perMatchMinutes);
  const ppm = Number.isFinite(ppmNum) && ppmNum > 0 ? ppmNum : 3;
  const nowMin = nowMinutes !== undefined ? nowMinutes : (() => {
    const d = new Date();
    return d.getHours() * 60 + d.getMinutes();
  })();

  return Object.entries(byCourt).map(([court, matches]) => {
    // Count any match that is not scheduled or running as "consumed" — this
    // mirrors the courtOrder sort which maps unknown statuses to the completed
    // bucket.  The backend only emits scheduled/running/completed today, but
    // treating the two active statuses as the set to exclude is more defensive
    // than a strict "=== completed" check.
    const completedCount = matches.filter(m => m.status !== "scheduled" && m.status !== "running").length;
    const remainingCount = matches.length - completedCount;
    const estimatedRemainingMin = remainingCount * ppm;

    // Earliest and latest scheduledAt on this court (in minutes)
    const times = matches
      .map(m => timeToMinutes(m.scheduledAt))
      .filter(t => t !== null);
    const latestMin = times.length > 0 ? Math.max(...times) : null;

    // Planned remaining: from now to end of last scheduled match (+ one match duration).
    // If no times available, fall back to estimatedRemainingMin.
    const plannedRemainingMin = latestMin !== null
      ? Math.max(0, latestMin + ppm - nowMin)
      : estimatedRemainingMin;

    const delta = estimatedRemainingMin - plannedRemainingMin;

    return { court, completedCount, remainingCount, estimatedRemainingMin, plannedRemainingMin, delta };
  });
}

// CourtPacePanel — admin-only collapsible card showing per-court pace status
// and a rebalancing suggestion. Never rendered in viewer or display views.
export function CourtPacePanel({ byCourt, safeMatchDuration }) {
  const [open, setOpen] = useStateA(false);
  // setTick forces a re-render every 60 s so computeCourtPaceStats re-reads
  // the current wall-clock time. paceByCourt is rebuilt on every parent render
  // so useMemo would never skip the call anyway — compute stats directly.
  const [, setTick] = useStateA(0);

  // hasData is checked inside the effect so the interval only runs (and causes
  // re-renders) when there are matches to display, not when the panel renders null.
  const hasData = Object.values(byCourt).some(arr => arr.length > 0);
  useEffectA(() => {
    if (!hasData) return;
    const timer = setInterval(() => {
      setTick(t => t + 1);
    }, 60000);
    return () => clearInterval(timer);
  }, [hasData]);

  const stats = computeCourtPaceStats(byCourt, safeMatchDuration);

  // Drop courts with zero matches so the cards (and the rebalance heuristic)
  // ignore empty buckets — e.g. a configured court the user hasn't placed
  // anything on yet, or every non-A court when the operator has applied
  // ?court=A scope to the page. Otherwise those courts render confusing
  // "0/0 done · Done" tiles.
  const populatedStats = stats.filter(s => s.completedCount + s.remainingCount > 0);
  if (populatedStats.length === 0) return null;

  const suggestion = suggestRebalances(populatedStats, safeMatchDuration);

  const badgeStyle = (delta) => {
    const abs = Math.abs(delta);
    if (abs <= 5) return { color: "var(--green, #16a34a)", fontWeight: 600 };
    if (abs <= 20) return { color: "var(--amber, #d97706)", fontWeight: 600 };
    return { color: "var(--red, #dc2626)", fontWeight: 700 };
  };

  const statusLabel = (stat) => {
    if (stat.remainingCount === 0) return <span style={{ color: "var(--ink-3)", fontWeight: 500 }}>Done</span>;
    const abs = Math.abs(stat.delta);
    if (abs <= 5) return <span style={badgeStyle(stat.delta)}>On track</span>;
    const dir = stat.delta > 0 ? "behind" : "ahead";
    return <span style={badgeStyle(stat.delta)}>{Math.round(abs)}m {dir}</span>;
  };

  return (
    <div className="card" style={{ marginBottom: 20 }} data-testid="court-pace-panel">
      <div
        className="card__title"
        style={{ display: "flex", justifyContent: "space-between", cursor: "pointer", marginBottom: open ? 12 : 0 }}
        onClick={() => setOpen(o => !o)}
        onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); setOpen(o => !o); } }}
        role="button"
        tabIndex={0}
        aria-expanded={open}
      >
        <span>Court pace</span>
        <span style={{ fontSize: 18, fontWeight: 400 }}>{open ? "−" : "+"}</span>
      </div>
      {open && (
        <div>
          <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(160px, 1fr))", gap: 8, marginBottom: suggestion ? 12 : 0 }}>
            {populatedStats.map(stat => (
              <div key={stat.court} style={{ fontSize: 12, padding: "6px 10px", background: "var(--bg-2)", borderRadius: 4, border: "1px solid var(--bg-3)" }}>
                <div style={{ fontWeight: 600, marginBottom: 2 }}>Shiaijo {stat.court}</div>
                <div style={{ color: "var(--ink-3)" }}>
                  {stat.completedCount}/{stat.completedCount + stat.remainingCount} done
                </div>
                <div style={{ marginTop: 2 }}>{statusLabel(stat)}</div>
              </div>
            ))}
          </div>
          {suggestion && (
            <div style={{ marginTop: 8, padding: "8px 12px", background: "var(--amber-bg, #fffbeb)", border: "1px solid var(--amber-border, #fde68a)", borderRadius: 6, fontSize: 13 }}>
              <strong>Suggestion:</strong> Move {suggestion.n} {suggestion.n === 1 ? "match" : "matches"} from Shiaijo {suggestion.from} to Shiaijo {suggestion.to} to rebalance court load. Use the court picker on each match card to reassign.
            </div>
          )}
        </div>
      )}
    </div>
  );
}

function suggestRebalances(perCourtStats, perMatchMinutes) {
  if (!perCourtStats || perCourtStats.length < 2 || !perMatchMinutes || perMatchMinutes <= 0) return null;

  let slowest = null;
  let fastest = null;

  perCourtStats.forEach(stat => {
    if (stat.remainingCount > 0) {
      if (!slowest || stat.delta > slowest.delta) {
        slowest = stat;
      }
    }
    if (!fastest || stat.delta < fastest.delta) {
      fastest = stat;
    }
  });

  if (!slowest || !fastest || slowest.court === fastest.court) return null;
  if (slowest.delta <= 0 || fastest.delta >= 0) return null;

  const n = Math.floor(Math.min(slowest.delta, Math.abs(fastest.delta)) / perMatchMinutes);
  if (n <= 0) return null;

  return {
    from: slowest.court,
    to: fastest.court,
    n: n
  };
}

window.AdminSchedulePage = AdminSchedulePage;
window.PerCourtBreakdown = PerCourtBreakdown;
window.AdminScoreEditorPage = AdminScoreEditorPage;
window.AdminScoreEditor = AdminScoreEditor;
window.AdminExport = AdminExport;

// ES exports for the vitest suite. computeCourtPaceStats,
// filterMatchesByCourt, and CourtPacePanel use `export function`
// at their declaration sites.  This block exports the
// remaining helpers that are declared without `export`.
//
// All other top-level components stay behind the window.* pattern to
// match the rest of admin_*.jsx.
// mp-bkg: export pickCopySource and MatchLineupPanel for the vitest suite.
export { timeEdited, timeToMinutes, clampMatchDuration, suggestRebalances, allMatchesCompleted, pickCopySource, MatchLineupPanel };
