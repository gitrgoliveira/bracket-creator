import { describe, it, expect } from 'vitest';
import {
  deriveCompetitionName, validatePoolSettings, validateSwissSettings,
  findStackCandidates,
  previewRowCourts, previewRowShiaijoError, importRowErrorText,
} from '../admin_setup.jsx';
import { normalizeTheme } from '../admin_branding.jsx';

describe('findStackCandidates', () => {
  it('returns no candidates when form date is blank, even when another competition has a blank date', () => {
    const comps = [
      { id: 'c1', date: '', startTime: '09:00', courts: ['A'] },
      { id: 'c2', date: '   ', startTime: '10:00', courts: ['A'] },
    ];
    expect(findStackCandidates(comps, '', ['A'], ['A'])).toEqual([]);
    expect(findStackCandidates(comps, '   ', ['A'], ['A'])).toEqual([]);
  });

  it('matches trimmed non-empty dates', () => {
    const comps = [
      { id: 'c1', date: ' 01-07-2026 ', startTime: '09:00', courts: ['A'] },
    ];
    expect(findStackCandidates(comps, '01-07-2026', ['A'], ['A'])).toHaveLength(1);
  });
});

describe('findStackCandidates: which competitions the new one stacks after', () => {
  const day = '01-07-2026';
  const ids = (list) => list.map((c) => c.id);

  it('returns none when there are no competitions', () => {
    expect(findStackCandidates([], day)).toEqual([]);
    expect(findStackCandidates(undefined, day)).toEqual([]);
  });

  it('leaves out competitions on another day', () => {
    expect(findStackCandidates([{ id: 'a', date: '02-07-2026', startTime: '09:00' }], day)).toEqual([]);
  });

  it('keeps every same-day competition when no courts are selected', () => {
    const comps = [
      { id: 'a', date: day, startTime: '09:00', courts: ['A'] },
      { id: 'b', date: day, startTime: '10:00', courts: ['B'] },
    ];
    expect(ids(findStackCandidates(comps, day))).toEqual(['a', 'b']);
  });

  it('keeps only same-day competitions sharing a selected court', () => {
    const comps = [
      { id: 'a', date: day, startTime: '09:00', courts: ['A'] },
      { id: 'b', date: day, startTime: '10:00', courts: ['B'] },
      { id: 'ab', date: day, startTime: '11:00', courts: ['A', 'B'] },
    ];
    expect(ids(findStackCandidates(comps, day, ['A'], ['A', 'B']))).toEqual(['a', 'ab']);
  });

  it('reads a competition with no courts as using every tournament court', () => {
    const comps = [{ id: 'all', date: day, startTime: '09:00', courts: [] }];
    expect(ids(findStackCandidates(comps, day, ['B'], ['A', 'B']))).toEqual(['all']);
    expect(findStackCandidates(comps, day, ['C'], ['A', 'B'])).toEqual([]);
  });

  it('drops competitions with a malformed or missing start time', () => {
    // A legacy/imported competition with an unparseable StartTime must not
    // stack the new one after it; addMinutes() would emit "NaN:NaN".
    const comps = [
      { id: 'good', date: day, startTime: '09:00' },
      { id: 'bad', date: day, startTime: 'garbage' },
      { id: 'empty', date: day, startTime: '' },
      { id: 'none', date: day, startTime: null },
    ];
    expect(ids(findStackCandidates(comps, day))).toEqual(['good']);
  });
});

