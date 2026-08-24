// Tests for the server-clock offset lifecycle in api_client.jsx (bc-cse).
//
// Writes are stamped in SERVER-RELATIVE time (the mp-y3nk block in
// api_client.jsx): payload.modifiedAt = Date.now() + offset, where the offset is
// learned from GET /api/time. Two properties are pinned here:
//
//   A. The load-time learn RETRIES with backoff until it first succeeds, and
//      stops scheduling once it has. Without it, a device whose load-time GET
//      fails and which never opens an SSE connection keeps offset 0 forever, so
//      every write from a behind-the-clock tablet is refused as superseded.
//   B. A 200 {"applied": false} RELEARNS the offset (throttled), so the
//      superseded banner's advice ("check the recorded result before
//      re-entering") becomes actionable: the re-entry carries a corrected stamp.
//      There are THREE sites that can see that response - a direct recordScore,
//      the reconnect flush of a queued write, and overrideBracketWinner - and
//      each is pinned separately, because deleting any one of them leaves the
//      other two covering for it.
//   C. The relearn's in-flight guard. The 5s throttle is measured from
//      COMPLETION, so it cannot cover a learn that never completes; only the
//      guard can. Pinned with a never-resolving /api/time.
//
// The offset is never exported. It is observed through the one thing that
// matters - payload.modifiedAt on a captured request body.
//
// Timer strategy: advanceTimersByTimeAsync(N), never runAllTimersAsync (the
// retry chain re-schedules itself, as does the queue backoff).

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

// Skew large enough that it can never be confused with fake-timer drift.
const SKEW_MS = 100000;

let _origFetch;
let _origEventSource;
let _origLocalStorage;
let _lsStore;

beforeEach(() => {
    vi.useFakeTimers();
    vi.resetModules();

    // Minimal EventSource stub for api_client's module-level code. It never
    // connects, which is precisely the scenario change A is about: no SSE means
    // no relearn from the onopen path.
    _origEventSource = global.EventSource;
    global.EventSource = class FakeES {
        constructor() { this.close = () => {}; }
    };
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
});

afterEach(() => {
    vi.useRealTimers();
    vi.resetModules();
    if (_origFetch === undefined) {
        delete global.fetch;
    } else {
        global.fetch = _origFetch;
    }
    if (_origEventSource === undefined) {
        delete global.EventSource;
    } else {
        global.EventSource = _origEventSource;
    }
    if (_origLocalStorage === undefined) {
        delete global.localStorage;
    } else {
        global.localStorage = _origLocalStorage;
    }
});

// Helpers

/** Resolve pending microtasks without advancing fake timers. */
async function flushMicrotasks() {
    for (let i = 0; i < 12; i++) await Promise.resolve();
}

/** Advance fake timers by the given ms, then flush microtasks. */
async function tick(ms = 0) {
    await vi.advanceTimersByTimeAsync(ms);
    await flushMicrotasks();
}

/**
 * Fake server: counts /api/time requests, can be flipped between failing and
 * succeeding, and captures every write payload. nowMs is computed at RESPONSE
 * time so the learned offset is exactly SKEW_MS under frozen fake timers.
 *
 * Mutable knobs (all flipped mid-test to stage a scenario):
 *   timeOk    - does GET /api/time answer, or fail like a dead network?
 *   timeHangs - GET /api/time never settles, so a learn stays in flight forever.
 *   online    - when false, every request EXCEPT /api/time fails, which is how a
 *               write gets queued instead of sent.
 *   writeReply - body of every non-/api/time response (score, override, ...).
 */
function makeServer({ timeOk = false } = {}) {
    const server = {
        timeCalls: 0,
        timeOk,
        timeHangs: false,
        online: true,
        payloads: [],
        writeReply: () => ({}),
    };
    server.fetch = vi.fn((url, opts) => {
        const u = String(url);
        if (u.includes('/api/time')) {
            server.timeCalls++;
            if (server.timeHangs) return new Promise(() => {});
            if (!server.timeOk) return Promise.reject(new TypeError('network error'));
            return Promise.resolve({
                ok: true,
                json: () => Promise.resolve({ nowMs: Date.now() + SKEW_MS }),
            });
        }
        if (!server.online) return Promise.reject(new TypeError('network error'));
        if (opts && opts.body) server.payloads.push(JSON.parse(opts.body));
        return Promise.resolve({
            ok: true, status: 200,
            json: () => Promise.resolve(server.writeReply()),
        });
    });
    return server;
}

