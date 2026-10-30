import React from 'react';
import { act } from '@testing-library/react';
import { describe, it, expect, vi, afterAll } from 'vitest';
import { mountApp, settle } from '../helpers/mount_app.js';

// Another competition's match_updated refetches the open competition's detail
// only when it can move a queue place on a court and day the two share: a
// match not seen before, or a change of its status, court or time. A score
// tap on a running match changes none of them, so it refetches nothing.

const page = { props: null };
function ProbeViewerCompetition(props) {
  React.useLayoutEffect(() => { page.props = props; });
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
    fetchTournament: vi.fn(async () => ({ name: 'T', mode: 'self-run', courts: ['A', 'B'] })),
    fetchCompetitions: vi.fn(async () => [
      { id: 'c1', name: 'Open', date: '2026-11-01' },
      { id: 'c2', name: 'Same Day', date: '2026-11-01' },
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

describe('cross-competition match_updated refetch in app.jsx', () => {
  it('refetches on a status or court change of a shared-court match, never on a score tap', async () => {
    ({ unmount } = await mountApp({ path: '/competition/c1', globals: STUBBED_GLOBALS }));
    expect(page.props).toBeTruthy();
    expect(window.API.fetchCompetitionDetails).toHaveBeenCalledTimes(1);

    const emit = async (result) => {
      const random = vi.spyOn(Math, 'random').mockReturnValue(0);
      try {
        await act(async () => {
          sse.emit({ type: 'match_updated', data: { competitionId: 'c2', result } });
        });
        await settle();
      } finally {
        random.mockRestore();
      }
    };
    const calls = () => window.API.fetchCompetitionDetails.mock.calls.length;

    const match = { id: 'Pool A-0', court: 'A', scheduledAt: '09:00', status: 'running', ipponsA: [] };
    await emit(match);
    expect(calls(), 'a match not seen before').toBe(2);
    expect(window.API.fetchCompetitionDetails).toHaveBeenLastCalledWith('c1');

    await emit({ ...match, ipponsA: ['M'] });
    await emit({ ...match, ipponsA: ['M', 'K'] });
    expect(calls(), 'score taps move no queue place').toBe(2);

    await emit({ ...match, ipponsA: ['M', 'K'], status: 'completed' });
    expect(calls(), 'a status change').toBe(3);

    const other = { id: 'Pool B-0', court: 'B', scheduledAt: '09:00', status: 'scheduled' };
    await emit(other);
    expect(calls(), 'a court the open competition does not use').toBe(3);

    await emit({ ...other, court: 'A' });
    expect(calls(), 'a match moved onto a shared court').toBe(4);
  });
});
