import React from 'react';
import { render, screen } from '@testing-library/react';
import { describe, it, expect, beforeAll } from 'vitest';

// MatchCard (bracket.jsx) is the bracket / bronze-match card shared by the
// admin bracket view and the public viewer. Engi is the ONLY competition
// type where a completed match's per-side score is a NUMBER (a referee flag
// count, via FlagsA=sideA=Aka / FlagsB=sideB=Shiro); every other competition
// type (kendo, naginata) must keep showing the ippon LETTERS.

let MatchCard;

beforeAll(async () => {
  await import('../../bracket.jsx');
  MatchCard = window.MatchCard;
});

function baseMatch(overrides = {}) {
  return {
    id: 'm1',
    court: 'A',
    status: 'completed',
    sideA: { id: 'p-aka', name: 'Aka Competitor' },
    sideB: { id: 'p-shiro', name: 'Shiro Competitor' },
    winner: { id: 'p-aka', name: 'Aka Competitor' },
    ...overrides,
  };
}

describe('MatchCard per-side score: engi numbers vs. everyone else letters', () => {
  it('an engi match shows the flag COUNT for each side, not ippon letters', () => {
    render(<MatchCard match={baseMatch({ flagsA: 3, flagsB: 2, ipponsA: [], ipponsB: [] })} variant="1" />);
    // Aka (sideA) shows its own flag count "3"; Shiro (sideB) shows "2".
    expect(screen.getByText('3')).toBeTruthy();
    expect(screen.getByText('2')).toBeTruthy();
  });

  it('a non-engi (kendo) match still shows ippon letters, never a bare digit', () => {
    render(<MatchCard match={baseMatch({ ipponsA: ['M', 'K'], ipponsB: ['D'] })} variant="1" />);
    expect(screen.getByText('MK')).toBeTruthy();
    expect(screen.getByText('D')).toBeTruthy();
    expect(screen.queryByText('3')).toBeNull();
  });

  it('a naginata match (Sune letter) still shows letters, not numbers', () => {
    render(<MatchCard match={baseMatch({ ipponsA: ['S'], ipponsB: ['M'] })} variant="1" />);
    expect(screen.getByText('S')).toBeTruthy();
    expect(screen.getByText('M')).toBeTruthy();
  });

  it('an unscored engi side (0 flags) still renders "0", not blank', () => {
    render(<MatchCard match={baseMatch({ flagsA: 5, flagsB: 0, ipponsA: [], ipponsB: [] })} variant="1" />);
    expect(screen.getByText('5')).toBeTruthy();
    expect(screen.getByText('0')).toBeTruthy();
  });
});
