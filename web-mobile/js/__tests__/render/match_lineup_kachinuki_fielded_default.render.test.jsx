// The at-court lineup panel ("Lineup for this match") on a kachinuki match, in a
// real DOM (operator answers D2 and D3, round ship-kno1-plan-1 and -2):
//
//  - each EMPTY place shows, as the box's placeholder, the member the server fields
//    there by member number, labelled like any member ("T1.1 Ishida", "T1.3" alone
//    for a nameless member), in the amber treatment of an empty box but not greyed;
//    a place the form fills shows its member and no default;
//  - the panel asks the server (API.fetchFieldedLineup) with the form's positions,
//    saved or not, once the lineup was read and again after every pick or typed name;
//    while that read is out, or when it fails or answers nothing, the places show
//    "Add player…", and an answer older than the form's latest change is dropped;
//  - a default is never saved: a pick replaces it and the Save carries the picked
//    place alone; a name typed into a place showing a NAMELESS default names that
//    member (a rename, the place saved with that member's id, no new member);
//  - a fixed-order match asks nothing and shows "Add player…" as before;
//  - on every team match a filled box reads with its member number before the name
//    ("T1.2 Sano"), drawn from the bar's data-number, so the box's value stays the
//    name; with its list open it keeps today's look.

import React from 'react';
import { render, act, fireEvent, within } from '@testing-library/react';
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { answered } from '../helpers/team_members.js';
import { lineupPutStub } from '../helpers/lineup_server.js';

// Seeded like a fresh team (team size 3 plus two reserves): two members named, the
// rest nameless.
const NAMED = [
  { id: 'mem-1', index: 1, name: 'Ishida' },
  { id: 'mem-2', index: 2, name: 'Sano' },
  { id: 'mem-3', index: 3, name: '' },
  { id: 'mem-4', index: 4, name: '' },
  { id: 'mem-5', index: 5, name: '' },
];
const NAMELESS = NAMED.map((m) => ({ ...m, name: '' }));

const A = { id: 'team-a', name: 'Team A', number: 'T1' };
const B = { id: 'team-b', name: 'Team B', number: 'T2' };
const KACHINUKI = { id: 'comp-1', name: 'Team Event', kind: 'team', format: 'pools', status: 'active', teamSize: 3, teamMatchType: 'kachinuki', players: [A, B] };
const FIXED = { ...KACHINUKI, teamMatchType: 'fixed' };
const MATCH = { id: 'Pool A-0', compId: 'comp-1', phase: 'pool', poolName: 'Pool A', status: 'scheduled', sideA: A, sideB: B };
const NOTHING_SAVED = null;
const EMPTY_PIN = { teamId: 'team-a', matchId: 'Pool A-0', sourceMatchId: 'Pool A-0', positions: {}, memberIds: {}, saved: true };
const SANO_AT_1 = { ...EMPTY_PIN, positions: { 1: 'Sano' }, memberIds: { 1: 'mem-2' } };

// The server's fill (engine kachinukiFielded): each empty place, in fighting order,
// takes the lowest-numbered member from 1 to the team size that the form does not hold.
function fieldedBy(squad, teamSize = 3) {
  return vi.fn((_c, _t, _m, form) => {
    const held = new Set(Object.values(form.memberIds || {}));
    const answer = {};
    const free = squad.filter((m) => m.index >= 1 && m.index <= teamSize && !held.has(m.id))
      .sort((x, y) => x.index - y.index);
    for (const key of ['1', '2', '3']) {
      const filled = (form.positions?.[key] || '') !== '' || !!form.memberIds?.[key];
      if (filled) continue;
      const next = free.shift();
      if (next) answer[key] = next.id;
    }
    return Promise.resolve(answer);
  });
}

function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((res, rej) => { resolve = res; reject = rej; });
  return { promise, resolve, reject };
}

let MatchLineupSideEditor;
let saved;
let api;
let serverMatch;

