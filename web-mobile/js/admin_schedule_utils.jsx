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

// secondsToMMSS formats a total-seconds integer as "M:SS" (e.g. 150 -> "2:30").
// Returns "" for non-finite / negative input so a cleared field renders empty.
export function secondsToMMSS(sec) {
  if (!Number.isFinite(sec) || sec < 0) return "";
  const s = Math.round(sec);
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`;
}

// mmssToSeconds parses "M", "M:SS", or "MM:SS" into integer seconds.
// Returns NaN for blank/invalid input; a seconds component >= 60 is rejected
// (operators must carry into minutes). Used by the mm:ss DurationInput.
export function mmssToSeconds(str) {
  if (str == null) return NaN;
  const t = String(str).trim();
  if (t === "") return NaN;
  const parts = t.split(":");
  if (parts.length === 1) {
    const m = Number(parts[0]);
    return Number.isFinite(m) && m >= 0 ? Math.round(m * 60) : NaN;
  }
  if (parts.length === 2) {
    const m = Number(parts[0]);
    const s = Number(parts[1]);
    if (!Number.isFinite(m) || !Number.isFinite(s) || m < 0 || s < 0 || s >= 60) return NaN;
    return Math.round(m * 60 + s);
  }
  return NaN;
}

// clampDurationSeconds coerces a raw seconds value to a safe positive integer
// for scheduling arithmetic, falling back to `fallback` (default 180s = 3 min,
// matching defaultPerMatchClockSeconds in internal/engine/scheduler_slots.go)
// for NaN / non-finite / sub-1 input. Fractional seconds round to nearest.
export function clampDurationSeconds(raw, fallback = 180) {
  return Number.isFinite(raw) && raw >= 1 ? Math.round(raw) : fallback;
}

// effectiveDurationSeconds resolves the canonical per-match seconds from a
// competition's seconds field, falling back to the legacy whole-minute field
// (x60). Mirrors EffectivePoolMatchSeconds in internal/state/models.go so the
// UI shows the same value the scheduler uses. Returns NaN when neither is set
// so callers can render the "default" placeholder.
export function effectiveDurationSeconds(seconds, minutes) {
  if (Number.isFinite(seconds) && seconds > 0) return seconds;
  if (Number.isFinite(minutes) && minutes > 0) return minutes * 60;
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
