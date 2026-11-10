import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

// The server requires an id and a made-at on a court call, and a made-at on a
// clear (modifiedAt, in the server's clock frame). Without them the doors
// answer 400. Since bc-obox both doors are outbox entries ({ pending,
// settled }): what is pinned here is the wire request, and that a clock_skew
// answer is sent again rather than thrown (the stored descriptor and the rest
// of the outbox behaviour: outbox_checkin_announce_settings.test.jsx).
describe('announcement doors carry what the server requires', () => {
  let mod;
  let API;
  let _lsStore;
  let _orig;

  beforeEach(async () => {
    vi.useFakeTimers();
    vi.resetModules();
    _orig = { fetch: global.fetch, ES: global.EventSource, ls: global.localStorage };
    global.EventSource = class FakeES { constructor() { this.close = () => {}; } };
    global.EventSource.OPEN = 1;
    _lsStore = {};
    global.localStorage = {
      getItem: (k) => (k in _lsStore ? _lsStore[k] : null),
      setItem: (k, v) => { _lsStore[k] = String(v); },
      removeItem: (k) => { delete _lsStore[k]; },
      clear: () => { _lsStore = {}; },
    };
    global.fetch = vi.fn(() => Promise.reject(new TypeError('offline')));
    mod = await import('../api_client.jsx');
    API = mod.API;
  });

  afterEach(() => {
    if (API && typeof API.clearQueue === 'function') API.clearQueue();
    vi.clearAllTimers();
    vi.useRealTimers();
    vi.resetModules();
    global.fetch = _orig.fetch;
    global.EventSource = _orig.ES;
    global.localStorage = _orig.ls;
  });

  const UUID_V4 = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
  const respond = (status, body) => Promise.resolve({ ok: status >= 200 && status < 300, status, json: async () => body });
  const flush = async () => { await vi.advanceTimersByTimeAsync(0); for (let i = 0; i < 30; i++) await Promise.resolve(); };
  // The server: `impl(n)` answers the n-th write; the clock answers too.
  function server(impl) {
    const sent = [];
    global.fetch = vi.fn((url, init = {}) => {
      if (String(url).includes('/api/time')) return respond(200, { nowMs: Date.now() });
      sent.push({ url: String(url), method: init.method, headers: init.headers || {}, body: init.body ? JSON.parse(init.body) : undefined });
      return respond(...impl(sent.length));
    });
    return sent;
  }

  it('sendAnnouncement sends a lowercase UUID v4 id and a positive modifiedAt', async () => {
    const sent = server(() => [200, { id: 'x', message: 'Hi' }]);
    const res = await API.sendAnnouncement('Hi', 5, 'pw');
    await res.settled;
    expect(sent[0].url).toBe('/api/tournament/announce');
    expect(sent[0].body.id).toMatch(UUID_V4);
    expect(sent[0].body.id).toBe(res.id);
    expect(sent[0].body.modifiedAt).toBeGreaterThan(0);
    expect(sent[0].body).toMatchObject({ message: 'Hi', durationMinutes: 5 });
  });

  it('sendAnnouncement mints a new id for each call', async () => {
    const sent = server(() => [200, {}]);
    const a = await API.sendAnnouncement('Hi', 5, 'pw');
    const b = await API.sendAnnouncement('Hi', 5, 'pw');
    await Promise.all([a.settled, b.settled]);
    expect(a.id).not.toBe(b.id);
    expect(sent.map((r) => r.body.id).sort()).toEqual([a.id, b.id].sort());
  });

  it('sendAnnouncement sends the call again, with its id, when the clock is refused', async () => {
    const sent = server((n) => (n === 1
      ? [200, { applied: false, reason: 'clock_skew', message: 'Nothing was changed: clock' }]
      : [200, { id: 'x', message: 'Hi' }]));
    const res = await API.sendAnnouncement('Hi', 5, 'pw');
    await flush();
    await expect(res.settled).resolves.toEqual({ id: 'x', message: 'Hi' });
    expect(sent).toHaveLength(2);
    expect(sent[1].body.id).toBe(sent[0].body.id);
  });

  it('clearAnnouncements sends a JSON body with a positive modifiedAt', async () => {
    const sent = server(() => [200, []]);
    const res = await API.clearAnnouncements('pw');
    await res.settled;
    expect(sent[0].url).toBe('/api/announcements');
    expect(sent[0].method).toBe('DELETE');
    expect(sent[0].headers['Content-Type']).toBe('application/json');
    expect(sent[0].body.modifiedAt).toBeGreaterThan(0);
  });

  it('clearAnnouncements sends the clear again when the clock is refused', async () => {
    const sent = server((n) => (n === 1
      ? [200, { applied: false, reason: 'clock_skew', message: 'Nothing was changed: clock' }]
      : [200, []]));
    const res = await API.clearAnnouncements('pw');
    await flush();
    await expect(res.settled).resolves.toEqual([]);
    expect(sent.map((r) => `${r.method} ${r.url}`)).toEqual(['DELETE /api/announcements', 'DELETE /api/announcements']);
  });
});
