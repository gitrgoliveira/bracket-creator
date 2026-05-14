import { describe, it, expect } from 'vitest';
import { sideName, hasBothSides, compMatchStats, normalizeDate, isValidISODate, validateAndNormalizeDate, decideNumericUpdate, DATE_ERR_INVALID_FORMAT, DATE_ERR_YEAR_RANGE, MIN_YEAR, MAX_YEAR, MAX_TEAM_SIZE } from '../admin_helpers.jsx';

describe('sideName', () => {
  it('returns "" for null / undefined', () => {
    expect(sideName(null)).toBe("");
    expect(sideName(undefined)).toBe("");
  });

  it('returns the string itself when side is a bare string', () => {
    expect(sideName("Akira Tanaka")).toBe("Akira Tanaka");
  });

  it('returns side.name when side is an object with a name', () => {
    expect(sideName({ id: "p1", name: "Akira Tanaka" })).toBe("Akira Tanaka");
  });

  it('returns "" for normalizeMatch\'s {id:"",name:""} placeholder', () => {
    // This shape comes from normalizeMatch substituting missing sides; the
    // object is truthy but should be treated as "no real side present".
    expect(sideName({ id: "", name: "" })).toBe("");
  });

  it('returns "" when side has no name property', () => {
    expect(sideName({ id: "p1" })).toBe("");
  });
});

describe('hasBothSides', () => {
  const real = (id, name) => ({ id, name });
  const placeholder = { id: "", name: "" }; // normalizeMatch's substitution

  it('returns true when both sides have real names', () => {
    expect(hasBothSides({ sideA: real("a", "Alice"), sideB: real("b", "Bob") })).toBe(true);
  });

  it('returns true for raw string sides (pre-normalizeMatch backend shape)', () => {
    expect(hasBothSides({ sideA: "Alice", sideB: "Bob" })).toBe(true);
  });

  it('returns false when sideA is a normalizeMatch placeholder', () => {
    // The bug Copilot caught: m.sideA && m.sideB used to be `true` here
    // because placeholder is a truthy object. hasBothSides correctly
    // detects the empty name.
    expect(hasBothSides({ sideA: placeholder, sideB: real("b", "Bob") })).toBe(false);
  });

  it('returns false when sideB is a normalizeMatch placeholder', () => {
    expect(hasBothSides({ sideA: real("a", "Alice"), sideB: placeholder })).toBe(false);
  });

  it('returns false when both sides are placeholders', () => {
    expect(hasBothSides({ sideA: placeholder, sideB: placeholder })).toBe(false);
  });

  it('returns false when sideA is null/undefined', () => {
    expect(hasBothSides({ sideA: null, sideB: real("b", "Bob") })).toBe(false);
    expect(hasBothSides({ sideA: undefined, sideB: real("b", "Bob") })).toBe(false);
  });

  it('returns false when sideB is missing entirely (bracket bye)', () => {
    expect(hasBothSides({ sideA: real("a", "Alice") })).toBe(false);
  });

  it('returns false when the match itself is null/undefined', () => {
    expect(hasBothSides(null)).toBe(false);
    expect(hasBothSides(undefined)).toBe(false);
  });

  it('returns false for empty string sides (raw backend bye)', () => {
    expect(hasBothSides({ sideA: "", sideB: "Bob" })).toBe(false);
    expect(hasBothSides({ sideA: "Alice", sideB: "" })).toBe(false);
  });

  it('returns a real boolean (not a truthy/falsy value) for use in JSX guards', () => {
    // Important for `{hasBothSides(m) ? <Component /> : null}` rendering —
    // returning a non-boolean truthy value (e.g. a string) would render
    // it as a text node in JSX.
    const t = hasBothSides({ sideA: real("a", "Alice"), sideB: real("b", "Bob") });
    expect(typeof t).toBe("boolean");
    const f = hasBothSides({ sideA: placeholder, sideB: real("b", "Bob") });
    expect(typeof f).toBe("boolean");
  });
});

