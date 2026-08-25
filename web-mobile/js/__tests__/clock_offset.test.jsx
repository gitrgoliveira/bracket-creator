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
// The clock-skew hardening adds five more, against the pinned wire contract
// (200 {"applied":false,"reason":"clock_skew","serverNowMs":…} for a write
// stamped more than 5s in the server's future; {"type":"heartbeat","nowMs":…}
// on the 15s SSE beat):
//
//   D. The heartbeat TRIPWIRE: a divergence beyond 3s asks for a real
//      GET /api/time, and the frame's nowMs is NEVER assigned into the offset.
//      Both halves are pinned, the second with a hanging /api/time so nothing
//      legitimate can move the offset either.
//   E. The browser 'online' event relearns, queue empty or not.
//   F. A live write refused for clock_skew: a COMPLETED one heals and resends
//      once (and is reported, never queued, if refused again); a RUNNING
//      autosave heals silently and does not resend.
//   G. A queued write refused on the flush path: the stamp is RECONSTRUCTED
//      from enqueuedAt + the freshly learned offset (so the write keeps its
//      real age) and retried once; a second refusal drops it with both
//      channels told.
//   I. The same recovery on overrideBracketWinner, plus the bracket-resync a
//      permanently-dropped assertion owes any optimistic local advance.
//   H. The counter-case. A plain supersede takes NONE of those paths: no
//      re-stamp, no resend, and the pre-existing supersede notification.
//      Pinned because every recovery above hangs off one `reason` comparison,
//      and getting that comparison wrong would turn "a newer result won" into
//      "resend and overwrite it" - the exact move the supersede advice exists
//      to prevent.
//
// The offset is never exported. It is observed through the one thing that
// matters - payload.modifiedAt on a captured request body.
//
// Timer strategy: advanceTimersByTimeAsync(N), never runAllTimersAsync (the
// retry chain re-schedules itself, as does the queue backoff).

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

// Skew large enough that it can never be confused with fake-timer drift.
const SKEW_MS = 100000;

// Hard cap on the writes the fake server will accept for one test. See the
// throw in makeServer: this is a hang-into-red-assertion tripwire, not a
// scenario limit.
const MAX_WRITE_ATTEMPTS = 4;

let _origFetch;
let _origEventSource;
let _origLocalStorage;
let _lsStore;
// Every EventSource api_client opens during a test, newest last. The heartbeat
// tripwire tests drive one by hand (es.onmessage({data})); everything else just
// needs the constructor to exist.
let _esInstances;

beforeEach(() => {
    vi.useFakeTimers();
    vi.resetModules();

    // Minimal EventSource stub for api_client's module-level code. It never
    // connects on its own, which is precisely the scenario change A is about: no
    // SSE means no relearn from the onopen path. Instances are recorded so a
    // test can deliver a frame to one.
    _esInstances = [];
    _origEventSource = global.EventSource;
    global.EventSource = class FakeES {
        constructor(url) {
            this.url = url;
            this.readyState = 0;
            this.close = () => {};
            _esInstances.push(this);
        }
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
        // How far ahead of THIS device the server's clock runs. Mutable so a
        // test can move the server's frame mid-run and prove the client adopted
        // the new offset from a real GET rather than from anything else.
        skew: SKEW_MS,
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
                json: () => Promise.resolve({ nowMs: Date.now() + server.skew }),
            });
        }
        if (!server.online) return Promise.reject(new TypeError('network error'));
        if (opts && opts.body) {
            server.payloads.push(JSON.parse(opts.body));
            // Bound the mock. Every recovery path here is "heal and retry ONCE",
            // so no scenario in this file legitimately sends a fifth write for
            // one action. Without this cap a mutation that removes a
            // retried-once guard turns the resend into an unbounded loop, and
            // the suite HANGS instead of failing: a hang reports nothing, names
            // nothing, and is indistinguishable from a slow machine. Throwing
            // makes the same mutation fail red, on an assertion that names it.
            if (server.payloads.length > MAX_WRITE_ATTEMPTS) {
                throw new Error(`unbounded resend loop: more than ${MAX_WRITE_ATTEMPTS} write attempts`);
            }
        }
        return Promise.resolve({
            ok: true, status: 200,
            json: () => Promise.resolve(server.writeReply()),
        });
    });
    return server;
}

