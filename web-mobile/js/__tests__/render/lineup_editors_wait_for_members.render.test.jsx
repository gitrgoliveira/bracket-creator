// A name typed in a lineup editor is resolved against the team's members, so it waits
// for their read (operator decision V1, round ship-kno1-review-2), in a real DOM, bounded
// by the read's deadline (FETCH_TIMEOUT_MS). Resolved against none:
//
//  - a new name would be minted where the member's seeded slot is free, instead of
//    naming that slot (the unnamed member whose number the position was shown with), and
//  - the name of a member the team already has would be refused by the server as a
//    second member of that name.
//
// So the at-court panel's Save waits under "Saving…" and the Lineups page's add waits
// under "Adding…". Once a list has been shown a typed name is placed by the member it
// belongs to, or added, and nothing more is said. After a read that failed or was not
// answered, whatever the cause (team-members.yaml that the server cannot read
// included), the name is saved by name only, no member is added, and the one note after
// the save is "Saved by name only: ..." (U2).

import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { answered } from '../helpers/team_members.js';
import { lineupPutStub } from '../helpers/lineup_server.js';
import { MEMBERS_UNAVAILABLE_NOTICE } from '../../lineup_draft.jsx';
import { FETCH_TIMEOUT_MS } from '../../write_result.jsx';

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
    const input = utils.getByLabelText(`${({ 1: 'Senpo', 2: 'Chuken', 3: 'Taisho' })[position] ?? position} player`);
    fireEvent.focus(input);
    fireEvent.change(input, { target: { value: name } });
    fireEvent.keyDown(input, { key: 'Enter' });
  });
}

const saveButton = (utils) => utils.getByRole('button', { name: /^(Save lineup|Saving…)$/ });
const putOf = (call) => ({ positions: call[3], memberIds: call[5] });
const announce = () => act(async () => {
  window.dispatchEvent(new CustomEvent('lineup-updated', { detail: { competitionId: 'comp-1' } }));
});
const warning = (utils) => utils.getByTestId('match-lineup-warning-team-a').textContent;
const SAVED_BY_NAME_ONLY = 'Saved by name only: the team member list could not be loaded. Scores will still record normally.';

function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((res, rej) => { resolve = res; reject = rej; });
  return { promise, resolve, reject };
}

// The ways a members read ends without a list: it fails, the server says the file
// cannot be read, or it is not answered by its deadline.
const NO_LIST = [
  ['fails', () => api.fetchSquads.mockRejectedValue(new TypeError('Failed to fetch'))],
  ['is refused because team-members.yaml cannot be read', () => api.fetchSquads.mockRejectedValue(
    Object.assign(new Error('The team members could not be read. The organiser needs to check team-members.yaml in the tournament data.'), { code: 'team_members_unreadable' }),
  )],
];

// Runs `body` with fake timers, from the editor's mount (the members read it makes as
// it opens has the deadline), and `deadline` carries that read past it.
async function pastTheDeadline(body) {
  vi.useFakeTimers();
  try {
    await body(async () => { await act(async () => { await vi.advanceTimersByTimeAsync(FETCH_TIMEOUT_MS); }); });
  } finally {
    vi.useRealTimers();
  }
}

