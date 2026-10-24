// Stage 6 review 9 (F5): a warning toast (a change kept with something to
// check) is neither a success nor an error: it carries the warning sign, stays
// as long as an error and can be dismissed, and is announced politely. Like an
// error it is latched (stage 6 review 10, m2): a success or an information
// toast arriving while it shows (the flush pass's "N sent", raised just after
// the warning) never replaces it. Since stage 6 review 12 nothing replaces a
// latched toast, an error or another warning included: each is told once, so
// it waits in the queue, in arrival order. The queue is toast_queue.jsx's.
import React from 'react';
import { render, fireEvent, act, cleanup } from '@testing-library/react';
import { describe, it, expect, vi, afterEach } from 'vitest';
import { Toast } from '../../ui.jsx';
import { pushToast, resetToastQueue, TOAST_ERROR_DWELL_MS, TOAST_SUCCESS_DWELL_MS, TOAST_FADE_MS } from '../../toast_queue.jsx';

afterEach(() => { act(() => { resetToastQueue(); }); cleanup(); });
const push = (...a) => act(() => { pushToast(...a); });

describe('a warning toast', () => {
  it('shows the warning sign and a dismiss, announced politely', () => {
    const { container, getByRole } = render(<Toast />);
    push("Check that match's lineup.", 'warning');
    const toast = container.querySelector('.toast');
    expect(toast.className).toContain('toast--warning');
    expect(toast.getAttribute('role')).toBe('status');
    expect(container.querySelector('.toast__icon').textContent).toBe('⚠️');
    expect(getByRole('button', { name: 'Dismiss' })).toBeTruthy();
  });

  it('is replaced by nothing that follows it: a success, a warning and an error wait, in order', () => {
    vi.useFakeTimers();
    const { container, getByRole } = render(<Toast />);
    const shown = () => container.querySelector('.toast__msg').textContent;
    push("Check that match's lineup.", 'warning');
    push('1 change sent', 'success');
    push('Another lineup to check.', 'warning');
    push('Not saved.', 'error');
    expect(shown()).toBe("Check that match's lineup.");
    fireEvent.click(getByRole('button', { name: 'Dismiss' }));
    expect(shown()).toBe('1 change sent');
    act(() => { vi.advanceTimersByTime(TOAST_SUCCESS_DWELL_MS + TOAST_FADE_MS); });
    expect(shown()).toBe('Another lineup to check.');
    fireEvent.click(getByRole('button', { name: 'Dismiss' }));
    expect(shown()).toBe('Not saved.');
    vi.useRealTimers();
  });
});

// Stage 6 review 11, minor 1: a toast held back by a latched warning or error
// is queued behind it, never dropped: "A tap shown as not applied was applied
// after all" is told once per device, so dropping it told nobody.
describe('a toast that arrives while a warning or an error shows', () => {
  afterEach(() => { vi.useRealTimers(); });
  const shown = (c) => c.querySelector('.toast__msg')?.textContent;

  it('shows once the warning is dismissed, and the toast goes only after it', () => {
    const { container, getByRole } = render(<Toast />);
    push("Check that match's lineup.", 'warning');
    push('A tap shown as not applied was applied after all.', 'info', { latched: true });
    expect(shown(container)).toBe("Check that match's lineup.");
    fireEvent.click(getByRole('button', { name: 'Dismiss' }));
    expect(shown(container)).toBe('A tap shown as not applied was applied after all.');
  });

  it('shows in order after the warning\'s dwell, each in turn', () => {
    vi.useFakeTimers();
    const { container } = render(<Toast />);
    push("Check that match's lineup.", 'warning');
    push('First note.', 'info');
    push('Second note.', 'success');
    act(() => { vi.advanceTimersByTime(TOAST_ERROR_DWELL_MS + TOAST_FADE_MS); });
    expect(shown(container)).toBe('First note.');
    act(() => { vi.advanceTimersByTime(TOAST_SUCCESS_DWELL_MS + TOAST_FADE_MS); });
    expect(shown(container)).toBe('Second note.');
    act(() => { vi.advanceTimersByTime(TOAST_SUCCESS_DWELL_MS + TOAST_FADE_MS); });
    expect(container.querySelector('.toast')).toBeNull();
  });

  it('shows after an error it arrived behind', () => {
    const { container, getByRole } = render(<Toast />);
    push('Not saved.', 'error');
    push('1 change sent', 'success');
    expect(shown(container)).toBe('Not saved.');
    fireEvent.click(getByRole('button', { name: 'Dismiss' }));
    expect(shown(container)).toBe('1 change sent');
  });
});
