// bc-obox C1: Start all draws every offered competition still in setup, so it
// waits for the check-ins this device still has queued for those, as Generate
// draw and a competition's own Start do. The screen says what it waits for and
// offers "Start now anyway" (whose confirm says who that leaves out) and
// Cancel; the starts go out once the last queued check-in has left the queue.
// A competition check-in does not apply to, or one already drawn, waits for
// nothing.
import React from 'react';
import { render, screen, cleanup, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterAll, afterEach, beforeEach, vi } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

const dashboard = { props: null };

let queued = [];
const unsentListeners = new Set();
const tellUnsent = () => { for (const fn of unsentListeners) fn(); };

const STUBBED_GLOBALS = {
  AdminDashboard: (props) => { dashboard.props = props; return <div data-testid="dashboard-probe" />; },
  AdminCompetition: () => <div />,
  AdminTopbar: () => <div />,
  Breadcrumbs: () => <div />,
  Modal: ({ title, children, footer }) => <div data-testid="modal" aria-label={title}>{children}{footer}</div>,
  confirmDialog: vi.fn(),
  pluralize: (n, word) => `${n} ${word}${n === 1 ? '' : 's'}`,
  partitionStartableCompetitions: (comps) => ({ startable: comps, blocked: [] }),
  subscribeUnsentWrites: (fn) => { unsentListeners.add(fn); return () => unsentListeners.delete(fn); },
  isNonPublicOrigin: () => false,
  API: {
    subscribeToEvents: vi.fn(() => () => {}),
    fetchCompetitions: vi.fn().mockResolvedValue([]),
    startCompetition: vi.fn().mockResolvedValue({}),
    CHECK_IN_WRITE_KINDS: Object.freeze(['checkin', 'checkin-bulk']),
    queuedWritesOf: (compID, kinds) => queued.filter((d) => d.compID === compID && kinds.includes(d.kind)).length,
    heldWrites: () => [],
  },
};

let restoreGlobals;
let AdminApp;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  await import('../../admin.jsx');
  AdminApp = window.AdminApp;
});
afterAll(() => restoreGlobals());

beforeEach(() => {
  queued = [];
  window.confirmDialog.mockReset();
  window.confirmDialog.mockResolvedValue(false);
  window.API.startCompetition.mockClear();
});
afterEach(() => cleanup());

const comp = (id, extra) => ({ id, name: `Comp ${id}`, status: 'setup', checkInEnabled: true, players: [], ...extra });

async function openStartAll(competitions, mode = 'officiated') {
  const props = {
    tournament: { name: 'Taikai', date: '01-07-2026', courts: ['A'], mode, competitions },
    onUpdate: vi.fn(), onLogout: vi.fn(), onViewerMode: vi.fn(), onPasswordChange: vi.fn(),
    tweaks: {}, password: 'pw', setView: vi.fn(), showToast: vi.fn(), authConfig: { mode: 'file' },
  };
  await act(async () => { render(<AdminApp {...props} view={{ kind: 'dashboard' }} />); });
  await act(async () => { dashboard.props.onStartAll(); });
  const confirm = screen.getByRole('button', { name: `Start ${competitions.length} competition${competitions.length === 1 ? '' : 's'}` });
  await act(async () => { fireEvent.click(confirm); });
}

const startedIDs = () => window.API.startCompetition.mock.calls.map((call) => call[0]);

describe('Start all waits for queued check-ins', () => {
  it('starts at once when nothing is queued', async () => {
    await openStartAll([comp('c1'), comp('c2')]);
    expect(startedIDs()).toEqual(['c1', 'c2']);
    expect(screen.queryByTestId('queued-wait-text')).toBeNull();
  });

  it('waits while a competition it draws still has a check-in queued, then starts them all', async () => {
    queued = [{ compID: 'c1', kind: 'checkin' }, { compID: 'c2', kind: 'checkin-bulk' }];
    await openStartAll([comp('c1'), comp('c2')]);
    expect(startedIDs()).toEqual([]);
    expect(screen.getByTestId('queued-wait-text').textContent)
      .toBe('Waiting for 2 check-ins to be sent. The draws are made and the competitions start once they arrive.');

    queued = [{ compID: 'c2', kind: 'checkin-bulk' }];
    await act(async () => { tellUnsent(); await Promise.resolve(); });
    expect(startedIDs()).toEqual([]);
    expect(screen.getByTestId('queued-wait-text').textContent).toMatch(/^Waiting for 1 check-in to be sent\./);

    queued = [];
    await act(async () => { tellUnsent(); await Promise.resolve(); });
    expect(startedIDs()).toEqual(['c1', 'c2']);
    expect(screen.queryByTestId('queued-wait-text')).toBeNull();
  });

  it('does not wait for a drawn competition, or one check-in does not apply to', async () => {
    queued = [{ compID: 'c1', kind: 'checkin' }, { compID: 'c2', kind: 'checkin' }];
    await openStartAll([comp('c1', { status: 'draw-ready' }), comp('c2', { checkInEnabled: false })]);
    expect(startedIDs()).toEqual(['c1', 'c2']);
  });

  it('does not wait in a self-run tournament, which has no check-in', async () => {
    queued = [{ compID: 'c1', kind: 'checkin' }];
    await openStartAll([comp('c1')], 'self-run');
    expect(startedIDs()).toEqual(['c1']);
  });

  it('Start now anyway says who is left out, and starts only once confirmed', async () => {
    queued = [{ compID: 'c1', kind: 'checkin' }];
    await openStartAll([comp('c1')]);
    await act(async () => { fireEvent.click(screen.getByTestId('queued-wait-now')); });
    expect(window.confirmDialog).toHaveBeenCalledTimes(1);
    expect(window.confirmDialog.mock.calls[0][0].message)
      .toContain('a competitor whose check-in has not arrived is left out of the draw');
    expect(startedIDs()).toEqual([]);

    window.confirmDialog.mockResolvedValue(true);
    await act(async () => { fireEvent.click(screen.getByTestId('queued-wait-now')); });
    expect(startedIDs()).toEqual(['c1']);
  });

  it('Cancel drops the held start: nothing starts when the check-ins arrive', async () => {
    queued = [{ compID: 'c1', kind: 'checkin' }];
    await openStartAll([comp('c1')]);
    await act(async () => { fireEvent.click(screen.getByTestId('queued-wait-cancel')); });
    expect(screen.queryByTestId('queued-wait-text')).toBeNull();
    queued = [];
    await act(async () => { tellUnsent(); await Promise.resolve(); });
    expect(startedIDs()).toEqual([]);
  });
});
