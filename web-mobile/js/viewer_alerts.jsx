// viewer_alerts.jsx — alert/notification-trigger hooks + banners extracted
// from viewer.jsx (mp-pxxc step 3). Pure file split: no behaviour change.
//
// Sharing model: esbuild compiles each .jsx to dist/.js individually (no bundle);
// the server rewrites `/dist/X.jsx` → the compiled `X.js`, so ES `import "./X.jsx"`
// specifiers resolve in the browser. viewer.js is the SOLE viewer entry script in
// index.html and imports this module — do NOT give it its own <script type="module">
// tag, or the browser fetches it under a second URL (.js?v=N vs .jsx) and evaluates
// it twice (double-load; same class as mp-zd1v).
//
// Cycle note: viewer.jsx imports from this file and re-exports every symbol
// here (plus window.* assignments) so the public surface of viewer.jsx is
// unchanged. subscribePermissionChanges (private to AnnBellBtn) lives in
// viewer_notifications.jsx and imports dispatchNotif from here — not a cycle:
// viewer_notifications.jsx loads after viewer_alerts.jsx, so the import resolves.

import { LS_NOTIFICATIONS_ENABLED } from './notification_keys.jsx';

const { useState, useRef: useRefV, useEffect } = React;

// ---------------------------------------------------------------------------
// mp-4fd: On-deck match alert — predicate, hook, banner, chime
// ---------------------------------------------------------------------------

// LocalStorage key for the chime mute preference (viewer-level).
const LS_CHIME_MUTED = "viewer.matchAlert.chimeMuted";

// Hook: chime-muted preference backed by localStorage.
// useChimeMuted syncs across multiple ViewerHome mounts via
// a custom DOM event dispatched on toggle — the native `storage` event only
// fires across tabs, not within the same page.
const CHIME_SYNC_EVENT = "chimeMutedSync";
// Mirrors CHIME_SYNC_EVENT for the notifications-enabled flag so all AnnBellBtn
// instances stay visually in sync within one page. The watchlist bell is driven
// by chimeMuted (chime-only) and does not subscribe to this event.
export const NOTIF_SYNC_EVENT = "notifEnabledSync";

// Notification opt-in helpers used by AnnBellBtn and handleBellToggle. Return values for notifEnable: "on" (granted + LS write
// succeeded), "off" (dismissed / threw / localStorage failed), "denied" (permanently blocked).
export function dispatchNotif(enabled) {
  try { window.dispatchEvent(new CustomEvent(NOTIF_SYNC_EVENT, { detail: enabled })); } catch (_e) { /* ignore */ }
}
// Guards an async handler against concurrent re-entry. ref is a useRef(false).
export async function runOnce(ref, fn) {
  if (ref.current) return;
  ref.current = true;
  try { return await fn(); } finally { ref.current = false; }
}
// Shared promise: concurrent callers (handleBellToggle + AnnBellBtn) all await
// the same permission dialog and get the same outcome instead of an immediate
// "off" that can leave bells out of sync with the first call's actual result.
let notifEnablePromise = null;
// Set to true by notifDisable() while notifEnable() is awaiting the permission
// dialog. Checked before the final LS write so a concurrent disable is not
// overridden when the user approves the dialog after having already tapped disable.
// Reset at the START of every notifEnable() call (before the in-flight guard) so
// that a caller joining an existing promise also signals "I want notifications on".
let notifCancelled = false;
export async function notifEnable() {
  notifCancelled = false; // reset before the guard: any new enable clears a prior cancel
  if (notifEnablePromise) return notifEnablePromise;
  notifEnablePromise = (async () => {
    if (typeof Notification === "undefined") { dispatchNotif(false); return "off"; }
    if (Notification.permission === "denied") {
      dispatchNotif(false);
      return "denied";
    }
    if (Notification.permission === "default") {
      let r;
      try { r = await Notification.requestPermission(); } catch (_e) {
        dispatchNotif(false);
        return "off";
      }
      if (r !== "granted") {
        dispatchNotif(false);
        return r === "denied" ? "denied" : "off";
      }
      // Only reachable after an async await — check cancel flag set by a
      // concurrent notifDisable() call that arrived while the dialog was open.
      if (notifCancelled) { dispatchNotif(false); return "off"; }
    }
    try { window.localStorage.setItem(LS_NOTIFICATIONS_ENABLED, "true"); } catch (_e) { /* quota */ }
    // Read back the actual persisted value — setItem may have thrown while the key
    // was already "true" (locked or quota-exceeded on a pre-existing opt-in).
    // Dispatching the real stored state keeps bells in sync with what
    // fireNotification() will see, mirroring the same pattern in notifDisable().
    let stored = false;
    try { stored = window.localStorage.getItem(LS_NOTIFICATIONS_ENABLED) === "true"; } catch (_e2) { /* ignore */ }
    dispatchNotif(stored);
    return stored ? "on" : "off";
  })().finally(() => { notifEnablePromise = null; });
  return notifEnablePromise;
}
export function notifDisable() {
  notifCancelled = true; // cancel any in-flight notifEnable() before the LS write
  try {
    window.localStorage.setItem(LS_NOTIFICATIONS_ENABLED, "false");
  } catch (_e) {
    // If setItem fails (quota), try removing the key so fireNotification won't see "true".
    try { window.localStorage.removeItem(LS_NOTIFICATIONS_ENABLED); } catch (_e2) { /* storage locked */ }
  }
  // Dispatch the actual persisted state so listeners don't show "off" when the
  // key is still "true" (i.e. both setItem and removeItem failed).
  let nowEnabled = false;
  try { nowEnabled = window.localStorage.getItem(LS_NOTIFICATIONS_ENABLED) === "true"; } catch (_e) { /* ignore */ }
  dispatchNotif(nowEnabled);
}

