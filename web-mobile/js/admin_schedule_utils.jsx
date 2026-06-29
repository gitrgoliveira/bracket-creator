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

// Coerces the matchDuration form value to a safe integer minutes count
// for arithmetic in durationEstimate (rendered as "HH h MM m") and the
// auto-schedule loop (`cursor += safeMatchDuration` + addMinutes).
//
// Rejects:
//   - NaN / undefined / null            (cleared input → stored as NaN)
//   - Infinity / -Infinity              (impossible via UI but defensive)
//   - non-integers like 2.5             (Copilot found: addMinutes would
//                                        produce "00:2.5": invalid HH:MM.
//                                        and durationEstimate "0h 32.5m")
//   - values < 1                        (zero or negative makes no sense)
//
// Falls back to 3 minutes: the same default the matchDuration state
// uses, so the UX is "if your typed value is invalid, we schedule as if
// you'd left the field at 3 (the placeholder default)."
export function clampMatchDuration(raw, fallback = 3) {
  return Number.isFinite(raw) && Number.isInteger(raw) && raw >= 1 ? raw : fallback;
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
