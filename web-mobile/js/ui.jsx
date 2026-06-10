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
// operator in a noisy hall doesn't miss a failed action. The host (app.jsx)
// is responsible for not overwriting a still-visible error with a later
// non-error toast — see TOAST_ERROR_DWELL_MS, exported for the host's guard.
const TOAST_SUCCESS_DWELL_MS = 2700;
const TOAST_ERROR_DWELL_MS = 8000;

// Toast renders a single visible slot. The host (app.jsx) keeps one toast in
// state and re-mounts <Toast> with new props on each showToast call, so the
// component holds its OWN copy of the message/type and ignores a parent prop
// change that would overwrite a still-visible ERROR with a later non-error
// toast. This protects ops-critical failures from being silently replaced by
// a routine success, without requiring the host to track dwell windows.
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
      <div className="toast__icon">{shownIsError ? '⚠️' : '✅'}</div>
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

export { StatusBadge, formatDate, Toast, StableInput, pluralize, useEscapeToClose, isTextEntry, isInteractiveTarget, formatAdminHeaderSub, formatViewerHeaderEyebrow };

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
}

