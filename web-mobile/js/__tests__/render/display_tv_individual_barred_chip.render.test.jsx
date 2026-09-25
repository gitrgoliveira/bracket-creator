import React from 'react';
import { render } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import { TvDisplay } from '../../display.jsx';

// bc-cse: every phone schedule row (VSchedItem, TWMatch, viewer_standings)
// shows the "Withdrawn" chip beside a barred competitor's name on a
// still-scheduled match. The TV individual pool board (TvIndividualBoard)
// listed the same row as a plain scheduled match with no chip at all --
// IndividualScore's showBarredChip prop (match_scoreboard.jsx) closes that
// gap, reusing barred_chip.jsx exactly as the phone rows do.
describe('TvIndividualBoard: a scheduled barred bout shows the Withdrawn chip (bc-cse)', () => {
  function comp() {
    return {
      id: 'c1', name: 'Kendo Cup', kind: 'individual', teamSize: 0, withZekkenName: false,
      poolMatches: [
        { id: 'Pool A-0', court: 'A', status: 'running',
          sideA: { name: 'Aoki Taro' }, sideB: { name: 'Endo Goro' }, ipponsA: ['M'], ipponsB: [] },
        { id: 'Pool A-1', court: 'A', status: 'scheduled',
          sideA: { name: 'Sato Ken' }, sideB: { name: 'Ito Rei' },
          ineligibleSides: { b: 'kiken-voluntary' } },
      ],
      bracket: { rounds: [] },
    };
  }

  it('shows a barred-chip on the scheduled barred row', () => {
    const { container } = render(
      <TvDisplay court="A" tournament={{ name: 'Cup' }} competitions={[comp()]} connected />
    );
    expect(container.querySelectorAll('[data-testid="barred-chip"]').length).toBe(1);
    expect(container.textContent).toContain('Withdrawn');
  });

  it('shows no chip when nobody is barred', () => {
    const c = comp();
    c.poolMatches[1].ineligibleSides = undefined;
    const { container } = render(
      <TvDisplay court="A" tournament={{ name: 'Cup' }} competitions={[c]} connected />
    );
    expect(container.querySelectorAll('[data-testid="barred-chip"]').length).toBe(0);
  });
});
