import { describe, it, expect, vi } from 'vitest';
import { applyPatchOrdered, checkSeqGap } from '../patch.jsx';

// T218 / Phase 12.D — gap detection on the SSE consumer side.
//
// The backend now stamps every envelope with a monotonic `seq`
// (mobileapp.SSEEvent.Seq, T215). The frontend wraps applyPatch with
// applyPatchOrdered, which:
//   - drops duplicate / replayed events (seq <= lastSeq),
//   - fires onGap({from, to}) on jumps (seq > lastSeq + 1),
//   - applies normal events transparently (seq === lastSeq + 1).
//
// These tests pin all three branches plus the first-event behaviour
// (no false-positive gap on initial connect).

const makeState = () => ({
  poolMatches: [
    { id: "p1", court: "A", status: "scheduled" },
    { id: "p2", court: "A", status: "scheduled" },
  ],
});

describe('applyPatchOrdered', () => {
  it('accepts the first event with any seq and updates lastSeq', () => {
    const prev = makeState();
    const state = {};
    const onGap = vi.fn();
    const next = applyPatchOrdered(prev, {
      type: "match_updated",
      seq: 5,
      data: { result: { id: "p1", winner: "Alice" } },
    }, state, onGap);
    expect(state.lastSeq).toBe(5);
    expect(next.poolMatches[0].winner).toEqual({ id: "Alice", name: "Alice" });
    expect(onGap).not.toHaveBeenCalled();
  });

  it('applies the next-sequential event without firing gap callback', () => {
    const prev = makeState();
    const state = { lastSeq: 5 };
    const onGap = vi.fn();
    const next = applyPatchOrdered(prev, {
      type: "match_updated",
      seq: 6,
      data: { result: { id: "p1", winner: "Alice" } },
    }, state, onGap);
    expect(state.lastSeq).toBe(6);
    expect(next.poolMatches[0].winner).toEqual({ id: "Alice", name: "Alice" });
    expect(onGap).not.toHaveBeenCalled();
  });

  it('fires onGap with the missing range when seq jumps forward', () => {
    const prev = makeState();
    const state = { lastSeq: 5 };
    const onGap = vi.fn();
    const next = applyPatchOrdered(prev, {
      type: "match_updated",
      seq: 10,
      data: { result: { id: "p1", winner: "Alice" } },
    }, state, onGap);
    // Still applies the current patch — the latest state is
    // authoritative even when intermediate events were lost.
    expect(next.poolMatches[0].winner).toEqual({ id: "Alice", name: "Alice" });
    expect(state.lastSeq).toBe(10);
    expect(onGap).toHaveBeenCalledTimes(1);
    expect(onGap).toHaveBeenCalledWith({ from: 6, to: 9 });
  });

  it('drops duplicate replayed events without firing gap callback or applying patch', () => {
    const prev = makeState();
    const state = { lastSeq: 10 };
    const onGap = vi.fn();
    // A replayed event (seq <= lastSeq) is silently dropped. Return
    // value must be `prev` identity-equal — the caller's render
    // memoisation depends on this.
    const next = applyPatchOrdered(prev, {
      type: "match_updated",
      seq: 7,
      data: { result: { id: "p1", winner: "Alice" } },
    }, state, onGap);
    expect(next).toBe(prev);
    expect(state.lastSeq).toBe(10);
    expect(onGap).not.toHaveBeenCalled();
  });

  it('drops equal-seq events (duplicate via reconnect race)', () => {
    const prev = makeState();
    const state = { lastSeq: 10 };
    const onGap = vi.fn();
    const next = applyPatchOrdered(prev, {
      type: "match_updated",
      seq: 10,
      data: { result: { id: "p1", winner: "Alice" } },
    }, state, onGap);
    expect(next).toBe(prev);
    expect(state.lastSeq).toBe(10);
    expect(onGap).not.toHaveBeenCalled();
  });

  it('passes through events without a seq field (additive — old envelopes)', () => {
    // Backward-compatibility: an old server without T215 emits events
    // without a `seq` field. applyPatchOrdered falls back to plain
    // applyPatch and does not update lastSeq.
    const prev = makeState();
    const state = { lastSeq: 10 };
    const onGap = vi.fn();
    const next = applyPatchOrdered(prev, {
      type: "match_updated",
      data: { result: { id: "p1", winner: "Alice" } },
    }, state, onGap);
    expect(next.poolMatches[0].winner).toEqual({ id: "Alice", name: "Alice" });
    expect(state.lastSeq).toBe(10);
    expect(onGap).not.toHaveBeenCalled();
  });

  it('handles a series of events with the same state object', () => {
    let cur = makeState();
    const state = {};
    const onGap = vi.fn();
    const events = [
      { type: "match_updated", seq: 1, data: { result: { id: "p1", winner: "A" } } },
      { type: "match_updated", seq: 2, data: { result: { id: "p2", winner: "B" } } },
      // Gap (3, 4 missing) — onGap fires once with {3, 4}.
      { type: "match_updated", seq: 5, data: { result: { id: "p1", winner: "C" } } },
      // Replay of seq 2 (duplicate) — dropped.
      { type: "match_updated", seq: 2, data: { result: { id: "p2", winner: "OLD" } } },
      // Normal continuation.
      { type: "match_updated", seq: 6, data: { result: { id: "p2", winner: "D" } } },
    ];
    for (const e of events) {
      cur = applyPatchOrdered(cur, e, state, onGap);
    }
    expect(state.lastSeq).toBe(6);
    expect(onGap).toHaveBeenCalledTimes(1);
    expect(onGap).toHaveBeenCalledWith({ from: 3, to: 4 });
    expect(cur.poolMatches[0].winner).toEqual({ id: "C", name: "C" });
    expect(cur.poolMatches[1].winner).toEqual({ id: "D", name: "D" });
  });

  it('survives a throwing onGap callback (does not break SSE processing)', () => {
    const prev = makeState();
    const state = { lastSeq: 5 };
    const onGap = vi.fn(() => { throw new Error("boom"); });
    // Silence the expected console.error from the catch branch so
    // CI doesn't flag it.
    const errSpy = vi.spyOn(console, 'error').mockImplementation(() => {});
    try {
      const next = applyPatchOrdered(prev, {
        type: "match_updated",
        seq: 10,
        data: { result: { id: "p1", winner: "Alice" } },
      }, state, onGap);
      expect(next.poolMatches[0].winner).toEqual({ id: "Alice", name: "Alice" });
      expect(state.lastSeq).toBe(10);
      expect(onGap).toHaveBeenCalledTimes(1);
    } finally {
      errSpy.mockRestore();
    }
  });

  it('still applies competitor_status_updated patches via the underlying applyPatch', () => {
    // competitor_status_updated returns prev unchanged but dispatches a
    // window CustomEvent — applyPatchOrdered must not short-circuit
    // that. We verify by checking the event handler fires.
    const prev = makeState();
    const state = {};
    const onGap = vi.fn();
    const received = [];
    const handler = (e) => received.push(e.detail);
    window.addEventListener('competitor-status-updated', handler);
    try {
      applyPatchOrdered(prev, {
        type: "competitor_status_updated",
        seq: 1,
        data: { competitionId: "c1", status: { playerId: "p1", eligible: false } },
      }, state, onGap);
      expect(received).toHaveLength(1);
      expect(received[0].competitionId).toBe("c1");
      expect(state.lastSeq).toBe(1);
    } finally {
      window.removeEventListener('competitor-status-updated', handler);
    }
  });
});

