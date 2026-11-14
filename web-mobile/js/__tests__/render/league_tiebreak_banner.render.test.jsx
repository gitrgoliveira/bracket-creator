// The team league's tie-break banner (admin_pools.jsx).
//
// Pins the operator-facing wording and flow of the tie rule, word for word as
// scripts/e2e/fixtures/league.mjs reads it:
//   * The banner speaks of a place that counts, says a tie at 1st or 2nd place
//     (and at 3rd with joint 3rd places off) needs a tie-breaker with a winner,
//     and offers Run tie-breaker: there is no way to accept shared ranks, and no
//     Undo for it either.
//   * The banner stays up, with Remove, while a group holds a tie-breaker bout
//     nobody has fought.
//   * A fought round that went in a circle points to the lots in the chusen
//     panel; a tie-breaker whose sides both withdrew earlier lands in the same
//     panel, with copy that names it.

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

const comp = (overrides = {}) => ({
  id: 'c1', name: 'League Cup', format: 'league', kind: 'team', teamSize: 5, status: 'pools', ...overrides,
});

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

const dhBout = (overrides = {}) => ({
  id: 'Pool A-DH-1', sideA: 'Team Gamma', sideB: 'Team Delta', sideAId: 'id-gamma', sideBId: 'id-delta',
  status: 'scheduled', winner: '', ...overrides,
});

function makeApi({ answer, chusen } = {}) {
  return {
    chusenCandidates: vi.fn().mockResolvedValue(chusen || { candidates: [], recorded: [] }),
    leagueTiebreakCandidates: vi.fn().mockResolvedValue(answer || { candidates: [group] }),
    leagueTiebreakGenerate: vi.fn().mockResolvedValue({ matches: [] }),
    leagueTiebreakRemove: vi.fn().mockResolvedValue({ deleted: 0 }),
  };
}

async function mountAdminPools({ api, c = comp(), poolMatches = [] }) {
  window.API = api;
  window.PoolsViewer = () => null;
  window.LeagueStandingsViewer = () => null;
  await act(async () => {
    render(
      <AdminPools
        c={c}
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
}

describe('the tie-break banner copy', () => {
  it('speaks of a place that counts, asks for a tie-breaker with a winner and offers no shared ranks', async () => {
    await mountAdminPools({ api: makeApi() });

    const banner = (await screen.findByText('Tie-breaker required')).closest('.league-tiebreak');
    const text = banner.textContent;
    expect(text).not.toMatch(/qualif/i);
    expect(text).not.toMatch(/finalis/i);
    expect(text).toContain('level on every criterion for a place that counts');
    expect(text).toContain('A tie at 1st or 2nd place (and at 3rd place when joint 3rd places are off) needs a tie-breaker with a winner.');
    expect(screen.getByRole('button', { name: 'Run tie-breaker' })).toBeTruthy();
    expect(text).not.toMatch(/shared ranks/i);
    expect(screen.queryByRole('button', { name: /shared ranks/i })).toBeNull();
    expect(screen.queryByTestId('league-shared-ranks')).toBeNull();
  });

  it('keeps the banner up with Remove while a tie-breaker bout waits, and no Accept beside it', async () => {
    await mountAdminPools({ api: makeApi(), poolMatches: [dhBout()] });

    expect(await screen.findByRole('button', { name: 'Remove unscored tie-breaker' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: /shared ranks/i })).toBeNull();
  });

  it('points a fought round that went in a circle to the lots, not to shared ranks', async () => {
    await mountAdminPools({ api: makeApi(), poolMatches: [dhBout({ status: 'completed', winner: 'Team Gamma' })] });

    const banner = (await screen.findByText('Tie-breaker required')).closest('.league-tiebreak');
    expect(banner.textContent).toContain('Every tie-breaker bout is finished and these teams are still level. Draw lots in the panel above.');
    expect(banner.textContent).not.toMatch(/shared ranks/i);
  });
});

describe('the lots panel for a tie the bouts cannot settle', () => {
  it('names a tie at 1st or 2nd needing a winner, and a tie-breaker whose teams withdrew earlier', async () => {
    const api = makeApi({
      answer: { candidates: [] },
      chusen: { candidates: [group], recorded: [] },
    });
    await mountAdminPools({ api });

    const panel = (await screen.findByText('Chusen (drawing lots) required')).closest('.league-tiebreak');
    expect(panel.textContent).toContain('A tie at 1st or 2nd place (and at 3rd place when joint 3rd places are off) needs a winner');
    expect(panel.textContent).toContain('the teams to fight it withdrew earlier');
    expect(panel.textContent).not.toMatch(/shared ranks/i);
    expect(screen.getByRole('button', { name: 'Record chusen result' })).toBeTruthy();
  });
});
