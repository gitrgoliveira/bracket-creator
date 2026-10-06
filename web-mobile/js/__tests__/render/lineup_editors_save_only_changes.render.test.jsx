// What a Save writes in both lineup editors (the at-court panel's
// MatchLineupSideEditor and the Lineups page), in a real DOM: only the positions
// the operator changed (operator decision 2026-10-05). Every other position keeps
// what is stored now, so a change another device made to it since the form was
// read is not put back by this Save.
//
//  - the lineup is read again at Save, and the PUT holds the stored value of every
//    position left alone and the operator's own for the ones changed;
//  - when that read fails, or is not answered by its deadline, the form is written
//    as it was loaded and changed, so an offline save is written and queued as it
//    always was; Save is off while the read is out;
//  - an untouched position is never re-resolved or minted, even when it names a
//    member this editor's list does not hold (one another device created);
//  - a lineup composed that way is refused when it would field one member twice,
//    naming the position that was already there (the one the operator did not
//    change; of two they changed, the one they picked the member for, not the box
//    they typed the name into), and what the Save read is shown, so the conflict is
//    on screen and a change to the position it names is a change;
//  - the panel refuses such a lineup before its resolver renames or mints for a
//    typed name, and hands the resolver the ids of the lineup as composed; for a
//    typed name it counts the member the resolver will place (the member the name
//    belongs to, else the unnamed member the position holds), so a Save the resolver
//    would place without a clash is not refused;
//  - while a Save waits for its read and its write nothing that changes the lineup
//    or the team's members is usable, and Save waits while one of those is out;
//  - a side of the panel given another team (a knockout feeder decided on another
//    device) gets a fresh editor, so a Save the old team still has out, its write or
//    the member it is minting, lands nothing in the new team's lineup or members;
//  - a lineup followed from another device is shown with the team's members read
//    again, so a member created elsewhere is not shown as an empty slot;
//  - a Save that shows what another device changed reads the team's members again
//    too, so a member created there is in the picker of the position it holds.

import React from 'react';
import { render, act, fireEvent, within } from '@testing-library/react';
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { FETCH_TIMEOUT_MS } from '../../write_result.jsx';

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
let MatchLineupPanel;
let realResolver;
let resolver;
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
    fetchLineupInForce: vi.fn().mockResolvedValue(CARRIED),
    fetchSquads: vi.fn().mockResolvedValue({ 'team-a': SQUAD }),
    putTeamLineup: vi.fn().mockImplementation((_c, _t, _r, positions, _pw, memberIds) => Promise.resolve({ positions, memberIds })),
    putMatchLineup: vi.fn().mockImplementation((_c, _t, _m, positions, _pw, memberIds) => Promise.resolve({ positions, memberIds })),
    addTeamMember: vi.fn().mockImplementation((_c, _t, name) => Promise.resolve({ id: 'mem-minted', index: 6, name })),
    renameTeamMember: vi.fn().mockResolvedValue(true),
    queuedLineupSave: vi.fn().mockReturnValue(false),
  };
  window.API = api;
  delete window.subscribeUnsentWrites;
  delete window.subscribeSyncStatus;
  ({ AdminTeamLineupsList } = await import('../../admin_lineup.jsx'));
  ({ MatchLineupSideEditor, MatchLineupPanel } = await import('../../admin_schedule_lineup.jsx'));
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
const putOf = (call) => ({ positions: call[3], memberIds: call[5] });
// What the stub answers a match's re-read with: the lineup another device left.
const matchLineup = (stored) => lineupFor({ ...stored, matchId: 'Pool A-2', sourceMatchId: 'Pool A-0' });
const startingLineup = (stored) => lineupFor({ ...stored, round: 0, sourceRound: 0 });