beforeEach(async () => {
  sessionStorage.clear();
  saved = { API: window.API, compMatches: window.compMatches, confirmDialog: window.confirmDialog };
  window.compMatches = () => [];
  window.confirmDialog = vi.fn().mockResolvedValue(true);
  serverMatch = { positions: {}, memberIds: {} };
  api = {
    fetchLineupInForce: vi.fn().mockResolvedValue(NOTHING_SAVED),
    fetchSquads: vi.fn().mockResolvedValue({ 'team-a': NAMED }),
    fetchFieldedLineup: fieldedBy(NAMED),
    putMatchLineup: lineupPutStub(serverMatch),
    addTeamMember: vi.fn().mockImplementation((_c, _t, name) => Promise.resolve(answered({ id: 'mem-minted', index: 6 }, { name }))),
    renameTeamMember: vi.fn().mockImplementation((_c, _t, id, name) => Promise.resolve(answered({ ...NAMED.find((m) => m.id === id), id }, { name }))),
    queuedLineupSave: vi.fn().mockReturnValue(false),
  };
  window.API = api;
  // The real helpers (positions Senpo, Chuken, Taisho; the resolver that renames or mints).
  await import('../../admin_lineup.jsx');
  ({ MatchLineupSideEditor } = await import('../../admin_schedule_lineup.jsx'));
});

afterEach(() => {
  for (const [key, value] of Object.entries(saved)) {
    if (value === undefined) delete window[key]; else window[key] = value;
  }
});

async function flush() {
  for (let i = 0; i < 6; i += 1) {
    await act(async () => { await Promise.resolve(); });
  }
}

async function mountPanel(comp = KACHINUKI) {
  let utils;
  await act(async () => {
    utils = render(<MatchLineupSideEditor comp={comp} team={A} match={MATCH} allMatches={[MATCH]} password="pw" showToast={vi.fn()} />);
  });
  await flush();
  return utils;
}

const PLACE = { 1: 'Senpo', 2: 'Chuken', 3: 'Taisho' };
const box = (utils, key) => utils.getByLabelText(`${PLACE[key]} player`);
const wrapper = (utils, key) => box(utils, key).closest('.lineup-name');
const bar = (utils, key) => box(utils, key).closest('.lineup-name__bar');
const placeholders = (utils) => ['1', '2', '3'].map((k) => box(utils, k).getAttribute('placeholder'));
const isDefault = (utils, key) => wrapper(utils, key).classList.contains('lineup-name--fielded');

async function pickFromList(utils, key, text) {
  const row = utils.getByTestId(`match-lineup-pos-team-a-${key}`);
  await act(async () => { fireEvent.focus(box(utils, key)); });
  const list = row.querySelector('.lineup-name__dropdown') || row;
  await act(async () => { fireEvent.click(within(list).getAllByText(text)[0]); });
  await flush();
}

async function typeName(utils, key, name) {
  await act(async () => {
    const input = box(utils, key);
    fireEvent.focus(input);
    fireEvent.change(input, { target: { value: name } });
    fireEvent.keyDown(input, { key: 'Enter' });
  });
  await flush();
}

async function save(utils) {
  await act(async () => { fireEvent.click(utils.getByRole('button', { name: /^Save lineup$/ })); });
  await flush();
}

