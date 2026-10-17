// bc-evts stage 3, operator decision M1 (a), 2026-10-17: what the outbox knows
// of a member id this device minted has one owner (minted_members.jsx). This
// is its gate: every state a minted member's add can reach, crossed with every
// write that names the id, in one tab and in two tabs that share one browser
// storage (the add made in tab A, sent or discarded in tab B, the storage
// events delivered in order to the other tab only). For each it asserts:
//   - no id is taken off a write naming a member the server holds;
//   - no id the server does not hold reaches it (a rename or a cleared name of
//     such a member included), except where every read of the team's members
//     failed, which the rule accepts (the writes then go as they were made and
//     the server judges them), asserted explicitly;
//   - no write naming the id reaches the server before the add that mints it;
//   - nothing stalls (no tab holds a write at the end) and no write is lost.
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import path from 'node:path';
import { FETCH_TIMEOUT_MS } from '../write_result.jsx';

// Loaded by its file path under a query per tab, so each tab is its own
// instance of the module.
const CLIENT = path.resolve(__dirname, '../api_client.jsx');

const MATCH = {
    id: 'm1', compId: 'c1', status: 'running', court: '', modifiedAt: 1,
    sideA: { id: 't1', name: 'Team One' }, sideB: { id: 't2', name: 'Team Two' },
};

const flushMicro = async () => { for (let i = 0; i < 40; i++) await Promise.resolve(); };
const tick = async (ms) => { await vi.advanceTimersByTimeAsync(ms); await flushMicro(); };
const resp = (status, body) => Promise.resolve({
    ok: status >= 200 && status < 300, status, headers: { get: () => null },
    json: async () => body, text: async () => JSON.stringify(body),
});

// The tab a call comes from: each tab is its own instance of the module,
// loaded under its own query, which every frame of its code carries.
function writerTab() {
    const m = new Error().stack.match(/api_client\.jsx\?tab=(\w+)/);
    return m ? m[1] : '?';
}

// Every member id a request names: in its body, or the member a rename or a
// cleared name is addressed to.
function idsIn(url, body) {
    const ids = new Set();
    const m = String(url).match(/\/members\/([^/?]+)$/);
    if (m) ids.add(decodeURIComponent(m[1]));
    if (!body || typeof body !== 'object') return ids;
    if (body.memberIds) for (const v of Object.values(body.memberIds)) if (v) ids.add(v);
    for (const e of body.events || []) if (e.memberId) ids.add(e.memberId);
    for (const r of body.subResults || []) {
        for (const f of ['sideAMemberId', 'sideBMemberId', 'winnerMemberId']) if (r && r[f]) ids.add(r[f]);
    }
    return ids;
}

const quiet = { onRefusal: () => true };
const WRITES = {
    matchLineup: (T, id, pw) => T.putMatchLineup('c1', 't1', 'm1', { 1: 'Ren Abe' }, pw, { 1: id }, null, quiet),
    startingLineup: (T, id, pw) => T.putTeamLineup('c1', 't1', 1, { 1: 'Ren Abe' }, pw, { 1: id }, null, quiet),
    scoreRepPick: (T, id, pw) => T.recordScore('c1', 'm1', {
        ...MATCH,
        subResults: [{ position: -1, sideA: 'Team One', sideB: 'Team Two', ipponsA: [], ipponsB: [], sideAMemberId: id }],
    }, pw, MATCH, quiet),
    repPickTap: (T, id, pw) => T.recordTap('c1', 'm1', { kind: 'rep.pick', pos: -1, side: 'a', memberId: id }, pw, MATCH),
    fighterTap: (T, id, pw) => T.recordTap('c1', 'm1', { kind: 'bout.fighter', pos: 2, side: 'a', memberId: id, name: 'Ren Abe' }, pw, MATCH),
    // Two taps queued in one tick share one batch; the second names the member,
    // so the gate is asked per tap INSIDE the batch (`_flushBase`'s inner loop).
    batchTaps: (T, id, pw) => {
        T.recordTap('c1', 'm1', { kind: 'ippon.add', pos: 1, side: 'b', letter: 'M' }, pw, MATCH);
        return T.recordTap('c1', 'm1', { kind: 'rep.pick', pos: -1, side: 'a', memberId: id }, pw, MATCH);
    },
    rename: (T, id, pw) => T.renameTeamMember('c1', 't1', id, 'Ren Abe Jr', pw, quiet),
    clear: (T, id, pw) => T.clearTeamMember('c1', 't1', id, pw, quiet),
};
const isWrite = (w, r) => ({
    matchLineup: r.method === 'PUT' && r.url.includes('/match-lineups/'),
    startingLineup: r.method === 'PUT' && /\/lineups\/\d+$/.test(r.url),
    scoreRepPick: r.method === 'PUT' && r.url.endsWith('/score'),
    repPickTap: r.url.endsWith('/score-events') && (r.body.events || []).some((e) => e.kind === 'rep.pick'),
    fighterTap: r.url.endsWith('/score-events') && (r.body.events || []).some((e) => e.kind === 'bout.fighter'),
    batchTaps: r.url.endsWith('/score-events') && (r.body.events || []).some((e) => e.kind === 'rep.pick'),
    rename: r.method === 'PUT' && /\/members\/[^/]+$/.test(r.url),
    clear: r.method === 'DELETE' && /\/members\/[^/]+$/.test(r.url),
}[w]);
const namesByUrl = (w) => w === 'rename' || w === 'clear';

