import { describe, it, expect } from 'vitest';
import { readFileSync } from 'fs';
import { resolve, dirname } from 'path';
import { fileURLToPath } from 'url';
import {
  LABEL_KIND, LABEL_FORMAT, LABEL_POOL_FORMAT,
  LABEL_SWISS_ROUNDS, HINT_SWISS_ROUNDS,
  LABEL_ROUND_ROBIN,
  LABEL_LEAGUE_TIEBREAK, HINT_LEAGUE_TIEBREAK,
  LABEL_POOL_DURATION, HINT_POOL_DURATION,
  LABEL_PLAYOFF_DURATION, HINT_PLAYOFF_DURATION,
  LABEL_TWO_THIRD_PLACES, HINT_TWO_THIRD_PLACES,
} from '../competition_shape.jsx';

const __dirname = dirname(fileURLToPath(import.meta.url));
const read = (f) => readFileSync(resolve(__dirname, '..', f), 'utf8');

// bc-symm Phase 5: the operator ruling behind competition_shape.jsx (see its
// header) is that the competition CREATE form (admin_setup.jsx's
// AdminCreateCompetition) and the competition SETTINGS page
// (admin_competition_settings.jsx's AdminSettings) must offer the SAME
// controls with the SAME copy. Phases 2-4 made that true by extracting every
// option list, label and hint into competition_shape.jsx and having both
// screens import from it. This file is the guard that keeps it true: without
// it, a later edit to either screen could add a control (or re-inline a
// label) the other lacks, and nothing would fail until an operator noticed
// the two screens disagreeing.
//
// Both describe blocks below read the two screens as SOURCE TEXT rather than
// importing and rendering the components. That is deliberate, not a
// shortcut: rendering only shows what a given fixture happens to produce in
// this run, not where the text came from. A screen that hardcodes a copy of
// "Format" right next to a genuine {LABEL_FORMAT} would render identically to
// one that removed the duplicate -- only reading the source can tell the two
// apart. Same rationale as admin_competition.test.jsx's `finalNext` allowlist
// parser (~line 416) and qualifier_preview.test.jsx's "radio copy has a
// single home" describe block, which this file's structure and voice follow.

