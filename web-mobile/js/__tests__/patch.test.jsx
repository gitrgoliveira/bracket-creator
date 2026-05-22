import { describe, it, expect } from 'vitest';
import { applyPatch, recomputeQueuePositions, recomputeBracketQueuePositions } from '../patch.jsx';

// Tests for the centralised SSE-patch applier (Slice 0 / NFR-006).
// applyPatch composes mergeMatchPatch (covered in mergeMatchPatch.test.jsx)
// and adds: result/results dual-form acceptance, bracket ippons→score
// mapping, and identity-preservation when no IDs match.

const makeState = () => ({
  poolMatches: [
    { id: "p1", court: "A", scheduledAt: "09:30", status: "scheduled" },
    { id: "p2", court: "B", scheduledAt: "10:00", status: "scheduled" },
  ],
  bracket: {
    rounds: [
      [
        { id: "b1", court: "A", scheduledAt: "11:00", status: "scheduled" },
        { id: "b2", court: "A", scheduledAt: "11:00", status: "scheduled" },
      ],
      [
        { id: "b3", court: "A", scheduledAt: "12:00", status: "scheduled" },
      ],
    ],
  },
});

describe('applyPatch', () => {
  it('returns prev unchanged when event has neither result nor results', () => {
    const prev = makeState();
    const next = applyPatch(prev, { data: { competitionId: "c1" } });
    expect(next).toBe(prev);
  });

  it('returns prev unchanged when no listed result IDs match any match', () => {
    const prev = makeState();
    const next = applyPatch(prev, { data: { result: { id: "does-not-exist", winner: "X" } } });
    expect(next).toBe(prev);
  });

  it('applies a single result to the matching poolMatch', () => {
    const prev = makeState();
    const next = applyPatch(prev, { data: { result: { id: "p1", winner: "Alice", status: "completed" } } });
    expect(next).not.toBe(prev);
    expect(next.poolMatches[0].winner).toBe("Alice");
    expect(next.poolMatches[0].status).toBe("completed");
    // unchanged matches keep their reference and values
    expect(next.poolMatches[1]).toBe(prev.poolMatches[1]);
  });

  it('applies an array of results to multiple poolMatches', () => {
    const prev = makeState();
    const next = applyPatch(prev, {
      data: {
        results: [
          { id: "p1", winner: "Alice" },
          { id: "p2", winner: "Bob" },
        ],
      },
    });
    expect(next.poolMatches[0].winner).toBe("Alice");
    expect(next.poolMatches[1].winner).toBe("Bob");
  });

  it('lets results take precedence over result when both present', () => {
    const prev = makeState();
    const next = applyPatch(prev, {
      data: {
        result: { id: "p1", winner: "FromSingleResult" },
        results: [{ id: "p1", winner: "FromArray" }],
      },
    });
    expect(next.poolMatches[0].winner).toBe("FromArray");
  });

  it('maps ipponsA/B to scoreA/B on bracket-round matches', () => {
    const prev = makeState();
    const next = applyPatch(prev, {
      data: {
        result: { id: "b1", winner: "Alice", ipponsA: ["M", "K"], ipponsB: ["D"], status: "completed" },
      },
    });
    expect(next.bracket.rounds[0][0].winner).toBe("Alice");
    expect(next.bracket.rounds[0][0].scoreA).toBe("MK");
    expect(next.bracket.rounds[0][0].scoreB).toBe("D");
    // ipponsA/B copied through too (the patch spread carries them)
    expect(next.bracket.rounds[0][0].ipponsA).toEqual(["M", "K"]);
  });

  it('preserves court/scheduledAt via the imported mergeMatchPatch (regression — pre-fix the spread fallback would overwrite)', () => {
    const prev = makeState();
    // patch has court:"" and scheduledAt:null — the real mergeMatchPatch
    // preserves both; the old spread-fallback would have dropped them
    const next = applyPatch(prev, {
      data: { result: { id: "p1", winner: "Alice", court: "", scheduledAt: null } },
    });
    expect(next.poolMatches[0].court).toBe("A");
    expect(next.poolMatches[0].scheduledAt).toBe("09:30");
    expect(next.poolMatches[0].winner).toBe("Alice");
  });

  it('does not mutate prev when changes apply', () => {
    const prev = makeState();
    const prevPoolMatches = prev.poolMatches;
    applyPatch(prev, { data: { result: { id: "p1", winner: "Alice" } } });
    expect(prev.poolMatches).toBe(prevPoolMatches);
    expect(prev.poolMatches[0].winner).toBeUndefined();
  });

  it('only rebuilds the bracket array when at least one bracket match was patched', () => {
    const prev = makeState();
    // Patch hits only a poolMatch — bracket reference should remain identical
    const next = applyPatch(prev, { data: { result: { id: "p1", winner: "Alice" } } });
    expect(next.bracket).toBe(prev.bracket);
  });

  it('returns prev when the event has no data field', () => {
    const prev = makeState();
    const next = applyPatch(prev, {});
    expect(next).toBe(prev);
  });

  it('returns prev when prev is falsy', () => {
    expect(applyPatch(null, { data: { result: { id: "x" } } })).toBe(null);
  });

  // T049 / FR-025: when an SSE patch flips one match to completed, the
  // queuePosition of its same-court scheduled siblings should drop by one
  // so the viewer's "N before yours" caption stays in sync without an
  // extra round-trip.
  it('recomputes queuePosition on same-court scheduled siblings when a match completes', () => {
    const prev = {
      poolMatches: [
        { id: "p1", court: "A", scheduledAt: "09:00", status: "scheduled", queuePosition: 1 },
        { id: "p2", court: "A", scheduledAt: "09:10", status: "scheduled", queuePosition: 2 },
        { id: "p3", court: "A", scheduledAt: "09:20", status: "scheduled", queuePosition: 3 },
        { id: "q1", court: "B", scheduledAt: "09:00", status: "scheduled", queuePosition: 1 },
      ],
    };
    const next = applyPatch(prev, {
      data: { result: { id: "p1", winner: "Alice", status: "completed" } },
    });
    expect(next.poolMatches[0].status).toBe("completed");
    expect(next.poolMatches[0].queuePosition).toBe(0);
    expect(next.poolMatches[1].queuePosition).toBe(1);
    expect(next.poolMatches[2].queuePosition).toBe(2);
    // Court B is untouched
    expect(next.poolMatches[3].queuePosition).toBe(1);
  });

  it('recomputes queue positions when patch transitions a scheduled match to running', () => {
    const prev = {
      poolMatches: [
        { id: "p1", court: "A", scheduledAt: "09:00", status: "scheduled", queuePosition: 1 },
        { id: "p2", court: "A", scheduledAt: "09:10", status: "scheduled", queuePosition: 2 },
      ],
    };
    const next = applyPatch(prev, {
      data: { result: { id: "p1", status: "running" } },
    });
    expect(next.poolMatches[0].status).toBe("running");
    expect(next.poolMatches[0].queuePosition).toBe(0);
    expect(next.poolMatches[1].queuePosition).toBe(1);
  });

  it('does not recompute queue positions when patch does not change scheduled status', () => {
    const prev = {
      poolMatches: [
        { id: "p1", court: "A", scheduledAt: "09:00", status: "scheduled", queuePosition: 1 },
        { id: "p2", court: "A", scheduledAt: "09:10", status: "scheduled", queuePosition: 2 },
      ],
    };
    const next = applyPatch(prev, {
      data: { result: { id: "p1", winner: "Alice" } },
    });
    expect(next.poolMatches[0].winner).toBe("Alice");
    // sibling untouched — same reference
    expect(next.poolMatches[1]).toBe(prev.poolMatches[1]);
  });

  it('recomputes pool queue positions when a scheduled match moves to a different court', () => {
    // p1 starts on court A (qp=1) and moves to court B. recomputeQueuePositions
    // walks the array in order, so p1 lands at B-qp=1 and b1 shifts down to
    // B-qp=2; p2 becomes A-qp=1. The point is *that the recompute happens at
    // all* (not waiting for a refetch) — the exact numbering follows the
    // existing helper's array-order contract.
    const prev = {
      poolMatches: [
        { id: "p1", court: "A", scheduledAt: "09:00", status: "scheduled", queuePosition: 1 },
        { id: "p2", court: "A", scheduledAt: "09:10", status: "scheduled", queuePosition: 2 },
        { id: "b1", court: "B", scheduledAt: "08:30", status: "scheduled", queuePosition: 1 },
      ],
    };
    const next = applyPatch(prev, {
      data: { result: { id: "p1", court: "B", scheduledAt: "09:00", status: "scheduled" } },
    });
    const byId = Object.fromEntries(next.poolMatches.map(m => [m.id, m]));
    expect(byId.p1.court).toBe("B");
    expect(byId.p1.queuePosition).toBe(1);
    expect(byId.p2.queuePosition).toBe(1);
    expect(byId.b1.queuePosition).toBe(2);
    // Sibling identities updated (regression guard: without the court-move
    // trigger, p2 and b1 would keep their stale qp values from `prev`).
    expect(next.poolMatches[1]).not.toBe(prev.poolMatches[1]);
    expect(next.poolMatches[2]).not.toBe(prev.poolMatches[2]);
  });
});