describe('the lineup panel on a kachinuki match shows who the server fields in each empty place', () => {
  it.each([
    ['nothing is saved (saved: false)', NOTHING_SAVED],
    ['the start pin is empty (saved: true, positions: {})', EMPTY_PIN],
  ])('when %s, every place shows its default, labelled like any member', async (_name, lineup) => {
    api.fetchLineupInForce.mockResolvedValue(lineup);
    const utils = await mountPanel();

    expect(placeholders(utils)).toEqual(['T1.1 Ishida', 'T1.2 Sano', 'T1.3']);
    for (const key of ['1', '2', '3']) {
      expect(box(utils, key).value, 'a default is never the box\'s value').toBe('');
      expect(isDefault(utils, key), 'drawn in the amber default style').toBe(true);
      expect(wrapper(utils, key).classList.contains('lineup-name--empty'), 'on the empty box\'s amber treatment').toBe(true);
    }
    expect(api.fetchFieldedLineup).toHaveBeenCalledWith('comp-1', 'team-a', 'Pool A-0', { positions: {}, memberIds: {} });
  });

  it('shows "T1.1" alone for a nameless member, with no trailing space', async () => {
    api.fetchSquads.mockResolvedValue({ 'team-a': NAMELESS });
    api.fetchFieldedLineup = fieldedBy(NAMELESS);
    const utils = await mountPanel();
    expect(placeholders(utils)).toEqual(['T1.1', 'T1.2', 'T1.3']);
  });

  it('a filled place shows its member, with its number, and no default; the others are asked with it', async () => {
    api.fetchLineupInForce.mockResolvedValue(SANO_AT_1);
    const utils = await mountPanel();

    expect(box(utils, '1').value).toBe('Sano');
    expect(bar(utils, '1').getAttribute('data-number'), 'the box reads "T1.2 Sano"').toBe('T1.2');
    expect(isDefault(utils, '1')).toBe(false);
    expect(placeholders(utils).slice(1)).toEqual(['T1.1 Ishida', 'T1.3']);
    expect(api.fetchFieldedLineup).toHaveBeenLastCalledWith('comp-1', 'team-a', 'Pool A-0', { positions: { 1: 'Sano' }, memberIds: { 1: 'mem-2' } });
  });

  it('a filled box with its list open keeps today\'s look: the name as its placeholder, no number, no default style', async () => {
    api.fetchLineupInForce.mockResolvedValue(SANO_AT_1);
    const utils = await mountPanel();
    await act(async () => { fireEvent.focus(box(utils, '1')); });

    expect(box(utils, '1').value).toBe('');
    expect(box(utils, '1').getAttribute('placeholder')).toBe('Sano');
    expect(bar(utils, '1').hasAttribute('data-number')).toBe(false);
    expect(isDefault(utils, '1')).toBe(false);
  });

  it('a pick replaces the default, the others are asked again, and the Save carries the picked place alone', async () => {
    const utils = await mountPanel();
    await pickFromList(utils, '1', 'Sano');

    expect(box(utils, '1').value).toBe('Sano');
    expect(bar(utils, '1').getAttribute('data-number')).toBe('T1.2');
    expect(placeholders(utils).slice(1), 'the fill moves on past the member picked').toEqual(['T1.1 Ishida', 'T1.3']);
    expect(api.fetchFieldedLineup).toHaveBeenLastCalledWith('comp-1', 'team-a', 'Pool A-0', { positions: { 1: 'Sano' }, memberIds: { 1: 'mem-2' } });

    await save(utils);
    expect(api.putMatchLineup).toHaveBeenCalledTimes(1);
    const [, , , positions, , memberIds, changed] = api.putMatchLineup.mock.calls[0];
    expect(positions).toEqual({ 1: 'Sano' });
    expect(memberIds).toEqual({ 1: 'mem-2' });
    expect(changed).toEqual(['1']);
  });

  it('a name typed into a place showing a NAMELESS default names that member, not the place\'s seeded slot, and mints nobody', async () => {
    api.fetchSquads.mockResolvedValue({ 'team-a': NAMELESS });
    api.fetchFieldedLineup = fieldedBy(NAMELESS);
    const utils = await mountPanel();
    // Member 3 at Senpo: the server now fields member 1 at Chuken (its seeded slot is member 2).
    await pickFromList(utils, '1', 'T1.3');
    expect(placeholders(utils).slice(1)).toEqual(['T1.1', 'T1.2']);
    // The picked nameless member reads its number alone, in ink like a name, not as a default.
    expect(box(utils, '1').value).toBe('');
    expect(box(utils, '1').getAttribute('placeholder')).toBe('T1.3');
    expect(wrapper(utils, '1').classList.contains('lineup-name--numbered')).toBe(true);
    expect(isDefault(utils, '1')).toBe(false);

    await typeName(utils, '2', 'Ishida');
    expect(box(utils, '2').value).toBe('Ishida');
    expect(bar(utils, '2').getAttribute('data-number'), 'the box reads "T1.1 Ishida"').toBe('T1.1');

    await save(utils);
    expect(api.addTeamMember, 'no new member').not.toHaveBeenCalled();
    expect(api.renameTeamMember).toHaveBeenCalledTimes(1);
    expect(api.renameTeamMember.mock.calls[0].slice(0, 4)).toEqual(['comp-1', 'team-a', 'mem-1', 'Ishida']);
    const [, , , positions, , memberIds, changed] = api.putMatchLineup.mock.calls[0];
    expect(positions).toEqual({ 1: '', 2: 'Ishida' });
    expect(memberIds).toEqual({ 1: 'mem-3', 2: 'mem-1' });
    expect(changed.slice().sort()).toEqual(['1', '2']);
  });

  it('a name a member already carries, typed over a nameless default, places that member and renames nobody', async () => {
    const utils = await mountPanel();
    expect(box(utils, '3').getAttribute('placeholder')).toBe('T1.3');
    await typeName(utils, '3', 'Sano');
    expect(bar(utils, '3').hasAttribute('data-number'), 'not read as T1.3 Sano').toBe(false);
    await save(utils);
    expect(api.renameTeamMember).not.toHaveBeenCalled();
    expect(api.addTeamMember).not.toHaveBeenCalled();
    const [, , , positions, , memberIds] = api.putMatchLineup.mock.calls[0];
    expect(positions).toEqual({ 3: 'Sano' });
    expect(memberIds).toEqual({ 3: 'mem-2' });
  });

  it('a name typed over a NAMED default is someone else: that member is not renamed', async () => {
    const utils = await mountPanel();
    await typeName(utils, '1', 'Kato');
    expect(bar(utils, '1').hasAttribute('data-number')).toBe(false);
    await save(utils);
    expect(api.renameTeamMember).not.toHaveBeenCalled();
    expect(api.addTeamMember).toHaveBeenCalledTimes(1);
    expect(api.addTeamMember.mock.calls[0][2]).toBe('Kato');
  });
});

