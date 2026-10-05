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
import { API as realApi } from '../api_client.jsx';

const stubReact = global.React;
const KEYS = ['1', '2', '3'];
const NAMES = { positions: { 1: 'Aoki', 2: 'Sato', 3: 'Ito' }, memberIds: { 1: 'mem-1', 2: 'mem-2', 3: 'mem-3' } };
// What API.queuedLineupSave answers while a save of the lineup is queued: the
// lineup that save would write. null is nothing queued.
const QUEUED = { positions: { 1: 'Mori' }, memberIds: {} };
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
