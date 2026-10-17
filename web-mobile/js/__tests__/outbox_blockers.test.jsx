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
        const sent = server((req) => (req.method === 'POST' ? answer(503, { error: 'unavailable' })
            // K1: the discarded add went out, so the team's members are read: it is not there.
            : req.url.endsWith('/team-members') ? answer(200, { teamMembers: { t1: [] } }) : lineupOk(req)));
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

describe('the write a match waits on is named by where it is (H5)', () => {
    async function wording() {
        return import('../write_result.jsx');
    }

    it('names another match by its label, after a reload too, and says a wait cycle is one', async () => {
        const shown = (id, n) => ({ id, compId: 'c1', phase: 'bracket', matchNumber: n, status: 'running', modifiedAt: 0 });
        for (const [m, n] of [['ma', 6], ['mb', 7]]) {
            const r = await API.recordScore('c1', m, { status: 'running', ipponsA: ['M'], ipponsB: [], changed: ['points'] }, 'pw', shown(m, n));
            r.settled.catch(() => {});
        }
        await flushMicrotasks();
        const q = stored();
        const key = (m) => q.find(([k]) => k.startsWith(`c1:${m}\u001f`))[0];
        for (const [k, d] of q) d.after = k === key('ma') ? key('mb') : key('ma');
        API.clearQueue();
        _lsStore.bc_write_queue_v2 = JSON.stringify(q);
        await load();
        await reconnect();
        const blocker = API.heldWriteBlocker('c1', 'ma');
        expect(blocker).toMatchObject({ matchID: 'mb', where: 'Match 7', cycled: true });
        const w = await wording();
        expect(w.queuedBlockedNotice(blocker)).toContain('Match 7');
        expect(w.queuedBlockedNotice(blocker), 'a cycle is not a refusal').not.toContain('keeps refusing');
        expect(w.heldWriteDiscardLabel(blocker)).toBe('Discard held score update (Match 7)');
        expect(w.heldWriteDiscardConfirm(blocker).message).toContain('score update (Match 7)');
        expect(w.heldWriteDiscardConfirm(blocker).message).not.toContain('keeps refusing');
    });

    it('names a team\'s write by the team, from a match of that team shown on this device', async () => {
        const warned = vi.spyOn(console, 'warn').mockImplementation(() => {});
        server((req) => (req.method === 'POST' ? answer(503, { error: 'unavailable' }) : lineupOk(req)));
        const member = await API.addTeamMember('c1', 't1', 'Ren Abe', 'pw');
        const res = await API.recordScore('c1', 'm1', scoreWithRepPick(member.id), 'pw', MATCH);
        res.settled.catch(() => {});
        for (let i = 0; i < 14 && !API.heldWrites().some((h) => h.kind === 'member' && h.keepsFailing); i++) await reconnect();
        const blocker = API.heldWriteBlocker('c1', 'm1');
        expect(blocker).toMatchObject({ kind: 'member', where: 'Team One' });
        const w = await wording();
        expect(w.queuedBlockedNotice(blocker)).toContain('team member change (Team One) the server keeps refusing');
        expect(w.heldWriteDiscardLabel(blocker)).toBe('Discard held team member change (Team One)');
        warned.mockRestore();
    });
});

describe('a match that does not say its phase is not labelled (H5)', () => {
    it('names no match rather than read a bracket id as a pool match', async () => {
        for (const m of ['m-2', 'm-3']) {
            const r = await API.recordScore('c1', m, { status: 'running', ipponsA: ['M'], ipponsB: [], changed: ['points'] }, 'pw', { id: m, compId: 'c1', status: 'running', modifiedAt: 0 });
            r.settled.catch(() => {});
        }
        await flushMicrotasks();
        const q = stored();
        const key = (m) => q.find(([k]) => k.startsWith(`c1:${m}\u001f`))[0];
        for (const [k, d] of q) d.after = k === key('m-2') ? key('m-3') : key('m-2');
        API.clearQueue();
        _lsStore.bc_write_queue_v2 = JSON.stringify(q);
        await load();
        await reconnect();
        expect(API.heldWriteBlocker('c1', 'm-2')).toMatchObject({ matchID: 'm-3', where: '' });
    });
});

