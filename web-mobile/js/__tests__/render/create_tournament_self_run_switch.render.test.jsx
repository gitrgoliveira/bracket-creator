// bc-hnam: a self-run tournament always shows competitor names on the public
// pages, so creating one while competitions on disk hide their names (a
// restored data folder) is answered 409 public_names_self_run_switch with the
// list. The first-run form names those competitions in a confirm, and only on
// a yes sends the create again with confirmShowNames; a no creates nothing and
// says so.
//
// CreateTournament is not exported: the real App renders it when the server
// has no tournament (fetchTournament answers null). The App mounts once per
// file (its module mounts on import), so the cases run in order on one form,
// the one that creates the tournament last.
import React from 'react';
import { act, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { mountApp } from '../helpers/mount_app.js';

const HIDING = [
  { id: 'c1', name: 'Kyu Men', publicNames: 'person' },
  { id: 'c2', name: 'Dan Women', publicNames: 'hidden' },
];

function switchRefusal() {
  const e = new Error('Creating a self-run tournament shows names.');
  e.code = 'public_names_self_run_switch';
  e.competitions = HIDING;
  return e;
}

const createTournament = vi.fn();
const confirmDialog = vi.fn();

const STUBBED_GLOBALS = {
  API: {
    fetchTournament: vi.fn(async () => null),
    fetchCompetitions: vi.fn(async () => []),
    fetchAuthConfig: vi.fn(async () => ({ mode: 'file', resetEnabled: true })),
    fetchAnnouncements: vi.fn(async () => []),
    fetchCompetitionDetails: vi.fn(async () => null),
    subscribeToEvents: vi.fn(() => () => {}),
    reconnectEvents: vi.fn(),
    resumeAfterAuth: vi.fn(),
    createTournament,
  },
  confirmDialog,
};

let unmount;
let app;

beforeAll(async () => {
  ({ unmount } = await mountApp({ path: '/', globals: STUBBED_GLOBALS }));
  app = await import('../../app.jsx');
  await act(async () => { fireEvent.input(screen.getByLabelText('Tournament Name'), { target: { value: 'Cup' } }); });
  await act(async () => { fireEvent.input(screen.getByLabelText('Admin Password'), { target: { value: 'secret-pw' } }); });
  await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Self-run' })); });
});

afterAll(() => { unmount(); });

beforeEach(() => {
  createTournament.mockReset();
  confirmDialog.mockReset();
  window.confirmDialog = confirmDialog;
});

async function submit() {
  await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Create Tournament' })); });
}

describe('selfRunSwitchMessage', () => {
  it('names every competition and what it shows now', () => {
    const msg = app.selfRunSwitchMessage(HIDING);
    expect(msg).toContain('2 competitions');
    expect(msg).toContain('Kyu Men (now: Numbers, a link per person)');
    expect(msg).toContain('Dan Women (now: Numbers only)');
    expect(msg).toContain('"Show names"');
  });

  it('counts one competition in the singular', () => {
    expect(app.selfRunSwitchMessage([HIDING[0]])).toContain('1 competition back');
  });
});

describe('the first-run form over competitions that hide names', () => {
  it('shows any other refusal as it is, asking nothing', async () => {
    createTournament.mockRejectedValueOnce(new Error('admin password is required'));
    await submit();
    expect(confirmDialog).not.toHaveBeenCalled();
    expect(screen.getByTestId('create-tournament-error').textContent).toBe('admin password is required');
  });

  it('creates nothing on a no, keeps the form and says the tournament was not created', async () => {
    createTournament.mockRejectedValueOnce(switchRefusal());
    confirmDialog.mockResolvedValueOnce(false);
    await submit();
    expect(confirmDialog).toHaveBeenCalledTimes(1);
    expect(createTournament).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId('create-tournament-error').textContent).toBe(app.SELF_RUN_SWITCH_CANCELLED);
    expect(screen.getByLabelText('Tournament Name').value).toBe('Cup');
  });

  it('asks, naming the competitions, and creates with confirmShowNames on a yes', async () => {
    createTournament
      .mockRejectedValueOnce(switchRefusal())
      .mockResolvedValueOnce({ name: 'Cup', mode: 'self-run', competitions: [] });
    confirmDialog.mockResolvedValueOnce(true);
    await submit();
    expect(confirmDialog).toHaveBeenCalledTimes(1);
    const asked = confirmDialog.mock.calls[0][0];
    expect(asked.message).toContain('Kyu Men');
    expect(asked.message).toContain('Dan Women');
    expect(createTournament).toHaveBeenCalledTimes(2);
    expect(createTournament.mock.calls[0][0].confirmShowNames).toBeUndefined();
    expect(createTournament.mock.calls[1][0]).toMatchObject({ name: 'Cup', mode: 'self-run', confirmShowNames: true });
  });
});
