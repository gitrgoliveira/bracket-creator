// Tests for C2 offline write queue + sync status indicator in api_client.jsx.
//
// Tests cover:
//   1. _nextRev via recordScore stamping: monotonic per-match counter
//   2. enqueueRunningWrite: last-write-wins semantics
//   3. _flushQueue: 409 → discard; network error → keep + backoff
//   4. subscribeSyncStatus: state transitions synced → syncing → offline → synced
//   5. window.online flush trigger
//   6. recordScore: running writes stamp rev + queue on network failure
//
// Timer strategy: Use advanceTimersByTimeAsync(N) rather than runAllTimersAsync
// to avoid infinite loops caused by the backoff re-scheduling itself.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

// Setup / teardown

let mod;
let API;
let subscribeSyncStatus;
let enqueueRunningWrite;

let _origFetch;
let _origEventSource;
let _origLocalStorage;
let _lsStore;

beforeEach(async () => {
    vi.useFakeTimers();
    vi.resetModules();

    // Provide a minimal EventSource stub (needed for module-level code in api_client).
    _origEventSource = global.EventSource;
    global.EventSource = class FakeES {
        constructor() { this.close = () => {}; }
    };
    global.EventSource.OPEN = 1;

    _origFetch = global.fetch;

    // Functional in-memory localStorage so F4 persist/rehydrate is testable
    // (jsdom's can be unavailable here). Reset before the import so the module's
    // rehydrate IIFE sees a clean store.
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
    subscribeSyncStatus = mod.subscribeSyncStatus;
    enqueueRunningWrite = mod.enqueueRunningWrite;
});