describe('deriveCompetitionName', () => {
  // Copilot round-8 finding: AdminCreateCompetition.create used
  //   const finalName = name || <kind default>
  // where `name` was the raw input. A whitespace-only string ("   ") is
  // truthy, so it would slip past the default-fallback and be sent to the
  // backend, which trims `comp.Name` on save; landing a competition with
  // an empty canonical name. It also bypassed the JS-side uniqueness
  // check ("  Men's Cup  ".toLowerCase() !== "men's cup") so two
  // competitions with the same effective name could be created.
  //
  // Fix: trim early; fall through to the default when the trimmed value
  // is empty. Tested below for each kind plus the whitespace edge cases.
  // (mp-ey84 removed the Men/Women/Mixed "gender" input, so the default
  // is now kind-only: "Teams" / "Individual".)

  describe('trimmed user input wins when non-empty', () => {
    it('uses the user-provided name when present', () => {
      expect(deriveCompetitionName('Cup A', 'individual')).toBe('Cup A');
    });

    it('strips surrounding whitespace', () => {
      expect(deriveCompetitionName('  Cup A  ', 'individual')).toBe('Cup A');
    });

    it('keeps internal whitespace', () => {
      expect(deriveCompetitionName('  Men Open  ', 'individual')).toBe('Men Open');
    });
  });

  describe('whitespace-only input falls through to the default', () => {
    it('"" falls through', () => {
      expect(deriveCompetitionName('', 'individual')).toBe('Individual');
    });

    it('"   " falls through (the Copilot finding)', () => {
      expect(deriveCompetitionName('   ', 'individual')).toBe('Individual');
    });

    it('"\\t\\n" falls through (tabs + newlines)', () => {
      expect(deriveCompetitionName('\t\n', 'individual')).toBe('Individual');
    });

    it('null/undefined falls through to default', () => {
      expect(deriveCompetitionName(null, 'individual')).toBe('Individual');
      expect(deriveCompetitionName(undefined, 'individual')).toBe('Individual');
    });
  });

  describe('default name picks (when input is empty)', () => {
    it('team → "Teams"', () => {
      expect(deriveCompetitionName('', 'team')).toBe('Teams');
    });

    it('individual → "Individual"', () => {
      expect(deriveCompetitionName('', 'individual')).toBe('Individual');
    });

    it('unknown kind → "Individual"', () => {
      // Defensive: if kind is some future value, fall through to the
      // most-generic individual default.
      expect(deriveCompetitionName('', 'unknown')).toBe('Individual');
    });
  });
});

describe('validatePoolSettings', () => {
  // Copilot finding on PR #103: the AdminCreateCompetition number inputs
  // (poolSize, winners) used `+e.target.value` which stored NaN on clear.
  // The display fix renders NaN as "" via Number.isFinite, but without a
  // submit-time guard NaN slips into buildEmptyCompetition where the
  // `poolSize || 3` fallback silently swaps to 3 (off-by-default), and
  // negative/fractional values pass the truthy gate entirely.
  // validatePoolSettings is the submit-time guard.

  describe('knockout format short-circuits', () => {
    it('format=knockout ignores pool fields entirely', () => {
      // Knockout-only competitions don't render the pool inputs, so
      // their state can legitimately be NaN/0/whatever; don't block.
      expect(validatePoolSettings('knockout', NaN, NaN)).toEqual({ ok: true, error: null });
      expect(validatePoolSettings('knockout', 0, 0)).toEqual({ ok: true, error: null });
      expect(validatePoolSettings('knockout', 3, 2)).toEqual({ ok: true, error: null });
    });
  });

  describe('format=mixed, valid inputs pass', () => {
    it('boundary: poolSize=3, winners=1 (smallest legal pool)', () => {
      expect(validatePoolSettings('mixed', 3, 1)).toEqual({ ok: true, error: null });
    });

    it('typical: poolSize=4, winners=2', () => {
      expect(validatePoolSettings('mixed', 4, 2)).toEqual({ ok: true, error: null });
    });

    it('large: poolSize=10, winners=4', () => {
      expect(validatePoolSettings('mixed', 10, 4)).toEqual({ ok: true, error: null });
    });
  });

  describe('format=mixed, poolSize invalid (the Copilot finding)', () => {
    it('NaN poolSize (cleared input) → blocked', () => {
      const r = validatePoolSettings('mixed', NaN, 2);
      expect(r.ok).toBe(false);
      expect(r.error).toMatch(/Players per pool/);
    });

    it('fractional poolSize=2.5 → blocked (Number.isInteger guard)', () => {
      const r = validatePoolSettings('mixed', 2.5, 2);
      expect(r.ok).toBe(false);
      expect(r.error).toMatch(/whole number/);
    });

    it('poolSize=2 below min → blocked', () => {
      const r = validatePoolSettings('mixed', 2, 2);
      expect(r.ok).toBe(false);
      expect(r.error).toMatch(/≥ 3/);
    });

    it('negative poolSize → blocked', () => {
      const r = validatePoolSettings('mixed', -1, 2);
      expect(r.ok).toBe(false);
    });
  });

  describe('format=mixed, winners invalid', () => {
    it('NaN winners (cleared input) → blocked', () => {
      const r = validatePoolSettings('mixed', 4, NaN);
      expect(r.ok).toBe(false);
      expect(r.error).toMatch(/Winners per pool/);
    });

    it('fractional winners=1.5 → blocked', () => {
      const r = validatePoolSettings('mixed', 4, 1.5);
      expect(r.ok).toBe(false);
      expect(r.error).toMatch(/whole number/);
    });

    it('winners=0 below min → blocked', () => {
      const r = validatePoolSettings('mixed', 4, 0);
      expect(r.ok).toBe(false);
      expect(r.error).toMatch(/≥ 1/);
    });
  });

  describe('error message stability', () => {
    it('poolSize error is checked BEFORE winners error', () => {
      // Both invalid: poolSize NaN, winners NaN. The function reports
      // the poolSize error first so the user fixes the higher-priority
      // field first. Pin the order so a refactor that flips the checks
      // doesn't silently change UX.
      const r = validatePoolSettings('mixed', NaN, NaN);
      expect(r.error).toMatch(/Players per pool/);
    });
  });
});

