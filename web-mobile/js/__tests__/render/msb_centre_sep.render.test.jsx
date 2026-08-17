import React from 'react';
import { render } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterAll } from 'vitest';
import { IndividualScore } from '../../match_scoreboard.jsx';
import { matchMiddleMark } from '../../bracket.jsx';

// The centre separator is an explicit `.msb-sep` span rendered by centreMarks
// when no mark (X / (E) / (DH)) occupies the centre cell. It reads "vs" — a
// dash is never a valid middle value (operator ruling) — and it is visible on
// EVERY surface: "vs" is the plain middle value of an unmarked row, so a board
// that hides it leaves the FIK row's centre blank. The CSS used to show it only
// in .msb--tv context, which is why the lobby board (the one display surface
// that passes no `variant`) rendered nothing between the ippon slots.
describe('msb-vs centre cell (centre separator)', () => {
  let prev, prevMid;
  beforeAll(() => {
    prev = window.isHikiwake; window.isHikiwake = (t) => t === 'hikiwake';
    prevMid = window.matchMiddleMark; window.matchMiddleMark = matchMiddleMark; // the real chip projection
  });
  afterAll(() => { window.isHikiwake = prev; window.matchMiddleMark = prevMid; });

  it('renders a plain "vs" msb-sep span when there is no draw/hantei mark', () => {
    const { container } = render(
      <IndividualScore match={{ sideA: { name: 'A' }, sideB: { name: 'B' }, ipponsA: ['M'], ipponsB: [] }} variant="tv" showNames />
    );
    const vs = container.querySelector('.msb-vs');
    expect(vs).toBeTruthy();
    const sep = vs.querySelector('.msb-sep');
    expect(sep).toBeTruthy();
    expect(sep.textContent).toBe('vs');
    expect(sep.getAttribute('aria-hidden')).toBe('true');
  });

  // The lobby board's shape: no `variant` at all, so it never matched .msb--tv.
  // jsdom applies no stylesheet, so this pins only the JS half (the span is
  // emitted regardless of variant); the CSS half is browser-verified.
  it.each([['tv'], ['card'], [undefined]])('renders the "vs" separator for variant=%s', (variant) => {
    const { container } = render(
      <IndividualScore match={{ sideA: { name: 'A' }, sideB: { name: 'B' }, ipponsA: [], ipponsB: [] }} variant={variant} showNames />
    );
    const sep = container.querySelector('.msb-vs .msb-sep');
    expect(sep).toBeTruthy();
    expect(sep.textContent).toBe('vs');
  });

  it('renders the draw X and no msb-sep on a hikiwake → no separator collision', () => {
    const { container } = render(
      <IndividualScore match={{ sideA: { name: 'A' }, sideB: { name: 'B' }, ipponsA: [], ipponsB: [], decision: 'hikiwake' }} variant="tv" showNames />
    );
    const vs = container.querySelector('.msb-vs');
    expect(vs.textContent).toContain('X');
    expect(vs.querySelector('.msb-sep')).toBeNull();
  });
});
