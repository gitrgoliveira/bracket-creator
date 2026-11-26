// bc-sseq FR4: a page fetches again once per burst, never for an event its
// answer already holds. read_seq.jsx owns the answer's stream position (the
// X-Event-Seq header, kept beside the value a fetcher returns); the refetch
// gate (createRefetchGate, patch.jsx) is the one owner of "does this event
// need a refetch". Its wait before a fetch is pinned to the server's push
// hold by the shared table internal/mobileapp/testdata/held_push_wait.json
// (the Go half is held_push_wait_test.go).

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { createRefetchGate, HELD_PUSH_WAIT_MS } from '../patch.jsx';
import { noteReadSeq, readSeqOf, EVENT_SEQ_HEADER } from '../read_seq.jsx';
import { API } from '../api_client.jsx';

const resWithSeq = (seq) => ({ headers: { get: (name) => (name === EVENT_SEQ_HEADER && seq != null ? String(seq) : null) } });

// answerAt: a fetched value whose answer named `seq` (none when null).
const answerAt = (seq, body = {}) => noteReadSeq({ ...body }, resWithSeq(seq));

// harness: a gate over a manual timer and a manual fetch, so a test decides
// when each scheduled fetch is sent and when each answer lands.
function harness({ maxDelayMs = 500, random = () => 0 } = {}) {
  const timers = [];
  const sent = [];
  const taken = [];
  const errors = [];
  const gate = createRefetchGate({
    schedule: (run, delay) => timers.push({ run, delay }),
    fetch: () => new Promise((resolveFetch, rejectFetch) => sent.push({ resolve: resolveFetch, reject: rejectFetch })),
    take: () => (value) => taken.push(value),
    maxDelayMs,
    random,
    onError: (err) => errors.push(err),
  });
  const fireTimers = () => { const due = timers.splice(0); due.forEach((t) => t.run()); return due.length; };
  const answer = async (value, i = sent.length - 1) => { sent[i].resolve(value); await Promise.resolve(); await Promise.resolve(); };
  const fail = async (err, i = sent.length - 1) => { sent[i].reject(err); await Promise.resolve(); await Promise.resolve(); };
  return { gate, timers, sent, taken, errors, fireTimers, answer, fail };
}

describe('read_seq: an answer keeps its stream position beside its value', () => {
  it('reads the header the answer carried', () => {
    const v = noteReadSeq({ a: 1 }, resWithSeq(42));
    expect(readSeqOf(v)).toBe(42);
    expect(v).toEqual({ a: 1 }); // the payload is unchanged
  });

  it('reads 0 when the header is missing, empty, not a whole number, or the response has no headers', () => {
    expect(readSeqOf(noteReadSeq({}, resWithSeq(null)))).toBe(0);
    expect(readSeqOf(noteReadSeq({}, { headers: { get: () => '' } }))).toBe(0);
    expect(readSeqOf(noteReadSeq({}, { headers: { get: () => 'abc' } }))).toBe(0);
    expect(readSeqOf(noteReadSeq({}, { headers: { get: () => '1.5' } }))).toBe(0);
    expect(readSeqOf(noteReadSeq({}, { headers: { get: () => '-3' } }))).toBe(0);
    expect(readSeqOf(noteReadSeq({}, { ok: true }))).toBe(0);
    expect(readSeqOf(noteReadSeq({}, undefined))).toBe(0);
  });

  it('keeps nothing on a value that cannot carry it, and reads 0 for it', () => {
    expect(noteReadSeq(null, resWithSeq(5))).toBe(null);
    expect(readSeqOf(null)).toBe(0);
    expect(readSeqOf(undefined)).toBe(0);
    expect(readSeqOf(7)).toBe(0);
  });

  it('a merged copy carries no seq: the host reads it off the fetched value', () => {
    const fetched = answerAt(9, { matches: [] });
    const merged = { ...fetched };
    expect(readSeqOf(fetched)).toBe(9);
    expect(readSeqOf(merged)).toBe(0);
  });

  it('arrays carry it too (the competitions list, the court feed)', () => {
    expect(readSeqOf(noteReadSeq([1, 2], resWithSeq(3)))).toBe(3);
  });
});

