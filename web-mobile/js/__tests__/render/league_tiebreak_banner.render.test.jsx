// The team league's tie-break banner and its Undo surface (admin_pools.jsx).
//
// Pins the league review's operator-facing wording and flow, word for word as
// scripts/e2e/fixtures/league.mjs reads it:
//   * F4 copy: the banner speaks of a place that counts, never a "qualifying
//     position", and the button reads "Accept shared ranks".
//   * The banner stays up, with Remove and Accept, while a group holds a
//     tie-breaker bout nobody has fought; the Accept confirm names each such
//     bout, says the competition finishes now and that Undo brings the question
//     back, and no longer says "This cannot be undone".
//   * A fought round that went in a circle gets the lots sentence.
//   * Accept refused for an unfinished league match (league_not_settled): the
//     server's sentence shows (outside the banner, which the refetch removes)
//     and the candidates are read again.
//   * Once the ranks are accepted, whatever the league's status, the page lists
//     each accepted tie and offers "Undo shared ranks", whose confirm names a
//     shiaijo the tournament no longer has.

import React from 'react';
import { render, act, screen, waitFor, within } from '@testing-library/react';
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

function makeApi({ answer, tournament } = {}) {
  return {
    chusenCandidates: vi.fn().mockResolvedValue({ candidates: [], recorded: [] }),
    leagueTiebreakCandidates: vi.fn().mockResolvedValue(answer || { candidates: [group], finalized: false }),
    leagueTiebreakGenerate: vi.fn().mockResolvedValue({ matches: [] }),
    leagueTiebreakRemove: vi.fn().mockResolvedValue({ deleted: 0 }),
    leagueTiebreakFinalize: vi.fn().mockResolvedValue({ finalized: true, removed: 0 }),
    leagueTiebreakUndo: vi.fn().mockResolvedValue({ finalized: false, reopened: true, unassignedMatches: [], removedCourts: [] }),
    fetchTournament: vi.fn().mockResolvedValue(tournament || { courts: ['A', 'B'] }),
  };
}