/** Install the fake server, then load api_client so its load-time learn uses it. */
async function loadWith(server) {
    global.fetch = server.fetch;
    const mod = await import('../api_client.jsx');
    await flushMicrotasks();
    return mod.API;
}

/** Send one score write and return its stamp relative to local now (= the offset). */
async function stampOffset(API, server, matchID) {
    const before = server.payloads.length;
    await API.recordScore('c1', matchID, { status: 'running' }, 'pw', null);
    await flushMicrotasks();
    const payload = server.payloads[before];
    expect(payload).toBeTruthy();
    return payload.modifiedAt - Date.now();
}

// A. Load-time retry chain

describe('server-clock offset: initial learn retries until it succeeds', () => {
    it('keeps retrying with backoff, then stops once learned', async () => {
        const server = makeServer(); // /api/time is failing
        const API = await loadWith(server);

        // One attempt at load; it failed, so writes carry local time (offset 0).
        expect(server.timeCalls).toBe(1);
        expect(Math.abs(await stampOffset(API, server, 'm1'))).toBeLessThan(1000);

        // Backoff #1 is 5s: nothing at 4s, an attempt at 5s.
        await tick(4000);
        expect(server.timeCalls).toBe(1);
        await tick(1000);
        expect(server.timeCalls).toBe(2);

        // Still no offset: that attempt failed too. Backoff #2 is 10s.
        expect(Math.abs(await stampOffset(API, server, 'm2'))).toBeLessThan(1000);
        server.timeOk = true;
        await tick(9999);
        expect(server.timeCalls).toBe(2);
        await tick(1);
        expect(server.timeCalls).toBe(3);

        // Learned: every subsequent write is stamped in the server's frame.
        expect(await stampOffset(API, server, 'm3')).toBe(SKEW_MS);

        // And the chain STOPS - five more minutes produce no further requests.
        await tick(300000);
        expect(server.timeCalls).toBe(3);
    });
});

// B. Relearn on a superseded response

describe('server-clock offset: a superseded write relearns the offset', () => {
    it('makes the "re-enter the result" advice true', async () => {
        const server = makeServer(); // load-time learn fails: offset stays 0
        const API = await loadWith(server);
        expect(server.timeCalls).toBe(1);

        // The server now answers, but the operator's write is dropped by the
        // timestamp guard because this device's stamp is behind.
        server.timeOk = true;
        server.writeReply = () => ({ applied: false });
        await API.recordScore('c1', 'm1', { status: 'running' }, 'pw', null);
        await flushMicrotasks();
        expect(server.timeCalls).toBe(2);

        // The operator re-enters the result: it now carries a corrected stamp.
        server.writeReply = () => ({});
        expect(await stampOffset(API, server, 'm1')).toBe(SKEW_MS);
    });

    it('does NOT relearn on a normal applied write', async () => {
        const server = makeServer({ timeOk: true });
        const API = await loadWith(server);
        expect(server.timeCalls).toBe(1);

        await stampOffset(API, server, 'm1');
        expect(server.timeCalls).toBe(1);

        // Past the relearn throttle window too, so the counter-case cannot be
        // passing merely because a wrongly-fired relearn was throttled away.
        await tick(6000);
        await stampOffset(API, server, 'm2');
        expect(server.timeCalls).toBe(1);
    });

    it('throttles a burst of superseded drops down to one request', async () => {
        // A reconnect flush can surface several drops back-to-back; they must not
        // fire one GET /api/time each.
        const server = makeServer({ timeOk: true });
        const API = await loadWith(server);
        expect(server.timeCalls).toBe(1);

        server.writeReply = () => ({ applied: false });
        await API.recordScore('c1', 'm1', { status: 'running' }, 'pw', null);
        await API.recordScore('c1', 'm2', { status: 'running' }, 'pw', null);
        await flushMicrotasks();

        expect(server.timeCalls).toBe(2); // 1 at load + exactly 1 relearn
    });
});

