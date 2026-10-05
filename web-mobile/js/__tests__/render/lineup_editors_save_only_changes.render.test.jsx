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
//  - a lineup composed that way is refused when it would field one member twice;
//  - a lineup followed from another device is shown with the team's members read
//    again, so a member created elsewhere is not shown as an empty slot.

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
const NAMES = { positions: { 1: 'Aoki', 2: 'Sato', 3: 'Ito' }, memberIds: { 1: 'mem-1', 2: 'mem-2', 3: 'mem-3' } };
const lineupFor = (extra) => ({ teamId: 'team-a', competitionId: 'comp-1', ...NAMES, saved: true, ...extra });
const STARTING = lineupFor({ round: 0, sourceRound: 0 });
const CARRIED = lineupFor({ matchId: 'Pool A-0', sourceMatchId: 'Pool A-0' });
// What the server holds once another device has set position 2 to a member this
// editor has never heard of.
const KATO_AT_2 = { positions: { 1: 'Aoki', 2: 'Kato', 3: 'Ito' }, memberIds: { 1: 'mem-1', 2: 'mem-5', 3: 'mem-3' } };
const ZED_AT_3 = { positions: { 1: 'Aoki', 2: 'Sato', 3: 'Zed' }, memberIds: { 1: 'mem-1', 2: 'mem-2', 3: 'mem-9' } };

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

async function mountPage() {
  let utils;
  await act(async () => {
    utils = render(<AdminTeamLineupsList comp={COMP} poolMatches={POOL_MATCHES} password="pw" showToast={showToast} />);
  });
  await flush();
  return utils;
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
