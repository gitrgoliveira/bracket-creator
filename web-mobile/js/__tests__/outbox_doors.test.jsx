// bc-evts stage 3: every rule-bound write goes to the OUTBOX, synchronously,
// and each match's writes are sent strictly in order (F1, reversing bc-mrgc
// C2). A door answers at once with `{ applied: true, pending: true, key,
// settled }`; `settled` carries the server's eventual answer in today's
// shapes. Nothing waits on the network before the caller proceeds.
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
async function tick(ms = 0) { await vi.advanceTimersByTimeAsync(ms); await flushMicrotasks(); }
const isClock = (url) => String(url).includes('/api/time');
function mockFetch(impl) {
    global.fetch = vi.fn((url, opts) => (isClock(url) ? Promise.reject(new TypeError('clock')) : impl(url, opts)));
}
const writes = () => global.fetch.mock.calls.filter(([u]) => !isClock(u));
const ok = (body) => Promise.resolve({ ok: true, status: 200, json: async () => body });
const status = (code, body) => Promise.resolve({ ok: false, status: code, json: async () => body });
const stored = () => JSON.parse(_lsStore.bc_write_queue_v2 || '[]');
// The refusal paths log a console.warn on purpose: take them off the record.
function expectWarned() { expect(console.warn).toHaveBeenCalled(); console.warn.mockClear(); }
const finish = { status: 'completed', winner: 'A', ipponsA: ['M', 'M'], ipponsB: [] };

describe('a door answers at once and the outbox holds the write', () => {
    it('recordScore (completed) is durable in the same tick and answers pending', async () => {
        mockFetch(() => new Promise(() => {}));
        const p = API.recordScore('c1', 'm1', finish, 'pw', { id: 'm1', modifiedAt: 0 });
        // Same tick: before anything is awaited the entry is in storage.
        expect(stored().length).toBe(1);
        const res = await p;
        expect(res.pending).toBe(true);
        expect(res.applied).toBe(true);
        expect(typeof res.key).toBe('string');
        expect(res.settled).toBeInstanceOf(Promise);
        expect(res.key.startsWith('c1:m1\u001f')).toBe(true);
    });

    it('settled resolves with the server body', async () => {
        mockFetch(() => ok({ id: 'm1', status: 'completed' }));
        const res = await API.recordScore('c1', 'm1', finish, 'pw', { id: 'm1' });
        await expect(res.settled).resolves.toEqual({ id: 'm1', status: 'completed' });
        expect(stored().length).toBe(0);
    });

    it('settled rejects with today\'s error and onRefusal can claim it (no alert)', async () => {
        mockFetch(() => status(409, { error: 'court_busy', court: 'A', matchId: 'm9', compId: 'c1' }));
        const alerts = [];
        mod.subscribeQueueAlert((a) => alerts.push(a));
        const claimed = [];
        const res = await API.recordScore('c1', 'm1', finish, 'pw', { id: 'm1' }, {
            onRefusal: (e) => { claimed.push(e); return true; },
        });
        await expect(res.settled).rejects.toBeInstanceOf(Error);
        expect(claimed.length).toBe(1);
        expect(alerts.filter((a) => a.kind === 'rejected')).toEqual([]);
        expectWarned();
    });

    it('an unclaimed refusal raises the rejected alert like a replay', async () => {
        mockFetch(() => status(400, { error: 'invalid' }));
        const alerts = [];
        mod.subscribeQueueAlert((a) => alerts.push(a));
        const res = await API.recordDecision('c1', 'm1', { decision: 'kiken-voluntary', decisionBy: 'A' }, 'pw');
        await expect(res.settled).rejects.toBeInstanceOf(Error);
        expect(alerts.some((a) => a.kind === 'rejected')).toBe(true);
        expectWarned();
    });

    it('a discarded write settles { discarded: true }', async () => {
        mockFetch(() => status(500, {}));
        const res = await API.recordScore('c1', 'm1', finish, 'pw', { id: 'm1' });
        for (let i = 0; i < 6; i++) await tick(60000);
        expect(API.heldWriteKeepsFailing('c1', 'm1')).toBe(true);
        expect(API.discardHeldWrite(res.key)).toBe(true);
        await expect(res.settled).resolves.toEqual({ discarded: true });
        expectWarned();
    });

    it('every door is an outbox door', async () => {
        mockFetch(() => new Promise(() => {}));
        const answers = [
            await API.recordDecision('c1', 'm1', { decision: 'fusensho', decisionBy: 'A' }, 'pw'),
            await API.reopenMatch('c1', 'm2', 'pw', { reason: 'x' }),
            await API.requeueBlockerAndReopen('c1', 'm3', 'c1', 'm4', 'pw', {}),
            await API.revertMatchToQueue('c1', 'm5', 'pw'),
            await API.recordDaihyosen('c1', 'm7', 'pw', 0),
            await API.removeDaihyosen('c1', 'm8', 'pw', 0),
            await API.removeKachinukiBout('c1', 'm9', 'pw'),
        ];
        for (const a of answers) expect(a.pending).toBe(true);
        const methods = stored().map(([, d]) => d.method).sort();
        expect(methods).toEqual(['DELETE', 'DELETE', 'POST', 'POST', 'POST', 'POST', 'POST']);
    });

    it('revert-to-queue sends a JSON body', async () => {
        mockFetch(() => ok({}));
        const res = await API.revertMatchToQueue('c1', 'm5', 'pw');
        await expect(res.settled).resolves.toBe(true);
        const [, opts] = writes()[0];
        expect(opts.headers['Content-Type']).toBe('application/json');
        expect(typeof JSON.parse(opts.body).writeId).toBe('string');
    });
});

