// Render tests for the masked m:ss DurationInput (mp-m5kf). DurationInput is a
// leaf component with no window.* deps, so it mounts directly under React 18 +
// jsdom. These cover the display derivation, the parse/normalize behaviour, and
// the validation states the pure parseDuration tests can't reach (draft state,
// aria wiring, onValidity plumbing, blur normalization).

import React from 'react';
import { render, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import {
  DurationInput, parseDuration, formatDuration,
  MIN_DURATION_SECONDS, MAX_DURATION_SECONDS,
} from '../../duration.jsx';

const field = () => screen.getByLabelText('Match duration');

function mount(props = {}) {
  const onChange = vi.fn();
  const onValidity = vi.fn();
  render(
    <DurationInput
      label="Match duration"
      seconds={NaN}
      onChange={onChange}
      onValidity={onValidity}
      {...props}
    />
  );
  return { onChange, onValidity };
}

describe('formatDuration', () => {
  it('renders canonical m:ss with a zero-padded seconds component', () => {
    expect(formatDuration(150)).toBe('2:30');
    expect(formatDuration(123)).toBe('2:03');
    expect(formatDuration(45)).toBe('0:45');
    expect(formatDuration(600)).toBe('10:00');
    expect(formatDuration(3600)).toBe('60:00');
  });

  it('renders unset / zero / negative as blank', () => {
    expect(formatDuration(NaN)).toBe('');
    expect(formatDuration(0)).toBe('');
    expect(formatDuration(-5)).toBe('');
    expect(formatDuration(undefined)).toBe('');
  });
});

describe('parseDuration', () => {
  it('treats blank as "use the default" rather than an error', () => {
    expect(parseDuration('')).toEqual({ seconds: NaN, error: null });
    expect(parseDuration('   ')).toEqual({ seconds: NaN, error: null });
  });

  it('reads a bare number as whole minutes', () => {
    expect(parseDuration('3')).toEqual({ seconds: 180, error: null });
  });

  it('reads the m:ss form the label advertises', () => {
    expect(parseDuration('2:30')).toEqual({ seconds: 150, error: null });
    expect(parseDuration('0:90')).toEqual({ seconds: NaN, error: 'Seconds must be 00-59.' });
  });

  it('rejects the bare :ss form, which the 1:00 floor makes unreachable', () => {
    // The seconds component tops out at 59, so any ":ss" entry is under the
    // floor by construction. Worth pinning: it used to be a valid shorthand
    // when the floor was 0:30.
    expect(parseDuration(':45')).toEqual({ seconds: NaN, error: 'Minimum is 1:00.' });
    expect(parseDuration(':59')).toEqual({ seconds: NaN, error: 'Minimum is 1:00.' });
  });

  it('accepts a single-digit seconds component so typing toward 2:30 never flashes an error', () => {
    // "2:3" is a keystroke on the way to "2:30"; it must parse, not reject.
    expect(parseDuration('2:3')).toEqual({ seconds: 123, error: null });
  });

  it('rejects a seconds component above 59', () => {
    expect(parseDuration('2:60').error).toBe('Seconds must be 00-59.');
  });

  it('rejects unparseable input', () => {
    expect(parseDuration('abc').error).toBe('Use m:ss, for example 2:30.');
    expect(parseDuration('1:2:3').error).toBe('Use m:ss, for example 2:30.');
  });

  it('rejects values outside the shiai band instead of clamping them', () => {
    expect(parseDuration('0:03')).toEqual({ seconds: NaN, error: 'Minimum is 1:00.' });
    expect(parseDuration('0:59')).toEqual({ seconds: NaN, error: 'Minimum is 1:00.' });
    expect(parseDuration('90')).toEqual({ seconds: NaN, error: 'Maximum is 60:00.' });
    expect(parseDuration('60:01')).toEqual({ seconds: NaN, error: 'Maximum is 60:00.' });
    // The band edges themselves are valid.
    expect(parseDuration('1:00')).toEqual({ seconds: MIN_DURATION_SECONDS, error: null });
    expect(parseDuration('60:00')).toEqual({ seconds: MAX_DURATION_SECONDS, error: null });
    // A realistic long match sits comfortably inside the band.
    expect(parseDuration('15:00')).toEqual({ seconds: 900, error: null });
  });
});

describe('DurationInput', () => {
  it('derives the m:ss display from the seconds prop', () => {
    mount({ seconds: 150 });
    expect(field().value).toBe('2:30');
  });

  it('renders blank and a "using the default" note when unset', () => {
    mount({ seconds: NaN });
    expect(field().value).toBe('');
    expect(screen.getByText('Using the default, 3:00.')).toBeTruthy();
  });

  it('emits total seconds for the m:ss the label promises', () => {
    // The two-field predecessor turned this exact input into 230 MINUTES.
    const { onChange } = mount({ seconds: 180 });
    fireEvent.change(field(), { target: { value: '2:30' } });
    expect(onChange).toHaveBeenLastCalledWith(150);
  });

  it('emits NaN when cleared so the caller falls back to the default', () => {
    const { onChange } = mount({ seconds: 150 });
    fireEvent.change(field(), { target: { value: '' } });
    expect(onChange).toHaveBeenLastCalledWith(NaN);
  });

  it('does NOT emit an out-of-band value, and reports the error upward', () => {
    const { onChange, onValidity } = mount({ seconds: 180 });
    fireEvent.change(field(), { target: { value: '0:03' } });
    expect(onChange).not.toHaveBeenCalled();
    expect(onValidity).toHaveBeenLastCalledWith('Minimum is 1:00.');
    expect(screen.getByRole('alert').textContent).toBe('Minimum is 1:00.');
    expect(field().getAttribute('aria-invalid')).toBe('true');
  });

  it('clears the error and resumes emitting once the value re-enters the band', () => {
    const { onChange, onValidity } = mount({ seconds: 180 });
    fireEvent.change(field(), { target: { value: '0:03' } });
    fireEvent.change(field(), { target: { value: '1:33' } });
    expect(onChange).toHaveBeenLastCalledWith(93);
    expect(onValidity).toHaveBeenLastCalledWith(null);
    expect(screen.queryByRole('alert')).toBeNull();
  });

  it('keeps the operator\'s raw text while they type, invalid or not', () => {
    mount({ seconds: 180 });
    fireEvent.change(field(), { target: { value: '2:6' } });
    expect(field().value).toBe('2:6');
  });

  it('normalizes a valid draft to canonical m:ss on blur', () => {
    mount({ seconds: 180 });
    fireEvent.change(field(), { target: { value: '2:3' } });
    fireEvent.blur(field());
    expect(field().value).toBe('2:03');
  });

  it('leaves an invalid draft alone on blur so the operator can correct it', () => {
    mount({ seconds: 180 });
    fireEvent.change(field(), { target: { value: '2:99' } });
    fireEvent.blur(field());
    expect(field().value).toBe('2:99');
    expect(screen.getByRole('alert')).toBeTruthy();
  });

  it('resyncs the draft when the seconds prop moves independently (e.g. an SSE push)', () => {
    const onChange = vi.fn();
    const { rerender } = render(
      <DurationInput label="Match duration" seconds={150} onChange={onChange} />
    );
    expect(field().value).toBe('2:30');
    rerender(<DurationInput label="Match duration" seconds={240} onChange={onChange} />);
    expect(field().value).toBe('4:00');
  });

  it('wires the caller hint and its own error into aria-describedby', () => {
    mount({ seconds: 180, id: 'dur', describedBy: 'dur-hint' });
    expect(field().getAttribute('aria-describedby')).toBe('dur-hint');
    fireEvent.change(field(), { target: { value: 'nope' } });
    expect(field().getAttribute('aria-describedby')).toBe('dur-hint dur-error');
  });

  it('opens a numeric keypad on touch devices', () => {
    mount({ seconds: 180 });
    expect(field().getAttribute('inputmode')).toBe('numeric');
  });
});

// Regression guard for the tri-review finding that clearing a duration field
// silently kept the previous value while the UI confirmed the opposite.
// updateDurationSeconds stages 0 (the wire value for "unset") on a clear, NOT
// NaN: NaN hit saveNow's safeNonNegInt disk-clobber fallback and re-saved the
// old duration, so the field displayed "Using the default, 3:00" over a save
// that had not reset anything.
describe('clearing a duration', () => {
  it('emits NaN from the component so the caller can stage an explicit reset', () => {
    const onChange = vi.fn();
    render(<DurationInput label="Match duration" seconds={150} onChange={onChange} />);
    fireEvent.change(screen.getByLabelText('Match duration'), { target: { value: '' } });
    expect(onChange).toHaveBeenLastCalledWith(NaN);
  });

  it('is staged as 0, not NaN, by the settings handler', () => {
    // Mirrors updateDurationSeconds in admin_competition_settings.jsx. Kept as
    // an inline mirror because the handler is a closure over component state.
    const staged = [];
    const updateDurationSeconds = (key) => (secOrNaN) =>
      staged.push([key, Number.isFinite(secOrNaN) ? secOrNaN : 0]);

    updateDurationSeconds('poolMatchDurationSeconds')(NaN);
    updateDurationSeconds('poolMatchDurationSeconds')(150);

    expect(staged).toEqual([
      ['poolMatchDurationSeconds', 0],
      ['poolMatchDurationSeconds', 150],
    ]);
    // 0 is finite, so safeNonNegInt round-trips it as a genuine reset rather
    // than falling back to the last-saved value.
    expect(Number.isFinite(staged[0][1])).toBe(true);
  });
});
