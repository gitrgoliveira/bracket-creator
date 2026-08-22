import React from 'react';
import { render, screen } from '@testing-library/react';
import { describe, it, expect, beforeAll } from 'vitest';
import { PoolsViewer } from '../../viewer_standings.jsx';

// THE AUDIENCE GATE.
//
// The admin console and the public viewer render this SAME component off the
// SAME public payload, so the server cannot tell the two apart: the flag it
// reveals travels on a payload the console itself reads, and stripping it
// server-side would blank it for the operator too (and make it vanish again on
// the next SSE broadcast). The whole gate is therefore the showDataIssues prop,
// which only the admin callers pass. If that stops working, a data-integrity
// warning appears on a spectator's phone at a live event.

beforeAll(async () => {
  // bracket.jsx publishes window.matchStateCell, which every match row calls.
  await import('../../bracket.jsx');
});

const pools = [{
  poolName: 'Pool A',
  players: [{ id: 'p1', name: 'Kenshikan' }, { id: 'p2', name: 'Sanshukan' }],
}];
const poolMatches = [{
  id: 'Pool A-1',
  sideA: { id: 'p1', name: 'Kenshikan' },
  sideB: { id: 'p2', name: 'Sanshukan' },
  status: 'completed',
  winner: { id: 'p1', name: 'Kenshikan' },
  // The server flagged this encounter: its sub-bout cell would not parse.
  subResultsUnreadable: true,
}];
const competition = { id: 'c', name: 'C', format: 'mixed', kind: 'team', teamSize: 3 };
const standings = { 'Pool A': [] };

const NOTICE = /individual bouts could not be read/;
const POOL_NOTICE = /IV and PW columns below are incomplete/;

describe('PoolsViewer data-integrity notices', () => {
  it('shows them on an operator surface, which opts in', () => {
    render(<PoolsViewer pools={pools} standings={standings} poolMatches={poolMatches}
      competition={competition} tweaks={{}} highlightPlayers={[]} showDataIssues />);
    expect(screen.getByText(NOTICE)).toBeTruthy();
    expect(screen.getByText(POOL_NOTICE)).toBeTruthy();
  });

  it('shows NOTHING on the public viewer, which does not', () => {
    render(<PoolsViewer pools={pools} standings={standings} poolMatches={poolMatches}
      competition={competition} tweaks={{}} highlightPlayers={[]} />);
    expect(screen.queryByText(NOTICE)).toBeNull();
    expect(screen.queryByText(POOL_NOTICE)).toBeNull();
    // The match itself still renders: the encounter is not hidden, only the
    // operator-facing explanation of what is missing from it.
    expect(screen.getAllByText("Kenshikan").length).toBeGreaterThan(0);
  });

  it('shows nothing when every cell parsed, even on the operator surface', () => {
    const clean = [{ ...poolMatches[0], subResultsUnreadable: false }];
    render(<PoolsViewer pools={pools} standings={standings} poolMatches={clean}
      competition={competition} tweaks={{}} highlightPlayers={[]} showDataIssues />);
    expect(screen.queryByText(NOTICE)).toBeNull();
    expect(screen.queryByText(POOL_NOTICE)).toBeNull();
  });
});
