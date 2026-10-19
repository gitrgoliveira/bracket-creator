// bc-evts stage 6 review 12: the toast queue has ONE owner, toast_queue.jsx,
// at module level, and <Toast> only draws it.
// - M1: a notice told once (a queue alert other than "N sent": tap_applied,
//   displaced, ...) is latched: a following success waits behind it.
// - m1: strict arrival order, a toast arriving while the shown one fades is
//   queued behind those queued earlier.
// - m2: the queue survives <Toast> remounting in another place (the admin and
//   viewer branches each render one), and identity, never words, dedupes.
import React from 'react';
import { render, act, cleanup, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, afterEach } from 'vitest';
import { Toast } from '../../ui.jsx';
import { pushToast, resetToastQueue, TOAST_ERROR_DWELL_MS, TOAST_SUCCESS_DWELL_MS, TOAST_FADE_MS } from '../../toast_queue.jsx';

// The queue alerts' own options (app.jsx queueAlertToastOptions) are pinned in
// queue_alert_toast.test.jsx: importing app.jsx here would mount the App.
const tapApplied = ['A tap shown as not applied was applied after all. Check the score.', 'info', { id: 'queue-alert:tap_applied:1', latched: true }];
const displaced = (id) => ['A result that landed moved a later change to the history.', 'info', { id, latched: true }];

afterEach(() => { act(() => { resetToastQueue(); }); cleanup(); vi.useRealTimers(); });
const msg = (c) => c.querySelector('.toast__msg')?.textContent;
const push = (...a) => act(() => { pushToast(...a); });
const tick = (ms) => act(() => { vi.advanceTimersByTime(ms); });

describe('the toast queue', () => {
  it('A: a tap_applied alert stays for its dwell; a following "3 sent" shows after it', () => {
    vi.useFakeTimers();
    const { container, getByRole } = render(<Toast />);
    push(...tapApplied);
    push('3 results sent.', 'success', { id: 'queue-alert:sent:2', latched: false });
    expect(msg(container)).toMatch(/applied after all/);
    expect(getByRole('button', { name: 'Dismiss' }), 'a latched info toast can be dismissed').toBeTruthy();
    tick(TOAST_ERROR_DWELL_MS - 1);
    expect(msg(container)).toMatch(/applied after all/);
    tick(1 + TOAST_FADE_MS);
    expect(msg(container)).toBe('3 results sent.');
  });

  it('B: a toast arriving during an error\'s fade shows after one queued earlier', () => {
    vi.useFakeTimers();
    const { container } = render(<Toast />);
    push('Not saved.', 'error');
    push('Older', 'success');
    tick(TOAST_ERROR_DWELL_MS);
    expect(container.querySelector('.toast').className).not.toContain('is-visible');
    push('Newer', 'success');
    tick(TOAST_FADE_MS);
    expect(msg(container)).toBe('Older');
    tick(TOAST_SUCCESS_DWELL_MS + TOAST_FADE_MS);
    expect(msg(container)).toBe('Newer');
  });

  it('B2: a success arriving while a success fades waits for it', () => {
    vi.useFakeTimers();
    const { container } = render(<Toast />);
    push('First', 'success');
    tick(TOAST_SUCCESS_DWELL_MS);
    push('Second', 'success');
    expect(msg(container)).toBe('First');
    tick(TOAST_FADE_MS);
    expect(msg(container)).toBe('Second');
  });

  it('C: two queued toasts survive the Toast remounting in another place, in order, without re-timing the shown one', () => {
    vi.useFakeTimers();
    const admin = render(<div className="admin"><Toast /></div>);
    push('Not saved.', 'error');
    push('One', 'success');
    push('Two', 'success');
    tick(TOAST_ERROR_DWELL_MS - 1000);
    admin.unmount();
    const viewer = render(<section className="viewer"><Toast /></section>);
    expect(msg(viewer.container)).toBe('Not saved.');
    tick(1000 + TOAST_FADE_MS);
    expect(msg(viewer.container)).toBe('One');
    tick(TOAST_SUCCESS_DWELL_MS + TOAST_FADE_MS);
    expect(msg(viewer.container)).toBe('Two');
    tick(TOAST_SUCCESS_DWELL_MS + TOAST_FADE_MS);
    expect(viewer.container.querySelector('.toast')).toBeNull();
  });

  it('D: two toasts with the same words and no id both show; two with the same id show once', () => {
    vi.useFakeTimers();
    const { container, getByRole } = render(<Toast />);
    push('Not saved.', 'error');
    push('Saved', 'success');
    push('Saved', 'success');
    push('Kept', 'success', { id: 'x' });
    push('Kept again', 'success', { id: 'x' });
    fireEvent.click(getByRole('button', { name: 'Dismiss' }));
    expect(msg(container)).toBe('Saved');
    tick(TOAST_SUCCESS_DWELL_MS + TOAST_FADE_MS);
    expect(msg(container)).toBe('Saved');
    tick(TOAST_SUCCESS_DWELL_MS + TOAST_FADE_MS);
    expect(msg(container)).toBe('Kept');
    tick(TOAST_SUCCESS_DWELL_MS + TOAST_FADE_MS);
    expect(container.querySelector('.toast')).toBeNull();
  });

  it('D2: one alert told twice (same id) shows once; two alerts with the same words both show', () => {
    const { container, getByRole } = render(<Toast />);
    push(...displaced('a'));
    push(...displaced('a'));
    push(...displaced('b'));
    fireEvent.click(getByRole('button', { name: 'Dismiss' }));
    expect(msg(container)).toBe(displaced('b')[0]);
    fireEvent.click(getByRole('button', { name: 'Dismiss' }));
    expect(container.querySelector('.toast')).toBeNull();
  });

  it('a success with nothing queued is replaced at once by the next toast', () => {
    const { container } = render(<Toast />);
    push('Saved', 'success');
    push('Not saved.', 'error');
    expect(msg(container)).toBe('Not saved.');
  });

});
