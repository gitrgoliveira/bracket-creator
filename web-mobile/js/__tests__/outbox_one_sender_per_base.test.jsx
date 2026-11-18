// One sender per base across the tabs of one device (base_sender_lease.jsx).
// Two module instances over one localStorage are two tabs (the ?tab= query,
// vitest.config.js); a write one tab makes reaches the other as a storage
// event, as a browser delivers it. With both tabs open a write is sent once, a
// tab takes a base over when the sender gives it up, and a closed or crashed
// sender does not strand a write. Both ways a lease is held are run: Web Locks
// (a navigator.locks stand-in) and the timed note kept for a browser without it.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import {
    createBaseLease, detectWebLocks, baseLeaseKey, BASE_LEASE_PREFIX, BASE_LEASE_WAKE_KEY, BASE_LEASE_LIVE_MS, BASE_LEASE_BEAT_MS,
} from '../base_sender_lease.jsx';

const CLIENT = new URL('../api_client.jsx', import.meta.url).pathname;
const FLUSH = new URL('../api_client_flush.jsx', import.meta.url).pathname;
const STORE = new URL('../api_client_store.jsx', import.meta.url).pathname;
const URL_LINEUP = '/api/competitions/c1/teams/t1/match-lineups/m1';
async function flushMicrotasks() { for (let i = 0; i < 40; i++) await Promise.resolve(); }
async function tick(ms = 0) { await vi.advanceTimersByTimeAsync(ms); await flushMicrotasks(); }
const answer = (status, body) => ({
    ok: status >= 200 && status < 300, status, headers: { get: () => null },
    json: async () => body, text: async () => JSON.stringify(body),
});

