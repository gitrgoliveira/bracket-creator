import { describe, it, expect } from 'vitest';
import { teamIVScore, teamIVPWScore } from '../bracket.jsx';

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
});

// teamIVPWScore: full team-match result "IV shiroIV–akaIV · PW shiroPW–akaPW".
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
    expect(teamIVPWScore(m)).toBe('IV 2–1 · PW 4–2');
  });

  it('all draws from server teamResult → "IV 0–0 · PW 0–0"', () => {
    const m = {
      sideA: 'TeamA',
      sideB: 'TeamB',
      teamResult: { shiroIV: 0, akaIV: 0, shiroPW: 0, akaPW: 0 },
    };
    expect(teamIVPWScore(m)).toBe('IV 0–0 · PW 0–0');
  });

  it('does NOT re-derive PW from ippons: trusts teamResult over subResults', () => {
    const m = {
      sideA: 'TeamA',
      sideB: 'TeamB',
      // subResults would count PW 2–1, but the server field is authoritative.
      subResults: [{ position: 0, winner: 'TeamB', ipponsA: ['M'], ipponsB: ['M', 'K'] }],
      teamResult: { shiroIV: 1, akaIV: 0, shiroPW: 9, akaPW: 9 },
    };
    expect(teamIVPWScore(m)).toBe('IV 1–0 · PW 9–9');
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
