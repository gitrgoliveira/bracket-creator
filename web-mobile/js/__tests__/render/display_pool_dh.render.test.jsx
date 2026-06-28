import React from 'react';
import { render } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import { TvDisplay } from '../../display.jsx';

// A pool daihyosen / tiebreaker rep bout ("Pool X-DH-N" / "Pool X-TB-N") is a
// single INDIVIDUAL ippon-shobu even in a TEAM competition (its score lives at
// the match top level, not in subResults). TvDisplay must route it to the
// individual scoreboard, NOT the 5-person team grid (which would render empty)
// and NOT the whole-pool individual feed (which is for genuine individual
// comps). Mirrors the admin scorer's compKind override in admin_pools.jsx.
describe('TvDisplay : pool DH/TB rep bout in a team comp renders as individual', () => {
  function teamCompWithSupplementary(suppId) {
    return {
      name: 'Team Cup', kind: 'team', teamSize: 5, withZekkenName: false,
      poolMatches: [
        { id: 'Pool A-0', court: 'A', status: 'completed',
          sideA: { name: 'Red Dojo' }, sideB: { name: 'White Dojo' }, subResults: [] },
        { id: suppId, court: 'A', status: 'running',
          sideA: { name: 'Taro' }, sideB: { name: 'Jiro' }, ipponsA: ['M'], ipponsB: [] },
      ],
      bracket: { rounds: [] },
    };
  }

  it('routes a running Pool A-DH-0 bout to the individual scoreboard, not the team grid', () => {
    const { container } = render(
      <TvDisplay court="A" tournament={{ name: 'Cup' }} competitions={[teamCompWithSupplementary('Pool A-DH-0')]} connected />
    );
    expect(container.querySelector('[data-testid="individual-score"]')).toBeTruthy();
    expect(container.querySelector('[data-testid="tvd-team-bouts"]')).toBeNull();
    expect(container.textContent).toContain('Taro');
    expect(container.textContent).toContain('Jiro');
  });

  it('does the same for a Pool A-TB-0 tiebreaker bout', () => {
    const { container } = render(
      <TvDisplay court="A" tournament={{ name: 'Cup' }} competitions={[teamCompWithSupplementary('Pool A-TB-0')]} connected />
    );
    expect(container.querySelector('[data-testid="individual-score"]')).toBeTruthy();
    expect(container.querySelector('[data-testid="tvd-team-bouts"]')).toBeNull();
  });

  it('a regular team encounter still renders the team grid', () => {
    const comp = {
      name: 'Team Cup', kind: 'team', teamSize: 5, withZekkenName: false,
      poolMatches: [
        { id: 'Pool A-0', court: 'A', status: 'running',
          sideA: { name: 'Red Dojo' }, sideB: { name: 'White Dojo' },
          subResults: [{ position: 1, ipponsA: ['M'], ipponsB: [] }] },
      ],
      bracket: { rounds: [] },
    };
    const { container } = render(
      <TvDisplay court="A" tournament={{ name: 'Cup' }} competitions={[comp]} connected />
    );
    expect(container.querySelector('[data-testid="tvd-team-bouts"]')).toBeTruthy();
  });
});
