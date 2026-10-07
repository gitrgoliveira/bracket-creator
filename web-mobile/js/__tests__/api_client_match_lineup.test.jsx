// mp-bkg: tests for the matchId-keyed lineup API helpers (fetchLineupInForce,
// putMatchLineup, deleteMatchLineup) in api_client.jsx. fetchLineupInForce turns
// a `saved: false` body into null (bc-k404: nothing saved is a 200, not a 404),
// the same rule fetchTeamLineup applies below, just against a different endpoint
// path (/lineup-in-force/:matchId vs /lineups/:round).

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { API } from '../api_client.jsx';
import { FETCH_TIMEOUT_MS } from '../write_result.jsx';
import { lineupReadFailure, LINEUP_READ_NO_ANSWER } from '../lineup_resolver.jsx';

// Minimal fetch stub that records the most recent call.
function mockFetch(status, body) {
  return vi.fn(() =>
    Promise.resolve({
      ok: status >= 200 && status < 300,
      status,
      json: () => Promise.resolve(body),
    })
  );
}

// The lineup a team fields at a match (operator ruling 2026-10-05): the server
// owns the rule; the client maps `saved: false` to null and hands the body back
// whole, source fields included.
describe('API.fetchLineupInForce', () => {
  let originalFetch;
  beforeEach(() => { originalFetch = global.fetch; });
  afterEach(() => { global.fetch = originalFetch; });

  it('calls the correct /lineup-in-force/:matchId URL', async () => {
    global.fetch = mockFetch(200, { positions: {}, saved: false });
    await API.fetchLineupInForce('c42', 't99', 'mx7');
    const [url] = global.fetch.mock.calls[0];
    expect(url).toBe('/api/competitions/c42/teams/t99/lineup-in-force/mx7');
  });

  it('returns null when nothing is in force (200, saved false)', async () => {
    global.fetch = mockFetch(200, { teamId: 'team1', matchId: 'match1', positions: {}, saved: false });
    expect(await API.fetchLineupInForce('comp1', 'team1', 'match1')).toBeNull();
  });

  it('returns the body whole, a carried lineup\'s source included', async () => {
    const carried = { teamId: 'team1', matchId: 'earlier', round: 0, positions: { senpo: 'Alice' }, sourceMatchId: 'earlier', saved: true };
    global.fetch = mockFetch(200, carried);
    expect(await API.fetchLineupInForce('comp1', 'team1', 'match1')).toEqual(carried);
  });

  it('keeps a Lineups-page lineup\'s round 0, the starting lineup, as a source', async () => {
    const starting = { teamId: 'team1', round: 0, positions: { senpo: 'Alice' }, sourceRound: 0, saved: true };
    global.fetch = mockFetch(200, starting);
    const result = await API.fetchLineupInForce('comp1', 'team1', 'match1');
    expect(result.sourceRound).toBe(0);
  });

  it('saved true with empty positions is a lineup', async () => {
    const lineup = { teamId: 'team1', matchId: 'match1', positions: {}, sourceMatchId: 'match1', saved: true };
    global.fetch = mockFetch(200, lineup);
    expect(await API.fetchLineupInForce('comp1', 'team1', 'match1')).toEqual(lineup);
  });

  it('throws on an error answer, 404 included, with the server\'s message', async () => {
    global.fetch = mockFetch(500, { error: 'internal' });
    await expect(API.fetchLineupInForce('c1', 't1', 'm1')).rejects.toThrow('internal');

    global.fetch = mockFetch(404, { error: 'competition not found' });
    await expect(API.fetchLineupInForce('c1', 't1', 'm1')).rejects.toThrow('competition not found');
  });
});