describe('checkSeqGap', () => {
  it('returns {duplicate:false,gap:false} and sets lastSeq on first event', () => {
    const state = {};
    const onGap = vi.fn();
    const result = checkSeqGap(state, 7, onGap);
    expect(result).toEqual({ duplicate: false, gap: false });
    expect(state.lastSeq).toBe(7);
    expect(onGap).not.toHaveBeenCalled();
  });

  it('returns {duplicate:true,gap:false} and does NOT advance lastSeq for replay', () => {
    const state = { lastSeq: 10 };
    const onGap = vi.fn();
    const result = checkSeqGap(state, 8, onGap);
    expect(result).toEqual({ duplicate: true, gap: false });
    expect(state.lastSeq).toBe(10); // unchanged
    expect(onGap).not.toHaveBeenCalled();
  });

  it('returns {duplicate:true,gap:false} for equal-seq (same-seq replay)', () => {
    const state = { lastSeq: 10 };
    const onGap = vi.fn();
    const result = checkSeqGap(state, 10, onGap);
    expect(result).toEqual({ duplicate: true, gap: false });
    expect(state.lastSeq).toBe(10);
    expect(onGap).not.toHaveBeenCalled();
  });

  it('returns {gap:true,duplicate:false}, calls onGap, and advances lastSeq on jump', () => {
    const state = { lastSeq: 5 };
    const onGap = vi.fn();
    const result = checkSeqGap(state, 9, onGap);
    expect(result).toEqual({ gap: true, duplicate: false });
    expect(state.lastSeq).toBe(9);
    expect(onGap).toHaveBeenCalledTimes(1);
    expect(onGap).toHaveBeenCalledWith({ from: 6, to: 8 });
  });

  it('returns {duplicate:false,gap:false} and does NOT mutate state when seq is not a number', () => {
    const state = { lastSeq: 5 };
    const onGap = vi.fn();
    const result = checkSeqGap(state, undefined, onGap);
    expect(result).toEqual({ duplicate: false, gap: false });
    expect(state.lastSeq).toBe(5); // unchanged
    expect(onGap).not.toHaveBeenCalled();
  });

  it('swallows a throwing onGap and still advances lastSeq', () => {
    const state = { lastSeq: 3 };
    const onGap = vi.fn(() => { throw new Error("boom"); });
    const errSpy = vi.spyOn(console, 'error').mockImplementation(() => {});
    try {
      const result = checkSeqGap(state, 10, onGap);
      expect(result).toEqual({ gap: true, duplicate: false });
      expect(state.lastSeq).toBe(10);
      expect(onGap).toHaveBeenCalledTimes(1);
    } finally {
      errSpy.mockRestore();
    }
  });
});
