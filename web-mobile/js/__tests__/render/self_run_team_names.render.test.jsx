// bc-dhas (operator decision: "Open lineups to participants"): on a self-run
// tournament a competitor names the fighters from the public score sheet.
// A typed name, a pick from the list and "+ Add" save the match lineup and
// name or add the team member, with no organiser password: the public page
// sends an empty one, and the server lets these writes through in self-run.
// Before, each of them was refused with "invalid tournament password".
//
// Mounted through the real door, MatchViewerModal and "Report result", with
// the real lineup helpers (admin_lineup.jsx) the public page loads too.
import React from 'react';
import { render, act, fireEvent, screen, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

const STUBBED_GLOBALS = {
  isHikiwake: () => false,
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  isKikenDecision: () => false,
  isTextEntry: () => false,
  isInteractiveTarget: () => false,
  confirmDialog: vi.fn().mockResolvedValue(true),
  resolveRoundIndex: () => 0,
  API: {},
  compMatches: () => [],
  compMatchesForCompetition: () => [],
  Term: ({ children }) => <span>{children}</span>,
  GlossaryHint: ({ name }) => <span title={name} />,
};

let restoreGlobals;
let MatchViewerModal;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  await import('../../admin_lineup.jsx');
  await import('../../admin_scoring_modal.jsx');
  ({ MatchViewerModal } = await import('../../viewer_match.jsx'));
});

afterAll(() => restoreGlobals());

const blank = (p) => [1, 2, 3, 4, 5].map((i) => ({ id: `${p}${i}`, index: i, name: '' }));
const named = (p, names) => names.map((name, i) => ({ id: `${p}${i + 1}`, index: i + 1, name }));

beforeEach(() => {
  window.API = {
    fetchCompetitionDetails: vi.fn().mockResolvedValue({ id: 'c1', config: { format: 'mixed', players: [] } }),
    fetchSquads: vi.fn(),
    fetchMatchLineup: vi.fn(async () => null),
    fetchTeamLineup: vi.fn(async () => null),
    // What the server now answers the public page.
    putMatchLineup: vi.fn(async (_c, teamId, matchId, positions, _pw, memberIds) => ({ teamId, matchId, positions, memberIds })),
    renameTeamMember: vi.fn(async () => true),
    addTeamMember: vi.fn(async (_c, _t, name) => ({ id: 'new-1', index: 6, name })),
    recordScore: vi.fn(async () => ({ status: 'running' })),
    hasPendingTerminalWrite: () => false,
    notePendingEdit: () => () => {},
  };
});

const teamMatch = (teamMatchType, subResults = []) => ({
  id: 'm1', compId: 'c1', compName: 'Teams', status: 'running', phase: 'pool', poolName: 'Pool 1', court: 'A',
  compKind: 'team', teamSize: 5, compFormat: 'mixed', teamMatchType,
  sideA: { id: 'team-A', name: 'Kodokan', number: 'T1' }, // Aka
  sideB: { id: 'team-B', name: 'Mumeishi', number: 'T2' }, // Shiro
  subResults,
});

async function openEditor(match, squads) {
  await act(async () => {
    render(<MatchViewerModal match={match} onClose={vi.fn()} tournament={{ mode: 'self-run', competitions: [{ id: 'c1', squads }] }} compId="c1" />);
  });
  await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Report result' })); });
  await act(async () => { await new Promise((r) => setTimeout(r, 0)); });
}

const akaBout1 = () => document.querySelector('.team-sub-match__side--aka input');

async function typeName(input, name) {
  await act(async () => {
    fireEvent.focus(input);
    fireEvent.change(input, { target: { value: name } });
    fireEvent.keyDown(input, { key: 'Enter' });
  });
  await act(async () => { await new Promise((r) => setTimeout(r, 0)); });
}

const noError = () => {
  expect(document.querySelector('[data-testid="team-editor-error"]'), 'no refusal is shown').toBeNull();
  expect(document.querySelector('[data-testid="team-editor-lineup-warning"]'), 'no warning is shown').toBeNull();
};

describe.each(['fixed', 'kachinuki'])('a participant names bout 1 of a %s team match (bc-dhas)', (tmt) => {
  it('a typed name names the position\'s numbered member and saves the lineup', async () => {
    await openEditor(teamMatch(tmt), { 'team-A': blank('a'), 'team-B': blank('b') });
    await typeName(akaBout1(), 'Mei Ito');

    expect(window.API.renameTeamMember).toHaveBeenCalledWith('c1', 'team-A', 'a1', 'Mei Ito', '');
    expect(window.API.putMatchLineup).toHaveBeenCalledWith('c1', 'team-A', 'm1', { senpo: 'Mei Ito' }, '', { senpo: 'a1' });
    noError();
    expect(akaBout1().value).toBe('Mei Ito');
  });
});

