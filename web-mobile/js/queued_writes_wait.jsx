// queued_writes_wait.jsx: the ONE owner of "wait for this competition's queued
// writes of these kinds before doing this" (bc-obox C1 and T1). A draw is not
// made while check-ins this device made are still on their way, and a drawn
// competition is not moved while a time change is still on its way: the
// screen says what it waits for and offers to do it now anyway, or Cancel.
// Waiting is the default; it is never a trap.
//
// The count is API.queuedWritesOf (api_client.jsx), the one reader of this
// device's queued writes, re-read on the outbox's change signal
// (subscribeUnsentWrites) and the sync status. An entry the server keeps
// refusing stays in the count and is named on the waiting screen, with the
// held-writes list's Discard as the way out; a refusal the server answers
// settles, leaves the queue and is told by the outbox, so the wait ends.
//
// A leaf: window.API, the subscriptions, window.Modal and React are read at
// call time, never at module load, so a test may install them after the
// import.

import { heldWriteWhere } from './held_writes_panel.jsx';
import { heldWriteLine, heldWriteDiscardConfirm, HELD_WRITE_DISCARD_ONE_LABEL } from './write_result.jsx';

// The outbox kind of a match's time change (API.updateMatchTime): what a move
// of a drawn competition waits for (bc-obox T1). The draw waits for
// API.CHECK_IN_WRITE_KINDS.
export const TIME_WRITE_KINDS = Object.freeze(['time']);

function kindList(kinds) {
  if (kinds instanceof Set) return [...kinds];
  return Array.isArray(kinds) ? kinds : [];
}

function kindsOfKey(key) {
  return key ? key.split(',') : [];
}

function apiOf() {
  return typeof window !== 'undefined' ? window.API : null;
}

// queuedWritesCount: how many writes of `kinds` this device still holds for
// `compID`. No kinds counts none.
export function queuedWritesCount(compID, kinds) {
  const list = kindList(kinds);
  const api = apiOf();
  if (list.length === 0 || !api || typeof api.queuedWritesOf !== 'function') return 0;
  return api.queuedWritesOf(compID, list) || 0;
}

// queuedWritesBlockers: the held writes of `kinds` for `compID` the server
// keeps refusing, as API.heldWrites() lists them: each holds the wait until
// it is discarded.
export function queuedWritesBlockers(compID, kinds) {
  const set = new Set(kindList(kinds));
  const api = apiOf();
  if (set.size === 0 || !api || typeof api.heldWrites !== 'function') return [];
  const list = api.heldWrites();
  return (Array.isArray(list) ? list : []).filter((held) => held && held.keepsFailing
    && held.compID === compID && set.has(held.kind));
}

const NOUNS = {
  checkin: ['check-in', 'check-ins'],
  time: ['time change', 'time changes'],
  settings: ['settings change', 'settings changes'],
};

// queuedWritesNounFor: "1 check-in", "3 check-ins", "2 time changes", "1 settings change".
export function queuedWritesNounFor(what, n) {
  const [one, many] = NOUNS[what] || ['change', 'changes'];
  return `${n} ${n === 1 ? one : many}`;
}

// waitingSentence: what the waiting screen says, e.g. "Waiting for 3
// check-ins to be sent. The draw is made once they arrive."
export function waitingSentence(what, n, outcome) {
  const arrive = n === 1 ? 'once it arrives' : 'once they arrive';
  return `Waiting for ${queuedWritesNounFor(what, n)} to be sent. ${outcome} ${arrive}.`;
}

