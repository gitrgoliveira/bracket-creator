// Surface-contract test for admin_schedule.jsx (mp-d7tl pre-split guard).
//
// Purpose: assert the complete public surface — all ES exports AND all
// window.* assignments — is present after a module import. This is the ONLY
// vitest test that catches a dropped window.* assignment; vitest's lenient
// resolver and esbuild's per-file transpile cannot catch cross-module
// import/export name mismatches, so this test must stay green before, during,
// and after the verbatim module split.
//
// Keep the symbol arrays explicit so adding or removing a public symbol forces
// a deliberate test update.

import { describe, it, expect } from 'vitest';
import {
  filterMatchesByCourt,
  computeCourtPaceStats,
  CourtPacePanel,
  timeEdited,
  timeToMinutes,
  clampMatchDuration,
  suggestRebalances,
  allMatchesCompleted,
  pickCopySource,
  MatchLineupPanel,
} from '../admin_schedule.jsx';

// ── ES export surface ───────────────────────────────────────────────────────

const ES_EXPORTS = {
  filterMatchesByCourt,
  computeCourtPaceStats,
  CourtPacePanel,
  timeEdited,
  timeToMinutes,
  clampMatchDuration,
  suggestRebalances,
  allMatchesCompleted,
  pickCopySource,
  MatchLineupPanel,
};

describe('admin_schedule ES export surface', () => {
  for (const [name, value] of Object.entries(ES_EXPORTS)) {
    it(`exports ${name} as a function`, () => {
      expect(typeof value, `${name} should be a function`).toBe('function');
    });
  }
});

// ── window.* assignment surface ─────────────────────────────────────────────
// The module sets these unconditionally at the top level; in the jsdom
// environment they must all be populated after the import above executes.

const WINDOW_NAMES = [
  'AdminSchedulePage',
  'PerCourtBreakdown',
  'AdminScoreEditorPage',
  'AdminScoreEditor',
  'AdminExport',
  'filterMatchesByCourt',
  'startPatch',
  'MatchLineupPanel',
];

describe('admin_schedule window.* assignment surface', () => {
  for (const name of WINDOW_NAMES) {
    it(`assigns window.${name} as a function`, () => {
      expect(
        typeof window[name],
        `window.${name} should be a function after module load`,
      ).toBe('function');
    });
  }
});
