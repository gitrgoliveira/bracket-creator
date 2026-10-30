// The ONE owner of the court pace projection (decision D4).
// Pure functions, plus the one hook a screen reads them through
// (useCourtPaceLookup); no imports from admin_* modules.

import {
    compareCourtQueue,
    courtQueueEntriesOf,
    courtQueueKey,
    queueStartRank,
} from './court_queue.jsx';
import { serverNowMs } from './server_clock.jsx';

export const PACE_SHOW_THRESHOLD_MINUTES = 5;

// PACE_TICK_MS: how often a screen showing projected times reads the clock again.
export const PACE_TICK_MS = 60000;

// paceClockMinutes: "HH:MM" to minutes of day, or null when invalid.
export function paceClockMinutes(hhmm) {
    const rank = queueStartRank(hhmm);
    return rank === Infinity ? null : rank;
}

// formatPaceClock: minutes of day to zero-padded "HH:MM" (rounded to nearest minute, wraps past 24h).
export function formatPaceClock(minutes) {
    if (typeof minutes !== 'number' || !Number.isFinite(minutes)) {
        return "";
    }
    const rounded = Math.round(minutes);
    const mod = ((rounded % 1440) + 1440) % 1440;
    const h = Math.floor(mod / 60);
    const m = mod % 60;
    const hh = String(h).padStart(2, '0');
    const mm = String(m).padStart(2, '0');
    return `${hh}:${mm}`;
}

// stampMinutesOfDay: local minutes of day of an epoch-ms stamp, or null for 0/missing/non-finite.
export function stampMinutesOfDay(ms) {
    if (typeof ms !== 'number' || !Number.isFinite(ms) || ms <= 0) {
        return null;
    }
    const d = new Date(ms);
    if (Number.isNaN(d.getTime())) {
        return null;
    }
    return d.getHours() * 60 + d.getMinutes() + d.getSeconds() / 60;
}

// paceDisplay: formats display text, ariaLabel and projected flag.
// When projected start differs from plan by 5 minutes or more, projected time is shown alone as "~HH:MM".
export function paceDisplay(scheduledAt, projectedMinutes) {
    const textDefault = scheduledAt || "";
    if (typeof projectedMinutes !== 'number' || !Number.isFinite(projectedMinutes)) {
        return { text: textDefault, ariaLabel: textDefault, projected: false };
    }
    const planned = paceClockMinutes(scheduledAt);
    if (planned === null) {
        return { text: textDefault, ariaLabel: textDefault, projected: false };
    }
    const rounded = Math.round(projectedMinutes);
    if (Math.abs(rounded - planned) >= PACE_SHOW_THRESHOLD_MINUTES) {
        const timeStr = formatPaceClock(projectedMinutes);
        return {
            text: `~${timeStr}`,
            ariaLabel: `about ${timeStr}`,
            projected: true,
        };
    }
    return { text: textDefault, ariaLabel: textDefault, projected: false };
}

// paceKey: lookup key for competitionId and matchId.
export function paceKey(competitionId, matchId) {
    return `${competitionId || ""}\u0000${matchId || ""}`;
}

// paceClockNow: the ONE reading of the clock a pace lookup takes, as local
// minutes of day plus today's date in the canonical DD-MM-YYYY (the format a
// competition's own `date` carries, which courtPaceLookup compares it with).
// It reads the SERVER's time, the frame the finish stamps (modifiedAt) are in,
// so a device whose own clock is off projects the same times as every other.
export function paceClockNow(d = new Date(serverNowMs())) {
    const dd = String(d.getDate()).padStart(2, '0');
    const mm = String(d.getMonth() + 1).padStart(2, '0');
    return { now: d.getHours() * 60 + d.getMinutes(), today: `${dd}-${mm}-${d.getFullYear()}` };
}

// paceDisplayOf: the projected-time display of one match, or null when the
// lookup is absent or holds nothing for it.
export function paceDisplayOf(lookup, competitionId, matchId) {
    return lookup?.byMatch?.get(paceKey(competitionId, matchId))?.display || null;
}

// PaceTime: the projected time as a marked span when the display is projected,
// otherwise `fallback` (the planned time). Extra props land on the span. The
// spoken "about HH:MM" is an sr-only span beside the hidden "~HH:MM": a plain
// span may not be named, so an aria-label on it is ignored by screen readers.
export function PaceTime({ display, fallback = null, ...rest }) {
    return display?.projected ? (
        <span className="pace-projected" {...rest}>
            <span aria-hidden="true">{display.text}</span>
            <span className="sr-only">{display.ariaLabel}</span>
        </span>
    ) : fallback;
}

