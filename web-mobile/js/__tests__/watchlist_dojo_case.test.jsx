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

  it('sameDojo ignores letter case', () => {
    expect(sameDojo('kenshinkan', 'Kenshinkan')).toBe(true);
    expect(sameDojo('KENSHINKAN', 'Kenshinkan')).toBe(true);
    expect(sameDojo('Tobukan', 'Kenshinkan')).toBe(false);
  });

  // D25: case only. The server keeps an accented dojo, and one spaced
  // differently, as a dojo of its own, so the watch does too.
  it('sameDojo does not fold accents or inner spacing', () => {
    expect(sameDojo('Kenshinkán', 'Kenshinkan')).toBe(false);
    expect(sameDojo('Kenshin Kan', 'Kenshinkan')).toBe(false);
    expect(entryKey({ type: 'dojo', dojo: 'Kenshinkán' })).not.toBe(entryKey({ type: 'dojo', dojo: 'Kenshinkan' }));
  });

  it('an accented dojo is its own watch, with its own competitors', () => {
    const roster = [...ROSTER, { id: 'p4', name: 'Ken Ito', dojo: 'Kenshinkán' }];
    expect(resolveEntryPlayerIds({ type: 'dojo', dojo: 'KENSHINKAN' }, roster)).toEqual(['p1', 'p3']);
    expect(resolveEntryPlayerIds({ type: 'dojo', dojo: 'Kenshinkán' }, roster)).toEqual(['p4']);
  });
});
