import { describe, it, expect } from 'vitest';
import {
  isTeamDefaultWinDecision, creditedSideKey, teamDefaultWinCreditActive,
  subBoutHasResult, creditedBoutSide, creditedTotals,
} from '../team_default_credit.jsx';

describe('isTeamDefaultWinDecision: the default-win decision class', () => {
  it('is true for every kiken variant, fusenpai and fusensho', () => {
    for (const d of ['kiken', 'kiken-voluntary', 'kiken-injury', 'fusenpai', 'fusensho']) {
      expect(isTeamDefaultWinDecision(d)).toBe(true);
    }
  });
  it('is false for fought/hikiwake/daihyosen/empty/unknown', () => {
    for (const d of ['fought', 'hikiwake', 'daihyosen', '', undefined, 'nonsense']) {
      expect(isTeamDefaultWinDecision(d)).toBe(false);
    }
  });
});

describe('creditedSideKey: the OTHER side from decisionBy, else the winner (bc-cse #9)', () => {
  it('shiro withdrew -> aka (sideA) is credited', () => {
    expect(creditedSideKey({ decisionBy: 'shiro' })).toBe('a');
  });
  it('aka withdrew -> shiro (sideB) is credited', () => {
    expect(creditedSideKey({ decisionBy: 'aka' })).toBe('b');
  });
  it('returns "" for a missing/unrecognised decisionBy and no winner to fall back to', () => {
    expect(creditedSideKey({ decisionBy: '' })).toBe('');
    expect(creditedSideKey({ decisionBy: undefined })).toBe('');
    expect(creditedSideKey({ decisionBy: 'nonsense' })).toBe('');
    expect(creditedSideKey(undefined)).toBe('');
  });
  it('decisionBy wins when set, even with a winner attribution present (Go: decisionBy checked first)', () => {
    expect(creditedSideKey({
      decisionBy: 'shiro',
      winner: { id: 'p-shiro', name: 'Shiro Competitor' },
      sideA: { id: 'p-aka', name: 'Aka Competitor' },
      sideB: { id: 'p-shiro', name: 'Shiro Competitor' },
    })).toBe('a');
  });
  it('empty decisionBy falls back to the winner side by id (legacy row, mirrors Go AttributeWinnerSide)', () => {
    expect(creditedSideKey({
      decisionBy: '',
      winner: { id: 'p-aka' },
      sideA: { id: 'p-aka', name: 'Aka Competitor' },
      sideB: { id: 'p-shiro', name: 'Shiro Competitor' },
    })).toBe('a');
    expect(creditedSideKey({
      decisionBy: '',
      winner: { id: 'p-shiro' },
      sideA: { id: 'p-aka', name: 'Aka Competitor' },
      sideB: { id: 'p-shiro', name: 'Shiro Competitor' },
    })).toBe('b');
  });
  it('empty decisionBy falls back to the winner side by name when no ids are present', () => {
    expect(creditedSideKey({
      decisionBy: '',
      winner: 'Aka Competitor',
      sideA: 'Aka Competitor',
      sideB: 'Shiro Competitor',
    })).toBe('a');
  });
  it('returns "" when neither decisionBy nor the winner attribution can name a side', () => {
    expect(creditedSideKey({ decisionBy: '', winner: '', sideA: 'Aka Competitor', sideB: 'Shiro Competitor' })).toBe('');
    expect(creditedSideKey({
      decisionBy: '',
      winner: { id: 'someone-else' },
      sideA: { id: 'p-aka', name: 'Aka Competitor' },
      sideB: { id: 'p-shiro', name: 'Shiro Competitor' },
    })).toBe('');
  });
});

describe('teamDefaultWinCreditActive: the ruling-in-force gate', () => {
  const base = { status: 'completed', decision: 'kiken-voluntary', decisionBy: 'shiro', kachinuki: false };
  it('active for a completed non-kachinuki default-win match with decisionBy', () => {
    expect(teamDefaultWinCreditActive(base)).toBe(true);
  });
  it('inactive while the match is still running', () => {
    expect(teamDefaultWinCreditActive({ ...base, status: 'running' })).toBe(false);
  });
  it('inactive for kachinuki: no fixed teamSize of positions to pad out to', () => {
    expect(teamDefaultWinCreditActive({ ...base, kachinuki: true })).toBe(false);
  });
  it('inactive for a decision outside the default-win class', () => {
    expect(teamDefaultWinCreditActive({ ...base, decision: 'fought' })).toBe(false);
  });
  it('inactive with no decisionBy to name a credited side', () => {
    expect(teamDefaultWinCreditActive({ ...base, decisionBy: '' })).toBe(false);
  });
});

