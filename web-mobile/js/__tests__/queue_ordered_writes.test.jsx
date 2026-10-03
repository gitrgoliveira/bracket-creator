// bc-mrgc phase 3 (operator ruling 2026-10-03: "Nothing should be dropped.
// All events must be ordered."): the offline queue keeps, per match, an
// ORDERED list of pending writes. A later running write never replaces a
// queued Finish or a decision, a decision and a score for the same match are
// two entries, and replay sends a match's entries in the order of the stamps
// they carry, never a later one ahead of an earlier one. A queue persisted by
// the previous build (one entry per match, keyed by the match itself) loads
// and replays as it is.
//
// Setup mirrors sync_queue.test.jsx (functional localStorage, EventSource
// stub, every fetch mock wrapped so the background clock poll never reaches it).

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

let mod;
let API;
let _origFetch;
let _origEventSource;
let _origLocalStorage;
let _lsStore;

beforeEach(async () => {
    vi.useFakeTimers();
    vi.resetModules();
    _origEventSource = global.EventSource;
    global.EventSource = class FakeES { constructor() { this.close = () => {}; } };
    global.EventSource.OPEN = 1;
    _origFetch = global.fetch;
    _origLocalStorage = global.localStorage;
    _lsStore = {};
    global.localStorage = {
        getItem: (k) => (k in _lsStore ? _lsStore[k] : null),
        setItem: (k, v) => { _lsStore[k] = String(v); },
        removeItem: (k) => { delete _lsStore[k]; },
        clear: () => { _lsStore = {}; },
    };
    mod = await import('../api_client.jsx');
    API = mod.API;
});

afterEach(() => {
    vi.useRealTimers();
    vi.resetModules();
    global.fetch = _origFetch;
    if (_origEventSource === undefined) delete global.EventSource;
    else global.EventSource = _origEventSource;
    if (_origLocalStorage === undefined) delete global.localStorage;
    else global.localStorage = _origLocalStorage;
});

async function flushMicrotasks() {
    for (let i = 0; i < 12; i++) await Promise.resolve();
}
async function tick(ms = 0) {
    await vi.advanceTimersByTimeAsync(ms);
    await flushMicrotasks();
}

const isClockPoll = (url) => String(url).includes('/api/time');

// A server: `online()` decides whether a send fails; every write that reaches
// it is recorded (path + body), in order.
function server({ answer = () => ({}), status = () => 200 } = {}) {
    const sent = [];
    let up = false;
    global.fetch = vi.fn((url, opts) => {
        if (isClockPoll(url)) return Promise.reject(new TypeError('clock poll: not part of this test'));
        if (!up) return Promise.reject(new TypeError('offline'));
        const body = JSON.parse(opts.body);
        sent.push({ url: String(url), body });
        const code = status(String(url), body);
        return Promise.resolve({
            ok: code >= 200 && code < 300, status: code,
            json: () => Promise.resolve(answer(String(url), body)),
        });
    });
    return { sent, online: () => { up = true; window.dispatchEvent(new Event('online')); }, offline: () => { up = false; } };
}

const storedEntries = () => JSON.parse(localStorage.getItem('bc_write_queue') || '[]');

