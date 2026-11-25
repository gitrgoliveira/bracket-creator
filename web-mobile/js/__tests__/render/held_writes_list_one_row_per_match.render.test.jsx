// The held-writes list names a match once, as the corner counts it (S6 of
// the public self-run walk): offline after a Finish, the outbox holds the
// match's score update, its tap and the finished result, and the corner says
// "1 result not sent". The list it opens showed three rows for that one match;
// it now shows one, named by the finished result. A write the server keeps
// refusing keeps a row of its own, with its Discard, since it is discarded
// alone.

import React from 'react';
import { render, screen, fireEvent, act, cleanup } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';

let PublicHeldWrites;
let heldWriteRows;

const KEYS = ['subscribeSyncStatus', 'subscribeUnsentWrites', 'API', 'confirmDialog', 'compMatches', 'Modal'];

const competitions = [{ id: 'c1', name: 'Offline Final', players: [] }];

const waiting = (key, kind, extra = {}) => ({
  key, compID: 'c1', matchID: 'm1', kind, terminal: false,
  keepsFailing: false, authBlocked: false, held: true, ...extra,
});

describe('the held-writes list: one row per match', () => {
  let saved;
  let heldList;

  beforeAll(async () => {
    saved = KEYS.map((k) => [k, k in window, window[k]]);
    const mod = await import('../../held_writes_panel.jsx');
    PublicHeldWrites = mod.PublicHeldWrites;
    heldWriteRows = mod.heldWriteRows;
  });

  afterAll(() => {
    for (const [k, had, v] of saved) {
      if (had) window[k] = v;
      else delete window[k];
    }
  });

  beforeEach(() => {
    heldList = [];
    window.subscribeSyncStatus = vi.fn((fn) => { fn('offline'); return () => {}; });
    window.subscribeUnsentWrites = vi.fn((fn) => { fn({ total: heldList.length }); return () => {}; });
    window.API = {
      heldWrites: vi.fn(() => heldList.map((item) => ({ ...item }))),
      discardHeldWrite: vi.fn((key) => { heldList = heldList.filter((item) => item.key !== key); return true; }),
    };
    window.confirmDialog = vi.fn().mockResolvedValue(true);
    window.compMatches = () => [{ id: 'm1', matchNumber: 1, phase: 'bracket' }, { id: 'm2', matchNumber: 2, phase: 'bracket' }];
    window.Modal = ({ title, onClose, children }) => (
      <div data-testid="modal" role="dialog" aria-label={title}>
        <button type="button" onClick={onClose}>Close</button>
        {children}
      </div>
    );
  });

  afterEach(() => { cleanup(); });

  async function openList() {
    render(<PublicHeldWrites competitions={competitions} />);
    await act(async () => { fireEvent.click(screen.getByTestId('public-held')); });
    return screen.getAllByTestId('held-write');
  }

  it('offline after a Finish: the corner says 1 result and the list shows that match once, as the finished result', async () => {
    heldList = [
      waiting('k-score', 'score'),
      waiting('k-tap', 'evt'),
      waiting('k-finish', 'score', { terminal: true }),
    ];
    const rows = await openList();
    expect(screen.getByTestId('public-held')).toHaveTextContent('1 result not sent');
    expect(rows).toHaveLength(1);
    expect(rows[0]).toHaveTextContent('Offline Final · Match 1');
    expect(rows[0]).toHaveTextContent('Finished result: waiting to be sent');
    expect(rows[0].querySelector('[data-testid="held-write-discard-one"]')).toBeNull();
  });

  it('two matches waiting are two rows; a match with only changes says how many', () => {
    const rows = heldWriteRows([
      waiting('a1', 'score'),
      waiting('b1', 'evt', { matchID: 'm2' }),
      waiting('a2', 'evt'),
      waiting('b2', 'evt', { matchID: 'm2', authBlocked: true }),
    ]);
    expect(rows.map((r) => r.key)).toEqual(['a1', 'b1']);
    expect(rows[0].line).toBe('2 changes: waiting to be sent');
    expect(rows[1].line).toBe('2 taps: waiting for you to sign in');
  });

  it('a write the server keeps refusing keeps its own row and its Discard, which discards that write alone', async () => {
    heldList = [
      waiting('k-tap', 'evt'),
      waiting('k-refused', 'score', { terminal: true, keepsFailing: true }),
      waiting('k-tap2', 'evt'),
    ];
    const rows = await openList();
    expect(rows).toHaveLength(2);
    expect(rows[0]).toHaveTextContent('2 taps: waiting to be sent');
    expect(rows[0].querySelector('[data-testid="held-write-discard-one"]')).toBeNull();
    expect(rows[1]).toHaveTextContent('Finished result: the server keeps refusing it');
    await act(async () => { fireEvent.click(rows[1].querySelector('[data-testid="held-write-discard-one"]')); });
    expect(window.API.discardHeldWrite).toHaveBeenCalledWith('k-refused');
    expect(window.API.discardHeldWrite).toHaveBeenCalledTimes(1);
  });

  it('a match lineup save is its own row, so the match row keeps naming the match', () => {
    const rows = heldWriteRows([
      waiting('k-lineup', 'lineup', { teamId: 't1' }),
      waiting('k-tap', 'evt'),
    ]);
    expect(rows.map((r) => r.key)).toEqual(['k-lineup', 'k-tap']);
    expect(rows[0].line).toBe('Team lineup: waiting to be sent');
    expect(rows[1].line).toBe('Tap: waiting to be sent');
  });

  it('a write about no match is its own row, named as before', () => {
    const rows = heldWriteRows([
      waiting('k-lineup', 'lineup', { matchID: '', teamId: 't1' }),
      waiting('k-checkin', 'checkin', { matchID: '' }),
    ]);
    expect(rows).toHaveLength(2);
    expect(rows[0].line).toBe('Team lineup: waiting to be sent');
    expect(rows[1].line).toBe('Check-in: waiting to be sent');
  });
});
