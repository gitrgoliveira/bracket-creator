// Stage 6 review 8 (M4, the screen rule): the status moves this device made
// (a send back to the queue, a finish, a decision) have ONE owner,
// API.ownStatusMove, read from the outbox itself: the court console shows them
// from there, so a reload or the console mounted again still shows what the
// operator did while the outbox holds it. A refused or discarded move leaves
// the outbox with nothing kept; one the server took is kept, with its stamp,
// until the feed shows it.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

let API;
let _lsStore;
let _orig;

async function load() {
    vi.resetModules();
    const mod = await import('../api_client.jsx');
    API = mod.API;
}

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
    await load();
});

afterEach(() => {
    vi.useRealTimers();
    vi.resetModules();
    global.fetch = _orig.fetch;
    global.EventSource = _orig.ES;
    global.localStorage = _orig.ls;
});

async function flushMicrotasks() { for (let i = 0; i < 30; i++) await Promise.resolve(); }
async function tick(ms = 0) { await vi.advanceTimersByTimeAsync(ms); await flushMicrotasks(); }
const isClock = (url) => String(url).includes('/api/time');
function mockFetch(impl) {
    global.fetch = vi.fn((url, opts) => (isClock(url) ? Promise.reject(new TypeError('clock')) : impl(url, opts)));
}
const finish = { status: 'completed', winner: 'A', ipponsA: ['M', 'M'], ipponsB: [] };
const shown = { id: 'm1', sideA: 'A', sideB: 'B', modifiedAt: 0 };

describe('the status moves this device made (API.ownStatusMove)', () => {
    it('a send back held offline: queued, and still after a reload', async () => {
        await API.revertMatchToQueue('c1', 'm1', 'pw');
        expect(API.ownStatusMove('c1', 'm1')).toMatchObject({ status: 'scheduled', kind: 'revert', landed: false });
        expect(API.ownStatusMove('c1', 'm2')).toBeNull();
        await load(); // a reload: the next page reads the outbox as stored
        expect(API.ownStatusMove('c1', 'm1')).toMatchObject({ status: 'scheduled', landed: false });
    });

    it('a finish held offline: completed, carrying what it sends; a decision too', async () => {
        await API.recordScore('c1', 'm1', finish, 'pw', shown);
        const mv = API.ownStatusMove('c1', 'm1');
        expect(mv).toMatchObject({ status: 'completed', kind: 'score', landed: false });
        expect(mv.payload.winner).toBe('A');
        await API.recordDecision('c1', 'm2', { decision: 'kiken', decisionBy: 'shiro' }, 'pw');
        expect(API.ownStatusMove('c1', 'm2')).toMatchObject({ status: 'completed', kind: 'decision', payload: { decisionBy: 'shiro' } });
    });

    it('a start queued after a send back ends it: the last move in the match order counts', async () => {
        await API.revertMatchToQueue('c1', 'm1', 'pw');
        await API.recordScore('c1', 'm1', { status: 'running', startOnly: true, ipponsA: [], ipponsB: [] }, 'pw', shown);
        expect(API.ownStatusMove('c1', 'm1')).toBeNull();
    });

    it('answered: kept with the stamp the server gave it, until forgotten', async () => {
        mockFetch(() => Promise.resolve({ ok: true, status: 200, json: async () => ({ ...finish, id: 'm1', modifiedAt: 777 }) }));
        const told = vi.fn();
        const off = API.subscribeOwnStatusMoves(told);
        await API.recordScore('c1', 'm1', finish, 'pw', shown);
        await tick(0);
        expect(API.unsentWrites().total).toBe(0);
        expect(API.ownStatusMove('c1', 'm1')).toMatchObject({ status: 'completed', landed: true, stamp: 777 });
        expect(told).toHaveBeenCalled();
        API.forgetOwnStatusMove('c1', 'm1');
        expect(API.ownStatusMove('c1', 'm1')).toBeNull();
        off();
    });

    // The send back door answers a bare 200, which names neither a stamp nor a
    // score version: the move keeps its own stamp and no version, so it goes on
    // holding over the feed's copy from before it (running, at the score
    // version the bout had) until the feed shows the bout queued. Read as
    // version 0, every running copy was past it, and the console showed the
    // bout it had just sent back as running beside the one it started, until
    // the next refetch (shared-shiaijo S3 under load).
    it('a send back answered with a bare 200 holds over the feed copy from before it', async () => {
        const { ownStatusMoveHolds, ownStatusMoveEnded } = await import('../admin_shiaijo_feed.jsx');
        mockFetch(() => Promise.resolve({ ok: true, status: 200, json: async () => { throw new SyntaxError('Unexpected end of JSON input'); }, text: async () => '' }));
        await API.revertMatchToQueue('c1', 'm1', 'pw');
        const queued = API.ownStatusMove('c1', 'm1');
        await tick(0);
        expect(API.unsentWrites().total).toBe(0);
        const mv = API.ownStatusMove('c1', 'm1');
        expect(mv).toMatchObject({ status: 'scheduled', landed: true, stamp: queued.stamp, version: null });
        const before = { id: 'm1', status: 'running', modifiedAt: queued.stamp - 5000, scoreVersion: 3 };
        expect(ownStatusMoveHolds(mv, before), 'the feed copy from before the send back').toBe(true);
        expect(ownStatusMoveEnded(mv, before)).toBe(false);
        const after = { id: 'm1', status: 'scheduled', modifiedAt: queued.stamp + 20, scoreVersion: 4 };
        expect(ownStatusMoveEnded(mv, after), 'the feed shows the send back').toBe(true);
    });

    it('refused: nothing kept, the match shows as the server holds it', async () => {
        mockFetch(() => Promise.resolve({ ok: false, status: 409, json: async () => ({ error: 'match is not running' }) }));
        const res = await API.revertMatchToQueue('c1', 'm1', 'pw', { onRefusal: () => true });
        res.settled.catch(() => {});
        await tick(0);
        // The refusal path logs a warning on purpose.
        if (console.warn.mockClear) console.warn.mockClear();
        expect(API.unsentWrites().total).toBe(0);
        expect(API.ownStatusMove('c1', 'm1')).toBeNull();
    });

    it('a sign-out discards the queue and what was answered', async () => {
        await API.revertMatchToQueue('c1', 'm1', 'pw');
        API.clearQueue();
        if (console.warn.mockClear) console.warn.mockClear();
        expect(API.ownStatusMove('c1', 'm1')).toBeNull();
    });
});
