// Pure-logic tests for buildPlayerMatchHighlight (FR-020, FR-022).
// Slice 4 / T108–T109: drives "Find my matches" filtering on the viewer home.
import { describe, it, expect } from 'vitest';
import { buildPlayerMatchHighlight, isFollowedPlayer, mymatchQueueLabel } from '../viewer.jsx';

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

  it('falls back to case-insensitive exact name match when UUID misses', () => {
    const matches = [{ id: 'm1', sideA: 'John Doe', sideB: 'Jane Smith' }];
    const result = buildPlayerMatchHighlight('unknown-uuid', matches, 'John Doe');
    expect(result).toHaveLength(1);
    expect(result[0].id).toBe('m1');
  });

  it('name fallback rejects substring-only matches (mp-egl)', () => {
    const matches = [{ id: 'm1', sideA: 'Banana', sideB: 'Charlie' }];
    expect(buildPlayerMatchHighlight('no-id', matches, 'Ana')).toEqual([]);
  });

  it('name fallback trims whitespace', () => {
    const matches = [{ id: 'm1', sideA: '  Alice  ', sideB: 'Bob' }];
    const result = buildPlayerMatchHighlight('no-id', matches, 'Alice');
    expect(result).toHaveLength(1);
  });

  it('does not fall back to name match when UUID hits at least once', () => {
    const matches = [
      { id: 'm1', sideA: { id: 'p1', name: 'John Doe' }, sideB: { id: 'p2', name: 'Jane Smith' } },
      { id: 'm2', sideA: { id: 'p3', name: 'Johnny Apple' }, sideB: { id: 'p4', name: 'Kim Lee' } },
    ];
    const result = buildPlayerMatchHighlight('p1', matches, 'John Doe');
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

  it('name fallback is case-insensitive and trims whitespace', () => {
    // Older payloads or manual entries may differ in capitalisation or
    // have leading/trailing spaces — treat them as the same player.
    expect(isFollowedPlayer({ id: '', name: 'ALICE' }, { id: '', name: 'alice' })).toBe(true);
    expect(isFollowedPlayer({ id: '', name: '  Alice  ' }, { id: '', name: 'Alice' })).toBe(true);
    expect(isFollowedPlayer('alice', { id: '', name: 'ALICE' })).toBe(true);
  });
});

// FR-025 — MyMatchPanel Queue chip label. Wording mirrors VSchedItem (also in
// viewer.jsx) so a viewer who looks at "Your next match" then scrolls down to
// the per-court schedule sees the same label.
describe('mymatchQueueLabel', () => {
  it('returns "Next up" when scheduled and queuePosition === 1', () => {
    expect(mymatchQueueLabel({ status: 'scheduled', queuePosition: 1 })).toBe('Next up');
  });

  it('returns "N before yours" for queuePosition > 1 (1-indexed → N-1 ahead)', () => {
    expect(mymatchQueueLabel({ status: 'scheduled', queuePosition: 2 })).toBe('1 before yours');
    expect(mymatchQueueLabel({ status: 'scheduled', queuePosition: 5 })).toBe('4 before yours');
    expect(mymatchQueueLabel({ status: 'scheduled', queuePosition: 99 })).toBe('98 before yours');
  });

  it('returns null when status === "running" (round label already shows LIVE NOW)', () => {
    // The my-match__round element appends " · LIVE NOW" for running matches, so
    // the Queue chip must not duplicate it.
    expect(mymatchQueueLabel({ status: 'running', queuePosition: 0 })).toBeNull();
    expect(mymatchQueueLabel({ status: 'running' })).toBeNull();
    expect(mymatchQueueLabel({ status: 'running', queuePosition: 3 })).toBeNull();
  });

  it('returns null for non-running / non-scheduled statuses', () => {
    for (const status of ['completed', 'forfeit', 'cancelled', '', undefined]) {
      expect(mymatchQueueLabel({ status, queuePosition: 1 })).toBeNull();
    }
  });

  it('returns null when scheduled but queuePosition is missing / 0 / negative', () => {
    expect(mymatchQueueLabel({ status: 'scheduled' })).toBeNull();
    expect(mymatchQueueLabel({ status: 'scheduled', queuePosition: 0 })).toBeNull();
    expect(mymatchQueueLabel({ status: 'scheduled', queuePosition: null })).toBeNull();
    expect(mymatchQueueLabel({ status: 'scheduled', queuePosition: -1 })).toBeNull();
  });

  it('accepts numeric strings and rejects non-numeric queuePosition values', () => {
    expect(mymatchQueueLabel({ status: 'scheduled', queuePosition: '1' })).toBe('Next up');
    expect(mymatchQueueLabel({ status: 'scheduled', queuePosition: '2' })).toBe('1 before yours');
    expect(mymatchQueueLabel({ status: 'scheduled', queuePosition: 'abc' })).toBeNull();
  });

  it('handles null / undefined match gracefully', () => {
    expect(mymatchQueueLabel(null)).toBeNull();
    expect(mymatchQueueLabel(undefined)).toBeNull();
  });
});