afterEach(() => {
    vi.useRealTimers();
    vi.resetModules();
    global.fetch = _origFetch;
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

/** Resolve all pending microtasks without advancing fake timers. */
async function flushMicrotasks() {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
}

/** Advance timers by the given ms, then flush microtasks. */
async function tick(ms = 0) {
    await vi.advanceTimersByTimeAsync(ms);
    await flushMicrotasks();
}

// 1. Monotonic revision counter

describe('_nextRev via recordScore stamping', () => {
    it('stamps rev on running writes and increments per matchId', async () => {
        const payloads = [];
        global.fetch = vi.fn().mockImplementation((_url, opts) => {
            payloads.push(JSON.parse(opts.body));
            return Promise.resolve({ ok: true, json: () => Promise.resolve({}) });
        });

        await API.recordScore('c1', 'm1', { status: 'running' }, 'pw', null);
        await API.recordScore('c1', 'm1', { status: 'running' }, 'pw', null);
        await API.recordScore('c1', 'm2', { status: 'running' }, 'pw', null);

        // m1 writes: rev 1 then 2
        expect(payloads[0].rev).toBe(1);
        expect(payloads[1].rev).toBe(2);
        // m2 writes: rev 1 (independent counter)
        expect(payloads[2].rev).toBe(1);
    });

    it('does NOT stamp rev on completed writes', async () => {
        const payloads = [];
        global.fetch = vi.fn().mockImplementation((_url, opts) => {
            payloads.push(JSON.parse(opts.body));
            return Promise.resolve({ ok: true, json: () => Promise.resolve({}) });
        });

        await API.recordScore('c1', 'm1', { status: 'completed' }, 'pw', null);
        expect(payloads[0].rev).toBeUndefined();
    });
});

// 2. Offline queue: last-write-wins

describe('enqueueRunningWrite: last-write-wins semantics', () => {
    it('replaces a stale pending write for the same matchId', async () => {
        let callCount = 0;
        const sentPayloads = [];

        global.fetch = vi.fn().mockImplementation((_url, opts) => {
            callCount++;
            const body = JSON.parse(opts.body);
            if (callCount === 1) {
                // First attempt fails (network down).
                return Promise.reject(new TypeError('network error'));
            }
            // Subsequent attempts succeed.
            sentPayloads.push(body);
            return Promise.resolve({ ok: true, json: () => Promise.resolve({}) });
        });

        // First (older) write for m1.
        enqueueRunningWrite('c1', 'm1', { status: 'running', rev: 1, ipponsA: [] }, 'pw');
        await flushMicrotasks(); // immediate flush attempt: fails, queue retains entry

        // Second (newer) write for m1; replaces the first in the queue.
        enqueueRunningWrite('c1', 'm1', { status: 'running', rev: 2, ipponsA: ['M'] }, 'pw');
        await flushMicrotasks(); // immediate flush with updated payload: should succeed

        // Only the newer write (rev 2) should have reached the server.
        expect(sentPayloads.length).toBe(1);
        expect(sentPayloads[0].rev).toBe(2);
    });

    it('queues different matchIds independently', async () => {
        let attempt = 0;
        const sentPayloads = [];

        global.fetch = vi.fn().mockImplementation((_url, opts) => {
            attempt++;
            if (attempt <= 2) {
                // Fail the first immediate flush for both matches.
                return Promise.reject(new TypeError('network error'));
            }
            sentPayloads.push(JSON.parse(opts.body));
            return Promise.resolve({ ok: true, json: () => Promise.resolve({}) });
        });

        enqueueRunningWrite('c1', 'm1', { status: 'running', rev: 1 }, 'pw');
        enqueueRunningWrite('c1', 'm2', { status: 'running', rev: 1 }, 'pw');
        await flushMicrotasks(); // first flush fails

        // Advance past the 500ms backoff to trigger the retry.
        await tick(600);

        expect(sentPayloads.length).toBe(2);
    });
});

// 3. Flush behaviour: 409 → discard; network error → keep and backoff

describe('_flushQueue: non-retryable 4xx discards, 5xx/429/network retries', () => {
    it('discards a queued write on a real 409 conflict (not retried)', async () => {
        // The 409 handler calls console.warn for devtools visibility; expect it.
        const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});

        let attempt = 0;
        global.fetch = vi.fn().mockImplementation(() => {
            attempt++;
            // Always return 409: simulates a real conflict (ineligible_competitor etc.).
            return Promise.resolve({
                ok: false,
                status: 409,
                json: () => Promise.resolve({ error: 'ineligible_competitor' }),
            });
        });

        enqueueRunningWrite('c1', 'm1', { status: 'running', rev: 1 }, 'pw');
        await flushMicrotasks();

        const callsAfterDiscard = attempt;
        // Advance timers; no retry should fire because the entry was discarded.
        await tick(1000);
        expect(global.fetch).toHaveBeenCalledTimes(callsAfterDiscard);

        // Verify the warn was emitted for devtools visibility.
        // F5: kind is now included in the warn prefix ('score' for running writes).
        expect(warnSpy).toHaveBeenCalledWith(
            '[sync] queued score write rejected (409):',
            expect.objectContaining({ error: 'ineligible_competitor' })
        );
        warnSpy.mockRestore();
    });

    it('discards a queued write on a non-retryable 4xx (e.g. 400): never retried forever', async () => {
        const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});

        let attempt = 0;
        global.fetch = vi.fn().mockImplementation(() => {
            attempt++;
            // A 400 (validation / bind error) can never succeed on retry.
            return Promise.resolve({
                ok: false,
                status: 400,
                json: () => Promise.resolve({ error: 'bad request' }),
            });
        });

        enqueueRunningWrite('c1', 'm1', { status: 'running', rev: 1 }, 'pw');
        await flushMicrotasks();

        const callsAfterDiscard = attempt;
        // Advance well past every backoff step; no retry should ever fire.
        await tick(20000);
        expect(global.fetch).toHaveBeenCalledTimes(callsAfterDiscard);
        // F5: kind is now included in the warn prefix ('score' for running writes).
        expect(warnSpy).toHaveBeenCalledWith(
            '[sync] queued score write rejected (400):',
            expect.objectContaining({ error: 'bad request' })
        );
        warnSpy.mockRestore();
    });

    it('keeps a queued write on a transient 5xx and retries with backoff', async () => {
        let attempt = 0;
        global.fetch = vi.fn().mockImplementation(() => {
            attempt++;
            if (attempt === 1) {
                return Promise.resolve({ ok: false, status: 503, json: () => Promise.resolve({}) });
            }
            return Promise.resolve({ ok: true, json: () => Promise.resolve({}) });
        });

        enqueueRunningWrite('c1', 'm1', { status: 'running', rev: 1 }, 'pw');
        await flushMicrotasks(); // first attempt 503

        // Advance past the 500ms backoff; the retry should succeed.
        await tick(600);
        expect(attempt).toBeGreaterThanOrEqual(2);
    });

    it('keeps a queued write on network error and schedules a retry', async () => {
        let attempt = 0;
        global.fetch = vi.fn().mockImplementation(() => {
            attempt++;
            if (attempt === 1) return Promise.reject(new TypeError('network error'));
            return Promise.resolve({ ok: true, json: () => Promise.resolve({}) });
        });

        enqueueRunningWrite('c1', 'm1', { status: 'running', rev: 1 }, 'pw');
        await flushMicrotasks(); // first attempt fails

        // Advance past the 500ms backoff; second attempt should succeed.
        await tick(600);

        expect(attempt).toBeGreaterThanOrEqual(2);
    });
});

