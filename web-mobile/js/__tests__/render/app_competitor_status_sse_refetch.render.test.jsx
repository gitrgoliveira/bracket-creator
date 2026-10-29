import React from 'react';
import { act } from '@testing-library/react';
import { describe, it, expect, vi, afterAll } from 'vitest';
import { mountApp, settle } from '../helpers/mount_app.js';

const page = { props: null };
function ProbeViewerCompetition(props) {
  page.props = props;
  return <div data-testid="viewer-competition" />;
}

const detail = (id) => ({
  config: { id, name: 'Open', kind: 'individual', format: 'mixed', status: 'pools', courts: ['A'], date: '2026-11-01' },
  pools: [],
  poolMatches: [{ id: 'Pool A-0', status: 'scheduled', court: 'A', modifiedAt: 100 }],
  standings: {},
  bracket: null,
});

const sse = { emit: null };

const STUBBED_GLOBALS = {
  ViewerCompetition: ProbeViewerCompetition,
  API: {
    fetchTournament: vi.fn(async () => ({ name: 'T', mode: 'self-run', courts: ['A'] })),
    fetchCompetitions: vi.fn(async () => [
      { id: 'c1', name: 'Open', date: '2026-11-01' },
      { id: 'c2', name: 'Same Day', date: '2026-11-01' },
      { id: 'c3', name: 'Other Day', date: '2026-11-02' },
    ]),
    fetchAuthConfig: vi.fn(async () => ({ mode: 'file', resetEnabled: true })),
    fetchAnnouncements: vi.fn(async () => []),
    fetchCompetitionDetails: vi.fn(async (id) => detail(id)),
    subscribeToEvents: vi.fn((onEvent) => { sse.emit = onEvent; return () => {}; }),
    reconnectEvents: vi.fn(),
    resumeAfterAuth: vi.fn(),
  },
};

let unmount;

afterAll(() => { if (unmount) unmount(); });

describe('competitor_status_updated cross-competition refetch in app.jsx', () => {
  it('refetches open competition detail on competitor_status_updated from a same-day competition, but not from another day', async () => {
    ({ unmount } = await mountApp({ path: '/competition/c1', globals: STUBBED_GLOBALS }));
    expect(page.props).toBeTruthy();
    expect(window.API.fetchCompetitionDetails).toHaveBeenCalledTimes(1);
    expect(window.API.fetchCompetitionDetails).toHaveBeenLastCalledWith('c1');

    // A competitor_status_updated event from another competition on the same day refetches the open competition.
    const randomSameDay = vi.spyOn(Math, 'random').mockReturnValue(0);
    try {
      await act(async () => {
        sse.emit({ type: 'competitor_status_updated', data: { competitionId: 'c2' } });
      });
      await settle();
    } finally {
      randomSameDay.mockRestore();
    }
    expect(window.API.fetchCompetitionDetails).toHaveBeenCalledTimes(2);
    expect(window.API.fetchCompetitionDetails).toHaveBeenLastCalledWith('c1');

    // A competitor_status_updated event from another competition on a different day does not refetch.
    const randomOtherDay = vi.spyOn(Math, 'random').mockReturnValue(0);
    try {
      await act(async () => {
        sse.emit({ type: 'competitor_status_updated', data: { competitionId: 'c3' } });
      });
      await settle();
    } finally {
      randomOtherDay.mockRestore();
    }
    expect(window.API.fetchCompetitionDetails).toHaveBeenCalledTimes(2);
  });
});
