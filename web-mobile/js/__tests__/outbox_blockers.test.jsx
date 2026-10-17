// bc-evts stage 3 review 4: what holds a write back, and what it carries.
// G3: an entry waits on everything its `after` waits on, its own base's earlier
// entries included. G1: a write naming a member id this device minted for an
// add not yet landed follows that add, whatever the write (a score write's
// representative pick included), and a refused add takes the id off it. G2: a
// match whose writes wait on a write of another base the server keeps
// refusing names that write, and offers to discard it.
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
const stored = () => JSON.parse(_lsStore.bc_write_queue_v2 || '[]');
const answer = (status, body) => Promise.resolve({
    ok: status >= 200 && status < 300, status, headers: { get: () => null },
    json: async () => body, text: async () => JSON.stringify(body),
});
const MEMBERS = '/api/competitions/c1/teams/t1/members';

function server(reply) {
    const sent = [];
    global.fetch = vi.fn((url, opts) => {
        if (String(url).includes('/api/time')) return Promise.reject(new TypeError('clock'));
        const req = { method: opts.method, url: String(url), body: opts.body ? JSON.parse(opts.body) : undefined };
        sent.push(req);
        const r = reply(req);
        return r === null ? new Promise(() => {}) : r;
    });
    return sent;
}
const reconnect = async () => { window.dispatchEvent(new Event('online')); await tick(50); };
const lineupOk = (req) => answer(200, { positions: req.body.positions, memberIds: req.body.memberIds });

// A team match: Shiro is t1, Aka t2.
const MATCH = {
    id: 'm1', compId: 'c1', status: 'running', court: '', modifiedAt: 1,
    sideA: { id: 't1', name: 'Team One' }, sideB: { id: 't2', name: 'Team Two' },
};
function scoreWithRepPick(memberId) {
    return {
        ...MATCH,
        status: 'running',
        subResults: [{
            position: -1, sideA: 'Team One', sideB: 'Team Two', ipponsA: [], ipponsB: [],
            sideAMemberId: memberId,
        }],
    };
}
const repRow = (req) => (req.body.subResults || []).find((r) => r.position === -1);

describe('an entry waits on its own base too (G3)', () => {
    it('a lineup save following a rename that waits behind an add the server keeps failing is held', async () => {
        const sent = server((req) => (req.method === 'POST' ? answer(503, { error: 'unavailable' }) : lineupOk(req)));
        const member = await API.addTeamMember('c1', 't1', 'Ren Abe', 'pw');
        // The rename is the team's last member write, so the save names it in
        // `after`; it waits behind the add on the team's base and is never sent.
        await API.renameTeamMember('c1', 't1', member.id, 'Ren Abe-Sato', 'pw', { member });
        await API.putMatchLineup('c1', 't1', 'm1', { senpo: 'Ren Abe-Sato' }, 'pw', { senpo: member.id }, ['senpo']);
        await reconnect();
        await reconnect();
        expect(sent.every((r) => r.method === 'POST'), 'only the add goes').toBe(true);
        expect(API.hasHeldWrite('c1', 'm1'), 'the match shows what holds it').toBe(true);
    });
});

