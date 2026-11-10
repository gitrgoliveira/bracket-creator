// The three check-in doors need the time of the action (modifiedAt) in a JSON
// body; the server refuses a check-in without one with a 400.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { API } from '../api_client.jsx';

describe('check-in requests carry their stamp', () => {
  let calls;
  beforeEach(() => {
    calls = [];
    global.fetch = vi.fn((url, init) => {
      calls.push({ url, init });
      return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({}) });
    });
  });
  afterEach(() => { vi.restoreAllMocks(); });

  it('sends modifiedAt on a check-in and an undo', async () => {
    await API.toggleCheckIn('c1', 'p1', true, 'pw');
    await API.toggleCheckIn('c1', 'p1', false, 'pw');
    expect(calls.map((c) => c.init.method)).toEqual(['PUT', 'DELETE']);
    for (const c of calls) {
      expect(c.init.headers['Content-Type']).toBe('application/json');
      expect(typeof JSON.parse(c.init.body).modifiedAt).toBe('number');
    }
  });

  it('sends modifiedAt beside the ids on a bulk check-in', async () => {
    await API.bulkCheckIn('c1', ['p1', 'p2'], 'pw');
    const body = JSON.parse(calls[0].init.body);
    expect(body.participantIds).toEqual(['p1', 'p2']);
    expect(typeof body.modifiedAt).toBe('number');
  });

  // The doors answer 200 {applied:false} when nothing was written, and
  // broadcast nothing: the desk must hear it, not keep the optimistic tick.
  const answer = (body) => {
    global.fetch = vi.fn(() => Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(body) }));
  };

  it('throws when a single check-in was superseded, naming the reason', async () => {
    answer({ applied: false, reason: 'superseded', participant: { id: 'p1' } });
    await expect(API.toggleCheckIn('c1', 'p1', true, 'pw')).rejects.toMatchObject({ reason: 'superseded' });
  });

  it('throws the server message when the clock is ahead, on a single and a bulk check-in', async () => {
    answer({ applied: false, reason: 'clock_skew', message: 'Nothing was changed: clock.' });
    await expect(API.toggleCheckIn('c1', 'p1', false, 'pw')).rejects.toThrow('Nothing was changed: clock.');
    await expect(API.bulkCheckIn('c1', ['p1'], 'pw')).rejects.toMatchObject({ reason: 'clock_skew' });
  });

  it('treats a superseded tap whose stored person already holds the asked state as done', async () => {
    answer({ applied: false, reason: 'superseded', participant: { id: 'p1', checkedIn: true } });
    await expect(API.toggleCheckIn('c1', 'p1', true, 'pw')).resolves.toEqual({ id: 'p1', checkedIn: true });
    answer({ applied: false, reason: 'superseded', participant: { id: 'p1', checkedIn: false } });
    await expect(API.toggleCheckIn('c1', 'p1', false, 'pw')).resolves.toEqual({ id: 'p1', checkedIn: false });
    // An unchecked person after a check-in tap is a real loss, not a tie.
    answer({ applied: false, reason: 'superseded', participant: { id: 'p1' } });
    await expect(API.toggleCheckIn('c1', 'p1', true, 'pw')).rejects.toMatchObject({ reason: 'superseded' });
  });

  // The direct doors never resend, so a refusal re-checks the clock frame (the
  // score doors do), whichever door it came from.
  let hop = 0;
  it.each([
    ['a check-in', () => API.toggleCheckIn('c1', 'p1', true, 'pw'), { applied: false, reason: 'clock_skew', message: 'm' }],
    ['a bulk check-in', () => API.bulkCheckIn('c1', ['p1'], 'pw'), { applied: false, reason: 'clock_skew', message: 'm' }],
    ['a court call', () => API.sendAnnouncement('Hi', 5, 'pw'), { applied: false, reason: 'clock_skew', message: 'm' }],
    ['a clear', () => API.clearAnnouncements('pw'), { applied: false, reason: 'clock_skew', message: 'm' }],
  ])('relearns the clock when %s is refused', async (_name, call, body) => {
    vi.useFakeTimers({ toFake: ['Date'] });
    try {
      vi.setSystemTime(Date.now() + (++hop) * 3_600_000); // past the relearn throttle window
      answer(body);
      await expect(call()).rejects.toThrow();
      expect(global.fetch.mock.calls.some(([url]) => url === '/api/time')).toBe(true);
    } finally {
      vi.useRealTimers();
    }
  });

  it('returns a bulk answer that applied some ids, superseded ones included', async () => {
    const body = { checkedIn: 1, alreadyCheckedIn: 0, notFound: [], superseded: [{ id: 'p2' }] };
    answer(body);
    await expect(API.bulkCheckIn('c1', ['p1', 'p2'], 'pw')).resolves.toEqual(body);
  });
});
