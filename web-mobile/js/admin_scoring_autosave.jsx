// Autosave / sync-status infra for the score editor modals.
// Used by both ScoreEditorModal (individual) and TeamScoreEditorModal (team).
// Extracted from admin_scoring_modal.jsx (mp-zac3).

import { toBackendMatchResult, matchWire } from './api_serializers.jsx';
import { changedGroups, statedGroupsOf, groupMatches, heldGroupsNote } from './match_groups.jsx';
import { writePartlyHeld, writeHeldGroups, writeDidNotLand } from './write_result.jsx';

const { useState: useStateA, useEffect: useEffectA, useRef: useRefA } = React;

export const AUTOSAVE_DEBOUNCE_MS = 300;

// ---------------------------------------------------------------------------
// useChangedGroups (bc-mrgc): every score write an editor builds names the
// groups it changes (match_groups.jsx), so the server applies only those and
// keeps every other group as stored. Called once per editor with the match it
// renders from; returns claim(patch), which every patch the editor hands to
// its host goes through, autosave included, and which returns the patch with
// `changed` set.
//
// A group counts as changed when the write's value differs from either of two
// baselines, both read through the serializer the write goes through
// (api_serializers.jsx matchWire) so only a real difference counts:
//
//   - the server's value the editor's state last AGREED with for that group:
//     the match as the editor mounted on it, moved on whenever a write is
//     built while the editor's value and the match it renders from are the
//     same (its own write come back, or a value it adopted). Not simply the
//     latest copy of the match: an editor that does not follow a group (the
//     individual editor seeds its points and overtime once) would otherwise
//     read another device's later change to that group as its own and put the
//     old value back.
//   - the last write this editor built for the match: a tap taken back before
//     the first write's echo arrives reads the same as the copy that has not
//     caught up yet, and without this the second write would name nothing and
//     the server would keep the first.
// ---------------------------------------------------------------------------
export function useChangedGroups(match) {
  const matchRef = useRefA(match);
  matchRef.current = match;
  const stateRef = useRefA(null);
  const keyOf = (m) => `${(m && m.compId) || ""}\u0000${(m && m.id) || ""}`;
  // The editor moving to another match starts over from that match.
  if (!stateRef.current || stateRef.current.key !== keyOf(match)) {
    stateRef.current = { key: keyOf(match), seed: matchWire(match), agreed: {}, last: null };
  }
  return (patch) => {
    if (!patch) return patch;
    const m = matchRef.current;
    const st = stateRef.current;
    const next = toBackendMatchResult(patch, m);
    const current = matchWire(m);
    for (const g of statedGroupsOf(next)) {
      if (groupMatches(g, next, current, next)) st.agreed[g] = current;
    }
    const changed = changedGroups(next, (g) => st.agreed[g] || st.seed, st.last);
    st.last = next;
    return { ...patch, changed };
  };
}

// ---------------------------------------------------------------------------
// useKeptInHistoryNote (bc-mrgc): the quiet note a score editor shows when the
// server applied a write in part and kept the rest in the match's history,
// because a newer change to the same thing was recorded first. The editor's
// flow carries on as for any landed write; the note only names what was kept
// (heldGroupsNote). A write that lands with nothing held clears it; one that
// did not land (queued, or refused, which has its own banner) leaves it.
// Returns { note, noteFromWrite }, noteFromWrite stable across renders so the
// autosave can hold it.
// ---------------------------------------------------------------------------
export function useKeptInHistoryNote() {
  const [note, setNote] = useStateA(null);
  const mountedRef = useRefA(true);
  useEffectA(() => () => { mountedRef.current = false; }, []);
  // The note last set, so a write that changes nothing about it (the common
  // case: every landed autosave) renders nothing.
  const shownRef = useRefA(null);
  const noteFromWriteRef = useRefA(null);
  if (!noteFromWriteRef.current) {
    noteFromWriteRef.current = (res) => {
      if (!mountedRef.current || !res) return;
      let next = shownRef.current;
      if (writePartlyHeld(res)) next = heldGroupsNote(writeHeldGroups(res));
      else if (!writeDidNotLand(res)) next = null;
      if (next === shownRef.current) return;
      shownRef.current = next;
      setNote(next);
    };
  }
  return { note, noteFromWrite: noteFromWriteRef.current };
}

export function KeptInHistoryNote({ note }) {
  if (!note) return null;
  return <div className="sb-hint" role="status" data-testid="kept-in-history-note">{note}</div>;
}

// The monotonic clock an edit is read on, or null where there is none (the
// write is then stamped when it is sent).
const perfNow = () => (typeof performance !== "undefined" && typeof performance.now === "function" ? performance.now() : null);

