import { describe, it, expect } from 'vitest';
import { changedLineupPositions, lineupDuplicateNote, alreadyPlacedNote } from '../lineup_resolver.jsx';

const KEYS = ['1', '2', '3'];
const side = (positions = {}, memberIds = {}) => ({ positions, memberIds });

describe('changedLineupPositions', () => {
  it('reports nothing for identical sides', () => {
    const s = side({ 1: 'Aoki', 2: 'Sato' }, { 1: 'mem-1' });
    expect(changedLineupPositions(s, side({ 1: 'Aoki', 2: 'Sato' }, { 1: 'mem-1' }), KEYS)).toEqual([]);
  });

  it('treats "" and a missing key as the same, for names and ids', () => {
    expect(changedLineupPositions(side({ 1: '' }, { 1: '' }), side({}, {}), KEYS)).toEqual([]);
    expect(changedLineupPositions(side(), side({ 2: '' }, { 2: '' }), KEYS)).toEqual([]);
  });

  it('ignores surrounding whitespace in a name', () => {
    expect(changedLineupPositions(side({ 1: 'Aoki' }), side({ 1: '  Aoki ' }), KEYS)).toEqual([]);
  });

  it('reports a changed name', () => {
    expect(changedLineupPositions(side({ 1: 'Aoki' }), side({ 1: 'Sato' }), KEYS)).toEqual(['1']);
  });

  it('reports a changed member id even when the name is the same', () => {
    expect(changedLineupPositions(side({ 1: '' }, { 1: 'mem-1' }), side({ 1: '' }, { 1: 'mem-2' }), KEYS)).toEqual(['1']);
    expect(changedLineupPositions(side({ 1: 'Aoki' }), side({ 1: 'Aoki' }, { 1: 'mem-1' }), KEYS)).toEqual(['1']);
  });

  it('reports a position that was emptied', () => {
    expect(changedLineupPositions(side({ 1: 'Aoki' }, { 1: 'mem-1' }), side(), KEYS)).toEqual(['1']);
  });

  it('only looks at the given position keys, in their order', () => {
    const base = side({ 1: 'A', 2: 'B', 9: 'X' });
    const cur = side({ 1: 'a', 2: 'b', 9: 'Y' });
    expect(changedLineupPositions(base, cur, ['2', '1'])).toEqual(['2', '1']);
  });

  it('tolerates a missing side', () => {
    expect(changedLineupPositions(undefined, side({ 1: 'A' }), KEYS)).toEqual(['1']);
    expect(changedLineupPositions(side({ 1: 'A' }), undefined, KEYS)).toEqual(['1']);
  });
});

