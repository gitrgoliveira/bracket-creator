import { describe, it, expect } from 'vitest';
import { shiaijoCountError, formatDrawsBracket } from '../admin_helpers.jsx';

// Mirrors internal/helper/court_parity_test.go. A competition whose draw builds
// a knockout bracket runs on 1 shiaijo or an even number: the draw pairs court
// regions, so an odd count above 1 leaves one region without a partner.
describe('shiaijoCountError', () => {
  const cases = [
    { n: 1, valid: true },  // a single-shiaijo competition is explicitly allowed
    { n: 2, valid: true },
    { n: 3, valid: false },
    { n: 4, valid: true },
    { n: 5, valid: false },
    { n: 6, valid: true },
    { n: 7, valid: false },
    { n: 8, valid: true },
  ];

  cases.forEach(({ n, valid }) => {
    it(`${valid ? 'accepts' : 'rejects'} ${n} shiaijo`, () => {
      const err = shiaijoCountError(n);
      if (valid) {
        expect(err).toBeNull();
      } else {
        expect(err).toContain(`${n} shiaijo cannot be paired`);
        expect(err).toContain(`Use ${n - 1} or ${n + 1}, or 1`);
        expect(err).toContain('partner');
      }
    });
  });

  it('never reads as "at least 2 courts"', () => {
    // A 1-shiaijo competition is legal, so the message must offer 1 rather
    // than stating a minimum. Same pin as the Go side.
    const err = shiaijoCountError(5).toLowerCase();
    expect(err).toContain(', or 1');
    expect(err).not.toContain('at least 2');
    expect(err).not.toContain('at least two');
  });

  it('stays silent for non-counts and empty allocations', () => {
    // 0 means "inherit the tournament's courts" and is resolved server-side;
    // NaN comes from a not-yet-loaded competition object.
    expect(shiaijoCountError(0)).toBeNull();
    expect(shiaijoCountError(NaN)).toBeNull();
    expect(shiaijoCountError(undefined)).toBeNull();
  });
});

describe('formatDrawsBracket', () => {
  // Mirrors engine.CompetitionDrawsBracket: league and Swiss courts are
  // parallel mats with no bracket regions to pair, so the rule is not applied
  // to them. The unset format IS in scope: the engine's draw pipeline builds a
  // standalone playoffs bracket for it.
  it.each([
    ['mixed', true],
    ['playoffs', true],
    ['', true],
    ['league', false],
    ['swiss', false],
  ])('format %s draws a bracket: %s', (format, expected) => {
    expect(formatDrawsBracket(format)).toBe(expected);
  });

  it('leaves an odd league allocation unflagged', () => {
    // The concrete case: the league court hint recommends
    // floor(players/2)-1 courts, which is 3 for 8 players. A format-blind
    // rule would reject the app's own recommendation.
    const suggestedForEight = Math.max(1, Math.floor(8 / 2) - 1);
    expect(suggestedForEight).toBe(3);
    expect(shiaijoCountError(suggestedForEight)).not.toBeNull();
    expect(formatDrawsBracket('league')).toBe(false);
  });
});