// 3b. Single-in-flight serialization: overlapping triggers must not start a
//     second concurrent flush loop or duplicate PUTs.

describe('_flushQueue: single-in-flight serialization', () => {
    it('a trigger during an in-flight flush does not overlap or duplicate PUTs', async () => {
        // Each fetch returns a promise we resolve manually, so we can hold a
        // flush "in flight" while we fire a second trigger.
        const resolvers = [];
        global.fetch = vi.fn().mockImplementation(() =>
            new Promise((resolve) => {
                resolvers.push(() => resolve({ ok: true, json: () => Promise.resolve({}) }));
            }),
        );

        // First enqueue starts a flush; its fetch is now pending (in flight).
        enqueueRunningWrite('c1', 'm1', { status: 'running', rev: 1 }, 'pw');
        await flushMicrotasks();
        expect(global.fetch).toHaveBeenCalledTimes(1); // one PUT in flight

        // A second enqueue for the SAME match while the flush is in flight must
        // NOT start an overlapping loop; no duplicate PUT yet. It replaces the
        // queued descriptor (last-write-wins, rev:2) and sets the rerun flag.
        enqueueRunningWrite('c1', 'm1', { status: 'running', rev: 2 }, 'pw');
        await flushMicrotasks();
        expect(global.fetch).toHaveBeenCalledTimes(1); // still one; no overlap

        // Resolve the in-flight (rev:1) fetch. The identity check leaves the
        // newer rev:2 descriptor in the queue; the loop reruns once and issues
        // exactly one more PUT for it.
        resolvers.shift()();
        await flushMicrotasks();
        expect(global.fetch).toHaveBeenCalledTimes(2); // rerun for the newer write

        // Resolve the rerun's fetch; the queue drains and no further PUTs fire.
        resolvers.shift()();
        await flushMicrotasks();
        await tick(0);
        expect(global.fetch).toHaveBeenCalledTimes(2);
    });
});

// 4. subscribeSyncStatus: state transitions

describe('subscribeSyncStatus: state transitions', () => {
    it('replays current status to new subscriber', () => {
        const received = [];
        subscribeSyncStatus((s) => received.push(s));
        // Before any queue activity the status is 'synced'.
        expect(received).toEqual(['synced']);
    });

    it('transitions synced → syncing when a write is enqueued', async () => {
        const states = [];
        subscribeSyncStatus((s) => states.push(s));

        // Slow fetch so 'syncing' is observable during the in-flight request.
        let resolve;
        global.fetch = vi.fn().mockReturnValue(new Promise(r => { resolve = r; }));

        enqueueRunningWrite('c1', 'm1', { status: 'running', rev: 1 }, 'pw');
        await flushMicrotasks(); // flush starts

        expect(states).toContain('syncing');

        // Clean up; let the fetch resolve.
        resolve({ ok: true, json: () => Promise.resolve({}) });
        await flushMicrotasks();
    });

    it('transitions to offline when flush fails', async () => {
        const states = [];
        subscribeSyncStatus((s) => states.push(s));

        global.fetch = vi.fn().mockRejectedValue(new TypeError('network error'));

        enqueueRunningWrite('c1', 'm1', { status: 'running', rev: 1 }, 'pw');
        await flushMicrotasks(); // flush fails → offline

        expect(states).toContain('offline');
    });

    it('transitions back to synced after a successful flush', async () => {
        const states = [];
        let attempt = 0;
        global.fetch = vi.fn().mockImplementation(() => {
            attempt++;
            if (attempt === 1) return Promise.reject(new TypeError('network error'));
            return Promise.resolve({ ok: true, json: () => Promise.resolve({}) });
        });

        enqueueRunningWrite('c1', 'm1', { status: 'running', rev: 1 }, 'pw');
        await flushMicrotasks(); // first attempt fails → offline

        subscribeSyncStatus((s) => states.push(s));

        // Advance past the 500ms backoff; retry succeeds → synced.
        await tick(600);
        expect(states).toContain('synced');
    });

    it('unsubscribe stops receiving updates', async () => {
        const received = [];
        const unsub = subscribeSyncStatus((s) => received.push(s));
        unsub();
        const before = received.length;

        global.fetch = vi.fn().mockRejectedValue(new TypeError('network error'));
        enqueueRunningWrite('c1', 'm1', { status: 'running', rev: 1 }, 'pw');
        await flushMicrotasks();

        // After unsubscribing, no additional calls should have reached the callback.
        expect(received.length).toBe(before);
    });
});

// 5. window.online event flushes the queue

