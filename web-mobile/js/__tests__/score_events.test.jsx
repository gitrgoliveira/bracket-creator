// Unit tests of the score-event leaf (bc-evts stage 3): the fold rules ported
// from internal/domain/score_event.go and internal/engine/score_replay.go, the
// mark ids, and applyPending (what a board shows: the server's last confirmed
// match with this device's pending taps on top). The replay itself is pinned
// against Go by score_replay_golden.test.jsx.
import { describe, it, expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { stripComments } from '../../check-helpers.mjs';
import {
  IPPON_ADD, IPPON_REMOVE, FOUL_ADD, FOUL_REMOVE, ENCHO_ADD, ENCHO_REMOVE, FLAG_ADD, FLAG_REMOVE,
  BOUT_DECISION, REP_PICK, POOL_REP, SIDE_A, SIDE_B,
  APPLIED, RECORDED, DUPLICATE, CAPPED, NO_TARGET, NOT_ALLOWED, MATCH_CLOSED, NOT_STARTED,
  CLOCK_SKEW, QUIET_OUTCOMES, MAX_FLAGS,
  makeEvent, markID, assignMarks, foldEvent, applyPending,
} from '../score_events.jsx';

let n = 0;
const ev = (fields) => ({ ...makeEvent(fields), id: fields.id || `e${++n}`, stamp: fields.stamp || 100 + n });
const running = (over = {}) => ({
  id: 'r0-m0', status: 'running', ipponsA: [], ipponsB: [], hansokuA: 0, hansokuB: 0,
  scoreVersion: 4, modifiedAt: 50, ...over,
});
// Folds events one after another onto a copy, returning the match and outcomes.
function fold(match, events, opts) {
  let m = match;
  const outs = [];
  for (const e of events) {
    const r = foldEvent(m, e, opts);
    m = r.match;
    outs.push(r.outcome);
  }
  return { m, outs };
}

describe('score events: constants and makeEvent', () => {
  it('spells the kinds, sides and outcomes as the server does', () => {
    expect([IPPON_ADD, IPPON_REMOVE, FOUL_ADD, FOUL_REMOVE, ENCHO_ADD, ENCHO_REMOVE, FLAG_ADD, FLAG_REMOVE])
      .toEqual(['ippon.add', 'ippon.remove', 'foul.add', 'foul.remove', 'encho.add', 'encho.remove', 'flag.add', 'flag.remove']);
    expect([SIDE_A, SIDE_B]).toEqual(['a', 'b']);
    expect([CLOCK_SKEW, NOT_STARTED, MATCH_CLOSED]).toEqual(['clock_skew', 'notStarted', 'matchClosed']);
    expect([...QUIET_OUTCOMES].sort()).toEqual([APPLIED, DUPLICATE, RECORDED].sort());
    expect(MAX_FLAGS).toBe(5);
  });

  it('makeEvent keeps only the fields given, with pos 0 by default, and no id or stamp', () => {
    expect(makeEvent({ kind: IPPON_ADD, side: SIDE_A, letter: 'M' })).toEqual({ kind: 'ippon.add', pos: 0, side: 'a', letter: 'M' });
    expect(makeEvent({ kind: IPPON_REMOVE, target: 'x' })).toEqual({ kind: 'ippon.remove', pos: 0, target: 'x' });
    expect(makeEvent({ kind: ENCHO_ADD })).toEqual({ kind: 'encho.add', pos: 0 });
  });
});

describe('score events: mark ids', () => {
  it('markID is b:<pos>:<side>:<letter>:<n>', () => {
    expect(markID(0, 'a', 'M', 1)).toBe('b:0:a:M:1');
  });
  it('assignMarks keeps a kept mark its id and mints the rest, skipping an id taken', () => {
    expect(assignMarks([], [], ['M', 'M', '•'], 0, 'a')).toEqual(['b:0:a:M:0', 'b:0:a:M:1', '']);
    expect(assignMarks(['M', 'K'], ['x', 'y'], ['K'], 0, 'b')).toEqual(['y']);
    expect(assignMarks(['M'], ['b:0:a:M:0'], ['M', 'M'], 0, 'a')).toEqual(['b:0:a:M:0', 'b:0:a:M:1']);
    expect(assignMarks(['K'], ['b:0:a:M:0'], ['M', 'K'], 0, 'a')).toEqual(['b:0:a:M:1', 'b:0:a:M:0']);
  });
});

describe('score events: the fold (individual)', () => {
  it('a strike lands with its event id beside it', () => {
    const { m, outs } = fold(running(), [ev({ id: 'm1', kind: IPPON_ADD, side: SIDE_A, letter: 'M' })]);
    expect(outs).toEqual([APPLIED]);
    expect(m.ipponsA).toEqual(['M']);
    expect(m.markIds).toEqual({ a: ['m1'] });
  });

  it('fills a placeholder slot before growing the side', () => {
    const { m } = fold(running({ ipponsA: ['•', 'K'], markIds: { a: ['', 'k'] } }), [ev({ id: 'm1', kind: IPPON_ADD, side: SIDE_A, letter: 'M' })]);
    expect(m.ipponsA).toEqual(['M', 'K']);
    expect(m.markIds.a).toEqual(['m1', 'k']);
  });

  it('caps a side at two and refuses the 2-2: once a side has two, the other is capped too', () => {
    const { m, outs } = fold(running(), [
      ev({ kind: IPPON_ADD, side: SIDE_A, letter: 'M' }),
      ev({ kind: IPPON_ADD, side: SIDE_A, letter: 'K' }),
      ev({ kind: IPPON_ADD, side: SIDE_A, letter: 'D' }),
      ev({ kind: IPPON_ADD, side: SIDE_B, letter: 'M' }),
    ]);
    expect(outs).toEqual([APPLIED, APPLIED, CAPPED, CAPPED]);
    expect(m.ipponsA).toEqual(['M', 'K']);
    expect(m.ipponsB).toEqual([]);
  });

  it('a hantei mark is no strike: the cap counts struck points only', () => {
    const { outs } = fold(running({ ipponsA: ['M', 'Ht'] }), [ev({ kind: IPPON_ADD, side: SIDE_A, letter: 'K' })]);
    expect(outs).toEqual([APPLIED]);
  });

  it('removes a mark by its id, on either side; an unknown id is noTarget; the maru cannot be removed', () => {
    const base = running({ ipponsA: ['M'], ipponsB: ['○'], markIds: { a: ['m1'], b: ['b:0:b:○:0'] } });
    const { m, outs } = fold(base, [
      ev({ kind: IPPON_REMOVE, target: 'm1' }),
      ev({ kind: IPPON_REMOVE, target: 'nope' }),
      ev({ kind: IPPON_REMOVE, target: 'b:0:b:○:0' }),
    ]);
    expect(outs).toEqual([APPLIED, NO_TARGET, NOT_ALLOWED]);
    expect(m.ipponsA).toEqual([]);
    expect(m.ipponsB).toEqual(['○']);
    expect(m.markIds).toEqual({ b: ['b:0:b:○:0'] });
  });

  it('the second foul lands an H on the OTHER side named by the foul, and the count goes back to 0', () => {
    const { m, outs } = fold(running(), [
      ev({ id: 'f1', kind: FOUL_ADD, side: SIDE_B }),
      ev({ id: 'f2', kind: FOUL_ADD, side: SIDE_B }),
    ]);
    expect(outs).toEqual([APPLIED, APPLIED]);
    expect(m.hansokuB).toBe(0);
    expect(m.ipponsA).toEqual(['H']);
    expect(m.markIds).toEqual({ a: ['f2'] });
    const back = fold(m, [ev({ kind: IPPON_REMOVE, target: 'f2' })]).m;
    expect(back.ipponsA).toEqual([]);
  });

  it('a second foul lands no H once either side holds two points, though the count still resets', () => {
    const { m } = fold(running({ ipponsA: ['M', 'K'], hansokuB: 1 }), [ev({ kind: FOUL_ADD, side: SIDE_B })]);
    expect(m.hansokuB).toBe(0);
    expect(m.ipponsA).toEqual(['M', 'K']);
  });

  it('the foul count is the points a maru side holds too (scoring ippons, not struck)', () => {
    // ApplyFoulIncrement counts scoring ippons: a maru counts.
    const { m } = fold(running({ ipponsA: ['○', '○'], hansokuB: 1 }), [ev({ kind: FOUL_ADD, side: SIDE_B })]);
    expect(m.ipponsA).toEqual(['○', '○']);
  });

  it('removes an outstanding foul; none outstanding is noTarget', () => {
    const { m, outs } = fold(running({ hansokuA: 1 }), [ev({ kind: FOUL_REMOVE, side: SIDE_A }), ev({ kind: FOUL_REMOVE, side: SIDE_A })]);
    expect(outs).toEqual([APPLIED, NO_TARGET]);
    expect(m.hansokuA).toBe(0);
  });

  it('overtime counts up and down, floored at 0, and none reads null', () => {
    const { m, outs } = fold(running(), [
      ev({ kind: ENCHO_ADD }), ev({ kind: ENCHO_ADD }), ev({ kind: ENCHO_REMOVE }), ev({ kind: ENCHO_REMOVE }), ev({ kind: ENCHO_REMOVE }),
    ]);
    expect(outs).toEqual([APPLIED, APPLIED, APPLIED, APPLIED, NO_TARGET]);
    expect(m.encho).toBeNull();
    expect(fold(running(), [ev({ kind: ENCHO_ADD })]).m.encho).toEqual({ periodCount: 1 });
  });

  it('an individual match takes no flags, no pool rep, no bout decision, and no bout position; a rep pick has no bout', () => {
    const { outs } = fold(running(), [
      ev({ kind: FLAG_ADD, side: SIDE_A }),
      ev({ kind: POOL_REP, side: SIDE_A, name: 'x' }),
      ev({ kind: BOUT_DECISION, pos: 1, value: 'hikiwake' }),
      ev({ kind: IPPON_ADD, pos: 1, side: SIDE_A, letter: 'M' }),
      ev({ kind: REP_PICK, pos: -1, side: SIDE_A, memberId: 'x' }),
    ]);
    expect(outs).toEqual([NOT_ALLOWED, NOT_ALLOWED, NOT_ALLOWED, NOT_ALLOWED, NO_TARGET]);
  });
});

describe('score events: the fold (engi)', () => {
  it('flags count 0..5 per side, and an engi match takes no strike, foul or overtime', () => {
    const six = Array.from({ length: 6 }, () => ev({ kind: FLAG_ADD, side: SIDE_B }));
    const { m, outs } = fold(running(), six, { engi: true });
    expect(outs).toEqual([APPLIED, APPLIED, APPLIED, APPLIED, APPLIED, CAPPED]);
    expect(m.flagsB).toBe(5);
    const down = fold(running(), [ev({ kind: FLAG_REMOVE, side: SIDE_A })], { engi: true });
    expect(down.outs).toEqual([NO_TARGET]);
    const other = fold(running(), [ev({ kind: IPPON_ADD, side: SIDE_A, letter: 'M' }), ev({ kind: FOUL_ADD, side: SIDE_A }), ev({ kind: ENCHO_ADD })], { engi: true });
    expect(other.outs).toEqual([NOT_ALLOWED, NOT_ALLOWED, NOT_ALLOWED]);
  });
});

describe('score events: team bouts are not folded yet', () => {
  it('throws rather than misfold a team match', () => {
    expect(() => foldEvent(running(), ev({ kind: IPPON_ADD, pos: 1, side: SIDE_A, letter: 'M' }), { team: true })).toThrow(/team/);
  });
});

describe('score events: applyPending', () => {
  it('shows the server match with the pending taps on top, keeping the server version and stamp', () => {
    const server = running({ ipponsA: ['M'], markIds: { a: ['b:0:a:M:0'] } });
    const pending = [ev({ id: 'p1', kind: IPPON_ADD, side: SIDE_B, letter: 'K' }), ev({ id: 'p2', kind: FOUL_ADD, side: SIDE_A })];
    const r = applyPending(server, pending);
    expect(r.match.ipponsB).toEqual(['K']);
    expect(r.match.hansokuA).toBe(1);
    expect(r.markIds).toEqual({ a: ['b:0:a:M:0'], b: ['p1'] });
    expect(r.match.markIds).toEqual(r.markIds);
    expect(r.outcomes).toEqual({ p1: APPLIED, p2: APPLIED });
    expect(r.match.scoreVersion).toBe(4);
    expect(r.match.modifiedAt).toBe(50);
  });

  it('reads a tap the fold would refuse as the server would: capped, noTarget', () => {
    const r = applyPending(running({ ipponsA: ['M', 'K'] }), [
      ev({ id: 'c', kind: IPPON_ADD, side: SIDE_B, letter: 'M' }),
      ev({ id: 't', kind: IPPON_REMOVE, target: 'gone' }),
    ]);
    expect(r.outcomes).toEqual({ c: CAPPED, t: NO_TARGET });
    expect(r.match.ipponsB).toEqual([]);
  });

  it('a tap already in the server match (its mark carries the tap id) is a duplicate, not a second point', () => {
    const r = applyPending(running({ ipponsA: ['M'], markIds: { a: ['p1'] } }), [ev({ id: 'p1', kind: IPPON_ADD, side: SIDE_A, letter: 'M' })]);
    expect(r.outcomes).toEqual({ p1: DUPLICATE });
    expect(r.match.ipponsA).toEqual(['M']);
  });

  it('a finished match changes nothing (matchClosed); a scheduled one takes no tap (notStarted)', () => {
    const done = applyPending(running({ status: 'completed', winner: 'Aka', ipponsA: ['M'] }), [ev({ id: 'x', kind: IPPON_ADD, side: SIDE_B, letter: 'K' })]);
    expect(done.outcomes).toEqual({ x: MATCH_CLOSED });
    expect(done.match.ipponsB).toEqual([]);
    const sched = applyPending(running({ status: 'scheduled' }), [ev({ id: 'y', kind: IPPON_ADD, side: SIDE_B, letter: 'K' })]);
    expect(sched.outcomes).toEqual({ y: NOT_STARTED });
  });

  it('never mutates the server match or the pending list', () => {
    const server = running({ ipponsA: ['M'], markIds: { a: ['b:0:a:M:0'] }, encho: { periodCount: 1 } });
    const pending = [ev({ kind: IPPON_REMOVE, target: 'b:0:a:M:0' }), ev({ kind: ENCHO_ADD }), ev({ kind: FOUL_ADD, side: SIDE_A })];
    const before = JSON.stringify([server, pending]);
    applyPending(server, pending);
    expect(JSON.stringify([server, pending])).toBe(before);
  });
});

describe('score events: the module is a leaf', () => {
  it('imports nothing', () => {
    const src = stripComments(readFileSync(resolve(__dirname, '..', 'score_events.jsx'), 'utf8'));
    expect(src).not.toMatch(/^\s*import\b/m);
    expect(src).not.toMatch(/\bwindow\./);
  });
});
