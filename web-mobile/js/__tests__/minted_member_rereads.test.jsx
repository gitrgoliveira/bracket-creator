// bc-evts stage 3 review 10, N5: a member whose add left unanswered is read
// for again when the read failed, with the password the operator signed in
// with since, backed off and bounded; the writes go as they were made
// meanwhile (nothing stalls).
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import path from 'node:path';
import { FETCH_TIMEOUT_MS } from '../write_result.jsx';
import { createMintedMembers, MINTED_REREAD_TRIES } from '../minted_members.jsx';

const CLIENT = path.resolve(__dirname, '../api_client.jsx');
const flushMicro = async () => { for (let i = 0; i < 40; i++) await Promise.resolve(); };
const tick = async (ms) => { await vi.advanceTimersByTimeAsync(ms); await flushMicro(); };
const resp = (status, body) => Promise.resolve({
    ok: status >= 200 && status < 300, status, headers: { get: () => null },
    json: async () => body, text: async () => JSON.stringify(body),
});
const quiet = { onRefusal: () => true };

describe('the owner asks again for a member whose read failed', () => {
    const add = { kind: 'member', method: 'POST', compID: 'c1', url: '/api/competitions/c1/teams/t1/members', payload: { id: 'm-1', name: 'Ren Abe' } };
    function unknownWithFailedReads() {
        const store = {};
        const clock = { t: 1000 };
        const m = createMintedMembers({
            storage: { get: (k) => (k in store ? store[k] : null), set: (k, v) => { store[k] = String(v); }, remove: (k) => { delete store[k]; } },
            now: () => clock.t, timeoutMs: FETCH_TIMEOUT_MS, tab: 'a',
        });
        m.minted(add);
        m.sent(add);
        m.answered(add, 'retry', true);
        m.discarded(add);
        return { m, clock };
    }
    const ctx = { addQueued: () => false, addOutHere: () => false, teamOf: () => 't1' };

    it('with no write waiting, a failed read is asked again a deadline later, then at growing gaps, then no more', () => {
        const { m, clock } = unknownWithFailedReads();
        const asked = [];
        let guard = 0;
        // Each time the sweep may ask, it reads and fails.
        while (guard++ < 100) {
            const due = m.due(['m-1'], ctx);
            if (due.reads.length > 0) {
                asked.push(clock.t);
                m.readEnded('m-1', null, ctx);
                continue;
            }
            if (!due.until) break;
            clock.t = due.until;
        }
        expect(asked.length, 'asked again, but a bounded number of times').toBeGreaterThan(1);
        expect(asked.length).toBeLessThanOrEqual(MINTED_REREAD_TRIES + 1);
        const gaps = asked.slice(1).map((t, i) => t - asked[i]);
        expect(gaps[0], 'the first gap is a deadline').toBeGreaterThanOrEqual(FETCH_TIMEOUT_MS);
        for (let i = 1; i < gaps.length; i++) expect(gaps[i], 'backing off').toBeGreaterThan(gaps[i - 1]);
        // And the writes still go as they were made.
        expect(m.gate({ kind: 'lineup', compID: 'c1', url: '/api/competitions/c1/teams/t1/match-lineups/m1', payload: { teamId: 't1', memberIds: { 1: 'm-1' } } }, ctx).wait).toBe(false);
    });
});

describe('a member add discarded after it went out, its read refused by a stale password', () => {
    let saved;
    beforeAll(() => {
        vi.useFakeTimers();
        saved = { fetch: global.fetch, ES: global.EventSource, ls: global.localStorage };
        global.EventSource = class { constructor() { this.close = () => {}; } };
        global.EventSource.OPEN = 1;
    });
    let quietened = [];
    beforeEach(() => {
        quietened = [vi.spyOn(console, 'warn').mockImplementation(() => {}), vi.spyOn(console, 'error').mockImplementation(() => {})];
    });
    afterEach(() => { for (const s of quietened) s.mockRestore(); });
    afterAll(() => {
        vi.useRealTimers();
        global.fetch = saved.fetch;
        global.EventSource = saved.ES;
        global.localStorage = saved.ls;
    });

    it('reads again with the password the operator signed in with, and learns the member never was', async () => {
        let ls = {};
        global.localStorage = {
            getItem: (k) => (k in ls ? ls[k] : null),
            setItem: (k, v) => { ls[k] = String(v); },
            removeItem: (k) => { delete ls[k]; },
            clear: () => { ls = {}; },
        };
        const reads = [];
        let serverPassword = 'old';
        global.fetch = vi.fn((url, opts = {}) => {
            url = String(url);
            if (url.includes('/api/time')) return Promise.reject(new TypeError('clock'));
            const method = opts.method || 'GET';
            const hdr = opts.headers || {};
            if (method === 'GET' && url.endsWith('/team-members')) {
                reads.push({ at: Date.now(), pw: hdr['X-Tournament-Password'] });
                if (hdr['X-Tournament-Password'] !== serverPassword) return resp(401, { error: 'unauthorized' });
                return resp(200, { teamMembers: { t1: [] } });
            }
            if (method === 'POST' && url.endsWith('/teams/t1/members')) return resp(500, { error: 'internal error' });
            return resp(200, {});
        });
        vi.resetModules();
        const API = (await import(/* @vite-ignore */ `${CLIENT}?tab=n5`)).API;
        const dropped = [];
        const onDrop = (e) => dropped.push(e.detail.memberId);
        window.addEventListener('team-member-dropped', onDrop);
        try {
            const member = await API.addTeamMember('c1', 't1', 'Ren Abe', 'old', quiet);
            if (member.settled) member.settled.catch(() => {});
            for (let i = 0; i < 40 && !API.heldWrites().some((h) => h.kind === 'member' && h.keepsFailing); i++) await tick(2000);
            const h = API.heldWrites().find((x) => x.kind === 'member' && x.keepsFailing);
            expect(h, 'the add keeps failing').toBeTruthy();
            // The organiser signs in again: the server now wants the new password.
            serverPassword = 'new';
            expect(API.discardHeldWrite(h.key)).toBeTruthy();
            await tick(10);
            expect(reads.length, 'a read was made with the add\'s password, and refused').toBeGreaterThanOrEqual(1);
            expect(reads.every((r) => r.pw === 'old')).toBe(true);
            expect(dropped, 'nothing proved yet').toEqual([]);
            API.resumeAfterAuth('new');
            for (let i = 0; i < 30; i++) await tick(FETCH_TIMEOUT_MS / 2);
            expect(reads.some((r) => r.pw === 'new'), 'asked again with the new password').toBe(true);
            expect(dropped, 'the list lacks the member: it never was').toEqual([member.id]);
            const before = reads.length;
            for (let i = 0; i < 30; i++) await tick(FETCH_TIMEOUT_MS);
            expect(reads.length, 'a settled member is not asked about again').toBe(before);
        } finally {
            window.removeEventListener('team-member-dropped', onDrop);
            try { API.clearQueue(); } catch (_e) { /* ignore */ }
            vi.clearAllTimers();
        }
    }, 60000);
});
