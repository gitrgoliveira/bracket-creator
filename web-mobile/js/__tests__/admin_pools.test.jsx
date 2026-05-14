import { describe, it, expect } from 'vitest';
import { decideRankCommit } from '../admin_pools.jsx';

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
});
