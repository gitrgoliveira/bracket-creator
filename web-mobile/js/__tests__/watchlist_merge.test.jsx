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
import { mergeSharedWatchlist, landedSharedKeys, buildRoster, WATCHLIST_MAX } from '../viewer_watchlist_core.jsx';
import { resolveFreshTokens } from '../watchlist_link.jsx';

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

// bc-wlpl: which tokens of a shared link are still outstanding, and what they
// resolve to against the roster AS IT STANDS. Lives beside the merge rule
// because the two together are what "opening a link" means.
describe('resolveFreshTokens', () => {
  const comp = (id, players) => ({ id, name: id, status: 'running', players });
  const alice = { id: 'A-p1', name: 'Alice', dojo: 'Shibuya', number: 'K1' };
  const bob = { id: 'B-p1', name: 'Bob', dojo: 'Kobe', number: 'V2' };
  const tok = (value) => ({ kind: 'competitor', value });

  it('resolves what the roster currently holds and reports the keys to remember', () => {
    const roster = buildRoster([comp('A', [alice])]);
    const { entries, keys } = resolveFreshTokens([tok('K1')], roster, new Set());
    expect(entries.map((e) => e.id)).toEqual(['A-p1']);
    expect(keys).toEqual(['competitor:K1']);
  });

  it('A PARTIALLY LOADED ROSTER LOSES NOTHING: the rest land on a later pass', () => {
    // THE regression this function exists for. A tournament payload can come
    // back with one competition's participants missing, and the link used to
    // be applied once against whatever had arrived -- so those entries were
    // dropped permanently, with no retry and no sign to either end.
    const applied = new Set();
    const partial = buildRoster([comp('A', [alice])]);          // B has not loaded
    const first = resolveFreshTokens([tok('K1'), tok('V2')], partial, applied);
    expect(first.entries.map((e) => e.id), 'only what loaded').toEqual(['A-p1']);
    first.keys.forEach((k) => applied.add(k));

    const full = buildRoster([comp('A', [alice]), comp('B', [bob])]); // B arrives
    const second = resolveFreshTokens([tok('K1'), tok('V2')], full, applied);
    expect(second.entries.map((e) => e.id), 'the straggler lands now').toEqual(['B-p1']);
    expect(second.entries, 'and Alice is NOT offered twice').toHaveLength(1);
  });

  it('never re-offers a token already applied, so a removed entry stays removed', () => {
    // The other half of the retry being safe. Without this, a roster that
    // heals would re-add a competitor the reader had just deleted.
    const roster = buildRoster([comp('A', [alice])]);
    const applied = new Set(['competitor:K1']);
    expect(resolveFreshTokens([tok('K1')], roster, applied).entries).toEqual([]);
  });

  it('a token that resolves to nobody is not recorded, so it is retried', () => {
    const roster = buildRoster([comp('A', [alice])]);
    const { entries, keys } = resolveFreshTokens([tok('ZZ99')], roster, new Set());
    expect(entries).toEqual([]);
    expect(keys, 'not recorded: the roster may simply not hold them YET').toEqual([]);
  });

  it('tolerates empty and missing inputs', () => {
    expect(resolveFreshTokens([], [], new Set()).entries).toEqual([]);
    expect(resolveFreshTokens(null, [], null).entries).toEqual([]);
  });

  // `outstanding` is how the caller knows the link is FINISHED with. It exists
  // because the alternative test -- "has every competition's roster loaded?"
  // -- is never satisfied when one of them permanently fails, and the caller
  // then never strips ?w= from the address bar, so every reload re-applies the
  // link and re-adds entries the reader has pruned.
  describe('outstanding: what a later pass could still answer', () => {
    it('is 0 once every token has been applied, even from a partial roster', () => {
      const applied = new Set();
      const partial = buildRoster([comp('A', [alice])]); // B never loads
      const first = resolveFreshTokens([tok('K1')], partial, applied);
      expect(first.outstanding, 'K1 resolved on this pass').toBe(0);
      first.keys.forEach((k) => applied.add(k));
      // The second pass sees it as already applied, which is equally settled.
      expect(resolveFreshTokens([tok('K1')], partial, applied).outstanding).toBe(0);
    });

    it('counts a token that resolved to nobody, because a later roster may hold it', () => {
      const partial = buildRoster([comp('A', [alice])]);
      const { outstanding } = resolveFreshTokens([tok('K1'), tok('V2')], partial, new Set());
      expect(outstanding, 'V2 belongs to a competition that has not loaded').toBe(1);
    });

    it('is 0 for a query carrying no tokens at all', () => {
      expect(resolveFreshTokens([], [], new Set()).outstanding).toBe(0);
      expect(resolveFreshTokens(null, [], null).outstanding).toBe(0);
    });
  });
});

// landedSharedKeys: the ledger must record what ARRIVED, not what resolved.
//
// The gap between the two is WATCHLIST_MAX. A reader already at the cap gets
// nothing from a link, and recording those tokens as applied told the ledger
// otherwise -- after which viewer_home stripped ?w= and the reader had no copy
// of the link left to retry with once they had pruned.
describe('landedSharedKeys', () => {
  const player = (n) => ({ type: 'player', id: 'p' + n, name: 'P' + n, dojo: 'D' });
  const keyFor = (n) => 'competitor:K' + n;

  it('reports every key when the list has room', () => {
    const entries = [player(1), player(2)];
    const keys = [keyFor(1), keyFor(2)];
    expect(landedSharedKeys([], entries, keys)).toEqual(keys);
  });

  it('reports NOTHING when the device is already at the cap', () => {
    const full = Array.from({ length: WATCHLIST_MAX }, (_, i) => player(100 + i));
    expect(full).toHaveLength(WATCHLIST_MAX);
    expect(landedSharedKeys(full, [player(1)], [keyFor(1)])).toEqual([]);
  });

  it('reports only the ones that FIT when the link straddles the cap', () => {
    // One free slot, two shared entries: the first lands, the second does not,
    // and the caller must keep retrying for the second alone.
    const nearlyFull = Array.from({ length: WATCHLIST_MAX - 1 }, (_, i) => player(100 + i));
    const landed = landedSharedKeys(nearlyFull, [player(1), player(2)], [keyFor(1), keyFor(2)]);
    expect(landed).toEqual([keyFor(1)]);
  });

  it('counts an entry the reader ALREADY watches as landed', () => {
    // It is in the list, so the link has nothing left to do for it. Retrying
    // forever on a duplicate would keep ?w= in the address bar for good.
    const landed = landedSharedKeys([player(1)], [player(1)], [keyFor(1)]);
    expect(landed).toEqual([keyFor(1)]);
  });

  it('the pruned-then-reload retry: what did not land, lands later', () => {
    const full = Array.from({ length: WATCHLIST_MAX }, (_, i) => player(100 + i));
    expect(landedSharedKeys(full, [player(1)], [keyFor(1)]), 'nothing at the cap').toEqual([]);
    const pruned = full.slice(1); // the reader removes one entry
    expect(landedSharedKeys(pruned, [player(1)], [keyFor(1)]), 'room now').toEqual([keyFor(1)]);
  });

  it('tolerates empty and missing inputs', () => {
    expect(landedSharedKeys([], [], [])).toEqual([]);
    expect(landedSharedKeys(null, null, null)).toEqual([]);
  });
});
