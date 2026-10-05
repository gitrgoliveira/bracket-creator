// What both lineup editors (the at-court panel's MatchLineupSideEditor and the
// Lineups page) do about a lineup they could not read, about a removal, and about
// a save of the lineup that is still queued, in a real DOM:
//
//  - a lineup that could not be read is never saved: Save and the boxes are off
//    and the problem stays, with a Try again that reads it again;
//  - "Use the previous match's lineup" waits while a save of that lineup is
//    still queued (the save would replay after the removal and bring the lineup
//    back), and says why in a line, never in a title alone;
//  - a removal whose re-read fails leaves an empty, unread form that cannot be
//    saved, never the removed lineup shown as the match's own;
//  - "Not restored" goes once the operator saves the lineup or changes it.

import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';

const SQUAD = [
  { id: 'mem-1', index: 1, name: 'Aoki' },
  { id: 'mem-2', index: 2, name: 'Sato' },
  { id: 'mem-3', index: 3, name: 'Ito' },
  { id: 'mem-4', index: 4, name: 'Mori' },
];
const NAMES = { positions: { 1: 'Aoki', 2: 'Sato', 3: 'Ito' }, memberIds: { 1: 'mem-1', 2: 'mem-2', 3: 'mem-3' } };
const lineupFor = (extra) => ({ teamId: 'team-a', competitionId: 'comp-1', ...NAMES, saved: true, ...extra });
const STARTING = lineupFor({ round: 0, sourceRound: 0 });
const CARRIED = lineupFor({ matchId: 'Pool A-0', sourceMatchId: 'Pool A-0' });
const OWN = lineupFor({ matchId: 'Pool A-2', sourceMatchId: 'Pool A-2' });
const SAVED_ELSEWHERE = lineupFor({
  matchId: 'Pool A-0', sourceMatchId: 'Pool A-0',
  positions: { 1: 'Kato', 2: 'Sato', 3: 'Ito' }, memberIds: { 1: 'mem-5', 2: 'mem-2', 3: 'mem-3' },
});

const A = { id: 'team-a', name: 'Team A' };
const B = { id: 'team-b', name: 'Team B' };
const C = { id: 'team-c', name: 'Team C' };
const match = (id, sideA, sideB) => ({ id, status: 'scheduled', sideA, sideB });
const POOL_MATCHES = [match('Pool A-0', A, B), match('Pool A-1', B, C), match('Pool A-2', A, C)];
const COMP = { id: 'comp-1', name: 'Team Event', kind: 'team', format: 'pools', status: 'active', teamSize: 3, players: [A, B, C] };
// The panel is handed matches shaped as the competition page builds them.
const PANEL_MATCHES = POOL_MATCHES.map((m) => ({ ...m, compId: 'comp-1', phase: 'pool', poolName: 'Pool A' }));
const PANEL_MATCH = PANEL_MATCHES[2];

const MATCH_KEY = 'bc.lineupDraft.v1:comp-1:team-a:match:Pool A-2';
const USE_PREVIOUS = "Use the previous match's lineup";
const QUEUED_LINE = 'A save of this lineup is still waiting to be sent.';
const REMOVED_UNREAD = 'Removed. The lineup this match now uses could not be read: try again.';
const NO_ANSWER = 'The lineup could not be read: the server did not answer. Check the connection and try again.';

let AdminTeamLineupsList;
let MatchLineupSideEditor;
let MatchLineupPanel;
let saved;
let api;

beforeEach(async () => {
  sessionStorage.clear();
  saved = {
    API: window.API, confirmDialog: window.confirmDialog,
    subscribeUnsentWrites: window.subscribeUnsentWrites, subscribeSyncStatus: window.subscribeSyncStatus,
    compMatches: window.compMatches,
  };
  window.confirmDialog = vi.fn().mockResolvedValue(true);
  window.compMatches = () => [];
  api = {
    fetchTeamLineup: vi.fn().mockResolvedValue(STARTING),
    fetchLineupInForce: vi.fn().mockResolvedValue(CARRIED),
    fetchSquads: vi.fn().mockResolvedValue({ 'team-a': SQUAD }),
    putTeamLineup: vi.fn().mockImplementation((_c, _t, _r, positions, _pw, memberIds) => Promise.resolve({ positions, memberIds })),
    putMatchLineup: vi.fn().mockImplementation((_c, _t, _m, positions, _pw, memberIds) => Promise.resolve({ positions, memberIds })),
    deleteMatchLineup: vi.fn().mockResolvedValue(true),
    deleteTeamLineup: vi.fn().mockResolvedValue(true),
    queuedLineupSave: vi.fn().mockReturnValue(false),
  };
  window.API = api;
  delete window.subscribeUnsentWrites;
  delete window.subscribeSyncStatus;
  ({ AdminTeamLineupsList } = await import('../../admin_lineup.jsx'));
  ({ MatchLineupSideEditor, MatchLineupPanel } = await import('../../admin_schedule_lineup.jsx'));
});

