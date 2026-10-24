// Stage 6 review 9 (F9): a time typed into the schedule row and left within
// the box's debounce was not saved, because the save read the time from the
// last render. The save reads what the box shows.
import React from 'react';
import { render, fireEvent, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, beforeEach, afterEach } from 'vitest';
import '../../ui.jsx';

let AdminTWMatch;
beforeAll(async () => {
  // Captured by the module as it loads; the row's court picker is not under test.
  window.CourtPicker = window.CourtPicker || (() => null);
  window.hasBothSides = window.hasBothSides || (() => true);
  ({ AdminTWMatch } = await import('../../admin_schedule_page.jsx'));
});

const match = {
  id: 'm1', compId: 'c1', compName: 'Autumn Cup', status: 'scheduled', phase: 'knockout', round: 0,
  scheduledAt: '09:00', court: 'A',
  sideA: { id: 'a', name: 'Alice' }, sideB: { id: 'b', name: 'Bob' },
};

describe('the schedule row time box', () => {
  beforeEach(() => { vi.useFakeTimers(); });
  afterEach(() => { vi.useRealTimers(); });

  function open(onTimeChange) {
    const r = render(<AdminTWMatch m={match} courts={['A']} onMove={() => {}} onTimeChange={onTimeChange} />);
    fireEvent.click(r.container.querySelector('.tw-match__time'));
    return { ...r, box: r.container.querySelector('input[type="time"]') };
  }

  it('saves a time typed and left at once', () => {
    const onTimeChange = vi.fn();
    const { box } = open(onTimeChange);
    fireEvent.focus(box);
    fireEvent.change(box, { target: { value: '10:30' } });
    fireEvent.blur(box);
    expect(onTimeChange).toHaveBeenCalledWith('10:30');
    expect(onTimeChange).toHaveBeenCalledTimes(1);
  });

  it('saves a time typed and sent with Enter at once', () => {
    const onTimeChange = vi.fn();
    const { box } = open(onTimeChange);
    fireEvent.focus(box);
    fireEvent.change(box, { target: { value: '11:15' } });
    fireEvent.submit(box.closest('form'));
    expect(onTimeChange).toHaveBeenCalledWith('11:15');
    expect(onTimeChange).toHaveBeenCalledTimes(1);
  });

  it('saves a time typed, waited on and left, once', async () => {
    const onTimeChange = vi.fn();
    const { box } = open(onTimeChange);
    fireEvent.focus(box);
    fireEvent.change(box, { target: { value: '12:00' } });
    await act(async () => { vi.advanceTimersByTime(250); });
    fireEvent.blur(box);
    expect(onTimeChange).toHaveBeenCalledWith('12:00');
    expect(onTimeChange).toHaveBeenCalledTimes(1);
  });
});
