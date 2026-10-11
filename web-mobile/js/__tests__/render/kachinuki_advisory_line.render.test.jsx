// bc-kfnl (operator ruling 2026-09-24): in kachinuki bout mode the team score
// sheet shows one advisory line after the fought bouts and immediately before
// the live bout: each side's fighter on, fighters left and who is next, every
// fighter with their team-member number. The queue is the server's, read with
// API.fetchKachinukiRoster and keyed on a VALUE (the bout log's fighters and
// recorded marks, the result of every bout but the live one until Record bout
// records it, and the lineups as the server last answered them), never on the
// match object a broadcast re-creates.
//
// NEGATIVE PINS: the line is advisory only. A roster that says a side has
// nobody left must not arm End match, change its label, or hold back Record
// bout or Encho; the buttons read exactly as they do with no roster at all.
// There is no line in the completed (correction) view, for a team match that
// is not kachinuki, or when the read fails, and the line never asks for a
// lineup.
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

// The server's split (engine.KachinukiRoster): each side's fighter on, and the
// queue behind them, next first. The client never matches the two itself.
const FULL_ROSTER = {
  sideA: { lineupFound: true, on: { name: 'Kudo', memberId: 'a1' }, remaining: [
    { name: 'Mori', memberId: 'a2' }, { name: '', memberId: 'a3' },
  ] },
  sideB: { lineupFound: true, on: { name: 'Ueda', memberId: 'b2' }, remaining: [{ name: 'Sato', memberId: 'b3' }] },
};
// Aka's lineup has nobody behind Kudo.
const AKA_EXHAUSTED = {
  sideA: { lineupFound: true, on: { name: 'Kudo', memberId: 'a1' }, remaining: [] },
  sideB: FULL_ROSTER.sideB,
};
// Each team's lineup in force, as the sheet reads it on mount.
const LINEUPS = {
  'team-A': { positions: { '1': 'Kudo', '2': 'Mori', '3': '' }, memberIds: { '1': 'a1', '2': 'a2', '3': 'a3' } },
  'team-B': { positions: { '1': 'Taki', '2': 'Ueda', '3': 'Sato' }, memberIds: { '1': 'b1', '2': 'b2', '3': 'b3' } },
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
    fetchLineupInForce: vi.fn((_c, teamId) => Promise.resolve(LINEUPS[teamId] || null)),
    fetchKachinukiRoster,
  };
}

beforeEach(() => {
  window.compMatches = () => [];
  window.compMatchesForCompetition = () => [];
  stubApi(vi.fn().mockResolvedValue(FULL_ROSTER));
});

// Several turns: the competition read, then both lineup reads, then the roster
// read, each lands on its own.
async function settle() {
  for (let i = 0; i < 6; i++) {
    await act(async () => { await Promise.resolve(); });
  }
}

