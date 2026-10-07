// The at-court lineup panel as its two hosts drive it (the Scores page's Lineup
// button and the court console's Enter lineup).
//
//  - It shows the match as the page holds it NOW. A knockout match's side can be given
//    another team after the panel opened (its feeding match corrected or decided on
//    another device): the panel's editors must then be the new team's, or the operator
//    keeps editing the old team and a Save stores a lineup for a team that is no longer
//    in the match. Driven through each host's own data, as production does it: the page
//    is given new data and its matches change underneath the open panel. No host hands
//    the panel a new match prop by hand.
//  - Its two editors ask for the team members once between them: one request answers
//    for every team.

import React from 'react';
import { render, act, fireEvent, within } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { answered } from '../helpers/team_members.js';

const A = { id: 'team-a', name: 'Team A' };
const B = { id: 'team-b', name: 'Team B' };
const C = { id: 'team-c', name: 'Team C' };
const COMP = { id: 'comp-1', name: 'Team Event', kind: 'team', format: 'mixed', status: 'active', teamSize: 3, players: [A, B, C] };
const knockout = (sideB) => ({
  id: 'm-r1-0', compId: 'comp-1', compName: 'Team Event', compKind: 'team', teamSize: 3, status: 'scheduled',
  phase: 'bracket', round: 'Final', court: 'A', scheduledAt: '09:00', sideA: A, sideB,
});

// What the hosts' match lists answer: the page's matches, which a test changes.
let matches;

const STUBS = {
  ScoreEditorModal: () => null,
  AdminTopbar: ({ children }) => <div>{children}</div>,
  Breadcrumbs: () => null,
  CourtPicker: () => null,
  BracketTree: () => null,
  Icon: ({ name }) => <span>{name}</span>,
  getScoreBtnClass: () => 'test-score-open',
  matchScoreStr: () => '',
  filterMatchesByCourt: (all) => all,
  tournamentMatches: () => matches,
  compMatches: () => matches,
  startPatch: () => ({ status: 'running', winner: null }),
  confirmDialog: vi.fn().mockResolvedValue(true),
  pluralize: (n, s, p) => `${n} ${n === 1 ? s : (p || `${s}s`)}`,
  API: {
    fetchCompetitionDetails: vi.fn().mockResolvedValue(null),
    fetchLineupInForce: vi.fn().mockResolvedValue(null),
    fetchSquads: vi.fn().mockResolvedValue({}),
    queuedLineupSave: vi.fn().mockReturnValue(null),
    putMatchLineup: vi.fn(),
    addTeamMember: vi.fn(),
    // The server answers a member write with the member it holds, stamped.
    renameTeamMember: vi.fn((_comp, _team, id, name) => Promise.resolve(answered({ id, index: 1 }, { name }))),
  },
};

let restore;
let restorePanel;
let AdminScoreEditor;
let AdminShiaijoPage;

beforeAll(async () => {
  window.scrollTo = vi.fn();
  restore = installWindowStubs(STUBS);
  await import('../../admin_lineup.jsx');
  const { MatchLineupPanel } = await import('../../admin_schedule_lineup.jsx');
  // admin_schedule.jsx publishes it; the console reads it off window.
  restorePanel = installWindowStubs({ MatchLineupPanel });
  ({ AdminScoreEditor } = await import('../../admin_schedule_score_editor.jsx'));
  await import('../../admin_shiaijo.jsx');
  AdminShiaijoPage = window.AdminShiaijoPage;
});
afterAll(() => { restorePanel(); restore(); });

beforeEach(() => {
  // A draft an earlier test left behind would be restored into the editor it opens.
  sessionStorage.clear();
  window.API.fetchLineupInForce.mockClear();
  window.API.fetchSquads.mockClear();
});

const flush = async () => {
  for (let i = 0; i < 6; i += 1) {
    await act(async () => { await Promise.resolve(); });
  }
};

// The sides editors that were asked for their team's lineup, in the order asked.
const teamsRead = () => window.API.fetchLineupInForce.mock.calls.map((call) => call[1]).sort();

// A new tournament object each time, as the admin app hands the page after a refresh.
const scoresPage = () => (
  <AdminScoreEditor t={{ competitions: [COMP] }} onEditScore={vi.fn()} onMoveCourt={null} password="pw" showToast={vi.fn()} />
);
const courtConsole = () => (
  <AdminShiaijoPage
    tournament={{ name: 'Test Tournament', courts: ['A', 'B'], competitions: [COMP] }}
    court="A" onBack={vi.fn()} onEditScore={vi.fn()} onMoveCourt={vi.fn()} onLogout={vi.fn()}
    onViewerMode={vi.fn()} password="pw" showToast={vi.fn()} tweaks={{}} onSwitchCourt={vi.fn()}
  />
);

