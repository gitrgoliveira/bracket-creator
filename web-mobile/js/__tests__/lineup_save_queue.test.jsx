// A lineup save names the positions it changed, and the offline queue keeps ONE
// entry per lineup (operator decision 2026-10-07, "Only changed positions"): a save
// of a lineup that already has one queued joins that entry, changing the union of
// the positions, the later value standing where both changed one. A save a previous
// build queued names no changed positions: it stays whole and has the new changes
// applied onto it. The server here follows the contract (helpers/lineup_server.js),
// so what survives a replay is judged on the lineup it ends with.
//
// Setup mirrors queue_ordered_writes.test.jsx (functional localStorage, EventSource
// stub, fake timers, the background clock poll never reaching the server).

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { makeLineupServer } from './helpers/lineup_server.js';

let API;
let _origFetch;
let _origEventSource;
let _origLocalStorage;
let _lsStore;

beforeEach(async () => {
  vi.useFakeTimers();
  vi.resetModules();
  _origEventSource = global.EventSource;
  global.EventSource = class FakeES { constructor() { this.close = () => {}; } };
  global.EventSource.OPEN = 1;
  _origFetch = global.fetch;
  _origLocalStorage = global.localStorage;
  _lsStore = {};
  global.localStorage = {
    getItem: (k) => (k in _lsStore ? _lsStore[k] : null),
    setItem: (k, v) => { _lsStore[k] = String(v); },
    removeItem: (k) => { delete _lsStore[k]; },
    clear: () => { _lsStore = {}; },
  };
  API = (await import('../api_client.jsx')).API;
});

afterEach(() => {
  vi.useRealTimers();
  vi.resetModules();
  global.fetch = _origFetch;
  if (_origEventSource === undefined) delete global.EventSource;
  else global.EventSource = _origEventSource;
  if (_origLocalStorage === undefined) delete global.localStorage;
  else global.localStorage = _origLocalStorage;
});

async function flushMicrotasks() {
  for (let i = 0; i < 12; i++) await Promise.resolve();
}
async function tick(ms = 0) {
  await vi.advanceTimersByTimeAsync(ms);
  await flushMicrotasks();
}

const storedEntries = () => JSON.parse(localStorage.getItem('bc_write_queue') || '[]');
const queuedPayloads = () => storedEntries().map(([, d]) => d.payload);

// The contract server behind the fetch the API sees; the background clock poll
// never reaches it. `reconnect` brings the connection back and tells the page.
function connect(options) {
  const server = makeLineupServer(options);
  global.fetch = vi.fn((url, opts) => (String(url).includes('/api/time')
    ? Promise.reject(new TypeError('clock poll: not part of this test'))
    : server.fetch(url, opts)));
  server.reconnect = async () => {
    server.offline = false;
    window.dispatchEvent(new Event('online'));
    await tick(50);
  };
  return server;
}

const STORED = {
  positions: { senpo: 'Sato', jiho: 'Tanaka', taisho: 'Ito' },
  memberIds: { senpo: 'mem-s', jiho: 'mem-t', taisho: 'mem-i' },
};

describe('a save that reaches the server names the positions it changed', () => {
  it('sends the changed positions and their ids, and the list that names them, and nothing else', async () => {
    const s = connect({ own: { m1: STORED } });
    await API.putMatchLineup('c1', 't1', 'm1', { senpo: 'Kato' }, 'pw', { senpo: 'mem-k' }, ['senpo']);
    expect(s.puts).toEqual([{
      url: '/api/competitions/c1/teams/t1/match-lineups/m1',
      body: {
        teamId: 't1', competitionId: 'c1', matchId: 'm1',
        positions: { senpo: 'Kato' }, memberIds: { senpo: 'mem-k' }, changed: ['senpo'],
      },
    }]);
  });

  it('does the same for a team\'s starting lineup, a cleared position being its empty name', async () => {
    const s = connect({ starting: STORED });
    await API.putTeamLineup('c1', 't1', 0, { jiho: '' }, 'pw', undefined, ['jiho']);
    expect(s.puts).toEqual([{
      url: '/api/competitions/c1/teams/t1/lineups/0',
      body: { teamId: 't1', competitionId: 'c1', round: 0, positions: { jiho: '' }, changed: ['jiho'] },
    }]);
    expect(s.starting.positions).toEqual({ senpo: 'Sato', taisho: 'Ito' });
  });

  it('answers the whole lineup as the server holds it, so a position another device changed shows', async () => {
    connect({ own: { m1: { positions: { ...STORED.positions, taisho: 'Mori' }, memberIds: { ...STORED.memberIds, taisho: 'mem-m' } } } });
    const answer = await API.putMatchLineup('c1', 't1', 'm1', { senpo: 'Kato' }, 'pw', { senpo: 'mem-k' }, ['senpo']);
    expect(answer.positions).toEqual({ senpo: 'Kato', jiho: 'Tanaka', taisho: 'Mori' });
    expect(answer.memberIds).toEqual({ senpo: 'mem-k', jiho: 'mem-t', taisho: 'mem-m' });
  });

  it('a save that names no changed positions is sent as the whole lineup it always was', async () => {
    const s = connect({ own: { m1: STORED } });
    await API.putMatchLineup('c1', 't1', 'm1', { senpo: 'Kato' }, 'pw');
    expect(s.puts[0].body).toEqual({ teamId: 't1', competitionId: 'c1', matchId: 'm1', positions: { senpo: 'Kato' } });
    expect('changed' in s.puts[0].body).toBe(false);
  });
});

