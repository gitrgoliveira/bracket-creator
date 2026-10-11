// bc-kfnl (operator ruling 2026-09-24): in kachinuki bout mode the team score
// sheet shows one advisory line after the fought bouts and immediately before
// the live bout: each side's fighter on, fighters left and who is next, every
// fighter with their team-member number. The queue is the server's, read with
// API.fetchKachinukiRoster and keyed on a VALUE (the bout log), never on the
// match object a broadcast re-creates.
//
// NEGATIVE PINS: the line is advisory only. A roster that says a side has
// nobody left must not arm End match, change its label, or hold back Record
// bout or Encho; the buttons read exactly as they do with no roster at all.
// There is no line in the completed (correction) view, for a team match that
// is not kachinuki, or when the read fails, and the line never asks for a
// lineup.
import React from 'react';
import { render, act, screen } from '@testing-library/react';
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

// Aka is the match's side A (team-A, number T2), Shiro side B (team-B, T1).
const SQUADS = {
  'team-A': [
    { id: 'a1', index: 1, name: 'Kudo' },
    { id: 'a2', index: 2, name: 'Mori' },
    { id: 'a3', index: 3, name: '' },
  ],
  'team-B': [
    { id: 'b1', index: 1, name: 'Taki' },
    { id: 'b2', index: 2, name: 'Ueda' },
    { id: 'b3', index: 3, name: 'Sato' },
  ],
};

// Bout 1: Kudo (Aka) beat Taki (Shiro). Bout 2, live: Kudo against Ueda.
const BOUT_1 = {
  position: 1, sideA: 'Kudo', sideAMemberId: 'a1', sideB: 'Taki', sideBMemberId: 'b1',
  winner: 'Kudo', winnerMemberId: 'a1', decision: 'fought', ipponsA: ['M'], ipponsB: [],
};
const BOUT_2 = { position: 2, sideA: 'Kudo', sideAMemberId: 'a1', sideB: 'Ueda', sideBMemberId: 'b2', ipponsA: [], ipponsB: [] };

function kachinukiMatch(overrides = {}) {
  return {
    id: 'm1',
    compId: 'comp1',
    status: 'running',
    phase: 'pool',
    poolName: 'Pool 1',
    court: 'A',
    compKind: 'team',
    teamSize: 3,
    compFormat: 'mixed',
    teamMatchType: 'kachinuki',
    sideA: { id: 'team-A', name: 'Team A', number: 'T2' },
    sideB: { id: 'team-B', name: 'Team B', number: 'T1' },
    subResults: [BOUT_1, BOUT_2],
    ...overrides,
  };
}

const FULL_ROSTER = {
  sideA: { lineupFound: true, remaining: [
    { name: 'Kudo', memberId: 'a1' }, { name: 'Mori', memberId: 'a2' }, { name: '', memberId: 'a3' },
  ] },
  sideB: { lineupFound: true, remaining: [{ name: 'Ueda', memberId: 'b2' }, { name: 'Sato', memberId: 'b3' }] },
};
// Aka's lineup has nobody behind Kudo.
const AKA_EXHAUSTED = {
  sideA: { lineupFound: true, remaining: [{ name: 'Kudo', memberId: 'a1' }] },
  sideB: FULL_ROSTER.sideB,
};

function stubApi(fetchKachinukiRoster) {
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
    fetchKachinukiRoster,
  };
}

beforeEach(() => {
  window.compMatches = () => [];
  window.compMatchesForCompetition = () => [];
  stubApi(vi.fn().mockResolvedValue(FULL_ROSTER));
});

async function mount(match) {
  let view;
  await act(async () => {
    view = render(<ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={vi.fn()} password="secret" />);
  });
  await act(async () => { await Promise.resolve(); });
  return view;
}

async function remount(view, match) {
  await act(async () => {
    view.rerender(<ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={vi.fn()} password="secret" />);
  });
  await act(async () => { await Promise.resolve(); });
}

const advisory = () => screen.queryByTestId('kachinuki-advisory');

function buttonStates() {
  const end = screen.getByTestId('kachinuki-end-match-button');
  const record = screen.getAllByRole('button').find(b => b.textContent === 'Record bout');
  const encho = screen.queryByTestId('kachinuki-encho-button');
  return {
    endLabel: end.textContent,
    endArmed: end.classList.contains('btn--confirm'),
    endDisabled: end.disabled,
    recordDisabled: record ? record.disabled : 'absent',
    enchoDisabled: encho ? encho.disabled : 'absent',
  };
}