afterEach(() => {
  for (const [key, value] of Object.entries(saved)) {
    if (value === undefined) delete window[key]; else window[key] = value;
  }
});

async function mountPage(props = {}) {
  let utils;
  await act(async () => {
    utils = render(<AdminTeamLineupsList comp={COMP} poolMatches={POOL_MATCHES} password="pw" showToast={vi.fn()} {...props} />);
  });
  await act(async () => { await Promise.resolve(); });
  return utils;
}

async function mountPanel() {
  let utils;
  await act(async () => {
    utils = render(
      <MatchLineupSideEditor comp={COMP} team={A} match={PANEL_MATCH} allMatches={PANEL_MATCHES} password="pw" showToast={vi.fn()} />
    );
  });
  await act(async () => { await Promise.resolve(); });
  return utils;
}

async function chooseTarget(utils, value) {
  await act(async () => { fireEvent.change(utils.getByLabelText('Lineup for'), { target: { value } }); });
  await act(async () => { await Promise.resolve(); });
}

async function click(button) {
  await act(async () => { fireEvent.click(button); });
  await act(async () => { await Promise.resolve(); });
}

async function typeName(utils, position, name) {
  await act(async () => {
    const input = utils.getByLabelText(`${position} player`);
    fireEvent.focus(input);
    fireEvent.change(input, { target: { value: name } });
    fireEvent.keyDown(input, { key: 'Enter' });
  });
}

const saveButton = (utils) => utils.getByRole('button', { name: /^Save lineup$/ });
const tryAgain = (utils) => utils.getByRole('button', { name: 'Try again' });
const useButton = (utils) => utils.queryByRole('button', { name: USE_PREVIOUS });
const pageSelect = (utils, position) => utils.getByTestId(`lineup-position-${position}`);

describe('the at-court panel, for a lineup that could not be read', () => {
  beforeEach(() => { api.fetchLineupInForce.mockRejectedValue(new Error('competition not found')); });

  it('shows the problem with a Try again, and turns Save and the boxes off', async () => {
    const utils = await mountPanel();
    expect(utils.getByText('competition not found')).toBeTruthy();
    expect(saveButton(utils).disabled).toBe(true);
    expect(utils.getByLabelText('1 player').disabled).toBe(true);
    // Nothing to say about where the lineup was saved, when it was not read.
    expect(utils.queryByText('No lineup saved yet')).toBeNull();
    expect(useButton(utils)).toBeNull();
    await click(saveButton(utils));
    expect(api.putMatchLineup).not.toHaveBeenCalled();
  });

  it('reads the lineup again on Try again, and the form then works', async () => {
    const utils = await mountPanel();
    api.fetchLineupInForce.mockResolvedValue(CARRIED);

    await click(tryAgain(utils));

    expect(api.fetchLineupInForce).toHaveBeenCalledTimes(2);
    expect(utils.queryByText('competition not found')).toBeNull();
    expect(utils.queryByRole('button', { name: 'Try again' })).toBeNull();
    expect(utils.getByLabelText('1 player').value).toBe('Aoki');
    expect(utils.getByLabelText('1 player').disabled).toBe(false);
    expect(utils.getByText('Same as Pool A · Match 1')).toBeTruthy();
    await typeName(utils, 1, 'Mori');
    expect(saveButton(utils).disabled).toBe(false);
  });

  it('offers the draft kept for the lineup once Try again has read it, as a fresh open would', async () => {
    api.fetchLineupInForce.mockResolvedValue(CARRIED);
    const first = await mountPanel();
    await typeName(first, 1, 'Mori');
    first.unmount();
    const kept = sessionStorage.getItem(MATCH_KEY);
    expect(kept).not.toBeNull();

    api.fetchLineupInForce.mockRejectedValue(new Error('competition not found'));
    const failed = await mountPanel();
    expect(sessionStorage.getItem(MATCH_KEY)).toBe(kept);
    expect(failed.queryByTestId('match-lineup-draft-team-a')).toBeNull();

    api.fetchLineupInForce.mockResolvedValue(CARRIED);
    await click(tryAgain(failed));

    expect(failed.getByLabelText('1 player').value).toBe('Mori');
    expect(failed.getByTestId('match-lineup-draft-team-a').textContent).toContain('Unsaved lineup changes restored');
  });

  it('says a read the server never answered in a plain sentence, with a Try again, never the browser\'s "Failed to fetch"', async () => {
    api.fetchLineupInForce.mockRejectedValue(new TypeError('Failed to fetch'));
    const utils = await mountPanel();
    expect(utils.getByText(NO_ANSWER)).toBeTruthy();
    expect(utils.queryByText('Failed to fetch')).toBeNull();
    expect(tryAgain(utils)).toBeTruthy();
    expect(saveButton(utils).disabled).toBe(true);
  });

  it('keeps the problem when the second read fails too', async () => {
    const utils = await mountPanel();
    api.fetchLineupInForce.mockRejectedValue(new Error('still down'));
    await click(tryAgain(utils));
    expect(utils.getByText('still down')).toBeTruthy();
    expect(saveButton(utils).disabled).toBe(true);
  });

  it('puts Try again in a plain .btn, so the coarse-pointer floor reaches it through the class', async () => {
    const utils = await mountPanel();
    const button = tryAgain(utils);
    expect(button.className.split(/\s+/)).toContain('btn');
    expect(button.hasAttribute('style')).toBe(false);
    expect(button.getAttribute('type')).toBe('button');
  });
});

