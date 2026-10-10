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
    ['repPickA', 'Aka\'s pick for the representative bout'],
    ['repPickB', 'Shiro\'s pick for the representative bout'],
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

// The two representatives of the representative bout (the member ids on the -1 row)
// are a change of their own for each side, `repPickA` (Aka) and `repPickB` (Shiro), each
// dated apart from the other side's and from the bout row they sit on.
// A pick never alters the score and a point never alters the pick, so a write names
// the one it changed and not the other. Numbered rows keep the rule they always had.
describe('the representatives of the representative bout (repPickA, repPickB)', () => {
  const bout = (position, extra = {}) => ({
    position, sideA: 'Team A', sideB: 'Team B', ipponsA: [], ipponsB: [], winner: '', decision: 'daihyosen', ...extra,
  });

  it('a side B pick cleared (key omitted) names repPickB and not bout:-1 or repPickA', () => {
    const stored = { subResults: [bout(-1, { sideBMemberId: 'm1b' })] };
    const next = { subResults: [bout(-1)] };
    expect(changedGroups(next, stored)).toEqual(['repPickB']);
  });

  it('a side B pick added names repPickB and not bout:-1 or repPickA', () => {
    const stored = { subResults: [bout(-1)] };
    const next = { subResults: [bout(-1, { sideBMemberId: 'm1b' })] };
    expect(changedGroups(next, stored)).toEqual(['repPickB']);
  });

  it('a side A pick names repPickA alone: a side B pick the write leaves out is not a change', () => {
    // The two-captain race: this device never saw side B's pick, so its write carries side A only.
    const stored = { subResults: [bout(-1)] };
    const next = { subResults: [bout(-1, { sideAMemberId: 'm1a' })] };
    expect(changedGroups(next, stored)).toEqual(['repPickA']);
  });

  it('a side A pick leaves a side B pick the editor kept unnamed', () => {
    const stored = { subResults: [bout(-1, { sideBMemberId: 'm1b' })] };
    const next = { subResults: [bout(-1, { sideAMemberId: 'm1a', sideBMemberId: 'm1b' })] };
    expect(changedGroups(next, stored)).toEqual(['repPickA']);
  });

  it('a side B pick cleared leaves a side A pick the editor kept unnamed', () => {
    const stored = { subResults: [bout(-1, { sideAMemberId: 'm1a', sideBMemberId: 'm1b' })] };
    const next = { subResults: [bout(-1, { sideAMemberId: 'm1a' })] };
    expect(changedGroups(next, stored)).toEqual(['repPickB']);
  });

  it('a point struck on the representative bout names bout:-1 and neither pick, whatever ids the row carries', () => {
    // A board that never saw the pick holds the row without it, and sends it so.
    expect(changedGroups({ subResults: [bout(-1, { ipponsA: ['M'] })] }, { subResults: [bout(-1)] })).toEqual(['bout:-1']);
    // A board that did see it holds it and sends it back.
    const seen = { subResults: [bout(-1, { sideAMemberId: 'm1a' })] };
    expect(changedGroups({ subResults: [bout(-1, { ipponsA: ['M'], sideAMemberId: 'm1a' })] }, seen)).toEqual(['bout:-1']);
  });

  it('a side B pick and a point together name both', () => {
    const stored = { subResults: [bout(-1)] };
    const next = { subResults: [bout(-1, { ipponsA: ['M'], sideBMemberId: 'm1b' })] };
    expect(changedGroups(next, stored)).toEqual(['repPickB', 'bout:-1']);
  });

  it('a decided representative bout whose side A pick is swapped names repPickA and not bout:-1 or repPickB', () => {
    // The server re-derives the winner's member id from the stored picks, so a swap that
    // moves it moves no bout: the score and the winner stand.
    const stored = { subResults: [bout(-1, { ipponsA: ['M'], winner: 'Team A', winnerMemberId: 'm1a', sideAMemberId: 'm1a', sideBMemberId: 'm1b' })] };
    const next = { subResults: [bout(-1, { ipponsA: ['M'], winner: 'Team A', winnerMemberId: 'm2a', sideAMemberId: 'm2a', sideBMemberId: 'm1b' })] };
    expect(changedGroups(next, stored)).toEqual(['repPickA']);
  });

  it('a decided representative bout whose winning side changes names bout:-1, and neither pick when the picks stand', () => {
    const stored = { subResults: [bout(-1, { winner: 'Team A', winnerMemberId: 'm1a', sideAMemberId: 'm1a', sideBMemberId: 'm1b' })] };
    const next = { subResults: [bout(-1, { winner: 'Team B', winnerMemberId: 'm1b', sideAMemberId: 'm1a', sideBMemberId: 'm1b' })] };
    expect(changedGroups(next, stored)).toEqual(['bout:-1']);
  });

  it('a write with no representative row names no picks', () => {
    expect(changedGroups({ subResults: [bout(1, { decision: '' })] }, { subResults: [bout(1, { decision: '' })] })).toEqual([]);
  });

  it('a numbered row whose id is left out is not a change, as before', () => {
    const stored = { subResults: [bout(1, { sideBMemberId: 'm1b', decision: '' })] };
    const next = { subResults: [bout(1, { decision: '' })] };
    expect(changedGroups(next, stored)).toEqual([]);
  });

  // The server stores NO side names on the representative row (engine.AddDaihyosen builds
  // {Position: -1, Decision: "daihyosen"}), while the editor restates the match's team names
  // on it so the hantei mark can be placed. The names are the match's sides, never a change of
  // the bout, so they must not name it.
  it('the team names the editor states on the representative row, which the server stores empty, are not a change of the bout', () => {
    const stored = { subResults: [bout(-1, { sideA: '', sideB: '' })] };
    expect(changedGroups({ subResults: [bout(-1)] }, stored)).toEqual([]);
  });

  it('a point struck on a representative row the server stores without side names still names bout:-1', () => {
    const stored = { subResults: [bout(-1, { sideA: '', sideB: '' })] };
    expect(changedGroups({ subResults: [bout(-1, { ipponsA: ['M'] })] }, stored)).toEqual(['bout:-1']);
  });
});
