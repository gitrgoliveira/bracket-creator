import { describe, it, expect } from 'vitest';
import { readFileSync } from 'fs';
import { resolve } from 'path';
import { enchoLabel, formatIpponsScore, ipponsFromScore, matchStateCell } from '../bracket.jsx';

// Convention enforced across all match-list views:
//   SHIRO (sideB) is always displayed on the LEFT.
//   AKA   (sideA) is always displayed on the RIGHT.
//
// Therefore every view that renders a completed score string must call:
//   formatIpponsScore(m.ipponsB, m.ipponsA, m.score, m.decision)
//                     ^^^^^^^^   ^^^^^^^^
//                     SHIRO      AKA
// so the result reads left-to-right as SHIRO_score–AKA_score.

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

    it('returns the points around the X for a scored equal draw (1–1)', () => {
      // Item 6 + the middle-column rule: the ippons are preserved on the
      // server, so show the techniques, AND the tie's X is the middle mark,
      // so the viewer sees both what was struck and that it was a tie.
      expect(formatIpponsScore(['M'], ['K'], { type: 'hikiwake' }, null)).toBe('M X K');
    });

    it('shows scored draw with one empty side using the placeholder dot', () => {
      expect(formatIpponsScore(['M'], [], { type: 'hikiwake' }, null)).toBe('M X ·');
    });

    // Numbers are NOT a valid display for ippon. The per-side waza-letter
    // arrays are the only source of an ippon score string: real data always
    // carries them (callers derive via ipponsFromScore from scoreA/scoreB),
    // and count-only score objects render NO score rather than digits. The
    // numeric winnerPts/loserPts fields stay untouched for logic that needs
    // them (activity checks, standings) — they just never render as ippon.
    it('never renders numeric counts as an ippon score', () => {
      expect(formatIpponsScore([], [], { type: 'ippon', winnerPts: 2, loserPts: 1 }, null)).toBe('');
      expect(formatIpponsScore([], [], { type: 'ippon', winnerPts: 2, loserPts: 1, ippons: ['M', 'K'] }, null)).toBe('');
      expect(formatIpponsScore([], [], { type: 'ippon', winnerPts: 1, loserPts: 0, ippons: ['•'] }, null)).toBe('');
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
      // This is the WRONG call order for SHIRO-left views. Test documents the mistake
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

  // The middle of a score string carries exactly ONE mark — X (tie), (E)
  // (overtime), (DH) (rep bout) — or the plain "–" separator. X beats (E)
  // because a match that went to encho cannot end tied; (DH) beats (E)
  // because a daihyosen bout is one-point sudden death with no overtime.
  describe('middle mark', () => {
    it('places (E) between the scores when encho has a positive period count', () => {
      expect(formatIpponsScore(['M'], ['K'], null, null, { periodCount: 1 })).toBe('M (E) K');
    });

    it('a tie is X, never (E): stale draw+encho data renders the X alone', () => {
      expect(formatIpponsScore(['M'], ['K'], { type: 'hikiwake' }, null, { periodCount: 1 })).toBe('M X K');
      expect(formatIpponsScore([], [], null, 'hikiwake', { periodCount: 2 })).toBe('X');
    });

    it('a daihyosen is (DH), never (E): DH bouts have no encho', () => {
      // Winner side known: Ht (the hantei winner mark) sits in the winner's
      // cell, (DH) alone holds the middle.
      expect(formatIpponsScore([], [], null, 'daihyosen', { periodCount: 3 }, true, 'left')).toBe('Ht (DH) ·');
    });

    it('does not mark the middle when periodCount is 0', () => {
      expect(formatIpponsScore(['M'], ['K'], null, null, { periodCount: 0 })).toBe('M–K');
    });

    it('is a no-op when encho argument is missing entirely', () => {
      expect(formatIpponsScore(['M'], ['K'], null, null)).toBe('M–K');
    });
  });

  // Result marks (Kiken / Fus. / Ht) ride in the cell of the competitor they
  // name when the caller supplies winnerSide (matchScoreStr derives it via
  // winnerSideLR); without it they trail so the result is never dropped.
  describe('side result marks', () => {
    it('kiken marks the withdrawing (losing) side', () => {
      expect(formatIpponsScore(['M'], [], null, 'kiken-voluntary', null, false, 'left')).toBe('M–Kiken');
      expect(formatIpponsScore([], [], null, 'kiken-voluntary', null, false, 'right')).toBe('Kiken–·');
    });

    it('fusenpai marks the no-show (losing) side', () => {
      // Winner on the left → the no-show's Fus. lands in the right cell.
      expect(formatIpponsScore([], [], null, 'fusenpai', null, false, 'left')).toBe('·–Fus.');
    });

    it('kiken during overtime: loser mark plus the (E) middle', () => {
      expect(formatIpponsScore(['M'], [], null, 'kiken-injury', { periodCount: 1 }, false, 'left')).toBe('M (E) Kiken');
    });

    it('falls back to a trailing mark when the winner side is unknown', () => {
      expect(formatIpponsScore(['M'], [], null, 'kiken-voluntary', null, false)).toBe('M–· Kiken');
    });
  });

  // FIK Art. 7-5 / 29-6: a knockout match that remains tied in encho is
  // decided by referee hantei. The renderer must mark this distinctly so
  // it's not confused with an ippon-derived win.
  describe('hantei (judges\' decision) winner mark', () => {
    it('a 0-0 hantei-decided overtime puts Ht in the winner\'s cell', () => {
      // Tied 0-0 in encho, SHIRO (left) awarded by hantei: the winner's cell
      // carries the Ht mark, the middle carries (E), the loser shows the dot.
      expect(formatIpponsScore([], [], null, null, { periodCount: 1 }, true, 'left')).toBe('Ht (E) ·');
      expect(formatIpponsScore([], [], null, null, { periodCount: 1 }, true, 'right')).toBe('· (E) Ht');
    });

    it('falls back to "(E) Ht" when the winner side is unknown', () => {
      expect(formatIpponsScore([], [], null, null, { periodCount: 1 }, true)).toBe('(E) Ht');
    });

    it('rides next to the winner\'s letters on a scored hantei overtime', () => {
      const result = formatIpponsScore(['M'], ['K'], null, null, { periodCount: 1 }, true, 'left');
      expect(result).toBe('M Ht (E) K');
    });

    it('trails when the winner side is unknown (never dropped)', () => {
      expect(formatIpponsScore(['M'], ['K'], null, null, { periodCount: 1 }, true)).toBe('M (E) K Ht');
    });

    it('omits Ht when decidedByHantei is false/missing', () => {
      expect(formatIpponsScore(['M'], ['K'], null, null, { periodCount: 1 }, false)).toBe('M (E) K');
      expect(formatIpponsScore(['M'], ['K'], null, null, { periodCount: 1 })).toBe('M (E) K');
    });

    it('score.hantei is not read. Only the decidedByHantei param controls Ht', () => {
      // The `score` object is derived client-side by normalizeMatch from flat
      // API fields (ipponsA/B, scoreA/B). The backend never emits a `score`
      // object, so score.hantei can never appear in real match data. Only the
      // positional decidedByHantei arg matters.
      expect(formatIpponsScore(['M'], ['K'], { type: 'ippon', hantei: true }, null, { periodCount: 1 })).toBe('M (E) K');
      expect(formatIpponsScore(['M'], ['K'], { type: 'ippon', hantei: true }, null, { periodCount: 1 }, true, 'left')).toBe('M Ht (E) K');
    });
  });
});

