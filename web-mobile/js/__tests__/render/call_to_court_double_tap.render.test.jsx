// bc-cdbl: a double tap on "Call to court" sends ONE announcement, and starting
// the called match withdraws that call.
//
// The tap is checked with acceptTap (tap_guard.jsx) before anything else in
// callToCourt, so the bounce of a tap is never sent. The callingKey disabled
// state stays the in-flight guard, and a deliberate tap after the bounce window
// still calls again. The call's announcement id is kept per match and deleted
// once the console's list shows that match out of scheduled, however it got
// there (started here, started elsewhere, or closed by a decision).
//
// Pointer taps pass detail: 1 (helpers/tap_events.js), or the guard exempts
// them and these tests could never go red.

import React from 'react';
import { render, act, screen, cleanup, within } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { TAP_BOUNCE_MS } from '../../tap_guard.jsx';
import { pointerTap, keyboardClick } from '../helpers/tap_events.js';

const CALL_TITLE = 'Announce this match to spectators and competitors';

const STUBBED_GLOBALS = {
  AdminTopbar: ({ children }) => <div data-testid="topbar">{children}</div>,
  Breadcrumbs: () => null,
  ScoreEditorModal: (props) => <div data-testid="score-editor" data-match={props.match ? props.match.id : ''} />,
  // One button per OTHER court, calling onChange as the real picker does, so a
  // test can reach the move confirm from a queue row.
  CourtPicker: ({ value, courts, onChange }) => (
    <span>
      {(courts || []).filter((cc) => cc !== value).map((cc) => (
        <button type="button" key={cc} data-testid={`move-to-${cc}`} onClick={() => onChange(cc)}>{`Move to ${cc}`}</button>
      ))}
    </span>
  ),
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
function consoleElement(onEditScore, onMoveCourt = vi.fn()) {
  return (
    <AdminShiaijoPage
      tournament={{ name: 'Test Tournament', courts: ['A', 'B'], competitions: [] }}
      court="A"
      onBack={vi.fn()}
      onEditScore={onEditScore}
      onMoveCourt={onMoveCourt}
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
  return render(consoleElement(onEditScore, props.onMoveCourt));
}

// The console's match list as the next refetch shows it: a rerender with a new
// tournament object re-derives the court's matches from window.tournamentMatches.
function showMatches(rerender, onEditScore, matches) {
  window.tournamentMatches = () => matches;
  rerender(consoleElement(onEditScore));
}

// The two scheduled matches with the first one changed, as a refetch shows it
// once it has been started or closed.
function firstMatchWith(patch) {
  const [m1, m2] = twoScheduledMatches();
  return [{ ...m1, ...patch }, m2];
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

  it('starting the called match withdraws its announcement once the list shows it running', async () => {
    const onEditScore = vi.fn().mockResolvedValue({ applied: true });
    const { rerender } = renderConsole({ onEditScore });
    await pointerTap(callButtons()[0]);
    expect(window.API.sendAnnouncement).toHaveBeenCalledTimes(1);

    await pointerTap(screen.getAllByRole('button', { name: 'Start match' })[0]);
    await act(async () => {});
    expect(onEditScore).toHaveBeenCalled();
    expect(window.API.deleteAnnouncement).not.toHaveBeenCalled();

    showMatches(rerender, onEditScore, firstMatchWith({ status: 'running' }));
    await act(async () => {});
    expect(window.API.deleteAnnouncement).toHaveBeenCalledTimes(1);
    expect(window.API.deleteAnnouncement).toHaveBeenCalledWith('ann-1', '');
  });

  it('a match started on another device withdraws its call', async () => {
    const onEditScore = vi.fn().mockResolvedValue({ applied: true });
    const { rerender } = renderConsole({ onEditScore });
    await pointerTap(callButtons()[0]);
    showMatches(rerender, onEditScore, firstMatchWith({ status: 'running' }));
    await act(async () => {});
    expect(window.API.deleteAnnouncement).toHaveBeenCalledWith('ann-1', '');
  });

  it('a call stays up while its match is scheduled or missing from the list', async () => {
    const onEditScore = vi.fn().mockResolvedValue({ applied: true });
    const { rerender } = renderConsole({ onEditScore });
    await pointerTap(callButtons()[0]);
    showMatches(rerender, onEditScore, twoScheduledMatches());
    await act(async () => {});
    // The list is empty while it loads: an absent match is not judged.
    showMatches(rerender, onEditScore, []);
    await act(async () => {});
    showMatches(rerender, onEditScore, twoScheduledMatches());
    await act(async () => {});
    expect(window.API.deleteAnnouncement).not.toHaveBeenCalled();
  });

  it('a failed withdrawal does not fail the start', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    window.API.deleteAnnouncement = vi.fn(async () => { throw new Error('network down'); });
    const onEditScore = vi.fn().mockResolvedValue({ applied: true });
    const { rerender } = renderConsole({ onEditScore });
    await pointerTap(callButtons()[0]);
    await pointerTap(screen.getAllByRole('button', { name: 'Start match' })[0]);
    await act(async () => {});
    expect(onEditScore).toHaveBeenCalled();
    showMatches(rerender, onEditScore, firstMatchWith({ status: 'running' }));
    await act(async () => {});
    expect(window.API.deleteAnnouncement).toHaveBeenCalledTimes(1);
    expect(warn).toHaveBeenCalled();
    warn.mockRestore();
  });

  it('a start that lands before the call is answered still withdraws it', async () => {
    let answer;
    window.API.sendAnnouncement = vi.fn(() => new Promise((resolve) => { answer = resolve; }));
    const onEditScore = vi.fn().mockResolvedValue({ applied: true });
    const { rerender } = renderConsole({ onEditScore });
    await pointerTap(callButtons()[0]);
    // The call's request is still out when the match starts.
    await pointerTap(screen.getAllByRole('button', { name: 'Start match' })[0]);
    await act(async () => {});
    showMatches(rerender, onEditScore, firstMatchWith({ status: 'running' }));
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
    // The decision lands and the next refetch shows the match closed.
    expect(window.API.deleteAnnouncement).not.toHaveBeenCalled();
    showMatches(rerender, onEditScore, firstMatchWith({ status: 'completed', decision: 'fusensho' }));
    await act(async () => {});
    expect(window.API.deleteAnnouncement).toHaveBeenCalledWith('ann-1', '');
  });

  it('a second deliberate call withdraws the first banner once the second is answered, and starting the match withdraws the second', async () => {
    const onEditScore = vi.fn().mockResolvedValue({ applied: true });
    const { rerender } = renderConsole({ onEditScore });
    await pointerTap(callButtons()[0]);
    expect(window.API.sendAnnouncement).toHaveBeenCalledTimes(1);
    await wait(TAP_BOUNCE_MS + 50);
    // A side's name resolved between the two calls, so the second announcement's
    // text differs. The server replaces only an announcement with identical text,
    // so the first banner must come down here, not wait out its expiry. It comes
    // down only once the second call is answered, so the banner is never taken
    // down for a call that did not go out.
    let answerSecond;
    window.API.sendAnnouncement.mockImplementationOnce(() => new Promise((resolve) => { answerSecond = resolve; }));
    const [m1, m2] = twoScheduledMatches();
    showMatches(rerender, onEditScore, [{ ...m1, sideA: { id: 'p1', name: 'Yamada Taro' } }, m2]);
    await act(async () => {});
    await pointerTap(callButtons()[0]);
    await act(async () => {});
    expect(window.API.sendAnnouncement).toHaveBeenCalledTimes(2);
    expect(window.API.sendAnnouncement.mock.calls[1][0]).toContain('Yamada Taro');
    expect(window.API.deleteAnnouncement).not.toHaveBeenCalled();
    await act(async () => { answerSecond({ id: 'ann-2', message: 'x' }); });
    await act(async () => {});
    expect(window.API.deleteAnnouncement).toHaveBeenCalledTimes(1);
    expect(window.API.deleteAnnouncement).toHaveBeenLastCalledWith('ann-1', '');

    await pointerTap(screen.getAllByRole('button', { name: 'Start match' })[0]);
    await act(async () => {});
    showMatches(rerender, onEditScore, firstMatchWith({ status: 'running', sideA: { id: 'p1', name: 'Yamada Taro' } }));
    await act(async () => {});
    expect(window.API.deleteAnnouncement).toHaveBeenCalledTimes(2);
    expect(window.API.deleteAnnouncement).toHaveBeenLastCalledWith('ann-2', '');
  });

  it('a failed re-send leaves the first call in place: nothing is withdrawn, and starting the match still takes it down', async () => {
    const onEditScore = vi.fn().mockResolvedValue({ applied: true });
    const { rerender } = renderConsole({ onEditScore });
    await pointerTap(callButtons()[0]);
    expect(window.API.sendAnnouncement).toHaveBeenCalledTimes(1);
    await wait(TAP_BOUNCE_MS + 50);
    window.API.sendAnnouncement.mockImplementationOnce(async () => { throw new Error('network down'); });
    const [m1, m2] = twoScheduledMatches();
    showMatches(rerender, onEditScore, [{ ...m1, sideA: { id: 'p1', name: 'Yamada Taro' } }, m2]);
    await act(async () => {});
    await pointerTap(callButtons()[0]);
    await act(async () => {});
    expect(window.API.sendAnnouncement).toHaveBeenCalledTimes(2);
    expect(window.API.deleteAnnouncement).not.toHaveBeenCalled();
    // The first banner is still up, so starting its match takes it down.
    await pointerTap(screen.getAllByRole('button', { name: 'Start match' })[0]);
    await act(async () => {});
    showMatches(rerender, onEditScore, firstMatchWith({ status: 'running', sideA: { id: 'p1', name: 'Yamada Taro' } }));
    await act(async () => {});
    expect(window.API.deleteAnnouncement).toHaveBeenCalledTimes(1);
    expect(window.API.deleteAnnouncement).toHaveBeenCalledWith('ann-1', '');
  });

  it('a re-call with unchanged text deletes nothing: the server already replaced the first banner, and starting the match withdraws the new one', async () => {
    const onEditScore = vi.fn().mockResolvedValue({ applied: true });
    const { rerender } = renderConsole({ onEditScore });
    await pointerTap(callButtons()[0]);
    expect(window.API.sendAnnouncement).toHaveBeenCalledTimes(1);
    await wait(TAP_BOUNCE_MS + 50);
    // The same text again. The server removes the active announcement with
    // identical text and answers with a new id, so the earlier id is already gone
    // and a DELETE of it would 404 ("Could not withdraw the court call").
    await pointerTap(callButtons()[0]);
    await act(async () => {});
    expect(window.API.sendAnnouncement).toHaveBeenCalledTimes(2);
    expect(window.API.deleteAnnouncement).not.toHaveBeenCalled();

    await pointerTap(screen.getAllByRole('button', { name: 'Start match' })[0]);
    await act(async () => {});
    showMatches(rerender, onEditScore, firstMatchWith({ status: 'running' }));
    await act(async () => {});
    expect(window.API.deleteAnnouncement).toHaveBeenCalledTimes(1);
    expect(window.API.deleteAnnouncement).toHaveBeenCalledWith('ann-2', '');
  });

  it('after an unchanged re-call, a changed re-call takes down the banner that is up, once it is answered', async () => {
    const onEditScore = vi.fn().mockResolvedValue({ applied: true });
    const { rerender } = renderConsole({ onEditScore });
    await pointerTap(callButtons()[0]);
    await wait(TAP_BOUNCE_MS + 50);
    await pointerTap(callButtons()[0]);
    await act(async () => {});
    expect(window.API.deleteAnnouncement).not.toHaveBeenCalled();

    await wait(TAP_BOUNCE_MS + 50);
    let answerThird;
    window.API.sendAnnouncement.mockImplementationOnce(() => new Promise((resolve) => { answerThird = resolve; }));
    const [m1, m2] = twoScheduledMatches();
    showMatches(rerender, onEditScore, [{ ...m1, sideA: { id: 'p1', name: 'Yamada Taro' } }, m2]);
    await act(async () => {});
    await pointerTap(callButtons()[0]);
    await act(async () => {});
    expect(window.API.sendAnnouncement).toHaveBeenCalledTimes(3);
    expect(window.API.deleteAnnouncement).not.toHaveBeenCalled();
    await act(async () => { answerThird({ id: 'ann-3', message: 'x' }); });
    await act(async () => {});
    // ann-2 is the banner up for this match; ann-1 was replaced by the server.
    expect(window.API.deleteAnnouncement).toHaveBeenCalledTimes(1);
    expect(window.API.deleteAnnouncement).toHaveBeenCalledWith('ann-2', '');
  });

  it('starting a match that was never called withdraws nothing', async () => {
    renderConsole();
    await pointerTap(screen.getAllByRole('button', { name: 'Start match' })[0]);
    await act(async () => {});
    expect(window.API.deleteAnnouncement).not.toHaveBeenCalled();
  });

  // Calls m2 from its queue row, then moves m2 to shiaijo B from the same row.
  // The move confirm is the console's own dialog (requestMoveCourt, then
  // confirmMoveCourt), so it is driven through the row's court picker.
  const callThenMoveSuzuki = async (onMoveCourt) => {
    const { container } = renderConsole({ onMoveCourt });
    await pointerTap(callButtons()[1]);
    expect(window.API.sendAnnouncement).toHaveBeenCalledTimes(1);
    const row = [...container.querySelectorAll('.shiaijo-qrow')].find((r) => r.textContent.includes('Suzuki'));
    await pointerTap(within(row).getByTestId('move-to-B'));
    await wait(TAP_BOUNCE_MS + 50);
    await pointerTap(screen.getByRole('button', { name: 'Move to Shiaijo B' }));
    await act(async () => {});
  };

  it('moving the called match to another court from this console withdraws its call', async () => {
    // The host answers true once the move has landed (admin.jsx moveMatchCourt).
    const onMoveCourt = vi.fn(async () => true);
    await callThenMoveSuzuki(onMoveCourt);
    expect(onMoveCourt).toHaveBeenCalledWith('c1', 'm2', 'B');
    expect(window.API.deleteAnnouncement).toHaveBeenCalledTimes(1);
    expect(window.API.deleteAnnouncement).toHaveBeenCalledWith('ann-1', '');
  });

  it('a move that fails leaves the call up', async () => {
    // The real host catches the failed move, toasts it and resolves false.
    const onMoveCourt = vi.fn(async () => false);
    await callThenMoveSuzuki(onMoveCourt);
    expect(onMoveCourt).toHaveBeenCalledWith('c1', 'm2', 'B');
    expect(window.API.deleteAnnouncement).not.toHaveBeenCalled();
  });
});
