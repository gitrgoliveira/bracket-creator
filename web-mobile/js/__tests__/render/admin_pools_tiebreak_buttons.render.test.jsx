// The league-tiebreak "Run tie-breaker" / "Remove unscored tie-breaker"
// buttons (admin_pools.jsx, ~460-560) call groupTeamIds(group.teams, names)
// to build the teamIds array the server now REQUIRES (operator ruling
// bc-pnum: a tied group is selected by id only). Before this fix, an
// id-less tied group (a legacy roster, or one predating this field) still
// rendered an ENABLED button; clicking it sent a request with no teamIds at
// all, which the server rejects with 400 -- a dead end the operator had no
// way to see coming from the UI. This pins the fix: both buttons are
// disabled, with a one-line hint, whenever groupTeamIds(...) is undefined,
// and enabled normally when every team in the group carries an id.

import React from 'react';
import { render, act, screen, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';

const originals = {};
let AdminPools;

beforeAll(async () => {
  const STUBBED_AT_LOAD = {
    ScoreEditorModal: () => null,
  };
  for (const [k, v] of Object.entries(STUBBED_AT_LOAD)) {
    originals[k] = { had: k in window, value: window[k] };
    window[k] = v;
  }
  await import('../../admin_pools.jsx');
  AdminPools = window.AdminPools;
});

afterAll(() => {
  for (const [k, orig] of Object.entries(originals)) {
    if (orig.had) window[k] = orig.value;
    else delete window[k];
  }
});

const PASSWORD = 'pw';

function teamLeagueComp(overrides = {}) {
  return {
    id: 'c1',
    name: 'League Cup',
    format: 'league',
    kind: 'team',
    teamSize: 5,
    status: 'pools',
    ...overrides,
  };
}

function makeApi({ candidates, finalized = false }) {
  return {
    // No chusen ties for these fixtures; isTeamComp is also true for a team
    // league, so this effect runs too and must resolve to something benign.
    chusenCandidates: vi.fn().mockResolvedValue([]),
    leagueTiebreakCandidates: vi.fn().mockResolvedValue({ candidates, finalized }),
    leagueTiebreakGenerate: vi.fn().mockResolvedValue({ matches: [] }),
    leagueTiebreakRemove: vi.fn().mockResolvedValue({ deleted: 0 }),
    leagueTiebreakFinalize: vi.fn().mockResolvedValue({ finalized: true }),
  };
}

async function mountAdminPools({ api, comp = teamLeagueComp() } = {}) {
  window.API = api;
  window.PoolsViewer = () => null;
  let utils;
  await act(async () => {
    utils = render(
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
  await waitFor(() => expect(api.leagueTiebreakCandidates).toHaveBeenCalled());
  return utils;
}

const idLessGroup = {
  poolName: 'Pool A',
  teamNames: ['Team Alpha', 'Team Beta'],
  // No `teams` array at all -- the shape a legacy candidates payload (or one
  // predating this field) sends. groupTeamIds(undefined, names) -> undefined.
  minPosition: 1,
  maxPosition: 2,
};

const idFullGroup = {
  poolName: 'Pool A',
  teamNames: ['Team Gamma', 'Team Delta'],
  teams: [
    { id: 'id-gamma', name: 'Team Gamma', dojo: 'Dojo G' },
    { id: 'id-delta', name: 'Team Delta', dojo: 'Dojo D' },
  ],
  minPosition: 1,
  maxPosition: 2,
};

describe('AdminPools league-tiebreak buttons: id-less tied group', () => {
  it('disables "Run tie-breaker" and shows a hint instead of letting the request 400', async () => {
    const api = makeApi({ candidates: [idLessGroup] });
    await mountAdminPools({ api });

    const btn = await screen.findByRole('button', { name: /Run tie-breaker/ });
    expect(btn.disabled).toBe(true);
    expect(screen.getByText(/has no id in the pool draw/)).toBeTruthy();
  });

  it('never calls leagueTiebreakGenerate when the button is disabled', async () => {
    const api = makeApi({ candidates: [idLessGroup] });
    await mountAdminPools({ api });

    const btn = await screen.findByRole('button', { name: /Run tie-breaker/ });
    // A disabled button does not dispatch a click handler in the DOM (jsdom
    // honours the disabled attribute for click dispatch), so this asserts
    // the guard is real, not merely cosmetic.
    btn.click();
    expect(api.leagueTiebreakGenerate).not.toHaveBeenCalled();
  });
});

describe('AdminPools league-tiebreak buttons: fully id-stamped tied group', () => {
  it('leaves "Run tie-breaker" enabled and shows no hint', async () => {
    const api = makeApi({ candidates: [idFullGroup] });
    await mountAdminPools({ api });

    const btn = await screen.findByRole('button', { name: /Run tie-breaker/ });
    expect(btn.disabled).toBe(false);
    expect(screen.queryByText(/has no id in the pool draw/)).toBeNull();
  });

  it('calls leagueTiebreakGenerate with the resolved teamIds on click', async () => {
    const api = makeApi({ candidates: [idFullGroup] });
    await mountAdminPools({ api });

    const btn = await screen.findByRole('button', { name: /Run tie-breaker/ });
    await act(async () => { btn.click(); });
    await waitFor(() => expect(api.leagueTiebreakGenerate).toHaveBeenCalled());
    expect(api.leagueTiebreakGenerate).toHaveBeenCalledWith(
      'c1', ['Team Gamma', 'Team Delta'], PASSWORD, ['id-gamma', 'id-delta']
    );
  });
});
