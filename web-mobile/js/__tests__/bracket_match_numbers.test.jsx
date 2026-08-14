import { describe, it, expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { buildDisplayModel } from '../bracket.jsx';

// JS half of the shared Go/JS golden table for bracket match numbers. Go half:
// TestBracketMatchNumbersGolden in internal/engine/bracket_match_numbers_golden_test.go,
// whose header explains what the contract is and why it is fragile.
//
// The short version: the engine stamps MatchNumber on every real match, and
// buildDisplayModel re-derives the same numbering here to label cards "M<n>".
// A referee reads the printed Excel sheet and the operator's screen side by
// side, so "M12" and "Match 12" have to be the same bout. The two walks are
// separate implementations in separate languages and they have drifted before.
describe('bracket match numbers: Go/JS mirror', () => {
  const table = JSON.parse(
    readFileSync(
      resolve(__dirname, '..', '..', '..', 'internal', 'engine', 'testdata', 'bracket_match_numbers.json'),
      'utf8'
    )
  );

  // Load-bearing: it.each over an empty array silently produces zero tests
  // (no red), so a degraded table needs its own failure.
  it('the shared golden table is present and non-empty', () => {
    expect(
      table.cases?.length,
      'internal/engine/testdata/bracket_match_numbers.json parsed to zero cases: the mirror would assert nothing'
    ).toBeGreaterThan(0);
  });

  // Also load-bearing, and less obvious: on most bracket shapes, ordering by
  // leaf slot and ordering by raw position agree, so a table of only those
  // shapes passes even against the pre-fix numbering. At least one case has to
  // be able to tell them apart. See the Go half for how they were found.
  it('the table contains a case that can catch the ordering drift', () => {
    expect(
      table.cases.filter((c) => c.discriminating).length,
      'no discriminating case left in the golden: this mirror would pass against the very bug it exists to catch'
    ).toBeGreaterThan(0);
  });

  // The engine's answer for one case: matchId -> number, for every match it
  // numbered.
  const engineNumbers = (rounds) => {
    const out = {};
    rounds.forEach((round) => round.forEach((m) => {
      if (m.matchNumber > 0) out[m.id] = m.matchNumber;
    }));
    return out;
  };

  it.each(table.cases)('$entrants entrants ($name)', ({ rounds }) => {
    const expected = engineNumbers(rounds);
    const { matchNumById } = buildDisplayModel(rounds);

    // The SET has to agree before the values can mean anything. This is the
    // assertion that catches the two REAL-match filters diverging: Go keeps a
    // match when it is not hidden and not empty-vs-empty, this file keeps it
    // when it is not hidden and displayRound > 0. One extra or missing match
    // shifts every number after it on one side only.
    expect(Object.keys(matchNumById).sort()).toEqual(Object.keys(expected).sort());
    expect(matchNumById).toEqual(expected);

    // Dense 1..N, which the two checks above cannot see: an equal-but-gapped
    // pair of maps satisfies both while meaning each walk skipped the same bout.
    const numbers = Object.values(matchNumById).sort((a, b) => a - b);
    expect(numbers).toEqual(numbers.map((_, i) => i + 1));
  });
});
