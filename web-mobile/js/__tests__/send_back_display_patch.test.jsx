// bc-stpf: a send back the outbox holds reaches a display window on the same
// machine at once (operator, 2026-10-27: the scoreboard display runs in another
// browser window). API.revertMatchToQueue publishes the bout on the court's
// channel as back in the queue in the tick it is queued, as a start's
// _broadcastPatch does, with its score kept, and running again when the send
// back does not land. A display holding the old bout and the picked one then
// shows the picked one.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { API } from '../api_client.jsx';
import { bridge, applyPatchToTree } from '../court_bridge.jsx';
import { findRunningOnCourt } from '../display_helpers.jsx';

function reply(status, body) {
  return vi.fn(() => Promise.resolve({
    ok: status >= 200 && status < 300,
    status,
    headers: { get: () => null },
    json: () => Promise.resolve(body),
  }));
}

const flush = async () => { for (let i = 0; i < 5; i++) await Promise.resolve(); };

const runningBout = (over = {}) => ({
  id: 'm1', compId: 'c1', court: 'A', status: 'running', scoreVersion: 7,
  sideA: { id: 'p1', name: 'Yamada' }, sideB: { id: 'p2', name: 'Tanaka' },
  ipponsA: ['M'], ipponsB: [], hansokuA: 1, hansokuB: 0,
  ...over,
});

// The patches published for match `id`, as [court, compId, result].
const patchesFor = (spy, id) => spy.mock.calls
  .filter(([type, , , payload]) => type === 'patch' && payload && payload.result && payload.result.id === id)
  .map(([, court, compId, payload]) => [court, compId, payload.result]);

