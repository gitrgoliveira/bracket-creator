// bc-kcdg: API.recordScore parses the server's 409
// {"error":"downstream_knockout_played", matchId, blockingMatchId, displaced,
// message} refusal (correcting a completed knockout match whose later round
// has already been played) into a thrown Error decorated with
// `.downstreamKnockoutPlayed`, so write_result.jsx's
// downstreamKnockoutPlayedRefusal(err) can read it without re-deriving the
// shape from the raw body. Also pins that a caller-supplied
// `forceDownstreamReopen: true` on the patch reaches the wire, since that is
// the flag the confirmed retry depends on (api_serializers.jsx).

import { describe, it, expect, vi, afterEach } from 'vitest';
import { API } from '../api_client.jsx';
import { downstreamKnockoutPlayedRefusal } from '../write_result.jsx';

describe('API.recordScore: downstream_knockout_played (bc-kcdg)', () => {
  let originalFetch;
  afterEach(() => { if (originalFetch) global.fetch = originalFetch; });

  it('throws an Error decorated with the structured refusal fields', async () => {
    originalFetch = global.fetch;
    global.fetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 409,
      json: async () => ({
        error: 'downstream_knockout_played',
        matchId: 'm1',
        blockingMatchId: 'm5',
        displaced: 'Aoki Taro',
        message: 'Aoki Taro already played match m5, correcting m1 would displace them.',
      }),
    });

    const err = await API.recordScore('c1', 'm1', { status: 'completed' }, 'pw').then(
      () => { throw new Error('expected a rejection'); },
      (e) => e,
    );

    // The operator-facing message is the server's sentence, not the bare code.
    expect(err.message).toBe('Aoki Taro already played match m5, correcting m1 would displace them.');
    // write_result.jsx's predicate must be able to read it back off the thrown
    // error without re-deriving the shape.
    expect(downstreamKnockoutPlayedRefusal(err)).toEqual({
      matchId: 'm1',
      blockingMatchId: 'm5',
      displaced: 'Aoki Taro',
    });
  });

  it('does not decorate an unrelated 409 (e.g. ineligible_competitor)', async () => {
    originalFetch = global.fetch;
    global.fetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 409,
      json: async () => ({ error: 'ineligible_competitor', reasonHuman: 'That competitor is ineligible.' }),
    });

    const err = await API.recordScore('c1', 'm1', { status: 'completed' }, 'pw').then(
      () => { throw new Error('expected a rejection'); },
      (e) => e,
    );
    expect(err.message).toBe('That competitor is ineligible.');
    expect(downstreamKnockoutPlayedRefusal(err)).toBeNull();
  });

  it('forwards forceDownstreamReopen:true on the confirmed retry onto the wire payload', async () => {
    originalFetch = global.fetch;
    global.fetch = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ id: 'm1', status: 'completed' }),
    });

    await API.recordScore('c1', 'm1', { status: 'completed', forceDownstreamReopen: true }, 'pw');

    const [url, opts] = global.fetch.mock.calls[0];
    expect(url).toBe('/api/competitions/c1/matches/m1/score');
    const sentBody = JSON.parse(opts.body);
    expect(sentBody.forceDownstreamReopen).toBe(true);
  });

  it('omits forceDownstreamReopen from the wire payload when not set on the patch', async () => {
    originalFetch = global.fetch;
    global.fetch = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ id: 'm1', status: 'completed' }),
    });

    await API.recordScore('c1', 'm1', { status: 'completed' }, 'pw');

    const [, opts] = global.fetch.mock.calls[0];
    const sentBody = JSON.parse(opts.body);
    expect(sentBody.forceDownstreamReopen).toBeUndefined();
  });
});
