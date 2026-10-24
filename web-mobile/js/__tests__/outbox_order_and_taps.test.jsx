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

// Bases flush side by side: one match's request that never answers does not
// hold another match's taps for the pass (review: "other matches never wait").
describe('matches flush concurrently, each in its own order', () => {
    it('a hanging request of one match does not hold back another match\'s taps', async () => {
        await load();
        const sent = [];
        global.fetch = vi.fn((url, opts) => {
            if (isClock(url)) return Promise.reject(new TypeError('clock'));
            const body = JSON.parse(opts.body);
            sent.push({ url: String(url), body });
            if (String(url).includes('/matches/m0/')) return new Promise(() => {});
            const outcomes = body.events.map((e) => ({ id: e.id, outcome: 'applied' }));
            return Promise.resolve({
                ok: true, status: 200,
                json: async () => ({ outcomes, match: { id: 'm1', status: 'running', scoreVersion: 1, eventSeqs: {} } }),
            });
        });
        API.recordTap('c1', 'm0', tap('M'), 'pw', { id: 'm0' });
        API.recordTap('c1', 'm0', tap('K'), 'pw', { id: 'm0' });
        API.recordTap('c1', 'm1', tap('D'), 'pw', { id: 'm1' });
        await tick(0);
        const m0 = sent.filter((s) => s.url.includes('/matches/m0/'));
        const m1 = sent.filter((s) => s.url.includes('/matches/m1/'));
        // m0's one batch is out and hangs; m1's tap went in the same pass.
        expect(m0.length).toBe(1);
        expect(m1.length).toBe(1);
        expect(m1[0].body.events.map((e) => e.letter)).toEqual(['D']);
        expect(API.pendingTaps('c1', 'm1')).toEqual([]);
        // m0 keeps its own order: both taps still queued, nothing sent past them.
        expect(API.pendingTaps('c1', 'm0').length).toBe(2);
    });

    it('within one match a later write still waits for the one that hangs', async () => {
        await load();
        const sent = [];
        global.fetch = vi.fn((url, opts) => {
            if (isClock(url)) return Promise.reject(new TypeError('clock'));
            sent.push({ url: String(url), body: JSON.parse(opts.body) });
            return new Promise(() => {});
        });
        API.recordTap('c1', 'm0', tap('M'), 'pw', { id: 'm0' });
        await tick(0);
        API.recordScore('c1', 'm0', { id: 'm0', status: 'running' }, 'pw');
        await tick(0);
        // The tap is out; the score write behind it is not sent.
        expect(sent.map((s) => s.url.endsWith('/score-events'))).toEqual([true]);
    });
});

