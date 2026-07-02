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

  it('does not throw on null / id-less entries in results and ignores them', () => {
    const prev = makeState();
    // A malformed event (e.g. results:[null]) must not crash the Map build;
    // the bad entries are skipped and a valid sibling result still applies.
    let next;
    expect(() => {
      next = applyPatch(prev, { data: { results: [null, "x", {}, { winner: "X" }, { id: "p1", status: "completed", winner: "X" }] } });
    }).not.toThrow();
    expect(next.poolMatches.find(m => m.id === "p1").status).toBe("completed");
    expect(next.poolMatches.find(m => m.id === "p2").status).toBe("scheduled");
  });

  it('applies a single result to the matching poolMatch', () => {
    const prev = makeState();
    const next = applyPatch(prev, { data: { result: { id: "p1", winner: "Alice", status: "completed" } } });
    expect(next).not.toBe(prev);
    expect(next.poolMatches[0].winner).toEqual({ id: "Alice", name: "Alice" });
    expect(next.poolMatches[0].status).toBe("completed");
    // p2 is the only remaining scheduled match on court B, so it gets
    // queuePosition: 1 via the post-patch recompute (FR-025).
    expect(next.poolMatches[1].queuePosition).toBe(1);
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

  it('preserves court/scheduledAt via the imported mergeMatchPatch (regression: pre-fix the spread fallback would overwrite)', () => {
    const prev = makeState();
    // patch has court:"" and scheduledAt:null. The real mergeMatchPatch
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
    // Patch hits only a poolMatch; bracket reference should remain identical
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

  // Any transition off "scheduled" releases a slot in the per-court
  // queue, including running. Previously this test asserted "no
  // recompute on scheduled → running"; the Copilot review on PR #124
  // flagged that as wrong because the remaining scheduled siblings
  // need to shift up immediately rather than wait for a refresh.
  it('recomputes queue positions when a scheduled match transitions to running', () => {
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
    // p1 dropped out of the queue (running ⇒ 0); p2 shifts up to 1.
    expect(next.poolMatches[0].queuePosition).toBe(0);
    expect(next.poolMatches[1].queuePosition).toBe(1);
  });

  // Admin correction case: an SSE patch can revert a completed match back
  // to scheduled (status set via the score endpoint). The match re-enters
  // the per-court queue and should claim a non-zero queuePosition while
  // shifting siblings down. Copilot flagged this on PR #124.
  it('recomputes queue positions when a completed match transitions back to scheduled', () => {
    const prev = {
      poolMatches: [
        { id: "p1", court: "A", scheduledAt: "09:00", status: "completed", queuePosition: 0 },
        { id: "p2", court: "A", scheduledAt: "09:10", status: "scheduled", queuePosition: 1 },
        { id: "p3", court: "A", scheduledAt: "09:20", status: "scheduled", queuePosition: 2 },
      ],
    };
    const next = applyPatch(prev, {
      data: { result: { id: "p1", status: "scheduled", winner: null } },
    });
    expect(next.poolMatches[0].status).toBe("scheduled");
    // p1 re-enters the queue at qp=1 (array order); p2 and p3 shift down.
    expect(next.poolMatches[0].queuePosition).toBe(1);
    expect(next.poolMatches[1].queuePosition).toBe(2);
    expect(next.poolMatches[2].queuePosition).toBe(3);
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
    // winner string is normalised into {id,name} by normalizeMatch (Swiss SSE
    // path: applies to all pool patches now, not just Swiss).
    expect(next.poolMatches[0].winner).toEqual({ id: "Alice", name: "Alice" });
    // sibling untouched: same reference
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

  it('does not recompute queue positions for a no-op patch on a non-scheduled match', () => {
    const prev = {
      poolMatches: [
        { id: "p1", court: "A", scheduledAt: "09:00", status: "completed", queuePosition: 0 },
        { id: "p2", court: "A", scheduledAt: "09:10", status: "scheduled", queuePosition: 1 },
      ],
    };
    const next = applyPatch(prev, {
      // Patch a completed match's metadata: no queue impact.
      data: { result: { id: "p1", status: "completed", winner: "Alice" } },
    });
    // sibling untouched: same reference
    expect(next.poolMatches[1]).toBe(prev.poolMatches[1]);
  });

  it('recomputes pool queue positions when a scheduled match moves to a different court', () => {
    // p1 (09:00) moves to court B where b1 (08:30) already is. recomputeQueuePositions
    // sorts per-court by scheduledAt, so b1 (08:30) is B-qp=1 and p1 (09:00) is
    // B-qp=2; p2 becomes A-qp=1. The point is *that the recompute happens at
    // all* (not waiting for a refetch): exact ordering follows scheduledAt.
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
    expect(byId.p1.queuePosition).toBe(2); // 09:00 is after b1's 08:30 on court B
    expect(byId.p2.queuePosition).toBe(1); // sole match on A
    expect(byId.b1.queuePosition).toBe(1); // 08:30 is first on B
    // p2's qp changed (2→1), so it's a new object; b1's qp stayed 1 (identity preserved).
    expect(next.poolMatches[1]).not.toBe(prev.poolMatches[1]);
    expect(next.poolMatches[2]).toBe(prev.poolMatches[2]);
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

  it('derives positions even when no prior queuePosition fields exist (omitempty payload)', () => {
    // Backend omits queuePosition=0 via omitempty. A viewer opened while
    // all matches were running/completed gets a payload with no
    // queuePosition fields. An SSE patch that transitions one match back
    // to scheduled must still derive positions from scratch.
    const matches = [
      { id: "a1", court: "A", status: "scheduled" },
      { id: "a2", court: "A", status: "scheduled" },
    ];
    const out = recomputeQueuePositions(matches);
    expect(out).not.toBe(matches);
    expect(out[0].queuePosition).toBe(1);
    expect(out[1].queuePosition).toBe(2);
  });

  it('no-ops when no matches are scheduled and no stale qps to clear', () => {
    const matches = [
      { id: "a1", court: "A", status: "running" },
      { id: "a2", court: "A", status: "completed" },
    ];
    expect(recomputeQueuePositions(matches)).toBe(matches);
  });

  it('clears stale non-zero queuePosition when the last scheduled match transitions off', () => {
    // The contract is that non-scheduled matches must have queuePosition === 0.
    // When the last scheduled match in a court flips to running/completed,
    // _mergeMatchPatch preserves the old queuePosition field, so the recompute
    // must still run to zero it out: even though no match is `scheduled`.
    const matches = [
      { id: "a1", court: "A", status: "running", queuePosition: 1 },
      { id: "a2", court: "A", status: "completed", queuePosition: 0 },
    ];
    const out = recomputeQueuePositions(matches);
    expect(out).not.toBe(matches);
    expect(out[0].queuePosition).toBe(0);
    expect(out[1].queuePosition).toBe(0);
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

  it('derives positions even when no prior queuePosition fields exist (omitempty payload)', () => {
    // Same omitempty scenario as the pool helper: bracket matches can
    // arrive without queuePosition when all were running/completed.
    const bracket = {
      rounds: [
        [
          { id: "r1m1", court: "A", status: "scheduled" },
          { id: "r1m2", court: "A", status: "scheduled" },
        ],
      ],
    };
    const out = recomputeBracketQueuePositions(bracket);
    expect(out).not.toBe(bracket);
    expect(out.rounds[0][0].queuePosition).toBe(1);
    expect(out.rounds[0][1].queuePosition).toBe(2);
  });

  it('no-ops when no bracket match is scheduled and no stale qps to clear', () => {
    const bracket = {
      rounds: [
        [
          { id: "r1m1", court: "A", status: "running" },
          { id: "r1m2", court: "A", status: "completed" },
        ],
      ],
    };
    expect(recomputeBracketQueuePositions(bracket)).toBe(bracket);
  });

  it('clears stale non-zero queuePosition on bracket matches that transitioned off scheduled', () => {
    // Mirror of the pool case: when the last scheduled bracket match
    // completes, the recompute must still zero out the stale qp left
    // behind by _mergeMatchPatch.
    const bracket = {
      rounds: [
        [
          { id: "r1m1", court: "A", status: "running", queuePosition: 1 },
          { id: "r1m2", court: "A", status: "completed", queuePosition: 0 },
        ],
      ],
    };
    const out = recomputeBracketQueuePositions(bracket);
    expect(out).not.toBe(bracket);
    expect(out.rounds[0][0].queuePosition).toBe(0);
    expect(out.rounds[0][1].queuePosition).toBe(0);
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

  it('applyPatch recomputes bracket queue positions when a scheduled match transitions to running', () => {
    // Bracket-side counterpart of the pool test above. Any off-scheduled
    // transition (running/forfeit/cancelled/kiken) releases a slot in
    // the per-court queue and triggers a recompute.
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
    // b1 dropped to 0 (running); b2 shifted up to 1.
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
    // sibling untouched: same reference
    expect(next.bracket.rounds[0][1]).toBe(prev.bracket.rounds[0][1]);
  });

  it('applyPatch recomputes bracket queue positions when a scheduled match moves to a different court', () => {
    // b1 (court A, qp=1) moves to court B. recomputeBracketQueuePositions
    // walks rounds in order, so b1 lands at B-qp=1, b2 becomes A-qp=1, and
    // c1 (10:30) lands ahead of b1 (11:00) on court B in scheduledAt order,
    // so c1=1 and b1=2. b2 is the only match left on court A → b2=1. The
    // test pins that the recompute fires *immediately on a court move*
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
    expect(byId.b1.queuePosition).toBe(2); // 11:00 > c1's 10:30 → b1 is second on B
    expect(byId.b2.queuePosition).toBe(1); // sole match on A
    expect(byId.c1.queuePosition).toBe(1); // 10:30 is first on B
  });
});

// Tri-review: the naginata bronze match (thirdPlaceMatch) is a SIBLING of
// bracket.rounds, so applyPatch's rounds loop and recomputeBracketQueuePositions
// both used to skip it — an SSE bronze score stayed stale until the background
// refetch, and the bronze never got a queue position on its court.
describe('applyPatch: naginata bronze (thirdPlaceMatch)', () => {
  const makeBronzeState = () => ({
    poolMatches: [],
    bracket: {
      rounds: [
        [{ id: "b1", court: "A", status: "completed" }, { id: "b2", court: "A", status: "completed" }],
        [{ id: "final", court: "A", status: "running" }],
      ],
      thirdPlaceMatch: { id: "m-bronze", court: "A", status: "scheduled", queuePosition: 0 },
    },
  });

  it('applies an SSE match_updated to the bronze match (was silently skipped)', () => {
    const prev = makeBronzeState();
    const next = applyPatch(prev, { data: { result: { id: "m-bronze", status: "completed", winner: "Alice", ipponsA: ["M"] } } });
    expect(next).not.toBe(prev);
    expect(next.bracket.thirdPlaceMatch.status).toBe("completed");
    expect(next.bracket.thirdPlaceMatch.scoreA).toBe("M"); // ippons→score mapping
  });

  it('preserves bronze identity when the event targets a different match', () => {
    const prev = makeBronzeState();
    const next = applyPatch(prev, { data: { result: { id: "final", status: "completed", winner: "Bob" } } });
    expect(next.bracket.thirdPlaceMatch).toBe(prev.bracket.thirdPlaceMatch);
  });
});

describe('recomputeBracketQueuePositions: bronze participates in its court queue', () => {
  it('assigns the bronze a queue position alongside the rounds on the same court', () => {
    const bracket = {
      rounds: [[{ id: "final", court: "A", status: "scheduled", scheduledAt: "12:00" }]],
      thirdPlaceMatch: { id: "m-bronze", court: "A", status: "scheduled", scheduledAt: "12:05" },
    };
    const out = recomputeBracketQueuePositions(bracket);
    expect(out.rounds[0][0].queuePosition).toBe(1); // earlier scheduledAt → 1st
    expect(out.thirdPlaceMatch.queuePosition).toBe(2); // later → 2nd
  });

  it('drops the bronze queue position to 0 once it is no longer scheduled', () => {
    const bracket = {
      rounds: [[{ id: "final", court: "A", status: "scheduled", scheduledAt: "12:00" }]],
      thirdPlaceMatch: { id: "m-bronze", court: "A", status: "completed", scheduledAt: "12:05", queuePosition: 2 },
    };
    const out = recomputeBracketQueuePositions(bracket);
    expect(out.thirdPlaceMatch.queuePosition).toBe(0);
    expect(out.rounds[0][0].queuePosition).toBe(1);
  });
});