describe('API.fetchTeamLineup', () => {
  let originalFetch;
  beforeEach(() => { originalFetch = global.fetch; });
  afterEach(() => { global.fetch = originalFetch; });

  it('calls the correct URL, with no query', async () => {
    global.fetch = mockFetch(200, { teamId: 't1', round: 1, positions: {}, saved: false });
    await API.fetchTeamLineup('c1', 't1', 1);
    const [url] = global.fetch.mock.calls[0];
    expect(url).toBe('/api/competitions/c1/teams/t1/lineups/1');
  });

  it('reads round 0, a team\'s starting lineup, from the round route', async () => {
    global.fetch = mockFetch(200, { teamId: 't1', round: 0, positions: {}, saved: false });
    await API.fetchTeamLineup('c1', 't1', 0);
    const [url] = global.fetch.mock.calls[0];
    expect(url).toBe('/api/competitions/c1/teams/t1/lineups/0');
  });

  it('returns null when saved is false', async () => {
    global.fetch = mockFetch(200, { teamId: 't1', round: 1, positions: {}, saved: false });
    expect(await API.fetchTeamLineup('c1', 't1', 1)).toBeNull();
  });

  it('returns the body when saved is true', async () => {
    const lineup = { teamId: 't1', round: 0, positions: { senpo: 'Alice' }, saved: true };
    global.fetch = mockFetch(200, lineup);
    const result = await API.fetchTeamLineup('c1', 't1', 0);
    expect(result).toEqual(lineup);
  });

  it('throws "competition not found" on 404', async () => {
    global.fetch = mockFetch(404, { error: 'competition not found' });
    await expect(API.fetchTeamLineup('c1', 't1', 1)).rejects.toThrow('competition not found');
  });

  it('throws on a 500', async () => {
    global.fetch = mockFetch(500, { error: 'internal' });
    await expect(API.fetchTeamLineup('c1', 't1', 1)).rejects.toThrow('internal');
  });
});

// Both reads are bounded like every sibling request (the deadline covers the
// body too): a read the server never answers must end, with the timed-out error
// lineupReadFailure words, or an editor stays on "Loading lineup…" until the
// browser gives up on the connection.
describe('reading a lineup when the server does not answer', () => {
  const reads = [
    ['fetchLineupInForce', () => API.fetchLineupInForce('c1', 't1', 'm1')],
    ['fetchTeamLineup', () => API.fetchTeamLineup('c1', 't1', 0)],
  ];
  let originalFetch;
  beforeEach(() => { originalFetch = global.fetch; });
  afterEach(() => { global.fetch = originalFetch; vi.useRealTimers(); });

  // What a read has come to so far, without waiting on it.
  function watch(read) {
    const seen = { outcome: 'waiting' };
    read().then((lineup) => { seen.outcome = lineup; }, (error) => { seen.outcome = error; });
    return seen;
  }

  it.each(reads)('%s rejects with the timed-out error when the request is never answered, at the deadline', async (_name, read) => {
    vi.useFakeTimers();
    global.fetch = vi.fn(() => new Promise(() => {}));
    const seen = watch(read);
    await vi.advanceTimersByTimeAsync(FETCH_TIMEOUT_MS - 1);
    expect(seen.outcome).toBe('waiting');
    await vi.advanceTimersByTimeAsync(2);
    expect(seen.outcome).toBeInstanceOf(Error);
    expect(seen.outcome.timedOut).toBe(true);
    expect(lineupReadFailure(seen.outcome)).toBe(LINEUP_READ_NO_ANSWER);
  });

  it.each(reads)('%s gives up on an answer whose body never completes, too', async (_name, read) => {
    vi.useFakeTimers();
    global.fetch = vi.fn(() => Promise.resolve({ ok: true, status: 200, json: () => new Promise(() => {}) }));
    const seen = watch(read);
    await vi.advanceTimersByTimeAsync(FETCH_TIMEOUT_MS + 1);
    expect(seen.outcome.timedOut).toBe(true);
  });

  it.each(reads)('%s aborts the request it gave up on, which frees its connection', async (_name, read) => {
    vi.useFakeTimers();
    global.fetch = vi.fn((_url, { signal }) => new Promise((_resolve, reject) => {
      signal.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')));
    }));
    const seen = watch(read);
    await vi.advanceTimersByTimeAsync(FETCH_TIMEOUT_MS + 1);
    expect(global.fetch.mock.calls[0][1].signal.aborted).toBe(true);
    expect(seen.outcome.timedOut).toBe(true);
  });

  it.each(reads)('%s lets a connection that is down through as the browser\'s own error, which lineupReadFailure words', async (_name, read) => {
    global.fetch = vi.fn(() => Promise.reject(new TypeError('Failed to fetch')));
    const error = await read().catch((e) => e);
    expect(error).toBeInstanceOf(TypeError);
    expect(error.timedOut).toBeUndefined();
    expect(lineupReadFailure(error)).toBe(LINEUP_READ_NO_ANSWER);
  });

  it.each(reads)('%s still throws the server\'s own message, or a plain sentence when it sends none', async (_name, read) => {
    global.fetch = mockFetch(404, { error: 'competition not found' });
    await expect(read()).rejects.toThrow('competition not found');
    global.fetch = mockFetch(500, {});
    await expect(read()).rejects.toThrow('Failed to load lineup');
  });

  // _fetchJson reads an unreadable body as {}. A 200 that is not JSON (a venue's
  // sign-in page answering for the server) must stay a failed read, never an
  // empty lineup: a Save composed on it would blank the lineup it stands for.
  it.each(reads)('%s does not hand an unreadable answer on as a lineup', async (_name, read) => {
    global.fetch = vi.fn(() => Promise.resolve({ ok: true, status: 200, json: () => Promise.reject(new SyntaxError('Unexpected token < in JSON')) }));
    await expect(read()).rejects.toThrow('The lineup could not be read. Check the connection and try again.');
  });
});

