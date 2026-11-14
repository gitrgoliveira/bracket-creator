// The lots warning. The organiser draws lots outside the app and enters the order
// they gave; the app never draws, changes or deletes them. A result, a decision
// or a reopen of the drawn tie-breaker those lots settled is answered 409
// lots_replaced with the server's sentence (the place and the order entered)
// until it carries the request-only confirmLots. attemptScoreWrite turns the
// refusal into the confirm and, on Replace the lots, resends the same write
// with confirmLots: true. The lots stay stored; only the bout's result then
// decides the place.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { attemptScoreWrite, lotsReplacedRefusal, lotsReplacedConfirm } from '../write_result.jsx';

const SENTENCE = "This replaces the lots you entered for 1st: Bea 1st, Aki 2nd. The bout's result will decide the place.";

function lotsError() {
    return Object.assign(new Error(SENTENCE), { code: 'lots_replaced', lotsReplaced: { matchId: 'Pool A-TB-0' } });
}

function outboxDoor() {
    const calls = [];
    const door = vi.fn((compId, matchId, result, password, match, opts) => {
        const call = { result, opts };
        call.settled = new Promise((resolve, reject) => { call.resolve = resolve; call.reject = reject; });
        call.settled.catch(() => {});
        calls.push(call);
        return Promise.resolve({ applied: true, pending: true, key: `k${calls.length}`, settled: call.settled });
    });
    door.calls = calls;
    door.refuse = (i, err) => {
        const claimed = calls[i].opts && calls[i].opts.onRefusal && calls[i].opts.onRefusal(err);
        calls[i].reject(err);
        return claimed;
    };
    return door;
}

async function settle() {
    for (let i = 0; i < 20; i++) await Promise.resolve();
}

const correction = { status: 'completed', winner: { id: 'p2', name: 'Bea' }, correctionReason: 'fought it' };
const match = { id: 'Pool A-TB-0', compId: 'c1', status: 'completed', court: 'A' };

describe('lotsReplacedConfirm', () => {
    it('shows the server\'s sentence with Replace the lots and Cancel', () => {
        const c = lotsReplacedConfirm({ message: SENTENCE });
        expect(c.message).toBe(SENTENCE);
        expect(c.confirmLabel).toBe('Replace the lots');
        expect(c.cancelLabel).toBe('Cancel');
    });

    it('never implies the app draws or decides the lots, and carries no em-dash', () => {
        const c = lotsReplacedConfirm();
        expect(c.message).not.toMatch(/—/);
        expect(c.message).not.toMatch(/\b(drew|draws|drawn by the app)\b/i);
    });

    it('lotsReplacedRefusal reads the parsed refusal and nothing else', () => {
        expect(lotsReplacedRefusal(lotsError())).toEqual({ matchId: 'Pool A-TB-0' });
        expect(lotsReplacedRefusal(Object.assign(new Error('x'), { code: 'court_busy' }))).toBeNull();
        expect(lotsReplacedRefusal(null)).toBeNull();
    });
});

describe('attemptScoreWrite and lots the organiser entered', () => {
    it('asks with the server\'s sentence, and on Replace the lots resends the same write with confirmLots', async () => {
        const door = outboxDoor();
        const confirmDialog = vi.fn().mockResolvedValue(true);
        await attemptScoreWrite({
            recordScore: door, confirmDialog, compId: 'c1', matchId: 'Pool A-TB-0', result: correction, password: 'pw', match,
        });
        expect(door.calls[0].result.confirmLots).toBeUndefined();

        expect(door.refuse(0, lotsError())).toBe(true);
        await settle();

        expect(confirmDialog).toHaveBeenCalledTimes(1);
        expect(confirmDialog.mock.calls[0][0].message).toBe(SENTENCE);
        expect(confirmDialog.mock.calls[0][0].confirmLabel).toBe('Replace the lots');
        expect(door).toHaveBeenCalledTimes(2);
        expect(door.calls[1].result).toEqual({ ...correction, confirmLots: true });
    });

    it('Cancel resends nothing and marks the refusal for the caller', async () => {
        const door = outboxDoor();
        const confirmDialog = vi.fn().mockResolvedValue(false);
        const onRefusal = vi.fn(() => true);
        await attemptScoreWrite({
            recordScore: door, confirmDialog, compId: 'c1', matchId: 'Pool A-TB-0', result: correction, password: 'pw', match, onRefusal,
        });
        const err = lotsError();
        door.refuse(0, err);
        await settle();

        expect(door).toHaveBeenCalledTimes(1);
        expect(onRefusal).toHaveBeenCalledWith(err);
        expect(err.lotsReplacedCancelled).toBe(true);
    });

    it('asks once per write: a second refusal after confirmLots goes to the caller', async () => {
        const door = outboxDoor();
        const confirmDialog = vi.fn().mockResolvedValue(true);
        const onRefusal = vi.fn(() => true);
        await attemptScoreWrite({
            recordScore: door, confirmDialog, compId: 'c1', matchId: 'Pool A-TB-0', result: correction, password: 'pw', match, onRefusal,
        });
        door.refuse(0, lotsError());
        await settle();
        const again = lotsError();
        door.refuse(1, again);
        await settle();

        expect(confirmDialog).toHaveBeenCalledTimes(1);
        expect(onRefusal).toHaveBeenCalledWith(again);
    });
});

