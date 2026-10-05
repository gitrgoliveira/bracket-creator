// bc-mrgc: the groups a score write names (`changed`) survive the offline
// write queue, and a write that takes the place of a queued one for the same
// match claims every group either changed.
//
// The server applies only the groups a write names and keeps every other
// group as stored, so a queue that dropped `changed` would turn a write into
// "every group" (the old whole-match overwrite), and a queue that coalesced
// two writes but kept only the later one's list would never apply the
// earlier one's change: the later write carries the later full state, but
// names only what IT changed.
//
// Setup mirrors sync_queue.test.jsx (functional localStorage, EventSource
// stub, every fetch mock wrapped so the background clock poll never reaches it).

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

let API;
let _origFetch;
let _origEventSource;
let _origLocalStorage;
let _lsStore;

beforeEach(async () => {
    vi.useFakeTimers();
    vi.resetModules();
    _origEventSource = global.EventSource;
    global.EventSource = class FakeES { constructor() { this.close = () => {}; } };
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
    ({ API } = await import('../api_client.jsx'));
});

afterEach(() => {
    vi.useRealTimers();
    vi.resetModules();
    global.fetch = _origFetch;
    if (_origEventSource === undefined) delete global.EventSource;
    else global.EventSource = _origEventSource;
    if (_origLocalStorage === undefined) delete global.localStorage;
    else global.localStorage = _origLocalStorage;
});

async function flushMicrotasks() {
    for (let i = 0; i < 12; i++) await Promise.resolve();
}
async function tick(ms = 0) {
    await vi.advanceTimersByTimeAsync(ms);
    await flushMicrotasks();
}

const isClockPoll = (url) => String(url).includes('/api/time');
// Every score body sent, in order. `online` decides whether a send fails.
let sent;
let online;
function installFetch() {
    sent = [];
    global.fetch = vi.fn((url, opts) => {
        if (isClockPoll(url)) return Promise.reject(new TypeError('clock poll: not part of this test'));
        if (!online) return Promise.reject(new TypeError('offline'));
        sent.push(JSON.parse(opts.body));
        return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({}), text: () => Promise.resolve('{}') });
    });
}

const storedEntries = () => JSON.parse(_lsStore.bc_write_queue || '[]').map(([, d]) => d);

describe('`changed` survives the queue', () => {
    it('persisted, rehydrated after a reload, and replayed', async () => {
        online = false;
        installFetch();
        await API.recordScore('c1', 'm1', { status: 'running', ipponsA: ['M'], ipponsB: [], changed: ['points'] }, 'pw', null);
        expect(storedEntries()).toHaveLength(1);
        expect(storedEntries()[0].payload.changed).toEqual(['points']);

        // A reload: the module comes back up from storage alone.
        vi.resetModules();
        online = true;
        installFetch();
        await import('../api_client.jsx');
        await tick(0);
        const replayed = sent.filter((b) => b.status === 'running');
        expect(replayed).toHaveLength(1);
        expect(replayed[0].changed).toEqual(['points']);
    });
});

