import { describe, it, expect } from 'vitest';
import { changedLineupPositions } from '../lineup_resolver.jsx';

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
