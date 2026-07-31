// mp-yqxn.1: shared axes + expected ippon-letter tables for the two
// score-editor config-matrix suites (team_editor_config_matrix and
// individual_editor_config_matrix), extracted so the matrices cannot silently
// diverge on their SHARED dimensions (e.g. a format added to one file only).
// Team-only axes (TEAM_SIZES, MATCH_TYPES) stay local to the team suite.
//
// Not a test file: the render project only collects *.render.test.jsx.

// Product-possible format+phase pairs: playoffs has ONLY bracket matches;
// league and swiss have ONLY pool-shaped matches; mixed has both. Cells
// outside this mapping are product-impossible and asserted as such in each
// suite's IMPOSSIBLE CELLS block, not silently skipped.
export const FORMAT_PHASES = [
  { format: 'playoffs', phase: 'bracket' },
  { format: 'mixed', phase: 'pool' },
  { format: 'mixed', phase: 'bracket' },
  { format: 'league', phase: 'pool' },
  { format: 'swiss', phase: 'pool' },
];

export const NAGINATA = [false, true];
export const MAX_ENCHO = [0, 2];

// Independent hardcoded expectations for getIpponButtons(isNaginata) — NOT
// computed from the component, so a letter-set change in
// admin_scoring_shared.jsx fails these suites.
export const KENDO_LETTERS = ['M', 'K', 'D', 'T', 'H'];
export const NAGINATA_LETTERS = ['M', 'K', 'D', 'T', 'S', 'H'];
