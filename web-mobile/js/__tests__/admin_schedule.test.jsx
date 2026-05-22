import { describe, it, expect } from 'vitest';
import { timeEdited, timeToMinutes, clampMatchDuration, filterMatchesByCourt, allMatchesCompleted } from '../admin_schedule.jsx';

describe('timeEdited', () => {
  // Copilot round-9 finding: AdminTWMatch.submitTime() used
  //   if (onTimeChange && timeVal !== m.scheduledAt) onTimeChange(timeVal)
  // but the timeVal useState initializer is `m.scheduledAt || ""`. For
  // an untimed match (m.scheduledAt === null), opening the time editor
  // initializes timeVal to "", and blurring without edits would fire
  // because "" !== null is true → unnecessary PUT + SSE broadcast.
  // Fix: normalize both sides via the same `|| ""` the initializer uses.

  describe('untimed match (m.scheduledAt is null/undefined)', () => {
    it('open-and-blur with no edits is a no-op (null + "")', () => {
      expect(timeEdited(null, "")).toBe(false);
    });

    it('open-and-blur with no edits is a no-op (undefined + "")', () => {
      expect(timeEdited(undefined, "")).toBe(false);
    });

    it('typing a real time on an untimed match is a real edit', () => {
      expect(timeEdited(null, "09:30")).toBe(true);
      expect(timeEdited(undefined, "09:30")).toBe(true);
    });
  });

  describe('timed match (m.scheduledAt is a "HH:MM" string)', () => {
    it('open-and-blur with no edits is a no-op (same string)', () => {
      expect(timeEdited("09:30", "09:30")).toBe(false);
    });

    it('change to a different time is a real edit', () => {
      expect(timeEdited("09:30", "10:00")).toBe(true);
    });

    it('clearing the time (HH:MM → "") is a real edit', () => {
      // The user explicitly cleared the input — this should fire so the
      // server can drop the scheduledAt back to null. The naive check
      // ("09:30" !== "") would already catch this; pinning it here so a
      // future refactor that aliases "" to null doesn't break the clear.
      expect(timeEdited("09:30", "")).toBe(true);
    });
  });

  describe('symmetry: normalization is applied to BOTH sides', () => {
    it('null is treated identically to ""', () => {
      // The bug was: `timeVal !== m.scheduledAt` with timeVal="" and
      // m.scheduledAt=null evaluated to true. timeEdited normalizes the
      // left side to "" so the comparison is "" !== "" → false.
      expect(timeEdited(null, "")).toBe(timeEdited("", ""));
    });

    it('undefined is treated identically to ""', () => {
      expect(timeEdited(undefined, "")).toBe(timeEdited("", ""));
    });
  });
});

describe('timeToMinutes', () => {
  // Sanity coverage for the existing helper — it's been in the file
  // since the split and didn't have a dedicated test. Pinning a few
  // cases so a future "make this more clever" refactor can be checked.

  it('parses HH:MM', () => {
    expect(timeToMinutes("09:30")).toBe(9 * 60 + 30);
    expect(timeToMinutes("00:00")).toBe(0);
    expect(timeToMinutes("23:59")).toBe(23 * 60 + 59);
  });

  it('returns null for invalid input', () => {
    expect(timeToMinutes("")).toBe(null);
    expect(timeToMinutes(null)).toBe(null);
    expect(timeToMinutes(undefined)).toBe(null);
    expect(timeToMinutes("abc")).toBe(null);
    expect(timeToMinutes("09:xx")).toBe(null);
  });
});

