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

const stubReact = global.React;
const KEYS = ['1', '2', '3'];
const NAMES = { positions: { 1: 'Aoki', 2: 'Sato', 3: 'Ito' }, memberIds: { 1: 'mem-1', 2: 'mem-2', 3: 'mem-3' } };
// What API.queuedLineupSave answers while a save of the lineup is queued: the
// lineup that save would write. null is nothing queued.
const QUEUED = { positions: { 1: 'Mori' }, memberIds: {} };
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
    queuedLineupSave: vi.fn().mockReturnValue(null),
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
  const promise = new Promise((res) => { resolve = res; });
  return { promise, resolve };
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

  it('removes any other stored lineup the same way (a legacy round)', async () => {
    api.fetchLineupInForce.mockResolvedValueOnce({ ...NAMES, sourceRound: 1 }).mockResolvedValue(STARTING);
    const view = await mount();
    const remove = vi.fn().mockResolvedValue(true);
    await act(async () => { await view.result.current.removeStored(remove, 'Failed to remove the lineup'); });
    expect(remove).toHaveBeenCalledTimes(1);
    expect(view.result.current.source).toEqual({ round: 0 });
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

  it('is true when the API answers with the queued lineup, even one with every position cleared, and false for null', async () => {
    api.queuedLineupSave.mockReturnValue(QUEUED);
    expect((await mount()).result.current.saveQueued).toBe(true);

    api.queuedLineupSave.mockReturnValue({ positions: {}, memberIds: {} });
    expect((await mount()).result.current.saveQueued).toBe(true);

    api.queuedLineupSave.mockReturnValue(null);
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

    api.queuedLineupSave.mockReturnValue(null);
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

// A Save writes the lineup as stored NOW with the operator's changes on it, not
// the form restated (operator decision 2026-10-05), so a position another device
// changed since the form was read is not put back. lineupToSave reads the lineup
// again for it, under the deadline every bounded request has, and composes on the
// lineup as loaded when that read cannot be made, so an offline save is written
// (and queued) as it always was. What it reads is never shown.
describe('the lineup a Save writes', () => {
  // Another device changed position 2 after the form was read.
  const CHANGED_ELSEWHERE = {
    positions: { 1: 'Aoki', 2: 'Kato', 3: 'Ito' }, memberIds: { 1: 'mem-1', 2: 'mem-5', 3: 'mem-3' },
    sourceMatchId: 'm1', saved: true,
  };
  const RESTATED = { positions: { 1: 'Mori', 2: 'Sato', 3: 'Ito' }, memberIds: { 1: 'mem-1', 2: 'mem-2', 3: 'mem-3' }, changed: ['1'] };

  async function lineupToSave(view) {
    let composed;
    await act(async () => { composed = await view.result.current.lineupToSave(); });
    return composed;
  }

  it('is the lineup as stored now with the operator\'s changes on it, and names the positions those are', async () => {
    const view = await mount();
    edit(view, { 1: 'Mori' });
    api.fetchLineupInForce.mockResolvedValue(CHANGED_ELSEWHERE);

    expect(await lineupToSave(view)).toEqual({
      positions: { 1: 'Mori', 2: 'Kato', 3: 'Ito' },
      memberIds: { 1: 'mem-1', 2: 'mem-5', 3: 'mem-3' },
      changed: ['1'],
    });
    expect(api.fetchLineupInForce, 'the lineup is read again for it').toHaveBeenCalledTimes(2);
  });

  it('reads a starting lineup the way the form did, as round 0', async () => {
    const view = await mount({ matchId: '' });
    edit(view, { 1: 'Mori' });
    api.fetchTeamLineup.mockResolvedValue({ ...STARTING, positions: CHANGED_ELSEWHERE.positions, memberIds: CHANGED_ELSEWHERE.memberIds });

    const composed = await lineupToSave(view);

    expect(api.fetchTeamLineup).toHaveBeenLastCalledWith('c', 't', 0);
    expect(composed.positions).toEqual({ 1: 'Mori', 2: 'Kato', 3: 'Ito' });
  });

  it('shows nothing of what it read: the form, its baseline and what can be saved stay as they were', async () => {
    const view = await mount();
    edit(view, { 1: 'Mori' });
    api.fetchLineupInForce.mockResolvedValue(CHANGED_ELSEWHERE);

    await lineupToSave(view);

    expect(view.result.current.values).toEqual({ 1: 'Mori', 2: 'Sato', 3: 'Ito' });
    expect(view.result.current.memberIds[2]).toBe('mem-2');
    expect(view.result.current.baseline.positions[2]).toBe('Sato');
    expect(view.result.current.dirty).toBe(true);
    expect(view.result.current.loading).toBe(false);
  });

  it('is the form restated on the lineup as loaded when the read fails, so an offline save is written as it always was', async () => {
    const view = await mount();
    edit(view, { 1: 'Mori' });
    api.fetchLineupInForce.mockRejectedValue(new TypeError('Failed to fetch'));

    expect(await lineupToSave(view)).toEqual(RESTATED);
    expect(api.fetchLineupInForce, 'the read was tried').toHaveBeenCalledTimes(2);
  });

  it('does the same when the read is not answered, once the deadline has passed and not before', async () => {
    const view = await mount();
    edit(view, { 1: 'Mori' });
    api.fetchLineupInForce.mockReturnValue(new Promise(() => {}));
    vi.useFakeTimers();
    try {
      let pending;
      act(() => { pending = view.result.current.lineupToSave(); });
      let settled = false;
      pending.then(() => { settled = true; });

      await act(async () => { await vi.advanceTimersByTimeAsync(FETCH_TIMEOUT_MS - 1); });
      expect(settled, 'it waits for the read until the deadline').toBe(false);
      await act(async () => { await vi.advanceTimersByTimeAsync(2); });

      expect(settled).toBe(true);
      expect(await pending).toEqual(RESTATED);
    } finally {
      vi.useRealTimers();
    }
  });

  it('takes the positions left alone as blank from a lineup that says nothing is in force', async () => {
    const view = await mount();
    edit(view, { 1: 'Mori' });
    api.fetchLineupInForce.mockResolvedValue(null);

    expect(await lineupToSave(view)).toEqual({
      positions: { 1: 'Mori', 2: '', 3: '' }, memberIds: { 1: 'mem-1', 2: '', 3: '' }, changed: ['1'],
    });
  });

  it('counts a position put back to what was loaded as no change at all, so what is stored there stands', async () => {
    const view = await mount();
    edit(view, { 1: 'Mori' });
    edit(view, { 1: 'Aoki', 3: 'Oda' });
    api.fetchLineupInForce.mockResolvedValue(CHANGED_ELSEWHERE);

    const composed = await lineupToSave(view);

    expect(composed.changed).toEqual(['3']);
    expect(composed.positions).toEqual({ 1: 'Aoki', 2: 'Kato', 3: 'Oda' });
  });
});

// The team's members are the other half of what both lineup editors show: a
// position's member is picked from them and named by them. The hook reads them,
// and again whenever it follows another device's lineup, so a lineup it shows
// never names a member the list lacks (one created elsewhere meanwhile).
describe('the team\'s members', () => {
  const NEW_MEMBER = { id: 'mem-9', index: 4, name: 'Zed' };
  const FOLLOWED = {
    positions: { 1: 'Aoki', 2: 'Zed', 3: 'Ito' }, memberIds: { 1: 'mem-1', 2: 'mem-9', 3: 'mem-3' },
    sourceMatchId: 'm1', saved: true,
  };
  const announce = () => act(async () => {
    window.dispatchEvent(new CustomEvent('lineup-updated', { detail: { competitionId: 'c' } }));
  });

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

  it('are the editor\'s to change after a rename or a mint: the list it sets is the list shown', async () => {
    const view = await mount();
    act(() => view.result.current.setSquad((list) => [...list, NEW_MEMBER]));
    expect(view.result.current.squad).toEqual([...MEMBERS, NEW_MEMBER]);
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

  it('are not read again for a lineup the form refuses to follow, and what a read answers is not shown over an edit', async () => {
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

    expect(view.result.current.values[1]).toBe('Mori');
    expect(view.result.current.squad).toEqual(MEMBERS);

    await announce();
    expect(api.fetchSquads, 'an edited form is not followed at all').toHaveBeenCalledTimes(2);
  });
});