describe('compMatchStats', () => {
  const realMatch = (status) => ({
    sideA: { id: "a", name: "Alice" },
    sideB: { id: "b", name: "Bob" },
    status,
  });
  const byeMatch = {
    sideA: { id: "a", name: "Alice" },
    sideB: { id: "", name: "" }, // normalizeMatch placeholder
    status: "scheduled",
  };

  it('returns zeros for a competition with no matches', () => {
    expect(compMatchStats({})).toEqual({ total: 0, done: 0, live: 0 });
  });

  it('counts flat poolMatches', () => {
    const c = {
      poolMatches: [
        realMatch("completed"),
        realMatch("running"),
        realMatch("scheduled"),
      ],
    };
    expect(compMatchStats(c)).toEqual({ total: 3, done: 1, live: 1 });
  });

  it('counts pools[].matches when poolMatches is absent', () => {
    const c = {
      pools: [
        { matches: [realMatch("completed"), realMatch("scheduled")] },
        { matches: [realMatch("running")] },
      ],
    };
    expect(compMatchStats(c)).toEqual({ total: 3, done: 1, live: 1 });
  });

  it('counts bracket rounds in addition to pool matches', () => {
    const c = {
      poolMatches: [realMatch("completed")],
      bracket: {
        rounds: [
          [realMatch("completed"), realMatch("running")],
          [realMatch("scheduled")],
        ],
      },
    };
    expect(compMatchStats(c)).toEqual({ total: 4, done: 2, live: 1 });
  });

  it('skips bye / unresolved sides (normalizeMatch placeholders)', () => {
    const c = { poolMatches: [realMatch("completed"), byeMatch] };
    expect(compMatchStats(c)).toEqual({ total: 1, done: 1, live: 0 });
  });
});

describe('normalizeDate', () => {
  it('returns falsy input unchanged', () => {
    expect(normalizeDate(null)).toBe(null);
    expect(normalizeDate(undefined)).toBe(undefined);
    expect(normalizeDate("")).toBe("");
  });

  it('passes through ISO-format dates', () => {
    expect(normalizeDate("2026-05-13")).toBe("2026-05-13");
  });

  it('converts DD-MM-YYYY to ISO', () => {
    expect(normalizeDate("13-05-2026")).toBe("2026-05-13");
  });

  it('converts DD/MM/YYYY to ISO', () => {
    expect(normalizeDate("13/05/2026")).toBe("2026-05-13");
  });

  it('zero-pads single-digit days and months', () => {
    expect(normalizeDate("3-5-2026")).toBe("2026-05-03");
    expect(normalizeDate("3/5/2026")).toBe("2026-05-03");
  });

  it('returns unrecognized strings unchanged (caller validates)', () => {
    expect(normalizeDate("not a date")).toBe("not a date");
    expect(normalizeDate("2026/05/13")).toBe("2026/05/13"); // wrong separator order
  });

  it('rejects semantically invalid ISO dates', () => {
    expect(normalizeDate("2026-13-32")).toBe(null);
    expect(normalizeDate("2026-02-31")).toBe(null);
    expect(normalizeDate("2026-00-15")).toBe(null);
    expect(normalizeDate("2026-04-31")).toBe(null); // April has 30 days
  });

  it('rejects semantically invalid DD-MM-YYYY dates', () => {
    expect(normalizeDate("32-13-2026")).toBe(null);
    expect(normalizeDate("31-02-2026")).toBe(null);
    expect(normalizeDate("00-05-2026")).toBe(null);
  });

  it('accepts Feb 29 in leap years and rejects in non-leap years', () => {
    expect(normalizeDate("2024-02-29")).toBe("2024-02-29");
    expect(normalizeDate("2026-02-29")).toBe(null);
  });
});