describe('recomputeQueuePositions', () => {
  it('assigns per-court 1-indexed positions to scheduled matches', () => {
    // Seed at least one queuePosition so the helper engages (it no-ops
    // when the server payload never populated the field).
    const matches = [
      { id: "a1", court: "A", status: "scheduled", queuePosition: 99 },
      { id: "a2", court: "A", status: "scheduled" },
      { id: "b1", court: "B", status: "scheduled" },
      { id: "a3", court: "A", status: "scheduled" },
    ];
    const out = recomputeQueuePositions(matches);
    expect(out[0].queuePosition).toBe(1);
    expect(out[1].queuePosition).toBe(2);
    expect(out[2].queuePosition).toBe(1);
    expect(out[3].queuePosition).toBe(3);
  });

  it('assigns 0 to running/completed matches', () => {
    const matches = [
      { id: "a1", court: "A", status: "running", queuePosition: 0 },
      { id: "a2", court: "A", status: "scheduled", queuePosition: 1 },
      { id: "a3", court: "A", status: "completed", queuePosition: 0 },
      { id: "a4", court: "A", status: "scheduled", queuePosition: 2 },
    ];
    const out = recomputeQueuePositions(matches);
    expect(out[0].queuePosition).toBe(0);
    expect(out[1].queuePosition).toBe(1);
    expect(out[2].queuePosition).toBe(0);
    expect(out[3].queuePosition).toBe(2);
  });

  it('preserves identity when nothing needs to change', () => {
    const matches = [
      { id: "a1", court: "A", status: "scheduled", queuePosition: 1 },
      { id: "a2", court: "A", status: "scheduled", queuePosition: 2 },
    ];
    expect(recomputeQueuePositions(matches)).toBe(matches);
  });

  it('no-ops when the payload never populated queuePosition (older server)', () => {
    const matches = [
      { id: "a1", court: "A", status: "scheduled" },
      { id: "a2", court: "A", status: "scheduled" },
    ];
    expect(recomputeQueuePositions(matches)).toBe(matches);
  });
});

