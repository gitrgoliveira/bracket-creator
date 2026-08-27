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
  LABEL_POOL_SIZE, LABEL_POOL_WINNERS, LABEL_EXTRA_QUALIFIERS,
  LABEL_TEAM_SIZE, LABEL_TEAM_MATCH_TYPE, LABEL_ZEKKEN, LABEL_ENGI,
  HINT_ZEKKEN, HINT_ENGI,
  LABEL_NAGINATA, HINT_NAGINATA,
  LABEL_CHECK_IN, HINT_CHECK_IN,
  LABEL_NUMBER_PREFIX, HINT_NUMBER_PREFIX,
} from '../competition_shape.jsx';

const __dirname = dirname(fileURLToPath(import.meta.url));
const read = (f) => readFileSync(resolve(__dirname, '..', f), 'utf8');

// The two screens under comparison, read and comment-stripped ONCE. The
// three describe blocks below previously re-read both files and re-ran
// stripComments over them independently, so the same ~200 KB was walked
// three times per run. Hoisting also guarantees all three blocks are
// judging byte-identical input.
const SETUP_SRC = read('admin_setup.jsx');
const SETTINGS_SRC = read('admin_competition_settings.jsx');

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

// Comment-stripped once each, for the same reason SETUP_SRC/SETTINGS_SRC
// are read once: two describe blocks below both need these and were each
// building their own copy.
const SETUP_STRIPPED = stripComments(SETUP_SRC);
const SETTINGS_STRIPPED = stripComments(SETTINGS_SRC);

