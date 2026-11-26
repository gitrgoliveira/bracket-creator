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
import { createRefetchGate, HELD_PUSH_WAIT_MS, FETCH_DEADLINE_MS } from '../patch.jsx';
import { noteReadSeq, readSeqOf, EVENT_SEQ_HEADER } from '../read_seq.jsx';
import { API } from '../api_client.jsx';

const resWithSeq = (seq) => ({ headers: { get: (name) => (name === EVENT_SEQ_HEADER && seq != null ? String(seq) : null) } });

// answerAt: a fetched value whose answer named `seq` (none when null).
const answerAt = (seq, body = {}) => noteReadSeq({ ...body }, resWithSeq(seq));

// harness: a gate over a manual timer and a manual fetch, so a test decides
// when each scheduled fetch is sent and when each answer lands. The deadline
// each sent fetch schedules is kept apart (deadlines, fireDeadlines), so
// `timers` holds only the refetches. `result` is what apply returns.
function harness({ maxDelayMs = 500, random = () => 0, result = () => undefined } = {}) {
  const timers = [];
  const deadlines = [];
  const sent = [];
  const taken = [];
  const errors = [];
  const gate = createRefetchGate({
    schedule: (run, delay) => (delay === FETCH_DEADLINE_MS ? deadlines : timers).push({ run, delay }),
    fetch: () => new Promise((resolveFetch, rejectFetch) => sent.push({ resolve: resolveFetch, reject: rejectFetch })),
    take: () => (value) => { taken.push(value); return result(value); },
    maxDelayMs,
    random,
    onError: (err) => errors.push(err),
  });
  const fireTimers = () => { const due = timers.splice(0); due.forEach((t) => t.run()); return due.length; };
  const fireDeadlines = () => { const due = deadlines.splice(0); due.forEach((t) => t.run()); return due.length; };
  const answer = async (value, i = sent.length - 1) => { sent[i].resolve(value); await Promise.resolve(); await Promise.resolve(); };
  const fail = async (err, i = sent.length - 1) => { sent[i].reject(err); await Promise.resolve(); await Promise.resolve(); };
  return { gate, timers, deadlines, sent, taken, errors, fireTimers, fireDeadlines, answer, fail };
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

  it('a seeded gate counts the seed as applied until its first answer, which replaces it', async () => {
    // The seed is the seq of a read made before the stream may have
    // restarted: the first answer after it names the new stream's seq, which
    // is lower, and a gate that only raised applied would keep the seed.
    const h = harness();
    h.gate.seed(40);
    expect(h.gate.event(30)).toBe(false); // the page holds it
    h.gate.applied(3);
    expect(h.gate.appliedSeq()).toBe(3);
    expect(h.gate.event(4)).toBe(true);
    h.gate.applied(2); // only the first answer replaces the seed
    expect(h.gate.appliedSeq()).toBe(3);

    // An applied seq that was never a seed is only ever raised.
    const plain = harness();
    plain.gate.applied(40);
    plain.gate.applied(3);
    expect(plain.gate.appliedSeq()).toBe(40);

    // forget() drops the seed with everything else.
    const forgotten = harness();
    forgotten.gate.seed(40);
    forgotten.gate.forget();
    forgotten.gate.applied(3);
    forgotten.gate.applied(2);
    expect(forgotten.gate.appliedSeq()).toBe(3);
  });

  it('the first answer after a seed is shown even when its seq is below the seed', async () => {
    // A seed older than a server restart is above the new stream's seqs: the
    // answer to the first fetch must still be applied, not dropped as late.
    const h = harness();
    h.gate.seed(40);
    expect(h.gate.event(41)).toBe(true);
    h.fireTimers();
    const value = answerAt(3, { fresh: true });
    await h.answer(value);
    expect(h.taken).toEqual([value]);
    expect(h.gate.appliedSeq()).toBe(3);
    // Past the seed, a lower answer is late again.
    expect(h.gate.event(5)).toBe(true);
    h.fireTimers();
    await h.answer(answerAt(2));
    expect(h.taken).toHaveLength(1);
    expect(h.gate.appliedSeq()).toBe(3);
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

  it('a late answer naming a lower seq than the applied one changes nothing', async () => {
    const h = harness();
    h.gate.event(1);
    h.fireTimers();
    await h.answer(answerAt(10));
    h.gate.event(12); // seen above the coming answer
    h.fireTimers();
    await h.answer(answerAt(12));
    expect(h.gate.appliedSeq()).toBe(12);
    // A read made outside the gate answers late, at seq 3 (a build that began earlier).
    h.gate.applied(3);
    expect(h.gate.appliedSeq()).toBe(12);
    expect(h.timers).toHaveLength(0); // no refetch for it
    expect(h.gate.event(12)).toBe(false);
    expect(h.gate.event(13)).toBe(true);
  });

  it('a gate fetch answering lower than the applied seq, with nothing newer seen, asks for nothing', async () => {
    const h = harness();
    h.gate.applied(10);
    h.gate.now();
    h.fireTimers();
    await h.answer(answerAt(4));
    expect(h.gate.appliedSeq()).toBe(10);
    expect(h.timers).toHaveLength(0);
    expect(h.sent).toHaveLength(1);
  });

  it('restart() (the stream reopened) drops every seq and asks for one refetch whatever its seq says', async () => {
    const h = harness();
    h.gate.event(1);
    h.fireTimers();
    await h.answer(answerAt(50));
    expect(h.gate.event(40)).toBe(false);
    h.gate.restart();
    expect(h.gate.appliedSeq()).toBe(0);
    expect(h.timers).toHaveLength(1);
    h.fireTimers();
    // A restarted server's head seq starts again: its answer names 0 (no
    // event yet) and then 3.
    await h.answer(answerAt(0));
    expect(h.gate.appliedSeq()).toBe(0);
    expect(h.gate.event(1)).toBe(true); // not covered by the old 50
    h.fireTimers();
    await h.answer(answerAt(3));
    expect(h.gate.appliedSeq()).toBe(3);
    expect(h.gate.event(3)).toBe(false);
    expect(h.gate.event(4)).toBe(true);
  });

  it('restart() while a fetch is out lets it go and sends a new one, and the old answer is neither applied nor adopted', async () => {
    const h = harness();
    h.gate.event(1);
    h.fireTimers();
    await h.answer(answerAt(50));
    h.gate.event(51);
    h.fireTimers();
    h.gate.restart();
    await h.answer(answerAt(51));
    expect(h.gate.appliedSeq()).toBe(0);
    expect(h.taken).toHaveLength(1);
    expect(h.timers).toHaveLength(1);
    h.fireTimers();
    expect(h.sent).toHaveLength(3);
  });

  it('now() or restart() while a fetch hangs sends a new one without waiting for the deadline', async () => {
    for (const resync of ['now', 'restart']) {
      const h = harness();
      h.gate.event(1);
      h.fireTimers();
      expect(h.sent).toHaveLength(1);
      h.gate[resync]();
      // The hung fetch is let go: the new one is scheduled now, not after it.
      expect(h.timers).toHaveLength(1);
      h.fireTimers();
      expect(h.sent).toHaveLength(2);
      await h.answer(answerAt(7));
      expect(h.gate.appliedSeq()).toBe(7);
      // The hung one answering later, or its deadline running, changes nothing.
      await h.answer(answerAt(3), 0);
      h.fireDeadlines();
      expect(h.gate.appliedSeq()).toBe(7);
      expect(h.errors).toHaveLength(0);
      expect(h.sent).toHaveLength(2);
      expect(h.timers).toHaveLength(0);
    }
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
      // The fetch deadline is not a refetch: only refetch timers are kept.
      schedule: (run, delay) => { if (delay !== FETCH_DEADLINE_MS) timers.push(run); },
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

  it('forget() drops every seq, and the answer of a fetch it left out is not adopted', async () => {
    const h = harness();
    h.gate.event(1);
    h.fireTimers();
    await h.answer(answerAt(8));
    expect(h.gate.event(8)).toBe(false);
    h.gate.event(9);
    h.fireTimers();
    h.gate.forget();
    h.gate.now(); // the host's resync refetch lets the fetch out go
    const stale = answerAt(9);
    await h.answer(stale);
    expect(h.taken).not.toContain(stale);
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
    expect(h.timers).toHaveLength(1); // the burst rode on it alone: asked for again
    h.fireTimers();
    h.gate.event(2);
    await h.fail(new Error('offline'));
    expect(h.timers).toHaveLength(1); // the event seen while it was out
  });

  it('a failed fetch is asked for again once, then waits for the next event (no polling)', async () => {
    const h = harness();
    h.gate.event(1);
    h.fireTimers();
    await h.fail(new Error('offline'));
    expect(h.timers).toHaveLength(1);
    h.fireTimers();
    expect(h.sent).toHaveLength(2);
    await h.fail(new Error('offline'));
    expect(h.errors).toHaveLength(2);
    expect(h.timers).toHaveLength(0); // the retry failed too: no loop
    // The next event asks again, and its failure is retried once more.
    expect(h.gate.event(2)).toBe(true);
    h.fireTimers();
    await h.fail(new Error('offline'));
    expect(h.timers).toHaveLength(1);
    // A retry that lands ends it.
    h.fireTimers();
    await h.answer(answerAt(2));
    expect(h.timers).toHaveLength(0);
    expect(h.gate.appliedSeq()).toBe(2);
  });

  it('the retry of a failed fetch is dropped when an out-of-gate read applied meanwhile', async () => {
    const h = harness();
    h.gate.event(1);
    h.fireTimers();
    await h.fail(new Error('offline'));
    h.gate.applied(1); // a save's own refresh holds the event
    h.fireTimers();
    expect(h.sent).toHaveLength(1);
  });

  it('a failed restart() fetch is asked for again once, though no event is seen', async () => {
    const h = harness();
    h.gate.restart(); // nothing counts as applied, and nothing was seen
    h.fireTimers();
    await h.fail(new Error('offline'));
    expect(h.timers).toHaveLength(1);
    h.fireTimers();
    expect(h.sent).toHaveLength(2); // the retry is as forced as the fetch that failed
    await h.fail(new Error('offline'));
    expect(h.timers).toHaveLength(0); // the second failure waits: no polling
  });

  it('a failed now() fetch is asked for again once, though every event seen is applied', async () => {
    const h = harness();
    h.gate.applied(5);
    h.gate.now();
    h.fireTimers();
    await h.fail(new Error('offline'));
    expect(h.timers).toHaveLength(1);
    h.fireTimers();
    expect(h.sent).toHaveLength(2);
    await h.answer(answerAt(5));
    expect(h.timers).toHaveLength(0);
    // The retry that landed was the last forced one: a later event the answer
    // holds asks for nothing.
    expect(h.gate.event(5)).toBe(false);
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

  it('dispose: an answer landing after it is not applied and asks for nothing more', async () => {
    const h = harness();
    h.gate.event(1);
    h.fireTimers();
    h.gate.event(2); // seen while the fetch is out: would ask for one more
    h.gate.dispose();
    await h.answer(answerAt(1));
    expect(h.taken).toHaveLength(0);
    expect(h.timers).toHaveLength(0);
    expect(h.sent).toHaveLength(1);
    expect(h.gate.appliedSeq()).toBe(0);
  });

  it('dispose: a failed fetch landing after it is not reported or asked for again', async () => {
    const h = harness();
    h.gate.event(1);
    h.fireTimers();
    h.gate.dispose();
    await h.fail(new Error('late'));
    expect(h.errors).toHaveLength(0);
    expect(h.timers).toHaveLength(0);
  });

  it('dispose: a refetch scheduled and not sent is never sent, and no event, now() or restart() asks again', () => {
    const h = harness();
    h.gate.event(1);
    expect(h.timers).toHaveLength(1);
    h.gate.dispose();
    h.fireTimers();
    expect(h.sent).toHaveLength(0);
    h.gate.event(2);
    h.gate.now();
    h.gate.restart();
    expect(h.timers).toHaveLength(0);
    expect(h.sent).toHaveLength(0);
  });

  it('now(seq) counts its seq as seen: an answer behind it is fetched once more', async () => {
    // tournament_updated at seq 8: the answer joined a build elected before it.
    const h = harness();
    h.gate.now(8);
    h.fireTimers();
    await h.answer(answerAt(5));
    expect(h.gate.appliedSeq()).toBe(5);
    expect(h.timers).toHaveLength(1);
    h.fireTimers();
    await h.answer(answerAt(8));
    expect(h.timers).toHaveLength(0);
    expect(h.sent).toHaveLength(2);
    expect(h.gate.event(8)).toBe(false);
  });

  it('an answer apply dropped (it returned false) is not adopted, and an event seen while it was out still gets its refetch', async () => {
    let drop = true;
    const h = harness({ result: () => (drop ? false : true) });
    h.gate.event(1);
    h.fireTimers();
    h.gate.event(2);
    await h.answer(answerAt(5));
    expect(h.taken).toHaveLength(1);
    expect(h.gate.appliedSeq()).toBe(0);
    expect(h.errors).toHaveLength(0);
    expect(h.timers).toHaveLength(1);
    drop = false; // true (or nothing) means applied
    h.fireTimers();
    await h.answer(answerAt(5));
    expect(h.gate.appliedSeq()).toBe(5);
    expect(h.timers).toHaveLength(0);
    expect(h.gate.event(2)).toBe(false);
  });

  it('an answer older than a read already applied is not applied; one at or above it, or with no seq, is', async () => {
    const h = harness();
    h.gate.applied(10); // an out-of-gate read already shown
    h.gate.now();
    h.fireTimers();
    await h.answer(answerAt(7));
    expect(h.taken).toHaveLength(0);
    expect(h.errors).toHaveLength(0);
    expect(h.gate.appliedSeq()).toBe(10);
    expect(h.timers).toHaveLength(0);
    const same = answerAt(10);
    h.gate.now();
    h.fireTimers();
    await h.answer(same);
    const bare = answerAt(null);
    h.gate.now();
    h.fireTimers();
    await h.answer(bare);
    const newer = answerAt(11);
    h.gate.now();
    h.fireTimers();
    await h.answer(newer);
    expect(h.taken).toEqual([same, bare, newer]);
    expect(h.gate.appliedSeq()).toBe(11);
  });

  it('a fetch with no answer by the deadline fails, is retried, and its late answer is ignored', async () => {
    const h = harness();
    h.gate.event(1);
    h.fireTimers();
    expect(h.deadlines).toHaveLength(1);
    expect(h.deadlines[0].delay).toBe(FETCH_DEADLINE_MS);
    h.gate.event(2); // seen while it hangs: recorded only
    expect(h.timers).toHaveLength(0);
    h.fireDeadlines();
    expect(h.errors).toHaveLength(1);
    expect(h.errors[0].message).toMatch(/did not answer in time/);
    expect(h.timers).toHaveLength(1); // no longer frozen
    h.fireTimers();
    expect(h.sent).toHaveLength(2);
    await h.answer(answerAt(1), 0); // the abandoned fetch answers late
    expect(h.taken).toHaveLength(0);
    expect(h.gate.appliedSeq()).toBe(0);
    expect(h.timers).toHaveLength(0);
    const fresh = answerAt(2);
    await h.answer(fresh, 1);
    expect(h.taken).toEqual([fresh]);
    expect(h.gate.appliedSeq()).toBe(2);
    h.fireDeadlines(); // the answered fetch's deadline does nothing
    expect(h.errors).toHaveLength(1);
    expect(h.timers).toHaveLength(0);
  });

  it('after a deadline whose retry also hangs, the next event sends a fetch', async () => {
    const h = harness();
    h.gate.event(1);
    h.fireTimers();
    h.fireDeadlines();
    h.fireTimers(); // the retry
    h.fireDeadlines(); // it hangs too: no polling
    expect(h.errors).toHaveLength(2);
    expect(h.timers).toHaveLength(0);
    expect(h.gate.event(2)).toBe(true);
    h.fireTimers();
    expect(h.sent).toHaveLength(3);
    await h.fail(new Error('late'), 0); // a late failure of an abandoned fetch is ignored
    expect(h.errors).toHaveLength(2);
    await h.answer(answerAt(2));
    expect(h.gate.appliedSeq()).toBe(2);
  });

  it('dispose: a deadline running after it reports nothing and asks for nothing', () => {
    const h = harness();
    h.gate.event(1);
    h.fireTimers();
    h.gate.dispose();
    h.fireDeadlines();
    expect(h.errors).toHaveLength(0);
    expect(h.timers).toHaveLength(0);
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
