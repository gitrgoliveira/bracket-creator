import { describe, it, expect } from 'vitest';
import { teamIVScore, teamIVPWScore, teamMatchMarks } from '../bracket.jsx';

// teamIVScore derives the individual-victories (IV) aggregate for a team pool match
// from persisted subResults. Mirrors Go engine.ComputeTeamSummary.
// Orientation: sideB = Shiro (left), sideA = Aka (right) → returns "${ivB}–${ivA}".

describe('teamIVScore', () => {
  it('returns null for individual match with no subResults', () => {
    const m = { sideA: 'TeamA', sideB: 'TeamB', status: 'completed' };
    expect(teamIVScore(m)).toBeNull();
  });

  it('returns null when subResults is an empty array', () => {
    const m = { sideA: 'TeamA', sideB: 'TeamB', subResults: [] };
    expect(teamIVScore(m)).toBeNull();
  });

  it('returns null for null/undefined match', () => {
    expect(teamIVScore(null)).toBeNull();
    expect(teamIVScore(undefined)).toBeNull();
  });

  it('B wins 2 of 3, A wins 1 → "2–1" (Shiro first)', () => {
    const m = {
      sideA: 'TeamA',
      sideB: 'TeamB',
      subResults: [
        { position: 0, winner: 'TeamB', sideA: 'P1', sideB: 'P2' },
        { position: 1, winner: 'TeamA', sideA: 'P3', sideB: 'P4' },
        { position: 2, winner: 'TeamB', sideA: 'P5', sideB: 'P6' },
      ],
    };
    expect(teamIVScore(m)).toBe('2–1');
  });

  it('hikiwake sub (empty winner) contributes to neither side', () => {
    const m = {
      sideA: 'TeamA',
      sideB: 'TeamB',
      subResults: [
        { position: 0, winner: 'TeamB', sideA: 'P1', sideB: 'P2' },
        { position: 1, winner: '',      sideA: 'P3', sideB: 'P4' }, // hikiwake
        { position: 2, winner: 'TeamA', sideA: 'P5', sideB: 'P6' },
      ],
    };
    // B=1, A=1 (hikiwake not counted)
    expect(teamIVScore(m)).toBe('1–1');
  });

  it('winner matched via sub.sideA fallback (winner !== match-level team name)', () => {
    // winner carries the individual player name rather than the match-level team name
    const m = {
      sideA: 'TeamA',
      sideB: 'TeamB',
      subResults: [
        { position: 0, winner: 'PlayerX', sideA: 'PlayerX', sideB: 'PlayerY' }, // sideA fallback
        { position: 1, winner: 'PlayerZ', sideA: 'PlayerW', sideB: 'PlayerZ' }, // sideB fallback
      ],
    };
    // PlayerX matches sub.sideA → ivA++; PlayerZ matches sub.sideB → ivB++
    expect(teamIVScore(m)).toBe('1–1');
  });

  it('daihyosen sentinel (position < 0) is excluded from the count', () => {
    const m = {
      sideA: 'TeamA',
      sideB: 'TeamB',
      subResults: [
        { position:  0, winner: 'TeamB', sideA: 'P1', sideB: 'P2' },
        { position: -1, winner: 'TeamA', sideA: 'P3', sideB: 'P4' }, // daihyosen sentinel
      ],
    };
    // Only the non-sentinel: B wins 1, daihyosen excluded
    expect(teamIVScore(m)).toBe('1–0');
  });

  it('orientation: Shiro(B) is the LEFT number: "ivB–ivA"', () => {
    const m = {
      sideA: { name: 'AkaTeam' },
      sideB: { name: 'ShiroTeam' },
      subResults: [
        { position: 0, winner: 'ShiroTeam', sideA: 'P1', sideB: 'P2' },
        { position: 1, winner: 'ShiroTeam', sideA: 'P3', sideB: 'P4' },
        { position: 2, winner: 'AkaTeam',   sideA: 'P5', sideB: 'P6' },
      ],
    };
    // ShiroTeam=sideB=Shiro wins 2, AkaTeam=sideA=Aka wins 1 → "2–1"
    expect(teamIVScore(m)).toBe('2–1');
  });

  it('works when sideA/sideB are objects with a name property', () => {
    const m = {
      sideA: { name: 'TeamA', id: 'team-a' },
      sideB: { name: 'TeamB', id: 'team-b' },
      subResults: [
        { position: 0, winner: 'TeamA', sideA: 'P1', sideB: 'P2' },
      ],
    };
    expect(teamIVScore(m)).toBe('0–1');
  });

  it('all draws → "0–0"', () => {
    const m = {
      sideA: 'TeamA',
      sideB: 'TeamB',
      subResults: [
        { position: 0, winner: '', sideA: 'P1', sideB: 'P2' },
        { position: 1, winner: '', sideA: 'P3', sideB: 'P4' },
      ],
    };
    expect(teamIVScore(m)).toBe('0–0');
  });

  it('malformed sub entries (null) are skipped without error', () => {
    const m = {
      sideA: 'TeamA',
      sideB: 'TeamB',
      subResults: [
        null,
        { position: 0, winner: 'TeamB', sideA: 'P1', sideB: 'P2' },
      ],
    };
    expect(teamIVScore(m)).toBe('1–0');
  });

  // bc-tmfn: a team match a match-level default-win decision closed credits
  // every numbered bout with no result of its own to the OTHER side from
  // decisionBy. This is the fallback path (no server teamResult); the
  // authoritative count lives in m.teamResult once the server sends it.
  describe('bc-tmfn default-win credit', () => {
    it('credits every unfought numbered bout to the side opposite decisionBy', () => {
      const m = {
        sideA: 'TeamA', sideB: 'TeamB',
        status: 'completed', decision: 'kiken-voluntary', decisionBy: 'shiro',
        subResults: [
          { position: 1, winner: 'TeamB', sideA: 'P1', sideB: 'P2' }, // fought, keeps its own winner
          { position: 2 }, // unfought → credited to Aka (sideA), decisionBy=shiro withdrew
          { position: 3 }, // unfought → credited to Aka too
        ],
      };
      // Fought: B=1. Credited: A+=2 (shiro withdrew, aka/sideA wins by default).
      expect(teamIVScore(m)).toBe('1–2');
    });

    it('does not credit a bout that already carries a result', () => {
      const m = {
        sideA: 'TeamA', sideB: 'TeamB',
        status: 'completed', decision: 'fusenpai', decisionBy: 'aka',
        subResults: [
          { position: 1, winner: 'TeamA', sideA: 'P1', sideB: 'P2' }, // already decided, not credited
        ],
      };
      expect(teamIVScore(m)).toBe('0–1');
    });

    it('does not credit while the match is still running', () => {
      const m = {
        sideA: 'TeamA', sideB: 'TeamB',
        status: 'running', decision: 'kiken-voluntary', decisionBy: 'shiro',
        subResults: [{ position: 1 }],
      };
      expect(teamIVScore(m)).toBe('0–0');
    });

    it('does not credit a kachinuki match', () => {
      const m = {
        sideA: 'TeamA', sideB: 'TeamB', teamMatchType: 'kachinuki',
        status: 'completed', decision: 'kiken-voluntary', decisionBy: 'shiro',
        subResults: [{ position: 1 }],
      };
      expect(teamIVScore(m)).toBe('0–0');
    });

    it('does not credit a decision outside the default-win class', () => {
      const m = {
        sideA: 'TeamA', sideB: 'TeamB',
        status: 'completed', decision: 'fought', decisionBy: 'shiro',
        subResults: [{ position: 1 }],
      };
      expect(teamIVScore(m)).toBe('0–0');
    });
  });
});

