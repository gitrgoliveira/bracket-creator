// Tournament-wide schedule, score editor, and export pages. See
// web-mobile/admin_split_plan.md.

const { useState: useStateA, useMemo: useMemoA, useEffect: useEffectA, useRef: useRefA } = React;

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
    if (estTeamSize > 0) {
      setEstBoutsPerTeamMatch(2 * estTeamSize - 1);
    } else {
      setEstBoutsPerTeamMatch(0);
    }
  }, [estTeamSize]);

  useEffectA(() => {
    if (!estOpen) return;
    // Guard: required params must be valid numbers > 0 to avoid 400s
    if (!Number.isFinite(estMatchDuration) || estMatchDuration <= 0 ||
        !Number.isFinite(estMultiplier) || estMultiplier <= 0 ||
        !Number.isFinite(estCourts) || estCourts <= 0 ||
        !Number.isFinite(estNumMatches) || estNumMatches <= 0) {
      setEstResult(null);
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
        if (!controller.signal.aborted) {
          setEstLoading(false);
        }
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
    const ao = order[a.status] ?? 99;
    const bo = order[b.status] ?? 99;
    if (ao !== bo) return ao - bo;
    return (a.scheduledAt || "99:99").localeCompare(b.scheduledAt || "99:99");
  };
  Object.values(byCourt).forEach((list) => list.sort(courtOrder));
  unassigned.sort(courtOrder);

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
                <div className="form-group">
                  <label className="label">Match duration (min)</label>
                  <input
                    type="number"
                    className="input"
                    value={Number.isFinite(estMatchDuration) ? estMatchDuration : ""}
                    min="1"
                    max="60"
                    step="1"
                    onChange={e => {
                      const val = e.target.value;
                      setEstMatchDuration(val === "" ? NaN : +val);
                    }}
                  />
                </div>
                <div className="form-group">
                  <label className="label">Multiplier</label>
                  <input
                    type="number"
                    step="0.1"
                    className="input"
                    value={Number.isFinite(estMultiplier) ? estMultiplier : ""}
                    min="1"
                    max="3"
                    onChange={e => {
                      const val = e.target.value;
                      setEstMultiplier(val === "" ? NaN : +val);
                    }}
                  />
                </div>
                <div className="form-group">
                  <label className="label">Courts</label>
                  <input
                    type="number"
                    className="input"
                    value={Number.isFinite(estCourts) ? estCourts : ""}
                    min="1"
                    max="26"
                    step="1"
                    onChange={e => {
                      const val = e.target.value;
                      setEstCourts(val === "" ? NaN : +val);
                    }}
                  />
                </div>
                <div className="form-group">
                  <label className="label">Matches</label>
                  <input
                    type="number"
                    className="input"
                    value={Number.isFinite(estNumMatches) ? estNumMatches : ""}
                    min="1"
                    step="1"
                    onChange={e => {
                      const val = e.target.value;
                      setEstNumMatches(val === "" ? NaN : +val);
                    }}
                  />
                </div>
                <div className="form-group">
                  <label className="label">Team size (0=indiv)</label>
                  <input
                    type="number"
                    className="input"
                    value={Number.isFinite(estTeamSize) ? estTeamSize : ""}
                    min="0"
                    step="1"
                    onChange={e => {
                      const val = e.target.value;
                      setEstTeamSize(val === "" ? NaN : +val);
                    }}
                  />
                </div>
                <div className="form-group">
                  <label className="label">Bouts per team match</label>
                  <input
                    type="number"
                    className="input"
                    value={Number.isFinite(estBoutsPerTeamMatch) ? estBoutsPerTeamMatch : ""}
                    min="0"
                    step="1"
                    onChange={e => {
                      const val = e.target.value;
                      setEstBoutsPerTeamMatch(val === "" ? NaN : +val);
                    }}
                  />
                </div>
                <div className="form-group">
                  <label className="label">Buffer %</label>
                  <input
                    type="number"
                    className="input"
                    value={Number.isFinite(estBuffer) ? estBuffer : ""}
                    min="0"
                    max="100"
                    step="1"
                    onChange={e => {
                      const val = e.target.value;
                      setEstBuffer(val === "" ? NaN : +val);
                    }}
                  />
                </div>
                <div className="form-group">
                  <label className="label">Ceremony (min)</label>
                  <input
                    type="number"
                    className="input"
                    value={Number.isFinite(estCeremony) ? estCeremony : ""}
                    min="0"
                    step="1"
                    onChange={e => {
                      const val = e.target.value;
                      setEstCeremony(val === "" ? NaN : +val);
                    }}
                  />
                </div>
              </div>

              {estResult && (
                <div style={{ marginTop: 20, paddingTop: 20, borderTop: "1px solid var(--bg-3)" }}>
                  <div style={{ display: "flex", flexWrap: "wrap", alignItems: "baseline", gap: 12 }}>
                    <div style={{ fontSize: 24, fontWeight: 700 }}>Total: {Math.floor(estResult.totalDurationMinutes / 60)}h {estResult.totalDurationMinutes % 60}m</div>
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
      {/* T097: formatIpponsScore appends "Kiken / Fus. / DH / (E)" suffixes
          for non-fought decisions and overtime. The match-level decision
          covers kiken / fusenpai / daihyosen here; per-bout fusensho is a
          SubMatchResult and is rendered inside the score modal — the row
          here doesn't expose individual bout cells.
          TODO(T096): once per-bout fusensho is wired through the team-score
          serializer and the schedule row exposes bout details, append an
          "FS" badge to each affected bout cell. */}
      <div style={{ display: "flex", flexDirection: "column", alignItems: "flex-end", gap: 4 }}>
        {m.status === "completed" && (
          <div style={{ fontFamily: "var(--font-mono)", fontWeight: 700, fontSize: 13 }}>{window.formatIpponsScore(m.ipponsB, m.ipponsA, m.score, m.decision, m.encho)}</div>
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
function AdminScoreEditorPage({ tournament, onBack, onEditScore, onMoveCourt, onLogout, onViewerMode }) {
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

function AdminScoreEditor({ t, c, onEditScore, onMoveCourt, restrictToCompId }) {
  const [filter, setFilter] = useStateA("");
  const [compFilter, setCompFilter] = useStateA(restrictToCompId || "all");
  const [statusFilter, setStatusFilter] = useStateA("all");
  const [openMatch, setOpenMatch] = useStateA(null);
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
                    {m.status === "completed" && window.formatIpponsScore(m.ipponsB, m.ipponsA, m.score, m.decision, m.encho)}
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
        // Chained nav (Prev/Next/Finish+Start Next/←/→) must stay on the same
        // shiaijo. Operators run matches per-court; jumping courts mid-flow
        // skips the wrong matches. Unassigned matches scope to other
        // unassigned matches so the behaviour is consistent.
        const openCourt = openMatch.court || "";
        const sameCourt = filtered.filter(m => (m.court || "") === openCourt);
        const openIdx = sameCourt.findIndex(m => `${m.compId}:${m.id}` === `${openMatch.compId}:${openMatch.id}`);
        const prevMatch = openIdx > 0 ? sameCourt[openIdx - 1] : null;
        const nextMatch = openIdx >= 0 && openIdx < sameCourt.length - 1 ? sameCourt[openIdx + 1] : null;
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
            onSubmitAndNext={nextMatch ? async (patch) => {
              try {
                await onEditScore(openMatch.compId, openMatch.id, patch, openMatch);
                if (mountedRef.current) setOpenMatch(nextMatch);
              } catch (_err) { /* keep modal open on error */ }
            } : null}
          />
        );
      })()}
    </div>
  );
}


function AdminExport({ c, t, password }) {
  const url = `${window.location.origin}/viewer.html?id=${t.id}#comp-${c.id}`;

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

function PerCourtBreakdown({ perCourtMinutes }) {
  if (!perCourtMinutes || perCourtMinutes.length === 0) return null;
  return (
    <div className="est-breakdown" style={{ marginTop: 12 }}>
      <div style={{ fontSize: 12, fontWeight: 600, marginBottom: 4, color: "var(--ink-2)" }}>Per-court breakdown:</div>
      <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(120px, 1fr))", gap: 8 }}>
        {perCourtMinutes.map((m, i) => (
          <div key={i} style={{ fontSize: 12, padding: "4px 8px", background: "var(--bg-2)", borderRadius: 4, border: "1px solid var(--bg-3)" }}>
            <span style={{ color: "var(--ink-3)" }}>Court {String.fromCharCode(65 + i)}:</span>
            <strong style={{ marginLeft: 4 }}>{Math.floor(m / 60)}h {m % 60}m</strong>
          </div>
        ))}
      </div>
    </div>
  );
}

window.AdminSchedulePage = AdminSchedulePage;
window.PerCourtBreakdown = PerCourtBreakdown;
window.AdminScoreEditorPage = AdminScoreEditorPage;
window.AdminScoreEditor = AdminScoreEditor;
window.AdminExport = AdminExport;

// ES export for the vitest suite — pure helpers only. Components stay
// behind the window.* pattern to match the rest of admin_*.jsx.
export { timeEdited, timeToMinutes, clampMatchDuration };
