// bc-sdtp: a double tap on the court console's Start match starts ONE match.
//
// The first start can land inside the bounce window of its tap; the Up next
// card then shows the next match, whose Start match is enabled, and the bounce
// used to start it, sending the match just started back to the queue. Every
// Start match on the console now shares one keyless bounce guard (acceptTap,
// tap_guard.jsx), so the bounce lands on nothing, whichever Start it hits. A
// deliberate tap after the window, and a keyboard activation, still start.
//
// Pointer taps pass detail: 1 (helpers/tap_events.js), or the guard exempts
// them and these tests could never go red.

import React from 'react';
import { render, act, screen, cleanup } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { TAP_BOUNCE_MS } from '../../tap_guard.jsx';
import { pointerTap, keyboardClick } from '../helpers/tap_events.js';

const STUBBED_GLOBALS = {
  AdminTopbar: ({ children }) => <div data-testid="topbar">{children}</div>,
  Breadcrumbs: () => null,
  ScoreEditorModal: (props) => <div data-testid="score-editor" data-match={props.match ? props.match.id : ''} />,
  CourtPicker: () => null,
  BracketTree: () => null,
  Icon: ({ name }) => <span>{name}</span>,
  filterMatchesByCourt: (matches) => matches,
  tournamentMatches: () => [],
  API: {
    fetchCompetitionDetails: vi.fn().mockResolvedValue(null),
    sendAnnouncement: vi.fn(),
    deleteAnnouncement: vi.fn(),
    updateMatchTime: vi.fn(),
    revertMatchToQueue: vi.fn(),
    recordDecision: vi.fn().mockResolvedValue({ applied: true }),
  },
  confirmDialog: vi.fn().mockResolvedValue(true),
  PoolsViewer: () => null,
  compMatches: () => [],
};

let restoreGlobals;
let AdminShiaijoPage;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  await import('../../admin_shiaijo.jsx');
  AdminShiaijoPage = window.AdminShiaijoPage;
});

afterAll(() => restoreGlobals());

beforeEach(() => {
  window.API.revertMatchToQueue = vi.fn(async () => ({ applied: true }));
  vi.useFakeTimers();
});

afterEach(() => {
  cleanup();
  window.tournamentMatches = STUBBED_GLOBALS.tournamentMatches;
  window.filterMatchesByCourt = STUBBED_GLOBALS.filterMatchesByCourt;
  vi.useRealTimers();
});

const wait = (ms) => act(async () => { vi.advanceTimersByTime(ms); });

const base = { compId: 'c1', court: 'A', status: 'scheduled', phase: 'pool', poolName: 'Pool 1' };
const threeMatches = (firstStatus = 'scheduled') => [
  { ...base, id: 'm1', status: firstStatus, sideA: { id: 'p1', name: 'Yamada' }, sideB: { id: 'p2', name: 'Tanaka' } },
  { ...base, id: 'm2', sideA: { id: 'p3', name: 'Suzuki' }, sideB: { id: 'p4', name: 'Sato' } },
  { ...base, id: 'm3', sideA: { id: 'p5', name: 'Ito' }, sideB: { id: 'p6', name: 'Kato' } },
];

// A new tournament object on every call, so rerender() re-derives the court's
// matches from window.tournamentMatches (the console memoises on it).
function consoleElement(onEditScore) {
  return (
    <AdminShiaijoPage
      tournament={{ name: 'Test Tournament', courts: ['A', 'B'], competitions: [] }}
      court="A"
      onBack={vi.fn()}
      onEditScore={onEditScore}
      onMoveCourt={vi.fn()}
      onLogout={vi.fn()}
      onViewerMode={vi.fn()}
      password=""
      showToast={vi.fn()}
      tweaks={{}}
      onSwitchCourt={vi.fn()}
    />
  );
}

function showMatches(rerender, onEditScore, matches) {
  window.tournamentMatches = () => matches;
  rerender(consoleElement(onEditScore));
}

// The matches each start write named, in order.
const started = (onEditScore) => onEditScore.mock.calls.filter((c) => c[2] && c[2].status === 'running').map((c) => c[1]);
const upNextStart = () => screen.getAllByRole('button', { name: 'Start match' })[0];

// Start m1 from the Up next card and let the start land: the refetch shows m1
// running, so the Up next card now holds m2 with its Start match enabled.
async function startFirstAndLand() {
  window.tournamentMatches = () => threeMatches();
  window.filterMatchesByCourt = (matches) => matches;
  const onEditScore = vi.fn().mockResolvedValue({ applied: true, status: 'running' });
  const { rerender } = render(consoleElement(onEditScore));
  await pointerTap(upNextStart());
  await act(async () => {});
  expect(started(onEditScore), 'precondition: the first tap started m1').toEqual(['m1']);
  showMatches(rerender, onEditScore, threeMatches('running'));
  await act(async () => {});
  return onEditScore;
}

describe('Start match double tap on the court console (bc-sdtp)', () => {
  it('the bounce of the first tap does not start the match the card shows next', async () => {
    const onEditScore = await startFirstAndLand();
    await wait(30);
    await pointerTap(upNextStart());
    await act(async () => {});
    expect(started(onEditScore), 'm2 is not started by the bounce').toEqual(['m1']);
    expect(window.API.revertMatchToQueue, 'and m1 is not sent back to the queue').not.toHaveBeenCalled();
  });

  it('a bounce landing on a queue row\'s Start match is ignored too', async () => {
    const onEditScore = await startFirstAndLand();
    await wait(30);
    // The queue lists m3 beside the Up next card's m2: its row has a Start match.
    const rowStart = screen.getAllByRole('button', { name: 'Start match' }).at(-1);
    await pointerTap(rowStart);
    await act(async () => {});
    expect(started(onEditScore)).toEqual(['m1']);
    expect(window.API.revertMatchToQueue).not.toHaveBeenCalled();
  });

  it('a deliberate tap after the bounce window starts the next match', async () => {
    const onEditScore = await startFirstAndLand();
    await wait(TAP_BOUNCE_MS + 50);
    await pointerTap(upNextStart());
    await act(async () => {});
    expect(started(onEditScore)).toEqual(['m1', 'm2']);
  });

  it('a keyboard activation (detail 0) is never read as a bounce', async () => {
    const onEditScore = await startFirstAndLand();
    await keyboardClick(upNextStart());
    await act(async () => {});
    expect(started(onEditScore)).toEqual(['m1', 'm2']);
  });
});
