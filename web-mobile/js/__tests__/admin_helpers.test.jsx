import { describe, it, expect } from 'vitest';
import { sideName, hasBothSides, compMatchStats, normalizeDate, isValidISODate } from '../admin_helpers.jsx';

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
