// Lots typed wrongly on a team league that has just finished stay correctable
// (decision F2 (a)): the server's AcceptsPoolRankOverride accepts a finished
// league, so the Pools page keeps asking for the chusen and keeps the recorded
// lots panel, with its Change control, on a finished team league. The change
// warns that the final table and podium move at once and the league stays
// finished, never the knockout sentence a pools + knockout competition gets.

import React from 'react';
import { render, act, fireEvent, screen, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

let restoreGlobals;
let AdminPools;

beforeAll(async () => {
  restoreGlobals = installWindowStubs({ ScoreEditorModal: () => null });
  await import('../../admin_pools.jsx');
  AdminPools = window.AdminPools;
});

afterAll(() => restoreGlobals());

beforeEach(() => {
  window.PoolsViewer = () => null;
  window.LeagueStandingsViewer = () => null;
});

const PASSWORD = 'pw';

const recordedGroup = {
  poolName: 'Pool A',
  teamNames: ['Alpha', 'Beta', 'Gamma'],
  teams: [
    { id: 'id-alpha', name: 'Alpha', dojo: 'Dojo A' },
    { id: 'id-beta', name: 'Beta', dojo: 'Dojo B' },
    { id: 'id-gamma', name: 'Gamma', dojo: 'Dojo G' },
  ],
  minPosition: 1,
  ranks: [2, 3, 1],
};

function leagueComp(overrides = {}) {
  return { id: 'c1', name: 'Team League', format: 'league', kind: 'team', teamSize: 3, status: 'complete', ...overrides };
}

function makeApi() {
  return {
    chusenCandidates: vi.fn().mockResolvedValue({ candidates: [], recorded: [recordedGroup] }),
    overridePoolRanks: vi.fn().mockResolvedValue(true),
    leagueTiebreakCandidates: vi.fn().mockResolvedValue({ candidates: [], finalized: false, accepted: [] }),
  };
}

async function mount(comp) {
  const api = makeApi();
  window.API = api;
  await act(async () => {
    render(
      <AdminPools
        c={comp}
        pools={[{ poolName: 'Pool A', players: [] }]}
        poolMatches={[]}
        standings={{}}
        tweaks={{}}
        onEditScore={vi.fn()}
        password={PASSWORD}
      />
    );
  });
  return api;
}

describe('AdminPools chusen: a finished team league keeps its lots correctable', () => {
  it('shows the recorded lots with Change, warns what a change does, and records the corrected order', async () => {
    const api = await mount(leagueComp());
    await waitFor(() => expect(api.chusenCandidates).toHaveBeenCalledWith('c1', PASSWORD));

    await screen.findByText('Chusen (drawing lots) recorded');
    expect(screen.getByText('Drawing lots: 1st Gamma, 2nd Alpha, 3rd Beta')).toBeTruthy();

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Change the drawing lots for Pool A' }));
    });
    expect(screen.getByText(/The final table and the podium change at once; the league stays finished/)).toBeTruthy();
    expect(screen.queryByText(/who qualifies/)).toBeNull();

    fireEvent.change(screen.getByLabelText('Alpha'), { target: { value: '1' } });
    fireEvent.change(screen.getByLabelText('Beta'), { target: { value: '2' } });
    fireEvent.change(screen.getByLabelText('Gamma'), { target: { value: '3' } });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /Record chusen result/ }));
    });
    await waitFor(() => expect(api.overridePoolRanks).toHaveBeenCalledTimes(1));
    expect(api.overridePoolRanks.mock.calls[0].slice(0, 3)).toEqual(['c1', 'Pool A', [
      { playerId: 'id-alpha', rank: 1 },
      { playerId: 'id-beta', rank: 2 },
      { playerId: 'id-gamma', rank: 3 },
    ]]);
  });

  // An individual league settles a tie by ippon-shobu, and only a round that
  // went in a circle takes lots, as the operator's other choice beside another
  // round (decision F5). Lots recorded over such a circle stay correctable on
  // the finished league, as a team league's do; no team "required" banner.
  it('keeps the recorded lots of a finished individual league correctable', async () => {
    const api = makeApi();
    api.chusenCandidates.mockResolvedValue({ candidates: [], recorded: [{
      poolName: 'Pool A',
      teamNames: ['Aki', 'Bea', 'Cal'],
      teams: [
        { id: 'id-aki', name: 'Aki', dojo: 'Dojo A' },
        { id: 'id-bea', name: 'Bea', dojo: 'Dojo B' },
        { id: 'id-cal', name: 'Cal', dojo: 'Dojo C' },
      ],
      minPosition: 1,
      ranks: [2, 3, 1],
    }] });
    window.API = api;
    await act(async () => {
      render(
        <AdminPools
          c={leagueComp({ name: 'Individual League', kind: 'individual', teamSize: 0 })}
          pools={[{ poolName: 'Pool A', players: [] }]}
          poolMatches={[]}
          standings={{}}
          tweaks={{}}
          onEditScore={vi.fn()}
          password={PASSWORD}
        />
      );
    });
    await waitFor(() => expect(api.chusenCandidates).toHaveBeenCalledWith('c1', PASSWORD));
    await screen.findByText('Chusen (drawing lots) recorded');
    expect(screen.getByText('Drawing lots: 1st Cal, 2nd Aki, 3rd Bea')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Change the drawing lots for Pool A' })).toBeTruthy();
    expect(screen.queryByText('Chusen (drawing lots) required')).toBeNull();
  });

  it('does not ask for the lots of a finished pools + knockout competition', async () => {
    const api = await mount(leagueComp({ format: 'mixed' }));
    expect(api.chusenCandidates).not.toHaveBeenCalled();
    expect(screen.queryByText('Chusen (drawing lots) recorded')).toBeNull();
  });
});
