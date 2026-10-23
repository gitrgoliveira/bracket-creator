// bc-evts stage 3 (the governing rule): ALL goes to the outbox, in strict
// order per match, and it never blocks the UI. A RUNNING score write (the team
// team sheet's row write, Start match, Record bout) is an outbox entry like every
// other door: enqueued in the same tick on the match's base, answered pending,
// sent after what the match already holds. A write may also name another
// match's entry it must follow (`after`): the court console's pick of another
// bout queues the send back and then the start, and the start is sent only
// once the send back has settled.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

let mod;
let API;
let _lsStore;
let _orig;

beforeEach(async () => {
    vi.useFakeTimers();
    vi.resetModules();
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
    mod = await import('../api_client.jsx');
    API = mod.API;
});

afterEach(() => {
    vi.useRealTimers();
    vi.resetModules();
    global.fetch = _orig.fetch;
    global.EventSource = _orig.ES;
    global.localStorage = _orig.ls;
});

async function flushMicrotasks() { for (let i = 0; i < 30; i++) await Promise.resolve(); }
const isClock = (url) => String(url).includes('/api/time');
function mockFetch(impl) {
    global.fetch = vi.fn((url, opts) => (isClock(url) ? Promise.reject(new TypeError('clock')) : impl(url, opts)));
}
const writes = () => global.fetch.mock.calls.filter(([u]) => !isClock(u));
const bodyOf = (call) => JSON.parse(call[1].body);
const ok = (body) => Promise.resolve({ ok: true, status: 200, json: async () => body });
const status = (code, body) => Promise.resolve({ ok: false, status: code, json: async () => body });
const stored = () => JSON.parse(_lsStore.bc_write_queue_v2 || '[]');
// A fetch the test answers by hand, one per call.
function heldFetches() {
    const held = [];
    mockFetch((url, opts) => new Promise((resolve) => { held.push({ url, opts, resolve }); }));
    return held;
}
const answer = (h, body) => h.resolve({ ok: true, status: 200, json: async () => body });
const finish = { status: 'completed', winner: 'A', ipponsA: ['M', 'M'], ipponsB: [] };
const running = (ipponsA) => ({ status: 'running', ipponsA, ipponsB: [], changed: ['points'] });

