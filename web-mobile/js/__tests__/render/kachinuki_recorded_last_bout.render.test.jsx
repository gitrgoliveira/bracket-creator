// bc-kbrc: when Record bout records the last bout and appends no pairing (a
// side has nobody left), that bout reads as recorded: it renders read-only
// like every other recorded bout, correctable through the same tap, and one
// line stands where the live bout was, naming who has nobody left and that
// End match finishes the encounter. It comes from the server's stored mark
// (subResults[].recorded), so a reload and a second device show the same.
// Record bout cannot record it again; "+ Add next bout manually" brings a
// live bout back. A knockout tie keeps its bout live (Encho is the way on
// there), and a bout scored but not recorded stays live as before.
import React from 'react';
import { render, act, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

const STUBBED_GLOBALS = {
  isHikiwake: () => false,
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  isKikenDecision: () => false,
  isTextEntry: () => false,
  isInteractiveTarget: () => false,
  confirmDialog: vi.fn().mockResolvedValue(true),
  API: {},
  AdminLineupHelpers: { rosterFor: vi.fn().mockReturnValue([]) },
  compMatches: () => [],
  compMatchesForCompetition: () => [],
  Term: ({ children }) => <span>{children}</span>,
  GlossaryHint: ({ name }) => <span title={name} />,
};

let restoreGlobals;
let ScoreEditorModal;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  await import('../../admin_scoring_modal.jsx');
  ScoreEditorModal = window.ScoreEditorModal;
});

afterAll(() => restoreGlobals());

const SQUADS = {
  'team-A': [{ id: 'a1', index: 1, name: 'Kudo' }, { id: 'a2', index: 2, name: 'Mori' }],
  'team-B': [{ id: 'b1', index: 1, name: 'Taki' }, { id: 'b2', index: 2, name: 'Ueda' }],
};
const LINEUPS = {
  'team-A': { positions: { '1': 'Kudo', '2': 'Mori' }, memberIds: { '1': 'a1', '2': 'a2' } },
  'team-B': { positions: { '1': 'Taki', '2': 'Ueda' }, memberIds: { '1': 'b1', '2': 'b2' } },
};

function stubApi(roster) {
  window.API = {
    fetchCompetitionDetails: vi.fn().mockResolvedValue({
      id: 'comp1',
      config: { format: 'mixed', teamMatchType: 'kachinuki', naginata: false, players: [] },
    }),
    recordScore: vi.fn().mockResolvedValue(undefined),
    recordDaihyosen: vi.fn(),
    removeDaihyosen: vi.fn(),
    putMatchLineup: vi.fn(),
    recordDecision: vi.fn(),
    fetchSquads: vi.fn().mockResolvedValue(SQUADS),
    fetchLineupInForce: vi.fn((_c, teamId) => Promise.resolve(LINEUPS[teamId] || null)),
    fetchKachinukiRoster: vi.fn().mockResolvedValue(roster),
  };
}

beforeEach(() => stubApi(null));

// Kudo (Aka) beat Taki, then Ueda, Shiro's last fighter: both recorded.
const BOUT_1 = {
  position: 1, sideA: 'Kudo', sideAMemberId: 'a1', sideB: 'Taki', sideBMemberId: 'b1',
  winner: 'Kudo', winnerMemberId: 'a1', decision: 'fought', ipponsA: ['M'], ipponsB: [], recorded: true,
};
const BOUT_2 = {
  position: 2, sideA: 'Kudo', sideAMemberId: 'a1', sideB: 'Ueda', sideBMemberId: 'b2',
  winner: 'Kudo', winnerMemberId: 'a1', decision: 'fought', ipponsA: ['M'], ipponsB: [], recorded: true,
};
const SHIRO_OUT = {
  sideA: { lineupFound: true, on: { name: 'Kudo', memberId: 'a1' }, remaining: [{ name: 'Mori', memberId: 'a2' }] },
  sideB: { lineupFound: true, on: null, remaining: [] },
};
const BOTH_OUT = {
  sideA: { lineupFound: true, on: null, remaining: [] },
  sideB: { lineupFound: true, on: null, remaining: [] },
};