describe('API.putMatchLineup', () => {
  let originalFetch;
  beforeEach(() => { originalFetch = global.fetch; });
  afterEach(() => { global.fetch = originalFetch; });

  it('sends PUT to the correct /match-lineups/:matchId URL', async () => {
    const saved = { teamId: 't1', matchId: 'm1', positions: { senpo: 'Bob' } };
    global.fetch = mockFetch(200, saved);
    const result = await API.putMatchLineup('c1', 't1', 'm1', { senpo: 'Bob' }, 'pw');
    const [url, opts] = global.fetch.mock.calls[0];
    expect(url).toBe('/api/competitions/c1/teams/t1/match-lineups/m1');
    expect(opts.method).toBe('PUT');
    expect(opts.headers['X-Tournament-Password']).toBe('pw');
    expect(JSON.parse(opts.body)).toMatchObject({ teamId: 't1', matchId: 'm1', positions: { senpo: 'Bob' } });
    expect(result).toEqual(saved);
  });

  it('throws on 400 with server message', async () => {
    global.fetch = mockFetch(400, { error: 'missing senpo' });
    await expect(API.putMatchLineup('c1', 't1', 'm1', {}, 'pw'))
      .rejects.toThrow('missing senpo');
  });

  // bc-dhas: on a self-run tournament the public score sheet may save a
  // lineup, but not once the match has finished. The refusal names the
  // reason in a sentence, which is what the editor shows, and the error
  // keeps the code beside it, as the member writes' errors do.
  it('throws the sentence of a finished match\'s refusal, carrying its code', async () => {
    global.fetch = mockFetch(409, {
      error: 'result_finalized',
      message: 'This match has finished, so its lineup can no longer be changed. Contact the tournament organizer to correct it.',
    });
    const err = await API.putMatchLineup('c1', 't1', 'm1', { senpo: 'Bob' }, '').catch((e) => e);
    expect(err.message).toBe('This match has finished, so its lineup can no longer be changed. Contact the tournament organizer to correct it.');
    expect(err.code).toBe('result_finalized');
  });
});

