// Tests for API.reopenMatch (mp-gmcg). Reopening a completed kachinuki
// encounter is ONE TAP and carries NO audit reason: an operator who ended a
// match by mistake gets straight back into it, and the justification is
// collected on the write that completes it again (correctionReason, enforced
// by the server via reopenPending). Pin the URL/method/headers, the reasonless
// body, the verbatim error surfacing the editor relies on (the server's 409s
// are full sentences the operator reads unchanged), and the blocking-match
// identity the court-busy remedy is built on.

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

describe('API.reopenMatch', () => {
  let originalFetch;
  beforeEach(() => { originalFetch = global.fetch; });
  afterEach(() => { global.fetch = originalFetch; });

  it('POSTs a reasonless JSON body with the password header', async () => {
    global.fetch = mockFetch(200, {});
    const ok = await API.reopenMatch('c42', 'm-r1-0', 'secret');
    expect(ok).toBe(true);
    const [url, opts] = global.fetch.mock.calls[0];
    expect(url).toBe('/api/competitions/c42/matches/m-r1-0/reopen');
    expect(opts.method).toBe('POST');
    expect(opts.headers['X-Tournament-Password']).toBe('secret');
    // Without the JSON content type the server cannot bind the body at all.
    expect(opts.headers['Content-Type']).toBe('application/json');
    // An empty OBJECT, not an absent body: a handler that binds JSON fails on
    // an absent one. And no `reason`: this call must never demand one.
    expect(JSON.parse(opts.body)).toEqual({});
  });

  it('surfaces the court-busy 409 sentence, not the machine code, and keeps the blocking match', async () => {
    // The court-busy conflict is new (mp-gmcg review): reopening would put a
    // second live match on the court. It reuses the score path's STRUCTURED
    // payload, where `error` is the code and the sentence is in `message`.
    // Throwing err.error here would show the operator a bare "court_busy".
    global.fetch = mockFetch(409, {
      error: 'court_busy',
      court: 'A',
      matchId: 'm-r1-1',
      compId: 'c1',
      message: 'Court A already has a running match (m-r1-1). Finish that match before reopening this one.',
    });
    const err = await API.reopenMatch('c1', 'm1', 'secret').then(
      () => { throw new Error('expected a rejection'); },
      (e) => e
    );
    expect(err.message).toBe(
      'Court A already has a running match (m-r1-1). Finish that match before reopening this one.'
    );
    // The blocking match's identity rides the Error: without it the editor can
    // only print the conflict, and a busy court becomes a dead end for the one
    // flow (kachinuki bout-log repair) that has no alternative route.
    expect(err.code).toBe('court_busy');
    expect(err.court).toBe('A');
    expect(err.matchId).toBe('m-r1-1');
    expect(err.compId).toBe('c1');
  });

  it('surfaces the plain-sentence 409s (not completed, downstream fought) verbatim', async () => {
    global.fetch = mockFetch(409, { error: 'cannot reopen: a downstream knockout match has already been fought' });
    await expect(API.reopenMatch('c1', 'm1', 'secret'))
      .rejects.toThrow('cannot reopen: a downstream knockout match has already been fought');
  });

  it('surfaces the server 400 verbatim (e.g. a non-kachinuki competition)', async () => {
    global.fetch = mockFetch(400, { error: 'reopen is only available for kachinuki competitions' });
    await expect(API.reopenMatch('c1', 'm1', 'secret'))
      .rejects.toThrow('reopen is only available for kachinuki competitions');
  });

  it('throws a fallback message when the error body has no error field', async () => {
    global.fetch = mockFetch(500, {});
    await expect(API.reopenMatch('c1', 'm1', 'secret'))
      .rejects.toThrow('Failed to reopen match');
  });
});

// requeueBlockerAndReopen shares reopenMatch's structured-error builder
// (mp-gmcg review R8: reopenFailureError). These pin that the second path
// surfaces the SAME court_busy shape applyReopenFailure depends on, so a
// regression that rewired only one method would fail here.
describe('API.requeueBlockerAndReopen', () => {
  let originalFetch;
  beforeEach(() => { originalFetch = global.fetch; });
  afterEach(() => { global.fetch = originalFetch; });

  it('POSTs the blocker identity to the atomic endpoint with the password header', async () => {
    global.fetch = mockFetch(200, {});
    const ok = await API.requeueBlockerAndReopen('tgt', 'm-r1-0', 'blk', 'm-r1-1', 'secret');
    expect(ok).toBe(true);
    const [url, opts] = global.fetch.mock.calls[0];
    expect(url).toBe('/api/competitions/tgt/matches/m-r1-0/requeue-blocker-and-reopen');
    expect(opts.method).toBe('POST');
    expect(opts.headers['X-Tournament-Password']).toBe('secret');
    expect(opts.headers['Content-Type']).toBe('application/json');
    expect(JSON.parse(opts.body)).toEqual({ blockerCompId: 'blk', blockerMatchId: 'm-r1-1' });
  });

  it('surfaces the same structured court-busy shape as reopenMatch', async () => {
    // A DIFFERENT match may have taken the freed court between the operator
    // opening the remedy and confirming it; the panel re-offers the remedy off
    // exactly these fields, so the requeue path must carry them too.
    global.fetch = mockFetch(409, {
      error: 'court_busy',
      court: 'B',
      matchId: 'm-r2-3',
      compId: 'c9',
      message: 'Court B already has a running match (m-r2-3). Finish that match before reopening this one.',
    });
    const err = await API.requeueBlockerAndReopen('c9', 'm1', 'c9', 'm-blk', 'secret').then(
      () => { throw new Error('expected a rejection'); },
      (e) => e
    );
    expect(err.message).toContain('Court B already has a running match');
    expect(err.code).toBe('court_busy');
    expect(err.court).toBe('B');
    expect(err.matchId).toBe('m-r2-3');
    expect(err.compId).toBe('c9');
  });

  it('throws the fallback message when the error body has no error field', async () => {
    global.fetch = mockFetch(500, {});
    await expect(API.requeueBlockerAndReopen('c1', 'm1', 'c1', 'm2', 'secret'))
      .rejects.toThrow('Failed to reopen match');
  });
});
