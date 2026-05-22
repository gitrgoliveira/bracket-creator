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

  it('applyPatch does not recompute bracket queue positions on non-completion', () => {
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
    // sibling untouched — same reference
    expect(next.bracket.rounds[0][1]).toBe(prev.bracket.rounds[0][1]);
  });
});
