// A dojo watch matches the dojo whatever case it was saved in. The server
// title-cases every dojo, so a link or a pin saved before that holds
// "kenshinkan" while the roster now says "Kenshinkan".
import { describe, it, expect } from 'vitest';
import { entryKey, normalizeEntryKey, resolveEntryPlayerIds, sameDojo } from '../viewer_watchlist_core.jsx';

const ROSTER = [
  { id: 'p1', name: 'Rin Sato', dojo: 'Kenshinkan' },
  { id: 'p2', name: 'Yuki Tanaka', dojo: 'Tobukan' },
  { id: 'p3', name: 'Aiko Mori', dojo: 'Kenshinkan' },
];

describe('dojo watch entries ignore case', () => {
  it('an old lower-case dojo entry resolves the dojo as stored now', () => {
    expect(resolveEntryPlayerIds({ type: 'dojo', dojo: 'kenshinkan' }, ROSTER)).toEqual(['p1', 'p3']);
  });

  it('an old link and a fresh pick of the same dojo are one entry', () => {
    expect(entryKey({ type: 'dojo', dojo: 'kenshinkan' })).toBe(entryKey({ type: 'dojo', dojo: 'Kenshinkan' }));
    expect(entryKey({ type: 'dojo', dojo: 'Tobukan' })).not.toBe(entryKey({ type: 'dojo', dojo: 'Kenshinkan' }));
  });

  it('a pin stored under the old key finds its entry', () => {
    const fresh = entryKey({ type: 'dojo', dojo: 'Kenshinkan' });
    expect(normalizeEntryKey('dojo:kenshinkan')).toBe(fresh);
    expect(normalizeEntryKey('dojo:Kenshinkan')).toBe(fresh);
    expect(normalizeEntryKey('player:p1')).toBe('player:p1');
    expect(normalizeEntryKey(null)).toBe('');
  });

  it('sameDojo compares by the normalised name', () => {
    expect(sameDojo('kenshinkan', 'Kenshinkan')).toBe(true);
    expect(sameDojo('KENSHINKAN', 'Kenshinkan')).toBe(true);
    expect(sameDojo('Tobukan', 'Kenshinkan')).toBe(false);
  });
});
