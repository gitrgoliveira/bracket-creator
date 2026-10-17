// bc-evts stage 3, review round 2, R4: a court move and a time change are
// outbox entries, and what this device shows follows them at once, as a score
// write's does: the outbox tells its local listeners the new value in the tick
// the change is made (subscribeLocalMatchPatch, an SSE-shaped match_updated the
// hosts apply), a refetch made while the change is still queued shows it too
// (the fetched match carries the queued value), and a refusal puts back the
// value the match had before (`was`).
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

let mod;
let API;
let _lsStore;
let _orig;

beforeEach(async () => {
    vi.useFakeTimers();
    _orig = { fetch: global.fetch, ES: global.EventSource, ls: global.localStorage };
    global.EventSource = class FakeES { constructor() { this.close = () => {}; } };
    global.EventSource.OPEN = 1;
    _lsStore = {};
    global.localStorage = {
        getItem: (k) => (k in _lsStore ? _lsStore[k] : null),
        setItem: (k, v) => { _lsStore[k] = String(v); },
        removeItem: (k) => { delete _lsStore[k]; },
        clear: () => { _lsStore = {}; },
    };
    global.fetch = vi.fn(() => Promise.reject(new TypeError('offline')));
    vi.resetModules();
    mod = await import('../api_client.jsx');
    API = mod.API;
});

afterEach(() => {
    if (API && typeof API.clearQueue === 'function') API.clearQueue();
    vi.clearAllTimers();
    vi.useRealTimers();
    vi.resetModules();
    global.fetch = _orig.fetch;
    global.EventSource = _orig.ES;
    global.localStorage = _orig.ls;
});

async function flushMicrotasks() { for (let i = 0; i < 30; i++) await Promise.resolve(); }
async function tick(ms = 0) { await vi.advanceTimersByTimeAsync(ms); await flushMicrotasks(); }
const ok = (body) => Promise.resolve({ ok: true, status: 200, headers: { get: () => null }, json: async () => body, text: async () => JSON.stringify(body) });
const refused = (status, body) => Promise.resolve({ ok: false, status, headers: { get: () => null }, json: async () => body, text: async () => JSON.stringify(body) });
const patchOf = (e) => e && e.data && e.data.result;

describe('a court move and a time change show at once (R4)', () => {
    it('tells the local listeners the new court and time in the tick they are made, offline', () => {
        const seen = [];
        API.subscribeLocalMatchPatch((e) => seen.push(e));
        API.moveMatchCourt('c1', 'm1', 'B', 'pw', { was: { court: 'A' } });
        API.updateMatchTime('c1', 'm2', '10:30', 'pw', { was: { scheduledAt: '10:00' } });
        expect(seen.map((e) => [e.type, e.data.competitionId, patchOf(e)])).toEqual([
            ['match_updated', 'c1', { id: 'm1', court: 'B' }],
            ['match_updated', 'c1', { id: 'm2', scheduledAt: '10:30' }],
        ]);
    });

    it('a refetch made while the change is queued shows the queued value', async () => {
        API.moveMatchCourt('c1', 'Pool A-0', 'B', 'pw', { was: { court: 'A' } });
        global.fetch = vi.fn((url) => {
            if (String(url).includes('/api/viewer/competitions/c1')) {
                return ok({ config: { id: 'c1' }, poolMatches: [{ id: 'Pool A-0', court: 'A', status: 'scheduled' }, { id: 'Pool A-1', court: 'A', status: 'scheduled' }] });
            }
            return Promise.reject(new TypeError('offline'));
        });
        const detail = await API.fetchCompetitionDetails('c1');
        expect(detail.poolMatches.map((m) => [m.id, m.court])).toEqual([['Pool A-0', 'B'], ['Pool A-1', 'A']]);
    });

    it('a refusal puts back the value the match had', async () => {
        const seen = [];
        API.subscribeLocalMatchPatch((e) => seen.push(patchOf(e)));
        global.fetch = vi.fn((url) => (String(url).includes('/api/time')
            ? Promise.reject(new TypeError('clock'))
            : refused(404, { error: 'match nope not found in competition c1' })));
        const answer = await API.moveMatchCourt('c1', 'm1', 'B', 'pw', { was: { court: 'A' }, onRefusal: () => true });
        await tick(0);
        await answer.settled.catch(() => {});
        expect(seen).toEqual([{ id: 'm1', court: 'B' }, { id: 'm1', court: 'A' }]);
        expect(API.unsentWrites().total).toBe(0);
        // The outbox logs the refusal (a 404 is not retried, R3).
        const warned = vi.spyOn(console, 'warn');
        expect(warned).toHaveBeenCalledTimes(1);
        warned.mockClear();
    });
});

// Stage 3 review 8, L1: a court move discarded while its request is out puts
// back nothing until that request ends, and nothing when it ends kept.
describe('a court move discarded while its request is out (L1)', () => {
    let warned = null;
    beforeEach(() => { warned = vi.spyOn(console, 'warn').mockImplementation(() => {}); });
    afterEach(() => { warned.mockRestore(); });

    async function discardWhileOut() {
        const seen = [];
        API.subscribeLocalMatchPatch((e) => seen.push(patchOf(e)));
        const moved = await API.moveMatchCourt('c1', 'm1', 'B', 'pw', { was: { court: 'A' }, onRefusal: () => true });
        if (moved && moved.settled) moved.settled.catch(() => {});
        const state = { hang: false, release: null };
        global.fetch = vi.fn((url) => {
            if (String(url).includes('/api/time')) return Promise.reject(new TypeError('clock'));
            if (!state.hang) return refused(500, { error: 'internal error' });
            return new Promise((r) => { state.release = r; });
        });
        for (let i = 0; i < 20 && !API.heldWrites().some((h) => h.keepsFailing); i++) {
            window.dispatchEvent(new Event('online'));
            await tick(9000);
        }
        state.hang = true;
        for (let i = 0; i < 20 && !state.release; i++) await tick(3000);
        expect(state.release, 'the move is out again').toBeTruthy();
        const held = API.heldWrites().find((h) => h.keepsFailing);
        expect(API.discardHeldWrite(held.key), 'the discard is taken at once').toBeTruthy();
        await tick(10);
        return { seen, state };
    }

    it('a 2xx then keeps the court it moved to', async () => {
        const { seen, state } = await discardWhileOut();
        expect(seen.filter((p) => p && p.court === 'A'), 'nothing put back while the move is out').toEqual([]);
        state.release(ok({ id: 'm1', court: 'B' }));
        await tick(10);
        expect(seen.filter((p) => p && p.court === 'A'), 'the server kept the move').toEqual([]);
    });

    it('a failure then puts back the court the match had', async () => {
        const { seen, state } = await discardWhileOut();
        state.release(refused(500, { error: 'internal error' }));
        await tick(10);
        expect(seen.filter((p) => p && p.court === 'A'), 'put back once the request has failed').toHaveLength(1);
    });
});
