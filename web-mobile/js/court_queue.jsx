import { isBarredMatch } from './ineligible_match.jsx';

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

    const bracketMatches = [];
    if (bracket) {
        const rounds = bracket.rounds || [];
        if (rounds.length === 0) {
            if (bracket.thirdPlaceMatch) {
                bracketMatches.push(bracket.thirdPlaceMatch);
            }
        } else {
            const finalRoundIdx = rounds.length - 1;
            for (let r = 0; r < finalRoundIdx; r++) {
                const round = rounds[r] || [];
                for (let i = 0; i < round.length; i++) {
                    bracketMatches.push(round[i]);
                }
            }
            if (bracket.thirdPlaceMatch) {
                bracketMatches.push(bracket.thirdPlaceMatch);
            }
            const finalRound = rounds[finalRoundIdx] || [];
            for (let i = 0; i < finalRound.length; i++) {
                bracketMatches.push(finalRound[i]);
            }
        }
    }

    const total = poolMatches.length + bracketMatches.length;
    if (total === 0) return [];

    const entries = [];
    let drawIndex = 0;

    for (let i = 0; i < poolMatches.length; i++) {
        const m = poolMatches[i];
        entries.push({
            competitionId: compId,
            competitionStartTime: compStartTime,
            date: compDate,
            court: m.court || "",
            matchId: m.id || m.ID || "",
            status: m.status || "",
            scheduledAt: m.scheduledAt || "",
            drawIndex: drawIndex++,
            barred: m.status === 'scheduled' && isBarredMatch(m),
            match: m,
        });
    }

    for (let i = 0; i < bracketMatches.length; i++) {
        const bm = bracketMatches[i];
        entries.push({
            competitionId: compId,
            competitionStartTime: compStartTime,
            date: compDate,
            court: bm.court || "",
            matchId: bm.id || bm.ID || "",
            status: bm.status || "",
            scheduledAt: bm.scheduledAt || "",
            drawIndex: drawIndex++,
            barred: bm.status === 'scheduled' && isBarredMatch(bm),
            match: bm,
        });
    }

    return entries;
}

export function withCourtQueuePositions(competitions) {
    if (!competitions || competitions.length === 0) return competitions;

    const compEntries = competitions.map(c => courtQueueEntriesOf(c));
    const allEntries = [];
    for (let c = 0; c < compEntries.length; c++) {
        const entries = compEntries[c];
        for (let i = 0; i < entries.length; i++) {
            allEntries.push(entries[i]);
        }
    }

    const allPositions = courtQueuePositions(allEntries);
    const posByMatch = new Map();
    for (let i = 0; i < allEntries.length; i++) {
        posByMatch.set(allEntries[i].match, allPositions[i]);
    }

    let anyCompChanged = false;
    const nextCompetitions = competitions.map(comp => {
        let compChanged = false;

        let nextPoolMatches = comp.poolMatches;
        if (comp.poolMatches) {
            let pmChanged = false;
            nextPoolMatches = comp.poolMatches.map(m => {
                const newPos = posByMatch.get(m) || 0;
                if ((m.queuePosition || 0) !== newPos) {
                    pmChanged = true;
                    return { ...m, queuePosition: newPos };
                }
                return m;
            });
            if (pmChanged) {
                compChanged = true;
            } else {
                nextPoolMatches = comp.poolMatches;
            }
        }

        let nextBracket = comp.bracket;
        if (comp.bracket) {
            let brChanged = false;
            let nextRounds = comp.bracket.rounds;
            if (comp.bracket.rounds) {
                let roundsChanged = false;
                nextRounds = comp.bracket.rounds.map(round => {
                    let rChanged = false;
                    const nextRound = round.map(m => {
                        const newPos = posByMatch.get(m) || 0;
                        if ((m.queuePosition || 0) !== newPos) {
                            rChanged = true;
                            return { ...m, queuePosition: newPos };
                        }
                        return m;
                    });
                    if (rChanged) roundsChanged = true;
                    return rChanged ? nextRound : round;
                });
                if (roundsChanged) {
                    brChanged = true;
                } else {
                    nextRounds = comp.bracket.rounds;
                }
            }

            let nextBronze = comp.bracket.thirdPlaceMatch;
            if (comp.bracket.thirdPlaceMatch) {
                const bm = comp.bracket.thirdPlaceMatch;
                const newPos = posByMatch.get(bm) || 0;
                if ((bm.queuePosition || 0) !== newPos) {
                    nextBronze = { ...bm, queuePosition: newPos };
                    brChanged = true;
                }
            }

            if (brChanged) {
                compChanged = true;
                nextBracket = { ...comp.bracket, rounds: nextRounds };
                if (nextBronze !== comp.bracket.thirdPlaceMatch) {
                    nextBracket.thirdPlaceMatch = nextBronze;
                }
            }
        }

        if (compChanged) {
            anyCompChanged = true;
            return {
                ...comp,
                poolMatches: nextPoolMatches,
                bracket: nextBracket,
            };
        }
        return comp;
    });

    return anyCompChanged ? nextCompetitions : competitions;
}

// orderOnCourt orders running or scheduled bouts across competitions for a court letter.
// Ordering by date first keeps one tournament day unchanged and puts a reused letter on another day after it.
export function orderOnCourt(competitions, court) {
    if (!competitions || competitions.length === 0) return [];
    const entries = [];

    for (let c = 0; c < competitions.length; c++) {
        const comp = competitions[c];
        const compEntries = courtQueueEntriesOf(comp);
        for (let i = 0; i < compEntries.length; i++) {
            const e = compEntries[i];
            if (e.court === court && (e.status === 'running' || e.status === 'scheduled')) {
                entries.push({
                    ...e,
                    comp,
                });
            }
        }
    }

    entries.sort((a, b) => {
        const da = (a.date || "").trim();
        const db = (b.date || "").trim();
        if (da !== db) {
            if (da === "") return -1;
            if (db === "") return 1;
            return da < db ? -1 : 1;
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