// useWaitForQueuedWrites(compID, kinds): { waiting, count, blockers, run,
// runNow, cancel }. run(fn) calls fn in the same tick when nothing is queued;
// otherwise it holds fn (a later run replaces it) and calls it once the count
// reaches 0. A caller whose fn must read the state of the moment it fires
// passes a thunk over a useLatestRef. runNow(...args) calls the held fn at
// once with args (the operator chose to go on anyway); cancel() drops it. A
// change of competition or kinds, or an unmount, drops a held run.
export function useWaitForQueuedWrites(compID, kinds) {
  const { useState, useEffect, useRef, useCallback } = React;
  const list = kindList(kinds);
  const kindsKey = list.join(',');
  const readNow = () => ({ count: queuedWritesCount(compID, list), blockers: queuedWritesBlockers(compID, list) });
  const [seen, setSeen] = useState(readNow);
  const [waiting, setWaiting] = useState(false);
  const heldRef = useRef(null);
  const mountedRef = useRef(true);
  const keyRef = useRef({ compID, kindsKey });

  useEffect(() => {
    mountedRef.current = true;
    return () => { mountedRef.current = false; heldRef.current = null; };
  }, []);

  useEffect(() => {
    keyRef.current = { compID, kindsKey };
    const kindsNow = kindsOfKey(kindsKey);
    const onChange = () => {
      if (!mountedRef.current) return;
      const count = queuedWritesCount(compID, kindsNow);
      setSeen({ count, blockers: queuedWritesBlockers(compID, kindsNow) });
      const fn = heldRef.current;
      if (!fn || count > 0) return;
      // Fired outside the outbox's listener loop, which swallows what a
      // listener throws: a refusal of the held action must reach its caller.
      queueMicrotask(() => {
        if (!mountedRef.current || heldRef.current !== fn) return;
        if (queuedWritesCount(compID, kindsNow) > 0) return;
        heldRef.current = null;
        setWaiting(false);
        fn();
      });
    };
    const offs = (typeof window !== 'undefined' ? [window.subscribeUnsentWrites, window.subscribeSyncStatus] : [])
      .filter((subscribe) => typeof subscribe === 'function')
      .map((subscribe) => subscribe(onChange));
    // A change of competition or kinds drops the held run: the cleanup runs
    // before the next key's effect, so a run held for the old key never fires
    // under the new one.
    return () => {
      offs.forEach((off) => { if (typeof off === 'function') off(); });
      heldRef.current = null;
      if (mountedRef.current) setWaiting(false);
    };
  }, [compID, kindsKey]);

  const run = useCallback((fn) => {
    if (typeof fn !== 'function') return;
    const { compID: id, kindsKey: key } = keyRef.current;
    const kindsNow = kindsOfKey(key);
    const count = queuedWritesCount(id, kindsNow);
    if (count === 0) { fn(); return; }
    heldRef.current = fn;
    setSeen({ count, blockers: queuedWritesBlockers(id, kindsNow) });
    setWaiting(true);
  }, []);
  const runNow = useCallback((...args) => {
    const fn = heldRef.current;
    heldRef.current = null;
    setWaiting(false);
    if (fn) fn(...args);
  }, []);
  const cancel = useCallback(() => {
    heldRef.current = null;
    setWaiting(false);
  }, []);

  return { waiting, count: seen.count, blockers: seen.blockers, run, runNow, cancel };
}

// QueuedWritesWait: the waiting screen, shown while `wait.waiting`. `what` is
// 'checkin' or 'time'; `outcome` finishes the sentence ("The draw is made");
// `nowLabel` names the way on ("Generate now anyway"), whose `onNow` asks its
// own confirm and then calls wait.runNow. Cancel, Escape and the backdrop drop
// the held action. A write the server keeps refusing is named with Discard.
export function QueuedWritesWait({ wait, comp, what, outcome, nowLabel, onNow, title }) {
  const { useState } = React;
  const [busyKey, setBusyKey] = useState(null);
  const Modal = typeof window !== 'undefined' ? window.Modal : null;
  if (!wait || !wait.waiting || !Modal) return null;
  const discard = async (held) => {
    setBusyKey(held.key);
    try {
      const ok = typeof window.confirmDialog === 'function'
        ? await window.confirmDialog(heldWriteDiscardConfirm(held))
        : false;
      const api = apiOf();
      if (ok && api && typeof api.discardHeldWrite === 'function') api.discardHeldWrite(held.key);
    } finally {
      setBusyKey(null);
    }
  };
  const blockers = Array.isArray(wait.blockers) ? wait.blockers : [];
  return (
    <Modal
      title={title || 'Waiting for changes to be sent'}
      onClose={wait.cancel}
      footer={(
        <>
          <button type="button" className="btn" data-testid="queued-wait-cancel" onClick={wait.cancel}>Cancel</button>
          <button type="button" className="btn btn--primary" data-testid="queued-wait-now" onClick={onNow}>{nowLabel}</button>
        </>
      )}
    >
      <p role="status" aria-live="polite" data-testid="queued-wait-text">{waitingSentence(what, wait.count, outcome)}</p>
      {blockers.length > 0 && (
        <>
          <p>The server keeps refusing {blockers.length === 1 ? 'this one' : 'these'}. Discard {blockers.length === 1 ? 'it' : 'them'} to go on.</p>
          <ul className="held-writes" data-testid="queued-wait-blockers">
            {blockers.map((held) => (
              <li key={held.key} className="held-writes__item">
                <div className="held-writes__text">
                  <div className="held-writes__where">{heldWriteWhere(held, comp ? [comp] : [])}</div>
                  <div className="held-writes__state held-writes__state--error">{heldWriteLine(held)}</div>
                </div>
                <button
                  type="button"
                  className="btn btn--sm btn--ghost"
                  data-testid="queued-wait-discard"
                  disabled={busyKey !== null}
                  onClick={() => discard(held)}
                >{HELD_WRITE_DISCARD_ONE_LABEL}</button>
              </li>
            ))}
          </ul>
        </>
      )}
    </Modal>
  );
}
