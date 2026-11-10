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
  });

  it('says only "Called" when the call was shown', async () => {
    const showToast = await callAndAnswer({ id: 'ann-1', message: 'x' });
    expect(showToast).toHaveBeenCalledTimes(1);
    expect(showToast).toHaveBeenCalledWith('Called Tanaka and Yamada to Shiaijo A');
  });
});
