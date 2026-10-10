// The winner tick's screen-reader words follow the tick's place in the row.
// Before the name (Shiro on Recent results, the bracket card, the queue row)
// the words are "Winner: "; after the name (Aka on Recent results, where the
// tick sits at the outer edge) they are ", winner", so the row is spoken as
// "Aka: Tanaka, winner" and not "Aka: Tanaka Winner:".
import React from 'react';
import { render } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterAll, vi } from 'vitest';

let WinnerTick, VSchedItem, normalizeMatch;

beforeAll(async () => {
  window.matchScoreStr = vi.fn(() => '');
  ({ WinnerTick } = await import('../../side_cell.jsx'));
  ({ normalizeMatch } = await import('../../api_serializers.jsx'));
  ({ VSchedItem } = await import('../../viewer_match.jsx'));
});

afterAll(() => {
  delete window.matchScoreStr;
});

// What a screen reader would say for an element: its text with the
// aria-hidden glyphs left out.
function spoken(el) {
  const copy = el.cloneNode(true);
  copy.querySelectorAll('[aria-hidden="true"]').forEach((n) => n.remove());
  return copy.textContent;
}

function completedBout(winner) {
  return normalizeMatch({
    id: 'm-recent-1',
    status: 'completed',
    court: 'A',
    phase: 'bracket',
    round: 'Final',
    sideA: 'Tanaka',
    sideB: 'Sato',
    winner,
  }, {});
}

describe('WinnerTick sr-only words follow the tick position', () => {
  it('a tick after the name reads ", winner"', () => {
    const { container } = render(<span>Tanaka<WinnerTick trailing /></span>);
    expect(container.querySelector('.sr-only').textContent).toBe(', winner');
    expect(spoken(container.firstChild)).toBe('Tanaka, winner');
  });

  it('a tick before the name keeps "Winner: "', () => {
    const { container } = render(<span><WinnerTick />Tanaka</span>);
    expect(container.querySelector('.sr-only').textContent).toBe('Winner: ');
    expect(spoken(container.firstChild)).toBe('Winner: Tanaka');
  });

  it('Recent results: Aka (tick after the name) reads ", winner"; Shiro keeps "Winner: "', () => {
    const akaWins = render(<VSchedItem m={completedBout('Tanaka')} tweaks={{}} winnerTick />);
    const aka = akaWins.container.querySelector('.vsched-item__side--aka');
    expect(aka.querySelector('.bc-winner-tick .sr-only').textContent).toBe(', winner');
    expect(spoken(aka)).toBe('Aka: Tanaka, winner');
    akaWins.unmount();

    const shiroWins = render(<VSchedItem m={completedBout('Sato')} tweaks={{}} winnerTick />);
    const shiro = shiroWins.container.querySelector('.vsched-item__side--shiro');
    expect(shiro.querySelector('.bc-winner-tick .sr-only').textContent).toBe('Winner: ');
    expect(spoken(shiro)).toBe('Shiro: Winner: Sato');
  });
});