describe('a participant fills in a fixed-order row from the list (bc-dhas)', () => {
  const squads = () => ({
    'team-A': named('a', ['Ren Abe', 'Kai Mori', 'Yui Sato', 'Rin Ota', 'Sho Ueda']),
    'team-B': blank('b'),
  });

  it('a pick saves the lineup with the picked member', async () => {
    await openEditor(teamMatch('fixed'), squads());
    await act(async () => { fireEvent.focus(akaBout1()); });
    const kai = [...document.querySelectorAll('.team-sub-match__side--aka .pmf__option')].find((b) => b.textContent.includes('Kai Mori'));
    expect(kai, 'Kai Mori is offered').toBeTruthy();
    await act(async () => { fireEvent.mouseDown(kai); });
    await act(async () => { await new Promise((r) => setTimeout(r, 0)); });

    expect(window.API.putMatchLineup).toHaveBeenCalledWith('c1', 'team-A', 'm1', { senpo: 'Kai Mori' }, '', { senpo: 'a2' });
    expect(window.API.renameTeamMember).not.toHaveBeenCalled();
    noError();
    expect(akaBout1().value).toBe('Kai Mori');
  });

  it('"+ Add" adds the team member and saves the lineup with them', async () => {
    await openEditor(teamMatch('fixed'), squads());
    const input = akaBout1();
    await act(async () => {
      fireEvent.focus(input);
      fireEvent.change(input, { target: { value: 'Jun Oda' } });
    });
    const add = document.querySelector('.team-sub-match__side--aka .lineup-name__add');
    expect(add, 'the "+ Add" row is offered').toBeTruthy();
    await act(async () => { fireEvent.mouseDown(add); });
    await act(async () => { await new Promise((r) => setTimeout(r, 0)); });

    expect(window.API.addTeamMember).toHaveBeenCalledWith('c1', 'team-A', 'Jun Oda', '');
    expect(window.API.putMatchLineup).toHaveBeenCalledWith('c1', 'team-A', 'm1', { senpo: 'Jun Oda' }, '', { senpo: 'new-1' });
    noError();
    expect(akaBout1().value).toBe('Jun Oda');
  });
});

describe('a participant names a later kachinuki bout\'s fighter (bc-dhas)', () => {
  it('a name typed over a picked member with no name names that member, with no warning', async () => {
    const played = [
      { position: 1, sideA: 'Ren Abe', sideB: 'Mei Ito', ipponsA: ['M', 'M'], ipponsB: [], winner: 'Ren Abe' },
      { position: 2, sideA: 'Ren Abe', sideB: '', ipponsA: [], ipponsB: [] },
    ];
    await openEditor(teamMatch('kachinuki', played), { 'team-A': blank('a'), 'team-B': blank('b') });
    const shiroNow = () => [...document.querySelectorAll('.team-sub-match__side--shiro input')].pop();
    await act(async () => { fireEvent.focus(shiroNow()); });
    const slot = [...document.querySelectorAll('.pmf__option')].find((b) => b.textContent.includes('T2.3'));
    expect(slot, 'the numbered member T2.3 is offered').toBeTruthy();
    await act(async () => { fireEvent.mouseDown(slot); });
    await typeName(shiroNow(), 'Ito');

    expect(window.API.renameTeamMember).toHaveBeenCalledWith('c1', 'team-B', 'b3', 'Ito', '');
    expect(window.API.putMatchLineup, 'a later kachinuki bout is not a lineup position').not.toHaveBeenCalled();
    noError();
    await waitFor(() => {
      const sent = window.API.recordScore.mock.calls.map((c) => c[2]).pop();
      const bout2 = (sent && sent.subResults || []).find((s) => s.position === 2);
      expect(bout2).toMatchObject({ sideB: 'Ito', sideBMemberId: 'b3' });
    }, { timeout: 3000 });
  });

  it('a rename that fails points the participant at the organizer, not the Lineups page they cannot open', async () => {
    window.API.renameTeamMember = vi.fn(async () => { throw new Error('this team member already has a name'); });
    const played = [
      { position: 1, sideA: 'Ren Abe', sideB: 'Mei Ito', ipponsA: ['M', 'M'], ipponsB: [], winner: 'Ren Abe' },
      { position: 2, sideA: 'Ren Abe', sideB: '', ipponsA: [], ipponsB: [] },
    ];
    await openEditor(teamMatch('kachinuki', played), { 'team-A': blank('a'), 'team-B': blank('b') });
    const shiroNow = () => [...document.querySelectorAll('.team-sub-match__side--shiro input')].pop();
    await act(async () => { fireEvent.focus(shiroNow()); });
    const slot = [...document.querySelectorAll('.pmf__option')].find((b) => b.textContent.includes('T2.3'));
    await act(async () => { fireEvent.mouseDown(slot); });
    await typeName(shiroNow(), 'Ito');

    const warning = document.querySelector('[data-testid="team-editor-lineup-warning"]');
    expect(warning, 'the failed rename is reported').toBeTruthy();
    expect(warning.textContent).toContain('Ask the tournament organizer to rename them.');
    expect(warning.textContent).not.toContain('Lineups page');
  });

  // Another device named that member first: the server refuses the rename with
  // a code and its own sentence, which the row shows as it is, the same words
  // a saved lineup's warning uses for it (memberRefusalNote).
  it('a rename refused because the member already has a name says so in the server\'s words', async () => {
    const sentence = 'This team member already has a name. Ask the tournament organizer to change it.';
    window.API.renameTeamMember = vi.fn(async () => { throw Object.assign(new Error(sentence), { code: 'member_already_named' }); });
    const played = [
      { position: 1, sideA: 'Ren Abe', sideB: 'Mei Ito', ipponsA: ['M', 'M'], ipponsB: [], winner: 'Ren Abe' },
      { position: 2, sideA: 'Ren Abe', sideB: '', ipponsA: [], ipponsB: [] },
    ];
    await openEditor(teamMatch('kachinuki', played), { 'team-A': blank('a'), 'team-B': blank('b') });
    const shiroNow = () => [...document.querySelectorAll('.team-sub-match__side--shiro input')].pop();
    await act(async () => { fireEvent.focus(shiroNow()); });
    const slot = [...document.querySelectorAll('.pmf__option')].find((b) => b.textContent.includes('T2.3'));
    await act(async () => { fireEvent.mouseDown(slot); });
    await typeName(shiroNow(), 'Ito');

    const warning = document.querySelector('[data-testid="team-editor-lineup-warning"]');
    expect(warning, 'the refused rename is reported').toBeTruthy();
    expect(warning.textContent).toBe(`"Ito" was used for this bout, but the team member could not be renamed. ${sentence}`);
  });
});
