// Stage 6 review 9 (F3, F4): what a board told as not applied.
// F3: a `duplicate` answer says only that the tap's id is already in the log
// (a tap turned away is logged too), so it takes a board's "Not applied" back
// only when the match's newest copy does not list the tap turned away.
// F4: the record is the device's, not the tab's: the tab that sends the batch
// knows what another tab's board told, and N taps told at once are one alert.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { toldTapsKey } from '../told_taps.jsx';

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
const strike = { kind: 'ippon.add', pos: 2, side: 'a', letter: 'M' };

function listen() {
    const alerts = [];
    const outcomes = [];
    mod.subscribeQueueAlert((a) => alerts.push(a));
    API.subscribeTapOutcome('c1', 'm1', (o) => outcomes.push(o));
    return {
        alerts, outcomes,
        notApplied: () => alerts.filter((a) => a.kind === 'not_applied'),
        applied: () => alerts.filter((a) => a.kind === 'tap_applied'),
    };
}

// A score-events door: the first send is lost (`lost`), later ones answer
// each tap with `outcome` and the match copy `match`.
function server({ outcome, match, lost = 0 }) {
    let sends = 0;
    global.fetch = vi.fn((url, opts) => {
        const u = String(url);
        if (u.includes('/api/time')) return Promise.reject(new TypeError('clock'));
        if (u.endsWith('/score-events')) {
            sends++;
            if (sends <= lost) return Promise.reject(new TypeError('answer lost'));
            const body = JSON.parse(opts.body);
            return Promise.resolve({
                ok: true, status: 200,
                json: async () => ({ outcomes: body.events.map((e) => ({ id: e.id, outcome })), match }),
            });
        }
        return Promise.reject(new TypeError('offline'));
    });
}

const push = (result) => es.onmessage({
    data: JSON.stringify({ type: 'match_updated', data: { competitionId: 'c1', matchId: 'm1', result: { id: 'm1', ...result } } }),
    lastEventId: '',
});

describe('a duplicate answer for a tap the board told as not applied (F3)', () => {
    it('keeps the notice when the match still lists the tap turned away, and tells nothing more', async () => {
        const off = API.subscribeToEvents(() => {});
        const heard = listen();
        const t = API.recordTap('c1', 'm1', strike, 'pw', shown);
        await tick(0);
        API.tellPendingTapRefused('c1', 'm1', { id: t.id, outcome: 'noTarget', reason: 'bout_removed' }, strike);
        await tick(0);
        expect(heard.notApplied()).toHaveLength(1);
        // The server logged it noTarget; that answer was lost, the resend is a duplicate.
        server({ outcome: 'duplicate', match: { id: 'm1', scoreVersion: 4, turnedAway: { [t.id]: 'noTarget' } } });
        window.dispatchEvent(new Event('online'));
        await tick(5_000);
        expect(heard.applied(), 'never "applied after all"').toHaveLength(0);
        expect(heard.outcomes.filter((o) => o.id === t.id).map((o) => o.outcome)).toEqual(['noTarget']);
        push({ status: 'running', scoreVersion: 5, turnedAway: { [t.id]: 'noTarget' } });
        await tick(25_000);
        expect(heard.notApplied(), 'told once').toHaveLength(1);
        off();
    });

    it('takes the notice back when the match\'s copy does not list the tap', async () => {
        const heard = listen();
        const t = API.recordTap('c1', 'm1', strike, 'pw', shown);
        await tick(0);
        API.tellPendingTapRefused('c1', 'm1', { id: t.id, outcome: 'noTarget', reason: 'bout_removed' }, strike);
        await tick(0);
        server({ outcome: 'duplicate', match: { id: 'm1', scoreVersion: 4 } });
        window.dispatchEvent(new Event('online'));
        await tick(5_000);
        expect(heard.applied()).toHaveLength(1);
        expect(heard.outcomes.filter((o) => o.id === t.id).map((o) => o.outcome)).toEqual(['noTarget', 'applied']);
    });
});

describe('the board\'s notices are the device\'s (F4)', () => {
    it('taps told in one go are one alert', async () => {
        const heard = listen();
        const ids = [1, 2, 3].map((n) => API.recordTap('c1', 'm1', { ...strike, letter: n === 1 ? 'M' : 'K' }, 'pw', shown).id);
        await tick(0);
        for (const id of ids) {
            API.tellPendingTapRefused('c1', 'm1', { id, outcome: 'noTarget', reason: 'bout_removed' }, strike);
        }
        await tick(0);
        expect(heard.notApplied()).toHaveLength(1);
        expect(heard.notApplied()[0].count).toBe(3);
        expect(heard.outcomes.map((o) => o.id)).toEqual(ids);
    });

    it('the tab that sends the batch knows what another tab\'s board told', async () => {
        const t = API.recordTap('c1', 'm1', strike, 'pw', shown);
        await tick(0);
        API.tellPendingTapRefused('c1', 'm1', { id: t.id, outcome: 'noTarget', reason: 'bout_removed' }, strike);
        await tick(0);
        // Another tab of the device, sharing storage, sends the queued tap.
        await load();
        const heard = listen();
        server({ outcome: 'noTarget', match: { id: 'm1', scoreVersion: 4, turnedAway: { [t.id]: 'noTarget' } } });
        window.dispatchEvent(new Event('online'));
        await tick(5_000);
        expect(heard.notApplied(), 'told once on the device, by the board').toHaveLength(0);
    });

    it('a tap another tab\'s board told that this tab\'s batch finds applied is taken back on the device', async () => {
        const t = API.recordTap('c1', 'm1', strike, 'pw', shown);
        await tick(0);
        API.tellPendingTapRefused('c1', 'm1', { id: t.id, outcome: 'noTarget', reason: 'bout_removed' }, strike);
        await tick(0);
        const before = _lsStore[toldTapsKey('c1', 'm1')];
        await load();
        const heard = listen();
        server({ outcome: 'applied', match: { id: 'm1', scoreVersion: 4 } });
        window.dispatchEvent(new Event('online'));
        await tick(5_000);
        expect(heard.applied(), 'the operator is told once').toHaveLength(1);
        const after = _lsStore[toldTapsKey('c1', 'm1')];
        expect(JSON.parse(after)[t.id].back, 'every tab hears it was taken back').toBe(true);
        expect(before).toBeTruthy();
    });

    it('a tab hears another tab took a notice back and takes its own note down', async () => {
        const heard = listen();
        const t = API.recordTap('c1', 'm1', strike, 'pw', shown);
        await tick(0);
        API.tellPendingTapRefused('c1', 'm1', { id: t.id, outcome: 'noTarget', reason: 'bout_removed' }, strike);
        await tick(0);
        const key = toldTapsKey('c1', 'm1');
        const oldValue = _lsStore[key];
        const rec = JSON.parse(oldValue);
        rec[t.id] = { ...rec[t.id], back: true, tab: 'other' };
        const newValue = JSON.stringify(rec);
        _lsStore[key] = newValue;
        window.__bcStoredQueueHandler({ key, oldValue, newValue });
        await tick(0);
        expect(heard.outcomes.filter((o) => o.id === t.id).map((o) => o.outcome)).toEqual(['noTarget', 'applied']);
        expect(heard.applied(), 'the tab that took it back told the operator').toHaveLength(0);
    });
});