describe('a save made offline replays naming only what it changed', () => {
  it('keeps what another device changed meanwhile: the replayed body carries Senpo alone, and Taisho survives', async () => {
    const s = connect({ own: { m1: STORED } });
    s.offline = true;
    // Device 1, with no connection, changes Senpo and saves.
    expect(await API.putMatchLineup('c1', 't1', 'm1', { senpo: 'Kato' }, 'pw', { senpo: 'mem-k' }, ['senpo'])).toEqual({ queued: true });
    await tick(0);
    // Device 2 changed Taisho meanwhile.
    s.own.m1 = { positions: { ...STORED.positions, taisho: 'Mori' }, memberIds: { ...STORED.memberIds, taisho: 'mem-m' } };
    await s.reconnect();
    expect(s.puts).toHaveLength(1);
    expect(s.puts[0].body.changed).toEqual(['senpo']);
    expect(s.own.m1.positions).toEqual({ senpo: 'Kato', jiho: 'Tanaka', taisho: 'Mori' });
    expect(s.own.m1.memberIds).toEqual({ senpo: 'mem-k', jiho: 'mem-t', taisho: 'mem-m' });
    expect(API.queuedLineupSave('c1', 't1', { matchId: 'm1' })).toBe(false);
    expect(localStorage.getItem('bc_write_queue')).toBeNull();
  });

  it('keeps the names the lineup in force carried when the match has no lineup of its own', async () => {
    const s = connect({ carried: { m1: STORED } });
    s.offline = true;
    await API.putMatchLineup('c1', 't1', 'm1', { senpo: 'Kato' }, 'pw', undefined, ['senpo']);
    await tick(0);
    await s.reconnect();
    expect(s.own.m1.positions).toEqual({ senpo: 'Kato', jiho: 'Tanaka', taisho: 'Ito' });
  });
});

