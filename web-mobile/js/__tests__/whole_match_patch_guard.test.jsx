// bc-sseq FR6: a refused write's answer carries the match as stored, and the
// outbox shows it as a WHOLE-match local patch (`whole: true`). applyPatch
// takes such a patch only when it is not older than the copy held: by the
// server's scoreVersion when both carry one, else by modifiedAt. An answer
// read before a newer push it lands after must never put the older match
// back. The court and time patches the outbox has always sent carry a field
// or two and no version, and apply exactly as before.
import { describe, it, expect } from 'vitest';
import { applyPatch } from '../patch.jsx';

const comp = (m) => ({ id: 'c1', poolMatches: [{ id: 'm1', court: 'A', scheduledAt: '09:00', ...m }] });
const whole = (result) => ({ type: 'match_updated', local: true, whole: true, data: { competitionId: 'c1', matchId: 'm1', result: { id: 'm1', ...result } } });
const field = (result) => ({ type: 'match_updated', local: true, data: { competitionId: 'c1', matchId: 'm1', result: { id: 'm1', ...result } } });

describe('a whole-match local patch never replaces a newer copy held', () => {
    it('is applied over an older copy, by scoreVersion', () => {
        const held = comp({ status: 'running', scoreVersion: 4, modifiedAt: 900 });
        const next = applyPatch(held, whole({ status: 'completed', winner: 'B', scoreVersion: 5, modifiedAt: 500 }));
        expect(next.poolMatches[0]).toMatchObject({ status: 'completed', scoreVersion: 5 });
    });

    it('is not applied over a copy with a higher scoreVersion, whatever the stamps', () => {
        const held = comp({ status: 'completed', winner: 'A', scoreVersion: 6, modifiedAt: 100 });
        const next = applyPatch(held, whole({ status: 'completed', winner: 'B', scoreVersion: 5, modifiedAt: 500 }));
        expect(next, 'nothing changed').toBe(held);
    });

    it('is applied at an equal scoreVersion (the same copy, not an older one)', () => {
        const held = comp({ status: 'running', scoreVersion: 5, modifiedAt: 500 });
        const next = applyPatch(held, whole({ status: 'completed', winner: 'B', scoreVersion: 5, modifiedAt: 500 }));
        expect(next.poolMatches[0].status).toBe('completed');
    });

    it('orders equal scoreVersions by modifiedAt (a court or time move adds no score-log line)', () => {
        const held = comp({ status: 'scheduled', court: 'B', scoreVersion: 5, modifiedAt: 800 });
        expect(applyPatch(held, whole({ status: 'scheduled', court: 'A', scoreVersion: 5, modifiedAt: 500 })), 'older stamp: court kept').toBe(held);
        const newer = applyPatch(held, whole({ status: 'scheduled', court: 'A', scoreVersion: 5, modifiedAt: 900 }));
        expect(newer.poolMatches[0].court, 'newer stamp').toBe('A');
    });

    it('falls back to modifiedAt when either copy has no scoreVersion', () => {
        const held = comp({ status: 'completed', winner: 'A', modifiedAt: 800 });
        expect(applyPatch(held, whole({ status: 'completed', winner: 'B', scoreVersion: 5, modifiedAt: 500 })), 'older stamp').toBe(held);
        const newer = applyPatch(held, whole({ status: 'completed', winner: 'B', scoreVersion: 5, modifiedAt: 900 }));
        expect(newer.poolMatches[0].winner.name, 'newer stamp').toBe('B');
        const unstamped = applyPatch(comp({ status: 'running' }), whole({ status: 'completed', winner: 'B' }));
        expect(unstamped.poolMatches[0].status, 'nothing to order by: applied').toBe('completed');
    });

    it('a court or time patch (never whole) applies as before, over any copy', () => {
        const held = comp({ status: 'scheduled', scoreVersion: 9, modifiedAt: 999 });
        expect(applyPatch(held, field({ court: 'B' })).poolMatches[0].court).toBe('B');
        expect(applyPatch(held, field({ scheduledAt: '10:30' })).poolMatches[0].scheduledAt).toBe('10:30');
    });

    it('the same older copy sent as an ordinary push is not held back by this rule', () => {
        // The guard is the whole patch's alone: a server push keeps its own
        // rules (mergeMatchPatch, the push ledger).
        const held = comp({ status: 'completed', winner: 'A', scoreVersion: 6 });
        const push = { type: 'match_updated', data: { competitionId: 'c1', matchId: 'm1', result: { id: 'm1', status: 'completed', winner: 'B', scoreVersion: 5 } } };
        expect(applyPatch(held, push).poolMatches[0].winner.name).toBe('B');
    });
});
