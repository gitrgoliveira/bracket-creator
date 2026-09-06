// Regression test: state.Tournament has no Competitions field, so
// POST /api/tournament's response carries no `competitions` key. app.jsx's
// onCreated handler used to store that body verbatim
// (`onCreated={(t, p) => setTournament(t)}`), and AdminSchedulePage indexed
// `tournament.competitions[0]` at component init (before any of its other
// `tournament.competitions || []` guards), so opening the "Tournament
// schedule" card on a wizard-fresh tournament threw a TypeError.
//
// This test mounts AdminSchedulePage directly with a tournament object that
// has no `competitions` key at all (the shape a raw create-tournament
// response has), proving the component is self-sufficient regardless of
// what app.jsx does upstream. See also app_normalize_created_tournament.test.jsx
// for the cheaper unit pin on the app.jsx ingress itself.

import React from 'react';
import { render, screen } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterAll } from 'vitest';

const Stub = (name) => function StubComp() {
  return React.createElement('div', { 'data-testid': name });
};

const STUBBED_GLOBALS = {
  // MODULE_LEVEL captures (read via `const X = window.X` at module eval time)
  pluralize:       (n, a, b) => `${n} ${n === 1 ? a : b}`,
  AdminTopbar:     Stub('admin-topbar'),
  Breadcrumbs:     Stub('breadcrumbs'),
  CourtPicker:     Stub('court-picker'),
  hasBothSides:    (m) => !!(m?.sideA?.id && m?.sideB?.id),

  // BODY_LEVEL: read directly during render
  tournamentMatches: () => [],
  applyFilters:      (arr) => arr,
  StableInput:       (props) => React.createElement('input', { 'data-testid': 'stable-input', type: props.type }),
  PlayerMultiFilter: Stub('player-multi-filter'),
};

const originals = {};
let AdminSchedulePage;

beforeAll(async () => {
  for (const [k, v] of Object.entries(STUBBED_GLOBALS)) {
    originals[k] = { had: k in window, value: window[k] };
    window[k] = v;
  }
  ({ AdminSchedulePage } = await import('../../admin_schedule_page.jsx'));
});

afterAll(() => {
  for (const [k, orig] of Object.entries(originals)) {
    if (orig.had) window[k] = orig.value;
    else delete window[k];
  }
});

const noop = () => {};

// The shape a raw POST /api/tournament response has: no `competitions` key
// at all (not even `competitions: []`).
const TOURNAMENT_WITHOUT_COMPETITIONS_KEY = {
  id: 't1',
  name: 'Wizard Fresh Tournament',
  courts: ['A'],
};

describe('AdminSchedulePage with a tournament missing the competitions key', () => {
  it('mounts without throwing (red before the fix: TypeError on tournament.competitions[0])', () => {
    render(
      React.createElement(AdminSchedulePage, {
        tournament:   TOURNAMENT_WITHOUT_COMPETITIONS_KEY,
        onBack:       noop,
        onMoveCourt:  noop,
        onLogout:     noop,
        onViewerMode: noop,
        password:     '',
      }),
    );
    expect(screen.getByText('Tournament schedule')).toBeInTheDocument();
  });
});