describe('the held-push wait is the server push hold plus a margin', () => {
  it('matches the shared table the Go test reads', () => {
    const table = JSON.parse(readFileSync(
      resolve(__dirname, '..', '..', '..', 'internal', 'mobileapp', 'testdata', 'held_push_wait.json'), 'utf8'));
    expect(HELD_PUSH_WAIT_MS).toBe(table.heldPushWaitMs);
    expect(table.heldPushWaitMs).toBe(table.matchCoalesceWindowMs + table.marginMs);
  });

  it('no fetch is scheduled sooner than the wait; the wait starts the host window, never adds to it', () => {
    const low = harness({ maxDelayMs: 500, random: () => 0 });
    low.gate.event(1);
    expect(low.timers[0].delay).toBe(HELD_PUSH_WAIT_MS);
    const high = harness({ maxDelayMs: 500, random: () => 0.999999 });
    high.gate.event(1);
    expect(high.timers[0].delay).toBeGreaterThan(499);
    expect(high.timers[0].delay).toBeLessThanOrEqual(500);
    const list = harness({ maxDelayMs: 2000, random: () => 0.5 });
    list.gate.event(1);
    expect(list.timers[0].delay).toBe(HELD_PUSH_WAIT_MS + 0.5 * (2000 - HELD_PUSH_WAIT_MS));
  });

  it('a host window narrower than the wait is widened to it', () => {
    const h = harness({ maxDelayMs: 100, random: () => 1 });
    h.gate.event(1);
    expect(h.timers[0].delay).toBe(HELD_PUSH_WAIT_MS);
  });
});