// mp-y3nk Phase 4: a feeder-winner assertion (override-winner) must survive an
// update outage exactly like a score does. Offline it is queued as a terminal
// write and replayed on reconnect (the server then propagates it into the
// dependent final). A real 4xx (e.g. the feeder is not yet playable) still throws
// so the operator sees the rejection instead of a silent no-op.
describe('overrideBracketWinner: offline durability (Phase 4)', () => {
    it('returns { queued: true } and enqueues on network failure', async () => {
        global.fetch = vi.fn().mockRejectedValue(new TypeError('network error'));
        const result = await API.overrideBracketWinner('c1', 'm-r2-0', 'Alice', 'pw');
        expect(result).toMatchObject({ queued: true });
    });

    it('replays the queued assertion to the override-winner endpoint on reconnect', async () => {
        global.fetch = vi.fn().mockRejectedValue(new TypeError('network error'));
        await API.overrideBracketWinner('c1', 'm-r2-0', 'Alice', 'pw'); // queued offline
        await flushMicrotasks();

        // Reconnect: fetch now succeeds; the online event drives the flush.
        global.fetch = vi.fn().mockResolvedValue({ ok: true, status: 200, json: () => Promise.resolve({}) });
        window.dispatchEvent(new Event('online'));
        await tick(50);

        const replay = global.fetch.mock.calls.find(([u]) => String(u).includes('/matches/m-r2-0/override-winner'));
        expect(replay).toBeTruthy();
        expect(replay[1].method).toBe('PUT');
        expect(JSON.parse(replay[1].body)).toMatchObject({ winnerName: 'Alice' });
    });

    it('throws on a non-retryable 4xx (feeder not ready to override) so the operator sees it', async () => {
        global.fetch = vi.fn().mockResolvedValue({
            ok: false, status: 400,
            json: () => Promise.resolve({ error: 'knockout match not ready to override' }),
        });
        await expect(
            API.overrideBracketWinner('c1', 'm-r2-0', 'Alice', 'pw')
        ).rejects.toThrow('not ready to override');
    });
});

// mp-y3nk Phase C: writes carry a server-relative modifiedAt so the backend can
// reconcile a reconnecting offline court's changes by timestamp (last-write-wins
// on conflict). The offset defaults to 0 in tests (no /api/time learned), so the
// stamp is ~local time: the contract under test is only that the field is present
// and numeric.
describe('client stamping: score/override writes carry a numeric modifiedAt', () => {
    it('overrideBracketWinner body includes winnerName and a numeric modifiedAt', async () => {
        let sentBody = null;
        global.fetch = vi.fn().mockImplementation((url, opts) => {
            if (String(url).includes('/override-winner')) sentBody = JSON.parse(opts.body);
            return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({}) });
        });
        await API.overrideBracketWinner('c1', 'm-r2-0', 'Alice', 'pw');
        expect(sentBody).toMatchObject({ winnerName: 'Alice' });
        expect(typeof sentBody.modifiedAt).toBe('number');
    });

    it('recordScore payload includes a numeric modifiedAt', async () => {
        let sentBody = null;
        global.fetch = vi.fn().mockImplementation((url, opts) => {
            if (String(url).includes('/matches/') && String(url).includes('/score')) sentBody = JSON.parse(opts.body);
            return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({}) });
        });
        await API.recordScore('c1', 'm1', { status: 'completed' }, 'pw', null);
        expect(sentBody).not.toBeNull();
        expect(typeof sentBody.modifiedAt).toBe('number');
    });
});

describe('window.online event flushes the queue', () => {
    it('triggers a flush when the queue is non-empty', async () => {
        // Put something in the queue (first attempt fails).
        global.fetch = vi.fn().mockRejectedValue(new TypeError('network error'));
        enqueueRunningWrite('c1', 'm1', { status: 'running', rev: 1 }, 'pw');
        await flushMicrotasks(); // first attempt fails → queue retained, status = offline

        // At this point the status is 'offline' and the queue has one entry.
        // Verify the queue is flushed after the 'online' event fires,
        // regardless of how many total attempts were made (backoff timers may
        // also fire). The important invariant is that the queue is drained.
        const statuses = [];
        subscribeSyncStatus((s) => statuses.push(s));

        // Replace fetch with a success responder.
        global.fetch = vi.fn().mockImplementation(() =>
            Promise.resolve({ ok: true, json: () => Promise.resolve({}) })
        );

        // Fire the 'online' event; api_client registers this listener at load.
        window.dispatchEvent(new Event('online'));
        await flushMicrotasks();

        // The queue should be empty and the status should be 'synced' again.
        expect(statuses).toContain('synced');
        expect(global.fetch).toHaveBeenCalled();
    });
});

// 6. recordScore: running writes queue on network failure

