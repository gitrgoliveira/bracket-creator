// bc-hnam: API.createTournament keeps a 409's code and competition list, so
// the first-run form can name the competitions a self-run create would show
// (public_names_self_run_switch) before sending it again with confirmShowNames.
import { describe, it, expect, vi, afterEach } from 'vitest';
import { API } from '../api_client.jsx';

const realFetch = global.fetch;
afterEach(() => { global.fetch = realFetch; });

describe('API.createTournament refusals', () => {
  it('keeps the code and the competitions of a public_names_self_run_switch 409', async () => {
    const competitions = [{ id: 'c1', name: 'Kyu Men', publicNames: 'person' }];
    global.fetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 409,
      json: async () => ({ code: 'public_names_self_run_switch', error: 'Names would show.', competitions }),
    });
    const err = await API.createTournament({ name: 'X', mode: 'self-run' }).catch((e) => e);
    expect(err).toBeInstanceOf(Error);
    expect(err.message).toBe('Names would show.');
    expect(err.code).toBe('public_names_self_run_switch');
    expect(err.competitions).toEqual(competitions);
  });

  it('sends confirmShowNames in the body when the form passes it', async () => {
    global.fetch = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ name: 'X' }) });
    await API.createTournament({ name: 'X', mode: 'self-run', confirmShowNames: true });
    const [, opts] = global.fetch.mock.calls[0];
    expect(JSON.parse(opts.body).confirmShowNames).toBe(true);
  });

  it('carries no code on a refusal that is not a 409', async () => {
    global.fetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 400,
      json: async () => ({ code: 'whatever', error: 'bad date' }),
    });
    const err = await API.createTournament({ name: 'X' }).catch((e) => e);
    expect(err.message).toBe('bad date');
    expect(err.code).toBeUndefined();
  });
});
