import React from 'react';
import { readFileSync } from 'node:fs';
import { resolve, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { render, screen, waitFor } from '@testing-library/react';
import { describe, it, expect, beforeAll, beforeEach, afterEach } from 'vitest';
import { LeagueStandingsViewer } from '../../viewer_standings.jsx';

// A league whose tie for a place waits for its tie-breaker. The server orders
// the level rows by draw order (ranks 1, 2, 3) and marks them `tied`; the public
// League tab shares the group's best place on the amber tied row, with no
// explanatory line. Once the order is fought the ordinary ranks come back.

beforeAll(async () => {
  await import('../../bracket.jsx');
  await import('../../api_serializers.jsx');
});

const player = (id, name) => ({ id, name, dojo: `${name} Dojo` });
const row = (p, rank, f) => ({
  player: p, rank, wins: 1, losses: 1, draws: 0, ipponsGiven: 2, ipponsTaken: 2, points: 5, ...f,
});
const A = player('a', 'Aoki');
const B = player('b', 'Baba');
const C = player('c', 'Cho');
const D = player('d', 'Dan');
const competition = { id: 'lg', name: 'L', format: 'league', kind: 'individual', status: 'pools' };
const tweaks = { showDojo: false };

let savedAPI;
beforeEach(() => { savedAPI = window.API; });
afterEach(() => { window.API = savedAPI; });

async function renderLeague(standings) {
  window.API = { ...(savedAPI || {}), leagueStandings: () => Promise.resolve(standings) };
  const { container } = render(
    <LeagueStandingsViewer competition={competition} poolMatches={[]} tweaks={tweaks} highlightPlayers={[]} />,
  );
  await waitFor(() => expect(container.querySelector('table.pool__table tbody tr')).not.toBeNull());
  return [...container.querySelectorAll('table.pool__table tbody tr')].map((tr) => ({
    place: tr.querySelector('.pool-standings__draw-pos').textContent,
    tied: tr.classList.contains('pool__row--tied'),
  }));
}

describe('a league tie awaiting its tie-breaker', () => {
  it('shows the level rows sharing the best place on the amber tied row', async () => {
    const rows = await renderLeague([
      row(A, 1, { points: 9, wins: 2, losses: 0 }),
      row(B, 2, { tied: true }),
      row(C, 3, { tied: true }),
      row(D, 4, { points: 1 }),
    ]);
    expect(rows).toEqual([
      { place: '1', tied: false },
      { place: '2', tied: true },
      { place: '2', tied: true },
      { place: '4', tied: false },
    ]);
  });

  it('shares 1st across a three-way tie at the top, with no explanatory line', async () => {
    const level = { tied: true };
    const rows = await renderLeague([row(A, 1, level), row(B, 2, level), row(C, 3, level), row(D, 4, { points: 1 })]);
    expect(rows.map((r) => r.place)).toEqual(['1', '1', '1', '4']);
    expect(rows.slice(0, 3).every((r) => r.tied)).toBe(true);
    expect(screen.queryByText(/tie-break|tied|level/i)).toBeNull();
  });

  it('reads 1, 2, 3 once the tie is fought', async () => {
    const rows = await renderLeague([
      row(A, 1, { points: 9 }), row(C, 2, { points: 6 }), row(B, 3, { points: 5 }),
    ]);
    expect(rows).toEqual([
      { place: '1', tied: false }, { place: '2', tied: false }, { place: '3', tied: false },
    ]);
  });
});

// The league Awards podium holds four cards when a joint 3rd is on; at phone
// width it wraps (1st and 2nd a row each, the 3rds a pair) rather than scroll.
describe('the awards podium at phone width', () => {
  const css = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), '..', '..', '..', 'css', 'styles.css'), 'utf8');
  const phone = css.match(/@media \(max-width: 480px\) \{\s*\.podium \{[\s\S]*?\n\}\n/);

  it('wraps and lets every step shrink under 480px', () => {
    expect(phone).not.toBeNull();
    expect(phone[0]).toMatch(/\.podium \{[^}]*flex-wrap: wrap/);
    expect(phone[0]).toMatch(/\.podium \.podium-step \{[^}]*min-width: 0/);
    expect(phone[0]).toMatch(/\.podium \.podium-step--1,\s*\.podium \.podium-step--2 \{[^}]*flex-basis: 100%/);
  });

  it('keeps the phone rules inside .podium, so the champion-hero cards keep their heights', () => {
    // A selector here that does not start at .podium would strip the 130px/110px
    // step heights from the hero Awards cards, which share the step class.
    const selectors = phone[0].split('\n').filter((l) => /^\s+\.podium/.test(l) || /^\s+\.podium-step/.test(l));
    expect(selectors.length).toBeGreaterThan(0);
    expect(selectors.filter((l) => /^\s+\.podium-step/.test(l))).toEqual([]);
  });
});