describe('a name typed while the fielded read is out', () => {
  // The reads whose form holds member 3 at Senpo alone: the pick's own, and the Save's.
  const askedWithChukenEmpty = () => api.fetchFieldedLineup.mock.calls
    .filter(([, , , form]) => JSON.stringify(form) === JSON.stringify({ positions: { 1: '' }, memberIds: { 1: 'mem-3' } })).length;

  async function typeWhileReadOut(squad) {
    api.fetchSquads.mockResolvedValue({ 'team-a': squad });
    api.fetchFieldedLineup = fieldedBy(squad);
    const utils = await mountPanel();
    // Member 3 at Senpo: its read never lands, so Chuken shows no default when the name is typed.
    api.fetchFieldedLineup.mockReturnValueOnce(deferred().promise);
    await pickFromList(utils, '1', 'T1.3');
    expect(placeholders(utils).slice(1)).toEqual(['Add player…', 'Add player…']);
    await typeName(utils, '2', 'Ishida');
    return utils;
  }

  it('names the member the place would have shown, not the position\'s seeded slot', async () => {
    const utils = await typeWhileReadOut(NAMELESS);
    await save(utils);
    // The Save asked for the default of Chuken with that place left empty: member 1
    // (the seeded slot of Chuken is member 2). The pick's own read is the first ask.
    expect(askedWithChukenEmpty()).toBe(2);
    expect(api.addTeamMember, 'no new member').not.toHaveBeenCalled();
    expect(api.renameTeamMember).toHaveBeenCalledTimes(1);
    expect(api.renameTeamMember.mock.calls[0].slice(0, 4)).toEqual(['comp-1', 'team-a', 'mem-1', 'Ishida']);
    const [, , , positions, , memberIds] = api.putMatchLineup.mock.calls[0];
    expect(positions).toEqual({ 1: '', 2: 'Ishida' });
    expect(memberIds).toEqual({ 1: 'mem-3', 2: 'mem-1' });
  });

  it('leaves the resolver\'s own rule when the Save\'s read fails', async () => {
    const utils = await typeWhileReadOut(NAMELESS);
    api.fetchFieldedLineup.mockRejectedValue(new Error('offline'));
    await save(utils);
    expect(api.renameTeamMember.mock.calls[0].slice(0, 4)).toEqual(['comp-1', 'team-a', 'mem-2', 'Ishida']);
  });

  it('does not wait on a read that is not answered: after two seconds the Save goes on with the resolver\'s own rule', async () => {
    const utils = await typeWhileReadOut(NAMELESS);
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      api.fetchFieldedLineup.mockReturnValue(deferred().promise);
      await act(async () => { fireEvent.click(utils.getByRole('button', { name: /^Save lineup$/ })); });
      await flush();
      expect(api.putMatchLineup, 'still waiting on the read').not.toHaveBeenCalled();
      await act(async () => { await vi.advanceTimersByTimeAsync(2000); });
      await flush();
    } finally {
      vi.useRealTimers();
    }
    expect(api.renameTeamMember.mock.calls[0].slice(0, 4)).toEqual(['comp-1', 'team-a', 'mem-2', 'Ishida']);
    expect(api.putMatchLineup).toHaveBeenCalledTimes(1);
  });

  it('asks nothing at the Save while the browser is offline', async () => {
    const utils = await typeWhileReadOut(NAMELESS);
    const online = vi.spyOn(navigator, 'onLine', 'get').mockReturnValue(false);
    try {
      await save(utils);
    } finally {
      online.mockRestore();
    }
    expect(askedWithChukenEmpty(), 'only the pick\'s own read').toBe(1);
    expect(api.renameTeamMember.mock.calls[0].slice(0, 4)).toEqual(['comp-1', 'team-a', 'mem-2', 'Ishida']);
  });

  it('does not ask for a name a member already carries', async () => {
    api.fetchSquads.mockResolvedValue({ 'team-a': NAMED });
    const utils = await mountPanel();
    api.fetchFieldedLineup.mockReturnValueOnce(deferred().promise);
    await pickFromList(utils, '1', 'T1.3');
    await typeName(utils, '2', 'Ishida');
    await save(utils);
    expect(askedWithChukenEmpty(), 'no read of its own at the Save').toBe(1);
  });
});

