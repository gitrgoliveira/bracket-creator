// Pure utility functions extracted from admin_schedule.jsx (mp-d7tl).
// No React, no window dependencies: safe to import anywhere including tests.

export function formatMinutes(m) {
  return `${Math.floor(m / 60)}h ${m % 60}m`;
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

// effectiveDurationSeconds resolves the canonical per-match seconds from a
// competition's seconds field, falling back to the per-phase whole-minute
// field and then the legacy single MatchDuration field (both x60). Mirrors the
// 3-tier effectiveMatchSeconds in internal/state/models.go so the UI shows the
// same value the scheduler uses even for a legacy config the server has not
// normalized (e.g. a raw SSE-pushed record). Returns NaN when none is set so
// callers can render the "default" placeholder.
export function effectiveDurationSeconds(seconds, minutes, legacyMinutes) {
  if (Number.isFinite(seconds) && seconds > 0) return seconds;
  if (Number.isFinite(minutes) && minutes > 0) return minutes * 60;
  if (Number.isFinite(legacyMinutes) && legacyMinutes > 0) return legacyMinutes * 60;
  return NaN;
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