describe('a running score write is an outbox entry', () => {
    it('is stored in the same tick and answered pending', async () => {
        mockFetch(() => new Promise(() => {}));
        const p = API.recordScore('c1', 'm1', running(['M']), 'pw', { id: 'm1', modifiedAt: 0 });
        expect(stored().length).toBe(1);
        const res = await p;
        expect(res.pending).toBe(true);
        expect(res.applied).toBe(true);
        expect(typeof res.key).toBe('string');
        expect(res.settled).toBeInstanceOf(Promise);
    });

    it('queued behind a Finish still out for the same match, it is sent after it, never before', async () => {
        const held = heldFetches();
        API.recordScore('c1', 'm1', finish, 'pw', { id: 'm1', modifiedAt: 0 });
        await flushMicrotasks();
        expect(writes().length).toBe(1);
        // A running write made after the Finish (e.g. Start on a correction, a
        // late running write): while the Finish is out, nothing else of m1 is sent.
        const res = await API.recordScore('c1', 'm1', running(['M', 'K']), 'pw', { id: 'm1', modifiedAt: 0 });
        await flushMicrotasks();
        expect(res.pending).toBe(true);
        expect(writes().length).toBe(1);
        expect(bodyOf(writes()[0]).status).toBe('completed');
        // The Finish answers: the running write goes next, in the same flush.
        answer(held[0], { applied: true, status: 'completed' });
        await flushMicrotasks();
        expect(writes().length).toBe(2);
        expect(bodyOf(writes()[1]).status).toBe('running');
        // And never stamped before the write it follows.
        expect(bodyOf(writes()[1]).modifiedAt).toBeGreaterThan(bodyOf(writes()[0]).modifiedAt);
        answer(held[1], { applied: true, status: 'running' });
        await expect(res.settled).resolves.toMatchObject({ applied: true, status: 'running' });
    });

    it('takes the place of a queued running write not yet out, claiming its groups', async () => {
        // Offline: the first running write stays queued.
        API.recordScore('c1', 'm1', { ...running(['M']), changed: ['points'] }, 'pw', { id: 'm1', modifiedAt: 0 });
        await flushMicrotasks();
        API.recordScore('c1', 'm1', { ...running(['M']), encho: { periodCount: 1 }, changed: ['encho'] }, 'pw', { id: 'm1', modifiedAt: 0 });
        await flushMicrotasks();
        const q = stored();
        expect(q.length).toBe(1);
        expect(q[0][1].payload.changed.sort()).toEqual(['encho', 'points']);
    });

    it('never takes the place of a queued Finish', async () => {
        // A queued Finish (offline) and a running write after it: two entries.
        API.recordScore('c1', 'm1', finish, 'pw', { id: 'm1', modifiedAt: 0 });
        await flushMicrotasks();
        API.recordScore('c1', 'm1', running(['M']), 'pw', { id: 'm1', modifiedAt: 0 });
        await flushMicrotasks();
        expect(stored().length).toBe(2);
    });

    it('never takes the place of a running write already being sent', async () => {
        const held = heldFetches();
        API.recordScore('c1', 'm2', running(['M']), 'pw', { id: 'm2', modifiedAt: 0 });
        await flushMicrotasks();
        const out = held.find((h) => String(h.url).includes('/m2/'));
        expect(out).toBeTruthy();
        API.recordScore('c1', 'm2', running(['M', 'K']), 'pw', { id: 'm2', modifiedAt: 0 });
        await flushMicrotasks();
        const m2Entries = stored().filter(([k]) => k.startsWith('c1:m2'));
        expect(m2Entries.length).toBe(2);
    });

    it('a pending kachinukiBoutFinal is carried by the running write that takes its place', async () => {
        API.recordScore('c1', 'm1', { ...running(['M']), kachinukiBoutFinal: true }, 'pw', { id: 'm1', modifiedAt: 0 });
        await flushMicrotasks();
        API.recordScore('c1', 'm1', running(['M', 'K']), 'pw', { id: 'm1', modifiedAt: 0 });
        await flushMicrotasks();
        const q = stored();
        expect(q.length).toBe(1);
        expect(q[0][1].payload.kachinukiBoutFinal).toBe(true);
    });

    it('carries rev, revSession, writeId and basis, and never sends the request-only fields', async () => {
        mockFetch(() => ok({ applied: true }));
        const res = await API.recordScore('c1', 'm1', { ...running(['M']), durable: true, after: 'x' }, 'pw', { id: 'm1', modifiedAt: 0 });
        await res.settled;
        const sent = bodyOf(writes()[0]);
        expect(sent.rev).toBeGreaterThan(0);
        expect(typeof sent.revSession).toBe('string');
        expect(typeof sent.writeId).toBe('string');
        expect(sent.basis).toBeTruthy();
        expect(sent.durable).toBeUndefined();
        expect(sent.after).toBeUndefined();
    });

    it('a refused running write rejects `settled` with the refusal (a court_busy start)', async () => {
        mockFetch(() => status(409, { error: 'court_busy', label: 'Pool A · Match 2', court: 'A' }));
        const spy = vi.spyOn(console, 'warn').mockImplementation(() => {});
        const res = await API.recordScore('c1', 'm1', { status: 'running', ipponsA: [], ipponsB: [] }, 'pw', { id: 'm1', modifiedAt: 0 }, { onRefusal: () => true });
        await expect(res.settled).rejects.toBeInstanceOf(Error);
        spy.mockRestore();
    });
});

