// bc-evts stage 3 S3d / S8 b: every rule-bound write carries a writeId, the
// device's event session and a basis {seen, own, seq}: what the screen showed
// (the score log line count, scoreVersion), the ids of this device's taps for
// the match still queued or in flight, and the last seq this session queued
// for it. A resend reuses the writeId.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

let mod;
let API;
let _orig;

beforeEach(async () => {
    vi.useFakeTimers();
    vi.resetModules();
    _orig = { fetch: global.fetch, ES: global.EventSource, ls: global.localStorage };
    global.EventSource = class FakeES { constructor() { this.close = () => {}; } };
    global.EventSource.OPEN = 1;
    const store = {};
    global.localStorage = {
        getItem: (k) => (k in store ? store[k] : null),
        setItem: (k, v) => { store[k] = String(v); },
        removeItem: (k) => { delete store[k]; },
    };
    global.fetch = vi.fn(() => new Promise(() => {}));
    mod = await import('../api_client.jsx');
    API = mod.API;
});

afterEach(() => {
    vi.useRealTimers();
    vi.resetModules();
    global.fetch = _orig.fetch;
    global.EventSource = _orig.ES;
    global.localStorage = _orig.ls;
});

async function flushMicrotasks() { for (let i = 0; i < 30; i++) await Promise.resolve(); }
const isClock = (url) => String(url).includes('/api/time');
// Every request the outbox made, by URL tail, with its parsed body.
function sentBodies() {
    return global.fetch.mock.calls
        .filter(([u]) => !isClock(u))
        .map(([u, o]) => ({ door: String(u).split('/').pop(), body: o && o.body ? JSON.parse(o.body) : null }));
}

const SHOWN = { id: 'mX', modifiedAt: 0, scoreVersion: 7 };

describe('every rule-bound door tags its write', () => {
    const DOORS = [
        ['score (Finish)', () => API.recordScore('c1', 'm1', { status: 'completed', winner: 'A' }, 'pw', { ...SHOWN, id: 'm1' })],
        ['score (running)', () => API.recordScore('c1', 'm1', { status: 'running', ipponsA: ['M'] }, 'pw', { ...SHOWN, id: 'm1' })],
        ['decision', () => API.recordDecision('c1', 'm1', { decision: 'kiken-voluntary', decisionBy: 'aka' }, 'pw', { seen: 7 })],
        ['reopen', () => API.reopenMatch('c1', 'm1', 'pw', { reason: 'x', seen: 7 })],
        ['requeue-blocker-and-reopen', () => API.requeueBlockerAndReopen('c1', 'm1', 'c1', 'm2', 'pw', { seen: 7 })],
        ['revert-to-queue', () => API.revertMatchToQueue('c1', 'm1', 'pw', { seen: 7 })],
        ['override-winner', () => API.overrideBracketWinner('c1', 'm1', 'Alice', 'pw', false, 0, { seen: 7 })],
        ['daihyosen (add)', () => API.recordDaihyosen('c1', 'm1', 'pw', 0, { seen: 7 })],
        ['daihyosen (remove)', () => API.removeDaihyosen('c1', 'm1', 'pw', 0, { seen: 7 })],
        ['kachinuki-bout', () => API.removeKachinukiBout('c1', 'm1', 'pw', { seen: 7 })],
    ];

    it.each(DOORS)('%s: writeId, session and basis {seen, own, seq}', async (_name, send) => {
        // Not awaited: a running write still goes direct and its fetch never
        // answers here; the outbox doors have sent theirs by the next turns.
        send();
        await flushMicrotasks();
        const [{ body }] = sentBodies();
        expect(typeof body.writeId).toBe('string');
        expect(body.writeId.length).toBeGreaterThan(0);
        expect(body.writeId.length).toBeLessThanOrEqual(128);
        expect(typeof body.session).toBe('string');
        expect(body.basis).toEqual({ seen: 7, own: [], seq: 0 });
    });
});

