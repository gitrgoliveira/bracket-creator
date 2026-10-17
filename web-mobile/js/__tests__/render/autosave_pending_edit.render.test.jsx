// bc-sync: an edit still inside the autosave debounce is unsent work.
//
// The pill used to read "Synced" through the whole 300ms window, and a reload
// in that window cancelled the timer and lost the edit. The hook now
// registers the pending edit with api_client (API.notePendingEdit) from the
// first tap until the write is dispatched, and on pagehide (or the tab being
// hidden) writes a pending edit at once: the outbox stores it in that tick
// (bc-evts).

import React from 'react';
import { render, act, fireEvent, screen, cleanup } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { AUTOSAVE_DEBOUNCE_MS, useDebouncedRunningWrite } from '../../admin_scoring_autosave.jsx';
import { makeFakeOutbox } from '../helpers/fake_outbox.js';

// The log of what happened, in order, so a test can say whether the write was
// dispatched before the pending edit was released.
let events;

const STUBBED_GLOBALS = {
  isHikiwake: () => false,
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  isKikenDecision: () => false,
  isTextEntry: () => false,
  isInteractiveTarget: () => false,
  confirmDialog: vi.fn().mockResolvedValue(true),
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
    events.push(`write:${patch.status}`);
    return Promise.resolve(undefined);
  });
  window.API.notePendingEdit.mockReset();
  window.API.notePendingEdit.mockImplementation((_token, on) => { events.push(on ? 'pending' : 'released'); });
  vi.useFakeTimers();
});

afterEach(() => { cleanup(); vi.useRealTimers(); });

// bc-evts stage 4: the individual and engi editors no longer autosave (a
// running match is scored by taps, below); the team editor still does until
// stage 5. The hook is pinned here on its own, in a host built the way the
// team editor builds it: refs refreshed every render, an edit calling
// markDirty.
function AutosaveHost({ running }) {
  const isRunningRef = React.useRef(running);
  const buildPatchRef = React.useRef(null);
  const onSubmitRef = React.useRef(null);
  const seenStampRef = React.useRef(0);
  const [count, setCount] = React.useState(0);
  isRunningRef.current = running;
  buildPatchRef.current = (status) => ({ count, status });
  onSubmitRef.current = (patch) => window.API.recordScore('comp1', 'm-run', patch, '', null);
  const { markDirty } = useDebouncedRunningWrite({ isRunningRef, buildPatchRef, onSubmitRef, onWriteResult: () => {}, seenStampRef });
  return <button type="button" data-testid="edit" onClick={() => { setCount((n) => n + 1); markDirty(); }}>edit</button>;
}

async function mountHost(running = true) {
  let utils;
  await act(async () => { utils = render(<AutosaveHost running={running} />); });
  return utils;
}

const edit = () => act(async () => { fireEvent.click(screen.getByTestId('edit')); });
const wait = (ms) => act(async () => { vi.advanceTimersByTime(ms); });

describe('bc-sync: the autosave reports a pending edit', () => {
  it('registers it on the first tap and releases it only after the write is dispatched', async () => {
    await mountHost();
    await edit();
    expect(events).toEqual(['pending']);
    await wait(AUTOSAVE_DEBOUNCE_MS + 50);
    expect(events).toEqual(['pending', 'write:running', 'released']);
  });

  it('every pending registration uses the same token for one editor', async () => {
    await mountHost();
    await edit();
    await wait(100);
    await edit();
    await wait(AUTOSAVE_DEBOUNCE_MS + 50);
    const tokens = window.API.notePendingEdit.mock.calls.map(([t]) => t);
    expect(new Set(tokens).size).toBe(1);
    // A second tap inside the window re-arms without releasing in between.
    expect(events).toEqual(['pending', 'pending', 'write:running', 'released']);
  });

  it('writes it when the editor unmounts with the edit still pending, then releases it', async () => {
    // Operator ruling 2026-09-27: every way out of the editor keeps the tap.
    const { unmount } = await mountHost();
    await edit();
    await act(async () => { unmount(); });
    expect(events).toEqual(['pending', 'write:running', 'released']);
    // Written once: no timer is left to fire it again.
    await wait(AUTOSAVE_DEBOUNCE_MS + 50);
    expect(events).toEqual(['pending', 'write:running', 'released']);
  });

  it('never registers one on a scheduled match', async () => {
    await mountHost(false);
    await edit();
    await wait(AUTOSAVE_DEBOUNCE_MS + 50);
    expect(window.API.notePendingEdit).not.toHaveBeenCalled();
  });
});

