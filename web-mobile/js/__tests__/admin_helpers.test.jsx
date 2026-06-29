import { describe, it, expect } from 'vitest';
import { sideName, hasBothSides, hasPoolOriginPlaceholder, compMatchStats, normalizeDate, dmyToIso, isoToDmy, compareDmy, isValidDate, validateAndNormalizeDate, decideNumericUpdate, getScoreBtnClass, deriveTournamentDays, normalizeCourts, courtCount, resolveRoundIndex, DATE_ERR_INVALID_FORMAT, DATE_ERR_YEAR_RANGE, MIN_YEAR, MAX_YEAR, MAX_TEAM_SIZE, MAX_COURTS, MAX_RANK, MAX_TOURNAMENT_DURATION_DAYS } from '../admin_helpers.jsx';

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

  // Copilot review finding on PR #104: hasBothSides previously only
  // checked for non-empty side names, which let bracket placeholder
  // strings like "Winner of r2-m0" slip through as "real participants".
  // Unresolved future-round bracket matches then showed up in the
  // viewer's upcoming-matches list. The fix excludes any "Winner of "
  // prefix explicitly.
  it('returns false when either side is an unresolved "Winner of" bracket placeholder', () => {
    expect(hasBothSides({ sideA: "Alice", sideB: "Winner of r0-m1" })).toBe(false);
    expect(hasBothSides({ sideA: "Winner of r1-m0", sideB: "Bob" })).toBe(false);
    expect(hasBothSides({ sideA: "Winner of r0-m0", sideB: "Winner of r0-m1" })).toBe(false);
  });

  it('returns false when "Winner of" placeholders are inside normalizeMatch side objects', () => {
    // normalizeMatch wraps raw side strings into {id, name} objects.
    // The "Winner of" prefix could still be in the name field.
    expect(hasBothSides({ sideA: { id: "", name: "Winner of r1-m0" }, sideB: real("b", "Bob") })).toBe(false);
    expect(hasBothSides({ sideA: real("a", "Alice"), sideB: { id: "", name: "Winner of r0-m0" } })).toBe(false);
  });

  it('returns true for resolved bracket matches (no "Winner of" prefix)', () => {
    // After the source match resolves, the bracket entry's side updates
    // to the actual winner's name. hasBothSides should accept those.
    expect(hasBothSides({ sideA: "Alice", sideB: "Charlie" })).toBe(true);
  });

  // Copilot round-4 finding on PR #104: the prefix-match `startsWith("Winner of ")`
  // was too broad: it rejected real participants whose names happened to
  // start with "Winner of " (e.g. "Winner of the 2025 Cup"). The placeholder
  // pattern emitted by the bracket generator is the exact shape
  // `Winner of r<digits>-m<digits>`; only that should be rejected.
  it('accepts legitimate participants whose names start with "Winner of "', () => {
    expect(hasBothSides({ sideA: "Winner of the 2025 Cup", sideB: "Alice" })).toBe(true);
    expect(hasBothSides({ sideA: "Alice", sideB: "Winner of the 2025 Cup" })).toBe(true);
    // "Winner of " without the rX-mY suffix is a real name, not a placeholder.
    expect(hasBothSides({ sideA: "Winner of Tournament", sideB: "Bob" })).toBe(true);
    // Mixed case or extra whitespace shouldn't match the strict placeholder shape.
    expect(hasBothSides({ sideA: "winner of r0-m1", sideB: "Bob" })).toBe(true);
  });

  it('still rejects the exact placeholder shape Winner of r<n>-m<n>', () => {
    // Pin the regex against representative bracket-generator outputs.
    // internal/engine/bracket.go emits "Winner of r%d-m%d": round and
    // match indices are zero-based non-negative integers.
    expect(hasBothSides({ sideA: "Winner of r0-m0", sideB: "Alice" })).toBe(false);
    expect(hasBothSides({ sideA: "Winner of r10-m255", sideB: "Alice" })).toBe(false);
    expect(hasBothSides({ sideA: "Winner of r1-m1", sideB: "Winner of r1-m2" })).toBe(false);
  });

  it('returns a real boolean (not a truthy/falsy value) for use in JSX guards', () => {
    // Important for `{hasBothSides(m) ? <Component />: null}` rendering:
    // returning a non-boolean truthy value (e.g. a string) would render
    // it as a text node in JSX.
    const t = hasBothSides({ sideA: real("a", "Alice"), sideB: real("b", "Bob") });
    expect(typeof t).toBe("boolean");
    const f = hasBothSides({ sideA: placeholder, sideB: real("b", "Bob") });
    expect(typeof f).toBe("boolean");
  });
});

