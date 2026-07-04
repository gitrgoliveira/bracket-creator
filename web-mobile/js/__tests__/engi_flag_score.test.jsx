import { describe, it, expect } from 'vitest';
import { engiFlagScore, matchScoreStr } from '../bracket.jsx';

// engiFlagScore derives an engi match's flag-count score string from
// FlagsA (sideA=Aka) / FlagsB (sideB=Shiro), in "Shiro–Aka" order to match
// formatIpponsScore's convention. Engi is the ONLY competition type where a
// completed match result is numeric; every other type shows ippon letters.

describe('engiFlagScore', () => {
  it('returns null for null/undefined match', () => {
    expect(engiFlagScore(null)).toBeNull();
    expect(engiFlagScore(undefined)).toBeNull();
  });

  it('returns null when the match carries no flag data at all (not an engi match)', () => {
    expect(engiFlagScore({ sideA: 'A', sideB: 'B', status: 'completed' })).toBeNull();
  });

  it('Aka (sideA/flagsA) wins 3-2 → "2–3" (Shiro/flagsB first)', () => {
    expect(engiFlagScore({ flagsA: 3, flagsB: 2 })).toBe('2–3');
  });

  it('Shiro (sideB/flagsB) wins 4-1 → "4–1" (Shiro/flagsB first)', () => {
    expect(engiFlagScore({ flagsA: 1, flagsB: 4 })).toBe('4–1');
  });

  it('unanimous decision: 5-0 either way', () => {
    expect(engiFlagScore({ flagsA: 5, flagsB: 0 })).toBe('0–5');
    expect(engiFlagScore({ flagsA: 0, flagsB: 5 })).toBe('5–0');
  });

  it('treats a present-but-zero flagsA as engi data, not "no flags"', () => {
    // flagsB unset (undefined), flagsA explicitly 0: still an engi match.
    expect(engiFlagScore({ flagsA: 0 })).toBe('0–0');
  });
});

// matchScoreStr dispatch order: engiFlagScore (numeric) takes priority over
// teamIVScore and formatIpponsScore (both letter-based) whenever the match
// carries flag data, since engi is never a team match and formatIpponsScore
// would otherwise see empty ipponsA/ipponsB and print nothing meaningful.

describe('matchScoreStr; engi takes priority and everything else stays letters', () => {
  it('an engi match returns the numeric flag score, not ippon letters', () => {
    const m = { flagsA: 3, flagsB: 2, ipponsA: [], ipponsB: [] };
    expect(matchScoreStr(m, [], [])).toBe('2–3');
  });

  it('a non-engi individual match still returns ippon letters, never digits', () => {
    const m = { status: 'completed' };
    const s = matchScoreStr(m, ['M', 'K'], ['D']);
    expect(s).toBe('MK–D');
    expect(s).not.toMatch(/^\d/);
  });

  it('a non-engi team match returns the IV/PW aggregate, not flag digits', () => {
    // Server-authoritative teamResult drives the team-match summary.
    const m = {
      sideA: 'TeamA', sideB: 'TeamB',
      subResults: [{ position: 0, winner: 'TeamB', sideA: 'P1', sideB: 'P2' }],
      teamResult: { shiroIV: 1, akaIV: 0, shiroPW: 2, akaPW: 1 },
    };
    const s = matchScoreStr(m, [], []);
    expect(s).toBe('IV 1–0 · PW 2–1');
    expect(s).not.toMatch(/^\d/);
  });
});
