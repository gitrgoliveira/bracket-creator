// Pure utility functions extracted from admin_schedule.jsx (mp-d7tl).
// No React, no window dependencies: safe to import anywhere including tests.

export function formatMinutes(m) {
  return `${Math.floor(m / 60)}h ${m % 60}m`;
}

// mp-gmcg: kachinuki team matches have a variable bout count, so the
// schedule estimate is a best/average/worst RANGE: the server returns
// additive bestCaseMinutes / worstCaseMinutes bracketing the headline
// totalDurationMinutes (the AVERAGE scenario). Returns {best, average,
// worst} when the estimate genuinely spans a range, or null when the three
// collapse to one number (individual / fixed-format matches, or a legacy
// response without the fields), in which case callers render the single
// total exactly as before.
export function estimateRangeParts(est) {
  if (!est) return null;
  const best = est.bestCaseMinutes;
  const worst = est.worstCaseMinutes;
  if (!Number.isFinite(best) || !Number.isFinite(worst) || best === worst) return null;
  return { best, average: est.totalDurationMinutes, worst };
}

// Estimate minutes from HH:MM string; returns null if invalid
export function timeToMinutes(t) {
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
export function timeEdited(oldScheduledAt, newVal) {
  return (oldScheduledAt || "") !== newVal;
}

// clampDurationSeconds coerces a raw seconds value to a safe positive integer
// for scheduling arithmetic, falling back to `fallback` (default 180s = 3 min,
// matching defaultPerMatchClockSeconds in internal/engine/scheduler_slots.go)
// for NaN / non-finite / sub-1 input. Fractional seconds round to nearest.
export function clampDurationSeconds(raw, fallback = 180) {
  return Number.isFinite(raw) && raw >= 1 ? Math.round(raw) : fallback;
}

// True when the list is non-empty and every match is in 'completed' status.
// Drives the "All matches scored" banner in AdminScoreEditor.
export function allMatchesCompleted(matches) {
  return matches.length > 0 && matches.every(m => m.status === "completed");
}

// T041 (US1, FR-002, SC-002): per-tablet localStorage key. The URL
// ?court= param remains canonical: localStorage is a fallback that lets
// a bookmarked operator tablet land on the same shiaijo after they
// navigate away and return via a bare URL.
export const COURT_STORAGE_KEY = "bc_operator_courts";
