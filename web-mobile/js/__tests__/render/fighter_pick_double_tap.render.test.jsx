// bc-flst: a double tap on a fighter in a bout row's list scored the ippon
// the list had covered.
//
// The list drops over this side's ippon buttons. A pick closes it, so the
// second tap of a double tap landed on the button underneath and scored it.
// A pick now stamps the bout list's bounce ref (the same one opening a fought
// bout stamps, bc-kbrw), so for TAP_BOUNCE_MS after a pick a pointer click
// anywhere in the bout list is swallowed. jsdom has no layout, so this pins
// the guard, not the geometry. Pointer taps pass detail: 1
// (helpers/tap_events.js); a keyboard click (detail 0) is never swallowed.
import React from 'react';
import { render, act, cleanup, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { TAP_BOUNCE_MS } from '../../tap_guard.jsx';
import { pointerTap, keyboardClick } from '../helpers/tap_events.js';

const STUBBED_GLOBALS = {
  isHikiwake: () => false,
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  isKikenDecision: () => false,
  isTextEntry: () => false,
  isInteractiveTarget: () => false,
  confirmDialog: vi.fn().mockResolvedValue(true),
  resolveRoundIndex: () => 0,
  API: {},
  AdminLineupHelpers: { rosterFor: vi.fn().mockReturnValue([]) },
  compMatches: () => [],
  compMatchesForCompetition: () => [],
  Term: ({ children }) => <span>{children}</span>,
  GlossaryHint: ({ name }) => <span title={name} />,
};

let restoreGlobals;
let ScoreEditorModal;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  await import('../../admin_scoring_modal.jsx');
  ScoreEditorModal = window.ScoreEditorModal;
});

afterAll(() => restoreGlobals());

const named = (p, names) => names.map((name, i) => ({ id: `${p}${i + 1}`, index: i + 1, name }));
const SQUADS = {
  'team-A': named('a', ['Ren Abe', 'Kai Mori', 'Yui Sato', 'Rin Ota', 'Sho Ueda']),
  'team-B': named('b', ['Mei Ito', 'Jun Oda', 'Aki Kato', 'Sora Endo', 'Taro Mori']),
};

beforeEach(() => {
  vi.useFakeTimers();
  window.API = {
    fetchCompetitionDetails: vi.fn().mockResolvedValue({ id: 'c1', config: { format: 'mixed', players: [] } }),
    fetchSquads: vi.fn().mockResolvedValue(SQUADS),
    fetchMatchLineup: vi.fn(async () => null),
    fetchTeamLineup: vi.fn(async () => null),
    putMatchLineup: vi.fn(async (_c, teamId, matchId, positions, _pw, memberIds) => ({ teamId, matchId, positions, memberIds })),
    renameTeamMember: vi.fn(async () => true),
    addTeamMember: vi.fn(),
    recordScore: vi.fn(async () => ({ status: 'running' })),
    recordDaihyosen: vi.fn(),
    removeDaihyosen: vi.fn(),
    recordDecision: vi.fn(),
    hasPendingTerminalWrite: () => false,
    notePendingEdit: () => () => {},
  };
});
afterEach(() => { cleanup(); vi.useRealTimers(); });

const wait = (ms) => act(async () => { vi.advanceTimersByTime(ms); });

const teamMatch = (teamMatchType, subResults = []) => ({
  id: 'm1', compId: 'c1', compName: 'Teams', status: 'running', phase: 'pool', poolName: 'Pool 1', court: 'A',
  compKind: 'team', teamSize: 5, compFormat: 'mixed', teamMatchType,
  sideA: { id: 'team-A', name: 'Kodokan', number: 'T1' }, // Aka
  sideB: { id: 'team-B', name: 'Mumeishi', number: 'T2' }, // Shiro
  subResults,
});

// A kachinuki encounter one bout in: Shiro's fighter for bout 2 is not yet
// named, and a later row's pick rides the bout itself (pickManual).
const kachinukiLaterBout = () => teamMatch('kachinuki', [
  { position: 1, sideA: 'Ren Abe', sideAMemberId: 'a1', sideB: 'Mei Ito', sideBMemberId: 'b1', ipponsA: ['M', 'M'], ipponsB: [], winner: 'Ren Abe' },
  { position: 2, sideA: 'Ren Abe', sideAMemberId: 'a1', sideB: '', ipponsA: [], ipponsB: [] },
]);

async function mount(match) {
  await act(async () => {
    render(<ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={vi.fn().mockResolvedValue(undefined)} password="secret" />);
  });
  // Let the squads fetch land so the row's list carries the team members.
  await act(async () => { await Promise.resolve(); });
}

// The bout being scored: the row whose Shiro side carries an editable picker.
const liveRow = () => [...document.querySelectorAll('.team-sub-match')]
  .filter((r) => !r.classList.contains('team-sub-match--readonly') && r.querySelector('.team-sub-match__side--shiro input'))
  .pop();
const shiroInput = () => liveRow().querySelector('.team-sub-match__side--shiro input');
const shiroBtn = (letter) => [...liveRow().querySelectorAll('.team-sub-match__side--shiro button.ipt-btn')].find((b) => b.textContent === letter);
const marks = (side) => [...liveRow().querySelectorAll(`.tsm-center-pts--${side} button.editor-side__pt`)].map((b) => b.textContent).filter((t) => t !== '·');

// The first tap of a double tap on an option: its mousedown, then its click.
async function pickShiro(name) {
  await act(async () => { fireEvent.focus(shiroInput()); });
  const option = [...liveRow().querySelectorAll('.team-sub-match__side--shiro .pmf__option')].find((b) => b.textContent.includes(name));
  expect(option, `${name} is offered`).toBeTruthy();
  await act(async () => { fireEvent.mouseDown(option); });
  await pointerTap(option);
  await act(async () => { await Promise.resolve(); });
}

describe.each([
  ['a kachinuki later bout', kachinukiLaterBout],
  ['a regular team match row', () => teamMatch('fixed')],
])('bc-flst: a double tap on a fighter in %s', (_label, makeMatch) => {
  it('picks the fighter and scores nothing under the list', async () => {
    await mount(makeMatch());
    await pickShiro('Jun Oda');
    expect(shiroInput().value).toBe('Jun Oda');

    // The second tap of the double tap lands on Shiro's D, which the list covered.
    await pointerTap(shiroBtn('D'));
    expect(marks('shiro')).toEqual([]);
  });

  it('a deliberate tap after the window scores the ippon', async () => {
    await mount(makeMatch());
    await pickShiro('Jun Oda');
    await wait(TAP_BOUNCE_MS + 50);
    await pointerTap(shiroBtn('D'));
    expect(marks('shiro')).toEqual(['D']);
  });

  it('a keyboard-synthesized click (detail 0) is never swallowed', async () => {
    await mount(makeMatch());
    await pickShiro('Jun Oda');
    await keyboardClick(shiroBtn('D'));
    expect(marks('shiro')).toEqual(['D']);
  });

  // A typed name is committed by the tap that leaves the box (click outside).
  // That tap lands on a visible ippon button, not on one a closing list
  // uncovered, so it scores.
  it('a typed name committed by tapping an ippon button scores that ippon', async () => {
    await mount(makeMatch());
    await act(async () => { fireEvent.focus(shiroInput()); });
    await act(async () => { fireEvent.change(shiroInput(), { target: { value: 'Jun Oda' } }); });
    await act(async () => { fireEvent.mouseDown(shiroBtn('D')); });
    expect(shiroInput().value).toBe('Jun Oda');
    await pointerTap(shiroBtn('D'));
    expect(marks('shiro')).toEqual(['D']);
  });
});