// The add's answers. `add(n)` answers its nth request: commit201 (kept,
// answered), commit500 (kept, answered 500 as state.ErrTxCommitted is),
// r500, r401, r409 with a refusal code, or hang (answered when the harness
// says; hangCommit is kept as it arrives). `discard` is when the operator
// discards it: `between` its requests, or while one is `out`.
const S = {
    ok: { add: () => 'commit201' },
    offlineThenOk: { offlineFirst: true, add: () => 'commit201' },
    duplicateName: { add: () => 'r409dup' },
    teamMemberLimit: { add: () => 'r409limit' },
    competitionFinished: { add: () => 'r409finished' },
    idConflictElsewhere: { add: () => 'r409conflict' },
    idConflictHere: { add: () => 'r409conflict', serverHasIdAlready: true },
    fiveDiscardNotKept: { add: () => 'r500', discard: 'between' },
    fiveDiscardKept: { add: (n) => (n === 1 ? 'commit500' : 'r500'), discard: 'between' },
    fiveDiscardReadFails: { add: () => 'r500', discard: 'between', readFails: true, rule: 'readFails' },
    outDiscard201: { add: (n) => (n <= 10 ? 'r500' : 'hang'), discard: 'out', release: 'commit201' },
    outDiscard409: { add: (n) => (n <= 10 ? 'r500' : 'hang'), discard: 'out', release: 'r409limit' },
    outDiscard500: { add: (n) => (n <= 10 ? 'r500' : 'hang'), discard: 'out', release: 'r500' },
    outDiscardTimeoutCommitted: { add: (n) => (n <= 10 ? 'r500' : 'hangCommit'), discard: 'out', release: 'timeout' },
    // Kept within the grace after the request was given up on: the read is
    // made after it, so the member is found.
    outDiscardTimeoutLateCommit: {
        add: (n) => (n <= 10 ? 'r500' : 'hang'), discard: 'out', release: 'timeout',
        commitAt: FETCH_TIMEOUT_MS + FETCH_TIMEOUT_MS / 2,
    },
    // Kept long after the grace, once the read has answered without it: no
    // rule on this device can know, and the id came off (accepted).
    outDiscardTimeoutCommitAfterRead: {
        add: (n) => (n <= 10 ? 'r500' : 'hang'), discard: 'out', release: 'timeout',
        commitAt: 4 * FETCH_TIMEOUT_MS, rule: 'commitAfterRead',
    },
    selfRunDiscardNotKept: { pw: '', add: () => 'r500', discard: 'between' },
    selfRunDiscardKept: { pw: '', add: (n) => (n === 1 ? 'commit500' : 'r500'), discard: 'between' },
    authThenResume: { add: (n) => (n === 1 ? 'r401' : 'commit201'), resume: true },
    // Browser storage refuses every write once the add is made: nothing is
    // shared, and nothing stalls (one tab only: two tabs share nothing then).
    storageRefusedOk: { add: () => 'commit201', refuseStorage: true, oneTab: true },
    storageRefusedLimit: { add: () => 'r409limit', refuseStorage: true, oneTab: true },
    storageRefusedDiscard: { add: () => 'r500', discard: 'between', refuseStorage: true, oneTab: true },
};
const REFUSAL = {
    r409dup: { error: 'member_name_taken', message: 'That name is taken.' },
    r409limit: { error: 'team_member_limit', message: 'No room.' },
    r409finished: { error: 'competition_finished', message: 'Finished.' },
    r409conflict: { error: 'member_id_conflict', message: 'Held.' },
};

