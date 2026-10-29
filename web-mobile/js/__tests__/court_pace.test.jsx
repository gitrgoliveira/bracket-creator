import { describe, it, expect } from 'vitest';
import {
    PACE_SHOW_THRESHOLD_MINUTES,
    paceClockMinutes,
    formatPaceClock,
    stampMinutesOfDay,
    projectCourtPace,
    paceDisplay,
    paceKey,
    courtPaceEntriesOf,
    courtPaceLookup,
    flatMatchesToPaceEntries,
} from '../court_pace.jsx';

function localStamp(h, m, s = 0) {
    return new Date(2026, 9, 29, h, m, s).getTime();
}

function makeEntry({
    competitionId = 'c1',
    competitionStartTime = '09:00',
    date = '29-10-2026',
    court = 'A',
    matchId = 'm1',
    status = 'scheduled',
    scheduledAt = '10:00',
    drawIndex = 0,
    slotMinutes = 10,
    modifiedAt = 0,
    match = null,
} = {}) {
    const rawMatch = match || { id: matchId, status, scheduledAt, modifiedAt, court };
    return {
        competitionId,
        competitionStartTime,
        date,
        court,
        matchId,
        status,
        scheduledAt,
        drawIndex,
        slotMinutes,
        match: rawMatch,
        barred: false,
    };
}

