import { describe, it, expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { normalizeCompetitionDetail } from '../api_serializers.jsx';
import { competitionHasRecordedResult } from '../recorded_result.jsx';

const __dirname = dirname(fileURLToPath(import.meta.url));
const casesPath = resolve(__dirname, '../../../internal/engine/testdata/recorded_result_cases.json');
const { cases } = JSON.parse(readFileSync(casesPath, 'utf8'));

// The table's matches are in wire shape. The Export tab is given poolMatches
// and bracket as normalizeCompetitionDetail makes them (sides as {id, name}
// objects), so each case goes through it before the rule reads it.
function pageShape(tc) {
  const detail = { config: {}, poolMatches: tc.poolMatches, bracket: tc.bracket };
  const out = normalizeCompetitionDetail(structuredClone(detail));
  return { poolMatches: out.poolMatches, bracket: out.bracket };
}

describe('competitionHasRecordedResult (shared recorded_result_cases.json)', () => {
  it('reads a non-empty table', () => {
    expect(cases.length).toBeGreaterThan(0);
  });

  cases.forEach((tc) => {
    it(tc.name, () => {
      const { poolMatches, bracket } = pageShape(tc);
      expect(competitionHasRecordedResult(poolMatches, bracket)).toBe(tc.expected);
    });
  });

  it('holds nothing for a competition whose matches are not loaded', () => {
    expect(competitionHasRecordedResult(undefined, undefined)).toBe(false);
    expect(competitionHasRecordedResult(null, null)).toBe(false);
  });
});
