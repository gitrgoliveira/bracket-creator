// A team carries the lineup of its previous team match by default (operator
// ruling 2026-10-05), so the at-court lineup panel no longer offers "Copy from
// previous match". What it offers instead, and only on a match's OWN lineup, is
// "Use the previous match's lineup": it removes the match's own lineup and
// shows the one the team carries again.

import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, beforeAll, beforeEach, afterAll, vi } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

const SQUAD = [
  { id: 'mem-1', index: 1, name: 'Aoki' },
  { id: 'mem-2', index: 2, name: 'Sato' },
  { id: 'mem-3', index: 3, name: 'Ito' },
];
const NAMES = { positions: { 1: 'Aoki', 2: 'Sato', 3: 'Ito' }, memberIds: { 1: 'mem-1', 2: 'mem-2', 3: 'mem-3' } };
const OWN = { teamId: 'uuid-grouped', matchId: 'Pool D-1', ...NAMES, sourceMatchId: 'Pool D-1', saved: true };
const OWN_OTHER_NAMES = { ...OWN, positions: { 1: 'Mori' }, memberIds: {} };
const CARRIED = { teamId: 'uuid-grouped', matchId: 'Pool D-0', ...NAMES, sourceMatchId: 'Pool D-0', saved: true };
const STARTING = { teamId: 'uuid-grouped', round: 0, ...NAMES, sourceRound: 0, saved: true };

const COMP = { id: 'comp-1', name: 'Team Event', kind: 'team', teamSize: 3 };
const TEAM = { id: 'uuid-grouped', name: 'Grouped Team', number: 'T5' };
const MATCH = {
  id: 'Pool D-1', compId: 'comp-1', phase: 'pool', poolName: 'Pool D',
  sideA: { id: 'uuid-grouped', name: 'Grouped Team' }, sideB: { id: 'other', name: 'Other' }, status: 'scheduled',
};
const EARLIER = {
  id: 'Pool D-0', compId: 'comp-1', phase: 'pool', poolName: 'Pool D',
  sideA: { id: 'uuid-grouped', name: 'Grouped Team' }, sideB: { id: 'third', name: 'Third' }, status: 'completed',
};

const api = {
  fetchLineupInForce: vi.fn(),
  fetchSquads: vi.fn().mockResolvedValue({ 'uuid-grouped': SQUAD }),
  putMatchLineup: vi.fn().mockResolvedValue({ positions: {} }),
  deleteMatchLineup: vi.fn().mockResolvedValue(true),
};
const STUBBED_GLOBALS = {
  compMatches: () => [],
  confirmDialog: vi.fn(),
  AdminLineupHelpers: {
    positionsForSize: (n) => Array.from({ length: n }, (_, i) => ({ key: String(i + 1), label: String(i + 1) })),
    rosterFor: () => [],
    mergeRosterWithAssigned: (base) => (Array.isArray(base) ? base : []),
    teamIdOf: (t) => t?.id || t?.name || '',
    resolveMemberIdsForPositions: vi.fn().mockResolvedValue({ memberIds: {}, squad: [], failures: [] }),
    memberIdentityWarning: () => '',
  },
  API: api,
};

let restoreGlobals;
let MatchLineupSideEditor;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  ({ MatchLineupSideEditor } = await import('../../admin_schedule_lineup.jsx'));
});
afterAll(() => restoreGlobals());

beforeEach(() => {
  api.fetchLineupInForce.mockReset().mockResolvedValue(CARRIED);
  api.deleteMatchLineup.mockReset().mockResolvedValue(true);
  api.putMatchLineup.mockClear();
  window.confirmDialog.mockReset().mockResolvedValue(true);
});

async function mountPanel() {
  let utils;
  await act(async () => {
    utils = render(
      <MatchLineupSideEditor comp={COMP} team={TEAM} match={MATCH} allMatches={[EARLIER, MATCH]} password="pw" showToast={vi.fn()} />
    );
  });
  await act(async () => { await Promise.resolve(); });
  return utils;
}

const useButton = (utils) => utils.queryByRole('button', { name: "Use the previous match's lineup" });

describe('the at-court lineup panel no longer copies from a previous match', () => {
  it.each([
    ['a carried lineup', CARRIED],
    ['a match\'s own lineup', OWN],
    ['the starting lineup', STARTING],
    ['no lineup at all', null],
  ])('offers no "Copy from previous match" on %s', async (_name, lineup) => {
    api.fetchLineupInForce.mockResolvedValue(lineup);
    const utils = await mountPanel();
    expect(utils.queryByRole('button', { name: /Copy from previous match/ })).toBeNull();
    expect(utils.queryByText(/Copy from previous/)).toBeNull();
  });
});

