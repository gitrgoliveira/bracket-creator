// The at-court lineup panel is a layer over the page, and takes what the app's other
// layers take: the bounce of the tap that opened it lands on nothing (a second click
// of a double tap used to land on whichever control of the panel lay under the
// finger), Escape closes it unless a write is out, focus goes into it and back to the
// control that opened it, and it is a dialog with a name. One Escape closes one layer:
// an open name list, or an open rename box, takes it first.

import React from 'react';
import { render, act, fireEvent, within, cleanup } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { TAP_BOUNCE_MS } from '../../tap_guard.jsx';
import { pointerTap, keyboardClick } from '../helpers/tap_events.js';
import { answered } from '../helpers/team_members.js';

const MEMBERS = [
  { id: 'mem-1', index: 1, name: 'Aoki' },
  { id: 'mem-2', index: 2, name: 'Sato' },
  { id: 'mem-3', index: 3, name: 'Ito' },
  { id: 'mem-4', index: 4, name: 'Mori' },
];
const NAMES = { positions: { 1: 'Aoki', 2: 'Sato', 3: 'Ito' }, memberIds: { 1: 'mem-1', 2: 'mem-2', 3: 'mem-3' } };
const A = { id: 'team-a', name: 'Team A' };
const B = { id: 'team-b', name: 'Team B' };
const COMP = { id: 'comp-1', name: 'Team Event', kind: 'team', format: 'mixed', status: 'active', teamSize: 3, players: [A, B] };
const MATCH = { id: 'm-r1-0', compId: 'comp-1', status: 'scheduled', phase: 'bracket', sideA: A, sideB: B };

let MatchLineupPanel;
let saved;
let api;

beforeEach(async () => {
  sessionStorage.clear();
  saved = { API: window.API, confirmDialog: window.confirmDialog, compMatches: window.compMatches };
  window.confirmDialog = vi.fn().mockResolvedValue(true);
  window.compMatches = () => [];
  api = {
    fetchLineupInForce: vi.fn().mockImplementation((_c, teamId) => Promise.resolve({
      teamId, competitionId: 'comp-1', ...NAMES, matchId: 'm-r1-0', sourceMatchId: 'm-r1-0', saved: true,
    })),
    fetchSquads: vi.fn().mockResolvedValue({ 'team-a': MEMBERS, 'team-b': MEMBERS }),
    putMatchLineup: vi.fn().mockImplementation((_c, _t, _m, positions, _pw, memberIds) => Promise.resolve({ positions, memberIds })),
    // The server answers a member write with the member it holds, stamped.
    addTeamMember: vi.fn().mockImplementation((_c, _t, name) => Promise.resolve(answered({ id: 'mem-minted', index: 6 }, { name }))),
    renameTeamMember: vi.fn().mockImplementation((_c, _t, id, name) => Promise.resolve(answered({ id, index: 1 }, { name }))),
    queuedLineupSave: vi.fn().mockReturnValue(null),
  };
  window.API = api;
  await import('../../admin_lineup.jsx');
  ({ MatchLineupPanel } = await import('../../admin_schedule_lineup.jsx'));
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  for (const [key, value] of Object.entries(saved)) {
    if (value === undefined) delete window[key]; else window[key] = value;
  }
});

const panelFor = (onClose, extra = {}) => (
  <MatchLineupPanel match={MATCH} tournament={{ competitions: [COMP] }} password="pw" showToast={vi.fn()} onClose={onClose} {...extra} />
);

async function mount(extra) {
  const onClose = vi.fn();
  let utils;
  await act(async () => { utils = render(panelFor(onClose, extra)); });
  return { ...utils, onClose };
}

const settle = () => act(async () => { await new Promise((resolve) => setTimeout(resolve, 5)); });
const escape = (target = document.body) => act(async () => { fireEvent.keyDown(target, { key: 'Escape' }); });
const closeButton = (utils) => utils.getByRole('button', { name: '✕ Close' });
const sideA = (utils) => within(utils.getByTestId('match-lineup-side-team-a'));

describe('the panel as a dialog', () => {
  it('is a modal dialog named for what it is', async () => {
    const utils = await mount();
    const dialog = utils.getByRole('dialog', { name: 'Lineup for this match' });
    expect(dialog.getAttribute('aria-modal')).toBe('true');
    expect(dialog.contains(closeButton(utils))).toBe(true);
  });

  it('is not a layer when it sits in the page (the inline variant): no dialog, and Escape leaves it', async () => {
    const utils = await mount({ variant: 'inline' });
    expect(utils.queryByRole('dialog')).toBeNull();
    await escape();
    expect(utils.onClose).not.toHaveBeenCalled();
  });
});