describe('M1: a match\'s own order survives a reload and another tab', () => {
    it('a tap after a reload is stamped after the taps the previous page left queued', async () => {
        const ahead = Date.now() + 2000;
        _lsStore.bc_write_queue_v2 = JSON.stringify([storedTap('old.1', 'old', 1, ahead)]);
        await load();
        const t = API.recordTap('c1', 'm1', { kind: 'ippon.remove', pos: 0, side: 'a', target: 'old.1' }, 'pw', { id: 'm1' });
        expect(t.stamp).toBeGreaterThan(ahead);
    });

    it('a tap after another tab\'s queued tap is taken up is stamped after it', async () => {
        await load();
        const ahead = Date.now() + 2000;
        window.dispatchEvent(new StorageEvent('storage', {
            key: 'bc_write_queue_v2', oldValue: null, newValue: JSON.stringify([storedTap('other.1', 'other', 1, ahead)]),
        }));
        const t = API.recordTap('c1', 'm1', tap('K'), 'pw', { id: 'm1' });
        expect(t.stamp).toBeGreaterThan(ahead);
    });

    // R10 (review 2): another tab flushing this tab's taps re-seqs one the
    // server did not log (_reseqUnheldTap) and stores it under the same key and
    // enqueuedAt. This tab takes the new seq into its own copy and moves its
    // seq counter past it, so its next tap never reuses that seq.
    it('a re-seq another tab made of this tab\'s tap is adopted, and the next tap goes past it', async () => {
        await load();
        API.recordTap('c1', 'm1', tap('M'), 'pw', { id: 'm1' });
        API.recordTap('c1', 'm1', tap('K'), 'pw', { id: 'm1' });
        await tick(0);
        const oldRaw = _lsStore.bc_write_queue_v2;
        const entries = JSON.parse(oldRaw);
        expect(entries.map(([, d]) => d.payload.seq)).toEqual([1, 2]);
        // The other tab: tap 2 landed, tap 1 was refused clock_skew behind it and re-seqd to 3.
        const [first] = entries;
        first[1].payload.seq = 3;
        const newRaw = JSON.stringify([first]);
        _lsStore.bc_write_queue_v2 = newRaw;
        window.dispatchEvent(new StorageEvent('storage', { key: 'bc_write_queue_v2', oldValue: oldRaw, newValue: newRaw }));
        expect(API.pendingTaps('c1', 'm1').map((e) => e.seq)).toEqual([3]);
        const next = API.recordTap('c1', 'm1', tap('D'), 'pw', { id: 'm1' });
        expect(next.seq).toBe(4);
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

    // R6 (review 2): only a clock_skew refusal is provably unlogged. A tap the
    // answer names no outcome for may be logged, so it keeps its id AND its seq
    // and is resent as it was, exactly like one a 5xx left queued.
    it('a tap the answer names no outcome for keeps its seq when resent, even behind an applied one', async () => {
        await load();
        const batches = eventServer({ outcome: (e, b) => (b === 0 && e.seq === 1 ? null : { outcome: 'applied' }) });
        API.recordTap('c1', 'm1', tap('M'), 'pw', { id: 'm1' });
        API.recordTap('c1', 'm1', tap('K'), 'pw', { id: 'm1' });
        await tick(0);
        expect(batches.length).toBe(1);
        const first = batches[0].body.events.find((e) => e.seq === 1);
        await tick(1000);
        expect(batches.length).toBe(2);
        // The applied tap behind it is resent too (strict order; a duplicate is
        // harmless): what matters is the first one goes as it went.
        expect(batches[1].body.events[0]).toMatchObject({ id: first.id, seq: 1 });
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

    // Minor 6, as stage 3 review 11 (m1) settled it: the board's derived
    // match counts through the board, which names it (seenScoreVersion), never
    // on its own, so a write claims only what its screen showed.
    it('a write\'s basis takes the copy its board showed, never the derived match on its own (minor 6, m1)', async () => {
        await load();
        eventServer({ version: () => 7 });
        API.recordTap('c1', 'm1', tap('M'), 'pw', { id: 'm1' });
        await tick(0);
        global.fetch = vi.fn(() => Promise.reject(new TypeError('offline')));
        const shown = await API.recordScore('c1', 'm1', { status: 'completed', winner: 'A', seenScoreVersion: 7 }, 'pw', { id: 'm1', scoreVersion: 3 });
        const host = await API.recordScore('c1', 'm1', { status: 'completed', winner: 'A' }, 'pw', { id: 'm1', scoreVersion: 3 });
        const stored = JSON.parse(_lsStore.bc_write_queue_v2);
        expect(stored.find(([k]) => k === shown.key)[1].payload.basis.seen).toBe(7);
        expect(stored.find(([k]) => k === host.key)[1].payload.basis.seen).toBe(3);
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

describe('M1: the re-stamp floor counts only writes the server ordered', () => {
    it('a tap refused for the clock again after its re-stamp is kept on the board and held, never dropped', async () => {
        // Review of PR #483: a tap refused for the clock again after its
        // re-stamp was dropped for good, though the server never logged it.
        // Now it stays queued and on the board, held, re-stamped on each
        // retry, told once, until it lands or the operator discards it.
        await load();
        const alerts = [];
        mod.subscribeQueueAlert((a) => alerts.push(a));
        let skewed = true;
        const batches = eventServer({ outcome: () => (skewed ? { outcome: 'clock_skew' } : { outcome: 'applied' }) });
        API.recordTap('c1', 'm1', tap('M'), 'pw', { id: 'm1', modifiedAt: Date.now() + 4000 });
        await tick(0);
        await tick(0);
        const sends = () => batches.flatMap((b) => b.body.events).filter((e) => e.letter === 'M');
        expect(sends().length).toBe(2);
        expect(API.pendingTaps('c1', 'm1').map((t) => t.letter)).toEqual(['M']);
        expect(API.heldWrites()).toEqual([expect.objectContaining({ matchID: 'm1', keepsFailing: true })]);
        expect(alerts.filter((a) => a.kind === 'server_error')).toEqual([
            expect.objectContaining({ count: 1, taps: true, detail: expect.stringMatching(/clock/i) }),
        ]);
        expect(alerts.some((a) => a.kind === 'not_applied')).toBe(false);

        // Retried on the backoff, re-stamped, still told only once.
        await tick(600);
        expect(sends().length).toBe(3);
        expect(alerts.filter((a) => a.kind === 'server_error')).toHaveLength(1);

        // The clock is right: it lands.
        skewed = false;
        await tick(1100);
        expect(sends().length).toBe(4);
        expect(API.pendingTaps('c1', 'm1')).toEqual([]);
        expect(API.heldWrites()).toEqual([]);
    });


    it('a tap held for the clock and discarded does not floor a later re-stamp into the future', async () => {
        await load();
        // The first tap (M) is refused every time; the second (K) only once.
        const sends = {};
        const batches = eventServer({
            outcome: (e) => {
                sends[e.letter] = (sends[e.letter] || 0) + 1;
                return e.letter === 'M' || sends[e.letter] === 1 ? { outcome: 'clock_skew' } : { outcome: 'applied' };
            },
        });
        API.recordTap('c1', 'm1', tap('M'), 'pw', { id: 'm1', modifiedAt: Date.now() + 4000 });
        await tick(0);
        await tick(0);
        const [held] = API.heldWrites();
        expect(API.discardHeldWrite(held.key)).toBeTruthy();
        expect(API.pendingTaps('c1', 'm1')).toEqual([]);
        const now = Date.now();
        API.recordTap('c1', 'm1', tap('K'), 'pw', { id: 'm1' });
        await tick(0);
        await tick(0);
        const k = batches.flatMap((b) => b.body.events).filter((e) => e.letter === 'K');
        expect(k.length).toBe(2);
        expect(k[1].stamp).toBeLessThanOrEqual(now + 1000);
    });
});
