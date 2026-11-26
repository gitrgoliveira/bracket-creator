// bc-sseq FR5/FR6: a write the server held answers `applied: false` with the
// match as stored (`match`). The outbox shows that copy BEFORE anyone hears of
// the refusal: it goes to the turned-away record (as every copy of a match
// does) and to the local listeners as a whole-match patch, then the editor
// open on the match (or the queue alert) is told, and only then does the
// entry leave the queue, which ends the court console's own status move. So
// the note and the sheet never disagree, and no moment shows the match as the
// refused Finish left it (running). The same holds when another tab of the
// device sent the write: the tab that made it settles it with the shared
// answer and shows the stored match too.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

let _origFetch;
let _origEventSource;
let _origLocalStorage;
let _lsStore;

beforeEach(() => {
    vi.useFakeTimers();
    vi.resetModules();
    vi.spyOn(console, 'warn').mockImplementation(() => {});
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
    console.warn.mockRestore();
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

// The match as another device's finish left it, as the server stores it.
const stored = () => ({
    id: 'm1', status: 'completed', winner: 'p2', winnerId: 'p2', ipponsB: ['M', 'K'],
    modifiedAt: 5000, scoreVersion: 7, turnedAway: { 'tap-1': 'match_closed' },
});
const heldAnswer = (match) => ({
    applied: false, reason: 'superseded', heldReason: 'match_closed', heldGroups: ['result'],
    message: 'Not applied: the match was already finished. To change the result, tap Correct.',
    ...(match ? { match } : {}),
});
const finish = (API) => API.recordScore('c1', 'm1', { status: 'completed', winner: 'A' }, 'pw', null);

// Records, in order, what each listener heard, and what the court console's
// own status move read at the moment the stored match was shown.
function listen(mod, log) {
    const unsubs = [
        mod.subscribeLocalMatchPatch((event) => {
            const mv = mod.API.ownStatusMove('c1', 'm1');
            log.push({ what: 'patch', event, ownMove: mv && mv.status });
        }),
        mod.subscribeTerminalWriteFailed((info) => {
            if (!info || info.matchID !== 'm1') return undefined;
            log.push({ what: 'editor', info });
            return true;
        }),
        mod.API.subscribeOwnStatusMoves(() => {
            const mv = mod.API.ownStatusMove('c1', 'm1');
            log.push({ what: 'own', status: mv && mv.status });
        }),
        mod.subscribeQueueAlert((a) => { if (a.kind !== 'sent') log.push({ what: 'alert', kind: a.kind }); }),
    ];
    return () => unsubs.forEach((u) => u());
}

describe('a refused write shows the stored match before the note (FR6)', () => {
    it('the whole stored match is patched in before the editor hears, while the Finish still holds the match', async () => {
        const server = makeServer();
        const mod = await load(server);
        const log = [];
        server.online = false;
        await finish(mod.API);
        await flushMicrotasks();
        expect(mod.API.ownStatusMove('c1', 'm1'), 'the queued Finish shows the match finished').toMatchObject({ status: 'completed' });
        const stop = listen(mod, log);
        server.online = true;
        server.reply = () => ({ status: 200, body: heldAnswer(stored()) });
        window.dispatchEvent(new Event('online'));
        await tick(3000);
        stop();

        const patches = log.filter((e) => e.what === 'patch');
        expect(patches.length, 'shown once, though the flush and the dequeue both ask').toBe(1);
        const [p] = patches;
        expect(p.event).toMatchObject({ type: 'match_updated', local: true, whole: true, data: { competitionId: 'c1', matchId: 'm1' } });
        expect(p.event.data.result).toMatchObject({ id: 'm1', status: 'completed', winner: 'p2', scoreVersion: 7 });
        expect(p.ownMove, 'the queued Finish still held the match when the stored copy came').toBe('completed');

        const order = log.map((e) => e.what).filter((w) => w !== 'own');
        expect(order.indexOf('patch'), 'the patch comes before the note').toBeLessThan(order.indexOf('editor'));
        expect(log.find((e) => e.what === 'editor').info.compID).toBe('c1');
        expect(log.filter((e) => e.what === 'alert'), 'the editor said it: no alert').toEqual([]);
        // The Finish left the queue after the patch: the console reads the
        // server's copy again, with no answered move kept over it.
        const lastOwn = log.filter((e) => e.what === 'own').pop();
        expect(lastOwn && lastOwn.status).toBe(null);
        expect(log.findIndex((e) => e.what === 'own' && e.status === null)).toBeGreaterThan(log.findIndex((e) => e.what === 'patch'));
        expect(mod.API.ownStatusMove('c1', 'm1')).toBeNull();
    });

    it('the stored copy goes to the turned-away record like every copy of the match', async () => {
        const server = makeServer();
        const mod = await load(server);
        const store = await import('../api_client_store.jsx');
        server.online = false;
        await finish(mod.API);
        await flushMicrotasks();
        server.online = true;
        server.reply = () => ({ status: 200, body: heldAnswer(stored()) });
        window.dispatchEvent(new Event('online'));
        await tick(3000);
        const seen = [...store._turnedAwaySeen.values()].find((x) => x && x.matchID === 'm1');
        expect(seen, 'remembered').toBeTruthy();
        expect(seen).toMatchObject({ v: 7, away: { 'tap-1': 'match_closed' } });
    });

    it('a refusal that carries no match, or another match, patches nothing', async () => {
        for (const match of [null, { ...stored(), id: 'm2' }]) {
            vi.resetModules();
            _lsStore = {};
            const server = makeServer();
            const mod = await load(server);
            const log = [];
            server.online = false;
            await finish(mod.API);
            await flushMicrotasks();
            const stop = listen(mod, log);
            server.online = true;
            server.reply = () => ({ status: 200, body: heldAnswer(match) });
            window.dispatchEvent(new Event('online'));
            await tick(3000);
            stop();
            expect(log.filter((e) => e.what === 'patch')).toEqual([]);
            expect(log.filter((e) => e.what === 'editor').length, 'still told').toBe(1);
        }
    });

    it('a write another tab sent: this tab settles it with the shared answer and shows the stored match', async () => {
        const server = makeServer();
        const mod = await load(server);
        const log = [];
        server.online = false;
        await finish(mod.API);
        await flushMicrotasks();
        const queued = JSON.parse(_lsStore.bc_write_queue_v2 || '[]');
        expect(queued.length, 'the Finish is stored').toBe(1);
        const [key, d] = queued[0];
        // Another tab sent it and left its answer (_shareAnswer).
        _lsStore[`bc_entry_answer_v1:${key}`] = JSON.stringify({
            at: Date.now(), tab: 'another-tab', enqueuedAt: d.enqueuedAt, outcome: { value: heldAnswer(stored()) },
        });
        const stop = listen(mod, log);
        server.online = true;
        const writesBefore = server.writes;
        window.dispatchEvent(new Event('online'));
        await tick(3000);
        stop();
        expect(server.writes, 'not sent again').toBe(writesBefore);
        const patches = log.filter((e) => e.what === 'patch');
        expect(patches.length).toBe(1);
        expect(patches[0].event.whole).toBe(true);
        expect(patches[0].event.data.result).toMatchObject({ status: 'completed', winner: 'p2' });
        expect(mod.API.ownStatusMove('c1', 'm1')).toBeNull();
    });
});