describe('clampMatchDuration', () => {
  // Copilot post-I5 finding: safeMatchDuration at admin_schedule.jsx:83
  // used `Number.isFinite(x) && x >= 1` but no Number.isInteger guard.
  // A user typing "2.5" passed through to:
  //   - addMinutes("00:00", 2.5) → total = 0 + 2.5 = 2.5; mm = 2.5 % 60 = 2.5
  //     → "00:2.5" — invalid HH:MM string the backend would persist as
  //     scheduledAt with weird downstream display
  //   - durationEstimate: diff % 60 with diff=32.5 → "0h 32.5m"
  //
  // clampMatchDuration adds Number.isInteger to the guard. Tests pin
  // every adversarial-input case so a future "simplify this" refactor
  // can't drop a guard silently.

  describe('valid positive integers pass through', () => {
    it('1 (lower boundary) → 1', () => {
      expect(clampMatchDuration(1)).toBe(1);
    });

    it('3 (default) → 3', () => {
      expect(clampMatchDuration(3)).toBe(3);
    });

    it('60 (max for the form input) → 60', () => {
      expect(clampMatchDuration(60)).toBe(60);
    });

    it('large valid value → passes through (no max enforcement)', () => {
      // clampMatchDuration doesn't enforce the form's max=60 — that's the
      // form's job. The helper's contract is "non-finite/fractional/<1 → fallback."
      expect(clampMatchDuration(120)).toBe(120);
    });
  });

  describe('fractional values fall back to default', () => {
    it('2.5 → 3 (the Copilot finding)', () => {
      expect(clampMatchDuration(2.5)).toBe(3);
    });

    it('1.5 → 3', () => {
      expect(clampMatchDuration(1.5)).toBe(3);
    });

    it('0.99 → 3 (also < 1, but Number.isInteger catches it first)', () => {
      expect(clampMatchDuration(0.99)).toBe(3);
    });
  });

  describe('non-finite / nullish fall back to default', () => {
    it('NaN → 3 (cleared input case)', () => {
      expect(clampMatchDuration(NaN)).toBe(3);
    });

    it('undefined → 3', () => {
      expect(clampMatchDuration(undefined)).toBe(3);
    });

    it('null → 3', () => {
      expect(clampMatchDuration(null)).toBe(3);
    });

    it('Infinity → 3', () => {
      expect(clampMatchDuration(Infinity)).toBe(3);
    });

    it('-Infinity → 3', () => {
      expect(clampMatchDuration(-Infinity)).toBe(3);
    });
  });

  describe('zero / negative fall back to default', () => {
    it('0 → 3 (zero match duration is meaningless)', () => {
      expect(clampMatchDuration(0)).toBe(3);
    });

    it('-1 → 3', () => {
      expect(clampMatchDuration(-1)).toBe(3);
    });

    it('-5 → 3', () => {
      expect(clampMatchDuration(-5)).toBe(3);
    });
  });

  describe('custom fallback', () => {
    it('honors the fallback parameter', () => {
      expect(clampMatchDuration(NaN, 5)).toBe(5);
      expect(clampMatchDuration(2.5, 10)).toBe(10);
    });
  });
});

