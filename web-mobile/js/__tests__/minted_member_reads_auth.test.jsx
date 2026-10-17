// bc-evts stage 3 review 11, Y4 and Y5: the reads of a minted member's team
// (minted_members.jsx, made by api_client.jsx).
//   Y4: a read refused for the password (401/403) is no failed try: it is
//       parked until a new sign-in, never looped and never counted toward the
//       bound; a revoked password's park pauses the timer; a new sign-in reads
//       again whatever the earlier tries.
//   Y5: the guards round 10 left unpinned: the timer re-armed after a failed
//       read with no write waiting, the read remembered for a member this tab
//       did not add itself, and one member_kept alert per tab per member.
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import path from 'node:path';
import { FETCH_TIMEOUT_MS } from '../write_result.jsx';
import { MINTED_MEMBERS_KEY, MINTED_REREAD_TRIES, createMintedMembers } from '../minted_members.jsx';

const CLIENT = path.resolve(__dirname, '../api_client.jsx');
const flushMicro = async () => { for (let i = 0; i < 40; i++) await Promise.resolve(); };
const tick = async (ms) => { await vi.advanceTimersByTimeAsync(ms); await flushMicro(); };
const resp = (status, body) => Promise.resolve({
    ok: status >= 200 && status < 300, status, headers: { get: () => null },
    json: async () => body, text: async () => JSON.stringify(body),
});
const quiet = { onRefusal: () => true };
const ID = '0d0d0d0d-0d0d-4d0d-8d0d-0d0d0d0d0d0d';

let ls;
let saved;
let quietened = [];
let tab = 0;
beforeAll(() => {
    vi.useFakeTimers();
    saved = { fetch: global.fetch, ES: global.EventSource, ls: global.localStorage };
    global.EventSource = class { constructor() { this.close = () => {}; } };
    global.EventSource.OPEN = 1;
});
afterAll(() => {
    vi.useRealTimers();
    global.fetch = saved.fetch;
    global.EventSource = saved.ES;
    global.localStorage = saved.ls;
});
beforeEach(() => {
    ls = {};
    global.localStorage = {
        getItem: (k) => (k in ls ? ls[k] : null),
        setItem: (k, v) => { ls[k] = String(v); },
        removeItem: (k) => { delete ls[k]; },
        clear: () => { ls = {}; },
    };
    quietened = [vi.spyOn(console, 'warn').mockImplementation(() => {}), vi.spyOn(console, 'error').mockImplementation(() => {})];
});
afterEach(() => {
    for (const s of quietened) s.mockRestore();
    vi.clearAllTimers();
});

async function loadTab() {
    vi.resetModules();
    tab++;
    return import(/* @vite-ignore */ `${CLIENT}?tab=y4y5-${tab}`);
}

// A server whose members door answers by `members(pw, n)` (n: the read's
// number), refuses every member add 500, and takes anything else.
function server(members) {
    const reads = [];
    global.fetch = vi.fn((url, opts = {}) => {
        url = String(url);
        if (url.includes('/api/time')) return Promise.reject(new TypeError('clock'));
        const method = opts.method || 'GET';
        const pw = (opts.headers || {})['X-Tournament-Password'];
        if (method === 'GET' && url.endsWith('/team-members')) {
            reads.push({ at: Date.now(), pw });
            return members(pw, reads.length);
        }
        if (method === 'POST' && url.endsWith('/teams/t1/members')) return resp(500, { error: 'internal error' });
        if (url.endsWith('/score-events')) {
            const events = JSON.parse(opts.body).events;
            return resp(200, { outcomes: events.map((e) => ({ id: e.id, outcome: 'applied' })) });
        }
        return resp(200, {});
    });
    return reads;
}

// An add that keeps failing, then discarded once it went out: its member is
// then known only by a read of the team's members.
async function addDiscardedAfterItWentOut(API, password) {
    const member = await API.addTeamMember('c1', 't1', 'Ren Abe', password, quiet);
    if (member.settled) member.settled.catch(() => {});
    for (let i = 0; i < 40 && !API.heldWrites().some((h) => h.kind === 'member' && h.keepsFailing); i++) await tick(2000);
    const h = API.heldWrites().find((x) => x.kind === 'member' && x.keepsFailing);
    expect(h, 'the add keeps failing').toBeTruthy();
    expect(API.discardHeldWrite(h.key)).toBeTruthy();
    await tick(10);
    return member;
}

function listenDropped() {
    const dropped = [];
    const onDrop = (e) => dropped.push(e.detail.memberId);
    window.addEventListener('team-member-dropped', onDrop);
    return { dropped, stop: () => window.removeEventListener('team-member-dropped', onDrop) };
}

