// useLineupForm (lineup_draft.jsx) is the state machine both lineup editors run:
// what was read, where it was saved, what is shown, the confirmed save and giving
// a match's own lineup up. These tests drive the hook alone, with the real React
// swapped in for this suite's stub (the hook reads the React global at call
// time), and window.API / window.confirmDialog stubbed the way the editors reach
// them.
//
// The rule they pin most: nothing is saved before the lineup has been READ. An
// editor that could not read a lineup shows an empty one, and a Save over it
// would write that empty lineup over the real one, and over every match that
// carries it.

import RealReact from 'react';
import { renderHook, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { useLineupForm, lineupDraftKey } from '../lineup_draft.jsx';
import { REMOVED_UNREAD_NOTICE, LINEUP_READ_NO_ANSWER } from '../lineup_resolver.jsx';
import { FETCH_TIMEOUT_MS } from '../write_result.jsx';
import { API as realApi } from '../api_client.jsx';
import { answered, namedLater } from './helpers/team_members.js';

const stubReact = global.React;
const KEYS = ['1', '2', '3'];
const NAMES = { positions: { 1: 'Aoki', 2: 'Sato', 3: 'Ito' }, memberIds: { 1: 'mem-1', 2: 'mem-2', 3: 'mem-3' } };
// What API.queuedLineupSave answers while a save of the lineup is queued.
const QUEUED = true;
// The team's members as the API answers them, keyed by the team's id.
const MEMBERS = [
  { id: 'mem-1', index: 1, name: 'Aoki' },
  { id: 'mem-2', index: 2, name: 'Sato' },
  { id: 'mem-3', index: 3, name: 'Ito' },
];
const OWN = { ...NAMES, sourceMatchId: 'm1', saved: true };
const CARRIED = { ...NAMES, sourceMatchId: 'm0', saved: true };
const STARTING = { ...NAMES, sourceRound: 0, saved: true };
const DRAFT_KEY = `bc.lineupDraft.v1:${lineupDraftKey({ compId: 'c', teamId: 't', matchId: 'm1' })}`;

let api;
let saved;

beforeEach(() => {
  global.React = RealReact;
  sessionStorage.clear();
  saved = {
    API: window.API, confirmDialog: window.confirmDialog,
    subscribeSyncStatus: window.subscribeSyncStatus, subscribeUnsentWrites: window.subscribeUnsentWrites,
  };
  api = {
    fetchTeamLineup: vi.fn().mockResolvedValue(STARTING),
    fetchLineupInForce: vi.fn().mockResolvedValue(CARRIED),
    deleteMatchLineup: vi.fn().mockResolvedValue(true),
    queuedLineupSave: vi.fn().mockReturnValue(false),
    fetchSquads: vi.fn().mockResolvedValue({ t: MEMBERS }),
  };
  window.API = api;
  window.confirmDialog = vi.fn().mockResolvedValue(true);
  delete window.subscribeSyncStatus;
  delete window.subscribeUnsentWrites;
});

afterEach(() => {
  global.React = stubReact;
  for (const [key, value] of Object.entries(saved)) {
    if (value === undefined) delete window[key]; else window[key] = value;
  }
});

const props = (extra = {}) => ({
  compId: 'c', teamId: 't', matchId: 'm1', positionKeys: KEYS, password: 'pw',
  matchLabel: 'Pool A · Match 2', teamName: 'Team A', ...extra,
});

// Mounts the hook and lets its first read settle.
async function mount(extra) {
  const view = renderHook((p) => useLineupForm(p), { initialProps: props(extra) });
  await act(async () => { await Promise.resolve(); });
  return view;
}

const edit = (view, positions = { 1: 'Mori' }) => act(() => {
  view.result.current.setValues((v) => ({ ...v, ...positions }));
});

function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((res, rej) => { resolve = res; reject = rej; });
  return { promise, resolve, reject };
}

describe('reading', () => {
  it('reads a match through the lineup in force, and the starting lineup exactly, as round 0', async () => {
    await mount();
    expect(api.fetchLineupInForce).toHaveBeenCalledWith('c', 't', 'm1');
    expect(api.fetchTeamLineup).not.toHaveBeenCalled();

    api.fetchLineupInForce.mockClear();
    await mount({ matchId: '' });
    expect(api.fetchTeamLineup).toHaveBeenCalledWith('c', 't', 0);
    expect(api.fetchLineupInForce).not.toHaveBeenCalled();
  });

  it('shows what was read, and where it was saved', async () => {
    const view = await mount();
    expect(view.result.current.values).toEqual(NAMES.positions);
    expect(view.result.current.memberIds).toEqual(NAMES.memberIds);
    expect(view.result.current.source).toEqual({ matchId: 'm0' });
    expect(view.result.current.loading).toBe(false);
    expect(view.result.current.read).toBe(true);
  });

  it('shows an empty form for nothing in force, and says nothing is saved', async () => {
    api.fetchLineupInForce.mockResolvedValue(null);
    const view = await mount();
    expect(view.result.current.values).toEqual({ 1: '', 2: '', 3: '' });
    expect(view.result.current.source).toBeNull();
    expect(view.result.current.read).toBe(true);
  });

  it('is unread, and cannot be saved, until the lineup has been read', async () => {
    let answer;
    api.fetchLineupInForce.mockReturnValue(new Promise((resolve) => { answer = resolve; }));
    const view = renderHook((p) => useLineupForm(p), { initialProps: props() });
    expect(view.result.current.loading).toBe(true);
    expect(view.result.current.read).toBe(false);
    act(() => view.result.current.setValues({ 1: 'Mori', 2: '', 3: '' }));
    expect(view.result.current.canSave).toBe(false);

    await act(async () => { answer(CARRIED); });
    expect(view.result.current.read).toBe(true);
    expect(view.result.current.canSave).toBe(false);
  });
});

describe('a lineup that could not be read', () => {
  beforeEach(() => { api.fetchLineupInForce.mockRejectedValue(new Error('competition not found')); });

  it('says why, holds nothing read, and can never be saved, whatever is typed', async () => {
    const view = await mount();
    expect(view.result.current.loadError).toBe('competition not found');
    expect(view.result.current.read).toBe(false);
    expect(view.result.current.loading).toBe(false);

    edit(view);
    expect(view.result.current.dirty).toBe(true);
    expect(view.result.current.canSave).toBe(false);
    expect(view.result.current.saveTitle).toBeUndefined();
  });

  it('falls back to a plain sentence when the failure carries none', async () => {
    api.fetchLineupInForce.mockRejectedValue(new Error(''));
    const view = await mount();
    expect(view.result.current.loadError).toBe('Failed to load lineup');
  });

  it.each([
    ['a match lineup', { matchId: 'm1' }, () => api.fetchLineupInForce],
    ['a starting lineup', { matchId: '' }, () => api.fetchTeamLineup],
  ])('says a read the server never answered in a plain sentence, never the browser\'s own text (%s)', async (_what, extra, read) => {
    read().mockRejectedValue(new TypeError('Failed to fetch'));
    const view = await mount(extra);
    expect(view.result.current.loadError).toBe(LINEUP_READ_NO_ANSWER);
    expect(view.result.current.loadError).not.toMatch(/Failed to fetch/);
    expect(view.result.current.read).toBe(false);
  });

  it('says a read given up on at its deadline the same way', async () => {
    api.fetchLineupInForce.mockRejectedValue(Object.assign(new Error('the request was not answered in time'), { timedOut: true }));
    const view = await mount();
    expect(view.result.current.loadError).toBe(LINEUP_READ_NO_ANSWER);
  });

  it('through the real client: a connection that is down reads as that sentence, an answer in the server\'s own words', async () => {
    const originalFetch = global.fetch;
    window.API = realApi;
    try {
      global.fetch = vi.fn(() => Promise.reject(new TypeError('Failed to fetch')));
      const view = await mount();
      expect(view.result.current.loadError).toBe(LINEUP_READ_NO_ANSWER);

      global.fetch = vi.fn(() => Promise.resolve({
        ok: false, status: 404, json: () => Promise.resolve({ error: 'competition not found' }),
      }));
      await act(async () => { view.result.current.retry(); });
      expect(view.result.current.loadError).toBe('competition not found');
    } finally {
      global.fetch = originalFetch;
    }
  });

  it('keeps its problem when the editor clears its own errors, until a read succeeds', async () => {
    const view = await mount();
    act(() => view.result.current.setError(''));
    act(() => view.result.current.setWarning(''));
    expect(view.result.current.loadError).toBe('competition not found');
    expect(view.result.current.canSave).toBe(false);
  });

  it('reads again on retry, and then the form can be saved', async () => {
    const view = await mount();
    api.fetchLineupInForce.mockResolvedValue(CARRIED);

    await act(async () => { view.result.current.retry(); });

    expect(api.fetchLineupInForce).toHaveBeenCalledTimes(2);
    expect(view.result.current.loadError).toBe('');
    expect(view.result.current.read).toBe(true);
    expect(view.result.current.values).toEqual(NAMES.positions);
    edit(view);
    expect(view.result.current.canSave).toBe(true);
  });

  it('keeps the problem when the retry fails too', async () => {
    const view = await mount();
    api.fetchLineupInForce.mockRejectedValue(new Error('still down'));
    await act(async () => { view.result.current.retry(); });
    expect(view.result.current.loadError).toBe('still down');
    expect(view.result.current.read).toBe(false);
    expect(view.result.current.loading).toBe(false);
  });

  it('leaves the unsaved draft alone', async () => {
    sessionStorage.setItem(DRAFT_KEY, JSON.stringify({ savedAt: Date.now(), baseline: NAMES, current: { ...NAMES, positions: { ...NAMES.positions, 1: 'Mori' } } }));
    const kept = sessionStorage.getItem(DRAFT_KEY);
    const view = await mount();
    expect(sessionStorage.getItem(DRAFT_KEY)).toBe(kept);
    expect(view.result.current.draft).toMatchObject({ restored: false, stale: null });
  });
});

