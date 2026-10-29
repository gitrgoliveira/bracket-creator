// Unit tests for bc-adna forceCourt through the outbox (p2-adna-wire).
// Verifies that forceCourt rides on the descriptors of score writes and reopens,
// survives persist, rehydrate, and cross-tab adopt, is merged when score writes coalesce,
// is stripped before display broadcast, and is absent when not requested.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

const QUEUE_STORAGE_KEY = 'bc_write_queue_v2';

let mod;
let API;
let _lsStore;
let _orig;

async function load() {
    vi.resetModules();
    mod = await import('../api_client.jsx');
    API = mod.API;
}

beforeEach(async () => {
    vi.useFakeTimers();
    _orig = { fetch: global.fetch, ES: global.EventSource, ls: global.localStorage, BC: global.BroadcastChannel };
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
    await load();
});

afterEach(() => {
    if (API && typeof API.clearQueue === 'function') API.clearQueue();
    vi.clearAllTimers();
    vi.useRealTimers();
    vi.resetModules();
    global.fetch = _orig.fetch;
    global.EventSource = _orig.ES;
    global.localStorage = _orig.ls;
    global.BroadcastChannel = _orig.BC;
});

async function flushMicrotasks() {
    for (let i = 0; i < 40; i++) await Promise.resolve();
}

async function tick(ms = 0) {
    await vi.advanceTimersByTimeAsync(ms);
    await flushMicrotasks();
}

const isClock = (url) => String(url).includes('/api/time');

function mockFetch(impl) {
    global.fetch = vi.fn((url, opts) => (isClock(url) ? Promise.reject(new TypeError('clock')) : impl(url, opts)));
}

const writes = () => global.fetch.mock.calls.filter(([u]) => !isClock(u));
const bodyOf = (call) => JSON.parse(call[1].body);
const ok = (body) => Promise.resolve({ ok: true, status: 200, json: async () => body });
const stored = () => JSON.parse(_lsStore[QUEUE_STORAGE_KEY] || '[]');

