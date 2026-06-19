// viewer_notifications.jsx — notification-settings + announcement UI extracted
// from viewer.jsx (mp-pxxc step 8). Pure file split: no behaviour change.
//
// Sharing model: esbuild compiles each .jsx to dist/.js individually (no bundle);
// the server rewrites `/dist/X.jsx` → the compiled `X.js`, so ES `import "./X.jsx"`
// specifiers resolve in the browser. viewer.js is the SOLE viewer entry script in
// index.html and imports this module — do NOT give it its own <script type="module">
// tag, or the browser fetches it under a second URL (.js?v=N vs .jsx) and evaluates
// it twice (double-load; same class as mp-zd1v).
//
// Exports: notificationSupported, AnnBellBtn, AnnouncementCard, AnnouncementBanner.
// window.AnnouncementBanner is set here (assigned at module-eval, when viewer.js
// imports this module) because app.jsx renders it via window without ES-importing
// this module. window.AnnouncementCard is also assigned to preserve the original
// viewer.jsx window surface verbatim (no current window consumer; ES importers use
// the export above).

import { LS_NOTIFICATIONS_ENABLED } from './notification_keys.jsx';
import { NOTIF_SYNC_EVENT, dispatchNotif, runOnce, notifEnable, notifDisable } from './viewer_alerts.jsx';

const { useState, useRef: useRefV, useEffect } = React;

// Module-level Permissions API singleton: dispatches NOTIF_SYNC_EVENT when the
// browser permission changes externally (user visits browser settings mid-session).
// AnnBellBtn calls this once per mount; no-op on subsequent mounts.
let _permSubscribed = false;
// Set after the first query() rejection so permanently-unsupported browsers
// (those that reject 'notifications' queries) don't retry on every AnnBellBtn mount.
let _permGaveUp = false;
export function subscribePermissionChanges() {
  if (_permSubscribed || _permGaveUp) return;
  try {
    const pq = navigator.permissions?.query?.({ name: "notifications" });
    // query() is absent on some WebViews / old iOS — optional-chain returns undefined.
    // Set _permGaveUp (not _permSubscribed) so future mounts skip retrying on a
    // browser that permanently lacks the API.
    if (!pq) { _permGaveUp = true; return; }
    pq.then((s) => {
      // Only set _permSubscribed once the promise resolves and the listener is wired;
      // a synchronous throw from pq.then() (non-conforming non-Promise) is caught
      // below and sets _permGaveUp instead, keeping the two flags mutually exclusive.
      _permSubscribed = true;
      const handleChange = () => {
        if (s.state === "denied" || s.state === "prompt") { dispatchNotif(false); return; }
        // "granted" — re-read LS so the dispatched state matches what fireNotification sees.
        let optIn = false;
        try { optIn = window.localStorage.getItem(LS_NOTIFICATIONS_ENABLED) === "true"; } catch (_e) { /* storage unavailable */ }
        dispatchNotif(optIn);
      };
      if (typeof s.addEventListener === "function") {
        s.addEventListener("change", handleChange);
      } else {
        s.onchange = handleChange;
      }
    }).catch(() => { _permGaveUp = true; });
  } catch (_e) {
    // pq.then() threw synchronously (non-conforming non-Promise pq) — give up
    // consistently with the !pq branch and the .catch() path above.
    _permGaveUp = true;
  }
}

// Pure helper: detect Notification API support.
// Exported for unit testing.
export function notificationSupported() {
  return typeof Notification !== "undefined";
}

// Shared formatter so the synchronous initializer and the useEffect tick
// produce identical strings — keeps the first paint stable.
function formatAnnouncementTimeLeft(expiresAtIso) {
  const diff = new Date(expiresAtIso).getTime() - Date.now();
  if (diff <= 0) return "";
  const totalSeconds = Math.floor(diff / 1000);
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  const paddedSeconds = seconds.toString().padStart(2, "0");
  return minutes > 0 ? `${minutes}:${paddedSeconds} left` : `${seconds}s left`;
}

