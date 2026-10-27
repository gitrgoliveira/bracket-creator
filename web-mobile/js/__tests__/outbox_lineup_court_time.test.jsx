// bc-evts stage 3, review round 1, M5: "ALL goes to the outbox ... but never
// blocks the UI" (operator ruling). A match's lineup save, a court move and a
// time change are entries of their match, answered at once and sent strictly
// in that match's order; a send back to the queue with a reopen follows the
// blocker match's own queued writes.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

let mod;
let API;
let _lsStore;
let _orig;

async function load() {
    vi.resetModules();
    mod = await import('../api_client.jsx');
    API = mod.API;
}

beforeEach(async () => {
    vi.useFakeTimers();
    _orig = { fetch: global.fetch, ES: global.EventSource, ls: global.localStorage };
    global.EventSource = class FakeES { constructor() { this.close = () => {}; } };
    global.EventSource.OPEN = 1;
    _lsStore = {};
    global.localStorage = {
        getItem: (k) => (k in _lsStore ? _lsStore[k] : null),
        setItem: (k, v) => { _lsStore[k] = String(v); },
        removeItem: (k) => { delete _lsStore[k]; },
        clear: () => { _lsStore = {}; },
    };
    global.fetch = vi.fn(() => Promise.reject(new TypeError('offline')));
    await load();
});

afterEach(() => {
    // Quiesce this module instance before the real timers come back, so a
    // flush still out never sends through a later test's fetch (sync_queue M6).
    if (API && typeof API.clearQueue === 'function') API.clearQueue();
    vi.clearAllTimers();
    vi.useRealTimers();
    vi.resetModules();
    global.fetch = _orig.fetch;
    global.EventSource = _orig.ES;
    global.localStorage = _orig.ls;
});

