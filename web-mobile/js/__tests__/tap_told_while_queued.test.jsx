// Stage 6 review 11, minor 2: another tab that settles a tap stores the told
// record before the queue, so this tab, which still holds the tap queued,
// hears the record lose it first. Its board still shows the tap pending and
// must not tell it a second time: a tap is forgotten as told here only once
// it has left this tab's queue. The same holds for the sweep of the record.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { toldTapsKey, TOLD_TAPS_MAX } from '../told_taps.jsx';
import { LANDED_TAB_BEAT_MS } from '../landed_taps.jsx';

let mod;
let API;
let _lsStore;
let _orig;

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
        key: (i) => Object.keys(_lsStore)[i] ?? null,
        get length() { return Object.keys(_lsStore).length; },
    };
    global.fetch = vi.fn(() => Promise.reject(new TypeError('offline')));
    vi.resetModules();
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

async function tick(ms = 0) {
    await vi.advanceTimersByTimeAsync(ms);
    for (let i = 0; i < 30; i++) await Promise.resolve();
}
const shown = { id: 'm1', compId: 'c1', phase: 'bracket', matchNumber: 3, status: 'running', modifiedAt: 0 };
const strike = { kind: 'ippon.add', pos: 2, side: 'a', letter: 'M' };

function notApplied() {
    const alerts = [];
    mod.subscribeQueueAlert((a) => alerts.push(a));
    return () => alerts.filter((a) => a.kind === 'not_applied');
}

describe('a tap told on the board while this tab still holds it queued', () => {
    it('is not told again when another tab settles it before this tab hears the queue', async () => {
        const told = notApplied();
        const t = API.recordTap('c1', 'm1', strike, 'pw', shown);
        await tick(0);
        const refused = { id: t.id, outcome: 'noTarget', reason: 'bout_removed' };
        API.tellPendingTapRefused('c1', 'm1', refused, strike);
        await tick(0);
        expect(told()).toHaveLength(1);
        // Another tab sent the batch and settled the tap: the told record
        // loses it first; this tab's queue still holds the tap.
        const key = toldTapsKey('c1', 'm1');
        const oldValue = _lsStore[key];
        delete _lsStore[key];
        window.dispatchEvent(new StorageEvent('storage', { key, oldValue, newValue: null }));
        await tick(0);
        // The board renders meanwhile, the tap still pending on it.
        API.tellPendingTapRefused('c1', 'm1', refused, strike);
        await tick(0);
        expect(told(), 'told once').toHaveLength(1);
    });

    it('is not told again when the sweep takes it out of the record', async () => {
        const told = notApplied();
        const t = API.recordTap('c1', 'm1', strike, 'pw', shown);
        await tick(0);
        const refused = { id: t.id, outcome: 'noTarget', reason: 'bout_removed' };
        API.tellPendingTapRefused('c1', 'm1', refused, strike);
        await tick(0);
        // The record overflows its cap with newer entries of another match:
        // the sweep at the next beat takes this tap's entry out.
        const now = Date.now() + 1;
        const many = {};
        for (let i = 0; i < TOLD_TAPS_MAX; i++) many[`o${i}`] = { at: now + i, tab: 'other' };
        _lsStore[toldTapsKey('c1', 'm2')] = JSON.stringify(many);
        await tick(LANDED_TAB_BEAT_MS * 60);
        expect(_lsStore[toldTapsKey('c1', 'm1')], 'the sweep took the entry out').toBeUndefined();
        API.tellPendingTapRefused('c1', 'm1', refused, strike);
        await tick(0);
        expect(told(), 'told once').toHaveLength(1);
    });
});
