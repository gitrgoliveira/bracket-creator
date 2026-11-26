// bc-drev: the words of the draw review. The server decides every meeting,
// move, number, seed warning and lineup; these helpers only word its answers,
// so each one is pinned against an answer of the shape the routes give.
import { describe, it, expect } from 'vitest';
import {
  reviewsDraw, canFirstMeet, stuckSentences, improvementSentence, optionLabel, drawVerdict, noSwapNeeded, byDojo,
} from '../admin_draw_review.jsx';
import {
  drawEntrants, drawPlaceLabel, meetingChange, separationSentences, movesSentence, numbersSentence,
  swapDetailLines, swapDoneToast, swapBody, competitorLabel, meetingMatchName, swapVerdict,
} from '../admin_draw_swap_check.jsx';

// A meeting in a bracket of three rounds: round 0 is the quarter-finals.
const meet = (matchId, round, extra = {}) => ({ matchId, round, rounds: 3, ...extra });
const side = (id, name, extra = {}) => ({ id, name, ...extra });

describe('which competitions the card reviews', () => {
  it('is pools + knockout and knockout only', () => {
    expect(reviewsDraw({ format: 'mixed' })).toBe(true);
    expect(reviewsDraw({ format: 'knockout' })).toBe(true);
    expect(reviewsDraw({ format: 'league' })).toBe(false);
    expect(reviewsDraw({ format: 'swiss' })).toBe(false);
    expect(reviewsDraw(null)).toBe(false);
  });

  it('reads a config with no format as a knockout, as the server does', () => {
    expect(reviewsDraw({})).toBe(true);
    expect(reviewsDraw({ format: '' })).toBe(true);
    expect(drawEntrants({ format: '' }, [], { rounds: [[{ sideA: { id: 'a', name: 'A' }, sideB: { id: 'b', name: 'B' } }]] })[0].entrants).toHaveLength(2);
  });
});

describe('the Can first meet cell', () => {
  it('reads Share Pool C for a same-pool pair', () => {
    expect(canFirstMeet({ samePool: 'Pool C' })).toEqual({ first: 'Share Pool C', second: '' });
  });

  it('names the earliest round, and the if-both-win round when it is a later match', () => {
    const row = { earliest: meet('r1-m0', 1), ifBothWin: meet('r2-m0', 2) };
    expect(canFirstMeet(row)).toEqual({ first: 'Semifinals', second: 'Final if both win their pools' });
  });

  it('has no second line when winning both pools meets in the same match', () => {
    const row = { earliest: meet('r2-m0', 2), ifBothWin: meet('r2-m0', 2) };
    expect(canFirstMeet(row)).toEqual({ first: 'Final', second: '' });
  });

  it('has no second line when winning both pools meets in another match of the same round, and names no match', () => {
    const row = { earliest: meet('r1-m0', 1, { matchNumber: 5 }), ifBothWin: meet('r1-m1', 1, { matchNumber: 6 }) };
    expect(canFirstMeet(row)).toEqual({ first: 'Semifinals', second: '' });
  });

  it('prefers the effective round to the raw position', () => {
    // A structural bye collapses a round: raw round 0 of 3 whose winner plays the final.
    expect(canFirstMeet({ earliest: meet('r0-m0', 0, { displayRound: 2 }) }).first).toBe('Semifinals');
  });

  it('says so when the two cannot meet in the knockout', () => {
    expect(canFirstMeet({}).first).toBe('Cannot meet in the knockout');
  });
});

describe('the suggestions', () => {
  it('words one improvement as before and after, in words and never an arrow', () => {
    expect(improvementSentence({ dojo: 'Team Omega', before: { samePool: 'Pool B' }, after: { earliest: meet('r1-m0', 1) } }))
      .toBe('Team Omega: shared Pool B; now not before the Semifinals.');
    expect(improvementSentence({ dojo: 'Team Omega', before: { earliest: meet('r0-m0', 0) }, after: {} }))
      .toBe('Team Omega: could first meet in the Quarterfinals; now cannot meet in the knockout.');
  });

  it('words the dojos no swap helps: one line for those meeting in the bracket, one per shared pool', () => {
    expect(stuckSentences([
      { dojo: 'Team Pi', meeting: { earliest: meet('r0-m1', 0) } },
      { dojo: 'Team Rho', meeting: { samePool: 'Pool A' } },
      { dojo: 'Team Sigma', meeting: { earliest: meet('r1-m0', 1) } },
    ])).toEqual([
      'Team Rho: no suggested swap takes them out of Pool A without making another dojo meet earlier.',
      'For Team Pi and Team Sigma, no suggested swap makes their first meeting later without making another dojo meet earlier.',
    ]);
    expect(stuckSentences([{ dojo: 'Team Pi', meeting: { earliest: meet('r0-m1', 0) } }]))
      .toEqual(['For Team Pi, no suggested swap makes their first meeting later without making another dojo meet earlier.']);
    expect(stuckSentences([])).toEqual([]);
  });
});

