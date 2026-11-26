// bc-sseq FR8: the device whose result the court replaced is told once. A
// finish this device sent was applied, and its own answer said so; a later
// result from the court console of the tablet running the match's shiaijo,
// which never saw it, then replaced it. Every copy of the match names the
// replaced result (`replacedResult`: the write id it was sent with), and the
// outbox, which keeps the write ids of its own applied results, raises ONE
// information alert (`court_replaced`), from whatever copy names it first,
// after a reload too, and never for a write it did not send or one the server
// did not apply.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { courtReplacedAlertText } from '../write_result.jsx';
import { queueAlertMessage, queueAlertToastType } from '../app.jsx';
import { createAppliedResults, APPLIED_RESULTS_KEY, APPLIED_RESULTS_MAX } from '../api_client_store.jsx';

let mod;
let API;
let _lsStore;
let _orig;
let finishAnswer;
let sentWriteId;
let detail;

async function load() {
    vi.resetModules();
    mod = await import('../api_client.jsx');
    API = mod.API;
}

const json = (body) => ({ ok: true, status: 200, json: async () => body });
const shown = { id: 'm1', compId: 'c1', phase: 'pool', pool: 'A', matchNumber: 3, court: 'A', status: 'running', modifiedAt: 0 };
const finish = { status: 'completed', ipponsA: ['M', 'M'], ipponsB: [], winner: 'a' };
const stored = (extra = {}) => ({ id: 'm1', status: 'completed', court: 'A', winner: 'a', scoreVersion: 3, ...extra });
const replacedCopy = (writeId, v = 5) => stored({ winner: 'b', scoreVersion: v, replacedResult: { writeId, stamp: 100 } });

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
    finishAnswer = null;
    sentWriteId = '';
    detail = null;
    global.fetch = vi.fn((url, init = {}) => {
        const u = String(url);
        if (u.includes('/api/time')) return Promise.reject(new TypeError('clock'));
        if (u.endsWith('/score')) {
            sentWriteId = JSON.parse(init.body).writeId;
            return new Promise((resolve) => { finishAnswer = (b) => resolve(json(b)); });
        }
        if (u.endsWith('/api/viewer/competitions/c1') && detail) return Promise.resolve(json(detail));
        return Promise.reject(new TypeError('offline'));
    });
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

function listen() {
    const alerts = [];
    const off = mod.subscribeQueueAlert((a) => alerts.push(a));
    return { alerts, off, replaced: () => alerts.filter((a) => a.kind === 'court_replaced') };
}

// The competition as a fetch reads it, its one match given as `m`.
async function fetchWith(m) {
    detail = { id: 'c1', poolMatches: [m] };
    await API.fetchCompetitionDetails('c1');
    await tick(0);
}

// This device finishes the match and the server applies it.
async function finishApplied() {
    API.recordScore('c1', 'm1', finish, 'pw', shown);
    await tick(0);
    expect(finishAnswer, 'the finish was sent').toBeTypeOf('function');
    expect(sentWriteId, 'the finish carries a write id').toBeTruthy();
    finishAnswer(stored());
    await tick(0);
    return sentWriteId;
}