describe('recordScore: queues running writes on network failure', () => {
    it('returns { queued: true } and enqueues on network error for running status', async () => {
        global.fetch = vi.fn().mockRejectedValue(new TypeError('network error'));

        const result = await API.recordScore('c1', 'm1', { status: 'running' }, 'pw', null);
        expect(result).toMatchObject({ queued: true });
    });

    it('queues completed writes on network failure (F5: terminal durability)', async () => {
        // F5: completed writes are now queued for durable re-delivery on transient
        // failures (network error, abort, 5xx, 429) instead of throwing. The caller
        // receives { queued: true } so the UI can show a "not yet saved" state.
        // 4xx errors on the direct call still throw (no retry possible).
        global.fetch = vi.fn().mockRejectedValue(new TypeError('network error'));

        const result = await API.recordScore('c1', 'm1', { status: 'completed' }, 'pw', null);
        expect(result).toMatchObject({ queued: true });
    });

    it('throws on 409 for running writes (real operator errors must not be swallowed)', async () => {
        // Finding #2: the old 409-swallow for running writes is removed. A real
        // 409 (ineligible_competitor, court_busy, side_mismatch, result_finalized)
        // must propagate as a thrown error so the UI can surface it to the operator.
        // The stale-rev signal from the server is HTTP 200 with {stale:true}, not 409.
        global.fetch = vi.fn().mockResolvedValue({
            ok: false,
            status: 409,
            json: () => Promise.resolve({ error: 'ineligible_competitor', reasonHuman: 'Already fighting in match X' }),
        });

        await expect(
            API.recordScore('c1', 'm1', { status: 'running' }, 'pw', null)
        ).rejects.toThrow('Already fighting in match X');
    });

    it('queues a running 5xx as "syncing" (server up, not "offline" or falsely "synced")', async () => {
        // A 5xx (or 429) for a running write is transient: queue it for retry so
        // the sync pill reflects the unsynced state rather than flipping back to
        // "Synced" (the autosave caller swallows throws). And because the network
        // is UP (the server responded), the pill must be "syncing", never
        // "offline". Non-retryable 4xx still throw; they won't succeed on retry.
        let status = 'synced';
        const unsub = subscribeSyncStatus((s) => { status = s; });
        global.fetch = vi.fn().mockResolvedValue({
            ok: false,
            status: 503,
            json: () => Promise.resolve({ error: 'temporarily unavailable' }),
        });

        const result = await API.recordScore('c1', 'm1', { status: 'running' }, 'pw', null);
        await flushMicrotasks(); // let the queued flush attempt run
        expect(result).toMatchObject({ queued: true });
        expect(status).toBe('syncing'); // server up but erroring; not offline, not synced
        unsub();
    });

    it('queues a running write on 429 (rate limited)', async () => {
        global.fetch = vi.fn().mockResolvedValue({
            ok: false,
            status: 429,
            json: () => Promise.resolve({ error: 'rate limited' }),
        });

        const result = await API.recordScore('c1', 'm1', { status: 'running' }, 'pw', null);
        expect(result).toMatchObject({ queued: true });
    });
});

// 7. Completed recordScore drains a previously-queued running write