async function mount(match, onSubmit = vi.fn()) {
  let view;
  await act(async () => {
    view = render(<ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={onSubmit} password="secret" />);
  });
  await settle();
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
    // One read on mount: it waits for both lineup reads rather than reading
    // once without them and again when they land.
    expect(window.API.fetchLineupInForce).toHaveBeenCalledTimes(2);
    expect(window.API.fetchKachinukiRoster).toHaveBeenCalledTimes(1);

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
      sideA: { lineupFound: true, on: { name: 'Mori', memberId: 'a2' }, remaining: [{ name: '', memberId: 'a3' }] },
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

  it('Record bout at exhaustion reads once and takes the beaten fighter off, on every mount', async () => {
    // Bout 2: Kudo leads Ueda, Shiro's last fighter. The lead is not a result
    // (bc-kfnl): the server keeps Ueda on until Record bout records the bout.
    const BOUT_1_RECORDED = { ...BOUT_1, recorded: true };
    const leading = { ...BOUT_2, winner: 'Kudo', winnerMemberId: 'a1', decision: 'fought', ipponsA: ['M', 'K'] };
    const live = kachinukiMatch({ subResults: [BOUT_1_RECORDED, leading] });
    const recorded = kachinukiMatch({ subResults: [BOUT_1_RECORDED, { ...leading, recorded: true }] });
    const uedaLast = {
      sideA: FULL_ROSTER.sideA,
      sideB: { lineupFound: true, on: { name: 'Ueda', memberId: 'b2' }, remaining: [] },
    };
    // The server's Record bout verdict once the bout is recorded: no pairing
    // follows and Shiro's lineup has nobody left.
    const exhausted = {
      nextPairing: false,
      sideA: { ...FULL_ROSTER.sideA, exhausted: false },
      sideB: { lineupFound: true, on: null, remaining: [], exhausted: true },
    };
    stubApi(vi.fn().mockResolvedValue(uedaLast));
    // Record bout appends nothing (Shiro has nobody left) but the answer
    // carries the bout as recorded.
    const onSubmit = vi.fn(async (patch) => ({ ...recorded, ...patch, subResults: recorded.subResults, modifiedAt: 1234 }));
    const view = await mount(live, onSubmit);
    const aka = 'Aka: T2.1 Kudo on, 2 left (T2.2 Mori, T2.3)';
    expect(advisory().textContent).toBe(`Shiro: T1.2 Ueda on, last fighter · ${aka}`);
    expect(window.API.fetchKachinukiRoster).toHaveBeenCalledTimes(1);

    // Once Record bout has recorded the bout, the server's roster has Ueda off.
    window.API.fetchKachinukiRoster.mockResolvedValue(exhausted);
    const record = screen.getAllByRole('button').find(b => b.textContent === 'Record bout');
    await act(async () => { fireEvent.click(record); });
    await settle();
    expect(onSubmit).toHaveBeenCalledTimes(1);
    const sent = onSubmit.mock.calls[0][0];
    expect(sent.kachinukiBoutFinal).toBe(true);
    // Review round 9, finding 6: no row carries the recorded mark on the
    // wire, not even the recorded bout 1 (only the server writes it, and it
    // ignores what a write says).
    expect(sent.subResults.find(s => s.position === 1).recorded).toBeUndefined();
    expect(sent.subResults.find(s => s.position === 2).recorded).toBeUndefined();

    // The sheet shows the answer's recorded bout at once (review round 5,
    // finding 1: it holds the mark until a newer stamp), and again once the
    // host passes it: the roster reads once more, Ueda off. No pairing
    // follows, so the recorded bout renders read-only and the no-pairing line
    // stands in the advisory's place (bc-kbrc).
    await remount(view, recorded);
    await settle();
    expect(window.API.fetchKachinukiRoster).toHaveBeenCalledTimes(2);
    const line = 'Shiro has no fighters left. End match finishes the encounter.';
    expect(advisory()).toBeNull();
    expect(screen.getByTestId('kachinuki-no-pairing').textContent).toBe(line);

    // Reopening the sheet (a reload, another device) reads the same line.
    view.unmount();
    stubApi(vi.fn().mockResolvedValue(exhausted));
    await mount(JSON.parse(JSON.stringify(recorded)));
    expect(window.API.fetchKachinukiRoster).toHaveBeenCalledTimes(1);
    expect(window.API.fetchKachinukiRoster).toHaveBeenCalledWith('comp1', 'm1');
    expect(screen.getByTestId('kachinuki-no-pairing').textContent).toBe(line);
  });

  it('reads nothing for a point, a lead or a 1-1 on the live bout, and once when Record bout records it', async () => {
    const view = await mount(kachinukiMatch());
    expect(window.API.fetchKachinukiRoster).toHaveBeenCalledTimes(1);
    // A point on the live bout reads nothing.
    await remount(view, kachinukiMatch({ subResults: [BOUT_1, { ...BOUT_2, ipponsA: ['M'], ipponsB: [] }] }));
    await settle();
    expect(window.API.fetchKachinukiRoster).toHaveBeenCalledTimes(1);
    // Nor does the winner the running autosave writes at a 1-0 lead, or the
    // hikiwake it writes at 1-1: neither is a result until the bout is
    // recorded (bc-kfnl).
    const won = { ...BOUT_2, winner: 'Kudo', winnerMemberId: 'a1', decision: 'fought', ipponsA: ['M'] };
    await remount(view, kachinukiMatch({ subResults: [BOUT_1, won] }));
    await settle();
    expect(window.API.fetchKachinukiRoster).toHaveBeenCalledTimes(1);
    await remount(view, kachinukiMatch({ subResults: [BOUT_1, { ...BOUT_2, decision: 'hikiwake', ipponsA: ['M'], ipponsB: ['K'] }] }));
    await settle();
    expect(window.API.fetchKachinukiRoster).toHaveBeenCalledTimes(1);
    // Record bout recording the bout reads once.
    await remount(view, kachinukiMatch({ subResults: [BOUT_1, { ...won, recorded: true }] }));
    await settle();
    expect(window.API.fetchKachinukiRoster).toHaveBeenCalledTimes(2);
  });

  it('reads again once when a lineup change is announced and read, not before and not for the same lineup', async () => {
    await mount(kachinukiMatch());
    expect(window.API.fetchKachinukiRoster).toHaveBeenCalledTimes(1);
    const announce = async (teamId) => {
      await act(async () => {
        window.dispatchEvent(new CustomEvent('lineup-updated', { detail: { competitionId: 'comp1', teamId } }));
      });
      await settle();
    };

    // An announcement whose read answers the lineup already read: nothing.
    await announce('team-A');
    expect(window.API.fetchLineupInForce).toHaveBeenCalledTimes(3);
    expect(window.API.fetchKachinukiRoster).toHaveBeenCalledTimes(1);

    // A save that landed (from another device, or this one's from the outbox)
    // is announced; the server now holds it, so the roster is read once.
    const saved = { positions: { '1': 'Kudo', '2': '', '3': 'Mori' }, memberIds: { '1': 'a1', '3': 'a2' } };
    window.API.fetchLineupInForce.mockImplementation((_c, teamId) => Promise.resolve(teamId === 'team-A' ? saved : LINEUPS[teamId]));
    await announce('team-A');
    expect(window.API.fetchKachinukiRoster).toHaveBeenCalledTimes(2);

    // While this device still holds a save of that lineup in its outbox, the
    // sheet keeps showing its own pick, but the roster follows what the server
    // answered: a change made elsewhere meanwhile is read once.
    window.API.queuedLineupSave = vi.fn().mockReturnValue(true);
    const elsewhere = { positions: { '1': 'Kudo', '2': 'Mori', '3': '' }, memberIds: { '1': 'a1', '2': 'a2' } };
    window.API.fetchLineupInForce.mockImplementation((_c, teamId) => Promise.resolve(teamId === 'team-A' ? elsewhere : LINEUPS[teamId]));
    await announce('team-A');
    expect(window.API.fetchKachinukiRoster).toHaveBeenCalledTimes(3);
    // The other team's announcement with its lineup unchanged reads nothing.
    await announce('team-B');
    expect(window.API.fetchKachinukiRoster).toHaveBeenCalledTimes(3);
  });

  it('reads once, not twice, when a side is given another team', async () => {
    window.API.fetchLineupInForce.mockImplementation((_c, teamId) => Promise.resolve(
      teamId === 'team-C'
        ? { positions: { '1': 'Kudo', '2': 'Mori', '3': '' }, memberIds: { '1': 'a1', '2': 'a2', '3': 'a3' } }
        : LINEUPS[teamId] || null,
    ));
    const view = await mount(kachinukiMatch());
    expect(window.API.fetchKachinukiRoster).toHaveBeenCalledTimes(1);
    await act(async () => {
      view.rerender(<ScoreEditorModal match={kachinukiMatch({ sideA: { id: 'team-C', name: 'Team C', number: 'T3' } })} onClose={vi.fn()} onSubmit={vi.fn()} password="secret" />);
    });
    await settle();
    expect(window.API.fetchLineupInForce).toHaveBeenCalledWith('comp1', 'team-C', 'm1');
    expect(window.API.fetchKachinukiRoster).toHaveBeenCalledTimes(2);
  });

  it('reads once more, not twice, when Record bout appends the next pairing', async () => {
    const live = kachinukiMatch({
      subResults: [BOUT_1, { ...BOUT_2, winner: 'Ueda', winnerMemberId: 'b2', ipponsB: ['K'] }],
    });
    const appended = [
      ...live.subResults,
      { position: 3, sideA: 'Mori', sideAMemberId: 'a2', sideB: 'Ueda', sideBMemberId: 'b2', ipponsA: [], ipponsB: [] },
    ];
    const onSubmit = vi.fn(async (patch) => ({ ...live, ...patch, subResults: appended, modifiedAt: 1234 }));
    const view = await mount(live, onSubmit);
    expect(window.API.fetchKachinukiRoster).toHaveBeenCalledTimes(1);

    const record = screen.getAllByRole('button').find(b => b.textContent === 'Record bout');
    await act(async () => { fireEvent.click(record); });
    await settle();
    // The answer's log grew past the recorded bout: the append alone re-keys.
    expect(window.API.fetchKachinukiRoster).toHaveBeenCalledTimes(1);

    await act(async () => {
      view.rerender(<ScoreEditorModal match={{ ...live, subResults: appended }} onClose={vi.fn()} onSubmit={onSubmit} password="secret" />);
    });
    await settle();
    expect(window.API.fetchKachinukiRoster).toHaveBeenCalledTimes(2);
    expect(window.API.fetchKachinukiRoster).toHaveBeenLastCalledWith('comp1', 'm1');
  });

  it('never asks for a lineup', async () => {
    stubApi(vi.fn().mockResolvedValue({ sideA: { lineupFound: false, on: FULL_ROSTER.sideA.on, remaining: [] }, sideB: FULL_ROSTER.sideB }));
    await mount(kachinukiMatch());
    expect(advisory().textContent).toBe('Shiro: T1.2 Ueda on, 1 left (T1.3 Sato) · Aka: T2.1 Kudo on');
    expect(advisory().textContent).not.toMatch(/lineup|incomplete|add/i);
  });

  it('shows no line when neither side has a lineup', async () => {
    stubApi(vi.fn().mockResolvedValue({
      sideA: { lineupFound: false, on: FULL_ROSTER.sideA.on, remaining: [] },
      sideB: { lineupFound: false, on: FULL_ROSTER.sideB.on, remaining: [] },
    }));
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