async function mountAdminPools({ api, c = comp(), poolMatches = [] }) {
  window.API = api;
  window.PoolsViewer = () => null;
  window.LeagueStandingsViewer = () => null;
  const page = (comp) => (
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
  let utils;
  await act(async () => { utils = render(page(c)); });
  await waitFor(() => expect(api.leagueTiebreakCandidates).toHaveBeenCalled());
  // The competition as the page next gets it (the server's competition_started
  // flips a reopened league back to pools).
  return { rerenderWith: async (next) => { await act(async () => { utils.rerender(page(next)); }); } };
}

// The confirm the page opens, answered by `answer`; the message it showed.
let confirmed;
beforeEach(() => {
  confirmed = [];
});
function answerConfirmWith(answer) {
  window.confirmDialog = vi.fn((opts) => {
    confirmed.push(opts);
    return Promise.resolve(answer);
  });
}

describe('the tie-break banner copy (F4)', () => {
  it('speaks of a place that counts and offers "Accept shared ranks"', async () => {
    answerConfirmWith(false);
    await mountAdminPools({ api: makeApi() });

    const banner = (await screen.findByText('Tie-breaker required')).closest('.league-tiebreak');
    const text = banner.textContent;
    expect(text).not.toMatch(/qualif/i);
    expect(text).not.toMatch(/finalis/i);
    expect(text).toContain('level on every criterion for a place that counts');
    expect(within(banner).getByRole('button', { name: 'Accept shared ranks' })).toBeTruthy();
    expect(text).toContain('Undo shared ranks brings the question back');
  });
});

describe('Accept shared ranks while a tie-breaker waits (F3)', () => {
  it('keeps the banner up with Remove and Accept, and the confirm names the unfought bout', async () => {
    answerConfirmWith(false);
    const api = makeApi();
    await mountAdminPools({ api, poolMatches: [dhBout()] });

    expect(await screen.findByRole('button', { name: 'Remove unscored tie-breaker' })).toBeTruthy();
    const accept = screen.getByRole('button', { name: 'Accept shared ranks' });
    await act(async () => { accept.click(); });

    await waitFor(() => expect(confirmed).toHaveLength(1));
    const { message, confirmLabel } = confirmed[0];
    expect(confirmLabel).toBe('Accept shared ranks');
    expect(message).toContain('Accept shared ranks? Team Gamma, Team Delta share 1st place.');
    expect(message).toContain('The competition finishes now. Undo shared ranks brings the question back.');
    expect(message).toContain('This also removes the tie-breaker not yet fought: Team Gamma v Team Delta.');
    expect(message).not.toContain('cannot be undone');
    expect(message).not.toContain('Drawing lots');
    // Declined: nothing was written.
    expect(api.leagueTiebreakFinalize).not.toHaveBeenCalled();
  });

  it('names no bout when none is waiting', async () => {
    answerConfirmWith(false);
    await mountAdminPools({ api: makeApi() });

    await act(async () => { (await screen.findByRole('button', { name: 'Accept shared ranks' })).click(); });
    await waitFor(() => expect(confirmed).toHaveLength(1));
    expect(confirmed[0].message).not.toContain('This also removes');
  });

  it('says drawing lots is the other way for a fought round that went in a circle', async () => {
    answerConfirmWith(false);
    await mountAdminPools({ api: makeApi(), poolMatches: [dhBout({ status: 'completed', winner: 'Team Gamma' })] });

    await act(async () => { (await screen.findByRole('button', { name: 'Accept shared ranks' })).click(); });
    await waitFor(() => expect(confirmed).toHaveLength(1));
    expect(confirmed[0].message).toContain('Drawing lots is the other way to separate them.');
    expect(confirmed[0].message).not.toContain('This also removes');
  });

  it('writes the acceptance once confirmed, then reads the answer again', async () => {
    answerConfirmWith(true);
    const api = makeApi();
    await mountAdminPools({ api });

    await act(async () => { (await screen.findByRole('button', { name: 'Accept shared ranks' })).click(); });
    await waitFor(() => expect(api.leagueTiebreakFinalize).toHaveBeenCalledWith('c1', PASSWORD));
    await waitFor(() => expect(api.leagueTiebreakCandidates.mock.calls.length).toBeGreaterThanOrEqual(2));
  });
});

describe('Accept refused while a league match is unfinished (D7)', () => {
  it('shows the server sentence outside the banner and reads the table again', async () => {
    answerConfirmWith(true);
    const sentence = 'A league match is not finished yet, so the table can still change. Finish it, then decide.';
    const api = makeApi();
    const refusal = Object.assign(new Error(sentence), { code: 'league_not_settled', detail: sentence });
    api.leagueTiebreakFinalize.mockRejectedValue(refusal);
    // The refetch answers an unfinished table: nothing to ask, so no banner.
    await mountAdminPools({ api });

    const before = api.leagueTiebreakCandidates.mock.calls.length;
    api.leagueTiebreakCandidates.mockResolvedValue({ candidates: [], finalized: false });
    await act(async () => { (await screen.findByRole('button', { name: 'Accept shared ranks' })).click(); });

    expect(await screen.findByText(sentence)).toBeTruthy();
    await waitFor(() => expect(api.leagueTiebreakCandidates.mock.calls.length).toBeGreaterThan(before));
    await waitFor(() => expect(screen.queryByText('Tie-breaker required')).toBeNull());
    // The sentence outlives the banner it was said about.
    expect(screen.getByText(sentence)).toBeTruthy();
  });
});

describe('the accepted shared ranks and Undo', () => {
  const accepted = { candidates: [], finalized: true, accepted: [group] };

  it('lists each accepted tie and offers Undo on a finished league, with no banner', async () => {
    answerConfirmWith(false);
    await mountAdminPools({ api: makeApi({ answer: accepted }), c: comp({ status: 'completed' }) });

    expect(await screen.findByText('Shared ranks accepted: 1st place: Team Gamma, Team Delta.')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Undo shared ranks' })).toBeTruthy();
    expect(screen.queryByText('Tie-breaker required')).toBeNull();
  });

  it('offers Undo for a legacy flag that stored no groups, naming no teams', async () => {
    answerConfirmWith(false);
    await mountAdminPools({ api: makeApi({ answer: { candidates: [], finalized: true, accepted: [] } }), c: comp({ status: 'completed' }) });

    expect(await screen.findByText('Shared ranks accepted.')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Undo shared ranks' })).toBeTruthy();
  });

  it('asks first, in the fixed words, and undoes only on a yes', async () => {
    answerConfirmWith(false);
    const api = makeApi({ answer: accepted });
    await mountAdminPools({ api, c: comp({ status: 'completed' }), poolMatches: [{ id: 'Pool A-1', court: 'A', status: 'completed' }] });

    await act(async () => { (await screen.findByRole('button', { name: 'Undo shared ranks' })).click(); });
    await waitFor(() => expect(confirmed).toHaveLength(1));
    expect(confirmed[0].message).toBe('Undo the shared ranks? The league goes back to match play and the tie is asked again: run a tie-breaker or accept shared ranks.');
    expect(confirmed[0].confirmLabel).toBe('Undo shared ranks');
    expect(api.leagueTiebreakUndo).not.toHaveBeenCalled();
  });

  it('names a shiaijo the tournament no longer has', async () => {
    answerConfirmWith(false);
    const api = makeApi({ answer: accepted, tournament: { courts: ['A', 'B'] } });
    await mountAdminPools({
      api, c: comp({ status: 'completed' }),
      poolMatches: [{ id: 'Pool A-1', court: 'A', status: 'completed' }, { id: 'Pool A-2', court: 'C', status: 'completed' }],
    });

    await act(async () => { (await screen.findByRole('button', { name: 'Undo shared ranks' })).click(); });
    await waitFor(() => expect(confirmed).toHaveLength(1));
    expect(confirmed[0].message).toContain('Matches on shiaijo C, which the tournament no longer has, become unassigned.');
  });

  it('undoes on a yes and reads the answer again, so the banner returns', async () => {
    answerConfirmWith(true);
    const api = makeApi({ answer: accepted });
    const { rerenderWith } = await mountAdminPools({ api, c: comp({ status: 'completed' }) });

    api.leagueTiebreakCandidates.mockResolvedValue({ candidates: [group], finalized: false });
    await act(async () => { (await screen.findByRole('button', { name: 'Undo shared ranks' })).click(); });

    await waitFor(() => expect(api.leagueTiebreakUndo).toHaveBeenCalledWith('c1', PASSWORD));
    await waitFor(() => expect(screen.queryByRole('button', { name: 'Undo shared ranks' })).toBeNull());
    await rerenderWith(comp({ status: 'pools' }));
    expect(await screen.findByText('Tie-breaker required')).toBeTruthy();
  });

  it('shows the refusal when Undo is refused', async () => {
    answerConfirmWith(true);
    const api = makeApi({ answer: accepted });
    api.leagueTiebreakUndo.mockRejectedValue(Object.assign(new Error('The shared ranks were not accepted, so there is nothing to undo.'), { code: 'not_finalized' }));
    await mountAdminPools({ api, c: comp({ status: 'completed' }) });

    await act(async () => { (await screen.findByRole('button', { name: 'Undo shared ranks' })).click(); });
    expect(await screen.findByText('The shared ranks were not accepted, so there is nothing to undo.')).toBeTruthy();
  });
});