// A stand-in for navigator.locks, shared by every "tab" that is handed it: a
// request is granted in a later microtask, `ifAvailable` answers null when the
// lock is taken, and a queued request is granted when the holder's callback
// promise settles (as a closed tab's locks are given up).
function fakeLocks() {
    const taken = new Set();
    const waiting = new Map();
    const pump = (name) => {
        const queue = waiting.get(name) || [];
        if (!taken.has(name) && queue.length) queue.shift()();
    };
    return {
        request(name, opts, callback) {
            if (typeof opts === 'function') { callback = opts; opts = {}; }
            return new Promise((resolve, reject) => {
                const start = (lock) => {
                    if (lock) taken.add(name);
                    const end = () => { if (lock) { taken.delete(name); pump(name); } };
                    Promise.resolve().then(() => callback(lock)).then(
                        (v) => { end(); resolve(v); },
                        (e) => { end(); reject(e); },
                    );
                };
                if (!taken.has(name)) return start({ name });
                if (opts.ifAvailable) return start(null);
                if (!waiting.has(name)) waiting.set(name, []);
                waiting.get(name).push(() => start({ name }));
                return undefined;
            });
        },
    };
}
const setLocks = (locks) => Object.defineProperty(navigator, 'locks', { value: locks, configurable: true });

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

    it('gives a base to one tab at a time and to the other once it is released', async () => {
        const a = tabOf('a');
        const b = tabOf('b');
        expect(await a.claim('m1')).toBe(true);
        expect(await b.claim('m1'), 'a live lease is not taken').toBe(false);
        expect(await b.claim('m2'), 'another base is free').toBe(true);
        expect(await a.claim('m1'), 'the holder holds it again').toBe(true);
        a.release('m1');
        expect(store[baseLeaseKey('m1')], 'm1 is given up').toBeUndefined();
        expect(JSON.parse(store[baseLeaseKey('m2')]), 'the other base is left').toEqual({ tab: 'b', at: clock });
        expect(await b.claim('m1')).toBe(true);
    });

    it('takes a base over from a tab whose lease is old, and keeps a holder that beats', async () => {
        const a = tabOf('a');
        const b = tabOf('b');
        expect(await a.claim('m1')).toBe(true);
        // The holder's timer keeps its lease alive well past the live time.
        clock += BASE_LEASE_LIVE_MS - 1;
        vi.advanceTimersByTime(BASE_LEASE_BEAT_MS);
        clock += BASE_LEASE_LIVE_MS - 1;
        expect(await b.claim('m1'), 'a beating holder keeps it').toBe(false);
        // A tab that crashed beats no more: its lease runs out.
        a.release('m1');
        const c = tabOf('c');
        expect(await c.claim('m3')).toBe(true);
        vi.clearAllTimers(); // the crash: c beats no more
        clock += BASE_LEASE_LIVE_MS;
        expect(await b.claim('m3'), 'an old lease is taken over').toBe(true);
    });

    it('keeps a lease the holder renews though its timer was slowed past the lease life', async () => {
        const a = tabOf('a');
        const b = tabOf('b');
        a.claim('m1');
        vi.clearAllTimers(); // a hidden page's timers run once a minute: no beat comes in time
        clock += BASE_LEASE_LIVE_MS - 1;
        a.renew(); // before the next entry it sends
        clock += BASE_LEASE_LIVE_MS - 1;
        expect(await b.claim('m1'), 'a renewed lease is not taken').toBe(false);
        clock += BASE_LEASE_LIVE_MS;
        expect(await b.claim('m1'), 'one nobody renews is').toBe(true);
    });

    it('lets a holder renew a lease that ran out during a slow request while no other tab took it', async () => {
        const a = tabOf('a');
        const b = tabOf('b');
        a.claim('m1');
        vi.clearAllTimers();
        clock += BASE_LEASE_LIVE_MS + 1_000; // a request under way past the lease life
        a.renew();
        expect(JSON.parse(store[baseLeaseKey('m1')]), 'the late holder renewed').toEqual({ tab: 'a', at: clock });
        expect(await b.claim('m1'), 'so another tab is still turned away').toBe(false);
    });

    it('does not renew a lease another tab took over, and leaves the other bases alone', async () => {
        const a = tabOf('a');
        const b = tabOf('b');
        const c = tabOf('c');
        a.claim('m1');
        c.claim('m2');
        vi.clearAllTimers();
        clock += BASE_LEASE_LIVE_MS;
        expect(await b.claim('m1'), 'a was late and b took the base').toBe(true);
        a.renew();
        expect(JSON.parse(store[baseLeaseKey('m1')]), 'a leaves b\'s lease alone').toEqual({ tab: 'b', at: clock });
        expect(JSON.parse(store[baseLeaseKey('m2')]), 'and c\'s record is its own').toEqual({ tab: 'c', at: clock - BASE_LEASE_LIVE_MS });
    });

    it('keeps each base in a record of its own, so a write for one base never takes another back', async () => {
        const a = tabOf('a');
        const b = tabOf('b');
        // b's write lands between a's read and write of the SAME item, were the
        // leases one record: with one record each, a's claim cannot touch b's.
        const racing = {
            ...storage,
            setItem: (k, v) => { storage.setItem(k, v); if (k === baseLeaseKey('m1')) b.claim('m2'); },
        };
        const a2 = createBaseLease({ storage: racing, tab: 'a', now: () => clock });
        expect(await a2.claim('m1')).toBe(true);
        expect(JSON.parse(store[baseLeaseKey('m2')]).tab, 'b keeps its base').toBe('b');
        expect(await a.claim('m2'), 'so a is still turned away from it').toBe(false);
    });

    it('gives every lease up when the page goes', async () => {
        const a = tabOf('a');
        a.claim('m1');
        a.claim('m2');
        a.leave();
        expect(Object.keys(store).filter((k) => k.startsWith(BASE_LEASE_PREFIX)), 'nothing is left in storage').toEqual([]);
        expect(await tabOf('b').claim('m1')).toBe(true);
    });

    it('sends as before when storage refuses the lease', async () => {
        const refusing = createBaseLease({
            storage: { getItem: () => null, setItem: () => { throw new Error('full'); }, removeItem: () => {} },
            tab: 'a',
            now: () => clock,
        });
        expect(await refusing.claim('m1')).toBe(true);
        expect(refusing.held()).toEqual([]);
    });

    it('wakes the other tabs only when the turn sent all it held', async () => {
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

describe('the base lease held with Web Locks', () => {
    let locks;
    const tabOf = (tab) => createBaseLease({ storage: null, tab, locks });
    beforeEach(() => { locks = fakeLocks(); });

    it('gives a base to one tab at a time and to the other once it is released', async () => {
        const a = tabOf('a');
        const b = tabOf('b');
        expect(await a.claim('m1')).toBe(true);
        expect(await b.claim('m1'), 'a held lock is not taken').toBe(false);
        expect(await b.claim('m2'), 'another base is free').toBe(true);
        expect(await a.claim('m1'), 'the holder holds it again').toBe(true);
        expect(a.held()).toEqual(['m1']);
        a.release('m1');
        expect(a.held()).toEqual([]);
        for (let i = 0; i < 10; i++) await Promise.resolve(); // the lock manager lets it go
        expect(await b.claim('m1')).toBe(true);
    });

    it('lets the same tab claim a base again right after it released it', async () => {
        const a = tabOf('a');
        expect(await a.claim('m1')).toBe(true);
        a.release('m1', true);
        expect(await a.claim('m1'), 'the release has landed, so the lock is free').toBe(true);
    });

    it('tells a tab that was turned away the moment the lock frees, and gives every lock up when the page goes', async () => {
        const a = tabOf('a');
        const b = tabOf('b');
        await a.claim('m1');
        await a.claim('m2');
        let freed = '';
        b.whenFree('m1').then(() => { freed += 'm1'; });
        await Promise.resolve(); await Promise.resolve();
        expect(freed, 'nothing yet: a holds it').toBe('');
        a.leave();
        for (let i = 0; i < 10; i++) await Promise.resolve();
        expect(freed, 'the holder is gone').toBe('m1');
        expect(a.held()).toEqual([]);
        expect(await b.claim('m2'), 'and so is its other lock').toBe(true);
    });

    it('sends as before when the page refuses locks', async () => {
        const refusing = createBaseLease({
            storage: null, tab: 'a',
            locks: { request: () => Promise.reject(new DOMException('opaque origin', 'SecurityError')) },
        });
        expect(await refusing.claim('m1')).toBe(true);
        expect(refusing.held()).toEqual([]);
        await expect(refusing.whenFree('m1')).resolves.toBeUndefined();
    });

    it('has nothing to renew, and the note lease is not used', async () => {
        const a = tabOf('a');
        await a.claim('m1');
        expect(() => a.renew()).not.toThrow();
        expect(Object.keys(localStorage).filter((k) => k.startsWith(BASE_LEASE_PREFIX))).toEqual([]);
    });
});

describe('which way the lease is held', () => {
    afterEach(() => { delete navigator.locks; });

    it('uses navigator.locks when the page has it, found by feature and not by browser', () => {
        expect(detectWebLocks(), 'jsdom has none: the timed note').toBeNull();
        const locks = fakeLocks();
        setLocks(locks);
        expect(detectWebLocks()).toBe(locks);
        setLocks({});
        expect(detectWebLocks(), 'an object that cannot request is not Web Locks').toBeNull();
    });

    it('holds with the timed note when no locks are handed to it', async () => {
        const store = {};
        const storage = { getItem: (k) => store[k] ?? null, setItem: (k, v) => { store[k] = String(v); }, removeItem: (k) => { delete store[k]; } };
        const lease = createBaseLease({ storage, tab: 'a' });
        expect(await lease.claim('m1')).toBe(true);
        expect(JSON.parse(store[baseLeaseKey('m1')]).tab).toBe('a');
        lease.leave();
    });
});

// The timed note is a fallback: one test below is Web Locks only (a closed tab's lock is given up at once).
describe.each([['the timed note', false], ['Web Locks', true]])('two tabs of one device send a write once (%s)', (_way, useLocks) => {
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
        if (useLocks) setLocks(fakeLocks());
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
            if (mode === 'fail-m2' && String(url).includes('/match-lineups/m2')) return Promise.resolve(answer(500, { error: 'boom' }));
            if (mode === 'fail-m2') return Promise.resolve(answer(200, { applied: true }));
            if (mode === 'refuse') return Promise.resolve(answer(400, { error: 'nope', reasonHuman: 'The lineup was refused.' }));
            if (mode === 'hold') return new Promise((resolve) => { held.push(() => resolve(answer(200, { applied: true }))); });
            return Promise.resolve(answer(200, { applied: true }));
        });
    });
    afterEach(() => {
        delete navigator.locks;
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
        expect(store[baseLeaseKey('c1:m1')], 'A gave the base up').toBeUndefined();
    });

    it('a pass that only leaves bases to another tab keeps the offline flag as it was', async () => {
        const A = await openTab('a');
        await openTab('b');
        const flushB = await import(/* @vite-ignore */ `${FLUSH}?tab=b`);
        flushB._setOffline(true); // tab B's last real pass found the connection down
        (await A.putMatchLineup('c1', 't1', 'm1', { 1: 'L1' }, 'pw', null, null)).settled.catch(() => {});
        await tick(100); // B took the write up and left it to A, who has it out
        await tick(3000); // B asks again at its backoff and finds A still sending
        expect(calls.filter((c) => c.url.includes(URL_LINEUP)).map((c) => c.tab), 'only A sent').toEqual(['a']);
        const storeB = await import(/* @vite-ignore */ `${STORE}?tab=b`);
        expect(storeB.store.offlineFlag, 'B learned nothing about the connection').toBe(true);
    });

    it('a pass that only waited for another tab does not climb the retry backoff', async () => {
        const A = await openTab('a');
        await openTab('b');
        const flushB = await import(/* @vite-ignore */ `${FLUSH}?tab=b`);
        (await A.putMatchLineup('c1', 't1', 'm1', { 1: 'L1' }, 'pw', null, null)).settled.catch(() => {});
        await tick(100); // B took the write up and left it to A, who has it out
        await tick(3000); // B asks again at 0.5 s and 1 s and finds A still sending (three steps once climbed)
        expect(calls.filter((c) => c.url.includes(URL_LINEUP)).map((c) => c.tab), 'only A sent').toEqual(['a']);
        expect(flushB._flushAttempt, 'waiting is not a failure').toBe(0);
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

    it('Finish + Start Next: the next match\'s start, which follows the finish, is sent once with two tabs open', async () => {
        mode = 'online';
        const A = await openTab('a');
        await openTab('b');
        const finish = await A.recordScore('c1', 'm1', { status: 'completed', ipponsA: ['M'], ipponsB: [], winner: 'A' }, 'pw', { id: 'm1', modifiedAt: 0 });
        finish.settled.catch(() => {});
        const start = await A.recordScore('c1', 'm2', { status: 'running', ipponsA: [], ipponsB: [], after: finish.key }, 'pw', { id: 'm2', modifiedAt: 0 });
        start.settled.catch(() => {});
        await tick(100);
        await tick(10_000);
        const url = (m) => `/api/competitions/c1/matches/${m}/score`;
        const sentTo = (m) => calls.filter((c) => c.url.includes(url(m))).map((c) => c.tab);
        expect(sentTo('m1'), 'the finish is sent once').toHaveLength(1);
        expect(sentTo('m2'), 'the start that follows it is sent once').toHaveLength(1);
        expect(A.unsentWrites().total).toBe(0);
    });

    it('a tab opened while a write is out sends nothing of it, on load or after', async () => {
        const A = await openTab('a');
        (await A.putMatchLineup('c1', 't1', 'm1', { 1: 'L1' }, 'pw', null, null)).settled.catch(() => {});
        await tick(100); // A has its request out
        const C = await openTab('c'); // a page loaded now rehydrates what storage holds
        await tick(100);
        await tick(10_000);
        expect(C.unsentWrites().total, 'tab C holds it').toBe(1);
        expect(calls.filter((c) => c.url.includes(URL_LINEUP)).map((c) => c.tab), 'only A sends').toEqual(['a']);
        for (const release of held.splice(0)) release();
        await tick(100);
        await tick(10_000);
        expect(calls.filter((c) => c.url.includes(URL_LINEUP)), 'and it is sent once').toHaveLength(1);
        expect(C.unsentWrites().total).toBe(0);
    });

    it('the answer of a write another tab sent reaches the tab that made it', async () => {
        mode = 'refuse';
        const A = await openTab('a');
        await openTab('b');
        const flushB = await import(/* @vite-ignore */ `${FLUSH}?tab=b`);
        expect(await flushB._baseLease.claim('c1:m1'), 'tab B is sending the match').toBe(true);
        const put = await A.putMatchLineup('c1', 't1', 'm1', { 1: 'L1' }, 'pw', null, null);
        let told;
        put.settled.then((v) => { told = { value: v }; }, (e) => { told = { error: e }; });
        await tick(100); // A found the base leased and left it to B
        await tick(3000);
        expect(calls.filter((c) => c.url.includes(URL_LINEUP)).map((c) => c.tab), 'B sent it, once').toEqual(['b']);
        expect(told && told.error, 'the maker is told the refusal, not that it was discarded').toBeInstanceOf(Error);
        expect(told.error.message, 'with the refusal the server gave').toBe('nope');
        expect(A.unsentWrites().total).toBe(0);
    });

    it('a base turned away for its lease starts as soon as the holder wakes it, behind another base\'s hanging request', async () => {
        const A = await openTab('a');
        const B = await openTab('b');
        const flushB = await import(/* @vite-ignore */ `${FLUSH}?tab=b`);
        expect(await flushB._baseLease.claim('c1:m1')).toBe(true); // B sends m1 ...
        const m1 = await A.putMatchLineup('c1', 't1', 'm1', { 1: 'L1' }, 'pw', null, null);
        m1.settled.catch(() => {});
        // ... and A has a hanging request on another base in the same pass.
        const m2 = await A.putMatchLineup('c1', 't1', 'm2', { 1: 'L2' }, 'pw', null, null);
        m2.settled.catch(() => {});
        await tick(100);
        expect(calls.filter((c) => c.url.includes('/match-lineups/m2')).map((c) => c.tab), 'A has m2 out').toEqual(['a']);
        mode = 'online';
        flushB._baseLease.release('c1:m1', true); // B gives m1 up and wakes the others
        await tick(100); // A starts m1 now, in this pass, with m2 still hanging
        expect(calls.filter((c) => c.url.includes('/match-lineups/m1')).length, 'm1 is sent while m2 hangs').toBeGreaterThan(0);
        for (const release of held.splice(0)) release();
        await tick(100);
        expect(B.unsentWrites().total + A.unsentWrites().total).toBe(0);
    });

    it('a wake from another tab does not send this tab\'s failing base again before its backoff', async () => {
        mode = 'fail-m2';
        const B = await openTab('b');
        (await B.putMatchLineup('c1', 't1', 'm2', { 1: 'L2' }, 'pw', null, null)).settled.catch(() => {});
        await tick(100);
        const sentM2 = () => calls.filter((c) => c.url.includes('/match-lineups/m2')).length;
        expect(sentM2(), 'sent once, refused with a 5xx').toBe(1);
        // Another tab finished a base: its wake reaches this tab.
        tabs.b.onStorage({ key: BASE_LEASE_WAKE_KEY, oldValue: null, newValue: 'x:1' });
        await tick(100); // well inside the first backoff step (500 ms)
        expect(sentM2(), 'the failing base waits for its backoff').toBe(1);
        await tick(1000);
        expect(sentM2(), 'and is retried on it').toBeGreaterThan(1);
    });

    it('a wake sends the base this tab was turned away from, and leaves a failing base to its backoff', async () => {
        mode = 'fail-m2';
        const B = await openTab('b');
        const other = createBaseLease({ storage: localStorage, tab: 'z', locks: useLocks ? navigator.locks : null });
        expect(await other.claim('c1:m1'), 'another tab is sending m1').toBe(true);
        (await B.putMatchLineup('c1', 't1', 'm1', { 1: 'L1' }, 'pw', null, null)).settled.catch(() => {});
        await tick(20); // m1's pass is over, left to its holder, before m2 is made
        (await B.putMatchLineup('c1', 't1', 'm2', { 1: 'L2' }, 'pw', null, null)).settled.catch(() => {});
        await tick(100);
        const sentTo = (m) => calls.filter((c) => c.url.includes(`/match-lineups/${m}`)).length;
        expect(sentTo('m1'), 'm1 is left to its holder').toBe(0);
        expect(sentTo('m2'), 'm2 failed once').toBe(1);
        other.release('c1:m1', true); // the holder's turn ended with everything sent
        await tick(100);
        expect(sentTo('m1'), 'm1 goes at once').toBe(1);
        expect(sentTo('m2'), 'm2 keeps its backoff').toBe(1);
    });
    if (useLocks) it('a tab that closes hands its base to the other at once, not after a lease life', async () => {
        const A = await openTab('a');
        await openTab('b');
        const flushA = await import(/* @vite-ignore */ `${FLUSH}?tab=a`);
        (await A.putMatchLineup('c1', 't1', 'm1', { 1: 'L1' }, 'pw', null, null)).settled.catch(() => {});
        await tick(500); // B took the write up and was turned away; A's request is still out
        expect(calls.filter((c) => c.url.includes(URL_LINEUP)).map((c) => c.tab)).toEqual(['a']);
        flushA._baseLease.leave(); // the page goes: the browser gives its locks up
        await tick(100); // far less than BASE_LEASE_LIVE_MS
        expect(calls.filter((c) => c.url.includes(URL_LINEUP)).map((c) => c.tab), 'B sends it now').toEqual(['a', 'b']);
    });
});
