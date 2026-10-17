// bc-evts stage 3 review 9. P1: when another tab sends this tab's write and
// the stored queue empties, this tab is told so (the stored list becomes "[]")
// and takes the write off its own queue, so it never sends it again later over
// a newer write; only a wipe (logout's clearQueue) removes the stored key, and
// a tab keeps its own writes through one. P2: a tab whose settled writes are
// still stored because browser storage refused the write that takes them out
// is not idle, so a reload does not send them again.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

const QUEUE_KEY = 'bc_write_queue_v2';
const CLIENT = new URL('../api_client.jsx', import.meta.url).pathname;
async function flushMicrotasks() { for (let i = 0; i < 40; i++) await Promise.resolve(); }
async function tick(ms = 0) { await vi.advanceTimersByTimeAsync(ms); await flushMicrotasks(); }
const answer = (status, body) => Promise.resolve({
    ok: status >= 200 && status < 300, status, headers: { get: () => null },
    json: async () => body, text: async () => JSON.stringify(body),
});

let _orig;
beforeEach(() => {
    vi.useFakeTimers();
    _orig = { fetch: global.fetch, ES: global.EventSource, ls: global.localStorage };
    global.EventSource = class FakeES { constructor() { this.close = () => {}; } };
    global.EventSource.OPEN = 1;
});
afterEach(() => {
    vi.clearAllTimers();
    vi.useRealTimers();
    vi.resetModules();
    global.fetch = _orig.fetch;
    global.EventSource = _orig.ES;
    global.localStorage = _orig.ls;
});

describe('two tabs of one device share the stored outbox (P1)', () => {
    // Two module instances over one localStorage are two tabs. Each tab's
    // storage handlers are taken off the window and called here, so a write
    // one tab makes reaches the other as a storage event, in order, as a
    // browser delivers them; a tab "in the background" gets its events only
    // when it wakes.
    let ls, tabs, asleep, heldForA, online, server, warned;
    // Which tab's code made a call: each instance is imported under its own
    // query string, which shows in the stack.
    const callerTab = () => {
        const m = new Error().stack.match(/api_client\.jsx\?tab=(\w+)/);
        return m ? m[1] : '?';
    };
    const deliver = (key, oldValue, newValue) => {
        if (oldValue === newValue) return;
        const from = callerTab();
        for (const t of Object.values(tabs)) {
            if (t.name === from) continue;
            const ev = { key, oldValue, newValue };
            if (t.name === 'a' && asleep) { heldForA.push(ev); continue; }
            setTimeout(() => t.onStorage(ev), 0);
        }
    };
    async function openTab(name) {
        const mod = await import(/* @vite-ignore */ `${CLIENT}?tab=${name}`);
        const API = mod.API;
        const onStorage = window.__bcStoredQueueHandler;
        window.removeEventListener('storage', onStorage);
        window.__bcStoredQueueHandler = undefined;
        const onOnline = window.__bcOnlineFlushHandler;
        window.removeEventListener('online', onOnline);
        window.__bcOnlineFlushHandler = undefined;
        tabs[name] = { name, API, onStorage, onOnline, subscribeQueueAlert: mod.subscribeQueueAlert };
        return API;
    }
    beforeEach(() => {
        warned = vi.spyOn(console, 'warn').mockImplementation(() => {});
        let store = {};
        tabs = {};
        asleep = false;
        heldForA = [];
        online = false;
        server = {};
        ls = {
            raw: () => store,
            getItem: (k) => (k in store ? store[k] : null),
            setItem: (k, v) => { const o = k in store ? store[k] : null; store[k] = String(v); deliver(k, o, store[k]); },
            removeItem: (k) => { const o = k in store ? store[k] : null; delete store[k]; deliver(k, o, null); },
            clear: () => { store = {}; },
        };
        global.localStorage = ls;
        global.fetch = vi.fn((url, opts = {}) => {
            url = String(url);
            if (url.includes('/api/time')) return Promise.reject(new TypeError('clock'));
            const tab = callerTab();
            if (!online || (tab === 'a' && asleep)) return Promise.reject(new TypeError('offline'));
            const body = opts.body ? JSON.parse(opts.body) : {};
            server[url] = body;
            return answer(200, { ...body, id: body.id || 'mem-1', modifiedAt: 5 });
        });
    });
    afterEach(() => {
        for (const t of Object.values(tabs)) t.API.clearQueue();
        warned.mockRestore();
    });

    async function wakeA() {
        asleep = false;
        for (const ev of heldForA.splice(0)) tabs.a.onStorage(ev);
        await flushMicrotasks();
        tabs.a.onOnline();
        for (let i = 0; i < 10; i++) await tick(2000);
    }

    // The device is offline: tab A queues the first save and goes to the
    // background. The connection returns, tab B sends A's save, then the
    // operator saves again in B and that lands. A wakes.
    async function scenario(first, second) {
        const A = await openTab('a');
        const B = await openTab('b');
        const r1 = await first(A);
        r1.settled.catch(() => {});
        await tick(100);
        asleep = true;
        online = true;
        tabs.b.onOnline();
        for (let i = 0; i < 5; i++) await tick(1000);
        const r2 = await second(B);
        r2.settled.catch(() => {});
        for (let i = 0; i < 5; i++) await tick(1000);
        await wakeA();
        return { A, B };
    }

    it('a match lineup A queued and B sent is not sent again over the later one', async () => {
        const url = '/api/competitions/c1/teams/t1/match-lineups/m1';
        const { A } = await scenario(
            (T) => T.putMatchLineup('c1', 't1', 'm1', { 1: 'L1' }, 'pw', null, null),
            (T) => T.putMatchLineup('c1', 't1', 'm1', { 1: 'L2' }, 'pw', null, null),
        );
        expect(server[url].positions, 'the server keeps the later lineup').toEqual({ 1: 'L2' });
        expect(A.unsentWrites().total, 'tab A holds nothing').toBe(0);
        expect(JSON.parse(ls.getItem(QUEUE_KEY) || '[]'), 'nothing is stored').toEqual([]);
    });

    it('a member rename A queued and B sent is not sent again over the later one', async () => {
        const url = '/api/competitions/c1/teams/t1/members/mem-1';
        const { A } = await scenario(
            (T) => T.renameTeamMember('c1', 't1', 'mem-1', 'Name1', 'pw', {}),
            (T) => T.renameTeamMember('c1', 't1', 'mem-1', 'Name2', 'pw', {}),
        );
        expect(server[url].name, 'the server keeps the later name').toBe('Name2');
        expect(A.unsentWrites().total, 'tab A holds nothing').toBe(0);
    });

    it('a wipe in another tab leaves this tab its own writes, and this tab still announces them', async () => {
        const A = await openTab('a');
        const B = await openTab('b');
        (await A.putMatchLineup('c1', 't1', 'm1', { 1: 'L1' }, 'pw', null, null)).settled.catch(() => {});
        await tick(100);
        expect(B.unsentWrites().total, 'tab B took it up').toBe(1);
        B.clearQueue();
        expect(ls.getItem(QUEUE_KEY), 'a wipe removes the stored queue').toBeNull();
        await tick(100);
        expect(A.unsentWrites().total, 'tab A keeps its own write').toBe(1);
        const alerts = [];
        const off = tabs.a.subscribeQueueAlert((a) => alerts.push(a));
        A.parkQueueForReauth();
        if (typeof off === 'function') off();
        expect(alerts.filter((a) => a.kind === 'auth_required').map((a) => a.count)).toEqual([1]);
    });
});

