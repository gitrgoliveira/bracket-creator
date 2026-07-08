import React from 'react';
import { render, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, afterEach } from 'vitest';

// Window globals required by admin_shiaijo.jsx.
// MODULE-EVAL-TIME entries (e.g. `const AdminTopbar = window.AdminTopbar;`)
// must be set before the dynamic import, or the module captures undefined.
const STUBBED_GLOBALS = {
  // MODULE-EVAL-TIME: captured at import; set before dynamic import below
  AdminTopbar: ({ children }) => <div data-testid="topbar">{children}</div>,
  Breadcrumbs: () => null,
  ScoreEditorModal: () => <div data-testid="score-editor" />,
  CourtPicker: () => null,
  BracketTree: () => null,
  Icon: ({ name }) => <span>{name}</span>,
  // hasBothSides / isPendingBracketMatch are the REAL implementations published
  // on window by vitest.setup.render.js (import of admin_helpers.jsx). We
  // deliberately do NOT stub them: the shiaijo queue's split between actionable
  // rows and pending placeholder finals (mp-y3nk) depends on their real logic.
  // LAZY: only called in event handlers or guarded effects
  filterMatchesByCourt: (matches, _court) => matches,
  tournamentMatches: () => [],
  filterMatchesByPhase: (matches) => matches,
  API: {
    fetchCompetitionDetails: vi.fn().mockResolvedValue(null),
    sendAnnouncement: vi.fn(),
    updateMatchTime: vi.fn(),
    startMatch: vi.fn(),
  },
  startPatch: vi.fn(),
  confirmDialog: vi.fn().mockResolvedValue(true),
  PoolsViewer: () => null,
  compMatches: () => [],
};

const originals = {};
let AdminShiaijoPage;

beforeAll(async () => {
  for (const [k, v] of Object.entries(STUBBED_GLOBALS)) {
    originals[k] = { had: k in window, value: window[k] };
    window[k] = v;
  }
  await import('../../admin_shiaijo.jsx');
  AdminShiaijoPage = window.AdminShiaijoPage;
});

afterAll(() => {
  for (const [k, orig] of Object.entries(originals)) {
    if (orig.had) window[k] = orig.value;
    else delete window[k];
  }
});

// Reset per-test window overrides so each test starts from a known baseline.
afterEach(() => {
  window.tournamentMatches = STUBBED_GLOBALS.tournamentMatches;
  window.filterMatchesByCourt = STUBBED_GLOBALS.filterMatchesByCourt;
});

function makeMinimalTournament(overrides = {}) {
  return {
    name: 'Test Tournament',
    courts: ['A', 'B'],
    competitions: [],
    ...overrides,
  };
}

function renderPage(tournament, court = 'A') {
  return render(
    <AdminShiaijoPage
      tournament={tournament}
      court={court}
      onBack={vi.fn()}
      onEditScore={vi.fn()}
      onMoveCourt={vi.fn()}
      onLogout={vi.fn()}
      onViewerMode={vi.fn()}
      password=""
      showToast={vi.fn()}
      tweaks={{}}
      onSwitchCourt={vi.fn()}
    />
  );
}

