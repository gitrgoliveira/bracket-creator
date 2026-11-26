// An event reaching the page while the court console is shown asks the
// tournament list gate for nothing: the console reads its own feed, the list
// load is skipped, and a gate fetch that answers nothing covers no seq, so
// each event would schedule a flight and leave its 30 s deadline timer in the
// pool for as long as the bout is scored.
import React from 'react';
import { act } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';
import { mountApp } from '../helpers/mount_app.js';
import { HELD_PUSH_WAIT_MS, FETCH_DEADLINE_MS } from '../../patch.jsx';

const sse = { emit: null };

const STUBBED_GLOBALS = {
  AdminApp: () => <div data-testid="admin-probe" />,
  API: {
    fetchTournament: vi.fn(async () => ({ name: 'T', courts: ['A'] })),
    fetchCompetitions: vi.fn(async () => []),
    fetchAuthConfig: vi.fn(async () => ({ mode: 'file', resetEnabled: true })),
    fetchAnnouncements: vi.fn(async () => []),
    fetchCompetitionDetails: vi.fn(async () => null),
    subscribeToEvents: vi.fn((onEvent) => { sse.emit = onEvent; return () => {}; }),
    reconnectEvents: vi.fn(),
    resumeAfterAuth: vi.fn(),
  },
};

let unmount;
let timeouts;
let random;

beforeAll(async () => {
  random = vi.spyOn(Math, 'random').mockReturnValue(0);
  window.localStorage.setItem('bc_authed', 'true');
  window.localStorage.setItem('bc_password', 'pw');
  ({ unmount } = await mountApp({ path: '/admin/shiaijo/A', globals: STUBBED_GLOBALS }));
  timeouts = vi.spyOn(globalThis, 'setTimeout');
});

afterAll(() => {
  timeouts.mockRestore();
  random.mockRestore();
  unmount();
  window.localStorage.clear();
});

describe('the court console route leaves the list gate idle', () => {
  it('a match event schedules neither a refetch nor a deadline', async () => {
    const loads = window.API.fetchCompetitions.mock.calls.length;
    await act(async () => {
      sse.emit({ type: 'match_updated', seq: 5, data: { competitionId: 'c1', matchId: 'm1', result: { id: 'm1', status: 'running' } } });
      sse.emit({ type: 'match_updated', seq: 6, data: { competitionId: 'c1', matchId: 'm1', result: { id: 'm1', status: 'running' } } });
    });
    await act(async () => { await new Promise((r) => setTimeout(r, HELD_PUSH_WAIT_MS + 40)); });
    const delays = timeouts.mock.calls.map((c) => c[1]);
    expect(delays, 'no deadline timer left in the pool').not.toContain(FETCH_DEADLINE_MS);
    expect(window.API.fetchCompetitions.mock.calls.length, 'no list read').toBe(loads);
  });
});
