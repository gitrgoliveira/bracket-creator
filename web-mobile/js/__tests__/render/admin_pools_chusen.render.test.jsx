// bc-cse follow-up: the chusen (drawing lots) banner in admin_pools.jsx keyed
// every per-member input, validation slot and override-rank write by the
// team's bare display NAME. The server's GET /chusen-candidates response
// carries a `teams` array ({id, name, dojo}) positionally parallel to the
// legacy `teamNames` strings specifically so two members that share a display
// name -- reachable only via the documented enforcement hole in team-name
// uniqueness (an unreadable config.md write skips checkNewTeamNameCollisions,
// internal/mobileapp/handlers_competition.go) -- can still be told apart. The
// name-keyed banner defeated that end to end: both namesakes collapsed onto
// one `identityByName` entry, so the banner:
//   * could not validate a permutation for the group (entered.has() always
//     tripped on the second same-name row), blocking pool completion, and
//   * would have sent the SAME playerId twice had validation ever passed.
//
// This file pins the fix -- keying every one of those things by the member's
// INDEX in the group instead -- through the real component, driving the
// actual chusen fetch effect and the actual override-rank submit.

import React from 'react';
import { render, act, fireEvent, screen, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';

const originals = {};
let AdminPools;

beforeAll(async () => {
  const STUBBED_AT_LOAD = {
    // Captured at admin_pools.jsx module-eval time (`const X = window.X`
    // outside the component). EmptyState is published by ui.jsx, already
    // loaded by vitest.setup.render.js; ScoreEditorModal is not exercised by
    // these tests (the score modal is never opened) so a stub is enough to
    // avoid an undefined capture leaking into an unrelated assertion.
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

beforeEach(() => {
  // Rendered before the banner; a stub is enough since these tests only
  // assert on the chusen banner itself, not the standings grid.
  window.PoolsViewer = () => null;
});

const PASSWORD = 'pw';

function teamComp(overrides = {}) {
  return {
    id: 'c1',
    name: 'Team Cup',
    format: 'mixed',
    kind: 'team',
    teamSize: 5,
    status: 'pools',
    ...overrides,
  };
}

const onePool = [{ poolName: 'Pool A', players: [] }];

async function mountAdminPools({ api, comp = teamComp() } = {}) {
  window.API = api;
  let utils;
  await act(async () => {
    utils = render(
      <AdminPools
        c={comp}
        pools={onePool}
        poolMatches={[]}
        standings={{}}
        tweaks={{}}
        onEditScore={vi.fn()}
        password={PASSWORD}
      />
    );
  });
  // Flush the chusenCandidates fetch effect (window.API.chusenCandidates is a
  // resolved promise, so its `.then` runs on a microtask after mount).
  await waitFor(() => expect(api.chusenCandidates).toHaveBeenCalled());
  return utils;
}

// Two members sharing the display name "Ryu Kan" from different dojos: the
// scenario the enforcement hole allows through.
const samenameGroup = {
  poolName: 'Pool A',
  teamNames: ['Ryu Kan', 'Ryu Kan'],
  teams: [
    { id: 'team-1', name: 'Ryu Kan', dojo: 'Dojo North' },
    { id: 'team-2', name: 'Ryu Kan', dojo: 'Dojo South' },
  ],
  minPosition: 1,
};

const uniqueGroup = {
  poolName: 'Pool A',
  teamNames: ['Alpha', 'Beta'],
  teams: [
    { id: 'team-a', name: 'Alpha', dojo: 'Dojo A' },
    { id: 'team-b', name: 'Beta', dojo: 'Dojo B' },
  ],
  minPosition: 1,
};

function makeApi(candidates) {
  return {
    chusenCandidates: vi.fn().mockResolvedValue(candidates),
    overridePoolRank: vi.fn().mockResolvedValue(true),
  };
}

describe('AdminPools chusen banner: same-name members are kept apart by index/identity', () => {
  it('renders two independent rank inputs (distinct DOM ids) for a same-name pair', async () => {
    const api = makeApi([samenameGroup]);
    await mountAdminPools({ api });

    await screen.findByText('Chusen (drawing lots) required');
    const inputs = screen.getAllByRole('spinbutton');
    expect(inputs.length).toBe(2);
    expect(inputs[0].id).not.toBe(inputs[1].id);
    // Both labels legitimately read "Ryu Kan" (that's the whole point of the
    // scenario); what must differ is the underlying input identity, not the
    // visible text.
    expect(screen.getAllByText('Ryu Kan').length).toBe(2);
  });

  it('accepts a valid permutation entered across the two same-name rows (no validation error)', async () => {
    const api = makeApi([samenameGroup]);
    await mountAdminPools({ api });

    await screen.findByText('Chusen (drawing lots) required');
    const inputs = screen.getAllByRole('spinbutton');
    // Swap the defaults (1,2) to (2,1): distinct ranks, still a valid
    // permutation of {1,2}. Old name-keyed validation could never reach this
    // state for a same-name pair: `entered.has(val)` always tripped on the
    // second row because both rows read/write the same `${poolName}::${name}`
    // key.
    fireEvent.change(inputs[0], { target: { value: '2' } });
    fireEvent.change(inputs[1], { target: { value: '1' } });

    const recordBtn = screen.getByRole('button', { name: /Record chusen result/ });
    await act(async () => { fireEvent.click(recordBtn); });

    await waitFor(() => expect(api.overridePoolRank).toHaveBeenCalledTimes(2));
    // No validation error banner rendered for the group.
    expect(screen.queryByText(/Enter each of positions/)).toBeNull();
  });

  it('sends one overridePoolRank call per member, each with its OWN playerId/dojo', async () => {
    const api = makeApi([samenameGroup]);
    await mountAdminPools({ api });

    await screen.findByText('Chusen (drawing lots) required');
    const inputs = screen.getAllByRole('spinbutton');
    fireEvent.change(inputs[0], { target: { value: '2' } });
    fireEvent.change(inputs[1], { target: { value: '1' } });

    const recordBtn = screen.getByRole('button', { name: /Record chusen result/ });
    await act(async () => { fireEvent.click(recordBtn); });

    await waitFor(() => expect(api.overridePoolRank).toHaveBeenCalledTimes(2));
    const calls = api.overridePoolRank.mock.calls;
    // (compID, poolID, playerName, rank, password, playerId, playerDojo)
    expect(calls[0]).toEqual(['c1', 'Pool A', 'Ryu Kan', 2, PASSWORD, 'team-1', 'Dojo North']);
    expect(calls[1]).toEqual(['c1', 'Pool A', 'Ryu Kan', 1, PASSWORD, 'team-2', 'Dojo South']);
    // The two calls must not carry the same identity: that was the defect
    // (both rows resolved through one identityByName['Ryu Kan'] entry).
    expect(calls[0][5]).not.toBe(calls[1][5]);
  });
});

describe('AdminPools chusen banner: unique-name group regression guard', () => {
  it('still renders, validates and submits exactly as before for distinct names', async () => {
    const api = makeApi([uniqueGroup]);
    await mountAdminPools({ api });

    await screen.findByText('Chusen (drawing lots) required');
    expect(screen.getByText('Alpha')).toBeTruthy();
    expect(screen.getByText('Beta')).toBeTruthy();

    const inputs = screen.getAllByRole('spinbutton');
    expect(inputs.length).toBe(2);
    fireEvent.change(inputs[0], { target: { value: '2' } });
    fireEvent.change(inputs[1], { target: { value: '1' } });

    const recordBtn = screen.getByRole('button', { name: /Record chusen result/ });
    await act(async () => { fireEvent.click(recordBtn); });

    await waitFor(() => expect(api.overridePoolRank).toHaveBeenCalledTimes(2));
    const calls = api.overridePoolRank.mock.calls;
    expect(calls[0]).toEqual(['c1', 'Pool A', 'Alpha', 2, PASSWORD, 'team-a', 'Dojo A']);
    expect(calls[1]).toEqual(['c1', 'Pool A', 'Beta', 1, PASSWORD, 'team-b', 'Dojo B']);
  });

  it('still rejects an invalid permutation (regression guard on validation)', async () => {
    const api = makeApi([uniqueGroup]);
    await mountAdminPools({ api });

    await screen.findByText('Chusen (drawing lots) required');
    const inputs = screen.getAllByRole('spinbutton');
    // Both rows claim position 1: not a permutation of {1,2}.
    fireEvent.change(inputs[0], { target: { value: '1' } });
    fireEvent.change(inputs[1], { target: { value: '1' } });

    const recordBtn = screen.getByRole('button', { name: /Record chusen result/ });
    await act(async () => { fireEvent.click(recordBtn); });

    await screen.findByText(/Enter each of positions 1 to 2 exactly once/);
    expect(api.overridePoolRank).not.toHaveBeenCalled();
  });
});
