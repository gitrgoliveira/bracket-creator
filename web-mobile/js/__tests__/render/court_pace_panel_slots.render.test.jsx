// The organiser's Court pace panel reads each match's slot from its
// competition (slotMinutes), as the public screens do, so both show the same
// drift; the page's "Time per match" is only the fallback.
import React from 'react';
import { render, cleanup, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { CourtPacePanel } from '../../admin_schedule_pacing.jsx';

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(new Date(2026, 9, 29, 9, 15, 0));
  globalThis.React = React;
  window.React = React;
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

const finishedAt = new Date(2026, 9, 29, 9, 20, 0).getTime();
const poolMatches = [
  { id: 'm1', court: 'A', status: 'completed', scheduledAt: '09:00', modifiedAt: finishedAt },
  { id: 'm2', court: 'A', status: 'scheduled', scheduledAt: '09:10' },
];
// The panel's own counts come from byCourt; drift and the next bout from the
// competitions, as on the public screens.
const byCourt = {
  A: poolMatches.map((m) => ({ ...m, compId: 'c1' })),
};

function openPanel(props) {
  const view = render(<CourtPacePanel byCourt={byCourt} safeMatchDuration={3} {...props} />);
  fireEvent.click(view.getByText('Court pace'));
  return view;
}

describe('CourtPacePanel slots', () => {
  it('measures drift with the competition slot, not the page time per match', () => {
    const view = openPanel({ competitions: [{ id: 'c1', slotMinutes: 10, poolMatches }] });
    expect(view.container.textContent).toContain('10m behind');
  });

  it('falls back to the page time per match for a competition with no slot', () => {
    const view = openPanel({ competitions: [{ id: 'c1', poolMatches }] });
    expect(view.container.textContent).toContain('17m behind');
  });

  it('a knockout bout is projected with the knockout slot, as the public screens do', () => {
    const view = openPanel({
      competitions: [{
        id: 'c1', slotMinutes: 4, knockoutSlotMinutes: 10, poolMatches: [],
        bracket: { rounds: [[poolMatches[0], { ...poolMatches[1], id: 'k2' }]] },
      }],
    });
    expect(view.container.textContent).toContain('10m behind');
  });

  it('reads today only: another day\'s waiting bout on the court does not set today\'s drift', () => {
    const today = { id: 'c1', date: '29-10-2026', slotMinutes: 10, poolMatches: [{ id: 't1', court: 'A', status: 'scheduled', scheduledAt: '09:10' }] };
    const tomorrow = { id: 'c2', date: '30-10-2026', slotMinutes: 10, poolMatches: [{ id: 'n1', court: 'A', status: 'scheduled', scheduledAt: '08:00' }] };
    const view = render(
      <CourtPacePanel
        byCourt={{ A: [{ ...tomorrow.poolMatches[0], compId: 'c2' }, { ...today.poolMatches[0], compId: 'c1' }] }}
        safeMatchDuration={3}
        competitions={[tomorrow, today]}
      />,
    );
    fireEvent.click(view.getByText('Court pace'));
    // Idle since 09:10 at 09:15: 5 minutes behind, never the 75 the 08:00 bout of the next day would give.
    expect(view.container.textContent).toContain('5m behind');
    expect(view.container.textContent).toContain('Next ~09:15');
  });
});
