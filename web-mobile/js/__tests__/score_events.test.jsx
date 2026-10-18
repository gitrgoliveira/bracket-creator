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
  CLOCK_SKEW, BEFORE_BASELINE, REFUSED, MAX_FLAGS,
  makeEvent, markID, assignMarks, foldEvent, applyPending,
} from '../score_events.jsx';
import { tapOutcomeQuiet, tapNotAppliedText } from '../write_result.jsx';

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
    expect(MAX_FLAGS).toBe(5);
  });

  // Y2 (stage 3 review 11): which outcomes need no word has ONE owner,
  // write_result.jsx's tapOutcomeQuiet, pinned here against the codes.
  it('the quiet outcomes are exactly applied, recorded and duplicate', () => {
    const all = [APPLIED, RECORDED, DUPLICATE, CAPPED, BEFORE_BASELINE, MATCH_CLOSED, NO_TARGET, NOT_ALLOWED, NOT_STARTED, REFUSED, CLOCK_SKEW];
    expect(all.filter((outcome) => tapOutcomeQuiet({ outcome })).sort()).toEqual([APPLIED, DUPLICATE, RECORDED].sort());
  });

  // Y3: a tap still refused for the device clock after its one re-stamp says
  // it was the clock, not a bare refusal.
  it('a clock_skew refusal names the device clock', () => {
    const text = tapNotAppliedText({ outcome: CLOCK_SKEW });
    expect(text).toMatch(/clock/i);
    expect(text).not.toBe('Not applied: the server refused it.');
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

describe('score events: a team match folds on its bouts (stage 5)', () => {
  const TEAM = { team: true };
  const KACHI = { team: true, kachinuki: true };
  const row = (position, over = {}) => ({ position, sideA: '', sideB: '', ipponsA: [], ipponsB: [], hansokuA: 0, hansokuB: 0, winner: '', decision: '', ...over });
  const team = (rows, over = {}) => running({ sideA: 'Kyoto', sideB: 'Osaka', subResults: rows, ...over });
  // Folds with one fold state, as applyPending and the replay do.
  const foldTeam = (match, events, opts) => {
    const ctx = { bouts: new Map(), stamps: {} };
    let m = match;
    const outs = [];
    for (const e of events) {
      const r = foldEvent(m, e, opts, ctx);
      m = r.match;
      outs.push(r.outcome);
    }
    return { m, outs };
  };

  it('scores a bout row, never the match level, and a bout that is not there is no target', () => {
    const { m, outs } = fold(team([row(1)]), [
      ev({ id: 'm', kind: IPPON_ADD, pos: 1, side: SIDE_B, letter: 'M' }),
      ev({ kind: IPPON_ADD, pos: 0, side: SIDE_A, letter: 'K' }),
      ev({ kind: IPPON_ADD, pos: 2, side: SIDE_A, letter: 'K' }),
    ], TEAM);
    expect(outs).toEqual([APPLIED, NOT_ALLOWED, NO_TARGET]);
    expect(m.subResults[0]).toMatchObject({ ipponsB: ['M'], winner: 'Osaka', decision: '', markIds: { b: ['m'] } });
    expect(m.ipponsA).toEqual([]);
  });

  it('a second foul lands an H for the other side, named by the foul; a removal at 0 has no target', () => {
    const { m, outs } = fold(team([row(1)]), [
      ev({ kind: FOUL_ADD, pos: 1, side: SIDE_A }),
      ev({ id: 'f2', kind: FOUL_ADD, pos: 1, side: SIDE_A }),
      ev({ kind: FOUL_REMOVE, pos: 1, side: SIDE_A }),
    ], TEAM);
    expect(outs).toEqual([APPLIED, APPLIED, NO_TARGET]);
    expect(m.subResults[0]).toMatchObject({ ipponsB: ['H'], hansokuA: 0, winner: 'Osaka', markIds: { b: ['f2'] } });
  });

  it('a hikiwake is a tie a strike then ends; clear takes the tie off an unplayed bout', () => {
    const tied = fold(team([row(1)]), [ev({ kind: BOUT_DECISION, pos: 1, value: 'hikiwake' })], TEAM);
    expect(tied.m.subResults[0].decision).toBe('hikiwake');
    const cleared = fold(tied.m, [ev({ kind: BOUT_DECISION, pos: 1, value: 'clear' })], TEAM);
    expect(cleared.m.subResults[0].decision).toBe('');
    const struck = fold(tied.m, [ev({ kind: IPPON_ADD, pos: 1, side: SIDE_A, letter: 'K' })], TEAM);
    expect(struck.m.subResults[0]).toMatchObject({ decision: '', winner: 'Kyoto' });
  });

  it('a fusensho keeps the other side\'s point, and clearing it within one fold gives back the fouls it set aside', () => {
    const { m, outs } = foldTeam(team([row(1)]), [
      ev({ kind: IPPON_ADD, pos: 1, side: SIDE_B, letter: 'M' }),
      ev({ kind: FOUL_ADD, pos: 1, side: SIDE_A }),
      ev({ id: 'fz', kind: BOUT_DECISION, pos: 1, side: SIDE_A, value: 'fusensho' }),
    ], TEAM);
    expect(outs).toEqual([APPLIED, APPLIED, APPLIED]);
    expect(m.subResults[0]).toMatchObject({ ipponsA: ['○', '○'], ipponsB: ['M'], hansokuA: 0, decision: 'fusensho', winner: 'Kyoto', markIds: { a: ['fz', 'fz:1'] } });
    const ctx = { bouts: new Map(), stamps: {} };
    let step = foldEvent(team([row(1)]), ev({ kind: FOUL_ADD, pos: 1, side: SIDE_A }), TEAM, ctx).match;
    step = foldEvent(step, ev({ kind: BOUT_DECISION, pos: 1, side: SIDE_B, value: 'fusensho' }), TEAM, ctx).match;
    step = foldEvent(step, ev({ kind: BOUT_DECISION, pos: 1, value: 'clear' }), TEAM, ctx).match;
    expect(step.subResults[0]).toMatchObject({ ipponsA: [], ipponsB: [], hansokuA: 1, decision: 'hikiwake' });
  });

  it('a fusensho against the side that won the bout is not allowed, nor on the representative bout', () => {
    const won = team([row(1, { ipponsB: ['M', 'K'], winner: 'Osaka' }), row(-1, { sideA: 'Kyoto', sideB: 'Osaka', decision: 'daihyosen' })]);
    const { outs } = fold(won, [
      ev({ kind: BOUT_DECISION, pos: 1, side: SIDE_A, value: 'fusensho' }),
      ev({ kind: BOUT_DECISION, pos: -1, side: SIDE_A, value: 'fusensho' }),
    ], TEAM);
    expect(outs).toEqual([NOT_ALLOWED, NOT_ALLOWED]);
  });

  it('encounter overtime is a fixed-order team\'s, never a kachinuki encounter\'s', () => {
    expect(fold(team([row(1)]), [ev({ kind: ENCHO_ADD })], TEAM).m.encho).toEqual({ periodCount: 1 });
    expect(fold(team([row(1)]), [ev({ kind: ENCHO_ADD })], KACHI).outs).toEqual([NOT_ALLOWED]);
    const onBout = fold(team([row(1)]), [ev({ kind: ENCHO_ADD, pos: 1 }), ev({ kind: ENCHO_REMOVE, pos: 1 }), ev({ kind: ENCHO_REMOVE, pos: 1 })], KACHI);
    expect(onBout.outs).toEqual([APPLIED, APPLIED, NO_TARGET]);
  });

  // K1 (c): a fighter is a bout.fighter event on every bout, the first
  // included; bout 1's are the lineup's, which the server sends as its own
  // events (engine.openerSeatEvents), ordered by stamp like these.
  it('a kachinuki fighter is the bout\'s on every bout, the first included; the winner follows it', () => {
    const k = team([row(1, { sideA: 'K1', sideB: 'O1' }), row(2, { sideA: 'K1', sideB: '' })]);
    const { m, outs } = fold(k, [
      ev({ kind: 'bout.fighter', pos: 1, side: SIDE_B, name: 'O9', memberId: 'o9' }),
      ev({ kind: 'bout.fighter', pos: 2, side: SIDE_B, name: 'O2', memberId: 'o2' }),
      ev({ kind: IPPON_ADD, pos: 2, side: SIDE_B, letter: 'D' }),
    ], KACHI);
    expect(outs).toEqual([APPLIED, APPLIED, APPLIED]);
    expect(m.subResults[0]).toMatchObject({ sideB: 'O9', sideBMemberId: 'o9' });
    expect(m.subResults[1]).toMatchObject({ sideB: 'O2', sideBMemberId: 'o2', winner: 'O2', winnerMemberId: 'o2' });
    expect(fold(k, [ev({ kind: 'bout.fighter', pos: 2, side: SIDE_B, name: 'O2' })], TEAM).outs).toEqual([NOT_ALLOWED]);
  });

  it('a representative pick lands on the representative bout and its verdict only on a level score', () => {
    const dh = team([row(-1, { sideA: 'Kyoto', sideB: 'Osaka', decision: 'daihyosen' })]);
    const { m, outs } = fold(dh, [
      ev({ kind: REP_PICK, pos: -1, side: SIDE_B, memberId: 'o3' }),
      ev({ id: 'v', kind: 'daihyosen.verdict', pos: -1, side: SIDE_B }),
    ], TEAM);
    expect(outs).toEqual([APPLIED, APPLIED]);
    expect(m.subResults[0]).toMatchObject({ sideBMemberId: 'o3', ipponsB: ['Ht'], winner: 'Osaka', winnerMemberId: 'o3' });
    const cleared = fold(m, [ev({ kind: 'daihyosen.verdict', pos: -1, value: 'clear' })], TEAM);
    expect(cleared.m.subResults[0]).toMatchObject({ ipponsB: [], winner: '' });
    const ahead = fold(dh, [ev({ kind: IPPON_ADD, pos: -1, side: SIDE_A, letter: 'M' }), ev({ kind: 'daihyosen.verdict', pos: -1, side: SIDE_B })], TEAM);
    expect(ahead.outs).toEqual([APPLIED, NOT_ALLOWED]);
  });

  it('a board counts a bout tap once when the pushed copy already holds its mark, and keeps bouts open after a team kiken', () => {
    const tap = ev({ id: 'k', kind: IPPON_ADD, pos: 1, side: SIDE_A, letter: 'K' });
    const pushed = team([row(1, { ipponsA: ['K'], winner: 'Kyoto', markIds: { a: ['k'] } })]);
    const board = applyPending(pushed, [tap], TEAM);
    expect(board.outcomes.k).toBe(DUPLICATE);
    expect(board.match.subResults[0].ipponsA).toEqual(['K']);
    const kiken = team([row(1)], { status: 'completed', decision: 'kiken-voluntary', winner: 'Kyoto' });
    expect(applyPending(kiken, [tap], TEAM).outcomes.k).toBe(APPLIED);
    expect(applyPending(kiken, [tap], { team: true, kachinuki: true }).outcomes.k).toBe(MATCH_CLOSED);
    expect(applyPending({ ...kiken, decision: '' }, [tap], TEAM).outcomes.k).toBe(MATCH_CLOSED);
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

  // G1: a push that arrives before a tap's own answer already holds the tap;
  // a foul that lands no H and an overtime period leave no mark id, so the
  // server copy says per session the highest seq its log holds (eventSeqs).
  it('a tap the server copy holds by seq (eventSeqs) is a duplicate: a foul and an overtime period count once', () => {
    const server = running({ hansokuB: 1, encho: { periodCount: 1 }, eventSeqs: { dev: 2, other: 9 } });
    const pending = [
      { ...ev({ id: 'f1', kind: FOUL_ADD, side: SIDE_B }), session: 'dev', seq: 1 },
      { ...ev({ id: 'x1', kind: ENCHO_ADD }), session: 'dev', seq: 2 },
    ];
    const r = applyPending(server, pending);
    expect(r.outcomes).toEqual({ f1: DUPLICATE, x1: DUPLICATE });
    expect(r.match.hansokuB).toBe(1);
    expect(r.match.encho).toEqual({ periodCount: 1 });
  });

  it('only taps at or below their own session\'s seq are skipped', () => {
    const server = running({ hansokuB: 1, eventSeqs: { dev: 1 } });
    const pending = [
      { ...ev({ id: 'f1', kind: FOUL_ADD, side: SIDE_B }), session: 'dev', seq: 1 },
      { ...ev({ id: 'x1', kind: ENCHO_ADD }), session: 'dev', seq: 2 },
      { ...ev({ id: 'f2', kind: FOUL_ADD, side: SIDE_A }), session: 'elsewhere', seq: 1 },
      { ...ev({ id: 'f3', kind: FOUL_ADD, side: SIDE_A }) },
    ];
    const r = applyPending(server, pending);
    expect(r.outcomes).toEqual({ f1: DUPLICATE, x1: APPLIED, f2: APPLIED, f3: APPLIED });
    expect(r.match.hansokuB).toBe(1);
    // f2 and f3, two fouls by side A: the second lands an H for side B.
    expect(r.match.ipponsB).toEqual(['H']);
    expect(r.match.encho).toEqual({ periodCount: 1 });
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
