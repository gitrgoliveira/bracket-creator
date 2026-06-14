import React from 'react';
import { render } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';

// Window globals that admin_shiaijo.jsx captures at MODULE EVALUATION TIME
// (e.g. `const AdminTopbar = window.AdminTopbar;`). These must be set before
// the dynamic import below or the module captures undefined.
const STUBBED_MODULE_GLOBALS = {
  AdminTopbar: ({ children }) => <div data-testid="topbar">{children}</div>,
  Breadcrumbs: () => null,
  ScoreEditorModal: () => <div data-testid="score-editor" />,
  CourtPicker: () => null,
  BracketTree: () => null,
  Icon: ({ name }) => <span>{name}</span>,
  hasBothSides: () => true,
};

// Window globals called lazily (in event handlers or guarded effects).
// Set them globally so any code path that reaches them doesn't throw.
const STUBBED_RUNTIME_GLOBALS = {
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

beforeAll(async () => {
  for (const [k, v] of Object.entries({ ...STUBBED_MODULE_GLOBALS, ...STUBBED_RUNTIME_GLOBALS })) {
    originals[k] = { had: k in window, value: window[k] };
    window[k] = v;
  }
  await import('../../admin_shiaijo.jsx');
});

afterAll(() => {
  for (const [k, orig] of Object.entries(originals)) {
    if (orig.had) window[k] = orig.value;
    else delete window[k];
  }
});

function makeMinimalTournament(overrides = {}) {
  return {
    name: 'Test Tournament',
    courts: ['A', 'B'],
    competitions: [],
    ...overrides,
  };
}

describe('AdminShiaijoPage render-smoke', () => {
  it('renders without throwing for an unknown court (empty state)', () => {
    const AdminShiaijoPage = window.AdminShiaijoPage;
    expect(() =>
      render(
        <AdminShiaijoPage
          tournament={makeMinimalTournament()}
          court="Z"
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
      )
    ).not.toThrow();
  });

  it('renders without throwing for a known court with no matches (empty queue)', () => {
    window.tournamentMatches = () => [];
    window.filterMatchesByCourt = () => [];
    const AdminShiaijoPage = window.AdminShiaijoPage;
    expect(() =>
      render(
        <AdminShiaijoPage
          tournament={makeMinimalTournament()}
          court="A"
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
      )
    ).not.toThrow();
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
    const AdminShiaijoPage = window.AdminShiaijoPage;
    expect(() =>
      render(
        <AdminShiaijoPage
          tournament={makeMinimalTournament()}
          court="A"
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
      )
    ).not.toThrow();
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
    const AdminShiaijoPage = window.AdminShiaijoPage;
    expect(() =>
      render(
        <AdminShiaijoPage
          tournament={makeMinimalTournament()}
          court="A"
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
      )
    ).not.toThrow();
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
    expect(() => render(<BrokenComponent />)).toThrow(ReferenceError);
  });
});