describe('a lineup has one entry in the queue', () => {
  it('joins a later save into the queued one: both changes, and the later value of a position both changed', async () => {
    const s = connect({ own: { m1: STORED } });
    s.offline = true;
    await API.putMatchLineup('c1', 't1', 'm1', { senpo: 'Kato' }, 'pw', { senpo: 'mem-k' }, ['senpo']);
    vi.advanceTimersByTime(1000);
    const answer = await API.putMatchLineup('c1', 't1', 'm1', { senpo: 'Ota', jiho: 'Hara' }, 'pw', { senpo: 'mem-o', jiho: 'mem-h' }, ['senpo', 'jiho']);
    expect(answer).toEqual({ queued: true });
    expect(storedEntries()).toHaveLength(1);
    expect(queuedPayloads()[0]).toEqual({
      teamId: 't1', competitionId: 'c1', matchId: 'm1',
      positions: { senpo: 'Ota', jiho: 'Hara' }, memberIds: { senpo: 'mem-o', jiho: 'mem-h' }, changed: ['senpo', 'jiho'],
    });
    await tick(0);
    await s.reconnect();
    expect(s.puts).toHaveLength(1);
    expect(s.own.m1.positions).toEqual({ senpo: 'Ota', jiho: 'Hara', taisho: 'Ito' });
  });

  it('joins a save of another position: the first change is not lost to the second', async () => {
    const s = connect({ own: { m1: STORED } });
    s.offline = true;
    await API.putMatchLineup('c1', 't1', 'm1', { senpo: 'Kato' }, 'pw', { senpo: 'mem-k' }, ['senpo']);
    await API.putMatchLineup('c1', 't1', 'm1', { taisho: 'Mori' }, 'pw', { taisho: 'mem-m' }, ['taisho']);
    expect(queuedPayloads()[0].changed).toEqual(['senpo', 'taisho']);
    await tick(0);
    await s.reconnect();
    expect(s.own.m1.positions).toEqual({ senpo: 'Kato', jiho: 'Tanaka', taisho: 'Mori' });
  });

  it('makes a position the later save cleared its empty name with no id, and the replay clears it', async () => {
    const s = connect({ own: { m1: STORED } });
    s.offline = true;
    await API.putMatchLineup('c1', 't1', 'm1', { jiho: 'Hara' }, 'pw', { jiho: 'mem-h' }, ['jiho']);
    await API.putMatchLineup('c1', 't1', 'm1', { jiho: '' }, 'pw', undefined, ['jiho']);
    expect(queuedPayloads()[0].positions).toEqual({ jiho: '' });
    expect(queuedPayloads()[0].memberIds).toEqual({});
    await tick(0);
    await s.reconnect();
    expect(s.own.m1.positions).toEqual({ senpo: 'Sato', taisho: 'Ito' });
    expect(s.own.m1.memberIds).toEqual({ senpo: 'mem-s', taisho: 'mem-i' });
  });

  it('never sends a save straight to the server while one is queued, even with the connection back: one request carries both', async () => {
    const s = connect({ own: { m1: STORED } });
    s.offline = true;
    await API.putMatchLineup('c1', 't1', 'm1', { senpo: 'Kato' }, 'pw', { senpo: 'mem-k' }, ['senpo']);
    await tick(0);
    // The connection is back, but the page has not been told: the queued save is
    // still waiting for its backoff timer.
    s.offline = false;
    expect(API.queuedLineupSave('c1', 't1', { matchId: 'm1' })).toBe(true);
    await API.putMatchLineup('c1', 't1', 'm1', { jiho: 'Hara' }, 'pw', { jiho: 'mem-h' }, ['jiho']);
    await tick(50);
    expect(s.puts).toHaveLength(1);
    expect(s.puts[0].body.changed).toEqual(['senpo', 'jiho']);
    expect(s.own.m1.positions).toEqual({ senpo: 'Kato', jiho: 'Hara', taisho: 'Ito' });
  });

  it('keeps a match\'s lineup and a team\'s starting lineup as two entries', async () => {
    global.fetch = vi.fn(() => Promise.reject(new TypeError('network error')));
    await API.putMatchLineup('c1', 't1', 'm1', { senpo: 'Kato' }, 'pw', undefined, ['senpo']);
    await API.putTeamLineup('c1', 't1', 0, { senpo: 'Ota' }, 'pw', undefined, ['senpo']);
    expect(storedEntries()).toHaveLength(2);
    expect(API.queuedLineupSave('c1', 't1', { matchId: 'm1' })).toBe(true);
    expect(API.queuedLineupSave('c1', 't1', { round: 0 })).toBe(true);
  });
});

describe('a save a previous build queued', () => {
  // The previous build sent a whole lineup and kept one entry per lineup, keyed by
  // the lineup itself. It loads, and replays, as it is.
  const oldEntry = [
    'lineup:c1:t1:match:m1',
    {
      enqueuedAt: Date.now() - 60_000, password: 'pw',
      compID: 'c1', matchID: 'm1', kind: 'lineup', terminal: true, method: 'PUT',
      url: '/api/competitions/c1/teams/t1/match-lineups/m1',
      payload: { teamId: 't1', competitionId: 'c1', matchId: 'm1', ...STORED },
    },
  ];

  async function reload() {
    localStorage.setItem('bc_write_queue', JSON.stringify([oldEntry]));
    vi.resetModules();
    API = (await import('../api_client.jsx')).API;
  }

  it('stays whole when a save is made: the new changes are applied onto its lineup, and it names none', async () => {
    await reload();
    const s = connect({ own: { m1: STORED } });
    s.offline = true;
    expect(API.queuedLineupSave('c1', 't1', { matchId: 'm1' })).toBe(true);
    await API.putMatchLineup('c1', 't1', 'm1', { senpo: 'Kato', jiho: '' }, 'pw', { senpo: 'mem-k' }, ['senpo', 'jiho']);
    expect(storedEntries()).toHaveLength(1);
    expect(queuedPayloads()[0]).toEqual({
      teamId: 't1', competitionId: 'c1', matchId: 'm1',
      positions: { senpo: 'Kato', taisho: 'Ito' }, memberIds: { senpo: 'mem-k', taisho: 'mem-i' },
    });
    expect('changed' in queuedPayloads()[0]).toBe(false);
  });

  it('replays as a whole lineup, which replaces what the server holds', async () => {
    await reload();
    const s = connect({ own: { m1: { positions: { senpo: 'Z' }, memberIds: {} } } });
    s.offline = true;
    await API.putMatchLineup('c1', 't1', 'm1', { senpo: 'Kato' }, 'pw', { senpo: 'mem-k' }, ['senpo']);
    await tick(0);
    await s.reconnect();
    expect(s.puts).toHaveLength(1);
    expect('changed' in s.puts[0].body).toBe(false);
    expect(s.own.m1.positions).toEqual({ senpo: 'Kato', jiho: 'Tanaka', taisho: 'Ito' });
  });
});