// useCourtPaceLookup: courtPaceLookup over `competitions`, read again on the
// pace tick so a projection moves on with the clock between pushes.
export function useCourtPaceLookup(competitions) {
    // The tick only renders again: the clock is read on every render, and the
    // lookup is worked out again when its minute or day moves on.
    const [, setTick] = React.useState(0);
    React.useEffect(() => {
        const id = setInterval(() => setTick((t) => t + 1), PACE_TICK_MS);
        return () => clearInterval(id);
    }, []);
    const { now, today } = paceClockNow();
    return React.useMemo(
        () => courtPaceLookup(competitions, { now, today }),
        [competitions, now, today]
    );
}

// projectCourtPace: projects waiting match starts for ONE court-day.
// entries: all entries of the court-day in court_queue entry shape (any status).
// now: current minutes of day.
// slotOf: returns slot in minutes for an entry (defaults to reading entry.slotMinutes).
export function projectCourtPace(entries, { now, slotOf } = {}) {
    if (!entries || !Array.isArray(entries)) {
        return { drift: 0, completedDrift: null, idleDrift: null, projections: [] };
    }

    const getSlot = typeof slotOf === 'function'
        ? slotOf
        : (e => e?.slotMinutes);

    const validSlot = (e) => {
        const s = getSlot(e);
        return (typeof s === 'number' && Number.isFinite(s) && s > 0) ? s : null;
    };

    const waiting = [];
    const completed = [];
    let hasRunning = false;

    for (const e of entries) {
        if (e.status === 'running') {
            hasRunning = true;
        } else if (e.status === 'scheduled') {
            // A barred match is never played (the console skips it and the
            // operator records its fusensho), so it takes no place in the
            // pace: it keeps its plan and moves no other match.
            if (!e.barred) waiting.push(e);
        } else {
            completed.push(e);
        }
    }

    waiting.sort(compareCourtQueue);
    completed.sort(compareCourtQueue);

    // Completed drift:
    // The LAST completed match on the court by court-queue keys (scheduledAt, competition start time,
    // competition id, draw index; NOT newest modifiedAt): finish stamp minus planned end.
    // A match closed before its own planned start (a fusensho or kiken recorded
    // ahead of its slot) says nothing about the court's pace, so it is skipped.
    let completedDrift = null;
    const validCompleted = completed.filter(e => {
        const plannedStart = paceClockMinutes(e.scheduledAt);
        if (plannedStart === null || validSlot(e) === null) return false;
        const finishMinutes = stampMinutesOfDay(e.match?.modifiedAt);
        return finishMinutes !== null && finishMinutes >= plannedStart;
    });
    if (validCompleted.length > 0) {
        const lastCompleted = validCompleted[validCompleted.length - 1];
        const plannedEnd = paceClockMinutes(lastCompleted.scheduledAt) + validSlot(lastCompleted);
        completedDrift = stampMinutesOfDay(lastCompleted.match?.modifiedAt) - plannedEnd;
    }

    // Idle drift:
    // ONLY when no bout is running on the court: now minus first waiting match scheduledAt,
    // counted only when positive.
    let idleDrift = null;
    if (!hasRunning && typeof now === 'number' && Number.isFinite(now)) {
        const firstWaitingWithTime = waiting.find(e => paceClockMinutes(e.scheduledAt) !== null);
        if (firstWaitingWithTime) {
            const schedMin = paceClockMinutes(firstWaitingWithTime.scheduledAt);
            const diff = now - schedMin;
            if (diff > 0) {
                idleDrift = diff;
            }
        }
    }

    // D is the larger of the two that exist; with neither, 0.
    const known = [completedDrift, idleDrift].filter(v => v !== null);
    const drift = known.length > 0 ? Math.max(...known) : 0;

    // Running bout expected end: scheduledAt + slot + D.
    // With several running bouts, use the latest expected end.
    let latestRunningEnd = null;
    for (const e of entries) {
        if (e.status === 'running') {
            const sched = paceClockMinutes(e.scheduledAt);
            const slot = validSlot(e);
            if (sched !== null && slot !== null) {
                const exp = sched + slot + drift;
                if (latestRunningEnd === null || exp > latestRunningEnd) {
                    latestRunningEnd = exp;
                }
            }
        }
    }

    // Projected start of each waiting match = scheduledAt + D, clamped:
    // never before now;
    // never before a running bout's expected end;
    // never before the previous waiting match's projected start plus ONE slot (the previous match's own slot).
    let prevProjectedEnd = null;
    const projections = [];

    for (const e of waiting) {
        const sched = paceClockMinutes(e.scheduledAt);
        const slot = validSlot(e);

        if (sched === null || slot === null) {
            projections.push({
                entry: e,
                plannedMinutes: sched,
                projectedMinutes: null,
            });
            continue;
        }

        let projected = sched + drift;
        if (typeof now === 'number' && Number.isFinite(now)) {
            projected = Math.max(projected, now);
        }
        if (latestRunningEnd !== null) {
            projected = Math.max(projected, latestRunningEnd);
        }
        if (prevProjectedEnd !== null) {
            projected = Math.max(projected, prevProjectedEnd);
        }

        projections.push({
            entry: e,
            plannedMinutes: sched,
            projectedMinutes: projected,
        });

        prevProjectedEnd = projected + slot;
    }

    return {
        drift,
        completedDrift,
        idleDrift,
        projections,
    };
}

