import { readFileSync } from 'fs';
import { resolve } from 'path';
import { describe, it, expect } from 'vitest';
import {
  standardSeedOrder, nextPow2, buildBracket, advanceByes,
  buildPools, poolLetterName, computeStandings, parseParticipantLines
} from '../data.jsx';
import { checkinPid } from '../data.jsx';

describe('Data Utils', () => {
  describe('standardSeedOrder', () => {
    it('should generate correct seed order for 4', () => {
      expect(standardSeedOrder(4)).toEqual([1, 4, 2, 3]);
    });
    it('should generate correct seed order for 8', () => {
      expect(standardSeedOrder(8)).toEqual([1, 8, 4, 5, 2, 7, 3, 6]);
    });
  });

  describe('nextPow2', () => {
    it('should return next power of 2', () => {
      expect(nextPow2(3)).toBe(4);
      expect(nextPow2(4)).toBe(4);
      expect(nextPow2(5)).toBe(8);
    });
  });

  describe('buildBracket', () => {
    it('should create a bracket with correct number of rounds', () => {
      const players = [{ name: 'P1' }, { name: 'P2' }, { name: 'P3' }];
      const bracket = buildBracket(players, ['A']);
      // 3 players -> 4 slots -> 2 rounds
      expect(bracket.length).toBe(2);
      expect(bracket[0].length).toBe(2); // 2 matches in R1
      expect(bracket[1].length).toBe(1); // 1 match in R2
    });
  });

  describe('advanceByes', () => {
    it('should advance players with byes', () => {
      // players[0]=P1, players[1]=P2, players[2]=P3, players[3]=null
      // slots for size 4: [1, 4, 2, 3] -> [P1, null, P2, P3]
      const players = [{ name: 'P1' }, { name: 'P2' }, { name: 'P3' }, null];
      const bracket = buildBracket(players, ['A']);
      advanceByes(bracket);
      
      expect(bracket[0][0].winner).toEqual({ name: 'P1' });
      expect(bracket[0][0].status).toBe('completed');
      expect(bracket[1][0].sideA).toEqual({ name: 'P1' });
    });
  });

  describe('buildPools', () => {
    it('should distribute players into pools using snake distribution', () => {
      const players = [
        { name: 'P1', seed: 1 }, { name: 'P2', seed: 2 }, 
        { name: 'P3', seed: 3 }, { name: 'P4', seed: 4 }
      ];
      const pools = buildPools(players, { poolSize: 2, numPools: 2 });
      // Pool A: P1, P4 (Seed 1, 4)
      // Pool B: P2, P3 (Seed 2, 3)
      expect(pools[0].players[0].name).toBe('P1');
      expect(pools[0].players[1].name).toBe('P4');
      expect(pools[1].players[0].name).toBe('P2');
      expect(pools[1].players[1].name).toBe('P3');
    });
  });

  describe('poolLetterName', () => {
    // Mirrors internal/helper/pool_position_name_test.go's
    // TestPoolPositionName_UniqueBeyond52 (bc-drwx item 9): the same six
    // 0-based positions, pinned on both sides of the Go/JS mirror so a
    // regression at either rollover (single letter to double, double to
    // triple) is caught wherever it happens. poolLetterName replaces a bare
    // String.fromCharCode(65+i), which never wrapped at all -- position 26
    // printed "Pool [" (one past "Z" in ASCII) instead of "Pool AA".
    it('is bijective base-26 and never repeats past the raw ASCII wrap point', () => {
      expect(poolLetterName(25)).toBe('Z');
      expect(poolLetterName(26)).toBe('AA');
      expect(poolLetterName(51)).toBe('AZ');
      expect(poolLetterName(52)).toBe('BA');
      expect(poolLetterName(701)).toBe('ZZ');
      expect(poolLetterName(702)).toBe('AAA');
    });

    it('never repeats a name across a large run (the raw scheme this replaces collided every 26)', () => {
      const seen = new Set();
      for (let i = 0; i < 64; i++) {
        const name = poolLetterName(i);
        expect(seen.has(name)).toBe(false);
        seen.add(name);
      }
    });
  });

  // bc-drwx item 9 / PR #416 finding 15: JS half of the shared Go/JS golden
  // table for the pool-name sequence -- see the `_comment` in
  // pool_letter_names.json for why the table is shared. Go half:
  // TestPoolPositionName_MatchesFixture in
  // internal/helper/pool_position_name_test.go.
  describe('poolLetterName Go/JS mirror (pool_letter_names.json)', () => {
    const table = JSON.parse(
      readFileSync(
        resolve(__dirname, '..', '..', '..', 'internal', 'helper', 'testdata', 'pool_letter_names.json'),
        'utf8'
      )
    );

    // Load-bearing: vitest's it.each over an empty array silently produces
    // zero tests (no red), so a degraded table needs its own failure.
    it('the shared golden table is present and non-empty', () => {
      expect(
        table.cases?.length,
        'internal/helper/testdata/pool_letter_names.json parsed to zero cases: the mirror would assert nothing'
      ).toBeGreaterThan(0);
    });

    // The fixture's `name` is the full "Pool <letters>" string (the Go
    // owner's own return value); poolLetterName returns the bare letters,
    // the "Pool " prefix is composed separately by buildPools' own template
    // literal, so the JS assertion strips it before comparing.
    it.each(table.cases)('index $index renders "$name"', ({ index, name }) => {
      expect(
        poolLetterName(index),
        'JS poolLetterName disagrees with the shared table; update BOTH the Go and JS sequences together'
      ).toBe(name.replace(/^Pool /, ''));
    });
  });

  describe('computeStandings', () => {
    it('should correctly calculate wins and sort standings', () => {
      const p1 = { id: 'p1', name: 'P1' };
      const p2 = { id: 'p2', name: 'P2' };
      const pool = {
        players: [p1, p2],
        matches: [
          { sideA: p1, sideB: p2, winner: p1, status: 'completed', score: { winnerPts: 2, loserPts: 1 } }
        ]
      };
      const standings = computeStandings(pool);
      expect(standings[0].player.name).toBe('P1');
      expect(standings[0].wins).toBe(1);
      expect(standings[0].ippons).toBe(2);
      expect(standings[1].player.name).toBe('P2');
      expect(standings[1].losses).toBe(1);
      expect(standings[1].ippons).toBe(1);
    });
  });
});