describe('the Lineups page, for a lineup that could not be read', () => {
  it('a starting lineup: the problem stays, Save and the pickers are off, and Try again reads it', async () => {
    api.fetchTeamLineup.mockRejectedValue(new Error('Failed to load lineup'));
    const utils = await mountPage();
    expect(utils.getByText('Failed to load lineup')).toBeTruthy();
    expect(saveButton(utils).disabled).toBe(true);
    expect(pageSelect(utils, 1).disabled).toBe(true);
    await click(saveButton(utils));
    expect(api.putTeamLineup).not.toHaveBeenCalled();

    api.fetchTeamLineup.mockResolvedValue(STARTING);
    await click(tryAgain(utils));

    expect(utils.queryByText('Failed to load lineup')).toBeNull();
    expect(pageSelect(utils, 1).disabled).toBe(false);
    expect(pageSelect(utils, 1).value).toBe('mem-1');
    // Read, and unchanged: nothing to save.
    expect(saveButton(utils).disabled).toBe(true);
    await act(async () => { fireEvent.change(pageSelect(utils, 2), { target: { value: 'mem-4' } }); });
    expect(saveButton(utils).disabled).toBe(false);
  });

  it('a match: the same', async () => {
    api.fetchLineupInForce.mockRejectedValue(new Error('competition not found'));
    const utils = await mountPage();
    await chooseTarget(utils, 'Pool A-2');
    expect(utils.getByText('competition not found')).toBeTruthy();
    expect(saveButton(utils).disabled).toBe(true);
    expect(utils.queryByTestId('lineup-source')).toBeNull();
    expect(useButton(utils)).toBeNull();

    api.fetchLineupInForce.mockResolvedValue(CARRIED);
    await click(tryAgain(utils));
    expect(utils.getByText('Same as Pool A · Match 1')).toBeTruthy();
    expect(saveButton(utils).disabled).toBe(true);
  });

  it('says a read the server never answered in a plain sentence, with a Try again, never the browser\'s "Failed to fetch"', async () => {
    api.fetchTeamLineup.mockRejectedValue(new TypeError('Failed to fetch'));
    const utils = await mountPage();
    expect(utils.getByText(NO_ANSWER)).toBeTruthy();
    expect(utils.queryByText('Failed to fetch')).toBeNull();
    expect(tryAgain(utils)).toBeTruthy();
    expect(saveButton(utils).disabled).toBe(true);
  });

  it('an untouched starting lineup that was read has nothing to save', async () => {
    const utils = await mountPage();
    expect(saveButton(utils).disabled).toBe(true);
    expect(saveButton(utils).getAttribute('title')).toBe('No changes to save');
  });
});

