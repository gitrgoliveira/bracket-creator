import { describe, it, expect } from 'vitest';
import { deriveCompetitionName, validatePoolSettings, validateSwissSettings } from '../admin_setup.jsx';
import { normalizeTheme } from '../admin_branding.jsx';

describe('deriveCompetitionName', () => {
  // Copilot round-8 finding: AdminCreateCompetition.create used
  //   const finalName = name || <kind/gender default>
  // where `name` was the raw input. A whitespace-only string ("   ") is
  // truthy, so it would slip past the default-fallback and be sent to the
  // backend, which trims `comp.Name` on save — landing a competition with
  // an empty canonical name. It also bypassed the JS-side uniqueness
  // check ("  Men's Cup  ".toLowerCase() !== "men's cup") so two
  // competitions with the same effective name could be created.
  //
  // Fix: trim early; fall through to the default when the trimmed value
  // is empty. Tested below for every (kind, gender) tuple plus the
  // whitespace edge cases.

  describe('trimmed user input wins when non-empty', () => {
    it('uses the user-provided name when present', () => {
      expect(deriveCompetitionName('Cup A', 'individual', 'M')).toBe('Cup A');
    });

    it('strips surrounding whitespace', () => {
      expect(deriveCompetitionName('  Cup A  ', 'individual', 'M')).toBe('Cup A');
    });

    it('keeps internal whitespace', () => {
      expect(deriveCompetitionName('  Men Open  ', 'individual', 'M')).toBe('Men Open');
    });
  });

  describe('whitespace-only input falls through to the default', () => {
    it('"" falls through', () => {
      expect(deriveCompetitionName('', 'individual', 'M')).toBe("Men's Individual");
    });

    it('"   " falls through (the Copilot finding)', () => {
      expect(deriveCompetitionName('   ', 'individual', 'M')).toBe("Men's Individual");
    });

    it('"\\t\\n" falls through (tabs + newlines)', () => {
      expect(deriveCompetitionName('\t\n', 'individual', 'M')).toBe("Men's Individual");
    });

    it('null/undefined falls through to default', () => {
      expect(deriveCompetitionName(null, 'individual', 'M')).toBe("Men's Individual");
      expect(deriveCompetitionName(undefined, 'individual', 'M')).toBe("Men's Individual");
    });
  });

  describe('default name picks (when input is empty)', () => {
    it("team / F → Women's Teams", () => {
      expect(deriveCompetitionName('', 'team', 'F')).toBe("Women's Teams");
    });

    it("team / M → Men's Teams", () => {
      expect(deriveCompetitionName('', 'team', 'M')).toBe("Men's Teams");
    });

    it("team / unspecified gender → Men's Teams (current default)", () => {
      // The original code falls through to the "Men's Teams" branch when
      // gender is neither "F" nor "M". Pinned here so anyone changing the
      // default has to update the test deliberately.
      expect(deriveCompetitionName('', 'team', null)).toBe("Men's Teams");
    });

    it("individual / F → Women's Individual", () => {
      expect(deriveCompetitionName('', 'individual', 'F')).toBe("Women's Individual");
    });

    it("individual / M → Men's Individual", () => {
      expect(deriveCompetitionName('', 'individual', 'M')).toBe("Men's Individual");
    });

    it('individual / unspecified → "Individual"', () => {
      expect(deriveCompetitionName('', 'individual', null)).toBe('Individual');
      expect(deriveCompetitionName('', 'individual', undefined)).toBe('Individual');
    });

    it('unknown kind / unspecified gender → "Individual"', () => {
      // Defensive: if kind is some future value, fall through to the
      // most-generic individual default.
      expect(deriveCompetitionName('', 'unknown', null)).toBe('Individual');
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

  describe('playoffs format short-circuits', () => {
    it('format=playoffs ignores pool fields entirely', () => {
      // Knockout-only competitions don't render the pool inputs, so
      // their state can legitimately be NaN/0/whatever — don't block.
      expect(validatePoolSettings('playoffs', NaN, NaN)).toEqual({ ok: true, error: null });
      expect(validatePoolSettings('playoffs', 0, 0)).toEqual({ ok: true, error: null });
      expect(validatePoolSettings('playoffs', 3, 2)).toEqual({ ok: true, error: null });
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
    it('format=playoffs ignores swissRounds', () => {
      expect(validateSwissSettings('playoffs', NaN)).toEqual({ ok: true, error: null });
      expect(validateSwissSettings('playoffs', 0)).toEqual({ ok: true, error: null });
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

describe('normalizeTheme (mp-sspn dirty tracking)', () => {
  // Copilot PR #266 round 2: branding colours/title ride on "Save changes" via
  // theme, so they must count toward the dirty cue — but the raw tournament.theme
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
