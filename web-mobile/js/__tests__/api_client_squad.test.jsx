// bc-tmid pass 3: tests for the squad API helpers in api_client.jsx
// (fetchSquads, addTeamMember, renameTeamMember) and the memberIds half of
// putTeamLineup. These pin the wire contract directly (URL, method,
// headers, body shape) independent of admin_lineup_form.test.jsx, which
// mocks window.API entirely and so never exercises api_client.jsx itself.

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

describe('API.fetchSquads', () => {
  let originalFetch;
  beforeEach(() => { originalFetch = global.fetch; });
  afterEach(() => { global.fetch = originalFetch; });

  it('returns the squads map on 200', async () => {
    const squads = { 'team-1': [{ id: 'm1', index: 1, name: 'Sato' }] };
    global.fetch = mockFetch(200, { squads });
    const result = await API.fetchSquads('c1', 'pw');
    expect(result).toEqual(squads);
  });

  it('calls GET /api/competitions/:id/squads with the password header', async () => {
    global.fetch = mockFetch(200, { squads: {} });
    await API.fetchSquads('c42', 'secret');
    const [url, opts] = global.fetch.mock.calls[0];
    expect(url).toBe('/api/competitions/c42/squads');
    expect(opts.headers['X-Tournament-Password']).toBe('secret');
  });

  it('returns {} when the response carries no squads key', async () => {
    global.fetch = mockFetch(200, {});
    const result = await API.fetchSquads('c1', 'pw');
    expect(result).toEqual({});
  });

  it('throws on a non-OK response', async () => {
    global.fetch = mockFetch(404, { error: 'competition not found' });
    await expect(API.fetchSquads('c1', 'pw')).rejects.toThrow('competition not found');
  });
});

describe('API.addTeamMember', () => {
  let originalFetch;
  beforeEach(() => { originalFetch = global.fetch; });
  afterEach(() => { global.fetch = originalFetch; });

  it('POSTs the name and returns the minted member', async () => {
    const created = { id: 'm2', index: 2, name: 'Ito' };
    global.fetch = mockFetch(201, created);
    const result = await API.addTeamMember('c1', 'team-1', 'Ito', 'pw');
    const [url, opts] = global.fetch.mock.calls[0];
    expect(url).toBe('/api/competitions/c1/teams/team-1/members');
    expect(opts.method).toBe('POST');
    expect(opts.headers['X-Tournament-Password']).toBe('pw');
    expect(JSON.parse(opts.body)).toEqual({ name: 'Ito' });
    expect(result).toEqual(created);
  });

  it('throws with the server message on a duplicate-name 409', async () => {
    global.fetch = mockFetch(409, { error: 'team "team-1" already has a member named "Ito"' });
    await expect(API.addTeamMember('c1', 'team-1', 'Ito', 'pw')).rejects.toThrow(/already has a member/);
  });
});

describe('API.renameTeamMember', () => {
  let originalFetch;
  beforeEach(() => { originalFetch = global.fetch; });
  afterEach(() => { global.fetch = originalFetch; });

  it('PUTs the new name to the member endpoint and resolves true on 204', async () => {
    global.fetch = vi.fn(() => Promise.resolve({ ok: true, status: 204 }));
    const result = await API.renameTeamMember('c1', 'team-1', 'm1', 'Sato-Renamed', 'pw');
    const [url, opts] = global.fetch.mock.calls[0];
    expect(url).toBe('/api/competitions/c1/teams/team-1/members/m1');
    expect(opts.method).toBe('PUT');
    expect(JSON.parse(opts.body)).toEqual({ name: 'Sato-Renamed' });
    expect(result).toBe(true);
  });

  it('throws on a 404 (member id does not resolve)', async () => {
    global.fetch = mockFetch(404, { error: 'team member not found' });
    await expect(API.renameTeamMember('c1', 'team-1', 'no-such-id', 'X', 'pw')).rejects.toThrow('team member not found');
  });
});

describe('API.clearTeamMember', () => {
  let originalFetch;
  beforeEach(() => { originalFetch = global.fetch; });
  afterEach(() => { global.fetch = originalFetch; });

  it('DELETEs the member endpoint and resolves true on 204', async () => {
    global.fetch = vi.fn(() => Promise.resolve({ ok: true, status: 204 }));
    const result = await API.clearTeamMember('c1', 'team-1', 'm1', 'pw');
    const [url, opts] = global.fetch.mock.calls[0];
    expect(url).toBe('/api/competitions/c1/teams/team-1/members/m1');
    expect(opts.method).toBe('DELETE');
    expect(opts.headers['X-Tournament-Password']).toBe('pw');
    expect(result).toBe(true);
  });

  it('throws with the server message on a 409 once the competition has started', async () => {
    global.fetch = mockFetch(409, { error: 'cannot clear a team member\'s name once the competition has started' });
    await expect(API.clearTeamMember('c1', 'team-1', 'm1', 'pw')).rejects.toThrow(/once the competition has started/);
  });

  it('throws on a 404 (member id does not resolve)', async () => {
    global.fetch = mockFetch(404, { error: 'team member not found' });
    await expect(API.clearTeamMember('c1', 'team-1', 'no-such-id', 'pw')).rejects.toThrow('team member not found');
  });
});

describe('API.putTeamLineup memberIds', () => {
  let originalFetch;
  beforeEach(() => { originalFetch = global.fetch; });
  afterEach(() => { global.fetch = originalFetch; });

  it('includes memberIds in the request body when provided', async () => {
    global.fetch = mockFetch(200, {});
    await API.putTeamLineup('c1', 'team-1', 0, { senpo: 'Sato' }, 'pw', { senpo: 'm1' });
    const [, opts] = global.fetch.mock.calls[0];
    const body = JSON.parse(opts.body);
    expect(body.memberIds).toEqual({ senpo: 'm1' });
  });

  it('omits memberIds from the request body when not provided (older-caller compatibility)', async () => {
    global.fetch = mockFetch(200, {});
    await API.putTeamLineup('c1', 'team-1', 0, { senpo: 'Sato' }, 'pw');
    const [, opts] = global.fetch.mock.calls[0];
    const body = JSON.parse(opts.body);
    expect(body).not.toHaveProperty('memberIds');
  });
});