describe('recordScore: completed write drains queued running autosave', () => {
    it('a queued running write is removed when a completed write for the same match succeeds', async () => {
        // Step 1: enqueue a running write by failing the network call.
        let callCount = 0;
        global.fetch = vi.fn().mockImplementation((_url, opts) => {
            callCount++;
            const body = JSON.parse(opts.body);
            if (body.status === 'running' && callCount === 1) {
                // First running write fails (network down) → queued.
                return Promise.reject(new TypeError('network error'));
            }
            // Completed write (and any subsequent flush attempt) succeeds.
            return Promise.resolve({ ok: true, json: () => Promise.resolve({}) });
        });

        // Trigger the queued running write.
        const runningResult = await API.recordScore('c1', 'm_drain', { status: 'running' }, 'pw', null);
        expect(runningResult).toMatchObject({ queued: true });

        // The queue should contain an entry for this match.
        // (We can't inspect _writeQueue directly, but the sync status will be
        // 'syncing' or 'offline', not 'synced', while the entry is in the queue.)
        const statuses = [];
        const unsub = subscribeSyncStatus((s) => statuses.push(s));
        // Drain microtasks so the backoff state is established.
        await flushMicrotasks();
        unsub();

        // Step 2: a completed write for the same match should drain the queue.
        await API.recordScore('c1', 'm_drain', { status: 'completed', winner: 'Alice' }, 'pw', null);
        await flushMicrotasks();

        // After the completed write, the queue should be empty and status synced.
        const finalStatuses = [];
        const unsub2 = subscribeSyncStatus((s) => finalStatuses.push(s));
        await flushMicrotasks();
        unsub2();
        expect(finalStatuses).toContain('synced');
    });

    it('a FAILED completed write is queued as terminal (supersedes running snapshot)', async () => {
        // F5: a completed write that fails offline is now queued as a terminal
        // entry. It supersedes the running entry for the same key (last-write-wins,
        // terminal takes priority) so only the terminal completed write is flushed
        // when connectivity returns; the stale running snapshot is not re-sent.
        // The drain of the running entry still happens only after a server-confirmed
        // success (not pre-flight), but here the terminal enqueue replaces it.
        let online = false;
        const delivered = [];
        global.fetch = vi.fn().mockImplementation((_url, opts) => {
            const body = JSON.parse(opts.body);
            if (!online) {
                return Promise.reject(new TypeError('network error'));
            }
            delivered.push(body.status);
            return Promise.resolve({ ok: true, json: () => Promise.resolve({}) });
        });

        // Step 1: running write fails → queued as running entry.
        const runQueued = await API.recordScore('c1', 'm_keep', { status: 'running', ipponsA: ['M'] }, 'pw', null);
        expect(runQueued).toMatchObject({ queued: true });

        // Step 2: completed write while offline → queued as terminal (not a throw).
        // The terminal entry supersedes the running entry for the same key.
        const completedQueued = await API.recordScore('c1', 'm_keep', { status: 'completed', winner: 'Alice' }, 'pw', null);
        expect(completedQueued).toMatchObject({ queued: true });

        // Step 3: connection returns; only the terminal completed write flushes.
        online = true;
        await tick(600);
        expect(delivered).toContain('completed');
    });
});

// mp-gpra review fixes: clearQueue (security), rehydrate url validation
// (security), hasPendingTerminalWrite contract (banner re-hydrate).

describe('clearQueue: drops queued writes + persisted store (logout / password_reset)', () => {
    it('empties the queue, removes bc_write_queue, and resets sync status to synced', async () => {
        global.fetch = vi.fn().mockRejectedValue(new TypeError('offline'));
        const queued = await API.recordScore('c1', 'm1', { status: 'completed', winner: 'A' }, 'pw', null);
        expect(queued).toMatchObject({ queued: true });
        expect(API.hasPendingTerminalWrite('c1', 'm1')).toBe(true);
        expect(localStorage.getItem('bc_write_queue')).not.toBeNull();

        let status;
        const unsub = subscribeSyncStatus((s) => { status = s; });
        API.clearQueue();
        unsub();

        expect(API.hasPendingTerminalWrite('c1', 'm1')).toBe(false);
        expect(localStorage.getItem('bc_write_queue')).toBeNull();
        expect(status).toBe('synced');
    });
});