describe('a tap batch ends at the first tap that waits (J1, stage 3 review 6)', () => {
    const tapsOf = (req) => (req.body.events || []).map((e) => e.letter || e.kind);

    it('two taps of one match with a send back between them go as M, the send back, then K', async () => {
        const blk = { id: 'blk', compId: 'c1', status: 'running', court: 'A', modifiedAt: 1, sideA: { id: 'p1', name: 'Kato' }, sideB: { id: 'p2', name: 'Sato' } };
        API.recordTap('c1', 'blk', { kind: 'ippon.add', pos: 0, side: 'a', letter: 'M' }, 'pw', blk);
        await flushMicrotasks();
        await tick(5);
        const r = await API.requeueBlockerAndReopen('c1', 'tgt', 'c1', 'blk', 'pw', {});
        if (r && r.settled) r.settled.catch(() => {});
        await tick(5);
        API.recordTap('c1', 'blk', { kind: 'ippon.add', pos: 0, side: 'b', letter: 'K' }, 'pw', blk);
        await flushMicrotasks();
        const sent = server((req) => (req.url.endsWith('/score-events')
            ? answer(200, { outcomes: req.body.events.map((e) => ({ id: e.id, outcome: 'applied' })) })
            : answer(200, { applied: true })));
        for (let i = 0; i < 6 && stored().length > 0; i++) await reconnect();
        expect(sent.map((req) => (req.url.endsWith('/score-events') ? `taps ${tapsOf(req).join('')}` : 'requeue')),
            'the second tap is not carried past the send back it follows').toEqual(['taps M', 'requeue', 'taps K']);
        expect(stored()).toEqual([]);
    });

    for (const refuse of [false, true]) {
        it(`a tap naming a minted member goes after its add (add ${refuse ? 'refused' : 'landed'})`, async () => {
            const warned = vi.spyOn(console, 'warn').mockImplementation(() => {});
            API.recordTap('c1', 'm1', { kind: 'ippon.add', pos: 1, side: 'a', letter: 'M' }, 'pw', MATCH);
            await flushMicrotasks();
            await tick(5);
            const member = await API.addTeamMember('c1', 't1', 'Ren Abe', 'pw', { onRefusal: () => true });
            if (member && member.settled) member.settled.catch(() => {});
            await tick(5);
            API.recordTap('c1', 'm1', { kind: 'rep.pick', pos: -1, side: 'a', memberId: member.id }, 'pw', MATCH);
            await flushMicrotasks();
            const sent = server((req) => {
                if (req.url.endsWith('/score-events')) return answer(200, { outcomes: req.body.events.map((e) => ({ id: e.id, outcome: 'applied' })), match: MATCH });
                return refuse ? answer(409, { error: 'team_member_limit', message: 'No room.' }) : answer(201, { id: req.body.id, name: req.body.name, index: 3, modifiedAt: 9 });
            });
            for (let i = 0; i < 6 && stored().length > 0; i++) await reconnect();
            expect(sent.map((req) => (req.url.endsWith('/members') ? 'add' : `taps ${tapsOf(req).join(',')}`)),
                'the pick is sent after the add').toEqual(['taps M', 'add', 'taps rep.pick']);
            const pickReq = sent[2].body.events[0];
            if (refuse) expect(pickReq.memberId, 'a refused add takes its id off the pick').toBeUndefined();
            else expect(pickReq.memberId).toBe(member.id);
            warned.mockRestore();
        });
    }
});

describe('settling a tap batch stores the queue once (J2, stage 3 review 6)', () => {
    it('writes the stored queue a bounded number of times, whatever the batch size', async () => {
        for (let i = 0; i < 40; i++) API.recordTap('c1', 'm1', { kind: 'encho.add', pos: 0 }, 'pw', MATCH);
        await flushMicrotasks();
        expect(stored()).toHaveLength(40);
        let writes = 0;
        const { setItem, removeItem } = global.localStorage;
        global.localStorage.setItem = (k, v) => { if (k === 'bc_write_queue_v2') writes++; setItem(k, v); };
        global.localStorage.removeItem = (k) => { if (k === 'bc_write_queue_v2') writes++; removeItem(k); };
        const sent = server((req) => answer(200, { outcomes: req.body.events.map((e) => ({ id: e.id, outcome: 'applied' })) }));
        await reconnect();
        expect(sent, 'the 40 taps go in one POST').toHaveLength(1);
        expect(stored(), 'all settled').toEqual([]);
        expect(writes, 'one store for the batch, not one per tap').toBeLessThanOrEqual(2);
    });
});

describe('a write naming a minted member follows the team\'s last member write (J3)', () => {
    it('not the team\'s last entry, which may be a starting lineup save', async () => {
        const member = await API.addTeamMember('c1', 't1', 'Ren Abe', 'pw');
        const lineup = await API.putTeamLineup('c1', 't1', 0, { senpo: 'Sato' }, 'pw', undefined, ['senpo']);
        if (lineup && lineup.settled) lineup.settled.catch(() => {});
        const res = await API.recordScore('c1', 'm1', scoreWithRepPick(member.id), 'pw', MATCH);
        res.settled.catch(() => {});
        await flushMicrotasks();
        const q = stored();
        const add = q.find(([, d]) => d.kind === 'member');
        const score = q.find(([, d]) => d.kind === 'score' || (d.url || '').endsWith('/score'));
        expect(q.some(([, d]) => d.kind === 'lineup'), 'the team lineup save is queued in the same base').toBe(true);
        expect(score[1].after, 'the score write follows the add, the team\'s last member write').toBe(add[0]);
    });
});