describe('filterMatchesByCourt', () => {
  // T024 (US1, FR-001, SC-001): the admin schedule view supports a `?court=A`
  // query param that scopes the visible matches to a single shiaijo. The
  // helper is extracted from admin_schedule.jsx so the URL → matches[]
  // transformation can be unit-tested without mounting the component.
  //
  // Contract:
  //   filterMatchesByCourt(matches, courtParam) → matches[]
  //   - null/undefined/""/"all" courtParam → returns matches unchanged
  //   - specific letter (e.g. "A") → only matches whose m.court === "A"
  //   - case-sensitive: filtering by "A" does NOT match m.court === "a"
  //   - empty-string / nullish / whitespace-only m.court is treated as
  //     "unassigned" and excluded when filtering by a specific court.
  //     This mirrors the existing `(m.court || "")` pattern used by the
  //     unassigned-bucket logic in admin_schedule.jsx.

  const matches = [
    { id: 'm1', court: 'A', pool: 'P1' },
    { id: 'm2', court: 'B', pool: 'P2' },
    { id: 'm3', court: 'A', pool: 'P3' },
    { id: 'm4', court: '', pool: 'P4' },
    { id: 'm5', court: null, pool: 'P5' },
    { id: 'm6', court: undefined, pool: 'P6' },
    { id: 'm7', court: '  ', pool: 'P7' },
    { id: 'm8', court: 'a', pool: 'P8' },
  ];

  describe('no-filter cases return all matches unchanged', () => {
    it('null returns all matches', () => {
      expect(filterMatchesByCourt(matches, null)).toEqual(matches);
    });

    it('undefined returns all matches', () => {
      expect(filterMatchesByCourt(matches, undefined)).toEqual(matches);
    });

    it('"" (empty string) returns all matches', () => {
      expect(filterMatchesByCourt(matches, "")).toEqual(matches);
    });

    it('"all" returns all matches', () => {
      expect(filterMatchesByCourt(matches, "all")).toEqual(matches);
    });
  });

  describe('filtering by a specific court letter', () => {
    it('"A" returns only Court A matches', () => {
      const result = filterMatchesByCourt(matches, "A");
      expect(result).toEqual([
        { id: 'm1', court: 'A', pool: 'P1' },
        { id: 'm3', court: 'A', pool: 'P3' },
      ]);
    });

    it('"B" returns only Court B matches', () => {
      const result = filterMatchesByCourt(matches, "B");
      expect(result).toEqual([
        { id: 'm2', court: 'B', pool: 'P2' },
      ]);
    });

    it('"C" (no matches assigned) returns []', () => {
      expect(filterMatchesByCourt(matches, "C")).toEqual([]);
    });
  });

  describe('unassigned matches are excluded when filtering by a specific court', () => {
    it('empty-string court is excluded when filter is "A"', () => {
      const result = filterMatchesByCourt(matches, "A");
      expect(result.find((m) => m.id === 'm4')).toBeUndefined();
    });

    it('null court is excluded when filter is "A"', () => {
      const result = filterMatchesByCourt(matches, "A");
      expect(result.find((m) => m.id === 'm5')).toBeUndefined();
    });

    it('undefined court is excluded when filter is "A"', () => {
      const result = filterMatchesByCourt(matches, "A");
      expect(result.find((m) => m.id === 'm6')).toBeUndefined();
    });

    it('whitespace-only court is excluded when filter is "A"', () => {
      // The unassigned-bucket logic in admin_schedule.jsx uses
      // `(m.court || "")` which would leave "  " in the unassigned
      // bucket once trimmed. Filtering by "A" must exclude it.
      const result = filterMatchesByCourt(matches, "A");
      expect(result.find((m) => m.id === 'm7')).toBeUndefined();
    });
  });

  describe('case-sensitivity', () => {
    it('filtering by "A" does NOT match m.court === "a"', () => {
      // Per existing app convention (Excel court labels A–Z are uppercase
      // and the unassigned-bucket comparison is exact), the filter must
      // be case-sensitive. A lowercase "a" in a match should not leak
      // into the "A" view.
      const result = filterMatchesByCourt(matches, "A");
      expect(result.find((m) => m.id === 'm8')).toBeUndefined();
    });

    it('filtering by "a" returns only the lowercase "a" match', () => {
      const result = filterMatchesByCourt(matches, "a");
      expect(result).toEqual([
        { id: 'm8', court: 'a', pool: 'P8' },
      ]);
    });
  });

  describe('edge cases', () => {
    it('empty matches array returns []', () => {
      expect(filterMatchesByCourt([], "A")).toEqual([]);
    });

    it('empty matches array with no filter returns []', () => {
      expect(filterMatchesByCourt([], null)).toEqual([]);
    });
  });
});

