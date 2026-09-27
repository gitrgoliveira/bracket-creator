// bc-sync: an edit still inside the autosave debounce is unsent work.
//
// The pill used to read "Synced" through the whole 300ms window, and a reload
// in that window cancelled the timer and lost the edit. The hook now
// registers the pending edit with api_client (API.notePendingEdit) from the
// first tap until the write is dispatched, and on pagehide (or the tab being
// hidden) writes a pending edit at once with `durable: true`, which puts it
// straight into the persisted outbox.

import React from 'react';
import { render, act, fireEvent, screen, cleanup } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { AUTOSAVE_DEBOUNCE_MS } from '../../admin_scoring_autosave.jsx';

// The log of what happened, in order, so a test can say whether the write was
// dispatched before the pending edit was released, and whether it was durable.
let events;

const STUBBED_GLOBALS = {
  isHikiwake: () => false,
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  isKikenDecision: () => false,
  isTextEntry: () => false,
  isInteractiveTarget: () => false,
  confirmDialog: vi.fn().mockResolvedValue(true),
  resolveRoundIndex: () => 0,
  API: {
    fetchCompetitionDetails: vi.fn().mockResolvedValue(null),
    recordScore: vi.fn(),
    recordDaihyosen: vi.fn(),
    removeDaihyosen: vi.fn(),
    putMatchLineup: vi.fn(),
    recordDecision: vi.fn(),
    notePendingEdit: vi.fn(),
  },
  AdminLineupHelpers: { rosterFor: vi.fn().mockReturnValue([]) },
  compMatches: () => [],
  Term: ({ children }) => <span>{children}</span>,
  GlossaryHint: ({ name }) => <span title={name} />,
};

let restoreGlobals;
let ScoreEditorModal;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  await import('../../admin_scoring_modal.jsx');
  ScoreEditorModal = window.ScoreEditorModal;
});

afterAll(() => restoreGlobals());

beforeEach(() => {
  events = [];
  window.API.recordScore.mockReset();
  window.API.recordScore.mockImplementation((_c, _m, patch) => {
    events.push(`write:${patch.status}${patch.durable ? ':durable' : ''}`);
    return Promise.resolve(undefined);
  });
  window.API.notePendingEdit.mockReset();
  window.API.notePendingEdit.mockImplementation((_token, on) => { events.push(on ? 'pending' : 'released'); });
  vi.useFakeTimers();
});

afterEach(() => { cleanup(); vi.useRealTimers(); });

const running = () => ({
  id: 'm-run', status: 'running', phase: 'pool', poolName: 'Pool 1', court: 'A',
  sideA: { id: 'p1', name: 'Yamada' }, sideB: { id: 'p2', name: 'Tanaka' },
});

async function mount(match) {
  let utils;
  await act(async () => {
    utils = render(<ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={(p) => window.API.recordScore('comp1', match.id, p, '', match)} password="" />);
  });
  return utils;
}

const tapM = () => act(async () => { fireEvent.click(screen.getAllByText('M')[0]); });
const wait = (ms) => act(async () => { vi.advanceTimersByTime(ms); });

describe('bc-sync: the autosave reports a pending edit', () => {
  it('registers it on the first tap and releases it only after the write is dispatched', async () => {
    await mount(running());
    await tapM();
    expect(events).toEqual(['pending']);
    await wait(AUTOSAVE_DEBOUNCE_MS + 50);
    expect(events).toEqual(['pending', 'write:running', 'released']);
  });

  it('every pending registration uses the same token for one editor', async () => {
    await mount(running());
    await tapM();
    await wait(100);
    await tapM();
    await wait(AUTOSAVE_DEBOUNCE_MS + 50);
    const tokens = window.API.notePendingEdit.mock.calls.map(([t]) => t);
    expect(new Set(tokens).size).toBe(1);
    // A second tap inside the window re-arms without releasing in between.
    expect(events).toEqual(['pending', 'pending', 'write:running', 'released']);
  });

  it('writes it when the editor unmounts with the edit still pending, then releases it', async () => {
    // Operator ruling 2026-09-27: every way out of the editor keeps the tap.
    const { unmount } = await mount(running());
    await tapM();
    await act(async () => { unmount(); });
    expect(events).toEqual(['pending', 'write:running', 'released']);
    // Written once: no timer is left to fire it again.
    await wait(AUTOSAVE_DEBOUNCE_MS + 50);
    expect(events).toEqual(['pending', 'write:running', 'released']);
  });

  it('never registers one on a scheduled match', async () => {
    await mount({ ...running(), status: 'scheduled' });
    await tapM();
    await wait(AUTOSAVE_DEBOUNCE_MS + 50);
    expect(window.API.notePendingEdit).not.toHaveBeenCalled();
  });
});

describe('bc-sync: a pending edit survives the page going away', () => {
  it('pagehide writes it at once, marked durable', async () => {
    await mount(running());
    await tapM();
    await act(async () => { window.dispatchEvent(new Event('pagehide')); });
    // Released only after the write is dispatched, so the status never reads
    // "synced" in between.
    expect(events).toEqual(['pending', 'write:running:durable', 'released']);
    // And the debounce timer is gone: no second write when it would have fired.
    await wait(AUTOSAVE_DEBOUNCE_MS + 50);
    expect(window.API.recordScore).toHaveBeenCalledTimes(1);
  });

  it('the tab being hidden writes it the same way', async () => {
    await mount(running());
    await tapM();
    const vis = Object.getOwnPropertyDescriptor(Document.prototype, 'visibilityState');
    Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => 'hidden' });
    try {
      await act(async () => { document.dispatchEvent(new Event('visibilitychange')); });
    } finally {
      delete document.visibilityState;
      if (vis) Object.defineProperty(Document.prototype, 'visibilityState', vis);
    }
    expect(events).toContain('write:running:durable');
  });

  it('with nothing pending, pagehide sends nothing', async () => {
    await mount(running());
    await tapM();
    await wait(AUTOSAVE_DEBOUNCE_MS + 50);
    window.API.recordScore.mockClear();
    await act(async () => { window.dispatchEvent(new Event('pagehide')); });
    expect(window.API.recordScore).not.toHaveBeenCalled();
  });
});
