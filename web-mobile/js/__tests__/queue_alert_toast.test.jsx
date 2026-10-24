// bc-evts stage 6 review 12 (M1): a queue alert told once is latched in the
// toast queue, so a following "N sent" waits behind it; one alert object is
// one toast (identity, never words). Drives the real subscriber's three calls
// (queueAlertMessage, queueAlertToastType, queueAlertToastOptions) into the
// real queue owner.
import { describe, it, expect, vi, afterEach } from 'vitest';
import { queueAlertMessage, queueAlertToastType, queueAlertToastOptions } from '../app.jsx';
import { pushToast, toastSnapshot, dismissToast, resetToastQueue, TOAST_ERROR_DWELL_MS, TOAST_FADE_MS } from '../toast_queue.jsx';

afterEach(() => { resetToastQueue(); vi.useRealTimers(); });
const tell = (alert) => pushToast(queueAlertMessage(alert), queueAlertToastType(alert), queueAlertToastOptions(alert));

describe('a queue alert in the toast queue', () => {
  it('A: tap_applied stays for its full dwell and "3 sent" shows after it', () => {
    vi.useFakeTimers();
    const sent = { kind: 'sent', count: 3, terminalCount: 3 };
    tell({ kind: 'tap_applied', count: 1, terminalCount: 0 });
    tell(sent);
    expect(toastSnapshot().message).toMatch(/applied after all/);
    expect(toastSnapshot().latched).toBe(true);
    vi.advanceTimersByTime(TOAST_ERROR_DWELL_MS - 1);
    expect(toastSnapshot().message).toMatch(/applied after all/);
    vi.advanceTimersByTime(1 + TOAST_FADE_MS);
    expect(toastSnapshot().message).toBe(queueAlertMessage(sent));
  });

  it('displaced is latched too: "N sent" waits behind it', () => {
    tell({ kind: 'displaced', count: 1, terminalCount: 1 });
    tell({ kind: 'sent', count: 1, terminalCount: 1 });
    expect(toastSnapshot().type).toBe('info');
    expect(toastSnapshot().latched).toBe(true);
    dismissToast();
    expect(toastSnapshot().type).toBe('success');
  });

  it('one alert object replayed shows once; two alerts with the same words both show', () => {
    const a = { kind: 'displaced', count: 1, terminalCount: 1 };
    const b = { kind: 'displaced', count: 1, terminalCount: 1 };
    tell(a); tell(a); tell(b);
    const words = toastSnapshot().message;
    dismissToast();
    expect(toastSnapshot().message).toBe(words);
    dismissToast();
    expect(toastSnapshot()).toBeNull();
  });

  it('"N sent" is not latched; every other kind is', () => {
    const kinds = ['tap_applied', 'displaced', 'not_applied', 'member_kept', 'member_unsent', 'taps_parked',
      'lineup_warning', 'rejected', 'superseded', 'storage_full', 'server_error', 'auth_required', 'discarded', 'unreadable'];
    for (const kind of kinds) expect(queueAlertToastOptions({ kind }).latched, kind).toBe(true);
    expect(queueAlertToastOptions({ kind: 'sent' }).latched).toBe(false);
  });
});
