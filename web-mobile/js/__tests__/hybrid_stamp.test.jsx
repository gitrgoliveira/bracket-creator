// bc-hlck (operator request 2026-10-03, raised on PR #453): a write is never
// stamped older than the result it was made against.
//
// Every match write carries modifiedAt, this device's estimate of the SERVER's
// time, and the server orders each thing a write changes by that stamp
// (bc-mrgc). Two devices' estimates can disagree by up to the accepted skew
// (5 s). A device whose estimate runs ahead stamps its write later than real
// time; a second device that SEES that write and changes it a moment later
// stamps its change EARLIER than the write it changed, so the server keeps the
// change in the history instead of applying it: the operator's correction is
// not applied, against the very result it corrected.
//
// The fix is a hybrid stamp: max(this device's time, the stamp of the match as
// the operator saw it + 1). Wall-clock order holds wherever it is right (an
// offline court's genuinely later action still wins); causality holds where
// the clocks disagree. For an autosave the match "as seen" is the one shown at
// the TAP, not at the send: a tap made before another device finished the
// match stays older than that finish (operator ruling 2026-09-27).
//
// Setup mirrors queue_ordered_writes.test.jsx.

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

const isClockPoll = (url) => String(url).includes('/api/time');

// A server that orders writes as the real one does for a thing two writes
// both change (engine/match_merge.go): the older stamp is not applied. It
// holds one match whose last change was stamped `storedStamp`, and refuses a
// stamp more than 5 s in its future (clock_skew, handlers_match.go).
function server(storedStamp, { serverNow = () => Date.now() } = {}) {
    const sent = [];
    let stored = storedStamp;
    global.fetch = vi.fn((url, opts) => {
        if (isClockPoll(url)) return Promise.reject(new TypeError('clock poll: not part of this test'));
        const body = JSON.parse(opts.body);
        sent.push({ url: String(url), body });
        let answer;
        if (body.modifiedAt - serverNow() > 5000) {
            answer = { applied: false, reason: 'clock_skew', serverNowMs: serverNow() };
        } else if (body.modifiedAt < stored) {
            answer = { applied: false, reason: 'superseded', heldGroups: ['points'] };
        } else {
            stored = body.modifiedAt;
            answer = { applied: true };
        }
        return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(answer) });
    });
    return { sent };
}

describe('a correction made against a write stamped ahead is applied', () => {
    // Device B's estimate of the server's time runs 3 s ahead (within the
    // accepted 5 s), so the point it recorded is stamped 3 s in the future.
    // Device A, whose estimate is right, sees that point a moment later and
    // corrects it. Before the fix A's correction was stamped older than the
    // point it corrected, and the server did not apply it.
    it('the correction is stamped after the point it corrected, and lands', async () => {
        const pointStamp = Date.now() + 3000;
        const { sent } = server(pointStamp);
        const shown = { id: 'm1', compId: 'c1', status: 'running', ipponsA: ['M'], modifiedAt: pointStamp };
        const res = await API.recordScore('c1', 'm1', { status: 'running', ipponsA: [] }, 'pw', shown);
        expect(sent).toHaveLength(1);
        expect(sent[0].body.modifiedAt).toBe(pointStamp + 1);
        expect(res && res.applied).not.toBe(false);
    });

    it('a device whose own time is already later stamps with its own time', async () => {
        const { sent } = server(0);
        const shown = { id: 'm1', compId: 'c1', status: 'running', modifiedAt: Date.now() - 60_000 };
        await API.recordScore('c1', 'm1', { status: 'running', ipponsA: ['K'] }, 'pw', shown);
        expect(sent[0].body.modifiedAt).toBe(Date.now());
    });

    it('a match never stamped sets no floor', async () => {
        const { sent } = server(0);
        await API.recordScore('c1', 'm1', { status: 'running', ipponsA: ['K'] }, 'pw', { id: 'm1', compId: 'c1', status: 'running' });
        expect(sent[0].body.modifiedAt).toBe(Date.now());
    });

    // The floor never carries a write past what the server accepts: a stamp
    // seen far ahead (a device whose clock was wrong by more than the server
    // allows, recorded before it was refused) is not chased past 4 s ahead.
    it('the floor stops 4 s ahead of this device\'s time, inside the 5 s the server accepts', async () => {
        const { sent } = server(0);
        const shown = { id: 'm1', compId: 'c1', status: 'running', modifiedAt: Date.now() + 60_000 };
        await API.recordScore('c1', 'm1', { status: 'running', ipponsA: ['K'] }, 'pw', shown);
        expect(sent[0].body.modifiedAt).toBe(Date.now() + 4000);
    });
});

