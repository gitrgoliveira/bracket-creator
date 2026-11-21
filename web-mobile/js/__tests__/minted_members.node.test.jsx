// minted_members.jsx, the one owner of what is known about a member id this
// device minted (bc-evts stage 3, M1 a): its states, its reading of the ids a
// write names, and its storage, refused or shared.
import { describe, it, expect } from 'vitest';
import {
    createMintedMembers, mintedIdsNamed, mintedStrip, MINTED_MEMBERS_KEY, LEGACY_DROPPED_MEMBERS_KEY, MINTED_KEEP_MS,
} from '../minted_members.jsx';

const T = 12000;
const add = (id, team = 't1') => ({ kind: 'member', method: 'POST', compID: 'c1', url: `/api/competitions/c1/teams/${team}/members`, payload: { id, name: 'Ren Abe' } });
const pick = (id) => ({ kind: 'evt', compID: 'c1', payload: { kind: 'rep.pick', memberId: id } });
const lineup = (id, team = 't1') => ({ kind: 'lineup', compID: 'c1', url: `/api/competitions/c1/teams/${team}/match-lineups/m1`, payload: { teamId: team, memberIds: { 1: id }, positions: { 1: 'Ren Abe' } } });
const rename = (id) => ({ kind: 'member', method: 'PUT', compID: 'c1', url: `/api/competitions/c1/teams/t1/members/${id}`, payload: { name: 'X' } });

function memoryStorage() {
    const s = {};
    return { s, get: (k) => (k in s ? s[k] : null), set: (k, v) => { s[k] = String(v); }, remove: (k) => { delete s[k]; } };
}
const refusing = { get: () => { throw new Error('denied'); }, set: () => { throw new Error('denied'); }, remove: () => { throw new Error('denied'); } };

function owner(storage, tab = 'a', clock = { t: 1000 }) {
    return { clock, m: createMintedMembers({ storage, now: () => clock.t, timeoutMs: T, tab }) };
}
const ctx = (over = {}) => ({ addQueued: () => false, addOutHere: () => false, teamOf: (d) => (d.payload && d.payload.teamId) || '', ...over });

describe('the ids a write names, and taking one off', () => {
    it('reads a lineup, a tap, a score write\'s rows and the member a rename is addressed to', () => {
        expect([...mintedIdsNamed(lineup('m-1'))]).toEqual(['m-1']);
        expect([...mintedIdsNamed(pick('m-2'))]).toEqual(['m-2']);
        expect([...mintedIdsNamed({ payload: { subResults: [{ sideAMemberId: 'm-3', sideBMemberId: 'm-4', winnerMemberId: 'm-3' }] } })]).toEqual(['m-3', 'm-4']);
        expect([...mintedIdsNamed(rename('m-5'))]).toEqual(['m-5']);
        expect([...mintedIdsNamed(add('m-6'))], 'the add names no member: it mints one').toEqual([]);
    });

    it('keeps a position\'s name, clears a pick, keeps a row\'s names', () => {
        const l = lineup('m-1');
        expect(mintedStrip(l, 'm-1')).toBe(true);
        expect(l.payload).toMatchObject({ memberIds: {}, positions: { 1: 'Ren Abe' } });
        const p = pick('m-1');
        mintedStrip(p, 'm-1');
        expect(p.payload.memberId).toBeUndefined();
        const s = { payload: { subResults: [{ sideA: 'Ren Abe', sideAMemberId: 'm-1', winnerMemberId: 'm-1' }] } };
        mintedStrip(s, 'm-1');
        expect(s.payload.subResults[0]).toEqual({ sideA: 'Ren Abe', sideAMemberId: '', winnerMemberId: '' });
    });
});

