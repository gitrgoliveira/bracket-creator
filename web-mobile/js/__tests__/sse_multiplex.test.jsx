// Tests for the shared ref-counted SSE singleton in api_client.jsx.
// Each test resets module state (vi.resetModules) so the singleton starts
// fresh — this is required because the module-level variables (_sharedSource,
// _subscribers, etc.) are reset only by re-importing the module.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

// ---------------------------------------------------------------------------
// Fake EventSource
// ---------------------------------------------------------------------------

class FakeEventSource {
    static CONNECTING = 0;
    static OPEN = 1;
    static CLOSED = 2;

    constructor(url) {
        this.url = url;
        this.readyState = FakeEventSource.CONNECTING;
        this.onopen = null;
        this.onmessage = null;
        this.onerror = null;
        FakeEventSource.instances.push(this);
    }

    /** Simulate a successful connection. */
    simulateOpen() {
        this.readyState = FakeEventSource.OPEN;
        if (typeof this.onopen === 'function') this.onopen();
    }

    /** Simulate receiving a server-sent event. */
    simulateMessage(data) {
        if (typeof this.onmessage === 'function') {
            this.onmessage({ data: typeof data === 'string' ? data : JSON.stringify(data) });
        }
    }

    /** Simulate a connection error / close from server side. */
    simulateError() {
        this.readyState = FakeEventSource.CLOSED;
        if (typeof this.onerror === 'function') this.onerror();
    }

    close() {
        this.readyState = FakeEventSource.CLOSED;
        FakeEventSource.closedCount++;
    }

    static reset() {
        FakeEventSource.instances = [];
        FakeEventSource.closedCount = 0;
    }
}
FakeEventSource.instances = [];
FakeEventSource.closedCount = 0;

// ---------------------------------------------------------------------------
// Setup / teardown
// ---------------------------------------------------------------------------

let API;
let _origEventSource;

beforeEach(async () => {
    FakeEventSource.reset();
    vi.useFakeTimers();
    // Provide FakeEventSource as the global EventSource before importing the
    // module so the module-level code can reference it via EventSource.OPEN etc.
    // Capture the original so afterEach can restore it (avoid leaking the fake
    // into unrelated test files sharing this environment).
    _origEventSource = global.EventSource;
    global.EventSource = FakeEventSource;
    vi.resetModules();
    const mod = await import('../api_client.jsx');
    API = mod.API;
});

