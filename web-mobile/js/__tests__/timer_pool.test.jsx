// timer_pool.test.jsx: unit tests for createTimerPool (mp-wng6).
//
// The pool backs the main SSE effect's jittered refetch timers in app.jsx.
// The load-bearing property is self-pruning: a FIRED timer must remove its
// own id from the pool, otherwise a tab that stays mounted all day (a
// /display TV wall, a parked viewer) grows the pool by one or two entries
// per SSE event for the tab's lifetime.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { createTimerPool } from '../app.jsx';

describe('createTimerPool', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('runs the callback and self-prunes the fired timer', () => {
    const pool = createTimerPool();
    const fn = vi.fn();
    pool.schedule(fn, 100);
    expect(pool.pendingCount()).toBe(1);

    vi.advanceTimersByTime(100);
    expect(fn).toHaveBeenCalledTimes(1);
    expect(pool.pendingCount()).toBe(0);
  });

  it('does not accumulate fired timers across many events (mp-wng6 regression)', () => {
    const pool = createTimerPool();
    // Simulate a long-lived display tab absorbing an SSE event stream.
    for (let i = 0; i < 5000; i++) {
      pool.schedule(() => {}, 1 + (i % 500));
      vi.advanceTimersByTime(2);
    }
    vi.runAllTimers();
    expect(pool.pendingCount()).toBe(0);
  });

  it('clearAll cancels pending timers so their callbacks never fire', () => {
    const pool = createTimerPool();
    const fired = vi.fn();
    pool.schedule(fired, 100);
    pool.schedule(fired, 200);
    expect(pool.pendingCount()).toBe(2);

    pool.clearAll();
    expect(pool.pendingCount()).toBe(0);
    vi.runAllTimers();
    expect(fired).not.toHaveBeenCalled();
  });

  it('clearAll leaves already-fired timers untouched and is idempotent', () => {
    const pool = createTimerPool();
    const fired = vi.fn();
    pool.schedule(fired, 50);
    vi.advanceTimersByTime(50);
    expect(fired).toHaveBeenCalledTimes(1);

    pool.clearAll();
    pool.clearAll();
    expect(pool.pendingCount()).toBe(0);
  });

});
