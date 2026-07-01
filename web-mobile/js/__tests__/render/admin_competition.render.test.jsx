import React from 'react';
import { render, act, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';

// mp-hpe3 Phase 0 safety net: RENDER-SMOKE characterization of the sections
// inside admin_competition.jsx that the upcoming split moves into their own
// modules: AdminCompOverview, AdminSettings, FightingSpiritAwardsEditor,
// AdminBracket, and AdminSwissRounds.
//
// These components are module-internal (not on window), so they are exercised
// through the PUBLIC AdminCompetition entry by routing each `section`. That
// also means the test survives the split unchanged; AdminCompetition stays on
// window and pins exactly what a split must preserve: every section must
// still mount with zero console errors. The render harness mounts with REAL
// React and FAILS on any console.warn/error, so a moved component that
// references a window.* dep not yet loaded at its render time throws here
// (the load-order class of breakage that vitest's default stub cannot catch).
//
// All window.* deps captured at admin_competition.jsx module load (the
// `const X = window.X` block at the top) MUST be set before the dynamic import,
// or the captured const is undefined and the component throws on render.

const noop = () => {};
const Stub = (name) => {
  const C = () => <div data-stub={name} />;
  C.displayName = `Stub(${name})`;
  return C;
};

const STUBBED_GLOBALS = {
  // Components rendered by AdminCompetition / its sections.
  AdminTopbar: Stub('AdminTopbar'),
  Breadcrumbs: Stub('Breadcrumbs'),
  StatusBadge: Stub('StatusBadge'),
  CourtPicker: Stub('CourtPicker'),
  AdminParticipants: Stub('AdminParticipants'),
  AdminPools: Stub('AdminPools'),
  AdminScoreEditor: Stub('AdminScoreEditor'),
  AdminExport: Stub('AdminExport'),
  BracketTree: Stub('BracketTree'),
  AdminTeamLineupsList: Stub('AdminTeamLineupsList'),
  // NOTE: the pure helpers admin_competition_* consumes (compMatchStats,
  // hasBothSides, hasPoolOriginPlaceholder, dmyToIso, isoToDmy, isValidDate,
  // validateAndNormalizeDate, decideNumericUpdate, deriveTournamentDays) are
  // deliberately NOT stubbed here; the render harness (vitest.setup.render.js)
  // loads the real admin_helpers.jsx, so the components run against the genuine
  // implementations and contracts. Hand-rolled stubs drifted from the real
  // signatures/shapes (e.g. decideNumericUpdate is (raw, min), not (field, value)),
  // which made the smoke test less representative; using the real helpers removes
  // that whole class of drift. Only cross-module components, browser APIs, dialogs,
  // and the backend API are stubbed below.
  competitionKindLabel: () => 'Individual',
  formatDate: (d) => String(d ?? ''),
  matchMedia: () => ({
    matches: false,
    addEventListener: noop, removeEventListener: noop,
    addListener: noop, removeListener: noop,
  }),
  // Dialogs / async: only reached from handlers; safe resolved stubs.
  confirmDialog: vi.fn().mockResolvedValue(false),
  promptAdminPassword: vi.fn().mockResolvedValue(null),
  promptDialog: vi.fn().mockResolvedValue(null),
  API: {
    estimateCompetitionSchedule: vi.fn().mockResolvedValue(null),
    swissGenerateRound: vi.fn().mockResolvedValue(null),
    updateCompetitionAwards: vi.fn().mockResolvedValue(null),
    completeCompetition: vi.fn().mockResolvedValue({ status: 'completed' }),
  },
};

const originals = {};
let AdminCompetition;

beforeAll(async () => {
  for (const [k, v] of Object.entries(STUBBED_GLOBALS)) {
    originals[k] = { had: k in window, value: window[k] };
    window[k] = v;
  }
  await import('../../admin_competition.jsx');
  AdminCompetition = window.AdminCompetition;
});

afterAll(() => {
  for (const [k, orig] of Object.entries(originals)) {
    if (orig.had) window[k] = orig.value;
    else delete window[k];
  }
});

function makeCompetition(overrides = {}) {
  return {
    id: 'c1',
    name: 'Mudansha',
    status: 'setup',
    format: 'pools',
    kind: 'individual',
    players: [
      { id: 'p1', name: 'Yamada', seed: 1 },
      { id: 'p2', name: 'Tanaka' },
    ],
    courts: ['A'],
    startTime: '09:00',
    date: '',
    fightingSpiritAwards: [],
    swissCurrentRound: 0,
    swissRounds: 0,
    ...overrides,
  };
}

function makeTournament(comp, overrides = {}) {
  return {
    name: 'Spring Taikai',
    courts: ['A', 'B'],
    competitions: [comp, { id: 'c2', name: 'Yudansha' }],
    ...overrides,
  };
}

// Mount within act() so async effects (e.g. AdminSettings' schedule-estimate
// fetch) flush and settle their state updates inside the act boundary: the
// render harness fails the test on the "not wrapped in act(...)" console.error
// otherwise. Returns the render result; a throw during mount fails the test.
async function mountSection(section, { comp = makeCompetition(), tweaks = {}, bracket = null } = {}) {
  const t = makeTournament(comp);
  let result;
  await act(async () => {
    result = render(
      <AdminCompetition
        tournament={t}
        competition={comp}
        pools={[]}
        poolMatches={[]}
        standings={[]}
        bracket={bracket}
        section={section}
        onSection={noop}
        onBack={noop}
        onOpenCompetition={noop}
        onUpdate={noop}
        onRefreshCompetition={noop}
        onMoveCourt={noop}
        onEditScore={noop}
        onLogout={noop}
        onViewerMode={noop}
        tweaks={tweaks}
        password=""
        showToast={noop}
      />
    );
  });
  return result;
}

describe('AdminCompetition section render-smoke (mp-hpe3 split characterization)', () => {
  it('renders Overview section without throwing', async () => {
    const { container } = await mountSection('overview');
    expect(container.querySelector('[data-stub="AdminTopbar"]')).not.toBeNull();
  });

  it('renders Settings section without throwing', async () => {
    const { container } = await mountSection('settings');
    expect(container.querySelector('[data-stub="AdminTopbar"]')).not.toBeNull();
  });

  it('renders Fighting Spirit (awards) section without throwing', async () => {
    const { container } = await mountSection('awards');
    expect(container.querySelector('[data-stub="AdminTopbar"]')).not.toBeNull();
  });

  it('renders Bracket section (not-generated empty state) without throwing', async () => {
    const { container } = await mountSection('bracket');
    expect(container.querySelector('[data-stub="AdminTopbar"]')).not.toBeNull();
  });

  it('renders Swiss-rounds section without throwing', async () => {
    const comp = makeCompetition({ format: 'swiss', swissRounds: 5, swissCurrentRound: 1 });
    const { container } = await mountSection('swiss', { comp });
    expect(container.querySelector('[data-stub="AdminTopbar"]')).not.toBeNull();
  });

  it('renders Overview for a team competition without throwing', async () => {
    const comp = makeCompetition({ kind: 'team', players: [{ id: 't1', name: 'Team A' }] });
    const { container } = await mountSection('overview', { comp });
    expect(container.querySelector('[data-stub="AdminTopbar"]')).not.toBeNull();
  });
});

// mp-gy6g: "Complete competition" is the only trigger for a bracket-based
// (playoffs, or mixed-after-knockout) competition to ever reach status
// "completed" — MaybeAutoCompletePools only auto-transitions League on its
// last pool match. Gated on canComplete (admin_competition.jsx), which
// delegates to bracketFullyComplete (admin_helpers.jsx, exercised for real
// here per this file's header comment).
describe('AdminCompetition "Complete competition" action (mp-gy6g)', () => {
  const realMatch = (status) => ({
    sideA: { id: 'p1', name: 'Alice' },
    sideB: { id: 'p2', name: 'Bob' },
    status,
  });
  const findButton = (container, text) =>
    Array.from(container.querySelectorAll('button')).find((b) => b.textContent.trim() === text);

  it('is hidden while a bracket match is still unfinished', async () => {
    const comp = makeCompetition({ format: 'playoffs', status: 'playoffs' });
    const bracket = { rounds: [[realMatch('completed')], [realMatch('running')]] };
    const { container } = await mountSection('overview', { comp, bracket });
    expect(findButton(container, 'Complete competition →')).toBeUndefined();
  });

  it('stays hidden once every round match is done but the bronze match is not (thirdPlaceMatch is a sibling of rounds)', async () => {
    const comp = makeCompetition({ format: 'playoffs', status: 'playoffs', naginata: true });
    const bracket = {
      rounds: [[realMatch('completed')], [realMatch('completed')]],
      thirdPlaceMatch: realMatch('running'),
    };
    const { container } = await mountSection('overview', { comp, bracket });
    expect(findButton(container, 'Complete competition →')).toBeUndefined();
  });

  it('appears once every bracket match, including the bronze match, is completed', async () => {
    const comp = makeCompetition({ format: 'playoffs', status: 'playoffs', naginata: true });
    const bracket = {
      rounds: [[realMatch('completed')], [realMatch('completed')]],
      thirdPlaceMatch: realMatch('completed'),
    };
    const { container } = await mountSection('overview', { comp, bracket });
    expect(findButton(container, 'Complete competition →')).not.toBeUndefined();
  });

  it('is hidden once the competition is already completed', async () => {
    const comp = makeCompetition({ format: 'playoffs', status: 'completed' });
    const bracket = { rounds: [[realMatch('completed')], [realMatch('completed')]] };
    const { container } = await mountSection('overview', { comp, bracket });
    expect(findButton(container, 'Complete competition →')).toBeUndefined();
  });

  it('is hidden while setup/draw-ready, even if a stale bracket looks complete', async () => {
    const bracket = { rounds: [[realMatch('completed')]] };
    for (const status of ['setup', 'draw-ready']) {
      const comp = makeCompetition({ format: 'playoffs', status });
      const { container } = await mountSection('overview', { comp, bracket });
      expect(findButton(container, 'Complete competition →')).toBeUndefined();
    }
  });

  it('calls the API and refreshes on confirm', async () => {
    window.confirmDialog.mockResolvedValueOnce(true);
    window.API.completeCompetition.mockClear();
    const onRefreshCompetition = vi.fn();
    const showToast = vi.fn();
    const comp = makeCompetition({ id: 'nagi-1', format: 'playoffs', status: 'playoffs' });
    const bracket = { rounds: [[realMatch('completed')], [realMatch('completed')]] };
    const t = makeTournament(comp);
    let container;
    await act(async () => {
      ({ container } = render(
        <AdminCompetition
          tournament={t}
          competition={comp}
          pools={[]}
          poolMatches={[]}
          standings={[]}
          bracket={bracket}
          section="overview"
          onSection={noop}
          onBack={noop}
          onOpenCompetition={noop}
          onUpdate={noop}
          onRefreshCompetition={onRefreshCompetition}
          onMoveCourt={noop}
          onEditScore={noop}
          onLogout={noop}
          onViewerMode={noop}
          tweaks={{}}
          password="shiaijo2026"
          showToast={showToast}
        />
      ));
    });

    const btn = findButton(container, 'Complete competition →');
    expect(btn).not.toBeUndefined();
    await act(async () => { fireEvent.click(btn); });

    await waitFor(() => expect(window.API.completeCompetition).toHaveBeenCalledWith('nagi-1', 'shiaijo2026'));
    expect(onRefreshCompetition).toHaveBeenCalled();
    expect(showToast).toHaveBeenCalledWith(expect.stringContaining('marked complete'));
  });

  it('does not call the API when the operator cancels the confirm dialog', async () => {
    window.confirmDialog.mockResolvedValueOnce(false);
    window.API.completeCompetition.mockClear();
    const comp = makeCompetition({ format: 'playoffs', status: 'playoffs' });
    const bracket = { rounds: [[realMatch('completed')]] };
    const { container } = await mountSection('overview', { comp, bracket });

    const btn = findButton(container, 'Complete competition →');
    await act(async () => { fireEvent.click(btn); });

    expect(window.API.completeCompetition).not.toHaveBeenCalled();
  });
});
