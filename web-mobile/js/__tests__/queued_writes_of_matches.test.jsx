// bc-sseq FR9: API.queuedWritesOfMatches, what the court console's loss band
// counts: this tab's outbox entries for the given matches, of any kind, that
// the server has not answered yet.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

let API;
let _lsStore;
let _orig;

beforeEach(async () => {
    vi.useFakeTimers();
    vi.resetModules();
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
    // The server never answers: every write stays queued.
    global.fetch = vi.fn(() => new Promise(() => {}));
    ({ API } = await import('../api_client.jsx'));
    vi.spyOn(console, 'warn').mockImplementation(() => {});
});

afterEach(() => {
    if (API && typeof API.clearQueue === 'function') API.clearQueue();
    console.warn.mockRestore();
    vi.clearAllTimers();
    vi.useRealTimers();
    vi.resetModules();
    global.fetch = _orig.fetch;
    global.EventSource = _orig.ES;
    global.localStorage = _orig.ls;
});

describe('queuedWritesOfMatches', () => {
    it('counts the queued entries of the matches asked, whatever their kind, and nothing else', async () => {
        const m1 = { compId: 'c1', id: 'm1', court: 'A', status: 'running', modifiedAt: 1 };
        API.recordTap('c1', 'm1', { type: 'ippon.add', side: 'a', letter: 'M' }, 'pw', m1);
        API.recordTap('c1', 'm1', { type: 'ippon.add', side: 'b', letter: 'K' }, 'pw', m1);
        API.recordTap('c1', 'm2', { type: 'ippon.add', side: 'a', letter: 'D' }, 'pw', { ...m1, id: 'm2' });
        API.recordTap('c2', 'm1', { type: 'ippon.add', side: 'a', letter: 'M' }, 'pw', { ...m1, compId: 'c2' });
        expect(API.queuedWritesOfMatches([{ compId: 'c1', id: 'm1' }])).toBe(2);
        expect(API.queuedWritesOfMatches([{ compId: 'c1', id: 'm1' }, { compId: 'c1', id: 'm2' }])).toBe(3);
        // A match named twice counts once.
        expect(API.queuedWritesOfMatches([{ compId: 'c1', id: 'm2' }, { compId: 'c1', id: 'm2' }])).toBe(1);
        expect(API.queuedWritesOfMatches([{ compId: 'c3', id: 'm1' }])).toBe(0);
        expect(API.queuedWritesOfMatches([])).toBe(0);
        expect(API.queuedWritesOfMatches(null)).toBe(0);
        API.clearQueue();
        expect(API.queuedWritesOfMatches([{ compId: 'c1', id: 'm1' }])).toBe(0);
    });
});
