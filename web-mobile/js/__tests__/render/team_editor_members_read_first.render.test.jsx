// The team score sheet keeps its own lists of each side's team members, and a name
// typed in a bout row is resolved against the list of that side's team. Three rules, in
// a real DOM:
//
//  1. A name typed before the team's members were read waits for them (bounded by the
//     deadline of any request). Resolved against none, a new name is minted where the
//     member's seeded slot is free instead of naming that slot, and the name of a
//     member the team has is refused by the server as a second member of that name,
//     so the lineup is saved with the name and no member id. The wait ends when the
//     read fails, and the sheet then goes on without them, as it always did, and says
//     so. The deadline ends it for good: while a read hangs, only the first name
//     waits, and the next goes on at once with the same warning.
//
//  2. A side given another team (the feeding match of a knockout corrected on another
//     device seats another team on it) holds nothing of the old team's members: they
//     are not merged into the new team's list, where they would be offered and
//     refused by the server (400 team_member_not_in_team), and nothing the sheet wrote
//     for them is applied to the new team's. Both ways the members reach the sheet:
//     handed in by the host (the public self-run page) and read by the sheet itself.
//
//  3. The same side shows the new team's lineup, and a pick made for the old team while
//     it was out writes nothing, and says so at the row: the organiser is not judged
//     by the server for putting one team's member in another team's lineup. One whose
//     member write was out keeps that write (it is the old team's own member) but saves
//     no lineup and records the member nowhere on the side. The old team's lineup goes
//     with its members until the new team's is read, and nothing still out for the old
//     team lands on the side, whenever it answers.
//
//  4. A side is given another team only when it really is one: its own key in the match
//     and the team it resolves to both change. The team's id arriving for a side that
//     carried only its name, or the match starting to carry the id of the team it named,
//     is not.
//
//  5. A side whose team is not known yet (it carries only a name, and the roster has not
//     named a participant for it) takes no members list: its wait stays open, and a pick
//     that waited writes for the team as it is known when the wait is over.

import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { readCode } from '../helpers/source.js';
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

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  await import('../../admin_lineup.jsx');
  await import('../../admin_scoring_modal.jsx');
  ScoreEditorModal = window.ScoreEditorModal;
});

afterAll(() => restoreGlobals());

const members = (p, names) => names.map((name, i) => ({ id: `${p}${i + 1}`, index: i + 1, name }));
const AKA = members('a', ['A One', 'A Two', 'A Three', 'A Four', 'A Five']);
const SHIRO_NAMED = members('b', ['Ren Abe', 'Kai Mori', 'Yui Sato', 'Rin Ota', 'Sho Ueda']);
// The first slot has no name yet: a name typed for the first bout names that member.
const SHIRO_FIRST_BLANK = members('b', ['', 'Kai Mori', 'Yui Sato', 'Rin Ota', 'Sho Ueda']);
const teams = (shiro) => ({ 'team-A': AKA, 'team-B': shiro });

function deferred() {
  let resolve;
  const promise = new Promise((res) => { resolve = res; });
  return { promise, resolve };
}

const flush = () => act(async () => { await new Promise((r) => setTimeout(r, 0)); });

// What the server holds, so a lineup the sheet reads is the one it saved.
let lineups;
// The members reads the sheet has asked for, in the order it asked: each answered by the test.
let memberReads;
// Held until a test lets the competition arrive, which makes the sheet read the members again.
let competition;