describe('the pickers', () => {
  it('list a pools draw by pool, each place 1-based in the pool\'s own order', () => {
    const pools = [
      { poolName: 'Pool A', players: [side('p1', 'Ann', { dojo: 'D1', seed: 1, number: 'K1' }), side('p2', 'Bo', { dojo: 'D2' })] },
      { poolName: 'Pool B', players: [side('p3', 'Cy', { dojo: 'D1' }), { name: 'No id' }] },
    ];
    const groups = drawEntrants({ format: 'mixed' }, pools, null);
    expect(groups.map((g) => g.label)).toEqual(['Pool A', 'Pool B']);
    expect(groups[0].entrants.map((e) => e.place)).toEqual([{ pool: 'Pool A', position: 1 }, { pool: 'Pool A', position: 2 }]);
    expect(groups[1].entrants.map((e) => e.id), 'a place with no participant id cannot be named').toEqual(['p3']);
    expect(optionLabel(groups[0].entrants[0])).toBe('K1 Ann (D1), seed 1');
    expect(optionLabel(groups[0].entrants[1])).toBe('Bo (D2)');
  });

  it('list a knockout draw by first-round match, side A of match m at slot 2m', () => {
    const bracket = { rounds: [[
      { matchNumber: 1, sideA: side('k1', 'Ann'), sideB: side('k2', 'Bo') },
      { matchNumber: 2, sideA: side('k3', 'Cy'), sideB: { id: '', name: '' } },
    ]] };
    const groups = drawEntrants({ format: 'knockout' }, [], bracket);
    expect(groups.map((g) => g.label)).toEqual(['First-round Match 1', 'First-round Match 2']);
    expect(groups[1].entrants.map((e) => [e.id, e.place])).toEqual([['k3', { slot: 2 }]]);
    expect(drawPlaceLabel({ slot: 3 }, bracket)).toBe('first-round Match 2');
  });

  it('mark a knockout seed from the roster, as a pool draw marks it from pools.csv', () => {
    const bracket = { rounds: [[{ matchNumber: 1, sideA: side('k1', 'Ann', { dojo: 'D1' }), sideB: side('k2', 'Bo', { dojo: 'D2' }) }]] };
    const comp = { format: 'knockout', players: [{ id: 'k1', seed: 1 }, { id: 'k2', seed: null }] };
    const [group] = drawEntrants(comp, [], bracket);
    expect(optionLabel(group.entrants[0])).toBe('Ann (D1), seed 1');
    expect(optionLabel(group.entrants[1])).toBe('Bo (D2)');
  });
});