describe('validateSwissSettings (T190 / FR-050a)', () => {
  // Same NaN/fractional/zero/negative concerns as validatePoolSettings:
  // the Swiss-rounds input uses decideNumericUpdate so a cleared field
  // stores NaN, which would slip past `swissRounds || 4` in the
  // create payload. This guard rejects them at submit time.

  describe('non-swiss formats short-circuit', () => {
    it('format=knockout ignores swissRounds', () => {
      expect(validateSwissSettings('knockout', NaN)).toEqual({ ok: true, error: null });
      expect(validateSwissSettings('knockout', 0)).toEqual({ ok: true, error: null });
    });

    it('format=mixed / league all skip the guard', () => {
      expect(validateSwissSettings('mixed', NaN)).toEqual({ ok: true, error: null });
      expect(validateSwissSettings('league', NaN)).toEqual({ ok: true, error: null });
    });
  });

  describe('format=swiss, valid inputs pass', () => {
    it('boundary: swissRounds=1', () => {
      expect(validateSwissSettings('swiss', 1)).toEqual({ ok: true, error: null });
    });

    it('typical: swissRounds=4 (default)', () => {
      expect(validateSwissSettings('swiss', 4)).toEqual({ ok: true, error: null });
    });

    it('large: swissRounds=10', () => {
      expect(validateSwissSettings('swiss', 10)).toEqual({ ok: true, error: null });
    });
  });

  describe('format=swiss, invalid inputs blocked', () => {
    it('NaN swissRounds (cleared input) → blocked', () => {
      const r = validateSwissSettings('swiss', NaN);
      expect(r.ok).toBe(false);
      expect(r.error).toMatch(/Swiss rounds/);
    });

    it('fractional swissRounds=4.5 → blocked', () => {
      const r = validateSwissSettings('swiss', 4.5);
      expect(r.ok).toBe(false);
      expect(r.error).toMatch(/whole number/);
    });

    it('swissRounds=0 below min → blocked', () => {
      const r = validateSwissSettings('swiss', 0);
      expect(r.ok).toBe(false);
      expect(r.error).toMatch(/≥ 1/);
    });

    it('negative swissRounds → blocked', () => {
      const r = validateSwissSettings('swiss', -3);
      expect(r.ok).toBe(false);
    });
  });
});

// bc-qual LP-5a: the "Knockout qualifiers" form-coupling pure functions
// (extraQualifiersRadioVisible / resetExtraQualifiersOnPoolModeChange /
// winnersForExtraQualifiersChange / winnersInputDisabled) moved to
// qualifier_preview.jsx -- shared by admin_setup.jsx (create form) AND
// admin_competition_settings.jsx (settings page), so their tests live in
// qualifier_preview.test.jsx now too.

describe('normalizeTheme (mp-sspn dirty tracking)', () => {
  // Copilot PR #266 round 2: branding colours/title ride on "Save changes" via
  // theme, so they must count toward the dirty cue; but the raw tournament.theme
  // and BrandingManager's mount-synced (defaults-filled) object differ, which
  // would false-dirty on load. normalizeTheme normalizes both to the same shape.

  it('returns null for absent / empty configs (logo-only or null)', () => {
    expect(normalizeTheme(null)).toBe(null);
    expect(normalizeTheme(undefined)).toBe(null);
    expect(normalizeTheme({})).toBe(null);
    expect(normalizeTheme({ logoPath: 'x' })).toBe(null); // unrelated keys don't count
  });

  it('fills BrandingManager defaults so raw and synced themes compare equal', () => {
    const raw = { primaryColor: '#aabbcc' };
    const synced = { primaryColor: '#aabbcc', accentSoftColor: '#e7eaf3', windowTitle: '' };
    expect(JSON.stringify(normalizeTheme(raw))).toBe(JSON.stringify(normalizeTheme(synced)));
    expect(normalizeTheme(raw)).toEqual({ primaryColor: '#aabbcc', accentSoftColor: '#e7eaf3', windowTitle: '' });
  });

  it('preserves a fully-specified theme and treats windowTitle-only as configured', () => {
    expect(normalizeTheme({ primaryColor: '#111', accentSoftColor: '#222', windowTitle: 'Cup' }))
      .toEqual({ primaryColor: '#111', accentSoftColor: '#222', windowTitle: 'Cup' });
    expect(normalizeTheme({ windowTitle: 'Cup' }))
      .toEqual({ primaryColor: '#1d3557', accentSoftColor: '#e7eaf3', windowTitle: 'Cup' });
  });
});

