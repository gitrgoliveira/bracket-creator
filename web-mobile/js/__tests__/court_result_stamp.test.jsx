// bc-sseq FR8: the court console's score and decision writes carry its court
// control, `courtControl: {tabId, deviceId}` (courtControlFor), so the server
// can give the result the court's standing on arrival. The stamp is put on
// the payload as the write is made, kept with the entry (persisted, with the
// shiaijo it was for), sent, checked on rehydrate, never shown to a display,
// and never inherited by a write that takes a queued one's place. An admin
// host's write, or a write of another shiaijo's match, is never stamped.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

vi.mock('../api_client_events.jsx', async (orig) => ({ ...(await orig()), _subscribeToEvents: () => () => {} }));

let API;
let cc;
let alerts;
let control;
let controlFails;
let scoreFails;
let _orig;
let offs;

const HOLDS = { court: 'A', holderKey: 'aaaa', device: 'ipad', youHold: true, youWereDisplaced: false, sameDevice: true, since: 1, lastActiveAt: 2 };
const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const TAB = 'abcdef12-3456-4789-8abc-def123456789';
const DEVICE = '22222222-2222-4222-8222-222222222222';
const sent = [];

const settle = async () => { for (let i = 0; i < 6; i++) await new Promise((r) => setTimeout(r, 0)); };
const stored = () => JSON.parse(localStorage.getItem('bc_write_queue_v2') || '[]');
const match = { id: 'm1', compId: 'c1', status: 'running', court: 'A', phase: 'pool', modifiedAt: 10, scoreVersion: 1 };
const finish = { status: 'completed', ipponsA: ['M', 'M'], ipponsB: [], winner: 'a' };
const writes = (path) => sent.filter((s) => s.url.endsWith(path) && s.method !== 'GET');
const bodyOf = (s) => JSON.parse(s.body);

async function load() {
    vi.resetModules();
    const mod = await import('../api_client.jsx');
    API = mod.API;
    alerts = [];
    mod.subscribeQueueAlert((a) => alerts.push(a));
    cc = await import('../court_control.jsx');
}

async function mount() {
    const off = cc.mountCourtControl('A', () => 'pw');
    offs.push(off);
    await settle();
    return off;
}

beforeEach(async () => {
    _orig = { fetch: global.fetch, ES: global.EventSource, BC: global.BroadcastChannel };
    global.BroadcastChannel = undefined;
    global.EventSource = class { constructor() { this.close = () => {}; } };
    localStorage.clear();
    sessionStorage.clear();
    sent.length = 0;
    offs = [];
    control = HOLDS;
    controlFails = false;
    scoreFails = false;
    global.fetch = vi.fn((url, init = {}) => {
        const u = String(url);
        if (u.includes('/api/courts/')) {
            if (controlFails) return Promise.reject(new TypeError('offline'));
            return Promise.resolve({ ok: true, status: 200, json: async () => control });
        }
        if (u.includes('/api/time')) return Promise.reject(new TypeError('clock'));
        sent.push({ url: u, method: init.method, body: init.body });
        if (scoreFails) return Promise.reject(new TypeError('offline'));
        // Every outbox send stays out, so what was queued stays queued.
        return new Promise(() => {});
    });
    await load();
});

afterEach(async () => {
    for (const off of offs) off();
    await settle();
    global.fetch = _orig.fetch;
    global.EventSource = _orig.ES;
    global.BroadcastChannel = _orig.BC;
    vi.resetModules();
});

