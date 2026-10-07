// The Lineups page picks WHICH lineup it edits (operator ruling 2026-10-05: "by
// default, a team carries the previous team match lineup. You can have a
// different team lineup in every team match"): the team's starting lineup, or
// the lineup of any one of its team matches. The matches come from the match
// data the competition page holds beside the competition's config (real
// compMatchesForCompetition, real match shapes), never from a stub that hands
// the list over, so a page that read the config alone would list none.

import React from 'react';
import { render, act, fireEvent, within } from '@testing-library/react';
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { bracketRoundLabel } from '../../bracket.jsx';

const SQUAD = [
  { id: 'mem-1', index: 1, name: 'Aoki' },
  { id: 'mem-2', index: 2, name: 'Sato' },
  { id: 'mem-3', index: 3, name: 'Ito' },
  { id: 'mem-4', index: 4, name: 'Mori' },
];
const NAMES = { positions: { 1: 'Aoki', 2: 'Sato', 3: 'Ito' }, memberIds: { 1: 'mem-1', 2: 'mem-2', 3: 'mem-3' } };
const lineupFor = (extra) => ({ teamId: 'team-a', competitionId: 'comp-1', ...NAMES, saved: true, ...extra });
const STARTING = lineupFor({ round: 0, sourceRound: 0 });
const CARRIED = lineupFor({ matchId: 'Pool A-0', sourceMatchId: 'Pool A-0' });
const OWN = lineupFor({ matchId: 'Pool A-2', sourceMatchId: 'Pool A-2' });

const A = { id: 'team-a', name: 'Team A' };
const B = { id: 'team-b', name: 'Team B' };
const C = { id: 'team-c', name: 'Team C' };
const D = { id: 'team-d', name: 'Team D' };
const E = { id: 'team-e', name: 'Team E' };
const NOBODY = { id: '', name: '' };
const match = (id, sideA, sideB, extra = {}) => ({ id, status: 'scheduled', sideA, sideB, ...extra });

// Stored out of order on purpose: the page orders them, the draw's file order
// is not its contract. Team A also holds a tiebreaker and a pool daihyosen,
// which are individual bouts and no team match.
const POOL_MATCHES = [
  match('Pool A-2', A, C),
  match('Pool A-1', B, C),
  match('Pool A-0', A, B),
  match('Pool A-TB-0', A, B),
  match('Pool A-DH-0', A, C),
];
const BRACKET = {
  rounds: [
    [
      match('m-r0-0', A, D, { matchNumber: 1 }),
      match('m-r0-1', B, C, { matchNumber: 2 }),
      match('m-r0-2', E, NOBODY, { hidden: true }),
    ],
    [match('m-r1-0', A, { id: '', name: 'Winner of r0-1' }, { matchNumber: 3 })],
  ],
  thirdPlaceMatch: match('m-bronze', A, B),
};
const PLAYERS = [A, B, C, D, E].map((t) => ({ ...t, number: '' }));
const COMP = {
  id: 'comp-1', name: 'Team Event', kind: 'team', format: 'mixed', status: 'active', teamSize: 3, players: PLAYERS,
};

const A_MATCH_LABELS = ['Pool A · Match 1', 'Pool A · Match 3', 'Match 1', 'Match 3', 'the 3rd-place match'];

let AdminTeamLineupsList;
let saved;
let api;

beforeEach(async () => {
  saved = { API: window.API, confirmDialog: window.confirmDialog, bracketRoundLabel: window.bracketRoundLabel };
  window.bracketRoundLabel = bracketRoundLabel;
  window.confirmDialog = vi.fn().mockResolvedValue(true);
  api = {
    fetchTeamLineup: vi.fn().mockResolvedValue(null),
    fetchLineupInForce: vi.fn().mockResolvedValue(null),
    fetchSquads: vi.fn().mockResolvedValue({ 'team-a': SQUAD }),
    putTeamLineup: vi.fn().mockResolvedValue({}),
    putMatchLineup: vi.fn().mockImplementation((_c, _t, _m, positions, _pw, memberIds) => Promise.resolve({ positions, memberIds })),
    deleteMatchLineup: vi.fn().mockResolvedValue(true),
  };
  window.API = api;
  ({ AdminTeamLineupsList } = await import('../../admin_lineup.jsx'));
});