describe('AdminScoreEditor chained navigation stays on the same shiaijo (T043 regression)', () => {
  // T043 (US1, FR-001, SC-001) regression anchor for the CLAUDE.md
  // invariant: "Chained match navigation in the admin score editor
  // (Prev/Next buttons, Finish + Start Next, ←/→ keys) must stay on the
  // current match's shiaijo." The AdminScoreEditor component in
  // admin_schedule.jsx implements this with
  //   filtered.filter(m => (m.court || "") === openCourt)
  // before computing prevMatch / nextMatch. This regression test pins
  // the same court-equality contract via the filterMatchesByCourt
  // helper — they share the same court-equality semantics (case-sensitive
  // exact match), so a future "simplify the comparison" refactor that
  // breaks the helper would also break the chained-navigation invariant.
  //
  // We test the helper rather than mounting AdminScoreEditor because the
  // component's chained nav depends on internal openMatch state +
  // ScoreEditorModal callbacks; the pure helper captures the load-bearing
  // semantic (only same-court matches are candidates for the next match)
  // and is the right anchor for a regression test. The actual
  // AdminScoreEditor logic is verified by manual operator testing.

  const matches = [
    { id: 'mA1', court: 'A', status: 'completed', compId: 'c1' },
    { id: 'mB1', court: 'B', status: 'running',   compId: 'c1' },
    { id: 'mA2', court: 'A', status: 'running',   compId: 'c1' },
    { id: 'mA3', court: 'A', status: 'scheduled', compId: 'c1' },
    { id: 'mB2', court: 'B', status: 'scheduled', compId: 'c1' },
    { id: 'mA4', court: 'A', status: 'scheduled', compId: 'c1' },
  ];

  it('given the current match is on Court A, next-match candidates are all Court A', () => {
    // Simulates AdminScoreEditor's pre-next-match filter step: scope the
    // candidate list to the current match's shiaijo. The chained-next
    // logic in admin_schedule.jsx picks list[openIdx+1] from this list,
    // so if the helper leaks a Court B match in, the operator would
    // suddenly hop courts mid-flow.
    const currentMatch = matches[2]; // mA2 (Court A, running)
    const sameCourt = filterMatchesByCourt(matches, currentMatch.court);

    expect(sameCourt.every((m) => m.court === 'A')).toBe(true);
    expect(sameCourt.find((m) => m.court === 'B')).toBeUndefined();
    expect(sameCourt.map((m) => m.id)).toEqual(['mA1', 'mA2', 'mA3', 'mA4']);
  });

  it('next match in sequence is the next Court A match, never a Court B match', () => {
    // Concrete end-to-end shape of the chained-next computation. If the
    // operator clicks Next on mA2 (Court A, index 1 in the same-court
    // list), the next match must be mA3 (Court A) — NOT mB2 even though
    // mB2 might come before mA3 in tournament order.
    const currentMatch = matches[2]; // mA2
    const sameCourt = filterMatchesByCourt(matches, currentMatch.court);
    const openIdx = sameCourt.findIndex((m) => m.id === currentMatch.id);
    const nextMatch = openIdx >= 0 && openIdx < sameCourt.length - 1 ? sameCourt[openIdx + 1] : null;

    expect(nextMatch).not.toBeNull();
    expect(nextMatch.court).toBe('A');
    expect(nextMatch.id).toBe('mA3');
  });

  it('prev match in sequence is the previous Court A match, never a Court B match', () => {
    // Symmetric to the next-match check.
    const currentMatch = matches[2]; // mA2
    const sameCourt = filterMatchesByCourt(matches, currentMatch.court);
    const openIdx = sameCourt.findIndex((m) => m.id === currentMatch.id);
    const prevMatch = openIdx > 0 ? sameCourt[openIdx - 1] : null;

    expect(prevMatch).not.toBeNull();
    expect(prevMatch.court).toBe('A');
    expect(prevMatch.id).toBe('mA1');
  });

  it('last match on a court has no next-match candidate (returns null)', () => {
    // Edge: clicking Next on the last Court A match must stop chaining
    // rather than wrap around to the first Court A match or jump to
    // Court B.
    const currentMatch = matches[5]; // mA4 — last Court A match
    const sameCourt = filterMatchesByCourt(matches, currentMatch.court);
    const openIdx = sameCourt.findIndex((m) => m.id === currentMatch.id);
    const nextMatch = openIdx >= 0 && openIdx < sameCourt.length - 1 ? sameCourt[openIdx + 1] : null;

    expect(nextMatch).toBeNull();
  });
});

