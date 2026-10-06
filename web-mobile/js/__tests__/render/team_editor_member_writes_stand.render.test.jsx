// A name typed in a bout row names or adds a team member on the server, and the
// sheet shows that member from then on. A list of the team's members that was read
// before the write must not undo it, whichever way the list reaches the sheet:
//
//   - an admin host leaves the sheet to read the members itself, once as it opens and
//     again when the competition arrives, and the second read can answer after a name
//     was typed;
//   - the public self-run page hands the members in (teamMembers), and its copy can be
//     older than the write.
//
// Undone, a member the sheet had just named shows as unnamed again, so the same name
// typed for another bout finds nobody and names a second member instead of being
// refused by the duplicate guard, and a member the sheet had just added vanishes with
// its number. A list that shows the write is the server catching up: after it, a
// change made elsewhere shows.

import React from 'react';
import { render, act, fireEvent, screen } from '@testing-library/react';
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

const members = (p, names) => names.map((name, i) => ({ id: `${p}${i + 1}`, index: i + 1, name }));
const AKA = members('a', ['A One', 'A Two', 'A Three', 'A Four', 'A Five']);
// The first slot has no name yet: a name typed for the first bout names that member.
const SHIRO_FIRST_BLANK = members('b', ['', 'Kai Mori', 'Yui Sato', 'Rin Ota', 'Sho Ueda']);
const SHIRO_NAMED = members('b', ['Ren Abe', 'Kai Mori', 'Yui Sato', 'Rin Ota', 'Sho Ueda']);
const blank = (p) => members(p, ['', '', '', '', '']);
const teams = (shiro) => ({ 'team-A': AKA, 'team-B': shiro });

const DETAIL = { id: 'comp1', config: { format: 'knockout', teamMatchType: 'fixed', naginata: false, players: [] } };

function deferred() {
  let resolve;
  const promise = new Promise((res) => { resolve = res; });
  return { promise, resolve };
}

const flush = () => act(async () => { await new Promise((r) => setTimeout(r, 0)); });

// What the server holds, so a lineup the sheet reads is the one it saved.
let lineups;
// The answers of the members reads the sheet has asked for, in the order it asked.
let memberReads;
// Held until the test lets the competition arrive, which makes the sheet read the
// members again.
let competition;

beforeEach(() => {
  lineups = {};
  memberReads = [];
  competition = deferred();
  window.API = {
    fetchCompetitionDetails: vi.fn(() => competition.promise),
    fetchSquads: vi.fn(() => new Promise((resolve) => { memberReads.push(resolve); })),
    fetchLineupInForce: vi.fn(async (_c, teamId) => lineups[teamId] || null),
    putMatchLineup: vi.fn(async (_c, teamId, _m, positions, _pw, memberIds) => {
      lineups[teamId] = { positions, memberIds: memberIds || {} };
      return {};
    }),
    renameTeamMember: vi.fn(async () => true),
    addTeamMember: vi.fn(async (_c, _t, name) => ({ id: 'new-1', index: 6, name })),
    recordScore: vi.fn().mockResolvedValue(undefined),
    recordDaihyosen: vi.fn(),
    removeDaihyosen: vi.fn(),
    recordDecision: vi.fn(),
    hasPendingTerminalWrite: () => false,
    notePendingEdit: () => () => {},
  };
});

const bout = (root, n, color) => root.querySelectorAll('.team-sub-match')[n].querySelector(`.team-sub-match__side--${color} input`);
const label = (root, n, color) => {
  const el = root.querySelectorAll('.team-sub-match')[n].querySelector(`[data-testid="team-sub-match-member-label-${color}"]`);
  return el ? el.textContent : '';
};
const notices = (root) => root.querySelectorAll('[data-testid="team-editor-lineup-warning"]');
const offered = (root, n, color) => [...root.querySelectorAll('.team-sub-match')[n].querySelectorAll(`.team-sub-match__side--${color} .pmf__option`)]
  .map((o) => o.textContent);

async function typeName(input, name) {
  await act(async () => {
    fireEvent.focus(input);
    fireEvent.change(input, { target: { value: name } });
    fireEvent.keyDown(input, { key: 'Enter' });
  });
  await flush();
}