// teamIVPWScore: full team-match result "IV shiroIV–akaIV" over "PW shiroPW–akaPW" (newline-separated).
// IV and PW come from the AUTHORITATIVE server field m.teamResult
// {shiroIV, akaIV, shiroPW, akaPW} (Go MatchResult.MarshalJSON via
// state.TeamResultFrom); the client does NOT re-derive PW. Legacy payloads that
// predate teamResult fall back to the client IV aggregate (IV only). Returns
// null for non-team matches (no teamResult and no subResults).
describe('teamIVPWScore', () => {
  it('returns null for a non-team match (no teamResult, no subResults)', () => {
    const m = { sideA: 'TeamA', sideB: 'TeamB', status: 'completed' };
    expect(teamIVPWScore(m)).toBeNull();
  });

  it('returns null for null/undefined match', () => {
    expect(teamIVPWScore(null)).toBeNull();
    expect(teamIVPWScore(undefined)).toBeNull();
  });

  it('renders the server teamResult verbatim: shiro–aka for IV and PW', () => {
    const m = {
      sideA: 'TeamA',
      sideB: 'TeamB',
      subResults: [{ position: 0, winner: 'TeamB' }], // present but ignored
      teamResult: { shiroIV: 2, akaIV: 1, shiroPW: 4, akaPW: 2 },
    };
    expect(teamIVPWScore(m)).toBe('IV 2–1\nPW 4–2');
  });

  it('all draws from server teamResult → "IV 0–0 / PW 0–0"', () => {
    const m = {
      sideA: 'TeamA',
      sideB: 'TeamB',
      teamResult: { shiroIV: 0, akaIV: 0, shiroPW: 0, akaPW: 0 },
    };
    expect(teamIVPWScore(m)).toBe('IV 0–0\nPW 0–0');
  });

  it('does NOT re-derive PW from ippons: trusts teamResult over subResults', () => {
    const m = {
      sideA: 'TeamA',
      sideB: 'TeamB',
      // subResults would count PW 2–1, but the server field is authoritative.
      subResults: [{ position: 0, winner: 'TeamB', ipponsA: ['M'], ipponsB: ['M', 'K'] }],
      teamResult: { shiroIV: 1, akaIV: 0, shiroPW: 9, akaPW: 9 },
    };
    expect(teamIVPWScore(m)).toBe('IV 1–0\nPW 9–9');
  });

  it('legacy fallback: no teamResult → IV only from subResults (no PW)', () => {
    const m = {
      sideA: 'TeamA',
      sideB: 'TeamB',
      subResults: [
        { position: 0, winner: 'TeamB', sideA: 'P1', sideB: 'P2' },
        { position: 1, winner: 'TeamA', sideA: 'P3', sideB: 'P4' },
        { position: 2, winner: 'TeamB', sideA: 'P5', sideB: 'P6' },
      ],
    };
    // teamIVScore → "ivB–ivA" = "2–1"; no server PW available.
    expect(teamIVPWScore(m)).toBe('IV 2–1');
  });
});

