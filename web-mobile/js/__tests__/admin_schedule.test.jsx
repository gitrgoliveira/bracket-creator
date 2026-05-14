import { describe, it, expect } from 'vitest';
import { timeEdited, timeToMinutes } from '../admin_schedule.jsx';

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
