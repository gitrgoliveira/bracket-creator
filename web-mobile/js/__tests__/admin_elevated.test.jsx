// Spec 004 / mp-e21 : elevated (destructive-ops) password: client header
// plumbing + the promptAdminPassword UI helper.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { API } from '../api_client.jsx';
import { promptAdminPassword, setCachedAuthConfig, getCachedAuthConfig } from '../admin_helpers.jsx';

describe('API elevated-password header (X-Admin-Password)', () => {
  afterEach(() => { vi.restoreAllMocks(); });

  // Each gated method must send X-Admin-Password when an adminPassword arg is
  // given, alongside the existing X-Tournament-Password.
  const gated = [
    ['deleteCompetition', () => API.deleteCompetition('c1', 'main', 'adminpw'), 'DELETE', '/api/competitions/c1'],
    ['invalidateCompetition', () => API.invalidateCompetition('c1', 'main', 'adminpw'), 'POST', '/api/competitions/c1/invalidate'],
    ['discardDraw', () => API.discardDraw('c1', 'main', 'adminpw'), 'DELETE', '/api/competitions/c1/draw'],
    ['resetOverrides', () => API.resetOverrides('c1', 'main', 'adminpw'), 'DELETE', '/api/competitions/c1/overrides'],
    ['addParticipant', () => API.addParticipant('c1', { name: 'X', dojo: 'D' }, 'main', 'adminpw'), 'POST', '/api/competitions/c1/participants'],
    ['replaceParticipant', () => API.replaceParticipant('c1', 'p1', { name: 'X', dojo: 'D' }, 'main', 'adminpw'), 'PUT', '/api/competitions/c1/participants/p1'],
    ['updateCompetitionAwards', () => API.updateCompetitionAwards('c1', [], 'main', 'adminpw'), 'PUT', '/api/competitions/c1/awards'],
  ];

  gated.forEach(([label, call, method, url]) => {
    it(`${label} sends X-Admin-Password when provided`, async () => {
      global.fetch = vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => ({}) });
      await call();
      expect(global.fetch).toHaveBeenCalledWith(
        url,
        expect.objectContaining({
          method,
          headers: expect.objectContaining({
            'X-Tournament-Password': 'main',
            'X-Admin-Password': 'adminpw',
          }),
        })
      );
    });
  });

  it('omits X-Admin-Password when no admin password is supplied (gate inactive)', async () => {
    global.fetch = vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => ({}) });
    await API.deleteCompetition('c1', 'main', '');
    const sentHeaders = global.fetch.mock.calls[0][1].headers;
    expect(sentHeaders).not.toHaveProperty('X-Admin-Password');
    expect(sentHeaders['X-Tournament-Password']).toBe('main');
  });

  // Copilot PR #193: the bulk PUT /competitions/:id is the SPA's primary
  // roster writer and must carry X-Admin-Password when it bears a roster.
  it('updateCompetition forwards X-Admin-Password (roster-bearing)', async () => {
    global.fetch = vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => ({}) });
    await API.updateCompetition('c1', { id: 'c1', players: [{ name: 'A', dojo: 'D' }] }, 'main', 'adminpw');
    const [url, opts] = global.fetch.mock.calls[0];
    expect(url).toBe('/api/competitions/c1');
    expect(opts.method).toBe('PUT');
    expect(opts.headers).toMatchObject({ 'X-Tournament-Password': 'main', 'X-Admin-Password': 'adminpw' });
  });

  it('updateCompetition omits X-Admin-Password for settings-only saves', async () => {
    global.fetch = vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => ({}) });
    await API.updateCompetition('c1', { id: 'c1', name: 'Renamed' }, 'main', '');
    expect(global.fetch.mock.calls[0][1].headers).not.toHaveProperty('X-Admin-Password');
  });

  it('importCompetitions forwards X-Admin-Password', async () => {
    global.fetch = vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) });
    await API.importCompetitions(new FormData(), 'main', 'adminpw');
    expect(global.fetch.mock.calls[0][1].headers).toMatchObject({
      'X-Tournament-Password': 'main',
      'X-Admin-Password': 'adminpw',
    });
  });

  describe('setAdminPassword', () => {
    it('sends newPassword + currentPassword with the main password header', async () => {
      global.fetch = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ status: 'ok' }) });
      await API.setAdminPassword('newpw', 'oldpw', 'main');
      const [url, opts] = global.fetch.mock.calls[0];
      expect(url).toBe('/api/auth/admin-password');
      expect(opts.method).toBe('PUT');
      expect(opts.headers['X-Tournament-Password']).toBe('main');
      expect(JSON.parse(opts.body)).toEqual({ newPassword: 'newpw', currentPassword: 'oldpw' });
    });

    it('omits currentPassword on first-time set (TOFU)', async () => {
      global.fetch = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ status: 'ok' }) });
      await API.setAdminPassword('newpw', '', 'main');
      expect(JSON.parse(global.fetch.mock.calls[0][1].body)).toEqual({ newPassword: 'newpw' });
    });

    it('throws with status on failure', async () => {
      global.fetch = vi.fn().mockResolvedValue({ ok: false, status: 401, json: async () => ({ error: 'current admin password is incorrect' }) });
      await expect(API.setAdminPassword('a', 'b', 'main')).rejects.toThrow('current admin password is incorrect');
    });
  });
});