describe('a regenerated or discarded draw forgets the match labels learned for it (J4)', () => {
    const shown = (id, n) => ({ id, compId: 'c1', phase: 'bracket', matchNumber: n, status: 'running', modifiedAt: 0 });
    const whereOf = (m) => (API.heldWrites().find((h) => h.matchID === m) || {}).where;

    async function labelled() {
        // Queued with no phase, so the entry itself carries no label: only the cache can name it.
        const r = await API.recordScore('c1', 'mb', { status: 'running', ipponsA: ['M'], ipponsB: [], changed: ['points'] }, 'pw', { id: 'mb', modifiedAt: 0 });
        r.settled.catch(() => {});
        const other = await API.recordScore('c2', 'mb', { status: 'running', ipponsA: ['M'], ipponsB: [], changed: ['points'] }, 'pw', { id: 'mb', modifiedAt: 0 });
        other.settled.catch(() => {});
        API.noteShownMatch('c1', shown('mb', 7));
        API.noteShownMatch('c2', shown('mb', 9));
        await flushMicrotasks();
        expect(whereOf('mb')).toBe('Match 7');
    }

    for (const type of ['draw_generated', 'draw_discarded']) {
        it(`the ${type} push clears that competition's labels and no other's`, async () => {
            let es;
            global.EventSource = class FakeES { constructor() { es = this; this.close = () => {}; } };
            global.EventSource.OPEN = 1;
            await load();
            const off = API.subscribeToEvents(() => {});
            await labelled();
            es.onmessage({ data: JSON.stringify({ type, data: { competitionId: 'c1' } }), lastEventId: '' });
            expect(whereOf('mb'), 'the label of a match the new draw may reuse the id of is gone').toBe('');
            const c2 = API.heldWrites().find((h) => h.compID === 'c2' || h.compId === 'c2');
            expect(c2 && c2.where, 'another competition keeps its label').toBe('Match 9');
            if (typeof off === 'function') off();
        });
    }

    // Stage 3 review 7, K8: an import (the competitions a bundle brings) or a
    // reset clears every competition's labels. Another device's import is
    // heard only as tournament_updated; a reset as password_reset.
    for (const type of ['tournament_updated', 'password_reset']) {
        it(`the ${type} push clears every competition's labels (K8)`, async () => {
            let es;
            global.EventSource = class FakeES { constructor() { es = this; this.close = () => {}; } };
            global.EventSource.OPEN = 1;
            await load();
            const off = API.subscribeToEvents(() => {});
            await labelled();
            es.onmessage({ data: JSON.stringify({ type, data: null }), lastEventId: '' });
            expect(whereOf('mb')).toBe('');
            const c2 = API.heldWrites().find((h) => h.compID === 'c2' || h.compId === 'c2');
            expect(c2 && c2.where, 'no competition keeps its labels').toBe('');
            if (typeof off === 'function') off();
        });
    }

    it('an import or a reset from this device clears every competition\'s labels (K8)', async () => {
        const calls = [
            [/\/api\/tournament\/import$/, (a) => a.importCompetitions(new FormData(), 'pw')],
            [/\/api\/tournament\/reset$/, (a) => a.resetPassword('new-pw', 'tab-1')],
        ];
        for (const [route, call] of calls) {
            global.fetch = vi.fn((url) => (route.test(String(url))
                ? Promise.resolve({ ok: true, status: 200, headers: { get: () => null }, json: async () => ({ results: [] }) })
                : Promise.reject(new TypeError('offline'))));
            await labelled();
            await call(API);
            expect(whereOf('mb')).toBe('');
            const c2 = API.heldWrites().find((h) => h.compID === 'c2' || h.compId === 'c2');
            expect(c2 && c2.where).toBe('');
            API.clearQueue();
        }
    });

    it('generating and discarding a draw from this device clears them too', async () => {
        for (const call of [(a) => a.generateDraw('c1', 'pw'), (a) => a.discardDraw('c1', 'pw')]) {
            // The draw calls answer; the queued writes stay queued (offline).
            global.fetch = vi.fn((url) => (/\/(generate-draw|draw)$/.test(String(url))
                ? answer(200, {}) : Promise.reject(new TypeError('offline'))));
            await labelled();
            await call(API);
            expect(whereOf('mb')).toBe('');
            API.clearQueue();
        }
    });
});

describe('a refused add takes its id off every tab\'s copies, whichever copies the tab held (J5)', () => {
    const DROPPED_KEY = 'bc_dropped_member_ids_v1';
    const storageEvent = (key, oldValue, newValue) => window.dispatchEvent(Object.assign(new Event('storage'), { key, oldValue, newValue }));

    it('this tab refuses the add before it adopts the other tab\'s write naming the id', async () => {
        const warned = vi.spyOn(console, 'warn').mockImplementation(() => {});
        // The other tab's write, as it is stored (a score write naming a placeholder).
        const s = await API.recordScore('c1', 'm1', scoreWithRepPick('PLACEHOLDER'), 'pw', MATCH);
        s.settled.catch(() => {});
        await flushMicrotasks();
        const [aKey, aEntry] = stored()[0];
        API.clearQueue();
        const member = await API.addTeamMember('c1', 't1', 'Ren Abe', 'pw', { onRefusal: () => true });
        await flushMicrotasks();
        const addKey = stored().find(([, d]) => d.kind === 'member')[0];
        const theirs = JSON.parse(JSON.stringify(aEntry).replace('PLACEHOLDER', member.id));
        theirs.after = addKey;
        const before = _lsStore.bc_write_queue_v2;
        // Stored by the other tab; this tab has not heard of it yet.
        _lsStore.bc_write_queue_v2 = JSON.stringify([...stored(), [aKey, theirs]]);
        const sent = server((req) => (req.method === 'POST' ? answer(409, { error: 'team_member_limit', message: 'No room.' }) : answer(200, { ...MATCH })));
        await reconnect();
        expect(sent.some((r) => r.method === 'POST'), 'the add went and was refused').toBe(true);
        // Now the other tab's write reaches this tab.
        storageEvent('bc_write_queue_v2', before, _lsStore.bc_write_queue_v2);
        await flushMicrotasks();
        await reconnect();
        expect(sent.filter((r) => r.method === 'POST'), 'the refused add is not sent again').toHaveLength(1);
        const put = sent.find((r) => r.method === 'PUT');
        expect(put, 'the other tab\'s score write goes').toBeTruthy();
        expect(repRow(put).sideAMemberId || '', 'without an id that names nobody').toBe('');
        warned.mockRestore();
    });

    it('this tab holds the write and hears of the refusal as the stored list and a dropped-ids note', async () => {
        const member = await API.addTeamMember('c1', 't1', 'Ren Abe', 'pw');
        const res = await API.recordScore('c1', 'm1', scoreWithRepPick(member.id), 'pw', MATCH);
        res.settled.catch(() => {});
        await flushMicrotasks();
        const oldRaw = _lsStore.bc_write_queue_v2;
        // The other tab refused the add before it held this write: the stored copy is unstripped.
        const note = JSON.stringify([{ id: member.id, at: Date.now(), competitionId: 'c1', teamId: 't1' }]);
        _lsStore[DROPPED_KEY] = note;
        const newRaw = JSON.stringify(JSON.parse(oldRaw).filter(([, d]) => d.kind !== 'member'));
        _lsStore.bc_write_queue_v2 = newRaw;
        const dropped = [];
        const onDrop = (e) => dropped.push(e.detail);
        window.addEventListener('team-member-dropped', onDrop);
        try {
            storageEvent(DROPPED_KEY, null, note);
            storageEvent('bc_write_queue_v2', oldRaw, newRaw);
            await flushMicrotasks();
        } finally {
            window.removeEventListener('team-member-dropped', onDrop);
        }
        expect(dropped).toEqual([{ competitionId: 'c1', teamId: 't1', memberId: member.id }]);
        const sent = server(() => answer(200, { ...MATCH }));
        await reconnect();
        expect(repRow(sent.find((r) => r.method === 'PUT')).sideAMemberId || '').toBe('');
    });

    it('a note is pruned once nothing names its id and it has been kept a while', async () => {
        const warned = vi.spyOn(console, 'warn').mockImplementation(() => {});
        const member = await API.addTeamMember('c1', 't1', 'Ren Abe', 'pw', { onRefusal: () => true });
        server(() => answer(409, { error: 'team_member_limit', message: 'No room.' }));
        await reconnect();
        expect(JSON.parse(_lsStore[DROPPED_KEY] || '[]').map((n) => n.id), 'the refusal leaves a note').toEqual([member.id]);
        await tick(11 * 60 * 1000);
        server((req) => answer(201, { id: req.body.id, name: req.body.name, index: 3, modifiedAt: 9 }));
        const late = await API.addTeamMember('c1', 't1', 'Aya Ito', 'pw');
        late.settled.catch(() => {});
        await flushMicrotasks();
        expect(_lsStore[DROPPED_KEY], 'old and unnamed: gone').toBeUndefined();
        warned.mockRestore();
    });
});