describe('a queued send back is published on the court\'s channel (bc-stpf)', () => {
  let originalFetch;
  let publish;
  beforeEach(() => {
    originalFetch = global.fetch;
    publish = vi.spyOn(bridge, 'publish');
  });
  afterEach(() => {
    global.fetch = originalFetch;
    publish.mockRestore();
    vi.useRealTimers();
    API.clearQueue();
  });

  it('as the bout back in the queue with its score kept, in the tick it is queued', async () => {
    global.fetch = reply(200, {});
    const res = API.revertMatchToQueue('c1', 'm1', 'pw', { match: runningBout() });
    // Before anything is awaited: the same tick.
    expect(patchesFor(publish, 'm1')).toEqual([
      ['A', 'c1', { id: 'm1', status: 'scheduled', sideAId: 'p1', sideBId: 'p2' }],
    ]);
    const [, , result] = patchesFor(publish, 'm1')[0];
    // No scoreline field: the display keeps the score it shows.
    for (const k of ['ipponsA', 'ipponsB', 'hansokuA', 'hansokuB', 'encho', 'subResults', 'scoreVersion']) {
      expect(k in result).toBe(false);
    }
    await expect((await res).settled).resolves.toBe(true);
    await flush();
    // Landed: nothing more is published.
    expect(patchesFor(publish, 'm1')).toHaveLength(1);
  });

  it('states the flag counts an engi bout holds', () => {
    global.fetch = reply(200, {});
    API.revertMatchToQueue('c1', 'm1', 'pw', { match: runningBout({ flagsA: 2, flagsB: undefined }) });
    const [, , result] = patchesFor(publish, 'm1')[0];
    expect(result.flagsA).toBe(2);
    expect(result.flagsB).toBe(0);
  });

  it('a court-less match publishes nothing', () => {
    global.fetch = reply(200, {});
    API.revertMatchToQueue('c1', 'm1', 'pw', { match: runningBout({ court: '' }) });
    expect(patchesFor(publish, 'm1')).toEqual([]);
  });

  it('a send back given no match publishes nothing', () => {
    global.fetch = reply(200, {});
    API.revertMatchToQueue('c1', 'm1', 'pw');
    expect(patchesFor(publish, 'm1')).toEqual([]);
  });

  it('a 409-finished refusal does not publish running', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    global.fetch = reply(409, { error: 'match_completed', message: 'match already completed' });
    const res = await API.revertMatchToQueue('c1', 'm1', 'pw', { match: runningBout() });
    await expect(res.settled).rejects.toThrow(/already completed/);
    await flush();
    expect(patchesFor(publish, 'm1').map(([, , r]) => r.status)).toEqual(['scheduled']);
    warn.mockRestore();
  });

  it('a plain refusal publishes the bout running again', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    global.fetch = reply(400, { error: 'invalid request' });
    const res = await API.revertMatchToQueue('c1', 'm1', 'pw', { match: runningBout() });
    await expect(res.settled).rejects.toThrow(/invalid request/);
    await flush();
    expect(patchesFor(publish, 'm1').map(([, , r]) => r.status)).toEqual(['scheduled', 'running']);
    const [court, compId, result] = patchesFor(publish, 'm1')[1];
    expect([court, compId]).toEqual(['A', 'c1']);
    // No scoreVersion: never read as a stale running board by the display.
    expect('scoreVersion' in result).toBe(false);
    // A send back moves no court, so neither patch names one.
    expect(patchesFor(publish, 'm1').some(([, , r]) => 'court' in r)).toBe(false);
    // Each names the status the bout moves from, for the display to judge.
    expect(publish.mock.calls.filter(([type]) => type === 'patch').map(([, , , payload]) => payload.ifStatus))
      .toEqual(['running', 'scheduled']);
    warn.mockRestore();
  });

  it('a confirmed running court move publishes the bout as scheduled on its old court, naming the new one', async () => {
    global.fetch = reply(200, {});
    const res = API.moveMatchCourt('c1', 'm1', 'B', 'pw', {
      stopsRunning: true,
      was: { court: 'A' },
      match: runningBout({ court: 'A' }),
    });
    // The patch names court B, so the old court's display takes the bout off
    // its queue at once rather than keeping it as queued on A.
    expect(patchesFor(publish, 'm1')).toEqual([
      ['A', 'c1', { id: 'm1', status: 'scheduled', sideAId: 'p1', sideBId: 'p2', court: 'B' }],
    ]);
    await expect((await res).settled).resolves.toEqual({});
    await flush();
    expect(patchesFor(publish, 'm1')).toHaveLength(1);
  });

  it('a court move without stopsRunning publishes nothing on the bridge', () => {
    global.fetch = reply(200, {});
    API.moveMatchCourt('c1', 'm1', 'B', 'pw', {
      was: { court: 'A' },
      match: runningBout({ court: 'A' }),
    });
    expect(patchesFor(publish, 'm1')).toEqual([]);
  });

  it('a refused running court move publishes the bout running again on its old court', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    global.fetch = reply(400, { error: 'invalid court' });
    const res = await API.moveMatchCourt('c1', 'm1', 'B', 'pw', {
      stopsRunning: true,
      was: { court: 'A' },
      match: runningBout({ court: 'A' }),
    });
    await expect(res.settled).rejects.toThrow(/invalid court/);
    await flush();
    expect(patchesFor(publish, 'm1').map(([, , r]) => r.status)).toEqual(['scheduled', 'running']);
    const [court, compId, result] = patchesFor(publish, 'm1')[1];
    expect([court, compId]).toEqual(['A', 'c1']);
    // The put-back names the court the bout came from, undoing the move's patch.
    expect(result.court).toBe('A');
    expect('scoreVersion' in result).toBe(false);
    warn.mockRestore();
  });

  it('a discarded send back publishes the bout running again', async () => {
    vi.useFakeTimers();
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    global.fetch = reply(500, { error: 'internal error' });
    const res = await API.revertMatchToQueue('c1', 'm1', 'pw', { match: runningBout() });
    // The server keeps refusing it until the write is discardable.
    let held = [];
    for (let i = 0; i < 40 && !held.some((h) => h.matchID === 'm1' && h.keepsFailing); i++) {
      await vi.advanceTimersByTimeAsync(60_000);
      held = API.heldWrites();
    }
    const entry = held.find((h) => h.matchID === 'm1' && h.keepsFailing);
    expect(entry, 'the send back is held and keeps failing').toBeTruthy();
    expect(API.discardHeldWrite(entry.key)).toBeTruthy();
    await expect(res.settled).resolves.toEqual({ discarded: true });
    await flush();
    expect(patchesFor(publish, 'm1').map(([, , r]) => r.status)).toEqual(['scheduled', 'running']);
    warn.mockRestore();
  });

  it('another tab removing the send back does not publish running', async () => {
    // This tab's own request is out when the other tab takes the entry out of
    // storage, so its own answer settles it.
    let answer;
    global.fetch = () => new Promise((resolve) => { answer = resolve; });
    const res = await API.revertMatchToQueue('c1', 'm1', 'pw', { match: runningBout() });
    expect(patchesFor(publish, 'm1').map(([, , r]) => r.status)).toEqual(['scheduled']);
    const oldRaw = localStorage.getItem('bc_write_queue_v2');
    const newRaw = '[]';
    localStorage.setItem('bc_write_queue_v2', newRaw);
    window.dispatchEvent(new StorageEvent('storage', {
      key: 'bc_write_queue_v2',
      oldValue: oldRaw,
      newValue: newRaw,
    }));
    await flush();
    answer({ ok: true, status: 200, headers: { get: () => null }, json: () => Promise.resolve({}) });
    await expect(res.settled).resolves.not.toHaveProperty('discarded');
    await flush();
    expect(patchesFor(publish, 'm1').map(([, , r]) => r.status)).toEqual(['scheduled']);
  });
});

