import React from 'react';
import { render, act } from '@testing-library/react';
import { vi, beforeAll, afterAll, afterEach } from 'vitest';
import { installWindowStubs } from './stub_globals.js';
import { createOwnStatusMoves } from './own_status_moves.js';

// This file is plain .js, where JSX is not compiled, so elements are made by hand.
const h = React.createElement;

// The shared fixture of the court console's render tests (admin_shiaijo*.render.test.jsx
// split by concern): the window globals the console reads, the page itself and the two
// ways of mounting it. A test file calls setupShiaijoConsole() once at its top level.

// Window globals required by admin_shiaijo.jsx.
// MODULE-EVAL-TIME entries (e.g. `const AdminTopbar = window.AdminTopbar;`)
// must be set before the dynamic import, or the module captures undefined.
//
// The write_result predicates are deliberately NOT here: admin_shiaijo.jsx
// imports them directly from the leaf, so the component always gets the real
// implementations and a window entry for them would be dead weight suggesting
// otherwise. The queued-vs-superseded split the offline-resolve test
// depends on is therefore the real one either way.

// bc-cse: captures the LATEST ScoreEditorModal props so a test can drive
// onSubmitAndNext/onAfterDecision directly (the chained-navigation tests), while the rendered markup stays exactly what the pre-existing
// tests already query (data-testid="score-editor" data-match=...).
const probe = { props: null };
// The outbox's status moves the console reads (API.ownStatusMove, M4).
const outbox = createOwnStatusMoves();
// The latest topbar and breadcrumb props, so a test can take the console's
// own exits (Sign out, Public viewer, the Dashboard crumb).
const chrome = { topbar: null, crumbs: null };

export { probe, outbox, chrome };

export const STUBBED_GLOBALS = {
  // MODULE-EVAL-TIME: captured at import; set before the dynamic import in setupShiaijoConsole
  AdminTopbar: (props) => { chrome.topbar = props; return h('div', { 'data-testid': 'topbar' }, props.children); },
  Breadcrumbs: (props) => { chrome.crumbs = props; return null; },
  // Carries the id of the match the panel is scoring, so a test can tell
  // WHICH match the console selected (captured at import, so it cannot be
  // swapped per test).
  ScoreEditorModal: (props) => { probe.props = props; return h('div', { 'data-testid': 'score-editor', 'data-match': props.match ? props.match.id : '' }); },
  // A bare court control: one button per OTHER court, calling onChange the
  // way the real CourtPicker's option does (admin_shell.jsx), so the console's
  // move confirm can be reached without the real popover.
  CourtPicker: ({ value, courts, onChange }) => h('span', { 'data-testid': 'court-picker-stub' },
    (courts || []).filter((cc) => cc !== value).map((cc) => (
      h('button', { type: 'button', key: cc, 'data-testid': `move-to-${cc}`, onClick: () => onChange(cc) }, `Move to ${cc}`)
    ))),
  BracketTree: () => null,
  Icon: ({ name }) => h('span', null, name),
  // hasBothSides / isPendingBracketMatch are the REAL implementations published
  // on window by vitest.setup.render.js (import of admin_helpers.jsx). We
  // deliberately do NOT stub them: the shiaijo queue's split between actionable
  // rows and pending placeholder finals (mp-y3nk) depends on their real logic.
  // LAZY: only called in event handlers or guarded effects
  filterMatchesByCourt: (matches, _court) => matches,
  tournamentMatches: () => [],
  API: {
    fetchCompetitionDetails: vi.fn().mockResolvedValue(null),
    sendAnnouncement: vi.fn(),
    updateMatchTime: vi.fn(),
    startMatch: vi.fn(),
    recordDecision: vi.fn().mockResolvedValue({ applied: true }),
    reinstateCompetitor: vi.fn().mockResolvedValue({}),
    ...outbox.api,
  },
  // No startPatch: the console imports it from start_match.jsx (a bare vi.fn()
  // here used to make every console start in this suite send `undefined`).
  confirmDialog: vi.fn().mockResolvedValue(true),
  PoolsViewer: () => null,
  compMatches: () => [],
};

