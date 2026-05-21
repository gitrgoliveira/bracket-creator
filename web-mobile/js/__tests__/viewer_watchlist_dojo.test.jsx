// Pure-logic tests for addDojoToWatchlist — coach-oriented bulk-add affordance.
import { describe, it, expect } from 'vitest';
import { addDojoToWatchlist } from '../viewer.jsx';

const roster = [
  { id: 'a1', name: 'Alice', dojo: 'Aoyama' },
  { id: 'a2', name: 'Akira', dojo: 'Aoyama' },
  { id: 'b1', name: 'Bob', dojo: 'Bunkyo' },
  { id: 'b2', name: 'Beni', dojo: 'Bunkyo' },
  { id: 'x1', name: 'Xena', dojo: '' }, // empty dojo — never matches
];

describe('addDojoToWatchlist', () => {
  it('adds every roster player from the dojo and dedups against existing', () => {
    const current = [{ id: 'a1', name: 'Alice', dojo: 'Aoyama' }];
    const { next, added, skipped } = addDojoToWatchlist(current, roster, 'Aoyama', 50);
    expect(added).toBe(1); // a2
    expect(skipped).toBe(0);
    expect(next.map((w) => w.id)).toEqual(['a1', 'a2']);
  });

  it('respects the max cap and reports the skipped count', () => {
    const current = [{ id: 'b1', name: 'Bob', dojo: 'Bunkyo' }];
    // max=2 → already at 1, only 1 slot left, with 2 candidates (a1, a2) so one is skipped.
    let r = addDojoToWatchlist(current, roster, 'Aoyama', 2);
    expect(r.added).toBe(1);
    expect(r.skipped).toBe(1); // a2 didn't fit
    expect(r.next.length).toBe(2);

    // Already full
    r = addDojoToWatchlist(r.next, roster, 'Bunkyo', 2);
    expect(r.added).toBe(0);
    expect(r.skipped).toBe(1); // b2 didn't fit (b1 was already in)
  });

  it('returns the original list unchanged when no dojo passed', () => {
    const current = [{ id: 'a1', name: 'Alice', dojo: 'Aoyama' }];
    const { next, added, skipped } = addDojoToWatchlist(current, roster, '', 50);
    expect(next).toBe(current);
    expect(added).toBe(0);
    expect(skipped).toBe(0);
  });

  it('never matches roster entries with an empty dojo', () => {
    const { next, added } = addDojoToWatchlist([], roster, '', 50);
    expect(next).toEqual([]);
    expect(added).toBe(0);
  });

  it('preserves dojo field on the watchlist entry', () => {
    const { next } = addDojoToWatchlist([], roster, 'Bunkyo', 50);
    expect(next).toEqual([
      { id: 'b1', name: 'Bob', dojo: 'Bunkyo' },
      { id: 'b2', name: 'Beni', dojo: 'Bunkyo' },
    ]);
  });

  it('is a no-op when everyone from the dojo is already watched', () => {
    const current = [
      { id: 'a1', name: 'Alice', dojo: 'Aoyama' },
      { id: 'a2', name: 'Akira', dojo: 'Aoyama' },
    ];
    const { next, added, skipped } = addDojoToWatchlist(current, roster, 'Aoyama', 50);
    expect(next).toEqual(current);
    expect(added).toBe(0);
    expect(skipped).toBe(0);
  });
});
