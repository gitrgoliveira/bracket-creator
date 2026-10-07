// A name typed in a lineup editor is resolved against the team's members, so the
// editor waits for them to have been read before it resolves one (useLineupForm's
// waitForMembers), in a real DOM. Resolved against none:
//
//  - a new name is minted where the member's seeded slot is free, instead of naming
//    that slot (the unnamed member whose number the position was shown with), and
//  - the name of a member the team already has is refused by the server as a second
//    member of that name, so the lineup is saved with the name and no member id.
//
// The wait is bounded by the deadline of any request, and ends when the read fails:
// the editor then goes on without the members, as it always did, and says so. Once a
// list has been shown nothing waits, whatever read is out.
//
// Both surfaces that resolve a typed name are covered: the at-court panel's Save and
// the Lineups page's add row.

import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { FETCH_TIMEOUT_MS } from '../../write_result.jsx';
import { answered } from '../helpers/team_members.js';
import { lineupPutStub } from '../helpers/lineup_server.js';

const TEAM_MEMBERS = [
  { id: 'mem-1', index: 1, name: 'Aoki' },
  { id: 'mem-2', index: 2, name: 'Sato' },
  { id: 'mem-3', index: 3, name: 'Ito' },
  { id: 'mem-4', index: 4, name: 'Mori' },
];
// A team just drawn: three members nobody has named yet, and nothing in the lineup.
const FRESH = [1, 2, 3].map((index) => ({ id: `mem-${index}`, index, name: '' }));
const NAMES = { positions: { 1: 'Aoki', 2: 'Sato', 3: 'Ito' }, memberIds: { 1: 'mem-1', 2: 'mem-2', 3: 'mem-3' } };
const EMPTY = { positions: {}, memberIds: {} };
const lineupFor = (extra) => ({ teamId: 'team-a', competitionId: 'comp-1', ...NAMES, saved: true, ...extra });
const STARTING = lineupFor({ round: 0, sourceRound: 0 });
const matchLineup = (stored) => lineupFor({ ...stored, matchId: 'Pool A-2', sourceMatchId: 'Pool A-0' });
const startingLineup = (stored) => lineupFor({ ...stored, round: 0, sourceRound: 0 });

const A = { id: 'team-a', name: 'Team A' };
const B = { id: 'team-b', name: 'Team B' };
const C = { id: 'team-c', name: 'Team C' };
const match = (id, sideA, sideB) => ({ id, status: 'scheduled', sideA, sideB });
const POOL_MATCHES = [match('Pool A-0', A, B), match('Pool A-1', B, C), match('Pool A-2', A, C)];
const COMP = { id: 'comp-1', name: 'Team Event', kind: 'team', format: 'pools', status: 'active', teamSize: 3, players: [A, B, C] };
const PANEL_MATCHES = POOL_MATCHES.map((m) => ({ ...m, compId: 'comp-1', phase: 'pool', poolName: 'Pool A' }));
const PANEL_MATCH = PANEL_MATCHES[2];

let AdminTeamLineupsList;
let MatchLineupSideEditor;
let saved;
let api;
let showToast;

beforeEach(async () => {
  sessionStorage.clear();
  saved = {
    API: window.API, confirmDialog: window.confirmDialog,
    subscribeUnsentWrites: window.subscribeUnsentWrites, subscribeSyncStatus: window.subscribeSyncStatus,
    compMatches: window.compMatches,
  };
  window.confirmDialog = vi.fn().mockResolvedValue(true);
  window.compMatches = () => [];
  showToast = vi.fn();
  api = {
    fetchTeamLineup: vi.fn().mockResolvedValue(STARTING),
    fetchLineupInForce: vi.fn().mockResolvedValue(lineupFor({ matchId: 'Pool A-0', sourceMatchId: 'Pool A-0' })),
    fetchSquads: vi.fn().mockResolvedValue({ 'team-a': TEAM_MEMBERS }),
    // A save names the positions it changed, and the server answers the lineup it holds then.
    putTeamLineup: lineupPutStub({ positions: { ...NAMES.positions }, memberIds: { ...NAMES.memberIds } }),
    putMatchLineup: lineupPutStub({ positions: { ...NAMES.positions }, memberIds: { ...NAMES.memberIds } }),
    // The server answers a member write with the member it holds, stamped.
    addTeamMember: vi.fn().mockImplementation((_c, _t, name) => Promise.resolve(answered({ id: 'mem-minted', index: 6 }, { name }))),
    renameTeamMember: vi.fn().mockImplementation((_c, _t, id, name) => Promise.resolve(answered({ id, index: Number(id.slice(4)) }, { name }))),
    queuedLineupSave: vi.fn().mockReturnValue(false),
  };
  window.API = api;
  delete window.subscribeUnsentWrites;
  delete window.subscribeSyncStatus;
  ({ AdminTeamLineupsList } = await import('../../admin_lineup.jsx'));
  ({ MatchLineupSideEditor } = await import('../../admin_schedule_lineup.jsx'));
});

