// Shared UI primitives used by both admin and viewer modules.

function StatusBadge({ status, showLiveDot, format }) {
  const map = {
    setup: ["badge--setup", "Pending"],
    "draw-ready": ["badge--draw-ready", "Draw ready"],
    pools: ["badge--pools", "Pools"],
    playoffs: ["badge--playoffs", "Playoffs"],
    completed: ["badge--completed", "Completed"],
  };
  const [cls, rawLabel] = map[status || "setup"] || ["badge--setup", status];
  const label = (status === "pools" && format === "league") ? "League" : rawLabel;
  const showLive = showLiveDot && (status === "pools" || status === "playoffs");
  return (
    <span className={`badge ${cls}`}>
      {showLive && <span className="dot dot--live" style={{ marginRight: 4 }}></span>}
      {label}
    </span>
  );
}

function formatDate(d) {
  if (!d) return "Date TBA";
  let iso = d;
  // Accept the canonical DD-MM-YYYY form (and the lax DD/MM/YYYY variant)
  // and convert to ISO YYYY-MM-DD, which is what the Date constructor
  // parses unambiguously. Any other shape is passed through unchanged so
  // the Date constructor's NaN check below converts unrecognized inputs
  // to the "Date TBA" fallback.
  const match = d.match(/^(\d{1,2})[-/](\d{1,2})[-/](\d{4})$/);
  if (match) {
    iso = `${match[3]}-${match[2].padStart(2, '0')}-${match[1].padStart(2, '0')}`;
  }
  const date = new Date(iso + "T00:00");
  if (isNaN(date.getTime())) return "Date TBA";
  return date.toLocaleDateString("en-GB", { day: "numeric", month: "short", year: "numeric" });
}

// Toast — single visible slot. Success toasts auto-dismiss quickly and are
// polite (announced when the SR is idle). ERROR toasts dwell ≥8s, expose a
// manual dismiss control, and are assertive (announced immediately) so an
// operator in a noisy hall doesn't miss a failed action. The Toast component
// itself (not the host) refuses to overwrite a still-visible error with a
// later non-error toast — it latches the shown payload and ignores a
// suppressed prop change (see the effect below). These dwell constants are
// module-local; they are not exported.
const TOAST_SUCCESS_DWELL_MS = 2700;
const TOAST_ERROR_DWELL_MS = 8000;

// Toast renders a single visible slot. The host (app.jsx) keeps one toast in
// state and RE-RENDERS the same <Toast> instance with new props on each
// showToast call (no changing key, so it is not remounted). Toast latches the
// payload it is currently showing in its OWN `shown` state and ignores a
// parent prop change that would overwrite a still-visible ERROR with a later
// non-error toast. This protects ops-critical failures from being silently
// replaced by a routine success, without requiring the host to track dwell
// windows.
//
//  - success/info: role=status + aria-live=polite, short auto-dismiss.
//  - error: role=alert + aria-live=assertive, long dwell (>=8s) plus a manual
//    dismiss control; cannot be clobbered by an incoming non-error toast.
function Toast({ message, type, onClose }) {
  // Latch the first non-trivial payload this mount sees. The host re-renders
  // this component (not remounts) when state.toast changes, so without this
  // latch an incoming success prop would overwrite a live error in place.
  const [shown, setShown] = React.useState({ message, type });
  const [visible, setVisible] = React.useState(true);
  const onCloseRef = React.useRef(onClose);
  React.useEffect(() => { onCloseRef.current = onClose; }, [onClose]);

  const shownIsError = shown.type === 'error';

  // Accept an incoming prop change only when it is NOT being suppressed: a
  // later non-error toast is ignored while an error is still visible; anything
  // else (error→error, error→after-dismiss, non-error→anything) replaces.
  React.useEffect(() => {
    const incomingIsError = type === 'error';
    if (shownIsError && visible && !incomingIsError) return; // protect the error
    if (message === shown.message && type === shown.type) return; // no change
    setShown({ message, type });
    setVisible(true);
  }, [message, type]);

  React.useEffect(() => {
    if (!visible) return undefined;
    const dwell = shownIsError ? TOAST_ERROR_DWELL_MS : TOAST_SUCCESS_DWELL_MS;
    const t1 = setTimeout(() => setVisible(false), dwell);
    const t2 = setTimeout(() => onCloseRef.current && onCloseRef.current(), dwell + 300);
    return () => { clearTimeout(t1); clearTimeout(t2); };
  }, [visible, shown.message, shown.type, shownIsError]);

  const role = shownIsError ? 'alert' : 'status';
  const ariaLive = shownIsError ? 'assertive' : 'polite';

  return (
    <div
      className={`toast toast--${shown.type || 'info'} ${visible ? 'is-visible' : ''}`}
      role={role}
      aria-live={ariaLive}
    >
      <div className="toast__icon" aria-hidden="true">{shownIsError ? '⚠️' : '✅'}</div>
      <div className="toast__msg">{shown.message}</div>
      {shownIsError && (
        <button
          className="toast__dismiss"
          aria-label="Dismiss"
          onClick={() => { setVisible(false); if (onCloseRef.current) onCloseRef.current(); }}
        >&times;</button>
      )}
    </div>
  );
}

