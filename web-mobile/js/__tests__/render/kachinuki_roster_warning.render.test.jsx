// bc-kno1 (operator decisions Q5 and L1): when the server cannot read
// team-members.yaml or lineups.yaml, the kachinuki roster read
// (GET .../kachinuki-roster) answers 200 with a `warning`, and the score sheet
// shows it as a warning toast ONCE per match per sentence. The toast's own id
// dedupes only while it is shown or queued, and the roster is read again after
// every Record bout, so the sheet keeps its own memory of what it has shown.
// While a warning is present the server works out no next fighter, so its
// `nextPairing: false` never disables Record bout, and "+ Add next bout
// manually" is reachable. Pinned on the admin sheet and on the public self-run
// sheet, with seeded nameless members (a bout reading "T2.1" against "T1.1").
import React from 'react';
import { render, act, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { makeFakeOutbox } from '../helpers/fake_outbox.js';
import { toastSnapshot, dismissToast, resetToastQueue } from '../../toast_queue.jsx';

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
let MatchViewerModal;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  await import('../../admin_lineup.jsx');
  await import('../../admin_scoring_modal.jsx');
  ScoreEditorModal = window.ScoreEditorModal;
  ({ MatchViewerModal } = await import('../../viewer_match.jsx'));
});

afterAll(() => restoreGlobals());

// The two sentences the server answers (engine, kachinuki_fielded.go).
const MEMBERS_WARNING = "Team members could not be read, so fighters are not seated by member number. Add each bout's pairing by hand.";
const LINEUPS_WARNING = "The lineups could not be read, so who fights next is not worked out. Add each bout's pairing by hand.";

// Seeded nameless members, as a fresh team holds them.
const blank = (p) => [1, 2, 3].map((i) => ({ id: `${p}${i}`, index: i, name: '' }));
const SQUADS = { 'team-A': blank('a'), 'team-B': blank('b') };

// The roster answer under a warning: both sides present, nobody listed,
// nobody exhausted, and the advance's own `nextPairing` (nothing is appended).
const warned = (warning, on = { a: 'a1', b: 'b1' }) => ({
  warning,
  nextPairing: false,
  sideA: { lineupFound: false, on: { name: '', memberId: on.a }, remaining: [], exhausted: false },
  sideB: { lineupFound: false, on: { name: '', memberId: on.b }, remaining: [], exhausted: false },
});

function stubApi({ roster, lineupsFail = false } = {}) {
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
    hasPendingTerminalWrite: () => false,
    fetchSquads: vi.fn().mockResolvedValue(SQUADS),
    fetchLineupInForce: lineupsFail
      ? vi.fn().mockRejectedValue(new Error('lineups.yaml could not be read'))
      : vi.fn().mockResolvedValue(null),
    fetchKachinukiRoster: vi.fn().mockResolvedValue(roster === undefined ? null : roster),
    ...makeFakeOutbox().api,
  };
}

beforeEach(() => {
  resetToastQueue();
  stubApi();
});

// Bout 1, Shiro's nameless member 1 against Aka's: scored for Shiro.
const BOUT_1 = {
  position: 1, sideA: '', sideAMemberId: 'a1', sideB: '', sideBMemberId: 'b1', ipponsA: [], ipponsB: ['M'],
};
// The same bout after Record bout: a nameless winner is stored as the team's
// name plus the member id (bc-dnst), and the server marks it recorded.
const BOUT_1_RECORDED = { ...BOUT_1, winner: 'Team B', winnerMemberId: 'b1', decision: 'fought', recorded: true };

function kachinukiMatch(subResults, compId = 'comp1') {
  return {
    id: 'm1', compId, compName: 'Teams', status: 'running', phase: 'pool', poolName: 'Pool 1', court: 'A',
    compKind: 'team', teamSize: 3, compFormat: 'mixed', teamMatchType: 'kachinuki',
    sideA: { id: 'team-A', name: 'Team A', number: 'T1' }, // Aka
    sideB: { id: 'team-B', name: 'Team B', number: 'T2' }, // Shiro
    subResults,
  };
}

