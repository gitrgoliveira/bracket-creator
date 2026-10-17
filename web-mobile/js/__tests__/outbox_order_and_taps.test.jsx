// bc-evts stage 3, review round 1: the outbox keeps a match's own order across
// a reload, another tab and a clock-skew re-stamp (M1), a tap the server does
// not hold stays on the board (M3), and the smaller tap rules (minors 6, 9, 12,
// 13).
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { applyPending, DUPLICATE } from '../score_events.jsx';

let mod;
let API;
let _lsStore;
let _orig;

async function load() {
    vi.resetModules();
    mod = await import('../api_client.jsx');
    API = mod.API;
}

beforeEach(() => {
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
});

afterEach(() => {
    // Quiesce this module instance before the real timers come back, so a
    // flush still out never sends through a later test's fetch (sync_queue M6).
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
const isClock = (url) => String(url).includes('/api/time');
const tap = (letter = 'M', side = 'a') => ({ kind: 'ippon.add', pos: 0, side, letter });
const foul = (side = 'a') => ({ kind: 'foul.add', pos: 0, side });

function storedTap(id, session, seq, stamp) {
    return [`c1:m1\u001fevt:${id}`, {
        compID: 'c1', matchID: 'm1', password: 'pw', kind: 'evt', terminal: false, method: 'POST',
        url: '/api/competitions/c1/matches/m1/score-events', enqueuedAt: Date.now(),
        payload: { kind: 'ippon.add', pos: 0, side: 'a', letter: 'M', id, session, seq, stamp },
    }];
}

// A score-events door answering each event with outcome(ev, batchIndex) and a
// match whose eventSeqs names the highest seq it applied per session.
function eventServer({ outcome = () => ({ outcome: 'applied' }), version = () => 1, hang = () => false } = {}) {
    const batches = [];
    const top = {};
    let n = 0;
    global.fetch = vi.fn((url, opts) => {
        if (isClock(url)) return Promise.reject(new TypeError('clock'));
        const body = JSON.parse(opts.body);
        const b = n++;
        batches.push({ url: String(url), body });
        if (hang(b)) return new Promise(() => {});
        if (!String(url).endsWith('/score-events')) {
            return Promise.resolve({ ok: true, status: 200, json: async () => ({}) });
        }
        const outcomes = [];
        for (const e of body.events) {
            const o = outcome(e, b);
            if (o === null) continue;
            if (o.outcome === 'applied') top[e.session] = Math.max(top[e.session] || 0, e.seq);
            outcomes.push({ id: e.id, ...o });
        }
        return Promise.resolve({
            ok: true, status: 200,
            json: async () => ({ outcomes, match: { id: 'm1', status: 'running', scoreVersion: version(b), eventSeqs: { ...top } } }),
        });
    });
    return batches;
}

describe('M1: a match\'s own order survives a reload and another tab', () => {
    it('a tap after a reload is stamped after the taps the previous page left queued', async () => {
        const ahead = Date.now() + 2000;
        _lsStore.bc_write_queue = JSON.stringify([storedTap('old.1', 'old', 1, ahead)]);
        await load();
        const t = API.recordTap('c1', 'm1', { kind: 'ippon.remove', pos: 0, side: 'a', target: 'old.1' }, 'pw', { id: 'm1' });
        expect(t.stamp).toBeGreaterThan(ahead);
    });

    it('a tap after another tab\'s queued tap is taken up is stamped after it', async () => {
        await load();
        const ahead = Date.now() + 2000;
        window.dispatchEvent(new StorageEvent('storage', {
            key: 'bc_write_queue', oldValue: null, newValue: JSON.stringify([storedTap('other.1', 'other', 1, ahead)]),
        }));
        const t = API.recordTap('c1', 'm1', tap('K'), 'pw', { id: 'm1' });
        expect(t.stamp).toBeGreaterThan(ahead);
    });

    it('a tap re-stamped for clock skew is stamped after the match\'s writes that already landed', async () => {
        await load();
        const batches = eventServer({
            outcome: (e, b) => (b === 0 && e.seq === 2 ? { outcome: 'clock_skew' } : { outcome: 'applied' }),
        });
        const a = API.recordTap('c1', 'm1', tap('M'), 'pw', { id: 'm1', modifiedAt: Date.now() + 4000 });
        API.recordTap('c1', 'm1', tap('K'), 'pw', { id: 'm1' });
        await tick(0);
        expect(batches.length).toBe(2);
        const resent = batches[1].body.events.find((e) => e.seq !== 1);
        expect(resent.stamp).toBeGreaterThan(a.stamp);
    });
});

describe('M3: a tap the server does not hold stays on the board', () => {
    it('a tap refused for clock skew behind an applied one is folded, not read as already held', async () => {
        await load();
        // The resend is never answered, so the re-stamped tap stays pending.
        eventServer({
            outcome: (e) => (e.seq === 1 ? { outcome: 'clock_skew' } : { outcome: 'applied' }),
            hang: (b) => b > 0,
        });
        API.recordTap('c1', 'm1', foul('a'), 'pw', { id: 'm1' });
        API.recordTap('c1', 'm1', tap('K', 'b'), 'pw', { id: 'm1' });
        await tick(0);
        const pending = API.pendingTaps('c1', 'm1').filter((e) => e.kind === 'foul.add');
        expect(pending).toHaveLength(1);
        const server = { id: 'm1', status: 'running', ipponsA: [], ipponsB: ['K'], hansokuA: 0, hansokuB: 0, eventSeqs: API.latestDerivedMatch('c1', 'm1').eventSeqs };
        const { outcomes } = applyPending(server, pending);
        expect(outcomes[pending[0].id]).not.toBe(DUPLICATE);
    });

    it('a tap the answer names no outcome for is retried on a backoff (minor 9)', async () => {
        await load();
        const batches = eventServer({ outcome: (e, b) => (b === 0 ? null : { outcome: 'applied' }) });
        API.recordTap('c1', 'm1', tap('M'), 'pw', { id: 'm1' });
        await tick(0);
        expect(batches.length).toBe(1);
        await tick(1000);
        expect(batches.length).toBe(2);
        expect(API.pendingTaps('c1', 'm1')).toEqual([]);
    });
});

describe('minor taps rules', () => {
    it('a parked tap is not shown as counted on the board (minor 12)', async () => {
        await load();
        vi.spyOn(console, 'warn').mockImplementation(() => {});
        global.fetch = vi.fn((url) => (isClock(url)
            ? Promise.reject(new TypeError('clock'))
            : Promise.resolve({ ok: false, status: 400, json: async () => ({ error: 'bad' }) })));
        API.recordTap('c1', 'm1', tap('M'), 'pw', { id: 'm1' });
        await tick(0);
        expect(API.heldWrites().filter((h) => h.kind === 'evt')).toHaveLength(1);
        expect(API.pendingTaps('c1', 'm1')).toEqual([]);
        console.warn.mockRestore();
    });

    it('a tap browser storage refused says so (minor 13)', async () => {
        await load();
        global.localStorage.setItem = () => { throw new Error('QuotaExceededError'); };
        const t = API.recordTap('c1', 'm1', tap('M'), 'pw', { id: 'm1' });
        expect(t.persisted).toBe(false);
        expect(t.seq).toBe(1);
    });

    it('a write\'s basis takes the newer of the caller\'s seen and the board\'s derived match (minor 6)', async () => {
        await load();
        eventServer({ version: () => 7 });
        API.recordTap('c1', 'm1', tap('M'), 'pw', { id: 'm1' });
        await tick(0);
        global.fetch = vi.fn(() => Promise.reject(new TypeError('offline')));
        const fin = await API.recordScore('c1', 'm1', { status: 'completed', winner: 'A' }, 'pw', { id: 'm1', scoreVersion: 3 });
        const [, entry] = JSON.parse(_lsStore.bc_write_queue).find(([k]) => k === fin.key);
        expect(entry.payload.basis.seen).toBe(7);
    });
});

describe('M4: a tap alert names taps', () => {
    it('a batch parked for sign-in says it holds taps', async () => {
        await load();
        global.fetch = vi.fn((url) => (isClock(url)
            ? Promise.reject(new TypeError('clock'))
            : Promise.resolve({ ok: false, status: 401, json: async () => ({ error: 'auth' }) })));
        const alerts = [];
        mod.subscribeQueueAlert((a) => alerts.push(a));
        API.recordTap('c1', 'm1', tap('M'), 'pw', { id: 'm1' });
        API.recordTap('c1', 'm1', tap('K'), 'pw', { id: 'm1' });
        await tick(0);
        expect(alerts.filter((a) => a.kind === 'auth_required')).toEqual([expect.objectContaining({ count: 2, taps: true })]);
    });
});
