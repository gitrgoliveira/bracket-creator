// Surface-contract test for admin_schedule.jsx (mp-d7tl pre-split guard).
//
// Purpose: assert the complete public surface (all ES exports AND all
// window.* assignments) is present after a module import. This is the ONLY
// vitest test that catches a dropped window.* assignment; vitest's lenient
// resolver and esbuild's per-file transpile cannot catch cross-module
// import/export name mismatches, so this test must stay green before, during,
// and after the verbatim module split.
//
// Keep the symbol arrays explicit so adding or removing a public symbol forces
// a deliberate test update.

import { describe, it, expect } from 'vitest';
import { readFileSync, readdirSync } from 'fs';
import { resolve, dirname } from 'path';
import { fileURLToPath } from 'url';
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

// ── window.API.* contract ───────────────────────────────────────────────────
// Regression guard (PR #293): the split's copyFromPrevious called
// window.API.fetchMatchLineupHeaders, a method that never existed; the
// try/catch swallowed the TypeError so the feature silently broke. vitest's
// fake-React stub never mounts these modules, so nothing else catches a call
// to an undefined API method. Statically assert every window.API.X referenced
// across the admin_schedule split is actually defined in api_client.jsx.

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = resolve(__dirname, '..');

describe('admin_schedule window.API.* contract', () => {
  const apiSrc = readFileSync(resolve(jsDir, 'api_client.jsx'), 'utf8');
  // A method definition is `\n<indent>[async ]name(` (i.e., the name follows
  // only whitespace after a newline). A call site is `.name(` (a dot precedes
  // the name), so this pattern matches definitions but never invocations.
  const isDefined = (name) =>
    new RegExp(`\\n\\s+(async\\s+)?${name}\\s*\\(`).test(apiSrc);

  const moduleFiles = readdirSync(jsDir)
    .filter((f) => /^admin_schedule.*\.jsx$/.test(f));

  // Collect every distinct window.API.X reference across the split.
  const refs = new Map(); // method name → set of files referencing it
  for (const file of moduleFiles) {
    const src = readFileSync(resolve(jsDir, file), 'utf8');
    for (const m of src.matchAll(/window\.API\.([a-zA-Z_$][\w$]*)/g)) {
      if (!refs.has(m[1])) refs.set(m[1], new Set());
      refs.get(m[1]).add(file);
    }
  }

  it('references at least one window.API method (sanity)', () => {
    expect(refs.size, 'expected the split to call some window.API.* methods').toBeGreaterThan(0);
  });

  for (const [method, files] of refs) {
    it(`window.API.${method} is defined in api_client.jsx`, () => {
      expect(
        isDefined(method),
        `window.API.${method} (used in ${[...files].join(', ')}) has no definition in api_client.jsx`,
      ).toBe(true);
    });
  }
});
