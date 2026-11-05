// calendar_rules.jsx: Browser ports of Go scheduling rules.
// Pinned to internal/engine/testdata/calendar_rule_cases.json.
// Go owners:
//   internal/engine:
//     - dayBreaks, skipBreaks (scheduler_slots.go)
//     - replayCourt (schedule.go)
//     - pastMidnight, Overlaps (calendar.go)
//     - ParseClockTime, ParseDurationMinutes (scheduler_slots.go)
//   internal/state:
//     - ParseClockMinutes, FormatClockHHMM (court_floor.go)

import { blockMinutes, parseClock } from './pause_list.jsx';

export { blockMinutes };

/**
 * Strict parser for 24-hour "HH:MM" or "H:MM" clock string without trimming.
 * Returns minutes since midnight (0..1439) or null when invalid.
 */
export function parseClockTime(s) {
  return typeof s === 'string' && s === s.trim() ? parseClock(s) : null;
}

/**
 * Formats minutes since midnight as "HH:MM".
 * Negative numbers become 00:00. Does not wrap at 24h.
 */
export function formatClockHHMM(mins) {
  let m = mins;
  if (typeof m !== 'number' || m < 0) {
    m = 0;
  }
  const h = Math.floor(m / 60);
  const min = Math.floor(m % 60);
  return `${String(h).padStart(2, '0')}:${String(min).padStart(2, '0')}`;
}

/**
 * Reports whether endMins is after midnight (greater than 1440 minutes).
 */
export function pastMidnight(endMins) {
  return endMins > 24 * 60;
}

/**
 * Derives usable pauses for date, sorted ascending by start time.
 * Returns an array of {start, end} windows in minutes since midnight.
 */
export function dayBreaks(pauses, date, days) {
  const pauseList = Array.isArray(pauses) ? pauses : [];
  const dayList = Array.isArray(days) ? days : [];
  const dStr = typeof date === 'string' ? date : '';

  let dayIdx = 0;
  if (dStr === '') {
    dayIdx = 1;
  } else {
    const idx = dayList.indexOf(dStr);
    if (idx !== -1) {
      dayIdx = idx + 1;
    }
  }

  const dayCount = Math.max(dayList.length, 1);
  const breaks = [];

  for (const p of pauseList) {
    if (!p) continue;
    const day = (p.day !== undefined && p.day !== null) ? p.day : 0;
    if (day < 0 || day > dayCount) {
      continue;
    }
    if (day !== 0 && day !== dayIdx) {
      continue;
    }
    const start = parseClockTime(p.start);
    if (start === null) {
      continue;
    }
    const lengthMin = blockMinutes(p.length);
    if (lengthMin <= 0) {
      continue;
    }
    breaks.push({ start, end: start + lengthMin });
  }

  breaks.sort((a, b) => a.start - b.start);
  return breaks;
}

/**
 * Pushes time t past any pause window it falls inside until a fixed point is reached.
 */
export function skipBreaks(t, breaks) {
  if (!Array.isArray(breaks) || breaks.length === 0) {
    return t;
  }
  let cur = t;
  while (true) {
    let moved = false;
    for (const b of breaks) {
      if (cur >= b.start && cur < b.end) {
        cur = b.end;
        moved = true;
        break;
      }
    }
    if (!moved) {
      return cur;
    }
  }
}

/**
 * Simulates a single court's match execution starting at start,
 * advancing past break windows via skipBreaks, and buffering match time.
 * Returns { end, duration } where duration is unrounded buffered minutes.
 */
export function replayCourt(start, recipe, breaks) {
  const runs = (recipe && Array.isArray(recipe.runs)) ? recipe.runs : [];
  const bufferPct = (recipe && typeof recipe.bufferPct === 'number') ? recipe.bufferPct : 0;
  const breakList = Array.isArray(breaks) ? breaks : [];

  let cur = start;
  let matchMin = 0;

  for (const run of runs) {
    const count = (run && typeof run.count === 'number') ? run.count : 0;
    const minutes = (run && typeof run.minutes === 'number') ? run.minutes : 0;
    if (count <= 0) continue;

    for (let i = 0; i < count; i++) {
      cur = skipBreaks(cur, breakList);
      cur += minutes;
    }
    matchMin += count * minutes;
  }

  const raw = cur - start;
  const fixedOverhead = raw - matchMin;
  let multiplier = 1.0;
  if (bufferPct > 0) {
    multiplier += bufferPct / 100.0;
  }
  const buffered = fixedOverhead + matchMin * multiplier;
  const end = start + Math.round(buffered);

  return { end, duration: buffered };
}

/**
 * Reports whether two half-open windows {start, end} in minutes overlap.
 */
export function overlaps(a, b) {
  if (!a || !b) return false;
  if (a.start >= a.end || b.start >= b.end) {
    return false;
  }
  return a.start < b.end && b.start < a.end;
}