describe('Use the previous match\'s lineup', () => {
  it.each([
    ['a carried lineup', CARRIED],
    ['the starting lineup', STARTING],
    ['no lineup at all', null],
  ])('is not offered on %s', async (_name, lineup) => {
    api.fetchLineupInForce.mockResolvedValue(lineup);
    const utils = await mountPanel();
    expect(useButton(utils)).toBeNull();
  });

  it('is offered on the match\'s own lineup, above the positions and Save', async () => {
    api.fetchLineupInForce.mockResolvedValue(OWN);
    const utils = await mountPanel();
    const button = useButton(utils);
    expect(button).toBeTruthy();
    const firstPosition = utils.container.querySelector('[data-testid^="match-lineup-pos-"]');
    const save = utils.getByRole('button', { name: /Save lineup/ });
    // Node.compareDocumentPosition: DOCUMENT_POSITION_FOLLOWING === 4.
    expect(button.compareDocumentPosition(firstPosition) & 4).toBeTruthy();
    expect(firstPosition.compareDocumentPosition(save) & 4).toBeTruthy();
  });

  it('asks first, removes the match\'s own lineup, and shows the one it carries again', async () => {
    api.fetchLineupInForce.mockResolvedValueOnce(OWN_OTHER_NAMES).mockResolvedValue(CARRIED);
    const utils = await mountPanel();
    expect(utils.getByText('Lineup for this match')).toBeTruthy();
    expect(utils.getByLabelText('1 player').value).toBe('Mori');

    await act(async () => { fireEvent.click(useButton(utils)); });
    await act(async () => { await Promise.resolve(); });

    expect(window.confirmDialog).toHaveBeenCalledTimes(1);
    const confirm = window.confirmDialog.mock.calls[0][0];
    expect(confirm.message).toContain('Pool D · Match 2');
    expect(confirm.message).toContain('Later matches that have no lineup of their own follow too.');
    expect(api.deleteMatchLineup).toHaveBeenCalledWith('comp-1', 'uuid-grouped', 'Pool D-1', 'pw');
    expect(api.fetchLineupInForce).toHaveBeenCalledTimes(2);
    expect(utils.getByText('Same as Pool D · Match 1')).toBeTruthy();
    expect(utils.getByLabelText('1 player').value).toBe('Aoki');
    expect(useButton(utils)).toBeNull();
    // What it now shows is what it loaded: nothing to save.
    expect(utils.getByRole('button', { name: /Save lineup/ }).disabled).toBe(true);
  });

  it('leaves the side empty when nothing is left for the match to carry', async () => {
    api.fetchLineupInForce.mockResolvedValueOnce(OWN).mockResolvedValue(null);
    const utils = await mountPanel();
    await act(async () => { fireEvent.click(useButton(utils)); });
    await act(async () => { await Promise.resolve(); });
    expect(utils.getByText('No lineup saved yet')).toBeTruthy();
    expect(utils.getByLabelText('1 player').value).toBe('');
  });

  it('removes nothing when the operator declines', async () => {
    api.fetchLineupInForce.mockResolvedValue(OWN);
    window.confirmDialog.mockResolvedValue(false);
    const utils = await mountPanel();
    await act(async () => { fireEvent.click(useButton(utils)); });
    await act(async () => { await Promise.resolve(); });
    expect(api.deleteMatchLineup).not.toHaveBeenCalled();
    expect(api.fetchLineupInForce).toHaveBeenCalledTimes(1);
    expect(utils.getByText('Lineup for this match')).toBeTruthy();
  });

  it('says so when the removal is refused, and keeps the lineup shown', async () => {
    api.fetchLineupInForce.mockResolvedValue(OWN);
    api.deleteMatchLineup.mockRejectedValue(new Error('Failed to delete match lineup'));
    const utils = await mountPanel();
    await act(async () => { fireEvent.click(useButton(utils)); });
    await act(async () => { await Promise.resolve(); });
    expect(utils.getByText('Failed to delete match lineup')).toBeTruthy();
    expect(utils.getByText('Lineup for this match')).toBeTruthy();
    expect(utils.getByLabelText('1 player').value).toBe('Aoki');
  });
});