describe('the panel asks again after every change (D3)', () => {
  it('shows "Add player…" while the read is out, and drops an answer older than the latest change', async () => {
    const utils = await mountPanel();
    expect(placeholders(utils)).toEqual(['T1.1 Ishida', 'T1.2 Sano', 'T1.3']);

    const afterFirst = deferred();
    const afterSecond = deferred();
    api.fetchFieldedLineup.mockReturnValueOnce(afterFirst.promise).mockReturnValueOnce(afterSecond.promise);

    await pickFromList(utils, '1', 'Sano');
    expect(placeholders(utils).slice(1), 'nothing is shown while the read is out').toEqual(['Add player…', 'Add player…']);
    expect(isDefault(utils, '2')).toBe(false);

    await pickFromList(utils, '2', 'Ishida');
    await act(async () => { afterSecond.resolve({ 3: 'mem-3' }); });
    await flush();
    expect(box(utils, '3').getAttribute('placeholder')).toBe('T1.3');

    // The answer for the form before the second pick arrives last: it is not shown.
    await act(async () => { afterFirst.resolve({ 2: 'mem-1', 3: 'mem-3' }); });
    await flush();
    expect(box(utils, '2').value).toBe('Ishida');
    expect(box(utils, '3').getAttribute('placeholder')).toBe('T1.3');
    expect(isDefault(utils, '2')).toBe(false);
  });

  it.each([
    ['fails', () => Promise.reject(new Error('offline'))],
    ['answers nothing', () => Promise.resolve({})],
  ])('shows "Add player…" when the read %s', async (_name, answer) => {
    api.fetchFieldedLineup = vi.fn(answer);
    const utils = await mountPanel();
    expect(placeholders(utils)).toEqual(['Add player…', 'Add player…', 'Add player…']);
    expect(isDefault(utils, '1')).toBe(false);
  });
});

