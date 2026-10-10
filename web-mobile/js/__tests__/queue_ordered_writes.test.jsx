// bc-mrgc phase 3 (operator ruling 2026-10-03: "Nothing should be dropped.
// All events must be ordered."): the offline queue keeps, per match, a list
// of pending writes. A later running write never replaces a queued Finish or
// a decision, a decision and a score for the same match are two entries, and
// a flush pass sends the entries in the order of the stamps they carry. Each
// entry is sent on its own: one that keeps failing holds back nothing after
// it, and a new write goes straight to the server whatever is queued, because
// the server orders a match write by its stamp, not by when it arrives. A
// queue persisted by the previous build (one entry per match, keyed by the
// match itself) loads and replays as it is.
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

// Let a flush pass started while offline end (its sends fail at once), so
// the pass `online()` starts holds every queued entry, in replay order.
const settleOfflinePass = () => tick(0);

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

        await settleOfflinePass();
        s.online();
        await tick(50);
        expect(s.sent.map((w) => w.body.status)).toEqual(['completed', 'running']);
        expect(s.sent[0].body.winner).toBe('A');
        expect(API.hasPendingTerminalWrite('c1', 'm1')).toBe(false);
        expect(localStorage.getItem('bc_write_queue')).toBeNull();
    });

    it('a Finish that keeps failing holds back nothing: the later write is sent in the same pass and lands', async () => {
        const s = server({ status: (url, body) => (body.status === 'completed' ? 503 : 200) });
        await API.recordScore('c1', 'm1', { status: 'completed', winner: 'A' }, 'pw', null);
        vi.advanceTimersByTime(1000);
        await API.recordScore('c1', 'm1', { status: 'running', ipponsA: ['M'] }, 'pw', null);
        await settleOfflinePass();
        s.online();
        await tick(50);
        // The Finish was refused with a retryable 503 and stays queued; the
        // later running write is still sent after it in the pass and lands.
        expect(s.sent.map((w) => w.body.status)).toEqual(['completed', 'running']);
        expect(storedEntries().map(([, d]) => d.payload.status)).toEqual(['completed']);
    });

    it('with a Finish stuck on the server, a new write goes straight to the server instead of queuing behind it', async () => {
        const s = server({ status: (url, body) => (body.status === 'completed' ? 500 : 200) });
        s.online();
        await flushMicrotasks();
        const finish = await API.recordScore('c1', 'm1', { status: 'completed', winner: 'A' }, 'pw', null);
        expect(finish).toEqual({ queued: true });
        await tick(50);
        const before = s.sent.length;
        // A later point, a decision and a feeder winner, each made while the
        // Finish keeps failing: each reaches the server at once and lands.
        const running = await API.recordScore('c1', 'm1', { status: 'running', ipponsA: ['M'] }, 'pw', null);
        const decision = await API.recordDecision('c1', 'm1', { decision: 'kiken-voluntary', decisionBy: 'aka' }, 'pw');
        const override = await API.overrideBracketWinner('c1', 'm2', 'Alice', 'pw');
        expect(running).not.toEqual(expect.objectContaining({ queued: true }));
        expect(decision).not.toEqual(expect.objectContaining({ queued: true }));
        expect(override).toEqual(expect.objectContaining({ applied: true }));
        const after = s.sent.slice(before).map((w) => w.url.split('/').pop() + (w.body.status ? `:${w.body.status}` : ''));
        expect(after).toEqual(expect.arrayContaining(['score:running', 'decision', 'override-winner']));
        // Only the stuck Finish is still queued.
        expect(storedEntries().map(([, d]) => d.payload.status)).toEqual(['completed']);
    });

    it('Retry on a stuck Finish sends it to the server at once and, when it now lands, clears the queued copy', async () => {
        let refuse = true;
        const s = server({ status: (url, body) => (body.status === 'completed' && refuse ? 503 : 200) });
        s.online();
        await flushMicrotasks();
        const finish = { status: 'completed', winner: 'A', ipponsA: ['M', 'K'] };
        await API.recordScore('c1', 'm1', { ...finish }, 'pw', null);
        await tick(50);
        expect(storedEntries()).toHaveLength(1);
        refuse = false;
        const before = s.sent.length;
        const res = await API.recordScore('c1', 'm1', { ...finish }, 'pw', null);
        // Sent, not queued behind the copy it restates, and it landed.
        expect(res).not.toEqual(expect.objectContaining({ queued: true }));
        expect(s.sent.length).toBe(before + 1);
        expect(localStorage.getItem('bc_write_queue')).toBeNull();
        expect(API.hasPendingTerminalWrite('c1', 'm1')).toBe(false);
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
        await settleOfflinePass();
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
        // "Retry now" re-invokes the same submit while still offline: a later
        // Finish of the same result, which takes the queued one's place.
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
        // Exactly two entries, the Finish first (stamp order): the running
        // write took nobody's place.
        const stored = storedEntries().map(([, d]) => d);
        expect(stored.map((d) => [d.terminal, d.payload.status])).toEqual([[true, 'completed'], [false, 'running']]);
        expect(stored[0].payload.winner).toBe('A');
    });
});