describe('If you swap them', () => {
  it('says where each goes, naming the places for a swap inside one pool', () => {
    const moves = [
      { id: 'a', name: 'Arthur Dick', to: { pool: 'Pool C', position: 2 } },
      { id: 'b', name: 'Lena Park', to: { pool: 'Pool B', position: 1 } },
    ];
    expect(movesSentence(moves, null)).toBe('Arthur Dick goes to Pool C, Lena Park to Pool B.');
    // Named by the number each holds now, as the check's headline names them.
    const numbered = [{ id: 'a', name: 'Player 08', to: { pool: 'Pool C' } }, { id: 'b', name: 'Player 07', to: { pool: 'Pool A' } }];
    expect(movesSentence(numbered, null, [side('b', 'Player 07', { number: 'P8' }), side('a', 'Player 08', { number: 'P1' })]))
      .toBe('P1 Player 08 goes to Pool C, P8 Player 07 to Pool A.');
    const inside = [
      { id: 'a', name: 'Arthur', to: { pool: 'Pool B', position: 3 } },
      { id: 'b', name: 'Lena', to: { pool: 'Pool B', position: 1 } },
    ];
    expect(movesSentence(inside, null)).toBe('Arthur goes to Pool B, place 3, Lena to Pool B, place 1.');
  });

  it('words the separation per dojo, a dojo whose pairs stay put as no change', () => {
    const before = [
      { dojo: 'Team Omega', a: side('a', 'Arthur'), b: side('x', 'Xu'), samePool: 'Pool B' },
      { dojo: 'Team Kappa', a: side('b', 'Lena'), b: side('y', 'Yan'), earliest: meet('r2-m0', 2) },
    ];
    const after = [
      { dojo: 'Team Omega', a: side('a', 'Arthur'), b: side('x', 'Xu'), earliest: meet('r1-m0', 1) },
      { dojo: 'Team Kappa', a: side('b', 'Lena'), b: side('y', 'Yan'), earliest: meet('r2-m0', 2) },
    ];
    expect(separationSentences({ before, after }).map((l) => l.text)).toEqual([
      'Team Omega: shared Pool B; now not before the Semifinals.',
      'Team Kappa: no change.',
    ]);
  });

  it('marks a pair brought together earlier, and names the pair in a dojo of more than one', () => {
    const pair = (bId, bName, m) => ({ dojo: 'D', a: side('a', 'Ann'), b: side(bId, bName), ...m });
    const before = [pair('x', 'Xu', { earliest: meet('r2-m0', 2) }), pair('y', 'Yan', { earliest: meet('r2-m0', 2) })];
    const after = [pair('x', 'Xu', { samePool: 'Pool A' }), pair('y', 'Yan', { earliest: meet('r2-m0', 2) })];
    const lines = separationSentences({ before, after });
    expect(lines).toEqual([{ dojo: 'D', text: 'D (Ann and Xu): could first meet in the Final; now share Pool A.', earlier: true }]);
  });

  it('reads a move to another match at the same depth as no change, not as an earlier meeting', () => {
    const pair = (m) => ({ dojo: 'Fudo', a: side('a', 'Ann'), b: side('b', 'Bo'), ...m });
    const before = [pair({ earliest: meet('r1-m0', 1, { matchNumber: 5 }), ifBothWin: meet('r2-m0', 2, { matchNumber: 7 }) })];
    const after = [pair({ earliest: meet('r1-m1', 1, { matchNumber: 6 }), ifBothWin: meet('r2-m0', 2, { matchNumber: 7 }) })];
    expect(separationSentences({ before, after })).toEqual([{ dojo: 'Fudo', text: 'Fudo: no change.', earlier: false }]);
    expect(swapVerdict({ before, after })).toEqual({ worse: false, text: 'No dojo-mates meet earlier.' });
    // Sharing another pool is still sharing a pool.
    expect(separationSentences({ before: [pair({ samePool: 'Pool A' })], after: [pair({ samePool: 'Pool B' })] })[0].text).toBe('Fudo: no change.');
  });

  it('names the pair, the match and the pool winners\' meeting that the swap moves', () => {
    const pair = (m) => ({ dojo: 'Fudo', a: side('a', 'Player 08'), b: side('b', 'Player 04'), ...m });
    const before = [pair({ earliest: meet('r1-m0', 1, { matchNumber: 5 }), ifBothWin: meet('r2-m0', 2, { matchNumber: 7 }) }), { ...pair({}), b: side('c', 'Player 02'), samePool: 'Pool A' }];
    const after = [pair({ earliest: meet('r1-m1', 1, { matchNumber: 6 }), ifBothWin: meet('r1-m1', 1, { matchNumber: 6 }) }), { ...pair({}), b: side('c', 'Player 02'), samePool: 'Pool A' }];
    expect(separationSentences({ before, after })).toEqual([{
      dojo: 'Fudo',
      text: 'Fudo (Player 08 and Player 04): could first meet in the Semifinals (M5), or the Final if both win their pools; now the Semifinals (M6) either way.',
      earlier: true,
    }]);
  });
});

