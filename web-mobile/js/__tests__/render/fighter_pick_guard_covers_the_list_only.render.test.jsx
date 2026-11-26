// The walk's finding S3: on the public team sheet, a competitor named a fighter
// from a bout's member list, then at once tapped M on ANOTHER bout, and the
// strike was lost (nothing shown, nothing sent); the second tap counted.
//
// A pick from a bout row's list closes the list, which dropped over that
// side's ippon buttons, so the second tap of a double tap on the option would
// land on the button the list uncovered (bc-flst). The pick therefore stamps
// the bout list's bounce ref. But the stamp swallowed a pointer tap ANYWHERE in
// the bout list for TAP_BOUNCE_MS, so a deliberate tap on another bout, which
// the list never covered, was swallowed too. The guard now covers only where
// the list was drawn: a bounce lands where the option was, inside the list.
// The box is kept relative to the picked name box, which scrolls with the
// sheet: in the walk the list opened upward over the sheet, and bringing bout 1
// into view scrolled its M under where the list had been on screen, so a box
// kept in screen coordinates still swallowed the tap. jsdom has no layout, so
// the list's box and the name box's place are given here, both moved by the
// sheet's scroll.
import React from 'react';
import { render, act, cleanup, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { makeFakeOutbox } from '../helpers/fake_outbox.js';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { answered } from '../helpers/team_members.js';

const STUBBED_GLOBALS = {
  isHikiwake: () => false,
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  isKikenDecision: () => false,
  isTextEntry: () => false,
  isInteractiveTarget: () => false,
  confirmDialog: vi.fn().mockResolvedValue(true),
  API: {},
  AdminLineupHelpers: { rosterFor: vi.fn().mockReturnValue([]) },
  compMatches: () => [],
  compMatchesForCompetition: () => [],
  Term: ({ children }) => <span>{children}</span>,
  GlossaryHint: ({ name }) => <span title={name} />,
};

let restoreGlobals;
let ScoreEditorModal;
let realRect;

// The list of bout 3 is drawn low on the sheet; bout 1's buttons sit above it.
const LIST_BOX = { top: 600, bottom: 800, left: 0, right: 390, width: 390, height: 200, x: 0, y: 600 };
// How far a test has scrolled the sheet down (content moves down on screen).
let scrolled = 0;
const moved = (b) => ({ ...b, top: b.top + scrolled, bottom: b.bottom + scrolled, y: b.y + scrolled, toJSON() {} });
// The name box the list opened upward from, just under the list.
const NAME_BOX = { top: 804, bottom: 844, left: 0, right: 390, width: 390, height: 40, x: 0, y: 804 };

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  await import('../../admin_scoring_modal.jsx');
  ScoreEditorModal = window.ScoreEditorModal;
  realRect = Element.prototype.getBoundingClientRect;
  Element.prototype.getBoundingClientRect = function rect() {
    if (this.classList && this.classList.contains('lineup-name__dropdown')) return moved(LIST_BOX);
    if (this.classList && this.classList.contains('lineup-name')) return moved(NAME_BOX);
    return realRect.call(this);
  };
});

afterAll(() => {
  Element.prototype.getBoundingClientRect = realRect;
  restoreGlobals();
});

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
    fetchLineupInForce: vi.fn(async () => null),
    putMatchLineup: vi.fn(async (_c, teamId, matchId, positions, _pw, memberIds) => ({ teamId, matchId, positions, memberIds })),
    renameTeamMember: vi.fn(async (_c, _t, id, name) => answered({ id, index: Number(id.slice(1)) }, { name })),
    addTeamMember: vi.fn(),
    recordScore: vi.fn(async () => ({ status: 'running' })),
    recordDaihyosen: vi.fn(),
    removeDaihyosen: vi.fn(),
    recordDecision: vi.fn(),
    hasPendingTerminalWrite: () => false,
  };
  Object.assign(window.API, makeFakeOutbox().api);
});
afterEach(() => { cleanup(); vi.useRealTimers(); scrolled = 0; });

const teamMatch = () => ({
  id: 'm1', compId: 'c1', compName: 'Teams', status: 'running', phase: 'pool', poolName: 'Pool 1', court: 'A',
  compKind: 'team', teamSize: 5, compFormat: 'mixed', teamMatchType: 'fixed',
  sideA: { id: 'team-A', name: 'Kodokan', number: 'T1' }, // Aka
  sideB: { id: 'team-B', name: 'Mumeishi', number: 'T2' }, // Shiro
  subResults: [],
});

async function mount() {
  await act(async () => {
    render(<ScoreEditorModal match={teamMatch()} onClose={vi.fn()} onSubmit={vi.fn().mockResolvedValue(undefined)} password="" selfReport />);
  });
  await act(async () => { await Promise.resolve(); });
}

const row = (n) => [...document.querySelectorAll('.team-sub-match')][n - 1];
const shiroInput = (n) => row(n).querySelector('.team-sub-match__side--shiro input');
const shiroBtn = (n, letter) => [...row(n).querySelectorAll('.team-sub-match__side--shiro button.ipt-btn')].find((b) => b.textContent === letter);
const marks = (n) => [...row(n).querySelectorAll('.tsm-center-pts--shiro button.editor-side__pt')].map((b) => b.textContent).filter((t) => t !== '·');
const tapAt = (el, clientX, clientY) => act(async () => { fireEvent.click(el, { detail: 1, clientX, clientY }); });

async function pickOnBout(n, name) {
  await act(async () => { fireEvent.focus(shiroInput(n)); });
  const option = [...row(n).querySelectorAll('.team-sub-match__side--shiro .pmf__option')].find((b) => b.textContent.includes(name));
  expect(option, `${name} is offered on bout ${n}`).toBeTruthy();
  await act(async () => { fireEvent.mouseDown(option); });
  await tapAt(option, 100, 700);
  await act(async () => { await Promise.resolve(); });
}

describe('S3: a pick from a bout list guards only where the list was drawn', () => {
  it('a tap on another bout straight after the pick scores, the name box still focused', async () => {
    await mount();
    await pickOnBout(3, 'Jun Oda');
    expect(shiroInput(3).value).toBe('Jun Oda');
    await tapAt(shiroBtn(1, 'M'), 100, 150);
    expect(marks(1)).toEqual(['M']);
  });

  it('a tap on another bout the sheet scrolled under where the list was drawn scores', async () => {
    await mount();
    await pickOnBout(3, 'Jun Oda');
    // Bringing bout 1 into view scrolls the sheet down by 500 px: its M is now
    // on screen at y 650, inside the list's old box on screen, but 500 px above
    // where the list was drawn in the sheet.
    scrolled = 500;
    await tapAt(shiroBtn(1, 'M'), 100, 650);
    expect(marks(1)).toEqual(['M']);
  });

  it('the bounce of the pick, landing inside the list, still scores nothing', async () => {
    await mount();
    await pickOnBout(3, 'Jun Oda');
    await tapAt(shiroBtn(3, 'D'), 100, 700);
    expect(marks(3)).toEqual([]);
  });
});
