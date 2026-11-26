// bc-sseq FR9: court_control.jsx, the one owner of "does this tab run the
// shiaijo": the tab, page and tablet ids, the device word, the claim, the
// heartbeat, the take, the release on pagehide only when the tablet has
// nothing queued for the shiaijo, and the mount-scoped answers the outbox asks.
import RealReact from 'react';
import { render, act, cleanup } from '@testing-library/react';
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';

let events;
vi.mock('../api_client_events.jsx', async (orig) => ({
  ...(await orig()),
  _subscribeToEvents: (cb, onStatus) => {
    const rec = { cb, onStatus };
    events.add(rec);
    return () => events.delete(rec);
  },
}));

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const answer = (over = {}) => ({
  court: 'A', holderKey: '', device: '', youHold: false, youWereDisplaced: false, tabWasDisplaced: false,
  sameDevice: false, since: 0, lastActiveAt: 0, ...over,
});
const HOLDS = answer({ holderKey: 'aaaa', device: 'ipad', youHold: true, sameDevice: true, since: 5 });
const OTHER = answer({ holderKey: 'bbbb', device: 'windows', lastActiveAt: 77, since: 9 });

let calls;
let reply;
const settle = () => new Promise((r) => setTimeout(r, 0));
const puts = () => calls.filter((c) => c.method === 'PUT');
const deletes = () => calls.filter((c) => c.method === 'DELETE');
const emit = (event) => { for (const r of Array.from(events)) r.cb(event); };
const status = (s) => { for (const r of Array.from(events)) if (r.onStatus) r.onStatus(s); };

let cc;
const load = async () => { cc = await import('../court_control.jsx'); return cc; };

beforeEach(() => {
  vi.resetModules();
  events = new Set();
  calls = [];
  reply = () => ({ body: HOLDS });
  localStorage.clear();
  sessionStorage.clear();
  global.BroadcastChannel = undefined;
  global.fetch = vi.fn(async (url, init = {}) => {
    const body = init.body ? JSON.parse(init.body) : null;
    calls.push({ url, method: init.method || 'GET', body, init });
    const r = reply(url, init, body);
    if (r instanceof Error) throw r;
    return { ok: (r.status || 200) < 400, status: r.status || 200, json: async () => r.body };
  });
});
// Every console a test mounts is unmounted after it, and its release timer
// run, so a module of an earlier test never answers a later one's page events.
let offs = [];
const track = (off) => { offs.push(off); return off; };
afterEach(async () => {
  vi.useRealTimers();
  Object.defineProperty(navigator, 'onLine', { value: true, configurable: true });
  for (const off of offs) off();
  offs = [];
  await settle();
});

