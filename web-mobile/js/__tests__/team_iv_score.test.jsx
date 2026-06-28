import { describe, it, expect } from 'vitest';
import { teamIVScore } from '../bracket.jsx';

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