describe('wall-clock order stands where it is right', () => {
    // An offline court saw the match at T0, then acted at T1 after going
    // offline; meanwhile another device changed the match at a time between.
    // The court's action was genuinely later, so its stamp is its own time
    // (never floored back, never pushed past what it did not see) and it wins
    // when it is sent. A pure logical clock would have it lose to everything
    // done while it was away.
    it('an offline court reconnecting with a genuinely later action still wins', async () => {
        const seenBeforeOffline = Date.now() - 120_000;
        global.fetch = vi.fn(() => Promise.reject(new TypeError('offline')));
        await API.recordScore('c1', 'm1', { status: 'completed', winner: 'A', ipponsA: ['M', 'M'] }, 'pw', { id: 'm1', compId: 'c1', status: 'running', modifiedAt: seenBeforeOffline });
        const actedAt = Date.now();
        await flushMicrotasks();
        vi.advanceTimersByTime(60_000);
        // The other device's change while this court was away: between the
        // two, so older than the court's action.
        const { sent } = server(actedAt - 30_000);
        window.dispatchEvent(new Event('online'));
        await vi.advanceTimersByTimeAsync(10_000);
        await flushMicrotasks();
        const replay = sent.find((r) => r.url.includes('/m1/'));
        expect(replay.body.modifiedAt).toBe(actedAt);
        expect(API.unsentWrites().total).toBe(0);
    });

    // A reopen, requeue or send back is stamped by the server's own clock. A
    // device whose estimate of that clock runs behind still stamps its next
    // change to the match past it.
    it('a write after a server-stamped reopen is stamped past it', async () => {
        const reopenedAt = Date.now() + 2000; // this device's estimate runs 2 s behind
        const { sent } = server(reopenedAt);
        const reopened = { id: 'm1', compId: 'c1', status: 'running', modifiedAt: reopenedAt };
        const res = await API.recordScore('c1', 'm1', { status: 'running', ipponsA: ['K'] }, 'pw', reopened);
        expect(sent[0].body.modifiedAt).toBe(reopenedAt + 1);
        expect(res && res.applied).not.toBe(false);
    });
});

describe('an autosave is floored by the match as it was at the tap', () => {
    // The tap was made against the point stamped 3 s ahead. Before the debounce
    // sent it, another device finished the match (stamped later still), and
    // the editor's prop moved on. The write is floored by what the tap saw,
    // so it stays older than the finish (operator ruling 2026-09-27).
    it('uses seenModifiedAt from the patch, never the match handed in at send', async () => {
        const tapSaw = Date.now() + 3000;
        const finish = Date.now() + 4500;
        const { sent } = server(0);
        const atSend = { id: 'm1', compId: 'c1', status: 'completed', modifiedAt: finish };
        await API.recordScore('c1', 'm1', { status: 'running', ipponsA: ['M'], seenModifiedAt: tapSaw }, 'pw', atSend);
        expect(sent[0].body.modifiedAt).toBe(tapSaw + 1);
        expect(sent[0].body.modifiedAt).toBeLessThan(finish);
        expect(sent[0].body).not.toHaveProperty('seenModifiedAt');
    });

    // A tap made while the match carried no stamp at all has nothing to be
    // floored by, whatever the match is stamped by when the write goes out.
    it('a tap that saw no stamp is not floored by one that arrived later', async () => {
        const { sent } = server(0);
        const atSend = { id: 'm1', compId: 'c1', status: 'completed', modifiedAt: Date.now() + 4500 };
        await API.recordScore('c1', 'm1', { status: 'running', ipponsA: ['M'], seenModifiedAt: 0 }, 'pw', atSend);
        expect(sent[0].body.modifiedAt).toBe(Date.now());
    });
});

describe('every stamped write is floored the same way', () => {
    it('a decision: floored by the match it was recorded on, which is never sent', async () => {
        const seen = Date.now() + 3000;
        const { sent } = server(seen);
        const res = await API.recordDecision('c1', 'm1', { decision: 'kiken', decisionBy: 'A', seenModifiedAt: seen }, 'pw');
        expect(sent[0].body.modifiedAt).toBe(seen + 1);
        expect(sent[0].body).not.toHaveProperty('seenModifiedAt');
        expect(res && res.applied).not.toBe(false);
    });

    it('a hand-set winner', async () => {
        const seen = Date.now() + 3000;
        const { sent } = server(seen);
        await API.overrideBracketWinner('c1', 'r1-m1', 'Team A', 'pw', false, seen);
        expect(sent[0].body.modifiedAt).toBe(seen + 1);
    });

    it('a representative bout added and removed', async () => {
        const seen = Date.now() + 3000;
        const { sent } = server(0);
        global.fetch = vi.fn((url, opts) => {
            if (isClockPoll(url)) return Promise.reject(new TypeError('clock poll'));
            sent.push({ url: String(url), body: JSON.parse(opts.body) });
            return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ id: 'r1-m1', status: 'running' }) });
        });
        await API.recordDaihyosen('c1', 'r1-m1', 'pw', seen);
        await API.removeDaihyosen('c1', 'r1-m1', 'pw', seen);
        expect(sent.map((r) => r.body.modifiedAt)).toEqual([seen + 1, seen + 1]);
    });
});

