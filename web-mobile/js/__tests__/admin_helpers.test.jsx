import { describe, it, expect } from 'vitest';
import { sideName, compMatchStats, normalizeDate } from '../admin_helpers.jsx';

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
