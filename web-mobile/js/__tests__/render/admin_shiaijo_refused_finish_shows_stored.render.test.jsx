// bc-sseq FR6 on the court console: the console finished a bout this device
// thought was running, but another device had already finished it, and the
// server holds the console's Finish (`applied: false`) with the match as
// stored. The outbox shows that stored match first (a whole-match local
// patch, subscribeLocalMatchPatch) and only then lets the Finish go, which
// ends the console's own status move (API.ownStatusMove). Between the two the
// console must never show the bout running again: with the patch applied
// first, the feed already holds the finished match when the move ends.
import { act } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { setupShiaijoConsole, probe, outbox, makeMinimalTournament, renderPage } from '../helpers/shiaijo_console.js';

setupShiaijoConsole();

const side = (id, name) => ({ id, name });
const match = (o = {}) => ({
    id: 'm1', status: 'running', phase: 'pool', poolName: 'Pool A', court: 'A', scheduledAt: '09:00',
    sideA: side('p1', 'Yamada'), sideB: side('p2', 'Tanaka'), ipponsA: [], ipponsB: [], modifiedAt: 100, scoreVersion: 3, ...o,
});

describe('a refused Finish shows the stored match on the court console', () => {
    it('never renders the bout running between the stored match and the end of the Finish', async () => {
        const comp = { id: 'c1', name: 'Cup', status: 'pools', poolMatches: [match()] };
        let localPatch = null;
        const feed = { m1: null };
        const prev = {
            fetch: window.API.fetchCourtMatches, sub: window.API.subscribeToEvents, local: window.API.subscribeLocalMatchPatch,
            tm: window.tournamentMatches, fbc: window.filterMatchesByCourt,
        };
        window.API.fetchCourtMatches = vi.fn().mockResolvedValue([comp]);
        window.API.subscribeToEvents = () => () => {};
        window.API.subscribeLocalMatchPatch = (cb) => { localPatch = cb; return () => { localPatch = null; }; };
        window.tournamentMatches = (t) => {
            const out = (t.competitions || []).flatMap((c) => (c.poolMatches || []).map((m) => ({ ...m, compId: c.id, compName: c.name })));
            feed.m1 = out.find((m) => m.id === 'm1') || feed.m1;
            return out;
        };
        window.filterMatchesByCourt = (matches) => matches;
        // Every render of the score panel, by the match it shows.
        const shown = [];
        let latest = null;
        Object.defineProperty(probe, 'props', {
            configurable: true,
            get: () => latest,
            set: (v) => { latest = v; shown.push(v && v.match ? `${v.match.id}:${v.match.status}` : '-'); },
        });
        try {
            await act(async () => { renderPage(makeMinimalTournament(), 'A'); });
            await act(async () => { await Promise.resolve(); });
            expect(probe.props.match, 'the bout is live on the panel').toMatchObject({ id: 'm1', status: 'running' });
            expect(localPatch, 'the console listens to the outbox\'s local patches').toBeTypeOf('function');

            // The Finish is queued: the console shows the bout finished.
            let answer;
            const settled = new Promise((resolve) => { answer = resolve; });
            await act(async () => {
                outbox.hold('c1', 'm1', { status: 'completed', kind: 'score', payload: { status: 'completed', winner: 'p1' } }, { pending: true, settled });
            });
            shown.length = 0;

            // The answer comes: the stored match first (another device's
            // finish, Tanaka winning) ...
            const stored = match({ status: 'completed', winner: 'p2', winnerId: 'p2', ipponsB: ['M', 'K'], modifiedAt: 500, scoreVersion: 7 });
            await act(async () => {
                localPatch({ type: 'match_updated', local: true, whole: true, data: { competitionId: 'c1', matchId: 'm1', result: stored } });
            });
            expect(feed.m1, 'the feed holds the stored match').toMatchObject({ status: 'completed', scoreVersion: 7, ipponsB: ['M', 'K'] });
            // ... then the Finish leaves the queue, refused.
            await act(async () => {
                answer({ applied: false, reason: 'superseded', heldReason: 'match_closed', match: stored });
                await settled;
                await Promise.resolve();
            });
            expect(window.API.ownStatusMove('c1', 'm1'), 'the own move ended').toBeNull();
            expect(shown.filter((s) => s === 'm1:running'), 'no render showed the bout running').toEqual([]);
            expect(feed.m1).toMatchObject({ status: 'completed', ipponsB: ['M', 'K'] });
        } finally {
            Object.defineProperty(probe, 'props', { configurable: true, writable: true, value: latest });
            outbox.clear();
            window.API.fetchCourtMatches = prev.fetch;
            window.API.subscribeToEvents = prev.sub;
            window.API.subscribeLocalMatchPatch = prev.local;
            window.tournamentMatches = prev.tm;
            window.filterMatchesByCourt = prev.fbc;
        }
    });
});
