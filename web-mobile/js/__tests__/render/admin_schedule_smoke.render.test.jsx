// Tier-2 render smoke tests for the three components in admin_schedule.jsx
// that previously had zero render coverage (mp-d7tl pre-split gate).
//
// Goal: each component mounts under real React 18 + jsdom without throwing
// and renders a key landmark. These are SHALLOW smoke tests — they do NOT
// test interaction or deep behaviour. The browser smoke pass (make run-mobile)
// is the authoritative end-to-end gate.
//
// Note on the vitest-mounts-big-components blind spot (see project memory):
// a green render suite does NOT substitute for the mandatory browser smoke.
// It only catches gross missing-window-ref / crash-on-mount failures that
// the unit suite's fake-React stub cannot see.
//
// Globals strategy: set ALL required window.* before the dynamic import so
// module-level `const X = window.X` captures pick up the stubs. We use a
// beforeAll / afterAll restore pattern copied from autosave_debounce.render.test.jsx.

import React from 'react';
import { render, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';

// ── window stubs ─────────────────────────────────────────────────────────────
// Split into:
//   MODULE_LEVEL — captured by `const X = window.X` at module eval time;
//                  must be set before the await import below.
//   BODY_LEVEL   — read directly in render/useMemo bodies (also set early).

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

  // BODY_LEVEL — called during initial render of AdminSchedulePage
  tournamentMatches: () => [],
  applyFilters:      (arr) => arr,
  StableInput:       (props) => React.createElement('input', { 'data-testid': 'stable-input', type: props.type }),
  PlayerMultiFilter: Stub('player-multi-filter'),

  // BODY_LEVEL — called during initial render of AdminScoreEditor
  // (rendered by AdminScoreEditorPage)
  compMatches: () => [],
};

const originals = {};
let AdminSchedulePage, AdminScoreEditorPage, PerCourtBreakdown;

beforeAll(async () => {
  for (const [k, v] of Object.entries(STUBBED_GLOBALS)) {
    originals[k] = { had: k in window, value: window[k] };
    window[k] = v;
  }
  await import('../../admin_schedule.jsx');
  AdminSchedulePage    = window.AdminSchedulePage;
  AdminScoreEditorPage = window.AdminScoreEditorPage;
  PerCourtBreakdown    = window.PerCourtBreakdown;
});

afterAll(() => {
  for (const [k, orig] of Object.entries(originals)) {
    if (orig.had) window[k] = orig.value;
    else delete window[k];
  }
});

// ── Fixtures ─────────────────────────────────────────────────────────────────

const EMPTY_TOURNAMENT = {
  id: 't1',
  name: 'Test Tournament',
  competitions: [],
  courts: [],
};

const noop = () => {};

// ── PerCourtBreakdown ────────────────────────────────────────────────────────

describe('PerCourtBreakdown smoke', () => {
  it('renders null when perCourtMinutes is empty', () => {
    const { container } = render(
      React.createElement(PerCourtBreakdown, { perCourtMinutes: [] }),
    );
    expect(container.firstChild).toBeNull();
  });

  it('mounts with data and renders court labels', () => {
    render(
      React.createElement(PerCourtBreakdown, { perCourtMinutes: [60, 90] }),
    );
    expect(screen.getByText(/Court A/)).toBeInTheDocument();
    expect(screen.getByText(/Court B/)).toBeInTheDocument();
  });
});

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
