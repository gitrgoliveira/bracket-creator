// A fake of the outbox's tap surface (api_client.jsx, bc-evts stage 3), for
// render tests of the individual score editor: recordTap queues an event,
// pendingTaps/subscribePendingTaps read and follow the queue,
// latestDerivedMatch/subscribeDerivedMatch the newest match an answer handed
// back, subscribeTapOutcome a tap the server did not apply. `ack` plays the
// server's answer to the taps queued so far; `serverFold` is a fake server
// running the JS replay port (score_events.jsx) over a match.
import { vi } from 'vitest';
import { foldEvent, IPPON_ADD, FOUL_ADD, DUPLICATE, MATCH_CLOSED, NOT_STARTED } from '../../score_events.jsx';

const markIdSet = (m) => new Set([...((m.markIds && m.markIds.a) || []), ...((m.markIds && m.markIds.b) || [])]);
const keyOf = (c, m) => `${c || ''}\u001f${m}`;

export function makeFakeOutbox({ session = 'dev1' } = {}) {
  const pending = new Map();
  const derived = new Map();
  const seqs = new Map();
  const subs = { pending: new Map(), derived: new Map(), outcome: new Map() };
  const listen = (kind, k, fn) => {
    if (!subs[kind].has(k)) subs[kind].set(k, new Set());
    subs[kind].get(k).add(fn);
    return () => { subs[kind].get(k)?.delete(fn); };
  };
  const emit = (kind, k, v) => { for (const fn of subs[kind].get(k) || []) fn(v); };
  const listFor = (k) => pending.get(k) || [];

  const api = {
    recordTap: vi.fn((c, m, ev, password, match) => {
      const k = keyOf(c, m);
      const seq = (seqs.get(k) || 0) + 1;
      seqs.set(k, seq);
      const id = `${session}.${seq}`;
      const stamp = Math.max(Number(match && match.modifiedAt) || 0, 0) + seq;
      pending.set(k, [...listFor(k), { ...ev, id, session, seq, stamp }]);
      emit('pending', k, listFor(k).map((e) => ({ ...e })));
      return { id, stamp, seq };
    }),
    pendingTaps: (c, m) => listFor(keyOf(c, m)).map((e) => ({ ...e })),
    subscribePendingTaps: (c, m, fn) => listen('pending', keyOf(c, m), fn),
    latestDerivedMatch: (c, m) => derived.get(keyOf(c, m)) || null,
    subscribeDerivedMatch: (c, m, fn) => listen('derived', keyOf(c, m), fn),
    subscribeTapOutcome: (c, m, fn) => listen('outcome', keyOf(c, m), fn),
  };

  // ack: the server answered the batch holding every tap queued so far for
  // the match: `match` is the derived copy, `outcomes` maps a tap id to a
  // not-applied outcome ({outcome, reason?, message?}); the rest are applied.
  // Like the real outbox, the derived copy is noted before the taps leave.
  const ack = (c, m, match, outcomes = {}) => {
    const k = keyOf(c, m);
    const sent = listFor(k);
    if (match) {
      const prev = derived.get(k);
      if (!prev || (Number(prev.scoreVersion) || 0) <= (Number(match.scoreVersion) || 0)) {
        derived.set(k, match);
        emit('derived', k, match);
      }
    }
    pending.set(k, []);
    emit('pending', k, []);
    for (const e of sent) {
      const o = outcomes[e.id];
      if (o) emit('outcome', k, { id: e.id, ...o });
    }
    return sent;
  };
  return { api, ack, taps: (c, m) => listFor(keyOf(c, m)).map((e) => ({ ...e })) };
}

// serverFold: a fake server holding `match`, applying `events` in order with
// the JS replay port; scoreVersion counts every event logged. Returns the new
// match and each event's outcome, as the score-events answer does.
export function serverFold(match, events) {
  let m = { ...match };
  const outcomes = {};
  for (const e of events) {
    let outcome;
    if (m.status === 'completed') outcome = MATCH_CLOSED;
    else if (m.status === 'scheduled') outcome = NOT_STARTED;
    else if ((e.kind === IPPON_ADD || e.kind === FOUL_ADD) && markIdSet(m).has(e.id)) outcome = DUPLICATE;
    else {
      const r = foldEvent(m, e);
      m = r.match;
      outcome = r.outcome;
    }
    outcomes[e.id] = outcome;
    if (outcome !== NOT_STARTED) {
      m = { ...m, scoreVersion: (Number(m.scoreVersion) || 0) + 1, modifiedAt: Math.max(Number(m.modifiedAt) || 0, Number(e.stamp) || 0) };
      // The highest seq logged per session (engine.ScoreLogEventSeqs).
      if (e.session && e.seq > 0) {
        m.eventSeqs = { ...m.eventSeqs, [e.session]: Math.max(Number((m.eventSeqs || {})[e.session]) || 0, e.seq) };
      }
    }
  }
  return { match: m, outcomes };
}
