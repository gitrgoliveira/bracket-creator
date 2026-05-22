import { describe, it, expect } from 'vitest';
import { decideRankCommit, buildLiveById, isRanksLocked } from '../admin_pools.jsx';

// decideRankCommit is the pure predicate that drives RankInput.handleBlur.
// It returns one of:
//   {action: "noop"}                    — do nothing
//   {action: "sync", value: <string>}   — setV(value), don't commit
//   {action: "revert", value: <string>} — setV(value) (visual revert)
//   {action: "commit", value: <string>} — call onCommit(value)
//
// The component test (focus/blur/keydown DOM events) is out of scope for
// the current vitest setup which mocks React with stub hooks. These pure
// tests cover the decision logic that drives those behaviours.

describe('decideRankCommit', () => {
  describe('cancelled (Esc was pressed)', () => {
    it('is noop regardless of other inputs', () => {
      expect(decideRankCommit({ v: "5", initial: 2, focusValue: "2", cancelled: true }))
        .toEqual({ action: "noop" });
      // Even if the typed value would otherwise be valid + different:
      expect(decideRankCommit({ v: "999", initial: 1, focusValue: "1", cancelled: true }))
        .toEqual({ action: "noop" });
    });
  });

  describe('focus-without-edit (v === focusValue)', () => {
    it('is noop when initial is unchanged', () => {
      expect(decideRankCommit({ v: "2", initial: 2, focusValue: "2", cancelled: false }))
        .toEqual({ action: "noop" });
    });

    it('syncs to latest initial when initial changed during focus (TOCTOU guard)', () => {
      // User focused while seeing rank=2 (v="2", focusValue="2").
      // SSE updated rank to 5 (initial=5). User clicked away without typing.
      // We must NOT commit "2" (would revert the server's 5). Instead,
      // visually sync to 5.
      expect(decideRankCommit({ v: "2", initial: 5, focusValue: "2", cancelled: false }))
        .toEqual({ action: "sync", value: "5" });
    });
  });

  describe('invalid input → revert', () => {
    it('reverts on non-numeric typing', () => {
      expect(decideRankCommit({ v: "abc", initial: 3, focusValue: "3", cancelled: false }))
        .toEqual({ action: "revert", value: "3" });
    });

    it('reverts on empty string', () => {
      expect(decideRankCommit({ v: "", initial: 3, focusValue: "3", cancelled: false }))
        .toEqual({ action: "revert", value: "3" });
    });

    it('reverts on zero', () => {
      expect(decideRankCommit({ v: "0", initial: 3, focusValue: "3", cancelled: false }))
        .toEqual({ action: "revert", value: "3" });
    });

    it('reverts on negative', () => {
      expect(decideRankCommit({ v: "-1", initial: 3, focusValue: "3", cancelled: false }))
        .toEqual({ action: "revert", value: "3" });
    });

    it('reverts on rank > 1000 (matches server cap)', () => {
      expect(decideRankCommit({ v: "1001", initial: 3, focusValue: "3", cancelled: false }))
        .toEqual({ action: "revert", value: "3" });
      expect(decideRankCommit({ v: "999999", initial: 3, focusValue: "3", cancelled: false }))
        .toEqual({ action: "revert", value: "3" });
    });

    it('accepts rank = 1000 (boundary)', () => {
      expect(decideRankCommit({ v: "1000", initial: 3, focusValue: "3", cancelled: false }))
        .toEqual({ action: "commit", value: "1000" });
    });

    it('reverts on NaN/Infinity-like input', () => {
      expect(decideRankCommit({ v: "NaN", initial: 3, focusValue: "3", cancelled: false }))
        .toEqual({ action: "revert", value: "3" });
      // parseInt("Infinity") is NaN, so this also reverts.
      expect(decideRankCommit({ v: "Infinity", initial: 3, focusValue: "3", cancelled: false }))
        .toEqual({ action: "revert", value: "3" });
    });
  });

  describe('valid edit → commit', () => {
    it('commits when user changed rank to a different valid value', () => {
      expect(decideRankCommit({ v: "5", initial: 2, focusValue: "2", cancelled: false }))
        .toEqual({ action: "commit", value: "5" });
    });

    it('does not commit when normalized value matches initial (e.g. typed "02" for 2)', () => {
      // parseInt("02") === 2 === String(initial). No real change.
      expect(decideRankCommit({ v: "02", initial: 2, focusValue: "2", cancelled: false }))
        .toEqual({ action: "noop" });
    });

    it('strips leading whitespace via parseInt (commit normalized)', () => {
      // parseInt("  5  ") === 5
      expect(decideRankCommit({ v: "  5  ", initial: 2, focusValue: "2", cancelled: false }))
        .toEqual({ action: "commit", value: "5" });
    });

    it('truncates fractional input via parseInt', () => {
      expect(decideRankCommit({ v: "5.7", initial: 2, focusValue: "2", cancelled: false }))
        .toEqual({ action: "commit", value: "5" });
    });

    it('handles "5abc" by parsing leading int', () => {
      // parseInt("5abc") === 5 — user got tired of typing or paste went wrong.
      expect(decideRankCommit({ v: "5abc", initial: 2, focusValue: "2", cancelled: false }))
        .toEqual({ action: "commit", value: "5" });
    });
  });

  describe('priority ordering', () => {
    it('cancelled wins over focus-without-edit-sync', () => {
      // SSE-changed initial AND Esc pressed simultaneously → cancelled
      // takes priority. We don't sync; we trust the Esc handler's setV.
      expect(decideRankCommit({ v: "2", initial: 5, focusValue: "2", cancelled: true }))
        .toEqual({ action: "noop" });
    });

    it('focus-without-edit wins over invalid-input revert', () => {
      // If v === focusValue, we never reach the parseInt branch. (User
      // didn't type — there's nothing to validate.) This matters for
      // an exotic case: focusValue is "0" somehow (shouldn't happen but
      // defensive) — we still sync rather than revert.
      expect(decideRankCommit({ v: "0", initial: 0, focusValue: "0", cancelled: false }))
        .toEqual({ action: "noop" });
    });
  });

  describe('RankInput consumer contract: commit value mirrors to local state', () => {
    // Copilot round-7 finding: when decideRankCommit returns {action:"commit",
    // value: "5"} for typed "5abc", RankInput.handleBlur must call BOTH
    // setV(result.value) AND onCommit(result.value). The earlier version only
    // called onCommit, leaving the input displaying "5abc" until the
    // SSE-driven prop refresh hit useEffectA — a confusing few-hundred-ms
    // window where the visible value didn't match what was sent.
    //
    // The handleBlur dispatch lives in admin_pools.jsx:70-91 and can't be
    // unit-tested in isolation without DOM rendering (vitest setup mocks
    // React with stubs; tracked as follow-up #4/#7). These assertions pin
    // the contract that handleBlur depends on: result.value is what should
    // be mirrored into local state on commit.

    it('commit result.value is the normalized form (whitespace trimmed)', () => {
      const r = decideRankCommit({ v: "  5  ", initial: 2, focusValue: "2", cancelled: false });
      expect(r).toEqual({ action: "commit", value: "5" });
      // Consumer contract: setV("5") so the input shows "5" not "  5  ".
    });

    it('commit result.value is the normalized form (trailing junk stripped)', () => {
      const r = decideRankCommit({ v: "5abc", initial: 2, focusValue: "2", cancelled: false });
      expect(r).toEqual({ action: "commit", value: "5" });
      // Consumer contract: setV("5") so the input shows "5" not "5abc".
    });

    it('commit result.value is the normalized form (fraction truncated)', () => {
      const r = decideRankCommit({ v: "5.9", initial: 2, focusValue: "2", cancelled: false });
      expect(r).toEqual({ action: "commit", value: "5" });
      // Consumer contract: setV("5") so the input shows "5" not "5.9".
    });

    it('commit result.value differs from the raw v for the normalization shapes above', () => {
      // Sanity: if these match, the consumer's setV(result.value) is a no-op
      // (whatever was typed is already canonical) and the bug doesn't manifest.
      // For "5abc" / "  5  " / "5.9" the raw v differs from result.value —
      // these are exactly the inputs where the consumer-side bug surfaces.
      for (const v of ["  5  ", "5abc", "5.9"]) {
        const r = decideRankCommit({ v, initial: 2, focusValue: "2", cancelled: false });
        expect(r.action).toBe("commit");
        expect(r.value).not.toBe(v);
      }
    });
  });
});

