// Stage 6 review 9 (F5): a warning toast (a change kept with something to
// check) is neither a success nor an error: it carries the warning sign, stays
// as long as an error and can be dismissed, and is announced politely. Like an
// error it is latched (stage 6 review 10, m2): a success or an information
// toast arriving while it shows (the flush pass's "N sent", raised just after
// the warning) never replaces it; an error or another warning does.
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

  it('is not replaced by a success that follows it, but is by an error', () => {
    const { container, rerender } = render(<Toast message="Check that match's lineup." type="warning" onClose={() => {}} />);
    rerender(<Toast message="1 change sent" type="success" onClose={() => {}} />);
    expect(container.querySelector('.toast__msg').textContent).toBe("Check that match's lineup.");
    rerender(<Toast message="Another lineup to check." type="warning" onClose={() => {}} />);
    expect(container.querySelector('.toast__msg').textContent).toBe('Another lineup to check.');
    rerender(<Toast message="Not saved." type="error" onClose={() => {}} />);
    expect(container.querySelector('.toast__msg').textContent).toBe('Not saved.');
  });
});
