import { afterEach, describe, expect, it, vi } from "vitest";
import {
    startDownloadPoll,
    stopActivePoll,
    getActivePoll
} from "../js/download_polling.js";

// Drains chained .then/.catch microtasks queued by the polling helper.
// `await tick()` alone only awaits tick's return value (undefined); the
// promise body schedules 2-3 microtask hops, so we wait a few extra rounds.
async function flushMicrotasks() {
    for (let i = 0; i < 8; i++) {
        await Promise.resolve();
    }
}

// Fake timer harness that records every set/clear so we can assert the
// polling helper cleans up properly across rapid successive submits.
function makeTimers() {
    let nextId = 1;
    const intervals = new Map();
    const timeouts = new Map();
    return {
        intervals,
        timeouts,
        setInterval(fn) {
            const id = nextId++;
            intervals.set(id, fn);
            return id;
        },
        clearInterval(id) {
            intervals.delete(id);
        },
        setTimeout(fn) {
            const id = nextId++;
            timeouts.set(id, fn);
            return id;
        },
        clearTimeout(id) {
            timeouts.delete(id);
        },
    };
}

describe("startDownloadPoll", () => {
    afterEach(() => {
        // Defensive — don't leak across tests.
        stopActivePoll();
    });

    it("rejects when fetchStatus is missing", () => {
        expect(() => startDownloadPoll("tok", {})).toThrow();
    });

    it("calling twice cancels the previous interval and timeout (no overlap)", () => {
        const timers = makeTimers();
        const fetchStatus = vi.fn(() => Promise.resolve({ ready: false }));

        startDownloadPoll("first-token", { fetchStatus, timers });
        expect(timers.intervals.size).toBe(1);
        expect(timers.timeouts.size).toBe(1);
        const firstPoll = getActivePoll();

        startDownloadPoll("second-token", { fetchStatus, timers });
        // Exactly one active interval + timeout — the first pair was cleared.
        expect(timers.intervals.size).toBe(1);
        expect(timers.timeouts.size).toBe(1);
        const secondPoll = getActivePoll();
        expect(secondPoll.interval).not.toBe(firstPoll.interval);
        expect(secondPoll.timeout).not.toBe(firstPoll.timeout);

        stopActivePoll(timers);
    });

    it("ready response fires onReady once and clears timers", async () => {
        const timers = makeTimers();
        const onReady = vi.fn();
        const onError = vi.fn();
        const onTimeout = vi.fn();
        let calls = 0;
        const fetchStatus = vi.fn(() => {
            calls += 1;
            return Promise.resolve({ ready: calls >= 2 });
        });

        startDownloadPoll("tok", { fetchStatus, timers, onReady, onError, onTimeout });

        // Fire the interval body twice — second call resolves to ready.
        const tick = [...timers.intervals.values()][0];
        tick();
        await flushMicrotasks();
        expect(onReady).not.toHaveBeenCalled();
        tick();
        await flushMicrotasks();
        expect(onReady).toHaveBeenCalledTimes(1);
        // Both timer slots cleared on ready.
        expect(timers.intervals.size).toBe(0);
        expect(timers.timeouts.size).toBe(0);
        expect(onError).not.toHaveBeenCalled();
        expect(onTimeout).not.toHaveBeenCalled();
    });

    it("five consecutive failures fire onError and clear timers", async () => {
        const timers = makeTimers();
        const onReady = vi.fn();
        const onError = vi.fn();
        const onTimeout = vi.fn();
        const fetchStatus = vi.fn(() => Promise.reject(new Error("boom")));
        // Suppress console.error noise during this test.
        const errSpy = vi.spyOn(console, "error").mockImplementation(() => {});

        startDownloadPoll("tok", { fetchStatus, timers, onReady, onError, onTimeout });
        const tick = [...timers.intervals.values()][0];
        for (let i = 0; i < 5; i++) {
            tick();
            await flushMicrotasks();
        }
        expect(onError).toHaveBeenCalledTimes(1);
        expect(onReady).not.toHaveBeenCalled();
        expect(onTimeout).not.toHaveBeenCalled();
        expect(timers.intervals.size).toBe(0);
        expect(timers.timeouts.size).toBe(0);

        errSpy.mockRestore();
    });

    it("a successful poll between failures resets the error counter", async () => {
        const timers = makeTimers();
        const onError = vi.fn();
        let i = 0;
        const fetchStatus = vi.fn(() => {
            i += 1;
            // Fail, fail, fail, fail, succeed-but-not-ready, then more fails.
            if (i === 5) return Promise.resolve({ ready: false });
            return Promise.reject(new Error("boom"));
        });
        const errSpy = vi.spyOn(console, "error").mockImplementation(() => {});

        startDownloadPoll("tok", { fetchStatus, timers, onError });
        const tick = [...timers.intervals.values()][0];
        for (let k = 0; k < 8; k++) {
            tick();
            await flushMicrotasks();
        }
        // 8 ticks: 4 fail, 1 success, 3 fail. Streak reset at tick 5 → never hits 5 in a row.
        expect(onError).not.toHaveBeenCalled();

        stopActivePoll(timers);
        errSpy.mockRestore();
    });

    it("timeout callback fires when the timeout slot is triggered", () => {
        const timers = makeTimers();
        const onTimeout = vi.fn();
        const onReady = vi.fn();
        startDownloadPoll("tok", {
            fetchStatus: () => new Promise(() => {}),
            timers,
            onReady,
            onTimeout,
        });

        const timeoutFn = [...timers.timeouts.values()][0];
        timeoutFn();
        expect(onTimeout).toHaveBeenCalledTimes(1);
        expect(onReady).not.toHaveBeenCalled();
        expect(timers.intervals.size).toBe(0);
        expect(timers.timeouts.size).toBe(0);
    });
});
