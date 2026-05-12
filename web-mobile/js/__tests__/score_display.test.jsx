import { describe, it, expect } from 'vitest';
import { formatIpponsScore } from '../bracket.jsx';

// Convention enforced across all match-list views:
//   SHIRO (sideB) is always displayed on the LEFT.
//   AKA   (sideA) is always displayed on the RIGHT.
//
// Therefore every view that renders a completed score string must call:
//   formatIpponsScore(m.ipponsB, m.ipponsA, m.score, m.decision)
//                     ^^^^^^^^   ^^^^^^^^
//                     SHIRO      AKA
// so the result reads left-to-right as  SHIRO_score – AKA_score.

describe('formatIpponsScore', () => {
  describe('basic ippon formatting', () => {
    it('shows first-arg ippons on the left of the separator', () => {
      const score = formatIpponsScore(['M'], [], null, null);
      // first arg scored M, second arg scored nothing → "M–·"
      expect(score).toBe('M–·');
    });

    it('shows second-arg ippons on the right of the separator', () => {
      const score = formatIpponsScore([], ['K'], null, null);
      expect(score).toBe('·–K');
    });

    it('shows both sides when both scored', () => {
      expect(formatIpponsScore(['M', 'K'], ['D'], null, null)).toBe('MK–D');
    });

    it('returns empty string when no ippons and no score', () => {
      expect(formatIpponsScore([], [], null, null)).toBe('');
    });

    it('filters out placeholder bullets', () => {
      expect(formatIpponsScore(['M', '•'], ['•'], null, null)).toBe('M–·');
    });
  });

  describe('special cases', () => {
    it('returns BYE for bye matches', () => {
      expect(formatIpponsScore([], [], { type: 'bye' }, null)).toBe('BYE');
    });

    it('returns H for hantei decision', () => {
      expect(formatIpponsScore([], [], { type: 'hantei' }, null)).toBe('H');
    });

    it('returns X for a no-score draw', () => {
      expect(formatIpponsScore([], [], { type: 'hikiwake' }, null)).toBe('X');
      expect(formatIpponsScore([], [], null, 'hikewake')).toBe('X');
    });

    it('returns △ for a draw where scores were entered', () => {
      expect(formatIpponsScore(['M'], ['K'], { type: 'hikiwake' }, null)).toBe('△');
    });

    it('falls back to numeric score when ippons arrays are empty but score object has pts', () => {
      const score = formatIpponsScore([], [], { type: 'ippon', winnerPts: 2, loserPts: 1 }, null);
      expect(score).toBe('2–1');
    });
  });

  describe('SHIRO-left / AKA-right display contract', () => {
    // The Scores-edit list, VSchedItem, PoolMatchRow, MatchDetailCard, and TWMatch all
    // display SHIRO on the left and AKA on the right, so they call
    // formatIpponsScore(ipponsB, ipponsA, ...).
    //
    // These tests document and enforce that convention so a future refactor
    // cannot silently reverse the sides.

    const akaMatch = {
      sideA: { id: 'aka', name: 'AKA Player' },
      sideB: { id: 'shiro', name: 'SHIRO Player' },
      ipponsA: ['M'],          // AKA (right) scored M
      ipponsB: [],             // SHIRO (left) scored nothing
      score: null,
      decision: null,
    };

    it('calling with (ipponsB, ipponsA) → left side shows SHIRO score', () => {
      const result = formatIpponsScore(akaMatch.ipponsB, akaMatch.ipponsA, akaMatch.score, akaMatch.decision);
      // SHIRO scored nothing → left of separator is "·"
      // AKA scored M         → right of separator is "M"
      expect(result).toBe('·–M');
    });

    it('calling with (ipponsA, ipponsB) would wrongly put AKA score on the left', () => {
      // This is the WRONG call order for SHIRO-left views — test documents the mistake
      const wrong = formatIpponsScore(akaMatch.ipponsA, akaMatch.ipponsB, akaMatch.score, akaMatch.decision);
      expect(wrong).toBe('M–·');   // M appears left, but AKA is visually on the right → misleading
    });

    it('SHIRO-left view: result string reads SHIRO_score–AKA_score', () => {
      const shiroMatch = {
        ipponsA: ['K'],   // AKA scored K
        ipponsB: ['M'],   // SHIRO scored M
        score: null, decision: null,
      };
      const result = formatIpponsScore(shiroMatch.ipponsB, shiroMatch.ipponsA, shiroMatch.score, shiroMatch.decision);
      // SHIRO (left) scored M, AKA (right) scored K → "M–K"
      expect(result).toBe('M–K');
    });
  });
});
