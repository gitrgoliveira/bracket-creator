// The chusen (drawing lots) banner in admin_pools.jsx has been through three
// keying schemes for its per-member rank inputs, validation slots and
// override-rank writes:
//
//   1. bare display NAME (bc-cse's original bug). The server's GET
//      /chusen-candidates response carries a `teams` array ({id, name, dojo})
//      positionally parallel to the legacy `teamNames` strings specifically
//      so two members that share a display name -- reachable only via the
//      documented enforcement hole in team-name uniqueness (an unreadable
//      config.md write skips checkNewTeamNameCollisions,
//      internal/mobileapp/handlers_competition.go) -- can still be told
//      apart. Name-keying defeated that: both namesakes collapsed onto one
//      `identityByName` entry, so validation could never accept a
//      permutation for the group, and a passed validation would have sent
//      the SAME playerId twice.
//   2. the member's INDEX in the group (bc-cse's fix). Separates a same-name
//      pair correctly, but the group order is the server's live standings
//      sort, which reorders after ANY partial write (a member carrying a
//      rank override sorts ahead of one without, regardless of the
//      override's value) -- so a mid-loop write failure followed by a
//      re-fetch can return the SAME group with its members permuted, and an
//      index-keyed retry silently re-attaches the operator's typed value to
//      the WRONG team (bc-appx item 2/3's finding).
//   3. the member's stable IDENTITY (chusenMemberKey: id when non-empty,
//      else "name|dojo") -- the fix in step 3, which survives both a
//      same-name pair and a reorder. Its own first cut had a further gap:
//      delegating to window.checkinPid, whose `p.id ?? fallback` treats only
//      a null/undefined id as absent, not the empty string a legacy/UUID-less
//      roster's "id" field actually is on the wire -- so every legacy member
//      in a group collapsed onto the SAME key (bc-appx item 1's blocker),
//      which chusenMemberKey worked around by staying self-contained rather
//      than delegating.
//   4. checkinPid itself, fixed and delegated to directly (M12).
//      chusenMemberKey's workaround duplicated the id-else-"name|dojo" rule
//      already meant to live in ONE place (helper.CompetitorKey server-side,
//      checkinPid client-side); checkinPid's own `p.id ?? fallback` is now
//      `p.id ? p.id : fallback` (a truthy check, not `??`), closing the empty-
//      string gap at the source, and chusenMemberKey was deleted so the
//      banner imports checkinPid instead of keeping a second copy of the rule.
//      (Separately, M11 changed the per-row DOM id to `idx`-based rather than
//      identity-based -- a DOM id only needs to be unique within one render,
//      unlike inputKey/chusenInputs, which must survive a re-fetch reorder --
//      so the "distinct DOM ids" assertions below no longer exercise the
//      identity collision on their own; the "does not move"/"submits all
//      three" assertions in the legacy-members block do.)
//
// Each describe block below pins one part of that history against the CURRENT
// code:
//   * "same-name members are kept apart by identity" -- pins step 3/4 closing
//     the #1 defect (a same-name pair still gets distinct keys).
//   * "unique-name group regression guard" -- pins that an ordinary,
//     non-colliding group is unaffected by any of the above.
//   * "two tied groups in the same pool stay independent" -- pins groupKey
//     scoping, orthogonal to the per-member keying scheme.
//   * "a mid-loop failure reorders the group" -- pins step 3/4 closing the #2
//     defect (the reorder reproduction).
//   * "legacy (UUID-less) members" -- pins the #3/#4 blocker fix (bc-appx
//     item 1 / M12): three empty-id members must not collapse onto one row,
//     now enforced by checkinPid itself rather than a self-contained
//     workaround.

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

