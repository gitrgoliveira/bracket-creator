// The finished-league warning through the outbox (design item 3): the
// organiser's "Change it anyway" resends a correction with the request-only
// confirmFinished, which rides the descriptor exactly as forceCourt does
// (outbox_force_court.test.jsx): sent on the payload, persisted, rehydrated,
// adopted across tabs, kept when a write takes a queued one's place, never
// broadcast to a display. The 409 league_finished refusal is parsed onto the
// thrown Error as `.leagueFinished`, on the score and the decision doors.

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
const conflict = (body) => Promise.resolve({ ok: false, status: 409, json: async () => body });
const stored = () => JSON.parse(_lsStore[QUEUE_STORAGE_KEY] || '[]');

const SERVER_SENTENCE = 'This league has finished. Changing this result can change the final table and podium, so it was not saved: correct it again and confirm the change.';
const correction = (extra = {}) => ({ status: 'completed', winner: { id: 'p2', name: 'Bob' }, correctionReason: 'typo', ...extra });

describe('confirmFinished outbox plumbing', () => {
    it('recordScore sends confirmFinished: true when the patch carries it, and no key otherwise', async () => {
        mockFetch(() => ok({ id: 'm1', status: 'completed' }));

        await (await API.recordScore('c1', 'm1', correction({ confirmFinished: true }), 'pw', { id: 'm1' })).settled;
        await (await API.recordScore('c1', 'm2', correction(), 'pw', { id: 'm2' })).settled;
        await flushMicrotasks();

        expect(writes().length).toBe(2);
        expect(writes()[0][0]).toBe('/api/competitions/c1/matches/m1/score');
        expect(bodyOf(writes()[0]).confirmFinished).toBe(true);
        expect('confirmFinished' in bodyOf(writes()[1])).toBe(false);
    });

    it('is held in browser storage and still sent after a reload', async () => {
        API.recordScore('c1', 'm1', correction({ confirmFinished: true }), 'pw', { id: 'm1' });
        await flushMicrotasks();
        const entries = stored();
        expect(entries.length).toBe(1);
        expect(entries[0][1].payload.confirmFinished).toBe(true);

        const replayed = [];
        mockFetch((url, opts) => {
            replayed.push({ url: String(url), body: JSON.parse(opts.body) });
            return ok({ id: 'm1', status: 'completed' });
        });
        await load();
        await tick(50);

        expect(replayed.length).toBe(1);
        expect(replayed[0].url).toBe('/api/competitions/c1/matches/m1/score');
        expect(replayed[0].body.confirmFinished).toBe(true);
    });

    it('an entry another tab stored with confirmFinished is taken up and sent with it', async () => {
        const delivered = [];
        mockFetch((url, opts) => {
            delivered.push({ url: String(url), body: JSON.parse(opts.body) });
            return ok({ id: 'm1', status: 'completed' });
        });
        const foreign = ['c1:m1\u001fother-write-1', {
            compID: 'c1', matchID: 'm1', kind: 'score', terminal: false, enqueuedAt: Date.now(),
            payload: { status: 'running', ipponsA: ['M'], confirmFinished: true },
            password: 'pw',
        }];
        const oldVal = _lsStore[QUEUE_STORAGE_KEY] || '[]';
        const newVal = JSON.stringify([foreign]);
        _lsStore[QUEUE_STORAGE_KEY] = newVal;
        if (window.__bcStoredQueueHandler) {
            window.__bcStoredQueueHandler({ key: QUEUE_STORAGE_KEY, oldValue: oldVal, newValue: newVal });
        }
        await tick(50);

        expect(delivered.length).toBe(1);
        expect(delivered[0].body.confirmFinished).toBe(true);
    });

    it('a write taking a queued one\'s place keeps confirmFinished when either carried it', async () => {
        API.recordScore('c1', 'm1', { status: 'running', ipponsA: ['M'], confirmFinished: true, changed: ['points'] }, 'pw', { id: 'm1', modifiedAt: 0 });
        await flushMicrotasks();
        API.recordScore('c1', 'm1', { status: 'running', ipponsA: ['M'], encho: { periodCount: 1 }, changed: ['encho'] }, 'pw', { id: 'm1', modifiedAt: 0 });
        await flushMicrotasks();
        let q = stored();
        expect(q.length).toBe(1);
        expect(q[0][1].payload.confirmFinished).toBe(true);

        API.clearQueue();
        API.recordScore('c1', 'm2', { status: 'running', ipponsA: ['M'], changed: ['points'] }, 'pw', { id: 'm2', modifiedAt: 0 });
        await flushMicrotasks();
        API.recordScore('c1', 'm2', { status: 'running', ipponsA: ['M'], encho: { periodCount: 1 }, confirmFinished: true, changed: ['encho'] }, 'pw', { id: 'm2', modifiedAt: 0 });
        await flushMicrotasks();
        q = stored();
        expect(q.length).toBe(1);
        expect(q[0][1].payload.confirmFinished).toBe(true);
    });

    it('a display broadcast never carries confirmFinished', async () => {
        mockFetch(() => ok({ id: 'm1', status: 'completed' }));
        const { bridge } = await import('../court_bridge.jsx');
        const publishSpy = vi.spyOn(bridge, 'publish');
        const match = { id: 'm1', court: 'A', status: 'completed', sideA: { id: 'p1' }, sideB: { id: 'p2' } };
        await (await API.recordScore('c1', 'm1', correction({ confirmFinished: true }), 'pw', match)).settled;
        await flushMicrotasks();

        const patches = publishSpy.mock.calls.filter(([type]) => type === 'patch');
        expect(patches.length).toBeGreaterThan(0);
        expect('confirmFinished' in patches[0][3].result).toBe(false);
        publishSpy.mockRestore();
    });

    it('a decision resent with confirmFinished carries it in its body, through storage', async () => {
        API.recordDecision('c1', 'm1', { decision: 'kiken-injury', decisionBy: 'shiro', confirmFinished: true }, 'pw');
        await flushMicrotasks();
        const entry = stored().find(([k]) => k.startsWith('c1:m1'));
        expect(entry).toBeDefined();
        expect(entry[1].payload.confirmFinished).toBe(true);

        const replayed = [];
        mockFetch((url, opts) => {
            replayed.push({ url: String(url), body: JSON.parse(opts.body) });
            return ok({ id: 'm1', status: 'completed' });
        });
        await load();
        await tick(50);
        const sent = replayed.find((r) => r.url === '/api/competitions/c1/matches/m1/decision');
        expect(sent).toBeDefined();
        expect(sent.body.confirmFinished).toBe(true);
    });
});

