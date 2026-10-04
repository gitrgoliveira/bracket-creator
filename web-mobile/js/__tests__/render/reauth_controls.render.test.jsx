// bc-qttl: the queued-write "auth-required" state used to be a dead end (see
// the note in app.jsx above window.requestReauth). These tests cover the two
// pieces of chrome that became actionable: SyncStatusPill (the element that
// says "Sign in to save" is now the thing you click) and AdminTopbar's
// always-visible "Sign in to save" button (SyncStatusPill only renders inside
// a running match's score editor, so the topbar is the persistent home for
// the parked-queue state once that editor is closed).
//
// Real React 18 + RTL, because these are subscribe-then-rerender components:
// the unit suite's fake React stub never invokes useEffect, so the
// subscription (and the state change it drives) would never fire there.

import React from 'react';
import { render, screen, fireEvent, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { SyncStatusPill } from '../../admin_scoring_autosave.jsx';

// A minimal stand-in for api_client.jsx's subscribeSyncStatus: same contract
// (replay the current value immediately on subscribe; return an unsub fn) but
// with a `set` escape hatch so a test can drive status transitions without
// exercising the real write queue.
function makeFakeSyncBus(initial) {
  let status = initial;
  const listeners = new Set();
  return {
    subscribe: (fn) => {
      listeners.add(fn);
      fn(status);
      return () => listeners.delete(fn);
    },
    set: (s) => {
      status = s;
      for (const fn of listeners) fn(s);
    },
  };
}

let originalSubscribe, originalRequestReauth;

beforeAll(async () => {
  originalSubscribe = { had: 'subscribeSyncStatus' in window, value: window.subscribeSyncStatus };
  originalRequestReauth = { had: 'requestReauth' in window, value: window.requestReauth };
  // admin_shell.jsx is a window.* global-script module (like admin_shell.test.jsx
  // documents); import it for its side effect of setting window.AdminTopbar.
  // sideName/Icon/Modal come from admin_helpers.jsx / ui.jsx, already loaded by
  // vitest.setup.render.js. AdminTopbar's other window.* reads (pluralize,
  // formatDate, etc.) are only reached from JSX branches this suite doesn't
  // trigger (the running-strip, gated behind hideRunningStrip below).
  await import('../../admin_shell.jsx');
});

afterAll(() => {
  if (originalSubscribe.had) window.subscribeSyncStatus = originalSubscribe.value;
  else delete window.subscribeSyncStatus;
  if (originalRequestReauth.had) window.requestReauth = originalRequestReauth.value;
  else delete window.requestReauth;
});

describe('SyncStatusPill: auth-required becomes a clickable control', () => {
  let bus;

  beforeEach(() => {
    bus = makeFakeSyncBus('synced');
    window.subscribeSyncStatus = bus.subscribe;
  });

  it('renders a <button> for auth-required when window.requestReauth exists, and clicking it calls window.requestReauth', async () => {
    const requestReauth = vi.fn();
    window.requestReauth = requestReauth;

    render(<SyncStatusPill isRunning={true} />);
    await act(async () => { bus.set('auth-required'); });

    const pill = screen.getByTestId('sync-status-pill');
    expect(pill.tagName).toBe('BUTTON');
    expect(pill).toHaveTextContent('Sign in to save');

    fireEvent.click(pill);
    expect(requestReauth).toHaveBeenCalledTimes(1);
  });

  it('falls back to a non-interactive span for auth-required when window.requestReauth is not installed', async () => {
    delete window.requestReauth;

    render(<SyncStatusPill isRunning={true} />);
    await act(async () => { bus.set('auth-required'); });

    const pill = screen.getByTestId('sync-status-pill');
    expect(pill.tagName).toBe('SPAN');
    expect(pill).toHaveTextContent('Sign in to save');
  });

  it.each(['synced', 'syncing', 'offline'])(
    'still renders a non-interactive span for status %s',
    async (status) => {
      window.requestReauth = vi.fn();

      render(<SyncStatusPill isRunning={true} />);
      await act(async () => { bus.set(status); });

      const pill = screen.getByTestId('sync-status-pill');
      expect(pill.tagName).toBe('SPAN');
    }
  );
});

describe('AdminTopbar: persistent "Sign in to save" control', () => {
  let bus;

  beforeEach(() => {
    bus = makeFakeSyncBus('synced');
    window.subscribeSyncStatus = bus.subscribe;
    window.requestReauth = vi.fn();
  });

  function renderTopbar() {
    return render(
      <window.AdminTopbar
        tournament={{ name: 'Kanto Open', competitions: [] }}
        onLogout={vi.fn()}
        onViewerMode={vi.fn()}
        hideRunningStrip
      />
    );
  }

  it('shows the "Sign in to save" button when sync status is auth-required, and it calls window.requestReauth', async () => {
    renderTopbar();
    expect(screen.queryByText('Sign in to save')).toBeNull();

    await act(async () => { bus.set('auth-required'); });

    const btn = screen.getByText('Sign in to save');
    fireEvent.click(btn);
    expect(window.requestReauth).toHaveBeenCalledTimes(1);
  });

  it('hides the button for synced/syncing/offline', async () => {
    renderTopbar();
    for (const status of ['synced', 'syncing', 'offline']) {
      await act(async () => { bus.set(status); });
      expect(screen.queryByText('Sign in to save')).toBeNull();
    }
  });

  it('hides the button again once the queue drains back to synced', async () => {
    renderTopbar();
    await act(async () => { bus.set('auth-required'); });
    expect(screen.queryByText('Sign in to save')).not.toBeNull();

    await act(async () => { bus.set('synced'); });
    expect(screen.queryByText('Sign in to save')).toBeNull();
  });
});

// bc-offl: a result finished offline is held on the device, and the court
// console moves on to the next match by itself, unmounting the editor whose
// banner said so. The topbar is the always-mounted home for the held count
// (operator decision 2026-09-27: a separate item after the connection pill,
// "Offline: 1 result not sent"; the pill reads "Reconnecting..." meanwhile).
describe('AdminTopbar: held results', () => {
  let bus, unsentBus, hadUnsent, origUnsent;

  beforeAll(() => {
    hadUnsent = 'subscribeUnsentWrites' in window;
    origUnsent = window.subscribeUnsentWrites;
  });
  afterAll(() => {
    if (hadUnsent) window.subscribeUnsentWrites = origUnsent;
    else delete window.subscribeUnsentWrites;
  });

  beforeEach(() => {
    bus = makeFakeSyncBus('synced');
    unsentBus = makeFakeSyncBus({ total: 0, terminal: 0, authBlocked: 0 });
    window.subscribeSyncStatus = bus.subscribe;
    window.subscribeUnsentWrites = unsentBus.subscribe;
    window.requestReauth = vi.fn();
  });

  function renderTopbar() {
    return render(
      <window.AdminTopbar
        tournament={{ name: 'Kanto Open', competitions: [] }}
        onLogout={vi.fn()}
        onViewerMode={vi.fn()}
        hideRunningStrip
      />
    );
  }
  const held = () => screen.queryByTestId('topbar-held');

  it('an offline held result shows "Offline: 1 result not sent" and the pill reads Reconnecting', async () => {
    renderTopbar();
    expect(held()).toBeNull();
    expect(screen.getByText('Connected')).toBeTruthy();

    await act(async () => {
      unsentBus.set({ total: 1, terminal: 1, authBlocked: 0 });
      bus.set('offline');
    });

    expect(held()).toHaveTextContent('Offline: 1 result not sent');
    expect(held().className).toContain('topbar__held--offline');
    // A button that opens the list; its text is the live region.
    expect(held().tagName).toBe('BUTTON');
    expect(held().querySelector('[role="status"]')).toHaveTextContent('Offline: 1 result not sent');
    expect(screen.queryByText('Connected')).toBeNull();
    expect(screen.getByText('Reconnecting…').className).toContain('topbar__conn--down');
  });

  it('counts results, plural, and only results when any are held', async () => {
    renderTopbar();
    await act(async () => {
      unsentBus.set({ total: 3, terminal: 2, authBlocked: 0 });
      bus.set('offline');
    });
    expect(held()).toHaveTextContent('Offline: 2 results not sent');
  });

  it('a held running update alone is a score update, and while sending it says so', async () => {
    renderTopbar();
    await act(async () => {
      unsentBus.set({ total: 1, terminal: 0, authBlocked: 0 });
      bus.set('syncing');
    });
    expect(held()).toHaveTextContent('Sending 1 score update…');
    expect(held().className).not.toContain('topbar__held--offline');
    // Only the write status 'offline' moves the pill.
    expect(screen.getByText('Connected')).toBeTruthy();
  });

  it('a server that keeps refusing reads "Not saving"', async () => {
    renderTopbar();
    await act(async () => {
      unsentBus.set({ total: 1, terminal: 1, authBlocked: 0 });
      bus.set('server-error');
    });
    expect(held()).toHaveTextContent('Not saving: 1 result');
    expect(held().className).toContain('topbar__held--error');
  });

  it('auth-required keeps its button and shows no indicator', async () => {
    renderTopbar();
    await act(async () => {
      unsentBus.set({ total: 1, terminal: 1, authBlocked: 1 });
      bus.set('auth-required');
    });
    expect(screen.getByText('Sign in to save')).toBeTruthy();
    expect(held()).toBeNull();
  });

  it('goes once the queue drains', async () => {
    renderTopbar();
    await act(async () => {
      unsentBus.set({ total: 1, terminal: 1, authBlocked: 0 });
      bus.set('offline');
    });
    expect(held()).not.toBeNull();
    await act(async () => {
      unsentBus.set({ total: 0, terminal: 0, authBlocked: 0 });
      bus.set('synced');
    });
    expect(held()).toBeNull();
    expect(screen.getByText('Connected')).toBeTruthy();
  });
});

// The held-writes indicator opens the list of what is held (HeldWritesPanel),
// where a write the server keeps refusing is discarded on its own, whatever it
// is: a running autosave, a lineup save and a hand-set winner have no editor of
// their own to offer it. A write only waiting is listed with no Discard.
describe('AdminTopbar: the held-writes list', () => {
  let bus, unsentBus, saved;
  const KEYS = ['subscribeUnsentWrites', 'API', 'confirmDialog', 'compMatches'];
  beforeAll(() => { saved = KEYS.map((k) => [k, k in window, window[k]]); });
  afterAll(() => { for (const [k, had, v] of saved) { if (had) window[k] = v; else delete window[k]; } });

  let held;
  beforeEach(() => {
    bus = makeFakeSyncBus('server-error');
    unsentBus = makeFakeSyncBus({ total: 3, terminal: 2, authBlocked: 0 });
    window.subscribeSyncStatus = bus.subscribe;
    window.subscribeUnsentWrites = unsentBus.subscribe;
    window.requestReauth = vi.fn();
    held = [
      { key: 'k1', compID: 'c1', matchID: 'Pool A-0', kind: 'score', terminal: false, keepsFailing: true, authBlocked: false },
      { key: 'k2', compID: 'c1', matchID: '', kind: 'lineup', terminal: true, keepsFailing: true, authBlocked: false, teamId: 't1', round: '1' },
      { key: 'k3', compID: 'c1', matchID: 'r1-m1', kind: 'override', terminal: true, keepsFailing: false, authBlocked: false },
    ];
    window.API = {
      heldWrites: vi.fn(() => held.map((h) => ({ ...h }))),
      discardHeldWrite: vi.fn((key) => { held = held.filter((h) => h.key !== key); return true; }),
    };
    window.confirmDialog = vi.fn().mockResolvedValue(true);
    window.compMatches = () => [{ id: 'Pool A-0', poolName: 'Pool A', phase: 'pool' }, { id: 'r1-m1', phase: 'bracket', matchNumber: 4 }];
  });

  function openList() {
    render(
      <window.AdminTopbar
        tournament={{ name: 'Kanto Open', competitions: [{ id: 'c1', name: 'Teams', participants: [{ id: 't1', name: 'Kodokan' }] }] }}
        onLogout={vi.fn()}
        onViewerMode={vi.fn()}
        hideRunningStrip
      />
    );
    fireEvent.click(screen.getByTestId('topbar-held'));
  }

  it('lists each held write by competition, match or team, what it is and where it stands', () => {
    openList();
    const rows = screen.getAllByTestId('held-write');
    expect(rows).toHaveLength(3);
    expect(rows[0]).toHaveTextContent('Teams · Pool A · Match 1');
    expect(rows[0]).toHaveTextContent('score update: the server keeps refusing it');
    expect(rows[1]).toHaveTextContent("Teams · Kodokan's lineup");
    expect(rows[1]).toHaveTextContent('team lineup: the server keeps refusing it');
    expect(rows[2]).toHaveTextContent('Teams · Match 4');
    expect(rows[2]).toHaveTextContent('winner set by hand: waiting to be sent');
    // Discard only where the server keeps refusing.
    expect(rows[0].querySelector('[data-testid="held-write-discard-one"]')).not.toBeNull();
    expect(rows[1].querySelector('[data-testid="held-write-discard-one"]')).not.toBeNull();
    expect(rows[2].querySelector('[data-testid="held-write-discard-one"]')).toBeNull();
  });

  it('discards only the one write, after a confirm naming what it is', async () => {
    openList();
    const lineupRow = screen.getAllByTestId('held-write')[1];
    await act(async () => { fireEvent.click(lineupRow.querySelector('[data-testid="held-write-discard-one"]')); });
    expect(window.confirmDialog).toHaveBeenCalledTimes(1);
    expect(window.confirmDialog.mock.calls[0][0].message).toContain('team lineup');
    expect(window.API.discardHeldWrite).toHaveBeenCalledWith('k2');
    expect(window.API.discardHeldWrite).toHaveBeenCalledTimes(1);
    expect(screen.getAllByTestId('held-write')).toHaveLength(2);
  });

  it('a confirm answered No discards nothing', async () => {
    window.confirmDialog = vi.fn().mockResolvedValue(false);
    openList();
    await act(async () => { fireEvent.click(screen.getAllByTestId('held-write-discard-one')[0]); });
    expect(window.API.discardHeldWrite).not.toHaveBeenCalled();
    expect(screen.getAllByTestId('held-write')).toHaveLength(3);
  });
});
