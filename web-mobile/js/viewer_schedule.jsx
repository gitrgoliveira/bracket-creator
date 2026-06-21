// Schedule + filter components extracted from viewer.jsx (mp-pxxc step 7).
// Pure file split — no behaviour change.

import { poolLabel, tournamentMatches, compareDmy } from './viewer_utils.jsx';
import { matchParticipantIds, matchParticipantNames, useWatchlist, resolveEntryPlayerIds, resolveWatchedPlayers, findPrimaryEntry, buildRoster } from './viewer_watchlist_core.jsx';
import { withNumber } from './match_scoreboard.jsx';
import { MatchViewerModal, localQueueLabelCompact } from './viewer_match.jsx';

const { useState, useMemo, useRef: useRefV } = React;
const EmptyState = window.EmptyState;

// mp-xhaa: the pinned-primary entry key (string), persisted separately from the
// list so reordering/dedup of the list never disturbs the pin. Only meaningful
// when the watchlist has ≥2 entries (with exactly one entry the primary is
// implicit — see effectivePrimaryKey).
export const LS_WATCH_PRIMARY = "bc_watch_primary";
export const WATCHED_UPCOMING_MAX = 6;
// mp-xhaa: bound on the "watched upcoming" compact list shown below the hero
// when ≥2 entities are watched. Kept deliberately small so the panel stays
// glanceable on a phone even with a 50-entry watchlist (critique P2).
export const WATCHED_UPCOMING_LIST_MAX = 10;

// Hook: the pinned-primary entry key (string) backed by localStorage.
// Empty string = no explicit pin (the effective primary is then implicit when
// exactly one entry exists — see effectivePrimaryKey).
export function usePrimaryWatch() {
  const [key, setKey] = useState(() => {
    if (typeof window === "undefined") return "";
    try {
      return window.localStorage.getItem(LS_WATCH_PRIMARY) || "";
    } catch (_e) {
      return "";
    }
  });
  const persist = (next) => {
    const val = next || "";
    setKey(val);
    if (typeof window === "undefined") return;
    try {
      if (val) window.localStorage.setItem(LS_WATCH_PRIMARY, val);
      else window.localStorage.removeItem(LS_WATCH_PRIMARY);
    } catch (_e) { /* ignore — in-memory primary selection remains valid for the session */ }
  };
  return [key, persist];
}

// Return up to 6 upcoming matches across any watched player, sorted by
// scheduledAt ascending (empty/missing times sort last via "99:99" sentinel).
// "Upcoming" = status !== "completed" — we keep `running` matches in the
// list so a coach can spot a watched player who just started.
export function buildWatchlistUpcoming(watched, allMatches, max = WATCHED_UPCOMING_MAX) {
  const watchedIds = new Set();
  const watchedNames = new Set();
  (Array.isArray(watched) ? watched : []).forEach((w) => {
    if (w && w.id) watchedIds.add(String(w.id));
    if (w && w.name) watchedNames.add(w.name.trim().toLowerCase());
  });
  if (watchedIds.size === 0 && watchedNames.size === 0) return [];
  const list = Array.isArray(allMatches) ? allMatches : [];
  const upcoming = list.filter((m) => {
    if (!m || m.status === "completed") return false;
    const [a, b] = matchParticipantIds(m);
    if ((a && watchedIds.has(a)) || (b && watchedIds.has(b))) return true;
    if (watchedNames.size > 0) {
      const [aN, bN] = matchParticipantNames(m);
      if ((aN && watchedNames.has(aN.trim().toLowerCase())) || (bN && watchedNames.has(bN.trim().toLowerCase()))) return true;
    }
    return false;
  });
  upcoming.sort((x, y) => {
    const xt = x.scheduledAt || "99:99";
    const yt = y.scheduledAt || "99:99";
    return xt.localeCompare(yt);
  });
  return upcoming.slice(0, max);
}

// Return the subset of `matches` where the followed player participates.
// Matching is by UUID first; if `fallbackName` is provided and no UUID hits
// (e.g., legacy data that still keys by display name), fall back to a
// case-insensitive exact match on either side's name.
export function buildPlayerMatchHighlight(playerId, matches, fallbackName) {
  const id = (playerId || "").toString();
  const list = Array.isArray(matches) ? matches : [];
  const byId = id ? list.filter((m) => {
    const [a, b] = matchParticipantIds(m);
    return (a && a === id) || (b && b === id);
  }) : [];
  if (byId.length > 0 || !fallbackName) return byId;
  const needle = String(fallbackName).trim().toLowerCase();
  if (!needle) return byId;
  return list.filter((m) => {
    const [an, bn] = matchParticipantNames(m);
    return (an && an.trim().toLowerCase() === needle)
        || (bn && bn.trim().toLowerCase() === needle);
  });
}

