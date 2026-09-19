// Tier-2 render smoke tests for the three components in admin_schedule.jsx
// that previously had zero render coverage (mp-d7tl pre-split gate).
//
// Goal: each component mounts under real React 18 + jsdom without throwing
// and renders a key landmark. These are SHALLOW smoke tests; they do NOT
// test interaction or deep behaviour. The browser smoke pass (make run-mobile)
// is the authoritative end-to-end gate.
//
// Note on the vitest-mounts-big-components blind spot (see project memory):
// a green render suite does NOT substitute for the mandatory browser smoke.
// It only catches gross missing-window-ref / crash-on-mount failures that
// the unit suite's fake-React stub cannot see.
//
// Globals strategy: set all required window.* before the dynamic import so
// module-level `const X = window.X` captures pick up the stubs. We use a
// beforeAll / afterAll restore pattern copied from autosave_debounce.render.test.jsx.

import React from 'react';
import { render, screen } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterAll } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

// ── window stubs ─────────────────────────────────────────────────────────────
// Split into:
//   MODULE_LEVEL: captured by `const X = window.X` at module eval time;
//                 must be set before the await import below.
//   BODY_LEVEL:   read directly in render/useMemo bodies (also set early).

const Stub = (name) => function StubComp() {
  return React.createElement('div', { 'data-testid': name });
};

const STUBBED_GLOBALS = {
  // MODULE_LEVEL captures
  pluralize:       (n, a, b) => `${n} ${n === 1 ? a : b}`,
  AdminTopbar:     Stub('admin-topbar'),
  Breadcrumbs:     Stub('breadcrumbs'),
  CourtPicker:     Stub('court-picker'),   // captured at module level; not rendered with empty tournament
  ScoreEditorModal: () => null,
  hasBothSides:    (m) => !!(m?.sideA?.id && m?.sideB?.id),
  getScoreBtnClass: () => '',

  // BODY_LEVEL: called during initial render of AdminSchedulePage
  tournamentMatches: () => [],
  applyFilters:      (arr) => arr,
  StableInput:       (props) => React.createElement('input', { 'data-testid': 'stable-input', type: props.type }),
  PlayerMultiFilter: Stub('player-multi-filter'),

  // BODY_LEVEL: called during initial render of AdminScoreEditor
  // (rendered by AdminScoreEditorPage)
  compMatches: () => [],
};

let restoreGlobals;
let AdminSchedulePage, AdminScoreEditorPage;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  await import('../../admin_schedule.jsx');
  AdminSchedulePage    = window.AdminSchedulePage;
  AdminScoreEditorPage = window.AdminScoreEditorPage;
});

afterAll(() => restoreGlobals());

// ── Fixtures ─────────────────────────────────────────────────────────────────

const EMPTY_TOURNAMENT = {
  id: 't1',
  name: 'Test Tournament',
  competitions: [],
  courts: [],
};

const noop = () => {};

// ── AdminScoreEditorPage ─────────────────────────────────────────────────────

describe('AdminScoreEditorPage smoke', () => {
  it('mounts without throwing and renders the Score editor heading', () => {
    render(
      React.createElement(AdminScoreEditorPage, {
        tournament:   EMPTY_TOURNAMENT,
        onBack:       noop,
        onEditScore:  noop,
        onMoveCourt:  noop,
        onLogout:     noop,
        onViewerMode: noop,
        password:     '',
      }),
    );
    expect(screen.getByText('Score editor')).toBeInTheDocument();
  });

  it('renders the topbar and breadcrumbs stubs', () => {
    render(
      React.createElement(AdminScoreEditorPage, {
        tournament:   EMPTY_TOURNAMENT,
        onBack:       noop,
        onEditScore:  noop,
        onMoveCourt:  noop,
        onLogout:     noop,
        onViewerMode: noop,
        password:     '',
      }),
    );
    expect(screen.getByTestId('admin-topbar')).toBeInTheDocument();
    expect(screen.getByTestId('breadcrumbs')).toBeInTheDocument();
  });
});

// ── AdminSchedulePage ────────────────────────────────────────────────────────

