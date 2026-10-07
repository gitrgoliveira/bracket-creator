// A lineup the operator has changed but not saved survives a reload, the app's
// Back and closing the at-court panel (bc-lnul, operator decision 2026-10-05):
// each side keeps its unsaved picks as a draft in this tab's sessionStorage and
// offers them back when the same match's lineup is opened again, with "Unsaved
// lineup changes restored" and a Discard. Nothing reaches the server until the
// operator presses Save: a restored draft is never written on its own.

import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, beforeAll, beforeEach, afterAll, vi } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

const NAMES = { positions: { 1: 'Aoki', 2: 'Sato', 3: 'Ito' }, memberIds: { 1: 'mem-1', 2: 'mem-2', 3: 'mem-3' } };
const CARRIED = { teamId: 'uuid-grouped', matchId: 'Pool D-0', ...NAMES, sourceMatchId: 'Pool D-0', saved: true };
const OWN = { teamId: 'uuid-grouped', matchId: 'Pool D-1', ...NAMES, sourceMatchId: 'Pool D-1', saved: true };
const SAVED_ELSEWHERE = { ...CARRIED, positions: { 1: 'Kato', 2: 'Sato', 3: 'Ito' } };

const SQUAD = [
  { id: 'mem-1', index: 1, name: 'Aoki' },
  { id: 'mem-2', index: 2, name: 'Sato' },
  { id: 'mem-3', index: 3, name: 'Ito' },
];
const COMP = { id: 'comp-1', name: 'Team Event', kind: 'team', teamSize: 3 };
const TEAM = { id: 'uuid-grouped', name: 'Grouped Team', number: 'T5' };
const side = (id, other) => ({ id, compId: 'comp-1', phase: 'pool', poolName: 'Pool D', sideA: { id: 'uuid-grouped', name: 'Grouped Team' }, sideB: { id: other, name: other }, status: 'scheduled' });
const MATCH = side('Pool D-1', 'other');
const NEXT_MATCH = side('Pool D-2', 'third');
const EARLIER = side('Pool D-0', 'second');

const STORAGE_KEY = 'bc.lineupDraft.v1:comp-1:uuid-grouped:match:Pool D-1';
const draftOf = () => JSON.parse(sessionStorage.getItem(STORAGE_KEY));
const NOTICE = 'match-lineup-draft-uuid-grouped';

const api = {
  fetchLineupInForce: vi.fn(),
  fetchSquads: vi.fn().mockResolvedValue({ 'uuid-grouped': SQUAD }),
  putMatchLineup: vi.fn(),
  deleteMatchLineup: vi.fn(),
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
  sessionStorage.clear();
  api.fetchLineupInForce.mockReset().mockResolvedValue(CARRIED);
  api.putMatchLineup.mockReset().mockImplementation(
    (_c, _t, _m, positions, _pw, memberIds) => Promise.resolve({ positions, memberIds }),
  );
  api.deleteMatchLineup.mockReset().mockResolvedValue(true);
  window.confirmDialog.mockReset().mockResolvedValue(true);
});

async function mountPanel(match = MATCH) {
  let utils;
  await act(async () => {
    utils = render(
      <MatchLineupSideEditor comp={COMP} team={TEAM} match={match} allMatches={[EARLIER, MATCH, NEXT_MATCH]} password="pw" showToast={vi.fn()} />
    );
  });
  await act(async () => { await Promise.resolve(); });
  return utils;
}

async function typeName(utils, position, name) {
  await act(async () => {
    const input = utils.getByLabelText(`${position} player`);
    fireEvent.focus(input);
    fireEvent.change(input, { target: { value: name } });
    fireEvent.keyDown(input, { key: 'Enter' });
  });
}

async function click(button) {
  await act(async () => { fireEvent.click(button); });
  await act(async () => { await Promise.resolve(); });
}

const saveButton = (utils) => utils.getByRole('button', { name: /Save lineup/ });
const discardButton = (utils) => utils.getByRole('button', { name: 'Discard' });

