import React from 'react';
import { act } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';
import { mountApp, settle, propsProbe } from '../helpers/mount_app.js';
import { PACE_TICK_MS } from '../../court_pace.jsx';

// On a competition page the pace tick refetches the tournament every
// PACE_TICK_MS, so shared-court pace drift stays current. That refetch goes
// through the SSE effect's gates (maybeLoad, the sign-in prompt, a hidden
// tab) and lands after a random delay, so the open tabs do not all ask at the
// same moment.

const page = propsProbe(() => <div data-testid="viewer-competition" />);

const STUBBED_GLOBALS = {
  ViewerCompetition: page.Component,
  API: {
    fetchTournament: vi.fn(async () => ({ name: 'T', courts: ['A'] })),
    fetchCompetitions: vi.fn(async () => [{ id: 'c1', name: 'Open', date: '2026-11-01' }]),
    fetchAuthConfig: vi.fn(async () => ({ mode: 'file', resetEnabled: true })),
    fetchAnnouncements: vi.fn(async () => []),
    fetchCompetitionDetails: vi.fn(async (id) => ({
      config: { id, name: 'Open', kind: 'individual', format: 'league', status: 'pools', courts: ['A'], date: '2026-11-01' },
      pools: [], poolMatches: [], standings: {}, bracket: null,
    })),
    subscribeToEvents: vi.fn(() => () => {}),
    reconnectEvents: vi.fn(),
    resumeAfterAuth: vi.fn(),
  },
};

// Every live interval the app sets on the pace period, run by hand.
const paceIntervals = new Map();
const realSetInterval = globalThis.setInterval;
const realClearInterval = globalThis.clearInterval;
let intervalSpy;
let clearSpy;
let unmount;

const loads = () => window.API.fetchTournament.mock.calls.length;
const tick = async (random = 0) => {
  const spy = vi.spyOn(Math, 'random').mockReturnValue(random);
  try {
    await act(async () => { [...paceIntervals.values()].forEach((fn) => fn()); });
  } finally {
    spy.mockRestore();
  }
  await settle();
};

beforeAll(async () => {
  intervalSpy = vi.spyOn(globalThis, 'setInterval').mockImplementation((fn, ms, ...rest) => {
    const id = realSetInterval(fn, ms, ...rest);
    if (ms === PACE_TICK_MS) paceIntervals.set(id, fn);
    return id;
  });
  clearSpy = vi.spyOn(globalThis, 'clearInterval').mockImplementation((id) => {
    paceIntervals.delete(id);
    return realClearInterval(id);
  });
  ({ unmount } = await mountApp({ path: '/competition/c1', globals: STUBBED_GLOBALS }));
});

afterAll(() => {
  if (unmount) unmount();
  intervalSpy.mockRestore();
  clearSpy.mockRestore();
});

describe('the pace tick refetch', () => {
  it('refetches the tournament once a tick, after a random delay', async () => {
    expect(page.props).toBeTruthy();
    const before = loads();
    await tick(0.5);
    expect(loads(), 'not at the tick itself').toBe(before);
    await act(async () => { await new Promise((r) => setTimeout(r, 1100)); });
    expect(loads(), 'within the jitter window').toBe(before + 1);
  });

  it('skips a hidden tab', async () => {
    const hidden = vi.spyOn(document, 'hidden', 'get').mockReturnValue(true);
    try {
      const before = loads();
      await tick();
      expect(loads()).toBe(before);
    } finally {
      hidden.mockRestore();
    }
  });

  it('skips while the sign-in prompt is open', async () => {
    const start = loads();
    await tick();
    expect(loads(), 'a tick with no prompt refetches').toBe(start + 1);
    const before = loads();
    await act(async () => { page.props.onAdminClick(); });
    await settle();
    await tick();
    expect(loads()).toBe(before);
  });
});