describe('forceCourt outbox plumbing (bc-adna / p2-adna-wire)', () => {
    // 1. recordScore with a patch carrying forceCourt: true sends a PUT .../score whose
    // JSON body has forceCourt: true; one without it sends no forceCourt key.
    it('recordScore with forceCourt: true sends PUT /score with forceCourt: true, and without it sends no forceCourt key', async () => {
        mockFetch(() => ok({ id: 'm1', status: 'running' }));

        // With forceCourt: true
        const p1 = API.recordScore('c1', 'm1', { status: 'running', forceCourt: true }, 'pw', { id: 'm1' });
        await (await p1).settled;
        await flushMicrotasks();

        expect(writes().length).toBe(1);
        expect(writes()[0][0]).toBe('/api/competitions/c1/matches/m1/score');
        expect(writes()[0][1].method).toBe('PUT');
        const body1 = bodyOf(writes()[0]);
        expect(body1.forceCourt).toBe(true);

        // Without forceCourt
        const p2 = API.recordScore('c1', 'm2', { status: 'running' }, 'pw', { id: 'm2' });
        await (await p2).settled;
        await flushMicrotasks();

        expect(writes().length).toBe(2);
        expect(writes()[1][0]).toBe('/api/competitions/c1/matches/m2/score');
        const body2 = bodyOf(writes()[1]);
        expect('forceCourt' in body2).toBe(false);
        expect(body2.forceCourt).toBeUndefined();
    });

    // 2. The queued (persisted) entry in browser storage (key bc_write_queue_v2, constant QUEUE_STORAGE_KEY)
    // holds forceCourt: true in its payload while the request is held (e.g. fetch rejecting / offline),
    // and after a module reload (rehydrate) the replayed request still carries forceCourt: true.
    it('queued entry in storage holds forceCourt: true while held, and replayed request after module reload carries forceCourt: true', async () => {
        mockFetch(() => Promise.reject(new TypeError('offline')));
        API.recordScore('c1', 'm1', { status: 'running', forceCourt: true }, 'pw', { id: 'm1' });
        await flushMicrotasks();

        const entries = stored();
        expect(entries.length).toBe(1);
        expect(entries[0][1].payload.forceCourt).toBe(true);

        // Reload module (triggers _rehydrateQueue from localStorage)
        const replayed = [];
        mockFetch((url, opts) => {
            replayed.push({ url: String(url), method: opts.method, body: JSON.parse(opts.body) });
            return ok({ id: 'm1', status: 'running' });
        });

        await load();
        await tick(50);

        expect(replayed.length).toBe(1);
        expect(replayed[0].url).toBe('/api/competitions/c1/matches/m1/score');
        expect(replayed[0].method).toBe('PUT');
        expect(replayed[0].body.forceCourt).toBe(true);
    });

    // 3. Cross-tab adopt: a stored entry written by "another tab" carrying forceCourt: true is taken up and sent with forceCourt: true.
    it('cross-tab adopt: a stored entry written by another tab carrying forceCourt: true is taken up and sent with forceCourt: true', async () => {
        const delivered = [];
        mockFetch((url, opts) => {
            delivered.push({ url: String(url), method: opts.method, body: JSON.parse(opts.body) });
            return ok({ id: 'm1', status: 'running' });
        });

        const foreignEntry = [
            'c1:m1\u001fother-write-1',
            {
                compID: 'c1',
                matchID: 'm1',
                kind: 'score',
                terminal: false,
                enqueuedAt: Date.now(),
                payload: { status: 'running', ipponsA: ['M'], forceCourt: true },
                password: 'pw',
            },
        ];

        const oldVal = _lsStore[QUEUE_STORAGE_KEY] || '[]';
        const newVal = JSON.stringify([foreignEntry]);
        _lsStore[QUEUE_STORAGE_KEY] = newVal;

        if (window.__bcStoredQueueHandler) {
            window.__bcStoredQueueHandler({ key: QUEUE_STORAGE_KEY, oldValue: oldVal, newValue: newVal });
        }
        await tick(50);

        expect(delivered.length).toBe(1);
        expect(delivered[0].url).toBe('/api/competitions/c1/matches/m1/score');
        expect(delivered[0].method).toBe('PUT');
        expect(delivered[0].body.forceCourt).toBe(true);
    });

    // 4. Coalesce: two running score writes for one match while offline, only the FIRST carrying forceCourt:
    // the queue holds one entry and its payload carries forceCourt: true; and the reverse (only the second carries it)
    // also ends with forceCourt: true.
    it('coalesce: two running score writes while offline keep forceCourt: true when either carried it', async () => {
        mockFetch(() => Promise.reject(new TypeError('offline')));

        // Case A: first carries forceCourt: true, second does not.
        API.recordScore('c1', 'm1', { status: 'running', ipponsA: ['M'], forceCourt: true, changed: ['points'] }, 'pw', { id: 'm1', modifiedAt: 0 });
        await flushMicrotasks();
        API.recordScore('c1', 'm1', { status: 'running', ipponsA: ['M'], encho: { periodCount: 1 }, changed: ['encho'] }, 'pw', { id: 'm1', modifiedAt: 0 });
        await flushMicrotasks();

        let q = stored();
        expect(q.length).toBe(1);
        expect(q[0][1].payload.forceCourt).toBe(true);
        expect(q[0][1].payload.changed.sort()).toEqual(['encho', 'points']);

        // Clear queue before Case B.
        API.clearQueue();
        expect(stored().length).toBe(0);

        // Case B: first has no forceCourt, second carries forceCourt: true.
        API.recordScore('c1', 'm2', { status: 'running', ipponsA: ['M'], changed: ['points'] }, 'pw', { id: 'm2', modifiedAt: 0 });
        await flushMicrotasks();
        API.recordScore('c1', 'm2', { status: 'running', ipponsA: ['M'], encho: { periodCount: 1 }, forceCourt: true, changed: ['encho'] }, 'pw', { id: 'm2', modifiedAt: 0 });
        await flushMicrotasks();

        q = stored();
        expect(q.length).toBe(1);
        expect(q[0][1].payload.forceCourt).toBe(true);
        expect(q[0][1].payload.changed.sort()).toEqual(['encho', 'points']);
    });

    // 5. A display broadcast (the court BroadcastChannel publish done by _broadcastPatch) never carries forceCourt.
    it('a display broadcast published by _broadcastPatch never carries forceCourt', async () => {
        mockFetch(() => ok({ id: 'm1', status: 'running' }));
        // load() reset the module registry, so the bridge api_client.jsx holds
        // is the one this import resolves to now, not a top-level import's.
        const { bridge } = await import('../court_bridge.jsx');
        const publishSpy = vi.spyOn(bridge, 'publish');

        const match = { id: 'm1', court: 'A', status: 'running', sideA: { id: 'p1' }, sideB: { id: 'p2' } };
        const p = API.recordScore('c1', 'm1', { status: 'running', forceCourt: true }, 'pw', match);
        await (await p).settled;
        await flushMicrotasks();

        const patchCalls = publishSpy.mock.calls.filter(([type]) => type === 'patch');
        expect(patchCalls.length).toBeGreaterThan(0);
        const [, court, compID, msg] = patchCalls[0];
        expect(court).toBe('A');
        expect(compID).toBe('c1');
        expect('forceCourt' in msg.result).toBe(false);
        expect(msg.result.forceCourt).toBeUndefined();

        publishSpy.mockRestore();
    });

    // 6. reopenMatch with { forceCourt: true } sends a body with forceCourt: true beside reason;
    // without it no forceCourt key; and the body survives persist + rehydrate.
    it('reopenMatch with forceCourt: true sends body with forceCourt: true beside reason, without it sends no forceCourt key, and survives persist + rehydrate', async () => {
        mockFetch(() => ok({ reopenedMatches: [{ id: 'm1' }] }));

        // With forceCourt: true
        const p1 = API.reopenMatch('c1', 'm1', 'pw', { reason: 'organizer confirmed', forceCourt: true });
        await (await p1).settled;
        await flushMicrotasks();

        expect(writes().length).toBe(1);
        expect(writes()[0][0]).toBe('/api/competitions/c1/matches/m1/reopen');
        expect(writes()[0][1].method).toBe('POST');
        const b1 = bodyOf(writes()[0]);
        expect(b1.reason).toBe('organizer confirmed');
        expect(b1.forceCourt).toBe(true);

        // Without forceCourt
        const p2 = API.reopenMatch('c1', 'm2', 'pw', { reason: 'regular reopen' });
        await (await p2).settled;
        await flushMicrotasks();

        expect(writes().length).toBe(2);
        expect(writes()[1][0]).toBe('/api/competitions/c1/matches/m2/reopen');
        const b2 = bodyOf(writes()[1]);
        expect(b2.reason).toBe('regular reopen');
        expect('forceCourt' in b2).toBe(false);
        expect(b2.forceCourt).toBeUndefined();

        // Survives persist + rehydrate while offline
        mockFetch(() => Promise.reject(new TypeError('offline')));
        API.reopenMatch('c1', 'm3', 'pw', { reason: 'persisted reopen', forceCourt: true });
        await flushMicrotasks();

        // Check storage holding it
        const entries = stored();
        const m3Entry = entries.find(([k]) => k.startsWith('c1:m3'));
        expect(m3Entry).toBeDefined();
        expect(m3Entry[1].payload.reason).toBe('persisted reopen');
        expect(m3Entry[1].payload.forceCourt).toBe(true);

        // Module reload to rehydrate
        const replayed = [];
        mockFetch((url, opts) => {
            replayed.push({ url: String(url), method: opts.method, body: JSON.parse(opts.body) });
            return ok({ reopenedMatches: [{ id: 'm3' }] });
        });

        await load();
        await tick(50);

        const m3Replayed = replayed.find((r) => r.url === '/api/competitions/c1/matches/m3/reopen');
        expect(m3Replayed).toBeDefined();
        expect(m3Replayed.method).toBe('POST');
        expect(m3Replayed.body.reason).toBe('persisted reopen');
        expect(m3Replayed.body.forceCourt).toBe(true);
    });
});
