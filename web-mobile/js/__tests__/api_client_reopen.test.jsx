// Tests for API.reopenMatch (mp-gmcg). Reopening a completed kachinuki
// encounter DISCARDS a recorded result, so the server requires a non-empty
// audit `reason` in the body and 400s without one. Pin the URL/method/headers,
// the JSON body carrying that reason, and the verbatim error surfacing the
// editor relies on (the server's 409s are full sentences the operator reads
// unchanged, including the court-busy one).

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

describe('API.reopenMatch', () => {
  let originalFetch;
  beforeEach(() => { originalFetch = global.fetch; });
  afterEach(() => { global.fetch = originalFetch; });

  it('POSTs the audit reason as JSON with the password header', async () => {
    global.fetch = mockFetch(200, {});
    const ok = await API.reopenMatch('c42', 'm-r1-0', 'Ended too early', 'secret');
    expect(ok).toBe(true);
    const [url, opts] = global.fetch.mock.calls[0];
    expect(url).toBe('/api/competitions/c42/matches/m-r1-0/reopen');
    expect(opts.method).toBe('POST');
    expect(opts.headers['X-Tournament-Password']).toBe('secret');
    // Without the JSON content type the server cannot bind the body at all.
    expect(opts.headers['Content-Type']).toBe('application/json');
    expect(JSON.parse(opts.body)).toEqual({ reason: 'Ended too early' });
  });

  it('surfaces the court-busy 409 sentence, not the machine code', async () => {
    // The court-busy conflict is new (mp-gmcg review): reopening would put a
    // second live match on the court. It reuses the score path's STRUCTURED
    // payload, where `error` is the code and the sentence is in `message`.
    // Throwing err.error here would show the operator a bare "court_busy".
    global.fetch = mockFetch(409, {
      error: 'court_busy',
      court: 'A',
      matchId: 'm-r1-1',
      compId: 'c1',
      message: 'Court A already has a running match (m-r1-1). Finish that match before reopening this one.',
    });
    await expect(API.reopenMatch('c1', 'm1', 'Scoring error', 'secret')).rejects.toThrow(
      'Court A already has a running match (m-r1-1). Finish that match before reopening this one.'
    );
  });

  it('surfaces the plain-sentence 409s (not completed, downstream fought) verbatim', async () => {
    global.fetch = mockFetch(409, { error: 'cannot reopen: a downstream knockout match has already been fought' });
    await expect(API.reopenMatch('c1', 'm1', 'Scoring error', 'secret'))
      .rejects.toThrow('cannot reopen: a downstream knockout match has already been fought');
  });

  it('surfaces the server 400 verbatim when the reason is rejected', async () => {
    global.fetch = mockFetch(400, { error: 'reason is required: reopening a finalized result must be justified' });
    await expect(API.reopenMatch('c1', 'm1', '   ', 'secret'))
      .rejects.toThrow('reason is required: reopening a finalized result must be justified');
  });

  it('throws a fallback message when the error body has no error field', async () => {
    global.fetch = mockFetch(500, {});
    await expect(API.reopenMatch('c1', 'm1', 'Scoring error', 'secret'))
      .rejects.toThrow('Failed to reopen match');
  });
});
