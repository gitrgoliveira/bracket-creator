// position_labels.test.jsx: the one owner of which team sizes name their
// positions (positionLabelsFor in lineup_resolver.jsx). Operator ruling
// 2026-11-07: a 3-person team reads Senpo, Chuken, Taisho over its stored keys
// "1".."3", a 5-person team keeps its FIK names, every other size its numbers.
// Go twin: TestPositionLabel_NamesThreeAndFiveMemberTeams (internal/domain).
import { describe, it, expect } from 'vitest';
import { positionLabelsFor, boutPositionName, lineupPositionLabel } from '../lineup_resolver.jsx';

describe('positionLabelsFor', () => {
  it('names a 3-person team Senpo, Chuken, Taisho', () => {
    expect(positionLabelsFor(3)).toEqual(['Senpo', 'Chuken', 'Taisho']);
  });
  it('keeps the five FIK names for a 5-person team', () => {
    expect(positionLabelsFor(5)).toEqual(['Senpo', 'Jiho', 'Chuken', 'Fukusho', 'Taisho']);
  });
  it('names no other size', () => {
    for (const size of [0, 1, 2, 4, 7, 11, undefined]) expect(positionLabelsFor(size)).toBeNull();
  });
});

describe('boutPositionName', () => {
  it('names each bout of a 3-person team by its 0-based index', () => {
    expect([0, 1, 2].map((i) => boutPositionName(3, i))).toEqual(['Senpo', 'Chuken', 'Taisho']);
    expect(boutPositionName(3, 3)).toBeNull();
    expect(boutPositionName(3, -1)).toBeNull();
  });
  it('is null for a numbered size', () => {
    expect(boutPositionName(4, 0)).toBeNull();
    expect(boutPositionName(7, 2)).toBeNull();
  });
});

describe('lineupPositionLabel', () => {
  it('names a 3-person team\'s numbered keys', () => {
    expect(lineupPositionLabel('1', 3)).toBe('Senpo');
    expect(lineupPositionLabel('2', 3)).toBe('Chuken');
    expect(lineupPositionLabel('3', 3)).toBe('Taisho');
    expect(lineupPositionLabel('4', 3)).toBe('Position 4');
  });
  it('reads a 5-person team as before', () => {
    expect(lineupPositionLabel('senpo', 5)).toBe('Senpo');
    expect(lineupPositionLabel('fukusho', 5)).toBe('Fukusho');
    expect(lineupPositionLabel('2', 5)).toBe('Position 2');
  });
  it('keeps "Position N" for every other size, and with no size', () => {
    expect(lineupPositionLabel('1', 4)).toBe('Position 1');
    expect(lineupPositionLabel('3', 7)).toBe('Position 3');
    expect(lineupPositionLabel('2')).toBe('Position 2');
    expect(lineupPositionLabel('jiho')).toBe('Jiho');
  });
});