describe('a later running write never replaces a queued Finish', () => {
    it('keeps the Finish and sends both, the Finish first', async () => {
        const s = server();
        // Finish made offline: queued.
        await API.recordScore('c1', 'm1', { status: 'completed', winner: 'A', ipponsA: ['M', 'K'] }, 'pw', null);
        expect(API.hasPendingTerminalWrite('c1', 'm1')).toBe(true);
        vi.advanceTimersByTime(1000);
        // The editor stayed open (queued keeps it open) and the operator
        // tapped again: a running write, made after the Finish.
        const res = await API.recordScore('c1', 'm1', { status: 'running', ipponsA: ['M', 'K'], ipponsB: ['D'] }, 'pw', null);
        expect(res).toEqual({ queued: true });
        expect(API.hasPendingTerminalWrite('c1', 'm1')).toBe(true); // the Finish is still queued
        expect(storedEntries()).toHaveLength(2);

        s.online();
        await tick(50);
        expect(s.sent.map((w) => w.body.status)).toEqual(['completed', 'running']);
        expect(s.sent[0].body.winner).toBe('A');
        expect(API.hasPendingTerminalWrite('c1', 'm1')).toBe(false);
        expect(localStorage.getItem('bc_write_queue')).toBeNull();
    });

    it('a write that waits behind an earlier one is not sent in a pass where the earlier one fails', async () => {
        const s = server({ status: (url, body) => (body.status === 'completed' ? 503 : 200) });
        await API.recordScore('c1', 'm1', { status: 'completed', winner: 'A' }, 'pw', null);
        vi.advanceTimersByTime(1000);
        await API.recordScore('c1', 'm1', { status: 'running', ipponsA: ['M'] }, 'pw', null);
        s.online();
        await tick(50);
        // The Finish was refused with a retryable 503 and stays queued; the
        // later running write waits behind it rather than land first.
        expect(s.sent.map((w) => w.body.status)).toEqual(['completed']);
        expect(storedEntries()).toHaveLength(2);
    });

    it('running writes still coalesce with the running write queued just before them, never across the Finish', async () => {
        const s = server();
        await API.recordScore('c1', 'm1', { status: 'running', ipponsA: ['M'] }, 'pw', null);
        vi.advanceTimersByTime(1000);
        await API.recordScore('c1', 'm1', { status: 'running', ipponsA: ['M', 'K'] }, 'pw', null);
        expect(storedEntries()).toHaveLength(1);
        vi.advanceTimersByTime(1000);
        await API.recordScore('c1', 'm1', { status: 'completed', winner: 'A', ipponsA: ['M', 'K'] }, 'pw', null);
        // A Finish takes the place of the running write it follows (it carries
        // the later state and claims its groups), as it always did.
        expect(storedEntries()).toHaveLength(1);
        vi.advanceTimersByTime(1000);
        await API.recordScore('c1', 'm1', { status: 'running', ipponsA: ['M', 'K'], ipponsB: ['D'] }, 'pw', null);
        vi.advanceTimersByTime(1000);
        await API.recordScore('c1', 'm1', { status: 'running', ipponsA: ['M', 'K'], ipponsB: ['D', 'K'] }, 'pw', null);
        // The two running writes after the Finish fold into one; the Finish
        // stays its own entry.
        expect(storedEntries()).toHaveLength(2);
        s.online();
        await tick(50);
        expect(s.sent.map((w) => w.body.status)).toEqual(['completed', 'running']);
        expect(s.sent[1].body.ipponsB).toEqual(['D', 'K']);
    });
});

describe('the editor\'s Retry of a queued Finish', () => {
    it('re-sending the queued Finish restates it: still one entry, one send', async () => {
        const s = server();
        const finish = { status: 'completed', winner: 'A', ipponsA: ['M', 'K'] };
        await API.recordScore('c1', 'm1', { ...finish }, 'pw', null);
        vi.advanceTimersByTime(1000);
        // "Retry now" re-invokes the same submit: a later Finish of the same
        // result, which waits behind the queued one and takes its place.
        const res = await API.recordScore('c1', 'm1', { ...finish }, 'pw', null);
        expect(res).toEqual({ queued: true });
        expect(storedEntries()).toHaveLength(1);
        s.online();
        await tick(50);
        expect(s.sent).toHaveLength(1);
        expect(s.sent[0].body).toMatchObject({ status: 'completed', winner: 'A' });
    });

    it('a running write never takes the place of the queued Finish', async () => {
        server();
        await API.recordScore('c1', 'm1', { status: 'completed', winner: 'A' }, 'pw', null);
        vi.advanceTimersByTime(1000);
        await API.recordScore('c1', 'm1', { status: 'running', ipponsA: ['M'] }, 'pw', null);
        expect(storedEntries().map(([, d]) => d.terminal)).toEqual(expect.arrayContaining([true, false]));
    });
});

describe('a decision and a score for the same match are both kept', () => {
    it('a decision made after a queued score waits behind it, and both are sent in order', async () => {
        const s = server();
        await API.recordScore('c1', 'm1', { status: 'running', ipponsA: ['M'] }, 'pw', null);
        vi.advanceTimersByTime(1000);
        const res = await API.recordDecision('c1', 'm1', { decision: 'kiken-voluntary', decisionBy: 'aka' }, 'pw');
        expect(res).toEqual({ queued: true });
        expect(storedEntries()).toHaveLength(2);
        s.online();
        await tick(50);
        expect(s.sent.map((w) => w.url.split('/').pop())).toEqual(['score', 'decision']);
        expect(s.sent[0].body.ipponsA).toEqual(['M']);
        expect(s.sent[1].body.decision).toBe('kiken-voluntary');
    });

    it('a score made after a queued decision does not replace it, and both are sent in order', async () => {
        const s = server();
        await API.recordDecision('c1', 'm1', { decision: 'fusenpai', decisionBy: 'shiro' }, 'pw');
        vi.advanceTimersByTime(1000);
        const res = await API.recordScore('c1', 'm1', { status: 'running', ipponsA: ['M'] }, 'pw', null);
        expect(res).toEqual({ queued: true });
        expect(storedEntries()).toHaveLength(2);
        s.online();
        await tick(50);
        expect(s.sent.map((w) => w.url.split('/').pop())).toEqual(['decision', 'score']);
    });
});

