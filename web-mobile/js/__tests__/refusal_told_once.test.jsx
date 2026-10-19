// A write the server refuses on replay is told ONCE ("Only one behaviour
// allowed"; nothing a device sends goes untold). The editor open on that match
// shows it in its not-saved banner, and when it does (its terminal-write
// listener answers true) the outbox raises no queue alert repeating it. With
// no editor showing it the alert is the one tell. Before, both fired: an
// offline Finish whose match was opened again before reconnecting, a decision
// recorded on the court console with no next match, or the kachinuki End
// match were said in the editor's banner and in the toast together.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

let _origFetch;
let _origEventSource;
let _origLocalStorage;
let _lsStore;

beforeEach(() => {
    vi.useFakeTimers();
    vi.resetModules();
    _origEventSource = global.EventSource;
    global.EventSource = class FakeES {
        constructor(url) { this.url = url; this.readyState = 0; this.close = () => {}; }
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
    if (_origFetch === undefined) delete global.fetch; else global.fetch = _origFetch;
    if (_origEventSource === undefined) delete global.EventSource; else global.EventSource = _origEventSource;
    if (_origLocalStorage === undefined) delete global.localStorage; else global.localStorage = _origLocalStorage;
});

async function flushMicrotasks() {
    for (let i = 0; i < 12; i++) await Promise.resolve();
}
async function tick(ms = 0) {
    await vi.advanceTimersByTimeAsync(ms);
    await flushMicrotasks();
}

// A server that answers /api/time at once and every write with `reply()`
// ({status, body}); offline, every write fails as a dead network does.
function makeServer() {
    const server = { online: true, writes: 0, reply: () => ({ status: 200, body: {} }) };
    server.fetch = vi.fn((url, opts) => {
        const u = String(url);
        if (u.includes('/api/time')) {
            return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ nowMs: Date.now() }) });
        }
        if (!server.online) return Promise.reject(new TypeError('network error'));
        if (opts && opts.body) {
            server.writes++;
            if (server.writes > 6) throw new Error('unbounded resend loop');
        }
        const { status, body } = server.reply();
        return Promise.resolve({
            ok: status >= 200 && status < 300, status,
            json: () => Promise.resolve(body),
            text: () => Promise.resolve(JSON.stringify(body)),
        });
    });
    return server;
}

async function load(server) {
    global.fetch = server.fetch;
    const mod = await import('../api_client.jsx');
    await flushMicrotasks();
    return mod;
}

// The editor's listener as the three editors register it: mounted on match
// `mid` of `cid`, it shows the refusal and answers true; for another match it
// takes nothing.
const editorOn = (cid, mid, shown) => (info) => {
    if (!info || info.compID !== cid || info.matchID !== mid) return undefined;
    shown.push(info);
    return true;
};

const superseded = { applied: false, reason: 'superseded', heldGroups: ['result'], message: 'A newer change was recorded first.' };
const skew = () => ({ status: 200, body: { applied: false, reason: 'clock_skew', serverNowMs: Date.now() + 100000 } });
const conflict = { error: 'already_ineligible', reasonHuman: 'Alice withdrew in Pool A · Match 1 and cannot fight again.' };

// Queues `send` offline, then brings the server back with `reply` and lets the
// flush run; answers the alerts raised and the refusals an editor showed.
async function replay(send, reply, { editor } = {}) {
    const server = makeServer();
    const mod = await load(server);
    const alerts = [];
    const shown = [];
    const unsubA = mod.subscribeQueueAlert((a) => alerts.push(a));
    const unsubE = mod.subscribeTerminalWriteFailed(editor ? editorOn(editor[0], editor[1], shown) : () => undefined);
    server.online = false;
    await send(mod.API);
    await flushMicrotasks();
    server.online = true;
    server.reply = reply;
    window.dispatchEvent(new Event('online'));
    await tick(3000);
    unsubA();
    unsubE();
    return { alerts: alerts.filter((a) => a.kind !== 'sent'), shown, server };
}

const finish = (API) => API.recordScore('c1', 'm1', { status: 'completed', winner: 'A' }, 'pw', null);
const decision = (API) => API.recordDecision('c1', 'm1', { decision: 'fusensho', decisionBy: 'B' }, 'pw');

describe('a refusal the open editor shows is not toasted again', () => {
    beforeEach(() => { vi.spyOn(console, 'warn').mockImplementation(() => {}); });
    afterEach(() => { console.warn.mockRestore(); });

    it('Finish superseded on replay: the editor on the match says it, no alert', async () => {
        const r = await replay(finish, () => ({ status: 200, body: superseded }), { editor: ['c1', 'm1'] });
        expect(r.shown.length).toBe(1);
        expect(r.alerts).toEqual([]);
    });

    it('Finish superseded with no editor on the match: the alert is the one tell', async () => {
        const r = await replay(finish, () => ({ status: 200, body: superseded }), { editor: ['c1', 'other'] });
        expect(r.shown.length).toBe(0);
        expect(r.alerts.map((a) => a.kind)).toEqual(['superseded']);
    });

    it('a decision refused 409 on replay (the court console with no next match): said once by the editor', async () => {
        const r = await replay(decision, () => ({ status: 409, body: conflict }), { editor: ['c1', 'm1'] });
        expect(r.shown.length).toBe(1);
        expect(r.alerts).toEqual([]);
    });

    it('a decision refused 409 with the editor closed: the alert still fires', async () => {
        const r = await replay(decision, () => ({ status: 409, body: conflict }));
        expect(r.alerts.map((a) => a.kind)).toEqual(['rejected']);
    });

    it('a Finish (or End match) refused twice for the clock: said once by the editor', async () => {
        const r = await replay(finish, skew, { editor: ['c1', 'm1'] });
        expect(r.server.writes).toBe(2);
        expect(r.shown.length).toBe(1);
        expect(r.alerts).toEqual([]);
    });

    it('a Finish refused twice for the clock with the editor closed: the alert still fires', async () => {
        const r = await replay(finish, skew);
        expect(r.server.writes).toBe(2);
        expect(r.alerts.map((a) => a.kind)).toEqual(['rejected']);
    });

    it('an own Start refused twice for the clock: no alert, its settlement carries the refusal the editor says', async () => {
        let settled;
        const r = await replay(async (API) => {
            const res = await API.recordScore('c1', 'm1', { status: 'running' }, 'pw', null);
            settled = res.settled;
        }, skew, { editor: ['c1', 'm1'] });
        expect(r.server.writes).toBe(2);
        expect(r.alerts).toEqual([]);
        expect(r.shown.length).toBe(0);
        await expect(settled).resolves.toMatchObject({ applied: false, reason: 'clock_skew' });
    });
});