describe('lots_replaced through the API client', () => {
    const QUEUE_STORAGE_KEY = 'bc_write_queue_v2';
    let API;
    let lsStore;
    let orig;

    async function load() {
        vi.resetModules();
        API = (await import('../api_client.jsx')).API;
    }

    beforeEach(async () => {
        vi.useFakeTimers();
        orig = { fetch: global.fetch, ES: global.EventSource, ls: global.localStorage, BC: global.BroadcastChannel };
        global.EventSource = class FakeES { constructor() { this.close = () => {}; } };
        global.EventSource.OPEN = 1;
        lsStore = {};
        global.localStorage = {
            getItem: (k) => (k in lsStore ? lsStore[k] : null),
            setItem: (k, v) => { lsStore[k] = String(v); },
            removeItem: (k) => { delete lsStore[k]; },
            clear: () => { lsStore = {}; },
        };
        global.fetch = vi.fn(() => Promise.reject(new TypeError('offline')));
        await load();
    });

    // A queued write the server refuses is logged by the sync loop.
    let warnSpy;
    beforeEach(() => { warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {}); });
    afterEach(() => {
        warnSpy.mockRestore();
        if (API && typeof API.clearQueue === 'function') API.clearQueue();
        vi.clearAllTimers();
        vi.useRealTimers();
        vi.resetModules();
        global.fetch = orig.fetch;
        global.EventSource = orig.ES;
        global.localStorage = orig.ls;
        global.BroadcastChannel = orig.BC;
    });

    const isClock = (url) => String(url).includes('/api/time');
    const mockFetch = (impl) => {
        global.fetch = vi.fn((url, opts) => (isClock(url) ? Promise.reject(new TypeError('clock')) : impl(url, opts)));
    };
    const writes = () => global.fetch.mock.calls.filter(([u]) => !isClock(u));
    const bodyOf = (call) => JSON.parse(call[1].body);
    const ok = (body) => Promise.resolve({ ok: true, status: 200, json: async () => body });
    const conflict = (body) => Promise.resolve({ ok: false, status: 409, json: async () => body });
    const refusal = { error: 'lots_replaced', matchId: 'Pool A-TB-0', message: SENTENCE };
    const fought = (extra = {}) => ({ status: 'completed', winner: { id: 'p2', name: 'Bea' }, correctionReason: 'fought it', ...extra });

    it('recordScore sends confirmLots when the patch carries it, and no key otherwise', async () => {
        mockFetch(() => ok({ id: 'm1', status: 'completed' }));
        await (await API.recordScore('c1', 'm1', fought({ confirmLots: true }), 'pw', { id: 'm1' })).settled;
        await (await API.recordScore('c1', 'm2', fought(), 'pw', { id: 'm2' })).settled;

        expect(bodyOf(writes()[0]).confirmLots).toBe(true);
        expect('confirmLots' in bodyOf(writes()[1])).toBe(false);
    });

    it('is held in browser storage with the write, so a reload still sends it', async () => {
        API.recordScore('c1', 'm1', fought({ confirmLots: true }), 'pw', { id: 'm1' });
        for (let i = 0; i < 40; i++) await Promise.resolve();
        const entries = JSON.parse(lsStore[QUEUE_STORAGE_KEY] || '[]');
        expect(entries.length).toBe(1);
        expect(entries[0][1].payload.confirmLots).toBe(true);
    });

    it('the score door parses the refusal onto .lotsReplaced with the server\'s sentence', async () => {
        mockFetch(() => conflict(refusal));
        const res = await API.recordScore('c1', 'm1', fought(), 'pw', { id: 'm1' });
        await expect(res.settled).rejects.toMatchObject({ message: SENTENCE, code: 'lots_replaced', lotsReplaced: { matchId: 'Pool A-TB-0' } });
    });

    it('the decision door parses it too', async () => {
        mockFetch(() => conflict(refusal));
        const res = await API.recordDecision('c1', 'm1', { decision: 'fusensho', decisionBy: 'aka' }, 'pw');
        await expect(res.settled).rejects.toMatchObject({ message: SENTENCE, lotsReplaced: { matchId: 'Pool A-TB-0' } });
    });

    it('the reopen door parses it, and sends confirmLots only when asked', async () => {
        mockFetch(() => conflict(refusal));
        const res = await API.reopenMatch('c1', 'm1', 'pw', { reason: 'fight it again' });
        await expect(res.settled).rejects.toMatchObject({ message: SENTENCE, lotsReplaced: { matchId: 'Pool A-TB-0' } });
        expect('confirmLots' in bodyOf(writes()[0])).toBe(false);

        mockFetch(() => ok({}));
        await (await API.reopenMatch('c1', 'm1', 'pw', { reason: 'fight it again', confirmLots: true })).settled;
        expect(bodyOf(writes()[0]).confirmLots).toBe(true);
    });
});
