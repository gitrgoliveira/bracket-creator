// Stage 6 review 7: a tap the stored result holds is never told "Not
// applied" (finding 3: an older copy's list told after a newer copy no
// longer listed the tap), a copy naming no version never wipes the list
// (finding 6), a record another tab's sign-out removed is never put back
// (finding 6), and two tabs of one device tell a turned-away tap once
// (review minor 13).
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

let _lsStore;
let _orig;
const sources = [];

async function freshTab() {
    vi.resetModules();
    const mod = await import('../api_client.jsx');
    return { mod, API: mod.API, es: () => sources[sources.length - 1] };
}

beforeEach(() => {
    vi.useFakeTimers();
    _orig = { fetch: global.fetch, ES: global.EventSource, ls: global.localStorage };
    sources.length = 0;
    global.EventSource = class FakeES { constructor() { sources.push(this); this.close = () => {}; } };
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
    vi.useRealTimers();
    vi.resetModules();
    global.fetch = _orig.fetch;
    global.EventSource = _orig.ES;
    global.localStorage = _orig.ls;
});

async function flush() { for (let i = 0; i < 30; i++) await Promise.resolve(); }
async function tick(ms = 0) { await vi.advanceTimersByTimeAsync(ms); await flush(); }
const shown = { id: 'm1', compId: 'c1', phase: 'bracket', matchNumber: 3, status: 'running', modifiedAt: 0 };
const strike = { kind: 'ippon.add', pos: 1, side: 'a', letter: 'M' };
const pushOn = (source, result) => source.onmessage({
    data: JSON.stringify({ type: 'match_updated', data: { competitionId: 'c1', matchId: 'm1', result: { id: 'm1', status: 'completed', ...result } } }),
    lastEventId: '',
});
function listen(mod) {
    const alerts = [];
    mod.subscribeQueueAlert((a) => alerts.push(a));
    return () => alerts.filter((a) => a.kind === 'not_applied');
}
// A score-events door answering every tap with `outcome` and the match's copy.
function eventsAnswer(outcome, match) {
    return (opts) => {
        const body = JSON.parse(opts.body);
        return Promise.resolve({ ok: true, status: 200, json: async () => ({ outcomes: body.events.map((e) => ({ id: e.id, outcome })), match }) });
    };
}

describe('a turned-away tap is told from the newest copy only', () => {
    it('a fetch newer than the push that listed it, then a duplicate resend: not told', async () => {
        const { mod, API, es } = await freshTab();
        let online = false;
        let detail = null;
        global.fetch = vi.fn((url, opts) => {
            const u = String(url);
            if (u.endsWith('/score-events') && online) return eventsAnswer('duplicate', { id: 'm1', scoreVersion: 11 })(opts);
            if (u.endsWith('/api/viewer/competitions/c1') && detail) return Promise.resolve({ ok: true, status: 200, json: async () => detail });
            return Promise.reject(new TypeError('offline'));
        });
        const off = API.subscribeToEvents(() => {});
        const told = listen(mod);
        const t = API.recordTap('c1', 'm1', strike, 'pw', shown);
        await tick(0);
        // A rival finish turned it away (v10); then the first result, which
        // holds it, arrives (v11, nothing listed): the push is missed, a fetch
        // reads it.
        pushOn(es(), { scoreVersion: 10, turnedAway: { [t.id]: 'beforeBaseline' } });
        detail = { id: 'c1', poolMatches: [{ id: 'm1', status: 'completed', scoreVersion: 11 }] };
        await API.fetchCompetitionDetails('c1');
        // The resend is answered duplicate (it landed).
        online = true;
        await tick(60000);
        expect(told()).toHaveLength(0);
        if (typeof off === 'function') off();
    });

    it('the answer that lands it is newer than the push that listed it: not told', async () => {
        const { mod, API, es } = await freshTab();
        let online = false;
        global.fetch = vi.fn((url, opts) => (String(url).endsWith('/score-events') && online
            ? eventsAnswer('duplicate', { id: 'm1', scoreVersion: 11 })(opts)
            : Promise.reject(new TypeError('offline'))));
        const off = API.subscribeToEvents(() => {});
        const told = listen(mod);
        const t = API.recordTap('c1', 'm1', strike, 'pw', shown);
        await tick(0);
        pushOn(es(), { scoreVersion: 10, turnedAway: { [t.id]: 'beforeBaseline' } });
        online = true;
        await tick(60000);
        expect(told(), 'the answer (v11) lists nothing').toHaveLength(0);
        if (typeof off === 'function') off();
    });

    it('a copy naming no version never wipes the list: told as the answer lands', async () => {
        const { mod, API, es } = await freshTab();
        let release;
        global.fetch = vi.fn((url, opts) => (String(url).endsWith('/score-events')
            ? new Promise((res) => { release = () => res(eventsAnswer('applied', { id: 'm1', scoreVersion: 2 })(opts)); }).then((r) => r)
            : Promise.reject(new TypeError('offline'))));
        const off = API.subscribeToEvents(() => {});
        const told = listen(mod);
        const t = API.recordTap('c1', 'm1', strike, 'pw', shown);
        await tick(0);
        pushOn(es(), { scoreVersion: 10, turnedAway: { [t.id]: 'beforeBaseline' } });
        pushOn(es(), {});
        release();
        await tick(0);
        expect(told()).toHaveLength(1);
        if (typeof off === 'function') off();
    });
});