describe('Escape', () => {
  it('closes the panel', async () => {
    const utils = await mount();
    await escape();
    expect(utils.onClose).toHaveBeenCalledTimes(1);
  });

  it('closes only an open name list first, and the panel on the next one', async () => {
    const utils = await mount();
    await settle();
    const input = sideA(utils).getByLabelText('1 player');
    await act(async () => { fireEvent.focus(input); });
    expect(sideA(utils).getByText('Mori'), 'the list is open, with the member nobody holds').toBeTruthy();

    await escape(input);
    expect(utils.onClose, 'the list took it').not.toHaveBeenCalled();
    expect(sideA(utils).queryByText('Mori')).toBeNull();

    await escape(input);
    expect(utils.onClose).toHaveBeenCalledTimes(1);
  });

  it('cancels an open rename box only, and closes the panel on the next one', async () => {
    const utils = await mount();
    await settle();
    await act(async () => { fireEvent.click(sideA(utils).getByRole('button', { name: 'Rename 1 player' })); });
    const box = sideA(utils).getByRole('textbox', { name: 'Rename 1 player' });

    await escape(box);
    expect(sideA(utils).queryByRole('textbox', { name: 'Rename 1 player' }), 'the rename box is gone').toBeNull();
    expect(utils.onClose, 'the rename took it').not.toHaveBeenCalled();

    await escape();
    expect(utils.onClose).toHaveBeenCalledTimes(1);
  });

  it('does not close the panel while a Save is out, and does once it has landed', async () => {
    let answer;
    api.putMatchLineup.mockImplementation(() => new Promise((resolve) => { answer = resolve; }));
    const utils = await mount();
    await settle();
    const input = sideA(utils).getByLabelText('1 player');
    await act(async () => {
      fireEvent.focus(input);
      fireEvent.change(input, { target: { value: 'Mori' } });
      fireEvent.keyDown(input, { key: 'Enter' });
    });
    await act(async () => { fireEvent.click(sideA(utils).getByRole('button', { name: /^Save lineup$/ })); });
    await settle();
    expect(api.putMatchLineup, 'the write is out').toHaveBeenCalledTimes(1);

    await escape();
    expect(utils.onClose).not.toHaveBeenCalled();

    await act(async () => { answer({ positions: { 1: 'Mori', 2: 'Sato', 3: 'Ito' }, memberIds: { 1: 'mem-4', 2: 'mem-2', 3: 'mem-3' } }); });
    await settle();
    await escape();
    expect(utils.onClose).toHaveBeenCalledTimes(1);
  });
});

describe('the tap that opened the panel', () => {
  const wait = (ms) => act(async () => { await vi.advanceTimersByTimeAsync(ms); });

  beforeEach(() => { vi.useFakeTimers(); });

  it('has its bounce swallowed: a second click that lands on a control of the panel does nothing', async () => {
    const utils = await mount();
    await wait(30);
    await pointerTap(closeButton(utils));
    expect(utils.onClose).not.toHaveBeenCalled();
  });

  it('is followed by a deliberate tap that works after the window', async () => {
    const utils = await mount();
    await wait(TAP_BOUNCE_MS + 50);
    await pointerTap(closeButton(utils));
    expect(utils.onClose).toHaveBeenCalledTimes(1);
  });

  it('is followed by a keyboard activation that works at once (detail 0 is never a bounce)', async () => {
    const utils = await mount();
    await wait(30);
    await keyboardClick(closeButton(utils));
    expect(utils.onClose).toHaveBeenCalledTimes(1);
  });
});

describe('focus', () => {
  // The host that opens the panel from a button, as the Scores page and the court
  // console do, and closes it by unmounting it.
  function Host() {
    const [open, setOpen] = React.useState(false);
    return (
      <div>
        <button type="button" onClick={() => setOpen(true)}>Open lineup</button>
        {open && (
          <MatchLineupPanel match={MATCH} tournament={{ competitions: [COMP] }} password="pw" showToast={vi.fn()} onClose={() => setOpen(false)} />
        )}
      </div>
    );
  }

  it('goes into the panel as it opens and back to the control that opened it, without scrolling', async () => {
    const focus = vi.spyOn(HTMLElement.prototype, 'focus');
    try {
      let utils;
      await act(async () => { utils = render(<Host />); });
      const opener = utils.getByRole('button', { name: 'Open lineup' });
      opener.focus();
      await act(async () => { opener.click(); });
      await settle();
      const dialog = utils.getByRole('dialog', { name: 'Lineup for this match' });
      expect(dialog.contains(document.activeElement), 'focus is in the panel').toBe(true);
      expect(document.activeElement).toBe(closeButton(utils));

      await act(async () => { fireEvent.click(closeButton(utils), { detail: 0 }); });
      await settle();
      expect(utils.queryByRole('dialog')).toBeNull();
      expect(document.activeElement, 'back on the control that opened it').toBe(utils.getByRole('button', { name: 'Open lineup' }));
      const moves = focus.mock.calls.slice(1).map(([options]) => options);
      expect(moves, 'both moves leave the page where it is').toEqual([{ preventScroll: true }, { preventScroll: true }]);
    } finally {
      focus.mockRestore();
    }
  });

  it('is not given back to a control that is gone', async () => {
    let utils;
    await act(async () => { utils = render(<Host />); });
    const opener = utils.getByRole('button', { name: 'Open lineup' });
    opener.focus();
    await act(async () => { opener.click(); });
    await settle();

    // The opener leaves the page while the panel is open (the match it belongs to was started).
    opener.remove();
    await act(async () => { fireEvent.click(closeButton(utils), { detail: 0 }); });
    await settle();

    expect(document.activeElement).toBe(document.body);
  });
});
