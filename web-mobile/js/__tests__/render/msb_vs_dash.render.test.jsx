import React from 'react';
import { render } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterAll } from 'vitest';
import { IndividualScore } from '../../match_scoreboard.jsx';

// The TV centre dash is a CSS rule: `.msb--tv .msb-vs:empty::before { content:"–" }`.
// It depends on `.msb-vs` being TRULY :empty (no text or element child nodes)
// when there's no draw/hantei mark. centreMarks renders the cell as
//   <span className="msb-vs">{isDraw?…:null}{hantei?…:null}</span>
// and esbuild strips the whitespace-only text between those sibling expressions,
// so when both are null the span has zero child nodes → :empty matches → the
// dash renders (a draw "X" or hantei "Ht" child makes it non-empty, suppressing
// the dash so there's no collision). These tests pin that invariant against a
// future whitespace/markup regression that would silently kill the dash.
describe('msb-vs centre cell emptiness (drives the TV centre dash)', () => {
  let prev;
  beforeAll(() => { prev = window.isHikiwake; window.isHikiwake = (t) => t === 'hikiwake'; });
  afterAll(() => { window.isHikiwake = prev; });

  it('is :empty (0 child nodes) when there is no draw/hantei mark → dash shows', () => {
    const { container } = render(
      <IndividualScore match={{ sideA: { name: 'A' }, sideB: { name: 'B' }, ipponsA: ['M'], ipponsB: [] }} variant="tv" showNames />
    );
    const vs = container.querySelector('.msb-vs');
    expect(vs).toBeTruthy();
    expect(vs.childNodes.length).toBe(0); // genuinely empty → :empty::before fires
  });

  it('is NOT empty on a draw (X child) → dash suppressed, no collision', () => {
    const { container } = render(
      <IndividualScore match={{ sideA: { name: 'A' }, sideB: { name: 'B' }, ipponsA: [], ipponsB: [], decision: 'hikiwake' }} variant="tv" showNames />
    );
    const vs = container.querySelector('.msb-vs');
    expect(vs.childNodes.length).toBeGreaterThan(0);
    expect(vs.textContent).toContain('X');
  });
});
