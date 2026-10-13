// bc-cmpw: the separate destructive-ops password is gone. Every organiser
// write is authorised by the tournament password alone, so no request may
// carry X-Admin-Password, and the client no longer offers a way to set one.
import { describe, it, expect, vi, afterEach } from 'vitest';
import { API } from '../api_client.jsx';
import * as adminHelpers from '../admin_helpers.jsx';

describe('organiser writes send the tournament password only', () => {
  afterEach(() => { vi.restoreAllMocks(); });

  const writes = [
    ['deleteCompetition', () => API.deleteCompetition('c1', 'main'), 'DELETE', '/api/competitions/c1'],
    ['invalidateCompetition', () => API.invalidateCompetition('c1', 'main'), 'POST', '/api/competitions/c1/invalidate'],
    ['discardDraw', () => API.discardDraw('c1', 'main'), 'DELETE', '/api/competitions/c1/draw'],
    ['quarantineBracket', () => API.quarantineBracket('c1', 'main'), 'POST', '/api/competitions/c1/bracket/quarantine'],
    ['completeCompetition', () => API.completeCompetition('c1', 'main'), 'POST', '/api/competitions/c1/complete'],
    ['resetOverrides', () => API.resetOverrides('c1', 'main'), 'DELETE', '/api/competitions/c1/overrides'],
    ['addParticipant', () => API.addParticipant('c1', { name: 'X', dojo: 'D' }, 'main'), 'POST', '/api/competitions/c1/participants'],
    ['replaceParticipant', () => API.replaceParticipant('c1', 'p1', { name: 'X', dojo: 'D' }, 'main'), 'PUT', '/api/competitions/c1/participants/p1'],
    ['updateCompetitionAwards', () => API.updateCompetitionAwards('c1', [], 'main'), 'PUT', '/api/competitions/c1/awards'],
    ['updateCompetition (roster-bearing)', () => API.updateCompetition('c1', { id: 'c1', players: [{ name: 'A', dojo: 'D' }] }, 'main'), 'PUT', '/api/competitions/c1'],
    ['importCompetitions', () => API.importCompetitions(new FormData(), 'main'), 'POST', '/api/tournament/import'],
  ];

  writes.forEach(([label, call, method, url]) => {
    it(`${label} sends X-Tournament-Password and no X-Admin-Password`, async () => {
      global.fetch = vi.fn().mockResolvedValue({ ok: true, status: 200, headers: { get: () => null }, json: async () => ({}) });
      await call();
      expect(global.fetch).toHaveBeenCalledTimes(1);
      const [sentUrl, opts] = global.fetch.mock.calls[0];
      expect(sentUrl).toBe(url);
      expect(opts.method).toBe(method);
      expect(opts.headers['X-Tournament-Password']).toBe('main');
      expect(opts.headers).not.toHaveProperty('X-Admin-Password');
    });
  });

  it('a stale trailing argument from an older caller is never sent as a header', async () => {
    global.fetch = vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => ({}) });
    await API.invalidateCompetition('c1', 'main', 'old-admin-pw');
    expect(global.fetch.mock.calls[0][1].headers).not.toHaveProperty('X-Admin-Password');
  });

  it('offers no way to set an admin password or prompt for one', () => {
    expect(API.setAdminPassword).toBeUndefined();
    expect(adminHelpers.promptAdminPassword).toBeUndefined();
    expect(adminHelpers.setCachedAuthConfig).toBeUndefined();
    expect(adminHelpers.getCachedAuthConfig).toBeUndefined();
  });
});