describe('the basis says what this device has sent', () => {
    it('own = this device\'s taps for the match still queued; seq = the last one queued; session = the taps\'', async () => {
        const a = API.recordTap('c1', 'm1', { kind: 'ippon.add', pos: 0, side: 'a', letter: 'M' }, 'pw', { id: 'm1' });
        const b = API.recordTap('c1', 'm1', { kind: 'ippon.add', pos: 0, side: 'b', letter: 'K' }, 'pw', { id: 'm1' });
        // A tap of ANOTHER match is no part of this one's basis.
        API.recordTap('c1', 'm2', { kind: 'ippon.add', pos: 0, side: 'a', letter: 'D' }, 'pw', { id: 'm2' });
        const fin = await API.recordScore('c1', 'm1', { status: 'completed', winner: 'A' }, 'pw', { ...SHOWN, id: 'm1' });
        const finBody = JSON.parse(localStorage.getItem('bc_write_queue'))
            .find(([k]) => k === fin.key)[1].payload;
        expect(finBody.basis).toEqual({ seen: 7, own: [a.id, b.id], seq: 2 });
        expect(a.id.startsWith(`${finBody.session}.`)).toBe(true);
    });

    it('seen falls back to the newest derived match a score-events answer handed back', async () => {
        global.fetch = vi.fn((url, opts) => {
            if (isClock(url)) return Promise.reject(new TypeError('clock'));
            if (String(url).endsWith('/score-events')) {
                const events = JSON.parse(opts.body).events;
                return Promise.resolve({ ok: true, status: 200, json: async () => ({
                    outcomes: events.map((e) => ({ id: e.id, outcome: 'applied' })),
                    match: { id: 'm1', scoreVersion: 12 },
                }) });
            }
            return new Promise(() => {});
        });
        API.recordTap('c1', 'm1', { kind: 'ippon.add', pos: 0, side: 'a', letter: 'M' }, 'pw', { id: 'm1' });
        await vi.advanceTimersByTimeAsync(0);
        await flushMicrotasks();
        await API.recordDecision('c1', 'm1', { decision: 'fusensho', decisionBy: 'aka' }, 'pw');
        await flushMicrotasks();
        const dec = sentBodies().find((s) => s.door === 'decision');
        expect(dec.body.basis).toEqual({ seen: 12, own: [], seq: 1 });
    });

    it('a resend keeps the writeId', async () => {
        vi.spyOn(console, 'warn').mockImplementation(() => {});
        let n = 0;
        global.fetch = vi.fn((url) => {
            if (isClock(url)) return Promise.reject(new TypeError('clock'));
            n++;
            return Promise.resolve(n === 1
                ? { ok: false, status: 503, json: async () => ({}) }
                : { ok: true, status: 200, json: async () => ({}) });
        });
        const res = await API.recordScore('c1', 'm1', { status: 'completed', winner: 'A' }, 'pw', { ...SHOWN, id: 'm1' });
        await vi.advanceTimersByTimeAsync(60000);
        await res.settled;
        const bodies = sentBodies().map((s) => s.body.writeId);
        expect(bodies.length).toBe(2);
        expect(bodies[0]).toBe(bodies[1]);
        console.warn.mockClear();
    });

    it('the court display overlay never carries writeId, basis or session', async () => {
        const published = [];
        const origBC = global.BroadcastChannel;
        global.BroadcastChannel = class {
            constructor() { this.onmessage = null; }
            postMessage(msg) { published.push(msg); }
            close() {}
        };
        vi.resetModules();
        const m = await import('../api_client.jsx');
        await m.API.recordScore('c1', 'm1', { status: 'completed', winner: 'A' }, 'pw', { ...SHOWN, id: 'm1', court: 'A' });
        global.BroadcastChannel = origBC;
        expect(published.length).toBe(1);
        const payload = JSON.stringify(published[0]);
        expect(payload).not.toContain('writeId');
        expect(payload).not.toContain('basis');
        expect(payload).not.toContain('"session"');
    });
});