describe('a write naming a member this device minted follows its add (G1)', () => {
    it('a score write carrying a typed representative waits for the add', async () => {
        // The add fails once (a 5xx keeps it queued and the pass goes on to the
        // other matches), then lands: the score write is sent only after it.
        let addUp = false;
        const sent = server((req) => {
            if (req.method === 'POST') return addUp ? answer(201, { id: req.body.id, index: 6, name: req.body.name, modifiedAt: 9 }) : answer(503, { error: 'unavailable' });
            return answer(200, { ...MATCH });
        });
        const member = await API.addTeamMember('c1', 't1', 'Ren Abe', 'pw');
        const res = await API.recordScore('c1', 'm1', scoreWithRepPick(member.id), 'pw', MATCH);
        res.settled.catch(() => {});
        await reconnect();
        expect(sent.length).toBeGreaterThan(0);
        expect(sent.every((r) => r.method === 'POST'), 'no score write while the add is not in').toBe(true);
        addUp = true;
        await reconnect();
        const methods = sent.map((r) => r.method);
        expect(methods[methods.length - 1]).toBe('PUT');
        expect(methods.filter((x) => x === 'PUT')).toHaveLength(1);
        expect(repRow(sent[sent.length - 1]).sideAMemberId).toBe(member.id);
    });

    it('a refused add takes its id off the queued score write, and the rest still goes', async () => {
        const sent = server((req) => (req.method === 'POST'
            ? answer(409, { error: 'team_member_limit', message: 'This team has no room for another member.' })
            : answer(200, { ...MATCH })));
        const warned = vi.spyOn(console, 'warn').mockImplementation(() => {});
        const member = await API.addTeamMember('c1', 't1', 'Ren Abe', 'pw', { onRefusal: () => true });
        member.settled.catch(() => {});
        const res = await API.recordScore('c1', 'm1', scoreWithRepPick(member.id), 'pw', MATCH);
        res.settled.catch(() => {});
        await reconnect();
        const put = sent.find((r) => r.method === 'PUT');
        expect(put, 'the score write is sent once the add has gone').toBeTruthy();
        expect(repRow(put).sideAMemberId || '', 'the pick that named nobody is cleared').toBe('');
        expect(stored()).toEqual([]);
        warned.mockRestore();
    });

    it('a numbered row naming the minted member keeps its name and loses the id', async () => {
        const sent = server((req) => (req.method === 'POST'
            ? answer(409, { error: 'team_member_limit', message: 'This team has no room for another member.' })
            : answer(200, { ...MATCH })));
        const warned = vi.spyOn(console, 'warn').mockImplementation(() => {});
        const member = await API.addTeamMember('c1', 't1', 'Ren Abe', 'pw', { onRefusal: () => true });
        member.settled.catch(() => {});
        const res = await API.recordScore('c1', 'm1', {
            ...MATCH,
            subResults: [{ position: 1, sideA: 'Ren Abe', sideB: 'Kai Ito', ipponsA: ['M'], ipponsB: [], sideAMemberId: member.id, winnerMemberId: member.id }],
        }, 'pw', MATCH);
        res.settled.catch(() => {});
        await reconnect();
        const put = sent.find((r) => r.method === 'PUT');
        const row = put.body.subResults.find((r) => r.position === 1);
        expect(row.sideA).toBe('Ren Abe');
        expect(row.sideAMemberId || '').toBe('');
        expect(row.winnerMemberId || '').toBe('');
        warned.mockRestore();
    });

    it('a score write naming two teams\' new members follows both adds', async () => {
        const sent = server((req) => (req.method === 'POST' ? answer(503, { error: 'unavailable' }) : answer(200, { ...MATCH })));
        const a = await API.addTeamMember('c1', 't1', 'Ren Abe', 'pw');
        const b = await API.addTeamMember('c1', 't2', 'Kai Ito', 'pw');
        await API.recordScore('c1', 'm1', {
            ...scoreWithRepPick(a.id),
            subResults: [{ ...scoreWithRepPick(a.id).subResults[0], sideBMemberId: b.id }],
        }, 'pw', MATCH);
        await reconnect();
        expect(sent.filter((r) => r.method === 'PUT'), 'held while either add is out').toEqual([]);
        expect([...new Set(sent.filter((r) => r.method === 'POST').map((r) => r.url))].sort())
            .toEqual([MEMBERS, '/api/competitions/c1/teams/t2/members']);
    });
});

describe('the match names the write it waits on (G2)', () => {
    // The outbox warns once the add crosses the notice threshold; that warning
    // is expected here.
    let warned = null;
    afterEach(() => { if (warned) warned.mockRestore(); warned = null; });
    async function stuckAdd() {
        warned = vi.spyOn(console, 'warn').mockImplementation(() => {});
        server((req) => (req.method === 'POST' ? answer(503, { error: 'unavailable' }) : lineupOk(req)));
        const member = await API.addTeamMember('c1', 't1', 'Ren Abe', 'pw');
        await API.putMatchLineup('c1', 't1', 'm1', { senpo: 'Ren Abe' }, 'pw', { senpo: member.id }, ['senpo']);
        for (let i = 0; i < 14 && !API.heldWrites().some((h) => h.kind === 'member' && h.keepsFailing); i++) {
            await reconnect();
        }
        expect(API.heldWrites().find((h) => h.kind === 'member').keepsFailing, 'the add keeps failing').toBe(true);
        return member;
    }

    it('reads as failing, and names the blocker, once the write it waits on keeps failing', async () => {
        await stuckAdd();
        expect(API.heldWriteKeepsFailing('c1', 'm1')).toBe(true);
        const blocker = API.heldWriteBlocker('c1', 'm1');
        expect(blocker).toMatchObject({ kind: 'member', keepsFailing: true });
    });

    it('discarding the match\'s failing writes discards the blocker, and the save then goes', async () => {
        await stuckAdd();
        const sent = server((req) => (req.method === 'POST' ? answer(503, { error: 'unavailable' }) : lineupOk(req)));
        expect(API.discardFailingHeldWrites('c1', 'm1')).toBe(1);
        await tick(10);
        await reconnect();
        const put = sent.find((r) => r.method === 'PUT');
        expect(put, 'the lineup save goes once its blocker is discarded').toBeTruthy();
        expect(put.body.memberIds || {}, 'and names nobody who never was').toEqual({});
        expect(API.heldWriteBlocker('c1', 'm1')).toBe(null);
    });
});