// StableInput solves the character duplication issue by using a local state
// that only syncs with the parent onBlur or after a debounce, while still
// being "controlled" by receiving props.
//
// For type="number", a cleared input lands as NaN in local state (NOT 0
// as `+""` would produce) so the parent's onChange receives NaN
// explicitly. The render layer maps NaN-or-non-finite values back to ""
// at the value prop so React doesn't warn about "Received NaN for the
// value attribute" and the cleared input stays visually empty. Mirrors
// the decideNumericUpdate pattern used by the AdminSettings team/pool
// inputs at admin_competition.jsx.
function StableInput({ value, onChange, type, autoSelect = true, ...props }) {
  const [local, setLocal] = React.useState(value);
  const timer = React.useRef(null);
  const composing = React.useRef(false);

  // Sync local state when prop changes from outside (e.g. SSE)
  // Only sync if the user is NOT currently focused/composing.
  React.useEffect(() => {
    if (!composing.current && value !== local) setLocal(value);
  }, [value]);

  // Cancel the 200ms debounce on unmount so the timer can't fire
  // onChange(val) (which is the parent's setState) after teardown.
  // Pre-existing in this component before the PR but fits the same
  // teardown-race theme as the admin-side mountedRef sweep — fixing
  // here while the file is open for the NaN-display changes.
  React.useEffect(() => () => clearTimeout(timer.current), []);

  const handleChange = (e) => {
    const raw = e.target.value;
    // For number inputs: empty string → NaN, so a cleared input doesn't
    // collapse to 0 via `+""`. Non-empty strings still parse via unary +
    // (so "2.5" stays 2.5, "abc" becomes NaN — same as before for those).
    const val = type === 'number' ? (raw === "" ? NaN : +raw) : raw;
    setLocal(val);

    // Immediate local update, debounced parent update to avoid race conditions
    // during typing if the parent re-renders the whole tree.
    clearTimeout(timer.current);
    timer.current = setTimeout(() => onChange(val), 200);
  };

  const handleBlur = (e) => {
    composing.current = false;
    clearTimeout(timer.current);
    onChange(local);
    if (props.onBlur) props.onBlur(e);
  };

  const handleFocus = (e) => {
    composing.current = true;
    if (autoSelect) e.target.select();
    if (props.onFocus) props.onFocus(e);
  };

  // Render NaN / non-finite numeric local state as "" so React doesn't
  // warn ("Received NaN for the value attribute") and the input stays
  // visually empty after the user clears it. Non-number types pass
  // through unchanged.
  const displayValue = type === 'number' && !Number.isFinite(local) ? "" : local;

  return (
    <input
      {...props}
      type={type}
      value={displayValue}
      onChange={handleChange}
      onBlur={handleBlur}
      onFocus={handleFocus}
    />
  );
}

function pluralize(count, singular, plural) {
  return count === 1 ? `${count} ${singular}` : `${count} ${plural || singular + 's'}`;
}

function formatAdminHeaderSub(dateStr, venue, courtsCount, compsCount, participantsCount) {
  return [
    dateStr,
    venue,
    pluralize(courtsCount, "shiaijo (court)", "shiaijo (courts)"),
    pluralize(compsCount, "competition"),
    pluralize(participantsCount, "participant")
  ].filter(Boolean).join(" · ");
}

function formatViewerHeaderEyebrow(dateStr, venue) {
  return [dateStr, venue].filter(Boolean).join(" · ");
}

function formatLabel(format) {
  if (format === "mixed")   return "Pools + Knockout";
  if (format === "league")  return "League";
  if (format === "swiss")   return "Swiss";
  return "Knockout";
}

function formatLabelShort(format) {
  if (format === "mixed")   return "P+KO";
  if (format === "league")  return "League";
  if (format === "swiss")   return "Swiss";
  return "KO";
}