describe('unsaved picks in the at-court lineup panel', () => {
  it('are kept when the panel goes away, and offered back with a Discard when it is opened again', async () => {
    const first = await mountPanel();
    await typeName(first, 1, 'Mori');
    expect(first.getByLabelText('1 player').value).toBe('Mori');
    first.unmount();

    const again = await mountPanel();

    expect(again.getByLabelText('1 player').value).toBe('Mori');
    expect(again.getByLabelText('2 player').value).toBe('Sato');
    const notice = again.getByTestId(NOTICE);
    expect(notice.textContent).toContain('Unsaved lineup changes restored');
    expect(notice.getAttribute('role')).toBe('status');
    expect(notice.className).toContain('alert--warn');
    expect(discardButton(again)).toBeTruthy();
    // Restoring is not saving: the operator's Save is the only write.
    expect(saveButton(again).disabled).toBe(false);
    expect(api.putMatchLineup).not.toHaveBeenCalled();
  });

  it('are kept in this tab\'s sessionStorage, against the lineup they were made on', async () => {
    const utils = await mountPanel();
    await typeName(utils, 1, 'Mori');
    const draft = draftOf();
    expect(draft.current.positions[1]).toBe('Mori');
    expect(draft.baseline.positions[1]).toBe('Aoki');
    expect(Number.isFinite(draft.savedAt)).toBe(true);
  });

  it('put no notice and no draft behind a lineup nobody changed', async () => {
    const first = await mountPanel();
    expect(first.queryByTestId(NOTICE)).toBeNull();
    first.unmount();
    expect(sessionStorage.length).toBe(0);
    const again = await mountPanel();
    expect(again.queryByTestId(NOTICE)).toBeNull();
  });

  it('are Discarded back to the lineup that was loaded, and the notice and the draft go', async () => {
    const first = await mountPanel();
    await typeName(first, 1, 'Mori');
    first.unmount();
    const again = await mountPanel();

    await click(discardButton(again));

    expect(again.getByLabelText('1 player').value).toBe('Aoki');
    expect(again.queryByTestId(NOTICE)).toBeNull();
    expect(saveButton(again).disabled).toBe(true);
    expect(sessionStorage.getItem(STORAGE_KEY)).toBeNull();
    expect(api.putMatchLineup).not.toHaveBeenCalled();
    again.unmount();
    expect((await mountPanel()).queryByTestId(NOTICE)).toBeNull();
  });

  it('are cleared by Save, with the notice, and a later open shows none', async () => {
    const first = await mountPanel();
    await typeName(first, 1, 'Mori');
    first.unmount();
    const again = await mountPanel();
    expect(again.getByTestId(NOTICE)).toBeTruthy();

    await click(saveButton(again));

    expect(api.putMatchLineup).toHaveBeenCalledTimes(1);
    expect(api.putMatchLineup.mock.calls[0].slice(0, 4)).toEqual(['comp-1', 'uuid-grouped', 'Pool D-1', { 1: 'Mori', 2: 'Sato', 3: 'Ito' }]);
    expect(again.queryByTestId(NOTICE)).toBeNull();
    expect(sessionStorage.getItem(STORAGE_KEY)).toBeNull();
    again.unmount();

    api.fetchLineupInForce.mockResolvedValue({ ...OWN, positions: { 1: 'Mori', 2: 'Sato', 3: 'Ito' } });
    const reopened = await mountPanel();
    expect(reopened.queryByTestId(NOTICE)).toBeNull();
    expect(reopened.getByLabelText('1 player').value).toBe('Mori');
  });

  it('are not cleared by a save that was only queued (offline): the lineup is still unsaved', async () => {
    api.putMatchLineup.mockResolvedValue({ queued: true });
    const utils = await mountPanel();
    await typeName(utils, 1, 'Mori');
    await click(saveButton(utils));
    expect(draftOf().current.positions[1]).toBe('Mori');
  });

  it('belong to their own match: another match of the team opens without them', async () => {
    const first = await mountPanel();
    await typeName(first, 1, 'Mori');
    first.unmount();

    const other = await mountPanel(NEXT_MATCH);

    expect(other.getByLabelText('1 player').value).toBe('Aoki');
    expect(other.queryByTestId(NOTICE)).toBeNull();
    expect(draftOf().current.positions[1]).toBe('Mori');
  });

  it('go when the match is told to use the previous match\'s lineup, which replaces what the panel shows', async () => {
    api.fetchLineupInForce.mockResolvedValueOnce(OWN).mockResolvedValue(CARRIED);
    const utils = await mountPanel();
    await typeName(utils, 1, 'Mori');
    expect(draftOf()).not.toBeNull();

    await click(utils.getByRole('button', { name: "Use the previous match's lineup" }));

    expect(utils.getByLabelText('1 player').value).toBe('Aoki');
    expect(sessionStorage.getItem(STORAGE_KEY)).toBeNull();
    expect(utils.queryByTestId(NOTICE)).toBeNull();
  });
});

