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
const byCourt = {
  A: [
    { compId: 'c1', id: 'm1', court: 'A', status: 'completed', scheduledAt: '09:00', modifiedAt: finishedAt },
    { compId: 'c1', id: 'm2', court: 'A', status: 'scheduled', scheduledAt: '09:10' },
  ],
};

function openPanel(props) {
  const view = render(<CourtPacePanel byCourt={byCourt} safeMatchDuration={3} {...props} />);
  fireEvent.click(view.getByText('Court pace'));
  return view;
}

describe('CourtPacePanel slots', () => {
  it('measures drift with the competition slot, not the page time per match', () => {
    const view = openPanel({ competitions: [{ id: 'c1', slotMinutes: 10 }] });
    expect(view.container.textContent).toContain('10m behind');
  });

  it('falls back to the page time per match for a competition with no slot', () => {
    const view = openPanel({ competitions: [{ id: 'c1' }] });
    expect(view.container.textContent).toContain('17m behind');
  });
});