describe('a display holding the old bout and the picked one shows the picked one (bc-stpf)', () => {
  const tree = () => ({
    competitions: [{
      id: 'c1',
      poolMatches: [
        { id: 'm1', court: 'A', status: 'running', scoreVersion: 7, sideA: { id: 'p1', name: 'Yamada' }, sideB: { id: 'p2', name: 'Tanaka' }, ipponsA: ['M'] },
        { id: 'm2', court: 'A', status: 'scheduled', scoreVersion: 0, sideA: { id: 'p3', name: 'Suzuki' }, sideB: { id: 'p4', name: 'Sato' } },
      ],
    }],
  });
  const patch = (result) => ({ type: 'patch', compId: 'c1', court: 'A', payload: { result } });

  it('takes the send back over its running copy and keeps its score', () => {
    const next = applyPatchToTree(tree(), patch({ id: 'm1', status: 'scheduled', sideAId: 'p1', sideBId: 'p2' }));
    const m1 = next.competitions[0].poolMatches.find((m) => m.id === 'm1');
    expect(m1.status).toBe('scheduled');
    expect(m1.ipponsA).toEqual(['M']);
  });

  it('after the pick\'s start and the send back, in either order', () => {
    const start = patch({ id: 'm2', status: 'running', sideAId: 'p3', sideBId: 'p4' });
    const back = patch({ id: 'm1', status: 'scheduled', sideAId: 'p1', sideBId: 'p2' });
    for (const order of [[start, back], [back, start]]) {
      const shown = order.reduce((t, msg) => applyPatchToTree(t, msg), tree());
      expect(findRunningOnCourt(shown.competitions, 'A').match.id).toBe('m2');
    }
  });

  it('a send back put back shows the old bout running again', () => {
    const back = applyPatchToTree(tree(), patch({ id: 'm1', status: 'scheduled' }));
    const again = applyPatchToTree(back, patch({ id: 'm1', status: 'running' }));
    expect(findRunningOnCourt(again.competitions, 'A').match.id).toBe('m1');
  });

  // Another device finished the bout and the display's feed said so first.
  const finishedHere = () => {
    const t = tree();
    t.competitions[0].poolMatches[0] = { ...t.competitions[0].poolMatches[0], status: 'completed', winner: 'p1', scoreVersion: 9 };
    return t;
  };
  const withFrom = (result, ifStatus) => ({ ...patch(result), payload: { result, ifStatus } });

  it('a send back of a bout the display holds finished leaves it finished', () => {
    const held = finishedHere();
    const next = applyPatchToTree(held, withFrom({ id: 'm1', status: 'scheduled', sideAId: 'p1', sideBId: 'p2' }, 'running'));
    expect(next).toBe(held);
  });

  it('a put back of a bout the display holds finished leaves it finished', () => {
    const held = finishedHere();
    const next = applyPatchToTree(held, withFrom({ id: 'm1', status: 'running' }, 'scheduled'));
    expect(next).toBe(held);
    expect(findRunningOnCourt(next.competitions, 'A')).toBeFalsy();
  });

  it('a send back and its put back from the statuses they name still apply', () => {
    const back = applyPatchToTree(tree(), withFrom({ id: 'm1', status: 'scheduled' }, 'running'));
    expect(back.competitions[0].poolMatches[0].status).toBe('scheduled');
    const again = applyPatchToTree(back, withFrom({ id: 'm1', status: 'running' }, 'scheduled'));
    expect(findRunningOnCourt(again.competitions, 'A').match.id).toBe('m1');
  });

  it('end to end: a send back published by the outbox leaves a finished copy finished', async () => {
    const originalFetch = global.fetch;
    const publish = vi.spyOn(bridge, 'publish');
    global.fetch = reply(200, {});
    try {
      await API.revertMatchToQueue('c1', 'm1', 'pw', { match: runningBout() });
      const [type, court, compId, payload] = publish.mock.calls.find(([t]) => t === 'patch');
      const held = finishedHere();
      expect(applyPatchToTree(held, { type, court, compId, payload })).toBe(held);
    } finally {
      publish.mockRestore();
      global.fetch = originalFetch;
      API.clearQueue();
    }
  });
});
