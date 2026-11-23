// bc-hnam, the client half of hiding competitor names from the public pages:
// the reveal keys a phone holds (reveal_key.jsx), the reveal link page
// (viewer_reveal.jsx) and its route (app.jsx), which credential each page's
// reads carry (api_client_requests.jsx), and the event stream's ticket on
// every connect (api_client_events.jsx).
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { parsePath, pathFromState } from '../app.jsx';

const KEY_A = 'a'.repeat(64);
const KEY_B = 'b'.repeat(64);
const KEY_C = 'c'.repeat(64);

// Each test reads the modules fresh: the keys held and the read credential
// are module state.
async function fresh() {
  vi.resetModules();
  const keys = await import('../reveal_key.jsx');
  const requests = await import('../api_client_requests.jsx');
  const reveal = await import('../viewer_reveal.jsx');
  return { keys, requests, reveal };
}

beforeEach(() => { window.localStorage.clear(); });
afterEach(() => { vi.restoreAllMocks(); window.localStorage.clear(); });

describe('reveal_key.jsx: the keys this phone holds', () => {
  it('keeps a well-formed key, once, and refuses a malformed one', async () => {
    const { keys } = await fresh();
    expect(keys.rememberRevealKey('juniors', KEY_A)).toBe(true);
    expect(keys.rememberRevealKey('juniors', KEY_A)).toBe(true);
    expect(keys.rememberRevealKey('juniors', 'ABC')).toBe(false);
    expect(keys.rememberRevealKey('juniors', KEY_A.toUpperCase())).toBe(false);
    expect(keys.rememberRevealKey('bad id!', KEY_B)).toBe(false);
    expect(keys.revealKeysFor('juniors')).toEqual([KEY_A]);
    expect(keys.revealKeysHeader()).toBe(`juniors:${KEY_A}`);
  });

  it('remembers the keys on the phone across a reload', async () => {
    let { keys } = await fresh();
    keys.rememberRevealKey('juniors', KEY_A);
    keys.rememberRevealKey('seniors', KEY_B);
    ({ keys } = await fresh());
    expect(keys.revealKeysHeader()).toBe(`juniors:${KEY_A},seniors:${KEY_B}`);
  });

  it('holds at most the 32 the server reads, dropping the oldest', async () => {
    const { keys } = await fresh();
    for (let i = 0; i < keys.MAX_REVEAL_KEYS + 1; i += 1) {
      keys.rememberRevealKey(`c${i}`, KEY_A);
    }
    const header = keys.revealKeysHeader().split(',');
    expect(header).toHaveLength(keys.MAX_REVEAL_KEYS);
    expect(header[0]).toBe(`c1:${KEY_A}`);
  });

  it('holds a key for the page when storage refuses it', async () => {
    const { keys } = await fresh();
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('denied'); });
    expect(keys.rememberRevealKey('juniors', KEY_A)).toBe(true);
    expect(keys.revealKeysFor('juniors')).toEqual([KEY_A]);
  });

  it('tells a listener when a key is taken', async () => {
    const { keys } = await fresh();
    const told = vi.fn();
    const stop = keys.onRevealKeysChange(told);
    keys.rememberRevealKey('juniors', KEY_A);
    keys.rememberRevealKey('juniors', KEY_A);
    expect(told).toHaveBeenCalledTimes(1);
    stop();
  });
});