// bc-pnum gap closure: putMatchLineup grows an optional trailing memberIds
// argument, mirroring putTeamLineup's own treatment exactly (see that
// function's comment in api_client.jsx). These three cases are the ones
// that matter: sent when given, omitted (not just falsy/undefined) when
// not given so an old caller's wire body is byte-identical, and carried
// into the offline queue too (bc-pnum: a client on unreliable venue wifi
// must not silently lose the ids on reconnect replay).
describe('API.putMatchLineup: memberIds (bc-pnum gap closure)', () => {
  let originalFetch;
  beforeEach(() => {
    originalFetch = global.fetch;
    localStorage.removeItem('bc_write_queue');
  });
  afterEach(async () => {
    global.fetch = originalFetch;
    API.clearQueue();
  });

  it('sends memberIds in the body when provided', async () => {
    global.fetch = mockFetch(200, { teamId: 't1', matchId: 'm1', positions: { senpo: 'Bob' }, memberIds: { senpo: 'mem-1' } });
    await API.putMatchLineup('c1', 't1', 'm1', { senpo: 'Bob' }, 'pw', { senpo: 'mem-1' });
    const [, opts] = global.fetch.mock.calls[0];
    const body = JSON.parse(opts.body);
    expect(body.memberIds).toEqual({ senpo: 'mem-1' });
  });

  it('omits the memberIds key entirely when not provided (byte-identical old body)', async () => {
    global.fetch = mockFetch(200, { teamId: 't1', matchId: 'm1', positions: { senpo: 'Bob' } });
    await API.putMatchLineup('c1', 't1', 'm1', { senpo: 'Bob' }, 'pw');
    const [, opts] = global.fetch.mock.calls[0];
    const body = JSON.parse(opts.body);
    expect(body).toEqual({ teamId: 't1', competitionId: 'c1', matchId: 'm1', positions: { senpo: 'Bob' } });
    expect('memberIds' in body).toBe(false);
  });

  it('carries memberIds into the offline-queued body on network failure', async () => {
    global.fetch = vi.fn(() => Promise.reject(new TypeError('network error')));
    const result = await API.putMatchLineup('c1', 't-queue', 'm-queue', { senpo: 'Bob' }, 'pw', { senpo: 'mem-9' });
    expect(result).toEqual({ queued: true });
    const raw = localStorage.getItem('bc_write_queue');
    expect(raw).not.toBeNull();
    const entries = JSON.parse(raw);
    const found = entries.find(([key]) => key.includes('m-queue'));
    expect(found).toBeTruthy();
    expect(found[1].payload.memberIds).toEqual({ senpo: 'mem-9' });
    // Drain the immediate background retry api_client fires on enqueue (it
    // also fails against this same rejecting mock), then cancel any
    // resulting backoff timer so it cannot fire during a LATER test in this
    // file: this file does not use fake timers, unlike sync_queue.test.jsx.
    await Promise.resolve(); await Promise.resolve(); await Promise.resolve();
    API.clearQueue();
  });
});

describe('API.deleteMatchLineup', () => {
  let originalFetch;
  beforeEach(() => { originalFetch = global.fetch; });
  afterEach(() => { global.fetch = originalFetch; });

  it('sends DELETE to the correct URL and returns true on 204', async () => {
    global.fetch = vi.fn(() => Promise.resolve({ ok: true, status: 204 }));
    const result = await API.deleteMatchLineup('c1', 't1', 'm1', 'pw');
    const [url, opts] = global.fetch.mock.calls[0];
    expect(url).toBe('/api/competitions/c1/teams/t1/match-lineups/m1');
    expect(opts.method).toBe('DELETE');
    expect(opts.headers['X-Tournament-Password']).toBe('pw');
    expect(result).toBe(true);
  });

  it('returns true on 404 (idempotent delete)', async () => {
    global.fetch = vi.fn(() => Promise.resolve({ ok: false, status: 404 }));
    const result = await API.deleteMatchLineup('c1', 't1', 'm1', 'pw');
    expect(result).toBe(true);
  });

  it('throws on non-404 error responses', async () => {
    global.fetch = vi.fn(() =>
      Promise.resolve({
        ok: false,
        status: 500,
        json: () => Promise.resolve({ error: 'internal error' }),
      })
    );
    await expect(API.deleteMatchLineup('c1', 't1', 'm1', 'pw'))
      .rejects.toThrow('internal error');
  });
});