describe('createRefetchGate', () => {
  it('five events cause one fetch', async () => {
    const h = harness();
    for (let seq = 1; seq <= 5; seq += 1) expect(h.gate.event(seq)).toBe(true);
    expect(h.timers).toHaveLength(1);
    h.fireTimers();
    expect(h.sent).toHaveLength(1);
    const value = answerAt(5);
    await h.answer(value);
    expect(h.taken).toEqual([value]);
    expect(h.timers).toHaveLength(0);
    expect(h.gate.appliedSeq()).toBe(5);
  });

  it('a replayed event the answer covers fetches nothing', async () => {
    const h = harness();
    h.gate.event(3);
    h.fireTimers();
    await h.answer(answerAt(7));
    expect(h.gate.event(3)).toBe(false);
    expect(h.gate.event(7)).toBe(false);
    expect(h.timers).toHaveLength(0);
    expect(h.gate.event(8)).toBe(true);
    expect(h.timers).toHaveLength(1);
  });

  it('an event while one is out schedules one more only when the answer names a lower seq', async () => {
    const covered = harness();
    covered.gate.event(1);
    covered.fireTimers();
    covered.gate.event(2);
    covered.gate.event(3);
    expect(covered.timers).toHaveLength(0);
    await covered.answer(answerAt(3));
    expect(covered.timers).toHaveLength(0);

    const behind = harness();
    behind.gate.event(1);
    behind.fireTimers();
    behind.gate.event(2);
    behind.gate.event(4);
    await behind.answer(answerAt(3));
    expect(behind.timers).toHaveLength(1);
    behind.fireTimers();
    await behind.answer(answerAt(4));
    expect(behind.timers).toHaveLength(0);
    expect(behind.sent).toHaveLength(2);
  });

  it('a missing header covers nothing', async () => {
    const h = harness();
    h.gate.event(1);
    h.fireTimers();
    h.gate.event(2);
    await h.answer(answerAt(null));
    expect(h.gate.appliedSeq()).toBe(0);
    expect(h.timers).toHaveLength(1); // the event during the flight is not covered
    h.fireTimers();
    await h.answer(answerAt(null));
    expect(h.timers).toHaveLength(0);
    expect(h.gate.event(1)).toBe(true); // nothing counts as applied
  });

  it('an answer behind an event seen before its fetch was sent is fetched once more, and only once', async () => {
    // The fetch joined a build the server elected before event 5: it names 4.
    const h = harness();
    h.gate.event(5);
    h.fireTimers();
    await h.answer(answerAt(4));
    expect(h.gate.appliedSeq()).toBe(4);
    expect(h.timers).toHaveLength(1);
    h.fireTimers();
    await h.answer(answerAt(5));
    expect(h.gate.appliedSeq()).toBe(5);
    expect(h.timers).toHaveLength(0);
    expect(h.sent).toHaveLength(2);

    // A server whose seq restarted below the page's answers behind every time:
    // the repeat is the only one.
    const restarted = harness();
    restarted.gate.event(50);
    restarted.fireTimers();
    await restarted.answer(answerAt(3));
    expect(restarted.timers).toHaveLength(1);
    restarted.fireTimers();
    await restarted.answer(answerAt(3));
    expect(restarted.timers).toHaveLength(0);
    expect(restarted.sent).toHaveLength(2);
  });

  it('an answer covering the events seen before its fetch was sent asks for nothing, and a missing header is not behind', async () => {
    const covered = harness();
    covered.gate.event(5);
    covered.fireTimers();
    await covered.answer(answerAt(5));
    expect(covered.timers).toHaveLength(0);

    const bare = harness();
    bare.gate.event(5);
    bare.fireTimers();
    await bare.answer(answerAt(null));
    expect(bare.timers).toHaveLength(0);
  });

  it('now() asks for one whatever the seq, still coalescing', async () => {
    const h = harness();
    h.gate.event(1);
    h.fireTimers();
    await h.answer(answerAt(9));
    h.gate.now();
    h.gate.now();
    expect(h.timers).toHaveLength(1);
    h.fireTimers();
    h.gate.now(); // while out: one more once it lands
    h.gate.now();
    await h.answer(answerAt(9));
    expect(h.timers).toHaveLength(1);
    h.fireTimers();
    await h.answer(answerAt(9));
    expect(h.timers).toHaveLength(0);
    expect(h.sent).toHaveLength(3);
  });

  it('an answer naming a lower seq is a new stream: adopted, with one refetch for the events above it', async () => {
    const h = harness();
    h.gate.event(1);
    h.fireTimers();
    await h.answer(answerAt(10));
    h.gate.event(12); // seen above the coming answer
    h.fireTimers();
    await h.answer(answerAt(12));
    expect(h.gate.appliedSeq()).toBe(12);
    // A fetch outside the gate answers at seq 3: a restarted server.
    h.gate.applied(3);
    expect(h.gate.appliedSeq()).toBe(3);
    expect(h.timers).toHaveLength(1);
    h.fireTimers();
    await h.answer(answerAt(4));
    expect(h.gate.appliedSeq()).toBe(4);
    expect(h.timers).toHaveLength(0); // one refetch, no loop
    expect(h.gate.event(4)).toBe(false);
    expect(h.gate.event(5)).toBe(true);
  });

  it('a lower seq with no event above it is adopted without a refetch', () => {
    const h = harness();
    h.gate.applied(10);
    h.gate.applied(3);
    expect(h.gate.appliedSeq()).toBe(3);
    expect(h.timers).toHaveLength(0);
  });

  it('a fetch applied outside the gate covers its events, read before any merge', () => {
    const h = harness();
    const fetched = answerAt(6, { config: { id: 'c1' } });
    const merged = { ...fetched, extra: true }; // keep(prev, data) makes a new object
    h.gate.applied(readSeqOf(fetched));
    expect(readSeqOf(merged)).toBe(0);
    expect(h.gate.event(5)).toBe(false);
    expect(h.gate.event(6)).toBe(false);
    expect(h.timers).toHaveLength(0);
    h.gate.applied(readSeqOf(merged)); // a missing seq changes nothing
    expect(h.gate.appliedSeq()).toBe(6);
  });

  it('an applied answer that covers every event seen cancels the refetch already scheduled', () => {
    const h = harness();
    expect(h.gate.event(5)).toBe(true);
    expect(h.timers).toHaveLength(1);
    h.gate.applied(5); // the save's own read holds the event
    h.fireTimers();
    expect(h.sent).toHaveLength(0);
    // An event above the answer still gets its fetch.
    h.gate.event(6);
    h.gate.applied(5);
    h.fireTimers();
    expect(h.sent).toHaveLength(1);
  });

  it('an applied answer never cancels a refetch asked for by now() or by an event with no seq', () => {
    const h = harness();
    h.gate.now();
    h.gate.applied(5);
    h.fireTimers();
    expect(h.sent).toHaveLength(1);
    const g = harness();
    g.gate.event(undefined);
    g.gate.applied(5);
    g.fireTimers();
    expect(g.sent).toHaveLength(1);
  });

  it('a merged apply through take still covers the events its answer named', async () => {
    let held = null;
    const timers = [];
    const sends = [];
    const gate = createRefetchGate({
      schedule: (run) => timers.push(run),
      fetch: () => new Promise((r) => sends.push(r)),
      take: () => (data) => { held = { ...held, ...data }; },
      random: () => 0,
    });
    gate.event(1);
    gate.event(2);
    timers.splice(0).forEach((run) => run());
    sends[0](answerAt(2, { score: 'a' }));
    await Promise.resolve(); await Promise.resolve();
    expect(held).toEqual({ score: 'a' });
    expect(readSeqOf(held)).toBe(0);
    expect(gate.event(2)).toBe(false);
    expect(timers).toHaveLength(0);
  });

  it('forget() drops every seq, and an answer sent before it is taken but not adopted', async () => {
    const h = harness();
    h.gate.event(1);
    h.fireTimers();
    await h.answer(answerAt(8));
    expect(h.gate.event(8)).toBe(false);
    h.gate.event(9);
    h.fireTimers();
    h.gate.forget();
    h.gate.now(); // the host's resync refetch
    const stale = answerAt(9);
    await h.answer(stale);
    expect(h.taken).toContain(stale);
    expect(h.gate.appliedSeq()).toBe(0);
    expect(h.timers).toHaveLength(1);
    h.fireTimers();
    await h.answer(answerAt(2));
    expect(h.gate.appliedSeq()).toBe(2);
    expect(h.gate.event(2)).toBe(false);
    expect(h.gate.event(3)).toBe(true);
  });

  it('take is asked as the fetch is sent, not when it was scheduled', () => {
    const order = [];
    const timers = [];
    const gate = createRefetchGate({
      schedule: (run) => timers.push(run),
      fetch: () => { order.push('fetch'); return new Promise(() => {}); },
      take: () => { order.push('take'); return () => {}; },
    });
    gate.event(1);
    expect(order).toEqual([]);
    timers[0]();
    expect(order).toEqual(['take', 'fetch']);
  });

  it('a failed fetch is reported; an event seen while it was out still gets its refetch', async () => {
    const h = harness();
    h.gate.event(1);
    h.fireTimers();
    await h.fail(new Error('offline'));
    expect(h.errors).toHaveLength(1);
    expect(h.timers).toHaveLength(0); // no retry loop on its own
    expect(h.gate.event(1)).toBe(true); // nothing was applied
    h.fireTimers();
    h.gate.event(2);
    await h.fail(new Error('offline'));
    expect(h.timers).toHaveLength(1);
  });

  it('a take that throws is reported and its seq is not adopted', async () => {
    const timers = [];
    const sends = [];
    const errors = [];
    const gate = createRefetchGate({
      schedule: (run) => timers.push(run),
      fetch: () => new Promise((r) => sends.push(r)),
      take: () => () => { throw new Error('boom'); },
      onError: (err) => errors.push(err),
    });
    gate.event(1);
    timers.splice(0).forEach((run) => run());
    sends[0](answerAt(1));
    await Promise.resolve(); await Promise.resolve();
    expect(errors).toHaveLength(1);
    expect(gate.appliedSeq()).toBe(0);
    expect(gate.event(1)).toBe(true);
  });

  it('an event with no seq always asks, and one arriving while a fetch is out asks for one more', async () => {
    const h = harness();
    h.gate.event(1);
    h.fireTimers();
    await h.answer(answerAt(5));
    expect(h.gate.event(undefined)).toBe(true);
    expect(h.timers).toHaveLength(1);
    h.fireTimers();
    h.gate.event(null);
    await h.answer(answerAt(5));
    expect(h.timers).toHaveLength(1);
  });
});