// Item 6 regression suite: scored-draw rendering (formatIpponsScore).
// Pinned so future changes to the hikiwake branch can't silently drop the
// struck techniques (they surround the X) or the X itself (the tie's one
// legal middle mark).
describe('formatIpponsScore: hikiwake draw display (item 6)', () => {
  it('0–0 hikiwake (score.type) → bare X', () => {
    expect(formatIpponsScore([], [], { type: 'hikiwake' }, null)).toBe('X');
  });

  it('0–0 hikiwake (decision string) → bare X', () => {
    expect(formatIpponsScore([], [], null, 'hikiwake')).toBe('X');
  });

  it('1–1 hikiwake (ipponsA=[M], ipponsB=[K]) → "M X K" (points around the X)', () => {
    // Canonical scored-equal-draw case: both sides hit one ippon, operator
    // toggled hikiwake. Server keeps ippons; display shows the techniques
    // AND the draw mark in the middle.
    expect(formatIpponsScore(['M'], ['K'], { type: 'hikiwake' }, null)).toBe('M X K');
  });

  it('1–1 hikiwake with stale encho data → still "M X K" (X beats (E))', () => {
    // A tie cannot have gone to encho; if drifted data carries both, the
    // draw mark wins and the overtime marker is dropped.
    expect(formatIpponsScore(['M'], ['K'], { type: 'hikiwake' }, null, { periodCount: 1 })).toBe('M X K');
  });
});

