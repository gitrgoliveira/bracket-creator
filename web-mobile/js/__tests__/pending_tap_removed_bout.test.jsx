// Stage 6 review 7 (minor 15): a pending tap on a bout another device removed
// left the board only when the server answered noTarget. The board folds the
// match's copy with its bout tombstones (the fold's own rule, BOUT_REMOVED),
// so the tap goes as soon as the removal is on screen, and the operator is
// told once.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { applyPending, makeEvent, BOUT_REMOVED, NO_TARGET, APPLIED, IPPON_ADD, BOUT_FIGHTER } from '../score_events.jsx';

const removedBoutCopy = () => ({
  id: 'm1', status: 'running', sideA: 'Taka', sideB: 'Kuma', sideAId: 'ta', sideBId: 'tb',
  subResults: [{ position: 1, sideA: 'Sato', sideB: 'Kumo', ipponsA: ['M'], ipponsB: [], winner: 'Sato', recorded: true }],
  // Bout 2 was added, then removed (its row gone, its stamp kept).
  groupStamps: { 'bout:1': 100, 'bout:2': 500 },
});
const ev = (over) => ({ ...makeEvent({ kind: IPPON_ADD, pos: 2, side: 'a', letter: 'M' }), id: 't1', session: 'dev1', seq: 1, stamp: 600, ...over });

describe('the board folds a pending tap against the bouts the copy shows removed', () => {
  it('a strike on a removed kachinuki bout is not on the board, with the reason the server gives', () => {
    const r = applyPending(removedBoutCopy(), [ev()], { team: true, kachinuki: true, teamSize: 3 });
    expect(r.outcomes.t1).toBe(NO_TARGET);
    expect(r.reasons.t1).toBe(BOUT_REMOVED);
    expect(r.match.subResults.map((s) => s.position)).toEqual([1]);
  });

  // Stage 6 review 8 (m9): one stamped before the removal is a change the
  // removal never saw, which the server answers beforeBaseline with no reason
  // (engine.onRemovedBout), so the board gives it none either.
  it('one stamped before the removal is not either, with no removed-bout reason; a fighter picked after it makes the bout again', () => {
    const early = applyPending(removedBoutCopy(), [ev({ stamp: 400 })], { team: true, kachinuki: true, teamSize: 3 });
    expect(early.outcomes.t1).toBe(NO_TARGET);
    expect(early.reasons.t1).toBeUndefined();
    const pick = applyPending(removedBoutCopy(), [ev({ id: 'f1', kind: BOUT_FIGHTER, letter: undefined, name: 'Ueda', stamp: 700 })], { team: true, kachinuki: true, teamSize: 3 });
    expect(pick.outcomes.f1).toBe(APPLIED);
    expect(pick.reasons.f1).toBeUndefined();
  });

  it('a bout never removed is made by the tap as before', () => {
    const copy = removedBoutCopy();
    delete copy.groupStamps['bout:2'];
    const r = applyPending(copy, [ev()], { team: true, kachinuki: true, teamSize: 3 });
    expect(r.outcomes.t1).toBe(APPLIED);
    expect(r.reasons.t1).toBeUndefined();
  });
});

describe('the operator is told once', () => {
  let _lsStore;
  let _orig;
  beforeEach(() => {
    vi.useFakeTimers();
    _orig = { fetch: global.fetch, ls: global.localStorage };
    _lsStore = {};
    global.localStorage = {
      getItem: (k) => (k in _lsStore ? _lsStore[k] : null),
      setItem: (k, v) => { _lsStore[k] = String(v); },
      removeItem: (k) => { delete _lsStore[k]; },
    };
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.resetModules();
    global.fetch = _orig.fetch;
    global.localStorage = _orig.ls;
  });

  it('by the board as the removal shows, and not again by the server answer', async () => {
    vi.resetModules();
    const mod = await import('../api_client.jsx');
    const API = mod.API;
    let online = false;
    global.fetch = vi.fn((url, opts) => {
      if (String(url).endsWith('/score-events') && online) {
        const body = JSON.parse(opts.body);
        return Promise.resolve({ ok: true, status: 200, json: async () => ({ outcomes: body.events.map((e) => ({ id: e.id, outcome: 'noTarget', reason: 'bout_removed' })) }) });
      }
      return Promise.reject(new TypeError('offline'));
    });
    const alerts = [];
    const notes = [];
    mod.subscribeQueueAlert((a) => { if (a.kind === 'not_applied') alerts.push(a); });
    API.subscribeTapOutcome('c1', 'm1', (o) => notes.push(o));
    const tap = API.recordTap('c1', 'm1', { kind: IPPON_ADD, pos: 2, side: 'a', letter: 'M' }, 'pw', { id: 'm1', compId: 'c1', status: 'running', modifiedAt: 0 });
    for (let i = 0; i < 30; i++) await Promise.resolve();
    // Every render of the board asks; it is told once.
    const outcome = { id: tap.id, outcome: NO_TARGET, reason: BOUT_REMOVED, message: 'removed' };
    API.tellPendingTapRefused('c1', 'm1', outcome, { kind: IPPON_ADD, side: 'a', pos: 2, letter: 'M' });
    API.tellPendingTapRefused('c1', 'm1', outcome, { kind: IPPON_ADD, side: 'a', pos: 2, letter: 'M' });
    for (let i = 0; i < 30; i++) await Promise.resolve();
    expect(alerts).toHaveLength(1);
    expect(notes.map((n) => n.reason)).toEqual([BOUT_REMOVED]);
    // The connection returns: the server answers the tap noTarget.
    online = true;
    await vi.advanceTimersByTimeAsync(60000);
    for (let i = 0; i < 30; i++) await Promise.resolve();
    expect(global.fetch.mock.calls.some((c) => String(c[0]).endsWith('/score-events'))).toBe(true);
    expect(alerts, 'not told again by the answer').toHaveLength(1);
    expect(notes).toHaveLength(1);
  });
});
