import React from 'react';
import { render, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';

// mp-hpe3 Phase 0 safety net — RENDER-SMOKE characterization of the sections
// inside admin_competition.jsx that the upcoming split moves into their own
// modules: AdminCompOverview, AdminSettings, FightingSpiritAwardsEditor,
// AdminBracket, and AdminSwissRounds.
//
// These components are module-internal (not on window), so they are exercised
// through the PUBLIC AdminCompetition entry by routing each `section`. That
// also means the test survives the split unchanged — AdminCompetition stays on
// window — and pins exactly what a split must preserve: every section must
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
  // deliberately NOT stubbed here — the render harness (vitest.setup.render.js)
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
  // Dialogs / async — only reached from handlers; safe resolved stubs.
  confirmDialog: vi.fn().mockResolvedValue(false),
  promptAdminPassword: vi.fn().mockResolvedValue(null),
  promptDialog: vi.fn().mockResolvedValue(null),
  API: {
    estimateCompetitionSchedule: vi.fn().mockResolvedValue(null),
    swissGenerateRound: vi.fn().mockResolvedValue(null),
    updateCompetitionAwards: vi.fn().mockResolvedValue(null),
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
// fetch) flush and settle their state updates inside the act boundary — the
// render harness fails the test on the "not wrapped in act(...)" console.error
// otherwise. Returns the render result; a throw during mount fails the test.
async function mountSection(section, { comp = makeCompetition(), tweaks = {} } = {}) {
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
        bracket={null}
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