afterEach(() => {
  window.API = saved.API;
  window.confirmDialog = saved.confirmDialog;
  window.bracketRoundLabel = saved.bracketRoundLabel;
});

// The competition page holds the config beside the match data, as props.
const pageFor = (props = {}) => (
  <AdminTeamLineupsList
    comp={COMP}
    poolMatches={POOL_MATCHES}
    bracket={BRACKET}
    password="pw"
    showToast={vi.fn()}
    {...props}
  />
);

async function mountPage(props = {}) {
  let utils;
  await act(async () => { utils = render(pageFor(props)); });
  await act(async () => { await Promise.resolve(); });
  return utils;
}

const targetSelect = (utils) => utils.getByLabelText('Lineup for');
const optionTexts = (select) => within(select).getAllByRole('option').map((o) => o.textContent);
const saveButton = (utils) => utils.getByRole('button', { name: /^Save lineup$/ });

async function chooseTarget(utils, value) {
  await act(async () => { fireEvent.change(targetSelect(utils), { target: { value } }); });
  await act(async () => { await Promise.resolve(); });
}

async function pick(utils, position, memberId) {
  await act(async () => {
    fireEvent.change(utils.getByTestId(`lineup-position-${position}`), { target: { value: memberId } });
  });
}

async function click(button) {
  await act(async () => { fireEvent.click(button); });
  await act(async () => { await Promise.resolve(); });
}

describe('the "Lineup for" select', () => {
  it('lists the starting lineup, then the team\'s own team matches in match order', async () => {
    const utils = await mountPage();
    expect(optionTexts(targetSelect(utils))).toEqual(['Starting lineup', ...A_MATCH_LABELS]);
    expect(targetSelect(utils).value).toBe('');
    // The Round number input is gone.
    expect(utils.queryByLabelText('Round')).toBeNull();
  });

  it('leaves out a pool tiebreaker, a pool daihyosen, a bye and other teams\' matches', async () => {
    const utils = await mountPage();
    const options = within(targetSelect(utils)).getAllByRole('option');
    const values = options.map((o) => o.value);
    expect(values).not.toContain('Pool A-TB-0');
    expect(values).not.toContain('Pool A-DH-0');
    expect(values).not.toContain('Pool A-1');
    expect(values).not.toContain('m-r0-1');
    // A team whose only knockout match is a bye has no match to field a lineup in.
    await act(async () => { fireEvent.change(utils.getByLabelText('Team'), { target: { value: 'team-e' } }); });
    expect(optionTexts(targetSelect(utils))).toEqual(['Starting lineup']);
  });

  it('lists a knockout match whose opponent is not decided yet: that side carries a placeholder name', async () => {
    const utils = await mountPage({
      poolMatches: undefined,
      bracket: { rounds: [[match('m-r0-0', A, { id: '', name: 'Winner of Match 2' }, { matchNumber: 1 })]] },
    });
    expect(optionTexts(targetSelect(utils))).toEqual(['Starting lineup', 'Match 1']);
  });

  it('leaves out a bye: a match with a side left empty, hidden or not', async () => {
    // A knockout bye the draw did not hide, and a Swiss bye (sideB "") alongside a real round.
    const knockout = await mountPage({
      poolMatches: undefined,
      bracket: { rounds: [[match('m-r0-0', A, D, { matchNumber: 1 }), match('m-r0-1', A, NOBODY, { matchNumber: 2 })]] },
    });
    expect(optionTexts(targetSelect(knockout))).toEqual(['Starting lineup', 'Match 1']);
    knockout.unmount();

    const swiss = await mountPage({
      comp: { ...COMP, format: 'swiss' },
      poolMatches: [match('Swiss-R1-0', A, C), match('Swiss-R2-0', A, NOBODY)],
      bracket: undefined,
    });
    expect(optionTexts(targetSelect(swiss))).toEqual(['Starting lineup', 'Round 1 · Match 1']);
  });

  it('leaves out a hidden match the team is seated in, whatever its opponent is named', async () => {
    const utils = await mountPage({
      poolMatches: undefined,
      bracket: { rounds: [[match('m-r0-0', A, D, { matchNumber: 1 }), match('m-r0-1', A, B, { hidden: true })]] },
    });
    expect(optionTexts(targetSelect(utils))).toEqual(['Starting lineup', 'Match 1']);
  });

  it('keeps a Swiss team\'s rounds in the order they were drawn, not by match number alone', async () => {
    // Round 2's match is number 1 of its round and Round 1's is number 4, so
    // ordering by number alone would put Round 2 first.
    const utils = await mountPage({
      comp: { ...COMP, format: 'swiss' },
      poolMatches: [match('Swiss-R1-3', A, C), match('Swiss-R2-0', A, B)],
      bracket: undefined,
    });
    expect(optionTexts(targetSelect(utils))).toEqual(['Starting lineup', 'Round 1 · Match 4', 'Round 2 · Match 1']);
  });

  it('offers only the starting lineup before the draw', async () => {
    const utils = await mountPage({ comp: { ...COMP, status: 'setup' }, poolMatches: undefined, bracket: undefined });
    expect(optionTexts(targetSelect(utils))).toEqual(['Starting lineup']);
  });

  it('reads the match data off the competition itself when it carries it', async () => {
    const utils = await mountPage({
      comp: { ...COMP, poolMatches: POOL_MATCHES, bracket: BRACKET },
      poolMatches: undefined,
      bracket: undefined,
    });
    expect(optionTexts(targetSelect(utils))).toEqual(['Starting lineup', ...A_MATCH_LABELS]);
  });

  it('goes back to the starting lineup when another team is picked', async () => {
    api.fetchLineupInForce.mockResolvedValue(CARRIED);
    const utils = await mountPage();
    await chooseTarget(utils, 'Pool A-2');
    expect(targetSelect(utils).value).toBe('Pool A-2');
    await act(async () => { fireEvent.change(utils.getByLabelText('Team'), { target: { value: 'team-b' } }); });
    expect(targetSelect(utils).value).toBe('');
  });
});