describe('what may be saved', () => {
  it('is a change to the lineup that was read', async () => {
    const view = await mount();
    expect(view.result.current.dirty).toBe(false);
    expect(view.result.current.canSave).toBe(false);
    expect(view.result.current.saveTitle).toBe('No changes to save');

    edit(view);
    expect(view.result.current.canSave).toBe(true);
    expect(view.result.current.saveTitle).toBeUndefined();

    edit(view, { 1: 'Aoki' });
    expect(view.result.current.canSave).toBe(false);
  });

  it('is the same for the starting lineup: it is no longer always saveable', async () => {
    const view = await mount({ matchId: '' });
    expect(view.result.current.canSave).toBe(false);
    edit(view);
    expect(view.result.current.canSave).toBe(true);
  });

  it('confirmSaved makes what the server answered the baseline, and a match\'s lineup its own', async () => {
    const view = await mount();
    edit(view);
    act(() => view.result.current.confirmSaved({
      positions: { 1: 'Mori', 2: 'Sato', 3: 'Ito' }, memberIds: { 1: 'mem-4', 2: 'mem-2', 3: 'mem-3' },
    }));
    expect(view.result.current.canSave).toBe(false);
    expect(view.result.current.values[1]).toBe('Mori');
    expect(view.result.current.memberIds[1]).toBe('mem-4');
    expect(view.result.current.source).toEqual({ matchId: 'm1' });
  });

  // A save names only the positions the operator changed, and the server answers the
  // whole lineup it holds then: a position the operator left alone that another
  // device changed is shown after the save, with the id the server holds, not the
  // one this form had.
  it('confirmSaved shows the whole lineup the server answered: a position left alone that another device changed shows, with its own id', async () => {
    const view = await mount();
    edit(view, { 1: 'Mori' });
    await act(async () => { await view.result.current.lineupToSave(); });
    act(() => view.result.current.confirmSaved({
      positions: { 1: 'Mori', 2: 'Kato', 3: 'Ito' }, memberIds: { 1: 'mem-1', 2: 'mem-5', 3: 'mem-3' },
    }));
    expect(view.result.current.values).toEqual({ 1: 'Mori', 2: 'Kato', 3: 'Ito' });
    expect(view.result.current.memberIds).toEqual({ 1: 'mem-1', 2: 'mem-5', 3: 'mem-3' });
    expect(view.result.current.baseline).toEqual({
      positions: { 1: 'Mori', 2: 'Kato', 3: 'Ito' }, memberIds: { 1: 'mem-1', 2: 'mem-5', 3: 'mem-3' },
    });
    expect(view.result.current.dirty).toBe(false);
  });

  it('confirmSaved leaves the source of a starting lineup alone, and takes a missing answer as blank', async () => {
    const view = await mount({ matchId: '' });
    edit(view);
    act(() => view.result.current.confirmSaved({ positions: undefined, memberIds: undefined }));
    expect(view.result.current.source).toEqual({ round: 0 });
    expect(view.result.current.values).toEqual({ 1: '', 2: '', 3: '' });
  });
});

describe('giving a match\'s own lineup up', () => {
  const drop = (view) => act(async () => { await view.result.current.dropOwnLineup(); });

  it('asks first, in the words of previousLineupConfirm, and removes nothing when declined', async () => {
    window.confirmDialog.mockResolvedValue(false);
    const view = await mount();
    await drop(view);
    expect(window.confirmDialog).toHaveBeenCalledTimes(1);
    expect(window.confirmDialog.mock.calls[0][0].message).toContain('Pool A · Match 2');
    expect(window.confirmDialog.mock.calls[0][0].message).toContain('Team A');
    expect(api.deleteMatchLineup).not.toHaveBeenCalled();
  });

  it('removes it, then shows the lineup the match now carries, and drops the draft made against the old one', async () => {
    api.fetchLineupInForce.mockResolvedValueOnce(OWN).mockResolvedValue(CARRIED);
    const view = await mount();
    edit(view);
    expect(sessionStorage.getItem(DRAFT_KEY)).not.toBeNull();

    await drop(view);

    expect(api.deleteMatchLineup).toHaveBeenCalledWith('c', 't', 'm1', 'pw');
    expect(view.result.current.source).toEqual({ matchId: 'm0' });
    expect(view.result.current.values).toEqual(NAMES.positions);
    expect(view.result.current.read).toBe(true);
    expect(view.result.current.removing).toBe(false);
    expect(sessionStorage.getItem(DRAFT_KEY)).toBeNull();
  });

  it('changes nothing when the server refuses the removal, and says so', async () => {
    api.fetchLineupInForce.mockResolvedValue(OWN);
    api.deleteMatchLineup.mockRejectedValue(new Error('Failed to delete match lineup'));
    const view = await mount();
    edit(view);

    await drop(view);

    expect(view.result.current.error).toBe('Failed to delete match lineup');
    expect(view.result.current.source).toEqual({ matchId: 'm1' });
    expect(view.result.current.values[1]).toBe('Mori');
    expect(view.result.current.read).toBe(true);
    expect(view.result.current.loadError).toBe('');
    expect(api.fetchLineupInForce).toHaveBeenCalledTimes(1);
  });

  it('says the removal was not done in a plain sentence of its own when the refusal carries none', async () => {
    api.fetchLineupInForce.mockResolvedValue(OWN);
    api.deleteMatchLineup.mockRejectedValue(new Error(''));
    const view = await mount();
    await drop(view);
    expect(view.result.current.error).toBe("Failed to use the previous match's lineup");
  });

  it('after a removal whose re-read fails, leaves an empty form that cannot be saved, never the removed lineup', async () => {
    api.fetchLineupInForce.mockResolvedValueOnce(OWN).mockRejectedValue(new Error('network down'));
    const view = await mount();
    edit(view);

    await drop(view);

    const form = view.result.current;
    expect(form.loadError).toBe(REMOVED_UNREAD_NOTICE);
    expect(form.read).toBe(false);
    expect(form.source).toBeNull();
    expect(form.values).toEqual({ 1: '', 2: '', 3: '' });
    expect(form.memberIds).toEqual({});
    expect(form.canSave).toBe(false);
    expect(sessionStorage.getItem(DRAFT_KEY)).toBeNull();
  });

  it('reads the lineup the match carries when it is told to try again', async () => {
    api.fetchLineupInForce.mockResolvedValueOnce(OWN).mockRejectedValueOnce(new Error('network down')).mockResolvedValue(CARRIED);
    const view = await mount();
    await drop(view);
    expect(view.result.current.read).toBe(false);

    await act(async () => { view.result.current.retry(); });

    expect(view.result.current.loadError).toBe('');
    expect(view.result.current.read).toBe(true);
    expect(view.result.current.source).toEqual({ matchId: 'm0' });
    expect(view.result.current.values).toEqual(NAMES.positions);
  });

  it('clears the editor\'s own error and warning when it starts', async () => {
    api.fetchLineupInForce.mockResolvedValue(OWN);
    const view = await mount();
    act(() => { view.result.current.setError('old'); view.result.current.setWarning('old warning'); });
    await drop(view);
    expect(view.result.current.error).toBe('');
    expect(view.result.current.warning).toBe('');
  });
});

