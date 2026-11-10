// The three check-in doors need the time of the action (modifiedAt) in a JSON
// body; the server refuses a check-in without one with a 400. Since bc-obox
// they are outbox doors: each answers at once ({ pending, settled }) and
// `settled` carries the server's answer, so what is pinned here is the wire
// request and what `settled` resolves with. The superseded answer that goes
// the other way (told once, the row put back) is pinned by
// outbox_checkin_announce_settings.test.jsx, which also covers the stored
// descriptor's stamp and the single check-in's clock_skew resend.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

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

async function flushMicrotasks() { for (let i = 0; i < 30; i++) await Promise.resolve(); }
async function tick(ms = 0) { await vi.advanceTimersByTimeAsync(ms); await flushMicrotasks(); }
const respond = (status, body) => Promise.resolve({ ok: status >= 200 && status < 300, status, json: async () => body });
const isClock = (url) => String(url).includes('/api/time');
// The server: `impl(n)` answers the n-th write [status, body]; every write is
// kept with its headers. The clock answers when `clock` is set.
function server(impl, { clock = false } = {}) {
  const sent = [];
  global.fetch = vi.fn((url, init = {}) => {
    if (isClock(url)) return clock ? respond(200, { nowMs: Date.now() }) : Promise.reject(new TypeError('clock'));
    sent.push({ url: String(url), method: init.method, headers: init.headers || {}, body: init.body ? JSON.parse(init.body) : undefined });
    return respond(...impl(sent.length));
  });
  return sent;
}
const alertsHeard = () => { const a = []; mod.subscribeQueueAlert((x) => a.push(x)); return a; };

describe('check-in requests carry their stamp', () => {
  it('sends modifiedAt in a JSON body on a check-in and an undo', async () => {
    const sent = server(() => [200, { id: 'p1' }]);
    const tap = await API.toggleCheckIn('c1', 'p1', true, 'pw');
    const undo = await API.toggleCheckIn('c1', 'p1', false, 'pw');
    await tap.settled;
    await undo.settled;
    expect(sent.map((c) => c.method)).toEqual(['PUT', 'DELETE']);
    for (const c of sent) {
      expect(c.headers['Content-Type']).toBe('application/json');
      expect(typeof c.body.modifiedAt).toBe('number');
    }
  });

  it('sends modifiedAt beside the ids on a bulk check-in', async () => {
    const sent = server(() => [200, { checkedIn: 2, alreadyCheckedIn: 0, notFound: [] }]);
    const res = await API.bulkCheckIn('c1', ['p1', 'p2'], 'pw');
    await res.settled;
    expect(sent[0].body.participantIds).toEqual(['p1', 'p2']);
    expect(typeof sent[0].body.modifiedAt).toBe('number');
  });

  // A superseded answer whose stored person already holds the asked state is
  // the tap done, in either direction: it settles quietly, nobody is told.
  it('treats a superseded tap whose stored person already holds the asked state as done', async () => {
    const alerts = alertsHeard();
    server(() => [200, { applied: false, reason: 'superseded', participant: { id: 'p1', checkedIn: true } }]);
    const on = await API.toggleCheckIn('c1', 'p1', true, 'pw');
    await expect(on.settled).resolves.toMatchObject({ reason: 'superseded', participant: { checkedIn: true } });
    server(() => [200, { applied: false, reason: 'superseded', participant: { id: 'p1', checkedIn: false } }]);
    const off = await API.toggleCheckIn('c1', 'p1', false, 'pw');
    await expect(off.settled).resolves.toMatchObject({ reason: 'superseded', participant: { checkedIn: false } });
    await tick(0);
    expect(alerts.filter((a) => a.kind === 'checkin_superseded')).toEqual([]);
  });

  it('tells a superseded tap whose stored person carries no check-in, after a check-in tap', async () => {
    // An unchecked person after a check-in tap is a real loss, not a tie.
    const alerts = alertsHeard();
    server(() => [200, { applied: false, reason: 'superseded', participant: { id: 'p1', name: 'Ren Abe' } }]);
    const res = await API.toggleCheckIn('c1', 'p1', true, 'pw');
    await res.settled;
    await tick(0);
    expect(alerts.filter((a) => a.kind === 'checkin_superseded')).toHaveLength(1);
  });

  // The doors once threw on a clock_skew answer and only relearned the clock.
  // The outbox relearns it, re-stamps the write and sends it again in its place.
  it.each([
    ['a check-in', () => API.toggleCheckIn('c1', 'p1', true, 'pw'), { id: 'p1', checkedIn: true }],
    ['a bulk check-in', () => API.bulkCheckIn('c1', ['p1'], 'pw'), { checkedIn: 1, alreadyCheckedIn: 0, notFound: [] }],
    ['a court call', () => API.sendAnnouncement('Hi', 5, 'pw'), { message: 'Hi' }],
    ['a clear', () => API.clearAnnouncements('pw'), []],
  ])('relearns the clock and sends %s again when it is refused for clock_skew', async (_name, call, landed) => {
    const sent = server((n) => (n === 1 ? [200, { applied: false, reason: 'clock_skew', message: 'm' }] : [200, landed]), { clock: true });
    const res = await call();
    await tick(0);
    await expect(res.settled).resolves.toEqual(landed);
    expect(global.fetch.mock.calls.some(([url]) => isClock(url))).toBe(true);
    expect(sent).toHaveLength(2);
    expect(sent[1].url).toBe(sent[0].url);
    expect(sent[1].method).toBe(sent[0].method);
  });

  it('settles a bulk answer that applied some ids with the answer, superseded ones included', async () => {
    const body = { checkedIn: 1, alreadyCheckedIn: 0, notFound: [], superseded: [{ id: 'p2', checkedIn: true }] };
    server(() => [200, body]);
    const res = await API.bulkCheckIn('c1', ['p1', 'p2'], 'pw');
    await expect(res.settled).resolves.toEqual(body);
  });
});
