// An individual tie a tie-breaker round left level (A beat B, B beat C, C beat
// A; decision F5): the Pools tab names the tie, its main button runs another
// tie-breaker round among the ones still level, and drawing lots is the
// operator's other choice, opening the same entry a team's chusen uses.

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

const members = [
  { id: 'id-aki', name: 'Aki', dojo: 'Dojo A' },
  { id: 'id-bea', name: 'Bea', dojo: 'Dojo B' },
  { id: 'id-cal', name: 'Cal', dojo: 'Dojo C' },
];

const circle = {
  poolName: 'Pool A',
  teamNames: ['Aki', 'Bea', 'Cal'],
  teams: members,
  minPosition: 1,
  stillLevel: [members],
};

function leagueComp(overrides = {}) {
  return { id: 'c1', name: 'Individual League', format: 'league', kind: 'individual', teamSize: 0, status: 'pools', ...overrides };
}

function makeApi(candidates = [circle]) {
  return {
    chusenCandidates: vi.fn().mockResolvedValue({ candidates, recorded: [] }),
    runTiebreakerRound: vi.fn().mockResolvedValue({ matches: [] }),
    overridePoolRanks: vi.fn().mockResolvedValue(true),
    leagueTiebreakCandidates: vi.fn().mockResolvedValue({ candidates: [], finalized: false, accepted: [] }),
  };
}

async function mount(comp, api = makeApi()) {
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

describe('AdminPools: an individual tie-breaker round that went in a circle', () => {
  it('names the tie and runs another tie-breaker round among the ones still level', async () => {
    const api = await mount(leagueComp());
    await waitFor(() => expect(api.chusenCandidates).toHaveBeenCalledWith('c1', PASSWORD));

    await screen.findByText('A tie-breaker round left a tie');
    expect(screen.getByText('Pool A, 1st to 3rd place')).toBeTruthy();
    expect(screen.getByText('Aki · Bea · Cal')).toBeTruthy();
    // Not the team wording, and the lots are not open until chosen.
    expect(screen.queryByText('Chusen (drawing lots) required')).toBeNull();
    expect(screen.queryByRole('button', { name: /Record chusen result/ })).toBeNull();

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Run another tie-breaker round' }));
    });
    await waitFor(() => expect(api.runTiebreakerRound).toHaveBeenCalledTimes(1));
    expect(api.runTiebreakerRound).toHaveBeenCalledWith('c1', 'Pool A', ['id-aki', 'id-bea', 'id-cal'], PASSWORD);
    expect(api.overridePoolRanks).not.toHaveBeenCalled();
  });

  it('says a refused round on the tie', async () => {
    const api = makeApi();
    api.runTiebreakerRound.mockRejectedValue(new Error('That tie is no longer level.'));
    await mount(leagueComp(), api);
    await screen.findByText('A tie-breaker round left a tie');
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Run another tie-breaker round' }));
    });
    expect(await screen.findByText('That tie is no longer level.')).toBeTruthy();
  });

  it('opens the lots only when the operator chooses them, and records them for each competitor', async () => {
    const api = await mount(leagueComp());
    await screen.findByText('A tie-breaker round left a tie');

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Draw lots instead' }));
    });
    expect(screen.getByText(/one per competitor/)).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Draw lots instead' })).toBeNull();

    fireEvent.change(screen.getByLabelText('Aki'), { target: { value: '3' } });
    fireEvent.change(screen.getByLabelText('Bea'), { target: { value: '1' } });
    fireEvent.change(screen.getByLabelText('Cal'), { target: { value: '2' } });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /Record chusen result/ }));
    });
    await waitFor(() => expect(api.overridePoolRanks).toHaveBeenCalledTimes(1));
    expect(api.overridePoolRanks.mock.calls[0].slice(0, 3)).toEqual(['c1', 'Pool A', [
      { playerId: 'id-aki', rank: 3 },
      { playerId: 'id-bea', rank: 1 },
      { playerId: 'id-cal', rank: 2 },
    ]]);
    expect(api.runTiebreakerRound).not.toHaveBeenCalled();
  });

  it('offers one round per run still level, each naming its competitors', async () => {
    const four = [...members, { id: 'id-dai', name: 'Dai', dojo: 'Dojo D' }];
    const group = {
      poolName: 'Pool A', teamNames: four.map(m => m.name), teams: four, minPosition: 1,
      stillLevel: [[four[0], four[1]], [four[2], four[3]]],
    };
    const api = await mount(leagueComp(), makeApi([group]));
    await screen.findByText('Pool A, 1st to 4th place');
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Run another tie-breaker round: Cal · Dai' }));
    });
    await waitFor(() => expect(api.runTiebreakerRound).toHaveBeenCalledWith('c1', 'Pool A', ['id-cal', 'id-dai'], PASSWORD));
  });

  it('keeps the team chusen banner for a team competition', async () => {
    await mount(leagueComp({ kind: 'team', teamSize: 3 }));
    await screen.findByText('Chusen (drawing lots) required');
    expect(screen.queryByText('A tie-breaker round left a tie')).toBeNull();
    expect(screen.getByText(/one per team/)).toBeTruthy();
  });
});
