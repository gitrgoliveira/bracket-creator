// Pure-logic tests for buildPlayerMatchHighlight (FR-020, FR-022).
// Slice 4 / T108–T109: drives "Find my matches" filtering on the viewer home.
import { describe, it, expect } from 'vitest';
import { buildPlayerMatchHighlight, isFollowedPlayer } from '../viewer.jsx';

describe('buildPlayerMatchHighlight', () => {
  it('returns matches where playerId is on SideA', () => {
    const matches = [
      { id: 'm1', sideAId: 'p1', sideBId: 'p2' },
      { id: 'm2', sideAId: 'p3', sideBId: 'p4' },
    ];
    const result = buildPlayerMatchHighlight('p1', matches);
    expect(result).toHaveLength(1);
    expect(result[0].id).toBe('m1');
  });

  it('returns matches where playerId is on SideB', () => {
    const matches = [
      { id: 'm1', sideAId: 'p1', sideBId: 'p2' },
      { id: 'm2', sideAId: 'p3', sideBId: 'p4' },
    ];
    const result = buildPlayerMatchHighlight('p4', matches);
    expect(result).toHaveLength(1);
    expect(result[0].id).toBe('m2');
  });

  it('also matches the canonical sideA.id / sideB.id object shape', () => {
    // The real API serialiser emits sideA/sideB as { id, name, dojo }.
    // Verifying both shapes work means the helper is safe to use both in
    // tests (which carry the flat shape) and against live tournament data.
    const matches = [
      { id: 'm1', sideA: { id: 'p1', name: 'Alice' }, sideB: { id: 'p2', name: 'Bob' } },
      { id: 'm2', sideA: { id: 'p3', name: 'Charlie' }, sideB: { id: 'p4', name: 'Dan' } },
    ];
    const result = buildPlayerMatchHighlight('p3', matches);
    expect(result).toHaveLength(1);
    expect(result[0].id).toBe('m2');
  });

  it('falls back to case-insensitive name match when UUID misses', () => {
    const matches = [{ id: 'm1', sideA: 'John Doe', sideB: 'Jane Smith' }];
    const result = buildPlayerMatchHighlight('unknown-uuid', matches, 'JOHN');
    expect(result).toHaveLength(1);
    expect(result[0].id).toBe('m1');
  });

  it('does not fall back to name match when UUID hits at least once', () => {
    // FR-020: a UUID hit means we have a definitive answer — do not widen
    // the result set with name substrings that could pull in unrelated
    // people sharing a common surname.
    const matches = [
      { id: 'm1', sideA: { id: 'p1', name: 'John Doe' }, sideB: { id: 'p2', name: 'Jane Smith' } },
      { id: 'm2', sideA: { id: 'p3', name: 'Johnny Apple' }, sideB: { id: 'p4', name: 'Kim Lee' } },
    ];
    const result = buildPlayerMatchHighlight('p1', matches, 'john');
    expect(result).toHaveLength(1);
    expect(result[0].id).toBe('m1');
  });

  it('returns an empty array when neither id nor fallback hits', () => {
    const matches = [{ id: 'm1', sideAId: 'p1', sideBId: 'p2' }];
    expect(buildPlayerMatchHighlight('nope', matches)).toEqual([]);
    expect(buildPlayerMatchHighlight('nope', matches, 'zzz')).toEqual([]);
  });

  it('handles empty / non-array inputs gracefully', () => {
    expect(buildPlayerMatchHighlight('p1', null)).toEqual([]);
    expect(buildPlayerMatchHighlight('', [{ id: 'm1', sideAId: 'p1' }])).toEqual([]);
  });
});

describe('isFollowedPlayer', () => {
  it('matches by UUID first, then falls back to name when ids diverge', () => {
    // Both sides have ids — same id is a match.
    const sideA = { id: 'p1', name: 'Alice' };
    expect(isFollowedPlayer(sideA, { id: 'p1', name: 'Alice' })).toBe(true);
    // Both sides have ids and they differ — the id check fails but the
    // name fallback still matches. Documents the two-layer match contract.
    expect(isFollowedPlayer(sideA, { id: 'p2', name: 'Alice' })).toBe(true);
  });

  it('falls back to name match when UUID is missing on either side', () => {
    // Team-match sub-players (or legacy fixtures) may key by display name.
    expect(isFollowedPlayer({ id: '', name: 'Alice' }, { id: 'p1', name: 'Alice' })).toBe(true);
    expect(isFollowedPlayer({ name: 'Alice' }, { id: '', name: 'Alice' })).toBe(true);
    // String side shape (legacy `sideA: 'Alice'`).
    expect(isFollowedPlayer('Alice', { id: '', name: 'Alice' })).toBe(true);
  });

  it('rejects different ids and different names', () => {
    expect(isFollowedPlayer({ id: 'p1', name: 'Alice' }, { id: 'p2', name: 'Bob' })).toBe(false);
  });

  it('handles null / undefined gracefully', () => {
    expect(isFollowedPlayer(null, { id: 'p1', name: 'Alice' })).toBe(false);
    expect(isFollowedPlayer({ id: 'p1' }, null)).toBe(false);
  });

  it('does not match when only blank ids align (avoids the "" === "" trap)', () => {
    // FR-020 regression guard: two participants both lacking UUIDs must not
    // be treated as the same person just because their ids are empty.
    expect(isFollowedPlayer({ id: '', name: 'Alice' }, { id: '', name: 'Bob' })).toBe(false);
  });
});
