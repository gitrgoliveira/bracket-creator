// withinDeadline (write_result.jsx) gives a wait that is not a request of its own
// the deadline every bounded request has: the team editor's save before a
// representative-bout change, and the lineup read a pick or a Save is composed on.

import { describe, it, expect, vi, afterEach } from 'vitest';
import { withinDeadline, TIMED_OUT, FETCH_TIMEOUT_MS } from '../write_result.jsx';

describe('withinDeadline', () => {
  afterEach(() => { vi.useRealTimers(); });

  it('is the promise\'s own outcome when it settles first, and leaves no timer behind', async () => {
    vi.useFakeTimers();
    expect(await withinDeadline(Promise.resolve('answer'), FETCH_TIMEOUT_MS)).toBe('answer');
    expect(vi.getTimerCount()).toBe(0);
  });

  it('is TIMED_OUT once the deadline has passed with no outcome, and not before', async () => {
    vi.useFakeTimers();
    let outcome = 'waiting';
    withinDeadline(new Promise(() => {}), FETCH_TIMEOUT_MS).then((value) => { outcome = value; });

    await vi.advanceTimersByTimeAsync(FETCH_TIMEOUT_MS - 1);
    expect(outcome).toBe('waiting');
    await vi.advanceTimersByTimeAsync(1);
    expect(outcome).toBe(TIMED_OUT);
  });

  it('ends only the wait: the promise runs on and its late outcome reaches whoever else holds it', async () => {
    vi.useFakeTimers();
    let answer;
    const slow = new Promise((resolve) => { answer = resolve; });
    const late = vi.fn();
    slow.then(late);

    const waited = withinDeadline(slow, 100);
    await vi.advanceTimersByTimeAsync(100);
    expect(await waited).toBe(TIMED_OUT);

    answer('late');
    await vi.advanceTimersByTimeAsync(0);
    expect(late).toHaveBeenCalledWith('late');
  });

  it('rejects with the promise\'s own rejection, before the deadline', async () => {
    vi.useFakeTimers();
    await expect(withinDeadline(Promise.reject(new Error('offline')), FETCH_TIMEOUT_MS)).rejects.toThrow('offline');
    expect(vi.getTimerCount()).toBe(0);
  });
});
