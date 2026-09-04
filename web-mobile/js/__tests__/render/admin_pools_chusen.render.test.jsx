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
// bc-cse keyed every one of those things by the member's INDEX in the group
// instead. bc-appx item 2 replaced index-keying with keying by the member's
// stable IDENTITY (chusenMemberKey, delegating to window.checkinPid): the
// `teams` array is "the still-tied members in current standings order"
// (engine.ChusenCandidates), and that order reorders after ANY partial write
// (a member carrying a rank override sorts ahead of one without, regardless
// of the override's value), so an index-keyed input silently re-attached the
// operator's typed value to the WRONG team on a retry after a mid-loop
// failure. This file pins the CURRENT (identity-keyed) fix through the real
// component, driving the actual chusen fetch effect and the actual
// override-rank submit; identity-keying still gives the same-name pair (and
// each group's members) distinct keys, so the assertions below hold either
// way and continue to guard the same end-to-end behaviour.

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

describe('AdminPools chusen banner: two tied groups in the same pool stay independent', () => {
  // A pool can hold more than one unresolved tied group at once (e.g. a
  // cycle at 1st/2nd AND a separate cycle at 3rd/4th -- PoolWinners has no
  // upper bound). Each input is keyed on `${groupKey}::${identity}`
  // (groupKey = "${poolName}::${minPosition}", identity = chusenMemberKey,
  // bc-appx item 2), so two same-pool groups never collide: their members
  // carry distinct identities, and groupKey additionally scopes the
  // busy/error state and the post-submit clear to exactly the group that
  // was submitted.
  const groupOne = {
    poolName: 'Pool A',
    teamNames: ['Alpha', 'Beta'],
    teams: [
      { id: 'team-a', name: 'Alpha', dojo: 'Dojo A' },
      { id: 'team-b', name: 'Beta', dojo: 'Dojo B' },
    ],
    minPosition: 1,
  };
  const groupTwo = {
    poolName: 'Pool A',
    teamNames: ['Gamma', 'Delta'],
    teams: [
      { id: 'team-g', name: 'Gamma', dojo: 'Dojo G' },
      { id: 'team-d', name: 'Delta', dojo: 'Dojo D' },
    ],
    minPosition: 3,
  };

  it("typing in group 1's inputs does not change group 2's effective values", async () => {
    const api = makeApi([groupOne, groupTwo]);
    await mountAdminPools({ api });

    await screen.findByText('Chusen (drawing lots) required');
    const inputs = screen.getAllByRole('spinbutton');
    expect(inputs.length).toBe(4);
    // DOM order follows candidate order: group 1's two rows, then group 2's.
    const [g1a, g1b, g2a, g2b] = inputs;
    expect(g2a.value).toBe('3');
    expect(g2b.value).toBe('4');
    expect(g1a.id).not.toBe(g2a.id);
    expect(g1b.id).not.toBe(g2b.id);

    fireEvent.change(g1a, { target: { value: '99' } });
    fireEvent.change(g1b, { target: { value: '98' } });

    // Group 1's and group 2's members carry different identities
    // (chusenMemberKey), so this edit cannot leak into group 2's
    // still-untouched rows regardless of groupKey scoping.
    expect(g2a.value).toBe('3');
    expect(g2b.value).toBe('4');
  });

  it("submitting group 1 sends only its own members and does not wipe group 2's in-progress edits", async () => {
    const api = makeApi([groupOne, groupTwo]);
    await mountAdminPools({ api });

    await screen.findByText('Chusen (drawing lots) required');
    const inputs = screen.getAllByRole('spinbutton');
    const [g1a, g1b, g2a, g2b] = inputs;

    // Group 1: swap its own valid permutation (1,2) -> (2,1) and submit it.
    fireEvent.change(g1a, { target: { value: '2' } });
    fireEvent.change(g1b, { target: { value: '1' } });
    // Group 2: swap its own valid permutation (3,4) -> (4,3), left UNSAVED.
    fireEvent.change(g2a, { target: { value: '4' } });
    fireEvent.change(g2b, { target: { value: '3' } });

    const recordButtons = screen.getAllByRole('button', { name: /Record chusen result/ });
    expect(recordButtons.length).toBe(2);
    await act(async () => { fireEvent.click(recordButtons[0]); }); // group 1's button

    await waitFor(() => expect(api.overridePoolRank).toHaveBeenCalledTimes(2));
    const calls = api.overridePoolRank.mock.calls;
    expect(calls[0]).toEqual(['c1', 'Pool A', 'Alpha', 2, PASSWORD, 'team-a', 'Dojo A']);
    expect(calls[1]).toEqual(['c1', 'Pool A', 'Beta', 1, PASSWORD, 'team-b', 'Dojo B']);
    // Group 2 must never appear in the write: it was not submitted.
    expect(calls.some((c) => c[2] === 'Gamma' || c[2] === 'Delta')).toBe(false);

    // Group 1's row is now gone (optimistically removed on success); only
    // group 2's row remains. Its inputs must still show the operator's
    // unsaved (4, 3) edit, not a reset to (3, 4): the post-submit clear
    // deletes `${groupKey}::${identity}` for exactly group 1's own members
    // (chusenMemberKey), never group 2's -- their identities differ, so
    // nothing group 2 reads is touched.
    const remaining = screen.getAllByRole('spinbutton');
    expect(remaining.length).toBe(2);
    expect(remaining[0].value).toBe('4');
    expect(remaining[1].value).toBe('3');
  });
});
