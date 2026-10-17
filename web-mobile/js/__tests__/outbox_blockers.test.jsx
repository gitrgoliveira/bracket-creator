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


// stage 3 review 5, H1: two entries waiting on each other are sent by nobody.
// The wait graph: an entry waits on what it names in `after` and on the entry
// before it in its own base (strict per-match order).
function waitCycle(queue) {
    const byKey = new Map(queue);
    const base = (k) => k.split('\u001f')[0];
    const order = new Map();
    for (const [k, d] of queue) {
        const b = base(k);
        if (!order.has(b)) order.set(b, []);
        order.get(b).push([k, d]);
    }
    const prev = new Map();
    for (const list of order.values()) {
        list.sort(([, a], [, b]) => (a.payload?.modifiedAt || a.sortStamp || 0) - (b.payload?.modifiedAt || b.sortStamp || 0)
            || (a.enqueuedAt || 0) - (b.enqueuedAt || 0));
        list.forEach(([k], i) => { if (i > 0) prev.set(k, list[i - 1][0]); });
    }
    const edges = (k) => {
        const d = byKey.get(k);
        const after = Array.isArray(d.after) ? d.after : (d.after ? [d.after] : []);
        return [...after.filter((a) => byKey.has(a)), ...(prev.has(k) ? [prev.get(k)] : [])];
    };
    const state = new Map();
    const visit = (k) => {
        if (state.get(k) === 1) return true;
        if (state.get(k) === 2) return false;
        state.set(k, 1);
        for (const n of edges(k)) if (visit(n)) return true;
        state.set(k, 2);
        return false;
    };
    return queue.some(([k]) => visit(k));
}

describe('no two entries ever wait on each other (H1)', () => {
    const running = (ippons) => ({ status: 'running', ipponsA: ippons, ipponsB: [], changed: ['points'] });
    const quiet = (r) => { if (r && r.settled) r.settled.catch(() => {}); return r; };

    it('a running write of the blocker made after a send back is its own entry, sent after it', async () => {
        quiet(await API.recordScore('c1', 'blk', running(['M']), 'pw', { id: 'blk', modifiedAt: 0 }));
        await flushMicrotasks();
        quiet(await API.requeueBlockerAndReopen('c1', 'tgt', 'c1', 'blk', 'pw', {}));
        await tick(5);
        quiet(await API.recordScore('c1', 'blk', running(['M', 'K']), 'pw', { id: 'blk', modifiedAt: 0 }));
        await flushMicrotasks();
        const q = stored();
        expect(waitCycle(q), 'no entry waits on one that waits on it').toBe(false);
        expect(q, 'the later running write does not take the place of one the send back follows').toHaveLength(3);

        const sent = server(() => answer(200, { applied: true }));
        for (let i = 0; i < 6 && stored().length > 0; i++) await reconnect();
        expect(sent.map((r) => r.url), 'strictly in the order they were made').toEqual([
            '/api/competitions/c1/matches/blk/score',
            '/api/competitions/c1/matches/tgt/requeue-blocker-and-reopen',
            '/api/competitions/c1/matches/blk/score',
        ]);
        expect(sent[2].body.ipponsA).toEqual(['M', 'K']);
        expect(stored()).toEqual([]);
    });
});

