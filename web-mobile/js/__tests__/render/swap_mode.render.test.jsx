// bc-drev FR10: the Swap competitors mode on Pools (preview) of a pools +
// knockout draw and Bracket (preview) of a knockout-only one. With the mode
// on, a tap on a competitor picks them, a tap on the picked one unpicks, a tap
// on another opens "If you swap them", and no tap opens a score sheet. The
// bounce of a double tap never picks and unpicks. The mode is an admin-only
// prop on the shared PoolsViewer and BracketTree: the public pages render as
// before, with no new tap targets.
import React from 'react';
import { render, act, screen, cleanup, fireEvent, within } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, afterEach, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { pointerTap, keyboardClick } from '../helpers/tap_events.js';

let restore;
let AdminPools;
let AdminBracket;
let PoolsViewer;
let BracketTree;
let now;

beforeAll(async () => {
  restore = installWindowStubs({
    ScoreEditorModal: ({ match }) => <div data-testid="editor-open">{match.id}</div>,
    CourtPicker: () => null,
    API: { swapDrawPlaces: vi.fn(), fetchSwapCandidates: vi.fn() },
  });
  // The bracket trees measure their cards with it; jsdom has none.
  if (typeof globalThis.ResizeObserver === 'undefined') {
    globalThis.ResizeObserver = class { observe() {} unobserve() {} disconnect() {} };
  }
  await import('../../bracket.jsx');
  ({ PoolsViewer } = await import('../../viewer_standings.jsx'));
  await import('../../admin_pools.jsx');
  await import('../../admin_competition_bracket.jsx');
  AdminPools = window.AdminPools;
  AdminBracket = window.AdminBracket;
  BracketTree = window.BracketTree;
  window.PoolsViewer = PoolsViewer;
});
afterAll(() => restore());

beforeEach(() => {
  // The tap guard times taps with Date.now: each test moves the clock itself.
  now = 1_000_000;
  vi.spyOn(Date, 'now').mockImplementation(() => now);
  Element.prototype.scrollIntoView = function scrollIntoView() {};
  // The marks of a pick are pinned in swap_marks.render.test.jsx: here the
  // read never answers, so every pick keeps its plain edge.
  window.API.fetchSwapCandidates = vi.fn(() => new Promise(() => {}));
});
afterEach(() => { cleanup(); vi.restoreAllMocks(); });

const later = () => { now += 1000; };

// --- a pools + knockout draw ----------------------------------------------

const elijah = { id: 'p1', name: 'Elijah Everdeen', dojo: 'Kenshi', number: 'IMS2' };
const arthur = { id: 'p2', name: 'Arthur Dick', dojo: 'Mumei', number: 'IMS3' };
const lena = { id: 'p3', name: 'Lena Park', dojo: 'Kenshi', number: 'IMS4' };
const omar = { id: 'p4', name: 'Omar Sy', dojo: 'Mumei', number: 'IMS5' };

const pools = [
  { poolName: 'Pool A', players: [elijah, arthur] },
  { poolName: 'Pool B', players: [lena, omar] },
];
const poolMatch = (id, a, b, status = 'scheduled') => ({ id, sideA: { id: a.id, name: a.name }, sideB: { id: b.id, name: b.name }, status, court: 'A' });
const poolMatches = [poolMatch('Pool A-0', elijah, arthur), poolMatch('Pool B-0', lena, omar)];
const mixed = { id: 'c1', name: 'Cup', format: 'mixed', kind: 'individual', status: 'draw-ready', numberPrefix: 'IMS' };

const dryRun = {
  applied: false, dryRun: true,
  moves: [
    { id: 'p1', name: 'Elijah Everdeen', to: { pool: 'Pool B', position: 1 } },
    { id: 'p3', name: 'Lena Park', to: { pool: 'Pool A', position: 1 } },
  ],
  numbers: [], seededWarning: [], bracketRebuilt: false, lineupsMoved: 0, lineupsRemoved: 0,
  separation: { before: [], after: [] },
};

