// draft_taps.jsx: a draft kept when another device started the match is sent
// as the taps that would have made it (operator decision D1 (a)), against the
// copy it was typed over, never against the board.
import { describe, it, expect } from 'vitest';
import { letterChanges, pointTaps, countTaps, individualDraftTaps, engiDraftTaps, teamDraftTaps } from '../draft_taps.jsx';

describe('letterChanges', () => {
  it('names the letters added in draft order and the base letters taken away, ignoring maru, Ht and placeholders', () => {
    expect(letterChanges(['M', '•'], ['M', 'K', 'Ht', '○'])).toEqual({ add: ['K'], remove: [] });
    expect(letterChanges(['M', 'K'], ['K'])).toEqual({ add: [], remove: ['M'] });
    expect(letterChanges(['M'], ['M', 'M'])).toEqual({ add: ['M'], remove: [] });
  });
});

describe('pointTaps', () => {
  it('removes name the board mark of the letter, the first not yet named; a letter the board lost sends nothing', () => {
    expect(pointTaps({ side: 'a', base: ['M', 'M', 'K'], draft: [], boardLetters: ['M', 'D', 'M'], boardIds: ['x1', 'y', 'x2'] })).toEqual([
      { kind: 'ippon.remove', side: 'a', target: 'x1' },
      { kind: 'ippon.remove', side: 'a', target: 'x2' },
    ]);
  });
  it('a bout carries its position', () => {
    expect(pointTaps({ pos: 2, side: 'b', base: [], draft: ['D'] })).toEqual([{ kind: 'ippon.add', pos: 2, side: 'b', letter: 'D' }]);
  });
});

describe('countTaps', () => {
  it('moves a count either way, one event per step', () => {
    expect(countTaps({ side: 'a', from: 0, to: 2, addKind: 'foul.add', removeKind: 'foul.remove' })).toEqual([
      { kind: 'foul.add', side: 'a' }, { kind: 'foul.add', side: 'a' },
    ]);
    expect(countTaps({ from: 2, to: 1, addKind: 'encho.add', removeKind: 'encho.remove' })).toEqual([{ kind: 'encho.remove' }]);
    expect(countTaps({ from: 1, to: 1, addKind: 'x', removeKind: 'y' })).toEqual([]);
  });
});

describe('individualDraftTaps', () => {
  it('points per side, then fouls, then overtime, then the pool representatives', () => {
    const base = { aPts: [], bPts: [], aFouls: 0, bFouls: 0, encho: 0, repA: '', repB: '' };
    const draft = { aPts: ['M'], bPts: ['K'], aFouls: 1, bFouls: 0, encho: 1, repA: 'Aoki', repB: '' };
    expect(individualDraftTaps(base, draft)).toEqual([
      { kind: 'ippon.add', side: 'a', letter: 'M' },
      { kind: 'ippon.add', side: 'b', letter: 'K' },
      { kind: 'foul.add', side: 'a' },
      { kind: 'encho.add' },
      { kind: 'pool.rep', side: 'a', name: 'Aoki' },
    ]);
  });
  it('an untouched draft sends nothing', () => {
    const same = { aPts: ['M'], bPts: [], aFouls: 1, bFouls: 0, encho: 0, repA: '', repB: '' };
    expect(individualDraftTaps(same, { ...same })).toEqual([]);
  });
});

describe('engiDraftTaps', () => {
  it('each side\'s flag count, as flag.add or flag.remove', () => {
    expect(engiDraftTaps({ a: 0, b: 1 }, { a: 2, b: 0 })).toEqual([
      { kind: 'flag.add', side: 'a' }, { kind: 'flag.add', side: 'a' }, { kind: 'flag.remove', side: 'b' },
    ]);
  });
});

describe('teamDraftTaps', () => {
  const row = (pos, o = {}) => ({ _pos: pos, aPts: [], bPts: [], aFouls: 0, bFouls: 0, fusensho: '', draw: false, encho: 0, ...o });
  it('bout by bout: points, fouls, overtime, then the result; the encounter overtime last', () => {
    const base = { rows: [row(1), row(2), row(3)], encho: 0 };
    const draft = {
      rows: [row(1, { aPts: ['M'], bFouls: 1 }), row(2, { draw: true }), row(3, { fusensho: 'a', aPts: ['○', '○'] })],
      encho: 1, enchoPos: 0,
    };
    expect(teamDraftTaps(base, draft)).toEqual([
      { kind: 'ippon.add', pos: 1, side: 'a', letter: 'M' },
      { kind: 'foul.add', pos: 1, side: 'b' },
      { kind: 'bout.decision', pos: 2, value: 'hikiwake' },
      { kind: 'bout.decision', pos: 3, side: 'a', value: 'fusensho' },
      { kind: 'encho.add' },
    ]);
  });
  it('the representative bout\'s picks lead its row, and it comes after the numbered bouts', () => {
    const base = { rows: [row(-1, { aMemberIdOverride: '', bMemberIdOverride: '' }), row(1)] };
    const draft = { rows: [row(-1, { aMemberIdOverride: 'k1', bMemberIdOverride: '', bPts: ['K'] }), row(1)] };
    expect(teamDraftTaps(base, draft)).toEqual([
      { kind: 'rep.pick', pos: -1, side: 'a', memberId: 'k1' },
      { kind: 'ippon.add', pos: -1, side: 'b', letter: 'K' },
    ]);
  });
});