async function runOne(scName, w, timing, twoTab) {
    const sc = S[scName];
    const pw = sc.pw === undefined ? 'pw' : sc.pw;
    let ls = {};
    let refuse = false;
    const tabs = {};
    // A storage event goes to every OTHER tab, in the order the writes were made.
    const deliver = (k, oldValue, newValue) => {
        if (oldValue === newValue) return;
        const from = writerTab();
        for (const t of Object.values(tabs)) {
            if (t.name === from) continue;
            setTimeout(() => t.onStorage({ key: k, oldValue, newValue }), 0);
        }
    };
    global.localStorage = {
        getItem: (k) => (k in ls ? ls[k] : null),
        setItem: (k, v) => {
            if (refuse) throw new DOMException('full', 'QuotaExceededError');
            const o = k in ls ? ls[k] : null; ls[k] = String(v); deliver(k, o, ls[k]);
        },
        removeItem: (k) => { const o = k in ls ? ls[k] : null; delete ls[k]; deliver(k, o, null); },
        clear: () => { ls = {}; },
    };
    const members = new Set();
    let mintedId = '';
    let addN = 0;
    let pending = null; // a hung add request
    const log = [];
    const online = { a: !twoTab && !sc.offlineFirst, b: true };
    global.fetch = vi.fn((url, opts = {}) => {
        url = String(url);
        const tab = writerTab();
        const method = opts.method || 'GET';
        if (url.includes('/api/time')) return Promise.reject(new TypeError('clock'));
        if (!online[tab]) return Promise.reject(new TypeError('offline'));
        const hdr = opts.headers || {};
        const body = opts.body ? JSON.parse(opts.body) : undefined;
        if (method === 'GET' && (url.endsWith('/team-members') || url.includes('/api/viewer/competitions/'))) {
            log.push({ tab, read: true, url, hdr: 'X-Tournament-Password' in hdr ? hdr['X-Tournament-Password'] : 'OMITTED' });
            if (sc.readFails) return resp(500, { error: 'x' });
            if (url.endsWith('/team-members') && hdr['X-Tournament-Password'] !== 'pw') return resp(401, { error: 'unauthorized' });
            return resp(200, { teamMembers: { t1: [...members].map((id) => ({ id, name: 'Ren Abe' })) } });
        }
        if (method === 'POST' && url.endsWith('/teams/t1/members')) {
            addN++;
            const act = sc.add(addN);
            log.push({ tab, add: act, n: addN });
            const id = body.id;
            if (act === 'commit201') { members.add(id); return resp(201, { id, name: body.name, modifiedAt: 5 }); }
            if (act === 'commit500') { members.add(id); return resp(500, { error: 'internal error' }); }
            if (act === 'r500') return resp(500, { error: 'internal error' });
            if (act === 'r401') return resp(401, { error: 'unauthorized' });
            if (REFUSAL[act]) return resp(409, REFUSAL[act]);
            if (act === 'hang' || act === 'hangCommit') {
                if (act === 'hangCommit') members.add(id);
                if (sc.commitAt) setTimeout(() => { members.add(id); log.push({ tab: 'server', commit: true }); }, sc.commitAt);
                return new Promise((resolve, reject) => {
                    pending = { resolve, id };
                    if (opts.signal) opts.signal.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')));
                });
            }
            throw new Error(`unknown add action ${act}`);
        }
        const ids = [...idsIn(url, body)];
        log.push({ tab, method, url, body, ids, present: ids.map((i) => members.has(i)) });
        if (/\/members\/[^/]+$/.test(url)) {
            if (!members.has(ids[0])) return resp(404, { error: 'member_not_found' });
            return resp(200, { id: ids[0], name: method === 'DELETE' ? '' : body.name, modifiedAt: 9 });
        }
        if (url.endsWith('/score-events')) {
            return resp(200, { outcomes: body.events.map((e) => ({ id: e.id, outcome: 'applied' })), match: MATCH });
        }
        if (url.endsWith('/score')) return resp(200, { ...MATCH, applied: true });
        return resp(200, { positions: body.positions, memberIds: body.memberIds || {} });
    });
    const dropped = { a: 0, b: 0 };
    const load = async (name) => {
        // A fresh instance each run (vi.resetModules), under one query per tab.
        const API = (await import(/* @vite-ignore */ `${CLIENT}?tab=${name}`)).API;
        const onStorage = window.__bcStoredQueueHandler;
        window.removeEventListener('storage', onStorage);
        window.__bcStoredQueueHandler = undefined;
        const onOnline = window.__bcOnlineFlushHandler;
        window.removeEventListener('online', onOnline);
        window.__bcOnlineFlushHandler = undefined;
        tabs[name] = { name, API, onStorage, onOnline };
        return API;
    };
    // The event is dispatched on the one window both tabs share: counted once
    // per dispatch, whichever tab raised it.
    const onDrop = () => { dropped.a++; };
    window.addEventListener('team-member-dropped', onDrop);
    vi.resetModules();
    const A = await load('a');
    if (twoTab) await load('b');
    const sender = twoTab ? tabs.b : tabs.a;
    const fire = (T) => {
        const r = WRITES[w](T, mintedId, pw);
        const settle = (x) => x && x.settled && x.settled.catch(() => {});
        if (r && r.then) r.then(settle).catch(() => {}); else settle(r);
    };
    try {
        const member = await A.addTeamMember('c1', 't1', 'Ren Abe', pw, quiet);
        if (member.settled) member.settled.catch(() => {});
        mintedId = member.id;
        if (sc.serverHasIdAlready) members.add(mintedId);
        if (sc.refuseStorage) refuse = true;
        if (timing === 'T1') fire(A);
        await tick(10);
        if (sc.offlineFirst) { online.a = true; tabs.a.onOnline(); await tick(10); }
        if (twoTab) { await tick(10); sender.onOnline(); await tick(10); }
        if (sc.discard) {
            for (let i = 0; i < 40 && !sender.API.heldWrites().some((h) => h.kind === 'member' && h.keepsFailing); i++) await tick(2000);
            if (sc.discard === 'out') {
                for (let i = 0; i < 20 && !pending; i++) await tick(1000);
                expect(pending, `${scName}: the add is out when it is discarded`).toBeTruthy();
            }
            const h = sender.API.heldWrites().find((x) => x.kind === 'member' && x.keepsFailing);
            expect(h, `${scName}: the add keeps failing`).toBeTruthy();
            expect(sender.API.discardHeldWrite(h.key), `${scName}: the add is discarded`).toBeTruthy();
            await tick(0);
            if (timing === 'T2') fire(A);
            await tick(10);
            // The other tab is online while the discarded request may still be out.
            if (twoTab) { online.a = true; tabs.a.onOnline(); await tick(10); }
            if (sc.discard === 'out' && pending) {
                if (sc.release === 'timeout') {
                    await tick(FETCH_TIMEOUT_MS + 1000);
                } else {
                    const act = sc.release;
                    if (act === 'commit201') members.add(pending.id);
                    const st = act === 'commit201' ? 201 : act === 'r409limit' ? 409 : 500;
                    const b = act === 'commit201' ? { id: pending.id, name: 'Ren Abe', modifiedAt: 5 }
                        : act === 'r409limit' ? REFUSAL.r409limit : { error: 'internal error' };
                    pending.resolve({ ok: st < 300, status: st, headers: { get: () => null }, json: async () => b, text: async () => JSON.stringify(b) });
                }
                pending = null;
            }
        }
        if (sc.resume) { await tick(3000); sender.API.resumeAfterAuth('pw'); }
        for (let i = 0; i < 40; i++) await tick(2000);
        if (twoTab && !online.a) { online.a = true; tabs.a.onOnline(); for (let i = 0; i < 20; i++) await tick(2000); }
    } finally {
        window.removeEventListener('team-member-dropped', onDrop);
    }
    // Classify every request of the write under test.
    const held = members.has(mintedId);
    const reqs = log.filter((r) => r.method && isWrite(w, r));
    const classify = (r) => {
        const carries = r.ids.includes(mintedId);
        const present = carries && r.present[r.ids.indexOf(mintedId)];
        if (carries && present) return 'OK_WITH_ID';
        if (carries && held) return 'EARLY';
        if (carries) return namesByUrl(w) ? 'DEAD_MEMBER_WRITE' : 'DEAD_ID';
        return held ? 'STRIPPED_EXISTING' : 'OK_STRIPPED';
    };
    const each = reqs.map(classify);
    const queued = Object.values(tabs).flatMap((t) => t.API.heldWrites().map((h) => `${t.name}:${h.kind}`));
    let outcome;
    if (queued.length > 0) outcome = 'STALL';
    else if (reqs.length === 0) outcome = namesByUrl(w) && !held ? 'OK_NOT_SENT' : 'LOST';
    else outcome = each.find((c) => !c.startsWith('OK')) || each[each.length - 1];
    const reads = log.filter((r) => r.read);
    for (const t of Object.values(tabs)) { try { t.API.clearQueue(); } catch (_) { /* ignore */ } }
    vi.clearAllTimers();
    return {
        sc: scName, w, timing, twoTab, held, outcome, each, queued, dropped: dropped.a,
        reads: reads.map((r) => `${r.tab}:${r.url.includes('viewer') ? 'viewer' : 'members'}:${JSON.stringify(r.hdr)}`),
        rule: sc.rule || '',
    };
}

