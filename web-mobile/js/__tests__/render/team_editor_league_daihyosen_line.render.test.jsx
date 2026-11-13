// F5: a league team encounter may end level, so the sheet offers no
// representative bout there. When the encounter IS level the sheet says why the
// daihyosen control a knockout sheet shows is absent, and where a tie that
// matters is settled. The line never shows on a knockout sheet (which has the
// control), on a mixed competition's pool match, or while a league encounter has
// a winner. These tests mount the REAL ScoreEditorModal dispatcher, as
// team_daihyosen_representative.render.test.jsx does.
import React from 'react';
import { render, act, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { makeFakeOutbox } from '../helpers/fake_outbox.js';

const SQUADS = {
  t1: [{ id: 'm1a', name: 'Alice', index: 1 }, { id: 'm2a', name: 'Brenda', index: 2 }],
  t2: [{ id: 'm1b', name: 'Carol', index: 1 }, { id: 'm2b', name: 'Dana', index: 2 }],
};

const STUBBED_GLOBALS = {
  isHikiwake: (_type) => false,
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  isKikenDecision: (_kind) => false,
  isTextEntry: () => false,
  isInteractiveTarget: () => false,
  confirmDialog: vi.fn().mockResolvedValue(true),
  API: {
    fetchCompetitionDetails: vi.fn().mockResolvedValue(null),
    fetchSquads: vi.fn(),
    fetchTeamLineup: vi.fn().mockResolvedValue({ saved: false, positions: {} }),
    fetchLineupInForce: vi.fn().mockResolvedValue({ saved: false, positions: {} }),
    recordScore: vi.fn().mockResolvedValue(undefined),
    recordDaihyosen: vi.fn(),
    removeDaihyosen: vi.fn(),
    putMatchLineup: vi.fn().mockResolvedValue({}),
    recordDecision: vi.fn(),
    addTeamMember: vi.fn(),
    renameTeamMember: vi.fn(),
    fetchMatchHistory: vi.fn().mockResolvedValue([]),
  },
  AdminLineupHelpers: { rosterFor: vi.fn().mockReturnValue([]) },
  compMatches: () => [],
  Term: ({ children }) => <span>{children}</span>,
  GlossaryHint: ({ name }) => <span title={name} />,
};

let restoreGlobals;
let ScoreEditorModal;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  await import('../../admin_scoring_modal.jsx');
  await import('../../admin_lineup.jsx');
  ScoreEditorModal = window.ScoreEditorModal;
});

afterAll(() => restoreGlobals());

beforeEach(() => {
  const outbox = makeFakeOutbox();
  Object.assign(window.API, outbox.api);
  window.API.fetchSquads.mockReset().mockResolvedValue(SQUADS);
  window.API.fetchLineupInForce.mockReset().mockResolvedValue({ saved: false, positions: {} });
});

// Three drawn bouts: the encounter is level on IV and PW (0-0 each side).
const DRAWN_BOUTS = [1, 2, 3].map((position) => ({
  position, sideA: '', sideB: '', ipponsA: [], ipponsB: [], winner: '', decision: 'hikiwake',
}));

// Bout 1 won by Team A on a men ippon pair: the encounter has a winner.
const A_WINS = [
  { position: 1, sideA: '', sideB: '', ipponsA: ['M', 'K'], ipponsB: [], winner: 'Team A', decision: 'fought' },
  ...DRAWN_BOUTS.slice(1),
];

function makeMatch(overrides = {}, subResults = DRAWN_BOUTS) {
  return {
    id: 'Pool A-1',
    compId: 'comp1',
    status: 'running',
    phase: 'pool',
    compFormat: 'league',
    court: 'A',
    compKind: 'team',
    teamSize: 3,
    sideA: { id: 't1', name: 'Team A' },
    sideB: { id: 't2', name: 'Team B' },
    ...overrides,
    subResults,
  };
}

async function mount(match) {
  let utils;
  await act(async () => {
    utils = render(
      <ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={vi.fn()} password="" selfReport={false} />,
    );
  });
  return utils;
}

describe('league team sheet: the line for a level encounter (F5)', () => {
  it('explains a level league encounter and where a tie that matters is settled', async () => {
    await mount(makeMatch());
    const line = screen.getByTestId('league-level-hint');
    expect(line.textContent).toMatch(/league encounter can end level/i);
    expect(line.textContent).toMatch(/counts as a draw/i);
    expect(line.textContent).toMatch(/Pools page offers a tie-breaker/i);
    // No representative bout is offered in a league.
    expect(screen.queryByTestId('scoring-modal-daihyosen-button')).toBeNull();
  });

  it('does not show while the league encounter has a winner', async () => {
    await mount(makeMatch({}, A_WINS));
    expect(screen.queryByTestId('league-level-hint')).toBeNull();
  });

  it('does not show before any bout has landed', async () => {
    await mount(makeMatch({}, []));
    expect(screen.queryByTestId('league-level-hint')).toBeNull();
  });

  it('does not show on a knockout sheet, which has the daihyosen control instead', async () => {
    await mount(makeMatch({ id: 'm-ko-1', phase: 'bracket', compFormat: 'knockout' }));
    expect(screen.queryByTestId('league-level-hint')).toBeNull();
    expect(screen.getByTestId('scoring-modal-daihyosen-button')).toBeTruthy();
  });

  it('does not show on a mixed competition\'s pool match', async () => {
    await mount(makeMatch({ compFormat: 'mixed' }));
    expect(screen.queryByTestId('league-level-hint')).toBeNull();
  });
});