describe('AdminPools chusen banner: a mid-loop failure reorders the group (bc-appx item 2/3)', () => {
  // Three DISTINCT ids (not the legacy/empty-id scenario below): this block
  // pins identity-keying surviving a REORDER, not the id-collision blocker.
  const initialGroup = {
    poolName: 'Pool A',
    teamNames: ['Alpha', 'Beta', 'Gamma'],
    teams: [
      { id: 'id-alpha', name: 'Alpha', dojo: 'Dojo A' },
      { id: 'id-beta', name: 'Beta', dojo: 'Dojo B' },
      { id: 'id-gamma', name: 'Gamma', dojo: 'Dojo G' },
    ],
    minPosition: 1,
  };
  // Same group, same pool/minPosition (so groupKey is unchanged -- the real
  // server keeps a still-tied group's boundary stable across a reorder;
  // detectPoolTies groups by Points equality, and a rank override only
  // changes ORDER within that block), but Alpha and Beta have swapped
  // places: a member carrying ANY override sorts ahead of one without,
  // regardless of the override's value (engine/scoring.go), which is exactly
  // what a partially-succeeded write produces.
  const reorderedGroup = {
    ...initialGroup,
    teamNames: ['Beta', 'Alpha', 'Gamma'],
    teams: [
      { id: 'id-beta', name: 'Beta', dojo: 'Dojo B' },
      { id: 'id-alpha', name: 'Alpha', dojo: 'Dojo A' },
      { id: 'id-gamma', name: 'Gamma', dojo: 'Dojo G' },
    ],
  };

  it("keeps each team's typed rank attached to it after the group reorders", async () => {
    const api = {
      // First call (mount) returns the original order; the second call (the
      // catch block's re-fetch, triggered below) returns the reordered one.
      chusenCandidates: vi.fn()
        .mockResolvedValueOnce([initialGroup])
        .mockResolvedValueOnce([reorderedGroup]),
      // The FIRST overridePoolRank call in the submit loop -- Alpha's, since
      // Alpha is members[0] in the order at the moment the operator clicks --
      // rejects, simulating the mid-loop write failure. The loop is
      // sequential and stops at the first rejection, so Beta's and Gamma's
      // writes are never attempted.
      overridePoolRank: vi.fn().mockRejectedValueOnce(new Error('conflict')),
    };
    await mountAdminPools({ api });
    await screen.findByText('Chusen (drawing lots) required');

    // Type a distinct, valid permutation of {1,2,3} by TEAM, not by row
    // position, using getByLabelText so this assertion is itself
    // identity-based (Alpha's own label, wherever its row currently sits).
    fireEvent.change(screen.getByLabelText('Alpha'), { target: { value: '3' } });
    fireEvent.change(screen.getByLabelText('Beta'), { target: { value: '1' } });
    fireEvent.change(screen.getByLabelText('Gamma'), { target: { value: '2' } });

    const recordBtn = screen.getByRole('button', { name: /Record chusen result/ });
    await act(async () => { fireEvent.click(recordBtn); });

    // The submit failed on Alpha's write; only that one call was attempted.
    await waitFor(() => expect(api.overridePoolRank).toHaveBeenCalledTimes(1));
    // The failure handler re-fetches candidates; wait for that SECOND call
    // (the reordered payload) to land and the component to re-render.
    await waitFor(() => expect(api.chusenCandidates).toHaveBeenCalledTimes(2));

    // Each team's own typed value must still show under ITS OWN label,
    // regardless of which DOM row it now occupies.
    await waitFor(() => {
      expect(screen.getByLabelText('Alpha').value).toBe('3');
      expect(screen.getByLabelText('Beta').value).toBe('1');
      expect(screen.getByLabelText('Gamma').value).toBe('2');
    });
  });
});

