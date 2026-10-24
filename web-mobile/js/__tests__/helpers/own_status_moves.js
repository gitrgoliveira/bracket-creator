// A fake of the outbox's ONE owner of the status moves this device made
// (API.ownStatusMove / subscribeOwnStatusMoves / forgetOwnStatusMove,
// api_client.jsx, stage 6 review 8, M4), for render tests that stub the write
// doors: a stubbed door's pending answer is held as a queued move, and its
// `settled` answers it (landed, with the answer's stamp) or takes it away (a
// refusal or a discard), as the real outbox does. The court console reads the
// moves from here alone, so a test that mounts it again reads what is held.
export function createOwnStatusMoves() {
  const moves = new Map();
  const listeners = new Set();
  const key = (c, m) => `${c}:${m}`;
  const tell = () => { for (const fn of Array.from(listeners)) fn(); };

  const api = {
    ownStatusMove: (c, m) => moves.get(key(c, m)) || null,
    subscribeOwnStatusMoves: (fn) => { listeners.add(fn); return () => listeners.delete(fn); },
    forgetOwnStatusMove: (c, m) => { if (moves.delete(key(c, m))) tell(); },
  };

  // hold: the outbox holds `move` for match (c, m), answered by `answer`'s
  // settled. A null move (a start, a reopen) ends the one held.
  const hold = (c, m, move, answer) => {
    const k = key(c, m);
    if (!move) {
      if (moves.delete(k)) tell();
      return answer;
    }
    const held = { landed: false, stamp: 0, ...move };
    moves.set(k, held);
    tell();
    const settled = answer && answer.settled;
    if (settled && typeof settled.then === 'function') {
      settled.then((body) => {
        if (moves.get(k) !== held) return;
        if (body && !body.discarded && body.applied !== false) {
          moves.set(k, { ...held, landed: true, stamp: Number(body.modifiedAt) || 0 });
        } else {
          moves.delete(k);
        }
        tell();
      }, () => {
        if (moves.get(k) !== held) return;
        moves.delete(k);
        tell();
      });
    }
    return answer;
  };

  // The move a score write's patch makes: a finish completes the match, a
  // start (startOnly) ends a move held; anything else moves nothing.
  const scoreMove = (patch) => {
    if (!patch) return undefined;
    if (patch.status === 'completed') return { status: 'completed', kind: 'score', payload: patch };
    if (patch.status === 'running' && patch.startOnly) return null;
    return undefined;
  };

  return {
    api,
    hold,
    // Between tests: nothing mounted is told (an update outside act()).
    clear() { moves.clear(); },
    // scoreDoor wraps a stubbed onEditScore: a pending answer it gives holds
    // the move its patch makes, as the real door's outbox entry does.
    scoreDoor(fn) {
      return async (c, m, patch, ...rest) => {
        const res = await fn(c, m, patch, ...rest);
        const mv = scoreMove(patch);
        if (res && res.pending && mv !== undefined) hold(c, m, mv, res);
        return res;
      };
    },
  };
}