describe('api_client_requests.jsx: which credential a read carries', () => {
  it('an admin route signed in sends the password, and only it', async () => {
    const { keys, requests } = await fresh();
    keys.rememberRevealKey('juniors', KEY_A);
    requests.setReadAudience('admin', 'pw');
    expect(requests.readHeaders()).toEqual({ 'X-Tournament-Password': 'pw' });
    expect(requests.readsAsOrganiser()).toBe(true);
    expect(requests.readAudienceKey('admin', 'pw')).toBe('organiser');
  });

  it('a public page sends the reveal keys held, never the password', async () => {
    const { keys, requests } = await fresh();
    requests.setReadAudience('viewer', 'pw');
    expect(requests.readHeaders()).toEqual({});
    expect(requests.readAudienceKey('viewer', 'pw')).toBe('public');
    keys.rememberRevealKey('juniors', KEY_A);
    expect(requests.readHeaders()).toEqual({ 'X-Reveal-Key': `juniors:${KEY_A}` });
    expect(requests.readAudienceKey('viewer', '')).toBe(`reveal:juniors:${KEY_A}`);
    expect(requests.readsAsOrganiser()).toBe(false);
  });

  it('a court board or the overlay sends nothing, whatever the device holds', async () => {
    const { keys, requests } = await fresh();
    keys.rememberRevealKey('juniors', KEY_A);
    requests.setReadAudience('display', 'pw');
    expect(requests.readHeaders()).toEqual({});
    expect(requests.readAudienceKey('display', 'pw')).toBe('display');
  });

  it('says whether the credential changed, a new key or password included', async () => {
    const { keys, requests } = await fresh();
    expect(requests.setReadAudience('viewer', '')).toBe(true);
    expect(requests.setReadAudience('viewer', '')).toBe(false);
    keys.rememberRevealKey('juniors', KEY_A);
    expect(requests.setReadAudience('viewer', '')).toBe(true);
    expect(requests.setReadAudience('admin', 'pw')).toBe(true);
    expect(requests.setReadAudience('admin', 'pw2')).toBe(true);
    expect(requests.currentReadAudienceKey()).toBe('organiser');
  });

  it('an organiser read that came back masked is refused; a public one is not', async () => {
    const { requests } = await fresh();
    requests.setReadAudience('admin', 'pw');
    expect(() => requests.refuseMaskedForOrganiser([{ id: 'c1' }, { id: 'c2', namesHidden: true }]))
      .toThrow(requests.NAMES_HIDDEN_FOR_ORGANISER);
    expect(requests.refuseMaskedForOrganiser({ id: 'c1' })).toEqual({ id: 'c1' });
    requests.setReadAudience('viewer', '');
    expect(requests.refuseMaskedForOrganiser({ namesHidden: true })).toEqual({ namesHidden: true });
  });
});

describe('viewer_reveal.jsx: the reveal link', () => {
  it('takes the key and removes the fragment, keeping the path', async () => {
    const { keys, reveal } = await fresh();
    const hist = { state: null, replaceState: vi.fn() };
    const got = reveal.takeRevealLink({ pathname: '/reveal/juniors', search: '', hash: `#${KEY_A}` }, hist);
    expect(got).toEqual({ compId: 'juniors', stored: true });
    expect(hist.replaceState).toHaveBeenCalledWith(null, '', '/reveal/juniors');
    expect(keys.revealKeysFor('juniors')).toEqual([KEY_A]);
  });

  it('a link without its key holds nothing and still loses its fragment', async () => {
    const { keys, reveal } = await fresh();
    const hist = { state: null, replaceState: vi.fn() };
    expect(reveal.takeRevealLink({ pathname: '/reveal/juniors', search: '', hash: '#nope' }, hist))
      .toEqual({ compId: 'juniors', stored: false });
    expect(hist.replaceState).toHaveBeenCalledWith(null, '', '/reveal/juniors');
    expect(keys.revealKeysHeader()).toBe('');
    expect(reveal.takeRevealLink({ pathname: '/competition/juniors', hash: `#${KEY_A}` }, hist)).toBeNull();
  });

  it('a per-person link follows the individual the server says its key reveals', async () => {
    const { reveal } = await fresh();
    const detail = {
      namesHidden: true,
      revealedCompetitors: ['p2'],
      config: {
        id: 'juniors',
        players: [
          { id: 'p1', name: 'J1', number: 'J1', nameHidden: true },
          { id: 'p2', name: 'Aiko Sato', number: 'J2' },
        ],
      },
    };
    expect(reveal.revealedRecords(detail).map((p) => p.id)).toEqual(['p2']);
    const landing = { compId: 'juniors', stored: true };
    expect(reveal.revealVerdict('juniors', landing, detail).kind).toBe('follow');
  });

  it('a team is followed when the server says its key reveals it, named members or not', async () => {
    const { reveal } = await fresh();
    const detail = {
      namesHidden: true,
      revealedCompetitors: ['t1', 't2'],
      config: {
        id: 't', kind: 'team', players: [
          { id: 't1', name: 'Kyoto', number: 'T1' },
          { id: 't2', name: 'Osaka', number: 'T2' },
          { id: 't3', name: 'Nara', number: 'T3' },
        ],
      },
      // t1 has no named member: its link reveals nothing to show, and it is
      // followed all the same.
      teamMembers: {
        t1: [{ id: 'm1', name: '', index: 1 }],
        t2: [{ id: 'm3', name: 'Ken Kato', index: 1 }],
      },
    };
    expect(reveal.revealedRecords(detail).map((p) => p.id)).toEqual(['t1', 't2']);
    expect(reveal.revealedRecords({ ...detail, revealedCompetitors: undefined })).toEqual([]);
    expect(reveal.revealedRecords({ ...detail, namesHidden: false })).toEqual([]);
  });

  it('opens a competition that shows names, and says so when a link shows none', async () => {
    const { keys, reveal } = await fresh();
    keys.rememberRevealKey('juniors', KEY_A);
    const landing = { compId: 'juniors', stored: true };
    expect(reveal.revealVerdict('juniors', landing, { namesHidden: false, config: {} }).kind).toBe('open');
    const masked = { namesHidden: true, config: { players: [{ id: 'p1', name: 'J1', number: 'J1', nameHidden: true }] } };
    expect(reveal.revealVerdict('juniors', landing, masked).kind).toBe('nothing');
    expect(reveal.revealVerdict('juniors', landing, null).kind).toBe('missing');
  });

  it('a link that carried no key, on a phone holding none, is incomplete', async () => {
    const { reveal } = await fresh();
    const landing = { compId: 'juniors', stored: false };
    expect(reveal.revealVerdict('juniors', landing, { namesHidden: true, config: {} }).kind).toBe('incomplete');
  });
});