function kachinukiMatch(subResults, phase = 'pool') {
  return {
    id: 'm1', compId: 'comp1', status: 'running', phase, poolName: 'Pool 1', court: 'A',
    compKind: 'team', teamSize: 2, compFormat: 'mixed', teamMatchType: 'kachinuki',
    sideA: { id: 'team-A', name: 'Team A', number: 'T2' },
    sideB: { id: 'team-B', name: 'Team B', number: 'T1' },
    subResults,
  };
}

async function settle() {
  for (let i = 0; i < 6; i++) await act(async () => { await Promise.resolve(); });
}

async function mount(match, extra = {}) {
  let view;
  await act(async () => {
    view = render(<ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={vi.fn()} password="secret" {...extra} />);
  });
  await settle();
  return view;
}

const liveRows = (container) => container.querySelectorAll('.team-sub-match:not(.team-sub-match--readonly)');
const recordButton = () => screen.getAllByRole('button').find(b => b.textContent === 'Record bout');

describe('a recorded last kachinuki bout with no pairing after it', () => {
  it('renders read-only with the others, and the line says who has nobody left', async () => {
    stubApi(SHIRO_OUT);
    const { container } = await mount(kachinukiMatch([BOUT_1, BOUT_2]));
    expect(screen.getByTestId('kachinuki-done-bout-0')).toBeTruthy();
    expect(screen.getByTestId('kachinuki-done-bout-1')).toBeTruthy();
    expect(liveRows(container).length).toBe(0);
    expect(screen.getByTestId('kachinuki-no-pairing').textContent)
      .toBe('Shiro has no fighters left. End match finishes the encounter.');
    expect(screen.queryByTestId('kachinuki-advisory')).toBeNull();
    // Record bout would record the same bout again; End match finishes.
    expect(recordButton().disabled).toBe(true);
    expect(screen.getByTestId('kachinuki-end-match-button').disabled).toBe(false);
    expect(screen.queryByTestId('kachinuki-encho-button')).toBeNull();
    expect(screen.queryByTestId('kachinuki-remove-bout-button')).toBeNull();
    expect(screen.queryByTestId('kachinuki-end-hint')).toBeNull();
    expect(screen.queryByTestId('kachinuki-record-hint')).toBeNull();
    expect(screen.getByTestId('kachinuki-add-bout-button').disabled).toBe(false);
  });

  it('is correctable through the same tap as an earlier bout', async () => {
    stubApi(SHIRO_OUT);
    const { container } = await mount(kachinukiMatch([BOUT_1, BOUT_2]));
    await act(async () => { fireEvent.click(screen.getByTestId('kachinuki-done-bout-1'), { detail: 1 }); });
    await settle();
    expect(screen.queryByTestId('kachinuki-done-bout-1')).toBeNull();
    expect(screen.getByTestId('kachinuki-done-bout-0')).toBeTruthy();
    expect(liveRows(container).length).toBe(1);
  });

  it('"+ Add next bout manually" brings a live bout back', async () => {
    stubApi(SHIRO_OUT);
    const { container } = await mount(kachinukiMatch([BOUT_1, BOUT_2]));
    await act(async () => { fireEvent.click(screen.getByTestId('kachinuki-add-bout-button')); });
    await settle();
    expect(screen.getByTestId('kachinuki-done-bout-1')).toBeTruthy();
    expect(liveRows(container).length).toBe(1);
    expect(screen.queryByTestId('kachinuki-no-pairing')).toBeNull();
  });

  it('stays live, as before, when no lineup says a side has nobody left', async () => {
    // No roster read (no lineups), and a roster whose lineups were not found.
    for (const roster of [null, {
      sideA: { lineupFound: false, on: { name: 'Kudo', memberId: 'a1' }, remaining: [] },
      sideB: { lineupFound: false, on: null, remaining: [] },
    }]) {
      stubApi(roster);
      const view = await mount(JSON.parse(JSON.stringify(kachinukiMatch([BOUT_1, BOUT_2]))));
      expect(liveRows(view.container).length).toBe(1);
      expect(screen.getByTestId('kachinuki-done-bout-0')).toBeTruthy();
      expect(screen.queryByTestId('kachinuki-done-bout-1')).toBeNull();
      expect(screen.queryByTestId('kachinuki-no-pairing')).toBeNull();
      expect(recordButton().disabled).toBe(false);
      view.unmount();
    }
  });

  it('a fresh mount keeps the bout live until the roster read lands, then closes it', async () => {
    let resolveRoster;
    stubApi(null);
    window.API.fetchKachinukiRoster = vi.fn(() => new Promise((r) => { resolveRoster = r; }));
    const { container } = await mount(JSON.parse(JSON.stringify(kachinukiMatch([BOUT_1, BOUT_2]))));
    expect(window.API.fetchKachinukiRoster).toHaveBeenCalledTimes(1);
    expect(liveRows(container).length).toBe(1);
    expect(screen.queryByTestId('kachinuki-no-pairing')).toBeNull();
    await act(async () => { resolveRoster(SHIRO_OUT); });
    await settle();
    expect(liveRows(container).length).toBe(0);
    expect(screen.getByTestId('kachinuki-no-pairing').textContent)
      .toBe('Shiro has no fighters left. End match finishes the encounter.');
  });

  it('names both sides after a recorded draw that left nobody, and never reads "Tied bout"', async () => {
    stubApi(BOTH_OUT);
    const draw = { ...BOUT_2, winner: '', winnerMemberId: '', decision: 'hikiwake', ipponsA: ['M'], ipponsB: ['K'] };
    const { container } = await mount(kachinukiMatch([BOUT_1, draw]));
    expect(liveRows(container).length).toBe(0);
    expect(screen.getByTestId('kachinuki-no-pairing').textContent)
      .toBe('Shiro and Aka have no fighters left. End match finishes the encounter.');
    expect(screen.queryByTestId('kachinuki-end-hint')).toBeNull();
  });

  it('shows the same on the self-run public score sheet', async () => {
    stubApi(SHIRO_OUT);
    // The props MatchViewerModal (viewer_match.jsx) hands the editor on the
    // public page: no password, selfReport, and the team members from the
    // viewer payload. ScoreEditorModal forwards all three to the team editor.
    const { container } = await mount(kachinukiMatch([BOUT_1, BOUT_2]), { selfReport: true, password: '', teamMembers: SQUADS });
    expect(window.API.fetchSquads).not.toHaveBeenCalled();
    expect(window.API.fetchKachinukiRoster).toHaveBeenCalledWith('comp1', 'm1');
    expect(liveRows(container).length).toBe(0);
    expect(screen.getByTestId('kachinuki-no-pairing').textContent)
      .toBe('Shiro has no fighters left. End match finishes the encounter.');
  });

  it('keeps a recorded knockout tie live, where Encho is the way on', async () => {
    stubApi(BOTH_OUT);
    const draw = { ...BOUT_2, winner: '', winnerMemberId: '', decision: 'hikiwake', ipponsA: ['M'], ipponsB: ['K'] };
    const { container } = await mount(kachinukiMatch([BOUT_1, draw], 'bracket'));
    expect(liveRows(container).length).toBe(1);
    expect(screen.queryByTestId('kachinuki-no-pairing')).toBeNull();
    expect(screen.getByTestId('kachinuki-encho-button')).toBeTruthy();
  });

  it('keeps a scored but unrecorded last bout live', async () => {
    stubApi(SHIRO_OUT);
    const { recorded: _r, ...unrecorded } = BOUT_2;
    const { container } = await mount(kachinukiMatch([BOUT_1, unrecorded]));
    expect(liveRows(container).length).toBe(1);
    expect(screen.queryByTestId('kachinuki-no-pairing')).toBeNull();
    expect(recordButton().disabled).toBe(false);
  });
});