describe('a save of the lineup that is still queued', () => {
  it('asks the API about this match\'s lineup, and about round 0 for the starting lineup', async () => {
    await mount();
    expect(api.queuedLineupSave).toHaveBeenCalledWith('c', 't', { matchId: 'm1' });
    api.queuedLineupSave.mockClear();
    await mount({ matchId: '' });
    expect(api.queuedLineupSave).toHaveBeenCalledWith('c', 't', { round: 0 });
  });

  it('is false when the API has no such question (a stub), and nothing is queued', async () => {
    delete api.queuedLineupSave;
    const view = await mount();
    expect(view.result.current.saveQueued).toBe(false);
  });

  it('is true when the API says a save is queued, and false when it says none is', async () => {
    api.queuedLineupSave.mockReturnValue(QUEUED);
    expect((await mount()).result.current.saveQueued).toBe(true);

    api.queuedLineupSave.mockReturnValue(false);
    expect((await mount()).result.current.saveQueued).toBe(false);
  });

  it('keeps the removal from being asked for: nothing is confirmed, nothing removed', async () => {
    api.fetchLineupInForce.mockResolvedValue(OWN);
    api.queuedLineupSave.mockReturnValue(QUEUED);
    const view = await mount();
    expect(view.result.current.saveQueued).toBe(true);

    await act(async () => { await view.result.current.dropOwnLineup(); });

    expect(window.confirmDialog).not.toHaveBeenCalled();
    expect(api.deleteMatchLineup).not.toHaveBeenCalled();
  });

  it('follows the queue: it is false again once the save has gone out', async () => {
    const listeners = new Set();
    window.subscribeUnsentWrites = (fn) => { listeners.add(fn); fn(); return () => listeners.delete(fn); };
    api.queuedLineupSave.mockReturnValue(QUEUED);
    const view = await mount();
    expect(view.result.current.saveQueued).toBe(true);

    api.queuedLineupSave.mockReturnValue(false);
    act(() => listeners.forEach((fn) => fn()));
    expect(view.result.current.saveQueued).toBe(false);

    api.queuedLineupSave.mockReturnValue(QUEUED);
    act(() => listeners.forEach((fn) => fn()));
    expect(view.result.current.saveQueued).toBe(true);

    view.unmount();
    expect(listeners.size).toBe(0);
  });
});

// Another device saved a lineup of the competition. The editors follow it, as the
// team sheet does, but never over what the operator has done to the form.
describe('a lineup change announced for the competition', () => {
  const CHANGED = {
    positions: { 1: 'Kato', 2: 'Sato', 3: 'Ito' }, memberIds: { 1: 'mem-5', 2: 'mem-2', 3: 'mem-3' },
    sourceMatchId: 'm1', saved: true,
  };
  const announce = (detail = { competitionId: 'c' }) => act(async () => {
    window.dispatchEvent(new CustomEvent('lineup-updated', detail === undefined ? {} : { detail }));
  });
  const reads = () => api.fetchLineupInForce.mock.calls.length;
  const settle = () => act(async () => { await new Promise((resolve) => setTimeout(resolve, 0)); });

  it('is read again and shown while the form is untouched, with no loading screen in between', async () => {
    const loadings = [];
    const view = renderHook((p) => {
      const form = useLineupForm(p);
      loadings.push(form.loading);
      return form;
    }, { initialProps: props() });
    await act(async () => { await Promise.resolve(); });
    expect(view.result.current.values[1]).toBe('Aoki');
    loadings.length = 0;

    api.fetchLineupInForce.mockResolvedValue(CHANGED);
    await announce();

    expect(reads()).toBe(2);
    expect(view.result.current.values[1]).toBe('Kato');
    expect(view.result.current.memberIds[1]).toBe('mem-5');
    expect(view.result.current.baseline.positions[1]).toBe('Kato');
    expect(view.result.current.source).toEqual({ matchId: 'm1' });
    expect(view.result.current.dirty).toBe(false);
    expect(loadings.length).toBeGreaterThan(0);
    expect(loadings.every((l) => l === false), 'the form stays on screen').toBe(true);
  });

  it('follows the starting lineup the same way', async () => {
    const view = await mount({ matchId: '' });
    api.fetchTeamLineup.mockResolvedValue({ ...STARTING, positions: { ...NAMES.positions, 1: 'Kato' } });

    await announce();

    expect(api.fetchTeamLineup).toHaveBeenCalledTimes(2);
    expect(view.result.current.values[1]).toBe('Kato');
  });

  it('follows an announcement that names no competition', async () => {
    await mount();
    await announce(undefined);
    expect(reads()).toBe(2);
  });

  it('leaves a form the operator has edited alone: nothing is read, and the edit stands', async () => {
    const view = await mount();
    edit(view);
    api.fetchLineupInForce.mockResolvedValue(CHANGED);

    await announce();

    expect(reads()).toBe(1);
    expect(view.result.current.values[1]).toBe('Mori');
    expect(view.result.current.dirty).toBe(true);
  });

  it('does not read over an edit made while the read was out', async () => {
    const view = await mount();
    const late = deferred();
    api.fetchLineupInForce.mockReturnValue(late.promise);
    await announce();
    expect(reads()).toBe(2);

    edit(view);
    await act(async () => { late.resolve(CHANGED); });

    expect(view.result.current.values[1]).toBe('Mori');
    expect(view.result.current.baseline.positions[1]).toBe('Aoki');
    expect(view.result.current.dirty).toBe(true);
  });

  it('does not put the lineup from before a save back over the saved one', async () => {
    const view = await mount();
    const late = deferred();
    api.fetchLineupInForce.mockReturnValue(late.promise);
    await announce();
    edit(view);
    act(() => view.result.current.confirmSaved({
      positions: { 1: 'Mori', 2: 'Sato', 3: 'Ito' }, memberIds: { 1: 'mem-4', 2: 'mem-2', 3: 'mem-3' },
    }));

    await act(async () => { late.resolve(CARRIED); });

    expect(view.result.current.values[1]).toBe('Mori');
    expect(view.result.current.baseline.positions[1]).toBe('Mori');
    expect(view.result.current.dirty).toBe(false);
  });

  it('is not read while a removal is out, and the removal\'s own read is what ends up shown', async () => {
    api.fetchLineupInForce.mockResolvedValueOnce(OWN).mockResolvedValue(CARRIED);
    const view = await mount();
    const gate = deferred();
    api.deleteMatchLineup.mockReturnValue(gate.promise);
    let removal;
    await act(async () => {
      removal = view.result.current.dropOwnLineup();
      await new Promise((resolve) => setTimeout(resolve, 0));
    });
    expect(view.result.current.removing).toBe(true);
    const before = reads();

    await announce();
    expect(reads(), 'nothing is read while the removal is out').toBe(before);

    await act(async () => { gate.resolve(true); await removal; });
    expect(view.result.current.removing).toBe(false);
    expect(view.result.current.source).toEqual({ matchId: 'm0' });
  });

  it('does not put the lineup that a removal took away back, when a read from before it answers', async () => {
    api.fetchLineupInForce.mockResolvedValueOnce(OWN);
    const view = await mount();
    const late = deferred();
    api.fetchLineupInForce.mockReturnValueOnce(late.promise).mockResolvedValue(CARRIED);
    await announce();
    expect(reads(), 'the announcement started a read').toBe(2);

    await act(async () => { await view.result.current.dropOwnLineup(); });
    expect(view.result.current.source).toEqual({ matchId: 'm0' });
    await act(async () => { late.resolve(OWN); });

    expect(view.result.current.source, 'the lineup the match carries now').toEqual({ matchId: 'm0' });
  });

  it('keeps what is shown, and raises no problem, when the read that follows fails', async () => {
    const view = await mount();
    api.fetchLineupInForce.mockRejectedValue(new Error('offline'));

    await announce();

    expect(reads()).toBe(2);
    expect(view.result.current.values[1]).toBe('Aoki');
    expect(view.result.current.loadError).toBe('');
    expect(view.result.current.read).toBe(true);
  });

  it('reads a form that could not be read again, and keeps its problem when that fails too', async () => {
    api.fetchLineupInForce.mockRejectedValue(new Error('competition not found'));
    const view = await mount();
    expect(view.result.current.read).toBe(false);

    await announce();
    expect(reads()).toBe(2);
    expect(view.result.current.loadError).toBe('competition not found');
    expect(view.result.current.loading).toBe(false);

    api.fetchLineupInForce.mockResolvedValue(CARRIED);
    await announce();
    expect(view.result.current.read).toBe(true);
    expect(view.result.current.loadError).toBe('');
    expect(view.result.current.values[1]).toBe('Aoki');
  });

  it('ignores a change announced for another competition', async () => {
    await mount();
    await announce({ competitionId: 'another' });
    expect(reads()).toBe(1);
  });

  it('ignores a change announced for another team of the competition: neither its lineup nor its members are read', async () => {
    await mount();
    await announce({ competitionId: 'c', teamId: 'u' });
    expect(reads()).toBe(1);
    expect(api.fetchSquads).toHaveBeenCalledTimes(1);
  });

  it('follows a change announced for this team, and one that names no team', async () => {
    await mount();
    await announce({ competitionId: 'c', teamId: 't' });
    expect(reads(), 'this team\'s').toBe(2);
    await announce({ competitionId: 'c', matchId: 'm1' });
    expect(reads(), 'one that names no team is read as every team\'s').toBe(3);
  });

  // A read that replaced the first one could fail where the first would have shown the
  // lineup, and the operator would be told it could not be read. So a change announced
  // while a read of the lineup is out waits for it.
  describe('announced while the lineup is being read', () => {
    // Mounts the editor with its first read answered by `first` (and every later one by
    // `later`), recording what position 1 showed, in order, once its lineup was read.
    const start = (later) => {
      const first = deferred();
      api.fetchLineupInForce.mockReturnValueOnce(first.promise);
      later();
      const shown = [];
      const view = renderHook((p) => {
        const form = useLineupForm(p);
        const name = form.read ? form.values[1] : '';
        if (shown[shown.length - 1] !== name) shown.push(name);
        return form;
      }, { initialProps: props() });
      return { first, shown, view };
    };

    it('does not replace that read: its lineup is shown, and then the change is followed', async () => {
      const { first, shown, view } = start(() => api.fetchLineupInForce.mockResolvedValue(CHANGED));
      await act(async () => { await Promise.resolve(); });

      await announce();
      expect(reads(), 'nothing is read over the first read').toBe(1);

      await act(async () => { first.resolve(CARRIED); });
      await settle();

      expect(reads(), 'followed once it was shown').toBe(2);
      expect(shown, 'the first read\'s lineup was shown before the change').toEqual(['', 'Aoki', 'Kato']);
      expect(view.result.current.baseline.positions[1]).toBe('Kato');
    });

    it('raises no problem when the follow then fails: the first read\'s lineup is still shown', async () => {
      const { first, view } = start(() => api.fetchLineupInForce.mockRejectedValue(new Error('offline')));
      await act(async () => { await Promise.resolve(); });
      await announce();

      await act(async () => { first.resolve(CARRIED); });
      await settle();

      expect(reads(), 'the follow was tried').toBe(2);
      expect(view.result.current.read).toBe(true);
      expect(view.result.current.values).toEqual(NAMES.positions);
      expect(view.result.current.loadError).toBe('');
    });

    it('is dropped when that read fails: its failure is shown, nothing else is read, and Try again reads the lineup as it is now', async () => {
      const { first, view } = start(() => api.fetchLineupInForce.mockResolvedValue(CHANGED));
      await act(async () => { await Promise.resolve(); });
      await announce();

      await act(async () => { first.reject(new Error('competition not found')); });
      await settle();

      expect(view.result.current.loadError).toBe('competition not found');
      expect(view.result.current.read).toBe(false);
      expect(reads(), 'the change was dropped, not followed').toBe(1);

      await act(async () => { view.result.current.retry(); });
      await settle();

      expect(reads()).toBe(2);
      expect(view.result.current.read).toBe(true);
      expect(view.result.current.values[1]).toBe('Kato');
    });

    it('is not followed once a later change shows the lineup: the failure of the read dropped it', async () => {
      const { first, view } = start(() => api.fetchLineupInForce.mockResolvedValue(CHANGED));
      await act(async () => { await Promise.resolve(); });
      await announce();
      await act(async () => { first.reject(new Error('competition not found')); });
      await settle();
      expect(reads()).toBe(1);

      await announce();
      await settle();

      expect(view.result.current.read).toBe(true);
      expect(reads(), 'one read for the later change, none for the dropped one').toBe(2);
    });

    it('is dropped when the editor is given another lineup meanwhile: that one is read as it is now', async () => {
      const { first, view } = start(() => api.fetchLineupInForce.mockResolvedValue(CARRIED));
      await act(async () => { await Promise.resolve(); });
      await announce();

      view.rerender(props({ matchId: 'm2' }));
      await settle();
      await act(async () => { first.resolve(CHANGED); });
      await settle();

      expect(view.result.current.read).toBe(true);
      expect(view.result.current.values[1], 'the late answer of the lineup left is not shown').toBe('Aoki');
      expect(reads(), 'the lineup left and the lineup opened, and no follow').toBe(2);
    });

    it('is held the same way while a Try again read is out', async () => {
      api.fetchLineupInForce.mockRejectedValueOnce(new Error('down'));
      const view = await mount();
      expect(view.result.current.loadError).toBe('down');
      const again = deferred();
      api.fetchLineupInForce.mockReturnValueOnce(again.promise).mockRejectedValue(new Error('offline'));
      await act(async () => { view.result.current.retry(); });

      await announce();
      expect(reads(), 'nothing is read over the Try again read').toBe(2);

      await act(async () => { again.resolve(CARRIED); });
      await settle();

      expect(reads()).toBe(3);
      expect(view.result.current.read).toBe(true);
      expect(view.result.current.loadError).toBe('');
    });

    it('ignores a change announced for another competition', async () => {
      const { first } = start(() => api.fetchLineupInForce.mockResolvedValue(CHANGED));
      await act(async () => { await Promise.resolve(); });

      await announce({ competitionId: 'another' });
      await act(async () => { first.resolve(CARRIED); });
      await settle();

      expect(reads()).toBe(1);
    });
  });

  it('stops listening when the editor closes', async () => {
    const view = await mount();
    view.unmount();
    await announce();
    await settle();
    expect(reads()).toBe(1);
  });
});

