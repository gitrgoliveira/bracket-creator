// One Escape closes one layer. The overlay score editor closes on Escape through a
// window listener; an open fighter list in it takes the key first and keeps it from
// that listener, so an operator closing the list does not close the editor too
// (found in the browser: on the Scores page, Escape on an open list closed both).
// With the list closed, Escape goes on to the editor as before.

import React from 'react';
import { render, act, fireEvent, cleanup } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterEach } from 'vitest';
import { LineupNameInput } from '../../admin_scoring_shared.jsx';

const ROSTER = ['Arai', 'Shiba', 'Tada'];

beforeAll(() => {
  if (typeof window.useClickOutside !== 'function') window.useClickOutside = () => {};
});

let escapesReachingWindow;
const onWindowKeyDown = (e) => { if (e.key === 'Escape') escapesReachingWindow += 1; };

afterEach(() => {
  window.removeEventListener('keydown', onWindowKeyDown);
  cleanup();
});

function mount(roster = ROSTER) {
  escapesReachingWindow = 0;
  window.addEventListener('keydown', onWindowKeyDown);
  const utils = render(<LineupNameInput value="" roster={roster} onSelect={() => {}} ariaLabel="pos" color="shiro" />);
  const input = utils.container.querySelector('input');
  return { ...utils, input, list: () => utils.container.querySelector('.lineup-name__dropdown') };
}

describe('Escape on a fighter name list', () => {
  it('closes an open list and does not reach the editor behind it', () => {
    const { input, list } = mount();
    act(() => { fireEvent.focus(input); });
    expect(list(), 'the list is open').not.toBeNull();
    act(() => { fireEvent.keyDown(input, { key: 'Escape' }); });
    expect(list(), 'the list is closed').toBeNull();
    expect(escapesReachingWindow, 'the editor does not see the Escape that closed the list').toBe(0);
  });

  it('goes on to the editor when the list is already closed', () => {
    const { input, list } = mount();
    act(() => { fireEvent.focus(input); });
    act(() => { fireEvent.keyDown(input, { key: 'Escape' }); });
    expect(list()).toBeNull();
    act(() => { fireEvent.keyDown(input, { key: 'Escape' }); });
    expect(escapesReachingWindow, 'a second Escape closes the editor, as before').toBe(1);
  });

  // The list is drawn only when it has an option to show, so a box with nothing to
  // offer (a team with no members yet, no query) holds no list for Escape to close:
  // taking the key anyway made the operator press it twice to close the editor.
  it('goes on to the editor when no list is drawn, since there is nothing to close', () => {
    const { input, list } = mount([]);
    act(() => { fireEvent.focus(input); });
    expect(list(), 'an empty roster draws no list').toBeNull();
    act(() => { fireEvent.keyDown(input, { key: 'Escape' }); });
    expect(escapesReachingWindow, 'the first Escape closes the editor').toBe(1);
  });

  it('still takes the Escape of a typed name that has only its add row to offer', () => {
    const { input, list } = mount([]);
    act(() => { fireEvent.focus(input); });
    act(() => { fireEvent.change(input, { target: { value: 'Newcomer' } }); });
    expect(list(), 'the add row is a list').not.toBeNull();
    act(() => { fireEvent.keyDown(input, { key: 'Escape' }); });
    expect(list(), 'the list is closed').toBeNull();
    expect(escapesReachingWindow, 'and the editor does not see that Escape').toBe(0);
  });
});
