// Tests for API.revertMatchToQueue (mp-s128). This helper is a critical part of
// the shiaijo flow: it backs both the explicit "Send back to queue" control and
// the pickMatch auto-defer when the operator picks another bout. Pin the
// URL/method/header and error-parsing behavior.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { API } from '../api_client.jsx';

function mockFetch(status, body) {
  return vi.fn(() =>
    Promise.resolve({
      ok: status >= 200 && status < 300,
      status,
      json: () => Promise.resolve(body),
    })
  );
}

// A refusal is logged as the queue drops it; take that off the record.
function expectWarned() { expect(console.warn).toHaveBeenCalled(); console.warn.mockClear(); }

// bc-evts: the send back is an OUTBOX door. It answers pending at once; the
// server's answer arrives through `settled` (true when it landed, a rejection
// with the server's sentence when refused).
describe('API.revertMatchToQueue', () => {
  let originalFetch;
  beforeEach(() => { originalFetch = global.fetch; });
  afterEach(() => { global.fetch = originalFetch; });

  it('POSTs to the revert-to-queue URL with the password header and a JSON body', async () => {
    global.fetch = mockFetch(200, {});
    const res = await API.revertMatchToQueue('c42', 'm-r1-0', 'secret');
    expect(res.pending).toBe(true);
    await expect(res.settled).resolves.toBe(true);
    const [url, opts] = global.fetch.mock.calls.find(([u]) => String(u).includes('/revert-to-queue'));
    expect(url).toBe('/api/competitions/c42/matches/m-r1-0/revert-to-queue');
    expect(opts.method).toBe('POST');
    expect(opts.headers['X-Tournament-Password']).toBe('secret');
    expect(opts.headers['Content-Type']).toBe('application/json');
    expect(typeof JSON.parse(opts.body).writeId).toBe('string');
  });

  it('rejects with the server error message on a 409 (completed match)', async () => {
    global.fetch = mockFetch(409, { error: 'match_completed', message: 'match already completed: use the score editor to correct a completed bout' });
    const res = await API.revertMatchToQueue('c1', 'm1', 'secret');
    await expect(res.settled).rejects.toThrow(/already completed/);
    expectWarned();
  });

  it('rejects with a fallback message when the error body has no error field', async () => {
    global.fetch = mockFetch(400, {});
    const res = await API.revertMatchToQueue('c1', 'm2', 'secret');
    await expect(res.settled).rejects.toThrow('Failed to send match back to queue');
    expectWarned();
  });
});
