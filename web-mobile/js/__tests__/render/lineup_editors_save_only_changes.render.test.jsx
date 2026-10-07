// What a Save carries in both lineup editors (the at-court panel's
// MatchLineupSideEditor and the Lineups page), in a real DOM: the positions the
// operator changed and nothing else (operator decision 2026-10-07, "Only changed
// positions"). The server puts them on the lineup it holds when the save arrives, so a
// change another device made to a position the operator left alone stays; the stand-in
// for the server here (lineupPutStub) does exactly that.
//
//  - the PUT carries the changed positions alone, named in `changed`, a cleared one as
//    its empty name; no read of the lineup is made for it, so a Save with no connection is
//    built, and queued, like any other;
//  - what the server answers, the whole lineup it holds, is what the editor shows after
//    the Save, a position left alone that another device changed included;
//  - an untouched position is never re-resolved or minted, even when it names a member
//    this editor's list does not hold (one another device created);
//  - a lineup that would field one member twice is refused before anything is written,
//    naming the position that was already there (the one the operator did not change; of
//    two they changed, the one they picked the member for, not the box they typed the name
//    into), when the form shows it; when another device made it so meanwhile the server
//    refuses it, and the editor shows the refusal and keeps the operator's change;
//  - the panel refuses such a lineup before its resolver renames or mints for a
//    typed name, and hands the resolver the ids of the lineup as the form shows it; for a
//    typed name it counts the member the resolver will place (the member the name
//    belongs to, else the unnamed member the position holds), so a Save the resolver
//    would place without a clash is not refused;
//  - while a Save waits for its write nothing that changes the lineup or the team's
//    members is usable, and Save waits while one of those is out;
//  - a lineup followed from another device is shown with the team's members read
//    again, so a member created elsewhere is not shown as an empty slot; a change
//    announced makes an edited form read them too, so a name typed for a member created
//    elsewhere is placed by its id, not minted a second time. A Save asks the server for
//    neither the lineup nor the members.

import React from 'react';
import { render, act, fireEvent, within } from '@testing-library/react';
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { answered, namedLater } from '../helpers/team_members.js';
import { lineupPutStub } from '../helpers/lineup_server.js';
import { QUEUED_NOTICE, QUEUED_UNSAVED_NOTICE } from '../../write_result.jsx';

const SQUAD = [
  { id: 'mem-1', index: 1, name: 'Aoki' },
  { id: 'mem-2', index: 2, name: 'Sato' },
  { id: 'mem-3', index: 3, name: 'Ito' },
  { id: 'mem-4', index: 4, name: 'Mori' },
];
// A member another device created after this editor read the team's members.
const ZED = { id: 'mem-9', index: 5, name: 'Zed' };
const KATO = { id: 'mem-5', index: 6, name: 'Kato' };
const NAMES = { positions: { 1: 'Aoki', 2: 'Sato', 3: 'Ito' }, memberIds: { 1: 'mem-1', 2: 'mem-2', 3: 'mem-3' } };
const lineupFor = (extra) => ({ teamId: 'team-a', competitionId: 'comp-1', ...NAMES, saved: true, ...extra });
const STARTING = lineupFor({ round: 0, sourceRound: 0 });
const CARRIED = lineupFor({ matchId: 'Pool A-0', sourceMatchId: 'Pool A-0' });
// What the server holds once another device has set position 2 to a member this
// editor has never heard of.
const KATO_AT_2 = { positions: { 1: 'Aoki', 2: 'Kato', 3: 'Ito' }, memberIds: { 1: 'mem-1', 2: 'mem-5', 3: 'mem-3' } };
const ZED_AT_3 = { positions: { 1: 'Aoki', 2: 'Sato', 3: 'Zed' }, memberIds: { 1: 'mem-1', 2: 'mem-2', 3: 'mem-9' } };
// A member nobody has named yet (a reserve slot), and a lineup that places it unnamed.
const BLANK = { id: 'mem-6', index: 6, name: '' };
const MORI_AT_1 = { positions: { 1: 'Mori', 2: 'Sato', 3: 'Ito' }, memberIds: { 1: 'mem-4', 2: 'mem-2', 3: 'mem-3' } };
const BLANK_AT_3 = { positions: { 1: 'Aoki', 2: 'Sato', 3: '' }, memberIds: { 1: 'mem-1', 2: 'mem-2', 3: 'mem-6' } };
const BLANK_AT_1 = { positions: { 1: '', 2: 'Sato', 3: 'Ito' }, memberIds: { 1: 'mem-6', 2: 'mem-2', 3: 'mem-3' } };
// A team just drawn: three members nobody has named yet, and nothing in the lineup.
const FRESH = [1, 2, 3].map((index) => ({ id: `mem-${index}`, index, name: '' }));
const EMPTY = { positions: {}, memberIds: {} };
// The member of a fixture by its id: what the server answers a write of a member with is
// that member, named as asked, with the stamp it gave the write.
const memberById = (id) => [...SQUAD, ZED, KATO, BLANK].find((m) => m.id === id) || { id, index: 0 };
const renamedAs = (id, name) => answered(memberById(id), { name });

const A = { id: 'team-a', name: 'Team A' };
const B = { id: 'team-b', name: 'Team B' };
const C = { id: 'team-c', name: 'Team C' };
const match = (id, sideA, sideB) => ({ id, status: 'scheduled', sideA, sideB });
const POOL_MATCHES = [match('Pool A-0', A, B), match('Pool A-1', B, C), match('Pool A-2', A, C)];
const COMP = { id: 'comp-1', name: 'Team Event', kind: 'team', format: 'pools', status: 'active', teamSize: 3, players: [A, B, C] };
// A team member's name can be cleared until the competition has started; a drawn one has its matches.
const DRAWN_COMP = { ...COMP, status: 'draw-ready' };
const DRAFT_KEY = 'bc.lineupDraft.v1:comp-1:team-a:';
const PANEL_MATCHES = POOL_MATCHES.map((m) => ({ ...m, compId: 'comp-1', phase: 'pool', poolName: 'Pool A' }));
const PANEL_MATCH = PANEL_MATCHES[2];

let AdminTeamLineupsList;
let MatchLineupSideEditor;
let realResolver;
let resolver;
let saved;
let api;
let showToast;
// What the server holds for the match and for the team's starting lineup. A save lands its
// changed positions on these and the stand-in answers the whole lineup (lineupPutStub); a test
// sets them to what another device saved (serverHolds, startingHolds).
let serverMatch;
let serverStarting;

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
  serverMatch = { positions: { ...NAMES.positions }, memberIds: { ...NAMES.memberIds } };
  serverStarting = { positions: { ...NAMES.positions }, memberIds: { ...NAMES.memberIds } };
  api = {
    fetchTeamLineup: vi.fn().mockResolvedValue(STARTING),
    fetchLineupInForce: vi.fn().mockResolvedValue(CARRIED),
    fetchSquads: vi.fn().mockResolvedValue({ 'team-a': SQUAD }),
    putTeamLineup: lineupPutStub(serverStarting),
    putMatchLineup: lineupPutStub(serverMatch),
    addTeamMember: vi.fn().mockImplementation((_c, _t, name) => Promise.resolve(answered({ id: 'mem-minted', index: 6 }, { name }))),
    renameTeamMember: vi.fn().mockImplementation((_c, _t, id, name) => Promise.resolve(renamedAs(id, name))),
    queuedLineupSave: vi.fn().mockReturnValue(false),
  };
  window.API = api;
  delete window.subscribeUnsentWrites;
  delete window.subscribeSyncStatus;
  ({ AdminTeamLineupsList } = await import('../../admin_lineup.jsx'));
  ({ MatchLineupSideEditor } = await import('../../admin_schedule_lineup.jsx'));
  // The panel resolves a typed name through window.AdminLineupHelpers at call time:
  // wrapped, so a test can see which positions it was asked about.
  realResolver ||= window.AdminLineupHelpers.resolveMemberIdsForPositions;
  resolver = vi.fn(realResolver);
  window.AdminLineupHelpers.resolveMemberIdsForPositions = resolver;
});

