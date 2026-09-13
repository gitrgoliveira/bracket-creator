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

// bc-pnum gap closure: putMatchLineup grows an optional trailing memberIds
// argument, mirroring putTeamLineup's own treatment exactly (see that
// function's comment in api_client.jsx). These three cases are the ones
// that matter: sent when given, omitted (not just falsy/undefined) when
// not given so an old caller's wire body is byte-identical, and carried
// into the offline queue too (bc-pnum: a client on unreliable venue wifi
// must not silently lose the ids on reconnect replay).
describe('API.putMatchLineup: memberIds (bc-pnum gap closure)', () => {
  let originalFetch;
  beforeEach(() => {
    originalFetch = global.fetch;
    localStorage.removeItem('bc_write_queue');
  });
  afterEach(async () => {
    global.fetch = originalFetch;
    API.clearQueue();
  });

  it('sends memberIds in the body when provided', async () => {
    global.fetch = mockFetch(200, { teamId: 't1', matchId: 'm1', positions: { senpo: 'Bob' }, memberIds: { senpo: 'mem-1' } });
    await API.putMatchLineup('c1', 't1', 'm1', { senpo: 'Bob' }, 'pw', { senpo: 'mem-1' });
    const [, opts] = global.fetch.mock.calls[0];
    const body = JSON.parse(opts.body);
    expect(body.memberIds).toEqual({ senpo: 'mem-1' });
  });

  it('omits the memberIds key entirely when not provided (byte-identical old body)', async () => {
    global.fetch = mockFetch(200, { teamId: 't1', matchId: 'm1', positions: { senpo: 'Bob' } });
    await API.putMatchLineup('c1', 't1', 'm1', { senpo: 'Bob' }, 'pw');
    const [, opts] = global.fetch.mock.calls[0];
    const body = JSON.parse(opts.body);
    expect(body).toEqual({ teamId: 't1', competitionId: 'c1', matchId: 'm1', positions: { senpo: 'Bob' } });
    expect('memberIds' in body).toBe(false);
  });

  it('carries memberIds into the offline-queued body on network failure', async () => {
    global.fetch = vi.fn(() => Promise.reject(new TypeError('network error')));
    const result = await API.putMatchLineup('c1', 't-queue', 'm-queue', { senpo: 'Bob' }, 'pw', { senpo: 'mem-9' });
    expect(result).toEqual({ queued: true });
    const raw = localStorage.getItem('bc_write_queue');
    expect(raw).not.toBeNull();
    const entries = JSON.parse(raw);
    const found = entries.find(([key]) => key.includes('m-queue'));
    expect(found).toBeTruthy();
    expect(found[1].payload.memberIds).toEqual({ senpo: 'mem-9' });
    // Drain the immediate background retry api_client fires on enqueue (it
    // also fails against this same rejecting mock), then cancel any
    // resulting backoff timer so it cannot fire during a LATER test in this
    // file: this file does not use fake timers, unlike sync_queue.test.jsx.
    await Promise.resolve(); await Promise.resolve(); await Promise.resolve();
    API.clearQueue();
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