describe('isValidISODate', () => {
  // This is the predicate used by AdminCompetition's "Start competition"
  // button gate. The Copilot finding: pre-fix, this function only did a
  // shape regex + year range check, so semantically-invalid dates like
  // "2026-13-32" (which normalizeDate correctly rejects) still enabled
  // the button, letting the operator start a competition with a date
  // that AdminSettings.saveNow would refuse to save.

  it('accepts a valid ISO date in range', () => {
    expect(isValidISODate("2026-05-13")).toBe(true);
    expect(isValidISODate("1900-01-01")).toBe(true); // year boundary
    expect(isValidISODate("2100-12-31")).toBe(true); // year boundary
  });

  it('accepts DD-MM-YYYY input that normalizeDate canonicalizes', () => {
    expect(isValidISODate("13-05-2026")).toBe(true);
    expect(isValidISODate("13/05/2026")).toBe(true);
  });

  it('rejects semantically invalid dates (Copilot finding)', () => {
    // These all have valid shape but represent impossible days.
    expect(isValidISODate("2026-13-32")).toBe(false);
    expect(isValidISODate("2026-02-31")).toBe(false);
    expect(isValidISODate("2026-00-15")).toBe(false);
    expect(isValidISODate("2026-04-31")).toBe(false); // April has 30 days
    expect(isValidISODate("2026-02-29")).toBe(false); // non-leap
  });

  it('accepts Feb 29 in a leap year', () => {
    expect(isValidISODate("2024-02-29")).toBe(true);
  });

  it('rejects years outside [1900, 2100]', () => {
    expect(isValidISODate("1899-12-31")).toBe(false);
    expect(isValidISODate("2101-01-01")).toBe(false);
    expect(isValidISODate("0001-01-01")).toBe(false);
  });

  it('rejects falsy / empty / undefined input', () => {
    expect(isValidISODate("")).toBe(false);
    expect(isValidISODate(null)).toBe(false);
    expect(isValidISODate(undefined)).toBe(false);
  });

  it('rejects unrecognized strings', () => {
    expect(isValidISODate("not a date")).toBe(false);
    expect(isValidISODate("2026/05/13")).toBe(false); // wrong separator order
    expect(isValidISODate("13.05.2026")).toBe(false);
  });

  it('returns a real boolean (for use in disabled={!isValidISODate(...)} props)', () => {
    expect(typeof isValidISODate("2026-05-13")).toBe("boolean");
    expect(typeof isValidISODate("")).toBe("boolean");
    expect(typeof isValidISODate(null)).toBe("boolean");
  });
});

