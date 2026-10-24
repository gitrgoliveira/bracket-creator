// Stage 6 review 11, item 11 (b): the outbox replays a DELETE only for the
// doors that queue one: the representative bout's remove, a kachinuki bout's
// removal, a team member's name cleared and a match's own lineup removed. A
// reset of a competition's pool-rank overrides is no outbox entry (no build
// that ever shipped queued one), so a stored entry naming it is not sent.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

let mod;
let _lsStore;
let _orig;

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

const stored = (key, url, kind) => [key, {
    compID: 'c1', matchID: 'm1', password: 'pw', kind, terminal: true, method: 'DELETE',
    url, enqueuedAt: Date.now(), payload: {},
}];

async function replay(entry) {
    _lsStore.bc_write_queue_v2 = JSON.stringify([entry]);
    const sent = [];
    global.fetch = vi.fn((url, opts) => {
        const u = String(url);
        if (u.includes('/api/time')) return Promise.reject(new TypeError('clock'));
        sent.push(`${(opts && opts.method) || 'GET'} ${u}`);
        return Promise.resolve({ ok: true, status: 200, json: async () => ({}) });
    });
    vi.resetModules();
    mod = await import('../api_client.jsx');
    const alerts = [];
    mod.subscribeQueueAlert((a) => alerts.push(a));
    window.dispatchEvent(new Event('online'));
    await tick(5_000);
    return { sent, alerts };
}

describe('the DELETEs the outbox replays', () => {
    it('sends a stored removal of a match\'s own lineup', async () => {
        const { sent } = await replay(stored('c1:m1\u001fa', '/api/competitions/c1/teams/t1/match-lineups/m1', 'lineup-remove'));
        expect(sent).toContain('DELETE /api/competitions/c1/teams/t1/match-lineups/m1');
    });

    it('never sends a stored reset of the pool-rank overrides', async () => {
        // The rehydrate says it was not replayed.
        const warned = vi.spyOn(console, 'warn').mockImplementation(() => {});
        const { sent, alerts } = await replay(stored('pool-ranks:c1\u001fa', '/api/competitions/c1/overrides', 'pool-ranks'));
        expect(sent.filter((s) => s.startsWith('DELETE'))).toEqual([]);
        expect(alerts.map((a) => a.kind)).toContain('unreadable');
        expect(warned).toHaveBeenCalled();
        warned.mockRestore();
    });
});
