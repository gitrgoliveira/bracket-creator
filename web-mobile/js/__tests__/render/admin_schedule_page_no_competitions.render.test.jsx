// bc-pnum ruling 3: the server now guarantees every tournament object it
// returns to the SPA carries a "competitions" key (buildTournamentResponse,
// handlers_tournament.go), so a wizard-fresh tournament is
// `{ ...fields, competitions: [] }`, never a bare object missing the key
// (that used to be the gap: state.Tournament itself has no Competitions
// field, so POST /api/tournament's raw response carried none, and
// AdminSchedulePage indexed `tournament.competitions[0]` at component init
// before any of its other `tournament.competitions || []` guards ran,
// throwing a TypeError the moment the "Tournament schedule" card opened on
// a freshly created tournament).
//
// This test mounts AdminSchedulePage directly with the GUARANTEED shape
// (competitions: []) as the pin that the page renders correctly with zero
// competitions -- the schedule-page init guard below stays regardless (it
// matches the file's siblings' own defensive `|| []` pattern), this is no
// longer defending against a missing key, since the server never omits one.

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

// The guaranteed shape every tournament response now carries (bc-pnum
// ruling 3): a fresh tournament has zero competitions, and the key is
// always present as an empty array, never omitted.
const FRESH_TOURNAMENT_WITH_NO_COMPETITIONS = {
  id: 't1',
  name: 'Wizard Fresh Tournament',
  courts: ['A'],
  competitions: [],
};

describe('AdminSchedulePage with a freshly created tournament (zero competitions)', () => {
  it('renders the schedule card with no competitions, competitions: [] guaranteed by the server', () => {
    render(
      React.createElement(AdminSchedulePage, {
        tournament:   FRESH_TOURNAMENT_WITH_NO_COMPETITIONS,
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
