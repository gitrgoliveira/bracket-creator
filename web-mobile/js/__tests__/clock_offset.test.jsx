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
//      running write heals silently and does not resend.
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
//   L. The representative-bout add and remove carry the stamp too, hand a
//      refusal back as the refusal and relearn, and the stamp is what keeps a
//      refetch read before the add from replacing the copy it pushed.
//
// bc-evts (stage 3, the outbox): every rule-bound door (a finished or
// scheduled recordScore, recordDecision, overrideBracketWinner, the
// representative bout's add and remove, ...) now enqueues at once and answers
// `{ applied: true, pending: true, key, settled }`; `settled` is the server's
// eventual answer in the shape the direct call used to return, and a 4xx
// refusal rejects it with the Error the direct call used to throw. So the
// "direct" recordScore and override sites above are the QUEUE's flush now
// (only a RUNNING recordScore still goes direct), a clock_skew resend is the
// queue's re-stamp (_restampQueuedEntryForSkew), and a request given up at the
// 12 s deadline keeps its entry queued and replays it with the same stamp.
//
// The offset is never exported. It is observed through the one thing that
// matters - payload.modifiedAt on a captured request body.
//
// Timer strategy: advanceTimersByTimeAsync(N), never runAllTimersAsync (the
// retry chain re-schedules itself, as does the queue backoff).

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { writeDidNotLand, writeWasSuperseded, writeWasRefusedForClock } from '../write_result.jsx';

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

        // The operator re-enters the result: it now carries a corrected stamp
        // (once the relearn's answer is in: the write went through the outbox).
        await flushMicrotasks();
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
        // bc-evts: the door answers pending at once; the write stays queued.
        const queued = await API.recordScore('c1', 'mq', { status: 'completed' }, 'pw', null);
        expect(queued).toMatchObject({ applied: true, pending: true });
        await flushMicrotasks();
        expect(API.unsentWrites().total).toBe(1);
        expect(server.timeCalls).toBe(1); // a network failure is not a supersede signal

        // Reconnect: the flush delivers it and the server drops it as stale.
        server.online = true;
        server.writeReply = () => ({ applied: false, reason: 'superseded' });
        window.dispatchEvent(new Event('online'));
        await tick(50);

        expect(server.timeCalls).toBe(2);
        expect(await queued.settled).toMatchObject({ applied: false, reason: 'superseded' });
    });
});