describe('a meeting that moves, in words', () => {
  const sf5 = meet('r1-m0', 1, { matchNumber: 5 });
  const sf6 = meet('r1-m1', 1, { matchNumber: 6 });
  const qf1 = meet('r0-m0', 0, { matchNumber: 1 });
  const qf2 = meet('r0-m1', 0, { matchNumber: 2 });
  const fin = meet('r2-m0', 2, { matchNumber: 7 });

  it('names a meeting by its round and the match the bracket card prints, the round alone without a number', () => {
    expect(meetingMatchName(sf5)).toBe('Semifinals (M5)');
    expect(meetingMatchName(meet('r1-m0', 1))).toBe('Semifinals');
    expect(meetingChange({ earliest: meet('r0-m0', 0) }, { earliest: meet('r1-m0', 1) }, true))
      .toBe('could first meet in the Quarterfinals; now not before the Semifinals');
  });

  it('a new earliest round says which way it moved', () => {
    expect(meetingChange({ earliest: qf2, ifBothWin: qf2 }, { earliest: sf5, ifBothWin: sf5 }, true))
      .toBe('could first meet in the Quarterfinals (M2); now not before the Semifinals (M5)');
    expect(meetingChange({ earliest: sf5, ifBothWin: sf5 }, { earliest: qf1, ifBothWin: qf1 }, false))
      .toBe('could first meet in the Semifinals (M5); now as early as the Quarterfinals (M1)');
  });

  it('a pool winners\' meeting in a later round is named on either side', () => {
    expect(meetingChange({ earliest: sf5, ifBothWin: sf5 }, { earliest: qf1, ifBothWin: fin }, false))
      .toBe('could first meet in the Semifinals (M5); now as early as the Quarterfinals (M1), or the Final if both win their pools');
    expect(meetingChange({ earliest: qf1, ifBothWin: sf5 }, { earliest: sf6, ifBothWin: fin }, true))
      .toBe('could first meet in the Quarterfinals (M1), or the Semifinals if both win their pools; now not before the Semifinals (M6), or the Final if both win their pools');
    expect(meetingChange({ samePool: 'Pool B' }, { earliest: sf5, ifBothWin: fin }, true))
      .toBe('shared Pool B; now not before the Semifinals (M5), or the Final if both win their pools');
  });

  it('"either way" says the pool winners\' later meeting is gone', () => {
    expect(meetingChange({ earliest: sf5, ifBothWin: fin }, { earliest: sf6, ifBothWin: sf6 }, false))
      .toBe('could first meet in the Semifinals (M5), or the Final if both win their pools; now the Semifinals (M6) either way');
    expect(meetingChange({ earliest: qf1, ifBothWin: fin }, { earliest: sf5, ifBothWin: sf5 }, true))
      .toBe('could first meet in the Quarterfinals (M1), or the Final if both win their pools; now not before the Semifinals (M5) either way');
  });

  it('the same round names the new match alone', () => {
    expect(meetingChange({ earliest: sf5, ifBothWin: sf5 }, { earliest: sf6, ifBothWin: fin }, true))
      .toBe('could first meet in the Semifinals (M5); now the Semifinals (M6), or the Final if both win their pools');
    expect(meetingChange({ earliest: sf5, ifBothWin: fin }, { earliest: sf6, ifBothWin: fin }, true))
      .toBe('could first meet in the Semifinals (M5), or the Final if both win their pools; now the Semifinals (M6), or the Final if both win their pools');
  });

  it('keeps the shared-pool and cannot-meet wordings', () => {
    expect(meetingChange({ earliest: sf5, ifBothWin: fin }, { samePool: 'Pool A' }, false))
      .toBe('could first meet in the Semifinals (M5), or the Final if both win their pools; now share Pool A');
    expect(meetingChange({ earliest: sf5 }, {}, true)).toBe('could first meet in the Semifinals (M5); now cannot meet in the knockout');
    expect(meetingChange({}, { earliest: qf1 }, false)).toBe('could not meet in the knockout; now as early as the Quarterfinals (M1)');
  });
});