describe('AdminPools chusen banner: legacy (UUID-less) members share an empty id (bc-appx item 1)', () => {
  // All three members carry id: "" -- exactly what the chusen-candidates
  // handler emits for a competitor with no UUID (handlers_competition.go:
  // `gin.H{"id": t.Player.ID, ...}`), NOT null/undefined. Names/dojos are
  // distinct so the roster itself is legal (no duplicate row).
  const legacyGroup = {
    poolName: 'Pool A',
    teamNames: ['Alpha', 'Beta', 'Gamma'],
    teams: [
      { id: '', name: 'Alpha', dojo: 'Dojo A' },
      { id: '', name: 'Beta', dojo: 'Dojo B' },
      { id: '', name: 'Gamma', dojo: 'Dojo G' },
    ],
    minPosition: 1,
  };

  it('renders three independent rows (distinct DOM ids), not one collapsed onto id ""', async () => {
    const api = makeApi([legacyGroup]);
    await mountAdminPools({ api });

    await screen.findByText('Chusen (drawing lots) required');
    const inputs = screen.getAllByRole('spinbutton');
    expect(inputs.length).toBe(3);
    // Since M11 the per-row DOM id is `idx`-based (chusen-${groupKey}-${idx}),
    // so this assertion alone no longer exercises the empty-id identity
    // collision (idx guarantees distinctness on its own regardless of
    // checkinPid). The identity guard for these three empty-id members is
    // pinned by the next two tests, which key off chusenInputs/checkinPid
    // (name|dojo fallback), not the DOM id.
    expect(new Set(inputs.map((el) => el.id)).size).toBe(3);
  });

  it("typing into one legacy member's row does not move the other two", async () => {
    const api = makeApi([legacyGroup]);
    await mountAdminPools({ api });

    await screen.findByText('Chusen (drawing lots) required');
    const alphaInput = screen.getByLabelText('Alpha');
    const betaInput = screen.getByLabelText('Beta');
    const gammaInput = screen.getByLabelText('Gamma');

    // Distinct defaults before any edit (1, 2, 3 in candidate order).
    expect(alphaInput.value).toBe('1');
    expect(betaInput.value).toBe('2');
    expect(gammaInput.value).toBe('3');

    // '9' deliberately matches none of the three defaults (1, 2, 3), so a
    // collapse onto Beta's or Gamma's row would be unambiguous either way.
    fireEvent.change(alphaInput, { target: { value: '9' } });

    // A key collapsed onto "" makes every row read/write the SAME
    // chusenInputs entry, so editing Alpha's row would also change Beta's
    // and Gamma's DISPLAYED value to '9' (the exact "typing in one input
    // moves all three" symptom the reviewer reproduced).
    expect(alphaInput.value).toBe('9');
    expect(betaInput.value).toBe('2');
    expect(gammaInput.value).toBe('3');
  });

  it('accepts the shown defaults (1,2,3) as a valid permutation and submits all three', async () => {
    const api = makeApi([legacyGroup]);
    await mountAdminPools({ api });

    await screen.findByText('Chusen (drawing lots) required');
    const recordBtn = screen.getByRole('button', { name: /Record chusen result/ });
    await act(async () => { fireEvent.click(recordBtn); });

    await waitFor(() => expect(api.overridePoolRank).toHaveBeenCalledTimes(3));
    expect(screen.queryByText(/Enter each of positions/)).toBeNull();
    const calls = api.overridePoolRank.mock.calls;
    expect(calls[0]).toEqual(['c1', 'Pool A', 'Alpha', 1, PASSWORD, '', 'Dojo A']);
    expect(calls[1]).toEqual(['c1', 'Pool A', 'Beta', 2, PASSWORD, '', 'Dojo B']);
    expect(calls[2]).toEqual(['c1', 'Pool A', 'Gamma', 3, PASSWORD, '', 'Dojo G']);
  });
});

describe('AdminPools chusen banner: non-ASCII member keys do not collapse the DOM id (M11)', () => {
  // Two id-less members (a legacy/UUID-less roster) whose chusenMemberKey
  // ("name|dojo") is entirely non-ASCII. The DOM id used to be built as
  // `chusen-${groupKey}-${memberKey}`.replace(/[^a-zA-Z0-9_-]+/g, "-"): that
  // regex collapses ANY run of non-ASCII characters to a single "-", so once
  // the id-prefix ("chusen-Pool A::1-") is stripped, both members' entire
  // Japanese name|dojo run collapses to the same trailing "-", regardless of
  // what the actual characters were. Two id-less Japanese-named members (the
  // normal case for this roster, not an edge case) therefore got the SAME DOM
  // id: invalid duplicate-id HTML, and the label's htmlFor then focused the
  // OTHER team's input on click.
  const nonAsciiGroup = {
    poolName: 'Pool A',
    teamNames: ['剣道さん', '剣道くん'],
    teams: [
      { id: '', name: '剣道さん', dojo: '道場A' },
      { id: '', name: '剣道くん', dojo: '道場A' },
    ],
    minPosition: 1,
  };

  it('gives the two members distinct DOM ids, each label pointing at its own input', async () => {
    const api = makeApi([nonAsciiGroup]);
    const { container } = await mountAdminPools({ api });

    await screen.findByText('Chusen (drawing lots) required');
    const inputs = screen.getAllByRole('spinbutton');
    expect(inputs.length).toBe(2);
    expect(inputs[0].id).not.toBe(inputs[1].id);

    // Each label's htmlFor must resolve to THAT team's own input. Checked
    // directly against the label/input DOM nodes (rather than via
    // getByLabelText, which would still appear to "work" even on a
    // duplicate id by returning the first DOM match) so a real id collision
    // cannot hide behind a lenient query.
    const labels = container.querySelectorAll('label');
    expect(labels.length).toBe(2);
    expect(labels[0].htmlFor).toBe(inputs[0].id);
    expect(labels[1].htmlFor).toBe(inputs[1].id);
    expect(labels[0].htmlFor).not.toBe(labels[1].htmlFor);
  });
});