// courtPaceEntriesOf: maps an array of competitions to courtQueueEntriesOf entries stamped with slotMinutes (pool slot for pools, knockout slot for bracket).
export function courtPaceEntriesOf(competitions) {
    if (!competitions || !Array.isArray(competitions)) return [];
    const all = [];
    for (const comp of competitions) {
        const poolSlot = comp?.slotMinutes !== undefined
            ? comp.slotMinutes
            : comp?.config?.slotMinutes;
        const koSlot = comp?.knockoutSlotMinutes ?? comp?.config?.knockoutSlotMinutes ?? poolSlot;
        const poolLen = (comp?.poolMatches || []).length;
        const entries = courtQueueEntriesOf(comp);
        for (const e of entries) {
            const slot = e.drawIndex >= poolLen ? koSlot : poolSlot;
            all.push({ ...e, slotMinutes: slot });
        }
    }
    return all;
}

// courtPaceLookup: groups entries by courtQueueKey, projects court-days matching today or empty date,
// returns { byMatch, driftByCourtDay }: byMatch maps paceKey(competitionId, matchId) to
// { plannedMinutes, projectedMinutes, display } for every waiting match of a projected
// court-day, driftByCourtDay maps courtQueueKey(date, court) to that court-day's D.
export function courtPaceLookup(competitions, { now, today, fallbackSlot } = {}) {
    const byMatch = new Map();
    const driftByCourtDay = new Map();
    // The first waiting match's display per court-day, null when it cannot be
    // projected (the organiser's Court pace panel reads it).
    const nextByCourtDay = new Map();

    const groups = new Map();
    for (const e of courtPaceEntriesOf(competitions)) {
        const court = (e.court || "").trim();
        if (!court) continue;
        const key = courtQueueKey(e.date, court);
        let group = groups.get(key);
        if (!group) {
            group = [];
            groups.set(key, group);
        }
        group.push(e);
    }

    // A competition with no slot of its own takes `fallbackSlot` when given.
    const slotOf = (e) => (typeof e.slotMinutes === 'number' && e.slotMinutes > 0) ? e.slotMinutes : fallbackSlot;

    // Another day's schedule is not today's pace: it keeps the plan.
    const todayStr = (today || "").trim();
    for (const [key, groupEntries] of groups.entries()) {
        const dateStr = (groupEntries[0].date || "").trim();
        if (dateStr !== "" && dateStr !== todayStr) continue;

        const pace = projectCourtPace(groupEntries, { now, slotOf });
        driftByCourtDay.set(key, pace.drift);

        const first = pace.projections[0];
        nextByCourtDay.set(key, first && typeof first.projectedMinutes === 'number'
            ? paceDisplay(first.entry.scheduledAt, first.projectedMinutes)
            : null);

        for (const p of pace.projections) {
            byMatch.set(paceKey(p.entry.competitionId, p.entry.matchId), {
                plannedMinutes: p.plannedMinutes,
                projectedMinutes: p.projectedMinutes,
                display: paceDisplay(p.entry.scheduledAt, p.projectedMinutes),
            });
        }
    }

    return { byMatch, driftByCourtDay, nextByCourtDay };
}