const teamMatch = (extra = {}) => ({
  id: 'm1',
  compId: 'comp1',
  status: 'running',
  phase: 'bracket',
  court: 'A',
  compKind: 'team',
  teamSize: 5,
  compFormat: 'knockout',
  teamMatchType: 'fixed',
  round: 'Semi-final',
  matchNumber: 1,
  sideA: { id: 'team-A', name: 'Team A', number: 'T1' },
  sideB: { id: 'team-B', name: 'Team B', number: 'T2' },
  ...extra,
});

// The sheet as an admin host opens it: the members read it makes as it opens is
// answered with `firstRead`, then the competition arrives and it reads them again,
// and that second read stays out until the test answers it.
async function mountWithSecondReadOut(firstRead, match = teamMatch()) {
  let utils;
  await act(async () => {
    utils = render(<ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={vi.fn().mockResolvedValue(undefined)} password="" />);
  });
  await act(async () => { memberReads[0](firstRead); });
  await flush();
  await act(async () => { competition.resolve(DETAIL); });
  await flush();
  expect(memberReads, 'the competition arriving reads the members again').toHaveLength(2);
  return utils;
}

describe('team editor: a members read that predates the sheet\'s own write does not undo it', () => {
  it('keeps the member the sheet named, so the same name typed for another bout is refused', async () => {
    const before = teams(SHIRO_FIRST_BLANK);
    const { container } = await mountWithSecondReadOut(before);

    await typeName(bout(container, 0, 'shiro'), 'Ito');
    expect(window.API.renameTeamMember).toHaveBeenCalledWith('comp1', 'team-B', 'b1', 'Ito', '');

    // The read begun before the rename answers with the list from before it.
    await act(async () => { memberReads[1](before); });
    await flush();
    await typeName(bout(container, 1, 'shiro'), 'Ito');

    expect(window.API.addTeamMember, 'no second member named Ito is added').not.toHaveBeenCalled();
    expect(window.API.renameTeamMember, 'no other member is named Ito').toHaveBeenCalledTimes(1);
    expect(window.API.putMatchLineup, 'the refused name saves no lineup').toHaveBeenCalledTimes(1);
    expect(notices(container)).toHaveLength(1);
    expect(notices(container)[0].textContent).toBe('Ito is already at Senpo.');
  });

  it('keeps the member the sheet added, with its number', async () => {
    const before = teams(SHIRO_NAMED);
    const { container } = await mountWithSecondReadOut(before);

    await typeName(bout(container, 0, 'shiro'), 'Newcomer');
    expect(window.API.addTeamMember).toHaveBeenCalledWith('comp1', 'team-B', 'Newcomer', '');
    expect(label(container, 0, 'shiro')).toBe('T2.6');

    await act(async () => { memberReads[1](before); });
    await flush();

    expect(label(container, 0, 'shiro'), 'the row still carries the added member').toBe('T2.6');
  });

  it('keeps a member added before the first list arrived, which lacks it', async () => {
    // The first read is still out, so the sheet knows no member yet and adds one.
    await act(async () => {
      render(<ScoreEditorModal match={teamMatch()} onClose={vi.fn()} onSubmit={vi.fn().mockResolvedValue(undefined)} password="" />);
    });
    await typeName(bout(document, 0, 'shiro'), 'Newcomer');
    expect(window.API.addTeamMember).toHaveBeenCalledTimes(1);

    await act(async () => { memberReads[0](teams(SHIRO_NAMED)); });
    await flush();

    expect(label(document, 0, 'shiro'), 'the row still carries the added member').toBe('T2.6');
    await act(async () => { fireEvent.focus(bout(document, 2, 'shiro')); });
    expect(offered(document, 2, 'shiro').some((o) => o.includes('Sho Ueda')), 'the list brings the rest of the team').toBe(true);
  });

  it('still shows a member another device added, beside the write', async () => {
    const before = teams(SHIRO_FIRST_BLANK);
    const { container } = await mountWithSecondReadOut(before);
    await typeName(bout(container, 0, 'shiro'), 'Ito');

    const another = { id: 'b6', index: 6, name: 'Mei Endo' };
    await act(async () => { memberReads[1](teams([...SHIRO_FIRST_BLANK, another])); });
    await flush();

    await act(async () => { fireEvent.focus(bout(container, 2, 'shiro')); });
    expect(offered(container, 2, 'shiro').some((o) => o.includes('Mei Endo')), 'the member another device added is offered').toBe(true);
    await typeName(bout(container, 1, 'shiro'), 'Ito');
    expect(window.API.addTeamMember).not.toHaveBeenCalled();
    expect(notices(container)[0].textContent).toBe('Ito is already at Senpo.');
  });

  // A kachinuki row past the first is a pairing, not a lineup position: a name typed
  // over a member picked from the list names that member by its own write.
  it('keeps the member a kachinuki row named', async () => {
    const played = [
      { position: 1, sideA: 'A One', sideB: 'Mei Ito', ipponsA: ['M', 'M'], ipponsB: [], winner: 'A One' },
      { position: 2, sideA: 'A One', sideB: '', ipponsA: [], ipponsB: [] },
    ];
    const before = { 'team-A': blank('a'), 'team-B': blank('b') };
    const { container } = await mountWithSecondReadOut(before, teamMatch({ phase: 'pool', compFormat: 'mixed', teamMatchType: 'kachinuki', subResults: played }));
    const shiroNow = () => [...container.querySelectorAll('.team-sub-match__side--shiro input')].pop();
    const shiroOptions = () => [...container.querySelectorAll('.team-sub-match__side--shiro .pmf__option')].map((o) => o.textContent);

    await act(async () => { fireEvent.focus(shiroNow()); });
    const slot = [...container.querySelectorAll('.team-sub-match__side--shiro .pmf__option')].find((o) => o.textContent.includes('T2.3'));
    expect(slot, 'the numbered member T2.3 is offered').toBeTruthy();
    await act(async () => { fireEvent.click(slot); });
    await typeName(shiroNow(), 'Ito');
    expect(window.API.renameTeamMember).toHaveBeenCalledWith('comp1', 'team-B', 'b3', 'Ito', '');

    await act(async () => { memberReads[1](before); });
    await flush();
    await act(async () => { fireEvent.focus(shiroNow()); });

    expect(shiroOptions().find((o) => o.includes('T2.3')), 'T2.3 keeps its name').toContain('Ito');
  });
});