async function mountPools({ c = mixed, matches = poolMatches, onEditScore = vi.fn() } = {}) {
  await act(async () => {
    render(<AdminPools c={c} pools={pools} poolMatches={matches} standings={{}} tweaks={{ showDojo: true }}
      onEditScore={onEditScore} password="pw" />);
  });
}

const targets = () => screen.queryAllByTestId('swap-target');
const targetNamed = (name) => targets().find((el) => el.textContent.includes(name));

async function turnOn() {
  await pointerTap(screen.getByTestId('swap-mode-on'));
  later();
}

describe('Swap competitors on Pools (preview)', () => {
  it('is offered on a draw-ready pools + knockout draw, and its banner says what taps do now', async () => {
    await mountPools();
    expect(targets()).toHaveLength(0);
    await turnOn();
    expect(screen.getByTestId('swap-mode').textContent).toContain(
      'Swap competitors: on. Tap two competitors to swap them. Taps do not open score sheets now.');
    expect(targets().map((el) => el.textContent)).toEqual([
      expect.stringContaining('Elijah Everdeen'), expect.stringContaining('Arthur Dick'),
      expect.stringContaining('Lena Park'), expect.stringContaining('Omar Sy'),
    ]);
  });

  it('is not offered once a result is recorded, on a league, or once the competition has started', async () => {
    await mountPools({ matches: [poolMatch('Pool A-0', elijah, arthur, 'running'), poolMatches[1]] });
    expect(screen.queryByTestId('swap-mode-on')).toBeNull();
    cleanup();
    await mountPools({ c: { ...mixed, format: 'league' } });
    expect(screen.queryByTestId('swap-mode-on')).toBeNull();
    cleanup();
    await mountPools({ c: { ...mixed, status: 'pools' } });
    expect(screen.queryByTestId('swap-mode-on')).toBeNull();
  });

  it('picks on the first tap, unpicks on a tap on the same name, and ignores the bounce', async () => {
    await mountPools();
    await turnOn();
    await pointerTap(targetNamed('Elijah Everdeen'));
    expect(targetNamed('Elijah Everdeen')).toHaveAttribute('aria-pressed', 'true');
    expect(screen.getByTestId('swap-mode-line').textContent)
      .toContain('Swap Elijah Everdeen (Pool A, IMS2) with… tap another competitor.');
    // The bounce of that tap lands on the same name a moment later.
    now += 30;
    await pointerTap(targetNamed('Elijah Everdeen'));
    expect(targetNamed('Elijah Everdeen')).toHaveAttribute('aria-pressed', 'true');
    // A deliberate second tap unpicks.
    later();
    await pointerTap(targetNamed('Elijah Everdeen'));
    expect(targetNamed('Elijah Everdeen')).toHaveAttribute('aria-pressed', 'false');
    expect(screen.getByTestId('swap-mode-line').textContent).toBe('');
  });

  it('a tap on another competitor opens the check with both places', async () => {
    window.API.swapDrawPlaces = vi.fn().mockResolvedValue(dryRun);
    await mountPools();
    await turnOn();
    await pointerTap(targetNamed('Elijah Everdeen'));
    later();
    await pointerTap(targetNamed('Lena Park'));
    expect(screen.getByTestId('draw-swap-check')).toBeInTheDocument();
    expect(window.API.swapDrawPlaces).toHaveBeenCalledWith('c1', {
      a: { id: 'p1', place: { pool: 'Pool A', position: 1 } },
      b: { id: 'p3', place: { pool: 'Pool B', position: 1 } },
      dryRun: true,
    }, 'pw');
  });

  it('taps on matches open nothing while the mode is on', async () => {
    await mountPools();
    const row = () => document.querySelector('.pool-match-numbered-row');
    // Off: a match row is a button and opens the score sheet.
    expect(row().tagName).toBe('BUTTON');
    await keyboardClick(row());
    expect(screen.getByTestId('editor-open').textContent).toBe('Pool A-0');
    cleanup();
    await mountPools();
    await turnOn();
    expect(row().tagName).toBe('DIV');
    fireEvent.click(row(), { detail: 1 });
    expect(screen.queryByTestId('editor-open')).toBeNull();
  });

  it('stays on with nothing picked after a swap, and Done turns it off', async () => {
    window.API.swapDrawPlaces = vi.fn()
      .mockResolvedValueOnce(dryRun)
      .mockResolvedValueOnce({ ...dryRun, applied: true, dryRun: undefined });
    await mountPools();
    await turnOn();
    await pointerTap(targetNamed('Elijah Everdeen'));
    later();
    await pointerTap(targetNamed('Lena Park'));
    later();
    await keyboardClick(screen.getByTestId('swap-them'));
    expect(screen.queryByTestId('draw-swap-check')).toBeNull();
    expect(screen.getByTestId('swap-mode')).toBeInTheDocument();
    expect(screen.getByTestId('swap-mode-line').textContent).toBe('Swapped Elijah Everdeen and Lena Park.');
    expect(targets().every((el) => el.getAttribute('aria-pressed') === 'false')).toBe(true);
    later();
    await pointerTap(screen.getByTestId('swap-mode-done'));
    expect(screen.queryByTestId('swap-mode')).toBeNull();
    expect(targets()).toHaveLength(0);
  });

  it('the bounce of the tap that turns it on does not land on Done', async () => {
    await mountPools();
    await pointerTap(screen.getByTestId('swap-mode-on'));
    now += 30;
    await pointerTap(screen.getByTestId('swap-mode-done'));
    expect(screen.getByTestId('swap-mode')).toBeInTheDocument();
  });

  it('Escape turns it off, but only closes the check while the check is open', async () => {
    window.API.swapDrawPlaces = vi.fn().mockResolvedValue(dryRun);
    await mountPools();
    await turnOn();
    await pointerTap(targetNamed('Elijah Everdeen'));
    later();
    await pointerTap(targetNamed('Lena Park'));
    await act(async () => { fireEvent.keyDown(window, { key: 'Escape' }); });
    expect(screen.queryByTestId('draw-swap-check')).toBeNull();
    expect(screen.getByTestId('swap-mode')).toBeInTheDocument();
    await act(async () => { fireEvent.keyDown(window, { key: 'Escape' }); });
    expect(screen.queryByTestId('swap-mode')).toBeNull();
  });
});