function stripComments(s) {
  return s.replace(/\/\*[\s\S]*?\*\//g, '').replace(/\/\/[^\n]*/g, '');
}

// ── Task 1: structural parity guard ─────────────────────────────────────
//
// Parses the `import { ... } from './competition_shape.jsx';` block out of a
// screen's source and returns the raw list of imported names. The block is a
// flat, single-level name list (unlike qualifier_preview.jsx's option-object
// literals), so a comment-strip + comma-split is all extraction needs.
function shapeImportNames(src) {
  const m = src.match(/import\s*\{([\s\S]*?)\}\s*from\s*['"]\.\/competition_shape\.jsx['"]/);
  if (!m) return [];
  return stripComments(m[1])
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean);
}

// "Control-bearing": an option list (*_OPTIONS) or a field label (LABEL_*) --
// the things a screen actually RENDERS as a control. Deliberately narrower
// than the full import list: coupling functions like poolFormatVisible or
// teamFieldsVisible express a RULE ("does this field apply"), and the two
// screens are free to consume the same rule through different call sites
// (see zekkenApplies/engiApplies's own doc comment on that split) without
// that being a parity concern in itself -- if a rule diverges, the CONTROL it
// gates will be the one missing, and this check catches that directly.
// HINT_* is likewise not control-bearing on its own; Task 2 below covers
// hint-copy drift by asserting on every exported HINT_* value regardless of
// whether it is control-bearing.
const isControlBearing = (name) => name.startsWith('LABEL_') || name.endsWith('_OPTIONS');

describe('competition CREATE vs SETTINGS: control parity (bc-symm Phase 5)', () => {
  const setupNames = shapeImportNames(read('admin_setup.jsx'));
  const settingsNames = shapeImportNames(read('admin_competition_settings.jsx'));

  it('the parser actually found a competition_shape.jsx import on both screens (not a vacuous pass)', () => {
    // Guards the guard: a regex that stops matching (module renamed, import
    // reformatted past what the pattern expects) would silently make every
    // assertion below compare two empty sets and pass on nothing. Same
    // technique as the finalNext parser's "parsed no fields" check.
    expect(setupNames.length, 'admin_setup.jsx: parsed no names from the competition_shape.jsx import; regex broken or import removed').toBeGreaterThan(0);
    expect(settingsNames.length, 'admin_competition_settings.jsx: parsed no names from the competition_shape.jsx import; regex broken or import removed').toBeGreaterThan(0);
  });

  const setupControls = new Set(setupNames.filter(isControlBearing));
  const settingsControls = new Set(settingsNames.filter(isControlBearing));

  it('found at least one control-bearing name on each screen (not a vacuous pass)', () => {
    expect(setupControls.size).toBeGreaterThan(0);
    expect(settingsControls.size).toBeGreaterThan(0);
  });

  // Any entry here is a DELIBERATE, JUSTIFIED asymmetry in the two screens'
  // control set. Left EMPTY on purpose: the operator ruling is full two-way
  // parity, and every control-bearing name competition_shape.jsx exports
  // today is imported by BOTH screens (verified 2026-08-25: 12 names each,
  // zero difference). Do not add an entry here for convenience or to make a
  // failing test pass -- if a screen is missing a control, give it the same
  // import (and render the control) instead. An entry only belongs here when
  // the asymmetry is inherent to what the screen can even mean: create has
  // no roster and no draw-ready state, settings has both, so a control
  // genuinely tied to either of those (e.g. something gated on isDrawReady)
  // could legitimately have no create-side equivalent. No such control
  // exists yet, hence empty.
  const ALLOWED_DIVERGENCE = new Set([]);

  it('every control-bearing import competition_shape.jsx offers is used by BOTH screens', () => {
    const onlySetup = [...setupControls].filter((n) => !settingsControls.has(n) && !ALLOWED_DIVERGENCE.has(n));
    const onlySettings = [...settingsControls].filter((n) => !setupControls.has(n) && !ALLOWED_DIVERGENCE.has(n));

    expect(
      onlySetup,
      `admin_setup.jsx imports control(s) admin_competition_settings.jsx does not: ${onlySetup.join(', ')}. ` +
      'Either give admin_competition_settings.jsx the same import (and render the control), or add a justified ' +
      'entry to ALLOWED_DIVERGENCE above naming why the settings page can never offer it.'
    ).toEqual([]);

    expect(
      onlySettings,
      `admin_competition_settings.jsx imports control(s) admin_setup.jsx does not: ${onlySettings.join(', ')}. ` +
      'Either give admin_setup.jsx the same import (and render the control), or add a justified entry to ' +
      'ALLOWED_DIVERGENCE above naming why the create form can never offer it.'
    ).toEqual([]);
  });
});

// ── Task 2: copy-drift guard ─────────────────────────────────────────────
//
// Precedent: qualifier_preview.test.jsx's "radio copy has a single home"
// describe block, same failure shape it exists for. A screen that re-spells a
// shared label/hint INLINE compiles fine, renders identically to the shared
// version today, and then silently drifts the first time only one of the two
// copies gets edited -- the render tests that pin visible text would stay
// green either way, because both still show *some* text. Importing the
// constants here (rather than re-typing the strings) means this test follows
// a copy change automatically instead of needing a second hand-transcription
// that could itself go stale.
describe('competition CREATE vs SETTINGS: copy has a single home (bc-symm Phase 5)', () => {
  const setupSrc = stripComments(read('admin_setup.jsx'));
  const settingsSrc = stripComments(read('admin_competition_settings.jsx'));

  // Every LABEL_*/HINT_* string competition_shape.jsx exports (verified
  // against `grep -n '^export const LABEL_\|^export const HINT_'
  // competition_shape.jsx` 2026-08-25: exactly these 14). Wire-value
  // constants (FORMAT_LEAGUE, KIND_TEAM, POOL_FORMAT_FULL, ...) are
  // deliberately excluded: those are expected to appear literally in both
  // screens' own comparison logic (`local.format === "league"` and the
  // like) and are not operator-facing copy, so testing them this way would
  // just be noise.
  const COPY = {
    LABEL_KIND, LABEL_FORMAT, LABEL_POOL_FORMAT,
    LABEL_SWISS_ROUNDS, HINT_SWISS_ROUNDS,
    LABEL_ROUND_ROBIN,
    LABEL_LEAGUE_TIEBREAK, HINT_LEAGUE_TIEBREAK,
    LABEL_POOL_DURATION, HINT_POOL_DURATION,
    LABEL_PLAYOFF_DURATION, HINT_PLAYOFF_DURATION,
    LABEL_TWO_THIRD_PLACES, HINT_TWO_THIRD_PLACES,
  };

  it('every listed constant is still a non-trivial string export (not a vacuous pass)', () => {
    // If competition_shape.jsx ever drops one of these exports, the named
    // import above resolves to `undefined` rather than throwing (ESM named
    // imports are not checked by esbuild transpile-only or vitest -- see
    // check-imports.mjs's own header), which would silently exclude that
    // constant from every assertion below instead of failing loudly.
    for (const [name, value] of Object.entries(COPY)) {
      expect(typeof value, `${name} is not a string export of competition_shape.jsx any more; update the COPY list above`).toBe('string');
      expect(value.length, `${name} is an empty string; nothing to assert`).toBeGreaterThan(0);
    }
  });

  // Two constants cannot be safely asserted by plain substring inclusion,
  // each for a different reason than "convenient to skip" (per-file, not
  // blanket, so the check stays as strong as it can be on the file where the
  // false positive does not occur):
  //
  //   LABEL_FORMAT ("Format") is a short, common substring of unrelated
  //   identifiers BOTH screens already contain for other reasons --
  //   poolFormat, updateFormat, normalizeConfigForFormat,
  //   validateCompetitionFormat -- and admin_setup.jsx's CSV batch-import
  //   preview table has its own unrelated `<th>Format</th>` column heading, a
  //   different table naming the same general concept, not a restated
  //   control label. A hit here is not evidence of copy drift on either
  //   screen.
  //
  //   LABEL_SWISS_ROUNDS ("Number of Swiss rounds") is a genuine prefix of a
  //   DIFFERENT string in admin_setup.jsx only: validateSwissRounds's error
  //   message reads "Number of Swiss rounds must be a whole number ≥ 1." --
  //   naming the field being validated is what an error message is supposed
  //   to do, not a second copy of the field's <label>. Settings has no such
  //   validator string, so it stays checked.
  const EXCLUDE_FROM = {
    LABEL_FORMAT: new Set(['setup', 'settings']),
    LABEL_SWISS_ROUNDS: new Set(['setup']),
  };

  const SOURCES = { setup: setupSrc, settings: settingsSrc };
  const FILE_NAMES = { setup: 'admin_setup.jsx', settings: 'admin_competition_settings.jsx' };

  for (const [name, value] of Object.entries(COPY)) {
    const excluded = EXCLUDE_FROM[name] || new Set();
    for (const key of ['setup', 'settings']) {
      if (excluded.has(key)) continue;
      it(`${FILE_NAMES[key]} does not re-spell ${name} inline`, () => {
        expect(
          SOURCES[key].includes(value),
          `${FILE_NAMES[key]} contains the literal string competition_shape.jsx's ${name} owns ` +
          `(${JSON.stringify(value)}): render it via the imported constant instead, or the two ` +
          'screens can drift apart the next time only one copy is edited.'
        ).toBe(false);
      });
    }
  }
});
