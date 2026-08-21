import React from 'react';
import { render, screen } from '@testing-library/react';
import { describe, it, expect, beforeAll } from 'vitest';

// MatchCard (bracket.jsx) builds each side's score by joining that side's
// ippon letters with its result mark (Ht / Kiken / Fus.). Since the "Ht"
// judges'-decision mark became a real entry inside the ippon array itself
// (ipponsFromScore tokenizes "MHt" -> ["M","Ht"]), the letters MUST be
// stripped of any embedded "Ht" via realIppons before the mark is
// re-appended - otherwise a hantei match doubles the mark ("MHt Ht" or,
// for a 0-0 hantei, "Ht Ht"). matchScoreStr (the shared score-string path)
// already does this; this pins the MatchCard side-score path too.

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
    decidedByHantei: true,
    sideA: { id: 'p-aka', name: 'Aka Competitor' },
    sideB: { id: 'p-shiro', name: 'Shiro Competitor' },
    winner: { id: 'p-aka', name: 'Aka Competitor' },
    ...overrides,
  };
}

describe('MatchCard per-side score: hantei mark is never doubled', () => {
  it('a 1-1 hantei match (winner already carries "Ht" in its ippons) renders "M Ht", never "MHt Ht"', () => {
    render(<MatchCard match={baseMatch({ ipponsA: ['M', 'Ht'], ipponsB: ['K'] })} variant="1" />);
    expect(screen.getByText('M Ht')).toBeTruthy();
    expect(screen.queryByText('MHt Ht')).toBeNull();
    expect(screen.getByText('K')).toBeTruthy();
  });

  it('a 0-0 hantei match renders a bare "Ht", never "Ht Ht"', () => {
    render(<MatchCard match={baseMatch({ ipponsA: ['Ht'], ipponsB: [] })} variant="1" />);
    expect(screen.getByText('Ht')).toBeTruthy();
    expect(screen.queryByText('Ht Ht')).toBeNull();
  });
});
