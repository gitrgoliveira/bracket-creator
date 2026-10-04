// The pending ("Not sent yet") banner of each score editor.
//
// 1. A queued write says "saved on this device" only when it was. The queue
// answers a write it holds with `persisted: false` when the browser could not
// store it (storage full or blocked; api_client.jsx _queuedAnswer): the write
// is in this page's memory alone and a reload loses it, so each score editor's
// pending banner must say to keep the page open instead (queuedNotice,
// write_result.jsx). Pinned for the three editors, both ways.
//
// 2. The way past a held write the server keeps refusing: once the queue
// reports it (API.heldWriteKeepsFailing), the banner offers to discard it,
// and only it, after a confirm (HeldWriteDiscard, admin_scoring_shared.jsx).

import React from 'react';
import { render, act, fireEvent, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { QUEUED_NOTICE, QUEUED_UNSAVED_NOTICE, HELD_WRITE_DISCARD_LABEL } from '../../write_result.jsx';
import { keyboardClick } from '../helpers/tap_events.js';

const STUBBED_GLOBALS = {
  isHikiwake: () => false,
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  isKikenDecision: (d) => d === 'kiken' || d === 'kiken-voluntary' || d === 'kiken-injury',
  isTextEntry: () => false,
  isInteractiveTarget: () => false,
  confirmDialog: vi.fn().mockResolvedValue(true),
  resolveRoundIndex: () => 0,
  API: {},
  AdminLineupHelpers: { rosterFor: vi.fn().mockReturnValue([]) },
  compMatches: () => [],
  compMatchesForCompetition: () => [],
  Term: ({ children }) => <span>{children}</span>,
  GlossaryHint: ({ name }) => <span title={name} />,
};

let restoreGlobals;
let ScoreEditorModal;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  await import('../../admin_scoring_modal.jsx');
  ScoreEditorModal = window.ScoreEditorModal;
});

afterAll(() => restoreGlobals());

beforeEach(() => {
  window.API = {
    fetchCompetitionDetails: vi.fn().mockResolvedValue({
      id: 'comp1',
      config: { format: 'knockout', teamMatchType: 'fixed', naginata: false, players: [] },
    }),
    hasPendingTerminalWrite: vi.fn().mockReturnValue(false),
    recordScore: vi.fn().mockResolvedValue(undefined),
    recordDecision: vi.fn(),
    recordDaihyosen: vi.fn(),
    removeDaihyosen: vi.fn(),
    putMatchLineup: vi.fn(),
    reopenMatch: vi.fn(),
  };
});

const individualMatch = {
  id: 'm-ind', compId: 'comp1', status: 'running', phase: 'pool', poolName: 'Pool 1', court: 'A',
  sideA: { id: 'p1', name: 'Yamada' }, sideB: { id: 'p2', name: 'Tanaka' },
  ipponsA: [], ipponsB: ['M'], hansokuA: 0, hansokuB: 0,
};
const engiMatch = {
  id: 'm-engi', compId: 'comp1', status: 'running', phase: 'pool', poolName: 'Pool 1', court: 'A',
  compEngi: true,
  sideA: { id: 'pa', name: 'Aoi - Haru', dojo: 'DojoA' }, sideB: { id: 'pb', name: 'Bo - Cho', dojo: 'DojoB' },
  flagsA: 3, flagsB: 0,
};
const teamMatch = {
  id: 'm-team', compId: 'comp1', status: 'scheduled', phase: 'bracket', round: 'Final', court: 'A',
  compKind: 'team', teamSize: 3, compFormat: 'knockout', teamMatchType: 'fixed',
  sideA: { id: 'team-A', name: 'Team A' }, sideB: { id: 'team-B', name: 'Team B' }, subResults: [],
};

// Each editor's commit that the host answers with the queued answer.
const EDITORS = [
  {
    name: 'individual', match: individualMatch,
    commit: async () => {
      const finish = () => screen.getByText(/^(Finish|Tap again to finish)$/);
      await keyboardClick(finish());
      await keyboardClick(finish());
    },
  },
  {
    name: 'engi', match: engiMatch,
    commit: async () => {
      await keyboardClick(screen.getByTestId('engi-submit'));
      await keyboardClick(screen.getByTestId('engi-submit'));
    },
  },
  {
    name: 'team', match: teamMatch,
    commit: async () => { await act(async () => { fireEvent.click(screen.getByText('Start match')); }); },
  },
];

describe.each(EDITORS)('$name editor: the pending banner promises only what the queue did', ({ match, commit }) => {
  async function queueWith(answer) {
    const onSubmit = vi.fn().mockResolvedValue(answer);
    await act(async () => {
      render(<ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={onSubmit} password="pw" />);
    });
    await commit();
    expect(onSubmit).toHaveBeenCalledTimes(1);
    return screen.findByRole('status');
  }

  it('stored on the device: "saved on this device"', async () => {
    const banner = await queueWith({ queued: true });
    expect(banner.textContent).toContain(QUEUED_NOTICE);
    expect(banner.textContent).not.toContain(QUEUED_UNSAVED_NOTICE);
  });

  it('not stored (browser storage full): keep this page open, never "saved on this device"', async () => {
    const banner = await queueWith({ queued: true, persisted: false });
    expect(banner.textContent).toContain(QUEUED_UNSAVED_NOTICE);
    expect(banner.textContent).not.toContain('saved on this device');
  });
});

describe.each(EDITORS)('$name editor: a held write the server keeps refusing can be discarded', ({ match, commit }) => {
  let statusListeners;
  beforeEach(() => {
    statusListeners = new Set();
    window.subscribeSyncStatus = (fn) => { statusListeners.add(fn); fn('syncing'); return () => statusListeners.delete(fn); };
    window.API.heldWriteKeepsFailing = vi.fn().mockReturnValue(false);
    window.API.discardFailingHeldWrites = vi.fn().mockReturnValue(1);
    window.confirmDialog = vi.fn().mockResolvedValue(true);
  });
  afterEach(() => { delete window.subscribeSyncStatus; });

  async function queued() {
    const onSubmit = vi.fn().mockResolvedValue({ queued: true });
    await act(async () => {
      render(<ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={onSubmit} password="pw" />);
    });
    await commit();
    return screen.findByRole('status');
  }
  const discardButton = () => screen.queryByTestId('held-write-discard');
  const crossThreshold = () => act(async () => {
    window.API.heldWriteKeepsFailing.mockReturnValue(true);
    for (const fn of statusListeners) fn('server-error');
  });

  it('offers nothing while the held write is only waiting', async () => {
    await queued();
    expect(discardButton()).toBeNull();
  });

  it('once it keeps failing, discards it after the confirm, and only for this match', async () => {
    await queued();
    await crossThreshold();
    expect(discardButton().textContent).toBe(HELD_WRITE_DISCARD_LABEL);
    await act(async () => { fireEvent.click(discardButton()); });
    expect(window.confirmDialog).toHaveBeenCalledWith(expect.objectContaining({ danger: true }));
    expect(window.API.discardFailingHeldWrites).toHaveBeenCalledWith(match.compId, match.id);
    // The pending banner goes with it.
    expect(screen.queryByRole('status')).toBeNull();
  });

  it('a cancelled confirm discards nothing', async () => {
    window.confirmDialog = vi.fn().mockResolvedValue(false);
    await queued();
    await crossThreshold();
    await act(async () => { fireEvent.click(discardButton()); });
    expect(window.API.discardFailingHeldWrites).not.toHaveBeenCalled();
    expect(screen.getByRole('status')).toBeTruthy();
  });
});