// --- Imperative dialog primitive ------------------------------------------
// confirmDialog(opts)/promptDialog(opts) return a Promise resolved by the
// user's choice, backed by a single <DialogHost/> mounted once at the app
// root. They replace native window.confirm/window.prompt across the admin
// SPA so destructive/elevated prompts use the app's themed, accessible modal
// (Escape to cancel, focus management, consistent styling) instead of browser
// chrome that can't be styled, labelled, or (for prompt) mask a password.
//
// Contract preserved from the natives they replace:
//   confirmDialog  → resolves true on confirm, false on cancel/Escape/backdrop.
//   promptDialog   → resolves the typed string on OK, or null on cancel/Escape/
//                    backdrop (an empty submit resolves null, matching the old
//                    `pw ? pw : null` callers).
let _dialogReq = null; // the active request, or null
let _dialogSeq = 0;     // monotonic id, used as the dialog node's React key
const _dialogListeners = new Set();
function _setDialogReq(req) {
  // Defensive: if a dialog is somehow still open when a new one is requested
  // (dialogs are normally sequential/user-driven, but a concurrent trigger
  // must not hang), resolve the previous promise with its cancel value so its
  // awaiter unblocks rather than waiting forever.
  if (_dialogReq && _dialogReq._resolve && req) {
    _dialogReq._resolve(_dialogReq.kind === "confirm" ? false : null);
  }
  // Stamp a unique id so a request that REPLACES an open one gets a fresh React
  // key on the dialog node — forcing a remount so the callback ref re-runs and
  // re-captures the trigger / re-focuses the new input (otherwise the trap and
  // focus would stay pinned to the first dialog). Cheap insurance for the
  // defensive replace path above.
  if (req) req._id = ++_dialogSeq;
  _dialogReq = req;
  _dialogListeners.forEach((fn) => fn(_dialogReq));
}

function confirmDialog(opts = {}) {
  const o = typeof opts === "string" ? { message: opts } : (opts || {});
  return new Promise((resolve) => {
    _setDialogReq({
      kind: "confirm",
      title: o.title || "Please confirm",
      message: o.message || "",
      confirmLabel: o.confirmLabel || "Confirm",
      cancelLabel: o.cancelLabel || "Cancel",
      danger: !!o.danger,
      _resolve: resolve,
    });
  });
}

function promptDialog(opts = {}) {
  const o = typeof opts === "string" ? { message: opts } : (opts || {});
  return new Promise((resolve) => {
    _setDialogReq({
      kind: "prompt",
      title: o.title || "Enter a value",
      message: o.message || "",
      defaultValue: o.defaultValue != null ? String(o.defaultValue) : "",
      placeholder: o.placeholder || "",
      password: !!o.password,
      confirmLabel: o.confirmLabel || "OK",
      cancelLabel: o.cancelLabel || "Cancel",
      _resolve: resolve,
    });
  });
}

