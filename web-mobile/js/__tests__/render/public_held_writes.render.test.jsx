// Public page held-writes indicator and panel render tests.
// The public page has no admin topbar, so when this device holds a write
// it cannot send, a held-writes count shows in the public page chrome
// and opens HeldWritesPanel.

import React from 'react';
import { render, screen, fireEvent, act, cleanup } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { HELD_WRITES_EMPTY } from '../../write_result.jsx';

let PublicHeldWrites;

const KEYS = [
  'subscribeSyncStatus',
  'subscribeUnsentWrites',
  'API',
  'confirmDialog',
  'compMatches',
  'Modal',
];

describe('PublicHeldWrites', () => {
  let saved;
  let syncListeners;
  let unsentListeners;
  let syncUnsub;
  let unsentUnsub;
  let heldList;

  const competitions = [
    {
      id: 'c1',
      name: 'Open Tournament',
      players: [{ id: 't1', name: 'Kodokan', dojo: 'Kodokan' }],
    },
  ];

  beforeAll(async () => {
    saved = KEYS.map((k) => [k, k in window, window[k]]);
    const mod = await import('../../held_writes_panel.jsx');
    PublicHeldWrites = mod.PublicHeldWrites;
  });

  afterAll(() => {
    for (const [k, had, v] of saved) {
      if (had) window[k] = v;
      else delete window[k];
    }
  });

  beforeEach(() => {
    syncListeners = new Set();
    unsentListeners = new Set();
    syncUnsub = vi.fn();
    unsentUnsub = vi.fn();
    heldList = [];

    window.subscribeSyncStatus = vi.fn((fn) => {
      syncListeners.add(fn);
      fn('synced');
      return vi.fn(() => {
        syncListeners.delete(fn);
        syncUnsub();
      });
    });

    window.subscribeUnsentWrites = vi.fn((fn) => {
      unsentListeners.add(fn);
      fn({ total: 0 });
      return vi.fn(() => {
        unsentListeners.delete(fn);
        unsentUnsub();
      });
    });

    window.API = {
      heldWrites: vi.fn(() => heldList.map((item) => ({ ...item }))),
      discardHeldWrite: vi.fn((key) => {
        heldList = heldList.filter((item) => item.key !== key);
        return true;
      }),
    };

    window.confirmDialog = vi.fn().mockResolvedValue(true);
    window.compMatches = () => [
      { id: 'm1', matchNumber: 1, phase: 'bracket' },
      { id: 'm2', matchNumber: 2, phase: 'bracket' },
    ];
    window.Modal = ({ title, onClose, children }) => (
      <div data-testid="modal">
        <h2>{title}</h2>
        <button type="button" onClick={onClose} data-testid="modal-close">Close</button>
        {children}
      </div>
    );
  });

  afterEach(() => {
    cleanup();
  });

  it('renders nothing with data-testid public-held when a write is only in flight (held is false)', () => {
    heldList = [
      {
        key: 'k-flight',
        compID: 'c1',
        matchID: 'm1',
        kind: 'score',
        terminal: false,
        keepsFailing: false,
        authBlocked: false,
        held: false,
        where: 'Match 1',
      },
    ];

    render(<PublicHeldWrites competitions={competitions} />);
    expect(screen.queryByTestId('public-held')).toBeNull();
  });

  it('renders 1 change not sent for 1 held item, and 2 changes not sent for 2 held items and 1 in flight', () => {
    heldList = [
      {
        key: 'k1',
        compID: 'c1',
        matchID: 'm1',
        kind: 'score',
        terminal: false,
        keepsFailing: false,
        authBlocked: false,
        held: true,
        where: 'Match 1',
      },
    ];

    const { rerender } = render(<PublicHeldWrites competitions={competitions} />);
    const button = screen.getByTestId('public-held');
    expect(button).toHaveTextContent('1 change not sent');

    heldList = [
      {
        key: 'k1',
        compID: 'c1',
        matchID: 'm1',
        kind: 'score',
        terminal: false,
        keepsFailing: false,
        authBlocked: false,
        held: true,
        where: 'Match 1',
      },
      {
        key: 'k2',
        compID: 'c1',
        matchID: '',
        kind: 'lineup',
        terminal: true,
        keepsFailing: true,
        authBlocked: false,
        held: true,
        teamId: 't1',
        round: '1',
        where: 'Kodokan',
      },
      {
        key: 'k-flight',
        compID: 'c1',
        matchID: 'm2',
        kind: 'score',
        terminal: false,
        keepsFailing: false,
        authBlocked: false,
        held: false,
        where: 'Match 2',
      },
    ];

    act(() => {
      for (const fn of unsentListeners) fn({ total: 3 });
    });

    rerender(<PublicHeldWrites competitions={competitions} />);
    expect(screen.getByTestId('public-held')).toHaveTextContent('2 changes not sent');
  });

  it('appears after subscription fires and disappears when the held item is gone', async () => {
    heldList = [];
    render(<PublicHeldWrites competitions={competitions} />);
    expect(screen.queryByTestId('public-held')).toBeNull();

    heldList = [
      {
        key: 'k1',
        compID: 'c1',
        matchID: 'm1',
        kind: 'score',
        terminal: false,
        keepsFailing: true,
        authBlocked: false,
        held: true,
        where: 'Match 1',
      },
    ];

    await act(async () => {
      for (const fn of unsentListeners) fn({ total: 1 });
    });
    expect(screen.getByTestId('public-held')).toHaveTextContent('1 change not sent');

    heldList = [];
    await act(async () => {
      for (const fn of unsentListeners) fn({ total: 0 });
    });
    expect(screen.queryByTestId('public-held')).toBeNull();
  });

  it('clicking the button opens the list with one row per outbox item, showing discard only when keepsFailing is true', async () => {
    heldList = [
      {
        key: 'k1',
        compID: 'c1',
        matchID: 'm1',
        kind: 'score',
        terminal: false,
        keepsFailing: true,
        authBlocked: false,
        held: true,
        where: 'Match 1',
      },
      {
        key: 'k2',
        compID: 'c1',
        matchID: 'm2',
        kind: 'score',
        terminal: false,
        keepsFailing: false,
        authBlocked: false,
        held: true,
        where: 'Match 2',
      },
    ];

    render(<PublicHeldWrites competitions={competitions} />);
    const button = screen.getByTestId('public-held');
    await act(async () => {
      fireEvent.click(button);
    });

    expect(screen.getByTestId('held-writes')).toBeTruthy();
    const rows = screen.getAllByTestId('held-write');
    expect(rows).toHaveLength(2);

    expect(rows[0].querySelector('[data-testid="held-write-discard-one"]')).not.toBeNull();
    expect(rows[1].querySelector('[data-testid="held-write-discard-one"]')).toBeNull();
  });

  it('clicking Discard after confirm resolves true calls window.API.discardHeldWrite with that item key', async () => {
    heldList = [
      {
        key: 'k-failing',
        compID: 'c1',
        matchID: 'm1',
        kind: 'score',
        terminal: false,
        keepsFailing: true,
        authBlocked: false,
        held: true,
        where: 'Match 1',
      },
    ];

    render(<PublicHeldWrites competitions={competitions} />);
    await act(async () => {
      fireEvent.click(screen.getByTestId('public-held'));
    });

    const discardBtn = screen.getByTestId('held-write-discard-one');
    await act(async () => {
      fireEvent.click(discardBtn);
    });

    expect(window.confirmDialog).toHaveBeenCalledTimes(1);
    expect(window.API.discardHeldWrite).toHaveBeenCalledWith('k-failing');
    expect(window.API.discardHeldWrite).toHaveBeenCalledTimes(1);
  });

  it('unmounting calls both unsubscribe functions', () => {
    heldList = [
      {
        key: 'k1',
        compID: 'c1',
        matchID: 'm1',
        kind: 'score',
        terminal: false,
        keepsFailing: false,
        authBlocked: false,
        held: true,
        where: 'Match 1',
      },
    ];

    const { unmount } = render(<PublicHeldWrites competitions={competitions} />);
    expect(syncUnsub).not.toHaveBeenCalled();
    expect(unsentUnsub).not.toHaveBeenCalled();

    unmount();
    expect(syncUnsub).toHaveBeenCalledTimes(1);
    expect(unsentUnsub).toHaveBeenCalledTimes(1);
  });

  it('renders 1 result not sent when a held item is a finished result', () => {
    heldList = [
      {
        key: 'k-finished',
        compID: 'c1',
        matchID: 'm1',
        kind: 'score',
        terminal: true,
        keepsFailing: false,
        authBlocked: false,
        held: true,
        where: 'Match 1',
      },
    ];

    render(<PublicHeldWrites competitions={competitions} />);
    expect(screen.getByTestId('public-held')).toHaveTextContent('1 result not sent');
  });

  it('keeps panel mounted with empty text when held count drops to 0, and does not pop up afterward until tapped', async () => {
    heldList = [
      {
        key: 'k1',
        compID: 'c1',
        matchID: 'm1',
        kind: 'score',
        terminal: false,
        keepsFailing: false,
        authBlocked: false,
        held: true,
        where: 'Match 1',
      },
    ];

    render(<PublicHeldWrites competitions={competitions} />);
    const badge = screen.getByTestId('public-held');
    await act(async () => {
      fireEvent.click(badge);
    });

    expect(screen.getByTestId('modal')).toBeTruthy();
    expect(screen.getByTestId('held-writes')).toBeTruthy();

    heldList = [];
    await act(async () => {
      for (const fn of unsentListeners) fn({ total: 0 });
    });

    expect(screen.queryByTestId('public-held')).toBeNull();
    expect(screen.getByTestId('modal')).toBeTruthy();
    expect(screen.getByText(HELD_WRITES_EMPTY)).toBeTruthy();

    await act(async () => {
      fireEvent.click(screen.getByTestId('modal-close'));
    });
    expect(screen.queryByTestId('modal')).toBeNull();

    heldList = [
      {
        key: 'k2',
        compID: 'c1',
        matchID: 'm1',
        kind: 'score',
        terminal: false,
        keepsFailing: false,
        authBlocked: false,
        held: true,
        where: 'Match 1',
      },
    ];
    await act(async () => {
      for (const fn of unsentListeners) fn({ total: 1 });
    });

    expect(screen.getByTestId('public-held')).toBeTruthy();
    expect(screen.queryByTestId('modal')).toBeNull();

    await act(async () => {
      fireEvent.click(screen.getByTestId('public-held'));
    });
    expect(screen.getByTestId('modal')).toBeTruthy();
  });
});