afterEach(() => {
  for (const [key, value] of Object.entries(saved)) {
    if (value === undefined) delete window[key]; else window[key] = value;
  }
});

function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((res, rej) => { resolve = res; reject = rej; });
  return { promise, resolve, reject };
}

async function flush() {
  for (let i = 0; i < 6; i += 1) {
    await act(async () => { await Promise.resolve(); });
  }
}

async function mountPanel() {
  let utils;
  await act(async () => {
    utils = render(
      <MatchLineupSideEditor comp={COMP} team={A} match={PANEL_MATCH} allMatches={PANEL_MATCHES} password="pw" showToast={showToast} />
    );
  });
  await flush();
  return utils;
}

async function mountPage() {
  let utils;
  await act(async () => {
    utils = render(<AdminTeamLineupsList comp={COMP} poolMatches={POOL_MATCHES} password="pw" showToast={showToast} />);
  });
  await flush();
  return utils;
}

async function click(button) {
  await act(async () => { fireEvent.click(button); });
  await flush();
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
const savingButton = (utils) => utils.getByRole('button', { name: 'Saving…' });
const putOf = (call) => ({ positions: call[3], memberIds: call[5] });
const announce = () => act(async () => {
  window.dispatchEvent(new CustomEvent('lineup-updated', { detail: { competitionId: 'comp-1' } }));
});
const warning = (utils) => utils.getByTestId('match-lineup-warning-team-a').textContent;

describe('the at-court panel: a Save with a typed name waits for the team\'s members', () => {
  it('places a member the team has by its id when the name is typed before the members were read', async () => {
    const members = deferred();
    api.fetchSquads.mockReturnValue(members.promise);
    const utils = await mountPanel();
    await typeName(utils, 1, 'Mori');

    await click(saveButton(utils));

    expect(api.addTeamMember, 'nothing is added while the members are out').not.toHaveBeenCalled();
    expect(api.putMatchLineup, 'nor written').not.toHaveBeenCalled();
    expect(savingButton(utils).disabled, 'the Save is waiting for the members').toBe(true);

    await act(async () => { members.resolve({ 'team-a': TEAM_MEMBERS }); });
    await flush();

    expect(api.addTeamMember, 'Mori is the member the team has, not a new one').not.toHaveBeenCalled();
    expect(api.renameTeamMember).not.toHaveBeenCalled();
    expect(putOf(api.putMatchLineup.mock.calls[0])).toEqual({ positions: { 1: 'Mori' }, memberIds: { 1: 'mem-4' } });
  });

  it('names the unnamed member seeded for the position when a new name is typed before the members were read', async () => {
    api.fetchLineupInForce.mockResolvedValue(matchLineup(EMPTY));
    const members = deferred();
    api.fetchSquads.mockReturnValue(members.promise);
    const utils = await mountPanel();
    await typeName(utils, 1, 'Mori');

    await click(saveButton(utils));
    expect(api.addTeamMember).not.toHaveBeenCalled();
    expect(api.renameTeamMember).not.toHaveBeenCalled();
    await act(async () => { members.resolve({ 'team-a': FRESH }); });
    await flush();

    expect(api.renameTeamMember, 'the slot the position was shown with is named').toHaveBeenCalledWith('comp-1', 'team-a', 'mem-1', 'Mori', 'pw');
    expect(api.addTeamMember, 'no member is added beside it').not.toHaveBeenCalled();
    expect(putOf(api.putMatchLineup.mock.calls[0])).toEqual({ positions: { 1: 'Mori' }, memberIds: { 1: 'mem-1' } });
  });

  // The Save is judged on the members once they were read, before the resolver names or
  // mints anything: against none, Kato would be minted for a Save the typed Mori refuses.
  it('refuses a lineup that would field a member twice before anything is minted, judged on the members once they were read', async () => {
    api.fetchLineupInForce.mockResolvedValue(matchLineup({
      positions: { 1: 'Mori', 2: 'Sato', 3: 'Ito' }, memberIds: { 1: 'mem-4', 2: 'mem-2', 3: 'mem-3' },
    }));
    const members = deferred();
    api.fetchSquads.mockReturnValue(members.promise);
    const utils = await mountPanel();
    await typeName(utils, 2, 'Mori');
    await typeName(utils, 3, 'Kato');
    await click(saveButton(utils));

    await act(async () => { members.resolve({ 'team-a': TEAM_MEMBERS }); });
    await flush();

    expect(utils.getByText('Mori is already at Position 1.')).toBeTruthy();
    expect(api.addTeamMember, 'Kato is not minted for a Save that is refused').not.toHaveBeenCalled();
    expect(api.renameTeamMember).not.toHaveBeenCalled();
    expect(api.putMatchLineup).not.toHaveBeenCalled();
  });

  it('does not wait for a Save with no typed name: a position that was picked or left alone needs no members', async () => {
    const members = deferred();
    api.fetchSquads.mockReturnValue(members.promise);
    const utils = await mountPanel();
    await act(async () => { fireEvent.click(utils.getAllByRole('button', { name: 'Clear player' })[0]); });

    await click(saveButton(utils));

    // The cleared position goes as its empty name, which the server needs to be there.
    expect(putOf(api.putMatchLineup.mock.calls[0])).toEqual({ positions: { 1: '' } });
  });

  it('goes on without the members when they could not be read, as it always did, and says so', async () => {
    api.fetchSquads.mockRejectedValue(new TypeError('Failed to fetch'));
    const utils = await mountPanel();
    await typeName(utils, 1, 'Mori');

    await click(saveButton(utils));

    expect(api.addTeamMember, 'the name is minted against no list, at once').toHaveBeenCalledTimes(1);
    expect(putOf(api.putMatchLineup.mock.calls[0]).memberIds).toEqual({ 1: 'mem-minted' });
    expect(warning(utils)).toContain('team member list could not be loaded');
  });

  it('goes on, and says so, when the read fails while the Save waits for it', async () => {
    const members = deferred();
    api.fetchSquads.mockReturnValue(members.promise);
    const utils = await mountPanel();
    await typeName(utils, 1, 'Mori');
    await click(saveButton(utils));
    expect(api.addTeamMember).not.toHaveBeenCalled();

    await act(async () => { members.reject(new TypeError('Failed to fetch')); });
    await flush();

    expect(api.addTeamMember).toHaveBeenCalledTimes(1);
    expect(putOf(api.putMatchLineup.mock.calls[0]).memberIds).toEqual({ 1: 'mem-minted' });
    expect(warning(utils)).toContain('team member list could not be loaded');
  });

  it('goes on, and says so, when the read is not answered by its deadline', async () => {
    api.fetchSquads.mockReturnValue(new Promise(() => {}));
    const utils = await mountPanel();
    await typeName(utils, 1, 'Mori');

    vi.useFakeTimers();
    try {
      await act(async () => { fireEvent.click(saveButton(utils)); });
      await act(async () => { vi.advanceTimersByTime(FETCH_TIMEOUT_MS - 1); });
      expect(api.addTeamMember, 'nothing is minted while the read may still answer').not.toHaveBeenCalled();
      await act(async () => { vi.advanceTimersByTime(2); });
      await flush();
    } finally {
      vi.useRealTimers();
    }

    expect(api.addTeamMember).toHaveBeenCalledTimes(1);
    expect(putOf(api.putMatchLineup.mock.calls[0]).memberIds).toEqual({ 1: 'mem-minted' });
    expect(warning(utils)).toContain('team member list could not be loaded');
  });

  it('does not wait once a list has been shown, whatever read is out', async () => {
    const utils = await mountPanel();
    // A lineup another device saved makes the editor read the members again; never answered.
    api.fetchSquads.mockReturnValue(new Promise(() => {}));
    await announce();
    await typeName(utils, 2, 'Kato');

    await click(saveButton(utils));

    expect(api.addTeamMember, 'Kato is minted against the list shown, at once').toHaveBeenCalledTimes(1);
    expect(putOf(api.putMatchLineup.mock.calls[0]).memberIds).toEqual({ 2: 'mem-minted' });
  });
});

describe('the Lineups page: an add row waits for the team\'s members', () => {
  const addRow = async (utils, name) => {
    await act(async () => { fireEvent.change(utils.getByTestId('lineup-position-1'), { target: { value: '__add__' } }); });
    await act(async () => { fireEvent.change(utils.getByLabelText('New member name for 1'), { target: { value: name } }); });
  };
  const addButton = (utils) => utils.getByRole('button', { name: /^(Add|Adding…)$/ });

  it('selects a member the team has, once the members were read, instead of adding the name again', async () => {
    const members = deferred();
    api.fetchSquads.mockReturnValue(members.promise);
    const utils = await mountPage();
    await addRow(utils, 'Mori');

    await click(addButton(utils));

    expect(api.addTeamMember, 'nothing is added while the members are out').not.toHaveBeenCalled();
    expect(window.confirmDialog, 'nor asked').not.toHaveBeenCalled();
    expect(addButton(utils).textContent, 'the add waits for the members').toBe('Adding…');
    expect(addButton(utils).disabled).toBe(true);

    await act(async () => { members.resolve({ 'team-a': TEAM_MEMBERS }); });
    await flush();

    expect(api.addTeamMember, 'Mori is the member the team has, not a new one').not.toHaveBeenCalled();
    expect(window.confirmDialog, 'selecting a member that exists asks nothing').not.toHaveBeenCalled();
    expect(utils.getByTestId('lineup-position-1').value).toBe('mem-4');
  });

  it('names the unnamed member seeded for the position, once the members were read, instead of adding one', async () => {
    api.fetchTeamLineup.mockResolvedValue(startingLineup(EMPTY));
    const members = deferred();
    api.fetchSquads.mockReturnValue(members.promise);
    const utils = await mountPage();
    await addRow(utils, 'Mori');

    await click(addButton(utils));
    expect(window.confirmDialog).not.toHaveBeenCalled();
    await act(async () => { members.resolve({ 'team-a': FRESH }); });
    await flush();

    expect(window.confirmDialog).toHaveBeenCalledTimes(1);
    expect(window.confirmDialog.mock.calls[0][0].message, 'the slot is named').not.toContain('as a new member of');
    expect(api.renameTeamMember).toHaveBeenCalledWith('comp-1', 'team-a', 'mem-1', 'Mori', 'pw');
    expect(api.addTeamMember).not.toHaveBeenCalled();
    expect(utils.getByTestId('lineup-position-1').value).toBe('mem-1');
  });

  it('judges the placements as they are once the wait is over: a position cleared meanwhile no longer holds the member', async () => {
    const members = deferred();
    api.fetchSquads.mockReturnValue(members.promise);
    const utils = await mountPage();
    // Sato holds position 2 of the starting lineup, and is typed for position 1.
    await addRow(utils, 'Sato');
    await click(addButton(utils));
    await act(async () => { fireEvent.change(utils.getByTestId('lineup-position-2'), { target: { value: '' } }); });

    await act(async () => { members.resolve({ 'team-a': TEAM_MEMBERS }); });
    await flush();

    expect(utils.queryByText(/is already at/), 'Sato is no longer at position 2').toBeNull();
    expect(api.addTeamMember).not.toHaveBeenCalled();
    expect(utils.getByTestId('lineup-position-1').value).toBe('mem-2');
  });

  it('adds the member when the members could not be read, as it always did', async () => {
    api.fetchSquads.mockRejectedValue(new TypeError('Failed to fetch'));
    const utils = await mountPage();
    await addRow(utils, 'Mori');

    await click(addButton(utils));

    expect(window.confirmDialog.mock.calls[0][0].message).toContain('as a new member of');
    expect(api.addTeamMember).toHaveBeenCalledWith('comp-1', 'team-a', 'Mori', 'pw');
    expect(utils.getByTestId('lineup-position-1').value).toBe('mem-minted');
  });

  it('adds the member when the read fails while the add waits for it', async () => {
    const members = deferred();
    api.fetchSquads.mockReturnValue(members.promise);
    const utils = await mountPage();
    await addRow(utils, 'Mori');
    await click(addButton(utils));
    expect(api.addTeamMember).not.toHaveBeenCalled();

    await act(async () => { members.reject(new TypeError('Failed to fetch')); });
    await flush();

    expect(api.addTeamMember).toHaveBeenCalledWith('comp-1', 'team-a', 'Mori', 'pw');
    expect(utils.getByTestId('lineup-position-1').value, 'the member is placed and the add row closed').toBe('mem-minted');
  });

  it('adds the member when the read is not answered by its deadline', async () => {
    api.fetchSquads.mockReturnValue(new Promise(() => {}));
    const utils = await mountPage();
    await addRow(utils, 'Mori');

    vi.useFakeTimers();
    try {
      await act(async () => { fireEvent.click(addButton(utils)); });
      await act(async () => { vi.advanceTimersByTime(FETCH_TIMEOUT_MS - 1); });
      expect(api.addTeamMember).not.toHaveBeenCalled();
      await act(async () => { vi.advanceTimersByTime(2); });
      await flush();
    } finally {
      vi.useRealTimers();
    }

    expect(api.addTeamMember).toHaveBeenCalledWith('comp-1', 'team-a', 'Mori', 'pw');
  });

  it('does not wait once a list has been shown, whatever read is out', async () => {
    const utils = await mountPage();
    api.fetchSquads.mockReturnValue(new Promise(() => {}));
    await announce();
    await addRow(utils, 'Kato');

    await click(addButton(utils));

    expect(api.addTeamMember, 'Kato is added against the list shown, at once').toHaveBeenCalledWith('comp-1', 'team-a', 'Kato', 'pw');
  });
});