export function useChimeMuted() {
  const [muted, setMuted] = useState(() => {
    if (typeof window === "undefined") return true;
    try {
      // Chime is opt-in: absent/null key → muted. The user opts in via
      // handleBellToggle; onFirstAdd triggers it automatically on the first
      // watchlist entry. Stored "false" = chime enabled (inverted for ergonomics:
      // "muted=false" means sound is on).
      const stored = window.localStorage.getItem(LS_CHIME_MUTED);
      return stored !== "false";
    } catch (_e) { return true; }
  });
  // Write the default once after mount so future default changes don't silently
  // flip returning users who never stored a preference. Kept out of the useState
  // initializer to avoid side effects during render (initializers can re-run on
  // strict-mode double-invocation / remounts).
  useEffect(() => {
    if (typeof window === "undefined") return;
    try {
      if (window.localStorage.getItem(LS_CHIME_MUTED) === null) {
        window.localStorage.setItem(LS_CHIME_MUTED, "true");
      }
    } catch (_e) { /* ignore */ }
  }, []);
  // Sync across same-page instances via custom event.
  useEffect(() => {
    if (typeof window === "undefined") return;
    const onSync = (e) => setMuted(!!e.detail);
    window.addEventListener(CHIME_SYNC_EVENT, onSync);
    return () => window.removeEventListener(CHIME_SYNC_EVENT, onSync);
  }, []);
  // Functional updater: `prev` is the current state at call time, not a
  // captured closure value. This lets callers call toggle() twice (optimistic
  // flip + revert on throw) without stale-closure issues.
  // `next` is captured from inside the updater and read after setMuted returns.
  // Preact 10 (hooks.umd.js in vendor/) executes functional updaters
  // synchronously within the useState setter call, so `next` is always
  // defined before the LS write and dispatch below. If upgrading Preact beyond
  // v10, verify this invariant still holds — the effect-based alternative is
  // a useEffect on `muted` for LS/dispatch, but it adds a render cycle delay.
  const _applyMuted = (v) => {
    if (typeof window === "undefined") return;
    try { window.localStorage.setItem(LS_CHIME_MUTED, v ? "true" : "false"); } catch (_e) { /* ignore */ }
    try { window.dispatchEvent(new CustomEvent(CHIME_SYNC_EVENT, { detail: v })); } catch (_e) { /* ignore */ }
  };
  const toggle = () => {
    let next;
    setMuted(prev => { next = !prev; return next; });
    _applyMuted(next);
  };
  const setChimeMuted = (value) => { setMuted(value); _applyMuted(value); };
  return [muted, toggle, setChimeMuted];
}

// Extract a side's display name from the normalised match shape.
// Handles both {sideA: {name}} (normalised) and flat sideAName (legacy).
function matchSideName(side, fallbackName) {
  return (side && side.name) || fallbackName || "";
}

// Predicate: is the followed player's match on-deck (up-next or running)?
// Exported for unit testing and mp-5px (service worker path must reuse this).
export function isFollowedMatchOnDeck(m) {
  if (!m) return false;
  if (m.status === "running") return true;
  if (m.status === "scheduled" && Number(m.queuePosition) === 1) return true;
  return false;
}