describe('rehydrate: tampered terminal url from localStorage is rejected (security)', () => {
    it('drops a terminal entry whose url is not a same-origin /api/ path', async () => {
        const evil = [['c1:m1', {
            compID: 'c1', matchID: 'm1', payload: {}, password: 'pw',
            kind: 'decision', terminal: true, method: 'POST',
            url: 'https://evil.example/steal', enqueuedAt: Date.now(),
        }]];
        localStorage.setItem('bc_write_queue', JSON.stringify(evil));
        global.fetch = vi.fn().mockRejectedValue(new TypeError('offline'));
        vi.resetModules();
        const m = await import('../api_client.jsx');
        expect(m.API.hasPendingTerminalWrite('c1', 'm1')).toBe(false);
    });

    it('keeps a terminal entry with a valid /api/ url', async () => {
        const ok = [['c1:m2', {
            compID: 'c1', matchID: 'm2', payload: {}, password: 'pw',
            kind: 'decision', terminal: true, method: 'POST',
            url: '/api/competitions/c1/matches/m2/decision', enqueuedAt: Date.now(),
        }]];
        localStorage.setItem('bc_write_queue', JSON.stringify(ok));
        global.fetch = vi.fn().mockRejectedValue(new TypeError('offline'));
        vi.resetModules();
        const m = await import('../api_client.jsx');
        expect(m.API.hasPendingTerminalWrite('c1', 'm2')).toBe(true);
    });

    it('rejects an /api/ url OUTSIDE /api/competitions/ (allowlist is route-scoped)', async () => {
        const bad = [['c1:m3', {
            compID: 'c1', matchID: 'm3', payload: {}, password: 'pw',
            kind: 'decision', terminal: true, method: 'POST',
            url: '/api/admin/secrets', enqueuedAt: Date.now(),
        }]];
        localStorage.setItem('bc_write_queue', JSON.stringify(bad));
        global.fetch = vi.fn().mockRejectedValue(new TypeError('offline'));
        vi.resetModules();
        const m = await import('../api_client.jsx');
        expect(m.API.hasPendingTerminalWrite('c1', 'm3')).toBe(false);
    });

    it('rejects a disallowed method (DELETE) even on an /api/competitions/ url', async () => {
        const bad = [['c1:m4', {
            compID: 'c1', matchID: 'm4', payload: {}, password: 'pw',
            kind: 'decision', terminal: true, method: 'DELETE',
            url: '/api/competitions/c1/matches/m4/decision', enqueuedAt: Date.now(),
        }]];
        localStorage.setItem('bc_write_queue', JSON.stringify(bad));
        global.fetch = vi.fn().mockRejectedValue(new TypeError('offline'));
        vi.resetModules();
        const m = await import('../api_client.jsx');
        expect(m.API.hasPendingTerminalWrite('c1', 'm4')).toBe(false);
    });

    it('rejects a dot-segment path-traversal url that normalizes outside /api/competitions/', async () => {
        // Passes a naive startsWith('/api/competitions/') but fetch would normalize
        // it to /api/admin/secrets on the wire; must be rejected.
        const bad = [['c1:m5', {
            compID: 'c1', matchID: 'm5', payload: {}, password: 'pw',
            kind: 'score', terminal: true, method: 'PUT',
            url: '/api/competitions/../../api/admin/secrets', enqueuedAt: Date.now(),
        }]];
        localStorage.setItem('bc_write_queue', JSON.stringify(bad));
        global.fetch = vi.fn().mockRejectedValue(new TypeError('offline'));
        vi.resetModules();
        const m = await import('../api_client.jsx');
        expect(m.API.hasPendingTerminalWrite('c1', 'm5')).toBe(false);
    });

    it('rejects a cross-origin / protocol-relative terminal url', async () => {
        const bad = [['c1:m6', {
            compID: 'c1', matchID: 'm6', payload: {}, password: 'pw',
            kind: 'score', terminal: true, method: 'PUT',
            url: '//evil.example.com/api/competitions/c1/matches/m6/score', enqueuedAt: Date.now(),
        }]];
        localStorage.setItem('bc_write_queue', JSON.stringify(bad));
        global.fetch = vi.fn().mockRejectedValue(new TypeError('offline'));
        vi.resetModules();
        const m = await import('../api_client.jsx');
        expect(m.API.hasPendingTerminalWrite('c1', 'm6')).toBe(false);
    });

    it('accepts a legitimate same-origin /api/competitions/ terminal url', async () => {
        const good = [['c1:m7', {
            compID: 'c1', matchID: 'm7', payload: {}, password: 'pw',
            kind: 'score', terminal: true, method: 'PUT',
            url: '/api/competitions/c1/matches/m7/score', enqueuedAt: Date.now(),
        }]];
        localStorage.setItem('bc_write_queue', JSON.stringify(good));
        global.fetch = vi.fn().mockRejectedValue(new TypeError('offline'));
        vi.resetModules();
        const m = await import('../api_client.jsx');
        expect(m.API.hasPendingTerminalWrite('c1', 'm7')).toBe(true);
    });

    it('rejects an entry whose enqueuedAt is a non-numeric (tampered) value', async () => {
        // A string enqueuedAt would make (now - enqueuedAt) NaN, so a naive TTL
        // check (NaN > TTL === false) would let an arbitrarily old write through.
        const bad = [['c1:m8', {
            compID: 'c1', matchID: 'm8', payload: {}, password: 'pw',
            kind: 'score', terminal: true, method: 'PUT',
            url: '/api/competitions/c1/matches/m8/score', enqueuedAt: 'not-a-number',
        }]];
        localStorage.setItem('bc_write_queue', JSON.stringify(bad));
        global.fetch = vi.fn().mockRejectedValue(new TypeError('offline'));
        vi.resetModules();
        const m = await import('../api_client.jsx');
        expect(m.API.hasPendingTerminalWrite('c1', 'm8')).toBe(false);
    });

    it('skips malformed persisted entries without dropping valid ones (durability)', async () => {
        // Corrupt/tampered queue: a number, null, a 1-element array, then a VALID
        // terminal entry. A non-array element would throw on destructure and (under
        // one try/catch) abort the entire rehydrate; the valid entry must survive.
        const mixed = [
            42,
            null,
            ['c1:bad'],
            ['c1:good', {
                compID: 'c1', matchID: 'good', payload: {}, password: 'pw',
                kind: 'decision', terminal: true, method: 'POST',
                url: '/api/competitions/c1/matches/good/decision', enqueuedAt: Date.now(),
            }],
        ];
        localStorage.setItem('bc_write_queue', JSON.stringify(mixed));
        global.fetch = vi.fn().mockRejectedValue(new TypeError('offline'));
        vi.resetModules();
        const m = await import('../api_client.jsx');
        expect(m.API.hasPendingTerminalWrite('c1', 'good')).toBe(true);
    });
});