// --- a knockout-only draw -------------------------------------------------

const side = (p) => ({ id: p.id, name: p.name, dojo: p.dojo, number: p.number });
const knockoutBracket = {
  rounds: [
    [
      { id: 'm-r0-0', matchNumber: 1, court: 'A', status: 'scheduled', sideA: side(elijah), sideB: side(arthur) },
      { id: 'm-r0-1', matchNumber: 2, court: 'A', status: 'scheduled', sideA: side(lena), sideB: side(omar) },
    ],
    [
      { id: 'm-r1-0', matchNumber: 3, court: 'A', status: 'scheduled', sideA: { id: '', name: 'Winner of r0-m0' }, sideB: { id: '', name: 'Winner of r0-m1' } },
    ],
  ],
};
const knockout = { ...mixed, format: 'knockout' };

async function mountBracket({ c = knockout, bracket = knockoutBracket } = {}) {
  await act(async () => {
    render(<AdminBracket c={c} t={{ courts: ['A'] }} bracket={bracket} onMoveCourt={vi.fn()} onEditScore={vi.fn()}
      tweaks={{ showDojo: true, cardVariant: 1 }} password="pw" />);
  });
}

describe('Swap competitors on Bracket (preview)', () => {
  it('turns each drawn competitor into its own tap target and a card into a group', async () => {
    window.API.swapDrawPlaces = vi.fn().mockResolvedValue(dryRun);
    await mountBracket();
    const card = () => document.querySelector('[data-match-id="m-r0-0"]');
    expect(card().tagName).toBe('BUTTON');
    await turnOn();
    expect(card().tagName).toBe('DIV');
    expect(card()).toHaveAttribute('role', 'group');
    // The four first-round competitors, not the "Winner of" places.
    expect(targets()).toHaveLength(4);
    await pointerTap(targetNamed('Elijah Everdeen'));
    expect(screen.getByTestId('swap-mode-line').textContent)
      .toContain('Swap Elijah Everdeen (first-round Match 1, IMS2) with… tap another competitor.');
    later();
    await pointerTap(targetNamed('Omar Sy'));
    expect(window.API.swapDrawPlaces).toHaveBeenCalledWith('c1', {
      a: { id: 'p1', place: { slot: 0 } }, b: { id: 'p4', place: { slot: 3 } }, dryRun: true,
    }, 'pw');
  });

  it('reaches the effective-round renderer too (a bracket with display metadata)', async () => {
    const meta = {
      rounds: knockoutBracket.rounds.map((r, ri) => r.map((m) => ({ ...m, displayRound: ri === 0 ? 2 : 1 }))),
    };
    await mountBracket({ bracket: meta });
    await turnOn();
    expect(document.querySelector('[data-match-id="m-r0-1"]').tagName).toBe('DIV');
    expect(targets()).toHaveLength(4);
    await pointerTap(targetNamed('Lena Park'));
    expect(targetNamed('Lena Park')).toHaveAttribute('aria-pressed', 'true');
  });

  it('a tap on a card opens no scoring panel while the mode is on', async () => {
    await mountBracket();
    await turnOn();
    fireEvent.click(document.querySelector('[data-match-id="m-r0-1"]'), { detail: 1 });
    expect(screen.queryByText('Pick a match')).toBeNull();
    expect(document.querySelector('.running-panel')).toBeNull();
  });

  it('is offered on a knockout whose config names no format, as the server reads it', async () => {
    const { format: _format, ...noFormat } = knockout;
    await mountBracket({ c: noFormat });
    await turnOn();
    expect(targets()).toHaveLength(4);
  });

  it('is not offered on the bracket of a pools + knockout draw, whose places are not competitors', async () => {
    await mountBracket({ c: mixed });
    expect(screen.queryByTestId('swap-mode-on')).toBeNull();
  });
});