describe('the at-court panel: a Save with a typed name waits for the team\'s members', () => {
  it('waits under Saving… while the members are being read, then places a member the team has by its id, saying nothing', async () => {
    const members = deferred();
    api.fetchSquads.mockReturnValue(members.promise);
    const utils = await mountPanel();
    await typeName(utils, 1, 'Mori');

    await click(saveButton(utils));

    expect(api.putMatchLineup, 'nothing is saved while the read is out').not.toHaveBeenCalled();
    expect(saveButton(utils).textContent).toBe('Saving…');
    expect(saveButton(utils).disabled).toBe(true);
    ['Senpo', 'Chuken', 'Taisho'].forEach((p) => {
      expect(utils.getByLabelText(`${p} player`).disabled, `the ${p} box is locked while Save waits`).toBe(true);
    });

    await act(async () => { members.resolve({ 'team-a': TEAM_MEMBERS }); });
    await flush();

    expect(api.addTeamMember, 'Mori is the member the team has, not a new one').not.toHaveBeenCalled();
    expect(putOf(api.putMatchLineup.mock.calls[0])).toEqual({ positions: { 1: 'Mori' }, memberIds: { 1: 'mem-4' } });
    expect(utils.queryByTestId('match-lineup-warning-team-a'), 'a linked name says nothing').toBeNull();
  });

  it('places a member the team has by its id once the members were read', async () => {
    const utils = await mountPanel();
    await typeName(utils, 1, 'Mori');

    await click(saveButton(utils));

    expect(api.addTeamMember, 'Mori is the member the team has, not a new one').not.toHaveBeenCalled();
    expect(api.renameTeamMember).not.toHaveBeenCalled();
    expect(putOf(api.putMatchLineup.mock.calls[0])).toEqual({ positions: { 1: 'Mori' }, memberIds: { 1: 'mem-4' } });
  });

  it('adds a name that is new to the team, and says nothing of a name only save', async () => {
    const utils = await mountPanel();
    await typeName(utils, 1, 'Kato');

    await click(saveButton(utils));

    expect(api.addTeamMember).toHaveBeenCalledTimes(1);
    expect(putOf(api.putMatchLineup.mock.calls[0]).memberIds).toEqual({ 1: 'mem-minted' });
    expect(utils.queryByTestId('match-lineup-warning-team-a'), 'an added member is no name only save').toBeNull();
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

    expect(utils.getByText('Mori is already at Senpo.')).toBeTruthy();
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

  it.each(NO_LIST)('saves a typed name by name only when the read %s, adds nobody, and says so in one sentence', async (_why, failRead) => {
    failRead();
    const utils = await mountPanel();
    expect(utils.getByTestId('lineup-members-unavailable').textContent).not.toBe('');
    await typeName(utils, 1, 'Kato');

    await click(saveButton(utils));

    expect(api.addTeamMember, 'no member is added without a list').not.toHaveBeenCalled();
    expect(api.renameTeamMember).not.toHaveBeenCalled();
    expect(putOf(api.putMatchLineup.mock.calls[0]), 'the name goes with no id').toEqual({ positions: { 1: 'Kato' }, memberIds: undefined });
    expect(warning(utils)).toBe(SAVED_BY_NAME_ONLY);
  });

  it('saves a name that waited by name only once the read is not answered by its deadline, and adds nobody', async () => {
    api.fetchSquads.mockReturnValue(new Promise(() => {}));
    let utils;
    await pastTheDeadline(async (deadline) => {
      utils = await mountPanel();
      await typeName(utils, 1, 'Kato');
      await act(async () => { fireEvent.click(saveButton(utils)); });
      expect(api.putMatchLineup, 'it waits').not.toHaveBeenCalled();
      await deadline();
    });
    await flush();

    expect(api.addTeamMember).not.toHaveBeenCalled();
    expect(putOf(api.putMatchLineup.mock.calls[0])).toEqual({ positions: { 1: 'Kato' }, memberIds: undefined });
    expect(warning(utils)).toBe(SAVED_BY_NAME_ONLY);
    expect(utils.getByTestId('lineup-members-unavailable').textContent).toBe(MEMBERS_UNAVAILABLE_NOTICE);
  });

  // A read made again (the password changed after a sign-in) gets a new wait: a Save
  // made while it is out waits for its list instead of going by name only.
  it('waits for a members read made again after one that failed', async () => {
    api.fetchSquads.mockRejectedValue(new Error('Invalid password'));
    const utils = await mountPanel();
    expect(utils.getByTestId('lineup-members-unavailable')).toBeTruthy();
    const members = deferred();
    api.fetchSquads.mockReturnValue(members.promise);
    await act(async () => {
      utils.rerender(<MatchLineupSideEditor comp={COMP} team={A} match={PANEL_MATCH} allMatches={PANEL_MATCHES} password="new-pw" showToast={showToast} />);
    });
    await flush();
    await typeName(utils, 1, 'Mori');

    await click(saveButton(utils));
    expect(api.putMatchLineup, 'the Save waits for the new read').not.toHaveBeenCalled();
    expect(saveButton(utils).textContent).toBe('Saving…');

    await act(async () => { members.resolve({ 'team-a': TEAM_MEMBERS }); });
    await flush();
    expect(putOf(api.putMatchLineup.mock.calls[0])).toEqual({ positions: { 1: 'Mori' }, memberIds: { 1: 'mem-4' } });
    expect(utils.queryByTestId('match-lineup-warning-team-a')).toBeNull();
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

describe('the Lineups page: an add row waits for the team\'s members', () => {
  const addRow = async (utils, name) => {
    await act(async () => { fireEvent.change(utils.getByTestId('lineup-position-1'), { target: { value: '__add__' } }); });
    await act(async () => { fireEvent.change(utils.getByLabelText('New member name for Senpo'), { target: { value: name } }); });
  };
  const addButton = (utils) => utils.getByRole('button', { name: /^(Add|Adding…)$/ });
  const pageSave = (utils) => utils.getByRole('button', { name: /^(Save lineup|Saving…)$/ });

  it('waits under Adding… while the members are being read, then selects a member the team has', async () => {
    const members = deferred();
    api.fetchSquads.mockReturnValue(members.promise);
    const utils = await mountPage();
    await addRow(utils, 'Mori');

    await click(addButton(utils));

    expect(addButton(utils).textContent).toBe('Adding…');
    expect(addButton(utils).disabled).toBe(true);
    expect(api.addTeamMember, 'nothing is added while the read is out').not.toHaveBeenCalled();

    await act(async () => { members.resolve({ 'team-a': TEAM_MEMBERS }); });
    await flush();

    expect(api.addTeamMember, 'Mori is the member the team has, not a new one').not.toHaveBeenCalled();
    expect(window.confirmDialog, 'selecting a member that exists asks nothing').not.toHaveBeenCalled();
    expect(utils.getByTestId('lineup-position-1').value).toBe('mem-4');
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

  it('adds a name that is new to the team, and its Save says nothing of a name only save', async () => {
    const utils = await mountPage();
    await addRow(utils, 'Kato');
    await click(addButton(utils));
    expect(api.addTeamMember).toHaveBeenCalledTimes(1);

    await click(pageSave(utils));

    expect(putOf(api.putTeamLineup.mock.calls[0]).memberIds).toEqual({ 1: 'mem-minted' });
    expect(utils.queryByTestId('lineup-member-warning')).toBeNull();
  });

  it.each(NO_LIST)('places the name by name only when the read %s, adds nobody, and its Save says so in one sentence', async (_why, failRead) => {
    failRead();
    const utils = await mountPage();
    await addRow(utils, 'Kato');

    await click(addButton(utils));

    expect(window.confirmDialog, 'nothing to confirm: nobody is added').not.toHaveBeenCalled();
    expect(api.addTeamMember).not.toHaveBeenCalled();
    expect(api.renameTeamMember).not.toHaveBeenCalled();
    expect(utils.getByText('Unresolved: Kato'), 'the position holds the name alone').toBeTruthy();

    await click(pageSave(utils));

    expect(putOf(api.putTeamLineup.mock.calls[0])).toEqual({ positions: { 1: 'Kato' }, memberIds: undefined });
    expect(utils.getByTestId('lineup-member-warning').textContent).toBe(SAVED_BY_NAME_ONLY);
  });

  // The note speaks for what the add did, not for the list as it is at Save: a list
  // that arrives after the name was placed by name only does not take it away.
  it('gives the note for a name placed by name only, even once the list has arrived', async () => {
    api.fetchSquads.mockRejectedValue(new TypeError('Failed to fetch'));
    const utils = await mountPage();
    await addRow(utils, 'Kato');
    await click(addButton(utils));
    expect(utils.getByText('Unresolved: Kato')).toBeTruthy();

    api.fetchSquads.mockResolvedValue({ 'team-a': TEAM_MEMBERS });
    await announce();
    await flush();
    expect(utils.queryByTestId('lineup-members-unavailable'), 'the list has arrived').toBeNull();

    await click(pageSave(utils));

    expect(putOf(api.putTeamLineup.mock.calls[0])).toEqual({ positions: { 1: 'Kato' }, memberIds: undefined });
    expect(utils.getByTestId('lineup-member-warning').textContent).toBe(SAVED_BY_NAME_ONLY);
  });

  // A name with no member id restored from a draft was not placed by name only by this
  // page's add, so its Save says nothing of it, while the list is down too.
  it('gives no note for a name with no member id restored from a draft', async () => {
    sessionStorage.setItem('bc.lineupDraft.v1:comp-1:team-a:start', JSON.stringify({
      savedAt: Date.now(),
      baseline: NAMES,
      current: { positions: { ...NAMES.positions, 1: 'Kato' }, memberIds: { 2: 'mem-2', 3: 'mem-3' } },
    }));
    api.fetchSquads.mockRejectedValue(new TypeError('Failed to fetch'));
    const utils = await mountPage();
    expect(utils.getByText('Unsaved lineup changes restored')).toBeTruthy();
    expect(utils.getByTestId('lineup-members-unavailable')).toBeTruthy();

    await click(pageSave(utils));

    expect(putOf(api.putTeamLineup.mock.calls[0])).toEqual({ positions: { 1: 'Kato' }, memberIds: undefined });
    expect(utils.queryByTestId('lineup-member-warning')).toBeNull();
  });

  // A read made again (the password changed after a sign-in) gets a new wait: an add
  // made while it is out waits for its list instead of going by name only.
  it('waits for a members read made again after one that failed', async () => {
    api.fetchSquads.mockRejectedValue(new Error('Invalid password'));
    let utils;
    await act(async () => {
      utils = render(<AdminTeamLineupsList comp={COMP} poolMatches={POOL_MATCHES} password="pw" showToast={showToast} />);
    });
    await flush();
    expect(utils.getByTestId('lineup-members-unavailable')).toBeTruthy();
    const members = deferred();
    api.fetchSquads.mockReturnValue(members.promise);
    await act(async () => {
      utils.rerender(<AdminTeamLineupsList comp={COMP} poolMatches={POOL_MATCHES} password="new-pw" showToast={showToast} />);
    });
    await flush();

    await addRow(utils, 'Mori');
    await click(addButton(utils));
    expect(addButton(utils).textContent, 'the add waits for the new read').toBe('Adding…');

    await act(async () => { members.resolve({ 'team-a': TEAM_MEMBERS }); });
    await flush();
    expect(api.addTeamMember).not.toHaveBeenCalled();
    expect(utils.getByTestId('lineup-position-1').value, 'Mori is selected by her member').toBe('mem-4');
  });

  it('places a name that waited by name only once the read is not answered by its deadline', async () => {
    api.fetchSquads.mockReturnValue(new Promise(() => {}));
    let utils;
    await pastTheDeadline(async (deadline) => {
      utils = await mountPage();
      await addRow(utils, 'Kato');
      await act(async () => { fireEvent.click(addButton(utils)); });
      expect(addButton(utils).textContent).toBe('Adding…');
      await deadline();
    });
    await flush();

    expect(api.addTeamMember).not.toHaveBeenCalled();
    expect(utils.getByText('Unresolved: Kato')).toBeTruthy();
    expect(utils.getByTestId('lineup-members-unavailable').textContent).toBe(MEMBERS_UNAVAILABLE_NOTICE);
  });
});