describe('the starting lineup', () => {
  it('reads round 0 exactly, with no fallback and no lineup in force', async () => {
    const utils = await mountPage();
    expect(api.fetchTeamLineup).toHaveBeenCalledTimes(1);
    expect(api.fetchTeamLineup).toHaveBeenCalledWith('comp-1', 'team-a', 0);
    expect(api.fetchLineupInForce).not.toHaveBeenCalled();
    expect(utils.getByText('Starting lineup', { selector: '.overline' })).toBeTruthy();
  });

  it('saves to round 0', async () => {
    const utils = await mountPage();
    await pick(utils, 1, 'mem-1');
    await click(saveButton(utils));
    expect(api.putTeamLineup).toHaveBeenCalledTimes(1);
    expect(api.putTeamLineup).toHaveBeenCalledWith('comp-1', 'team-a', 0, { 1: 'Aoki' }, 'pw', { 1: 'mem-1' });
    expect(api.putMatchLineup).not.toHaveBeenCalled();
  });
});

describe('a match', () => {
  it('loads the lineup in force and says where it comes from', async () => {
    api.fetchLineupInForce.mockResolvedValue(CARRIED);
    const utils = await mountPage();
    await chooseTarget(utils, 'Pool A-2');

    expect(api.fetchLineupInForce).toHaveBeenCalledWith('comp-1', 'team-a', 'Pool A-2');
    expect(utils.getByText('Same as Pool A · Match 1')).toBeTruthy();
    expect(utils.getByTestId('lineup-position-1').value).toBe('mem-1');
    expect(utils.getByTestId('lineup-position-2').value).toBe('mem-2');
    // The starting lineup's own read is not the match's.
    expect(api.fetchTeamLineup).toHaveBeenCalledTimes(1);
  });

  it('keeps Save disabled until something changes, then writes the match\'s own lineup', async () => {
    api.fetchLineupInForce.mockResolvedValue(CARRIED);
    const utils = await mountPage();
    await chooseTarget(utils, 'Pool A-2');

    expect(saveButton(utils).disabled).toBe(true);
    expect(saveButton(utils).getAttribute('title')).toBe('No changes to save');
    await click(saveButton(utils));
    expect(api.putMatchLineup).not.toHaveBeenCalled();

    await pick(utils, 2, 'mem-4');
    expect(saveButton(utils).disabled).toBe(false);
    await click(saveButton(utils));

    expect(api.putMatchLineup).toHaveBeenCalledTimes(1);
    expect(api.putMatchLineup).toHaveBeenCalledWith(
      'comp-1', 'team-a', 'Pool A-2', { 1: 'Aoki', 2: 'Mori', 3: 'Ito' }, 'pw', { 1: 'mem-1', 2: 'mem-4', 3: 'mem-3' },
    );
    expect(api.putTeamLineup).not.toHaveBeenCalled();
  });

  it('is the match\'s own lineup once saved, and Save disables again', async () => {
    api.fetchLineupInForce.mockResolvedValue(CARRIED);
    const utils = await mountPage();
    await chooseTarget(utils, 'Pool A-2');
    await pick(utils, 2, 'mem-4');
    await click(saveButton(utils));

    expect(utils.getByText('Lineup for this match')).toBeTruthy();
    expect(utils.queryByText('Same as Pool A · Match 1')).toBeNull();
    expect(saveButton(utils).disabled).toBe(true);
    expect(utils.getByRole('button', { name: "Use the previous match's lineup" })).toBeTruthy();
  });

  it('an edit put back to what was loaded is not a change', async () => {
    api.fetchLineupInForce.mockResolvedValue(CARRIED);
    const utils = await mountPage();
    await chooseTarget(utils, 'Pool A-2');
    await pick(utils, 2, 'mem-4');
    await pick(utils, 2, 'mem-2');
    expect(saveButton(utils).disabled).toBe(true);
  });

  it('a queued (offline) save keeps the lineup unsaved', async () => {
    api.fetchLineupInForce.mockResolvedValue(CARRIED);
    api.putMatchLineup.mockResolvedValue({ queued: true });
    const utils = await mountPage();
    await chooseTarget(utils, 'Pool A-2');
    await pick(utils, 2, 'mem-4');
    await click(saveButton(utils));
    expect(saveButton(utils).disabled).toBe(false);
    expect(utils.queryByText('Lineup for this match')).toBeNull();
  });

  it('shows the starting lineup as the source of a match that carries it', async () => {
    api.fetchLineupInForce.mockResolvedValue(STARTING);
    const utils = await mountPage();
    await chooseTarget(utils, 'Pool A-0');
    expect(utils.getByText('Starting lineup', { selector: '[data-testid="lineup-source"]' })).toBeTruthy();
    expect(utils.queryByRole('button', { name: "Use the previous match's lineup" })).toBeNull();
  });

  it('reads a failed load as an error, never as an empty lineup', async () => {
    api.fetchLineupInForce.mockRejectedValue(new Error('competition not found'));
    const utils = await mountPage();
    await chooseTarget(utils, 'Pool A-2');
    expect(utils.getByText('competition not found')).toBeTruthy();
  });
});

