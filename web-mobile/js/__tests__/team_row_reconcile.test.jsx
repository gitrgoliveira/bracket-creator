// reconcileRowsToPositions is the one rule keeping the team editor's local
// bout board aligned with the positions the server currently has. It is pinned
// directly, not through the editor, because in the editor the adopt merge also
// re-shapes the committed state — so an index-aligned version looks correct
// there and the distinction that matters would go unnoticed.
import { describe, it, expect } from 'vitest';
import { reconcileRowsToPositions } from '../admin_scoring_team.jsx';

// _pos is the only field the reconciliation reads; `tag` stands in for the
// scoring state so a carried-over row is identifiable in the result.
const row = (pos, tag) => ({ _pos: pos, tag });
const DH = -1;

describe('reconcileRowsToPositions', () => {
  it('returns the SAME array when the board already aligns', () => {
    const rows = [row(1, 'a'), row(2, 'b')];
    // Identity, not just equality: a fresh array every render re-fires every
    // consumer keyed on it (the mp-gmcg F1 thrash).
    expect(reconcileRowsToPositions(rows, [row(1, 'x'), row(2, 'y')])).toBe(rows);
  });

  it('keeps the operator\'s rows and fills a newly appended position', () => {
    const out = reconcileRowsToPositions(
      [row(1, 'mine'), row(2, 'mine')],
      [row(1, 'srv'), row(2, 'srv'), row(3, 'srv')],
    );
    expect(out.map(r => [r._pos, r.tag])).toEqual([[1, 'mine'], [2, 'mine'], [3, 'srv']]);
  });

  it('drops a row whose position no longer exists', () => {
    // A daihyosen deleted on another device. Left in place it rides the next
    // full-snapshot write out as a phantom numbered bout.
    const out = reconcileRowsToPositions(
      [row(1, 'mine'), row(2, 'mine'), row(3, 'mine'), row(DH, 'rep')],
      [row(1, 'srv'), row(2, 'srv'), row(3, 'srv')],
    );
    expect(out.map(r => [r._pos, r.tag])).toEqual([[1, 'mine'], [2, 'mine'], [3, 'mine']]);
  });

  it('keeps the rep bout AT the rep position when the numbered count grows', () => {
    // The case index alignment gets wrong, and the reason this matches on
    // position at all. The daihyosen is last in both boards, so appending by
    // index leaves it sitting in numbered slot 4 — where its ippons are then
    // counted as a numbered bout's IV/PW and written to the wire as one.
    const out = reconcileRowsToPositions(
      [row(1, 'mine'), row(2, 'mine'), row(3, 'mine'), row(DH, 'rep')],
      [row(1, 'srv'), row(2, 'srv'), row(3, 'srv'), row(4, 'srv'), row(5, 'srv'), row(DH, 'srv')],
    );
    expect(out.map(r => [r._pos, r.tag])).toEqual([
      [1, 'mine'], [2, 'mine'], [3, 'mine'], [4, 'srv'], [5, 'srv'], [DH, 'rep'],
    ]);
  });

  it('rebuilds when the shape is right but a position moved', () => {
    // Same length, different positions: an index-only check would call this
    // aligned and leave every row pointing at the wrong bout.
    const out = reconcileRowsToPositions(
      [row(1, 'mine'), row(2, 'mine'), row(DH, 'rep')],
      [row(1, 'srv'), row(2, 'srv'), row(3, 'srv')],
    );
    expect(out.map(r => [r._pos, r.tag])).toEqual([[1, 'mine'], [2, 'mine'], [3, 'srv']]);
  });
});