// ---------------------------------------------------------------------------
// C2: SyncStatusPill
// ---------------------------------------------------------------------------
// Small indicator rendered in the scoring-panel header while a match is
// running. Subscribes to the write-queue sync status from api_client.jsx
// (via window.subscribeSyncStatus) and reflects:
//   synced        : last write landed; no queue pending
//   syncing       : an edit waiting in the autosave window
//                   (useDebouncedRunningWrite registers it, bc-sync), or a
//                   write in flight / in queue
//   offline       : network down; queue retrying with backoff
//   auth-required : write refused with 401; parked, still queued, needs the
//                   operator to sign in again to save (403 is a different
//                   case: server misconfiguration, not a bad credential, so
//                   it retries with backoff instead of parking; see
//                   api_client.jsx's 401 vs 403 handling)
//   server-error  : server reachable but this write keeps failing (10+
//                   consecutive 5xx/429); still queued and retrying
//                   automatically, no operator action needed
//
// COPY RULE: NEVER use the word "live" in user-facing strings.
// Colors use design tokens only (var(--...)): no hardcoded hex.
// ---------------------------------------------------------------------------

// Module-level const: hoisted so the object is not rebuilt on every render.
// Every SyncStatusValue MUST have an entry here: SyncStatusPill falls back
// to SYNC_PILL_CONFIG.synced for anything missing, so an unmapped status
// would silently render the word "Synced" over unsaved work.
const SYNC_PILL_CONFIG = {
  synced: { label: 'Synced',   cls: 'sync-pill--synced',  dot: '●' },
  syncing: { label: 'Syncing…', cls: 'sync-pill--syncing', dot: '◌' },
  offline: { label: 'Offline',  cls: 'sync-pill--offline', dot: '●' },
  'auth-required': { label: 'Sign in to save', cls: 'sync-pill--auth',  dot: '●' },
  'server-error': { label: 'Not saving',        cls: 'sync-pill--error', dot: '●' },
};

export function SyncStatusPill({ isRunning }) {
  // The component always mounts and subscribes to sync status (the subscription
  // is a single Set entry and replays the current value on subscribe). It only
  // renders a VISIBLE pill while the match is running: autosave fires only on
  // running matches, so the pill carries no meaning otherwise. The render guard
  // is the `if (!isRunning) return null` below.
  const [status, setStatus] = useStateA('synced');
  useEffectA(() => {
    // window.subscribeSyncStatus is set by api_client.jsx when loaded.
    const subscribe = typeof window !== 'undefined' && window.subscribeSyncStatus;
    if (!subscribe) return;
    const unsub = subscribe((s) => setStatus(s));
    return () => unsub();
  }, []);

  if (!isRunning) return null; // render guard: no visible pill unless running

  const c = SYNC_PILL_CONFIG[status] || SYNC_PILL_CONFIG.synced;

  // bc-qttl: this pill is the element that TELLS the operator "Sign in to
  // save", so it should also be the thing they can act on. When the queue is
  // parked on a 401 and the re-auth entry point is installed (App mounts it
  // on window.requestReauth), render a real button instead of an inert span.
  // Every other status keeps the plain, non-interactive span.
  if (status === 'auth-required' && typeof window.requestReauth === 'function') {
    return (
      <button
        type="button"
        className={`sync-status-pill ${c.cls}`}
        data-testid="sync-status-pill"
        aria-label={`Score sync: ${c.label}`}
        onClick={() => window.requestReauth()}
      >
        <span className="sync-pill__dot" aria-hidden="true">{c.dot}</span>
        <span className="sync-pill__label">{c.label}</span>
      </button>
    );
  }

  return (
    <span className={`sync-status-pill ${c.cls}`} data-testid="sync-status-pill" aria-label={`Score sync: ${c.label}`}>
      <span className="sync-pill__dot" aria-hidden="true">{c.dot}</span>
      <span className="sync-pill__label">{c.label}</span>
    </span>
  );
}