describe.each([
  ['the Scores page', scoresPage, 'Lineup'],
  ['the court console', courtConsole, 'Enter lineup'],
])('%s lineup panel when a side of its match is given another team', (_host, page, openLabel) => {
  const open = async () => {
    matches = [knockout(B)];
    let utils;
    await act(async () => { utils = render(page()); });
    await act(async () => { fireEvent.click(utils.getByRole('button', { name: openLabel })); });
    await flush();
    return utils;
  };

  it('shows an editor for the new team, and none for the old one', async () => {
    const utils = await open();
    expect(utils.getByTestId('match-lineup-side-team-b'), 'team B is on the Shiro side').toBeTruthy();
    expect(utils.getByTestId('match-lineup-side-team-a')).toBeTruthy();

    // Another device corrects the feeding match: team C takes team B's place.
    matches = [knockout(C)];
    await act(async () => { utils.rerender(page()); });
    await flush();

    expect(utils.queryByTestId('match-lineup-side-team-b'), 'the old team is gone').toBeNull();
    expect(utils.getByTestId('match-lineup-side-team-c'), 'the new team is on the side').toBeTruthy();
    expect(utils.getByTestId('match-lineup-side-team-a'), 'the other side is not touched').toBeTruthy();
    expect(teamsRead(), 'the new team\'s lineup is read').toEqual(['team-a', 'team-b', 'team-c']);
  });

  it('closes when the match leaves the page\'s data', async () => {
    const utils = await open();
    expect(utils.getByTestId('match-lineup-side-team-b')).toBeTruthy();

    matches = [];
    await act(async () => { utils.rerender(page()); });

    expect(utils.queryByTestId('match-lineup-side-team-b')).toBeNull();
    expect(utils.queryByText('Lineup for this match')).toBeNull();
  });
});

