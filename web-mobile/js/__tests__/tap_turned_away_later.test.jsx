// Stage 6 review 5, finding 2: a tap the server answered as landed can be
// turned away LATER, when a finish that never saw it reaches the server after
// it (rule D5). Its own answer said applied, so the device that made it learns
// of the change from the match: every copy of it names the tap in
// `turnedAway`, and the outbox tells the operator once, naming the match and
// the tap, for this device's own taps only, through a push, an answer or a
// fetch, after a reload too.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { tapNotAppliedAlertText, tapNotAppliedText } from '../write_result.jsx';
import { tapWords } from '../match_history_view.jsx';

let mod;
let API;
let _lsStore;
let _orig;
let es;

async function load() {
    vi.resetModules();
    mod = await import('../api_client.jsx');
    API = mod.API;
}

beforeEach(async () => {
    vi.useFakeTimers();
    _orig = { fetch: global.fetch, ES: global.EventSource, ls: global.localStorage };
    global.EventSource = class FakeES { constructor() { es = this; this.close = () => {}; } };
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

// What the landed-tap record holds, every match's (landed_taps.jsx).
const landedText = () => {
    const keys = Object.keys(_lsStore).filter((k) => k.startsWith('bc_landed_taps_v2:'));
    return keys.length ? keys.map((k) => _lsStore[k]).join('\n') : undefined;
};

async function flushMicrotasks() { for (let i = 0; i < 30; i++) await Promise.resolve(); }
async function tick(ms = 0) { await vi.advanceTimersByTimeAsync(ms); await flushMicrotasks(); }
const shown = { id: 'm1', compId: 'c1', phase: 'bracket', matchNumber: 3, status: 'running', modifiedAt: 0 };
const strike = { kind: 'ippon.add', pos: 1, side: 'a', letter: 'M' };

// A score-events door answering every tap with `outcome`, and a competition
// fetch answering `detail`.
function server({ outcome = 'applied', detail = null } = {}) {
    global.fetch = vi.fn((url, opts) => {
        const u = String(url);
        if (u.includes('/api/time')) return Promise.reject(new TypeError('clock'));
        if (u.endsWith('/score-events')) {
            const body = JSON.parse(opts.body);
            return Promise.resolve({
                ok: true, status: 200,
                json: async () => ({ outcomes: body.events.map((e) => ({ id: e.id, outcome })), match: { id: 'm1', scoreVersion: 2 } }),
            });
        }
        if (u.endsWith('/api/viewer/competitions/c1') && detail) {
            return Promise.resolve({ ok: true, status: 200, json: async () => detail });
        }
        return Promise.reject(new TypeError('offline'));
    });
}

function listen() {
    const alerts = [];
    const outcomes = [];
    mod.subscribeQueueAlert((a) => alerts.push(a));
    API.subscribeTapOutcome('c1', 'm1', (o) => outcomes.push(o));
    return { alerts, outcomes, notApplied: () => alerts.filter((a) => a.kind === 'not_applied') };
}

const push = (turnedAway, matchId = 'm1') => es.onmessage({
    data: JSON.stringify({ type: 'match_updated', data: { competitionId: 'c1', matchId, result: { id: matchId, status: 'completed', turnedAway } } }),
    lastEventId: '',
});

const alertText = (a) => tapNotAppliedAlertText(a.count, a.count === 1,
    tapNotAppliedText(a.outcome, a.where || '', a.tap ? tapWords(a.tap) : ''), a.where || '');

// Stage 6 review 8 (m12): the board can judge a pending tap not applied from
// a copy behind the server (its bout shown removed, while the server already
// holds the bout picked again). The server then applies the tap: the notice
// the board gave is taken back, the operator told it was applied after all,
// and nothing of it is kept.
describe('a tap the board told as not applied that the server applies', () => {
    it('is corrected: its note goes and the operator is told it was applied', async () => {
        let answer;
        global.fetch = vi.fn((url, opts) => {
            const u = String(url);
            if (u.includes('/api/time')) return Promise.reject(new TypeError('clock'));
            if (u.endsWith('/score-events')) {
                const body = JSON.parse(opts.body);
                return new Promise((resolve) => { answer = () => resolve({
                    ok: true, status: 200,
                    json: async () => ({ outcomes: body.events.map((e) => ({ id: e.id, outcome: 'applied' })), match: { id: 'm1', scoreVersion: 3 } }),
                }); });
            }
            return Promise.reject(new TypeError('offline'));
        });
        const heard = listen();
        const t = API.recordTap('c1', 'm1', { ...strike, pos: 2 }, 'pw', shown);
        await tick(0);
        // The board, behind the server, sees the bout removed.
        API.tellPendingTapRefused('c1', 'm1', { id: t.id, outcome: 'noTarget', reason: 'bout_removed' }, { ...strike, pos: 2 });
        await tick(0);
        expect(heard.notApplied()).toHaveLength(1);
        answer();
        await tick(0);
        const corrected = heard.alerts.filter((a) => a.kind === 'tap_applied');
        expect(corrected, 'the operator is told it was applied after all').toHaveLength(1);
        expect(heard.outcomes.filter((o) => o.id === t.id).map((o) => o.outcome)).toEqual(['noTarget', 'applied']);
        expect(landedText(), 'landed, so a later turn-away is told').toContain(t.id);
    });
});

describe('a tap turned away after its answer said it landed', () => {
    // Stage 6 review 6, finding 2: the finish's push can list the tap before
    // the tap's own applied answer arrives. It is told when the answer comes,
    // from the latest copy of the match seen, never left until a later copy.
    // Stage 6 review 6, finding 6: the landed-tap record goes with the
    // operator on sign-out, so the next sign-in is not told of their taps,
    // unless a tap of this device is still queued, when it is kept.
    it('is forgotten on sign-out when no tap of the device is still queued', async () => {
        server();
        const off = API.subscribeToEvents(() => {});
        const heard = listen();
        const t = API.recordTap('c1', 'm1', strike, 'pw', shown);
        await tick(0);
        expect(landedText(), 'recorded as landed').toContain(t.id);
        API.clearQueue();
        expect(landedText(), 'forgotten on sign-out').toBeUndefined();
        push({ [t.id]: 'beforeBaseline' });
        expect(heard.notApplied()).toHaveLength(0);
        if (typeof off === 'function') off();
    });

    // Stage 6 review 7, finding 5: the record belongs to the operator signed
    // in. EVERY sign-out clears it, "Sign out and discard" included (which
    // discards the queued tap too), and a revoked password's park; only a tap
    // still queued to be sent by this device keeps its entry, so the next
    // operator is never told of the previous operator's taps.
    it('is forgotten on Sign out and discard, though a tap was still queued', async () => {
        server();
        const off = API.subscribeToEvents(() => {});
        const t = API.recordTap('c1', 'm1', strike, 'pw', shown);
        await tick(0);
        global.fetch = vi.fn(() => Promise.reject(new TypeError('offline')));
        API.recordTap('c1', 'm1', { ...strike, letter: 'K' }, 'pw', shown);
        await tick(0);
        API.clearQueue();
        expect(landedText(), 'forgotten').toBeUndefined();
        const heard = listen();
        push({ [t.id]: 'beforeBaseline' });
        expect(heard.notApplied(), 'the next operator is not told').toHaveLength(0);
        if (typeof off === 'function') off();
    });

    // Stage 6 review 8, m10: a revoked password's park is no sign-out. The
    // same operator signs in again, so what this device knows of its landed
    // taps stays, and a tap turned away meanwhile is told.
    it('is kept when a revoked password parks the queue, the queued tap kept to be sent', async () => {
        server();
        const off = API.subscribeToEvents(() => {});
        const t = API.recordTap('c1', 'm1', strike, 'pw', shown);
        await tick(0);
        global.fetch = vi.fn(() => Promise.reject(new TypeError('offline')));
        const queued = API.recordTap('c1', 'm1', { ...strike, letter: 'K' }, 'pw', shown);
        await tick(0);
        API.parkQueueForReauth();
        expect(landedText(), 'kept').toContain(t.id);
        expect(API.unsentWrites().total, 'the queued tap stays, to be sent').toBe(1);
        const heard = listen();
        push({ [t.id]: 'beforeBaseline', [queued.id]: 'beforeBaseline' });
        expect(heard.notApplied(), 'the landed tap is told; the queued one, never landed, is not').toHaveLength(1);
        if (typeof off === 'function') off();
    });

    it('is told when the push listing it arrived before its own answer', async () => {
        let release;
        global.fetch = vi.fn((url, opts) => {
            const u = String(url);
            if (u.endsWith('/score-events')) {
                const body = JSON.parse(opts.body);
                return new Promise((res) => {
                    release = () => res({
                        ok: true, status: 200,
                        json: async () => ({ outcomes: body.events.map((e) => ({ id: e.id, outcome: 'applied' })), match: { id: 'm1', scoreVersion: 2 } }),
                    });
                });
            }
            return Promise.reject(new TypeError('offline'));
        });
        const off = API.subscribeToEvents(() => {});
        const heard = listen();
        const t = API.recordTap('c1', 'm1', strike, 'pw', shown);
        await tick(0);
        push({ [t.id]: 'beforeBaseline' });
        await tick(0);
        expect(heard.notApplied(), 'not yet known as landed').toHaveLength(0);
        release();
        await tick(0);
        expect(heard.notApplied(), 'told as its answer lands').toHaveLength(1);
        expect(heard.outcomes).toEqual([{ id: t.id, outcome: 'beforeBaseline' }]);
        push({ [t.id]: 'beforeBaseline' });
        expect(heard.notApplied(), 'told once').toHaveLength(1);
        if (typeof off === 'function') off();
    });

    it('is told once, on the push, naming the match and the tap', async () => {
        server();
        const off = API.subscribeToEvents(() => {});
        const heard = listen();
        const t = API.recordTap('c1', 'm1', strike, 'pw', shown);
        await tick(0);
        expect(heard.notApplied(), 'its own answer said applied').toHaveLength(0);

        push({ [t.id]: 'beforeBaseline' });
        const told = heard.notApplied();
        expect(told).toHaveLength(1);
        expect(told[0].where, 'the match is named').toBeTruthy();
        expect(alertText(told[0])).toBe(
            `Not applied in ${told[0].where} (Aka M added on bout 1): the result was recorded without this change, so it was kept in the match's history. To change the result, tap Correct.`);
        expect(heard.outcomes, 'the match\'s open editor hears it too').toEqual([{ id: t.id, outcome: 'beforeBaseline' }]);

        push({ [t.id]: 'beforeBaseline' });
        expect(heard.notApplied(), 'told once').toHaveLength(1);
        if (typeof off === 'function') off();
    });

    it("is not told for another device's tap, nor for another match's", async () => {
        server();
        const off = API.subscribeToEvents(() => {});
        const heard = listen();
        const t = API.recordTap('c1', 'm1', strike, 'pw', shown);
        await tick(0);
        push({ 'another-device.1': 'beforeBaseline' });
        push({ [t.id]: 'beforeBaseline' }, 'm2');
        expect(heard.notApplied()).toHaveLength(0);
        if (typeof off === 'function') off();
    });

    it('a tap answered not applied at once is told by its answer alone', async () => {
        server({ outcome: 'matchClosed' });
        const off = API.subscribeToEvents(() => {});
        const heard = listen();
        const t = API.recordTap('c1', 'm1', strike, 'pw', shown);
        await tick(0);
        expect(heard.notApplied(), 'the answer').toHaveLength(1);
        push({ [t.id]: 'matchClosed' });
        expect(heard.notApplied(), 'never a second time').toHaveLength(1);
        if (typeof off === 'function') off();
    });

    it('is told by the next page after a reload, on its first fetch of the competition', async () => {
        server();
        const t = API.recordTap('c1', 'm1', strike, 'pw', shown);
        await tick(0);
        // The page goes (its tab with it); the next one reads the competition.
        window.dispatchEvent(new Event('pagehide'));
        await load();
        server({ detail: { id: 'c1', poolMatches: [{ id: 'm1', status: 'completed', turnedAway: { [t.id]: 'beforeBaseline' } }] } });
        const heard = listen();
        await API.fetchCompetitionDetails('c1');
        expect(heard.notApplied()).toHaveLength(1);
        expect(heard.notApplied()[0].outcome).toEqual({ id: t.id, outcome: 'beforeBaseline' });
        await API.fetchCompetitionDetails('c1');
        expect(heard.notApplied(), 'told once').toHaveLength(1);
    });

    it("is told by the court console's reload, a knockout match read from the court's feed", async () => {
        server();
        const t = API.recordTap('c1', 'm1', strike, 'pw', shown);
        await tick(0);
        // The page goes (its tab with it), and the next one loads.
        window.dispatchEvent(new Event('pagehide'));
        await load();
        const feed = { competitions: [{ config: { id: 'c1', name: 'Cup' }, bracket: { rounds: [[{ id: 'm1', status: 'completed', turnedAway: { [t.id]: 'matchClosed' } }]] } }] };
        global.fetch = vi.fn((url) => (String(url).includes('/api/viewer/court/A/matches')
            ? Promise.resolve({ ok: true, status: 200, json: async () => feed })
            : Promise.reject(new TypeError('offline'))));
        const heard = listen();
        await API.fetchCourtMatches('A');
        expect(heard.notApplied()).toHaveLength(1);
        expect(heard.notApplied()[0].outcome).toEqual({ id: t.id, outcome: 'matchClosed' });
    });
});
