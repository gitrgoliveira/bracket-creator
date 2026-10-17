// bc-evts stage 3 S3a-c: scoring taps go to the outbox as events. API.recordTap
// is synchronous (durable at enqueue in the same tick), taps are never merged,
// a match's consecutive taps of one session go in one POST of at most 64, and
// each event settles on its own outcome.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

let mod;
let API;
let _lsStore;
let _orig;

async function load() {
    vi.resetModules();
    mod = await import('../api_client.jsx');
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
const stored = () => JSON.parse(_lsStore.bc_write_queue_v2 || '[]');
const tap = (letter = 'M', side = 'a') => ({ kind: 'ippon.add', pos: 0, side, letter });

// A fake score-events door: answers each event with `outcome(ev)` (applied by
// default) and the derived match; `status` can refuse the whole batch.
function eventServer({ outcome = () => ({ outcome: 'applied' }), status = () => 200, extra = () => ({}) } = {}) {
    const batches = [];
    let version = 0;
    global.fetch = vi.fn((url, opts) => {
        if (isClock(url)) return Promise.reject(new TypeError('clock'));
        const body = JSON.parse(opts.body);
        batches.push({ url: String(url), body, password: opts.headers['X-Tournament-Password'] });
        const code = status(String(url), body);
        if (code !== 200) return Promise.resolve({ ok: false, status: code, json: async () => ({ error: 'bad' }) });
        if (!String(url).endsWith('/score-events')) return Promise.resolve({ ok: true, status: 200, json: async () => ({}) });
        version += body.events.length;
        const outcomes = body.events.map((e) => ({ id: e.id, ...outcome(e) }));
        return Promise.resolve({
            ok: true, status: 200,
            json: async () => ({ outcomes, match: { id: 'm1', scoreVersion: version }, ...extra(body) }),
        });
    });
    return batches;
}

describe('recordTap', () => {
    it('is durable in the same tick and returns { id, stamp, seq }', () => {
        const a = API.recordTap('c1', 'm1', tap(), 'pw', { id: 'm1', modifiedAt: 0 });
        const b = API.recordTap('c1', 'm1', tap('K'), 'pw', { id: 'm1', modifiedAt: 0 });
        expect(a.seq).toBe(1);
        expect(b.seq).toBe(2);
        expect(b.stamp).toBeGreaterThan(a.stamp);
        const keys = stored().map(([k]) => k);
        expect(keys).toEqual([`c1:m1\u001fevt:${a.id}`, `c1:m1\u001fevt:${b.id}`]);
        expect(a.id.endsWith('.1')).toBe(true);
        // Two taps are two entries: never merged (_coalesceTarget names only
        // score and lineup writes).
        expect(API.unsentWrites().total).toBe(2);
        expect(API.heldWrites().map((h) => h.kind)).toEqual(['evt', 'evt']);
    });

    it('is floored by the match it was made against (bc-hlck)', () => {
        const seen = Date.now() + 3000;
        const a = API.recordTap('c1', 'm1', tap(), 'pw', { id: 'm1', modifiedAt: seen });
        expect(a.stamp).toBe(seen + 1);
    });

    it('a Finish after a tap is its own entry, stamped after it and sent after it', async () => {
        const batches = eventServer();
        const t = API.recordTap('c1', 'm1', tap(), 'pw', { id: 'm1' });
        const fin = await API.recordScore('c1', 'm1', { status: 'completed', winner: 'A' }, 'pw', { id: 'm1' });
        expect(stored()).toHaveLength(2);
        const [, finEntry] = stored().find(([k]) => k === fin.key);
        expect(finEntry.payload.modifiedAt).toBeGreaterThan(t.stamp);
        await fin.settled;
        expect(batches.map((b) => b.url.split('/').pop())).toEqual(['score-events', 'score']);
    });
});

describe('batching', () => {
    it('consecutive taps of one tick go in one POST; past 64 they split', async () => {
        const batches = eventServer();
        for (let i = 0; i < 70; i++) API.recordTap('c1', 'm1', tap(i % 2 ? 'K' : 'M'), 'pw', { id: 'm1' });
        await tick(0);
        expect(batches.map((b) => b.body.events.length)).toEqual([64, 6]);
        expect(batches[0].url).toBe('/api/competitions/c1/matches/m1/score-events');
        expect(batches[0].body.session).toBe(batches[0].body.events[0].session);
        expect(stored()).toEqual([]);
    });

    // bc-evts stage 4: the public self-run sheet scores with the EMPTY
    // password, an engi match by flag taps; the header is sent empty, never
    // left out, so the server reads the caller as a participant.
    it('a participant\'s flag taps send the password header empty, never omitted', async () => {
        const batches = eventServer();
        API.recordTap('c1', 'm1', { kind: 'flag.add', pos: 0, side: 'b' }, '', { id: 'm1' });
        API.recordTap('c1', 'm1', { kind: 'flag.remove', pos: 0, side: 'b' }, undefined, { id: 'm1' });
        await tick(0);
        expect(batches).toHaveLength(1);
        expect(batches[0].password).toBe('');
        expect(batches[0].body.events.map((e) => `${e.kind}:${e.side}`)).toEqual(['flag.add:b', 'flag.remove:b']);
    });

    it('one batch per session: taps an earlier page left behind go apart from this page\'s', async () => {
        const old = [1, 2].map((seq) => [`c1:m1\u001fevt:oldsess.${seq}`, {
            compID: 'c1', matchID: 'm1', password: 'pw', kind: 'evt', terminal: false, method: 'POST',
            url: '/api/competitions/c1/matches/m1/score-events', enqueuedAt: Date.now() - 1000,
            payload: { kind: 'ippon.add', pos: 0, side: 'a', letter: 'M', id: `oldsess.${seq}`, session: 'oldsess', seq, stamp: Date.now() - 1000 + seq },
        }]);
        _lsStore.bc_write_queue_v2 = JSON.stringify(old);
        const batches = eventServer();
        await load();
        API.recordTap('c1', 'm1', tap(), 'pw', { id: 'm1' });
        await tick(0);
        expect(batches.map((b) => b.body.session === 'oldsess')).toEqual([true, false]);
        expect(batches[0].body.events.map((e) => e.seq)).toEqual([1, 2]);
    });
});

describe('outcomes', () => {
    it('quiet outcomes leave the queue with no alert; a not-applied one leaves it with ONE alert a pass', async () => {
        eventServer({ outcome: (e) => (e.letter === 'K' ? { outcome: 'capped' } : { outcome: 'applied' }) });
        const alerts = [];
        mod.subscribeQueueAlert((a) => alerts.push(a));
        const notes = [];
        API.subscribeTapOutcome('c1', 'm1', (o) => notes.push(o));
        API.recordTap('c1', 'm1', tap('M'), 'pw', { id: 'm1' });
        API.recordTap('c1', 'm1', tap('K'), 'pw', { id: 'm1' });
        API.recordTap('c1', 'm1', tap('K'), 'pw', { id: 'm1' });
        await tick(0);
        expect(stored()).toEqual([]);
        expect(alerts.filter((a) => a.kind === 'not_applied')).toEqual([
            expect.objectContaining({ count: 2, outcome: expect.objectContaining({ outcome: 'capped' }) }),
        ]);
        expect(alerts.filter((a) => a.kind === 'sent')).toEqual([]);
        expect(notes.map((n) => n.outcome)).toEqual(['capped', 'capped']);
    });

    it('clock_skew is re-stamped once and resent with the same id', async () => {
        let first = true;
        const batches = eventServer({
            outcome: () => {
                if (first) { first = false; return { outcome: 'clock_skew', reason: 'clock_skew', serverNowMs: Date.now() }; }
                return { outcome: 'applied' };
            },
        });
        const t = API.recordTap('c1', 'm1', tap(), 'pw', { id: 'm1' });
        await tick(0);
        expect(batches.length).toBe(2);
        expect(batches[1].body.events[0].id).toBe(t.id);
        expect(stored()).toEqual([]);
    });

    it('the answer\'s match is kept by the larger scoreVersion and pushed to its followers', async () => {
        eventServer();
        const seen = [];
        API.subscribeDerivedMatch('c1', 'm1', (m) => seen.push(m.scoreVersion));
        API.recordTap('c1', 'm1', tap(), 'pw', { id: 'm1' });
        await tick(0);
        API.recordTap('c1', 'm1', tap('K'), 'pw', { id: 'm1' });
        await tick(0);
        expect(seen).toEqual([1, 2]);
        expect(API.latestDerivedMatch('c1', 'm1')).toMatchObject({ scoreVersion: 2 });
    });

    it('gaps: a missing tap the outbox still holds is sent at once, not on the backoff timer', async () => {
        // The first answer leaves the second tap without an outcome and
        // reports it missing; the outbox still holds it.
        let firstBatch = true;
        const batches = [];
        global.fetch = vi.fn((url, opts) => {
            if (isClock(url)) return Promise.reject(new TypeError('clock'));
            const body = JSON.parse(opts.body);
            batches.push(body);
            const evs = firstBatch ? body.events.slice(0, 1) : body.events;
            const gaps = firstBatch ? [2] : undefined;
            firstBatch = false;
            return Promise.resolve({ ok: true, status: 200, json: async () => ({
                outcomes: evs.map((e) => ({ id: e.id, outcome: 'applied' })), ...(gaps ? { gaps } : {}),
            }) });
        });
        API.recordTap('c1', 'm1', tap(), 'pw', { id: 'm1' });
        API.recordTap('c1', 'm1', tap('K'), 'pw', { id: 'm1' });
        await tick(0);
        expect(batches.length).toBe(2);
        expect(batches[1].events.map((e) => e.seq)).toEqual([2]);
        expect(stored()).toEqual([]);
    });
});

describe('a refused batch is parked; a failing one holds only its match', () => {
    it('a 400 parks the taps: held, failing, blocking the match\'s later writes until discarded', async () => {
        vi.spyOn(console, 'warn').mockImplementation(() => {});
        const batches = eventServer({ status: (url) => (url.endsWith('/score-events') ? 400 : 200) });
        const alerts = [];
        mod.subscribeQueueAlert((a) => alerts.push(a));
        API.recordTap('c1', 'm1', tap(), 'pw', { id: 'm1' });
        API.recordTap('c1', 'm1', tap('K'), 'pw', { id: 'm1' });
        await tick(0);
        const fin = await API.recordScore('c1', 'm1', { status: 'completed', winner: 'A' }, 'pw', { id: 'm1' });
        const other = await API.recordScore('c1', 'm2', { status: 'completed', winner: 'A' }, 'pw', { id: 'm2' });
        await tick(60000);
        await other.settled;
        // Parked once, never resent; the Finish behind the taps never went.
        expect(batches.filter((b) => b.url.endsWith('/score-events')).length).toBe(1);
        expect(batches.some((b) => b.url.endsWith('/m1/score'))).toBe(false);
        expect(alerts.filter((a) => a.kind === 'taps_parked')).toEqual([expect.objectContaining({ count: 2 })]);
        const held = API.heldWrites().filter((h) => h.matchID === 'm1');
        expect(held.map((h) => [h.kind, h.keepsFailing])).toEqual([['evt', true], ['evt', true], ['score', false]]);
        expect(API.heldWriteKeepsFailing('c1', 'm1')).toBe(true);
        // An operator discards the taps: the Finish goes.
        expect(API.discardFailingHeldWrites('c1', 'm1')).toBe(2);
        await tick(0);
        await fin.settled;
        expect(batches.some((b) => b.url.endsWith('/m1/score'))).toBe(true);
        console.warn.mockClear();
    });

    it('a 5xx keeps the taps and blocks only their match', async () => {
        const batches = eventServer({ status: (url) => (url.endsWith('/score-events') ? 503 : 200) });
        API.recordTap('c1', 'm1', tap(), 'pw', { id: 'm1' });
        const other = await API.recordDecision('c1', 'm2', { decision: 'fusensho', decisionBy: 'aka' }, 'pw');
        await tick(0);
        await other.settled;
        expect(batches.filter((b) => b.url.endsWith('/score-events')).length).toBeGreaterThanOrEqual(1);
        expect(API.pendingTaps('c1', 'm1')).toHaveLength(1);
        expect(API.hasHeldWrite('c1', 'm1')).toBe(true);
    });
});

describe('pending taps', () => {
    it('lists this device\'s taps in send order and tells followers as they come and go', async () => {
        eventServer();
        const seen = [];
        API.subscribePendingTaps('c1', 'm1', (list) => seen.push(list.map((e) => e.letter).join('')));
        API.recordTap('c1', 'm1', tap('M'), 'pw', { id: 'm1' });
        API.recordTap('c1', 'm1', tap('K'), 'pw', { id: 'm1' });
        expect(API.pendingTaps('c1', 'm1').map((e) => e.letter)).toEqual(['M', 'K']);
        await tick(0);
        expect(API.pendingTaps('c1', 'm1')).toEqual([]);
        expect(seen.slice(0, 2)).toEqual(['M', 'MK']);
        expect(seen.at(-1)).toBe('');
    });

    it('a tap another tab stored is taken up and sent (cross-tab merge by key)', async () => {
        const batches = eventServer();
        const entry = [`c1:m1\u001fevt:other.1`, {
            compID: 'c1', matchID: 'm1', password: 'pw', kind: 'evt', terminal: false, method: 'POST',
            url: '/api/competitions/c1/matches/m1/score-events', enqueuedAt: Date.now(),
            payload: { kind: 'ippon.add', pos: 0, side: 'b', letter: 'D', id: 'other.1', session: 'other', seq: 1, stamp: Date.now() },
        }];
        window.dispatchEvent(new StorageEvent('storage', {
            key: 'bc_write_queue_v2', oldValue: null, newValue: JSON.stringify([entry]),
        }));
        await tick(0);
        expect(batches.map((b) => b.body.events[0].id)).toEqual(['other.1']);
    });
});