describe('a long single-match queue is checked for wait cycles without deep recursion (J6)', () => {
    it('20000 taps of one match finish a pass and none reads as on a cycle', async () => {
        API.recordTap('c1', 'm1', { kind: 'ippon.add', pos: 0, side: 'a', letter: 'M' }, 'pw', MATCH);
        await flushMicrotasks();
        const [k0, d0] = stored()[0];
        const base = k0.split('\u001f')[0];
        const big = [];
        for (let i = 0; i < 20000; i++) {
            const d = { ...d0, payload: { ...d0.payload, id: `e${i}`, stamp: d0.payload.stamp + i, seq: i + 1 }, enqueuedAt: d0.enqueuedAt + i };
            big.push([`${base}\u001fevt:e${i}`, d]);
        }
        API.clearQueue();
        // Newest first: the search along the chain starts from its far end.
        _lsStore.bc_write_queue_v2 = JSON.stringify(big.toReversed());
        await load();
        const failures = [];
        const onFail = (e) => failures.push(e);
        process.on('unhandledRejection', onFail);
        try {
            // Offline: the pass groups and checks the queue, then every send fails.
            await reconnect();
            await tick(2000);
        } finally {
            process.off('unhandledRejection', onFail);
        }
        expect(failures.map(String), 'the pass did not throw').toEqual([]);
        expect(API.heldWrites().filter((h) => h.cycled), 'a chain is not a cycle').toEqual([]);
        expect(stored().filter(([, d]) => d.cycled)).toEqual([]);
        expect(stored(), 'every tap still queued, in order').toHaveLength(20000);
    }, 60000);
});

// Stage 3 review 7, K6: settling a batch of taps does its follow-ups (the
// match's pending taps published, the writes following a settled one kicked)
// once for the batch, not once per tap over the whole queue. Counted as work
// done: reads of the entries' `after` and pending-tap publications.
describe('settling a tap batch is linear in the queue (K6)', () => {
    it('a long queue of taps drains with work proportional to its length', async () => {
        const N = 640;
        API.recordTap('c1', 'm1', { kind: 'ippon.add', pos: 0, side: 'a', letter: 'M' }, 'pw', MATCH);
        await flushMicrotasks();
        const [k0, d0] = stored()[0];
        const base = k0.split('\u001f')[0];
        const big = [];
        for (let i = 0; i < N; i++) {
            const d = { ...d0, payload: { ...d0.payload, id: `e${i}`, stamp: d0.payload.stamp + i, seq: i + 1 }, enqueuedAt: d0.enqueuedAt + i };
            big.push([`${base}\u001fevt:e${i}`, d]);
        }
        API.clearQueue();
        _lsStore.bc_write_queue_v2 = JSON.stringify(big);
        // Every queued entry read from storage counts the reads of its `after`.
        let afterReads = 0;
        const realParse = JSON.parse;
        const spy = vi.spyOn(JSON, 'parse').mockImplementation((s, r) => {
            const v = realParse(s, r);
            if (Array.isArray(v)) {
                for (const item of v) {
                    const d = Array.isArray(item) && item[1];
                    if (!d || typeof d !== 'object' || d.kind !== 'evt') continue;
                    let after = d.after;
                    delete d.after;
                    Object.defineProperty(d, 'after', {
                        get() { afterReads++; return after; }, set(x) { after = x; }, enumerable: true, configurable: true,
                    });
                }
            }
            return v;
        });
        try {
            await load();
            let published = 0;
            const off = API.subscribePendingTaps('c1', 'm1', () => { published++; });
            const sent = server((req) => answer(200, { outcomes: req.body.events.map((e) => ({ id: e.id, outcome: 'applied' })) }));
            afterReads = 0;
            await reconnect();
            await tick(2000);
            if (typeof off === 'function') off();
            expect(stored(), 'all settled').toEqual([]);
            expect(sent.length, 'in batches of 64').toBe(Math.ceil(N / 64));
            expect(published, 'the pending taps are published once per batch').toBeLessThanOrEqual(2 * sent.length + 2);
            expect(afterReads, 'each batch reads the queue a bounded number of times').toBeLessThan(40 * N);
        } finally {
            spy.mockRestore();
        }
    }, 60000);
});

