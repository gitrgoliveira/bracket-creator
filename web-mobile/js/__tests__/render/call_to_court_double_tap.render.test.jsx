// bc-cdbl: a double tap on "Call to court" sends ONE announcement, and starting
// the called match withdraws that call.
//
// The tap is checked with acceptTap (tap_guard.jsx) before anything else in
// callToCourt, so the bounce of a tap is never sent. The callingKey disabled
// state stays the in-flight guard, and a deliberate tap after the bounce window
// still calls again. The call's announcement id is kept per match and deleted
// when the console starts that match (startMatch).
//
// Pointer taps pass detail: 1 (helpers/tap_events.js), or the guard exempts
// them and these tests could never go red.

import React from 'react';
import { render, act, screen, cleanup } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { TAP_BOUNCE_MS } from '../../tap_guard.jsx';
import { pointerTap, keyboardClick } from '../helpers/tap_events.js';

const CALL_TITLE = 'Announce this match to spectators and competitors';

const STUBBED_GLOBALS = {
  AdminTopbar: ({ children }) => <div data-testid="topbar">{children}</div>,
  Breadcrumbs: () => null,
  ScoreEditorModal: (props) => <div data-testid="score-editor" data-match={props.match ? props.match.id : ''} />,
  CourtPicker: () => <span />,
  BracketTree: () => null,
  Icon: ({ name }) => <span>{name}</span>,
  filterMatchesByCourt: (matches) => matches,
  tournamentMatches: () => [],
  API: {
    fetchCompetitionDetails: vi.fn().mockResolvedValue(null),
    sendAnnouncement: vi.fn(),
    deleteAnnouncement: vi.fn(),
    updateMatchTime: vi.fn(),
    recordDecision: vi.fn().mockResolvedValue({ applied: true }),
    reinstateCompetitor: vi.fn().mockResolvedValue({}),
  },
  startPatch: vi.fn(),
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

// Each call gets its own id, so a test can tell which announcement was deleted.
let nextAnnId = 0;

beforeEach(() => {
  nextAnnId = 0;
  window.API.sendAnnouncement = vi.fn(async () => ({ id: `ann-${++nextAnnId}`, message: 'x' }));
  window.API.deleteAnnouncement = vi.fn(async () => {});
  window.API.recordDecision = vi.fn(async () => ({ applied: true }));
  vi.useFakeTimers();
});

afterEach(() => {
  cleanup();
  window.tournamentMatches = STUBBED_GLOBALS.tournamentMatches;
  window.filterMatchesByCourt = STUBBED_GLOBALS.filterMatchesByCourt;
  vi.useRealTimers();
});

const wait = (ms) => act(async () => { vi.advanceTimersByTime(ms); });

function twoScheduledMatches() {
  const base = { compId: 'c1', court: 'A', status: 'scheduled', phase: 'pool', poolName: 'Pool 1' };
  return [
    { ...base, id: 'm1', sideA: { id: 'p1', name: 'Yamada' }, sideB: { id: 'p2', name: 'Tanaka' } },
    { ...base, id: 'm2', sideA: { id: 'p3', name: 'Suzuki' }, sideB: { id: 'p4', name: 'Sato' } },
  ];
}

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

function renderConsole(props = {}) {
  window.tournamentMatches = () => twoScheduledMatches();
  window.filterMatchesByCourt = (matches) => matches;
  const onEditScore = props.onEditScore || vi.fn().mockResolvedValue({ applied: true });
  return render(consoleElement(onEditScore));
}

// Every "Call to court" control on the page, in DOM order: the Up next card's
// first, then one per queue row.
const callButtons = () => screen.getAllByTitle(CALL_TITLE);

describe('Call to court double tap (bc-cdbl)', () => {
  it('a double tap on the Up next card sends one announcement', async () => {
    renderConsole();
    await pointerTap(callButtons()[0]);
    expect(window.API.sendAnnouncement).toHaveBeenCalledTimes(1);
    // The first request has settled (pointerTap flushes microtasks); the second
    // tap lands inside the bounce window of the first.
    await wait(30);
    await pointerTap(callButtons()[0]);
    expect(window.API.sendAnnouncement).toHaveBeenCalledTimes(1);
  });

  it('a deliberate tap after the bounce window calls again ("Call again")', async () => {
    renderConsole();
    await pointerTap(callButtons()[0]);
    await wait(TAP_BOUNCE_MS + 50);
    await pointerTap(callButtons()[0]);
    expect(window.API.sendAnnouncement).toHaveBeenCalledTimes(2);
  });

  it('a double tap on a queue row sends one announcement, for that row', async () => {
    renderConsole();
    // callButtons()[1] is the queue row: the Up next card holds m1, so the queue
    // lists m2 (Suzuki v Sato) alone.
    const rowButton = callButtons()[1];
    await pointerTap(rowButton);
    expect(window.API.sendAnnouncement).toHaveBeenCalledTimes(1);
    expect(window.API.sendAnnouncement.mock.calls[0][0]).toContain('Suzuki');
    await wait(30);
    await pointerTap(rowButton);
    expect(window.API.sendAnnouncement).toHaveBeenCalledTimes(1);
  });

  it('a keyboard activation (detail 0) is never read as a bounce', async () => {
    renderConsole();
    await keyboardClick(callButtons()[0]);
    await keyboardClick(callButtons()[0]);
    expect(window.API.sendAnnouncement).toHaveBeenCalledTimes(2);
  });

  it('starting the called match withdraws its announcement', async () => {
    const onEditScore = vi.fn().mockResolvedValue({ applied: true });
    renderConsole({ onEditScore });
    await pointerTap(callButtons()[0]);
    expect(window.API.sendAnnouncement).toHaveBeenCalledTimes(1);

    await pointerTap(screen.getAllByRole('button', { name: 'Start match' })[0]);
    await act(async () => {});
    expect(onEditScore).toHaveBeenCalled();
    expect(window.API.deleteAnnouncement).toHaveBeenCalledTimes(1);
    expect(window.API.deleteAnnouncement).toHaveBeenCalledWith('ann-1', '');
  });

  it('a failed withdrawal does not fail the start', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    window.API.deleteAnnouncement = vi.fn(async () => { throw new Error('network down'); });
    const onEditScore = vi.fn().mockResolvedValue({ applied: true });
    renderConsole({ onEditScore });
    await pointerTap(callButtons()[0]);
    await pointerTap(screen.getAllByRole('button', { name: 'Start match' })[0]);
    await act(async () => {});
    expect(onEditScore).toHaveBeenCalled();
    expect(window.API.deleteAnnouncement).toHaveBeenCalledTimes(1);
    expect(warn).toHaveBeenCalled();
    warn.mockRestore();
  });

  it('a start that lands before the call is answered still withdraws it', async () => {
    let answer;
    window.API.sendAnnouncement = vi.fn(() => new Promise((resolve) => { answer = resolve; }));
    renderConsole();
    await pointerTap(callButtons()[0]);
    // The call's request is still out when the match starts.
    await pointerTap(screen.getAllByRole('button', { name: 'Start match' })[0]);
    await act(async () => {});
    expect(window.API.deleteAnnouncement).not.toHaveBeenCalled();
    await act(async () => { answer({ id: 'ann-late', message: 'x' }); });
    await act(async () => {});
    expect(window.API.deleteAnnouncement).toHaveBeenCalledWith('ann-late', '');
  });

  it('recording the fusensho for a barred match withdraws its call', async () => {
    const onEditScore = vi.fn().mockResolvedValue({ applied: true });
    const { rerender } = renderConsole({ onEditScore });
    await pointerTap(callButtons()[0]);
    expect(window.API.sendAnnouncement).toHaveBeenCalledTimes(1);

    // Yamada's withdrawal reaches the console after the call went out: the
    // match is barred, its call button goes, and the notice offers the fusensho.
    window.tournamentMatches = () => {
      const [m1, m2] = twoScheduledMatches();
      return [{ ...m1, ineligibleSides: { a: 'kiken-voluntary' } }, m2];
    };
    rerender(consoleElement(onEditScore));
    await pointerTap(screen.getByTestId('barred-match-record-fusensho'));
    await act(async () => {});
    expect(window.API.recordDecision).toHaveBeenCalledTimes(1);
    expect(window.API.deleteAnnouncement).toHaveBeenCalledWith('ann-1', '');
  });

  it('starting a match that was never called withdraws nothing', async () => {
    renderConsole();
    await pointerTap(screen.getAllByRole('button', { name: 'Start match' })[0]);
    await act(async () => {});
    expect(window.API.deleteAnnouncement).not.toHaveBeenCalled();
  });
});
