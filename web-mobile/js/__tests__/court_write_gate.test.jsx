// bc-sseq FR9: the one write gate. While a court console is on screen in this
// tab and another device runs its shiaijo (or nobody does and this tablet lost
// it earlier), every new write is refused at _placeWrite, never queued, and its
// door answers court_not_held; a refused tap is told through the tap-outcome
// channel. The withdrawal of a court call is the one door that does not ask.
// With no console mounted, or a holding one, nothing is gated.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { writeCourtNotHeld, notLandedBanner, notSavedText, tapNotAppliedText, writeRetryable, writeKeepsEditorOpen } from '../write_result.jsx';

vi.mock('../api_client_events.jsx', async (orig) => ({ ...(await orig()), _subscribeToEvents: () => () => {} }));

let API;
let cc;
let enqueue;
let control;
let controlStatus;
let _orig;
let offs;

const OTHER = { court: 'A', holderKey: 'bbbb', device: 'windows', youHold: false, youWereDisplaced: false, sameDevice: false, since: 1, lastActiveAt: 2 };
const HOLDS = { court: 'A', holderKey: 'aaaa', device: 'ipad', youHold: true, youWereDisplaced: false, sameDevice: true, since: 1, lastActiveAt: 2 };
const sent = [];

const settle = async () => { for (let i = 0; i < 5; i++) await new Promise((r) => setTimeout(r, 0)); };
const stored = () => JSON.parse(localStorage.getItem('bc_write_queue_v2') || '[]');
const match = { id: 'm1', compId: 'c1', status: 'running', court: 'A', phase: 'pool', modifiedAt: 10, scoreVersion: 1 };

async function load() {
  vi.resetModules();
  ({ API } = await import('../api_client.jsx'));
  cc = await import('../court_control.jsx');
  enqueue = await import('../api_client_enqueue.jsx');
}