describe('subBoutHasResult: the wire-level twin of Go SubMatchResult.HasResult', () => {
  it('false for a padding row with nothing recorded', () => {
    expect(subBoutHasResult({ position: 3 })).toBe(false);
    expect(subBoutHasResult(undefined)).toBe(false);
  });
  it('true when a winner name is recorded', () => {
    expect(subBoutHasResult({ position: 1, winner: 'Taro' })).toBe(true);
  });
  it('true when a decision string is recorded (even hikiwake)', () => {
    expect(subBoutHasResult({ position: 1, decision: 'hikiwake' })).toBe(true);
  });
  it('true for a real struck ippon on either side', () => {
    expect(subBoutHasResult({ position: 1, ipponsA: ['M'], ipponsB: [] })).toBe(true);
    expect(subBoutHasResult({ position: 1, ipponsA: [], ipponsB: ['K'] })).toBe(true);
  });
  it('false for a row carrying only the unfilled placeholder', () => {
    expect(subBoutHasResult({ position: 1, ipponsA: ['•'], ipponsB: ['•'] })).toBe(false);
  });
  it('true for the hantei mark alone, with no other score', () => {
    expect(subBoutHasResult({ position: 1, ipponsA: ['Ht'], ipponsB: [] })).toBe(true);
  });
  it('true for a hansoku foul with no ippons', () => {
    expect(subBoutHasResult({ position: 1, hansokuA: 1 })).toBe(true);
    expect(subBoutHasResult({ position: 1, hansokuB: 1 })).toBe(true);
  });
  it('true for an overtime period with no other score', () => {
    expect(subBoutHasResult({ position: 1, encho: { periodCount: 1 } })).toBe(true);
  });
  it('false for a zero-period encho block (degenerate, not real overtime)', () => {
    expect(subBoutHasResult({ position: 1, encho: { periodCount: 0 } })).toBe(false);
  });
});

describe('creditedBoutSide: per-bout credit under the active ruling', () => {
  const ctx = { status: 'completed', decision: 'fusenpai', decisionBy: 'aka', kachinuki: false };
  it('credits shiro (sideB) when aka withdrew and the bout has no result', () => {
    expect(creditedBoutSide({ position: 2 }, ctx)).toBe('b');
  });
  it('credits nothing when the bout already has its own result', () => {
    expect(creditedBoutSide({ position: 2, winner: 'Someone' }, ctx)).toBe('');
  });
  it('credits nothing when the ruling is not active', () => {
    expect(creditedBoutSide({ position: 2 }, { ...ctx, status: 'running' })).toBe('');
  });
  it('credits nothing for kachinuki even with a matching decision', () => {
    expect(creditedBoutSide({ position: 2 }, { ...ctx, kachinuki: true })).toBe('');
  });
  it('handles a bout the wire has not sent at all (undefined sub)', () => {
    expect(creditedBoutSide(undefined, ctx)).toBe('b');
  });
});

describe('creditedTotals: IV +1 / PW +2 per credited bout, all to one side', () => {
  it('awards ivA/pwA for side "a"', () => {
    expect(creditedTotals(3, 'a')).toEqual({ ivA: 3, ivB: 0, pwA: 6, pwB: 0 });
  });
  it('awards ivB/pwB for side "b"', () => {
    expect(creditedTotals(2, 'b')).toEqual({ ivA: 0, ivB: 2, pwA: 0, pwB: 4 });
  });
  it('is all-zero for a falsy side regardless of count', () => {
    expect(creditedTotals(5, '')).toEqual({ ivA: 0, ivB: 0, pwA: 0, pwB: 0 });
  });
  it('is all-zero for a zero count regardless of side', () => {
    expect(creditedTotals(0, 'a')).toEqual({ ivA: 0, ivB: 0, pwA: 0, pwB: 0 });
  });
});