describe('a queue persisted by the previous build', () => {
    // The previous build kept ONE entry per match, keyed by the match itself
    // ('c1:m1'). Such a key is its own base, so the entry is a valid entry of
    // the ordered queue: it loads, keeps its place, and replays.
    const oldShape = (key, d) => [key, { enqueuedAt: Date.now() - 60_000, password: 'pw', ...d }];

    it('loads, and a new write for the same match is queued beside it and sent after it', async () => {
        localStorage.setItem('bc_write_queue', JSON.stringify([
            oldShape('c1:m1', {
                compID: 'c1', matchID: 'm1', kind: 'decision', terminal: true, method: 'POST',
                url: '/api/competitions/c1/matches/m1/decision',
                payload: { decision: 'kiken-voluntary', decisionBy: 'aka', modifiedAt: Date.now() - 60_000 },
            }),
            oldShape('override:c1:m2', {
                compID: 'c1', matchID: 'm2', kind: 'override', terminal: true, method: 'PUT',
                url: '/api/competitions/c1/matches/m2/override-winner',
                payload: { winnerName: 'Alice', modifiedAt: Date.now() - 60_000 },
            }),
        ]));
        vi.resetModules();
        mod = await import('../api_client.jsx');
        API = mod.API;
        const s = server();
        expect(API.hasPendingTerminalWrite('c1', 'm1')).toBe(true);
        await API.recordScore('c1', 'm1', { status: 'running', ipponsA: ['M'] }, 'pw', null);
        expect(storedEntries()).toHaveLength(3);
        s.online();
        await tick(50);
        const m1 = s.sent.filter((w) => w.url.includes('/matches/m1/')).map((w) => w.url.split('/').pop());
        expect(m1).toEqual(['decision', 'score']);
        expect(s.sent.some((w) => w.url.endsWith('/matches/m2/override-winner'))).toBe(true);
        expect(localStorage.getItem('bc_write_queue')).toBeNull();
    });
});

describe('override-winner: a held assertion is surfaced like a superseded score', () => {
    it('a direct assertion the server held returns heldGroups', async () => {
        const s = server({ answer: () => ({ applied: false, reason: 'superseded', heldGroups: ['result'] }) });
        s.online();
        await flushMicrotasks();
        const res = await API.overrideBracketWinner('c1', 'm1', 'Alice', 'pw');
        expect(res).toMatchObject({ applied: false, reason: 'superseded', heldGroups: ['result'] });
    });

    it('a queued assertion the server held raises the superseded alert, not only a resync', async () => {
        const s = server({ answer: () => ({ applied: false, reason: 'superseded', heldGroups: ['result'] }) });
        await API.overrideBracketWinner('c1', 'm1', 'Alice', 'pw');
        const alerts = [];
        const unsub = mod.subscribeQueueAlert((a) => alerts.push(a));
        s.online();
        await tick(50);
        unsub();
        expect(alerts.filter((a) => a.kind === 'superseded')).toEqual([
            expect.objectContaining({ count: 1, compID: 'c1', matchID: 'm1' }),
        ]);
    });
});

describe('engi: the finish is stamped like every other write', () => {
    it('an engi Save carries modifiedAt and the groups it changed', async () => {
        const s = server();
        s.online();
        await flushMicrotasks();
        await API.recordScore('c1', 'm1', { status: 'completed', flagsA: 3, flagsB: 0, changed: ['result', 'flags'] }, 'pw', null);
        expect(s.sent).toHaveLength(1);
        expect(Number.isInteger(s.sent[0].body.modifiedAt)).toBe(true);
        expect(s.sent[0].body.modifiedAt).toBeGreaterThan(0);
        expect(s.sent[0].body).toMatchObject({ flagsA: 3, flagsB: 0, status: 'completed' });
    });
});
