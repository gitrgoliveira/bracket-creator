import { isBarredMatch } from './ineligible_match.jsx';
import { compareDmy } from './admin_helpers.jsx';

// The JS mirror of internal/state/court_queue.go, pinned by internal/state/testdata/court_queue.json;
// change the rule there and here together, never at a call site.

export function queueSortTime(s) {
    return s ? s : "99:99";
}

export function compareCourtQueue(a, b) {
    const aRunning = a.status === 'running';
    const bRunning = b.status === 'running';
    if (aRunning !== bRunning) return aRunning ? -1 : 1;

    const ta = queueSortTime(a.scheduledAt);
    const tb = queueSortTime(b.scheduledAt);
    if (ta !== tb) return ta < tb ? -1 : 1;

    const ca = queueSortTime(a.competitionStartTime);
    const cb = queueSortTime(b.competitionStartTime);
    if (ca !== cb) return ca < cb ? -1 : 1;

    if (a.competitionId !== b.competitionId) {
        return a.competitionId < b.competitionId ? -1 : 1;
    }

    if (a.drawIndex !== b.drawIndex) {
        return a.drawIndex < b.drawIndex ? -1 : 1;
    }

    return 0;
}

export function courtQueueKey(date, court) {
    return `${(date || "").trim()}\u0000${court || ""}`;
}

export function courtQueues(entries) {
    const byCourt = new Map();
    if (!entries) return byCourt;

    for (let i = 0; i < entries.length; i++) {
        const e = entries[i];
        if (e.status !== 'running' && e.status !== 'scheduled') {
            continue;
        }
        const key = courtQueueKey(e.date, e.court);
        let bucket = byCourt.get(key);
        if (!bucket) {
            bucket = [];
            byCourt.set(key, bucket);
        }
        bucket.push(i);
    }

    for (const idxs of byCourt.values()) {
        idxs.sort((i, j) => {
            const cmp = compareCourtQueue(entries[i], entries[j]);
            if (cmp !== 0) return cmp;
            return i - j;
        });
    }

    return byCourt;
}

export function courtQueuePositions(entries) {
    if (!entries || entries.length === 0) return [];
    const positions = Array.from({ length: entries.length }, () => 0);
    const queues = courtQueues(entries);

    for (const queue of queues.values()) {
        let counter = 0;
        for (const idx of queue) {
            const e = entries[idx];
            if (e.status === 'scheduled' && !e.barred) {
                counter++;
                positions[idx] = counter;
            }
        }
    }

    return positions;
}

export function courtQueueEntriesOf(comp) {
    if (!comp) return [];
    const compId = comp.id || comp.config?.id || "";
    const compStartTime = comp.startTime || comp.config?.startTime || "";
    const compDate = comp.date || comp.config?.date || "";

    const poolMatches = comp.poolMatches || [];
    const bracket = comp.bracket;

    // Draw order: round by round, the bronze just before the final round.
    const bracketMatches = [];
    if (bracket) {
        const rounds = bracket.rounds || [];
        const finalRoundIdx = rounds.length - 1;
        for (let r = 0; r < finalRoundIdx; r++) {
            bracketMatches.push(...(rounds[r] || []));
        }
        if (bracket.thirdPlaceMatch) {
            bracketMatches.push(bracket.thirdPlaceMatch);
        }
        if (finalRoundIdx >= 0) {
            bracketMatches.push(...(rounds[finalRoundIdx] || []));
        }
    }

    return [...poolMatches, ...bracketMatches].map((m, drawIndex) => ({
        competitionId: compId,
        competitionStartTime: compStartTime,
        date: compDate,
        court: m.court || "",
        matchId: m.id || m.ID || "",
        status: m.status || "",
        scheduledAt: m.scheduledAt || "",
        drawIndex,
        barred: m.status === 'scheduled' && isBarredMatch(m),
        match: m,
    }));
}

export function withCourtQueuePositions(competitions) {
    if (!competitions || competitions.length === 0) return competitions;

    const allEntries = competitions.flatMap(c => courtQueueEntriesOf(c));
    const allPositions = courtQueuePositions(allEntries);
    const posByMatch = new Map();
    for (let i = 0; i < allEntries.length; i++) {
        posByMatch.set(allEntries[i].match, allPositions[i]);
    }

    // Identity is kept wherever nothing changes (matters for memoised components).
    const stampPos = (m) => {
        const newPos = posByMatch.get(m) || 0;
        return (m.queuePosition || 0) === newPos ? m : { ...m, queuePosition: newPos };
    };
    const mapKeep = (arr, fn) => {
        const next = arr.map(fn);
        return next.some((x, i) => x !== arr[i]) ? next : arr;
    };

    return mapKeep(competitions, comp => {
        const poolMatches = comp.poolMatches ? mapKeep(comp.poolMatches, stampPos) : comp.poolMatches;

        let bracket = comp.bracket;
        if (bracket) {
            const rounds = bracket.rounds ? mapKeep(bracket.rounds, round => mapKeep(round, stampPos)) : bracket.rounds;
            const bronze = bracket.thirdPlaceMatch ? stampPos(bracket.thirdPlaceMatch) : bracket.thirdPlaceMatch;
            if (rounds !== bracket.rounds || bronze !== bracket.thirdPlaceMatch) {
                bracket = { ...bracket, rounds };
                if (bronze !== comp.bracket.thirdPlaceMatch) bracket.thirdPlaceMatch = bronze;
            }
        }

        if (poolMatches === comp.poolMatches && bracket === comp.bracket) return comp;
        return { ...comp, poolMatches, bracket };
    });
}

// orderOnCourt orders running or scheduled bouts across competitions for a court letter.
// Ordering by date first keeps one tournament day unchanged and puts a reused letter on another day after it.
export function orderOnCourt(competitions, court) {
    if (!competitions || competitions.length === 0) return [];
    const entries = [];

    for (const comp of competitions) {
        for (const e of courtQueueEntriesOf(comp)) {
            if (e.court === court && (e.status === 'running' || e.status === 'scheduled')) {
                entries.push({ ...e, comp });
            }
        }
    }

    entries.sort((a, b) => {
        const da = (a.date || "").trim();
        const db = (b.date || "").trim();
        if (da !== db) {
            if (da === "") return -1;
            if (db === "") return 1;
            return compareDmy(da, db);
        }
        return compareCourtQueue(a, b);
    });

    return entries;
}

export function sharesCourtDay(comp, otherDate, courts) {
    if (!comp) return false;
    const compDate = (comp.date || comp.config?.date || "").trim();
    const targetDate = (otherDate || "").trim();
    if (compDate !== targetDate) return false;

    const courtList = Array.isArray(courts) ? courts : [courts];
    if (courtList.length === 0) return false;

    const compCourts = new Set();
    if (comp.poolMatches) {
        for (const m of comp.poolMatches) {
            if (m.court) compCourts.add(m.court);
        }
    }
    if (comp.bracket) {
        if (comp.bracket.rounds) {
            for (const round of comp.bracket.rounds) {
                for (const m of round) {
                    if (m.court) compCourts.add(m.court);
                }
            }
        }
        if (comp.bracket.thirdPlaceMatch?.court) {
            compCourts.add(comp.bracket.thirdPlaceMatch.court);
        }
    }

    for (const c of courtList) {
        if (c && compCourts.has(c)) return true;
    }
    return false;
}
