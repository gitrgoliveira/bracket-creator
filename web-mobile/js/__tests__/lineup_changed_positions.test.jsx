import { describe, it, expect } from 'vitest';
import { changedLineupPositions, composeLineupSave, lineupDuplicateNote, alreadyPlacedNote } from '../lineup_resolver.jsx';

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

// composeLineupSave is what a Save writes: the lineup as stored now, with the
// positions the operator changed put on it. Restating the whole form would put
// back, on every position the operator left alone, the value the form was read
// with, over a change another device made since.
describe('composeLineupSave', () => {
  const baseline = side({ 1: 'Aoki', 2: 'Sato', 3: 'Ito' }, { 1: 'mem-1', 2: 'mem-2', 3: 'mem-3' });
  // The operator changed position 1 and nothing else.
  const form = side({ 1: 'Mori', 2: 'Sato', 3: 'Ito' }, { 1: 'mem-4', 2: 'mem-2', 3: 'mem-3' });
  // Another device changed position 2 meanwhile.
  const stored = side({ 1: 'Aoki', 2: 'Kato', 3: 'Ito' }, { 1: 'mem-1', 2: 'mem-5', 3: 'mem-3' });

  it('takes a position the operator changed from the form, and every other from the stored lineup', () => {
    expect(composeLineupSave(baseline, form, stored, KEYS)).toEqual({
      positions: { 1: 'Mori', 2: 'Kato', 3: 'Ito' },
      memberIds: { 1: 'mem-4', 2: 'mem-5', 3: 'mem-3' },
    });
  });

  it('carries a member id with the name it was taken with, never one side\'s name and the other\'s id', () => {
    const idLess = side({ 1: 'Aoki', 2: 'Kato', 3: 'Ito' }, { 1: 'mem-1', 3: 'mem-3' });
    const composed = composeLineupSave(baseline, side({ 1: 'Mori', 2: 'Sato', 3: 'Ito' }, { 2: 'mem-2', 3: 'mem-3' }), idLess, KEYS);
    // Changed: the form's name with the form's (absent) id, not the stored id.
    expect(composed.positions[1]).toBe('Mori');
    expect(composed.memberIds[1]).toBe('');
    // Left alone: the stored name with the stored (absent) id, not the form's id.
    expect(composed.positions[2]).toBe('Kato');
    expect(composed.memberIds[2]).toBe('');
  });

  it('clears a position the operator cleared, whatever the stored lineup holds there', () => {
    const cleared = side({ 1: '', 2: 'Sato', 3: 'Ito' }, { 2: 'mem-2', 3: 'mem-3' });
    const composed = composeLineupSave(baseline, cleared, stored, KEYS);
    expect(composed.positions[1]).toBe('');
    expect(composed.memberIds[1]).toBe('');
    expect(composed.positions[2]).toBe('Kato');
  });

  it('is the form, restated, when what is stored is the baseline (a read that failed)', () => {
    expect(composeLineupSave(baseline, form, baseline, KEYS)).toEqual({
      positions: { 1: 'Mori', 2: 'Sato', 3: 'Ito' },
      memberIds: { 1: 'mem-4', 2: 'mem-2', 3: 'mem-3' },
    });
  });

  it('does not count a position put back to what was loaded as a change: the stored value stands', () => {
    const putBack = side({ 1: 'Aoki', 2: 'Sato', 3: 'Ito' }, { 1: 'mem-1', 2: 'mem-2', 3: 'mem-3' });
    expect(composeLineupSave(baseline, putBack, stored, KEYS)).toEqual({
      positions: { 1: 'Aoki', 2: 'Kato', 3: 'Ito' },
      memberIds: { 1: 'mem-1', 2: 'mem-5', 3: 'mem-3' },
    });
  });

  it('takes the unchanged positions as blank from a lineup that holds nothing (null: nothing in force)', () => {
    expect(composeLineupSave(baseline, form, null, KEYS)).toEqual({
      positions: { 1: 'Mori', 2: '', 3: '' },
      memberIds: { 1: 'mem-4', 2: '', 3: '' },
    });
  });

  it('answers over the position keys only, in the same shape whatever either side holds besides', () => {
    const extra = { ...stored, saved: true, sourceMatchId: 'm0', positions: { ...stored.positions, 9: 'Elsewhere' } };
    const composed = composeLineupSave(baseline, form, extra, KEYS);
    expect(Object.keys(composed)).toEqual(['positions', 'memberIds']);
    expect(Object.keys(composed.positions)).toEqual(KEYS);
    expect(Object.keys(composed.memberIds)).toEqual(KEYS);
  });

  it('changes nothing it was given', () => {
    const before = JSON.stringify([baseline, form, stored]);
    composeLineupSave(baseline, form, stored, KEYS);
    expect(JSON.stringify([baseline, form, stored])).toBe(before);
  });
});

// lineupDuplicateNote is asked of the lineup a Save is about to write, by both
// editors, so the refusal for one member at two positions reads alike. It names the
// position the operator did NOT change: the one that was already there.
describe('lineupDuplicateNote', () => {
  const label = (key) => `Position ${key}`;
  const note = (positions, memberIds, changed = []) => lineupDuplicateNote(positions, memberIds, label, KEYS, changed);

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
