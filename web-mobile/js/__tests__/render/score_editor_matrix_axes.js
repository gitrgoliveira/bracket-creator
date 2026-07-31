// mp-yqxn.1: shared axes + expected ippon-letter tables for the two
// score-editor config-matrix suites (team_editor_config_matrix and
// individual_editor_config_matrix), extracted so the matrices cannot silently
// diverge on their SHARED dimensions (e.g. a format added to one file only).
// Team-only axes (TEAM_SIZES, MATCH_TYPES) stay local to the team suite.
//
// Not a test file: the render project only collects *.render.test.jsx.

// Product-possible format+phase pairs: playoffs has ONLY bracket matches;
// league and swiss have ONLY pool-shaped matches; mixed has both. This list is
// the single source of truth for the whole axis space: the format/phase value
// sets below and the impossible complement are both derived from it.
export const FORMAT_PHASES = [
  { format: 'playoffs', phase: 'bracket' },
  { format: 'mixed', phase: 'pool' },
  { format: 'mixed', phase: 'bracket' },
  { format: 'league', phase: 'pool' },
  { format: 'swiss', phase: 'pool' },
];

const FORMATS = [...new Set(FORMAT_PHASES.map(fp => fp.format))];
const PHASES = [...new Set(FORMAT_PHASES.map(fp => fp.phase))];

// The complement of FORMAT_PHASES within FORMATS × PHASES: shapes the product
// cannot produce, which each suite's IMPOSSIBLE CELLS block pins (the editors
// trust a mis-stamped phase). Each suite pairs this DERIVED list with an
// EXPLICIT per-cell expectation map — the editors' knockout gates differ (the
// team editor has a format fallback clause the individual editor lacks), so a
// derived expectation cannot be shared; a newly-derived cell instead fails
// loudly until someone pins its expectation deliberately.
export const IMPOSSIBLE_FORMAT_PHASES = FORMATS
  .flatMap(format => PHASES.map(phase => ({ format, phase })))
  .filter(fp => !FORMAT_PHASES.some(p => p.format === fp.format && p.phase === fp.phase));

export const NAGINATA = [false, true];
export const MAX_ENCHO = [0, 2];

// Independent hardcoded expectations for getIpponButtons(isNaginata) — NOT
// computed from the component, so a letter-set change in
// admin_scoring_shared.jsx fails these suites. Exported as Sets, the form the
// suites compare against the rendered buttons.
const KENDO_LETTERS = ['M', 'K', 'D', 'T', 'H'];
const NAGINATA_LETTERS = ['M', 'K', 'D', 'T', 'S', 'H'];
export const KENDO_SET = new Set(KENDO_LETTERS);
export const NAGINATA_SET = new Set(NAGINATA_LETTERS);