describe('a decision and a score for the same match are both kept', () => {
    it('a decision made after a queued score is its own entry, and both are sent in stamp order', async () => {
        const s = server();
        await API.recordScore('c1', 'm1', { status: 'running', ipponsA: ['M'] }, 'pw', null);
        vi.advanceTimersByTime(1000);
        const res = await API.recordDecision('c1', 'm1', { decision: 'kiken-voluntary', decisionBy: 'aka' }, 'pw');
        expect(res).toEqual({ queued: true });
        expect(storedEntries()).toHaveLength(2);
        await settleOfflinePass();
        s.online();
        await tick(50);
        expect(s.sent.map((w) => w.url.split('/').pop())).toEqual(['score', 'decision']);
        expect(s.sent[0].body.ipponsA).toEqual(['M']);
        expect(s.sent[1].body.decision).toBe('kiken-voluntary');
    });

    it('a score made after a queued decision does not replace it, and both are sent in stamp order', async () => {
        const s = server();
        await API.recordDecision('c1', 'm1', { decision: 'fusenpai', decisionBy: 'shiro' }, 'pw');
        vi.advanceTimersByTime(1000);
        const res = await API.recordScore('c1', 'm1', { status: 'running', ipponsA: ['M'] }, 'pw', null);
        expect(res).toEqual({ queued: true });
        expect(storedEntries()).toHaveLength(2);
        await settleOfflinePass();
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
        await settleOfflinePass();
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

describe('a lineup save takes the place of a queued save of the same lineup', () => {
    // A lineup PUT restates the whole lineup and carries no stamp the server
    // could order it by, so a later save of the same lineup replaces the
    // queued one (the flush sends it at once) rather than go out beside it
    // and be overwritten by the older one's replay.
    it('two saves of one lineup while offline: one entry, the later one, sent once', async () => {
        const s = server();
        await API.putMatchLineup('c1', 'team-A', 'm1', { 1: 'Aoki' }, 'pw');
        vi.advanceTimersByTime(1000);
        const res = await API.putMatchLineup('c1', 'team-A', 'm1', { 1: 'Baba' }, 'pw');
        expect(res).toEqual({ queued: true });
        // A different lineup is an entry of its own.
        await API.putMatchLineup('c1', 'team-B', 'm1', { 1: 'Chiba' }, 'pw');
        const stored = storedEntries().map(([, d]) => d.payload);
        expect(stored).toHaveLength(2);
        expect(stored.find((p) => p.teamId === 'team-A').positions).toEqual({ 1: 'Baba' });
        await settleOfflinePass();
        s.online();
        await tick(50);
        const teamA = s.sent.filter((w) => w.body.teamId === 'team-A');
        expect(teamA.map((w) => w.body.positions)).toEqual([{ 1: 'Baba' }]);
        expect(localStorage.getItem('bc_write_queue')).toBeNull();
    });
});

describe('nothing is dropped for its age', () => {
    it('an entry several days old is still replayed', async () => {
        const fiveDays = 5 * 24 * 60 * 60 * 1000;
        const old = Date.now() - fiveDays;
        localStorage.setItem('bc_write_queue', JSON.stringify([
            ['c1:m1\u001fold.1.x', {
                compID: 'c1', matchID: 'm1', kind: 'score', terminal: true, method: 'PUT',
                url: '/api/competitions/c1/matches/m1/score', password: 'pw', enqueuedAt: old,
                payload: { status: 'completed', winner: 'A', ipponsA: ['M', 'K'], ipponsB: [], modifiedAt: old },
            }],
        ]));
        vi.resetModules();
        mod = await import('../api_client.jsx');
        API = mod.API;
        const alerts = [];
        const unsub = mod.subscribeQueueAlert((a) => alerts.push(a));
        expect(API.hasPendingTerminalWrite('c1', 'm1')).toBe(true);
        const s = server();
        s.online();
        await tick(50);
        unsub();
        expect(s.sent).toHaveLength(1);
        expect(s.sent[0].body).toMatchObject({ status: 'completed', winner: 'A', modifiedAt: old });
        expect(localStorage.getItem('bc_write_queue')).toBeNull();
        // Nothing was announced as discarded, expired or unreadable.
        expect(alerts.filter((a) => a.kind !== 'sent')).toEqual([]);
    });
});

describe('the queued answer says whether the write reached browser storage', () => {
    it('stored: { queued: true }', async () => {
        server();
        const res = await API.recordScore('c1', 'm1', { status: 'completed', winner: 'A' }, 'pw', null);
        expect(res).toEqual({ queued: true });
    });

    it('storage full: { queued: true, persisted: false } for a score, a decision and a lineup', async () => {
        server();
        global.localStorage.setItem = () => { throw new Error('QuotaExceededError'); };
        expect(await API.recordScore('c1', 'm1', { status: 'completed', winner: 'A' }, 'pw', null))
            .toEqual({ queued: true, persisted: false });
        expect(await API.recordDecision('c1', 'm2', { decision: 'fusenpai', decisionBy: 'aka' }, 'pw'))
            .toEqual({ queued: true, persisted: false });
        expect(await API.putMatchLineup('c1', 'team-A', 'm3', { 1: 'Aoki' }, 'pw'))
            .toEqual({ queued: true, persisted: false });
    });
});

// A deferred fetch: each score request waits until the test answers it.
function heldServer() {
    const calls = [];
    let netDown = false;
    global.fetch = vi.fn((url, opts) => {
        if (isClockPoll(url)) return Promise.reject(new TypeError('clock poll: not part of this test'));
        const call = { url: String(url), body: JSON.parse(opts.body), password: opts.headers['X-Tournament-Password'] };
        calls.push(call);
        if (netDown) return Promise.reject(new TypeError('offline'));
        if (call.hold) return call.promise;
        return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({}) });
    });
    return {
        calls,
        setNetDown: (v) => { netDown = v; },
    };
}
// Install a fetch whose NEXT score request is held until answered.
function holdNextRequest(srv) {
    const prev = global.fetch;
    let answer;
    const promise = new Promise((r) => { answer = r; });
    global.fetch = vi.fn((url, opts) => {
        if (isClockPoll(url)) return Promise.reject(new TypeError('clock poll: not part of this test'));
        global.fetch = prev;
        srv.calls.push({ url: String(url), body: JSON.parse(opts.body), password: opts.headers['X-Tournament-Password'] });
        return promise;
    });
    return () => answer({ ok: true, status: 200, json: () => Promise.resolve({}) });
}

describe('the page-hide copy of a write still being sent', () => {
    it('a tap made and hidden while the earlier write is in flight is sent without any other trigger', async () => {
        const srv = heldServer();
        const answerM = holdNextRequest(srv);
        const pM = API.recordScore('c1', 'm1', { status: 'running', ipponsA: ['M'] }, 'pw', null);
        await flushMicrotasks();
        // The page is hidden with M's fetch open: M is kept, and the editor's
        // own page-hide flush writes the newer tap K durably.
        window.dispatchEvent(new Event('pagehide'));
        await API.recordScore('c1', 'm1', { status: 'running', ipponsA: ['M', 'K'], durable: true }, 'pw', null);
        await flushMicrotasks();
        answerM();
        await pM;
        await tick(0);
        const scores = srv.calls.map((c) => c.body.ipponsA.join(''));
        expect(scores).toContain('MK');
        expect(localStorage.getItem('bc_write_queue')).toBeNull();
    });

    it('when the held copy settles, a write that failed meanwhile is sent at once, not on its backoff timer', async () => {
        const srv = heldServer();
        const answerM = holdNextRequest(srv);
        const pM = API.recordScore('c1', 'm1', { status: 'running', ipponsA: ['M'] }, 'pw', null);
        await flushMicrotasks();
        window.dispatchEvent(new Event('pagehide'));
        // K is written durably and its send fails (the connection dropped):
        // queued, with a backoff timer armed.
        srv.setNetDown(true);
        await API.recordScore('c1', 'm1', { status: 'running', ipponsA: ['M', 'K'], durable: true }, 'pw', null);
        await flushMicrotasks();
        const kSends = () => srv.calls.filter((c) => c.body.ipponsA.join('') === 'MK').length;
        expect(kSends()).toBe(1);
        // The connection is back and M's fetch answers.
        srv.setNetDown(false);
        answerM();
        await pM;
        await tick(0); // no timer advanced past the backoff
        expect(kSends()).toBe(2);
        expect(localStorage.getItem('bc_write_queue')).toBeNull();
    });

    it('the held copy goes when its own fetch answers, even after a later write took its place in flight', async () => {
        const srv = heldServer();
        const answerM = holdNextRequest(srv);
        const pM = API.recordScore('c1', 'm1', { status: 'running', ipponsA: ['M'] }, 'pw', null);
        await flushMicrotasks();
        window.dispatchEvent(new Event('pagehide'));
        // The tab comes back and the operator taps K: sent straight away
        // while M's fetch is still open.
        const answerK = holdNextRequest(srv);
        const pK = API.recordScore('c1', 'm1', { status: 'running', ipponsA: ['M', 'K'] }, 'pw', null);
        await flushMicrotasks();
        answerM();
        await pM;
        answerK();
        await pK;
        await tick(0);
        // M's copy did not stay behind in the outbox.
        expect(localStorage.getItem('bc_write_queue')).toBeNull();
        expect(API.unsentWrites().total).toBe(0);
    });
});

describe('signing in again re-stamps every write carrying the refused password', () => {
    it('a write queued after the parked one, with the same old password, is sent with the new one', async () => {
        // Parking a write, and a write crossing the notice threshold, each
        // console.warn for devtools: expected here.
        const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
        const sent = [];
        let up = false;
        global.fetch = vi.fn((url, opts) => {
            if (isClockPoll(url)) return Promise.reject(new TypeError('clock poll: not part of this test'));
            if (!up) return Promise.reject(new TypeError('offline'));
            const password = opts.headers['X-Tournament-Password'];
            sent.push({ url: String(url), password });
            const code = password === 'new' ? 200 : 401;
            return Promise.resolve({ ok: code === 200, status: code, json: () => Promise.resolve({}) });
        });
        // A queued Finish meets the rotated password and is parked.
        await API.recordScore('c1', 'm1', { status: 'completed', winner: 'A' }, 'old', null);
        await settleOfflinePass();
        up = true;
        window.dispatchEvent(new Event('online'));
        await tick(50);
        expect(API.unsentWrites().authBlocked).toBe(1);
        // Offline again: a second result is queued with the same old password
        // and has never been answered.
        up = false;
        await API.recordScore('c1', 'm2', { status: 'completed', winner: 'B' }, 'old', null);
        await settleOfflinePass();
        // Back online, and the operator signs in with the new password.
        up = true;
        const before = sent.length;
        API.resumeAfterAuth('new');
        await tick(50);
        expect(sent.slice(before).map((r) => r.password)).toEqual(['new', 'new']);
        expect(API.unsentWrites()).toEqual({ total: 0, terminal: 0, authBlocked: 0, failing: 0 });
        warnSpy.mockRestore();
    });

    it('never gives the password to a write queued without one (a self-run participant)', async () => {
        // Parking a write, and a write crossing the notice threshold, each
        // console.warn for devtools: expected here.
        const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
        const sent = [];
        let up = false;
        global.fetch = vi.fn((url, opts) => {
            if (isClockPoll(url)) return Promise.reject(new TypeError('clock poll: not part of this test'));
            if (!up) return Promise.reject(new TypeError('offline'));
            const password = opts.headers['X-Tournament-Password'];
            sent.push({ url: String(url), password });
            const code = password === 'old' ? 401 : 200;
            return Promise.resolve({ ok: code === 200, status: code, json: () => Promise.resolve({}) });
        });
        await API.recordScore('c1', 'm1', { status: 'completed', winner: 'A' }, 'old', null);
        await settleOfflinePass();
        up = true;
        window.dispatchEvent(new Event('online'));
        await tick(50);
        up = false;
        await API.recordScore('c1', 'm2', { status: 'completed', winner: 'B' }, '', null);
        await settleOfflinePass();
        up = true;
        const before = sent.length;
        API.resumeAfterAuth('new');
        await tick(50);
        const after = sent.slice(before);
        expect(after.find((r) => r.url.includes('/m1/')).password).toBe('new');
        expect(after.find((r) => r.url.includes('/m2/')).password).toBe('');
        warnSpy.mockRestore();
    });
});

describe('the way past a held write the server keeps refusing', () => {
    it('only that match\'s failing write is discarded; the status leaves "server-error"', async () => {
        // Parking a write, and a write crossing the notice threshold, each
        // console.warn for devtools: expected here.
        const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
        const s = server({ status: (url) => (url.includes('/m1/') ? 500 : 200) });
        s.online();
        await flushMicrotasks();
        await API.recordScore('c1', 'm1', { status: 'completed', winner: 'A' }, 'pw', null);
        await tick(60_000); // well past the server-error notice threshold
        // A second match's result made while m1 keeps failing lands.
        await API.recordScore('c1', 'm2', { status: 'completed', winner: 'B' }, 'pw', null);
        const statuses = [];
        const unsub = mod.subscribeSyncStatus((st) => statuses.push(st));
        expect(statuses.at(-1)).toBe('server-error');
        expect(API.heldWriteKeepsFailing('c1', 'm1')).toBe(true);
        expect(API.heldWriteKeepsFailing('c1', 'm2')).toBe(false);
        expect(API.discardFailingHeldWrites('c1', 'm2')).toBe(0);
        expect(API.discardFailingHeldWrites('c1', 'm1')).toBe(1);
        unsub();
        expect(statuses.at(-1)).not.toBe('server-error');
        expect(API.unsentWrites().total).toBe(0);
        expect(localStorage.getItem('bc_write_queue')).toBeNull();
        warnSpy.mockRestore();
    });
});

// The topbar's held-writes list (HeldWritesPanel) reaches the held writes no
// editor offers to discard: a running autosave, a lineup save, a hand-set
// winner. Each is discarded on its own, and only once the server keeps
// refusing it; a write only waiting for the connection is never discarded.
describe('any held write the server keeps refusing can be discarded on its own', () => {
    it('lists every kind, refuses to discard one only waiting, discards each failing one', async () => {
        const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
        const s = server({ status: (url) => (url.includes('/m2/') ? 200 : 500) });
        await API.recordScore('c1', 'm1', { status: 'running', ipponsA: ['M'] }, 'pw', null);
        await API.putTeamLineup('c1', 't1', '1', { 1: 'Ito' }, 'pw');
        await API.overrideBracketWinner('c1', 'r1-m1', 'Team A', 'pw');
        await API.recordScore('c1', 'm2', { status: 'completed', winner: 'B' }, 'pw', null);
        await settleOfflinePass();
        const waiting = API.heldWrites();
        expect(waiting.map((h) => h.kind).sort()).toEqual(['lineup', 'override', 'score', 'score']);
        expect(waiting.every((h) => !h.keepsFailing)).toBe(true);
        // Only waiting for the connection: not discarded through this door.
        expect(API.discardHeldWrite(waiting[0].key)).toBe(false);
        expect(API.heldWrites()).toHaveLength(4);
        s.online();
        await tick(60_000); // past the server-error notice threshold
        const failing = API.heldWrites();
        // m2 landed; the other three keep failing.
        expect(failing.map((h) => [h.kind, h.terminal, h.keepsFailing]).sort()).toEqual([
            ['lineup', true, true], ['override', true, true], ['score', false, true],
        ]);
        const lineup = failing.find((h) => h.kind === 'lineup');
        expect(lineup).toMatchObject({ compID: 'c1', teamId: 't1', round: '1' });
        expect(failing.find((h) => h.kind === 'override')).toMatchObject({ compID: 'c1', matchID: 'r1-m1' });
        const statuses = [];
        const unsub = mod.subscribeSyncStatus((st) => statuses.push(st));
        expect(statuses.at(-1)).toBe('server-error');
        expect(API.discardHeldWrite(lineup.key)).toBe(true);
        expect(API.discardHeldWrite(lineup.key)).toBe(false);
        expect(API.heldWrites().map((h) => h.kind).sort()).toEqual(['override', 'score']);
        expect(storedEntries()).toHaveLength(2);
        for (const h of API.heldWrites()) expect(API.discardHeldWrite(h.key)).toBe(true);
        unsub();
        expect(statuses.at(-1)).not.toBe('server-error');
        expect(API.unsentWrites().total).toBe(0);
        warnSpy.mockRestore();
    });
});

// Once one held write keeps failing, the sync status is already
// "server-error", so a SECOND write crossing the threshold changes no status.
// The held counts carry `failing`, so it is still published, at the crossing:
// that match's editor offers its discard, and the topbar's list re-reads.
describe('a second held write crossing the server-error threshold is published', () => {
    it('the held counts move to two failing at the second crossing', async () => {
        const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
        const s = server({ status: () => 500 });
        s.online();
        await flushMicrotasks();
        await API.recordScore('c1', 'm1', { status: 'completed', winner: 'A' }, 'pw', null);
        await tick(60_000);
        expect(API.heldWriteKeepsFailing('c1', 'm1')).toBe(true);
        const published = [];
        const unsub = mod.subscribeUnsentWrites((c) => published.push(c));
        await API.recordScore('c1', 'm2', { status: 'completed', winner: 'B' }, 'pw', null);
        await tick(60_000);
        unsub();
        expect(API.heldWriteKeepsFailing('c1', 'm2')).toBe(true);
        expect(published.at(-1)).toMatchObject({ total: 2, failing: 2 });
        warnSpy.mockRestore();
    });
});

describe('a queued change held because the finished match needs a winner', () => {
    // Operator ruling 2026-10-04: the server keeps the match's recorded finish
    // and the change in its history, and says why (heldReason). The replay
    // tells the editor and the operator to correct the result with a winner.
    it('the not-applied banner and the alert both say to correct it with a winner', async () => {
        const s = server({ answer: () => ({ applied: false, reason: 'superseded', heldGroups: ['points'], heldReason: 'needs_winner' }) });
        await API.recordScore('c1', 'm1', { status: 'completed', winner: 'A', ipponsA: ['M'] }, 'pw', null);
        await settleOfflinePass();
        const failed = [];
        const alerts = [];
        const unsubFail = mod.subscribeTerminalWriteFailed((i) => failed.push(i));
        const unsubAlert = mod.subscribeQueueAlert((a) => alerts.push(a));
        s.online();
        await tick(50);
        unsubFail();
        unsubAlert();
        expect(failed).toHaveLength(1);
        expect(failed[0].reason).toMatch(/without a winner/);
        expect(failed[0].advice).toBe('Correct the result with a winner.');
        expect(alerts.filter((a) => a.kind === 'superseded')).toEqual([
            expect.objectContaining({ count: 1, needsWinner: true, matchID: 'm1' }),
        ]);
    });

    it('a plain superseded replay keeps the plain copy', async () => {
        const s = server({ answer: () => ({ applied: false, reason: 'superseded', heldGroups: ['points'] }) });
        await API.recordScore('c1', 'm1', { status: 'completed', winner: 'A', ipponsA: ['M'] }, 'pw', null);
        await settleOfflinePass();
        const failed = [];
        const unsubFail = mod.subscribeTerminalWriteFailed((i) => failed.push(i));
        s.online();
        await tick(50);
        unsubFail();
        expect(failed).toHaveLength(1);
        expect(failed[0].reason).toMatch(/newer change/);
    });
});

describe('a queued finish that moved a later change to the history', () => {
    // The server records the finish and moves a newer scoring change that
    // would have left the knockout without a winner into the history. The
    // answer is applied: the finish counts as sent, the operator is told
    // once that a later change was moved, and no "not applied" is raised.
    it('sent, plus one displaced alert; no not-applied banner', async () => {
        const s = server({ answer: () => ({ id: 'm1', status: 'completed', displacedGroups: ['points'], heldReason: 'needs_winner' }) });
        await API.recordScore('c1', 'm1', { status: 'completed', winner: 'A', ipponsA: ['M'] }, 'pw', null);
        await settleOfflinePass();
        const failed = [];
        const alerts = [];
        const unsubFail = mod.subscribeTerminalWriteFailed((i) => failed.push(i));
        const unsubAlert = mod.subscribeQueueAlert((a) => alerts.push(a));
        s.online();
        await tick(50);
        unsubFail();
        unsubAlert();
        expect(failed).toEqual([]);
        expect(alerts.map((a) => a.kind)).toEqual(['sent', 'displaced']);
        expect(alerts.find((a) => a.kind === 'displaced')).toEqual(expect.objectContaining({ count: 1, forWinner: 1 }));
        expect(localStorage.getItem('bc_write_queue')).toBeNull();
    });

    // A change can also be moved for another reason (a representative's pick
    // stamped after the representative bout's removal): the answer carries no
    // heldReason, and the alert counts it as moved, not as moved for a winner.
    it('a change moved with no reason is told as moved, not as moved for a winner', async () => {
        const s = server({ answer: () => ({ id: 'm1', status: 'completed', displacedGroups: ['repPickB'] }) });
        await API.recordScore('c1', 'm1', { status: 'completed', winner: 'A', ipponsA: ['M'] }, 'pw', null);
        await settleOfflinePass();
        const failed = [];
        const alerts = [];
        const unsubFail = mod.subscribeTerminalWriteFailed((i) => failed.push(i));
        const unsubAlert = mod.subscribeQueueAlert((a) => alerts.push(a));
        s.online();
        await tick(50);
        unsubFail();
        unsubAlert();
        expect(failed).toEqual([]);
        expect(alerts.map((a) => a.kind)).toEqual(['sent', 'displaced']);
        expect(alerts.find((a) => a.kind === 'displaced')).toEqual(expect.objectContaining({ count: 1, forWinner: 0 }));
        expect(localStorage.getItem('bc_write_queue')).toBeNull();
    });
});