describe('the landed record and the other tabs', () => {
    it("another tab's sign-out removed it: this tab never puts it back", async () => {
        const { mod, API, es } = await freshTab();
        global.fetch = vi.fn((url, opts) => (String(url).endsWith('/score-events')
            ? eventsAnswer('applied', { id: 'm1', scoreVersion: 2 })(opts)
            : Promise.reject(new TypeError('offline'))));
        const off = API.subscribeToEvents(() => {});
        const told = listen(mod);
        const t1 = API.recordTap('c1', 'm1', strike, 'pw', shown);
        await tick(0);
        const landed = () => Object.keys(_lsStore).filter((k) => k.startsWith('bc_landed_taps_v2:')).map((k) => _lsStore[k]).join('');
        expect(landed()).toContain(t1.id);
        // Another tab signs out: the record goes.
        for (const k of Object.keys(_lsStore)) if (k.startsWith('bc_landed_taps_v2')) delete _lsStore[k];
        const t2 = API.recordTap('c1', 'm1', { ...strike, letter: 'K' }, 'pw', shown);
        await tick(0);
        expect(landed()).toContain(t2.id);
        expect(landed(), 'the removed tap is not put back').not.toContain(t1.id);
        pushOn(es(), { scoreVersion: 5, turnedAway: { [t1.id]: 'beforeBaseline' } });
        expect(told()).toHaveLength(0);
        if (typeof off === 'function') off();
    });

    it('two tabs both reading the copy that lists it tell it once', async () => {
        global.fetch = vi.fn((url, opts) => (String(url).endsWith('/score-events')
            ? eventsAnswer('applied', { id: 'm1', scoreVersion: 2 })(opts)
            : Promise.reject(new TypeError('offline'))));
        const a = await freshTab();
        const offA = a.API.subscribeToEvents(() => {});
        const esA = a.es();
        const toldA = listen(a.mod);
        const t = a.API.recordTap('c1', 'm1', strike, 'pw', shown);
        await tick(0);
        // A second tab of the device, made after the tap landed, so it never
        // sent it.
        const b = await freshTab();
        const offB = b.API.subscribeToEvents(() => {});
        const esB = b.es();
        const toldB = listen(b.mod);
        const copy = { scoreVersion: 9, turnedAway: { [t.id]: 'beforeBaseline' } };
        // B reads it first, then A: only A, the tab that noted it, tells.
        pushOn(esB, copy);
        pushOn(esA, copy);
        expect(toldB()).toHaveLength(0);
        expect(toldA()).toHaveLength(1);
        // A goes (its page hidden for good): B, the only tab left, tells what
        // A noted and never told.
        const t2 = a.API.recordTap('c1', 'm1', { ...strike, letter: 'K' }, 'pw', shown);
        await tick(0);
        window.dispatchEvent(new Event('pagehide'));
        pushOn(esB, { scoreVersion: 12, turnedAway: { [t2.id]: 'matchClosed' } });
        expect(toldB(), "a gone tab's tap is told by the tab left").toHaveLength(1);
        pushOn(esA, { scoreVersion: 12, turnedAway: { [t2.id]: 'matchClosed' } });
        expect(toldA(), 'and only once').toHaveLength(1);
        if (typeof offA === 'function') offA();
        if (typeof offB === 'function') offB();
    });

    // Stage 6 review 8, m11: a tab that dies without its pagehide (a crash)
    // keeps its taps until its heartbeat is old. The copy that listed them came
    // while it still looked alive, and no later copy comes: the tab left tells
    // them at its next heartbeat.
    it('a tab that died without pagehide: its tap is told at the next heartbeat, with no later copy', async () => {
        global.fetch = vi.fn((url, opts) => (String(url).endsWith('/score-events')
            ? eventsAnswer('applied', { id: 'm1', scoreVersion: 2 })(opts)
            : Promise.reject(new TypeError('offline'))));
        const realInterval = globalThis.setInterval;
        globalThis.setInterval = () => ({ unref() {} }); // A never beats again
        const a = await freshTab();
        globalThis.setInterval = realInterval;
        const offA = a.API.subscribeToEvents(() => {});
        const t = a.API.recordTap('c1', 'm1', strike, 'pw', shown);
        await tick(0);
        const b = await freshTab();
        const offB = b.API.subscribeToEvents(() => {});
        const toldB = listen(b.mod);
        pushOn(b.es(), { scoreVersion: 9, turnedAway: { [t.id]: 'beforeBaseline' } });
        expect(toldB(), 'A still looks alive').toHaveLength(0);
        const { LANDED_TAB_LIVE_MS, LANDED_TAB_BEAT_MS } = await import('../landed_taps.jsx');
        await tick(LANDED_TAB_LIVE_MS + LANDED_TAB_BEAT_MS);
        expect(toldB(), 'told by the tab left, from the copy it saw').toHaveLength(1);
        if (typeof offA === 'function') offA();
        if (typeof offB === 'function') offB();
    });
});
