import { describe, it, expect } from 'vitest';
import { isPlayerWatched, buildWatchedSets } from '../viewer_watchlist_core.jsx';

// bc-pnum (Opus review round): isPlayerWatched used to pool watched ids AND
// lowercased names into ONE flat Set and accept a hit on either
// independently, so watching Sato of Tokyo also highlighted Sato of Osaka's
// rows in the bracket/pool/schedule wherever the id check missed. `watched`
// is now {ids, names} (buildWatchedSets): the CHECKED player decides which
// set to consult by its own id presence.
describe('isPlayerWatched', () => {
  it('an id-carrying player matches only a watched id, never falling through to name', () => {
    const watched = buildWatchedSets([{ id: 'sato-tokyo', name: 'Sato' }]);
    const osaka = { id: 'sato-osaka', name: 'Sato' };
    expect(isPlayerWatched(osaka, watched)).toBe(false);
    const tokyo = { id: 'sato-tokyo', name: 'Sato' };
    expect(isPlayerWatched(tokyo, watched)).toBe(true);
  });

  it('an id-less player matches by name (unresolved bracket side, or a bare name string)', () => {
    const watched = buildWatchedSets([{ id: '', name: 'Sato' }]);
    expect(isPlayerWatched({ id: '', name: 'Sato' }, watched)).toBe(true);
    expect(isPlayerWatched('Sato', watched)).toBe(true);
    expect(isPlayerWatched({ id: '', name: 'Tanaka' }, watched)).toBe(false);
  });

  // An id-CARRYING player must never fall through to a name hit, even when
  // the watchlist separately holds an id-less "Sato" entry (a legitimately
  // different, unresolved watch target). The checked player's OWN id
  // presence decides which set is consulted -- never both.
  it('an id-carrying player never matches by name, even when the watchlist also holds an id-less same-name entry', () => {
    const watched = buildWatchedSets([{ id: '', name: 'Sato' }]);
    const realSatoElsewhere = { id: 'sato-osaka', name: 'Sato' };
    expect(isPlayerWatched(realSatoElsewhere, watched)).toBe(false);
  });

  it('is false for null/undefined players or an empty/absent watched set', () => {
    expect(isPlayerWatched(null, buildWatchedSets([{ id: 'x' }]))).toBe(false);
    expect(isPlayerWatched({ id: 'x', name: 'X' }, null)).toBe(false);
    expect(isPlayerWatched({ id: 'x', name: 'X' }, buildWatchedSets([]))).toBe(false);
  });

  // admin_pools.jsx / admin_shiaijo.jsx pass highlightPlayers={[]} (a legacy
  // "no watchlist concept here" sentinel, not the {ids,names} shape): must
  // degrade to "not watched" rather than throwing.
  it('degrades safely when watched is a bare array (admin console sentinel)', () => {
    expect(isPlayerWatched({ id: 'x', name: 'X' }, [])).toBe(false);
  });
});

describe('buildWatchedSets', () => {
  it('routes an id-carrying entry to ids only, an id-less entry to names only', () => {
    const { ids, names } = buildWatchedSets([
      { id: 'S1', name: 'Sato' },
      { id: '', name: 'Tanaka' },
    ]);
    expect(ids.has('S1')).toBe(true);
    expect(names.has('sato')).toBe(false); // id-carrying entry's name is NOT duplicated into names
    expect(names.has('tanaka')).toBe(true);
  });

  it('tolerates a non-array input', () => {
    expect(buildWatchedSets(null).ids.size).toBe(0);
    expect(buildWatchedSets(undefined).names.size).toBe(0);
  });
});
