// A name typed in a lineup editor is resolved against the team's members only once
// their read is over, and nothing waits for that read (bc-evts stage 3 review 2, R1,
// the operator's rule: "ALL goes to the outbox ... but never blocks the UI"), in a
// real DOM. Resolved against none while the read is out:
//
//  - a new name would be minted where the member's seeded slot is free, instead of
//    naming that slot (the unnamed member whose number the position was shown with), and
//  - the name of a member the team already has would be refused by the server as a
//    second member of that name.
//
// So while the read is out the at-court panel's Save saves the typed name alone, with
// no id and the members-unavailable warning, and the Lineups page's add row refuses
// the add at once, keeping the typed name. Once a list has been shown a typed name is
// placed by the member it belongs to; after a read that failed the editor goes on
// without the members, as it always did, and says so.

import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { answered } from '../helpers/team_members.js';
import { lineupPutStub } from '../helpers/lineup_server.js';
import { membersNotReadNotice } from '../../lineup_draft.jsx';

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
const putOf = (call) => ({ positions: call[3], memberIds: call[5] });
const announce = () => act(async () => {
  window.dispatchEvent(new CustomEvent('lineup-updated', { detail: { competitionId: 'comp-1' } }));
});
const warning = (utils) => utils.getByTestId('match-lineup-warning-team-a').textContent;

