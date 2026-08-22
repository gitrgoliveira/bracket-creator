import { describe, it, expect } from 'vitest';
import {
  matchDataUnreadable,
  unreadableMatches,
  dataIssueText,
  bracketIssue,
  canRebuildBracket,
} from '../data_integrity.jsx';

// data_integrity.jsx is the ONE owner of what the operator is told when a file
// does not say what the app expects. These pin the two rules that cannot be
// re-derived at a call site without drifting: which state counts as a problem,
// and which audience is allowed to see it.

describe('matchDataUnreadable: the one test for a lost encounter', () => {
  it('is true only when the server says the cell would not parse', () => {
    expect(matchDataUnreadable({ subResultsUnreadable: true })).toBe(true);
    expect(matchDataUnreadable({ subResultsUnreadable: false })).toBe(false);
    // An ordinary team match with no sub-bouts recorded yet is NOT a problem.
    expect(matchDataUnreadable({ subResults: [] })).toBe(false);
    expect(matchDataUnreadable(null)).toBe(false);
    expect(matchDataUnreadable(undefined)).toBe(false);
  });

  it('counts only the affected matches in a list', () => {
    const matches = [
      { id: 'a' },
      { id: 'b', subResultsUnreadable: true },
      { id: 'c', subResultsUnreadable: true },
    ];
    expect(unreadableMatches(matches).map(m => m.id)).toEqual(['b', 'c']);
    expect(unreadableMatches(null)).toEqual([]);
  });
});

describe('dataIssueText: a line an operator can act on', () => {
  it('names the file, the position and the parser reason', () => {
    expect(dataIssueText({ file: 'bracket.json', line: 47, column: 12, detail: "invalid character 'x'" }))
      .toBe("bracket.json, line 47, column 12: invalid character 'x'");
  });

  it('omits the position when the parser could not place the fault', () => {
    // Rather than printing "line 0", which reads as a real location.
    expect(dataIssueText({ file: 'pools.csv', line: 0, column: 0, detail: 'unexpected end of file' }))
      .toBe('pools.csv: unexpected end of file');
  });

  it('is empty for nothing', () => {
    expect(dataIssueText(null)).toBe('');
    expect(dataIssueText({})).toBe('');
  });
});

describe('canRebuildBracket: where a rebuild would invent a draw', () => {
  // A pool-fed competition keeps its draw in pools.csv. A direct-elimination
  // competition keeps it ONLY in bracket.json, so rebuilding from today's
  // roster produces different pairings rather than restoring these.
  it('allows the formats whose draw survives elsewhere', () => {
    expect(canRebuildBracket({ format: 'mixed' })).toBe(true);
    expect(canRebuildBracket({ format: 'league' })).toBe(true);
    expect(canRebuildBracket({ format: 'swiss' })).toBe(true);
  });

  it('refuses direct elimination, where the file IS the draw', () => {
    expect(canRebuildBracket({ format: 'playoffs' })).toBe(false);
    expect(canRebuildBracket({})).toBe(false);
    expect(canRebuildBracket(null)).toBe(false);
  });

  it('finds the bracket among several issues', () => {
    const issues = [{ file: 'pools.csv' }, { file: 'bracket.json', line: 3 }];
    expect(bracketIssue(issues).line).toBe(3);
    expect(bracketIssue([{ file: 'pools.csv' }])).toBeNull();
    expect(bracketIssue(null)).toBeNull();
  });
});