describe('each match\'s writes go strictly in order (F1)', () => {
    it('a write that does not settle holds back its match, not the others', async () => {
        mockFetch((url) => (String(url).includes('/m1/') ? status(500, {}) : ok({})));
        await API.recordScore('c1', 'm1', finish, 'pw', { id: 'm1' });
        await API.recordDecision('c1', 'm1', { decision: 'fusensho', decisionBy: 'A' }, 'pw');
        const other = await API.recordScore('c1', 'm2', finish, 'pw', { id: 'm2' });
        await tick(0);
        await expect(other.settled).resolves.toEqual({});
        const m1 = writes().filter(([u]) => String(u).includes('/m1/'));
        // Only the first of m1's two entries was tried: the decision waits behind it.
        expect(m1.every(([u]) => String(u).endsWith('/score'))).toBe(true);
        expect(API.unsentWrites().total).toBe(2);
    });
});

describe('the held flag: only a write that already failed a pass is "held"', () => {
    it('an ordinary online Finish raises no sent toast and never reads as held', async () => {
        let release;
        mockFetch(() => new Promise((r) => { release = () => r({ ok: true, status: 200, json: async () => ({}) }); }));
        const alerts = [];
        mod.subscribeQueueAlert((a) => alerts.push(a));
        const res = await API.recordScore('c1', 'm1', finish, 'pw', { id: 'm1' });
        expect(API.hasHeldWrite('c1', 'm1')).toBe(false);
        release();
        await res.settled;
        await tick(0);
        expect(alerts.filter((a) => a.kind === 'sent')).toEqual([]);
    });

    it('a write that failed once is held, and its landing is announced', async () => {
        let up = false;
        mockFetch(() => (up ? ok({}) : Promise.reject(new TypeError('offline'))));
        const alerts = [];
        mod.subscribeQueueAlert((a) => alerts.push(a));
        const res = await API.recordScore('c1', 'm1', finish, 'pw', { id: 'm1' });
        await tick(0);
        expect(API.hasHeldWrite('c1', 'm1')).toBe(true);
        up = true;
        await tick(60000);
        await res.settled;
        expect(alerts.filter((a) => a.kind === 'sent').length).toBe(1);
        expect(API.hasHeldWrite('c1', 'm1')).toBe(false);
    });
});

describe('the forced write after a downstream confirm reaches the server', () => {
    it('forceDownstreamReopen is sent but never persisted', async () => {
        mockFetch(() => new Promise(() => {}));
        await API.recordScore('c1', 'm1', { ...finish, forceDownstreamReopen: true }, 'pw', { id: 'm1' });
        const [, opts] = writes()[0];
        expect(JSON.parse(opts.body).forceDownstreamReopen).toBe(true);
        expect(stored()[0][1].payload.forceDownstreamReopen).toBeUndefined();
    });
});
