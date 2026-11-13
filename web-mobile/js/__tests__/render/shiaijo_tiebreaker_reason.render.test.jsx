// The court console says why a tie-breaker bout is on the court: a line on its
// queue row (and its Up next card) naming the place it decides. A league reads
// the league table, a pool the competition detail's rows for the bout's pool.
// Before the standings are known, or when the two sides are no longer level,
// the line still says the bout is a tie-breaker. A regular bout and a finished
// tie-breaker carry no line.

import React from 'react';
import { render, screen, cleanup, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, afterEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

const STUBBED_GLOBALS = {
  AdminTopbar: ({ children }) => <div data-testid="topbar">{children}</div>,
  Breadcrumbs: () => null,
  ScoreEditorModal: () => null,
  CourtPicker: () => null,
  BracketTree: () => null,
  Icon: ({ name }) => <span>{name}</span>,
  filterMatchesByCourt: (matches) => matches,
  tournamentMatches: () => [],
  API: {
    leagueStandings: vi.fn(),
    fetchCompetitionDetails: vi.fn(),
  },
  PoolsViewer: () => null,
  compMatches: () => [],
};

let restoreGlobals;
let ShiaijoQueueRow;
let tieBreakerReasonLine;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  ({ ShiaijoQueueRow, tieBreakerReasonLine } = await import('../../admin_shiaijo.jsx'));
});

afterAll(() => restoreGlobals());
afterEach(() => cleanup());

const row = (id, name, rank, points) => ({ player: { id, name }, rank, points });
const bout = (over = {}) => ({
  id: 'Pool A-TB-0', compId: 'c1', compName: 'Cycle League', compFormat: 'league',
  court: 'A', status: 'scheduled', phase: 'pool', poolName: 'Pool A', scheduledAt: '10:00',
  sideA: { id: 'p1', name: 'Aki' }, sideB: { id: 'p2', name: 'Bea' }, sideAId: 'p1', sideBId: 'p2',
  ...over,
});
const queueRow = (m) => render(<ShiaijoQueueRow m={m} scheduled={[m]} courts={['A']} />);

describe('tieBreakerReasonLine', () => {
  it('names the place two level sides decide', () => {
    const rows = [row('p0', 'Zed', 1, 6), row('p1', 'Aki', 2, 4), row('p2', 'Bea', 3, 4), row('p3', 'Cal', 4, 0)];
    expect(tieBreakerReasonLine(bout(), rows)).toBe('Tie-breaker for 2nd place. They finished level, so this bout separates them.');
  });

  it('names the span of a larger level group', () => {
    const rows = [row('p1', 'Aki', 1, 4), row('p2', 'Bea', 2, 4), row('p3', 'Cal', 3, 4)];
    expect(tieBreakerReasonLine(bout(), rows)).toBe('Tie-breaker for 1st to 3rd place. They finished level, so this bout helps separate them.');
  });

  it('names no place when it cannot know one', () => {
    const generic = 'Tie-breaker bout. These two finished level.';
    expect(tieBreakerReasonLine(bout(), null)).toBe(generic);
    expect(tieBreakerReasonLine(bout(), [row('p1', 'Aki', 1, 4), row('p2', 'Bea', 2, 3)])).toBe(generic);
    // A side is found by id only: the same name under another id is nobody.
    expect(tieBreakerReasonLine(bout(), [row('x1', 'Aki', 1, 4), row('x2', 'Bea', 2, 4)])).toBe(generic);
  });
});

describe('TieBreakerReason on the queue row', () => {
  it('shows the place a league tie-breaker decides, from the league table', async () => {
    window.API.leagueStandings = vi.fn().mockResolvedValue([row('p1', 'Aki', 1, 4), row('p2', 'Bea', 2, 4), row('p3', 'Cal', 3, 0)]);
    queueRow(bout());
    await waitFor(() => expect(screen.getByTestId('tiebreaker-reason').textContent).toMatch(/^Tie-breaker for 1st place\./));
    expect(window.API.leagueStandings).toHaveBeenCalledWith('c1');
  });

  it('reads a pool tie-breaker from the competition detail rows of its own pool', async () => {
    window.API.fetchCompetitionDetails = vi.fn().mockResolvedValue({
      standings: {
        'Pool A': [row('p1', 'Aki', 1, 4), row('p2', 'Bea', 2, 4)],
        'Pool B': [row('p1', 'Aki', 3, 4), row('p2', 'Bea', 4, 4)],
      },
    });
    queueRow(bout({ compFormat: 'mixed', id: 'Pool A-TB-0' }));
    await waitFor(() => expect(screen.getByTestId('tiebreaker-reason').textContent).toMatch(/^Tie-breaker for 1st place\./));
  });

  it('still says it is a tie-breaker while the standings load or when they fail', async () => {
    window.API.leagueStandings = vi.fn().mockRejectedValue(new Error('offline'));
    queueRow(bout());
    expect(screen.getByTestId('tiebreaker-reason').textContent).toBe('Tie-breaker bout. These two finished level.');
    await waitFor(() => expect(window.API.leagueStandings).toHaveBeenCalled());
    expect(screen.getByTestId('tiebreaker-reason').textContent).toBe('Tie-breaker bout. These two finished level.');
  });

  it('names a daihyosen too', async () => {
    window.API.leagueStandings = vi.fn().mockResolvedValue([row('p1', 'Aki', 1, 4), row('p2', 'Bea', 2, 4)]);
    queueRow(bout({ id: 'Pool A-DH-0' }));
    await waitFor(() => expect(screen.getByTestId('tiebreaker-reason').textContent).toMatch(/^Tie-breaker for 1st place\./));
  });

  it('adds no line to a regular bout or a finished tie-breaker', () => {
    window.API.leagueStandings = vi.fn().mockResolvedValue([]);
    queueRow(bout({ id: 'Pool A-3' }));
    expect(screen.queryByTestId('tiebreaker-reason')).toBeNull();
    cleanup();
    queueRow(bout({ status: 'completed' }));
    expect(screen.queryByTestId('tiebreaker-reason')).toBeNull();
    expect(window.API.leagueStandings).not.toHaveBeenCalled();
  });
});
