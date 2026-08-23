import { describe, it, expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import {
  formatDrawsBracket,
  shiaijoCountErrorFor,
  shiaijoCountHintFor,
} from '../admin_helpers.jsx';
import {
  bracketRecoveryKind,
  BRACKET_RECOVERY_REBUILD,
  BRACKET_RECOVERY_DISCARD,
  BRACKET_RECOVERY_NONE,
} from '../data_integrity.jsx';

// JS half of the shared Go/JS golden table for "does this format draw a
// bracket?": see the `_comment` in format_draws_bracket.json for why the table
// is shared and what the pipeline's default branch means in it. Go half:
// TestCompetitionDrawsBracket_GoldenTable in
// internal/engine/format_draws_bracket_golden_test.go.
describe('formatDrawsBracket Go/JS mirror', () => {
  const table = JSON.parse(
    readFileSync(
      resolve(__dirname, '..', '..', '..', 'internal', 'engine', 'testdata', 'format_draws_bracket.json'),
      'utf8'
    )
  );

  // Load-bearing: it.each over an empty array silently produces zero tests
  // (no red), so a degraded table needs its own failure.
  it('the shared golden table is present and non-empty', () => {
    expect(
      table.cases?.length,
      'internal/engine/testdata/format_draws_bracket.json parsed to zero cases: the mirror would assert nothing'
    ).toBeGreaterThan(0);
  });

  it.each(table.cases)('$why', ({ format, drawsBracket }) => {
    expect(formatDrawsBracket(format)).toBe(drawsBracket);
  });

  // SECOND QUESTION, same table: which formats can have a lost bracket.json
  // REBUILT rather than only moved aside. Go half:
  // TestCompetitionRebuildableFromPools_GoldenTable and
  // TestQuarantineCorruptBracket_OutcomeFollowsTheFormatTable.
  //
  // This exists because the console's first version of this predicate was a
  // hand-written `mixed || league || swiss`, which offered a rebuild to every
  // UNRECOGNISED format -- the half that a hand-written list gets wrong, and
  // the exact failure this table was built to stop.
  describe('bracketRecoveryKind reads the same table', () => {
    it.each(table.cases)('$why (recovery kind)', ({ format, drawsBracket, rebuildableFromPools }) => {
      const expected = rebuildableFromPools
        ? BRACKET_RECOVERY_REBUILD
        : (drawsBracket ? BRACKET_RECOVERY_NONE : BRACKET_RECOVERY_DISCARD);
      expect(bracketRecoveryKind({ format })).toBe(expected);
    });

    // The table must not describe a state the draw pipeline cannot reach: a
    // format whose draw builds no bracket has none to rebuild.
    it.each(table.cases)('$why (rebuildable implies drawsBracket)', ({ drawsBracket, rebuildableFromPools }) => {
      if (rebuildableFromPools) expect(drawsBracket).toBe(true);
    });
  });

  // The scope is only worth pinning because of what it gates. These are the two
  // surfaces an operator meets it through, and both take the format rather than
  // re-deriving the gate, so the table covers them as well.
  describe('the scope reaches the surfaces that apply it', () => {
    // 3 is the smallest illegal count and the one the app's own league hint
    // recommends (floor(players/2)-1), so it is exactly where a format-blind
    // rule shows up as a refusal the server would never make.
    it.each(table.cases)('$why (count rule)', ({ format, drawsBracket }) => {
      const err = shiaijoCountErrorFor(format, 3);
      if (drawsBracket) {
        expect(err).toContain('3 shiaijo cannot be paired down to a single bracket');
      } else {
        expect(err).toBeNull();
      }
    });

    it.each(table.cases)('$why (standing hint)', ({ format, drawsBracket }) => {
      const hint = shiaijoCountHintFor(format, 3);
      if (drawsBracket) {
        expect(hint).toContain('This competition can use');
      } else {
        expect(hint).toBeNull();
      }
    });
  });
});