describe('parseParticipantLines', () => {
  it('sets checkedIn=true when last column is "checked_in" and parts > 2', () => {
    const [p] = parseParticipantLines(['Name, Dojo, checked_in'], false);
    expect(p.checkedIn).toBe(true);
    expect(p.name).toBe('Name');
    expect(p.dojo).toBe('Dojo');
  });

  it('does NOT set checkedIn when only 2 parts (below threshold)', () => {
    // "Name, checked_in" has only 2 parts; must not be treated as the flag.
    const [p] = parseParticipantLines(['Name, checked_in'], false);
    expect(p.checkedIn).toBe(false);
    // The second column is read as dojo.
    expect(p.dojo).toBe('checked_in');
  });

  it('detects both source and checkedIn when "..., source, checked_in"', () => {
    const [p] = parseParticipantLines(['Name, Dojo, registered, checked_in'], false);
    expect(p.checkedIn).toBe(true);
    expect(p.source).toBe('registered');
    expect(p.name).toBe('Name');
    expect(p.dojo).toBe('Dojo');
  });

  it('aliases the legacy "reserved" source to "manual" (mirrors Go)', () => {
    const [p] = parseParticipantLines(['Jane Doe, Osaka, reserved'], false);
    expect(p.source).toBe('manual');
    expect(p.dojo).toBe('Osaka');
    expect(p.danGrade).toBe(''); // "reserved" consumed as source, not left as danGrade
  });

  it('leaves an unknown trailing token as danGrade, not source', () => {
    const [p] = parseParticipantLines(['Bob Lee, Kyoto, vip'], false);
    expect(p.source).toBe('');
    expect(p.danGrade).toBe('vip');
  });
});

// checkinPid is the ONE owner of the id-else-"name|dojo" identity rule
// client-side (mirroring helper.CompetitorKey server-side). Its own first
// cut used `p.id ?? fallback`, which only substitutes on null/undefined, so
// an empty-string id -- what the chusen-candidates handler always emits for
// a competitor with no UUID (handlers_competition.go:
// `gin.H{"id": t.Player.ID, ...}`) -- fell through as "" instead of the
// name|dojo fallback, collapsing every legacy member in a group onto the
// SAME key. Fixed to a truthy check ("" is never a valid participant id).
describe('checkinPid', () => {
  it('keys by id when present', () => {
    expect(checkinPid({ id: 'uuid-1', name: 'A', dojo: 'D' })).toBe('uuid-1');
  });

  it('falls back to "name|dojo" when id is absent (legacy roster)', () => {
    expect(checkinPid({ name: 'A', dojo: 'D' })).toBe('A|D');
  });

  it('falls back to "name|dojo" when id is an EMPTY STRING, not just null/undefined', () => {
    // "" is not nullish, so a `??` fallback would return "" here instead of
    // falling through -- exactly the bug this pins.
    expect(checkinPid({ id: '', name: 'A', dojo: 'D' })).toBe('A|D');
  });

  it('tells two empty-id members with different names/dojos apart', () => {
    const alpha = { id: '', name: 'Alpha', dojo: 'Dojo A' };
    const beta = { id: '', name: 'Beta', dojo: 'Dojo B' };
    expect(checkinPid(alpha)).not.toBe(checkinPid(beta));
  });

  it('degrades a missing dojo to an empty string in the composite key', () => {
    expect(checkinPid({ name: 'A' })).toBe('A|');
  });
});
