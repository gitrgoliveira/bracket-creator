import { describe, it, expect } from 'vitest';
import { queueLabel, queueLabelCompact } from '../display.jsx';

// Bead mp-e3k: consolidate queue-label wording across viewer surfaces.
// These tests pin the canonical wording for the per-court queue position
// so that VSchedItem (viewer.jsx), the TV display (display.jsx), and the
// TWMatch pill (viewer.jsx) cannot drift apart again.
//
// Contract:
//   - queueLabel(m)        — full-form label used in scheduled-list rows
//     - status !== "scheduled" → "" (gate applied first, matches queueLabelCompact)
//     - qp === 1 → "Next up"  (qp coerced with Number(), handles string values)
//     - qp >  1 → "(qp - 1) before yours"
//     - status === "scheduled" + falsy qp + scheduledAt → "Scheduled hh:mm" fallback
//     - any other combination → ""
//   - queueLabelCompact(m) — pill form used in dense rows
//     - status !== "scheduled" → null (so callers can hide the pill)
//     - qp === 1 → "Next up"
//     - qp >  1 → "#N"
//     - falsy/non-positive qp → null

describe('queueLabel (full form)', () => {
  it('returns "Next up" for queue position 1', () => {
    expect(queueLabel({ status: 'scheduled', queuePosition: 1 })).toBe('Next up');
  });

  it('returns "(N-1) before yours" for queue position N > 1', () => {
    expect(queueLabel({ status: 'scheduled', queuePosition: 2 })).toBe('1 before yours');
  });

  it('handles a deep queue position', () => {
    expect(queueLabel({ status: 'scheduled', queuePosition: 99 })).toBe('98 before yours');
  });

  it('falls back to scheduledAt when no queue position is present', () => {
    expect(queueLabel({ status: 'scheduled', scheduledAt: '10:30' })).toBe('Scheduled 10:30');
  });

  it('returns "" for an empty match object', () => {
    expect(queueLabel({})).toBe('');
  });

  it('returns "" for null input (defensive)', () => {
    expect(queueLabel(null)).toBe('');
  });

  it('returns "" for queuePosition 0 (non-positive treated as no queue)', () => {
    expect(queueLabel({ status: 'scheduled', queuePosition: 0 })).toBe('');
  });

  it('treats a negative queuePosition as "no queue"', () => {
    expect(queueLabel({ status: 'scheduled', queuePosition: -1 })).toBe('');
  });

  it('handles a string queuePosition (e.g. from JSON deserialization)', () => {
    expect(queueLabel({ status: 'scheduled', queuePosition: '1' })).toBe('Next up');
    expect(queueLabel({ status: 'scheduled', queuePosition: '2' })).toBe('1 before yours');
  });

  it('does not show scheduledAt for running matches even if scheduledAt is present', () => {
    expect(queueLabel({ status: 'running', scheduledAt: '10:30' })).toBe('');
  });

  it('does not show scheduledAt for completed matches even if scheduledAt is present', () => {
    expect(queueLabel({ status: 'completed', scheduledAt: '10:30' })).toBe('');
  });

  it('returns "" for a running match with queuePosition (status gate applied first)', () => {
    expect(queueLabel({ status: 'running', queuePosition: 1 })).toBe('');
    expect(queueLabel({ status: 'running', queuePosition: 2 })).toBe('');
  });

  it('returns "" for a completed match with queuePosition', () => {
    expect(queueLabel({ status: 'completed', queuePosition: 1 })).toBe('');
  });

  // Regression test for Copilot PR #133 comment 3288473578: the docstring
  // promises the status gate takes precedence over the queuePosition branch.
  // Pin every non-scheduled status × qp>0 combination so the contract
  // cannot regress to leaking "Next up" / "N before yours" on running,
  // completed, or cancelled rows.
  it('docstring contract: status gate beats queuePosition on every non-scheduled status', () => {
    const nonScheduledStatuses = ['running', 'completed', 'cancelled', 'pending', undefined, ''];
    const positiveQueuePositions = [1, 2, 99, '1', '2'];
    for (const status of nonScheduledStatuses) {
      for (const queuePosition of positiveQueuePositions) {
        expect(queueLabel({ status, queuePosition })).toBe('');
        // Even with a scheduledAt fallback set, the status gate wins.
        expect(queueLabel({ status, queuePosition, scheduledAt: '10:30' })).toBe('');
      }
    }
  });
});

describe('queueLabelCompact (pill form)', () => {
  it('returns "Next up" for queue position 1 (canonical wording)', () => {
    // Pinning this prevents drift back to the older "Next" wording
    // that used to live in TWMatch.
    expect(queueLabelCompact({ status: 'scheduled', queuePosition: 1 })).toBe('Next up');
  });

  it('returns "#N" for queue position N > 1', () => {
    expect(queueLabelCompact({ status: 'scheduled', queuePosition: 2 })).toBe('#2');
    expect(queueLabelCompact({ status: 'scheduled', queuePosition: 99 })).toBe('#99');
  });

  it('returns null for running matches (no pill on live row)', () => {
    expect(queueLabelCompact({ status: 'running', queuePosition: 1 })).toBeNull();
  });

  it('returns null for completed matches', () => {
    expect(queueLabelCompact({ status: 'completed', queuePosition: 1 })).toBeNull();
  });

  it('returns null when queuePosition is missing / non-positive', () => {
    expect(queueLabelCompact({ status: 'scheduled' })).toBeNull();
    expect(queueLabelCompact({ status: 'scheduled', queuePosition: 0 })).toBeNull();
    expect(queueLabelCompact({ status: 'scheduled', queuePosition: -3 })).toBeNull();
  });

  it('handles string queuePosition (coerces to number)', () => {
    expect(queueLabelCompact({ status: 'scheduled', queuePosition: '1' })).toBe('Next up');
    expect(queueLabelCompact({ status: 'scheduled', queuePosition: '2' })).toBe('#2');
  });

  it('returns null for non-numeric string queuePosition', () => {
    expect(queueLabelCompact({ status: 'scheduled', queuePosition: 'foo' })).toBeNull();
  });

  it('returns null for null input (defensive)', () => {
    expect(queueLabelCompact(null)).toBeNull();
  });
});
