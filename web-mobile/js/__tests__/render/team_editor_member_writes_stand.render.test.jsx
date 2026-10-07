// A name typed in a bout row names or adds a team member on the server, and the
// sheet shows that member from then on. The server stamps every member write
// (`modifiedAt`) and answers the member with its stamp, and of two copies of a member
// the sheet keeps the one with the larger stamp, whichever way a list reaches it:
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
// its number. A list read before the write holds the member with an older stamp, so it
// undoes nothing; a list that holds a larger stamp was written after, by another device,
// and shows at once. The admin sheet reads its members again whenever a lineup change is
// announced for its competition, so such a change shows without reopening the sheet.

import React from 'react';
import { render, act, fireEvent, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { answered, namedLater } from '../helpers/team_members.js';
import { lineupPutStubByTeam } from '../helpers/lineup_server.js';
import { FETCH_TIMEOUT_MS } from '../../write_result.jsx';

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
// The answers of the members reads the sheet has asked for, in the order it asked, and
// what makes each of them fail.
let memberReads;
let memberFailures;
// Held until the test lets the competition arrive, which makes the sheet read the
// members again.
let competition;

beforeEach(() => {
  lineups = {};
  memberReads = [];
  memberFailures = [];
  competition = deferred();
  window.API = {
    fetchCompetitionDetails: vi.fn(() => competition.promise),
    fetchSquads: vi.fn(() => new Promise((resolve, reject) => { memberReads.push(resolve); memberFailures.push(reject); })),
    fetchLineupInForce: vi.fn(async (_c, teamId) => lineups[teamId] || null),
    // A save names the position it changed, and the server answers the lineup it holds then.
    putMatchLineup: lineupPutStubByTeam(() => lineups),
    // The server answers a member write with the member it holds, stamped. The ids here
    // are the letter of the team and the member's number.
    renameTeamMember: vi.fn(async (_c, _t, id, name) => answered({ id, index: Number(id.slice(1)) }, { name })),
    addTeamMember: vi.fn(async (_c, _t, name) => answered({ id: 'new-1', index: 6 }, { name })),
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

  // A name typed before the first list arrives waits for it, until the deadline of any
  // request (team_editor_members_read_first.render.test.jsx). Once it has given up, the
  // sheet knows no member and adds one, and the list answering late lacks it.
  it('keeps a member added before the first list arrived, which lacks it', async () => {
    vi.useFakeTimers();
    try {
      await act(async () => {
        render(<ScoreEditorModal match={teamMatch()} onClose={vi.fn()} onSubmit={vi.fn().mockResolvedValue(undefined)} password="" />);
      });
      const input = bout(document, 0, 'shiro');
      await act(async () => {
        fireEvent.focus(input);
        fireEvent.change(input, { target: { value: 'Newcomer' } });
        fireEvent.keyDown(input, { key: 'Enter' });
      });
      await act(async () => { await vi.advanceTimersByTimeAsync(FETCH_TIMEOUT_MS); });
      expect(window.API.addTeamMember).toHaveBeenCalledTimes(1);
    } finally {
      vi.useRealTimers();
    }

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

  // The member as the server answered the sheet's write, and the host's copy of team A
  // with that member in place of the unnamed a1.
  const written = () => window.API.renameTeamMember.mock.results[0].value;
  const copyWith = (member) => ({ 'team-A': [member, ...blank('a').slice(1)], 'team-B': blank('b') });

  it('keeps the member the sheet named over a copy that predates the write, and a copy that holds the write changes nothing', async () => {
    await typeName(aka(0), 'Mei Ito');
    expect(window.API.renameTeamMember).toHaveBeenCalledWith('c1', 'team-A', 'a1', 'Mei Ito', '');
    expect(aka(0).value).toBe('Mei Ito');

    // A copy refreshed before the write reached the server: a1 has no name in it, and no
    // stamp. The other team's list changed too, which is what makes the sheet take the
    // copy in.
    await hostHolds({ 'team-A': blank('a'), 'team-B': [...blank('b'), { id: 'b6', index: 6, name: '' }] });
    await typeName(aka(1), 'Mei Ito');

    expect(window.API.renameTeamMember, 'no other member is named Mei Ito').toHaveBeenCalledTimes(1);
    expect(window.API.addTeamMember).not.toHaveBeenCalled();
    expect(notices(document)).toHaveLength(1);
    expect(notices(document)[0].textContent).toBe('Mei Ito is already at Senpo.');

    // The copy that has the write: the server has caught up.
    await hostHolds(copyWith(await written()));
    expect(aka(0).value).toBe('Mei Ito');
  });

  it('shows a rename another device made after the sheet\'s own write, and follows the copies after it', async () => {
    await typeName(aka(0), 'Mei Ito');
    const mine = await written();
    await hostHolds(copyWith(mine));
    expect(aka(0).value).toBe('Mei Ito');

    // Another device renames the member: its stamp is the larger, so that shows now.
    const theirs = namedLater(mine, 'Mei Ito-Kato');
    await hostHolds(copyWith(theirs));
    expect(aka(0).value).toBe('Mei Ito-Kato');

    await hostHolds(copyWith(namedLater(theirs, 'Mei Ito-Kato Jr')));
    expect(aka(0).value).toBe('Mei Ito-Kato Jr');
  });

  it('keeps the name the sheet wrote over a copy that still holds the name the member had before the write', async () => {
    await typeName(aka(0), 'Mei Ito');
    expect(aka(0).value).toBe('Mei Ito');

    // A copy refreshed before the write reached the server: a1 is unnamed in it, as it was.
    await hostHolds({ 'team-A': blank('a'), 'team-B': [...blank('b'), { id: 'b6', index: 6, name: '' }] });

    expect(aka(0).value).toBe('Mei Ito');
  });

  // The stamp, not the name, says which copy is newer: a copy that holds a name the member
  // never had before the write but carries an OLDER stamp was read before it, so it
  // undoes nothing; the same name with a larger stamp was given after the write, and shows.
  it('tells another device\'s name from a stale copy by the stamp: an older stamp changes nothing, a larger one shows', async () => {
    await typeName(aka(0), 'Mei Ito');
    const mine = await written();
    expect(aka(0).value).toBe('Mei Ito');

    await hostHolds(copyWith({ ...mine, name: 'Mei Endo', modifiedAt: mine.modifiedAt - 1 }));
    expect(aka(0).value, 'a name with an older stamp was read before the write').toBe('Mei Ito');

    await hostHolds(copyWith(namedLater(mine, 'Mei Endo')));
    expect(aka(0).value, 'a name with a larger stamp was given after it').toBe('Mei Endo');
  });

  // The public page's copy follows lineup changes through the host's own refresh, so the
  // sheet reads nothing for them.
  it('is not read again on a lineup change: the host\'s copy is what it follows', async () => {
    await act(async () => { window.dispatchEvent(new CustomEvent('lineup-updated', { detail: { competitionId: 'c1' } })); });
    await flush();

    expect(window.API.fetchSquads).not.toHaveBeenCalled();
  });
});

// The admin sheet reads its own members, and reads them again whenever a lineup change is
// announced for its competition (the event the server sends for a rename, a cleared name
// and every lineup save), so a member renamed, cleared or added on another device shows
// without reopening the sheet, as the lineup editors' members do.
describe('team editor: the admin sheet follows the members another device changes', () => {
  const announce = (competitionId = 'comp1') => act(async () => {
    window.dispatchEvent(new CustomEvent('lineup-updated', { detail: { competitionId } }));
  });
  const shiro = (...names) => teams(members('b', names));
  const OTHERS = ['Kai Mori', 'Yui Sato', 'Rin Ota', 'Sho Ueda'];
  // Opens the sheet with its two reads (as it opens, and when the competition arrives) both answered.
  async function open(firstNames) {
    const utils = await mountWithSecondReadOut(shiro(...firstNames));
    await act(async () => { memberReads[1](shiro(...firstNames)); });
    await flush();
    return utils;
  }
  const options = async (container) => {
    await act(async () => { fireEvent.focus(bout(container, 2, 'shiro')); });
    return offered(container, 2, 'shiro');
  };

  it('reads the members again and shows a rename made elsewhere', async () => {
    const { container } = await open(['Ren Abe', ...OTHERS]);
    expect((await options(container)).some((o) => o.includes('Ren Abe'))).toBe(true);

    await announce();
    expect(memberReads, 'a read for the announcement').toHaveLength(3);
    await act(async () => { memberReads[2](shiro('Ren Abe-Kato', ...OTHERS)); });
    await flush();

    const after = await options(container);
    expect(after.some((o) => o.includes('Ren Abe-Kato')), 'the new name is offered').toBe(true);
    expect(after.some((o) => /Ren Abe(?!-)/.test(o)), 'and the old one no longer').toBe(false);
  });

  it('shows a member added or cleared elsewhere in the same way', async () => {
    const { container } = await open(['Ren Abe', ...OTHERS]);

    await announce();
    await act(async () => { memberReads[2](teams([...members('b', ['', ...OTHERS]), { id: 'b6', index: 6, name: 'Newcomer' }])); });
    await flush();

    const after = await options(container);
    expect(after.some((o) => o.includes('Newcomer')), 'the member another device added').toBe(true);
    expect(after.some((o) => o.includes('Ren Abe')), 'the name another device cleared').toBe(false);
  });

  // The list a read answers once the server holds `first` as the first member, as the
  // write's own answer stamped it.
  const shiroWith = (first, ...rest) => ({ 'team-A': AKA, 'team-B': [first, ...members('b', ['', ...rest]).slice(1)] });

  it('shows a rename made elsewhere of a member the sheet named, however the sheet\'s own write was read', async () => {
    const { container } = await open(['', ...OTHERS]);
    await typeName(bout(container, 0, 'shiro'), 'Ito');
    expect(window.API.renameTeamMember).toHaveBeenCalledWith('comp1', 'team-B', 'b1', 'Ito', '');
    expect(bout(container, 0, 'shiro').value).toBe('Ito');
    const mine = await window.API.renameTeamMember.mock.results[0].value;

    // The server announces the rename, and the read holds it, with the stamp of the write.
    await announce();
    await act(async () => { memberReads[2](shiroWith(mine, ...OTHERS)); });
    await flush();
    expect(bout(container, 0, 'shiro').value).toBe('Ito');

    // Another device renames the member: its stamp is the larger, so that shows.
    await announce();
    await act(async () => { memberReads[3](shiroWith(namedLater(mine, 'Itoh'), ...OTHERS)); });
    await flush();
    expect(bout(container, 0, 'shiro').value).toBe('Itoh');
  });

  it('shows another device\'s later rename of a member the sheet named even when no read has shown the sheet\'s own name first', async () => {
    const { container } = await open(['', ...OTHERS]);
    await typeName(bout(container, 0, 'shiro'), 'Ito');
    const mine = await window.API.renameTeamMember.mock.results[0].value;

    await announce();
    await act(async () => { memberReads[2](shiroWith(namedLater(mine, 'Itoh'), ...OTHERS)); });
    await flush();

    expect(bout(container, 0, 'shiro').value).toBe('Itoh');
  });

  it('keeps the member the sheet named over a read begun before the write that answers after it, and over one the write\'s own answer is older than', async () => {
    const { container } = await open(['', ...OTHERS]);
    await announce();
    await typeName(bout(container, 0, 'shiro'), 'Ito');
    const mine = await window.API.renameTeamMember.mock.results[0].value;
    // A read of what the server held before the write answers after it.
    await act(async () => { memberReads[2](shiro('', ...OTHERS)); });
    await flush();
    expect(bout(container, 0, 'shiro').value, 'the older list does not undo the write').toBe('Ito');

    // A list another device's later rename made, then the answer of this sheet's own write
    // arriving once more (a retry of the same write is the same stamp): the larger stays.
    await announce();
    await act(async () => { memberReads[3](shiroWith(namedLater(mine, 'Itoh'), ...OTHERS)); });
    await flush();
    expect(bout(container, 0, 'shiro').value).toBe('Itoh');
    await announce();
    await act(async () => { memberReads[4](shiroWith(mine, ...OTHERS)); });
    await flush();
    expect(bout(container, 0, 'shiro').value, 'a list holding only this sheet\'s own, older copy does not undo the later rename').toBe('Itoh');
  });

  // Only the read made as the sheet opens says the members could not be read: a later one
  // that fails changes nothing, and the list that was read stays.
  it('says nothing when a later read fails: the members that were read stay, and a name typed next carries no warning', async () => {
    const { container } = await open(['', ...OTHERS]);
    await announce();
    await act(async () => { memberFailures[2](new TypeError('Failed to fetch')); });
    await flush();

    await typeName(bout(container, 0, 'shiro'), 'Ito');

    expect(window.API.renameTeamMember, 'resolved against the list that was read').toHaveBeenCalledWith('comp1', 'team-B', 'b1', 'Ito', '');
    expect(notices(container), 'no warning that the members could not be read').toHaveLength(0);
  });

  it('keeps the name the sheet wrote over a read that predates the write, as before', async () => {
    const { container } = await open(['', ...OTHERS]);
    await announce();
    await typeName(bout(container, 0, 'shiro'), 'Ito');

    // The read begun by the announcement, before the write, answers with the member unnamed.
    await act(async () => { memberReads[2](shiro('', ...OTHERS)); });
    await flush();

    expect(bout(container, 0, 'shiro').value).toBe('Ito');
  });

  // Which copy of a member is newer is read off the stamp the server gave it, never off
  // which read began first or which answer arrived last.
  describe('keeps the newest copy of a member, whichever order the answers arrive in', () => {
    const older = answered({ id: 'b1', index: 1 }, { name: 'Ren Older' });
    const newer = namedLater(older, 'Ren Newer');
    const holding = (first) => shiroWith(first, ...OTHERS);

    it('an older copy arriving after a newer one is ignored', async () => {
      const { container } = await open(['Ren Abe', ...OTHERS]);
      await announce();
      await announce();
      expect(memberReads, 'one read for each announcement').toHaveLength(4);

      await act(async () => { memberReads[3](holding(newer)); });
      await flush();
      await act(async () => { memberReads[2](holding(older)); });
      await flush();

      const after = await options(container);
      expect(after.some((o) => o.includes('Ren Newer')), 'the newer copy stays').toBe(true);
      expect(after.some((o) => o.includes('Ren Older')), 'the older one does not replace it').toBe(false);
    });

    // The sheet's write is answered after a list that holds a later rename of the same member
    // has been shown: the answer carries the older stamp, so it does not undo that list.
    it('an own write\'s answer older than a list already shown does not undo the list', async () => {
      const { container } = await open(['', ...OTHERS]);
      const answer = deferred();
      const mine = answered({ id: 'b1', index: 1 }, { name: 'Ito' });
      window.API.renameTeamMember = vi.fn(() => answer.promise);
      await typeName(bout(container, 0, 'shiro'), 'Ito');
      expect(window.API.renameTeamMember, 'the write is out').toHaveBeenCalledTimes(1);

      // Another device renames the member after this write was stamped, and a read shows it.
      await announce();
      await act(async () => { memberReads[2](holding(namedLater(mine, 'Itoh'))); });
      await flush();
      await act(async () => { answer.resolve(mine); });
      await flush();

      expect(bout(container, 0, 'shiro').value).toBe('Itoh');
    });

    it('a newer copy shows even when the read that holds it began first', async () => {
      const { container } = await open(['Ren Abe', ...OTHERS]);
      await announce();
      await announce();

      await act(async () => { memberReads[3](holding(older)); });
      await flush();
      await act(async () => { memberReads[2](holding(newer)); });
      await flush();

      const after = await options(container);
      expect(after.some((o) => o.includes('Ren Newer')), 'the newer copy shows').toBe(true);
      expect(after.some((o) => o.includes('Ren Older')), 'and the older one does not stand beside it').toBe(false);
    });
  });

  it('still takes an older answer while no newer one has been, and ignores an announcement for another competition', async () => {
    const { container } = await open(['Ren Abe', ...OTHERS]);
    await announce('another-competition');
    expect(memberReads, 'nothing is read for another competition').toHaveLength(2);

    await announce();
    await announce();
    await act(async () => { memberReads[2](shiro('Ren Older', ...OTHERS)); });
    await flush();

    expect((await options(container)).some((o) => o.includes('Ren Older')), 'the only answer so far is taken').toBe(true);
  });

  // The announcement names the team it changed (teamId): the sheet reads the members
  // again only when that is one of its two teams, and still reads on one that names none.
  it('reads again for an announcement that names one of its teams, and not for another team\'s', async () => {
    const announceFor = (teamId, competitionId = 'comp1') => act(async () => {
      window.dispatchEvent(new CustomEvent('lineup-updated', { detail: { competitionId, teamId } }));
    });
    await open(['Ren Abe', ...OTHERS]);

    await announceFor('team-elsewhere');
    expect(memberReads, 'nothing is read for another team').toHaveLength(2);
    await announceFor('team-B');
    expect(memberReads, 'its Shiro team is read').toHaveLength(3);
    await announceFor('team-A');
    expect(memberReads, 'and its Aka team').toHaveLength(4);
    await announce();
    expect(memberReads, 'an announcement that names no team is read, as before').toHaveLength(5);
  });

  // The warning that the members could not be read speaks for the LATEST read: the opening
  // read failing after a later read already showed the members is stale news.
  it('says nothing when the opening read fails after a later read showed the members', async () => {
    let utils;
    await act(async () => {
      utils = render(<ScoreEditorModal match={teamMatch()} onClose={vi.fn()} onSubmit={vi.fn().mockResolvedValue(undefined)} password="" />);
    });
    expect(memberReads, 'the opening read is out').toHaveLength(1);
    await announce();
    expect(memberReads, 'and a later one begins').toHaveLength(2);
    await act(async () => { memberReads[1](shiro('', ...OTHERS)); });
    await flush();

    await act(async () => { memberFailures[0](new TypeError('Failed to fetch')); });
    await flush();
    await typeName(bout(utils.container, 0, 'shiro'), 'Ito');

    expect(window.API.renameTeamMember, 'resolved against the list the later read showed').toHaveBeenCalledWith('comp1', 'team-B', 'b1', 'Ito', '');
    expect(notices(utils.container), 'no warning that the members could not be read').toHaveLength(0);
  });

  it('still says so when the opening read fails and no later read showed the members', async () => {
    let utils;
    await act(async () => {
      utils = render(<ScoreEditorModal match={teamMatch()} onClose={vi.fn()} onSubmit={vi.fn().mockResolvedValue(undefined)} password="" />);
    });
    await act(async () => { memberFailures[0](new TypeError('Failed to fetch')); });
    await flush();

    await typeName(bout(utils.container, 0, 'shiro'), 'Ito');

    expect(notices(utils.container)).toHaveLength(1);
    expect(notices(utils.container)[0].textContent).toContain('team member list could not be loaded');
  });
});

// A kachinuki row past the first names its fighters on the bout itself, and it can be
// the removable one: its notice goes with the bout it was about, and a rename that is
// still out when another team is given the side does not reach the new team's list.
describe('team editor: a kachinuki row\'s name box', () => {
  const PLAYED = [
    { position: 1, sideA: 'A One', sideB: 'Mei Ito', ipponsA: ['M', 'M'], ipponsB: [], winner: 'A One' },
    { position: 2, sideA: 'A One', sideB: '', ipponsA: [], ipponsB: [] },
  ];
  const BEFORE = { 'team-A': blank('a'), 'team-B': blank('b') };
  const kachinuki = (sideB) => teamMatch({ phase: 'pool', compFormat: 'mixed', teamMatchType: 'kachinuki', subResults: PLAYED, ...(sideB ? { sideB } : {}) });
  const shiroNow = (container) => [...container.querySelectorAll('.team-sub-match__side--shiro input')].pop();
  const shiroOptions = (container) => [...container.querySelectorAll('.team-sub-match__side--shiro .pmf__option')].map((o) => o.textContent);

  // Picks the numbered member T2.3 from the current bout's list and types a name over it.
  async function nameTheThirdMember(container) {
    await act(async () => { fireEvent.focus(shiroNow(container)); });
    const slot = [...container.querySelectorAll('.team-sub-match__side--shiro .pmf__option')].find((o) => o.textContent.includes('T2.3'));
    await act(async () => { fireEvent.click(slot); });
    await typeName(shiroNow(container), 'Ito');
  }

  it('shows no warning about the old pairing on a bout added at the place of a removed one', async () => {
    window.API.renameTeamMember = vi.fn().mockRejectedValue(new Error('offline'));
    window.API.removeKachinukiBout = vi.fn().mockResolvedValue({ id: 'm1', subResults: [PLAYED[0]] });
    const { container } = await mountWithSecondReadOut(BEFORE, kachinuki());

    await nameTheThirdMember(container);
    expect(notices(container), 'the rename that failed is reported in the bout row').toHaveLength(1);

    await act(async () => { fireEvent.click(screen.getByTestId('kachinuki-remove-bout-button')); });
    await flush();
    expect(window.API.removeKachinukiBout).toHaveBeenCalledTimes(1);
    // The next pairing is added by hand, at the same place.
    await act(async () => { fireEvent.click(screen.getByTestId('kachinuki-add-bout-button')); });
    await flush();

    expect(container.querySelectorAll('.team-sub-match').length, 'the new bout is a row of its own').toBeGreaterThan(1);
    expect(notices(container), 'nothing is said about the pairing that was removed').toHaveLength(0);
  });

  it('does not put the old team\'s renamed member in the list of the team the side was given while the rename was out', async () => {
    const rename = deferred();
    const { container, rerender } = await mountWithSecondReadOut(BEFORE, kachinuki());
    window.API.renameTeamMember = vi.fn(() => rename.promise);

    await nameTheThirdMember(container);
    expect(window.API.renameTeamMember).toHaveBeenCalledWith('comp1', 'team-B', 'b3', 'Ito', '');

    // Another team is given the Shiro side, and its members are read.
    await act(async () => {
      rerender(<ScoreEditorModal match={kachinuki({ id: 'team-C', name: 'Team C', number: 'T3' })} onClose={vi.fn()} onSubmit={vi.fn().mockResolvedValue(undefined)} password="" />);
    });
    await flush();
    await act(async () => { memberReads[memberReads.length - 1]({ 'team-A': blank('a'), 'team-C': blank('c') }); });
    await flush();

    // The rename the old team's member was waiting on lands, answered as the server does.
    await act(async () => { rename.resolve(answered({ id: 'b3', index: 3 }, { name: 'Ito' })); });
    await flush();

    await act(async () => { fireEvent.focus(shiroNow(container)); });
    expect(shiroOptions(container).some((o) => o.includes('Ito')), 'the old team\'s member is not offered to the new team').toBe(false);
    expect(notices(container)[0].textContent).toBe('Nothing was saved because another team is now on this side. Type the name again.');
  });
});