// What each rule-accepted scenario may give, and nothing else.
const ACCEPTED = {
    readFails: new Set(['OK_WITH_ID', 'DEAD_ID', 'DEAD_MEMBER_WRITE']),
    // A rename or a cleared name of the member left unsent is then lost too.
    commitAfterRead: new Set(['OK_STRIPPED', 'STRIPPED_EXISTING', 'OK_NOT_SENT', 'OK_WITH_ID', 'LOST']),
};

function wrongOf(row) {
    if (row.rule) return ACCEPTED[row.rule].has(row.outcome) ? null : row.outcome;
    if (!row.outcome.startsWith('OK')) return row.outcome;
    // A read is made with the header, even empty; a participant's through the
    // public competition payload.
    for (const r of row.reads) {
        if (r.endsWith(':"OMITTED"')) return 'READ_WITHOUT_HEADER';
        if (S[row.sc].pw === '' && !r.includes(':viewer:')) return 'SELF_RUN_READ_MEMBERS_DOOR';
    }
    return null;
}

const ALL = [];
for (const sc of Object.keys(S)) {
    for (const w of Object.keys(WRITES)) {
        ALL.push([sc, w, 'T1', false]);
        if (S[sc].discard) ALL.push([sc, w, 'T2', false]);
    }
}
for (const sc of Object.keys(S)) {
    if (sc === 'offlineThenOk' || S[sc].oneTab) continue; // the other tab is the one that sends
    for (const w of Object.keys(WRITES)) {
        ALL.push([sc, w, 'T1', true]);
        if (S[sc].discard) ALL.push([sc, w, 'T2', true]);
    }
}