describe('the court console stamps its results', () => {
    it('a score write from the console that runs the shiaijo carries {tabId, deviceId}, sent and kept with the entry', async () => {
        await mount();
        API.recordScore('c1', 'm1', finish, 'pw', match);
        await settle();
        const stamp = { tabId: cc.courtTabId(), deviceId: cc.courtDeviceId() };
        expect(stamp.tabId).toMatch(UUID);
        expect(stamp.deviceId).toMatch(UUID);
        const [[, d]] = stored();
        expect(d.payload.courtControl).toEqual(stamp);
        expect(d.courtControlCourt).toBe('A');
        const [put] = writes('/score');
        expect(bodyOf(put).courtControl).toEqual(stamp);
        // The tablet keeps the shiaijo while the stamped result is queued.
        expect(cc.courtReleasable('A')).toBe(false);
    });

    it('a running write (a start, an autosave) is stamped too', async () => {
        await mount();
        API.recordScore('c1', 'm1', { status: 'running', startOnly: true, changed: ['result'] }, 'pw', { ...match, status: 'scheduled' });
        await settle();
        expect(bodyOf(writes('/score')[0]).courtControl).toEqual({ tabId: cc.courtTabId(), deviceId: cc.courtDeviceId() });
    });

    it('a decision carries it when the caller names the shiaijo, and not otherwise', async () => {
        await mount();
        API.recordDecision('c1', 'm1', { decision: 'kiken-injury', decisionBy: 'aka' }, 'pw', { court: 'A' });
        API.recordDecision('c1', 'm2', { decision: 'fusenpai', decisionBy: 'shiro' }, 'pw');
        await settle();
        const byMatch = Object.fromEntries(stored().map(([, d]) => [d.matchID, d]));
        expect(byMatch.m1.payload.courtControl).toEqual({ tabId: cc.courtTabId(), deviceId: cc.courtDeviceId() });
        expect(byMatch.m1.courtControlCourt).toBe('A');
        expect(byMatch.m2.payload.courtControl).toBeUndefined();
        const posts = writes('/decision');
        expect(posts.map((p) => !!bodyOf(p).courtControl).sort()).toEqual([false, true]);
    });

    it('a console with no answer yet (opened offline) stamps too, so its result claims a free shiaijo on arrival', async () => {
        controlFails = true;
        await mount();
        expect(cc.courtControlView('A').state).toBe('unknown');
        API.recordScore('c1', 'm1', finish, 'pw', match);
        await settle();
        expect(stored()[0][1].payload.courtControl).toEqual({ tabId: cc.courtTabId(), deviceId: cc.courtDeviceId() });
    });

    it('an admin host (no court console on screen) is never stamped', async () => {
        API.recordScore('c1', 'm1', finish, 'pw', match);
        API.recordDecision('c1', 'm2', { decision: 'kiken-injury', decisionBy: 'aka' }, 'pw', { court: 'A' });
        await settle();
        for (const [, d] of stored()) {
            expect(d.payload.courtControl).toBeUndefined();
            expect(d.courtControlCourt).toBeUndefined();
        }
        for (const s of sent) expect(s.body ? JSON.parse(s.body).courtControl : undefined).toBeUndefined();
    });

    it("a match on another shiaijo is not stamped with this console's control", async () => {
        await mount();
        API.recordScore('c1', 'm3', finish, 'pw', { ...match, id: 'm3', court: 'B' });
        await settle();
        expect(stored()[0][1].payload.courtControl).toBeUndefined();
    });

    it('a display broadcast never carries it', async () => {
        await mount();
        const { bridge } = await import('../court_bridge.jsx');
        const publish = vi.spyOn(bridge, 'publish');
        API.recordScore('c1', 'm1', finish, 'pw', match);
        await settle();
        const patches = publish.mock.calls.filter(([kind]) => kind === 'patch');
        expect(patches.length).toBeGreaterThan(0);
        for (const [, msg] of patches) expect(JSON.stringify(msg)).not.toContain('courtControl');
        publish.mockRestore();
    });

    // The court console's points after another device's finish, with no court
    // result: the existing note ("... tap Correct") is the answer, and a
    // correction from the console has seen the stored result, so it counts.
    it("a correction from the console over another device's finish carries the stamp and a basis that saw the stored result", async () => {
        await mount();
        const stored4 = { ...match, status: 'completed', winner: 'b', scoreVersion: 4 };
        API.recordScore('c1', 'm1', { ...finish, correctionReason: 'the court saw Aka win' }, 'pw', stored4);
        await settle();
        const body = bodyOf(writes('/score')[0]);
        expect(body.courtControl).toEqual({ tabId: cc.courtTabId(), deviceId: cc.courtDeviceId() });
        expect(body.basis.seen).toBe(4);
        expect(body.correctionReason).toBe('the court saw Aka win');
    });
});