async function mount(answer) {
  control = answer;
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
  controlStatus = 200;
  global.fetch = vi.fn((url, init = {}) => {
    const u = String(url);
    if (u.includes('/api/courts/')) return Promise.resolve({ ok: controlStatus < 400, status: controlStatus, json: async () => control });
    if (u.includes('/api/time')) return Promise.reject(new TypeError('clock'));
    sent.push({ url: u, method: init.method });
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

const finish = { status: 'completed', ipponsA: ['M', 'M'], ipponsB: [], winner: 'a' };

describe('a console another device runs', () => {
  beforeEach(async () => { await mount(OTHER); });

  it('refuses a score write before it is queued, with the court sentence', async () => {
    const res = await API.recordScore('c1', 'm1', finish, 'pw', match);
    expect(writeCourtNotHeld(res)).toBe(true);
    expect(writeRetryable(res)).toBe(false);
    expect(writeKeepsEditorOpen(finish, res)).toBe(true);
    expect(notSavedText(notLandedBanner(res))).toMatch(/^Not saved: this tablet does not run this shiaijo/);
    await expect(res.settled).rejects.toMatchObject({ code: 'court_not_held' });
    await settle();
    expect(stored()).toEqual([]);
    expect(sent.filter((s) => s.url.includes('/score'))).toEqual([]);
  });

  it('refuses every other console write door too, and queues nothing', async () => {
    const answers = [
      await API.recordDecision('c1', 'm1', { decision: 'kiken-injury', decisionBy: 'aka' }, 'pw'),
      await API.revertMatchToQueue('c1', 'm1', 'pw', { match }),
      await API.reopenMatch('c1', 'm1', 'pw', { reason: 'fix' }),
      await API.recordDaihyosen('c1', 'm1', 'pw', 10),
      await API.removeKachinukiBout('c1', 'm1', 'pw'),
      await API.updateMatchTime('c1', 'm1', '2026-11-26T10:00:00Z', 'pw', { was: { scheduledAt: '' } }),
      await API.putMatchLineup('c1', 't1', 'm1', ['a'], 'pw', ['u1'], ['0']),
      await API.sendAnnouncement('Shiaijo A: next match', 2, 'pw'),
    ];
    for (const res of answers) expect(writeCourtNotHeld(res)).toBe(true);
    await settle();
    expect(stored()).toEqual([]);
    expect(sent).toEqual([]);
  });

  it('a refused court move or time change is never shown: the court stays as it was', async () => {
    const seen = [];
    const off = API.subscribeLocalMatchPatch((p) => seen.push(p));
    const res = await API.moveMatchCourt('c1', 'm1', 'B', 'pw', { was: { court: 'A' }, stopsRunning: true, match });
    expect(writeCourtNotHeld(res)).toBe(true);
    const time = await API.updateMatchTime('c1', 'm1', '10:30', 'pw', { was: { scheduledAt: '10:00' } });
    expect(writeCourtNotHeld(time)).toBe(true);
    await settle();
    // Only the value it already had is ever said (the door's put-back).
    expect(seen.map((p) => p.data.result.court).filter(Boolean)).not.toContain('B');
    expect(seen.map((p) => p.data.result.scheduledAt).filter(Boolean)).not.toContain('10:30');
    if (typeof off === 'function') off();
    expect(stored()).toEqual([]);
  });

  it('a refused score write or send back is never shown on the displays of this machine', async () => {
    const { bridge } = await import('../court_bridge.jsx');
    const publish = vi.spyOn(bridge, 'publish');
    const res = await API.recordScore('c1', 'm1', finish, 'pw', match);
    expect(writeCourtNotHeld(res)).toBe(true);
    const back = await API.revertMatchToQueue('c1', 'm1', 'pw', { match });
    expect(writeCourtNotHeld(back)).toBe(true);
    await settle();
    expect(publish.mock.calls.filter(([kind]) => kind === 'patch')).toEqual([]);
    publish.mockRestore();
  });

  it('still withdraws its own court calls: the one door that does not ask', async () => {
    const res = await API.deleteAnnouncement('11111111-2222-4333-8444-555555555555', 'pw');
    expect(writeCourtNotHeld(res)).toBe(false);
    expect(stored().map(([, d]) => d.kind)).toEqual(['announce-remove']);
  });

  it('a refused tap is told to its editor and never shows on the board', () => {
    const told = [];
    API.subscribeTapOutcome('c1', 'm1', (o) => told.push(o));
    const { id } = API.recordTap('c1', 'm1', { kind: 'ippon.add', pos: 0, side: 'a', letter: 'M' }, 'pw', match);
    expect(told).toEqual([{ id, outcome: 'notQueued', reason: 'court_not_held' }]);
    expect(tapNotAppliedText(told[0])).toMatch(/^Not applied: this tablet does not run this shiaijo/);
    expect(API.pendingTaps('c1', 'm1')).toEqual([]);
    expect(stored()).toEqual([]);
    // Its seq is given back: the next tap allowed is not a gap.
    offs.pop()();
    const next = API.recordTap('c1', 'm1', { kind: 'ippon.add', pos: 0, side: 'a', letter: 'K' }, 'pw', match);
    expect(next.seq).toBe(1);
  });

  it('nothing is gated once the console is gone', async () => {
    offs.pop()();
    const res = await API.recordScore('c1', 'm1', finish, 'pw', match);
    expect(writeCourtNotHeld(res)).toBe(false);
    expect(stored().length).toBe(1);
  });
});

describe('a free shiaijo this tablet lost earlier', () => {
  it('is gated too', async () => {
    await mount({ ...OTHER, holderKey: '', youWereDisplaced: true });
    const res = await API.recordScore('c1', 'm1', finish, 'pw', match);
    expect(writeCourtNotHeld(res)).toBe(true);
  });
});

describe('no console, or a holding one', () => {
  it('an admin host with no console mounted is never gated or stamped', async () => {
    const res = await API.recordScore('c1', 'm1', finish, 'pw', match);
    expect(writeCourtNotHeld(res)).toBe(false);
    expect(cc.courtControlFor('A')).toBeNull();
    expect(stored().length).toBe(1);
    expect(stored()[0][1].courtControlCourt).toBeUndefined();
  });

  it('a holding console is not gated, and an entry carrying its court control is recorded for that shiaijo', async () => {
    await mount(HOLDS);
    const res = await API.recordScore('c1', 'm1', finish, 'pw', match);
    expect(writeCourtNotHeld(res)).toBe(false);
    enqueue._outboxWrite({
      kind: 'decision', compID: 'c1', matchID: 'm2', method: 'POST', url: '/api/competitions/c1/matches/m2/decision',
      payload: { decision: 'fusenpai', decisionBy: 'aka', modifiedAt: 20, courtControl: cc.courtControlFor('A') }, password: 'pw',
    });
    const entry = stored().map(([, d]) => d).find((d) => d.matchID === 'm2');
    expect(entry.courtControlCourt).toBe('A');
    expect(entry.payload.courtControl).toEqual({ tabId: cc.courtTabId(), deviceId: cc.courtDeviceId() });
    // The tablet keeps the control while that entry is queued.
    expect(cc.courtReleasable('A')).toBe(false);
  });

  it('a shiaijo the tournament no longer lists (404 not-listed) is not gated', async () => {
    controlStatus = 404;
    await mount({ error: 'not found' });
    expect(cc.courtControlView('A').state).toBe('not-listed');
    const res = await API.recordScore('c1', 'm1', finish, 'pw', match);
    expect(writeCourtNotHeld(res)).toBe(false);
    expect(stored().length).toBe(1);
  });
});