describe('the states of a minted id', () => {
    it('queued and out hold a write back; a 2xx forgets the record and the write goes as made', () => {
        const { m } = owner(memoryStorage());
        m.minted(add('m-1'));
        expect(m.gate(pick('m-1'), ctx({ addQueued: () => true })).wait).toBe(true);
        m.sent(add('m-1'));
        expect(m.gate(pick('m-1'), ctx({ addOutHere: () => true })).wait).toBe(true);
        m.answered(add('m-1'), 'kept', false);
        expect(m.recordOf('m-1')).toBeNull();
        expect(m.gate(pick('m-1'), ctx())).toMatchObject({ wait: false, strip: [], leave: false });
    });

    it('a refusal on its merits drops the id: it comes off a write, and a rename of it leaves', () => {
        const { m } = owner(memoryStorage());
        m.minted(add('m-1'));
        m.sent(add('m-1'));
        expect(m.answered(add('m-1'), 'refused', true)).toBe('dropped');
        expect(m.gate(pick('m-1'), ctx())).toMatchObject({ wait: false, strip: ['m-1'] });
        expect(m.gate(rename('m-1'), ctx())).toMatchObject({ leave: true });
        expect(m.gate(lineup('m-1', 't2'), ctx()).strip, 'another team\'s write is not covered').toEqual([]);
    });

    it('an add discarded before it went out is dropped; one that went out is read first', () => {
        const { m } = owner(memoryStorage());
        m.minted(add('m-1'));
        expect(m.discarded(add('m-1'))).toBe('dropped');
        m.minted(add('m-2'));
        m.sent(add('m-2'));
        m.answered(add('m-2'), 'retry', true);
        expect(m.discarded(add('m-2'))).toBe('');
        expect(m.gate(pick('m-2'), ctx())).toMatchObject({ wait: true, reads: [{ id: 'm-2', c: 'c1', t: 't1' }] });
        expect(m.readEnded('m-2', [{ id: 'other' }], ctx())).toBe('dropped');
    });

    it('a request given up on at its deadline is read only a deadline later', () => {
        const { m, clock } = owner(memoryStorage());
        m.minted(add('m-1'));
        m.sent(add('m-1'));
        m.answered(add('m-1'), 'timeout', true);
        m.discarded(add('m-1'));
        expect(m.gate(pick('m-1'), ctx())).toMatchObject({ wait: true, reads: [], until: clock.t + T });
        clock.t += T;
        expect(m.gate(pick('m-1'), ctx()).reads).toHaveLength(1);
    });

    it('a read that fails lets the writes go as made, and the members are asked again later', () => {
        const { m, clock } = owner(memoryStorage());
        m.minted(add('m-1'));
        m.sent(add('m-1'));
        m.answered(add('m-1'), 'held', false);
        expect(m.readEnded('m-1', null, ctx())).toBe('');
        expect(m.gate(pick('m-1'), ctx())).toMatchObject({ wait: false, reads: [], strip: [] });
        clock.t += T;
        expect(m.gate(pick('m-1'), ctx())).toMatchObject({ wait: false, reads: [{ id: 'm-1', c: 'c1', t: 't1' }] });
    });

    it('a read made while the add is queued again proves nothing', () => {
        const { m } = owner(memoryStorage());
        m.minted(add('m-1'));
        m.sent(add('m-1'));
        m.answered(add('m-1'), 'held', false);
        expect(m.readEnded('m-1', [], ctx({ addQueued: () => true }))).toBe('');
        expect(m.recordOf('m-1')).toMatchObject({ s: 'unknown' });
    });

    it('a members list holding the id forgets what was known, for its own competition and team only', () => {
        const { m } = owner(memoryStorage());
        m.minted(add('m-1'));
        m.answered(add('m-1'), 'refused', false);
        m.listed({ t2: [{ id: 'm-1' }] }, 'c1');
        m.listed({ t1: [{ id: 'm-1' }] }, 'c2');
        expect(m.recordOf('m-1')).toMatchObject({ s: 'dropped' });
        m.listed({ t1: [{ id: 'm-1' }] }, 'c1');
        // Dropped before: kept now, and told (N4 below), not forgotten.
        expect(m.recordOf('m-1')).toMatchObject({ s: 'kept' });
    });
});

describe('a member judged never to have been that the server holds after all (N4)', () => {
    it('a members list holding a dropped id is a contradiction, answered once; the record is kept for the other tabs', () => {
        const st = memoryStorage();
        const { m } = owner(st);
        m.minted(add('m-1'));
        m.sent(add('m-1'));
        m.answered(add('m-1'), 'refused', true);
        expect(m.listed({ t1: [{ id: 'other' }] }, 'c1'), 'a list without it contradicts nothing').toEqual([]);
        expect(m.listed({ t1: [{ id: 'm-1' }] }, 'c2'), 'another competition\'s list says nothing').toEqual([]);
        expect(m.listed({ t1: [{ id: 'm-1' }] }, 'c1')).toEqual([{ id: 'm-1', c: 'c1', t: 't1' }]);
        expect(m.listed({ t1: [{ id: 'm-1' }] }, 'c1'), 'told once').toEqual([]);
        expect(m.recordOf('m-1')).toMatchObject({ s: 'kept' });
        expect(m.gate(pick('m-1'), ctx()), 'a kept id goes as made').toMatchObject({ wait: false, strip: [], leave: false });
    });

    it('a late 2xx of an add judged never to have been is a contradiction; an ordinary 2xx is not', () => {
        const { m } = owner(memoryStorage());
        m.minted(add('m-1'));
        m.sent(add('m-1'));
        expect(m.answered(add('m-1'), 'kept', false), 'an ordinary 2xx').toBe('');
        m.minted(add('m-2'));
        m.sent(add('m-2'));
        m.answered(add('m-2'), 'refused', true);
        expect(m.answered(add('m-2'), 'kept', false)).toBe('contradicted');
        expect(m.recordOf('m-2')).toMatchObject({ s: 'kept' });
    });

    it('another tab hears of it through the records, once', () => {
        const st = memoryStorage();
        const { m: a } = owner(st, 'a');
        const { m: b } = owner(st, 'b');
        a.minted(add('m-1'));
        a.sent(add('m-1'));
        a.answered(add('m-1'), 'refused', true);
        const dropped = st.get(MINTED_MEMBERS_KEY);
        a.listed({ t1: [{ id: 'm-1' }] }, 'c1');
        const kept = st.get(MINTED_MEMBERS_KEY);
        expect(b.onStored(dropped, kept)).toMatchObject({ kept: [{ id: 'm-1', c: 'c1', t: 't1' }], dropped: [] });
        expect(b.onStored(kept, kept).kept).toEqual([]);
    });
});