describe('the device word', () => {
  it('reads a Mac user agent with touch points as an iPad (iPadOS Safari)', async () => {
    const { deviceWord } = await load();
    const mac = 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15';
    expect(deviceWord({ userAgent: mac, maxTouchPoints: 5 })).toBe('ipad');
    expect(deviceWord({ userAgent: mac, maxTouchPoints: 0 })).toBe('mac');
  });

  it('names each kind of device, and anything else other', async () => {
    const { deviceWord } = await load();
    expect(deviceWord({ userAgent: 'Mozilla/5.0 (iPad; CPU OS 12_0 like Mac OS X)' })).toBe('ipad');
    expect(deviceWord({ userAgent: 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X)' })).toBe('iphone');
    expect(deviceWord({ userAgent: 'Mozilla/5.0 (Linux; Android 14; SM-X710) Safari/537.36' })).toBe('android-tablet');
    expect(deviceWord({ userAgent: 'Mozilla/5.0 (Linux; Android 14; Pixel 8) Mobile Safari/537.36' })).toBe('android-phone');
    expect(deviceWord({ userAgent: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64)' })).toBe('windows');
    expect(deviceWord({ userAgent: 'Mozilla/5.0 (X11; Linux x86_64)' })).toBe('other');
  });
});

describe('the ids', () => {
  it('keeps the tab id in sessionStorage and the tablet id in localStorage, apart from each other', async () => {
    const { courtTabId, courtDeviceId, courtPageId, COURT_TAB_KEY, COURT_DEVICE_KEY } = await load();
    const tab = courtTabId();
    const device = courtDeviceId();
    expect(tab).toMatch(UUID);
    expect(device).toMatch(UUID);
    expect(courtPageId()).toMatch(UUID);
    expect(tab).not.toBe(device);
    expect(sessionStorage.getItem(COURT_TAB_KEY)).toBe(tab);
    expect(localStorage.getItem(COURT_DEVICE_KEY)).toBe(device);
  });

  it('a reload keeps the tab and the tablet and gets a new page load; a new tab gets its own tab id', async () => {
    const first = await load();
    const tab = first.courtTabId();
    const device = first.courtDeviceId();
    const page = first.courtPageId();
    vi.resetModules();
    const reloaded = await load();
    expect(reloaded.courtTabId()).toBe(tab);
    expect(reloaded.courtDeviceId()).toBe(device);
    expect(reloaded.courtPageId()).not.toBe(page);
    sessionStorage.clear();
    vi.resetModules();
    const newTab = await load();
    expect(newTab.courtTabId()).not.toBe(tab);
    expect(newTab.courtDeviceId(), 'every tab of the browser is one tablet').toBe(device);
  });

  it('a tab whose localStorage read throws mints its own tablet id, not the shared one', async () => {
    const shared = (await load()).courtDeviceId();
    vi.resetModules();
    const saved = global.localStorage;
    global.localStorage = { getItem: () => { throw new Error('blocked'); }, setItem: () => { throw new Error('blocked'); } };
    try {
      const own = await load();
      const id = own.courtDeviceId();
      expect(id).toMatch(UUID);
      expect(id).not.toBe(shared);
      expect(sessionStorage.getItem(own.COURT_DEVICE_TAB_KEY)).toBe(id);
    } finally {
      global.localStorage = saved;
    }
  });

  it('a duplicated tab mints a fresh tab id when an older page says the id is its own', async () => {
    const channels = [];
    global.BroadcastChannel = class {
      constructor() { this.onmessage = null; channels.push(this); }
      postMessage(m) { this.sent = (this.sent || []).concat([m]); }
      close() {}
    };
    const { courtTabId, courtPageId, COURT_TAB_KEY } = await load();
    const copied = courtTabId();
    const [ch] = channels;
    expect(ch.sent[0]).toMatchObject({ type: 'who', tabId: copied, pageId: courtPageId() });
    // A page of another id says nothing to it.
    ch.onmessage({ data: { type: 'mine', tabId: 'not-this-tab', pageId: 'p-old' } });
    expect(courtTabId()).toBe(copied);
    ch.onmessage({ data: { type: 'mine', tabId: copied, pageId: 'p-old' } });
    expect(courtTabId()).not.toBe(copied);
    expect(courtTabId()).toMatch(UUID);
    expect(sessionStorage.getItem(COURT_TAB_KEY)).toBe(courtTabId());
  });

  it('the older page answers a younger copy that its id is its own', async () => {
    const channels = [];
    global.BroadcastChannel = class {
      constructor() { this.onmessage = null; channels.push(this); }
      postMessage(m) { this.sent = (this.sent || []).concat([m]); }
      close() {}
    };
    const { courtTabId } = await load();
    const tab = courtTabId();
    const [ch] = channels;
    ch.onmessage({ data: { type: 'who', tabId: tab, pageId: 'p-young', loadedAt: Date.now() + 1000 } });
    expect(ch.sent.at(-1)).toMatchObject({ type: 'mine', tabId: tab });
    expect(courtTabId()).toBe(tab);
  });
});

describe('the claim and the state', () => {
  it('mounting claims the shiaijo with the tab, page and tablet ids and the device word', async () => {
    const { mountCourtControl, courtTabId, courtDeviceId, courtPageId, courtControlView } = await load();
    const off = track(mountCourtControl('A', () => 'pw'));
    await settle();
    const [put] = puts();
    expect(put.url).toBe('/api/courts/A/control');
    expect(put.init.headers['X-Tournament-Password']).toBe('pw');
    expect(put.body).toEqual({
      tabId: courtTabId(), pageId: courtPageId(), deviceId: courtDeviceId(), take: false, device: expect.any(String),
    });
    expect(courtControlView('A')).toMatchObject({ holds: true, state: 'holds', holderDevice: 'ipad', connected: true });
    off();
  });

  it('reads each answer as one state', async () => {
    const { courtControlState } = await load();
    expect(courtControlState(null)).toBe('unknown');
    expect(courtControlState(HOLDS)).toBe('holds');
    expect(courtControlState(OTHER)).toBe('other');
    expect(courtControlState(answer({ holderKey: 'aaaa', sameDevice: true }))).toBe('other');
    expect(courtControlState(answer({ youWereDisplaced: true }))).toBe('free-displaced');
    expect(courtControlState(answer({ tabWasDisplaced: true }))).toBe('free-displaced');
    expect(courtControlState(answer())).toBe('unknown');
  });

  it('keeps tabWasDisplaced separately on free-displaced and held answers', async () => {
    reply = () => ({ body: answer({ tabWasDisplaced: true }) });
    const { mountCourtControl, consoleWriteRefusal, courtControlView } = await load();
    const off = track(mountCourtControl('A', () => 'pw'));
    await settle();
    expect(courtControlView('A')).toMatchObject({
      state: 'free-displaced', youWereDisplaced: false, tabWasDisplaced: true,
    });
    expect(consoleWriteRefusal()).toEqual({ applied: false, reason: 'court_not_held' });

    reply = () => ({ body: answer({ holderKey: 'bbbb', device: 'ipad', tabWasDisplaced: true }) });
    emit({ type: 'resync_required' });
    await settle();
    expect(courtControlView('A')).toMatchObject({
      state: 'other', youWereDisplaced: false, tabWasDisplaced: true,
    });
    off();
  });

  it('records HTTP refusal status on 401 and 500 while 404 and network errors clear it', async () => {
    reply = () => ({ status: 401, body: { error: 'Unauthorized' } });
    const { mountCourtControl, consoleWriteRefusal, courtControlView } = await load();
    const off = track(mountCourtControl('A', () => 'pw'));
    await settle();
    expect(courtControlView('A')).toMatchObject({ state: 'unknown', connected: false, refusedStatus: 401 });
    expect(consoleWriteRefusal()).toBeNull();

    reply = () => ({ status: 500, body: { error: 'boom' } });
    emit({ type: 'resync_required' });
    await settle();
    expect(courtControlView('A')).toMatchObject({ state: 'unknown', connected: false, refusedStatus: 500 });
    expect(consoleWriteRefusal()).toBeNull();

    reply = () => ({ status: 404, body: { error: 'not found' } });
    emit({ type: 'resync_required' });
    await settle();
    expect(courtControlView('A')).toMatchObject({ state: 'not-listed', connected: true, refusedStatus: 0 });
    expect(consoleWriteRefusal()).toBeNull();

    reply = () => new TypeError('Failed to fetch');
    emit({ type: 'resync_required' });
    await settle();
    expect(courtControlView('A')).toMatchObject({ state: 'unknown', connected: false, refusedStatus: 0 });

    reply = () => ({ body: HOLDS });
    emit({ type: 'resync_required' });
    await settle();
    expect(courtControlView('A')).toMatchObject({ state: 'holds', connected: true, refusedStatus: 0 });
    off();
  });

  it('a 404 claim records not-listed without refusing writes, clears cached answer, and a later 200 restores holds', async () => {
    const {
      mountCourtControl, consoleWriteRefusal, courtControlView, courtReleasable, courtControlFor, COURT_ANSWER_PREFIX,
    } = await load();
    const off = track(mountCourtControl('A', () => 'pw'));
    await settle();
    expect(courtControlView('A').state).toBe('holds');
    expect(sessionStorage.getItem(COURT_ANSWER_PREFIX + 'A')).not.toBeNull();

    reply = () => ({ status: 404, body: { error: 'not found' } });
    emit({ type: 'resync_required' });
    await settle();
    expect(courtControlView('A')).toMatchObject({ state: 'not-listed', holds: false, connected: true, refusedStatus: 0 });
    expect(sessionStorage.getItem(COURT_ANSWER_PREFIX + 'A')).toBeNull();
    expect(consoleWriteRefusal()).toBeNull();
    expect(courtReleasable('A')).toBe(false);
    expect(courtControlFor('A')).toBeNull();

    reply = () => ({ body: HOLDS });
    emit({ type: 'resync_required' });
    await settle();
    expect(courtControlView('A')).toMatchObject({ state: 'holds', holds: true, connected: true, refusedStatus: 0 });
    expect(consoleWriteRefusal()).toBeNull();
    off();
  });

  it('a holding console stamps its writes and is never refused', async () => {
    const { mountCourtControl, courtControlFor, consoleWriteRefusal, courtOfControlStamp, courtTabId, courtDeviceId } = await load();
    const off = track(mountCourtControl('A', () => 'pw'));
    await settle();
    const stamp = courtControlFor('A');
    expect(stamp).toEqual({ tabId: courtTabId(), deviceId: courtDeviceId() });
    expect(courtOfControlStamp(stamp)).toBe('A');
    expect(courtOfControlStamp({ ...stamp }), 'a copy is not a stamp the module handed out').toBe('');
    expect(consoleWriteRefusal()).toBeNull();
    expect(courtControlFor('B'), 'a shiaijo the console does not show').toBeNull();
    off();
  });

  it('a console another device runs is refused every write and stamps none', async () => {
    reply = () => ({ body: OTHER });
    const { mountCourtControl, courtControlFor, consoleWriteRefusal, courtControlView } = await load();
    const off = track(mountCourtControl('A', () => 'pw'));
    await settle();
    expect(courtControlView('A')).toMatchObject({ holds: false, state: 'other', holderDevice: 'windows', holderLastActiveAt: 77 });
    expect(consoleWriteRefusal()).toEqual({ applied: false, reason: 'court_not_held' });
    expect(courtControlFor('A')).toBeNull();
    off();
    expect(consoleWriteRefusal(), 'no console mounted: never refused').toBeNull();
    expect(courtControlFor('A'), 'no console mounted: never stamped').toBeNull();
  });

  it('a free shiaijo this tablet lost earlier is refused too', async () => {
    reply = () => ({ body: answer({ youWereDisplaced: true }) });
    const { mountCourtControl, consoleWriteRefusal, courtControlView } = await load();
    const off = track(mountCourtControl('A', () => 'pw'));
    await settle();
    expect(courtControlView('A').state).toBe('free-displaced');
    expect(consoleWriteRefusal()).toEqual({ applied: false, reason: 'court_not_held' });
    off();
  });

  it('a console with no answer (offline) scores and stamps its writes, so they claim the shiaijo on arrival', async () => {
    reply = () => new TypeError('Failed to fetch');
    const { mountCourtControl, courtControlFor, consoleWriteRefusal, courtControlView } = await load();
    const off = track(mountCourtControl('A', () => 'pw'));
    await settle();
    expect(courtControlView('A')).toMatchObject({ state: 'unknown', connected: false });
    expect(consoleWriteRefusal()).toBeNull();
    expect(courtControlFor('A')).not.toBeNull();
    off();
  });

  it('a reload offline still shows the last answer', async () => {
    reply = () => ({ body: OTHER });
    const first = await load();
    const off = track(first.mountCourtControl('A', () => 'pw'));
    await settle();
    off();
    vi.resetModules();
    reply = () => new TypeError('Failed to fetch');
    const reloaded = await load();
    const off2 = track(reloaded.mountCourtControl('A', () => 'pw'));
    await settle();
    expect(reloaded.courtControlView('A').state).toBe('other');
    expect(reloaded.consoleWriteRefusal()).not.toBeNull();
    off2();
  });

  it('an answer older than the last one is not taken', async () => {
    let n = 0;
    const slow = [];
    global.fetch = vi.fn(() => new Promise((res) => {
      const body = n++ === 0 ? OTHER : HOLDS;
      slow.push(() => res({ ok: true, status: 200, json: async () => body }));
    }));
    const { mountCourtControl, courtControlView } = await load();
    const off = track(mountCourtControl('A', () => 'pw'));
    await settle();
    emit({ type: 'resync_required' });
    await settle();
    slow[1]();
    await settle();
    slow[0]();
    await settle();
    expect(courtControlView('A').state).toBe('holds');
    off();
  });

  it('claims again on a holder change for its shiaijo, a resync, a reconnect and a page restored from the cache', async () => {
    const { mountCourtControl } = await load();
    const off = track(mountCourtControl('A', () => 'pw'));
    await settle();
    expect(puts().length).toBe(1);
    emit({ type: 'court_control_updated', data: { court: 'B', holderKey: 'x' } });
    await settle();
    expect(puts().length, 'another shiaijo').toBe(1);
    emit({ type: 'court_control_updated', data: { court: 'A', holderKey: 'other' } });
    await settle();
    expect(puts().length).toBe(2);
    emit({ type: 'resync_required' });
    await settle();
    expect(puts().length).toBe(3);
    status('error');
    status('open');
    await settle();
    expect(puts().length).toBe(4);
    window.dispatchEvent(Object.assign(new Event('pageshow'), { persisted: false }));
    await settle();
    expect(puts().length, 'an ordinary page show').toBe(4);
    window.dispatchEvent(Object.assign(new Event('pageshow'), { persisted: true }));
    await settle();
    expect(puts().length).toBe(5);
    off();
  });

  it('beats every 30 s while shown and online, and stops once unmounted', async () => {
    vi.useFakeTimers();
    const { mountCourtControl, COURT_HEARTBEAT_MS } = await load();
    const off = track(mountCourtControl('A', () => 'pw'));
    await vi.advanceTimersByTimeAsync(1);
    expect(puts().length).toBe(1);
    await vi.advanceTimersByTimeAsync(COURT_HEARTBEAT_MS);
    expect(puts().length).toBe(2);
    Object.defineProperty(navigator, 'onLine', { value: false, configurable: true });
    await vi.advanceTimersByTimeAsync(COURT_HEARTBEAT_MS);
    expect(puts().length, 'offline: no beat').toBe(2);
    Object.defineProperty(navigator, 'onLine', { value: true, configurable: true });
    off();
    await vi.advanceTimersByTimeAsync(COURT_HEARTBEAT_MS * 2);
    expect(puts().length).toBe(2);
  });

  it('takes the shiaijo back at once when the server frees it under the tab that holds it', async () => {
    vi.useFakeTimers();
    const { mountCourtControl } = await load();
    const off = track(mountCourtControl('A', () => 'pw'));
    await vi.advanceTimersByTimeAsync(1);
    expect(puts().length).toBe(1);

    emit({ type: 'court_control_updated', data: { court: 'A', holderKey: '' } });
    await vi.advanceTimersByTimeAsync(1);
    expect(puts().length).toBe(2);
    off();
    await vi.advanceTimersByTimeAsync(1);
  });

  it('a watching console claims at once on a freed-court event: the server kept the grace before announcing it', async () => {
    vi.useFakeTimers();
    reply = () => ({ body: OTHER });
    const cc = await load();
    expect(cc.COURT_FREED_GRACE_MS, 'the grace is the server\'s alone').toBeUndefined();
    const off = track(cc.mountCourtControl('A', () => 'pw'));
    await vi.advanceTimersByTimeAsync(1);
    expect(puts().length).toBe(1);

    emit({ type: 'court_control_updated', data: { court: 'A', holderKey: '' } });
    await vi.advanceTimersByTimeAsync(1);
    expect(puts().length).toBe(2);
    expect(puts()[1].body.take, 'a claim, not a take').toBe(false);
    off();
    await vi.advanceTimersByTimeAsync(1);
  });

  it('never claims after a freed-court event once unmounted', async () => {
    vi.useFakeTimers();
    reply = () => ({ body: OTHER });
    const { mountCourtControl } = await load();
    const off = track(mountCourtControl('A', () => 'pw'));
    await vi.advanceTimersByTimeAsync(1);
    expect(puts().length).toBe(1);

    off();
    emit({ type: 'court_control_updated', data: { court: 'A', holderKey: '' } });
    await vi.advanceTimersByTimeAsync(100);
    expect(puts().length).toBe(1);
  });

  it('a reloading holder gets its shiaijo back while a console opened during the reload is told it is run from another device', async () => {
    vi.useFakeTimers();
    global.BroadcastChannel = class {
      constructor() { this.onmessage = null; }
      postMessage() {}
      close() {}
    };
    // A server that keeps a released shiaijo for its tablet (the grace,
    // court_control.go): the release announces nothing, and a claim from
    // another tablet is answered with the hold that was given back.
    let holder = null;
    let graced = null;
    const view = (h, body) => answer({
      holderKey: h.holderKey, device: h.device, youHold: h.tabId === body.tabId, sameDevice: h.deviceId === body.deviceId,
    });
    reply = (url, init, body) => {
      if (init.method === 'DELETE') {
        if (holder && holder.tabId === body.tabId) { graced = holder; holder = null; }
        return { body: answer() };
      }
      if (graced) {
        if (graced.tabId !== body.tabId && graced.deviceId !== body.deviceId && !body.take) return { body: view(graced, body) };
        holder = { ...graced, tabId: body.tabId, holderKey: `key-${body.tabId}` };
        graced = null;
      }
      if (!holder) holder = { tabId: body.tabId, deviceId: body.deviceId, holderKey: `key-${body.tabId}`, device: body.device };
      return { body: view(holder, body) };
    };

    const reloader1 = await load();
    const offA1 = reloader1.mountCourtControl('A', () => 'pw');
    await vi.advanceTimersByTimeAsync(reloader1.TAB_CHECK_MS + 1);
    expect(reloader1.courtControlView('A').holds).toBe(true);
    const tabA = sessionStorage.getItem(reloader1.COURT_TAB_KEY);
    const deviceA = localStorage.getItem(reloader1.COURT_DEVICE_KEY);

    // The page goes: pagehide gives the shiaijo back.
    window.dispatchEvent(new Event('pagehide'));
    offA1();
    await vi.advanceTimersByTimeAsync(1);
    expect(holder).toBeNull();
    expect(graced && graced.tabId).toBe(tabA);

    // A console on another tablet opened while the tablet reloads: it does not take it.
    sessionStorage.clear();
    localStorage.clear();
    vi.resetModules();
    const watcher = await load();
    const offW = watcher.mountCourtControl('A', () => 'pw');
    await vi.advanceTimersByTimeAsync(watcher.TAB_CHECK_MS + 1);
    expect(watcher.courtControlView('A').state, 'the grey bar, with Take control').toBe('other');
    expect(holder).toBeNull();

    // The reloaded page (the same tab and tablet ids) takes it back.
    sessionStorage.setItem(reloader1.COURT_TAB_KEY, tabA);
    localStorage.setItem(reloader1.COURT_DEVICE_KEY, deviceA);
    vi.resetModules();
    const reloader2 = await load();
    const offA2 = reloader2.mountCourtControl('A', () => 'pw');
    await vi.advanceTimersByTimeAsync(reloader2.TAB_CHECK_MS + 1);
    expect(reloader2.courtControlView('A').holds).toBe(true);
    expect(holder && holder.tabId).toBe(tabA);
    expect(watcher.courtControlView('A').state).toBe('other');

    offW();
    offA2();
    await vi.advanceTimersByTimeAsync(1);
  });

  it('a late timeout settling after a newer 401 leaves refusedStatus 401', async () => {
    vi.useFakeTimers();
    let n = 0;
    global.fetch = vi.fn((url, init = {}) => {
      const body = init.body ? JSON.parse(init.body) : null;
      calls.push({ url, method: init.method || 'GET', body, init });
      if (n++ === 0) return new Promise(() => {});
      return Promise.resolve({ ok: false, status: 401, json: async () => ({ error: 'Unauthorized' }) });
    });
    const { mountCourtControl, courtControlView, CLAIM_DEADLINE_MS } = await load();
    const off = track(mountCourtControl('A', () => 'pw'));
    await vi.advanceTimersByTimeAsync(1);
    expect(puts().length).toBe(1);

    emit({ type: 'resync_required' });
    await vi.advanceTimersByTimeAsync(1);
    expect(puts().length).toBe(2);
    expect(courtControlView('A')).toMatchObject({ state: 'unknown', connected: false, refusedStatus: 401 });

    await vi.advanceTimersByTimeAsync(CLAIM_DEADLINE_MS);
    expect(courtControlView('A')).toMatchObject({ state: 'unknown', connected: false, refusedStatus: 401 });
    off();
    await vi.advanceTimersByTimeAsync(1);
  });

  it('changing the password after a 401 sends a new claim at once, and an unchanged password does not', async () => {
    reply = (_url, init) => (
      init.headers && init.headers['X-Tournament-Password'] === 'good'
        ? { body: HOLDS }
        : { status: 401, body: { error: 'Unauthorized' } }
    );
    const prevReact = global.React;
    global.React = RealReact;
    try {
      const { useCourtControl } = await load();
      let latest = null;
      function Probe({ court, password }) {
        latest = useCourtControl(court, password);
        return RealReact.createElement('div', { 'data-state': latest.state });
      }
      let view;
      await act(async () => {
        view = render(RealReact.createElement(Probe, { court: 'A', password: 'stale' }));
      });
      await settle();
      expect(puts().length).toBe(1);
      expect(latest).toMatchObject({ state: 'unknown', connected: false, refusedStatus: 401 });

      await act(async () => {
        view.rerender(RealReact.createElement(Probe, { court: 'A', password: 'stale' }));
      });
      await settle();
      expect(puts().length, 'unchanged password sends no claim').toBe(1);

      await act(async () => {
        view.rerender(RealReact.createElement(Probe, { court: 'A', password: 'good' }));
      });
      await settle();
      expect(puts().length, 'password change after 401 claims at once').toBe(2);
      expect(puts()[1].init.headers['X-Tournament-Password']).toBe('good');
      expect(latest).toMatchObject({ holds: true, state: 'holds', connected: true, refusedStatus: 0 });

      await act(async () => {
        view.rerender(RealReact.createElement(Probe, { court: 'A', password: 'good2' }));
      });
      await settle();
      expect(puts().length, 'password change while holding sends no extra claim').toBe(2);

      await act(async () => {
        view.unmount();
        cleanup();
      });
      await settle();
    } finally {
      global.React = prevReact;
    }
  });
});

describe('Take control', () => {
  it('aborts a hanging take after CLAIM_DEADLINE_MS and re-claims to learn who holds', async () => {
    vi.useFakeTimers();
    let heldAfter = false;
    reply = () => ({ body: heldAfter ? HOLDS : OTHER });
    global.fetch = vi.fn((url, init = {}) => {
      const body = init.body ? JSON.parse(init.body) : null;
      calls.push({ url, method: init.method || 'GET', body, init });
      if (body && body.take) return new Promise(() => {});
      const r = reply(url, init, body);
      return Promise.resolve({ ok: (r.status || 200) < 400, status: r.status || 200, json: async () => r.body });
    });

    const { mountCourtControl, takeCourtControl, courtControlView, CLAIM_DEADLINE_MS, TAKE_TIMED_OUT } = await load();
    const off = track(mountCourtControl('A', () => 'pw'));
    await vi.advanceTimersByTimeAsync(1);
    expect(courtControlView('A').state).toBe('other');

    heldAfter = true;
    let caught = null;
    takeCourtControl('A').catch((e) => { caught = e; });

    await vi.advanceTimersByTimeAsync(CLAIM_DEADLINE_MS - 1);
    expect(caught).toBeNull();
    await vi.advanceTimersByTimeAsync(1);
    expect(caught).toBeInstanceOf(Error);
    expect(caught.message).toBe(TAKE_TIMED_OUT);
    await vi.advanceTimersByTimeAsync(1);
    expect(courtControlView('A').state).toBe('holds');
    off();
    await vi.advanceTimersByTimeAsync(1);
  });
  it('sends a take with the time on this tablet\'s clock and holds after it', async () => {
    reply = (url, init, body) => ({ body: body && body.take ? HOLDS : OTHER });
    const { mountCourtControl, takeCourtControl, courtControlView } = await load();
    const off = track(mountCourtControl('A', () => 'pw'));
    await settle();
    expect(courtControlView('A').state).toBe('other');
    const before = Date.now();
    await takeCourtControl('A');
    const take = puts().find((c) => c.body.take);
    expect(take.body.takenAt).toBeGreaterThanOrEqual(before - 1);
    expect(Number.isInteger(take.body.takenAt)).toBe(true);
    expect(courtControlView('A').state).toBe('holds');
    off();
  });

  it('needs a connection: offline it says so and sends nothing', async () => {
    const { takeCourtControl, TAKE_NEEDS_CONNECTION } = await load();
    Object.defineProperty(navigator, 'onLine', { value: false, configurable: true });
    await expect(takeCourtControl('A')).rejects.toThrow(TAKE_NEEDS_CONNECTION);
    expect(calls.length).toBe(0);
  });

  it('a refused take throws the server\'s sentence', async () => {
    reply = (url, init, body) => (body && body.take ? { status: 404, body: { code: 'unknown_court', error: 'This tournament has no such shiaijo.' } } : { body: OTHER });
    const { takeCourtControl } = await load();
    await expect(takeCourtControl('Z')).rejects.toThrow('This tournament has no such shiaijo.');
  });
});

describe('giving the shiaijo back', () => {
  const queueKey = 'bc_write_queue_v2';
  const entry = (court) => ({
    compID: 'c1', matchID: 'm1', kind: 'score', terminal: true, method: 'PUT', enqueuedAt: 1,
    url: '/api/competitions/c1/matches/m1/score', payload: { courtControl: { tabId: 't', deviceId: 'd' } },
    courtControlCourt: court,
  });

  it('closing the tab gives it back, naming this page load, when the tablet has nothing queued for it', async () => {
    const { mountCourtControl, courtTabId, courtPageId } = await load();
    track(mountCourtControl('A', () => 'pw'));
    await settle();
    window.dispatchEvent(new Event('pagehide'));
    const [del] = deletes();
    expect(del.url).toBe('/api/courts/A/control');
    expect(del.init.keepalive).toBe(true);
    expect(del.body).toEqual({ tabId: courtTabId(), pageId: courtPageId() });
  });

  it('keeps it while any tab of the tablet holds an entry stamped for that shiaijo', async () => {
    localStorage.setItem(queueKey, JSON.stringify([['k1', entry('A')]]));
    const { mountCourtControl, courtReleasable } = await load();
    track(mountCourtControl('A', () => 'pw'));
    await settle();
    expect(courtReleasable('A')).toBe(false);
    window.dispatchEvent(new Event('pagehide'));
    expect(deletes().length).toBe(0);
  });

  it('an entry for another shiaijo does not keep it', async () => {
    localStorage.setItem(queueKey, JSON.stringify([['k1', entry('B')]]));
    const { mountCourtControl } = await load();
    track(mountCourtControl('A', () => 'pw'));
    await settle();
    window.dispatchEvent(new Event('pagehide'));
    expect(deletes().length).toBe(1);
  });

  it('a tab that does not run it gives nothing back', async () => {
    reply = () => ({ body: OTHER });
    const { mountCourtControl } = await load();
    track(mountCourtControl('A', () => 'pw'));
    await settle();
    window.dispatchEvent(new Event('pagehide'));
    expect(deletes().length).toBe(0);
  });

  it('leaving the console gives it back on the same rule, and a quick remount gives nothing back', async () => {
    const { mountCourtControl } = await load();
    const off = track(mountCourtControl('A', () => 'pw'));
    await settle();
    off();
    const again = track(mountCourtControl('A', () => 'pw'));
    await settle();
    expect(deletes().length, 'remounted at once').toBe(0);
    again();
    await settle();
    expect(deletes().length).toBe(1);
  });

  it('unmounting while a claim is in flight gives the shiaijo back once the hold arrives, while a mounted claim keeps it', async () => {
    const pending = [];
    global.fetch = vi.fn((url, init = {}) => {
      const body = init.body ? JSON.parse(init.body) : null;
      const method = init.method || 'GET';
      calls.push({ url, method, body, init });
      if (method === 'PUT') {
        return new Promise((res) => {
          pending.push((r) => res({ ok: true, status: 200, json: async () => r }));
        });
      }
      return Promise.resolve({ ok: true, status: 200, json: async () => ({}) });
    });
    const { mountCourtControl, courtControlView } = await load();
    const offA = track(mountCourtControl('A', () => 'pw'));
    await settle();
    expect(puts().filter((c) => c.url === '/api/courts/A/control')).toHaveLength(1);
    offA();
    await settle();
    expect(deletes()).toHaveLength(0);
    pending[0](HOLDS);
    await settle();
    expect(deletes()).toHaveLength(1);
    expect(deletes()[0].url).toBe('/api/courts/A/control');
    expect(courtControlView('A').state).toBe('unknown');

    const offB = track(mountCourtControl('B', () => 'pw'));
    await settle();
    pending[1](answer({ ...HOLDS, court: 'B' }));
    await settle();
    expect(deletes()).toHaveLength(1);
    expect(courtControlView('B').state).toBe('holds');
    offB();
  });

  it('claiming B while this tab holds A with a stamped queued entry for A sends keep ["A"]', async () => {
    reply = (url) => ({ body: answer({ ...HOLDS, court: url.includes('/B/') ? 'B' : 'A' }) });
    localStorage.setItem(queueKey, JSON.stringify([['k1', entry('A')]]));
    const { mountCourtControl } = await load();
    const offA = track(mountCourtControl('A', () => 'pw'));
    await settle();
    offA();
    await settle();
    const offB = track(mountCourtControl('B', () => 'pw'));
    await settle();
    const putB = puts().find((c) => c.url === '/api/courts/B/control');
    expect(putB.body.keep).toEqual(['A']);
    offB();
  });

  it('claiming B while this tab holds A with nothing queued for A omits keep', async () => {
    reply = (url) => ({ body: answer({ ...HOLDS, court: url.includes('/B/') ? 'B' : 'A' }) });
    const { mountCourtControl } = await load();
    const offA = track(mountCourtControl('A', () => 'pw'));
    await settle();
    const offB = track(mountCourtControl('B', () => 'pw'));
    await settle();
    const putB = puts().find((c) => c.url === '/api/courts/B/control');
    expect(putB.body.take).toBe(false);
    expect('keep' in putB.body).toBe(false);
    offA();
    offB();
  });
});
