// The court picker gives focus back to its button when its list closes, so a
// keyboard is not dropped to the page. It must not take focus as it mounts:
// every queue row and score row carries one, so focus jumped to the last row's
// picker (with its focus ring) on load and whenever a row appeared, and the
// browser scrolled the page to it (found in the browser on the court console).

import React from 'react';
import { render, act, fireEvent, cleanup } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterEach } from 'vitest';

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

  // Focus goes back only for a close made inside the picker, and without scrolling:
  // the button is where the operator was, so the page has no reason to move.
  it('gives focus back without scrolling the page, for Escape and for a choice', async () => {
    const onChange = vi.fn();
    const { container } = render(<CourtPicker value="A" courts={COURTS} onChange={onChange} />);
    const trigger = container.querySelector('button');
    const focus = vi.spyOn(trigger, 'focus');
    await act(async () => { trigger.click(); });
    await act(async () => { fireEvent.keyDown(document.activeElement, { key: 'Escape' }); });
    expect(focus).toHaveBeenCalledTimes(1);
    expect(focus, 'Escape').toHaveBeenCalledWith({ preventScroll: true });

    await act(async () => { trigger.click(); });
    const option = [...container.querySelectorAll('[role="option"]')].find((o) => o.textContent === 'B');
    await act(async () => { option.click(); });
    expect(onChange).toHaveBeenCalledWith('B');
    expect(container.querySelector('[role="listbox"]'), 'the choice closed the list').toBeNull();
    expect(focus).toHaveBeenCalledTimes(2);
    expect(focus, 'a choice').toHaveBeenLastCalledWith({ preventScroll: true });
    expect(document.activeElement).toBe(trigger);
  });

  // A tap on another control closes the list from outside: its mousedown closes the
  // list, then the browser gives the tapped control focus, and only after that does
  // the picker's close effect run. Taking focus back there blurred the tapped name
  // box (and dropped the iPad keyboard), and without preventScroll it scrolled the
  // Scores page back to the button (1682 to 0).
  it('leaves focus where an outside tap put it', async () => {
    const { container, getByTestId } = render(
      <div>
        <input data-testid="elsewhere" />
        <CourtPicker value="A" courts={COURTS} onChange={() => {}} />
      </div>
    );
    const trigger = container.querySelector('button');
    const focus = vi.spyOn(trigger, 'focus');
    await act(async () => { trigger.click(); });
    expect(container.querySelector('[role="listbox"]'), 'the list is open').not.toBeNull();

    const other = getByTestId('elsewhere');
    await act(async () => { fireEvent.mouseDown(other); other.focus(); });

    expect(container.querySelector('[role="listbox"]'), 'the outside tap closed the list').toBeNull();
    expect(document.activeElement, 'the tapped control keeps focus').toBe(other);
    expect(focus, 'the picker did not reach for its button').not.toHaveBeenCalled();
  });

  it('gives focus back when its own button closes the list', async () => {
    const { container } = render(<CourtPicker value="A" courts={COURTS} onChange={() => {}} />);
    const trigger = container.querySelector('button');
    const focus = vi.spyOn(trigger, 'focus');
    await act(async () => { trigger.click(); });
    await act(async () => { trigger.click(); });
    expect(container.querySelector('[role="listbox"]'), 'the list is closed').toBeNull();
    expect(focus).toHaveBeenCalledWith({ preventScroll: true });
  });
});