describe('server-clock offset: a superseded override assertion relearns', () => {
    it('relearns when overrideBracketWinner is answered applied:false', async () => {
        const server = makeServer({ timeOk: true });
        const API = await loadWith(server);
        expect(server.timeCalls).toBe(1);

        server.writeReply = () => ({ applied: false });
        // bc-evts: the assertion is the outbox's; its answer arrives on `settled`.
        const r = await API.overrideBracketWinner('c1', 'm-r2-0', 'Alice', 'pw');
        expect(r).toMatchObject({ applied: true, pending: true });
        expect(await r.settled).toEqual({ applied: false });
        await flushMicrotasks();

        expect(server.timeCalls).toBe(2);
    });

    it('does NOT relearn when the server applies the assertion', async () => {
        const server = makeServer({ timeOk: true });
        const API = await loadWith(server);
        expect(server.timeCalls).toBe(1);

        server.writeReply = () => ({ applied: true });
        const r = await API.overrideBracketWinner('c1', 'm-r2-0', 'Alice', 'pw');
        expect(await r.settled).toEqual({ applied: true });

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
    // bc-evts: a finished write is the outbox's, so the resend is the queue's
    // re-stamp (_restampQueuedEntryForSkew), still exactly one, at once.
    it('is re-stamped by the queue, resent exactly once, and lands', async () => {
        const server = makeServer({ timeOk: true });
        const mod = await loadModWith(server);
        const API = mod.API;
        expect(server.timeCalls).toBe(1);

        const failures = [];
        const unsub = mod.subscribeTerminalWriteFailed((info) => failures.push(info));

        server.skew = SKEW_MS + 20000; // what a fresh learn will report
        server.writeReply = replies(skewRefusal(), {});

        const sentAt = Date.now();
        const res = await API.recordScore('c1', 'm1', { status: 'completed' }, 'pw', null);
        await tick(50);

        // Exactly two attempts: the refused one and the healed one.
        expect(server.payloads.length).toBe(2);
        expect(server.timeCalls).toBe(2);
        // The resend carries the CORRECTED stamp: the first stamp plus the
        // delta the learn discovered.
        expect(server.payloads[0].modifiedAt).toBe(sentAt + SKEW_MS);
        expect(server.payloads[1].modifiedAt).toBe(server.payloads[0].modifiedAt + 20000);
        // It landed, so nothing is reported and nothing is left queued.
        expect(await res.settled).toEqual({});
        expect(failures).toEqual([]);
        expect(_lsStore.bc_write_queue_v2).toBe('[]');
        unsub();
    });

    // bc-dhas: the resend meets whatever the server says about the match NOW;
    // a refusal carrying a sentence (the organiser finished the match, or
    // recorded the judges' decision, meanwhile) is thrown with the sentence,
    // as the first attempt's refusal is, never the bare code.
    // bc-evts: the refusal now rejects the door's `settled` (and the queue
    // reports it: the 'rejected' alert and its devtools warn).
    it('a resend refused with a sentence rejects settled with the sentence', async () => {
        const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
        const server = makeServer({ timeOk: true });
        const API = await loadWith(server);
        const answering = server.fetch;
        let writes = 0;
        global.fetch = vi.fn((url, opts) => {
            if (String(url).includes('/api/time') || !(opts && opts.body)) return answering(url, opts);
            writes++;
            if (writes === 1) return answering(url, opts); // server.writeReply: the skew refusal
            return Promise.resolve({
                ok: false, status: 409,
                json: () => Promise.resolve({
                    error: 'hantei_organiser_only',
                    message: 'The judges decided this representative bout (hantei). Ask the tournament organizer to change it.',
                }),
            });
        });
        server.writeReply = () => skewRefusal();

        const answer = await API.recordScore('c1', 'm1', { status: 'completed' }, '', null);
        expect(answer).toMatchObject({ pending: true });
        await expect(answer.settled)
            .rejects.toThrow('The judges decided this representative bout (hantei). Ask the tournament organizer to change it.');
        expect(writes).toBe(2);
        expect(API.unsentWrites().total).toBe(0);
        expect(warnSpy).toHaveBeenCalledWith(expect.stringMatching(/rejected/), expect.anything());
        warnSpy.mockRestore();
    });

    // bc-evts: the write is queued now (every finished write is); what is
    // pinned is that the second refusal drops it rather than loop.
    it('refused twice: kept queued and held, re-stamped on each retry, told once, and lands once the clock is right', async () => {
        // Review of PR #483: a write refused for the clock again after its
        // re-stamp was dropped for good, though the server logged nothing.
        // Now it is kept queued and held, re-stamped on every retry, told once,
        // and lands once the clock is right.
        const server = makeServer({ timeOk: true });
        const mod = await loadModWith(server);
        const API = mod.API;

        const failures = [];
        const alerts = [];
        const unsubFail = mod.subscribeTerminalWriteFailed((info) => failures.push(info));
        const unsubAlert = mod.subscribeQueueAlert((a) => alerts.push(a));

        server.writeReply = () => skewRefusal(); // refuses until the clock is fixed

        await API.recordScore('c1', 'm1', { status: 'completed' }, 'pw', null);
        await tick(0);

        // Re-stamped and resent at once, refused again: kept, never dropped.
        expect(server.payloads.length).toBe(2);
        expect(failures).toEqual([]);
        expect(API.heldWrites()).toEqual([expect.objectContaining({ matchID: 'm1', keepsFailing: true })]);
        expect(JSON.parse(_lsStore.bc_write_queue_v2)).toHaveLength(1);
        const told = alerts.filter((a) => a.kind === 'server_error');
        expect(told).toEqual([expect.objectContaining({ matchID: 'm1', terminalCount: 1, detail: expect.stringMatching(/clock/i) })]);

        // Retried on the backoff, re-stamped each time, told only once.
        await tick(600);
        expect(server.payloads.length).toBe(3);
        expect(alerts.filter((a) => a.kind === 'server_error')).toHaveLength(1);

        // The clock is right again: it lands and leaves the queue.
        server.writeReply = () => ({});
        await tick(1100);
        expect(server.payloads.length).toBe(4);
        expect(API.heldWrites()).toEqual([]);
        expect(_lsStore.bc_write_queue_v2).toBe('[]');
        expect(failures).toEqual([]);
        unsubFail();
        unsubAlert();
    });

    // bc-evts: a running write is an outbox entry, so a clock refusal is
    // handled as a queued running write's is (J below): re-stamped once and
    // resent, silently.
    it('a running write is healed silently: no banner, re-stamped and resent once', async () => {
        const server = makeServer({ timeOk: true });
        const mod = await loadModWith(server);
        const API = mod.API;
        expect(server.timeCalls).toBe(1);

        const failures = [];
        const unsub = mod.subscribeTerminalWriteFailed((info) => failures.push(info));

        server.writeReply = replies(skewRefusal(), {});
        await API.recordScore('c1', 'm1', { status: 'running' }, 'pw', null);
        await flushMicrotasks();
        await tick(50);

        // One resend with the corrected stamp, and no banner: one here would
        // fire on every running write of a skewed device.
        expect(server.payloads.length).toBe(2);
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
        // bc-evts: pending, and held in the queue while offline.
        expect(await API.recordScore('c1', 'mq', { status: 'completed' }, 'pw', null)).toMatchObject({ pending: true });
        await flushMicrotasks();
        expect(server.payloads.length).toBe(0);
        expect(API.hasPendingTerminalWrite('c1', 'mq')).toBe(true);

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
        expect(_lsStore.bc_write_queue_v2).toBe('[]');
    });

    it('refused twice: held, told once, and discardable', async () => {
        // Held, never dropped: told once, and the operator may discard it.
        const server = makeServer({ timeOk: true });
        const mod = await loadModWith(server);
        const API = mod.API;

        const failures = [];
        const alerts = [];
        const unsubFail = mod.subscribeTerminalWriteFailed((info) => failures.push(info));
        const unsubAlert = mod.subscribeQueueAlert((a) => alerts.push(a));

        server.online = false;
        const r = await API.recordScore('c1', 'mq', { status: 'completed' }, 'pw', null);
        await flushMicrotasks();

        server.online = true;
        server.writeReply = () => skewRefusal(); // refuses until the clock is fixed
        window.dispatchEvent(new Event('online'));
        await tick(0);

        expect(server.payloads.length).toBe(2);
        expect(failures).toEqual([]);
        expect(alerts.filter((a) => a.kind === 'server_error' && /clock/i.test(a.detail || ''))).toHaveLength(1);
        expect(alerts.some((a) => a.kind === 'rejected')).toBe(false);
        const held = API.heldWrites();
        expect(held).toEqual([expect.objectContaining({ matchID: 'mq', keepsFailing: true })]);

        // Discardable like any write the server keeps refusing.
        expect(API.discardHeldWrite(held[0].key)).toBeTruthy();
        expect(await r.settled).toEqual({ discarded: true });
        expect(_lsStore.bc_write_queue_v2).toBe('[]');
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
        // bc-evts: the supersede is the answer `settled` resolves with.
        expect(res).toMatchObject({ pending: true });
        expect(await res.settled).toMatchObject({ applied: false, reason: 'superseded' });
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
    // bc-evts: the resend is the queue's re-stamp; `settled` reports applied.
    it('relearns, is re-stamped by the queue and resent once, then settles applied', async () => {
        const server = makeServer({ timeOk: true });
        const API = await loadWith(server);
        expect(server.timeCalls).toBe(1);

        server.skew = SKEW_MS + 20000;
        server.writeReply = replies(skewRefusal(), { applied: true });

        const r = await API.overrideBracketWinner('c1', 'm-r2-0', 'Alice', 'pw');
        await tick(50);

        expect(await r.settled).toEqual({ applied: true });
        expect(server.payloads.length).toBe(2);
        expect(server.payloads[1].modifiedAt).toBe(server.payloads[0].modifiedAt + 20000);
        expect(server.timeCalls).toBe(2);
    });

    it('refused twice: kept, and settles with the answer once the clock is right', async () => {
        // Refused again after its re-stamp: kept, not settled as refused; it
        // settles with the server's answer once the clock is right.
        const server = makeServer({ timeOk: true });
        const API = await loadWith(server);

        server.writeReply = () => skewRefusal();
        const r = await API.overrideBracketWinner('c1', 'm-r2-0', 'Alice', 'pw');
        let settled = null;
        r.settled.then((v) => { settled = v; });
        await tick(0);

        expect(server.payloads.length).toBe(2);
        expect(settled).toBeNull();
        expect(API.heldWrites()).toEqual([expect.objectContaining({ matchID: 'm-r2-0', keepsFailing: true })]);

        server.writeReply = () => ({ applied: true });
        await tick(600);
        expect(await r.settled).toEqual({ applied: true });
        expect(server.payloads.length).toBe(3);
    });
});

describe('clock_skew recovery: a queued override assertion', () => {
    it('held for the clock, it asks for no resync and tells no failure', async () => {
        // Held, never dropped: the assertion stays queued and the local
        // advance stands on a write that will still land, so no resync is
        // asked for and no failure is told; the held write is told once.
        const server = makeServer({ timeOk: true });
        const mod = await loadModWith(server);
        const API = mod.API;

        const resyncs = [];
        const failures = [];
        const alerts = [];
        const unsubResync = mod.subscribeBracketResync((info) => resyncs.push(info));
        const unsubFail = mod.subscribeTerminalWriteFailed((info) => failures.push(info));
        const unsubAlert = mod.subscribeQueueAlert((a) => alerts.push(a));

        server.online = false;
        expect(await API.overrideBracketWinner('c1', 'm-r2-0', 'Alice', 'pw')).toMatchObject({ pending: true });
        await flushMicrotasks();

        server.online = true;
        server.writeReply = () => skewRefusal(); // refuses until the clock is fixed
        window.dispatchEvent(new Event('online'));
        await tick(0);

        expect(server.payloads.length).toBe(2);
        expect(resyncs).toEqual([]);
        expect(failures).toEqual([]);
        expect(alerts.filter((a) => a.kind === 'server_error')).toHaveLength(1);
        expect(JSON.parse(_lsStore.bc_write_queue_v2)).toHaveLength(1);
        unsubResync();
        unsubFail();
        unsubAlert();
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
// payload is level-triggered and the next running write restates it; that flag is
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
            .toMatchObject({ pending: true });
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
        expect(_lsStore.bc_write_queue_v2).toBe('[]');
    });

    it('refused twice: held like a result, told once, with no banner', async () => {
        // A running write is held for the clock exactly as a result is: kept
        // queued (its kachinuki command with it), told once, no banner.
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
        server.writeReply = () => skewRefusal(); // refuses until the clock is fixed
        window.dispatchEvent(new Event('online'));
        await tick(0);

        expect(server.payloads.length).toBe(2);
        expect(failures).toEqual([]);
        expect(alerts).toEqual([expect.objectContaining({ kind: 'server_error', matchID: 'mk', terminalCount: 0 })]);
        expect(API.heldWrites()).toEqual([expect.objectContaining({ matchID: 'mk', keepsFailing: true })]);
        expect(JSON.parse(_lsStore.bc_write_queue_v2)).toHaveLength(1);
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
        expect(_lsStore.bc_write_queue_v2).toBe('[]');
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
        expect(_lsStore.bc_write_queue_v2).toBe('[]');
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
    it('survives the localStorage round-trip, so the rehydrated entry is held on its FIRST refusal', async () => {
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
        const persisted = JSON.parse(_lsStore.bc_write_queue_v2);
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
        // already had its immediate resend, so its first refusal holds it (kept,
        // re-stamped, on the backoff), never dropped.
        expect(server2.payloads.length).toBe(1);
        expect(failures).toEqual([]);
        const kept = JSON.parse(_lsStore.bc_write_queue_v2);
        expect(kept).toHaveLength(1);
        expect(kept[0][1].clockRefused).toBe(true);
        expect(mod2.API.heldWrites()).toEqual([expect.objectContaining({ matchID: 'mq', keepsFailing: true })]);
        unsub();
    });
});

// ---------------------------------------------------------------------------
// L. The representative-bout add and remove (bc-dhas)
// ---------------------------------------------------------------------------
// Both carry the stamp, exactly as recordDecision does, so each competes on
// timestamps; a refusal is handed back as the refusal body (the team editor
// reports it through notLandedBanner) and relearns the offset like any other.

describe('the daihyosen add and remove carry the stamp', () => {
    it.each([
        ['add', 'POST', (API) => API.recordDaihyosen('c1', 'B1', 'pw')],
        ['remove', 'DELETE', (API) => API.removeDaihyosen('c1', 'B1', 'pw')],
    ])('the %s sends a server-relative modifiedAt in a JSON body', async (_name, method, send) => {
        const server = makeServer({ timeOk: true });
        const API = await loadWith(server);

        await send(API);

        const [url, opts] = server.fetch.mock.calls.find(([u]) => String(u).includes('/daihyosen'));
        expect(String(url)).toBe('/api/competitions/c1/matches/B1/daihyosen');
        expect(opts.method).toBe(method);
        expect(opts.headers['Content-Type']).toBe('application/json');
        expect(server.payloads).toHaveLength(1);
        expect(server.payloads[0].modifiedAt).toBe(Date.now() + SKEW_MS);
    });

    // bc-evts: the add and remove are the outbox's, so the refusal is what
    // `settled` resolves with. A clock_skew refusal is re-stamped by the queue
    // and resent at once first; refused again, it is kept queued and held for
    // the clock (never dropped: the server logged nothing).
    it.each([
        ['add', (API) => API.recordDaihyosen('c1', 'B1', 'pw')],
        ['remove', (API) => API.removeDaihyosen('c1', 'B1', 'pw')],
    ])('a superseded %s settles as the refusal and relearns the offset', async (_name, send) => {
        const server = makeServer({ timeOk: true });
        const API = await loadWith(server);
        expect(server.timeCalls).toBe(1);
        server.writeReply = () => ({ applied: false, reason: 'superseded', message: 'Not saved.' });

        const answer = await send(API);
        expect(answer).toMatchObject({ applied: true, pending: true });
        await tick(50);
        const res = await answer.settled;

        expect(writeDidNotLand(res)).toBe(true);
        expect(writeWasSuperseded(res)).toBe(true);
        expect(writeWasRefusedForClock(res)).toBe(false);
        expect(server.timeCalls).toBe(2);
        expect(server.payloads).toHaveLength(1);
        expect(API.unsentWrites().total).toBe(0);
    });

    it.each([
        ['add', (API) => API.recordDaihyosen('c1', 'B1', 'pw')],
        ['remove', (API) => API.removeDaihyosen('c1', 'B1', 'pw')],
    ])('a %s refused for the clock twice is kept and held, never dropped', async (_name, send) => {
        const server = makeServer({ timeOk: true });
        const API = await loadWith(server);
        server.writeReply = () => ({ applied: false, reason: 'clock_skew', message: 'Not saved.' });

        const answer = await send(API);
        let settled = null;
        answer.settled.then((v) => { settled = v; });
        await tick(50);

        // Re-stamped (its own learn) and resent at once, refused again: kept.
        expect(server.timeCalls).toBe(3);
        expect(server.payloads).toHaveLength(2);
        expect(settled).toBeNull();
        expect(API.unsentWrites().total).toBe(1);
        expect(API.heldWrites()).toEqual([expect.objectContaining({ matchID: 'B1', keepsFailing: true })]);
    });

    // The team editor holds every running write on the court until the add or
    // remove settles. bc-evts: the request is the queue's now, still bounded
    // by _fetchJson's 12 s rather than left to the browser to fail a
    // half-open connection; given up, the entry STAYS queued (a network
    // failure, never a refusal), `settled` waits for a real answer, and the
    // backoff replays it with the same stamp.
    //
    // `first` is how the first request fails to answer: a connection that
    // never answers (it settles only when its signal aborts it, like a real
    // fetch), or headers whose body never completes (fetchWithTimeout's abort
    // used to end when the headers arrived; _fetchJson reads the body by the
    // same deadline). `retry` is the answer the replay gets.
    const repMatch = { id: 'B1', status: 'running', subResults: [] };
    const neverAnswers = (opts) => new Promise((_resolve, reject) => {
        if (opts && opts.signal) {
            opts.signal.addEventListener('abort', () => reject(new DOMException('The operation was aborted.', 'AbortError')));
        }
    });
    const bodyNeverCompletes = (ok) => () => Promise.resolve({ ok, status: ok ? 200 : 409, json: () => new Promise(() => {}) });
    const landedReply = () => Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ result: repMatch }) });
    const refusedReply = () => Promise.resolve({ ok: false, status: 409, json: () => Promise.resolve({ error: 'daihyosen_scored' }) });

    it.each([
        ['add that is never answered', 'POST', (API) => API.recordDaihyosen('c1', 'B1', 'pw'), neverAnswers, landedReply],
        ['remove that is never answered', 'DELETE', (API) => API.removeDaihyosen('c1', 'B1', 'pw'), neverAnswers, landedReply],
        ['add whose body never completes', 'POST', (API) => API.recordDaihyosen('c1', 'B1', 'pw'), bodyNeverCompletes(true), landedReply],
        ['remove whose body never completes', 'DELETE', (API) => API.removeDaihyosen('c1', 'B1', 'pw'), bodyNeverCompletes(true), landedReply],
        ['refused add whose body never completes', 'POST', (API) => API.recordDaihyosen('c1', 'B1', 'pw'), bodyNeverCompletes(false), refusedReply],
    ])('the %s is given up at 12 s, stays queued, and is replayed with the same stamp', async (_name, method, send, first, retry) => {
        const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
        const server = makeServer({ timeOk: true });
        const API = await loadWith(server);
        const answering = server.fetch;
        const sent = [];
        global.fetch = vi.fn((url, opts) => {
            if (!String(url).includes('/daihyosen')) return answering(url, opts);
            sent.push({ method: opts.method, body: JSON.parse(opts.body) });
            return sent.length === 1 ? first(opts) : retry();
        });
        const answer = await send(API);
        expect(answer).toMatchObject({ applied: true, pending: true });
        let outcome = null;
        answer.settled.then((value) => { outcome = { value }; }, (error) => { outcome = { error }; });

        await tick(11000);
        expect(outcome, 'still waiting inside the 12 s').toBeNull();
        expect(sent).toHaveLength(1);
        // Past the deadline: given up, but kept, and `settled` still waits.
        await tick(1100);
        expect(outcome, 'a request given up on is not an answer').toBeNull();
        expect(API.hasPendingTerminalWrite('c1', 'B1')).toBe(true);
        expect(API.unsentWrites().total).toBe(1);

        // The backoff replays it, as it does a write whose connection failed.
        await tick(1000);
        expect(sent).toHaveLength(2);
        expect(sent[1].method).toBe(method);
        expect(sent[1].body.modifiedAt).toBe(sent[0].body.modifiedAt);
        expect(API.unsentWrites().total, 'out of the queue once answered').toBe(0);
        if (retry === landedReply) {
            expect(outcome).toEqual({ value: repMatch });
        } else {
            expect(outcome && outcome.error && outcome.error.message).toBe('daihyosen_scored');
        }
        warnSpy.mockRestore();
    });

    // ONE controller stays armed through the body read (_fetchJson): a stalled
    // body is ABORTED at the deadline, which frees its connection, instead of
    // being left reading. bc-evts: the read is the flush's now, and the
    // entry stays queued after it.
    it('a stalled body is aborted at the deadline, freeing its connection', async () => {
        const server = makeServer({ timeOk: true });
        const API = await loadWith(server);
        const answering = server.fetch;
        let signal = null;
        global.fetch = vi.fn((url, opts) => {
            if (!String(url).includes('/daihyosen')) return answering(url, opts);
            if (!signal) signal = opts.signal; // the first request's
            return Promise.resolve({ ok: true, status: 200, json: () => new Promise(() => {}) });
        });
        await API.recordDaihyosen('c1', 'B1', 'pw');

        await tick(11000);
        expect(signal && signal.aborted, 'still reading inside the 12 s').toBe(false);
        await tick(1100);
        expect(signal.aborted, 'the body read is aborted, not just stopped waiting for').toBe(true);
        expect(API.unsentWrites().total, 'given up on, still queued').toBe(1);
    });

    // The budget runs on a timer, not the wall clock: a device clock stepped
    // forward while the request is out (an NTP correction) must not cut it
    // short and leave a landed add unsettled.
    it('a step of the wall clock during the request does not cut its budget', async () => {
        const server = makeServer({ timeOk: true });
        const API = await loadWith(server);
        const answering = server.fetch;
        let answerHeaders;
        const row = { position: -1, sideA: '', sideB: '', winner: '', decision: 'daihyosen' };
        const body = { subResult: row, result: { id: 'B1', status: 'running', subResults: [row] } };
        global.fetch = vi.fn((url, opts) => {
            if (!String(url).includes('/daihyosen')) return answering(url, opts);
            return new Promise((resolve) => { answerHeaders = resolve; });
        });
        let outcome = null;
        const answer = await API.recordDaihyosen('c1', 'B1', 'pw');
        answer.settled.then((res) => { outcome = { res }; }, (error) => { outcome = { error }; });
        await tick(100);

        // The clock steps forward while the request is out; then the answer
        // arrives, its body a moment after its headers.
        vi.setSystemTime(Date.now() + 20000);
        answerHeaders({ ok: true, status: 200, json: () => new Promise((resolve) => setTimeout(() => resolve(body), 50)) });
        await tick(100);

        expect(outcome && outcome.error, 'not reported as unanswered').toBeFalsy();
        expect(outcome && outcome.res).toEqual({ id: 'B1', status: 'running', subResults: [row] });
        expect(API.unsentWrites().total).toBe(0);
    });

    // A refused add or remove shows the server's own sentence when it sends
    // one (a finished match's, for a participant); a bare code is thrown as it
    // is, for the editor's own wording of it. bc-evts: it rejects `settled`,
    // and the queue reports the refusal (its devtools warn).
    it.each([
        ['an add refused with a sentence', (API) => API.recordDaihyosen('c1', 'B1', ''), { error: 'result_finalized', message: 'This match result has already been reported. Contact the tournament organizer to correct it.' },
            'This match result has already been reported. Contact the tournament organizer to correct it.'],
        ['a remove refused with a sentence', (API) => API.removeDaihyosen('c1', 'B1', ''), { error: 'result_finalized', message: 'This match result has already been reported. Contact the tournament organizer to correct it.' },
            'This match result has already been reported. Contact the tournament organizer to correct it.'],
        ['a remove refused with a bare code', (API) => API.removeDaihyosen('c1', 'B1', 'pw'), { error: 'daihyosen_scored' }, 'daihyosen_scored'],
    ])('%s rejects settled with the words the editor shows', async (_name, send, reply, words) => {
        const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
        const server = makeServer({ timeOk: true });
        const API = await loadWith(server);
        const answering = server.fetch;
        global.fetch = vi.fn((url, opts) => {
            if (!String(url).includes('/daihyosen')) return answering(url, opts);
            return Promise.resolve({ ok: false, status: 409, json: () => Promise.resolve(reply) });
        });

        const answer = await send(API);
        await expect(answer.settled).rejects.toThrow(words);
        expect(API.unsentWrites().total, 'a refusal leaves the queue').toBe(0);
        expect(warnSpy).toHaveBeenCalledWith(expect.stringMatching(/rejected/), expect.anything());
        warnSpy.mockRestore();
    });

    it('a landed add is the match, unwrapped from its envelope', async () => {
        const server = makeServer({ timeOk: true });
        const API = await loadWith(server);
        const row = { position: -1, sideA: '', sideB: '', ipponsA: null, ipponsB: null, winner: '', decision: 'daihyosen' };
        server.writeReply = () => ({ subResult: row, result: { id: 'B1', status: 'running', subResults: [row] } });

        // bc-evts: the stored match is what `settled` resolves with.
        const res = await (await API.recordDaihyosen('c1', 'B1', 'pw')).settled;

        expect(res).toEqual({ id: 'B1', status: 'running', subResults: [row] });
        expect(writeDidNotLand(res)).toBe(false);
        expect(server.timeCalls).toBe(1);
    });

    // The window the stamp closes, end to end on the client. A refetch that
    // read the match just before the add committed can answer after the add's
    // push was applied; keepNewerMatches keeps a held running copy only when it
    // is STRICTLY newer. The fake answers as the handler does (pinned by
    // TestDaihyosenWrites_CompeteOnTimestamps): the match it returns, and
    // broadcasts, carries the request's stamp; an unstamped add wrote the
    // bracket projection, which carries none, so the held copy kept the
    // pre-add stamp and the stale refetch replaced it.
    it('a refetch read before the add does not replace the running copy the add pushed', async () => {
        const server = makeServer({ timeOk: true });
        const API = await loadWith(server);
        const { applyPatch, keepNewerMatches } = await import('../patch.jsx');
        // The pre-save the editor sends first lands 50ms before the add goes.
        const b1 = { id: 'B1', sideA: 'TeamA', sideB: 'TeamB', status: 'running', modifiedAt: Date.now() + SKEW_MS - 50, subResults: [] };
        const row = { position: -1, sideA: '', sideB: '', ipponsA: null, ipponsB: null, hansokuA: 0, hansokuB: 0, winner: '', decision: 'daihyosen' };
        server.writeReply = () => {
            const [, opts] = server.fetch.mock.calls.findLast(([u]) => String(u).includes('/daihyosen'));
            const stamp = opts && opts.body ? JSON.parse(opts.body).modifiedAt : 0;
            const result = { ...b1, subResults: [row] };
            delete result.modifiedAt;
            if (stamp) result.modifiedAt = stamp;
            return { subResult: row, result };
        };
        const comp = (m) => ({ id: 'c1', poolMatches: [], bracket: { rounds: [[m]], thirdPlaceMatch: null } });

        const added = await (await API.recordDaihyosen('c1', 'B1', 'pw')).settled;
        const held = applyPatch(comp(b1), { type: 'match_updated', data: { competitionId: 'c1', matchId: 'B1', result: added } });
        const out = keepNewerMatches(held, comp({ ...b1 }));

        expect(out.bracket.rounds[0][0].subResults.some((s) => s.position === -1)).toBe(true);
    });
});

// The organiser's refresh after a write (admin.jsx refreshCompsBestEffort)
// asks for the aggregate bounded: a score editor waits on that refresh after
// every write it makes, the save before a representative-bout add included,
// so one left hanging held the editor, and the add, for good. The bound is the
// same one controller through the body read (_fetchJson). Every other caller
// still waits as long as the aggregate takes.
describe('the organiser\'s refresh after a write is bounded', () => {
    const comps = [{ id: 'c1', name: 'Open', kind: 'individual', format: 'knockout', status: 'knockout', players: [], poolMatches: [], bracket: null }];

    it('gives up after 12 s when the server never answers', async () => {
        const server = makeServer({ timeOk: true });
        const API = await loadWith(server);
        const answering = server.fetch;
        let signal = null;
        global.fetch = vi.fn((url, opts) => {
            if (!String(url).includes('/api/viewer/competitions')) return answering(url, opts);
            signal = opts && opts.signal;
            return new Promise((_resolve, reject) => {
                if (signal) signal.addEventListener('abort', () => reject(new DOMException('The operation was aborted.', 'AbortError')));
            });
        });
        let outcome = null;
        API.fetchCompetitions({ bounded: true }).then(() => { outcome = { landed: true }; }, (error) => { outcome = { error }; });

        await tick(11000);
        expect(outcome, 'still waiting inside the 12 s').toBeNull();
        await tick(1500);
        expect(outcome && outcome.error, 'given up').toBeTruthy();
        expect(outcome.error.timedOut, 'reported as given up on, not as failed').toBe(true);
        expect(signal.aborted).toBe(true);
    });

    it('aborts a body that never completes at the same deadline', async () => {
        const server = makeServer({ timeOk: true });
        const API = await loadWith(server);
        const answering = server.fetch;
        let signal = null;
        global.fetch = vi.fn((url, opts) => {
            if (!String(url).includes('/api/viewer/competitions')) return answering(url, opts);
            signal = opts.signal;
            return Promise.resolve({ ok: true, status: 200, json: () => new Promise(() => {}) });
        });
        let outcome = null;
        API.fetchCompetitions({ bounded: true }).then(() => { outcome = { landed: true }; }, (error) => { outcome = { error }; });

        await tick(11000);
        expect(signal.aborted, 'still reading inside the 12 s').toBe(false);
        await tick(1500);
        expect(signal.aborted).toBe(true);
        expect(outcome && outcome.error, 'given up').toBeTruthy();
        expect(outcome.error.timedOut, 'reported as given up on, not as failed').toBe(true);
    });

    // The request is given up on at the deadline and the fetch then rejects
    // with its own AbortError, as a real one does: still the one timeout.
    it('reports the deadline as the one timeout when the fetch rejects with its own abort error', async () => {
        const server = makeServer({ timeOk: true });
        const API = await loadWith(server);
        const answering = server.fetch;
        global.fetch = vi.fn((url, opts) => {
            if (!String(url).includes('/api/viewer/competitions')) return answering(url, opts);
            return new Promise((_resolve, reject) => {
                opts.signal.addEventListener('abort', () => reject(new DOMException('The operation was aborted.', 'AbortError')));
            });
        });
        let outcome = null;
        API.fetchCompetitions({ bounded: true }).then(() => { outcome = { landed: true }; }, (error) => { outcome = { error }; });

        await tick(12500);
        expect(outcome && outcome.error && outcome.error.timedOut).toBe(true);
        expect(outcome.error.name).not.toBe('AbortError');
    });

    it('does not report a failure that is not the deadline as a timeout', async () => {
        const server = makeServer({ timeOk: true });
        const API = await loadWith(server);
        const answering = server.fetch;
        global.fetch = vi.fn((url, opts) => {
            if (!String(url).includes('/api/viewer/competitions')) return answering(url, opts);
            return Promise.reject(new TypeError('network error'));
        });

        const error = await API.fetchCompetitions({ bounded: true }).catch((e) => e);

        expect(error).toBeInstanceOf(TypeError);
        expect(error.timedOut).toBeFalsy();
    });

    // What the organiser is told (admin.jsx refreshFailureToast), from the
    // errors this request really rejects with: a refresh given up on says
    // nothing, since the write it follows reaches the page by push and a
    // reload would fetch the same slow aggregate; any other failure keeps the
    // hint to reload.
    it('a refresh given up on toasts nothing; a network failure or an unreadable answer toasts the reload hint', async () => {
        const { refreshFailureToast } = await import('../admin.jsx');
        const server = makeServer({ timeOk: true });
        const API = await loadWith(server);
        const answering = server.fetch;
        let reply = null;
        global.fetch = vi.fn((url, opts) => {
            if (!String(url).includes('/api/viewer/competitions')) return answering(url, opts);
            return reply(opts);
        });
        const failWith = async (make) => {
            reply = make;
            let error = null;
            const pending = API.fetchCompetitions({ bounded: true }).catch((e) => { error = e; });
            await tick(12500);
            await pending;
            return error;
        };

        const timedOut = await failWith(() => new Promise(() => {}));
        const network = await failWith(() => Promise.reject(new TypeError('network error')));
        const unreadable = await failWith(() => Promise.resolve({ ok: true, status: 200, json: () => Promise.reject(new SyntaxError('Unexpected end of JSON input')) }));

        expect(refreshFailureToast('Score', timedOut)).toBeNull();
        expect(refreshFailureToast('Score', network)).toBe('Score succeeded; refresh failed. Reload to see latest');
        expect(refreshFailureToast('Score', unreadable)).toBe('Score succeeded; refresh failed. Reload to see latest');
    });

    it('answers with the competitions when the server does', async () => {
        const server = makeServer({ timeOk: true });
        const API = await loadWith(server);
        const answering = server.fetch;
        global.fetch = vi.fn((url, opts) => {
            if (!String(url).includes('/api/viewer/competitions')) return answering(url, opts);
            return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(comps) });
        });

        const out = await API.fetchCompetitions({ bounded: true });

        expect(Array.isArray(out)).toBe(true);
        expect(out.map((c) => c.id)).toEqual(['c1']);
    });

    // _fetchJson reads an unreadable body as {}; handed on, the refresh would
    // set the tournament's competitions to {} and break the admin.
    it('refuses an answer that is not a list, rather than handing it on', async () => {
        const server = makeServer({ timeOk: true });
        const API = await loadWith(server);
        const answering = server.fetch;
        global.fetch = vi.fn((url, opts) => {
            if (!String(url).includes('/api/viewer/competitions')) return answering(url, opts);
            return Promise.resolve({ ok: true, status: 200, json: () => Promise.reject(new SyntaxError('Unexpected end of JSON input')) });
        });

        await expect(API.fetchCompetitions({ bounded: true })).rejects.toThrow('Failed to fetch competitions');
    });

    it('leaves every other caller unbounded', async () => {
        const server = makeServer({ timeOk: true });
        const API = await loadWith(server);
        const answering = server.fetch;
        let opts = 'not called';
        global.fetch = vi.fn((url, o) => {
            if (!String(url).includes('/api/viewer/competitions')) return answering(url, o);
            opts = o;
            return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(comps) });
        });

        await API.fetchCompetitions();

        expect(opts === undefined || !opts.signal, 'no deadline on the plain call').toBe(true);
    });
});

