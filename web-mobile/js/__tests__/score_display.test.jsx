import { describe, it, expect } from 'vitest';
import { formatIpponsScore, ipponsFromScore, matchStateCell } from '../bracket.jsx';

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

    it('returns X for a no-score draw', () => {
      expect(formatIpponsScore([], [], { type: 'hikiwake' }, null)).toBe('X');
      expect(formatIpponsScore([], [], null, 'hikiwake')).toBe('X');
    });

    it('returns X for a scoreless draw (canonical hikiwake glyph, no ippons)', () => {
      expect(formatIpponsScore([], [], { type: 'hikiwake' }, null)).toBe('X');
      expect(formatIpponsScore([], [], null, 'hikiwake')).toBe('X');
    });

    it('returns the points for a scored equal draw (1–1), not bare X', () => {
      // Item 6: operator entered M on one side, K on the other, then toggled
      // hikiwake. The ippons are preserved on the server (the score.type is
      // hikiwake but ipponsA/B are non-empty). Show the techniques so the
      // viewer sees what was struck rather than losing that information.
      expect(formatIpponsScore(['M'], ['K'], { type: 'hikiwake' }, null)).toBe('M–K');
    });

    it('shows scored draw with one empty side using the placeholder dot', () => {
      expect(formatIpponsScore(['M'], [], { type: 'hikiwake' }, null)).toBe('M–·');
    });

    it('falls back to numeric score when ippons arrays are empty AND score has no ippon letters', () => {
      const score = formatIpponsScore([], [], { type: 'ippon', winnerPts: 2, loserPts: 1 }, null);
      expect(score).toBe('2–1');
    });

    it('prefers the winner waza LETTERS over a count when score.ippons is present', () => {
      // Per-side arrays absent (server bracket score) but score.ippons carries
      // the winner's techniques — show "MK–1", not "2–1". Loser is a count-only
      // value in this degenerate path, so it stays numeric. Display-only: the
      // numeric winnerPts/loserPts fields are untouched for logic that needs them.
      const score = formatIpponsScore([], [], { type: 'ippon', winnerPts: 2, loserPts: 1, ippons: ['M', 'K'] }, null);
      expect(score).toBe('MK–1');
    });

    it('ignores empty/dot placeholders in score.ippons and falls back to the count', () => {
      const score = formatIpponsScore([], [], { type: 'ippon', winnerPts: 1, loserPts: 0, ippons: ['•'] }, null);
      expect(score).toBe('1–0');
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

  // FR-033: encho is rendered as a trailing " (E)" so the match list and
  // bracket views surface that the match went to overtime.
  describe('encho suffix', () => {
    it('appends (E) to a normal ippon score when encho has a positive period count', () => {
      expect(formatIpponsScore(['M'], ['K'], null, null, { periodCount: 1 })).toBe('M–K (E)');
    });

    it('appends (E) to a scored draw (shows points, not X)', () => {
      // Item 6: scored equal draw shows techniques + encho suffix, not bare X.
      expect(formatIpponsScore(['M'], ['K'], { type: 'hikiwake' }, null, { periodCount: 1 })).toBe('M–K (E)');
    });

    it('appends (E) to a no-score draw (X is the scoreless-draw glyph)', () => {
      expect(formatIpponsScore([], [], null, 'hikiwake', { periodCount: 2 })).toBe('X (E)');
    });

    it('does not append (E) when periodCount is 0', () => {
      expect(formatIpponsScore(['M'], ['K'], null, null, { periodCount: 0 })).toBe('M–K');
    });

    it('is a no-op when encho argument is missing entirely', () => {
      expect(formatIpponsScore(['M'], ['K'], null, null)).toBe('M–K');
    });
  });

  // FIK Art. 7-5 / 29-6: a knockout match that remains tied in encho is
  // decided by referee hantei. The renderer must mark this distinctly so
  // it's not confused with an ippon-derived win.
  describe('hantei (judges\' decision) suffix', () => {
    it('appends "(E) Ht" for a 0-0 hantei-decided overtime', () => {
      // Tied 0-0 in encho, AKA awarded by hantei. No ippons → suffix only.
      expect(formatIpponsScore([], [], null, null, { periodCount: 1 }, true)).toBe('(E) Ht');
    });

    it('combines (E) Ht for a hantei-decided overtime', () => {
      // Realistic: tied with scores, then hantei chose a winner — backend
      // sends decidedByHantei=true alongside the tied ippons.
      const result = formatIpponsScore(['M'], ['K'], null, null, { periodCount: 1 }, true);
      expect(result).toBe('M–K (E) Ht');
    });

    it('omits Ht when decidedByHantei is false/missing', () => {
      expect(formatIpponsScore(['M'], ['K'], null, null, { periodCount: 1 }, false)).toBe('M–K (E)');
      expect(formatIpponsScore(['M'], ['K'], null, null, { periodCount: 1 })).toBe('M–K (E)');
    });

    it('score.hantei is not read — only the decidedByHantei param controls Ht', () => {
      // The `score` object is derived client-side by normalizeMatch from flat
      // API fields (ipponsA/B, scoreA/B). The backend never emits a `score`
      // object, so score.hantei can never appear in real match data. Only the
      // positional decidedByHantei arg matters.
      expect(formatIpponsScore(['M'], ['K'], { type: 'ippon', hantei: true }, null, { periodCount: 1 })).toBe('M–K (E)');
      expect(formatIpponsScore(['M'], ['K'], { type: 'ippon', hantei: true }, null, { periodCount: 1 }, true)).toBe('M–K (E) Ht');
    });
  });
});

