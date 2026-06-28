import { describe, it, expect } from 'vitest';
import { mergeMatchPatch } from '../data.jsx';

describe('mergeMatchPatch', () => {
  it('preserves existing court when patch.court is empty string', () => {
    const existing = { id: "m1", court: "A", scheduledAt: "09:30", status: "scheduled" };
    const patch = { winner: "P1", court: "", status: "completed" };
    const merged = mergeMatchPatch(existing, patch);
    expect(merged.court).toBe("A");
    expect(merged.status).toBe("completed");
    expect(merged.winner).toBe("P1");
  });

  it('preserves existing scheduledAt when patch.scheduledAt is empty string', () => {
    const existing = { id: "m1", court: "A", scheduledAt: "09:30" };
    const patch = { winner: "P1", scheduledAt: "" };
    const merged = mergeMatchPatch(existing, patch);
    expect(merged.scheduledAt).toBe("09:30");
  });

  it('preserves existing court when patch.court is null', () => {
    const existing = { id: "m1", court: "A", scheduledAt: "09:30" };
    const patch = { winner: "P1", court: null };
    const merged = mergeMatchPatch(existing, patch);
    expect(merged.court).toBe("A");
  });

  it('preserves existing scheduledAt when patch.scheduledAt is undefined', () => {
    const existing = { id: "m1", court: "A", scheduledAt: "09:30" };
    const patch = { winner: "P1" };
    const merged = mergeMatchPatch(existing, patch);
    expect(merged.scheduledAt).toBe("09:30");
  });

  it('lets non-empty patch.court override existing court', () => {
    const existing = { id: "m1", court: "A", scheduledAt: "09:30" };
    const patch = { court: "B" };
    const merged = mergeMatchPatch(existing, patch);
    expect(merged.court).toBe("B");
  });

  it('lets non-empty patch.scheduledAt override existing scheduledAt', () => {
    const existing = { id: "m1", court: "A", scheduledAt: "09:30" };
    const patch = { scheduledAt: "10:00" };
    const merged = mergeMatchPatch(existing, patch);
    expect(merged.scheduledAt).toBe("10:00");
  });

  it('only guards court and scheduledAt : other fields follow normal spread semantics', () => {
    const existing = { id: "m1", winner: "P1", status: "completed", court: "A" };
    // status: "" is a normal empty string and *should* overwrite (we only special-case scheduling fields)
    const patch = { status: "" };
    const merged = mergeMatchPatch(existing, patch);
    expect(merged.status).toBe("");
    expect(merged.court).toBe("A"); // still preserved
  });

  it('does not mutate the existing match object', () => {
    const existing = { id: "m1", court: "A", scheduledAt: "09:30" };
    const patch = { court: "B" };
    mergeMatchPatch(existing, patch);
    expect(existing.court).toBe("A");
    expect(existing.scheduledAt).toBe("09:30");
  });
});
