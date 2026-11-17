// One sender per base across the tabs of one device (base_sender_lease.jsx).
// Two module instances over one localStorage are two tabs (the ?tab= query,
// vitest.config.js); a write one tab makes reaches the other as a storage
// event, as a browser delivers it. With both tabs open a write is sent once, a
// tab takes a base over when the sender gives it up, and a closed or crashed
// sender does not strand a write.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import {
    createBaseLease, BASE_LEASE_KEY, BASE_LEASE_WAKE_KEY, BASE_LEASE_LIVE_MS, BASE_LEASE_BEAT_MS,
} from '../base_sender_lease.jsx';

const CLIENT = new URL('../api_client.jsx', import.meta.url).pathname;
const URL_LINEUP = '/api/competitions/c1/teams/t1/match-lineups/m1';
async function flushMicrotasks() { for (let i = 0; i < 40; i++) await Promise.resolve(); }
async function tick(ms = 0) { await vi.advanceTimersByTimeAsync(ms); await flushMicrotasks(); }
const answer = (status, body) => ({
    ok: status >= 200 && status < 300, status, headers: { get: () => null },
    json: async () => body, text: async () => JSON.stringify(body),
});

describe('the base lease (one storage, two tabs)', () => {
    let store, clock;
    const storage = {
        getItem: (k) => (k in store ? store[k] : null),
        setItem: (k, v) => { store[k] = String(v); },
        removeItem: (k) => { delete store[k]; },
    };
    const tabOf = (tab) => createBaseLease({ storage, tab, now: () => clock });
    beforeEach(() => { store = {}; clock = 1_000_000; vi.useFakeTimers(); });
    afterEach(() => { vi.clearAllTimers(); vi.useRealTimers(); });

    it('gives a base to one tab at a time and to the other once it is released', () => {
        const a = tabOf('a');
        const b = tabOf('b');
        expect(a.claim('m1')).toBe(true);
        expect(b.claim('m1'), 'a live lease is not taken').toBe(false);
        expect(b.claim('m2'), 'another base is free').toBe(true);
        expect(a.claim('m1'), 'the holder holds it again').toBe(true);
        a.release('m1');
        expect(JSON.parse(store[BASE_LEASE_KEY]), 'only the other base is left').toEqual({ m2: { tab: 'b', at: clock } });
        expect(b.claim('m1')).toBe(true);
    });

    it('takes a base over from a tab whose lease is old, and keeps a holder that beats', () => {
        const a = tabOf('a');
        const b = tabOf('b');
        expect(a.claim('m1')).toBe(true);
        // The holder's timer keeps its lease alive well past the live time.
        clock += BASE_LEASE_LIVE_MS - 1;
        vi.advanceTimersByTime(BASE_LEASE_BEAT_MS);
        clock += BASE_LEASE_LIVE_MS - 1;
        expect(b.claim('m1'), 'a beating holder keeps it').toBe(false);
        // A tab that crashed beats no more: its lease runs out.
        a.release('m1');
        const c = tabOf('c');
        expect(c.claim('m3')).toBe(true);
        vi.clearAllTimers(); // the crash: c beats no more
        clock += BASE_LEASE_LIVE_MS;
        expect(b.claim('m3'), 'an old lease is taken over').toBe(true);
    });

    it('gives every lease up when the page goes', () => {
        const a = tabOf('a');
        a.claim('m1');
        a.claim('m2');
        a.leave();
        expect(store[BASE_LEASE_KEY], 'nothing is left in storage').toBeUndefined();
        expect(tabOf('b').claim('m1')).toBe(true);
    });

    it('sends as before when storage refuses the lease', () => {
        const refusing = createBaseLease({
            storage: { getItem: () => null, setItem: () => { throw new Error('full'); }, removeItem: () => {} },
            tab: 'a',
            now: () => clock,
        });
        expect(refusing.claim('m1')).toBe(true);
        expect(refusing.held()).toEqual([]);
    });

    it('wakes the other tabs only when the turn sent all it held', () => {
        const a = tabOf('a');
        a.claim('m1');
        a.release('m1', false);
        expect(store[BASE_LEASE_WAKE_KEY], 'a turn that ended failing wakes nobody').toBeUndefined();
        a.claim('m1');
        a.release('m1', true);
        const first = store[BASE_LEASE_WAKE_KEY];
        expect(first).toBeDefined();
        a.claim('m1');
        a.release('m1', true);
        expect(store[BASE_LEASE_WAKE_KEY], 'every wake is a change a tab reads').not.toBe(first);
    });
});

