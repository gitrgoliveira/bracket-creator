import { describe, it, expect } from 'vitest';
import {
  matchDataUnreadable,
  unreadableMatches,
  dataIssueText,
  bracketIssue,
  bracketRecoveryKind,
  bracketResetPrompt,
  bracketResetToast,
  missingIDsIssue,
  isAdvisoryIssue,
  isLoudIssue,
  BRACKET_RECOVERY_REBUILD,
  BRACKET_RECOVERY_DISCARD,
  BRACKET_RECOVERY_NONE,
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

describe('missingIDsIssue: picking the ADVISORY entry out of dataIssues', () => {
  it('finds the one entry whose kind is missing-ids', () => {
    const corrupt = { file: 'bracket.json', line: 1, column: 1, detail: 'bad' };
    const missing = { kind: 'missing-ids', file: 'participants.csv', detail: 'Dave: no id on file.' };
    expect(missingIDsIssue([corrupt, missing])).toBe(missing);
  });

  it('is null when there is no such entry, or no list at all', () => {
    expect(missingIDsIssue([{ file: 'bracket.json', line: 1, column: 1, detail: 'bad' }])).toBeNull();
    expect(missingIDsIssue([])).toBeNull();
    expect(missingIDsIssue(null)).toBeNull();
    expect(missingIDsIssue(undefined)).toBeNull();
  });
});

describe('isAdvisoryIssue / isLoudIssue: partitioning a dataIssues entry by kind', () => {
  it('an explicit kind:missing-ids entry is advisory, never loud', () => {
    const missing = { kind: 'missing-ids', file: 'participants.csv', detail: 'Dave: no id on file.' };
    expect(isAdvisoryIssue(missing)).toBe(true);
    expect(isLoudIssue(missing)).toBe(false);
  });

  it('an explicit kind:corrupt-file entry is loud, never advisory', () => {
    const corrupt = { kind: 'corrupt-file', file: 'bracket.json', line: 1, column: 1, detail: 'bad' };
    expect(isAdvisoryIssue(corrupt)).toBe(false);
    expect(isLoudIssue(corrupt)).toBe(true);
  });

  it('an entry with no kind at all (an older server payload) reads as loud', () => {
    const noKind = { file: 'bracket.json', line: 1, column: 1, detail: 'bad' };
    expect(isAdvisoryIssue(noKind)).toBe(false);
    expect(isLoudIssue(noKind)).toBe(true);
  });

  it('is false for nothing', () => {
    expect(isAdvisoryIssue(null)).toBe(false);
    expect(isAdvisoryIssue(undefined)).toBe(false);
    expect(isLoudIssue(null)).toBe(false);
    expect(isLoudIssue(undefined)).toBe(false);
  });
});

describe('bracketRecoveryKind: what a lost bracket can be recovered with', () => {
  // The three outcomes are pinned format-by-format against the shared Go/JS
  // table in format_draws_bracket.test.jsx. These cover the shape of the
  // answer and the inputs that table cannot carry.
  it('rebuilds only where the draw survives in another file', () => {
    expect(bracketRecoveryKind({ format: 'mixed' })).toBe(BRACKET_RECOVERY_REBUILD);
  });

  it('discards a vestigial bracket for the formats that never draw one', () => {
    expect(bracketRecoveryKind({ format: 'league' })).toBe(BRACKET_RECOVERY_DISCARD);
    expect(bracketRecoveryKind({ format: 'swiss' })).toBe(BRACKET_RECOVERY_DISCARD);
  });

  it('offers nothing where the file IS the draw, including an unknown format', () => {
    expect(bracketRecoveryKind({ format: 'playoffs' })).toBe(BRACKET_RECOVERY_NONE);
    // A typo in a hand-edited config.md takes the draw pipeline's default
    // branch and gets a standalone playoffs bracket, so it must be refused for
    // the same reason playoffs is. The first version of this predicate offered
    // it a rebuild.
    expect(bracketRecoveryKind({ format: 'not-a-format' })).toBe(BRACKET_RECOVERY_NONE);
    expect(bracketRecoveryKind({})).toBe(BRACKET_RECOVERY_NONE);
    expect(bracketRecoveryKind(null)).toBe(BRACKET_RECOVERY_NONE);
  });
});

describe('the words that go with each recovery kind', () => {
  it('never asks a competition without a knockout stage to reset one', () => {
    const p = bracketResetPrompt(BRACKET_RECOVERY_DISCARD, 'Kanto League');
    expect(p.message).toContain('Kanto League');
    expect(p.message).toContain('no knockout stage');
    expect(p.message).not.toMatch(/reset the knockout/i);
    expect(p.confirmLabel).not.toMatch(/knockout/i);
  });

  it('names the knockout stage where there is one', () => {
    const p = bracketResetPrompt(BRACKET_RECOVERY_REBUILD, 'Open Cup');
    expect(p.message).toMatch(/knockout stage/i);
    expect(p.confirmLabel).toBe('Reset knockout stage');
  });

  // The toast reports what the SERVER did, not what the client predicted, so a
  // false `rebuilt` can never be dressed up as a rebuild.
  it('does not claim a rebuild the server did not do', () => {
    const said = bracketResetToast('bracket.json.corrupt-20260823-101500', false);
    expect(said).toContain('bracket.json.corrupt-20260823-101500');
    expect(said).toContain('Nothing was rebuilt');
    expect(said).not.toMatch(/knockout stage reset/i);
  });

  it('says so when the server did rebuild', () => {
    expect(bracketResetToast('x', true)).toMatch(/knockout stage reset/i);
  });

  it('finds the bracket among several issues', () => {
    const issues = [{ file: 'pools.csv' }, { file: 'bracket.json', line: 3 }];
    expect(bracketIssue(issues).line).toBe(3);
    expect(bracketIssue([{ file: 'pools.csv' }])).toBeNull();
    expect(bracketIssue(null)).toBeNull();
  });
});