// lineupDuplicateNote is asked of the lineup a Save leaves behind (the form with its
// changes, which is what the server composes on the lineup it holds), by both
// editors, so the refusal for one member at two positions reads alike. It names the
// position the operator did NOT change: the one that was already there. Of two
// they changed it names the one they picked the member for, never the box they typed
// a name into, and failing that the earlier.
describe('lineupDuplicateNote', () => {
  const label = (key) => `Position ${key}`;
  const note = (positions, memberIds, changed = [], typed = []) => lineupDuplicateNote(positions, memberIds, label, KEYS, changed, typed);

  it('is empty for a lineup that fields each member once, and for one with no ids at all', () => {
    expect(note({ 1: 'Aoki', 2: 'Sato' }, { 1: 'mem-1', 2: 'mem-2' })).toBe('');
    expect(note({ 1: 'Aoki', 2: 'Sato' }, {})).toBe('');
    expect(note({ 1: 'Aoki' }, undefined)).toBe('');
  });

  it('names the fighter and the position the operator did not change, whichever of the two comes first', () => {
    const positions = { 1: 'Mori', 2: 'Mori' };
    const ids = { 1: 'mem-4', 2: 'mem-4' };
    expect(note(positions, ids, ['2'])).toBe('Mori is already at Position 1.');
    expect(note(positions, ids, ['1'])).toBe('Mori is already at Position 2.');
  });

  it('does not depend on the order the ids were collected in: an id resolved last is still the operator\'s', () => {
    const keys = ['senpo', 'jiho', 'chuken'];
    const words = { senpo: 'Senpo', jiho: 'Jiho', chuken: 'Chuken' };
    // Senpo is stored; Jiho's id, resolved from a typed name, is added after the others.
    const ids = { senpo: 'mem-4', chuken: 'mem-3' };
    ids.jiho = 'mem-4';
    const positions = { senpo: 'Mori', jiho: 'Mori', chuken: 'Ito' };
    expect(lineupDuplicateNote(positions, ids, (key) => words[key], keys, ['jiho'])).toBe('Mori is already at Senpo.');
    expect(lineupDuplicateNote(positions, ids, (key) => words[key], keys, ['senpo'])).toBe('Mori is already at Jiho.');
  });

  it('names the earlier position when the operator changed both of them, or neither', () => {
    const positions = { 1: 'Mori', 2: 'Mori' };
    const ids = { 1: 'mem-4', 2: 'mem-4' };
    expect(note(positions, ids, ['1', '2'])).toBe('Mori is already at Position 1.');
    expect(note(positions, ids, [])).toBe('Mori is already at Position 1.');
    expect(note(positions, ids, ['3'])).toBe('Mori is already at Position 1.');
  });

  it('names the position the member was picked for, not the box a name was typed into, when the operator changed both', () => {
    const positions = { 1: 'Mori', 2: 'Mori' };
    const ids = { 1: 'mem-4', 2: 'mem-4' };
    expect(note(positions, ids, ['1', '2'], ['1'])).toBe('Mori is already at Position 2.');
    expect(note(positions, ids, ['1', '2'], ['2'])).toBe('Mori is already at Position 1.');
  });

  it('names the earlier position when the operator typed a name into both of them, or picked the member for both', () => {
    const positions = { 1: 'Mori', 2: 'Mori' };
    const ids = { 1: 'mem-4', 2: 'mem-4' };
    expect(note(positions, ids, ['1', '2'], ['1', '2'])).toBe('Mori is already at Position 1.');
    expect(note(positions, ids, ['1', '2'], [])).toBe('Mori is already at Position 1.');
  });

  it('names a position the operator did not change before it looks at what they typed', () => {
    const positions = { 1: 'Mori', 2: 'Mori' };
    const ids = { 1: 'mem-4', 2: 'mem-4' };
    expect(note(positions, ids, ['1'], ['1'])).toBe('Mori is already at Position 2.');
    expect(note(positions, ids, ['2'], ['2'])).toBe('Mori is already at Position 1.');
    expect(note(positions, ids, ['2'], [])).toBe('Mori is already at Position 1.');
  });

  it('takes the name from the position it names, and from the other when that one has none yet', () => {
    expect(note({ 1: 'Mori', 2: 'mori' }, { 1: 'mem-4', 2: 'mem-4' }, ['2'])).toBe('Mori is already at Position 1.');
    expect(note({ 1: '', 2: 'Kato' }, { 1: 'mem-6', 2: 'mem-6' }, ['2'])).toBe('Kato is already at Position 1.');
  });

  it('trims the name, and says "This fighter" for a placement that has none yet', () => {
    expect(note({ 1: ' Aoki ', 2: 'Sato' }, { 1: 'mem-1', 2: 'mem-1' })).toBe('Aoki is already at Position 1.');
    expect(note({ 1: '', 2: '' }, { 1: 'mem-1', 2: 'mem-1' })).toBe('This fighter is already at Position 1.');
    expect(note({ 1: '  ', 2: 'Sato' }, { 1: 'mem-1', 2: 'mem-1' })).toBe('Sato is already at Position 1.');
  });

  it('is not troubled by positions that hold no member', () => {
    expect(note({ 1: 'Aoki', 2: 'Sato', 3: 'Ito' }, { 1: '', 2: '', 3: 'mem-3' })).toBe('');
  });
});

// alreadyPlacedNote words that refusal for every writer: lineupDuplicateNote
// above, the score sheet's row pick and the Lineups page's add path.
describe('alreadyPlacedNote', () => {
  it('names the fighter and the position that already holds them', () => {
    expect(alreadyPlacedNote('Aoki', 'Senpo')).toBe('Aoki is already at Senpo.');
  });

  it('trims the name', () => {
    expect(alreadyPlacedNote('  Aoki ', 'Position 2')).toBe('Aoki is already at Position 2.');
  });

  it('says "This fighter" for a name that is empty, blank or missing', () => {
    expect(alreadyPlacedNote('', 'Senpo')).toBe('This fighter is already at Senpo.');
    expect(alreadyPlacedNote('   ', 'Senpo')).toBe('This fighter is already at Senpo.');
    expect(alreadyPlacedNote(undefined, 'Senpo')).toBe('This fighter is already at Senpo.');
    expect(alreadyPlacedNote(null, 'Senpo')).toBe('This fighter is already at Senpo.');
  });
});