describe('the records, shared and refused', () => {
    it('another tab\'s request still out holds a write back until its deadline, then the members are read after a grace', () => {
        const storage = memoryStorage();
        const { m: a, clock } = owner(storage, 'a');
        const { m: b } = owner(storage, 'b', clock);
        a.minted(add('m-1'));
        a.sent(add('m-1'));
        expect(b.gate(pick('m-1'), ctx()), 'out in tab a').toMatchObject({ wait: true, until: 1000 + T + 1000 });
        expect(b.discarded(add('m-1')), 'b\'s discard does not judge a\'s request').toBe('');
        clock.t += T + 1000;
        // Tab a went away with its request out: the request is over, and
        // the add may commit up to a deadline after it.
        expect(b.gate(pick('m-1'), ctx())).toMatchObject({ wait: true, reads: [], until: 1000 + 2 * T + 1000 });
        clock.t += T;
        expect(b.gate(pick('m-1'), ctx()).reads).toHaveLength(1);
    });

    it('says when another tab dropped an id, and when a record it held back a write on is gone', () => {
        const storage = memoryStorage();
        const { m: a } = owner(storage, 'a');
        const { m: b } = owner(storage, 'b');
        a.minted(add('m-1'));
        const before = storage.s[MINTED_MEMBERS_KEY];
        a.answered(add('m-1'), 'refused', true);
        expect(b.onStored(before, storage.s[MINTED_MEMBERS_KEY])).toEqual({ dropped: [{ id: 'm-1', c: 'c1', t: 't1' }], kept: [], unblocks: true });
        a.minted(add('m-2'));
        const queued = storage.s[MINTED_MEMBERS_KEY];
        a.sent(add('m-2'));
        expect(b.onStored(queued, storage.s[MINTED_MEMBERS_KEY]).unblocks, 'an add sent frees nothing').toBe(false);
        const out = storage.s[MINTED_MEMBERS_KEY];
        a.answered(add('m-2'), 'kept', false);
        expect(b.onStored(out, storage.s[MINTED_MEMBERS_KEY]).unblocks, 'an add kept frees its writes').toBe(true);
    });

    it('storage that refuses keeps what this tab learned in memory, and never holds a write for good', () => {
        const { m } = owner(refusing);
        m.minted(add('m-1'));
        m.sent(add('m-1'));
        expect(m.answered(add('m-1'), 'refused', true)).toBe('dropped');
        expect(m.gate(pick('m-1'), ctx()).strip).toEqual(['m-1']);
        expect(m.gate(pick('never-minted'), ctx()).wait, 'an id nothing is known of goes as made').toBe(false);
    });

    it('a change storage refused is laid over what storage holds once it reads again', () => {
        const storage = memoryStorage();
        let refuse = false;
        const flaky = { get: storage.get, set: (k, v) => { if (refuse) throw new Error('full'); storage.set(k, v); }, remove: storage.remove };
        const { m } = owner(flaky);
        m.minted(add('m-1'));
        refuse = true;
        m.answered(add('m-1'), 'refused', true);
        expect(m.recordOf('m-1'), 'the stored copy still says queued; this tab knows better').toMatchObject({ s: 'dropped' });
    });

    it('takes an older page\'s dropped-member notes as dropped records, once', () => {
        const storage = memoryStorage();
        storage.s[LEGACY_DROPPED_MEMBERS_KEY] = JSON.stringify([{ id: 'm-9', at: 1, competitionId: 'c1', teamId: 't1' }]);
        const { m } = owner(storage);
        expect(m.recordOf('m-9')).toMatchObject({ s: 'dropped', c: 'c1', t: 't1' });
        expect(storage.s[LEGACY_DROPPED_MEMBERS_KEY]).toBeUndefined();
    });

    it('forgets a settled record nothing names once it is old, never one an add still queued holds', () => {
        const storage = memoryStorage();
        const { m, clock } = owner(storage);
        m.minted(add('m-1'));
        m.answered(add('m-1'), 'refused', false);
        m.minted(add('m-2'));
        clock.t += MINTED_KEEP_MS + 1;
        m.prune(new Set(), new Set(['m-2']));
        expect(m.recordOf('m-1')).toBeNull();
        expect(m.recordOf('m-2')).toMatchObject({ s: 'queued' });
        m.clear();
        expect(storage.s[MINTED_MEMBERS_KEY]).toBeUndefined();
    });
});
