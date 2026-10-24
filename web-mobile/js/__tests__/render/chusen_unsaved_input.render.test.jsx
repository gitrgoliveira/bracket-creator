// Review of PR #483 (d6a45d25): the chusen panel held a new build's automatic
// reload while ANY key stood in its typed ranks, and a key is only deleted when
// a group is recorded or its change cancelled. A rank typed then erased, or a
// group another device resolved, kept the reload held for as long as the Pools
// tab stayed open. It holds the reload only while a group still shown carries
// a rank that is not empty and not the one the box shows by itself.
import React from 'react';
import { render, act, fireEvent, screen, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import * as V from '../../app_version.jsx';

let restoreGlobals;
let AdminPools;
let reload;

beforeAll(async () => {
  restoreGlobals = installWindowStubs({ ScoreEditorModal: () => null });
  await import('../../admin_pools.jsx');
  AdminPools = window.AdminPools;
});

afterAll(() => restoreGlobals());

beforeEach(() => {
  window.PoolsViewer = () => null;
  document.head.innerHTML = '<meta name="app-build" content="build-a">';
  V.resetVersionForTests();
  reload = vi.fn();
  V.setReloader(reload);
});

afterEach(() => {
  document.head.innerHTML = '';
  V.resetVersionForTests();
});

const comp = { id: 'c1', name: 'Team Cup', format: 'mixed', kind: 'team', teamSize: 5, status: 'pools' };
const group = {
  poolName: 'Pool A',
  teamNames: ['Alpha', 'Beta'],
  teams: [
    { id: 'team-a', name: 'Alpha', dojo: 'Dojo A' },
    { id: 'team-b', name: 'Beta', dojo: 'Dojo B' },
  ],
  minPosition: 1,
};

function Host({ sig }) {
  return (
    <AdminPools c={comp} pools={[{ poolName: 'Pool A', players: [] }]} poolMatches={[]}
      standings={{ 'Pool A': [{ player: { name: 'Alpha' }, rank: sig }] }} tweaks={{}} onEditScore={vi.fn()} password="pw" />
  );
}

async function mount(api) {
  window.API = api;
  let r;
  await act(async () => { r = render(<Host sig={1} />); });
  await waitFor(() => expect(screen.getAllByRole('spinbutton').length).toBe(2));
  return r;
}

describe('the chusen panel holds the reload only while it holds a change', () => {
  it('a rank typed and then erased holds nothing', async () => {
    const r = await mount({ chusenCandidates: vi.fn().mockResolvedValue({ candidates: [group], recorded: [] }) });
    const [first] = screen.getAllByRole('spinbutton');
    await act(async () => { fireEvent.change(first, { target: { value: '2' } }); });
    act(() => { V.noteServerBuild('build-b'); });
    expect(reload, 'a typed rank holds the reload').not.toHaveBeenCalled();
    await act(async () => { fireEvent.change(first, { target: { value: '' } }); });
    expect(reload, 'erased, it holds nothing').toHaveBeenCalledTimes(1);
    r.unmount();
  });

  it('a rank typed back to the one shown holds nothing', async () => {
    const r = await mount({ chusenCandidates: vi.fn().mockResolvedValue({ candidates: [group], recorded: [] }) });
    const [first] = screen.getAllByRole('spinbutton');
    await act(async () => { fireEvent.change(first, { target: { value: '2' } }); });
    await act(async () => { fireEvent.change(first, { target: { value: '1' } }); });
    act(() => { V.noteServerBuild('build-b'); });
    expect(reload).toHaveBeenCalledTimes(1);
    r.unmount();
  });

  it('a group another device resolved no longer holds the reload', async () => {
    const api = {
      chusenCandidates: vi.fn()
        .mockResolvedValueOnce({ candidates: [group], recorded: [] })
        .mockResolvedValue({ candidates: [], recorded: [] }),
    };
    const r = await mount(api);
    const [first] = screen.getAllByRole('spinbutton');
    await act(async () => { fireEvent.change(first, { target: { value: '2' } }); });
    act(() => { V.noteServerBuild('build-b'); });
    expect(reload).not.toHaveBeenCalled();
    // Another device recorded the order: the standings move and the read
    // answers no tie left. The typed key is still in the state.
    await act(async () => { r.rerender(<Host sig={2} />); });
    await waitFor(() => expect(api.chusenCandidates).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(screen.queryAllByRole('spinbutton').length).toBe(0));
    expect(reload).toHaveBeenCalledTimes(1);
    r.unmount();
  });
});