describe('promptAdminPassword', () => {
  // promptAdminPassword is now async and collects the password via the shared
  // window.promptDialog primitive (themed, masked input) instead of the native
  // window.prompt. ui.jsx isn't imported here, so we install a window.promptDialog
  // mock per test and assert against it.
  beforeEach(() => { setCachedAuthConfig(null); window.promptDialog = vi.fn(); });
  afterEach(() => { vi.restoreAllMocks(); setCachedAuthConfig(null); delete window.promptDialog; });

  it('returns "" (no dialog) when the gate is inactive', async () => {
    setCachedAuthConfig({ mode: 'file', elevatedRequired: false });
    await expect(promptAdminPassword()).resolves.toBe('');
    expect(window.promptDialog).not.toHaveBeenCalled();
  });

  it('returns "" when authConfig is unset (loading / fail-open)', async () => {
    await expect(promptAdminPassword()).resolves.toBe('');
    expect(window.promptDialog).not.toHaveBeenCalled();
  });

  it('alerts and returns null when required but not configured (locked, env unset)', async () => {
    setCachedAuthConfig({ mode: 'locked', elevatedRequired: true, elevatedConfigured: false });
    const alertSpy = vi.spyOn(window, 'alert').mockImplementation(() => {});
    await expect(promptAdminPassword()).resolves.toBeNull();
    expect(alertSpy).toHaveBeenCalled();
    expect(window.promptDialog).not.toHaveBeenCalled();
  });

  it('prompts and returns the typed password when required + configured', async () => {
    setCachedAuthConfig({ mode: 'file', elevatedRequired: true, elevatedConfigured: true });
    window.promptDialog.mockResolvedValue('typed-secret');
    await expect(promptAdminPassword()).resolves.toBe('typed-secret');
    expect(window.promptDialog).toHaveBeenCalledWith(expect.objectContaining({ password: true }));
  });

  it('returns null when the operator cancels the dialog', async () => {
    setCachedAuthConfig({ mode: 'file', elevatedRequired: true, elevatedConfigured: true });
    window.promptDialog.mockResolvedValue(null);
    await expect(promptAdminPassword()).resolves.toBeNull();
  });

  it('returns null on empty submit (empty is never a valid admin password)', async () => {
    setCachedAuthConfig({ mode: 'file', elevatedRequired: true, elevatedConfigured: true });
    window.promptDialog.mockResolvedValue('');
    await expect(promptAdminPassword()).resolves.toBeNull();
  });

  it('cache is shared via window across import sites', () => {
    setCachedAuthConfig({ mode: 'file', elevatedRequired: true, elevatedConfigured: true });
    expect(getCachedAuthConfig()).toMatchObject({ elevatedRequired: true });
    // window-backed so a separately-bundled reader sees the same value.
    expect(window.__bcAuthConfig).toMatchObject({ elevatedRequired: true });
  });
});
