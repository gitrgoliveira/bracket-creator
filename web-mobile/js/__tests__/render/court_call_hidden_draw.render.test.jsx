// bc-drpb (FR19): the court console asks before it calls a match of a draft
// whose draw is still hidden from the public, since the call puts that
// match's competitors on every public screen. A warning, never a refusal:
// Call anyway sends the call, Cancel, Escape or the backdrop sends nothing. A
// published draft, a started competition and a self-run tournament call at
// once. The question is confirmDialog's (ui.jsx), so the real DialogHost is
// mounted beside the console: the bounce of the opening tap lands on nothing,
// focus goes in and back, and it sits on the dialog layer.
//
// Pointer taps pass detail: 1 (helpers/tap_events.js), or the opened-tap guard
// exempts them and the bounce case could never go red.

import React from 'react';
import { render, act, screen, cleanup, fireEvent, within } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { TAP_BOUNCE_MS } from '../../tap_guard.jsx';
import { DialogHost, confirmDialog } from '../../ui.jsx';
import { callHiddenDrawMessage, CALL_ANYWAY_LABEL } from '../../draw_visibility.jsx';
import { pointerTap } from '../helpers/tap_events.js';

const CALL_TITLE = 'Announce this match to spectators and competitors';
const COMP_NAME = 'Kyu men';

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
  // The real one, answered by the DialogHost each test mounts.
  confirmDialog,
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

let nextAnnId = 0;
const sentAnswer = (id) => ({
  applied: true, pending: true, key: `announce:${id}`, id,
  settled: Promise.resolve({ id, message: 'x' }),
});

beforeEach(() => {
  nextAnnId = 0;
  window.API.sendAnnouncement = vi.fn(async () => sentAnswer(`ann-${++nextAnnId}`));
  window.API.deleteAnnouncement = vi.fn(async () => ({ applied: true, pending: true, key: 'k', settled: Promise.resolve(true) }));
  vi.useFakeTimers();
});

afterEach(() => {
  cleanup();
  window.tournamentMatches = STUBBED_GLOBALS.tournamentMatches;
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

const HIDDEN = { id: 'c1', name: COMP_NAME, status: 'draw-ready', drawHidden: true };

function renderConsole({ comp = HIDDEN, mode } = {}) {
  window.tournamentMatches = () => twoScheduledMatches();
  return render(
    <div>
      <AdminShiaijoPage
        tournament={{ name: 'Test Tournament', mode, courts: ['A', 'B'], competitions: [comp] }}
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
      />
      <DialogHost />
    </div>,
  );
}

// The Up next card's Call to court first, then one per queue row.
const callButtons = () => screen.getAllByTitle(CALL_TITLE);
const callDialog = () => screen.queryByRole('dialog', { name: 'Call to court?' });

// Opens the question from `button` and waits out the bounce window of that tap.
async function askFrom(button) {
  await pointerTap(button);
  await wait(TAP_BOUNCE_MS + 50);
}

describe('Call to court on a hidden draft (bc-drpb)', () => {
  it('asks first, naming the competition, and sends nothing until answered', async () => {
    renderConsole();
    await askFrom(callButtons()[0]);
    const dialog = callDialog();
    expect(dialog, 'the console asks before calling').not.toBeNull();
    expect(dialog.textContent).toContain(callHiddenDrawMessage(COMP_NAME));
    expect(within(dialog).getByRole('button', { name: CALL_ANYWAY_LABEL })).toBeTruthy();
    expect(within(dialog).getByText('Cancel')).toBeTruthy();
    expect(window.API.sendAnnouncement).not.toHaveBeenCalled();
  });

  it('Call anyway sends the call', async () => {
    renderConsole();
    await askFrom(callButtons()[0]);
    await pointerTap(within(callDialog()).getByRole('button', { name: CALL_ANYWAY_LABEL }));
    await act(async () => {});
    expect(callDialog()).toBeNull();
    expect(window.API.sendAnnouncement).toHaveBeenCalledTimes(1);
    expect(window.API.sendAnnouncement.mock.calls[0][0]).toBe('Now calling Tanaka and Yamada to Shiaijo A.');
  });

  it('Cancel sends nothing', async () => {
    renderConsole();
    await askFrom(callButtons()[0]);
    // The Cancel button (the close × is labelled Cancel too).
    await pointerTap(within(callDialog()).getByText('Cancel'));
    await act(async () => {});
    expect(callDialog()).toBeNull();
    expect(window.API.sendAnnouncement).not.toHaveBeenCalled();
  });

  it('Escape cancels and gives focus back to the Call to court button', async () => {
    renderConsole();
    const button = callButtons()[0];
    button.focus();
    await askFrom(button);
    expect(callDialog().contains(document.activeElement), 'focus goes into the question').toBe(true);
    await act(async () => { fireEvent.keyDown(document.activeElement, { key: 'Escape' }); });
    expect(callDialog()).toBeNull();
    expect(document.activeElement).toBe(button);
    expect(window.API.sendAnnouncement).not.toHaveBeenCalled();
  });

  it('the bounce of the opening tap does not answer the question', async () => {
    renderConsole();
    await pointerTap(callButtons()[0]);
    await wait(30);
    await pointerTap(within(callDialog()).getByRole('button', { name: CALL_ANYWAY_LABEL }));
    await act(async () => {});
    expect(callDialog(), 'the question stays open').not.toBeNull();
    expect(window.API.sendAnnouncement).not.toHaveBeenCalled();
  });

  it('sits on the dialog layer', async () => {
    const { container } = renderConsole();
    await askFrom(callButtons()[0]);
    expect(container.querySelector('.modal-backdrop--dialog')).not.toBeNull();
  });

  it('a queue row asks too', async () => {
    renderConsole();
    // The Up next card holds m1, so the queue row is m2 (Suzuki v Sato).
    await askFrom(callButtons()[1]);
    expect(window.API.sendAnnouncement).not.toHaveBeenCalled();
    await pointerTap(within(callDialog()).getByRole('button', { name: CALL_ANYWAY_LABEL }));
    await act(async () => {});
    expect(window.API.sendAnnouncement).toHaveBeenCalledTimes(1);
    expect(window.API.sendAnnouncement.mock.calls[0][0]).toContain('Suzuki');
  });

  it.each([
    ['a published draft', { id: 'c1', name: COMP_NAME, status: 'draw-ready' }, undefined],
    ['a started competition stored as hidden', { ...HIDDEN, status: 'running' }, undefined],
    ['a hidden draft of a self-run tournament', HIDDEN, 'self-run'],
  ])('%s calls at once', async (_label, comp, mode) => {
    renderConsole({ comp, mode });
    await pointerTap(callButtons()[0]);
    await act(async () => {});
    expect(callDialog()).toBeNull();
    expect(window.API.sendAnnouncement).toHaveBeenCalledTimes(1);
  });
});