async function settle() {
  for (let i = 0; i < 6; i++) await act(async () => { await Promise.resolve(); });
  await act(async () => { await new Promise((r) => setTimeout(r, 0)); });
}

// The two hosts of the team sheet: the admin dispatcher, and the public page's
// "Report result" on a self-run tournament (empty password).
const HOSTS = {
  admin: {
    mount: async (match) => {
      let view;
      await act(async () => {
        view = render(<ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={vi.fn()} password="secret" />);
      });
      await settle();
      return { rerender: async (next) => {
        await act(async () => { view.rerender(<ScoreEditorModal match={next} onClose={vi.fn()} onSubmit={vi.fn()} password="secret" />); });
        await settle();
      } };
    },
  },
  public: {
    mount: async (match) => {
      const tournament = { mode: 'self-run', competitions: [{ id: match.compId, squads: SQUADS }] };
      let view;
      await act(async () => {
        view = render(<MatchViewerModal match={match} onClose={vi.fn()} tournament={tournament} compId={match.compId} />);
      });
      await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Report result' })); });
      await settle();
      return { rerender: async (next) => {
        await act(async () => { view.rerender(<MatchViewerModal match={next} onClose={vi.fn()} tournament={tournament} compId={next.compId} />); });
        await settle();
      } };
    },
  },
};

const recordButton = () => screen.getAllByRole('button').find((b) => b.textContent === 'Record bout');
const shown = () => toastSnapshot();

describe.each(Object.keys(HOSTS))('the kachinuki roster warning on the %s sheet', (host) => {
  const { mount } = HOSTS[host];

  it('shows a warning toast once per match per sentence, even after it was dismissed', async () => {
    stubApi({ roster: warned(MEMBERS_WARNING) });
    const view = await mount(kachinukiMatch([BOUT_1]));
    expect(window.API.fetchKachinukiRoster).toHaveBeenCalledTimes(1);
    expect(shown()).toMatchObject({ message: MEMBERS_WARNING, type: 'warning', latched: true });

    // The operator dismisses it; Record bout lands and the roster is read again,
    // with the same warning: nothing is shown a second time.
    await act(async () => { dismissToast(); });
    expect(shown()).toBeNull();
    await view.rerender(kachinukiMatch([BOUT_1_RECORDED]));
    expect(window.API.fetchKachinukiRoster).toHaveBeenCalledTimes(2);
    expect(shown()).toBeNull();

    // The other sentence is its own warning, shown once too.
    window.API.fetchKachinukiRoster.mockResolvedValue(warned(LINEUPS_WARNING, { a: 'a2', b: 'b1' }));
    await view.rerender(kachinukiMatch([BOUT_1_RECORDED, { position: 2, sideA: '', sideAMemberId: 'a2', sideB: '', sideBMemberId: 'b1', ipponsA: [], ipponsB: [] }]));
    expect(window.API.fetchKachinukiRoster).toHaveBeenCalledTimes(3);
    expect(shown()).toMatchObject({ message: LINEUPS_WARNING, type: 'warning' });
  });

  it('shows nothing for an answer with no warning', async () => {
    stubApi({ roster: { nextPairing: true, sideA: { lineupFound: true, on: { name: '', memberId: 'a1' }, remaining: [{ name: '', memberId: 'a2' }], exhausted: false }, sideB: { lineupFound: true, on: { name: '', memberId: 'b1' }, remaining: [{ name: '', memberId: 'b2' }], exhausted: false } } });
    await mount(kachinukiMatch([BOUT_1]));
    expect(window.API.fetchKachinukiRoster).toHaveBeenCalledTimes(1);
    expect(shown()).toBeNull();
  });

  it('a second match shows the same sentence again', async () => {
    stubApi({ roster: warned(MEMBERS_WARNING) });
    const view = await mount(kachinukiMatch([BOUT_1]));
    await act(async () => { dismissToast(); });
    await view.rerender({ ...kachinukiMatch([BOUT_1]), id: 'm2' });
    expect(shown()).toMatchObject({ message: MEMBERS_WARNING, type: 'warning' });
  });

  it('is read, and warns, when the lineup reads fail', async () => {
    stubApi({ roster: warned(LINEUPS_WARNING), lineupsFail: true });
    await mount(kachinukiMatch([BOUT_1]));
    expect(window.API.fetchLineupInForce).toHaveBeenCalled();
    expect(window.API.fetchKachinukiRoster).toHaveBeenCalledTimes(1);
    expect(shown()).toMatchObject({ message: LINEUPS_WARNING, type: 'warning' });
  });

  it('leaves Record bout enabled on a recorded live bout, and the manual next bout reachable', async () => {
    stubApi({ roster: warned(LINEUPS_WARNING) });
    await mount(kachinukiMatch([BOUT_1_RECORDED]));
    // The recorded bout stays live (no side is exhausted under a warning).
    expect(screen.queryByTestId('kachinuki-no-pairing')).toBeNull();
    expect(recordButton().disabled).toBe(false);
    const add = screen.getByTestId('kachinuki-add-bout-button');
    expect(add.disabled).toBe(false);
    // The operator adds the next pairing by hand: a new live row, both boxes open.
    await act(async () => { fireEvent.click(add); });
    await settle();
    const live = [...document.querySelectorAll('.team-sub-match:not(.team-sub-match--readonly)')];
    expect(live.length).toBe(1);
    expect(live[0].querySelectorAll('input.pmf__input').length).toBe(2);
  });

  it('without a warning, the same nextPairing: false still disables Record bout', async () => {    const answer = warned(LINEUPS_WARNING);
    delete answer.warning;
    stubApi({ roster: answer });
    await mount(kachinukiMatch([BOUT_1_RECORDED]));
    expect(recordButton().disabled).toBe(true);
    expect(shown()).toBeNull();
  });
});