describe('hasPendingTerminalWrite: true only for terminal writes (banner re-hydrate contract)', () => {
    it('is false for a queued running write and true for a queued terminal write', async () => {
        global.fetch = vi.fn().mockRejectedValue(new TypeError('offline'));
        await API.recordScore('c1', 'mr', { status: 'running', ipponsA: ['M'] }, 'pw', null);
        expect(API.hasPendingTerminalWrite('c1', 'mr')).toBe(false);
        await API.recordScore('c1', 'mt', { status: 'completed', winner: 'A' }, 'pw', null);
        expect(API.hasPendingTerminalWrite('c1', 'mt')).toBe(true);
    });
});

describe('clearQueue: cancels an in-flight flush (no sends with a revoked password)', () => {
    it('does not send remaining queued writes after clearQueue() during a flush', async () => {
        // Enqueue two terminal writes while offline (direct call fails → queued).
        global.fetch = vi.fn().mockRejectedValue(new TypeError('offline'));
        await API.recordScore('c1', 'mA', { status: 'completed', winner: 'A' }, 'pw', null);
        await API.recordScore('c1', 'mB', { status: 'completed', winner: 'B' }, 'pw', null);
        await flushMicrotasks();

        // Controllable fetch: the FIRST flush request hangs until we resolve it,
        // so we can call clearQueue() while the loop is awaiting it.
        let resolveFirst;
        const flushFetch = vi.fn().mockImplementation(() => {
            if (flushFetch.mock.calls.length === 1) {
                return new Promise((res) => { resolveFirst = () => res({ ok: true, json: () => Promise.resolve({}) }); });
            }
            return Promise.resolve({ ok: true, json: () => Promise.resolve({}) });
        });
        global.fetch = flushFetch;

        // Trigger a fresh flush; the loop sends entry 1 and parks on resolveFirst.
        window.dispatchEvent(new Event('online'));
        await flushMicrotasks();
        expect(flushFetch).toHaveBeenCalledTimes(1);

        // Credential revocation mid-flight, then let the in-flight request finish.
        API.clearQueue();
        resolveFirst();
        await flushMicrotasks();

        // The second queued write must NOT have been sent (gen-guard aborted the loop).
        expect(flushFetch).toHaveBeenCalledTimes(1);
        expect(API.hasPendingTerminalWrite('c1', 'mA')).toBe(false);
        expect(API.hasPendingTerminalWrite('c1', 'mB')).toBe(false);
        // The in-flight flush's post-loop _persistQueue() must NOT re-create an
        // empty bc_write_queue after clearQueue() removed it (credential revocation).
        expect(localStorage.getItem('bc_write_queue')).toBeNull();
    });
});

describe('subscribeTerminalWriteFailed: permanent terminal-write rejection is surfaced (no silent drop)', () => {
    it('fires on a non-retryable 4xx for a queued terminal write', async () => {
        // The discard path console.warns for devtools; the strict test setup fails
        // on unexpected warns, so suppress it here (it's expected).
        const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
        // Enqueue a terminal write while offline.
        global.fetch = vi.fn().mockRejectedValue(new TypeError('offline'));
        await API.recordScore('c1', 'mfail', { status: 'completed', winner: 'A' }, 'pw', null);
        expect(API.hasPendingTerminalWrite('c1', 'mfail')).toBe(true);

        // On flush the server returns a non-retryable 4xx (e.g. 409 conflict).
        const failures = [];
        const unsub = mod.subscribeTerminalWriteFailed((info) => failures.push(info));
        global.fetch = vi.fn().mockResolvedValue({
            ok: false, status: 409,
            json: () => Promise.resolve({ error: 'conflict', reasonHuman: 'Match already finished' }),
        });
        window.dispatchEvent(new Event('online'));
        await tick(50);
        unsub();
        warnSpy.mockRestore();

        // The failure was surfaced (not silently dropped) and the entry discarded.
        expect(failures.length).toBeGreaterThanOrEqual(1);
        expect(failures[0]).toMatchObject({ compID: 'c1', matchID: 'mfail', status: 409 });
        expect(failures[0].reason).toContain('Match already finished');
        expect(API.hasPendingTerminalWrite('c1', 'mfail')).toBe(false);
    });
});
