// bc-tmfn: a scheduled match whose competitor is barred (withdrew earlier;
// ineligible_match.jsx) stays visible on every public schedule and pool
// list, with a small "Withdrawn" chip beside the barred competitor's name
// (barred_chip.jsx). This pins the chip on all four consuming surfaces:
// TWMatch (viewer_schedule.jsx), VSchedItem (viewer_match.jsx),
// PoolNumberedMatchRow (viewer_standings.jsx), and MatchCard (bracket.jsx).
//
// bracket.jsx is imported first in every case: it is the one module that
// sets the window.* globals (matchScoreStr, matchStateCell, boutMiddle,
// teamMatchMarks, …) the other three read at render time.
import React from 'react';
import { render, screen, cleanup } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterEach } from 'vitest';

let TWMatch, VSchedItem, PoolNumberedMatchRow, MatchCard;

beforeAll(async () => {
  await import('../../bracket.jsx');
  MatchCard = window.MatchCard;
  ({ TWMatch } = await import('../../viewer_schedule.jsx'));
  ({ VSchedItem } = await import('../../viewer_match.jsx'));
  ({ PoolNumberedMatchRow } = await import('../../viewer_standings.jsx'));
});

afterEach(() => cleanup());

const shiro = { id: 'p-shiro', name: 'Yama Competitor', number: 'K1' };
const aka = { id: 'p-aka', name: 'Umi Competitor', number: 'K2' };

describe('TWMatch: the Withdrawn chip beside a barred competitor', () => {
  it('shows the chip on the barred (shiro/sideB) side only', () => {
    const m = {
      id: 'p1', compId: 'c1', status: 'scheduled', scheduledAt: '10:00', phase: 'pool',
      sideA: aka, sideB: shiro,
      ineligibleSides: { b: 'kiken-voluntary' },
    };
    render(<TWMatch m={m} />);
    const chips = screen.getAllByTestId('barred-chip');
    expect(chips).toHaveLength(1);
    expect(chips[0].textContent).toBe('Withdrawn');
    // Rides in the shiro name cell (sideB), never the aka one.
    const [shiroCell, akaCell] = document.querySelectorAll('.tw-match__name');
    expect(shiroCell.contains(chips[0])).toBe(true);
    expect(akaCell.contains(chips[0])).toBe(false);
  });

  it('shows no chip on either side when nobody is barred', () => {
    const m = {
      id: 'p2', compId: 'c1', status: 'scheduled', scheduledAt: '10:00', phase: 'pool',
      sideA: aka, sideB: shiro,
    };
    render(<TWMatch m={m} />);
    expect(screen.queryByTestId('barred-chip')).toBeNull();
  });
});

describe('VSchedItem: the Withdrawn chip beside a barred competitor', () => {
  it('shows the chip on the barred (aka/sideA) side only', () => {
    const m = {
      id: 'p3', compId: 'c1', status: 'scheduled', scheduledAt: '10:00', phase: 'pool', court: 'A',
      sideA: aka, sideB: shiro,
      ineligibleSides: { a: 'fusenpai' },
    };
    render(<VSchedItem m={m} tweaks={{}} />);
    const chips = screen.getAllByTestId('barred-chip');
    expect(chips).toHaveLength(1);
    // Lives inside the aka side, not the shiro side.
    expect(document.querySelector('.vsched-item__side--aka').contains(chips[0])).toBe(true);
    expect(document.querySelector('.vsched-item__side--shiro').contains(chips[0])).toBe(false);
    // The centre middle mark stays "vs", never the chip's text.
    expect(document.querySelector('.vsched-item__vs').textContent).toBe('vs');
  });
});

describe('PoolNumberedMatchRow: the Withdrawn chip beside a barred competitor', () => {
  it('shows the chip on the barred (shiro/sideB) side only, centre stays vs', () => {
    const m = {
      id: 'Pool A-1', status: 'scheduled',
      sideA: aka, sideB: shiro,
      ineligibleSides: { b: 'kiken-injury' },
    };
    render(<PoolNumberedMatchRow m={m} num={1} />);
    const chips = screen.getAllByTestId('barred-chip');
    expect(chips).toHaveLength(1);
    expect(document.querySelector('.pool-match-numbered-row__side--shiro').contains(chips[0])).toBe(true);
    expect(document.querySelector('.pool-match-numbered-row__score').textContent).toBe('vs');
  });
});

describe('MatchCard: the Withdrawn chip rides in the results column, never the meta strip', () => {
  it('shows the chip on the barred side (sideA) and not on sideB', () => {
    const m = {
      id: 'b1', court: 'A', status: 'scheduled',
      sideA: aka, sideB: shiro,
      ineligibleSides: { a: 'kiken-voluntary' },
    };
    render(<MatchCard match={m} variant="1" />);
    const chips = screen.getAllByTestId('barred-chip');
    expect(chips).toHaveLength(1);
    // Inside .bc-side (the results column), never .bc-match-meta (the strip
    // that carries the closed middle-mark set: NOW / BYE / X / (E) / (DH)).
    expect(document.querySelector('.bc-match-meta').contains(chips[0])).toBe(false);
    expect(document.querySelector('.bc-side--a').contains(chips[0])).toBe(true);
    expect(document.querySelector('.bc-side--b').contains(chips[0])).toBe(false);
  });

  it('shows no chip for an ordinary scheduled match', () => {
    const m = { id: 'b2', court: 'A', status: 'scheduled', sideA: aka, sideB: shiro };
    render(<MatchCard match={m} variant="1" />);
    expect(screen.queryByTestId('barred-chip')).toBeNull();
  });
});