// The roster read is keyed on a VALUE that folds in both teams' members as the
// sheet holds them (id, number, name): with no lineup the server fields members
// by member number, so a member named since the last read changes the answer,
// and a list re-created with the same members reads nothing.
describe('the kachinuki roster read key follows the team members', () => {
  it('reads again when a member is named, and not when the same members come again', async () => {
    stubApi({ roster: null });
    const match = kachinukiMatch([BOUT_1]);
    const tournamentWith = (squads) => ({ mode: 'self-run', competitions: [{ id: 'comp1', squads }] });
    let view;
    await act(async () => {
      view = render(<MatchViewerModal match={match} onClose={vi.fn()} tournament={tournamentWith(SQUADS)} compId="comp1" />);
    });
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Report result' })); });
    await settle();
    expect(window.API.fetchKachinukiRoster).toHaveBeenCalledTimes(1);

    // The same members in new objects: no read.
    await act(async () => { view.rerender(<MatchViewerModal match={match} onClose={vi.fn()} tournament={tournamentWith({ 'team-A': blank('a'), 'team-B': blank('b') })} compId="comp1" />); });
    await settle();
    expect(window.API.fetchKachinukiRoster).toHaveBeenCalledTimes(1);

    // Aka's member 2 is named, with a later stamp: one read.
    const named = { 'team-A': [blank('a')[0], { id: 'a2', index: 2, name: 'Ito', modifiedAt: Date.now() + 1000 }, blank('a')[2]], 'team-B': blank('b') };
    await act(async () => { view.rerender(<MatchViewerModal match={match} onClose={vi.fn()} tournament={tournamentWith(named)} compId="comp1" />); });
    await settle();
    expect(window.API.fetchKachinukiRoster).toHaveBeenCalledTimes(2);
  });
});