describe('the check\'s verdict', () => {
  const pair = (bId, m) => ({ dojo: 'D', a: side('a', 'Ann'), b: side(bId, bId), ...m });
  const sf = meet('r1-m0', 1);
  const qf = meet('r0-m0', 0);

  it('counts the pairs brought into a pool and the pairs brought earlier, either part alone', () => {
    const before = [pair('x', { earliest: sf }), pair('y', { earliest: sf }), pair('z', { earliest: sf }), pair('w', { earliest: sf })];
    expect(swapVerdict({ before, after: [pair('x', { samePool: 'Pool A' }), pair('y', { samePool: 'Pool B' }), pair('z', { earliest: qf }), pair('w', { earliest: qf })] }))
      .toEqual({ worse: true, text: '2 pairs would share a pool and 2 would meet earlier.' });
    expect(swapVerdict({ before, after: [pair('x', { samePool: 'Pool A' }), pair('y', { earliest: sf }), pair('z', { earliest: qf }), pair('w', { earliest: sf })] }).text)
      .toBe('1 pair would share a pool and 1 would meet earlier.');
    expect(swapVerdict({ before, after: [pair('x', { samePool: 'Pool A' }), pair('y', { earliest: sf }), pair('z', { earliest: sf }), pair('w', { earliest: sf })] }).text)
      .toBe('1 pair would share a pool.');
    expect(swapVerdict({ before, after: [pair('x', { earliest: qf }), pair('y', { earliest: qf }), pair('z', { earliest: sf }), pair('w', { earliest: sf })] }).text)
      .toBe('2 pairs would meet earlier.');
  });

  it('says no dojo-mates meet earlier when every move is later, or there is none', () => {
    expect(swapVerdict({ before: [pair('x', { samePool: 'Pool A' })], after: [pair('x', { earliest: sf })] }))
      .toEqual({ worse: false, text: 'No dojo-mates meet earlier.' });
    expect(swapVerdict({ before: [], after: [] }).worse).toBe(false);
    expect(swapVerdict(undefined).worse).toBe(false);
  });
});

describe('the card\'s verdict', () => {
  const row = (dojo, bId, m) => ({ dojo, a: side(`a-${bId}`, 'A'), b: side(bId, bId), ...m });
  const qf = meet('r0-m0', 0);
  const sf = meet('r1-m0', 1);
  const fin = meet('r2-m0', 2);

  it('counts the pairs and dojos and names the earliest meeting', () => {
    const rows = [row('D1', 'a', { earliest: qf }), row('D1', 'b', { earliest: qf }), row('D2', 'c', { earliest: sf }), row('D3', 'd', { earliest: fin })];
    expect(drawVerdict(rows)).toBe('4 pairs from 3 dojos. None share a pool; the earliest any can meet is the Quarterfinals (2 pairs).');
    expect(drawVerdict(rows.slice(2))).toBe('2 pairs from 2 dojos. None share a pool; the earliest any can meet is the Semifinals (1 pair).');
    expect(drawVerdict(rows, { knockoutOnly: true })).toBe('4 pairs from 3 dojos. The earliest any can meet is the Quarterfinals (2 pairs).');
  });

  it('says how many share a pool', () => {
    expect(drawVerdict([row('D1', 'a', { samePool: 'Pool A' }), row('D1', 'b', { samePool: 'Pool B' }), row('D2', 'c', { earliest: qf })]))
      .toBe('3 pairs from 2 dojos. 2 pairs share a pool.');
    expect(drawVerdict([row('D1', 'a', { samePool: 'Pool A' }), row('D2', 'c', { earliest: qf })])).toBe('2 pairs from 2 dojos. 1 pair shares a pool.');
  });

  it('says when they can meet only in the Final, or not at all', () => {
    expect(drawVerdict([row('D1', 'a', { earliest: fin }), row('D2', 'b', {})])).toBe('2 pairs from 2 dojos. They can meet only in the Final.');
    expect(drawVerdict([row('D1', 'a', {}), row('D1', 'b', {})])).toBe('2 pairs from 1 dojo. None can meet in the knockout.');
  });

  it('words one pair', () => {
    expect(drawVerdict([row('D1', 'a', { samePool: 'Pool C' })])).toBe('1 pair from 1 dojo. They share Pool C.');
    expect(drawVerdict([row('D1', 'a', { earliest: qf })])).toBe('1 pair from 1 dojo. They do not share a pool and can first meet in the Quarterfinals.');
    expect(drawVerdict([row('D1', 'a', { earliest: qf })], { knockoutOnly: true })).toBe('1 pair from 1 dojo. They can first meet in the Quarterfinals.');
    expect(drawVerdict([row('D1', 'a', { earliest: fin })])).toBe('1 pair from 1 dojo. They can meet only in the Final.');
    expect(drawVerdict([row('D1', 'a', {})])).toBe('1 pair from 1 dojo. They cannot meet in the knockout.');
  });

  it('says nothing with no rows', () => {
    expect(drawVerdict([])).toBe('');
    expect(drawVerdict(null)).toBe('');
  });

  it('needs no swap only when no pair shares a pool or meets before the Final', () => {
    expect(noSwapNeeded([row('D1', 'a', { earliest: fin }), row('D2', 'b', {})])).toBe(true);
    expect(noSwapNeeded([])).toBe(true);
    expect(noSwapNeeded([row('D1', 'a', { earliest: sf })])).toBe(false);
    expect(noSwapNeeded([row('D1', 'a', { samePool: 'Pool A' })])).toBe(false);
    expect(noSwapNeeded(null), 'not read yet').toBe(false);
  });

  it('groups the rows by dojo in the order the dojos first appear, each dojo\'s pairs in the server\'s order', () => {
    const rows = [row('D2', 'a', { earliest: qf }), row('D1', 'b', { earliest: qf }), row('D2', 'c', { earliest: sf }), row('D1', 'd', { earliest: fin })];
    expect(byDojo(rows).map((g) => [g.dojo, g.rows.map((r) => r.b.id)])).toEqual([['D2', ['a', 'c']], ['D1', ['b', 'd']]]);
  });
});