// A write the old team's editor still has out lands nowhere once its side is given
// another team: the new team's editor is a new one.
describe('the Scores page lineup panel when the old team still has a save out', () => {
  const NAMES = { positions: { 1: 'Aoki', 2: 'Sato', 3: 'Ito' }, memberIds: { 1: 'mem-1', 2: 'mem-2', 3: 'mem-3' } };
  const LINEUPS = {
    'team-a': NAMES,
    'team-b': { positions: { 1: 'Baba', 2: 'Bando', 3: 'Bessho' }, memberIds: { 1: 'b-1', 2: 'b-2', 3: 'b-3' } },
    'team-c': { positions: { 1: 'Chiba', 2: 'Cho', 3: 'Date' }, memberIds: { 1: 'c-1', 2: 'c-2', 3: 'c-3' } },
  };
  const MEMBERS = {
    'team-a': [{ id: 'mem-1', index: 1, name: 'Aoki' }, { id: 'mem-2', index: 2, name: 'Sato' }, { id: 'mem-3', index: 3, name: 'Ito' }, { id: 'mem-4', index: 4, name: 'Mori' }],
    'team-b': [{ id: 'b-1', index: 1, name: 'Baba' }, { id: 'b-2', index: 2, name: 'Bando' }, { id: 'b-3', index: 3, name: 'Bessho' }, { id: 'b-4', index: 4, name: 'Bito' }],
    'team-c': [{ id: 'c-1', index: 1, name: 'Chiba' }, { id: 'c-2', index: 2, name: 'Cho' }, { id: 'c-3', index: 3, name: 'Date' }, { id: 'c-4', index: 4, name: 'Endo' }],
  };

  beforeEach(() => {
    window.API.fetchSquads.mockResolvedValue(MEMBERS);
    window.API.fetchLineupInForce.mockImplementation((_comp, teamId) => Promise.resolve({
      teamId, competitionId: 'comp-1', ...LINEUPS[teamId], matchId: 'm-r1-0', sourceMatchId: 'm-r1-0', saved: true,
    }));
    window.API.putMatchLineup.mockReset();
    window.API.addTeamMember.mockReset().mockImplementation((_comp, _team, name) => Promise.resolve(answered({ id: 'mem-minted', index: 6 }, { name })));
  });

  const deferred = () => {
    let resolve;
    const promise = new Promise((res) => { resolve = res; });
    return { promise, resolve };
  };
  const side = (utils, teamId) => within(utils.getByTestId(`match-lineup-side-${teamId}`));

  async function open() {
    matches = [knockout(B)];
    let utils;
    await act(async () => { utils = render(scoresPage()); });
    await act(async () => { fireEvent.click(utils.getByRole('button', { name: 'Lineup' })); });
    await flush();
    return utils;
  }

  // The operator types a name nobody has at team B's position 1 and saves.
  async function saveNewNameForTeamB(utils) {
    await act(async () => {
      const input = side(utils, 'team-b').getByLabelText('1 player');
      fireEvent.focus(input);
      fireEvent.change(input, { target: { value: 'Kobayashi' } });
      fireEvent.keyDown(input, { key: 'Enter' });
    });
    await act(async () => { fireEvent.click(side(utils, 'team-b').getByRole('button', { name: /^Save lineup$/ })); });
    await flush();
  }

  async function giveTeamCTheSide(utils) {
    matches = [knockout(C)];
    await act(async () => { utils.rerender(scoresPage()); });
    await flush();
  }

  // The new team's editor shows its own lineup and offers its own members, none of team B's.
  async function expectTeamCAlone(utils) {
    const c = side(utils, 'team-c');
    expect(c.getByLabelText('1 player').value).toBe('Chiba');
    expect(c.getByLabelText('2 player').value).toBe('Cho');
    expect(c.getByLabelText('3 player').value).toBe('Date');
    // The open list shows the query, not the value: look, then close it again.
    await act(async () => { fireEvent.focus(c.getByLabelText('1 player')); });
    expect(c.getByText('Endo'), 'its own unplaced member').toBeTruthy();
    expect(c.queryByText('Bito'), 'team B\'s unplaced member').toBeNull();
    expect(c.queryByText('Kobayashi'), 'the member team B\'s Save minted').toBeNull();
    await act(async () => { fireEvent.keyDown(c.getByLabelText('1 player'), { key: 'Escape' }); });
  }

  it('lands nothing of a write the old team still has out in the new team\'s lineup or members', async () => {
    const utils = await open();
    const write = deferred();
    window.API.putMatchLineup.mockReturnValue(write.promise);
    await saveNewNameForTeamB(utils);
    expect(window.API.putMatchLineup, 'team B\'s write is out').toHaveBeenCalledTimes(1);

    await giveTeamCTheSide(utils);
    await expectTeamCAlone(utils);
    await act(async () => {
      write.resolve({ positions: { 1: 'Kobayashi', 2: 'Bando', 3: 'Bessho' }, memberIds: { 1: 'mem-minted', 2: 'b-2', 3: 'b-3' } });
    });
    await flush();

    await expectTeamCAlone(utils);
  });

  it('lands nothing either of the member the old team\'s Save was still minting', async () => {
    const utils = await open();
    const mint = deferred();
    window.API.addTeamMember.mockReturnValue(mint.promise);
    await saveNewNameForTeamB(utils);
    expect(window.API.addTeamMember, 'the member is being minted for team B').toHaveBeenCalledTimes(1);

    await giveTeamCTheSide(utils);
    await expectTeamCAlone(utils);
    await act(async () => { mint.resolve(answered({ id: 'mem-minted', index: 6 }, { name: 'Kobayashi' })); });
    await flush();

    await expectTeamCAlone(utils);
  });
});

// The API answers every team's members in one request, and the panel mounts two
// editors, each of which needs its own team's: they share the request.
describe('the lineup panel asks for the team members once', () => {
  const announce = (detail) => act(async () => {
    window.dispatchEvent(new CustomEvent('lineup-updated', { detail }));
  });

  async function open() {
    matches = [knockout(B)];
    let utils;
    await act(async () => { utils = render(scoresPage()); });
    await act(async () => { fireEvent.click(utils.getByRole('button', { name: 'Lineup' })); });
    await flush();
    return utils;
  }

  it('when it opens', async () => {
    const utils = await open();
    expect(utils.getByTestId('match-lineup-side-team-a')).toBeTruthy();
    expect(utils.getByTestId('match-lineup-side-team-b')).toBeTruthy();
    expect(window.API.fetchSquads).toHaveBeenCalledTimes(1);
  });

  it('when a lineup change is announced that names no team', async () => {
    await open();
    await announce({ competitionId: 'comp-1' });
    await flush();

    expect(window.API.fetchLineupInForce, 'both sides read their lineup again').toHaveBeenCalledTimes(4);
    expect(window.API.fetchSquads, 'for one more request of the members').toHaveBeenCalledTimes(2);
  });

  it('when one is announced for one of its teams: only that side reads again', async () => {
    await open();
    window.API.fetchLineupInForce.mockClear();
    await announce({ competitionId: 'comp-1', teamId: 'team-b' });
    await flush();

    expect(window.API.fetchLineupInForce.mock.calls.map((call) => call[1])).toEqual(['team-b']);
    expect(window.API.fetchSquads).toHaveBeenCalledTimes(2);
  });
});