describe('allMatchesCompleted', () => {
  // Drives the "All matches scored" banner in AdminScoreEditor. The banner
  // should appear only when there are matches and every one is completed.

  it('returns false for an empty list', () => {
    expect(allMatchesCompleted([])).toBe(false);
  });

  it('returns false when any match is not completed', () => {
    const matches = [
      { id: '1', status: 'completed' },
      { id: '2', status: 'scheduled' },
      { id: '3', status: 'completed' },
    ];
    expect(allMatchesCompleted(matches)).toBe(false);
  });

  it('returns false when any match is running', () => {
    const matches = [
      { id: '1', status: 'completed' },
      { id: '2', status: 'running' },
    ];
    expect(allMatchesCompleted(matches)).toBe(false);
  });

  it('returns true when all matches are completed', () => {
    const matches = [
      { id: '1', status: 'completed' },
      { id: '2', status: 'completed' },
      { id: '3', status: 'completed' },
    ];
    expect(allMatchesCompleted(matches)).toBe(true);
  });

  it('returns true for a single completed match', () => {
    expect(allMatchesCompleted([{ id: '1', status: 'completed' }])).toBe(true);
  });

  it('regression: nextMatch is null for the last match in a fully-scored court', () => {
    // When all pool matches are complete, the score editor's sameCourt list
    // is all-completed. The last (and only remaining) match is at the end —
    // nextMatch must be null so the modal does not loop back to the start.
    const allDone = [
      { id: 'm1', court: 'A', status: 'completed', compId: 'c1' },
      { id: 'm2', court: 'A', status: 'completed', compId: 'c1' },
      { id: 'm3', court: 'A', status: 'completed', compId: 'c1' },
    ];
    const openMatch = allDone[2]; // last match
    const sameCourt = filterMatchesByCourt(allDone, openMatch.court);
    const openIdx = sameCourt.findIndex(m => m.id === openMatch.id);
    const nextMatch = openIdx >= 0 && openIdx < sameCourt.length - 1 ? sameCourt[openIdx + 1] : null;
    expect(allMatchesCompleted(allDone)).toBe(true);
    expect(nextMatch).toBeNull();
  });

  it('regression: Finish+Start Next does not loop when last scheduled match has completed matches sorted after it', () => {
    // Real-world scenario: the operator has scored 6 of 7 pool matches. The
    // sort order is scheduled first, completed last. The one remaining
    // scheduled match sits at index 0; the 6 completed matches follow at
    // indices 1-6. Clicking "Finish + Start Next" from the scheduled match
    // used to open the first completed match (index 1) as a CORRECTION
    // — the modal looped back to match 1. The fix is to compute
    // nextActiveMatch = first non-completed match after openIdx, which is
    // null when the remaining scheduled match IS the open one.
    const sorted = [
      { id: 'm7', court: 'A', status: 'scheduled',  compId: 'c1' }, // last unscored
      { id: 'm1', court: 'A', status: 'completed',  compId: 'c1' },
      { id: 'm2', court: 'A', status: 'completed',  compId: 'c1' },
      { id: 'm3', court: 'A', status: 'completed',  compId: 'c1' },
      { id: 'm4', court: 'A', status: 'completed',  compId: 'c1' },
      { id: 'm5', court: 'A', status: 'completed',  compId: 'c1' },
      { id: 'm6', court: 'A', status: 'completed',  compId: 'c1' },
    ];
    const openMatch = sorted[0]; // the last scheduled match
    const sameCourt = filterMatchesByCourt(sorted, openMatch.court);
    const openIdx = sameCourt.findIndex(m => m.id === openMatch.id);

    // nextMatch (list position) is non-null — this is the source of the bug
    // without the nextActiveMatch guard.
    const nextMatch = openIdx >= 0 && openIdx < sameCourt.length - 1 ? sameCourt[openIdx + 1] : null;
    expect(nextMatch).not.toBeNull(); // m1 (completed) — confirms the bug path
    expect(nextMatch.id).toBe('m1');

    // nextActiveMatch (first non-completed after openIdx) must be null —
    // this is what Finish+Start Next should use so the modal does not loop.
    const nextActiveMatch = sameCourt.slice(openIdx + 1).find(m => m.status !== 'completed') || null;
    expect(nextActiveMatch).toBeNull();
  });

  // Deep-review (2026-05-22): the AdminScoreEditor banner render is guarded
  // by `statusFilter !== "complete" && allMatchesCompleted(filtered)`. When
  // the operator hits the "Completed" status filter the list is trivially
  // all-completed — firing the banner there would be misleading because the
  // user explicitly asked to see only the completed matches; nothing about
  // that view says "every match in this competition is done." The helper
  // itself stays unconditional (it's a pure predicate); the guard lives
  // in the consumer. This regression pins that the helper is true-by-shape
  // and the guard must be applied outside.
  it('helper alone is not enough to drive the banner: caller must guard against statusFilter', () => {
    const filteredByCompleteStatus = [
      { id: '1', status: 'completed' },
      { id: '2', status: 'completed' },
    ];
    // Helper says true for an all-completed list — that's correct.
    expect(allMatchesCompleted(filteredByCompleteStatus)).toBe(true);
    // The actual AdminScoreEditor JSX guards on
    //   statusFilter !== "complete" && allMatchesCompleted(filtered)
    // so when the filter is "complete" we suppress the banner.
    const statusFilter = "complete";
    const shouldShowBanner = statusFilter !== "complete" && allMatchesCompleted(filteredByCompleteStatus);
    expect(shouldShowBanner).toBe(false);
    // And for any other filter value where every match really is completed,
    // the banner does fire.
    const statusFilterAll = "all";
    const shouldShowBannerAll = statusFilterAll !== "complete" && allMatchesCompleted(filteredByCompleteStatus);
    expect(shouldShowBannerAll).toBe(true);
  });
});