// --- the public pages -----------------------------------------------------

describe('the shared viewers without the admin prop', () => {
  it('PoolsViewer draws no pick buttons and its matches still open', async () => {
    const onMatchClick = vi.fn();
    render(<PoolsViewer pools={pools} standings={{}} poolMatches={poolMatches} competition={mixed}
      tweaks={{ showDojo: true }} highlightPlayers={[]} onMatchClick={onMatchClick} />);
    expect(targets()).toHaveLength(0);
    const table = document.querySelector('.pool__table');
    expect(within(table).queryAllByRole('button')).toHaveLength(0);
  });

  it('BracketTree draws every card as a button and no side as one', async () => {
    const onMatchClick = vi.fn();
    render(<BracketTree rounds={knockoutBracket.rounds} variant={1} showDojo onMatchClick={onMatchClick} />);
    expect(targets()).toHaveLength(0);
    const cards = document.querySelectorAll('.bc-match');
    expect(cards.length).toBeGreaterThan(0);
    cards.forEach((el) => expect(el.tagName).toBe('BUTTON'));
    expect(document.querySelectorAll('.bc-side').length).toBeGreaterThan(0);
    document.querySelectorAll('.bc-side').forEach((el) => expect(el.tagName).toBe('DIV'));
    fireEvent.click(document.querySelector('[data-match-id="m-r0-0"]'));
    expect(onMatchClick).toHaveBeenCalled();
  });
});