describe('"Use the previous match\'s lineup" while a save of that lineup is still queued', () => {
  beforeEach(() => {
    api.fetchLineupInForce.mockResolvedValue(OWN);
    api.queuedLineupSave.mockReturnValue(true);
  });

  it('is disabled in the panel, with the reason as a line (a title never shows on a touchscreen)', async () => {
    const utils = await mountPanel();
    const button = useButton(utils);
    expect(button.disabled).toBe(true);
    expect(button.hasAttribute('title')).toBe(false);
    const line = utils.getByText(QUEUED_LINE);
    expect(line.getAttribute('role')).toBe('status');
    expect(api.queuedLineupSave).toHaveBeenCalledWith('comp-1', 'team-a', { matchId: 'Pool A-2' });
    await click(button);
    expect(window.confirmDialog).not.toHaveBeenCalled();
    expect(api.deleteMatchLineup).not.toHaveBeenCalled();
  });

  it('is disabled on the Lineups page too', async () => {
    const utils = await mountPage();
    await chooseTarget(utils, 'Pool A-2');
    expect(useButton(utils).disabled).toBe(true);
    expect(utils.getByText(QUEUED_LINE)).toBeTruthy();
    await click(useButton(utils));
    expect(window.confirmDialog).not.toHaveBeenCalled();
  });

  it('says nothing about a queued save on a lineup that is not the match\'s own', async () => {
    api.fetchLineupInForce.mockResolvedValue(CARRIED);
    const utils = await mountPanel();
    expect(utils.queryByText(QUEUED_LINE)).toBeNull();
  });

  it('is enabled again, and the line goes, once the save has gone out', async () => {
    const listeners = new Set();
    window.subscribeUnsentWrites = (fn) => { listeners.add(fn); fn(); return () => listeners.delete(fn); };
    const utils = await mountPanel();
    expect(useButton(utils).disabled).toBe(true);

    api.queuedLineupSave.mockReturnValue(false);
    await act(async () => { listeners.forEach((fn) => fn()); });

    expect(useButton(utils).disabled).toBe(false);
    expect(utils.queryByText(QUEUED_LINE)).toBeNull();
  });
});

describe('a removal whose re-read fails', () => {
  async function removeInPanel() {
    api.fetchLineupInForce.mockResolvedValueOnce(OWN).mockRejectedValue(new Error('network down'));
    const utils = await mountPanel();
    await typeName(utils, 1, 'Mori');
    expect(sessionStorage.getItem(MATCH_KEY)).not.toBeNull();
    await click(useButton(utils));
    return utils;
  }

  it('leaves the panel with an empty, unread form: not the removed lineup, and nothing to save', async () => {
    const utils = await removeInPanel();

    expect(api.deleteMatchLineup).toHaveBeenCalledWith('comp-1', 'team-a', 'Pool A-2', 'pw');
    expect(utils.getByText(REMOVED_UNREAD)).toBeTruthy();
    expect(utils.queryByText('Lineup for this match')).toBeNull();
    expect(useButton(utils)).toBeNull();
    expect(utils.getByLabelText('1 player').value).toBe('');
    expect(utils.getByLabelText('1 player').disabled).toBe(true);
    expect(saveButton(utils).disabled).toBe(true);
    // What was typed was made against the lineup that went.
    expect(sessionStorage.getItem(MATCH_KEY)).toBeNull();
    expect(utils.queryByRole('button', { name: 'Discard' })).toBeNull();
  });

  it('shows what the match now carries once Try again reads it', async () => {
    const utils = await removeInPanel();
    api.fetchLineupInForce.mockResolvedValue(CARRIED);

    await click(tryAgain(utils));

    expect(utils.queryByText(REMOVED_UNREAD)).toBeNull();
    expect(utils.getByText('Same as Pool A · Match 1')).toBeTruthy();
    expect(utils.getByLabelText('1 player').value).toBe('Aoki');
    expect(saveButton(utils).disabled).toBe(true);
  });

  it('does the same on the Lineups page', async () => {
    api.fetchLineupInForce.mockResolvedValueOnce(OWN).mockRejectedValue(new Error('network down'));
    const utils = await mountPage();
    await chooseTarget(utils, 'Pool A-2');
    await act(async () => { fireEvent.change(pageSelect(utils, 1), { target: { value: 'mem-4' } }); });

    await click(useButton(utils));

    expect(utils.getByText(REMOVED_UNREAD)).toBeTruthy();
    expect(utils.queryByText('Lineup for this match')).toBeNull();
    expect(pageSelect(utils, 1).value).toBe('');
    expect(pageSelect(utils, 1).disabled).toBe(true);
    expect(saveButton(utils).disabled).toBe(true);
    expect(sessionStorage.getItem(MATCH_KEY)).toBeNull();
  });
});