// "Not restored, the lineup changed since: Mori, Kato" names the names a draft
// could not restore. Re-entering one is not an answer to the others, so it stays
// through every edit; saving the lineup, or giving it up, answers it.
describe('the "not restored" notice', () => {
  const STALE_DRAFT = {
    savedAt: Date.now(),
    baseline: { positions: { 1: 'Old', 2: 'Sato', 3: 'Ito' }, memberIds: { 1: 'mem-9', 2: 'mem-2', 3: 'mem-3' } },
    current: { positions: { 1: 'Mori', 2: 'Kato', 3: 'Ito' }, memberIds: { 1: 'mem-4', 2: 'mem-5', 3: 'mem-3' } },
  };
  const NAMED = { names: ['Mori', 'Kato'] };

  async function mountWithStaleDraft() {
    sessionStorage.setItem(DRAFT_KEY, JSON.stringify(STALE_DRAFT));
    const view = await mount();
    expect(view.result.current.draft.stale).toEqual(NAMED);
    return view;
  }

  it('stays through the operator\'s edits, and through putting one back', async () => {
    const view = await mountWithStaleDraft();

    edit(view, { 1: 'Mori' });
    expect(view.result.current.draft.stale).toEqual(NAMED);
    edit(view, { 2: 'Kato' });
    expect(view.result.current.draft.stale).toEqual(NAMED);
    edit(view, { 1: 'Aoki', 2: 'Sato' });
    expect(view.result.current.dirty).toBe(false);
    expect(view.result.current.draft.stale).toEqual(NAMED);
  });

  it('stays when another device\'s change is followed', async () => {
    const view = await mountWithStaleDraft();
    api.fetchLineupInForce.mockResolvedValue({ ...CARRIED, positions: { ...NAMES.positions, 3: 'Oda' } });

    await act(async () => { window.dispatchEvent(new CustomEvent('lineup-updated', { detail: { competitionId: 'c' } })); });

    expect(view.result.current.values[3]).toBe('Oda');
    expect(view.result.current.draft.stale).toEqual(NAMED);
  });

  it('goes once the lineup is saved', async () => {
    const view = await mountWithStaleDraft();
    edit(view, { 1: 'Mori' });

    act(() => view.result.current.confirmSaved({
      positions: { 1: 'Mori', 2: 'Sato', 3: 'Ito' }, memberIds: { 1: 'mem-4', 2: 'mem-2', 3: 'mem-3' },
    }));

    expect(view.result.current.draft.stale).toBeNull();
  });

  it('goes once the lineup is given up for the one the match carries', async () => {
    api.fetchLineupInForce.mockResolvedValueOnce(OWN).mockResolvedValue(CARRIED);
    const view = await mountWithStaleDraft();

    await act(async () => { await view.result.current.dropOwnLineup(); });

    expect(view.result.current.draft.stale).toBeNull();
  });

  it('is not shown again when the operator leaves the lineup and comes back to it', async () => {
    const view = await mountWithStaleDraft();

    view.rerender(props({ matchId: 'm2' }));
    await act(async () => { await Promise.resolve(); });
    expect(view.result.current.draft.stale).toBeNull();

    view.rerender(props({ matchId: 'm1' }));
    await act(async () => { await Promise.resolve(); });
    expect(view.result.current.read).toBe(true);
    expect(view.result.current.draft.stale).toBeNull();
  });
});

