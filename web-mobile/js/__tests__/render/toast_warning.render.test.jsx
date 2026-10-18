// Stage 6 review 9 (F5): a warning toast (a change kept with something to
// check) is neither a success nor an error: it carries the warning sign, stays
// as long as an error and can be dismissed, but is announced politely and
// never holds back the next toast.
import React from 'react';
import { render } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import { Toast } from '../../ui.jsx';

describe('a warning toast', () => {
  it('shows the warning sign and a dismiss, announced politely', () => {
    const { container, getByRole } = render(<Toast message="Check that match's lineup." type="warning" onClose={() => {}} />);
    const toast = container.querySelector('.toast');
    expect(toast.className).toContain('toast--warning');
    expect(toast.getAttribute('role')).toBe('status');
    expect(container.querySelector('.toast__icon').textContent).toBe('⚠️');
    expect(getByRole('button', { name: 'Dismiss' })).toBeTruthy();
  });
});
