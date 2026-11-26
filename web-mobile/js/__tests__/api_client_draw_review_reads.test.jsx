// The draw review's reads (bc-drev): API.fetchSwapCandidates asks the server,
// for the competitor picked first in the swap mode, whether swapping them with
// each other drawn competitor keeps dojo-mates apart for longer. It sends the
// organiser's password and the pick as the `id` query, answers the pick and
// the candidates as the server lists them, and throws a refusal with the
// server's sentence and code, like fetchDojoSeparation and fetchSwapSuggestions,
// whose shape this file pins beside it.
import { describe, it, expect, vi, afterEach } from 'vitest';
import { API } from '../api_client.jsx';

function answer(status, body) {
  return { ok: status >= 200 && status < 300, status, json: async () => body };
}

describe('API.fetchSwapCandidates', () => {
  const originalFetch = global.fetch;
  afterEach(() => { global.fetch = originalFetch; });

  it('sends the pick and the password, and answers the pick and the candidates', async () => {
    const body = {
      pick: { id: 'p 1', place: { pool: 'Pool A', position: 1 } },
      candidates: [
        { id: 'p2', place: { pool: 'Pool A', position: 2 }, verdict: 'same' },
        { id: 'p3', place: { pool: 'Pool B', position: 1 }, verdict: 'better' },
        { id: 'p4', place: { pool: 'Pool B', position: 2 }, verdict: 'worse' },
      ],
    };
    global.fetch = vi.fn().mockResolvedValue(answer(200, body));
    const got = await API.fetchSwapCandidates('c1', 'p 1', 'pw');
    expect(got).toEqual(body);
    const [url, opts] = global.fetch.mock.calls[0];
    expect(url).toBe('/api/competitions/c1/draw/swap-candidates?id=p%201');
    expect(opts.headers['X-Tournament-Password']).toBe('pw');
  });

  it('answers empty lists for a body without them', async () => {
    global.fetch = vi.fn().mockResolvedValue(answer(200, {}));
    expect(await API.fetchSwapCandidates('c1', 'p1', 'pw')).toEqual({ pick: null, candidates: [] });
  });

  it('throws a refusal with the server sentence, its code and status', async () => {
    global.fetch = vi.fn().mockResolvedValue(answer(409, { error: 'not_in_draw', message: 'Ana is not in the draw.' }));
    const err = await API.fetchSwapCandidates('c1', 'p1', 'pw').catch((e) => e);
    expect(err).toBeInstanceOf(Error);
    expect(err.message).toBe('Ana is not in the draw.');
    expect(err.code).toBe('not_in_draw');
    expect(err.status).toBe(409);
  });

  it('says it read nothing when the connection drops', async () => {
    global.fetch = vi.fn().mockRejectedValue(new TypeError('Failed to fetch'));
    const err = await API.fetchSwapCandidates('c1', 'p1', 'pw').catch((e) => e);
    expect(err.message).toBe('Could not read the swap candidates. Check the connection and try again.');
  });
});

describe('the other draw review reads keep their shape', () => {
  const originalFetch = global.fetch;
  afterEach(() => { global.fetch = originalFetch; });

  it('fetchSwapSuggestions and fetchDojoSeparation answer lists, and a refusal carries its code', async () => {
    global.fetch = vi.fn().mockResolvedValue(answer(200, {}));
    expect(await API.fetchSwapSuggestions('c1', 'pw')).toEqual({ suggestions: [], stuck: [] });
    expect(await API.fetchDojoSeparation('c1', 'pw')).toEqual([]);
    global.fetch = vi.fn().mockResolvedValue(answer(409, { error: 'no_draw', message: 'There is no draw to change yet.' }));
    const err = await API.fetchSwapSuggestions('c1', 'pw').catch((e) => e);
    expect(err.message).toBe('There is no draw to change yet.');
    expect(err.code).toBe('no_draw');
  });
});
