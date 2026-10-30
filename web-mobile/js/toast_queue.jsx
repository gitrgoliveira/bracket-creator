// toast_queue.jsx: the ONE owner of the toast queue (bc-evts stage 6 review 12).
//
// The queue, the toast shown, whether it is visible or fading, and its timers
// live here, at module level, so they survive the Toast being rendered from
// another place (the admin and viewer branches each render one, and switching
// between them remounts it). `Toast` (ui.jsx) only draws the snapshot and calls
// `dismissToast`; `showToast` (app.jsx) calls `pushToast`.
//
// The rules:
// - A LATCHED toast is a notice told once: an error, a warning, and every
//   queue alert but "N sent" (app.jsx passes `latched`). It is shown for the
//   error dwell, carries a dismiss button, and is never replaced by a later
//   toast: what arrives behind it waits in the queue.
// - Strict arrival order: an incoming toast is queued whenever the queue is
//   not empty, or the toast shown is latched and visible, or the toast shown
//   is fading (its close is pending). Only a visible, unlatched toast with
//   nothing queued behind it is replaced at once.
// - Dedupe by IDENTITY, never by words: a toast pushed with an `id` is
//   dropped when the shown (or fading) toast or a queued one carries the same
//   id. Without an id it is never collapsed, so two notices with the same
//   words both show.
// A leaf with no imports. Timers are looked up at call time so a test's fake
// timers apply.

export const TOAST_SUCCESS_DWELL_MS = 2700;
export const TOAST_ERROR_DWELL_MS = 8000;
export const TOAST_FADE_MS = 300;

let _current = null; // { message, type, id, latched, seq, visible }
let _queue = [];
let _seq = 0;
let _hideTimer = null;
let _advanceTimer = null;
const _listeners = new Set();

function _emit() {
  const snap = toastSnapshot();
  for (const fn of Array.from(_listeners)) fn(snap);
}

function _clearTimers() {
  if (_hideTimer !== null) clearTimeout(_hideTimer);
  if (_advanceTimer !== null) clearTimeout(_advanceTimer);
  _hideTimer = null;
  _advanceTimer = null;
}

function _show(entry) {
  _clearTimers();
  _seq += 1;
  _current = { ...entry, seq: _seq, visible: true };
  const dwell = entry.latched ? TOAST_ERROR_DWELL_MS : TOAST_SUCCESS_DWELL_MS;
  _hideTimer = setTimeout(() => {
    _hideTimer = null;
    if (_current) { _current = { ..._current, visible: false }; _emit(); }
  }, dwell);
  _advanceTimer = setTimeout(() => { _advanceTimer = null; _advance(); }, dwell + TOAST_FADE_MS);
  _emit();
}

// The toast shown is done: the next one queued shows, else none.
function _advance() {
  _clearTimers();
  const next = _queue.shift();
  if (next) { _show(next); return; }
  _current = null;
  _emit();
}

// isLatchedToastType: the types latched by default.
export function isLatchedToastType(type) {
  return type === 'error' || type === 'warning';
}

// pushToast: show a toast now or queue it behind the one shown (see above).
// `opts.id` dedupes by identity; `opts.latched` overrides the type's default.
export function pushToast(message, type = 'success', opts = {}) {
  const o = opts || {};
  const id = o.id === undefined || o.id === null ? null : o.id;
  if (id !== null) {
    if (_current && _current.id === id) return;
    if (_queue.some((q) => q.id === id)) return;
  }
  const latched = typeof o.latched === 'boolean' ? o.latched : isLatchedToastType(type);
  const entry = { message, type, id, latched };
  if (!_current) { _show(entry); return; }
  const waits = _queue.length > 0 || !_current.visible || _current.latched;
  if (waits) { _queue.push(entry); return; }
  _show(entry);
}

// dismissToast: the operator closed the toast shown; the next one shows.
export function dismissToast() {
  if (!_current) return;
  _advance();
}

// toastSnapshot: the toast shown, or null. The same object while none of its
// fields has changed, a new one once one has, as useSyncExternalStore (the
// Toast's read of it) requires of a snapshot.
let _snapshot = null;
export function toastSnapshot() {
  if (!_current) return null;
  const { message, type, id, latched, seq, visible } = _current;
  const s = _snapshot;
  if (s && s.message === message && s.type === type && s.id === id
    && s.latched === latched && s.seq === seq && s.visible === visible) return s;
  _snapshot = { message, type, id, latched, seq, visible };
  return _snapshot;
}

// subscribeToast: called with the snapshot on every change; returns the
// unsubscribe.
export function subscribeToast(fn) {
  _listeners.add(fn);
  return () => { _listeners.delete(fn); };
}

// queuedToastCount: how many toasts wait behind the one shown.
export function queuedToastCount() {
  return _queue.length;
}

// resetToastQueue: tests only. Clears the queue, the toast shown and timers.
export function resetToastQueue() {
  _clearTimers();
  _queue = [];
  _current = null;
  _emit();
}
