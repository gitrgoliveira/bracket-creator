import React from 'react';
import { render } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import { TvWhiteBoard } from '../../display.jsx';

// bc-cse (fix #4): the TV headline (TvWhiteBoard, display_scoreboard.jsx)
// wraps a NumberedName clip element beside a Kiken/Fus. mark inside a
// block-ellipsis div -- a long Shiro name used to clip the trailing mark off
// the end. See viewer_match.jsx's VSchedItem comment for the mechanism; this
// pins the same structural fix (msb-name--labelled) landed on this host.
describe('TV headline: the name mark survives clipping (bc-cse #4)', () => {
  const base = {
    tournament: { name: 'Cup' }, court: 'A', connected: true,
    lineupA: null, lineupB: null, showDH: false, queueMatches: [], zekken: false,
  };

  // Shiro (the long name) withdrew (decisionBy "shiro"), crediting Aka the
  // Kiken mark's COMPLEMENT -- Shiro carries the "Kiken" loser mark.
  function promotedWithdrawn() {
    return {
      match: {
        id: 'm1', status: 'completed', decision: 'kiken-voluntary', decisionBy: 'shiro',
        sideA: { name: 'Aoki Dojo', number: 'T1' },
        sideB: { name: 'A Very Long Shiro Team Name That Should Clip', number: 'T2' },
        winner: { name: 'Aoki Dojo' },
        subResults: [{ position: 1 }],
      },
      competition: { id: 'c1', name: 'Team Cup', kind: 'team', teamSize: 3, format: 'mixed' },
      isBracket: false,
    };
  }

  it('the headline div carries msb-name--labelled, and the mark is a real sb-result-mark sibling', () => {
    const p = promotedWithdrawn();
    const props = { ...base, promoted: p, isTeamMatch: true, subResults: p.match.subResults, teamSize: 3 };
    const { container } = render(<TvWhiteBoard {...props} />);

    const mark = container.querySelector('.sb-result-mark');
    expect(mark).toBeTruthy();
    expect(mark.textContent).toBe('Kiken');

    const labelled = mark.closest('.msb-name--labelled');
    expect(labelled).toBeTruthy();

    // The mark is a sibling, not appended into the name's own text node.
    const nameText = labelled.querySelector('.numbered-name__text');
    expect(nameText).toBeTruthy();
    expect(nameText.textContent).toContain('A Very Long Shiro Team Name');
    expect(nameText.textContent).not.toContain('Kiken');
  });
});