describe('the league_finished refusal', () => {
    const refusalBody = { error: 'league_finished', message: SERVER_SENTENCE, removedCourts: ['C'] };

    it('rejects a score write\'s settled with the server\'s sentence and the removed courts', async () => {
        const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
        mockFetch(() => conflict(refusalBody));
        let claimed = null;
        const answer = await API.recordScore('c1', 'm1', correction(), 'pw', { id: 'm1' }, {
            onRefusal: (e) => { claimed = e; return true; },
        });
        answer.settled.catch(() => {});
        await tick(50);

        expect(claimed).toBeTruthy();
        expect(claimed.message).toBe(SERVER_SENTENCE);
        expect(claimed.code).toBe('league_finished');
        expect(claimed.leagueFinished).toEqual({ removedCourts: ['C'] });
        expect(stored().length).toBe(0);
        warnSpy.mockRestore();
    });

    it('rejects a decision alike', async () => {
        const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
        mockFetch(() => conflict({ ...refusalBody, removedCourts: [] }));
        let claimed = null;
        const answer = await API.recordDecision('c1', 'm1', { decision: 'kiken-injury', decisionBy: 'shiro' }, 'pw', {
            onRefusal: (e) => { claimed = e; return true; },
        });
        answer.settled.catch(() => {});
        await tick(50);

        expect(claimed).toBeTruthy();
        expect(claimed.leagueFinished).toEqual({ removedCourts: [] });
        expect(claimed.message).toBe(SERVER_SENTENCE);
        warnSpy.mockRestore();
    });

    it('rejects a reopen alike, marked as a reopen, and sends its confirmation', async () => {
        const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
        mockFetch(() => conflict(refusalBody));
        let claimed = null;
        const answer = await API.reopenMatch('c1', 'Pool A-0', 'pw', {
            onRefusal: (e) => { claimed = e; return true; },
        });
        answer.settled.catch(() => {});
        await tick(50);

        expect(claimed).toBeTruthy();
        expect(claimed.leagueFinished).toEqual({ removedCourts: ['C'], reopen: true });
        expect(writes().length).toBe(1);
        expect(bodyOf(writes()[0]).confirmFinished).toBeUndefined();

        mockFetch(() => ok({ reopenedMatches: [] }));
        await (await API.reopenMatch('c1', 'Pool A-0', 'pw', { confirmFinished: true })).settled;
        expect(bodyOf(writes()[0]).confirmFinished).toBe(true);
        warnSpy.mockRestore();
    });
});