// ---------------------------------------------------------------------------
// Import preview: the shiaijo allocation, shown BEFORE every row is refused
// ---------------------------------------------------------------------------
// A manifest row that omits `courts` inherits the tournament's whole list, and
// the inherited count is validated exactly like a spelled-out one. On a
// 3-shiaijo venue that makes the commonest manifest of all - one with no
// courts key - the one that gets refused, and the preview table showed no
// allocation at all, so the operator learned it one row at a time from the
// results list.
describe('previewRowCourts', () => {
  const venue = ['A', 'B', 'C'];

  it("uses the row's own shiaijo when it has them", () => {
    expect(previewRowCourts({ courts: ['A', 'B'] }, venue)).toEqual(['A', 'B']);
  });

  it('inherits the venue when the row omits courts, matching the server', () => {
    expect(previewRowCourts({}, venue)).toEqual(venue);
    expect(previewRowCourts({ courts: [] }, venue)).toEqual(venue);
    expect(previewRowCourts({ courts: null }, venue)).toEqual(venue);
  });

  it('survives a missing venue list', () => {
    expect(previewRowCourts({}, undefined)).toEqual([]);
    expect(previewRowCourts(null, venue)).toEqual(venue);
  });
});

describe('previewRowShiaijoError', () => {
  const venue = ['A', 'B', 'C'];

  it('flags the inherited 3-shiaijo allocation the server will refuse', () => {
    const err = previewRowShiaijoError({ name: 'Mudansha', format: 'mixed' }, venue);
    expect(err).toContain('3 shiaijo cannot be paired down to a single bracket');
  });

  it('offers only counts the venue can supply', () => {
    const err = previewRowShiaijoError({ format: 'mixed', courts: ['A', 'B', 'C'] }, venue);
    expect(err).toContain('This tournament has 3, so this competition can use 1 or 2');
    expect(err).not.toContain('4');
  });

  it('passes a row that spelled out a legal allocation', () => {
    expect(previewRowShiaijoError({ format: 'mixed', courts: ['A', 'B'] }, venue)).toBeNull();
  });

  it('leaves league and Swiss rows alone, as every other surface does', () => {
    expect(previewRowShiaijoError({ format: 'league' }, venue)).toBeNull();
    expect(previewRowShiaijoError({ format: 'swiss' }, venue)).toBeNull();
  });

  it('treats a row with no format as bracket-drawing, matching the importer', () => {
    expect(previewRowShiaijoError({}, venue)).toContain('cannot be paired down');
  });
});

describe('importRowErrorText', () => {
  it('strips the raw courts: API prefix and reads as a sentence', () => {
    const raw = 'courts: shiaijo count must be a power of two (1, 2, 4, 8 or 16), got 3: use 2 or 4, or 1';
    const out = importRowErrorText(raw);
    expect(out.startsWith('Shiaijo count must be a power of two')).toBe(true);
    expect(out).not.toContain('courts:');
  });

  it('keeps prefixes that are the only thing naming the faulty setting or step', () => {
    expect(importRowErrorText('format: unknown format "kachinuki"')).toBe('Format: unknown format "kachinuki"');
    expect(importRowErrorText('swissRounds: must be at least 1')).toBe('SwissRounds: must be at least 1');
    expect(importRowErrorText('parse participants: bad line 3')).toBe('Parse participants: bad line 3');
  });

  it('survives an empty or missing error', () => {
    expect(importRowErrorText('')).toBe('');
    expect(importRowErrorText(undefined)).toBe('');
    expect(importRowErrorText(null)).toBe('');
  });
});
