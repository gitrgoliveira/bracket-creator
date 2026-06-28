// Autosave / sync-status infra for the score editor modals.
// Used by both ScoreEditorModal (individual) and TeamScoreEditorModal (team).
// Extracted from admin_scoring_modal.jsx (mp-zac3).

const { useState: useStateA, useEffect: useEffectA, useRef: useRefA } = React;

export const AUTOSAVE_DEBOUNCE_MS = 300;

// ---------------------------------------------------------------------------
// C2: SyncStatusPill
// ---------------------------------------------------------------------------
// Small indicator rendered in the scoring-panel header while a match is
// running. Subscribes to the write-queue sync status from api_client.jsx
// (via window.subscribeSyncStatus) and reflects:
//   synced     : last write landed; no queue pending
//   syncing    : write in flight / in queue
//   offline    : network down; queue retrying with backoff
//
// COPY RULE: NEVER use the word "live" in user-facing strings.
// Colors use design tokens only (var(--...)) : no hardcoded hex.
// ---------------------------------------------------------------------------

// Module-level const : hoisted so the object is not rebuilt on every render.
const SYNC_PILL_CONFIG = {
  synced: { label: 'Synced',   cls: 'sync-pill--synced',  dot: '●' },
  syncing: { label: 'Syncing…', cls: 'sync-pill--syncing', dot: '◌' },
  offline: { label: 'Offline',  cls: 'sync-pill--offline', dot: '●' },
};

export function SyncStatusPill({ isRunning }) {
  // The component always mounts and subscribes to sync status (the subscription
  // is a single Set entry and replays the current value on subscribe). It only
  // renders a VISIBLE pill while the match is running : autosave fires only on
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
  return (
    <span className={`sync-status-pill ${c.cls}`} data-testid="sync-status-pill" aria-label={`Score sync: ${c.label}`}>
      <span className="sync-pill__dot" aria-hidden="true">{c.dot}</span>
      <span className="sync-pill__label">{c.label}</span>
    </span>
  );
}

export function useDebouncedRunningWrite({ isRunningRef, buildPatchRef, onSubmitRef, mountedRef }) {
  const timerRef = useRefA(null);

  // cancelDebounce : call this before any explicit submit (Start / Finish /
  // Hantei / Decision) so the queued timer can't fire afterward.
  const cancelDebounce = () => {
    if (timerRef.current !== null) {
      clearTimeout(timerRef.current);
      timerRef.current = null;
    }
  };

  // Clear on unmount so the closure can't fire after the component is gone.
  useEffectA(() => () => { cancelDebounce(); }, []);

  // markDirty : call from every user-driven mutation handler (addPt,
  // removePt, foul increment/decrement, draw toggle, encho change, team
  // sub-bout edits). Do NOT call from prop/SSE-driven state writes.
  const markDirty = () => {
    if (!isRunningRef.current) return; // gate 1: never auto-start a scheduled match
    cancelDebounce();
    timerRef.current = setTimeout(() => {
      timerRef.current = null;
      if (!mountedRef.current) return;
      // gate 3: re-check running at FIRE time. If the match was completed
      // during the debounce window (this operator's Finish cancels the timer,
      // but an SSE update or another operator can complete it out from under
      // us), isRunningRef has flipped false on re-render : sending a
      // status:"running" autosave now would regress the completed result.
      if (!isRunningRef.current) return;
      // Fire-and-forget: errors swallowed; operator's explicit Finish is
      // the authoritative write.
      try {
        const p = onSubmitRef.current(buildPatchRef.current("running"));
        if (p && typeof p.catch === "function") p.catch(() => {});
      } catch (_) { /* swallow */ }
    }, AUTOSAVE_DEBOUNCE_MS);
  };

  return { markDirty, cancelDebounce };
}