// A removal is bounded like every sibling write (the deadline covers the body
// too), and answers in a plain sentence when the server cannot be reached: the
// editor shows it as it is, never the browser's "Failed to fetch".
describe('removing a lineup when the server does not answer', () => {
  const NOT_REMOVED = 'The lineup was not removed: the server did not answer. Check the connection and try again.';
  const removals = [
    ['deleteMatchLineup', () => API.deleteMatchLineup('c1', 't1', 'm1', 'pw')],
  ];
  let originalFetch;
  beforeEach(() => { originalFetch = global.fetch; });
  afterEach(() => { global.fetch = originalFetch; vi.useRealTimers(); });

  it.each(removals)('%s rejects with a plain sentence when the connection is down', async (_name, remove) => {
    global.fetch = vi.fn(() => Promise.reject(new TypeError('Failed to fetch')));
    const err = await remove().catch((e) => e);
    expect(err.message).toBe(NOT_REMOVED);
    expect(err.message).not.toMatch(/Failed to fetch/);
  });

  it.each(removals)('%s sends its request with an abort signal, so it can be given up on', async (_name, remove) => {
    global.fetch = vi.fn(() => Promise.resolve({ ok: true, status: 204 }));
    await remove();
    const [, opts] = global.fetch.mock.calls[0];
    expect(opts.method).toBe('DELETE');
    expect(opts.signal).toBeInstanceOf(AbortSignal);
  });

  it.each(removals)('%s gives up on a request that is never answered, at the deadline', async (_name, remove) => {
    vi.useFakeTimers();
    global.fetch = vi.fn((_url, { signal }) => new Promise((_resolve, reject) => {
      signal.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')));
    }));
    let settled = false;
    const pending = remove().catch((e) => e).finally(() => { settled = true; });
    await vi.advanceTimersByTimeAsync(FETCH_TIMEOUT_MS - 1);
    expect(settled).toBe(false);
    await vi.advanceTimersByTimeAsync(2);
    expect((await pending).message).toBe(NOT_REMOVED);
  });

  it.each(removals)('%s gives up on an answer whose body never completes, too', async (_name, remove) => {
    vi.useFakeTimers();
    global.fetch = vi.fn(() => Promise.resolve({ ok: false, status: 500, json: () => new Promise(() => {}) }));
    const pending = remove().catch((e) => e);
    await vi.advanceTimersByTimeAsync(FETCH_TIMEOUT_MS + 1);
    expect((await pending).message).toBe(NOT_REMOVED);
  });

  it.each(removals)('%s still says a refusal in the server\'s own words, and a missing lineup is removed already', async (_name, remove) => {
    global.fetch = vi.fn(() => Promise.resolve({
      ok: false, status: 409, json: () => Promise.resolve({ error: 'locked', message: 'The competition has finished.' }),
    }));
    const err = await remove().catch((e) => e);
    expect(err.message).toBe('The competition has finished.');
    expect(err.code).toBe('locked');

    global.fetch = vi.fn(() => Promise.resolve({ ok: false, status: 404, json: () => Promise.resolve({ error: 'not found' }) }));
    expect(await remove()).toBe(true);
  });
});