describe('Y4: a read refused for the password waits for a new sign-in', () => {
    it('is not asked again with the refused password, counts no try, and a new sign-in reads again', async () => {
        let serverPassword = 'old';
        const reads = server((pw) => (pw === serverPassword ? resp(200, { teamMembers: { t1: [] } }) : resp(401, { error: 'unauthorized' })));
        const { API } = await loadTab();
        const d = listenDropped();
        try {
            serverPassword = 'new';
            const member = await addDiscardedAfterItWentOut(API, 'old');
            expect(reads.map((r) => r.pw), 'one read with the add\'s password, refused').toEqual(['old']);
            // Long past every re-read the bound allows: nothing is asked with
            // the password the server refused.
            for (let i = 0; i < 40; i++) await tick(FETCH_TIMEOUT_MS * 4);
            expect(reads.length, 'a refused password is not asked again').toBe(1);
            API.resumeAfterAuth('new');
            for (let i = 0; i < 10; i++) await tick(FETCH_TIMEOUT_MS / 2);
            expect(reads.at(-1).pw, 'asked again with the new sign-in').toBe('new');
            expect(d.dropped).toEqual([member.id]);
        } finally {
            d.stop();
            API.clearQueue();
        }
    }, 60000);

    it('a new sign-in reads again even after every try the bound allows has failed', async () => {
        let fail = true;
        const reads = server(() => (fail ? resp(500, { error: 'x' }) : resp(200, { teamMembers: { t1: [] } })));
        const { API } = await loadTab();
        const d = listenDropped();
        try {
            const member = await addDiscardedAfterItWentOut(API, 'pw');
            for (let i = 0; i < 60; i++) await tick(FETCH_TIMEOUT_MS * 8);
            const tried = reads.length;
            for (let i = 0; i < 20; i++) await tick(FETCH_TIMEOUT_MS * 8);
            expect(reads.length, 'precondition: the bound is reached and nothing more is asked').toBe(tried);
            fail = false;
            API.resumeAfterAuth('pw2');
            for (let i = 0; i < 10; i++) await tick(FETCH_TIMEOUT_MS / 2);
            expect(reads.length).toBeGreaterThan(tried);
            expect(reads.at(-1).pw).toBe('pw2');
            expect(d.dropped).toEqual([member.id]);
        } finally {
            d.stop();
            API.clearQueue();
        }
    }, 60000);

    it('a revoked password\'s park pauses the re-reads until the new sign-in', async () => {
        let fail = true;
        const reads = server(() => (fail ? resp(500, { error: 'x' }) : resp(200, { teamMembers: { t1: [] } })));
        const { API } = await loadTab();
        const d = listenDropped();
        try {
            const member = await addDiscardedAfterItWentOut(API, 'old');
            expect(reads.length).toBe(1);
            API.parkQueueForReauth();
            fail = false;
            for (let i = 0; i < 40; i++) await tick(FETCH_TIMEOUT_MS * 4);
            expect(reads.length, 'nothing is read with the revoked password').toBe(1);
            API.resumeAfterAuth('new');
            for (let i = 0; i < 10; i++) await tick(FETCH_TIMEOUT_MS / 2);
            expect(reads.at(-1).pw).toBe('new');
            expect(d.dropped).toEqual([member.id]);
        } finally {
            d.stop();
            API.clearQueue();
        }
    }, 60000);
});

