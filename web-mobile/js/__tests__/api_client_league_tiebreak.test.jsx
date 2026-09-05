// Tests for API.leagueTiebreakGenerate / API.leagueTiebreakRemove
// (second-Opus-pass nit 7): teamIds must be sent alongside teamNames when
// the caller has one (so a namesake-holding tied group can be selected and
// removed at all, not just created via curl), and OMITTED entirely --
// never sent as an empty array or padded with blanks -- when the caller has
// none, since the server now rejects a blank teamIds entry outright
// (second-Opus-pass item 4).

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

  it('omits teamIds entirely when the caller passes none (legacy id-less group)', async () => {
    global.fetch = mockFetch(201, { matches: [] });
    await API.leagueTiebreakGenerate('c1', ['Team A', 'Team B'], 'secret');
    const [, opts] = global.fetch.mock.calls[0];
    const body = JSON.parse(opts.body);
    expect(body).toEqual({ teamNames: ['Team A', 'Team B'] });
    expect(body).not.toHaveProperty('teamIds');
  });

  it('omits teamIds when the caller passes an empty array', async () => {
    global.fetch = mockFetch(201, { matches: [] });
    await API.leagueTiebreakGenerate('c1', ['Team A', 'Team B'], 'secret', []);
    const [, opts] = global.fetch.mock.calls[0];
    expect(JSON.parse(opts.body)).not.toHaveProperty('teamIds');
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

  it('omits teamIds entirely when the caller passes none (legacy id-less group)', async () => {
    global.fetch = mockFetch(200, { deleted: 1 });
    await API.leagueTiebreakRemove('c1', ['Team A', 'Team B'], 'secret');
    const [, opts] = global.fetch.mock.calls[0];
    const body = JSON.parse(opts.body);
    expect(body).toEqual({ teamNames: ['Team A', 'Team B'] });
    expect(body).not.toHaveProperty('teamIds');
  });
});
