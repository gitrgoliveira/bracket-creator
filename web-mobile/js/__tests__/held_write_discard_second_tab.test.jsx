// A write the server keeps refusing can be discarded from the held-writes list
// of ANY tab of the device, not only the tab that made it: tab B takes A's write
// up from storage, sees it failing, discards it, and tab A then holds nothing and
// never sends it again.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

const QUEUE_KEY = 'bc_write_queue_v2';
const CLIENT = new URL('../api_client.jsx', import.meta.url).pathname;
async function flushMicrotasks() { for (let i = 0; i < 40; i++) await Promise.resolve(); }
async function tick(ms = 0) { await vi.advanceTimersByTimeAsync(ms); await flushMicrotasks(); }
const answer = (status, body) => Promise.resolve({
    ok: status >= 200 && status < 300, status, headers: { get: () => null },
    json: async () => body, text: async () => JSON.stringify(body),
});

describe('discarding a refused held write from a second tab', () => {
    let orig, store, tabs, sent, warned;
    // Which tab's code made a call: each instance is imported under its own
    // query string, which shows in the stack.
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
        sent = 0;
        global.localStorage = {
            getItem: (k) => (k in store ? store[k] : null),
            setItem: (k, v) => { const o = k in store ? store[k] : null; store[k] = String(v); deliver(k, o, store[k]); },
            removeItem: (k) => { const o = k in store ? store[k] : null; delete store[k]; deliver(k, o, null); },
            clear: () => { store = {}; },
        };
        global.fetch = vi.fn((url) => {
            if (String(url).includes('/api/time')) return Promise.reject(new TypeError('clock'));
            sent++;
            return answer(500, { error: 'boom' });
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

    it('takes the write out of both tabs and it is never sent again', async () => {
        const A = await openTab('a');
        const B = await openTab('b');
        (await A.putMatchLineup('c1', 't1', 'm1', { 1: 'L1' }, 'pw', null, null)).settled.catch(() => {});
        await tick(100);
        expect(B.unsentWrites().total, 'tab B took it up').toBe(1);
        for (let i = 0; i < 40; i++) await tick(2000); // well past the server-error notice threshold

        const held = B.heldWrites();
        expect(held, 'the second tab lists it').toHaveLength(1);
        expect(held[0].keepsFailing, 'and sees it refused').toBe(true);

        expect(B.discardHeldWrite(held[0].key)).toBe(true);
        expect(B.unsentWrites().total).toBe(0);
        await tick(100); // the storage event reaches tab A
        expect(A.unsentWrites().total, 'tab A holds nothing either').toBe(0);
        expect(JSON.parse(store[QUEUE_KEY] || '[]'), 'nothing is stored').toEqual([]);

        const before = sent;
        for (let i = 0; i < 10; i++) await tick(2000);
        expect(sent, 'no tab sends it again').toBe(before);
    });
});
