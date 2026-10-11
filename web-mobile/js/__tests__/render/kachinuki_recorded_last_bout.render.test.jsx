// bc-kbrc: when Record bout records the last bout and appends no pairing (a
// side has nobody left), that bout reads as recorded: it renders read-only
// like every other recorded bout, correctable through the same tap, and one
// line stands where the live bout was, naming who has nobody left and that
// End match finishes the encounter. It comes from the server's stored mark
// (subResults[].recorded), so a reload and a second device show the same.
// Record bout cannot record it again; "+ Add next bout manually" brings a
// live bout back. A recorded draw keeps its bout live in every phase (Encho on
// it is the operator's call), and a bout scored but not recorded stays live as
// before.
import React from 'react';
import { render, act, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { keepNewerMatches, keepNewerCompetitions } from '../../patch.jsx';

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
// The server's Record bout verdict (engine.KachinukiRoster): no pairing
// follows, and the loser's lineup has nobody left.
const SHIRO_OUT = {
  nextPairing: false,
  sideA: { lineupFound: true, on: { name: 'Kudo', memberId: 'a1' }, remaining: [{ name: 'Mori', memberId: 'a2' }], exhausted: false },
  sideB: { lineupFound: true, on: null, remaining: [], exhausted: true },
};
// A forged answer the server never gives (it exhausts at most one side, and
// never after a draw): the sheet's own draw rule must still keep a recorded
// draw live.
const BOTH_OUT = {
  nextPairing: false,
  sideA: { lineupFound: true, on: null, remaining: [], exhausted: true },
  sideB: { lineupFound: true, on: null, remaining: [], exhausted: true },
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
    // No roster read, and the server's answer for a loser with no lineup: no
    // pairing follows, but no side is exhausted (the bouts alone cannot say).
    // Review round 9, finding 2: with that answer Record bout would write
    // nothing, so it is disabled; with no read it stays enabled.
    for (const [roster, recordDisabled] of [[null, false], [{
      nextPairing: false,
      sideA: { lineupFound: true, on: { name: 'Kudo', memberId: 'a1' }, remaining: [], exhausted: false },
      sideB: { lineupFound: false, on: null, remaining: [], exhausted: false },
    }, true]]) {
      stubApi(roster);
      const view = await mount(JSON.parse(JSON.stringify(kachinukiMatch([BOUT_1, BOUT_2]))));
      expect(liveRows(view.container).length).toBe(1);
      expect(screen.getByTestId('kachinuki-done-bout-0')).toBeTruthy();
      expect(screen.queryByTestId('kachinuki-done-bout-1')).toBeNull();
      expect(screen.queryByTestId('kachinuki-no-pairing')).toBeNull();
      expect(recordButton().disabled).toBe(recordDisabled);
      view.unmount();
    }
  });

  it('keeps Record bout open on a recorded bout when the server says a pairing would follow', async () => {
    // A lineup saved after the bout was recorded gives the loser's side a
    // fighter: a tap now appends, so the button stays open and nothing closes.
    stubApi({
      nextPairing: true,
      sideA: { lineupFound: true, on: { name: 'Kudo', memberId: 'a1' }, remaining: [], exhausted: false },
      sideB: { lineupFound: true, on: null, remaining: [{ name: 'Sato', memberId: 'b3' }], exhausted: false },
    });
    const { container } = await mount(kachinukiMatch([BOUT_1, BOUT_2]));
    expect(liveRows(container).length).toBe(1);
    expect(screen.queryByTestId('kachinuki-no-pairing')).toBeNull();
    expect(recordButton().disabled).toBe(false);
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

  // Review round 4, finding 1: a recorded draw that is still the last bout
  // (both sides had nobody left) stays live in every phase, Encho offered: the
  // operator decides whether the pair fights on, End match decides the rest.
  // The roster read stubbed here says both sides are out (an answer the server
  // no longer gives for it), so the sheet's own rule is what keeps it live.
  it.each(['pool', 'bracket'])('keeps a recorded draw live in a %s match, Encho offered', async (phase) => {
    stubApi(BOTH_OUT);
    const draw = { ...BOUT_2, winner: '', winnerMemberId: '', decision: 'hikiwake', ipponsA: ['M'], ipponsB: ['K'] };
    const { container } = await mount(kachinukiMatch([BOUT_1, draw], phase));
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

// Review round 3, finding 2: the closed state keeps the last roster answer that
// landed for the same recorded bout while a newer read is out or after one
// fails, and the stored log decides first, at once.
describe('a closed recorded last bout and a roster re-read', () => {
  const announce = async (teamId) => {
    await act(async () => {
      window.dispatchEvent(new CustomEvent('lineup-updated', { detail: { competitionId: 'comp1', teamId } }));
    });
    await settle();
  };
  // A lineup the server now holds for Aka that differs from the one read.
  const changedLineupA = { positions: { '1': 'Kudo', '2': 'Mori', '3': '' }, memberIds: { '1': 'a1', '2': 'a2' }, matchId: 'm1' };
  const rerender = async (view, match) => {
    await act(async () => {
      view.rerender(<ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={vi.fn()} password="secret" />);
    });
    await settle();
  };
  const expectClosed = () => {
    expect(screen.getByTestId('kachinuki-no-pairing').textContent)
      .toBe('Shiro has no fighters left. End match finishes the encounter.');
  };

  it('does not reopen while a lineup-updated re-read is in flight', async () => {
    stubApi(SHIRO_OUT);
    const { container } = await mount(kachinukiMatch([BOUT_1, BOUT_2]));
    expect(liveRows(container).length).toBe(0);
    window.API.fetchKachinukiRoster = vi.fn(() => new Promise(() => {}));
    window.API.fetchLineupInForce.mockImplementation((_c, teamId) => Promise.resolve(teamId === 'team-A' ? changedLineupA : LINEUPS[teamId]));
    await announce('team-A');
    expect(window.API.fetchKachinukiRoster).toHaveBeenCalledTimes(1);
    expect(liveRows(container).length).toBe(0);
    expectClosed();
  });

  it('does not reopen when the re-read fails', async () => {
    stubApi(SHIRO_OUT);
    const { container } = await mount(kachinukiMatch([BOUT_1, BOUT_2]));
    expect(liveRows(container).length).toBe(0);
    // fetchKachinukiRoster answers a failed read with null.
    window.API.fetchKachinukiRoster = vi.fn().mockResolvedValue(null);
    window.API.fetchLineupInForce.mockImplementation((_c, teamId) => Promise.resolve(teamId === 'team-A' ? changedLineupA : LINEUPS[teamId]));
    await announce('team-A');
    expect(window.API.fetchKachinukiRoster).toHaveBeenCalledTimes(1);
    expect(liveRows(container).length).toBe(0);
    expectClosed();
  });

  it('reopens at once when the bout is un-recorded, before any re-read lands', async () => {
    stubApi(SHIRO_OUT);
    const view = await mount(kachinukiMatch([BOUT_1, BOUT_2]));
    expect(liveRows(view.container).length).toBe(0);
    window.API.fetchKachinukiRoster = vi.fn(() => new Promise(() => {}));
    const { recorded: _r, ...unrecorded } = BOUT_2;
    await rerender(view, kachinukiMatch([BOUT_1, unrecorded]));
    expect(liveRows(view.container).length).toBe(1);
    expect(screen.queryByTestId('kachinuki-no-pairing')).toBeNull();
  });
});

// Review round 3, finding 4: a knockout tie recorded when both teams had nobody
// left is fought on in encho on that bout. The server keeps both fighters on
// for it (engine.KachinukiRoster), and the write that starts the encho takes
// the recorded mark off, so the line names both on before and during encho.
describe('a recorded knockout tie fought on in encho', () => {
  const BOTH_ON = {
    nextPairing: false,
    sideA: { lineupFound: true, on: { name: 'Mori', memberId: 'a2' }, remaining: [], exhausted: false },
    sideB: { lineupFound: true, on: { name: 'Ueda', memberId: 'b2' }, remaining: [], exhausted: false },
  };
  const T1 = {
    position: 1, sideA: 'Kudo', sideAMemberId: 'a1', sideB: 'Taki', sideBMemberId: 'b1',
    winner: 'Taki', winnerMemberId: 'b1', decision: 'fought', ipponsA: [], ipponsB: ['M'], recorded: true,
  };
  const T2 = {
    position: 2, sideA: 'Mori', sideAMemberId: 'a2', sideB: 'Taki', sideBMemberId: 'b1',
    winner: 'Mori', winnerMemberId: 'a2', decision: 'fought', ipponsA: ['K'], ipponsB: [], recorded: true,
  };
  const TIE = {
    position: 3, sideA: 'Mori', sideAMemberId: 'a2', sideB: 'Ueda', sideBMemberId: 'b2',
    winner: '', winnerMemberId: '', decision: 'hikiwake', ipponsA: ['M'], ipponsB: ['K'], recorded: true,
  };

  it('names both fighters on after the recorded tie and in encho, the bout live', async () => {
    stubApi(BOTH_ON);
    const view = await mount(kachinukiMatch([T1, T2, TIE], 'bracket'));
    expect(liveRows(view.container).length).toBe(1);
    expect(screen.queryByTestId('kachinuki-no-pairing')).toBeNull();
    const line = () => screen.getByTestId('kachinuki-advisory').textContent;
    expect(line()).toContain('Ueda on');
    expect(line()).toContain('Mori on');
    expect(line()).not.toContain('no fighters left');

    // The Encho tap lands: the stored row is no longer recorded.
    const { recorded: _r, ...inEncho } = { ...TIE, decision: '', encho: { periodCount: 1 } };
    await act(async () => {
      view.rerender(<ScoreEditorModal match={kachinukiMatch([T1, T2, inEncho], 'bracket')} onClose={vi.fn()} onSubmit={vi.fn()} password="secret" />);
    });
    await settle();
    expect(liveRows(view.container).length).toBe(1);
    expect(line()).toContain('Ueda on');
    expect(line()).toContain('Mori on');
  });

  // Review round 8, finding 4: the recorded tie stays live, but Record bout
  // would write nothing on it, so it is disabled and says why.
  it('disables Record bout while the live bout is stored as recorded', async () => {
    stubApi(BOTH_ON);
    await mount(kachinukiMatch([T1, T2, TIE]));
    expect(recordButton().disabled).toBe(true);
    expect(recordButton().getAttribute('title')).toBe('Bout recorded');
  });


  // Review round 10, finding 3: a lineup saved since may make a pairing
  // possible. When the roster re-read that save causes fails, the answer read
  // before it is stale and must not keep Record bout disabled.
  it('a lineup save whose roster re-read fails re-enables Record bout', async () => {
    stubApi(BOTH_ON);
    await mount(kachinukiMatch([T1, T2, TIE]));
    expect(recordButton().disabled).toBe(true);
    // fetchKachinukiRoster answers a failed read with null.
    window.API.fetchKachinukiRoster = vi.fn().mockResolvedValue(null);
    const savedA = { positions: { '1': 'Kudo', '2': 'Mori', '3': 'Sato' }, memberIds: { '1': 'a1', '2': 'a2' }, matchId: 'm1' };
    window.API.fetchLineupInForce.mockImplementation((_c, teamId) => Promise.resolve(teamId === 'team-A' ? savedA : LINEUPS[teamId]));
    await act(async () => {
      window.dispatchEvent(new CustomEvent('lineup-updated', { detail: { competitionId: 'comp1', teamId: 'team-A' } }));
    });
    await settle();
    expect(window.API.fetchKachinukiRoster).toHaveBeenCalledTimes(1);
    expect(recordButton().disabled).toBe(false);
  });

  // Review round 8, finding 2: a point scored on the recorded tie changes the
  // bout's result, and the server's merge takes the mark off. The bout scored
  // to 1-0 stays live with both fighters on, nothing closes, and Record bout
  // is open again.
  it('a recorded tie scored to 1-0 does not close, and Record bout comes back', async () => {
    stubApi(BOTH_ON);
    const view = await mount(kachinukiMatch([T1, T2, TIE]));
    const { recorded: _r, ...scored } = { ...TIE, ipponsA: ['M', 'D'], winner: 'Mori', winnerMemberId: 'a2', decision: '' };
    await act(async () => {
      view.rerender(<ScoreEditorModal match={kachinukiMatch([T1, T2, scored])} onClose={vi.fn()} onSubmit={vi.fn()} password="secret" />);
    });
    await settle();
    expect(liveRows(view.container).length).toBe(1);
    expect(screen.queryByTestId('kachinuki-no-pairing')).toBeNull();
    const line = screen.getByTestId('kachinuki-advisory').textContent;
    expect(line).toContain('Ueda on');
    expect(line).toContain('Mori on');
    expect(recordButton().disabled).toBe(false);
  });
});

// Review rounds 5 and 6: a Record bout that appends no pairing only marks its
// bout recorded. The mark is a merge group of its own (GroupRecorded), stamped
// strictly after the match it was made on, so the copy carrying it is NEWER
// than every copy read before it, and each host's keepNewerMatches (patch.jsx)
// keeps it over a refetch that read the match before the mark and answers
// after it. No hold in the editor: the props here go through the host's own
// rule, as on every device.
describe('the recorded mark against a stale copy', () => {
  const { recorded: _r, ...UNRECORDED_2 } = BOUT_2;
  const stamped = (subResults, modifiedAt) => ({ ...kachinukiMatch(subResults), modifiedAt });

  // host keeps the detail a host holds and renders the editor from it, every
  // copy delivered through keepNewerMatches as a refetch is.
  function host(view, props) {
    let held = null;
    return async (match) => {
      held = keepNewerMatches(held, { poolMatches: [match] });
      await act(async () => { view.rerender(<ScoreEditorModal match={held.poolMatches[0]} {...props} />); });
      await settle();
    };
  }

  it('this device: a stale unmarked copy arriving after the marked one does not reopen the bout', async () => {
    stubApi(SHIRO_OUT);
    // The server's answer: the mark's stamp is strictly after the match's.
    const onSubmit = vi.fn().mockResolvedValue({ subResults: [BOUT_1, BOUT_2], modifiedAt: 101 });
    const props = { onClose: vi.fn(), onSubmit, password: 'secret' };
    let view;
    await act(async () => {
      view = render(<ScoreEditorModal match={stamped([BOUT_1, UNRECORDED_2], 100)} {...props} />);
    });
    await settle();
    const show = host(view, props);
    await show(stamped([BOUT_1, UNRECORDED_2], 100));
    expect(liveRows(view.container).length).toBe(1);
    await act(async () => { fireEvent.click(recordButton()); });
    await settle();
    expect(onSubmit).toHaveBeenCalledTimes(1);
    // The push that brings the mark, then the refetch read before it.
    await show(stamped([BOUT_1, BOUT_2], 101));
    expect(liveRows(view.container).length).toBe(0);
    await show(stamped([BOUT_1, UNRECORDED_2], 100));
    expect(liveRows(view.container).length).toBe(0);
    expect(screen.getByTestId('kachinuki-done-bout-1')).toBeTruthy();
    expect(screen.getByTestId('kachinuki-no-pairing')).toBeTruthy();
  });

  it('a second device: a list refetch read before the mark keeps the marked copy', async () => {
    stubApi(SHIRO_OUT);
    const props = { onClose: vi.fn(), onSubmit: vi.fn(), password: 'secret' };
    const unmarked = stamped([BOUT_1, UNRECORDED_2], 100);
    let view;
    await act(async () => { view = render(<ScoreEditorModal match={unmarked} {...props} />); });
    await settle();
    expect(liveRows(view.container).length).toBe(1);
    // The tournament's competitions, as the Scores tab and the console hold
    // them (keepNewerCompetitions): the push with the mark, then a list
    // refetch that read the match before it.
    let list = keepNewerCompetitions(null, [{ id: 'comp1', poolMatches: [unmarked] }]);
    const deliver = async (fetched) => {
      list = keepNewerCompetitions(list, fetched);
      await act(async () => { view.rerender(<ScoreEditorModal match={list[0].poolMatches[0]} {...props} />); });
      await settle();
    };
    await deliver([{ id: 'comp1', poolMatches: [stamped([BOUT_1, BOUT_2], 101)] }]);
    expect(liveRows(view.container).length).toBe(0);
    await deliver([{ id: 'comp1', poolMatches: [unmarked] }]);
    expect(liveRows(view.container).length).toBe(0);
    expect(screen.getByTestId('kachinuki-no-pairing')).toBeTruthy();
  });

  it('a strictly newer unmarked copy does reopen it', async () => {
    stubApi(SHIRO_OUT);
    const view = await mount(stamped([BOUT_1, BOUT_2], 101));
    expect(liveRows(view.container).length).toBe(0);
    const show = host(view, { onClose: vi.fn(), onSubmit: vi.fn(), password: 'secret' });
    await show(stamped([BOUT_1, BOUT_2], 101));
    await show(stamped([BOUT_1, UNRECORDED_2], 102));
    expect(liveRows(view.container).length).toBe(1);
    expect(screen.queryByTestId('kachinuki-no-pairing')).toBeNull();
  });
});