// Installs the globals, imports the page and restores both, as the original single file did.
export function setupShiaijoConsole() {
  let restoreGlobals;
  beforeAll(async () => {
    restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
    await import('../../admin_shiaijo.jsx');
  });

  afterAll(() => restoreGlobals());

  // Reset per-test window overrides so each test starts from a known baseline.
  afterEach(() => {
    window.tournamentMatches = STUBBED_GLOBALS.tournamentMatches;
    window.filterMatchesByCourt = STUBBED_GLOBALS.filterMatchesByCourt;
  });
}

// The page is published on window by the dynamic import above, so it is read at render time.
export const AdminShiaijoPage = (props) => h(window.AdminShiaijoPage, props);

export function makeMinimalTournament(overrides = {}) {
  return {
    name: 'Test Tournament',
    courts: ['A', 'B'],
    competitions: [],
    ...overrides,
  };
}

export function renderPage(tournament, court = 'A', props = {}) {
  return render(h(AdminShiaijoPage, {
    tournament,
    court,
    onBack: vi.fn(),
    onEditScore: props.onEditScore || vi.fn(),
    onMoveCourt: vi.fn(),
    onLogout: vi.fn(),
    onViewerMode: vi.fn(),
    password: '',
    showToast: props.showToast || vi.fn(),
    tweaks: {},
    onSwitchCourt: vi.fn(),
  }));
}

// Shared by the render tests: a mutable court feed, a Refresh that
// re-reads it, and the editor probe's current match.
export async function mountCourt(initial, opts = {}) {
  const feed = { current: initial };
  window.tournamentMatches = () => feed.current;
  window.filterMatchesByCourt = (matches) => matches;
  const prev = {
    fetch: window.API.fetchCourtMatches,
    sub: window.API.subscribeToEvents,
    revert: window.API.revertMatchToQueue,
  };
  window.API.fetchCourtMatches = vi.fn().mockImplementation(() => Promise.resolve(opts.comps || [{ id: 'c1', name: 'Cup' }]));
  window.API.subscribeToEvents = opts.subscribeToEvents || (() => () => {});
  window.API.revertMatchToQueue = vi.fn().mockResolvedValue(true);
  const showToast = vi.fn();
  const onMoveCourt = opts.onMoveCourt || vi.fn();
  const exits = { onBack: vi.fn(), onLogout: vi.fn(), onViewerMode: vi.fn(), onSwitchCourt: vi.fn() };
  let utils;
  await act(async () => {
    utils = render(h(AdminShiaijoPage, {
      tournament: makeMinimalTournament(opts.tournament),
      court: 'A',
      onBack: exits.onBack,
      onEditScore: opts.onEditScore || vi.fn(),
      onMoveCourt,
      onLogout: exits.onLogout,
      onViewerMode: exits.onViewerMode,
      password: '',
      showToast,
      tweaks: {},
      onSwitchCourt: exits.onSwitchCourt,
    }));
  });
  return {
    utils,
    feed,
    showToast,
    onMoveCourt,
    exits,
    editorMatch: () => { const el = utils.queryByTestId('score-editor'); return el ? el.getAttribute('data-match') : null; },
    refresh: async () => { await act(async () => { utils.getByRole('button', { name: /refresh/i }).click(); }); },
    restore: () => {
      window.API.fetchCourtMatches = prev.fetch;
      window.API.subscribeToEvents = prev.sub;
      window.API.revertMatchToQueue = prev.revert;
    },
  };
}

export const courtSide = (id, name) => ({ id, name });
export const courtMatch = (id, status, over = {}) => ({
  id, compId: 'c1', compName: 'Cup', status, phase: 'pool', poolName: 'Pool A', court: 'A',
  scheduledAt: `09:0${id.slice(-1)}`,
  sideA: courtSide(`${id}-a`, `Aka ${id}`), sideB: courtSide(`${id}-b`, `Shiro ${id}`),
  ...over,
});
