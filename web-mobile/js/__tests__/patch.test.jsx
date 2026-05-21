import { describe, it, expect } from 'vitest';
import { applyPatch, recomputeQueuePositions } from '../patch.jsx';

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
    expect(next.poolMatches[0].winner).toEqual({ id: "Alice", name: "Alice" });
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
    expect(next.poolMatches[0].winner).toEqual({ id: "Alice", name: "Alice" });
    expect(next.poolMatches[1].winner).toEqual({ id: "Bob", name: "Bob" });
  });

  it('lets results take precedence over result when both present', () => {
    const prev = makeState();
    const next = applyPatch(prev, {
      data: {
        result: { id: "p1", winner: "FromSingleResult" },
        results: [{ id: "p1", winner: "FromArray" }],
      },
    });
    expect(next.poolMatches[0].winner).toEqual({ id: "FromArray", name: "FromArray" });
  });

  it('maps ipponsA/B to scoreA/B on bracket-round matches', () => {
    const prev = makeState();
    const next = applyPatch(prev, {
      data: {
        result: { id: "b1", winner: "Alice", ipponsA: ["M", "K"], ipponsB: ["D"], status: "completed" },
      },
    });
    expect(next.bracket.rounds[0][0].winner).toEqual({ id: "Alice", name: "Alice" });
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
    expect(next.poolMatches[0].winner).toEqual({ id: "Alice", name: "Alice" });
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

  it('does not recompute queue positions when patch is not a completion', () => {
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
    // sibling untouched — same reference
    expect(next.poolMatches[1]).toBe(prev.poolMatches[1]);
  });

  it('normalizes sideA and sideB strings into player objects when applying a patch', () => {
    const prev = {
      poolMatches: [
        { id: "p1", sideA: { id: "Alice", name: "Alice" }, sideB: { id: "Bob", name: "Bob" }, status: "scheduled" }
      ]
    };
    const next = applyPatch(prev, {
      data: {
        result: {
          id: "p1",
          sideA: "Alice",
          sideB: "Bob",
          winner: "Alice",
          status: "completed"
        }
      }
    });
    expect(next.poolMatches[0].sideA).toEqual({ id: "Alice", name: "Alice" });
    expect(next.poolMatches[0].sideB).toEqual({ id: "Bob", name: "Bob" });
    expect(next.poolMatches[0].winner).toEqual({ id: "Alice", name: "Alice" });
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