describe('validateAndNormalizeDate', () => {
  // Combined predicate + normalizer used by save flows that need both
  // the user-facing error message AND the normalized date value to save.
  // Two consumers: AdminEditTournament.handleSave, AdminCreateCompetition.create.

  it('returns {norm, error: null} for a valid date', () => {
    expect(validateAndNormalizeDate("2026-05-13")).toEqual({
      norm: "2026-05-13",
      error: null,
    });
  });

  it('normalizes DD-MM-YYYY input to ISO', () => {
    expect(validateAndNormalizeDate("13-05-2026")).toEqual({
      norm: "2026-05-13",
      error: null,
    });
  });

  it('returns the "Invalid date" message for semantically invalid input', () => {
    expect(validateAndNormalizeDate("2026-13-32")).toEqual({
      norm: null,
      error: "Invalid date. Please pick a valid day.",
    });
    expect(validateAndNormalizeDate("2026-02-29")).toEqual({
      norm: null,
      error: "Invalid date. Please pick a valid day.",
    });
  });

  it('returns the "Invalid date" message for empty / falsy input', () => {
    expect(validateAndNormalizeDate("").error).toBe("Invalid date. Please pick a valid day.");
    expect(validateAndNormalizeDate(null).error).toBe("Invalid date. Please pick a valid day.");
    expect(validateAndNormalizeDate(undefined).error).toBe("Invalid date. Please pick a valid day.");
  });

  it('returns the "Year must be..." message for out-of-range years', () => {
    expect(validateAndNormalizeDate("1899-12-31")).toEqual({
      norm: null,
      error: "Year must be between 1900 and 2100.",
    });
    expect(validateAndNormalizeDate("2101-01-01")).toEqual({
      norm: null,
      error: "Year must be between 1900 and 2100.",
    });
  });

  it('accepts year boundary values 1900 and 2100', () => {
    expect(validateAndNormalizeDate("1900-01-01").error).toBe(null);
    expect(validateAndNormalizeDate("2100-12-31").error).toBe(null);
  });

  it('returns the canonical DATE_ERR_* constants (lockstep with saveNow)', () => {
    // saveNow in admin_competition.jsx imports the same constants from
    // window.DATE_ERR_*, so this assertion mechanically guarantees that
    // the four date-validation sites can't drift on error messages.
    expect(validateAndNormalizeDate("not a date").error).toBe(DATE_ERR_INVALID_FORMAT);
    expect(validateAndNormalizeDate("1850-01-01").error).toBe(DATE_ERR_YEAR_RANGE);
  });

  it('DATE_ERR_* constants have the expected user-facing strings', () => {
    // Pin the user-visible text so an accidental string change is
    // caught by tests (the test failure tells whoever changes it to
    // also update any screenshot fixtures, docs, etc.). DATE_ERR_YEAR_RANGE
    // is a template literal built from MIN_YEAR/MAX_YEAR — if you bump
    // those, also update this pin.
    expect(DATE_ERR_INVALID_FORMAT).toBe("Invalid date. Please pick a valid day.");
    expect(DATE_ERR_YEAR_RANGE).toBe("Year must be between 1900 and 2100.");
  });

  it('DATE_ERR_YEAR_RANGE is derived from MIN_YEAR/MAX_YEAR', () => {
    // Mechanically guarantees the user-facing text stays consistent
    // with the predicate bounds. A future change to MIN_YEAR or
    // MAX_YEAR auto-updates the error message.
    expect(DATE_ERR_YEAR_RANGE).toContain(String(MIN_YEAR));
    expect(DATE_ERR_YEAR_RANGE).toContain(String(MAX_YEAR));
  });
});

describe('numeric bounds constants', () => {
  it('MIN_YEAR and MAX_YEAR have the expected values (pin)', () => {
    // Pin the current bounds. If anyone tightens to (2000, 2050) or
    // loosens further, this test fails and tells them to also update
    // docs / screenshot fixtures / saveNow's still-inline usage.
    expect(MIN_YEAR).toBe(1900);
    expect(MAX_YEAR).toBe(2100);
  });

  it('MAX_TEAM_SIZE matches kendo team-position conventions', () => {
    // Pin the current cap. admin_scoring_modal.jsx builds
    // TEAM_POSITIONS from this; if you bump it, also extend the
    // scoring UI / docs / screenshot fixtures.
    expect(MAX_TEAM_SIZE).toBe(9);
  });

  it('validateAndNormalizeDate predicate matches MIN_YEAR/MAX_YEAR bounds', () => {
    // The boundary cases must be in lockstep with the constants
    // (off-by-one regression guard).
    expect(validateAndNormalizeDate(`${MIN_YEAR}-01-01`).error).toBe(null);
    expect(validateAndNormalizeDate(`${MAX_YEAR}-12-31`).error).toBe(null);
    expect(validateAndNormalizeDate(`${MIN_YEAR - 1}-12-31`).error).toBe(DATE_ERR_YEAR_RANGE);
    expect(validateAndNormalizeDate(`${MAX_YEAR + 1}-01-01`).error).toBe(DATE_ERR_YEAR_RANGE);
  });

  it('isValidISODate is a thin wrapper that returns error === null', () => {
    // Verify the two helpers stay in lockstep — isValidISODate is now
    // implemented as `validateAndNormalizeDate(d).error === null`.
    const cases = ["2026-05-13", "13-05-2026", "2026-13-32", "1899-01-01", "", null];
    cases.forEach((c) => {
      expect(isValidISODate(c)).toBe(validateAndNormalizeDate(c).error === null);
    });
  });
});