// FR-025: bracket-side queue position recompute. Mirrors the pool helper
// tests above. Without this, a knockout-only competition would show
// stale "N before yours" labels for ~500-1000ms after a bracket match
// completes (until the jittered GET refresh lands).
describe('recomputeBracketQueuePositions', () => {
  it('assigns per-court 1-indexed positions to scheduled bracket matches across rounds', () => {
    const bracket = {
      rounds: [
        [
          { id: "r1m1", court: "A", status: "scheduled", queuePosition: 99 },
          { id: "r1m2", court: "B", status: "scheduled" },
        ],
        [
          { id: "r2m1", court: "A", status: "scheduled" },
        ],
      ],
    };
    const out = recomputeBracketQueuePositions(bracket);
    expect(out.rounds[0][0].queuePosition).toBe(1);
    expect(out.rounds[0][1].queuePosition).toBe(1);
    expect(out.rounds[1][0].queuePosition).toBe(2);
  });

  it('assigns 0 to running/completed bracket matches', () => {
    const bracket = {
      rounds: [
        [
          { id: "r1m1", court: "A", status: "running", queuePosition: 99 },
          { id: "r1m2", court: "A", status: "scheduled" },
          { id: "r1m3", court: "A", status: "completed", queuePosition: 77 },
          { id: "r1m4", court: "A", status: "scheduled" },
        ],
      ],
    };
    const out = recomputeBracketQueuePositions(bracket);
    expect(out.rounds[0][0].queuePosition).toBe(0);
    expect(out.rounds[0][1].queuePosition).toBe(1);
    expect(out.rounds[0][2].queuePosition).toBe(0);
    expect(out.rounds[0][3].queuePosition).toBe(2);
  });

  it('preserves identity when nothing needs to change', () => {
    const bracket = {
      rounds: [
        [
          { id: "r1m1", court: "A", status: "scheduled", queuePosition: 1 },
          { id: "r1m2", court: "A", status: "scheduled", queuePosition: 2 },
        ],
      ],
    };
    expect(recomputeBracketQueuePositions(bracket)).toBe(bracket);
  });

  it('no-ops when no round has a populated queuePosition (older server)', () => {
    const bracket = {
      rounds: [
        [
          { id: "r1m1", court: "A", status: "scheduled" },
          { id: "r1m2", court: "A", status: "scheduled" },
        ],
      ],
    };
    expect(recomputeBracketQueuePositions(bracket)).toBe(bracket);
  });

  it('handles nil / empty / malformed bracket gracefully', () => {
    expect(recomputeBracketQueuePositions(null)).toBeNull();
    expect(recomputeBracketQueuePositions(undefined)).toBeUndefined();
    expect(recomputeBracketQueuePositions({})).toEqual({});
    const emptyRounds = { rounds: [] };
    expect(recomputeBracketQueuePositions(emptyRounds)).toBe(emptyRounds);
  });

  it('applyPatch recomputes bracket queuePositions on completion', () => {
    // The applyPatch SSE integration: a single bracket match transitions
    // to completed; its scheduled siblings on the same court should drop
    // by one slot. Mirrors the existing pool integration test.
    const prev = {
      bracket: {
        rounds: [
          [
            { id: "b1", court: "A", scheduledAt: "11:00", status: "scheduled", queuePosition: 1 },
            { id: "b2", court: "A", scheduledAt: "11:10", status: "scheduled", queuePosition: 2 },
            { id: "b3", court: "B", scheduledAt: "11:00", status: "scheduled", queuePosition: 1 },
          ],
        ],
      },
    };
    const next = applyPatch(prev, {
      data: { result: { id: "b1", winner: "Alice", status: "completed" } },
    });
    expect(next.bracket.rounds[0][0].status).toBe("completed");
    expect(next.bracket.rounds[0][0].queuePosition).toBe(0);
    expect(next.bracket.rounds[0][1].queuePosition).toBe(1);
    // Court B is untouched
    expect(next.bracket.rounds[0][2].queuePosition).toBe(1);
  });

  it('applyPatch recomputes bracket queue positions when patch transitions a scheduled match to running', () => {
    const prev = {
      bracket: {
        rounds: [
          [
            { id: "b1", court: "A", status: "scheduled", queuePosition: 1 },
            { id: "b2", court: "A", status: "scheduled", queuePosition: 2 },
          ],
        ],
      },
    };
    const next = applyPatch(prev, {
      data: { result: { id: "b1", status: "running" } },
    });
    expect(next.bracket.rounds[0][0].status).toBe("running");
    expect(next.bracket.rounds[0][0].queuePosition).toBe(0);
    expect(next.bracket.rounds[0][1].queuePosition).toBe(1);
  });

  it('applyPatch does not recompute bracket queue positions when patch does not change scheduled status', () => {
    const prev = {
      bracket: {
        rounds: [
          [
            { id: "b1", court: "A", status: "scheduled", queuePosition: 1 },
            { id: "b2", court: "A", status: "scheduled", queuePosition: 2 },
          ],
        ],
      },
    };
    const next = applyPatch(prev, {
      data: { result: { id: "b1", scoreA: "M" } },
    });
    expect(next.bracket.rounds[0][0].scoreA).toBe("M");
    // sibling untouched — same reference
    expect(next.bracket.rounds[0][1]).toBe(prev.bracket.rounds[0][1]);
  });

  it('applyPatch recomputes bracket queue positions when a scheduled match moves to a different court', () => {
    // b1 (court A, qp=1) moves to court B. recomputeBracketQueuePositions
    // walks rounds in order, so b1 lands at B-qp=1, b2 becomes A-qp=1, and
    // c1 shifts down to B-qp=2. Same array-order contract as the pool helper —
    // the test pins that the recompute fires *immediately on a court move*
    // (not just on a status flip), which is the bug Copilot flagged.
    const prev = {
      bracket: {
        rounds: [
          [
            { id: "b1", court: "A", scheduledAt: "11:00", status: "scheduled", queuePosition: 1 },
            { id: "b2", court: "A", scheduledAt: "11:30", status: "scheduled", queuePosition: 2 },
            { id: "c1", court: "B", scheduledAt: "10:30", status: "scheduled", queuePosition: 1 },
          ],
        ],
      },
    };
    const next = applyPatch(prev, {
      data: { result: { id: "b1", court: "B", scheduledAt: "11:00", status: "scheduled" } },
    });
    const flat = next.bracket.rounds[0];
    const byId = Object.fromEntries(flat.map(m => [m.id, m]));
    expect(byId.b1.court).toBe("B");
    expect(byId.b1.queuePosition).toBe(1);
    expect(byId.b2.queuePosition).toBe(1);
    expect(byId.c1.queuePosition).toBe(2);
  });
});
