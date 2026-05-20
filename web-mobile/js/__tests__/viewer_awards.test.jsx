// Pure-logic tests for deriveAwards — the helper that maps bracket/standings
// payloads into the 1/2/3/3 podium per FIK convention (no bronze match).
import { describe, it, expect } from 'vitest';
import { deriveAwards } from '../viewer.jsx';

const playerMap = (...pairs) => {
  const m = new Map();
  pairs.forEach(([name, dojo]) => m.set(name, { name, dojo }));
  return m;
};

describe('deriveAwards (bracket-based)', () => {
  it('returns 1st/2nd and two 3rd places from the final + semis', () => {
    const bracket = {
      rounds: [
        // round 0 — semis (irrelevant)
        [],
        // round 1 — semi-finals
        [
          { sideA: 'Alice', sideB: 'Bob', winner: 'Alice' },
          { sideA: 'Carol', sideB: 'Dan', winner: 'Carol' },
        ],
        // round 2 — final
        [{ sideA: 'Alice', sideB: 'Carol', winner: 'Alice' }],
      ],
    };
    const m = playerMap(['Alice', 'Aoyama'], ['Bob', 'Bunkyo'], ['Carol', 'Chiba'], ['Dan', 'Denenchofu']);
    const awards = deriveAwards(bracket, null, null, 'playoffs', m);
    expect(awards).toEqual([
      { place: 1, name: 'Alice', dojo: 'Aoyama' },
      { place: 2, name: 'Carol', dojo: 'Chiba' },
      { place: 3, name: 'Bob', dojo: 'Bunkyo' },
      { place: 3, name: 'Dan', dojo: 'Denenchofu' },
    ]);
  });

  it('returns [] when the final has no winner yet', () => {
    const bracket = {
      rounds: [
        [{ sideA: 'A', sideB: 'B', winner: 'A' }],
        [{ sideA: 'A', sideB: 'C', winner: '' }],
      ],
    };
    expect(deriveAwards(bracket, null, null, 'playoffs', new Map())).toEqual([]);
  });

  it('handles missing dojos by defaulting to empty string', () => {
    const bracket = {
      rounds: [
        [{ sideA: 'X', sideB: 'Y', winner: 'X' }],
      ],
    };
    const awards = deriveAwards(bracket, null, null, 'playoffs', new Map());
    expect(awards.length).toBe(2);
    expect(awards[0]).toEqual({ place: 1, name: 'X', dojo: '' });
    expect(awards[1]).toEqual({ place: 2, name: 'Y', dojo: '' });
  });

  it('omits a third place when only one semi-final exists (4-player bracket)', () => {
    const bracket = {
      rounds: [
        [{ sideA: 'A', sideB: 'B', winner: 'A' }],
        [{ sideA: 'A', sideB: 'C', winner: 'A' }],
      ],
    };
    const awards = deriveAwards(bracket, null, null, 'playoffs', new Map());
    expect(awards.map((a) => a.name)).toEqual(['A', 'C', 'B']);
  });
});

describe('deriveAwards (standings-based)', () => {
  it('falls back to the top 4 of the first pool when no bracket exists', () => {
    const pools = [{ poolName: 'Pool A' }];
    const standings = {
      'Pool A': [
        { player: { name: 'Alice', dojo: 'Aoyama' } },
        { player: { name: 'Bob', dojo: 'Bunkyo' } },
        { player: { name: 'Carol', dojo: 'Chiba' } },
        { player: { name: 'Dan', dojo: 'Denenchofu' } },
        { player: { name: 'Eve', dojo: 'Edogawa' } },
      ],
    };
    const awards = deriveAwards(null, standings, pools, 'league', new Map());
    expect(awards).toEqual([
      { place: 1, name: 'Alice', dojo: 'Aoyama' },
      { place: 2, name: 'Bob', dojo: 'Bunkyo' },
      { place: 3, name: 'Carol', dojo: 'Chiba' },
      { place: 3, name: 'Dan', dojo: 'Denenchofu' },
    ]);
  });

  it('returns [] when standings is empty', () => {
    const pools = [{ poolName: 'Pool A' }];
    expect(deriveAwards(null, { 'Pool A': [] }, pools, 'league', new Map())).toEqual([]);
  });

  it('returns [] when no bracket and no pools/standings', () => {
    expect(deriveAwards(null, null, null, 'playoffs', new Map())).toEqual([]);
  });

  it('accepts a flat Swiss-shape standings array (no pool key)', () => {
    const standings = [
      { player: { name: 'Alice', dojo: 'Aoyama' } },
      { player: { name: 'Bob', dojo: 'Bunkyo' } },
      { player: { name: 'Carol', dojo: 'Chiba' } },
      { player: { name: 'Dan', dojo: 'Denenchofu' } },
    ];
    const awards = deriveAwards(null, standings, null, 'swiss', new Map());
    expect(awards).toEqual([
      { place: 1, name: 'Alice', dojo: 'Aoyama' },
      { place: 2, name: 'Bob', dojo: 'Bunkyo' },
      { place: 3, name: 'Carol', dojo: 'Chiba' },
      { place: 3, name: 'Dan', dojo: 'Denenchofu' },
    ]);
  });

  it('falls back to standings when a bracket exists but the final has no winner (pools+playoffs placeholder)', () => {
    // Simulates a pools-only competition where derivedBracket is a TBD
    // placeholder (rounds present, no winners). The standings fallback
    // should still produce the podium.
    const bracket = {
      rounds: [
        [
          { sideA: null, sideB: null, winner: null },
          { sideA: null, sideB: null, winner: null },
        ],
        [{ sideA: null, sideB: null, winner: null }],
      ],
    };
    const pools = [{ poolName: 'Pool A' }];
    const standings = {
      'Pool A': [
        { player: { name: 'Alice', dojo: 'Aoyama' } },
        { player: { name: 'Bob', dojo: 'Bunkyo' } },
        { player: { name: 'Carol', dojo: 'Chiba' } },
        { player: { name: 'Dan', dojo: 'Denenchofu' } },
      ],
    };
    const awards = deriveAwards(bracket, standings, pools, 'pools', new Map());
    expect(awards.map((a) => a.name)).toEqual(['Alice', 'Bob', 'Carol', 'Dan']);
    expect(awards.map((a) => a.place)).toEqual([1, 2, 3, 3]);
  });
});