describe('a lineup saved meanwhile', () => {
  it('is not overwritten by an older draft: the notice names what was not restored', async () => {
    const first = await mountPanel();
    await typeName(first, 1, 'Mori');
    first.unmount();
    api.fetchLineupInForce.mockResolvedValue(SAVED_ELSEWHERE);

    const again = await mountPanel();

    expect(again.getByLabelText('1 player').value).toBe('Kato');
    const notice = again.getByTestId(NOTICE);
    expect(notice.textContent).toBe('Not restored, the lineup changed since: Mori');
    expect(notice.getAttribute('role')).toBe('status');
    expect(notice.className).toContain('alert--warn');
    expect(again.queryByRole('button', { name: 'Discard' })).toBeNull();
    expect(saveButton(again).disabled).toBe(true);
    expect(sessionStorage.getItem(STORAGE_KEY)).toBeNull();
    expect(api.putMatchLineup).not.toHaveBeenCalled();
  });
});

// The operator's own save can land after the panel was closed: it was queued (offline),
// or still out when they closed it. A name typed in is given its member by the save, so
// the lineup the server then holds carries an id the draft never had. That is their
// save having landed, not another device's change, and it is never reported as one.
describe('the operator\'s own save, landed after the panel was closed', () => {
  const LANDED = { ...OWN, positions: { 1: 'Mori', 2: 'Sato', 3: 'Ito' }, memberIds: { 1: 'mem-9', 2: 'mem-2', 3: 'mem-3' } };

  beforeEach(() => {
    window.AdminLineupHelpers.resolveMemberIdsForPositions.mockResolvedValueOnce({
      memberIds: { 1: 'mem-9' }, squad: [...SQUAD, { id: 'mem-9', index: 4, name: 'Mori' }], failures: [],
    });
  });

  it('is no conflict when the save was queued and has since been written', async () => {
    api.putMatchLineup.mockResolvedValue({ queued: true });
    const first = await mountPanel();
    await typeName(first, 1, 'Mori');
    await click(saveButton(first));
    expect(api.putMatchLineup.mock.calls[0][5], 'the save carried the id of the member it named').toEqual({ 1: 'mem-9', 2: 'mem-2', 3: 'mem-3' });
    first.unmount();
    api.fetchLineupInForce.mockResolvedValue(LANDED);

    const again = await mountPanel();

    expect(again.queryByTestId(NOTICE), 'no "Not restored" and no restored draft').toBeNull();
    expect(again.getByLabelText('1 player').value).toBe('Mori');
    expect(saveButton(again).disabled).toBe(true);
    expect(sessionStorage.getItem(STORAGE_KEY)).toBeNull();
  });

  it('is no conflict either when the panel was closed while the save was still out', async () => {
    let answer;
    api.putMatchLineup.mockImplementation(() => new Promise((resolve) => { answer = resolve; }));
    const first = await mountPanel();
    await typeName(first, 1, 'Mori');
    await click(saveButton(first));
    first.unmount();
    await act(async () => { answer({ positions: LANDED.positions, memberIds: LANDED.memberIds }); });
    api.fetchLineupInForce.mockResolvedValue(LANDED);

    const again = await mountPanel();

    expect(again.queryByTestId(NOTICE)).toBeNull();
    expect(again.getByLabelText('1 player').value).toBe('Mori');
    expect(sessionStorage.getItem(STORAGE_KEY)).toBeNull();
  });
});

describe('a lineup that could not be read', () => {
  it('keeps the draft and shows no notice, and offers the draft once the lineup loads', async () => {
    const first = await mountPanel();
    await typeName(first, 1, 'Mori');
    first.unmount();
    const kept = sessionStorage.getItem(STORAGE_KEY);
    api.fetchLineupInForce.mockRejectedValue(new Error('competition not found'));

    const failed = await mountPanel();

    expect(failed.getByText('competition not found')).toBeTruthy();
    expect(failed.queryByTestId(NOTICE)).toBeNull();
    expect(sessionStorage.getItem(STORAGE_KEY)).toBe(kept);
    failed.unmount();

    api.fetchLineupInForce.mockResolvedValue(CARRIED);
    const recovered = await mountPanel();
    expect(recovered.getByLabelText('1 player').value).toBe('Mori');
    expect(recovered.getByTestId(NOTICE).textContent).toContain('Unsaved lineup changes restored');
  });
});

describe('the Discard button', () => {
  it('is a plain .btn, so the coarse-pointer floor reaches it through the class, never an inline style', async () => {
    const first = await mountPanel();
    await typeName(first, 1, 'Mori');
    first.unmount();
    const again = await mountPanel();

    const discard = discardButton(again);
    expect(discard.className.split(/\s+/)).toContain('btn');
    expect(discard.hasAttribute('style')).toBe(false);
    expect(discard.getAttribute('type')).toBe('button');
  });
});
