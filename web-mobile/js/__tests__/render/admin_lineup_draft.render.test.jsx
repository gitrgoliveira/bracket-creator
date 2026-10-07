// The Lineups page keeps the position picks the operator has not saved as a
// draft in this tab's sessionStorage (bc-lnul, operator decision 2026-10-05) and
// offers them back, with "Unsaved lineup changes restored" and a Discard, when
// the same lineup is opened again: after a reload, after the app's Back, or after
// picking another lineup and coming back. The starting lineup and a match each
// have a draft of their own, and a match's draft is the one the at-court panel
// keeps for it. Naming, renaming and clearing a team member write through the API
// and are not drafts. Nothing is written to the server until Save.

import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { lineupPutStub } from '../helpers/lineup_server.js';

const SQUAD = [
  { id: 'mem-1', index: 1, name: 'Aoki' },
  { id: 'mem-2', index: 2, name: 'Sato' },
  { id: 'mem-3', index: 3, name: 'Ito' },
  { id: 'mem-4', index: 4, name: 'Mori' },
  { id: 'mem-5', index: 5, name: 'Kato' },
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

const MATCH_KEY = 'bc.lineupDraft.v1:comp-1:team-a:match:Pool A-2';
const START_KEY = 'bc.lineupDraft.v1:comp-1:team-a:start';
const draftAt = (key) => JSON.parse(sessionStorage.getItem(key));
const NOTICE = 'lineup-draft-notice';

let AdminTeamLineupsList;
let MatchLineupSideEditor;
let saved;
let api;

beforeEach(async () => {
  sessionStorage.clear();
  saved = { API: window.API, confirmDialog: window.confirmDialog };
  window.confirmDialog = vi.fn().mockResolvedValue(true);
  api = {
    fetchTeamLineup: vi.fn().mockResolvedValue(STARTING),
    fetchLineupInForce: vi.fn().mockResolvedValue(CARRIED),
    fetchSquads: vi.fn().mockResolvedValue({ 'team-a': SQUAD }),
    // A save names the positions it changed, and the server answers the lineup it
    // holds then (lineupPutStub): the lineups the page read are what it holds.
    putTeamLineup: lineupPutStub({ positions: { ...NAMES.positions }, memberIds: { ...NAMES.memberIds } }),
    putMatchLineup: lineupPutStub({ positions: { ...NAMES.positions }, memberIds: { ...NAMES.memberIds } }),
    deleteMatchLineup: vi.fn().mockResolvedValue(true),
  };
  window.API = api;
  ({ AdminTeamLineupsList } = await import('../../admin_lineup.jsx'));
  ({ MatchLineupSideEditor } = await import('../../admin_schedule_lineup.jsx'));
});

afterEach(() => {
  window.API = saved.API;
  window.confirmDialog = saved.confirmDialog;
});

async function mountPage(props = {}) {
  let utils;
  await act(async () => {
    utils = render(
      <AdminTeamLineupsList comp={COMP} poolMatches={POOL_MATCHES} password="pw" showToast={vi.fn()} {...props} />
    );
  });
  await act(async () => { await Promise.resolve(); });
  return utils;
}

async function chooseTarget(utils, value) {
  await act(async () => { fireEvent.change(utils.getByLabelText('Lineup for'), { target: { value } }); });
  await act(async () => { await Promise.resolve(); });
}

async function pick(utils, position, memberId) {
  await act(async () => {
    fireEvent.change(utils.getByTestId(`lineup-position-${position}`), { target: { value: memberId } });
  });
}

async function click(button) {
  await act(async () => { fireEvent.click(button); });
  await act(async () => { await Promise.resolve(); });
}

const saveButton = (utils) => utils.getByRole('button', { name: /^Save lineup$/ });
const discardButton = (utils) => utils.getByRole('button', { name: 'Discard' });
const positionValue = (utils, position) => utils.getByTestId(`lineup-position-${position}`).value;

describe('a match\'s unsaved picks', () => {
  it('are offered back with a Discard when the page is opened again on that match', async () => {
    const first = await mountPage();
    await chooseTarget(first, 'Pool A-2');
    await pick(first, 1, 'mem-4');
    first.unmount();

    const again = await mountPage();
    await chooseTarget(again, 'Pool A-2');

    expect(positionValue(again, 1)).toBe('mem-4');
    expect(positionValue(again, 2)).toBe('mem-2');
    const notice = again.getByTestId(NOTICE);
    expect(notice.textContent).toContain('Unsaved lineup changes restored');
    expect(notice.getAttribute('role')).toBe('status');
    expect(notice.className).toContain('alert--warn');
    expect(discardButton(again)).toBeTruthy();
    expect(saveButton(again).disabled).toBe(false);
    // Restoring is not saving.
    expect(api.putMatchLineup).not.toHaveBeenCalled();
    expect(api.putTeamLineup).not.toHaveBeenCalled();
  });

  it('are kept in this tab\'s sessionStorage under the match\'s key, the one the at-court panel uses', async () => {
    const utils = await mountPage();
    await chooseTarget(utils, 'Pool A-2');
    await pick(utils, 1, 'mem-4');
    const draft = draftAt(MATCH_KEY);
    expect(draft.current).toEqual({ positions: { 1: 'Mori', 2: 'Sato', 3: 'Ito' }, memberIds: { 1: 'mem-4', 2: 'mem-2', 3: 'mem-3' } });
    expect(draft.baseline).toEqual(NAMES);
    expect(sessionStorage.getItem(START_KEY)).toBeNull();
  });

  it('are Discarded back to the lineup that was loaded, and the notice and the draft go', async () => {
    const first = await mountPage();
    await chooseTarget(first, 'Pool A-2');
    await pick(first, 1, 'mem-4');
    first.unmount();
    const again = await mountPage();
    await chooseTarget(again, 'Pool A-2');

    await click(discardButton(again));

    expect(positionValue(again, 1)).toBe('mem-1');
    expect(again.queryByTestId(NOTICE)).toBeNull();
    expect(saveButton(again).disabled).toBe(true);
    expect(sessionStorage.getItem(MATCH_KEY)).toBeNull();
  });

  it('are cleared by Save, with the notice, and a later open shows none', async () => {
    const first = await mountPage();
    await chooseTarget(first, 'Pool A-2');
    await pick(first, 1, 'mem-4');
    first.unmount();
    const again = await mountPage();
    await chooseTarget(again, 'Pool A-2');

    await click(saveButton(again));

    expect(api.putMatchLineup).toHaveBeenCalledWith(
      'comp-1', 'team-a', 'Pool A-2', { 1: 'Mori' }, 'pw', { 1: 'mem-4' }, ['1'],
    );
    expect(again.queryByTestId(NOTICE)).toBeNull();
    expect(sessionStorage.getItem(MATCH_KEY)).toBeNull();
    again.unmount();

    api.fetchLineupInForce.mockResolvedValue({ ...OWN, positions: { 1: 'Mori', 2: 'Sato', 3: 'Ito' }, memberIds: { 1: 'mem-4', 2: 'mem-2', 3: 'mem-3' } });
    const reopened = await mountPage();
    await chooseTarget(reopened, 'Pool A-2');
    expect(reopened.queryByTestId(NOTICE)).toBeNull();
    expect(positionValue(reopened, 1)).toBe('mem-4');
  });

  it('are kept while another lineup is picked and come back when the match is picked again', async () => {
    const utils = await mountPage();
    await chooseTarget(utils, 'Pool A-2');
    await pick(utils, 1, 'mem-4');

    await chooseTarget(utils, '');
    expect(utils.queryByTestId(NOTICE)).toBeNull();
    expect(positionValue(utils, 1)).toBe('mem-1');
    await chooseTarget(utils, 'Pool A-2');

    expect(positionValue(utils, 1)).toBe('mem-4');
    expect(utils.getByTestId(NOTICE).textContent).toContain('Unsaved lineup changes restored');
  });

  it('are not offered on another match, or on another team\'s lineup', async () => {
    const first = await mountPage();
    await chooseTarget(first, 'Pool A-2');
    await pick(first, 1, 'mem-4');
    first.unmount();

    const again = await mountPage();
    await chooseTarget(again, 'Pool A-0');
    expect(positionValue(again, 1)).toBe('mem-1');
    expect(again.queryByTestId(NOTICE)).toBeNull();

    await act(async () => { fireEvent.change(again.getByLabelText('Team'), { target: { value: 'team-b' } }); });
    await act(async () => { await Promise.resolve(); });
    expect(again.queryByTestId(NOTICE)).toBeNull();
  });

  it('go when the match is told to use the previous match\'s lineup', async () => {
    api.fetchLineupInForce.mockResolvedValueOnce(OWN).mockResolvedValue(CARRIED);
    const utils = await mountPage();
    await chooseTarget(utils, 'Pool A-2');
    await pick(utils, 1, 'mem-4');
    expect(draftAt(MATCH_KEY)).not.toBeNull();

    await click(utils.getByRole('button', { name: "Use the previous match's lineup" }));

    expect(positionValue(utils, 1)).toBe('mem-1');
    expect(sessionStorage.getItem(MATCH_KEY)).toBeNull();
    expect(utils.queryByTestId(NOTICE)).toBeNull();
  });
});

describe('the starting lineup\'s unsaved picks', () => {
  it('are offered back with a Discard when the page is opened again', async () => {
    const first = await mountPage();
    await pick(first, 2, 'mem-4');
    expect(draftAt(START_KEY).current.positions[2]).toBe('Mori');
    first.unmount();

    const again = await mountPage();

    expect(positionValue(again, 2)).toBe('mem-4');
    expect(again.getByTestId(NOTICE).textContent).toContain('Unsaved lineup changes restored');
    expect(discardButton(again)).toBeTruthy();
    expect(api.putTeamLineup).not.toHaveBeenCalled();
  });

  it('are Discarded back to the starting lineup that was loaded', async () => {
    const first = await mountPage();
    await pick(first, 2, 'mem-4');
    first.unmount();
    const again = await mountPage();

    await click(discardButton(again));

    expect(positionValue(again, 2)).toBe('mem-2');
    expect(again.queryByTestId(NOTICE)).toBeNull();
    expect(sessionStorage.getItem(START_KEY)).toBeNull();
  });

  it('are cleared by Save, which waits for a change like a match\'s, and a later open shows no notice', async () => {
    const first = await mountPage();
    expect(saveButton(first).disabled).toBe(true);
    await pick(first, 2, 'mem-4');
    expect(saveButton(first).disabled).toBe(false);
    first.unmount();
    const again = await mountPage();
    expect(again.getByTestId(NOTICE)).toBeTruthy();

    await click(saveButton(again));

    expect(api.putTeamLineup).toHaveBeenCalledWith(
      'comp-1', 'team-a', 0, { 2: 'Mori' }, 'pw', { 2: 'mem-4' }, ['2'],
    );
    expect(again.queryByTestId(NOTICE)).toBeNull();
    expect(sessionStorage.getItem(START_KEY)).toBeNull();
    again.unmount();

    api.fetchTeamLineup.mockResolvedValue({ ...STARTING, positions: { 1: 'Aoki', 2: 'Mori', 3: 'Ito' }, memberIds: { 1: 'mem-1', 2: 'mem-4', 3: 'mem-3' } });
    const reopened = await mountPage();
    expect(reopened.queryByTestId(NOTICE)).toBeNull();
    expect(positionValue(reopened, 2)).toBe('mem-4');
  });

  it('are not cleared by a save that was only queued (offline): the lineup is still unsaved', async () => {
    api.putTeamLineup.mockResolvedValue({ queued: true });
    const utils = await mountPage();
    await pick(utils, 2, 'mem-4');
    await click(saveButton(utils));
    expect(draftAt(START_KEY).current.positions[2]).toBe('Mori');
  });

  it('put no notice and no draft behind a lineup nobody changed', async () => {
    const first = await mountPage();
    expect(first.queryByTestId(NOTICE)).toBeNull();
    first.unmount();
    expect(sessionStorage.length).toBe(0);
  });
});

describe('a draft started in the at-court panel', () => {
  it('is offered on the Lineups page for that match, with the team member it picked', async () => {
    let panel;
    await act(async () => {
      panel = render(
        <MatchLineupSideEditor comp={COMP} team={A} match={{ ...POOL_MATCHES[2], compId: 'comp-1' }} allMatches={POOL_MATCHES} password="pw" showToast={vi.fn()} />
      );
    });
    await act(async () => { await Promise.resolve(); });
    // Pick Mori from the position's list, so the pick carries the member's id.
    await act(async () => { fireEvent.focus(panel.getByLabelText('1 player')); });
    await act(async () => { fireEvent.click(panel.getByText('Mori').closest('button')); });
    expect(draftAt(MATCH_KEY).current.memberIds[1]).toBe('mem-4');
    panel.unmount();

    const page = await mountPage();
    await chooseTarget(page, 'Pool A-2');

    expect(positionValue(page, 1)).toBe('mem-4');
    expect(page.getByTestId(NOTICE).textContent).toContain('Unsaved lineup changes restored');
  });

  it('is offered back in the panel when the Lineups page started it', async () => {
    const first = await mountPage();
    await chooseTarget(first, 'Pool A-2');
    await pick(first, 1, 'mem-4');
    first.unmount();

    let panel;
    await act(async () => {
      panel = render(
        <MatchLineupSideEditor comp={COMP} team={A} match={{ ...POOL_MATCHES[2], compId: 'comp-1' }} allMatches={POOL_MATCHES} password="pw" showToast={vi.fn()} />
      );
    });
    await act(async () => { await Promise.resolve(); });

    expect(panel.getByLabelText('1 player').value).toBe('Mori');
    expect(panel.getByTestId('match-lineup-draft-team-a').textContent).toContain('Unsaved lineup changes restored');
  });
});

describe('a lineup saved meanwhile', () => {
  it('is not overwritten by an older draft: the notice names what was not restored', async () => {
    const first = await mountPage();
    await chooseTarget(first, 'Pool A-2');
    await pick(first, 1, 'mem-4');
    first.unmount();
    api.fetchLineupInForce.mockResolvedValue(SAVED_ELSEWHERE);

    const again = await mountPage();
    await chooseTarget(again, 'Pool A-2');

    const notice = again.getByTestId(NOTICE);
    expect(notice.textContent).toBe('Not restored, the lineup changed since: Mori');
    expect(notice.getAttribute('role')).toBe('status');
    expect(again.queryByRole('button', { name: 'Discard' })).toBeNull();
    // What the server holds is what is shown, and Save has nothing to write.
    expect(positionValue(again, 1)).toBe('mem-5');
    expect(saveButton(again).disabled).toBe(true);
    expect(sessionStorage.getItem(MATCH_KEY)).toBeNull();
    expect(api.putMatchLineup).not.toHaveBeenCalled();
  });

  it('is found on the starting lineup too', async () => {
    const first = await mountPage();
    await pick(first, 2, 'mem-4');
    first.unmount();
    api.fetchTeamLineup.mockResolvedValue({ ...STARTING, positions: { 1: 'Aoki', 2: 'Kato', 3: 'Ito' }, memberIds: { 1: 'mem-1', 2: 'mem-5', 3: 'mem-3' } });

    const again = await mountPage();

    expect(again.getByTestId(NOTICE).textContent).toBe('Not restored, the lineup changed since: Mori');
    expect(sessionStorage.getItem(START_KEY)).toBeNull();
  });
});

describe('a lineup that could not be read', () => {
  it('keeps the draft and shows no notice', async () => {
    const first = await mountPage();
    await chooseTarget(first, 'Pool A-2');
    await pick(first, 1, 'mem-4');
    first.unmount();
    const kept = sessionStorage.getItem(MATCH_KEY);
    api.fetchLineupInForce.mockRejectedValue(new Error('competition not found'));

    const failed = await mountPage();
    await chooseTarget(failed, 'Pool A-2');

    expect(failed.getByText('competition not found')).toBeTruthy();
    expect(failed.queryByTestId(NOTICE)).toBeNull();
    expect(sessionStorage.getItem(MATCH_KEY)).toBe(kept);
  });
});

describe('the Discard button', () => {
  it('is a plain .btn, so the coarse-pointer floor reaches it through the class, never an inline style', async () => {
    const first = await mountPage();
    await pick(first, 2, 'mem-4');
    first.unmount();
    const again = await mountPage();

    const discard = discardButton(again);
    expect(discard.className.split(/\s+/)).toContain('btn');
    expect(discard.hasAttribute('style')).toBe(false);
  });
});
