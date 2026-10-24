// bc-evts stage 3 review 3, F4: a tab of an older build open across the upgrade
// shared the outbox's storage key with the new build, read the new build's
// entries (tap events it does not know), rewrote them into its own score writes
// and pruned the shared key on a 4xx, so new taps were sent out of order or lost.
// The outbox now lives under its own key, `bc_write_queue_v2`. What an older
// build left under `bc_write_queue` is taken over once, on load and whenever an
// older tab writes it, and the old key is handed back EMPTY ("[]", not removed:
// an older tab drops every entry of its own that the stored list no longer
// holds, so it stops sending what was taken over, where a removal would only
// make it drop the entries it took up from other tabs).
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

let API;
let _lsStore;
let _orig;

async function load() {
    vi.resetModules();
    API = (await import('../api_client.jsx')).API;
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
const NEW_KEY = 'bc_write_queue_v2';
const OLD_KEY = 'bc_write_queue';
const read = (k) => JSON.parse(_lsStore[k] || '[]');
const answer = (status, body) => Promise.resolve({
    ok: status >= 200 && status < 300, status, headers: { get: () => null },
    json: async () => body, text: async () => JSON.stringify(body),
});
function server() {
    const sent = [];
    global.fetch = vi.fn((url, opts) => {
        if (String(url).includes('/api/time')) return Promise.reject(new TypeError('clock'));
        sent.push({ method: opts.method, url: String(url) });
        return answer(200, { id: 'x', index: 1, name: 'x', modifiedAt: 1 });
    });
    return sent;
}
const reconnect = async () => { window.dispatchEvent(new Event('online')); await tick(50); };

// A queued entry as a previous page left it: built by this module, then moved
// under the old key, as an older build would have stored it.
async function entryLeftUnderOldKey(name) {
    await API.addTeamMember('c1', 't1', name, 'pw');
    const entries = read(NEW_KEY);
    delete _lsStore[NEW_KEY];
    return entries;
}

describe('the outbox\'s own storage key (F4)', () => {
    it('stores a write under bc_write_queue_v2 and never under the key an older build reads', async () => {
        await API.addTeamMember('c1', 't1', 'Ren Abe', 'pw');
        expect(read(NEW_KEY)).toHaveLength(1);
        expect(_lsStore[OLD_KEY]).toBeUndefined();
    });

    it('takes over what an older build left under the old key on load, once, and hands the old key back empty', async () => {
        const left = await entryLeftUnderOldKey('Ren Abe');
        _lsStore[OLD_KEY] = JSON.stringify(left);
        await load();
        expect(read(NEW_KEY).map(([k]) => k)).toEqual(left.map(([k]) => k));
        expect(_lsStore[OLD_KEY], 'handed back empty, so an older tab drops its copy').toBe('[]');

        const sent = server();
        await reconnect();
        expect(sent.map((r) => `${r.method} ${r.url}`)).toEqual(['POST /api/competitions/c1/teams/t1/members']);
        expect(read(NEW_KEY)).toEqual([]);
    });

    it('takes over what an older tab writes under the old key while this page is open, in order', async () => {
        const left = await entryLeftUnderOldKey('Ren Abe');
        await load();
        const before = _lsStore[OLD_KEY] ?? null;
        _lsStore[OLD_KEY] = JSON.stringify(left);
        window.dispatchEvent(Object.assign(new Event('storage'), { key: OLD_KEY, oldValue: before, newValue: _lsStore[OLD_KEY] }));
        await tick(0);
        expect(read(NEW_KEY).map(([k]) => k)).toEqual(left.map(([k]) => k));
        expect(_lsStore[OLD_KEY]).toBe('[]');
    });

    it('never takes over again an entry this tab already took over and sent (G5)', async () => {
        // One module instance only: a second load would leave the first one's
        // listeners and queue running beside it.
        await API.addTeamMember('c1', 't1', 'Ren Abe', 'pw');
        const left = read(NEW_KEY);
        API.clearQueue();
        const sent = server();
        const write = (value) => {
            const before = _lsStore[OLD_KEY] ?? null;
            _lsStore[OLD_KEY] = value;
            window.dispatchEvent(Object.assign(new Event('storage'), { key: OLD_KEY, oldValue: before, newValue: value }));
        };
        write(JSON.stringify(left));
        await tick(0);
        await reconnect();
        expect(sent).toHaveLength(1);
        expect(read(NEW_KEY)).toEqual([]);
        // The older tab, which has not yet read the "[]" handed back, stores
        // its list again with the entry this tab has already sent.
        write(JSON.stringify(left));
        await tick(0);
        await reconnect();
        expect(sent, 'sent once, not again').toHaveLength(1);
        expect(read(NEW_KEY)).toEqual([]);
    });

    it('a storage event that empties the old key takes nothing', async () => {
        await API.addTeamMember('c1', 't1', 'Ren Abe', 'pw');
        window.dispatchEvent(Object.assign(new Event('storage'), { key: OLD_KEY, oldValue: '[]', newValue: null }));
        await tick(0);
        expect(read(NEW_KEY)).toHaveLength(1);
    });

    it('a logout wipe clears both keys', async () => {
        await API.addTeamMember('c1', 't1', 'Ren Abe', 'pw');
        _lsStore[OLD_KEY] = '[]';
        API.clearQueue();
        expect(_lsStore[NEW_KEY]).toBeUndefined();
        expect(_lsStore[OLD_KEY]).toBeUndefined();
    });
});
