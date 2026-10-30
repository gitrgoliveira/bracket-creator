import React from 'react';
import { render } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import { TvDisplay } from '../../display.jsx';
import { findUpcomingOnCourt } from '../../display_helpers.jsx';

describe('Shared court next up: court-queue ordering across competitions', () => {
  const compA = {
    id: 'a',
    name: 'Competition A',
    status: 'running',
    date: '2026-11-01',
    startTime: '09:00',
    poolMatches: [],
    bracket: {
      rounds: [
        [
          {
            id: 'm-a1',
            court: 'A',
            status: 'scheduled',
            scheduledAt: '09:20',
            queuePosition: 1, // Stale local queuePosition
            sideA: { id: 'p-a1', name: 'Alice Red' },
            sideB: { id: 'p-a2', name: 'Alice White' },
          },
        ],
      ],
    },
  };

  const compB = {
    id: 'b',
    name: 'Competition B',
    status: 'running',
    date: '2026-11-01',
    startTime: '09:00',
    poolMatches: [],
    bracket: {
      rounds: [
        [
          {
            id: 'm-b1',
            court: 'A',
            status: 'scheduled',
            scheduledAt: '09:10',
            // A stale stored position that says a's match goes first: the
            // board must follow the court queue (printed time), not it.
            queuePosition: 2,
            sideA: { id: 'p-b1', name: 'Bob Red' },
            sideB: { id: 'p-b2', name: 'Bob White' },
          },
        ],
      ],
    },
  };

  it('findUpcomingOnCourt orders b match then a match by court queue', () => {
    const upcoming = findUpcomingOnCourt([compA, compB], 'A', 2);
    expect(upcoming).toHaveLength(2);
    expect(upcoming[0].id).toBe('m-b1');
    expect(upcoming[0]._comp.id).toBe('b');
    expect(upcoming[1].id).toBe('m-a1');
    expect(upcoming[1]._comp.id).toBe('a');
  });

  it('the TV board leads with b match (printed earlier) and names a match as NEXT', () => {
    const { container } = render(
      <TvDisplay
        court="A"
        tournament={{ name: 'Tournament' }}
        competitions={[compA, compB]}
        connected
      />
    );
    const text = container.textContent;

    // With no bout running the board promotes the court's first waiting match:
    // b's, printed at 09:10, although both carry a stale queuePosition of 1.
    expect(text).toContain('Competition B');
    expect(text).not.toContain('Competition A');
    const next = text.indexOf('NEXT');
    expect(next).toBeGreaterThan(-1);
    expect(text.indexOf('Bob Red')).toBeGreaterThan(-1);
    expect(text.indexOf('Bob Red')).toBeLessThan(next);
    // One next line on the court, and it names a's match.
    expect(text.indexOf('NEXT', next + 1)).toBe(-1);
    expect(text.indexOf('Alice Red')).toBeGreaterThan(next);
  });
});
