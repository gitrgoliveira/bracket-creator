// bc-sseq FR8 (S2), the client half: the court's own taps while its result is
// queued. The court console scores a match another device already finished:
// each tap is answered matchClosed by the result in force, but the court's
// result, queued behind the taps, replaces that result when it lands, and the
// taps after it then count. So while this tab's outbox holds a court result
// for the match, such a tap is noted as landed and not told; once the result
// settles, the newest copy of the match says which are still turned away, and
// only those are told, once. With no court result queued, a tap answered
// matchClosed is told at once, as before.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

vi.mock('../api_client_events.jsx', async (orig) => ({ ...(await orig()), _subscribeToEvents: () => () => {} }));

let mod;
let API;
let cc;
let _orig;
let offs;
let finishAnswer;

const HOLDS = { court: 'A', holderKey: 'aaaa', device: 'ipad', youHold: true, youWereDisplaced: false, sameDevice: true, since: 1, lastActiveAt: 2 };
const settle = async () => { for (let i = 0; i < 8; i++) await new Promise((r) => setTimeout(r, 0)); };
const match = { id: 'm1', compId: 'c1', status: 'completed', court: 'A', phase: 'pool', modifiedAt: 10, scoreVersion: 2 };
const strike = { kind: 'ippon.add', pos: 0, side: 'a', letter: 'M' };
const finish = { status: 'completed', ipponsA: ['M', 'M'], ipponsB: [], winner: 'a' };
const json = (body) => ({ ok: true, status: 200, json: async () => body });

async function load() {
    vi.resetModules();
    mod = await import('../api_client.jsx');
    API = mod.API;
    cc = await import('../court_control.jsx');
}

function listen() {
    const alerts = [];
    const outcomes = [];
    mod.subscribeQueueAlert((a) => alerts.push(a));
    API.subscribeTapOutcome('c1', 'm1', (o) => outcomes.push(o));
    return { alerts, outcomes, notApplied: () => alerts.filter((a) => a.kind === 'not_applied') };
}

beforeEach(async () => {
    _orig = { fetch: global.fetch, ES: global.EventSource, BC: global.BroadcastChannel };
    global.BroadcastChannel = undefined;
    global.EventSource = class { constructor() { this.close = () => {}; } };
    localStorage.clear();
    sessionStorage.clear();
    offs = [];
    finishAnswer = null;
    global.fetch = vi.fn((url, init = {}) => {
        const u = String(url);
        if (u.includes('/api/courts/')) return Promise.resolve(json(HOLDS));
        if (u.includes('/api/time')) return Promise.reject(new TypeError('clock'));
        if (u.endsWith('/score-events')) {
            // Every tap turned away by the other device's finish in force.
            const body = JSON.parse(init.body);
            const away = Object.fromEntries(body.events.map((e) => [e.id, 'matchClosed']));
            return Promise.resolve(json({
                outcomes: body.events.map((e) => ({ id: e.id, outcome: 'matchClosed', reason: 'match_closed' })),
                match: { id: 'm1', status: 'completed', scoreVersion: 3, turnedAway: away },
            }));
        }
        if (u.endsWith('/score')) return new Promise((resolve) => { finishAnswer = (b) => resolve(json(b)); });
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

async function courtTapsThenFinish() {
    offs.push(cc.mountCourtControl('A', () => 'pw'));
    await settle();
    const heard = listen();
    const t = API.recordTap('c1', 'm1', strike, 'pw', match);
    API.recordScore('c1', 'm1', finish, 'pw', match);
    await settle();
    return { heard, t };
}

describe("the court's taps while its result is queued", () => {
    it('are not told while the court result waits, and never once it lands and they count', async () => {
        const { heard, t } = await courtTapsThenFinish();
        expect(finishAnswer, 'the court result is sent after the taps').toBeTypeOf('function');
        expect(heard.notApplied()).toEqual([]);
        expect(heard.outcomes.filter((o) => o.id === t.id)).toEqual([]);
        // The court's result replaces the other device's; its taps now count.
        finishAnswer({ ...match, status: 'completed', winner: 'a', scoreVersion: 4 });
        await settle();
        expect(heard.notApplied()).toEqual([]);
        expect(heard.outcomes.filter((o) => o.id === t.id)).toEqual([]);
    });

    it('are told once, from the newest copy, when the court result is held', async () => {
        const { heard, t } = await courtTapsThenFinish();
        expect(heard.notApplied()).toEqual([]);
        finishAnswer({
            applied: false, reason: 'superseded', heldGroups: ['result'], heldReason: 'downstream_built_on',
            message: "Not applied: round 2 was already paired from the result another device recorded for this match. The court's result is kept in the match's history. To use it, tap Correct; round 2 keeps its pairings.",
            match: { id: 'm1', status: 'completed', scoreVersion: 4, turnedAway: { [t.id]: 'matchClosed' } },
        });
        await settle();
        const told = heard.notApplied().filter((a) => a.turnedAway);
        expect(told).toHaveLength(1);
        expect(told[0].outcome).toMatchObject({ id: t.id, outcome: 'matchClosed' });
        expect(heard.outcomes.filter((o) => o.id === t.id).map((o) => o.outcome)).toEqual(['matchClosed']);
    });

    it('with no court result queued, a tap answered matchClosed is told at once', async () => {
        // An admin host: no court console on screen, so nothing is stamped.
        const heard = listen();
        const t = API.recordTap('c1', 'm1', strike, 'pw', match);
        await settle();
        expect(heard.outcomes.filter((o) => o.id === t.id).map((o) => o.outcome)).toEqual(['matchClosed']);
        expect(heard.notApplied()).toHaveLength(1);
    });

    it('a court result that settles with no copy of the match leaves the newest copy seen to decide', async () => {
        const { heard, t } = await courtTapsThenFinish();
        expect(heard.notApplied()).toEqual([]);
        // An answer that names no match: the newest copy seen (the taps'
        // answer, version 3) still lists the tap, so it is told once the
        // court result has left the queue.
        finishAnswer({ applied: true });
        await settle();
        const told = heard.notApplied().filter((a) => a.turnedAway);
        expect(told).toHaveLength(1);
        expect(told[0].outcome).toMatchObject({ id: t.id, outcome: 'matchClosed' });
    });
});
