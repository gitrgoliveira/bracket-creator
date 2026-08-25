// bc-cse: the clock-skew re-stamp takes the OLDER of two reconstructions.
//
// A queued write refused for clock_skew is re-stamped once before its retry.
// There are two independent ways to reconstruct "when did the operator act, in
// the server's frame", and each is wrong in a case the other survives:
//
//   wall = enqueuedAt + offset      breaks when the OS STEPS the wall clock
//                                   between enqueue and flush (the NTP jump on
//                                   reconnect). enqueuedAt is then recorded in a
//                                   frame the freshly learned offset does not
//                                   describe, so the re-stamp lands in the
//                                   server's future AGAIN and the entry drops.
//
//   perf = serverNow - trueAge      breaks when performance.now() FREEZES while
//                                   the device is suspended: the measured age is
//                                   short, so the write looks fresher than it is.
//
// The rule is Math.min, and it is a SAFETY property rather than a tie-break, so
// it is pinned in both directions here. An over-old stamp can only make this
// write lose a comparison it might have won - recoverable, and the operator is
// already being told. An over-NEW stamp lets a stale replay beat a result
// recorded during the outage, silently: the exact overwrite bc-lww1 exists to
// stop. Tested through the exported arithmetic rather than a full
// offline/refuse/reconnect fixture, because what needs pinning is which
// estimate wins, not that the queue can be driven to ask.
//
// The offset is 0 throughout (a freshly imported module that never reaches
// /api/time), so wall === enqueuedAt and serverNow === Date.now(), and each
// case is legible as plain arithmetic.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

const NOW = 1_700_000_000_000;

let _origFetch, _origEventSource, _origLocalStorage, _origPerf, _lsStore;
let _restampFor, _enqueueRunningWrite;

beforeEach(async () => {
    vi.useFakeTimers();
    vi.setSystemTime(NOW);
    vi.resetModules();

    _origEventSource = global.EventSource;
    global.EventSource = class FakeES {
        constructor() { this.readyState = 0; this.close = () => {}; }
    };
    global.EventSource.OPEN = 1;

    // /api/time never answers, so the offset stays 0 for the whole test.
    _origFetch = global.fetch;
    global.fetch = vi.fn(() => new Promise(() => {}));

    _origLocalStorage = global.localStorage;
    _lsStore = {};
    global.localStorage = {
        getItem: (k) => (k in _lsStore ? _lsStore[k] : null),
        setItem: (k, v) => { _lsStore[k] = String(v); },
        removeItem: (k) => { delete _lsStore[k]; },
        clear: () => { _lsStore = {}; },
    };

    _origPerf = global.performance;
    ({ _restampFor, enqueueRunningWrite: _enqueueRunningWrite } = await import('../api_client.jsx'));
});

afterEach(() => {
    vi.useRealTimers();
    vi.resetModules();
    for (const [k, v] of [['fetch', _origFetch], ['EventSource', _origEventSource],
                          ['localStorage', _origLocalStorage], ['performance', _origPerf]]) {
        if (v === undefined) delete global[k]; else global[k] = v;
    }
});

/** Pin performance.now() to a fixed reading for the duration of one call. */
function withPerfNow(ms, fn) {
    global.performance = { now: () => ms };
    try { return fn(); } finally { global.performance = _origPerf; }
}

describe('clock-skew re-stamp takes the older reconstruction (bc-cse)', () => {
    it('uses the wall clock when there is no monotonic anchor', () => {
        // A rehydrated entry: perfAtEnqueue is stripped on persist, so this is
        // what every write across a reload gets, and what an older build's
        // entries get. Must behave exactly as it did before the anchor existed.
        expect(withPerfNow(5_000, () => _restampFor(NOW - 60_000, undefined)))
            .toBe(NOW - 60_000);
    });

    it('prefers the monotonic estimate when the wall clock was stepped', () => {
        // The failure this option was chosen to close. The device was 10 min
        // fast when the operator scored and has since been corrected, so
        // enqueuedAt is 10 min in the future while the true age is 60s.
        const trueAgeMs = 60_000;
        const stamp = withPerfNow(500_000, () =>
            _restampFor(NOW + 600_000, 500_000 - trueAgeMs));
        expect(stamp).toBe(NOW - trueAgeMs);
        // And the point of it: no longer in the server's future, so the retry
        // is accepted rather than refused a second time and dropped.
        expect(stamp).toBeLessThan(NOW);
    });

    it('prefers the wall clock when the monotonic reading froze', () => {
        // The counter-case, and the reason this is min() and not "use perf when
        // present". A tablet suspended for 10 minutes whose performance.now()
        // did not advance reports an age of 1s; trusting it would stamp the
        // write as one second old and let it beat results recorded during the
        // outage.
        const stamp = withPerfNow(500_000, () =>
            _restampFor(NOW - 600_000, 499_000));
        expect(stamp).toBe(NOW - 600_000);
        expect(stamp).not.toBe(NOW - 1_000);
    });

    it('falls back when the two readings come from different time origins', () => {
        // A negative age is not a clock step, it is arithmetic across a reload
        // (or a hand-edited store). There is no true age to recover, so the
        // wall-clock estimate stands rather than an invented one.
        expect(withPerfNow(5_000, () => _restampFor(NOW - 60_000, 900_000)))
            .toBe(NOW - 60_000);
    });

    it('falls back where performance.now() does not exist', () => {
        global.performance = undefined;
        expect(_restampFor(NOW - 60_000, 1_000)).toBe(NOW - 60_000);
    });
});

// The anchor is only SAFE because it never crosses a session. performance.now()
// is measured from a per-document time origin, so a reading persisted by one
// page load and subtracted from a reading taken by the next is arithmetic on two
// different zeros - and the resulting age is unbounded in BOTH directions, which
// is exactly what min() cannot protect against (an over-large age stamps the
// write arbitrarily old; the negative guard in _restampFor only catches the
// other sign). Stripping on write is what keeps a rehydrated entry in the
// wall-clock-only case the tests above pin.
describe('the monotonic anchor never reaches localStorage (bc-cse)', () => {
    it('is stamped in memory but stripped from the persisted queue', () => {
        global.performance = { now: () => 12_345 };
        _enqueueRunningWrite('c1', 'm1', { status: 'running' }, 'pw');

        const raw = global.localStorage.getItem('bc_write_queue');
        expect(raw, 'the entry must actually have been persisted').toBeTruthy();
        expect(raw).not.toContain('perfAtEnqueue');

        // And the round trip a reload would perform yields no anchor, so
        // _restampFor takes the wall-clock branch for it.
        const [[, descriptor]] = JSON.parse(raw);
        expect(descriptor.perfAtEnqueue).toBeUndefined();
        expect(descriptor.enqueuedAt).toBe(NOW);
    });
});