describe('a write that replaces a queued one claims both writes\' groups', () => {
    it('two queued running writes: the survivor names the union', async () => {
        online = false;
        installFetch();
        await API.recordScore('c1', 'm1', { status: 'running', ipponsA: ['M'], ipponsB: [], changed: ['points'] }, 'pw', null);
        await API.recordScore('c1', 'm1', { status: 'running', ipponsA: ['M'], ipponsB: [], encho: { periodCount: 1 }, changed: ['encho'] }, 'pw', null);
        expect(storedEntries()).toHaveLength(1);
        expect(storedEntries()[0].payload.changed).toEqual(['points', 'encho']);

        online = true;
        await tick(5000);
        expect(sent.at(-1).changed).toEqual(['points', 'encho']);
    });

    it('a direct running write sent past a queued one names the queued one\'s groups too', async () => {
        online = false;
        installFetch();
        await API.recordScore('c1', 'm1', { status: 'running', ipponsA: ['M'], ipponsB: [], changed: ['points'] }, 'pw', null);
        online = true;
        await API.recordScore('c1', 'm1', { status: 'running', ipponsA: ['M'], ipponsB: [], encho: { periodCount: 1 }, changed: ['encho'] }, 'pw', null);
        const direct = sent.find((b) => b.rev === 2);
        expect(direct).toBeTruthy();
        expect(direct.changed).toEqual(['points', 'encho']);
    });

    it('a Finish sent past a queued autosave names the autosave\'s groups too', async () => {
        online = false;
        installFetch();
        await API.recordScore('c1', 'm1', { status: 'running', ipponsA: ['M'], ipponsB: [], changed: ['points'] }, 'pw', null);
        online = true;
        await API.recordScore('c1', 'm1', { status: 'completed', winner: 'A', ipponsA: ['M'], ipponsB: [], changed: ['result'] }, 'pw', null);
        const finish = sent.find((b) => b.status === 'completed');
        expect(finish.changed).toEqual(['points', 'result']);
    });

    it('a Finish queued over a queued autosave names both', async () => {
        online = false;
        installFetch();
        await API.recordScore('c1', 'm1', { status: 'running', ipponsA: ['M'], ipponsB: [], changed: ['points'] }, 'pw', null);
        await API.recordScore('c1', 'm1', { status: 'completed', winner: 'A', ipponsA: ['M'], ipponsB: [], changed: ['result'] }, 'pw', null);
        const entries = storedEntries();
        expect(entries).toHaveLength(1);
        expect(entries[0].terminal).toBe(true);
        expect(entries[0].payload.changed).toEqual(['points', 'result']);
    });

    it('enqueueRunningWrite over a queued write names the union', async () => {
        online = false;
        installFetch();
        const mod = await import('../api_client.jsx');
        mod.enqueueRunningWrite('c1', 'm1', { status: 'running', ipponsA: ['M'], changed: ['points'] }, 'pw');
        await flushMicrotasks();
        mod.enqueueRunningWrite('c1', 'm1', { status: 'running', ipponsA: ['M'], changed: ['bout:2'] }, 'pw');
        expect(storedEntries()[0].payload.changed).toEqual(['points', 'bout:2']);
    });

    it('a Finish whose send fails after another write was queued meanwhile names both', async () => {
        // The Finish is in flight when an autosave from another editor for the
        // same match fails and is queued; the Finish then fails too and takes
        // that entry's place in the queue.
        installFetch();
        const mod = await import('../api_client.jsx');
        let failFinish;
        global.fetch = vi.fn((url, opts) => {
            if (isClockPoll(url)) return Promise.reject(new TypeError('clock poll: not part of this test'));
            const body = JSON.parse(opts.body);
            if (body.status === 'completed') return new Promise((_res, rej) => { failFinish = () => rej(new TypeError('offline')); });
            return Promise.reject(new TypeError('offline'));
        });
        const finishing = API.recordScore('c1', 'm1', { status: 'completed', winner: 'A', ipponsA: ['M'], ipponsB: [], changed: ['result'] }, 'pw', null);
        await flushMicrotasks();
        mod.enqueueRunningWrite('c1', 'm1', { status: 'running', ipponsA: ['M'], encho: { periodCount: 1 }, changed: ['encho'] }, 'pw');
        await flushMicrotasks();
        failFinish();
        await finishing;
        const entries = storedEntries();
        expect(entries).toHaveLength(1);
        expect(entries[0].terminal).toBe(true);
        expect(entries[0].payload.changed).toEqual(['encho', 'result']);
    });

    it('a queued write that named no groups ("every group") makes the survivor name none', async () => {
        online = false;
        installFetch();
        await API.recordScore('c1', 'm1', { status: 'running', ipponsA: ['M'], ipponsB: [] }, 'pw', null);
        await API.recordScore('c1', 'm1', { status: 'running', ipponsA: ['M'], ipponsB: [], changed: ['encho'] }, 'pw', null);
        expect('changed' in storedEntries()[0].payload).toBe(false);
    });
});