// B (continued). The other two relearn sites. Each is exercised on its own path
// because the three are independent call sites, not one shared chokepoint: with
// only the recordScore site pinned, deleting either of these left the whole
// suite green.

describe('server-clock offset: a superseded QUEUED write relearns on the flush path', () => {
    it('relearns when a reconnect flush is told the queued result was superseded', async () => {
        // This is the path the feature targets. The tablet was offline while the
        // same match was re-scored elsewhere, so the result the server drops is
        // the one sitting in this device's queue - and the drop is discovered by
        // the flush, not by the operator's own submit.
        const server = makeServer({ timeOk: true });
        const API = await loadWith(server);
        expect(server.timeCalls).toBe(1); // learned at load, so no retry chain is pending

        // Offline: the finished result is queued rather than sent.
        server.online = false;
        const queued = await API.recordScore('c1', 'mq', { status: 'completed' }, 'pw', null);
        expect(queued).toMatchObject({ queued: true });
        await flushMicrotasks();
        expect(server.timeCalls).toBe(1); // a network failure is not a supersede signal

        // Reconnect: the flush delivers it and the server drops it as stale.
        server.online = true;
        server.writeReply = () => ({ applied: false, reason: 'superseded' });
        window.dispatchEvent(new Event('online'));
        await tick(50);

        expect(server.timeCalls).toBe(2);
    });
});

describe('server-clock offset: a superseded override assertion relearns', () => {
    it('relearns when overrideBracketWinner is answered applied:false', async () => {
        const server = makeServer({ timeOk: true });
        const API = await loadWith(server);
        expect(server.timeCalls).toBe(1);

        server.writeReply = () => ({ applied: false });
        const r = await API.overrideBracketWinner('c1', 'm-r2-0', 'Alice', 'pw');
        expect(r).toEqual({ applied: false });
        await flushMicrotasks();

        expect(server.timeCalls).toBe(2);
    });

    it('does NOT relearn when the server applies the assertion', async () => {
        const server = makeServer({ timeOk: true });
        const API = await loadWith(server);
        expect(server.timeCalls).toBe(1);

        server.writeReply = () => ({ applied: true });
        const r = await API.overrideBracketWinner('c1', 'm-r2-0', 'Alice', 'pw');
        expect(r).toEqual({ applied: true });

        // Ride past the throttle window, so a wrongly-fired relearn cannot be
        // hidden by the throttle instead of by the applied:true check.
        await tick(6000);
        expect(server.timeCalls).toBe(1);
    });
});

// C. The in-flight guard.

describe('server-clock offset: the relearn in-flight guard', () => {
    it('starts no second learn while one is still outstanding, even past the throttle window', async () => {
        // The throttle stamps its window on COMPLETION, so a learn that never
        // completes never opens one: the in-flight guard is the only thing
        // between a stalled /api/time and one more GET per superseded response.
        // A tablet on the wifi that caused the skew is exactly where a request
        // hangs for its full 12s abort, so this is the realistic case.
        const server = makeServer({ timeOk: true });
        const API = await loadWith(server);
        expect(server.timeCalls).toBe(1); // learned at load: no retry chain pending

        server.timeHangs = true; // every later /api/time never settles
        server.writeReply = () => ({ applied: false });

        await API.recordScore('c1', 'm1', { status: 'completed' }, 'pw', null);
        await flushMicrotasks();
        expect(server.timeCalls).toBe(2); // relearn started, now stuck in flight

        // Well beyond the 5s throttle window that was never opened.
        await tick(30000);

        await API.recordScore('c1', 'm2', { status: 'completed' }, 'pw', null);
        await flushMicrotasks();

        expect(server.timeCalls).toBe(2);
    });
});
