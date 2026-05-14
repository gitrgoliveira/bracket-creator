import { describe, it, expect } from 'vitest';
import { deriveCompetitionName, validatePoolSettings } from '../admin_setup.jsx';

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

  describe('format=pools, valid inputs pass', () => {
    it('boundary: poolSize=3, winners=1 (smallest legal pool)', () => {
      expect(validatePoolSettings('pools', 3, 1)).toEqual({ ok: true, error: null });
    });

    it('typical: poolSize=4, winners=2', () => {
      expect(validatePoolSettings('pools', 4, 2)).toEqual({ ok: true, error: null });
    });

    it('large: poolSize=10, winners=4', () => {
      expect(validatePoolSettings('pools', 10, 4)).toEqual({ ok: true, error: null });
    });
  });

  describe('format=pools, poolSize invalid (the Copilot finding)', () => {
    it('NaN poolSize (cleared input) → blocked', () => {
      const r = validatePoolSettings('pools', NaN, 2);
      expect(r.ok).toBe(false);
      expect(r.error).toMatch(/Players per pool/);
    });

    it('fractional poolSize=2.5 → blocked (Number.isInteger guard)', () => {
      const r = validatePoolSettings('pools', 2.5, 2);
      expect(r.ok).toBe(false);
      expect(r.error).toMatch(/whole number/);
    });

    it('poolSize=2 below min → blocked', () => {
      const r = validatePoolSettings('pools', 2, 2);
      expect(r.ok).toBe(false);
      expect(r.error).toMatch(/≥ 3/);
    });

    it('negative poolSize → blocked', () => {
      const r = validatePoolSettings('pools', -1, 2);
      expect(r.ok).toBe(false);
    });
  });

  describe('format=pools, winners invalid', () => {
    it('NaN winners (cleared input) → blocked', () => {
      const r = validatePoolSettings('pools', 4, NaN);
      expect(r.ok).toBe(false);
      expect(r.error).toMatch(/Winners per pool/);
    });

    it('fractional winners=1.5 → blocked', () => {
      const r = validatePoolSettings('pools', 4, 1.5);
      expect(r.ok).toBe(false);
      expect(r.error).toMatch(/whole number/);
    });

    it('winners=0 below min → blocked', () => {
      const r = validatePoolSettings('pools', 4, 0);
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
      const r = validatePoolSettings('pools', NaN, NaN);
      expect(r.error).toMatch(/Players per pool/);
    });
  });
});
