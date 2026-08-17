import React from 'react';
import { render } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterAll } from 'vitest';
import { IndividualScore } from '../../match_scoreboard.jsx';
import { matchMiddleMark } from '../../bracket.jsx';

// The scoreboard's name cell owns two display rules that used to live outside
// it, each as a per-surface override reaching into these internals:
//
//   * the dojo second line — the viewer card built a VNode and passed it in as
//     a "name", with CSS scoped to .match-detail-card;
//   * the winner emphasis — the card bolded its OWN name row via
//     .match-detail-card__side--win, so every other surface rendering this row
//     marked an ordinary ippon win nowhere.
//
// Both now belong to the component, driven by props/state it already has. That
// is the same shape as the `.msb-sep { display: none }` bug: a rule about the
// shared row, encoded per surface, drifts the moment a second surface wants it.
describe('msb name cell (dojo line + winner emphasis)', () => {
  let prevMid, prevHik;
  beforeAll(() => {
    prevMid = window.matchMiddleMark; window.matchMiddleMark = matchMiddleMark;
    prevHik = window.isHikiwake; window.isHikiwake = (t) => t === 'hikiwake';
  });
  afterAll(() => { window.matchMiddleMark = prevMid; window.isHikiwake = prevHik; });

  const sides = {
    sideA: { id: 'pA', name: 'Alice', dojo: 'Kyoto Renmei' },
    sideB: { id: 'pB', name: 'Bob', dojo: 'Osaka Budokan' },
  };
  const shiroCell = (c) => c.querySelector('[data-testid="indiv-shiro-name"]');
  const akaCell = (c) => c.querySelector('[data-testid="indiv-aka-name"]');

  // Shiro is sideB, Aka is sideA.
  it('renders the dojo as a second line inside each name cell', () => {
    const { container } = render(
      <IndividualScore match={{ ...sides, ipponsA: ['M'], ipponsB: [] }} variant="card" showNames showDojo />
    );
    expect(shiroCell(container).querySelector('.msb-dojo').textContent).toBe('Osaka Budokan');
    expect(akaCell(container).querySelector('.msb-dojo').textContent).toBe('Kyoto Renmei');
    // The name still leads the cell: the dojo goes UNDER it, never replaces it.
    expect(shiroCell(container).textContent).toBe('BobOsaka Budokan');
  });

  it('gives the dojo the shared bc-dojo type rather than a fourth copy of it', () => {
    const { container } = render(
      <IndividualScore match={{ ...sides, ipponsA: [], ipponsB: [] }} variant="card" showNames showDojo />
    );
    expect(shiroCell(container).querySelector('.msb-dojo').className).toContain('bc-dojo');
  });

  // --stacked rides WITH the dojo instead of being selected per surface, so the
  // cell is multi-line exactly when it has a second line.
  it('marks only a cell that actually has a dojo as stacked', () => {
    const { container } = render(
      <IndividualScore match={{ sideA: { name: 'Winner of M1' }, sideB: sides.sideB, ipponsA: [], ipponsB: [] }}
        variant="card" showNames showDojo />
    );
    expect(shiroCell(container).className).toContain('msb-name--stacked');
    // A feeder side with no dojo gets the bare name and no empty line, so the
    // card does not pad unevenly.
    expect(akaCell(container).className).not.toContain('msb-name--stacked');
    expect(akaCell(container).querySelector('.msb-dojo')).toBeNull();
  });

  it('renders no dojo at all without the flag, leaving the TV boards single-line', () => {
    const { container } = render(
      <IndividualScore match={{ ...sides, ipponsA: [], ipponsB: [] }} variant="tv" showNames />
    );
    expect(container.querySelector('.msb-dojo')).toBeNull();
    expect(shiroCell(container).className).not.toContain('msb-name--stacked');
  });

  describe('winner emphasis', () => {
    // .msb-slots--win only fires for an ippon-LESS result (hantei, default
    // win), which is why an ordinary 2-0 needs this.
    it('marks the winner name on a plain ippon win', () => {
      const { container } = render(
        <IndividualScore match={{ ...sides, winner: { id: 'pA', name: 'Alice' }, ipponsA: ['M', 'K'], ipponsB: [] }}
          variant="card" showNames />
      );
      expect(akaCell(container).className).toContain('msb-name--win');
      expect(shiroCell(container).className).not.toContain('msb-name--win');
    });

    it('marks the Shiro side when Shiro won', () => {
      const { container } = render(
        <IndividualScore match={{ ...sides, winner: { id: 'pB', name: 'Bob' }, ipponsA: [], ipponsB: ['M'] }}
          variant="card" showNames />
      );
      expect(shiroCell(container).className).toContain('msb-name--win');
      expect(akaCell(container).className).not.toContain('msb-name--win');
    });

    it('marks neither side while the match is undecided', () => {
      const { container } = render(
        <IndividualScore match={{ ...sides, ipponsA: ['M'], ipponsB: [] }} variant="card" showNames />
      );
      expect(container.querySelectorAll('.msb-name--win')).toHaveLength(0);
    });

    // The winner key is id-first and is blanked for an indistinguishable
    // same-name pair, so neither side lights up rather than both — the same
    // rule centreMarks applies to the slot marks.
    it('marks neither side for a same-name pair with no ids', () => {
      const { container } = render(
        <IndividualScore
          match={{ sideA: { name: 'Alice' }, sideB: { name: 'Alice' }, winner: { name: 'Alice' },
                   ipponsA: [], ipponsB: [] }}
          variant="card" showNames />
      );
      expect(container.querySelectorAll('.msb-name--win')).toHaveLength(0);
    });
  });
});
