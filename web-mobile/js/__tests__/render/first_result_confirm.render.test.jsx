// bc-drev (operator decision D4 b, "This warning really must be on the shiaijo
// operator view"): on the court console, the Start of a drawn competition's
// first match asks whether to start the competition, through the app's confirm
// dialog (first_result_confirm.jsx), BEFORE anything is sent: a Not yet sends
// nothing back to the queue, checks nothing and starts nothing. Agreed, the
// start goes out as before, and the competition is not asked about again.

import React from 'react';
import { render, act, screen, cleanup, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { pointerTap, keyboardClick } from '../helpers/tap_events.js';
import { resetFirstResultConfirm } from '../../first_result_confirm.jsx';

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
    checkStart: vi.fn(),
    startCheckUntrusted: vi.fn(),
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
  resetFirstResultConfirm();
  window.API.revertMatchToQueue = vi.fn(async () => ({ applied: true, pending: true, key: 'c1:m1\u001fsendback', settled: Promise.resolve(true) }));
  window.API.checkStart = vi.fn(async () => ({ ok: true }));
  window.API.startCheckUntrusted = vi.fn(() => false);
});

afterEach(() => {
  cleanup();
  window.tournamentMatches = STUBBED_GLOBALS.tournamentMatches;
  window.filterMatchesByCourt = STUBBED_GLOBALS.filterMatchesByCourt;
  window.confirmDialog = STUBBED_GLOBALS.confirmDialog;
});

const pool = (compId, compName) => ({ compId, compName, court: 'A', status: 'scheduled', phase: 'pool', poolName: 'Pool 1' });
const drawnMatches = () => [
  { ...pool('c2', 'Women'), id: 'm2', sideA: { id: 'p3', name: 'Suzuki' }, sideB: { id: 'p4', name: 'Sato' } },
  { ...pool('c2', 'Women'), id: 'm3', sideA: { id: 'p5', name: 'Ito' }, sideB: { id: 'p6', name: 'Kato' } },
];
const runningElsewhere = () => ({ ...pool('c1', 'Men'), id: 'm1', status: 'running', scoreVersion: 4, ipponsA: ['M'], sideA: { id: 'p1', name: 'Yamada' }, sideB: { id: 'p2', name: 'Tanaka' } });
const competition = (id, name, status, list) => ({ id, name, status, poolMatches: list.filter((m) => m.compId === id), bracket: null });

function renderConsole(list, competitions) {
  window.tournamentMatches = () => list;
  window.filterMatchesByCourt = (ms) => ms;
  const onEditScore = vi.fn().mockResolvedValue({ applied: true, status: 'running' });
  const showToast = vi.fn();
  const handlers = { onBack: vi.fn(), onMoveCourt: vi.fn(), onLogout: vi.fn(), onViewerMode: vi.fn(), onSwitchCourt: vi.fn() };
  render(
    <AdminShiaijoPage
      tournament={{ name: 'Test Tournament', courts: ['A', 'B'], competitions }}
      court="A"
      onEditScore={onEditScore}
      password="pw"
      showToast={showToast}
      tweaks={{}}
      {...handlers}
    />
  );
  return { onEditScore, showToast };
}

const starts = (onEditScore) => onEditScore.mock.calls.filter((c) => c[2] && c[2].status === 'running').map((c) => c[1]);
const upNextStart = () => screen.getAllByRole('button', { name: 'Start match' })[0];
const settle = () => act(async () => { for (let i = 0; i < 6; i++) await Promise.resolve(); });

describe('the court console asks before the first result of a drawn competition (bc-drev D4 b)', () => {
  it('asks, naming the competition and the match, before anything is sent', async () => {
    const list = drawnMatches();
    window.confirmDialog = vi.fn().mockResolvedValue(false);
    const { onEditScore } = renderConsole(list, [competition('c2', 'Women', 'draw-ready', list)]);
    await pointerTap(upNextStart());
    await settle();
    expect(window.confirmDialog).toHaveBeenCalledTimes(1);
    const q = window.confirmDialog.mock.calls[0][0];
    expect(q.title).toBe('Start Women?');
    expect(q.message).toContain('Women has not started yet. Starting ');
    expect(q.message).toContain('Sato vs Suzuki starts it.');
    expect(q.confirmLabel).toBe('Start the competition');
    // Not yet: nothing was checked or started.
    expect(starts(onEditScore)).toEqual([]);
    expect(window.API.checkStart).not.toHaveBeenCalled();
  });

  it('agreed, the match starts once, and the next Start asks nothing', async () => {
    const list = drawnMatches();
    window.confirmDialog = vi.fn().mockResolvedValue(true);
    const { onEditScore } = renderConsole(list, [competition('c2', 'Women', 'draw-ready', list)]);
    await pointerTap(upNextStart());
    await settle();
    expect(starts(onEditScore)).toEqual(['m2']);
    expect(window.confirmDialog).toHaveBeenCalledTimes(1);
  });

  it('a Not yet over a running bout of another competition sends that bout nowhere', async () => {
    const list = [runningElsewhere(), ...drawnMatches()];
    window.confirmDialog = vi.fn().mockResolvedValue(false);
    const { onEditScore } = renderConsole(list, [competition('c1', 'Men', 'pools', list), competition('c2', 'Women', 'draw-ready', list)]);
    fireEvent.change(screen.getByLabelText('Select competition to officiate'), { target: { value: 'c2' } });
    await settle();
    await pointerTap(upNextStart());
    await settle();
    expect(window.confirmDialog).toHaveBeenCalledTimes(1);
    expect(window.API.checkStart).not.toHaveBeenCalled();
    expect(window.API.revertMatchToQueue).not.toHaveBeenCalled();
    expect(starts(onEditScore)).toEqual([]);

    // Asked again on the next tap (a keyboard click, which is never the bounce
    // of the first); agreed, the running bout goes back and the pick starts.
    window.confirmDialog = vi.fn().mockResolvedValue(true);
    await keyboardClick(upNextStart());
    await settle();
    expect(window.confirmDialog).toHaveBeenCalledTimes(1);
    expect(window.API.revertMatchToQueue).toHaveBeenCalledTimes(1);
    expect(starts(onEditScore)).toEqual(['m2']);
  });

  it('a competition already started asks nothing', async () => {
    const list = drawnMatches();
    window.confirmDialog = vi.fn().mockResolvedValue(false);
    const { onEditScore } = renderConsole(list, [competition('c2', 'Women', 'pools', list)]);
    await pointerTap(upNextStart());
    await settle();
    expect(window.confirmDialog).not.toHaveBeenCalled();
    expect(starts(onEditScore)).toEqual(['m2']);
  });
});