describe('the at-court panel: a Save with a typed name never waits for the team\'s members', () => {
  it('saves the typed name alone, with the warning, while the members are being read', async () => {
    api.fetchSquads.mockReturnValue(new Promise(() => {}));
    const utils = await mountPanel();
    await typeName(utils, 1, 'Mori');

    await click(saveButton(utils));

    expect(api.addTeamMember, 'nothing is minted against no list').not.toHaveBeenCalled();
    expect(api.renameTeamMember).not.toHaveBeenCalled();
    expect(putOf(api.putMatchLineup.mock.calls[0]), 'the name goes with no id').toEqual({ positions: { 1: 'Mori' }, memberIds: undefined });
    expect(saveButton(utils), 'nothing waits').toBeTruthy();
    expect(warning(utils)).toContain('team member list could not be loaded');
  });

  it('places a member the team has by its id once the members were read', async () => {
    const utils = await mountPanel();
    await typeName(utils, 1, 'Mori');

    await click(saveButton(utils));

    expect(api.addTeamMember, 'Mori is the member the team has, not a new one').not.toHaveBeenCalled();
    expect(api.renameTeamMember).not.toHaveBeenCalled();
    expect(putOf(api.putMatchLineup.mock.calls[0])).toEqual({ positions: { 1: 'Mori' }, memberIds: { 1: 'mem-4' } });
  });

  it('names the unnamed member seeded for the position once the members were read', async () => {
    api.fetchLineupInForce.mockResolvedValue(matchLineup(EMPTY));
    api.fetchSquads.mockResolvedValue({ 'team-a': FRESH });
    const utils = await mountPanel();
    await typeName(utils, 1, 'Mori');

    await click(saveButton(utils));

    expect(api.renameTeamMember, 'the slot the position was shown with is named').toHaveBeenCalledWith('comp-1', 'team-a', 'mem-1', 'Mori', 'pw', expect.any(Object));
    expect(api.addTeamMember, 'no member is added beside it').not.toHaveBeenCalled();
    expect(putOf(api.putMatchLineup.mock.calls[0])).toEqual({ positions: { 1: 'Mori' }, memberIds: { 1: 'mem-1' } });
  });

  // Judged before the resolver names or mints anything: Kato is not minted for a Save
  // the typed Mori refuses.
  it('refuses a lineup that would field a member twice before anything is minted', async () => {
    api.fetchLineupInForce.mockResolvedValue(matchLineup({
      positions: { 1: 'Mori', 2: 'Sato', 3: 'Ito' }, memberIds: { 1: 'mem-4', 2: 'mem-2', 3: 'mem-3' },
    }));
    const utils = await mountPanel();
    await typeName(utils, 2, 'Mori');
    await typeName(utils, 3, 'Kato');
    await click(saveButton(utils));

    expect(utils.getByText('Mori is already at Position 1.')).toBeTruthy();
    expect(api.addTeamMember, 'Kato is not minted for a Save that is refused').not.toHaveBeenCalled();
    expect(api.renameTeamMember).not.toHaveBeenCalled();
    expect(api.putMatchLineup).not.toHaveBeenCalled();
  });

  it('needs no members for a Save with no typed name: a position that was picked or left alone', async () => {
    api.fetchSquads.mockReturnValue(new Promise(() => {}));
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

  it('places the name by its id once a list has been shown, whatever read is out', async () => {
    const utils = await mountPanel();
    api.fetchSquads.mockReturnValue(new Promise(() => {}));
    await announce();
    await typeName(utils, 1, 'Mori');

    await click(saveButton(utils));

    expect(putOf(api.putMatchLineup.mock.calls[0])).toEqual({ positions: { 1: 'Mori' }, memberIds: { 1: 'mem-4' } });
  });
});

describe('the Lineups page: an add row never waits for the team\'s members', () => {
  const addRow = async (utils, name) => {
    await act(async () => { fireEvent.change(utils.getByTestId('lineup-position-1'), { target: { value: '__add__' } }); });
    await act(async () => { fireEvent.change(utils.getByLabelText('New member name for 1'), { target: { value: name } }); });
  };
  const addButton = (utils) => utils.getByRole('button', { name: /^(Add|Adding…)$/ });

  it('refuses the add at once while the members are being read, and keeps the typed name', async () => {
    api.fetchSquads.mockReturnValue(new Promise(() => {}));
    const utils = await mountPage();
    await addRow(utils, 'Mori');

    await click(addButton(utils));

    expect(api.addTeamMember, 'nothing is added').not.toHaveBeenCalled();
    expect(window.confirmDialog, 'nor asked').not.toHaveBeenCalled();
    expect(addButton(utils).disabled, 'nothing waits').toBe(false);
    expect(utils.getByText(membersNotReadNotice('Mori'))).toBeTruthy();
    expect(utils.getByLabelText('New member name for 1').value).toBe('Mori');
  });

  it('selects a member the team has, once the members were read, instead of adding the name again', async () => {
    const utils = await mountPage();
    await addRow(utils, 'Mori');

    await click(addButton(utils));

    expect(api.addTeamMember, 'Mori is the member the team has, not a new one').not.toHaveBeenCalled();
    expect(window.confirmDialog, 'selecting a member that exists asks nothing').not.toHaveBeenCalled();
    expect(utils.getByTestId('lineup-position-1').value).toBe('mem-4');
  });

  it('names the unnamed member seeded for the position, once the members were read, instead of adding one', async () => {
    api.fetchTeamLineup.mockResolvedValue(startingLineup(EMPTY));
    api.fetchSquads.mockResolvedValue({ 'team-a': FRESH });
    const utils = await mountPage();
    await addRow(utils, 'Mori');

    await click(addButton(utils));

    expect(window.confirmDialog).toHaveBeenCalledTimes(1);
    expect(api.renameTeamMember).toHaveBeenCalledWith('comp-1', 'team-a', 'mem-1', 'Mori', 'pw', expect.any(Object));
    expect(api.addTeamMember).not.toHaveBeenCalled();
  });

  it('adds the member when the members could not be read, as it always did', async () => {
    api.fetchSquads.mockRejectedValue(new TypeError('Failed to fetch'));
    const utils = await mountPage();
    await addRow(utils, 'Mori');

    await click(addButton(utils));

    expect(window.confirmDialog).toHaveBeenCalledTimes(1);
    expect(api.addTeamMember).toHaveBeenCalledTimes(1);
  });
});