// Stage 3 review 7, K2: a tap this tab has settled is out of storage before
// anything awaits, and is never taken up again from another tab's write.
describe('a settled tap is never sent again because another tab wrote the queue (K2)', () => {
    const storageEvent = (oldValue, newValue) => window.dispatchEvent(Object.assign(new Event('storage'), { key: 'bc_write_queue_v2', oldValue, newValue }));
    const otherTabEntry = (list) => {
        const [k0, d0] = list[0];
        return [k0.split('\u001f')[0].replace('m1', 'm9') + '\u001fx',
            { ...d0, matchID: 'm9', url: d0.url.replace('m1', 'm9'), payload: { ...d0.payload, id: 'other.1' }, enqueuedAt: d0.enqueuedAt + 5 }];
    };

    it('the taps settled before a clock_skew tap leave storage before the clock is read', async () => {
        const t1 = API.recordTap('c1', 'm1', { kind: 'ippon.add', pos: 0, side: 'a', letter: 'M' }, 'pw', MATCH);
        const t2 = API.recordTap('c1', 'm1', { kind: 'ippon.add', pos: 0, side: 'b', letter: 'K' }, 'pw', MATCH);
        await flushMicrotasks();
        let releaseTime;
        const timeP = new Promise((r) => { releaseTime = r; });
        const posts = [];
        global.fetch = vi.fn((url, opts) => {
            if (String(url).includes('/api/time')) return timeP.then(() => answer(200, { nowMs: Date.now() }));
            const body = JSON.parse(opts.body);
            posts.push(body.events.map((e) => e.id));
            return answer(200, { outcomes: body.events.map((e) => ({ id: e.id, outcome: e.id === t2.id && posts.length === 1 ? 'clock_skew' : 'applied' })) });
        });
        window.dispatchEvent(new Event('online'));
        await tick(10);
        const ids = stored().map(([, d]) => d.payload && d.payload.id);
        expect(ids, 'the settled tap is out of storage while the clock is read').not.toContain(t1.id);
        // Another tab writes the queue meanwhile, from what storage holds.
        const oldRaw = _lsStore.bc_write_queue_v2;
        const list = JSON.parse(oldRaw);
        _lsStore.bc_write_queue_v2 = JSON.stringify([...list, otherTabEntry(list)]);
        storageEvent(oldRaw, _lsStore.bc_write_queue_v2);
        await flushMicrotasks();
        releaseTime();
        for (let i = 0; i < 6; i++) await tick(2000);
        expect(posts.filter((p) => p.includes(t1.id)), 'the settled tap is sent once').toHaveLength(1);
    });

    it('a tab writing back its stale copy of a settled tap does not make this tab send it again', async () => {
        const t1 = API.recordTap('c1', 'm1', { kind: 'ippon.add', pos: 0, side: 'a', letter: 'M' }, 'pw', MATCH);
        await flushMicrotasks();
        const stale = stored();
        const posts = [];
        global.fetch = vi.fn((url, opts) => {
            if (String(url).includes('/api/time')) return Promise.reject(new TypeError('clock'));
            const body = JSON.parse(opts.body);
            posts.push(body.events.map((e) => e.id));
            return answer(200, { outcomes: body.events.map((e) => ({ id: e.id, outcome: 'applied' })) });
        });
        await reconnect();
        expect(stored(), 'settled and stored so').toEqual([]);
        // The other tab held a copy and writes it back with a write of its own.
        const newRaw = JSON.stringify([...stale, otherTabEntry(stale)]);
        _lsStore.bc_write_queue_v2 = newRaw;
        storageEvent(JSON.stringify([]), newRaw);
        await flushMicrotasks();
        await reconnect();
        expect(posts.filter((p) => p.includes(t1.id)), 'sent once, by this tab').toHaveLength(1);
        expect(posts.some((p) => p.includes('other.1')), 'the other tab\'s own write is still taken up').toBe(true);
    });
});

