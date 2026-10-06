// The court picker gives focus back to its button when its list closes, so a
// keyboard is not dropped to the page. It must not take focus as it mounts:
// every queue row and score row carries one, so focus jumped to the last row's
// picker (with its focus ring) on load and whenever a row appeared, and the
// browser scrolled the page to it (found in the browser on the court console).

import React from 'react';
import { render, act, fireEvent, cleanup } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterEach } from 'vitest';

let CourtPicker;

beforeAll(async () => {
  await import('../../admin_shell.jsx');
  CourtPicker = window.CourtPicker;
});

afterEach(cleanup);

const COURTS = ['A', 'B', 'C'];

describe('the court picker and focus', () => {
  it('takes no focus as it mounts, so a page of rows keeps the focus it had', () => {
    const { getByTestId } = render(
      <div>
        <input data-testid="elsewhere" />
        <CourtPicker value="A" courts={COURTS} onChange={() => {}} />
        <CourtPicker value="B" courts={COURTS} onChange={() => {}} />
      </div>
    );
    expect(document.activeElement, 'nothing took focus on mount').toBe(document.body);
    getByTestId('elsewhere').focus();
    const { container } = render(<CourtPicker value="C" courts={COURTS} onChange={() => {}} />);
    expect(container.querySelector('button')).not.toBe(document.activeElement);
    expect(document.activeElement, 'a row that appears later takes nothing either').toBe(getByTestId('elsewhere'));
  });

  it('gives focus back to its button when the list closes', async () => {
    const { container } = render(<CourtPicker value="A" courts={COURTS} onChange={() => {}} />);
    const trigger = container.querySelector('button');
    await act(async () => { trigger.click(); });
    const list = container.querySelector('[role="listbox"]');
    expect(list, 'the list is open').not.toBeNull();
    expect(list.contains(document.activeElement), 'focus is in the list').toBe(true);
    await act(async () => { fireEvent.keyDown(document.activeElement, { key: 'Escape' }); });
    expect(container.querySelector('[role="listbox"]'), 'the list is closed').toBeNull();
    expect(document.activeElement, 'focus is back on the button').toBe(trigger);
  });
});
