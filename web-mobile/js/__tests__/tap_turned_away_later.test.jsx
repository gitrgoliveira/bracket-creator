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

describe('a tap turned away after its answer said it landed', () => {
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
        // The page goes; the next one reads the competition.
        await load();
        server({ detail: { id: 'c1', poolMatches: [{ id: 'm1', status: 'completed', turnedAway: { [t.id]: 'beforeBaseline' } }] } });
        const heard = listen();
        await API.fetchCompetitionDetails('c1');
        expect(heard.notApplied()).toHaveLength(1);
        expect(heard.notApplied()[0].outcome).toEqual({ id: t.id, outcome: 'beforeBaseline' });
        await API.fetchCompetitionDetails('c1');
        expect(heard.notApplied(), 'told once').toHaveLength(1);
    });
});