describe('a failed read is asked again', () => {
  afterEach(() => { vi.useRealTimers(); });

  it('shows the defaults once a later try answers', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    api.fetchFieldedLineup = vi.fn()
      .mockRejectedValueOnce(new Error('offline'))
      .mockResolvedValueOnce(null)
      .mockImplementation(fieldedBy(NAMED));
    const utils = await mountPanel();
    expect(placeholders(utils)).toEqual(['Add player…', 'Add player…', 'Add player…']);
    await act(async () => { await vi.advanceTimersByTimeAsync(5000); });
    await act(async () => { await vi.advanceTimersByTimeAsync(5000); });
    await flush();
    expect(placeholders(utils)).toEqual(['T1.1 Ishida', 'T1.2 Sano', 'T1.3']);
    expect(api.fetchFieldedLineup).toHaveBeenCalledTimes(3);
  });

  it('gives up after three retries', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    api.fetchFieldedLineup = vi.fn(() => Promise.reject(new Error('offline')));
    await mountPanel();
    for (let i = 0; i < 5; i += 1) await act(async () => { await vi.advanceTimersByTimeAsync(5000); });
    expect(api.fetchFieldedLineup).toHaveBeenCalledTimes(4);
  });
});

describe('a fixed-order match', () => {
  it('asks nothing and shows "Add player…" in an empty place', async () => {
    const utils = await mountPanel(FIXED);
    expect(api.fetchFieldedLineup).not.toHaveBeenCalled();
    expect(placeholders(utils)).toEqual(['Add player…', 'Add player…', 'Add player…']);
  });

  it('still reads a filled box with its member number', async () => {
    api.fetchLineupInForce.mockResolvedValue(SANO_AT_1);
    const utils = await mountPanel(FIXED);
    expect(box(utils, '1').value).toBe('Sano');
    expect(bar(utils, '1').getAttribute('data-number')).toBe('T1.2');
  });
});

describe('API.fetchFieldedLineup', () => {
  let realFetch;
  beforeEach(() => { realFetch = global.fetch; });
  afterEach(() => { global.fetch = realFetch; });

  const call = async () => {
    const { teamsApi } = await import('../../api_client_teams.jsx');
    return teamsApi.fetchFieldedLineup('comp 1', 'team-a', 'Pool A-0', { positions: { 1: 'Sano' }, memberIds: { 1: 'mem-2' } });
  };

  it('POSTs the form, writes nothing else, and answers the map', async () => {
    global.fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify({ fieldedMemberIds: { 2: 'mem-1' } }), { status: 200 }));
    expect(await call()).toEqual({ 2: 'mem-1' });
    const [url, opts] = global.fetch.mock.calls[0];
    expect(url).toBe('/api/competitions/comp%201/teams/team-a/fielded-lineup/Pool%20A-0');
    expect(opts.method).toBe('POST');
    expect(JSON.parse(opts.body)).toEqual({ positions: { 1: 'Sano' }, memberIds: { 1: 'mem-2' } });
  });

  it.each([
    ['a refusal', () => Promise.resolve(new Response(JSON.stringify({ error: 'nope' }), { status: 500 }))],
    ['an answer without the map', () => Promise.resolve(new Response('{}', { status: 200 }))],
    ['no connection', () => Promise.reject(new TypeError('Failed to fetch'))],
  ])('answers null on %s', async (_name, answer) => {
    global.fetch = vi.fn(answer);
    expect(await call()).toBeNull();
  });
});
