import { describe, it, expect } from 'vitest';
import { filterSecondaryOnDeck } from '../viewer_home.jsx';

// bc-pnum (Opus review round): filterSecondaryOnDeck backs ViewerHome's
// quiet on-deck banner for non-primary watched players. A side WITH an id
// matches only a watched id; a side WITHOUT one matches by name only.
describe('filterSecondaryOnDeck', () => {
  const noPrimary = new Set();

  it('a side carrying a real id matches only a watched id, never by name', () => {
    const watched = [{ id: 'sato-tokyo', name: 'Sato' }];
    const osakaMatch = { id: 'm1', status: 'running', sideA: { id: 'sato-osaka', name: 'Sato' }, sideB: { id: 'other', name: 'Someone' } };
    expect(filterSecondaryOnDeck([osakaMatch], watched, noPrimary)).toEqual([]);
  });

  it('a side carrying the watched id matches', () => {
    const watched = [{ id: 'sato-tokyo', name: 'Sato' }];
    const tokyoMatch = { id: 'm1', status: 'running', sideA: { id: 'sato-tokyo', name: 'Sato' }, sideB: { id: 'other', name: 'Someone' } };
    expect(filterSecondaryOnDeck([tokyoMatch], watched, noPrimary).map((m) => m.id)).toEqual(['m1']);
  });

  it('an id-less side matches by name (unresolved bracket row)', () => {
    const watched = [{ id: '', name: 'Sato' }];
    const idLessMatch = { id: 'm1', status: 'running', sideA: { id: '', name: 'Sato' }, sideB: { id: 'other', name: 'Someone' } };
    expect(filterSecondaryOnDeck([idLessMatch], watched, noPrimary).map((m) => m.id)).toEqual(['m1']);
  });

  it('excludes a match already covered by the primary watched player', () => {
    const watched = [{ id: 'sato-tokyo', name: 'Sato' }];
    const primaryIds = new Set(['sato-tokyo']);
    const tokyoMatch = { id: 'm1', status: 'running', sideA: { id: 'sato-tokyo', name: 'Sato' }, sideB: { id: 'other', name: 'Someone' } };
    expect(filterSecondaryOnDeck([tokyoMatch], watched, primaryIds)).toEqual([]);
  });

  it('returns [] when nothing is watched', () => {
    const m = { id: 'm1', status: 'running', sideA: { id: 'a', name: 'A' }, sideB: { id: 'b', name: 'B' } };
    expect(filterSecondaryOnDeck([m], [], noPrimary)).toEqual([]);
  });
});