describe('Y5: the guards round 10 left unpinned', () => {
    it('a failed read with no write waiting re-arms the timer, so the member is asked about again', async () => {
        const reads = server((_pw, n) => (n === 1 ? resp(500, { error: 'x' }) : resp(200, { teamMembers: { t1: [] } })));
        const { API } = await loadTab();
        const d = listenDropped();
        try {
            const member = await addDiscardedAfterItWentOut(API, 'pw');
            expect(reads.length).toBe(1);
            for (let i = 0; i < 30; i++) await tick(FETCH_TIMEOUT_MS / 2);
            expect(reads.length, 'asked again').toBeGreaterThanOrEqual(2);
            expect(d.dropped).toEqual([member.id]);
        } finally {
            d.stop();
            API.clearQueue();
        }
    }, 60000);

    it('a member this tab did not add is asked about again by this tab once the write that named it has gone', async () => {
        // What another tab left known of its add: sent, then unanswered.
        ls[MINTED_MEMBERS_KEY] = JSON.stringify({ [ID]: { c: 'c1', t: 't1', s: 'unknown', at: Date.now(), w: 1 } });
        const reads = server((_pw, n) => (n === 1 ? resp(500, { error: 'x' }) : resp(200, { teamMembers: { t1: [] } })));
        const { API } = await loadTab();
        const d = listenDropped();
        try {
            // A tap naming the member: the gate reads the team, the read fails
            // and the tap goes as it was made.
            API.recordTap('c1', 'm1', { kind: 'rep.pick', pos: -1, side: 'a', memberId: ID }, 'pw', { id: 'm1' });
            await tick(10);
            expect(reads.length).toBe(1);
            for (let i = 0; i < 10 && API.pendingTaps('c1', 'm1').length > 0; i++) await tick(FETCH_TIMEOUT_MS);
            expect(API.pendingTaps('c1', 'm1'), 'precondition: the tap has gone').toEqual([]);
            for (let i = 0; i < 30; i++) await tick(FETCH_TIMEOUT_MS / 2);
            expect(reads.length, 'asked again with nothing naming the member').toBeGreaterThanOrEqual(2);
            expect(d.dropped).toEqual([ID]);
        } finally {
            d.stop();
            API.clearQueue();
        }
    }, 60000);

    it('two tabs that both find a dropped member kept raise one alert each', async () => {
        const dropped = JSON.stringify({ [ID]: { c: 'c1', t: 't1', s: 'dropped', at: Date.now() } });
        ls[MINTED_MEMBERS_KEY] = dropped;
        server(() => resp(200, { teamMembers: { t1: [{ id: ID, name: 'Ren Abe', number: 1 }] } }));
        // One window here, so each tab's storage handler is called as the
        // browser calls it in the OTHER tab (the module keeps only the latest
        // tab's on this window).
        const tabA = await loadTab();
        const hearA = window.__bcStoredQueueHandler;
        const tabB = await loadTab();
        const hearB = window.__bcStoredQueueHandler;
        const alerts = { a: [], b: [] };
        const offA = tabA.subscribeQueueAlert((x) => { if (x.kind === 'member_kept') alerts.a.push(x); });
        const offB = tabB.subscribeQueueAlert((x) => { if (x.kind === 'member_kept') alerts.b.push(x); });
        const event = (newValue) => ({ key: MINTED_MEMBERS_KEY, oldValue: dropped, newValue });
        try {
            // Both tabs read the dropped record before either wrote, and each
            // finds the member in its list: each says so once.
            await tabA.API.fetchSquads('c1', 'pw');
            const keptByA = ls[MINTED_MEMBERS_KEY];
            ls[MINTED_MEMBERS_KEY] = dropped;
            await tabB.API.fetchSquads('c1', 'pw');
            const keptByB = ls[MINTED_MEMBERS_KEY];
            await tick(10);
            expect([alerts.a.length, alerts.b.length]).toEqual([1, 1]);
            // Each then hears the other's write, which took back the same drop.
            hearA(event(keptByB));
            hearB(event(keptByA));
            await tick(10);
            expect(alerts.a.length, 'tab A says it once').toBe(1);
            expect(alerts.b.length, 'tab B says it once').toBe(1);
        } finally {
            if (typeof offA === 'function') offA();
            if (typeof offB === 'function') offB();
            tabA.API.clearQueue();
            tabB.API.clearQueue();
        }
    }, 60000);
});

describe('Y4: the owner starts the failed reads again for a new sign-in', () => {
    it('readsResumed forgets the failed tries, so the member is asked about at once', () => {
        const store = {};
        const clock = { t: 1000 };
        const m = createMintedMembers({
            storage: { get: (k) => (k in store ? store[k] : null), set: (k, v) => { store[k] = String(v); }, remove: (k) => { delete store[k]; } },
            now: () => clock.t, timeoutMs: FETCH_TIMEOUT_MS, tab: 'a',
        });
        const add = { kind: 'member', method: 'POST', compID: 'c1', url: '/api/competitions/c1/teams/t1/members', payload: { id: ID, name: 'Ren Abe' } };
        m.minted(add);
        m.sent(add);
        m.answered(add, 'retry', true);
        m.discarded(add);
        const ctx = { addQueued: () => false, addOutHere: () => false, teamOf: () => 't1' };
        // Every read the bound allows, each failing.
        for (let guard = 0; guard < 100; guard++) {
            const due = m.due([ID], ctx);
            if (due.reads.length > 0) { m.readEnded(ID, null, ctx); continue; }
            if (!due.until) break;
            clock.t = due.until;
        }
        expect(m.recordOf(ID).rn).toBeGreaterThan(MINTED_REREAD_TRIES);
        clock.t += 60 * 60 * 1000;
        expect(m.due([ID], ctx).reads, 'precondition: the bound is reached').toEqual([]);
        m.readsResumed([ID]);
        expect(m.due([ID], ctx).reads.map((r) => r.id)).toEqual([ID]);
    });
});
