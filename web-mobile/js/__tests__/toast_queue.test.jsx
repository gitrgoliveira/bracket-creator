// toast_queue.jsx: a toast that is no longer true is taken back by its id,
// whether it is the one shown or still queued behind another.
import { describe, it, expect, afterEach } from 'vitest';
import { pushToast, toastSnapshot, dismissToast, dismissToastById, queuedToastCount, resetToastQueue } from '../toast_queue.jsx';

afterEach(() => { resetToastQueue(); });

describe('dismissToastById', () => {
  it('takes the toast shown down and shows the next one', () => {
    pushToast('Saved offline', 'warning', { id: 'saved' });
    pushToast('Court A called');
    dismissToastById('saved');
    expect(toastSnapshot().message).toBe('Court A called');
    expect(queuedToastCount()).toBe(0);
  });

  it('drops a copy queued behind another toast, so it never shows', () => {
    pushToast('Court A called', 'error');
    pushToast('Saved offline', 'success', { id: 'saved' });
    pushToast('Court B called', 'success');
    expect(queuedToastCount()).toBe(2);
    dismissToastById('saved');
    expect(toastSnapshot().message).toBe('Court A called');
    expect(queuedToastCount()).toBe(1);
    dismissToast();
    expect(toastSnapshot().message).toBe('Court B called');
    dismissToast();
    expect(toastSnapshot()).toBeNull();
  });

  it('leaves toasts with another id, or none, as they are', () => {
    pushToast('Court A called', 'error');
    pushToast('Other', 'success', { id: 'other' });
    dismissToastById('saved');
    dismissToastById(null);
    expect(toastSnapshot().message).toBe('Court A called');
    expect(queuedToastCount()).toBe(1);
  });
});