describe('app.jsx: the /reveal route', () => {
  it('parses and prints /reveal/<competition>', () => {
    expect(parsePath('/reveal/juniors')).toEqual({ mode: 'viewer', viewerScreen: 'reveal', viewerCompId: 'juniors' });
    expect(pathFromState('viewer', 'reveal', 'juniors', {}, null)).toBe('/reveal/juniors');
  });
});

// The event stream: a page holding a credential opens it with a ticket asked
// for with that credential, on EVERY connect.
class FakeEventSource {
  static CONNECTING = 0;
  static OPEN = 1;
  static CLOSED = 2;
  constructor(url) {
    this.url = url;
    this.readyState = FakeEventSource.CONNECTING;
    FakeEventSource.instances.push(this);
  }
  error() {
    this.readyState = FakeEventSource.CLOSED;
    if (typeof this.onerror === 'function') this.onerror();
  }
  close() { this.readyState = FakeEventSource.CLOSED; }
}
FakeEventSource.instances = [];

describe('api_client_events.jsx: the stream ticket', () => {
  let origEventSource;
  let ticketAnswers;
  let tickets;
  let mods;

  const flush = async () => { for (let i = 0; i < 20; i += 1) await Promise.resolve(); };

  beforeEach(async () => {
    FakeEventSource.instances = [];
    origEventSource = global.EventSource;
    global.EventSource = FakeEventSource;
    vi.useFakeTimers();
    tickets = [];
    ticketAnswers = [];
    global.fetch = vi.fn(async (url, opts = {}) => {
      if (url === '/api/events/ticket') {
        tickets.push(opts.headers || {});
        const next = ticketAnswers.shift() || { status: 200, body: { ticket: `t${tickets.length}` } };
        return { ok: next.status === 200, status: next.status, headers: { get: () => null }, json: async () => next.body };
      }
      return { ok: true, status: 200, headers: { get: () => null }, json: async () => ({}) };
    });
    vi.resetModules();
    const api = await import('../api_client.jsx');
    const events = await import('../api_client_events.jsx');
    const keys = await import('../reveal_key.jsx');
    const requests = await import('../api_client_requests.jsx');
    mods = { API: api.API, events, keys, requests };
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.resetModules();
    if (origEventSource === undefined) delete global.EventSource;
    else global.EventSource = origEventSource;
  });

  it('a public page with no key opens the public stream with no ticket', async () => {
    mods.events.setAudienceAndReconnect('viewer', '');
    const unsub = mods.API.subscribeToEvents(() => {});
    await flush();
    expect(tickets).toHaveLength(0);
    expect(FakeEventSource.instances.map((s) => s.url)).toEqual(['/api/events']);
    unsub();
  });

  it('a server that needs no ticket (a self-run tournament) lets a signed-in console open the public stream', async () => {
    ticketAnswers.push({ status: 400, body: { code: 'event_ticket_not_needed' } });
    mods.events.setAudienceAndReconnect('admin', 'pw');
    const unsub = mods.API.subscribeToEvents(() => {});
    await flush();
    expect(tickets).toHaveLength(1);
    expect(FakeEventSource.instances.map((s) => s.url)).toEqual(['/api/events']);
    unsub();
  });

  it('the organiser asks a ticket with the password, and a new one on every reconnect', async () => {
    mods.events.setAudienceAndReconnect('admin', 'pw');
    const unsub = mods.API.subscribeToEvents(() => {});
    await flush();
    expect(tickets).toEqual([{ 'X-Tournament-Password': 'pw' }]);
    expect(FakeEventSource.instances[0].url).toBe('/api/events?ticket=t1');

    FakeEventSource.instances[0].error();
    await vi.advanceTimersByTimeAsync(3000);
    await flush();
    expect(tickets).toHaveLength(2);
    expect(FakeEventSource.instances[1].url).toBe('/api/events?ticket=t2');
    unsub();
  });

  it('a ticket the password was refused for tells the page and is not asked for again', async () => {
    ticketAnswers.push({ status: 401, body: { error: 'Invalid password' } });
    const refused = vi.fn();
    mods.requests.onCredentialRefused(refused);
    mods.events.setAudienceAndReconnect('admin', 'stale');
    const status = vi.fn();
    const unsub = mods.API.subscribeToEvents(() => {}, status);
    await flush();
    expect(FakeEventSource.instances).toHaveLength(0);
    expect(status).toHaveBeenCalledWith('error');
    expect(refused).toHaveBeenCalledTimes(1);
    // A stale password cannot be mended by retrying: nothing asks again, and
    // the page's change of audience is what reopens the stream.
    await vi.advanceTimersByTimeAsync(30000);
    await flush();
    expect(tickets).toHaveLength(1);
    expect(mods.events.setAudienceAndReconnect('viewer', '')).toBe(true);
    await flush();
    expect(FakeEventSource.instances[0].url).toBe('/api/events');
    unsub();
    console.warn.mockClear();
  });

  it('a refused ticket opens nothing, never the public stream, and is asked again', async () => {
    ticketAnswers.push({ status: 503, body: { error: 'busy' } });
    mods.events.setAudienceAndReconnect('admin', 'stale');
    const status = vi.fn();
    const unsub = mods.API.subscribeToEvents(() => {}, status);
    await flush();
    expect(FakeEventSource.instances).toHaveLength(0);
    expect(status).toHaveBeenCalledWith('error');
    await vi.advanceTimersByTimeAsync(3000);
    await flush();
    expect(tickets).toHaveLength(2);
    expect(FakeEventSource.instances[0].url).toBe('/api/events?ticket=t2');
    unsub();
    // The refusal is logged once; nothing else is.
    expect(console.warn).toHaveBeenCalledTimes(1);
    console.warn.mockClear();
  });

  it('a phone with reveal keys asks its ticket with them, and a new credential reconnects', async () => {
    mods.keys.rememberRevealKey('juniors', KEY_A);
    mods.events.setAudienceAndReconnect('viewer', '');
    const unsub = mods.API.subscribeToEvents(() => {});
    await flush();
    expect(tickets).toEqual([{ 'X-Reveal-Key': `juniors:${KEY_A}` }]);

    mods.keys.rememberRevealKey('seniors', KEY_C);
    expect(mods.events.setAudienceAndReconnect('viewer', '')).toBe(true);
    await flush();
    expect(tickets[1]).toEqual({ 'X-Reveal-Key': `juniors:${KEY_A},seniors:${KEY_C}` });
    expect(FakeEventSource.instances[0].readyState).toBe(FakeEventSource.CLOSED);
    expect(FakeEventSource.instances[1].url).toBe('/api/events?ticket=t2');

    // Moving to a court board drops every credential: the public stream.
    mods.events.setAudienceAndReconnect('display', '');
    await flush();
    expect(tickets).toHaveLength(2);
    expect(FakeEventSource.instances[2].url).toBe('/api/events');
    unsub();
  });
});