describe('the kachinuki advisory line', () => {
  it('shows each side on, left and next, after the fought bouts and before the live bout', async () => {
    await mount(kachinukiMatch());
    const line = advisory();
    expect(line).not.toBeNull();
    expect(line.textContent).toBe('Shiro: T1.2 Ueda on, 1 left (T1.3 Sato) · Aka: T2.1 Kudo on, 2 left (T2.2 Mori, T2.3)');
    expect(window.API.fetchKachinukiRoster).toHaveBeenCalledWith('comp1', 'm1');

    const fought = screen.getByTestId('kachinuki-done-bout-0');
    expect(fought.compareDocumentPosition(line) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    const liveButtons = document.querySelector('.team-sub-match__btns');
    expect(liveButtons).not.toBeNull();
    expect(line.compareDocumentPosition(liveButtons) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it('reads again when a bout is recorded, and not for a new object with the same bout log', async () => {
    const view = await mount(kachinukiMatch());
    expect(window.API.fetchKachinukiRoster).toHaveBeenCalledTimes(1);

    await remount(view, JSON.parse(JSON.stringify(kachinukiMatch())));
    expect(window.API.fetchKachinukiRoster).toHaveBeenCalledTimes(1);

    // Bout 2 recorded (Ueda beat Kudo); the server appended Mori against Ueda.
    window.API.fetchKachinukiRoster.mockResolvedValue({
      sideA: { lineupFound: true, remaining: [{ name: 'Mori', memberId: 'a2' }, { name: '', memberId: 'a3' }] },
      sideB: FULL_ROSTER.sideB,
    });
    await remount(view, kachinukiMatch({
      subResults: [
        BOUT_1,
        { ...BOUT_2, winner: 'Ueda', winnerMemberId: 'b2', decision: 'fought', ipponsB: ['K'] },
        { position: 3, sideA: 'Mori', sideAMemberId: 'a2', sideB: 'Ueda', sideBMemberId: 'b2', ipponsA: [], ipponsB: [] },
      ],
    }));
    expect(window.API.fetchKachinukiRoster).toHaveBeenCalledTimes(2);
    expect(advisory().textContent).toBe('Shiro: T1.2 Ueda on, 1 left (T1.3 Sato) · Aka: T2.2 Mori on, 1 left (T2.3)');
  });

  it('reads "last fighter" and leaves every button as it is with no roster', async () => {
    stubApi(vi.fn().mockResolvedValue(null));
    const bare = await mount(kachinukiMatch());
    expect(advisory()).toBeNull();
    const without = buttonStates();
    bare.unmount();

    stubApi(vi.fn().mockResolvedValue(AKA_EXHAUSTED));
    await mount(kachinukiMatch());
    expect(advisory().textContent).toContain('Aka: T2.1 Kudo on, last fighter');
    const withLine = buttonStates();
    expect(withLine).toEqual(without);
    expect(withLine.endArmed).toBe(false);
    expect(withLine.endLabel).toBe('End match');
  });

  it('never asks for a lineup', async () => {
    stubApi(vi.fn().mockResolvedValue({ sideA: { lineupFound: false, remaining: [] }, sideB: FULL_ROSTER.sideB }));
    await mount(kachinukiMatch());
    expect(advisory().textContent).toBe('Shiro: T1.2 Ueda on, 1 left (T1.3 Sato) · Aka: T2.1 Kudo on');
    expect(advisory().textContent).not.toMatch(/lineup|incomplete|add/i);
  });

  it('shows no line when neither side has a lineup', async () => {
    stubApi(vi.fn().mockResolvedValue({ sideA: { lineupFound: false, remaining: [] }, sideB: { lineupFound: false, remaining: [] } }));
    await mount(kachinukiMatch());
    expect(advisory()).toBeNull();
  });

  it('shows no line when the read fails', async () => {
    stubApi(vi.fn().mockRejectedValue(new Error('offline')));
    await mount(kachinukiMatch());
    expect(advisory()).toBeNull();
    expect(screen.getByTestId('kachinuki-end-match-button')).toBeTruthy();
  });

  it('shows no line in the completed (correction) view', async () => {
    await mount(kachinukiMatch({
      status: 'completed', winner: 'Team A', decision: 'kachinuki-exhaustion',
      subResults: [BOUT_1, { ...BOUT_2, winner: 'Kudo', winnerMemberId: 'a1', decision: 'fought', ipponsA: ['M'] }],
    }));
    expect(advisory()).toBeNull();
    expect(window.API.fetchKachinukiRoster).not.toHaveBeenCalled();
  });

  it('shows no line for a team match that is not kachinuki', async () => {
    window.API.fetchCompetitionDetails = vi.fn().mockResolvedValue({
      id: 'comp1', config: { format: 'mixed', teamMatchType: '', naginata: false, players: [] },
    });
    await mount(kachinukiMatch({ teamMatchType: '' }));
    expect(advisory()).toBeNull();
    expect(window.API.fetchKachinukiRoster).not.toHaveBeenCalled();
  });
});