// AnnBellBtn — per-announcement bell icon that opts the viewer into browser notifications.
export function AnnBellBtn() {
  // viewer_watchlist.js is loaded before viewer.js in index.html; BellIcon
  // is always set by the time this component renders.
  const BellIcon = window.BellIcon;
  const supported = notificationSupported();
  const inFlight = useRefV(false);
  const [state, setState] = useState(() => {
    if (!supported) return "unsupported";
    if (Notification.permission === "denied") return "denied";
    try {
      const optIn = window.localStorage.getItem(LS_NOTIFICATIONS_ENABLED) === "true";
      return (optIn && Notification.permission === "granted") ? "on" : "off";
    } catch (_e) { return "off"; }
  });

  // Sync with other AnnBellBtn instances on the page (watchlist bell is chime-only).
  // External permission changes (user visits browser settings) arrive via
  // NOTIF_SYNC_EVENT dispatched by the module-level subscribePermissionChanges singleton.
  useEffect(() => {
    if (!supported) return;
    const onSync = (e) => {
      if (Notification.permission === "denied") { setState("denied"); return; }
      setState((e.detail && Notification.permission === "granted") ? "on" : "off");
    };
    // CustomEvents dispatched on window are same-origin; no origin check needed.
    window.addEventListener(NOTIF_SYNC_EVENT, onSync);
    // Wire up the singleton Permissions API observer (no-op if already registered).
    subscribePermissionChanges();
    return () => {
      window.removeEventListener(NOTIF_SYNC_EVENT, onSync);
    };
  }, []); // supported is a static boolean; the effect only wires up listeners once

  if (state === "unsupported") return null;
  if (!BellIcon) return null; // viewer_watchlist.js failed to load

  const toggle = () => runOnce(inFlight, async () => {
    if (state === "on") {
      notifDisable();
      return;
    }
    await notifEnable();
    // State is driven by the NOTIF_SYNC_EVENT listener (onSync above).
  });
  return (
    <button
      className={`ann-bell-btn${state === "on" ? " ann-bell-btn--on" : ""}${state === "denied" ? " ann-bell-btn--denied" : ""}`}
      onClick={toggle}
      disabled={state === "denied"}
      aria-pressed={state === "on"}
      aria-label={state === "denied" ? "Notifications blocked in your browser" : state === "on" ? "Notifications on — tap to disable" : "Get notified of announcements"}
      title={state === "denied" ? "Notifications blocked in browser settings" : state === "on" ? "Notifications on" : "Notify me of announcements"}
    >
      <BellIcon muted={state !== "on"} size={13} />
    </button>
  );
}

// AnnouncementCard — renders a single announcement card with its own
// independent per-card countdown and auto-dismiss timer.
// Exported for unit testing; consumed only by AnnouncementBanner below.
export function AnnouncementCard({ ann, onDismiss }) {
  const [timeLeft, setTimeLeft] = useState(() => formatAnnouncementTimeLeft(ann.expiresAt));

  useEffect(() => {
    // intervalId and dismissed are captured in the closure so updateTimer can
    // self-clear the interval on expiry and guard against repeated onDismiss
    // calls if React state updates are delayed before cleanup runs.
    let intervalId;
    let dismissed = false;
    const updateTimer = () => {
      const diff = new Date(ann.expiresAt).getTime() - Date.now();
      if (diff <= 0) {
        clearInterval(intervalId);
        if (!dismissed) {
          dismissed = true;
          onDismiss(ann.id);
        }
        return;
      }
      setTimeLeft(formatAnnouncementTimeLeft(ann.expiresAt));
    };
    updateTimer();
    intervalId = setInterval(updateTimer, 1000);
    return () => clearInterval(intervalId);
  }, [ann.id, ann.expiresAt, onDismiss]);

  return (
    <div className="announcement-banner">
      <div className="announcement-banner__content">
        <div className="announcement-banner__icon" aria-hidden="true">📢</div>
        <p className="announcement-banner__message">{ann.message}</p>
      </div>
      <div className="announcement-banner__meta">
        <span className="announcement-banner__badge">{timeLeft}</span>
        <AnnBellBtn />
        <button
          className="announcement-banner__dismiss"
          onClick={() => onDismiss(ann.id)}
          aria-label="Dismiss announcement"
        >
          &times;
        </button>
      </div>
    </div>
  );
}

// AnnouncementBanner — fixed-position overlay that stacks ALL active
// announcements as independent cards. Does NOT rotate; each card owns
// its own countdown and auto-dismiss timer. Public props unchanged so
// app.jsx needs no edit.
export function AnnouncementBanner({ announcements, onDismiss }) {
  const list = announcements || [];
  if (list.length === 0) return null;

  return (
    <div className="announcement-overlay" role="region" aria-label="Announcements">
      {list.map(ann => (
        <AnnouncementCard key={ann.id} ann={ann} onDismiss={onDismiss} />
      ))}
    </div>
  );
}

if (typeof window !== 'undefined') {
  window.AnnouncementCard = AnnouncementCard;
  window.AnnouncementBanner = AnnouncementBanner;
}
