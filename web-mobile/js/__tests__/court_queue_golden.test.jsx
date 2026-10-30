import { describe, it, expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import {
    courtQueueKey,
    courtQueues,
    courtQueuePositions,
    courtQueueEntriesOf,
    withCourtQueuePositions,
    sharesDay,
    sharesCourtDay,
    queueMoveCourts,
    orderOnCourt,
    compareCourtDayQueue,
} from '../court_queue.jsx';

describe('court queue: golden table parity with internal/state/court_queue.go', () => {
    const table = JSON.parse(
        readFileSync(
            resolve(__dirname, '..', '..', '..', 'internal', 'state', 'testdata', 'court_queue.json'),
            'utf8'
        )
    );

    it('the shared golden table is present and non-empty', () => {
        expect(
            table.cases?.length,
            'internal/state/testdata/court_queue.json parsed to zero cases'
        ).toBeGreaterThan(0);
    });

    describe.each(table.cases)('$name', (tc) => {
        it('queues match golden table', () => {
            const actualQueues = courtQueues(tc.entries);

            const expectedQueueMap = {};
            for (const q of tc.queues) {
                const key = courtQueueKey(q.date, q.court);
                expectedQueueMap[key] = q.order;
            }

            const actualQueueMap = {};
            for (const [key, idxs] of actualQueues.entries()) {
                actualQueueMap[key] = idxs.map((idx) => `${tc.entries[idx].competitionId}/${tc.entries[idx].matchId}`);
            }

            expect(actualQueueMap).toEqual(expectedQueueMap);
        });

        it('positions match golden table', () => {
            const actualPositions = courtQueuePositions(tc.entries);
            expect(actualPositions.length).toBe(tc.entries.length);

            const actualPosMap = {};
            for (let i = 0; i < actualPositions.length; i++) {
                const name = `${tc.entries[i].competitionId}/${tc.entries[i].matchId}`;
                const expectedPos = tc.positions[name] || 0;
                expect(actualPositions[i]).toBe(expectedPos);
                if (actualPositions[i] !== 0) {
                    actualPosMap[name] = actualPositions[i];
                }
            }
            expect(actualPosMap).toEqual(tc.positions);
        });
    });

    describe('courtQueueEntriesOf draw index and barred status', () => {
        it('orders pool matches, then rounds except last, then bronze, then final', () => {
            const comp = {
                id: 'c1',
                startTime: '09:00',
                date: '2026-11-01',
                poolMatches: [
                    { id: 'p0', court: 'A', status: 'scheduled' },
                    { id: 'p1', court: 'A', status: 'scheduled', ineligibleSides: { a: 'kiken-injury' } },
                ],
                bracket: {
                    rounds: [
                        [
                            { id: 'r0m0', court: 'A', status: 'completed', ineligibleSides: { a: 'kiken-injury' } },
                            { id: 'r0m1', court: 'A', status: 'scheduled' },
                        ],
                        [
                            { id: 'r1m0', court: 'A', status: 'scheduled' },
                        ],
                    ],
                    thirdPlaceMatch: { id: 'bronze', court: 'A', status: 'scheduled' },
                },
            };

            const entries = courtQueueEntriesOf(comp);
            expect(entries).toHaveLength(6);

            const expected = [
                { matchId: 'p0', drawIndex: 0, barred: false },
                { matchId: 'p1', drawIndex: 1, barred: true },
                { matchId: 'r0m0', drawIndex: 2, barred: false },
                { matchId: 'r0m1', drawIndex: 3, barred: false },
                { matchId: 'bronze', drawIndex: 4, barred: false },
                { matchId: 'r1m0', drawIndex: 5, barred: false },
            ];

            for (let i = 0; i < expected.length; i++) {
                expect(entries[i].competitionId).toBe('c1');
                expect(entries[i].competitionStartTime).toBe('09:00');
                expect(entries[i].date).toBe('2026-11-01');
                expect(entries[i].matchId).toBe(expected[i].matchId);
                expect(entries[i].drawIndex).toBe(expected[i].drawIndex);
                expect(entries[i].barred).toBe(expected[i].barred);
            }
        });

        it('gives a competition still in setup no entries, as the server queue leaves it out', () => {
            const leftovers = {
                poolMatches: [{ id: 'p0', court: 'A', status: 'scheduled' }],
                bracket: { rounds: [[{ id: 'r0m0', court: 'A', status: 'scheduled' }]] },
            };
            expect(courtQueueEntriesOf({ id: 'c1', status: 'setup', ...leftovers })).toEqual([]);
            expect(courtQueueEntriesOf({ config: { id: 'c1', status: 'setup' }, ...leftovers })).toEqual([]);
            expect(courtQueueEntriesOf({ id: 'c1', status: 'pools', ...leftovers })).toHaveLength(2);
        });
    });

    describe('withCourtQueuePositions', () => {
        it('interleaves positions across competitions sharing a court', () => {
            const compA = {
                id: 'a',
                date: '2026-11-01',
                startTime: '09:00',
                poolMatches: [
                    { id: 'm1', court: 'A', status: 'scheduled', scheduledAt: '09:20' },
                ],
            };
            const compB = {
                id: 'b',
                date: '2026-11-01',
                startTime: '09:00',
                poolMatches: [
                    { id: 'm1', court: 'A', status: 'scheduled', scheduledAt: '09:10' },
                ],
            };

            const updated = withCourtQueuePositions([compA, compB]);
            expect(updated[0].poolMatches[0].queuePosition).toBe(2);
            expect(updated[1].poolMatches[0].queuePosition).toBe(1);
        });

        it('preserves identity when nothing changes', () => {
            const compA = {
                id: 'a',
                date: '2026-11-01',
                startTime: '09:00',
                poolMatches: [
                    { id: 'm1', court: 'A', status: 'scheduled', scheduledAt: '09:20', queuePosition: 2 },
                ],
            };
            const compB = {
                id: 'b',
                date: '2026-11-01',
                startTime: '09:00',
                poolMatches: [
                    { id: 'm1', court: 'A', status: 'scheduled', scheduledAt: '09:10', queuePosition: 1 },
                ],
            };

            const originalList = [compA, compB];
            const updated = withCourtQueuePositions(originalList);
            expect(updated).toBe(originalList);
            expect(updated[0]).toBe(compA);
            expect(updated[0].poolMatches[0]).toBe(compA.poolMatches[0]);
        });
    });

    describe('queueMoveCourts', () => {
        it('names courts only for a new match or a change of status, court or time', () => {
            const seen = new Map();
            const m = { id: 'Pool A-0', court: 'A', scheduledAt: '09:00', status: 'running' };
            expect(queueMoveCourts(seen, 'c2', [m])).toEqual(['A']);
            expect(queueMoveCourts(seen, 'c2', [{ ...m, ipponsA: ['M'] }])).toEqual([]);
            expect(queueMoveCourts(seen, 'c2', [{ ...m, status: 'completed' }])).toEqual(['A']);
            expect(queueMoveCourts(seen, 'c2', [{ ...m, status: 'completed', scheduledAt: '09:10' }])).toEqual(['A']);
            expect(queueMoveCourts(seen, 'c2', [{ ...m, status: 'completed', scheduledAt: '09:10', court: 'B' }]).sort()).toEqual(['A', 'B']);
        });

        it('keys matches by competition, since match ids repeat across competitions', () => {
            const seen = new Map();
            const m = { id: 'Pool A-0', court: 'A', scheduledAt: '09:00', status: 'running' };
            expect(queueMoveCourts(seen, 'c2', [m])).toEqual(['A']);
            expect(queueMoveCourts(seen, 'c3', [m])).toEqual(['A']);
            expect(queueMoveCourts(seen, 'c2', [m, null])).toEqual([]);
            expect(queueMoveCourts(seen, 'c2', undefined)).toEqual([]);
        });
    });

    describe('sharesCourtDay', () => {
        const comp = {
            id: 'c1',
            date: '2026-11-01',
            poolMatches: [{ court: 'A' }],
            bracket: {
                rounds: [[{ court: 'B' }]],
            },
        };

        it('returns true when court and day match', () => {
            expect(sharesCourtDay(comp, '2026-11-01', 'A')).toBe(true);
            expect(sharesCourtDay(comp, '2026-11-01', ['B'])).toBe(true);
        });

        it('returns false when court does not match', () => {
            expect(sharesCourtDay(comp, '2026-11-01', 'C')).toBe(false);
        });

        it('returns false when day does not match', () => {
            expect(sharesCourtDay(comp, '2026-11-02', 'A')).toBe(false);
        });

        it('matches blank dates as the same day', () => {
            const compBlank = {
                id: 'c2',
                date: '',
                poolMatches: [{ court: 'A' }],
            };
            expect(sharesCourtDay(compBlank, '', 'A')).toBe(true);
            expect(sharesCourtDay(compBlank, '   ', 'A')).toBe(true);
            expect(sharesCourtDay(compBlank, '2026-11-01', 'A')).toBe(false);
        });
    });

    describe('sharesDay', () => {
        const comp = {
            id: 'c1',
            date: '2026-11-01',
        };

        it('returns true when day matches', () => {
            expect(sharesDay(comp, '2026-11-01')).toBe(true);
            expect(sharesDay(comp, '  2026-11-01  ')).toBe(true);
        });

        it('returns false when day does not match', () => {
            expect(sharesDay(comp, '2026-11-02')).toBe(false);
        });

        it('matches blank dates as the same day', () => {
            const compBlank = { id: 'c2', date: '' };
            expect(sharesDay(compBlank, '')).toBe(true);
            expect(sharesDay(compBlank, '   ')).toBe(true);
            expect(sharesDay(compBlank, '2026-11-01')).toBe(false);
        });

        it('reads date from config.date if top-level date is not present', () => {
            const compConfig = { id: 'c3', config: { date: '01-06-2026' } };
            expect(sharesDay(compConfig, '01-06-2026')).toBe(true);
            expect(sharesDay(compConfig, '31-05-2026')).toBe(false);
        });

        it('returns false when comp is null or undefined', () => {
            expect(sharesDay(null, '2026-11-01')).toBe(false);
            expect(sharesDay(undefined, '2026-11-01')).toBe(false);
        });
    });
});

describe('compareCourtDayQueue', () => {
    it('a day-2 09:00 scheduled entry sorts after a day-1 10:00 scheduled entry, dates in DD-MM-YYYY, and an empty date sorts first', () => {
        const day1Late = { date: '31-05-2026', scheduledAt: '10:00', status: 'scheduled' };
        const day2Early = { date: '01-06-2026', scheduledAt: '09:00', status: 'scheduled' };
        const emptyDate = { date: '', scheduledAt: '12:00', status: 'scheduled' };

        expect(compareCourtDayQueue(day2Early, day1Late)).toBeGreaterThan(0);
        expect(compareCourtDayQueue(day1Late, day2Early)).toBeLessThan(0);

        expect(compareCourtDayQueue(emptyDate, day1Late)).toBeLessThan(0);
        expect(compareCourtDayQueue(emptyDate, day2Early)).toBeLessThan(0);
        expect(compareCourtDayQueue(day1Late, emptyDate)).toBeGreaterThan(0);
    });
});

describe('orderOnCourt day order', () => {
    it('orders 31-05-2026 before 01-06-2026 rather than lexical order', () => {
        const compMay = {
            id: 'comp-may',
            date: '31-05-2026',
            poolMatches: [
                { id: 'm-may', court: 'A', status: 'scheduled' },
            ],
        };
        const compJune = {
            id: 'comp-june',
            date: '01-06-2026',
            poolMatches: [
                { id: 'm-june', court: 'A', status: 'scheduled' },
            ],
        };

        const entries = orderOnCourt([compJune, compMay], 'A');
        expect(entries).toHaveLength(2);
        expect(entries[0].matchId).toBe('m-may');
        expect(entries[1].matchId).toBe('m-june');
    });

    it('orders across a year boundary: 31-12-2026 before 01-01-2027', () => {
        const comp2026 = {
            id: 'comp-2026',
            date: '31-12-2026',
            poolMatches: [
                { id: 'm-2026', court: 'A', status: 'scheduled' },
            ],
        };
        const comp2027 = {
            id: 'comp-2027',
            date: '01-01-2027',
            poolMatches: [
                { id: 'm-2027', court: 'A', status: 'scheduled' },
            ],
        };

        const entries = orderOnCourt([comp2027, comp2026], 'A');
        expect(entries).toHaveLength(2);
        expect(entries[0].matchId).toBe('m-2026');
        expect(entries[1].matchId).toBe('m-2027');
    });

    it('sorts an empty-date competition still first', () => {
        const compEmpty = {
            id: 'comp-empty',
            date: '',
            poolMatches: [
                { id: 'm-empty', court: 'A', status: 'scheduled' },
            ],
        };
        const compDated = {
            id: 'comp-dated',
            date: '31-05-2026',
            poolMatches: [
                { id: 'm-dated', court: 'A', status: 'scheduled' },
            ],
        };

        const entries = orderOnCourt([compDated, compEmpty], 'A');
        expect(entries).toHaveLength(2);
        expect(entries[0].matchId).toBe('m-empty');
        expect(entries[1].matchId).toBe('m-dated');
    });
});
