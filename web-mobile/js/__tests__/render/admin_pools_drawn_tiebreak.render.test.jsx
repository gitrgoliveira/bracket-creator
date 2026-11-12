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

async function mount(poolMatches, format = 'mixed', extra = {}, api = {}) {
  window.API = { reopenMatch: vi.fn().mockResolvedValue({}), ...api };
  window.PoolsViewer = () => null;
  await act(async () => {
    render(
      <AdminPools
        c={{ ...comp, format, ...extra }}
        pools={[{ poolName: 'Pool A', players: [] }]}
        poolMatches={poolMatches}
        standings={{}}
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

  // A drawn daihyosen is never settled, so a chusen (waiting or recorded) does
  // not complete the pool: the notice and its Fight it again stay, the operator's
  // only way out, and they do not wait for the chusen answer either.
  describe.each([
    ['a chusen waiting', { candidates: [{ poolName: 'Pool A', teamNames: ['Bea', 'Cal'], teams: [{ id: 'a', name: 'Bea' }, { id: 'b', name: 'Cal' }], minPosition: 1 }], recorded: [] }],
    ['a chusen recorded', { candidates: [], recorded: [{ poolName: 'Pool A', teamNames: ['Bea', 'Cal'], teams: [{ id: 'a', name: 'Bea' }, { id: 'b', name: 'Cal' }], ranks: [1, 2], minPosition: 1 }] }],
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

  it.each([
    ['a tie-break bout with a winner', bout('Pool A-TB-0', { winner: 'Bea' })],
    ['a tie-break bout not fought yet', bout('Pool A-TB-0', { status: 'scheduled' })],
    ['a drawn regular pool match', bout('Pool A-0')],
  ])('shows no notice for %s', async (_name, m) => {
    await mount([m]);
    expect(screen.queryByTestId('drawn-tiebreak-notice')).toBeNull();
  });
});
