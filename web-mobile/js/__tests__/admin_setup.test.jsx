import { describe, it, expect } from 'vitest';
import { deriveCompetitionName } from '../admin_setup.jsx';

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