// Hook: detect transitions into on-deck state for the followed match.
// Fires the alert surfaces (document.title, chime, backgrounded Notification)
// exactly once per genuine transition; ignores SSE re-renders that leave the
// state unchanged. Accepts a callback `onAlert` for testability.
//
// Dedup is by "signature" (matchId + "running" or "upnext"), NOT a timer.
// First render primes the ref without alerting (avoids false alert on mount
// when loading a late-open tab).
export function useFollowedMatchAlert(myNextMatch, { chimeMuted, onAlert } = {}) {
  const lastSigRef = useRefV(null); // null = not yet primed
  const audioCtxRef = useRefV(null);
  const originalTitleRef = useRefV(null);

  // Unlock AudioContext on first user interaction (gesture gate).
  // Also cleans up AudioContext and restores document.title on unmount.
  useEffect(() => {
    const unlock = () => {
      if (audioCtxRef.current && audioCtxRef.current.state === "suspended") {
        audioCtxRef.current.resume().catch(() => {});
      }
    };
    if (typeof window !== "undefined") {
      window.addEventListener("click", unlock, { passive: true });
      window.addEventListener("touchstart", unlock, { passive: true });
    }
    return () => {
      if (typeof window !== "undefined") {
        window.removeEventListener("click", unlock);
        window.removeEventListener("touchstart", unlock);
      }
      // Restore document.title if it was flashed when unmounting.
      if (originalTitleRef.current !== null && typeof document !== "undefined") {
        document.title = originalTitleRef.current;
        originalTitleRef.current = null;
      }
      // Close AudioContext to free the browser resource.
      if (audioCtxRef.current) {
        try { audioCtxRef.current.close(); } catch (_e) { /* already closed */ }
        audioCtxRef.current = null;
      }
    };
  }, []);

  useEffect(() => {
    const m = myNextMatch;
    const onDeck = isFollowedMatchOnDeck(m);

    // Build the signature for this state.  Always a string so the
    // strict-equality fast-path (sig === lastSigRef.current) works when
    // off-deck too — null !== "" would bypass the dedup on every render.
    const sig = onDeck && m
      ? m.id + ":" + (m.status === "running" ? "running" : "upnext")
      : "";

    // First call: prime without alerting (avoids false alert on reconnect).
    if (lastSigRef.current === null) {
      lastSigRef.current = sig || "";
      return;
    }

    // No transition: same sig as before.
    if (sig === lastSigRef.current) return;
    lastSigRef.current = sig || "";

    if (!onDeck) {
      // Left on-deck: restore title.
      if (originalTitleRef.current !== null && typeof document !== "undefined") {
        document.title = originalTitleRef.current;
        originalTitleRef.current = null;
      }
      return;
    }

    // Genuine transition INTO on-deck: fire all alert surfaces.

    // 1. document.title flash.
    if (typeof document !== "undefined") {
      if (originalTitleRef.current === null) {
        originalTitleRef.current = document.title;
      }
      const titlePrefix = m.status === "running" ? "🔴 NOW — " : "(1) Your match is next — ";
      document.title = titlePrefix + (originalTitleRef.current || "Tournament");
    }

    // 2. Chime via WebAudio (two-tone, no asset needed).
    if (!chimeMuted) {
      try {
        const AudioCtx = typeof AudioContext !== "undefined" ? AudioContext
          : (typeof window !== "undefined" ? window.AudioContext || window.webkitAudioContext : null);
        if (AudioCtx) {
          if (!audioCtxRef.current) audioCtxRef.current = new AudioCtx();
          const ctx = audioCtxRef.current;
          const playTone = (freq, startTime, duration) => {
            const osc = ctx.createOscillator();
            const gain = ctx.createGain();
            osc.connect(gain);
            gain.connect(ctx.destination);
            osc.type = "sine";
            osc.frequency.value = freq;
            gain.gain.setValueAtTime(0.3, startTime);
            gain.gain.exponentialRampToValueAtTime(0.001, startTime + duration);
            osc.start(startTime);
            osc.stop(startTime + duration);
          };
          const t0 = ctx.currentTime;
          playTone(880, t0, 0.25);
          playTone(1100, t0 + 0.3, 0.35);
        }
      } catch (_e) { /* autoplay restrictions or unavailable AudioContext — silent fail */ }
    }

    // 3. Backgrounded browser Notification (reuses existing opt-in).
    if (typeof window !== "undefined" && typeof window.fireNotification === "function" && m) {
      const sideA = matchSideName(m.sideA, m.sideAName);
      const sideB = matchSideName(m.sideB, m.sideBName);
      const courtStr = m.court ? ` — Shiaijo ${m.court}` : "";
      const body = (sideA && sideB) ? `${sideA} vs ${sideB}${courtStr}` : courtStr.slice(3) || "";
      const notifTitle = m.status === "running" ? "Your match is on now" : "Your match is next";
      window.fireNotification(notifTitle, body, { tag: "match-" + m.id });
    }

    // 4. Notify consumer (e.g. to show/update the banner).
    if (typeof onAlert === "function") onAlert(m);
  });
}