describe('settled writes storage would not take out keep a reload back (P2)', () => {
    let API, store, refuseSet;
    beforeEach(async () => {
        store = {};
        refuseSet = false;
        global.localStorage = {
            getItem: (k) => (k in store ? store[k] : null),
            setItem: (k, v) => {
                if (refuseSet) throw new DOMException('full', 'QuotaExceededError');
                store[k] = String(v);
            },
            removeItem: (k) => { delete store[k]; },
            clear: () => { store = {}; },
        };
        global.fetch = vi.fn(() => Promise.reject(new TypeError('offline')));
        vi.resetModules();
        API = (await import('../api_client.jsx')).API;
    });
    afterEach(() => { refuseSet = false; API.clearQueue(); });

    it('is not idle while a write it settled is still stored, and is once storage takes it out', async () => {
        const { outboxIsIdle } = await import('../app_version.jsx');
        const mine = await API.putMatchLineup('c1', 't1', 'm1', { 1: 'L1' }, 'pw', null, null);
        mine.settled.catch(() => {});
        await flushMicrotasks();
        const [[mineKey, mineEntry]] = JSON.parse(store[QUEUE_KEY]);
        // Another tab's entry this tab has not taken up yet (its event is
        // still to come).
        const theirs = ['lineup:c1:t9:0\u001fother.1', { ...mineEntry, url: '/api/competitions/c1/teams/t9/match-lineups/m9', enqueuedAt: mineEntry.enqueuedAt + 1 }];
        store[QUEUE_KEY] = JSON.stringify([[mineKey, mineEntry], theirs]);
        refuseSet = true;
        global.fetch = vi.fn((url) => (String(url).includes('/api/time')
            ? Promise.reject(new TypeError('clock'))
            : answer(200, {})));
        window.dispatchEvent(new Event('online'));
        await tick(100);
        await expect(mine.settled).resolves.toBeTruthy();
        expect(API.unsentWrites().total, 'this tab holds nothing').toBe(0);
        expect(JSON.parse(store[QUEUE_KEY]).map(([k]) => k), 'storage refused taking it out').toContain(mineKey);
        expect(API.persistQueue(), 'not stored as it should be').toBe(false);
        expect(outboxIsIdle(), 'a reload would send the settled write again').toBe(false);
        refuseSet = false;
        expect(API.persistQueue()).toBe(true);
        expect(JSON.parse(store[QUEUE_KEY]).map(([k]) => k)).toEqual([theirs[0]]);
        expect(outboxIsIdle()).toBe(true);
    });

    it('a write it settled that was never stored keeps an empty outbox idle (L2)', async () => {
        const { outboxIsIdle } = await import('../app_version.jsx');
        refuseSet = true;
        const mine = await API.putMatchLineup('c1', 't1', 'm1', { 1: 'L1' }, 'pw', null, null);
        mine.settled.catch(() => {});
        global.fetch = vi.fn((url) => (String(url).includes('/api/time')
            ? Promise.reject(new TypeError('clock'))
            : answer(200, {})));
        window.dispatchEvent(new Event('online'));
        await tick(100);
        await expect(mine.settled).resolves.toBeTruthy();
        expect(API.persistQueue(), 'nothing of this tab\'s is stored').toBe(true);
        expect(outboxIsIdle()).toBe(true);
    });
});
