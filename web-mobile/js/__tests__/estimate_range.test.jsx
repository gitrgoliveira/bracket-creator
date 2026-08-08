// estimateRangeParts (mp-gmcg): kachinuki team matches have a variable bout
// count, so the schedule estimate is a best/average/worst RANGE. The server
// returns additive bestCaseMinutes / worstCaseMinutes bracketing the headline
// totalDurationMinutes (the AVERAGE scenario). The helper returns {best,
// average, worst} only when the estimate genuinely spans a range, and null
// when the three collapse (individual / fixed-format, or a legacy response
// without the fields) so callers render the single total exactly as before.

import { describe, it, expect } from 'vitest';
import { estimateRangeParts } from '../admin_schedule_utils.jsx';

describe('estimateRangeParts', () => {
  it('returns null for a nullish estimate', () => {
    expect(estimateRangeParts(null)).toBeNull();
    expect(estimateRangeParts(undefined)).toBeNull();
  });

  it('returns null when best === worst (non-kachinuki / fixed collapse)', () => {
    expect(estimateRangeParts({ totalDurationMinutes: 60, bestCaseMinutes: 60, worstCaseMinutes: 60 })).toBeNull();
  });

  it('returns null for a legacy response lacking the range fields', () => {
    expect(estimateRangeParts({ totalDurationMinutes: 60 })).toBeNull();
  });

  it('returns {best, average, worst} when the estimate spans a range', () => {
    const est = { totalDurationMinutes: 75, bestCaseMinutes: 53, worstCaseMinutes: 97 };
    expect(estimateRangeParts(est)).toEqual({ best: 53, average: 75, worst: 97 });
  });

  it('returns null when the range fields are non-finite', () => {
    expect(estimateRangeParts({ totalDurationMinutes: 75, bestCaseMinutes: NaN, worstCaseMinutes: 97 })).toBeNull();
    expect(estimateRangeParts({ totalDurationMinutes: 75, bestCaseMinutes: 53, worstCaseMinutes: Infinity })).toBeNull();
  });
});