describe('the outbox keeps the newer write\'s stamp', () => {
    it('a write made after the console left takes a stamped write\'s place without its stamp', async () => {
        scoreFails = true;
        const off = await mount();
        API.recordScore('c1', 'm1', { status: 'running', ipponsA: ['M'], changed: ['points'] }, 'pw', match);
        await settle();
        expect(stored()[0][1].payload.courtControl).toBeTruthy();
        off();
        offs = offs.filter((o) => o !== off);
        await settle();
        API.recordScore('c1', 'm1', { status: 'running', ipponsA: ['M', 'K'], changed: ['points'] }, 'pw', match);
        await settle();
        const q = stored();
        expect(q).toHaveLength(1);
        expect(q[0][1].payload.ipponsA).toEqual(['M', 'K']);
        expect(q[0][1].payload.courtControl).toBeUndefined();
        expect(q[0][1].courtControlCourt).toBeUndefined();
    });

    it('a stamped write that takes an unstamped one\'s place carries its own stamp', async () => {
        scoreFails = true;
        API.recordScore('c1', 'm1', { status: 'running', ipponsA: ['M'], changed: ['points'] }, 'pw', match);
        await settle();
        await mount();
        API.recordScore('c1', 'm1', { status: 'running', ipponsA: ['M', 'K'], changed: ['points'] }, 'pw', match);
        await settle();
        const q = stored();
        expect(q).toHaveLength(1);
        expect(q[0][1].payload.courtControl).toEqual({ tabId: cc.courtTabId(), deviceId: cc.courtDeviceId() });
        expect(q[0][1].courtControlCourt).toBe('A');
    });
});

describe('a stored stamp is checked on rehydrate', () => {
    const entry = (over = {}, payloadOver = {}) => [`c1:m1\u001fr${Math.random().toString(36).slice(2)}`, {
        compID: 'c1', matchID: 'm1', password: 'pw', kind: 'score', terminal: true, method: 'PUT',
        url: '/api/competitions/c1/matches/m1/score', enqueuedAt: Date.now() - 1000,
        payload: { ...finish, modifiedAt: 5, writeId: 's.w1', courtControl: { tabId: TAB, deviceId: DEVICE }, ...payloadOver },
        courtControlCourt: 'A', ...over,
    }];
    const reload = async (items) => {
        localStorage.setItem('bc_write_queue_v2', JSON.stringify(items));
        sent.length = 0;
        await load();
        await settle();
    };

    it('a stamp this build made is replayed with it after a reload', async () => {
        await reload([entry()]);
        const [put] = writes('/score');
        expect(bodyOf(put).courtControl).toEqual({ tabId: TAB, deviceId: DEVICE });
    });

    const bad = {
        'a tab id that is no lowercase UUID v4': entry({}, { courtControl: { tabId: TAB.toUpperCase(), deviceId: DEVICE } }),
        'a missing tablet id': entry({}, { courtControl: { tabId: TAB } }),
        'a field this build never sends': entry({}, { courtControl: { tabId: TAB, deviceId: DEVICE, standing: true } }),
        'a stamp with no shiaijo named for it': entry({ courtControlCourt: undefined }),
        'a shiaijo named with no stamp': entry({}, { courtControl: undefined }),
        'a stamp on a write that is not a score or a decision': entry({ kind: 'time', url: '/api/competitions/c1/matches/m1/time' }),
    };
    for (const [name, item] of Object.entries(bad)) {
        it(`is unreadable, never sent: ${name}`, async () => {
            // The rehydrate says it discarded one unreadable entry.
            const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
            await reload([item]);
            expect(warn.mock.calls.some((c) => String(c[0]).includes('unreadable'))).toBe(true);
            expect(writes('/score')).toEqual([]);
            expect(writes('/time')).toEqual([]);
            expect(stored()).toEqual([]);
            expect(alerts.some((a) => a.kind === 'unreadable')).toBe(true);
            warn.mockRestore();
        });
    }
});