describe('Use the previous match\'s lineup', () => {
  it('is offered only on a match\'s own lineup', async () => {
    api.fetchLineupInForce.mockResolvedValue(CARRIED);
    const utils = await mountPage();
    expect(utils.queryByRole('button', { name: "Use the previous match's lineup" })).toBeNull();
    await chooseTarget(utils, 'Pool A-2');
    expect(utils.queryByRole('button', { name: "Use the previous match's lineup" })).toBeNull();
  });

  it('removes the match\'s own lineup and shows the one it carries again', async () => {
    api.fetchLineupInForce.mockResolvedValueOnce(OWN).mockResolvedValue(CARRIED);
    const utils = await mountPage();
    await chooseTarget(utils, 'Pool A-2');
    expect(utils.getByText('Lineup for this match')).toBeTruthy();

    await click(utils.getByRole('button', { name: "Use the previous match's lineup" }));

    // The confirm says what else follows.
    expect(window.confirmDialog).toHaveBeenCalledTimes(1);
    expect(window.confirmDialog.mock.calls[0][0].message).toMatch(/later match.*no lineup of their own/i);
    expect(api.deleteMatchLineup).toHaveBeenCalledWith('comp-1', 'team-a', 'Pool A-2', 'pw');
    expect(api.fetchLineupInForce).toHaveBeenCalledTimes(2);
    expect(utils.getByText('Same as Pool A · Match 1')).toBeTruthy();
    expect(utils.queryByRole('button', { name: "Use the previous match's lineup" })).toBeNull();
  });

  it('shows an empty lineup when nothing is left for the match to carry', async () => {
    api.fetchLineupInForce.mockResolvedValueOnce(OWN).mockResolvedValue(null);
    const utils = await mountPage();
    await chooseTarget(utils, 'Pool A-2');
    await click(utils.getByRole('button', { name: "Use the previous match's lineup" }));
    expect(utils.getByText('No lineup saved yet')).toBeTruthy();
    expect(utils.getByTestId('lineup-position-1').value).toBe('');
  });

  it('removes nothing when the operator declines', async () => {
    api.fetchLineupInForce.mockResolvedValue(OWN);
    window.confirmDialog.mockResolvedValue(false);
    const utils = await mountPage();
    await chooseTarget(utils, 'Pool A-2');
    await click(utils.getByRole('button', { name: "Use the previous match's lineup" }));
    expect(api.deleteMatchLineup).not.toHaveBeenCalled();
    expect(utils.getByText('Lineup for this match')).toBeTruthy();
  });

  it('reports a refused removal and keeps the lineup shown', async () => {
    api.fetchLineupInForce.mockResolvedValue(OWN);
    api.deleteMatchLineup.mockRejectedValue(new Error('Failed to delete match lineup'));
    const utils = await mountPage();
    await chooseTarget(utils, 'Pool A-2');
    await click(utils.getByRole('button', { name: "Use the previous match's lineup" }));
    expect(utils.getByText('Failed to delete match lineup')).toBeTruthy();
    expect(utils.getByText('Lineup for this match')).toBeTruthy();
  });
});

