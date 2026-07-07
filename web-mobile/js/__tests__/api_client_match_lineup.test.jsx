// mp-bkg: tests for the three matchId-keyed lineup API helpers
// (fetchMatchLineup, putMatchLineup, deleteMatchLineup) in api_client.jsx.
// These mirror the round-scoped helpers; same 404/error handling, just
// targeting a different endpoint path (/match-lineups/:matchId vs /lineups/:round).

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { API } from '../api_client.jsx';

// Minimal fetch stub that records the most recent call.
function mockFetch(status, body) {
  return vi.fn(() =>
    Promise.resolve({
      ok: status >= 200 && status < 300,
      status,
      json: () => Promise.resolve(body),
    })
  );
}

describe('API.fetchMatchLineup', () => {
  let originalFetch;
  beforeEach(() => { originalFetch = global.fetch; });
  afterEach(() => { global.fetch = originalFetch; });

  it('returns null on 404 (no lineup saved yet)', async () => {
    global.fetch = mockFetch(404, { error: 'not found' });
    const result = await API.fetchMatchLineup('comp1', 'team1', 'match1');
    expect(result).toBeNull();
  });

  it('returns parsed body on 200', async () => {
    const lineup = { teamId: 'team1', matchId: 'match1', positions: { senpo: 'Alice' } };
    global.fetch = mockFetch(200, lineup);
    const result = await API.fetchMatchLineup('comp1', 'team1', 'match1');
    expect(result).toEqual(lineup);
  });

  it('calls the correct /match-lineups/:matchId URL', async () => {
    global.fetch = mockFetch(200, {});
    await API.fetchMatchLineup('c42', 't99', 'mx7');
    const [url] = global.fetch.mock.calls[0];
    expect(url).toBe('/api/competitions/c42/teams/t99/match-lineups/mx7');
  });

  it('throws on non-404 error responses', async () => {
    global.fetch = mockFetch(500, { error: 'internal' });
    await expect(API.fetchMatchLineup('c1', 't1', 'm1')).rejects.toThrow('internal');
  });
});

describe('API.putMatchLineup', () => {
  let originalFetch;
  beforeEach(() => { originalFetch = global.fetch; });
  afterEach(() => { global.fetch = originalFetch; });

  it('sends PUT to the correct /match-lineups/:matchId URL', async () => {
    const saved = { teamId: 't1', matchId: 'm1', positions: { senpo: 'Bob' } };
    global.fetch = mockFetch(200, saved);
    const result = await API.putMatchLineup('c1', 't1', 'm1', { senpo: 'Bob' }, 'pw');
    const [url, opts] = global.fetch.mock.calls[0];
    expect(url).toBe('/api/competitions/c1/teams/t1/match-lineups/m1');
    expect(opts.method).toBe('PUT');
    expect(opts.headers['X-Tournament-Password']).toBe('pw');
    expect(JSON.parse(opts.body)).toMatchObject({ teamId: 't1', matchId: 'm1', positions: { senpo: 'Bob' } });
    expect(result).toEqual(saved);
  });

  it('throws on 400 with server message', async () => {
    global.fetch = mockFetch(400, { error: 'missing senpo' });
    await expect(API.putMatchLineup('c1', 't1', 'm1', {}, 'pw'))
      .rejects.toThrow('missing senpo');
  });
});

describe('API.deleteMatchLineup', () => {
  let originalFetch;
  beforeEach(() => { originalFetch = global.fetch; });
  afterEach(() => { global.fetch = originalFetch; });

  it('sends DELETE to the correct URL and returns true on 204', async () => {
    global.fetch = vi.fn(() => Promise.resolve({ ok: true, status: 204 }));
    const result = await API.deleteMatchLineup('c1', 't1', 'm1', 'pw');
    const [url, opts] = global.fetch.mock.calls[0];
    expect(url).toBe('/api/competitions/c1/teams/t1/match-lineups/m1');
    expect(opts.method).toBe('DELETE');
    expect(opts.headers['X-Tournament-Password']).toBe('pw');
    expect(result).toBe(true);
  });

  it('returns true on 404 (idempotent delete)', async () => {
    global.fetch = vi.fn(() => Promise.resolve({ ok: false, status: 404 }));
    const result = await API.deleteMatchLineup('c1', 't1', 'm1', 'pw');
    expect(result).toBe(true);
  });

  it('throws on non-404 error responses', async () => {
    global.fetch = vi.fn(() =>
      Promise.resolve({
        ok: false,
        status: 500,
        json: () => Promise.resolve({ error: 'internal error' }),
      })
    );
    await expect(API.deleteMatchLineup('c1', 't1', 'm1', 'pw'))
      .rejects.toThrow('internal error');
  });
});