describe('a member id this device minted, in every state its add can reach, named by every write', () => {
    let saved;
    beforeAll(() => {
        vi.useFakeTimers();
        saved = { fetch: global.fetch, ES: global.EventSource, ls: global.localStorage };
        global.EventSource = class { constructor() { this.close = () => {}; } };
        global.EventSource.OPEN = 1;
    });
    // The outbox says what it keeps and retries; none of it is under test here.
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

    const rows = [];
    for (const sc of Object.keys(S)) {
        it(`${sc}: no id stripped from a member the server holds, no dead id sent, nothing stalls or is lost`, async () => {
            const wrong = [];
            for (const args of ALL.filter((a) => a[0] === sc)) {
                const row = await runOne(...args);
                rows.push(row);
                const bad = wrongOf(row);
                if (bad) wrong.push(`${row.w} ${row.timing}${row.twoTab ? ' two tabs' : ''}: ${bad} [${row.each.join(',')}]${row.queued.length ? ` queued=${row.queued.join(',')}` : ''}`);
            }
            expect(wrong).toEqual([]);
        }, 120000);
    }

    it('says what the rule accepts, explicitly: a dead id goes only where every read failed', () => {
        const counts = {};
        for (const r of rows) counts[r.outcome] = (counts[r.outcome] || 0) + 1;
        const wrongRows = rows.filter((r) => wrongOf(r));
        const bySc = {};
        for (const r of wrongRows) bySc[r.sc] = (bySc[r.sc] || 0) + 1;
        const accepted = rows.filter((r) => r.rule && !wrongOf(r) && !r.outcome.startsWith('OK'));
        // eslint-disable-next-line no-console
        console.info(`minted member permutations: ${rows.length} runs, ${wrongRows.length} wrong, ${accepted.length} rule-accepted`,
            JSON.stringify(counts), JSON.stringify(bySc));
        const dead = rows.filter((r) => r.outcome === 'DEAD_ID' || r.outcome === 'DEAD_MEMBER_WRITE');
        expect(dead.every((r) => r.rule === 'readFails')).toBe(true);
        const stripped = rows.filter((r) => r.outcome === 'STRIPPED_EXISTING' || r.outcome === 'LOST');
        expect(stripped.every((r) => r.rule === 'commitAfterRead')).toBe(true);
        expect(rows.length).toBe(ALL.length);
    });
});