describe('a queued write keeps its floor when it is re-stamped', () => {
    // A write queued offline is replayed with the stamp it was made with. One
    // refused for clock skew is re-stamped from when it was queued (_restampFor),
    // and that reconstruction must not drop below what the edit was made
    // against, or the replay loses the guarantee the first send had.
    it('the re-stamp after a clock_skew refusal stays after the match it was made against', async () => {
        const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
        const seen = Date.now() + 3000;
        // Offline first: the write is queued with its floor.
        global.fetch = vi.fn(() => Promise.reject(new TypeError('offline')));
        await API.recordScore('c1', 'm1', { status: 'completed', winner: 'A', ipponsA: ['M', 'M'] }, 'pw', { id: 'm1', compId: 'c1', status: 'running', modifiedAt: seen });
        await flushMicrotasks();
        const stored = JSON.parse(localStorage.getItem('bc_write_queue'));
        expect(stored[0][1].seenModifiedAt).toBe(seen);
        // Back online, the server refuses the first replay for clock skew
        // (its clock is behind this device's); the re-stamp is floored.
        let call = 0;
        const sent = [];
        global.fetch = vi.fn((url, opts) => {
            if (isClockPoll(url)) {
                return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ nowMs: Date.now() }) });
            }
            const body = JSON.parse(opts.body);
            sent.push(body);
            call++;
            const answer = call === 1
                ? { applied: false, reason: 'clock_skew', serverNowMs: Date.now() }
                : { applied: true };
            return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(answer) });
        });
        window.dispatchEvent(new Event('online'));
        await vi.advanceTimersByTimeAsync(10_000);
        await flushMicrotasks();
        expect(sent.length).toBeGreaterThanOrEqual(2);
        expect(sent[1].modifiedAt).toBeGreaterThan(seen);
        warnSpy.mockRestore();
    });
});

describe('a write resent after a clock_skew refusal keeps its floor', () => {
    // The direct resend (not the queue's) re-stamps with this device's time
    // once the offset is relearned; it stays after what the write was made
    // against.
    function skewOnce() {
        const sent = [];
        let call = 0;
        global.fetch = vi.fn((url, opts) => {
            if (isClockPoll(url)) {
                return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ nowMs: Date.now() }) });
            }
            sent.push(JSON.parse(opts.body));
            call++;
            const answer = call === 1 ? { applied: false, reason: 'clock_skew', serverNowMs: Date.now() } : { applied: true };
            return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(answer) });
        });
        return sent;
    }

    // Only a finished result is resent at once (a running update is left to
    // the next autosave).
    it('a finished result', async () => {
        const seen = Date.now() + 3000;
        const sent = skewOnce();
        await API.recordScore('c1', 'm1', { status: 'completed', winner: 'A', ipponsA: ['M', 'M'] }, 'pw', { id: 'm1', compId: 'c1', status: 'running', modifiedAt: seen });
        expect(sent).toHaveLength(2);
        expect(sent[1].modifiedAt).toBe(seen + 1);
    });

    it('a hand-set winner', async () => {
        const seen = Date.now() + 3000;
        const sent = skewOnce();
        await API.overrideBracketWinner('c1', 'r1-m1', 'Team A', 'pw', false, seen);
        expect(sent).toHaveLength(2);
        expect(sent[1].modifiedAt).toBe(seen + 1);
    });
});

describe('a queued write that takes another\'s place keeps the later floor', () => {
    // Two running updates made offline coalesce into one queued entry (the
    // later one is stamped no earlier: here 5 s pass between them). The first
    // was made against a newer match than the second's snapshot: the entry
    // keeps the later floor, so a re-stamped replay is never before either.
    it('the stored entry carries the higher seen stamp', async () => {
        global.fetch = vi.fn(() => Promise.reject(new TypeError('offline')));
        const high = Date.now() + 3000;
        const low = Date.now() - 60_000;
        await API.recordScore('c1', 'm1', { status: 'running', ipponsA: ['M'], seenModifiedAt: high }, 'pw', null);
        await flushMicrotasks();
        vi.advanceTimersByTime(5000);
        await API.recordScore('c1', 'm1', { status: 'running', ipponsA: ['M', 'K'], seenModifiedAt: low }, 'pw', null);
        await flushMicrotasks();
        const stored = JSON.parse(localStorage.getItem('bc_write_queue'));
        expect(stored).toHaveLength(1);
        expect(stored[0][1].seenModifiedAt).toBe(high);
    });
});
