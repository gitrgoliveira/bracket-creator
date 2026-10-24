// A toast says what happened in its icon as well as its words. A write the device holds
// until the connection returns is neither saved nor refused: it takes the pending icon,
// not the success check that "not saved yet" used to sit beside (operator decision
// 2026-10-07), and it is a status, polite, with no dismiss button, like a success.

import React from 'react';
import { render, cleanup, act } from '@testing-library/react';
import { describe, it, expect, afterEach } from 'vitest';
import { Toast } from '../../ui.jsx';
import { pushToast, resetToastQueue } from '../../toast_queue.jsx';

afterEach(() => { act(() => { resetToastQueue(); }); cleanup(); });

const icon = (container) => container.querySelector('.toast__icon').textContent;
const shownAs = (message, type) => {
  const r = render(<Toast />);
  act(() => { pushToast(message, type); });
  return r;
};

describe('the toast icon', () => {
  it('is the pending icon for a write held on the device', () => {
    const { container, getByRole } = shownAs('Not sent yet', 'pending');
    expect(icon(container)).toBe('⏳');
    expect(getByRole('status')).toBeTruthy();
    expect(container.querySelector('.toast__dismiss'), 'nothing to dismiss').toBeNull();
  });

  it('stays the success check for a success and the warning for an error', () => {
    const ok = shownAs('Lineup saved', undefined);
    expect(icon(ok.container)).toBe('✅');
    act(() => { resetToastQueue(); });
    cleanup();
    const err = shownAs('Failed', 'error');
    expect(icon(err.container)).toBe('⚠️');
  });
});
