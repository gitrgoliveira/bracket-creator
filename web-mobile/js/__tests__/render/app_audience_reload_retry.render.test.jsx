// bc-hnam: when the page's read audience changes, the competitions it held are
// dropped and read again. A read that fails (the venue Wi-Fi drops) must not
// leave the page showing the emptied tournament as if its competitions were
// gone: the spinner stays, says the page is trying again, and the page reads
// again until a read lands.
import React from 'react';
import { act } from '@testing-library/react';
import { describe, it, expect, vi, afterAll } from 'vitest';
import { mountApp, settle, propsProbe } from '../helpers/mount_app.js';
import { rememberRevealKey } from '../../reveal_key.jsx';

const page = propsProbe(() => <div data-testid="viewer-competition" />);

const detail = {
  config: { id: 'c1', name: 'Open', kind: 'individual', format: 'mixed', status: 'pools', courts: ['A'] },
  pools: [],
  poolMatches: [],
  standings: {},
  bracket: null,
};

const failing = { on: false };

const STUBBED_GLOBALS = {
  ViewerCompetition: page.Component,
  API: {
    fetchTournament: vi.fn(async () => {
      if (failing.on) throw new Error('network down');
      return { name: 'T', mode: 'file', courts: ['A'] };
    }),
    fetchCompetitions: vi.fn(async () => [{ id: 'c1', name: 'Open' }]),
    fetchAuthConfig: vi.fn(async () => ({ mode: 'file', resetEnabled: true })),
    fetchAnnouncements: vi.fn(async () => []),
    fetchCompetitionDetails: vi.fn(async () => detail),
    subscribeToEvents: vi.fn(() => () => {}),
    reconnectEvents: vi.fn(),
    resumeAfterAuth: vi.fn(),
  },
};

let unmount;

afterAll(() => { vi.useRealTimers(); unmount(); window.localStorage.clear(); });

const shown = () => document.querySelector('[data-testid="viewer-competition"]') !== null;

describe('a failed read as a new audience is tried again (bc-hnam)', () => {
  it('keeps the spinner through the failure and shows the page once a read lands', async () => {
    ({ unmount } = await mountApp({ path: '/competition/c1', globals: STUBBED_GLOBALS }));
    expect(shown()).toBe(true);

    vi.useFakeTimers({ shouldAdvanceTime: true });
    // load() logs each failure; that log is expected here.
    const logged = vi.spyOn(console, 'error').mockImplementation(() => {});
    try {
      failing.on = true;
      const reads = STUBBED_GLOBALS.API.fetchTournament.mock.calls.length;
      await act(async () => { rememberRevealKey('c1', 'b'.repeat(64)); });
      await settle();
      await act(async () => { vi.advanceTimersByTime(300); });
      await settle();

      expect(STUBBED_GLOBALS.API.fetchTournament.mock.calls.length).toBe(reads + 1);
      expect(shown()).toBe(false);
      expect(document.body.textContent).toContain('Connection interrupted. Trying again');

      // Still down at the next try: still the spinner, never the emptied page.
      await act(async () => { vi.advanceTimersByTime(3000); });
      await settle();
      expect(STUBBED_GLOBALS.API.fetchTournament.mock.calls.length).toBe(reads + 2);
      expect(shown()).toBe(false);

      // The network is back: the next try lands and the page shows again.
      failing.on = false;
      await act(async () => { vi.advanceTimersByTime(3000); });
      await settle();
      expect(shown()).toBe(true);
      expect(document.body.textContent).not.toContain('Trying again');
      expect(logged.mock.calls.map((c) => c[0])).toEqual(['Failed to load tournament', 'Failed to load tournament']);
    } finally {
      logged.mockRestore();
    }
  });
});
