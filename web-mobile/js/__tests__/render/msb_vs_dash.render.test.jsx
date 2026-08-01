import React from 'react';
import { render } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterAll } from 'vitest';
import { IndividualScore } from '../../match_scoreboard.jsx';
import { boutMiddle } from '../../bracket.jsx';

// The TV centre separator is an explicit `.msb-sep` span rendered by
// centreMarks when neither a draw "X" nor a hantei "Ht" mark occupies the
// centre cell. It reads "vs" — a dash is never a valid middle value
// (operator ruling). The CSS hides it on non-TV surfaces and shows it in
// .msb--tv context.
describe('msb-vs centre cell (TV centre separator)', () => {
  let prev, prevMid;
  beforeAll(() => {
    prev = window.isHikiwake; window.isHikiwake = (t) => t === 'hikiwake';
    prevMid = window.boutMiddle; window.boutMiddle = boutMiddle; // the real single source
  });
  afterAll(() => { window.isHikiwake = prev; window.boutMiddle = prevMid; });

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

  it('renders the draw X and no msb-sep on a hikiwake → no separator collision', () => {
    const { container } = render(
      <IndividualScore match={{ sideA: { name: 'A' }, sideB: { name: 'B' }, ipponsA: [], ipponsB: [], decision: 'hikiwake' }} variant="tv" showNames />
    );
    const vs = container.querySelector('.msb-vs');
    expect(vs.textContent).toContain('X');
    expect(vs.querySelector('.msb-sep')).toBeNull();
  });
});