// mp-xhaa: secondary (non-primary) watched matches get a QUIET, rate-limited
// banner — no chime, no title flash, no notification. This is the anti-storm
// rule (critique P2): a coach watching a 50-player dojo must not be pinged for
// every student. The decision is a pure function so the rate-limiting is
// unit-testable without timers.
//
//   prev:       { seen: string[], lastAt: number }
//   onDeck:     match objects currently on-deck for non-primary watched players
//   now:        epoch ms (Date.now())
//   cooldownMs: minimum gap between secondary banners
//
// Returns { fire, match, seen, lastAt } — whether to surface a banner, which
// match, and the next state. A "signature" (id + running/upnext) dedups SSE
// re-renders; `seen` is pruned to matches still on-deck so a match that leaves
// and returns can alert again.
function secondaryAlertSig(m) {
  return (m && m.id != null) ? String(m.id) + ":" + (m.status === "running" ? "running" : "upnext") : "";
}

export function computeSecondaryAlert(prev, onDeck, now, cooldownMs) {
  const current = (Array.isArray(onDeck) ? onDeck : [])
    .map((m) => ({ m, sig: secondaryAlertSig(m) }))
    .filter((o) => o.sig);
  const currentSigs = current.map((o) => o.sig);
  const prevSeen = (prev && Array.isArray(prev.seen)) ? prev.seen : [];
  const lastAt = (prev && typeof prev.lastAt === "number") ? prev.lastAt : 0;
  const prunedSeen = prevSeen.filter((s) => currentSigs.includes(s));
  const fresh = current.filter((o) => !prevSeen.includes(o.sig));
  if (fresh.length === 0) {
    return { fire: false, match: null, seen: prunedSeen, lastAt };
  }
  // Within the cooldown window: suppress, but DON'T mark the fresh sigs seen,
  // so they fire as soon as the cooldown elapses on a later render.
  if (now - lastAt < cooldownMs) {
    return { fire: false, match: null, seen: prunedSeen, lastAt };
  }
  // Cooled down: fire ONE banner (the earliest-scheduled fresh match), mark
  // every current on-deck sig seen so the rest stay quiet, and start cooldown.
  const ordered = fresh.slice().sort((a, b) => (a.m.scheduledAt || "99:99").localeCompare(b.m.scheduledAt || "99:99"));
  return { fire: true, match: ordered[0].m, seen: currentSigs, lastAt: now };
}

// Hook wrapper around computeSecondaryAlert. Primes on first render without
// firing (so a late-opened tab doesn't banner for matches already on-deck),
// then re-evaluates on every render (dedup is by signature, mirroring
// useFollowedMatchAlert). Cooldown defaults to 30s.
export function useSecondaryWatchAlert(secondaryOnDeck, { onSecondaryAlert, cooldownMs = 30000 } = {}) {
  const stateRef = useRefV(null); // null = not yet primed
  useEffect(() => {
    const now = (typeof Date !== "undefined" && typeof Date.now === "function") ? Date.now() : 0;
    if (stateRef.current === null) {
      const sigs = (Array.isArray(secondaryOnDeck) ? secondaryOnDeck : []).map(secondaryAlertSig).filter(Boolean);
      stateRef.current = { seen: sigs, lastAt: 0 };
      return;
    }
    const res = computeSecondaryAlert(stateRef.current, secondaryOnDeck, now, cooldownMs);
    stateRef.current = { seen: res.seen, lastAt: res.lastAt };
    if (res.fire && typeof onSecondaryAlert === "function") onSecondaryAlert(res.match);
  });
}

// Banner component: rendered when the followed match is on-deck.
export function MyMatchAlertBanner({ match, onView, onDismiss }) {
  if (!match) return null;
  const kind = match.status === "running" ? "NOW" : "Next up";
  const sideA = matchSideName(match.sideA, match.sideAName);
  const sideB = matchSideName(match.sideB, match.sideBName);
  const vs = (sideA && sideB) ? `${sideA} vs ${sideB}` : "";
  const courtStr = match.court ? `Shiaijo ${match.court}` : "";
  return (
    <div className="match-alert-banner" data-testid="match-alert-banner" role="alert">
      <div className="match-alert-banner__content">
        <span className="match-alert-banner__badge">{kind}</span>
        <span className="match-alert-banner__text">
          {vs && <strong>{vs}</strong>}
          {courtStr && <span> · {courtStr}</span>}
        </span>
      </div>
      <div className="match-alert-banner__actions">
        {onView && (
          <button type="button" className="btn btn--sm btn--primary" onClick={() => onView(match)}>
            View
          </button>
        )}
        {onDismiss && <button type="button"
          className="match-alert-banner__dismiss"
          onClick={onDismiss}
          aria-label="Dismiss match alert"
        >✕</button>}
      </div>
    </div>
  );
}