describe('buildLiveById', () => {
  it('returns an object keyed by match id', () => {
    const matches = [
      { id: 'pool-A-1', status: 'completed', ipponsA: 2, ipponsB: 0 },
      { id: 'pool-A-2', status: 'scheduled' },
    ];
    const live = buildLiveById(matches);
    expect(live['pool-A-1'].status).toBe('completed');
    expect(live['pool-A-2'].status).toBe('scheduled');
  });

  it('returns empty object when poolMatches is null or empty', () => {
    expect(buildLiveById(null)).toEqual({});
    expect(buildLiveById([])).toEqual({});
  });

  it('live state overrides stale pool.matches entry', () => {
    const stale = { id: 'pool-A-1', status: 'scheduled' };
    const live = buildLiveById([{ id: 'pool-A-1', status: 'completed', ipponsA: 1, ipponsB: 0 }]);
    const resolved = live[stale.id] || stale;
    expect(resolved.status).toBe('completed');
    expect(resolved.ipponsA).toBe(1);
  });

  it('falls back to stale entry when match not yet in live list', () => {
    const stale = { id: 'pool-A-99', status: 'scheduled' };
    const live = buildLiveById([]);
    const resolved = live[stale.id] || stale;
    expect(resolved.status).toBe('scheduled');
  });
});

describe('isRanksLocked', () => {
  it('unlocked when status is pools', () => {
    expect(isRanksLocked('pools')).toBe(false);
  });

  it('locked when status is playoffs', () => {
    expect(isRanksLocked('playoffs')).toBe(true);
  });

  it('locked when status is completed', () => {
    expect(isRanksLocked('completed')).toBe(true);
  });

  it('locked when status is setup', () => {
    // CompStatusSetup ("setup") is the pre-pools state. The component
    // early-returns when pools is empty, so this branch rarely renders,
    // but the predicate must still report locked for defense-in-depth.
    expect(isRanksLocked('setup')).toBe(true);
  });

  it('locked when status is invalid', () => {
    // CompStatusInvalid ("invalid") is set when a competition is reset.
    // Pools may still exist on disk but rank inputs must not be editable.
    expect(isRanksLocked('invalid')).toBe(true);
  });

  it('locked when status is empty string or undefined', () => {
    expect(isRanksLocked('')).toBe(true);
    expect(isRanksLocked(undefined)).toBe(true);
  });
});