// hasPoolOriginPlaceholder gates the admin "Knockout filling in" banner. Unlike
// !hasBothSides it must be TRUE only for pool-origin "Pool A-1st" placeholders:
// NOT for "Winner of rX-mY" feeders or structural byes: so the banner doesn't
// show for standalone playoffs or bye-containing brackets (Copilot round-7 finding).
describe('hasPoolOriginPlaceholder', () => {
  it('returns true when a side is a pool-origin "Pool X-Nth" placeholder', () => {
    expect(hasPoolOriginPlaceholder({ sideA: "Pool A-1st", sideB: "Bob" })).toBe(true);
    expect(hasPoolOriginPlaceholder({ sideA: "Alice", sideB: "Pool B-2nd" })).toBe(true);
    expect(hasPoolOriginPlaceholder({ sideA: { id: "", name: "Pool C-1st" }, sideB: "Bob" })).toBe(true);
  });

  it('returns false for "Winner of rX-mY" feeders (a playoffs bracket is not "filling in")', () => {
    expect(hasPoolOriginPlaceholder({ sideA: "Winner of r0-m1", sideB: "Bob" })).toBe(false);
    expect(hasPoolOriginPlaceholder({ sideA: "Winner of r1-m0", sideB: "Winner of r1-m1" })).toBe(false);
  });

  it('returns false for structural byes and resolved matches', () => {
    expect(hasPoolOriginPlaceholder({ sideA: "Alice", sideB: "" })).toBe(false);
    expect(hasPoolOriginPlaceholder({ sideA: "Alice", sideB: "Bob" })).toBe(false);
    expect(hasPoolOriginPlaceholder(null)).toBe(false);
  });

  it('returns false for a real participant whose name merely contains "Pool"', () => {
    expect(hasPoolOriginPlaceholder({ sideA: "Liverpool FC", sideB: "Bob" })).toBe(false);
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
    expect(compMatchStats({})).toEqual({ total: 0, done: 0, running: 0 });
  });

  it('counts flat poolMatches', () => {
    const c = {
      poolMatches: [
        realMatch("completed"),
        realMatch("running"),
        realMatch("scheduled"),
      ],
    };
    expect(compMatchStats(c)).toEqual({ total: 3, done: 1, running: 1 });
  });

  it('counts pools[].matches when poolMatches is absent', () => {
    const c = {
      pools: [
        { matches: [realMatch("completed"), realMatch("scheduled")] },
        { matches: [realMatch("running")] },
      ],
    };
    expect(compMatchStats(c)).toEqual({ total: 3, done: 1, running: 1 });
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
    expect(compMatchStats(c)).toEqual({ total: 4, done: 2, running: 1 });
  });

  it('skips bye / unresolved sides (normalizeMatch placeholders)', () => {
    const c = { poolMatches: [realMatch("completed"), byeMatch] };
    expect(compMatchStats(c)).toEqual({ total: 1, done: 1, running: 0 });
  });
});

