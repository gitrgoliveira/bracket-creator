// One tie, one banner (admin_pools.jsx). When every tie-breaker bout of a team
// league's tied group is finished and the tie is owed lots (a chusen candidate
// names the same teams), only the lots banner shows: the tie-breaker banner
// does not also ask to remove an unscored tie-breaker nobody can fight, or
// point to a panel above that it is repeating.

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

const PASSWORD = 'pw';
const comp = { id: 'c1', name: 'League Cup', format: 'league', kind: 'team', teamSize: 5, status: 'pools' };

const teams = [
  { id: 'id-gamma', name: 'Team Gamma', dojo: 'Dojo G' },
  { id: 'id-delta', name: 'Team Delta', dojo: 'Dojo D' },
  { id: 'id-eta', name: 'Team Eta', dojo: 'Dojo E' },
];
const group = {
  poolName: 'Pool A', teamNames: teams.map(t => t.name), teams, minPosition: 1, maxPosition: 3,
};

// A circle: Gamma beat Delta, Delta beat Eta, Eta beat Gamma, so the table is
// still level. Each bout carries a winner, so none is a drawn bout.
const pair = (n, a, b, status, winner) => ({
  id: `Pool A-DH-${n}`, sideA: a.name, sideB: b.name, sideAId: a.id, sideBId: b.id, status,
  winner: status === 'completed' ? winner.name : '',
});
const everyPair = (status) => [
  pair(1, teams[0], teams[1], status, teams[0]),
  pair(2, teams[0], teams[2], status, teams[2]),
  pair(3, teams[1], teams[2], status, teams[1]),
];

async function mount({ poolMatches, chusen }) {
  const api = {
    chusenCandidates: vi.fn().mockResolvedValue({ candidates: chusen, recorded: [] }),
    leagueTiebreakCandidates: vi.fn().mockResolvedValue({ candidates: [group] }),
    leagueTiebreakGenerate: vi.fn().mockResolvedValue({ matches: [] }),
    leagueTiebreakRemove: vi.fn().mockResolvedValue({ deleted: 0 }),
  };
  window.API = api;
  window.PoolsViewer = () => null;
  window.LeagueStandingsViewer = () => null;
  await act(async () => {
    render(
      <AdminPools
        c={comp}
        pools={[{ poolName: 'Pool A', players: [] }]}
        poolMatches={poolMatches}
        standings={{}}
        tweaks={{}}
        onEditScore={vi.fn()}
        password={PASSWORD}
      />
    );
  });
  await waitFor(() => expect(api.leagueTiebreakCandidates).toHaveBeenCalled());
  await waitFor(() => expect(api.chusenCandidates).toHaveBeenCalled());
  return api;
}

describe('one tie, one banner', () => {
  it('shows only the lots banner once every tie-breaker bout is finished and lots are owed', async () => {
    await mount({ poolMatches: everyPair('completed'), chusen: [group] });

    await screen.findByText('Chusen (drawing lots) required');
    await waitFor(() => expect(screen.queryByText('Tie-breaker required')).toBeNull());
    expect(screen.queryByRole('button', { name: 'Remove unscored tie-breaker' })).toBeNull();
    expect(screen.queryByText(/Draw lots in the panel above/)).toBeNull();
    expect(document.querySelectorAll('.league-tiebreak')).toHaveLength(1);
    // The league has no pools and a place reads in the ordinal form.
    expect(screen.getByText('1st to 3rd place')).toBeTruthy();
    expect(screen.queryByText(/Pool A/)).toBeNull();
    expect(screen.queryByText(/Positions?\s/)).toBeNull();
    // The rule is said at most once on screen, and not in the lots banner.
    expect(screen.queryAllByText(/needs a winner|needs a tie-breaker with a winner/)).toHaveLength(0);
  });

  it('keeps the tie-breaker banner while a bout is still to be fought, even with a lots banner for another tie', async () => {
    const other = { poolName: 'Pool A', teamNames: ['Team X', 'Team Y'], teams: [{ id: 'id-x', name: 'Team X' }, { id: 'id-y', name: 'Team Y' }], minPosition: 4 };
    await mount({ poolMatches: everyPair('scheduled'), chusen: [other] });

    await screen.findByText('Tie-breaker required');
    expect(screen.getByRole('button', { name: 'Remove unscored tie-breaker' })).toBeTruthy();
    expect(screen.getByText('1st to 3rd place')).toBeTruthy();
    // The rule is said once: in the tie-breaker banner, not again in the lots one.
    expect(screen.getAllByText(/needs a tie-breaker with a winner/)).toHaveLength(1);
    expect(screen.queryByText(/needs a winner/)).toBeNull();
  });
});
