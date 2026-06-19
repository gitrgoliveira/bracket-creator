// Barrel-completeness test for admin_scoring_modal.jsx.
//
// Purpose: catch dropped re-exports and missing window.ScoreEditorModal
// after any refactor of the module (including the 4-way split in mp-zac3).
// This test is intentionally WIRING-focused, not logic-focused. It verifies
// that every name the surrounding codebase depends on is still exported, and
// that the window bridge is still set. It does NOT test any function behavior.
//
// Run on the current (pre-split) file first to establish a green baseline.
// After the split the SAME test — unchanged, importing from the thin entry
// admin_scoring_modal.jsx — must stay green.

import { describe, it, expect } from 'vitest';

// The window.ScoreEditorModal bridge is set as a side-effect of importing
// admin_scoring_modal.jsx. vitest.setup.js pre-loads admin_helpers.jsx so
// window.MAX_TEAM_SIZE etc. are available when this module evaluates.
import * as M from '../admin_scoring_modal.jsx';

// admin_scoring_modal.jsx sets window.ScoreEditorModal as a side-effect.
// We import AFTER the module has been evaluated so the window global is set.
// (ESM static imports are evaluated before the test body runs, so the
// assignment at the bottom of admin_scoring_modal.jsx runs at import time.)

// ─── Expected exports ────────────────────────────────────────────────────────
// This list is the single source of truth for what admin_scoring_modal.jsx
// MUST export. Add names here when new exports are intentionally added;
// remove them only when a deliberate breaking change is made.

const EXPECTED_EXPORTS = [
  // Autosave / sync (from admin_scoring_autosave.jsx in post-split world)
  'AUTOSAVE_DEBOUNCE_MS',
  'SyncStatusPill',
  'useDebouncedRunningWrite',
  // Pure helpers re-exported from admin_scoring_shared.jsx
  'MAX_IPPONS_PER_SIDE',
  'isBoutDecided',
  'getIpponButtons',
  'getValidPointKeys',
  'applyFusenshoToggle',
  'applyFoulIncrement',
  'reconcileFoulsAtOpen',
  'nextFoulOnDecrement',
  'resolveDecisionPassword',
  'assertRunningWritePersisted',
  'buildDecisionBody',
  'submitDecisionRequest',
  'makeSubmitDecision',
  'shouldShowEnchoMaxBanner',
  'canIncrementEncho',
  'nextEnchoPeriod',
  'prevEnchoPeriod',
  'initialEnchoPeriodsForMatch',
  'daihyosenEnchoFields',
  'decideDrawToggle',
  'shouldBlockScoringKeys',
  'DecisionPrompt',
  // Team-specific helpers (from admin_scoring_team.jsx in post-split world)
  'teamResultLabel',
  'isKoTieBlocked',
  // Lineup resolvers (from lineup_resolver.jsx in post-split world)
  'resolveMatchLineup',
  'resolveLineupTeamId',
];

describe('admin_scoring_modal barrel completeness', () => {
  it('exports every expected named symbol', () => {
    for (const name of EXPECTED_EXPORTS) {
      expect(
        M[name],
        `Missing export: "${name}" — was it dropped from the barrel?`,
      ).toBeDefined();
    }
  });

  it('exports no UNEXPECTED symbols (frozen surface — update this list when adding exports)', () => {
    const actual = new Set(Object.keys(M));
    const expected = new Set(EXPECTED_EXPORTS);
    const unexpected = [...actual].filter(k => !expected.has(k));
    expect(
      unexpected,
      `Unexpected export(s): ${unexpected.join(', ')} — add to EXPECTED_EXPORTS if intentional`,
    ).toHaveLength(0);
  });

  it('sets window.ScoreEditorModal to a function (window bridge)', () => {
    expect(typeof window.ScoreEditorModal).toBe('function');
  });
});