// Mounted once (app root). Renders the active request and resolves its promise.
// Idle state renders null so it costs nothing when no dialog is open.
function DialogHost() {
  const [req, setReq] = React.useState(_dialogReq);
  const [value, setValue] = React.useState("");
  const inputRef = React.useRef(null);
  const triggerRef = React.useRef(null);
  const trapRef = React.useRef(null);

  React.useEffect(() => {
    const fn = (r) => { setReq(r); if (r && r.kind === "prompt") setValue(r.defaultValue || ""); };
    _dialogListeners.add(fn);
    // Pick up a request created before this host finished mounting.
    if (_dialogReq) fn(_dialogReq);
    return () => { _dialogListeners.delete(fn); };
  }, []);

  // Modal focus management (WCAG 2.4.3 / ARIA APG dialog) via a callback ref on
  // the dialog node — fires synchronously on mount (open) and unmount (close),
  // which is more reliable here than a [req] effect. On open: remember the
  // trigger, move focus into the dialog (prompt → input; confirm → the dialog
  // container, so no button is "armed" for an accidental Enter), trap Tab, and
  // lock background scroll. On close: tear all that down and restore focus to
  // the trigger. NB: DialogHost renders *inside* #root, so we must NOT set
  // `inert` on #root (it would disable the dialog itself) — the focus trap plus
  // aria-modal carry the background-isolation contract instead.
  const dialogRefCb = React.useCallback((node) => {
    if (node) {
      triggerRef.current = document.activeElement;
      // Save the baseline inline overflow so close restores EXACTLY it, rather
      // than blindly clearing to "" (which would clobber a pre-existing inline
      // value). DialogHost is the only body-overflow manager, and the callback
      // ref guarantees a paired open/close, so this can't get stuck-locked.
      const prevOverflow = document.body.style.overflow;
      document.body.style.overflow = "hidden";
      const onKeyDown = (e) => {
        if (e.key !== "Tab") return;
        const f = [...node.querySelectorAll('button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])')]
          .filter((el) => !el.disabled && el.offsetParent !== null);
        if (f.length === 0) { e.preventDefault(); return; }
        const first = f[0], last = f[f.length - 1], active = document.activeElement;
        if (e.shiftKey && (active === first || active === node)) { e.preventDefault(); last.focus(); }
        else if (!e.shiftKey && active === last) { e.preventDefault(); first.focus(); }
      };
      document.addEventListener("keydown", onKeyDown, true);
      // Move focus into the dialog on a 0ms timer: focusing synchronously
      // during the commit phase gets reset afterwards, and rAF doesn't fire in
      // non-painting/headless contexts — setTimeout(0) defers past commit and
      // still fires. Prompt → input (focus+select the default); confirm → the
      // first real focusable (the close "×", whose Enter is a safe cancel),
      // since a tabindex=-1 container doesn't reliably take programmatic focus.
      const focusTimer = setTimeout(() => {
        if (inputRef.current) { inputRef.current.focus(); inputRef.current.select(); }
        else { (node.querySelector("button") || node).focus(); }
      }, 0);
      trapRef.current = { onKeyDown, focusTimer, prevOverflow };
    } else {
      const t = trapRef.current;
      if (t) {
        clearTimeout(t.focusTimer);
        document.removeEventListener("keydown", t.onKeyDown, true);
        document.body.style.overflow = t.prevOverflow;
        trapRef.current = null;
      }
      const trig = triggerRef.current;
      if (trig && typeof trig.focus === "function" && document.contains(trig)) trig.focus();
    }
  }, []);

  const close = (result) => {
    const r = req;
    setReq(null);
    if (_dialogReq && _dialogReq._resolve === (r && r._resolve)) _dialogReq = null;
    if (r && r._resolve) r._resolve(result);
  };

  if (!req) return null;
  // req is guaranteed truthy past the guard, so no need to handle the null case.
  const cancelResult = req.kind === "confirm" ? false : null;
  const onCancel = () => close(cancelResult);
  const onConfirm = () => close(req.kind === "confirm" ? true : (value || null));

  // Escape-to-cancel is handled ON the dialog container (not via the global
  // useEscapeToClose window listener). The dialog frequently opens on top of a
  // surface that ALSO has a window-level Escape handler (e.g. the scoring
  // modal's handleDismiss). A window listener would let one Escape fire BOTH —
  // cancelling the dialog and re-dismissing the underlying modal. Because focus
  // is trapped inside the dialog, the keydown bubbles through this container
  // first; stopPropagation() keeps it from reaching any window listener.
  const onDialogKeyDown = (e) => {
    if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); onCancel(); }
  };

  return (
    <div className="modal-backdrop" onClick={onCancel}>
      <div key={req._id} className="modal" ref={dialogRefCb} tabIndex={-1} role="dialog" aria-modal="true" aria-label={req.title} onKeyDown={onDialogKeyDown} onClick={(e) => e.stopPropagation()}>
        <div className="modal__head">
          <div className="modal__title">{req.title}</div>
          <button className="modal__close" onClick={onCancel} aria-label="Cancel">&times;</button>
        </div>
        <div className="modal__body">
          {req.message && <p className="dialog-msg">{req.message}</p>}
          {req.kind === "prompt" && (
            <input
              ref={inputRef}
              className="input dialog-prompt-input"
              type={req.password ? "password" : "text"}
              value={value}
              placeholder={req.placeholder}
              // Accessible name: the field has no visible <label>, so name it
              // from the dialog's own prompt (message preferred, title as
              // fallback) — otherwise screen readers announce a bare text field.
              aria-label={req.message || req.title}
              autoComplete={req.password ? "current-password" : "off"}
              onChange={(e) => setValue(e.target.value)}
              onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); onConfirm(); } }}
            />
          )}
        </div>
        <div className="modal__foot">
          <button className="btn btn--ghost" onClick={onCancel}>{req.cancelLabel}</button>
          <button className={`btn ${req.danger ? "btn--danger" : "btn--primary"}`} onClick={onConfirm}>{req.confirmLabel}</button>
        </div>
      </div>
    </div>
  );
}

