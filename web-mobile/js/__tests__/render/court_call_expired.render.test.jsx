// A court call held on the device for longer than its five minutes reaches the
// server expired and is never shown. The console has already said "Called ...",
// so once the server answers it tells the operator the call did not go out, in
// the console's own toast channel. The composer's counterpart is
// announcement_expired_call.render.test.jsx.
//
// Pointer taps pass detail: 1 (helpers/tap_events.js), or the guard exempts
// them.

import React from 'react';
import { render, act, screen, cleanup } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { pointerTap } from '../helpers/tap_events.js';

const CALL_TITLE = 'Announce this match to spectators and competitors';

const STUBBED_GLOBALS = {
  AdminTopbar: ({ children }) => <div data-testid="topbar">{children}</div>,
  Breadcrumbs: () => null,
  ScoreEditorModal: () => null,
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
  window.API.deleteAnnouncement = vi.fn(async () => ({ applied: true, pending: true, key: 'k', settled: Promise.resolve(true) }));
  window.tournamentMatches = () => [{
    compId: 'c1', court: 'A', status: 'scheduled', phase: 'pool', poolName: 'Pool 1',
    id: 'm1', sideA: { id: 'p1', name: 'Yamada' }, sideB: { id: 'p2', name: 'Tanaka' },
  }];
});

afterEach(() => {
  cleanup();
  window.tournamentMatches = STUBBED_GLOBALS.tournamentMatches;
});

// Renders the console, taps Call to court, and lets the server answer `stored`.
async function callAndAnswer(stored) {
  window.API.sendAnnouncement = vi.fn(async () => ({
    applied: true, pending: true, key: 'announce:ann-1', id: 'ann-1', settled: Promise.resolve(stored),
  }));
  const showToast = vi.fn();
  render(
    <AdminShiaijoPage
      tournament={{ name: 'Test Tournament', courts: ['A'], competitions: [] }}
      court="A"
      onBack={vi.fn()}
      onEditScore={vi.fn().mockResolvedValue({ applied: true })}
      onMoveCourt={vi.fn()}
      onLogout={vi.fn()}
      onViewerMode={vi.fn()}
      password=""
      showToast={showToast}
      tweaks={{}}
      onSwitchCourt={vi.fn()}
    />,
  );
  await pointerTap(screen.getAllByTitle(CALL_TITLE)[0]);
  await act(async () => {});
  return showToast;
}

describe('the court console after the server answers a call', () => {
  it('says the call did not go out when its five minutes had run out on arrival', async () => {
    const showToast = await callAndAnswer({ id: 'ann-1', message: 'x', expired: true });
    expect(window.API.sendAnnouncement).toHaveBeenCalledTimes(1);
    expect(showToast).toHaveBeenCalledWith(
      'That court call did not go out: its 5 minutes ran out before it reached the server.', 'error');
    // The card goes back to uncalled: the button offers a first call again.
    expect(screen.queryByText('Announced to spectators. Start the match when both are at the line.')).toBeNull();
    expect(screen.queryByText('Call again')).toBeNull();
    expect(screen.getAllByText('Call to court').length).toBeGreaterThan(0);
  });

  it('says only "Called" when the call was shown', async () => {
    const showToast = await callAndAnswer({ id: 'ann-1', message: 'x' });
    expect(showToast).toHaveBeenCalledTimes(1);
    expect(showToast).toHaveBeenCalledWith('Called Tanaka and Yamada to Shiaijo A');
    expect(screen.getAllByText('Call again').length).toBeGreaterThan(0);
  });
});

// The card reads as called only while a call this console made for the match
// is still showing: once the call's time is over it offers Call to court
// again. Fake timers move the clock past the call's five minutes.
const ANNOUNCED = 'Announced to spectators. Start the match when both are at the line.';
const MINUTE = 60 * 1000;

function renderConsole() {
  render(
    <AdminShiaijoPage
      tournament={{ name: 'Test Tournament', courts: ['A'], competitions: [] }}
      court="A"
      onBack={vi.fn()}
      onEditScore={vi.fn().mockResolvedValue({ applied: true })}
      onMoveCourt={vi.fn()}
      onLogout={vi.fn()}
      onViewerMode={vi.fn()}
      password=""
      showToast={vi.fn()}
      tweaks={{}}
      onSwitchCourt={vi.fn()}
    />,
  );
}

