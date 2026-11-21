// bc-kno1: the server seats kachinuki bout 1 from the match's lineup on every
// write, whatever the match's status (engine ReseatKachinukiOpenersTx), so a
// fighter named on bout 1 any other way is overruled by the seat. So every pick
// on the FIRST row goes through the match lineup (submitInlineLineup ->
// putMatchLineup), running or not: a roster pick, a free name typed where the
// team has no members to list, and a manual row there. "+ Add next bout
// manually" numbers from bout 2, so the free name on a match that is not
// running is the reachable case that used to ride the draft row instead
// (pickManual), and be replaced by the seat at the start. Later rows keep
// riding the bout itself and never become a lineup write. The one exception is a
// side whose lineup read failed: its bout 1 is named by hand, on the row.
import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { makeFakeOutbox } from '../helpers/fake_outbox.js';
import { answered } from '../helpers/team_members.js';

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
  await import('../../admin_lineup.jsx');
  await import('../../admin_scoring_modal.jsx');
  ScoreEditorModal = window.ScoreEditorModal;
});

afterAll(() => restoreGlobals());

// Seeded nameless members, as a fresh team holds them.
const blank = (p) => [1, 2, 3].map((i) => ({ id: `${p}${i}`, index: i, name: '' }));

function stubApi(squads) {
  window.API = {
    fetchCompetitionDetails: vi.fn().mockResolvedValue({
      id: 'comp1',
      config: { format: 'mixed', teamMatchType: 'kachinuki', naginata: false, players: [] },
    }),
    recordScore: vi.fn().mockResolvedValue(undefined),
    recordDaihyosen: vi.fn(),
    removeDaihyosen: vi.fn(),
    putMatchLineup: vi.fn(async (_c, _t, matchId, positions, _p, memberIds) => ({ matchId, positions, memberIds: memberIds || {} })),
    renameTeamMember: vi.fn(async (_c, _t, id, name) => answered({ id, index: Number(id.slice(1)) }, { name })),
    addTeamMember: vi.fn(async (_c, _t, name) => answered({ id: 'new-1', index: 4 }, { name })),
    recordDecision: vi.fn(),
    fetchSquads: vi.fn().mockResolvedValue(squads),
    fetchLineupInForce: vi.fn().mockResolvedValue(null),
    fetchKachinukiRoster: vi.fn().mockResolvedValue(null),
    ...makeFakeOutbox().api,
  };
}

beforeEach(() => stubApi({ 'team-A': blank('a'), 'team-B': blank('b') }));

