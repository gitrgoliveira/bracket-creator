// A toast says what happened in its icon as well as its words. A write the device holds
// until the connection returns is neither saved nor refused: it takes the pending icon,
// not the success check that "not saved yet" used to sit beside (operator decision
// 2026-10-07), and it is a status, polite, with no dismiss button, like a success.

import React from 'react';
import { render, cleanup } from '@testing-library/react';
import { describe, it, expect, afterEach } from 'vitest';
import { Toast } from '../../ui.jsx';

afterEach(cleanup);

const icon = (container) => container.querySelector('.toast__icon').textContent;

describe('the toast icon', () => {
  it('is the pending icon for a write held on the device', () => {
    const { container, getByRole } = render(<Toast message="Not sent yet" type="pending" onClose={() => {}} />);
    expect(icon(container)).toBe('⏳');
    expect(getByRole('status')).toBeTruthy();
    expect(container.querySelector('.toast__dismiss'), 'nothing to dismiss').toBeNull();
  });

  it('stays the success check for a success and the warning for an error', () => {
    const ok = render(<Toast message="Lineup saved" onClose={() => {}} />);
    expect(icon(ok.container)).toBe('✅');
    cleanup();
    const err = render(<Toast message="Failed" type="error" onClose={() => {}} />);
    expect(icon(err.container)).toBe('⚠️');
  });
});