// useDebouncedRunningWrite: the running-match autosave. Every edit is held
// for AUTOSAVE_DEBOUNCE_MS and then written as one status:"running" PUT.
//
// bc-sync: that held edit exists nowhere but this hook's timer, so it is
// REPORTED and KEPT. Reported: markDirty registers a pending-edit token with
// api_client (API.notePendingEdit), so the sync pill reads "Syncing..." from
// the first tap instead of "Synced" over unsent work, and the token is
// released only once the write has been handed to onSubmit (by then
// recordScore counts it as in flight). Kept when the editor goes away: the
// unmount writes it. Kept when the page goes away: on pagehide, or the tab
// being hidden, a pending edit is written at once (a hidden tab first waits
// for a hold, see the effect below) with `durable: true` on the patch, which
// makes recordScore put it straight into the persisted outbox.
// Every write carries the time of the edit it saves (editedPerf), so however
// late it goes out, it is never newer than a result recorded after the tap.
//
// onWriteResult, optional, is handed what each running write comes back with
// (the editors' kept-in-history note, useKeptInHistoryNote). Read through a
// ref, so a new function each render is fine.
export function useDebouncedRunningWrite({ isRunningRef, buildPatchRef, onSubmitRef, onWriteResult }) {
  const onWriteResultRef = useRefA(onWriteResult);
  onWriteResultRef.current = onWriteResult;
  const timerRef = useRefA(null);
  // The monotonic reading of the last edit, sent on the patch as `editedPerf`
  // (never on the wire) so recordScore stamps the write with the time of that
  // edit, not of its sending (api_client.jsx _editAge).
  const editPerfRef = useRefA(null);
  // One token per editor instance, so two open editors never release each
  // other's pending edit.
  const pendingTokenRef = useRefA({});
  // The caller's hold (see hold below), or null. A debounce that fires while
  // one is on sets deferredRef instead of writing, and the edit stays owed.
  const holdRef = useRefA(null);
  const deferredRef = useRefA(false);
  // Existing test stubs of window.API predate notePendingEdit, so ask first.
  const notePending = (on) => {
    const api = window.API;
    if (api && typeof api.notePendingEdit === "function") api.notePendingEdit(pendingTokenRef.current, on);
  };

  // clearTimer: stop the debounce timer and nothing else. The pending edit
  // stays registered; whoever clears the timer decides when it is released.
  const clearTimer = () => {
    if (timerRef.current === null) return false;
    clearTimeout(timerRef.current);
    timerRef.current = null;
    return true;
  };

  // takeOwed: clear the timer and a deferred edit, reporting whether an edit
  // was owed. Whoever takes it writes it or drops it.
  const takeOwed = () => {
    const owed = clearTimer() || deferredRef.current;
    deferredRef.current = false;
    return owed;
  };

  // cancelDebounce: call this before any explicit submit (Start / Finish /
  // Hantei / Decision) so the queued timer can't fire afterward, and before
  // closing on the operator's Discard, so the unmount does not write what they
  // threw away. Nothing of this hook's is left pending then, so the pending
  // edit is released too. Returns whether an edit was owed, so a caller can
  // tell a save is still due. A deferred edit is dropped, not written: the
  // submit this precedes carries the same current state.
  const cancelDebounce = () => {
    const owed = takeOwed();
    if (owed) notePending(false);
    return owed;
  };

  // The running write itself, shared by the debounce timer, release, the
  // unmount and the page-hide flush so they can never apply different gates.
  // `durable` asks recordScore to queue the write rather than fetch it
  // (bc-sync); `transform` applies a hold's outcome to the patch (see hold).
  const fireRunningWrite = (durable, transform) => {
    try {
      // gate 3: re-check running at FIRE time. If the match was completed
      // during the debounce window (this operator's Finish cancels the timer,
      // but an SSE update or another operator can complete it out from under
      // us), isRunningRef has flipped false on re-render: sending a
      // status:"running" autosave now would regress the completed result.
      if (!isRunningRef.current) return;
      // Fire-and-forget: errors swallowed; operator's explicit Finish is
      // the authoritative write. The pending edit is released only AFTER the
      // write is dispatched (the finally below): recordScore counts it as in
      // flight before its first await, so the status stays "syncing" through
      // the hand-over instead of flickering to "synced" and back.
      try {
        let patch = { ...buildPatchRef.current("running"), editedPerf: editPerfRef.current };
        if (transform) patch = transform(patch);
        const p = onSubmitRef.current(durable ? { ...patch, durable: true } : patch);
        if (p && typeof p.then === "function") {
          p.then((res) => { const cb = onWriteResultRef.current; if (cb) cb(res); }).catch(() => {});
        }
      } catch (_) { /* swallow */ }
    } finally {
      notePending(false);
    }
  };

  // hold: for a caller whose own request (the team editor's representative-bout
  // add or remove) changes the sheet an autosave is built from. A debounce
  // that fires while held defers its write, and the mounted caller calls
  // release() from an effect that runs after the render adopting the result,
  // since refs update during render, not inside the request's continuation.
  //
  // hold() returns settle(transform), which the caller calls once the request
  // has settled, mounted or not: with a function applying the outcome to a
  // patch when it landed, with nothing when it failed or was refused. An edit
  // owed when the editor unmounts during the request waits for it and is
  // written with that outcome applied, since nothing renders to adopt it.
  const hold = () => {
    const h = { settled: false, transform: null, onSettle: null };
    holdRef.current = h;
    return (transform) => {
      if (h.settled) return;
      h.settled = true;
      h.transform = transform || null;
      if (h.onSettle) h.onSettle();
    };
  };
  const release = () => {
    holdRef.current = null;
    if (deferredRef.current) {
      deferredRef.current = false;
      fireRunningWrite();
    }
  };

  // Unmount writes an edit still inside the window, whatever unmounted the
  // editor (operator ruling 2026-09-27): Close, Prev/Next or their keys,
  // another match picked, a correction opened, a court switch, or the host
  // moving on by itself because another device finished the match. That last
  // one is why the write carries the time of the tap (editedPerf): the tap is
  // older than that finish, so the server keeps the finish. Discarding is the
  // one way out that saves nothing: the editor cancels first (cancelDebounce).
  // Under a hold the edit goes out with the request's outcome applied: at
  // once if it has settled, else when it settles.
  //
  // bc-sync: the page going away (a reload, a closed tab, the iPad locking or
  // switching app) writes an edit still in the window NOW, durably: recordScore
  // puts a `durable` write into the persisted outbox, because a fetch started
  // here would die with the document. On pagehide a hold does not delay it,
  // since nothing can wait for a page that is going away: it goes with the
  // outcome applied if the request has settled, else as it stands. The tab
  // being hidden does not mean it is going, so there an unsettled hold is
  // waited for and the edit then goes durably with the outcome, where a write
  // at once would put back a removed representative bout or wipe an added one.
  // Accepted gap: a hidden page the system discards before the request
  // answers, with no pagehide, loses that tap. The listeners stay on while an
  // edit waits for its request.
  //
  // One effect for both, so the unmount decides whether the page listeners
  // stay. Everything the closures read is a ref, so the mount-time closure is
  // safe.
  useEffectA(() => {
    let gone = false;
    const outcome = () => (holdRef.current && holdRef.current.transform) || null;
    const isHidden = () => document.visibilityState === "hidden";
    const listen = (on) => {
      const method = on ? "addEventListener" : "removeEventListener";
      window[method]("pagehide", onPageHide);
      document[method]("visibilitychange", onVisibility);
    };
    // Once a hold waited for settles: a mounted, shown editor leaves the edit
    // to release(), which writes it after the render adopting the outcome.
    const writeOnSettle = (h) => () => {
      if (!gone && !isHidden()) return;
      if (gone) listen(false);
      if (takeOwed()) fireRunningWrite(isHidden(), h.transform);
    };
    const flushDurably = (goingAway) => {
      const h = holdRef.current;
      if (!goingAway && h && !h.settled) {
        h.onSettle = writeOnSettle(h);
        return;
      }
      if (takeOwed()) fireRunningWrite(true, outcome());
      if (gone) listen(false);
    };
    const onPageHide = () => flushDurably(true);
    const onVisibility = () => { if (isHidden()) flushDurably(false); };
    listen(true);
    return () => {
      gone = true;
      const h = holdRef.current;
      const owed = takeOwed();
      if (owed && h && !h.settled) {
        deferredRef.current = true;
        h.onSettle = writeOnSettle(h);
        return;
      }
      listen(false);
      if (owed) fireRunningWrite(false, outcome());
    };
  }, []);

  // markDirty: call from every user-driven mutation handler (addPt,
  // removePt, foul increment/decrement, draw toggle, encho change, team
  // sub-bout edits). Do NOT call from prop/SSE-driven state writes. An edit
  // that does not call it is not saved by the unmount or the page-hide flush.
  const markDirty = () => {
    if (!isRunningRef.current) return; // gate 1: never auto-start a scheduled match
    // Re-arm WITHOUT releasing the pending edit (not cancelDebounce): the edit
    // is still pending, and a release here would publish synced-then-syncing
    // on every tap inside the window.
    clearTimer();
    // The new timer carries every edit so far (buildPatchRef reads current
    // state), so a deferred one is superseded by it, not lost.
    deferredRef.current = false;
    notePending(true);
    editPerfRef.current = perfNow();
    timerRef.current = setTimeout(() => {
      timerRef.current = null;
      if (holdRef.current) { deferredRef.current = true; return; }
      fireRunningWrite();
    }, AUTOSAVE_DEBOUNCE_MS);
  };

  return { markDirty, cancelDebounce, hold, release };
}
