// bc-obox C1/T1: the queued-writes gate. The unit project's global React is a
// stub whose effects never run, so each test swaps the real one in.
import RealReact from 'react';
import { renderHook, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import {
  useWaitForQueuedWrites, queuedWritesCount, queuedWritesBlockers, queuedWritesNounFor, waitingSentence,
  TIME_WRITE_KINDS,
} from '../queued_writes_wait.jsx';

const stubReact = global.React;
let queued;
let held;
let listeners;

function tell() {
  for (const fn of listeners) fn({ total: queued.length });
}

beforeEach(() => {
  global.React = RealReact;
  queued = [];
  held = [];
  listeners = new Set();
  window.API = {
    queuedWritesOf: vi.fn((compID, kinds) => queued.filter((d) => d.compID === compID && kinds.includes(d.kind)).length),
    heldWrites: vi.fn(() => held),
  };
  window.subscribeUnsentWrites = (fn) => { listeners.add(fn); fn({ total: queued.length }); return () => listeners.delete(fn); };
});

afterEach(() => {
  global.React = stubReact;
  delete window.API;
  delete window.subscribeUnsentWrites;
});

// The held run fires in a microtask after the signal.
const flush = () => act(async () => { await Promise.resolve(); });

describe('the words', () => {
  it('counts check-ins and time changes, one and many', () => {
    expect(queuedWritesNounFor('checkin', 1)).toBe('1 check-in');
    expect(queuedWritesNounFor('checkin', 3)).toBe('3 check-ins');
    expect(queuedWritesNounFor('time', 1)).toBe('1 time change');
    expect(queuedWritesNounFor('time', 2)).toBe('2 time changes');
    expect(queuedWritesNounFor('settings', 1)).toBe('1 settings change');
    expect(queuedWritesNounFor('settings', 2)).toBe('2 settings changes');
  });

  it('says what it waits for and what follows', () => {
    expect(waitingSentence('checkin', 3, 'The draw is made'))
      .toBe('Waiting for 3 check-ins to be sent. The draw is made once they arrive.');
    expect(waitingSentence('time', 1, 'The move is made'))
      .toBe('Waiting for 1 time change to be sent. The move is made once it arrives.');
  });
});

describe('the reader', () => {
  it('asks API.queuedWritesOf for the competition and kinds, and counts none for no kinds', () => {
    queued = [{ compID: 'c1', kind: 'time' }, { compID: 'c2', kind: 'time' }, { compID: 'c1', kind: 'score' }];
    expect(queuedWritesCount('c1', TIME_WRITE_KINDS)).toBe(1);
    expect(queuedWritesCount('c1', [])).toBe(0);
    expect(window.API.queuedWritesOf).toHaveBeenCalledWith('c1', ['time']);
  });

  it('names as blockers only the held writes of those kinds the server keeps refusing', () => {
    held = [
      { key: 'a', compID: 'c1', kind: 'checkin', keepsFailing: true },
      { key: 'b', compID: 'c1', kind: 'checkin', keepsFailing: false },
      { key: 'c', compID: 'c2', kind: 'checkin', keepsFailing: true },
      { key: 'd', compID: 'c1', kind: 'score', keepsFailing: true },
    ];
    expect(queuedWritesBlockers('c1', ['checkin', 'checkin-bulk']).map((h) => h.key)).toEqual(['a']);
  });
});

describe('useWaitForQueuedWrites', () => {
  it('runs at once, in the same tick, when nothing is queued', () => {
    const { result } = renderHook(() => useWaitForQueuedWrites('c1', TIME_WRITE_KINDS));
    const fn = vi.fn();
    act(() => { result.current.run(fn); });
    expect(fn).toHaveBeenCalledTimes(1);
    expect(result.current.waiting).toBe(false);
  });

  it('holds the run while writes are queued and runs it once the last one leaves', async () => {
    queued = [{ compID: 'c1', kind: 'time' }, { compID: 'c1', kind: 'time' }];
    const { result } = renderHook(() => useWaitForQueuedWrites('c1', TIME_WRITE_KINDS));
    const fn = vi.fn();
    act(() => { result.current.run(fn); });
    expect(fn).not.toHaveBeenCalled();
    expect(result.current.waiting).toBe(true);
    expect(result.current.count).toBe(2);

    queued.pop();
    act(() => { tell(); });
    await flush();
    expect(fn).not.toHaveBeenCalled();
    expect(result.current.count).toBe(1);

    queued.pop();
    act(() => { tell(); });
    await flush();
    expect(fn).toHaveBeenCalledTimes(1);
    expect(fn).toHaveBeenCalledWith();
    expect(result.current.waiting).toBe(false);

    // Nothing held: a later signal runs nothing again.
    act(() => { tell(); });
    await flush();
    expect(fn).toHaveBeenCalledTimes(1);
  });

  it('runs the latest run it was given', async () => {
    queued = [{ compID: 'c1', kind: 'time' }];
    const { result } = renderHook(() => useWaitForQueuedWrites('c1', TIME_WRITE_KINDS));
    const first = vi.fn();
    const second = vi.fn();
    act(() => { result.current.run(first); });
    act(() => { result.current.run(second); });
    queued = [];
    act(() => { tell(); });
    await flush();
    expect(first).not.toHaveBeenCalled();
    expect(second).toHaveBeenCalledTimes(1);
  });

  it('is not held by another competition or another kind', () => {
    queued = [{ compID: 'c2', kind: 'time' }, { compID: 'c1', kind: 'score' }];
    const { result } = renderHook(() => useWaitForQueuedWrites('c1', TIME_WRITE_KINDS));
    const fn = vi.fn();
    act(() => { result.current.run(fn); });
    expect(fn).toHaveBeenCalledTimes(1);
  });

  it('runNow runs the held run at once with its arguments; cancel drops it', async () => {
    queued = [{ compID: 'c1', kind: 'checkin' }];
    const { result } = renderHook(() => useWaitForQueuedWrites('c1', ['checkin', 'checkin-bulk']));
    const fn = vi.fn();
    act(() => { result.current.run(fn); });
    act(() => { result.current.runNow({ anyway: true }); });
    expect(fn).toHaveBeenCalledWith({ anyway: true });
    expect(result.current.waiting).toBe(false);

    const dropped = vi.fn();
    act(() => { result.current.run(dropped); });
    expect(result.current.waiting).toBe(true);
    act(() => { result.current.cancel(); });
    expect(result.current.waiting).toBe(false);
    queued = [];
    act(() => { tell(); });
    await flush();
    expect(dropped).not.toHaveBeenCalled();
    expect(fn).toHaveBeenCalledTimes(1);
  });

  it('names a write the server keeps refusing while it waits', () => {
    queued = [{ compID: 'c1', kind: 'checkin' }];
    const { result } = renderHook(() => useWaitForQueuedWrites('c1', ['checkin', 'checkin-bulk']));
    act(() => { result.current.run(vi.fn()); });
    expect(result.current.blockers).toEqual([]);
    held = [{ key: 'k1', compID: 'c1', kind: 'checkin', keepsFailing: true }];
    act(() => { tell(); });
    expect(result.current.blockers.map((h) => h.key)).toEqual(['k1']);
  });

  it('drops a held run on unmount', async () => {
    queued = [{ compID: 'c1', kind: 'time' }];
    const { result, unmount } = renderHook(() => useWaitForQueuedWrites('c1', TIME_WRITE_KINDS));
    const fn = vi.fn();
    act(() => { result.current.run(fn); });
    unmount();
    queued = [];
    tell();
    await flush();
    expect(fn).not.toHaveBeenCalled();
    expect(listeners.size).toBe(0);
  });

  it('drops a held run when the competition changes', async () => {
    queued = [{ compID: 'c1', kind: 'time' }];
    const { result, rerender } = renderHook(({ id }) => useWaitForQueuedWrites(id, TIME_WRITE_KINDS), { initialProps: { id: 'c1' } });
    const fn = vi.fn();
    act(() => { result.current.run(fn); });
    rerender({ id: 'c2' });
    expect(result.current.waiting).toBe(false);
    queued = [];
    act(() => { tell(); });
    await flush();
    expect(fn).not.toHaveBeenCalled();
  });
});