// Stage 3 review 7, K1: only a member add the server provably never kept takes
// its id off the writes naming the member. A discard of an add that went out
// (a 5xx can be a change the server already kept) asks the server first.
describe('a discarded add that went out is checked against the team\'s members (K1)', () => {
    const EVENTS = '/api/competitions/c1/matches/m1/score-events';
    const TEAM_MEMBERS = '/api/competitions/c1/team-members';
    const DROPPED_KEY = 'bc_dropped_member_ids_v1';
    const pick = (memberId) => ({ kind: 'rep.pick', pos: -1, side: 'a', memberId });
    let warned = null;
    beforeEach(() => { warned = vi.spyOn(console, 'warn').mockImplementation(() => {}); });
    afterEach(() => { warned.mockRestore(); });

    // The add goes, is answered 500 until it keeps failing, and is discarded.
    async function discardAfterFailures(members) {
        const member = await API.addTeamMember('c1', 't1', 'Ren Abe', 'pw', { onRefusal: () => true });
        member.settled.catch(() => {});
        const listed = { current: members };
        const sent = server((req) => {
            if (req.url === MEMBERS) return answer(500, { error: 'internal error' });
            if (req.url === TEAM_MEMBERS) {
                const list = typeof listed.current === 'function' ? listed.current(member) : listed.current;
                return list === null ? null : answer(200, { teamMembers: list });
            }
            if (req.url === EVENTS) return answer(200, { outcomes: req.body.events.map((e) => ({ id: e.id, outcome: 'applied' })), match: MATCH });
            return lineupOk(req);
        });
        for (let i = 0; i < 20 && !API.heldWrites().some((h) => h.kind === 'member' && h.keepsFailing); i++) {
            await reconnect();
            await tick(9000);
        }
        const add = API.heldWrites().find((h) => h.kind === 'member');
        expect(add && add.keepsFailing, 'the add keeps failing').toBe(true);
        return { member, sent, add, listed };
    }

    it('the server holds the member: a pick made later still names them, and no note is left', async () => {
        const { member, sent, add } = await discardAfterFailures((m) => ({ t1: [{ id: m.id, name: 'Ren Abe' }] }));
        expect(API.discardHeldWrite(add.key)).toBeTruthy();
        await tick(10);
        expect(sent.some((r) => r.url === TEAM_MEMBERS), 'the team\'s members are read again').toBe(true);
        expect(_lsStore[DROPPED_KEY], 'nothing proves the member never was').toBeUndefined();
        await tick(60 * 1000);
        API.recordTap('c1', 'm1', pick(member.id), 'pw', MATCH);
        await tick(2000);
        const post = sent.find((r) => r.url === EVENTS);
        expect(post, 'the pick goes').toBeTruthy();
        expect(post.body.events[0].memberId, 'and names the member the server holds').toBe(member.id);
    });

    it('a write naming the member waits while the read is out; a read that fails strips nothing', async () => {
        const { member, sent, add } = await discardAfterFailures(null);
        API.recordTap('c1', 'm1', pick(member.id), 'pw', MATCH);
        await flushMicrotasks();
        expect(API.discardHeldWrite(add.key)).toBeTruthy();
        await tick(2000);
        expect(sent.some((r) => r.url === EVENTS), 'not sent while the read is out').toBe(false);
        // The read that hangs is given up at its deadline: a failure, which
        // proves nothing, so the pick goes as it was made and the server judges it.
        await tick(15000);
        await reconnect();
        const post = sent.find((r) => r.url === EVENTS);
        expect(post, 'the pick goes once the read is over').toBeTruthy();
        expect(post.body.events[0].memberId, 'with its id: nothing proved the member absent').toBe(member.id);
        expect(_lsStore[DROPPED_KEY]).toBeUndefined();
    });

    it('the read answers without the member: the waiting pick goes without the id, and a note is left', async () => {
        const { member, sent, add } = await discardAfterFailures({ t1: [{ id: 'someone-else', name: 'Sato' }] });
        API.recordTap('c1', 'm1', pick(member.id), 'pw', MATCH);
        await flushMicrotasks();
        expect(API.discardHeldWrite(add.key)).toBeTruthy();
        await tick(10);
        await reconnect();
        const post = sent.find((r) => r.url === EVENTS);
        expect(post, 'the pick goes once the read has answered').toBeTruthy();
        expect(post.body.events[0].memberId, 'the pick that named nobody is cleared').toBeUndefined();
        expect(JSON.parse(_lsStore[DROPPED_KEY] || '[]').map((n) => n.id)).toEqual([member.id]);
    });

    it('an add refused member_id_conflict is checked too: the id is held, by this team or another', async () => {
        const member = await API.addTeamMember('c1', 't1', 'Ren Abe', 'pw', { onRefusal: () => true });
        member.settled.catch(() => {});
        API.recordTap('c1', 'm1', pick(member.id), 'pw', MATCH);
        await flushMicrotasks();
        const sent = server((req) => {
            if (req.url === MEMBERS) return answer(409, { error: 'member_id_conflict', message: 'Another team member already has this id.' });
            if (req.url === TEAM_MEMBERS) return answer(200, { teamMembers: { t1: [{ id: member.id, name: 'Ren Abe-Sato' }] } });
            return answer(200, { outcomes: req.body.events.map((e) => ({ id: e.id, outcome: 'applied' })), match: MATCH });
        });
        await reconnect();
        await reconnect();
        const post = sent.find((r) => r.url === EVENTS);
        expect(post.body.events[0].memberId, 'this team holds the id: the pick keeps it').toBe(member.id);
        expect(_lsStore[DROPPED_KEY]).toBeUndefined();
    });

    it('a note is applied only to writes queued before it', async () => {
        const res = await API.recordScore('c1', 'm1', scoreWithRepPick('mem-9'), 'pw', MATCH);
        res.settled.catch(() => {});
        await flushMicrotasks();
        _lsStore[DROPPED_KEY] = JSON.stringify([{ id: 'mem-9', at: Date.now() - 5000, competitionId: 'c1', teamId: 't1' }]);
        const sent = server(() => answer(200, { ...MATCH }));
        await reconnect();
        expect(repRow(sent.find((r) => r.method === 'PUT')).sideAMemberId, 'made after the proof: the server judges it').toBe('mem-9');
    });

    it('a member list that carries a noted id forgets the note', async () => {
        _lsStore[DROPPED_KEY] = JSON.stringify([
            { id: 'mem-9', at: Date.now(), competitionId: 'c1', teamId: 't1' },
            { id: 'mem-8', at: Date.now(), competitionId: 'c1', teamId: 't1' },
        ]);
        global.fetch = vi.fn(() => answer(200, { teamMembers: { t1: [{ id: 'mem-9', name: 'Ren Abe' }] } }));
        await API.fetchSquads('c1', 'pw');
        expect(JSON.parse(_lsStore[DROPPED_KEY]).map((n) => n.id)).toEqual(['mem-8']);
    });

    // Stage 3 review 8, L4: a note is forgotten only by a list of its own
    // competition and team.
    it('a member list forgets only the notes of its own competition and team (L4)', async () => {
        _lsStore[DROPPED_KEY] = JSON.stringify([
            { id: 'mem-9', at: Date.now(), competitionId: 'c1', teamId: 't1' },
            { id: 'mem-6', at: Date.now(), competitionId: 'c2', teamId: 't1' },
            { id: 'mem-7', at: Date.now(), competitionId: 'c1', teamId: 't2' },
        ]);
        global.fetch = vi.fn(() => answer(200, { teamMembers: { t1: [{ id: 'mem-9' }, { id: 'mem-6' }, { id: 'mem-7' }] } }));
        await API.fetchSquads('c1', 'pw');
        expect(JSON.parse(_lsStore[DROPPED_KEY] || '[]').map((n) => n.id), 'another competition\'s and another team\'s notes stand')
            .toEqual(['mem-6', 'mem-7']);
    });
});