describe('court_pace projection table tests', () => {
    it('late: last completed planned 10:00 (slot 10) finished 10:25, next waiting planned 10:10 -> D = 15, projected 10:25, display ~10:25 (aria about 10:25)', () => {
        const entries = [
            makeEntry({ matchId: 'm1', status: 'completed', scheduledAt: '10:00', slotMinutes: 10, modifiedAt: localStamp(10, 25), drawIndex: 0 }),
            makeEntry({ matchId: 'm2', status: 'scheduled', scheduledAt: '10:10', slotMinutes: 10, drawIndex: 1 }),
        ];
        const res = projectCourtPace(entries, { now: 10 * 60 + 25 });
        expect(res.completedDrift).toBe(15);
        expect(res.drift).toBe(15);
        expect(res.projections).toHaveLength(1);
        expect(res.projections[0].projectedMinutes).toBe(10 * 60 + 25);
        const disp = paceDisplay(entries[1].scheduledAt, res.projections[0].projectedMinutes);
        expect(disp).toEqual({
            text: '~10:25',
            ariaLabel: 'about 10:25',
            projected: true,
        });
    });

    it('early: last completed planned 10:00 finished 10:05 (D = -5), next waiting planned 10:30, now 10:06 -> projected 10:25, display ~10:25', () => {
        const entries = [
            makeEntry({ matchId: 'm1', status: 'completed', scheduledAt: '10:00', slotMinutes: 10, modifiedAt: localStamp(10, 5), drawIndex: 0 }),
            makeEntry({ matchId: 'm2', status: 'scheduled', scheduledAt: '10:30', slotMinutes: 10, drawIndex: 1 }),
        ];
        const res = projectCourtPace(entries, { now: 10 * 60 + 6 });
        expect(res.completedDrift).toBe(-5);
        expect(res.drift).toBe(-5);
        expect(res.projections[0].projectedMinutes).toBe(10 * 60 + 25);
        const disp = paceDisplay(entries[1].scheduledAt, res.projections[0].projectedMinutes);
        expect(disp).toEqual({
            text: '~10:25',
            ariaLabel: 'about 10:25',
            projected: true,
        });
    });

    it('idle: nothing completed, nothing running, first waiting planned 10:00, now 10:20 -> idle drift 20, projected 10:20, display ~10:20; second waiting planned 10:10 -> 10:30', () => {
        const entries = [
            makeEntry({ matchId: 'm1', status: 'scheduled', scheduledAt: '10:00', slotMinutes: 10, drawIndex: 0 }),
            makeEntry({ matchId: 'm2', status: 'scheduled', scheduledAt: '10:10', slotMinutes: 10, drawIndex: 1 }),
        ];
        const res = projectCourtPace(entries, { now: 10 * 60 + 20 });
        expect(res.idleDrift).toBe(20);
        expect(res.completedDrift).toBeNull();
        expect(res.drift).toBe(20);
        expect(res.projections[0].projectedMinutes).toBe(10 * 60 + 20);
        expect(res.projections[1].projectedMinutes).toBe(10 * 60 + 30);
        const disp1 = paceDisplay(entries[0].scheduledAt, res.projections[0].projectedMinutes);
        expect(disp1.text).toBe('~10:20');
        const disp2 = paceDisplay(entries[1].scheduledAt, res.projections[1].projectedMinutes);
        expect(disp2.text).toBe('~10:30');
    });

    it('idle after an on-time finish: last completed planned 10:00 finished 10:10 (completed drift 0), nothing running, next waiting planned 10:10, now 10:30 -> D = 20 (idle wins), projected 10:30', () => {
        const entries = [
            makeEntry({ matchId: 'm1', status: 'completed', scheduledAt: '10:00', slotMinutes: 10, modifiedAt: localStamp(10, 10), drawIndex: 0 }),
            makeEntry({ matchId: 'm2', status: 'scheduled', scheduledAt: '10:10', slotMinutes: 10, drawIndex: 1 }),
        ];
        const res = projectCourtPace(entries, { now: 10 * 60 + 30 });
        expect(res.completedDrift).toBe(0);
        expect(res.idleDrift).toBe(20);
        expect(res.drift).toBe(20);
        expect(res.projections[0].projectedMinutes).toBe(10 * 60 + 30);
        const disp = paceDisplay(entries[1].scheduledAt, res.projections[0].projectedMinutes);
        expect(disp.text).toBe('~10:30');
    });

    it('running bout: a running match planned 10:10 (slot 10), D = 0, next waiting planned 10:20, now 10:12 -> projected 10:20, display the plan 10:20 (projected false)', () => {
        const entries = [
            makeEntry({ matchId: 'm1', status: 'running', scheduledAt: '10:10', slotMinutes: 10, drawIndex: 0 }),
            makeEntry({ matchId: 'm2', status: 'scheduled', scheduledAt: '10:20', slotMinutes: 10, drawIndex: 1 }),
        ];
        const res = projectCourtPace(entries, { now: 10 * 60 + 12 });
        expect(res.drift).toBe(0);
        expect(res.idleDrift).toBeNull();
        expect(res.projections[0].projectedMinutes).toBe(10 * 60 + 20);
        const disp = paceDisplay(entries[1].scheduledAt, res.projections[0].projectedMinutes);
        expect(disp).toEqual({
            text: '10:20',
            ariaLabel: '10:20',
            projected: false,
        });
    });

    it('a bout running long: running planned 10:10, slot 10, D = 0, now 10:40 -> next waiting planned 10:20 projected 10:40 (never before now)', () => {
        const entries = [
            makeEntry({ matchId: 'm1', status: 'running', scheduledAt: '10:10', slotMinutes: 10, drawIndex: 0 }),
            makeEntry({ matchId: 'm2', status: 'scheduled', scheduledAt: '10:20', slotMinutes: 10, drawIndex: 1 }),
        ];
        const res = projectCourtPace(entries, { now: 10 * 60 + 40 });
        expect(res.drift).toBe(0);
        expect(res.projections[0].projectedMinutes).toBe(10 * 60 + 40);
        const disp = paceDisplay(entries[1].scheduledAt, res.projections[0].projectedMinutes);
        expect(disp.text).toBe('~10:40');
    });

    it('two competitions on one court: competition X (slot 10) and Y (slot 6) interleaved on one court; the last completed by queue keys is Y; waiting matches of both are chained in court-queue order with each previous match own slot; assert every projection', () => {
        const entries = [
            makeEntry({ competitionId: 'X', matchId: 'mX_comp', status: 'completed', scheduledAt: '09:00', slotMinutes: 10, drawIndex: 0 }),
            makeEntry({ competitionId: 'Y', matchId: 'mY_comp', status: 'completed', scheduledAt: '09:10', slotMinutes: 6, modifiedAt: localStamp(9, 20), drawIndex: 0 }),
            makeEntry({ competitionId: 'X', matchId: 'mX_wait1', status: 'scheduled', scheduledAt: '09:16', slotMinutes: 10, drawIndex: 1 }),
            makeEntry({ competitionId: 'Y', matchId: 'mY_wait1', status: 'scheduled', scheduledAt: '09:26', slotMinutes: 6, drawIndex: 1 }),
            makeEntry({ competitionId: 'X', matchId: 'mX_wait2', status: 'scheduled', scheduledAt: '09:32', slotMinutes: 10, drawIndex: 2 }),
        ];
        // Given in reverse: the projection orders the court itself, across competitions.
        const res = projectCourtPace([...entries].reverse(), { now: 9 * 60 });
        expect(res.drift).toBe(4);
        expect(res.projections.map(p => p.entry.matchId)).toEqual(['mX_wait1', 'mY_wait1', 'mX_wait2']);
        expect(res.projections).toHaveLength(3);
        // mX_wait1: planned 09:16 (556) + 4 = 560 (09:20). Slot is 10, chaining bound = 570.
        expect(res.projections[0].projectedMinutes).toBe(9 * 60 + 20);
        // mY_wait1: planned 09:26 (566) + 4 = 570 (09:30). Clamped to 570. Slot is 6, chaining bound = 576.
        expect(res.projections[1].projectedMinutes).toBe(9 * 60 + 30);
        // mX_wait2: planned 09:32 (572) + 4 = 576 (09:36). Clamped to 576.
        expect(res.projections[2].projectedMinutes).toBe(9 * 60 + 36);
    });

    it('nothing completed: no completed, a running bout or nothing, now before the first plan -> D = 0, everything shows the plan', () => {
        const entries = [
            makeEntry({ matchId: 'm1', status: 'running', scheduledAt: '10:00', slotMinutes: 10, drawIndex: 0 }),
            makeEntry({ matchId: 'm2', status: 'scheduled', scheduledAt: '10:10', slotMinutes: 10, drawIndex: 1 }),
        ];
        const res = projectCourtPace(entries, { now: 9 * 60 + 50 });
        expect(res.drift).toBe(0);
        expect(res.projections[0].projectedMinutes).toBe(10 * 60 + 10);
        const disp = paceDisplay(entries[1].scheduledAt, res.projections[0].projectedMinutes);
        expect(disp).toEqual({
            text: '10:10',
            ariaLabel: '10:10',
            projected: false,
        });
    });

    it('below the threshold: D = 4 -> projected differs by 4 -> display shows the plan (projected false); D = 5 -> ~HH:MM shown', () => {
        const d4 = paceDisplay('10:00', 10 * 60 + 4);
        expect(d4).toEqual({
            text: '10:00',
            ariaLabel: '10:00',
            projected: false,
        });

        const d5 = paceDisplay('10:00', 10 * 60 + 5);
        expect(d5).toEqual({
            text: '~10:05',
            ariaLabel: 'about 10:05',
            projected: true,
        });
    });

    it('a correction of an early match: the court early completed match (planned 09:00) was corrected later so its modifiedAt is the newest stamp (e.g. 10:50); the last completed by queue keys (planned 10:00, finished 10:15) still decides D = 5, not the corrected one', () => {
        const entries = [
            makeEntry({ matchId: 'm1', status: 'completed', scheduledAt: '09:00', slotMinutes: 10, modifiedAt: localStamp(10, 50), drawIndex: 0 }),
            makeEntry({ matchId: 'm2', status: 'completed', scheduledAt: '10:00', slotMinutes: 10, modifiedAt: localStamp(10, 15), drawIndex: 1 }),
            makeEntry({ matchId: 'm3', status: 'scheduled', scheduledAt: '10:10', slotMinutes: 10, drawIndex: 2 }),
        ];
        const res = projectCourtPace(entries, { now: 10 * 60 + 15 });
        expect(res.completedDrift).toBe(5);
        expect(res.drift).toBe(5);
        expect(res.projections[0].projectedMinutes).toBe(10 * 60 + 15);
    });

    it('a correction of the last one: the last completed (planned 10:00) re-stamped at 10:40 by a correction -> D moves to 30 (this is a known limit, pin it)', () => {
        const entries = [
            makeEntry({ matchId: 'm1', status: 'completed', scheduledAt: '10:00', slotMinutes: 10, modifiedAt: localStamp(10, 40), drawIndex: 0 }),
            makeEntry({ matchId: 'm2', status: 'scheduled', scheduledAt: '10:10', slotMinutes: 10, drawIndex: 1 }),
        ];
        const res = projectCourtPace(entries, { now: 10 * 60 + 40 });
        expect(res.completedDrift).toBe(30);
        expect(res.drift).toBe(30);
        expect(res.projections[0].projectedMinutes).toBe(10 * 60 + 40);
    });
});

