// bc-stpf: API.checkStart asks GET .../start-check whether a pick would start
// once the bout running on its court is sent back. A direct read, bounded by
// START_CHECK_TIMEOUT_MS, that never throws and never enters the outbox:
// {ok}, {refused: Error carrying the server's sentence} or {unknown}.
// API.startCheckUntrusted says when an answer could not be trusted.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { API, START_CHECK_TIMEOUT_MS } from '../api_client.jsx';

function reply(status, body) {
  return vi.fn(() => Promise.resolve({
    ok: status >= 200 && status < 300,
    status,
    headers: { get: () => null },
    json: () => Promise.resolve(body),
  }));
}

describe('API.checkStart', () => {
  let originalFetch;
  beforeEach(() => { originalFetch = global.fetch; });
  afterEach(() => { global.fetch = originalFetch; vi.useRealTimers(); });

  it('GETs the start-check URL for the court with the password, and answers ok on a 200', async () => {
    global.fetch = reply(200, { ok: true });
    await expect(API.checkStart('c 1', 'Pool A-1', 'A/1', 'secret')).resolves.toEqual({ ok: true });
    expect(global.fetch).toHaveBeenCalledTimes(1);
    const [url, opts] = global.fetch.mock.calls[0];
    expect(url).toBe('/api/competitions/c%201/matches/Pool%20A-1/start-check?court=A%2F1');
    expect(opts.method).toBe('GET');
    expect(opts.headers['X-Tournament-Password']).toBe('secret');
    expect(opts.body).toBeUndefined();
  });

  it('never enters the outbox', async () => {
    global.fetch = reply(200, { ok: true });
    const before = API.unsentWrites().total;
    await API.checkStart('c1', 'm1', 'A', 'secret');
    expect(API.unsentWrites().total).toBe(before);
  });

  it('a 409 ineligible_competitor is a refusal carrying reasonHuman', async () => {
    const sentence = 'Alice is fighting now in Pool A · Match 3 on Shiaijo B. Finish that match first.';
    global.fetch = reply(409, { error: 'ineligible_competitor', playerId: 'p1', reason: 'running elsewhere', reasonHuman: sentence });
    const res = await API.checkStart('c1', 'm1', 'A', 'secret');
    expect(res.refused).toBeInstanceOf(Error);
    expect(res.refused.message).toBe(sentence);
    expect(res.ok).toBeUndefined();
  });

  it('a 409 match_not_scheduled is a refusal carrying reasonHuman', async () => {
    const sentence = 'This match has already started or finished on another device. Check the court\'s list.';
    global.fetch = reply(409, { error: 'match_not_scheduled', matchId: 'm1', status: 'running', reasonHuman: sentence });
    const res = await API.checkStart('c1', 'm1', 'A', 'secret');
    expect(res.refused.message).toBe(sentence);
  });

  it('a 409 match_on_other_court is a refusal carrying reasonHuman', async () => {
    const sentence = 'This match was moved to Shiaijo B.';
    global.fetch = reply(409, { error: 'match_on_other_court', matchId: 'm1', court: 'B', reasonHuman: sentence });
    const res = await API.checkStart('c1', 'm1', 'A', 'secret');
    expect(res.refused.message).toBe(sentence);
  });

  it.each([
    ['a 500', 500, { error: 'internal error' }],
    ['a 404', 404, { error: 'not found' }],
    ['a 400', 400, { error: 'court is required' }],
    ['a 401', 401, { error: 'invalid password' }],
  ])('%s is unknown', async (_name, status, body) => {
    global.fetch = reply(status, body);
    await expect(API.checkStart('c1', 'm1', 'A', 'secret')).resolves.toEqual({ unknown: true });
  });

  it('a network failure is unknown', async () => {
    global.fetch = vi.fn(() => Promise.reject(new TypeError('Failed to fetch')));
    await expect(API.checkStart('c1', 'm1', 'A', 'secret')).resolves.toEqual({ unknown: true });
  });

  it('no answer within START_CHECK_TIMEOUT_MS is unknown', async () => {
    expect(START_CHECK_TIMEOUT_MS).toBe(1500);
    vi.useFakeTimers();
    global.fetch = vi.fn((_url, opts) => new Promise((_resolve, reject) => {
      opts.signal.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')));
    }));
    let settled = null;
    const p = API.checkStart('c1', 'm1', 'A', 'secret').then((r) => { settled = r; });
    await vi.advanceTimersByTimeAsync(START_CHECK_TIMEOUT_MS - 1);
    expect(settled).toBeNull();
    await vi.advanceTimersByTimeAsync(1);
    await p;
    expect(settled).toEqual({ unknown: true });
  });

  it('a body that stalls past the deadline is unknown too', async () => {
    vi.useFakeTimers();
    global.fetch = vi.fn(() => Promise.resolve({
      ok: true, status: 200, headers: { get: () => null },
      json: () => new Promise(() => {}),
    }));
    let settled = null;
    const p = API.checkStart('c1', 'm1', 'A', 'secret').then((r) => { settled = r; });
    await vi.advanceTimersByTimeAsync(START_CHECK_TIMEOUT_MS);
    await p;
    expect(settled).toEqual({ unknown: true });
  });
});

describe('API.startCheckUntrusted', () => {
  let originalFetch;
  let onLine;
  beforeEach(() => {
    originalFetch = global.fetch;
    onLine = vi.spyOn(window.navigator, 'onLine', 'get').mockReturnValue(true);
  });
  afterEach(() => { global.fetch = originalFetch; onLine.mockRestore(); });

  it('is false online with nothing of the competition unsent', () => {
    expect(API.startCheckUntrusted('c-none')).toBe(false);
  });

  it('is true when the browser says it is offline', () => {
    onLine.mockReturnValue(false);
    expect(API.startCheckUntrusted('c-none')).toBe(true);
  });

  it('is true while the outbox holds a write of the competition, false for another competition', async () => {
    // The write waits on a fetch that never answers, so it stays in the outbox.
    global.fetch = vi.fn(() => new Promise(() => {}));
    await API.revertMatchToQueue('c-held', 'm9', 'secret');
    expect(API.startCheckUntrusted('c-held')).toBe(true);
    expect(API.startCheckUntrusted('c-other')).toBe(false);
  });
});