// A Save carries the form as shown and names the positions the operator changed
// (operator decision 2026-10-07, "Only changed positions"): the server puts those on
// the lineup it holds when the save arrives, so a position another device changed
// since the form was read stays, and nothing is asked of the server for it: no lineup
// to compose on, and no members either (a member another device creates is announced,
// and the editor reads the members again for it). That is why a Save with no
// connection is built, and queued, like any other.
describe('what a Save carries', () => {
  // A name typed in, as the panel's boxes do: no member id is picked for it.
  const typeName = (view, position, name) => act(() => {
    view.result.current.setValues((v) => ({ ...v, [position]: name }));
    view.result.current.setMemberIds((ids) => ({ ...ids, [position]: '' }));
  });

  it('is the form as shown, and names the positions the operator changed', async () => {
    const view = await mount();
    edit(view, { 1: 'Mori' });

    expect(view.result.current.lineupToSave()).toEqual({
      positions: { 1: 'Mori', 2: 'Sato', 3: 'Ito' },
      memberIds: { 1: 'mem-1', 2: 'mem-2', 3: 'mem-3' },
      changed: ['1'],
    });
  });

  it('asks nothing of the server: no lineup to compose on and no members, a name typed in included', async () => {
    const view = await mount();
    edit(view, { 1: 'Mori' });
    typeName(view, 2, 'Zed');
    api.fetchLineupInForce.mockClear();
    api.fetchTeamLineup.mockClear();
    api.fetchSquads.mockClear();

    expect(view.result.current.lineupToSave().changed).toEqual(['1', '2']);

    expect(api.fetchLineupInForce).not.toHaveBeenCalled();
    expect(api.fetchTeamLineup).not.toHaveBeenCalled();
    expect(api.fetchSquads).not.toHaveBeenCalled();
  });

  it('is built the same with no connection: the server not answering changes nothing', async () => {
    const view = await mount();
    edit(view, { 1: 'Mori' });
    api.fetchLineupInForce.mockRejectedValue(new TypeError('Failed to fetch'));
    api.fetchSquads.mockRejectedValue(new TypeError('Failed to fetch'));

    expect(view.result.current.lineupToSave().changed).toEqual(['1']);
  });

  it('shows nothing of the server: the form, its baseline and its source stay as they were', async () => {
    const view = await mount();
    edit(view, { 1: 'Mori' });
    const before = view.result.current.baseline;

    view.result.current.lineupToSave();

    expect(view.result.current.baseline).toBe(before);
    expect(view.result.current.values).toEqual({ 1: 'Mori', 2: 'Sato', 3: 'Ito' });
    expect(view.result.current.source).toEqual({ matchId: 'm0' });
    expect(view.result.current.dirty).toBe(true);
  });

  it('names a position put back to what was loaded as no change at all', async () => {
    const view = await mount();
    edit(view, { 1: 'Mori' });
    edit(view, { 1: 'Aoki', 3: 'Oda' });

    expect(view.result.current.lineupToSave().changed).toEqual(['3']);
  });

  it('names a position the operator cleared, with its name and its id empty', async () => {
    const view = await mount();
    act(() => {
      view.result.current.setValues((v) => ({ ...v, 2: '' }));
      view.result.current.setMemberIds((ids) => ({ ...ids, 2: '' }));
    });

    expect(view.result.current.lineupToSave()).toEqual({
      positions: { 1: 'Aoki', 2: '', 3: 'Ito' }, memberIds: { 1: 'mem-1', 2: '', 3: 'mem-3' }, changed: ['2'],
    });
  });

  it('names a member picked before it was named: its name is empty and its id is the placement', async () => {
    const view = await mount();
    act(() => {
      view.result.current.setValues((v) => ({ ...v, 2: '' }));
      view.result.current.setMemberIds((ids) => ({ ...ids, 2: 'mem-blank' }));
    });

    const carried = view.result.current.lineupToSave();

    expect(carried.changed).toEqual(['2']);
    expect(carried.positions[2]).toBe('');
    expect(carried.memberIds[2]).toBe('mem-blank');
  });
});