/** Install the fake server, then load api_client so its load-time learn uses it. */
async function loadModWith(server) {
    global.fetch = server.fetch;
    const mod = await import('../api_client.jsx');
    await flushMicrotasks();
    return mod;
}

/** loadModWith, for the majority of tests that only need the API object. */
async function loadWith(server) {
    return (await loadModWith(server)).API;
}

/**
 * Open the shared SSE source and return the EventSource api_client created, so
 * a test can deliver a frame to its onmessage. Returns {es, unsubscribe}.
 */
function connectSSE(API) {
    const unsubscribe = API.subscribeToEvents(() => {});
    const es = _esInstances[_esInstances.length - 1];
    expect(es).toBeTruthy();
    return { es, unsubscribe };
}

/** Deliver one SSE frame to the given source, exactly as the browser would. */
function deliver(es, frame) {
    es.onmessage({ data: JSON.stringify(frame) });
}

/** A server reply queue: each write gets the next body, the last one repeats. */
function replies(...bodies) {
    let i = 0;
    return () => bodies[Math.min(i++, bodies.length - 1)];
}

/** The clock-skew refusal body (the pinned wire contract). */
function skewRefusal() {
    return { applied: false, reason: 'clock_skew', serverNowMs: Date.now() + SKEW_MS, message: 'stamp is in the future' };
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

// ---------------------------------------------------------------------------
// D. Heartbeat clock tripwire (bc-cse clock-skew hardening)
// ---------------------------------------------------------------------------
// The 15s SSE heartbeat carries the server's nowMs. It is a DETECTOR: a
// divergence beyond 3s asks for a real GET /api/time, and nothing else. It is
// never a SOURCE, because a one-way push has no round-trip to correct for and a
// frame delivered late reads as a stale nowMs - assigning it would push this
// device's frame backwards, manufacturing the skew the whole mechanism exists
// to prevent.

describe('heartbeat clock tripwire', () => {
    it('relearns when the heartbeat diverges beyond the tripwire', async () => {
        const server = makeServer({ timeOk: true });
        const API = await loadWith(server);
        expect(server.timeCalls).toBe(1);
        expect(await stampOffset(API, server, 'm0')).toBe(SKEW_MS);

        // The server's frame moves (or this tablet's clock jumps: the client
        // cannot tell, which is the point). The next heartbeat exposes it.
        server.skew = SKEW_MS + 20000;
        const { es, unsubscribe } = connectSSE(API);
        deliver(es, { type: 'heartbeat', nowMs: Date.now() + SKEW_MS + 10000 });
        await flushMicrotasks();

        expect(server.timeCalls).toBe(2);
        // Healed from the GET, whose RTT-halving math is the only thing entitled
        // to move the offset - so the new value is the SERVER's frame, not the
        // 10s divergence the heartbeat reported.
        expect(await stampOffset(API, server, 'm1')).toBe(SKEW_MS + 20000);
        unsubscribe();
    });

    it('does NOT relearn when the heartbeat agrees with our frame', async () => {
        const server = makeServer({ timeOk: true });
        const API = await loadWith(server);
        expect(server.timeCalls).toBe(1);

        const { es, unsubscribe } = connectSSE(API);
        // Just inside the tripwire, on both sides.
        deliver(es, { type: 'heartbeat', nowMs: Date.now() + SKEW_MS + 2500 });
        deliver(es, { type: 'heartbeat', nowMs: Date.now() + SKEW_MS - 2500 });
        await flushMicrotasks();
        // A heartbeat with no clock at all (an older server) is inert too.
        deliver(es, { type: 'heartbeat' });
        await flushMicrotasks();

        // Past the throttle window, so a wrongly-fired relearn cannot be hidden
        // by the throttle instead of by the threshold.
        await tick(6000);
        expect(server.timeCalls).toBe(1);
        unsubscribe();
    });

    it('never assigns the heartbeat nowMs into the offset', async () => {
        const server = makeServer({ timeOk: true });
        const API = await loadWith(server);
        expect(server.timeCalls).toBe(1);

        // A frame delivered LATE on a congested court reads as a stale nowMs -
        // here a full minute behind our frame. Trip, but keep the GET
        // outstanding so nothing legitimate can move the offset either.
        server.timeHangs = true;
        const { es, unsubscribe } = connectSSE(API);
        deliver(es, { type: 'heartbeat', nowMs: Date.now() + SKEW_MS - 60000 });
        await flushMicrotasks();
        expect(server.timeCalls).toBe(2); // the tripwire fired

        // The offset is untouched. Had the frame been assigned, this would be
        // SKEW_MS - 60000: a device now stamping a minute in the past.
        expect(await stampOffset(API, server, 'm1')).toBe(SKEW_MS);
        unsubscribe();
    });
});

// ---------------------------------------------------------------------------
// E. Relearn on the browser 'online' event
// ---------------------------------------------------------------------------

describe('clock relearn on reconnect', () => {
    it('relearns on the online event even with nothing queued', async () => {
        // Reconnect is when an OS is most likely to have NTP-jumped the clock,
        // and the SSE onopen relearn only helps if SSE actually opens. The
        // writes at risk are the LIVE ones the operator makes next, so an empty
        // queue is exactly the case that must still relearn.
        const server = makeServer({ timeOk: true });
        const API = await loadWith(server);
        expect(server.timeCalls).toBe(1);

        server.skew = SKEW_MS + 20000;
        window.dispatchEvent(new Event('online'));
        await flushMicrotasks();

        expect(server.timeCalls).toBe(2);
        expect(await stampOffset(API, server, 'm1')).toBe(SKEW_MS + 20000);

        // Throttled: a flapping connection cannot turn into a request storm.
        window.dispatchEvent(new Event('online'));
        await flushMicrotasks();
        expect(server.timeCalls).toBe(2);
    });
});

// ---------------------------------------------------------------------------
// F. The clock_skew recovery loop: live writes
// ---------------------------------------------------------------------------
// A clock_skew refusal is NOT a supersede: nothing newer is stored and there is
// nothing for the operator to check. The same write, re-stamped in the server's
// frame, is what should be recorded - so the client heals and resends once.

describe('clock_skew recovery: a live completed write', () => {
    it('relearns, re-stamps and resends exactly once, and lands', async () => {
        const server = makeServer({ timeOk: true });
        const mod = await loadModWith(server);
        const API = mod.API;
        expect(server.timeCalls).toBe(1);

        const failures = [];
        const unsub = mod.subscribeTerminalWriteFailed((info) => failures.push(info));

        server.skew = SKEW_MS + 20000; // what a fresh learn will report
        server.writeReply = replies(skewRefusal(), {});

        const res = await API.recordScore('c1', 'm1', { status: 'completed' }, 'pw', null);
        await flushMicrotasks();

        // Exactly two attempts: the refused one and the healed one.
        expect(server.payloads.length).toBe(2);
        expect(server.timeCalls).toBe(2);
        // The resend carries the CORRECTED stamp: the first stamp plus the
        // delta the learn discovered.
        expect(server.payloads[0].modifiedAt).toBe(Date.now() + SKEW_MS);
        expect(server.payloads[1].modifiedAt).toBe(server.payloads[0].modifiedAt + 20000);
        // It landed, so nothing is reported and nothing is queued.
        expect(res).toEqual({});
        expect(failures).toEqual([]);
        expect(_lsStore.bc_write_queue).toBeUndefined();
        unsub();
    });

    it('refused twice: reports it, never a third attempt, never queued', async () => {
        // The drop console.warns for devtools; the strict test setup fails on an
        // unexpected warn, so own the spy here and assert it fired.
        const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
        const server = makeServer({ timeOk: true });
        const mod = await loadModWith(server);
        const API = mod.API;

        const failures = [];
        const unsub = mod.subscribeTerminalWriteFailed((info) => failures.push(info));

        server.writeReply = () => skewRefusal(); // refuses forever

        await API.recordScore('c1', 'm1', { status: 'completed' }, 'pw', null);
        await tick(2000);

        expect(server.payloads.length).toBe(2);
        expect(failures.length).toBe(1);
        expect(failures[0]).toMatchObject({ compID: 'c1', matchID: 'm1', kind: 'score', status: 200 });
        // The advice must name the CLOCK: "re-enter the result" alone would
        // send the operator round the same refusal forever.
        expect(failures[0].advice).toMatch(/clock/i);
        // Never queued: an entry stamped by a device that cannot get the stamp
        // right would poison-loop the flush.
        expect(_lsStore.bc_write_queue).toBeUndefined();
        expect(warnSpy).toHaveBeenCalledWith(expect.stringMatching(/clock skew/i));
        warnSpy.mockRestore();
        unsub();
    });

    it('a running autosave is healed silently: no banner, one attempt', async () => {
        const server = makeServer({ timeOk: true });
        const mod = await loadModWith(server);
        const API = mod.API;
        expect(server.timeCalls).toBe(1);

        const failures = [];
        const unsub = mod.subscribeTerminalWriteFailed((info) => failures.push(info));

        server.writeReply = () => skewRefusal();
        await API.recordScore('c1', 'm1', { status: 'running' }, 'pw', null);
        await flushMicrotasks();

        // No resend: the next keystroke's autosave (~300ms) carries the
        // corrected stamp, and a banner here would fire on every one of them.
        expect(server.payloads.length).toBe(1);
        expect(failures).toEqual([]);
        // But the heal still happens.
        expect(server.timeCalls).toBe(2);
        unsub();
    });
});

// ---------------------------------------------------------------------------
// G. The clock_skew recovery loop: the reconnect flush
// ---------------------------------------------------------------------------
// A queued write's stamp is RECONSTRUCTED, not refreshed: enqueuedAt is the
// local time of the operator's action, so enqueuedAt + the freshly learned
// offset is that action's time in the server's frame. The write keeps its real
// age and must still lose to a genuinely newer result.

describe('clock_skew recovery: a queued write on the flush path', () => {
    it('relearns, reconstructs the stamp from enqueuedAt, retries once and lands', async () => {
        const server = makeServer({ timeOk: true });
        const API = await loadWith(server);
        expect(server.timeCalls).toBe(1);

        // Offline: the finished result is queued, stamped in the frame this
        // device believed at the time.
        server.online = false;
        const enqueuedAt = Date.now();
        expect(await API.recordScore('c1', 'mq', { status: 'completed' }, 'pw', null)).toMatchObject({ queued: true });
        await flushMicrotasks();
        expect(server.payloads.length).toBe(0);

        // Time passes on this court before the wifi returns.
        await tick(30000);
        server.skew = SKEW_MS + 20000;
        server.online = true;
        server.writeReply = replies(skewRefusal(), {});
        window.dispatchEvent(new Event('online'));
        await tick(50);

        expect(server.payloads.length).toBe(2);
        expect(server.payloads[0].modifiedAt).toBe(enqueuedAt + SKEW_MS);
        // Reconstructed, NOT refreshed: 30s later, the retry still carries the
        // moment the operator entered the result.
        expect(server.payloads[1].modifiedAt).toBe(enqueuedAt + SKEW_MS + 20000);
        expect(server.payloads[1].modifiedAt).toBeLessThan(Date.now() + SKEW_MS + 20000);
        // Landed, so the queue is empty again.
        expect(_lsStore.bc_write_queue).toBeUndefined();
    });

    it('refused twice: dropped, reported on both channels, never a third attempt', async () => {
        // The drop console.warns for devtools; the strict test setup fails on an
        // unexpected warn, so own the spy here and assert it fired.
        const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
        const server = makeServer({ timeOk: true });
        const mod = await loadModWith(server);
        const API = mod.API;

        const failures = [];
        const alerts = [];
        const unsubFail = mod.subscribeTerminalWriteFailed((info) => failures.push(info));
        const unsubAlert = mod.subscribeQueueAlert((a) => alerts.push(a));

        server.online = false;
        await API.recordScore('c1', 'mq', { status: 'completed' }, 'pw', null);
        await flushMicrotasks();

        server.online = true;
        server.writeReply = () => skewRefusal(); // refuses forever
        window.dispatchEvent(new Event('online'));
        await tick(2000);

        expect(server.payloads.length).toBe(2);
        // The editor-scoped channel, for an operator still on this match...
        expect(failures.length).toBe(1);
        expect(failures[0]).toMatchObject({ compID: 'c1', matchID: 'mq', kind: 'score' });
        expect(failures[0].advice).toMatch(/clock/i);
        // ...and the alert channel, for one who has moved to another court.
        expect(alerts.some((a) => a.kind === 'rejected' && /clock/i.test(a.detail || ''))).toBe(true);
        // Dropped rather than left to poison-loop the flush.
        expect(_lsStore.bc_write_queue).toBeUndefined();
        expect(warnSpy).toHaveBeenCalledWith(expect.stringMatching(/clock skew/i));
        warnSpy.mockRestore();
        unsubFail();
        unsubAlert();
    });
});

// ---------------------------------------------------------------------------
// H. Counter-case: a plain supersede takes NONE of the new paths
// ---------------------------------------------------------------------------

describe('a superseded write is untouched by the clock_skew recovery', () => {
    it('does not re-stamp or resend, and still reports the supersede', async () => {
        const server = makeServer({ timeOk: true });
        const mod = await loadModWith(server);
        const API = mod.API;
        expect(server.timeCalls).toBe(1);

        const failures = [];
        const alerts = [];
        const unsubFail = mod.subscribeTerminalWriteFailed((info) => failures.push(info));
        const unsubAlert = mod.subscribeQueueAlert((a) => alerts.push(a));

        // A newer result really did win. Nothing about this device's clock is
        // being asserted, so re-stamping and resending would OVERWRITE the
        // newer result - the exact thing the superseded advice warns against.
        server.skew = SKEW_MS + 20000; // would be visible if anything re-stamped
        server.writeReply = () => ({ applied: false, reason: 'superseded' });

        const sentAt = Date.now();
        const res = await API.recordScore('c1', 'm1', { status: 'completed' }, 'pw', null);
        await tick(2000);

        expect(server.payloads.length).toBe(1);
        expect(server.payloads[0].modifiedAt).toBe(sentAt + SKEW_MS);
        expect(res).toMatchObject({ applied: false });
        // The pre-existing supersede notification, unchanged.
        expect(failures.length).toBe(1);
        expect(failures[0].reason).toBe(mod.SUPERSEDED_REASON);
        expect(failures[0].advice).toBe(mod.SUPERSEDED_ADVICE);
        expect(alerts.some((a) => a.kind === 'superseded')).toBe(true);
        // A supersede has relearned the offset since bc-cse, and still does.
        expect(server.timeCalls).toBe(2);
        unsubFail();
        unsubAlert();
    });
});

// ---------------------------------------------------------------------------
// I. The clock_skew recovery loop: overrideBracketWinner
// ---------------------------------------------------------------------------
// The third site that can see the refusal. Pinned separately for the same
// reason the supersede relearn is: these are independent call sites, not one
// shared chokepoint, so the other two cover for a deletion here.

describe('clock_skew recovery: an override assertion', () => {
    it('relearns, re-stamps and resends once, then reports applied', async () => {
        const server = makeServer({ timeOk: true });
        const API = await loadWith(server);
        expect(server.timeCalls).toBe(1);

        server.skew = SKEW_MS + 20000;
        server.writeReply = replies(skewRefusal(), { applied: true });

        const r = await API.overrideBracketWinner('c1', 'm-r2-0', 'Alice', 'pw');
        await flushMicrotasks();

        expect(r).toEqual({ applied: true });
        expect(server.payloads.length).toBe(2);
        expect(server.payloads[1].modifiedAt).toBe(server.payloads[0].modifiedAt + 20000);
        expect(server.timeCalls).toBe(2);
    });

    it('refused twice: reports applied:false to the caller, never a third attempt', async () => {
        // The second refusal console.warns for devtools; own the spy so the
        // strict test setup does not fail on it.
        const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
        const server = makeServer({ timeOk: true });
        const API = await loadWith(server);

        server.writeReply = () => skewRefusal();
        const r = await API.overrideBracketWinner('c1', 'm-r2-0', 'Alice', 'pw');
        await tick(2000);

        // Unchanged contract: the caller must not trust its optimistic pick -
        // now WITH the reason, so a caller can say why. The two verdicts this
        // boundary can return need opposite things said about them, and
        // `applied:false` alone cannot tell them apart.
        expect(r).toEqual({ applied: false, reason: 'clock_skew', message: 'stamp is in the future' });
        expect(server.payloads.length).toBe(2);
        expect(warnSpy).toHaveBeenCalledWith(expect.stringMatching(/clock skew/i));
        warnSpy.mockRestore();
    });
});

describe('clock_skew recovery: a queued override assertion', () => {
    it('asks for a bracket resync when it is dropped for good', async () => {
        // A dropped assertion leaves any optimistic local advance standing on a
        // write that will now never land. The supersede case is benign (a newer
        // result won, so the server is right and only a refetch is owed); this
        // one is a failure AND a wrong local bracket, so it owes both.
        const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
        const server = makeServer({ timeOk: true });
        const mod = await loadModWith(server);
        const API = mod.API;

        const resyncs = [];
        const failures = [];
        const unsubResync = mod.subscribeBracketResync((info) => resyncs.push(info));
        const unsubFail = mod.subscribeTerminalWriteFailed((info) => failures.push(info));

        server.online = false;
        expect(await API.overrideBracketWinner('c1', 'm-r2-0', 'Alice', 'pw')).toMatchObject({ queued: true });
        await flushMicrotasks();

        server.online = true;
        server.writeReply = () => skewRefusal(); // refuses forever
        window.dispatchEvent(new Event('online'));
        await tick(2000);

        expect(server.payloads.length).toBe(2);
        expect(resyncs.length).toBe(1);
        expect(resyncs[0]).toMatchObject({ compID: 'c1', matchID: 'm-r2-0', reason: 'clock_skew' });
        expect(failures.length).toBe(1);
        expect(_lsStore.bc_write_queue).toBeUndefined();
        expect(warnSpy).toHaveBeenCalledWith(expect.stringMatching(/clock skew/i));
        warnSpy.mockRestore();
        unsubResync();
        unsubFail();
    });
});

// ---------------------------------------------------------------------------
// J. The clock_skew recovery loop: a queued RUNNING write
// ---------------------------------------------------------------------------
// A running replay is normally fire-and-forget: res.ok means delivered and the
// entry is deleted unread. A clock_skew refusal is the one 2xx where that reads
// the response backwards - nothing was stored - and deleting the entry throws
// away the only copy of an EDGE-TRIGGERED command: kachinukiBoutFinal, which
// tells the server to advance the winner-stays sequence. The rest of a running
// payload is level-triggered and the next autosave restates it; that flag is
// not, so losing the entry stalls the encounter until the operator presses
// Record bout again.

describe('clock_skew recovery: a queued running write on the flush path', () => {
    it('relearns, reconstructs the stamp, retries once, and keeps the kachinuki command', async () => {
        const server = makeServer({ timeOk: true });
        const API = await loadWith(server);
        expect(server.timeCalls).toBe(1);

        // Offline: "Record bout" is queued, carrying the advancement command.
        server.online = false;
        const enqueuedAt = Date.now();
        expect(await API.recordScore('c1', 'mk', { status: 'running', kachinukiBoutFinal: true }, 'pw', null))
            .toMatchObject({ queued: true });
        await flushMicrotasks();
        expect(server.payloads.length).toBe(0);

        // The court runs on for a while before the wifi returns.
        await tick(30000);
        server.skew = SKEW_MS + 20000;
        server.online = true;
        server.writeReply = replies(skewRefusal(), {});
        window.dispatchEvent(new Event('online'));
        await tick(50);

        // Refused once, healed, retried once - not deleted as delivered.
        expect(server.payloads.length).toBe(2);
        expect(server.payloads[0].modifiedAt).toBe(enqueuedAt + SKEW_MS);
        // Reconstructed from enqueuedAt, exactly as a terminal replay is: the
        // write keeps its real age and must still lose to a genuinely newer
        // result rather than being made fresh by the act of retrying.
        expect(server.payloads[1].modifiedAt).toBe(enqueuedAt + SKEW_MS + 20000);
        expect(server.payloads[1].modifiedAt).toBeLessThan(Date.now() + SKEW_MS + 20000);
        // The whole point: the command survived the refusal.
        expect(server.payloads[1].kachinukiBoutFinal).toBe(true);
        // Landed on the retry, so the entry leaves the queue.
        expect(_lsStore.bc_write_queue).toBeUndefined();
    });

    it('refused twice: dropped exactly as a delivered entry is, with no banner', async () => {
        // Running state is level-triggered and the next autosave supersedes it,
        // so there is nothing here an operator could act on - only the devtools
        // warn, which names the kachinuki risk.
        const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
        const server = makeServer({ timeOk: true });
        const mod = await loadModWith(server);
        const API = mod.API;

        const failures = [];
        const alerts = [];
        const unsubFail = mod.subscribeTerminalWriteFailed((i) => failures.push(i));
        const unsubAlert = mod.subscribeQueueAlert((a) => alerts.push(a));

        server.online = false;
        await API.recordScore('c1', 'mk', { status: 'running' }, 'pw', null);
        await flushMicrotasks();

        server.online = true;
        server.writeReply = () => skewRefusal(); // refuses forever
        window.dispatchEvent(new Event('online'));
        await tick(2000);

        expect(server.payloads.length).toBe(2);
        expect(failures).toEqual([]);
        expect(alerts).toEqual([]);
        expect(_lsStore.bc_write_queue).toBeUndefined();
        expect(warnSpy).toHaveBeenCalledWith(expect.stringMatching(/clock skew/i));
        warnSpy.mockRestore();
        unsubFail();
        unsubAlert();
    });

    // The counter-cases. Everything that is NOT a clock_skew refusal must behave
    // exactly as it did before the body was ever parsed on this path: delivered,
    // deleted, silent. Without these, widening the parse could quietly start
    // re-stamping or re-sending running writes the server had already taken.
    it('a plain 200 is still delivered-and-deleted, with no relearn', async () => {
        const server = makeServer({ timeOk: true });
        const API = await loadWith(server);
        expect(server.timeCalls).toBe(1);

        server.online = false;
        await API.recordScore('c1', 'mk', { status: 'running' }, 'pw', null);
        await flushMicrotasks();

        server.online = true;
        window.dispatchEvent(new Event('online'));
        await tick(50);

        expect(server.payloads.length).toBe(1);
        expect(_lsStore.bc_write_queue).toBeUndefined();
        // The 'online' event relearns on its own (change E), so exactly one extra
        // call is expected and nothing more: the delivered body added none. Read
        // past the relearn throttle window, so a wrongly-fired relearn cannot be
        // hidden by the throttle instead of by the reason check.
        await tick(6000);
        expect(server.timeCalls).toBe(2);
    });

    it("a running entry refused as 'superseded' takes none of the skew branch", async () => {
        // The terminal path answers this one with a relearn and a banner. The
        // running path deliberately does neither, and the new parse must not
        // have changed that: no re-stamp, no retry, no notification.
        const server = makeServer({ timeOk: true });
        const mod = await loadModWith(server);
        const API = mod.API;
        expect(server.timeCalls).toBe(1);

        const failures = [];
        const alerts = [];
        const unsubFail = mod.subscribeTerminalWriteFailed((i) => failures.push(i));
        const unsubAlert = mod.subscribeQueueAlert((a) => alerts.push(a));

        server.online = false;
        const enqueuedAt = Date.now();
        await API.recordScore('c1', 'mk', { status: 'running' }, 'pw', null);
        await flushMicrotasks();

        await tick(30000);
        server.skew = SKEW_MS + 20000; // would be visible if anything re-stamped
        server.online = true;
        server.writeReply = () => ({ applied: false, reason: 'superseded' });
        window.dispatchEvent(new Event('online'));
        await tick(50);

        expect(server.payloads.length).toBe(1);
        expect(server.payloads[0].modifiedAt).toBe(enqueuedAt + SKEW_MS);
        expect(_lsStore.bc_write_queue).toBeUndefined();
        expect(failures).toEqual([]);
        expect(alerts).toEqual([]);
        // The 'online' event relearns on its own (change E), so one extra call is
        // expected here and nothing more: the supersede itself added none.
        expect(server.timeCalls).toBe(2);
        unsubFail();
        unsubAlert();
    });
});

// ---------------------------------------------------------------------------
// K. skewRetried survives a reload
// ---------------------------------------------------------------------------
// The mark is what makes the retry ONCE rather than forever, so it has to be
// part of the persisted entry and not just live memory. A reload landing between
// the re-stamp and the retry therefore costs the entry its retry - conservative
// by design, since the stamp it carries was already built from a learned offset.

describe('clock_skew recovery: the retried-once mark is persisted', () => {
    it('survives the localStorage round-trip, so the rehydrated entry is dropped on its FIRST refusal', async () => {
        const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
        const server = makeServer({ timeOk: true });
        const API = await loadWith(server);

        server.online = false;
        await API.recordScore('c1', 'mq', { status: 'completed' }, 'pw', null);
        await flushMicrotasks();

        // Reconnect: refused once and re-stamped, but the wifi drops again
        // before the retry can go out, so the entry stays queued WITH the mark.
        server.online = true;
        server.writeReply = () => { server.online = false; return skewRefusal(); };
        window.dispatchEvent(new Event('online'));
        await tick(50);

        expect(server.payloads.length).toBe(1);
        const persisted = JSON.parse(_lsStore.bc_write_queue);
        expect(persisted).toHaveLength(1);
        expect(persisted[0][1].skewRetried).toBe(true);

        // The operator reloads the tab. Fresh module state, same storage.
        vi.resetModules();
        const server2 = makeServer({ timeOk: true });
        server2.online = false; // so the rehydrate flush cannot run before we subscribe
        const mod2 = await loadModWith(server2);
        const failures = [];
        const unsub = mod2.subscribeTerminalWriteFailed((i) => failures.push(i));

        server2.online = true;
        server2.writeReply = () => skewRefusal();
        window.dispatchEvent(new Event('online'));
        await tick(50);

        // ONE attempt in the new session: the rehydrated mark says this entry has
        // already had its retry. Had the mark been dropped by the round-trip, the
        // entry would have been re-stamped and sent a second time instead.
        expect(server2.payloads.length).toBe(1);
        expect(failures.length).toBe(1);
        expect(failures[0]).toMatchObject({ compID: 'c1', matchID: 'mq', kind: 'score' });
        expect(_lsStore.bc_write_queue).toBeUndefined();
        expect(warnSpy).toHaveBeenCalledWith(expect.stringMatching(/clock skew/i));
        warnSpy.mockRestore();
        unsub();
    });
});
