// Stage 6 review 9 (G1): a write that first starts a kachinuki match keeps its
// lineup, and when a later match's carried lineup cannot follow, the score or
// decision answer carries `warning`, as the lineup doors' answers do. The
// outbox tells it to the operator, never only logs it.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

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
    };
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

async function flushMicrotasks() { for (let i = 0; i < 30; i++) await Promise.resolve(); }
async function tick(ms = 0) { await vi.advanceTimersByTimeAsync(ms); await flushMicrotasks(); }
const WARNING = 'The match was started, but the lineup carried to Pool A · Match 2 could not follow it and was kept as it was. Check that match\'s lineup.';

describe('a score write answered with a warning', () => {
    it('tells the operator once, as a lineup warning', async () => {
        global.fetch = vi.fn((url) => {
            if (String(url).includes('/api/time')) return Promise.reject(new TypeError('clock'));
            return Promise.resolve({ ok: true, status: 200, json: async () => ({ id: 'm1', status: 'running', modifiedAt: 50, warning: WARNING }) });
        });
        const alerts = [];
        mod.subscribeQueueAlert((a) => alerts.push(a));
        await API.recordScore('c1', 'm1', { status: 'running', startOnly: true, ipponsA: [], ipponsB: [] }, 'pw', { id: 'm1', modifiedAt: 0 });
        await tick(0);
        const warned = alerts.filter((a) => a.kind === 'lineup_warning');
        expect(warned).toHaveLength(1);
        expect(warned[0].detail).toBe(WARNING);
    });
});