describe('`after`: a write that follows another match\'s entry', () => {
    it('the start is sent only once the send back has settled, in the same flush', async () => {
        const held = heldFetches();
        const back = await API.revertMatchToQueue('c1', 'm1', 'pw');
        const start = await API.recordScore('c1', 'm2', { status: 'running', ipponsA: [], ipponsB: [], after: back.key }, 'pw', { id: 'm2', modifiedAt: 0 });
        expect(start.pending).toBe(true);
        await flushMicrotasks();
        // Only the send back is out.
        expect(writes().length).toBe(1);
        expect(String(writes()[0][0])).toContain('/m1/');
        // Persisted with the entry, so a reload keeps the order.
        const startEntry = stored().find(([k]) => k.startsWith('c1:m2'));
        expect(startEntry[1].after).toBe(back.key);
        answer(held[0], { ok: true });
        await flushMicrotasks();
        // No timer advanced: the start goes in the same flush.
        expect(writes().length).toBe(2);
        expect(String(writes()[1][0])).toContain('/m2/score');
    });

    it('a send back refused for good lets the start go; the server judges it', async () => {
        const spy = vi.spyOn(console, 'warn').mockImplementation(() => {});
        let n = 0;
        mockFetch((url) => {
            n++;
            if (String(url).includes('/m1/')) return status(409, { error: 'not_running' });
            return ok({ applied: true });
        });
        const back = await API.revertMatchToQueue('c1', 'm1', 'pw', { onRefusal: () => true });
        const start = await API.recordScore('c1', 'm2', { status: 'running', ipponsA: [], ipponsB: [], after: back.key }, 'pw', { id: 'm2', modifiedAt: 0 });
        await flushMicrotasks();
        expect(n).toBe(2);
        await expect(start.settled).resolves.toMatchObject({ applied: true });
        spy.mockRestore();
    });

    it('a send back that cannot be sent (offline) holds the start back, and it is not marked held', async () => {
        const back = await API.revertMatchToQueue('c1', 'm1', 'pw');
        await API.recordScore('c1', 'm2', { status: 'running', ipponsA: [], ipponsB: [], after: back.key }, 'pw', { id: 'm2', modifiedAt: 0 });
        await flushMicrotasks();
        expect(writes().filter(([u]) => String(u).includes('/m2/')).length).toBe(0);
        // Waiting is not failing: the start is not marked held (the send back,
        // whose send failed, is).
        const byMatch = Object.fromEntries(stored().map(([k, d]) => [k.split('\u001f')[0], d]));
        expect(byMatch['c1:m1'].held).toBe(true);
        expect(byMatch['c1:m2'].held).toBeFalsy();
    });
});

describe('`after` on a write held for the clock', () => {
    it('a Finish refused for the clock twice holds the start that follows it, names it, and can be discarded from the waiting match', async () => {
        // Review of PR #483: a write kept for the clock (`clockRefused`) never
        // gets `attempts`, so the start queued `after` it read as only waiting:
        // never held, no banner naming the Finish, waiting until someone found
        // and discarded it. Held for the clock is stuck, exactly as refused.
        const spy = vi.spyOn(console, 'warn').mockImplementation(() => {});
        mockFetch((url) => (String(url).includes('/m1/')
            ? ok({ applied: false, reason: 'clock_skew', serverNowMs: Date.now() + 100000 })
            : ok({ applied: true })));
        const fin = await API.recordScore('c1', 'm1', finish, 'pw', { id: 'm1', modifiedAt: 0 });
        await API.recordScore('c1', 'm2', { status: 'running', ipponsA: [], ipponsB: [], after: fin.key }, 'pw', { id: 'm2', modifiedAt: 0 });
        for (let i = 0; i < 4; i++) { await vi.advanceTimersByTimeAsync(5000); await flushMicrotasks(); }
        const byMatch = Object.fromEntries(stored().map(([k, d]) => [k.split('\u001f')[0], d]));
        expect(byMatch['c1:m1'].clockRefused, 'the Finish is kept for the clock').toBe(true);
        expect(byMatch['c1:m2'].held, 'the start waiting on it is held with it').toBe(true);
        expect(writes().filter(([u]) => String(u).includes('/m2/')).length, 'and never sent').toBe(0);
        expect(API.heldWriteBlocker('c1', 'm2')).toMatchObject({ matchID: 'm1', keepsFailing: true });
        // Discarded from the waiting match's banner, the start then goes.
        expect(API.discardFailingHeldWrites('c1', 'm2')).toBe(1);
        await vi.advanceTimersByTimeAsync(10); await flushMicrotasks();
        expect(writes().filter(([u]) => String(u).includes('/m2/score')).length).toBe(1);
        spy.mockRestore();
    });

    it('a write held for the clock is discarded from the held-writes list', async () => {
        const spy = vi.spyOn(console, 'warn').mockImplementation(() => {});
        mockFetch(() => ok({ applied: false, reason: 'clock_skew', serverNowMs: Date.now() + 100000 }));
        const fin = await API.recordScore('c1', 'm1', finish, 'pw', { id: 'm1', modifiedAt: 0 });
        for (let i = 0; i < 4; i++) { await vi.advanceTimersByTimeAsync(5000); await flushMicrotasks(); }
        expect(API.heldWrites().find((h) => h.key === fin.key)).toMatchObject({ keepsFailing: true, held: true });
        expect(API.discardHeldWrite(fin.key)).toBe(true);
        expect(stored().length).toBe(0);
        spy.mockRestore();
    });
});
