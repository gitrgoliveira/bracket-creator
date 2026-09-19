// bc-kcdg: API.recordScore parses the server's 409
// {"error":"downstream_knockout_played", matchId, blockingMatchId, displaced,
// message} refusal (correcting a completed knockout match whose later round
// has already been played) into a thrown Error decorated with
// `.downstreamKnockoutPlayed`, so write_result.jsx's
// downstreamKnockoutPlayedRefusal(err) can read it without re-deriving the
// shape from the raw body. Also pins that a caller-supplied
// `forceDownstreamReopen: true` on the patch reaches the wire, since that is
// the flag the confirmed retry depends on (api_serializers.jsx).
//
// The second describe block below pins the SAME contract for
// API.overrideBracketWinner (the manual winner pick used by the admin bracket
// panel and ResolveFeedersModal's "Run now" recovery), which shares the parser
// (_downstreamKnockoutPlayedError) rather than re-deriving it.

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
      // Singleton list: the only multi-entry case is a semifinal that fed
      // both the final and the bronze match.
      blockingMatchIds: ['m5'],
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

// bc-kcdg: overrideBracketWinner (the admin bracket panel's / ResolveFeedersModal's
// manual winner pick) shares the SAME parser (_downstreamKnockoutPlayedError in
// api_client.jsx) and the SAME force-flag field name as recordScore above, so a
// caller can offer the identical confirm+retry loop rather than surfacing the
// raw 409 token with no explanation.
describe('API.overrideBracketWinner: downstream_knockout_played (bc-kcdg)', () => {
  let originalFetch;
  afterEach(() => { if (originalFetch) global.fetch = originalFetch; });

  it('throws an Error decorated with the structured refusal fields', async () => {
    originalFetch = global.fetch;
    global.fetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 409,
      json: async () => ({
        error: 'downstream_knockout_played',
        matchId: 'm-r2-0',
        blockingMatchId: 'm-r1-0',
        displaced: 'Bob',
        message: 'Bob already played match m-r1-0, asserting this winner would displace them.',
      }),
    });

    const err = await API.overrideBracketWinner('c1', 'm-r2-0', 'Alice', 'pw').then(
      () => { throw new Error('expected a rejection'); },
      (e) => e,
    );

    // The operator-facing message is the server's sentence, not the bare code
    // -- this is exactly the "raw token with no explanation" gap being closed.
    expect(err.message).toBe('Bob already played match m-r1-0, asserting this winner would displace them.');
    expect(downstreamKnockoutPlayedRefusal(err)).toEqual({
      matchId: 'm-r2-0',
      blockingMatchId: 'm-r1-0',
      // Singleton list: the only multi-entry case is a semifinal that fed
      // both the final and the bronze match.
      blockingMatchIds: ['m-r1-0'],
      displaced: 'Bob',
    });
  });

  it('does not decorate an unrelated 4xx failure', async () => {
    originalFetch = global.fetch;
    global.fetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 400,
      json: async () => ({ error: 'match not found' }),
    });

    const err = await API.overrideBracketWinner('c1', 'm1', 'Alice', 'pw').then(
      () => { throw new Error('expected a rejection'); },
      (e) => e,
    );
    expect(err.message).toBe('match not found');
    expect(downstreamKnockoutPlayedRefusal(err)).toBeNull();
  });

  it('forwards forceDownstreamReopen:true on the confirmed retry onto the wire payload', async () => {
    originalFetch = global.fetch;
    global.fetch = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ applied: true }),
    });

    await API.overrideBracketWinner('c1', 'm-r2-0', 'Alice', 'pw', true);

    const [url, opts] = global.fetch.mock.calls[0];
    expect(url).toBe('/api/competitions/c1/matches/m-r2-0/override-winner');
    const sentBody = JSON.parse(opts.body);
    expect(sentBody.forceDownstreamReopen).toBe(true);
  });

  it('omits forceDownstreamReopen from the wire payload when not passed', async () => {
    originalFetch = global.fetch;
    global.fetch = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ applied: true }),
    });

    await API.overrideBracketWinner('c1', 'm-r2-0', 'Alice', 'pw');

    const [, opts] = global.fetch.mock.calls[0];
    const sentBody = JSON.parse(opts.body);
    expect(sentBody.forceDownstreamReopen).toBeUndefined();
  });
});

// bc-cse: API.recordDecision (kiken / fusenpai / daihyosen) shares the SAME
// parser (_downstreamKnockoutPlayedError) and the SAME force-flag field name
// as recordScore/overrideBracketWinner above. Before this fix the /decision
// 4xx branch threw a plain `new Error(err.error || ...)`, so a
// downstream_knockout_played refusal surfaced as the literal string
// "downstream_knockout_played" with the server's actual message discarded and
// no field a retry could set to get past it.
describe('API.recordDecision: downstream_knockout_played (bc-cse)', () => {
  let originalFetch;
  afterEach(() => { if (originalFetch) global.fetch = originalFetch; });

  it('throws an Error decorated with the structured refusal fields (not the bare token)', async () => {
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

    const err = await API.recordDecision('c1', 'm1', { decision: 'kiken-voluntary', decisionBy: 'aka' }, 'pw').then(
      () => { throw new Error('expected a rejection'); },
      (e) => e,
    );

    // The operator-facing message is the server's sentence, not the bare code.
    expect(err.message).toBe('Aoki Taro already played match m5, correcting m1 would displace them.');
    expect(err.message).not.toBe('downstream_knockout_played');
    expect(downstreamKnockoutPlayedRefusal(err)).toEqual({
      matchId: 'm1',
      blockingMatchId: 'm5',
      // Singleton list: the only multi-entry case is a semifinal that fed
      // both the final and the bronze match.
      blockingMatchIds: ['m5'],
      displaced: 'Aoki Taro',
    });
  });

  it('does not decorate an unrelated 409 (e.g. decision_locked)', async () => {
    originalFetch = global.fetch;
    global.fetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 409,
      json: async () => ({ error: 'decision_locked' }),
    });

    const err = await API.recordDecision('c1', 'm1', { decision: 'kiken-voluntary', decisionBy: 'aka' }, 'pw').then(
      () => { throw new Error('expected a rejection'); },
      (e) => e,
    );
    expect(err.message).toBe('decision_locked');
    expect(downstreamKnockoutPlayedRefusal(err)).toBeNull();
  });

  it('forwards forceDownstreamReopen:true on the confirmed retry onto the wire payload', async () => {
    originalFetch = global.fetch;
    global.fetch = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ id: 'm1', status: 'completed' }),
    });

    await API.recordDecision('c1', 'm1', { decision: 'kiken-voluntary', decisionBy: 'aka', forceDownstreamReopen: true }, 'pw');

    const [url, opts] = global.fetch.mock.calls[0];
    expect(url).toBe('/api/competitions/c1/matches/m1/decision');
    const sentBody = JSON.parse(opts.body);
    expect(sentBody.forceDownstreamReopen).toBe(true);
  });
});