// Stage 3 review 8, L1: an add discarded while its request is out may still be
// kept by the server when that request ends, so nothing is judged until then.
describe('an add discarded while its request is out is judged once the request ends (L1)', () => {
    const EVENTS = '/api/competitions/c1/matches/m1/score-events';
    const TEAM_MEMBERS = '/api/competitions/c1/team-members';
    const DROPPED_KEY = 'bc_dropped_member_ids_v1';
    const pick = (memberId) => ({ kind: 'rep.pick', pos: -1, side: 'a', memberId });
    let warned = null;
    let dropped = [];
    const onDropped = (e) => dropped.push(e.detail);
    beforeEach(() => {
        warned = vi.spyOn(console, 'warn').mockImplementation(() => {});
        dropped = [];
        window.addEventListener('team-member-dropped', onDropped);
    });
    afterEach(() => {
        warned.mockRestore();
        window.removeEventListener('team-member-dropped', onDropped);
    });

    // The add fails until it keeps failing, goes out again and hangs; it is
    // discarded while out, and a pick naming the member is made.
    async function discardWhileOut() {
        const member = await API.addTeamMember('c1', 't1', 'Ren Abe', 'pw', { onRefusal: () => true });
        member.settled.catch(() => {});
        const state = { hang: false, release: null };
        const sent = server((req) => {
            if (req.url === MEMBERS) {
                if (!state.hang) return answer(500, { error: 'internal error' });
                return new Promise((r) => { state.release = r; });
            }
            if (req.url === TEAM_MEMBERS) return answer(200, { teamMembers: { t1: [] } });
            if (req.url === EVENTS) return answer(200, { outcomes: req.body.events.map((e) => ({ id: e.id, outcome: 'applied' })), match: MATCH });
            return lineupOk(req);
        });
        for (let i = 0; i < 20 && !API.heldWrites().some((h) => h.kind === 'member' && h.keepsFailing); i++) {
            await reconnect();
            await tick(9000);
        }
        state.hang = true;
        for (let i = 0; i < 20 && !state.release; i++) await tick(3000);
        expect(state.release, 'the add is out again').toBeTruthy();
        const add = API.heldWrites().find((h) => h.kind === 'member');
        expect(API.discardHeldWrite(add.key), 'the discard is taken at once').toBeTruthy();
        API.recordTap('c1', 'm1', pick(member.id), 'pw', MATCH);
        await tick(10);
        return { member, sent, release: (status, body) => state.release(answer(status, body)) };
    }

    it('nothing is read or noted while the add is out; a 2xx then leaves the pick its id', async () => {
        const { member, sent, release } = await discardWhileOut();
        expect(sent.some((r) => r.url === TEAM_MEMBERS), 'no members read while the add is out').toBe(false);
        expect(_lsStore[DROPPED_KEY], 'no note while the add is out').toBeUndefined();
        release(201, { id: member.id, name: 'Ren Abe', modifiedAt: 5 });
        await tick(10);
        await reconnect();
        expect(sent.some((r) => r.url === TEAM_MEMBERS), 'an add that landed needs no read').toBe(false);
        const post = sent.find((r) => r.url === EVENTS);
        expect(post, 'the pick goes').toBeTruthy();
        expect(post.body.events[0].memberId, 'naming the member the server kept').toBe(member.id);
        expect(_lsStore[DROPPED_KEY]).toBeUndefined();
        expect(dropped).toEqual([]);
    });

    it('a late 2xx forgets a note naming the member', async () => {
        const { member, release } = await discardWhileOut();
        _lsStore[DROPPED_KEY] = JSON.stringify([{ id: member.id, at: Date.now(), competitionId: 'c1', teamId: 't1' }]);
        release(201, { id: member.id, name: 'Ren Abe', modifiedAt: 5 });
        await tick(10);
        expect(JSON.parse(_lsStore[DROPPED_KEY] || '[]'), 'the server holds the member').toEqual([]);
    });

    it('a 5xx after the discard: the members are read then, and the pick waits for that read', async () => {
        const { member, sent, release } = await discardWhileOut();
        expect(sent.some((r) => r.url === TEAM_MEMBERS), 'no members read while the add is out').toBe(false);
        release(500, { error: 'internal error' });
        await tick(10);
        await reconnect();
        const read = sent.findIndex((r) => r.url === TEAM_MEMBERS);
        const post = sent.findIndex((r) => r.url === EVENTS);
        expect(read, 'the members are read once the add has failed').toBeGreaterThanOrEqual(0);
        expect(post, 'the pick goes after the read').toBeGreaterThan(read);
        expect(sent[post].body.events[0].memberId, 'the read found no such member').toBeUndefined();
        expect(dropped).toEqual([{ competitionId: 'c1', teamId: 't1', memberId: member.id }]);
    });
});