describe('the confirm of "Use the previous match\'s lineup"', () => {
  it('names the team and the match, the first-match case and the unsaved changes it discards', async () => {
    api.fetchLineupInForce.mockResolvedValue(OWN);
    const utils = await mountPage();
    await chooseTarget(utils, 'Pool A-2');

    await click(useButton(utils));

    const { message, confirmLabel } = window.confirmDialog.mock.calls[0][0];
    expect(message).toContain('Use the lineup Team A had before Pool A · Match 3?');
    expect(message).toContain('so Team A carries the lineup of its previous match, or its starting lineup if this is its first match.');
    expect(message).toContain('Later matches that have no lineup of their own follow too.');
    expect(message).toContain('Unsaved changes here are discarded.');
    expect(confirmLabel).toBe('Use previous lineup');
  });
});

describe('"Not restored, the lineup changed since"', () => {
  async function openWithStaleDraft() {
    const first = await mountPanel();
    await typeName(first, 1, 'Mori');
    first.unmount();
    api.fetchLineupInForce.mockResolvedValue(SAVED_ELSEWHERE);
    const again = await mountPanel();
    expect(again.getByTestId('match-lineup-draft-team-a').textContent).toBe('Not restored, the lineup changed since: Mori');
    return again;
  }

  it('goes once the operator changes the lineup', async () => {
    const utils = await openWithStaleDraft();
    await typeName(utils, 2, 'Mori');
    expect(utils.queryByTestId('match-lineup-draft-team-a')).toBeNull();
  });

  it('goes once the operator saves it', async () => {
    const utils = await openWithStaleDraft();
    await typeName(utils, 2, 'Mori');
    await click(saveButton(utils));
    expect(api.putMatchLineup).toHaveBeenCalledTimes(1);
    expect(utils.queryByTestId('match-lineup-draft-team-a')).toBeNull();
  });

  it('stays while the operator has done nothing to the lineup', async () => {
    const utils = await openWithStaleDraft();
    await act(async () => { await Promise.resolve(); });
    expect(utils.getByTestId('match-lineup-draft-team-a')).toBeTruthy();
  });
});

describe('the source line, on both editors', () => {
  it('is rendered by the same component: one class for the emphasis, no inline size on either', async () => {
    api.fetchLineupInForce.mockResolvedValue(OWN);
    const panel = await mountPanel();
    const fromPanel = panel.getByTestId('match-lineup-source-team-a');
    panel.unmount();
    const page = await mountPage();
    await chooseTarget(page, 'Pool A-2');
    const fromPage = page.getByTestId('lineup-source');

    expect(fromPanel.className).toBe(fromPage.className);
    expect(fromPanel.className.split(/\s+/)).toContain('lineup-source__label--own');
    expect(fromPanel.hasAttribute('style')).toBe(false);
    expect(fromPage.hasAttribute('style')).toBe(false);
    expect(fromPanel.closest('.lineup-source')).not.toBeNull();
    expect(fromPage.closest('.lineup-source')).not.toBeNull();
  });
});

describe('the at-court panel and the matches it names a carried lineup from', () => {
  const TOURNAMENT = { competitions: [COMP] };
  async function mountWholePanel() {
    let utils;
    await act(async () => {
      utils = render(<MatchLineupPanel match={PANEL_MATCH} tournament={TOURNAMENT} password="pw" showToast={vi.fn()} onClose={vi.fn()} />);
    });
    await act(async () => { await Promise.resolve(); });
    return utils;
  }

  it('builds the competition\'s matches to name a lineup carried from another match', async () => {
    window.compMatches = vi.fn(() => PANEL_MATCHES);
    const utils = await mountWholePanel();
    // Both sides carry the lineup of Team A's earlier match.
    expect(utils.getAllByText('Same as Pool A · Match 1').length).toBe(2);
    expect(window.compMatches).toHaveBeenCalled();
    expect(window.compMatches.mock.calls.every(([comp]) => comp.id === 'comp-1')).toBe(true);
  });

  it('builds nothing when no side carries a lineup from another match', async () => {
    window.compMatches = vi.fn(() => PANEL_MATCHES);
    api.fetchLineupInForce.mockResolvedValue(OWN);
    const utils = await mountWholePanel();
    expect(utils.getAllByText('Lineup for this match').length).toBeGreaterThan(0);
    expect(window.compMatches).not.toHaveBeenCalled();
  });
});