// The match chosen in "Lineup for" can stop seating the team while the page is open:
// a feeding match is reopened or corrected on another device, or the draw is
// discarded. The page never falls back to the starting lineup by itself: the next edit
// and Save there would rewrite a lineup that every later match without its own carries.
describe('a match the team is no longer in', () => {
  // Team C plays the final until the match feeding it is corrected and team B plays it.
  const finalWith = (sideB) => ({
    rounds: [
      [match('m-r0-0', A, D, { matchNumber: 1 }), match('m-r0-1', B, C, { matchNumber: 2 })],
      [match('m-r1-0', A, sideB, { matchNumber: 3 })],
    ],
  });
  const pageWith = (bracket) => pageFor({ poolMatches: undefined, bracket });
  const notInMatch = (utils) => utils.getByTestId('lineup-not-in-match');

  // Team C's lineup for the final, chosen and edited: an unsaved change for that match.
  async function editFinalAsTeamC() {
    api.fetchSquads.mockResolvedValue({ 'team-a': SQUAD, 'team-c': SQUAD });
    api.fetchLineupInForce.mockResolvedValue(lineupFor({ teamId: 'team-c', matchId: 'm-r1-0', sourceMatchId: 'm-r0-1' }));
    api.fetchTeamLineup.mockResolvedValue(lineupFor({ teamId: 'team-c', round: 0, sourceRound: 0 }));
    const utils = await mountPage({ poolMatches: undefined, bracket: finalWith(C) });
    await act(async () => { fireEvent.change(utils.getByLabelText('Team'), { target: { value: 'team-c' } }); });
    await chooseTarget(utils, 'm-r1-0');
    await pick(utils, 2, 'mem-4');
    return utils;
  }

  async function giveTheFinalTo(utils, bracket) {
    await act(async () => { utils.rerender(pageWith(bracket)); });
    await act(async () => { await Promise.resolve(); });
  }

  it('keeps the match chosen and says plainly that the team is no longer in it', async () => {
    const utils = await editFinalAsTeamC();
    expect(targetSelect(utils).value).toBe('m-r1-0');

    await giveTheFinalTo(utils, finalWith(B));

    expect(targetSelect(utils).value, 'the chosen match is still chosen').toBe('m-r1-0');
    expect(optionTexts(targetSelect(utils)), 'and still named').toEqual(['Starting lineup', 'Match 2', 'Match 3']);
    expect(notInMatch(utils).textContent).toBe(
      'Team C is no longer in Match 3. Choose another match, or the starting lineup, in Lineup for.',
    );
    expect(utils.queryByText('Starting lineup', { selector: '.overline' }), 'the starting lineup is not shown').toBeNull();
  });

  it('keeps Save off for it, and the operator\'s edit stays on the form', async () => {
    const utils = await editFinalAsTeamC();
    expect(saveButton(utils).disabled).toBe(false);

    await giveTheFinalTo(utils, finalWith(B));
    await click(saveButton(utils));

    expect(saveButton(utils).disabled).toBe(true);
    expect(api.putMatchLineup, 'nothing is written for a match the team is not in').not.toHaveBeenCalled();
    expect(api.putTeamLineup, 'and nothing is written over the starting lineup').not.toHaveBeenCalled();
    expect(utils.getByTestId('lineup-position-2').value, 'the edit is not lost from view').toBe('mem-4');
    expect(utils.getByTestId('lineup-position-2').disabled, 'but cannot be taken further').toBe(true);
  });

  it('is the same for a match that has left the draw altogether', async () => {
    const utils = await editFinalAsTeamC();

    // The final is gone from the data, not re-seated.
    await giveTheFinalTo(utils, { rounds: [finalWith(B).rounds[0]] });

    expect(targetSelect(utils).value).toBe('m-r1-0');
    expect(notInMatch(utils).textContent).toContain('Team C is no longer in Match 3.');
    expect(saveButton(utils).disabled).toBe(true);
  });

  it('lets the operator choose another lineup, and the notice goes with the match', async () => {
    const utils = await editFinalAsTeamC();
    await giveTheFinalTo(utils, finalWith(B));
    expect(targetSelect(utils).value, 'the chosen match waits for the operator').toBe('m-r1-0');

    await chooseTarget(utils, '');
    expect(targetSelect(utils).value).toBe('');
    expect(utils.queryByTestId('lineup-not-in-match')).toBeNull();
    expect(utils.getByText('Starting lineup', { selector: '.overline' })).toBeTruthy();
    expect(optionTexts(targetSelect(utils)), 'the match that was kept is not offered again').toEqual(['Starting lineup', 'Match 2']);

    await pick(utils, 1, 'mem-4');
    await click(saveButton(utils));
    expect(api.putTeamLineup, 'the starting lineup is saved because the operator chose it').toHaveBeenCalledTimes(1);

    await chooseTarget(utils, 'm-r0-1');
    expect(utils.queryByTestId('lineup-not-in-match')).toBeNull();
    expect(utils.getByText('Match 2', { selector: '.overline' })).toBeTruthy();
  });

  it('goes when the team is seated in the match again, with the edit still there to save', async () => {
    const utils = await editFinalAsTeamC();
    await giveTheFinalTo(utils, finalWith(B));
    expect(utils.queryByTestId('lineup-not-in-match')).not.toBeNull();

    await giveTheFinalTo(utils, finalWith(C));

    expect(utils.queryByTestId('lineup-not-in-match')).toBeNull();
    expect(utils.getByTestId('lineup-position-2').value).toBe('mem-4');
    expect(saveButton(utils).disabled).toBe(false);
  });
});

