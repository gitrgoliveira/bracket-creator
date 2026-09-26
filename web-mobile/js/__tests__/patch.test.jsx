import { describe, it, expect } from 'vitest';
import { applyPatch, recomputeQueuePositions, recomputeBracketQueuePositions, keepNewerMatches, keepNewerDetail } from '../patch.jsx';

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
    // bc-pnum: resolveSide no longer invents an id from
    // the name for a side absent from the (empty, here) player map -- id
    // stays "" rather than "Alice".
    expect(next.poolMatches[0].winner).toEqual({ id: "", name: "Alice" });
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
    expect(next.poolMatches[0].winner).toEqual({ id: "", name: "Alice" });
    expect(next.poolMatches[1].winner).toEqual({ id: "", name: "Bob" });
  });

  it('lets results take precedence over result when both present', () => {
    const prev = makeState();
    const next = applyPatch(prev, {
      data: {
        result: { id: "p1", winner: "FromSingleResult" },
        results: [{ id: "p1", winner: "FromArray" }],
      },
    });
    expect(next.poolMatches[0].winner).toEqual({ id: "", name: "FromArray" });
  });

  it('merges ipponsA/B straight through on bracket-round matches, no scoreA/B synthesis', () => {
    // Pool and bracket matches share one wire shape (ipponsA/ipponsB arrays;
    // scoreA/scoreB strings never appear), so applyPatch hands the patch to
    // mergeMatchPatch with no per-kind translation for either.
    const prev = makeState();
    const next = applyPatch(prev, {
      data: {
        result: { id: "b1", winner: "Alice", ipponsA: ["M", "K"], ipponsB: ["D"], status: "completed" },
      },
    });
    expect(next.bracket.rounds[0][0].winner).toEqual({ id: "", name: "Alice" });
    expect(next.bracket.rounds[0][0].ipponsA).toEqual(["M", "K"]);
    expect(next.bracket.rounds[0][0].ipponsB).toEqual(["D"]);
    expect(next.bracket.rounds[0][0].scoreA).toBeUndefined();
    expect(next.bracket.rounds[0][0].scoreB).toBeUndefined();
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
    expect(next.poolMatches[0].winner).toEqual({ id: "", name: "Alice" });
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
    // path: applies to all pool patches now, not just Swiss). id stays ""
    // (no player map entry for "Alice" here): resolveSide no longer invents
    // one from the name.
    expect(next.poolMatches[0].winner).toEqual({ id: "", name: "Alice" });
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
    // sideA/sideB/winner are freshly re-resolved from the patch's raw name
    // strings against an empty player map: id "" for all three (the prior
    // fixture's invented {id:"Alice",...} on prev is overwritten, not read).
    expect(next.poolMatches[0].sideA).toEqual({ id: "", name: "Alice" });
    expect(next.poolMatches[0].sideB).toEqual({ id: "", name: "Bob" });
    expect(next.poolMatches[0].winner).toEqual({ id: "", name: "Alice" });
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

  // bc-tmfn: the server derives `ineligibleSides` from eligibility, which
  // only a competitor_status_updated refetch changes (see
  // ineligible_match.jsx). An SSE match_updated patch for that match is not
  // that refetch, so it may arrive without the field -- and when it does,
  // the stored stamp must survive the merge.
  describe('ineligibleSides survives a patch', () => {
    const prevWithStamp = () => ({
      poolMatches: [
        { id: "p1", court: "A", scheduledAt: "09:30", status: "scheduled", ineligibleSides: { b: "fusenpai" } },
      ],
    });

    it('keeps the stamp when the patch omits the field entirely', () => {
      const next = applyPatch(prevWithStamp(), { data: { result: { id: "p1", court: "A" } } });
      expect(next.poolMatches[0].ineligibleSides).toEqual({ b: "fusenpai" });
    });

    // bc-cse: the sibling test that sent `ineligibleSides: null` explicitly
    // is gone -- the field is a Go pointer with `omitempty`
    // (internal/state/models.go), so an absent stamp is OMITTED from the
    // wire, never sent as a literal `null`; that payload shape cannot occur
    // and the wrapper that used to defend against it was removed with it.

    it('a patch carrying a real ineligibleSides value wins (server says nobody is barred any more)', () => {
      const next = applyPatch(prevWithStamp(), { data: { result: { id: "p1", ineligibleSides: {} } } });
      expect(next.poolMatches[0].ineligibleSides).toEqual({});
    });

    it('a patch carrying a different ineligibleSides value wins', () => {
      const next = applyPatch(prevWithStamp(), { data: { result: { id: "p1", ineligibleSides: { a: "kiken-injury" } } } });
      expect(next.poolMatches[0].ineligibleSides).toEqual({ a: "kiken-injury" });
    });
  });

  // bc-tmfn: a patch that flips whether a still-scheduled match is barred
  // must re-rank the per-court queue, same as a status/court/scheduledAt
  // change does -- recomputeQueuePositions gives a barred match position 0
  // and skips it, so its sibling must move up to take that slot.
  it('re-ranks per-court siblings when a patch flips a still-scheduled match to barred', () => {
    const prev = {
      poolMatches: [
        // p1 was next-up (qp 1) before the withdrawal that bars it.
        { id: "p1", court: "A", scheduledAt: "09:00", status: "scheduled", queuePosition: 1 },
        { id: "p1b", court: "A", scheduledAt: "09:45", status: "scheduled" },
      ],
    };
    const next = applyPatch(prev, {
      data: { result: { id: "p1", status: "scheduled", ineligibleSides: { a: "fusenpai" } } },
    });
    const byId = Object.fromEntries(next.poolMatches.map(m => [m.id, m]));
    expect(byId.p1.queuePosition).toBe(0);
    expect(byId.p1b.queuePosition).toBe(1);
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

  // bc-tmfn: a barred match (ineligible_match.jsx -- a scheduled match whose
  // competitor withdrew earlier) cannot be fought, so it holds position 0
  // and is skipped by the counter: later matches on the same court move up
  // to take the slot it would otherwise have consumed.
  it('gives a barred scheduled match position 0 and does not count it', () => {
    const matches = [
      // Stale qp 5 (as if the withdrawal barred it after it already held a
      // real position): the recompute must zero it, not just leave it be.
      { id: "a1", court: "A", status: "scheduled", queuePosition: 5, ineligibleSides: { a: "kiken-voluntary" } },
      { id: "a2", court: "A", status: "scheduled" },
      { id: "a3", court: "A", status: "scheduled" },
    ];
    const out = recomputeQueuePositions(matches);
    expect(out[0].queuePosition).toBe(0);
    expect(out[1].queuePosition).toBe(1);
    expect(out[2].queuePosition).toBe(2);
  });

  it('a match with an ineligibleSides stamp that is NOT scheduled counts normally (barredSides gates on status)', () => {
    const matches = [
      { id: "a1", court: "A", status: "running", queuePosition: 3, ineligibleSides: { a: "kiken-voluntary" } },
      { id: "a2", court: "A", status: "scheduled" },
    ];
    const out = recomputeQueuePositions(matches);
    expect(out[0].queuePosition).toBe(0); // running is never counted anyway
    expect(out[1].queuePosition).toBe(1);
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

  // bc-tmfn: mirrors the pool helper's barred-skip test above.
  it('gives a barred scheduled bracket match position 0 and does not count it', () => {
    const bracket = {
      rounds: [
        [
          { id: "r1m1", court: "A", status: "scheduled", queuePosition: 5, ineligibleSides: { b: "fusenpai" } },
          { id: "r1m2", court: "A", status: "scheduled" },
        ],
        [
          { id: "r2m1", court: "A", status: "scheduled" },
        ],
      ],
    };
    const out = recomputeBracketQueuePositions(bracket);
    expect(out.rounds[0][0].queuePosition).toBe(0);
    expect(out.rounds[0][1].queuePosition).toBe(1);
    expect(out.rounds[1][0].queuePosition).toBe(2);
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
      data: { result: { id: "b1", ipponsA: ["M"] } },
    });
    expect(next.bracket.rounds[0][0].ipponsA).toEqual(["M"]);
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
    expect(next.bracket.thirdPlaceMatch.ipponsA).toEqual(["M"]); // no scoreA/B synthesis
    expect(next.bracket.thirdPlaceMatch.scoreA).toBeUndefined();
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

  it('orders the bronze BEFORE the final when scheduledAt is blank (bronze plays first)', () => {
    // No scheduledAt anywhere → the idx tie-break decides. The bronze must slot
    // in just before the final on their shared court (viewer_awards: "the bronze
    // is normally played first"), not after it.
    const bracket = {
      rounds: [
        [{ id: "sf1", court: "A", status: "completed" }, { id: "sf2", court: "B", status: "completed" }],
        [{ id: "final", court: "A", status: "scheduled" }],
      ],
      thirdPlaceMatch: { id: "m-bronze", court: "A", status: "scheduled" },
    };
    const out = recomputeBracketQueuePositions(bracket);
    expect(out.thirdPlaceMatch.queuePosition).toBe(1); // bronze first
    expect(out.rounds[1][0].queuePosition).toBe(2); // final second
  });
});

// A refetch can read the data just before a write commits and answer after
// that write's push was applied: the match it carries is older than the one
// already held, and must not replace it.
describe('keepNewerMatches', () => {
  const row = (id, modifiedAt, ipponsA = []) => ({ id, status: 'running', modifiedAt, ipponsA });
  const comp = (pool, rounds = [], third = null) => ({ id: 'c1', poolMatches: pool, bracket: { rounds, thirdPlaceMatch: third } });

  it('keeps a held match newer than the fetched one, everywhere a match lives', () => {
    const held = comp([row('P1', 300, ['M', 'K'])], [[row('K1', 300, ['D'])]], row('B3', 300, ['T']));
    const fetched = comp([row('P1', 200, ['M'])], [[row('K1', 200)]], row('B3', 200));
    const out = keepNewerMatches(held, fetched);
    expect(out.poolMatches[0].ipponsA).toEqual(['M', 'K']);
    expect(out.bracket.rounds[0][0].ipponsA).toEqual(['D']);
    expect(out.bracket.thirdPlaceMatch.ipponsA).toEqual(['T']);
  });

  it('takes the fetched match when it is as new as the held one or newer', () => {
    const held = comp([row('P1', 300, ['M']), row('P2', 300, ['M'])]);
    const fetched = comp([row('P1', 300, ['M', 'K']), row('P2', 400, [])]);
    const out = keepNewerMatches(held, fetched);
    expect(out.poolMatches[0].ipponsA).toEqual(['M', 'K']);
    expect(out.poolMatches[1].ipponsA).toEqual([]);
  });

  it('takes the fetch whole when nothing is held yet', () => {
    const fetched = comp([row('P1', 100)]);
    expect(keepNewerMatches(undefined, fetched)).toBe(fetched);
  });
});

describe('keepNewerDetail', () => {
  const detail = (id, modifiedAt, ipponsA) => ({ config: { id }, poolMatches: [{ id: 'Pool A-0', status: 'running', modifiedAt, ipponsA }] });

  it('keeps the newer held match of the same competition', () => {
    expect(keepNewerDetail(detail('c1', 300, ['M', 'K']), detail('c1', 200, ['M'])).poolMatches[0].ipponsA).toEqual(['M', 'K']);
  });

  it('takes the fetch whole for another competition, whose match ids may repeat', () => {
    const fetched = detail('c2', 100, []);
    expect(keepNewerDetail(detail('c1', 300, ['M']), fetched)).toBe(fetched);
    expect(keepNewerDetail(null, fetched)).toBe(fetched);
  });
});
