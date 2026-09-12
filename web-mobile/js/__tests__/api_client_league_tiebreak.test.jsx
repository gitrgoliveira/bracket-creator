// Tests for API.leagueTiebreakGenerate / API.leagueTiebreakRemove.
// teamIds is REQUIRED by the server now (operator ruling bc-pnum: the tied
// group is selected by id only), so the client sends whatever it is given
// verbatim -- the caller (admin_pools.jsx's groupTeamIds + the disabled
// "Run tie-breaker" / "Remove unscored tie-breaker" buttons) is responsible
// for never invoking these with a missing or incomplete teamIds; these
// functions no longer special-case an empty/absent array. Passing no
// teamIds argument at all still omits the key (JSON.stringify drops an
// undefined property), which is what a caller that never learned about the
// parameter produces; it is not a supported "safe to omit" path.

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

describe('API.leagueTiebreakGenerate', () => {
  let originalFetch;
  beforeEach(() => { originalFetch = global.fetch; });
  afterEach(() => { global.fetch = originalFetch; });

  it('includes teamIds in the body when the caller passes a non-empty array', async () => {
    global.fetch = mockFetch(201, { matches: [] });
    await API.leagueTiebreakGenerate('c1', ['Team X', 'Team X'], 'secret', ['id-a', 'id-b']);
    const [url, opts] = global.fetch.mock.calls[0];
    expect(url).toBe('/api/competitions/c1/league-tiebreak');
    expect(opts.method).toBe('POST');
    expect(JSON.parse(opts.body)).toEqual({ teamNames: ['Team X', 'Team X'], teamIds: ['id-a', 'id-b'] });
  });

  it('a caller passing no teamIds argument at all sends no teamIds key (JSON.stringify drops undefined)', async () => {
    global.fetch = mockFetch(201, { matches: [] });
    await API.leagueTiebreakGenerate('c1', ['Team A', 'Team B'], 'secret');
    const [, opts] = global.fetch.mock.calls[0];
    const body = JSON.parse(opts.body);
    expect(body).toEqual({ teamNames: ['Team A', 'Team B'] });
    expect(body).not.toHaveProperty('teamIds');
  });

  it('sends an empty teamIds array through verbatim (the server, not this function, rejects it)', async () => {
    global.fetch = mockFetch(201, { matches: [] });
    await API.leagueTiebreakGenerate('c1', ['Team A', 'Team B'], 'secret', []);
    const [, opts] = global.fetch.mock.calls[0];
    expect(JSON.parse(opts.body)).toEqual({ teamNames: ['Team A', 'Team B'], teamIds: [] });
  });
});

describe('API.leagueTiebreakRemove', () => {
  let originalFetch;
  beforeEach(() => { originalFetch = global.fetch; });
  afterEach(() => { global.fetch = originalFetch; });

  it('includes teamIds in the body when the caller passes a non-empty array', async () => {
    global.fetch = mockFetch(200, { deleted: 1 });
    await API.leagueTiebreakRemove('c1', ['Team X', 'Team X'], 'secret', ['id-a', 'id-b']);
    const [url, opts] = global.fetch.mock.calls[0];
    expect(url).toBe('/api/competitions/c1/league-tiebreak');
    expect(opts.method).toBe('DELETE');
    expect(JSON.parse(opts.body)).toEqual({ teamNames: ['Team X', 'Team X'], teamIds: ['id-a', 'id-b'] });
  });

  it('a caller passing no teamIds argument at all sends no teamIds key (JSON.stringify drops undefined)', async () => {
    global.fetch = mockFetch(200, { deleted: 1 });
    await API.leagueTiebreakRemove('c1', ['Team A', 'Team B'], 'secret');
    const [, opts] = global.fetch.mock.calls[0];
    const body = JSON.parse(opts.body);
    expect(body).toEqual({ teamNames: ['Team A', 'Team B'] });
    expect(body).not.toHaveProperty('teamIds');
  });

  it('sends an empty teamIds array through verbatim (the server, not this function, rejects it)', async () => {
    global.fetch = mockFetch(200, { deleted: 1 });
    await API.leagueTiebreakRemove('c1', ['Team A', 'Team B'], 'secret', []);
    const [, opts] = global.fetch.mock.calls[0];
    expect(JSON.parse(opts.body)).toEqual({ teamNames: ['Team A', 'Team B'], teamIds: [] });
  });
});