// Item 6 regression suite: scored-draw rendering (formatIpponsScore).
// Pinned so future changes to the hikiwake branch can't silently revert
// the display from "M–K" back to the bare-X glyph.
describe('formatIpponsScore — hikiwake draw display (item 6)', () => {
  it('0–0 hikiwake (score.type) → bare X', () => {
    expect(formatIpponsScore([], [], { type: 'hikiwake' }, null)).toBe('X');
  });

  it('0–0 hikiwake (decision string) → bare X', () => {
    expect(formatIpponsScore([], [], null, 'hikiwake')).toBe('X');
  });

  it('1–1 hikiwake (ipponsA=[M], ipponsB=[K]) → "M–K" (points shown, no X)', () => {
    // Canonical scored-equal-draw case: both sides hit one ippon, operator
    // toggled hikiwake. Server keeps ippons; display must show techniques.
    expect(formatIpponsScore(['M'], ['K'], { type: 'hikiwake' }, null)).toBe('M–K');
  });

  it('1–1 hikiwake with encho (periodCount>0) → "M–K (E)"', () => {
    // Encho suffix must survive the scored-draw path unchanged.
    expect(formatIpponsScore(['M'], ['K'], { type: 'hikiwake' }, null, { periodCount: 1 })).toBe('M–K (E)');
  });
});

// matchStateCell: the shared centre-cell lifecycle cue. completed → score
// string (with "—" fallback), running → "vs" (the row highlight is the "now"
// signal, NOT a centre dot), scheduled/other → "–".
describe('matchStateCell — shared running-row centre cue', () => {
  it('completed → the formatted ippon score (first arg = SHIRO/left)', () => {
    // matchStateCell(m, ipponsB, ipponsA) → matchScoreStr → formatIpponsScore
    // renders firstArg–secondArg, so ['M'],['K'] → "M–K".
    expect(matchStateCell({ status: 'completed' }, ['M'], ['K'])).toBe('M–K');
  });

  it('completed with no derivable score → "—" fallback', () => {
    // No ippons, no score, no decision → matchScoreStr returns "" → "—".
    expect(matchStateCell({ status: 'completed' }, [], [])).toBe('—');
  });

  it('running → "vs" (no centre dot; the row highlight is the now signal)', () => {
    expect(matchStateCell({ status: 'running' }, [], [])).toBe('vs');
  });

  it('scheduled → "–"', () => {
    expect(matchStateCell({ status: 'scheduled' }, [], [])).toBe('–');
  });

  it('unknown/missing status → "–" (treated as not-yet-run)', () => {
    expect(matchStateCell({ status: 'bye' }, [], [])).toBe('–');
    expect(matchStateCell({}, [], [])).toBe('–');
  });

  it('never emits a bare "●" for any state', () => {
    for (const status of ['completed', 'running', 'scheduled', 'bye', undefined]) {
      expect(matchStateCell({ status }, [], [])).not.toContain('●');
    }
  });
});

// ipponsFromScore: strips the Go formatScore "(HN)" hansoku suffix before splitting
describe('ipponsFromScore', () => {
  it('splits plain letters', () => {
    expect(ipponsFromScore('MK')).toEqual(['M', 'K']);
  });

  it('strips (HN) suffix before splitting', () => {
    expect(ipponsFromScore('M(H1)')).toEqual(['M']);
    expect(ipponsFromScore('MK(H2)')).toEqual(['M', 'K']);
  });

  // Real backend output: engine/scoring.go formatScore() inserts a space
  // between ippons and the (HN) suffix when both are present
  // ("MK (H1)"). The regex must strip the optional whitespace too,
  // otherwise split("") returns a trailing " " token that renders as a
  // bogus ippon character.
  it('strips spaced (HN) suffix before splitting (real backend shape)', () => {
    expect(ipponsFromScore('M (H1)')).toEqual(['M']);
    expect(ipponsFromScore('MK (H2)')).toEqual(['M', 'K']);
    expect(ipponsFromScore('MKD (H1)')).toEqual(['M', 'K', 'D']);
  });

  it('handles suffix-only string (no ippons, just fouls)', () => {
    expect(ipponsFromScore('(H1)')).toEqual([]);
  });

  it('returns [] for empty/null/undefined', () => {
    expect(ipponsFromScore('')).toEqual([]);
    expect(ipponsFromScore(null)).toEqual([]);
    expect(ipponsFromScore(undefined)).toEqual([]);
  });
});