describe('the device whose result the court replaced', () => {
    it('is told once, from the first copy that names its applied result', async () => {
        const heard = listen();
        const writeId = await finishApplied();
        expect(heard.replaced()).toEqual([]);
        await fetchWith(replacedCopy(writeId));
        expect(heard.replaced()).toHaveLength(1);
        expect(heard.replaced()[0]).toMatchObject({ compID: 'c1', matchID: 'm1', court: 'A', writeId });
        expect(queueAlertToastType(heard.replaced()[0])).toBe('info');
        expect(queueAlertMessage(heard.replaced()[0])).toMatch(/^Shiaijo A's court replaced the result you recorded for /);
        // Every later copy naming it, a newer one included, tells nothing more.
        await fetchWith(replacedCopy(writeId));
        await fetchWith(replacedCopy(writeId, 6));
        await tick(60_000);
        expect(heard.replaced(), 'told once').toHaveLength(1);
        heard.off();
    });

    it('is not told again by the next page after a reload', async () => {
        const writeId = await finishApplied();
        await fetchWith(replacedCopy(writeId));
        window.dispatchEvent(new Event('pagehide'));
        await load();
        const heard = listen();
        await fetchWith(replacedCopy(writeId));
        await tick(60_000);
        expect(heard.replaced()).toEqual([]);
        heard.off();
    });

    it('is told by the next page when the copy first reaches it after a reload', async () => {
        const writeId = await finishApplied();
        window.dispatchEvent(new Event('pagehide'));
        await load();
        const heard = listen();
        await fetchWith(replacedCopy(writeId));
        expect(heard.replaced()).toHaveLength(1);
        await fetchWith(replacedCopy(writeId));
        expect(heard.replaced(), 'told once').toHaveLength(1);
        heard.off();
    });

    it("is told as its own answer lands when the court's copy overtook it", async () => {
        const heard = listen();
        API.recordScore('c1', 'm1', finish, 'pw', shown);
        await tick(0);
        // The court's result reaches this page before the finish's answer.
        await fetchWith(replacedCopy(sentWriteId));
        expect(heard.replaced()).toEqual([]);
        finishAnswer(stored({ scoreVersion: 3 }));
        await tick(0);
        expect(heard.replaced()).toHaveLength(1);
        heard.off();
    });

    it('is never told for a result the server did not apply', async () => {
        const heard = listen();
        API.recordScore('c1', 'm1', finish, 'pw', shown);
        await tick(0);
        const writeId = sentWriteId;
        finishAnswer({
            applied: false, reason: 'superseded', heldGroups: ['result'], heldReason: 'match_closed',
            match: stored({ winner: 'b', scoreVersion: 4 }),
        });
        await tick(0);
        await fetchWith(replacedCopy(writeId));
        await tick(60_000);
        expect(heard.replaced()).toEqual([]);
        heard.off();
    });

    it('is never told for a write id this device never sent', async () => {
        const heard = listen();
        await finishApplied();
        await fetchWith(replacedCopy('other-device.w1'));
        await tick(60_000);
        expect(heard.replaced()).toEqual([]);
        heard.off();
    });

    it('is not told from an older copy once a newer one names no replaced result', async () => {
        const heard = listen();
        const writeId = await finishApplied();
        await fetchWith(stored({ scoreVersion: 7 }));
        await fetchWith(replacedCopy(writeId, 5));
        await tick(60_000);
        expect(heard.replaced()).toEqual([]);
        heard.off();
    });
});

describe('courtReplacedAlertText', () => {
    it('names the shiaijo and the match, and says where the result went', () => {
        expect(courtReplacedAlertText({ court: 'A' }, 'Pool A · Match 3')).toBe(
            "Shiaijo A's court replaced the result you recorded for Pool A · Match 3. The court's result counts; yours is in the match's history.",
        );
    });
    it('reads whole with no shiaijo or match label', () => {
        expect(courtReplacedAlertText({}, '')).toBe(
            "The shiaijo's court replaced the result you recorded for a match. The court's result counts; yours is in the match's history.",
        );
    });
});

describe('the applied-results record', () => {
    const memStorage = () => {
        const data = {};
        return {
            data,
            getItem: (k) => (k in data ? data[k] : null),
            setItem: (k, v) => { data[k] = String(v); },
            removeItem: (k) => { delete data[k]; },
        };
    };

    it(`keeps the newest ${APPLIED_RESULTS_MAX}, the oldest going first`, () => {
        const storage = memStorage();
        const rec = createAppliedResults({ storage });
        for (let i = 0; i <= APPLIED_RESULTS_MAX; i++) rec.note({ id: `w${i}`, c: 'c1', m: 'm1', tab: 't' });
        expect(rec.has('w0')).toBe(false);
        expect(rec.has(`w${APPLIED_RESULTS_MAX}`)).toBe(true);
        expect(JSON.parse(storage.data[APPLIED_RESULTS_KEY])).toHaveLength(APPLIED_RESULTS_MAX);
    });

    it('drops an entry older than a day', () => {
        let t = 1_000;
        const rec = createAppliedResults({ storage: memStorage(), now: () => t });
        rec.note({ id: 'old', c: 'c1', m: 'm1', tab: 't' });
        t += 25 * 60 * 60 * 1000;
        rec.note({ id: 'new', c: 'c1', m: 'm1', tab: 't' });
        expect(rec.has('old')).toBe(false);
        expect(rec.has('new')).toBe(true);
    });

    it('takes an entry out once, only when allowed', () => {
        const rec = createAppliedResults({ storage: memStorage() });
        rec.note({ id: 'w1', c: 'c1', m: 'm1', tab: 't' });
        expect(rec.take('w1', () => false)).toBeNull();
        expect(rec.take('w1')).toMatchObject({ id: 'w1' });
        expect(rec.take('w1')).toBeNull();
    });

    it('is kept in memory while storage refuses a write', () => {
        const storage = memStorage();
        storage.setItem = () => { throw new Error('quota'); };
        const rec = createAppliedResults({ storage });
        rec.note({ id: 'w1', c: 'c1', m: 'm1', tab: 't' });
        expect(rec.has('w1')).toBe(true);
    });

    it('a sign-out takes out the entries it names', () => {
        const rec = createAppliedResults({ storage: memStorage() });
        rec.note({ id: 'mine', c: 'c1', m: 'm1', tab: 'a' });
        rec.note({ id: 'theirs', c: 'c1', m: 'm1', tab: 'b' });
        rec.forget((e) => e.tab === 'a');
        expect(rec.has('mine')).toBe(false);
        expect(rec.has('theirs')).toBe(true);
    });
});

describe('a sign-out', () => {
    it("forgets this tab's applied results, so the next operator is not told", async () => {
        const writeId = await finishApplied();
        API.clearQueue();
        await tick(0);
        const heard = listen();
        await fetchWith(replacedCopy(writeId));
        await tick(60_000);
        expect(heard.replaced()).toEqual([]);
        heard.off();
    });
});