describe('bc-sync: a pending edit survives the page going away', () => {
  it('pagehide writes it at once', async () => {
    await mountHost();
    await edit();
    await act(async () => { window.dispatchEvent(new Event('pagehide')); });
    // Released only after the write is dispatched, so the status never reads
    // "synced" in between.
    expect(events).toEqual(['pending', 'write:running', 'released']);
    // And the debounce timer is gone: no second write when it would have fired.
    await wait(AUTOSAVE_DEBOUNCE_MS + 50);
    expect(window.API.recordScore).toHaveBeenCalledTimes(1);
  });

  it('the tab being hidden writes it the same way', async () => {
    await mountHost();
    await edit();
    const vis = Object.getOwnPropertyDescriptor(Document.prototype, 'visibilityState');
    Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => 'hidden' });
    try {
      await act(async () => { document.dispatchEvent(new Event('visibilitychange')); });
    } finally {
      delete document.visibilityState;
      if (vis) Object.defineProperty(Document.prototype, 'visibilityState', vis);
    }
    expect(events).toContain('write:running');
  });

  it('with nothing pending, pagehide sends nothing', async () => {
    await mountHost();
    await edit();
    await wait(AUTOSAVE_DEBOUNCE_MS + 50);
    window.API.recordScore.mockClear();
    await act(async () => { window.dispatchEvent(new Event('pagehide')); });
    expect(window.API.recordScore).not.toHaveBeenCalled();
  });
});


// bc-evts stages 3 and 4: the individual and engi editors hold no pending
// edit. A tap on a running individual or engi match is handed to the outbox as it is made (which
// persists it at once and owns the sync pill), so nothing is registered,
// nothing waits for a debounce, and the page going away sends nothing more.
describe('the tap editors have no pending edit to report', () => {
  beforeEach(() => { Object.assign(window.API, makeFakeOutbox().api); });
  const individual = () => ({
    id: 'm-ind', status: 'running', phase: 'pool', poolName: 'Pool 1', court: 'A',
    sideA: { id: 'p1', name: 'Yamada' }, sideB: { id: 'p2', name: 'Tanaka' },
  });
  const engi = () => ({
    id: 'm-run', status: 'running', phase: 'pool', poolName: 'Pool 1', court: 'A', compEngi: true,
    sideA: { id: 'p1', name: 'Aka One - Aka Two' }, sideB: { id: 'p2', name: 'Shiro One - Shiro Two' },
  });
  const mount = (match) => act(async () => {
    render(<ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={(p) => window.API.recordScore('comp1', match.id, p, '', match)} password="" />);
  });

  it.each([
    ['individual', individual, () => fireEvent.click(screen.getAllByText('M')[0])],
    ['engi', engi, () => fireEvent.click(screen.getByTestId('engi-aka-inc'))],
  ])('%s: a tap goes to the outbox at once and registers no pending edit', async (_kind, match, tap) => {
    await mount(match());
    await act(async () => { tap(); });
    expect(window.API.recordTap).toHaveBeenCalledTimes(1);
    expect(window.API.notePendingEdit).not.toHaveBeenCalled();
    await act(async () => { window.dispatchEvent(new Event('pagehide')); });
    await wait(AUTOSAVE_DEBOUNCE_MS + 50);
    expect(events).toEqual([]);
  });
});