describe('court_pace helper small cases', () => {
    it('paceDisplay with null projection and empty scheduledAt', () => {
        expect(paceDisplay('', null)).toEqual({ text: '', ariaLabel: '', projected: false });
        expect(paceDisplay('10:00', null)).toEqual({ text: '10:00', ariaLabel: '10:00', projected: false });
    });

    it('formatPaceClock zero padding', () => {
        expect(formatPaceClock(5)).toBe('00:05');
        expect(formatPaceClock(605)).toBe('10:05');
        expect(formatPaceClock(1445)).toBe('00:05');
    });

    it('courtPaceLookup skips a court-day whose date is not today and projects one whose date is today', () => {
        const compToday = {
            id: 'comp1',
            date: '29-10-2026',
            slotMinutes: 10,
            poolMatches: [
                { id: 'p1', court: 'A', status: 'scheduled', scheduledAt: '10:00' }
            ],
        };
        const compTomorrow = {
            id: 'comp2',
            date: '30-10-2026',
            slotMinutes: 10,
            poolMatches: [
                { id: 'p2', court: 'A', status: 'scheduled', scheduledAt: '10:00' }
            ],
        };
        const lookup = courtPaceLookup([compToday, compTomorrow], { now: 10 * 60, today: '29-10-2026' });
        expect(lookup.byMatch.has(paceKey('comp1', 'p1'))).toBe(true);
        expect(lookup.byMatch.has(paceKey('comp2', 'p2'))).toBe(false);
        expect(lookup.driftByCourtDay.size).toBe(1);
    });

    it('flatMatchesToPaceEntries keeps the input order as drawIndex', () => {
        const raw = [
            { compId: 'c1', id: 'm1', status: 'scheduled', scheduledAt: '10:00', court: 'A' },
            { compId: 'c1', id: 'm2', status: 'completed', scheduledAt: '09:00', court: 'A' },
            { compId: 'c2', id: 'm3', status: 'running', scheduledAt: '09:30', court: 'A' },
        ];
        const entries = flatMatchesToPaceEntries(raw);
        expect(entries[0].drawIndex).toBe(0);
        expect(entries[1].drawIndex).toBe(1);
        expect(entries[2].drawIndex).toBe(2);
        expect(entries[0].competitionId).toBe('c1');
        expect(entries[0].matchId).toBe('m1');
    });

    it('a waiting match with no scheduledAt gets null and does not break the chain', () => {
        const entries = [
            makeEntry({ matchId: 'm1', status: 'scheduled', scheduledAt: '10:00', slotMinutes: 10, drawIndex: 0 }),
            makeEntry({ matchId: 'm2', status: 'scheduled', scheduledAt: '', slotMinutes: 10, drawIndex: 1 }),
            makeEntry({ matchId: 'm3', status: 'scheduled', scheduledAt: '10:10', slotMinutes: 10, drawIndex: 2 }),
        ];
        const res = projectCourtPace(entries, { now: 10 * 60 });
        // A match with no time sorts last in the court queue (queueSortTime).
        const byId = Object.fromEntries(res.projections.map(p => [p.entry.matchId, p.projectedMinutes]));
        expect(res.projections.map(p => p.entry.matchId)).toEqual(['m1', 'm3', 'm2']);
        expect(byId.m1).toBe(10 * 60);
        expect(byId.m3).toBe(10 * 60 + 10);
        expect(byId.m2).toBeNull();
    });
});
