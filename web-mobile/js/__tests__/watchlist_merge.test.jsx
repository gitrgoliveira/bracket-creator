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
import { mergeSharedWatchlist, landedSharedKeys, sharedLinkPass, buildRoster, WATCHLIST_MAX } from '../viewer_watchlist_core.jsx';
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

// sharedLinkPass: the whole pass as a decision, and the proof that the
// sequence of passes ENDS.
//
// The effect that applies a ?w= link re-fires on every render (its setter is
// a fresh closure each time), so the only thing standing between it and a
// loop is that some pass writes nothing. It used to write whenever a token
// resolved and settle only once every token landed; at WATCHLIST_MAX those
// differ, and the page spun at ~60 localStorage writes a second. `drive`
// below is that effect with the state fed back, and every case must reach a
// pass that writes nothing within tokens+1 passes.
describe('sharedLinkPass', () => {
  const comp = (id, players) => ({ id, name: id, status: 'running', checkInEnabled: false, players });
  const ROSTER = buildRoster([comp('A', [
    { id: 'A-p1', name: 'Alice', dojo: 'Shibuya', number: 'K1' },
    { id: 'A-p2', name: 'Bob', dojo: 'Osaka', number: 'K2' },
  ])]);
  const filler = (n) => Array.from({ length: n }, (_, i) => ({ type: 'player', id: 'fill-' + i, name: 'F' + i, dojo: 'D' }));

  // The effect, with its state fed back. Returns how it came to rest.
  function drive({ search, roster = ROSTER, watchlist = [], rosterLoaded = true }, maxPasses = 6) {
    const applied = new Set();
    let list = watchlist;
    let writes = 0;
    for (let i = 1; i <= maxPasses; i++) {
      const pass = sharedLinkPass({ search, roster, watchlist: list, applied, rosterLoaded });
      pass.landed.forEach((k) => applied.add(k));
      if (pass.write) { list = mergeSharedWatchlist(list, pass.entries); writes++; continue; }
      return { passes: i, writes, list, settle: pass.settle, applied };
    }
    throw new Error('no quiescence within ' + maxPasses + ' passes');
  }

  it('an ordinary link lands, writes once, and settles', () => {
    const r = drive({ search: '?w=K1,K2' });
    expect(r.writes).toBe(1);
    expect(r.list.map((e) => e.id)).toEqual(['A-p1', 'A-p2']);
    expect(r.settle).toBe(true);
  });

  it('AT THE CAP: writes nothing, records nothing, keeps the query -- and stops', () => {
    // The loop. One pass, zero writes, not settled (so ?w= survives for the
    // reader to prune and retry), and the drive comes to rest immediately.
    const r = drive({ search: '?w=K1', watchlist: filler(WATCHLIST_MAX) });
    expect(r.passes).toBe(1);
    expect(r.writes).toBe(0);
    expect(r.applied.size).toBe(0);
    expect(r.settle).toBe(false);
    expect(r.list).toHaveLength(WATCHLIST_MAX);
  });

  it('STRADDLING the cap: the one that fits lands, the other holds the query, and it stops', () => {
    const r = drive({ search: '?w=K1,K2', watchlist: filler(WATCHLIST_MAX - 1) });
    expect(r.writes).toBe(1);
    expect(r.list.some((e) => e.id === 'A-p1')).toBe(true);
    expect(r.list.some((e) => e.id === 'A-p2')).toBe(false);
    expect(r.applied.has('competitor:K1')).toBe(true);
    expect(r.applied.has('competitor:K2')).toBe(false);
    expect(r.settle).toBe(false);
  });

  it('the prune-and-retry the query is kept for: room appears, the rest lands', () => {
    const first = drive({ search: '?w=K1', watchlist: filler(WATCHLIST_MAX) });
    expect(first.writes).toBe(0);
    const pruned = first.list.slice(1);
    const second = drive({ search: '?w=K1', watchlist: pruned });
    expect(second.list.some((e) => e.id === 'A-p1')).toBe(true);
    expect(second.settle).toBe(true);
  });

  it('an entry already watched counts as landed: one idempotent write, then quiet', () => {
    const already = [{ type: 'player', id: 'A-p1', name: 'Alice', dojo: 'Shibuya' }];
    const r = drive({ search: '?w=K1', watchlist: already });
    expect(r.passes).toBeLessThanOrEqual(2);
    expect(r.list).toHaveLength(1);
    expect(r.settle).toBe(true);
  });

  it('does not settle while a roster may still bring a token home', () => {
    const p = sharedLinkPass({ search: '?w=ZZ9', roster: ROSTER, watchlist: [], applied: new Set(), rosterLoaded: false });
    expect(p.write).toBe(false);
    expect(p.settle).toBe(false);
  });

  it('settles on a token genuinely not in this tournament once every roster is here', () => {
    const p = sharedLinkPass({ search: '?w=ZZ9', roster: ROSTER, watchlist: [], applied: new Set(), rosterLoaded: true });
    expect(p.write).toBe(false);
    expect(p.settle).toBe(true);
  });

  it('with no ?w= at all: nothing to write, settled on the first pass', () => {
    const p = sharedLinkPass({ search: '?playerNumber=K1', roster: ROSTER, watchlist: [], applied: new Set(), rosterLoaded: false });
    expect(p.write).toBe(false);
    expect(p.settle).toBe(true);
  });
});