// teamMatchMarks: bc-tmfn. The one shared "which side gets which
// match-level Kiken/Fus. mark" composition (sameCompetitor + sideMarks +
// placeMarks), reused by every list/headline row that shows a TEAM's name
// separately from its (mark-free) score cell.
describe('teamMatchMarks', () => {
  const toraA = { name: 'Tora A', dojo: 'Nara', id: 'a-id' };
  const toraB = { name: 'Tora B', dojo: 'Tokyo', id: 'b-id' };

  it('kiken: the mark names the WITHDRAWER (loser), on their side', () => {
    // Shiro (Tora B) withdrew; Aka (Tora A) is credited and wins.
    const m = { status: 'completed', decision: 'kiken-voluntary', sideA: toraA, sideB: toraB, winner: toraA };
    expect(teamMatchMarks(m, true)).toEqual({ shiro: 'Kiken', aka: '' });
  });

  it('fusenpai: the mark also names the no-show (loser), on their side', () => {
    const m = { status: 'completed', decision: 'fusenpai', sideA: toraA, sideB: toraB, winner: toraB };
    expect(teamMatchMarks(m, true)).toEqual({ shiro: '', aka: 'Fus.' });
  });

  it('fusensho: the mark names the WINNER (the present side), on their side', () => {
    const m = { status: 'completed', decision: 'fusensho', sideA: toraA, sideB: toraB, winner: toraA };
    expect(teamMatchMarks(m, true)).toEqual({ shiro: '', aka: 'Fus.' });
  });

  it('returns no marks for a non-team row (isTeamRow=false): an individual match already carries its own mark inline', () => {
    const m = { status: 'completed', decision: 'kiken-voluntary', sideA: toraA, sideB: toraB, winner: toraA };
    expect(teamMatchMarks(m, false)).toEqual({ shiro: '', aka: '' });
  });

  it('returns no marks before the match is completed', () => {
    const m = { status: 'running', decision: 'kiken-voluntary', sideA: toraA, sideB: toraB, winner: toraA };
    expect(teamMatchMarks(m, true)).toEqual({ shiro: '', aka: '' });
  });

  it('returns no marks for a decision outside the default-win class (e.g. fought)', () => {
    const m = { status: 'completed', decision: 'fought', sideA: toraA, sideB: toraB, winner: toraA };
    expect(teamMatchMarks(m, true)).toEqual({ shiro: '', aka: '' });
  });

  it('returns no marks for a null/undefined match', () => {
    expect(teamMatchMarks(null, true)).toEqual({ shiro: '', aka: '' });
    expect(teamMatchMarks(undefined, true)).toEqual({ shiro: '', aka: '' });
  });

  // bc-cse: isTeamRow is now OPTIONAL. Omitting it (the four list/row callers
  // no longer pre-compute their own copy) derives the same
  // Array.isArray(subResults) && subResults.length > 0 signal internally.
  describe('isTeamRow omitted: derives the team-row signal from subResults', () => {
    it('a team row (non-empty subResults) still gets its mark', () => {
      const m = { status: 'completed', decision: 'kiken-voluntary', sideA: toraA, sideB: toraB, winner: toraA, subResults: [{ position: 1 }] };
      expect(teamMatchMarks(m)).toEqual({ shiro: 'Kiken', aka: '' });
    });

    it('an individual row (no subResults) gets no marks, same as isTeamRow=false', () => {
      const m = { status: 'completed', decision: 'kiken-voluntary', sideA: toraA, sideB: toraB, winner: toraA };
      expect(teamMatchMarks(m)).toEqual({ shiro: '', aka: '' });
    });

    it('a null/undefined match still returns no marks', () => {
      expect(teamMatchMarks(null)).toEqual({ shiro: '', aka: '' });
      expect(teamMatchMarks(undefined)).toEqual({ shiro: '', aka: '' });
    });
  });
});
