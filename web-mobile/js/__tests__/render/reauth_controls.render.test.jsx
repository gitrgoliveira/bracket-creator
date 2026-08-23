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