describe('normalizeDate', () => {
  // Canonical output is DD-MM-YYYY. ISO YYYY-MM-DD is accepted as input
  // (boundary convenience for HTML `<input type="date">`) and converted.
  // See admin_helpers.jsx for rationale.

  it('returns falsy input unchanged', () => {
    expect(normalizeDate(null)).toBe(null);
    expect(normalizeDate(undefined)).toBe(undefined);
    expect(normalizeDate("")).toBe("");
  });

  it('passes through canonical DD-MM-YYYY dates', () => {
    expect(normalizeDate("13-05-2026")).toBe("13-05-2026");
  });

  it('converts ISO YYYY-MM-DD to DD-MM-YYYY (boundary convenience)', () => {
    expect(normalizeDate("2026-05-13")).toBe("13-05-2026");
  });

  it('converts DD/MM/YYYY to DD-MM-YYYY', () => {
    expect(normalizeDate("13/05/2026")).toBe("13-05-2026");
  });

  it('zero-pads single-digit days and months', () => {
    expect(normalizeDate("3-5-2026")).toBe("03-05-2026");
    expect(normalizeDate("3/5/2026")).toBe("03-05-2026");
  });

  it('rejects unrecognized strings (returns null)', () => {
    expect(normalizeDate("not a date")).toBe(null);
    expect(normalizeDate("2026/05/13")).toBe(null); // wrong separator order
  });

  it('rejects semantically invalid ISO dates (post-conversion)', () => {
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
    expect(normalizeDate("29-02-2024")).toBe("29-02-2024");
    expect(normalizeDate("2024-02-29")).toBe("29-02-2024"); // ISO → DMY
    expect(normalizeDate("29-02-2026")).toBe(null);
  });
});

describe('dmyToIso / isoToDmy', () => {
  // Boundary converters for HTML `<input type="date">`, which uses ISO
  // YYYY-MM-DD natively. Everywhere else in the app uses DMY.
  it('dmyToIso converts canonical DD-MM-YYYY to ISO', () => {
    expect(dmyToIso("13-05-2026")).toBe("2026-05-13");
  });
  it('dmyToIso passes ISO YYYY-MM-DD through unchanged', () => {
    // Defense-in-depth for code paths that still hand ISO values (e.g.
    // a competition record loaded from a pre-canonicalization save still
    // has an ISO date in state until the next save round-trips it). Pre-fix
    // dmyToIso returned "" for ISO input, which blanked the date picker
    // in the admin UI until the user manually picked a date again.
    expect(dmyToIso("2026-05-13")).toBe("2026-05-13");
  });
  it('dmyToIso returns "" for invalid input', () => {
    expect(dmyToIso("")).toBe("");
    expect(dmyToIso(null)).toBe("");
    expect(dmyToIso("13/05/2026")).toBe("");
    expect(dmyToIso("garbage")).toBe("");
  });
  it('isoToDmy converts ISO YYYY-MM-DD to canonical DD-MM-YYYY', () => {
    expect(isoToDmy("2026-05-13")).toBe("13-05-2026");
  });
  it('isoToDmy passes DMY DD-MM-YYYY through unchanged', () => {
    // Symmetric defense-in-depth for the reverse direction.
    expect(isoToDmy("13-05-2026")).toBe("13-05-2026");
  });
  it('isoToDmy returns "" for invalid input', () => {
    expect(isoToDmy("")).toBe("");
    expect(isoToDmy(null)).toBe("");
    expect(isoToDmy("garbage")).toBe("");
  });
});