describe('AdminShiaijoPage render-smoke', () => {
  it('renders without throwing for an unknown court (empty state)', () => {
    expect(() => renderPage(makeMinimalTournament(), 'Z')).not.toThrow();
  });

  it('renders without throwing for a known court with no matches (empty queue)', () => {
    window.tournamentMatches = () => [];
    window.filterMatchesByCourt = () => [];
    expect(() => renderPage(makeMinimalTournament())).not.toThrow();
  });

  it('renders without throwing with an individual match in "Up Next"', () => {
    const upNextMatch = {
      id: 'm1', compId: 'c1', status: 'scheduled',
      phase: 'pool', poolName: 'Pool 1', court: 'A',
      sideA: { id: 'p1', name: 'Yamada' },
      sideB: { id: 'p2', name: 'Tanaka' },
    };
    window.tournamentMatches = () => [upNextMatch];
    window.filterMatchesByCourt = (matches) => matches;
    expect(() => renderPage(makeMinimalTournament())).not.toThrow();
  });

  it('renders without throwing with a running team match', () => {
    const runningTeamMatch = {
      id: 'm2', compId: 'c1', status: 'running',
      phase: 'pool', poolName: 'Pool 1', court: 'A',
      compKind: 'team', teamSize: 5,
      sideA: { id: 'team-A', name: 'Team A' },
      sideB: { id: 'team-B', name: 'Team B' },
    };
    window.tournamentMatches = () => [runningTeamMatch];
    window.filterMatchesByCourt = (matches) => matches;
    expect(() => renderPage(makeMinimalTournament())).not.toThrow();
  });

  // mp-y3nk: a scheduled knockout final whose sides are still "Winner of rX-mY"
  // feeders must appear in the queue as a non-actionable "Later" row (so a court
  // whose only remaining bout is a downstream final is not shown as empty/done),
  // and must NEVER become the startable Up Next card.
  it('shows a pending placeholder final as a non-actionable "Later" row, never as Up Next', () => {
    const completedFeeder = {
      id: 'r2-m0', compId: 'c1', compName: 'Cup', status: 'completed',
      phase: 'bracket', matchNumber: 1, court: 'A',
      sideA: { id: 'p1', name: 'Yamada' }, sideB: { id: 'p2', name: 'Tanaka' },
      winner: { id: 'p1', name: 'Yamada' },
    };
    const pendingFinal = {
      id: 'r3-m0', compId: 'c1', compName: 'Cup', status: 'scheduled',
      phase: 'bracket', matchNumber: 3, court: 'A',
      sideA: { id: '', name: 'Winner of r2-m0' },
      sideB: { id: '', name: 'Winner of r2-m1' },
    };
    window.tournamentMatches = () => [completedFeeder, pendingFinal];
    window.filterMatchesByCourt = (matches) => matches;

    const { getByText, queryByText } = renderPage(makeMinimalTournament());

    // The placeholder text renders (the "Later" row is the visible signal).
    expect(getByText('Winner of r2-m0')).toBeTruthy();
    expect(getByText('Winner of r2-m1')).toBeTruthy();
    // It is flagged as waiting, not actionable.
    expect(getByText('Waiting')).toBeTruthy();
    // Crucially: it never surfaces a Start button (no real Up Next exists here).
    expect(queryByText('Start match')).toBeNull();
  });

  // mp-y3nk Phase 2: the manual "Refresh" button re-pulls the court feed on
  // demand, the operator's recovery when the queue looks stale after a dropped
  // connection. It must call fetchCourtMatches again beyond the mount fetch.
  it('Refresh button re-pulls the court feed on click', async () => {
    const fetchCourtMatches = vi.fn().mockResolvedValue([]);
    const prevFetch = window.API.fetchCourtMatches;
    const prevSub = window.API.subscribeToEvents;
    window.API.fetchCourtMatches = fetchCourtMatches;
    window.API.subscribeToEvents = () => () => {};
    window.tournamentMatches = () => [];
    window.filterMatchesByCourt = (matches) => matches;
    try {
      let utils;
      // act() wraps the async setCourtComps/setRefreshing updates the mount
      // fetch and the click trigger.
      await act(async () => { utils = renderPage(makeMinimalTournament()); });
      const callsAfterMount = fetchCourtMatches.mock.calls.length;
      expect(callsAfterMount).toBeGreaterThanOrEqual(1);
      await act(async () => { utils.getByRole('button', { name: /refresh/i }).click(); });
      expect(fetchCourtMatches.mock.calls.length).toBeGreaterThan(callsAfterMount);
    } finally {
      window.API.fetchCourtMatches = prevFetch;
      window.API.subscribeToEvents = prevSub;
    }
  });

  // mp-y3nk Phase 3: "Run now" on a pending final opens the resolve-feeders
  // modal; recording each feeder's winner calls overrideBracketWinner (which
  // server-side propagates to resolve the final) and then refetches the court.
  it('Run now → recording feeder winners calls overrideBracketWinner per feeder', async () => {
    const rounds = [
      [
        { id: 'm-r2-0', status: 'scheduled', sideA: { id: 'a', name: 'Alice' }, sideB: { id: 'b', name: 'Bob' } },
        { id: 'm-r2-1', status: 'scheduled', sideA: { id: 'c', name: 'Carol' }, sideB: { id: 'd', name: 'Dan' } },
      ],
      [
        { id: 'm-r1-0', status: 'scheduled', sideA: { id: '', name: 'Winner of r2-m0' }, sideB: { id: '', name: 'Winner of r2-m1' } },
      ],
    ];
    const comp = { id: 'c1', name: 'Cup', bracket: { rounds } };
    const pendingFinal = {
      id: 'm-r1-0', compId: 'c1', compName: 'Cup', status: 'scheduled',
      phase: 'bracket', matchNumber: 3, court: 'A',
      sideA: { id: '', name: 'Winner of r2-m0' }, sideB: { id: '', name: 'Winner of r2-m1' },
    };
    const overrideBracketWinner = vi.fn().mockResolvedValue(true);
    const prevOverride = window.API.overrideBracketWinner;
    window.API.overrideBracketWinner = overrideBracketWinner;
    window.tournamentMatches = () => [pendingFinal];
    window.filterMatchesByCourt = (matches) => matches;
    try {
      let utils;
      await act(async () => { utils = renderPage(makeMinimalTournament({ competitions: [comp] })); });
      await act(async () => { utils.getByRole('button', { name: /run now/i }).click(); });
      // Pick a winner for each feeder, then confirm.
      await act(async () => { utils.getByRole('button', { name: 'Alice' }).click(); });
      await act(async () => { utils.getByRole('button', { name: 'Carol' }).click(); });
      await act(async () => { utils.getByRole('button', { name: /record & make startable/i }).click(); });
      expect(overrideBracketWinner).toHaveBeenCalledTimes(2);
      expect(overrideBracketWinner).toHaveBeenCalledWith('c1', 'm-r2-0', 'Alice', expect.anything());
      expect(overrideBracketWinner).toHaveBeenCalledWith('c1', 'm-r2-1', 'Carol', expect.anything());
    } finally {
      window.API.overrideBracketWinner = prevOverride;
    }
  });

  // mp-y3nk offline console: when the override write is only QUEUED (offline),
  // resolving feeders must still optimistically advance the LOCAL bracket so the
  // final becomes a startable Up Next immediately (not stuck in "Later"). This is
  // the "run the competition offline" path.
  it('offline resolve (queued write) advances the local bracket so the final is startable', async () => {
    const rounds = [
      [
        { id: 'm-r2-0', status: 'scheduled', sideA: { id: 'a', name: 'Alice' }, sideB: { id: 'b', name: 'Bob' } },
        { id: 'm-r2-1', status: 'scheduled', sideA: { id: 'c', name: 'Carol' }, sideB: { id: 'd', name: 'Dan' } },
      ],
      [
        { id: 'm-r1-0', status: 'scheduled', sideA: { id: '', name: 'Winner of r2-m0' }, sideB: { id: '', name: 'Winner of r2-m1' } },
      ],
    ];
    const comp = { id: 'c1', name: 'Cup', bracket: { rounds } };
    const pendingFinal = {
      id: 'm-r1-0', compId: 'c1', compName: 'Cup', status: 'scheduled',
      phase: 'bracket', matchNumber: 3, court: 'A',
      sideA: { id: '', name: 'Winner of r2-m0' }, sideB: { id: '', name: 'Winner of r2-m1' },
    };
    // OFFLINE: override returns a queued discriminator instead of confirming.
    const overrideBracketWinner = vi.fn().mockResolvedValue({ queued: true });
    const prevOverride = window.API.overrideBracketWinner;
    window.API.overrideBracketWinner = overrideBracketWinner;
    // tournamentMatches re-derives queue rows from each comp's bracket.rounds, so
    // an optimistic advance to the bracket surfaces the resolved final.
    window.tournamentMatches = (t) => {
      const c = (t.competitions || []).find((x) => x.id === 'c1');
      const r = c && c.bracket && c.bracket.rounds;
      const out = [];
      if (r) {
        r[0].forEach((m) => out.push({ ...m, compId: 'c1', compName: 'Cup', phase: 'bracket', court: 'A' }));
        r[1].forEach((m, i) => out.push({ ...m, id: 'm-r1-' + i, compId: 'c1', compName: 'Cup', phase: 'bracket', matchNumber: 3, court: 'A' }));
      }
      return out.length ? out : [pendingFinal];
    };
    window.filterMatchesByCourt = (matches) => matches;
    try {
      let utils;
      await act(async () => { utils = renderPage(makeMinimalTournament({ competitions: [comp] })); });
      await act(async () => { utils.getByRole('button', { name: /run now/i }).click(); });
      await act(async () => { utils.getByRole('button', { name: 'Alice' }).click(); });
      await act(async () => { utils.getByRole('button', { name: 'Carol' }).click(); });
      await act(async () => { utils.getByRole('button', { name: /record & make startable/i }).click(); });
      // The final now shows resolved competitors (no placeholder) and is startable
      // even though the override only queued (offline).
      expect(utils.queryByText(/Winner of r2/)).toBeNull(); // no placeholder left anywhere
      expect(utils.getAllByText('Alice').length).toBeGreaterThan(0);
      expect(utils.getAllByText('Carol').length).toBeGreaterThan(0);
      expect(utils.getByRole('button', { name: /start match/i })).toBeTruthy();
    } finally {
      window.API.overrideBracketWinner = prevOverride;
    }
  });

  // Guard: verify that a missing window scope reference causes a render failure.
  // This is the exact class of bug that PR #271 introduced undetected:
  // requestMoveCourt was used in ShiaijoQueueGroup but not passed as a prop,
  // causing a ReferenceError that the fake-React stub suite never caught.
  it('GUARD: a component referencing an undefined prop throws a ReferenceError', () => {
    // A minimal component that references an undefined variable. Simulates
    // the requestMoveCourt bug class. Real React invokes the function body;
    // the stub suite's createElement never does.
    const BrokenComponent = () => {
      // eslint-disable-next-line no-undef
      return <div>{undefinedVariable}</div>;
    };
    // React 18 calls console.error internally when a component throws (before
    // re-throwing the error). Suppress those expected calls with a local spy so
    // the suite's fail-on-console.error guard does not trip on this intentional
    // throw. The local spy replaces the beforeEach spy for this test's duration;
    // afterEach checks the beforeEach spy (which has 0 calls) and passes.
    const localError = vi.spyOn(console, 'error').mockImplementation(() => {});
    const localWarn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    try {
      expect(() => render(<BrokenComponent />)).toThrow(ReferenceError);
    } finally {
      localError.mockRestore();
      localWarn.mockRestore();
    }
  });
});