// ── Task 1: structural parity guard ─────────────────────────────────────
//
// Parses the `import { ... } from './competition_shape.jsx';` block out of a
// screen's source and returns the raw list of imported names. The block is a
// flat, single-level name list (unlike qualifier_preview.jsx's option-object
// literals), so a comment-strip + comma-split is all extraction needs.
//
// The character class is `[^{}]`, NOT `[\s\S]`, and that is the whole
// correctness of this parser. `[\s\S]*?` is lazy but unanchored, so the match
// began at the FIRST `import {` anywhere above and ran on until it found the
// competition_shape.jsx tail -- swallowing every import statement in between.
// Measured on the real files before the fix: 48 and 58 "names" instead of 39
// and 46, four unrelated modules' imports (pool_ids, duration,
// admin_schedule_utils, qualifier_preview) folded in, and LABEL_KIND -- the
// FIRST real name -- fused into a multi-line garbage token and therefore
// invisible to the parity check below.
//
// It passed anyway, purely because both screens happen to list the same
// imports in the same order, so both were garbled identically. That is luck,
// not a guarantee: reordering imports on ONE screen would have produced a
// wild false failure, and a control-bearing name landing first in the block
// on one screen only would have been silently dropped there and seen on the
// other -- a false FAILURE, or with the sets the other way round, a genuine
// divergence masked. Banning braces from the captured span forces the match
// to start at the right `import {`, because any intervening statement's
// `} from '...'` contains one. The identifier assertion below is what makes
// a future regression of this kind loud instead of silent.
function shapeImportNames(src) {
  const m = src.match(/import\s*\{([^{}]*?)\}\s*from\s*['"]\.\/competition_shape\.jsx['"]/);
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
  const setupNames = shapeImportNames(SETUP_SRC);
  const settingsNames = shapeImportNames(SETTINGS_SRC);

  it('the parser actually found a competition_shape.jsx import on both screens (not a vacuous pass)', () => {
    // Guards the guard: a regex that stops matching (module renamed, import
    // reformatted past what the pattern expects) would silently make every
    // assertion below compare two empty sets and pass on nothing. Same
    // technique as the finalNext parser's "parsed no fields" check.
    expect(setupNames.length, 'admin_setup.jsx: parsed no names from the competition_shape.jsx import; regex broken or import removed').toBeGreaterThan(0);
    expect(settingsNames.length, 'admin_competition_settings.jsx: parsed no names from the competition_shape.jsx import; regex broken or import removed').toBeGreaterThan(0);
  });

  it('every parsed token is a bare identifier, so the parser cannot pass on garbage', () => {
    // A length check alone does NOT guard this guard: the previous regex
    // over-matched across four unrelated import statements and still returned
    // 48/58 non-empty "names", so the check above passed while the first real
    // name was fused into a multi-line blob (see shapeImportNames). Anything
    // that is not a plain identifier means the parser has walked outside the
    // import block it was aiming at, whatever the count says.
    for (const [file, names] of [['admin_setup.jsx', setupNames], ['admin_competition_settings.jsx', settingsNames]]) {
      const bad = names.filter((n) => !/^[A-Za-z_$][\w$]*$/.test(n));
      expect(
        bad,
        `${file}: shapeImportNames returned token(s) that are not bare identifiers, so it matched past the ` +
        `competition_shape.jsx import block and every comparison below is being made on garbage: ` +
        `${JSON.stringify(bad.slice(0, 3))}`
      ).toEqual([]);
    }
  });

  it('parses the first name in the block, which over-matching silently drops', () => {
    // LABEL_KIND is deliberately named rather than checked positionally: it is
    // the first name in both screens' import blocks, and the first name is
    // exactly what an over-matching regex swallows into its garbage token
    // while leaving the rest of the list intact and plausible.
    expect(setupNames, 'admin_setup.jsx: LABEL_KIND missing from the parsed import list').toContain('LABEL_KIND');
    expect(settingsNames, 'admin_competition_settings.jsx: LABEL_KIND missing from the parsed import list').toContain('LABEL_KIND');
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
  // today is imported by BOTH screens (re-verified 2026-08-26 by actually
  // running setupControls/settingsControls above, not by eyeballing the
  // import list: 20 names each, zero difference -- 20 and not 19 because
  // fixing shapeImportNames' over-match restored LABEL_KIND, which the old
  // regex had been swallowing on both screens). Do not add an entry here
  // for convenience or to make a failing test pass -- if a screen is
  // missing a control, give it the same import (and render the control)
  // instead. An entry only belongs here when the asymmetry is inherent to
  // what the screen can even mean: create has no roster and no draw-ready
  // state, settings has both, so a control genuinely tied to either of
  // those (e.g. something gated on isDrawReady) could legitimately have no
  // create-side equivalent. No such control exists yet, hence empty.
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
  const setupSrc = SETUP_STRIPPED;
  const settingsSrc = SETTINGS_STRIPPED;

  // Every LABEL_*/HINT_* string competition_shape.jsx exports (verified
  // against `grep -n '^export const LABEL_\|^export const HINT_'
  // competition_shape.jsx` 2026-08-26: exactly these 21). Wire-value
  // constants (FORMAT_LEAGUE, KIND_TEAM, POOL_FORMAT_FULL, ...) are
  // deliberately excluded: those are expected to appear literally in both
  // screens' own comparison logic (`local.format === "league"` and the
  // like) and are not operator-facing copy, so testing them this way would
  // just be noise. Likewise *_OPTIONS array literals (TEAM_MATCH_TYPE_OPTIONS
  // and friends) are not listed here -- Task 1's structural check above
  // already covers them (they end in `_OPTIONS`, so `isControlBearing`
  // catches a missing import), and their per-option `label` strings are
  // asserted where they are used (e.g. formatHint) rather than restated in
  // this flat list.
  const COPY = {
    LABEL_KIND, LABEL_FORMAT, LABEL_POOL_FORMAT,
    LABEL_SWISS_ROUNDS, HINT_SWISS_ROUNDS,
    LABEL_ROUND_ROBIN,
    LABEL_LEAGUE_TIEBREAK, HINT_LEAGUE_TIEBREAK,
    LABEL_POOL_DURATION, HINT_POOL_DURATION,
    LABEL_PLAYOFF_DURATION, HINT_PLAYOFF_DURATION,
    LABEL_TWO_THIRD_PLACES, HINT_TWO_THIRD_PLACES,
    LABEL_POOL_SIZE, LABEL_POOL_WINNERS, LABEL_EXTRA_QUALIFIERS,
    LABEL_TEAM_SIZE, LABEL_TEAM_MATCH_TYPE, LABEL_ZEKKEN, LABEL_ENGI,
    HINT_ZEKKEN, HINT_ENGI,
    LABEL_NAGINATA, HINT_NAGINATA,
    LABEL_CHECK_IN, HINT_CHECK_IN,
    LABEL_NUMBER_PREFIX, HINT_NUMBER_PREFIX,
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

  // One constant cannot be safely asserted by plain substring inclusion, for
  // a reason other than "convenient to skip":
  //
  //   LABEL_FORMAT ("Format") is a short, common substring of unrelated
  //   identifiers BOTH screens already contain for other reasons --
  //   poolFormat (both), normalizeConfigForFormat/validateCompetitionFormat
  //   (settings only) -- and admin_setup.jsx's CSV batch-import preview
  //   table has its own unrelated `<th>Format</th>` column heading, a
  //   different table naming the same general concept, not a restated
  //   control label. A hit here is not evidence of copy drift on either
  //   screen.
  //
  // LABEL_SWISS_ROUNDS and LABEL_TEAM_SIZE used to be excluded here too, on
  // admin_setup.jsx only: each was a genuine prefix of that screen's own
  // validator message ("Number of Swiss rounds must be a whole number >= 1.",
  // 'Team size must be a whole number.'). Naming the field being validated is
  // what an error message is supposed to do, so those were false positives
  // rather than copy drift -- but the messages now interpolate the constant
  // instead of restating it, which removes the false positive AND makes the
  // message track a copy change. Both constants are checked on both screens
  // again.
  const EXCLUDE_FROM = {
    LABEL_FORMAT: new Set(['setup', 'settings']),
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

  // The check above is only half the rule, and the missing half is the one
  // that actually broke. "Does not re-spell the constant's text" catches a
  // screen that COPIES the shared string; it is blind to a screen that uses
  // a DIFFERENT string instead -- which is exactly what happened to the
  // check-in hint, where create said "...counter for this competition." and
  // settings said "...counter. Disable for competitions that don't need
  // attendance tracking.". Neither screen contained the other's text, so a
  // literal-absence check would have passed on both for as long as the drift
  // survived. Hoisting the copy into competition_shape.jsx creates a single
  // home; this is what makes both screens live in it.
  //
  // Two or more occurrences of the bare name = the import line plus at least
  // one render site. One occurrence = imported and never used, which is the
  // shape a re-inlined literal leaves behind.
  const VIA_HELPER = new Set(['LABEL_POOL_DURATION', 'HINT_POOL_DURATION']);
  for (const name of Object.keys(COPY)) {
    if (VIA_HELPER.has(name)) continue;
    for (const key of ['setup', 'settings']) {
      it(`${FILE_NAMES[key]} actually renders ${name}, not just imports it`, () => {
        const uses = (SOURCES[key].match(new RegExp(`\\b${name}\\b`, 'g')) || []).length;
        expect(
          uses,
          `${FILE_NAMES[key]} names ${name} ${uses} time(s): it must import the constant AND render ` +
          'it at least once. One occurrence means the import is there but the control is drawing its ' +
          'copy from somewhere else, which is how two screens drift while owning a shared constant.'
        ).toBeGreaterThan(1);
      });
    }
  }
});

// ── Task 3: control ORDER parity (bc-symm Phase 6) ──────────────────────
//
// The two describe blocks above guard the control SET (Task 1: every
// LABEL_*/_OPTIONS name competition_shape.jsx exports is imported by both
// screens) and the control COPY (Task 2: no screen re-spells a shared
// label/hint inline). Neither can see ORDER: a screen that imports
// LABEL_LEAGUE_TIEBREAK passes both checks whether the control it labels
// renders first on the page or dead last. Order is a separate property,
// and until this block nothing asserted it -- which is exactly how
// admin_competition_settings.jsx ended up rendering League's own two
// settings ("Break ties for top", "Award two joint 3rd places") fifteen
// positions below Format: choosing League on the Settings screen showed
// the operator nothing about what League needed until they had scrolled
// past Team size, Team match type, Pool format, Round-robin, Pool size
// mode, Pool size, Pool winners, Extra qualifiers, Swiss rounds, Assigned
// shiaijo, both match durations, Player number prefix and all four
// competition-option checkboxes -- fourteen unrelated controls between the
// format pick and the settings it unlocked. bc-symm reordered both screens
// to one dependency-first sequence (the two shape-gating controls first,
// then each dependent group beside its gate) specifically to close that
// gap. This block is what keeps a future edit from silently reopening it
// on one screen while the other stays fixed.
//
// Deliberately does NOT hardcode the canonical group order as the
// assertion below -- it asserts the two screens AGREE WITH EACH OTHER, by
// comparing each one's own rendered sequence against the other's. A future
// deliberate reorder only has to be applied to both screens; it never also
// has to be transcribed into a third, hand-maintained list here that could
// itself drift from what actually shipped (the exact failure class Task 1's
// own header commentary warns about for shapeImportNames' over-match: a
// guard that silently compares less than it claims).
describe('competition CREATE vs SETTINGS: control ORDER parity (bc-symm Phase 6)', () => {
  const setupSrc = SETUP_STRIPPED;
  const settingsSrc = SETTINGS_STRIPPED;

  // Each marker names one control (in the canonical dependency-first order,
  // for readability only -- see the header above for why that order is
  // never itself asserted) and a pattern that locates its LABEL RENDER
  // SITE, not just any mention of the control. Patterns are matched
  // against a COMMENT-STRIPPED source specifically because this file's own
  // doc comments quote LABEL_* tokens verbatim (e.g. CLEARED_FIELD_LABELS'
  // header in admin_competition_settings.jsx) -- on the raw source, a
  // marker built from the same token the reorder's own commentary
  // discusses would silently count a sentence about the control as a
  // second render site instead of the render site moving.
  const MARKERS = [
    { key: 'Display name', pattern: '>Display name<' },
    { key: 'Day', pattern: '>Day<' },
    { key: 'Start time', pattern: '>Start time<' },
    { key: 'Competition type (Kind)', pattern: '{LABEL_KIND}' },
    { key: 'Format', pattern: '{LABEL_FORMAT}' },
    // Anchored as a JSX text node (`>{...}<`) rather than the bare
    // `{LABEL_TEAM_SIZE}`: create()'s validator now names the field
    // through the same constant (`${LABEL_TEAM_SIZE} must be a whole
    // number.`), which contains the bare form as a substring and would
    // make this marker match twice. The label render site is the one
    // that defines where the control sits, so that is what we anchor to.
    { key: 'Team size', pattern: '>{LABEL_TEAM_SIZE}<' },
    { key: 'Team match type', pattern: '{LABEL_TEAM_MATCH_TYPE}' },
    { key: 'Pool format (round-robin shape)', pattern: '{LABEL_POOL_FORMAT}' },
    { key: 'Round-robin (round-robin in pools)', pattern: '{LABEL_ROUND_ROBIN}' },
    { key: 'Pool size mode ("Pool size is a")', pattern: '{LABEL_POOL_SIZE_MODE}' },
    { key: 'Pool size', pattern: '{LABEL_POOL_SIZE}' },
    { key: 'Pool winners', pattern: '{LABEL_POOL_WINNERS}' },
    { key: 'Extra qualifiers', pattern: '{LABEL_EXTRA_QUALIFIERS}' },
    { key: 'Swiss rounds', pattern: '>{LABEL_SWISS_ROUNDS}<' },
    { key: 'League tiebreak', pattern: '{LABEL_LEAGUE_TIEBREAK}' },
    { key: 'Two joint 3rd places', pattern: '{LABEL_TWO_THIRD_PLACES}' },
    { key: 'Assigned shiaijo (courts)', pattern: '>Assigned shiaijo (courts)<' },
    // Pool duration's <label> is built by a shared helper on each screen
    // (admin_setup.jsx inlines the JSX; admin_competition_settings.jsx
    // calls its own durationField()), so the one substring both render
    // sites share verbatim is the poolDurationLabel(...) CALL -- only its
    // argument differs (`format` vs `local.format`). The
    // competition_shape.jsx import list also names the function, but only
    // as a bare `poolDurationLabel,`, never followed by `(`, so the open
    // paren is what keeps the import line from matching.
    { key: 'Pool duration', pattern: 'poolDurationLabel(' },
    // Playoff duration's two screens render through genuinely different
    // shapes: admin_setup.jsx as an inline `{LABEL_PLAYOFF_DURATION}` JSX
    // expression, admin_competition_settings.jsx as a bare argument to
    // durationField(...). One literal string cannot name both render
    // sites, so this is the file's one regex marker: it alternates between
    // the two call shapes. Neither arm matches the competition_shape.jsx
    // import line (`LABEL_PLAYOFF_DURATION, HINT_PLAYOFF_DURATION,`),
    // which has no `{`/`}` around the bare name and is never itself a
    // `durationField(...)` call.
    { key: 'Playoff duration', pattern: /\{LABEL_PLAYOFF_DURATION\}|durationField\(LABEL_PLAYOFF_DURATION,/ },
    { key: 'Player number prefix', pattern: '{LABEL_NUMBER_PREFIX}' },
    { key: 'Zekken (participant CSV column)', pattern: '{LABEL_ZEKKEN}' },
    { key: 'Engi (kata pairs)', pattern: '{LABEL_ENGI}' },
    // Naginata and Check-in DID have no LABEL_* constant, so these two
    // markers used to anchor on the raw label markup and Task 2 above could
    // not check their copy at all. That blind spot was not hypothetical:
    // the two screens' check-in HINTS had already drifted apart under it.
    // Both controls now own their copy in competition_shape.jsx, so the
    // markers anchor on the constants like every other entry here, and
    // Task 2 covers the hints for free.
    { key: 'Naginata competition', pattern: '{LABEL_NAGINATA}' },
    { key: 'Check-in tracking', pattern: '{LABEL_CHECK_IN}' },
  ];

  it('the marker list is non-empty (not a vacuous pass)', () => {
    expect(MARKERS.length).toBeGreaterThan(0);
  });

  // Every match START INDEX for `pattern` in `src`, string or regex alike.
  // Returns an array (not a boolean or a count) so both "exactly once" and
  // "the one position" can be read off the same call: a length !== 1 is the
  // ambiguous/missing case the per-marker checks below reject, and
  // idxs[0] is the position the order comparison sorts by.
  function occurrences(src, pattern) {
    const idxs = [];
    if (typeof pattern === 'string') {
      let from = 0;
      for (;;) {
        const i = src.indexOf(pattern, from);
        if (i === -1) break;
        idxs.push(i);
        from = i + 1;
      }
      return idxs;
    }
    const flags = pattern.flags.includes('g') ? pattern.flags : pattern.flags + 'g';
    const re = new RegExp(pattern.source, flags);
    let m;
    while ((m = re.exec(src))) {
      idxs.push(m.index);
      if (m[0].length === 0) re.lastIndex++; // guard a zero-width pattern from looping forever
    }
    return idxs;
  }

  const setupOcc = {};
  const settingsOcc = {};
  for (const { key, pattern } of MARKERS) {
    setupOcc[key] = occurrences(setupSrc, pattern);
    settingsOcc[key] = occurrences(settingsSrc, pattern);
  }

  // One assertion per marker per screen, exactly as Task 1's header
  // commentary demands of this whole file's approach: a pattern that
  // matches zero times (the control's label was renamed, or moved into a
  // shared helper this marker doesn't know about) or more than once (the
  // pattern was too loose and is now also matching a comment, an unrelated
  // control, or its own import) must fail LOUDLY, naming the marker and
  // the actual count -- never silently drop the control from the order
  // comparison below, which is the same class of bug as shapeImportNames'
  // original over-match: a guard that quietly compares less than it claims.
  for (const { key } of MARKERS) {
    it(`"${key}": render site appears exactly once in admin_setup.jsx`, () => {
      const n = setupOcc[key].length;
      expect(
        n,
        `admin_setup.jsx: marker "${key}" matched ${n} time(s) (expected exactly 1). ` +
        (n === 0
          ? 'The control\'s render site moved, was renamed, or the marker pattern is stale.'
          : 'The pattern is ambiguous -- tighten it so it can only match this control\'s own label render site.')
      ).toBe(1);
    });
    it(`"${key}": render site appears exactly once in admin_competition_settings.jsx`, () => {
      const n = settingsOcc[key].length;
      expect(
        n,
        `admin_competition_settings.jsx: marker "${key}" matched ${n} time(s) (expected exactly 1). ` +
        (n === 0
          ? 'The control\'s render site moved, was renamed, or the marker pattern is stale.'
          : 'The pattern is ambiguous -- tighten it so it can only match this control\'s own label render site.')
      ).toBe(1);
    });
  }

  it('every marker was located on both screens (not a vacuous pass)', () => {
    // Belt-and-braces summary of the per-marker checks above: if the loop
    // that generated them ever stopped running (a refactor that hoists
    // MARKERS out of scope, a typo that empties the loop body), THIS check
    // still catches a marker silently missing from one screen instead of
    // the suite quietly having fewer assertions than it claims.
    const missingSetup = MARKERS.filter((m) => setupOcc[m.key].length !== 1).map((m) => m.key);
    const missingSettings = MARKERS.filter((m) => settingsOcc[m.key].length !== 1).map((m) => m.key);
    expect(missingSetup, `admin_setup.jsx: marker(s) not found exactly once: ${missingSetup.join(', ')}`).toEqual([]);
    expect(missingSettings, `admin_competition_settings.jsx: marker(s) not found exactly once: ${missingSettings.join(', ')}`).toEqual([]);
  });

  it('both screens render the shared controls in the SAME RELATIVE ORDER', () => {
    // Restricted to markers unambiguously found on BOTH screens: a marker
    // that failed the "exactly once" checks above is already reported
    // there by name; folding its missing/duplicate position into a sort
    // here would either crash on an empty array or sort on a meaningless
    // first-of-several index, producing a confusing order mismatch that
    // hides the real (locate) failure instead of pointing at it.
    const usable = MARKERS.filter((m) => setupOcc[m.key].length === 1 && settingsOcc[m.key].length === 1);

    const bySetupPosition = (a, b) => setupOcc[a.key][0] - setupOcc[b.key][0];
    const bySettingsPosition = (a, b) => settingsOcc[a.key][0] - settingsOcc[b.key][0];
    const setupOrder = [...usable].sort(bySetupPosition).map((m) => m.key);
    const settingsOrder = [...usable].sort(bySettingsPosition).map((m) => m.key);

    // The two sequences are the WHOLE assertion -- no canonical list is
    // compared against either one (see this describe block's header). A
    // mismatch prints both orders in full so a maintainer can see exactly
    // which control(s) moved on which screen, rather than a bare
    // pass/fail that would send them back to diff the two files by hand.
    expect(
      settingsOrder,
      'admin_setup.jsx and admin_competition_settings.jsx render the shared controls in a ' +
      'DIFFERENT relative order.\n' +
      `  admin_setup.jsx order:               ${JSON.stringify(setupOrder)}\n` +
      `  admin_competition_settings.jsx order: ${JSON.stringify(settingsOrder)}\n` +
      'Both screens must render every shared control in the same sequence. Move the control(s) ' +
      'that differ to match the other screen -- do not special-case an exception here.'
    ).toEqual(setupOrder);
  });
});

// bc-symm-settings-create-parity, review round: the rules this module owns
// have to be ASKED, not re-derived at a call site. A respelling renders
// identically to a call, which is exactly why this is a source-text check --
// see this file's header for the full rationale.
//
// Both classes below were live in the shipped diff. The create form gated
// its zekken and engi RENDER sites on zekkenApplies/engiApplies while its
// PUT payload, two and eighty lines away, still spelled
// `kind === "individual"` by hand -- so the pair would have parted company
// the moment the kind rule gained a third value (the server already accepts
// "" as a legal kind). And the settings screen respelled optionLabel's
// `.find(...)?.label` twice, ten lines from formatClearedValue which calls
// the real thing.
describe('competition CREATE vs SETTINGS: shared rules are asked, not re-derived', () => {
  // Both screens import these predicates already, so a bare comparison
  // against the wire value is always a respelling rather than a missing
  // helper.
  const RESPELLINGS = [
    {
      pattern: /kind\s*===\s*["']individual["']/g,
      rule: 'zekkenApplies(kind) / engiApplies(kind) / teamFieldsVisible(kind)',
      why: 'the kind rule has one owner in competition_shape.jsx, and both screens already import it for their render gates',
    },
    {
      pattern: /format\s*===\s*["']mixed["']/g,
      rule: 'FORMAT_MIXED',
      why: 'the wire value is an exported constant; a literal is the copy that stops following when the constant moves',
    },
    {
      pattern: /poolSizeMode\s*===\s*["'](max|min)["']/g,
      rule: 'resolvePoolSizeMode + POOL_SIZE_MODE_MAX / POOL_SIZE_MODE_MIN',
      why: 'the stored field has a third state ("") the pills do not, and a bare equality lights neither pill for it',
    },
    {
      pattern: /_OPTIONS\.find\(\s*\(\s*o\s*\)\s*=>\s*o\.value\s*===/g,
      rule: 'optionLabel(options, value)',
      why: "optionLabel is the one option-list lookup in this codebase, so the .find shape and its ?? fallback are not respelled per call site",
    },
  ];

  const SCREENS = [
    ['admin_setup.jsx', SETUP_STRIPPED],
    ['admin_competition_settings.jsx', SETTINGS_STRIPPED],
  ];

  // A guard that can only ever pass is not a guard. Prove each pattern
  // actually matches the shape it is written to catch, against text this
  // test controls.
  it('every pattern matches its own respelling (not a vacuous pass)', () => {
    const SAMPLES = [
      'const x = kind === "individual" ? a : b;',
      'if (format === "mixed") { emit(); }',
      'const lit = poolSizeMode === "max";',
      'FORMAT_OPTIONS.find((o) => o.value === format)?.label',
    ];
    RESPELLINGS.forEach((r, i) => {
      expect(
        new RegExp(r.pattern.source).test(SAMPLES[i]),
        `pattern for ${r.rule} does not match its own sample: ${SAMPLES[i]}`
      ).toBe(true);
    });
  });

  // The markup half of the same rule. Sharing the CONSTANTS stopped the two
  // screens disagreeing about what a control is called; it did nothing about
  // the `<div className="field"> <label> <div className="radio-group">` each
  // screen hand-wrote around them, and four divergences lived in exactly that
  // gap (see competition_fields.jsx's header). Both screens now render every
  // shared option-list control through PillGroup and every shared checkbox
  // through CheckboxField, so a new hand-rolled one is the regression to
  // catch.
  //
  // The two allowances are real controls that are NOT shared: the
  // "Knockout qualifiers" radio (each pill carries its own coupling side
  // effect on poolWinners, and its preview line differs by surface) and the
  // shiaijo picker (driven by window.courtPillOptions, whose flagged-orphan
  // state exists only on the settings screen). Both are one `radio-group`
  // each, which is what the count pins -- add a third and this fails until
  // it is either shared or deliberately allowed here.
  it('renders every shared control through the shared field components', () => {
    for (const [name, src] of SCREENS) {
      const rawGroups = (src.match(/className="radio-group"/g) || []).length;
      expect(
        rawGroups,
        `${name} hand-writes ${rawGroups} radio-group(s); only 2 are sanctioned ` +
        '(Knockout qualifiers, Assigned shiaijo). Render option-list controls ' +
        'with <PillGroup> from competition_fields.jsx instead.'
      ).toBe(2);

      const rawCheckboxes = (src.match(/<label className="checkbox">/g) || []).length;
      expect(
        rawCheckboxes,
        `${name} hand-writes ${rawCheckboxes} checkbox field(s). Render them with ` +
        '<CheckboxField> from competition_fields.jsx, which owns the label-to-hint ' +
        'gap the two screens used to spell differently.'
      ).toBe(0);

      expect(
        /from '\.\/competition_fields\.jsx'/.test(src),
        `${name} must import the shared field components`
      ).toBe(true);
    }
  });

  // Both screens must ask the same question about which team-match-format
  // pill is lit. They did not: settings read anything-but-kachinuki as
  // Regular (correct for a stored legacy value) while create used plain
  // equality and carried a comment explaining why that was safe for its own
  // state. Two spellings of one rule is how the next reader picks the wrong
  // one.
  it('both screens light the team-match-format pill by the same predicate', () => {
    for (const [name, src] of SCREENS) {
      expect(
        /teamMatchTypeActive\(/.test(src),
        `${name} must use teamMatchTypeActive rather than spelling the ` +
        'fixed-vs-kachinuki comparison inline'
      ).toBe(true);
    }
  });

  // The naginata coupling: ticking "Naginata competition" clears "Award two
  // joint 3rd places", because naginata awards a single 3rd. The create form
  // has always done this inline; the settings screen did not, so setting the
  // discipline on the screen built for editing left the kendo convention in
  // place.
  it('both screens apply the naginata / joint-3rd-places coupling', () => {
    for (const [name, src] of SCREENS) {
      expect(
        /twoThirdPlacesForNaginata\(/.test(src),
        `${name} must clear the joint-3rd-places setting when naginata is ` +
        'ticked, via the shared twoThirdPlacesForNaginata rule'
      ).toBe(true);
    }
  });

  it('neither screen re-derives a rule competition_shape.jsx already owns', () => {
    for (const [name, src] of SCREENS) {
      for (const { pattern, rule, why } of RESPELLINGS) {
        const hits = src.match(new RegExp(pattern.source, 'g')) || [];
        expect(
          hits,
          `${name} spells out a rule that ${rule} owns (${hits.length} time(s): ${hits.join(', ')}). ` +
          `Call ${rule} instead -- ${why}.`
        ).toEqual([]);
      }
    }
  });
});

// bc-symm review round: the rule that explains why "Award two joint 3rd
// places" was missing from disk, written down where it can fail.
//
// A `bool` with `omitempty` cannot record FALSE. Go drops it from the JSON
// and the YAML, so an explicit false and a field nobody ever set are the
// same bytes, and every later reader sees the zero value. That is fine, and
// keeps config.md clean, for exactly as long as the CREATE FORM's default
// for that control is also false -- then "absent" and "the default" agree
// and nothing is lost.
//
// LeagueTwoThirdPlaces was the one field where they disagreed: the create
// form defaults it to TRUE (two joint 3rds is the standard kendo
// convention; naginata is the exception), so a competition that had never
// been a league carried no value, read back as false, and the settings
// screen's Format editor -- which can now make any competition a league --
// showed the naginata convention on a kendo competition.
//
// RoundRobin is the proof that the pattern was already understood: it also
// defaults to true, and it is the one bool in the struct that deliberately
// carries no omitempty. LeagueTwoThirdPlaces now matches it.
//
// This reads the Go struct directly rather than mirroring it, because a
// mirror is the thing that drifts. Reading both sides means the test fails
// on a change to EITHER: flip a create-form default to true, or add
// omitempty to a field whose default is true, and it goes red.
describe('competition CREATE defaults vs the Go wire tags', () => {
  const MODELS_GO = readFileSync(
    resolve(__dirname, '..', '..', '..', 'internal', 'state', 'models.go'), 'utf8');

  // JS create-form state variable -> the state.Competition field it becomes.
  // Explicit because the names differ (withZekken/WithZekkenName) and because
  // a rename should fail this test loudly rather than skip a field silently.
  const BOOL_FIELDS = {
    roundRobin: 'RoundRobin',
    leagueTwoThirdPlaces: 'LeagueTwoThirdPlaces',
    naginata: 'Naginata',
    engi: 'Engi',
    checkInEnabled: 'CheckInEnabled',
    withZekken: 'WithZekkenName',
  };

  const goFieldOmitsEmpty = (field) => {
    const m = MODELS_GO.match(new RegExp(`\\n\\t${field}\\s+bool\\s+\`([^\`]*)\``));
    if (!m) return null;
    return m[1].includes('omitempty');
  };

  const createDefault = (stateVar) => {
    const m = SETUP_SRC.match(
      new RegExp(`const \\[${stateVar}, set\\w+\\] = useStateA\\((true|false)\\)`));
    return m ? m[1] === 'true' : null;
  };

  it('found every mapped field on both sides (not a vacuous pass)', () => {
    for (const [stateVar, goField] of Object.entries(BOOL_FIELDS)) {
      expect(createDefault(stateVar), `create form has no boolean useStateA seed for "${stateVar}"`).not.toBeNull();
      expect(goFieldOmitsEmpty(goField), `state.Competition has no bool field "${goField}"`).not.toBeNull();
    }
  });

  it('a control the create form defaults to TRUE is never omitempty on the wire', () => {
    for (const [stateVar, goField] of Object.entries(BOOL_FIELDS)) {
      if (createDefault(stateVar) !== true) continue;
      expect(
        goFieldOmitsEmpty(goField),
        `${stateVar} defaults to true on the create form, but state.Competition.${goField} ` +
        'is omitempty -- so an explicit false is dropped from the wire and reads back as ' +
        'false, while a competition created through the form reads back true. The two ' +
        'config surfaces then disagree about the same setting depending on which one the ' +
        'operator used. Drop omitempty (RoundRobin is the precedent), or default the ' +
        'control to false.'
      ).toBe(false);
    }
  });

  it('an omitempty bool is only safe because its create default is the zero value', () => {
    // The converse, stated so the safe cases are safe ON PURPOSE rather than
    // by luck: every field that DOES keep omitempty must default to false.
    for (const [stateVar, goField] of Object.entries(BOOL_FIELDS)) {
      if (!goFieldOmitsEmpty(goField)) continue;
      expect(
        createDefault(stateVar),
        `state.Competition.${goField} is omitempty, so it cannot record false; that is ` +
        `only lossless while ${stateVar} also defaults to false on the create form.`
      ).toBe(false);
    }
  });
});