describe('decideNumericUpdate', () => {
  // Deep-review I5 finding: AdminSettings.teamSize/poolSize/poolWinners
  // each used `onChange={e => update(k, +e.target.value)}`. Clearing the
  // input → `+""` → 0 → debounced saveLater fires saveNow → sends `0` to
  // the backend, which rejects. The user sees the input collapse to "0"
  // then a "Save failed" toast 400ms later.
  //
  // Same shape as the matchDuration fix in round 7 (admin_schedule.jsx),
  // but in a different file. The grep recipe at deep-review/SKILL.md I5
  // surfaces every `+e.target.value` so the parallel sites stop falling
  // through this gap.

  describe('empty / nullish input → NaN, do not save', () => {
    it('empty string returns NaN + shouldSave=false', () => {
      expect(decideNumericUpdate("", 1)).toEqual({ value: NaN, shouldSave: false });
    });

    it('null returns NaN + shouldSave=false', () => {
      expect(decideNumericUpdate(null, 1)).toEqual({ value: NaN, shouldSave: false });
    });

    it('undefined returns NaN + shouldSave=false', () => {
      expect(decideNumericUpdate(undefined, 1)).toEqual({ value: NaN, shouldSave: false });
    });
  });

  describe('valid positive integer ≥ min → save', () => {
    it('"5" with min=1 → {value:5, shouldSave:true}', () => {
      expect(decideNumericUpdate("5", 1)).toEqual({ value: 5, shouldSave: true });
    });

    it('"3" with min=3 (at boundary) → save', () => {
      expect(decideNumericUpdate("3", 3)).toEqual({ value: 3, shouldSave: true });
    });

    it('"100" with min=1 → save', () => {
      expect(decideNumericUpdate("100", 1)).toEqual({ value: 100, shouldSave: true });
    });

    it('default min is 1 when not provided', () => {
      expect(decideNumericUpdate("5")).toEqual({ value: 5, shouldSave: true });
      expect(decideNumericUpdate("0")).toEqual({ value: 0, shouldSave: false });
    });
  });

  describe('below min → keep value but do not save', () => {
    it('"0" with min=1 → no save (the original bug — backend rejected 0)', () => {
      expect(decideNumericUpdate("0", 1)).toEqual({ value: 0, shouldSave: false });
    });

    it('"2" with min=3 → no save (just below pool-size minimum)', () => {
      expect(decideNumericUpdate("2", 3)).toEqual({ value: 2, shouldSave: false });
    });

    it('"-5" with min=1 → no save', () => {
      expect(decideNumericUpdate("-5", 1)).toEqual({ value: -5, shouldSave: false });
    });
  });

  describe('non-integer → keep value but do not save', () => {
    it('"1.5" with min=1 → no save (browser number inputs accept fractions)', () => {
      expect(decideNumericUpdate("1.5", 1)).toEqual({ value: 1.5, shouldSave: false });
    });

    it('"3.14" with min=3 → no save', () => {
      expect(decideNumericUpdate("3.14", 3)).toEqual({ value: 3.14, shouldSave: false });
    });
  });

  describe('non-numeric → keep value but do not save', () => {
    it('"abc" → {value:NaN, shouldSave:false}', () => {
      const result = decideNumericUpdate("abc", 1);
      expect(Number.isNaN(result.value)).toBe(true);
      expect(result.shouldSave).toBe(false);
    });

    it('"5abc" → {value:NaN, shouldSave:false}', () => {
      // `+"5abc"` is NaN, not 5 — JS coercion doesn't substring-parse.
      const result = decideNumericUpdate("5abc", 1);
      expect(Number.isNaN(result.value)).toBe(true);
      expect(result.shouldSave).toBe(false);
    });
  });

  describe('Infinity / -Infinity → keep but do not save', () => {
    // Number-typed inputs can't normally produce these, but the helper
    // shouldn't allow them through if a caller passes weird values.
    it('"Infinity" → no save', () => {
      expect(decideNumericUpdate("Infinity", 1)).toEqual({ value: Infinity, shouldSave: false });
    });

    it('"-Infinity" → no save', () => {
      expect(decideNumericUpdate("-Infinity", 1)).toEqual({ value: -Infinity, shouldSave: false });
    });
  });
});
