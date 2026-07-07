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

describe('API.revertMatchToQueue', () => {
  let originalFetch;
  beforeEach(() => { originalFetch = global.fetch; });
  afterEach(() => { global.fetch = originalFetch; });

  it('POSTs to the revert-to-queue URL with the password header', async () => {
    global.fetch = mockFetch(200, {});
    const ok = await API.revertMatchToQueue('c42', 'm-r1-0', 'secret');
    expect(ok).toBe(true);
    const [url, opts] = global.fetch.mock.calls[0];
    expect(url).toBe('/api/competitions/c42/matches/m-r1-0/revert-to-queue');
    expect(opts.method).toBe('POST');
    expect(opts.headers['X-Tournament-Password']).toBe('secret');
  });

  it('throws the server error message on a 409 (completed match)', async () => {
    global.fetch = mockFetch(409, { error: 'match already completed: use the score editor to correct a completed bout' });
    await expect(API.revertMatchToQueue('c1', 'm1', 'secret')).rejects.toThrow(/already completed/);
  });

  it('throws a fallback message when the error body has no error field', async () => {
    global.fetch = mockFetch(500, {});
    await expect(API.revertMatchToQueue('c1', 'm1', 'secret')).rejects.toThrow('Failed to send match back to queue');
  });
});