// A score write's deadline covers its answer's body as well as its headers
// (_fetchJson). fetchWithTimeout's abort ended when the headers arrived, so a
// server that sent them and then stalled kept the write pending for good, and
// with it the editor's `submitting`, which disables Finish and Close. Past the
// deadline the write is a network failure like any other: queued ({queued:
// true}) and replayed with the stamp it was sent with, which is safe if the
// server did store it, since the write guard is last-write-wins and an equal
// stamp re-applies.
describe('a score write whose answer never completes is queued, not held', () => {
    const stalledBody = () => Promise.resolve({ ok: true, status: 200, json: () => new Promise(() => {}) });
    const answered = () => Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({}) });

    // bc-evts: a finished score, a decision and an override are the outbox's:
    // the door answers pending at once, and the 12 s deadline (_fetchJson,
    // inside the flush) gives the request up while the entry stays queued and
    // `settled` waits; the backoff replays it with the same stamp.
    it.each([
        ['a finished score', '/score', (API) => API.recordScore('c1', 'm1', { status: 'completed' }, 'pw', null)],
        ['a decision', '/decision', (API) => API.recordDecision('c1', 'm1', { decision: 'kiken-voluntary', decisionBy: 'shiro' }, 'pw')],
        ['a winner override', '/override-winner', (API) => API.overrideBracketWinner('c1', 'm1', 'Alice', 'pw')],
    ])('%s is given up at 12 s, stays queued, and is replayed with the same stamp', async (_name, path, send) => {
        const server = makeServer({ timeOk: true });
        const API = await loadWith(server);
        const answering = server.fetch;
        const sent = [];
        global.fetch = vi.fn((url, opts) => {
            if (!String(url).includes(path)) return answering(url, opts);
            sent.push(JSON.parse(opts.body));
            return sent.length === 1 ? stalledBody() : answered();
        });
        const answer = await send(API);
        expect(answer).toMatchObject({ applied: true, pending: true });
        let outcome = null;
        answer.settled.then((value) => { outcome = { value }; }, (error) => { outcome = { error }; });

        await tick(11000);
        expect(outcome, 'still reading inside the 12 s').toBeNull();
        await tick(1100);
        expect(outcome, 'given up on, but not answered').toBeNull();
        expect(API.hasPendingTerminalWrite('c1', 'm1'), 'kept queued, not thrown').toBe(true);
        expect(sent).toHaveLength(1);

        // The queue replays it, as it does a write whose connection failed.
        await tick(1000);
        expect(sent).toHaveLength(2);
        expect(sent[1].modifiedAt).toBe(sent[0].modifiedAt);
        expect(sent[1].rev).toBe(sent[0].rev);
        expect(API.unsentWrites().total, 'delivered and out of the queue').toBe(0);
        expect(outcome && 'value' in outcome, 'settled with the answer').toBe(true);
    });

    // A RUNNING score is the outbox's too (bc-evts): answered pending at once;
    // its send, given up at 12 s, stays queued and is replayed with the same
    // stamp and rev.
    it('a running score is given up at 12 s, kept queued, and replayed with the same stamp', async () => {
        const server = makeServer({ timeOk: true });
        const API = await loadWith(server);
        const answering = server.fetch;
        const sent = [];
        global.fetch = vi.fn((url, opts) => {
            if (!String(url).includes('/score')) return answering(url, opts);
            sent.push(JSON.parse(opts.body));
            return sent.length === 1 ? stalledBody() : answered();
        });
        const answer = await API.recordScore('c1', 'm1', { status: 'running' }, 'pw', null);
        expect(answer).toMatchObject({ applied: true, pending: true });
        expect(writeDidNotLand(answer)).toBe(false);
        let outcome = null;
        answer.settled.then((value) => { outcome = { value }; }, (error) => { outcome = { error }; });

        await tick(11000);
        expect(outcome, 'still reading inside the 12 s').toBeNull();
        await tick(1100);
        expect(outcome, 'given up on, but not answered').toBeNull();
        expect(API.unsentWrites().total, 'kept queued, not thrown').toBe(1);
        expect(sent).toHaveLength(1);

        await tick(1000);
        expect(sent).toHaveLength(2);
        expect(sent[1].modifiedAt).toBe(sent[0].modifiedAt);
        expect(sent[1].rev).toBe(sent[0].rev);
        expect(API.unsentWrites().total, 'delivered and out of the queue').toBe(0);
        expect(outcome && 'value' in outcome, 'settled with the answer').toBe(true);
    });

    // The replay loop is single-flight: one replay whose body never completed
    // stopped every later flush, so the queue the write above goes into must
    // be bounded the same way or the hang only moves there.
    it('a queued write whose replay answer never completes does not stop the queue', async () => {
        const server = makeServer({ timeOk: true });
        const API = await loadWith(server);
        const answering = server.fetch;
        const sent = [];
        global.fetch = vi.fn((url, opts) => {
            if (!String(url).includes('/score')) return answering(url, opts);
            sent.push(JSON.parse(opts.body));
            if (sent.length === 1) return Promise.reject(new TypeError('network error'));
            return sent.length === 2 ? stalledBody() : answered();
        });

        // bc-evts: the finished score is the outbox's at once; its first send
        // fails on the network, so it stays queued, and the backoff (500 ms)
        // replays it; that replay's answer never completes.
        const answer = await API.recordScore('c1', 'm1', { status: 'completed' }, 'pw', null);
        expect(answer).toMatchObject({ applied: true, pending: true });
        await flushMicrotasks();
        expect(sent).toHaveLength(1);
        await tick(500);
        expect(sent).toHaveLength(2);

        await tick(11000);
        expect(API.unsentWrites().total, 'still waiting on the replay inside the 12 s').toBe(1);
        // Given up at the deadline (12.5 s), kept, and retried after the
        // next backoff (1 s).
        await tick(1500);
        expect(sent).toHaveLength(2);
        await tick(1000);
        expect(sent).toHaveLength(3);
        expect(sent[2].modifiedAt).toBe(sent[0].modifiedAt);
        await expect(answer.settled).resolves.toEqual({});
        expect(API.unsentWrites().total, 'delivered and out of the queue').toBe(0);
    });
});