// matchStateCell: the shared centre-cell lifecycle cue. completed → score
// string (with "-" fallback), running → "vs" (the row highlight is the "now"
// signal, NOT a centre dot), scheduled/other → "–".
describe('matchStateCell: shared running-row centre cue', () => {
  it('completed → the formatted ippon score (first arg = SHIRO/left)', () => {
    // matchStateCell(m, ipponsB, ipponsA) → matchScoreStr → formatIpponsScore
    // renders firstArg–secondArg, so ['M'],['K'] → "M–K".
    expect(matchStateCell({ status: 'completed' }, ['M'], ['K'])).toBe('M–K');
  });

  it('completed with no derivable score → "-" fallback', () => {
    // No ippons, no score, no decision → matchScoreStr returns "" → "-".
    expect(matchStateCell({ status: 'completed' }, [], [])).toBe('-');
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

// Bracket (knockout) team matches must render BOTH IV and PW, exactly like
// pool team matches. The server attaches teamResult to bracket matches via
// BracketMatch.MarshalJSON (internal/state/team_result.go); the pre-fix wire
// had no teamResult on bracket matches, so matchScoreStr fell back to the
// legacy IV-only aggregate (bead mp-8b1b).
describe('team score string carries IV and PW', () => {
  it('renders IV and PW from a bracket-shaped match with teamResult', () => {
    const m = {
      status: 'completed', sideA: 'Ryu', sideB: 'Tora', winner: 'Ryu',
      teamResult: { shiroIV: 0, akaIV: 5, shiroPW: 0, akaPW: 5 },
      subResults: [
        { position: 1, sideA: 'Ryu Ichiro', sideB: 'Tora Ichiro', winner: 'Ryu Ichiro', ipponsA: ['M'] },
      ],
    };
    expect(matchStateCell(m, [], [])).toBe('IV 0–5\nPW 0–5');
  });

  it('renders the tied IV and PW for a daihyosen-decided final', () => {
    const m = {
      status: 'completed', sideA: 'Ryu', sideB: 'Kaze', winner: 'Ryu',
      teamResult: { shiroIV: 0, akaIV: 0, shiroPW: 0, akaPW: 0 },
      subResults: [{ position: -1, sideA: 'Ryu', sideB: 'Kaze', winner: 'Ryu', ipponsA: ['M'] }],
    };
    expect(matchStateCell(m, [], [])).toBe('IV 0–0\nPW 0–0');
  });
});

// mp-m4bn: JS half of the shared Go/JS golden table for the overtime marker —
// see the `_comment` in encho_labels.json for why the table is shared and why
// it pins values, not source text. Go half: TestEnchoLabel_GoldenTable in
// internal/export/suffix_test.go.
describe('enchoLabel Go/JS mirror (mp-m4bn)', () => {
  const table = JSON.parse(
    readFileSync(
      resolve(__dirname, '..', '..', '..', 'internal', 'export', 'testdata', 'encho_labels.json'),
      'utf8'
    )
  );

  // Load-bearing: vitest's it.each over an empty array silently produces
  // zero tests (no red), so a degraded table needs its own failure.
  it('the shared golden table is present and non-empty', () => {
    expect(
      table.cases?.length,
      'internal/export/testdata/encho_labels.json parsed to zero cases: the mirror would assert nothing'
    ).toBeGreaterThan(0);
  });

  // enchoLabel is driven directly, so the table value is asserted EXACTLY —
  // an unrelated formatIpponsScore change cannot redden this with a
  // misleading "update both renderers" message.
  it.each(table.cases)('periodCount $periodCount renders "$label"', ({ periodCount, label }) => {
    expect(
      enchoLabel({ periodCount }),
      'JS enchoLabel disagrees with the shared table; update BOTH renderers, not just this one'
    ).toBe(label);
  });
});