describe('If you swap them: the numbers, the details and the request', () => {
  it('names the two tags to exchange, and nothing when the competition does not number', () => {
    expect(numbersSentence([{ name: 'Arthur', from: 'IMS4', to: 'IMS8' }, { name: 'Lena', from: 'IMS8', to: 'IMS4' }]))
      .toBe('Numbers stay with the place: Arthur takes IMS8, Lena takes IMS4. Swap their tags.');
    expect(numbersSentence(undefined)).toBe('');
  });

  it('warns about a seed, a rebuilt bracket and its new times, and words lineups and byes', () => {
    const lines = swapDetailLines({
      seededWarning: [{ id: 'a', name: 'Arthur', seed: 1, to: { pool: 'Pool C', position: 1 } }],
      bracketRebuilt: true, timesRescheduled: true, lineupsMoved: 2, lineupsRemoved: 1,
      byes: [{ id: 'a', name: 'Arthur', bye: true }, { id: 'b', name: 'Lena', bye: false }],
    }, null).map((l) => l.text);
    expect(lines).toEqual([
      'Arthur is seed 1 and moves to Pool C.',
      'The knockout bracket is rebuilt from the new pools.',
      'The knockout matches are scheduled again, so their times and shiaijo can change.',
      '2 saved lineups move with their teams.',
      '1 saved lineup has no match at the new place and is removed.',
      'Arthur now has a bye in the first round.',
      'Lena no longer has a bye in the first round.',
    ]);
    expect(swapDetailLines({ lineupsMoved: 0, lineupsRemoved: 0 }, null)).toEqual([]);
  });

  it('sends the two places it was given, with dryRun only for the check', () => {
    const a = { id: 'a', name: 'A', place: { pool: 'Pool A', position: 1 } };
    const b = { id: 'b', name: 'B', place: { pool: 'Pool B', position: 2 } };
    expect(swapBody(a, b, true)).toEqual({ a: { id: 'a', place: a.place }, b: { id: 'b', place: b.place }, dryRun: true });
    expect(swapBody(a, b, false)).toEqual({ a: { id: 'a', place: a.place }, b: { id: 'b', place: b.place } });
  });

  it('tells the swap once it is in, with the lineups for a team swap', () => {
    expect(swapDoneToast({ applied: true, lineupsMoved: 0 }, side('a', 'Arthur'), side('b', 'Lena'))).toBe('Swapped Arthur and Lena.');
    expect(swapDoneToast({ applied: true, lineupsMoved: 1, lineupsRemoved: 2 }, side('a', 'A'), side('b', 'B')))
      .toBe('Swapped A and B. 1 lineup moved. 2 lineups removed.');
    expect(swapDoneToast({ applied: false, reason: 'already_swapped' })).toBe('That swap was already made. The draw shows it now.');
  });

  it('names a competitor by number then name', () => {
    expect(competitorLabel(side('a', 'Arthur', { number: 'K3' }))).toBe('K3 Arthur');
    expect(meetingChange({}, { earliest: meet('r0-m0', 0) }, false)).toBe('could not meet in the knockout; now as early as the Quarterfinals');
  });
});