describe('the at-court panel', () => {
  it('writes what another device changed to a position the operator left alone, with the operator\'s own change', async () => {
    const utils = await mountPanel();
    await typeName(utils, 1, 'Mori');
    api.fetchLineupInForce.mockResolvedValue(matchLineup(KATO_AT_2));

    await click(saveButton(utils));

    expect(api.fetchLineupInForce, 'the lineup is read again at Save').toHaveBeenCalledTimes(2);
    expect(api.putMatchLineup).toHaveBeenCalledTimes(1);
    expect(putOf(api.putMatchLineup.mock.calls[0])).toEqual({
      positions: { 1: 'Mori', 2: 'Kato', 3: 'Ito' },
      memberIds: { 1: 'mem-4', 2: 'mem-5', 3: 'mem-3' },
    });
    expect(api.addTeamMember, 'the position left alone is not minted').not.toHaveBeenCalled();
    // The server's answer is what is shown afterwards.
    expect(utils.getByLabelText('2 player').value).toBe('Kato');
  });

  it('sends a position left alone that names a member this list has never heard of as the server holds it: never resolved, never minted', async () => {
    api.fetchLineupInForce.mockResolvedValue(matchLineup(ZED_AT_3));
    const utils = await mountPanel();
    expect(utils.getByLabelText('3 player').value).toBe('Zed');
    await typeName(utils, 1, 'Mori');

    await click(saveButton(utils));

    expect(putOf(api.putMatchLineup.mock.calls[0])).toEqual({
      positions: { 1: 'Mori', 2: 'Sato', 3: 'Zed' },
      memberIds: { 1: 'mem-4', 2: 'mem-2', 3: 'mem-9' },
    });
    expect(api.addTeamMember).not.toHaveBeenCalled();
    expect(api.renameTeamMember).not.toHaveBeenCalled();
    const asked = resolver.mock.calls.flatMap(([, , positions]) => Object.keys(positions));
    expect(asked, 'only the position the operator typed over was resolved').toEqual(['1']);
  });

  it('still resolves a name typed over a position, with the id that position holds now', async () => {
    const utils = await mountPanel();
    await typeName(utils, 2, 'Kato');
    api.fetchLineupInForce.mockResolvedValue(matchLineup(NAMES));

    await click(saveButton(utils));

    expect(api.addTeamMember).toHaveBeenCalledTimes(1);
    expect(api.addTeamMember.mock.calls[0][2]).toBe('Kato');
    const asked = resolver.mock.calls.flatMap(([, , positions]) => Object.keys(positions));
    expect(asked).toEqual(['2']);
    expect(putOf(api.putMatchLineup.mock.calls[0]).positions).toEqual({ 1: 'Aoki', 2: 'Kato', 3: 'Ito' });
  });

  it('resolves a name typed over a position against the members as the Save read them again: a member another device created is placed by its id, not minted a second time', async () => {
    const utils = await mountPanel();
    await typeName(utils, 1, 'Zed');
    // Meanwhile another device created Zed and Kato, and put Kato at Position 2.
    api.fetchSquads.mockResolvedValue({ 'team-a': [...SQUAD, ZED, KATO] });
    api.fetchLineupInForce.mockResolvedValue(matchLineup(KATO_AT_2));

    await click(saveButton(utils));

    expect(api.addTeamMember, 'Zed is the member that exists, not a new one').not.toHaveBeenCalled();
    expect(putOf(api.putMatchLineup.mock.calls[0])).toEqual({
      positions: { 1: 'Zed', 2: 'Kato', 3: 'Ito' },
      memberIds: { 1: 'mem-9', 2: 'mem-5', 3: 'mem-3' },
    });
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

  it('clears a position the operator cleared, whatever another device put there meanwhile', async () => {
    const utils = await mountPanel();
    await act(async () => { fireEvent.click(within(utils.getByTestId('match-lineup-pos-team-a-1')).getByRole('button', { name: 'Clear player' })); });
    api.fetchLineupInForce.mockResolvedValue(matchLineup({
      positions: { 1: 'Oda', 2: 'Kato', 3: 'Ito' }, memberIds: { 1: 'mem-8', 2: 'mem-5', 3: 'mem-3' },
    }));

    await click(saveButton(utils));

    expect(putOf(api.putMatchLineup.mock.calls[0])).toEqual({
      positions: { 2: 'Kato', 3: 'Ito' },
      memberIds: { 2: 'mem-5', 3: 'mem-3' },
    });
  });

  describe('when the lineup cannot be read again', () => {
    it('writes the form as it was loaded and changed, as a save always was', async () => {
      const utils = await mountPanel();
      await typeName(utils, 1, 'Mori');
      api.fetchLineupInForce.mockRejectedValue(new TypeError('Failed to fetch'));

      await click(saveButton(utils));

      expect(api.fetchLineupInForce, 'the read was tried').toHaveBeenCalledTimes(2);
      expect(putOf(api.putMatchLineup.mock.calls[0])).toEqual({
        positions: { 1: 'Mori', 2: 'Sato', 3: 'Ito' },
        memberIds: { 1: 'mem-4', 2: 'mem-2', 3: 'mem-3' },
      });
    });

    it('queues the save the connection could not take, and keeps the operator\'s change on screen', async () => {
      const utils = await mountPanel();
      await typeName(utils, 1, 'Mori');
      api.fetchLineupInForce.mockRejectedValue(new TypeError('Failed to fetch'));
      api.putMatchLineup.mockResolvedValue({ queued: true });

      await click(saveButton(utils));

      expect(showToast).toHaveBeenCalledWith('Offline: match lineup not saved yet, will retry');
      expect(utils.getByLabelText('1 player').value).toBe('Mori');
      expect(saveButton(utils).disabled, 'still unsaved').toBe(false);
    });

    it('waits for a read that is never answered until its deadline, then writes the same', async () => {
      const utils = await mountPanel();
      await typeName(utils, 1, 'Mori');
      api.fetchLineupInForce.mockReturnValue(new Promise(() => {}));

      vi.useFakeTimers();
      try {
        await act(async () => { fireEvent.click(saveButton(utils)); });
        await act(async () => { vi.advanceTimersByTime(FETCH_TIMEOUT_MS - 1); });
        expect(api.putMatchLineup, 'nothing is written while the read may still answer').not.toHaveBeenCalled();
        expect(savingButton(utils).disabled).toBe(true);
        await act(async () => { vi.advanceTimersByTime(2); });
        await flush();
      } finally {
        vi.useRealTimers();
      }

      expect(api.putMatchLineup).toHaveBeenCalledTimes(1);
      expect(putOf(api.putMatchLineup.mock.calls[0]).positions).toEqual({ 1: 'Mori', 2: 'Sato', 3: 'Ito' });
    });
  });

  it('turns Save and the boxes off while the lineup is read again, and writes once it has been', async () => {
    const utils = await mountPanel();
    await typeName(utils, 1, 'Mori');
    const reread = deferred();
    api.fetchLineupInForce.mockReturnValue(reread.promise);

    await click(saveButton(utils));

    expect(savingButton(utils).disabled).toBe(true);
    expect(utils.getByLabelText('2 player').disabled).toBe(true);
    expect(api.putMatchLineup).not.toHaveBeenCalled();

    await act(async () => { reread.resolve(matchLineup(KATO_AT_2)); });
    await flush();

    expect(api.putMatchLineup).toHaveBeenCalledTimes(1);
    expect(putOf(api.putMatchLineup.mock.calls[0]).positions[2]).toBe('Kato');
    expect(saveButton(utils).disabled, 'saved: nothing left to save').toBe(true);
  });

  it('refuses a lineup that, composed on what is stored now, would field one member twice, and writes nothing', async () => {
    const utils = await mountPanel();
    await typeName(utils, 1, 'Mori');
    // Meanwhile another device put Mori at position 2.
    api.fetchLineupInForce.mockResolvedValue(matchLineup({
      positions: { 1: 'Aoki', 2: 'Mori', 3: 'Ito' }, memberIds: { 1: 'mem-1', 2: 'mem-4', 3: 'mem-3' },
    }));

    await click(saveButton(utils));

    expect(utils.getByText('Mori is already at 2.')).toBeTruthy();
    expect(api.putMatchLineup).not.toHaveBeenCalled();
    expect(saveButton(utils).disabled, 'the operator can still fix it and save').toBe(false);
    expect(utils.getByLabelText('2 player').value, 'the position the note names shows what the Save read').toBe('Mori');
    expect(utils.getByLabelText('1 player').value, 'the operator\'s own change stays').toBe('Mori');
  });

  it('names the position that was already there, not the one the operator typed into, when a typed name is a member stored elsewhere', async () => {
    api.fetchLineupInForce.mockResolvedValue(matchLineup(MORI_AT_1));
    const utils = await mountPanel();
    await typeName(utils, 2, 'Mori');

    await click(saveButton(utils));

    expect(utils.getByText('Mori is already at 1.')).toBeTruthy();
    expect(api.putMatchLineup).not.toHaveBeenCalled();
    expect(api.addTeamMember).not.toHaveBeenCalled();
  });

  it('names the position the operator picked the member for, not the box they typed the same name into', async () => {
    const utils = await mountPanel();
    await pickFromList(utils, 2, 'Mori');
    await typeName(utils, 1, 'Mori');

    await click(saveButton(utils));

    expect(utils.getByText('Mori is already at 2.')).toBeTruthy();
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
    it('is refused before the blank member a typed name is put on is renamed', async () => {
      api.fetchSquads.mockResolvedValue({ 'team-a': [...SQUAD, BLANK] });
      api.fetchLineupInForce.mockResolvedValue(matchLineup(BLANK_AT_3));
      const utils = await mountPanel();
      // The operator names the unnamed member at position 3 ...
      await typeName(utils, 3, 'Kato');
      // ... while another device moves that member to position 1.
      api.fetchLineupInForce.mockResolvedValue(matchLineup({
        positions: { 1: '', 2: 'Sato', 3: '' }, memberIds: { 1: 'mem-6', 2: 'mem-2' },
      }));

      await click(saveButton(utils));

      expect(utils.getByText('Kato is already at 1.')).toBeTruthy();
      expect(api.renameTeamMember, 'nobody is named for a Save that is refused').not.toHaveBeenCalled();
      expect(api.addTeamMember).not.toHaveBeenCalled();
      expect(api.putMatchLineup).not.toHaveBeenCalled();
    });

    it('is refused before a member is minted for another typed name', async () => {
      const utils = await mountPanel();
      await pickFromList(utils, 2, 'Mori');
      await typeName(utils, 3, 'Kato');
      // Another device put Mori at position 1 meanwhile.
      api.fetchLineupInForce.mockResolvedValue(matchLineup(MORI_AT_1));

      await click(saveButton(utils));

      expect(utils.getByText('Mori is already at 1.')).toBeTruthy();
      expect(api.addTeamMember, 'nothing is minted for a Save that is refused').not.toHaveBeenCalled();
      expect(api.renameTeamMember).not.toHaveBeenCalled();
      expect(api.putMatchLineup).not.toHaveBeenCalled();
    });

    it('is refused before anything is minted or renamed when a typed name is a member the lineup holds elsewhere', async () => {
      api.fetchLineupInForce.mockResolvedValue(matchLineup(MORI_AT_1));
      const utils = await mountPanel();
      await typeName(utils, 2, 'Mori');
      await typeName(utils, 3, 'Kato');

      await click(saveButton(utils));

      expect(utils.getByText('Mori is already at 1.')).toBeTruthy();
      expect(resolver, 'the names are not resolved for a Save that is refused').not.toHaveBeenCalled();
      expect(api.addTeamMember, 'Kato is not minted').not.toHaveBeenCalled();
      expect(api.renameTeamMember).not.toHaveBeenCalled();
      expect(api.putMatchLineup).not.toHaveBeenCalled();
    });

    it('counts the member a typed name belongs to, not the blank member the position held: another device moved that blank member, and the Save goes through', async () => {
      api.fetchSquads.mockResolvedValue({ 'team-a': [...SQUAD, BLANK] });
      api.fetchLineupInForce.mockResolvedValue(matchLineup(BLANK_AT_3));
      const utils = await mountPanel();
      // The operator types Mori, who is on the team, over the unnamed member at position 3 ...
      await typeName(utils, 3, 'Mori');
      // ... while another device moves that unnamed member to position 1.
      api.fetchLineupInForce.mockResolvedValue(matchLineup({
        positions: { 1: '', 2: 'Sato', 3: '' }, memberIds: { 1: 'mem-6', 2: 'mem-2' },
      }));

      await click(saveButton(utils));

      expect(api.renameTeamMember, 'Mori is not a new name for the unnamed member').not.toHaveBeenCalled();
      expect(api.addTeamMember).not.toHaveBeenCalled();
      expect(putOf(api.putMatchLineup.mock.calls[0])).toEqual({
        positions: { 1: '', 2: 'Sato', 3: 'Mori' },
        memberIds: { 1: 'mem-6', 2: 'mem-2', 3: 'mem-4' },
      });
    });

    it('does not take the member a typed-over position held for one it still holds: Sato moved elsewhere, and Kato typed over his old position, saves', async () => {
      const utils = await mountPanel();
      await typeName(utils, 2, 'Kato');
      // Another device moved Sato to position 1.
      api.fetchLineupInForce.mockResolvedValue(matchLineup({
        positions: { 1: 'Sato', 2: 'Ito', 3: 'Aoki' }, memberIds: { 1: 'mem-2', 2: 'mem-3', 3: 'mem-1' },
      }));

      await click(saveButton(utils));

      expect(api.addTeamMember).toHaveBeenCalledTimes(1);
      expect(putOf(api.putMatchLineup.mock.calls[0])).toEqual({
        positions: { 1: 'Sato', 2: 'Kato', 3: 'Aoki' },
        memberIds: { 1: 'mem-2', 2: 'mem-minted', 3: 'mem-1' },
      });
    });
  });

  it('refuses after the resolver a name that is new to the team typed at two positions: it is one member only once the first is minted', async () => {
    const utils = await mountPanel();
    await typeName(utils, 2, 'Kato');
    await typeName(utils, 3, 'Kato');

    await click(saveButton(utils));

    expect(api.addTeamMember, 'Kato is minted once, for the first position').toHaveBeenCalledTimes(1);
    expect(utils.getByText('Kato is already at 2.')).toBeTruthy();
    expect(api.putMatchLineup).not.toHaveBeenCalled();
  });

  describe('the ids handed to the name resolver', () => {
    // The seeded member for position 3 is unnamed; another device put it at position 2.
    const SEEDED = [{ id: 'mem-1', index: 1, name: 'Aoki' }, { id: 'mem-2', index: 2, name: 'Sato' }, { id: 'mem-3', index: 3, name: '' }];

    it('are the lineup as stored now, so a blank member another device placed is not renamed for a name typed at its own position: that mints', async () => {
      api.fetchSquads.mockResolvedValue({ 'team-a': SEEDED });
      api.fetchLineupInForce.mockResolvedValue(matchLineup({
        positions: { 1: 'Aoki', 2: 'Sato' }, memberIds: { 1: 'mem-1', 2: 'mem-2' },
      }));
      const utils = await mountPanel();
      await typeName(utils, 3, 'Kato');
      api.fetchLineupInForce.mockResolvedValue(matchLineup({
        positions: { 1: 'Aoki', 2: '' }, memberIds: { 1: 'mem-1', 2: 'mem-3' },
      }));

      await click(saveButton(utils));

      expect(resolver).toHaveBeenCalledTimes(1);
      const [, , asked, , , idsGiven] = resolver.mock.calls[0];
      expect(asked).toEqual({ 3: 'Kato' });
      expect(idsGiven, 'what another device placed counts').toEqual({ 1: 'mem-1', 2: 'mem-3', 3: '' });
      expect(api.renameTeamMember, 'the member at position 2 keeps its place and its blank name').not.toHaveBeenCalled();
      expect(api.addTeamMember).toHaveBeenCalledWith('comp-1', 'team-a', 'Kato', 'pw');
      expect(putOf(api.putMatchLineup.mock.calls[0])).toEqual({
        positions: { 1: 'Aoki', 2: '', 3: 'Kato' },
        memberIds: { 1: 'mem-1', 2: 'mem-3', 3: 'mem-minted' },
      });
    });
  });

  it('keeps a position left alone that holds a member and no name, with its id', async () => {
    api.fetchSquads.mockResolvedValue({ 'team-a': [...SQUAD, BLANK] });
    api.fetchLineupInForce.mockResolvedValue(matchLineup(BLANK_AT_3));
    const utils = await mountPanel();
    await typeName(utils, 1, 'Mori');

    await click(saveButton(utils));

    expect(putOf(api.putMatchLineup.mock.calls[0])).toEqual({
      positions: { 1: 'Mori', 2: 'Sato', 3: '' },
      memberIds: { 1: 'mem-4', 2: 'mem-2', 3: 'mem-6' },
    });
  });

  describe('while a Save waits for its read and its write', () => {
    it('turns an open rename box and its buttons off, since a rename made then would be overwritten by the Save', async () => {
      const utils = await mountPanel();
      await typeName(utils, 2, 'Mori');
      await click(panelRow(utils, 1).getByRole('button', { name: 'Rename 1 player' }));
      const reread = deferred();
      api.fetchLineupInForce.mockReturnValue(reread.promise);

      await click(saveButton(utils));

      expect(savingButton(utils).disabled).toBe(true);
      expect(panelRow(utils, 1).getByLabelText('Rename 1 player').disabled).toBe(true);
      expect(panelRow(utils, 1).getByRole('button', { name: 'Save' }).disabled, 'its own label does not say it is saving').toBe(true);
      expect(panelRow(utils, 1).getByRole('button', { name: 'Cancel' }).disabled).toBe(true);

      await act(async () => { reread.resolve(matchLineup(NAMES)); });
      await flush();
      expect(panelRow(utils, 1).getByLabelText('Rename 1 player').disabled, 'usable again once the Save is done').toBe(false);
    });

    it('turns the draft notice\'s Discard off', async () => {
      keepDraft('match:Pool A-2', NAMES);
      const utils = await mountPanel();
      expect(utils.getByText('Unsaved lineup changes restored')).toBeTruthy();
      const reread = deferred();
      api.fetchLineupInForce.mockReturnValue(reread.promise);

      await click(saveButton(utils));

      expect(utils.getByRole('button', { name: 'Discard' }).disabled).toBe(true);
      await act(async () => { reread.resolve(matchLineup(NAMES)); });
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

    await act(async () => { renamed.resolve(true); });
    await flush();

    expect(saveButton(utils).disabled, 'the rename has landed, and the lineup is still unsaved').toBe(false);
  });
});

// A side of a match can be given another team while the panel is open (a knockout
// feeder decided on another device). That side's editor is then a new one, so
// whatever the old team's Save still has out lands nowhere.
describe('the at-court panel when a side is given another team', () => {
  const B_MEMBERS = [
    { id: 'b-1', index: 1, name: 'Baba' }, { id: 'b-2', index: 2, name: 'Bando' },
    { id: 'b-3', index: 3, name: 'Bessho' }, { id: 'b-4', index: 4, name: 'Bito' },
  ];
  const C_MEMBERS = [
    { id: 'c-1', index: 1, name: 'Chiba' }, { id: 'c-2', index: 2, name: 'Cho' },
    { id: 'c-3', index: 3, name: 'Date' }, { id: 'c-4', index: 4, name: 'Endo' },
  ];
  const LINEUPS = {
    'team-a': NAMES,
    'team-b': { positions: { 1: 'Baba', 2: 'Bando', 3: 'Bessho' }, memberIds: { 1: 'b-1', 2: 'b-2', 3: 'b-3' } },
    'team-c': { positions: { 1: 'Chiba', 2: 'Cho', 3: 'Date' }, memberIds: { 1: 'c-1', 2: 'c-2', 3: 'c-3' } },
  };
  // Team B plays on the SHIRO side until team C takes its place.
  const WITH_B = { ...PANEL_MATCH, sideB: B };
  const WITH_C = { ...PANEL_MATCH, sideB: C };

  beforeEach(() => {
    api.fetchSquads.mockResolvedValue({ 'team-a': SQUAD, 'team-b': B_MEMBERS, 'team-c': C_MEMBERS });
    api.fetchLineupInForce.mockImplementation((_c, teamId) => Promise.resolve(
      lineupFor({ ...LINEUPS[teamId], teamId, matchId: 'Pool A-2', sourceMatchId: 'Pool A-0' }),
    ));
  });

  const panelFor = (match) => (
    <MatchLineupPanel match={match} tournament={{ competitions: [COMP] }} password="pw" showToast={showToast} onClose={() => {}} variant="inline" />
  );
  const side = (utils, teamId) => within(utils.getByTestId(`match-lineup-side-${teamId}`));

  async function mountMatchPanel(match) {
    let utils;
    await act(async () => { utils = render(panelFor(match)); });
    await flush();
    return utils;
  }

  // The operator types a name nobody has at team B's position 1 and saves.
  async function saveNewNameForTeamB(utils) {
    await act(async () => {
      const input = side(utils, 'team-b').getByLabelText('1 player');
      fireEvent.focus(input);
      fireEvent.change(input, { target: { value: 'Kobayashi' } });
      fireEvent.keyDown(input, { key: 'Enter' });
    });
    await click(side(utils, 'team-b').getByRole('button', { name: /^Save lineup$/ }));
  }

  async function giveTeamCTheSide(utils) {
    await act(async () => { utils.rerender(panelFor(WITH_C)); });
    await flush();
  }

  // The new team's editor shows its own lineup and offers its own members, none of team B's.
  async function expectTeamCAlone(utils) {
    const c = side(utils, 'team-c');
    expect(c.getByLabelText('1 player').value).toBe('Chiba');
    expect(c.getByLabelText('2 player').value).toBe('Cho');
    expect(c.getByLabelText('3 player').value).toBe('Date');
    // The open list shows the query, not the value: look, then close it again.
    await act(async () => { fireEvent.focus(c.getByLabelText('1 player')); });
    expect(c.getByText('Endo'), 'its own unplaced member').toBeTruthy();
    expect(c.queryByText('Bito'), 'team B\'s unplaced member').toBeNull();
    expect(c.queryByText('Kobayashi'), 'the member team B\'s Save minted').toBeNull();
    await act(async () => { fireEvent.keyDown(c.getByLabelText('1 player'), { key: 'Escape' }); });
  }

  it('lands nothing of a write the old team still has out in the new team\'s lineup or members', async () => {
    const utils = await mountMatchPanel(WITH_B);
    const write = deferred();
    api.putMatchLineup.mockReturnValue(write.promise);
    await saveNewNameForTeamB(utils);
    expect(api.putMatchLineup, 'team B\'s write is out').toHaveBeenCalledTimes(1);

    await giveTeamCTheSide(utils);
    await expectTeamCAlone(utils);
    await act(async () => {
      write.resolve({ positions: { 1: 'Kobayashi', 2: 'Bando', 3: 'Bessho' }, memberIds: { 1: 'mem-minted', 2: 'b-2', 3: 'b-3' } });
    });
    await flush();

    await expectTeamCAlone(utils);
  });

  it('lands nothing either of the member the old team\'s Save was still minting', async () => {
    const utils = await mountMatchPanel(WITH_B);
    const mint = deferred();
    api.addTeamMember.mockReturnValue(mint.promise);
    await saveNewNameForTeamB(utils);
    expect(api.addTeamMember, 'the member is being minted for team B').toHaveBeenCalledTimes(1);

    await giveTeamCTheSide(utils);
    await expectTeamCAlone(utils);
    await act(async () => { mint.resolve({ id: 'mem-minted', index: 6, name: 'Kobayashi' }); });
    await flush();

    await expectTeamCAlone(utils);
  });
});

describe('the Lineups page', () => {
  it('a starting lineup: writes what another device changed to a position left alone, with the operator\'s own change', async () => {
    const utils = await mountPage();
    await pick(utils, 1, 'mem-4');
    api.fetchTeamLineup.mockResolvedValue(startingLineup(KATO_AT_2));

    await click(saveButton(utils));

    expect(api.fetchTeamLineup, 'the lineup is read again at Save').toHaveBeenCalledTimes(2);
    expect(api.putTeamLineup).toHaveBeenCalledTimes(1);
    const [, , round] = api.putTeamLineup.mock.calls[0];
    expect(round).toBe(0);
    expect(putOf(api.putTeamLineup.mock.calls[0])).toEqual({
      positions: { 1: 'Mori', 2: 'Kato', 3: 'Ito' },
      memberIds: { 1: 'mem-4', 2: 'mem-5', 3: 'mem-3' },
    });
  });

  it('a match\'s lineup: the same', async () => {
    const utils = await mountPage();
    await chooseTarget(utils, 'Pool A-2');
    await pick(utils, 1, 'mem-4');
    api.fetchLineupInForce.mockResolvedValue(matchLineup(KATO_AT_2));

    await click(saveButton(utils));

    expect(api.putMatchLineup).toHaveBeenCalledTimes(1);
    expect(putOf(api.putMatchLineup.mock.calls[0])).toEqual({
      positions: { 1: 'Mori', 2: 'Kato', 3: 'Ito' },
      memberIds: { 1: 'mem-4', 2: 'mem-5', 3: 'mem-3' },
    });
  });

  it('clears a position the operator cleared, whatever another device put there meanwhile', async () => {
    const utils = await mountPage();
    await pick(utils, 1, '');
    api.fetchTeamLineup.mockResolvedValue(startingLineup({
      positions: { 1: 'Oda', 2: 'Kato', 3: 'Ito' }, memberIds: { 1: 'mem-8', 2: 'mem-5', 3: 'mem-3' },
    }));

    await click(saveButton(utils));

    expect(putOf(api.putTeamLineup.mock.calls[0])).toEqual({
      positions: { 2: 'Kato', 3: 'Ito' },
      memberIds: { 2: 'mem-5', 3: 'mem-3' },
    });
  });

  describe('when the lineup cannot be read again', () => {
    it('writes the form as it was loaded and changed, as a save always was', async () => {
      const utils = await mountPage();
      await pick(utils, 1, 'mem-4');
      api.fetchTeamLineup.mockRejectedValue(new TypeError('Failed to fetch'));

      await click(saveButton(utils));

      expect(api.fetchTeamLineup, 'the read was tried').toHaveBeenCalledTimes(2);
      expect(putOf(api.putTeamLineup.mock.calls[0])).toEqual({
        positions: { 1: 'Mori', 2: 'Sato', 3: 'Ito' },
        memberIds: { 1: 'mem-4', 2: 'mem-2', 3: 'mem-3' },
      });
    });

    it('queues the save the connection could not take, and keeps the operator\'s change on screen', async () => {
      const utils = await mountPage();
      await pick(utils, 1, 'mem-4');
      api.fetchTeamLineup.mockRejectedValue(new TypeError('Failed to fetch'));
      api.putTeamLineup.mockResolvedValue({ queued: true });

      await click(saveButton(utils));

      expect(showToast).toHaveBeenCalledWith('Offline: lineup not saved yet, will retry');
      expect(utils.getByTestId('lineup-position-1').value).toBe('mem-4');
    });

    it('waits for a read that is never answered until its deadline, then writes the same', async () => {
      const utils = await mountPage();
      await pick(utils, 1, 'mem-4');
      api.fetchTeamLineup.mockReturnValue(new Promise(() => {}));

      vi.useFakeTimers();
      try {
        await act(async () => { fireEvent.click(saveButton(utils)); });
        await act(async () => { vi.advanceTimersByTime(FETCH_TIMEOUT_MS - 1); });
        expect(api.putTeamLineup, 'nothing is written while the read may still answer').not.toHaveBeenCalled();
        expect(savingButton(utils).disabled).toBe(true);
        await act(async () => { vi.advanceTimersByTime(2); });
        await flush();
      } finally {
        vi.useRealTimers();
      }

      expect(api.putTeamLineup).toHaveBeenCalledTimes(1);
      expect(putOf(api.putTeamLineup.mock.calls[0]).positions).toEqual({ 1: 'Mori', 2: 'Sato', 3: 'Ito' });
    });
  });

  it('turns Save and the pickers off while the lineup is read again, and writes once it has been', async () => {
    const utils = await mountPage();
    await pick(utils, 1, 'mem-4');
    const reread = deferred();
    api.fetchTeamLineup.mockReturnValue(reread.promise);

    await click(saveButton(utils));

    expect(savingButton(utils).disabled).toBe(true);
    expect(utils.getByTestId('lineup-position-2').disabled).toBe(true);
    expect(api.putTeamLineup).not.toHaveBeenCalled();

    await act(async () => { reread.resolve(startingLineup(KATO_AT_2)); });
    await flush();

    expect(api.putTeamLineup).toHaveBeenCalledTimes(1);
    expect(putOf(api.putTeamLineup.mock.calls[0]).positions[2]).toBe('Kato');
  });

  it('refuses a lineup that, composed on what is stored now, would field one member twice, and writes nothing', async () => {
    const utils = await mountPage();
    await pick(utils, 1, 'mem-4');
    // Meanwhile another device put Mori at position 2.
    api.fetchTeamLineup.mockResolvedValue(startingLineup({
      positions: { 1: 'Aoki', 2: 'Mori', 3: 'Ito' }, memberIds: { 1: 'mem-1', 2: 'mem-4', 3: 'mem-3' },
    }));

    await click(saveButton(utils));

    expect(utils.getByText('Mori is already at Position 2.')).toBeTruthy();
    expect(api.putTeamLineup).not.toHaveBeenCalled();
    expect(saveButton(utils).disabled, 'the operator can still fix it and save').toBe(false);
  });

  it('names the position that was already there when another device\'s placement is the one the operator picked a member for, shows it, and lets a change to it through', async () => {
    const utils = await mountPage();
    await pick(utils, 2, 'mem-4');
    // Meanwhile another device put Mori at Position 1.
    api.fetchTeamLineup.mockResolvedValue(startingLineup(MORI_AT_1));

    await click(saveButton(utils));

    expect(utils.getByText('Mori is already at Position 1.')).toBeTruthy();
    expect(api.putTeamLineup).not.toHaveBeenCalled();
    // What the Save read is on screen, beside the operator's own pick.
    expect(utils.getByTestId('lineup-position-1').value).toBe('mem-4');
    expect(utils.getByTestId('lineup-position-1').selectedOptions[0].textContent).toContain('Mori');
    expect(utils.getByTestId('lineup-position-2').value).toBe('mem-4');

    // Aoki, who was at Position 1 when this page was read, is not what is stored there now.
    await pick(utils, 1, 'mem-1');
    await click(saveButton(utils));

    expect(api.putTeamLineup).toHaveBeenCalledTimes(1);
    expect(putOf(api.putTeamLineup.mock.calls[0])).toEqual({
      positions: { 1: 'Aoki', 2: 'Mori', 3: 'Ito' },
      memberIds: { 1: 'mem-1', 2: 'mem-4', 3: 'mem-3' },
    });
  });

  it('shows a member another device created and placed at a position the operator left alone, in that position\'s picker, after a Save that is refused', async () => {
    const utils = await mountPage();
    await pick(utils, 2, 'mem-4');
    // Meanwhile another device put Mori at Position 1, and a member it created, Zed, at Position 3.
    api.fetchSquads.mockResolvedValue({ 'team-a': [...SQUAD, ZED] });
    api.fetchTeamLineup.mockResolvedValue(startingLineup({
      positions: { ...MORI_AT_1.positions, 3: 'Zed' }, memberIds: { ...MORI_AT_1.memberIds, 3: 'mem-9' },
    }));

    await click(saveButton(utils));

    expect(utils.getByText('Mori is already at Position 1.')).toBeTruthy();
    expect(api.putTeamLineup).not.toHaveBeenCalled();
    const select = utils.getByTestId('lineup-position-3');
    expect(select.value).toBe('mem-9');
    expect(select.selectedOptions[0].textContent).toContain('Zed');
    expect(utils.getByTestId('squad-member-mem-9'), 'and lists them with the team\'s members').toBeTruthy();
  });

  it('holds the write until the members the Save read again have answered, and writes it once they have', async () => {
    const utils = await mountPage();
    await pick(utils, 1, 'mem-4');
    // Meanwhile another device put Zed, a member it created, at Position 3.
    const members = deferred();
    api.fetchSquads.mockReturnValue(members.promise);
    api.fetchTeamLineup.mockResolvedValue(startingLineup(ZED_AT_3));

    await click(saveButton(utils));

    expect(api.fetchSquads, 'the members are read again').toHaveBeenCalledTimes(2);
    expect(api.putTeamLineup, 'nothing is written while they are out').not.toHaveBeenCalled();
    expect(savingButton(utils).disabled).toBe(true);

    await act(async () => { members.resolve({ 'team-a': [...SQUAD, ZED] }); });
    await flush();

    expect(api.putTeamLineup).toHaveBeenCalledTimes(1);
    expect(putOf(api.putTeamLineup.mock.calls[0])).toEqual({
      positions: { 1: 'Mori', 2: 'Sato', 3: 'Zed' },
      memberIds: { 1: 'mem-4', 2: 'mem-2', 3: 'mem-9' },
    });
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

  it('a match\'s lineup: shows where the lineup the Save read was saved, whether or not the Save goes through', async () => {
    const utils = await mountPage();
    await chooseTarget(utils, 'Pool A-2');
    await pick(utils, 1, '');
    expect(utils.getByTestId('lineup-source').textContent).not.toMatch(/Lineup for this match/);
    // Meanwhile another device saved a lineup for this match, with Mori at Position 2.
    api.fetchLineupInForce.mockResolvedValue({
      ...matchLineup({ positions: { 1: 'Aoki', 2: 'Mori', 3: 'Ito' }, memberIds: { 1: 'mem-1', 2: 'mem-4', 3: 'mem-3' } }),
      sourceMatchId: 'Pool A-2',
    });
    api.putMatchLineup.mockRejectedValue(new Error('Failed to save lineup'));

    await click(saveButton(utils));

    expect(utils.getByTestId('lineup-source').textContent).toMatch(/Lineup for this match/);
    expect(utils.getByTestId('lineup-position-2').value, 'what the Save read, though it was not saved').toBe('mem-4');
    expect(utils.getByTestId('lineup-position-1').value, 'the position the operator cleared stays cleared').toBe('');
  });
});

describe('the Lineups page while a Save waits for its read and its write', () => {
  const openAddRow = async (utils, position) => {
    await pick(utils, position, '__add__');
    await act(async () => { fireEvent.change(utils.getByLabelText(`New member name for ${position}`), { target: { value: 'Kato' } }); });
  };

  it('turns off what would change the lineup or its members: Rename, Clear name, an open add row, an open rename box and Discard', async () => {
    keepDraft('start', NAMES);
    api.clearTeamMember = vi.fn().mockResolvedValue(true);
    const utils = await mountPage(DRAWN_COMP);
    expect(utils.getByText('Unsaved lineup changes restored')).toBeTruthy();
    await openAddRow(utils, 3);
    await click(memberRow(utils, 'mem-2').getByRole('button', { name: 'Rename' }));
    const reread = deferred();
    api.fetchTeamLineup.mockReturnValue(reread.promise);

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

    await act(async () => { reread.resolve(startingLineup(NAMES)); });
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

    await act(async () => { renamed.resolve(true); });
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

    await act(async () => { cleared.resolve(true); });
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

    await act(async () => { added.resolve({ id: 'mem-minted', index: 6, name: 'Kato' }); });
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
