// Court-pacing components extracted from admin_schedule.jsx (mp-d7tl).
// filterMatchesByCourt, computeCourtPaceStats, CourtPacePanel, PerCourtBreakdown, suggestRebalances.

import { formatMinutes, timeToMinutes } from './admin_schedule_utils.jsx';

const { useState: useStateA, useEffect: useEffectA } = React;

// filterMatchesByCourt(matches, courtParam): pure list filter.
//
// FR-001 / T040 (US1, SC-001): bookmark `/admin/schedule?court=A` to scope
// an operator's view to a single shiaijo. Returns matches unchanged when no
// filter is set ("", null, undefined, or "all"); otherwise returns only
// matches whose `m.court` exactly equals courtParam (case-sensitive: the
// app's canonical court labels are uppercase A–Z per the Excel layout).
// Pure and DOM-free so the helper is unit-testable from jsdom without
// mounting AdminSchedulePage.
export function filterMatchesByCourt(matches, courtParam) {
  const c = (courtParam || "").trim();
  if (c === "" || c === "all") return matches;
  return matches.filter((m) => m.court === c);
}

// computeCourtPaceStats(byCourt, perMatchMinutes, nowMinutes): deterministic
// when nowMinutes is supplied; non-deterministic (reads wall-clock via
// `new Date()`) when omitted.
//
// For each court, derive:
//   court               : the court label (e.g. "A")
//   completedCount      : matches that are neither scheduled nor running (i.e. the slot is consumed)
//   remainingCount      : matches NOT yet completed
//   estimatedRemainingMin: remainingCount × perMatchMinutes
//   plannedRemainingMin   : time from now to the *end* of the last scheduled
//                           match on the court (latestMin + perMatchMinutes
//                           − nowMin, floored at 0). Falls back to
//                           estimatedRemainingMin when no scheduled times exist.
//   delta               : estimatedRemainingMin − plannedRemainingMin
//                         positive = behind schedule, negative = ahead
//
// nowMinutes is optional; defaults to the current wall-clock (read via
// `new Date()`). CourtPacePanel omits it: the 60 s tick forces a re-render
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
    // Count any match that is not scheduled or running as "consumed": this
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

export function suggestRebalances(perCourtStats, perMatchMinutes) {
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

export function PerCourtBreakdown({ perCourtMinutes }) {
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

// CourtPacePanel: admin-only collapsible card showing per-court pace status
// and a rebalancing suggestion. Never rendered in viewer or display views.
export function CourtPacePanel({ byCourt, safeMatchDuration }) {
  const [open, setOpen] = useStateA(false);
  // setTick forces a re-render every 60 s so computeCourtPaceStats re-reads
  // the current wall-clock time. paceByCourt is rebuilt on every parent render
  // so useMemo would never skip the call anyway: compute stats directly.
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
  // ignore empty buckets: e.g. a configured court the user hasn't placed
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
