// A pool tie-break bout saved as a draw settles nothing: the pool keeps its tie
// and, in a Pools + Knockout competition, the knockout places it decides stay
// empty. Nothing on the page said so (found by walking it in a browser), so the
// Pools tab names the bout and offers the two ways on: fight it again (the
// reopen the editors use) or open it to decide it by encho or hantei.

import React from 'react';
import { render, act, screen, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

let restoreGlobals;
let AdminPools;

beforeAll(async () => {
  restoreGlobals = installWindowStubs({
    ScoreEditorModal: ({ match }) => <div data-testid="editor-open">{match.id}</div>,
  });
  await import('../../admin_pools.jsx');
  AdminPools = window.AdminPools;
});

afterAll(() => restoreGlobals());

const comp = { id: 'c1', name: 'Cup', format: 'mixed', kind: 'individual', status: 'pools' };

const bout = (id, over = {}) => ({
  id, sideA: 'Bea', sideB: 'Cal', sideAId: 'a', sideBId: 'b', status: 'completed', winner: '', court: 'A', ...over,
});

async function mount(poolMatches, format = 'mixed', extra = {}, api = {}, standings = {}) {
  window.API = { reopenMatch: vi.fn().mockResolvedValue({}), ...api };
  window.PoolsViewer = () => null;
  await act(async () => {
    render(
      <AdminPools
        c={{ ...comp, format, ...extra }}
        pools={[{ poolName: 'Pool A', players: [] }]}
        poolMatches={poolMatches}
        standings={standings}
        tweaks={{}}
        onEditScore={vi.fn()}
        password="pw"
      />
    );
  });
  return window.API;
}

describe('AdminPools: a tie-break bout saved as a draw', () => {
  it('names the bout and what the draw leaves undecided', async () => {
    await mount([bout('Pool A-TB-0')]);
    expect(screen.getByText('A tie-break bout was saved as a draw')).toBeTruthy();
    expect(screen.getByTestId('drawn-tiebreak-notice').textContent)
      .toMatch(/Pool A: Bea and Cal are still tied, so the knockout places this pool decides stay empty/);
  });

  it('says the table keeps them tied outside a Pools + Knockout competition', async () => {
    await mount([bout('Pool A-TB-0')], 'league');
    expect(screen.getByTestId('drawn-tiebreak-notice').textContent).toMatch(/so the table keeps them tied/);
  });

  it('does not name a pool on a league, which has none', async () => {
    await mount([bout('Pool A-TB-0')], 'league');
    const text = screen.getByTestId('drawn-tiebreak-notice').textContent;
    expect(text).toMatch(/^A tie-break bout was saved as a drawBea and Cal are still tied/);
    expect(text).not.toMatch(/Pool A/);
  });

  it('Open bout opens the score editor on that bout', async () => {
    await mount([bout('Pool A-TB-0')]);
    expect(screen.queryByTestId('editor-open')).toBeNull();
    await act(async () => { screen.getByRole('button', { name: 'Open bout' }).click(); });
    expect(screen.getByTestId('editor-open').textContent).toBe('Pool A-TB-0');
  });

  it('Fight it again reopens the bout', async () => {
    const api = await mount([bout('Pool A-TB-0')]);
    await act(async () => { screen.getByRole('button', { name: /Fight it again/ }).click(); });
    await waitFor(() => expect(api.reopenMatch).toHaveBeenCalled());
    expect(api.reopenMatch.mock.calls[0].slice(0, 3)).toEqual(['c1', 'Pool A-TB-0', 'pw']);
  });

  it('holds both buttons once the reopen has landed, so a tap is not swallowed', async () => {
    await mount([bout('Pool A-TB-0')]);
    await act(async () => { screen.getByRole('button', { name: /Fight it again/ }).click(); });
    const again = await screen.findByRole('button', { name: 'Reopened' });
    expect(again.disabled).toBe(true);
    expect(screen.getByRole('button', { name: 'Open bout' }).disabled).toBe(true);
  });

  it('names a daihyosen the same way', async () => {
    await mount([bout('Pool A-DH-0')]);
    expect(screen.getByText('A daihyosen was saved as a draw')).toBeTruthy();
  });

  // A chusen that is waiting, or whose answer is outstanding, has ranked nobody:
  // the drawn bout keeps its notice and its Fight it again, the operator's way
  // out, and the notice does not wait for the chusen answer either.
  describe.each([
    ['a chusen waiting', { candidates: [{ poolName: 'Pool A', teamNames: ['Bea', 'Cal'], teams: [{ id: 'a', name: 'Bea' }, { id: 'b', name: 'Cal' }], minPosition: 1 }], recorded: [] }],
    ['the chusen answer outstanding', new Promise(() => {})],
  ])('on a team competition with %s', (_name, chusen) => {
    const team = { kind: 'team', teamSize: 3 };
    const api = { chusenCandidates: vi.fn().mockReturnValue(chusen instanceof Promise ? chusen : Promise.resolve(chusen)) };

    it('still shows the notice for the drawn daihyosen', async () => {
      await mount([bout('Pool A-DH-0')], 'mixed', team, api);
      await waitFor(() => expect(window.API.chusenCandidates).toHaveBeenCalled());
      expect(screen.getByText('A daihyosen was saved as a draw')).toBeTruthy();
    });

    it('still shows the notice for a drawn tie-break bout', async () => {
      await mount([bout('Pool A-TB-0')], 'mixed', team, api);
      await waitFor(() => expect(window.API.chusenCandidates).toHaveBeenCalled());
      expect(screen.getByTestId('drawn-tiebreak-notice')).toBeTruthy();
    });
  });

  // Once a chusen has ranked the tied teams (their standings rows carry
  // isOverridden) the lots decided the place: the pool is settled and nothing is
  // stuck, so the "still tied" notice is gone. The lots were drawn outside the
  // app and the organiser entered the order they gave, so the bout stays within
  // reach for a correction: the notice says the lots decide it and Fight it
  // again asks before its result replaces them.
  describe('on a team competition with a chusen recorded', () => {
    const team = { kind: 'team', teamSize: 3 };
    const row = (id, name) => ({ player: { id, name }, points: 2, isOverridden: true });
    const standings = { 'Pool A': [row('a', 'Bea'), row('b', 'Cal')] };
    const chusen = { candidates: [], recorded: [{ poolName: 'Pool A', teamNames: ['Bea', 'Cal'], teams: [{ id: 'a', name: 'Bea' }, { id: 'b', name: 'Cal' }], ranks: [1, 2], minPosition: 1 }] };
    const api = (extra = {}) => ({ chusenCandidates: vi.fn().mockResolvedValue(chusen), ...extra });

    it.each([
      ['daihyosen', 'Pool A-DH-0', 'A daihyosen was saved as a draw, and your lots decide it'],
      ['tie-break bout', 'Pool A-TB-0', 'A tie-break bout was saved as a draw, and your lots decide it'],
    ])('names the drawn %s as one the lots decide, never as still tied', async (_what, id, title) => {
      await mount([bout(id)], 'mixed', team, api(), standings);
      await waitFor(() => expect(window.API.chusenCandidates).toHaveBeenCalled());
      expect(screen.getByText(title)).toBeTruthy();
      const text = screen.getByTestId('drawn-tiebreak-notice').textContent;
      expect(text).toMatch(/the order you entered from the lots stands/);
      expect(text).toMatch(/its result then replaces the lots for that place/);
      expect(text).not.toMatch(/still tied|keeps them tied|stay empty/);
      expect(screen.getByRole('button', { name: /Fight it again/ })).toBeTruthy();
      expect(screen.getByRole('button', { name: 'Open bout' })).toBeTruthy();
    });

    it('Fight it again asks first, and on Replace the lots reopens with confirmLots', async () => {
      const sentence = "This replaces the lots you entered for 1st: Bea 1st, Cal 2nd. The bout's result will decide the place.";
      const refused = Object.assign(new Error(sentence), { code: 'lots_replaced', lotsReplaced: { matchId: 'Pool A-TB-0' } });
      const reopenMatch = vi.fn().mockRejectedValueOnce(refused).mockResolvedValue({});
      window.confirmDialog = vi.fn().mockResolvedValue(true);
      await mount([bout('Pool A-TB-0')], 'mixed', team, api({ reopenMatch }), standings);
      await act(async () => { screen.getByRole('button', { name: /Fight it again/ }).click(); });

      await waitFor(() => expect(reopenMatch).toHaveBeenCalledTimes(2));
      expect(window.confirmDialog).toHaveBeenCalledTimes(1);
      expect(window.confirmDialog.mock.calls[0][0]).toMatchObject({ message: sentence, confirmLabel: 'Replace the lots' });
      expect(reopenMatch.mock.calls[0][3].confirmLots).toBe(false);
      expect(reopenMatch.mock.calls[1][3].confirmLots).toBe(true);
    });

    it('Cancel on the warning leaves the bout and the lots as they were, and says so', async () => {
      const refused = Object.assign(new Error('sentence'), { code: 'lots_replaced', lotsReplaced: { matchId: 'Pool A-TB-0' } });
      const reopenMatch = vi.fn().mockRejectedValue(refused);
      window.confirmDialog = vi.fn().mockResolvedValue(false);
      await mount([bout('Pool A-TB-0')], 'mixed', team, api({ reopenMatch }), standings);
      await act(async () => { screen.getByRole('button', { name: /Fight it again/ }).click(); });

      await screen.findByText('Cancelled: the lots you entered stand and the bout was left unchanged.');
      expect(reopenMatch).toHaveBeenCalledTimes(1);
    });
  });

  // A bout whose two sides are both barred, once the lots the organiser entered
  // rule it: the app enforces no path (operator ruling 2026-11-16), so the
  // notice still offers Fight it again, behind the reopen's own "Replace the
  // lots?" warning, beside Open bout, and says what a refight means.
  describe('on a league with the lots ruling a bout neither side can fight', () => {
    const row = (id, name) => ({ player: { id, name }, points: 2, isOverridden: true });
    const standings = { 'Pool A': [row('a', 'Bea'), row('b', 'Cal')] };
    const api = (extra = {}) => ({ chusenCandidates: vi.fn().mockResolvedValue({ candidates: [], recorded: [] }), ...extra });

    it('offers Fight it again and Open bout, and never says the bout cannot be fought', async () => {
      await mount([bout('Pool A-TB-0', { ineligibleSides: { a: true, b: true } })], 'league', {}, api(), standings);
      const text = screen.getByTestId('drawn-tiebreak-notice').textContent;
      expect(text).toMatch(/the order you entered from the lots stands/);
      expect(text).toMatch(/its result then replaces the lots for that place/);
      expect(text).toMatch(/Fought again, it waits on its court until both can fight; while they cannot, the court can record it as drawn again, and the lots you entered apply again\./);
      expect(text).not.toMatch(/offers the lots again/);
      expect(text).not.toMatch(/cannot be fought again|reinstated/);
      expect(screen.getByRole('button', { name: /Fight it again/ })).toBeTruthy();
      expect(screen.getByRole('button', { name: 'Open bout' })).toBeTruthy();
    });

    it('Fight it again asks first, and on Replace the lots reopens with confirmLots', async () => {
      const refused = Object.assign(new Error('sentence'), { code: 'lots_replaced', lotsReplaced: { matchId: 'Pool A-TB-0' } });
      const reopenMatch = vi.fn().mockRejectedValueOnce(refused).mockResolvedValue({});
      window.confirmDialog = vi.fn().mockResolvedValue(true);
      await mount([bout('Pool A-TB-0', { ineligibleSides: { a: true, b: true } })], 'league', {}, api({ reopenMatch }), standings);
      await act(async () => { screen.getByRole('button', { name: /Fight it again/ }).click(); });

      await waitFor(() => expect(reopenMatch).toHaveBeenCalledTimes(2));
      expect(window.confirmDialog.mock.calls[0][0]).toMatchObject({ confirmLabel: 'Replace the lots' });
      expect(reopenMatch.mock.calls[1][3].confirmLots).toBe(true);
    });

    it('still offers Fight it again when only one side is barred', async () => {
      await mount([bout('Pool A-TB-0', { ineligibleSides: { a: true, b: false } })], 'league', {}, api, standings);
      expect(screen.getByRole('button', { name: /Fight it again/ })).toBeTruthy();
    });
  });

  // A bout drawn because both sides withdrew earlier, with no lots entered yet:
  // the organiser may fight it again (it waits on its court until both can
  // fight, which may be never: only an injury withdrawal is reinstated) or enter
  // the lots, and the app forces neither (operator ruling 2026-11-16). The notice shows in a league and in pools + knockout alike,
  // with Fight it again, beside the lots entry the circle notice opens.
  describe.each([
    ['pools + knockout', 'mixed'],
    ['a league', 'league'],
  ])('on %s with a tie-breaker drawn because both sides withdrew', (_name, format) => {
    const barredBout = (id) => bout(id, { ineligibleSides: { a: 'kiken-voluntary', b: 'fusenpai' } });
    const row = (id, name) => ({ player: { id, name }, points: 2 });
    const standings = { 'Pool A': [row('a', 'Bea'), row('b', 'Cal')] };
    const pending = { poolName: 'Pool A', teamNames: ['Bea', 'Cal'], teams: [{ id: 'a', name: 'Bea' }, { id: 'b', name: 'Cal' }], minPosition: 1, stillLevel: null };
    const api = () => ({
      chusenCandidates: vi.fn().mockResolvedValue({ candidates: [pending], recorded: [] }),
      leagueTiebreakCandidates: vi.fn().mockResolvedValue({ groups: [] }),
    });

    it('names both ways on, fight it again or the lots, and offers Fight it again beside the lots entry', async () => {
      const calls = await mount([barredBout('Pool A-TB-0')], format, {}, api(), standings);
      const text = screen.getByTestId('drawn-tiebreak-notice').textContent;
      expect(text).toMatch(/Bea and Cal are still tied/);
      expect(text).toMatch(/Neither can fight while both are withdrawn\. Fight it again, or enter the lots below\. Fought again, it waits on its court until both can fight; while they cannot, the court can record it as drawn again, which offers the lots again\./);
      expect(text).not.toMatch(/reinstated/);
      expect(text).not.toMatch(/encho or hantei/);
      expect(screen.getByRole('button', { name: /Fight it again/ })).toBeTruthy();
      expect(await screen.findByRole('button', { name: /Record chusen result/ })).toBeTruthy();
      await act(async () => { screen.getByRole('button', { name: /Fight it again/ }).click(); });
      await waitFor(() => expect(calls.reopenMatch).toHaveBeenCalled());
      expect(calls.reopenMatch.mock.calls[0].slice(0, 3)).toEqual(['c1', 'Pool A-TB-0', 'pw']);
    });

    it('names a daihyosen the same way', async () => {
      await mount([barredBout('Pool A-DH-0')], format, { kind: 'team', teamSize: 3 }, api(), standings);
      expect(screen.getByText('A daihyosen was saved as a draw')).toBeTruthy();
      expect(screen.getByRole('button', { name: /Fight it again/ })).toBeTruthy();
    });
  });

  it.each([
    ['a tie-break bout with a winner', bout('Pool A-TB-0', { winner: 'Bea' })],
    ['a tie-break bout not fought yet', bout('Pool A-TB-0', { status: 'scheduled' })],
    ['a drawn regular pool match', bout('Pool A-0')],
  ])('shows no notice for %s', async (_name, m) => {
    await mount([m]);
    expect(screen.queryByTestId('drawn-tiebreak-notice')).toBeNull();
  });
});
