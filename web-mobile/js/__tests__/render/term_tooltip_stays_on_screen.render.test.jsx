// A glossary tooltip never runs off the screen (the impeccable audit of PR
// #507): it opens rightwards when it fits there, leftwards when only that
// fits, and when neither fits it is moved inline to end just inside the
// screen's right edge. jsdom has no layout, so the boxes are given here.
import React from 'react';
import { render, fireEvent, cleanup } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterAll, afterEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

const SCREEN = { width: 375, height: 800 };
const TIP = { width: 220, height: 60 };

let restoreGlobals;
let Term;

beforeAll(async () => {
  restoreGlobals = installWindowStubs({});
  ({ Term } = await import('../../glossary.jsx'));
  Object.defineProperty(document.documentElement, 'clientWidth', { configurable: true, value: SCREEN.width });
  Object.defineProperty(document.documentElement, 'clientHeight', { configurable: true, value: SCREEN.height });
});

afterEach(() => cleanup());
afterAll(() => {
  delete document.documentElement.clientWidth;
  delete document.documentElement.clientHeight;
  restoreGlobals();
});

const rect = (left, top, width, height) => ({ left, top, width, height, right: left + width, bottom: top + height, x: left, y: top });

// Renders a 24px term at `termLeft` and hovers it, as a pointer does.
function hoverTermAt(termLeft) {
  const { getByTestId, getByRole } = render(<Term name="kiken">?</Term>);
  const wrap = getByTestId('term-wrapper');
  const tip = getByRole('tooltip', { hidden: true });
  wrap.getBoundingClientRect = () => rect(termLeft, 300, 24, 24);
  tip.getBoundingClientRect = () => rect(termLeft, 330, TIP.width, TIP.height);
  fireEvent.pointerEnter(wrap);
  return { wrap, tip };
}

describe('a glossary tooltip stays on screen', () => {
  it('opens rightwards when it fits to the right', () => {
    const { wrap, tip } = hoverTermAt(40);
    expect(wrap.className).not.toContain('tw-term--flip');
    expect(tip.style.cssText).toBe('');
  });

  it('opens leftwards when it fits only to the left', () => {
    const { wrap, tip } = hoverTermAt(300);
    expect(wrap.className).toContain('tw-term--flip');
    expect(tip.style.cssText).toBe('');
  });

  it('fits on neither side: moved to end 16px inside the screen, at its own width', () => {
    const termLeft = 160; // 215px to the right, 184px to the left: both short of 220
    const { wrap, tip } = hoverTermAt(termLeft);
    expect(wrap.className).not.toContain('tw-term--flip');
    expect(tip.style.width).toBe(`${TIP.width}px`);
    expect(tip.style.right).toBe('auto');
    const screenLeft = termLeft + parseFloat(tip.style.left);
    expect(screenLeft).toBe(SCREEN.width - 16 - TIP.width);
    expect(screenLeft).toBeGreaterThanOrEqual(16);
  });

  it('opened by a tap near the bottom, it is measured again once drawn and opens upwards', () => {
    const { getByTestId, getByRole } = render(<Term name="kiken">?</Term>);
    const wrap = getByTestId('term-wrapper');
    const tip = getByRole('tooltip', { hidden: true });
    const drawn = () => wrap.className.includes('tw-term--open');
    wrap.getBoundingClientRect = () => rect(40, 760, 24, 24);
    // Hidden until open, as display:none measures: no size at all.
    tip.getBoundingClientRect = () => (drawn() ? rect(40, 790, TIP.width, TIP.height) : rect(0, 0, 0, 0));
    fireEvent.click(wrap);
    expect(drawn()).toBe(true);
    expect(wrap.className).toContain('tw-term--up');
  });

  it('a width pinned by one measure is not read back as the box\'s own by the next', () => {
    const { getByTestId, getByRole } = render(<Term name="kiken">?</Term>);
    const wrap = getByTestId('term-wrapper');
    const tip = getByRole('tooltip', { hidden: true });
    const drawn = () => wrap.className.includes('tw-term--open');
    wrap.getBoundingClientRect = () => rect(100, 300, 24, 24);
    // Hidden, the box is sized by its declared 320px cap, which fits on
    // neither side; drawn, it is its natural 220px unless a width is pinned.
    tip.getBoundingClientRect = () => {
      if (!drawn()) return rect(0, 0, 0, 0);
      return rect(100, 330, parseFloat(tip.style.width) || TIP.width, TIP.height);
    };
    fireEvent.click(wrap);
    expect(drawn()).toBe(true);
    expect(tip.style.width).toBe(''); // 275px to the right holds 220px
    expect(wrap.className).not.toContain('tw-term--flip');
  });
});
