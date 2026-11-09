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
});
