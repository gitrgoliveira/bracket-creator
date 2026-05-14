import { describe, it, expect } from 'vitest';
import { timeEdited, timeToMinutes, clampMatchDuration } from '../admin_schedule.jsx';

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