describe('team editor: the host\'s copy of the members that predates the sheet\'s own write does not undo it (public page)', () => {
  const selfRun = (list) => ({ mode: 'self-run', competitions: [{ id: 'c1', squads: list }] });
  const publicMatch = () => teamMatch({
    compId: 'c1', compName: 'Teams', phase: 'pool', poolName: 'Pool 1', compFormat: 'mixed',
  });
  const aka = (n) => bout(document, n, 'aka');
  let view;

  const host = (list) => <MatchViewerModal match={publicMatch()} onClose={vi.fn()} tournament={selfRun(list)} compId="c1" />;
  const hostHolds = (list) => act(async () => { view.rerender(host(list)); });

  beforeEach(async () => {
    competition.resolve({ id: 'c1', config: { format: 'mixed', teamMatchType: 'fixed', naginata: false, players: [] } });
    await act(async () => { view = render(host({ 'team-A': blank('a'), 'team-B': blank('b') })); });
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Report result' })); });
    await flush();
  });

  it('keeps the member the sheet named until the copy shows the name, then follows a rename made elsewhere', async () => {
    await typeName(aka(0), 'Mei Ito');
    expect(window.API.renameTeamMember).toHaveBeenCalledWith('c1', 'team-A', 'a1', 'Mei Ito', '');
    expect(aka(0).value).toBe('Mei Ito');

    // A copy refreshed before the write reached the server: a1 has no name in it. The
    // other team's list changed too, which is what makes the sheet take the copy in.
    await hostHolds({ 'team-A': blank('a'), 'team-B': [...blank('b'), { id: 'b6', index: 6, name: '' }] });
    await typeName(aka(1), 'Mei Ito');

    expect(window.API.renameTeamMember, 'no other member is named Mei Ito').toHaveBeenCalledTimes(1);
    expect(window.API.addTeamMember).not.toHaveBeenCalled();
    expect(notices(document)).toHaveLength(1);
    expect(notices(document)[0].textContent).toBe('Mei Ito is already at Senpo.');

    // The copy that has the write: the server has caught up.
    await hostHolds({ 'team-A': [{ id: 'a1', index: 1, name: 'Mei Ito' }, ...blank('a').slice(1)], 'team-B': blank('b') });
    expect(aka(0).value).toBe('Mei Ito');

    // Another device renames the member: that shows now.
    await hostHolds({ 'team-A': [{ id: 'a1', index: 1, name: 'Mei Ito-Kato' }, ...blank('a').slice(1)], 'team-B': blank('b') });
    expect(aka(0).value).toBe('Mei Ito-Kato');
  });
});