beforeEach(() => {
  lineups = {};
  memberReads = [];
  competition = deferred();
  window.API = {
    fetchCompetitionDetails: vi.fn(() => competition.promise),
    fetchSquads: vi.fn(() => new Promise((resolve, reject) => { memberReads.push({ resolve, reject }); })),
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
const notices = (root) => [...root.querySelectorAll('[data-testid="team-editor-lineup-warning"]')].map((n) => n.textContent);
const offered = (root, n, color) => [...root.querySelectorAll('.team-sub-match')[n].querySelectorAll(`.team-sub-match__side--${color} .pmf__option`)]
  .map((o) => o.textContent);
// The names a bout row's list offers for a side once it is opened.
async function offers(root, n, color) {
  await act(async () => { fireEvent.focus(bout(root, n, color)); });
  return offered(root, n, color);
}

// Types a name into a bout row and takes it, without waiting for what it starts.
const type = (input, name) => act(async () => {
  fireEvent.focus(input);
  fireEvent.change(input, { target: { value: name } });
  fireEvent.keyDown(input, { key: 'Enter' });
});
async function typeName(input, name) {
  await type(input, name);
  await flush();
}

const side = (id, number) => ({ id, name: `Team ${id.slice(5)}`, number });
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
  sideA: side('team-A', 'T1'),
  sideB: side('team-B', 'T2'),
  ...extra,
});

async function open(match = teamMatch(), extra = {}) {
  let view;
  await act(async () => {
    view = render(<ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={vi.fn().mockResolvedValue(undefined)} password="" {...extra} />);
  });
  return view;
}

describe('team editor: a name typed before the team\'s members were read waits for them', () => {
  it('places the member the team has by its id, instead of adding the name again', async () => {
    const { container } = await open();

    await typeName(bout(container, 0, 'shiro'), 'Kai Mori');
    expect(window.API.addTeamMember, 'nothing is added while the members are out').not.toHaveBeenCalled();
    expect(window.API.putMatchLineup, 'nor written').not.toHaveBeenCalled();

    await act(async () => { memberReads[0].resolve(teams(SHIRO_NAMED)); });
    await flush();

    expect(window.API.addTeamMember, 'Kai Mori is the member the team has, not a new one').not.toHaveBeenCalled();
    expect(window.API.renameTeamMember).not.toHaveBeenCalled();
    const [, teamId, , positions, , memberIds] = window.API.putMatchLineup.mock.calls[0];
    expect(teamId).toBe('team-B');
    expect(positions).toEqual({ senpo: 'Kai Mori' });
    expect(memberIds).toEqual({ senpo: 'b2' });
    expect(notices(container)).toEqual([]);
  });

  it('names the unnamed member seeded for the position, instead of adding one', async () => {
    const { container } = await open();

    await typeName(bout(container, 0, 'shiro'), 'Ito');
    expect(window.API.addTeamMember).not.toHaveBeenCalled();
    expect(window.API.renameTeamMember).not.toHaveBeenCalled();

    await act(async () => { memberReads[0].resolve(teams(SHIRO_FIRST_BLANK)); });
    await flush();

    expect(window.API.renameTeamMember, 'the slot the row was shown with is named').toHaveBeenCalledWith('comp1', 'team-B', 'b1', 'Ito', '');
    expect(window.API.addTeamMember, 'no member is added beside it').not.toHaveBeenCalled();
    expect(window.API.putMatchLineup.mock.calls[0][5]).toEqual({ senpo: 'b1' });
    expect(label(container, 0, 'shiro')).toBe('T2.1');
  });

  it('goes on without the members when they could not be read, as it always did, and says so', async () => {
    const { container } = await open();
    await act(async () => { memberReads[0].reject(new TypeError('Failed to fetch')); });
    await flush();

    await typeName(bout(container, 0, 'shiro'), 'Newcomer');

    expect(window.API.addTeamMember, 'the name is added against no list, at once').toHaveBeenCalledWith('comp1', 'team-B', 'Newcomer', '');
    expect(notices(container)).toHaveLength(1);
    expect(notices(container)[0]).toContain('team member list could not be loaded');
  });

  it('goes on, and says so, when the read fails while the name waits for it', async () => {
    const { container } = await open();
    await typeName(bout(container, 0, 'shiro'), 'Newcomer');
    expect(window.API.addTeamMember).not.toHaveBeenCalled();

    await act(async () => { memberReads[0].reject(new TypeError('Failed to fetch')); });
    await flush();

    expect(window.API.addTeamMember).toHaveBeenCalledWith('comp1', 'team-B', 'Newcomer', '');
    expect(notices(container)).toHaveLength(1);
    expect(notices(container)[0]).toContain('team member list could not be loaded');
  });

  it('does not wait once a list has been shown, whatever read is out', async () => {
    const { container } = await open();
    await act(async () => { memberReads[0].resolve(teams(SHIRO_NAMED)); });
    await flush();
    // The competition arriving makes the sheet read the members again, and that read is never answered.
    await act(async () => { competition.resolve({ id: 'comp1', config: { format: 'knockout', teamMatchType: 'fixed', naginata: false, players: [] } }); });
    await flush();
    expect(memberReads, 'the second read is out').toHaveLength(2);

    await typeName(bout(container, 0, 'shiro'), 'Newcomer');

    expect(window.API.addTeamMember, 'the name is added against the list shown, at once').toHaveBeenCalledWith('comp1', 'team-B', 'Newcomer', '');
  });

  // The member added then is one the list that arrives late lacks, which the sheet keeps
  // (team_editor_member_writes_stand.render.test.jsx).
  it('goes on at the deadline, and says so', async () => {
    vi.useFakeTimers();
    let container;
    try {
      ({ container } = await open());
      await type(bout(container, 0, 'shiro'), 'Newcomer');
      await act(async () => { await vi.advanceTimersByTimeAsync(FETCH_TIMEOUT_MS - 1); });
      expect(window.API.addTeamMember, 'nothing is added while the read may still answer').not.toHaveBeenCalled();

      await act(async () => { await vi.advanceTimersByTimeAsync(1); });
      await act(async () => { await vi.advanceTimersByTimeAsync(0); });

      expect(window.API.addTeamMember).toHaveBeenCalledWith('comp1', 'team-B', 'Newcomer', '');
    } finally {
      vi.useRealTimers();
    }
    expect(notices(container)).toHaveLength(1);
    expect(notices(container)[0]).toContain('team member list could not be loaded');
  });

  // The deadline ends the wait for good (lineup_form.test.jsx): while the read hangs, only
  // the first name waits for it. The next goes on at once, with the same warning, and a
  // list that arrives late is still shown.
  it('waits once for a read that hangs: the next name goes on at once, with the warning, and the late list is still shown', async () => {
    let added = 0;
    window.API.addTeamMember.mockImplementation(async (_c, _t, name) => ({ id: `new-${++added}`, index: 5 + added, name }));
    vi.useFakeTimers();
    let container;
    try {
      ({ container } = await open());
      await type(bout(container, 0, 'shiro'), 'Newcomer');
      await act(async () => { await vi.advanceTimersByTimeAsync(FETCH_TIMEOUT_MS); });
      expect(window.API.addTeamMember).toHaveBeenCalledTimes(1);

      await type(bout(container, 1, 'shiro'), 'Another');
      await act(async () => { await vi.advanceTimersByTimeAsync(0); });

      expect(window.API.addTeamMember, 'the second name does not wait for a deadline of its own').toHaveBeenCalledTimes(2);
      const second = container.querySelectorAll('.team-sub-match')[1].querySelector('[data-testid="team-editor-lineup-warning"]');
      expect(second, 'and says the members could not be read, as the first did').not.toBeNull();
      expect(second.textContent).toContain('team member list could not be loaded');
    } finally {
      vi.useRealTimers();
    }

    await act(async () => { memberReads[0].resolve(teams(SHIRO_NAMED)); });
    await flush();

    expect((await offers(container, 3, 'shiro')).some((o) => o.includes('Sho Ueda')), 'the list that arrives late is shown').toBe(true);
  });
});

describe('team editor: a side given another team holds nothing of the old team\'s members', () => {
  const X = members('x', ['Xavier One', 'Xavier Two', 'Xavier Three', 'Xavier Four', 'Xavier Five']);
  const Y = members('y', ['Yuri One', 'Yuri Two', 'Yuri Three', 'Yuri Four', 'Yuri Five']);
  const everyTeam = { 'team-X': X, 'team-Y': Y, 'team-B': SHIRO_NAMED };
  const holds = (list, who) => list.some((o) => o.includes(who));
  const withSideA = (id, number) => teamMatch({ sideA: side(id, number) });

  describe('handed in by the host (the public self-run page)', () => {
    const host = (id, number, lists = everyTeam) => (
      <ScoreEditorModal match={withSideA(id, number)} onClose={vi.fn()} onSubmit={vi.fn()} password="" teamMembers={lists} />
    );

    it('offers only the new team\'s members, not the old team\'s beside them', async () => {
      let view;
      await act(async () => { view = render(host('team-X', 'T3')); });
      await flush();
      const before = await offers(document, 2, 'aka');
      expect(holds(before, 'Xavier One'), 'the old team\'s members are offered while it plays the side').toBe(true);

      await act(async () => { view.rerender(host('team-Y', 'T4')); });
      await flush();
      const after = await offers(document, 2, 'aka');

      expect(holds(after, 'Yuri One'), 'the new team\'s members are offered').toBe(true);
      expect(after.filter((o) => o.includes('Xavier')), 'and none of the old team\'s').toEqual([]);
      expect(after, 'five members, not ten').toHaveLength(5);
      expect(holds(await offers(document, 2, 'shiro'), 'Ren Abe'), 'the other side is left as it was').toBe(true);
    });

    it('applies no name the sheet wrote for the old team\'s member to the new team\'s', async () => {
      // Two teams whose members carry the same ids, which only a test can arrange: what
      // the sheet wrote is kept by member id until a list shows it.
      const oldTeam = members('m', ['', 'Xavier Two', 'Xavier Three', 'Xavier Four', 'Xavier Five']);
      const newTeam = members('m', ['Yuri One', 'Yuri Two', 'Yuri Three', 'Yuri Four', 'Yuri Five']);
      const lists = { 'team-X': oldTeam, 'team-Y': newTeam, 'team-B': SHIRO_NAMED };
      let view;
      await act(async () => { view = render(host('team-X', 'T3', lists)); });
      await flush();
      await typeName(bout(document, 0, 'aka'), 'Ito');
      expect(window.API.renameTeamMember, 'the first slot of the old team is named').toHaveBeenCalledWith('comp1', 'team-X', 'm1', 'Ito', '');

      await act(async () => { view.rerender(host('team-Y', 'T4', lists)); });
      await flush();
      // The first row's list: the lineup the sheet wrote still names the member that shares
      // the id at that position, so it is that row's own member and is offered there.
      const first = (await offers(document, 0, 'aka')).find((o) => o.includes('T4.1'));

      expect(first, 'the new team\'s first member is offered').toBeTruthy();
      expect(first, 'under its own name, not the one written for the old team\'s').toContain('Yuri One');
      expect(first).not.toContain('Ito');
    });
  });

  describe('read by the sheet itself (an admin host)', () => {
    const host = (id, number) => (
      <ScoreEditorModal match={withSideA(id, number)} onClose={vi.fn()} onSubmit={vi.fn()} password="pw" />
    );

    it('drops the old team\'s members at once, reads the new team\'s, and offers only those', async () => {
      let view;
      await act(async () => { view = render(host('team-X', 'T3')); });
      await act(async () => { memberReads[0].resolve(everyTeam); });
      await flush();
      expect(holds(await offers(document, 2, 'aka'), 'Xavier One')).toBe(true);

      await act(async () => { view.rerender(host('team-Y', 'T4')); });
      await flush();

      expect(memberReads, 'the new team\'s members are read').toHaveLength(2);
      expect(await offers(document, 2, 'aka'), 'nothing of the old team is offered while they are out').toEqual([]);

      await act(async () => { memberReads[1].resolve(everyTeam); });
      await flush();
      const after = await offers(document, 2, 'aka');

      expect(holds(after, 'Yuri One')).toBe(true);
      expect(after.filter((o) => o.includes('Xavier'))).toEqual([]);
      expect(after).toHaveLength(5);
    });

    it('waits for the new team\'s members before a name typed for it is resolved', async () => {
      let view;
      await act(async () => { view = render(host('team-X', 'T3')); });
      await act(async () => { memberReads[0].resolve(everyTeam); });
      await flush();
      await act(async () => { view.rerender(host('team-Y', 'T4')); });
      await flush();

      await typeName(bout(document, 0, 'aka'), 'Yuri Two');
      expect(window.API.addTeamMember, 'the new team\'s members were not read yet').not.toHaveBeenCalled();

      await act(async () => { memberReads[1].resolve(everyTeam); });
      await flush();

      expect(window.API.addTeamMember, 'Yuri Two is the member the new team has').not.toHaveBeenCalled();
      expect(window.API.putMatchLineup.mock.calls[0][1]).toBe('team-Y');
      expect(window.API.putMatchLineup.mock.calls[0][5]).toEqual({ senpo: 'y2' });
    });

    // A name is picked for the team that plays the side when it is typed. When another
    // team takes the side while the pick is still out (the feeding match corrected on
    // another device), nothing of it is written: the member it would name or add is the old
    // team's, and the lineup it would save is one the new team does not carry. The organiser
    // is not judged by the server for either, so the sheet stops it, and says why at the row.
    const OTHER_TEAM = 'Nothing was saved because another team is now on this side. Type the name again.';
    const noWrite = () => {
      expect(window.API.addTeamMember, 'no member is added').not.toHaveBeenCalled();
      expect(window.API.renameTeamMember, 'none is named').not.toHaveBeenCalled();
      expect(window.API.putMatchLineup, 'and no lineup is written').not.toHaveBeenCalled();
    };

    it('stops a pick that waits for the members when the side is given another team', async () => {
      let view;
      await act(async () => { view = render(host('team-X', 'T3')); });
      await type(bout(document, 0, 'aka'), 'Newcomer');
      expect(window.API.addTeamMember, 'the members are out, so the pick waits').not.toHaveBeenCalled();

      await act(async () => { view.rerender(host('team-Y', 'T4')); });
      await flush();
      await act(async () => { memberReads.forEach((read) => read.resolve(everyTeam)); });
      await flush();

      noWrite();
      expect(window.API.fetchLineupInForce, 'the old team\'s lineup is not read back into the side').not.toHaveBeenCalledWith('comp1', 'team-X', 'm1');
      expect(notices(document)).toEqual([OTHER_TEAM]);
    });

    it('stops a pick that waits for the lineup when the side is given another team', async () => {
      let view;
      await act(async () => { view = render(host('team-X', 'T3')); });
      await act(async () => { memberReads[0].resolve(everyTeam); });
      await flush();
      const lineupRead = deferred();
      window.API.fetchLineupInForce = vi.fn(() => lineupRead.promise);
      await typeName(bout(document, 0, 'aka'), 'Newcomer');
      expect(window.API.fetchLineupInForce, 'the lineup is being read for the pick').toHaveBeenCalledTimes(1);

      await act(async () => { view.rerender(host('team-Y', 'T4')); });
      await flush();
      await act(async () => { lineupRead.resolve(null); });
      await flush();

      noWrite();
      expect(notices(document)).toEqual([OTHER_TEAM]);
    });

    it('lets a pick through when the side keeps its team', async () => {
      await act(async () => { render(host('team-X', 'T3')); });
      await act(async () => { memberReads[0].resolve(everyTeam); });
      await flush();

      await typeName(bout(document, 0, 'aka'), 'Newcomer');

      expect(window.API.addTeamMember).toHaveBeenCalledWith('comp1', 'team-X', 'Newcomer', 'pw');
      expect(notices(document)).toEqual([]);
    });

    // The sheet reads each side's lineup for the team that plays it, so a side given
    // another team shows that team's lineup and not the old team's beside its members.
    it('reads the lineup of the team a side is given, and shows it instead of the old team\'s', async () => {
      lineups = {
        'team-X': { positions: { senpo: 'Xavier One' }, memberIds: { senpo: 'x1' } },
        'team-Y': { positions: { senpo: 'Yuri One' }, memberIds: { senpo: 'y1' } },
      };
      let view;
      await act(async () => { view = render(host('team-X', 'T3')); });
      await act(async () => { competition.resolve({ id: 'comp1', config: { format: 'knockout', teamMatchType: 'fixed', naginata: false, players: [] } }); });
      await flush();
      expect(bout(document, 0, 'aka').value, 'the lineup of the team on the side').toBe('Xavier One');

      await act(async () => { view.rerender(host('team-Y', 'T4')); });
      await flush();

      expect(window.API.fetchLineupInForce).toHaveBeenCalledWith('comp1', 'team-Y', 'm1');
      expect(bout(document, 0, 'aka').value, 'the new team\'s lineup, not the old team\'s').toBe('Yuri One');
    });

    // A pick whose member write is out when the side is given another team: the write was
    // typed for the old team and stays (it is the team's own member), but the lineup is
    // not saved, and the member is not recorded on the side, whose list is the new team's.
    it('stops a pick whose member write is still out when the side is given another team', async () => {
      let view;
      await act(async () => { view = render(host('team-X', 'T3')); });
      await act(async () => { memberReads[0].resolve(everyTeam); });
      await flush();
      const mint = deferred();
      window.API.addTeamMember = vi.fn(() => mint.promise);
      await typeName(bout(document, 0, 'aka'), 'Newcomer');
      expect(window.API.addTeamMember, 'the member is being added for the old team').toHaveBeenCalledWith('comp1', 'team-X', 'Newcomer', 'pw');

      await act(async () => { view.rerender(host('team-Y', 'T4')); });
      await flush();
      await act(async () => { mint.resolve({ id: 'new-1', index: 6, name: 'Newcomer' }); });
      await flush();
      await act(async () => { memberReads[1].resolve(everyTeam); });
      await flush();

      expect(window.API.addTeamMember, 'the member that was added stays').toHaveBeenCalledTimes(1);
      expect(window.API.putMatchLineup, 'but no lineup is written').not.toHaveBeenCalled();
      expect(notices(document)).toEqual([OTHER_TEAM]);
      const offeredNow = await offers(document, 2, 'aka');
      expect(holds(offeredNow, 'Yuri One'), 'the side lists the new team\'s members').toBe(true);
      expect(holds(offeredNow, 'Newcomer'), 'and not the member added for the old team').toBe(false);
    });

    // The lineup each side shows is dropped when it is given another team, before the new
    // team's is read: a read that fails leaves the side with no lineup, and a read still out
    // for the old team never lands on it, whenever it answers.
    const ROSTER = { id: 'comp1', config: { format: 'knockout', teamMatchType: 'fixed', naginata: false, players: [] } };
    const lineupOf = (name, id) => ({ positions: { senpo: name }, memberIds: { senpo: id } });

    it('leaves a side with no lineup when the new team\'s lineup cannot be read, never the old team\'s', async () => {
      lineups = { 'team-X': lineupOf('Xavier One', 'x1') };
      window.API.fetchLineupInForce = vi.fn(async (_c, teamId) => {
        if (teamId === 'team-Y') throw new TypeError('Failed to fetch');
        return lineups[teamId] || null;
      });
      let view;
      await act(async () => { view = render(host('team-X', 'T3')); });
      await act(async () => { competition.resolve(ROSTER); });
      await flush();
      expect(bout(document, 0, 'aka').value).toBe('Xavier One');

      await act(async () => { view.rerender(host('team-Y', 'T4')); });
      await flush();

      expect(window.API.fetchLineupInForce, 'the new team\'s lineup was asked for').toHaveBeenCalledWith('comp1', 'team-Y', 'm1');
      expect(bout(document, 0, 'aka').value, 'and the side shows none, not the old team\'s').toBe('');
    });

    it('refuses a pick on a side whose new team\'s lineup was not read, rather than composing it on the old team\'s', async () => {
      lineups = { 'team-X': lineupOf('Xavier One', 'x1') };
      window.API.fetchLineupInForce = vi.fn(async (_c, teamId) => {
        if (teamId === 'team-Y') throw new TypeError('Failed to fetch');
        return lineups[teamId] || null;
      });
      let view;
      await act(async () => { view = render(host('team-X', 'T3')); });
      await act(async () => { competition.resolve(ROSTER); });
      await flush();
      await act(async () => { view.rerender(host('team-Y', 'T4')); });
      await flush();
      await act(async () => { memberReads[memberReads.length - 1].resolve(everyTeam); });
      await flush();

      await typeName(bout(document, 0, 'aka'), 'Yuri Two');

      expect(window.API.putMatchLineup, 'nothing is written for the new team').not.toHaveBeenCalled();
      expect(notices(document)).toHaveLength(1);
      expect(notices(document)[0]).toContain('lineup could not be read');
    });

    it('never shows the old team\'s lineup when its read answers after the side was given another team', async () => {
      const lateRead = deferred();
      window.API.fetchLineupInForce = vi.fn(async (_c, teamId) => {
        if (teamId === 'team-X') return lateRead.promise;
        if (teamId === 'team-Y') throw new TypeError('Failed to fetch');
        return null;
      });
      let view;
      await act(async () => { view = render(host('team-X', 'T3')); });
      await act(async () => { competition.resolve(ROSTER); });
      await flush();
      await act(async () => { view.rerender(host('team-Y', 'T4')); });
      await flush();

      await act(async () => { lateRead.resolve(lineupOf('Xavier One', 'x1')); });
      await flush();

      expect(bout(document, 0, 'aka').value, 'a late answer for the old team is not shown').toBe('');
    });

    it('never shows a lineup the old team had written when it lands after the side was given another team', async () => {
      lineups = { 'team-X': lineupOf('Xavier One', 'x1') };
      let view;
      await act(async () => { view = render(host('team-X', 'T3')); });
      await act(async () => { memberReads[0].resolve(everyTeam); });
      await act(async () => { competition.resolve(ROSTER); });
      await flush();
      const put = deferred();
      window.API.putMatchLineup = vi.fn(() => put.promise);
      window.API.fetchLineupInForce = vi.fn(async (_c, teamId) => (teamId === 'team-Y' ? null : lineups[teamId] || null));
      await typeName(bout(document, 1, 'aka'), 'Newcomer');
      expect(window.API.putMatchLineup, 'the lineup is being written for the old team').toHaveBeenCalledTimes(1);

      await act(async () => { view.rerender(host('team-Y', 'T4')); });
      await flush();
      await act(async () => { put.resolve({}); });
      await flush();

      expect(bout(document, 1, 'aka').value, 'the side shows what the new team has, not what was written for the old team').toBe('');
    });
  });
});

// A side that carries only its name has no team the sheet can name until the competition's
// roster resolves it to a participant: its key in the match is the name, and so is its
// team, until then. Two rules, one for each half of that.
describe('team editor: a side that carries only its name', () => {
  const NAMED = members('n', ['Nao One', 'Nao Two', 'Nao Three', 'Nao Four', 'Nao Five']);
  const OTHERS = members('o', ['Omi One', 'Omi Two', 'Omi Three', 'Omi Four', 'Omi Five']);
  // What the members route answers: lists keyed by participant id, never by name.
  const lists = { 'team-N': NAMED, 'team-O': OTHERS, 'team-B': SHIRO_NAMED };
  const roster = (...players) => ({ id: 'comp1', config: { format: 'knockout', teamMatchType: 'fixed', naginata: false, players } });
  const ROSTER = roster({ id: 'team-N', name: 'Team N' }, { id: 'team-O', name: 'Team O' });
  const onlyName = (name) => ({ name, number: 'T5' });
  const host = (sideA) => (
    <ScoreEditorModal match={teamMatch({ sideA })} onClose={vi.fn()} onSubmit={vi.fn()} password="pw" />
  );
  const holds = (list, who) => list.some((o) => o.includes(who));
  const lineupOf = (name, id) => ({ positions: { senpo: name }, memberIds: { senpo: id } });
  const OTHER_TEAM = 'Nothing was saved because another team is now on this side. Type the name again.';

  // A side is given another team only when it really is one. The team's id arriving for the
  // side is not, and nor is the match starting to carry the id of the team it named: nothing
  // is dropped, and a pick made meanwhile is not stopped.
  describe('is given another team only when its key in the match and its team both change', () => {
    it('is not one when the team\'s id arrives for a side that carried only its name: the lineup read for the team stays', async () => {
      lineups = { 'team-N': lineupOf('Nao One', 'n1') };
      await act(async () => { render(host(onlyName('Team N'))); });

      await act(async () => { competition.resolve(ROSTER); });
      await flush();

      expect(bout(document, 0, 'aka').value, 'the team\'s lineup, read once the roster named it, is not dropped').toBe('Nao One');
    });

    it('is not one when the match starts to carry the id of the team it named', async () => {
      let view;
      await act(async () => { view = render(host(onlyName('Team N'))); });
      await act(async () => { competition.resolve(ROSTER); });
      await act(async () => { memberReads.forEach((read) => read.resolve(lists)); });
      await flush();
      const lineupRead = deferred();
      window.API.fetchLineupInForce = vi.fn(() => lineupRead.promise);
      await typeName(bout(document, 0, 'aka'), 'Newcomer');

      await act(async () => { view.rerender(host({ id: 'team-N', name: 'Team N', number: 'T5' })); });
      await flush();
      expect(holds(await offers(document, 2, 'aka'), 'Nao One'), 'the list is kept').toBe(true);
      await act(async () => { lineupRead.resolve(null); });
      await flush();

      expect(window.API.putMatchLineup, 'the pick is written').toHaveBeenCalledTimes(1);
      expect(notices(document)).toEqual([]);
    });

    it('is one when a side that carries only its name is given another team by name', async () => {
      let view;
      await act(async () => { view = render(host(onlyName('Team N'))); });
      await act(async () => { competition.resolve(ROSTER); });
      await act(async () => { memberReads.forEach((read) => read.resolve(lists)); });
      await flush();
      expect(holds(await offers(document, 2, 'aka'), 'Nao One')).toBe(true);
      const lineupRead = deferred();
      window.API.fetchLineupInForce = vi.fn(() => lineupRead.promise);
      await typeName(bout(document, 0, 'aka'), 'Newcomer');

      await act(async () => { view.rerender(host(onlyName('Team O'))); });
      await flush();
      await act(async () => { lineupRead.resolve(null); });
      await act(async () => { memberReads.forEach((read) => read.resolve(lists)); });
      await flush();

      expect(window.API.putMatchLineup, 'nothing is written for the old team').not.toHaveBeenCalled();
      expect(notices(document)).toEqual([OTHER_TEAM]);
      const after = await offers(document, 2, 'aka');
      expect(holds(after, 'Omi One'), 'the side lists the new team\'s members').toBe(true);
      expect(holds(after, 'Nao'), 'and none of the old team\'s').toBe(false);
    });
  });

  // The members route answers by participant id, so for the name it has nothing: the sheet
  // would show an empty list, which a name typed meanwhile is resolved against, and its member
  // add is refused by the server (the team id is checked against the participants) while the
  // name is saved without a member. A side takes its list only once its team is known: it
  // carries an id, or the roster named a participant for it. Until then its wait stays open
  // (bounded as ever), and a pick that waited writes for the team as it is known when the wait
  // is over, not as it was when the name was typed.
  describe('takes no members list until its team is known, and a pick writes for the team as it is known then', () => {
    it('takes none, so a name typed meanwhile waits, and is written under the participant id with the member found by id', async () => {
      await act(async () => { render(host(onlyName('Team N'))); });
      await act(async () => { memberReads[0].resolve(lists); });
      await flush();
      expect(await offers(document, 2, 'aka'), 'no list is taken for a side whose team is not known').toEqual([]);
      expect(holds(await offers(document, 2, 'shiro'), 'Ren Abe'), 'the other side carries its id and takes its list').toBe(true);

      await typeName(bout(document, 0, 'aka'), 'Nao Two');
      expect(window.API.addTeamMember, 'the pick waits for the roster to name the team').not.toHaveBeenCalled();
      expect(window.API.putMatchLineup).not.toHaveBeenCalled();

      await act(async () => { competition.resolve(ROSTER); });
      await flush();
      await act(async () => { memberReads.slice(1).forEach((read) => read.resolve(lists)); });
      await flush();

      expect(window.API.addTeamMember, 'Nao Two is the team\'s member, found in the list that was read').not.toHaveBeenCalled();
      expect(window.API.renameTeamMember).not.toHaveBeenCalled();
      const [, teamId, , positions, , memberIds] = window.API.putMatchLineup.mock.calls[0];
      expect(teamId, 'written under the participant id, not the name').toBe('team-N');
      expect(positions).toEqual({ senpo: 'Nao Two' });
      expect(memberIds).toEqual({ senpo: 'n2' });
      expect(notices(document)).toEqual([]);
    });

    it('takes no copy of the host\'s either, and takes it once the roster names the team', async () => {
      const copy = { 'team-N': NAMED, 'team-B': SHIRO_NAMED };
      await act(async () => {
        render(<ScoreEditorModal match={teamMatch({ sideA: onlyName('Team N') })} onClose={vi.fn()} onSubmit={vi.fn()} password="" teamMembers={copy} />);
      });
      await flush();
      expect(await offers(document, 2, 'aka'), 'no list for a side whose team is not known').toEqual([]);
      await typeName(bout(document, 0, 'aka'), 'Nao Two');
      expect(window.API.addTeamMember, 'the pick waits').not.toHaveBeenCalled();
      expect(window.API.putMatchLineup).not.toHaveBeenCalled();

      await act(async () => { competition.resolve(ROSTER); });
      await flush();

      expect(window.API.addTeamMember).not.toHaveBeenCalled();
      const [, teamId, , , , memberIds] = window.API.putMatchLineup.mock.calls[0];
      expect(teamId).toBe('team-N');
      expect(memberIds).toEqual({ senpo: 'n2' });
      expect(holds(await offers(document, 2, 'aka'), 'Nao One'), 'and the side lists the copy\'s members').toBe(true);
    });

    it('is not held back for a side that carries its id: it takes its list before the roster names the team, and its picks do not wait', async () => {
      await act(async () => { render(host({ id: 'team-N', name: 'Team N', number: 'T5' })); });
      await act(async () => { memberReads[0].resolve(lists); });
      await flush();
      expect(holds(await offers(document, 2, 'aka'), 'Nao One')).toBe(true);

      await typeName(bout(document, 0, 'aka'), 'Nao Two');

      expect(window.API.addTeamMember).not.toHaveBeenCalled();
      expect(window.API.putMatchLineup.mock.calls[0][1]).toBe('team-N');
      expect(window.API.putMatchLineup.mock.calls[0][5]).toEqual({ senpo: 'n2' });
    });

    it('goes on at the deadline when the team is never known, as it did, and says the members could not be read', async () => {
      vi.useFakeTimers();
      try {
        await act(async () => { render(host(onlyName('Team N'))); });
        await act(async () => { memberReads[0].resolve(lists); });
        await type(bout(document, 0, 'aka'), 'Nao Two');
        await act(async () => { await vi.advanceTimersByTimeAsync(FETCH_TIMEOUT_MS - 1); });
        expect(window.API.addTeamMember, 'nothing is added while the roster may still name the team').not.toHaveBeenCalled();

        await act(async () => { await vi.advanceTimersByTimeAsync(1); });
        await act(async () => { await vi.advanceTimersByTimeAsync(0); });

        expect(window.API.addTeamMember).toHaveBeenCalledWith('comp1', 'Team N', 'Nao Two', 'pw');
        expect(notices(document)).toHaveLength(1);
        expect(notices(document)[0]).toContain('team member list could not be loaded');
      } finally {
        vi.useRealTimers();
      }
    });
  });
});

// The old team's lineup is dropped before the new team's is read: the effect that notices a
// side given another team is declared before the one that reads the lineups, so that holds
// whatever the read awaits. The read starts after `await fetchCompetitionDetails`, so within
// one commit the drop runs first whatever the order, and no render test can go red for a
// reversed one: the read would have to begin synchronously. The order is pinned here, so a
// change that does start the read synchronously cannot bring the race back unseen.
describe('team editor: the effect that notices a side given another team is declared before the one that reads the lineups', () => {
  it('has the side identity effect ahead of the lineup reads in the source', () => {
    const code = readCode('admin_scoring_team.jsx');
    const noticesSide = code.indexOf('sideSeen.current = {');
    const readsLineups = code.indexOf('startLineupReads({');

    expect(noticesSide, 'the effect that notices a side given another team is in the source').toBeGreaterThan(-1);
    expect(readsLineups, 'and so is the call that starts the lineup reads').toBeGreaterThan(-1);
    expect(noticesSide, 'declared first').toBeLessThan(readsLineups);
  });
});

// A side is given another team once, and the lineup sync's epoch (raised when it drops
// the side's lineup) is the one counter that says so: a pick asks the epoch it began on,
// not a second count kept beside it. The team of a side is read through sideTeam alone.
describe('team editor: one counter says a side was given another team', () => {
  it('keeps no second generation counter, and no wrapper over sideTeam', () => {
    const code = readCode('admin_scoring_team.jsx');
    expect(code).not.toMatch(/sideGeneration/);
    expect(code).not.toMatch(/teamIdForSide/);
  });
});