// Icon — small single-stroke inline SVG set (lucide geometry) for primary admin
// chrome, replacing OS emoji so glyphs inherit the token palette via
// `currentColor`, render identically across the tablets/laptops operators use,
// and stay on-brand for a serious tournament tool. Decorative by default
// (aria-hidden) since it always sits beside a text label. Add new glyphs to the
// map as needed rather than reaching for emoji.
function Icon({ name, size = 16, className }) {
  const inner = {
    megaphone: <><path d="m3 11 18-5v12L3 14v-3z" /><path d="M11.6 16.8a3 3 0 1 1-5.8-1.6" /></>,
    printer: <><path d="M6 9V2h12v7" /><path d="M6 18H4a2 2 0 0 1-2-2v-5a2 2 0 0 1 2-2h16a2 2 0 0 1 2 2v5a2 2 0 0 1-2 2h-2" /><path d="M6 14h12v8H6z" /></>,
    trophy: <><path d="M6 9H4.5a2.5 2.5 0 0 1 0-5H6" /><path d="M18 9h1.5a2.5 2.5 0 0 0 0-5H18" /><path d="M4 22h16" /><path d="M10 14.66V17c0 .55-.47.98-.97 1.21C7.85 18.75 7 20.24 7 22" /><path d="M14 14.66V17c0 .55.47.98.97 1.21C16.15 18.75 17 20.24 17 22" /><path d="M18 2H6v7a6 6 0 0 0 12 0V2Z" /></>,
    eye: <><path d="M2 12s3-7 10-7 10 7 10 7-3 7-10 7-10-7-10-7Z" /><circle cx="12" cy="12" r="3" /></>,
    calendar: <><rect width="18" height="18" x="3" y="4" rx="2" /><path d="M3 10h18" /><path d="M8 2v4" /><path d="M16 2v4" /></>,
    pencil: <><path d="M12 3H5a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7" /><path d="M18.4 2.6a2 2 0 0 1 2.8 2.8L11 15.6 7 17l1.4-4z" /></>,
    folder: <><path d="M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z" /></>,
  }[name];
  if (!inner) return null;
  return (
    <svg className={className} width={size} height={size} viewBox="0 0 24 24" fill="none"
      stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"
      aria-hidden="true" style={{ flexShrink: 0, verticalAlign: "-3px" }}>{inner}</svg>
  );
}

// Hook: register an Escape key listener that always calls the latest onClose
// without re-registering on every render (listener registered once, ref kept fresh).
function useEscapeToClose(onClose) {
  const { useRef, useEffect } = React;
  const cbRef = useRef(onClose);
  useEffect(() => { cbRef.current = onClose; }, [onClose]);
  useEffect(() => {
    const onKey = (e) => {
      if (e.key === "Escape" && typeof cbRef.current === "function") {
        e.preventDefault();
        cbRef.current();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);
}

// Returns true when el is a text-entry element (blocks navigation shortcuts
// to avoid clobbering cursor movement in inputs).
function isTextEntry(el) {
  if (!el) return false;
  const tag = (el.tagName || "").toLowerCase();
  return tag === "input" || tag === "textarea" || tag === "select" || !!el.isContentEditable;
}

// Returns true when el is any interactive element (blocks scoring shortcuts
// so native keyboard activation of buttons/links still works).
function isInteractiveTarget(el) {
  if (!el) return false;
  const tag = (el.tagName || "").toLowerCase();
  return tag === "input" || tag === "textarea" || tag === "select" || tag === "button" || tag === "a" || !!el.isContentEditable;
}

export { StatusBadge, formatDate, Toast, StableInput, pluralize, useEscapeToClose, isTextEntry, isInteractiveTarget, formatAdminHeaderSub, formatViewerHeaderEyebrow, confirmDialog, promptDialog, DialogHost, Icon };

if (typeof window !== "undefined") {
  window.StatusBadge = StatusBadge;
  window.formatDate = formatDate;
  window.Toast = Toast;
  window.StableInput = StableInput;
  window.pluralize = pluralize;
  window.formatLabel = formatLabel;
  window.formatLabelShort = formatLabelShort;
  window.useEscapeToClose = useEscapeToClose;
  window.isTextEntry = isTextEntry;
  window.isInteractiveTarget = isInteractiveTarget;
  window.formatAdminHeaderSub = formatAdminHeaderSub;
  window.formatViewerHeaderEyebrow = formatViewerHeaderEyebrow;
  window.confirmDialog = confirmDialog;
  window.promptDialog = promptDialog;
  window.DialogHost = DialogHost;
  window.Icon = Icon;
}