describe('two tabs of one device send a write once', () => {
    let orig, store, tabs, calls, held, mode, warned;
    const callerTab = () => {
        const m = new Error().stack.match(/api_client(?:_\w+)?\.jsx\?tab=([^&#:\s)]+)/);
        return m ? m[1] : '?';
    };
    const deliver = (key, oldValue, newValue) => {
        if (oldValue === newValue) return;
        const from = callerTab();
        for (const t of Object.values(tabs)) {
            if (t.name === from) continue;
            const ev = { key, oldValue, newValue };
            setTimeout(() => t.onStorage(ev), 0);
        }
    };
    async function openTab(name) {
        const mod = await import(/* @vite-ignore */ `${CLIENT}?tab=${name}`);
        const onStorage = window.__bcStoredQueueHandler;
        window.removeEventListener('storage', onStorage);
        window.__bcStoredQueueHandler = undefined;
        const onOnline = window.__bcOnlineFlushHandler;
        window.removeEventListener('online', onOnline);
        window.__bcOnlineFlushHandler = undefined;
        tabs[name] = { name, API: mod.API, onStorage, onOnline };
        return mod.API;
    }
    beforeEach(() => {
        vi.useFakeTimers();
        orig = { fetch: global.fetch, ES: global.EventSource, ls: global.localStorage };
        global.EventSource = class FakeES { constructor() { this.close = () => {}; } };
        global.EventSource.OPEN = 1;
        warned = vi.spyOn(console, 'warn').mockImplementation(() => {});
        store = {};
        tabs = {};
        calls = [];
        held = [];
        mode = 'hold';
        global.localStorage = {
            getItem: (k) => (k in store ? store[k] : null),
            setItem: (k, v) => { const o = k in store ? store[k] : null; store[k] = String(v); deliver(k, o, store[k]); },
            removeItem: (k) => { const o = k in store ? store[k] : null; delete store[k]; deliver(k, o, null); },
            clear: () => { store = {}; },
        };
        global.fetch = vi.fn((url) => {
            if (String(url).includes('/api/time')) return Promise.reject(new TypeError('clock'));
            calls.push({ url: String(url), tab: callerTab() });
            if (mode === 'offline') { mode = 'online'; return Promise.reject(new TypeError('offline')); }
            if (mode === 'hold') return new Promise((resolve) => { held.push(() => resolve(answer(200, { applied: true }))); });
            return Promise.resolve(answer(200, { applied: true }));
        });
    });
    afterEach(() => {
        for (const t of Object.values(tabs)) t.API.clearQueue();
        warned.mockRestore();
        vi.clearAllTimers();
        vi.useRealTimers();
        vi.resetModules();
        global.fetch = orig.fetch;
        global.EventSource = orig.ES;
        global.localStorage = orig.ls;
    });

    it('a write tab B took up while tab A has it out is sent by A alone', async () => {
        const A = await openTab('a');
        const B = await openTab('b');
        (await A.putMatchLineup('c1', 't1', 'm1', { 1: 'L1' }, 'pw', null, null)).settled.catch(() => {});
        await tick(100);
        expect(B.unsentWrites().total, 'tab B took it up').toBe(1);
        await tick(3000); // B's backoff fires while A's request is still out
        expect(calls.filter((c) => c.url.includes(URL_LINEUP)).map((c) => c.tab), 'only A sends').toEqual(['a']);
        for (const release of held.splice(0)) release();
        await tick(100);
        await tick(10_000);
        expect(calls.filter((c) => c.url.includes(URL_LINEUP)), 'and it is sent once').toHaveLength(1);
        expect(A.unsentWrites().total).toBe(0);
        expect(B.unsentWrites().total).toBe(0);
        expect(store[BASE_LEASE_KEY], 'A gave the base up').toBeUndefined();
    });

    it('tab B sends what is left when tab A\'s send fails and A gives the base up', async () => {
        mode = 'offline';
        const A = await openTab('a');
        const B = await openTab('b');
        (await A.putMatchLineup('c1', 't1', 'm1', { 1: 'L1' }, 'pw', null, null)).settled.catch(() => {});
        await tick(100);
        await tick(10_000);
        const sent = calls.filter((c) => c.url.includes(URL_LINEUP));
        expect(sent.map((c) => c.tab), 'A tried, then one tab sent it').toHaveLength(2);
        expect(sent[0].tab).toBe('a');
        expect(A.unsentWrites().total).toBe(0);
        expect(B.unsentWrites().total).toBe(0);
        expect(JSON.parse(store.bc_write_queue_v2 || '[]')).toEqual([]);
    });

    it('a court call and its withdrawal are each sent once with two tabs open', async () => {
        const A = await openTab('a');
        const B = await openTab('b');
        const sentCall = await A.sendAnnouncement('Court A, next match', 5, 'pw');
        sentCall.settled.catch(() => {});
        await tick(100); // tab B has taken the call up; A's request is still out
        for (const release of held.splice(0)) release();
        await tick(100);
        const withdrawn = await A.deleteAnnouncement(sentCall.id, 'pw');
        withdrawn.settled.catch(() => {});
        await tick(100);
        for (const release of held.splice(0)) release();
        await tick(100);
        await tick(10_000);
        const announce = calls.filter((c) => c.url.endsWith('/api/tournament/announce'));
        const withdraw = calls.filter((c) => c.url.includes('/announcements/'));
        expect(announce.map((c) => c.tab), 'the call is sent once').toEqual(['a']);
        expect(withdraw.map((c) => c.tab), 'its withdrawal is sent once').toEqual(['a']);
        expect(A.unsentWrites().total + B.unsentWrites().total).toBe(0);
    });
});