describe('the reads note the answer seq on the value they finally return', () => {
  let originalFetch;
  beforeEach(() => { originalFetch = global.fetch; });
  afterEach(() => { global.fetch = originalFetch; });

  const answering = (body, seq) => vi.fn(() => Promise.resolve({
    ok: true, status: 200, headers: resWithSeq(seq).headers, json: () => Promise.resolve(body),
  }));
  const item = { config: { id: 'c1', name: 'Cup', players: [] }, poolMatches: [], bracket: null };

  it('fetchCompetitions, both arms', async () => {
    global.fetch = answering([item], 11);
    expect(readSeqOf(await API.fetchCompetitions())).toBe(11);
    global.fetch = answering([item], 12);
    expect(readSeqOf(await API.fetchCompetitions({ bounded: true }))).toBe(12);
  });

  it('fetchCompetitionDetails', async () => {
    global.fetch = answering(item, 13);
    const detail = await API.fetchCompetitionDetails('c1');
    expect(detail.config.id).toBe('c1');
    expect(readSeqOf(detail)).toBe(13);
  });

  it('fetchCourtMatches', async () => {
    global.fetch = answering({ competitions: [item] }, 14);
    const comps = await API.fetchCourtMatches('A');
    expect(comps).toHaveLength(1);
    expect(readSeqOf(comps)).toBe(14);
  });

  it('fetchTournament', async () => {
    global.fetch = answering({ name: 'T', competitions: [] }, 15);
    expect(readSeqOf(await API.fetchTournament())).toBe(15);
  });

  it('an answer with no header reads 0, and a null tournament stays null', async () => {
    global.fetch = vi.fn(() => Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(item) }));
    expect(readSeqOf(await API.fetchCompetitionDetails('c1'))).toBe(0);
    global.fetch = answering(null, 16);
    expect(await API.fetchTournament()).toBe(null);
  });
});
