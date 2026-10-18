// Stage 6 review 10, m5: the queue's index of status moves
// (_statusEntriesByBase) holds no taps, so a tap entering or leaving the queue
// never makes it again: the court console asks for it on every render, and a
// rescan after every tap cost a scan and sort of the whole queue.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

let mod;
let API;
let _orig;

beforeEach(async () => {
    vi.useFakeTimers();
    _orig = { fetch: global.fetch, ES: global.EventSource, ls: global.localStorage };
    global.EventSource = class FakeES { constructor() { this.close = () => {}; } };
    global.EventSource.OPEN = 1;
    let store = {};
    global.localStorage = {
        getItem: (k) => (k in store ? store[k] : null),
        setItem: (k, v) => { store[k] = String(v); },
        removeItem: (k) => { delete store[k]; },
        clear: () => { store = {}; },
    };
    global.fetch = vi.fn((url, opts) => {
        if (String(url).endsWith('/score-events')) {
            const body = JSON.parse(opts.body);
            return Promise.resolve({
                ok: true, status: 200,
                json: async () => ({ outcomes: body.events.map((e) => ({ id: e.id, outcome: 'applied' })), match: { id: 'm1', scoreVersion: 2 } }),
            });
        }
        return Promise.reject(new TypeError('offline'));
    });
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

describe('the status-move index', () => {
    it('is not made again for a tap that enters or leaves the queue', async () => {
        const before = mod._statusEntriesByBase();
        API.recordTap('c1', 'm1', { kind: 'ippon.add', pos: 0, side: 'a', letter: 'M' }, 'pw', shown);
        expect(mod._statusEntriesByBase(), 'a tap queued').toBe(before);
        await tick(1_000);
        expect(mod._statusEntriesByBase(), 'a tap answered and gone').toBe(before);
    });
});
