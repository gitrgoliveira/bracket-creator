// Deleting a competition asks for the password again (operator ruling
// 2026-10-12): the password typed in that moment travels in X-Confirm-Password,
// beside the sign-in's X-Tournament-Password. A refusal of that typed password
// is a wrong password in a dialog, never a lost session.
import { describe, it, expect, vi, afterEach } from 'vitest';
import { API } from '../api_client.jsx';

const SENTENCE = 'Wrong password. The competition was not deleted.';

const refusal = (code) => ({
  ok: false,
  status: 401,
  headers: { get: () => null },
  json: async () => ({ error: SENTENCE, message: SENTENCE, code }),
});

describe('API.deleteCompetition', () => {
  afterEach(() => { vi.restoreAllMocks(); });

  it('sends the sign-in password and the password typed again, each in its own header', async () => {
    global.fetch = vi.fn().mockResolvedValue({ ok: true, status: 204, headers: { get: () => null }, json: async () => ({}) });
    await expect(API.deleteCompetition('c1', 'main', 'typed-now')).resolves.toBe(true);
    const [url, opts] = global.fetch.mock.calls[0];
    expect(url).toBe('/api/competitions/c1');
    expect(opts.method).toBe('DELETE');
    expect(opts.headers['X-Tournament-Password']).toBe('main');
    expect(opts.headers['X-Confirm-Password']).toBe('typed-now');
  });

  it('a refused confirm throws the server sentence with its code, once, and signs nobody out', async () => {
    global.fetch = vi.fn().mockResolvedValue(refusal('confirm_password_wrong'));
    const before = API.unsentWrites().total;
    const err = await API.deleteCompetition('c1', 'main', 'nope').catch((e) => e);
    expect(err).toBeInstanceOf(Error);
    expect(err.message).toBe(SENTENCE);
    expect(err.code).toBe('confirm_password_wrong');
    expect(global.fetch).toHaveBeenCalledTimes(1);
    // Nothing parked or queued: the 401 is not a refused session.
    expect(API.unsentWrites().total).toBe(before);
    expect(API.heldWrites()).toEqual([]);
  });

  it('only the other 401 (the sign-in itself refused) has no confirm code', async () => {
    global.fetch = vi.fn().mockResolvedValue({
      ok: false, status: 401, headers: { get: () => null },
      json: async () => ({ error: 'invalid tournament password' }),
    });
    const err = await API.deleteCompetition('c1', 'stale', 'typed').catch((e) => e);
    expect(err.message).toBe('invalid tournament password');
    expect(err.code).toBeUndefined();
  });
});