// Stage 3 review 8, L3: the public page sends the password header EMPTY, never
// omits it, and reads a team's members from the public competition payload.
describe('a discarded add is checked through the door its caller may read (L3)', () => {
    const EVENTS = '/api/competitions/c1/matches/m1/score-events';
    const TEAM_MEMBERS = '/api/competitions/c1/team-members';
    const VIEWER = '/api/viewer/competitions/c1';
    const DROPPED_KEY = 'bc_dropped_member_ids_v1';
    const pick = (memberId) => ({ kind: 'rep.pick', pos: -1, side: 'a', memberId });
    let warned = null;
    beforeEach(() => { warned = vi.spyOn(console, 'warn').mockImplementation(() => {}); });
    afterEach(() => { warned.mockRestore(); });

    async function discardAfterFailures(password, reads, answerRead) {
        const member = await API.addTeamMember('c1', 't1', 'Ren Abe', password, { onRefusal: () => true });
        member.settled.catch(() => {});
        API.recordTap('c1', 'm1', pick(member.id), password, MATCH);
        const posts = [];
        global.fetch = vi.fn((url, opts) => {
            url = String(url);
            const headers = (opts && opts.headers) || {};
            if (url.includes('/api/time')) return Promise.reject(new TypeError('clock'));
            if (url === MEMBERS) return answer(500, { error: 'internal error' });
            if (url === TEAM_MEMBERS || url === VIEWER) {
                reads.push([url, 'X-Tournament-Password' in headers ? headers['X-Tournament-Password'] : 'omitted']);
                return answerRead(url, member);
            }
            const body = JSON.parse(opts.body);
            if (url === EVENTS) posts.push(body);
            return answer(200, { outcomes: (body.events || []).map((e) => ({ id: e.id, outcome: 'applied' })), match: MATCH });
        });
        for (let i = 0; i < 20 && !API.heldWrites().some((h) => h.kind === 'member' && h.keepsFailing); i++) {
            await reconnect();
            await tick(9000);
        }
        const add = API.heldWrites().find((h) => h.kind === 'member');
        expect(API.discardHeldWrite(add.key)).toBeTruthy();
        await tick(10);
        await reconnect();
        return { member, posts };
    }

    it('a self-run add is checked against the public payload, with the header sent empty', async () => {
        const reads = [];
        const { posts } = await discardAfterFailures('', reads, (url) => (url === VIEWER
            ? answer(200, { id: 'c1', teamMembers: { t1: [{ id: 'mem-2', name: 'Sato' }] } })
            : answer(401, { error: 'unauthorized' })));
        expect(reads, 'one read, of the public payload, the header present and empty').toEqual([[VIEWER, '']]);
        expect(posts[0].events[0].memberId, 'the public payload proves the member never was').toBeUndefined();
    });

    it('an organiser whose read is refused 401 (a stale password) strips nothing', async () => {
        const reads = [];
        const { member, posts } = await discardAfterFailures('pw', reads, () => answer(401, { error: 'unauthorized' }));
        expect(reads).toEqual([[TEAM_MEMBERS, 'pw']]);
        expect(_lsStore[DROPPED_KEY], 'a refused read proves nothing').toBeUndefined();
        expect(posts[0].events[0].memberId, 'the pick goes as it was made').toBe(member.id);
    });
});

// Stage 3 review 8, L5: what this tab settled is remembered long enough to
// cover a stale copy another tab writes back, then forgotten; an entry with no
// stamp is never remembered so that it blocks a later copy.
describe('what this tab settled is remembered for a while only (L5)', () => {
    const storageEvent = (oldValue, newValue) => window.dispatchEvent(Object.assign(new Event('storage'), { key: 'bc_write_queue_v2', oldValue, newValue }));
    let warned = null;
    beforeEach(() => { warned = vi.spyOn(console, 'warn').mockImplementation(() => {}); });
    afterEach(() => { warned.mockRestore(); });

    it('a stale copy written back long after the settle is taken up as any entry', async () => {
        API.recordTap('c1', 'm1', { kind: 'ippon.add', pos: 0, side: 'a', letter: 'M' }, 'pw', MATCH);
        await flushMicrotasks();
        const stale = stored();
        global.fetch = vi.fn((url, opts) => {
            if (String(url).includes('/api/time')) return Promise.reject(new TypeError('clock'));
            const body = JSON.parse(opts.body);
            return answer(200, { outcomes: body.events.map((e) => ({ id: e.id, outcome: 'applied' })) });
        });
        await reconnect();
        expect(stored(), 'settled').toEqual([]);
        global.fetch = vi.fn(() => Promise.reject(new TypeError('offline')));
        await tick(2 * 60 * 60 * 1000);
        const newRaw = JSON.stringify(stale);
        _lsStore.bc_write_queue_v2 = newRaw;
        storageEvent(JSON.stringify([]), newRaw);
        await flushMicrotasks();
        expect(API.unsentWrites().total, 'the record of the settle is gone').toBe(1);
    });

    it('an entry dropped with no stamp does not block a stamped copy stored later under its key', async () => {
        API.recordTap('c1', 'm1', { kind: 'ippon.add', pos: 0, side: 'a', letter: 'M' }, 'pw', MATCH);
        await flushMicrotasks();
        const [[key, d]] = stored();
        API.clearQueue();
        const { enqueuedAt: _at, ...unstamped } = d;
        _lsStore.bc_write_queue_v2 = JSON.stringify([[key, unstamped]]);
        await load();
        await flushMicrotasks();
        expect(stored(), 'the unstamped entry is dropped as unreadable').toEqual([]);
        const newRaw = JSON.stringify([[key, d]]);
        _lsStore.bc_write_queue_v2 = newRaw;
        storageEvent(JSON.stringify([]), newRaw);
        await flushMicrotasks();
        expect(API.unsentWrites().total, 'the stamped copy is taken up').toBe(1);
    });
});
