// bc-drev: the words of the draw review. The server decides every meeting,
// move, number, seed warning and lineup; these helpers only word its answers,
// so each one is pinned against an answer of the shape the routes give.
import { describe, it, expect } from 'vitest';
import {
  reviewsDraw, canFirstMeet, pairText, stuckSentence, improvementSentence, optionLabel,
} from '../admin_draw_review.jsx';
import {
  drawEntrants, drawPlaceLabel, meetingPhrase, separationSentences, movesSentence, numbersSentence,
  swapDetailLines, swapDoneToast, swapBody, competitorLabel,
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

  it('prefers the effective round to the raw position', () => {
    // A structural bye collapses a round: raw round 0 of 3 whose winner plays the final.
    expect(canFirstMeet({ earliest: meet('r0-m0', 0, { displayRound: 2 }) }).first).toBe('Semifinals');
  });

  it('says so when the two cannot meet in the knockout', () => {
    expect(canFirstMeet({}).first).toBe('Cannot meet in the knockout');
  });
});

describe('the Pair cell', () => {
  it('joins the two by "·", each with their number when the competition numbers', () => {
    expect(pairText({ a: side('a', 'Elijah Everdeen', { number: 'IMS2' }), b: side('b', 'Lena Park', { number: 'IMS5' }) }))
      .toBe('IMS2 Elijah Everdeen · IMS5 Lena Park');
    expect(pairText({ a: side('a', 'Elijah'), b: side('b', 'Lena') })).toBe('Elijah · Lena');
  });
});

describe('the suggestions', () => {
  it('words one improvement as before and after', () => {
    expect(improvementSentence({ dojo: 'Team Omega', before: { samePool: 'Pool B' }, after: { earliest: meet('r1-m0', 1) } }))
      .toBe('Team Omega: share Pool B → earliest Semifinals.');
  });

  it('words a stuck dojo by its earliest meeting', () => {
    expect(stuckSentence({ dojo: 'Team Pi', meeting: { earliest: meet('r0-m1', 0) } }))
      .toBe('Team Pi: no swap brings their Quarterfinals meeting later without making another dojo meet earlier.');
    expect(stuckSentence({ dojo: 'Team Pi', meeting: { samePool: 'Pool A' } }))
      .toBe('Team Pi: no swap takes them out of Pool A without making another dojo meet earlier.');
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
    expect(optionLabel(groups[0].entrants[0])).toBe('K1 Ann (D1) · seed 1');
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
});

describe('If you swap them', () => {
  it('says where each goes, naming the places for a swap inside one pool', () => {
    const moves = [
      { id: 'a', name: 'Arthur Dick', to: { pool: 'Pool C', position: 2 } },
      { id: 'b', name: 'Lena Park', to: { pool: 'Pool B', position: 1 } },
    ];
    expect(movesSentence(moves, null)).toBe('Arthur Dick goes to Pool C, Lena Park to Pool B.');
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
      'Team Omega: share Pool B → earliest Semifinals (later).',
      'Team Kappa: no change.',
    ]);
  });

  it('marks a pair brought together earlier, and names the pair in a dojo of more than one', () => {
    const pair = (bId, bName, m) => ({ dojo: 'D', a: side('a', 'Ann'), b: side(bId, bName), ...m });
    const before = [pair('x', 'Xu', { earliest: meet('r2-m0', 2) }), pair('y', 'Yan', { earliest: meet('r2-m0', 2) })];
    const after = [pair('x', 'Xu', { samePool: 'Pool A' }), pair('y', 'Yan', { earliest: meet('r2-m0', 2) })];
    const lines = separationSentences({ before, after });
    expect(lines).toEqual([{ dojo: 'D', text: 'D (Ann and Xu): earliest Final → share Pool A (earlier).', earlier: true }]);
  });

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
    expect(meetingPhrase({})).toBe('cannot meet in the knockout');
  });
});
