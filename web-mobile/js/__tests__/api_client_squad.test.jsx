// bc-tmid pass 3: tests for the squad API helpers in api_client.jsx
// (fetchSquads) and the memberIds half of
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

  it('returns the team members map on 200', async () => {
    const squads = { 'team-1': [{ id: 'm1', index: 1, name: 'Sato' }] };
    global.fetch = mockFetch(200, { teamMembers: squads });
    const result = await API.fetchSquads('c1', 'pw');
    expect(result).toEqual(squads);
  });

  it('calls GET /api/competitions/:id/team-members with the password header', async () => {
    global.fetch = mockFetch(200, { teamMembers: {} });
    await API.fetchSquads('c42', 'secret');
    const [url, opts] = global.fetch.mock.calls[0];
    expect(url).toBe('/api/competitions/c42/team-members');
    expect(opts.headers['X-Tournament-Password']).toBe('secret');
  });

  it('returns {} when the response carries no teamMembers key', async () => {
    global.fetch = mockFetch(200, {});
    const result = await API.fetchSquads('c1', 'pw');
    expect(result).toEqual({});
  });

  it('throws on a non-OK response', async () => {
    global.fetch = mockFetch(404, { error: 'competition not found' });
    await expect(API.fetchSquads('c1', 'pw')).rejects.toThrow('competition not found');
  });
});

// The member writes (addTeamMember, renameTeamMember, clearTeamMember) are
// outbox entries since bc-evts stage 3 review 2 (R1): pinned in
// member_writes_outbox.test.jsx.

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
