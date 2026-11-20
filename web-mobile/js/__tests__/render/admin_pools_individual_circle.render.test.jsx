// An individual tie a tie-breaker round left level (A beat B, B beat C, C beat
// A): the Pools tab names the tie, its main button runs another
// tie-breaker round among the ones still level, and drawing lots is the
// operator's other choice, opening the same entry a team's chusen uses.

import React from 'react';
import { render, act, fireEvent, screen, waitFor, cleanup } from '@testing-library/react';
import { NO_ID_POOL_HINT } from '../../data_integrity.jsx';
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
    leagueTiebreakCandidates: vi.fn().mockResolvedValue({ candidates: [] }),
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
    // A league has no pools: the tie is named by its places alone.
    expect(screen.getByText('1st to 3rd place')).toBeTruthy();
    expect(screen.queryByText(/Pool A/)).toBeNull();
    expect(screen.getByText('Aki, Bea and Cal')).toBeTruthy();
    // Not the team wording, and the lots are not open until chosen.
    expect(screen.queryByText('Chusen (drawing lots) required')).toBeNull();
    expect(screen.queryByRole('button', { name: /Record chusen result/ })).toBeNull();

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Run another round' }));
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
      fireEvent.click(screen.getByRole('button', { name: 'Run another round' }));
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

  it('offers one round per run still level, each naming its competitors beside a one-line button', async () => {
    const four = [...members, { id: 'id-dai', name: 'Dai', dojo: 'Dojo D' }];
    const group = {
      poolName: 'Pool A', teamNames: four.map(m => m.name), teams: four, minPosition: 1,
      stillLevel: [[four[0], four[1]], [four[2], four[3]]],
    };
    const api = await mount(leagueComp(), makeApi([group]));
    await screen.findByText('1st to 4th place');
    const buttons = screen.getAllByRole('button', { name: 'Run another round' });
    expect(buttons).toHaveLength(2);
    // The names ride in a sibling the button is described by, never in its label.
    expect(buttons[1].textContent).toBe('Run another round');
    const names = document.getElementById(buttons[1].getAttribute('aria-describedby'));
    expect(names.textContent.trim()).toBe('Cal and Dai');
    expect(buttons[1].contains(names)).toBe(false);
    await act(async () => {
      fireEvent.click(buttons[1]);
    });
    await waitFor(() => expect(api.runTiebreakerRound).toHaveBeenCalledWith('c1', 'Pool A', ['id-cal', 'id-dai'], PASSWORD));
  });

  // A tie-breaker whose two sides both withdrew earlier has no new round: the
  // server lists the tie with no run still level. The other way on is fighting
  // the drawn bout again, from its own notice (the drawn-bout notice), so the
  // lots entry opens at once beside it, and neither is forced.
  it('opens the lots at once, with no round to run, when nobody can fight the tie-breaker', async () => {
    const barred = { poolName: 'Pool A', teamNames: ['Aki', 'Bea'], teams: members.slice(0, 2), minPosition: 1, stillLevel: null };
    const api = await mount(leagueComp(), makeApi([barred]));
    await screen.findByText('A tie-breaker round left a tie');

    expect(screen.getByText(/one per competitor/)).toBeTruthy();
    expect(screen.getByText(/needs a winner/)).toBeTruthy();
    expect(screen.queryByRole('button', { name: /Run another round/ })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Draw lots instead' })).toBeNull();

    fireEvent.change(screen.getByLabelText('Aki'), { target: { value: '2' } });
    fireEvent.change(screen.getByLabelText('Bea'), { target: { value: '1' } });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /Record chusen result/ }));
    });
    await waitFor(() => expect(api.overridePoolRanks).toHaveBeenCalledTimes(1));
    expect(api.runTiebreakerRound).not.toHaveBeenCalled();
  });

  const barred = { poolName: 'Pool A', teamNames: ['Aki', 'Bea'], teams: members.slice(0, 2), minPosition: 1, stillLevel: null };

  it('names the cause of the tie: who won as many bouts, or that the fighters withdrew earlier, never both', async () => {
    await mount(leagueComp());
    let notice = (await screen.findByText('A tie-breaker round left a tie')).closest('.league-tiebreak');
    expect(notice.textContent).toContain('Aki, Bea and Cal each won as many tie-breaker bouts as the others.');
    expect(notice.textContent).toContain('Run another round among them, or draw lots instead.');
    expect(notice.textContent).not.toMatch(/withdrew/);
    expect(notice.textContent).not.toMatch(/teams/);
    cleanup();

    await mount(leagueComp(), makeApi([barred]));
    notice = (await screen.findByText('A tie-breaker round left a tie')).closest('.league-tiebreak');
    expect(notice.textContent).toContain('The competitors to fight the tie-breaker withdrew earlier. Fight it again from its notice above, or draw lots and enter the order below.');
    expect(notice.textContent).not.toMatch(/no round can be fought/);
    expect(notice.textContent).not.toMatch(/won as many/);
    expect(notice.textContent).not.toMatch(/teams/);
    cleanup();

    // Two competitors level have one other, not "the others".
    const two = { ...barred, stillLevel: [members.slice(0, 2)] };
    await mount(leagueComp(), makeApi([two]));
    notice = (await screen.findByText('A tie-breaker round left a tie')).closest('.league-tiebreak');
    expect(notice.textContent).toContain('Aki and Bea each won as many tie-breaker bouts as the other. Run another round');
  });

  it('speaks of pairs for an engi competition whose fighters withdrew earlier', async () => {
    await mount(leagueComp({ engi: true }), makeApi([barred]));
    const notice = (await screen.findByText('A tie-breaker round left a tie')).closest('.league-tiebreak');
    expect(notice.textContent).toContain('The pairs to fight the tie-breaker withdrew earlier');
  });

  it('keeps the pool name for a pools competition and drops it for a league', async () => {
    await mount(leagueComp({ format: 'mixed' }));
    expect(await screen.findByText('Pool A, 1st to 3rd place')).toBeTruthy();
  });

  it('states the pool rule in a pools competition and the league rule in a league', async () => {
    await mount(leagueComp({ format: 'mixed' }));
    const pool = (await screen.findByText('A tie-breaker round left a tie')).closest('.league-tiebreak');
    expect(pool.textContent).toContain('this tie decides who goes through or where they are seated');
    expect(pool.textContent).not.toContain('joint 3rd places');
    cleanup();
    await mount(leagueComp());
    const league = (await screen.findByText('A tie-breaker round left a tie')).closest('.league-tiebreak');
    expect(league.textContent).toContain('a tie at 1st or 2nd place (and at 3rd place when joint 3rd places are off) needs a winner');
    expect(league.textContent).not.toContain('goes through or where they are seated');
  });

  it('shows the lots without a header of their own, spaced from the buttons, and Cancel closes them', async () => {
    await mount(leagueComp());
    await screen.findByText('A tie-breaker round left a tie');
    expect(screen.queryByRole('button', { name: 'Cancel' })).toBeNull();

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Draw lots instead' }));
    });
    const notice = screen.getByText('A tie-breaker round left a tie').closest('.league-tiebreak');
    // One tie, one header: the entry repeats neither the places nor the names.
    expect(notice.querySelectorAll('.league-tiebreak__group-header')).toHaveLength(1);
    const entry = screen.getByText(/one per competitor/).closest('.league-tiebreak__group');
    expect(entry.classList.contains('league-tiebreak__group--embedded')).toBe(true);

    fireEvent.change(screen.getByLabelText('Aki'), { target: { value: '3' } });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    });
    expect(screen.queryByText(/one per competitor/)).toBeNull();
    expect(screen.getByRole('button', { name: 'Draw lots instead' })).toBeTruthy();

    // Opened again, the order typed before the Cancel is gone.
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Draw lots instead' }));
    });
    expect(screen.getByLabelText('Aki').value).toBe('1');
  });

  it('gives no Cancel where the lots entry opens at once (no round to run)', async () => {
    await mount(leagueComp(), makeApi([barred]));
    await screen.findByText(/one per competitor/);
    expect(screen.queryByRole('button', { name: 'Cancel' })).toBeNull();
  });

  it('disables a round whose competitor has no id and says why, as the lots do', async () => {
    const idless = { ...circle, teams: [members[0], members[1], { name: 'Cal', dojo: 'Dojo C' }], stillLevel: [[members[0], members[1], { name: 'Cal', dojo: 'Dojo C' }]] };
    await mount(leagueComp(), makeApi([idless]));
    const btn = await screen.findByRole('button', { name: 'Run another round' });
    expect(btn.disabled).toBe(true);
    expect(btn.parentElement.textContent).toContain(NO_ID_POOL_HINT);
  });

  it('says a refused round without a status code when the server gave no sentence', async () => {
    const api = makeApi();
    api.runTiebreakerRound.mockRejectedValue(new Error(''));
    await mount(leagueComp(), api);
    await screen.findByText('A tie-breaker round left a tie');
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Run another round' }));
    });
    expect(await screen.findByText('Could not add the round. Check the connection and try again.')).toBeTruthy();
  });

  it('keeps the team chusen banner for a team competition', async () => {
    await mount(leagueComp({ kind: 'team', teamSize: 3 }));
    await screen.findByText('Chusen (drawing lots) required');
    expect(screen.queryByText('A tie-breaker round left a tie')).toBeNull();
    expect(screen.getByText(/one per team/)).toBeTruthy();
  });
});