describe('a queue whose entries wait on each other is stuck, and says so (H1)', () => {
    // A queue an earlier build stored with a cycle in it: A follows B, B
    // follows C, C follows A. Nothing of it can ever be sent on its own.
    async function storedCycle() {
        for (const m of ['ma', 'mb', 'mc']) {
            const r = await API.recordScore('c1', m, { status: 'running', ipponsA: ['M'], ipponsB: [], changed: ['points'] }, 'pw', { id: m, modifiedAt: 0 });
            r.settled.catch(() => {});
        }
        await flushMicrotasks();
        const q = stored();
        const key = (m) => q.find(([k]) => k.startsWith(`c1:${m}\u001f`))[0];
        for (const [k, d] of q) {
            if (k === key('ma')) d.after = key('mb');
            if (k === key('mb')) d.after = key('mc');
            if (k === key('mc')) d.after = key('ma');
        }
        API.clearQueue();
        _lsStore.bc_write_queue_v2 = JSON.stringify(q);
        // The page is loaded again over what was stored.
        await load();
        await flushMicrotasks();
        return key;
    }

    it('reads as stuck after a reload, names the write it waits on, and goes once that is discarded', async () => {
        const key = await storedCycle();
        const sent = server(() => answer(200, { applied: true }));
        await reconnect();
        await reconnect();
        expect(sent.filter((r) => r.url.includes('/matches/')), 'nothing of the cycle can go').toEqual([]);
        expect(API.hasHeldWrite('c1', 'ma'), 'held, so the editor shows its banner').toBe(true);
        expect(API.heldWriteKeepsFailing('c1', 'ma'), 'stuck, so the editor offers Discard').toBe(true);
        const blocker = API.heldWriteBlocker('c1', 'ma');
        expect(blocker, 'the match names the write it waits on').toMatchObject({ key: key('mb'), matchID: 'mb' });
        expect(API.heldWrites().some((h) => h.matchID === 'mb' && h.keepsFailing), 'the list offers it too').toBe(true);

        expect(API.discardHeldWrite(blocker.key)).toBeTruthy();
        for (let i = 0; i < 6 && stored().length > 0; i++) await reconnect();
        expect(sent.filter((r) => r.url.includes('/matches/')).map((r) => r.url), 'the rest go, in order').toEqual([
            '/api/competitions/c1/matches/ma/score',
            '/api/competitions/c1/matches/mc/score',
        ]);
        expect(API.heldWriteKeepsFailing('c1', 'ma')).toBe(false);
        expect(stored()).toEqual([]);
    });
});

describe('asking what holds a match back reads the queue once (H2)', () => {
    // Every mounted editor asks on every sync-status change; a walk that
    // scanned and sorted the whole queue at each step cost seconds on a long
    // offline queue. Counted by sorts: the queue is put in replay order by a
    // sort, once per question however long the queue and the walk.
    it('sorts the queue a bounded number of times, whatever its length', async () => {
        const matches = [['big', 40], ...Array.from({ length: 30 }, (_, i) => [`o${i}`, 20])];
        for (const [m, n] of matches) {
            for (let i = 0; i < n; i++) {
                API.recordTap('c1', m, { kind: 'encho.add', pos: 0 }, 'pw', { id: m, modifiedAt: 0, status: 'running' });
            }
        }
        await flushMicrotasks();
        // `big` waits on o0's last tap, so the walk crosses into another match.
        const q = stored();
        const o0 = q.filter(([k]) => k.startsWith('c1:o0\u001f')).map(([k]) => k).pop();
        for (const [k, d] of q) if (k.startsWith('c1:big\u001f')) d.after = o0;
        API.clearQueue();
        _lsStore.bc_write_queue_v2 = JSON.stringify(q);
        await load();
        await flushMicrotasks();
        expect(stored().length).toBe(640);

        const sort = vi.spyOn(Array.prototype, 'sort');
        API.heldWriteBlocker('c1', 'big');
        API.heldWriteKeepsFailing('c1', 'big');
        const both = sort.mock.calls.length;
        sort.mockClear();
        let once;
        try {
            expect(both, 'the two questions read the queue a few times, not once per step').toBeLessThanOrEqual(4);
            API.heldWriteState('c1', 'big');
            once = sort.mock.calls.length;
        } finally {
            sort.mockRestore();
        }
        expect(once, 'the one question the editors ask reads it once').toBeLessThanOrEqual(2);
    });
});