const tapCall = async () => {
  await pointerTap(screen.getAllByTitle(CALL_TITLE)[0]);
  await act(async () => {});
};
const advance = (ms) => act(async () => { vi.advanceTimersByTime(ms); });
const expectCalled = () => {
  expect(screen.getByText(ANNOUNCED)).toBeTruthy();
  expect(screen.getAllByText('Call again').length).toBeGreaterThan(0);
};
const expectUncalled = () => {
  expect(screen.queryByText(ANNOUNCED)).toBeNull();
  expect(screen.queryByText('Call again')).toBeNull();
  expect(screen.getAllByText('Call to court').length).toBeGreaterThan(0);
};

describe('the court console once a call has stopped showing', () => {
  beforeEach(() => { vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'Date'] }); });
  afterEach(() => { vi.useRealTimers(); });

  it('offers Call to court again once a shown call\'s time is over', async () => {
    const expiresAt = new Date(Date.now() + 5 * MINUTE).toISOString();
    window.API.sendAnnouncement = vi.fn(async () => ({
      applied: true, pending: true, key: 'announce:ann-1', id: 'ann-1',
      settled: Promise.resolve({ id: 'ann-1', message: 'x', expiresAt }),
    }));
    renderConsole();
    await tapCall();
    expectCalled();
    await advance(5 * MINUTE - 1000);
    expectCalled();
    await advance(1000);
    expectUncalled();
  });

  it('does not keep an earlier call that has expired standing when a held call arrives expired', async () => {
    const t0 = Date.now();
    let answerHeld;
    window.API.sendAnnouncement = vi.fn()
      .mockImplementationOnce(async () => ({
        applied: true, pending: true, key: 'announce:ann-1', id: 'ann-1',
        settled: Promise.resolve({ id: 'ann-1', message: 'x', expiresAt: new Date(t0 + 5 * MINUTE).toISOString() }),
      }))
      .mockImplementationOnce(async () => ({
        applied: true, pending: true, key: 'announce:ann-2', id: 'ann-2',
        settled: new Promise((resolve) => { answerHeld = resolve; }),
      }));
    renderConsole();
    await tapCall();
    expectCalled();
    // A second call made a minute later is held on the device (offline).
    await advance(MINUTE);
    await tapCall();
    expect(window.API.sendAnnouncement).toHaveBeenCalledTimes(2);
    expectCalled();
    // The first call's time is over; the held one would still show, had it
    // gone out. It arrives expired: nothing is showing now.
    await advance(4.5 * MINUTE);
    await act(async () => { answerHeld({ id: 'ann-2', message: 'x', expired: true }); });
    expectUncalled();
  });

  it('reads uncalled once a held call\'s time is over, before the server has answered', async () => {
    window.API.sendAnnouncement = vi.fn(async () => ({
      applied: true, pending: true, key: 'announce:ann-1', id: 'ann-1',
      settled: new Promise(() => {}),
    }));
    renderConsole();
    await tapCall();
    expectCalled();
    await advance(5 * MINUTE);
    expectUncalled();
  });

  it('reads called again when a held call whose time ran out on the device lands with time left', async () => {
    let answerHeld;
    window.API.sendAnnouncement = vi.fn(async () => ({
      applied: true, pending: true, key: 'announce:ann-1', id: 'ann-1',
      settled: new Promise((resolve) => { answerHeld = resolve; }),
    }));
    renderConsole();
    await tapCall();
    expectCalled();
    // Held offline past its five minutes: the device reads it as over.
    await advance(5 * MINUTE + 10 * 1000);
    expectUncalled();
    // It lands late, and the server shows it for its one-minute floor: the
    // screens show the call, so the card reads as called until that expiry.
    const expiresAt = new Date(Date.now() + MINUTE).toISOString();
    await act(async () => { answerHeld({ id: 'ann-1', message: 'x', expiresAt }); });
    expectCalled();
    await advance(MINUTE - 1000);
    expectCalled();
    await advance(1000);
    expectUncalled();
  });
});
