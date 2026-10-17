// bc-dtip: a double tap on an ippon button records ONE ippon.
//
// A bouncing thumb delivers two clicks 16-44ms apart, and each landed on a
// fresh render, so M M was recorded and the bout read as won 2-0. Two real
// ippon calls can never arrive that close (the shushin stops play for each),
// so a repeat POINTER tap on the same side within TAP_BOUNCE_MS is ignored
// (tap_guard.jsx). The same holds for the foul "+", whose second foul awards
// an H to the opponent. Keyboard input (detail 0) is never swallowed.
//
// Pointer taps pass detail: 1 (helpers/tap_events.js): fireEvent.click's
// default of 0 is what the guard exempts, so the tests could never fail.

import React from 'react';
import { render, act, cleanup } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { TAP_BOUNCE_MS } from '../../tap_guard.jsx';
import { pointerTap, keyboardClick } from '../helpers/tap_events.js';
import { makeFakeOutbox } from '../helpers/fake_outbox.js';

const STUBBED_GLOBALS = {
  isHikiwake: () => false,
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  isKikenDecision: () => false,
  isTextEntry: () => false,
  isInteractiveTarget: () => false,
  confirmDialog: vi.fn().mockResolvedValue(true),
  API: {
    fetchCompetitionDetails: vi.fn().mockResolvedValue(null),
    recordScore: vi.fn().mockResolvedValue({ applied: true }),
    recordDaihyosen: vi.fn(),
    removeDaihyosen: vi.fn(),
    putMatchLineup: vi.fn(),
    recordDecision: vi.fn(),
  },
  AdminLineupHelpers: { rosterFor: vi.fn().mockReturnValue([]) },
  compMatches: () => [],
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

beforeEach(() => {
  window.API.recordScore.mockClear();
  vi.useFakeTimers();
});
afterEach(() => { cleanup(); vi.useRealTimers(); });

const wait = (ms) => act(async () => { vi.advanceTimersByTime(ms); });

async function mount(match) {
  if (match.compFormat === 'knockout') {
    window.API.fetchCompetitionDetails.mockResolvedValue({
      id: 'comp1', config: { format: 'knockout', teamMatchType: 'kachinuki', naginata: false, players: [] },
    });
  } else {
    window.API.fetchCompetitionDetails.mockResolvedValue(null);
  }
  await act(async () => {
    render(<ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={(p) => window.API.recordScore('comp1', match.id, p, '', match)} password="" />);
  });
}


describe('bc-dtip: individual editor', () => {
  // bc-evts stage 3: a running individual match is scored by taps handed to
  // the outbox (API.recordTap); the board shows them at once.
  beforeEach(() => { Object.assign(window.API, makeFakeOutbox().api); });
  const match = () => ({
    id: 'm-ind', compId: 'comp1', status: 'running', phase: 'pool', poolName: 'Pool 1', court: 'A',
    sideA: { id: 'p1', name: 'Yamada' },
    sideB: { id: 'p2', name: 'Tanaka' },
  });
  // side: "aka" (a, ipponsA) or "shiro" (b, ipponsB)
  const btn = (side, letter) => [...document.querySelectorAll(`.sb-side--${side} button.ipt-btn`)].find((b) => b.textContent === letter);
  const foulPlus = (side) => document.querySelector(`[data-testid="scoring-modal-hansoku-${side}"] .foul-counter__btn--inc`);
  const foulCount = (side) => document.querySelector(`[data-testid="scoring-modal-hansoku-${side}"] .foul-counter__num`).textContent;
  // The letters a side's slots show, in slot order.
  const board = (side) => [...document.querySelectorAll('button')]
    .map((b) => (b.getAttribute('aria-label') || '').match(new RegExp(`^${side === 'aka' ? 'Aka' : 'Shiro'} slot \\d: remove (.+)$`)))
    .filter(Boolean).map((x) => x[1]);
  const taps = () => window.API.recordTap.mock.calls.map((c) => c[2]);

  it('a double tap on one ippon button records one ippon and one tap', async () => {
    await mount(match());
    await pointerTap(btn('aka', 'M'));
    await pointerTap(btn('aka', 'M'));
    expect(taps()).toEqual([{ kind: 'ippon.add', pos: 0, side: 'a', letter: 'M' }]);
    expect(board('aka')).toEqual(['M']);
    expect(window.API.recordScore).not.toHaveBeenCalled();
  });

  it('a deliberate second tap after the window records the second ippon', async () => {
    await mount(match());
    await pointerTap(btn('aka', 'M'));
    await wait(TAP_BOUNCE_MS + 50);
    await pointerTap(btn('aka', 'M'));
    expect(board('aka')).toEqual(['M', 'M']);
  });

  it('a bounce onto the same side\'s neighbouring letter is ignored too', async () => {
    await mount(match());
    await pointerTap(btn('aka', 'M'));
    await pointerTap(btn('aka', 'K'));
    expect(board('aka')).toEqual(['M']);
  });

  it('the other side\'s button is never refused', async () => {
    await mount(match());
    await pointerTap(btn('aka', 'M'));
    await pointerTap(btn('shiro', 'M'));
    expect(board('aka')).toEqual(['M']);
    expect(board('shiro')).toEqual(['M']);
  });

  it('a keyboard-synthesized click (detail 0) is never swallowed', async () => {
    await mount(match());
    await pointerTap(btn('aka', 'M'));
    await keyboardClick(btn('aka', 'K'));
    // Aka's slots are mirrored on screen, so compare the set.
    expect(board('aka').sort()).toEqual(['K', 'M']);
  });

  it('taking the mark back off lets the right letter in at once', async () => {
    await mount(match());
    await pointerTap(btn('aka', 'M'));
    const mSlot = [...document.querySelectorAll('button')].find((b) => /Aka slot \d: remove M/.test(b.getAttribute('aria-label') || ''));
    expect(mSlot).toBeTruthy();
    await pointerTap(mSlot);
    await pointerTap(btn('aka', 'K'));
    expect(board('aka')).toEqual(['K']);
  });

  it('a double tap on the foul "+" records one foul and awards no H', async () => {
    await mount(match());
    await pointerTap(foulPlus('shiro'));
    await pointerTap(foulPlus('shiro'));
    expect(foulCount('shiro')).toBe('1');
    expect(board('aka')).toEqual([]);
    expect(taps()).toEqual([{ kind: 'foul.add', pos: 0, side: 'b' }]);
  });
});

describe('bc-dtip: team bout rows', () => {
  // bc-evts stage 5: a running team match is scored by taps too; each test gets
  // a fresh fake of the outbox, so no tap of an earlier test is on its board.
  beforeEach(() => { Object.assign(window.API, makeFakeOutbox().api); });
  const teamMatch = () => ({
    id: 'm-team', compId: 'comp1', status: 'running', phase: 'pool', poolName: 'Pool 1', court: 'A',
    compKind: 'team', teamSize: 3,
    sideA: { id: 'team-kyoto', name: 'Kyoto' },
    sideB: { id: 'team-osaka', name: 'Osaka' },
  });
  const kachinukiMatch = () => ({
    id: 'm-kachi', compId: 'comp1', status: 'running', phase: 'bracket',
    round: 'Semi-final', matchNumber: 1, court: 'A',
    compKind: 'team', teamSize: 5, compFormat: 'knockout', teamMatchType: 'kachinuki',
    sideA: { id: 'team-A', name: 'Team A' },
    sideB: { id: 'team-B', name: 'Team B' },
    subResults: [
      { position: 1, sideA: 'A1', sideB: 'B1', ipponsA: [], ipponsB: ['M', 'K'], winner: 'B1' },
      { position: 2, sideA: 'A1', sideB: 'B1', ipponsA: [], ipponsB: [] },
    ],
  });
  const liveRow = () => [...document.querySelectorAll('.team-sub-match')].find((r) => !r.classList.contains('team-sub-match--readonly'));
  const btn = (side, letter) => [...liveRow().querySelectorAll(`.team-sub-match__side--${side} button.ipt-btn`)].find((b) => b.textContent === letter);
  const marks = (side) => [...liveRow().querySelectorAll(`.tsm-center-pts--${side} button.editor-side__pt`)].map((b) => b.textContent).filter((t) => t !== '·');

  it('a double tap on a team bout\'s ippon button records one ippon', async () => {
    await mount(teamMatch());
    await pointerTap(btn('shiro', 'K'));
    await pointerTap(btn('shiro', 'K'));
    expect(marks('shiro')).toEqual(['K']);
    await wait(TAP_BOUNCE_MS + 50);
    await pointerTap(btn('shiro', 'K'));
    expect(marks('shiro')).toEqual(['K', 'K']);
  });

  it('a double tap on a kachinuki bout\'s ippon button records one ippon', async () => {
    await mount(kachinukiMatch());
    await pointerTap(btn('aka', 'M'));
    await pointerTap(btn('aka', 'M'));
    expect(marks('aka')).toEqual(['M']);
  });

  it('a double tap on a team bout\'s foul "+" records one foul and awards no H', async () => {
    await mount(teamMatch());
    const plus = liveRow().querySelector('[data-testid="scoring-modal-hansoku-shiro"] .tsm-fouls__btn[aria-label^="Add"]');
    await pointerTap(plus);
    await pointerTap(plus);
    expect(liveRow().querySelector('[data-testid="scoring-modal-hansoku-shiro"] .tsm-fouls__count').textContent).toBe('1');
    expect(marks('aka')).toEqual([]);
  });
});