afterEach(() => {
  window.AdminLineupHelpers.resolveMemberIdsForPositions = realResolver;
  for (const [key, value] of Object.entries(saved)) {
    if (value === undefined) delete window[key]; else window[key] = value;
  }
});

function deferred() {
  let resolve;
  const promise = new Promise((res) => { resolve = res; });
  return { promise, resolve };
}

async function flush() {
  for (let i = 0; i < 6; i += 1) {
    await act(async () => { await Promise.resolve(); });
  }
}

async function mountPage(comp = COMP) {
  let utils;
  await act(async () => {
    utils = render(<AdminTeamLineupsList comp={comp} poolMatches={POOL_MATCHES} password="pw" showToast={showToast} />);
  });
  await flush();
  return utils;
}

async function mountPanel(comp = COMP) {
  let utils;
  await act(async () => {
    utils = render(
      <MatchLineupSideEditor comp={comp} team={A} match={PANEL_MATCH} allMatches={PANEL_MATCHES} password="pw" showToast={showToast} />
    );
  });
  await flush();
  return utils;
}

async function chooseTarget(utils, value) {
  await act(async () => { fireEvent.change(utils.getByLabelText('Lineup for'), { target: { value } }); });
  await flush();
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

const pick = (utils, position, memberId) => act(async () => {
  fireEvent.change(utils.getByTestId(`lineup-position-${position}`), { target: { value: memberId } });
});

// The panel's name box offers the team's members in a list: opens it and takes one.
async function pickFromList(utils, position, name) {
  const row = utils.getByTestId(`match-lineup-pos-team-a-${position}`);
  await act(async () => { fireEvent.focus(within(row).getByLabelText(`${position} player`)); });
  await act(async () => { fireEvent.click(within(row).getByText(name)); });
}

const panelRow = (utils, position) => within(utils.getByTestId(`match-lineup-pos-team-a-${position}`));
const memberRow = (utils, id) => within(utils.getByTestId(`squad-member-${id}`));
// A draft the editor left behind: the lineup as read, and the operator's name at position 1.
const keepDraft = (suffix, baseline) => sessionStorage.setItem(`${DRAFT_KEY}${suffix}`, JSON.stringify({
  savedAt: Date.now(),
  baseline,
  current: { positions: { ...baseline.positions, 1: 'Mori' }, memberIds: { ...baseline.memberIds, 1: 'mem-4' } },
}));

const saveButton = (utils) => utils.getByRole('button', { name: /^Save lineup$/ });
const savingButton = (utils) => utils.getByRole('button', { name: 'Saving…' });
const announce = () => act(async () => {
  window.dispatchEvent(new CustomEvent('lineup-updated', { detail: { competitionId: 'comp-1' } }));
});
// What a save sent: the positions and ids it carries, and the list that names the changed ones.
const putOf = (call) => ({ positions: call[3], memberIds: call[5] });
const changedOf = (call) => call[6];
// What the stub answers a read of the match's lineup with, and of the starting lineup.
const matchLineup = (stored) => lineupFor({ ...stored, matchId: 'Pool A-2', sourceMatchId: 'Pool A-0' });
const startingLineup = (stored) => lineupFor({ ...stored, round: 0, sourceRound: 0 });
// The lineup the server holds, as another device left it: what the editor reads from now
// on and what a save lands on.
const serverHolds = (stored) => {
  api.fetchLineupInForce.mockResolvedValue(matchLineup(stored));
  Object.assign(serverMatch, { positions: { ...stored.positions }, memberIds: { ...stored.memberIds } });
};
const startingHolds = (stored) => {
  api.fetchTeamLineup.mockResolvedValue(startingLineup(stored));
  Object.assign(serverStarting, { positions: { ...stored.positions }, memberIds: { ...stored.memberIds } });
};
// Another device changed the lineup after the editor read it: the editor does not know yet.
const changedElsewhere = (stored) => Object.assign(serverMatch, { positions: { ...stored.positions }, memberIds: { ...stored.memberIds } });
const changedElsewhereStarting = (stored) => Object.assign(serverStarting, { positions: { ...stored.positions }, memberIds: { ...stored.memberIds } });

describe('the at-court panel', () => {
  it('carries the changed position alone, naming it, and shows what the server answered: a position another device changed meanwhile stays and shows', async () => {
    const utils = await mountPanel();
    await typeName(utils, 1, 'Mori');
    // Another device changed position 2 after the panel read the lineup.
    changedElsewhere(KATO_AT_2);

    await click(saveButton(utils));

    expect(api.fetchLineupInForce, 'no read of the lineup is made for the Save').toHaveBeenCalledTimes(1);
    expect(api.fetchSquads, 'nor of the members, a typed name included').toHaveBeenCalledTimes(1);
    expect(api.putMatchLineup).toHaveBeenCalledTimes(1);
    expect(putOf(api.putMatchLineup.mock.calls[0])).toEqual({ positions: { 1: 'Mori' }, memberIds: { 1: 'mem-4' } });
    expect(changedOf(api.putMatchLineup.mock.calls[0])).toEqual(['1']);
    expect(api.addTeamMember, 'the position left alone is not minted').not.toHaveBeenCalled();
    // The server's answer is what is shown afterwards: the lineup it holds, Kato included.
    expect(utils.getByLabelText('2 player').value).toBe('Kato');
    expect(utils.getByLabelText('1 player').value).toBe('Mori');
    expect(saveButton(utils).disabled, 'saved: nothing left to save').toBe(true);
  });

  it('never resolves or mints a position left alone, even when it names a member this list has never heard of', async () => {
    serverHolds(ZED_AT_3);
    const utils = await mountPanel();
    expect(utils.getByLabelText('3 player').value).toBe('Zed');
    await typeName(utils, 1, 'Mori');

    await click(saveButton(utils));

    expect(putOf(api.putMatchLineup.mock.calls[0])).toEqual({ positions: { 1: 'Mori' }, memberIds: { 1: 'mem-4' } });
    expect(api.addTeamMember).not.toHaveBeenCalled();
    expect(api.renameTeamMember).not.toHaveBeenCalled();
    const asked = resolver.mock.calls.flatMap(([, , positions]) => Object.keys(positions));
    expect(asked, 'only the position the operator typed over was resolved').toEqual(['1']);
    expect(utils.getByLabelText('3 player').value, 'and the position left alone is still shown').toBe('Zed');
  });

  it('still resolves a name typed over a position, with the id that position holds now', async () => {
    const utils = await mountPanel();
    await typeName(utils, 2, 'Kato');

    await click(saveButton(utils));

    expect(api.addTeamMember).toHaveBeenCalledTimes(1);
    expect(api.addTeamMember.mock.calls[0][2]).toBe('Kato');
    const asked = resolver.mock.calls.flatMap(([, , positions]) => Object.keys(positions));
    expect(asked).toEqual(['2']);
    expect(putOf(api.putMatchLineup.mock.calls[0])).toEqual({ positions: { 2: 'Kato' }, memberIds: { 2: 'mem-minted' } });
  });

  it('places a name typed over an edited form by the id of a member another device created: the members are read again when the change is announced, edits or not', async () => {
    const utils = await mountPanel();
    await typeName(utils, 1, 'Mori');
    // Meanwhile another device creates Kato and announces a lineup change.
    api.fetchSquads.mockResolvedValue({ 'team-a': [...SQUAD, KATO] });
    await announce();
    await typeName(utils, 2, 'Kato');

    await click(saveButton(utils));

    expect(api.addTeamMember, 'Kato is the member another device created, not a new one').not.toHaveBeenCalled();
    expect(putOf(api.putMatchLineup.mock.calls[0])).toEqual({
      positions: { 1: 'Mori', 2: 'Kato' },
      memberIds: { 1: 'mem-4', 2: 'mem-5' },
    });
    expect(changedOf(api.putMatchLineup.mock.calls[0])).toEqual(['1', '2']);
  });

  it('does not put the list from before a mint back: the members read of a followed lineup, still out when Save is tapped, is ended with it', async () => {
    const utils = await mountPanel();
    // A lineup change from another device starts a follow while the form is untouched.
    const followed = deferred();
    api.fetchSquads.mockReturnValue(followed.promise);
    await announce();
    await typeName(utils, 1, 'Kato');
    const write = deferred();
    api.putMatchLineup.mockReturnValue(write.promise);

    await click(saveButton(utils));

    expect(api.addTeamMember, 'the typed name is minted').toHaveBeenCalledTimes(1);
    expect(api.putMatchLineup).toHaveBeenCalledTimes(1);

    // The follow's read answers while the write is out, with the list from before the mint.
    await act(async () => { followed.resolve({ 'team-a': SQUAD }); });
    await flush();
    await act(async () => {
      write.resolve({ positions: { 1: 'Kato', 2: 'Sato', 3: 'Ito' }, memberIds: { 1: 'mem-minted', 2: 'mem-2', 3: 'mem-3' } });
    });
    await flush();

    expect(panelRow(utils, 1).getByText('Slot 6'), 'the member the Save minted is still listed').toBeTruthy();
    expect(panelRow(utils, 1).getByRole('button', { name: 'Rename 1 player' })).toBeTruthy();
  });

  it('places the member a rename gave its new name by its id when that name is typed: a read of the team\'s members begun before the rename, answering after it with the old name, does not undo it', async () => {
    const utils = await mountPanel();
    // A lineup change from another device starts a read of the team's members, still out
    // when the rename lands (the lineup it reads already carries the new spelling).
    const older = deferred();
    api.fetchSquads.mockReturnValueOnce(older.promise);
    api.fetchLineupInForce.mockResolvedValue(matchLineup({ positions: { ...NAMES.positions, 3: 'Itoh' } }));
    await announce();
    // The operator renames Ito (position 3) to Itoh; the server answers the member with the
    // stamp of that write, and no read is made after it.
    await click(panelRow(utils, 3).getByRole('button', { name: 'Rename 3 player' }));
    await act(async () => { fireEvent.change(panelRow(utils, 3).getByLabelText('Rename 3 player'), { target: { value: 'Itoh' } }); });
    await click(panelRow(utils, 3).getByRole('button', { name: 'Save' }));
    expect(api.renameTeamMember).toHaveBeenCalledWith('comp-1', 'team-a', 'mem-3', 'Itoh', 'pw');
    // The read begun before it answers now, with the old name.
    await act(async () => { older.resolve({ 'team-a': SQUAD }); });
    await flush();
    // The operator takes Itoh off position 3 and types the new name there again.
    await act(async () => { fireEvent.click(within(utils.getByTestId('match-lineup-pos-team-a-3')).getByRole('button', { name: 'Clear player' })); });
    await typeName(utils, 3, 'Itoh');

    await click(saveButton(utils));

    expect(api.addTeamMember, 'Itoh is the member that exists, not a new one').not.toHaveBeenCalled();
    expect(api.renameTeamMember, 'and no slot is named for it: only the rename the operator made').toHaveBeenCalledTimes(1);
    expect(putOf(api.putMatchLineup.mock.calls[0])).toEqual({ positions: { 3: 'Itoh' }, memberIds: { 3: 'mem-3' } });
  });

  it('clears a position the operator cleared, whatever another device put there meanwhile', async () => {
    const utils = await mountPanel();
    await act(async () => { fireEvent.click(within(utils.getByTestId('match-lineup-pos-team-a-1')).getByRole('button', { name: 'Clear player' })); });
    changedElsewhere({
      positions: { 1: 'Oda', 2: 'Kato', 3: 'Ito' }, memberIds: { 1: 'mem-8', 2: 'mem-5', 3: 'mem-3' },
    });

    await click(saveButton(utils));

    // The cleared position goes as its empty name, which the server needs to be there.
    expect(putOf(api.putMatchLineup.mock.calls[0])).toEqual({ positions: { 1: '' } });
    expect(changedOf(api.putMatchLineup.mock.calls[0])).toEqual(['1']);
    expect(serverMatch.positions).toEqual({ 2: 'Kato', 3: 'Ito' });
    expect(utils.getByLabelText('1 player').value).toBe('');
    expect(utils.getByLabelText('2 player').value).toBe('Kato');
  });

  describe('with no connection', () => {
    it('is built as it is online, naming the changed position, and queued, and the operator\'s change stays on screen', async () => {
      const utils = await mountPanel();
      await typeName(utils, 1, 'Mori');
      api.fetchLineupInForce.mockRejectedValue(new TypeError('Failed to fetch'));
      api.putMatchLineup.mockResolvedValue({ queued: true });

      await click(saveButton(utils));

      expect(api.fetchLineupInForce, 'nothing is read for the Save').toHaveBeenCalledTimes(1);
      expect(api.putMatchLineup.mock.calls[0][3]).toEqual({ 1: 'Mori' });
      expect(changedOf(api.putMatchLineup.mock.calls[0])).toEqual(['1']);
      // The held-write words every editor uses, with the pending icon: not a success.
      expect(showToast).toHaveBeenCalledWith(QUEUED_NOTICE, 'pending');
      expect(utils.getByLabelText('1 player').value).toBe('Mori');
      expect(saveButton(utils).disabled, 'still unsaved').toBe(false);
    });

    it('says to keep the page open when the browser could not store the save', async () => {
      const utils = await mountPanel();
      await typeName(utils, 1, 'Mori');
      api.putMatchLineup.mockResolvedValue({ queued: true, persisted: false });

      await click(saveButton(utils));

      expect(showToast).toHaveBeenCalledWith(QUEUED_UNSAVED_NOTICE, 'pending');
    });
  });

  it('turns Save and the boxes off while the save is out, and is done once it lands', async () => {
    const utils = await mountPanel();
    await typeName(utils, 1, 'Mori');
    const write = deferred();
    api.putMatchLineup.mockReturnValue(write.promise);

    await click(saveButton(utils));

    expect(savingButton(utils).disabled).toBe(true);
    expect(utils.getByLabelText('2 player').disabled).toBe(true);
    expect(api.putMatchLineup).toHaveBeenCalledTimes(1);

    await act(async () => { write.resolve({ positions: { 1: 'Mori', 2: 'Sato', 3: 'Ito' }, memberIds: { 1: 'mem-4', 2: 'mem-2', 3: 'mem-3' } }); });
    await flush();

    expect(saveButton(utils).disabled, 'saved: nothing left to save').toBe(true);
  });

  it('shows the server\'s refusal when another device placed the member meanwhile, and keeps the operator\'s change', async () => {
    const utils = await mountPanel();
    await typeName(utils, 1, 'Mori');
    // Meanwhile another device put Mori at position 2: put on that lineup, the change fields him twice.
    changedElsewhere({
      positions: { 1: 'Aoki', 2: 'Mori', 3: 'Ito' }, memberIds: { 1: 'mem-1', 2: 'mem-4', 3: 'mem-3' },
    });

    await click(saveButton(utils));

    expect(api.putMatchLineup).toHaveBeenCalledTimes(1);
    expect(utils.getByText(/is at positions/), 'the server\'s refusal is shown').toBeTruthy();
    expect(saveButton(utils).disabled, 'the operator can still fix it and save').toBe(false);
    expect(utils.getByLabelText('1 player').value, 'the operator\'s own change stays').toBe('Mori');
  });

  it('names the position that was already there, not the one the operator typed into, when a typed name is a member stored elsewhere', async () => {
    serverHolds(MORI_AT_1);
    const utils = await mountPanel();
    await typeName(utils, 2, 'Mori');

    await click(saveButton(utils));

    expect(utils.getByText('Mori is already at Position 1.')).toBeTruthy();
    expect(api.putMatchLineup).not.toHaveBeenCalled();
    expect(api.addTeamMember).not.toHaveBeenCalled();
  });

  it('names the position the operator picked the member for, not the box they typed the same name into', async () => {
    const utils = await mountPanel();
    await pickFromList(utils, 2, 'Mori');
    await typeName(utils, 1, 'Mori');

    await click(saveButton(utils));

    expect(utils.getByText('Mori is already at Position 2.')).toBeTruthy();
    expect(api.putMatchLineup).not.toHaveBeenCalled();
    expect(api.addTeamMember).not.toHaveBeenCalled();
    expect(api.renameTeamMember).not.toHaveBeenCalled();
  });

  it('names Senpo, not the Jiho the operator typed into, on a five-person team whose positions are named', async () => {
    api.fetchLineupInForce.mockResolvedValue(matchLineup({
      positions: { senpo: 'Mori', jiho: 'Sato', chuken: 'Ito' }, memberIds: { senpo: 'mem-4', jiho: 'mem-2', chuken: 'mem-3' },
    }));
    const utils = await mountPanel({ ...COMP, teamSize: 5 });
    await typeName(utils, 'Jiho', 'Mori');

    await click(saveButton(utils));

    expect(utils.getByText('Mori is already at Senpo.')).toBeTruthy();
    expect(api.putMatchLineup).not.toHaveBeenCalled();
  });

  describe('a duplicate that is there before the typed names are resolved', () => {
    it('takes the refusal down once the operator changes a position: the next Save judges the lineup again', async () => {
      const utils = await mountPanel();
      await pickFromList(utils, 2, 'Mori');
      await typeName(utils, 3, 'Mori');
      await click(saveButton(utils));
      expect(utils.getByText('Mori is already at Position 2.')).toBeTruthy();

      await typeName(utils, 3, 'Ito');

      expect(utils.queryByText('Mori is already at Position 2.')).toBeNull();
    });

    it('is refused before anything is minted or renamed when a typed name is a member the lineup holds elsewhere', async () => {
      serverHolds(MORI_AT_1);
      const utils = await mountPanel();
      await typeName(utils, 2, 'Mori');
      await typeName(utils, 3, 'Kato');

      await click(saveButton(utils));

      expect(utils.getByText('Mori is already at Position 1.')).toBeTruthy();
      expect(resolver, 'the names are not resolved for a Save that is refused').not.toHaveBeenCalled();
      expect(api.addTeamMember, 'Kato is not minted').not.toHaveBeenCalled();
      expect(api.renameTeamMember).not.toHaveBeenCalled();
      expect(api.putMatchLineup).not.toHaveBeenCalled();
    });

    it('counts the member a typed name belongs to, not the blank member the position held: another device moved that blank member, and the Save goes through', async () => {
      api.fetchSquads.mockResolvedValue({ 'team-a': [...SQUAD, BLANK] });
      serverHolds(BLANK_AT_3);
      const utils = await mountPanel();
      // The operator types Mori, who is on the team, over the unnamed member at position 3 ...
      await typeName(utils, 3, 'Mori');
      // ... while another device moves that unnamed member to position 1.
      changedElsewhere({ positions: { 1: '', 2: 'Sato', 3: '' }, memberIds: { 1: 'mem-6', 2: 'mem-2' } });

      await click(saveButton(utils));

      expect(api.renameTeamMember, 'Mori is not a new name for the unnamed member').not.toHaveBeenCalled();
      expect(api.addTeamMember).not.toHaveBeenCalled();
      expect(putOf(api.putMatchLineup.mock.calls[0])).toEqual({ positions: { 3: 'Mori' }, memberIds: { 3: 'mem-4' } });
      expect(serverMatch.positions).toEqual({ 1: '', 2: 'Sato', 3: 'Mori' });
      expect(serverMatch.memberIds).toEqual({ 1: 'mem-6', 2: 'mem-2', 3: 'mem-4' });
    });

    it('does not take the member a typed-over position held for one it still holds: Sato moved elsewhere, and Kato typed over his old position, saves', async () => {
      const utils = await mountPanel();
      await typeName(utils, 2, 'Kato');
      // Another device moved Sato to position 1.
      changedElsewhere({
        positions: { 1: 'Sato', 2: 'Ito', 3: 'Aoki' }, memberIds: { 1: 'mem-2', 2: 'mem-3', 3: 'mem-1' },
      });

      await click(saveButton(utils));

      expect(api.addTeamMember).toHaveBeenCalledTimes(1);
      expect(putOf(api.putMatchLineup.mock.calls[0])).toEqual({ positions: { 2: 'Kato' }, memberIds: { 2: 'mem-minted' } });
      expect(serverMatch.positions).toEqual({ 1: 'Sato', 2: 'Kato', 3: 'Aoki' });
    });
  });

  // A name nobody on the team has is one member once the resolver has named or minted it
  // for the first position, and it finds that member again for the second. Typed at two
  // positions it is one member fielded twice, so the Save is refused before the first is
  // named or minted, not after.
  describe('a name that is new to the team typed at two positions', () => {
    const refusedBeforeAnythingIsWritten = () => {
      expect(api.addTeamMember, 'nobody is minted for it').not.toHaveBeenCalled();
      expect(api.renameTeamMember, 'no slot is named for it').not.toHaveBeenCalled();
      expect(api.putMatchLineup).not.toHaveBeenCalled();
      expect(resolver, 'the names are not resolved for a Save that is refused').not.toHaveBeenCalled();
    };

    it('is refused before a member is minted for it', async () => {
      const utils = await mountPanel();
      await typeName(utils, 2, 'Kato');
      await typeName(utils, 3, 'Kato');

      await click(saveButton(utils));

      expect(utils.getByText('Kato is already at Position 2.')).toBeTruthy();
      refusedBeforeAnythingIsWritten();
    });

    it('is refused before the unnamed member of a slot is named for it, naming the earlier position', async () => {
      api.fetchSquads.mockResolvedValue({ 'team-a': FRESH });
      serverHolds(EMPTY);
      const utils = await mountPanel();
      await typeName(utils, 1, 'Ito');
      await typeName(utils, 3, 'Ito');

      await click(saveButton(utils));

      expect(utils.getByText('Ito is already at Position 1.')).toBeTruthy();
      refusedBeforeAnythingIsWritten();
      expect(saveButton(utils).disabled, 'the operator can still fix it and save').toBe(false);
    });

    it('is refused when the unnamed member the first position holds is the one that would carry it', async () => {
      api.fetchSquads.mockResolvedValue({ 'team-a': [...SQUAD, BLANK] });
      serverHolds(BLANK_AT_1);
      const utils = await mountPanel();
      await typeName(utils, 1, 'Kato');
      await typeName(utils, 3, 'Kato');

      await click(saveButton(utils));

      expect(utils.getByText('Kato is already at Position 1.')).toBeTruthy();
      refusedBeforeAnythingIsWritten();
    });

    it('is one name however it is written, as the resolver reads it: case and accents do not make two', async () => {
      api.fetchSquads.mockResolvedValue({ 'team-a': FRESH });
      serverHolds(EMPTY);
      const utils = await mountPanel();
      await typeName(utils, 1, 'Ito');
      await typeName(utils, 3, 'ITÔ');

      await click(saveButton(utils));

      expect(utils.getByText('Ito is already at Position 1.')).toBeTruthy();
      refusedBeforeAnythingIsWritten();
    });

    it('is not held against two different new names: each is written for its own position', async () => {
      api.fetchSquads.mockResolvedValue({ 'team-a': FRESH });
      serverHolds(EMPTY);
      const utils = await mountPanel();
      await typeName(utils, 1, 'Ito');
      await typeName(utils, 3, 'Itoh');

      await click(saveButton(utils));

      expect(api.renameTeamMember).toHaveBeenCalledTimes(2);
      expect(api.addTeamMember).not.toHaveBeenCalled();
      expect(putOf(api.putMatchLineup.mock.calls[0])).toEqual({
        positions: { 1: 'Ito', 3: 'Itoh' },
        memberIds: { 1: 'mem-1', 3: 'mem-3' },
      });
      expect(changedOf(api.putMatchLineup.mock.calls[0])).toEqual(['1', '3']);
    });
  });

  describe('the ids handed to the name resolver', () => {
    // The seeded member for position 3 is unnamed; the lineup places it at position 2.
    const SEEDED = [{ id: 'mem-1', index: 1, name: 'Aoki' }, { id: 'mem-2', index: 2, name: 'Sato' }, { id: 'mem-3', index: 3, name: '' }];

    it('are the lineup as the form shows it, so a blank member placed at another position is not renamed for a name typed at its own position: that mints', async () => {
      api.fetchSquads.mockResolvedValue({ 'team-a': SEEDED });
      serverHolds({ positions: { 1: 'Aoki', 2: '' }, memberIds: { 1: 'mem-1', 2: 'mem-3' } });
      const utils = await mountPanel();
      await typeName(utils, 3, 'Kato');

      await click(saveButton(utils));

      expect(resolver).toHaveBeenCalledTimes(1);
      const [, , asked, , , idsGiven] = resolver.mock.calls[0];
      expect(asked).toEqual({ 3: 'Kato' });
      expect(idsGiven, 'what the lineup places counts').toEqual({ 1: 'mem-1', 2: 'mem-3', 3: '' });
      expect(api.renameTeamMember, 'the member at position 2 keeps its place and its blank name').not.toHaveBeenCalled();
      expect(api.addTeamMember).toHaveBeenCalledWith('comp-1', 'team-a', 'Kato', 'pw');
      expect(putOf(api.putMatchLineup.mock.calls[0])).toEqual({ positions: { 3: 'Kato' }, memberIds: { 3: 'mem-minted' } });
    });
  });

  it('leaves a position that holds a member and no name alone, with its id: it is not sent, and the server keeps it', async () => {
    api.fetchSquads.mockResolvedValue({ 'team-a': [...SQUAD, BLANK] });
    serverHolds(BLANK_AT_3);
    const utils = await mountPanel();
    await typeName(utils, 1, 'Mori');

    await click(saveButton(utils));

    expect(putOf(api.putMatchLineup.mock.calls[0])).toEqual({ positions: { 1: 'Mori' }, memberIds: { 1: 'mem-4' } });
    expect(serverMatch.memberIds[3], 'the placement the Save left alone is still there').toBe('mem-6');
    expect(serverMatch.positions[3]).toBe('');
  });

  describe('while a Save waits for its write', () => {
    it('turns an open rename box and its buttons off, since a rename made then would be overwritten by the Save', async () => {
      const utils = await mountPanel();
      await typeName(utils, 2, 'Mori');
      await click(panelRow(utils, 1).getByRole('button', { name: 'Rename 1 player' }));
      const write = deferred();
      api.putMatchLineup.mockReturnValue(write.promise);

      await click(saveButton(utils));

      expect(savingButton(utils).disabled).toBe(true);
      expect(panelRow(utils, 1).getByLabelText('Rename 1 player').disabled).toBe(true);
      expect(panelRow(utils, 1).getByRole('button', { name: 'Save' }).disabled, 'its own label does not say it is saving').toBe(true);
      expect(panelRow(utils, 1).getByRole('button', { name: 'Cancel' }).disabled).toBe(true);

      await act(async () => { write.resolve({ positions: { 1: 'Aoki', 2: 'Mori', 3: 'Ito' }, memberIds: { 1: 'mem-1', 2: 'mem-4', 3: 'mem-3' } }); });
      await flush();
      expect(panelRow(utils, 1).getByLabelText('Rename 1 player').disabled, 'usable again once the Save is done').toBe(false);
    });

    it('turns the draft notice\'s Discard off', async () => {
      keepDraft('match:Pool A-2', NAMES);
      const utils = await mountPanel();
      expect(utils.getByText('Unsaved lineup changes restored')).toBeTruthy();
      const write = deferred();
      api.putMatchLineup.mockReturnValue(write.promise);

      await click(saveButton(utils));

      expect(utils.getByRole('button', { name: 'Discard' }).disabled).toBe(true);
      await act(async () => { write.resolve({ positions: { 1: 'Mori', 2: 'Sato', 3: 'Ito' }, memberIds: { 1: 'mem-4', 2: 'mem-2', 3: 'mem-3' } }); });
      await flush();
    });
  });

  it('keeps Save off while a rename is out, and on again once it has landed', async () => {
    const utils = await mountPanel();
    await typeName(utils, 2, 'Mori');
    await click(panelRow(utils, 1).getByRole('button', { name: 'Rename 1 player' }));
    await act(async () => { fireEvent.change(panelRow(utils, 1).getByLabelText('Rename 1 player'), { target: { value: 'Aoki-san' } }); });
    const renamed = deferred();
    api.renameTeamMember.mockReturnValue(renamed.promise);

    await click(panelRow(utils, 1).getByRole('button', { name: 'Save' }));

    expect(api.renameTeamMember).toHaveBeenCalledTimes(1);
    expect(saveButton(utils).disabled, 'a rename is out').toBe(true);

    await act(async () => { renamed.resolve(renamedAs('mem-1', 'Aoki-san')); });
    await flush();

    expect(saveButton(utils).disabled, 'the rename has landed, and the lineup is still unsaved').toBe(false);
  });
});

describe('the Lineups page', () => {
  it('a starting lineup: carries the changed position alone and shows what the server answered, a position another device changed meanwhile included', async () => {
    api.fetchSquads.mockResolvedValue({ 'team-a': [...SQUAD, KATO] });
    const utils = await mountPage();
    await pick(utils, 1, 'mem-4');
    // Another device changed position 2 after the page read the lineup.
    changedElsewhereStarting(KATO_AT_2);

    await click(saveButton(utils));

    expect(api.fetchTeamLineup, 'the lineup is not read again for the Save').toHaveBeenCalledTimes(1);
    expect(api.putTeamLineup).toHaveBeenCalledTimes(1);
    const [, , round] = api.putTeamLineup.mock.calls[0];
    expect(round).toBe(0);
    expect(putOf(api.putTeamLineup.mock.calls[0])).toEqual({ positions: { 1: 'Mori' }, memberIds: { 1: 'mem-4' } });
    expect(changedOf(api.putTeamLineup.mock.calls[0])).toEqual(['1']);
    expect(utils.getByTestId('lineup-position-2').value, 'what the server answered is shown').toBe('mem-5');
    expect(saveButton(utils).disabled, 'saved: nothing left to save').toBe(true);
  });

  it('a match\'s lineup: the same', async () => {
    api.fetchSquads.mockResolvedValue({ 'team-a': [...SQUAD, KATO] });
    const utils = await mountPage();
    await chooseTarget(utils, 'Pool A-2');
    await pick(utils, 1, 'mem-4');
    changedElsewhere(KATO_AT_2);

    await click(saveButton(utils));

    expect(api.fetchLineupInForce, 'the lineup is not read again for the Save').toHaveBeenCalledTimes(1);
    expect(api.putMatchLineup).toHaveBeenCalledTimes(1);
    expect(putOf(api.putMatchLineup.mock.calls[0])).toEqual({ positions: { 1: 'Mori' }, memberIds: { 1: 'mem-4' } });
    expect(changedOf(api.putMatchLineup.mock.calls[0])).toEqual(['1']);
    expect(utils.getByTestId('lineup-position-2').value).toBe('mem-5');
  });

  it('clears a position the operator cleared, whatever another device put there meanwhile', async () => {
    const utils = await mountPage();
    await pick(utils, 1, '');
    changedElsewhereStarting({
      positions: { 1: 'Oda', 2: 'Kato', 3: 'Ito' }, memberIds: { 1: 'mem-8', 2: 'mem-5', 3: 'mem-3' },
    });

    await click(saveButton(utils));

    // The cleared position goes as its empty name, which the server needs to be there.
    expect(putOf(api.putTeamLineup.mock.calls[0])).toEqual({ positions: { 1: '' } });
    expect(changedOf(api.putTeamLineup.mock.calls[0])).toEqual(['1']);
    expect(serverStarting.positions).toEqual({ 2: 'Kato', 3: 'Ito' });
  });

  describe('with no connection', () => {
    it('is built as it is online, naming the changed position, and queued, and the operator\'s change stays on screen', async () => {
      const utils = await mountPage();
      await pick(utils, 1, 'mem-4');
      api.fetchTeamLineup.mockRejectedValue(new TypeError('Failed to fetch'));
      api.putTeamLineup.mockResolvedValue({ queued: true });

      await click(saveButton(utils));

      expect(api.fetchTeamLineup, 'nothing is read for the Save').toHaveBeenCalledTimes(1);
      expect(api.putTeamLineup.mock.calls[0][3]).toEqual({ 1: 'Mori' });
      expect(changedOf(api.putTeamLineup.mock.calls[0])).toEqual(['1']);
      expect(showToast).toHaveBeenCalledWith(QUEUED_NOTICE, 'pending');
      expect(utils.getByTestId('lineup-position-1').value).toBe('mem-4');
    });
  });

  it('turns Save and the pickers off while the save is out, and is done once it lands', async () => {
    const utils = await mountPage();
    await pick(utils, 1, 'mem-4');
    const write = deferred();
    api.putTeamLineup.mockReturnValue(write.promise);

    await click(saveButton(utils));

    expect(savingButton(utils).disabled).toBe(true);
    expect(utils.getByTestId('lineup-position-2').disabled).toBe(true);
    expect(api.putTeamLineup).toHaveBeenCalledTimes(1);

    await act(async () => { write.resolve({ positions: { 1: 'Mori', 2: 'Sato', 3: 'Ito' }, memberIds: { 1: 'mem-4', 2: 'mem-2', 3: 'mem-3' } }); });
    await flush();

    expect(saveButton(utils).disabled, 'saved: nothing left to save').toBe(true);
    expect(utils.getByTestId('lineup-position-2').disabled).toBe(false);
  });

  it('shows the server\'s refusal when another device placed the member meanwhile, and keeps the operator\'s change', async () => {
    const utils = await mountPage();
    await pick(utils, 1, 'mem-4');
    // Meanwhile another device put Mori at position 2: put on that lineup, the change fields him twice.
    changedElsewhereStarting({
      positions: { 1: 'Aoki', 2: 'Mori', 3: 'Ito' }, memberIds: { 1: 'mem-1', 2: 'mem-4', 3: 'mem-3' },
    });

    await click(saveButton(utils));

    expect(api.putTeamLineup).toHaveBeenCalledTimes(1);
    expect(utils.getByText(/is at positions/), 'the server\'s refusal is shown').toBeTruthy();
    expect(saveButton(utils).disabled, 'the operator can still fix it and save').toBe(false);
    expect(utils.getByTestId('lineup-position-1').value, 'the operator\'s own change stays').toBe('mem-4');
  });

  it('takes the refusal down once the operator changes a position: the next Save judges the lineup again', async () => {
    const utils = await mountPage();
    await pick(utils, 1, 'mem-4');
    changedElsewhereStarting({
      positions: { 1: 'Aoki', 2: 'Mori', 3: 'Ito' }, memberIds: { 1: 'mem-1', 2: 'mem-4', 3: 'mem-3' },
    });
    await click(saveButton(utils));
    expect(utils.getByText(/is at positions/)).toBeTruthy();

    await pick(utils, 1, 'mem-1');

    expect(utils.queryByText(/is at positions/)).toBeNull();
  });

  // The pickers never offer a member the form holds at another position, but a lineup
  // saved before that rule can hold one twice: the page refuses it before it asks the
  // server, naming the position that was already there.
  it('refuses a lineup that shows one member twice, naming the position that was already there, and writes nothing', async () => {
    startingHolds({
      positions: { 1: 'Mori', 2: 'Mori', 3: 'Ito' }, memberIds: { 1: 'mem-4', 2: 'mem-4', 3: 'mem-3' },
    });
    const utils = await mountPage();
    await pick(utils, 3, 'mem-1');

    await click(saveButton(utils));

    expect(utils.getByText('Mori is already at Position 1.')).toBeTruthy();
    expect(api.putTeamLineup).not.toHaveBeenCalled();
    expect(saveButton(utils).disabled, 'the operator can still fix it and save').toBe(false);
  });

  it('keeps a member it added while a read of the team\'s members begun before the add is out: that read answers without them', async () => {
    const utils = await mountPage();
    // Another device saves a lineup: the page reads the lineup and the team's members again.
    const late = deferred();
    api.fetchSquads.mockReturnValue(late.promise);
    await announce();
    // Meanwhile the operator adds Kato at position 3.
    await pick(utils, 3, '__add__');
    await act(async () => { fireEvent.change(utils.getByLabelText('New member name for 3'), { target: { value: 'Kato' } }); });
    await click(utils.getByRole('button', { name: 'Add' }));
    expect(utils.getByTestId('squad-member-mem-minted')).toBeTruthy();

    await act(async () => { late.resolve({ 'team-a': SQUAD }); });
    await flush();

    expect(utils.getByTestId('squad-member-mem-minted'), 'the member added is still listed').toBeTruthy();
    expect(utils.getByTestId('lineup-position-3').value).toBe('mem-minted');
  });

  it('a match\'s lineup: is the match\'s own once saved, where the lineup read came from another match', async () => {
    const utils = await mountPage();
    await chooseTarget(utils, 'Pool A-2');
    await pick(utils, 1, '');
    expect(utils.getByTestId('lineup-source').textContent).not.toMatch(/Lineup for this match/);

    await click(saveButton(utils));

    expect(utils.getByTestId('lineup-source').textContent).toMatch(/Lineup for this match/);
    expect(utils.getByTestId('lineup-position-1').value, 'the position the operator cleared stays cleared').toBe('');
  });
});

describe('the Lineups page while a Save waits for its write', () => {
  const openAddRow = async (utils, position) => {
    await pick(utils, position, '__add__');
    await act(async () => { fireEvent.change(utils.getByLabelText(`New member name for ${position}`), { target: { value: 'Kato' } }); });
  };

  it('turns off what would change the lineup or its members: Rename, Clear name, an open add row, an open rename box and Discard', async () => {
    keepDraft('start', NAMES);
    api.clearTeamMember = vi.fn().mockImplementation((_c, _t, id) => Promise.resolve(renamedAs(id, '')));
    const utils = await mountPage(DRAWN_COMP);
    expect(utils.getByText('Unsaved lineup changes restored')).toBeTruthy();
    await openAddRow(utils, 3);
    await click(memberRow(utils, 'mem-2').getByRole('button', { name: 'Rename' }));
    const write = deferred();
    api.putTeamLineup.mockReturnValue(write.promise);

    await click(saveButton(utils));

    expect(savingButton(utils).disabled).toBe(true);
    expect(memberRow(utils, 'mem-1').getByRole('button', { name: 'Rename' }).disabled).toBe(true);
    expect(memberRow(utils, 'mem-1').getByRole('button', { name: 'Clear name' }).disabled).toBe(true);
    const addInput = utils.getByLabelText('New member name for 3');
    expect(addInput.disabled).toBe(true);
    expect(within(addInput.parentElement).getByRole('button', { name: 'Add' }).disabled).toBe(true);
    expect(within(addInput.parentElement).getByRole('button', { name: 'Cancel' }).disabled).toBe(true);
    expect(utils.getByLabelText('Rename Sato').disabled).toBe(true);
    expect(memberRow(utils, 'mem-2').getByRole('button', { name: 'Save' }).disabled, 'its own label does not say it is saving').toBe(true);
    expect(memberRow(utils, 'mem-2').getByRole('button', { name: 'Cancel' }).disabled).toBe(true);
    expect(utils.getByRole('button', { name: 'Discard' }).disabled).toBe(true);

    await act(async () => { write.resolve({ positions: { 1: 'Mori', 2: 'Sato', 3: 'Ito' }, memberIds: { 1: 'mem-4', 2: 'mem-2', 3: 'mem-3' } }); });
    await flush();
    expect(memberRow(utils, 'mem-1').getByRole('button', { name: 'Rename' }).disabled, 'usable again once the Save is done').toBe(false);
    expect(utils.getByLabelText('Rename Sato').disabled).toBe(false);
  });

  it('turns them off for a removal as well: nothing that changes the lineup or its members while it is out', async () => {
    api.fetchLineupInForce.mockResolvedValueOnce({ ...CARRIED, sourceMatchId: 'Pool A-2' }).mockResolvedValue(CARRIED);
    const removal = deferred();
    api.deleteMatchLineup = vi.fn().mockReturnValue(removal.promise);
    const utils = await mountPage(DRAWN_COMP);
    await chooseTarget(utils, 'Pool A-2');
    await click(utils.getByRole('button', { name: /Use the previous match/ }));

    expect(api.deleteMatchLineup).toHaveBeenCalledTimes(1);
    expect(memberRow(utils, 'mem-1').getByRole('button', { name: 'Rename' }).disabled).toBe(true);
    expect(memberRow(utils, 'mem-1').getByRole('button', { name: 'Clear name' }).disabled).toBe(true);

    await act(async () => { removal.resolve(true); });
    await flush();
    expect(memberRow(utils, 'mem-1').getByRole('button', { name: 'Rename' }).disabled).toBe(false);
  });

  it('keeps Save off while a rename is out, and on again once it has landed', async () => {
    const utils = await mountPage();
    await pick(utils, 1, 'mem-4');
    await click(memberRow(utils, 'mem-2').getByRole('button', { name: 'Rename' }));
    await act(async () => { fireEvent.change(utils.getByLabelText('Rename Sato'), { target: { value: 'Sato-san' } }); });
    const renamed = deferred();
    api.renameTeamMember.mockReturnValue(renamed.promise);

    await click(memberRow(utils, 'mem-2').getByRole('button', { name: 'Save' }));

    expect(api.renameTeamMember).toHaveBeenCalledTimes(1);
    expect(saveButton(utils).disabled, 'a rename is out').toBe(true);

    await act(async () => { renamed.resolve(renamedAs('mem-2', 'Sato-san')); });
    await flush();

    expect(saveButton(utils).disabled, 'the rename has landed, and the lineup is still unsaved').toBe(false);
  });

  it('keeps Save off while a clear is out, and on again once it has landed', async () => {
    const cleared = deferred();
    api.clearTeamMember = vi.fn().mockReturnValue(cleared.promise);
    const utils = await mountPage(DRAWN_COMP);
    await pick(utils, 1, 'mem-4');

    await click(memberRow(utils, 'mem-2').getByRole('button', { name: 'Clear name' }));

    expect(api.clearTeamMember).toHaveBeenCalledTimes(1);
    expect(saveButton(utils).disabled, 'a clear is out').toBe(true);

    await act(async () => { cleared.resolve(renamedAs('mem-2', '')); });
    await flush();

    expect(saveButton(utils).disabled).toBe(false);
  });

  it('keeps Save off while an add is out, and on again once it has landed', async () => {
    const added = deferred();
    api.addTeamMember.mockReturnValue(added.promise);
    const utils = await mountPage();
    await pick(utils, 1, 'mem-4');
    await openAddRow(utils, 3);

    await click(utils.getByRole('button', { name: 'Add' }));

    expect(api.addTeamMember).toHaveBeenCalledTimes(1);
    expect(saveButton(utils).disabled, 'an add is out').toBe(true);

    await act(async () => { added.resolve(answered({ id: 'mem-minted', index: 6 }, { name: 'Kato' })); });
    await flush();

    expect(saveButton(utils).disabled).toBe(false);
    expect(utils.getByTestId('lineup-position-3').value, 'the member added is the one placed').toBe('mem-minted');
  });
});

// Both editors load the team's members once, when they open. A member another
// device creates afterwards (a name typed on a team score sheet mints one and
// saves the lineup) arrives in a followed lineup with an id the list lacks.
describe('a lineup followed from another device that names a member created there', () => {
  const FOLLOWED = { positions: { 1: 'Aoki', 2: 'Zed', 3: 'Ito' }, memberIds: { 1: 'mem-1', 2: 'mem-9', 3: 'mem-3' } };

  it('the Lineups page shows that member at the position, not an empty slot', async () => {
    const utils = await mountPage();
    await chooseTarget(utils, 'Pool A-2');
    api.fetchSquads.mockResolvedValue({ 'team-a': [...SQUAD, ZED] });
    api.fetchLineupInForce.mockResolvedValue(matchLineup(FOLLOWED));

    await announce();

    const select = utils.getByTestId('lineup-position-2');
    expect(select.value).toBe('mem-9');
    expect(select.selectedOptions[0].textContent).toContain('Zed');
    expect(utils.getByTestId('squad-member-mem-9'), 'and lists them with the team\'s members').toBeTruthy();
  });

  it('the at-court panel shows that member\'s number and its Rename', async () => {
    const utils = await mountPanel();
    api.fetchSquads.mockResolvedValue({ 'team-a': [...SQUAD, ZED] });
    api.fetchLineupInForce.mockResolvedValue(matchLineup(FOLLOWED));

    await announce();

    expect(utils.getByLabelText('2 player').value).toBe('Zed');
    const row = utils.getByTestId('match-lineup-pos-team-a-2');
    expect(within(row).getByText('Slot 5')).toBeTruthy();
    expect(within(row).getByRole('button', { name: 'Rename 2 player' })).toBeTruthy();
  });

  it('is still shown, with the list as it was, when the members cannot be read again', async () => {
    const utils = await mountPage();
    await chooseTarget(utils, 'Pool A-2');
    api.fetchSquads.mockRejectedValue(new Error('offline'));
    api.fetchLineupInForce.mockResolvedValue(matchLineup({
      positions: { 1: 'Mori', 2: 'Sato', 3: 'Ito' }, memberIds: { 1: 'mem-4', 2: 'mem-2', 3: 'mem-3' },
    }));

    await announce();

    expect(utils.getByTestId('lineup-position-1').value, 'the lineup that was read').toBe('mem-4');
    expect(utils.getByTestId('squad-member-mem-1'), 'the list that was read before').toBeTruthy();
    expect(utils.queryByTestId('lineup-members-unavailable')).toBeNull();
  });
});

// Each write of a member is stamped by the server, and the Lineups page, like the panel,
// keeps the newest copy of a member by that stamp: a read begun before a rename that
// answers after it holds the older copy and undoes nothing, and a rename another device
// makes after it shows. No read is made after the page's own write: the server announces it.
describe('the Lineups page: the newest copy of a member stands', () => {
  const rowText = (utils, id) => utils.getByTestId(`squad-member-${id}`).textContent;
  const renameSato = async (utils) => {
    await click(memberRow(utils, 'mem-2').getByRole('button', { name: 'Rename' }));
    await act(async () => { fireEvent.change(utils.getByLabelText('Rename Sato'), { target: { value: 'Sato-san' } }); });
    await click(memberRow(utils, 'mem-2').getByRole('button', { name: 'Save' }));
  };

  it('a read begun before a rename, answering after it with the old name, does not undo it', async () => {
    const utils = await mountPage();
    const older = deferred();
    api.fetchSquads.mockReturnValueOnce(older.promise);
    await announce();
    await renameSato(utils);
    expect(rowText(utils, 'mem-2')).toContain('Sato-san');

    await act(async () => { older.resolve({ 'team-a': SQUAD }); });
    await flush();

    expect(rowText(utils, 'mem-2')).toContain('Sato-san');
    expect(api.fetchSquads, 'the read made as the page opened and the announced one, none after the rename').toHaveBeenCalledTimes(2);
  });

  it('a rename another device makes after the page\'s own write shows', async () => {
    const utils = await mountPage();
    await renameSato(utils);
    const mine = await api.renameTeamMember.mock.results[0].value;
    api.fetchSquads.mockResolvedValue({ 'team-a': SQUAD.map((m) => (m.id === 'mem-2' ? namedLater(mine, 'Sato-kun') : m)) });

    await announce();

    expect(rowText(utils, 'mem-2')).toContain('Sato-kun');
    expect(rowText(utils, 'mem-2')).not.toContain('Sato-san');
  });

  it('an own rename\'s answer older than the list already shown does not undo the list', async () => {
    const utils = await mountPage();
    const answer = deferred();
    const mine = renamedAs('mem-2', 'Sato-san');
    api.renameTeamMember.mockReturnValue(answer.promise);
    await renameSato(utils);
    api.fetchSquads.mockResolvedValue({ 'team-a': SQUAD.map((m) => (m.id === 'mem-2' ? namedLater(mine, 'Sato-kun') : m)) });
    await announce();

    // The row is still saving its own rename; once that lands, it shows the list's name.
    await act(async () => { answer.resolve(mine); });
    await flush();

    expect(rowText(utils, 'mem-2')).toContain('Sato-kun');
    expect(rowText(utils, 'mem-2')).not.toContain('Sato-san');
  });
});
