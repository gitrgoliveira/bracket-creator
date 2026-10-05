// lineup_draft.jsx keeps the position picks a lineup editor has not saved in
// this tab's sessionStorage and offers them back when the same lineup is opened
// again (bc-lnul). The hook reads the React global at call time and this suite's
// global React is a stub whose effects never run, so each test swaps the real
// one in and drives the hook through a small harness that holds `current` in
// state the way the editors do, taking a restore as one more setState.

import RealReact from 'react';
import { renderHook, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { useLineupDraft, lineupDraftKey } from '../lineup_draft.jsx';

const stubReact = global.React;
beforeEach(() => { global.React = RealReact; });
afterEach(() => {
  global.React = stubReact;
  vi.unstubAllGlobals();
});

const HOUR = 60 * 60 * 1000;
const KEYS = ['1', '2', '3'];
const KEY = lineupDraftKey({ compId: 'comp', teamId: 'team', matchId: 'Pool A-0' });
const OTHER_KEY = lineupDraftKey({ compId: 'comp', teamId: 'team', matchId: 'Pool A-1' });
const stored = (map, key = KEY) => JSON.parse(map.get(`bc.lineupDraft.v1:${key}`));
const hasDraft = (map, key = KEY) => map.has(`bc.lineupDraft.v1:${key}`);
const seed = (map, draft, key = KEY) => map.set(`bc.lineupDraft.v1:${key}`, typeof draft === 'string' ? draft : JSON.stringify(draft));

const side = (positions = {}, memberIds = {}) => ({
  positions: { 1: '', 2: '', 3: '', ...positions },
  memberIds: { 1: '', 2: '', 3: '', ...memberIds },
});
const BLANK = side();
const NAMED = side({ 1: 'Aoki', 2: 'Sato' }, { 1: 'mem-1', 2: 'mem-2' });
const OTHER_NAMED = side({ 3: 'Ito' }, { 3: 'mem-3' });

function installStorage() {
  const map = new Map();
  const storage = {
    getItem: vi.fn((k) => (map.has(k) ? map.get(k) : null)),
    setItem: vi.fn((k, v) => { map.set(k, String(v)); }),
    removeItem: vi.fn((k) => { map.delete(k); }),
  };
  vi.stubGlobal('sessionStorage', storage);
  return { storage, map };
}

function useEditor({ key, ready, baseline, onRestore }) {
  const [current, setCurrent] = RealReact.useState(baseline);
  const draft = useLineupDraft({
    key, ready, baseline, current, positionKeys: KEYS,
    onRestore: (restored) => { onRestore(restored); setCurrent(restored); },
  });
  return { draft, current, setCurrent };
}

// A fresh mount of the editor: the lineup loaded is `baseline` (what the server
// holds), and `ready` says the load has finished.
function mount(props = {}) {
  const onRestore = vi.fn();
  const view = renderHook((p) => useEditor({ onRestore, ...p }), {
    initialProps: { key: KEY, ready: true, baseline: BLANK, ...props },
  });
  return { ...view, onRestore };
}

describe('lineupDraftKey', () => {
  it('names a match by its id and the starting lineup by "start"', () => {
    expect(lineupDraftKey({ compId: 'c', teamId: 't', matchId: 'Pool A-0' })).toBe('c:t:match:Pool A-0');
    expect(lineupDraftKey({ compId: 'c', teamId: 't', matchId: '' })).toBe('c:t:start');
    expect(lineupDraftKey({ compId: 'c', teamId: 't' })).toBe('c:t:start');
  });
});

describe('writing the draft', () => {
  it('keeps what differs from the loaded lineup, with the lineup it was made against', () => {
    const { map } = installStorage();
    const view = mount();
    expect(hasDraft(map)).toBe(false);

    act(() => view.result.current.setCurrent(NAMED));

    expect(stored(map)).toMatchObject({ baseline: BLANK, current: NAMED });
    expect(Date.now() - stored(map).savedAt).toBeLessThan(5000);
  });

  it('removes the draft when the lineup is edited back to what was loaded', () => {
    const { map } = installStorage();
    const view = mount();
    act(() => view.result.current.setCurrent(NAMED));
    expect(hasDraft(map)).toBe(true);
    act(() => view.result.current.setCurrent(BLANK));
    expect(hasDraft(map)).toBe(false);
  });

  it('removes the draft when the loaded lineup becomes the edited one (a save)', () => {
    const { map } = installStorage();
    const view = mount();
    act(() => view.result.current.setCurrent(NAMED));
    expect(hasDraft(map)).toBe(true);
    view.rerender({ key: KEY, ready: true, baseline: NAMED });
    expect(hasDraft(map)).toBe(false);
  });

  it('writes nothing for a lineup nobody changed', () => {
    const { storage } = installStorage();
    const view = mount({ ready: false });
    view.rerender({ key: KEY, ready: true, baseline: BLANK });
    expect(storage.setItem).not.toHaveBeenCalled();
  });
});

describe('restoring the draft', () => {
  it('offers an unsaved draft back when the same lineup is opened again', () => {
    const { map } = installStorage();
    const first = mount();
    act(() => first.result.current.setCurrent(NAMED));
    first.unmount();
    expect(hasDraft(map)).toBe(true);

    const again = mount();

    expect(again.onRestore).toHaveBeenCalledTimes(1);
    expect(again.onRestore).toHaveBeenCalledWith(NAMED);
    expect(again.result.current.current).toEqual(NAMED);
    expect(again.result.current.draft).toMatchObject({ restored: true, stale: null });
    // Restoring is not a save: the draft stays until the lineup is saved or discarded.
    expect(hasDraft(map)).toBe(true);
  });

  it('touches nothing until the lineup has loaded, then restores and keeps the draft', () => {
    const { storage, map } = installStorage();
    seed(map, { savedAt: Date.now(), baseline: BLANK, current: NAMED });

    // While loading, the editor shows an empty lineup that equals its empty baseline:
    // a sync step that ran now would remove the draft before it is read.
    const view = mount({ ready: false });
    expect(view.onRestore).not.toHaveBeenCalled();
    expect(storage.setItem).not.toHaveBeenCalled();
    expect(storage.removeItem).not.toHaveBeenCalled();
    expect(hasDraft(map)).toBe(true);

    view.rerender({ key: KEY, ready: true, baseline: BLANK });

    expect(view.onRestore).toHaveBeenCalledTimes(1);
    expect(view.onRestore).toHaveBeenCalledWith(NAMED);
    expect(view.result.current.current).toEqual(NAMED);
    expect(view.result.current.draft.restored).toBe(true);
    expect(stored(map)).toMatchObject({ baseline: BLANK, current: NAMED });
  });

  it('reads the draft once: later renders do not restore it again', () => {
    const { map } = installStorage();
    seed(map, { savedAt: Date.now(), baseline: BLANK, current: NAMED });
    const view = mount();
    act(() => view.result.current.setCurrent(side({ 1: 'Aoki' }, { 1: 'mem-1' })));
    view.rerender({ key: KEY, ready: true, baseline: BLANK });
    expect(view.onRestore).toHaveBeenCalledTimes(1);
  });

  it('stops reporting restored once the lineup is saved', () => {
    const { map } = installStorage();
    seed(map, { savedAt: Date.now(), baseline: BLANK, current: NAMED });
    const view = mount();
    expect(view.result.current.draft.restored).toBe(true);

    view.rerender({ key: KEY, ready: true, baseline: NAMED });

    expect(view.result.current.draft.restored).toBe(false);
    expect(hasDraft(map)).toBe(false);
  });

  it('stops reporting restored once the lineup is edited back to what was loaded', () => {
    const { map } = installStorage();
    seed(map, { savedAt: Date.now(), baseline: BLANK, current: NAMED });
    const view = mount();
    act(() => view.result.current.setCurrent(BLANK));
    expect(view.result.current.draft.restored).toBe(false);
    expect(hasDraft(map)).toBe(false);
  });

  it('restores only the positions of the lineup it is asked about', () => {
    const { map } = installStorage();
    seed(map, {
      savedAt: Date.now(),
      baseline: { positions: { 1: '', 9: 'Gone' }, memberIds: {} },
      current: { positions: { 1: 'Aoki', 9: 'Gone' }, memberIds: { 1: 'mem-1' } },
    });
    const view = mount();
    expect(view.onRestore).toHaveBeenCalledWith(side({ 1: 'Aoki' }, { 1: 'mem-1' }));
  });
});

describe('a lineup saved meanwhile', () => {
  const SAVED_MEANWHILE = side({ 3: 'Mori' }, { 3: 'mem-4' });

  it('is not overwritten: the draft is dropped and what was not restored is named', () => {
    const { map } = installStorage();
    seed(map, {
      savedAt: Date.now(),
      baseline: side({ 3: 'Ito' }, { 3: 'mem-3' }),
      current: side({ 1: 'Aoki', 2: 'Sato', 3: 'Ito' }, { 1: 'mem-1', 2: 'mem-2', 3: 'mem-3' }),
    });

    const view = mount({ baseline: SAVED_MEANWHILE });

    expect(view.onRestore).not.toHaveBeenCalled();
    expect(view.result.current.current).toEqual(SAVED_MEANWHILE);
    // Position 3 was not changed by the draft, so its name is not listed.
    expect(view.result.current.draft).toMatchObject({ restored: false, stale: { names: ['Aoki', 'Sato'] } });
    expect(hasDraft(map)).toBe(false);
  });

  it('still says so when the draft only cleared positions', () => {
    const { map } = installStorage();
    seed(map, { savedAt: Date.now(), baseline: side({ 1: 'Aoki' }, { 1: 'mem-1' }), current: BLANK });
    const view = mount({ baseline: SAVED_MEANWHILE });
    expect(view.result.current.draft.stale).toEqual({ names: [] });
    expect(hasDraft(map)).toBe(false);
  });

  it('lists a name once however many positions hold it', () => {
    const { map } = installStorage();
    seed(map, { savedAt: Date.now(), baseline: BLANK, current: side({ 1: 'Aoki', 2: 'Aoki' }) });
    const view = mount({ baseline: SAVED_MEANWHILE });
    expect(view.result.current.draft.stale).toEqual({ names: ['Aoki'] });
  });

  it('is dropped without a word when the saved lineup already holds what the draft held', () => {
    const { map } = installStorage();
    seed(map, { savedAt: Date.now(), baseline: BLANK, current: NAMED });
    const view = mount({ baseline: NAMED });
    expect(view.onRestore).not.toHaveBeenCalled();
    expect(view.result.current.draft).toMatchObject({ restored: false, stale: null });
    expect(hasDraft(map)).toBe(false);
  });
});

// "Not restored, the lineup changed since" names the names the draft could not
// restore from the lineup as it was opened. Re-entering one of several is no
// answer to the others, so it stays through every edit and goes when the editor
// says the lineup was saved or given up (`resolved`), or when the lineup is left.
describe('the "not restored" notice', () => {
  const SAVED_MEANWHILE = side({ 3: 'Mori' }, { 3: 'mem-4' });

  function openWithDroppedDraft() {
    const { map } = installStorage();
    seed(map, { savedAt: Date.now(), baseline: BLANK, current: NAMED });
    const view = mount({ baseline: SAVED_MEANWHILE });
    expect(view.result.current.draft.stale).toEqual({ names: ['Aoki', 'Sato'] });
    return { view, map };
  }

  it('stays while nothing is done to the lineup', () => {
    const { view } = openWithDroppedDraft();
    view.rerender({ key: KEY, ready: true, baseline: SAVED_MEANWHILE });
    view.rerender({ key: KEY, ready: true, baseline: SAVED_MEANWHILE });
    expect(view.result.current.draft.stale).toEqual({ names: ['Aoki', 'Sato'] });
  });

  it('stays when the operator changes the lineup, and the change is a draft of its own', () => {
    const { view, map } = openWithDroppedDraft();
    act(() => view.result.current.setCurrent(side({ 1: 'Aoki', 3: 'Mori' }, { 1: 'mem-1', 3: 'mem-4' })));
    expect(view.result.current.draft.stale).toEqual({ names: ['Aoki', 'Sato'] });
    expect(stored(map).current.positions[1]).toBe('Aoki');
  });

  it('stays through one name after another being entered, so the rest are still named', () => {
    const { view } = openWithDroppedDraft();
    act(() => view.result.current.setCurrent(side({ 1: 'Aoki', 3: 'Mori' }, { 1: 'mem-1', 3: 'mem-4' })));
    act(() => view.result.current.setCurrent(side({ 1: 'Aoki', 2: 'Sato', 3: 'Mori' }, { 1: 'mem-1', 2: 'mem-2', 3: 'mem-4' })));
    expect(view.result.current.draft.stale).toEqual({ names: ['Aoki', 'Sato'] });
  });

  it('stays when the operator changes it and puts it back: the names are still not in the lineup', () => {
    const { view } = openWithDroppedDraft();
    act(() => view.result.current.setCurrent(NAMED));
    act(() => view.result.current.setCurrent(SAVED_MEANWHILE));
    expect(view.result.current.draft.stale).toEqual({ names: ['Aoki', 'Sato'] });
  });

  it('stays when the lineup it was opened on is read again and has changed once more', () => {
    const { view } = openWithDroppedDraft();
    view.rerender({ key: KEY, ready: true, baseline: OTHER_NAMED });
    act(() => view.result.current.setCurrent(OTHER_NAMED));
    expect(view.result.current.draft.stale).toEqual({ names: ['Aoki', 'Sato'] });
  });

  it('goes once the editor says the lineup was saved', () => {
    const { view, map } = openWithDroppedDraft();
    act(() => view.result.current.setCurrent(NAMED));
    act(() => view.result.current.draft.resolved());
    view.rerender({ key: KEY, ready: true, baseline: NAMED });

    expect(view.result.current.draft.stale).toBeNull();
    expect(hasDraft(map)).toBe(false);
  });

  it('does not come back for the next change', () => {
    const { view } = openWithDroppedDraft();
    act(() => view.result.current.draft.resolved());
    act(() => view.result.current.setCurrent(NAMED));
    act(() => view.result.current.setCurrent(SAVED_MEANWHILE));
    act(() => view.result.current.setCurrent(OTHER_NAMED));
    expect(view.result.current.draft.stale).toBeNull();
  });

  it('is not shown again when the operator leaves the lineup and comes back to it', () => {
    const { view } = openWithDroppedDraft();
    view.rerender({ key: OTHER_KEY, ready: true, baseline: SAVED_MEANWHILE });
    expect(view.result.current.draft.stale).toBeNull();
    view.rerender({ key: KEY, ready: true, baseline: SAVED_MEANWHILE });
    expect(view.result.current.draft.stale).toBeNull();
  });
});

describe('an old or unreadable draft', () => {
  it.each([
    ['13 hours old', 13 * HOUR],
    ['just over 12 hours old', 12 * HOUR + 1000],
  ])('is ignored and removed when it is %s', (_name, age) => {
    const { map } = installStorage();
    seed(map, { savedAt: Date.now() - age, baseline: BLANK, current: NAMED });
    const view = mount();
    expect(view.onRestore).not.toHaveBeenCalled();
    expect(view.result.current.draft).toMatchObject({ restored: false, stale: null });
    expect(hasDraft(map)).toBe(false);
  });

  it('is still offered when it is just under 12 hours old', () => {
    const { map } = installStorage();
    seed(map, { savedAt: Date.now() - (12 * HOUR - 60 * 1000), baseline: BLANK, current: NAMED });
    const view = mount();
    expect(view.onRestore).toHaveBeenCalledWith(NAMED);
  });

  it.each([
    ['not JSON', 'not json'],
    ['null', 'null'],
    ['a number', '12'],
    ['a draft with no time', JSON.stringify({ baseline: BLANK, current: NAMED })],
    ['a draft with no lineup', JSON.stringify({ savedAt: Date.now(), baseline: BLANK })],
    ['a draft that changed nothing', JSON.stringify({ savedAt: Date.now(), baseline: NAMED, current: NAMED })],
  ])('is dropped when it is %s', (_name, raw) => {
    const { map } = installStorage();
    seed(map, raw);
    const view = mount();
    expect(view.onRestore).not.toHaveBeenCalled();
    expect(view.result.current.draft).toMatchObject({ restored: false, stale: null });
    expect(hasDraft(map)).toBe(false);
  });
});

describe('a storage that throws', () => {
  const boom = () => { throw new Error('storage refused'); };

  it('never throws out of the hook when it cannot read', () => {
    const { storage } = installStorage();
    storage.getItem.mockImplementation(boom);
    let view;
    expect(() => { view = mount(); }).not.toThrow();
    expect(view.result.current.draft).toMatchObject({ restored: false, stale: null });
    expect(() => act(() => view.result.current.setCurrent(NAMED))).not.toThrow();
    expect(view.result.current.current).toEqual(NAMED);
  });

  it('never throws out of the hook when it cannot write (private mode, quota)', () => {
    const { storage } = installStorage();
    storage.setItem.mockImplementation(boom);
    const view = mount();
    expect(() => act(() => view.result.current.setCurrent(NAMED))).not.toThrow();
    expect(view.result.current.current).toEqual(NAMED);
  });

  it('never throws out of the hook when it cannot remove, and discard still restores the lineup', () => {
    const { storage, map } = installStorage();
    storage.removeItem.mockImplementation(boom);
    seed(map, { savedAt: Date.now(), baseline: BLANK, current: NAMED });
    const view = mount();
    expect(() => act(() => view.result.current.draft.discard())).not.toThrow();
    expect(view.result.current.current).toEqual(BLANK);
  });

  it('never throws out of the hook when reaching sessionStorage itself throws', () => {
    const original = Object.getOwnPropertyDescriptor(globalThis, 'sessionStorage');
    Object.defineProperty(globalThis, 'sessionStorage', { configurable: true, get: boom });
    try {
      let view;
      expect(() => { view = mount(); }).not.toThrow();
      expect(() => act(() => view.result.current.setCurrent(NAMED))).not.toThrow();
      expect(() => act(() => view.result.current.draft.discard())).not.toThrow();
      expect(view.result.current.current).toEqual(BLANK);
    } finally {
      if (original) Object.defineProperty(globalThis, 'sessionStorage', original);
      else delete globalThis.sessionStorage;
    }
  });

  it('works with no sessionStorage at all', () => {
    vi.stubGlobal('sessionStorage', undefined);
    const view = mount();
    expect(() => act(() => view.result.current.setCurrent(NAMED))).not.toThrow();
    expect(view.result.current.draft).toMatchObject({ restored: false, stale: null });
  });
});

describe('discard', () => {
  it('puts the loaded lineup back and removes the draft', () => {
    const { map } = installStorage();
    seed(map, { savedAt: Date.now(), baseline: BLANK, current: NAMED });
    const view = mount();
    expect(view.result.current.current).toEqual(NAMED);

    act(() => view.result.current.draft.discard());

    expect(view.onRestore).toHaveBeenLastCalledWith(BLANK);
    expect(view.result.current.current).toEqual(BLANK);
    expect(view.result.current.draft.restored).toBe(false);
    expect(hasDraft(map)).toBe(false);
  });

  it('leaves the next edit a draft of its own, never one that was restored', () => {
    const { map } = installStorage();
    seed(map, { savedAt: Date.now(), baseline: BLANK, current: NAMED });
    const view = mount();
    act(() => view.result.current.draft.discard());

    act(() => view.result.current.setCurrent(side({ 3: 'Ito' }, { 3: 'mem-3' })));

    expect(view.result.current.draft.restored).toBe(false);
    expect(stored(map).current).toEqual(OTHER_NAMED);
  });
});

describe('an editor that takes the restore only as a callback', () => {
  it('is told it was restored at once, and discard removes the draft by itself', () => {
    const { map } = installStorage();
    seed(map, { savedAt: Date.now(), baseline: BLANK, current: NAMED });
    const onRestore = vi.fn();
    const view = renderHook(() => useLineupDraft({
      key: KEY, ready: true, baseline: BLANK, current: BLANK, positionKeys: KEYS, onRestore,
    }));

    expect(onRestore).toHaveBeenCalledWith(NAMED);
    expect(view.result.current.restored).toBe(true);

    act(() => view.result.current.discard());

    expect(onRestore).toHaveBeenLastCalledWith(BLANK);
    expect(view.result.current.restored).toBe(false);
    expect(hasDraft(map)).toBe(false);
  });
});

describe('another lineup', () => {
  it('does not take the previous key\'s draft', () => {
    const { map } = installStorage();
    seed(map, { savedAt: Date.now(), baseline: BLANK, current: NAMED }, KEY);

    const view = mount({ key: OTHER_KEY });

    expect(view.onRestore).not.toHaveBeenCalled();
    expect(view.result.current.draft).toMatchObject({ restored: false, stale: null });
    expect(hasDraft(map, KEY)).toBe(true);
  });

  it('starts over when the key changes under a mounted hook: the new key\'s draft, and nothing written across', () => {
    const { map } = installStorage();
    seed(map, { savedAt: Date.now(), baseline: BLANK, current: NAMED }, KEY);
    seed(map, { savedAt: Date.now(), baseline: BLANK, current: OTHER_NAMED }, OTHER_KEY);
    const view = mount({ key: KEY });
    expect(view.result.current.draft.restored).toBe(true);
    const firstDraft = map.get(`bc.lineupDraft.v1:${KEY}`);
    const otherDraft = map.get(`bc.lineupDraft.v1:${OTHER_KEY}`);

    // The other lineup is loading: the editor still shows the first one's picks.
    view.rerender({ key: OTHER_KEY, ready: false, baseline: BLANK });
    expect(view.result.current.draft).toMatchObject({ restored: false, stale: null });
    expect(map.get(`bc.lineupDraft.v1:${OTHER_KEY}`)).toBe(otherDraft);

    act(() => view.result.current.setCurrent(BLANK));
    view.rerender({ key: OTHER_KEY, ready: true, baseline: BLANK });

    expect(view.onRestore).toHaveBeenLastCalledWith(OTHER_NAMED);
    expect(view.result.current.current).toEqual(OTHER_NAMED);
    expect(view.result.current.draft.restored).toBe(true);
    expect(map.get(`bc.lineupDraft.v1:${KEY}`)).toBe(firstDraft);
  });
});
