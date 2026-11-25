// One message per match when a phone's offline result is refused (decision
// q-f6-one-message, F6). Phone A scores a match offline and finishes it while
// phone B finishes it first. Back online, A's taps are refused one by one (the
// score-events door's 200, each `refused` with the server's sentence) and its
// Finish is refused 409 `result_finalized`. A was told twice: "2 taps were not
// applied. Open their matches to see why ..." (no match named, and a
// competitor has no history to open), then the refused result. Now the refused
// result is the one message, it names its match, and the taps of that match
// the same send refused are not told apart. Taps told on their own name their
// match and end on the server's sentence when every one was given it.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { queueAlertMessage } from '../app.jsx';

let _orig;
let _lsStore;

beforeEach(() => {
    vi.useFakeTimers();
    vi.resetModules();
    vi.spyOn(console, 'warn').mockImplementation(() => {});
    _orig = { fetch: global.fetch, ES: global.EventSource, ls: global.localStorage };
    global.EventSource = class FakeES { constructor() { this.readyState = 0; this.close = () => {}; } };
    global.EventSource.OPEN = 1;
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
    if (_orig.fetch === undefined) delete global.fetch; else global.fetch = _orig.fetch;
    if (_orig.ES === undefined) delete global.EventSource; else global.EventSource = _orig.ES;
    if (_orig.ls === undefined) delete global.localStorage; else global.localStorage = _orig.ls;
});

async function flushMicrotasks() { for (let i = 0; i < 30; i++) await Promise.resolve(); }
async function tick(ms = 0) { await vi.advanceTimersByTimeAsync(ms); await flushMicrotasks(); }

// The server's own sentences (handlers_match_refusals.go, the self-run judge).
const FINALIZED = 'This match result has already been reported. Contact the tournament organizer to correct it.';
const NOT_ALLOWED = 'This decision is the tournament organizer\'s to record.';

const M1 = { id: 'm1', phase: 'bracket', matchNumber: 3, status: 'running', modifiedAt: 0 };
const M2 = { id: 'm2', phase: 'bracket', matchNumber: 4, status: 'running', modifiedAt: 0 };
const tap = (letter = 'M', side = 'a') => ({ kind: 'ippon.add', pos: 0, side, letter });

// A server that answers every tap with `tapAnswer(event, matchID)` and every
// Finish 409 result_finalized; offline, every request fails as a dead network.
function makeServer(tapAnswer) {
    const server = { online: false };
    server.fetch = vi.fn((url, opts) => {
        const u = String(url);
        if (u.includes('/api/time')) {
            return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ nowMs: Date.now() }) });
        }
        if (!server.online) return Promise.reject(new TypeError('network error'));
        const matchID = u.split('/matches/')[1].split('/')[0];
        if (u.endsWith('/score-events')) {
            const body = JSON.parse(opts.body);
            const outcomes = body.events.map((e) => ({ id: e.id, ...tapAnswer(e, matchID) }));
            return Promise.resolve({
                ok: true, status: 200,
                json: () => Promise.resolve({ outcomes, match: { id: matchID, scoreVersion: 9 } }),
            });
        }
        const refusal = { error: 'result_finalized', message: FINALIZED };
        return Promise.resolve({
            ok: false, status: 409,
            json: () => Promise.resolve(refusal),
            text: () => Promise.resolve(JSON.stringify(refusal)),
        });
    });
    return server;
}

const finalized = () => ({ outcome: 'refused', reason: 'result_finalized', message: FINALIZED });

// Queues `send` offline as a competitor (the empty password), brings the
// server back and lets one send run; answers the words the operator reads.
async function backOnline(send, tapAnswer = finalized) {
    const server = makeServer(tapAnswer);
    global.fetch = server.fetch;
    const mod = await import('../api_client.jsx');
    await flushMicrotasks();
    const alerts = [];
    mod.subscribeQueueAlert((a) => alerts.push(a));
    mod.API.noteShownMatch('c1', M1);
    mod.API.noteShownMatch('c1', M2);
    await send(mod.API);
    await flushMicrotasks();
    server.online = true;
    window.dispatchEvent(new Event('online'));
    await tick(3000);
    return alerts.filter((a) => a.kind !== 'sent').map(queueAlertMessage);
}