async function flushMicrotasks() { for (let i = 0; i < 30; i++) await Promise.resolve(); }
async function tick(ms = 0) { await vi.advanceTimersByTimeAsync(ms); await flushMicrotasks(); }
const isClock = (url) => String(url).includes('/api/time');
const stored = () => JSON.parse(_lsStore.bc_write_queue_v2 || '[]');
const path = (url) => String(url).replace(/^\/api\/competitions\/c1\//, '');

// Records every request; `answer(path, n)` returns {status, body} or null to
// leave it unanswered.
function server(answer = () => ({ status: 200, body: {} })) {
    const sent = [];
    global.fetch = vi.fn((url, opts) => {
        if (isClock(url)) return Promise.reject(new TypeError('clock'));
        const p = path(url);
        const n = sent.filter((s) => s.path === p).length;
        sent.push({ path: p, method: opts.method, body: opts.body ? JSON.parse(opts.body) : null });
        const a = answer(p, n);
        if (!a) return new Promise(() => {});
        return Promise.resolve({ ok: a.status < 400, status: a.status, json: async () => a.body });
    });
    return sent;
}

// Whether a promise settled by now.
async function settledNow(p) {
    let done = false;
    p.then(() => { done = true; }, () => { done = true; });
    await flushMicrotasks();
    return done;
}

describe('a match lineup save is an entry of its match', () => {
    it('answers at once, pending, even when the server does not answer', async () => {
        server(() => null);
        const p = API.putMatchLineup('c1', 't1', 'm1', { senpo: 'Ann' }, 'pw', undefined, ['senpo']);
        expect(await settledNow(p)).toBe(true);
        const res = await p;
        expect(res.pending).toBe(true);
        expect(stored().map(([k]) => k.split('\u001f')[0])).toEqual(['c1:m1']);
    });

    it('goes after the match\'s score write queued before it, and settles with the stored lineup', async () => {
        const fin = await API.recordScore('c1', 'm1', { status: 'running', ipponsA: ['M'] }, 'pw', { id: 'm1' });
        const save = await API.putMatchLineup('c1', 't1', 'm1', { senpo: 'Ann' }, 'pw', undefined, ['senpo']);
        const sent = server((p) => (p.includes('match-lineups')
            ? { status: 200, body: { positions: { senpo: 'Ann', jiho: 'Bo' } } }
            : { status: 200, body: { applied: true } }));
        window.dispatchEvent(new Event('online'));
        await tick(0);
        expect(sent.map((s) => s.path)).toEqual(['matches/m1/score', 'teams/t1/match-lineups/m1']);
        expect(sent[1].method).toBe('PUT');
        expect(sent[1].body.changed).toEqual(['senpo']);
        await fin.settled;
        expect(await save.settled).toEqual({ positions: { senpo: 'Ann', jiho: 'Bo' } });
    });

    it('a refusal settles as a rejection the caller may claim', async () => {
        vi.spyOn(console, 'warn').mockImplementation(() => {});
        server(() => ({ status: 400, body: { error: 'duplicate', message: 'Ann is already placed.' } }));
        const claimed = [];
        const save = await API.putMatchLineup('c1', 't1', 'm1', { senpo: 'Ann' }, 'pw', undefined, ['senpo'],
            { onRefusal: (e) => { claimed.push(e.message); return true; } });
        await tick(0);
        await expect(save.settled).rejects.toThrow('Ann is already placed.');
        expect(claimed).toEqual(['Ann is already placed.']);
        console.warn.mockClear();
    });

    it('a second save of the team\'s lineup joins the queued one; the other team\'s stays apart', async () => {
        await API.putMatchLineup('c1', 't1', 'm1', { senpo: 'Ann' }, 'pw', undefined, ['senpo']);
        await API.putMatchLineup('c1', 't2', 'm1', { senpo: 'Cy' }, 'pw', undefined, ['senpo']);
        await API.putMatchLineup('c1', 't2', 'm1', { jiho: 'Di' }, 'pw', undefined, ['jiho']);
        expect(stored()).toHaveLength(2);
        expect(API.queuedLineupSave('c1', 't1', { matchId: 'm1' })).toBe(true);
        expect(API.queuedLineupSave('c1', 't2', { matchId: 'm1' })).toBe(true);
        expect(API.queuedLineupSave('c1', 't3', { matchId: 'm1' })).toBe(false);
        const t2 = stored().map(([, d]) => d.payload).find((p) => p.teamId === 't2');
        expect([...t2.changed].sort()).toEqual(['jiho', 'senpo']);
    });

    it('a team\'s starting lineup save answers at once too', async () => {
        server(() => null);
        const p = API.putTeamLineup('c1', 't1', 0, { senpo: 'Ann' }, 'pw', undefined, ['senpo']);
        expect(await settledNow(p)).toBe(true);
        expect((await p).pending).toBe(true);
    });
});

describe('a court move and a time change are entries of their match', () => {
    it('answer at once and go after the match\'s queued score write', async () => {
        await API.recordScore('c1', 'm1', { status: 'running', ipponsA: ['M'] }, 'pw', { id: 'm1' });
        const court = API.moveMatchCourt('c1', 'm1', 'B', 'pw');
        const time = API.updateMatchTime('c1', 'm1', '10:30', 'pw');
        expect(await settledNow(court)).toBe(true);
        expect(await settledNow(time)).toBe(true);
        expect((await court).pending).toBe(true);
        const sent = server((p) => ({ status: 200, body: p.endsWith('/score') ? { applied: true } : {} }));
        window.dispatchEvent(new Event('online'));
        await tick(0);
        expect(sent.map((s) => s.path)).toEqual(['matches/m1/score', 'matches/m1/court', 'matches/m1/time']);
        expect(sent[1].body.court).toBe('B');
        expect(sent[1].body.stopsRunning).toBeUndefined();
        expect(sent[2].body.scheduledAt).toBe('10:30');
    });

    it('courtMoveQueued is true while a court entry of the match is queued, and not for a time change or another match', async () => {
        expect(API.courtMoveQueued('c1', 'm1')).toBe(false);
        await API.updateMatchTime('c1', 'm1', '10:30', 'pw');
        expect(API.courtMoveQueued('c1', 'm1')).toBe(false);
        await API.moveMatchCourt('c1', 'm1', 'B', 'pw', { was: { court: 'A' } });
        expect(API.courtMoveQueued('c1', 'm1')).toBe(true);
        expect(API.courtMoveQueued('c1', 'm2')).toBe(false);

        const sent = server(() => ({ status: 200, body: {} }));
        window.dispatchEvent(new Event('online'));
        await tick(0);
        expect(sent.map((s) => s.path)).toContain('matches/m1/court');
        expect(API.courtMoveQueued('c1', 'm1')).toBe(false);
    });

    // The host asks it before a running match's move (admin.jsx
    // moveMatchCourt): the first confirmed move stops the match on the
    // server, so a second one made before the answer is a waiting match's.
    it('stoppingCourtMoveQueued is true while a move that stops the match is queued or out, and not for a plain move', async () => {
        expect(API.stoppingCourtMoveQueued('c1', 'm1')).toBe(false);
        await API.moveMatchCourt('c1', 'm1', 'B', 'pw', { was: { court: 'A' } });
        expect(API.stoppingCourtMoveQueued('c1', 'm1')).toBe(false);
        await API.moveMatchCourt('c1', 'm1', 'C', 'pw', { stopsRunning: true, was: { court: 'B' } });
        expect(API.stoppingCourtMoveQueued('c1', 'm1')).toBe(true);
        expect(API.stoppingCourtMoveQueued('c1', 'm2')).toBe(false);

        // Out (sent, unanswered): still the server's to apply.
        const release = [];
        global.fetch = vi.fn((url) => (isClock(url)
            ? Promise.reject(new TypeError('clock'))
            : new Promise((r) => { release.push(() => r({ ok: true, status: 200, json: async () => ({}) })); })));
        window.dispatchEvent(new Event('online'));
        await tick(0);
        release.shift()();
        await tick(0);
        expect(global.fetch.mock.calls.filter(([u]) => path(u) === 'matches/m1/court')).toHaveLength(2);
        expect(API.stoppingCourtMoveQueued('c1', 'm1')).toBe(true);

        release.shift()();
        await tick(0);
        expect(API.stoppingCourtMoveQueued('c1', 'm1')).toBe(false);
    });

    it('keeps stopsRunning: true across a reload and sends it when rehydrated', async () => {
        await API.moveMatchCourt('c1', 'm1', 'B', 'pw', { stopsRunning: true, was: { court: 'A' } });
        expect(stored()).toHaveLength(1);
        expect(stored()[0][1].payload).toEqual(expect.objectContaining({ court: 'B', stopsRunning: true }));

        // Simulate page reload: re-import api_client over the same localStorage.
        await load();
        const sent = server(() => ({ status: 200, body: {} }));
        window.dispatchEvent(new Event('online'));
        await tick(0);
        expect(sent.map((s) => s.path)).toEqual(['matches/m1/court']);
        expect(sent[0].body).toEqual(expect.objectContaining({ court: 'B', stopsRunning: true }));
    });

    it('a 409 match_status_changed on the court entry is terminal, fires onRefusal once with the sentence, and puts the old court back', async () => {
        const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
        const sentence = 'Shiro vs Aka was started on Shiaijo A before your move reached the server, so it was not moved. To stop it and put it first on Shiaijo B, move it from the Scores tab, the schedule page or the Bracket tab, which ask first.';
        const patches = [];
        API.subscribeLocalMatchPatch((e) => patches.push(e && e.data && e.data.result));
        const sent = server(() => ({
            status: 409,
            body: { code: 'match_status_changed', error: 'match_status_changed', message: sentence },
        }));
        const refused = [];
        const move = await API.moveMatchCourt('c1', 'm1', 'B', 'pw', {
            was: { court: 'A' },
            onRefusal: (e) => { refused.push(e); return true; },
        });
        await tick(0);
        await expect(move.settled).rejects.toThrow(sentence);
        const caught = await move.settled.catch((e) => e);
        expect(caught.code).toBe('match_status_changed');
        expect(refused.map((e) => e.message)).toEqual([sentence]);
        expect(refused[0].code).toBe('match_status_changed');
        expect(sent).toHaveLength(1);
        expect(stored()).toHaveLength(0);
        // Does not retry on later flushes.
        await tick(10000);
        expect(sent).toHaveLength(1);
        expect(patches).toEqual([{ id: 'm1', court: 'B' }, { id: 'm1', court: 'A' }]);
        warn.mockRestore();
    });
});

describe('a send back to the queue with a reopen follows the blocker\'s own writes', () => {
    it('is not sent while the blocker match still holds a queued write', async () => {
        await API.recordScore('c1', 'm2', { status: 'running', ipponsA: ['M'] }, 'pw', { id: 'm2' });
        const sent = server((p, n) => (p.startsWith('matches/m2/') && n === 0
            ? { status: 503, body: {} } : { status: 200, body: { applied: true } }));
        await API.requeueBlockerAndReopen('c1', 'm1', 'c1', 'm2', 'pw');
        await tick(0);
        expect(sent.map((s) => s.path)).toEqual(['matches/m2/score']);
        await tick(1000);
        expect(sent.map((s) => s.path)).toEqual([
            'matches/m2/score', 'matches/m2/score', 'matches/m1/requeue-blocker-and-reopen',
        ]);
    });
});