describe('compareDmy', () => {
  // Deep-review finding: JS's default `.sort()` does lex compare on
  // strings. That happens to match chronological order for ISO
  // YYYY-MM-DD (the format we used before the cleanup) but NOT for the
  // canonical DMY DD-MM-YYYY: "01-06-2026" (June 1) sorts before
  // "12-05-2026" (May 12) lexically. This caused the multi-day viewer
  // tab strip and the home-page day grouping to show months
  // out-of-order whenever competitions crossed a month boundary.

  it('orders DMY dates chronologically (cross-month)', () => {
    // The bug case: May 12 comes before June 1 chronologically; lex
    // compare gets it wrong. compareDmy must get it right.
    const sorted = ["01-06-2026", "12-05-2026"].sort(compareDmy);
    expect(sorted).toEqual(["12-05-2026", "01-06-2026"]);
  });

  it('orders DMY dates chronologically (cross-year)', () => {
    const sorted = ["01-01-2027", "31-12-2026"].sort(compareDmy);
    expect(sorted).toEqual(["31-12-2026", "01-01-2027"]);
  });

  it('orders DMY dates chronologically (same year, mixed months)', () => {
    const sorted = ["15-03-2026", "01-01-2026", "31-12-2026", "15-07-2026"].sort(compareDmy);
    expect(sorted).toEqual([
      "01-01-2026", "15-03-2026", "15-07-2026", "31-12-2026",
    ]);
  });

  it('puts empty strings first (lexically smallest)', () => {
    const sorted = ["12-05-2026", "", "01-06-2026"].sort(compareDmy);
    expect(sorted).toEqual(["", "12-05-2026", "01-06-2026"]);
  });

  it('falls back to string compare for non-DMY values', () => {
    // Defensive: a stray non-DMY string (shouldn't happen post-validation
    // but we don't want compareDmy to throw on it) sorts by raw value.
    const sorted = ["zzz", "12-05-2026", "aaa"].sort(compareDmy);
    // The two non-DMY entries fall through toKey unchanged; the DMY
    // entry maps to "2026-05-12". Lex order: "12-05-2026"→"2026-05-12",
    // "aaa", "zzz" → ["2026-05-12", "aaa", "zzz"]. Map back: the DMY
    // entry stays as the input "12-05-2026".
    expect(sorted[0]).toBe("12-05-2026");
    expect(sorted[1]).toBe("aaa");
    expect(sorted[2]).toBe("zzz");
  });

  it('is a stable comparator (returns 0 for equal inputs)', () => {
    expect(compareDmy("12-05-2026", "12-05-2026")).toBe(0);
    expect(compareDmy("", "")).toBe(0);
  });

  it('returns negative/positive/zero per the comparator contract', () => {
    expect(compareDmy("12-05-2026", "01-06-2026") < 0).toBe(true);  // earlier
    expect(compareDmy("01-06-2026", "12-05-2026") > 0).toBe(true);  // later
    expect(compareDmy("12-05-2026", "12-05-2026")).toBe(0);          // equal
  });
});

describe('isValidDate', () => {
  // Predicate used by AdminCompetition's "Start competition" button gate.
  // Canonical input is DD-MM-YYYY; ISO accepted as boundary convenience.

  it('accepts a valid DD-MM-YYYY date in range', () => {
    expect(isValidDate("13-05-2026")).toBe(true);
    expect(isValidDate("01-01-1900")).toBe(true); // year boundary
    expect(isValidDate("31-12-2100")).toBe(true); // year boundary
  });

  it('accepts ISO YYYY-MM-DD input that normalizeDate canonicalizes', () => {
    expect(isValidDate("2026-05-13")).toBe(true);
    expect(isValidDate("13/05/2026")).toBe(true);
  });

  it('rejects semantically invalid dates', () => {
    expect(isValidDate("32-13-2026")).toBe(false);
    expect(isValidDate("31-02-2026")).toBe(false);
    expect(isValidDate("00-05-2026")).toBe(false);
    expect(isValidDate("31-04-2026")).toBe(false); // April has 30 days
    expect(isValidDate("29-02-2026")).toBe(false); // non-leap
  });

  it('accepts Feb 29 in a leap year', () => {
    expect(isValidDate("29-02-2024")).toBe(true);
  });

  it('rejects years outside [1900, 2100]', () => {
    expect(isValidDate("31-12-1899")).toBe(false);
    expect(isValidDate("01-01-2101")).toBe(false);
    expect(isValidDate("01-01-0001")).toBe(false);
  });

  it('rejects falsy / empty / undefined input', () => {
    expect(isValidDate("")).toBe(false);
    expect(isValidDate(null)).toBe(false);
    expect(isValidDate(undefined)).toBe(false);
  });

  it('rejects unrecognized strings', () => {
    expect(isValidDate("not a date")).toBe(false);
    expect(isValidDate("2026/05/13")).toBe(false); // wrong separator order
    expect(isValidDate("13.05.2026")).toBe(false);
  });

  it('returns a real boolean (for use in disabled={!isValidDate(...)} props)', () => {
    expect(typeof isValidDate("13-05-2026")).toBe("boolean");
    expect(typeof isValidDate("")).toBe("boolean");
    expect(typeof isValidDate(null)).toBe("boolean");
  });
});

