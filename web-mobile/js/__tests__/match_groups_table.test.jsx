// JS half of the shared Go/JS table of match write groups (bc-mrgc). Go half:
// TestMatchGroups_SharedTable (internal/state/match_groups_table_test.go).
// A client names the groups its write changes by these exact strings, and the
// server refuses a name it does not know, so the two lists must never drift.
//
// Also pinned here, below the table: the comparison that decides which groups
// a write changed (changedGroups), the union two coalesced writes carry
// (unionChanged), the operator words for a group (groupLabel), and the
// serializer carrying `changed` onto the wire (toBackendMatchResult).
import { describe, it, expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import {
  SCALAR_GROUPS, boutGroup, parseBoutGroup, isValidGroup,
  changedGroups, unionChanged, groupLabel, groupsLabel,
} from '../match_groups.jsx';
import { toBackendMatchResult, matchWire } from '../api_serializers.jsx';

const table = JSON.parse(readFileSync(
  resolve(__dirname, '..', '..', '..', 'internal', 'state', 'testdata', 'match_groups.json'), 'utf8'));

describe('the shared group table', () => {
  it('is not empty', () => {
    expect(table.scalarGroups.length).toBeGreaterThan(0);
    expect(table.bouts.length).toBeGreaterThan(0);
    expect(table.invalid.length).toBeGreaterThan(0);
  });

  it('names the scalar groups in the server\'s order', () => {
    expect(SCALAR_GROUPS).toEqual(table.scalarGroups);
    for (const g of table.scalarGroups) expect(isValidGroup(g)).toBe(true);
  });

  it.each(table.bouts)('bout $position is "$group"', ({ position, group }) => {
    expect(boutGroup(position)).toBe(group);
    expect(parseBoutGroup(group)).toBe(position);
    expect(isValidGroup(group)).toBe(true);
  });

  it.each(table.invalid)('"%s" is not a group', (g) => {
    expect(isValidGroup(g)).toBe(false);
  });
});

describe('changedGroups', () => {
  const base = { status: 'running', winner: '', decision: '', ipponsA: ['M'], ipponsB: [], hansokuA: 0, hansokuB: 0 };

  it('names nothing for a write equal to its baseline', () => {
    expect(changedGroups({ ...base }, base)).toEqual([]);
  });

  it('names each group whose value moved, in the server\'s order', () => {
    const next = { ...base, ipponsB: ['K'], encho: { periodCount: 1 }, status: 'completed' };
    expect(changedGroups(next, base)).toEqual(['points', 'result', 'encho']);
  });

  it('a group differing from ANY baseline counts', () => {
    const last = { ...base, ipponsA: ['M', 'K'] };
    expect(changedGroups({ ...base }, base, last)).toEqual(['points']);
  });

  it('a group the write does not state is never named', () => {
    // A start sends no scoreline and no flags.
    const next = { status: 'running', winner: '', decision: '' };
    expect(changedGroups(next, { ...base, flagsA: 2 })).toEqual([]);
  });

  it('an omitted correction reason is not a change, a stated one is', () => {
    const stored = { ...base, status: 'completed', correctionReason: 'Scoring error' };
    expect(changedGroups({ ...base, status: 'completed' }, stored)).toEqual([]);
    expect(changedGroups({ ...base, status: 'completed', correctionReason: 'Wrong side' }, stored)).toEqual(['result']);
  });

  it('clearWithdrawal always names the result', () => {
    expect(changedGroups({ ...base, clearWithdrawal: true }, base)).toEqual(['result']);
  });

  it('a bout row the baseline does not hold reads as an empty one', () => {
    const empty = { position: 3, sideA: '', sideB: '', ipponsA: [], ipponsB: [], hansokuA: 0, hansokuB: 0, winner: '', decision: '' };
    const scored = { ...empty, position: 2, ipponsB: ['K'], winner: 'Osaka' };
    expect(changedGroups({ ...base, subResults: [scored, empty] }, base)).toEqual(['bout:2']);
  });

  it('a bout key the write leaves out is not compared', () => {
    // The representative bout's ippons are left out by a writer that has not
    // touched it (team editor daihyosenSilent).
    const stored = { ...base, subResults: [{ position: -1, sideA: 'A', sideB: 'B', ipponsA: ['Ht'], ipponsB: [], winner: 'A', decision: 'daihyosen' }] };
    const silent = { ...base, subResults: [{ position: -1, sideA: 'A', sideB: 'B', winner: 'A', decision: 'daihyosen' }] };
    expect(changedGroups(silent, stored)).toEqual([]);
  });

  it('a baseline may be a function of the group', () => {
    const next = { ...base, ipponsB: ['K'], encho: { periodCount: 1 } };
    const perGroup = (g) => (g === 'encho' ? { encho: { periodCount: 1 } } : base);
    expect(changedGroups(next, perGroup)).toEqual(['points']);
  });
});

describe('unionChanged', () => {
  it('is every group either write changed', () => {
    expect(unionChanged(['points'], ['encho', 'points', 'bout:2'])).toEqual(['points', 'encho', 'bout:2']);
  });

  it('never narrows "every group" (no list) to a partial one', () => {
    expect(unionChanged(undefined, ['points'])).toBeUndefined();
    expect(unionChanged(['points'], undefined)).toBeUndefined();
  });
});

describe('groupLabel', () => {
  it.each([
    ['points', 'points'],
    ['result', 'the result'],
    ['encho', 'overtime'],
    ['flags', 'flags'],
    ['rep', 'the representative players'],
    ['bout:2', 'bout 2'],
    ['bout:-1', 'the representative bout'],
  ])('%s reads "%s"', (g, words) => {
    expect(groupLabel(g)).toBe(words);
  });

  it('joins several for a sentence', () => {
    expect(groupsLabel(['points', 'bout:2', 'result'])).toBe('points, bout 2 and the result');
  });
});

describe('the serializer', () => {
  const match = { id: 'm1', sideA: { id: 'p1', name: 'Yamada' }, sideB: { id: 'p2', name: 'Tanaka' }, sideAId: 'p1', sideBId: 'p2' };

  it('carries `changed` onto the wire, as a copy', () => {
    const patch = { status: 'running', ipponsA: ['M'], ipponsB: [], changed: ['points'] };
    const wire = toBackendMatchResult(patch, match);
    expect(wire.changed).toEqual(['points']);
    wire.changed.push('result');
    expect(patch.changed).toEqual(['points']);
  });

  it('sends no `changed` for a patch that names none', () => {
    expect('changed' in toBackendMatchResult({ status: 'running' }, match)).toBe(false);
  });

  it('a start carries its `changed` without a scoreline', () => {
    const wire = toBackendMatchResult({ startOnly: true, status: 'running', ipponsA: [], changed: ['result'] }, match);
    expect(wire.changed).toEqual(['result']);
    expect('ipponsA' in wire).toBe(false);
  });

  it('a match the client holds serialises to the shape a write would send', () => {
    const held = { ...match, status: 'completed', winner: { id: 'p1', name: 'Yamada' }, winnerId: 'p1', ipponsA: ['M', '•'], ipponsB: [] };
    const write = toBackendMatchResult({ status: 'completed', winner: match.sideA, ipponsA: ['M'], ipponsB: [] }, match);
    expect(changedGroups(write, matchWire(held))).toEqual([]);
  });
});
