// bc-evts stage 3 (S5): a live score never goes back in time, judged by the
// server's scoreVersion first. scoreVersion is the count of lines in the
// match's score log: it only grows, and two devices' clocks never enter it,
// so where both copies carry one it decides which is older; the stamp rule
// (isOlderRunningCopy's old test) stays for copies without one.
import { describe, it, expect } from 'vitest';
import { isOlderRunningCopy, keepNewerMatches } from '../patch.jsx';
import { mergeMatchPatch, olderByScoreVersion } from '../data.jsx';
import { normalizeMatch, toBackendMatchResult } from '../api_serializers.jsx';

const run = (over) => ({ id: 'm1', status: 'running', modifiedAt: 100, ...over });

describe('isOlderRunningCopy reads scoreVersion first', () => {
    it('a fetched copy with a lower scoreVersion is older even when its stamp reads newer', () => {
        const held = run({ modifiedAt: 100, scoreVersion: 5 });
        const fetched = run({ modifiedAt: 200, scoreVersion: 4 });
        expect(isOlderRunningCopy(fetched, held.status, held.modifiedAt, held.scoreVersion)).toBe(true);
    });
    it('a fetched copy with a higher scoreVersion is newer even when its stamp reads older', () => {
        const fetched = run({ modifiedAt: 50, scoreVersion: 6 });
        expect(isOlderRunningCopy(fetched, 'running', 100, 5)).toBe(false);
    });
    it('an equal scoreVersion is not older', () => {
        expect(isOlderRunningCopy(run({ modifiedAt: 50, scoreVersion: 5 }), 'running', 100, 5)).toBe(false);
    });
    it('without a version on either side the stamp rule stands', () => {
        expect(isOlderRunningCopy(run({ modifiedAt: 50 }), 'running', 100, 5)).toBe(true);
        expect(isOlderRunningCopy(run({ modifiedAt: 50, scoreVersion: 3 }), 'running', 100)).toBe(true);
        expect(isOlderRunningCopy(run({ modifiedAt: 150 }), 'running', 100)).toBe(false);
    });
    it('only running over running', () => {
        expect(isOlderRunningCopy(run({ status: 'completed', scoreVersion: 1 }), 'running', 100, 5)).toBe(false);
        expect(isOlderRunningCopy(run({ scoreVersion: 1 }), 'completed', 100, 5)).toBe(false);
    });
    it('keepNewerMatches keeps the held copy with the higher scoreVersion', () => {
        const held = { poolMatches: [run({ modifiedAt: 100, scoreVersion: 5, ipponsA: ['M'] })] };
        const fetched = { poolMatches: [run({ modifiedAt: 200, scoreVersion: 4, ipponsA: [] })] };
        expect(keepNewerMatches(held, fetched).poolMatches[0].ipponsA).toEqual(['M']);
    });
});

describe('mergeMatchPatch does not take an older push', () => {
    it('a running push with a lower scoreVersion leaves the held running match as it is', () => {
        const existing = run({ scoreVersion: 5, ipponsA: ['M'] });
        expect(mergeMatchPatch(existing, run({ scoreVersion: 4, ipponsA: [] }))).toBe(existing);
    });
    it('a newer push, a push with no version and a status change all merge', () => {
        const existing = run({ scoreVersion: 5, ipponsA: ['M'], court: 'A' });
        expect(mergeMatchPatch(existing, run({ scoreVersion: 6, ipponsA: ['M', 'K'] })).ipponsA).toEqual(['M', 'K']);
        expect(mergeMatchPatch(existing, { id: 'm1', ipponsA: [] }).ipponsA).toEqual([]);
        expect(mergeMatchPatch(existing, { id: 'm1', status: 'completed', scoreVersion: 4 }).status).toBe('completed');
    });
    it('olderByScoreVersion needs both versions', () => {
        expect(olderByScoreVersion({ scoreVersion: 4 }, { scoreVersion: 5 })).toBe(true);
        expect(olderByScoreVersion({ scoreVersion: 0 }, { scoreVersion: 5 })).toBe(null);
        expect(olderByScoreVersion({}, { scoreVersion: 5 })).toBe(null);
        expect(olderByScoreVersion({ scoreVersion: 6 }, { scoreVersion: 5 })).toBe(false);
    });
});

describe('scoreVersion and markIds are read, never sent', () => {
    it('normalizeMatch keeps them', () => {
        const m = normalizeMatch({ id: 'm1', sideA: 'A', sideB: 'B', status: 'running', ipponsA: ['M'], ipponsB: [], markIds: { a: ['s.1'], b: [] }, scoreVersion: 7 }, {});
        expect(m.scoreVersion).toBe(7);
        expect(m.markIds).toEqual({ a: ['s.1'], b: [] });
    });
    it('toBackendMatchResult sends neither, on the match or on a bout row', () => {
        const wire = toBackendMatchResult({
            status: 'running', ipponsA: ['M'], ipponsB: [], markIds: { a: ['s.1'], b: [] }, scoreVersion: 7,
            subResults: [{ position: 1, ipponsA: ['K'], ipponsB: [], markIds: { a: ['b:1:a:K:1'], b: [] } }],
        }, null);
        expect(wire.scoreVersion).toBeUndefined();
        expect(wire.markIds).toBeUndefined();
        expect(wire.subResults[0].markIds).toBeUndefined();
        expect(wire.subResults[0].ipponsA).toEqual(['K']);
    });
});
