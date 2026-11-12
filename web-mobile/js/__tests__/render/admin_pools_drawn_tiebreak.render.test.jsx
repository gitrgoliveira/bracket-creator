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

async function mount(poolMatches, format = 'mixed') {
  window.API = { reopenMatch: vi.fn().mockResolvedValue({}) };
  window.PoolsViewer = () => null;
  await act(async () => {
    render(
      <AdminPools
        c={{ ...comp, format }}
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

  it('names a daihyosen the same way', async () => {
    await mount([bout('Pool A-DH-0')]);
    expect(screen.getByText('A daihyosen was saved as a draw')).toBeTruthy();
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