const finish = (API, m) => API.recordScore('c1', m.id, { status: 'completed', winner: 'A' }, '', m);

describe('a refused offline result is told once, naming its match', () => {
    it('refused taps and the refused Finish of one match: one message, the result, with the whole sentence', async () => {
        const told = await backOnline(async (API) => {
            API.recordTap('c1', 'm1', tap('M'), '', M1);
            API.recordTap('c1', 'm1', tap('K'), '', M1);
            await finish(API, M1);
        });
        expect(told).toEqual([
            `A result in Match 3 was refused by the server and cannot be saved. ${FINALIZED}`,
        ]);
    });

    it('a refused Finish with no taps before it names its match too', async () => {
        const told = await backOnline((API) => finish(API, M1));
        expect(told).toEqual([`A result in Match 3 was refused by the server and cannot be saved. ${FINALIZED}`]);
    });

    it('only the taps of the refused result\'s match are folded: another match\'s taps are told, named', async () => {
        const told = await backOnline(async (API) => {
            API.recordTap('c1', 'm1', tap('M'), '', M1);
            await finish(API, M1);
            API.recordTap('c1', 'm2', tap('M'), '', M2);
            API.recordTap('c1', 'm2', tap('D'), '', M2);
        });
        expect(told).toHaveLength(2);
        expect(told).toContain(`A result in Match 3 was refused by the server and cannot be saved. ${FINALIZED}`);
        expect(told).toContain(`2 taps were not applied in Match 4. ${FINALIZED}`);
    });
});

describe('taps told on their own name the match and end on the server\'s sentence', () => {
    it('several taps given the same sentence', async () => {
        const told = await backOnline((API) => {
            API.recordTap('c1', 'm1', tap('M'), '', M1);
            API.recordTap('c1', 'm1', tap('K'), '', M1);
        });
        expect(told).toEqual([`2 taps were not applied in Match 3. ${FINALIZED}`]);
    });

    it('one tap', async () => {
        const told = await backOnline((API) => { API.recordTap('c1', 'm1', tap('M'), '', M1); });
        expect(told).toEqual([`Not applied in Match 3: ${FINALIZED}`]);
    });

    it('a sentence that leads "Not applied:" is not said twice', async () => {
        const closed = "Not applied: this match is already finished, so this change was kept in the match's history. To change the result, tap Correct.";
        const told = await backOnline((API) => {
            API.recordTap('c1', 'm1', tap('M'), '', M1);
            API.recordTap('c1', 'm1', tap('K'), '', M1);
        }, () => ({ outcome: 'matchClosed', reason: 'match_closed', message: closed }));
        expect(told).toEqual([
            "2 taps were not applied in Match 3: this match is already finished, so this change was kept in the match's history. To change the result, tap Correct.",
        ]);
    });

    it('taps given different sentences still name the match, and send the reader to it', async () => {
        const told = await backOnline((API) => {
            API.recordTap('c1', 'm1', tap('M'), '', M1);
            API.recordTap('c1', 'm1', { kind: 'bout.decision', pos: 0, value: 'fusensho' }, '', M1);
        }, (e) => (e.kind === 'bout.decision'
            ? { outcome: 'refused', reason: 'decision_not_allowed', message: NOT_ALLOWED }
            : finalized()));
        expect(told).toEqual([
            "2 taps were not applied in Match 3. Open the match to see why; anything the server kept is in the match's history.",
        ]);
    });

    it('two matches\' taps: one message per match', async () => {
        const told = await backOnline((API) => {
            API.recordTap('c1', 'm1', tap('M'), '', M1);
            API.recordTap('c1', 'm2', tap('M'), '', M2);
        });
        expect(told).toHaveLength(2);
        expect(told).toContain(`Not applied in Match 3: ${FINALIZED}`);
        expect(told).toContain(`Not applied in Match 4: ${FINALIZED}`);
    });
});