// Is a save of this lineup still queued? A queued save replays after anything sent
// now, so a removal made meanwhile would be undone by it: the editors hold "Use the
// previous match's lineup" until it has gone out. That is all the answer is for:
// a new save of the lineup joins the queued one and names what it changed, so
// nothing is composed on what is queued (lineup_save_queue.test.jsx).
describe('API.queuedLineupSave', () => {
  let originalFetch;
  beforeEach(() => {
    originalFetch = global.fetch;
    localStorage.removeItem('bc_write_queue');
    API.clearQueue();
  });
  afterEach(async () => {
    global.fetch = originalFetch;
    // Drain the immediate background retry the enqueue fires (it fails against the
    // same rejecting mock), then cancel any backoff timer it left.
    await Promise.resolve(); await Promise.resolve(); await Promise.resolve();
    API.clearQueue();
  });
  const offline = () => { global.fetch = vi.fn(() => Promise.reject(new TypeError('network error'))); };

  it('is false while nothing is queued', () => {
    expect(API.queuedLineupSave('c1', 't1', { matchId: 'm1' })).toBe(false);
    expect(API.queuedLineupSave('c1', 't1', { round: 0 })).toBe(false);
  });

  it('is true for the match whose own lineup save is queued, and for that match of that team only', async () => {
    offline();
    expect(await API.putMatchLineup('c1', 't-q', 'm-q', { senpo: 'Bob' }, 'pw', undefined, ['senpo'])).toEqual({ queued: true });
    expect(API.queuedLineupSave('c1', 't-q', { matchId: 'm-q' })).toBe(true);
    expect(API.queuedLineupSave('c1', 't-q', { matchId: 'm-other' })).toBe(false);
    expect(API.queuedLineupSave('c1', 't-other', { matchId: 'm-q' })).toBe(false);
    expect(API.queuedLineupSave('c2', 't-q', { matchId: 'm-q' })).toBe(false);
    // Neither the team's starting lineup nor another round.
    expect(API.queuedLineupSave('c1', 't-q', { round: 0 })).toBe(false);
  });

  it('is true for a queued starting lineup (round 0), and for that round only', async () => {
    offline();
    await API.putTeamLineup('c1', 't-q', 0, { senpo: 'Bob' }, 'pw', undefined, ['senpo']);
    expect(API.queuedLineupSave('c1', 't-q', { round: 0 })).toBe(true);
    expect(API.queuedLineupSave('c1', 't-q', { round: 1 })).toBe(false);
    expect(API.queuedLineupSave('c1', 't-q', { matchId: 'm-q' })).toBe(false);
  });

  it('stays true when a later save joined the queued one: the lineup still has one save queued', async () => {
    offline();
    await API.putMatchLineup('c1', 't-q', 'm-q', { senpo: 'Bob' }, 'pw', { senpo: 'mem-1' }, ['senpo']);
    await API.putMatchLineup('c1', 't-q', 'm-q', { jiho: 'Amy' }, 'pw', undefined, ['jiho']);
    expect(API.queuedLineupSave('c1', 't-q', { matchId: 'm-q' })).toBe(true);
  });

  it('is true when the save clears every position, a queued save of a lineup being a queued save', async () => {
    offline();
    await API.putMatchLineup('c1', 't-q', 'm-q', {}, 'pw');
    expect(API.queuedLineupSave('c1', 't-q', { matchId: 'm-q' })).toBe(true);
  });

  it('asks about the very queue entry a save is queued under: the same key the save used', async () => {
    offline();
    await API.putMatchLineup('c1', 't-q', 'm-q', { senpo: 'Bob' }, 'pw');
    const keys = JSON.parse(localStorage.getItem('bc_write_queue')).map(([key]) => key);
    expect(keys.some((key) => key.startsWith('lineup:c1:t-q:match:m-q'))).toBe(true);
  });

  it('is false again once the queue is cleared', async () => {
    offline();
    await API.putMatchLineup('c1', 't-q', 'm-q', { senpo: 'Bob' }, 'pw');
    expect(API.queuedLineupSave('c1', 't-q', { matchId: 'm-q' })).toBe(true);
    API.clearQueue();
    expect(API.queuedLineupSave('c1', 't-q', { matchId: 'm-q' })).toBe(false);
  });

  it('asks about no lineup at all when it is given no target, and does not throw', () => {
    expect(API.queuedLineupSave('c1', 't1')).toBe(false);
  });
});
