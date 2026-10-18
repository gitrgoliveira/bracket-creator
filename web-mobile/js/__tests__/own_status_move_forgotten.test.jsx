// Stage 6 review 9 (F7): a status move the server answered is kept only while
// it can still say something about the match on screen. It goes when the
// draw is generated or discarded (the new draw reuses the match ids), when
// the court's feed no longer holds the match, and a feed copy past the
// answer's score version is newer whatever its stamp.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

let API;
let _lsStore;
let _orig;
let es;

async function load() {
    vi.resetModules();
    const mod = await import('../api_client.jsx');
    API = mod.API;
}

beforeEach(async () => {
    vi.useFakeTimers();
    _orig = { fetch: global.fetch, ES: global.EventSource, ls: global.localStorage };
    global.EventSource = class FakeES { constructor() { es = this; this.close = () => {}; } };
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
    vi.useRealTimers();
    vi.resetModules();
    global.fetch = _orig.fetch;
    global.EventSource = _orig.ES;
    global.localStorage = _orig.ls;
});

async function flushMicrotasks() { for (let i = 0; i < 30; i++) await Promise.resolve(); }
async function tick(ms = 0) { await vi.advanceTimersByTimeAsync(ms); await flushMicrotasks(); }
const finish = { status: 'completed', winner: 'A', ipponsA: ['M', 'M'], ipponsB: [] };
const shown = { id: 'm1', sideA: 'A', sideB: 'B', modifiedAt: 0 };

function answering() {
    global.fetch = vi.fn((url) => {
        if (String(url).includes('/api/time')) return Promise.reject(new TypeError('clock'));
        return Promise.resolve({ ok: true, status: 200, json: async () => ({ ...finish, id: 'm1', modifiedAt: 777, scoreVersion: 5 }) });
    });
}

async function finishAnswered() {
    answering();
    await API.recordScore('c1', 'm1', finish, 'pw', shown);
    await tick(0);
    expect(API.ownStatusMove('c1', 'm1')).toMatchObject({ status: 'completed', landed: true, stamp: 777, version: 5 });
}

describe('an answered status move', () => {
    it('goes when the draw is generated again', async () => {
        const off = API.subscribeToEvents(() => {});
        const told = vi.fn();
        const offMoves = API.subscribeOwnStatusMoves(told);
        await finishAnswered();
        told.mockClear();
        es.onmessage({ data: JSON.stringify({ type: 'draw_generated', data: { competitionId: 'c1' } }), lastEventId: '' });
        await tick(0);
        expect(API.ownStatusMove('c1', 'm1')).toBeNull();
        expect(told).toHaveBeenCalled();
        offMoves();
        off();
    });

    it('goes when the court\'s feed no longer holds the match', async () => {
        await finishAnswered();
        API.forgetOwnStatusMovesNotIn([{ compId: 'c1', id: 'm1' }]);
        expect(API.ownStatusMove('c1', 'm1'), 'still in the feed').not.toBeNull();
        API.forgetOwnStatusMovesNotIn([{ compId: 'c1', id: 'm2' }]);
        expect(API.ownStatusMove('c1', 'm1')).toBeNull();
    });
});
