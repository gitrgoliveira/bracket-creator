import React from 'react';
import { render } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterAll } from 'vitest';
import { IndividualScore } from '../../match_scoreboard.jsx';

// The TV centre dash is an explicit `.msb-sep` span rendered by centreMarks
// when neither a draw "X" nor a hantei "Ht" mark occupies the centre cell.
// The CSS hides it on non-TV surfaces and shows it in .msb--tv context.
describe('msb-vs centre cell (TV centre dash)', () => {
  let prev;
  beforeAll(() => { prev = window.isHikiwake; window.isHikiwake = (t) => t === 'hikiwake'; });
  afterAll(() => { window.isHikiwake = prev; });

  it('renders an msb-sep dash span when there is no draw/hantei mark', () => {
    const { container } = render(
      <IndividualScore match={{ sideA: { name: 'A' }, sideB: { name: 'B' }, ipponsA: ['M'], ipponsB: [] }} variant="tv" showNames />
    );
    const vs = container.querySelector('.msb-vs');
    expect(vs).toBeTruthy();
    const sep = vs.querySelector('.msb-sep');
    expect(sep).toBeTruthy();
    expect(sep.textContent).toBe('–');
    expect(sep.getAttribute('aria-hidden')).toBe('true');
  });

  it('renders the draw X and no msb-sep on a hikiwake → no dash collision', () => {
    const { container } = render(
      <IndividualScore match={{ sideA: { name: 'A' }, sideB: { name: 'B' }, ipponsA: [], ipponsB: [], decision: 'hikiwake' }} variant="tv" showNames />
    );
    const vs = container.querySelector('.msb-vs');
    expect(vs.textContent).toContain('X');
    expect(vs.querySelector('.msb-sep')).toBeNull();
  });
});
