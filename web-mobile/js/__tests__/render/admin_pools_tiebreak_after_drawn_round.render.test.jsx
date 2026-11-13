// After a league daihyosen round in which every bout was saved as a draw, the
// tied group still shows in the "Tie-breaker required" banner, but every pair
// of it already holds a daihyosen row, which POST /league-tiebreak refuses
// with 409 tiebreak_matches_exist. The banner already withholds "Run
// tie-breaker" once every pair of the group holds a daihyosen row, so that
// button cannot reach the 409; this pins that branch, and that the drawn-bout
// notice ("Fight it again") shows as the way on. A group the server still owes
// bouts (a correction grew the tie, so only some pairs hold one) is offered
// "Run tie-breaker" again, which adds the missing pairs.

import React from 'react';
import { render, act, screen, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

let restoreGlobals;
let AdminPools;

beforeAll(async () => {
  restoreGlobals = installWindowStubs({ ScoreEditorModal: () => null });
  await import('../../admin_pools.jsx');
  AdminPools = window.AdminPools;
});

afterAll(() => restoreGlobals());

const comp = { id: 'c1', name: 'League Cup', format: 'league', kind: 'team', teamSize: 3, status: 'pools' };

const group = {
  poolName: 'Pool A',
  teamNames: ['Team Gamma', 'Team Delta'],
  teams: [
    { id: 'id-gamma', name: 'Team Gamma', dojo: 'Dojo G' },
    { id: 'id-delta', name: 'Team Delta', dojo: 'Dojo D' },
  ],
  minPosition: 1,
  maxPosition: 2,
};

const gamma = { id: 'id-gamma', name: 'Team Gamma', dojo: 'Dojo G' };
const delta = { id: 'id-delta', name: 'Team Delta', dojo: 'Dojo D' };

function dh(status) {
  return {
    id: 'Pool A-DH-0', status, winner: null,
    sideA: gamma, sideB: delta, sideAId: 'id-gamma', sideBId: 'id-delta',
  };
}

async function mount(poolMatches, tied = group) {
  const api = {
    chusenCandidates: vi.fn().mockResolvedValue({ candidates: [], recorded: [] }),
    leagueTiebreakCandidates: vi.fn().mockResolvedValue({ candidates: [tied], finalized: false }),
    leagueTiebreakGenerate: vi.fn().mockResolvedValue({ matches: [] }),
    leagueTiebreakRemove: vi.fn().mockResolvedValue({ deleted: 0 }),
    leagueTiebreakFinalize: vi.fn().mockResolvedValue({ finalized: true }),
  };
  window.API = api;
  window.PoolsViewer = () => null;
  const standings = {
    'Pool A': tied.teams.map(t => ({ player: t, points: 2, rank: 1 })),
  };
  await act(async () => {
    render(
      <AdminPools c={comp} pools={[{ poolName: 'Pool A', players: [] }]} poolMatches={poolMatches}
        standings={standings} tweaks={{}} onEditScore={vi.fn()} password="pw" />
    );
  });
  await waitFor(() => expect(screen.getByText('Tie-breaker required')).toBeTruthy());
  return api;
}

describe('AdminPools league tie-breaker banner after an all-drawn daihyosen round', () => {
  it('offers no "Run tie-breaker" for a group whose every pair already holds a daihyosen', async () => {
    await mount([dh('completed')]);
    expect(screen.queryByRole('button', { name: /Run tie-breaker/ })).toBeNull();
    expect(screen.getByTestId('drawn-tiebreak-notice')).toBeTruthy();
    expect(screen.getByRole('button', { name: /Fight it again/ })).toBeTruthy();
  });

  it('still offers "Run tie-breaker" before any daihyosen is generated', async () => {
    await mount([]);
    expect(screen.getByRole('button', { name: /Run tie-breaker/ })).toBeTruthy();
  });

  it('offers "Run tie-breaker" again when a grown tie holds a bout for only some of its pairs', async () => {
    const sigma = { id: 'id-sigma', name: 'Team Sigma', dojo: 'Dojo S' };
    const three = {
      ...group,
      teamNames: ['Team Gamma', 'Team Delta', 'Team Sigma'],
      teams: [...group.teams, sigma],
    };
    // Gamma v Delta was fought and drawn; Gamma v Sigma and Delta v Sigma are owed.
    const api = await mount([dh('completed')], three);
    expect(screen.getByRole('button', { name: /Run tie-breaker/ })).toBeTruthy();
    expect(screen.queryByRole('button', { name: /Remove unscored tie-breaker/ })).toBeNull();
    expect(api.leagueTiebreakGenerate).not.toHaveBeenCalled();
  });

  it('offers "Remove" only once every pair of a three-team group holds a bout', async () => {
    const sigma = { id: 'id-sigma', name: 'Team Sigma', dojo: 'Dojo S' };
    const three = {
      ...group,
      teamNames: ['Team Gamma', 'Team Delta', 'Team Sigma'],
      teams: [...group.teams, sigma],
    };
    const bout = (n, a, b) => ({
      id: `Pool A-DH-${n}`, status: 'scheduled', winner: null,
      sideA: a, sideB: b, sideAId: a.id, sideBId: b.id,
    });
    await mount([bout(0, gamma, delta), bout(1, gamma, sigma), bout(2, delta, sigma)], three);
    expect(screen.queryByRole('button', { name: /Run tie-breaker/ })).toBeNull();
    expect(screen.getByRole('button', { name: /Remove unscored tie-breaker/ })).toBeTruthy();
  });
});