describe('AdminSchedulePage smoke', () => {
  it('mounts without throwing and renders the Tournament schedule heading', () => {
    render(
      React.createElement(AdminSchedulePage, {
        tournament:   EMPTY_TOURNAMENT,
        onBack:       noop,
        onMoveCourt:  noop,
        onLogout:     noop,
        onViewerMode: noop,
        password:     '',
      }),
    );
    expect(screen.getByText('Tournament schedule')).toBeInTheDocument();
  });

  it('renders the topbar, breadcrumbs, and filter stubs', () => {
    render(
      React.createElement(AdminSchedulePage, {
        tournament:   EMPTY_TOURNAMENT,
        onBack:       noop,
        onMoveCourt:  noop,
        onLogout:     noop,
        onViewerMode: noop,
        password:     '',
      }),
    );
    expect(screen.getByTestId('admin-topbar')).toBeInTheDocument();
    expect(screen.getByTestId('breadcrumbs')).toBeInTheDocument();
    expect(screen.getByTestId('player-multi-filter')).toBeInTheDocument();
  });
});

// bc-cse regression: AdminTWMatch (the per-court row rendered by
// AdminSchedulePage) gated its score cell on `m.status === "completed"`, so
// a running team match's live subResults-derived score never showed on the
// admin schedule board -- only once the match ended. Widened to admit
// "running" too. These tests supply a populated tournament (rather than
// EMPTY_TOURNAMENT above) so AdminTWMatch actually renders a match row, with
// window.tournamentMatches/window.matchScoreStr/window.poolLabel/
// window.matchHighlightedBy overridden locally per test and restored
// afterward -- the base STUBBED_GLOBALS above intentionally returns no
// matches, so those four are not among them.
describe('AdminSchedulePage: running match score cell (bc-cse)', () => {
  // Deliberately INDIVIDUAL-shaped (no teamResult): AdminTWMatch's gate does
  // not distinguish match kind, unlike matchStateCell's team-only running
  // branch (bracket.jsx) -- this fixture is the direct evidence that choice
  // was made on purpose here, not an oversight (see the comment above
  // AdminTWMatch's score block for why the two call sites diverge).
  const RUNNING_MATCH = {
    id: 'm-running', compId: 'c1', court: 'A', status: 'running', phase: 'bracket', round: 'Final',
    sideA: { id: 'a', name: 'Aka Dojo' }, sideB: { id: 'b', name: 'Shiro Dojo' },
  };
  const SCHEDULED_MATCH = {
    id: 'm-scheduled', compId: 'c1', court: 'A', status: 'scheduled', phase: 'bracket', round: 'Semifinal',
    sideA: { id: 'c', name: 'Third Dojo' }, sideB: { id: 'd', name: 'Fourth Dojo' },
  };
  const TOURNAMENT_ONE_COURT = { id: 't1', name: 'Test Tournament', competitions: [], courts: ['A'] };

  let saved;

  beforeEach(() => {
    saved = {
      tournamentMatches: window.tournamentMatches,
      matchScoreStr: window.matchScoreStr,
      poolLabel: window.poolLabel,
      matchHighlightedBy: window.matchHighlightedBy,
    };
    window.matchScoreStr = () => 'IV 1-1\nPW 3-2';
    window.poolLabel = () => 'Pool A';
    window.matchHighlightedBy = () => false;
  });

  afterEach(() => {
    window.tournamentMatches = saved.tournamentMatches;
    window.matchScoreStr = saved.matchScoreStr;
    window.poolLabel = saved.poolLabel;
    window.matchHighlightedBy = saved.matchHighlightedBy;
  });

  it('shows the live score for a RUNNING match', () => {
    window.tournamentMatches = () => [RUNNING_MATCH];
    const { container } = render(
      React.createElement(AdminSchedulePage, {
        tournament: TOURNAMENT_ONE_COURT, onBack: noop, onMoveCourt: noop, onLogout: noop, onViewerMode: noop, password: '',
      }),
    );
    expect(container.textContent).toContain('IV 1-1');
    expect(container.textContent).toContain('PW 3-2');
  });

  it('shows no score for a SCHEDULED match (gate stays closed pre-play)', () => {
    window.tournamentMatches = () => [SCHEDULED_MATCH];
    const { container } = render(
      React.createElement(AdminSchedulePage, {
        tournament: TOURNAMENT_ONE_COURT, onBack: noop, onMoveCourt: noop, onLogout: noop, onViewerMode: noop, password: '',
      }),
    );
    expect(container.textContent).not.toContain('IV 1-1');
  });
});