const pluralize = window.pluralize;
const hasBothSides = (m) => window.hasBothSides(m);
const formatDate = window.formatDate;

// Reusable multi-player filter — used by both viewer & admin schedule pages.
// Picks any number of participants/teams across all competitions; matches are
// kept if they involve ANY of the picked sides. Free-text dojo search works in parallel.
export function PlayerMultiFilter({ tournament, picked, setPicked, dojoText, setDojoText }) {
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

  window.useClickOutside(ref, () => setOpen(false));

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
            <button type="button" onClick={(e) => { e.stopPropagation(); toggle(p); }} aria-label="Remove">×</button>
          </span>
        ))}
        {dojoText ? (
          <span className="pmf__chip pmf__chip--text">
            "{dojoText}"
            <button type="button" onClick={(e) => { e.stopPropagation(); setDojoText(""); }} aria-label="Remove">×</button>
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
              <button type="button" className="btn btn--ghost btn--sm" onClick={() => { setPicked([]); setDojoText(""); setQuery(""); }}>Clear all</button>
            )}
          </div>
          {q && (
            <button type="button" className="pmf__option pmf__option--text" onClick={() => { setDojoText(query.trim()); setQuery(""); }}>
              <span>Match text "<b>{query}</b>" in any name/dojo</span>
            </button>
          )}
          {matches.map((p) => {
            const isPicked = !!picked.find((x) => x.id === p.id);
            return (
              <button type="button" key={p.id} className={`pmf__option ${isPicked ? "is-picked" : ""}`} onClick={() => toggle(p)}>
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

export function applyFilters(matches, picked, dojoText, compFilter) {
  const ids = new Set(picked.map((p) => p.id));
  const names = new Set(picked.map((p) => p.name).filter(Boolean));
  const dt = (dojoText || "").trim().toLowerCase();
  return matches.filter((m) => {
    if (compFilter !== "all" && m.compId !== compFilter) return false;
    if (ids.size > 0) {
      const hit = (m.sideA && (ids.has(m.sideA.id) || names.has(m.sideA.name))) || (m.sideB && (ids.has(m.sideB.id) || names.has(m.sideB.name)));
      if (!hit) return false;
    }
    if (dt) {
      const hit = [m.sideA?.name, m.sideB?.name, m.sideA?.dojo, m.sideB?.dojo].some((s) => (s || "").toLowerCase().includes(dt));
      if (!hit) return false;
    }
    return true;
  });
}

export function matchHighlightedBy(m, picked, dojoText) {
  const ids = new Set(picked.map((p) => p.id));
  const names = new Set(picked.map((p) => p.name).filter(Boolean));
  if (ids.size > 0 && ((m.sideA && (ids.has(m.sideA.id) || names.has(m.sideA.name))) || (m.sideB && (ids.has(m.sideB.id) || names.has(m.sideB.name))))) return true;
  const dt = (dojoText || "").trim().toLowerCase();
  if (dt && [m.sideA?.name, m.sideB?.name, m.sideA?.dojo, m.sideB?.dojo].some((s) => (s || "").toLowerCase().includes(dt))) return true;
  return false;
}

export function TWMatch({ m, highlight, onClick }) {
  const aWin = m.winner && m.sideA && m.winner.id === m.sideA.id;
  const bWin = m.winner && m.sideB && m.winner.id === m.sideB.id;
  // Bracket matches carry scoreA/scoreB strings rather than ipponsA/B arrays
  // (see normalizeMatch). Apply the same fallback used in VSchedItem so the
  // score cell renders the derived winnerPts–loserPts string instead of "—".
  const twIpponsA = m.ipponsA || window.ipponsFromScore(m.scoreA);
  const twIpponsB = m.ipponsB || window.ipponsFromScore(m.scoreB);
  const scoreStr = m.status === "completed" ? window.matchScoreStr(m, twIpponsB, twIpponsA) : null;
  // FR-025: per-court queue position — see VSchedItem for the contract.
  // Short pill form here because the tw-match row is denser than the
  // upcoming-list row in the per-competition viewer. Wording is owned
  // by display.jsx::queueLabelCompact (bead mp-e3k); we still grab `qp`
  // separately because the accent-color styling below keys off qp===1.
  const qp = Number(m.queuePosition);
  const queuePill = (window.queueLabelCompact || localQueueLabelCompact)(m);
  // Render an interactive <button> only when there is a real click handler;
  // otherwise a plain <div> so the row is not focusable and not announced as a
  // button to keyboard / screen-reader users (a no-op button is confusing).
  const Tag = onClick ? "button" : "div";
  return (
    <Tag className={`tw-match ${m.status === "running" ? "tw-match--running" : ""} ${m.status === "completed" ? "tw-match--done" : ""} ${highlight ? "tw-match--highlight" : ""}`} {...(onClick ? { type: "button", onClick } : {})} style={{ textAlign: "left", border: "none", background: "none", cursor: onClick ? "pointer" : "default" }}>
      <div className="tw-match__meta">
        <div className="tw-match__time">{m.scheduledAt || "—"}</div>
        <div className="tw-match__phase">{m.phase === "pool" ? poolLabel(m) : m.round}</div>
        {queuePill && (
          <div className="tw-match__queue" style={{ fontSize: 10, fontWeight: 700, color: qp === 1 ? "var(--accent)" : "var(--ink-3)", marginTop: 2 }}>
            {queuePill}
          </div>
        )}
      </div>
      <div className="tw-match__players">
        <div className={`tw-match__name ${bWin ? "tw-match__name--w" : ""}`}>
          <span className="tw-match__badge tw-match__badge--shiro">S</span>
          {withNumber(m.sideB)}
        </div>
        <div className={`tw-match__name ${aWin ? "tw-match__name--w" : ""}`}>
          <span className="tw-match__badge tw-match__badge--aka">A</span>
          {withNumber(m.sideA)}
        </div>
        <div className="tw-match__comp">{m.compName}</div>
      </div>
      <div style={{ textAlign: "right", fontFamily: "var(--font-mono)", fontWeight: 700, fontSize: 13 }}>
        {m.status === "completed" && scoreStr}
        {m.status === "completed" && m.score?.type === "bye" && <span style={{ fontSize: 10, color: "var(--ink-3)" }}>BYE</span>}
        {/* No centre "●" dot: a running match is signalled by the row's
            .tw-match--running highlight (accent ring). The labelled "● NOW"
            badge elsewhere is a separate status affordance. */}
      </div>
    </Tag>
  );
}

// Tournament-wide schedule (across competitions) — grouped by day, then court swimlanes + filter
export function ScheduleViewer({ tournament, tweaks }) {
  const allMatches = useMemo(() => tournamentMatches(tournament).filter(hasBothSides), [tournament]);
  const courts = tournament.courts || [];

  // mp-xhaa: auto-populate the schedule's `picked` filter from the unified
  // watchlist (resolved to flat players, so dojo entries expand to current
  // members) so the existing matchHighlightedBy / .tw-match--highlight path
  // lights up the rows the viewer cares about. Seeded once from localStorage;
  // the user can still add or remove chips — we only set the initial value,
  // then `picked` is owned by the schedule (no live re-sync, which would
  // fight the user's edits).
  const [watchlist] = useWatchlist();
  const [primaryKey, setPrimaryKey] = usePrimaryWatch();
  const schedRoster = useMemo(() => buildRoster(tournament.competitions), [tournament]);
  const initialPicked = useMemo(() => {
    return resolveWatchedPlayers(watchlist, schedRoster).map((p) => ({ id: p.id, name: p.name || "", dojo: p.dojo || "" }));
  }, [watchlist, schedRoster]);
  const primaryEntry = useMemo(() => findPrimaryEntry(watchlist, primaryKey), [watchlist, primaryKey]);
  const primaryLabel = primaryEntry
    ? (primaryEntry.type === "dojo" ? primaryEntry.dojo : (schedRoster.find((p) => p.id === primaryEntry.id)?.name || primaryEntry.name || ""))
    : "";
  const [picked, setPicked] = useState(initialPicked);
  // If schedRoster was empty at mount (async load), seed picked once it arrives.
  // pickedSeeded starts true when data was ready at mount (no async case to handle).
  // Once seeded, user edits are never overwritten.
  const pickedSeeded = useRefV(initialPicked.length > 0);
  React.useEffect(() => {
    if (pickedSeeded.current) return;
    if (initialPicked.length > 0) {
      setPicked(initialPicked);
      pickedSeeded.current = true;
    }
  }, [initialPicked]);
  const [dojoText, setDojoText] = useState("");
  const [courtFilter, setCourtFilter] = useState("all");

  // Derive unique days from competitions and matches
  const allDates = useMemo(() => {
    const days = new Set();
    (tournament.competitions || []).forEach((c) => { if (c.date) days.add(c.date); });
    allMatches.forEach((m) => { if (m.date) days.add(m.date); });
    const sorted = Array.from(days).sort(compareDmy);
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
    const ao = order[a.status] ?? 2;
    const bo = order[b.status] ?? 2;
    if (ao !== bo) return ao - bo;
    return (a.scheduledAt || "99:99").localeCompare(b.scheduledAt || "99:99");
  }));

  const matchHasFilter = (m) => matchHighlightedBy(m, picked, dojoText);
  const hasAnyFilter = picked.length > 0 || dojoText || compFilter !== "all" || courtFilter !== "all";
  const multiDay = allDates.length > 1 || (allDates.length === 1 && allDates[0] !== "");

  return (
    <div className="tw-sched">
      {primaryEntry && watchlist.length >= 2 && primaryKey ? (
        <div
          className="tw-sched__following"
          style={{
            display: "flex", alignItems: "center", gap: 8, padding: "6px 10px",
            marginBottom: 8, fontSize: 13, background: "var(--bg-2)",
            border: "1px solid var(--line)", borderRadius: 6
          }}
        >
          <span style={{ color: "var(--ink-3)" }}>Primary:</span>
          <span style={{ fontWeight: 600 }}>{primaryLabel || "(unknown)"}</span>
          <button type="button"
            className="btn btn--ghost btn--sm btn--clear-follow"
            style={{ marginLeft: "auto" }}
            onClick={() => {
              // Unpin the primary and drop its resolved players from the
              // local schedule filter so the highlight disappears.
              const ids = new Set(resolveEntryPlayerIds(primaryEntry, schedRoster));
              setPrimaryKey("");
              setPicked(picked.filter((p) => !ids.has(p.id)));
            }}
            aria-label="Unpin primary"
          >
            ✕ Clear
          </button>
        </div>
      ) : null}
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
          <button type="button" className="btn btn--ghost btn--sm" onClick={() => { setPicked([]); setDojoText(""); setCompFilter("all"); setCourtFilter("all"); }}>Clear</button>
        )}
        <span style={{ marginLeft: "auto", fontSize: 12, color: "var(--ink-3)" }}>{pluralize(dayFiltered.length, "match", "matches")} of {allMatches.length}</span>
      </div>

      {multiDay && (
        <div className="day-tabs">
          {allDates.map((d) => (
            <button type="button" key={d} className={`day-tab ${activeDay === d ? "is-active" : ""}`} onClick={() => setActiveDay(d)}>
              {d ? formatDate(d) : "All days"}
            </button>
          ))}
        </div>
      )}

      <div className="tw-courts">
        {allMatches.length === 0 ? (
          <EmptyState icon="🗓" title="No matches scheduled yet" message="The schedule will appear here once the tournament begins." style={{ gridColumn: "1 / -1" }} />
        ) : courts.map((cc) => {
          const list = byCourt[cc] || [];
          const runningOn = list.find((m) => m.status === "running");
          if (courtFilter !== "all" && cc !== courtFilter) return null;
          return (
            <div key={cc} className="tw-court">
              <div className="tw-court__head">
                <div>
                  <div className="tw-court__title">SHIAIJO {cc}</div>
                  <div className="tw-court__sub">{list.length} match{list.length === 1 ? "" : "es"}{runningOn ? " · in progress" : ""}</div>
                </div>
                {runningOn && <span className="bc-running">● NOW</span>}
              </div>
              <div className="tw-court__list">
                {list.length === 0 ? (
                  <div style={{ fontSize: 12, color: "var(--ink-3)", padding: "20px 8px", textAlign: "center" }}>No matches</div>
                ) : list.map((m) => (
                  <TWMatch key={`${m.compId}:${m.id}`} m={m} highlight={matchHasFilter(m)} onClick={tweaks.onMatchClick ? () => tweaks.onMatchClick(m) : undefined} />
                ))}
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}

// Tournament-wide schedule wrapper for the viewer (its own screen)
export function ViewerSchedule({ tournament, onBack, tweaks }) {
  const [selectedMatch, setSelectedMatch] = useState(null);
  const extendedTweaks = { ...tweaks, onMatchClick: setSelectedMatch };
  return (
    <div className="viewer">
      <div className="viewer__shell">
        <div className="viewer__head">
          <button type="button" className="viewer__back" onClick={onBack} aria-label="Back">←</button>
          <div className="viewer__title-block">
            <div className="viewer__eyebrow">{tournament.name}</div>
            <div className="viewer__title">Schedule</div>
            <div className="viewer__sub">All matches across courts and competitions</div>
          </div>
        </div>
        <div className="viewer__body">
          <ScheduleViewer tournament={tournament} tweaks={extendedTweaks} />
          {window.VersionFooter && <window.VersionFooter />}
        </div>
      </div>
      {selectedMatch && <MatchViewerModal match={selectedMatch} onClose={() => setSelectedMatch(null)} tournament={tournament} />}
    </div>
  );
}

if (typeof window !== 'undefined') {
  window.PlayerMultiFilter = PlayerMultiFilter;
  window.applyFilters = applyFilters;
  window.matchHighlightedBy = matchHighlightedBy;
  window.ViewerSchedule = ViewerSchedule;
}
