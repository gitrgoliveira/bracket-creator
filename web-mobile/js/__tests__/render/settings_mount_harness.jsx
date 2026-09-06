import React from 'react';
import { render, act } from '@testing-library/react';
import { vi, beforeAll, afterAll } from 'vitest';

// Shared mount harness for the AdminSettings render tests.
//
// AdminSettings is module-internal to admin_competition_settings.jsx, so a
// test cannot render it directly -- it has to come up through
// AdminCompetition with the "settings" tab selected, which in turn needs the
// whole admin shell stubbed out. That setup is ~75 lines and was copied
// verbatim into every test that needed it: by the time bc-symm added three
// more, four files carried a byte-identical mountSettings (md5-checked) and
// four near-identical STUBBED_GLOBALS blocks differing only in
// competitionKindLabel. A prop change on AdminCompetition meant four
// identical edits, and each new file's header said "harness copied from
// <the previous one>".
//
// Same pattern as score_editor_matrix_axes.js in this directory: a plain
// module the render tests import, not a vitest setup file.

const noop = () => {};

const Stub = (name) => {
  const C = () => <div data-stub={name} />;
  C.displayName = `Stub(${name})`;
  return C;
};

// Built per call rather than shared as a module constant: the API entries are
// vi.fn() instances, and handing the same mock objects to several test files
// would let call counts leak between them.
function defaultStubbedGlobals() {
  return {
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
    competitionKindLabel: () => 'Individual',
    formatDate: (d) => String(d ?? ''),
    matchMedia: () => ({
      matches: false,
      addEventListener: noop, removeEventListener: noop,
      addListener: noop, removeListener: noop,
    }),
    confirmDialog: vi.fn().mockResolvedValue(false),
    promptAdminPassword: vi.fn().mockResolvedValue(null),
    promptDialog: vi.fn().mockResolvedValue(null),
    API: {
      estimateCompetitionSchedule: vi.fn().mockResolvedValue(null),
      swissGenerateRound: vi.fn().mockResolvedValue(null),
      updateCompetitionAwards: vi.fn().mockResolvedValue(null),
      completeCompetition: vi.fn().mockResolvedValue({ status: 'completed' }),
      fetchDrawWarnings: vi.fn().mockResolvedValue([]),
      // saveNow runs a post-save clash check before navigating away.
      getScheduleClashes: vi.fn().mockResolvedValue([]),
    },
  };
}

// Module-local, so mountSettings below keeps the (comp, onUpdate) signature
// the call sites already use. vitest isolates the module registry per test
// file, so this is per-file state, not shared across the suite.
let AdminCompetition = null;

// Call at module scope in a render test. Registers the beforeAll/afterAll
// that install and restore the window globals, and imports the component
// under test. `overrides` is merged over the defaults -- pass
// `{ competitionKindLabel: () => 'Team' }` for a team-competition fixture.
export function installSettingsHarness(overrides = {}) {
  const stubs = { ...defaultStubbedGlobals(), ...overrides };
  const originals = {};

  beforeAll(async () => {
    for (const [k, v] of Object.entries(stubs)) {
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
    AdminCompetition = null;
  });

  // Returned so a test can assert against the very mocks that were installed
  // (e.g. expect(stubs.API.getScheduleClashes).toHaveBeenCalled()).
  return stubs;
}

// makeSettingsCompetition builds the competition fixture the settings render
// tests mount against (PR #416 finding 14): numberprefix_reprint_hint,
// qualifier_settings_save, pool_settings_error_gate and settings_review_round
// each carried a byte-near-identical version of this object, differing only
// in the fields their own scenario cares about (format, poolSize/poolWinners,
// extraQualifiers, players, ...). Each test file now passes only ITS OWN
// differences as `overrides`, the same "shared base, per-caller overrides"
// shape mountSettings' own stubs already use above.
export function makeSettingsCompetition(overrides = {}) {
  return {
    id: 'c1',
    name: 'Autumn Cup',
    status: 'setup',
    format: 'playoffs',
    kind: 'individual',
    teamSize: 0,
    teamMatchType: 'fixed',
    poolSize: 0,
    poolSizeMode: 'min',
    poolWinners: 0,
    extraQualifiers: '',
    players: [],
    courts: ['A'],
    startTime: '09:00',
    date: '',
    fightingSpiritAwards: [],
    swissCurrentRound: 0,
    swissRounds: 0,
    withZekkenName: false,
    engi: false,
    roundRobin: true,
    poolFormat: 'full',
    numberPrefix: 'K',
    ...overrides,
  };
}

// Mounts AdminCompetition on its settings tab for `comp` and returns whatever
// @testing-library/react's render() returned.
export async function mountSettings(comp, onUpdate) {
  if (!AdminCompetition) {
    throw new Error(
      'mountSettings: AdminCompetition is not loaded. Call installSettingsHarness() ' +
      'at module scope in this test file before using mountSettings.'
    );
  }
  const t = {
    name: 'Spring Taikai',
    courts: ['A', 'B'],
    competitions: [comp, { id: 'c2', name: 'Yudansha' }],
  };
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
        section="settings"
        onSection={noop}
        onBack={noop}
        onOpenCompetition={noop}
        onUpdate={onUpdate}
        onRefreshCompetition={noop}
        onMoveCourt={noop}
        onEditScore={noop}
        onLogout={noop}
        onViewerMode={noop}
        tweaks={{}}
        password=""
        showToast={noop}
      />
    );
  });
  return result;
}
