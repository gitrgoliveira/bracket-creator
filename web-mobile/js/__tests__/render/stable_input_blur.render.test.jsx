// Stage 6 review 7 (minor 17): a seed box saved again on blur though its save
// had already gone, so typing a seed, waiting, then tapping Generate draw sent
// a second save that raced the draw and was refused (409). A blur sends only
// a change still waiting for its debounce.
import React from 'react';
import { render, fireEvent, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { StableInput } from '../../ui.jsx';

describe('StableInput on blur', () => {
  beforeEach(() => { vi.useFakeTimers(); });
  afterEach(() => { vi.useRealTimers(); });

  it('sends nothing more once the debounced change has gone', async () => {
    const onChange = vi.fn();
    const { getByLabelText } = render(<StableInput aria-label="Seed" type="number" value="" onChange={onChange} />);
    const box = getByLabelText('Seed');
    fireEvent.focus(box);
    fireEvent.change(box, { target: { value: '3' } });
    await act(async () => { vi.advanceTimersByTime(250); });
    expect(onChange).toHaveBeenCalledTimes(1);
    fireEvent.blur(box);
    expect(onChange, 'no second save on blur').toHaveBeenCalledTimes(1);
  });

  it('sends a change still waiting when the box loses focus, once', async () => {
    const onChange = vi.fn();
    const { getByLabelText } = render(<StableInput aria-label="Seed" type="number" value="" onChange={onChange} />);
    const box = getByLabelText('Seed');
    fireEvent.focus(box);
    fireEvent.change(box, { target: { value: '4' } });
    fireEvent.blur(box);
    expect(onChange).toHaveBeenCalledTimes(1);
    expect(onChange).toHaveBeenLastCalledWith(4);
    await act(async () => { vi.advanceTimersByTime(250); });
    expect(onChange, 'the debounce was cancelled').toHaveBeenCalledTimes(1);
  });

  it('sends nothing on a blur with no change made', () => {
    const onChange = vi.fn();
    const { getByLabelText } = render(<StableInput aria-label="Seed" type="number" value={2} onChange={onChange} />);
    const box = getByLabelText('Seed');
    fireEvent.focus(box);
    fireEvent.blur(box);
    expect(onChange).not.toHaveBeenCalled();
  });
});