describe('a tap naming a member this device minted follows its add (H4)', () => {
    const EVENTS = '/api/competitions/c1/matches/m1/score-events';
    const pick = (memberId) => ({ kind: 'rep.pick', pos: -1, side: 'a', memberId });

    it('is queued after the add', async () => {
        const member = await API.addTeamMember('c1', 't1', 'Ren Abe', 'pw');
        API.recordTap('c1', 'm1', pick(member.id), 'pw', MATCH);
        await flushMicrotasks();
        const q = stored();
        const add = q.find(([, d]) => d.kind === 'member');
        const tap = q.find(([, d]) => d.kind === 'evt');
        const after = Array.isArray(tap[1].after) ? tap[1].after : [tap[1].after];
        expect(after).toContain(add[0]);
    });

    it('a refused add takes its id off the queued tap before the tap goes', async () => {
        const warned = vi.spyOn(console, 'warn').mockImplementation(() => {});
        const member = await API.addTeamMember('c1', 't1', 'Ren Abe', 'pw', { onRefusal: () => true });
        member.settled.catch(() => {});
        API.recordTap('c1', 'm1', pick(member.id), 'pw', MATCH);
        await flushMicrotasks();
        const sent = server((req) => (req.url === MEMBERS
            ? answer(409, { error: 'team_member_limit', message: 'This team has no room for another member.' })
            : null));
        await reconnect();
        const post = sent.find((r) => r.url === EVENTS);
        expect(post, 'the tap goes once the add has gone').toBeTruthy();
        const events = post.body.events || [];
        expect(events).toHaveLength(1);
        expect(events[0].memberId, 'the pick that named nobody is cleared').toBeUndefined();
        warned.mockRestore();
    });
});

describe('a refused add another tab sent takes its id off this tab too (H3)', () => {
    // Both tabs hold the add and the score write naming the member. The other
    // tab sent the add, the server refused it, and that tab took the id off
    // its copy of the score write and stored it: this tab follows.
    it('strips its own copy and tells its editors', async () => {
        const member = await API.addTeamMember('c1', 't1', 'Ren Abe', 'pw');
        const res = await API.recordScore('c1', 'm1', scoreWithRepPick(member.id), 'pw', MATCH);
        res.settled.catch(() => {});
        await flushMicrotasks();
        const oldRaw = _lsStore.bc_write_queue_v2;
        const next = JSON.parse(oldRaw).filter(([, d]) => d.kind !== 'member');
        for (const [, d] of next) for (const row of d.payload.subResults || []) if (row.sideAMemberId === member.id) row.sideAMemberId = '';
        const newRaw = JSON.stringify(next);
        _lsStore.bc_write_queue_v2 = newRaw;
        const dropped = [];
        const onDrop = (e) => dropped.push(e.detail);
        window.addEventListener('team-member-dropped', onDrop);
        try {
            window.dispatchEvent(Object.assign(new Event('storage'), { key: 'bc_write_queue_v2', oldValue: oldRaw, newValue: newRaw }));
            await flushMicrotasks();
        } finally {
            window.removeEventListener('team-member-dropped', onDrop);
        }
        expect(dropped, 'this tab\'s editors take the member off too').toEqual([
            { competitionId: 'c1', teamId: 't1', memberId: member.id },
        ]);
        const sent = server(() => answer(200, { ...MATCH }));
        await reconnect();
        const put = sent.find((r) => r.method === 'PUT');
        expect(put, 'the score write still goes').toBeTruthy();
        expect(repRow(put).sideAMemberId || '', 'without the id that names nobody').toBe('');
    });

    it('keeps the id when the add landed', async () => {
        const member = await API.addTeamMember('c1', 't1', 'Ren Abe', 'pw');
        const res = await API.recordScore('c1', 'm1', scoreWithRepPick(member.id), 'pw', MATCH);
        res.settled.catch(() => {});
        await flushMicrotasks();
        const oldRaw = _lsStore.bc_write_queue_v2;
        const newRaw = JSON.stringify(JSON.parse(oldRaw).filter(([, d]) => d.kind !== 'member'));
        _lsStore.bc_write_queue_v2 = newRaw;
        const dropped = [];
        const onDrop = (e) => dropped.push(e.detail);
        window.addEventListener('team-member-dropped', onDrop);
        try {
            window.dispatchEvent(Object.assign(new Event('storage'), { key: 'bc_write_queue_v2', oldValue: oldRaw, newValue: newRaw }));
            await flushMicrotasks();
        } finally {
            window.removeEventListener('team-member-dropped', onDrop);
        }
        expect(dropped).toEqual([]);
        const sent = server(() => answer(200, { ...MATCH }));
        await reconnect();
        expect(repRow(sent.find((r) => r.method === 'PUT')).sideAMemberId).toBe(member.id);
    });
});
