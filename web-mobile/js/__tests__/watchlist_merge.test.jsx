// bc-wlpl: what opening a shared watchlist link does to the list the device
// already holds.
//
// This file exists because of a specific near-miss, and the comments say so on
// purpose. A commit briefly shipped `normalizeWatchlist(shared)` where the
// source had `normalizeWatchlist([...prev, ...shared])` -- one expression, and
// the difference between a shared link ADDING competitors and DELETING every
// competitor the recipient was already watching. The full suite (4433 tests)
// passed with the bug in place, verified by mutation: the rule lived inline in
// a useEffect, so nothing could reach it.
//
// mergeSharedWatchlist is that rule with a name. Every test below is written
// to go red on the exact mutation that escaped.
import { describe, it, expect } from 'vitest';
import { mergeSharedWatchlist, WATCHLIST_MAX } from '../viewer_watchlist_core.jsx';

const player = (id, name) => ({ type: 'player', id, name, dojo: 'Hagane' });
const dojo = (name) => ({ type: 'dojo', dojo: name });

describe('mergeSharedWatchlist', () => {
  it('KEEPS what the device already watches', () => {
    // THE regression. `normalizeWatchlist(shared)` passes every other test in
    // this file and fails only this one, so this assertion is the guard.
    const mine = [player('p1', 'Ken Saito')];
    const shared = [player('p2', 'Kenji Mori')];
    const out = mergeSharedWatchlist(mine, shared);
    expect(out.map((e) => e.id)).toContain('p1');
  });

  it('adds the shared entries alongside, in order', () => {
    const mine = [player('p1', 'Ken Saito')];
    const shared = [player('p2', 'Kenji Mori'), dojo('Kobe')];
    const out = mergeSharedWatchlist(mine, shared);
    expect(out.map((e) => e.id || e.dojo)).toEqual(['p1', 'p2', 'Kobe']);
  });

  it('a competitor on both lists is kept ONCE, and the device\'s own entry wins', () => {
    // First occurrence wins, so a local entry that carries a pin or a name the
    // reader recognises is not overwritten by the incoming copy.
    const mine = [{ type: 'player', id: 'p1', name: 'Ken Saito', dojo: 'Nara' }];
    const shared = [{ type: 'player', id: 'p1', name: 'K. SAITO', dojo: 'NARA' }];
    const out = mergeSharedWatchlist(mine, shared);
    expect(out).toHaveLength(1);
    expect(out[0].name, "the device's own entry survives").toBe('Ken Saito');
  });

  it('caps the result at WATCHLIST_MAX rather than letting a big link overflow it', () => {
    const mine = Array.from({ length: WATCHLIST_MAX - 1 }, (_, i) => player(`mine${i}`, `Mine ${i}`));
    const shared = Array.from({ length: 20 }, (_, i) => player(`shared${i}`, `Shared ${i}`));
    const out = mergeSharedWatchlist(mine, shared);
    expect(out).toHaveLength(WATCHLIST_MAX);
    // The cap must not evict what was already there: the device's list is the
    // one the reader built by hand.
    expect(out.filter((e) => String(e.id).startsWith('mine'))).toHaveLength(WATCHLIST_MAX - 1);
  });

  it('an empty shared list leaves the device untouched', () => {
    const mine = [player('p1', 'Ken Saito')];
    expect(mergeSharedWatchlist(mine, [])).toEqual(mine);
  });

  it('an empty device takes the shared list whole', () => {
    const shared = [player('p2', 'Kenji Mori')];
    expect(mergeSharedWatchlist([], shared)).toEqual(shared);
  });

  it('tolerates null/undefined on either side', () => {
    // useWatchlist can hand back [] before storage is read, and
    // resolveWatchlistTokens returns [] for a link that resolves to nobody.
    expect(mergeSharedWatchlist(null, null)).toEqual([]);
    expect(mergeSharedWatchlist(undefined, [player('p1', 'A')])).toHaveLength(1);
    expect(mergeSharedWatchlist([player('p1', 'A')], undefined)).toHaveLength(1);
  });

  it('drops an unusable incoming entry without dropping the good ones', () => {
    // normalizeWatchlist's own guard, asserted here because a shared link is
    // the one path where entries arrive from OUTSIDE this device.
    const mine = [player('p1', 'Ken Saito')];
    const shared = [{ type: 'player', name: 'No id at all' }, player('p2', 'Kenji Mori')];
    const out = mergeSharedWatchlist(mine, shared);
    expect(out.map((e) => e.id)).toEqual(['p1', 'p2']);
  });
});