describe('validateAndNormalizeDate', () => {
  // Combined predicate + normalizer used by save flows that need both
  // the user-facing error message AND the normalized date value to save.
  // Two consumers: AdminEditTournament.handleSave, AdminCreateCompetition.create.

  it('returns {norm, error: null} for a valid DD-MM-YYYY date', () => {
    expect(validateAndNormalizeDate("13-05-2026")).toEqual({
      norm: "13-05-2026",
      error: null,
    });
  });

  it('normalizes ISO YYYY-MM-DD input to canonical DD-MM-YYYY', () => {
    expect(validateAndNormalizeDate("2026-05-13")).toEqual({
      norm: "13-05-2026",
      error: null,
    });
  });

  it('returns the "Invalid date" message for semantically invalid input', () => {
    expect(validateAndNormalizeDate("32-13-2026")).toEqual({
      norm: null,
      error: "Invalid date. Please pick a valid day.",
    });
    expect(validateAndNormalizeDate("29-02-2026")).toEqual({
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
    expect(validateAndNormalizeDate("31-12-1899")).toEqual({
      norm: null,
      error: "Year must be between 1900 and 2100.",
    });
    expect(validateAndNormalizeDate("01-01-2101")).toEqual({
      norm: null,
      error: "Year must be between 1900 and 2100.",
    });
  });

  it('accepts year boundary values 1900 and 2100', () => {
    expect(validateAndNormalizeDate("01-01-1900").error).toBe(null);
    expect(validateAndNormalizeDate("31-12-2100").error).toBe(null);
  });

  it('returns the canonical DATE_ERR_* constants (lockstep with saveNow)', () => {
    // saveNow in admin_competition.jsx delegates to validateAndNormalizeDate
    // for the canonical error string. This mechanically guarantees the
    // four date-validation sites can't drift on error messages.
    expect(validateAndNormalizeDate("not a date").error).toBe(DATE_ERR_INVALID_FORMAT);
    expect(validateAndNormalizeDate("01-01-1850").error).toBe(DATE_ERR_YEAR_RANGE);
  });

  it('DATE_ERR_* constants have the expected user-facing strings', () => {
    // Pin the user-visible text so an accidental string change is
    // caught by tests (the test failure tells whoever changes it to
    // also update any screenshot fixtures, docs, etc.). DATE_ERR_YEAR_RANGE
    // is a template literal built from MIN_YEAR/MAX_YEAR: if you bump
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
    //
    // Mirrors helper.MinDateYear / helper.MaxDateYear in
    // internal/helper/constants.go: the Go HTTP handlers
    // (validateDateDMY in handlers_tournament.go) reject out-of-range
    // years on every write path. Bumping these here without bumping
    // the Go side (or vice versa) would let the UI offer dates the
    // backend rejects (or land dates the UI then refuses to render).
    // The Go pin tests in constants_test.go assert the same literals.
    expect(MIN_YEAR).toBe(1900);
    expect(MAX_YEAR).toBe(2100);
  });

  it('MAX_TEAM_SIZE matches kendo team-position conventions', () => {
    // Pin the current cap. admin_scoring_modal.jsx builds
    // TEAM_POSITIONS from this; if you bump it, also extend the
    // scoring UI / docs / screenshot fixtures.
    expect(MAX_TEAM_SIZE).toBe(9);
  });

  it('MAX_COURTS matches A–Z labelling cap (lockstep with helper.MaxCourts)', () => {
    // Pin the courts cap. Anchored to the single-letter A..Z labelling
    // used on Shiaijo headers in the Excel output and in
    // CourtPicker / admin_setup.jsx's courts input. The Go side
    // declares the same value at internal/helper/constants.go as
    // `MaxCourts`. Bumping past 26 here without bumping there (or
    // vice versa) would let the UI offer values the backend rejects.
    expect(MAX_COURTS).toBe(26);
  });

  it('MAX_RANK matches helper.MaxRankOverride (Go-side overflow cap)', () => {
    // Pin the rank-override absolute cap. The override-rank handler
    // ALSO validates against the actual pool size (real semantic
    // constraint); this constant is the defense-in-depth overflow
    // guard. Mirrors helper.MaxRankOverride in
    // internal/helper/constants.go: keep both in lockstep.
    expect(MAX_RANK).toBe(1000);
  });

  it('validateAndNormalizeDate predicate matches MIN_YEAR/MAX_YEAR bounds', () => {
    // The boundary cases must be in lockstep with the constants
    // (off-by-one regression guard).
    expect(validateAndNormalizeDate(`${MIN_YEAR}-01-01`).error).toBe(null);
    expect(validateAndNormalizeDate(`${MAX_YEAR}-12-31`).error).toBe(null);
    expect(validateAndNormalizeDate(`${MIN_YEAR - 1}-12-31`).error).toBe(DATE_ERR_YEAR_RANGE);
    expect(validateAndNormalizeDate(`${MAX_YEAR + 1}-01-01`).error).toBe(DATE_ERR_YEAR_RANGE);
  });

  it('isValidDate is a thin wrapper that returns error === null', () => {
    // Verify the two helpers stay in lockstep: isValidDate is now
    // implemented as `validateAndNormalizeDate(d).error === null`.
    const cases = ["13-05-2026", "2026-05-13", "32-13-2026", "31-12-1899", "", null];
    cases.forEach((c) => {
      expect(isValidDate(c)).toBe(validateAndNormalizeDate(c).error === null);
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
    it('"0" with min=1 → no save (the original bug: backend rejected 0)', () => {
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
      // `+"5abc"` is NaN, not 5: JS coercion doesn't substring-parse.
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

describe('getScoreBtnClass', () => {
  it('returns active variant for non-completed matches', () => {
    expect(getScoreBtnClass('scheduled')).toBe('score-btn score-btn--active');
    expect(getScoreBtnClass('running')).toBe('score-btn score-btn--active');
    expect(getScoreBtnClass(null)).toBe('score-btn score-btn--active');
    expect(getScoreBtnClass(undefined)).toBe('score-btn score-btn--active');
  });

  it('returns correct variant for completed matches', () => {
    expect(getScoreBtnClass('completed')).toBe('score-btn score-btn--correct');
  });
});

describe('deriveTournamentDays', () => {
  // This mirrors Tournament.Days() in internal/state/models.go. These cases
  // guard against frontend/backend drift in day derivation (mp-ehf).
  it('single day → one entry equal to the start date', () => {
    expect(deriveTournamentDays('05-06-2026', 1)).toEqual(['05-06-2026']);
  });

  it('multi-day → contiguous DD-MM-YYYY list starting at Day 1', () => {
    expect(deriveTournamentDays('05-06-2026', 3)).toEqual(['05-06-2026', '06-06-2026', '07-06-2026']);
  });

  it('rolls over month boundary', () => {
    expect(deriveTournamentDays('30-06-2026', 3)).toEqual(['30-06-2026', '01-07-2026', '02-07-2026']);
  });

  it('rolls over year boundary', () => {
    expect(deriveTournamentDays('31-12-2026', 2)).toEqual(['31-12-2026', '01-01-2027']);
  });

  it('handles leap-year February correctly', () => {
    // 2028 is a leap year: 28 Feb → 29 Feb → 1 Mar.
    expect(deriveTournamentDays('28-02-2028', 3)).toEqual(['28-02-2028', '29-02-2028', '01-03-2028']);
  });

  it('normalizes a non-canonical (ISO) start date before deriving', () => {
    // normalizeDate accepts ISO and converts to DD-MM-YYYY.
    expect(deriveTournamentDays('2026-06-05', 2)).toEqual(['05-06-2026', '06-06-2026']);
  });

  it('derives across the full supported duration cap', () => {
    const days = deriveTournamentDays('01-01-2026', MAX_TOURNAMENT_DURATION_DAYS);
    expect(days).toHaveLength(MAX_TOURNAMENT_DURATION_DAYS);
    expect(days[0]).toBe('01-01-2026');
    expect(days[MAX_TOURNAMENT_DURATION_DAYS - 1]).toBe('30-01-2026');
  });

  it('returns [] for empty / missing start date', () => {
    expect(deriveTournamentDays('', 3)).toEqual([]);
    expect(deriveTournamentDays(null, 3)).toEqual([]);
    expect(deriveTournamentDays(undefined, 3)).toEqual([]);
  });

  it('returns [] for an unparseable start date', () => {
    expect(deriveTournamentDays('not-a-date', 3)).toEqual([]);
    expect(deriveTournamentDays('31-02-2026', 3)).toEqual([]); // Feb 31 invalid
  });

  it('returns [] for invalid durations', () => {
    expect(deriveTournamentDays('05-06-2026', 0)).toEqual([]);
    expect(deriveTournamentDays('05-06-2026', -1)).toEqual([]);
    expect(deriveTournamentDays('05-06-2026', 1.5)).toEqual([]); // non-integer
    expect(deriveTournamentDays('05-06-2026', NaN)).toEqual([]);
    expect(deriveTournamentDays('05-06-2026', '3')).toEqual([]); // non-number
  });
});

describe('normalizeCourts', () => {
  it('returns the array if it is a non-empty array', () => {
    expect(normalizeCourts(["A", "B"])).toEqual(["A", "B"]);
  });

  it('returns ["A"] for null or undefined', () => {
    expect(normalizeCourts(null)).toEqual(["A"]);
    expect(normalizeCourts(undefined)).toEqual(["A"]);
  });

  it('returns ["A"] for an empty array', () => {
    expect(normalizeCourts([])).toEqual(["A"]);
  });

  it('returns ["A"] for a truthy non-array like a string', () => {
    expect(normalizeCourts("AB")).toEqual(["A"]);
  });
});

describe('courtCount', () => {
  it('returns the length of the array', () => {
    expect(courtCount(["A", "B"])).toBe(2);
  });

  it('returns 1 for null or undefined', () => {
    expect(courtCount(null)).toBe(1);
    expect(courtCount(undefined)).toBe(1);
  });

  it('returns 1 for an empty array', () => {
    expect(courtCount([])).toBe(1);
  });

  it('returns 1 for a truthy non-array like a string', () => {
    expect(courtCount("AB")).toBe(1);
  });
});

describe('resolveRoundIndex', () => {
  it('returns m.roundIndex when non-negative', () => {
    expect(resolveRoundIndex({ roundIndex: 0 })).toBe(0);
    expect(resolveRoundIndex({ roundIndex: 3 })).toBe(3);
  });

  it('falls back to numeric m.round when roundIndex is absent', () => {
    expect(resolveRoundIndex({ round: 2 })).toBe(2);
  });

  it('clamps negative numeric round to 0 via fallback', () => {
    expect(resolveRoundIndex({ round: -1 })).toBe(0);
  });

  it('returns 0 for pool matches with no roundIndex or numeric round', () => {
    expect(resolveRoundIndex({ round: 'Pool A', status: 'completed' })).toBe(0);
  });

  it('returns 0 when match has no round fields at all', () => {
    expect(resolveRoundIndex({})).toBe(0);
  });

  it('prefers roundIndex over numeric round', () => {
    expect(resolveRoundIndex({ roundIndex: 1, round: 5 })).toBe(1);
  });
});