// The team's members are the other half of what both lineup editors show: a
// position's member is picked from them and named by them. The hook reads them,
// and again whenever it follows another device's lineup, so a member created
// elsewhere meanwhile is in the list the followed lineup names, whenever that read
// is answered. Each member carries the stamp the server gave its last write, and of two
// copies of a member the one with the larger stamp is the one that stands, whichever
// list brought it and whichever read began first.
describe('the team\'s members', () => {
  const NEW_MEMBER = { id: 'mem-9', index: 4, name: 'Zed' };
  const FOLLOWED = {
    positions: { 1: 'Aoki', 2: 'Zed', 3: 'Ito' }, memberIds: { 1: 'mem-1', 2: 'mem-9', 3: 'mem-3' },
    sourceMatchId: 'm1', saved: true,
  };
  const announce = () => act(async () => {
    window.dispatchEvent(new CustomEvent('lineup-updated', { detail: { competitionId: 'c' } }));
  });
  // An editor's own change to the members, made once the server holds it (an add, a
  // rename, a mint): the members as the server answered the write, stamped.
  const change = (view, members) => act(async () => { view.result.current.changeMembers(members); });

  it('are read when the editor opens, for its team, with the password', async () => {
    const view = await mount();
    expect(api.fetchSquads).toHaveBeenCalledWith('c', 'pw');
    expect(view.result.current.squad).toEqual(MEMBERS);
    expect(view.result.current.squadUnavailable).toBe(false);
  });

  it('are an empty list, not a failure, when the answer holds none for the team', async () => {
    api.fetchSquads.mockResolvedValue({ other: MEMBERS });
    const view = await mount();
    expect(view.result.current.squad).toEqual([]);
    expect(view.result.current.squadUnavailable).toBe(false);
  });

  it('say they could not be read when the read fails, and the list stays empty', async () => {
    api.fetchSquads.mockRejectedValue(new Error('down'));
    const view = await mount();
    expect(view.result.current.squad).toEqual([]);
    expect(view.result.current.squadUnavailable).toBe(true);
  });

  it('do not hold the lineup back: a lineup that loads is shown whether or not the members could be read', async () => {
    api.fetchSquads.mockRejectedValue(new Error('down'));
    const view = await mount();
    expect(view.result.current.read).toBe(true);
    expect(view.result.current.values).toEqual(NAMES.positions);
  });

  it('are read again when the password changes, and a read that now succeeds clears the earlier failure', async () => {
    api.fetchSquads.mockRejectedValueOnce(new Error('401')).mockResolvedValue({ t: MEMBERS });
    const view = await mount();
    expect(view.result.current.squadUnavailable).toBe(true);

    view.rerender(props({ password: 'new' }));
    await act(async () => { await Promise.resolve(); });

    expect(api.fetchSquads).toHaveBeenLastCalledWith('c', 'new');
    expect(view.result.current.squad).toEqual(MEMBERS);
    expect(view.result.current.squadUnavailable).toBe(false);
  });

  it('are not replaced by an older copy: the read made when the editor opened, answering after a followed lineup read them again, undoes nothing the later read brought', async () => {
    const opening = deferred();
    api.fetchSquads.mockReturnValueOnce(opening.promise);
    const view = renderHook((p) => useLineupForm(p), { initialProps: props() });
    await act(async () => { await Promise.resolve(); });
    // Another device named Sato after the opening read began: the followed read holds it, stamped.
    const renamed = answered(MEMBERS[1], { name: 'Sato-san' });
    api.fetchSquads.mockResolvedValue({ t: [MEMBERS[0], renamed, MEMBERS[2], NEW_MEMBER] });
    api.fetchLineupInForce.mockResolvedValue(FOLLOWED);
    await announce();
    expect(view.result.current.squad, 'the followed read').toContainEqual(NEW_MEMBER);

    await act(async () => { opening.resolve({ t: MEMBERS }); });

    expect(view.result.current.squad, 'the member the later read brought stands').toContainEqual(NEW_MEMBER);
    expect(view.result.current.squad[1], 'and so does the name it gave Sato, which the older list predates').toEqual(renamed);
    expect(view.result.current.squadUnavailable).toBe(false);
  });

  it('do not read as unavailable because an older read failed after a newer one was shown', async () => {
    const opening = deferred();
    api.fetchSquads.mockReturnValueOnce(opening.promise);
    const view = renderHook((p) => useLineupForm(p), { initialProps: props() });
    await act(async () => { await Promise.resolve(); });
    api.fetchSquads.mockResolvedValue({ t: [...MEMBERS, NEW_MEMBER] });
    api.fetchLineupInForce.mockResolvedValue(FOLLOWED);
    await announce();

    await act(async () => { opening.reject(new Error('down')); });

    expect(view.result.current.squad).toContainEqual(NEW_MEMBER);
    expect(view.result.current.squadUnavailable).toBe(false);
  });

  it('are shown when the read made as the editor opened answers after a followed lineup was dropped for an edit: a follow that shows nothing ends nothing', async () => {
    const opening = deferred();
    api.fetchSquads.mockReturnValueOnce(opening.promise);
    const view = renderHook((p) => useLineupForm(p), { initialProps: props() });
    await act(async () => { await Promise.resolve(); });
    const lineup = deferred();
    api.fetchSquads.mockRejectedValue(new Error('offline'));
    api.fetchLineupInForce.mockReturnValue(lineup.promise);
    await announce();

    edit(view, { 1: 'Mori' });
    await act(async () => { lineup.resolve(FOLLOWED); });
    expect(view.result.current.values[1], 'the followed lineup is not shown over an edit').toBe('Mori');
    expect(view.result.current.squad, 'and the follow read no list').toEqual([]);

    await act(async () => { opening.resolve({ t: MEMBERS }); });

    expect(view.result.current.squad).toEqual(MEMBERS);
    expect(view.result.current.squadUnavailable).toBe(false);
  });

  it('are shown with a followed lineup whose read fails, which leaves the form as it was', async () => {
    const view = await mount();
    api.fetchSquads.mockResolvedValue({ t: [...MEMBERS, NEW_MEMBER] });
    api.fetchLineupInForce.mockRejectedValue(new Error('offline'));

    await announce();

    expect(view.result.current.squad).toContainEqual(NEW_MEMBER);
    expect(view.result.current.squadUnavailable).toBe(false);
    expect(view.result.current.values[1]).toBe('Aoki');
    expect(view.result.current.loadError, 'a lineup that was read raises no problem').toBe('');
  });

  it('are not shown from a read made for a team the editor has since left, whenever it answers', async () => {
    const opening = deferred();
    api.fetchSquads.mockReturnValueOnce(opening.promise);
    const view = renderHook((p) => useLineupForm(p), { initialProps: props() });
    await act(async () => { await Promise.resolve(); });
    api.fetchSquads.mockRejectedValue(new Error('offline'));
    view.rerender(props({ teamId: 'u' }));
    await act(async () => { await Promise.resolve(); });

    await act(async () => { opening.resolve({ t: MEMBERS }); });

    expect(view.result.current.squad).toEqual([]);
    expect(view.result.current.squadUnavailable, 'the read made for the team it shows failed').toBe(true);
  });

  it('are the editor\'s to change after a rename or a mint: the member the server answered joins the list shown', async () => {
    const view = await mount();
    await change(view, [NEW_MEMBER]);
    expect(view.result.current.squad).toEqual([...MEMBERS, NEW_MEMBER]);
  });

  // The editors call memberRenamed with the member the server answered once it holds a
  // rename, or a cleared name (the name ""): the one place that shows it on the members,
  // the positions and the baseline.
  describe('a member renamed on the server', () => {
    // The server's answer to the write: the member, named, with the stamp it gave it.
    const renamed = (view, id, name) => {
      const member = answered(MEMBERS.find((m) => m.id === id), { name });
      return act(async () => { view.result.current.memberRenamed(member); });
    };

    it('shows the new name on the members, the positions that hold the member and the baseline: a rename alone is no edit', async () => {
      const view = await mount();

      await renamed(view, 'mem-2', 'Sato-san');

      expect(view.result.current.squad.find((m) => m.id === 'mem-2').name).toBe('Sato-san');
      expect(view.result.current.squadRef.current.find((m) => m.id === 'mem-2').name).toBe('Sato-san');
      expect(view.result.current.values).toEqual({ 1: 'Aoki', 2: 'Sato-san', 3: 'Ito' });
      expect(view.result.current.baseline.positions[2]).toBe('Sato-san');
      expect(view.result.current.dirty).toBe(false);
      expect(sessionStorage.getItem(DRAFT_KEY), 'and leaves no draft').toBeNull();
    });

    it('shows a cleared name the same way, with the member still placed', async () => {
      const view = await mount();

      await renamed(view, 'mem-2', '');

      expect(view.result.current.squad.find((m) => m.id === 'mem-2').name).toBe('');
      expect(view.result.current.values[2]).toBe('');
      expect(view.result.current.memberIds[2], 'the member keeps its place').toBe('mem-2');
      expect(view.result.current.dirty).toBe(false);
    });

    it('reads the positions as they are now: a position the member was moved to meanwhile takes the name, the one it left keeps what the operator made of it', async () => {
      const view = await mount();
      // While the rename was out the operator put Sato at Position 1, and cleared Position 2.
      act(() => {
        view.result.current.setValues((v) => ({ ...v, 1: 'Sato', 2: '' }));
        view.result.current.setMemberIds((ids) => ({ ...ids, 1: 'mem-2', 2: '' }));
      });

      await renamed(view, 'mem-2', 'Sato-san');

      expect(view.result.current.values).toEqual({ 1: 'Sato-san', 2: '', 3: 'Ito' });
    });

    it('does not read the members again: the change is announced to every device, this one included', async () => {
      const view = await mount();
      expect(api.fetchSquads).toHaveBeenCalledTimes(1);

      await renamed(view, 'mem-2', 'Sato-san');

      expect(api.fetchSquads).toHaveBeenCalledTimes(1);
    });

    it('shows the name the copy that is kept has, when the answer is older than the member shown: the positions follow the list', async () => {
      const view = await mount();
      // Another device named Sato after this rename was made, and a read has shown it.
      const mine = answered(MEMBERS[1], { name: 'Sato-san' });
      const theirs = namedLater(mine, 'Sato-kun');
      api.fetchSquads.mockResolvedValue({ t: [MEMBERS[0], theirs, MEMBERS[2]] });
      await announce();
      expect(view.result.current.squad[1]).toEqual(theirs);

      await act(async () => { view.result.current.memberRenamed(mine); });

      expect(view.result.current.squad[1], 'the answer older than the list does not undo it').toEqual(theirs);
      expect(view.result.current.values[2], 'nor does it name the position the old way').toBe('Sato-kun');
      expect(view.result.current.baseline.positions[2]).toBe('Sato-kun');
    });

    it('is nothing for a team the editor has since left', async () => {
      const view = await mount();
      const staleRenamed = view.result.current.memberRenamed;
      api.fetchSquads.mockResolvedValue({ u: [{ id: 'oth-1', index: 1, name: 'Oda' }] });
      view.rerender(props({ teamId: 'u' }));
      await act(async () => { await Promise.resolve(); });

      act(() => { staleRenamed(answered(MEMBERS[1], { name: 'Sato-san' })); });

      expect(view.result.current.squad, 'the list of the team it shows now').toEqual([{ id: 'oth-1', index: 1, name: 'Oda' }]);
      expect(view.result.current.squadRef.current.map((m) => m.id)).toEqual(['oth-1']);
    });
  });

  // A read begun before an editor's change can answer after it, with the list from
  // before: a rename undone by it would also leave the typed new name resolving to
  // nobody, and a typed name that finds nobody is named onto a blank slot or minted
  // as a second member. The member the server answered carries the stamp of the write,
  // and of two copies the larger stamp stands, so no list can undo it and no read is
  // made after the change to put it right: the server announces the change to every
  // device, which reads then.
  describe('an editor\'s own change', () => {
    const RENAMED = () => answered(MEMBERS[1], { name: 'Sato-san' });

    it('is not read again, and flags nothing: the list is the one the change made', async () => {
      const view = await mount();

      await change(view, [NEW_MEMBER]);

      expect(api.fetchSquads, 'no read is made after the change').toHaveBeenCalledTimes(1);
      expect(view.result.current.squad).toEqual([...MEMBERS, NEW_MEMBER]);
      expect(view.result.current.squadUnavailable, 'a list that was read is not unavailable').toBe(false);
    });

    it('stands over a read begun before it, which answers after it with the list from before', async () => {
      const view = await mount();
      // A lineup another device saved starts a read of the members, still out when the rename is answered.
      const older = deferred();
      api.fetchSquads.mockReturnValueOnce(older.promise);
      api.fetchLineupInForce.mockResolvedValue(FOLLOWED);
      await announce();
      const renamed = RENAMED();

      await change(view, [renamed]);
      await act(async () => { older.resolve({ t: MEMBERS }); });

      expect(view.result.current.squad[1], 'the read begun before the rename does not bring the old name back').toEqual(renamed);
      expect(view.result.current.squadRef.current[1].name, 'nor on the ref a handler reads').toBe('Sato-san');
      expect(api.fetchSquads, 'and the change is not followed by a read of its own').toHaveBeenCalledTimes(2);
    });

    // A Save can mint a typed name before the read made as the editor opened has
    // answered: nothing shown can be undone then, and that read holds the rest of the
    // team, which a name typed next must find rather than mint again.
    it('stands over the read made as the editor opened, which answers last with the list from before it, and that read still brings the rest of the team', async () => {
      const opening = deferred();
      api.fetchSquads.mockReturnValueOnce(opening.promise);
      const view = renderHook((p) => useLineupForm(p), { initialProps: props() });
      await act(async () => { await Promise.resolve(); });
      const renamed = RENAMED();

      await change(view, [renamed]);
      expect(view.result.current.squad, 'the change alone, until a list is read').toEqual([renamed]);
      await act(async () => { opening.resolve({ t: MEMBERS }); });

      expect(view.result.current.squad).toEqual([MEMBERS[0], renamed, MEMBERS[2]]);
      expect(view.result.current.squadRef.current[1].name).toBe('Sato-san');
      expect(view.result.current.squadUnavailable).toBe(false);
    });

    // While no list is shown the editor can only have changed members it made itself (a
    // mint, then a rename of it), which the read made as the editor opened does not
    // hold, and every other read begun before a change holds an older state of them.
    it.each([
      ['the read made as the editor opened first', true],
      ['the followed lineup\'s read first', false],
    ])('stands through a mint and a rename of the minted member, %s', async (_order, openingFirst) => {
      const opening = deferred();
      const followed = deferred();
      api.fetchSquads.mockReturnValueOnce(opening.promise).mockReturnValueOnce(followed.promise);
      const view = renderHook((p) => useLineupForm(p), { initialProps: props() });
      await act(async () => { await Promise.resolve(); });
      await announce();
      expect(api.fetchSquads, 'the followed lineup reads the members').toHaveBeenCalledTimes(2);
      const minted = answered(NEW_MEMBER);
      const renamedMinted = answered(minted, { name: 'Zed-san' });

      await change(view, [minted]);
      await change(view, [renamedMinted]);
      const answers = [
        () => act(async () => { opening.resolve({ t: MEMBERS }); }),
        () => act(async () => { followed.resolve({ t: [...MEMBERS, NEW_MEMBER] }); }),
      ];
      if (!openingFirst) answers.reverse();
      await answers[0]();
      await answers[1]();

      expect(view.result.current.squad.map((m) => m.id), 'the rest of the team is shown').toEqual(['mem-1', 'mem-2', 'mem-3', 'mem-9']);
      expect(view.result.current.squad[3].name, 'under the name the rename gave').toBe('Zed-san');
      expect(view.result.current.squadRef.current[3].name, 'and on the ref a handler reads').toBe('Zed-san');
      expect(view.result.current.squadUnavailable).toBe(false);
    });

    it('is nothing for a team the editor has since left: a handler that resumes after the editor was given another team adds it to no list', async () => {
      const OTHER = [{ id: 'oth-1', index: 1, name: 'Oda' }];
      const view = await mount();
      const staleChange = view.result.current.changeMembers;
      api.fetchSquads.mockResolvedValue({ u: OTHER });
      view.rerender(props({ teamId: 'u' }));
      await act(async () => { await Promise.resolve(); });

      act(() => { staleChange([NEW_MEMBER]); });

      expect(view.result.current.squad).toEqual(OTHER);
      expect(view.result.current.squadRef.current).toEqual(OTHER);
    });

    it('is not undone by a read of the members a followed lineup made for a team the editor has since left', async () => {
      const view = await mount();
      const followed = deferred();
      api.fetchSquads.mockReturnValueOnce(followed.promise);
      await announce();
      api.fetchSquads.mockRejectedValue(new Error('offline'));
      view.rerender(props({ teamId: 'u' }));
      await act(async () => { await Promise.resolve(); });

      await act(async () => { followed.resolve({ t: [...MEMBERS, NEW_MEMBER] }); });

      expect(view.result.current.squad, 'the team it shows now has no list').toEqual([]);
    });
  });

  // No screen removes a member, so a list that arrives, from a read or from an editor's
  // own write, only ever adds to the one shown: every member of both lists, in member
  // number order, and of a member both hold the copy with the larger stamp.
  describe('a list that arrives is merged with the one shown', () => {
    const MINTED = { id: 'mem-10', index: 5, name: 'Kato' };

    it('keeps a member the editor added while a read begun before it was out: that read answers without them', async () => {
      const view = await mount();
      const members = deferred();
      api.fetchSquads.mockReturnValue(members.promise);
      await announce();

      await change(view, [NEW_MEMBER]);
      await act(async () => { members.resolve({ t: MEMBERS }); });

      expect(view.result.current.squad).toEqual([...MEMBERS, NEW_MEMBER]);
      expect(view.result.current.squadRef.current, 'and the ref a handler reads').toEqual([...MEMBERS, NEW_MEMBER]);
    });

    it('keeps a member a read brought in when the editor\'s update was built on the list from before it', async () => {
      const view = await mount();
      const before = view.result.current.squad;
      api.fetchSquads.mockResolvedValue({ t: [...MEMBERS, NEW_MEMBER] });
      api.fetchLineupInForce.mockResolvedValue(FOLLOWED);
      await announce();
      expect(view.result.current.squad).toContainEqual(NEW_MEMBER);

      await change(view, [...before, MINTED]);

      expect(view.result.current.squad).toEqual([...MEMBERS, NEW_MEMBER, MINTED]);
      expect(view.result.current.squadRef.current).toEqual([...MEMBERS, NEW_MEMBER, MINTED]);
    });

    it('takes a rename as the server answered it: the member with the new name and its stamp', async () => {
      const view = await mount();
      const renamed = answered(MEMBERS[1], { name: 'Sato-san' });

      await change(view, [renamed]);

      expect(view.result.current.squad).toEqual([MEMBERS[0], renamed, MEMBERS[2]]);
      expect(view.result.current.squadRef.current[1].name).toBe('Sato-san');
    });

    it('takes the member as a read holds it too: a name another device gave it later replaces the one shown', async () => {
      const view = await mount();
      const later = answered(MEMBERS[1], { name: 'Sato-san' });
      api.fetchSquads.mockResolvedValue({ t: [MEMBERS[0], later, MEMBERS[2]] });

      await announce();

      expect(view.result.current.squad[1]).toEqual(later);
    });

    it('lists the members in member number order, whichever list each came in on', async () => {
      const view = await mount();
      await change(view, [NEW_MEMBER]);
      // The read answers with a member numbered after the one the editor added, and without it.
      api.fetchSquads.mockResolvedValue({ t: [...MEMBERS, MINTED] });

      await announce();

      expect(view.result.current.squad.map((m) => m.index)).toEqual([1, 2, 3, 4, 5]);
      expect(view.result.current.squadRef.current.map((m) => m.index)).toEqual([1, 2, 3, 4, 5]);
    });

    it('does not carry another team\'s members into the list of the team the editor is given', async () => {
      const OTHER = [{ id: 'oth-1', index: 1, name: 'Oda' }];
      const view = await mount();
      expect(view.result.current.squad).toEqual(MEMBERS);
      api.fetchSquads.mockResolvedValue({ u: OTHER });

      view.rerender(props({ teamId: 'u' }));
      await act(async () => { await Promise.resolve(); });

      expect(view.result.current.squad).toEqual(OTHER);
      expect(view.result.current.squadRef.current).toEqual(OTHER);
    });
  });

  // Each write of a member is stamped by the server, and the editor keeps the newest
  // copy of each member by that stamp, whichever way it came: an own write's answer or a
  // read's list, in whatever order they arrive.
  describe('the newest copy of a member stands', () => {
    it('an older list never undoes a newer rename', async () => {
      const view = await mount();
      const renamed = answered(MEMBERS[1], { name: 'Sato-san' });
      await change(view, [renamed]);
      // A list read before the rename was made: the member as it was, unstamped.
      api.fetchSquads.mockResolvedValue({ t: MEMBERS });

      await announce();

      expect(view.result.current.squad[1]).toEqual(renamed);
      expect(view.result.current.squadRef.current[1]).toEqual(renamed);
    });

    it('another device\'s later rename shows, even after this device wrote the member', async () => {
      const view = await mount();
      const mine = answered(MEMBERS[1], { name: 'Sato-san' });
      await change(view, [mine]);
      const theirs = namedLater(mine, 'Sato-kun');
      api.fetchSquads.mockResolvedValue({ t: [MEMBERS[0], theirs, MEMBERS[2]] });

      await announce();

      expect(view.result.current.squad[1]).toEqual(theirs);
      expect(view.result.current.squadRef.current[1].name).toBe('Sato-kun');
    });

    it('an own write\'s answer older than a list already shown does not undo the list', async () => {
      const view = await mount();
      const mine = answered(MEMBERS[1], { name: 'Sato-san' });
      const theirs = namedLater(mine, 'Sato-kun');
      api.fetchSquads.mockResolvedValue({ t: [MEMBERS[0], theirs, MEMBERS[2]] });
      await announce();
      expect(view.result.current.squad[1]).toEqual(theirs);

      await change(view, [mine]);

      expect(view.result.current.squad[1]).toEqual(theirs);
      expect(view.result.current.squadRef.current[1]).toEqual(theirs);
    });

    it('a name cleared by a write stands over a list that still names the member', async () => {
      const view = await mount();
      const cleared = answered(MEMBERS[1], { name: '' });
      await change(view, [cleared]);
      api.fetchSquads.mockResolvedValue({ t: MEMBERS });

      await announce();

      expect(view.result.current.squad[1]).toEqual(cleared);
    });

    it('a member nobody has written yet is taken as the list that arrives holds it', async () => {
      const view = await mount();
      api.fetchSquads.mockResolvedValue({ t: [MEMBERS[0], { ...MEMBERS[1], name: 'Sato-sensei' }, MEMBERS[2]] });

      await announce();

      expect(view.result.current.squad[1].name).toBe('Sato-sensei');
    });
  });

  it('are read again with a followed lineup and shown with it: no render shows a lineup naming a member the list lacks', async () => {
    const renders = [];
    const view = renderHook((p) => {
      const form = useLineupForm(p);
      renders.push({ squad: form.squad, memberIds: form.memberIds });
      return form;
    }, { initialProps: props() });
    await act(async () => { await Promise.resolve(); });
    api.fetchSquads.mockResolvedValue({ t: [...MEMBERS, NEW_MEMBER] });
    api.fetchLineupInForce.mockResolvedValue(FOLLOWED);
    renders.length = 0;

    await announce();

    expect(api.fetchSquads).toHaveBeenCalledTimes(2);
    expect(view.result.current.memberIds[2]).toBe('mem-9');
    expect(view.result.current.squad).toContainEqual(NEW_MEMBER);
    expect(renders.length).toBeGreaterThan(0);
    expect(renders.every(({ squad, memberIds }) => Object.values(memberIds).every((id) => !id || squad.some((m) => m.id === id)))).toBe(true);
  });

  it('keep the list the editor has, and the lineup is still shown, when the read that comes with a followed lineup fails', async () => {
    const view = await mount();
    api.fetchSquads.mockRejectedValue(new Error('offline'));
    api.fetchLineupInForce.mockResolvedValue(FOLLOWED);

    await announce();

    expect(view.result.current.memberIds[2]).toBe('mem-9');
    expect(view.result.current.squad).toEqual(MEMBERS);
    expect(view.result.current.squadUnavailable, 'a list that was read is not unavailable').toBe(false);
  });

  it('are waited for, with the lineup, only until the deadline: a read never answered does not hold a followed lineup back for good', async () => {
    const view = await mount();
    api.fetchSquads.mockReturnValue(new Promise(() => {}));
    api.fetchLineupInForce.mockResolvedValue(FOLLOWED);
    vi.useFakeTimers();
    try {
      await announce();
      expect(view.result.current.memberIds[2], 'shown with the members, so held while they are awaited').toBe('mem-2');

      await act(async () => { await vi.advanceTimersByTimeAsync(FETCH_TIMEOUT_MS); });

      expect(view.result.current.memberIds[2]).toBe('mem-9');
      expect(view.result.current.squad).toEqual(MEMBERS);
    } finally {
      vi.useRealTimers();
    }
  });

  it('are shown over an edit made while they were out, which the followed lineup is not: the list is not the operator\'s edit', async () => {
    const view = await mount();
    const members = deferred();
    const lineup = deferred();
    api.fetchSquads.mockReturnValue(members.promise);
    api.fetchLineupInForce.mockReturnValue(lineup.promise);
    await announce();
    expect(api.fetchSquads).toHaveBeenCalledTimes(2);

    edit(view, { 1: 'Mori' });
    await act(async () => {
      members.resolve({ t: [...MEMBERS, NEW_MEMBER] });
      lineup.resolve(FOLLOWED);
    });

    expect(view.result.current.values[1], 'the followed lineup is not shown over an edit').toBe('Mori');
    expect(view.result.current.squad, 'the list is').toEqual([...MEMBERS, NEW_MEMBER]);

    await announce();
    expect(api.fetchLineupInForce, 'the lineup of an edited form is not read again').toHaveBeenCalledTimes(2);
    expect(api.fetchSquads, 'its members are').toHaveBeenCalledTimes(3);
  });

  it('are read again while the form holds edits, and the edits stay: the list is not the operator\'s edit', async () => {
    const view = await mount();
    edit(view, { 1: 'Mori' });
    api.fetchSquads.mockResolvedValue({ t: [...MEMBERS, NEW_MEMBER] });
    api.fetchLineupInForce.mockResolvedValue(FOLLOWED);

    await announce();

    expect(api.fetchSquads, 'a member another device added is read').toHaveBeenCalledTimes(2);
    expect(view.result.current.squad).toContainEqual(NEW_MEMBER);
    expect(view.result.current.squadRef.current, 'and is on the ref a Save resolves a typed name against').toContainEqual(NEW_MEMBER);
    expect(api.fetchLineupInForce, 'the lineup is not read over the edit').toHaveBeenCalledTimes(1);
    expect(view.result.current.values[1]).toBe('Mori');
    expect(view.result.current.values[2], 'nor is a position the operator left alone replaced').toBe('Sato');
    expect(view.result.current.dirty).toBe(true);
  });

  // A name typed before the first list is shown would be resolved against no members: a
  // new name minted where the member's seeded slot is free, and the name of a member the
  // team has refused by the server as a second one. The editors that resolve a typed name
  // wait for the list first (waitForMembers), bounded by the deadline of any request.
  describe('the wait for the first list', () => {
    const openedWith = async (opening) => {
      api.fetchSquads.mockReturnValueOnce(opening.promise);
      const view = renderHook((p) => useLineupForm(p), { initialProps: props() });
      await act(async () => { await Promise.resolve(); });
      return view;
    };

    it('is nothing to wait for once the list the editor opened with is shown', async () => {
      const view = await mount();
      expect(view.result.current.waitForMembers()).toBeNull();
    });

    it('lasts until the read made as the editor opened shows its list, and says so', async () => {
      const opening = deferred();
      const view = await openedWith(opening);
      let outcome;
      const waiting = view.result.current.waitForMembers();
      expect(waiting, 'the read is out').not.toBeNull();
      waiting.then((listed) => { outcome = listed; });
      await act(async () => { await Promise.resolve(); });
      expect(outcome, 'still waiting').toBeUndefined();

      await act(async () => { opening.resolve({ t: MEMBERS }); });

      expect(await waiting).toBe(true);
      expect(view.result.current.squad).toEqual(MEMBERS);
      expect(view.result.current.waitForMembers()).toBeNull();
    });

    it('is ended by an empty list: a team with no members is a list that was read', async () => {
      const opening = deferred();
      const view = await openedWith(opening);
      const waiting = view.result.current.waitForMembers();

      await act(async () => { opening.resolve({ t: [] }); });

      expect(await waiting).toBe(true);
    });

    it('is ended by the read failing, which says no list was shown, and there is nothing to wait for after it', async () => {
      const opening = deferred();
      const view = await openedWith(opening);
      const waiting = view.result.current.waitForMembers();

      await act(async () => { opening.reject(new Error('down')); });

      expect(await waiting).toBe(false);
      expect(view.result.current.squadUnavailable).toBe(true);
      expect(view.result.current.waitForMembers(), 'the editor goes on without them, as it always did').toBeNull();
    });

    it('is ended at the deadline of any bounded request, saying no list was shown', async () => {
      vi.useFakeTimers();
      try {
        const view = await openedWith(deferred());
        let outcome = 'waiting';
        view.result.current.waitForMembers().then((listed) => { outcome = listed; });

        await act(async () => { await vi.advanceTimersByTimeAsync(FETCH_TIMEOUT_MS - 1); });
        expect(outcome).toBe('waiting');
        await act(async () => { await vi.advanceTimersByTimeAsync(1); });

        expect(outcome).toBe(false);
      } finally {
        vi.useRealTimers();
      }
    });

    // A read that hangs holds up the first typed name for the deadline and no other: after
    // it the editor goes on without the members, as it does when the read fails, and says
    // so. A list that arrives later is still shown.
    it('ends at the deadline for good: nothing is left to wait for, the members read as unavailable, and a list that arrives later is still shown', async () => {
      vi.useFakeTimers();
      try {
        const opening = deferred();
        const view = await openedWith(opening);
        view.result.current.waitForMembers();

        await act(async () => { await vi.advanceTimersByTimeAsync(FETCH_TIMEOUT_MS); });

        expect(view.result.current.waitForMembers(), 'a name typed next goes on at once').toBeNull();
        expect(view.result.current.squadUnavailable, 'and the editor says the members could not be read').toBe(true);

        await act(async () => { opening.resolve({ t: MEMBERS }); });

        expect(view.result.current.squad, 'the list that arrives late is shown').toEqual(MEMBERS);
        expect(view.result.current.squadUnavailable, 'and the members are available again').toBe(false);
        expect(view.result.current.waitForMembers()).toBeNull();
      } finally {
        vi.useRealTimers();
      }
    });

    it('is ended for a second wait begun meanwhile by the first one\'s deadline, not held for a deadline of its own', async () => {
      vi.useFakeTimers();
      try {
        const view = await openedWith(deferred());
        view.result.current.waitForMembers();
        await act(async () => { await vi.advanceTimersByTimeAsync(5000); });
        let second = 'waiting';
        view.result.current.waitForMembers().then((listed) => { second = listed; });

        await act(async () => { await vi.advanceTimersByTimeAsync(FETCH_TIMEOUT_MS - 5000); });

        expect(second).toBe(false);
      } finally {
        vi.useRealTimers();
      }
    });

    // Only a read's list ends it: the members an editor's own write put on the list are
    // not the team's, and a typed name resolved against them alone still misses the rest.
    it('is not ended by a member the editor wrote itself, however newly stamped: only a read\'s list ends it', async () => {
      const opening = deferred();
      const view = await openedWith(opening);

      await change(view, [answered(NEW_MEMBER)]);

      expect(view.result.current.squad).toEqual([expect.objectContaining({ id: 'mem-9', name: 'Zed' })]);
      expect(view.result.current.waitForMembers()).not.toBeNull();
      await act(async () => { opening.resolve({ t: MEMBERS }); });
      expect(view.result.current.waitForMembers()).toBeNull();
      expect(view.result.current.squad.map((m) => m.id)).toEqual(['mem-1', 'mem-2', 'mem-3', 'mem-9']);
    });

    it('is not started again by a read that is out once a list has been shown', async () => {
      const view = await mount();
      api.fetchSquads.mockReturnValue(new Promise(() => {}));

      await announce();

      expect(api.fetchSquads, 'a followed lineup reads the members again').toHaveBeenCalledTimes(2);
      expect(view.result.current.waitForMembers()).toBeNull();
    });

    it('starts again when the editor is given another team, whose members were not read yet', async () => {
      const view = await mount();
      expect(view.result.current.waitForMembers()).toBeNull();
      const other = deferred();
      api.fetchSquads.mockReturnValue(other.promise);

      view.rerender(props({ teamId: 'u' }));
      await act(async () => { await Promise.resolve(); });
      const waiting = view.result.current.waitForMembers();
      expect(waiting).not.toBeNull();

      await act(async () => { other.resolve({ u: [{ id: 'oth-1', index: 1, name: 'Oda' }] }); });
      expect(await waiting).toBe(true);
    });
  });
});
