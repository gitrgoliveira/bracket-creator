// Taking an announcement down is asked for its OUTCOME: the banner is gone. A
// 404 says it already is (it expired, another console withdrew it, or the
// identical call that replaced it was withdrawn first), so it is not a failure
// for either caller: the court console's withdrawal of its own call, and the
// organiser's Dismiss, which refreshes the list after it. The withdrawal is an
// outbox entry (bc-obox A1): it answers at once, and `settled` carries the
// outcome.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

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
  API = (await import('../api_client.jsx')).API;
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

function answer(status, body) {
  global.fetch = vi.fn((url) => (String(url).includes('/api/time')
    ? Promise.reject(new TypeError('clock'))
    : Promise.resolve({ ok: status >= 200 && status < 300, status, json: async () => body })));
}

describe('deleteAnnouncement', () => {
  it('settles done when the announcement is already gone (404)', async () => {
    answer(404, { error: 'Announcement not found' });
    const res = await API.deleteAnnouncement('gone', 'pw');
    expect(res.pending).toBe(true);
    await expect(res.settled).resolves.toBe(true);
    expect(API.unsentWrites().total).toBe(0);
  });

  it('settles done when it is taken down (200)', async () => {
    answer(200, []);
    const res = await API.deleteAnnouncement('a1', 'pw');
    await expect(res.settled).resolves.toBe(true);
  });

  it('still rejects on a refusal (400), and the outbox lets it go', async () => {
    const warned = vi.spyOn(console, 'warn').mockImplementation(() => {});
    answer(400, { error: 'Invalid request payload' });
    const res = await API.deleteAnnouncement('x', 'pw');
    await expect(res.settled).rejects.toThrow('Invalid request payload');
    expect(API.unsentWrites().total).toBe(0);
    warned.mockRestore();
  });
});
