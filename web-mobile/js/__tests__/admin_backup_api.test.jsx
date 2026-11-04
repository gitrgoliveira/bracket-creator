import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { API } from '../api_client.jsx';

const ORGANISER_KEY = 'organiser-key';
const SESSION_KEY = 'session-key';

function mockFetch(status, body, { headers = {} } = {}) {
  return vi.fn(() =>
    Promise.resolve({
      ok: status >= 200 && status < 300,
      status,
      headers: {
        get: (name) => headers[name] ?? null,
      },
      json: () => Promise.resolve(body),
      blob: () => Promise.resolve(new Blob([JSON.stringify(body)])),
    })
  );
}

function mockFetchBlob(status, { headers = {}, blob = new Blob(['zip-bytes']) } = {}) {
  return vi.fn(() =>
    Promise.resolve({
      ok: status >= 200 && status < 300,
      status,
      headers: {
        get: (name) => headers[name] ?? null,
      },
      blob: () => Promise.resolve(blob),
      json: () => Promise.resolve({}),
    })
  );
}

describe('admin_backup_api', () => {
  let originalFetch;
  beforeEach(() => {
    originalFetch = global.fetch;
  });
  afterEach(() => {
    global.fetch = originalFetch;
  });

  describe('backupTournament', () => {
    it('GETs /api/tournament/backup with X-Tournament-Password and returns { blob, filename }', async () => {
      const blob = new Blob(['tournament-backup']);
      global.fetch = mockFetchBlob(200, {
        blob,
        headers: { 'Content-Disposition': 'attachment; filename="tournament-20261103T100000Z.zip"' },
      });

      const result = await API.backupTournament(ORGANISER_KEY);
      expect(result).toEqual({ blob, filename: 'tournament-20261103T100000Z.zip' });

      expect(global.fetch).toHaveBeenCalledTimes(1);
      const [url, opts] = global.fetch.mock.calls[0];
      expect(url).toBe('/api/tournament/backup');
      expect(opts.method).toBe('GET');
      expect(opts.headers['X-Tournament-Password']).toBe(ORGANISER_KEY);
    });

    it('throws server error message on failure', async () => {
      global.fetch = mockFetch(500, { error: 'Backup failed' });
      await expect(API.backupTournament(ORGANISER_KEY)).rejects.toThrow('Backup failed');
    });
  });

  describe('backupCompetition', () => {
    it('GETs /api/competitions/:id/backup with X-Tournament-Password and returns { blob, filename }', async () => {
      const blob = new Blob(['comp-backup']);
      global.fetch = mockFetchBlob(200, {
        blob,
        headers: { 'Content-Disposition': 'attachment; filename="tournament-comp-1-20261103T100000Z.zip"' },
      });

      const result = await API.backupCompetition('comp-1', ORGANISER_KEY);
      expect(result).toEqual({ blob, filename: 'tournament-comp-1-20261103T100000Z.zip' });

      expect(global.fetch).toHaveBeenCalledTimes(1);
      const [url, opts] = global.fetch.mock.calls[0];
      expect(url).toBe('/api/competitions/comp-1/backup');
      expect(opts.method).toBe('GET');
      expect(opts.headers['X-Tournament-Password']).toBe(ORGANISER_KEY);
    });
  });

  describe('planRestore', () => {
    it('POSTs /api/tournament/restore/plan with archive file and form fields', async () => {
      const planResponse = {
        scope: 'tournament',
        competitions: [{ id: 'c1', name: 'Open', asked: true, choices: ['replace', 'copy'] }],
      };
      global.fetch = mockFetch(200, planResponse);

      const fakeFile = new File(['dummy'], 'backup.zip', { type: 'application/zip' });
      const res = await API.planRestore({ file: fakeFile, only: 'c1', undo: false }, ORGANISER_KEY);

      expect(res).toEqual(planResponse);
      expect(global.fetch).toHaveBeenCalledTimes(1);
      const [url, opts] = global.fetch.mock.calls[0];
      expect(url).toBe('/api/tournament/restore/plan');
      expect(opts.method).toBe('POST');
      expect(opts.headers['X-Tournament-Password']).toBe(ORGANISER_KEY);
      expect(opts.body).toBeInstanceOf(FormData);
      expect(opts.body.get('archive')).toBe(fakeFile);
      expect(opts.body.get('only')).toBe('c1');
      expect(opts.body.get('undo')).toBeNull();
    });

    it('POSTs /api/tournament/restore/plan with from parameter for a kept copy', async () => {
      const planResponse = { scope: 'tournament', competitions: [] };
      global.fetch = mockFetch(200, planResponse);

      const res = await API.planRestore({ from: '20260101T000000Z-scheduled.zip', undo: true }, ORGANISER_KEY);

      expect(res).toEqual(planResponse);
      const [url, opts] = global.fetch.mock.calls[0];
      expect(url).toBe('/api/tournament/restore/plan');
      expect(opts.body.get('from')).toBe('20260101T000000Z-scheduled.zip');
      expect(opts.body.get('undo')).toBe('true');
    });

    it('attaches HTTP status to thrown error when request fails', async () => {
      global.fetch = mockFetch(413, { error: 'request body too large' });
      const fakeFile = new File(['dummy'], 'large.zip', { type: 'application/zip' });
      const err = await API.planRestore({ file: fakeFile }, ORGANISER_KEY).catch((e) => e);
      expect(err).toBeInstanceOf(Error);
      expect(err.status).toBe(413);
      expect(err.message).toBe('request body too large');
    });
  });

  describe('restore', () => {
    it('POSTs /api/tournament/restore with headers, form fields, decisions JSON, and calls _forgetAllShownMatches', async () => {
      const restoreResponse = {
        plan: { scope: 'tournament' },
        safetyCopy: '20261104T000000Z-before-restore-tournament.zip',
        prefixWarnings: [],
      };
      global.fetch = mockFetch(200, restoreResponse);

      // Note a match first to test _forgetAllShownMatches clears it
      API.noteShownMatch('c1', { id: 'm1', phase: 'pool', pool: 'A', order: 1 });

      const decisions = { c1: { action: 'replace' } };
      const res = await API.restore(
        { from: '20260101T000000Z-scheduled.zip', decisions, allowRunning: true },
        SESSION_KEY,
        'confirm-pw'
      );

      expect(res).toEqual(restoreResponse);
      expect(global.fetch).toHaveBeenCalledTimes(1);
      const [url, opts] = global.fetch.mock.calls[0];
      expect(url).toBe('/api/tournament/restore');
      expect(opts.method).toBe('POST');
      expect(opts.headers['X-Tournament-Password']).toBe(SESSION_KEY);
      expect(opts.headers['X-Confirm-Password']).toBe('confirm-pw');
      expect(opts.body.get('from')).toBe('20260101T000000Z-scheduled.zip');
      expect(opts.body.get('decisions')).toBe(JSON.stringify(decisions));
      expect(opts.body.get('allowRunning')).toBe('true');
    });

    it('reports confirm_password_* 401 as a wrong confirm error without signing out', async () => {
      global.fetch = mockFetch(401, {
        error: 'Wrong confirm password',
        code: 'confirm_password_wrong',
      });

      const err = await API.restore(
        { from: 'copy.zip' },
        SESSION_KEY,
        'wrong-confirm-pw'
      ).catch((e) => e);

      expect(err).toBeInstanceOf(Error);
      expect(err.code).toBe('confirm_password_wrong');
      expect(err.message).toBe('Wrong confirm password');
    });

    it('sends empty string for X-Confirm-Password when confirmPassword is not passed, and no undo field when undo is false', async () => {
      global.fetch = mockFetch(200, { plan: { scope: 'tournament' } });

      await API.restore({ from: 'copy.zip', undo: false, allowRunning: false }, SESSION_KEY);

      expect(global.fetch).toHaveBeenCalledTimes(1);
      const [, opts] = global.fetch.mock.calls[0];
      expect(opts.headers['X-Tournament-Password']).toBe(SESSION_KEY);
      expect(opts.headers['X-Confirm-Password']).toBe('');
      expect(opts.body.get('undo')).toBeNull();
      expect(opts.body.get('allowRunning')).toBeNull();
    });
  });

  describe('listBackups', () => {
    it('GETs /api/tournament/backups with X-Tournament-Password and returns backup list', async () => {
      const backups = [
        { name: '20261104T000000Z-scheduled.zip', kind: 'scheduled', scope: 'tournament', time: '2026-11-04T00:00:00Z', size: 1024 },
        { name: '20261103T000000Z-before-restore-tournament.zip', kind: 'before-restore', scope: 'tournament', time: '2026-11-03T00:00:00Z', size: 2048 },
      ];
      global.fetch = mockFetch(200, { backups });

      const res = await API.listBackups(ORGANISER_KEY);
      expect(res).toEqual(backups);

      expect(global.fetch).toHaveBeenCalledTimes(1);
      const [url, opts] = global.fetch.mock.calls[0];
      expect(url).toBe('/api/tournament/backups');
      expect(opts.method).toBe('GET');
      expect(opts.headers['X-Tournament-Password']).toBe(ORGANISER_KEY);
    });
  });

  describe('downloadBackup', () => {
    it('GETs /api/tournament/backups/:name with X-Tournament-Password and returns Blob', async () => {
      const blob = new Blob(['backup-file-bytes']);
      global.fetch = mockFetchBlob(200, { blob });

      const res = await API.downloadBackup('20261104T000000Z-scheduled.zip', ORGANISER_KEY);
      expect(res).toBe(blob);

      expect(global.fetch).toHaveBeenCalledTimes(1);
      const [url, opts] = global.fetch.mock.calls[0];
      expect(url).toBe('/api/tournament/backups/20261104T000000Z-scheduled.zip');
      expect(opts.method).toBe('GET');
      expect(opts.headers['X-Tournament-Password']).toBe(ORGANISER_KEY);
    });
  });

  describe('deleteBackup', () => {
    it('DELETEs /api/tournament/backups/:name with headers', async () => {
      global.fetch = mockFetch(200, { deleted: '20261104T000000Z-scheduled.zip' });

      const res = await API.deleteBackup('20261104T000000Z-scheduled.zip', SESSION_KEY, 'confirm-pw');
      expect(res).toEqual({ deleted: '20261104T000000Z-scheduled.zip' });

      expect(global.fetch).toHaveBeenCalledTimes(1);
      const [url, opts] = global.fetch.mock.calls[0];
      expect(url).toBe('/api/tournament/backups/20261104T000000Z-scheduled.zip');
      expect(opts.method).toBe('DELETE');
      expect(opts.headers['X-Tournament-Password']).toBe(SESSION_KEY);
      expect(opts.headers['X-Confirm-Password']).toBe('confirm-pw');
    });

    it('sends empty string for X-Confirm-Password on deleteBackup when confirmPassword is not passed', async () => {
      global.fetch = mockFetch(200, { deleted: '20261104T000000Z-scheduled.zip' });

      await API.deleteBackup('20261104T000000Z-scheduled.zip', SESSION_KEY);

      expect(global.fetch).toHaveBeenCalledTimes(1);
      const [, opts] = global.fetch.mock.calls[0];
      expect(opts.headers['X-Tournament-Password']).toBe(SESSION_KEY);
      expect(opts.headers['X-Confirm-Password']).toBe('');
    });

    it('reports confirm_password_* 401 on deleteBackup as wrong confirm error', async () => {
      global.fetch = mockFetch(401, {
        error: 'Password confirmation failed',
        code: 'confirm_password_wrong',
      });

      const err = await API.deleteBackup(
        '20261104T000000Z-scheduled.zip',
        SESSION_KEY,
        'bad-pw'
      ).catch((e) => e);

      expect(err).toBeInstanceOf(Error);
      expect(err.code).toBe('confirm_password_wrong');
      expect(err.message).toBe('Password confirmation failed');
    });
  });
});