function kachinukiMatch(status, subResults = []) {
  return {
    id: 'm1', compId: 'comp1', status, phase: 'pool', poolName: 'Pool 1', court: 'A',
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

async function mount(match) {
  await act(async () => {
    render(<ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={vi.fn()} password="secret" />);
  });
  await settle();
}

const liveRow = () => document.querySelector('.team-sub-match:not(.team-sub-match--readonly)');
const akaInput = () => liveRow().querySelector('.team-sub-match__side--aka input');

async function typeName(input, name) {
  await act(async () => {
    fireEvent.focus(input);
    fireEvent.change(input, { target: { value: name } });
    fireEvent.keyDown(input, { key: 'Enter' });
  });
  await settle();
}

describe.each(['scheduled', 'running'])('a pick on kachinuki bout 1 of a %s match goes through the lineup', (status) => {
  it('a nameless member picked by number is saved to the lineup by id', async () => {
    await mount(kachinukiMatch(status));
    // Bout 1 shows member 1 of each team by number before the server seats it.
    expect(liveRow().querySelector('[data-testid="team-sub-match-member-label-aka"]').textContent).toBe('T1.1');
    expect(liveRow().querySelector('[data-testid="team-sub-match-member-label-shiro"]').textContent).toBe('T2.1');
    await act(async () => { fireEvent.focus(akaInput()); });
    const two = [...liveRow().querySelectorAll('.team-sub-match__side--aka .pmf__option')].find((b) => b.textContent.includes('T1.2'));
    expect(two, 'the nameless member T1.2 is offered').toBeTruthy();
    await act(async () => { fireEvent.click(two); });
    await settle();
    expect(window.API.putMatchLineup).toHaveBeenCalledWith('comp1', 'team-A', 'm1', { '1': '' }, 'secret', { '1': 'a2' }, ['1'], expect.any(Object));
  });

  it('a free name typed where the team lists no members is saved to the lineup', async () => {
    stubApi({ 'team-A': [], 'team-B': blank('b') });
    await mount(kachinukiMatch(status));
    await typeName(akaInput(), 'Ito');
    expect(window.API.putMatchLineup).toHaveBeenCalledTimes(1);
    const [compId, teamId, matchId, positions, , , changed] = window.API.putMatchLineup.mock.calls[0];
    expect([compId, teamId, matchId, changed]).toEqual(['comp1', 'team-A', 'm1', ['1']]);
    expect(positions).toEqual({ '1': 'Ito' });
    expect(akaInput().value).toBe('Ito');
  });
});

// A lineup read that FAILED (a damaged file or a lost connection) leaves bout 1 to be
// named by hand, on the bout row like a later bout; a read still out refuses the pick
// and reads again, so the name goes once the lineup shows.
describe('kachinuki bout 1 when the lineup could not be read', () => {
  const noticeText = () => [...document.querySelectorAll('[data-testid="team-editor-lineup-warning"]')].map((n) => n.textContent);

  it.each(['scheduled', 'running'])('a failed read lets a name typed on bout 1 of a %s match ride the row', async (status) => {
    stubApi({ 'team-A': [], 'team-B': blank('b') });
    window.API.fetchLineupInForce = vi.fn().mockRejectedValue(new Error('damaged'));
    await mount(kachinukiMatch(status));
    await typeName(akaInput(), 'Ito');
    expect(window.API.putMatchLineup).not.toHaveBeenCalled();
    expect(noticeText()).toEqual([]);
    expect(akaInput().value).toBe('Ito');
  });

  // Before the start the name is an entry of the sheet's draft, like any score entered
  // then (unsaved_entries.jsx: kept in memory, warned about on leaving, carried by the
  // Start): Start match sends it on bout 1.
  it('a name typed on bout 1 of a match not yet started rides the Start', async () => {
    stubApi({ 'team-A': [], 'team-B': blank('b') });
    window.API.fetchLineupInForce = vi.fn().mockRejectedValue(new Error('damaged'));
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    await act(async () => {
      render(<ScoreEditorModal match={kachinukiMatch('scheduled')} onClose={vi.fn()} onSubmit={onSubmit} password="secret" />);
    });
    await settle();
    await typeName(akaInput(), 'Ito');
    const start = [...document.querySelectorAll('.score-nav__actions button')].find((b) => b.textContent === 'Start match');
    await act(async () => { fireEvent.click(start); });
    await settle();
    expect(onSubmit).toHaveBeenCalledTimes(1);
    const [patch] = onSubmit.mock.calls[0];
    expect(patch.status).toBe('running');
    expect(patch.subResults[0]).toMatchObject({ position: 1, sideA: 'Ito' });
  });

  it('a failed read lets a listed member picked on bout 1 ride the row', async () => {
    window.API.fetchLineupInForce = vi.fn().mockRejectedValue(new Error('damaged'));
    await mount(kachinukiMatch('scheduled'));
    await act(async () => { fireEvent.focus(akaInput()); });
    const two = [...liveRow().querySelectorAll('.team-sub-match__side--aka .pmf__option')].find((b) => b.textContent.includes('T1.2'));
    expect(two, 'the nameless member T1.2 is offered').toBeTruthy();
    await act(async () => { fireEvent.click(two); });
    await settle();
    expect(window.API.putMatchLineup).not.toHaveBeenCalled();
    expect(noticeText()).toEqual([]);
  });

  it('a read still out refuses the name, says it is being read again, and writes nothing', async () => {
    stubApi({ 'team-A': [], 'team-B': blank('b') });
    window.API.fetchLineupInForce = vi.fn(() => new Promise(() => {}));
    await mount(kachinukiMatch('scheduled'));
    await typeName(akaInput(), 'Ito');
    expect(window.API.putMatchLineup).not.toHaveBeenCalled();
    expect(noticeText()).toEqual([expect.stringContaining('being read again')]);
  });

  it('once the lineup is read, a pick on bout 1 goes through it again', async () => {
    let healthy = false;
    window.API.fetchLineupInForce = vi.fn(async () => {
      if (!healthy) throw new Error('offline');
      return null;
    });
    await mount(kachinukiMatch('scheduled'));
    healthy = true;
    await act(async () => { window.dispatchEvent(new Event('online')); });
    await settle();
    await act(async () => { fireEvent.focus(akaInput()); });
    const two = [...liveRow().querySelectorAll('.team-sub-match__side--aka .pmf__option')].find((b) => b.textContent.includes('T1.2'));
    await act(async () => { fireEvent.click(two); });
    await settle();
    expect(window.API.putMatchLineup).toHaveBeenCalledTimes(1);
  });
});

// team-members.yaml unreadable (the members read fails, the lineups are fine): a name
// typed on bout 1 goes on without the members (stage 3 review 3, F1): the member is
// added and the lineup save naming it is queued behind the add. The server refuses the
// add 409 team_members_unreadable, which is final: the outbox drops the add and sends
// the save without its id (outbox_blockers.test.jsx), and the save lands by name
// alone. Before, the add's 500 was retried for the page's life and held the save, and
// every later write of the match, behind it. The row says the add was refused.
describe('kachinuki bout 1 when the team members could not be read', () => {
  const noticeText = () => [...document.querySelectorAll('[data-testid="team-editor-lineup-warning"]')].map((n) => n.textContent);
  const SERVER_SENTENCE = 'The team members could not be read, so the team member was not saved. The organiser needs to check team-members.yaml in the tournament data.';

  it.each(['scheduled', 'running'])('a name typed on bout 1 of a %s match adds the member, and the refused add is said in the row', async (status) => {
    window.API.fetchSquads = vi.fn().mockRejectedValue(new Error('internal error'));
    let refuse;
    window.API.addTeamMember = vi.fn(async (_c, _t, name, _p, opts) => {
      refuse = () => opts.onRefusal(Object.assign(new Error(SERVER_SENTENCE), { code: 'team_members_unreadable', status: 409 }));
      return answered({ id: 'new-1', index: 4 }, { name });
    });
    let land;
    window.API.putMatchLineup = vi.fn(async (_c, _t, matchId, positions) => ({
      pending: true, settled: new Promise((r) => { land = () => r({ matchId, positions, memberIds: {} }); }),
    }));
    await mount(kachinukiMatch(status));
    await typeName(akaInput(), 'Maeda');
    expect(window.API.addTeamMember, 'the add is sent once').toHaveBeenCalledTimes(1);
    expect(window.API.putMatchLineup).toHaveBeenCalledTimes(1);
    const [, teamId, , positions, , memberIds, changed] = window.API.putMatchLineup.mock.calls[0];
    expect([teamId, changed]).toEqual(['team-A', ['1']]);
    expect(positions).toEqual({ '1': 'Maeda' });
    expect(memberIds, 'queued behind the add, which the outbox strips once refused').toEqual({ '1': 'new-1' });
    // The add goes first and is refused; the save then lands by name alone.
    await act(async () => { refuse(); });
    expect(noticeText(), 'the refused add, said in the row').toEqual([`Lineup saved, but Senpo (Maeda) could not be linked to a team member (${SERVER_SENTENCE}). Scores will still record normally.`]);
    await act(async () => { land(); });
    await settle();
    expect(akaInput().value, 'bout 1 shows the name').toBe('Maeda');
    expect(noticeText(), 'once the save lands').toEqual(['Saved by name only: the team member list could not be loaded. Scores will still record normally.']);
  });
});

describe('a pick on a later kachinuki bout rides the bout, never the lineup', () => {
  it('bout 2 of a running match writes no lineup', async () => {
    const bout1 = {
      position: 1, sideA: '', sideAMemberId: 'a1', sideB: '', sideBMemberId: 'b1', ipponsA: [], ipponsB: ['M'],
      winner: 'Team B', winnerMemberId: 'b1', decision: 'fought', recorded: true,
    };
    const bout2 = { position: 2, sideA: '', sideAMemberId: 'a2', sideB: '', sideBMemberId: 'b1', ipponsA: [], ipponsB: [] };
    await mount(kachinukiMatch('running', [bout1, bout2]));
    await act(async () => { fireEvent.focus(akaInput()); });
    const three = [...liveRow().querySelectorAll('.team-sub-match__side--aka .pmf__option')].find((b) => b.textContent.includes('T1.3'));
    expect(three, 'the nameless member T1.3 is offered').toBeTruthy();
    await act(async () => { fireEvent.click(three); });
    await settle();
    expect(window.API.putMatchLineup).not.toHaveBeenCalled();
  });
});
