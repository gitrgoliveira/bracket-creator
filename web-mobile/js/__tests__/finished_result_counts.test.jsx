// bc-evts stage 3, review round 2, R5: `terminal` on an outbox entry says how
// it is sent (its own method and url), not that it is a finished result. A
// court move, a time change, a lineup save or a reopen is a CHANGE: it is
// never counted as a finished result in the held-writes counts, the sign-out
// dialog or the pass's "sent" alert. One predicate, isFinishedResultWrite
// (write_result.jsx), says which entries are finished results.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { isFinishedResultWrite, queuedWritesNoun, heldWritesText } from '../write_result.jsx';

let API;
let mod;
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
    global.fetch = vi.fn(() => Promise.reject(new TypeError('offline')));
    vi.resetModules();
    mod = await import('../api_client.jsx');
    API = mod.API;
});

afterEach(() => {
    if (API && typeof API.clearQueue === 'function') API.clearQueue();
    vi.clearAllTimers();
    vi.useRealTimers();
    vi.resetModules();
    global.fetch = _orig.fetch;
    global.EventSource = _orig.ES;
    global.localStorage = _orig.ls;
});

async function flushMicrotasks() { for (let i = 0; i < 30; i++) await Promise.resolve(); }
async function tick(ms = 0) { await vi.advanceTimersByTimeAsync(ms); await flushMicrotasks(); }

describe('isFinishedResultWrite (R5)', () => {
    it('names a finished score, a decision, an override and a representative bout change', () => {
        expect(isFinishedResultWrite({ kind: 'score', terminal: true })).toBe(true);
        expect(isFinishedResultWrite({ kind: 'decision', terminal: true })).toBe(true);
        expect(isFinishedResultWrite({ kind: 'override', terminal: true })).toBe(true);
        expect(isFinishedResultWrite({ kind: 'daihyosen', terminal: true })).toBe(true);
    });
    it('does not name a running score write, a tap or any other change', () => {
        expect(isFinishedResultWrite({ kind: 'score', terminal: false })).toBe(false);
        expect(isFinishedResultWrite({ kind: 'evt', terminal: false })).toBe(false);
        for (const kind of ['court', 'time', 'lineup', 'reopen', 'requeue', 'revert', 'kachinuki', 'member', 'reinstate']) {
            expect(isFinishedResultWrite({ kind, terminal: true })).toBe(false);
        }
    });
    it('words a count with no finished result as changes', () => {
        expect(queuedWritesNoun(0, 1).noun).toBe('change');
        expect(queuedWritesNoun(0, 3).noun).toBe('changes');
        expect(queuedWritesNoun(2, 3).noun).toBe('finished results');
        expect(heldWritesText('offline', { total: 2, terminal: 0 })).toBe('Offline: 2 changes not sent');
    });
});

describe('the outbox counts only finished results as results (R5)', () => {
    it('a queued court move, time change, lineup save and reopen count as changes', async () => {
        API.moveMatchCourt('c1', 'm1', 'B', 'pw');
        API.updateMatchTime('c1', 'm1', '10:00', 'pw');
        API.putMatchLineup('c1', 't1', 'm1', { senpo: 'A' }, 'pw', undefined, ['senpo']);
        API.reopenMatch('c1', 'm2', 'pw');
        await tick();
        const counts = API.unsentWrites();
        expect(counts.total).toBe(4);
        expect(counts.terminal).toBe(0);
    });

    it('a held court move that lands raises no "results sent" alert', async () => {
        const alerts = [];
        mod.subscribeQueueAlert((a) => alerts.push(a));
        API.moveMatchCourt('c1', 'm1', 'B', 'pw');
        await tick(); // the first send fails offline: the entry is held
        global.fetch = vi.fn((url) => (String(url).includes('/api/time')
            ? Promise.reject(new TypeError('clock'))
            : Promise.resolve({ ok: true, status: 200, headers: { get: () => null }, json: async () => ({}), text: async () => '{}' })));
        window.dispatchEvent(new Event('online'));
        await tick(10000);
        expect(API.unsentWrites().total).toBe(0);
        expect(alerts.filter((a) => a.kind === 'sent')).toEqual([]);
    });
});
