// Pure-logic tests for buildPlayerMatchHighlight (FR-020, FR-022).
// Slice 4 / T108–T109: drives "Find my matches" filtering on the viewer home.
import { describe, it, expect } from 'vitest';
import { buildPlayerMatchHighlight, mymatchQueueLabel } from '../viewer.jsx';

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

// FR-025 — MyMatchPanel Queue chip label. Sibling helpers in display.jsx
// (queueLabel) and lower in viewer.jsx (VSchedItem inline) must agree on
// wording so a viewer who looks at "Your next match" then scrolls down to
// the per-court schedule sees the same label.
describe('mymatchQueueLabel', () => {
  it('returns "Up next" when scheduled and queuePosition === 1', () => {
    expect(mymatchQueueLabel({ status: 'scheduled', queuePosition: 1 })).toBe('Up next');
  });

  it('returns "N before yours" for queuePosition > 1 (1-indexed → N-1 ahead)', () => {
    expect(mymatchQueueLabel({ status: 'scheduled', queuePosition: 2 })).toBe('1 before yours');
    expect(mymatchQueueLabel({ status: 'scheduled', queuePosition: 5 })).toBe('4 before yours');
    expect(mymatchQueueLabel({ status: 'scheduled', queuePosition: 99 })).toBe('98 before yours');
  });

  it('returns "LIVE NOW" when status === "running" regardless of queuePosition', () => {
    expect(mymatchQueueLabel({ status: 'running', queuePosition: 0 })).toBe('LIVE NOW');
    expect(mymatchQueueLabel({ status: 'running' })).toBe('LIVE NOW');
    // qp is meant to be 0 for running per FR-025, but a stale 3 must not bleed through.
    expect(mymatchQueueLabel({ status: 'running', queuePosition: 3 })).toBe('LIVE NOW');
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

  it('rejects non-numeric queuePosition values', () => {
    // Older payloads or buggy fixtures may send strings; we should not render
    // "1 before yours" if qp is the string "2".
    expect(mymatchQueueLabel({ status: 'scheduled', queuePosition: '2' })).toBeNull();
  });

  it('handles null / undefined match gracefully', () => {
    expect(mymatchQueueLabel(null)).toBeNull();
    expect(mymatchQueueLabel(undefined)).toBeNull();
  });
});
