// bc-evts stage 3, review round 2, R1 (the operator's rule: "ALL goes to the
// outbox ... but never blocks the UI"): a team member's add, rename and
// cleared name, a match's lineup removal and a competitor's reinstatement are
// outbox entries, answered at once and never awaited. The add mints the
// member's id, so a match lineup save that names the member is queued after
// the add (`after`) and reaches the server only once the add has.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

let mod;
let API;
let _lsStore;
let _orig;

async function load() {
    vi.resetModules();
    mod = await import('../api_client.jsx');
    API = mod.API;
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
const UUID_V4 = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const PATH = '/api/competitions/c1/teams/t1/members';

// A server recording every request; `reply(method, url)` answers it, null to hang.
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

describe('a team member write is the outbox\'s (R1)', () => {
    it('the add answers at once, offline, with the member under an id this device minted', async () => {
        const member = await API.addTeamMember('c1', 't1', '  Ren Abe ', 'pw', { index: 6 });
        expect(member.id).toMatch(UUID_V4);
        expect({ ...member }).toEqual({ id: member.id, index: 6, name: 'Ren Abe' });
        expect(member.pending).toBe(true);
        expect(typeof member.settled.then).toBe('function');
        const [[, d]] = stored();
        expect(d).toMatchObject({ kind: 'member', method: 'POST', url: PATH });
        expect(d.payload).toMatchObject({ id: member.id, name: 'Ren Abe' });
    });

    it('the add is sent when the connection comes, and settles with the member as stored', async () => {
        const member = await API.addTeamMember('c1', 't1', 'Ren Abe', 'pw');
        const sent = server((req) => answer(201, { id: req.body.id, index: 6, name: req.body.name, modifiedAt: 9 }));
        await reconnect();
        expect(sent.map((r) => `${r.method} ${r.url}`)).toEqual([`POST ${PATH}`]);
        await expect(member.settled).resolves.toEqual({ id: member.id, index: 6, name: 'Ren Abe', modifiedAt: 9 });
        expect(stored()).toEqual([]);
    });

    it('a match lineup save naming the new member waits for the add, and goes once it lands', async () => {
        let releaseAdd;
        const sent = server((req) => {
            if (req.method === 'POST') return new Promise((r) => { releaseAdd = () => r(answer(201, { id: req.body.id, index: 6, name: req.body.name, modifiedAt: 9 })); });
            return answer(200, { positions: req.body.positions, memberIds: req.body.memberIds });
        });
        const member = await API.addTeamMember('c1', 't1', 'Ren Abe', 'pw');
        const save = await API.putMatchLineup('c1', 't1', 'm1', { senpo: 'Ren Abe' }, 'pw', { senpo: member.id }, ['senpo']);
        expect(save.pending).toBe(true);
        await tick(10);
        expect(sent.map((r) => r.method)).toEqual(['POST']);
        releaseAdd();
        await tick(10);
        expect(sent.map((r) => r.method)).toEqual(['POST', 'PUT']);
        expect(sent[1].body.memberIds).toEqual({ senpo: member.id });
    });

    it('a lineup save waiting on a member write the server keeps failing is held, so its match shows it (F6)', async () => {
        // Stage 3 review 3, F6: the add is stuck on a 5xx, and the match's lineup
        // save waits for it (`after`). Waiting on a write that keeps failing is
        // no longer only waiting: the save is held, so the match editor's
        // pending banner (hasHeldWrite) shows the blocker.
        const sent = server((req) => (req.method === 'POST' ? answer(503, { error: 'unavailable' })
            : answer(200, { positions: req.body.positions, memberIds: req.body.memberIds })));
        const member = await API.addTeamMember('c1', 't1', 'Ren Abe', 'pw');
        await API.putMatchLineup('c1', 't1', 'm1', { senpo: 'Ren Abe' }, 'pw', { senpo: member.id }, ['senpo']);
        await reconnect();
        await reconnect();
        expect(sent.length).toBeGreaterThan(0);
        expect(sent.every((r) => r.method === 'POST')).toBe(true);
        expect(API.hasHeldWrite('c1', 'm1')).toBe(true);
    });

    it('a rename answers at once with the new name stamped over the copy shown, and a refusal brings the old one back', async () => {
        // The spy is in place before the refusal can fire, so the count is the
        // outbox's own warning and not an earlier test's.
        const warned = vi.spyOn(console, 'warn').mockImplementation(() => {});
        const shown = { id: 'mem-2', index: 2, name: 'Sato', modifiedAt: 5 };
        server(() => answer(409, { error: 'member_already_named', message: 'This team member already has a name. Ask the tournament organizer to change it.' }));
        let claimed;
        const renamed = await API.renameTeamMember('c1', 't1', 'mem-2', 'Kato', '', {
            member: shown, onRefusal: (e) => { claimed = e; return true; },
        });
        expect(renamed).toMatchObject({ id: 'mem-2', index: 2, name: 'Kato' });
        expect(renamed.modifiedAt).toBeGreaterThan(shown.modifiedAt);
        await tick(10);
        await renamed.settled.catch(() => {});
        expect(claimed.message).toBe('This team member already has a name. Ask the tournament organizer to change it.');
        expect(claimed.revertMember).toMatchObject({ id: 'mem-2', index: 2, name: 'Sato' });
        expect(claimed.revertMember.modifiedAt).toBeGreaterThanOrEqual(renamed.modifiedAt);
        // The outbox logs the refusal it hands back.
        expect(warned).toHaveBeenCalledTimes(1);
        warned.mockRestore();
    });

    it('a cleared name is a DELETE the outbox still sends after a reload', async () => {
        const cleared = await API.clearTeamMember('c1', 't1', 'mem-2', 'pw', { member: { id: 'mem-2', index: 2, name: 'Sato' } });
        expect(cleared).toMatchObject({ id: 'mem-2', index: 2, name: '' });
        await load();
        const sent = server(() => answer(200, { id: 'mem-2', index: 2, name: '', modifiedAt: 9 }));
        await reconnect();
        expect(sent.map((r) => `${r.method} ${r.url}`)).toEqual([`DELETE ${PATH}/mem-2`]);
    });
});

describe('a lineup removal and a reinstatement are the outbox\'s (R1)', () => {
    it('removing a match\'s own lineup follows a save of it queued before, and a 404 is the outcome asked for', async () => {
        await API.putMatchLineup('c1', 't1', 'm1', { senpo: 'Sato' }, 'pw', undefined, ['senpo']);
        const removed = await API.deleteMatchLineup('c1', 't1', 'm1', 'pw');
        expect(removed.pending).toBe(true);
        const sent = server((req) => (req.method === 'DELETE' ? answer(404, { error: 'no lineup' }) : answer(200, { positions: {} })));
        await reconnect();
        expect(sent.map((r) => r.method)).toEqual(['PUT', 'DELETE']);
        await expect(removed.settled).resolves.toBe(true);
        expect(stored()).toEqual([]);
    });

    it('a removal the server refuses rejects with the server\'s own sentence, and leaves the queue', async () => {
        const warned = vi.spyOn(console, 'warn').mockImplementation(() => {});
        const removed = await API.deleteMatchLineup('c1', 't1', 'm1', 'pw', { onRefusal: () => true });
        server(() => answer(409, { error: 'result_finalized', message: 'The competition has finished.' }));
        await reconnect();
        await expect(removed.settled).rejects.toThrow('The competition has finished.');
        expect(stored()).toEqual([]);
        // The outbox logs the refusal.
        expect(warned).toHaveBeenCalledTimes(1);
        warned.mockRestore();
    });

    it('a reinstatement answers at once and settles with the status the server stored', async () => {
        const res = await API.reinstateCompetitor('c1', 'p1', 'pw');
        expect(res.pending).toBe(true);
        const sent = server(() => answer(200, { playerId: 'p1', eligible: true }));
        await reconnect();
        expect(sent.map((r) => `${r.method} ${r.url}`)).toEqual(['POST /api/competitions/c1/competitors/p1/reinstate']);
        await expect(res.settled).resolves.toEqual({ playerId: 'p1', eligible: true });
    });
});

// Stage 3 review 3, F8: the refusal sentences the direct member writes threw
// (api_client_squad.test.jsx before R1), in their outbox form: `settled`
// rejects with the server's sentence, its code beside it.
describe('a member write the server refuses (F8)', () => {
    const SENDS = [
        ['add', () => API.addTeamMember('c1', 't1', 'Ito', '', { onRefusal: () => true })],
        ['rename', () => API.renameTeamMember('c1', 't1', 'mem-1', 'Ito', '', { member: { id: 'mem-1', index: 1, name: 'Sato' }, onRefusal: () => true })],
        ['clear', () => API.clearTeamMember('c1', 't1', 'mem-1', '', { member: { id: 'mem-1', index: 1, name: 'Sato' }, onRefusal: () => true })],
    ];
    let warned;
    beforeEach(() => { warned = vi.spyOn(console, 'warn').mockImplementation(() => {}); });
    afterEach(() => { warned.mockRestore(); });

    async function refused(send, status, body) {
        server(() => answer(status, body));
        const member = await send();
        await reconnect();
        const err = await member.settled.then(() => null, (e) => e);
        expect(err, 'the write was refused').toBeInstanceOf(Error);
        expect(stored(), 'and left the queue').toEqual([]);
        return err;
    }

    it.each(SENDS)('the %s rejects with a refusal\'s sentence, not its code, and keeps the code beside it', async (_name, send) => {
        const err = await refused(send, 409, { error: 'member_already_named', message: 'This team member already has a name. Ask the tournament organizer to change it.' });
        expect(err.message).toBe('This team member already has a name. Ask the tournament organizer to change it.');
        expect(err.code).toBe('member_already_named');
    });

    it.each(SENDS)('the %s leaves no code on a refusal that is only a message', async (_name, send) => {
        const err = await refused(send, 404, { error: 'team member not found' });
        expect(err.message).toBe('team member not found');
        expect(err.code).toBeUndefined();
    });

    it('an add refused for a name the team already has says the server\'s message', async () => {
        const [, add] = SENDS[0];
        const err = await refused(add, 409, { error: 'team "t1" already has a member named "Ito"' });
        expect(err.message).toMatch(/already has a member named "Ito"/);
    });

    it('an add refused member_id_conflict says the server\'s sentence, with the code', async () => {
        const [, add] = SENDS[0];
        const err = await refused(add, 409, { error: 'member_id_conflict', message: 'Another team member already has this id. Reload the page and add the member again.' });
        expect(err.message).toBe('Another team member already has this id. Reload the page and add the member again.');
        expect(err.code).toBe('member_id_conflict');
    });
});

// Stage 3 review 3, F2: an add the server refuses, or an operator discards,
// leaves the id this device minted for it naming nobody. A lineup save queued
// after it would carry that id to the server; the outbox takes it off every
// queued lineup save (the position keeps its name), says the member is gone
// (the `team-member-dropped` event, which the editors take the member off their
// lists by), and the refusal carries the id (`droppedMemberId`).
describe('a member add that never lands (F2)', () => {
    let warned;
    let dropped;
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

    async function queueAddAndSave(addAnswer) {
        const sent = server((req) => (req.method === 'POST' ? addAnswer()
            : answer(200, { positions: req.body.positions, memberIds: req.body.memberIds || {} })));
        const member = await API.addTeamMember('c1', 't1', 'Ren Abe', 'pw', { onRefusal: () => true });
        await API.putMatchLineup('c1', 't1', 'm1', { senpo: 'Ren Abe', jiho: 'Sato' }, 'pw', { senpo: member.id, jiho: 'mem-2' }, ['senpo', 'jiho']);
        return { sent, member };
    }

    it('a refused add takes its id off the queued lineup save, which keeps the name, and says the member is gone', async () => {
        const { sent, member } = await queueAddAndSave(() => answer(409, { error: 'team "t1" already has a member named "Ren Abe"' }));
        await reconnect();
        const err = await member.settled.then(() => null, (e) => e);
        expect(err.droppedMemberId).toBe(member.id);
        const put = sent.find((r) => r.method === 'PUT');
        expect(put, 'the lineup save still goes').toBeDefined();
        expect(put.body.positions).toEqual({ senpo: 'Ren Abe', jiho: 'Sato' });
        expect(put.body.memberIds, 'with no id for the member that never was').toEqual({ jiho: 'mem-2' });
        expect(dropped).toEqual([{ competitionId: 'c1', teamId: 't1', memberId: member.id }]);
    });

    it('an add the operator discards does the same', async () => {
        const { sent, member } = await queueAddAndSave(() => answer(503, { error: 'unavailable' }));
        for (let i = 0; i < 12 && !API.heldWrites().some((h) => h.kind === 'member' && h.keepsFailing); i++) {
            await reconnect();
            await tick(60000);
        }
        const add = API.heldWrites().find((h) => h.kind === 'member');
        expect(add && add.keepsFailing, 'the add keeps failing').toBe(true);
        expect(API.discardHeldWrite(add.key)).toBe(true);
        await tick(10);
        const put = sent.find((r) => r.method === 'PUT');
        expect(put, 'the lineup save goes once the add is discarded').toBeDefined();
        expect(put.body.memberIds).toEqual({ jiho: 'mem-2' });
        expect(dropped).toEqual([{ competitionId: 'c1', teamId: 't1', memberId: member.id }]);
    });

    it('an add that lands takes nothing off', async () => {
        const { sent, member } = await queueAddAndSave(() => Promise.resolve(null).then(() => answer(201, { id: 'x', index: 6, name: 'Ren Abe', modifiedAt: 9 })));
        await reconnect();
        const put = sent.find((r) => r.method === 'PUT');
        expect(put.body.memberIds).toEqual({ senpo: member.id, jiho: 'mem-2' });
        expect(dropped).toEqual([]);
    });
});
