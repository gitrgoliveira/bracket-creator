import React from 'react';
import { render } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, afterEach } from 'vitest';

// Window globals required by admin_shiaijo.jsx.
// MODULE-EVAL-TIME entries (e.g. `const AdminTopbar = window.AdminTopbar;`)
// must be set before the dynamic import or the module captures undefined.
const STUBBED_GLOBALS = {
  // MODULE-EVAL-TIME — captured at import; set before dynamic import below
  AdminTopbar: ({ children }) => <div data-testid="topbar">{children}</div>,
  Breadcrumbs: () => null,
  ScoreEditorModal: () => <div data-testid="score-editor" />,
  CourtPicker: () => null,
  BracketTree: () => null,
  Icon: ({ name }) => <span>{name}</span>,
  hasBothSides: () => true,
  // LAZY — only called in event handlers or guarded effects
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

  // Guard: verify that a missing window scope reference causes a render failure.
  // This is the exact class of bug that PR #271 introduced undetected:
  // requestMoveCourt was used in ShiaijoQueueGroup but not passed as a prop,
  // causing a ReferenceError that the fake-React stub suite never caught.
  it('GUARD: a component referencing an undefined prop throws a ReferenceError', () => {
    // A minimal component that references an undefined variable — simulates
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
