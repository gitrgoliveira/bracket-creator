// Autosave / sync-status infra for the score editor modals.
// Used by both ScoreEditorModal (individual) and TeamScoreEditorModal (team).
// Extracted from admin_scoring_modal.jsx (mp-zac3).

const { useState: useStateA, useEffect: useEffectA, useRef: useRefA } = React;

export const AUTOSAVE_DEBOUNCE_MS = 300;

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
// being hidden, a pending edit is written at once with `durable: true` on the
// patch, which makes recordScore put it straight into the persisted outbox.
// Every write carries the time of the edit it saves (editedPerf), so however
// late it goes out, it is never newer than a result recorded after the tap.
export function useDebouncedRunningWrite({ isRunningRef, buildPatchRef, onSubmitRef }) {
  const timerRef = useRefA(null);
  // The monotonic reading of the last edit, sent on the patch as `editedPerf`
  // (never on the wire) so recordScore stamps the write with the time of that
  // edit, not of its sending (api_client.jsx _editAge).
  const editPerfRef = useRefA(null);
  // One token per editor instance, so two open editors never release each
  // other's pending edit.
  const pendingTokenRef = useRefA({});
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

  // cancelDebounce: call this before any explicit submit (Start / Finish /
  // Hantei / Decision) so the queued timer can't fire afterward, and before
  // closing on the operator's Discard, so the unmount does not write what they
  // threw away. Nothing of this hook's is left pending then, so the pending
  // edit is released too.
  const cancelDebounce = () => {
    if (clearTimer()) notePending(false);
  };

  // The running write itself, shared by the debounce timer, the unmount and
  // the page-hide flush so they can never apply different gates. `durable`
  // asks recordScore to queue the write rather than fetch it (bc-sync).
  const fireRunningWrite = (durable) => {
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
        const patch = { ...buildPatchRef.current("running"), editedPerf: editPerfRef.current };
        const p = onSubmitRef.current(durable ? { ...patch, durable: true } : patch);
        if (p && typeof p.catch === "function") p.catch(() => {});
      } catch (_) { /* swallow */ }
    } finally {
      notePending(false);
    }
  };

  // Unmount writes an edit still inside the window, whatever unmounted the
  // editor (operator ruling 2026-09-27): Close, Prev/Next or their keys,
  // another match picked, a correction opened, a court switch, or the host
  // moving on by itself because another device finished the match. That last
  // one is why the write carries the time of the tap (editedPerf): the tap is
  // older than that finish, so the server keeps the finish. Discarding is the
  // one way out that saves nothing: the editor cancels first (cancelDebounce).
  // A reload never unmounts; the pagehide listener below keeps an edit across
  // one. Everything this reads is a ref, so the mount-time closure is safe.
  useEffectA(() => () => { if (clearTimer()) fireRunningWrite(); }, []);

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
    notePending(true);
    editPerfRef.current = perfNow();
    timerRef.current = setTimeout(() => {
      timerRef.current = null;
      fireRunningWrite();
    }, AUTOSAVE_DEBOUNCE_MS);
  };

  // bc-sync: the page is going away (a reload, a closed tab, the iPad locking
  // or switching app). An edit still in the debounce window is written NOW,
  // durably: recordScore puts a `durable` write into the persisted outbox,
  // because a fetch started here would die with the document. Only a PENDING
  // edit is written; with nothing pending there is nothing to lose. The
  // mount-time closures are safe here: everything they read is a ref.
  useEffectA(() => {
    const flushDurably = () => {
      if (!clearTimer()) return;
      fireRunningWrite(true);
    };
    const onVisibility = () => { if (document.visibilityState === "hidden") flushDurably(); };
    window.addEventListener("pagehide", flushDurably);
    document.addEventListener("visibilitychange", onVisibility);
    return () => {
      window.removeEventListener("pagehide", flushDurably);
      document.removeEventListener("visibilitychange", onVisibility);
    };
  }, []);

  return { markDirty, cancelDebounce };
}