afterEach(() => {
    vi.useRealTimers();
    vi.resetModules();
    if (_origEventSource === undefined) {
        delete global.EventSource;
    } else {
        global.EventSource = _origEventSource;
    }
});

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('subscribeToEvents — shared singleton', () => {
    it('(a) N subscribers open exactly ONE EventSource', () => {
        const unsub1 = API.subscribeToEvents(() => {});
        const unsub2 = API.subscribeToEvents(() => {});
        const unsub3 = API.subscribeToEvents(() => {});

        expect(FakeEventSource.instances).toHaveLength(1);

        unsub1(); unsub2(); unsub3();
    });

    it('(b) a message fans out to all callbacks', () => {
        const cb1 = vi.fn();
        const cb2 = vi.fn();
        const cb3 = vi.fn();

        const unsub1 = API.subscribeToEvents(cb1);
        const unsub2 = API.subscribeToEvents(cb2);
        const unsub3 = API.subscribeToEvents(cb3);

        const src = FakeEventSource.instances[0];
        src.simulateOpen();
        src.simulateMessage({ type: 'match_updated', id: 42 });

        expect(cb1).toHaveBeenCalledOnce();
        expect(cb1).toHaveBeenCalledWith({ type: 'match_updated', id: 42 });
        expect(cb2).toHaveBeenCalledWith({ type: 'match_updated', id: 42 });
        expect(cb3).toHaveBeenCalledWith({ type: 'match_updated', id: 42 });

        unsub1(); unsub2(); unsub3();
    });

    it('(c) late subscriber to OPEN source gets immediate onStatus("open")', () => {
        // First subscriber opens and connects the source.
        const unsub1 = API.subscribeToEvents(() => {});
        const src = FakeEventSource.instances[0];
        src.simulateOpen();

        // Second subscriber arrives after the source is already OPEN.
        const status2 = vi.fn();
        const unsub2 = API.subscribeToEvents(() => {}, status2);

        expect(status2).toHaveBeenCalledWith('open');

        unsub1(); unsub2();
    });

    it('(c) late subscriber while source is CONNECTING gets NO synchronous status, then "open" on connect', () => {
        // First subscriber opens the source but it has NOT connected yet
        // (readyState === CONNECTING) — mirrors an admin tab where app.jsx
        // subscribes first and AdminTopbar/AdminDashboard mount mid-handshake.
        const unsub1 = API.subscribeToEvents(() => {});
        const src = FakeEventSource.instances[0];
        expect(src.readyState).toBe(FakeEventSource.CONNECTING);

        // Second subscriber joins during the handshake. It must NOT be handed a
        // synchronous 'error' (that would flash a false "Reconnecting…").
        const status2 = vi.fn();
        const unsub2 = API.subscribeToEvents(() => {}, status2);
        expect(status2).not.toHaveBeenCalled();
        // No second EventSource — it multiplexed onto the existing one.
        expect(FakeEventSource.instances).toHaveLength(1);

        // When the shared source connects, the late subscriber gets the real status.
        src.simulateOpen();
        expect(status2).toHaveBeenCalledWith('open');
        expect(status2).not.toHaveBeenCalledWith('error');

        unsub1(); unsub2();
    });

    it('(c) late subscriber after an error opens a fresh source and is NOT replayed a status', () => {
        // First subscriber opens, then an error fires.
        const status1 = vi.fn();
        const unsub1 = API.subscribeToEvents(() => {}, status1);
        const src = FakeEventSource.instances[0];
        src.simulateOpen();
        expect(status1).toHaveBeenCalledWith('open');

        // Simulate error — source closes, retryTimer is scheduled since unsub1 is still live.
        src.simulateError();
        expect(status1).toHaveBeenCalledWith('error');
        // Cancel the retry so a new source isn't opened before our late subscriber.
        vi.clearAllTimers();

        // Late subscriber: no shared source exists right now (it was nulled on
        // error). subscribeToEvents only replays a status when a source ALREADY
        // exists; here it opens a fresh source (CONNECTING) and waits for that
        // source's onopen/onerror — so status2 is NOT called synchronously.
        const status2 = vi.fn();
        const unsub2 = API.subscribeToEvents(() => {}, status2);

        expect(status2).not.toHaveBeenCalled();

        // Clean up the retry timers.
        vi.clearAllTimers();
        unsub1(); unsub2();
    });

    it('(d) unsubscribing all closes the source; a fresh subscribe reopens it', () => {
        const unsub1 = API.subscribeToEvents(() => {});
        const unsub2 = API.subscribeToEvents(() => {});

        const src1 = FakeEventSource.instances[0];
        src1.simulateOpen();

        // Remove all subscribers — should close the source immediately.
        unsub1();
        unsub2();

        expect(src1.readyState).toBe(FakeEventSource.CLOSED);
        expect(FakeEventSource.closedCount).toBe(1);

        // A new subscribe should open a brand-new EventSource.
        const unsub3 = API.subscribeToEvents(() => {});
        expect(FakeEventSource.instances).toHaveLength(2);

        unsub3();
    });

    it('(e) idempotent unsubscribe: calling twice decrements only once', () => {
        const unsub1 = API.subscribeToEvents(() => {});
        const unsub2 = API.subscribeToEvents(() => {});

        const src = FakeEventSource.instances[0];
        src.simulateOpen();

        // Call unsub1 twice.
        unsub1();
        unsub1(); // must be a no-op

        // Source should still be open (unsub2 still active).
        expect(src.readyState).toBe(FakeEventSource.OPEN);

        unsub2();
        // Now the source is closed (last subscriber left).
        expect(src.readyState).toBe(FakeEventSource.CLOSED);
    });

    it('(f) one throwing callback does not block delivery to the others', () => {
        const cb1 = vi.fn(() => { throw new Error('boom'); });
        const cb2 = vi.fn();

        const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {});

        const unsub1 = API.subscribeToEvents(cb1);
        const unsub2 = API.subscribeToEvents(cb2);

        const src = FakeEventSource.instances[0];
        src.simulateOpen();
        src.simulateMessage({ type: 'test' });

        // cb1 threw but cb2 still received the message.
        expect(cb1).toHaveBeenCalledOnce();
        expect(cb2).toHaveBeenCalledWith({ type: 'test' });
        expect(consoleError).toHaveBeenCalled();

        consoleError.mockRestore();
        unsub1(); unsub2();
    });

    it('(f) one throwing onStatus does not block other onStatus callbacks', () => {
        const status1 = vi.fn(() => { throw new Error('boom'); });
        const status2 = vi.fn();

        const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {});

        const unsub1 = API.subscribeToEvents(() => {}, status1);
        const unsub2 = API.subscribeToEvents(() => {}, status2);

        const src = FakeEventSource.instances[0];
        src.simulateOpen();

        // status1 threw but status2 still received 'open'.
        expect(status1).toHaveBeenCalledWith('open');
        expect(status2).toHaveBeenCalledWith('open');
        expect(consoleError).toHaveBeenCalled();

        consoleError.mockRestore();
        unsub1(); unsub2();
    });

    it('retry timer is scheduled after onerror with active subscribers', () => {
        const unsub1 = API.subscribeToEvents(() => {});
        const src = FakeEventSource.instances[0];
        src.simulateOpen();
        src.simulateError();

        // Timer should be scheduled — advance it and check a new source opens.
        expect(FakeEventSource.instances).toHaveLength(1);
        vi.advanceTimersByTime(5000);
        expect(FakeEventSource.instances).toHaveLength(2);

        vi.clearAllTimers();
        unsub1();
    });

    it('retry timer is NOT scheduled after onerror when no subscribers remain', () => {
        const unsub1 = API.subscribeToEvents(() => {});
        const src = FakeEventSource.instances[0];
        src.simulateOpen();
        unsub1(); // last subscriber leaves before error fires

        // Manually trigger onerror on the (now-orphaned) source to confirm no timer.
        // We have to reach inside since unsub already cleared things.
        // Simulate error directly — should not schedule a reconnect.
        src.simulateError();

        vi.advanceTimersByTime(10000);
        // No new sources should have been opened.
        expect(FakeEventSource.instances).toHaveLength(1);
    });

    it('unsubscribing all clears the retry timer (no zombie reconnect)', () => {
        const unsub1 = API.subscribeToEvents(() => {});
        const src = FakeEventSource.instances[0];
        src.simulateOpen();
        src.simulateError(); // schedules retry timer

        // Now remove last subscriber before timer fires.
        unsub1();

        vi.advanceTimersByTime(5000);
        // Timer should have been cleared — no new source.
        expect(FakeEventSource.instances).toHaveLength(1);
    });

    it('a new subscribe during the retry window cancels the pending timer (no double-connect)', () => {
        // Regression for the leak Copilot flagged on PR #268: after onerror
        // schedules the 5s reconnect, a fresh subscribe reconnects immediately;
        // the stale timer must NOT later open a SECOND EventSource.
        const unsub1 = API.subscribeToEvents(() => {});
        const src = FakeEventSource.instances[0];
        src.simulateOpen();
        src.simulateError(); // source nulled, retry timer armed for 5s
        expect(FakeEventSource.instances).toHaveLength(1);

        // New subscriber arrives inside the retry window → opens a fresh source now.
        const unsub2 = API.subscribeToEvents(() => {});
        expect(FakeEventSource.instances).toHaveLength(2);

        // Advance past the original 5s retry — it must have been cancelled, so
        // NO third source is created (pre-fix this would be length 3 and leak).
        vi.advanceTimersByTime(5000);
        expect(FakeEventSource.instances).toHaveLength(2);

        vi.clearAllTimers();
        unsub1(); unsub2();
    });
});
