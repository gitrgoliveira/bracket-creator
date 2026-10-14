import React from 'react';
import { render } from '@testing-library/react';
import { describe, it, expect, beforeAll } from 'vitest';
import { PoolsViewer, tiedPlaces } from '../../viewer_standings.jsx';

// A pool whose second place is level on EVERY criterion between two
// competitors, with the tie-break bout still to be fought. The admin Pools tab
// and the public viewer render this same component, so one test covers both.
// The server sorts the level pair by draw order (ranks 2 and 3) and marks both
// `tied`; the table must not badge one of them 2nd and advancing.

beforeAll(async () => {
  await import('../../bracket.jsx');
  // api_serializers publishes it for the round-robin matrix under the table.
  await import('../../api_serializers.jsx');
});

const player = (id, name) => ({ id, name, dojo: `${name} Dojo` });
const A = player('a', 'Aoki');
const B = player('b', 'Baba');
const C = player('c', 'Cho');
const pools = [{ poolName: 'Pool A', players: [A, B, C] }];
const competition = { id: 'c', name: 'C', format: 'mixed', kind: 'individual', poolWinners: 2 };
const tweaks = { showDojo: false };

const row = (p, rank, f) => ({
  player: p, rank, wins: 0, losses: 1, draws: 1, ipponsGiven: 1, ipponsTaken: 2, points: 1, ...f,
});
const winner = row(A, 1, { wins: 2, losses: 0, draws: 0, ipponsGiven: 2, ipponsTaken: 0, points: 9 });

const bout = (id, sideA, sideB, status = 'completed') => ({ id, sideA, sideB, status });
const regular = [bout('Pool A-0', A, B), bout('Pool A-1', A, C), bout('Pool A-2', B, C)];

function renderPool(poolStandings, poolMatches) {
  const { container } = render(
    <PoolsViewer pools={pools} standings={{ 'Pool A': poolStandings }} poolMatches={poolMatches}
      competition={competition} tweaks={tweaks} highlightPlayers={[]} />,
  );
  const byName = {};
  container.querySelectorAll('table.pool__table tbody tr').forEach((tr) => {
    const badge = tr.querySelector('.rank-badge');
    const text = tr.querySelector('.pool__player-name').textContent;
    byName[[A, B, C].find((p) => text.includes(p.name)).name] = {
      badge: badge ? badge.textContent : '',
      badgeAdv: !!badge && badge.classList.contains('rank-badge--adv'),
      advancing: tr.classList.contains('advancing'),
      tied: tr.classList.contains('pool__row--tied'),
    };
  });
  return byName;
}

describe('a tie awaiting its tie-break bout', () => {
  it('shows both level rows at the same tied place, with no advancing mark', () => {
    const tbPending = [...regular, bout('Pool A-TB-1', B, C, 'scheduled')];
    const rows = renderPool([
      winner,
      row(B, 2, { tied: true }),
      row(C, 3, { tied: true }),
    ], tbPending);

    expect(rows.Aoki).toMatchObject({ badge: '1st', badgeAdv: true, advancing: true, tied: false });
    expect(rows.Baba).toMatchObject({ badge: '=2nd', badgeAdv: false, advancing: false, tied: true });
    expect(rows.Cho).toMatchObject({ badge: '=2nd', badgeAdv: false, advancing: false, tied: true });
  });

  it('brings the ordinary badges and the advancing mark back once the bout is scored', () => {
    const tbDone = [...regular, bout('Pool A-TB-1', B, C, 'completed')];
    const rows = renderPool([
      winner,
      row(C, 2, { points: 2 }),
      row(B, 3, { points: 1 }),
    ], tbDone);

    expect(rows.Aoki).toMatchObject({ badge: '1st', advancing: true, tied: false });
    expect(rows.Cho).toMatchObject({ badge: '2nd', badgeAdv: true, advancing: true, tied: false });
    expect(rows.Baba).toMatchObject({ badge: '3rd', badgeAdv: false, advancing: false, tied: false });
  });
});

describe('tiedPlaces', () => {
  const rows = (...r) => r;
  it('shares the best place across a group and says whether it straddles the cut', () => {
    const m = tiedPlaces(rows(winner, row(B, 2, { tied: true }), row(C, 3, { tied: true })), 2);
    expect(m.get('b')).toEqual({ place: 2, decidesAdvancement: true });
    expect(m.get('c')).toEqual({ place: 2, decidesAdvancement: true });
    expect(m.has('a')).toBe(false);
  });

  it('keeps the advancing mark for a tie that sits wholly inside the qualifying places', () => {
    const level = { points: 5, tied: true };
    const m = tiedPlaces(rows(row(A, 1, level), row(B, 2, level), row(C, 3, { points: 1 })), 2);
    expect(m.get('a')).toEqual({ place: 1, decidesAdvancement: false });
    expect(m.get('b')).toEqual({ place: 1, decidesAdvancement: false });
  });

  it('keeps two separate tied groups apart', () => {
    const D = player('d', 'Dan');
    const m = tiedPlaces(rows(
      row(A, 1, { points: 9, tied: true }), row(B, 2, { points: 9, tied: true }),
      row(C, 3, { points: 4, tied: true }), row(D, 4, { points: 4, tied: true }),
    ), 2);
    expect(m.get('a').place).toBe(1);
    expect(m.get('c').place).toBe(3);
    expect(m.get('d')).toEqual({ place: 3, decidesAdvancement: false });
  });
});
