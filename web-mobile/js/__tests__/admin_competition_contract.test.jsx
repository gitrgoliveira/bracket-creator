import { describe, it, expect } from 'vitest';
import * as adminCompetition from '../admin_competition.jsx';

// mp-hpe3 Phase 0 safety net — PUBLIC-SURFACE CONTRACT for admin_competition.jsx.
//
// This is the regression oracle for the upcoming split of the 1.7k-line
// admin_competition.jsx into cohesive modules. The split keeps the original
// file as a THIN ENTRY that must re-export the FULL public surface — both the
// ES `export {…}` block AND the `window.*` component assignments — so that
// dependent test files and non-module admin scripts keep resolving symbols.
//
// vitest's lenient resolver + esbuild's per-file transpile do NOT catch a
// dropped re-export across module boundaries (only native ESM in the browser
// throws "does not provide an export named X"). So this test pins the surface
// EXPLICITLY: it passes against today's monolith and must stay green through
// every step of the split. If a symbol goes missing from the thin entry, this
// fails loudly in CI instead of silently blanking the SPA in production.
//
// When the public surface legitimately changes, update the manifests below in
// the same commit — that diff is the intentional record of the change.

// ES named exports the thin entry must expose (consumed by the vitest suite).
const EXPECTED_ES_EXPORTS = [
  'buildRunningIpponResult',
  'loadScoreboardPoints',
  'swissRoundIDPrefix',
  'filterSwissRoundMatches',
  'isSwissRoundComplete',
  'canGenerateNextSwissRound',
  'isSwissCompetitionComplete',
  'formatCompMinutes',
];

// Components assigned to window at module load. Importing admin_competition.jsx
// evaluates every section sub-module — overview via the side-effect import,
// settings/bracket/swiss via the helper re-exports — so ALL of these
// assignments run during this test. A split that drops any `window.X = X`
// would render that section undefined in the browser; this pins all six so it
// fails loudly in CI instead.
const EXPECTED_WINDOW_GLOBALS = [
  'AdminCompetition',
  'AdminCompOverview',
  'FightingSpiritAwardsEditor',
  'AdminSettings',
  'AdminBracket',
  'AdminSwissRounds',
];

describe('admin_competition.jsx public-surface contract (mp-hpe3 split oracle)', () => {
  describe('ES named exports', () => {
    for (const name of EXPECTED_ES_EXPORTS) {
      it(`re-exports ${name} as a function`, () => {
        expect(adminCompetition[name], `missing ES export: ${name}`).toBeDefined();
        expect(typeof adminCompetition[name], `${name} should be a function`).toBe('function');
      });
    }

    it('does not silently drop any expected export (exact set guard)', () => {
      const actual = Object.keys(adminCompetition).filter(
        (k) => typeof adminCompetition[k] === 'function',
      );
      for (const name of EXPECTED_ES_EXPORTS) {
        expect(actual, `thin entry no longer exports ${name}`).toContain(name);
      }
    });
  });

  describe('window.* component globals', () => {
    for (const name of EXPECTED_WINDOW_GLOBALS) {
      it(`assigns window.${name} at module load`, () => {
        // The module was imported above for its side effects; the assignments
        // at the tail of admin_competition.jsx (window.AdminCompetition = …)
        // run at load regardless of the React stub.
        expect(window[name], `window.${name} not set — split dropped the assignment`).toBeDefined();
        expect(typeof window[name], `window.${name} should be a component (function)`).toBe('function');
      });
    }
  });
});
