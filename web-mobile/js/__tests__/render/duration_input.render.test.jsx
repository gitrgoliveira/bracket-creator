// Render tests for the shared mm:ss DurationInput (mp-m5kf). DurationInput is a
// leaf component with no window.* deps, so it mounts directly under React 18 +
// jsdom. These pin the display derivation and the emit() edge cases the unit
// suite can't reach (they live inside the component closure).

import React from 'react';
import { render, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { DurationInput } from '../../duration.jsx';

function inputs() {
  return {
    min: screen.getByLabelText('minutes'),
    sec: screen.getByLabelText('seconds'),
  };
}

describe('DurationInput', () => {
  it('derives the min/sec display from the seconds prop', () => {
    render(<DurationInput seconds={150} onChange={() => {}} />);
    const { min, sec } = inputs();
    expect(min.value).toBe('2');
    expect(sec.value).toBe('30');
  });

  it('renders both fields blank when seconds is unset', () => {
    render(<DurationInput seconds={NaN} onChange={() => {}} />);
    const { min, sec } = inputs();
    expect(min.value).toBe('');
    expect(sec.value).toBe('');
  });

  it('emits total seconds when the minutes field changes', () => {
    const onChange = vi.fn();
    render(<DurationInput seconds={150} onChange={onChange} />);
    fireEvent.input(inputs().min, { target: { value: '3' } });
    // sibling seconds (30) is read from the controlled prop → 3*60 + 30
    expect(onChange).toHaveBeenLastCalledWith(210);
  });

  it('counts a blank minutes field as 0', () => {
    const onChange = vi.fn();
    render(<DurationInput seconds={NaN} onChange={onChange} />);
    fireEvent.input(inputs().sec, { target: { value: '30' } });
    expect(onChange).toHaveBeenLastCalledWith(30);
  });

  it('clamps the seconds component to 59', () => {
    const onChange = vi.fn();
    render(<DurationInput seconds={120} onChange={onChange} />);
    fireEvent.input(inputs().sec, { target: { value: '75' } });
    expect(onChange).toHaveBeenLastCalledWith(2 * 60 + 59);
  });

  it('clamps the minutes component to the 24h ceiling', () => {
    const onChange = vi.fn();
    render(<DurationInput seconds={NaN} onChange={onChange} />);
    fireEvent.input(inputs().min, { target: { value: '999999' } });
    expect(onChange).toHaveBeenLastCalledWith(1440 * 60);
  });

  it('renders both fields blank for a zero value (resolves to default upstream)', () => {
    render(<DurationInput seconds={0} onChange={() => {}} />);
    const { min, sec } = inputs();
    expect(min.value).toBe('');
    expect(sec.value).toBe('');
  });

  it('coerces a negative / non-numeric seconds entry to 0', () => {
    const onChange = vi.fn();
    render(<DurationInput seconds={120} onChange={onChange} />);
    fireEvent.input(inputs().sec, { target: { value: '-5' } });
    expect(onChange).toHaveBeenLastCalledWith(120);
  });
});
